package exthost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The Debug API of the extensions (step 25.3): each call through the host, with daptest as the adapter of a contributed debugger.

func TestSessionsAreFollowedByTheListenersOfAnExtension(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	rec := &strRecorder{}
	api := d.starter.Debug()
	removeStart := api.OnDidStartSession(func(s extension.DebugSession) { rec.add("start " + s.Name()) })
	api.OnDidTerminateSession(func(s extension.DebugSession) { rec.add("end " + s.Name()) })
	api.OnDidChangeActiveSession(func(s extension.DebugSession) {
		if s == nil {
			rec.add("active none")
			return
		}
		rec.add("active " + s.Name())
	})
	d.script("a", sessionScript())
	d.start(fakeConfig("a", "exec"))
	waitDebug(t, "the listeners", func() bool { return len(rec.all()) >= 2 })
	d.stop()
	waitDebug(t, "the end", func() bool { return len(rec.all()) >= 4 })
	got := strings.Join(rec.all(), ",")
	for _, want := range []string{"start a", "active a", "end a", "active none"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from %s", want, got)
		}
	}
	if strings.Index(got, "start a") > strings.Index(got, "end a") {
		t.Errorf("the end came before the start: %s", got)
	}
	// a listener that was removed hears no more
	removeStart()
	d.script("b", sessionScript())
	d.start(fakeConfig("b", "exec"))
	d.stop()
	if strings.Contains(strings.Join(rec.all(), ","), "start b") {
		t.Error("a removed listener heard of a session")
	}
}

func TestACustomRequestAndACustomEventGoThroughTheSession(t *testing.T) {
	events := &strRecorder{}
	d := newDebugRig(t, dbgOpts{})
	d.starter.Debug().OnDidReceiveCustomEvent(func(e extension.DebugSessionCustomEvent) {
		events.add(e.Session.Name() + " " + e.Event + " " + string(e.Body))
	})
	d.script("a", sessionScript(
		daptest.Event("fake/ready", map[string]any{"port": 4711}),
		daptest.ReplyArgs("fake/stats", map[string]any{"depth": 3}, map[string]any{"count": 42}),
	))
	s := d.start(fakeConfig("a", "server"))
	waitDebug(t, "the custom event", func() bool { return len(events.all()) == 1 })
	if got := events.all()[0]; got != `a fake/ready {"port":4711}` {
		t.Errorf("event = %s", got)
	}
	d.idle(s)
	ctx, cancel := context.WithTimeout(context.Background(), debugWait)
	defer cancel()
	body, err := s.CustomRequest(ctx, "fake/stats", map[string]any{"depth": 3})
	if err != nil || string(body) != `{"count":42}` {
		t.Fatalf("CustomRequest = %s, %v", body, err)
	}
	d.stop()
	if v := d.server("a").Violations(); len(v) > 0 {
		t.Errorf("the fake adapter noticed %v", v)
	}
}

func TestAChildSessionIsShownUnderItsParent(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	d.script("parent", sessionScript())
	// a session without debugging sends no breakpoints
	d.script("child", daptest.Script{
		daptest.Reply("initialize", map[string]any{}),
		daptest.Expect("launch", map[string]any{"type": "fake", "noDebug": true}),
		daptest.Event("initialized", nil),
		daptest.Respond("launch", nil),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
		daptest.Reply("disconnect", nil), daptest.WaitClose(),
	})
	parent := d.start(fakeConfig("parent", "server"))
	d.idle(parent)
	ctx, cancel := context.WithTimeout(context.Background(), debugWait*2)
	defer cancel()
	ok, err := d.starter.Debug().StartDebugging(ctx, "", extension.DebugTarget{Config: fakeConfig("child", "pipe")},
		extension.DebugStartOptions{Parent: parent, Compact: true, ConsoleMode: extension.DebugConsoleMergeWithParent, NoDebug: true})
	if err != nil || !ok {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
	child := d.sessionNamed("child")
	if child.Parent() == nil || child.Parent().ID() != parent.ID() {
		t.Fatalf("the parent of the child = %v", child.Parent())
	}
	if parent.Parent() != nil {
		t.Error("the parent has a parent")
	}
	cs := d.coreSession(child)
	if o := cs.Options(); !o.Compact || o.Repl != "mergeWithParent" || o.NoDebug == nil || !*o.NoDebug || cs.Parent() != d.coreSession(parent) {
		t.Errorf("options = %+v", o)
	}
	if child.Configuration()["noDebug"] != true {
		t.Errorf("configuration of a child of a session without debugging = %v", child.Configuration())
	}
	d.stop()
}

func TestAnExtensionStartsANamedConfigurationOfALaunchFile(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	d.script("From file", sessionScript())
	d.script("Second", sessionScript())
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o700); err != nil {
		t.Fatal(err)
	}
	text := `{"configurations": [{"name": "From file", "type": "fake", "request": "launch", "program": "p", "transport": "exec"}],
 "compounds": [{"name": "Both", "configurations": ["From file"]}]}`
	if err := os.WriteFile(debug.LaunchFilePath(folder), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.debug.Service().Launches().Reload(context.Background(), debug.LaunchSources{Folder: folder}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, folderArg := range []string{folder, ""} {
		ok, err := d.starter.Debug().StartDebugging(ctx, folderArg, extension.DebugTarget{Name: "From file"}, extension.DebugStartOptions{})
		if err != nil || !ok {
			t.Fatalf("StartDebugging(%q) = %v, %v", folderArg, ok, err)
		}
		s := d.sessionNamed("From file")
		if s.Folder() != folder {
			t.Errorf("folder of the session = %q, want %q", s.Folder(), folder)
		}
		d.stop()
		d.script("From file", sessionScript())
	}
	// a name that is not there
	ok, err := d.starter.Debug().StartDebugging(ctx, folder, extension.DebugTarget{Name: "Missing"}, extension.DebugStartOptions{})
	appErr, isErr := errors.FromError(err)
	if ok || !isErr || appErr.Tag != "DebugConfigurationMissing" {
		t.Errorf("StartDebugging(Missing) = %v, %v", ok, err)
	}
	// a compound
	ok, err = d.starter.Debug().StartDebugging(ctx, "", extension.DebugTarget{Name: "Both"}, extension.DebugStartOptions{})
	if err != nil || !ok {
		t.Errorf("StartDebugging(compound) = %v, %v", ok, err)
	}
	d.stop()
}

func TestTheBreakpointsOfAnExtension(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	api := d.starter.Debug()
	var mu sync.Mutex
	var changes []extension.DebugBreakpointsChange
	api.OnDidChangeBreakpoints(func(c extension.DebugBreakpointsChange) { mu.Lock(); changes = append(changes, c); mu.Unlock() })
	ctx := context.Background()
	a := document.FileURI("/w/a.go").String()
	err := api.AddBreakpoints(ctx, []extension.DebugBreakpoint{
		{URI: a, Position: extension.Position{Line: 9}, Condition: "i > 3"},
		{URI: a, Position: extension.Position{Line: 19, Col: 4}, Disabled: true, LogMessage: "x={x}"},
		{Function: "main.run", HitCondition: "> 2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := api.Breakpoints()
	if len(got) != 3 {
		t.Fatalf("breakpoints = %+v", got)
	}
	by := map[string]extension.DebugBreakpoint{}
	for _, b := range got {
		if b.ID == "" {
			t.Errorf("a breakpoint has no ID: %+v", b)
		}
		key := b.Function
		if key == "" {
			key = fmt.Sprintf("%d:%d", b.Position.Line, b.Position.Col)
		}
		by[key] = b
	}
	if b := by["9:0"]; b.URI != a || b.Condition != "i > 3" || b.Disabled {
		t.Errorf("line breakpoint = %+v", b)
	}
	if b := by["19:4"]; !b.Disabled || b.LogMessage != "x={x}" {
		t.Errorf("inline breakpoint = %+v", b)
	}
	if b := by["main.run"]; b.HitCondition != "> 2" {
		t.Errorf("function breakpoint = %+v", b)
	}
	mu.Lock()
	added := 0
	for _, c := range changes {
		added += len(c.Added)
	}
	mu.Unlock()
	if added != 3 {
		t.Errorf("the listener heard of %d added breakpoints: %+v", added, changes)
	}
	// removed by place, by function and by ID
	if err := api.RemoveBreakpoints(ctx, []extension.DebugBreakpoint{{URI: a, Position: extension.Position{Line: 9}}, {Function: "main.run"}}); err != nil {
		t.Fatal(err)
	}
	rest := api.Breakpoints()
	if len(rest) != 1 || rest[0].Position.Line != 19 {
		t.Fatalf("after removing: %+v", rest)
	}
	if err := api.RemoveBreakpoints(ctx, []extension.DebugBreakpoint{{ID: rest[0].ID}}); err != nil {
		t.Fatal(err)
	}
	if n := len(api.Breakpoints()); n != 0 {
		t.Errorf("%d breakpoints remain", n)
	}
	mu.Lock()
	removed := 0
	for _, c := range changes {
		removed += len(c.Removed)
	}
	mu.Unlock()
	if removed != 3 {
		t.Errorf("the listener heard of %d removed breakpoints", removed)
	}
}

func TestABreakpointThatIsNotInAFileIsRefused(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	for _, uri := range []string{"", "untitled:1", "::bad"} {
		err := d.starter.Debug().AddBreakpoints(context.Background(), []extension.DebugBreakpoint{{URI: uri}})
		appErr, isErr := errors.FromError(err)
		if !isErr || !appErr.HasRoute("debugAPI.AddBreakpoints.ExtensionDebugBreakpointInvalid") {
			t.Errorf("AddBreakpoints(%q) = %v", uri, err)
		}
	}
}

func TestTheDebugConsoleReceivesWhatAnExtensionAppends(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	var mu sync.Mutex
	var out []string
	d.debug.Service().Subscribe(func(ev debug.Event) {
		if ev.Kind == debug.EventOutput && ev.Output != nil {
			mu.Lock()
			out = append(out, ev.Output.Category+":"+ev.Output.Output)
			mu.Unlock()
		}
	})
	d.starter.Debug().Console().Append("one")
	d.starter.Debug().Console().AppendLine("two")
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(out, "|") != "console:one|console:two\n" {
		t.Errorf("console = %q", out)
	}
}

func TestTheRegistrationsFail(t *testing.T) {
	provider := extension.DebugConfigurationProvider{Resolve: func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
		return nil, nil
	}}
	factory := func(context.Context, extension.DebugSession, *extension.DebugAdapter) (*extension.DebugAdapter, error) {
		return nil, nil
	}
	tracker := func(context.Context, extension.DebugSession) (*extension.DebugAdapterTracker, error) { return nil, nil }
	register := map[string]func(a extension.Debug, typ string) error{
		"factory": func(a extension.Debug, typ string) error {
			_, err := a.RegisterAdapterDescriptorFactory(typ, factory)
			return err
		},
		"nil factory": func(a extension.Debug, typ string) error {
			_, err := a.RegisterAdapterDescriptorFactory(typ, nil)
			return err
		},
		"provider": func(a extension.Debug, typ string) error {
			_, err := a.RegisterConfigurationProvider(typ, provider, extension.DebugTriggerInitial)
			return err
		},
		"empty provider": func(a extension.Debug, typ string) error {
			_, err := a.RegisterConfigurationProvider(typ, extension.DebugConfigurationProvider{}, extension.DebugTriggerInitial)
			return err
		},
		"tracker": func(a extension.Debug, typ string) error {
			_, err := a.RegisterAdapterTrackerFactory(typ, tracker)
			return err
		},
		"nil tracker factory": func(a extension.Debug, typ string) error {
			_, err := a.RegisterAdapterTrackerFactory(typ, nil)
			return err
		},
	}
	tests := []struct {
		name  string
		which string
		typ   string
		tab   func(*dbgOpts)
		route string
	}{
		{"a factory with no type", "factory", "", nil, "debugAPI.register.ExtensionDebugInvalid"},
		{"no factory function", "nil factory", "fake", nil, "debugAPI.register.ExtensionDebugInvalid"},
		{"a provider with no function", "empty provider", "fake", nil, "debugAPI.register.ExtensionDebugInvalid"},
		{"a provider with no type", "provider", "", nil, "debugAPI.register.ExtensionDebugInvalid"},
		{"no tracker function", "nil tracker factory", "fake", nil, "debugAPI.register.ExtensionDebugInvalid"},
		{"a tracker with no type", "tracker", "", nil, "debugAPI.register.ExtensionDebugInvalid"},
		{"a factory in a tab with no debug host", "factory", "fake", func(o *dbgOpts) { o.noHost = true }, "debugAPI.host.ExtensionDebugUnavailable"},
		{"a provider in a tab with no debug host", "provider", "fake", func(o *dbgOpts) { o.noHost = true }, "debugAPI.host.ExtensionDebugUnavailable"},
		{"a tracker in a tab with no debug host", "tracker", "fake", func(o *dbgOpts) { o.noHost = true }, "debugAPI.host.ExtensionDebugUnavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var o dbgOpts
			if tt.tab != nil {
				tt.tab(&o)
			}
			d := newDebugRig(t, o)
			err := register[tt.which](d.starter.Debug(), tt.typ)
			appErr, isErr := errors.FromError(err)
			if !isErr || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want the route %s", err, tt.route)
			}
		})
	}
	t.Run("a second factory for a type", func(t *testing.T) {
		d := newDebugRig(t, dbgOpts{})
		d.scope.Fire(extension.OnDebug("fake")) // dbg registers its factory
		err := register["factory"](d.starter.Debug(), "fake")
		appErr, isErr := errors.FromError(err)
		if !isErr || !appErr.HasRoute("debugAPI.RegisterAdapterDescriptorFactory.ExtensionDebugFactoryTaken") {
			t.Errorf("err = %v", err)
		}
		// once the first is removed the type is free
		d.disabled = []string{"dbg"}
		d.scope.Refresh()
		if err := register["factory"](d.starter.Debug(), "fake"); err != nil {
			t.Errorf("the type stayed taken after its extension went: %v", err)
		}
	})
	t.Run("after the activation ended", func(t *testing.T) {
		d := newDebugRig(t, dbgOpts{})
		api := d.starter.Debug()
		d.scope.Close()
		for name, fn := range register {
			err := fn(api, "fake")
			appErr, isErr := errors.FromError(err)
			if !isErr || !appErr.HasRoute("debugAPI.host.ExtensionDeactivated") {
				t.Errorf("%s: err = %v", name, err)
			}
		}
		ctx := context.Background()
		for name, err := range map[string]error{
			"StartDebugging": func() error {
				_, e := api.StartDebugging(ctx, "", extension.DebugTarget{Name: "x"}, extension.DebugStartOptions{})
				return e
			}(),
			"StopDebugging":     api.StopDebugging(ctx, nil),
			"AddBreakpoints":    api.AddBreakpoints(ctx, nil),
			"RemoveBreakpoints": api.RemoveBreakpoints(ctx, nil),
		} {
			appErr, isErr := errors.FromError(err)
			if !isErr || !appErr.HasRoute("debugAPI.host.ExtensionDeactivated") {
				t.Errorf("%s: err = %v", name, err)
			}
		}
		if api.ActiveSession() != nil || len(api.Sessions()) != 0 || len(api.Breakpoints()) != 0 {
			t.Error("a deactivated extension sees sessions or breakpoints")
		}
		api.Console().Append("ignored")
		remove := api.OnDidStartSession(func(extension.DebugSession) {})
		remove()
	})
}

func TestStartDebuggingAndStoppingFail(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	api := d.starter.Debug()
	ctx := context.Background()
	t.Run("a target with nothing in it", func(t *testing.T) {
		_, err := api.StartDebugging(ctx, "", extension.DebugTarget{}, extension.DebugStartOptions{})
		appErr, isErr := errors.FromError(err)
		if !isErr || !appErr.HasRoute("debugAPI.StartDebugging.ExtensionDebugInvalid") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a parent that is not a session of the tab", func(t *testing.T) {
		other := newDebugRig(t, dbgOpts{})
		other.script("o", sessionScript())
		foreign := other.start(fakeConfig("o", "exec"))
		_, err := api.StartDebugging(ctx, "", extension.DebugTarget{Config: fakeConfig("c", "exec")}, extension.DebugStartOptions{Parent: foreign})
		appErr, isErr := errors.FromError(err)
		if !isErr || !appErr.HasRoute("DebugHost.coreSession.ExtensionDebugSessionForeign") {
			t.Errorf("StartDebugging err = %v", err)
		}
		err = api.StopDebugging(ctx, foreign)
		appErr, isErr = errors.FromError(err)
		if !isErr || !appErr.HasRoute("DebugHost.coreSession.ExtensionDebugSessionForeign") {
			t.Errorf("StopDebugging err = %v", err)
		}
		other.stop()
	})
	t.Run("an unavailable service", func(t *testing.T) {
		nohost := newDebugRig(t, dbgOpts{noHost: true})
		_, err := nohost.starter.Debug().StartDebugging(ctx, "", extension.DebugTarget{Name: "x"}, extension.DebugStartOptions{})
		appErr, isErr := errors.FromError(err)
		if !isErr || !appErr.HasRoute("debugAPI.host.ExtensionDebugUnavailable") {
			t.Errorf("err = %v", err)
		}
		if nohost.starter.Debug().ActiveSession() != nil || len(nohost.starter.Debug().Sessions()) != 0 || len(nohost.starter.Debug().Breakpoints()) != 0 {
			t.Error("a tab with no debug host has sessions")
		}
		nohost.starter.Debug().Console().Append("ignored")
		nohost.starter.Debug().OnDidStartSession(func(extension.DebugSession) {})()
	})
}

func TestAPanicInAListenerTakesTheExtensionOff(t *testing.T) {
	d := newDebugRig(t, dbgOpts{activate: func(_ *debugRig, api extension.API) error {
		api.Debug().OnDidStartSession(func(extension.DebugSession) { panic("boom") })
		return nil
	}})
	d.script("a", sessionScript())
	// the session of the failed extension's debug type ends with it, so whether the start reports it is not fixed
	if _, err := d.startErr(fakeConfig("a", "exec")); err != nil {
		t.Log("StartDebugging:", err)
	}
	waitDebug(t, "the extension to fail", func() bool { return d.scope.State("dbg") == StateFailed })
	f := d.scope.Failure("dbg")
	if f == nil || !strings.Contains(f.Where, "OnDidStartSession") || !f.Err.(*errors.Error).HasRoute("DebugHost.guard.func1.ExtensionDebugPanicked") {
		t.Errorf("failure = %+v", f)
	}
	waitDebug(t, "the session to end", func() bool { return len(d.starter.Debug().Sessions()) == 0 })
}

// ---- what the manifest declares

func TestWhatTheEnabledExtensionsDeclare(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	infos := d.debug.Debuggers()
	if len(infos) != 1 || infos[0].Type != "fake" || infos[0].Label != "Fake" || infos[0].Extension != "dbg" || strings.Join(infos[0].Languages, ",") != "fakelang" || infos[0].Dynamic {
		t.Errorf("debuggers = %+v", infos)
	}
	if got := d.debug.DebuggersForLanguage("fakelang"); len(got) != 1 {
		t.Errorf("debuggers for the language = %+v", got)
	}
	if got := d.debug.DebuggersForLanguage("go"); len(got) != 0 {
		t.Errorf("debuggers for another language = %+v", got)
	}
	if got := d.debug.BreakpointLanguages(); strings.Join(got, ",") != "fakelang" {
		t.Errorf("breakpoint languages = %v", got)
	}
	for _, tc := range []struct {
		lang       string
		everywhere bool
		want       bool
	}{{"fakelang", false, true}, {"go", false, false}, {"go", true, true}} {
		if got := d.debug.AllowsBreakpoints(tc.lang, tc.everywhere); got != tc.want {
			t.Errorf("AllowsBreakpoints(%s, %v) = %v", tc.lang, tc.everywhere, got)
		}
	}
	dbg, ok := d.debug.debugger("fake")
	if !ok || dbg.Label != "Fake" || dbg.Variables["pickProcess"] != "dbg.pickProcess" || strings.Join(dbg.Languages, ",") != "fakelang" {
		t.Errorf("debugger = %+v, %v", dbg, ok)
	}
	if _, ok := d.debug.debugger("other"); ok {
		t.Error("a debug type nobody declared")
	}
	d.disabled = []string{"dbg"}
	d.scope.Refresh()
	if len(d.debug.Debuggers()) != 0 || len(d.debug.BreakpointLanguages()) != 0 {
		t.Error("the declarations of a turned off extension remain")
	}
}

// ---- launch.json

func TestLaunchJSONIsCheckedAgainstTheAttributesOfItsDebuggers(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	path := "/w/.emetos/launch.json"
	text := `{
  "configurations": [
    {"name": "ok", "type": "fake", "request": "launch", "program": "p"},
    {"name": "no program", "type": "fake", "request": "launch"},
    {"name": "bad mode", "type": "fake", "request": "launch", "program": "p", "mode": "c"},
    {"name": "unknown", "type": "nope", "request": "launch"},
    {"name": "no type", "request": "launch"}
  ]
}`
	d.debug.SetLaunchText(path, text)
	read := func() []problems.Marker { return d.markers.Read(problems.ReadFilter{Owner: LaunchProblemsOwner}) }
	got := read()
	if len(got) != 4 {
		t.Fatalf("%d markers: %+v", len(got), got)
	}
	want := map[int]string{
		4: `Missing property "program".`,
		5: `Value is not accepted. Valid values: "a", "b".`,
		6: "Unknown debug type 'nope'.",
		7: `Missing property "type".`,
	}
	for _, m := range got {
		w, ok := want[m.StartLine]
		if !ok || !strings.HasPrefix(m.Message, w) {
			t.Errorf("marker at line %d: %q (want %q)", m.StartLine, m.Message, w)
		}
		if m.Resource != path || m.Owner != LaunchProblemsOwner || m.StartColumn < 1 || m.EndColumn < 1 {
			t.Errorf("marker = %+v", m)
		}
		if m.Source != "launch.json" || m.Severity != problems.SeverityWarning {
			t.Errorf("marker source and severity = %q %v", m.Source, m.Severity)
		}
	}
	// turning the debugger off makes its configurations unknown: the file is checked again
	d.disabled = []string{"dbg"}
	d.scope.Refresh()
	after := read()
	unknown := 0
	for _, m := range after {
		if strings.HasPrefix(m.Message, "Unknown debug type 'fake'") {
			unknown++
		}
	}
	if unknown != 3 {
		t.Errorf("%d unknown fake types after the debugger went: %+v", unknown, after)
	}
	d.debug.ForgetLaunch(path)
	if n := len(read()); n != 0 {
		t.Errorf("%d markers after the file was forgotten", n)
	}
}

func TestLaunchJSONSyntaxErrorsAreProblemsToo(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	d.debug.SetLaunchText("/w/.emetos/launch.json", "{ \"configurations\": [ { \"name\": 1 } ] ")
	got := d.markers.Read(problems.ReadFilter{Owner: LaunchProblemsOwner})
	hasError := false
	for _, m := range got {
		hasError = hasError || m.Severity == problems.SeverityError
	}
	if !hasError {
		t.Errorf("markers = %+v", got)
	}
}

func launchDoc(path, text string) langfeatures.Document {
	return langfeatures.Document{URI: document.FileURI(path), LanguageID: "jsonc", Version: 1, Text: buffer.New(text).Snapshot()}
}

func TestLaunchJSONCompletesFromTheAttributesOfItsDebuggers(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	f := d.scope.Features()
	text := "{\n  \"configurations\": [\n    {\"type\": \"fake\", \"request\": \"launch\", \n    }\n  ]\n}"
	path := "/w/.emetos/launch.json"
	doc := launchDoc(path, text)
	if !f.HasCompletion(doc) {
		t.Fatal("launch.json has no completion")
	}
	if f.HasCompletion(launchDoc("/w/other.json", text)) {
		t.Error("another JSON file completes as a launch file")
	}
	offset := strings.Index(text, "\n    }")
	pos := doc.Text.PositionAt(offset)
	list, errs := f.Completion(context.Background(), doc, pos, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	have := map[string]langfeatures.CompletionItem{}
	for _, it := range list.Items {
		have[it.Label] = it
	}
	for _, name := range []string{"program", "transport", "mode", "name"} {
		if _, ok := have[name]; !ok {
			t.Errorf("%s is not suggested: %v", name, list.Items)
		}
	}
	if it := have["program"]; !it.Snippet || !strings.HasPrefix(it.InsertText, `"program": `) || it.Owner != launchOwner || it.Insert == nil {
		t.Errorf("program = %+v", it)
	}
	if _, has := have["type"]; has {
		t.Error("an attribute that is there is suggested again")
	}
	// and the debug types where the type goes
	text2 := "{\"configurations\": [{\"type\": }]}"
	doc2 := launchDoc(path, text2)
	list, _ = f.Completion(context.Background(), doc2, doc2.Text.PositionAt(strings.Index(text2, "}]")), langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
	if len(list.Items) != 1 || list.Items[0].Label != "fake" || list.Items[0].InsertText != `"fake"` || list.Items[0].Detail != "Fake" {
		t.Errorf("types = %+v", list.Items)
	}
	// and the snippets of the debugger where a configuration starts
	text3 := "{\"configurations\": [  ]}"
	doc3 := launchDoc(path, text3)
	list, _ = f.Completion(context.Background(), doc3, doc3.Text.PositionAt(strings.Index(text3, "  ]")+1), langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
	if len(list.Items) != 1 || list.Items[0].Label != "Fake: Launch" || !list.Items[0].Snippet {
		t.Errorf("snippets = %+v", list.Items)
	}
}

func TestAClientsCommandStartsASessionOfTheHost(t *testing.T) {
	const view extension.Origin = 3
	var mu sync.Mutex
	var origins []extension.Origin
	d := newDebugRig(t, dbgOpts{})
	d.script("from client", sessionScript())
	// the starter gets a command that starts a session, as the commands of the Run and Debug view will
	if err := d.starter.Commands().Register(extension.CommandSpec{ID: "starter.debug.start", Title: "Start"}, func(ctx context.Context, _ any) error {
		mu.Lock()
		origins = append(origins, d.starter.Origins().Current())
		mu.Unlock()
		ok, err := d.starter.Debug().StartDebugging(ctx, "", extension.DebugTarget{Config: fakeConfig("from client", "exec")}, extension.DebugStartOptions{})
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("TestStartCancelled", "the session did not start")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	client := command.DefaultRegistry()
	client.Inherit(d.commands, view)
	if !client.HasHandler("starter.debug.start") {
		t.Fatal("the view of the client does not have the command of the extension")
	}
	if err := client.Run(command.Context{Ctx: context.Background(), Source: command.SourceMenu}, "starter.debug.start", nil); err != nil {
		t.Fatalf("the client's command: %v", err)
	}
	mu.Lock()
	if len(origins) != 1 || origins[0] != view {
		t.Errorf("origins = %v, want [%d]", origins, view)
	}
	mu.Unlock()
	sessions := d.debug.Service().Model().Sessions(false)
	if len(sessions) != 1 || sessions[0].Name() != "from client" {
		t.Fatalf("the host's service has %d sessions", len(sessions))
	}
	// the host sees the same session as the client's command started, and may stop it
	if got := d.starter.Debug().Sessions(); len(got) != 1 || got[0].Name() != "from client" {
		t.Errorf("the extension sees %v", got)
	}
	d.stop()
}

func TestARestartIsNotATermination(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	ended := &strRecorder{}
	d.starter.Debug().OnDidTerminateSession(func(s extension.DebugSession) { ended.add(s.Name()) })
	d.script("a", sessionScript())
	s := d.start(fakeConfig("a", "exec"))
	d.idle(s)
	cs := d.coreSession(s)
	// the end that a restart follows
	d.debug.onEvent(debug.Event{Kind: debug.EventSessionEnded, Session: cs, Restart: true})
	if got := ended.all(); len(got) != 0 {
		t.Errorf("a restart was reported as the end of %v", got)
	}
	d.debug.onEvent(debug.Event{Kind: debug.EventSessionEnded, Session: cs})
	if got := ended.all(); len(got) != 1 || got[0] != "a" {
		t.Errorf("the end was reported as %v", got)
	}
	d.stop()
}

func TestADebugHostServesOneScope(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	second := d.r.host.NewScope(ScopeOptions{
		Info: extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "second"}, Ctx: context.Background(), Notify: d.notify, Debug: d.debug,
	})
	t.Cleanup(second.Close)
	if !strings.Contains(d.log.String(), "serves one scope") {
		t.Errorf("the host did not say that it serves one scope: %s", d.log.String())
	}
	if d.debug.scopeOf() != d.scope {
		t.Error("the host serves the second scope")
	}
}
