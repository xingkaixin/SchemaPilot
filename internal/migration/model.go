package migration

import "time"

type Driver string

const (
	DriverPostgres  Driver = "postgres"
	DriverMySQL     Driver = "mysql"
	DriverSQLServer Driver = "sqlserver"
)

func (driver Driver) IsValid() bool {
	switch driver {
	case DriverPostgres, DriverMySQL, DriverSQLServer:
		return true
	default:
		return false
	}
}

type ErrorPolicy string

const (
	ErrorPolicyHalt     ErrorPolicy = "halt"
	ErrorPolicyContinue ErrorPolicy = "continue"
)

func (policy ErrorPolicy) IsValid() bool {
	switch policy {
	case ErrorPolicyHalt, ErrorPolicyContinue:
		return true
	default:
		return false
	}
}

type Graph struct {
	Version     int
	Name        string
	Parallelism int
	OnError     ErrorPolicy
	Nodes       []Node
}

type Node struct {
	Name        string
	Database    string
	DependsOn   []string
	Scripts     []Script
	ErrorPolicy ErrorPolicy
}

type Script struct {
	Path     string
	Checksum string
	SQL      string
}

type DatabaseProfile struct {
	Name               string
	Driver             Driver
	DSN                string
	MaxOpenConnections int
	ConnectionTimeout  time.Duration
}

type Project struct {
	Graph                  Graph
	Databases              map[string]DatabaseProfile
	GraphPath              string
	DatabasesPath          string
	RootDirectory          string
	Fingerprint            string
	StructureFingerprint   string
	EnvironmentFingerprint string
}

func (graph Graph) Node(name string) (Node, bool) {
	for _, node := range graph.Nodes {
		if node.Name == name {
			return node, true
		}
	}

	return Node{}, false
}

func (node Node) EffectiveErrorPolicy(graphPolicy ErrorPolicy) ErrorPolicy {
	if node.ErrorPolicy.IsValid() {
		return node.ErrorPolicy
	}

	return graphPolicy
}
