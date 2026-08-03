package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schemapilot/schemapilot/internal/migration"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		graph       string
		databases   string
		environment map[string]string
		files       map[string]string
		wantErr     string
		check       func(*testing.T, migration.Project)
	}{
		{
			name: "valid project and stable node order",
			graph: `version: 1
name: shop
parallelism: 2
on_error: halt
nodes:
  report:
    database: reporting
    depends_on: [user, order]
    scripts:
      - report/001.sql
  user:
    database: primary
    scripts:
      - user/001.sql
  order:
    database: primary
    scripts:
      - order/001.sql
`,
			databases: `version = 1

[databases.primary]
driver = "postgres"
dsn = "${PRIMARY_DSN}"
max_open_connections = 8
connection_timeout = "2s"

[databases.reporting]
driver = "postgres"
dsn = "postgres://reporting"
`,
			environment: map[string]string{"PRIMARY_DSN": "postgres://primary"},
			files: map[string]string{
				"user/001.sql":   "create table users (id bigint);",
				"order/001.sql":  "create table orders (id bigint);",
				"report/001.sql": "create view report as select 1;",
			},
			check: func(t *testing.T, project migration.Project) {
				t.Helper()
				if got := []string{project.Graph.Nodes[0].Name, project.Graph.Nodes[1].Name, project.Graph.Nodes[2].Name}; strings.Join(got, ",") != "order,report,user" {
					t.Fatalf("node order = %v", got)
				}
				if project.Databases["primary"].DSN != "postgres://primary" {
					t.Fatalf("expanded DSN = %q", project.Databases["primary"].DSN)
				}
				if project.Graph.Nodes[1].Scripts[0].Checksum == "" || len(project.Graph.Nodes[1].Scripts[0].SQL) == 0 {
					t.Fatal("script content and checksum were not loaded")
				}
			},
		},
		{
			name: "cycle",
			graph: `version: 1
name: cycle
nodes:
  a:
    database: primary
    depends_on: [b]
    scripts: [a.sql]
  b:
    database: primary
    depends_on: [a]
    scripts: [b.sql]
`,
			databases: validDatabases(),
			files:     map[string]string{"a.sql": "a", "b.sql": "b"},
			wantErr:   "dependency cycle",
		},
		{
			name: "missing profile",
			graph: `version: 1
name: missing
nodes:
  user:
    database: absent
    scripts: [user.sql]
`,
			databases: validDatabases(),
			files:     map[string]string{"user.sql": "select 1;"},
			wantErr:   "unknown database profile",
		},
		{
			name: "path escape",
			graph: `version: 1
name: escape
nodes:
  user:
    database: primary
    scripts: [../outside.sql]
`,
			databases: validDatabases(),
			files:     map[string]string{"outside.sql": "select 1;"},
			wantErr:   "escapes the graph directory",
		},
		{
			name: "missing environment",
			graph: `version: 1
name: env
nodes:
  user:
    database: primary
    scripts: [user.sql]
`,
			databases: `version = 1
[databases.primary]
driver = "postgres"
dsn = "postgres://${NOT_SET}"
`,
			files:   map[string]string{"user.sql": "select 1;"},
			wantErr: "environment variable \"NOT_SET\" is not set",
		},
		{
			name: "defaults",
			graph: `version: 1
name: defaults
nodes:
  user:
    database: primary
    scripts: [user.sql]
`,
			databases: `version = 1
[databases.primary]
driver = "postgres"
dsn = "postgres://primary"
`,
			files: map[string]string{"user.sql": "select 1;"},
			check: func(t *testing.T, project migration.Project) {
				t.Helper()
				if project.Graph.Parallelism != defaultParallelism || project.Graph.OnError != migration.ErrorPolicyHalt {
					t.Fatalf("graph defaults = %+v", project.Graph)
				}
				profile := project.Databases["primary"]
				if profile.MaxOpenConnections != defaultMaxOpenConnections || profile.ConnectionTimeout != defaultConnectionTimeout {
					t.Fatalf("database defaults = %+v", profile)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			graphPath := filepath.Join(root, "migration.yaml")
			databasesPath := filepath.Join(root, "databases.toml")
			if err := os.WriteFile(graphPath, []byte(test.graph), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(databasesPath, []byte(test.databases), 0o600); err != nil {
				t.Fatal(err)
			}
			for filePath, content := range test.files {
				fullPath := filepath.Join(root, filePath)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for name, value := range test.environment {
				t.Setenv(name, value)
			}
			project, err := Load(context.Background(), graphPath, databasesPath)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.check != nil {
				test.check(t, project)
			}
		})
	}
}

func TestFingerprintStability(t *testing.T) {
	root := t.TempDir()
	graphPath := filepath.Join(root, "migration.yaml")
	databasesPath := filepath.Join(root, "databases.toml")
	graph := `version: 1
name: stable
parallelism: 4
on_error: halt
nodes:
  b:
    database: primary
    depends_on: [a]
    scripts: [b.sql]
  a:
    database: primary
    scripts: [a.sql]
`
	if err := os.WriteFile(graphPath, []byte(graph), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasesPath, []byte(validDatabases()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.sql"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.sql"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Load(context.Background(), graphPath, databasesPath)
	if err != nil {
		t.Fatal(err)
	}
	reordered := strings.Replace(graph, "  b:\n    database: primary\n    depends_on: [a]\n    scripts: [b.sql]\n  a:\n    database: primary\n    scripts: [a.sql]", "  a:\n    database: primary\n    scripts: [a.sql]\n  b:\n    database: primary\n    depends_on: [a]\n    scripts: [b.sql]", 1)
	if err := os.WriteFile(graphPath, []byte(reordered), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Load(context.Background(), graphPath, databasesPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprint changed after node reorder: %s != %s", first.Fingerprint, second.Fingerprint)
	}
	if first.StructureFingerprint != second.StructureFingerprint {
		t.Fatalf("structure fingerprint changed after node reorder")
	}
	if err := os.WriteFile(filepath.Join(root, "a.sql"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := Load(context.Background(), graphPath, databasesPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == third.Fingerprint {
		t.Fatal("fingerprint did not change after script content changed")
	}
	if first.StructureFingerprint != third.StructureFingerprint {
		t.Fatal("structure fingerprint changed after script content changed")
	}
	if err := os.WriteFile(databasesPath, []byte(strings.Replace(validDatabases(), "postgres://primary", "postgres://changed", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	fourth, err := Load(context.Background(), graphPath, databasesPath)
	if err != nil {
		t.Fatal(err)
	}
	if third.Fingerprint != fourth.Fingerprint || third.StructureFingerprint != fourth.StructureFingerprint {
		t.Fatal("database DSN changed graph fingerprint")
	}
	if third.EnvironmentFingerprint == fourth.EnvironmentFingerprint {
		t.Fatal("database DSN did not change environment fingerprint")
	}
	if first.Graph.Nodes[0].Scripts[0].Checksum != hex.EncodeToString(sha256Sum([]byte("a"))) {
		t.Fatal("unexpected first checksum")
	}
}

func TestSaveGraphRoundTripAndFileTree(t *testing.T) {
	root := t.TempDir()
	graphPath := filepath.Join(root, "migration.yaml")
	databasesPath := filepath.Join(root, "databases.toml")
	graph := `version: 1
name: save
nodes:
  z:
    database: primary
    depends_on: []
    scripts: [z.sql]
  a:
    database: primary
    scripts: [nested/a.sql]
`
	if err := os.WriteFile(graphPath, []byte(graph), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasesPath, []byte(validDatabases()), 0o600); err != nil {
		t.Fatal(err)
	}
	for filePath, content := range map[string]string{"z.sql": "z", "nested/a.sql": "a", "notes.txt": "n"} {
		fullPath := filepath.Join(root, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Load(context.Background(), graphPath, databasesPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveGraph(context.Background(), graphPath, loaded.Graph); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(context.Background(), graphPath, databasesPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Graph.Name != loaded.Graph.Name || reloaded.Fingerprint != loaded.Fingerprint {
		t.Fatalf("round trip changed graph: before=%+v after=%+v", loaded.Graph, reloaded.Graph)
	}
	paths, err := FileTree(context.Background(), graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths, ","); got != "nested/a.sql,z.sql" {
		t.Fatalf("file tree = %q", got)
	}
}

func TestSaveGraphRejectsInvalidGraphBeforeReplacement(t *testing.T) {
	root := t.TempDir()
	graphPath := filepath.Join(root, "migration.yaml")
	original := []byte("original graph\n")
	if err := os.WriteFile(graphPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	invalid := migration.Graph{
		Version:     migration.CurrentGraphVersion,
		Name:        "invalid",
		Parallelism: 1,
		OnError:     migration.ErrorPolicyHalt,
		Nodes:       []migration.Node{{Name: "user", Database: "primary", Scripts: []migration.Script{{Path: "missing.sql"}}}},
	}
	if err := SaveGraph(context.Background(), graphPath, invalid); err == nil || !strings.Contains(err.Error(), "read SQL file") {
		t.Fatalf("error = %v", err)
	}
	content, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(original) {
		t.Fatal("invalid graph replaced the existing file")
	}
	info, err := os.Stat(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("file mode changed: %o", info.Mode().Perm())
	}
}

func validDatabases() string {
	return `version = 1
[databases.primary]
driver = "postgres"
dsn = "postgres://primary"
`
}

func sha256Sum(value []byte) []byte {
	sum := sha256.Sum256(value)
	return sum[:]
}

func TestLoadContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Load(ctx, "migration.yaml", "databases.toml")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestFingerprintIncludesChecksumsOnly(t *testing.T) {
	graph := migration.Graph{
		Version:     1,
		Name:        "fingerprint",
		Parallelism: 1,
		OnError:     migration.ErrorPolicyHalt,
		Nodes:       []migration.Node{{Name: "a", Database: "primary", Scripts: []migration.Script{{Path: "a.sql", Checksum: "one", SQL: "first"}}}},
	}
	first, err := fingerprint(graph)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := structureFingerprint(graph)
	if err != nil {
		t.Fatal(err)
	}
	graph.Parallelism = 9
	graph.OnError = migration.ErrorPolicyContinue
	changedStructure, err := structureFingerprint(graph)
	if err != nil {
		t.Fatal(err)
	}
	if structure != changedStructure {
		t.Fatal("structure fingerprint includes execution policy")
	}
	graph.Parallelism = 1
	graph.OnError = migration.ErrorPolicyHalt
	graph.Nodes[0].ErrorPolicy = migration.ErrorPolicyHalt
	explicitPolicyFingerprint, err := fingerprint(graph)
	if err != nil {
		t.Fatal(err)
	}
	if first != explicitPolicyFingerprint {
		t.Fatal("effective node policy changed fingerprint")
	}
	graph.Nodes[0].Scripts[0].Checksum = "two"
	graph.Nodes[0].Scripts[0].SQL = "second"
	second, err := fingerprint(graph)
	if err != nil {
		t.Fatal(err)
	}
	newStructure, err := structureFingerprint(graph)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || structure != newStructure {
		t.Fatal("fingerprint contract violated")
	}
}
