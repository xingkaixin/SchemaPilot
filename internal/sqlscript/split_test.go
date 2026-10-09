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

func TestSplitSQLServerBatchesAndBlocks(t *testing.T) {
	source := "CREATE TABLE [a;b] (id int);\nGO\nCREATE OR ALTER PROCEDURE p AS\n  SELECT 1;\n  SELECT 2;\ngo\nIF 1 = 1\nBEGIN\n  BEGIN TRAN;\n  UPDATE t SET v = CASE WHEN v > 0 THEN 1 END;\n  COMMIT;\nEND;\nSELECT N'x;y'\n"
	check(t, source, SQLServer, []want{
		{"CREATE TABLE [a;b] (id int)", 1, 1},
		{"CREATE OR ALTER PROCEDURE p AS\n  SELECT 1;\n  SELECT 2;", 3, 5},
		{"IF 1 = 1\nBEGIN\n  BEGIN TRAN;\n  UPDATE t SET v = CASE WHEN v > 0 THEN 1 END;\n  COMMIT;\nEND", 7, 12},
		{"SELECT N'x;y'", 13, 13},
	})
}

func TestSplitOraclePLSQLUnits(t *testing.T) {
	source := "CREATE TABLE t (v VARCHAR2(10));\nINSERT INTO t VALUES (q'[it's;]');\nCREATE OR REPLACE PROCEDURE p IS\nBEGIN\n  UPDATE t SET v = 'x';\nEND;\n/\nBEGIN\n  p;\nEND;\n/\nSELECT 4 / 2 FROM dual;\n"
	check(t, source, Oracle, []want{
		{"CREATE TABLE t (v VARCHAR2(10))", 1, 1},
		{"INSERT INTO t VALUES (q'[it's;]')", 2, 2},
		{"CREATE OR REPLACE PROCEDURE p IS\nBEGIN\n  UPDATE t SET v = 'x';\nEND;", 3, 6},
		{"BEGIN\n  p;\nEND;", 8, 10},
		{"SELECT 4 / 2 FROM dual", 12, 12},
	})
}

func TestSplitSQLiteTriggerBody(t *testing.T) {
	source := "BEGIN;\nCREATE TEMP TRIGGER tr AFTER INSERT ON t BEGIN\n  UPDATE t SET v = CASE WHEN v IS NULL THEN 0 END;\n  INSERT INTO log VALUES (1);\nEND;\nCOMMIT;\n"
	check(t, source, SQLite, []want{
		{"BEGIN", 1, 1},
		{"CREATE TEMP TRIGGER tr AFTER INSERT ON t BEGIN\n  UPDATE t SET v = CASE WHEN v IS NULL THEN 0 END;\n  INSERT INTO log VALUES (1);\nEND", 2, 5},
		{"COMMIT", 6, 6},
	})
}

func TestSplitOpenGaussPLSQLAndDollarBodies(t *testing.T) {
	source := "BEGIN;\nCREATE FUNCTION f(a int DEFAULT CAST(1 AS int)) RETURNS int AS $$ SELECT a; $$ LANGUAGE sql;\nCREATE OR REPLACE PROCEDURE p(n int) AS\n  total int;\nBEGIN\n  total := n;\nEND;\n/\nBEGIN\n  p(1);\nEND;\n/\nCOMMIT;\n"
	check(t, source, OpenGauss, []want{
		{"BEGIN", 1, 1},
		{"CREATE FUNCTION f(a int DEFAULT CAST(1 AS int)) RETURNS int AS $$ SELECT a; $$ LANGUAGE sql", 2, 2},
		{"CREATE OR REPLACE PROCEDURE p(n int) AS\n  total int;\nBEGIN\n  total := n;\nEND;", 3, 7},
		{"BEGIN\n  p(1);\nEND;", 9, 11},
		{"COMMIT", 13, 13},
	})
}
