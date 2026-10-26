package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The permission requests (step 13.3), with the fake program asking.

// response returns the body of the answer the fake program got to the request with the id (the object with the "behavior"),
// or nil when it got none.
func (r *rig) response(id string) map[string]any {
	r.t.Helper()
	f, err := os.Open(r.log)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		var rec struct {
			Kind string `json:"kind"`
			Msg  struct {
				Type     string `json:"type"`
				Response struct {
					RequestID string         `json:"request_id"`
					Response  map[string]any `json:"response"`
				} `json:"response"`
			} `json:"msg"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Kind == "in" && rec.Msg.Type == "control_response" && rec.Msg.Response.RequestID == id {
			return rec.Msg.Response.Response
		}
	}
	return nil
}

// waitResponse waits until the fake program has got the answer to a request.
func (r *rig) waitResponse(id string) map[string]any {
	r.t.Helper()
	var got map[string]any
	r.api.WaitFor("the answer to "+id, func() bool { got = r.response(id); return got != nil })
	return got
}

// noResponse checks that the program got no answer to a request.
func (r *rig) noResponse(id string) {
	r.t.Helper()
	time.Sleep(50 * time.Millisecond)
	r.api.Pump()
	if got := r.response(id); got != nil {
		r.t.Errorf("the program got the answer %v to %s", got, id)
	}
}

// asked starts a turn with a script that asks and waits until the request is the one the tab asks about.
func (r *rig) asked(script, message string) *sessionTab {
	r.t.Helper()
	tab := r.open()
	r.ready(tab)
	r.send(tab, message)
	r.api.WaitFor("the request", func() bool { return tab.current() != nil })
	return tab
}

// proposal waits for the n-th proposed change the extension asked for.
func (r *rig) proposal(n int) *fakeapi.Proposal {
	r.t.Helper()
	var p *fakeapi.Proposal
	r.api.WaitFor("a proposed change", func() bool {
		all := r.api.Proposals()
		if len(all) >= n {
			p = all[n-1]
		}
		return p != nil
	})
	return p
}

// fileText is the text of a file of the folder.
func (r *rig) fileText(name string) string {
	r.t.Helper()
	data, err := os.ReadFile(r.folder + "/" + name)
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

const mainGo = "package main\n\nfunc main() {}\n"

func TestAnEditRequestOpensTheDiffAndAcceptAnswersAllow(t *testing.T) {
	r := newRig(t, "edit.jsonl")
	tab := r.asked("edit.jsonl", "add a print")
	p := r.proposal(1)
	want := extension.ProposedChange{
		URI: "file://" + r.folder + "/main.go", Original: mainGo, Title: "main.go (proposed)",
		Proposed: "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n",
	}
	if p.Change != want {
		t.Fatalf("the diff was asked for %+v, want %+v", p.Change, want)
	}
	if cur := tab.current(); cur == nil || cur.kind != permEdit || !tab.waiting() {
		t.Fatalf("the tab does not wait on the edit: %+v", cur)
	}
	p.Accept(want.Proposed)
	got := r.waitResponse("perm_edit")
	if got["behavior"] != "allow" {
		t.Fatalf("the answer is %v", got)
	}
	// unchanged text: the input of the request goes back as it came
	input, _ := got["updatedInput"].(map[string]any)
	if input["file_path"] != "main.go" || input["old_string"] != "func main() {}" || !strings.Contains(input["new_string"].(string), "println") {
		t.Errorf("updatedInput = %v", input)
	}
	r.idle(tab)
	if got := r.fileText("main.go"); got != mainGo {
		t.Errorf("Emetos wrote the file: %q", got)
	}
	if tab.waiting() || tab.current() != nil {
		t.Error("the tab still waits")
	}
	if !slices.Contains(texts(tab), "notice:Accepted Edit main.go.") {
		t.Errorf("the conversation is %q", texts(tab))
	}
}

func TestAnEditedAcceptSendsUpdatedInputThatGivesTheUsersText(t *testing.T) {
	r := newRig(t, "edit.jsonl")
	tab := r.asked("edit.jsonl", "add a print")
	p := r.proposal(1)
	final := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n\t// edited\n}\n"
	p.Accept(final)
	got := r.waitResponse("perm_edit")
	if got["behavior"] != "allow" {
		t.Fatalf("the answer is %v", got)
	}
	input, _ := got["updatedInput"].(map[string]any)
	old, repl := input["old_string"].(string), input["new_string"].(string)
	if input["file_path"] != "main.go" || strings.Count(mainGo, old) != 1 {
		t.Fatalf("updatedInput = %v", input)
	}
	if applied := strings.Replace(mainGo, old, repl, 1); applied != final {
		t.Errorf("applying updatedInput gives %q, want %q", applied, final)
	}
	r.idle(tab)
	if !slices.Contains(texts(tab), "notice:Accepted Edit main.go with changes.") {
		t.Errorf("the conversation is %q", texts(tab))
	}
}

func TestRejectingOrClosingTheDiffAnswersDeny(t *testing.T) {
	tests := []struct {
		name    string
		typed   string
		answer  func(p *fakeapi.Proposal)
		message string
	}{
		{"reject", "", (*fakeapi.Proposal).Reject, "The user rejected the proposed edit."},
		{"reject with a reason typed in the session", "use fmt.Println", (*fakeapi.Proposal).Reject, "The user rejected the proposed edit. use fmt.Println"},
		{"close the tab", "", (*fakeapi.Proposal).Close, "The user rejected the proposed edit."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "edit.jsonl")
			tab := r.asked("edit.jsonl", "add a print")
			p := r.proposal(1)
			tab.current().message.SetValue(tc.typed)
			tc.answer(p)
			got := r.waitResponse("perm_edit")
			if got["behavior"] != "deny" || got["message"] != tc.message {
				t.Errorf("the answer is %v, want a denial with %q", got, tc.message)
			}
			r.idle(tab)
			if r.fileText("main.go") != mainGo {
				t.Error("the file changed")
			}
			if !slices.Contains(texts(tab), "notice:Rejected Edit main.go.") {
				t.Errorf("the conversation is %q", texts(tab))
			}
		})
	}
}

func TestAWriteOpensTheDiffOfANewFileAgainstNothing(t *testing.T) {
	r := newRig(t, "write.jsonl")
	tab := r.asked("write.jsonl", "write notes")
	p := r.proposal(1)
	if p.Change.Original != "" || p.Change.Proposed != "first\nsecond\n" || p.Change.URI != "file://"+r.folder+"/notes/new.txt" {
		t.Errorf("the diff was asked for %+v", p.Change)
	}
	p.Accept("first\nsecond\nthird\n")
	got := r.waitResponse("perm_write")
	input, _ := got["updatedInput"].(map[string]any)
	if got["behavior"] != "allow" || input["content"] != "first\nsecond\nthird\n" || input["file_path"] != "notes/new.txt" {
		t.Errorf("the answer is %v", got)
	}
	r.idle(tab)
}

func TestAnEditThatCannotBeAppliedIsDeniedWithTheReason(t *testing.T) {
	r := newRig(t, "edit_missing.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "change it")
	got := r.waitResponse("perm_edit")
	msg, _ := got["message"].(string)
	if got["behavior"] != "deny" || !strings.HasPrefix(msg, "The edit cannot be applied:") || !strings.Contains(msg, "old_string is not in the file") {
		t.Errorf("the answer is %v", got)
	}
	r.idle(tab)
	if len(r.api.Proposals()) != 0 {
		t.Error("a diff was opened for an edit that cannot be applied")
	}
	if tab.waiting() {
		t.Error("the tab waits on a request that was answered")
	}
}

func TestAnEditTheDiffCannotShowBecomesTheDialogOfTheTool(t *testing.T) {
	r := newRig(t, "edit_denied.jsonl")
	tab := r.asked("edit_denied.jsonl", "change the secrets") // .env is a file a deny rule excludes: it is never read into a diff
	p := tab.current()
	r.api.WaitFor("the dialog", func() bool { return p.kind == permTool })
	if !strings.Contains(p.note, "cannot show this change as a diff") || !strings.Contains(p.note, "read deny rule") {
		t.Errorf("note = %q", p.note)
	}
	if len(r.api.Proposals()) != 0 {
		t.Error("a diff was opened for a file a deny rule excludes")
	}
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 600})
	if !s.ts.Exists(gluttertest.ByText("Allow once")) {
		t.Error("the dialog of the tool has no Allow once")
	}
	tab.allowOnce(p)
	if got := r.waitResponse("perm_edit"); got["behavior"] != "allow" {
		t.Errorf("the answer is %v", got)
	}
	r.idle(tab)
}

func TestTheBashDialogShowsTheCommandAndAnswersOnceSessionAndDeny(t *testing.T) {
	tests := []struct {
		name   string
		button string
		typed  string
		check  func(t *testing.T, got map[string]any)
		notice string
	}{
		{"allow once", "Allow once", "", func(t *testing.T, got map[string]any) {
			input, _ := got["updatedInput"].(map[string]any)
			if got["behavior"] != "allow" || input["command"] != "rm -rf build\nls" || got["updatedPermissions"] != nil {
				t.Errorf("the answer is %v", got)
			}
		}, "notice:Allowed Bash rm -rf build ... ."},
		{"allow for this session", "Allow for this session", "", func(t *testing.T, got map[string]any) {
			updates, _ := got["updatedPermissions"].([]any)
			if got["behavior"] != "allow" || len(updates) != 1 {
				t.Fatalf("the answer is %v", got)
			}
			u := updates[0].(map[string]any)
			// the suggestion goes back with the destination changed to this session
			if u["destination"] != "session" || u["behavior"] != "allow" || u["type"] != "addRules" || u["rules"] == nil {
				t.Errorf("the rule change is %v", u)
			}
		}, "notice:Allowed Bash rm -rf build ...  for this session."},
		{"deny with a message", "Deny", "use trash instead", func(t *testing.T, got map[string]any) {
			if got["behavior"] != "deny" || got["message"] != "The user denied this tool use. use trash instead" {
				t.Errorf("the answer is %v", got)
			}
		}, "notice:Denied Bash rm -rf build ... ."},
		{"deny without a message", "Deny", "", func(t *testing.T, got map[string]any) {
			if got["behavior"] != "deny" || got["message"] != "The user denied this tool use." {
				t.Errorf("the answer is %v", got)
			}
		}, "notice:Denied Bash rm -rf build ... ."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "bash.jsonl")
			tab := r.asked("bash.jsonl", "remove the build")
			s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 700})
			// the dialog names the command, why the program asks and the three answers
			for _, text := range []string{"Run this command?", "Remove the build folder", "Not in the allowed commands", "Allow once", "Allow for this session", "Deny"} {
				if !s.ts.Exists(gluttertest.ByText(text)) {
					t.Errorf("the dialog does not show %q", text)
				}
			}
			if !s.ts.Exists(gluttertest.ByTextContaining("rm -rf build")) {
				t.Error("the dialog does not show the command")
			}
			tab.current().message.SetValue(tc.typed)
			s.ts.Tap(gluttertest.ByText(tc.button))
			tc.check(t, r.waitResponse("perm_bash"))
			r.idle(tab)
			if !slices.Contains(texts(tab), strings.Replace(tc.notice, " ... ", " ...", 1)) && !slices.Contains(texts(tab), tc.notice) {
				t.Errorf("the conversation is %q, want %q", texts(tab), tc.notice)
			}
		})
	}
}

func TestADialogWithoutSuggestionsHasNoAllowForThisSession(t *testing.T) {
	r := newRig(t, "fetch.jsonl")
	tab := r.asked("fetch.jsonl", "fetch it")
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 600})
	if !s.ts.Exists(gluttertest.ByText("The agent wants to use WebFetch")) || !s.ts.Exists(gluttertest.ByTextContaining("https://example.com/docs")) {
		t.Error("the dialog does not name the tool and the address")
	}
	if s.ts.Exists(gluttertest.ByText("Allow for this session")) {
		t.Error("a request with no suggestions offers Allow for this session")
	}
	s.ts.Tap(gluttertest.ByText("Allow once"))
	if got := r.waitResponse("perm_fetch"); got["behavior"] != "allow" {
		t.Errorf("the answer is %v", got)
	}
	r.idle(tab)
}

func TestAPlanIsApprovedOrKept(t *testing.T) {
	tests := []struct {
		name   string
		button string
		typed  string
		check  func(t *testing.T, tab *sessionTab, got map[string]any)
	}{
		{"approve", "Approve", "", func(t *testing.T, tab *sessionTab, got map[string]any) {
			if got["behavior"] != "allow" {
				t.Errorf("the answer is %v", got)
			}
			if tab.mode != defaultMode {
				t.Errorf("the mode after the plan is %q", tab.mode)
			}
		}},
		{"keep planning", "Keep Planning", "also cover util", func(t *testing.T, tab *sessionTab, got map[string]any) {
			if got["behavior"] != "deny" || got["message"] != "The user wants to keep planning; the plan is not approved. also cover util" {
				t.Errorf("the answer is %v", got)
			}
			if tab.mode != claude.ModePlan {
				t.Errorf("the mode changed to %q", tab.mode)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "plan.jsonl")
			tab := r.open()
			r.ready(tab)
			tab.chooseMode(claude.ModePlan)
			r.send(tab, "plan it")
			r.api.WaitFor("the plan", func() bool { return tab.current() != nil })
			if tab.current().kind != permPlan || !strings.Contains(tab.current().plan, "Add a test") {
				t.Fatalf("the request is %+v", tab.current())
			}
			s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 700})
			if !s.ts.Exists(gluttertest.ByText("The agent has a plan")) || !s.ts.Exists(gluttertest.ByText("Approve")) || !s.ts.Exists(gluttertest.ByText("Keep Planning")) {
				t.Error("the panel of the plan is not complete")
			}
			tab.current().message.SetValue(tc.typed)
			s.ts.Tap(gluttertest.ByText(tc.button))
			got := r.waitResponse("perm_plan")
			tc.check(t, tab, got)
			r.idle(tab)
		})
	}
}

func TestAQuestionIsAnsweredWithTheChosenOptionsAndATypedAnswer(t *testing.T) {
	r := newRig(t, "question.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "set it up")
	r.api.WaitFor("the question", func() bool { return tab.current() != nil })
	p := tab.current()
	if p.kind != permQuestion || len(p.questions) != 2 || !p.questions[1].multi || len(p.questions[0].options) != 2 {
		t.Fatalf("the request is %+v", p)
	}
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 760})
	for _, text := range []string{"The agent has a question", "Which framework?", "Gin", "Fast and small", "Which checks?", "Answer", "Skip"} {
		if !s.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("the panel does not show %q", text)
		}
	}
	if p.answered() {
		t.Fatal("the questions count as answered before the user chose")
	}
	tab.submitAnswers(p) // does nothing until every question has an answer
	r.noResponse("perm_question")
	s.ts.Tap(gluttertest.ByText("Echo"))
	s.ts.Tap(gluttertest.ByText("Gin")) // a single choice keeps one
	s.ts.Tap(gluttertest.ByText("vet"))
	s.ts.Tap(gluttertest.ByText("race"))
	p.questions[1].other.SetValue("staticcheck")
	if !p.answered() {
		t.Fatal("the questions are not answered")
	}
	tab.submitAnswers(p)
	got := r.waitResponse("perm_question")
	input, _ := got["updatedInput"].(map[string]any)
	answers, _ := input["answers"].(map[string]any)
	if got["behavior"] != "allow" || answers["Which framework?"] != "Gin" || answers["Which checks?"] != "vet, race, staticcheck" || input["questions"] == nil {
		t.Errorf("the answer is %v", got)
	}
	r.idle(tab)
}

func TestSkippingTheQuestionsDenies(t *testing.T) {
	r := newRig(t, "question.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "set it up")
	r.api.WaitFor("the question", func() bool { return tab.current() != nil })
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 760})
	s.ts.Tap(gluttertest.ByText("Skip"))
	got := r.waitResponse("perm_question")
	if got["behavior"] != "deny" || !strings.Contains(got["message"].(string), "chose not to answer") {
		t.Errorf("the answer is %v", got)
	}
	r.idle(tab)
}

func TestSeveralRequestsAreAskedOneAfterTheOther(t *testing.T) {
	r := newRig(t, "queue.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "build and fetch")
	r.api.WaitFor("two requests", func() bool { return len(tab.perms) == 2 })
	first, second := tab.perms[0], tab.perms[1]
	if first.req.ToolName != "Bash" || second.req.ToolName != "WebFetch" || !first.started || second.started {
		t.Fatalf("the queue is %s, %s (started %v, %v)", first.req.ToolName, second.req.ToolName, first.started, second.started)
	}
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 600})
	if !s.ts.Exists(gluttertest.ByText("1 more waiting")) || !s.ts.Exists(gluttertest.ByTextContaining("make build")) {
		t.Error("the dialog does not show the first request and that one waits behind it")
	}
	r.noResponse("perm_b")
	s.ts.Tap(gluttertest.ByText("Allow once"))
	r.waitResponse("perm_a")
	if tab.current() != second {
		t.Fatal("the second request is not the one asked now")
	}
	s.sync()
	if s.ts.Exists(gluttertest.ByText("1 more waiting")) || !s.ts.Exists(gluttertest.ByTextContaining("https://example.com")) {
		t.Error("the dialog did not move to the second request")
	}
	s.ts.Tap(gluttertest.ByText("Deny"))
	if got := r.waitResponse("perm_b"); got["behavior"] != "deny" {
		t.Errorf("the second answer is %v", got)
	}
	r.idle(tab)
}

func TestARequestTheProgramCancelsLeavesTheTabAndAnswersNothing(t *testing.T) {
	r := newRig(t, "cancel.jsonl")
	tab := r.asked("cancel.jsonl", "sleep")
	r.send(tab, "never mind") // makes the script cancel the request
	r.api.WaitFor("the request to go", func() bool { return tab.current() == nil })
	r.idle(tab)
	r.noResponse("perm_c")
	if !slices.Contains(texts(tab), "notice:The request to use Bash was withdrawn.") {
		t.Errorf("the conversation is %q", texts(tab))
	}
}

func TestACancelledEditClosesItsDiff(t *testing.T) {
	r := newRig(t, "cancel_edit.jsonl")
	tab := r.asked("cancel_edit.jsonl", "edit")
	p := r.proposal(1)
	r.send(tab, "never mind")
	r.api.WaitFor("the request to go", func() bool { return tab.current() == nil && p.Cancelled() })
	r.idle(tab)
	r.noResponse("perm_e")
}

func TestClosingTheTabWithdrawsTheRequestsAndEndsTheProgram(t *testing.T) {
	r := newRig(t, "edit.jsonl")
	tab := r.asked("edit.jsonl", "edit")
	p := r.proposal(1)
	tab.tab.Close()
	r.api.WaitFor("the diff to be cancelled", p.Cancelled)
	r.ext.wait()
	r.noProgramLeft()
	if len(tab.perms) != 0 {
		t.Error("the closed tab still holds requests")
	}
	r.noResponse("perm_edit")
}

func TestCanUseToolReportsAWithdrawnRequestWithItsRoute(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tab.canUseTool(ctx, tab.gen, claude.ToolRequest{ToolName: "Bash", Input: json.RawMessage(`{"command":"ls"}`)})
	route(t, err, "sessionTab.canUseTool.PermissionWithdrawn")
	r.api.Pump()
	if len(tab.perms) != 0 {
		t.Error("a withdrawn request is in the queue")
	}
}

func TestARequestOfAnEndedProgramIsDenied(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	p := &permission{kind: permTool, req: claude.ToolRequest{ToolName: "Bash"}, reply: make(chan claude.Decision, 1)}
	tab.addPermission(tab.gen-1, p)
	select {
	case d := <-p.reply:
		if d.Allow {
			t.Error("the request of an old program was allowed")
		}
	default:
		t.Error("no answer")
	}
	if len(tab.perms) != 0 {
		t.Error("the request is in the queue")
	}
}

func TestATabThatWaitsHasTheMarkTheStatusItemAndTheNotification(t *testing.T) {
	r := newRig(t, "edit.jsonl")
	tab := r.asked("edit.jsonl", "edit")
	fake := r.api.EditorTabs()[0]
	if !fake.Dirty() {
		t.Error("the tab that waits has no mark")
	}
	item := r.api.StatusItem(StatusID)
	if item == nil || item.Hidden || !strings.Contains(item.Text, "1 waiting") || !strings.Contains(item.Tooltip, ": waiting for you") {
		t.Fatalf("the status item is %+v", item)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "waiting for your permission to use Edit") || !slices.Equal(notes[0].Actions, []string{"Show"}) {
		t.Fatalf("the notifications are %+v", notes)
	}
	r.api.RunNoteAction(0, "Show")
	if !fake.Revealed {
		t.Error("Show did not reveal the tab")
	}
	fake.Revealed = false
	item.OnClick()
	if !fake.Revealed {
		t.Error("a click on the status item did not go to the session")
	}
	r.proposal(1).Reject()
	r.waitResponse("perm_edit")
	r.idle(tab)
	r.api.WaitFor("the status item to hide", func() bool { return r.api.StatusItem(StatusID).Hidden })
	if fake.Dirty() {
		t.Error("the tab keeps its mark")
	}
	if notes := r.api.Notes(); len(notes) != 2 || !strings.Contains(notes[1].Message, "has finished") || notes[1].Severity != "info" {
		t.Errorf("the notifications are %+v", notes)
	}
}

func TestATabOnScreenRaisesNoNotification(t *testing.T) {
	r := newRig(t, "edit.jsonl")
	tab := r.open()
	r.ready(tab)
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 500})
	if !tab.visible {
		t.Fatal("a tab on screen is not marked as visible")
	}
	r.send(tab, "edit")
	r.api.WaitFor("the request", func() bool { return tab.current() != nil })
	r.proposal(1).Reject()
	r.waitResponse("perm_edit")
	r.idle(tab)
	if notes := r.api.Notes(); len(notes) != 0 {
		t.Errorf("a tab on screen raised %+v", notes)
	}
	// a tab that leaves the screen is hidden again
	s.ts.Pump()
	s.ts.PumpWidget(widgets.NewSizedBox(nil))
	if tab.visible {
		t.Error("a tab that left the screen is still visible")
	}
}

func TestTheStatusItemCountsTheSessionsThatWork(t *testing.T) {
	r := newRig(t, "")
	a := r.openIdle("")
	b := r.openIdle("")
	a.busy = true
	a.changed()
	b.busy = true
	b.changed()
	item := r.api.StatusItem(StatusID)
	if item.Hidden || !strings.Contains(item.Text, "2 working") || strings.Contains(item.Text, "waiting") {
		t.Fatalf("the status item is %+v", item)
	}
	b.perms = []*permission{{kind: permTool, req: claude.ToolRequest{ToolName: "Bash"}}}
	b.changed()
	item = r.api.StatusItem(StatusID)
	if !strings.Contains(item.Text, "1 waiting") || !strings.Contains(item.Text, "1 working") {
		t.Errorf("the status item is %+v", item)
	}
	item.OnClick() // goes to the one that waits
	if !r.api.EditorTabs()[1].Revealed || r.api.EditorTabs()[0].Revealed {
		t.Error("the click did not go to the session that waits")
	}
	b.perms = nil
	a.busy, b.busy = false, false
	a.changed()
	b.changed()
	if !r.api.StatusItem(StatusID).Hidden {
		t.Error("the status item stays when nothing works")
	}
}

func TestTheOutputChannelCarriesTheStandardErrorAndTheRequests(t *testing.T) {
	r := newRig(t, "stderr.jsonl")
	tab := r.open()
	r.ready(tab)
	r.send(tab, "go")
	r.idle(tab)
	r.api.WaitFor("the line of standard error", func() bool {
		return slices.ContainsFunc(r.api.Channel(OutputChannel).Lines(), func(l string) bool { return strings.HasSuffix(l, "stderr: something went wrong") })
	})
	r2 := newRig(t, "bash.jsonl")
	tab2 := r2.asked("bash.jsonl", "remove")
	tab2.allowOnce(tab2.current())
	r2.idle(tab2)
	lines := r2.api.Channel(OutputChannel).Lines()
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "Bash asks for permission") }) || !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "allowed Bash") }) {
		t.Errorf("the channel has %q", lines)
	}
}

func TestTheLogOfTheAdapterGoesToTheChannelToo(t *testing.T) {
	r := newRig(t, "")
	r.ext.adapter().Logger.Warn("something is odd", "session", "abc", "n", 3)
	lines := r.api.Channel(OutputChannel).Lines()
	if len(lines) != 1 || lines[0] != `warn: something is odd session="abc" n="3"` {
		t.Errorf("the channel has %q", lines)
	}
	l := r.ext.logger().With("a", 1).WithGroup("g")
	l.Debug("deep", "b", 2)
	lines = r.api.Channel(OutputChannel).Lines()
	if len(lines) != 2 || lines[1] != `debug: deep a="1" g.b="2"` {
		t.Errorf("the channel has %q", lines)
	}
}

func TestReplayShowsTheRequestsOfARecordingAsARealProgramAsksThem(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	fixture := filepath.Join(t.TempDir(), "asks.jsonl")
	if err := os.WriteFile(fixture, []byte(strings.Join([]string{
		`{"type":"user","uuid":"u1","session_id":"s","parent_tool_use_id":null,"isReplay":true,"message":{"role":"user","content":"change main"}}`,
		`{"type":"control_request","request_id":"p1","request":{"subtype":"can_use_tool","tool_name":"Edit","input":{"file_path":"main.go","old_string":"func main() {}","new_string":"func main() {\n}"}}}`,
		`{"$":"ask","id":"p2","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test ./..."}}}`,
		`{"$":"wait","for":"user"}`,
		`{"type":"control_request","request_id":"p3","request":{"subtype":"hook_callback"}}`,
	}, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(ReplayCommand, map[string]any{"fixture": fixture}); err != nil {
		t.Fatal(err)
	}
	if !tab.replay || len(tab.perms) != 2 || tab.perms[0].req.ToolName != "Edit" || tab.perms[1].req.ToolName != "Bash" {
		t.Fatalf("the tab holds %d requests", len(tab.perms))
	}
	// the edit is a diff, as for a real program
	p := r.proposal(1)
	if p.Change.Original != mainGo || !strings.HasSuffix(p.Change.URI, "/main.go") {
		t.Errorf("the diff was asked for %+v", p.Change)
	}
	if r.api.StatusItem(StatusID).Hidden {
		t.Error("a recording that waits for the user is not in the status bar")
	}
	if len(r.api.Notes()) != 0 {
		t.Errorf("a recording raised %+v", r.api.Notes())
	}
	p.Accept(p.Change.Proposed)
	r.api.WaitFor("the dialog of the second request", func() bool { return tab.current() != nil && tab.current().req.ToolName == "Bash" })
	tab.deny(tab.current())
	if len(tab.perms) != 0 || tab.waiting() {
		t.Error("the answers did not clear the queue")
	}
	// the answers go nowhere: no program was started
	if len(r.started()) != 0 {
		t.Error("a recording started a program")
	}
	if got := texts(tab); !slices.Contains(got, "notice:Accepted Edit main.go.") || !slices.Contains(got, "notice:Denied Bash go test ./....") {
		t.Errorf("the conversation is %q", got)
	}
}
