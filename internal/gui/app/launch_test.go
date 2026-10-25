package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var launchFiles = []string{"a.go", "b.go", "c.go", "sub/d.go", "go.mod"}

// launchApp is a window over a folder with the launcher of its process attached.
type launchApp struct {
	*explorerApp
}

func newLaunchApp(t *testing.T, open ...string) *launchApp {
	t.Helper()
	return &launchApp{newExplorerApp(t, ui.Size{Width: 900, Height: 600}, launchFiles, open...)}
}

// launch runs HandleLaunch and returns the outcomes it reported.
func (a *launchApp) launch(args ipc.OpenArgs) *launchResult {
	a.t.Helper()
	r := &launchResult{}
	r.cancel = a.win.HandleLaunch(args, func(o LaunchOutcome) { r.outcomes = append(r.outcomes, o) })
	return r
}

type launchResult struct {
	outcomes []LaunchOutcome
	cancel   func()
}

// target is a file of the folder with a position.
func (a *launchApp) target(rel string, line, column int) ipc.FileTarget {
	return ipc.FileTarget{Path: a.path(rel), Line: line, Column: column}
}

// editorFor gives the editor of a file of the folder in the active group.
func (a *launchApp) editorFor(rel string) *textEditor {
	a.t.Helper()
	s := a.shell()
	doc, ok := a.win.Documents.Get(document.FileURI(a.path(rel)))
	if !ok {
		return nil
	}
	if in := s.inputFor(s.activeGroup(), doc); in != nil {
		return s.group.editors[in]
	}
	return nil
}

// openNames are the names of the files the active group has open, in the order of its tabs.
func (a *launchApp) openNames() []string {
	s := a.shell()
	var names []string
	for _, in := range s.activeGroup().Model.Editors(editorgroup.Sequential, false) {
		names = append(names, filepath.Base(in.Name()))
	}
	return names
}

func (a *launchApp) activeName() string {
	s := a.shell()
	if e := s.activeEditor(); e != nil {
		return filepath.Base(e.doc.URI().Path)
	}
	return ""
}

func (a *launchApp) notices() []string {
	var out []string
	for _, it := range a.win.Notify.Items() {
		out = append(out, it.Message)
	}
	return out
}

func wantOutcomes(t *testing.T, r *launchResult, want ...LaunchOutcome) {
	t.Helper()
	if !slices.Equal(r.outcomes, want) {
		t.Errorf("outcomes = %+v, want %+v", r.outcomes, want)
	}
}

func TestLaunchOpensFilesAndShowsTheFirst(t *testing.T) {
	a := newLaunchApp(t)
	r := a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{a.target("b.go", 0, 0), a.target("a.go", 2, 3), a.target("sub/d.go", 1, 5)}})
	// the client is told at once, before the files are read
	wantOutcomes(t, r, LaunchOutcome{})
	a.waitFor("the files", func() bool { return a.shell().editorCount() == 3 })
	if got := a.openNames(); !slices.Equal(got, []string{"b.go", "a.go", "d.go"}) {
		t.Errorf("tabs %v", got)
	}
	if got := a.activeName(); got != "b.go" {
		t.Errorf("the active editor is %q, want the first file", got)
	}
	for name, want := range map[string]buffer.Position{"a.go": {Line: 1, Col: 2}, "sub/d.go": {Line: 0, Col: 4}} {
		e := a.editorFor(name)
		if e == nil {
			t.Fatalf("no editor for %s", name)
		}
		if got := primarySelection(e).Active; got != want {
			t.Errorf("%s: the cursor is at %+v, want %+v", name, got, want)
		}
	}
	// the files are kept, not previews
	if in := a.shell().activeGroup().Model.Editors(editorgroup.Sequential, false); len(in) != 3 {
		t.Fatalf("%d editors", len(in))
	}
	for _, in := range a.shell().activeGroup().Model.Editors(editorgroup.Sequential, false) {
		if !a.shell().activeGroup().Model.IsPinned(in) {
			t.Errorf("%s is a preview", in.Name())
		}
	}
}

func TestLaunchPositionIsClampedToTheFile(t *testing.T) {
	a := newLaunchApp(t)
	a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{a.target("a.go", 999, 999)}})
	a.waitFor("the file", func() bool { return a.shell().editorCount() == 1 })
	e := a.editorFor("a.go")
	last := e.doc.Snapshot().LineCount() - 1
	if got := primarySelection(e).Active; got.Line != last {
		t.Errorf("the cursor is at %+v, want the last line %d", got, last)
	}
}

func TestLaunchOfAFileThatIsOpenShowsIt(t *testing.T) {
	a := newLaunchApp(t, "a.go", "b.go")
	if a.activeName() != "a.go" {
		t.Fatalf("active %s", a.activeName())
	}
	a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{a.target("b.go", 2, 1)}})
	a.waitFor("b.go", func() bool { return a.activeName() == "b.go" })
	if n := a.shell().editorCount(); n != 2 {
		t.Errorf("%d editors, want the two that were open", n)
	}
	if got := primarySelection(a.editorFor("b.go")).Active; got != (buffer.Position{Line: 1, Col: 0}) {
		t.Errorf("cursor %+v", got)
	}
}

func TestLaunchReportsAFileThatCannotBeOpened(t *testing.T) {
	a := newLaunchApp(t)
	missing := filepath.Join(a.root, "missing.go")
	a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{{Path: missing}, a.target("a.go", 0, 0)}})
	a.waitFor("the file", func() bool { return a.shell().editorCount() == 1 })
	a.waitFor("the error", func() bool { return len(a.notices()) > 0 })
	if n := a.notices(); len(n) != 1 || !strings.Contains(n[0], "Cannot open "+missing) {
		t.Errorf("notices %v", n)
	}
}

func TestLaunchFolder(t *testing.T) {
	other := t.TempDir()
	writeTree(t, other, "x.go", "y.go")
	t.Run("a clean window takes the folder and then the files", func(t *testing.T) {
		a := newLaunchApp(t, "a.go")
		changes := 0
		stop := a.win.OnFolderChange(func() { changes++ })
		defer stop()
		a.launch(ipc.OpenArgs{Folder: other, Files: []ipc.FileTarget{{Path: filepath.Join(other, "y.go"), Line: 2}}})
		a.waitFor("the file of the new folder", func() bool { return a.shell().editorCount() == 1 && a.activeName() == "y.go" })
		want, _ := filepath.EvalSymlinks(other)
		if got, _ := filepath.EvalSymlinks(a.win.Folder); got != want || changes != 1 {
			t.Errorf("folder %q after %d changes, want %q", a.win.Folder, changes, want)
		}
		if got := primarySelection(a.shell().activeEditor()).Active; got.Line != 1 {
			t.Errorf("cursor %+v", got)
		}
		if _, ok := a.win.Documents.Get(document.FileURI(a.path("a.go"))); ok {
			t.Error("the document of the old folder is still open")
		}
	})
	t.Run("the folder the window has is nothing to do", func(t *testing.T) {
		a := newLaunchApp(t, "a.go")
		changes := 0
		stop := a.win.OnFolderChange(func() { changes++ })
		defer stop()
		a.launch(ipc.OpenArgs{Folder: a.root, Files: []ipc.FileTarget{a.target("b.go", 0, 0)}})
		a.waitFor("the file", func() bool { return a.shell().editorCount() == 2 })
		if changes != 0 {
			t.Errorf("%d folder changes", changes)
		}
	})
	t.Run("the same folder through a link", func(t *testing.T) {
		a := newLaunchApp(t, "a.go")
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(a.root, link); err != nil {
			t.Fatal(err)
		}
		changes := 0
		stop := a.win.OnFolderChange(func() { changes++ })
		defer stop()
		a.launch(ipc.OpenArgs{Folder: link, Files: []ipc.FileTarget{a.target("b.go", 0, 0)}})
		a.waitFor("the file", func() bool { return a.shell().editorCount() == 2 })
		if changes != 0 {
			t.Errorf("%d folder changes", changes)
		}
	})
	t.Run("unsaved changes are asked about, and Cancel keeps the folder but opens the files", func(t *testing.T) {
		a := newLaunchApp(t, "a.go")
		doc, _ := a.win.Documents.Get(document.FileURI(a.path("a.go")))
		if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
		a.launch(ipc.OpenArgs{Folder: other, Files: []ipc.FileTarget{{Path: filepath.Join(other, "y.go")}}})
		a.pump()
		if !a.win.Dialogs.IsShowing() {
			t.Fatal("no question about the unsaved changes")
		}
		tapDialogButton(a, "Cancel")
		a.waitFor("the file", func() bool { return a.activeName() == "y.go" })
		if a.win.Folder != a.root {
			t.Errorf("the folder is %q after Cancel", a.win.Folder)
		}
	})
}

func tapDialogButton(a *launchApp, label string) {
	a.t.Helper()
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText(label)))
	a.pump()
}

func TestLaunchNotices(t *testing.T) {
	tests := []struct {
		name    string
		args    ipc.OpenArgs
		notices []string
		outcome []LaunchOutcome
		opens   bool
	}{
		{"merge", ipc.OpenArgs{Merge: []string{"/1", "/2", "/3", "/4"}}, []string{MergeUnavailableNotice},
			[]LaunchOutcome{{ExitCode: 1, Message: MergeUnavailableNotice}}, false},
		// the manager picks the window that has the profile; a window that is asked for another one keeps its own and says so
		{"profile of another window", ipc.OpenArgs{Profile: "work"}, []string{"The window uses the profile 'Default'."},
			[]LaunchOutcome{{}}, true},
		{"the profile of the window", ipc.OpenArgs{Profile: "default"}, nil, []LaunchOutcome{{}}, true},
		// the manager of the windows decides where a launch opens (Windows.HandleLaunch); the window itself opens files
		{"new window", ipc.OpenArgs{NewWindow: true}, nil, []LaunchOutcome{{}}, true},
		{"reuse window", ipc.OpenArgs{ReuseWindow: true}, nil, []LaunchOutcome{{}}, true},
		{"nothing", ipc.OpenArgs{}, nil, []LaunchOutcome{{}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newLaunchApp(t)
			args := tt.args
			args.Files = []ipc.FileTarget{a.target("a.go", 0, 0)}
			r := a.launch(args)
			wantOutcomes(t, r, tt.outcome...)
			a.pump()
			if tt.opens {
				a.waitFor("the file", func() bool { return a.shell().editorCount() == 1 })
			} else if n := a.shell().editorCount(); n != 0 {
				t.Errorf("%d editors opened for a launch that opens nothing", n)
			}
			if got := a.notices(); !slices.Equal(got, tt.notices) {
				t.Errorf("notices %q, want %q", got, tt.notices)
			}
		})
	}
}

func TestLaunchWaitsForTheFilesToClose(t *testing.T) {
	a := newLaunchApp(t, "go.mod")
	s := a.shell()
	r := a.launch(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0), a.target("b.go", 0, 0)}})
	a.waitFor("the files", func() bool { return s.editorCount() == 3 })
	wantOutcomes(t, r)
	closeFile := func(rel string) {
		t.Helper()
		e := a.editorFor(rel)
		s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{e.input})
		a.pump()
	}
	// a file that was open before the launch does not count
	closeFile("go.mod")
	wantOutcomes(t, r)
	closeFile("a.go")
	wantOutcomes(t, r)
	closeFile("b.go")
	wantOutcomes(t, r, LaunchOutcome{})
	// nothing ends it twice
	a.pump()
	wantOutcomes(t, r, LaunchOutcome{})
}

func TestLaunchWaitForAFileThatWasOpen(t *testing.T) {
	a := newLaunchApp(t, "a.go")
	s := a.shell()
	r := a.launch(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	a.pump()
	wantOutcomes(t, r)
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{a.editorFor("a.go").input})
	a.pump()
	wantOutcomes(t, r, LaunchOutcome{})
}

func TestLaunchWaitOnTwoLaunchesOfTheSameFile(t *testing.T) {
	a := newLaunchApp(t)
	s := a.shell()
	r1 := a.launch(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	r2 := a.launch(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0), a.target("b.go", 0, 0)}})
	a.waitFor("the files", func() bool { return s.editorCount() == 2 })
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{a.editorFor("a.go").input})
	a.pump()
	wantOutcomes(t, r1, LaunchOutcome{})
	wantOutcomes(t, r2)
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{a.editorFor("b.go").input})
	a.pump()
	wantOutcomes(t, r2, LaunchOutcome{})
}

func TestLaunchWaitWithNothingToOpen(t *testing.T) {
	tests := []struct {
		name string
		args ipc.OpenArgs
		want LaunchOutcome
	}{
		{"no files", ipc.OpenArgs{Wait: true}, LaunchOutcome{}},
		{"only files that cannot be opened", ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{{Path: "/nonexistent/x.go"}}},
			LaunchOutcome{ExitCode: 1, Message: "none of the files could be opened"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newLaunchApp(t)
			r := a.launch(tt.args)
			a.waitFor("the end of the wait", func() bool { return len(r.outcomes) > 0 })
			wantOutcomes(t, r, tt.want)
		})
	}
}

func TestLaunchWaitWithOneFileThatFailsAndOneThatOpens(t *testing.T) {
	a := newLaunchApp(t)
	s := a.shell()
	r := a.launch(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{{Path: "/nonexistent/x.go"}, a.target("a.go", 0, 0)}})
	a.waitFor("the file", func() bool { return s.editorCount() == 1 })
	wantOutcomes(t, r)
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{a.editorFor("a.go").input})
	a.pump()
	wantOutcomes(t, r, LaunchOutcome{})
}

func TestLaunchWaitEndsWhenTheWindowCloses(t *testing.T) {
	a := newLaunchApp(t)
	s := a.shell()
	r := a.launch(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	a.waitFor("the file", func() bool { return s.editorCount() == 1 })
	wantOutcomes(t, r)
	// the window goes away with the file open
	a.ts.PumpWidget(widgets.NewSizedBox(nil))
	a.pump()
	wantOutcomes(t, r, LaunchOutcome{})
	if a.win.launcher != nil {
		t.Error("the window still has a launcher")
	}
}

func TestLaunchCancelStopsTheWait(t *testing.T) {
	a := newLaunchApp(t)
	s := a.shell()
	r := a.launch(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	a.waitFor("the file", func() bool { return s.editorCount() == 1 })
	r.cancel()
	r.cancel()
	wantOutcomes(t, r, LaunchOutcome{})
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{a.editorFor("a.go").input})
	a.pump()
	wantOutcomes(t, r, LaunchOutcome{})
	if n := len(s.launches.waits); n != 0 {
		t.Errorf("%d waits are kept", n)
	}
}

func TestHandleLaunchWithoutAShell(t *testing.T) {
	win := newTestWindow(t, "")
	var got []LaunchOutcome
	cancel := win.HandleLaunch(ipc.OpenArgs{}, func(o LaunchOutcome) { got = append(got, o) })
	cancel()
	if len(got) != 1 || got[0].ExitCode != 1 {
		t.Errorf("outcomes %+v", got)
	}
}

// launcherApp is a window whose process has a launcher.
func newLauncherApp(t *testing.T, attach bool) (*launchApp, *Launcher) {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, launchFiles...)
	win := newTestWindow(t, root)
	launcher := NewLauncher()
	win.Process.Launcher = launcher
	a := &explorerApp{t: t, win: win, root: win.Folder}
	a.ts = testutil.New(t, ui.Size{Width: 900, Height: 600})
	if attach {
		a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
		a.pump()
	}
	return &launchApp{a}, launcher
}

// handle runs Launcher.Handle on a goroutine and pumps the UI until it returns.
func (a *launchApp) handle(l *Launcher, ctx context.Context, args ipc.OpenArgs) <-chan ipc.Reply {
	out := make(chan ipc.Reply, 1)
	go func() { out <- l.Handle(ctx, args) }()
	return out
}

func (a *launchApp) await(out <-chan ipc.Reply) ipc.Reply {
	a.t.Helper()
	for range 600 {
		select {
		case r := <-out:
			return r
		default:
		}
		a.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	a.t.Fatal("no reply")
	return ipc.Reply{}
}

func TestLauncherHandle(t *testing.T) {
	a, l := newLauncherApp(t, true)
	reply := a.await(a.handle(l, context.Background(), ipc.OpenArgs{Files: []ipc.FileTarget{a.target("a.go", 1, 1)}}))
	if reply.Type != ipc.TypeOK || reply.ExitCode != 0 {
		t.Errorf("reply %+v", reply)
	}
	a.waitFor("the file", func() bool { return a.shell().editorCount() == 1 })
}

func TestLauncherHandleErrorReplies(t *testing.T) {
	a, l := newLauncherApp(t, true)
	reply := a.await(a.handle(l, context.Background(), ipc.OpenArgs{Merge: []string{"/1", "/2", "/3", "/4"}}))
	if reply.Type != ipc.TypeError || reply.ExitCode != 1 || reply.Message != MergeUnavailableNotice {
		t.Errorf("reply %+v", reply)
	}
}

func TestLauncherQueuesLaunchesUntilTheWindowIsOnScreen(t *testing.T) {
	a, l := newLauncherApp(t, false)
	out := a.handle(l, context.Background(), ipc.OpenArgs{Files: []ipc.FileTarget{a.target("b.go", 0, 0)}})
	out2 := a.handle(l, context.Background(), ipc.OpenArgs{Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	// nothing is delivered before the shell attaches
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case r := <-out:
			t.Fatalf("a reply before the window: %+v", r)
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	a.ts.PumpWidget(newAppDocs(context.Background(), a.win, nil, nil, ""))
	for _, o := range []<-chan ipc.Reply{out, out2} {
		if r := a.await(o); r.Type != ipc.TypeOK {
			t.Errorf("reply %+v", r)
		}
	}
	a.waitFor("both files", func() bool { return a.shell().editorCount() == 2 })
}

func TestLauncherWaitAndDone(t *testing.T) {
	a, l := newLauncherApp(t, true)
	s := a.shell()
	out := a.handle(l, context.Background(), ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	a.waitFor("the file", func() bool { return s.editorCount() == 1 })
	select {
	case r := <-out:
		t.Fatalf("reply before the file closed: %+v", r)
	default:
	}
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{a.editorFor("a.go").input})
	if r := a.await(out); r.Type != ipc.TypeDone || r.ExitCode != 0 {
		t.Errorf("reply %+v", r)
	}
}

func TestLauncherWaitEndsWithTheWindow(t *testing.T) {
	a, l := newLauncherApp(t, true)
	s := a.shell()
	out := a.handle(l, context.Background(), ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	a.waitFor("the file", func() bool { return s.editorCount() == 1 })
	a.ts.PumpWidget(widgets.NewSizedBox(nil))
	if r := a.await(out); r.Type != ipc.TypeDone || r.ExitCode != 0 {
		t.Errorf("reply %+v", r)
	}
	// a launch after the window is gone
	if r := l.Handle(context.Background(), ipc.OpenArgs{}); r.Type != ipc.TypeError || r.Message != LauncherClosingMessage {
		t.Errorf("reply %+v", r)
	}
}

func TestLauncherCancelledContextStopsTheWait(t *testing.T) {
	a, l := newLauncherApp(t, true)
	s := a.shell()
	ctx, cancel := context.WithCancel(context.Background())
	out := a.handle(l, ctx, ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	a.waitFor("the file", func() bool { return s.editorCount() == 1 })
	cancel()
	if r := a.await(out); r.Type != ipc.TypeError {
		t.Errorf("reply %+v", r)
	}
	a.waitFor("the wait to be forgotten", func() bool { return len(s.launches.waits) == 0 })
}

func TestLauncherClosedBeforeTheWindowAnswers(t *testing.T) {
	a, l := newLauncherApp(t, false)
	out := a.handle(l, context.Background(), ipc.OpenArgs{Files: []ipc.FileTarget{a.target("a.go", 0, 0)}})
	// let the goroutine queue its launch
	var queued bool
	for i := 0; i < 500 && !queued; i++ {
		l.mu.Lock()
		queued = len(l.queue) == 1
		l.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	l.Close()
	l.Close()
	if r := a.await(out); r.Type != ipc.TypeError || r.Message != LauncherClosingMessage {
		t.Errorf("reply %+v", r)
	}
}

func TestLauncherHandlesManyClientsAtOnce(t *testing.T) {
	a, l := newLauncherApp(t, true)
	var outs []<-chan ipc.Reply
	for _, f := range []string{"a.go", "b.go", "c.go", "go.mod"} {
		outs = append(outs, a.handle(l, context.Background(), ipc.OpenArgs{Files: []ipc.FileTarget{a.target(f, 0, 0)}}))
	}
	for _, o := range outs {
		if r := a.await(o); r.Type != ipc.TypeOK {
			t.Errorf("reply %+v", r)
		}
	}
	a.waitFor("the files", func() bool { return a.shell().editorCount() == 4 })
}

func TestStartupArgumentsOfTheFirstInstance(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, launchFiles...)
	win := newTestWindow(t, root)
	win.startup = &ipc.OpenArgs{
		Files:   []ipc.FileTarget{{Path: filepath.Join(root, "a.go"), Line: 2, Column: 3}, {Path: filepath.Join(root, "b.go")}},
		Profile: "p", NewWindow: true,
	}
	var docs []*document.Document
	for _, f := range win.startup.Files {
		doc, err := win.Documents.OpenFile(context.Background(), f.Path, document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	a := &launchApp{&explorerApp{t: t, win: win, root: win.Folder, ts: testutil.New(t, ui.Size{Width: 900, Height: 600})}}
	a.ts.PumpWidget(newAppDocs(context.Background(), win, docs, nil, ""))
	a.pump()
	if got := a.activeName(); got != "a.go" {
		t.Errorf("active %q", got)
	}
	if got := primarySelection(a.editorFor("a.go")).Active; got != (buffer.Position{Line: 1, Col: 2}) {
		t.Errorf("the cursor is at %+v", got)
	}
	// the new window of the first instance is nothing to say; the window does not use the profile, and says so
	if got := a.notices(); !slices.Equal(got, []string{"The window uses the profile 'Default'."}) {
		t.Errorf("notices %q", got)
	}
	if win.startup != nil {
		t.Error("the shell did not take the startup arguments")
	}
}

func TestStartupDiffOfTheFirstInstance(t *testing.T) {
	win := newTestWindow(t, "")
	dir := t.TempDir()
	left, right := filepath.Join(dir, "old.go"), filepath.Join(dir, "new.go")
	for path, text := range map[string]string{left: "a\nb\n", right: "a\nB\n"} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	win.startup = &ipc.OpenArgs{Diff: []string{left, right}}
	a := &launchApp{&explorerApp{t: t, win: win, ts: testutil.New(t, ui.Size{Width: 900, Height: 600})}}
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.waitFor("the diff editor", func() bool { return len(a.shell().diffPanes()) == 1 })
	if got := a.openNames(); !slices.Equal(got, []string{"old.go ↔ new.go"}) {
		t.Errorf("tabs %v", got)
	}
	if got := a.notices(); len(got) != 0 {
		t.Errorf("notices %q", got)
	}
	in, ok := a.shell().activeGroup().Model.ActiveEditor().(*editorgroup.DiffInput)
	if !ok || in.Original().URI() != document.FileURI(left) || in.Modified().URI() != document.FileURI(right) {
		t.Errorf("the active editor is %v: the first file is the left side, the second the right", a.shell().activeGroup().Model.ActiveEditor())
	}
	if win.startup != nil {
		t.Error("the shell did not take the startup arguments")
	}
}

func TestDocumentsWatchClose(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "a.go", "b.go")
	d := NewDocuments()
	var mu sync.Mutex
	var closed []string
	stop := d.WatchClose(func(doc *document.Document) {
		mu.Lock()
		closed = append(closed, filepath.Base(doc.URI().Path))
		mu.Unlock()
	})
	for _, f := range []string{"a.go", "b.go"} {
		if _, err := d.Acquire(context.Background(), filepath.Join(root, f), document.NewOptions()); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := d.Get(document.FileURI(filepath.Join(root, "a.go")))
	b, _ := d.Get(document.FileURI(filepath.Join(root, "b.go")))
	if !d.Release(a) {
		t.Fatal("a.go did not close")
	}
	stop()
	stop()
	if !d.Release(b) {
		t.Fatal("b.go did not close")
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(closed, []string{"a.go"}) {
		t.Errorf("closed %v, want only a.go (b.go closed after the watch was stopped)", closed)
	}
}

// diffTargets are the two files of a --diff in the folder of the app.
func (a *launchApp) diffTargets(left, right string) []string {
	return []string{a.path(left), a.path(right)}
}

func TestLaunchDiffOpensTheDiffEditorInTheActiveGroup(t *testing.T) {
	a := newLaunchApp(t, "c.go")
	r := a.launch(ipc.OpenArgs{Diff: a.diffTargets("a.go", "b.go")})
	// the client does not wait: it is told at once
	wantOutcomes(t, r, LaunchOutcome{})
	a.waitFor("the diff editor", func() bool { return len(a.shell().diffPanes()) == 1 })
	in, ok := a.shell().activeGroup().Model.ActiveEditor().(*editorgroup.DiffInput)
	if !ok {
		t.Fatalf("the active editor is %T", a.shell().activeGroup().Model.ActiveEditor())
	}
	if in.Original().URI() != document.FileURI(a.path("a.go")) || in.Modified().URI() != document.FileURI(a.path("b.go")) {
		t.Errorf("the sides are %v and %v", in.Original().URI(), in.Modified().URI())
	}
	if !a.shell().activeGroup().Model.IsPinned(in) {
		t.Error("the diff editor is a preview")
	}
	if got := a.openNames(); !slices.Equal(got, []string{"c.go", "a.go ↔ b.go"}) {
		t.Errorf("tabs %v", got)
	}
	if got := a.notices(); len(got) != 0 {
		t.Errorf("notices %q", got)
	}
}

func TestLaunchDiffWaitsForTheDiffEditorToClose(t *testing.T) {
	a := newLaunchApp(t, "a.go")
	s := a.shell()
	r := a.launch(ipc.OpenArgs{Wait: true, Diff: a.diffTargets("a.go", "b.go")})
	a.waitFor("the diff editor", func() bool { return len(s.diffPanes()) == 1 })
	wantOutcomes(t, r)
	// the text editor of a.go is still open, and the files are used by it: it is the diff editor that counts
	in := s.activeGroup().Model.ActiveEditor()
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{in})
	a.pump()
	wantOutcomes(t, r, LaunchOutcome{})
	if len(s.diffPanes()) != 0 {
		t.Error("the diff editor is still open")
	}
}

func TestLaunchDiffWithAFileThatCannotBeOpened(t *testing.T) {
	for _, wait := range []bool{false, true} {
		name := "without --wait"
		if wait {
			name = "with --wait"
		}
		t.Run(name, func(t *testing.T) {
			a := newLaunchApp(t)
			r := a.launch(ipc.OpenArgs{Wait: wait, Diff: []string{a.path("a.go"), filepath.Join(a.root, "missing.go")}})
			a.waitFor("the notice", func() bool { return len(a.notices()) == 1 })
			if got := a.notices(); !strings.Contains(got[0], "missing.go") {
				t.Errorf("notices %q", got)
			}
			if len(a.shell().diffPanes()) != 0 || a.shell().editorCount() != 0 {
				t.Error("something was opened")
			}
			if wait {
				a.waitFor("the end of the wait", func() bool { return len(r.outcomes) == 1 })
				if r.outcomes[0].ExitCode != 1 {
					t.Errorf("outcome %+v", r.outcomes[0])
				}
			}
			if n := len(a.win.Documents.All()); n != 0 {
				// a.go was read for the diff and is let go again
				t.Errorf("%d documents are kept", n)
			}
		})
	}
}

func TestLaunchDiffNeedsTwoFiles(t *testing.T) {
	a := newLaunchApp(t)
	r := a.launch(ipc.OpenArgs{Wait: true, Diff: []string{a.path("a.go")}})
	a.pump()
	if got := a.notices(); !slices.Equal(got, []string{"--diff needs two files"}) {
		t.Errorf("notices %q", got)
	}
	wantOutcomes(t, r, LaunchOutcome{ExitCode: 1, Message: "none of the files could be opened"})
}
