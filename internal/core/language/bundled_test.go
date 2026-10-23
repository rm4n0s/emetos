package language

import (
	"encoding/json"
	"io/fs"
	"path"
	"regexp"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/errors"
)

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
		{"app.properties", "", "properties"},
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
	for _, id := range []string{"go", "json", "jsonc", "jsonl", "snippets", "yaml", "markdown", "python", "javascript", "javascriptreact", "typescript", "typescriptreact", "html", "css", "scss", "less", "xml", "xsl", "sql", "shellscript", "makefile", "dockerfile", "ini", "properties"} {
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
	err := fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Base(path.Dir(p)) != "syntaxes" {
			return nil
		}
		data, err := fs.ReadFile(assets, p)
		if err != nil {
			return err
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		files++
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
					patterns++
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
	if files < 15 || patterns < 2000 {
		t.Errorf("only %d grammar files and %d patterns were found", files, patterns)
	}
}

func TestAddExtensionsErrors(t *testing.T) {
	cases := []struct {
		name  string
		names []string
		route string
	}{
		{"a name that is not a path", []string{"../x"}, "addExtensions.BundledExtensionUnreadable"},
		{"a folder that is not there", []string{"nothing"}, "addExtensions->Registry.AddExtension.ExtensionUnreadable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := addExtensions(NewRegistry(), assets, tc.names)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
