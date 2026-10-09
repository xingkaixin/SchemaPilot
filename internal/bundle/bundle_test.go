package bundle

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/schemapilot/schemapilot/internal/arrangement"
	"github.com/schemapilot/schemapilot/internal/workspace"
)

func workspaceWith(t *testing.T, files map[string]string) workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opened, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return opened
}

func exportPackage(t *testing.T) string {
	t.Helper()
	source := workspaceWith(t, map[string]string{
		"pg/010_a.sql":   "SELECT 1;",
		"pg/020_b.sql":   "SELECT 2;",
		"other/x.sql":    "SELECT 3;",
		"unarranged.sql": "SELECT 4;",
	})
	arranged := arrangement.Arrangement{Connections: map[string]arrangement.Connection{
		"pg":    {Driver: "postgres", Steps: [][][]string{{{"pg/010_a.sql"}, {"pg/020_b.sql"}}}, Disabled: []string{"pg/020_b.sql"}},
		"other": {Driver: "mysql", Steps: [][][]string{{{"other/x.sql"}}}},
	}}
	var archive bytes.Buffer
	if err := Export(&archive, source, arranged, []string{"pg"}, "test"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pg.zip")
	if err := os.WriteFile(path, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportUnpacksOnlyTheChosenConnections(t *testing.T) {
	archive := exportPackage(t)
	target := workspaceWith(t, map[string]string{"pg/010_a.sql": "SELECT 1;"})

	summary, err := Import(archive, target)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Written != 1 || summary.Kept != 1 {
		t.Fatalf("written %d, kept %d", summary.Written, summary.Kept)
	}
	if _, err := os.Stat(filepath.Join(target.Root, "other", "x.sql")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a connection that was not chosen was unpacked")
	}
	loaded, _, err := arrangement.Load(target.Root)
	if err != nil || loaded.Connections["pg"].Driver != "postgres" || len(loaded.Connections["pg"].Disabled) != 1 {
		t.Fatalf("arrangement not restored: %+v %v", loaded, err)
	}

	if _, err := Import(archive, target); err != nil {
		t.Fatalf("importing the same package again: %v", err)
	}
}

func TestImportRefusesConflicts(t *testing.T) {
	archive := exportPackage(t)
	target := workspaceWith(t, map[string]string{"pg/020_b.sql": "DROP TABLE everything;"})

	_, err := Import(archive, target)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || len(conflict.Files) != 1 {
		t.Fatalf("expected a file conflict, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(target.Root, "pg", "010_a.sql")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("files were written despite the conflict")
	}

	clean := workspaceWith(t, nil)
	if _, err := arrangement.Save(clean.Root, arrangement.Arrangement{Connections: map[string]arrangement.Connection{
		"pg": {Steps: [][][]string{{{"pg/020_b.sql"}}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(archive, clean); !errors.As(err, &conflict) || len(conflict.Arrangements) != 1 {
		t.Fatalf("expected an arrangement conflict, got %v", err)
	}
}

func TestExportRefusesMissingFiles(t *testing.T) {
	source := workspaceWith(t, nil)
	arranged := arrangement.Arrangement{Connections: map[string]arrangement.Connection{
		"pg": {Steps: [][][]string{{{"pg/gone.sql"}}}},
	}}
	if err := Export(&bytes.Buffer{}, source, arranged, []string{"pg"}, ""); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
