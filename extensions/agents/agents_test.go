package agents

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// mustParse reads a line of the program's output.
func mustParse(t *testing.T, line string) claude.Message {
	t.Helper()
	m, err := claude.ParseLine([]byte(line))
	if err != nil {
		t.Fatalf("ParseLine(%s): %v", line, err)
	}
	return m
}

// openIdle opens a session tab that does not start the program.
func (r *rig) openIdle(id string) *sessionTab {
	r.t.Helper()
	if id == "" {
		var err error
		if id, err = newUUID(rand.Reader); err != nil {
			r.t.Fatal(err)
		}
	}
	if _, err := r.api.Editors().Open(KindID, tabState{Session: id}, extension.EditorOpenOptions{}); err != nil {
		r.t.Fatal(err)
	}
	return r.ext.tabs[len(r.ext.tabs)-1]
}

func TestTheManifestIsValidAndDeclaresTheContributions(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	c := m.Contributes
	if len(c.EditorKinds) != 1 || c.EditorKinds[0].ID != KindID {
		t.Errorf("editor kinds = %+v", c.EditorKinds)
	}
	if len(c.EditorActions) != 1 || c.EditorActions[0].Command != NewSessionCommand || c.EditorActions[0].Tooltip != "New Session (runs Claude Code)" {
		t.Errorf("editor actions = %+v", c.EditorActions)
	}
	if len(c.ViewContainers) != 1 || len(c.Views) != 1 || len(c.PanelViews) != 1 || c.PanelViews[0].Title != "Agents: Log in" {
		t.Errorf("views = %+v %+v %+v", c.ViewContainers, c.Views, c.PanelViews)
	}
	if len(c.Keybindings) != 1 || c.Keybindings[0].Key != "alt+k" || c.Keybindings[0].Command != AddSelectionCommand {
		t.Errorf("keybindings = %+v", c.Keybindings)
	}
	menu := false
	for _, item := range c.Menus {
		menu = menu || (item.Menu == extension.MenuEditorContext && item.Command == AddSelectionCommand)
	}
	if !menu {
		t.Errorf("Add Selection to Agent is not in the context menu of the editor: %+v", c.Menus)
	}
}

func TestEveryIconTheExtensionUsesExists(t *testing.T) {
	c := New().Manifest().Contributes
	names := []string{agentIcon, "key", "add", "edit", "trash", "comment-discussion", "error", "info", "check", "loading", "lightbulb",
		"chevron-right", "chevron-down", "warning", "stop-circle", "send"}
	for _, a := range c.ViewActions {
		names = append(names, a.Icon)
	}
	for _, k := range c.EditorKinds {
		names = append(names, k.Icon)
	}
	for _, p := range c.PanelViews {
		names = append(names, p.Icon)
	}
	for _, v := range c.ViewContainers {
		names = append(names, v.Icon)
	}
	for _, a := range c.EditorActions {
		names = append(names, a.Icon)
	}
	for _, tool := range []string{"Read", "Edit", "Write", "Bash", "Grep", "WebFetch", "Task", "TodoWrite", "Other"} {
		names = append(names, toolIcon(tool))
	}
	for _, n := range names {
		if _, ok := ui.CodiconCode(n); !ok {
			t.Errorf("there is no Codicon named %q", n)
		}
	}
}

// TestTheNamingFollowsTheTermsOfAnthropic checks SPEC §10.16 "Naming": no part of the extension is named "Claude Code"; the
// plain text of the tooltip of the title action and of the description of the extension may say that the adapter runs it, and
// the adapter is "Claude CLI" in the menus.
func TestTheNamingFollowsTheTermsOfAnthropic(t *testing.T) {
	m := New().Manifest()
	c := m.Contributes
	var names []string
	names = append(names, m.ID, m.Name)
	for _, cmd := range c.Commands {
		names = append(names, cmd.ID, cmd.Title, cmd.Category)
	}
	for _, v := range c.ViewContainers {
		names = append(names, v.ID, v.Title)
	}
	for _, v := range c.Views {
		names = append(names, v.ID, v.Name)
	}
	for _, v := range c.PanelViews {
		names = append(names, v.ID, v.Title)
	}
	for _, k := range c.EditorKinds {
		names = append(names, k.ID, k.Name)
	}
	names = append(names, PathSetting, AttachSetting)
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), "claude code") || strings.Contains(strings.ToLower(n), "anthropic") {
			t.Errorf("%q names Claude Code or Anthropic", n)
		}
	}
	if !strings.Contains(m.Description, "runs Claude Code") {
		t.Errorf("the description %q should say that the adapter runs Claude Code", m.Description)
	}
	var newSession string
	for _, cmd := range c.Commands {
		if cmd.ID == NewSessionCommand {
			newSession = cmd.Title
		}
	}
	if !strings.Contains(newSession, "Claude CLI") {
		t.Errorf("the adapter is called %q in the menus, want Claude CLI", newSession)
	}
}

func TestANewSessionStreamsIntoTheTab(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	tab := r.open()
	if tab.phase != phaseStarting {
		t.Errorf("a new session starts its program at once, the phase is %v", tab.phase)
	}
	r.ready(tab)
	starts := r.started()
	if len(starts) != 1 {
		t.Fatalf("the program was started %d times", len(starts))
	}
	cwd, _ := filepath.EvalSymlinks(r.folder)
	gotCWD, _ := filepath.EvalSymlinks(starts[0].CWD)
	if starts[0].value("--session-id") != tab.id || starts[0].has("--resume") || gotCWD != cwd || starts[0].value("--permission-mode") != "default" {
		t.Errorf("the command line is %v in %s", starts[0].Argv, starts[0].CWD)
	}
	if len(tab.commands) != 3 || len(tab.models) != 2 || tab.models[0].Name != "Sonnet" {
		t.Errorf("the handshake gave commands %+v and models %+v", tab.commands, tab.models)
	}

	r.send(tab, "What is main.go?")
	if !tab.busy || !r.api.EditorTabs()[0].Busy() {
		t.Error("the tab should be busy while the turn runs")
	}
	r.idle(tab)
	want := []string{
		"user:What is main.go?", "thinking:Let me look at the file.", "text:Hello, **world**.", "tool:Read:/work/main.go", "text:It is a Go file.",
	}
	if got := texts(tab); !slices.Equal(got, want) {
		t.Errorf("the conversation is\n%q\nwant\n%q", got, want)
	}
	tool := tab.tr.items[3]
	if !tool.Done || tool.Failed || tool.Result != "package main\n" {
		t.Errorf("the tool call is %+v", tool)
	}
	if tab.tr.items[0].ID == "" {
		t.Error("the message of the user did not get the UUID the program echoes")
	}
	if r.api.EditorTabs()[0].Busy() || len(tab.tr.live()) != 0 {
		t.Error("the turn is over: no busy mark and no partial block")
	}
	if got := r.api.EditorTabs()[0].Title(); got != "What is main.go?" {
		t.Errorf("the title of the tab is %q", got)
	}
	if e := r.ext.entry(tab.id); e == nil || e.Title != "What is main.go?" || e.Updated == 0 {
		t.Errorf("the index has %+v", e)
	}

	// a second message in the same program
	r.send(tab, "And again?")
	r.idle(tab)
	if got := texts(tab); got[len(got)-1] != "text:Second answer." {
		t.Errorf("the conversation ends with %q", got[len(got)-1])
	}
	if len(r.started()) != 1 {
		t.Error("the second message started another program")
	}
}

func TestPartialTextShowsWhileItStreamsAndTheEventsOfAFrameBuildOnce(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	fake := r.api.EditorTabs()[0]
	deltas := []string{
		`{"type":"stream_event","uuid":"e1","session_id":"s","parent_tool_use_id":null,"event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","uuid":"e2","session_id":"s","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}}`,
		`{"type":"stream_event","uuid":"e3","session_id":"s","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}}`,
	}
	before := fake.Refreshs
	for _, d := range deltas {
		tab.push(tab.gen, mustParse(t, d))
	}
	r.api.Pump()
	if fake.Refreshs != before+1 {
		t.Errorf("three events of a frame built the tab %d times, want 1", fake.Refreshs-before)
	}
	live := tab.tr.live()
	if len(live) != 1 || live[0].Kind != kindText || live[0].Text != "Hello" {
		t.Fatalf("the live blocks are %+v", live)
	}
	// the whole message replaces the partial block
	tab.push(tab.gen, mustParse(t, `{"type":"assistant","uuid":"a1","session_id":"s","parent_tool_use_id":null,"message":{"id":"m","model":"m","content":[{"type":"text","text":"Hello there."}]}}`))
	r.api.Pump()
	if len(tab.tr.live()) != 0 || !slices.Equal(texts(tab), []string{"text:Hello there."}) {
		t.Errorf("after the whole message: live %+v, items %q", tab.tr.live(), texts(tab))
	}
	// events of a program that was replaced are dropped
	tab.push(tab.gen-1, mustParse(t, `{"type":"assistant","uuid":"a2","session_id":"s","parent_tool_use_id":null,"message":{"id":"m2","model":"m","content":[{"type":"text","text":"old"}]}}`))
	r.api.Pump()
	if len(tab.tr.items) != 1 {
		t.Errorf("an event of an old program reached the conversation: %q", texts(tab))
	}
}

func TestToolCallsAreRowsWithTheirResults(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.tr.folder = "/work"
	for _, line := range []string{
		`{"type":"assistant","uuid":"a1","session_id":"s","parent_tool_use_id":null,"message":{"id":"m","model":"m","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./...\nsecond line"}},{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/work/main.go"}},{"type":"tool_use","id":"t3","name":"Grep","input":{"pattern":"func main","path":"/work/util"}}]}}`,
		`{"type":"assistant","uuid":"a2","session_id":"s","parent_tool_use_id":"t1","message":{"id":"m","model":"m","content":[{"type":"text","text":"hidden"},{"type":"tool_use","id":"t4","name":"Read","input":{"file_path":"/work/a.go"}}]}}`,
		`{"type":"user","uuid":"u1","session_id":"s","parent_tool_use_id":null,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"ok"},{"type":"text","text":"done"}]},{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"no such file"}]}}`,
		`{"type":"assistant","uuid":"a3","session_id":"s","parent_tool_use_id":null,"message":{"id":"m","model":"m","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"again"}}]}}`,
	} {
		tab.push(tab.gen, mustParse(t, line))
	}
	r.api.Pump()
	want := []string{"tool:Bash:go test ./... ...", "tool:Edit:main.go", "tool:Grep:func main in util", "tool:Read:a.go"}
	if got := texts(tab); !slices.Equal(got, want) {
		t.Fatalf("the rows are\n%q\nwant\n%q", got, want)
	}
	items := tab.tr.items
	if !items[0].Done || items[0].Result != "ok\ndone" || !items[1].Done || !items[1].Failed || items[1].Result != "no such file" || items[2].Done {
		t.Errorf("results: %+v %+v %+v", items[0], items[1], items[2])
	}
	if items[3].Depth != 1 {
		t.Errorf("the tool call of a sub-agent should be one level in: %+v", items[3])
	}
}

func TestAResultThatFailedShowsWhy(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.turns, tab.busy = 2, true
	tab.push(tab.gen, mustParse(t, `{"type":"result","subtype":"error_max_turns","uuid":"r","session_id":"s","is_error":true,"errors":["Reached the maximum number of turns"]}`))
	r.api.Pump()
	if !tab.busy || tab.turns != 1 {
		t.Errorf("one of two turns ended: turns %d busy %v", tab.turns, tab.busy)
	}
	tab.push(tab.gen, mustParse(t, `{"type":"result","subtype":"error_during_execution","uuid":"r","session_id":"s","is_error":true}`))
	r.api.Pump()
	if tab.busy || tab.turns != 0 {
		t.Errorf("both turns ended: turns %d busy %v", tab.turns, tab.busy)
	}
	want := []string{"error:Reached the maximum number of turns", "error:The turn failed (error_during_execution)."}
	if got := texts(tab); !slices.Equal(got, want) {
		t.Errorf("the notices are %q", got)
	}
}

func TestAPermissionRequestIsDeniedWithAMessageUntilStep133(t *testing.T) {
	r := newRig(t, "permission.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "remove the build")
	r.idle(tab)
	got := texts(tab)
	if len(got) != 3 || !strings.Contains(got[1], "Emetos can't answer this permission request yet") || !strings.Contains(got[1], "Bash") || got[2] != "text:I could not run that." {
		t.Fatalf("the conversation is %q", got)
	}
	log, err := os.ReadFile(r.log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), `"behavior":"deny"`) || !strings.Contains(string(log), "Emetos can't answer this permission request yet") {
		t.Errorf("the program was not told that the request is denied:\n%s", log)
	}
}

func TestADeadProgramIsShownAndTheNextMessageStartsAgain(t *testing.T) {
	r := newRig(t, "crash.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "hello")
	r.api.WaitFor("the program to exit", func() bool { return tab.sess == nil && tab.phase == phaseIdle })
	got := texts(tab)
	if len(got) != 2 || !strings.HasPrefix(got[1], "error:The program ended unexpectedly") || !strings.Contains(got[1], "out of memory") {
		t.Fatalf("the conversation is %q", got)
	}
	if tab.busy || tab.turns != 0 {
		t.Error("a dead program leaves the tab busy")
	}
	// the message was written, so the session exists: the next start resumes it
	if !tab.used {
		t.Fatal("the session should count as used")
	}
	r.send(tab, "again")
	r.api.WaitFor("the second program", func() bool { return len(r.started()) == 2 })
	if got := r.started()[1].value("--resume"); got != tab.id {
		t.Errorf("the second program was started with --resume %q, want %q", got, tab.id)
	}
}

func TestStopInterruptsTheTurn(t *testing.T) {
	r := newRig(t, "stop.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "do a long thing")
	r.api.WaitFor("the partial text", func() bool {
		live := tab.tr.live()
		return len(live) == 1 && live[0].Text == "Working on it"
	})
	if !tab.busy {
		t.Fatal("the turn should run")
	}
	tab.stop()
	if !tab.interrupting {
		t.Error("Stop marks the interrupt")
	}
	r.idle(tab)
	got := texts(tab)
	if got[len(got)-1] != "notice:Stopped." {
		t.Errorf("the conversation ends with %q, want the notice that the turn was stopped", got[len(got)-1])
	}
	if tab.interrupting || len(tab.tr.live()) != 0 {
		t.Error("after the result the partial text is gone and the interrupt is over")
	}
	// Stop with nothing running does nothing
	tab.stop()
	if tab.interrupting {
		t.Error("Stop with no turn must not mark an interrupt")
	}
}

func TestThePickersSwitchTheModelAndTheModeInTheRunningProgram(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	tab := r.open()
	r.ready(tab)
	tab.chooseModel("opus")
	tab.chooseMode(claude.ModePlan)
	tab.chooseMode(claude.PermissionMode("bypassPermissions")) // not offered
	if tab.model != "opus" || tab.mode != claude.ModePlan {
		t.Errorf("model %q mode %q", tab.model, tab.mode)
	}
	r.api.WaitFor("both control requests", func() bool {
		log, _ := os.ReadFile(r.log)
		return strings.Contains(string(log), `"subtype":"set_model"`) && strings.Contains(string(log), `"subtype":"set_permission_mode"`)
	})
	log, _ := os.ReadFile(r.log)
	if !strings.Contains(string(log), `"model":"opus"`) || !strings.Contains(string(log), `"mode":"plan"`) || strings.Contains(string(log), "bypassPermissions") {
		t.Errorf("the control requests are\n%s", log)
	}
	// the choice is saved with the tab, and the next program starts with it
	var st tabState
	if err := json.Unmarshal(r.api.EditorTabs()[0].State(), &st); err != nil || st.Model != "opus" || st.Mode != "plan" {
		t.Errorf("the saved state is %+v (%v)", st, err)
	}
}

func TestTheSessionTabDoesNotStartAProgramWithoutAFolder(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	r.ext.api = noFolderAPI{r.api}
	tab := r.open()
	if tab.phase != phaseFailed || !strings.Contains(tab.problem, "Open a folder first") {
		t.Errorf("phase %v problem %q", tab.phase, tab.problem)
	}
	if len(r.started()) != 0 {
		t.Error("a program was started without a folder")
	}
}

// noFolderAPI is the API of a scope that has no folder.
type noFolderAPI struct{ extension.API }

func (noFolderAPI) Scope() extension.ScopeInfo {
	return extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "No Folder"}
}

func TestSessionIDsAreUUIDsAndAFailedRandomSourceIsReported(t *testing.T) {
	id, err := newUUID(rand.Reader)
	if err != nil || len(id) != 36 || id[14] != '4' {
		t.Errorf("id %q err %v", id, err)
	}
	_, err = newUUID(failingReader{})
	route(t, err, "newUUID.SessionIDFailed")
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("RandomFailed", "no randomness") }

func TestTheStateOfATabMustHaveASession(t *testing.T) {
	r := newRig(t, "")
	for name, raw := range map[string]string{"not JSON": `{`, "no session": `{"used":true}`} {
		_, err := r.ext.createTab(&fakeapi.Tab{}, []byte(raw))
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		route(t, err, "createTab.SessionTabStateInvalid")
	}
	r.openIdle("11111111-2222-4333-8444-555555555555")
	_, err := r.api.Editors().Open(KindID, tabState{Session: "11111111-2222-4333-8444-555555555555"}, extension.EditorOpenOptions{})
	route(t, err, "createTab.SessionTabDuplicate")
}
