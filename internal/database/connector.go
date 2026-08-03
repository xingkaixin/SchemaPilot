package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

const defaultConnectionTimeout = 30 * time.Second

type Connector struct {
	historyGates sync.Map
}

var _ execution.DatabaseConnector = (*Connector)(nil)
var _ execution.TargetDatabase = (*SQLTargetDatabase)(nil)

func NewConnector() *Connector {
	return &Connector{}
}

func NewSQLConnector() *Connector {
	return NewConnector()
}

func (connector *Connector) Connect(ctx context.Context, profile migration.DatabaseProfile) (execution.TargetDatabase, error) {
	driverName, err := sqlDriverName(profile.Driver)
	if err != nil {
		return nil, err
	}
	dialect, err := dialectFor(profile.Driver)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driverName, profile.DSN)
	if err != nil {
		return nil, redactDatabaseError("open", profile.Driver, profile.DSN, err)
	}
	configurePool(db, profile)

	pingContext, cancel := connectionContext(ctx, profile.ConnectionTimeout)
	defer cancel()
	if err := db.PingContext(pingContext); err != nil {
		_ = db.Close()
		return nil, redactDatabaseError("ping", profile.Driver, profile.DSN, err)
	}

	return &SQLTargetDatabase{db: db, dialect: dialect, history: connector.historyGate(profile)}, nil
}

func (connector *Connector) historyGate(profile migration.DatabaseProfile) *historyGate {
	key := sha256.Sum256([]byte(string(profile.Driver) + "\x00" + profile.DSN))
	gate, _ := connector.historyGates.LoadOrStore(key, &historyGate{})
	return gate.(*historyGate)
}

func sqlDriverName(driver migration.Driver) (string, error) {
	switch driver {
	case migration.DriverPostgres:
		return "pgx", nil
	case migration.DriverMySQL:
		return "mysql", nil
	case migration.DriverSQLServer:
		return "sqlserver", nil
	default:
		return "", fmt.Errorf("unsupported database driver %q", driver)
	}
}

func configurePool(db *sql.DB, profile migration.DatabaseProfile) {
	if profile.MaxOpenConnections <= 0 {
		return
	}
	db.SetMaxOpenConns(profile.MaxOpenConnections)
	db.SetMaxIdleConns(profile.MaxOpenConnections)
}

func connectionContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = defaultConnectionTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

type redactedDatabaseError struct {
	operation string
	driver    migration.Driver
	message   string
	canceled  bool
	deadline  bool
}

func (err *redactedDatabaseError) Error() string {
	return fmt.Sprintf("%s %s database: %s", err.operation, err.driver, err.message)
}

func (err *redactedDatabaseError) Is(target error) bool {
	if target == context.Canceled {
		return err.canceled
	}
	if target == context.DeadlineExceeded {
		return err.deadline
	}
	return false
}

func redactDatabaseError(operation string, driver migration.Driver, dsn string, cause error) error {
	return &redactedDatabaseError{
		operation: operation,
		driver:    driver,
		message:   redactMessage(cause.Error(), dsn),
		canceled:  errors.Is(cause, context.Canceled),
		deadline:  errors.Is(cause, context.DeadlineExceeded),
	}
}

func redactMessage(message, dsn string) string {
	if dsn == "" {
		return message
	}
	message = strings.ReplaceAll(message, dsn, "<redacted>")
	for _, secret := range dsnSecrets(dsn) {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "<redacted>")
		}
	}
	return message
}

func dsnSecrets(dsn string) []string {
	secrets := make([]string, 0, 4)
	if parsed, err := url.Parse(dsn); err == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			secrets = append(secrets, password, url.QueryEscape(password), url.PathEscape(password))
		}
	}

	if at := strings.IndexByte(dsn, '@'); at > 0 {
		credentials := dsn[:at]
		if separator := strings.IndexByte(credentials, ':'); separator >= 0 {
			secrets = append(secrets, credentials[separator+1:])
		}
	}

	for _, key := range []string{"password=", "pass="} {
		lowerDSN := strings.ToLower(dsn)
		start := strings.Index(lowerDSN, key)
		if start < 0 {
			continue
		}
		valueStart := start + len(key)
		valueEnd := valueStart
		for valueEnd < len(dsn) && !strings.ContainsRune("&;, \t\r\n", rune(dsn[valueEnd])) {
			valueEnd++
		}
		secrets = append(secrets, dsn[valueStart:valueEnd])
	}
	return uniqueStrings(secrets)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

type SQLTargetDatabase struct {
	db      *sql.DB
	dialect sqlDialect
	history *historyGate
}

type historyGate struct {
	mu    sync.Mutex
	ready bool
}

func NewTargetDatabase(db *sql.DB, driver migration.Driver) (*SQLTargetDatabase, error) {
	if db == nil {
		return nil, errors.New("database handle is nil")
	}
	dialect, err := dialectFor(driver)
	if err != nil {
		return nil, err
	}
	return &SQLTargetDatabase{db: db, dialect: dialect, history: &historyGate{}}, nil
}

func NewSQLTargetDatabase(db *sql.DB, driver migration.Driver) (*SQLTargetDatabase, error) {
	return NewTargetDatabase(db, driver)
}

func (target *SQLTargetDatabase) Ping(ctx context.Context) error {
	if target == nil || target.db == nil {
		return errors.New("database handle is nil")
	}
	return target.db.PingContext(ctx)
}

func (target *SQLTargetDatabase) Close() error {
	if target == nil || target.db == nil {
		return nil
	}
	return target.db.Close()
}

func (target *SQLTargetDatabase) ensureHistoryTable(ctx context.Context) error {
	target.history.mu.Lock()
	defer target.history.mu.Unlock()
	if target.history.ready {
		return nil
	}
	if _, err := target.db.ExecContext(ctx, target.dialect.historyDDL()); err != nil {
		return err
	}
	target.history.ready = true
	return nil
}

func (target *SQLTargetDatabase) Apply(ctx context.Context, application execution.ScriptApplication) (execution.ApplyResult, error) {
	if target == nil || target.db == nil {
		return execution.ApplyResult{}, errors.New("database handle is nil")
	}
	if err := validateApplication(application); err != nil {
		return execution.ApplyResult{}, err
	}
	if err := target.ensureHistoryTable(ctx); err != nil {
		return execution.ApplyResult{}, fmt.Errorf("ensure migration history: %w", err)
	}

	tx, err := target.db.BeginTx(ctx, nil)
	if err != nil {
		return execution.ApplyResult{}, fmt.Errorf("begin migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	recordedChecksum, found, err := target.lookupChecksum(ctx, tx, application)
	if err != nil {
		return execution.ApplyResult{}, fmt.Errorf("read migration history: %w", err)
	}
	if found && recordedChecksum == application.Checksum {
		if err := tx.Commit(); err != nil {
			return execution.ApplyResult{}, fmt.Errorf("commit already-applied migration: %w", err)
		}
		committed = true
		return execution.ApplyResult{Outcome: execution.ApplyOutcomeAlreadyApplied}, nil
	}
	if found && !application.Force {
		return execution.ApplyResult{}, &ChecksumMismatchError{
			Path:     application.Path,
			Recorded: recordedChecksum,
			Current:  application.Checksum,
		}
	}

	rowsAffected, err := executeScript(ctx, tx, application.SQL)
	if err != nil {
		return execution.ApplyResult{}, fmt.Errorf("execute migration script %q: %w", application.Path, err)
	}
	if err := target.recordApplied(ctx, tx, application, found); err != nil {
		return execution.ApplyResult{}, fmt.Errorf("record migration script %q: %w", application.Path, err)
	}
	if err := tx.Commit(); err != nil {
		return execution.ApplyResult{}, fmt.Errorf("commit migration script %q: %w", application.Path, err)
	}
	committed = true
	return execution.ApplyResult{Outcome: execution.ApplyOutcomeApplied, RowsAffected: rowsAffected}, nil
}

func validateApplication(application execution.ScriptApplication) error {
	if strings.TrimSpace(application.GraphName) == "" {
		return errors.New("migration graph name is required")
	}
	if strings.TrimSpace(application.NodeName) == "" {
		return errors.New("migration node name is required")
	}
	if strings.TrimSpace(application.Path) == "" {
		return errors.New("migration script path is required")
	}
	if strings.TrimSpace(application.Checksum) == "" {
		return fmt.Errorf("migration script %q checksum is required", application.Path)
	}
	return nil
}

func (target *SQLTargetDatabase) lookupChecksum(ctx context.Context, tx *sql.Tx, application execution.ScriptApplication) (string, bool, error) {
	key := recordKey(application.GraphName, application.NodeName, application.Path)
	row := tx.QueryRowContext(ctx, historySelectFor(target.dialect), key, application.GraphName, application.NodeName, application.Path)
	var checksum string
	if err := row.Scan(&checksum); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return checksum, true, nil
}

func executeScript(ctx context.Context, tx *sql.Tx, script string) (int64, error) {
	result, err := tx.ExecContext(ctx, script)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return rowsAffected, nil
}

func (target *SQLTargetDatabase) recordApplied(ctx context.Context, tx *sql.Tx, application execution.ScriptApplication, existing bool) error {
	appliedAt := time.Now().UTC()
	key := recordKey(application.GraphName, application.NodeName, application.Path)
	if existing {
		_, err := tx.ExecContext(ctx, target.dialect.historyUpdate(), application.Checksum, appliedAt, application.Force, key, application.GraphName, application.NodeName, application.Path)
		return err
	}
	_, err := tx.ExecContext(ctx, target.dialect.historyInsert(), key, application.GraphName, application.NodeName, application.Path, application.Checksum, appliedAt, application.Force)
	return err
}

func recordKey(graphName, nodeName, path string) string {
	digest := sha256.Sum256([]byte(graphName + "\x00" + nodeName + "\x00" + path))
	return hex.EncodeToString(digest[:])
}
