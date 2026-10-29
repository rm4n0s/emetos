package sql

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// exportOf makes an export of a table with one row of each kind of value.
func exportOf(rows ...[]resultgrid.Value) *resultgrid.Export {
	cols := []resultgrid.Column{{Name: "id"}, {Name: "na\"me"}, {Name: "ok"}, {Name: "data"}, {Name: "at"}, {Name: "ratio"}}
	t := resultgrid.NewTable(cols)
	t.Append(rows...)
	m := resultgrid.NewModel(resultgrid.ModelOptions{})
	m.SetSource(t)
	return m.Export()
}

func TestInsertStatementsPerEngine(t *testing.T) {
	at := time.Date(2026, 10, 29, 6, 7, 8, 123_000_000, time.UTC)
	row := []resultgrid.Value{
		resultgrid.Int(7), resultgrid.Text("it's a \x00test"), resultgrid.Bool(true), resultgrid.Bytes([]byte{0xca, 0xfe}), resultgrid.Time(at), resultgrid.Float(2.5),
	}
	null := []resultgrid.Value{resultgrid.Null(), resultgrid.JSON(`{"a": 1}`), resultgrid.Bool(false), resultgrid.Null(), resultgrid.Null(), resultgrid.Float(1e21)}
	tests := []struct {
		d     dialect.Dialect
		table string
		want  string
	}{
		{dialect.Postgres{}, "shop.items", "INSERT INTO \"shop\".\"items\" (\"id\", \"na\"\"me\", \"ok\", \"data\", \"at\", \"ratio\") VALUES (7, 'it''s a test', TRUE, '\\xcafe', '2026-10-29 06:07:08.123+00:00', 2.5);\n" +
			"INSERT INTO \"shop\".\"items\" (\"id\", \"na\"\"me\", \"ok\", \"data\", \"at\", \"ratio\") VALUES (NULL, '{\"a\": 1}', FALSE, NULL, NULL, 1e+21);\n"},
		{dialect.SQLite{}, `"my table"`, "INSERT INTO \"my table\" (\"id\", \"na\"\"me\", \"ok\", \"data\", \"at\", \"ratio\") VALUES (7, 'it''s a test', 1, X'cafe', '2026-10-29 06:07:08.123', 2.5);\n" +
			"INSERT INTO \"my table\" (\"id\", \"na\"\"me\", \"ok\", \"data\", \"at\", \"ratio\") VALUES (NULL, '{\"a\": 1}', 0, NULL, NULL, 1e+21);\n"},
		{dialect.SQLServer{}, "dbo.items", "INSERT INTO [dbo].[items] ([id], [na\"me], [ok], [data], [at], [ratio]) VALUES (7, N'it''s a test', 1, 0xcafe, N'2026-10-29 06:07:08.123+00:00', 2.5);\n" +
			"INSERT INTO [dbo].[items] ([id], [na\"me], [ok], [data], [at], [ratio]) VALUES (NULL, N'{\"a\": 1}', 0, NULL, NULL, 1e+21);\n"},
	}
	for _, tc := range tests {
		t.Run(tc.d.ID(), func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteInserts(context.Background(), &buf, tc.d, tc.table, exportOf(row, null)); err != nil {
				t.Fatal(err)
			}
			if buf.String() != tc.want {
				t.Errorf("got\n%s\nwant\n%s", buf.String(), tc.want)
			}
		})
	}
	// MySQL escapes with backslashes
	var buf bytes.Buffer
	if err := WriteInserts(context.Background(), &buf, dialect.MySQL{}, "t", exportOf([]resultgrid.Value{resultgrid.Int(1), resultgrid.Text(`a\b'c`), resultgrid.Bool(true), resultgrid.Bytes([]byte{1}), resultgrid.Null(), resultgrid.Null()})); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "VALUES (1, CONVERT(0x615c622763 USING utf8mb4), TRUE, X'01', NULL, NULL);") {
		t.Errorf("mysql = %q", buf.String())
	}
}

func TestInsertStatementsStopOnCancelAndWriteFailures(t *testing.T) {
	big := make([][]resultgrid.Value, 300)
	for i := range big {
		big[i] = []resultgrid.Value{resultgrid.Int(int64(i)), resultgrid.Text(strings.Repeat("x", 50)), resultgrid.Bool(true), resultgrid.Null(), resultgrid.Null(), resultgrid.Null()}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err := WriteInserts(cancelled, &bytes.Buffer{}, dialect.Postgres{}, "t", exportOf(big...))
	wantRoute(t, err, "Export.Each.ResultExportCancelled")
	// the buffer flushes at the end of a small result, and in the middle of a big one
	wantRoute(t, WriteInserts(context.Background(), &failingWriter{}, dialect.Postgres{}, "t", exportOf(big[:1]...)), "WriteInserts.SQLExportWriteFailed")
	wantRoute(t, WriteInserts(context.Background(), &failingWriter{}, dialect.Postgres{}, "t", exportOf(big...)), "WriteInserts.func1.SQLExportWriteFailed")
}

// failingWriter fails every write.
type failingWriter struct{}

func (*failingWriter) Write([]byte) (int, error) { return 0, errors.New("DiskFull", "no space left") }

func TestWritingAnExportFile(t *testing.T) {
	exp := exportOf([]resultgrid.Value{resultgrid.Int(1), resultgrid.Text("a"), resultgrid.Bool(true), resultgrid.Null(), resultgrid.Null(), resultgrid.Null()})
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sql")
	if err := writeExport(context.Background(), path, formatSQL, dialect.SQLite{}, "t", exp); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.HasPrefix(string(data), `INSERT INTO "t"`) {
		t.Errorf("file = %q, %v", data, err)
	}
	// a folder that is not there
	wantRoute(t, writeExport(context.Background(), filepath.Join(dir, "no", "such", "out.csv"), formatCSV, dialect.SQLite{}, "", exp), "writeExport.SQLExportFileFailed")
	// a file that is half written is removed
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	half := filepath.Join(dir, "half.json")
	wantRoute(t, writeExport(cancelled, half, formatJSON, dialect.SQLite{}, "", exportOf(make([][]resultgrid.Value, 600)...)), "Export.WriteJSON.ResultExportCancelled")
	if _, err := os.Stat(half); !os.IsNotExist(err) {
		t.Errorf("the half written file stays: %v", err)
	}
}

func TestGuessingTheTableOfAnExport(t *testing.T) {
	for stmt, want := range map[string]string{
		"SELECT * FROM books":                          "books",
		"select id from public.books b where x":        "public.books",
		`SELECT * FROM "My Schema"."my table" LIMIT 5`: `"My Schema"."my table"`,
		"SELECT * FROM `shop`.`orders`":                "`shop`.`orders`",
		"SELECT 1":                                     "table_name",
		"WITH x AS (SELECT 1) SELECT * FROM x":         "x",
	} {
		if got := tableOf(stmt); got != want {
			t.Errorf("tableOf(%q) = %q, want %q", stmt, got, want)
		}
	}
}
