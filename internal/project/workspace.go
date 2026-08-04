package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/schemapilot/schemapilot/internal/migration"
)

var nonNameCharacterPattern = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

var ErrDatabaseProfileNotFound = errors.New("database profile not found")

type Workspace struct {
	Graph               migration.Graph
	Databases           map[string]migration.DatabaseProfile
	Files               []string
	GraphPath           string
	DatabasesPath       string
	RootDirectory       string
	Fingerprint         string
	DatabaseFingerprint string
	Ready               bool
	Problems            []string
}

type DatabaseProfileInput struct {
	Driver             migration.Driver
	DSN                *string
	MaxOpenConnections int
	ConnectionTimeout  time.Duration
}

func Inspect(ctx context.Context, graphPath, databasesPath string) (Workspace, error) {
	ctx = nonNilContext(ctx)
	if err := contextError(ctx); err != nil {
		return Workspace{}, err
	}

	absGraphPath, err := absolutePath(graphPath, "graph path")
	if err != nil {
		return Workspace{}, err
	}
	absDatabasesPath, err := absolutePath(databasesPath, "databases path")
	if err != nil {
		return Workspace{}, err
	}
	root := filepath.Dir(absGraphPath)
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Workspace{}, wrapField(absGraphPath, "root directory", err)
	}

	graph, graphExists, graphProblems, err := inspectGraph(ctx, absGraphPath, root, rootReal)
	if err != nil {
		return Workspace{}, err
	}
	databases, databaseData, databasesExist, databaseProblems, err := inspectDatabases(absDatabasesPath)
	if err != nil {
		return Workspace{}, err
	}
	files, err := FileTree(ctx, absGraphPath)
	if err != nil {
		return Workspace{}, err
	}

	problems := append([]string(nil), graphProblems...)
	problems = append(problems, databaseProblems...)
	if !graphExists {
		problems = append(problems, "migration graph has not been saved")
	}
	if !databasesExist {
		problems = append(problems, "database profiles have not been configured")
	}
	if validationErr := migration.ValidateProject(migration.Project{Graph: graph, Databases: databases}); validationErr != nil {
		var detailed *migration.ValidationError
		if errors.As(validationErr, &detailed) {
			problems = append(problems, detailed.Problems...)
		} else {
			problems = append(problems, validationErr.Error())
		}
	}
	problems = uniqueSorted(problems)

	graphFingerprint, err := fingerprint(graph)
	if err != nil {
		return Workspace{}, wrapField(absGraphPath, "fingerprint", err)
	}
	databaseFingerprint := checksumBytes(databaseData)
	return Workspace{
		Graph:               graph,
		Databases:           databases,
		Files:               files,
		GraphPath:           absGraphPath,
		DatabasesPath:       absDatabasesPath,
		RootDirectory:       root,
		Fingerprint:         graphFingerprint,
		DatabaseFingerprint: databaseFingerprint,
		Ready:               graphExists && databasesExist && len(problems) == 0,
		Problems:            problems,
	}, nil
}

func LoadDatabases(ctx context.Context, databasesPath string) (map[string]migration.DatabaseProfile, error) {
	ctx = nonNilContext(ctx)
	absPath, data, err := readConfigFile(ctx, databasesPath, "databases path")
	if err != nil {
		return nil, err
	}
	return loadDatabases(absPath, data)
}

func SaveDatabaseProfile(ctx context.Context, databasesPath, name string, input DatabaseProfileInput) error {
	ctx = nonNilContext(ctx)
	if err := contextError(ctx); err != nil {
		return err
	}
	if !migration.ValidName(name) {
		return fmt.Errorf("database profile name must start with a letter and contain only letters, digits, underscores, or hyphens")
	}
	if !input.Driver.IsValid() {
		return fmt.Errorf("unsupported database driver %q", input.Driver)
	}
	if input.MaxOpenConnections < 1 {
		return errors.New("max_open_connections must be at least 1")
	}
	if input.ConnectionTimeout <= 0 {
		return errors.New("connection_timeout must be greater than zero")
	}

	absPath, err := absolutePath(databasesPath, "databases path")
	if err != nil {
		return err
	}
	document, mode, err := editableDatabasesDocument(absPath)
	if err != nil {
		return err
	}
	current, exists := document.Databases[name]
	if input.DSN == nil || strings.TrimSpace(*input.DSN) == "" {
		if !exists || current.DSN == nil || strings.TrimSpace(*current.DSN) == "" {
			return errors.New("dsn is required for a new database profile")
		}
	} else {
		dsn := *input.DSN
		current.DSN = &dsn
	}
	driver := string(input.Driver)
	maxConnections := input.MaxOpenConnections
	timeout := input.ConnectionTimeout.String()
	current.Driver = &driver
	current.MaxOpenConnections = &maxConnections
	current.ConnectionTimeout = &timeout
	document.Databases[name] = current

	data, err := marshalDatabases(document)
	if err != nil {
		return wrapField(absPath, "toml", err)
	}
	return writeConfigFile(ctx, absPath, data, mode)
}

func DeleteDatabaseProfile(ctx context.Context, databasesPath, name string) error {
	ctx = nonNilContext(ctx)
	absPath, err := absolutePath(databasesPath, "databases path")
	if err != nil {
		return err
	}
	document, mode, err := editableDatabasesDocument(absPath)
	if err != nil {
		return err
	}
	if _, exists := document.Databases[name]; !exists {
		return fmt.Errorf("%w: %q", ErrDatabaseProfileNotFound, name)
	}
	delete(document.Databases, name)
	data, err := marshalDatabases(document)
	if err != nil {
		return wrapField(absPath, "toml", err)
	}
	return writeConfigFile(ctx, absPath, data, mode)
}

func inspectGraph(ctx context.Context, graphPath, root, rootReal string) (migration.Graph, bool, []string, error) {
	data, err := os.ReadFile(graphPath)
	if errors.Is(err, os.ErrNotExist) {
		return defaultGraph(root), false, nil, nil
	}
	if err != nil {
		return migration.Graph{}, false, nil, wrapField(graphPath, "graph path", err)
	}
	graph, err := loadGraphDefinition(ctx, graphPath, root, data)
	if err != nil {
		return migration.Graph{}, true, nil, err
	}
	problems := make([]string, 0)
	for nodeIndex := range graph.Nodes {
		for scriptIndex, script := range graph.Nodes[nodeIndex].Scripts {
			if err := contextError(ctx); err != nil {
				return migration.Graph{}, true, nil, err
			}
			_, content, loadErr := loadScript(root, rootReal, script.Path)
			if loadErr != nil {
				problems = append(problems, fmt.Sprintf("node %q script %q: %v", graph.Nodes[nodeIndex].Name, script.Path, loadErr))
				continue
			}
			graph.Nodes[nodeIndex].Scripts[scriptIndex] = loadedScript(script.Path, content)
		}
	}
	return graph, true, problems, nil
}

func inspectDatabases(path string) (map[string]migration.DatabaseProfile, []byte, bool, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]migration.DatabaseProfile{}, nil, false, nil, nil
	}
	if err != nil {
		return nil, nil, false, nil, wrapField(path, "databases path", err)
	}
	document, err := decodeDatabases(data)
	if err != nil {
		return nil, nil, true, nil, wrapField(path, "toml", err)
	}
	if document.Version == nil {
		return nil, nil, true, nil, wrapField(path, "version", errors.New("field is required"))
	}
	if *document.Version != currentDatabaseVersion {
		return nil, nil, true, nil, wrapField(path, "version", fmt.Errorf("must be %d", currentDatabaseVersion))
	}

	profiles := make(map[string]migration.DatabaseProfile, len(document.Databases))
	problems := make([]string, 0)
	for _, name := range sortedDatabaseConfigNames(document.Databases) {
		values := document.Databases[name]
		if !migration.ValidName(name) {
			problems = append(problems, fmt.Sprintf("database profile %q has an invalid name", name))
		}
		profile, profileProblems := inspectDatabaseProfile(name, values)
		profiles[name] = profile
		problems = append(problems, profileProblems...)
	}
	return profiles, data, true, problems, nil
}

func inspectDatabaseProfile(name string, values databaseConfig) (migration.DatabaseProfile, []string) {
	problems := make([]string, 0)
	fieldPath := "database profile " + name
	driver := migration.Driver("")
	if values.Driver == nil || !migration.Driver(*values.Driver).IsValid() {
		problems = append(problems, fieldPath+" has an unsupported driver")
	} else {
		driver = migration.Driver(*values.Driver)
	}
	dsn := ""
	if values.DSN == nil || strings.TrimSpace(*values.DSN) == "" {
		problems = append(problems, fieldPath+" has an empty DSN")
	} else {
		dsn = *values.DSN
		expanded, err := expandEnvironment(dsn)
		if err != nil {
			problems = append(problems, fieldPath+" "+err.Error())
		} else {
			dsn = expanded
		}
	}
	maxConnections := defaultMaxOpenConnections
	if values.MaxOpenConnections != nil {
		maxConnections = *values.MaxOpenConnections
	}
	if maxConnections < 1 {
		problems = append(problems, fieldPath+" max_open_connections must be at least 1")
	}
	timeout := defaultConnectionTimeout
	if values.ConnectionTimeout != nil {
		parsed, err := time.ParseDuration(*values.ConnectionTimeout)
		if err != nil || parsed <= 0 {
			problems = append(problems, fieldPath+" connection_timeout must be a positive duration")
		} else {
			timeout = parsed
		}
	}
	return migration.DatabaseProfile{
		Name:               name,
		Driver:             driver,
		DSN:                dsn,
		MaxOpenConnections: maxConnections,
		ConnectionTimeout:  timeout,
	}, problems
}

func editableDatabasesDocument(path string) (databasesConfig, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		version := currentDatabaseVersion
		return databasesConfig{Version: &version, Databases: map[string]databaseConfig{}}, 0o600, nil
	}
	if err != nil {
		return databasesConfig{}, 0, wrapField(path, "databases path", err)
	}
	document, err := decodeDatabases(data)
	if err != nil {
		return databasesConfig{}, 0, wrapField(path, "toml", err)
	}
	if document.Version == nil || *document.Version != currentDatabaseVersion {
		return databasesConfig{}, 0, wrapField(path, "version", fmt.Errorf("must be %d", currentDatabaseVersion))
	}
	if document.Databases == nil {
		document.Databases = map[string]databaseConfig{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return databasesConfig{}, 0, wrapField(path, "databases path", err)
	}
	return document, info.Mode().Perm(), nil
}

func defaultGraph(root string) migration.Graph {
	name := nonNameCharacterPattern.ReplaceAllString(filepath.Base(root), "-")
	name = strings.Trim(name, "-_")
	if !migration.ValidName(name) {
		name = "migration"
	}
	return migration.Graph{
		Version:     migration.CurrentGraphVersion,
		Name:        name,
		Parallelism: defaultParallelism,
		OnError:     migration.ErrorPolicyHalt,
		Nodes:       []migration.Node{},
	}
}

func sortedDatabaseConfigNames(config map[string]databaseConfig) []string {
	names := make([]string, 0, len(config))
	for name := range config {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func checksumBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
