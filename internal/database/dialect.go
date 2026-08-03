package database

import (
	"fmt"
	"strings"

	"github.com/schemapilot/schemapilot/internal/migration"
)

type sqlDialect interface {
	historyDDL() string
	historySelect() string
	historyInsert() string
	historyUpdate() string
}

func dialectFor(driver migration.Driver) (sqlDialect, error) {
	switch driver {
	case migration.DriverPostgres:
		return postgresDialect{}, nil
	case migration.DriverMySQL:
		return mysqlDialect{}, nil
	case migration.DriverSQLServer:
		return sqlServerDialect{}, nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
}

type postgresDialect struct{}

func (postgresDialect) historyDDL() string {
	return `CREATE TABLE IF NOT EXISTS _schemapilot_history (
    record_key CHAR(64) NOT NULL,
    graph_name TEXT NOT NULL,
    node_name TEXT NOT NULL,
    script_path TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL,
    forced BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (record_key)
)`
}

func (postgresDialect) historySelect() string {
	return `SELECT checksum FROM _schemapilot_history
WHERE record_key = $1 AND graph_name = $2 AND node_name = $3 AND script_path = $4
FOR UPDATE`
}

func (postgresDialect) historyInsert() string {
	return `INSERT INTO _schemapilot_history
(record_key, graph_name, node_name, script_path, checksum, applied_at, forced)
VALUES ($1, $2, $3, $4, $5, $6, $7)`
}

func (postgresDialect) historyUpdate() string {
	return `UPDATE _schemapilot_history
SET checksum = $1, applied_at = $2, forced = $3
WHERE record_key = $4 AND graph_name = $5 AND node_name = $6 AND script_path = $7`
}

type mysqlDialect struct{}

func (mysqlDialect) historyDDL() string {
	return `CREATE TABLE IF NOT EXISTS _schemapilot_history (
    record_key CHAR(64) NOT NULL,
    graph_name TEXT NOT NULL,
    node_name TEXT NOT NULL,
    script_path LONGTEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at DATETIME(6) NOT NULL,
    forced BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (record_key)
)`
}

func (mysqlDialect) historySelect() string {
	return `SELECT checksum FROM _schemapilot_history
WHERE record_key = ? AND graph_name = ? AND node_name = ? AND script_path = ?
FOR UPDATE`
}

func (mysqlDialect) historyInsert() string {
	return `INSERT INTO _schemapilot_history
(record_key, graph_name, node_name, script_path, checksum, applied_at, forced)
VALUES (?, ?, ?, ?, ?, ?, ?)`
}

func (mysqlDialect) historyUpdate() string {
	return `UPDATE _schemapilot_history
SET checksum = ?, applied_at = ?, forced = ?
WHERE record_key = ? AND graph_name = ? AND node_name = ? AND script_path = ?`
}

type sqlServerDialect struct{}

func (sqlServerDialect) historyDDL() string {
	return `IF OBJECT_ID(N'_schemapilot_history', N'U') IS NULL
BEGIN
    CREATE TABLE _schemapilot_history (
        record_key VARCHAR(64) NOT NULL,
        graph_name NVARCHAR(MAX) NOT NULL,
        node_name NVARCHAR(MAX) NOT NULL,
        script_path NVARCHAR(MAX) NOT NULL,
        checksum NVARCHAR(MAX) NOT NULL,
        applied_at DATETIME2(7) NOT NULL,
        forced BIT NOT NULL CONSTRAINT DF_schemapilot_history_forced DEFAULT 0,
        CONSTRAINT PK_schemapilot_history PRIMARY KEY (record_key)
    )
END`
}

func (sqlServerDialect) historySelect() string {
	return `SELECT checksum FROM _schemapilot_history WITH (UPDLOCK, HOLDLOCK)
WHERE record_key = @p1 AND graph_name = @p2 AND node_name = @p3 AND script_path = @p4`
}

func (sqlServerDialect) historyInsert() string {
	return `INSERT INTO _schemapilot_history
(record_key, graph_name, node_name, script_path, checksum, applied_at, forced)
VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7)`
}

func (sqlServerDialect) historyUpdate() string {
	return `UPDATE _schemapilot_history
SET checksum = @p1, applied_at = @p2, forced = @p3
WHERE record_key = @p4 AND graph_name = @p5 AND node_name = @p6 AND script_path = @p7`
}

func historySelectFor(dialect sqlDialect) string {
	return strings.TrimSpace(dialect.historySelect())
}
