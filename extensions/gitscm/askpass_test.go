package gitscm

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// Passwords and editors (step 24.9, SPEC §10.30 "Passwords and editors"): the real git, the real helper (this test binary answers the
// three flags in TestMain, as cmd/emetos does), the real server in the extension, and a test that plays the user by answering the
// boxes the extension shows. The remote is git http-backend behind Basic authentication on 127.0.0.1. Nothing reaches the user's git
// configuration, credentials, ssh-agent or runtime folder, and no sshd is started.

// typed is the password the test types; the tests look for it everywhere it must not be.
const typed = "Tr0ub4dor&3-typed-in-the-box"

// httpWorld is a remote that wants a password and a clone of it.
type httpWorld struct {
	env  *gittest.Env
	srv  *gittest.HTTPServer
	src  *gittest.Repo
	work *gittest.Repo
	// plain is the URL of the remote with no user, withUser the one with the user name only.
	plain, withUser string
}

func newHTTPWorld(t *testing.T) *httpWorld {
	t.Helper()
	e := gittest.NewEnv(t)
	src := e.Init("src")
	src.Write("a.txt", "one\n")
	src.Commit("one")
	src.AddRemote("origin")
	src.Git("push", "-q", "origin", "main")
	srv := e.HTTPBackend(e.Work, "ada", typed)
	w := &httpWorld{env: e, srv: srv, src: src}
	w.plain = srv.URL + "/origin.git"
	w.withUser = strings.Replace(srv.URL, "http://", "http://ada@", 1) + "/origin.git"
	dir := filepath.Join(e.Work, "clone")
	e.Git(e.Work, "clone", "-q", strings.Replace(srv.URL, "http://", "http://ada:"+typed+"@", 1)+"/origin.git", dir)
	e.Git(dir, "config", "core.hooksPath", e.HooksDir)
	e.Git(dir, "remote", "set-url", "origin", w.withUser)
	e.Trust(dir)
	w.work = &gittest.Repo{Env: e, Dir: dir}
	return w
}

// publish pushes a new commit of the source to the remote and returns its hash.
func (w *httpWorld) publish(name string) string {
	w.src.Write(name, name+"\n")
	head := w.src.Commit("add " + name)
	w.src.Git("push", "-q", "origin", "main")
	return head
}

// host is "http://127.0.0.1:<port>".
func (w *httpWorld) host() string { return w.srv.URL }

// recordLogs makes the log of the extension (the default logger the fake API takes) a buffer for the test.
func recordLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

// nextInput waits for the n-th input box the extension shows (counted from 0), pumping the UI thread meanwhile.
func (r *rig) nextInput(n int) *fakeapi.Input {
	r.t.Helper()
	var in *fakeapi.Input
	r.api.WaitFor("an input box", func() bool {
		r.api.Pump()
		if inputs := r.api.Inputs(); len(inputs) > n {
			in = inputs[n]
			return true
		}
		return false
	})
	return in
}

// nextPick waits for the n-th quick pick.
func (r *rig) nextPick(n int) *fakeapi.Pick {
	r.t.Helper()
	var p *fakeapi.Pick
	r.api.WaitFor("a quick pick", func() bool {
		r.api.Pump()
		if picks := r.api.Picks(); len(picks) > n {
			p = picks[n]
			return true
		}
		return false
	})
	return p
}

// nextNote waits for the n-th notification.
func (r *rig) nextNote(n int) fakeapi.Note {
	r.t.Helper()
	var note fakeapi.Note
	r.api.WaitFor("a notification", func() bool {
		r.api.Pump()
		if notes := r.api.Notes(); len(notes) > n {
			note = notes[n]
			return true
		}
		return false
	})
	return note
}

// result is the end of a command that runs in the background.
type result struct {
	res git.Result
	err error
}

// runGit runs a command of the runner of the extension on a goroutine, as a command of the user does.
func (r *rig) runGit(c git.Command) <-chan result {
	r.t.Helper()
	ch := make(chan result, 1)
	runner := r.x.runner
	if runner == nil {
		r.t.Fatal("the extension has no runner")
	}
	go func() {
		res, err := runner.Run(context.Background(), c)
		ch <- result{res, err}
	}()
	return ch
}

// wait pumps the UI thread until the command ends.
func (r *rig) wait(ch <-chan result) result {
	r.t.Helper()
	var out result
	r.api.WaitFor("the command to end", func() bool {
		r.api.Pump()
		select {
		case out = <-ch:
			return true
		default:
			return false
		}
	})
	return out
}

func newWorldRig(t *testing.T, w *httpWorld, settings map[string]any) *rig {
	t.Helper()
	r := newRig(t, w.env, rigOptions{folder: w.work.Dir, settings: settings})
	r.settle()
	return r
}

func TestFetchAsksForThePasswordThroughTheHelper(t *testing.T) {
	logs := recordLogs(t)
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, nil)
	repo := r.repo(w.work.Dir)
	head := w.publish("two.txt")

	if err := r.api.Run("git.fetch", nil); err != nil {
		t.Fatal(err)
	}
	in := r.nextInput(0)
	o := in.Options()
	want := extension.InputBoxOptions{Prompt: "Password for '" + w.host() + "'", Message: "Git: " + w.host(), Password: true, IgnoreFocusOut: true}
	want.Prompt = strings.Replace(want.Prompt, "http://", "http://ada@", 1)
	if o.Prompt != want.Prompt || o.Message != want.Message || !o.Password || !o.IgnoreFocusOut || o.Value != "" {
		t.Errorf("the box is %+v, want %+v", o, want)
	}
	in.Accept(typed)
	r.idle(repo)

	if got := w.work.Git("rev-parse", "origin/main"); got != head {
		t.Errorf("origin/main is %s after the fetch, want %s", got, head)
	}
	if n := len(r.api.Inputs()); n != 1 {
		t.Errorf("%d boxes were shown, want 1", n)
	}
	// 401 first, then the request with the password
	reqs := w.srv.Requests()
	if len(reqs) < 2 || !strings.HasSuffix(reqs[0], " 401") || !strings.HasSuffix(reqs[len(reqs)-1], " 200") {
		t.Errorf("requests %v", reqs)
	}
	if notes := r.api.Notes(); len(notes) != 0 {
		t.Errorf("notifications %+v", notes)
	}
	noSecretAnywhere(t, r, w, logs, typed)
}

// noSecretAnywhere looks for the secret in the log of Emetos, the Git channel, the notifications, the questions that were shown, every
// file of the world and the folder of the helper.
func noSecretAnywhere(t *testing.T, r *rig, w *httpWorld, logs *lockedBuffer, secret string) {
	t.Helper()
	if strings.Contains(logs.String(), secret) {
		t.Errorf("the secret is in the log of Emetos:\n%s", logs.String())
	}
	for _, line := range r.api.Channel(ChannelName).Lines() {
		if strings.Contains(line, secret) {
			t.Errorf("the secret is in the Git channel: %s", line)
		}
	}
	for _, n := range r.api.Notes() {
		if strings.Contains(n.Message, secret) {
			t.Errorf("the secret is in a notification: %s", n.Message)
		}
	}
	for _, in := range r.api.Inputs() {
		if o := in.Options(); strings.Contains(o.Prompt+o.Message+o.Value, secret) {
			t.Errorf("the secret is in a box: %+v", o)
		}
	}
	for _, req := range w.srv.Requests() {
		if strings.Contains(req, secret) {
			t.Errorf("the secret is in the log of the server: %s", req)
		}
	}
	roots := []string{w.env.Root}
	if r.x.helperSrv != nil {
		roots = append(roots, filepath.Dir(filepath.Dir(r.x.helperSrv.Folder())))
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return nil
			}
			if data, rerr := os.ReadFile(path); rerr == nil && bytes.Contains(data, []byte(secret)) {
				// the clone was made with the credentials in its URL by the harness, and git keeps that in no file but the objects of the
				// first clone's config, which was set to the plain URL at once: any other file is a leak
				t.Errorf("the secret is in the file %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestACloneAndAPushAskForTheUserNameAndThePassword(t *testing.T) {
	logs := recordLogs(t)
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, nil)
	host := w.host()

	// a clone of the plain URL asks for both
	dest := filepath.Join(w.env.Work, "second")
	ch := r.runGit(git.Command{Dir: w.env.Work, Args: []string{"clone", "-q", w.plain, dest}})
	user := r.nextInput(0)
	if o := user.Options(); o.Prompt != "Username for '"+host+"'" || o.Message != "Git: "+host || o.Password || !o.IgnoreFocusOut {
		t.Errorf("the user name box is %+v", o)
	}
	user.Accept("ada")
	pass := r.nextInput(1)
	if o := pass.Options(); o.Prompt != "Password for '"+strings.Replace(host, "http://", "http://ada@", 1)+"'" || !o.Password {
		t.Errorf("the password box is %+v", o)
	}
	pass.Accept(typed)
	if got := r.wait(ch); got.err != nil {
		t.Fatalf("clone: %v", got.err)
	}
	if got, want := w.env.Git(dest, "rev-parse", "HEAD"), w.src.Head(); got != want {
		t.Errorf("the clone is at %s, want %s", got, want)
	}

	// a push from the clone of the window asks for the password only (the user is in the URL)
	w.work.Write("pushed.txt", "pushed\n")
	head := w.work.Commit("push me")
	ch = r.runGit(git.Command{Dir: w.work.Dir, Args: []string{"push", "-q", "origin", "main"}})
	push := r.nextInput(2)
	if o := push.Options(); !strings.HasPrefix(o.Prompt, "Password for '") || !o.Password {
		t.Errorf("the push box is %+v", o)
	}
	push.Accept(typed)
	if got := r.wait(ch); got.err != nil {
		t.Fatalf("push: %v", got.err)
	}
	if got := w.env.Git(filepath.Join(w.env.Work, "origin.git"), "rev-parse", "main"); got != head {
		t.Errorf("origin is at %s after the push, want %s", got, head)
	}
	// a fetch (the third command) asks again: nothing is kept
	ch = r.runGit(git.Command{Dir: w.work.Dir, Args: []string{"fetch", "-q"}})
	again := r.nextInput(3)
	again.Accept(typed)
	if got := r.wait(ch); got.err != nil {
		t.Fatalf("fetch: %v", got.err)
	}
	noSecretAnywhere(t, r, w, logs, typed)
}

func TestAWrongPasswordFailsAndOffersRetry(t *testing.T) {
	logs := recordLogs(t)
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, nil)
	repo := r.repo(w.work.Dir)
	head := w.publish("two.txt")

	if err := r.api.Run("git.fetch", nil); err != nil {
		t.Fatal(err)
	}
	r.nextInput(0).Accept("not-the-password")
	note := r.nextNote(0)
	if note.Severity != "error" || !strings.HasPrefix(note.Message, "Failed to authenticate to git remote") || strings.Join(note.Actions, ",") != "Retry,Show Git Output" {
		t.Fatalf("the notification is %+v", note)
	}
	if strings.Contains(note.Message, "ada@") || strings.Contains(note.Message, "not-the-password") {
		t.Errorf("the notification names the user or the password: %s", note.Message)
	}
	r.idle(repo)
	if got := w.work.Git("rev-parse", "origin/main"); got == head {
		t.Fatal("the fetch worked with a wrong password")
	}

	// Retry asks again, and the right password finishes the fetch
	r.api.RunNoteAction(0, "Retry")
	r.nextInput(1).Accept(typed)
	r.idle(repo)
	if got := w.work.Git("rev-parse", "origin/main"); got != head {
		t.Errorf("origin/main is %s after the retry, want %s", got, head)
	}
	noSecretAnywhere(t, r, w, logs, "not-the-password")
	noSecretAnywhere(t, r, w, logs, typed)

	// the same from the runner: the tag, with the remote in the error output and no password anywhere in it
	ch := r.runGit(git.Command{Dir: w.work.Dir, Args: []string{"fetch", "-q"}})
	r.nextInput(2).Accept("still-not-the-password")
	got := r.wait(ch)
	wantRoute(t, got.err, "classify.AuthenticationFailed")
	if appErr, ok := errors.FromError(got.err); !ok || strings.Contains(appErr.Route()+describe(got.err), "still-not-the-password") {
		t.Errorf("the error names the password: %v", got.err)
	}
}

func TestEscapeFailsTheCommandWithAuthenticationFailed(t *testing.T) {
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, nil)
	repo := r.repo(w.work.Dir)
	w.publish("two.txt")

	if err := r.api.Run("git.fetch", nil); err != nil {
		t.Fatal(err)
	}
	r.nextInput(0).Cancel()
	note := r.nextNote(0)
	if !strings.HasPrefix(note.Message, "Failed to authenticate to git remote") || strings.Join(note.Actions, ",") != "Retry,Show Git Output" {
		t.Errorf("the notification is %+v", note)
	}
	r.idle(repo)

	// the same, seen from the runner: the tag and the route
	ch := r.runGit(git.Command{Dir: w.work.Dir, Args: []string{"fetch", "-q"}})
	r.nextInput(1).Cancel()
	got := r.wait(ch)
	wantRoute(t, got.err, "classify.AuthenticationFailed")
	if appErr, ok := errors.FromError(got.err); ok && !strings.Contains(git.MaskURLs(describe(got.err)), "") {
		t.Errorf("tag %s", appErr.Tag)
	}
}

func TestACredentialHelperAnswersBeforeThePromptAppears(t *testing.T) {
	w := newHTTPWorld(t)
	// git's own store helper, with its file inside the isolated HOME
	store := filepath.Join(w.env.Home, ".git-credentials")
	writeFile(t, store, strings.Replace(w.host(), "http://", "http://ada:"+typed+"@", 1)+"\n")
	w.env.Git(w.work.Dir, "config", "credential.helper", "store")
	r := newWorldRig(t, w, nil)
	repo := r.repo(w.work.Dir)
	head := w.publish("two.txt")

	if err := r.api.Run("git.fetch", nil); err != nil {
		t.Fatal(err)
	}
	r.idle(repo)
	if got := w.work.Git("rev-parse", "origin/main"); got != head {
		t.Errorf("origin/main is %s, want %s", got, head)
	}
	if n := len(r.api.Inputs()); n != 0 {
		t.Errorf("%d boxes were shown although the credential helper had the password", n)
	}
	if !strings.HasPrefix(store, w.env.Home) {
		t.Errorf("the store file %s is outside the isolated home", store)
	}
}

// fakeSSH runs the script ssh would run, with a prompt, and returns what it printed and its exit status.
func (r *rig) fakeSSH(t *testing.T, sshPrompt string, prompt string) <-chan scriptResult {
	t.Helper()
	srv := r.x.helperServer()
	if srv == nil {
		t.Fatal("no helper server")
	}
	ch := make(chan scriptResult, 1)
	cmd := exec.Command(srv.Script(helper.ScriptSSHAskpass), prompt)
	cmd.Env = append(srv.Environment(true, false), "SSH_ASKPASS_PROMPT="+sshPrompt, "PATH="+os.Getenv("PATH"))
	go func() {
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		code := 0
		if err != nil {
			code = 1
		}
		ch <- scriptResult{out.String(), errOut.String(), code}
	}()
	return ch
}

type scriptResult struct {
	stdout, stderr string
	code           int
}

func (r *rig) waitScript(ch <-chan scriptResult) scriptResult {
	r.t.Helper()
	var out scriptResult
	r.api.WaitFor("the script to end", func() bool {
		r.api.Pump()
		select {
		case out = <-ch:
			return true
		default:
			return false
		}
	})
	return out
}

func TestSSHQuestionsAreAskedFromTheRealTexts(t *testing.T) {
	e := gittest.NewEnv(t)
	r := newRig(t, e, rigOptions{})
	r.settle()
	hostKey := "The authenticity of host 'github.com (140.82.121.4)' can't be established.\n" +
		"ED25519 key fingerprint is SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU.\n" +
		"This key is not known by any other names.\n" +
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? "

	t.Run("a passphrase", func(t *testing.T) {
		ch := r.fakeSSH(t, "", "Enter passphrase for key '/home/u/.ssh/id_ed25519': ")
		in := r.nextInput(0)
		if o := in.Options(); o.Prompt != "Passphrase" || o.Message != "SSH Key: /home/u/.ssh/id_ed25519" || !o.Password || !o.IgnoreFocusOut {
			t.Errorf("the box is %+v", o)
		}
		in.Accept("pass phrase")
		if got := r.waitScript(ch); got.stdout != "pass phrase\n" || got.code != 0 {
			t.Errorf("the script printed %+v", got)
		}
	})
	t.Run("a cancelled passphrase", func(t *testing.T) {
		ch := r.fakeSSH(t, "", "Enter passphrase for key '/home/u/.ssh/id_ed25519': ")
		r.nextInput(1).Cancel()
		if got := r.waitScript(ch); got.stdout != "" || got.code != 1 {
			t.Errorf("the script printed %+v", got)
		}
	})
	t.Run("the host key, yes", func(t *testing.T) {
		ch := r.fakeSSH(t, "confirm", hostKey)
		pick := r.nextPick(0)
		o := pick.Options()
		if o.Placeholder != "Are you sure you want to continue connecting?" || !o.IgnoreFocusOut || strings.Join(pick.Labels(), ",") != "Yes,No" ||
			o.Items[0].Detail != `"github.com (140.82.121.4)" has fingerprint "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"` {
			t.Errorf("the pick is %+v", o)
		}
		pick.Choose(0)
		if got := r.waitScript(ch); got.stdout != "yes\n" || got.code != 0 {
			t.Errorf("the script printed %+v", got)
		}
	})
	t.Run("the host key, no", func(t *testing.T) {
		ch := r.fakeSSH(t, "confirm", hostKey)
		r.nextPick(1).Choose(1)
		if got := r.waitScript(ch); got.stdout != "no\n" || got.code != 0 {
			t.Errorf("the script printed %+v", got)
		}
	})
	t.Run("the host key, escape", func(t *testing.T) {
		ch := r.fakeSSH(t, "confirm", hostKey)
		r.nextPick(2).Close()
		if got := r.waitScript(ch); got.stdout != "" || got.code != 1 {
			t.Errorf("the script printed %+v", got)
		}
	})
	t.Run("a message that wants no answer", func(t *testing.T) {
		before := len(r.api.Notes())
		ch := r.fakeSSH(t, "none", "Confirm user presence for key ECDSA-SK SHA256:abc")
		if got := r.waitScript(ch); got.stdout != "\n" || got.code != 0 {
			t.Errorf("the script printed %+v", got)
		}
		notes := r.api.Notes()
		if len(notes) != before+1 || notes[before].Message != "Confirm user presence for key ECDSA-SK SHA256:abc" || notes[before].Severity != "info" {
			t.Errorf("notifications %+v", notes)
		}
	})
}

func TestQuestionsAreAskedOneAtATime(t *testing.T) {
	e := gittest.NewEnv(t)
	r := newRig(t, e, rigOptions{})
	r.settle()
	first := r.fakeSSH(t, "", "Enter passphrase for key '/k/one': ")
	in := r.nextInput(0)
	second := r.fakeSSH(t, "", "Enter passphrase for key '/k/two': ")
	// the second waits for its turn; nothing but the first box is on screen
	for range 20 {
		r.api.Pump()
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(r.api.Inputs()); n != 1 {
		t.Fatalf("%d boxes are shown at the same time", n)
	}
	in.Accept("one")
	if got := r.waitScript(first); got.stdout != "one\n" {
		t.Errorf("first printed %+v", got)
	}
	next := r.nextInput(1)
	if o := next.Options(); o.Message != "SSH Key: /k/two" {
		t.Errorf("the second box is %+v", o)
	}
	next.Accept("two")
	if got := r.waitScript(second); got.stdout != "two\n" {
		t.Errorf("second printed %+v", got)
	}
}

func TestAskpassSettingsDecideWhatGitGets(t *testing.T) {
	tests := []struct {
		name         string
		settings     map[string]any
		command      string
		background   bool
		wantAskpass  bool
		wantEditor   bool
		wantStarted  bool
		wantNothingF bool
	}{
		{name: "both on, fetch", command: "fetch", wantAskpass: true, wantEditor: true, wantStarted: true},
		{name: "askpass off", settings: map[string]any{"git.useIntegratedAskPass": false}, command: "fetch", wantEditor: true, wantStarted: true},
		{name: "editor off", settings: map[string]any{"git.useEditorAsCommitInput": false}, command: "push", wantAskpass: true, wantStarted: true},
		{name: "both off", settings: map[string]any{"git.useIntegratedAskPass": false, "git.useEditorAsCommitInput": false}, command: "fetch"},
		{name: "a background command never asks", command: "fetch", background: true},
		{name: "a command that never asks", command: "add"},
		{name: "status", command: "status"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := newRig(t, e, rigOptions{settings: tc.settings})
			r.settle()
			env := r.x.helperEnv(context.Background(), tc.command, tc.background)
			has := func(prefix string) bool {
				for _, kv := range env {
					if strings.HasPrefix(kv, prefix) {
						return true
					}
				}
				return false
			}
			if has("GIT_ASKPASS=") != tc.wantAskpass || has("SSH_ASKPASS=") != tc.wantAskpass || has("SSH_ASKPASS_REQUIRE=force") != tc.wantAskpass {
				t.Errorf("askpass variables = %v, want %v: %v", has("GIT_ASKPASS="), tc.wantAskpass, env)
			}
			if has("GIT_EDITOR=") != tc.wantEditor {
				t.Errorf("GIT_EDITOR = %v, want %v: %v", has("GIT_EDITOR="), tc.wantEditor, env)
			}
			if has(helper.EnvToken+"=") != (tc.wantAskpass || tc.wantEditor) {
				t.Errorf("token = %v: %v", has(helper.EnvToken+"="), env)
			}
			if (r.x.helperSrv != nil) != tc.wantStarted {
				t.Errorf("the server is running = %v, want %v", r.x.helperSrv != nil, tc.wantStarted)
			}
		})
	}
}

func TestTheServerLivesInTheRuntimeFolderAndEndsWithTheExtension(t *testing.T) {
	e := gittest.NewEnv(t)
	r := newRig(t, e, rigOptions{})
	r.settle()
	if r.x.helperEnv(context.Background(), "fetch", false) == nil {
		t.Fatal("no variables for a fetch")
	}
	srv := r.x.helperSrv
	runtime := r.x.opts.RuntimeDir
	if want := filepath.Join(runtime, "emetos", "git-"+strconv.Itoa(os.Getpid())); srv.Folder() != want {
		t.Errorf("the folder is %s, want %s", srv.Folder(), want)
	}
	if _, err := os.Stat(srv.Socket()); err != nil {
		t.Fatal(err)
	}
	// the window says the workspace closes before the extension is deactivated: the folder is gone at once, not when a goroutine gets to it
	r.api.Emit(extension.TopicWorkspaceClosed, extension.WorkspaceInfo{})
	r.api.Pump()
	if _, err := os.Stat(srv.Folder()); !os.IsNotExist(err) {
		t.Errorf("the folder of the helper is still there after the workspace closed: %v", err)
	}
	r.cancel()
	r.api.WaitFor("the server to end", func() bool {
		_, err := os.Stat(srv.Folder())
		return os.IsNotExist(err)
	})
	if r.x.helperEnv(context.Background(), "fetch", false) != nil {
		t.Error("variables are given after the extension ended")
	}
}

func TestAServerThatCannotStartIsLoggedOnceAndGitGoesOn(t *testing.T) {
	logs := recordLogs(t)
	e := gittest.NewEnv(t)
	r := newRig(t, e, rigOptions{})
	r.settle()
	file := filepath.Join(e.Root, "not-a-folder")
	writeFile(t, file, "x")
	r.x.opts.RuntimeDir = file
	for range 3 {
		if env := r.x.helperEnv(context.Background(), "fetch", false); env != nil {
			t.Fatalf("variables %v although the server cannot start", env)
		}
	}
	if n := strings.Count(logs.String(), "cannot start"); n != 1 {
		t.Errorf("the failure is logged %d times:\n%s", n, logs.String())
	}
	if lines := r.api.Channel(ChannelName).Lines(); !strings.Contains(strings.Join(lines, "\n"), "cannot start") {
		t.Errorf("the Git channel does not say so: %v", lines)
	}
}

func TestACommandSeesNoSessionOfTheUser(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/nonexistent/agent.sock")
	t.Setenv("DISPLAY", ":99")
	t.Setenv("SSH_ASKPASS", "/nonexistent/ssh-askpass")
	t.Setenv("GIT_ASKPASS", "/nonexistent/askpass")
	t.Setenv("WAYLAND_DISPLAY", "wayland-nonexistent")
	e := gittest.NewEnv(t)
	src := e.Init("hook")
	src.Write("a.txt", "a\n")
	dump := filepath.Join(e.Root, "hook-env.txt")
	src.Hook("pre-commit", "#!/bin/sh\nenv > "+dump+"\n")
	src.Git("add", "-A")
	r := newRig(t, e, rigOptions{folder: src.Dir})
	r.settle()
	ch := r.runGit(git.Command{Dir: src.Dir, Args: []string{"commit", "-q", "-m", "x"}})
	if got := r.wait(ch); got.err != nil {
		t.Fatalf("commit: %v", got.err)
	}
	data, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			vars[k] = v
		}
	}
	for _, name := range []string{"SSH_AUTH_SOCK", "DISPLAY", "WAYLAND_DISPLAY"} {
		if v, has := vars[name]; has {
			t.Errorf("git sees %s=%s from the session of the test", name, v)
		}
	}
	srv := r.x.helperSrv
	for name, want := range map[string]string{
		"GIT_ASKPASS": srv.Script(helper.ScriptAskpass), "SSH_ASKPASS": srv.Script(helper.ScriptSSHAskpass), "SSH_ASKPASS_REQUIRE": "force",
		"GIT_TERMINAL_PROMPT": "0", helper.EnvSocket: srv.Socket(), helper.EnvCommand: "commit",
	} {
		if vars[name] != want {
			t.Errorf("%s = %q, want %q", name, vars[name], want)
		}
	}
	if len(vars[helper.EnvToken]) != 64 {
		t.Errorf("the token is %q", vars[helper.EnvToken])
	}
}

func TestADisabledExtensionAnswersNothing(t *testing.T) {
	e := gittest.NewEnv(t)
	r := newRig(t, e, rigOptions{})
	r.settle()
	srv := r.x.helperServer()
	r.x.cfg.enabled = false
	ch := r.fakeSSH(t, "", "Enter passphrase for key '/k': ")
	if got := r.waitScript(ch); got.stdout != "" || got.code != 1 || !strings.Contains(got.stderr, "turned off") {
		t.Errorf("the script printed %+v", got)
	}
	if n := len(r.api.Inputs()); n != 0 {
		t.Errorf("%d boxes were shown", n)
	}
	_ = srv
}

// A host that cannot show an input box answers a cancel and logs nothing.
func TestAHostWithoutBoxesCancelsInSilence(t *testing.T) {
	logs := recordLogs(t)
	e := gittest.NewEnv(t)
	r := newRig(t, e, rigOptions{})
	r.settle()
	r.x.api = noBoxes{r.x.api}
	ch := r.fakeSSH(t, "", "Enter passphrase for key '/k': ")
	if got := r.waitScript(ch); got.code != 1 {
		t.Errorf("the script printed %+v", got)
	}
	ch = r.fakeSSH(t, "confirm", "Are you sure you want to continue connecting (yes/no)? ")
	if got := r.waitScript(ch); got.code != 1 {
		t.Errorf("the script printed %+v", got)
	}
	if strings.Contains(logs.String(), "cannot ask") {
		t.Errorf("the extension logged:\n%s", logs.String())
	}
}

// noBoxes is an API whose scope has no input box and no quick pick, as a window tab of an extension has none.
type noBoxes struct{ extension.API }

func (noBoxes) InputBox() extension.InputBox           { return noInput{} }
func (noBoxes) QuickPick() extension.QuickPick         { return noPick{} }
func (noBoxes) Notifications() extension.Notifications { return nil }

type noInput struct{}

func (noInput) Show(extension.InputBoxOptions, func(string, bool)) error {
	return errors.New("ExtensionInputBoxUnavailable", "the scope cannot ask for a line of text")
}

type noPick struct{}

func (noPick) Show(extension.QuickPickOptions, func(int)) error {
	return errors.New("ExtensionQuickPickUnavailable", "the scope cannot show a list to choose from")
}

// wantRoute checks that err is an error of Emetos whose route has the given end.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
