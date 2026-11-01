package gitscm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// Autofetch (step 24.15, SPEC §10.30 "Remotes, fetch, pull, push and sync", §12): off by default, a fetch when the repository is idle and
// the window focused, the period on a fake clock, `all`, off after a failed authentication, and the one question after the first good
// remote command. The remotes are local bare repositories and the local test HTTP server; the number of fetches is read from the Git
// channel, which has one "> git fetch" line for each.

// fetches counts the fetch commands in the Git channel.
func (r *rig) fetches() int {
	n := 0
	for _, l := range r.api.Channel(ChannelName).Lines() {
		if strings.HasPrefix(l, "> git fetch") {
			n++
		}
	}
	return n
}

// fetchLine is the n-th fetch command of the Git channel (from 0).
func (r *rig) fetchLine(n int) string {
	for _, l := range r.api.Channel(ChannelName).Lines() {
		if strings.HasPrefix(l, "> git fetch") {
			if n == 0 {
				return l
			}
			n--
		}
	}
	return ""
}

// waitFetches waits until the Git channel has n fetches and the repository is idle again.
func (r *rig) waitFetches(rp *repository, n int) {
	r.t.Helper()
	r.api.WaitFor("fetches", func() bool { return r.fetches() >= n && rp.own == 0 && !rp.refreshing && !rp.again })
}

// stayQuiet pumps for a moment and says that no fetch started.
func (r *rig) noMoreFetches(rp *repository, n int) {
	r.t.Helper()
	time.Sleep(40 * time.Millisecond)
	r.api.Pump()
	if got := r.fetches(); got != n {
		r.t.Errorf("%d fetches, want %d", got, n)
	}
}

func TestAutofetchIsOffByDefaultAndStartsNothing(t *testing.T) {
	w := newSyncWorld(t)
	r, rp := w.open(t, rigOptions{})
	w.remoteCommit("b.txt")
	r.clock.Advance(24 * time.Hour)
	r.api.Pump()
	r.noMoreFetches(rp, 0)
	if rp.af == nil || rp.af.enabled || rp.af.stop != nil || rp.af.wanting || rp.af.running {
		t.Errorf("the autofetcher is not asleep: %+v", rp.af)
	}
	if got := w.work.Git("rev-parse", "origin/main"); got == w.remoteHead() {
		t.Error("origin/main moved with autofetch off")
	}
	// a fake git that records every call: nothing at all runs on a timer, not even a status
	if lines := r.api.Channel(ChannelName).Lines(); hasLine(lines, "> git fetch") {
		t.Errorf("a fetch ran: %v", lines)
	}
}

func (w *syncWorld) remoteHead() string { return w.bare.Git("rev-parse", "main") }

func TestAutofetchFetchesWhenIdleAndAgainAfterThePeriod(t *testing.T) {
	w := newSyncWorld(t)
	first := w.remoteCommit("b.txt")
	r, rp := w.open(t, rigOptions{settings: map[string]any{"git.autofetch": true}})
	r.waitFetches(rp, 1) // the first fetch is due at once
	if got := w.work.Git("rev-parse", "origin/main"); got != first {
		t.Errorf("origin/main = %s, want %s", got, first)
	}
	if line := r.fetchLine(0); strings.Contains(line, "--all") {
		t.Errorf("fetch line %q", line)
	}
	second := w.remoteCommit("c.txt")
	r.clock.Advance(179 * time.Second)
	r.noMoreFetches(rp, 1)
	r.clock.Advance(time.Second)
	r.waitFetches(rp, 2)
	if got := w.work.Git("rev-parse", "origin/main"); got != second {
		t.Errorf("origin/main = %s, want %s", got, second)
	}
	// the status shows what was fetched, with no progress bar and no notification for a fetch nobody asked for
	if ahead, behind := stateOf(r, rp); ahead != 0 || behind != 2 {
		t.Errorf("ahead %d behind %d", ahead, behind)
	}
	if notes := r.noteTexts(); len(notes) != 0 {
		t.Errorf("notifications %v", notes)
	}
	if rp.syncing {
		t.Error("the sync item spins for an autofetch")
	}
}

func TestAutofetchPeriodAndAllRemotes(t *testing.T) {
	w := newSyncWorld(t)
	second := w.work.AddRemote("second")
	w.work.Git("push", "-q", "second", "main")
	clone := second.Clone("second-clone")
	clone.Write("s.txt", "s\n")
	secondHead := clone.Commit("on second")
	clone.Git("push", "-q", "origin", "main")
	r, rp := w.open(t, rigOptions{settings: map[string]any{"git.autofetch": "all", "git.autofetchPeriod": 60}})
	r.waitFetches(rp, 1)
	if line := r.fetchLine(0); !strings.Contains(line, "--all") {
		t.Errorf("fetch line %q", line)
	}
	if got := w.work.Git("rev-parse", "second/main"); got != secondHead {
		t.Errorf("second/main = %s", got)
	}
	r.clock.Advance(59 * time.Second)
	r.noMoreFetches(rp, 1)
	r.clock.Advance(time.Second)
	r.waitFetches(rp, 2)
}

func TestAutofetchPeriodHasALowerBound(t *testing.T) {
	w := newSyncWorld(t)
	r, rp := w.open(t, rigOptions{settings: map[string]any{"git.autofetch": true, "git.autofetchPeriod": 0}})
	r.waitFetches(rp, 1)
	r.clock.Advance(500 * time.Millisecond)
	r.noMoreFetches(rp, 1)
	r.clock.Advance(500 * time.Millisecond)
	r.waitFetches(rp, 2)
}

func TestAutofetchWaitsForTheWindowFocus(t *testing.T) {
	w := newSyncWorld(t)
	r, rp := w.open(t, rigOptions{settings: map[string]any{"git.autofetch": true}})
	r.waitFetches(rp, 1)
	r.api.Emit(extension.TopicWindowFocusChanged, extension.WindowFocus{Focused: false})
	r.api.Pump()
	head := w.remoteCommit("b.txt")
	r.clock.Advance(10 * time.Minute)
	r.noMoreFetches(rp, 1)
	if !rp.af.wanting {
		t.Error("the fetch that is due is not waiting")
	}
	r.api.Emit(extension.TopicWindowFocusChanged, extension.WindowFocus{Focused: true})
	r.waitFetches(rp, 2)
	if got := w.work.Git("rev-parse", "origin/main"); got != head {
		t.Errorf("origin/main = %s, want %s", got, head)
	}
}

func TestAutofetchWaitsForAnOperationToEnd(t *testing.T) {
	w := newSyncWorld(t)
	r, rp := w.open(t, rigOptions{settings: map[string]any{"git.autofetch": true}})
	r.waitFetches(rp, 1)
	rp.beginOwn() // a command of the user is running
	r.clock.Advance(10 * time.Minute)
	r.noMoreFetches(rp, 1)
	rp.endOwn() // and ends
	r.waitFetches(rp, 2)
}

func TestAutofetchFollowsTheSetting(t *testing.T) {
	w := newSyncWorld(t)
	r, rp := w.open(t, rigOptions{})
	r.noMoreFetches(rp, 0)
	r.api.SetSetting("git.autofetch", true)
	r.waitFetches(rp, 1)
	r.api.SetSetting("git.autofetch", false)
	r.clock.Advance(time.Hour)
	r.noMoreFetches(rp, 1)
	if rp.af.stop != nil || rp.af.enabled {
		t.Errorf("the autofetcher stays on: %+v", rp.af)
	}
	r.api.SetSetting("git.autofetch", "all")
	r.waitFetches(rp, 2)
	if !strings.Contains(r.fetchLine(1), "--all") {
		t.Errorf("fetch line %q", r.fetchLine(1))
	}
}

func TestClosingTheRepositoryEndsItsAutofetch(t *testing.T) {
	w := newSyncWorld(t)
	r, rp := w.open(t, rigOptions{settings: map[string]any{"git.autofetch": true}})
	r.waitFetches(rp, 1)
	af := rp.af
	r.run(rp, "git.close", nil)
	r.clock.Advance(time.Hour)
	r.noMoreFetches(rp, 1)
	if af.enabled || af.stop != nil {
		t.Errorf("the autofetcher of the closed repository stays on: %+v", af)
	}
}

// An autofetch never asks for a password: the helper answers a fetch that is marked silent with a cancel, the failure is an
// AuthenticationFailed, and the autofetch turns itself off so that a missing credential does not ask every three minutes.
func TestAFailedAuthenticationTurnsAutofetchOff(t *testing.T) {
	logs := recordLogs(t)
	w := newHTTPWorld(t)
	w.publish("two.txt")
	r := newWorldRig(t, w, map[string]any{"git.autofetch": true})
	rp := r.repo(w.work.Dir)
	r.waitFetches(rp, 1)
	if n := len(r.api.Inputs()); n != 0 {
		t.Fatalf("%d boxes: the autofetch asked for a password", n)
	}
	if notes := r.noteTexts(); len(notes) != 0 {
		t.Errorf("notifications %v: an autofetch has no failure to show", notes)
	}
	r.api.WaitFor("the autofetcher to be off", func() bool { return !rp.af.enabled })
	r.clock.Advance(time.Hour)
	r.noMoreFetches(rp, 1)
	if got := w.work.Git("rev-parse", "origin/main"); got == w.src.Head() {
		t.Error("origin/main moved without a password")
	}
	// the user fetches by hand and is asked, as always
	if err := r.api.Run("git.fetch", nil); err != nil {
		t.Fatal(err)
	}
	r.nextInput(0).Accept(typed)
	r.idle(rp)
	if got := w.work.Git("rev-parse", "origin/main"); got != w.src.Head() {
		t.Errorf("origin/main = %s after the fetch of the user", got)
	}
	noSecretAnywhere(t, r, w, logs, typed)
	// switching the setting off and on again starts it again
	r.api.SetSetting("git.autofetch", false)
	r.api.SetSetting("git.autofetch", true)
	r.waitFetches(rp, 3)
}

func TestOtherFailuresOfAnAutofetchDoNotTurnItOff(t *testing.T) {
	w := newSyncWorld(t)
	w.work.Git("remote", "set-url", "origin", w.env.Root+"/nowhere.git")
	r, rp := w.open(t, rigOptions{settings: map[string]any{"git.autofetch": true}})
	r.waitFetches(rp, 1)
	if !rp.af.enabled {
		t.Fatal("a missing remote turned autofetch off")
	}
	w.work.Git("remote", "set-url", "origin", w.bare.Dir)
	head := w.remoteCommit("b.txt")
	r.clock.Advance(3 * time.Minute)
	r.waitFetches(rp, 2)
	if got := w.work.Git("rev-parse", "origin/main"); got != head {
		t.Errorf("origin/main = %s", got)
	}
	if notes := r.noteTexts(); len(notes) != 0 {
		t.Errorf("notifications %v", notes)
	}
}

// stateFileOf reads the state file of the world.
func stateFileOf(t *testing.T, e *gittest.Env) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.StateHome, "emetos", stateFile))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestTheAutofetchQuestionIsAskedOnceAfterTheFirstGoodRemoteCommand(t *testing.T) {
	const question = `Would you like Emetos to periodically run "git fetch"?`
	askedRig := func(t *testing.T) (*syncWorld, *rig, *repository) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{autofetchQuestion: true})
		return w, r, rp
	}
	t.Run("nothing is fetched on a schedule before the answer", func(t *testing.T) {
		w, r, rp := askedRig(t)
		w.remoteCommit("b.txt")
		r.run(rp, "git.fetch", nil)
		notes := r.api.Notes()
		if len(notes) != 1 || notes[0].Message != question || strings.Join(notes[0].Actions, ",") != "Yes,No,Ask Me Later" || notes[0].Severity != "info" {
			t.Fatalf("notifications %+v", notes)
		}
		r.clock.Advance(time.Hour)
		r.noMoreFetches(rp, 1) // only the fetch of the user
		if rp.af.enabled {
			t.Error("autofetch is on before the answer")
		}
		// a second good command does not ask again
		r.run(rp, "git.fetch", nil)
		if n := len(r.api.Notes()); n != 1 {
			t.Errorf("%d notifications", n)
		}
	})
	t.Run("Yes turns git.autofetch on in the user settings and starts it", func(t *testing.T) {
		w, r, rp := askedRig(t)
		r.run(rp, "git.fetch", nil)
		r.api.RunNoteAction(0, "Yes")
		ups := r.api.SettingUpdates()
		if len(ups) != 1 || ups[0].Key != "git.autofetch" || ups[0].Value != true {
			t.Fatalf("updates %+v", ups)
		}
		if !strings.Contains(stateFileOf(t, w.env), `"autofetchInformed":true`) {
			t.Errorf("state file %q", stateFileOf(t, w.env))
		}
		r.waitFetches(rp, 2)
	})
	t.Run("No is remembered and changes no setting", func(t *testing.T) {
		w, r, rp := askedRig(t)
		r.run(rp, "git.fetch", nil)
		r.api.RunNoteAction(0, "No")
		if ups := r.api.SettingUpdates(); len(ups) != 0 {
			t.Errorf("updates %+v", ups)
		}
		if !strings.Contains(stateFileOf(t, w.env), `"autofetchInformed":true`) {
			t.Errorf("state file %q", stateFileOf(t, w.env))
		}
		// another window of the same profile does not ask
		r2 := newRig(t, w.env, rigOptions{folder: w.work.Dir, autofetchQuestion: true})
		r2.settle()
		rp2 := r2.repo(w.work.Dir)
		r2.run(rp2, "git.fetch", nil)
		if notes := r2.noteTexts(); len(notes) != 0 {
			t.Errorf("asked again: %v", notes)
		}
		r2.clock.Advance(time.Hour)
		r2.noMoreFetches(rp2, 1)
	})
	t.Run("Ask Me Later keeps the question for the next time", func(t *testing.T) {
		w, r, rp := askedRig(t)
		r.run(rp, "git.fetch", nil)
		r.api.RunNoteAction(0, "Ask Me Later")
		if ups := r.api.SettingUpdates(); len(ups) != 0 {
			t.Errorf("updates %+v", ups)
		}
		if strings.Contains(stateFileOf(t, w.env), "autofetchInformed") {
			t.Errorf("state file %q", stateFileOf(t, w.env))
		}
		r2 := newRig(t, w.env, rigOptions{folder: w.work.Dir, autofetchQuestion: true})
		r2.settle()
		r2.run(r2.repo(w.work.Dir), "git.fetch", nil)
		if notes := r2.noteTexts(); len(notes) != 1 {
			t.Errorf("notifications of the next window %v", notes)
		}
	})
	t.Run("a pull, a push and a sync count; a failure and the addition of a remote do not", func(t *testing.T) {
		for _, cmd := range []string{"git.pull", "git.push", "git.sync"} {
			w := newSyncWorld(t)
			r, rp := w.open(t, rigOptions{autofetchQuestion: true, settings: map[string]any{"git.confirmSync": false}})
			if cmd != "git.pull" {
				w.localCommit("c.txt")
			}
			r.run(rp, cmd, nil)
			if notes := r.noteTexts(); len(notes) != 1 || !strings.HasSuffix(notes[0], question) {
				t.Errorf("%s: notifications %v", cmd, notes)
			}
		}
		w := newSyncWorld(t)
		w.work.Git("remote", "set-url", "origin", w.env.Root+"/nowhere.git")
		r, rp := w.open(t, rigOptions{autofetchQuestion: true})
		r.run(rp, "git.fetch", nil)
		for _, n := range r.api.Notes() {
			if n.Message == question {
				t.Error("the question was asked after a failure")
			}
		}
		if rp.af.enabled {
			t.Error("autofetch on")
		}
	})
	t.Run("with autofetch on already the question counts as answered", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{autofetchQuestion: true, settings: map[string]any{"git.autofetch": true}})
		r.waitFetches(rp, 1)
		if notes := r.noteTexts(); len(notes) != 0 {
			t.Errorf("notifications %v", notes)
		}
		if !strings.Contains(stateFileOf(t, w.env), `"autofetchInformed":true`) {
			t.Errorf("state file %q", stateFileOf(t, w.env))
		}
	})
}

func TestTheStateFileOfTheProfile(t *testing.T) {
	dir := t.TempDir()
	t.Run("a file that is not JSON is an empty state", func(t *testing.T) {
		path := filepath.Join(dir, stateFile)
		if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := newStateStore(dir).get(); got != (profileState{}) {
			t.Errorf("state %+v", got)
		}
	})
	t.Run("a file others can write is not read", func(t *testing.T) {
		path := filepath.Join(dir, stateFile)
		if err := os.WriteFile(path, []byte(`{"autofetchInformed":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o666); err != nil {
			t.Fatal(err)
		}
		if got := newStateStore(dir).get(); got != (profileState{}) {
			t.Errorf("state %+v", got)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := newStateStore(dir).get(); !got.AutofetchInformed {
			t.Errorf("state %+v", got)
		}
	})
	t.Run("a missing file is an empty state", func(t *testing.T) {
		if got := newStateStore(t.TempDir()).get(); got != (profileState{}) {
			t.Errorf("state %+v", got)
		}
	})
}
