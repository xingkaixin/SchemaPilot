package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, relative string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanAssignsConnectionDirectories(t *testing.T) {
	root := t.TempDir()
	for _, file := range []string{
		"010_root.sql",
		"notes.txt",
		"pg-main/001_schema.sql",
		"pg-main/seed/002_users.sql",
		"pg-main/.cache/skip.sql",
		"other/ignored.sql",
	} {
		write(t, root, file)
	}
	workspace, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := workspace.Scan([]string{"pg-main", "mysql-report"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, file := range files {
		got = append(got, file.Path+"@"+file.Connection)
	}
	want := "010_root.sql@,pg-main/001_schema.sql@pg-main,pg-main/seed/002_users.sql@pg-main"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v", got)
	}
}

func TestImportNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	write(t, root, "hotfix.sql")
	workspace, _ := Open(root)
	name, err := workspace.Import("/Users/me/Downloads/hotfix.sql", strings.NewReader("SELECT 2;"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "hotfix-1.sql" {
		t.Fatalf("got %s", name)
	}
	if _, err := workspace.Import("notes.txt", strings.NewReader("")); err == nil {
		t.Fatal("non-SQL import should fail")
	}
}

func TestResolveRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.sql")
	if err := os.Symlink(filepath.Join(outside, "secret.sql"), filepath.Join(root, "link.sql")); err != nil {
		t.Fatal(err)
	}
	workspace, _ := Open(root)
	for _, relative := range []string{"../secret.sql", "/etc/passwd.sql", "link.sql", "a.txt"} {
		if _, err := workspace.Resolve(relative); err == nil {
			t.Fatalf("%s should be rejected", relative)
		}
	}
}
