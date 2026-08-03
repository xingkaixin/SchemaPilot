package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/schemapilot/schemapilot/internal/migration"
)

type executionRequest struct {
	runID   RunID
	attempt int
	project migration.Project
	force   bool
	notify  NotifyFunc
}

type attemptExecutor struct {
	engine   *Engine
	request  executionRequest
	notifyMu sync.Mutex
}

type nodeResult struct {
	name   string
	status migration.NodeStatus
	err    error
}

type scriptFailure struct {
	cause error
}

func (failure *scriptFailure) Error() string {
	return failure.cause.Error()
}

func (failure *scriptFailure) Unwrap() error {
	return failure.cause
}

func (engine *Engine) execute(ctx context.Context, request executionRequest) error {
	executor := &attemptExecutor{engine: engine, request: request}
	startedAt := time.Now().UTC()
	if err := executor.apply(ctx, Transition{
		Kind:             TransitionAttemptStarted,
		RunID:            request.runID,
		Attempt:          request.attempt,
		RunStatus:        migration.RunStatusRunning,
		GraphFingerprint: request.project.Fingerprint,
		At:               startedAt,
	}); err != nil {
		return err
	}
	if err := executor.log(ctx, LogLevelInfo, "", "", fmt.Sprintf("attempt %d started with %d workers", request.attempt, request.project.Graph.Parallelism)); err != nil {
		return err
	}

	snapshot, err := engine.store.Snapshot(ctx, request.runID)
	if err != nil {
		return executor.finish(ctx, migration.RunStatusFailed, fmt.Errorf("load run state: %w", err))
	}
	statuses, err := executor.runGraph(ctx, snapshot)
	if err != nil {
		status := migration.RunStatusFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = migration.RunStatusCancelled
		}
		return executor.finish(ctx, status, err)
	}

	status := runStatus(statuses)
	if status == migration.RunStatusFailed {
		return executor.finish(ctx, status, &RunFailedError{RunID: request.runID})
	}
	return executor.finish(ctx, status, nil)
}

func (executor *attemptExecutor) runGraph(ctx context.Context, snapshot RunSnapshot) (map[string]migration.NodeStatus, error) {
	statuses := previousNodeStatuses(executor.request.project.Graph, snapshot)
	pending := pendingNodes(executor.request.project.Graph, statuses)
	results := make(chan nodeResult, len(pending))
	running := 0

	for len(pending) > 0 || running > 0 {
		if ctx.Err() != nil && running == 0 {
			if err := executor.cancelPending(context.WithoutCancel(ctx), pending, statuses); err != nil {
				return statuses, err
			}
			return statuses, ctx.Err()
		}

		progressed, err := executor.scheduleReady(ctx, pending, statuses, results, &running)
		if err != nil {
			return statuses, err
		}
		if running == 0 {
			if len(pending) == 0 {
				break
			}
			if !progressed {
				return statuses, errors.New("scheduler cannot make progress")
			}
			continue
		}

		select {
		case result := <-results:
			running--
			statuses[result.name] = result.status
			if result.err != nil && ctx.Err() == nil {
				return statuses, result.err
			}
		case <-ctx.Done():
			result := <-results
			running--
			statuses[result.name] = result.status
		}
	}
	if ctx.Err() != nil && hasNodeStatus(statuses, migration.NodeStatusCancelled) {
		return statuses, ctx.Err()
	}

	return statuses, nil
}

func (executor *attemptExecutor) scheduleReady(
	ctx context.Context,
	pending map[string]migration.Node,
	statuses map[string]migration.NodeStatus,
	results chan<- nodeResult,
	running *int,
) (bool, error) {
	if ctx.Err() != nil {
		return false, nil
	}

	progressed := false
	for _, node := range executor.request.project.Graph.Nodes {
		if *running >= executor.request.project.Graph.Parallelism {
			break
		}
		if _, exists := pending[node.Name]; !exists {
			continue
		}

		state := dependencyState(node, statuses)
		switch state {
		case dependenciesWaiting:
			continue
		case dependenciesBlocked:
			if err := executor.finishUnstartedNode(ctx, node, migration.NodeStatusBlocked, "blocked by a failed dependency"); err != nil {
				return progressed, err
			}
			statuses[node.Name] = migration.NodeStatusBlocked
			delete(pending, node.Name)
			progressed = true
		case dependenciesReady:
			delete(pending, node.Name)
			(*running)++
			progressed = true
			go func() {
				status, err := executor.runNode(ctx, node)
				results <- nodeResult{name: node.Name, status: status, err: err}
			}()
		}
	}

	return progressed, nil
}

func (executor *attemptExecutor) runNode(ctx context.Context, node migration.Node) (migration.NodeStatus, error) {
	startedAt := time.Now().UTC()
	if err := executor.apply(ctx, Transition{
		Kind:       TransitionNodeStarted,
		RunID:      executor.request.runID,
		Attempt:    executor.request.attempt,
		Node:       node.Name,
		NodeStatus: migration.NodeStatusRunning,
		At:         startedAt,
	}); err != nil {
		return migration.NodeStatusFailed, err
	}
	if err := executor.log(ctx, LogLevelInfo, node.Name, "", "node started"); err != nil {
		return migration.NodeStatusFailed, err
	}

	profile := executor.request.project.Databases[node.Database]
	target, err := executor.engine.connector.Connect(ctx, profile)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return executor.cancelNode(context.WithoutCancel(ctx), node, 0, err)
		}
		return executor.failNode(ctx, node, fmt.Errorf("connect to database profile %s: %w", node.Database, err))
	}
	defer target.Close()

	hadErrors := false
	for index, script := range node.Scripts {
		_, applyErr := executor.runScript(ctx, target, node, script)
		if applyErr == nil {
			continue
		}
		if errors.Is(applyErr, context.Canceled) || errors.Is(applyErr, context.DeadlineExceeded) {
			return executor.cancelNode(context.WithoutCancel(ctx), node, index+1, applyErr)
		}

		var migrationFailure *scriptFailure
		if !errors.As(applyErr, &migrationFailure) {
			return migration.NodeStatusFailed, applyErr
		}
		var checksumMismatch *ChecksumMismatchError
		if errors.As(migrationFailure.cause, &checksumMismatch) {
			return executor.haltNode(ctx, node, index+1, migrationFailure.cause)
		}

		hadErrors = true
		if node.EffectiveErrorPolicy(executor.request.project.Graph.OnError) == migration.ErrorPolicyContinue {
			continue
		}
		return executor.haltNode(ctx, node, index+1, migrationFailure.cause)
	}

	status := migration.NodeStatusSucceeded
	if hadErrors {
		status = migration.NodeStatusCompletedWithErrors
	}
	if err := executor.finishNode(ctx, node.Name, status, ""); err != nil {
		return status, err
	}
	return status, nil
}

func (executor *attemptExecutor) runScript(
	ctx context.Context,
	target TargetDatabase,
	node migration.Node,
	script migration.Script,
) (ApplyResult, error) {
	startedAt := time.Now().UTC()
	if err := executor.apply(ctx, Transition{
		Kind:         TransitionScriptStarted,
		RunID:        executor.request.runID,
		Attempt:      executor.request.attempt,
		Node:         node.Name,
		Script:       script.Path,
		Checksum:     script.Checksum,
		ScriptStatus: migration.ScriptStatusRunning,
		At:           startedAt,
	}); err != nil {
		return ApplyResult{}, err
	}

	result, err := target.Apply(ctx, ScriptApplication{
		GraphName: executor.request.project.Graph.Name,
		NodeName:  node.Name,
		Path:      script.Path,
		Checksum:  script.Checksum,
		SQL:       script.SQL,
		Force:     executor.request.force,
	})
	if err != nil {
		status := migration.ScriptStatusFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = migration.ScriptStatusCancelled
		}
		finishErr := executor.finishScript(context.WithoutCancel(ctx), node.Name, script.Path, status, 0, err.Error())
		logErr := executor.log(context.WithoutCancel(ctx), LogLevelError, node.Name, script.Path, err.Error())
		if recordErr := errors.Join(finishErr, logErr); recordErr != nil {
			return result, recordErr
		}
		if status == migration.ScriptStatusCancelled {
			return result, err
		}
		return result, &scriptFailure{cause: err}
	}

	status := migration.ScriptStatusSucceeded
	message := fmt.Sprintf("applied (%d rows affected)", result.RowsAffected)
	if result.Outcome == ApplyOutcomeAlreadyApplied {
		status = migration.ScriptStatusAlreadyApplied
		message = "already applied with matching checksum"
	}
	if err := executor.finishScript(ctx, node.Name, script.Path, status, result.RowsAffected, ""); err != nil {
		return result, err
	}
	if err := executor.log(ctx, LogLevelInfo, node.Name, script.Path, message); err != nil {
		return result, err
	}
	return result, nil
}

func (executor *attemptExecutor) haltNode(ctx context.Context, node migration.Node, remainingIndex int, cause error) (migration.NodeStatus, error) {
	if err := executor.finishScripts(ctx, node, remainingIndex, migration.ScriptStatusBlocked, "blocked by an earlier script failure"); err != nil {
		return migration.NodeStatusFailed, err
	}
	if err := executor.finishNode(ctx, node.Name, migration.NodeStatusFailed, cause.Error()); err != nil {
		return migration.NodeStatusFailed, err
	}
	return migration.NodeStatusFailed, nil
}

func (executor *attemptExecutor) failNode(ctx context.Context, node migration.Node, cause error) (migration.NodeStatus, error) {
	if err := executor.finishScripts(context.WithoutCancel(ctx), node, 0, migration.ScriptStatusBlocked, cause.Error()); err != nil {
		return migration.NodeStatusFailed, err
	}
	if err := executor.log(context.WithoutCancel(ctx), LogLevelError, node.Name, "", cause.Error()); err != nil {
		return migration.NodeStatusFailed, err
	}
	if err := executor.finishNode(context.WithoutCancel(ctx), node.Name, migration.NodeStatusFailed, cause.Error()); err != nil {
		return migration.NodeStatusFailed, err
	}
	return migration.NodeStatusFailed, nil
}

func (executor *attemptExecutor) cancelNode(ctx context.Context, node migration.Node, remainingIndex int, cause error) (migration.NodeStatus, error) {
	if err := executor.finishScripts(ctx, node, remainingIndex, migration.ScriptStatusCancelled, cause.Error()); err != nil {
		return migration.NodeStatusCancelled, err
	}
	if err := executor.finishNode(ctx, node.Name, migration.NodeStatusCancelled, cause.Error()); err != nil {
		return migration.NodeStatusCancelled, err
	}
	return migration.NodeStatusCancelled, cause
}

func (executor *attemptExecutor) finishUnstartedNode(ctx context.Context, node migration.Node, status migration.NodeStatus, message string) error {
	scriptStatus := migration.ScriptStatusBlocked
	if status == migration.NodeStatusCancelled {
		scriptStatus = migration.ScriptStatusCancelled
	}
	if err := executor.finishScripts(ctx, node, 0, scriptStatus, message); err != nil {
		return err
	}
	if err := executor.finishNode(ctx, node.Name, status, message); err != nil {
		return err
	}
	return executor.log(ctx, LogLevelWarn, node.Name, "", message)
}

func (executor *attemptExecutor) finishScripts(ctx context.Context, node migration.Node, start int, status migration.ScriptStatus, message string) error {
	for _, script := range node.Scripts[start:] {
		if err := executor.finishScript(ctx, node.Name, script.Path, status, 0, message); err != nil {
			return err
		}
	}
	return nil
}

func (executor *attemptExecutor) finishScript(
	ctx context.Context,
	node string,
	script string,
	status migration.ScriptStatus,
	rowsAffected int64,
	message string,
) error {
	return executor.apply(ctx, Transition{
		Kind:         TransitionScriptFinished,
		RunID:        executor.request.runID,
		Attempt:      executor.request.attempt,
		Node:         node,
		Script:       script,
		ScriptStatus: status,
		At:           time.Now().UTC(),
		RowsAffected: rowsAffected,
		Error:        message,
	})
}

func (executor *attemptExecutor) finishNode(ctx context.Context, node string, status migration.NodeStatus, message string) error {
	return executor.apply(ctx, Transition{
		Kind:       TransitionNodeFinished,
		RunID:      executor.request.runID,
		Attempt:    executor.request.attempt,
		Node:       node,
		NodeStatus: status,
		At:         time.Now().UTC(),
		Error:      message,
	})
}

func (executor *attemptExecutor) cancelPending(
	ctx context.Context,
	pending map[string]migration.Node,
	statuses map[string]migration.NodeStatus,
) error {
	for _, node := range executor.request.project.Graph.Nodes {
		if _, exists := pending[node.Name]; !exists {
			continue
		}
		if err := executor.finishUnstartedNode(ctx, node, migration.NodeStatusCancelled, "run cancelled"); err != nil {
			return err
		}
		statuses[node.Name] = migration.NodeStatusCancelled
		delete(pending, node.Name)
	}
	return nil
}

func (executor *attemptExecutor) finish(ctx context.Context, status migration.RunStatus, cause error) error {
	finishContext := context.WithoutCancel(ctx)
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	now := time.Now().UTC()
	if err := executor.apply(finishContext, Transition{
		Kind:      TransitionAttemptFinished,
		RunID:     executor.request.runID,
		Attempt:   executor.request.attempt,
		RunStatus: status,
		At:        now,
		Error:     message,
	}); err != nil {
		return errors.Join(cause, err)
	}
	if err := executor.apply(finishContext, Transition{
		Kind:      TransitionRunFinished,
		RunID:     executor.request.runID,
		Attempt:   executor.request.attempt,
		RunStatus: status,
		At:        now,
		Error:     message,
	}); err != nil {
		return errors.Join(cause, err)
	}

	level := LogLevelInfo
	if status == migration.RunStatusFailed || status == migration.RunStatusCancelled {
		level = LogLevelError
	} else if status == migration.RunStatusCompletedWithErrors {
		level = LogLevelWarn
	}
	if err := executor.log(finishContext, level, "", "", fmt.Sprintf("run finished with status %s", status)); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (executor *attemptExecutor) apply(ctx context.Context, transition Transition) error {
	if err := executor.engine.store.Apply(ctx, transition); err != nil {
		return fmt.Errorf("record %s transition: %w", transition.Kind, err)
	}
	return nil
}

func (executor *attemptExecutor) log(ctx context.Context, level LogLevel, node string, script string, message string) error {
	entry := LogEntry{
		At:      time.Now().UTC(),
		Level:   level,
		Node:    node,
		Script:  script,
		Message: message,
	}
	if err := executor.apply(ctx, Transition{
		Kind:    TransitionLogAppended,
		RunID:   executor.request.runID,
		Attempt: executor.request.attempt,
		At:      entry.At,
		Log:     entry,
	}); err != nil {
		return err
	}
	if executor.request.notify == nil {
		return nil
	}

	executor.notifyMu.Lock()
	defer executor.notifyMu.Unlock()
	executor.request.notify(Event{RunID: executor.request.runID, Attempt: executor.request.attempt, Log: entry})
	return nil
}

type dependenciesState int

const (
	dependenciesWaiting dependenciesState = iota
	dependenciesReady
	dependenciesBlocked
)

func dependencyState(node migration.Node, statuses map[string]migration.NodeStatus) dependenciesState {
	for _, dependency := range node.DependsOn {
		status := statuses[dependency]
		if !status.IsTerminal() {
			return dependenciesWaiting
		}
		if !status.AllowsDownstream() {
			return dependenciesBlocked
		}
	}
	return dependenciesReady
}

func previousNodeStatuses(graph migration.Graph, snapshot RunSnapshot) map[string]migration.NodeStatus {
	statuses := make(map[string]migration.NodeStatus, len(graph.Nodes))
	for _, node := range graph.Nodes {
		statuses[node.Name] = migration.NodeStatusPending
	}
	for _, node := range snapshot.Nodes {
		if node.Status.AllowsDownstream() {
			statuses[node.Name] = node.Status
		}
	}
	return statuses
}

func pendingNodes(graph migration.Graph, statuses map[string]migration.NodeStatus) map[string]migration.Node {
	pending := make(map[string]migration.Node)
	for _, node := range graph.Nodes {
		if statuses[node.Name].AllowsDownstream() {
			continue
		}
		pending[node.Name] = node
	}
	return pending
}

func runStatus(statuses map[string]migration.NodeStatus) migration.RunStatus {
	hasWarnings := false
	for _, status := range statuses {
		switch status {
		case migration.NodeStatusFailed, migration.NodeStatusBlocked, migration.NodeStatusCancelled:
			return migration.RunStatusFailed
		case migration.NodeStatusCompletedWithErrors:
			hasWarnings = true
		}
	}
	if hasWarnings {
		return migration.RunStatusCompletedWithErrors
	}
	return migration.RunStatusSucceeded
}

func hasNodeStatus(statuses map[string]migration.NodeStatus, expected migration.NodeStatus) bool {
	for _, status := range statuses {
		if status == expected {
			return true
		}
	}
	return false
}
