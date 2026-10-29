package dialect

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	pg, my, lite, ms := Postgres{}, MySQL{}, SQLite{}, SQLServer{}
	tests := []struct {
		name string
		d    Dialect
		sql  string
		want Class
		// destructive is the Why of a destructive statement, empty when it is not one.
		destructive string
		rows        bool
	}{
		{"select", pg, "SELECT * FROM t", ClassRead, "", true},
		{"select in lower case", pg, "select 1", ClassRead, "", true},
		{"select with a leading comment", pg, "-- hi\n/* x */ SELECT 1", ClassRead, "", true},
		{"a parenthesised select", pg, "(SELECT 1) UNION (SELECT 2)", ClassRead, "", true},
		{"select for update is a read", pg, "SELECT * FROM t FOR UPDATE", ClassRead, "", true},
		{"select into creates a table in PostgreSQL", pg, "SELECT * INTO copy FROM t", ClassWrite, "", true},
		{"select into creates a table in SQL Server", ms, "SELECT * INTO #copy FROM t", ClassWrite, "", true},
		{"select into a variable in MySQL", my, "SELECT 1 INTO @x", ClassRead, "", true},
		{"select into outfile", my, "SELECT * FROM t INTO OUTFILE '/tmp/x'", ClassWrite, "", true},
		{"the word INTO in a string", pg, "SELECT 'INTO' FROM t", ClassRead, "", true},
		{"with over a select", pg, "WITH a AS (SELECT 1) SELECT * FROM a", ClassRead, "", true},
		{"with over a delete", pg, "WITH d AS (DELETE FROM t WHERE x = 1 RETURNING *) SELECT * FROM d", ClassWrite, "", true},
		{"with over a delete without where", pg, "WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", ClassWrite, "DELETE without WHERE", true},
		{"with before an update", pg, "WITH a AS (SELECT 1) UPDATE t SET x = 1", ClassWrite, "UPDATE without WHERE", false},
		{"with before an update that has a where", pg, "WITH a AS (SELECT 1 x) UPDATE t SET x = 1 WHERE y IN (SELECT x FROM a)", ClassWrite, "", false},
		{"values", pg, "VALUES (1), (2)", ClassRead, "", true},
		{"show", my, "SHOW TABLES", ClassRead, "", true},
		{"describe", my, "DESCRIBE t", ClassRead, "", true},
		{"explain", pg, "EXPLAIN SELECT 1", ClassRead, "", true},
		{"explain analyze of a select", pg, "EXPLAIN ANALYZE SELECT 1", ClassRead, "", true},
		{"explain analyze of a delete", pg, "EXPLAIN ANALYZE DELETE FROM t", ClassWrite, "DELETE without WHERE", true},
		{"explain with options of an update", pg, "EXPLAIN (ANALYZE, BUFFERS) UPDATE t SET a = 1 WHERE b = 2", ClassWrite, "", true},
		{"explain query plan", lite, "EXPLAIN QUERY PLAN SELECT 1", ClassRead, "", true},
		{"insert", pg, "INSERT INTO t VALUES (1)", ClassWrite, "", false},
		{"insert returning", pg, "INSERT INTO t VALUES (1) RETURNING id", ClassWrite, "", true},
		{"output clause", ms, "INSERT INTO t OUTPUT inserted.id VALUES (1)", ClassWrite, "", true},
		{"update with a where", pg, "UPDATE t SET a = 1 WHERE id = 2", ClassWrite, "", false},
		{"update without a where", pg, "UPDATE t SET a = 1", ClassWrite, "UPDATE without WHERE", false},
		{"update whose where is in a subquery only", pg, "UPDATE t SET a = (SELECT b FROM u WHERE u.id = 1)", ClassWrite, "UPDATE without WHERE", false},
		{"delete with a where", pg, "DELETE FROM t WHERE a = 1", ClassWrite, "", false},
		{"delete without a where", pg, "delete from t", ClassWrite, "DELETE without WHERE", false},
		{"delete using", pg, "DELETE FROM t USING u WHERE t.id = u.id", ClassWrite, "", false},
		{"merge", ms, "MERGE t USING s ON t.id = s.id WHEN MATCHED THEN DELETE;", ClassWrite, "", false},
		{"replace into", my, "REPLACE INTO t VALUES (1)", ClassWrite, "", false},
		{"drop table", pg, "DROP TABLE t", ClassWrite, "DROP TABLE", false},
		{"drop temporary table", my, "DROP TEMPORARY TABLE t", ClassWrite, "DROP TABLE", false},
		{"drop index concurrently", pg, "DROP INDEX CONCURRENTLY i", ClassWrite, "DROP INDEX", false},
		{"truncate", pg, "TRUNCATE t", ClassWrite, "TRUNCATE", false},
		{"truncate table", pg, "TRUNCATE TABLE t", ClassWrite, "TRUNCATE TABLE", false},
		{"alter table", pg, "ALTER TABLE t ADD COLUMN c int", ClassWrite, "ALTER TABLE", false},
		{"create table", pg, "CREATE TABLE t (a int)", ClassWrite, "", false},
		{"create or replace view", pg, "CREATE OR REPLACE VIEW v AS SELECT 1", ClassWrite, "", false},
		{"grant", pg, "GRANT SELECT ON t TO u", ClassWrite, "", false},
		{"call", pg, "CALL p()", ClassWrite, "", true},
		{"exec", ms, "EXEC sp_who", ClassWrite, "", true},
		{"begin", pg, "BEGIN", ClassTransaction, "", false},
		{"begin transaction", ms, "BEGIN TRANSACTION", ClassTransaction, "", false},
		{"begin read write", pg, "BEGIN READ WRITE", ClassWrite, "", false},
		{"start transaction", my, "START TRANSACTION", ClassTransaction, "", false},
		{"start transaction read write", my, "START TRANSACTION READ WRITE", ClassWrite, "", false},
		{"commit", pg, "COMMIT", ClassTransaction, "", false},
		{"rollback", pg, "ROLLBACK", ClassTransaction, "", false},
		{"savepoint", pg, "SAVEPOINT a", ClassTransaction, "", false},
		{"use", my, "USE shop", ClassSession, "", false},
		{"set", pg, "SET search_path TO shop", ClassSession, "", false},
		{"set the session read write", pg, "SET default_transaction_read_only = off", ClassWrite, "", false},
		{"set transaction read write", pg, "SET TRANSACTION READ WRITE", ClassWrite, "", false},
		{"set session transaction read write", my, "SET SESSION TRANSACTION READ WRITE", ClassWrite, "", false},
		{"pragma without an argument", lite, "PRAGMA journal_mode", ClassRead, "", true},
		{"pragma table_info", lite, "PRAGMA table_info(books)", ClassRead, "", true},
		{"pragma with a schema", lite, "PRAGMA main.table_info('books')", ClassRead, "", true},
		{"pragma that sets", lite, "PRAGMA journal_mode = WAL", ClassWrite, "", true},
		{"pragma that sets with an argument", lite, "PRAGMA foreign_keys(ON)", ClassWrite, "", true},
		{"pragma table_info that is set", lite, "PRAGMA table_info = 1", ClassWrite, "", true},
		{"pragma that acts", lite, "PRAGMA wal_checkpoint", ClassWrite, "", false},
		{"empty", pg, "", ClassEmpty, "", false},
		{"only a comment", pg, "-- nothing", ClassEmpty, "", false},
		{"unknown verb", pg, "FROBNICATE t", ClassWrite, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := Classify(tc.d, tc.sql)
			if info.Class != tc.want {
				t.Errorf("Classify(%q).Class = %v, want %v", tc.sql, info.Class, tc.want)
			}
			if tc.destructive == "" && info.Destructive {
				t.Errorf("Classify(%q) is destructive (%s)", tc.sql, info.Why)
			}
			if tc.destructive != "" && (!info.Destructive || info.Why != tc.destructive) {
				t.Errorf("Classify(%q) destructive = %v, why %q; want %q", tc.sql, info.Destructive, info.Why, tc.destructive)
			}
			if info.Rows != tc.rows {
				t.Errorf("Classify(%q).Rows = %v, want %v", tc.sql, info.Rows, tc.rows)
			}
		})
	}
}

func TestClassifyTransactionControl(t *testing.T) {
	tests := []struct {
		d            Dialect
		sql          string
		begins, ends bool
	}{
		{Postgres{}, "BEGIN", true, false},
		{Postgres{}, "BEGIN ISOLATION LEVEL SERIALIZABLE", true, false},
		{Postgres{}, "START TRANSACTION", true, false},
		{Postgres{}, "COMMIT", false, true},
		{Postgres{}, "END", false, true},
		{Postgres{}, "ROLLBACK", false, true},
		{Postgres{}, "ROLLBACK TO SAVEPOINT a", false, false},
		{Postgres{}, "SAVEPOINT a", false, false},
		{SQLServer{}, "BEGIN TRAN", true, false},
		{SQLServer{}, "COMMIT TRANSACTION", false, true},
		{SQLite{}, "BEGIN IMMEDIATE", true, false},
		{MySQL{}, "START TRANSACTION", true, false},
		{MySQL{}, "SELECT 1", false, false},
	}
	for _, tc := range tests {
		info := Classify(tc.d, tc.sql)
		if info.Begins != tc.begins || info.Ends != tc.ends {
			t.Errorf("Classify(%s, %q): begins %v ends %v, want %v %v", tc.d.ID(), tc.sql, info.Begins, info.Ends, tc.begins, tc.ends)
		}
	}
}

func TestOnlyReadsPassAReadOnlyConnection(t *testing.T) {
	for _, tc := range []struct {
		sql     string
		allowed bool
	}{
		{"SELECT 1", true}, {"BEGIN", true}, {"USE x", true}, {"SET search_path TO a", true}, {"", true},
		{"INSERT INTO t VALUES (1)", false}, {"DROP TABLE t", false}, {"CALL p()", false}, {"SET default_transaction_read_only = off", false},
	} {
		if got := Classify(Postgres{}, tc.sql).Allowed(); got != tc.allowed {
			t.Errorf("Allowed(%q) = %v, want %v", tc.sql, got, tc.allowed)
		}
	}
}

func TestSchemaChangingStatements(t *testing.T) {
	for sql, want := range map[string]bool{"CREATE TABLE t (a int)": true, "DROP VIEW v": true, "ALTER TABLE t ADD c int": true, "SELECT 1": false, "INSERT INTO t VALUES (1)": false} {
		if got := Classify(Postgres{}, sql).Schema; got != want {
			t.Errorf("Schema(%q) = %v, want %v", sql, got, want)
		}
	}
}

// TestClassifyNeverDiffersByCaseOrComments checks that comments and the case of the words change nothing in what a statement is.
func TestClassifyNeverDiffersByCaseOrComments(t *testing.T) {
	for _, sql := range []string{"DELETE FROM t", "SELECT 1", "DROP TABLE x", "UPDATE t SET a = 1 WHERE b = 2", "INSERT INTO t VALUES (1)"} {
		want := Classify(Postgres{}, sql)
		for _, v := range []string{strings.ToLower(sql), "/* c */ " + sql, "-- c\n" + sql, "  \n" + sql + " -- c"} {
			got := Classify(Postgres{}, v)
			if got.Class != want.Class || got.Destructive != want.Destructive || got.Why != want.Why {
				t.Errorf("Classify(%q) = %+v, want %+v", v, got, want)
			}
		}
	}
}
