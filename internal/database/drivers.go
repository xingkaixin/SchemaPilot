package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	ogpq "gitcode.com/opengauss/openGauss-connector-go-pq"
	_ "gitee.com/XuguDB/go-xugu-driver"
	"gitee.com/chunanyong/dm"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	mssql "github.com/microsoft/go-mssqldb"
	goora "github.com/sijms/go-ora/v2"
	"github.com/sijms/go-ora/v2/network"
	_ "modernc.org/sqlite"

	"github.com/schemapilot/schemapilot/internal/config"
	"github.com/schemapilot/schemapilot/internal/sqlscript"
)

type postgresDriver struct{}

func (postgresDriver) info() Info {
	return Info{ID: config.Postgres, Label: "PostgreSQL", DefaultPort: 5432}
}

func (postgresDriver) dialect() sqlscript.Dialect { return sqlscript.Postgres }

func postgresURL(connection config.Connection) string {
	query := url.Values{}
	for key, value := range connection.Params {
		query.Set(key, value)
	}
	if query.Get("connect_timeout") == "" {
		query.Set("connect_timeout", strconv.Itoa(int(connectTimeout.Seconds())))
	}
	if query.Get("application_name") == "" {
		query.Set("application_name", "schemapilot")
	}
	target := url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port)),
		Path:     "/" + connection.Database,
		RawQuery: query.Encode(),
	}
	if connection.User != "" {
		target.User = url.UserPassword(connection.User, connection.Password)
	}
	return target.String()
}

func (postgresDriver) open(connection config.Connection, notices *noticeRouter) (*sql.DB, error) {
	parsed, err := pgx.ParseConfig(postgresURL(connection))
	if err != nil {
		return nil, err
	}
	parsed.OnNotice = func(conn *pgconn.PgConn, notice *pgconn.Notice) {
		notices.publish(int64(conn.PID()), notice.Severity+": "+notice.Message)
	}
	return stdlib.OpenDB(*parsed), nil
}

func (postgresDriver) sessionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var pid int64
	err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid)
	return pid, err
}

func (postgresDriver) cancel(ctx context.Context, db *sql.DB, session int64) error {
	_, err := db.ExecContext(ctx, "SELECT pg_cancel_backend($1)", session)
	return err
}

func (postgresDriver) version(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	if err := db.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		return "", err
	}
	if fields := strings.Fields(version); len(fields) > 0 {
		version = fields[0]
	}
	return version, nil
}

func (postgresDriver) describe(err error) (ErrorInfo, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return ErrorInfo{}, false
	}
	return ErrorInfo{
		Message:  pgErr.Severity + ": " + pgErr.Message,
		Detail:   pgErr.Detail,
		Hint:     pgErr.Hint,
		Code:     pgErr.Code,
		Position: int(pgErr.Position),
	}, true
}

type mysqlDriver struct{}

func (mysqlDriver) info() Info {
	return Info{ID: config.MySQL, Label: "MySQL", DefaultPort: 3306}
}

func (mysqlDriver) dialect() sqlscript.Dialect { return sqlscript.MySQL }

func (mysqlDriver) open(connection config.Connection, _ *noticeRouter) (*sql.DB, error) {
	base := mysql.NewConfig()
	base.User = connection.User
	base.Passwd = connection.Password
	base.Net = "tcp"
	base.Addr = net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port))
	base.DBName = connection.Database
	base.Timeout = connectTimeout
	dsn := base.FormatDSN()
	if len(connection.Params) > 0 {
		query := url.Values{}
		for key, value := range connection.Params {
			query.Set(key, value)
		}
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		dsn += separator + query.Encode()
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	// Scripts are split client-side, so multi-statement mode stays off.
	parsed.MultiStatements = false
	connector, err := mysql.NewConnector(parsed)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func (mysqlDriver) sessionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var id int64
	err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id)
	return id, err
}

func (mysqlDriver) cancel(ctx context.Context, db *sql.DB, session int64) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("KILL QUERY %d", session))
	return err
}

func (mysqlDriver) version(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
	return version, err
}

func (mysqlDriver) describe(err error) (ErrorInfo, bool) {
	var myErr *mysql.MySQLError
	if !errors.As(err, &myErr) {
		return ErrorInfo{}, false
	}
	info := ErrorInfo{Message: fmt.Sprintf("ERROR %d: %s", myErr.Number, myErr.Message)}
	if myErr.SQLState != [5]byte{} {
		info.Code = string(myErr.SQLState[:])
	}
	return info, true
}

// The drivers below abort a running statement themselves when its context
// is cancelled, so cancel has nothing to do on a separate connection.

type sqlServerDriver struct{}

func (sqlServerDriver) info() Info {
	return Info{ID: config.SQLServer, Label: "SQL Server", DefaultPort: 1433}
}

func (sqlServerDriver) dialect() sqlscript.Dialect { return sqlscript.SQLServer }

func (sqlServerDriver) open(connection config.Connection, _ *noticeRouter) (*sql.DB, error) {
	query := url.Values{}
	for key, value := range connection.Params {
		query.Set(key, value)
	}
	if query.Get("database") == "" && connection.Database != "" {
		query.Set("database", connection.Database)
	}
	if query.Get("app name") == "" {
		query.Set("app name", "schemapilot")
	}
	if query.Get("dial timeout") == "" {
		query.Set("dial timeout", strconv.Itoa(int(connectTimeout.Seconds())))
	}
	target := url.URL{
		Scheme:   "sqlserver",
		Host:     net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port)),
		RawQuery: query.Encode(),
	}
	if connection.User != "" {
		target.User = url.UserPassword(connection.User, connection.Password)
	}
	connector, err := mssql.NewConnector(target.String())
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func (sqlServerDriver) sessionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var id int64
	err := conn.QueryRowContext(ctx, "SELECT @@SPID").Scan(&id)
	return id, err
}

func (sqlServerDriver) cancel(context.Context, *sql.DB, int64) error { return nil }

func (sqlServerDriver) version(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	err := db.QueryRowContext(ctx, "SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128))").Scan(&version)
	return version, err
}

func (sqlServerDriver) describe(err error) (ErrorInfo, bool) {
	var msErr mssql.Error
	if !errors.As(err, &msErr) {
		return ErrorInfo{}, false
	}
	return ErrorInfo{
		Message:       fmt.Sprintf("Msg %d, Level %d, State %d: %s", msErr.Number, msErr.Class, msErr.State, msErr.Message),
		Code:          strconv.Itoa(int(msErr.Number)),
		StatementLine: int(msErr.LineNo),
	}, true
}

type oracleDriver struct{}

func (oracleDriver) info() Info {
	return Info{ID: config.Oracle, Label: "Oracle", DefaultPort: 1521}
}

func (oracleDriver) dialect() sqlscript.Dialect { return sqlscript.Oracle }

func (oracleDriver) open(connection config.Connection, _ *noticeRouter) (*sql.DB, error) {
	dsn := goora.BuildUrl(connection.Host, connection.Port, connection.Database, connection.User, connection.Password, connection.Params)
	return sql.Open("oracle", dsn)
}

func (oracleDriver) sessionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var id int64
	err := conn.QueryRowContext(ctx, "SELECT SYS_CONTEXT('USERENV', 'SID') FROM DUAL").Scan(&id)
	return id, err
}

func (oracleDriver) cancel(context.Context, *sql.DB, int64) error { return nil }

func (oracleDriver) version(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	err := db.QueryRowContext(ctx, "SELECT VERSION_FULL FROM PRODUCT_COMPONENT_VERSION WHERE PRODUCT LIKE 'Oracle%' AND ROWNUM = 1").Scan(&version)
	return version, err
}

func (oracleDriver) describe(err error) (ErrorInfo, bool) {
	var oraErr *network.OracleError
	if !errors.As(err, &oraErr) {
		return ErrorInfo{}, false
	}
	// Error() fills ErrMsg on first use and appends the position to it.
	_ = oraErr.Error()
	info := ErrorInfo{Message: strings.TrimSpace(oraErr.ErrMsg), Code: fmt.Sprintf("ORA-%05d", oraErr.ErrCode)}
	if position := oraErr.ErrPos(); position >= 0 {
		info.Position = position + 1
	}
	return info, true
}

// sqliteDriver opens a database file; relative paths are resolved against
// the working directory.
type sqliteDriver struct {
	sessions *atomic.Int64
}

func (sqliteDriver) info() Info {
	return Info{ID: config.SQLite, Label: "SQLite", File: true}
}

func (sqliteDriver) dialect() sqlscript.Dialect { return sqlscript.SQLite }

func (sqliteDriver) open(connection config.Connection, _ *noticeRouter) (*sql.DB, error) {
	query := url.Values{}
	for key, value := range connection.Params {
		query.Set(key, value)
	}
	if !query.Has("_pragma") {
		query.Add("_pragma", "foreign_keys(1)")
		query.Add("_pragma", "busy_timeout(10000)")
	}
	return sql.Open("sqlite", connection.Database+"?"+query.Encode())
}

// SQLite has no server sessions; a process-wide counter keeps the IDs of
// concurrent lanes apart.
func (driver sqliteDriver) sessionID(context.Context, *sql.Conn) (int64, error) {
	return driver.sessions.Add(1), nil
}

func (sqliteDriver) cancel(context.Context, *sql.DB, int64) error { return nil }

func (sqliteDriver) version(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version)
	return version, err
}

func (sqliteDriver) describe(error) (ErrorInfo, bool) { return ErrorInfo{}, false }

// openGaussDriver uses the openGauss fork of lib/pq, which speaks the
// SHA256 and SM3 password methods that pgx lacks.
type openGaussDriver struct{}

func (openGaussDriver) info() Info {
	return Info{ID: config.OpenGauss, Label: "openGauss", DefaultPort: 5432}
}

func (openGaussDriver) dialect() sqlscript.Dialect { return sqlscript.OpenGauss }

func (openGaussDriver) open(connection config.Connection, _ *noticeRouter) (*sql.DB, error) {
	connector, err := ogpq.NewConnector(postgresURL(connection))
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func (openGaussDriver) sessionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var pid int64
	err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid)
	return pid, err
}

func (openGaussDriver) cancel(ctx context.Context, db *sql.DB, session int64) error {
	_, err := db.ExecContext(ctx, "SELECT pg_cancel_backend($1)", session)
	return err
}

var versionNumber = regexp.MustCompile(`\d+(\.\d+)+`)

// version() reads like "(openGauss 6.0.0 build aee4abd5) compiled at ...".
func (openGaussDriver) version(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	if err := db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		return "", err
	}
	if start := strings.IndexByte(version, '('); start >= 0 {
		if number := versionNumber.FindString(version[start:]); number != "" {
			return number, nil
		}
	}
	return version, nil
}

func (openGaussDriver) describe(err error) (ErrorInfo, bool) {
	var ogErr *ogpq.Error
	if !errors.As(err, &ogErr) {
		return ErrorInfo{}, false
	}
	position, _ := strconv.Atoi(ogErr.Position)
	return ErrorInfo{
		Message:  ogErr.Severity + ": " + ogErr.Message,
		Detail:   ogErr.Detail,
		Hint:     ogErr.Hint,
		Code:     string(ogErr.Code),
		Position: position,
	}, true
}

// damengDriver treats the connection's database as the default schema,
// since a DM instance holds one database.
type damengDriver struct{}

func (damengDriver) info() Info {
	return Info{ID: config.Dameng, Label: "达梦 DM", DefaultPort: 5236}
}

func (damengDriver) dialect() sqlscript.Dialect { return sqlscript.Oracle }

func (damengDriver) open(connection config.Connection, _ *noticeRouter) (*sql.DB, error) {
	query := url.Values{}
	for key, value := range connection.Params {
		query.Set(key, value)
	}
	if query.Get("schema") == "" && connection.Database != "" {
		query.Set("schema", connection.Database)
	}
	target := url.URL{
		Scheme:   "dm",
		User:     url.UserPassword(connection.User, connection.Password),
		Host:     net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port)),
		RawQuery: query.Encode(),
	}
	return sql.Open("dm", target.String())
}

func (damengDriver) sessionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var id int64
	err := conn.QueryRowContext(ctx, "SELECT SESSID()").Scan(&id)
	return id, err
}

func (damengDriver) cancel(ctx context.Context, db *sql.DB, session int64) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("CALL SP_CANCEL_SESSION_OPERATION(%d)", session))
	return err
}

// The banner reads "DM Database Server x64 V8"; ID_CODE carries the build.
func (damengDriver) version(ctx context.Context, db *sql.DB) (string, error) {
	var banner, build string
	if err := db.QueryRowContext(ctx, "SELECT SVR_VERSION FROM V$INSTANCE").Scan(&banner); err != nil {
		return "", err
	}
	version := strings.TrimSpace(banner)
	if fields := strings.Fields(version); len(fields) > 0 {
		version = fields[len(fields)-1]
	}
	if err := db.QueryRowContext(ctx, "SELECT ID_CODE").Scan(&build); err == nil {
		version += " (" + strings.Trim(strings.TrimSpace(build), "-") + ")"
	}
	return version, nil
}

func (damengDriver) describe(err error) (ErrorInfo, bool) {
	var dmErr *dm.DmError
	if !errors.As(err, &dmErr) {
		return ErrorInfo{}, false
	}
	return ErrorInfo{Message: dmErr.Error(), Code: strconv.Itoa(int(dmErr.ErrCode))}, true
}

type xuguDriver struct{}

func (xuguDriver) info() Info {
	return Info{ID: config.Xugu, Label: "虚谷 XuguDB", DefaultPort: 5138}
}

func (xuguDriver) dialect() sqlscript.Dialect { return sqlscript.Oracle }

func (xuguDriver) open(connection config.Connection, _ *noticeRouter) (*sql.DB, error) {
	settings := map[string]string{
		"IP":              connection.Host,
		"Port":            strconv.Itoa(connection.Port),
		"DB":              connection.Database,
		"User":            connection.User,
		"PWD":             connection.Password,
		"CHAR_SET":        "UTF8",
		"AUTO_COMMIT":     "on",
		"CONNECT_TIMEOUT": strconv.Itoa(int(connectTimeout.Seconds())),
	}
	for key, value := range connection.Params {
		settings[key] = value
	}
	var dsn strings.Builder
	for _, key := range slices.Sorted(maps.Keys(settings)) {
		value := settings[key]
		if strings.ContainsAny(value, ";'= ") {
			value = "'" + strings.ReplaceAll(value, "'", "''") + "'"
		}
		fmt.Fprintf(&dsn, "%s=%s;", key, value)
	}
	return sql.Open("xugu", dsn.String())
}

func (xuguDriver) sessionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var id int64
	err := conn.QueryRowContext(ctx, "SELECT SYS_CONTEXT('USERENV', 'SESSIONID')").Scan(&id)
	return id, err
}

// The Xugu driver ignores contexts, so the server aborts the session's
// transaction instead; the session itself stays open.
func (xuguDriver) cancel(ctx context.Context, db *sql.DB, session int64) error {
	var node int64
	if err := db.QueryRowContext(ctx, "SELECT NODEID FROM SYS_ALL_SESSIONS WHERE SESSION_ID = ?", session).Scan(&node); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "EXEC DBMS_DBA.KILL_SESSION_TRANS(?, ?)", node, session)
	return err
}

func (xuguDriver) version(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return "", err
	}
	return strings.TrimPrefix(version, "XuGu SQL Server "), nil
}

// Xugu errors are plain strings such as "[E5021 L1 C15] 表或视图T不存在".
var xuguErrorTag = regexp.MustCompile(`\[(E\d+)(?: L(\d+) C\d+)?\]`)

func (xuguDriver) describe(err error) (ErrorInfo, bool) {
	message := strings.TrimSpace(strings.ReplaceAll(err.Error(), "\x00", ""))
	tags := xuguErrorTag.FindAllStringSubmatch(message, -1)
	if len(tags) == 0 {
		return ErrorInfo{}, false
	}
	info := ErrorInfo{Message: message, Code: tags[0][1]}
	for _, tag := range tags {
		if tag[2] != "" {
			info.StatementLine, _ = strconv.Atoi(tag[2])
		}
	}
	return info, true
}
