package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/agents/claude/claudetest"
)

func TestTwoSessionTabsRunAtOnceAndEachKeepsItsOwnProgram(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	a, b := r.open(), r.open()
	r.ready(a)
	r.ready(b)
	starts := r.started()
	if len(starts) != 2 || starts[0].PGID == starts[1].PGID || a.id == b.id {
		t.Fatalf("starts = %+v", starts)
	}
	ids := []string{starts[0].value("--session-id"), starts[1].value("--session-id")}
	if !slices.Contains(ids, a.id) || !slices.Contains(ids, b.id) {
		t.Errorf("session ids %v, tabs %s %s", ids, a.id, b.id)
	}
	r.send(a, "question for a")
	r.send(b, "question for b")
	r.idle(a)
	r.idle(b)
	for _, tab := range []*sessionTab{a, b} {
		got := texts(tab)
		if len(got) != 5 || !strings.HasPrefix(got[0], "user:question for") || got[4] != "text:It is a Go file." {
			t.Errorf("the conversation of %s is %q", tab.id, got)
		}
	}
	if texts(a)[0] == texts(b)[0] {
		t.Error("the two tabs show the same message")
	}
	// closing one tab ends its program only
	pgid := a.sess.PID()
	a.tab.Close()
	r.ext.wait()
	if err := syscall.Kill(-pgid, 0); err == nil {
		t.Error("the program of the closed tab still runs")
	}
	r.send(b, "still there?")
	r.idle(b)
	if got := texts(b); got[len(got)-1] != "text:Second answer." {
		t.Errorf("the other tab ends with %q", got[len(got)-1])
	}
}

func TestARestoredTabResumesWhenTheUserSendsNotAtStart(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "What is main.go?")
	r.idle(tab)
	r.chooseSaved(tab)
	saved := r.api.EditorTabs()[0].State()
	index, _ := r.api.View(ViewID).Content.SaveState()
	rawIndex, _ := json.Marshal(index)
	before := texts(tab)
	id := tab.id
	r.finish()

	// the next session of Emetos: the view comes back first, then the tab
	r2 := newRig(t, "chat.jsonl")
	if err := r2.api.View(ViewID).Content.RestoreState(rawIndex); err != nil {
		t.Fatal(err)
	}
	r2.api.RestoreTab(KindID, "tab-restored", saved)
	restored := r2.ext.tabs[0]
	if restored.id != id || !slices.Equal(texts(restored), before) || !restored.used || restored.model != "opus" {
		t.Fatalf("restored: id %s used %v model %q items %q", restored.id, restored.used, restored.model, texts(restored))
	}
	if restored.phase != phaseIdle || len(restored.commands) != 3 || len(restored.models) != 2 {
		t.Errorf("phase %v, commands %d, models %d", restored.phase, len(restored.commands), len(restored.models))
	}
	if got := r2.api.EditorTabs()[0].Title(); got != "What is main.go?" {
		t.Errorf("the title of the restored tab is %q", got)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(r2.started()); n != 0 {
		t.Fatalf("a restored tab started %d programs before the user sent anything", n)
	}
	// the user sends: the program is started with --resume, with the model of the tab
	r2.send(restored, "go on")
	r2.idle(restored)
	starts := r2.started()
	if len(starts) != 1 || starts[0].value("--resume") != id || starts[0].has("--session-id") || starts[0].value("--model") != "opus" {
		t.Fatalf("starts = %+v", starts)
	}
	// the fake replays its script from the start, so the answer is the one of its first turn
	if got := texts(restored); len(got) <= len(before)+1 || got[len(before)] != "user:go on" || got[len(got)-1] != "text:It is a Go file." {
		t.Errorf("the conversation is %q", got)
	}
}

// chooseSaved picks the model before the tab is saved.
func (r *rig) chooseSaved(t *sessionTab) { t.chooseModel("opus") }

func TestARestoredTabThatNeverSentAMessageStartsANewSession(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	id := "11111111-2222-4333-8444-555555555555"
	state, _ := json.Marshal(tabState{Session: id})
	r.api.RestoreTab(KindID, "t", state)
	tab := r.ext.tabs[0]
	r.send(tab, "first")
	r.ready(tab)
	starts := r.started()
	if len(starts) != 1 || starts[0].value("--session-id") != id || starts[0].has("--resume") {
		t.Errorf("starts = %+v", starts)
	}
}

func TestAMissingProgramShowsTheStateAndCheckAgainStartsIt(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	r.api.SetSetting(PathSetting, filepath.Join(r.folder, "no-such-program"))
	tab := r.open()
	if tab.phase != phaseMissing {
		t.Fatalf("phase = %v", tab.phase)
	}
	if len(r.started()) != 0 {
		t.Error("something was started")
	}
	// sending does nothing: the banner says what to do
	tab.input.SetValue("hello")
	tab.submit("hello")
	if len(tab.tr.items) != 0 || tab.busy {
		t.Errorf("a message was taken in the missing state: %q", texts(tab))
	}
	// the program is put where the setting says: Check Again starts it
	r.api.SetSetting(PathSetting, fakeProgram)
	tab.retry()
	r.ready(tab)
}

func TestARelativePathOfTheSettingIsRelativeToTheFolder(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	if err := os.Symlink(fakeProgram, filepath.Join(r.folder, "bin-claude")); err != nil {
		t.Fatal(err)
	}
	r.api.SetSetting(PathSetting, "./bin-claude")
	if got := r.ext.adapter().Path; got != filepath.Join(r.folder, "bin-claude") {
		t.Errorf("Path = %q", got)
	}
	tab := r.open()
	r.ready(tab)
	// a name with no slash is looked up in PATH, as the program's own name is
	r.api.SetSetting(PathSetting, "claude")
	if got := r.ext.adapter().Path; got != "claude" {
		t.Errorf("Path = %q", got)
	}
	r.api.SetSetting(PathSetting, "")
	if got := r.ext.adapter().Path; got != "" {
		t.Errorf("Path = %q", got)
	}
}

func TestNotLoggedInShowsTheStateAndTheLoginStartsTheProgram(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	t.Setenv(claudetest.EnvAuth, "out")
	tab := r.open()
	r.api.WaitFor("the logged-out state", func() bool { return tab.phase == phaseLoggedOut })
	if len(r.started()) != 0 {
		t.Error("a session was started for a user who is not logged in")
	}
	if r.ext.loginState.Load() != loginOut {
		t.Error("the extension should remember that the user is not logged in")
	}

	// Log in: the program's own flow runs in the panel
	r.ext.startLogin()
	run := r.ext.login
	if run == nil || r.api.View(LoginViewID) == nil {
		t.Fatal("no login run")
	}
	if got := r.api.View(LoginViewID).Reveals; len(got) != 1 || !got[0] {
		t.Errorf("the panel was revealed with %v", got)
	}
	r.api.WaitFor("the prompt of the login", func() bool {
		run.mu.Lock()
		defer run.mu.Unlock()
		return strings.Contains(run.output.text(), "Paste the code here")
	})
	// a second Log in while one runs only shows the panel
	r.ext.startLogin()
	if r.ext.login != run {
		t.Error("a second login was started while one runs")
	}
	// a wrong code fails the login: the tab stays logged out
	run.sendLine(r.ext, "wrong")
	r.api.WaitFor("the login to end", func() bool { return run.finished })
	if !strings.Contains(run.status, "exit status 1") {
		t.Errorf("status = %q", run.status)
	}
	r.api.WaitFor("the status to be checked", func() bool { return strings.Contains(run.status, "not logged in") })
	if tab.phase != phaseLoggedOut {
		t.Errorf("phase = %v", tab.phase)
	}

	// the right code: the login succeeds and the waiting tab starts its program
	r.ext.startLogin()
	second := r.ext.login
	if second == run {
		t.Fatal("a finished login was not started again")
	}
	r.api.WaitFor("the prompt", func() bool {
		second.mu.Lock()
		defer second.mu.Unlock()
		return strings.Contains(second.output.text(), "Paste the code here")
	})
	second.sendLine(r.ext, "code")
	r.api.WaitFor("the tab to be ready", func() bool { return tab.phase == phaseReady })
	if r.ext.loginState.Load() != loginIn || !strings.Contains(second.status, "logged in") {
		t.Errorf("login state %d, status %q", r.ext.loginState.Load(), second.status)
	}
	if infos := r.api.Notes(); len(infos) == 0 || infos[len(infos)-1].Message != "The agent is logged in." {
		t.Errorf("notes = %+v", infos)
	}
	if len(r.started()) != 1 {
		t.Errorf("starts = %d", len(r.started()))
	}
	if !strings.Contains(second.output.text(), "Login successful.") || strings.Contains(second.output.text(), "code\n") {
		t.Errorf("the output is %q (what the user typed must not be shown again)", second.output.text())
	}
}

func TestCancelEndsTheLoginAndTheScopeEndsIt(t *testing.T) {
	r := newRig(t, "")
	t.Setenv(claudetest.EnvAuth, "hang")
	r.ext.startLogin()
	run := r.ext.login
	r.api.WaitFor("the prompt", func() bool {
		run.mu.Lock()
		defer run.mu.Unlock()
		return strings.Contains(run.output.text(), "Paste the code here")
	})
	pgid := run.proc.pgid
	run.proc.stop()
	r.api.WaitFor("the login to end", func() bool { return run.finished })
	if err := syscall.Kill(-pgid, 0); err == nil {
		t.Error("the program of the login is still running")
	}

	// a login that runs when the scope ends is ended with it
	t.Setenv(claudetest.EnvAuth, "hang")
	r.ext.startLogin()
	second := r.ext.login
	pgid = second.proc.pgid
	r.cancel()
	select {
	case <-second.proc.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the login survived the end of the scope")
	}
	if err := syscall.Kill(-pgid, 0); err == nil {
		t.Error("the program of the login is still running")
	}
}

func TestLoginWithoutTheProgramOrAFolderIsRefusedWithANotification(t *testing.T) {
	r := newRig(t, "")
	r.api.SetSetting(PathSetting, filepath.Join(r.folder, "nope"))
	r.ext.startLogin()
	if r.ext.login != nil || len(r.api.Notes()) != 1 || !strings.Contains(r.api.Notes()[0].Message, "claude program was not found") {
		t.Errorf("login %v notes %+v", r.ext.login, r.api.Notes())
	}
	r2 := newRig(t, "")
	r2.ext.api = noFolderAPI{r2.api}
	r2.ext.startLogin()
	if r2.ext.login != nil || len(r2.api.Notes()) != 1 || !strings.Contains(r2.api.Notes()[0].Message, "Open a folder") {
		t.Errorf("login %v notes %+v", r2.ext.login, r2.api.Notes())
	}
}

func TestAProgramThatCannotCheckTheLoginFailsTheTab(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	t.Setenv(claudetest.EnvAuth, "crash")
	tab := r.open()
	r.api.WaitFor("the failure", func() bool { return tab.phase == phaseFailed })
	if !strings.Contains(tab.problem, "could not be started") {
		t.Errorf("problem = %q", tab.problem)
	}
	// sending from the failed state tries again, and the program is fine now
	t.Setenv(claudetest.EnvAuth, "in")
	r.send(tab, "try")
	r.idle(tab)
	if got := texts(tab); got[len(got)-1] != "text:It is a Go file." {
		t.Errorf("the conversation is %q", got)
	}
}

func TestTheAgentReportingALoggedOutUserSendsTheTabToTheLogin(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.push(tab.gen, mustParse(t, `{"type":"assistant","uuid":"a","session_id":"s","parent_tool_use_id":null,"error":"authentication_failed","message":{"id":"m","model":"m","content":[{"type":"text","text":"Please run /login"}]}}`))
	r.api.Pump()
	if tab.phase != phaseLoggedOut || r.ext.loginState.Load() != loginOut {
		t.Errorf("phase %v login %d", tab.phase, r.ext.loginState.Load())
	}
}

func TestEveryProgramEndsWithItsScope(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	a, b := r.open(), r.open()
	r.ready(a)
	r.ready(b)
	r.cancel() // the extension is deactivated: the context of Activate ends
	for _, tab := range []*sessionTab{a, b} {
		select {
		case <-tab.sess.Exited():
		case <-time.After(10 * time.Second):
			t.Fatal("a program survived the end of the scope")
		}
	}
	_ = context.Background()
}
