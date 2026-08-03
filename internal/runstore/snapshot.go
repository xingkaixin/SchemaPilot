package runstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

func (store *Store) Snapshot(ctx context.Context, runID execution.RunID) (execution.RunSnapshot, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.RunSnapshot{}, fmt.Errorf("begin snapshot transaction: %w", err)
	}
	defer tx.Rollback()

	run, err := readStoredRun(ctx, tx, runID)
	if err != nil {
		return execution.RunSnapshot{}, err
	}

	attempts, err := readAttemptSnapshots(ctx, tx, runID)
	if err != nil {
		return execution.RunSnapshot{}, err
	}
	nodes, err := readNodeSnapshots(ctx, tx, runID)
	if err != nil {
		return execution.RunSnapshot{}, err
	}
	logs, err := readLogEntries(ctx, tx, runID)
	if err != nil {
		return execution.RunSnapshot{}, err
	}

	if err := tx.Commit(); err != nil {
		return execution.RunSnapshot{}, fmt.Errorf("commit snapshot transaction: %w", err)
	}

	return execution.RunSnapshot{
		Run:      run.run,
		Attempts: attempts,
		Nodes:    nodes,
		Logs:     logs,
	}, nil
}

func readAttemptSnapshots(ctx context.Context, queryer sqlQueryer, runID execution.RunID) ([]execution.AttemptSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, `
        SELECT attempt, graph_fingerprint, status, started_at, finished_at, error
          FROM run_attempts
         WHERE run_id = ?
         ORDER BY attempt`, string(runID))
	if err != nil {
		return nil, fmt.Errorf("read run attempts: %w", err)
	}
	defer rows.Close()

	attempts := make([]execution.AttemptSnapshot, 0)
	for rows.Next() {
		var (
			attempt               execution.AttemptSnapshot
			graphFingerprint      string
			status                string
			startedAt, finishedAt sql.NullInt64
		)
		if err := rows.Scan(&attempt.Number, &graphFingerprint, &status, &startedAt, &finishedAt, &attempt.Error); err != nil {
			return nil, fmt.Errorf("scan run attempt: %w", err)
		}
		attempt.GraphFingerprint = graphFingerprint
		attempt.Status = migration.RunStatus(status)
		attempt.StartedAt = nullableTimeValue(startedAt)
		attempt.FinishedAt = nullableTimePointer(finishedAt)
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run attempts: %w", err)
	}

	return attempts, nil
}

func readNodeSnapshots(ctx context.Context, queryer sqlQueryer, runID execution.RunID) ([]execution.NodeSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, `
        SELECT node_name, database_name, depends_on
          FROM migration_nodes
         WHERE run_id = ?
         ORDER BY node_order`, string(runID))
	if err != nil {
		return nil, fmt.Errorf("read migration nodes: %w", err)
	}
	nodeMetadataRows := make([]struct {
		name, databaseName, dependenciesJSON string
	}, 0)
	for rows.Next() {
		var (
			name, databaseName, dependenciesJSON string
		)
		if err := rows.Scan(&name, &databaseName, &dependenciesJSON); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan migration node: %w", err)
		}
		nodeMetadataRows = append(nodeMetadataRows, struct {
			name, databaseName, dependenciesJSON string
		}{name, databaseName, dependenciesJSON})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate migration nodes: %w", err)
	}
	rows.Close()

	nodes := make([]execution.NodeSnapshot, 0, len(nodeMetadataRows))
	for _, metadata := range nodeMetadataRows {
		var dependencies []string
		if err := json.Unmarshal([]byte(metadata.dependenciesJSON), &dependencies); err != nil {
			return nil, fmt.Errorf("decode dependencies for node %q: %w", metadata.name, err)
		}

		node := execution.NodeSnapshot{
			Name:      metadata.name,
			Database:  metadata.databaseName,
			DependsOn: dependencies,
			Status:    migration.NodeStatusPending,
		}
		if err := readLatestNodeExecution(ctx, queryer, runID, &node); err != nil {
			return nil, err
		}
		scripts, err := readScriptSnapshots(ctx, queryer, runID, metadata.name)
		if err != nil {
			return nil, err
		}
		node.Scripts = scripts
		nodes = append(nodes, node)
	}

	return nodes, nil
}

func readLatestNodeExecution(ctx context.Context, queryer sqlQueryer, runID execution.RunID, node *execution.NodeSnapshot) error {
	var (
		status                string
		startedAt, finishedAt sql.NullInt64
	)
	err := queryer.QueryRowContext(ctx, `
        SELECT status, attempt, started_at, finished_at, error
          FROM node_executions
         WHERE run_id = ? AND node_name = ?
         ORDER BY attempt DESC, id DESC
         LIMIT 1`, string(runID), node.Name).Scan(
		&status,
		&node.Attempt,
		&startedAt,
		&finishedAt,
		&node.Error,
	)
	if err == nil {
		node.Status = migration.NodeStatus(status)
		node.StartedAt = nullableTimePointer(startedAt)
		node.FinishedAt = nullableTimePointer(finishedAt)
		return nil
	}
	if errorsIsNoRows(err) {
		return nil
	}

	return fmt.Errorf("read latest node %q execution: %w", node.Name, err)
}

func readScriptSnapshots(ctx context.Context, queryer sqlQueryer, runID execution.RunID, nodeName string) ([]execution.ScriptSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, `
        SELECT path, checksum
          FROM migration_scripts
         WHERE run_id = ? AND node_name = ?
         ORDER BY script_order`, string(runID), nodeName)
	if err != nil {
		return nil, fmt.Errorf("read scripts for node %q: %w", nodeName, err)
	}
	scriptMetadataRows := make([]struct {
		path, checksum string
	}, 0)
	for rows.Next() {
		var path, checksum string
		if err := rows.Scan(&path, &checksum); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan script for node %q: %w", nodeName, err)
		}
		scriptMetadataRows = append(scriptMetadataRows, struct {
			path, checksum string
		}{path, checksum})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate scripts for node %q: %w", nodeName, err)
	}
	rows.Close()

	scripts := make([]execution.ScriptSnapshot, 0, len(scriptMetadataRows))
	for _, metadata := range scriptMetadataRows {
		script := execution.ScriptSnapshot{
			Path:     metadata.path,
			Checksum: metadata.checksum,
			Status:   migration.ScriptStatusPending,
		}
		if err := readLatestScriptExecution(ctx, queryer, runID, nodeName, &script); err != nil {
			return nil, err
		}
		scripts = append(scripts, script)
	}

	return scripts, nil
}

func readLatestScriptExecution(ctx context.Context, queryer sqlQueryer, runID execution.RunID, nodeName string, script *execution.ScriptSnapshot) error {
	var (
		status                string
		startedAt, finishedAt sql.NullInt64
	)
	err := queryer.QueryRowContext(ctx, `
        SELECT path, checksum, status, attempt, started_at, finished_at, rows_affected, error
          FROM script_executions
         WHERE run_id = ? AND node_name = ? AND path = ?
         ORDER BY attempt DESC, id DESC
         LIMIT 1`, string(runID), nodeName, script.Path).Scan(
		&script.Path,
		&script.Checksum,
		&status,
		&script.Attempt,
		&startedAt,
		&finishedAt,
		&script.RowsAffected,
		&script.Error,
	)
	if err == nil {
		script.Status = migration.ScriptStatus(status)
		script.StartedAt = nullableTimePointer(startedAt)
		script.FinishedAt = nullableTimePointer(finishedAt)
		return nil
	}
	if errorsIsNoRows(err) {
		return nil
	}

	return fmt.Errorf("read latest script %q execution: %w", script.Path, err)
}

func readLogEntries(ctx context.Context, queryer sqlQueryer, runID execution.RunID) ([]execution.LogEntry, error) {
	rows, err := queryer.QueryContext(ctx, `
        SELECT sequence, at, level, node_name, script_path, message
          FROM run_logs
         WHERE run_id = ?
         ORDER BY sequence, id`, string(runID))
	if err != nil {
		return nil, fmt.Errorf("read run logs: %w", err)
	}
	defer rows.Close()

	logs := make([]execution.LogEntry, 0)
	for rows.Next() {
		var (
			logEntry execution.LogEntry
			at       int64
			level    string
		)
		if err := rows.Scan(&logEntry.Sequence, &at, &level, &logEntry.Node, &logEntry.Script, &logEntry.Message); err != nil {
			return nil, fmt.Errorf("scan run log: %w", err)
		}
		logEntry.At = timeFromUnixNano(at)
		logEntry.Level = execution.LogLevel(level)
		logs = append(logs, logEntry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run logs: %w", err)
	}

	return logs, nil
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}
