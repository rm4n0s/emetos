package debug

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap"
)

func lines(bps []Breakpoint) []string {
	var out []string
	for _, b := range bps {
		out = append(out, baseName(b.Path)+":"+itoa(b.Line)+":"+itoa(b.Column))
	}
	return out
}

func TestModelBreakpointsAreSortedAndDeduplicated(t *testing.T) {
	m := NewModel()
	m.addBreakpoints("/p/b.go", []BreakpointData{{Line: 9}, {Line: 3}, {Line: 3}, {Line: 3, Column: 5}, {Line: 3, Column: 2}})
	m.addBreakpoints("/p/a.go", []BreakpointData{{Line: 7}})
	got := lines(m.Breakpoints(BreakpointFilter{}))
	want := []string{"a.go:7:0", "b.go:3:0", "b.go:3:2", "b.go:3:5", "b.go:9:0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("breakpoints = %v, want %v", got, want)
	}
}

func TestModelFilters(t *testing.T) {
	m := NewModel()
	added := m.addBreakpoints("/p/a.go", []BreakpointData{{Line: 1}, {Line: 2, Disabled: true}, {Line: 3, Column: 4}, {Line: 4, TriggeredBy: "x"}})
	m.addBreakpoints("/p/b.go", []BreakpointData{{Line: 1}})
	tests := []struct {
		name string
		f    BreakpointFilter
		want []string
	}{
		{"all", BreakpointFilter{}, []string{"a.go:1:0", "a.go:2:0", "a.go:3:4", "a.go:4:0", "b.go:1:0"}},
		{"by path", BreakpointFilter{Path: "/p/b.go"}, []string{"b.go:1:0"}},
		{"by original path", BreakpointFilter{OriginalPath: "/p/a.go"}, []string{"a.go:1:0", "a.go:2:0", "a.go:3:4", "a.go:4:0"}},
		{"by line", BreakpointFilter{Line: 1}, []string{"a.go:1:0", "b.go:1:0"}},
		{"by column", BreakpointFilter{Column: 4}, []string{"a.go:3:4"}},
		{"enabled only", BreakpointFilter{EnabledOnly: true, OriginalPath: "/p/a.go"}, []string{"a.go:1:0", "a.go:3:4", "a.go:4:0"}},
		{"triggered only", BreakpointFilter{TriggeredOnly: true}, []string{"a.go:4:0"}},
	}
	for _, tt := range tests {
		if got := lines(m.Breakpoints(tt.f)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	// A breakpoint that waits for another and has not been asked yet is pending.
	if bp, ok := m.Breakpoint(added[3].ID); !ok || !bp.Pending || bp.TriggeredBy != "x" {
		t.Errorf("triggered = %+v %v", bp, ok)
	}
	// Not activated: nothing is enabled.
	m.setActivated(false)
	if got := m.Breakpoints(BreakpointFilter{EnabledOnly: true}); len(got) != 0 {
		t.Errorf("enabled while not activated: %v", lines(got))
	}
	// Enabling one activates all.
	m.setEnablement(added[1].ID, true)
	if !m.BreakpointsActivated() {
		t.Error("enabling a breakpoint did not activate breakpoints")
	}
}

func TestModelUpdateMovesAndMergesBreakpoints(t *testing.T) {
	m := NewModel()
	b := m.addBreakpoints("/p/a.go", []BreakpointData{{Line: 3}, {Line: 5}, {Line: 8, Column: 2}})
	var events []BreakpointsChange
	m.Subscribe(func(ev Event) {
		if ev.Kind == EventBreakpoints {
			events = append(events, ev.Breakpoints)
		}
	})
	changed := m.updateBreakpoints(map[string]BreakpointUpdate{
		b[0].ID: {Line: 5}, // lands on the other: one of them goes
		b[2].ID: {Line: 9, Column: ip(7), Condition: sp("x > 1"), HitCondition: sp("3"), LogMessage: sp("hi"), Mode: sp("m"), ModeLabel: sp("M")},
		"nope":  {Line: 1},
	})
	got := m.Breakpoints(BreakpointFilter{})
	if want := []string{"a.go:5:0", "a.go:9:7"}; !reflect.DeepEqual(lines(got), want) {
		t.Errorf("breakpoints = %v, want %v", lines(got), want)
	}
	if got[1].Condition != "x > 1" || got[1].HitCondition != "3" || got[1].LogMessage != "hi" || got[1].Mode != "m" || got[1].ModeLabel != "M" || !got[1].IsLogpoint() || !got[1].Inline() {
		t.Errorf("updated = %+v", got[1])
	}
	if len(changed) == 0 || len(events) != 1 || len(events[0].Removed) != 1 {
		t.Errorf("changed = %v, events = %+v", changed, events)
	}
	// Clearing the column and changing the trigger.
	m.updateBreakpoints(map[string]BreakpointUpdate{b[2].ID: {Column: ip(0), TriggeredBy: sp(b[1].ID)}})
	if bp, _ := m.Breakpoint(b[2].ID); bp.Column != 0 || bp.TriggeredBy != b[1].ID {
		t.Errorf("after clearing = %+v", bp)
	}
}

func TestModelSessionDataVerifiesAndMoves(t *testing.T) {
	m := NewModel()
	b := m.addBreakpoints("/p/a.go", []BreakpointData{{Line: 3, Condition: "c"}, {Line: 10, LogMessage: "x"}, {Line: 20, HitCondition: "2"}})
	caps := dap.Capabilities{SupportsConditionalBreakpoints: true, SupportsHitConditionalBreakpoints: false, SupportsLogPoints: true}
	// Session s1 verifies the first at line 4 (moved, a column added), rejects the second, and verifies the third.
	m.setBreakpointSessionData("s1", caps, map[string]dap.Breakpoint{
		b[0].ID: {ID: 11, Verified: true, Line: 4, Column: 2, EndLine: 4, EndColumn: 9, Source: &dap.Source{Path: "/p/other.go"}},
		b[1].ID: {ID: 12, Verified: false, Message: "no code here"},
		b[2].ID: {ID: 13, Verified: true},
	})
	got := m.Breakpoints(BreakpointFilter{})
	// The shown line is the adapter's; the requested line stays.
	if want := []string{"a.go:20:0", "a.go:10:0", "other.go:4:2"}; len(got) != 3 {
		t.Fatalf("breakpoints = %v, want %v", lines(got), want)
	}
	byID := map[string]Breakpoint{}
	for _, x := range got {
		byID[x.ID] = x
	}
	moved := byID[b[0].ID]
	if moved.Line != 4 || moved.Column != 2 || moved.RequestedLine != 3 || moved.RequestedColumn != 0 || moved.SourcePath != "/p/other.go" || moved.Path != "/p/a.go" || moved.EndLine != 4 || moved.EndColumn != 9 || !moved.Verified || !moved.Supported {
		t.Errorf("moved = %+v", moved)
	}
	if rejected := byID[b[1].ID]; rejected.Verified || rejected.Message != "no code here" || rejected.Line != 10 {
		t.Errorf("rejected = %+v", rejected)
	}
	if unsupported := byID[b[2].ID]; unsupported.Supported {
		t.Errorf("a hit condition the adapter cannot do is supported: %+v", unsupported)
	}
	// Filtering by where the breakpoint is shown and where it was set.
	if got := lines(m.Breakpoints(BreakpointFilter{Path: "/p/other.go"})); !reflect.DeepEqual(got, []string{"a.go:4:2"}) {
		t.Errorf("by shown path = %v", got)
	}
	if got := m.Breakpoints(BreakpointFilter{OriginalPath: "/p/a.go"}); len(got) != 3 {
		t.Errorf("by original path = %d", len(got))
	}
	// A file with unsaved changes unverifies its breakpoints, with a message.
	m.SetDocumentDirty("/p/a.go", true)
	if bp, _ := m.Breakpoint(b[2].ID); bp.Verified || bp.Message != unverifiedDirty {
		t.Errorf("dirty = %+v", bp)
	}
	m.SetDocumentDirty("/p/a.go", false)
	if bp, _ := m.Breakpoint(b[2].ID); !bp.Verified {
		t.Errorf("clean again = %+v", bp)
	}
	// The adapter id and the sessions that verified.
	m.mu.Lock()
	id, ok := m.lines[0].adapterID("s1")
	m.mu.Unlock()
	if !ok || id == 0 {
		t.Errorf("adapter id = %d %v", id, ok)
	}
	// The end of the session removes what it knew.
	m.setBreakpointSessionData("s1", caps, nil)
	for _, x := range m.Breakpoints(BreakpointFilter{}) {
		if !x.Verified && x.Message != "" || x.Line != x.RequestedLine {
			t.Errorf("after the session ended: %+v", x)
		}
	}
}

func TestModelSeveralSessionsDecideWhatIsShown(t *testing.T) {
	m := NewModel()
	b := m.addBreakpoints("/p/a.go", []BreakpointData{{Line: 3}})
	caps := dap.Capabilities{}
	m.setBreakpointSessionData("s1", caps, map[string]dap.Breakpoint{b[0].ID: {ID: 1, Verified: true, Line: 4}})
	if bp, _ := m.Breakpoint(b[0].ID); bp.Line != 4 {
		t.Errorf("one session verified: %+v", bp)
	}
	// Two sessions verify it at different places: the place the user set is shown.
	m.setBreakpointSessionData("s2", caps, map[string]dap.Breakpoint{b[0].ID: {ID: 2, Verified: true, Line: 6}})
	if bp, _ := m.Breakpoint(b[0].ID); bp.Line != 3 || !bp.Verified {
		t.Errorf("two sessions disagree: %+v", bp)
	}
	// Two sessions verify it at the same place.
	m.setBreakpointSessionData("s2", caps, map[string]dap.Breakpoint{b[0].ID: {ID: 2, Verified: true, Line: 4}})
	if bp, _ := m.Breakpoint(b[0].ID); bp.Line != 4 {
		t.Errorf("two sessions agree: %+v", bp)
	}
	m.mu.Lock()
	sessions := m.lines[0].sessionsThatVerified()
	m.mu.Unlock()
	if !reflect.DeepEqual(sessions, []string{"s1", "s2"}) {
		t.Errorf("sessions that verified = %v", sessions)
	}
	// Only unverified answers: the first one's message.
	m.setBreakpointSessionData("s1", caps, map[string]dap.Breakpoint{b[0].ID: {ID: 1, Message: "m1"}})
	m.setBreakpointSessionData("s2", caps, map[string]dap.Breakpoint{b[0].ID: {ID: 2, Message: "m2"}})
	if bp, _ := m.Breakpoint(b[0].ID); bp.Verified || bp.Message != "m1" {
		t.Errorf("unverified: %+v", bp)
	}
}

func TestModelEnableRemoveAndFunctionBreakpoints(t *testing.T) {
	m := NewModel()
	b := m.addBreakpoints("/p/a.go", []BreakpointData{{Line: 1}, {Line: 2}})
	f1 := m.addFunctionBreakpoint(FunctionBreakpointOptions{Name: "main"}, "")
	f2 := m.addFunctionBreakpoint(FunctionBreakpointOptions{Name: "init", Condition: "x", Disabled: true}, "fixed-id")
	if f2.ID != "fixed-id" || f2.Enabled || f2.Condition != "x" {
		t.Errorf("function breakpoint = %+v", f2)
	}
	m.enableAll(false)
	for _, x := range m.Breakpoints(BreakpointFilter{}) {
		if x.Enabled {
			t.Errorf("still enabled: %+v", x)
		}
	}
	for _, x := range m.FunctionBreakpoints() {
		if x.Enabled {
			t.Errorf("function still enabled: %+v", x)
		}
	}
	m.setActivated(false)
	m.enableAll(true)
	if !m.BreakpointsActivated() {
		t.Error("enable all did not activate")
	}
	if !m.updateFunctionBreakpoint(f1.ID, sp("start"), sp("c"), sp("h")) {
		t.Error("function breakpoint not found")
	}
	if m.updateFunctionBreakpoint("nope", nil, nil, nil) {
		t.Error("an unknown function breakpoint was found")
	}
	fbs := m.FunctionBreakpoints()
	if fbs[0].Name != "start" || fbs[0].Condition != "c" || fbs[0].HitCondition != "h" {
		t.Errorf("updated = %+v", fbs[0])
	}
	m.removeFunctionBreakpoints(f1.ID)
	if got := m.FunctionBreakpoints(); len(got) != 1 || got[0].ID != "fixed-id" {
		t.Errorf("after removing one: %+v", got)
	}
	m.removeFunctionBreakpoints("")
	if len(m.FunctionBreakpoints()) != 0 {
		t.Error("remove all left function breakpoints")
	}
	removed := m.removeBreakpoints([]string{b[0].ID, "nope"})
	if len(removed) != 1 || removed[0].ID != b[0].ID || len(m.Breakpoints(BreakpointFilter{})) != 1 {
		t.Errorf("removed = %+v", removed)
	}
	// A function breakpoint answered by a session.
	f3 := m.addFunctionBreakpoint(FunctionBreakpointOptions{Name: "x"}, "")
	m.setBreakpointSessionData("s", dap.Capabilities{SupportsFunctionBreakpoints: false}, map[string]dap.Breakpoint{f3.ID: {Verified: false, Message: "no"}})
	if got := m.FunctionBreakpoints()[0]; got.Verified || got.Supported || got.Message != "no" {
		t.Errorf("function answer = %+v", got)
	}
}

func TestModelExceptionBreakpoints(t *testing.T) {
	m := NewModel()
	filters := []dap.ExceptionBreakpointsFilter{
		{Filter: "all", Label: "All Exceptions"},
		{Filter: "uncaught", Label: "Uncaught", Default: true, SupportsCondition: true, ConditionDescription: "expr"},
	}
	m.setExceptionBreakpointsForSession("s1", filters)
	got := m.ExceptionBreakpoints("s1")
	if len(got) != 2 || got[0].Enabled || !got[1].Enabled || !got[1].SupportsCondition {
		t.Fatalf("exception breakpoints = %+v", got)
	}
	// Nothing is shown for no session until one is the fallback.
	if len(m.ExceptionBreakpoints("")) != 0 {
		t.Error("filters shown without a session or a fallback")
	}
	m.setExceptionFallbackSession("s1")
	if len(m.ExceptionBreakpoints("")) != 2 {
		t.Error("the fallback filters are not shown")
	}
	// The choice survives a second session with the same filters, and a condition is kept.
	m.setEnablement(got[0].ID, true)
	m.setExceptionCondition(got[1].ID, "x == 1")
	m.removeExceptionBreakpointsForSession("s1")
	m.setExceptionBreakpointsForSession("s2", filters)
	again := m.ExceptionBreakpoints("s2")
	if len(again) != 2 || again[0].ID != got[0].ID || !again[0].Enabled || again[1].Condition != "x == 1" {
		t.Errorf("second session = %+v", again)
	}
	if len(m.ExceptionBreakpoints("s1")) != 0 {
		t.Error("the first session still has filters")
	}
	if m.setExceptionCondition("nope", "x") {
		t.Error("an unknown filter was found")
	}
	// A filter that differs (another label) is a different one.
	m.setExceptionBreakpointsForSession("s3", []dap.ExceptionBreakpointsFilter{{Filter: "all", Label: "Other"}})
	if len(m.ExceptionBreakpoints("s3")) != 1 || m.ExceptionBreakpoints("s3")[0].ID == got[0].ID {
		t.Error("a filter with another label was taken for the same")
	}
}

func TestModelWatchExpressions(t *testing.T) {
	m := NewModel()
	var kinds []EventKind
	m.Subscribe(func(ev Event) { kinds = append(kinds, ev.Kind) })
	a := m.AddWatchExpression("a")
	b := m.AddWatchExpression("b")
	c := m.AddWatchExpression("")
	if a.Value() != NotAvailable || c.Value() != "" {
		t.Errorf("values: %q %q", a.Value(), c.Value())
	}
	names := func() []string {
		var out []string
		for _, w := range m.WatchExpressions() {
			out = append(out, w.Name)
		}
		return out
	}
	m.RenameWatchExpression(c.ID(), "c")
	m.MoveWatchExpression(c.ID(), 0)
	if want := []string{"c", "a", "b"}; !reflect.DeepEqual(names(), want) {
		t.Errorf("names = %v, want %v", names(), want)
	}
	m.MoveWatchExpression(c.ID(), 99)
	m.MoveWatchExpression("nope", 0)
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(names(), want) {
		t.Errorf("names = %v, want %v", names(), want)
	}
	m.RenameWatchExpression("nope", "x")
	m.RemoveWatchExpressions(b.ID())
	if want := []string{"a", "c"}; !reflect.DeepEqual(names(), want) {
		t.Errorf("names = %v, want %v", names(), want)
	}
	m.RemoveWatchExpressions("")
	if len(names()) != 0 {
		t.Errorf("names = %v", names())
	}
	for _, k := range kinds {
		if k != EventWatch {
			t.Errorf("event kind %v", k)
		}
	}
}

func TestModelBreakpointModes(t *testing.T) {
	m := NewModel()
	m.registerBreakpointModes("go", []dap.BreakpointMode{{Mode: "hw", Label: "Hardware", AppliesTo: []string{"source"}}})
	m.registerBreakpointModes("lldb", []dap.BreakpointMode{
		{Mode: "hw", Label: "Hardware", AppliesTo: []string{"data"}},   // the same: kinds are added
		{Mode: "sw", Label: "Hardware", AppliesTo: []string{"source"}}, // the same label: both get the debug type
	})
	src := m.BreakpointModes("source")
	if len(src) != 2 || src[0].Label != "Hardware (go)" || src[1].Label != "Hardware (lldb)" {
		t.Errorf("source modes = %+v", src)
	}
	if data := m.BreakpointModes("data"); len(data) != 1 || data[0].Mode != "hw" {
		t.Errorf("data modes = %+v", data)
	}
	if len(m.BreakpointModes("exception")) != 0 {
		t.Error("modes for a kind that none applies to")
	}
}

func TestModelReplHistory(t *testing.T) {
	m := NewModel()
	m.AddReplHistory("a")
	m.AddReplHistory("a")
	m.AddReplHistory("  ")
	m.AddReplHistory("b")
	if got := m.ReplHistory(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("history = %v", got)
	}
	for i := 0; i < MaxReplHistory+20; i++ {
		m.AddReplHistory(itoa(i))
	}
	if h := m.ReplHistory(); len(h) != MaxReplHistory || h[len(h)-1] != itoa(MaxReplHistory+19) {
		t.Errorf("history has %d entries, last %q", len(h), h[len(h)-1])
	}
}

func TestDecorationColumnConversions(t *testing.T) {
	tests := []struct {
		line   string
		column int // UTF-16, from 1
		offset int // bytes
	}{
		{"abc", 1, 0}, {"abc", 3, 2}, {"abc", 9, 3},
		{"é€x", 2, 2}, {"é€x", 3, 5}, {"é€x", 4, 6},
		{"😀x", 1, 0}, {"😀x", 3, 4}, {"😀x", 4, 5},
	}
	for _, tt := range tests {
		if got := utf16ColumnToByte(tt.line, tt.column); got != tt.offset {
			t.Errorf("utf16ColumnToByte(%q, %d) = %d, want %d", tt.line, tt.column, got, tt.offset)
		}
	}
	// And back, for the offsets that start a character.
	for _, tt := range []struct {
		line   string
		offset int
		column int
	}{{"abc", 2, 3}, {"é€x", 5, 3}, {"😀x", 4, 3}, {"😀x", 5, 4}} {
		if got := byteToUTF16Column(tt.line, tt.offset); got != tt.column {
			t.Errorf("byteToUTF16Column(%q, %d) = %d, want %d", tt.line, tt.offset, got, tt.column)
		}
	}
}
