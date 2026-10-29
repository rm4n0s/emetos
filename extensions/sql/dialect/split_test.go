package dialect

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// texts returns the text of each statement.
func texts(stmts []Statement) []string {
	out := make([]string, len(stmts))
	for i, s := range stmts {
		out[i] = s.Text
	}
	return out
}

func TestSplitByTheRulesOfEachDialect(t *testing.T) {
	pg, my, lite, ms := Postgres{}, MySQL{}, SQLite{}, SQLServer{}
	tests := []struct {
		name string
		d    Dialect
		src  string
		want []string
	}{
		{"one statement, no semicolon", pg, "SELECT 1", []string{"SELECT 1"}},
		{"two statements", pg, "SELECT 1; SELECT 2;", []string{"SELECT 1", "SELECT 2"}},
		{"empty pieces are not statements", pg, ";;  ;\n; SELECT 1 ;;", []string{"SELECT 1"}},
		{"only comments", pg, "-- nothing\n/* here */", nil},
		{"semicolon in a string", pg, "SELECT 'a;b'; SELECT 2", []string{"SELECT 'a;b'", "SELECT 2"}},
		{"doubled quote in a string", pg, "SELECT 'it''s;'; SELECT 2", []string{"SELECT 'it''s;'", "SELECT 2"}},
		{"semicolon in a quoted name", pg, `SELECT "a;b" FROM t; SELECT 2`, []string{`SELECT "a;b" FROM t`, "SELECT 2"}},
		{"line comment", pg, "SELECT 1 -- a; b\n; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"a comment at the end of the line of a semicolon is not the next statement's", pg, "SELECT 1; -- one\nSELECT 2; /* two */ SELECT 3", []string{"SELECT 1", "SELECT 2", "SELECT 3"}},
		{"a leading comment belongs to the statement", pg, "-- the users\nSELECT 1;", []string{"-- the users\nSELECT 1"}},
		{"block comment", pg, "SELECT /* ; */ 1; SELECT 2", []string{"SELECT /* ; */ 1", "SELECT 2"}},
		{"nested comment in PostgreSQL", pg, "SELECT /* a /* ; */ ; */ 1; SELECT 2", []string{"SELECT /* a /* ; */ ; */ 1", "SELECT 2"}},
		{"comments do not nest in MySQL", my, "SELECT /* a /* ; */ 1; SELECT 2", []string{"SELECT /* a /* ; */ 1", "SELECT 2"}},
		{"dollar quotes", pg, "SELECT $$a;b$$; SELECT 2", []string{"SELECT $$a;b$$", "SELECT 2"}},
		{"tagged dollar quotes", pg, "SELECT $q$a;$$b$q$; SELECT 2", []string{"SELECT $q$a;$$b$q$", "SELECT 2"}},
		{"a function body in dollar quotes", pg,
			"CREATE FUNCTION f() RETURNS int AS $body$ BEGIN RETURN 1; END; $body$ LANGUAGE plpgsql; SELECT f();",
			[]string{"CREATE FUNCTION f() RETURNS int AS $body$ BEGIN RETURN 1; END; $body$ LANGUAGE plpgsql", "SELECT f()"}},
		{"a parameter is not a dollar quote", pg, "SELECT $1; SELECT $2", []string{"SELECT $1", "SELECT $2"}},
		{"a dollar in a name is not a dollar quote", pg, "SELECT a$b$c; SELECT 2", []string{"SELECT a$b$c", "SELECT 2"}},
		{"E string with an escaped quote", pg, `SELECT E'it\'s;'; SELECT 2`, []string{`SELECT E'it\'s;'`, "SELECT 2"}},
		{"a backslash is nothing in a plain PostgreSQL string", pg, `SELECT 'a\'; SELECT 2`, []string{`SELECT 'a\'`, "SELECT 2"}},
		{"BEGIN ATOMIC body", pg,
			"CREATE FUNCTION f() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT 1; SELECT 2; END; SELECT 3",
			[]string{"CREATE FUNCTION f() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT 1; SELECT 2; END", "SELECT 3"}},
		{"a PostgreSQL BEGIN is a statement", pg, "BEGIN; SELECT 1; COMMIT;", []string{"BEGIN", "SELECT 1", "COMMIT"}},
		{"an unterminated string takes the rest", pg, "SELECT 1; SELECT 'a; SELECT 2", []string{"SELECT 1", "SELECT 'a; SELECT 2"}},

		{"MySQL backslash escape", my, `SELECT 'it\'s;'; SELECT 2`, []string{`SELECT 'it\'s;'`, "SELECT 2"}},
		{"MySQL double-quoted string", my, `SELECT "a;b"; SELECT 2`, []string{`SELECT "a;b"`, "SELECT 2"}},
		{"MySQL backticks", my, "SELECT `a;b` FROM t; SELECT 2", []string{"SELECT `a;b` FROM t", "SELECT 2"}},
		{"MySQL hash comment", my, "SELECT 1 # a; b\n; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"MySQL dashes need a space", my, "SELECT 1--1; SELECT 2", []string{"SELECT 1--1", "SELECT 2"}},
		{"MySQL dashes and a space", my, "SELECT 1 -- 1;\n; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"DELIMITER", my,
			"DELIMITER //\nCREATE PROCEDURE p()\nBEGIN\n  SELECT 1;\n  SELECT 2;\nEND//\nDELIMITER ;\nCALL p();",
			[]string{"CREATE PROCEDURE p()\nBEGIN\n  SELECT 1;\n  SELECT 2;\nEND", "CALL p()"}},
		{"DELIMITER $$ with the name against it", my,
			"DELIMITER $$\nCREATE PROCEDURE p() BEGIN SELECT 1; END$$\nSELECT 2$$\nDELIMITER ;\nSELECT 3;",
			[]string{"CREATE PROCEDURE p() BEGIN SELECT 1; END", "SELECT 2", "SELECT 3"}},
		{"a semicolon is nothing under another delimiter", my, "DELIMITER //\nSELECT 1; SELECT 2//\n", []string{"SELECT 1; SELECT 2"}},
		{"delimiter in lower case", my, "delimiter ;;\nSELECT 1;;\n", []string{"SELECT 1"}},

		{"SQLite brackets", lite, "SELECT [a;b] FROM t; SELECT 2", []string{"SELECT [a;b] FROM t", "SELECT 2"}},
		{"SQLite trigger body", lite,
			"CREATE TRIGGER trg AFTER INSERT ON t BEGIN UPDATE u SET n = n + 1; INSERT INTO log VALUES (1); END; SELECT 1;",
			[]string{"CREATE TRIGGER trg AFTER INSERT ON t BEGIN UPDATE u SET n = n + 1; INSERT INTO log VALUES (1); END", "SELECT 1"}},
		{"SQLite trigger with a CASE", lite,
			"CREATE TEMP TRIGGER trg AFTER INSERT ON t BEGIN UPDATE u SET n = CASE WHEN 1 THEN 2 ELSE 3 END; END; SELECT 1",
			[]string{"CREATE TEMP TRIGGER trg AFTER INSERT ON t BEGIN UPDATE u SET n = CASE WHEN 1 THEN 2 ELSE 3 END; END", "SELECT 1"}},
		{"SQLite BEGIN is a statement", lite, "BEGIN; SELECT 1; END;", []string{"BEGIN", "SELECT 1", "END"}},

		{"GO ends a batch", ms, "SELECT 1\nGO\nSELECT 2\nGO\n", []string{"SELECT 1", "SELECT 2"}},
		{"GO with a count and a comment", ms, "SELECT 1\nGO 3 -- three\nSELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"GO in lower case with spaces around it", ms, "SELECT 1\n  go  \nSELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"go inside a line is a name", ms, "SELECT go FROM t\nWHERE go = 1", []string{"SELECT go FROM t\nWHERE go = 1"}},
		{"GO in a string is text", ms, "SELECT '\nGO\n'", []string{"SELECT '\nGO\n'"}},
		{"GO followed by something is not GO", ms, "SELECT 1\nGO now\nSELECT 2", []string{"SELECT 1\nGO now\nSELECT 2"}},
		{"semicolons split a batch too", ms, "SELECT 1; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"SQL Server block", ms, "IF 1 = 1\nBEGIN\n  SELECT 1;\n  SELECT 2;\nEND; SELECT 3", []string{"IF 1 = 1\nBEGIN\n  SELECT 1;\n  SELECT 2;\nEND", "SELECT 3"}},
		{"BEGIN TRANSACTION is not a block", ms, "BEGIN TRANSACTION; SELECT 1; COMMIT;", []string{"BEGIN TRANSACTION", "SELECT 1", "COMMIT"}},
		{"BEGIN TRY", ms, "BEGIN TRY SELECT 1; END TRY BEGIN CATCH SELECT 2; END CATCH; SELECT 3", []string{"BEGIN TRY SELECT 1; END TRY BEGIN CATCH SELECT 2; END CATCH", "SELECT 3"}},
		{"CASE in a statement", ms, "SELECT CASE WHEN 1=1 THEN 'a;' END; SELECT 2", []string{"SELECT CASE WHEN 1=1 THEN 'a;' END", "SELECT 2"}},
		{"a procedure runs to the end of the batch", ms,
			"CREATE PROCEDURE p AS\nSELECT 1;\nSELECT 2;\nGO\nEXEC p",
			[]string{"CREATE PROCEDURE p AS\nSELECT 1;\nSELECT 2;", "EXEC p"}},
		{"CREATE OR ALTER", ms, "CREATE OR ALTER VIEW v AS SELECT 1;\nGO\nSELECT 2", []string{"CREATE OR ALTER VIEW v AS SELECT 1;", "SELECT 2"}},
		{"square brackets in SQL Server", ms, "SELECT [a;b]]c] FROM t; SELECT 2", []string{"SELECT [a;b]]c] FROM t", "SELECT 2"}},
		{"nested comments in SQL Server", ms, "SELECT /* a /* ; */ ; */ 1; SELECT 2", []string{"SELECT /* a /* ; */ ; */ 1", "SELECT 2"}},

		{"an empty script", pg, "", nil},
		{"white space only", pg, " \n\t ", nil},
		{"non-ASCII text", pg, "SELECT 'héllo;wörld', 'ü'; SELECT 2", []string{"SELECT 'héllo;wörld', 'ü'", "SELECT 2"}},
		{"Windows line ends", pg, "SELECT 1;\r\nSELECT 2;\r\n", []string{"SELECT 1", "SELECT 2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stmts := Split(tc.d, tc.src)
			got := texts(stmts)
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Split(%q)\n got  %q\n want %q", tc.src, got, tc.want)
			}
			for _, s := range stmts {
				if tc.src[s.Start:s.End] != s.Text {
					t.Errorf("statement %q is not src[%d:%d] = %q", s.Text, s.Start, s.End, tc.src[s.Start:s.End])
				}
			}
		})
	}
}

func TestGoCountsAndBatches(t *testing.T) {
	stmts := Split(SQLServer{}, "SELECT 1; SELECT 2\nGO 3\nSELECT 3\nGO\nSELECT 4")
	type bs struct {
		text          string
		batch, repeat int
	}
	var got []bs
	for _, s := range stmts {
		got = append(got, bs{s.Text, s.Batch, s.Repeat})
	}
	want := []bs{{"SELECT 1", 0, 3}, {"SELECT 2", 0, 3}, {"SELECT 3", 1, 1}, {"SELECT 4", 2, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTheStatementUnderTheCursor(t *testing.T) {
	src := "SELECT 1;\n\nSELECT 2 FROM t;  SELECT 3;\n\n\nSELECT 4"
	stmts := Split(Postgres{}, src)
	tests := []struct {
		name   string
		offset int
		want   string
	}{
		{"in the first", 3, "SELECT 1"},
		{"at the start of a statement", strings.Index(src, "SELECT 2"), "SELECT 2 FROM t"},
		{"at the end of a statement", strings.Index(src, "SELECT 2 FROM t") + len("SELECT 2 FROM t"), "SELECT 2 FROM t"},
		{"just after the semicolon", strings.Index(src, "SELECT 2 FROM t;") + len("SELECT 2 FROM t;"), "SELECT 2 FROM t"},
		{"two spaces after the semicolon", strings.Index(src, "SELECT 2 FROM t;") + len("SELECT 2 FROM t;  "), "SELECT 3"},
		{"on a blank line it is the next statement", strings.Index(src, "SELECT 1;") + len("SELECT 1;\n"), "SELECT 2 FROM t"},
		{"after the last", len(src), "SELECT 4"},
		{"beyond the text", len(src) + 10, "SELECT 4"},
		{"negative", -3, "SELECT 1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, ok := At(src, stmts, tc.offset)
			if !ok || st.Text != tc.want {
				t.Errorf("At(%d) = %q, %v; want %q", tc.offset, st.Text, ok, tc.want)
			}
		})
	}
	if _, ok := At("", nil, 0); ok {
		t.Error("an empty script has a statement")
	}
}

// fragments are single statements whose text holds characters that must not end them, by dialect.
var fragments = map[string][]string{
	"postgres": {
		"SELECT 1", "SELECT 'a;b'", `SELECT "x;y" FROM t`, "-- c;d\nSELECT 2", "/* ; */ SELECT 3", "SELECT $$a;b$$", "SELECT $t$ ; $$ ; $t$",
		"SELECT E'\\';'", "SELECT /* /* ; */ ; */ 4", "CREATE FUNCTION f() RETURNS int AS $f$ BEGIN RETURN 1; END; $f$ LANGUAGE plpgsql",
		"INSERT INTO t VALUES ('a''b;c')", "SELECT 'é;ü'",
	},
	"mysql": {
		"SELECT 1", "SELECT 'a;b'", "SELECT 'it\\'s;'", `SELECT "a;b"`, "SELECT `a;b` FROM t", "-- c;d\nSELECT 2", "# c;d\nSELECT 3", "/* ; */ SELECT 4",
		"INSERT INTO t VALUES ('a''b;c')",
	},
	"sqlite": {
		"SELECT 1", "SELECT 'a;b'", `SELECT "x;y" FROM t`, "SELECT [x;y] FROM t", "-- c;d\nSELECT 2", "/* ; */ SELECT 3",
		"CREATE TRIGGER t1 AFTER INSERT ON t BEGIN INSERT INTO l VALUES (1); INSERT INTO l VALUES (2); END",
	},
	"sqlserver": {
		"SELECT 1", "SELECT 'a;b'", `SELECT "x;y" FROM t`, "SELECT [x;y] FROM t", "-- c;d\nSELECT 2", "/* ; */ SELECT 3",
		"IF 1 = 1 BEGIN SELECT 1; SELECT 2; END", "SELECT CASE WHEN 1 = 1 THEN ';' END",
	},
}

// TestSplitRandomScripts builds scripts from fragments that are one statement each, joined by semicolons, white space and
// comments, and checks that Split gives the fragments back; the generator is seeded, so a failure repeats.
func TestSplitRandomScripts(t *testing.T) {
	for _, d := range All() {
		t.Run(d.ID(), func(t *testing.T) {
			pool := fragments[d.ID()]
			for seed := int64(1); seed <= 300; seed++ {
				rng := rand.New(rand.NewSource(seed))
				var want []string
				var src strings.Builder
				for n := rng.Intn(6); n >= 0; n-- {
					f := pool[rng.Intn(len(pool))]
					want = append(want, f)
					gap := []string{"", " ", "\n", "\n\n  ", "\t"}[rng.Intn(5)]
					if strings.HasPrefix(f, "--") || strings.HasPrefix(f, "#") || strings.HasPrefix(f, "/*") {
						gap += "\n" // a comment on the line of the semicolon before it is that statement's, not this one's
					}
					src.WriteString(gap)
					src.WriteString(f)
					src.WriteString([]string{";", " ;", ";\n", ";;", "; -- done\n"}[rng.Intn(5)])
				}
				got := texts(Split(d, src.String()))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("seed %d: Split(%q)\n got  %q\n want %q", seed, src.String(), got, want)
				}
			}
		})
	}
}

// TestSplitNeverFailsOnAnyText cuts random text made of the characters that matter to the lexer and checks the invariants: no panic,
// statements in order, not overlapping, each the exact text of its bounds and holding more than comments.
func TestSplitNeverFailsOnAnyText(t *testing.T) {
	alphabet := []string{"'", "\"", "`", "[", "]", "$", "$$", "$a$", "-", "--", "/*", "*/", "#", ";", "\n", " ", "GO", "go 2", "DELIMITER //", "//", "BEGIN", "END", "CASE",
		"ATOMIC", "CREATE", "TRIGGER", "PROC", "SELECT", "x", "1", "\\", "é", "(", ")", "E'"}
	for _, d := range All() {
		t.Run(d.ID(), func(t *testing.T) {
			for seed := int64(1); seed <= 500; seed++ {
				rng := rand.New(rand.NewSource(seed))
				var b strings.Builder
				for n := rng.Intn(40); n > 0; n-- {
					b.WriteString(alphabet[rng.Intn(len(alphabet))])
				}
				src := b.String()
				stmts := Split(d, src)
				prev := 0
				for _, s := range stmts {
					if s.Start < prev || s.End < s.Start || s.End > len(src) || src[s.Start:s.End] != s.Text || strings.TrimSpace(s.Text) == "" {
						t.Fatalf("seed %d: bad statement %+v in %q", seed, s, src)
					}
					prev = s.End
					Classify(d, s.Text) // must not panic either
				}
				if st, ok := At(src, stmts, rng.Intn(len(src)+3)-1); ok != (len(stmts) > 0) || (ok && st.Text == "") {
					t.Fatalf("seed %d: At gave %+v, %v for %d statements", seed, st, ok, len(stmts))
				}
			}
		})
	}
}
