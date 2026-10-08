package sqlscript

import (
	"reflect"
	"testing"
)

type want struct {
	text       string
	start, end int
}

func check(t *testing.T, source string, dialect Dialect, expected []want) {
	t.Helper()
	statements := Split(source, dialect)
	var got []want
	for index, statement := range statements {
		if statement.Index != index+1 {
			t.Fatalf("statement %d has index %d", index+1, statement.Index)
		}
		got = append(got, want{statement.Text, statement.StartLine, statement.EndLine})
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("Split mismatch\n got: %#v\nwant: %#v", got, expected)
	}
}

func TestSplitSkipsCommentsAndTracksLines(t *testing.T) {
	source := "-- header\nUPDATE orders o\nSET a = 1\nWHERE b IS NULL;\n\n/* block\n comment */\nANALYZE orders; -- trailing\n-- only comments\n"
	check(t, source, Postgres, []want{
		{"UPDATE orders o\nSET a = 1\nWHERE b IS NULL", 2, 4},
		{"ANALYZE orders", 8, 8},
	})
}

func TestSplitKeepsDelimitersInsideLiterals(t *testing.T) {
	source := "INSERT INTO t VALUES ('a;b', 'it''s; fine');\nSELECT \"semi;colon\" FROM t\n"
	check(t, source, Postgres, []want{
		{"INSERT INTO t VALUES ('a;b', 'it''s; fine')", 1, 1},
		{"SELECT \"semi;colon\" FROM t", 2, 2},
	})
}

func TestSplitPostgresDollarQuotedBodies(t *testing.T) {
	source := "CREATE FUNCTION f() RETURNS int AS $$\nBEGIN\n  RETURN 1;\nEND;\n$$ LANGUAGE plpgsql;\nDO $body$ BEGIN PERFORM 1; END $body$;\nSELECT $1;"
	statements := Split(source, Postgres)
	if len(statements) != 3 {
		t.Fatalf("got %d statements: %#v", len(statements), statements)
	}
	if statements[0].StartLine != 1 || statements[0].EndLine != 5 {
		t.Fatalf("function spans %d-%d", statements[0].StartLine, statements[0].EndLine)
	}
	if statements[1].Text != "DO $body$ BEGIN PERFORM 1; END $body$" {
		t.Fatalf("unexpected DO block %q", statements[1].Text)
	}
}

func TestSplitPostgresEscapeStringsAndNestedComments(t *testing.T) {
	source := "SELECT E'it\\'s;';\n/* outer /* inner; */ still; */ SELECT 2;"
	check(t, source, Postgres, []want{
		{"SELECT E'it\\'s;'", 1, 1},
		{"SELECT 2", 2, 2},
	})
}

func TestSplitMySQLDelimiterCommand(t *testing.T) {
	source := "DROP PROCEDURE IF EXISTS p;\nDELIMITER $$\nCREATE PROCEDURE p()\nBEGIN\n  SELECT 1;\n  SELECT 2;\nEND$$\nDELIMITER ;\nCALL p();\n"
	check(t, source, MySQL, []want{
		{"DROP PROCEDURE IF EXISTS p", 1, 1},
		{"CREATE PROCEDURE p()\nBEGIN\n  SELECT 1;\n  SELECT 2;\nEND", 3, 7},
		{"CALL p()", 9, 9},
	})
}

func TestSplitMySQLCommentsAndQuotes(t *testing.T) {
	source := "# hash comment;\nSELECT 'a\\';b', `c;d` FROM t; -- note\nSELECT 5--1;\n/*!40101 SET NAMES utf8mb4 */;\n"
	check(t, source, MySQL, []want{
		{"SELECT 'a\\';b', `c;d` FROM t", 2, 2},
		{"SELECT 5--1", 3, 3},
		{"/*!40101 SET NAMES utf8mb4 */", 4, 4},
	})
}

func TestSplitWithoutTrailingDelimiter(t *testing.T) {
	check(t, "CREATE TABLE a (id int);\nCREATE TABLE b (id int)", Postgres, []want{
		{"CREATE TABLE a (id int)", 1, 1},
		{"CREATE TABLE b (id int)", 2, 2},
	})
}
