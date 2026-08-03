package runstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

func validateRunDefinition(definition execution.RunDefinition) error {
	if strings.TrimSpace(string(definition.Run.ID)) == "" {
		return errors.New("run id is required")
	}

	if !isValidRunStatus(definition.Run.Status) {
		return fmt.Errorf("invalid run status %q", definition.Run.Status)
	}
	if definition.Run.Attempt < 0 {
		return errors.New("run attempt cannot be negative")
	}

	graphName := definition.Run.GraphName
	if graphName == "" {
		graphName = definition.Graph.Name
	}
	if strings.TrimSpace(graphName) == "" {
		return errors.New("graph name is required")
	}

	nodeNames := make(map[string]struct{}, len(definition.Graph.Nodes))
	for index, node := range definition.Graph.Nodes {
		if strings.TrimSpace(node.Name) == "" {
			return fmt.Errorf("graph node %d has no name", index)
		}
		if _, exists := nodeNames[node.Name]; exists {
			return fmt.Errorf("graph node %q is duplicated", node.Name)
		}
		nodeNames[node.Name] = struct{}{}

		scriptPaths := make(map[string]struct{}, len(node.Scripts))
		for scriptIndex, script := range node.Scripts {
			if strings.TrimSpace(script.Path) == "" {
				return fmt.Errorf("graph node %q script %d has no path", node.Name, scriptIndex)
			}
			if _, exists := scriptPaths[script.Path]; exists {
				return fmt.Errorf("graph node %q script %q is duplicated", node.Name, script.Path)
			}
			scriptPaths[script.Path] = struct{}{}
		}
	}

	for _, node := range definition.Graph.Nodes {
		dependencies := make(map[string]struct{}, len(node.DependsOn))
		for _, dependency := range node.DependsOn {
			if _, exists := nodeNames[dependency]; !exists {
				return fmt.Errorf("graph node %q depends on unknown node %q", node.Name, dependency)
			}
			if _, exists := dependencies[dependency]; exists {
				return fmt.Errorf("graph node %q duplicates dependency %q", node.Name, dependency)
			}
			dependencies[dependency] = struct{}{}
		}
	}

	return nil
}

func validateTransition(transition execution.Transition) error {
	if strings.TrimSpace(string(transition.RunID)) == "" {
		return errors.New("transition run id is required")
	}
	if transition.Attempt < 1 {
		return errors.New("transition attempt must be positive")
	}
	if transition.At.IsZero() && transition.Kind != execution.TransitionLogAppended {
		return errors.New("transition timestamp is required")
	}

	switch transition.Kind {
	case execution.TransitionAttemptStarted:
		if transition.RunStatus != migration.RunStatusRunning {
			return fmt.Errorf("attempt started status must be %q", migration.RunStatusRunning)
		}
		if strings.TrimSpace(transition.GraphFingerprint) == "" {
			return errors.New("attempt started transition requires a graph fingerprint")
		}
	case execution.TransitionNodeStarted:
		if strings.TrimSpace(transition.Node) == "" {
			return errors.New("node started transition requires a node")
		}
		if transition.NodeStatus != migration.NodeStatusRunning {
			return fmt.Errorf("node started status must be %q", migration.NodeStatusRunning)
		}
	case execution.TransitionScriptStarted:
		if strings.TrimSpace(transition.Node) == "" {
			return errors.New("script started transition requires a node")
		}
		if strings.TrimSpace(transition.Script) == "" {
			return errors.New("script started transition requires a script")
		}
		if transition.ScriptStatus != migration.ScriptStatusRunning {
			return fmt.Errorf("script started status must be %q", migration.ScriptStatusRunning)
		}
		if strings.TrimSpace(transition.Checksum) == "" {
			return errors.New("script started transition requires a checksum")
		}
	case execution.TransitionScriptFinished:
		if strings.TrimSpace(transition.Node) == "" {
			return errors.New("script finished transition requires a node")
		}
		if strings.TrimSpace(transition.Script) == "" {
			return errors.New("script finished transition requires a script")
		}
		if !isTerminalScriptStatus(transition.ScriptStatus) {
			return fmt.Errorf("script finished status %q is not terminal", transition.ScriptStatus)
		}
	case execution.TransitionNodeFinished:
		if strings.TrimSpace(transition.Node) == "" {
			return errors.New("node finished transition requires a node")
		}
		if !isTerminalNodeStatus(transition.NodeStatus) {
			return fmt.Errorf("node finished status %q is not terminal", transition.NodeStatus)
		}
	case execution.TransitionAttemptFinished:
		if !isTerminalRunStatus(transition.RunStatus) {
			return fmt.Errorf("attempt finished status %q is not terminal", transition.RunStatus)
		}
	case execution.TransitionRunFinished:
		if !isTerminalRunStatus(transition.RunStatus) {
			return fmt.Errorf("run finished status %q is not terminal", transition.RunStatus)
		}
	case execution.TransitionLogAppended:
		if transition.At.IsZero() && transition.Log.At.IsZero() {
			return errors.New("log transition requires a timestamp")
		}
		if !isValidLogLevel(transition.Log.Level) {
			return fmt.Errorf("invalid log level %q", transition.Log.Level)
		}
		if strings.TrimSpace(transition.Log.Message) == "" {
			return errors.New("log transition requires a message")
		}
	default:
		return fmt.Errorf("unknown transition kind %q", transition.Kind)
	}

	return nil
}

func transitionTime(transition execution.Transition) time.Time {
	if !transition.At.IsZero() {
		return transition.At
	}

	return transition.Log.At
}

func isValidRunStatus(status migration.RunStatus) bool {
	switch status {
	case migration.RunStatusPending, migration.RunStatusRunning, migration.RunStatusSucceeded, migration.RunStatusFailed, migration.RunStatusCancelled, migration.RunStatusCompletedWithErrors:
		return true
	default:
		return false
	}
}

func isTerminalRunStatus(status migration.RunStatus) bool {
	switch status {
	case migration.RunStatusSucceeded, migration.RunStatusFailed, migration.RunStatusCancelled, migration.RunStatusCompletedWithErrors:
		return true
	default:
		return false
	}
}

func isTerminalNodeStatus(status migration.NodeStatus) bool {
	return status.IsTerminal()
}

func isTerminalScriptStatus(status migration.ScriptStatus) bool {
	switch status {
	case migration.ScriptStatusSucceeded, migration.ScriptStatusAlreadyApplied, migration.ScriptStatusFailed, migration.ScriptStatusBlocked, migration.ScriptStatusCancelled:
		return true
	default:
		return false
	}
}

func isValidLogLevel(level execution.LogLevel) bool {
	switch level {
	case execution.LogLevelInfo, execution.LogLevelWarn, execution.LogLevelError:
		return true
	default:
		return false
	}
}

func timeValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}

	return value.UnixNano()
}

func timeFromUnixNano(value int64) time.Time {
	return time.Unix(0, value).UTC()
}
