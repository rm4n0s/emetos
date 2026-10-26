package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// proofFixture is the recorded conversation of the proof of step 13.2.
const proofFixture = "../../testdata/proof/13.2/session.jsonl"

// shown is a session tab on screen.
type shown struct {
	t       *testing.T
	ts      *gluttertest.Tester
	tab     *sessionTab
	rebuild func()
}

// show puts the tab on screen in the theme and size, as the box of the editor area does.
func show(t *testing.T, tab *sessionTab, th uitest.Theme, size gui.Size) *shown {
	t.Helper()
	s := &shown{t: t, tab: tab}
	host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		s.rebuild = func() { set(func() {}) }
		return tab.build(bc)
	})
	s.ts = uitest.Pump(t, th, size, host)
	return s
}

// sync builds the tab again, as the host does after Refresh.
func (s *shown) sync() {
	s.t.Helper()
	s.rebuild()
	s.ts.Pump()
}

// replayed is a session tab that played the recorded conversation.
func replayed(t *testing.T) (*rig, *sessionTab) {
	t.Helper()
	r := newRig(t, "")
	tab := r.openIdle("5f0c2b1e-8a54-4d0e-9c1b-2d7a6b3e9f10")
	data, err := os.ReadFile(proofFixture)
	if err != nil {
		t.Fatal(err)
	}
	events, err := parseFixture(data)
	if err != nil {
		t.Fatal(err)
	}
	tab.replayEvents(events)
	return r, tab
}

func TestGoldenTheSessionTab(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			_, tab := replayed(t)
			// one of the rows is unfolded to show its result
			tab.tr.items[3].open = true
			s := show(t, tab, th, gui.Size{Width: 760, Height: 800})
			uitest.MatchesGolden(s.ts, "session_"+th.Name)
		})
	}
}

func TestGoldenTheSessionTabWhileItWorks(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.push(tab.gen, mustParse(t, `{"type":"user","uuid":"u","session_id":"s","parent_tool_use_id":null,"isReplay":true,"message":{"role":"user","content":"Find the bug in util.go"}}`))
	tab.replay = true
	for _, line := range []string{
		`{"type":"assistant","uuid":"a1","session_id":"s","parent_tool_use_id":null,"message":{"id":"m","model":"m","content":[{"type":"tool_use","id":"t1","name":"Grep","input":{"pattern":"func Parse"}}]}}`,
		`{"type":"stream_event","uuid":"e1","session_id":"s","parent_tool_use_id":null,"event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","uuid":"e2","session_id":"s","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"I found two candidates. The first one is in ` + "`util.go`" + `"}}}`,
	} {
		tab.push(tab.gen, mustParse(t, line))
	}
	r.api.Pump()
	tab.turns, tab.busy = 1, true
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 420})
	uitest.MatchesGolden(s.ts, "session_working")
}

func TestGoldenTheBanners(t *testing.T) {
	for name, phase := range map[string]phase{"missing": phaseMissing, "loggedout": phaseLoggedOut, "failed": phaseFailed} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, "")
			tab := r.openIdle("")
			tab.phase, tab.problem = phase, "The program could not be started: exit status 2"
			s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 300})
			uitest.MatchesGolden(s.ts, "banner_"+name)
		})
	}
}

func TestGoldenTheCompletionList(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.commands = []commandInfo{{Name: "compact", Description: "Summarize the conversation", Hint: "[instructions]"}, {Name: "review", Description: "Review the changes"}, {Name: "init", Description: "Write a CLAUDE.md"}}
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 300})
	s.ts.EnterText(gluttertest.ByType[*ui.InputBox](), "/")
	s.sync()
	uitest.MatchesGolden(s.ts, "completion_commands")
}

func TestTheInputSendsOnEnterAndMakesANewLineOnShiftEnter(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	tab := r.open()
	r.ready(tab)
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 400})
	input := gluttertest.ByType[*ui.InputBox]()
	s.ts.EnterText(input, "first line")
	s.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShift)
	s.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	s.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShift)
	if len(tab.tr.items) != 0 {
		t.Fatalf("Shift+Enter sent the message: %q", texts(tab))
	}
	s.ts.EnterText(input, "first line\nsecond line")
	if got := tab.input.Value(); got != "first line\nsecond line" {
		t.Fatalf("the input is %q", got)
	}
	s.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	if got := texts(tab); len(got) != 1 || got[0] != "user:first line\nsecond line" {
		t.Fatalf("Enter did not send the message: %q", got)
	}
	if tab.input.Value() != "" || !tab.busy {
		t.Errorf("after sending: input %q busy %v", tab.input.Value(), tab.busy)
	}
	r.idle(tab)
	s.sync()
	// the Stop button shows only while a turn runs
	if len(s.ts.Elements(gluttertest.ByText("Stop"))) != 0 {
		t.Error("the Stop button should be gone after the turn")
	}
}

func TestSlashAndAtOpenTheCompletionListAndTheKeysChooseFromIt(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.commands = []commandInfo{{Name: "compact", Description: "Summarize"}, {Name: "review", Description: "Review"}, {Name: "init", Description: "Write"}}
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 360})
	input := gluttertest.ByType[*ui.InputBox]()

	s.ts.EnterText(input, "/re")
	s.sync()
	if len(tab.comp.items) != 1 || tab.comp.items[0].Label != "/review" {
		t.Fatalf("the list for /re is %+v", tab.comp.items)
	}
	if len(s.ts.Elements(gluttertest.ByText("/review"))) != 1 {
		t.Error("the list is not on screen")
	}
	// Tab takes the command
	s.ts.SendKeyEvent(services.LogicalKeyboardKeyTab)
	s.sync()
	if tab.input.Value() != "/review " || len(tab.comp.items) != 0 {
		t.Fatalf("after Tab: input %q list %+v", tab.input.Value(), tab.comp.items)
	}

	// @ lists files; the arrows move, Enter takes
	s.ts.EnterText(input, "see @u")
	s.sync()
	if len(tab.comp.items) != 2 || tab.comp.items[0].Insert != "@util/util.go " {
		t.Fatalf("the list for @u is %+v", tab.comp.items)
	}
	s.ts.SendKeyEvent(services.LogicalKeyboardKeyArrowDown)
	s.sync()
	if tab.comp.sel != 1 {
		t.Fatalf("selected = %d", tab.comp.sel)
	}
	s.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	s.sync()
	if tab.input.Value() != "see @util/util_test.go " || len(tab.turns2()) != 0 {
		t.Fatalf("after Enter: input %q", tab.input.Value())
	}
	if len(tab.tr.items) != 0 {
		t.Errorf("Enter on the list sent the message: %q", texts(tab))
	}

	// Escape closes the list and leaves the text
	s.ts.EnterText(input, "see @m")
	s.sync()
	if len(tab.comp.items) == 0 {
		t.Fatal("no list for @m")
	}
	s.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	s.sync()
	if len(tab.comp.items) != 0 || tab.input.Value() != "see @m" {
		t.Errorf("after Escape: input %q list %+v", tab.input.Value(), tab.comp.items)
	}
}

// turns2 is the pending messages of the tab, for the test above.
func (t *sessionTab) turns2() []string { return t.pending }

func TestStopShowsWhileATurnRunsAndInterruptsIt(t *testing.T) {
	r := newRig(t, "stop.jsonl")
	tab := r.open()
	r.ready(tab)
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 400})
	r.send(tab, "do a long thing")
	s.sync()
	stop := gluttertest.ByText("Stop")
	if len(s.ts.Elements(stop)) != 1 {
		t.Fatal("no Stop button while the turn runs")
	}
	r.api.WaitFor("the partial text", func() bool { return len(tab.tr.live()) == 1 })
	s.sync()
	s.ts.Tap(stop)
	r.idle(tab)
	s.sync()
	if got := texts(tab); got[len(got)-1] != "notice:Stopped." {
		t.Errorf("the conversation ends with %q", got[len(got)-1])
	}
	if len(s.ts.Elements(gluttertest.ByText("Stop"))) != 0 {
		t.Error("the Stop button should be gone")
	}
}

func TestAConversationThatFollowsItsEndKeepsFollowingAndOneScrolledUpStays(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	for i := range 60 {
		tab.tr.add(&item{Kind: kindText, Text: "paragraph " + strings.Repeat("word ", 20) + string(rune('a'+i%26))})
	}
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 400})
	s.ts.Pump()
	s.ts.Pump()
	_, top := tab.scroll.Position()
	_, max := tab.scroll.MaxPosition()
	if max <= 0 || top < max-1 {
		t.Fatalf("a new conversation shows its end: top %v max %v", top, max)
	}
	// a message arrives while the user is at the end: the end is followed
	tab.tr.add(&item{Kind: kindText, Text: "one more"})
	tab.stick = tab.atEnd()
	s.sync()
	s.ts.Pump()
	_, top = tab.scroll.Position()
	_, max = tab.scroll.MaxPosition()
	if top < max-1 {
		t.Errorf("the end was not followed: top %v max %v", top, max)
	}
	// the user scrolls up: the next message does not move the view
	tab.scroll.ScrollTo(0, 0)
	tab.stick = tab.atEnd()
	if tab.stick {
		t.Fatal("scrolled to the top, the tab still follows the end")
	}
	tab.tr.add(&item{Kind: kindText, Text: "and another"})
	s.sync()
	s.ts.Pump()
	if _, top = tab.scroll.Position(); top != 0 {
		t.Errorf("the view moved to %v while the user reads the top", top)
	}
	_ = filepath.Join
}

// asking is a tab that shows a conversation and the panel of a permission request of a recording.
func asking(t *testing.T, requests ...string) (*rig, *sessionTab) {
	t.Helper()
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.replay = true
	tab.tr.add(&item{Kind: kindUser, Text: "Make the program say hello, then run the tests."})
	tab.tr.add(&item{Kind: kindText, Text: "I will change `main.go` first."})
	for _, line := range requests {
		req, ok, err := claude.ParseToolRequest([]byte(line))
		if err != nil || !ok {
			t.Fatalf("request %s: %v %v", line, ok, err)
		}
		tab.replayRequest(req)
	}
	return r, tab
}

func TestGoldenThePermissionPanels(t *testing.T) {
	bash := `{"type":"control_request","request_id":"p","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test -race ./...\ngo vet ./..."},"title":"Run this command?","description":"Run the tests and vet the code","decision_reason":"Not in the allowed commands","permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash"}],"behavior":"allow","destination":"localSettings"}]}}`
	plan := `{"type":"control_request","request_id":"p","request":{"subtype":"can_use_tool","tool_name":"ExitPlanMode","input":{"plan":"## Plan\n\n1. Print the greeting from ` + "`main`" + `\n2. Add a test that captures the output\n3. Run ` + "`go test`" + `\n"}}}`
	question := `{"type":"control_request","request_id":"p","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which greeting?","header":"Greeting","multiSelect":false,"options":[{"label":"Hello, world","description":"The classic"},{"label":"Hi there","description":"Shorter"}]}]}}}`
	edit := `{"type":"control_request","request_id":"p","request":{"subtype":"can_use_tool","tool_name":"Edit","input":{"file_path":"main.go","old_string":"func main() {}","new_string":"func main() {}"}}}`
	for _, tc := range []struct {
		name, request string
		themes        []uitest.Theme
		size          gui.Size
	}{
		{"permission_bash", bash, uitest.Themes(), gui.Size{Width: 760, Height: 520}},
		{"permission_plan", plan, uitest.Themes()[:1], gui.Size{Width: 760, Height: 620}},
		{"permission_question", question, uitest.Themes()[:1], gui.Size{Width: 760, Height: 560}},
		{"permission_edit", edit, uitest.Themes()[:1], gui.Size{Width: 760, Height: 480}},
	} {
		for _, th := range tc.themes {
			t.Run(tc.name+"_"+th.Name, func(t *testing.T) {
				_, tab := asking(t, tc.request)
				s := show(t, tab, th, tc.size)
				uitest.MatchesGolden(s.ts, tc.name+"_"+th.Name)
			})
		}
	}
}

func TestGoldenTheRewindButton(t *testing.T) {
	r := newRig(t, "rewind.jsonl")
	tab, _ := r.answered("Make the program say hello")
	tab.tr.add(&item{Kind: kindText, Text: "Done: the program says hello."})
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 360})
	uitest.MatchesGolden(s.ts, "rewind_button")
	tab.askRewind(tab.tr.items[0])
	s.sync()
	uitest.MatchesGolden(s.ts, "rewind_question")
}
