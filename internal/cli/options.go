package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/schemapilot/schemapilot/internal/database"
	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
	"github.com/schemapilot/schemapilot/internal/project"
	"github.com/schemapilot/schemapilot/internal/runstore"
)

const (
	defaultGraphPath     = "migration.yaml"
	defaultDatabasesName = "databases.toml"
)

type projectOptions struct {
	graphPath     string
	databasesPath string
	statePath     string
}

type runtime struct {
	project   migration.Project
	engine    *execution.Engine
	connector *database.Connector
	store     *runstore.Store
}

func (options *projectOptions) bind(command *cobra.Command, includeState bool) {
	command.Flags().StringVar(&options.graphPath, "graph", "", "migration graph YAML path")
	command.Flags().StringVar(&options.databasesPath, "databases", "", "database profiles TOML path")
	if includeState {
		command.Flags().StringVar(&options.statePath, "state", "", "SQLite run journal path")
	}
}

func (options projectOptions) paths(positionalGraph string) (string, string, string, error) {
	if options.graphPath != "" && positionalGraph != "" {
		return "", "", "", errors.New("provide the graph as an argument or with --graph, not both")
	}
	graphPath := options.graphPath
	if graphPath == "" {
		graphPath = positionalGraph
	}
	if graphPath == "" {
		graphPath = defaultGraphPath
	}
	graphPath = filepath.Clean(graphPath)

	directory := filepath.Dir(graphPath)
	databasesPath := options.databasesPath
	if databasesPath == "" {
		databasesPath = filepath.Join(directory, defaultDatabasesName)
	}
	statePath := options.statePath
	if statePath == "" {
		statePath = filepath.Join(directory, ".schemapilot", "runs.db")
	}
	return graphPath, filepath.Clean(databasesPath), filepath.Clean(statePath), nil
}

func openRuntime(ctx context.Context, graphPath, databasesPath, statePath string) (*runtime, error) {
	loaded, err := project.Load(ctx, graphPath, databasesPath)
	if err != nil {
		return nil, err
	}
	runtime, err := openServiceRuntime(statePath)
	if err != nil {
		return nil, err
	}
	runtime.project = loaded
	return runtime, nil
}

func openServiceRuntime(statePath string) (*runtime, error) {
	store, err := openStore(statePath)
	if err != nil {
		return nil, err
	}
	connector := database.NewConnector()
	return &runtime{
		engine:    execution.NewEngine(store, connector),
		connector: connector,
		store:     store,
	}, nil
}

func openStore(path string) (*runstore.Store, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create run journal directory: %w", err)
	}
	store, err := runstore.Open(path)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func positionalGraph(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
