package language

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/errors"
)

// tokensOf tokenizes lines and writes each token as text|scopes.
func tokensOf(g *textmate.Grammar, lines ...string) [][]string {
	var out [][]string
	var state *textmate.State
	for _, line := range lines {
		res := g.TokenizeLine(line, state, 0)
		var toks []string
		for _, tok := range res.Tokens {
			toks = append(toks, line[tok.Start:tok.End]+"|"+strings.Join(tok.Scopes.Names(), " "))
		}
		out = append(out, toks)
		state = res.State
	}
	return out
}

func TestTheRegistryIsTheRegistryOfGrammars(t *testing.T) {
	var _ textmate.Registry = (*Registry)(nil)
	r := NewRegistry()
	files := map[string]string{
		"host.json":  `{"scopeName": "source.host", "patterns": [{"match": "\\w+", "name": "word"}, {"begin": "<<", "end": ">>", "name": "embedded.block.guest", "patterns": [{"include": "source.guest"}]}]}`,
		"guest.json": `{"scopeName": "source.guest", "patterns": [{"match": "#\\w+", "name": "comment.line"}, {"match": "\\d+", "name": "num"}]}`,
		"inj.json":   `{"scopeName": "inj.todo", "injectionSelector": "L:source.host", "patterns": [{"match": "TODO", "name": "todo"}]}`,
		"bad.json":   `{`,
	}
	if err := r.AddExtension(ext(`{"name": "x", "contributes": {
		"languages": [{"id": "host"}, {"id": "guest"}, {"id": "broken"}, {"id": "missing"}],
		"grammars": [
			{"language": "host", "scopeName": "source.host", "path": "./host.json", "embeddedLanguages": {"embedded.block.guest": "guest", "embedded.block.nothing": "nothing"}, "tokenTypes": {"num": "string", "word": "comment", "x": "regex", "y": "other", "z": "weird"}},
			{"language": "guest", "scopeName": "source.guest", "path": "./guest.json"},
			{"scopeName": "inj.todo", "path": "./inj.json", "injectTo": ["source.host"], "embeddedLanguages": {"todo": "guest"}},
			{"language": "broken", "scopeName": "source.broken", "path": "./bad.json"},
			{"language": "missing", "scopeName": "source.missing", "path": "./nope.json"}
		]}}`, files)); err != nil {
		t.Fatal(err)
	}
	g, err := r.NewGrammar("host", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := tokensOf(g, "ab << 12 #c >> TODO")
	want := []string{
		"ab|source.host word", " |source.host", "<<|source.host embedded.block.guest", " |source.host embedded.block.guest",
		"12|source.host embedded.block.guest num", " |source.host embedded.block.guest",
		"#c|source.host embedded.block.guest comment.line", " |source.host embedded.block.guest", ">>|source.host embedded.block.guest",
		" |source.host", "TODO|source.host todo",
	}
	if strings.Join(got[0], "\n") != strings.Join(want, "\n") {
		t.Errorf("tokens:\n%s\nwant:\n%s", strings.Join(got[0], "\n"), strings.Join(want, "\n"))
	}
	res := g.TokenizeLine("<< 12 #c >> TODO", nil, 0)
	host, guest := r.Code("host"), r.Code("guest")
	for _, tok := range res.Tokens {
		text := "<< 12 #c >> TODO"[tok.Start:tok.End]
		switch text {
		case "12":
			if tok.Language != guest || tok.Type != textmate.TokenString {
				t.Errorf("12: language %d, type %d", tok.Language, tok.Type)
			}
		case "#c":
			if tok.Language != guest || tok.Type != textmate.TokenComment {
				t.Errorf("#c: language %d, type %d", tok.Language, tok.Type)
			}
		case "TODO":
			if tok.Language != guest {
				t.Errorf("TODO: the embedded languages of an injected grammar apply: %d", tok.Language)
			}
		case "<<", ">>":
			if tok.Language != guest {
				t.Errorf("%s: language %d, want %d", text, tok.Language, guest)
			}
		}
		_ = host
	}
	if _, err := r.NewGrammar("guest", nil); err != nil {
		t.Errorf("guest: %v", err)
	}
	if _, err := r.NewGrammar("plaintext", nil); err == nil {
		t.Error("plain text has no grammar")
	} else {
		wantTag(t, err, "Registry.NewGrammarWith.GrammarNotRegistered")
	}
	if _, err := r.NewGrammar("unknown", nil); err == nil {
		t.Error("an unknown language has no grammar")
	}
	if _, err := r.NewGrammar("broken", nil); err == nil {
		t.Error("a grammar that cannot be read")
	} else {
		wantTag(t, err, "Registry.NewGrammarWith->NewGrammar.GrammarNotFound")
	}
	if _, err := r.NewGrammar("missing", nil); err == nil {
		t.Error("a grammar file that is not there")
	}
	tags := map[string]bool{}
	for _, p := range r.Problems() {
		tag, _ := errorsFrom(p)
		tags[tag] = true
	}
	if !tags["GrammarFileInvalid"] || !tags["GrammarFileUnreadable"] {
		t.Errorf("problems: %v", r.Problems())
	}
	first := r.Grammar("source.host")
	if r.Grammar("source.none") != nil || first == nil || first != r.Grammar("source.host") {
		t.Error("Grammar of a scope that is not there, and the same grammar twice")
	}
}

func TestInjectionsOfParentScopes(t *testing.T) {
	r := NewRegistry()
	if err := r.AddExtension(ext(`{"contributes": {"grammars": [
		{"scopeName": "a", "path": "a.json", "injectTo": ["source"]},
		{"scopeName": "b", "path": "b.json", "injectTo": ["source.js"]},
		{"scopeName": "c", "path": "c.json", "injectTo": ["source.js.x", "text"]}]}}`, nil)); err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{"source": "a", "source.js": "a b", "source.js.x": "a b c", "source.ts": "a", "text": "c", "other": ""}
	for scope, want := range tests {
		if got := strings.Join(r.Injections(scope), " "); got != want {
			t.Errorf("Injections(%q) = %q, want %q", scope, got, want)
		}
	}
}

func TestBundledLanguagesOfVSCode(t *testing.T) {
	dir := referenceExtensions(t)
	r := NewRegistry()
	for _, name := range []string{"go", "markdown-basics", "json", "yaml", "typescript-basics"} {
		if err := r.AddExtension(os.DirFS(filepath.Join(dir, name))); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.AddExtension(os.DirFS(filepath.Join(dir, "..", "..", "vscode-go", "extension"))); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/p/main.go": "go", "/p/go.mod": "go.mod", "/p/go.work": "go.work", "/p/go.sum": "go.sum", "/p/README.md": "markdown",
		"/p/a.json": "json", "/p/tsconfig.json": "jsonc", "/p/b.yaml": "yaml", "/p/c.tsx": "typescriptreact",
		"/p/e.unknown": PlainTextID,
	} {
		if got := r.Detect(path, ""); got != want {
			t.Errorf("Detect(%s) = %q, want %q", path, got, want)
		}
	}
	if got := r.Detect("/p/script", "#!/usr/bin/env deno"); got != "typescript" {
		t.Errorf("first line: %q", got)
	}
	g, err := r.NewGrammar("go", nil)
	if err != nil {
		t.Fatal(err)
	}
	var kw bool
	for _, line := range tokensOf(g, "package main", "func main() {", "\tx := \"hi\" // c", "}") {
		for _, tok := range line {
			if strings.Contains(tok, "keyword") {
				kw = true
			}
			if !strings.Contains(tok, "source.go") {
				t.Errorf("token outside the grammar: %s", tok)
			}
		}
	}
	if !kw {
		t.Error("no keyword in Go code")
	}
	md, err := r.NewGrammar("markdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	var state *textmate.State
	goCode := r.Code("go")
	var embedded bool
	for _, line := range []string{"# Title", "```go", "package main", "```"} {
		res := md.TokenizeLine(line, state, 0)
		state = res.State
		if line == "package main" {
			for _, tok := range res.Tokens {
				if tok.Language == goCode && strings.Contains(strings.Join(tok.Scopes.Names(), " "), "meta.embedded.block.go") {
					embedded = true
				}
			}
		}
	}
	if !embedded {
		t.Error("Go code in a Markdown block is not of the Go language")
	}
	if cfg := r.Configuration("go"); cfg.Comments == nil || cfg.Comments.LineComment != "//" {
		t.Errorf("go configuration: %+v", cfg.Comments)
	}
	if cfg := r.Configuration("go.mod"); cfg.Comments == nil {
		t.Errorf("go.mod configuration: %+v", cfg)
	}
	for _, p := range r.Problems() {
		appErr, _ := errors.FromError(p)
		t.Errorf("problem: %v %v", p, appErr.Metadata)
	}
}
