package database

import (
	"strings"
	"testing"

	"github.com/schemapilot/schemapilot/internal/migration"
)

func TestDialects(t *testing.T) {
	tests := []struct {
		name         string
		driver       migration.Driver
		ddlNeedle    string
		selectNeedle string
	}{
		{name: "postgres", driver: migration.DriverPostgres, ddlNeedle: "CREATE TABLE IF NOT EXISTS", selectNeedle: "FOR UPDATE"},
		{name: "mysql", driver: migration.DriverMySQL, ddlNeedle: "DATETIME(6)", selectNeedle: "FOR UPDATE"},
		{name: "sqlserver", driver: migration.DriverSQLServer, ddlNeedle: "OBJECT_ID", selectNeedle: "HOLDLOCK"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dialect, err := dialectFor(test.driver)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(dialect.historyDDL(), test.ddlNeedle) {
				t.Fatalf("DDL does not contain %q: %s", test.ddlNeedle, dialect.historyDDL())
			}
			if !strings.Contains(dialect.historySelect(), test.selectNeedle) {
				t.Fatalf("history SELECT does not contain %q: %s", test.selectNeedle, dialect.historySelect())
			}
			if !strings.Contains(dialect.historyDDL(), "record_key") {
				t.Fatalf("DDL has no record key: %s", dialect.historyDDL())
			}
		})
	}
}

func TestRecordKeyIncludesCompleteScriptPath(t *testing.T) {
	prefix := strings.Repeat("a", 512)
	first := recordKey("graph", "node", prefix+"-one.sql")
	second := recordKey("graph", "node", prefix+"-two.sql")
	if first == second {
		t.Fatalf("different script paths share record key %q", first)
	}
	if len(first) != 64 || len(second) != 64 {
		t.Fatalf("record keys must be 64 hex characters: %q, %q", first, second)
	}
}
