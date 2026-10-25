package todo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// project is the folder the tests scan.
var project = map[string]string{
	"main.go":           "package main\n\n// TODO: parse the flags\nfunc main() {\n\t// FIXME(ann): crashes without a config\n}\n",
	"internal/db/db.go": "package db\n\n/*\n * TODO add a retry\n */\nfunc Open() {}\n",
	"scripts/run.sh":    "#!/bin/sh\n# TODO: use set -e\necho run\n",
	"README.md":         "# project\n\nNothing to do here.\n",
	"logo.png":          "\x89PNG TODO not a comment",
	"clean.txt":         "no comments at all\n",
}

// writeProject writes the files into a new folder.
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
	ext    *todo
	folder string
	cancel context.CancelFunc
}

// newRig activates the extension over a folder with the files and waits until it has read them.
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
	r := &rig{t: t, api: api, ext: e.(*todo), folder: folder, cancel: cancel}
	if folder != "" {
		r.settle()
	}
	return r
}

// settle lets the worker read what is queued and the results be applied.
func (r *rig) settle() {
	r.t.Helper()
	r.api.WaitFor("the files to be scanned", func() bool { return r.ext.inflight == 0 && !r.ext.dirty })
}

// rows lists the keys of the rows of the tree that are shown.
func (r *rig) rows() []string {
	var out []string
	for _, n := range r.ext.model.Rows() {
		out = append(out, n.Key)
	}
	return out
}

// counts is the number of comments by file.
func (r *rig) counts() map[string]int {
	out := map[string]int{}
	for f, items := range r.ext.byFile {
		out[f] = len(items)
	}
	return out
}

func TestTheViewListsTheCommentsOfTheFolder(t *testing.T) {
	r := newRig(t, project)
	want := map[string]int{"main.go": 2, "internal/db/db.go": 1, "scripts/run.sh": 1}
	if got := r.counts(); !equalCounts(got, want) {
		t.Errorf("comments by file = %v, want %v", got, want)
	}
	// the files are open the first time they are seen, in the order of their paths
	rows := r.rows()
	wantRows := []string{
		"internal/db/db.go", "internal/db/db.go:4:0",
		"main.go", "main.go:3:0", "main.go:5:1",
		"scripts/run.sh", "scripts/run.sh:2:0",
	}
	if !slices.Equal(rows, wantRows) {
		t.Errorf("rows = %v, want %v", rows, wantRows)
	}
	if it := r.api.StatusItem("count"); it == nil || it.Text != "$(checklist) 4" {
		t.Errorf("the status item = %+v", it)
	}
	if v := r.api.View(ViewID); v == nil || v.Content.Build == nil {
		t.Fatal("the view has no content")
	}
	// the setting is registered with its default, and the tags are read from it
	tags, err := extension.SettingValue[[]string](r.api.Settings(), TagsSetting)
	if err != nil || !slices.Equal(tags, []string{"TODO", "FIXME"}) {
		t.Errorf("tags = %v, %v", tags, err)
	}
}

func equalCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestTheListFollowsTheChangesOfTheFiles(t *testing.T) {
	r := newRig(t, project)
	write := func(name, text string) {
		t.Helper()
		full := filepath.Join(r.folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	changed := func(changes ...extension.FileChange) {
		r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: changes})
		r.settle()
	}

	// a file changes: its comments are read again
	write("main.go", "package main\n// TODO: only one now\n")
	changed(extension.FileChange{Kind: extension.FileChanged, Path: "main.go"})
	if got := r.ext.byFile["main.go"]; len(got) != 1 || got[0].Message != "only one now" || got[0].Line != 2 {
		t.Errorf("main.go = %+v", got)
	}
	// a new file
	write("extra/new.go", "package extra\n// FIXME: new file\n")
	r.api.RescanFiles()
	changed(extension.FileChange{Kind: extension.FileCreated, Path: "extra/new.go"})
	if got := r.ext.byFile["extra/new.go"]; len(got) != 1 || got[0].Tag != "FIXME" {
		t.Errorf("extra/new.go = %+v", got)
	}
	if it := r.api.StatusItem("count"); it.Text != "$(checklist) 4" {
		t.Errorf("status = %q", it.Text)
	}
	// a file with its comments removed leaves the list
	write("scripts/run.sh", "echo run\n")
	changed(extension.FileChange{Kind: extension.FileChanged, Path: "scripts/run.sh"})
	if _, ok := r.ext.byFile["scripts/run.sh"]; ok {
		t.Error("a file without comments is still listed")
	}
	// a deleted file and a deleted folder
	if err := os.Remove(filepath.Join(r.folder, "main.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(r.folder, "internal")); err != nil {
		t.Fatal(err)
	}
	r.api.RescanFiles()
	changed(extension.FileChange{Kind: extension.FileDeleted, Path: "main.go"}, extension.FileChange{Kind: extension.FileDeleted, Path: "internal"})
	if got := r.counts(); !equalCounts(got, map[string]int{"extra/new.go": 1}) {
		t.Errorf("after the deletions: %v", got)
	}
	// a rename moves the comments
	if err := os.Rename(filepath.Join(r.folder, "extra", "new.go"), filepath.Join(r.folder, "extra", "renamed.go")); err != nil {
		t.Fatal(err)
	}
	r.api.RescanFiles()
	changed(extension.FileChange{Kind: extension.FileRenamed, Path: "extra/renamed.go", OldPath: "extra/new.go"})
	if got := r.counts(); !equalCounts(got, map[string]int{"extra/renamed.go": 1}) {
		t.Errorf("after the rename: %v", got)
	}
	// a rescan reads everything below the path again; the list changing reads the files that are new
	write("late.go", "// TODO: found by the walk\n")
	r.api.RescanFiles()
	changed(extension.FileChange{Kind: extension.FileListed})
	if _, ok := r.ext.byFile["late.go"]; !ok {
		t.Error("a file the walk found later is not listed")
	}
	write("late.go", "// TODO: changed behind its back\n")
	changed(extension.FileChange{Kind: extension.FileRescan, Path: "."})
	if got := r.ext.byFile["late.go"]; len(got) != 1 || got[0].Message != "changed behind its back" {
		t.Errorf("after a rescan late.go = %+v", got)
	}
	// the view was asked to build again each time
	if refreshs, _, _ := r.api.View(ViewID).Snapshot(); refreshs == 0 {
		t.Error("the view was never refreshed")
	}
}

func TestAClickOnACommentOpensTheFileThere(t *testing.T) {
	r := newRig(t, project)
	node, ok := r.ext.model.Node("main.go:5:1")
	if !ok {
		t.Fatalf("no such row in %v", r.rows())
	}
	r.ext.onSelect(node)
	got := r.api.ShownDocuments()
	if len(got) != 1 || got[0].URI != "file://"+filepath.ToSlash(filepath.Join(r.folder, "main.go")) || got[0].Line != 5 || got[0].Column != 5 || !got[0].Preview {
		t.Errorf("shown = %+v", got)
	}
	// a click on a file only folds it
	fileNode, _ := r.ext.model.Node("main.go")
	r.ext.onSelect(fileNode)
	if len(r.api.ShownDocuments()) != 1 {
		t.Error("a click on a file opened a document")
	}
}

func TestTheTagsAreASetting(t *testing.T) {
	r := newRig(t, project)
	r.api.SetSetting(TagsSetting, []string{"NOTE"})
	r.settle()
	if len(r.ext.byFile) != 0 {
		t.Errorf("comments of old tags remain: %v", r.counts())
	}
	// a file with a note
	if err := os.WriteFile(filepath.Join(r.folder, "n.go"), []byte("// NOTE: remember\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.api.RescanFiles()
	r.api.SetSetting(TagsSetting, []string{"NOTE", "TODO"})
	r.settle()
	if got := r.counts(); got["n.go"] != 1 || got["main.go"] != 1 {
		t.Errorf("comments = %v", got)
	}
}

func TestTheStateKeepsWhichFilesAreOpen(t *testing.T) {
	r := newRig(t, project)
	r.ext.model.SetExpandedKeys([]string{"main.go"})
	saved, err := r.ext.saveState()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(saved)
	if string(data) != `{"expanded":["main.go"]}` {
		t.Errorf("state = %s", data)
	}
	// a new session restores it as soon as the comments are there
	e := New().(*todo)
	folder := writeProject(t, project)
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	if err := api.View(ViewID).Content.RestoreState(data); err != nil {
		t.Fatal(err)
	}
	api.WaitFor("the files", func() bool { return e.inflight == 0 && !e.dirty })
	var rows []string
	for _, n := range e.model.Rows() {
		rows = append(rows, n.Key)
	}
	if !slices.Equal(rows, []string{"internal/db/db.go", "main.go", "main.go:3:0", "main.go:5:1", "scripts/run.sh"}) {
		t.Errorf("rows after the restore = %v", rows)
	}
	if err := api.View(ViewID).Content.RestoreState([]byte(`{`)); err == nil {
		t.Error("a state that is not JSON was accepted")
	}
}

func TestAWorkspaceWithNoFolderSaysSo(t *testing.T) {
	r := newRig(t, nil)
	r.api.Pump()
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 100}, widgetOf(r))
	if !ts.Exists(gluttertest.ByText("Open a folder to list its TODO and FIXME comments.")) {
		t.Error("the view does not ask for a folder")
	}
}

func TestAFolderWithoutCommentsSaysSo(t *testing.T) {
	r := newRig(t, map[string]string{"a.go": "package a\n"})
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 100}, widgetOf(r))
	if !ts.Exists(gluttertest.ByText("No TODO or FIXME comments in the folder.")) {
		t.Error("the view does not say that there are none")
	}
}

func TestTheWorkerEndsWithTheActivation(t *testing.T) {
	r := newRig(t, project)
	r.cancel()
	select {
	case <-r.ext.done:
	case <-timeout():
		t.Fatal("the worker did not end when the context was cancelled")
	}
}

func TestTheCommandsRefreshAndCollapse(t *testing.T) {
	r := newRig(t, project)
	if err := os.WriteFile(filepath.Join(r.folder, "main.go"), []byte("// TODO: changed without an event\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(RefreshCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := r.ext.byFile["main.go"]; len(got) != 1 || got[0].Message != "changed without an event" {
		t.Errorf("main.go after Refresh = %+v", got)
	}
	if err := r.api.Run(CollapseCommand, nil); err != nil {
		t.Fatal(err)
	}
	for _, n := range r.ext.model.Rows() {
		if n.Depth > 0 {
			t.Errorf("a child is shown after Collapse All: %s", n.Key)
		}
	}
}

func TestTheManifestDeclaresWhatItNeeds(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.ViewContainers) != 1 || m.Contributes.ViewContainers[0].Icon != "checklist" {
		t.Errorf("containers = %+v", m.Contributes.ViewContainers)
	}
	for _, a := range m.Contributes.ViewActions {
		if _, ok := ui.CodiconCode(a.Icon); !ok {
			t.Errorf("the icon %q of an action is not a Codicon", a.Icon)
		}
	}
	if code, ok := ui.CodiconCode(m.Contributes.ViewContainers[0].Icon); !ok || code == 0 {
		t.Error("the icon of the container is not a Codicon")
	}
	if !strings.HasPrefix(m.Contributes.Views[0].ID, m.ID+".") {
		t.Error("the id of the view is not under the id of the extension")
	}
}
