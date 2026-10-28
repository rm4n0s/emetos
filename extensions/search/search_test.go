package search

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// project is the folder the tests search.
var project = map[string]string{
	"main.go":           "package main\n\nfunc main() {\n\tneedle()\n\tNeedle()\n}\n",
	"internal/db/db.go": "package db\n\n// needle opens it\nfunc Open() { needle() }\n",
	"internal/db/x.go":  "package db\n\nvar x = 1\n",
	"docs/readme.md":    "# readme\n\nThe needle is here.\n",
	"scripts/run.sh":    "#!/bin/sh\necho needle needle\n",
}

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// rig is the extension activated over the fake API.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *view
	folder string
}

func newRig(t *testing.T, files map[string]string) *rig {
	t.Helper()
	folder := ""
	if files != nil {
		folder = writeProject(t, files)
	}
	e := New()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, api: api, ext: e.(*view), folder: folder}
	return r
}

// find runs the command with a query and waits for the search to end.
func (r *rig) find(args map[string]any) {
	r.t.Helper()
	if _, ok := args["triggerSearch"]; !ok {
		args["triggerSearch"] = true
	}
	if err := r.api.Run(FindInFilesCommand, args); err != nil {
		r.t.Fatal(err)
	}
	r.wait()
}

// wait pumps until the search is over.
func (r *rig) wait() {
	r.t.Helper()
	r.api.WaitFor("the search to end", func() bool {
		return r.ext.status.done || (!r.ext.status.running && r.ext.res.empty() && r.ext.searchBox.Value() == "")
	})
}

// list is the files of the results with their matches, as "path:count".
func (r *rig) list() []string {
	var out []string
	for _, f := range r.ext.res.files {
		out = append(out, f.path+":"+itoa(len(f.matches)))
	}
	return out
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (r *rig) key(k string) any { return r.api.ContextKey(k) }

func TestTheManifestIsValidAndEveryCommandHasAHandler(t *testing.T) {
	e := New()
	m := e.Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, project)
	declared := map[string]bool{}
	for _, c := range m.Contributes.Commands {
		declared[c.ID] = true
		if !r.api.HasHandler(c.ID) {
			t.Errorf("the command %s has no handler", c.ID)
		}
	}
	for _, k := range m.Contributes.Keybindings {
		if !declared[k.Command] {
			t.Errorf("the key %s names the command %s, which the manifest does not declare", k.Key, k.Command)
		}
	}
	for _, a := range m.Contributes.ViewActions {
		if !declared[a.Command] {
			t.Errorf("the title action names the command %s, which the manifest does not declare", a.Command)
		}
	}
	for _, it := range m.Contributes.Menus {
		if !declared[it.Command] {
			t.Errorf("the menu item names the command %s, which the manifest does not declare", it.Command)
		}
	}
}

// TestTheSettingsHaveVSCodesDefaults holds the defaults of search.contribution.ts.
func TestTheSettingsHaveVSCodesDefaults(t *testing.T) {
	want := map[string]any{
		settingSmartCase: false, settingMaxResults: 20000, settingSearchOnType: true, settingDebounce: 300, settingSeedNearestWord: false,
		settingSeedOnFocus: false, settingCollapseResults: "alwaysExpand", settingShowLineNumbers: false, settingDefaultViewMode: "list",
		settingSortOrder: "default", settingDecorationColors: true, settingDecorationBadges: true,
	}
	got := map[string]any{}
	for _, s := range New().Manifest().Contributes.Settings {
		got[s.Key] = s.Default
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("settings %v", got)
	}
}

func TestVSCodesKeysAreBound(t *testing.T) {
	type bound struct{ cmd, when string }
	got := map[string]bound{}
	for _, k := range New().Manifest().Contributes.Keybindings {
		got[k.Key+"|"+k.Command] = bound{k.Command, k.When}
	}
	for _, want := range []struct{ key, cmd string }{
		{"ctrl+shift+f", FindInFilesCommand}, {"ctrl+shift+h", ReplaceInFilesCommand}, {"alt+c", ToggleCaseSensitiveCommand},
		{"alt+w", ToggleWholeWordCommand}, {"alt+r", ToggleRegexCommand}, {"alt+p", TogglePreserveCaseCommand},
		{"f4", FocusNextResultCommand}, {"shift+f4", FocusPreviousResultCommand}, {"ctrl+alt+enter", ReplaceAllCommand},
		{"ctrl+shift+j", ToggleQueryDetailsCommand},
	} {
		if _, ok := got[want.key+"|"+want.cmd]; !ok {
			t.Errorf("%s is not bound to %s", want.key, want.cmd)
		}
	}
}

func TestFindInFilesWithArgumentsSearchesAndShowsTheResults(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle"})
	want := []string{"docs/readme.md:1", "internal/db/db.go:2", "main.go:2", "scripts/run.sh:2"}
	if got := r.list(); !slices.Equal(got, want) {
		t.Errorf("results %v, want %v", got, want)
	}
	if r.ext.searchBox.Value() != "needle" {
		t.Errorf("the search box has %q", r.ext.searchBox.Value())
	}
	if calls := r.api.SearchCalls(); len(calls) != 1 || calls[0].Query.Pattern != "needle" || calls[0].Query.IsCaseSensitive {
		t.Errorf("calls %+v", calls)
	}
	m, f := r.ext.res.counts()
	if m != 7 || f != 4 || resultCountText(m, f) != "7 results in 4 files" {
		t.Errorf("counts %d, %d", m, f)
	}
	for k, want := range map[string]any{"hasSearchResult": true, "viewHasSearchPattern": true, "replaceActive": false, "inTreeView": false, "searchViewletVisible": true} {
		if got := r.key(k); got != want {
			t.Errorf("context key %s = %v, want %v", k, got, want)
		}
	}
	if v := r.api.View(ViewID); v == nil || len(v.Reveals) == 0 {
		t.Error("the view was not revealed")
	}
	// the arguments of VS Code
	r.find(map[string]any{"query": "needle", "isCaseSensitive": true, "matchWholeWord": true, "isRegex": false, "filesToInclude": "*.go", "filesToExclude": "internal"})
	if got, want := r.list(), []string{"main.go:1"}; !slices.Equal(got, want) {
		t.Errorf("with arguments: %v, want %v", got, want)
	}
	last := r.api.SearchCalls()[len(r.api.SearchCalls())-1]
	if !last.Query.IsCaseSensitive || !last.Query.IsWordMatch || last.Options.Include != "*.go" || last.Options.Exclude != "internal" {
		t.Errorf("last call %+v", last)
	}
	if !r.ext.detailsOpen {
		t.Error("the details did not open for the include box")
	}
}

func TestReplaceInFilesOpensTheReplaceBox(t *testing.T) {
	r := newRig(t, project)
	if err := r.api.Run(ReplaceInFilesCommand, map[string]any{"query": "needle", "replace": "pin"}); err != nil {
		t.Fatal(err)
	}
	r.wait()
	if !r.ext.replaceOpen || r.ext.replaceBox.Value() != "pin" || r.key("replaceActive") != true {
		t.Errorf("replaceOpen %v, replace %q, key %v", r.ext.replaceOpen, r.ext.replaceBox.Value(), r.key("replaceActive"))
	}
}

func TestTheSeedIsTheSelectionOrTheWordAtTheCursor(t *testing.T) {
	uri := "file:///work/a.go"
	text := "package a\n\nfunc seedWord() {\n\tx := 1\n}\n"
	sel := func(l1, c1, l2, c2 int) extension.ActiveDocument {
		return extension.ActiveDocument{URI: uri, Selections: []extension.Selection{{Anchor: extension.Position{Line: l1, Col: c1}, Active: extension.Position{Line: l2, Col: c2}}}}
	}
	tests := []struct {
		name    string
		sel     extension.ActiveDocument
		nearest bool
		want    string
	}{
		{"a selection", sel(2, 5, 2, 13), false, "seedWord"},
		{"a selection made backwards", sel(2, 13, 2, 5), false, "seedWord"},
		{"a cursor without the setting", sel(2, 8, 2, 8), false, ""},
		{"a cursor with the setting", sel(2, 8, 2, 8), true, "seedWord"},
		{"a cursor at the end of a word", sel(2, 13, 2, 13), true, "seedWord"},
		{"a cursor between non word characters", sel(2, 16, 2, 16), true, ""},
		{"a selection over lines", sel(2, 5, 3, 2), true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, project)
			r.api.SetDocument(uri, text)
			r.api.SetSetting(settingSeedNearestWord, tt.nearest)
			r.api.SetActive(&tt.sel)
			if err := r.api.Run(FindInFilesCommand, nil); err != nil {
				t.Fatal(err)
			}
			if got := r.ext.searchBox.Value(); got != tt.want {
				t.Errorf("seed %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTheTogglesSearchAgain(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle"})
	before := len(r.api.SearchCalls())
	for _, tt := range []struct {
		cmd  string
		flag func() bool
	}{
		{ToggleCaseSensitiveCommand, func() bool { return r.ext.caseSensitive }},
		{ToggleWholeWordCommand, func() bool { return r.ext.wholeWord }},
		{ToggleRegexCommand, func() bool { return r.ext.regex }},
	} {
		if err := r.api.Run(tt.cmd, nil); err != nil {
			t.Fatal(err)
		}
		r.wait()
		if !tt.flag() {
			t.Errorf("%s did not turn the toggle on", tt.cmd)
		}
	}
	calls := r.api.SearchCalls()
	if len(calls) != before+3 {
		t.Fatalf("%d searches after the toggles, %d before", len(calls), before)
	}
	last := calls[len(calls)-1].Query
	if !last.IsCaseSensitive || !last.IsWordMatch || !last.IsRegExp {
		t.Errorf("the last query %+v", last)
	}
	if err := r.api.Run(TogglePreserveCaseCommand, nil); err != nil || !r.ext.preserveCase {
		t.Errorf("preserve case: %v %v", err, r.ext.preserveCase)
	}
	if len(r.api.SearchCalls()) != len(calls) {
		t.Error("preserve case started a search")
	}
}

func TestSmartCaseIsPassedToTheSearch(t *testing.T) {
	r := newRig(t, project)
	r.api.SetSetting(settingSmartCase, true)
	r.find(map[string]any{"query": "Needle"})
	if calls := r.api.SearchCalls(); !calls[len(calls)-1].Query.IsSmartCase {
		t.Errorf("calls %+v", calls)
	}
	// the fake honors smart case: a capital makes the search case sensitive
	if got, want := r.list(), []string{"main.go:1"}; !slices.Equal(got, want) {
		t.Errorf("results %v, want %v", got, want)
	}
}

func TestTheMessages(t *testing.T) {
	tests := []struct {
		name string
		opts extension.TextSearchOptions
		want string
	}{
		{"nothing", extension.TextSearchOptions{}, "No results found. Review your configured exclusions and check your gitignore files - "},
		{"include", extension.TextSearchOptions{Include: "*.go"}, "No results found in '*.go' - "},
		{"exclude", extension.TextSearchOptions{Exclude: "vendor"}, "No results found excluding 'vendor' - "},
		{"both", extension.TextSearchOptions{Include: "src", Exclude: "vendor"}, "No results found in 'src' excluding 'vendor' - "},
		{"open editors", extension.TextSearchOptions{OnlyOpenEditors: true}, "No results found in open editors. Review your configured exclusions and check your gitignore files - "},
		{"open editors with both", extension.TextSearchOptions{OnlyOpenEditors: true, Include: "a", Exclude: "b"}, "No results found in open editors matching 'a' excluding 'b' - "},
		{"open editors with include", extension.TextSearchOptions{OnlyOpenEditors: true, Include: "a"}, "No results found in open editors matching 'a' - "},
		{"open editors with exclude", extension.TextSearchOptions{OnlyOpenEditors: true, Exclude: "b"}, "No results found in open editors excluding 'b' - "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := noResultsText(tt.opts); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	for _, c := range []struct {
		m, f int
		want string
	}{{1, 1, "1 result in 1 file"}, {2, 1, "2 results in 1 file"}, {1, 2, "1 result in 2 files"}, {7, 4, "7 results in 4 files"}} {
		if got := resultCountText(c.m, c.f); got != c.want {
			t.Errorf("resultCountText(%d, %d) = %q, want %q", c.m, c.f, got, c.want)
		}
	}
	if got := confirmMessage(3, 2, "x"); got != "Replace 3 occurrences across 2 files with 'x'?" {
		t.Errorf("confirm: %q", got)
	}
	if got := confirmMessage(1, 1, ""); got != "Replace 1 occurrence across 1 file?" {
		t.Errorf("confirm: %q", got)
	}
	if got := replacedMessage(1, 2, "pin"); got != "Replaced 1 occurrence across 2 files with 'pin'." {
		t.Errorf("replaced: %q", got)
	}
}

func TestNoResultsAndAnInvalidExpression(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "zzzzz"})
	if !r.ext.res.empty() || !r.ext.status.done || r.key("hasSearchResult") != false {
		t.Errorf("status %+v, key %v", r.ext.status, r.key("hasSearchResult"))
	}
	r.find(map[string]any{"query": "(", "isRegex": true})
	if !r.ext.queryInvalid || r.ext.status.err == nil {
		t.Errorf("an expression that does not compile: invalid %v, err %v", r.ext.queryInvalid, r.ext.status.err)
	}
	// a valid one clears the error
	r.find(map[string]any{"query": "n[e]+dle", "isRegex": true})
	if r.ext.queryInvalid || r.ext.res.empty() {
		t.Errorf("invalid %v, results %v", r.ext.queryInvalid, r.list())
	}
}

func TestAFolderlessWindowSaysSo(t *testing.T) {
	r := newRig(t, nil)
	r.find(map[string]any{"query": "needle"})
	if !r.ext.status.noFolder {
		t.Errorf("status %+v", r.ext.status)
	}
	if len(r.api.SearchCalls()) != 0 {
		t.Error("a search was asked of a window with no folder")
	}
}

func TestTheModesOfTheTree(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle"})
	rows := func() []string {
		var out []string
		for _, n := range r.ext.tree.model.Rows() {
			switch d := n.Data.(type) {
			case folderNode:
				out = append(out, strings.Repeat(" ", n.Depth)+d.label+"/")
			case fileNode:
				out = append(out, strings.Repeat(" ", n.Depth)+d.file.Name())
			case matchNode:
				out = append(out, strings.Repeat(" ", n.Depth)+"- "+strings.TrimSpace(d.match.Preview))
			}
		}
		return out
	}
	wantList := []string{
		"readme.md", " - The needle is here.", "db.go", " - // needle opens it", " - func Open() { needle() }", "main.go", " - needle()", " - Needle()", "run.sh", " - echo needle needle", " - echo needle needle",
	}
	if got := rows(); !slices.Equal(got, wantList) {
		t.Errorf("list:\n%q\nwant\n%q", got, wantList)
	}
	if err := r.api.Run(ViewAsTreeCommand, nil); err != nil {
		t.Fatal(err)
	}
	wantTree := []string{
		"docs/", " readme.md", "  - The needle is here.", "internal/db/", " db.go", "  - // needle opens it", "  - func Open() { needle() }",
		"scripts/", " run.sh", "  - echo needle needle", "  - echo needle needle", "main.go", " - needle()", " - Needle()",
	}
	if got := rows(); !slices.Equal(got, wantTree) {
		t.Errorf("tree:\n%q\nwant\n%q", got, wantTree)
	}
	if r.key("inTreeView") != true {
		t.Error("inTreeView is not set")
	}
	if err := r.api.Run(ViewAsListCommand, nil); err != nil || r.key("inTreeView") != false {
		t.Errorf("back to the list: %v %v", err, r.key("inTreeView"))
	}
	// collapse and expand
	if err := r.api.Run(CollapseCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := rows(); !slices.Equal(got, []string{"readme.md", "db.go", "main.go", "run.sh"}) {
		t.Errorf("collapsed: %q", got)
	}
	if r.key("viewHasSomeCollapsibleResult") != false {
		t.Error("something is still collapsible")
	}
	if err := r.api.Run(ExpandCommand, nil); err != nil || len(rows()) != len(wantList) {
		t.Errorf("expanded: %v %d rows", err, len(rows()))
	}
}

func TestTheSortOrdersAndTheCollapsePolicy(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle"})
	order := func(o string) []string {
		r.api.SetSetting(settingSortOrder, o)
		r.ext.rebuildTree()
		var out []string
		for _, f := range r.ext.res.files {
			out = append(out, f.path)
		}
		return out
	}
	tests := map[string][]string{
		orderDefault:         {"docs/readme.md", "internal/db/db.go", "main.go", "scripts/run.sh"},
		orderFileNames:       {"internal/db/db.go", "main.go", "docs/readme.md", "scripts/run.sh"},
		orderType:            {"internal/db/db.go", "main.go", "docs/readme.md", "scripts/run.sh"},
		orderCountDescending: {"internal/db/db.go", "main.go", "scripts/run.sh", "docs/readme.md"},
		orderCountAscending:  {"docs/readme.md", "internal/db/db.go", "main.go", "scripts/run.sh"},
		orderModified:        {"docs/readme.md", "internal/db/db.go", "main.go", "scripts/run.sh"},
	}
	for o, want := range tests {
		if got := order(o); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", o, got, want)
		}
	}
	for _, tt := range []struct {
		policy string
		rows   int
	}{{"alwaysExpand", 11}, {"alwaysCollapse", 4}, {"auto", 11}} {
		r2 := newRig(t, project)
		r2.api.SetSetting(settingCollapseResults, tt.policy)
		r2.find(map[string]any{"query": "needle"})
		if got := len(r2.ext.tree.model.Rows()); got != tt.rows {
			t.Errorf("%s: %d rows, want %d", tt.policy, got, tt.rows)
		}
	}
}

func TestNextAndPreviousResultOpenTheMatchesAndWrap(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle", "filesToInclude": "*.go"})
	var shown []string
	open := func(cmd string) {
		if err := r.api.Run(cmd, nil); err != nil {
			t.Fatal(err)
		}
		s := r.api.ShownDocuments()
		last := s[len(s)-1]
		shown = append(shown, strings.TrimPrefix(last.URI, "file://"+r.folder+"/")+":"+itoa(last.Line)+":"+itoa(last.Column))
		if !last.Preview {
			t.Errorf("%s opened a tab that is not a preview", cmd)
		}
	}
	open(FocusNextResultCommand)
	open(FocusNextResultCommand)
	open(FocusNextResultCommand)
	open(FocusNextResultCommand)
	open(FocusNextResultCommand)     // wraps to the first
	open(FocusPreviousResultCommand) // and back to the last
	// db.go has two matches (lines 3 and 4) and main.go two (lines 4 and 5): the list shows them in this order, and the keys wrap
	want := []string{"internal/db/db.go:3:4", "internal/db/db.go:4:15", "main.go:4:2", "main.go:5:2", "internal/db/db.go:3:4", "main.go:5:2"}
	if !slices.Equal(shown, want) {
		t.Errorf("shown %v\nwant  %v", shown, want)
	}
}

func TestDismissRemovesFromTheResultsAndNeverFromDisk(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle"})
	m := r.ext.res.byPath["main.go"].matches[0]
	node, ok := r.ext.tree.model.Node(matchKey(m))
	if !ok {
		t.Fatal("no node for the match")
	}
	r.ext.dismiss(node)
	if got := len(r.ext.res.byPath["main.go"].matches); got != 1 {
		t.Errorf("main.go has %d matches", got)
	}
	node, _ = r.ext.tree.model.Node(fileKey(r.ext.res.byPath["scripts/run.sh"]))
	r.ext.dismiss(node)
	if _, ok := r.ext.res.byPath["scripts/run.sh"]; ok {
		t.Error("the file stayed")
	}
	// a folder, in the tree mode
	if err := r.api.Run(ViewAsTreeCommand, nil); err != nil {
		t.Fatal(err)
	}
	node, ok = r.ext.tree.model.Node(folderKey("internal/db"))
	if !ok {
		t.Fatalf("no node for the folder; rows %d", len(r.ext.tree.model.Rows()))
	}
	r.ext.dismiss(node)
	if _, ok := r.ext.res.byPath["internal/db/db.go"]; ok {
		t.Error("the files of the folder stayed")
	}
	if data, err := os.ReadFile(filepath.Join(r.folder, "main.go")); err != nil || !strings.Contains(string(data), "needle()") {
		t.Errorf("the file on disk changed: %v", err)
	}
	// the last file takes hasSearchResult away
	for _, f := range append([]*fileItem(nil), r.ext.res.files...) {
		r.ext.res.removeFile(f)
	}
	r.ext.updateKeys()
	if r.key("hasSearchResult") != false {
		t.Error("hasSearchResult stayed")
	}
}

func TestCopy(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle", "filesToInclude": "run.sh"})
	file := r.ext.res.byPath["scripts/run.sh"]
	r.ext.focus.list = true
	focus := func(key string) {
		r.ext.tree.model.SetFocus(key)
		r.ext.tree.model.SetSelected(key)
	}
	focus(matchKey(file.matches[0]))
	if err := r.api.Run(CopyMatchCommand, nil); err != nil {
		t.Fatal(err)
	}
	focus(fileKey(file))
	if err := r.api.Run(CopyMatchCommand, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(CopyPathCommand, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(CopyAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(r.folder, "scripts", "run.sh")
	want := []string{"echo needle needle", abs + "\n  2: echo needle needle\n  2: echo needle needle\n", abs, abs + "\n  2: echo needle needle\n  2: echo needle needle"}
	if !slices.Equal(r.api.ClipboardTexts(), want) {
		t.Errorf("copied %q\nwant %q", r.api.ClipboardTexts(), want)
	}
}

func TestStateIsSavedAndRestored(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle", "isRegex": true, "isCaseSensitive": true, "filesToInclude": "*.go", "replace": "pin"})
	r.ext.useExcludes = false
	r.ext.tree.treeView = true
	raw, err := r.ext.saveState()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(raw)
	r2 := newRig(t, project)
	if err := r2.ext.restoreState(data); err != nil {
		t.Fatal(err)
	}
	e := r2.ext
	if e.searchBox.Value() != "needle" || !e.regex || !e.caseSensitive || e.includeBox.Value() != "*.go" || e.replaceBox.Value() != "pin" || !e.replaceOpen || !e.detailsOpen || e.useExcludes || !e.tree.treeView {
		t.Errorf("restored %+v", e.inputs)
	}
	if err := r2.ext.restoreState(json.RawMessage(`[1]`)); err == nil {
		t.Error("a state that is not an object should fail")
	}
}

func TestAStaleSearchIsDropped(t *testing.T) {
	r := newRig(t, project)
	// two searches without a turn of the UI thread between them: only the second counts
	r.ext.searchBox.SetValue("needle")
	r.ext.startSearch()
	r.ext.searchBox.SetValue("readme")
	r.ext.startSearch()
	r.wait()
	r.api.WaitFor("the second search", func() bool { return len(r.api.SearchCalls()) >= 2 })
	for i := 0; i < 20; i++ {
		r.api.Pump()
	}
	if got := r.list(); !slices.Equal(got, []string{"docs/readme.md:1"}) {
		t.Errorf("results %v", got)
	}
}

func TestCancelKeepsWhatWasFound(t *testing.T) {
	r := newRig(t, project)
	r.ext.status.running = true
	r.ext.res.add(extension.TextSearchFile{Path: "a.txt", URI: "file:///a.txt", Matches: []extension.TextSearchMatch{{Preview: "x"}}})
	r.ext.cancelSearch()
	if r.ext.status.running || !r.ext.status.cancelled || r.ext.res.empty() {
		t.Errorf("status %+v", r.ext.status)
	}
	r.ext.cancelSearch() // nothing runs: nothing happens
}

func TestClearEmptiesTheBoxesAndTheResults(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle", "filesToInclude": "*.go"})
	if err := r.api.Run(ClearCommand, nil); err != nil {
		t.Fatal(err)
	}
	if !r.ext.res.empty() || r.ext.searchBox.Value() != "" || r.ext.includeBox.Value() != "" || r.key("hasSearchResult") != false {
		t.Errorf("results %v, box %q", r.list(), r.ext.searchBox.Value())
	}
	r.find(map[string]any{"query": "needle"})
	r.ext.searchBox.SetValue("")
	if err := r.api.Run(RefreshCommand, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshSearchesAgain(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle"})
	n := len(r.api.SearchCalls())
	if err := r.api.Run(RefreshCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.wait()
	if len(r.api.SearchCalls()) != n+1 {
		t.Errorf("%d searches", len(r.api.SearchCalls()))
	}
}

func TestReplaceFilesThatNoEditorHasOpen(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle", "isCaseSensitive": true})
	r.ext.replaceOpen = true
	r.ext.replaceBox.SetValue("pin")
	r.ext.replacer = nil
	if err := r.api.Run(ReplaceAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick := r.api.LastPick()
	if pick == nil || pick.Options().Placeholder != "Replace 6 occurrences across 4 files with 'pin'?" {
		t.Fatalf("the confirmation: %+v", pick)
	}
	if got := pick.Labels(); !slices.Equal(got, []string{"Replace", "Cancel"}) {
		t.Errorf("labels %v", got)
	}
	// Cancel leaves everything as it is
	pick.Choose(1)
	if len(r.api.ReplaceCalls()) != 0 {
		t.Error("Cancel replaced")
	}
	if err := r.api.Run(ReplaceAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.api.LastPick().Choose(0)
	r.api.WaitFor("the files to be replaced", func() bool { return r.ext.status.replaced != "" })
	if want := "Replaced 6 occurrences across 4 files with 'pin'."; r.ext.status.replaced != want {
		t.Errorf("message %q, want %q", r.ext.status.replaced, want)
	}
	for path, want := range map[string]string{
		"main.go":           "package main\n\nfunc main() {\n\tpin()\n\tNeedle()\n}\n",
		"internal/db/db.go": "package db\n\n// pin opens it\nfunc Open() { pin() }\n",
		"docs/readme.md":    "# readme\n\nThe pin is here.\n",
		"scripts/run.sh":    "#!/bin/sh\necho pin pin\n",
	} {
		if got, _ := os.ReadFile(filepath.Join(r.folder, filepath.FromSlash(path))); string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	// the replaced matches are gone from the results; the one of main.go that did not match the case was never in them
	if got := r.list(); len(got) != 0 {
		t.Errorf("results left: %v", got)
	}
}

func TestReplaceOpenDocumentsInOneUndoStepAndSaveThem(t *testing.T) {
	r := newRig(t, project)
	uri := "file://" + filepath.Join(r.folder, "main.go")
	r.api.SetDocument(uri, "package main\n\nfunc main() {\n\tneedle()\n\tNeedle()\n}\n// unsaved needle\n")
	var saved []string
	var mu sync.Mutex
	fakeapi.OnRequestValue(r.api, extension.TopicDocumentSave, func(_ context.Context, p extension.DocumentURIRequest) (any, error) {
		mu.Lock()
		saved = append(saved, p.URI)
		mu.Unlock()
		return extension.DocumentSaved{URI: p.URI}, nil
	})
	r.find(map[string]any{"query": "needle"})
	if f := r.ext.res.byPath["main.go"]; len(f.matches) != 3 {
		t.Fatalf("main.go has %d matches (the unsaved text counts)", len(f.matches))
	}
	r.ext.replaceOpen = true
	r.ext.replaceBox.SetValue("pin")
	if err := r.api.Run(ReplaceAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.api.LastPick().Choose(0)
	r.api.WaitFor("the replacement", func() bool { return r.ext.status.replaced != "" })
	applied := r.api.Applied()
	if len(applied) != 1 {
		t.Fatalf("%d edits were applied; one Apply is one undo step for each document", len(applied))
	}
	if len(applied[0].Changes) != 1 || applied[0].Changes[0].URI != uri || len(applied[0].Changes[0].Edits) != 3 {
		t.Errorf("the edit: %+v", applied[0])
	}
	text, _ := r.api.DocumentText(uri)
	if text != "package main\n\nfunc main() {\n\tpin()\n\tpin()\n}\n// unsaved pin\n" {
		t.Errorf("document %q", text)
	}
	r.api.WaitFor("the save", func() bool { mu.Lock(); defer mu.Unlock(); return len(saved) == 1 })
	if saved[0] != uri {
		t.Errorf("saved %v", saved)
	}
	// the other files went through ReplaceInFiles, main.go did not
	for _, call := range r.api.ReplaceCalls() {
		for _, f := range call {
			if f.Path == "main.go" {
				t.Error("a file with an open document was written to disk")
			}
		}
	}
	if data, _ := os.ReadFile(filepath.Join(r.folder, "main.go")); !strings.Contains(string(data), "needle()") {
		t.Error("the file of an open document was changed by the replace on disk")
	}
}

func TestReplaceOneMatchAndAFile(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle", "isCaseSensitive": true, "filesToInclude": "*.sh"})
	r.ext.replaceOpen = true
	r.ext.replaceBox.SetValue("pin")
	file := r.ext.res.byPath["scripts/run.sh"]
	node, _ := r.ext.tree.model.Node(matchKey(file.matches[1]))
	r.ext.replaceNode(node)
	r.api.WaitFor("the replace", func() bool { return r.ext.status.replaced != "" })
	if got, _ := os.ReadFile(filepath.Join(r.folder, "scripts", "run.sh")); string(got) != "#!/bin/sh\necho needle pin\n" {
		t.Errorf("file %q", got)
	}
	if want := "Replaced 1 occurrence across 1 file with 'pin'."; r.ext.status.replaced != want {
		t.Errorf("message %q", r.ext.status.replaced)
	}
	if len(file.matches) != 1 {
		t.Errorf("%d matches left", len(file.matches))
	}
	r.ext.status.replaced = ""
	node, _ = r.ext.tree.model.Node(fileKey(file))
	r.ext.replaceNode(node)
	r.api.WaitFor("the replace of the file", func() bool { return r.ext.status.replaced != "" })
	// the first match is stale now? no: the line changed from "needle needle" to "needle pin", the second match was replaced and
	// the remaining one still has its text
	if got, _ := os.ReadFile(filepath.Join(r.folder, "scripts", "run.sh")); string(got) != "#!/bin/sh\necho pin pin\n" {
		t.Errorf("file %q", got)
	}
	if !r.ext.res.empty() {
		t.Errorf("results left: %v", r.list())
	}
}

func TestAStaleMatchIsNotReplaced(t *testing.T) {
	r := newRig(t, map[string]string{"a.txt": "one needle\n"})
	r.find(map[string]any{"query": "needle"})
	if err := os.WriteFile(filepath.Join(r.folder, "a.txt"), []byte("one pin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.ext.replaceOpen = true
	r.ext.replaceBox.SetValue("x")
	r.ext.replaceAllNoConfirm()
	r.api.WaitFor("the replace", func() bool { return r.ext.status.replaced != "" })
	if got, _ := os.ReadFile(filepath.Join(r.folder, "a.txt")); string(got) != "one pin\n" {
		t.Errorf("file %q", got)
	}
	if want := nothingReplaced; r.ext.status.replaced != want {
		t.Errorf("message %q", r.ext.status.replaced)
	}
}

func TestTheReplacePreviewAcceptsOrRejects(t *testing.T) {
	r := newRig(t, map[string]string{"a.txt": "one needle\ntwo needle\n"})
	r.find(map[string]any{"query": "needle"})
	r.ext.replaceOpen = true
	r.ext.replaceBox.SetValue("pin")
	file := r.ext.res.byPath["a.txt"]
	node, _ := r.ext.tree.model.Node(matchKey(file.matches[0]))
	r.ext.onSelect(node)
	r.api.WaitFor("the proposal", func() bool { return len(r.api.Proposals()) == 1 })
	p := r.api.Proposals()[0]
	if p.Change.Original != "one needle\ntwo needle\n" || p.Change.Proposed != "one pin\ntwo pin\n" || !strings.Contains(p.Change.Title, "Replace Preview") {
		t.Errorf("proposal %+v", p.Change)
	}
	p.Reject()
	for i := 0; i < 10; i++ {
		r.api.Pump()
	}
	if got, _ := os.ReadFile(filepath.Join(r.folder, "a.txt")); string(got) != "one needle\ntwo needle\n" {
		t.Errorf("a rejected preview changed the file: %q", got)
	}
	r.ext.onSelect(node)
	r.api.WaitFor("the second proposal", func() bool { return len(r.api.Proposals()) == 2 })
	r.api.Proposals()[1].Accept("")
	r.api.WaitFor("the replace", func() bool { return r.ext.status.replaced != "" })
	if got, _ := os.ReadFile(filepath.Join(r.folder, "a.txt")); string(got) != "one pin\ntwo pin\n" {
		t.Errorf("file %q", got)
	}
}

func TestAClickOnAMatchOpensItInAPreview(t *testing.T) {
	r := newRig(t, project)
	r.find(map[string]any{"query": "needle", "filesToInclude": "readme"})
	m := r.ext.res.byPath["docs/readme.md"].matches[0]
	node, _ := r.ext.tree.model.Node(matchKey(m))
	r.ext.onSelect(node)
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].Line != 3 || shown[0].Column != 5 || !shown[0].Preview {
		t.Errorf("shown %+v", shown)
	}
	r.ext.onOpen(node)
	if shown = r.api.ShownDocuments(); len(shown) != 2 || shown[1].Preview {
		t.Errorf("Enter opens a tab of its own: %+v", shown)
	}
}

func TestTheColumnOfAMatchIsInUTF16Units(t *testing.T) {
	m := &matchItem{TextSearchMatch: extension.TextSearchMatch{
		Range:   extension.Range{Start: extension.Position{Line: 0, Col: 6}, End: extension.Position{Line: 0, Col: 12}},
		Preview: "héé x🙂 needle", PreviewStart: 6, PreviewEnd: 12,
	}}
	// "héé " is 5 UTF-16 units; the preview "héé x🙂 needle" has the match after "héé x🙂 " (byte 6 is inside "x🙂"): use a real offset
	m.Preview = "héé needle"
	m.PreviewStart, m.PreviewEnd = 6, 12
	m.Range.Start.Col = 6
	if got := utf16Col(m); got != 5 {
		t.Errorf("column %d, want 5", got)
	}
	m.Preview, m.PreviewStart, m.Range.Start.Col = "a🙂b needle", 7, 7
	if got := utf16Col(m); got != 6 {
		t.Errorf("column %d, want 6 (a, a surrogate pair, b, a space, and 1 based)", got)
	}
	// a line that was cut: the byte column stands in
	m.PreviewStart, m.Range.Start.Col = 3, 900
	if got := utf16Col(m); got != 901 {
		t.Errorf("column %d, want 901", got)
	}
}

func TestSearchOnTypeWaitsForTheDebounce(t *testing.T) {
	r := newRig(t, project)
	r.api.SetSetting(settingDebounce, 20)
	r.ext.searchBox.SetValue("needle")
	r.ext.queryChanged(false)
	if len(r.api.SearchCalls()) != 0 {
		t.Error("the search started before the debounce")
	}
	r.api.WaitFor("the search", func() bool { return r.ext.status.done })
	if len(r.api.SearchCalls()) != 1 {
		t.Errorf("%d searches", len(r.api.SearchCalls()))
	}
	// typing again replaces the timer: one more search
	r.ext.searchBox.SetValue("readme")
	r.ext.queryChanged(false)
	r.ext.searchBox.SetValue("readme!")
	r.ext.queryChanged(false)
	r.api.WaitFor("the second search", func() bool { return len(r.api.SearchCalls()) >= 2 && r.ext.status.done })
	if got := r.api.SearchCalls(); len(got) != 2 || got[1].Query.Pattern != "readme!" {
		t.Errorf("searches %+v", got)
	}
	// with search.searchOnType off only Enter searches
	r.api.SetSetting(settingSearchOnType, false)
	r.ext.searchBox.SetValue("needle")
	r.ext.queryChanged(false)
	if len(r.api.SearchCalls()) != 2 {
		t.Error("typing searched with search on type off")
	}
	r.ext.queryChanged(true)
	r.wait()
	if len(r.api.SearchCalls()) != 3 {
		t.Errorf("%d searches", len(r.api.SearchCalls()))
	}
	// an empty box clears
	r.ext.searchBox.SetValue("")
	r.ext.queryChanged(false)
	if !r.ext.res.empty() {
		t.Error("an empty search kept its results")
	}
}

func TestTheFocusInputsMoveOnWithCtrlDown(t *testing.T) {
	r := newRig(t, project)
	r.ext.replaceOpen, r.ext.detailsOpen = true, true
	// no widget is mounted, so no box has the focus; the move from "nothing" goes to the search box and on
	r.ext.moveInputFocus(1)
	r.ext.moveInputFocus(-1)
}

func TestThePatternBoxesBecomeTheOptions(t *testing.T) {
	r := newRig(t, project)
	r.ext.includeBox.SetValue(" ./internal , *.md ")
	r.ext.excludeBox.SetValue("docs")
	r.ext.useExcludes = false
	r.ext.onlyOpen = true
	r.api.SetSetting(settingMaxResults, 5)
	o := r.ext.options()
	if o.Include != "./internal , *.md" || o.Exclude != "docs" || !o.DisregardExcludeSettings || !o.DisregardIgnoreFiles || !o.OnlyOpenEditors || o.MaxResults != 5 {
		t.Errorf("options %+v", o)
	}
}
