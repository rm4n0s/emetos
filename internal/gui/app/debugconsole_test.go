package app

import (
	"context"
	"image"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	dbgview "github.com/rm4n0s/emetos/internal/gui/views/debug"
)

// consoleTexts are the lines a console shows, as text.
func consoleTexts(c *dbgview.Console) []string {
	var out []string
	for _, r := range c.Rows() {
		out = append(out, strings.TrimSuffix(dbgview.RowText(r), "\n"))
	}
	return out
}

func hasLine(c *dbgview.Console, want string) bool {
	for _, l := range consoleTexts(c) {
		if l == want {
			return true
		}
	}
	return false
}

// A client of a hosted workspace sees the Debug Console of the host's session and evaluates in it: the line it types and the result are
// on both screens, and the host's adapter did the evaluation (D46, SPEC §10.31 "Remote Work").
func TestAClientEvaluatesInTheDebugConsoleAndBothScreensShowTheResult(t *testing.T) {
	steps := append(stopSteps("breakpoint", "23.75"),
		daptest.Event("output", map[string]any{"category": "stdout", "output": "\x1b[32mloaded\x1b[0m 3 rows\n"}),
		daptest.ReplyArgs("evaluate", map[string]any{"expression": "sum * 2", "context": "repl", "frameId": 101}, map[string]any{"result": "47.5", "type": "float64"}))
	r := newDebugRigWith(t, debugFolder(t, scriptOf(steps...)), rigOpts{open: []string{"main.go"}})
	v, err := r.win.makeHeadlessView(extension.Size{Width: 900, Height: 560}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	var frames atomic.Int64
	v.OnFrame(func(*image.RGBA) { frames.Add(1) })
	r.waitFor("the view to draw", func() bool { return frames.Load() > 0 && v.ws.shell != nil && v.ws.shell.debug.console != nil })
	client := v.ws
	host := r.win.shell.debug.console
	mine := client.shell.debug.console
	if host == nil || mine == nil || host == mine {
		t.Fatalf("the screens need a console of their own: host %p, client %p", host, mine)
	}
	run := func(w *Workspace, id string) {
		t.Helper()
		if err := w.Commands.Run(command.Context{Ctx: context.Background(), Keys: w.Keys, Source: command.SourceAPI}, id, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.breakpoint() // the script of the fixture answers the setBreakpoints of a first breakpoint
	// the client starts the session; the output of the program is on both screens
	run(client, command.DebugStartCommand)
	r.waitFor("the stop", func() bool { return r.svc().Model().Focus().Frame != nil })
	r.waitFor("the output on both screens", func() bool { return hasLine(host, "loaded 3 rows") && hasLine(mine, "loaded 3 rows") })
	// the client shows its console and types in it
	run(client, command.DebugFocusRepl)
	r.waitFor("the client's Debug Console", func() bool { return client.Views.ActivePanel() == command.ViewContainerRepl })
	mine.Input().SetValue("sum * 2")
	run(client, command.DebugReplAcceptInput)
	r.waitFor("the result on both screens", func() bool { return hasLine(host, "47.5") && hasLine(mine, "47.5") })
	for name, c := range map[string]*dbgview.Console{"host": host, "client": mine} {
		if got := consoleTexts(c); len(got) < 3 || got[len(got)-2] != "sum * 2" || got[len(got)-1] != "47.5" {
			t.Errorf("the %s's console shows %q, want the input and its result last", name, got)
		}
	}
	if v := mine.Input().Value(); v != "" {
		t.Errorf("the client's input keeps %q", v)
	}
	// the host's screen draws it
	run(r.win, command.DebugFocusRepl)
	r.waitFor("the host's Debug Console", func() bool { return r.win.Views.ActivePanel() == command.ViewContainerRepl })
	rich := gluttertest.FindOpts{FindRichText: true}
	r.waitFor("the host's screen to draw the result", func() bool { return r.ts.Exists(gluttertest.ByText("47.5", rich)) })
	if h := r.svc().Model().ReplHistory(); len(h) != 1 || h[0] != "sum * 2" {
		t.Errorf("the host's history of the console = %q", h)
	}
}

// The Debug Console in the panel of the window while the program is stopped, dark and light: lines of the program in color, a group, the
// source of a line, an object and an evaluated line.
func TestGoldenDebugConsolePanel(t *testing.T) {
	steps := append(stopSteps("breakpoint", "23.75"),
		daptest.Event("output", map[string]any{"category": "console", "output": "Launching: bin/pricelist\n"}),
		daptest.Event("output", map[string]any{"category": "stdout", "output": "\x1b[1;32mOK\x1b[0m loaded \x1b[36m3 rows\x1b[0m from prices.txt\n"}),
		daptest.Event("output", map[string]any{"category": "stdout", "output": "Prices", "group": "start"}),
		daptest.Event("output", map[string]any{"category": "stdout", "output": "bread  \x1b[33m2.50\x1b[0m\n"}),
		daptest.Event("output", map[string]any{"category": "stdout", "group": "end"}),
		daptest.Event("output", map[string]any{"category": "stderr", "output": "warning: row 4 is empty\n",
			"source": map[string]any{"name": "main.go", "path": "${workspaceFolder}/main.go"}, "line": 18}),
		daptest.ReplyArgs("evaluate", map[string]any{"expression": "sum * 2", "context": "repl"}, map[string]any{"result": "47.5", "type": "float64"}))
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			r := goldenDebug(t, th.theme, debugFolder(t, scriptOf(steps...)), nil)
			// the rows of the console are in the monospace font of the desktop, which the test fonts stand for
			r.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) {
				c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12
				c.MonospaceFontFamily, c.MonospaceFontSize = testutil.MonoFamily, 13
			})
			r.stopped()
			c := r.win.shell.debug.console
			r.waitFor("the lines", func() bool { return hasLine(c, "warning: row 4 is empty") })
			c.Evaluate("sum * 2")
			r.waitFor("the result", func() bool { return hasLine(c, "47.5") })
			r.run(command.DebugFocusRepl, nil)
			r.settle()
			testutil.MatchesGolden(r.ts, "debug_console_panel_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestTheArgumentsOfTheDevEvaluateCommand(t *testing.T) {
	tests := []struct {
		name string
		args any
		want string
		ok   bool
	}{
		{"valid", map[string]any{"expression": "a + b"}, "a + b", true},
		{"no object", "a + b", "", false},
		{"no expression", map[string]any{"line": 2.0}, "", false},
		{"empty expression", map[string]any{"expression": ""}, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := devEvaluateArgs(tc.args)
			if tc.ok != (err == nil) || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
			if !tc.ok {
				assertRoute(t, err, "devEvaluateArgs.DevDebugEvaluateInvalid")
			}
		})
	}
}

func TestTheDebugConsoleOpensWhenASessionStartsAsTheSettingSays(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		opens    bool
	}{
		{"the default opens it with the first session", map[string]any{"debug.internalConsoleOptions": "openOnFirstSessionStart"}, true},
		{"openOnSessionStart", map[string]any{"debug.internalConsoleOptions": "openOnSessionStart"}, true},
		{"neverOpen", map[string]any{"debug.internalConsoleOptions": "neverOpen"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)), rigOpts{open: []string{"main.go"}, settings: tc.settings})
			if r.win.Layout.State().PanelVisible {
				t.Fatal("the panel is open before the session")
			}
			r.stopped()
			shown := r.win.Layout.State().PanelVisible && r.win.Views.ActivePanel() == command.ViewContainerRepl
			if shown != tc.opens {
				t.Errorf("the Debug Console shows = %v, want %v", shown, tc.opens)
			}
		})
	}
}

func TestTheDebugConsoleClosesWhenTheLastSessionEnds(t *testing.T) {
	for _, closeOnEnd := range []bool{true, false} {
		t.Run(map[bool]string{true: "closeOnEnd", false: "left open"}[closeOnEnd], func(t *testing.T) {
			r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)),
				rigOpts{open: []string{"main.go"}, settings: map[string]any{"debug.console.closeOnEnd": closeOnEnd, "debug.internalConsoleOptions": "openOnFirstSessionStart"}})
			r.stopped()
			if !r.win.Layout.State().PanelVisible || r.win.Views.ActivePanel() != command.ViewContainerRepl {
				t.Fatal("the Debug Console did not open with the session")
			}
			r.run(command.DebugStopCommand, nil)
			r.waitFor("the end of the session", func() bool { return r.win.Keys.Get("debugState") == "inactive" })
			if open := r.win.Layout.State().PanelVisible; open == closeOnEnd {
				t.Errorf("the panel is open = %v after the session with closeOnEnd %v", open, closeOnEnd)
			}
		})
	}
}
