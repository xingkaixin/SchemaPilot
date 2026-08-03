package runstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

func (store *Store) Apply(ctx context.Context, transition execution.Transition) error {
	if err := validateTransition(transition); err != nil {
		return err
	}

	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	var lastErr error
	for attempt := 0; attempt < transitionRetryLimit; attempt++ {
		lastErr = store.applyTransition(ctx, transition)
		if !isBusyError(lastErr) {
			return lastErr
		}
		if err := waitForTransitionRetry(ctx, attempt); err != nil {
			return err
		}
	}

	return lastErr
}

const transitionRetryLimit = 8

func (store *Store) applyTransition(ctx context.Context, transition execution.Transition) error {

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transition transaction: %w", err)
	}
	defer tx.Rollback()

	run, err := readStoredRun(ctx, tx, transition.RunID)
	if err != nil {
		return err
	}

	switch transition.Kind {
	case execution.TransitionAttemptStarted:
		err = applyAttemptStarted(ctx, tx, run, transition)
	case execution.TransitionNodeStarted:
		err = applyNodeStarted(ctx, tx, transition)
	case execution.TransitionScriptStarted:
		err = applyScriptStarted(ctx, tx, transition)
	case execution.TransitionScriptFinished:
		err = applyScriptFinished(ctx, tx, transition)
	case execution.TransitionNodeFinished:
		err = applyNodeFinished(ctx, tx, transition)
	case execution.TransitionAttemptFinished:
		err = applyAttemptFinished(ctx, tx, transition)
	case execution.TransitionRunFinished:
		err = applyRunFinished(ctx, tx, transition)
	case execution.TransitionLogAppended:
		err = applyLogAppended(ctx, tx, transition)
	}
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transition %q: %w", transition.Kind, err)
	}

	return nil
}

func waitForTransitionRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(5*(1<<attempt)) * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}

func applyAttemptStarted(ctx context.Context, tx *sql.Tx, run storedRun, transition execution.Transition) error {
	if transition.Attempt != run.run.Attempt+1 {
		return fmt.Errorf("attempt %d must follow current run attempt %d", transition.Attempt, run.run.Attempt)
	}

	var existing int
	err := tx.QueryRowContext(ctx, `
        SELECT COUNT(*)
          FROM run_attempts
         WHERE run_id = ? AND attempt = ?`,
		string(transition.RunID), transition.Attempt).Scan(&existing)
	if err != nil {
		return fmt.Errorf("check run attempt %d: %w", transition.Attempt, err)
	}
	if existing != 0 {
		return fmt.Errorf("run attempt %d already started", transition.Attempt)
	}

	_, err = tx.ExecContext(ctx, `
        INSERT INTO run_attempts (run_id, attempt, graph_fingerprint, status, started_at, finished_at, error)
        VALUES (?, ?, ?, ?, ?, NULL, '')`,
		string(transition.RunID),
		transition.Attempt,
		transition.GraphFingerprint,
		string(transition.RunStatus),
		transition.At.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("insert run attempt %d: %w", transition.Attempt, err)
	}

	startedAt := run.run.StartedAt
	if startedAt.IsZero() {
		startedAt = transition.At
	}
	_, err = tx.ExecContext(ctx, `
        UPDATE migration_runs
           SET status = ?, attempt = ?, graph_fingerprint = ?, started_at = ?, finished_at = NULL, error = ''
         WHERE id = ?`,
		string(transition.RunStatus),
		transition.Attempt,
		transition.GraphFingerprint,
		startedAt.UnixNano(),
		string(transition.RunID),
	)
	if err != nil {
		return fmt.Errorf("update run for attempt %d: %w", transition.Attempt, err)
	}

	return nil
}

func applyNodeStarted(ctx context.Context, tx *sql.Tx, transition execution.Transition) error {
	if err := requireRunningAttempt(ctx, tx, transition.RunID, transition.Attempt); err != nil {
		return err
	}
	if _, err := readNodeMetadata(ctx, tx, transition.RunID, transition.Node); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
        INSERT INTO node_executions (run_id, attempt, node_name, status, started_at, finished_at, error)
        VALUES (?, ?, ?, ?, ?, NULL, '')`,
		string(transition.RunID),
		transition.Attempt,
		transition.Node,
		string(transition.NodeStatus),
		transition.At.UnixNano(),
	)
	if err != nil {
		if isConstraintError(err) {
			return fmt.Errorf("node %q already started in attempt %d", transition.Node, transition.Attempt)
		}
		return fmt.Errorf("insert node %q execution: %w", transition.Node, err)
	}

	return nil
}

func applyNodeFinished(ctx context.Context, tx *sql.Tx, transition execution.Transition) error {
	if err := requireAttempt(ctx, tx, transition.RunID, transition.Attempt); err != nil {
		return err
	}
	if _, err := readNodeMetadata(ctx, tx, transition.RunID, transition.Node); err != nil {
		return err
	}

	var currentStatus string
	err := tx.QueryRowContext(ctx, `
        SELECT status
          FROM node_executions
         WHERE run_id = ? AND attempt = ? AND node_name = ?`,
		string(transition.RunID), transition.Attempt, transition.Node).Scan(&currentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		if !isBlockedNodeStatus(transition.NodeStatus) {
			return fmt.Errorf("node %q must be started before finishing", transition.Node)
		}

		_, err = tx.ExecContext(ctx, `
            INSERT INTO node_executions (run_id, attempt, node_name, status, started_at, finished_at, error)
            VALUES (?, ?, ?, ?, NULL, ?, ?)`,
			string(transition.RunID),
			transition.Attempt,
			transition.Node,
			string(transition.NodeStatus),
			transition.At.UnixNano(),
			transition.Error,
		)
		if err != nil {
			return fmt.Errorf("insert blocked node %q execution: %w", transition.Node, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read node %q execution: %w", transition.Node, err)
	}
	if isTerminalNodeStatus(migration.NodeStatus(currentStatus)) {
		return fmt.Errorf("node %q already finished in attempt %d", transition.Node, transition.Attempt)
	}
	if currentStatus != string(migration.NodeStatusRunning) && !isBlockedNodeStatus(transition.NodeStatus) {
		return fmt.Errorf("node %q has status %q and cannot finish as %q", transition.Node, currentStatus, transition.NodeStatus)
	}

	_, err = tx.ExecContext(ctx, `
        UPDATE node_executions
           SET status = ?, finished_at = ?, error = ?
         WHERE run_id = ? AND attempt = ? AND node_name = ?`,
		string(transition.NodeStatus),
		transition.At.UnixNano(),
		transition.Error,
		string(transition.RunID),
		transition.Attempt,
		transition.Node,
	)
	if err != nil {
		return fmt.Errorf("finish node %q: %w", transition.Node, err)
	}

	return nil
}

func applyScriptStarted(ctx context.Context, tx *sql.Tx, transition execution.Transition) error {
	if err := requireRunningAttempt(ctx, tx, transition.RunID, transition.Attempt); err != nil {
		return err
	}
	if _, err := readNodeMetadata(ctx, tx, transition.RunID, transition.Node); err != nil {
		return err
	}
	script, err := readScriptMetadata(ctx, tx, transition.RunID, transition.Node, transition.Script)
	if err != nil {
		return err
	}
	if err := requireRunningNode(ctx, tx, transition.RunID, transition.Attempt, transition.Node); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
        INSERT INTO script_executions (
            run_id, attempt, node_name, script_order, path, checksum,
            status, started_at, finished_at, rows_affected, error
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, 0, '')`,
		string(transition.RunID),
		transition.Attempt,
		transition.Node,
		script.order,
		script.path,
		transition.Checksum,
		string(transition.ScriptStatus),
		transition.At.UnixNano(),
	)
	if err != nil {
		if isConstraintError(err) {
			return fmt.Errorf("script %q already started in attempt %d", transition.Script, transition.Attempt)
		}
		return fmt.Errorf("insert script %q execution: %w", transition.Script, err)
	}

	return nil
}

func applyScriptFinished(ctx context.Context, tx *sql.Tx, transition execution.Transition) error {
	if err := requireAttempt(ctx, tx, transition.RunID, transition.Attempt); err != nil {
		return err
	}
	if _, err := readNodeMetadata(ctx, tx, transition.RunID, transition.Node); err != nil {
		return err
	}
	script, err := readScriptMetadata(ctx, tx, transition.RunID, transition.Node, transition.Script)
	if err != nil {
		return err
	}

	var currentStatus string
	err = tx.QueryRowContext(ctx, `
        SELECT status
          FROM script_executions
         WHERE run_id = ? AND attempt = ? AND node_name = ? AND path = ?`,
		string(transition.RunID), transition.Attempt, transition.Node, script.path).Scan(&currentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		if !isBlockedScriptStatus(transition.ScriptStatus) {
			return fmt.Errorf("script %q must be started before finishing", transition.Script)
		}
		if err := ensureBlockedNode(ctx, tx, transition); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
            INSERT INTO script_executions (
                run_id, attempt, node_name, script_order, path, checksum,
                status, started_at, finished_at, rows_affected, error
            ) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, 0, ?)`,
			string(transition.RunID),
			transition.Attempt,
			transition.Node,
			script.order,
			script.path,
			script.checksum,
			string(transition.ScriptStatus),
			transition.At.UnixNano(),
			transition.Error,
		)
		if err != nil {
			return fmt.Errorf("insert blocked script %q execution: %w", transition.Script, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read script %q execution: %w", transition.Script, err)
	}
	if isTerminalScriptStatus(migration.ScriptStatus(currentStatus)) {
		return fmt.Errorf("script %q already finished in attempt %d", transition.Script, transition.Attempt)
	}
	if currentStatus != string(migration.ScriptStatusRunning) && !isBlockedScriptStatus(transition.ScriptStatus) {
		return fmt.Errorf("script %q has status %q and cannot finish as %q", transition.Script, currentStatus, transition.ScriptStatus)
	}

	_, err = tx.ExecContext(ctx, `
        UPDATE script_executions
           SET status = ?, finished_at = ?, rows_affected = ?, error = ?
         WHERE run_id = ? AND attempt = ? AND node_name = ? AND path = ?`,
		string(transition.ScriptStatus),
		transition.At.UnixNano(),
		transition.RowsAffected,
		transition.Error,
		string(transition.RunID),
		transition.Attempt,
		transition.Node,
		script.path,
	)
	if err != nil {
		return fmt.Errorf("finish script %q: %w", transition.Script, err)
	}

	return nil
}

func applyAttemptFinished(ctx context.Context, tx *sql.Tx, transition execution.Transition) error {
	if err := finishableAttempt(ctx, tx, transition.RunID, transition.Attempt); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
        UPDATE run_attempts
           SET status = ?, finished_at = ?, error = ?
         WHERE run_id = ? AND attempt = ?`,
		string(transition.RunStatus),
		transition.At.UnixNano(),
		transition.Error,
		string(transition.RunID),
		transition.Attempt,
	)
	if err != nil {
		return fmt.Errorf("finish run attempt %d: %w", transition.Attempt, err)
	}

	return nil
}

func applyRunFinished(ctx context.Context, tx *sql.Tx, transition execution.Transition) error {
	if err := requireAttempt(ctx, tx, transition.RunID, transition.Attempt); err != nil {
		return err
	}

	var currentStatus string
	err := tx.QueryRowContext(ctx, `SELECT status FROM migration_runs WHERE id = ?`, string(transition.RunID)).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("read run status: %w", err)
	}
	if isTerminalRunStatus(migration.RunStatus(currentStatus)) {
		return fmt.Errorf("run %q already finished", transition.RunID)
	}

	_, err = tx.ExecContext(ctx, `
        UPDATE migration_runs
           SET status = ?, finished_at = ?, error = ?, attempt = ?
         WHERE id = ?`,
		string(transition.RunStatus),
		transition.At.UnixNano(),
		transition.Error,
		transition.Attempt,
		string(transition.RunID),
	)
	if err != nil {
		return fmt.Errorf("finish migration run %q: %w", transition.RunID, err)
	}

	return nil
}

func applyLogAppended(ctx context.Context, tx *sql.Tx, transition execution.Transition) error {
	if err := requireAttempt(ctx, tx, transition.RunID, transition.Attempt); err != nil {
		return err
	}
	if transition.Log.Node != "" {
		if _, err := readNodeMetadata(ctx, tx, transition.RunID, transition.Log.Node); err != nil {
			return err
		}
	}
	if transition.Log.Script != "" && transition.Log.Node == "" {
		return errors.New("log script requires a node")
	}
	if transition.Log.Script != "" {
		if _, err := readScriptMetadata(ctx, tx, transition.RunID, transition.Log.Node, transition.Log.Script); err != nil {
			return err
		}
	}
	at := transitionTime(transition)

	sequence := transition.Log.Sequence
	if sequence > 0 {
		_, err := tx.ExecContext(ctx, `
            INSERT INTO run_logs (run_id, attempt, sequence, at, level, node_name, script_path, message)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(transition.RunID),
			transition.Attempt,
			sequence,
			at.UnixNano(),
			string(transition.Log.Level),
			transition.Log.Node,
			transition.Log.Script,
			transition.Log.Message,
		)
		if err != nil {
			return fmt.Errorf("append log entry: %w", err)
		}
		return nil
	}

	result, err := tx.ExecContext(ctx, `
        INSERT INTO run_logs (run_id, attempt, sequence, at, level, node_name, script_path, message)
        VALUES (?, ?, 0, ?, ?, ?, ?, ?)`,
		string(transition.RunID),
		transition.Attempt,
		at.UnixNano(),
		string(transition.Log.Level),
		transition.Log.Node,
		transition.Log.Script,
		transition.Log.Message,
	)
	if err != nil {
		return fmt.Errorf("append log entry: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read log sequence: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
        UPDATE run_logs
           SET sequence = (
               SELECT COALESCE(MAX(sequence), 0) + 1
                 FROM run_logs
                WHERE run_id = ? AND id <> ?
           )
         WHERE id = ?`,
		string(transition.RunID), id, id)
	if err != nil {
		return fmt.Errorf("assign log sequence: %w", err)
	}

	return nil
}

type nodeMetadata struct {
	name string
}

type scriptMetadata struct {
	order    int
	path     string
	checksum string
}

func readNodeMetadata(ctx context.Context, queryer sqlQueryer, runID execution.RunID, nodeName string) (nodeMetadata, error) {
	var result nodeMetadata
	err := queryer.QueryRowContext(ctx, `
        SELECT node_name
          FROM migration_nodes
         WHERE run_id = ? AND node_name = ?`, string(runID), nodeName).Scan(&result.name)
	if errors.Is(err, sql.ErrNoRows) {
		return nodeMetadata{}, fmt.Errorf("node %q does not belong to run %q", nodeName, runID)
	}
	if err != nil {
		return nodeMetadata{}, fmt.Errorf("read node %q metadata: %w", nodeName, err)
	}

	return result, nil
}

func readScriptMetadata(ctx context.Context, queryer sqlQueryer, runID execution.RunID, nodeName, path string) (scriptMetadata, error) {
	var result scriptMetadata
	err := queryer.QueryRowContext(ctx, `
        SELECT script_order, path, checksum
          FROM migration_scripts
         WHERE run_id = ? AND node_name = ? AND path = ?`, string(runID), nodeName, path).Scan(
		&result.order,
		&result.path,
		&result.checksum,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return scriptMetadata{}, fmt.Errorf("script %q does not belong to node %q", path, nodeName)
	}
	if err != nil {
		return scriptMetadata{}, fmt.Errorf("read script %q metadata: %w", path, err)
	}

	return result, nil
}

func requireAttempt(ctx context.Context, queryer sqlQueryer, runID execution.RunID, attempt int) error {
	var status string
	err := queryer.QueryRowContext(ctx, `
        SELECT status
          FROM run_attempts
         WHERE run_id = ? AND attempt = ?`, string(runID), attempt).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("run attempt %d does not exist", attempt)
	}
	if err != nil {
		return fmt.Errorf("read run attempt %d: %w", attempt, err)
	}

	return nil
}

func requireRunningAttempt(ctx context.Context, queryer sqlQueryer, runID execution.RunID, attempt int) error {
	var status string
	err := queryer.QueryRowContext(ctx, `
        SELECT status
          FROM run_attempts
         WHERE run_id = ? AND attempt = ?`, string(runID), attempt).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("run attempt %d does not exist", attempt)
	}
	if err != nil {
		return fmt.Errorf("read run attempt %d: %w", attempt, err)
	}
	if status != string(migration.RunStatusRunning) {
		return fmt.Errorf("run attempt %d has status %q", attempt, status)
	}

	return nil
}

func finishableAttempt(ctx context.Context, queryer sqlQueryer, runID execution.RunID, attempt int) error {
	var status string
	err := queryer.QueryRowContext(ctx, `
        SELECT status
          FROM run_attempts
         WHERE run_id = ? AND attempt = ?`, string(runID), attempt).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("run attempt %d does not exist", attempt)
	}
	if err != nil {
		return fmt.Errorf("read run attempt %d: %w", attempt, err)
	}
	if status != string(migration.RunStatusRunning) {
		return fmt.Errorf("run attempt %d has status %q and cannot finish", attempt, status)
	}

	return nil
}

func requireRunningNode(ctx context.Context, queryer sqlQueryer, runID execution.RunID, attempt int, nodeName string) error {
	var status string
	err := queryer.QueryRowContext(ctx, `
        SELECT status
          FROM node_executions
         WHERE run_id = ? AND attempt = ? AND node_name = ?`, string(runID), attempt, nodeName).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("node %q does not exist in attempt %d", nodeName, attempt)
	}
	if err != nil {
		return fmt.Errorf("read node %q execution: %w", nodeName, err)
	}
	if status != string(migration.NodeStatusRunning) {
		return fmt.Errorf("node %q has status %q", nodeName, status)
	}

	return nil
}

func ensureBlockedNode(ctx context.Context, tx *sql.Tx, transition execution.Transition) error {
	err := tx.QueryRowContext(ctx, `
        SELECT status
         FROM node_executions
         WHERE run_id = ? AND attempt = ? AND node_name = ?`,
		string(transition.RunID), transition.Attempt, transition.Node).Scan(new(string))
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `
            INSERT INTO node_executions (run_id, attempt, node_name, status, started_at, finished_at, error)
            VALUES (?, ?, ?, ?, NULL, NULL, '')`,
			string(transition.RunID),
			transition.Attempt,
			transition.Node,
			string(migration.NodeStatusPending),
		)
		if err != nil {
			return fmt.Errorf("insert blocked node %q execution: %w", transition.Node, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read node %q execution: %w", transition.Node, err)
	}
	return nil
}

func isBlockedNodeStatus(status migration.NodeStatus) bool {
	return status == migration.NodeStatusBlocked || status == migration.NodeStatusCancelled
}

func isBlockedScriptStatus(status migration.ScriptStatus) bool {
	return status == migration.ScriptStatusBlocked || status == migration.ScriptStatusCancelled
}

func isConstraintError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "constraint") || strings.Contains(strings.ToLower(err.Error()), "unique")
}
