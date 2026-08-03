package runstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

const defaultRunLimit = 50

func (store *Store) Runs(ctx context.Context, query execution.RunQuery) ([]execution.RunSummary, error) {
	limit := query.Limit
	if limit == 0 {
		limit = defaultRunLimit
	}
	if limit < 0 {
		return nil, fmt.Errorf("run limit cannot be negative")
	}
	for _, status := range query.Statuses {
		if !isValidRunStatus(status) {
			return nil, fmt.Errorf("invalid run status filter %q", status)
		}
	}

	where := ""
	args := make([]any, 0, len(query.Statuses)+1)
	if len(query.Statuses) > 0 {
		placeholders := make([]string, len(query.Statuses))
		for index, status := range query.Statuses {
			placeholders[index] = "?"
			args = append(args, string(status))
		}
		where = "WHERE status IN (" + strings.Join(placeholders, ", ") + ")"
	}
	args = append(args, limit)

	rows, err := store.db.QueryContext(ctx, fmt.Sprintf(`
        SELECT id, graph_name, graph_fingerprint, structure_fingerprint, environment_fingerprint, graph_path, databases_path,
               status, attempt, started_at, finished_at, error, created_at
          FROM migration_runs
          %s
         ORDER BY created_at DESC, id DESC
         LIMIT ?`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("list migration runs: %w", err)
	}
	defer rows.Close()

	runs := make([]execution.RunSummary, 0)
	for rows.Next() {
		var (
			id, graphName, fingerprint string
			structureFingerprint       string
			environmentFingerprint     string
			graphPath, databasesPath   string
			status                     string
			attempt                    int
			startedAt, finishedAt      sql.NullInt64
			runError                   string
			createdAt                  sql.NullInt64
		)
		if err := rows.Scan(
			&id,
			&graphName,
			&fingerprint,
			&structureFingerprint,
			&environmentFingerprint,
			&graphPath,
			&databasesPath,
			&status,
			&attempt,
			&startedAt,
			&finishedAt,
			&runError,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan migration run: %w", err)
		}

		summary := execution.RunSummary{
			Run: execution.Run{
				ID:                     execution.RunID(id),
				GraphName:              graphName,
				GraphFingerprint:       fingerprint,
				StructureFingerprint:   structureFingerprint,
				EnvironmentFingerprint: environmentFingerprint,
				GraphPath:              graphPath,
				DatabasesPath:          databasesPath,
				Status:                 migration.RunStatus(status),
				Attempt:                attempt,
				StartedAt:              nullableTimeValue(startedAt),
				FinishedAt:             nullableTimePointer(finishedAt),
				Error:                  runError,
			},
		}
		if err := readNodeCounts(ctx, store.db, execution.RunID(id), &summary); err != nil {
			return nil, err
		}
		runs = append(runs, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration runs: %w", err)
	}

	return runs, nil
}

func readNodeCounts(ctx context.Context, queryer sqlQueryer, runID execution.RunID, summary *execution.RunSummary) error {
	err := queryer.QueryRowContext(ctx, `
        SELECT COUNT(*)
          FROM migration_nodes
         WHERE run_id = ?`, string(runID)).Scan(&summary.TotalNodes)
	if err != nil {
		return fmt.Errorf("count nodes for run %q: %w", runID, err)
	}

	err = queryer.QueryRowContext(ctx, `
        SELECT COUNT(*)
          FROM (
              SELECT node_name,
                     status,
                     ROW_NUMBER() OVER (PARTITION BY node_name ORDER BY attempt DESC, id DESC) AS row_number
                FROM node_executions
               WHERE run_id = ?
          )
         WHERE row_number = 1
           AND status IN (?, ?)`,
		string(runID),
		string(migration.NodeStatusSucceeded),
		string(migration.NodeStatusCompletedWithErrors),
	).Scan(&summary.CompletedNodes)
	if err != nil {
		return fmt.Errorf("count completed nodes for run %q: %w", runID, err)
	}

	return nil
}
