package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/language/languagetest"
	"github.com/rm4n0s/emetos/internal/core/language/packfs"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// noPrograms makes PATH an empty folder: the Go extension runs go (and the host gopls) when it is activated, and a test of the
// languages must not reach the programs of the user.
func noPrograms(t *testing.T) { t.Setenv("PATH", t.TempDir()) }

// packIDs are the ids of the language packs of extensions.All, in order.
func packIDs() []string {
	var ids []string
	for _, f := range extensions.All() {
		if m := f().Manifest(); m.Contributes.Languages != nil {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// hostOver makes the host the editor makes over the extensions, with a language registry of its own.
func hostOver(t *testing.T) (*exthost.Host, *language.Registry, *settings.Registry) {
	t.Helper()
	langs := language.NewRegistry()
	defaults := settings.DefaultRegistry()
	var logged bytes.Buffer
	host := exthost.NewHost(exthost.HostOptions{
		Factories: extensions.All(), Settings: defaults, Languages: langs, Logger: slog.New(slog.NewTextHandler(&logged, nil)),
	})
	t.Cleanup(func() {
		if logged.Len() > 0 {
			t.Errorf("the host logged: %s", logged.String())
		}
	})
	return host, langs, defaults
}

// TestTheTestsReadThePacksInTheOrderOfTheEditor keeps packfs.Names, the order in which the tests of internal/ read the
// language packs from the source tree, the same as the order of the packs in extensions.All, which is the order of
// registration (and decides which language wins when two claim a file, and the number of each language).
func TestTheTestsReadThePacksInTheOrderOfTheEditor(t *testing.T) {
	var want []string
	for _, name := range packfs.Names() {
		want = append(want, "emetos."+name)
	}
	if got := packIDs(); !reflect.DeepEqual(got, want) {
		t.Errorf("extensions.All has the packs\n%v\nand packfs.Names\n%v", got, want)
	}
}

// TestTheEditorAndTheTestsMakeTheSameRegistry compares the registry the host makes from the extensions with the one the
// tests make from the files: the same languages in the same order with the same numbers, the same files detected, the
// same settings defaults and no problems.
func TestTheEditorAndTheTestsMakeTheSameRegistry(t *testing.T) {
	_, got, defaults := hostOver(t)
	want, err := languagetest.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Languages(), want.Languages()) {
		t.Errorf("languages differ:\n%v\n%v", got.Languages(), want.Languages())
	}
	for _, l := range want.Languages() {
		if got.Code(l.ID) != want.Code(l.ID) || got.HasGrammar(l.ID) != want.HasGrammar(l.ID) {
			t.Errorf("language %s: code %d/%d, grammar %v/%v", l.ID, got.Code(l.ID), want.Code(l.ID), got.HasGrammar(l.ID), want.HasGrammar(l.ID))
		}
	}
	for _, file := range []string{"a.go", "go.mod", "README.md", "a.json", "tsconfig.json", "a.ts", "a.tsx", "a.js", "defs.h", "Makefile", "Dockerfile", ".gitignore", "COMMIT_EDITMSG", "a.yml", "a.xml", "style.xsl", "a.txt"} {
		if g, w := got.Detect(file, ""), want.Detect(file, ""); g != w {
			t.Errorf("Detect(%q) = %q, want %q", file, g, w)
		}
	}
	if !reflect.DeepEqual(got.ConfigurationDefaults(), want.ConfigurationDefaults()) {
		t.Errorf("settings defaults differ:\n%v\n%v", got.ConfigurationDefaults(), want.ConfigurationDefaults())
	}
	store := settings.NewStore(defaults, settings.StoreOptions{})
	if settings.Get[bool](store, "editor.insertSpaces", "go") || settings.Get[int](store, "editor.tabSize", "yaml") != 2 {
		t.Error("the settings defaults of the packs (Go indents with tabs, YAML with two spaces) are not given")
	}
	for _, p := range got.Problems() {
		t.Errorf("problem: %v", p)
	}
}

// TestAPackFindsTheGrammarsOfOtherPacks tokenizes a Go block of a Markdown file: the grammar of one pack includes
// the grammar of another, and the embedded language is a language of a third.
func TestAPackFindsTheGrammarsOfOtherPacks(t *testing.T) {
	_, langs, _ := hostOver(t)
	md, err := langs.NewGrammar("markdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	var state *textmate.State
	var embedded bool
	for _, line := range []string{"# Title", "```go", "package main", "```"} {
		res := md.TokenizeLine(line, state, 0)
		state = res.State
		for _, tok := range res.Tokens {
			if line == "package main" && tok.Language == langs.Code("go") && strings.Contains(strings.Join(tok.Scopes.Names(), " "), "meta.embedded.block.go") {
				embedded = true
			}
		}
	}
	if !embedded {
		t.Error("Go code in a Markdown block is not of the Go language")
	}
}

// TestDisablingMarkdownMakesPlainText is the contract of step 11.4: with emetos.markdown off, .md files are plain text
// and have no grammar; every other language stays; turning it on brings Markdown back.
func TestDisablingMarkdownMakesPlainText(t *testing.T) {
	noPrograms(t)
	host, langs, defaults := hostOver(t)
	var disabled []string
	// the services a workspace always gives its extensions: the Go extension has a status item, a panel view and settings
	store := settings.NewStore(defaults, settings.StoreOptions{})
	commands := command.DefaultRegistry()
	scope := host.NewScope(exthost.ScopeOptions{
		Info:     extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "w"},
		Disabled: func() []string { return disabled },
		Commands: commands, Menus: command.DefaultMenus(commands), Settings: store, StatusBar: workbench.NewStatusBar(),
		Notify: notify.NewService(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), nil),
		Views:  workbench.NewViews(command.DefaultViews(), workbench.NewLayout(workbench.LayoutStateFromSettings(store))),
	})
	t.Cleanup(scope.Close)
	scope.Start()
	if langs.Detect("README.md", "") != "markdown" || !langs.HasGrammar("markdown") {
		t.Fatal("Markdown is not there to begin with")
	}
	if scope.State("emetos.markdown") != exthost.StateActive {
		t.Errorf("the pack is %v, want active", scope.State("emetos.markdown"))
	}
	disabled = []string{"emetos.markdown"}
	scope.Refresh()
	if got := langs.Detect("README.md", ""); got != language.PlainTextID {
		t.Errorf("README.md is %q with emetos.markdown disabled", got)
	}
	if langs.HasGrammar("markdown") || langs.IsRegistered("markdown") {
		t.Error("Markdown is still registered")
	}
	if langs.Detect("main.go", "") != "go" || langs.Detect("a.json", "") != "json" {
		t.Error("another language went with it")
	}
	disabled = nil
	scope.Refresh()
	if langs.Detect("README.md", "") != "markdown" || !langs.HasGrammar("markdown") {
		t.Error("Markdown did not come back")
	}
}

// TestAProcessKnowsEveryLanguageBeforeItsFirstWindow makes a process the way the editor does and opens files in its first
// workspace: the languages are there from the start (the files that come back or come from the command line get theirs,
// and the grammars are made for them), and a pack the user disabled is not.
func TestAProcessKnowsEveryLanguageBeforeItsFirstWindow(t *testing.T) {
	cases := []struct {
		name     string
		disabled []string
		want     map[string]string // file name -> language id of the document
	}{
		{"nothing disabled", nil, map[string]string{"main.go": "go", "README.md": "markdown", "a.json": "json"}},
		{"markdown disabled", []string{"emetos.markdown"}, map[string]string{"main.go": "go", "README.md": "plaintext", "a.json": "json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			noPrograms(t)
			paths := testutil.Isolate(t)
			extra := map[string]any{"window.menuBarVisibility": "classic"}
			if tc.disabled != nil {
				extra["extensions.disabled"] = tc.disabled
			}
			testutil.Settings(t, paths, extra)
			p := app.NewProcess(app.ProcessOptions{Paths: paths, Extensions: extensions.All(), StateAfter: storagetest.NewClock().AfterFunc, ScanTimeout: testutil.ScanTimeout})
			t.Cleanup(p.Extensions.Shutdown)
			if p.Languages.Detect("main.go", "") != "go" {
				t.Fatal("a fresh process does not know Go")
			}
			w, err := app.NewWorkspace(context.Background(), p, "")
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for file, want := range tc.want {
				path := filepath.Join(dir, file)
				if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				doc, err := w.Documents.OpenFile(context.Background(), path, document.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if got := doc.LanguageID(); got != want {
					t.Errorf("%s is %q, want %q", file, got, want)
				}
			}
			g, err := p.Grammar("go", 0)
			if err != nil || g == nil {
				t.Errorf("the grammar of Go: %v, %v", g, err)
			}
			if g, err := p.Grammar("markdown", 0); err != nil || (g != nil) != (len(tc.disabled) == 0) {
				t.Errorf("the grammar of Markdown: %v, %v (disabled: %v)", g, err, tc.disabled)
			}
		})
	}
}
