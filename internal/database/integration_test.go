//go:build integration

package database_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/schemapilot/schemapilot/internal/database"
	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

func TestDatabaseAdapters(t *testing.T) {
	tests := []struct {
		name        string
		driver      migration.Driver
		environment string
		createSQL   func(string) string
		alterSQL    func(string) string
	}{
		{
			name:        "postgres",
			driver:      migration.DriverPostgres,
			environment: "SCHEMAPILOT_POSTGRES_DSN",
			createSQL:   func(table string) string { return fmt.Sprintf("CREATE TABLE %s (id BIGINT NOT NULL)", table) },
			alterSQL:    func(table string) string { return fmt.Sprintf("ALTER TABLE %s ADD COLUMN force_marker BIGINT", table) },
		},
		{
			name:        "mysql",
			driver:      migration.DriverMySQL,
			environment: "SCHEMAPILOT_MYSQL_DSN",
			createSQL:   func(table string) string { return fmt.Sprintf("CREATE TABLE %s (id BIGINT NOT NULL)", table) },
			alterSQL:    func(table string) string { return fmt.Sprintf("ALTER TABLE %s ADD COLUMN force_marker BIGINT", table) },
		},
		{
			name:        "sqlserver",
			driver:      migration.DriverSQLServer,
			environment: "SCHEMAPILOT_SQLSERVER_DSN",
			createSQL:   func(table string) string { return fmt.Sprintf("CREATE TABLE [%s] (id BIGINT NOT NULL)", table) },
			alterSQL:    func(table string) string { return fmt.Sprintf("ALTER TABLE [%s] ADD force_marker BIGINT NULL", table) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := os.Getenv(test.environment)
			if dsn == "" {
				t.Skipf("%s is not set", test.environment)
			}
			table := fmt.Sprintf("schemapilot_contract_%d", time.Now().UnixNano())
			runAdapterContract(t, test.driver, dsn, table, test.createSQL(table), test.alterSQL(table))
			runConcurrentHistoryContract(t, test.driver, dsn, test.createSQL)
		})
	}
}

func runAdapterContract(t *testing.T, driver migration.Driver, dsn, table, createSQL, alterSQL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	target, err := database.NewConnector().Connect(ctx, migration.DatabaseProfile{
		Name:               string(driver),
		Driver:             driver,
		DSN:                dsn,
		MaxOpenConnections: 2,
		ConnectionTimeout:  15 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	application := execution.ScriptApplication{
		GraphName: "adapter-contract",
		NodeName:  string(driver),
		Path:      table + ".sql",
		Checksum:  "checksum-v1",
		SQL:       createSQL,
	}
	result, err := target.Apply(ctx, application)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != execution.ApplyOutcomeApplied {
		t.Fatalf("first outcome = %s", result.Outcome)
	}

	result, err = target.Apply(ctx, application)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != execution.ApplyOutcomeAlreadyApplied {
		t.Fatalf("repeated outcome = %s", result.Outcome)
	}

	application.Checksum = "checksum-v2"
	application.SQL = alterSQL
	_, err = target.Apply(ctx, application)
	var mismatch *execution.ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("checksum mismatch error = %v", err)
	}
	if strings.Contains(err.Error(), dsn) {
		t.Fatal("checksum error leaked the DSN")
	}

	application.Force = true
	result, err = target.Apply(ctx, application)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != execution.ApplyOutcomeApplied {
		t.Fatalf("forced outcome = %s", result.Outcome)
	}
}

func runConcurrentHistoryContract(t *testing.T, driver migration.Driver, dsn string, createSQL func(string) string) {
	t.Helper()
	db, err := sql.Open(sqlDriver(driver), dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(dropHistorySQL(driver)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	connector := database.NewConnector()
	profile := migration.DatabaseProfile{
		Name:               string(driver),
		Driver:             driver,
		DSN:                dsn,
		MaxOpenConnections: 2,
		ConnectionTimeout:  15 * time.Second,
	}
	targets := make([]execution.TargetDatabase, 2)
	for index := range targets {
		targets[index], err = connector.Connect(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		defer targets[index].Close()
	}

	start := make(chan struct{})
	errors := make(chan error, len(targets))
	for index, target := range targets {
		table := fmt.Sprintf("schemapilot_parallel_%d_%d", time.Now().UnixNano(), index)
		application := execution.ScriptApplication{
			GraphName: "parallel-history-contract",
			NodeName:  fmt.Sprintf("node-%d", index),
			Path:      table + ".sql",
			Checksum:  "checksum-v1",
			SQL:       createSQL(table),
		}
		go func() {
			<-start
			_, applyErr := target.Apply(ctx, application)
			errors <- applyErr
		}()
	}
	close(start)
	for range targets {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
}

func sqlDriver(driver migration.Driver) string {
	switch driver {
	case migration.DriverPostgres:
		return "pgx"
	case migration.DriverMySQL:
		return "mysql"
	default:
		return "sqlserver"
	}
}

func dropHistorySQL(driver migration.Driver) string {
	if driver == migration.DriverSQLServer {
		return "DROP TABLE IF EXISTS [_schemapilot_history]"
	}
	return "DROP TABLE IF EXISTS _schemapilot_history"
}
