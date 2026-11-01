package gitscm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Git for the client of a shared workspace (step 24.23, SPEC §10.30 "Security and privacy", D38): every Git command works for a client of
// Remote Work, with the credentials, keys and agent of the host, exactly as at the host's keyboard. A client's command runs in the process
// of the host, in the extension of the host's window tab, so what a client can do is what these tests do: the five commands that reach a
// remote, each asking the credential prompt of the host's window (the box of the host's quick input, which the test answers) and doing
// its work with the host's git. Nothing refuses Git to a client: command.ClientRefusal knows no command of the extension (cmd/emetos checks that, as an extension may not import the core).

func TestFetchPullPushSyncAndCloneAskTheHostForItsCredentials(t *testing.T) {
	logs := recordLogs(t)
	w := newHTTPWorld(t)
	bare := filepath.Join(w.env.Work, "origin.git")
	r := newWorldRig(t, w, map[string]any{"git.confirmSync": false})
	rp := r.repo(w.work.Dir)
	box := 0
	// the source of the world has not seen what the clone pushed: it takes that first
	publish := func(name string) string {
		w.src.Git("pull", "-q", "--rebase", "origin", "main")
		return w.publish(name)
	}
	answer := func(t *testing.T) {
		t.Helper()
		in := r.nextInput(box)
		box++
		if o := in.Options(); !strings.HasPrefix(o.Prompt, "Password for '") || !strings.Contains(o.Prompt, "ada@") || !o.Password || o.Message != "Git: "+w.host() {
			t.Errorf("the box %d is %+v", box, o)
		}
		in.Accept(typed)
	}

	t.Run("git.fetch", func(t *testing.T) {
		head := publish("fetched.txt")
		if err := r.api.Run("git.fetch", nil); err != nil {
			t.Fatal(err)
		}
		answer(t)
		r.idle(rp)
		if got := w.work.Git("rev-parse", "origin/main"); got != head {
			t.Errorf("origin/main is %s, want %s", got, head)
		}
	})
	t.Run("git.pull", func(t *testing.T) {
		head := publish("pulled.txt")
		if err := r.api.Run("git.pull", nil); err != nil {
			t.Fatal(err)
		}
		answer(t)
		r.idle(rp)
		if got := w.work.Head(); got != head {
			t.Errorf("HEAD is %s after the pull, want %s", got, head)
		}
	})
	t.Run("git.push", func(t *testing.T) {
		w.work.Write("pushed.txt", "mine\n")
		head := w.work.Commit("pushed")
		r.refresh(rp)
		if err := r.api.Run("git.push", nil); err != nil {
			t.Fatal(err)
		}
		answer(t)
		r.idle(rp)
		if got := w.env.Git(bare, "rev-parse", "main"); got != head {
			t.Errorf("the remote has %s, want %s", got, head)
		}
	})
	t.Run("git.sync", func(t *testing.T) {
		w.work.Git("config", "pull.rebase", "false") // diverged branches are merged, which needs no choice from the person
		publish("upstream.txt")
		w.work.Write("local.txt", "local\n")
		w.work.Commit("local")
		r.refresh(rp)
		if err := r.api.Run("git.sync", nil); err != nil {
			t.Fatal(err)
		}
		answer(t) // the pull
		answer(t) // the push: nothing is remembered between the two (SPEC §10.30: Emetos stores no credential)
		r.idle(rp)
		if got, want := w.env.Git(bare, "rev-parse", "main"), w.work.Head(); got != want {
			t.Errorf("the remote has %s after the sync, the clone %s", got, want)
		}
		for _, f := range []string{"upstream.txt", "local.txt"} {
			if _, err := os.Stat(filepath.Join(w.work.Dir, f)); err != nil {
				t.Errorf("the sync lost %s: %v", f, err)
			}
		}
	})
	t.Run("git.clone", func(t *testing.T) {
		dest := filepath.Join(w.env.Root, "clones")
		r.api.AnswerFileChooser(dest, nil)
		// the address with no user name: the window has a clone of the one with it open, and Clone opens that one
		if err := r.api.Run("git.clone", w.plain); err != nil {
			t.Fatal(err)
		}
		user := r.nextInput(box)
		box++
		if o := user.Options(); !strings.HasPrefix(o.Prompt, "Username for '") || o.Password {
			t.Errorf("the first box is %+v", o)
		}
		user.Accept("ada")
		answer(t)
		r.api.WaitFor("the clone", func() bool {
			r.api.Pump()
			_, err := os.Stat(filepath.Join(dest, "origin", "local.txt"))
			return err == nil
		})
	})
	noSecretAnywhere(t, r, w, logs, typed)
}
