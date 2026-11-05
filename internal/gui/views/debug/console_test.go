package debug

import (
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	coredebug "github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

func (r *consoleRig) type_(text string) {
	r.t.Helper()
	r.ts.EnterText(gluttertest.ByKey(ConsoleInputKey), text)
	r.pump()
}

func (r *consoleRig) press(k services.LogicalKeyboardKey, mods ...services.LogicalKeyboardKey) {
	r.t.Helper()
	for _, m := range mods {
		r.ts.SendKeyDownEvent(m)
	}
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	for _, m := range slices.Backward(mods) {
		r.ts.SendKeyUpEvent(m)
	}
	r.pump()
}

func evalScript(expr, result string) daptest.Step {
	return daptest.ReplyArgs("evaluate", map[string]any{"expression": expr, "context": "repl"}, map[string]any{"result": result})
}

func TestConsoleEvaluatesWhatIsTyped(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps, evalScript("1 + 2", "3"))})
	r.type_("1 + 2")
	r.press(services.LogicalKeyboardKeyEnter)
	r.waitFor("the result", func() bool { return r.con.Len() >= 2 })
	if got := r.rowTexts(); !slices.Equal(got, []string{"1 + 2", "3"}) {
		t.Errorf("rows %q", got)
	}
	if v := r.con.Input().Value(); v != "" {
		t.Errorf("the input keeps %q", v)
	}
	if h := r.svc.Model().ReplHistory(); !slices.Equal(h, []string{"1 + 2"}) {
		t.Errorf("history %q", h)
	}
	if !r.ts.Exists(gluttertest.ByText("3", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the result is not drawn")
	}
}

func TestConsoleShiftEnterMakesALineAndEnterEvaluatesAll(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps, evalScript("a\nb", "ab"))})
	r.type_("a")
	r.press(services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyShift)
	if v := r.con.Input().Value(); !strings.Contains(v, "\n") || len(r.svc.Model().ReplHistory()) != 0 {
		t.Fatalf("Shift+Enter left %q", v)
	}
	r.con.Input().SetValue("a\nb")
	r.pump()
	r.press(services.LogicalKeyboardKeyEnter)
	r.waitFor("the result", func() bool { return r.con.Len() >= 2 })
	if got := r.rowTexts(); !slices.Equal(got, []string{"a\nb", "ab"}) {
		t.Errorf("rows %q", got)
	}
}

func TestConsoleHistoryWithUpAndDown(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps, evalScript("one", "1"), evalScript("two", "2"))})
	for _, e := range []string{"one", "two"} {
		r.type_(e)
		r.press(services.LogicalKeyboardKeyEnter)
		n := r.con.Len()
		r.waitFor("the result", func() bool { return r.con.Len() > n || r.con.Len() >= 2*(1+slices.Index([]string{"one", "two"}, e)) })
	}
	r.press(services.LogicalKeyboardKeyArrowUp)
	if v := r.con.Input().Value(); v != "two" {
		t.Errorf("Up shows %q", v)
	}
	r.press(services.LogicalKeyboardKeyArrowUp)
	if v := r.con.Input().Value(); v != "one" {
		t.Errorf("Up again shows %q", v)
	}
	r.press(services.LogicalKeyboardKeyArrowDown)
	if v := r.con.Input().Value(); v != "two" {
		t.Errorf("Down shows %q", v)
	}
	r.press(services.LogicalKeyboardKeyArrowDown)
	if v := r.con.Input().Value(); v != "" {
		t.Errorf("Down at the end shows %q", v)
	}
}

func TestConsoleCompletionsOfTheAdapter(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps, daptest.ReplyArgs("completions", map[string]any{"text": "ord", "column": 4, "line": 1},
		map[string]any{"targets": []map[string]any{
			{"label": "order", "type": "variable", "length": 3},
			{"label": "orders", "text": "orders", "detail": "[]Order", "type": "variable", "length": 3},
		}}))}) //nolint
	r.type_("ord")
	r.waitFor("the completions", func() bool { return r.con.sugg.open() })
	if !r.ts.Exists(gluttertest.ByKey(ConsoleSuggestKey)) {
		t.Fatal("the list of completions is not drawn")
	}
	if !r.ts.Exists(gluttertest.ByText("orders", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the second completion is not in the list")
	}
	r.press(services.LogicalKeyboardKeyArrowDown)
	r.press(services.LogicalKeyboardKeyTab)
	if v := r.con.Input().Value(); v != "orders" {
		t.Errorf("Tab on the second item left %q", v)
	}
	if r.con.sugg.open() {
		t.Error("the list stays open after Tab")
	}
}

func TestConsoleCompletionsEscapeAndHistory(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps,
		daptest.Reply("completions", map[string]any{"targets": []map[string]any{{"label": "len", "length": 1}}}))})
	r.svc.Model().AddReplHistory("len(items)")
	r.con.History().Add("len(items)")
	r.type_("l")
	r.waitFor("the completions", func() bool { return r.con.sugg.open() })
	if n := len(r.con.sugg.items); n != 2 || !r.con.sugg.items[1].history {
		t.Errorf("items %+v: want the adapter's and the line from the history", r.con.sugg.items)
	}
	r.press(services.LogicalKeyboardKeyEscape)
	if r.con.sugg.open() {
		t.Error("Escape leaves the list open")
	}
	if v := r.con.Input().Value(); v != "l" {
		t.Errorf("Escape changed the input to %q", v)
	}
}

func TestConsoleAcceptSuggestionOnEnter(t *testing.T) {
	cfg := DefaultConsoleConfig
	cfg.AcceptSuggestionOnEnter = true
	r := newConsoleRig(t, consoleRigOpts{cfg: &cfg, script: scriptOf(fullCaps,
		daptest.Reply("completions", map[string]any{"targets": []map[string]any{{"label": "total", "length": 2}}}))})
	r.type_("to")
	r.waitFor("the completions", func() bool { return r.con.sugg.open() })
	r.press(services.LogicalKeyboardKeyEnter)
	if v := r.con.Input().Value(); v != "total" {
		t.Errorf("Enter left %q, want the completion", v)
	}
}

func TestConsoleClear(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps, kickWith(
		out(map[string]any{"category": "stdout", "output": "one\n"}),
		out(map[string]any{"category": "stdout", "output": "two\n"}))...)})
	r.kick()
	r.waitRows(2)
	r.con.Clear()
	r.pump()
	if r.con.Len() != 0 || r.con.Session().Repl().Len() != 0 {
		t.Errorf("rows %d, elements %d after Clear", r.con.Len(), r.con.Session().Repl().Len())
	}
	if r.ts.Exists(gluttertest.ByText("one", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the cleared output is still drawn")
	}
}

func TestConsoleIsReadOnlyWithoutASession(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{})
	if !r.con.ReadOnly() {
		t.Fatal("a console with no session takes input")
	}
	if !r.ts.Exists(gluttertest.ByText("Please start a debug session to evaluate expressions", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("no placeholder")
	}
	r.con.Input().SetValue("x")
	r.con.Accept()
	if r.con.Input().Value() != "x" || len(r.svc.Model().ReplHistory()) != 0 {
		t.Error("evaluated with no session")
	}
}

func TestConsoleFilter(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps, kickWith(
		out(map[string]any{"category": "stdout", "output": "alpha one\n"}),
		out(map[string]any{"category": "stdout", "output": "beta two\n"}),
		out(map[string]any{"category": "stderr", "output": "alpha three\n"}),
		out(map[string]any{"category": "stdout", "output": "gamma four\n"}))...)})
	r.kick()
	r.waitRows(4)
	tests := []struct {
		filter string
		want   []string
	}{
		{"alpha", []string{"alpha one", "alpha three"}},
		{"ALPHA", []string{"alpha one", "alpha three"}},
		{"alpha, beta", []string{"alpha one", "beta two", "alpha three"}},
		{"!alpha", []string{"beta two", "gamma four"}},
		{"\\!alpha", nil},
		{"", []string{"alpha one", "beta two", "alpha three", "gamma four"}},
	}
	for _, tt := range tests {
		r.con.SetFilter(tt.filter)
		r.pump()
		var got []string
		for _, x := range r.rowTexts() {
			got = append(got, strings.TrimSuffix(x, "\n"))
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("filter %q shows %q, want %q", tt.filter, got, tt.want)
		}
	}
	if total, shown := r.con.Counts(); total != 4 || shown != 4 {
		t.Errorf("counts %d/%d", shown, total)
	}
}

func TestConsoleLinksOpenFilesAndAddresses(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{})
	tests := []struct {
		name string
		part kit.LinkPart
		want string
	}{
		{"absolute path with position", kit.LinkPart{Kind: kit.LinkPath, Value: "/work/src/loader.go:18:3", Path: "/work/src/loader.go", Line: 18, Column: 3}, "/work/src/loader.go:18:3"},
		{"address", kit.LinkPart{Kind: kit.LinkWeb, Value: "https://example.com/docs"}, "https://example.com/docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.opened = nil
			r.waitFor("the link to resolve", func() bool { return r.con.ResolveLink(tt.part) })
			if !r.con.ResolveLink(tt.part) {
				t.Fatal("the link does not resolve")
			}
			r.con.OpenLink(tt.part)
			r.waitFor("the link to open", func() bool { return len(r.opened) > 0 })
			if r.opened[0] != tt.want {
				t.Errorf("opened %q, want %q", r.opened[0], tt.want)
			}
		})
	}
	if r.con.ResolveLink(kit.LinkPart{Kind: kit.LinkPath, Value: "/work/src/nothing.go", Path: "/work/src/nothing.go"}) {
		r.pump()
	}
}

func TestConsoleCollapsesIdenticalLines(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps, kickWith(
		out(map[string]any{"category": "stdout", "output": "tick\n"}),
		out(map[string]any{"category": "stdout", "output": "tick\n"}),
		out(map[string]any{"category": "stdout", "output": "tick\n"}),
		out(map[string]any{"category": "stdout", "output": "tock\n"}))...)})
	r.kick()
	r.waitRows(2)
	if !r.ts.Exists(gluttertest.ByText("3", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the count of the repeated line is not drawn")
	}
}

// An output that has an object stands for the variables of the object (VS Code names them null and adds them as lines); the ones
// that have children open on their twistie.
func TestConsoleOpensTheObjectOfAnOutput(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps, append(kickWith(
		out(map[string]any{"category": "stdout", "output": "order", "variablesReference": 700})),
		daptest.ReplyArgs("variables", map[string]any{"variablesReference": 700}, map[string]any{"variables": []map[string]any{
			{"name": "Items", "value": "[]string len:1", "type": "[]string", "variablesReference": 701}}}),
		daptest.ReplyArgs("variables", map[string]any{"variablesReference": 701}, map[string]any{"variables": []map[string]any{
			{"name": "0", "value": "\"bread\"", "type": "string"}}}))...)})
	r.kick()
	r.waitRows(1)
	row := r.con.Rows()[0]
	if !row.Expandable || row.Expanded {
		t.Fatalf("the object row: %+v", row)
	}
	r.con.Toggle(row.Key)
	r.waitRows(2)
	if r.con.Rows()[1].Kind != ConsoleChild || !r.ts.Exists(gluttertest.ByTextContaining("bread", gluttertest.FindOpts{FindRichText: true})) {
		t.Errorf("rows %+v", r.con.Rows())
	}
	r.con.Toggle(row.Key)
	r.waitRows(1)
}

func TestConsoleHundredThousandLinesStayInTheFrameBudget(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps)})
	sess := r.con.Session()
	for i := range 100_000 {
		sess.AppendToRepl("line "+strconv.Itoa(i)+"\n", coredebug.ReplInfo)
	}
	r.waitFor("the lines", func() bool { return r.con.Len() >= 100_000 })
	start := time.Now()
	const frames = 20
	for range frames {
		r.ts.Pump(16 * time.Millisecond)
	}
	if per := time.Since(start) / frames; per > 16*time.Millisecond {
		t.Errorf("a frame with 100000 lines takes %v", per)
	}
	if !r.ts.Exists(gluttertest.ByText("line 99999", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the console does not show the end")
	}
	r.con.SetFilter("line 5")
	r.pump()
	if r.con.Len() == 0 {
		t.Error("the filter shows nothing")
	}
}

// TestConsoleJumpToTheTopOfHundredThousandLinesStaysInTheFrameBudget guards the fix that removed
// docs/GLUTTER-ISSUES.md's one entry: with Glutter's EstimateItemOffsets (list, consolebody.go), a
// big jump within a huge, reversed console builds one window of rows, not every row passed over
// (which used to cost over a minute for 100,000 lines).
func TestConsoleJumpToTheTopOfHundredThousandLinesStaysInTheFrameBudget(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps)})
	sess := r.con.Session()
	for i := range 100_000 {
		sess.AppendToRepl("line "+strconv.Itoa(i)+"\n", coredebug.ReplInfo)
	}
	rich := gluttertest.FindOpts{FindRichText: true}
	r.waitFor("the lines", func() bool { return r.con.Len() >= 100_000 })
	if !r.ts.Exists(gluttertest.ByText("line 99999", rich)) {
		t.Fatal("the console does not start at its end")
	}
	start := time.Now()
	// One big wheel tick, far past the top: a walk of every one of the 100,000 rows passed over would take well over a second.
	r.ts.ScrollAt(ui.Offset{Dx: 300, Dy: 200}, ui.Offset{Dy: -100_000_000})
	r.ts.Pump(16 * time.Millisecond)
	if d := time.Since(start); d > time.Second {
		t.Errorf("the jump to the top took %v, want it bounded by one window of rows, not every row passed over", d)
	}
	if !r.ts.Exists(gluttertest.ByText("line 0", rich)) {
		t.Error("the jump did not reach the top of the console")
	}
}

func TestConsoleFollowsTheEndAndGoesBackToTheTop(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps)})
	sess := r.con.Session()
	rich := gluttertest.FindOpts{FindRichText: true}
	sess.AppendToRepl("first\n", coredebug.ReplInfo)
	r.waitRows(1)
	if y := r.ts.TopLeft(gluttertest.ByText("first", rich)).Dy; y > 20 {
		t.Errorf("a console that fits starts at the top of the view, not at y=%v", y)
	}
	for i := range 200 {
		sess.AppendToRepl("line "+strconv.Itoa(i)+"\n", coredebug.ReplInfo)
	}
	r.waitRows(201)
	if !r.ts.Exists(gluttertest.ByText("line 199", rich)) || r.ts.Exists(gluttertest.ByText("first", rich)) {
		t.Fatal("a console that does not fit shows its end")
	}
	sess.AppendToRepl("last\n", coredebug.ReplInfo)
	r.waitRows(202)
	if !r.ts.Exists(gluttertest.ByText("last", rich)) {
		t.Error("the view does not follow new lines while it is at the end")
	}
	r.con.Clear()
	sess.AppendToRepl("again\n", coredebug.ReplInfo)
	r.waitRows(1)
	r.pump()
	if y := r.ts.TopLeft(gluttertest.ByText("again", rich)).Dy; y > 20 {
		t.Errorf("a console that fits again starts at the top of the view, not at y=%v", y)
	}
}

// uiFrame runs what was posted and a frame, and tells the processor time of the UI thread they took.
func (r *consoleRig) uiFrame() time.Duration {
	start := threadCPU(r.t)
	r.drain()
	r.ts.Pump(16 * time.Millisecond)
	return threadCPU(r.t) - start
}

func TestConsoleFilterOfHundredThousandLinesStaysInTheFrameBudget(t *testing.T) {
	// the budget is the UI thread's processor time (this goroutine is locked to its thread), not wall time; the race detector makes the
	// code about ten times slower
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	budget := 16 * time.Millisecond
	if raceEnabled {
		budget *= 10
	}
	const n = 100_000
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps), tune: func(o *ConsoleOpts) { o.FilterDelay = 200 * time.Millisecond }})
	sess := r.con.Session()
	for i := range n {
		sess.AppendToRepl("line "+strconv.Itoa(i)+"\n", coredebug.ReplInfo)
	}
	r.waitFor("the lines", func() bool { return r.con.Len() >= n })

	// warm up: the first frames are cold (first layout, first paint) and are not what is under test
	for range 5 {
		r.uiFrame()
	}

	// frames until the rows are the ones of a filter, each within the budget
	var worst time.Duration
	settle := func(want int) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for r.con.Len() != want {
			if time.Now().After(deadline) {
				t.Fatalf("the rows are %d, want %d", r.con.Len(), want)
			}
			worst = max(worst, r.uiFrame())
			time.Sleep(2 * time.Millisecond)
		}
		worst = max(worst, r.uiFrame())
	}
	expect := func(text string) int {
		f := coredebug.NewReplFilter(text)
		count := 0
		for _, e := range sess.Repl().Elements() {
			if f.Visible(e, true) {
				count++
			}
		}
		return count
	}

	// one pass of the filter work; the processor time of a thread still grows when the machine is busy, so a pass over the budget is
	// measured again, up to three times, and the best one counts: a real slowdown is over the budget every time
	pass := func() time.Duration {
		worst = 0
		base := r.con.Len()
		// typing "line 5", a key every 5 ms: nothing is matched on the UI thread, and the last text is the one that shows
		for _, text := range []string{"l", "li", "lin", "line", "line ", "line 5"} {
			r.con.SetFilter(text)
			for range 3 {
				worst = max(worst, r.uiFrame())
				time.Sleep(2 * time.Millisecond)
			}
			if r.con.Len() != base {
				t.Fatalf("typing %q: the rows changed to %d before the delay is over", text, r.con.Len())
			}
		}
		want := expect("line 5")
		settle(want)
		for _, row := range r.con.Rows() {
			if !strings.Contains(RowText(row), "5") {
				t.Fatalf("the row %q does not match the filter", RowText(row))
			}
		}
		// lines that arrive while the next filter is matched are filtered too
		r.con.SetFilter("line 7")
		r.drain()
		time.Sleep(220 * time.Millisecond) // the delay is over: the match is on its goroutine or done
		sess.AppendToRepl("line 7 late\n", coredebug.ReplInfo)
		sess.AppendToRepl("other late\n", coredebug.ReplInfo)
		want = expect("line 7") // the lines are all in the model now (AppendToRepl is synchronous)
		settle(want)
		if rows := r.rowTexts(); len(rows) == 0 || !slices.Contains(rows, "line 7 late") || slices.Contains(rows, "other late") {
			t.Errorf("the late lines are not filtered: %d rows", len(rows))
		}
		// a fast sequence of edits: only the last shows
		r.con.SetFilter("line 3")
		r.con.SetFilter("line 4")
		r.con.SetFilter("line 99999")
		settle(expect("line 99999"))
		if got := r.rowTexts(); len(got) != 1 || got[0] != "line 99999" {
			t.Errorf("rows of the last edit: %v", got)
		}
		r.con.SetFilter("")
		settle(base + 2)
		return worst
	}
	var best time.Duration
	for attempt := range 3 {
		w := pass()
		t.Logf("attempt %d: the longest frame while filtering %d lines: %v", attempt+1, n, w)
		if attempt == 0 || w < best {
			best = w
		}
		if best <= budget {
			break
		}
	}
	if best > budget {
		t.Errorf("a frame while filtering 100000 lines takes %v of the UI thread, over %v", best, budget)
	}
}
