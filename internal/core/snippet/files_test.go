package snippet

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func names(list []*Snippet) []string {
	var out []string
	for _, s := range list {
		out = append(out, s.Name+":"+s.Prefix)
	}
	return out
}

func TestParseFile(t *testing.T) {
	tests := []struct {
		name string
		path string
		text string
		want []string
	}{
		{"object per snippet", "go.json", `{"a": {"prefix": "x", "body": "one"}, "b": {"prefix": "y", "body": ["l1", "l2"]}}`, []string{"a:x", "b:y"}},
		{"prefix array", "go.json", `{"a": {"prefix": ["x", "z"], "body": "one"}}`, []string{"a:x", "a:z"}},
		{"empty prefix array has none", "go.json", `{"a": {"prefix": [], "body": "one"}}`, nil},
		{"no prefix", "go.json", `{"a": {"body": "one"}}`, []string{"a:"}},
		{"comments and trailing comma", "go.json", "{\n// c\n\"a\": {\"prefix\": \"x\", \"body\": \"b\",},\n}", []string{"a:x"}},
		{"no body is not a snippet", "go.json", `{"a": {"prefix": "x"}, "b": {"prefix": "y", "body": ""}}`, nil},
		{"body of numbers", "go.json", `{"a": {"prefix": "x", "body": 1}}`, nil},
		{"group", "my.code-snippets", `{"a": {"c": {"prefix": "x", "body": "b"}}}`, []string{"c:x"}},
		{"not an object", "go.json", `[1, 2]`, nil},
		{"empty", "go.json", ``, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := names(ParseFile(tc.path, tc.text).Snippets)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseFileFields(t *testing.T) {
	f := ParseFile("/c/x.code-snippets", `{"a": {"prefix": "Pre", "body": ["a", "b"], "description": ["d1", "d2"],
		"scope": "go, javascript,", "include": "*.test.go", "exclude": ["a/**"], "isFileTemplate": true}}`)
	if len(f.Snippets) != 1 {
		t.Fatalf("got %d snippets", len(f.Snippets))
	}
	s := f.Snippets[0]
	if s.Body != "a\nb" || s.Description != "d1\nd2" || s.PrefixLow != "pre" || !s.IsFileTemplate {
		t.Errorf("fields: %+v", s)
	}
	if !slices.Equal(s.Scopes, []string{"go", "javascript"}) {
		t.Errorf("scopes: %v", s.Scopes)
	}
	if !slices.Equal(s.Include, []string{"*.test.go"}) || !slices.Equal(s.Exclude, []string{"a/**"}) {
		t.Errorf("include/exclude: %v %v", s.Include, s.Exclude)
	}
	if s.Identifier != "x.code-snippets/a" || s.Source != "Global User Snippet" || !f.IsGlobal {
		t.Errorf("identity: %q %q", s.Identifier, s.Source)
	}
}

func TestFileSelect(t *testing.T) {
	global := ParseFile("g.code-snippets", `{
		"any": {"prefix": "a", "body": "x"},
		"go": {"prefix": "b", "body": "x", "scope": "go"},
		"js": {"prefix": "c", "body": "x", "scope": "javascript,typescript"},
		"dotted": {"prefix": "d", "body": "x", "scope": "source"}}`)
	lang := ParseFile("go.json", `{"l": {"prefix": "e", "body": "x", "scope": "ignored"}}`)
	tests := []struct {
		name string
		file *File
		id   string
		want []string
	}{
		{"global go", global, "go", []string{"any:a", "go:b"}},
		{"global typescript", global, "typescript", []string{"any:a", "js:c"}},
		{"global parent scope", global, "source.go", []string{"any:a", "dotted:d"}},
		{"language file", lang, "go", []string{"l:e"}},
		{"language file other", lang, "rust", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := names(tc.file.Select(tc.id, nil))
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSnippetInsights(t *testing.T) {
	tests := []struct {
		name                                 string
		body                                 string
		code                                 string
		bogus, trivial, clipboard, selection bool
	}{
		{"plain", "hello", "hello", false, true, false, false},
		{"final only at end", "a$0", "a$0", false, true, false, false},
		{"final in middle", "a$0b", "a$0b", false, false, false, false},
		{"placeholder", "a${1:b}", "a${1:b}", false, false, false, false},
		{"unknown variable", "${foo} ${foo} ${bar}", "${1:foo} ${1:foo} ${2:bar}", true, true, false, false},
		{"known variable", "${TM_FILENAME}", "${TM_FILENAME}", false, true, false, false},
		{"clipboard", "${CLIPBOARD}", "${CLIPBOARD}", false, true, true, false},
		{"selection", "${TM_SELECTED_TEXT:x}", "${TM_SELECTED_TEXT:x}", false, true, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Snippet{Body: tc.body}
			if s.CodeSnippet() != tc.code || s.IsBogus() != tc.bogus || s.IsTrivial() != tc.trivial ||
				s.NeedsClipboard() != tc.clipboard || s.UsesSelection() != tc.selection {
				t.Errorf("got %q bogus=%v trivial=%v clip=%v sel=%v", s.CodeSnippet(), s.IsBogus(), s.IsTrivial(), s.NeedsClipboard(), s.UsesSelection())
			}
		})
	}
}

func TestIsFileIncluded(t *testing.T) {
	tests := []struct {
		name             string
		include, exclude []string
		path             string
		want             bool
	}{
		{"no limits", nil, nil, "/a/b.go", true},
		{"include name", []string{"*_test.go"}, nil, "/a/b_test.go", true},
		{"include name misses", []string{"*_test.go"}, nil, "/a/b.go", false},
		{"include case", []string{"*.GO"}, nil, "/a/b.go", true},
		{"exclude wins", []string{"*.go"}, []string{"b.go"}, "/a/b.go", false},
		{"exclude path", nil, []string{"**/vendor/**"}, "/a/vendor/b.go", false},
		{"path pattern", []string{"**/src/*.go"}, nil, "/a/src/b.go", true},
		{"empty include list", []string{}, nil, "/a/b.go", false},
		{"empty pattern is skipped", []string{""}, []string{""}, "/a/b.go", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Snippet{Include: tc.include, Exclude: tc.exclude}
			if got := s.IsFileIncluded(tc.path); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func writeSnippets(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestServiceReload(t *testing.T) {
	dir := t.TempDir()
	writeSnippets(t, dir, "go.json", `{"zeta": {"prefix": "fn", "body": "func"}, "alpha": {"prefix": "if", "body": "if"}, "np": {"body": "x"}}`)
	writeSnippets(t, dir, "all.code-snippets", `{"g": {"prefix": "log", "body": "log", "exclude": "*.md"}}`)
	writeSnippets(t, dir, "notes.txt", `{"t": {"prefix": "t", "body": "t"}}`)
	svc := NewService(dir)
	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := names(svc.Snippets(Query{LanguageID: "go"})); !slices.Equal(got, []string{"g:log", "alpha:if", "zeta:fn"}) {
		t.Errorf("go: %v", got)
	}
	if got := names(svc.Snippets(Query{LanguageID: "go", Path: "/p/a.md"})); !slices.Equal(got, []string{"alpha:if", "zeta:fn"}) {
		t.Errorf("md excluded: %v", got)
	}
	if got := names(svc.Snippets(Query{LanguageID: "go", IncludeNoPrefix: true})); len(got) != 4 {
		t.Errorf("with no prefix: %v", got)
	}
	if got := names(svc.Snippets(Query{})); len(got) != 3 {
		t.Errorf("all: %v", got)
	}
	svc.SetDisabled("go.json/alpha")
	if got := names(svc.Snippets(Query{LanguageID: "go"})); !slices.Equal(got, []string{"g:log", "zeta:fn"}) {
		t.Errorf("disabled: %v", got)
	}
	if got := names(svc.Snippets(Query{LanguageID: "go", IncludeDisabled: true})); len(got) != 3 {
		t.Errorf("disabled kept: %v", got)
	}
	yes := true
	if got := svc.Snippets(Query{LanguageID: "go", FileTemplates: &yes}); len(got) != 0 {
		t.Errorf("templates: %v", names(got))
	}

	// a changed file shows after a reload
	writeSnippets(t, dir, "go.json", `{"new": {"prefix": "n", "body": "n"}}`)
	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := names(svc.Snippets(Query{LanguageID: "go"})); !slices.Equal(got, []string{"g:log", "new:n"}) {
		t.Errorf("reloaded: %v", got)
	}
}

func TestServiceSetDir(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeSnippets(t, a, "go.json", `{"one": {"prefix": "one", "body": "1"}}`)
	writeSnippets(t, b, "go.json", `{"two": {"prefix": "two", "body": "2"}}`)
	notDir := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	svc := NewService(a)
	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		dir     string
		want    []string
		wantTag string
	}{
		{"another folder replaces the snippets", b, []string{"two:two"}, ""},
		{"a folder that is not there has none", filepath.Join(t.TempDir(), "none"), nil, ""},
		{"a file is not a folder: the old snippets are gone anyway", notDir, nil, "SnippetDirReadFailed"},
		{"the first folder again", a, []string{"one:one"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.SetDir(context.Background(), tt.dir)
			if tt.wantTag != "" {
				appErr, ok := errors.FromError(err)
				if !ok || !appErr.HasRoute("Service.SetDir->Service.Reload.SnippetDirReadFailed") && !appErr.HasRoute("Service.Reload.SnippetDirReadFailed") {
					t.Fatalf("unexpected failure path: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if svc.Dir() != tt.dir {
				t.Errorf("Dir = %q, want %q", svc.Dir(), tt.dir)
			}
			if got := names(svc.Snippets(Query{LanguageID: "go"})); !slices.Equal(got, tt.want) {
				t.Errorf("snippets %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceMissingDir(t *testing.T) {
	svc := NewService(filepath.Join(t.TempDir(), "none"))
	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := svc.Snippets(Query{}); len(got) != 0 {
		t.Errorf("got %v", names(got))
	}
}

func TestServiceReloadFailures(t *testing.T) {
	t.Run("dir is a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		err := NewService(path).Reload(context.Background())
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("expected *errors.Error, got %T", err)
		}
		if !appErr.HasRoute("Service.Reload.SnippetDirReadFailed") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
	})
	t.Run("unreadable file", func(t *testing.T) {
		dir := t.TempDir()
		writeSnippets(t, dir, "a.json", `{"a": {"prefix": "a", "body": "a"}}`)
		writeSnippets(t, dir, "b.json", `{"b": {"prefix": "b", "body": "b"}}`)
		if err := os.Chmod(filepath.Join(dir, "a.json"), 0); err != nil {
			t.Fatal(err)
		}
		svc := NewService(dir)
		err := svc.Reload(context.Background())
		if os.Geteuid() == 0 {
			t.Skip("root reads anything")
		}
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("expected *errors.Error, got %T", err)
		}
		if !appErr.HasRoute("Service.Reload.SnippetFileReadFailed") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
		if got := names(svc.Snippets(Query{})); !slices.Equal(got, []string{"b:b"}) {
			t.Errorf("the readable file is still loaded: %v", got)
		}
	})
}

func TestNonWhitespacePrefix(t *testing.T) {
	long := string(make([]rune, 0))
	for range 120 {
		long += "a"
	}
	tests := []struct {
		name   string
		line   string
		column int
		want   string
	}{
		{"word", "foo bar", 7, "bar"},
		{"cursor inside", "foo bar", 5, "b"},
		{"after space", "foo ", 4, ""},
		{"tab", "a\tb", 3, "b"},
		{"whole line", "abc", 3, "abc"},
		{"too long", long, len(long), ""},
		{"long but spaced", "x " + long[:90], 92, long[:90]},
		{"column past end", "ab", 10, "ab"},
		{"unicode", "é é", len("é é"), "é"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NonWhitespacePrefix(tc.line, tc.column); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTabCompletions(t *testing.T) {
	list := []*Snippet{{Name: "a", Prefix: "fn"}, {Name: "b", Prefix: "fun"}, {Name: "c", Prefix: "n"}}
	tests := []struct {
		name      string
		before    string
		selected  string
		selection bool
		multiline bool
		want      []string
	}{
		{"suffix match", "  fn", "", false, false, []string{"a:fn", "c:n"}},
		{"exact word", "x fun", "", false, false, []string{"b:fun", "c:n"}},
		{"prefix not at the end", "fnx", "", false, false, nil},
		{"only whitespace", "fn ", "", false, false, nil},
		{"selection equals", "", "fn", true, false, []string{"a:fn"}},
		{"selection differs", "", "f", true, false, nil},
		{"multiline selection", "", "fn", true, true, nil},
		{"long selection", "", string(make([]rune, 101)), true, false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := names(TabCompletions(list, tc.before, tc.selected, tc.selection, tc.multiline))
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
