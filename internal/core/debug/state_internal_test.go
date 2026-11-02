package debug

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap"
)

func stateService(t *testing.T) (*Service, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := NewService(ctx, Options{})
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	text := `{"configurations": [{"name": "A", "type": "go", "request": "launch"}, {"name": "B", "type": "go", "request": "launch"}]}`
	if err := os.WriteFile(LaunchFilePath(folder), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return svc, folder
}

func TestStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, folder := stateService(t)
	if err := svc.Launches().Reload(ctx, LaunchSources{Folder: folder}); err != nil {
		t.Fatal(err)
	}
	svc.Launches().Select(svc.Launches().Launch(folder), "B", SelectOptions{})

	m := svc.Model()
	trig := m.addBreakpoints("/p/a.go", []BreakpointData{{Line: 3, Condition: "x > 1", HitCondition: "> 2", LogMessage: "x={x}"}})
	m.addBreakpoints("/p/b.go", []BreakpointData{{Line: 9, Column: 4, Disabled: true}, {Line: 12, TriggeredBy: trig[0].ID, Mode: "m", ModeLabel: "Mode"}})
	m.addFunctionBreakpoint(FunctionBreakpointOptions{Name: "main.run", Condition: "c", HitCondition: "2"}, "")
	m.setExceptionBreakpointsForSession("s1", []dap.ExceptionBreakpointsFilter{{Filter: "all", Label: "All", SupportsCondition: true, Default: true}, {Filter: "uncaught", Label: "Uncaught"}})
	m.setExceptionCondition(m.exceptions[0].id, "e.ok")
	m.AddWatchExpression("a+b")
	m.AddWatchExpression("")
	m.setActivated(false)
	m.AddReplHistory("1+1")
	m.AddReplHistory("x")
	svc.inputs.Set("k1", "v1")
	svc.inputs.Set("k2", "v2")

	data, err := svc.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("not JSON: %s", data)
	}

	// A new window: the state first, then the launches are read.
	other, _ := stateService(t)
	if err := other.RestoreState(data); err != nil {
		t.Fatal(err)
	}
	if err := other.Launches().Reload(ctx, LaunchSources{Folder: folder}); err != nil {
		t.Fatal(err)
	}
	om := other.Model()
	if got, want := om.Breakpoints(BreakpointFilter{}), m.Breakpoints(BreakpointFilter{}); !reflect.DeepEqual(stripSession(got), stripSession(want)) {
		t.Errorf("breakpoints\n got %+v\nwant %+v", got, want)
	}
	if got, want := om.FunctionBreakpoints(), m.FunctionBreakpoints(); !reflect.DeepEqual(got, want) {
		t.Errorf("function breakpoints\n got %+v\nwant %+v", got, want)
	}
	if got, want := fallbackOff(om.ExceptionBreakpoints("")), fallbackOff(m.ExceptionBreakpoints("s1")); len(got) != 2 || !reflect.DeepEqual(got, want) {
		t.Errorf("exception breakpoints\n got %+v\nwant %+v", got, want)
	}
	if w := om.WatchExpressions(); len(w) != 1 || w[0].Name != "a+b" || w[0].ID() != m.WatchExpressions()[0].ID() {
		t.Errorf("watch expressions = %v", w)
	}
	if om.BreakpointsActivated() {
		t.Error("the breakpoints are activated again")
	}
	if got := om.ReplHistory(); !reflect.DeepEqual(got, []string{"1+1", "x"}) {
		t.Errorf("history = %v", got)
	}
	if v, ok := other.inputs.Get("k2"); !ok || v != "v2" {
		t.Errorf("inputs = %v", other.inputs.entries())
	}
	if sel := other.Launches().Selected(); sel.Name != "B" || sel.Launch == nil || sel.Launch.Folder != folder {
		t.Errorf("selected = %+v", sel)
	}
}

// stripSession drops what only a running session knows.
func stripSession(bps []Breakpoint) []Breakpoint {
	out := append([]Breakpoint(nil), bps...)
	for i := range out {
		out[i].Verified, out[i].Message = false, ""
	}
	return out
}

func TestRestoreStateFailsAndKeepsTheModel(t *testing.T) {
	svc, _ := stateService(t)
	svc.Model().addBreakpoints("/p/a.go", []BreakpointData{{Line: 1}})
	tests := []struct {
		name  string
		data  string
		route string
	}{
		{"not JSON", `{"version": 1,`, "Service.RestoreState.DebugStateInvalid"},
		{"the wrong shape", `{"version": "one"}`, "Service.RestoreState.DebugStateInvalid"},
		{"another version", `{"version": 2}`, "Service.RestoreState.DebugStateUnsupported"},
		{"no version", `{}`, "Service.RestoreState.DebugStateUnsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routeOf(t, svc.RestoreState([]byte(tt.data)), tt.route)
			if n := len(svc.Model().Breakpoints(BreakpointFilter{})); n != 1 {
				t.Errorf("breakpoints = %d, the model changed", n)
			}
		})
	}
	if err := svc.RestoreState(nil); err != nil {
		t.Errorf("no state: %v", err)
	}
}

func TestRestoreStateDropsWhatCannotBeUsed(t *testing.T) {
	svc, _ := stateService(t)
	var many []string
	for i := 0; i < MaxStateBreakpoints+5; i++ {
		many = append(many, `{"path": "/p/m.go", "line": `+itoa(i+1)+`}`)
	}
	state := `{"version": 1, "breakpoints": [
		{"id": "x", "path": "", "line": 3},
		{"id": "y", "path": "/p/a.go", "line": 0},
		{"id": "same", "path": "/p/a.go", "line": 5},
		{"id": "same", "path": "/p/a.go", "line": 6},
		{"path": "/p/a.go", "line": 7, "column": -4},
		` + strings.Join(many, ",") + `],
		"exceptionBreakpoints": [{"id": "e", "filter": ""}, {"id": "e2", "filter": "all", "label": "All", "fallback": true}],
		"watchExpressions": [{"id": "w", "name": ""}, {"id": "w2", "name": "q"}]}`
	if err := svc.RestoreState([]byte(state)); err != nil {
		t.Fatal(err)
	}
	m := svc.Model()
	a := m.Breakpoints(BreakpointFilter{Path: "/p/a.go"})
	if len(a) != 3 || a[0].Line != 5 || a[1].Line != 6 || a[2].Line != 7 || a[2].Column != 0 {
		t.Errorf("breakpoints of a.go = %+v", a)
	}
	ids := map[string]bool{}
	for _, b := range a {
		ids[b.ID] = true
	}
	if len(ids) != 3 {
		t.Errorf("two breakpoints share an id: %v", ids)
	}
	if got := len(m.Breakpoints(BreakpointFilter{})); got > MaxStateBreakpoints {
		t.Errorf("%d breakpoints were restored, the limit is %d", got, MaxStateBreakpoints)
	}
	if ex := m.ExceptionBreakpoints(""); len(ex) != 1 || ex[0].Filter != "all" {
		t.Errorf("exception breakpoints = %+v", ex)
	}
	if w := m.WatchExpressions(); len(w) != 1 || w[0].Name != "q" {
		t.Errorf("watch = %v", w)
	}
}

func TestRestoredSelectionWaitsForTheLaunches(t *testing.T) {
	svc, folder := stateService(t)
	// The saved selection names a configuration that is gone: the first one is picked once the launches are read.
	state := `{"version": 1, "selected": {"launch": "` + folder + `", "name": "gone"}}`
	if err := svc.RestoreState([]byte(state)); err != nil {
		t.Fatal(err)
	}
	if sel := svc.Launches().Selected(); sel.Name != "" {
		t.Errorf("selected before the launches = %+v", sel)
	}
	if err := svc.Launches().Reload(context.Background(), LaunchSources{Folder: folder}); err != nil {
		t.Fatal(err)
	}
	if sel := svc.Launches().Selected(); sel.Name != "A" {
		t.Errorf("selected = %+v, want the first configuration", sel)
	}
}

func TestSaveStateFailsWhenAdapterDataIsNotJSON(t *testing.T) {
	svc, _ := stateService(t)
	added := svc.Model().addBreakpoints("/p/a.go", []BreakpointData{{Line: 1}})
	svc.Model().mu.Lock()
	for _, b := range svc.Model().lines {
		if b.id == added[0].ID {
			b.adapterData = json.RawMessage(`{broken`)
		}
	}
	svc.Model().mu.Unlock()
	_, err := svc.SaveState()
	routeOf(t, err, "Service.SaveState.DebugStateEncodeFailed")
}

func TestInputMemoryKeepsTheLastFive(t *testing.T) {
	var l LRUInputMemory
	for i := 0; i < 8; i++ {
		l.Set("k"+itoa(i), "v"+itoa(i))
	}
	l.Set("k5", "again")
	if _, ok := l.Get("k0"); ok {
		t.Error("k0 is remembered")
	}
	if v, ok := l.Get("k5"); !ok || v != "again" {
		t.Errorf("k5 = %q %v", v, ok)
	}
	if got := len(l.entries()); got != InputMemoryCapacity {
		t.Errorf("entries = %d, want %d", got, InputMemoryCapacity)
	}
	// k5 was used last, so it is the one that stays longest; k3 is now the oldest.
	l.Set("k8", "v8")
	if _, ok := l.Get("k3"); ok {
		t.Error("k3 is remembered")
	}
}

func fallbackOff(in []ExceptionBreakpoint) []ExceptionBreakpoint {
	out := append([]ExceptionBreakpoint(nil), in...)
	for i := range out {
		out[i].Fallback = false
	}
	return out
}
