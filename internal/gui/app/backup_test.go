package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of hot exit (step 7.6): the backups of unsaved documents, written as they change and at exit, and
// restored when the window opens again. The windows are opened one after the other over the same isolated state
// folder, as the runs of the program are.

// hotExitEnv is a project whose settings have files.hotExit set to mode.
func hotExitEnv(t *testing.T, mode string) *stateEnv {
	t.Helper()
	return newStateEnv(t, map[string]any{"files.hotExit": mode})
}

// setHotExit changes files.hotExit in the settings file, for the windows that open next.
func (e *stateEnv) setHotExit(mode string) {
	e.t.Helper()
	testutil.Settings(e.t, e.paths, map[string]any{"files.hotExit": mode})
}

// backupStore is the store of the backups of the project's window, read directly from the disk.
func (e *stateEnv) backupStore() *storage.BackupStore {
	e.t.Helper()
	id, err := storage.WindowID(e.root)
	if err != nil {
		e.t.Fatal(err)
	}
	s, err := storage.NewBackupStore(e.paths, id, storage.BackupStoreOptions{})
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// backups are the backups on the disk by URI.
func (e *stateEnv) backups() map[string]storage.Backup {
	e.t.Helper()
	list, problems := e.backupStore().List(context.Background())
	if len(problems) != 0 {
		e.t.Fatalf("backup problems: %v", problems)
	}
	out := map[string]storage.Backup{}
	for _, b := range list {
		out[b.URI] = b
	}
	return out
}

func (e *stateEnv) backupURIs() []string {
	var out []string
	for uri := range e.backups() {
		out = append(out, uri)
	}
	slices.Sort(out)
	return out
}

// marker tells whether the window's running marker is on the disk.
func (e *stateEnv) marker() bool {
	_, err := os.Stat(filepath.Join(e.backupStore().Dir(), "running"))
	return err == nil
}

// wait lets the writer of the backups finish what it was given.
func (r *stateRun) settleBackups() {
	r.env.t.Helper()
	t := r.win.Backups()
	if t == nil {
		r.env.t.Fatal("the window keeps no backups")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := t.Wait(ctx); err != nil {
		r.env.t.Fatal(err)
	}
}

// delay moves the clock by the delay of the backups and lets the writes happen.
func (r *stateRun) delay() {
	r.env.t.Helper()
	r.env.clock.Advance(storage.DefaultBackupDelay)
	r.pump() // the timers hand their work to the UI thread
	r.settleBackups()
}

// typeText types into the active editor.
func (r *stateRun) typeText(text string) {
	r.env.t.Helper()
	e := r.shell().activeEditor()
	if e == nil {
		r.env.t.Fatal("no active editor")
	}
	if err := e.view.RunCommand("type", map[string]any{"text": text}); err != nil {
		r.env.t.Fatal(err)
	}
	r.pump()
}

// exit quits the way the user does and waits for the window to be gone: the backups are written on a goroutine.
func (r *stateRun) exit() {
	r.env.t.Helper()
	before := r.quits
	r.win.RequestClose(CloseReasonQuit)
	r.waitFor("the window to quit", func() bool {
		r.ts.FlushPosted()
		return r.quits > before
	})
	if r.win.Dialogs.IsShowing() {
		spec, _ := r.win.Dialogs.Current()
		r.env.t.Fatalf("a dialog shows: %q", spec.Message)
	}
	if r.quits != before+1 {
		r.env.t.Fatalf("%d quits", r.quits-before)
	}
	r.ts.PumpWidget(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{}))
}

// doc is the open document of a file.
func (r *stateRun) doc(path string) *document.Document {
	r.env.t.Helper()
	d, ok := r.win.Documents.Get(document.FileURI(path))
	if !ok {
		r.env.t.Fatalf("%s is not open", path)
	}
	return d
}

func (r *stateRun) untitled(n int) *document.Document {
	r.env.t.Helper()
	d, ok := r.win.Documents.Get(document.UntitledURI(n))
	if !ok {
		r.env.t.Fatalf("Untitled-%d is not open", n)
	}
	return d
}

// notes are the messages of the notifications.
func (r *stateRun) notes() []string {
	var out []string
	for _, it := range r.win.Process.Notify.Items() {
		out = append(out, it.Message)
	}
	return out
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTheBackupIsWrittenADelayAfterTheLastChange(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	run := env.start(env.root)
	run.open(env.path("main.go"), 0)
	if !env.marker() {
		t.Error("an open window has no running marker")
	}
	run.typeText("// one\n")
	env.clock.Advance(999 * time.Millisecond)
	run.pump()
	run.settleBackups()
	if got := env.backupURIs(); len(got) != 0 {
		t.Fatalf("backups %v before the delay", got)
	}
	run.typeText("// two\n") // restarts the delay
	env.clock.Advance(999 * time.Millisecond)
	run.pump()
	run.settleBackups()
	if got := env.backupURIs(); len(got) != 0 {
		t.Fatalf("backups %v: the second change must restart the delay", got)
	}
	run.delay()
	uri := document.FileURI(env.path("main.go")).String()
	b, ok := env.backups()[uri]
	if !ok {
		t.Fatalf("no backup of main.go: %v", env.backupURIs())
	}
	if !strings.HasPrefix(b.Content, "// one\n// two\n") || b.Stat == nil || b.Language != "go" {
		t.Errorf("backup %q stat %v language %q", b.Content[:min(20, len(b.Content))], b.Stat, b.Language)
	}
	// saving takes it away
	run.command("workbench.action.files.save", nil)
	run.waitFor("the save", func() bool { return !run.doc(env.path("main.go")).Dirty() })
	run.settleBackups()
	if got := env.backupURIs(); len(got) != 0 {
		t.Errorf("backups %v after the save", got)
	}
}

func TestQuittingBacksUpWithoutAskingAndTheNextStartRestores(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.open(env.path("src/a.go"), 0)
	first.command("workbench.action.splitEditorRight", nil)
	first.command("workbench.action.files.newUntitledFile", nil)
	first.typeText("my notes\nsecond line")
	first.command("workbench.action.focusFirstEditorGroup", nil)
	first.command("dev.openFile", map[string]any{"path": env.path("main.go"), "group": float64(1)})
	first.typeText("// typed\n")
	disk := readText(t, env.path("main.go"))
	main := first.editorOf(1, "main.go")
	main.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 9, Col: 1})})
	wantCursor := main.view.Cursors().Selections()
	wantLayout := first.layout()
	if len(wantLayout) != 2 {
		t.Fatalf("layout %v", wantLayout)
	}
	first.win.Process.Notify.ClearAll()

	first.exit()
	if first.quits != 1 {
		t.Fatalf("Quit called %d times", first.quits)
	}
	if env.marker() {
		t.Error("the running marker stays after a clean exit")
	}
	got := env.backups()
	mainURI := document.FileURI(env.path("main.go")).String()
	if len(got) != 2 || got[mainURI].Content == "" || got["untitled:1"].Content != "my notes\nsecond line" {
		t.Fatalf("backups %v", env.backupURIs())
	}
	if readText(t, env.path("main.go")) != disk {
		t.Error("quitting changed the file")
	}

	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, wantLayout) {
		t.Errorf("layout %v, want %v", got, wantLayout)
	}
	doc := second.doc(env.path("main.go"))
	if !doc.Dirty() || !strings.HasPrefix(doc.Text(), "// typed\n") {
		t.Errorf("main.go: dirty %v, text %q", doc.Dirty(), doc.Text()[:20])
	}
	if sel := second.editorOf(1, "main.go").view.Cursors().Selections(); !slices.Equal(sel, wantCursor) {
		t.Errorf("cursors %v, want %v", sel, wantCursor)
	}
	notes := second.untitled(1)
	if notes.Text() != "my notes\nsecond line" || !notes.Dirty() || notes.URI().Title() != "Untitled-1" {
		t.Errorf("untitled: %q dirty %v", notes.Text(), notes.Dirty())
	}
	if n := second.notes(); len(n) != 0 {
		t.Errorf("notifications %v after a clean restore", n)
	}
	if !env.marker() {
		t.Error("the second window has no running marker")
	}
	// the backups stay until the documents are saved or given up
	if len(env.backups()) != 2 {
		t.Errorf("backups %v", env.backupURIs())
	}
	// undo returns to the text of the file
	second.shell().activeEditor() // the active editor of the first group is main.go
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if doc.Text() != disk || doc.Dirty() {
		t.Errorf("after undo: dirty %v, text %q", doc.Dirty(), doc.Text()[:10])
	}
	second.settleBackups()
	if _, ok := env.backups()[mainURI]; ok {
		t.Error("the backup of main.go stays after the document is clean again")
	}
	// saving the untitled document through Save As takes its backup away
	saved := filepath.Join(env.root, "notes.txt")
	if _, err := notes.SaveAs(context.Background(), saved, document.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	second.pump()
	second.settleBackups()
	if got := env.backupURIs(); len(got) != 0 {
		t.Errorf("backups %v after the saves", got)
	}
	if readText(t, saved) != "my notes\nsecond line" {
		t.Error("the untitled document was not saved")
	}
}

func TestHotExitOffAsksAndDiscardsTheBackupsOfWhatCloses(t *testing.T) {
	env := hotExitEnv(t, "off")
	run := env.start(env.root)
	run.open(env.path("main.go"), 0)
	run.typeText("// x\n")
	run.delay()
	if got := env.backupURIs(); len(got) != 0 {
		t.Fatalf("hot exit off made backups: %v", got)
	}
	run.win.RequestClose(CloseReasonQuit)
	run.pump()
	if !run.win.Dialogs.IsShowing() {
		t.Fatal("the window did not ask")
	}
	run.tapDialog("Cancel")
	if run.quits != 0 {
		t.Fatal("Cancel closed the window")
	}
	run.win.RequestClose(CloseReasonQuit)
	run.pump()
	run.tapDialog("Don't Save")
	run.waitFor("the quit", func() bool { return run.quits == 1 })
	if got := env.backupURIs(); len(got) != 0 {
		t.Errorf("backups %v", got)
	}
	if env.marker() {
		t.Error("the marker stays")
	}
	// nothing comes back
	second := env.start(env.root)
	if second.doc(env.path("main.go")).Dirty() {
		t.Error("the unsaved changes came back")
	}
}

func TestSwitchingHotExitOffDropsTheBackupsAndOnMakesThemAgain(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	run := env.start(env.root)
	run.open(env.path("main.go"), 0)
	run.typeText("// x\n")
	run.delay()
	if len(env.backups()) != 1 {
		t.Fatalf("backups %v", env.backupURIs())
	}
	setting := func(mode string) {
		if err := run.win.Settings.Update(context.Background(), "files.hotExit", mode, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		run.pump()
		run.settleBackups()
	}
	setting("off")
	if got := env.backupURIs(); len(got) != 0 {
		t.Errorf("backups %v after switching to off", got)
	}
	run.typeText("// y\n")
	run.delay()
	if got := env.backupURIs(); len(got) != 0 {
		t.Errorf("backups %v while off", got)
	}
	setting("onExitAndWindowClose")
	run.delay()
	if got := env.backupURIs(); len(got) != 1 {
		t.Errorf("backups %v after switching on", got)
	}
}

func TestACrashRestoresThePeriodicBackupsWhateverTheModeIs(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.typeText("// before the crash\n")
	first.command("workbench.action.files.newUntitledFile", nil)
	first.typeText("lost notes")
	first.delay()
	if !env.marker() || len(env.backups()) != 2 {
		t.Fatalf("marker %v backups %v", env.marker(), env.backupURIs())
	}
	// no RequestClose: the process dies. The user has turned hot exit off meanwhile.
	env.setHotExit("off")
	second := env.start(env.root)
	doc := second.doc(env.path("main.go"))
	if !doc.Dirty() || !strings.HasPrefix(doc.Text(), "// before the crash\n") {
		t.Errorf("main.go: dirty %v", doc.Dirty())
	}
	if notes := second.untitled(1); notes.Text() != "lost notes" {
		t.Errorf("untitled: %q", notes.Text())
	}
	if got := second.notes(); len(got) != 1 || got[0] != NoticeBackupsRestored(2) {
		t.Errorf("notifications %q, want %q", got, NoticeBackupsRestored(2))
	}
	// with the mode off the documents are asked about; giving them up discards everything
	second.win.RequestClose(CloseReasonQuit)
	second.pump()
	second.tapDialog("Don't Save")
	second.waitFor("the quit", func() bool { return second.quits == 1 })
	if got := env.backupURIs(); len(got) != 0 || env.marker() {
		t.Errorf("backups %v marker %v", got, env.marker())
	}
}

func TestACleanRestartAfterABackupExitSaysNothing(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.typeText("// x\n")
	first.exit()
	second := env.start(env.root)
	if got := second.notes(); len(got) != 0 {
		t.Errorf("notifications %q", got)
	}
}

func TestTheFileChangedBetweenRunsOpensDirtyWithOneNotice(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	path := env.path("main.go")
	first := env.start(env.root)
	first.open(path, 0)
	first.typeText("// mine\n")
	first.exit()

	external := "package other\n"
	if err := os.WriteFile(path, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	second := env.start(env.root)
	doc := second.doc(path)
	if !doc.Dirty() || !strings.HasPrefix(doc.Text(), "// mine\n") || !doc.Conflicted() {
		t.Fatalf("dirty %v conflicted %v text %q", doc.Dirty(), doc.Conflicted(), doc.Text()[:12])
	}
	want := NoticeChangedOnDisk("main.go")
	if got := second.notes(); len(got) != 1 || got[0] != want {
		t.Fatalf("notifications %q, want exactly %q", got, want)
	}
	items := second.win.Process.Notify.Items()
	var labels []string
	for _, a := range items[0].Actions {
		labels = append(labels, a.Label)
	}
	if !slices.Equal(labels, []string{"Overwrite", "Revert"}) {
		t.Errorf("buttons %v", labels)
	}
	// a save asks before overwriting: it fails and leaves the file
	second.command("workbench.action.files.save", nil)
	second.waitFor("the failed save", func() bool {
		second.ts.FlushPosted()
		return second.win.Process.Notify.Items() != nil && len(second.notes()) >= 1 && strings.Contains(strings.Join(second.notes(), "\n"), "Failed to save")
	})
	if readText(t, path) != external || !doc.Dirty() {
		t.Errorf("the save overwrote the file or lost the changes: %q", readText(t, path))
	}
	// the Overwrite button saves over the file
	var overwrite func()
	for _, it := range second.win.Process.Notify.Items() {
		for _, a := range it.Actions {
			if a.Label == "Overwrite" {
				overwrite = a.Run
			}
		}
	}
	if overwrite == nil {
		t.Fatal("no Overwrite button")
	}
	overwrite()
	second.waitFor("the overwrite", func() bool { second.ts.FlushPosted(); return !doc.Dirty() })
	if !strings.HasPrefix(readText(t, path), "// mine\n") {
		t.Errorf("file %q", readText(t, path))
	}
	second.settleBackups()
	if got := env.backupURIs(); len(got) != 0 {
		t.Errorf("backups %v", got)
	}
}

func TestTheFileDeletedBetweenRunsOpensOrphanedWithTheNotice(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	path := env.path("src/a.go")
	first := env.start(env.root)
	first.open(path, 0)
	first.typeText("// kept\n")
	wantText := first.doc(path).Text()
	first.exit()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	second := env.start(env.root)
	doc := second.doc(path)
	if !doc.Dirty() || !doc.Orphaned() || doc.Text() != wantText {
		t.Fatalf("dirty %v orphaned %v text %q", doc.Dirty(), doc.Orphaned(), doc.Text())
	}
	if got := second.notes(); len(got) != 1 || got[0] != NoticeDeletedOnDisk([]string{"a.go"}) {
		t.Fatalf("notifications %q", got)
	}
	if layout := second.layout(); len(layout) != 1 || !strings.Contains(layout[0], "a.go") {
		t.Errorf("layout %v: the editor must stay", layout)
	}
	// saving makes the file again
	second.command("workbench.action.files.save", nil)
	second.waitFor("the save", func() bool { second.ts.FlushPosted(); return !doc.Dirty() })
	if readText(t, path) != wantText {
		t.Errorf("file %q", readText(t, path))
	}
}

func TestABackupWriteThatFailsAtQuitFallsBackToTheDialog(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	var fail atomic.Bool
	opts := ProcessOptions{BackupWriteFile: func(path string, data []byte) error {
		if fail.Load() && strings.HasSuffix(path, ".json") {
			return os.ErrPermission
		}
		return storage.WriteFileAtomic(path, data)
	}}
	run := env.startWith(env.root, opts)
	run.open(env.path("main.go"), 0)
	run.typeText("// x\n")
	fail.Store(true)
	run.win.RequestClose(CloseReasonQuit)
	run.waitFor("the dialog", func() bool { run.ts.FlushPosted(); return run.win.Dialogs.IsShowing() })
	if run.quits != 0 {
		t.Fatal("the window closed although the backup failed")
	}
	// the window is not busy: Cancel keeps it open and a later request asks again
	run.tapDialog("Cancel")
	if run.quits != 0 || !run.doc(env.path("main.go")).Dirty() {
		t.Fatal("Cancel did not keep the window")
	}
	fail.Store(false)
	run.win.RequestClose(CloseReasonQuit)
	run.waitFor("the quit", func() bool { run.ts.FlushPosted(); return run.quits == 1 })
	if len(env.backups()) != 1 {
		t.Errorf("backups %v", env.backupURIs())
	}
}

func TestQuittingAfterThePeriodicBackupsKeepsThem(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	run := env.start(env.root)
	run.open(env.path("main.go"), 0)
	run.typeText("// x\n")
	run.delay()
	if len(env.backups()) != 1 {
		t.Fatalf("backups %v", env.backupURIs())
	}
	run.win.RequestClose(CloseReasonQuit) // onExit: backed up, no dialog
	run.waitFor("the quit", func() bool { run.ts.FlushPosted(); return run.quits == 1 })
	if len(env.backups()) != 1 {
		t.Errorf("backups %v", env.backupURIs())
	}
}

func TestClosingAnEditorStillAsksAndDiscardsItsBackup(t *testing.T) {
	for _, answer := range []string{"Don't Save", "Save"} {
		t.Run(answer, func(t *testing.T) {
			env := hotExitEnv(t, "onExit")
			run := env.start(env.root)
			run.open(env.path("main.go"), 0)
			run.typeText("// x\n")
			run.delay()
			if len(env.backups()) != 1 {
				t.Fatalf("backups %v", env.backupURIs())
			}
			run.command("workbench.action.closeActiveEditor", nil)
			if !run.win.Dialogs.IsShowing() {
				t.Fatal("closing an editor with unsaved changes did not ask")
			}
			run.tapDialog(answer)
			run.waitFor("the editor to close", func() bool { run.ts.FlushPosted(); return len(run.win.Documents.All()) == 0 })
			run.settleBackups()
			if got := env.backupURIs(); len(got) != 0 {
				t.Errorf("backups %v after %s", got, answer)
			}
		})
	}
}

func TestRestoredUntitledDocumentsKeepTheirNumbers(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	first := env.start(env.root)
	for range 3 {
		first.command("workbench.action.files.newUntitledFile", nil)
		first.typeText("text")
	}
	first.exit()
	// Untitled-1 is given up: only 2 and 3 have a backup
	if err := env.backupStore().Discard(context.Background(), "untitled:1"); err != nil {
		t.Fatal(err)
	}
	second := env.start(env.root)
	for _, n := range []int{2, 3} {
		if d := second.untitled(n); d.Text() != "text" {
			t.Errorf("Untitled-%d: %q", n, d.Text())
		}
	}
	if _, ok := second.win.Documents.Get(document.UntitledURI(1)); ok {
		t.Error("Untitled-1 came back without a backup")
	}
	second.command("workbench.action.files.newUntitledFile", nil)
	if _, ok := second.win.Documents.Get(document.UntitledURI(1)); !ok {
		t.Errorf("the new document is not Untitled-1: %v", second.layout())
	}
	second.command("workbench.action.files.newUntitledFile", nil)
	if _, ok := second.win.Documents.Get(document.UntitledURI(4)); !ok {
		t.Errorf("the next document is not Untitled-4: %v", second.layout())
	}
}

func TestBackupsWithoutEditorsOpenInTheActiveGroup(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.typeText("// m\n")
	first.open(env.path("e.txt"), 0)
	first.typeText("e\n")
	first.command("workbench.action.files.newUntitledFile", nil)
	first.typeText("notes")
	first.exit()
	// the saved state is lost
	id, _ := storage.WindowID(env.root)
	path, err := env.paths.WindowStatePath(storage.DefaultProfile, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	second := env.start(env.root)
	layout := second.layout()
	if len(layout) != 1 {
		t.Fatalf("layout %v", layout)
	}
	// in the order of the URIs: file:///.../e.txt, file:///.../main.go, untitled:1
	order := strings.Join(layout, "")
	if e, m, u := strings.Index(order, "e.txt"), strings.Index(order, "main.go"), strings.Index(order, "Untitled-1"); e >= m || m >= u {
		t.Errorf("layout %v: want e.txt, main.go, Untitled-1 in the order of their URIs", layout)
	}
	for _, p := range []string{env.path("main.go"), env.path("e.txt")} {
		if !second.doc(p).Dirty() {
			t.Errorf("%s is not dirty", p)
		}
	}
}

func TestACorruptBackupIsIgnoredAndTheOthersComeBack(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.typeText("// m\n")
	first.open(env.path("src/a.go"), 0)
	first.typeText("// a\n")
	first.exit()
	store := env.backupStore()
	corrupt := filepath.Join(store.Dir(), storage.BackupName(document.FileURI(env.path("src/a.go")).String()))
	if err := os.WriteFile(corrupt, []byte("{ this is not a backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(store.Dir(), "0000.json")
	if err := os.WriteFile(foreign, []byte(`{"version":1,"uri":"file:///x","content":"y"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	second := env.start(env.root)
	if !second.doc(env.path("main.go")).Dirty() {
		t.Error("main.go did not come back")
	}
	a := second.doc(env.path("src/a.go"))
	if a.Dirty() {
		t.Error("a.go came back from a corrupt backup")
	}
	for _, p := range []string{corrupt, foreign} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s was not removed (%v)", filepath.Base(p), err)
		}
	}
	if got := env.backupURIs(); len(got) != 1 {
		t.Errorf("backups %v", got)
	}
}

func TestAnOrphanedDocumentIsBackedUpWithTheFlag(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	path := env.path("main.go")
	run := env.start(env.root)
	run.open(path, 0)
	run.typeText("// x\n")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	doc := run.doc(path)
	if _, err := doc.CheckExternal(context.Background(), nil, fileio.ReadOptions{}); err != nil {
		t.Fatal(err)
	}
	run.delay()
	b := env.backups()[document.FileURI(path).String()]
	if !b.Orphaned {
		t.Errorf("backup %+v: want the orphaned flag", b)
	}
}

func TestTheNoticeOfSeveralDeletedFilesIsOne(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	first := env.start(env.root)
	for _, f := range []string{"main.go", "src/a.go"} {
		first.open(env.path(f), 0)
		first.typeText("// x\n")
	}
	first.exit()
	for _, f := range []string{"main.go", "src/a.go"} {
		if err := os.Remove(env.path(f)); err != nil {
			t.Fatal(err)
		}
	}
	second := env.start(env.root)
	if got := second.notes(); len(got) != 1 || got[0] != NoticeDeletedOnDisk([]string{"a.go", "main.go"}) {
		t.Errorf("notifications %q", got)
	}
}

func TestTheSignalThatEndsTheProgramBacksUpAndLeavesNoMarker(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process := newTestProcess(ProcessOptions{Paths: env.paths, StateAfter: env.clock.AfterFunc})
	win, err := NewWindow(ctx, process, env.root)
	if err != nil {
		t.Fatal(err)
	}
	win.PrepareRestore(ctx)
	run := &stateRun{env: env, win: win}
	run.ts = testutil.New(t, testWindowSize)
	run.ts.PumpWidget(newAppDocs(ctx, win, nil, nil, ""))
	run.pump()
	run.open(env.path("main.go"), 0)
	run.typeText("// x\n")
	if len(env.backups()) != 0 {
		t.Fatal("a backup was written before the delay")
	}
	cancel()
	run.waitFor("the backups", func() bool { run.ts.FlushPosted(); return len(env.backups()) == 1 })
	if env.marker() {
		t.Error("the marker stays after the signal")
	}
}

func TestUnmountingTheWindowWithoutClosingItKeepsTheBackupsAndTheMarker(t *testing.T) {
	env := hotExitEnv(t, "onExit")
	run := env.start(env.root)
	run.open(env.path("main.go"), 0)
	run.typeText("// x\n")
	run.delay()
	// the widgets go (the process ends, a test ends) without the window being asked to close
	run.ts.PumpWidget(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{}))
	if len(env.backups()) != 1 || !env.marker() {
		t.Errorf("backups %v marker %v", env.backupURIs(), env.marker())
	}
	// and the next start finds a crash
	second := env.start(env.root)
	if got := second.notes(); len(got) != 1 || got[0] != NoticeBackupsRestored(1) {
		t.Errorf("notifications %q", got)
	}
}

func TestAWindowWithoutAFolderKeepsItsUntitledDocuments(t *testing.T) {
	env := hotExitEnv(t, "onExitAndWindowClose")
	first := env.start("")
	first.command("workbench.action.files.newUntitledFile", nil)
	first.typeText("scratch")
	first.exit()
	second := env.start("")
	if d := second.untitled(1); d.Text() != "scratch" || !d.Dirty() {
		t.Errorf("untitled: %q dirty %v", d.Text(), d.Dirty())
	}
	// another folder has backups of its own
	other := env.start(env.folder("other", "x.go"))
	if _, ok := other.win.Documents.Get(document.UntitledURI(1)); ok {
		t.Error("the untitled document of another window came back")
	}
}
