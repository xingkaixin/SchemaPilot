package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schemapilot/schemapilot/internal/config"
	"github.com/schemapilot/schemapilot/internal/sqlscript"
)

const connectTimeout = 10 * time.Second

type Info struct {
	ID          config.Driver `json:"id"`
	Label       string        `json:"label"`
	DefaultPort int           `json:"defaultPort"`
	// File drivers take a database file path instead of a server address.
	File bool `json:"file,omitempty"`
}

type driver interface {
	info() Info
	dialect() sqlscript.Dialect
	open(connection config.Connection, notices *noticeRouter) (*sql.DB, error)
	sessionID(ctx context.Context, conn *sql.Conn) (int64, error)
	cancel(ctx context.Context, db *sql.DB, session int64) error
	version(ctx context.Context, db *sql.DB) (string, error)
	describe(err error) (ErrorInfo, bool)
}

var drivers = []driver{
	postgresDriver{},
	mysqlDriver{},
	sqlServerDriver{},
	oracleDriver{},
	sqliteDriver{sessions: &atomic.Int64{}},
}

func Drivers() []Info {
	infos := make([]Info, 0, len(drivers))
	for _, driver := range drivers {
		infos = append(infos, driver.info())
	}
	return infos
}

func lookup(id config.Driver) (driver, error) {
	for _, driver := range drivers {
		if driver.info().ID == id {
			return driver, nil
		}
	}
	return nil, fmt.Errorf("不支持的数据库类型 %q", id)
}

func Dialect(id config.Driver) sqlscript.Dialect {
	if driver, err := lookup(id); err == nil {
		return driver.dialect()
	}
	return sqlscript.Postgres
}

type DB struct {
	db       *sql.DB
	driver   driver
	notices  *noticeRouter
	password string
}

func Open(ctx context.Context, connection config.Connection) (*DB, error) {
	driver, err := lookup(connection.Driver)
	if err != nil {
		return nil, err
	}
	connection = expand(connection)
	if connection.Port == 0 {
		connection.Port = driver.info().DefaultPort
	}
	notices := &noticeRouter{}
	db, err := driver.open(connection, notices)
	if err != nil {
		return nil, redact(err, connection.Password)
	}
	pingContext, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(pingContext); err != nil {
		db.Close()
		return nil, redact(err, connection.Password)
	}
	return &DB{db: db, driver: driver, notices: notices, password: connection.Password}, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

func (db *DB) Dialect() sqlscript.Dialect {
	return db.driver.dialect()
}

func (db *DB) Version(ctx context.Context) (string, error) {
	version, err := db.driver.version(ctx, db.db)
	if err != nil {
		return "", redact(err, db.password)
	}
	return db.driver.info().Label + " " + version, nil
}

// Cancel stops the statement currently running in another session. It
// goes through a separate connection because the session itself is busy.
func (db *DB) Cancel(ctx context.Context, session int64) error {
	return db.driver.cancel(ctx, db.db, session)
}

// Session pins one physical connection so that every statement of a lane
// shares temporary tables and session settings.
type Session struct {
	ID   int64
	conn *sql.Conn
	db   *DB
}

func (db *DB) Session(ctx context.Context) (*Session, error) {
	conn, err := db.db.Conn(ctx)
	if err != nil {
		return nil, redact(err, db.password)
	}
	id, err := db.driver.sessionID(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, redact(err, db.password)
	}
	return &Session{ID: id, conn: conn, db: db}, nil
}

func (session *Session) Exec(ctx context.Context, statement string) (int64, error) {
	result, err := session.conn.ExecContext(ctx, statement)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return -1, nil
	}
	return rows, nil
}

// OnNotice receives server notices raised by this session until the
// returned function is called.
func (session *Session) OnNotice(handler func(string)) func() {
	return session.db.notices.subscribe(session.ID, handler)
}

func (session *Session) Close() error {
	return session.conn.Close()
}

type ErrorInfo struct {
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Code     string `json:"code,omitempty"`
	Position int    `json:"-"`
}

func (db *DB) Describe(err error) ErrorInfo {
	if info, ok := db.driver.describe(err); ok {
		return info
	}
	return ErrorInfo{Message: redact(err, db.password).Error()}
}

type TestResult struct {
	Version   string `json:"version"`
	LatencyMs int64  `json:"latencyMs"`
}

func Test(ctx context.Context, connection config.Connection) (TestResult, error) {
	startedAt := time.Now()
	db, err := Open(ctx, connection)
	if err != nil {
		return TestResult{}, err
	}
	defer db.Close()
	latency := time.Since(startedAt)
	version, err := db.Version(ctx)
	if err != nil {
		return TestResult{}, err
	}
	return TestResult{Version: version, LatencyMs: latency.Milliseconds()}, nil
}

type noticeRouter struct {
	handlers sync.Map
}

func (router *noticeRouter) subscribe(session int64, handler func(string)) func() {
	router.handlers.Store(session, handler)
	return func() { router.handlers.Delete(session) }
}

func (router *noticeRouter) publish(session int64, message string) {
	if handler, ok := router.handlers.Load(session); ok {
		handler.(func(string))(message)
	}
}

var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expand resolves ${NAME} references so secrets can stay out of the config
// file. A bare $ is left alone because passwords may contain one.
func expand(connection config.Connection) config.Connection {
	resolve := func(value string) string {
		return envReference.ReplaceAllStringFunc(value, func(match string) string {
			return os.Getenv(match[2 : len(match)-1])
		})
	}
	connection.Host = resolve(connection.Host)
	connection.Database = resolve(connection.Database)
	connection.User = resolve(connection.User)
	connection.Password = resolve(connection.Password)
	return connection
}

func redact(err error, password string) error {
	if err == nil || password == "" || !strings.Contains(err.Error(), password) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), password, "***"))
}
