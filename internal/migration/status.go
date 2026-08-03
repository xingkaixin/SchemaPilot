package migration

type RunStatus string

const (
	RunStatusPending             RunStatus = "pending"
	RunStatusRunning             RunStatus = "running"
	RunStatusSucceeded           RunStatus = "succeeded"
	RunStatusCompletedWithErrors RunStatus = "completed_with_errors"
	RunStatusFailed              RunStatus = "failed"
	RunStatusCancelled           RunStatus = "cancelled"
)

type NodeStatus string

const (
	NodeStatusPending             NodeStatus = "pending"
	NodeStatusRunning             NodeStatus = "running"
	NodeStatusSucceeded           NodeStatus = "succeeded"
	NodeStatusCompletedWithErrors NodeStatus = "completed_with_errors"
	NodeStatusFailed              NodeStatus = "failed"
	NodeStatusBlocked             NodeStatus = "blocked"
	NodeStatusCancelled           NodeStatus = "cancelled"
)

func (status NodeStatus) AllowsDownstream() bool {
	return status == NodeStatusSucceeded || status == NodeStatusCompletedWithErrors
}

func (status NodeStatus) IsTerminal() bool {
	switch status {
	case NodeStatusSucceeded, NodeStatusCompletedWithErrors, NodeStatusFailed, NodeStatusBlocked, NodeStatusCancelled:
		return true
	default:
		return false
	}
}

type ScriptStatus string

const (
	ScriptStatusPending        ScriptStatus = "pending"
	ScriptStatusRunning        ScriptStatus = "running"
	ScriptStatusSucceeded      ScriptStatus = "succeeded"
	ScriptStatusAlreadyApplied ScriptStatus = "already_applied"
	ScriptStatusFailed         ScriptStatus = "failed"
	ScriptStatusBlocked        ScriptStatus = "blocked"
	ScriptStatusCancelled      ScriptStatus = "cancelled"
)
