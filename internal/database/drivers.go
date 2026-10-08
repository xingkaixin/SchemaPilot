package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/schemapilot/schemapilot/internal/config"
	"github.com/schemapilot/schemapilot/internal/sqlscript"
)

type postgresDriver struct{}

func (postgresDriver) info() Info {
	return Info{ID: config.Postgres, Label: "PostgreSQL", DefaultPort: 5432}
}

func (postgresDriver) dialect() sqlscript.Dialect { return sqlscript.Postgres }

func (postgresDriver) open(connection config.Connection, notices *noticeRouter) (*sql.DB, error) {
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
	parsed, err := pgx.ParseConfig(target.String())
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
