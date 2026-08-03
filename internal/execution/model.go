package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/schemapilot/schemapilot/internal/migration"
)

type RunID string

var ErrRunNotFound = errors.New("run not found")

type Run struct {
	ID                     RunID
	GraphName              string
	GraphFingerprint       string
	StructureFingerprint   string
	EnvironmentFingerprint string
	GraphPath              string
	DatabasesPath          string
	Status                 migration.RunStatus
	Attempt                int
	StartedAt              time.Time
	FinishedAt             *time.Time
	Error                  string
}

type RunDefinition struct {
	Run     Run
	Graph   migration.Graph
	Created time.Time
}

type RunSummary struct {
	Run
	CompletedNodes int
	TotalNodes     int
}

type RunSnapshot struct {
	Run      Run
	Attempts []AttemptSnapshot
	Nodes    []NodeSnapshot
	Logs     []LogEntry
}

type AttemptSnapshot struct {
	Number           int
	GraphFingerprint string
	Status           migration.RunStatus
	StartedAt        time.Time
	FinishedAt       *time.Time
	Error            string
}

type NodeSnapshot struct {
	Name       string
	Database   string
	DependsOn  []string
	Status     migration.NodeStatus
	Attempt    int
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      string
	Scripts    []ScriptSnapshot
}

type ScriptSnapshot struct {
	Path         string
	Checksum     string
	Status       migration.ScriptStatus
	Attempt      int
	StartedAt    *time.Time
	FinishedAt   *time.Time
	RowsAffected int64
	Error        string
}

type LogLevel string

const (
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type LogEntry struct {
	Sequence int64
	At       time.Time
	Level    LogLevel
	Node     string
	Script   string
	Message  string
}

type TransitionKind string

const (
	TransitionAttemptStarted  TransitionKind = "attempt_started"
	TransitionNodeStarted     TransitionKind = "node_started"
	TransitionScriptStarted   TransitionKind = "script_started"
	TransitionScriptFinished  TransitionKind = "script_finished"
	TransitionNodeFinished    TransitionKind = "node_finished"
	TransitionAttemptFinished TransitionKind = "attempt_finished"
	TransitionRunFinished     TransitionKind = "run_finished"
	TransitionLogAppended     TransitionKind = "log_appended"
)

type Transition struct {
	Kind             TransitionKind
	RunID            RunID
	Attempt          int
	Node             string
	Script           string
	RunStatus        migration.RunStatus
	NodeStatus       migration.NodeStatus
	ScriptStatus     migration.ScriptStatus
	GraphFingerprint string
	Checksum         string
	At               time.Time
	RowsAffected     int64
	Error            string
	Log              LogEntry
}

type RunQuery struct {
	Statuses []migration.RunStatus
	Limit    int
}

type RunStore interface {
	Create(context.Context, RunDefinition) error
	Apply(context.Context, Transition) error
	Snapshot(context.Context, RunID) (RunSnapshot, error)
	Runs(context.Context, RunQuery) ([]RunSummary, error)
}

type ApplyOutcome string

const (
	ApplyOutcomeApplied        ApplyOutcome = "applied"
	ApplyOutcomeAlreadyApplied ApplyOutcome = "already_applied"
)

type ScriptApplication struct {
	GraphName string
	NodeName  string
	Path      string
	Checksum  string
	SQL       string
	Force     bool
}

type ApplyResult struct {
	Outcome      ApplyOutcome
	RowsAffected int64
}

type ChecksumMismatchError struct {
	Path     string
	Recorded string
	Current  string
}

func (mismatch *ChecksumMismatchError) Error() string {
	return fmt.Sprintf(
		"migration script %q checksum mismatch (recorded %s, current %s)",
		mismatch.Path,
		mismatch.Recorded,
		mismatch.Current,
	)
}

type TargetDatabase interface {
	Apply(context.Context, ScriptApplication) (ApplyResult, error)
	Ping(context.Context) error
	Close() error
}

type DatabaseConnector interface {
	Connect(context.Context, migration.DatabaseProfile) (TargetDatabase, error)
}
