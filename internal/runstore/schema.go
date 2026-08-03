package runstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS migration_runs (
    id TEXT PRIMARY KEY,
    graph_name TEXT NOT NULL,
    graph_fingerprint TEXT NOT NULL DEFAULT '',
    structure_fingerprint TEXT NOT NULL DEFAULT '',
    environment_fingerprint TEXT NOT NULL DEFAULT '',
    graph_path TEXT NOT NULL DEFAULT '',
    databases_path TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 0,
    started_at INTEGER,
    finished_at INTEGER,
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS migration_nodes (
    run_id TEXT NOT NULL,
    node_name TEXT NOT NULL,
    node_order INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    depends_on TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (run_id, node_name),
    UNIQUE (run_id, node_order),
    FOREIGN KEY (run_id) REFERENCES migration_runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS migration_scripts (
    run_id TEXT NOT NULL,
    node_name TEXT NOT NULL,
    script_order INTEGER NOT NULL,
    path TEXT NOT NULL,
    checksum TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (run_id, node_name, path),
    UNIQUE (run_id, node_name, script_order),
    FOREIGN KEY (run_id, node_name) REFERENCES migration_nodes(run_id, node_name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS run_attempts (
    run_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    graph_fingerprint TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    started_at INTEGER NOT NULL,
    finished_at INTEGER,
    error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (run_id, attempt),
    FOREIGN KEY (run_id) REFERENCES migration_runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS node_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    node_name TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at INTEGER,
    finished_at INTEGER,
    error TEXT NOT NULL DEFAULT '',
    UNIQUE (run_id, attempt, node_name),
    FOREIGN KEY (run_id, attempt) REFERENCES run_attempts(run_id, attempt) ON DELETE CASCADE,
    FOREIGN KEY (run_id, node_name) REFERENCES migration_nodes(run_id, node_name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS script_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    node_name TEXT NOT NULL,
    script_order INTEGER NOT NULL,
    path TEXT NOT NULL,
    checksum TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    started_at INTEGER,
    finished_at INTEGER,
    rows_affected INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    UNIQUE (run_id, attempt, node_name, path),
    UNIQUE (run_id, attempt, node_name, script_order),
    FOREIGN KEY (run_id, attempt, node_name) REFERENCES node_executions(run_id, attempt, node_name) ON DELETE CASCADE,
    FOREIGN KEY (run_id, node_name, path) REFERENCES migration_scripts(run_id, node_name, path) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS run_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    sequence INTEGER NOT NULL,
    at INTEGER NOT NULL,
    level TEXT NOT NULL,
    node_name TEXT NOT NULL DEFAULT '',
    script_path TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL,
    UNIQUE (run_id, sequence),
    FOREIGN KEY (run_id, attempt) REFERENCES run_attempts(run_id, attempt) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_migration_runs_created_at ON migration_runs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_node_executions_latest ON node_executions(run_id, node_name, attempt DESC);
CREATE INDEX IF NOT EXISTS idx_script_executions_latest ON script_executions(run_id, node_name, path, attempt DESC);
CREATE INDEX IF NOT EXISTS idx_run_logs_run_sequence ON run_logs(run_id, sequence, id);
`

func (store *Store) initialize(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("enable sqlite WAL: %w", err)
	}
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMilliseconds)); err != nil {
		return fmt.Errorf("configure sqlite busy timeout: %w", err)
	}
	if _, err := store.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create run store schema: %w", err)
	}
	if err := store.ensureMigrationRunColumn(ctx, "structure_fingerprint", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := store.ensureMigrationRunColumn(ctx, "environment_fingerprint", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := store.ensureRunAttemptColumn(ctx, "graph_fingerprint", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	return nil
}

func (store *Store) ensureMigrationRunColumn(ctx context.Context, name, definition string) error {
	rows, err := store.db.QueryContext(ctx, "PRAGMA table_info(migration_runs)")
	if err != nil {
		return fmt.Errorf("inspect migration_runs columns: %w", err)
	}
	defer rows.Close()

	var (
		cid          int
		column       string
		columnType   string
		notNull      int
		defaultValue sql.NullString
		primaryKey   int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &column, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan migration_runs columns: %w", err)
		}
		if column == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate migration_runs columns: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close migration_runs columns: %w", err)
	}

	if _, err := store.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE migration_runs ADD COLUMN %s %s", name, definition)); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return fmt.Errorf("add migration_runs.%s: %w", name, err)
	}

	return nil
}

func (store *Store) ensureRunAttemptColumn(ctx context.Context, name, definition string) error {
	rows, err := store.db.QueryContext(ctx, "PRAGMA table_info(run_attempts)")
	if err != nil {
		return fmt.Errorf("inspect run_attempts columns: %w", err)
	}
	defer rows.Close()

	var (
		cid          int
		column       string
		columnType   string
		notNull      int
		defaultValue sql.NullString
		primaryKey   int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &column, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan run_attempts columns: %w", err)
		}
		if column == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate run_attempts columns: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close run_attempts columns: %w", err)
	}

	if _, err := store.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE run_attempts ADD COLUMN %s %s", name, definition)); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return fmt.Errorf("add run_attempts.%s: %w", name, err)
	}

	return nil
}

type sqlQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
