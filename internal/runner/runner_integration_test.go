//go:build integration

package runner

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/schemapilot/schemapilot/internal/config"
	"github.com/schemapilot/schemapilot/internal/database"
	"github.com/schemapilot/schemapilot/internal/workspace"
)

// Connections come from URLs such as
// SCHEMAPILOT_TEST_POSTGRES=postgres://user:pass@host:5432/db?sslmode=disable
// SCHEMAPILOT_TEST_MYSQL=mysql://user:pass@host:3306/db
func testConnection(t *testing.T, variable string, driver config.Driver) config.Connection {
	t.Helper()
	raw := os.Getenv(variable)
	if raw == "" {
		t.Skipf("%s is not set", variable)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(parsed.Port())
	password, _ := parsed.User.Password()
	params := map[string]string{}
	for key := range parsed.Query() {
		params[key] = parsed.Query().Get(key)
	}
	return config.Connection{
		Name:     "it",
		Driver:   driver,
		Host:     parsed.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(parsed.Path, "/"),
		User:     parsed.User.Username(),
		Password: password,
		Params:   params,
	}
}

func newWorkspace(t *testing.T, files map[string]string) workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opened, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return opened
}

func runPlan(t *testing.T, manager *Manager, connection config.Connection, steps [][][]string) Run {
	t.Helper()
	if _, err := manager.Start(Plan{Connection: connection.Name, Steps: steps}, connection); err != nil {
		t.Fatal(err)
	}
	manager.Wait(connection.Name)
	run, _ := manager.Get(connection.Name)
	return run
}

func fileStatus(run Run) map[string]*FileRun {
	files := map[string]*FileRun{}
	for _, file := range run.Files {
		files[file.Path] = file
	}
	return files
}

func suffix() string {
	return strconv.FormatInt(time.Now().UnixNano()%1_000_000_000, 36)
}

func TestPostgresRunHaltsOnFailureAndReportsStatement(t *testing.T) {
	connection := testConnection(t, "SCHEMAPILOT_TEST_POSTGRES", config.Postgres)
	table := "sp_it_" + suffix()
	files := newWorkspace(t, map[string]string{
		"001_create.sql":   "CREATE TABLE " + table + " (id int PRIMARY KEY, name text NOT NULL);\nCREATE TABLE IF NOT EXISTS " + table + " (id int);",
		"002_seed_a.sql":   "INSERT INTO " + table + " VALUES (1, 'a'), (2, 'b');",
		"003_seed_b.sql":   "INSERT INTO " + table + " VALUES (3, 'c');",
		"004_backfill.sql": "UPDATE " + table + " SET name = name || '!';\n\nINSERT INTO " + table + " (id, name)\nVALUES (4, NULL);\nDELETE FROM " + table + ";",
		"005_after.sql":    "DROP TABLE " + table + ";",
	})
	manager := NewManager(context.Background(), files)
	t.Cleanup(func() {
		db, err := database.Open(context.Background(), connection)
		if err == nil {
			session, _ := db.Session(context.Background())
			_, _ = session.Exec(context.Background(), "DROP TABLE IF EXISTS "+table)
			session.Close()
			db.Close()
		}
	})

	run := runPlan(t, manager, connection, [][][]string{
		{{"001_create.sql"}},
		{{"002_seed_a.sql"}, {"003_seed_b.sql"}},
		{{"004_backfill.sql"}},
		{{"005_after.sql"}},
	})
	if run.Status != Failed {
		t.Fatalf("run status %s (%s)", run.Status, run.Error)
	}
	byPath := fileStatus(run)
	create := byPath["001_create.sql"]
	if create.Status != Succeeded || len(create.Log) != 3 || create.Log[1].Kind != "notice" {
		t.Fatalf("create: %+v", create)
	}
	if byPath["002_seed_a.sql"].RowsAffected != 2 || byPath["003_seed_b.sql"].Status != Succeeded {
		t.Fatalf("parallel lanes did not succeed: %+v %+v", byPath["002_seed_a.sql"], byPath["003_seed_b.sql"])
	}
	backfill := byPath["004_backfill.sql"]
	if backfill.Status != Failed || backfill.Executed != 1 || backfill.Error == nil {
		t.Fatalf("backfill: %+v", backfill)
	}
	if backfill.Error.Index != 2 || backfill.Error.StartLine != 3 || backfill.Error.EndLine != 4 || backfill.Error.Code != "23502" {
		t.Fatalf("statement error: %+v", backfill.Error)
	}
	if byPath["005_after.sql"].Status != Pending {
		t.Fatalf("file after failure should stay pending: %+v", byPath["005_after.sql"])
	}
}

func TestPostgresStopCancelsRunningStatement(t *testing.T) {
	connection := testConnection(t, "SCHEMAPILOT_TEST_POSTGRES", config.Postgres)
	files := newWorkspace(t, map[string]string{"sleep.sql": "SELECT 1;\nSELECT pg_sleep(30);"})
	manager := NewManager(context.Background(), files)
	if _, err := manager.Start(Plan{Connection: connection.Name, Steps: [][][]string{{{"sleep.sql"}}}}, connection); err != nil {
		t.Fatal(err)
	}
	waitForStatement(t, manager, connection.Name, 2)
	startedAt := time.Now()
	if err := manager.Stop(connection.Name); err != nil {
		t.Fatal(err)
	}
	manager.Wait(connection.Name)
	if time.Since(startedAt) > 10*time.Second {
		t.Fatal("stop took too long")
	}
	run, _ := manager.Get(connection.Name)
	if run.Status != Cancelled || run.Files[0].Status != Cancelled || run.Files[0].Executed != 1 {
		t.Fatalf("run after stop: %+v %+v", run, run.Files[0])
	}
}

func TestMySQLDelimiterProcedureAndStop(t *testing.T) {
	connection := testConnection(t, "SCHEMAPILOT_TEST_MYSQL", config.MySQL)
	name := "sp_it_" + suffix()
	files := newWorkspace(t, map[string]string{
		"001_proc.sql":  "CREATE TABLE " + name + " (id INT PRIMARY KEY);\nDELIMITER $$\nCREATE PROCEDURE " + name + "_fill()\nBEGIN\n  INSERT INTO " + name + " VALUES (1);\n  INSERT INTO " + name + " VALUES (2);\nEND$$\nDELIMITER ;\nCALL " + name + "_fill();",
		"002_fail.sql":  "INSERT INTO " + name + " VALUES (1);",
		"003_sleep.sql": "SELECT SLEEP(30);",
	})
	manager := NewManager(context.Background(), files)
	t.Cleanup(func() {
		db, err := database.Open(context.Background(), connection)
		if err == nil {
			session, _ := db.Session(context.Background())
			_, _ = session.Exec(context.Background(), "DROP PROCEDURE IF EXISTS "+name+"_fill")
			_, _ = session.Exec(context.Background(), "DROP TABLE IF EXISTS "+name)
			session.Close()
			db.Close()
		}
	})

	run := runPlan(t, manager, connection, [][][]string{{{"001_proc.sql"}}, {{"002_fail.sql"}}})
	byPath := fileStatus(run)
	if proc := byPath["001_proc.sql"]; proc.Status != Succeeded || proc.Statements != 3 {
		t.Fatalf("procedure file: %+v", proc)
	}
	if fail := byPath["002_fail.sql"]; fail.Status != Failed || fail.Error == nil || !strings.Contains(fail.Error.Message, "1062") {
		t.Fatalf("duplicate insert: %+v", fail)
	}

	if _, err := manager.Start(Plan{Connection: connection.Name, Steps: [][][]string{{{"003_sleep.sql"}}}}, connection); err != nil {
		t.Fatal(err)
	}
	waitForStatement(t, manager, connection.Name, 1)
	if err := manager.Stop(connection.Name); err != nil {
		t.Fatal(err)
	}
	manager.Wait(connection.Name)
	stopped, _ := manager.Get(connection.Name)
	if stopped.Status != Cancelled {
		t.Fatalf("mysql stop: %+v", stopped.Files[0])
	}

	db, err := database.Open(context.Background(), connection)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if rows, _ := session.Exec(context.Background(), "UPDATE "+name+" SET id = id WHERE id < 0"); rows != 0 {
		t.Fatalf("unexpected rows %d", rows)
	}
}

func waitForStatement(t *testing.T, manager *Manager, connection string, index int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if run, ok := manager.Get(connection); ok && len(run.Files) > 0 && run.Files[0].Current == index {
			time.Sleep(300 * time.Millisecond)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("statement %d never started", index)
}
