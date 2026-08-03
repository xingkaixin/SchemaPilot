package httpapi

import (
	"slices"
	"time"

	"github.com/schemapilot/schemapilot/internal/database"
	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

type projectResponse struct {
	Graph       graphDTO      `json:"graph"`
	Databases   []databaseDTO `json:"databases"`
	Files       []string      `json:"files"`
	Fingerprint string        `json:"fingerprint"`
}

type graphDTO struct {
	Version     int                   `json:"version"`
	Name        string                `json:"name"`
	Parallelism int                   `json:"parallelism"`
	OnError     migration.ErrorPolicy `json:"on_error"`
	Nodes       []nodeDTO             `json:"nodes"`
}

type nodeDTO struct {
	Name      string                `json:"name"`
	Database  string                `json:"database"`
	DependsOn []string              `json:"depends_on"`
	OnError   migration.ErrorPolicy `json:"on_error,omitempty"`
	Scripts   []scriptDTO           `json:"scripts"`
}

type scriptDTO struct {
	Path     string `json:"path"`
	Checksum string `json:"checksum,omitempty"`
}

type databaseDTO struct {
	Name               string           `json:"name"`
	Driver             migration.Driver `json:"driver"`
	DSN                string           `json:"dsn"`
	MaxOpenConnections int              `json:"max_open_connections"`
	ConnectionTimeout  string           `json:"connection_timeout"`
}

type scriptResponse struct {
	Path        string `json:"path"`
	Content     string `json:"content"`
	Checksum    string `json:"checksum"`
	Fingerprint string `json:"fingerprint"`
}

type scriptUpdateRequest struct {
	Content string `json:"content"`
}

type startRunRequest struct {
	Force bool `json:"force"`
}

type runAcceptedResponse struct {
	ID     execution.RunID     `json:"id"`
	Status migration.RunStatus `json:"status"`
}

type runsResponse struct {
	Runs []runSummaryDTO `json:"runs"`
}

type runSummaryDTO struct {
	ID             execution.RunID     `json:"id"`
	GraphName      string              `json:"graph_name"`
	Status         migration.RunStatus `json:"status"`
	Attempt        int                 `json:"attempt"`
	StartedAt      time.Time           `json:"started_at"`
	FinishedAt     *time.Time          `json:"finished_at,omitempty"`
	Error          string              `json:"error,omitempty"`
	CompletedNodes int                 `json:"completed_nodes"`
	TotalNodes     int                 `json:"total_nodes"`
}

type runSnapshotDTO struct {
	Run      runSummaryDTO     `json:"run"`
	Attempts []attemptDTO      `json:"attempts"`
	Nodes    []nodeSnapshotDTO `json:"nodes"`
	Logs     []logDTO          `json:"logs"`
}

type attemptDTO struct {
	Number           int                 `json:"number"`
	GraphFingerprint string              `json:"graph_fingerprint"`
	Status           migration.RunStatus `json:"status"`
	StartedAt        time.Time           `json:"started_at"`
	FinishedAt       *time.Time          `json:"finished_at,omitempty"`
	Error            string              `json:"error,omitempty"`
}

type nodeSnapshotDTO struct {
	Name       string               `json:"name"`
	Database   string               `json:"database"`
	DependsOn  []string             `json:"depends_on"`
	Status     migration.NodeStatus `json:"status"`
	Attempt    int                  `json:"attempt"`
	StartedAt  *time.Time           `json:"started_at,omitempty"`
	FinishedAt *time.Time           `json:"finished_at,omitempty"`
	Error      string               `json:"error,omitempty"`
	Scripts    []scriptSnapshotDTO  `json:"scripts"`
}

type scriptSnapshotDTO struct {
	Path         string                 `json:"path"`
	Checksum     string                 `json:"checksum"`
	Status       migration.ScriptStatus `json:"status"`
	Attempt      int                    `json:"attempt"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	FinishedAt   *time.Time             `json:"finished_at,omitempty"`
	RowsAffected int64                  `json:"rows_affected"`
	Error        string                 `json:"error,omitempty"`
}

type logDTO struct {
	Sequence int64              `json:"sequence"`
	At       time.Time          `json:"at"`
	Level    execution.LogLevel `json:"level"`
	Node     string             `json:"node,omitempty"`
	Script   string             `json:"script,omitempty"`
	Message  string             `json:"message"`
}

type databaseTestResponse struct {
	Name      string `json:"name"`
	LatencyMS int64  `json:"latency_ms"`
}

func projectDTO(project migration.Project, files []string) projectResponse {
	databases := make([]databaseDTO, 0, len(project.Databases))
	for _, name := range sortedDatabaseNames(project.Databases) {
		profile := project.Databases[name]
		databases = append(databases, databaseDTO{
			Name:               profile.Name,
			Driver:             profile.Driver,
			DSN:                database.MaskDSN(profile.DSN),
			MaxOpenConnections: profile.MaxOpenConnections,
			ConnectionTimeout:  profile.ConnectionTimeout.String(),
		})
	}

	return projectResponse{
		Graph:       graphToDTO(project.Graph),
		Databases:   databases,
		Files:       files,
		Fingerprint: project.Fingerprint,
	}
}

func graphToDTO(graph migration.Graph) graphDTO {
	nodes := make([]nodeDTO, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		scripts := make([]scriptDTO, 0, len(node.Scripts))
		for _, script := range node.Scripts {
			scripts = append(scripts, scriptDTO{Path: script.Path, Checksum: script.Checksum})
		}
		nodes = append(nodes, nodeDTO{
			Name:      node.Name,
			Database:  node.Database,
			DependsOn: append([]string(nil), node.DependsOn...),
			OnError:   node.ErrorPolicy,
			Scripts:   scripts,
		})
	}
	return graphDTO{Version: graph.Version, Name: graph.Name, Parallelism: graph.Parallelism, OnError: graph.OnError, Nodes: nodes}
}

func (graph graphDTO) domain() migration.Graph {
	nodes := make([]migration.Node, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		scripts := make([]migration.Script, 0, len(node.Scripts))
		for _, script := range node.Scripts {
			scripts = append(scripts, migration.Script{Path: script.Path})
		}
		nodes = append(nodes, migration.Node{
			Name:        node.Name,
			Database:    node.Database,
			DependsOn:   append([]string(nil), node.DependsOn...),
			ErrorPolicy: node.OnError,
			Scripts:     scripts,
		})
	}
	return migration.Graph{Version: graph.Version, Name: graph.Name, Parallelism: graph.Parallelism, OnError: graph.OnError, Nodes: nodes}
}

func snapshotDTO(snapshot execution.RunSnapshot) runSnapshotDTO {
	attempts := make([]attemptDTO, 0, len(snapshot.Attempts))
	for _, attempt := range snapshot.Attempts {
		attempts = append(attempts, attemptDTO{
			Number:           attempt.Number,
			GraphFingerprint: attempt.GraphFingerprint,
			Status:           attempt.Status,
			StartedAt:        attempt.StartedAt,
			FinishedAt:       attempt.FinishedAt,
			Error:            attempt.Error,
		})
	}

	nodes := make([]nodeSnapshotDTO, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		scripts := make([]scriptSnapshotDTO, 0, len(node.Scripts))
		for _, script := range node.Scripts {
			scripts = append(scripts, scriptSnapshotDTO{
				Path:         script.Path,
				Checksum:     script.Checksum,
				Status:       script.Status,
				Attempt:      script.Attempt,
				StartedAt:    script.StartedAt,
				FinishedAt:   script.FinishedAt,
				RowsAffected: script.RowsAffected,
				Error:        script.Error,
			})
		}
		nodes = append(nodes, nodeSnapshotDTO{
			Name:       node.Name,
			Database:   node.Database,
			DependsOn:  append([]string(nil), node.DependsOn...),
			Status:     node.Status,
			Attempt:    node.Attempt,
			StartedAt:  node.StartedAt,
			FinishedAt: node.FinishedAt,
			Error:      node.Error,
			Scripts:    scripts,
		})
	}

	logs := make([]logDTO, 0, len(snapshot.Logs))
	for _, entry := range snapshot.Logs {
		logs = append(logs, logDTO{
			Sequence: entry.Sequence,
			At:       entry.At,
			Level:    entry.Level,
			Node:     entry.Node,
			Script:   entry.Script,
			Message:  entry.Message,
		})
	}

	return runSnapshotDTO{
		Run: runSummaryDTO{
			ID:             snapshot.Run.ID,
			GraphName:      snapshot.Run.GraphName,
			Status:         snapshot.Run.Status,
			Attempt:        snapshot.Run.Attempt,
			StartedAt:      snapshot.Run.StartedAt,
			FinishedAt:     snapshot.Run.FinishedAt,
			Error:          snapshot.Run.Error,
			TotalNodes:     len(snapshot.Nodes),
			CompletedNodes: completedNodeCount(snapshot.Nodes),
		},
		Attempts: attempts,
		Nodes:    nodes,
		Logs:     logs,
	}
}

func summaryDTO(summary execution.RunSummary) runSummaryDTO {
	return runSummaryDTO{
		ID:             summary.ID,
		GraphName:      summary.GraphName,
		Status:         summary.Status,
		Attempt:        summary.Attempt,
		StartedAt:      summary.StartedAt,
		FinishedAt:     summary.FinishedAt,
		Error:          summary.Error,
		CompletedNodes: summary.CompletedNodes,
		TotalNodes:     summary.TotalNodes,
	}
}

func completedNodeCount(nodes []execution.NodeSnapshot) int {
	count := 0
	for _, node := range nodes {
		if node.Status.AllowsDownstream() {
			count++
		}
	}
	return count
}

func sortedDatabaseNames(databases map[string]migration.DatabaseProfile) []string {
	names := make([]string, 0, len(databases))
	for name := range databases {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
