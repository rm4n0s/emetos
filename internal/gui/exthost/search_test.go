package exthost

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// The search across files for extensions (step 17e.2): Workspace().FindTextInFiles, NewTextReplacer and ReplaceInFiles.

// Unsaved implements UnsavedSource for the rig: every file document it holds counts as one with unsaved changes.
func (d *docSource) Unsaved() map[string]string {
	out := map[string]string{}
	for uri, text := range d.texts {
		if path, ok := strings.CutPrefix(uri, "file://"); ok {
			out[path] = text
		}
	}
	return out
}

// OpenFiles implements UnsavedSource for the rig.
func (d *docSource) OpenFiles() []string {
	var out []string
	for _, uri := range d.order {
		if path, ok := strings.CutPrefix(uri, "file://"); ok {
			out = append(out, path)
		}
	}
	return out
}

// searchRig makes a tab with a folder of files and returns the folder, the tab and the API.
func searchRig(t *testing.T, files map[string]string) (string, *tab, extension.API) {
	t.Helper()
	folder := t.TempDir()
	for name, text := range files {
		full := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, tb, api := grabAPI(t, "ext", func(r *rig) { r.folders["one"] = folder; r.noCoreDocs["one"] = true })
	return folder, tb, api
}

// findAll runs a search and lists "path:line:col:text", sorted.
func findAll(t *testing.T, api extension.API, q extension.TextSearchQuery, o extension.TextSearchOptions) ([]string, extension.TextSearchSummary) {
	t.Helper()
	var mu sync.Mutex
	var out []string
	sum, err := api.Workspace().FindTextInFiles(context.Background(), q, o, func(f extension.TextSearchFile) {
		mu.Lock()
		defer mu.Unlock()
		for _, m := range f.Matches {
			out = append(out, f.Path+":"+string(rune('0'+m.Range.Start.Line))+":"+string(rune('0'+m.Range.Start.Col))+":"+m.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out, sum
}

func TestFindTextInFilesSearchesTheFolderWithTheSettings(t *testing.T) {
	folder, tb, api := searchRig(t, map[string]string{
		".gitignore":        "ignored.txt\n",
		"a.txt":             "needle\n",
		"sub/b.go":          "x\nxneedle\n",
		"ignored.txt":       "needle\n",
		"node_modules/n.js": "needle\n",
		"bin.dat":           "needle\x00\n",
	})
	want := func(t *testing.T, got []string, w ...string) {
		t.Helper()
		if !slices.Equal(got, w) {
			t.Errorf("got %q, want %q", got, w)
		}
	}
	q := extension.TextSearchQuery{Pattern: "needle"}
	got, sum := findAll(t, api, q, extension.TextSearchOptions{})
	want(t, got, "a.txt:0:0:needle", "sub/b.go:1:1:needle")
	if sum.Files != 2 || sum.Matches != 2 || sum.Binary != 1 {
		t.Errorf("summary %+v", sum)
	}
	got, _ = findAll(t, api, q, extension.TextSearchOptions{DisregardIgnoreFiles: true})
	want(t, got, "a.txt:0:0:needle", "ignored.txt:0:0:needle", "sub/b.go:1:1:needle")
	got, _ = findAll(t, api, q, extension.TextSearchOptions{DisregardExcludeSettings: true})
	want(t, got, "a.txt:0:0:needle", "node_modules/n.js:0:0:needle", "sub/b.go:1:1:needle")
	got, _ = findAll(t, api, q, extension.TextSearchOptions{Include: "*.go"})
	want(t, got, "sub/b.go:1:1:needle")
	got, _ = findAll(t, api, q, extension.TextSearchOptions{Exclude: "sub"})
	want(t, got, "a.txt:0:0:needle")

	// the URI of a file
	var uri string
	if _, err := api.Workspace().FindTextInFiles(context.Background(), q, extension.TextSearchOptions{Include: "a.txt"}, func(f extension.TextSearchFile) { uri = f.URI }); err != nil {
		t.Fatal(err)
	}
	if uri != "file://"+filepath.Join(folder, "a.txt") {
		t.Errorf("uri %q", uri)
	}

	t.Run("the settings of the tab", func(t *testing.T) {
		tb.changeSetting("search.exclude", map[string]any{"sub": true})
		got, _ := findAll(t, api, q, extension.TextSearchOptions{})
		want(t, got, "a.txt:0:0:needle")
	})
}

func TestFindTextInFilesUsesTheFileIndexWhenItIsComplete(t *testing.T) {
	_, tb, api := searchRig(t, map[string]string{"a.txt": "needle\n", "b.txt": "needle\n", "c.txt": "needle\n"})
	q := extension.TextSearchQuery{Pattern: "needle"}
	tb.files, tb.complete = []string{"a.txt", "c.txt"}, true
	if got, _ := findAll(t, api, q, extension.TextSearchOptions{}); !slices.Equal(got, []string{"a.txt:0:0:needle", "c.txt:0:0:needle"}) {
		t.Errorf("with the index: %q", got)
	}
	tb.complete = false
	if got, _ := findAll(t, api, q, extension.TextSearchOptions{}); len(got) != 3 {
		t.Errorf("while the index is walking: %q", got)
	}
	tb.complete = true
	if got, _ := findAll(t, api, q, extension.TextSearchOptions{DisregardIgnoreFiles: true}); len(got) != 3 {
		t.Errorf("without the ignore files the index does not apply: %q", got)
	}
}

func TestFindTextInFilesReadsTheEditors(t *testing.T) {
	folder, tb, api := searchRig(t, map[string]string{"a.txt": "disk needle\n", "b.txt": "disk needle\n"})
	a, b, n := filepath.Join(folder, "a.txt"), filepath.Join(folder, "b.txt"), filepath.Join(folder, "new.txt")
	tb.docs.texts["file://"+a] = "unsaved\nneedle in the editor\n"
	tb.docs.texts["file://"+n] = "needle in a new file\n"
	tb.docs.texts["untitled:1"] = "needle in an untitled document\n"
	tb.docs.order = []string{"file://" + a, "file://" + n, "untitled:1"}
	q := extension.TextSearchQuery{Pattern: "needle"}
	got, _ := findAll(t, api, q, extension.TextSearchOptions{})
	if want := []string{"a.txt:1:0:needle", "b.txt:0:5:needle", "new.txt:0:0:needle"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	got, _ = findAll(t, api, q, extension.TextSearchOptions{OnlyOpenEditors: true})
	if want := []string{"a.txt:1:0:needle", "new.txt:0:0:needle"}; !slices.Equal(got, want) {
		t.Errorf("only open editors: got %q, want %q", got, want)
	}
	_ = b
	tb.docs.order = nil
	tb.docs.texts = map[string]string{}
	if got, _ := findAll(t, api, q, extension.TextSearchOptions{OnlyOpenEditors: true}); len(got) != 0 {
		t.Errorf("no editor is open: %q", got)
	}
}

func TestFindTextInFilesErrors(t *testing.T) {
	_, tb, api := searchRig(t, map[string]string{"a.txt": strings.Repeat("needle\n", 1000)})
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	tests := []struct {
		name  string
		ctx   context.Context
		q     extension.TextSearchQuery
		route string
	}{
		{"an empty pattern", ctx, extension.TextSearchQuery{}, "SearchPatternEmpty"},
		{"a regular expression that does not compile", ctx, extension.TextSearchQuery{Pattern: "(", IsRegExp: true}, "Run->compile->Compile->compileECMAScript.RegexCompileFailed"},
		{"a context that is done", cancelled, extension.TextSearchQuery{Pattern: "needle"}, "SearchCancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := api.Workspace().FindTextInFiles(tt.ctx, tt.q, extension.TextSearchOptions{}, func(extension.TextSearchFile) {})
			mustRoute(t, err, tt.route)
		})
	}
	t.Run("a scope with no folder", func(t *testing.T) {
		_, _, api := grabAPI(t, "ext", func(r *rig) { r.folders["one"] = "" })
		_, err := api.Workspace().FindTextInFiles(ctx, extension.TextSearchQuery{Pattern: "x"}, extension.TextSearchOptions{}, nil)
		mustRoute(t, err, "workspaceAPI.FindTextInFiles.ExtensionNoFolder")
		_, err = api.Workspace().ReplaceInFiles(ctx, nil)
		mustRoute(t, err, "workspaceAPI.ReplaceInFiles.ExtensionNoFolder")
	})
	t.Run("after the extension was turned off", func(t *testing.T) {
		tb.disabled = []string{"ext"}
		tb.scope.Refresh()
		_, err := api.Workspace().FindTextInFiles(ctx, extension.TextSearchQuery{Pattern: "needle"}, extension.TextSearchOptions{}, nil)
		mustRoute(t, err, "workspaceAPI.FindTextInFiles.ExtensionDeactivated")
		_, err = api.Workspace().ReplaceInFiles(ctx, nil)
		mustRoute(t, err, "workspaceAPI.ReplaceInFiles.ExtensionDeactivated")
	})
}

func TestNewTextReplacer(t *testing.T) {
	_, _, api := searchRig(t, nil)
	r, err := api.Workspace().NewTextReplacer(extension.TextSearchQuery{Pattern: `(\w+)-(\w+)`, IsRegExp: true}, "$2-$1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Replace("ab-cd"); got != "cd-ab" {
		t.Errorf("got %q", got)
	}
	p, err := api.Workspace().NewTextReplacer(extension.TextSearchQuery{Pattern: "foo"}, "bar", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Replace("Foo"); got != "Bar" {
		t.Errorf("preserve case: %q", got)
	}
	_, err = api.Workspace().NewTextReplacer(extension.TextSearchQuery{Pattern: "(", IsRegExp: true}, "x", false)
	mustRoute(t, err, "NewReplacer->Compile->compileECMAScript.RegexCompileFailed")
}

func TestReplaceInFiles(t *testing.T) {
	folder, _, api := searchRig(t, map[string]string{
		"a.txt":     "one needle\r\ntwo\r\n",
		"sub/b.txt": "needle needle\n",
		"bin.dat":   "needle\x00\n",
		"gone.txt":  "needle\n",
	})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(folder, "link")); err != nil {
		t.Skipf("no symbolic links: %v", err)
	}
	if err := os.Remove(filepath.Join(folder, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	rep := func(l, c, el, ec int, expected, text string) extension.TextReplacement {
		return extension.TextReplacement{Range: extension.Range{Start: extension.Position{Line: l, Col: c}, End: extension.Position{Line: el, Col: ec}}, Expected: expected, Text: text}
	}
	report, err := api.Workspace().ReplaceInFiles(context.Background(), []extension.FileReplacement{
		{Path: "a.txt", Replacements: []extension.TextReplacement{rep(0, 4, 0, 10, "needle", "pin")}},
		{Path: "sub/b.txt", Replacements: []extension.TextReplacement{rep(0, 0, 0, 6, "needle", "A"), rep(0, 7, 0, 13, "NEEDLE", "B")}},
		{Path: "bin.dat", Replacements: []extension.TextReplacement{rep(0, 0, 0, 6, "needle", "A")}},
		{Path: "gone.txt", Replacements: []extension.TextReplacement{rep(0, 0, 0, 6, "needle", "A")}},
		{Path: "link/secret.txt", Replacements: []extension.TextReplacement{rep(0, 0, 0, 6, "needle", "A")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Replaced != 2 || report.Skipped != 1 || !slices.Equal(report.Files, []string{"a.txt", "sub/b.txt"}) || len(report.Failed) != 3 {
		t.Errorf("report %+v", report)
	}
	for _, f := range report.Failed {
		if f.Message == "" {
			t.Errorf("a failure with no message: %+v", f)
		}
	}
	for path, want := range map[string]string{"a.txt": "one pin\r\ntwo\r\n", "sub/b.txt": "A needle\n", "bin.dat": "needle\x00\n"} {
		if got, _ := os.ReadFile(filepath.Join(folder, path)); string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(got) != "needle\n" {
		t.Errorf("a file outside the folder was changed: %q", got)
	}

	bad := []struct {
		name, path, route string
	}{
		{"a path that leaves the folder", "../x", "workspaceAPI.ReplaceInFiles.ExtensionFilePathInvalid"},
		{"the folder itself", ".", "workspaceAPI.ReplaceInFiles.ExtensionFilePathInvalid"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			_, err := api.Workspace().ReplaceInFiles(context.Background(), []extension.FileReplacement{{Path: tt.path}})
			mustRoute(t, err, tt.route)
		})
	}
	t.Run("a link that leaves the folder is a failure of that file", func(t *testing.T) {
		rep, err := api.Workspace().ReplaceInFiles(context.Background(), []extension.FileReplacement{{Path: "link/secret.txt", Replacements: []extension.TextReplacement{rep(0, 0, 0, 6, "needle", "A")}}})
		if err != nil || len(rep.Failed) != 1 || !strings.Contains(rep.Failed[0].Message, "link") {
			t.Errorf("report %+v, %v", rep, err)
		}
	})
	t.Run("a context that is done", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := api.Workspace().ReplaceInFiles(ctx, []extension.FileReplacement{{Path: "a.txt"}})
		if err == nil {
			t.Error("a cancelled context should fail")
		}
	})
}

func TestContextKeysTheManifestDeclares(t *testing.T) {
	var api extension.API
	m := manifest("emetos.ck")
	m.Contributes.ContextKeys = []string{"hasSearchResult"}
	f, _ := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	if err := api.Context().Set("hasSearchResult", true); err != nil {
		t.Fatal(err)
	}
	if tb.ctxKeys.Get("hasSearchResult") != true {
		t.Error("the declared key is not set")
	}
	mustRoute(t, api.Context().Set("searchViewletVisible", true), "contextAPI.Set.ExtensionContextKeyForeign")
	tb.disabled = []string{"emetos.ck"}
	tb.scope.Refresh()
	if tb.ctxKeys.Get("hasSearchResult") != nil {
		t.Error("the declared key stayed after the extension was turned off")
	}
}
