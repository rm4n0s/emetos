package sql

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// completionAt asks the provider of the editor for the items at the marker "|" in the text, which is taken out of the text.
func completionAt(t *testing.T, e *editorRig, text string) map[string]extension.CompletionItem {
	t.Helper()
	off := strings.Index(text, "|")
	text = strings.Replace(text, "|", "", 1)
	e.api.SetDocument(e.uri, text)
	var p extension.CompletionProvider
	for _, reg := range e.api.Providers() {
		if reg.Feature == "completion" && !reg.Disposed {
			p = reg.Provider.(extension.CompletionProvider)
		}
	}
	if p == nil {
		t.Fatal("no completion provider")
	}
	line := strings.Count(text[:off], "\n")
	col := off - (strings.LastIndexByte(text[:off], '\n') + 1)
	list, err := p.ProvideCompletion(context.Background(), extension.TextDocument{URI: e.uri, LanguageID: "sql", Text: text}, extension.Position{Line: line, Col: col}, extension.CompletionTrigger{Kind: "invoked"})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]extension.CompletionItem{}
	for _, it := range list.Items {
		if _, dup := out[it.Label]; !dup {
			out[it.Label] = it
		}
	}
	return out
}

func labelsOf(items map[string]extension.CompletionItem, kind extension.CompletionKind) []string {
	var out []string
	for l, it := range items {
		if it.Kind == kind {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}

func TestCompletionOfKeywordsTablesAndColumns(t *testing.T) {
	e := newEditorRig(t, "", true)
	tests := []struct {
		name  string
		text  string
		kind  extension.CompletionKind
		want  []string
		other []string // labels that must not be there
	}{
		{"tables after FROM", "SELECT * FROM |", extension.CompletionStruct, []string{"authors", "books", "log", "tags"}, nil},
		{"views after JOIN", "SELECT * FROM books JOIN |", extension.CompletionInterface, []string{"book_titles"}, nil},
		{"schemas after FROM", "SELECT * FROM |", extension.CompletionModule, []string{"main"}, nil},
		{"the tables of a schema after its name", "SELECT * FROM main.|", extension.CompletionStruct, []string{"authors", "books", "log", "tags"}, nil},
		{"the columns of a table named in the statement", "SELECT | FROM authors", extension.CompletionField, []string{"born", "name"}, nil},
		{"the key columns are told apart", "SELECT | FROM authors", extension.CompletionConstant, []string{"id"}, nil},
		{"the columns of an alias", "SELECT b.| FROM books b JOIN authors a ON a.id = b.author_id", extension.CompletionField, []string{"author_id", "isbn", "price", "title"}, []string{"name", "born"}},
		{"the columns of the other alias", "SELECT a.| FROM books b JOIN authors a ON a.id = b.author_id", extension.CompletionField, []string{"born", "name"}, []string{"title"}},
		{"the columns of a table by its name", "SELECT books.| FROM books", extension.CompletionField, []string{"author_id", "isbn", "price", "title"}, nil},
		{"the columns of a quoted table", "SELECT \"authors\".| FROM \"authors\"", extension.CompletionField, []string{"born", "name"}, nil},
		{"in a WHERE of an update", "UPDATE authors SET name = 'x' WHERE |", extension.CompletionField, []string{"born", "name"}, nil},
		{"keywords after SELECT", "SELECT |", extension.CompletionKeyword, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items := completionAt(t, e, tc.text)
			got := labelsOf(items, tc.kind)
			if tc.kind == extension.CompletionKeyword {
				for _, kw := range []string{"SELECT", "WHERE", "PRAGMA"} {
					if _, ok := items[kw]; !ok {
						t.Errorf("no keyword %s", kw)
					}
				}
				return
			}
			for _, w := range tc.want {
				if !slices.Contains(got, w) {
					t.Errorf("%s is not offered; got %v", w, got)
				}
			}
			for _, o := range tc.other {
				if slices.Contains(got, o) {
					t.Errorf("%s is offered, got %v", o, got)
				}
			}
		})
	}
	// keywords are not offered after a dot
	if items := completionAt(t, e, "SELECT books.|"); items["SELECT"].Label != "" {
		t.Error("a keyword after a dot")
	}
}

func TestCompletionLearnsOfTheTablesYouCreate(t *testing.T) {
	e := newEditorRig(t, "CREATE TABLE brand_new (a INTEGER)", true)
	if items := completionAt(t, e, "SELECT * FROM |"); items["brand_new"].Label != "" {
		t.Fatal("the table is there before it exists")
	}
	e.api.SetDocument(e.uri, "CREATE TABLE brand_new (a INTEGER)")
	e.cursorAt("CREATE")
	e.run(RunQuery)
	// the CREATE made the catalog forget what it knew
	if items := completionAt(t, e, "SELECT * FROM |"); items["brand_new"].Label == "" {
		t.Error("the new table is not offered")
	}
}

func TestCompletionWithoutAConnectionOffersKeywords(t *testing.T) {
	e := newEditorRig(t, "", false)
	items := completionAt(t, e, "SEL|")
	if items["SELECT"].Label == "" || len(labelsOf(items, extension.CompletionStruct)) != 0 {
		t.Errorf("items = %d", len(items))
	}
	// a connection that is not open offers the keywords of its engine
	e.ext.q.setBinding(e.uri, binding{Connection: e.conn.ID})
	if err := e.hub.Service().Close(e.conn.ID); err != nil {
		t.Fatal(err)
	}
	items = completionAt(t, e, "SEL|")
	if items["SELECT"].Label == "" || items["PRAGMA"].Label == "" {
		t.Error("no keywords of the engine")
	}
}

func TestTheContextOfTheCursor(t *testing.T) {
	tests := []struct {
		text      string
		prefix    string
		qualifier string
		afterFrom bool
	}{
		{"SELECT * FROM |", "", "", true},
		{"SELECT * FROM au|", "au", "", true},
		{"select * from\n  join_me join |", "", "", true},
		{"SELECT a.na|", "na", "a", false},
		{"SELECT \"my table\".|", "", "my table", false},
		{"SELECT |", "", "", false},
		{"SELECT x, y|", "y", "", false},
		{"UPDATE |", "", "", true},
		{"INSERT INTO |", "", "", true},
		{"", "", "", false},
	}
	for _, tc := range tests {
		off := strings.Index(tc.text, "|")
		stmt := strings.Replace(tc.text, "|", "", 1)
		if off < 0 {
			off = len(stmt)
		}
		got := analyze(stmt, off)
		if got.prefix != tc.prefix || got.qualifier != tc.qualifier || got.afterFrom != tc.afterFrom {
			t.Errorf("analyze(%q) = %+v", tc.text, got)
		}
	}
}

func TestTheTablesAStatementMentions(t *testing.T) {
	tests := []struct {
		sql  string
		want []reference
	}{
		{"SELECT * FROM books", []reference{{name: "books"}}},
		{"SELECT * FROM books b", []reference{{name: "books", alias: "b"}}},
		{"SELECT * FROM books AS b JOIN public.authors a ON 1 = 1 WHERE x", []reference{{name: "books", alias: "b"}, {schema: "public", name: "authors", alias: "a"}}},
		{"SELECT * FROM \"My Schema\".\"my table\" t", []reference{{schema: "My Schema", name: "my table", alias: "t"}}},
		{"SELECT * FROM books WHERE id = 1", []reference{{name: "books"}}},
		{"SELECT * FROM books LEFT JOIN tags USING (id)", []reference{{name: "books"}, {name: "tags"}}},
		{"UPDATE books SET a = 1", []reference{{name: "books"}}},
		{"INSERT INTO tags (a) VALUES (1)", []reference{{name: "tags"}}},
		{"SELECT 1", nil},
	}
	for _, tc := range tests {
		if got := references(tc.sql); !slices.Equal(got, tc.want) {
			t.Errorf("references(%q) = %+v, want %+v", tc.sql, got, tc.want)
		}
	}
}

func TestDefaultSchemas(t *testing.T) {
	for id, want := range map[string]string{"postgres": "public", "sqlserver": "dbo", "sqlite": "main", "mysql": ""} {
		d, _ := dialect.ByID(id)
		if got := defaultSchema(d); got != want {
			t.Errorf("defaultSchema(%s) = %q, want %q", id, got, want)
		}
	}
}

func TestTextHelpers(t *testing.T) {
	text := "SELECT 'é'\nFROM t"
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"offset of line 0 col 7", offsetOf(text, 0, 7), 7},
		{"offset of line 1 col 2", offsetOf(text, 1, 2), len("SELECT 'é'\n") + 2},
		{"offset past the end of a line", offsetOf(text, 0, 99), len("SELECT 'é'")},
		{"offset of a line past the end", offsetOf(text, 9, 0), len(text)},
		{"elapsed under a millisecond", formatElapsed(400_000), "0.4 ms"},
		{"elapsed in milliseconds", formatElapsed(12_000_000), "12 ms"},
		{"elapsed in seconds", formatElapsed(1_250_000_000), "1.25 s"},
		{"elapsed in minutes", formatElapsed(125_000_000_000), "2 min 5 s"},
		{"rows", rowsText(1, false), "1 row"},
		{"more rows", rowsText(1000, true), "1,000+ rows"},
		{"first line", firstLine("  SELECT a,\n b FROM t", 100), "SELECT a, ..."},
		{"first line cut", firstLine("SELECT abcdefghij FROM t", 10), "SELECT abc ..."},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	// the column counts UTF-16 units: 'é' is one, an emoji two
	line, col := lineColumn("a\nb 😀 c", len("a\nb 😀 "))
	if line != 2 || col != 6 {
		t.Errorf("lineColumn = %d, %d", line, col)
	}
	if l, c := lineColumn("abc", 99); l != 1 || c != 4 {
		t.Errorf("lineColumn past the end = %d, %d", l, c)
	}
}
