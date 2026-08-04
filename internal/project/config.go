package project

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"

	"github.com/schemapilot/schemapilot/internal/migration"
)

type graphConfig struct {
	Version     *int                  `yaml:"version"`
	Name        *string               `yaml:"name"`
	Parallelism *int                  `yaml:"parallelism"`
	OnError     *string               `yaml:"on_error"`
	Nodes       map[string]nodeConfig `yaml:"nodes"`
}

type nodeConfig struct {
	Database  *string  `yaml:"database"`
	DependsOn []string `yaml:"depends_on"`
	Scripts   []string `yaml:"scripts"`
	OnError   *string  `yaml:"on_error"`
}

type databaseConfig struct {
	Driver             *string `toml:"driver"`
	DSN                *string `toml:"dsn"`
	MaxOpenConnections *int    `toml:"max_open_connections"`
	ConnectionTimeout  *string `toml:"connection_timeout"`
}

type databasesConfig struct {
	Version   *int                      `toml:"version"`
	Databases map[string]databaseConfig `toml:"databases"`
}

func decodeGraph(data []byte) (graphConfig, error) {
	var config graphConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return graphConfig{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return graphConfig{}, fmt.Errorf("multiple YAML documents are not supported")
		}
		return graphConfig{}, err
	}
	return config, nil
}

func decodeDatabases(data []byte) (databasesConfig, error) {
	var config databasesConfig
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return databasesConfig{}, err
	}
	return config, nil
}

func marshalGraph(graph migration.Graph) ([]byte, error) {
	version := graph.Version
	name := graph.Name
	parallelism := graph.Parallelism
	onError := string(graph.OnError)
	config := graphConfig{Version: &version, Name: &name, Parallelism: &parallelism, OnError: &onError, Nodes: make(map[string]nodeConfig, len(graph.Nodes))}
	nodes := append([]migration.Node(nil), graph.Nodes...)
	sort.SliceStable(nodes, func(left, right int) bool { return nodes[left].Name < nodes[right].Name })
	for _, node := range nodes {
		database := node.Database
		nodeOutput := nodeConfig{Database: &database, DependsOn: append([]string(nil), node.DependsOn...), Scripts: make([]string, 0, len(node.Scripts))}
		sort.Strings(nodeOutput.DependsOn)
		if node.ErrorPolicy != "" {
			onError := string(node.ErrorPolicy)
			nodeOutput.OnError = &onError
		}
		for _, script := range node.Scripts {
			nodeOutput.Scripts = append(nodeOutput.Scripts, filepath.ToSlash(filepath.Clean(script.Path)))
		}
		config.Nodes[node.Name] = nodeOutput
	}
	return yaml.Marshal(config)
}

func marshalDatabases(config databasesConfig) ([]byte, error) {
	return toml.Marshal(config)
}
