package gitscm

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// Clone, initialize and open a repository (step 24.16, SPEC §10.30 "Clone, init, open"): real git, local bare repositories and the local test
// server. The user is played by answering the boxes, the pick, the folder chooser and the dialogs the extension shows. Nothing leaves the machine.

// cloneWorld is a bare remote with two commits, an empty folder for the window tab and a folder the clones go into.
type cloneWorld struct {
	env     *gittest.Env
	src     *gittest.Repo
	remote  *gittest.Repo
	project string
	dest    string
	r       *rig
}

func newCloneWorld(t *testing.T, settings map[string]any) *cloneWorld {
	t.Helper()
	e := gittest.NewEnv(t)
	src := e.Init("src")
	src.Write("a.txt", "one\n")
	src.Commit("one")
	remote := src.AddRemote("origin")
	src.Git("push", "-q", "origin", "main")
	src.Write("b.txt", "two\n")
	src.Commit("two")
	src.Git("push", "-q", "origin", "main")
	w := &cloneWorld{env: e, src: src, remote: remote, project: filepath.Join(e.Root, "project"), dest: filepath.Join(e.Root, "clones")}
	if err := os.MkdirAll(w.project, 0o755); err != nil {
		t.Fatal(err)
	}
	w.r = newRig(t, e, rigOptions{folder: w.project, settings: settings})
	w.r.settle()
	return w
}

// nextDialog waits for the n-th dialog the extension asks.
func (r *rig) nextDialog(n int) *fakeapi.FakeDialog {
	r.t.Helper()
	var d *fakeapi.FakeDialog
	r.api.WaitFor("a dialog", func() bool {
		r.api.Pump()
		if list := r.api.OpenDialogs(); len(list) > n {
			d = list[n]
			return true
		}
		return false
	})
	return d
}

// progress waits for the n-th notification with a progress bar.
func (r *rig) progress(n int) *fakeapi.FakeProgress {
	r.t.Helper()
	var p *fakeapi.FakeProgress
	r.api.WaitFor("a progress notification", func() bool {
		r.api.Pump()
		if list := r.api.Progresses(); len(list) > n {
			p = list[n]
			return true
		}
		return false
	})
	return p
}

// clone runs git.clone for the address, the folder chooser answers dest, and it waits for the progress to end.
func (w *cloneWorld) clone(cmd string, args any) *fakeapi.FakeProgress {
	w.r.t.Helper()
	before := len(w.r.api.Progresses())
	w.r.api.AnswerFileChooser(w.dest, nil)
	if err := w.r.api.Run(cmd, args); err != nil {
		w.r.t.Fatal(err)
	}
	p := w.r.progress(before)
	w.r.api.WaitFor("the clone to end", func() bool { w.r.api.Pump(); return p.Ended() })
	return p
}

func TestCloneAsksForTheURLAndTheFolderAndShowsTheProgress(t *testing.T) {
	w := newCloneWorld(t, nil)
	r := w.r
	if err := r.api.Run("git.clone", nil); err != nil {
		t.Fatal(err)
	}
	in := r.nextInput(0)
	if o := in.Options(); o.Prompt != "Provide repository URL" || !o.IgnoreFocusOut || o.Validate == nil {
		t.Errorf("the box is %+v", o)
	}
	url := "file://" + w.remote.Dir
	r.api.AnswerFileChooser(w.dest, nil)
	in.Accept("git clone " + url) // a pasted command line works
	p := r.progress(0)
	r.api.WaitFor("the clone to end", func() bool { r.api.Pump(); return p.Ended() })
	if p.Title() != `Cloning git repository "`+url+`"...` || !p.Cancellable() {
		t.Errorf("the progress is %q, cancellable %v", p.Title(), p.Cancellable())
	}
	if len(p.Reports()) == 0 {
		t.Error("the progress of the transfer was not reported")
	}
	for _, rep := range p.Reports() {
		if rep.Percent < 0 || rep.Percent > 100 || rep.Message == "" {
			t.Errorf("a report is %+v", rep)
		}
	}
	calls := r.api.FileChooserCalls()
	if len(calls) != 1 {
		t.Fatalf("the chooser was asked %d times", len(calls))
	}
	if o := calls[0].Options; !o.Directory || o.Save || o.AcceptLabel != "Select as Repository Destination" || o.Title != "Choose a folder to clone "+url+" into" || o.Folder != w.env.Home {
		t.Errorf("the chooser was asked %+v", o)
	}
	clone := filepath.Join(w.dest, "origin")
	if got := w.env.Git(clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("HEAD is %q", got)
	}
	// the user made it: it is trusted at once, and the question that follows is about opening it
	if !r.x.trust.IsTrusted(clone) {
		t.Error("the clone is not trusted")
	}
	d := r.nextDialog(0)
	if got := d.Spec(); got.Message != "Would you like to open the repository?" || strings.Join(dialogLabels(got), ",") != "Open,Open in New Window,Cancel" {
		t.Errorf("the dialog is %+v", got)
	}
	d.Press("Open")
	r.api.Pump()
	if got := r.api.OpenedFolders(); len(got) != 1 || got[0] != (fakeapi.OpenedFolder{Path: clone, NewTab: false}) {
		t.Errorf("opened folders %v", got)
	}
	if len(r.api.Notes()) != 0 {
		t.Errorf("notifications %v", r.noteTexts())
	}
}

func TestTheFolderChooserStartsInTheDefaultCloneDirectory(t *testing.T) {
	tests := []struct {
		name    string
		setting any
		want    func(w *cloneWorld) string
	}{
		{"none is the home folder", nil, func(w *cloneWorld) string { return w.env.Home }},
		{"a folder", "/srv/code", func(*cloneWorld) string { return "/srv/code" }},
		{"a tilde is the home folder", "~/code", func(w *cloneWorld) string { return filepath.Join(w.env.Home, "code") }},
		{"only a tilde", "~", func(w *cloneWorld) string { return w.env.Home }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newCloneWorld(t, map[string]any{"git.defaultCloneDirectory": tc.setting})
			w.r.api.AnswerFileChooser("", nil) // the dialog is closed
			if err := w.r.api.Run("git.clone", w.remote.Dir); err != nil {
				t.Fatal(err)
			}
			w.r.api.WaitFor("the chooser", func() bool { w.r.api.Pump(); return len(w.r.api.FileChooserCalls()) == 1 })
			if got := w.r.api.FileChooserCalls()[0].Options.Folder; got != tc.want(w) {
				t.Errorf("the chooser starts in %q, want %q", got, tc.want(w))
			}
			w.r.api.Pump()
			if len(w.r.api.Progresses()) != 0 {
				t.Error("a clone started although the chooser was closed")
			}
		})
	}
}

func TestOpenAfterCloneForEveryValue(t *testing.T) {
	tests := []struct {
		name     string
		setting  string
		noFolder bool
		args     func(w *cloneWorld) any
		press    string // the button of the dialog, "" when none is asked
		want     []bool // NewTab of the folders opened
	}{
		{"always", "always", false, nil, "", []bool{false}},
		{"always in a new window", "alwaysNewWindow", false, nil, "", []bool{true}},
		{"when no folder is open and one is", "whenNoFolderOpen", false, nil, "Cancel", nil},
		{"when no folder is open and none is", "whenNoFolderOpen", true, nil, "", []bool{false}},
		{"prompt and Open", "prompt", false, nil, "Open", []bool{false}},
		{"prompt and Open in New Window", "prompt", false, nil, "Open in New Window", []bool{true}},
		{"prompt and Cancel", "prompt", false, nil, "Cancel", nil},
		{"the post clone action none asks nothing", "prompt", false, func(w *cloneWorld) any {
			return map[string]any{"url": w.remote.Dir, "postCloneAction": "none"}
		}, "", nil},
		{"the post clone action none beats always", "always", false, func(w *cloneWorld) any {
			return map[string]any{"url": w.remote.Dir, "postCloneAction": "none"}
		}, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newCloneWorld(t, map[string]any{"git.openAfterClone": tc.setting})
			if tc.noFolder {
				w.r.x.folder = ""
			}
			var args any = w.remote.Dir
			if tc.args != nil {
				args = tc.args(w)
			}
			w.clone("git.clone", args)
			if tc.press != "" {
				w.r.nextDialog(0).Press(tc.press)
			}
			w.r.api.Pump()
			var got []bool
			for _, f := range w.r.api.OpenedFolders() {
				if f.Path != filepath.Join(w.dest, "origin") {
					t.Errorf("opened %q", f.Path)
				}
				got = append(got, f.NewTab)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("opened %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("opened %v, want %v", got, tc.want)
				}
			}
			if tc.press == "" && len(w.r.api.OpenDialogs()) != 0 {
				t.Errorf("a dialog was asked: %+v", w.r.api.OpenDialogs()[0].Spec())
			}
		})
	}
}

func TestCloneRecursiveClonesTheSubmodulesAndTrustsThem(t *testing.T) {
	w := newCloneWorld(t, map[string]any{"git.openAfterClone": "always"})
	sub := w.env.Init("lib")
	sub.Write("lib.go", "package lib\n")
	sub.Commit("lib")
	app := w.env.Init("app")
	app.Write("main.go", "package main\n")
	app.Commit("app")
	app.Submodule("vendor/lib", sub)
	for _, tc := range []struct {
		cmd       string
		recursive bool
	}{{"git.clone", false}, {"git.cloneRecursive", true}} {
		t.Run(tc.cmd, func(t *testing.T) {
			w.dest = filepath.Join(w.env.Root, "clones-"+tc.cmd)
			w.clone(tc.cmd, app.Dir)
			path := filepath.Join(w.dest, "app")
			_, err := os.Stat(filepath.Join(path, "vendor", "lib", "lib.go"))
			if (err == nil) != tc.recursive {
				t.Errorf("the submodule is checked out: %v, want %v", err == nil, tc.recursive)
			}
			if tc.recursive {
				loc, ok := git.FindRepository(filepath.Join(path, "vendor", "lib"))
				if !ok || loc.Kind != git.KindSubmodule || !w.r.x.trustedBy(loc) {
					t.Errorf("the submodule %+v (found %v) is not trusted with its clone", loc, ok)
				}
			}
		})
	}
}

func TestTheCloneAddressIsValidatedBeforeItIsAccepted(t *testing.T) {
	w := newCloneWorld(t, nil)
	if err := w.r.api.Run("git.clone", nil); err != nil {
		t.Fatal(err)
	}
	in := w.r.nextInput(0)
	tests := []struct {
		url string
		ok  bool
	}{
		{"https://example.com/org/project.git", true},
		{"http://example.com/project", true},
		{"ssh://git@example.com:2222/srv/project.git", true},
		{"git@example.com:org/project.git", true},
		{"git://example.com/project.git", true},
		{"file:///srv/git/project.git", true},
		{"/srv/git/project.git", true},
		{"../project.git", true},
		{"~/code/project", true},
		{"/srv/git/my project.git", true},
		{"git clone https://example.com/project.git", true},
		{"   https://example.com/project.git   ", true},
		{"", false},
		{"   ", false},
		{"git clone ", false},
		{"https://example.com/my project.git", false},
		{"https://", false},
		{"ftp://example.com/project.git", false},
		{"--upload-pack=touch /tmp/x", false},
		{"https://example.com/a\nb", false},
		{"file://relative/path", false},
	}
	for _, tc := range tests {
		if msg := in.Validate(tc.url); (msg == "") != tc.ok {
			t.Errorf("Validate(%q) = %q, want ok %v", tc.url, msg, tc.ok)
		}
	}
}

func TestCloneArgumentsAreReadFromAStringAMapAndAList(t *testing.T) {
	tests := []struct {
		name string
		args any
		want cloneRequest
	}{
		{"nothing", nil, cloneRequest{}},
		{"the address", " https://example.com/x.git ", cloneRequest{url: "https://example.com/x.git"}},
		{"a pasted command", "git   clone https://example.com/x.git", cloneRequest{url: "https://example.com/x.git"}},
		{"a map", map[string]any{"url": "https://example.com/x.git", "parentPath": "/srv", "ref": "dev", "postCloneAction": "none"},
			cloneRequest{url: "https://example.com/x.git", parent: "/srv", ref: "dev", none: true}},
		{"a list like the arguments of VS Code's command", []any{"https://example.com/x.git", "/srv", map[string]any{"ref": "dev"}},
			cloneRequest{url: "https://example.com/x.git", parent: "/srv", ref: "dev"}},
		{"another post clone action is ignored", map[string]any{"url": "u", "postCloneAction": "other"}, cloneRequest{url: "u"}},
		{"numbers are ignored", 3.0, cloneRequest{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cloneArgs(tc.args); got != tc.want {
				t.Errorf("cloneArgs(%v) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestACloneIntoATakenNameGetsTheNextFreeOneAndNoNameIsEverReused(t *testing.T) {
	w := newCloneWorld(t, map[string]any{"git.openAfterClone": "always"})
	if err := os.MkdirAll(filepath.Join(w.dest, "origin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.dest, "origin", "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.clone("git.clone", w.remote.Dir)
	if data, err := os.ReadFile(filepath.Join(w.dest, "origin", "keep.txt")); err != nil || string(data) != "keep\n" {
		t.Errorf("the folder that was there was touched: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(w.dest, "origin-1", "b.txt")); err != nil {
		t.Errorf("the clone is not in origin-1: %v", err)
	}
	w.r.api.Pump()
	if got := w.r.api.OpenedFolders(); len(got) != 1 || got[0].Path != filepath.Join(w.dest, "origin-1") {
		t.Errorf("opened folders %v", got)
	}
}

func TestACloneThatCannotFindAFreeNameIsRefused(t *testing.T) {
	w := newCloneWorld(t, nil)
	if err := os.MkdirAll(filepath.Join(w.dest, "origin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 20; i++ {
		if err := os.MkdirAll(filepath.Join(w.dest, "origin-"+strconv.Itoa(i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.r.api.AnswerFileChooser(w.dest, nil)
	if err := w.r.api.Run("git.clone", w.remote.Dir); err != nil {
		t.Fatal(err)
	}
	p := w.r.progress(0)
	note := w.r.nextNote(0)
	if note.Severity != "error" || !strings.HasPrefix(note.Message, "Git: The folder ") || !strings.HasSuffix(note.Message, "already exists and is not an empty directory.") || strings.Join(note.Actions, ",") != "Show Git Output" {
		t.Errorf("notification %+v", note)
	}
	w.r.api.WaitFor("the progress to end", func() bool { w.r.api.Pump(); return p.Ended() })
	if len(w.r.api.OpenDialogs()) != 0 || len(w.r.api.OpenedFolders()) != 0 {
		t.Error("the refused clone went on")
	}
}

func TestACloneThatFailsSaysWhyAndLeavesNothingBehind(t *testing.T) {
	tests := []struct {
		name string
		url  func(w *cloneWorld) string
		want string
	}{
		{"a repository that is not there", func(w *cloneWorld) string { return filepath.Join(w.env.Root, "no-such.git") }, "Git: "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newCloneWorld(t, nil)
			w.clone("git.clone", tc.url(w))
			note := w.r.nextNote(0)
			if note.Severity != "error" || !strings.HasPrefix(note.Message, tc.want) || strings.Join(note.Actions, ",") != "Show Git Output" {
				t.Errorf("notification %+v", note)
			}
			if entries, _ := os.ReadDir(w.dest); len(entries) != 0 {
				t.Errorf("the failed clone left %v", entries)
			}
			if len(w.r.api.OpenDialogs()) != 0 {
				t.Error("the failed clone asked to open it")
			}
		})
	}
}

func TestAnAddressGitCannotCloneIsRefusedWithoutAnyProcess(t *testing.T) {
	for _, url := range []string{"ftp://example.com/x.git", "--upload-pack=x", "https://"} {
		t.Run(url, func(t *testing.T) {
			w := newCloneWorld(t, nil)
			if err := w.r.api.Run("git.clone", url); err != nil {
				t.Fatal(err)
			}
			w.r.api.Pump()
			notes := w.r.api.Notes()
			if len(notes) != 1 || notes[0].Severity != "error" || len(w.r.api.Progresses()) != 0 || len(w.r.api.FileChooserCalls()) != 0 {
				t.Errorf("notes %v, progresses %d, chooser calls %d", notes, len(w.r.api.Progresses()), len(w.r.api.FileChooserCalls()))
			}
		})
	}
}

// slowClone is a git whose clone makes the folder with a file in it and then waits; "--version" answers as git does. It records the pid of its
// child.
func slowClone(t *testing.T) (program, pidFile string) {
	t.Helper()
	dir := t.TempDir()
	program, pidFile = filepath.Join(dir, "git"), filepath.Join(dir, "pid")
	script := `#!/bin/sh
PATH=/usr/bin:/bin
if [ "$1" = "--version" ]; then echo "git version 2.43.0"; exit 0; fi
for a in "$@"; do last=$a; done
mkdir -p "$last/.git"
echo partial > "$last/partial.txt"
echo "Receiving objects:  10% (1/10)" >&2
sleep 30 &
echo $! > "` + pidFile + `"
wait
`
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, pidFile
}

func TestCancellingTheProgressStopsGitAndRemovesTheFolder(t *testing.T) {
	e := gittest.NewEnv(t)
	program, pidFile := slowClone(t)
	project := filepath.Join(e.Root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, e, rigOptions{folder: project, gitPath: program})
	r.settle()
	dest := filepath.Join(e.Root, "clones")
	r.api.AnswerFileChooser(dest, nil)
	if err := r.api.Run("git.clone", "https://example.com/org/repo.git"); err != nil {
		t.Fatal(err)
	}
	p := r.progress(0)
	r.api.WaitFor("the clone to begin", func() bool {
		r.api.Pump()
		_, err := os.Stat(pidFile)
		return err == nil && len(p.Reports()) > 0
	})
	if _, err := os.Stat(filepath.Join(dest, "repo", "partial.txt")); err != nil {
		t.Fatalf("the fake clone made no folder: %v", err)
	}
	p.Cancel()
	r.api.WaitFor("the folder to go", func() bool {
		r.api.Pump()
		_, err := os.Stat(filepath.Join(dest, "repo"))
		return err != nil
	})
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid := 0
	for _, c := range strings.TrimSpace(string(data)) {
		pid = pid*10 + int(c-'0')
	}
	gone := false
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		r.api.Pump()
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			gone = true
			break
		}
	}
	if !gone {
		t.Errorf("process %d of the clone is still running", pid)
	}
	r.api.Pump()
	if len(r.api.Notes()) != 0 || len(r.api.OpenDialogs()) != 0 || !p.Ended() {
		t.Errorf("a cancelled clone said something: notes %v, dialogs %d", r.noteTexts(), len(r.api.OpenDialogs()))
	}
}

func TestACloneOverHTTPAsksForThePasswordThroughTheHelper(t *testing.T) {
	logs := recordLogs(t)
	w := newHTTPWorld(t)
	project := filepath.Join(w.env.Root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, w.env, rigOptions{folder: project, settings: map[string]any{"git.openAfterClone": "always"}})
	r.settle()
	dest := filepath.Join(w.env.Root, "clones")

	r.api.AnswerFileChooser(dest, nil)
	if err := r.api.Run("git.clone", w.withUser); err != nil {
		t.Fatal(err)
	}
	r.nextInput(0).Accept("not-the-password")
	note := r.nextNote(0)
	if note.Severity != "error" || !strings.HasPrefix(note.Message, "Failed to authenticate to git remote") || strings.Join(note.Actions, ",") != "Retry,Show Git Output" {
		t.Fatalf("notification %+v", note)
	}
	if _, err := os.Stat(filepath.Join(dest, "origin")); err == nil {
		t.Error("a clone with the wrong password left its folder")
	}
	// Retry asks again, and the right password clones
	r.api.RunNoteAction(0, "Retry")
	r.nextInput(1).Accept(typed)
	r.api.WaitFor("the clone", func() bool {
		r.api.Pump()
		_, err := os.Stat(filepath.Join(dest, "origin", "a.txt"))
		return err == nil && len(r.api.OpenedFolders()) == 1
	})
	noSecretAnywhere(t, r, w, logs, typed)
	noSecretAnywhere(t, r, w, logs, "not-the-password")
}

func TestACancelledPasswordPromptOfACloneFails(t *testing.T) {
	w := newHTTPWorld(t)
	project := filepath.Join(w.env.Root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, w.env, rigOptions{folder: project})
	r.settle()
	dest := filepath.Join(w.env.Root, "clones")
	r.api.AnswerFileChooser(dest, nil)
	if err := r.api.Run("git.clone", w.withUser); err != nil {
		t.Fatal(err)
	}
	r.nextInput(0).Cancel()
	note := r.nextNote(0)
	if !strings.HasPrefix(note.Message, "Failed to authenticate to git remote") {
		t.Errorf("notification %+v", note)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Errorf("the clone left %v", entries)
	}
}

func TestTheSecretsOfACloneAddressAreMaskedEverywhere(t *testing.T) {
	logs := recordLogs(t)
	w := newHTTPWorld(t)
	project := filepath.Join(w.env.Root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, w.env, rigOptions{folder: project, settings: map[string]any{"git.openAfterClone": "prompt"}})
	r.settle()
	const secret = "tok3n-in-the-address"
	url := strings.Replace(w.srv.URL, "http://", "http://ada:"+secret+"@", 1) + "/origin.git"
	dest := filepath.Join(w.env.Root, "clones")
	r.api.AnswerFileChooser(dest, nil)
	if err := r.api.Run("git.clone", url); err != nil {
		t.Fatal(err)
	}
	// the wrong password in the address: git asks nothing, it fails, and nothing shows the address as typed
	note := r.nextNote(0)
	if !strings.HasPrefix(note.Message, "Failed to authenticate to git remote") {
		t.Fatalf("notification %+v", note)
	}
	for _, p := range r.api.Progresses() {
		if strings.Contains(p.Title(), secret) || !strings.Contains(p.Title(), "***@") {
			t.Errorf("the progress title is %q", p.Title())
		}
	}
	for _, c := range r.api.FileChooserCalls() {
		if strings.Contains(c.Options.Title, secret) || !strings.Contains(c.Options.Title, "***@") {
			t.Errorf("the chooser title is %q", c.Options.Title)
		}
	}
	noSecretAnywhere(t, r, w, logs, secret)
	// the key of the repository cache does not hold it either
	r.x.cache.set(url, filepath.Join(project, "x"))
	data, _ := os.ReadFile(filepath.Join(w.env.StateHome, "emetos", repoCacheFile))
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), "***@") {
		t.Errorf("the repository cache file is %s", data)
	}
}

func TestSafeNotificationDropsTextWithLinks(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", `Cloning git repository "https://example.com/normal.git"...`, `Cloning git repository "https://example.com/normal.git"...`},
		{"a Markdown link", `x-[Open](command:test.noop).git`, "fallback"},
		{"a command in capitals", `x-COMMAND:test.noop.git`, "fallback"},
		{"a link to the web", `x-[Help](https://example.com).git`, "fallback"},
		{"an escaped link", `x-\[Open\](command:test.noop)`, "fallback"},
		{"a bracket that is no link", `x-[1].git`, `x-[1].git`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeNotification(tc.in, "fallback"); got != tc.want {
				t.Errorf("safeNotification(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestACloneAddressWithALinkInItIsNotShownAsALink(t *testing.T) {
	w := newCloneWorld(t, nil)
	// a local repository whose folder is named like a Markdown link to a command
	url := filepath.Join(w.env.Root, "repo-[Open](command:test.noop).git")
	if err := os.Rename(w.remote.Dir, url); err != nil {
		t.Fatal(err)
	}
	p := w.clone("git.clone", url)
	if got := w.r.api.FileChooserCalls()[0].Options.Title; got != "Choose a folder to clone into" {
		t.Errorf("the chooser title is %q", got)
	}
	if p.Title() != "Cloning git repository..." {
		t.Errorf("the progress title is %q", p.Title())
	}
	if _, err := os.Stat(filepath.Join(w.dest, "repo-[Open](command:test.noop)", "b.txt")); err != nil {
		t.Errorf("the clone: %v", err)
	}
}

// ---- the repository cache ----

func TestCloningAnAddressThatHasAnOpenCloneOpensThatOne(t *testing.T) {
	w := newCloneWorld(t, map[string]any{"git.openAfterClone": "always"})
	r := w.r
	url := "https://example.com/org/app.git"
	other1 := filepath.Join(w.env.Root, "work1")
	other2 := filepath.Join(w.env.Root, "work2")
	gone := filepath.Join(w.env.Root, "work-gone")
	for _, d := range []string{other1, other2} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := func(folder string) {
		r.x.cache.setFolders(func() []string { return []string{folder} })
		r.x.cache.set(url, filepath.Join(folder, "app"))
	}
	reset := func() {
		for _, folder := range []string{r.x.folder, other1, other2, gone} {
			r.x.cache.delete(url, folder)
		}
		r.x.cache.setFolders(func() []string { return []string{r.x.folder} })
		r.api.AnswerFileChooser("", nil)
	}

	t.Run("the clone of the folder of the tab is the answer", func(t *testing.T) {
		reset()
		file(r.x.folder)
		if err := r.api.Run("git.clone", url); err != nil {
			t.Fatal(err)
		}
		r.api.Pump()
		if notes := r.api.Notes(); len(notes) != 1 || !strings.HasPrefix(notes[0].Message, "The repository is open already") {
			t.Errorf("notifications %v", r.noteTexts())
		}
		if len(r.api.FileChooserCalls()) != 0 || len(r.api.OpenedFolders()) != 0 {
			t.Error("the clone of the folder of the tab was cloned or opened again")
		}
	})
	t.Run("a single clone elsewhere is opened by the rule of openAfterClone", func(t *testing.T) {
		reset()
		file(other1)
		before := len(r.api.OpenedFolders())
		if err := r.api.Run("git.clone", url); err != nil {
			t.Fatal(err)
		}
		r.api.Pump()
		got := r.api.OpenedFolders()
		if len(got) != before+1 || got[before].Path != other1 {
			t.Errorf("opened folders %v", got)
		}
		if len(r.api.FileChooserCalls()) != 0 {
			t.Error("it cloned again")
		}
	})
	t.Run("several are a pick that ends with Clone again", func(t *testing.T) {
		reset()
		file(other1)
		file(other2)
		picks := len(r.api.Picks())
		if err := r.api.Run("git.clone", url); err != nil {
			t.Fatal(err)
		}
		p := r.nextPick(picks)
		if got := p.Labels(); strings.Join(got, ",") != "work1,work2,Clone again" || p.Options().Placeholder != "Open Existing Repository Clone" {
			t.Errorf("the pick is %v, %q", got, p.Options().Placeholder)
		}
		before := len(r.api.OpenedFolders())
		p.Choose(1)
		r.api.Pump()
		if got := r.api.OpenedFolders(); len(got) != before+1 || got[before].Path != other2 {
			t.Errorf("opened folders %v", got)
		}
		// Clone again goes on to the chooser
		file(other1)
		file(other2)
		picks = len(r.api.Picks())
		if err := r.api.Run("git.clone", url); err != nil {
			t.Fatal(err)
		}
		r.nextPick(picks).Choose(2)
		r.api.WaitFor("the chooser", func() bool { r.api.Pump(); return len(r.api.FileChooserCalls()) > 0 })
	})
	t.Run("a clone whose folder is gone is cloned again", func(t *testing.T) {
		reset()
		file(gone) // the folder was never made
		if err := r.api.Run("git.clone", url); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the chooser", func() bool { r.api.Pump(); return len(r.api.FileChooserCalls()) > 0 })
	})
}

// ---- Initialize Repository ----

func TestInitInTheFolderOfTheTabMakesARepositoryAndOpensIt(t *testing.T) {
	tests := []struct {
		name    string
		setting map[string]any
		args    any
		branch  string
	}{
		{"the default branch main", nil, true, "main"},
		{"the branch of the setting", map[string]any{"git.defaultBranchName": "trunk"}, true, "trunk"},
		{"a name with white space is made safe", map[string]any{"git.defaultBranchName": "my branch", "git.branchWhitespaceChar": "_"}, []any{true}, "my_branch"},
		{"no name leaves git's own default (init.defaultBranch is main in this world)", map[string]any{"git.defaultBranchName": ""}, true, "main"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newCloneWorld(t, tc.setting)
			r := w.r
			if len(r.x.model.order) != 0 {
				t.Fatal("the folder has a repository already")
			}
			if err := r.api.Run("git.init", tc.args); err != nil {
				t.Fatal(err)
			}
			r.api.WaitFor("the repository", func() bool {
				r.api.Pump()
				rep := r.x.model.byRoot(w.project)
				return rep != nil && rep.repo != nil && !rep.refreshing
			})
			if got := w.env.Git(w.project, "symbolic-ref", "--short", "HEAD"); got != tc.branch {
				t.Errorf("HEAD is %q, want %q", got, tc.branch)
			}
			if len(r.api.Picks()) != 0 || len(r.api.OpenDialogs()) != 0 || len(r.api.FileChooserCalls()) != 0 || len(r.api.Notes()) != 0 {
				t.Errorf("the command asked something: picks %d, dialogs %d, chooser %d, notes %v", len(r.api.Picks()), len(r.api.OpenDialogs()), len(r.api.FileChooserCalls()), r.noteTexts())
			}
			if !r.x.trust.IsTrusted(w.project) {
				t.Error("the repository is not trusted")
			}
		})
	}
}

func TestInitWithTheDefaultBranchNameLeftToGit(t *testing.T) {
	// the same command in a world without init.defaultBranch gives git's own default
	e := gittest.NewEnv(t)
	project := filepath.Join(e.Root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, e, rigOptions{folder: project, settings: map[string]any{"git.defaultBranchName": ""}, extraEnv: []string{"GIT_CONFIG_PARAMETERS="}})
	r.settle()
	if err := r.api.Run("git.init", true); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the repository", func() bool {
		r.api.Pump()
		rep := r.x.model.byRoot(project)
		return rep != nil && rep.repo != nil
	})
	head, err := os.ReadFile(filepath.Join(project, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(head)); got != "ref: refs/heads/master" {
		t.Errorf("HEAD is %q", got)
	}
}

func TestInitAsksWhichFolderWhenNotToldToSkipTheQuestion(t *testing.T) {
	w := newCloneWorld(t, nil)
	r := w.r
	other := filepath.Join(w.env.Root, "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run("git.init", nil); err != nil {
		t.Fatal(err)
	}
	p := r.nextPick(0)
	if got := p.Labels(); strings.Join(got, ",") != "project,Choose Folder..." || p.Options().Placeholder != "Pick workspace folder to initialize git repo in" {
		t.Errorf("the pick is %v, %q", got, p.Options().Placeholder)
	}
	// Choose Folder asks the chooser; the folder is not the tab's, so the repository is made and the question is about opening it
	r.api.AnswerFileChooser(other, nil)
	p.Choose(1)
	d := r.nextDialog(0)
	if got := d.Spec(); got.Message != "Would you like to open the initialized repository?" || strings.Join(dialogLabels(got), ",") != "Open,Open in New Window,Cancel" {
		t.Errorf("the dialog is %+v", got)
	}
	if o := r.api.FileChooserCalls()[0].Options; !o.Directory || o.AcceptLabel != "Initialize Repository" || o.Folder != w.project {
		t.Errorf("the chooser was asked %+v", o)
	}
	if _, err := os.Stat(filepath.Join(other, ".git")); err != nil {
		t.Errorf("no repository in the chosen folder: %v", err)
	}
	if !r.x.trust.IsTrusted(other) {
		t.Error("the new repository is not trusted")
	}
	d.Press("Open in New Window")
	r.api.Pump()
	if got := r.api.OpenedFolders(); len(got) != 1 || got[0] != (fakeapi.OpenedFolder{Path: other, NewTab: true}) {
		t.Errorf("opened folders %v", got)
	}
}

func TestInitOfAnotherFolderOpensItWhenAskedTo(t *testing.T) {
	tests := []struct {
		name   string
		press  string
		opened []fakeapi.OpenedFolder
		inView bool
	}{
		{"Open", "Open", []fakeapi.OpenedFolder{{NewTab: false}}, false},
		{"Cancel opens the repository in the view, as dismissing VS Code's message does", "Cancel", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newCloneWorld(t, nil)
			r := w.r
			other := filepath.Join(w.env.Root, "elsewhere")
			if err := os.MkdirAll(other, 0o755); err != nil {
				t.Fatal(err)
			}
			r.x.folder = "" // a tab with no folder goes straight to the chooser
			r.api.AnswerFileChooser(other, nil)
			if err := r.api.Run("git.init", nil); err != nil {
				t.Fatal(err)
			}
			r.nextDialog(0).Press(tc.press)
			r.api.WaitFor("the answer", func() bool {
				r.api.Pump()
				return r.x.model.byRoot(other) != nil == tc.inView
			})
			if len(r.api.Picks()) != 0 {
				t.Error("a tab with no folder was asked which folder")
			}
			for i := range tc.opened {
				tc.opened[i].Path = other
			}
			got := r.api.OpenedFolders()
			if len(got) != len(tc.opened) || (len(got) == 1 && got[0] != tc.opened[0]) {
				t.Errorf("opened folders %v, want %v", got, tc.opened)
			}
		})
	}
}

func TestInitOfTheHomeFolderOrAboveItAsksFirst(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pick  func(w *cloneWorld) string
		asked bool
	}{
		{"the home folder", func(w *cloneWorld) string { return w.env.Home }, true},
		{"a folder above the home folder", func(w *cloneWorld) string { return filepath.Dir(w.env.Home) }, true},
		{"a folder below the home folder", func(w *cloneWorld) string { return filepath.Join(w.env.Home, "code") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newCloneWorld(t, nil)
			r := w.r
			folder := tc.pick(w)
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			r.x.folder = ""
			r.api.AnswerFileChooser(folder, nil)
			if err := r.api.Run("git.init", nil); err != nil {
				t.Fatal(err)
			}
			if !tc.asked {
				r.nextDialog(0).Press("Cancel") // the question about opening it
				if _, err := os.Stat(filepath.Join(folder, ".git")); err != nil {
					t.Errorf("no repository: %v", err)
				}
				return
			}
			d := r.nextDialog(0)
			if got := d.Spec(); got.Type != extension.DialogWarning || got.Message != "This will create a Git repository in the selected folder. Are you sure you want to continue?" || strings.Join(dialogLabels(got), ",") != "Initialize Repository,Cancel" {
				t.Errorf("the dialog is %+v", got)
			}
			d.Cancel()
			r.api.Pump()
			time.Sleep(50 * time.Millisecond)
			r.api.Pump()
			if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
				t.Error("a repository was made although the user said no")
			}
		})
	}
}

func TestInitThatFailsSaysWhy(t *testing.T) {
	w := newCloneWorld(t, nil)
	r := w.r
	file := filepath.Join(w.env.Root, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.x.folder = ""
	r.api.AnswerFileChooser(filepath.Join(file, "inside"), nil)
	if err := r.api.Run("git.init", nil); err != nil {
		t.Fatal(err)
	}
	note := r.nextNote(0)
	if note.Severity != "error" || !strings.HasPrefix(note.Message, "Git: ") || strings.Join(note.Actions, ",") != "Show Git Output" {
		t.Errorf("notification %+v", note)
	}
	if len(r.api.OpenDialogs()) != 0 {
		t.Error("a failed init asked to open the repository")
	}
}

// ---- Open Repository ----

func TestOpenRepositoryPicksAFolderAndOpensItsRepositoryInTheView(t *testing.T) {
	w := newCloneWorld(t, nil)
	r := w.r
	repo := w.env.Init("elsewhere")
	repo.Write("x.txt", "x\n")
	repo.Commit("x")
	sub := filepath.Join(repo.Dir, "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	notRepo := filepath.Join(w.env.Root, "plain")
	if err := os.MkdirAll(notRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		args   any
		chosen string
		note   string
		root   string
	}{
		{"the folder chosen", nil, repo.Dir, "", repo.Dir},
		{"a folder inside the repository", nil, sub, "", repo.Dir},
		{"a path in the argument", repo.Dir, "", "", repo.Dir},
		{"a path in a map", map[string]any{"path": repo.Dir}, "", "", repo.Dir},
		{"a folder that is no repository", nil, notRepo, "There is no Git repository in " + notRepo, ""},
		{"a relative path", "relative/dir", "", "The path of a repository is absolute: relative/dir", ""},
		{"a dialog that is closed", nil, "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rep := r.x.model.byRoot(repo.Dir); rep != nil {
				r.x.model.drop(rep)
			}
			notes := len(r.api.Notes())
			chooser := len(r.api.FileChooserCalls())
			if tc.args == nil {
				r.api.AnswerFileChooser(tc.chosen, nil)
			}
			if err := r.api.Run("git.openRepository", tc.args); err != nil {
				t.Fatal(err)
			}
			if tc.args == nil {
				r.api.WaitFor("the chooser", func() bool { r.api.Pump(); return len(r.api.FileChooserCalls()) == chooser+1 })
				if o := r.api.FileChooserCalls()[chooser].Options; !o.Directory || o.AcceptLabel != "Open Repository" || o.Folder != w.env.Home {
					t.Errorf("the chooser was asked %+v", o)
				}
			}
			r.api.WaitFor("the end", func() bool { r.api.Pump(); return tc.note != "" && len(r.api.Notes()) > notes || tc.note == "" })
			r.api.Pump()
			if tc.note != "" {
				if got := r.api.Notes()[notes]; got.Message != tc.note || got.Severity != "error" {
					t.Errorf("notification %+v", got)
				}
			}
			if (r.x.model.byRoot(tc.root) != nil) != (tc.root != "") {
				t.Errorf("repository at %q open: %v", tc.root, r.x.model.byRoot(tc.root) != nil)
			}
			if len(r.api.OpenedFolders()) != 0 {
				t.Error("the folder was opened as the folder of the tab")
			}
		})
	}
}

func TestOpenRepositoryOfARepositoryThatWasNotTrustedAsksTheQuestion(t *testing.T) {
	w := newCloneWorld(t, nil)
	r := w.r
	foreign := filepath.Join(w.env.Root, "foreign")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	w.env.Git(foreign, "init", "-q", "-b", "main") // a repository the harness did not trust
	if err := r.api.Run("git.openRepository", foreign); err != nil {
		t.Fatal(err)
	}
	d := r.nextDialog(0)
	if !strings.Contains(d.Spec().Message, "Do you trust the authors of the Git repository") {
		t.Errorf("the dialog is %+v", d.Spec())
	}
	d.Press("Don't Trust")
}

func TestTheCloneCommandsFailQuietlyWhenGitIsMissing(t *testing.T) {
	e := gittest.NewEnv(t)
	project := filepath.Join(e.Root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, e, rigOptions{folder: project, noGit: true})
	r.settle()
	for _, cmd := range []string{"git.clone", "git.cloneRecursive", "git.init"} {
		before := len(r.api.Notes())
		if err := r.api.Run(cmd, nil); err != nil {
			t.Fatal(err)
		}
		r.api.Pump()
		notes := r.api.Notes()
		if len(notes) != before+1 || notes[before].Message != "Git is not available." {
			t.Errorf("%s: notifications %v", cmd, r.noteTexts())
		}
	}
	if len(r.api.Inputs()) != 0 || len(r.api.Picks()) != 0 {
		t.Error("a box or a pick was shown without git")
	}
}

func TestAClonePathThatDoesNotExistYetIsMadeByTheClone(t *testing.T) {
	w := newCloneWorld(t, map[string]any{"git.openAfterClone": "always"})
	w.dest = filepath.Join(w.env.Root, "a", "b", "c")
	w.clone("git.clone", w.remote.Dir)
	if _, err := os.Stat(filepath.Join(w.dest, "origin", "a.txt")); err != nil {
		t.Error(err)
	}
}

func TestTheCloneCommandsAreNotLoggedAsWarningsInAHostWithoutAWorkbench(t *testing.T) {
	// the errors of a scope that cannot ask are the unavailable ones, which the extension leaves out of its log
	for _, tag := range []string{"ExtensionFileChooserUnavailable", "ExtensionDialogsUnavailable", "ExtensionInputBoxUnavailable", "ExtensionQuickPickUnavailable", "ExtensionDeactivated"} {
		logs := recordLogs(t)
		e := gittest.NewEnv(t)
		r := newRig(t, e, rigOptions{})
		r.x.logUnavailable("cannot do it", errors.New(tag, "unavailable"))
		if strings.Contains(logs.String(), "cannot do it") {
			t.Errorf("%s was logged:\n%s", tag, logs.String())
		}
	}
	logs := recordLogs(t)
	e := gittest.NewEnv(t)
	r := newRig(t, e, rigOptions{})
	r.x.logUnavailable("cannot do it", errors.New("SomethingElse", "broken"))
	if !strings.Contains(logs.String(), "cannot do it") {
		t.Error("another failure was not logged")
	}
}
