package runstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
	_ "modernc.org/sqlite"
)

const (
	sqliteDriverName        = "sqlite"
	busyTimeoutMilliseconds = 5000
	maxOpenConnections      = 16
)

var ErrRunNotFound = execution.ErrRunNotFound

var memoryDatabaseSequence uint64

type Store struct {
	db      *sql.DB
	writeMu sync.Mutex
}

type storedRun struct {
	run       execution.Run
	createdAt time.Time
}

func readStoredRun(ctx context.Context, queryer sqlQueryer, runID execution.RunID) (storedRun, error) {
	var (
		result                           storedRun
		id, graphName, fingerprint       string
		structureFingerprint             string
		environmentFingerprint           string
		graphPath, databasesPath         string
		status                           string
		attempt                          int
		startedAt, finishedAt, createdAt sql.NullInt64
		runError                         string
	)

	err := queryer.QueryRowContext(ctx, `
		SELECT id, graph_name, graph_fingerprint, structure_fingerprint, environment_fingerprint, graph_path, databases_path,
               status, attempt, started_at, finished_at, error, created_at
          FROM migration_runs
         WHERE id = ?`, string(runID)).Scan(
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
	)
	if errors.Is(err, sql.ErrNoRows) {
		return storedRun{}, fmt.Errorf("%w: %s", ErrRunNotFound, runID)
	}
	if err != nil {
		return storedRun{}, fmt.Errorf("read migration run %q: %w", runID, err)
	}

	result.run = execution.Run{
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
	}
	if createdAt.Valid {
		result.createdAt = time.Unix(0, createdAt.Int64).UTC()
	}

	return result, nil
}

func nullableTimeValue(value sql.NullInt64) time.Time {
	if !value.Valid {
		return time.Time{}
	}

	return time.Unix(0, value.Int64).UTC()
}

func nullableTimePointer(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}

	result := time.Unix(0, value.Int64).UTC()
	return &result
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite path is required")
	}

	dsn := sqliteDSN(path)
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open run store: %w", err)
	}

	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxOpenConnections)

	store := &Store{db: db}
	if err := store.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}

	return store.db.Close()
}

func sqliteDSN(path string) string {
	if path == ":memory:" {
		id := atomic.AddUint64(&memoryDatabaseSequence, 1)
		path = fmt.Sprintf("file:runstore-memory-%d?mode=memory&cache=shared", id)
	}

	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}

	return fmt.Sprintf(
		"%s%s_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)",
		path,
		separator,
		busyTimeoutMilliseconds,
	)
}
