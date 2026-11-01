package gitscm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The remote commands that need credentials (step 24.15): HTTP authentication through the helper of step 24.9 against the local test
// server (a wrong password, a cancelled prompt), ssh:// URLs with a fake ssh program that fails with a permission error and that asks
// SSH_ASKPASS for a passphrase, a sync that can be cancelled. Nothing leaves the machine: the address of the "ssh server" is a documentation
// address that the fake program never contacts.

func TestPushAsksForThePasswordAndOffersRetryAfterAWrongOne(t *testing.T) {
	logs := recordLogs(t)
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, nil)
	rp := r.repo(w.work.Dir)
	w.work.Write("mine.txt", "mine\n")
	head := w.work.Commit("mine")
	r.refresh(rp)

	if err := r.api.Run("git.push", nil); err != nil {
		t.Fatal(err)
	}
	in := r.nextInput(0)
	if o := in.Options(); !strings.HasPrefix(o.Prompt, "Password for '") || !o.Password {
		t.Errorf("the box is %+v", o)
	}
	in.Accept("not-the-password")
	note := r.nextNote(0)
	if note.Severity != "error" || !strings.HasPrefix(note.Message, "Failed to authenticate to git remote") || strings.Join(note.Actions, ",") != "Retry,Show Git Output" {
		t.Fatalf("notification %+v", note)
	}
	r.idle(rp)
	if got := w.env.Git(filepath.Join(w.env.Work, "origin.git"), "rev-parse", "main"); got == head {
		t.Fatal("the wrong password pushed")
	}
	// Retry pushes again, and the right password goes through
	r.api.RunNoteAction(0, "Retry")
	r.nextInput(1).Accept(typed)
	r.idle(rp)
	if got := w.env.Git(filepath.Join(w.env.Work, "origin.git"), "rev-parse", "main"); got != head {
		t.Errorf("origin is at %s after the retry, want %s", got, head)
	}
	noSecretAnywhere(t, r, w, logs, typed)
	noSecretAnywhere(t, r, w, logs, "not-the-password")
}

func TestACancelledPasswordPromptOfAPushFails(t *testing.T) {
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, nil)
	rp := r.repo(w.work.Dir)
	w.work.Write("mine.txt", "mine\n")
	w.work.Commit("mine")
	r.refresh(rp)
	if err := r.api.Run("git.push", nil); err != nil {
		t.Fatal(err)
	}
	r.nextInput(0).Cancel()
	note := r.nextNote(0)
	if note.Severity != "error" || !strings.HasPrefix(note.Message, "Failed to authenticate to git remote") || strings.Join(note.Actions, ",") != "Retry,Show Git Output" {
		t.Fatalf("notification %+v", note)
	}
	r.idle(rp)
	if rp.syncing || r.source(rp.root).Busy() {
		t.Error("the push still looks like it runs")
	}
	// the right password after Retry
	r.api.RunNoteAction(0, "Retry")
	r.nextInput(1).Accept(typed)
	r.idle(rp)
	if got := w.env.Git(filepath.Join(w.env.Work, "origin.git"), "rev-parse", "main"); got != w.work.Head() {
		t.Errorf("origin is at %s", got)
	}
}

func TestACancelledPasswordPromptOfAPullFails(t *testing.T) {
	w := newHTTPWorld(t)
	w.publish("two.txt")
	r := newWorldRig(t, w, nil)
	rp := r.repo(w.work.Dir)
	before := w.work.Head()
	if err := r.api.Run("git.pull", nil); err != nil {
		t.Fatal(err)
	}
	r.nextInput(0).Cancel()
	note := r.nextNote(0)
	if !strings.HasPrefix(note.Message, "Failed to authenticate to git remote") {
		t.Fatalf("notification %+v", note)
	}
	r.idle(rp)
	if w.work.Head() != before {
		t.Error("the pull went through")
	}
	if rp.syncing {
		t.Error("syncing stays on")
	}
}

// fakeSSH writes a program that stands in for ssh: it asks SSH_ASKPASS for the passphrase of a key as OpenSSH does, keeps the answer in a
// file, and then fails with a permission error. It returns its path and the file of the answer.
func fakeSSH(t *testing.T) (program, answer string) {
	t.Helper()
	dir := t.TempDir()
	program, answer = filepath.Join(dir, "ssh"), filepath.Join(dir, "answer")
	script := `#!/bin/sh
case "$FAKE_SSH_MODE" in
  passphrase)
    pass=$("$SSH_ASKPASS" "Enter passphrase for key '/home/ada/.ssh/id_ed25519': ") || {
      echo "ada@192.0.2.1: Permission denied (publickey)." >&2
      exit 255
    }
    printf '%s' "$pass" > "$FAKE_SSH_ANSWER"
    ;;
esac
echo "ada@192.0.2.1: Permission denied (publickey)." >&2
exit 255
`
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, answer
}

// sshWorld is a repository whose origin has an ssh:// URL that the fake ssh program fails to reach.
func sshWorld(t *testing.T, mode string) (*syncWorld, *rig, *repository, string) {
	t.Helper()
	w := newSyncWorld(t)
	prog, answer := fakeSSH(t)
	w.work.Git("remote", "set-url", "origin", "ssh://ada@192.0.2.1/srv/git/origin.git")
	r, rp := w.open(t, rigOptions{extraEnv: []string{
		"GIT_SSH_COMMAND=" + prog, "GIT_ALLOW_PROTOCOL=file:http:ssh", "FAKE_SSH_MODE=" + mode, "FAKE_SSH_ANSWER=" + answer,
	}})
	return w, r, rp, answer
}

func TestAnSSHRemoteThatRefusesTheKeyFailsWithTheMessageOfSSH(t *testing.T) {
	_, r, rp, _ := sshWorld(t, "denied")
	for _, cmd := range []string{"git.fetch", "git.pull", "git.push"} {
		n := len(r.api.Notes())
		if cmd == "git.push" {
			r.localCommitNow(rp)
		}
		r.run(rp, cmd, nil)
		notes := r.api.Notes()
		if len(notes) != n+1 {
			t.Fatalf("%s: notifications %v", cmd, r.noteTexts())
		}
		note := notes[n]
		if note.Severity != "error" || note.Message != "Git: ada@192.0.2.1: Permission denied (publickey)." || strings.Join(note.Actions, ",") != "Show Git Output" {
			t.Errorf("%s: notification %+v", cmd, note)
		}
	}
	if len(r.api.Inputs()) != 0 {
		t.Errorf("%d boxes for a program that asks nothing", len(r.api.Inputs()))
	}
}

// localCommitNow commits a file in the work repository of the rig.
func (r *rig) localCommitNow(rp *repository) {
	r.t.Helper()
	writeFile(r.t, filepath.Join(rp.root, "ahead.txt"), "ahead\n")
	g := &gittest.Repo{Env: r.env, Dir: rp.root}
	g.Commit("ahead")
	r.refresh(rp)
}

// The tags of the failures: a push is a PermissionDenied, a fetch and a pull are a RemoteConnectionError (git says only that the remote
// could not be read).
func TestTheTagsOfAnSSHFailure(t *testing.T) {
	_, r, rp, _ := sshWorld(t, "denied")
	repo := rp.repo
	for _, tc := range []struct {
		name string
		run  func() error
		tag  string
	}{
		{"push", func() error { return repo.Push(r.x.ctx, git.PushOptions{Remote: "origin", Refspec: "main:main"}) }, "PermissionDenied"},
		{"fetch", func() error { return repo.Fetch(r.x.ctx, git.FetchOptions{}) }, "RemoteConnectionError"},
		{"pull", func() error { _, err := repo.Pull(r.x.ctx, git.PullOptions{}); return err }, "RemoteConnectionError"},
	} {
		if got := errTag(tc.run()); got != tc.tag {
			t.Errorf("%s: tag %q, want %q", tc.name, got, tc.tag)
		}
	}
}

func TestTheSSHPassphrasePromptReachesTheInputBox(t *testing.T) {
	_, r, rp, answer := sshWorld(t, "passphrase")
	if err := r.api.Run("git.fetch", nil); err != nil {
		t.Fatal(err)
	}
	in := r.nextInput(0)
	o := in.Options()
	if o.Prompt != "Passphrase" || o.Message != "SSH Key: /home/ada/.ssh/id_ed25519" || !o.Password || !o.IgnoreFocusOut {
		t.Errorf("the box is %+v", o)
	}
	in.Accept("a pass phrase")
	r.idle(rp)
	data, err := os.ReadFile(answer)
	if err != nil || string(data) != "a pass phrase" {
		t.Errorf("the program was given %q (%v)", data, err)
	}
	note := r.lastNote()
	if note.Message != "Git: ada@192.0.2.1: Permission denied (publickey)." {
		t.Errorf("notification %+v", note)
	}
	for _, line := range r.api.Channel(ChannelName).Lines() {
		if strings.Contains(line, "a pass phrase") {
			t.Errorf("the passphrase is in the Git channel: %s", line)
		}
	}
	for _, n := range r.api.Notes() {
		if strings.Contains(n.Message, "a pass phrase") {
			t.Errorf("the passphrase is in a notification: %s", n.Message)
		}
	}
}

func TestACancelledSSHPromptIsAPermissionError(t *testing.T) {
	_, r, rp, answer := sshWorld(t, "passphrase")
	if err := r.api.Run("git.fetch", nil); err != nil {
		t.Fatal(err)
	}
	r.nextInput(0).Cancel()
	r.idle(rp)
	if _, err := os.Stat(answer); err == nil {
		t.Error("the program was given an answer")
	}
	if note := r.lastNote(); note.Message != "Git: ada@192.0.2.1: Permission denied (publickey)." {
		t.Errorf("notification %+v", note)
	}
}

// With git.supportCancellation a sync shows a notification with a Cancel button and the progress of the transfer.
func TestASyncThatTheUserMayCancel(t *testing.T) {
	t.Run("progress and the end of the notification", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.supportCancellation": true, "git.confirmSync": false}})
		for i := 0; i < 8; i++ {
			w.other.Write("f"+string(rune('a'+i))+".txt", strings.Repeat("data of the file\n", 100))
		}
		w.other.Commit("many files")
		w.other.Git("push", "-q", "origin", "main")
		w.localCommit("mine.txt")
		r.run(rp, "git.sync", nil)
		progs := r.api.Progresses()
		if len(progs) != 1 || progs[0].Title() != "Syncing. Cancelling may cause serious damages to the repository" || !progs[0].Cancellable() {
			t.Fatalf("progresses %+v", progs)
		}
		if !progs[0].Ended() {
			t.Error("the notification stays after the sync")
		}
		for _, rep := range progs[0].Reports() {
			if rep.Percent < 0 || rep.Percent > 100 || rep.Message == "" {
				t.Errorf("report %+v", rep)
			}
		}
		if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
			t.Error("not pushed")
		}
	})
	t.Run("without the setting there is no notification", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.confirmSync": false}})
		w.localCommit("mine.txt")
		r.run(rp, "git.sync", nil)
		if n := len(r.api.Progresses()); n != 0 {
			t.Errorf("%d progresses", n)
		}
	})
	t.Run("Cancel ends the pull and the sync, and pushes nothing", func(t *testing.T) {
		w := newHTTPWorld(t)
		w.publish("two.txt")
		w.work.Write("mine.txt", "mine\n")
		w.work.Commit("mine")
		r := newWorldRig(t, w, map[string]any{"git.supportCancellation": true, "git.confirmSync": false})
		rp := r.repo(w.work.Dir)
		before := w.env.Git(filepath.Join(w.env.Work, "origin.git"), "rev-parse", "main")
		if err := r.api.Run("git.sync", nil); err != nil {
			t.Fatal(err)
		}
		r.nextInput(0) // the pull waits for the password
		progs := r.api.Progresses()
		if len(progs) != 1 || progs[0].Ended() {
			t.Fatalf("progresses %+v", progs)
		}
		progs[0].Cancel()
		r.idle(rp)
		if rp.syncing {
			t.Error("the sync item still spins")
		}
		if got := w.env.Git(filepath.Join(w.env.Work, "origin.git"), "rev-parse", "main"); got != before {
			t.Error("a push followed the cancelled pull")
		}
		for _, n := range r.api.Notes() {
			if n.Severity == "error" {
				t.Errorf("a cancelled sync made a failure: %+v", n)
			}
		}
	})
}
