package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The placement of launches and of Open Folder in the tabs of the one window (step 9b.3, SPEC §10.14), driven the way a
// second `emetos` drives it: over the real socket of the first instance, in a runtime folder of the test.

// socketApp is a run of the program that listens on a real socket (core/ipc) and hands what arrives to its Launcher.
type socketApp struct {
	*multiApp
	loc ipc.Location
}

// runSocket starts the program the way main does for the first instance: the socket is joined, served by the launcher
// of the process, and the window is shown with the command line of the first instance.
func (e *multiEnv) runSocket(opts Options) *socketApp {
	e.t.Helper()
	launcher := NewLauncher()
	a := e.runWith(opts, ProcessOptions{Launcher: launcher})
	loc, err := ipc.Locations(e.paths, os.Getuid())
	if err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.t.Context())
	srv, client, err := ipc.Join(ctx, ipc.Options{Location: loc})
	if err != nil || srv == nil || client != nil {
		cancel()
		e.t.Fatalf("Join: server %v, client %v, err %v", srv, client, err)
	}
	served := make(chan error, 1)
	go func() {
		served <- srv.Serve(ctx, func(hctx context.Context, req ipc.Request) ipc.Reply {
			return launcher.Handle(hctx, *req.Open)
		})
	}()
	e.t.Cleanup(func() {
		launcher.Close()
		cancel()
		if err := <-served; err != nil {
			e.t.Errorf("Serve = %v", err)
		}
	})
	return &socketApp{multiApp: a, loc: loc}
}

// sentLaunch is a launch that a client sent over the socket and has not been answered yet.
type sentLaunch struct {
	ch    chan ipc.Reply
	reply *ipc.Reply
}

// send makes a second `emetos` hand a command line over: a client in its own goroutine sends the request and waits
// for the reply, while the test pumps the window.
func (a *socketApp) send(args ipc.OpenArgs) *sentLaunch {
	a.t.Helper()
	s := &sentLaunch{ch: make(chan ipc.Reply, 1)}
	go func() {
		client, err := ipc.Dial(a.ctx, ipc.Options{Location: a.loc})
		if err != nil {
			s.ch <- ipc.Failed("dial: " + err.Error())
			return
		}
		defer func() { _ = client.Close() }()
		reply, err := client.Send(a.ctx, ipc.Request{Cwd: "/", Open: &args})
		if err != nil {
			reply = ipc.Failed("send: " + err.Error())
		}
		s.ch <- reply
	}()
	return s
}

// answered pumps until the instance answered the launch, and gives the reply.
func (a *socketApp) answered(s *sentLaunch) ipc.Reply {
	a.t.Helper()
	if s.reply == nil {
		a.waitFor("the reply of the instance", func() bool {
			select {
			case r := <-s.ch:
				s.reply = &r
			default:
			}
			return s.reply != nil
		})
	}
	return *s.reply
}

// pending tells whether the instance has not answered yet (after pumping for a while).
func (a *socketApp) pending(s *sentLaunch) bool {
	a.t.Helper()
	a.settle()
	select {
	case r := <-s.ch:
		s.reply = &r
	default:
	}
	return s.reply == nil
}

// launch hands a command line over and waits for the instance to answer with "ok" (or "done" with --wait).
func (a *socketApp) launch(args ipc.OpenArgs) {
	a.t.Helper()
	r := a.answered(a.send(args))
	if r.Type == ipc.TypeError {
		a.t.Fatalf("the launch %+v failed: %+v", args, r)
	}
	a.settle()
}

// tabFolders are the folders of the tabs in the order of the strip, with the base name only.
func (a *socketApp) tabNames() []string {
	var out []string
	for _, w := range a.m.All() {
		if w.Folder == "" {
			out = append(out, "")
			continue
		}
		out = append(out, filepath.Base(w.Folder))
	}
	return out
}

func (a *socketApp) activeName() string {
	if w := a.m.Active(); w != nil && w.Folder != "" {
		return filepath.Base(w.Folder)
	}
	return ""
}

// docNames are the base names of the documents a workspace has open, sorted.
func docNames(w *Workspace) []string {
	var out []string
	for _, d := range w.Documents.All() {
		out = append(out, filepath.Base(d.URI().Path))
	}
	slices.Sort(out)
	return out
}

// socketTabs starts the program over the folders a, b and c in this order (a is the first instance's folder), each with
// a main.go, and the folder d which no tab has.
func socketTabs(t *testing.T, extra map[string]any) (*socketApp, map[string]string) {
	t.Helper()
	env := newMultiEnv(t, extra)
	dirs := map[string]string{}
	for _, name := range []string{"a", "b", "c", "d"} {
		dirs[name] = env.folder(name, "main.go", "util.go")
	}
	a := env.runSocket(Options{Folder: dirs["a"]})
	for _, name := range []string{"b", "c"} {
		if _, err := a.m.Open(WorkspaceSpec{Folder: dirs[name]}); err != nil {
			t.Fatal(err)
		}
	}
	a.settle()
	return a, dirs
}

func TestSocketTwoLaunchesOfTwoFoldersGiveOneWindowWithTwoTabs(t *testing.T) {
	env := newMultiEnv(t, nil)
	api := env.folder("api", "main.go")
	notes := env.folder("notes", "main.go")
	// the first launch is the command line of the first instance, the second is handed over the socket
	a := env.runSocket(Options{Folder: api})
	if got := a.tabNames(); !slices.Equal(got, []string{"api"}) {
		t.Fatalf("tabs %q after the first launch", got)
	}
	a.launch(ipc.OpenArgs{Folder: notes})
	if got := a.tabNames(); !slices.Equal(got, []string{"api", "notes"}) {
		t.Errorf("tabs %q, want api and notes", got)
	}
	if a.activeName() != "notes" {
		t.Errorf("the active tab is %q, want the one that was launched", a.activeName())
	}
	if got := a.otherWindows(); len(got) != 0 {
		t.Errorf("%d Glutter windows were made for the tabs", len(got))
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestSocketTheSameFolderFocusesItsTab(t *testing.T) {
	a, dirs := socketTabs(t, nil)
	link := filepath.Join(t.TempDir(), "link-to-a")
	if err := os.Symlink(dirs["a"], link); err != nil {
		t.Fatal(err)
	}
	// every request names a folder some tab has: whatever else is asked, that tab is the active one and no tab is made
	for _, tc := range []struct {
		name string
		args ipc.OpenArgs
		want string
	}{
		{"the folder", ipc.OpenArgs{Folder: dirs["b"]}, "b"},
		{"the folder again", ipc.OpenArgs{Folder: dirs["b"]}, "b"},
		{"through a link", ipc.OpenArgs{Folder: link}, "a"},
		{"with --new-window", ipc.OpenArgs{Folder: dirs["c"], NewWindow: true}, "c"},
		{"with --reuse-window", ipc.OpenArgs{Folder: dirs["a"], ReuseWindow: true}, "a"},
	} {
		a.launch(tc.args)
		if a.activeName() != tc.want || a.m.Count() != 3 {
			t.Errorf("%s: active %q with %d tabs, want %q and 3", tc.name, a.activeName(), a.m.Count(), tc.want)
		}
	}
	if got := a.tabNames(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("tabs %q", got)
	}
}

func TestSocketNewWindowOpensANewTabAfterTheActiveOne(t *testing.T) {
	// "off" would replace the active tab: -n wins over the setting
	a, dirs := socketTabs(t, map[string]any{"window.openFoldersInNewWindow": "off"})
	a.m.ActivateIndex(0)
	a.launch(ipc.OpenArgs{Folder: dirs["d"], NewWindow: true})
	if got := a.tabNames(); !slices.Equal(got, []string{"a", "d", "b", "c"}) || a.activeName() != "d" {
		t.Errorf("tabs %q, active %q, want a d b c with d active", got, a.activeName())
	}
}

func TestSocketReuseWindowReplacesTheActiveTabAfterTheQuestions(t *testing.T) {
	a, dirs := socketTabs(t, nil)
	a.m.ActivateIndex(1)
	b := a.workspaceOf(dirs["b"])
	id := b.Tab().ID()
	a.edit(b, filepath.Join(dirs["b"], "main.go"))

	// Cancel keeps the tab as it was
	s := a.send(ipc.OpenArgs{Folder: dirs["d"], ReuseWindow: true})
	a.waitFor("the question", func() bool { return a.dialog(b) != "" })
	if got := a.dialog(b); got != askAbout("main.go") {
		t.Fatalf("the question is %q", got)
	}
	if !a.pending(s) {
		t.Fatal("the launch was answered before the question was")
	}
	a.answer(b, "Cancel")
	if r := a.answered(s); r.Type != ipc.TypeOK {
		t.Errorf("reply %+v", r)
	}
	if got := a.tabNames(); !slices.Equal(got, []string{"a", "b", "c"}) || a.m.Active() != b {
		t.Fatalf("tabs %q after Cancel", got)
	}

	// Don't Save replaces the active tab, in its place, and the others stay
	s = a.send(ipc.OpenArgs{Folder: dirs["d"], ReuseWindow: true})
	a.waitFor("the question", func() bool { return a.dialog(b) != "" })
	a.answer(b, "Don't Save")
	if r := a.answered(s); r.Type != ipc.TypeOK {
		t.Errorf("reply %+v", r)
	}
	a.waitFor("the folder", func() bool { return a.m.workspaceForFolder(dirs["d"]) != nil })
	a.settle()
	if got := a.tabNames(); !slices.Equal(got, []string{"a", "d", "c"}) {
		t.Errorf("tabs %q, want a d c", got)
	}
	if d := a.workspaceOf(dirs["d"]); d.Tab().ID() != id || a.m.Active() != d {
		t.Errorf("the replaced tab changed its identity or is not the active one")
	}
}

func TestSocketAnUntouchedNoFolderTabIsReplaced(t *testing.T) {
	t.Run("a launch with the default setting", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		api := env.folder("api", "main.go")
		a := env.runSocket(Options{})
		a.launch(ipc.OpenArgs{Folder: api})
		if got := a.tabNames(); !slices.Equal(got, []string{"api"}) {
			t.Errorf("tabs %q, want the No Folder tab replaced", got)
		}
	})
	t.Run("a No Folder tab with an editor is not", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		api := env.folder("api", "main.go")
		a := env.runSocket(Options{})
		a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{{Path: filepath.Join(api, "main.go")}}})
		first := a.m.Active()
		a.waitFor("the file", func() bool { return len(first.Documents.All()) == 1 })
		if first.emptyWorkspace() {
			t.Fatal("a tab with a file is empty")
		}
		a.launch(ipc.OpenArgs{Folder: api})
		if got := a.tabNames(); !slices.Equal(got, []string{"", "api"}) {
			t.Errorf("tabs %q, want the tab with the file kept", got)
		}
	})
	t.Run("a tab that was used and emptied is untouched again", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		api := env.folder("api", "main.go")
		a := env.runSocket(Options{})
		w := a.m.Active()
		if !w.emptyWorkspace() {
			t.Fatal("a new No Folder tab is not empty")
		}
		a.launch(ipc.OpenArgs{Folder: api})
		if a.m.Count() != 1 || a.m.Active().Folder != api {
			t.Errorf("tabs %q", a.tabNames())
		}
	})
	t.Run("--new-window and the setting on keep it", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			extra map[string]any
			args  ipc.OpenArgs
		}{
			{"--new-window", nil, ipc.OpenArgs{NewWindow: true}},
			{"on", map[string]any{"window.openFoldersInNewWindow": "on"}, ipc.OpenArgs{}},
		} {
			env := newMultiEnv(t, tc.extra)
			api := env.folder("api", "main.go")
			a := env.runSocket(Options{})
			args := tc.args
			args.Folder = api
			a.launch(args)
			if got := a.tabNames(); !slices.Equal(got, []string{"", "api"}) {
				t.Errorf("%s: tabs %q, want a new tab", tc.name, got)
			}
		}
	})
}

func TestSocketOpenFoldersInNewWindowIsReadAsInANewTab(t *testing.T) {
	for _, tc := range []struct {
		setting string
		want    []string
	}{
		{"on", []string{"a", "b", "d", "c"}},
		{"off", []string{"a", "d", "c"}},
		{"default", []string{"a", "b", "d", "c"}},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			a, dirs := socketTabs(t, map[string]any{"window.openFoldersInNewWindow": tc.setting})
			a.m.ActivateIndex(1)
			a.launch(ipc.OpenArgs{Folder: dirs["d"]})
			if got := a.tabNames(); !slices.Equal(got, tc.want) {
				t.Errorf("tabs %q, want %q", got, tc.want)
			}
			if a.activeName() != "d" {
				t.Errorf("the active tab is %q", a.activeName())
			}
		})
	}
}

func TestSocketFilesOpenInTheActiveTab(t *testing.T) {
	a, dirs := socketTabs(t, nil)
	a.m.ActivateIndex(0)
	// the files are of the folder of the third tab, and open in the active one all the same
	a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{
		{Path: filepath.Join(dirs["c"], "main.go")},
		{Path: filepath.Join(dirs["c"], "util.go"), Line: 2, Column: 4},
	}})
	w := a.m.Active()
	a.waitFor("the files", func() bool { return len(w.Documents.All()) == 2 })
	if w.Folder != dirs["a"] || a.m.Count() != 3 {
		t.Errorf("active %q with %d tabs", w.Folder, a.m.Count())
	}
	for _, other := range []string{"b", "c"} {
		if got := docNames(a.workspaceOf(dirs[other])); len(got) != 0 {
			t.Errorf("tab %s opened %v", other, got)
		}
	}
	// the first file is the one shown; --goto's line and column place the cursor
	s := w.shell
	if got := filepath.Base(s.activeEditor().doc.URI().Path); got != "main.go" {
		t.Errorf("the shown file is %q, want the first", got)
	}
	doc, _ := w.Documents.Get(document.FileURI(filepath.Join(dirs["c"], "util.go")))
	e := s.group.editors[s.inputFor(s.activeGroup(), doc)]
	if got := primarySelection(e).Active; got != (buffer.Position{Line: 1, Col: 3}) {
		t.Errorf("the cursor is at %+v, want line 2 column 4", got)
	}
}

func TestSocketFilesAndOpenFilesInNewWindow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		extra   map[string]any
		args    ipc.OpenArgs
		wantNew bool
	}{
		{"off", map[string]any{"window.openFilesInNewWindow": "off"}, ipc.OpenArgs{}, false},
		{"default", map[string]any{"window.openFilesInNewWindow": "default"}, ipc.OpenArgs{}, false},
		{"on", map[string]any{"window.openFilesInNewWindow": "on"}, ipc.OpenArgs{}, true},
		{"on with --reuse-window", map[string]any{"window.openFilesInNewWindow": "on"}, ipc.OpenArgs{ReuseWindow: true}, false},
		{"off with --new-window", map[string]any{"window.openFilesInNewWindow": "off"}, ipc.OpenArgs{NewWindow: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, dirs := socketTabs(t, tc.extra)
			args := tc.args
			args.Files = []ipc.FileTarget{{Path: filepath.Join(dirs["a"], "main.go")}}
			a.launch(args)
			w := a.m.Active()
			a.waitFor("the file", func() bool { return len(w.Documents.All()) == 1 })
			if tc.wantNew {
				if a.m.Count() != 4 || w.Folder != "" {
					t.Errorf("%d tabs, active folder %q, want a new No Folder tab", a.m.Count(), w.Folder)
				}
				return
			}
			if a.m.Count() != 3 || w.Folder != dirs["c"] {
				t.Errorf("%d tabs, active folder %q, want the active tab", a.m.Count(), w.Folder)
			}
		})
	}
}

func TestSocketAFolderWithFilesOpensThemInTheTabThatGotTheFolder(t *testing.T) {
	a, dirs := socketTabs(t, nil)
	a.launch(ipc.OpenArgs{Folder: dirs["a"], Files: []ipc.FileTarget{{Path: filepath.Join(dirs["a"], "util.go"), Line: 2}}})
	w := a.workspaceOf(dirs["a"])
	a.waitFor("the file", func() bool { return len(w.Documents.All()) == 1 })
	if a.m.Active() != w || a.m.Count() != 3 {
		t.Errorf("active %q with %d tabs", a.m.Active().Folder, a.m.Count())
	}
	if got := primarySelection(w.shell.activeEditor()).Active; got.Line != 1 {
		t.Errorf("the cursor is at %+v", got)
	}
}

func TestSocketDiffAndMergeOpenTheirEditorInTheActiveTab(t *testing.T) {
	a, dirs := socketTabs(t, nil)
	a.m.ActivateIndex(1)
	b := a.m.Active()
	other := a.workspaceOf(dirs["c"])
	a.launch(ipc.OpenArgs{Diff: []string{filepath.Join(dirs["a"], "main.go"), filepath.Join(dirs["a"], "util.go")}})
	a.waitFor("the diff editor", func() bool { return len(b.shell.diffPanes()) == 1 })
	if len(other.shell.diffPanes()) != 0 || a.m.Active() != b || a.m.Count() != 3 {
		t.Errorf("the diff editor is not only in the active tab (%d tabs)", a.m.Count())
	}

	dir := t.TempDir()
	paths := make([]string, 4)
	for i, text := range []string{"a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n"} {
		paths[i] = filepath.Join(dir, []string{"in1", "in2", "base", "result"}[i]+".txt")
		if err := os.WriteFile(paths[i], []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.launch(ipc.OpenArgs{Merge: paths})
	a.waitFor("the merge editor", func() bool { return b.shell.activeMergePane() != nil })
	if other.shell.activeMergePane() != nil || a.m.Count() != 3 {
		t.Error("the merge editor is not only in the active tab")
	}
}

func TestSocketWaitReturnsWhenTheFilesAreClosed(t *testing.T) {
	t.Run("files", func(t *testing.T) {
		a, dirs := socketTabs(t, nil)
		w := a.m.Active()
		s := a.send(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{
			{Path: filepath.Join(dirs["a"], "main.go")}, {Path: filepath.Join(dirs["a"], "util.go")}}})
		a.waitFor("the files", func() bool { return len(w.Documents.All()) == 2 })
		if !a.pending(s) {
			t.Fatal("the instance answered before the files were closed")
		}
		sh := w.shell
		docs := w.Documents.All()
		sh.closeEditors(sh.activeGroup(), []editorgroup.EditorInput{sh.inputFor(sh.activeGroup(), docs[0])})
		a.settle()
		if !a.pending(s) {
			t.Fatal("the instance answered with a file still open")
		}
		sh.closeEditors(sh.activeGroup(), []editorgroup.EditorInput{sh.inputFor(sh.activeGroup(), docs[1])})
		if r := a.answered(s); r.Type != ipc.TypeDone || r.ExitCode != 0 {
			t.Errorf("reply %+v, want done with exit code 0", r)
		}
	})
	t.Run("the tab closes", func(t *testing.T) {
		a, dirs := socketTabs(t, nil)
		w := a.m.Active()
		s := a.send(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{{Path: filepath.Join(dirs["a"], "main.go")}}})
		a.waitFor("the file", func() bool { return len(w.Documents.All()) == 1 })
		a.m.Close(w.Tab(), nil)
		if r := a.answered(s); r.Type != ipc.TypeDone || r.ExitCode != 0 {
			t.Errorf("reply %+v", r)
		}
	})
	t.Run("a diff ends when its editor closes", func(t *testing.T) {
		a, dirs := socketTabs(t, nil)
		w := a.m.Active()
		s := a.send(ipc.OpenArgs{Wait: true, Diff: []string{filepath.Join(dirs["a"], "main.go"), filepath.Join(dirs["a"], "util.go")}})
		a.waitFor("the diff editor", func() bool { return len(w.shell.diffPanes()) == 1 })
		if !a.pending(s) {
			t.Fatal("the instance answered before the diff editor closed")
		}
		sh := w.shell
		sh.closeEditors(sh.activeGroup(), []editorgroup.EditorInput{sh.activeGroup().Model.ActiveEditor()})
		if r := a.answered(s); r.Type != ipc.TypeDone || r.ExitCode != 0 {
			t.Errorf("reply %+v", r)
		}
	})
	t.Run("a merge ends with 0 when it is completed and with 1 when it is not", func(t *testing.T) {
		for _, complete := range []bool{true, false} {
			a, _ := socketTabs(t, nil)
			w := a.m.Active()
			dir := t.TempDir()
			paths := make([]string, 4)
			for i, text := range []string{"a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n"} {
				paths[i] = filepath.Join(dir, []string{"in1", "in2", "base", "result"}[i]+".txt")
				if err := os.WriteFile(paths[i], []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s := a.send(ipc.OpenArgs{Wait: true, Merge: paths})
			a.waitFor("the merge editor", func() bool { return w.shell.activeMergePane() != nil })
			if !a.pending(s) {
				t.Fatal("the instance answered before the merge editor closed")
			}
			if complete {
				if err := w.shell.activeMergePane().Model().AcceptAll(diff.Input1); err != nil {
					t.Fatal(err)
				}
				if err := a.command(w, "mergeEditor.acceptMerge", nil); err != nil {
					t.Fatal(err)
				}
				if r := a.answered(s); r.Type != ipc.TypeDone || r.ExitCode != 0 {
					t.Errorf("completed: reply %+v", r)
				}
				continue
			}
			// the merge editor has changes of its own: closing it asks, and Don't Save gives the merge up
			sh := w.shell
			sh.closeEditors(sh.activeGroup(), []editorgroup.EditorInput{sh.activeMergePane().input})
			a.settle()
			if a.dialog(w) != "" {
				a.answer(w, "Don't Save")
			}
			if r := a.answered(s); r.Type != ipc.TypeError || r.ExitCode != 1 || r.Message != MergeNotCompletedNotice {
				t.Errorf("closed: reply %+v", r)
			}
		}
	})
}

func TestSocketProfileRules(t *testing.T) {
	setup := func(t *testing.T) (*socketApp, string, string) {
		env := newMultiEnv(t, nil)
		workProfile(t, env)
		one := env.folder("one", "a.go")
		two := env.folder("two", "b.go")
		return env.runSocket(Options{Folder: one}), one, two
	}
	notices := func(w *Workspace) []string {
		var out []string
		for _, it := range w.Notify.Items() {
			out = append(out, it.Message)
		}
		return out
	}
	t.Run("a folder that is not open opens in a new tab with the profile", func(t *testing.T) {
		a, one, two := setup(t)
		a.launch(ipc.OpenArgs{Folder: two, Profile: "Work"})
		if a.m.Count() != 2 || a.workspaceOf(two).ProfileName() != "Work" {
			t.Fatalf("%d tabs, profile %q", a.m.Count(), a.workspaceOf(two).ProfileName())
		}
		if w := a.workspaceOf(one); w.ProfileName() != "Default" || len(notices(w)) != 0 {
			t.Errorf("the first tab uses %q: %v", w.ProfileName(), notices(w))
		}
		if a.m.Active() != a.workspaceOf(two) {
			t.Error("the new tab is not the active one")
		}
	})
	t.Run("a folder that is open with another profile becomes the active tab and says so", func(t *testing.T) {
		a, one, two := setup(t)
		a.launch(ipc.OpenArgs{Folder: two, Profile: "Work"})
		a.launch(ipc.OpenArgs{Folder: one, Profile: "Work"})
		w := a.workspaceOf(one)
		if a.m.Count() != 2 || a.m.Active() != w || w.ProfileName() != "Default" {
			t.Fatalf("%d tabs, active %q, profile %q", a.m.Count(), a.m.Active().Folder, w.ProfileName())
		}
		if got := notices(w); !slices.Equal(got, []string{"The folder is already open with the profile 'Default'."}) {
			t.Errorf("notices %q", got)
		}
	})
	t.Run("files use the active tab when it has the profile, else a new No Folder tab with it", func(t *testing.T) {
		a, one, _ := setup(t)
		file := ipc.FileTarget{Path: filepath.Join(one, "a.go")}
		a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{file}, Profile: "default"})
		if a.m.Count() != 1 {
			t.Errorf("%d tabs after files with the profile of the tab", a.m.Count())
		}
		a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{file}, Profile: "Work"})
		w := a.m.Active()
		if a.m.Count() != 2 || w.ProfileName() != "Work" || w.Folder != "" {
			t.Fatalf("%d tabs, active profile %q folder %q", a.m.Count(), w.ProfileName(), w.Folder)
		}
		a.waitFor("the file", func() bool { return len(w.Documents.All()) == 1 })
		// the tab has the profile now: the next launch with it uses it
		a.launch(ipc.OpenArgs{Files: []ipc.FileTarget{{Path: filepath.Join(one, "a.go"), Line: 1}}, Profile: "Work"})
		if a.m.Count() != 2 {
			t.Errorf("%d tabs after files with the profile of the active tab", a.m.Count())
		}
	})
	t.Run("a launch with only a profile and --reuse-window", func(t *testing.T) {
		a, _, _ := setup(t)
		a.launch(ipc.OpenArgs{Profile: "Work"})
		if a.m.Count() != 2 || a.m.Active().ProfileName() != "Work" {
			t.Fatalf("%d tabs", a.m.Count())
		}
		a.launch(ipc.OpenArgs{Profile: "Default", ReuseWindow: true})
		if a.m.Count() != 2 {
			t.Errorf("%d tabs", a.m.Count())
		}
		if got := notices(a.m.Active()); !slices.Equal(got, []string{"The window uses the profile 'Work'."}) {
			t.Errorf("notices %q", got)
		}
	})
	t.Run("an untouched No Folder tab is replaced by the folder with the profile", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		workProfile(t, env)
		two := env.folder("two", "b.go")
		a := env.runSocket(Options{})
		a.launch(ipc.OpenArgs{Folder: two, Profile: "Work"})
		if a.m.Count() != 1 || a.workspaceOf(two).ProfileName() != "Work" {
			t.Errorf("%d tabs", a.m.Count())
		}
	})
}

// hideWindow takes the window off the screen, as the tray of step 9b.4 does (but at once, with no backups and no frame).
func (a *socketApp) hideWindow() {
	a.t.Helper()
	c, ok := a.m.implicitController()
	if !ok {
		a.t.Fatal("the window has no controller")
	}
	c.SetHidden(true)
	a.settle()
	if !c.IsHidden() || !a.ts.WindowHidden(ui.ImplicitViewID) {
		a.t.Fatal("the window is not hidden")
	}
}

func (a *socketApp) windowHidden() bool { return a.ts.WindowHidden(ui.ImplicitViewID) }

func TestSocketALaunchShowsAHiddenWindowAndBringsItToTheFront(t *testing.T) {
	for _, tc := range []struct {
		name string
		args func(dirs map[string]string) ipc.OpenArgs
	}{
		{"emetos with no argument", func(map[string]string) ipc.OpenArgs { return ipc.OpenArgs{} }},
		{"a folder", func(d map[string]string) ipc.OpenArgs { return ipc.OpenArgs{Folder: d["d"]} }},
		{"a folder that is open", func(d map[string]string) ipc.OpenArgs { return ipc.OpenArgs{Folder: d["a"]} }},
		{"files", func(d map[string]string) ipc.OpenArgs {
			return ipc.OpenArgs{Files: []ipc.FileTarget{{Path: filepath.Join(d["a"], "main.go")}}}
		}},
		{"--new-window", func(map[string]string) ipc.OpenArgs { return ipc.OpenArgs{NewWindow: true} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, dirs := socketTabs(t, nil)
			a.hideWindow()
			before := len(a.ts.ViewActions(ui.ImplicitViewID))
			a.launch(tc.args(dirs))
			if a.windowHidden() {
				t.Error("the window is still hidden after the launch")
			}
			acts := a.ts.ViewActions(ui.ImplicitViewID)
			if !slices.ContainsFunc(acts[before:], func(w ui.WindowAction) bool { return w.Kind == ui.WindowActionActivate }) {
				t.Errorf("the window was not asked to come to the front: %v", acts[before:])
			}
		})
	}
}

func TestShowWindowIsTheSeamOfTheTray(t *testing.T) {
	a, _ := socketTabs(t, nil)
	// a window that is shown stays shown, and is asked to come to the front
	before := len(a.ts.ViewActions(ui.ImplicitViewID))
	a.m.showWindow(nil)
	if a.windowHidden() || len(a.ts.ViewActions(ui.ImplicitViewID)) == before {
		t.Error("showing a shown window did not activate it")
	}
	a.hideWindow()
	a.m.showWindow(nil)
	a.settle()
	if a.windowHidden() {
		t.Error("showWindow left the window hidden")
	}
	// a workspace that raises itself shows the window too
	a.hideWindow()
	a.m.All()[0].Raise()
	a.settle()
	if a.windowHidden() || a.m.Active() != a.m.All()[0] {
		t.Error("Raise did not show the window and activate the tab")
	}
}

func TestOpenFolderFromInsideFollowsTheSameRule(t *testing.T) {
	pick := func(t *testing.T, a *socketApp, folder string) {
		t.Helper()
		fd := a.ts.FakeDesktop()
		w := a.m.Active()
		if err := a.command(w, "workbench.action.files.openFolder", nil); err != nil {
			t.Fatal(err)
		}
		a.waitFor("the folder dialog", func() bool { return fd.PendingFileChoosers() == 1 })
		fd.AnswerFileChooser(folder)
		a.waitFor("the folder", func() bool { return a.m.workspaceForFolder(folder) != nil })
		a.settle()
	}
	for _, tc := range []struct {
		setting string
		want    []string
	}{
		{"default", []string{"a", "b", "d", "c"}},
		{"on", []string{"a", "b", "d", "c"}},
		{"off", []string{"a", "d", "c"}},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			a, dirs := socketTabs(t, map[string]any{"window.openFoldersInNewWindow": tc.setting})
			a.m.ActivateIndex(1)
			pick(t, a, dirs["d"])
			if got := a.tabNames(); !slices.Equal(got, tc.want) || a.activeName() != "d" {
				t.Errorf("tabs %q active %q, want %q", got, a.activeName(), tc.want)
			}
		})
	}
	t.Run("a folder that a tab has becomes the active tab", func(t *testing.T) {
		a, dirs := socketTabs(t, nil)
		pick(t, a, dirs["a"])
		if a.activeName() != "a" || a.m.Count() != 3 {
			t.Errorf("active %q with %d tabs", a.activeName(), a.m.Count())
		}
	})
	t.Run("an untouched No Folder tab is replaced", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		api := env.folder("api", "main.go")
		a := env.runSocket(Options{})
		pick(t, a, api)
		if got := a.tabNames(); !slices.Equal(got, []string{"api"}) {
			t.Errorf("tabs %q", got)
		}
	})
	t.Run("a cancelled dialog changes nothing", func(t *testing.T) {
		a, _ := socketTabs(t, nil)
		fd := a.ts.FakeDesktop()
		if err := a.command(a.m.Active(), "workbench.action.files.openFolder", nil); err != nil {
			t.Fatal(err)
		}
		a.waitFor("the folder dialog", func() bool { return fd.PendingFileChoosers() == 1 })
		fd.CancelFileChooser()
		a.settle()
		if a.m.Count() != 3 {
			t.Errorf("%d tabs", a.m.Count())
		}
	})
}

// recentRow gives the row of the Open Recent picker that is the folder.
func (a *socketApp) recentRow(folder string) quickinput.Row {
	a.t.Helper()
	w := a.m.Active()
	if err := a.command(w, "workbench.action.openRecent", nil); err != nil {
		a.t.Fatal(err)
	}
	a.waitFor("the picker", func() bool { return w.QuickInput.Visible() })
	for _, r := range w.QuickInput.Picker().Rows() {
		if r.Item.ID == folder {
			return r
		}
	}
	a.t.Fatalf("no row for %s", folder)
	return quickinput.Row{}
}

func TestOpenRecentFollowsTheRuleAndTheCtrlKeyForcesANewTab(t *testing.T) {
	remember := func(a *socketApp, folder string) {
		t.Helper()
		err := a.p.State.UpdateGlobal(a.ctx, func(g *storage.GlobalState) bool { return g.Recent.AddFolder(folder) })
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name    string
		setting string
		ctrl    bool
		want    []string
	}{
		{"off replaces the active tab", "off", false, []string{"a", "d", "c"}},
		{"off with Ctrl opens a new tab", "off", true, []string{"a", "b", "d", "c"}},
		{"default opens a new tab", "default", false, []string{"a", "b", "d", "c"}},
		{"default with Ctrl opens a new tab", "default", true, []string{"a", "b", "d", "c"}},
		{"on opens a new tab", "on", false, []string{"a", "b", "d", "c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, dirs := socketTabs(t, map[string]any{"window.openFoldersInNewWindow": tc.setting})
			remember(a, dirs["d"])
			a.m.ActivateIndex(1)
			row := a.recentRow(dirs["d"])
			a.m.Active().QuickInput.AcceptRow(row, quickinput.KeyMods{Ctrl: tc.ctrl})
			a.waitFor("the folder", func() bool { return a.m.workspaceForFolder(dirs["d"]) != nil })
			a.settle()
			if got := a.tabNames(); !slices.Equal(got, tc.want) || a.activeName() != "d" {
				t.Errorf("tabs %q active %q, want %q", got, a.activeName(), tc.want)
			}
		})
	}
	t.Run("the menu row (the command) follows the rule", func(t *testing.T) {
		a, dirs := socketTabs(t, map[string]any{"window.openFoldersInNewWindow": "off"})
		a.m.ActivateIndex(1)
		w := a.m.Active()
		err := w.Commands.Run(command.Context{Ctx: a.ctx, Keys: w.Keys, Source: command.SourceAPI}, "openRecentFolder", dirs["d"])
		if err != nil {
			t.Fatal(err)
		}
		a.waitFor("the folder", func() bool { return a.m.workspaceForFolder(dirs["d"]) != nil })
		a.settle()
		if got := a.tabNames(); !slices.Equal(got, []string{"a", "d", "c"}) {
			t.Errorf("tabs %q", got)
		}
	})
}

func TestCloseFolderTurnsTheActiveTabIntoANoFolderTab(t *testing.T) {
	a, dirs := socketTabs(t, nil)
	a.m.ActivateIndex(1)
	id := a.m.Active().Tab().ID()
	if err := a.command(a.m.Active(), "workbench.action.closeFolder", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the No Folder tab", func() bool { return a.m.Active().Folder == "" })
	a.settle()
	if got := a.tabNames(); !slices.Equal(got, []string{"a", "", "c"}) {
		t.Errorf("tabs %q, want the active tab turned into a No Folder tab", got)
	}
	if a.m.Active().Tab().ID() != id || !a.m.Active().emptyWorkspace() {
		t.Error("the tab changed its identity or is not an untouched No Folder tab")
	}
	// the folder can be opened again, and the empty tab takes it
	a.launch(ipc.OpenArgs{Folder: dirs["b"]})
	if got := a.tabNames(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("tabs %q", got)
	}
}

func TestAnEmptyLaunchOpensNothing(t *testing.T) {
	a, _ := socketTabs(t, nil)
	a.m.ActivateIndex(1)
	a.launch(ipc.OpenArgs{})
	if got := a.tabNames(); !slices.Equal(got, []string{"a", "b", "c"}) || a.activeName() != "b" {
		t.Errorf("tabs %q active %q: emetos with no argument only shows the window", got, a.activeName())
	}
}

func TestSocketLaunchWhileAQuestionIsShownWaitsForIt(t *testing.T) {
	// two launches that replace the same tab are answered one after the other, each after its question
	a, dirs := socketTabs(t, nil)
	b := a.workspaceOf(dirs["b"])
	a.m.ActivateIndex(1)
	a.edit(b, filepath.Join(dirs["b"], "main.go"))
	first := a.send(ipc.OpenArgs{Folder: dirs["d"], ReuseWindow: true})
	a.waitFor("the question", func() bool { return a.dialog(b) != "" })
	a.answer(b, "Cancel")
	if r := a.answered(first); r.Type != ipc.TypeOK {
		t.Errorf("reply %+v", r)
	}
	// the files of the launch whose folder was refused open in the tab that asked
	second := a.send(ipc.OpenArgs{Folder: dirs["d"], ReuseWindow: true, Files: []ipc.FileTarget{{Path: filepath.Join(dirs["b"], "util.go")}}})
	a.waitFor("the question", func() bool { return a.dialog(b) != "" })
	a.answer(b, "Cancel")
	if r := a.answered(second); r.Type != ipc.TypeOK {
		t.Errorf("reply %+v", r)
	}
	a.waitFor("the file", func() bool { return slices.Contains(docNames(b), "util.go") })
	if a.m.workspaceForFolder(dirs["d"]) != nil || a.m.Count() != 3 {
		t.Errorf("tabs %q", a.tabNames())
	}
}
