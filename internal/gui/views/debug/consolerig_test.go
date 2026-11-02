package debug

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	coredebug "github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var fullCaps = map[string]any{
	"supportsConfigurationDoneRequest": true, "supportsCompletionsRequest": true, "completionTriggerCharacters": []string{"."},
}

// consoleRig is the Debug Console over a service whose adapter is a scripted daptest adapter. The test goroutine is the UI thread: what
// the console posts waits for the next wait or pump.
type consoleRig struct {
	t      *testing.T
	ts     *gluttertest.Tester
	svc    *coredebug.Service
	con    *Console
	colors kit.Colors

	mu     sync.Mutex
	posted []func()

	menus  []ConsoleRow
	opened []string
	cfg    ConsoleConfig
}

type consoleRigOpts struct {
	colors kit.Colors
	size   ui.Size
	// script is the script of the session; nil starts no session.
	script daptest.Script
	// tune changes the options of the console before it is made.
	tune func(*ConsoleOpts)
	cfg  *ConsoleConfig
	// service options
	service func(*coredebug.Options)
}

// scriptOf is the start of a session of the fake adapter, the steps, and its end.
func scriptOf(caps map[string]any, steps ...daptest.Step) daptest.Script {
	s := daptest.Script{
		daptest.Reply("initialize", caps),
		daptest.Expect("launch", map[string]any{"type": "fake", "request": "launch"}),
		daptest.Event("initialized", nil),
		daptest.Reply("configurationDone", nil),
		daptest.Respond("launch", nil),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
	}
	s = append(s, steps...)
	return append(s, daptest.Reply("disconnect", nil), daptest.WaitClose())
}

func out(body map[string]any) daptest.Step { return daptest.Event("output", body) }

// kickWith is the steps of an adapter that sends events as a result of the custom request "kick".
func kickWith(events ...daptest.Step) []daptest.Step {
	return append(append([]daptest.Step{daptest.Expect("kick", nil)}, events...), daptest.Respond("kick", nil))
}

func newConsoleRig(t *testing.T, o consoleRigOpts) *consoleRig {
	t.Helper()
	r := &consoleRig{t: t, colors: o.colors}
	if r.colors == (kit.Colors{}) {
		r.colors = kit.DarkModern()
	}
	if o.size == (ui.Size{}) {
		o.size = ui.Size{Width: 720, Height: 360}
	}
	r.cfg = DefaultConsoleConfig
	if o.cfg != nil {
		r.cfg = *o.cfg
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	opts := coredebug.Options{
		FocusDelay: 20 * time.Millisecond, RunningDelay: 20 * time.Millisecond,
		Debugger: func(typ string) (coredebug.Debugger, bool) {
			if typ != "fake" {
				return coredebug.Debugger{}, false
			}
			return coredebug.Debugger{Type: "fake", Label: "Fake", Connect: func(_ context.Context, _ coredebug.Config, c dap.ClientOptions) (*dap.Client, error) {
				h := daptest.Start(t, daptest.Options{Script: o.script, Client: c})
				return h.Client, nil
			}}, true
		},
		Console: func() coredebug.ReplConfig {
			return coredebug.ReplConfig{CollapseIdenticalLines: true, MaximumLines: 1_000_000}
		},
	}
	if o.service != nil {
		o.service(&opts)
	}
	r.svc = coredebug.NewService(ctx, opts)
	co := ConsoleOpts{
		Ctx: ctx, Service: r.svc, Post: r.post, Config: func() ConsoleConfig { return r.cfg }, Folder: "/work",
		OpenFile: func(path string, line, col int) {
			r.opened = append(r.opened, path+":"+strconv.Itoa(line)+":"+strconv.Itoa(col))
		},
		OpenURL: func(u string) { r.opened = append(r.opened, u) },
		Stat:    func(path string) bool { return path == "/work/src/loader.go" },
		Home:    "/home/ada", CompletionDelay: time.Millisecond,
	}
	if o.tune != nil {
		o.tune(&co)
	}
	r.con = NewConsole(co)
	t.Cleanup(r.con.Dispose)
	body := NewConsoleBody(r.con, ConsoleBodyOpts{Menu: func(row ConsoleRow, ok bool, _ ui.Offset) {
		if ok {
			r.menus = append(r.menus, row)
		}
	}})
	r.ts = testutil.New(t, o.size)
	r.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) {
		c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12
		c.MonospaceFontFamily, c.MonospaceFontSize = testutil.MonoFamily, 13
	})
	r.ts.PumpWidget(widgets.NewWidgetsApp(r.colors.PanelBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewContainer(body, widgets.ContainerOpts{Color: foundation.Some(r.colors.PanelBackground)})
				return kit.NewTheme(r.colors, page, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	r.ts.Pump(time.Millisecond)
	if o.script != nil {
		ok, err := r.svc.StartDebugging(ctx, nil, coredebug.Target{Config: coredebug.Config{"name": "console", "type": "fake", "request": "launch"}}, coredebug.SessionOptions{})
		if err != nil || !ok {
			t.Fatalf("start: %v %v", ok, err)
		}
		r.waitFor("the console to show the session", func() bool { return r.con.Session() != nil })
	}
	return r
}

func (r *consoleRig) post(f func()) {
	r.mu.Lock()
	r.posted = append(r.posted, f)
	r.mu.Unlock()
}

// drain runs what was posted, as the UI thread would.
func (r *consoleRig) drain() {
	for {
		r.mu.Lock()
		q := r.posted
		r.posted = nil
		r.mu.Unlock()
		if len(q) == 0 {
			return
		}
		for _, f := range q {
			f()
		}
	}
}

func (r *consoleRig) pump() {
	for range 4 {
		r.drain()
		r.ts.Pump(20 * time.Millisecond)
	}
}

func (r *consoleRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		r.drain()
		if cond() {
			break
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		r.ts.Pump(5 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	r.pump()
}

// kick makes the adapter send what it has for the custom request "kick".
func (r *consoleRig) kick() {
	r.t.Helper()
	sess := r.con.Session()
	if _, err := sess.CustomRequest(context.Background(), "kick", nil); err != nil {
		r.t.Fatal(err)
	}
}

// waitRows waits for a number of rows.
func (r *consoleRig) waitRows(n int) {
	r.t.Helper()
	r.waitFor("rows", func() bool { return r.con.Len() == n })
}

func (r *consoleRig) rowTexts() []string {
	var out []string
	for _, row := range r.con.Rows() {
		out = append(out, RowText(row))
	}
	return out
}

var _ = painting.AlignmentTopLeft

// waitForRow waits for a row whose text contains a text.
func (r *consoleRig) waitForRow(sub string) {
	r.t.Helper()
	r.waitFor("a row with "+sub, func() bool {
		for _, t := range r.rowTexts() {
			if strings.Contains(t, sub) {
				return true
			}
		}
		return false
	})
}
