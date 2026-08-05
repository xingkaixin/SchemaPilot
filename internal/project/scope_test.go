package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/schemapilot/schemapilot/internal/migration"
)

func loadScopeFixture(t *testing.T) migration.Project {
	t.Helper()
	root := t.TempDir()
	graphPath := filepath.Join(root, "migration.yaml")
	databasesPath := filepath.Join(root, "databases.toml")
	graph := `version: 1
name: shop
parallelism: 2
on_error: halt
nodes:
  user:
    database: primary
    scripts: [user.sql]
  order:
    database: primary
    scripts: [order.sql]
  report:
    database: primary
    depends_on: [user, order]
    scripts: [report.sql]
`
	if err := os.WriteFile(graphPath, []byte(graph), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasesPath, []byte(validDatabases()), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"user.sql", "order.sql", "report.sql"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("select 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Load(context.Background(), graphPath, databasesPath)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestScopedRestrictsGraphAndRefreshesFingerprints(t *testing.T) {
	loaded := loadScopeFixture(t)
	scoped, err := Scoped(loaded, []string{"order"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Graph.Nodes) != 1 || scoped.Graph.Nodes[0].Name != "order" {
		t.Fatalf("scoped nodes = %+v", scoped.Graph.Nodes)
	}
	if scoped.Fingerprint == loaded.Fingerprint {
		t.Fatal("fingerprint was not refreshed for the subgraph")
	}
	if scoped.StructureFingerprint == loaded.StructureFingerprint {
		t.Fatal("structure fingerprint was not refreshed for the subgraph")
	}
	if scoped.EnvironmentFingerprint != loaded.EnvironmentFingerprint {
		t.Fatal("environment fingerprint must stay stable")
	}
}

func TestScopedCoveringAllNodesKeepsFingerprints(t *testing.T) {
	loaded := loadScopeFixture(t)
	scoped, err := Scoped(loaded, []string{"report"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Graph.Nodes) != 3 {
		t.Fatalf("expected full closure, got %+v", scoped.Graph.Nodes)
	}
	if scoped.Fingerprint != loaded.Fingerprint || scoped.StructureFingerprint != loaded.StructureFingerprint {
		t.Fatal("fingerprints must be unchanged when the closure covers every node")
	}
}

func TestScopedRejectsUnknownNode(t *testing.T) {
	loaded := loadScopeFixture(t)
	if _, err := Scoped(loaded, []string{"missing"}); err == nil {
		t.Fatal("expected error for unknown node")
	}
}
