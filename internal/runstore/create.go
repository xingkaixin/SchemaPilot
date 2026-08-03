package runstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/schemapilot/schemapilot/internal/execution"
)

func (store *Store) Create(ctx context.Context, definition execution.RunDefinition) error {
	if err := validateRunDefinition(definition); err != nil {
		return err
	}

	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create run transaction: %w", err)
	}
	defer tx.Rollback()

	graphName := definition.Run.GraphName
	if strings.TrimSpace(graphName) == "" {
		graphName = definition.Graph.Name
	}
	createdAt := definition.Created
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	_, err = tx.ExecContext(ctx, `
        INSERT INTO migration_runs (
            id, graph_name, graph_fingerprint, structure_fingerprint, environment_fingerprint, graph_path, databases_path,
            status, attempt, started_at, finished_at, error, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(definition.Run.ID),
		graphName,
		definition.Run.GraphFingerprint,
		definition.Run.StructureFingerprint,
		definition.Run.EnvironmentFingerprint,
		definition.Run.GraphPath,
		definition.Run.DatabasesPath,
		string(definition.Run.Status),
		definition.Run.Attempt,
		timeValue(definition.Run.StartedAt),
		timeValuePointer(definition.Run.FinishedAt),
		definition.Run.Error,
		createdAt.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("insert migration run %q: %w", definition.Run.ID, err)
	}

	if err := insertGraphMetadata(ctx, tx, definition); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration run %q: %w", definition.Run.ID, err)
	}

	return nil
}

func insertGraphMetadata(ctx context.Context, tx *sql.Tx, definition execution.RunDefinition) error {
	for nodeOrder, node := range definition.Graph.Nodes {
		dependencies, err := json.Marshal(node.DependsOn)
		if err != nil {
			return fmt.Errorf("encode dependencies for node %q: %w", node.Name, err)
		}

		_, err = tx.ExecContext(ctx, `
            INSERT INTO migration_nodes (run_id, node_name, node_order, database_name, depends_on)
            VALUES (?, ?, ?, ?, ?)`,
			string(definition.Run.ID),
			node.Name,
			nodeOrder,
			node.Database,
			string(dependencies),
		)
		if err != nil {
			return fmt.Errorf("insert migration node %q: %w", node.Name, err)
		}

		for scriptOrder, script := range node.Scripts {
			_, err = tx.ExecContext(ctx, `
                INSERT INTO migration_scripts (run_id, node_name, script_order, path, checksum)
                VALUES (?, ?, ?, ?, ?)`,
				string(definition.Run.ID),
				node.Name,
				scriptOrder,
				script.Path,
				script.Checksum,
			)
			if err != nil {
				return fmt.Errorf("insert migration script %q: %w", script.Path, err)
			}
		}
	}

	return nil
}

func timeValuePointer(value *time.Time) any {
	if value == nil {
		return nil
	}

	return timeValue(*value)
}
