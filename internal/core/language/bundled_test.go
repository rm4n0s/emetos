package language

import (
	"encoding/json"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/language/packfs"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Bundled makes a registry of its own with the languages of the language packs, which live under extensions/ (SPEC §8.1,
// §10.15) and are read from the source tree in the order the editor registers them. The tests of other packages use
// languagetest.Bundled, which cannot be imported here.
func Bundled() (*Registry, error) {
	r := NewRegistry()
	list, err := packfs.All()
	if err != nil {
		return nil, err
	}
	for _, fsys := range list {
		if err := r.AddExtension(fsys); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// backReferences are the references to the captures of a begin in an end or while pattern.
var backReferences = regexp.MustCompile(`\\(\d+)`)

func TestBundledRegistry(t *testing.T) {
	r, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.Problems() {
		t.Errorf("problem: %v", p)
	}
	cases := []struct {
		file, firstLine, want string
	}{
		{"main.go", "", "go"},
		{"a.json", "", "json"},
		{"settings.jsonc", "", "jsonc"},
		{"babel.config.json", "", "jsonc"},
		{"log.jsonl", "", "jsonl"},
		{"a.code-snippets", "", "snippets"},
		{"ci.yaml", "", "yaml"},
		{"ci.yml", "", "yaml"},
		{"user-data", "#cloud-config", "yaml"},
		{"README.md", "", "markdown"},
		{"compose.yaml", "", "dockercompose"},
		{"app.py", "", "python"},
		{"deploy", "#!/usr/bin/env python3", "python"},
		{"SConstruct", "", "python"},
		{"app.js", "", "javascript"},
		{"app.mjs", "", "javascript"},
		{"run", "#!/usr/bin/env node", "javascript"},
		{"app.jsx", "", "javascriptreact"},
		{"app.ts", "", "typescript"},
		{"app.mts", "", "typescript"},
		{"app.tsx", "", "typescriptreact"},
		{"tsconfig.json", "", "jsonc"},
		{"index.html", "", "html"},
		{"page.htm", "", "html"},
		{"page.xhtml", "", "html"},
		{"site.css", "", "css"},
		{"site.scss", "", "scss"},
		{"site.less", "", "less"},
		{"data.xml", "", "xml"},
		{"icon.svg", "", "xml"},
		{"pom.xsd", "", "xml"},
		{"style.xsl", "", "xsl"},
		{"query.sql", "", "sql"},
		{"build.sh", "", "shellscript"},
		{"build.bash", "", "shellscript"},
		{"zshrc", "", "shellscript"},
		{".bashrc", "", "shellscript"},
		{".zshrc", "", "shellscript"},
		{"PKGBUILD", "", "shellscript"},
		{"run", "#!/bin/bash", "shellscript"},
		{"run2", "#!/usr/bin/env bash", "shellscript"},
		{"Makefile", "", "makefile"},
		{"makefile", "", "makefile"},
		{"GNUmakefile", "", "makefile"},
		{"rules.mk", "", "makefile"},
		{"Dockerfile", "", "dockerfile"},
		{"Dockerfile.dev", "", "dockerfile"},
		{"app.dockerfile", "", "dockerfile"},
		{"Containerfile", "", "dockerfile"},
		{"settings.ini", "", "ini"},
		{"session.redis", "", "redis"},
		{"app.properties", "", "properties"},
		{"main.c", "", "c"},
		{"defs.h", "", "cpp"}, // as in VS Code, .h is C++
		{"main.cpp", "", "cpp"},
		{"main.cc", "", "cpp"},
		{"main.cxx", "", "cpp"},
		{"defs.hpp", "", "cpp"},
		{"kernel.cu", "", "cuda-cpp"},
		{"lib.rs", "", "rust"},
		{"change.diff", "", "diff"},
		{"change.patch", "", "diff"},
		{"COMMIT_EDITMSG", "", "git-commit"},
		{"MERGE_MSG", "", "git-commit"},
		{"git-rebase-todo", "", "git-rebase"},
		{".gitignore", "", "ignore"},
		{".gitignore_global", "", "ignore"},
		{".git-blame-ignore-revs", "", "ignore"},
		{"go.mod", "", "go.mod"},
		{"go.work", "", "go.work"},
		{"go.sum", "", "go.sum"},
		{"orders.mongodb", "", "mongodb"},
		{"notes.txt", "", "plaintext"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			if got := r.Detect(tc.file, tc.firstLine); got != tc.want {
				t.Errorf("Detect(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
	if !r.HasGrammar("go") || r.HasGrammar("plaintext") || r.HasGrammar("nope") {
		t.Error("HasGrammar tells the languages with a grammar")
	}
	// A line of a few bytes is one token when it is the limit.
	g, err := r.NewGrammarWith("go", GrammarSettings{MaxLineBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if res := g.TokenizeLine("package main", nil, 0); len(res.Tokens) != 1 {
		t.Errorf("a line over the limit is one token: %+v", res.Tokens)
	}
	// Every language with a grammar makes a tokenizer.
	for _, id := range []string{"go", "json", "jsonc", "jsonl", "snippets", "yaml", "markdown", "python", "javascript", "javascriptreact", "typescript", "typescriptreact", "html", "css", "scss", "less", "xml", "xsl", "sql", "shellscript", "makefile", "dockerfile", "ini", "properties", "c", "cpp", "cuda-cpp", "rust", "diff", "git-commit", "git-rebase", "ignore", "go.mod", "go.work", "go.sum", "mongodb", "redis"} {
		if _, err := r.NewGrammar(id, nil); err != nil {
			t.Errorf("NewGrammar(%q): %v", id, err)
		}
		if r.Configuration(id) == nil {
			t.Errorf("%q has no language configuration", id)
		}
	}
}

func TestBundledRegistriesAreIndependent(t *testing.T) {
	a, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetFileAssociations(map[string]any{"*.foo": "go"}); len(err) != 0 {
		t.Fatal(err)
	}
	if got := a.Detect("x.foo", ""); got != "go" {
		t.Errorf("a: %q", got)
	}
	if got := b.Detect("x.foo", ""); got == "go" {
		t.Error("an association of one registry reached another")
	}
}

// TestEveryBundledGrammarCompiles compiles every pattern of every grammar file of the assets,
// not only those the fixtures reach.
func TestEveryBundledGrammarCompiles(t *testing.T) {
	cache := regex.NewCache(1 << 20)
	files := 0
	patterns := 0
	packs, err := packfs.All()
	if err != nil {
		t.Fatal(err)
	}
	for i, fsys := range packs {
		walkGrammars(t, fsys, packfs.Names()[i], cache, &files, &patterns)
	}
	if files < 15 || patterns < 2000 {
		t.Errorf("only %d grammar files and %d patterns were found", files, patterns)
	}
}

// walkGrammars compiles every pattern of the grammar files of one pack.
func walkGrammars(t *testing.T, assets fs.FS, pack string, cache *regex.Cache, files, patterns *int) {
	t.Helper()
	err := fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Base(path.Dir(p)) != "syntaxes" {
			return nil
		}
		data, err := fs.ReadFile(assets, p)
		p = pack + "/" + p
		if err != nil {
			return err
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		*files++
		var walk func(v any)
		walk = func(v any) {
			switch v := v.(type) {
			case map[string]any:
				for k, x := range v {
					s, isString := x.(string)
					if !isString || (k != "match" && k != "begin" && k != "end" && k != "while") {
						walk(x)
						continue
					}
					*patterns++
					if k == "end" || k == "while" {
						// The captures of the begin are written into these when they are used.
						s = backReferences.ReplaceAllString(s, "x")
					}
					if _, err := cache.Compile(s, regex.Options{}); err != nil {
						t.Errorf("%s: %q: %v", p, s, err)
					}
				}
			case []any:
				for _, x := range v {
					walk(x)
				}
			}
		}
		walk(doc)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestTheRedisGrammarScopesACommandFile tokenizes the lines of a .redis file (step 21.2): the command, the quoted arguments, the
// options, the numbers and the comments.
func TestTheRedisGrammarScopesACommandFile(t *testing.T) {
	r, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	g, err := r.NewGrammar("redis", nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		line string
		want []string // text|last scope
	}{
		{`# a comment`, []string{"#|punctuation.definition.comment.redis", " a comment|comment.line.number-sign.redis"}},
		{`SET greeting "hello \"you\"" EX 60`, []string{
			"SET|entity.name.function.command.redis", `"|punctuation.definition.string.begin.redis`, `hello |string.quoted.double.redis`,
			`\"|constant.character.escape.redis`, `you|string.quoted.double.redis`, `\"|constant.character.escape.redis`,
			`"|punctuation.definition.string.end.redis`, "EX|constant.language.option.redis", "60|constant.numeric.redis"}},
		{`  zadd scores 1.5 'a b' NX`, []string{
			"zadd|entity.name.function.command.redis", "1.5|constant.numeric.redis", "'|punctuation.definition.string.begin.redis",
			"a b|string.quoted.single.redis", "'|punctuation.definition.string.end.redis", "NX|constant.language.option.redis"}},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			var got []string
			res := g.TokenizeLine(tc.line, nil, 0)
			for _, tok := range res.Tokens {
				names := tok.Scopes.Names()
				text := tc.line[tok.Start:tok.End]
				if names[len(names)-1] == "source.redis" {
					continue // text the grammar does not scope: the key and white space
				}
				got = append(got, text+"|"+names[len(names)-1])
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("tokens\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}
