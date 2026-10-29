package sql

import (
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/rm4n0s/errors"
)

func TestWhereTheEnginesSayAnErrorIs(t *testing.T) {
	tests := []struct {
		name   string
		engine string
		err    error
		stmt   string
		want   QueryError
	}{
		{
			"PostgreSQL gives a character position", "postgres",
			&pgconn.PgError{Code: "42601", Message: `syntax error at or near "FORM"`, Position: 11, Hint: "check the spelling"},
			"SELECT id FORM authors",
			QueryError{Message: `syntax error at or near "FORM" (check the spelling)`, Code: "42601", Offset: 10, Length: 4},
		},
		{
			"PostgreSQL counts characters, not bytes", "postgres",
			&pgconn.PgError{Code: "42703", Message: `column "ñ" does not exist`, Position: 18},
			"SELECT 'é', 'ü', ñ FROM t",
			QueryError{Message: `column "ñ" does not exist`, Code: "42703", Offset: len("SELECT 'é', 'ü', "), Length: len("ñ")},
		},
		{
			"PostgreSQL with a detail and no position", "postgres",
			&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint", Detail: "Key (id)=(1) already exists."},
			"INSERT INTO t VALUES (1)",
			QueryError{Message: "duplicate key value violates unique constraint. Key (id)=(1) already exists.", Code: "23505", Offset: -1},
		},
		{
			"MySQL quotes the rest of the statement", "mysql",
			&mysql.MySQLError{Number: 1064, Message: "You have an error in your SQL syntax; check the manual that corresponds to your MySQL server version for the right syntax to use near 'FORM authors' at line 1"},
			"SELECT id FORM authors",
			QueryError{Message: "You have an error in your SQL syntax; check the manual that corresponds to your MySQL server version for the right syntax to use near 'FORM authors' at line 1", Code: "1064", Offset: 10, Length: 4},
		},
		{
			"MySQL with a line only", "mysql",
			&mysql.MySQLError{Number: 1064, Message: "You have an error in your SQL syntax; near 'gone' at line 2"},
			"SELECT 1,\n   oops",
			QueryError{Message: "You have an error in your SQL syntax; near 'gone' at line 2", Code: "1064", Offset: len("SELECT 1,\n   "), Length: 4},
		},
		{
			"MySQL with no place", "mysql",
			&mysql.MySQLError{Number: 1146, Message: "Table 'shop.nothing' doesn't exist"},
			"SELECT * FROM nothing",
			QueryError{Message: "Table 'shop.nothing' doesn't exist", Code: "1146", Offset: -1},
		},
		{
			"SQL Server gives a line", "sqlserver",
			mssql.Error{Number: 207, Message: "Invalid column name 'nope'.", LineNo: 3},
			"SELECT id,\n  name,\n  nope FROM t",
			QueryError{Message: "Invalid column name 'nope'.", Code: "207", Offset: len("SELECT id,\n  name,\n  "), Length: len("nope")},
		},
		{
			"SQL Server line past the statement", "sqlserver",
			mssql.Error{Number: 102, Message: "Incorrect syntax", LineNo: 9},
			"SELECT",
			QueryError{Message: "Incorrect syntax", Code: "102", Offset: -1},
		},
		{
			"SQLite names the token", "sqlite",
			errors.New("SQLite", `SQL logic error: near "authors": syntax error (1)`),
			"SELECT id FORM authors",
			QueryError{Message: `near "authors": syntax error`, Offset: 15, Length: 7},
		},
		{
			"SQLite names the missing table", "sqlite",
			errors.New("SQLite", "SQL logic error: no such table: nothing (1)"),
			"SELECT * FROM nothing",
			QueryError{Message: "no such table: nothing", Offset: 14, Length: 7},
		},
		{
			"SQLite gives a code of its own for a constraint", "sqlite",
			errors.New("SQLite", "constraint failed: UNIQUE constraint failed: authors.id (2067)"),
			"INSERT INTO authors (id) VALUES (1)",
			QueryError{Message: "constraint failed: UNIQUE constraint failed: authors.id", Code: "2067", Offset: -1},
		},
		{
			"SQLite without a code", "sqlite",
			errors.New("SQLite", "interrupted"),
			"SELECT 1",
			QueryError{Message: "interrupted", Offset: -1},
		},
		{
			"an error of no engine in particular", "postgres",
			errors.New("Other", "the pipe broke"),
			"SELECT 1",
			QueryError{Message: "the pipe broke", Offset: -1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := describeError(tc.engine, tc.err, tc.stmt)
			if got != tc.want {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestTheTextOfAnError(t *testing.T) {
	if got := (QueryError{Message: "bad", Code: "42"}).Text(); got != "42: bad" {
		t.Errorf("text = %q", got)
	}
	if got := (QueryError{Message: "bad"}).Text(); got != "bad" {
		t.Errorf("text = %q", got)
	}
}

func TestHowMuchOfTheStatementAnErrorMarks(t *testing.T) {
	tests := []struct {
		stmt string
		off  int
		want int
	}{
		{"SELECT nope FROM t", 7, 4},
		{`SELECT "my col" FROM t`, 7, 8},
		{"SELECT `my col` FROM t", 7, 8},
		{"SELECT 'unclosed", 7, 9},
		{"SELECT a.b FROM t", 7, 3},
		{"SELECT , FROM t", 7, 1},
		{"SELECT ñandú FROM t", 7, len("ñandú")},
		{"SELECT 1", 8, 0},
		{"SELECT 1", 99, 0},
	}
	for _, tc := range tests {
		if got := tokenLength(tc.stmt, tc.off); got != tc.want {
			t.Errorf("tokenLength(%q, %d) = %d, want %d", tc.stmt, tc.off, got, tc.want)
		}
	}
}
