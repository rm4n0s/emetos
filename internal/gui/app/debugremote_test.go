package app

import (
	"context"
	"image"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// Remote Work and debugging (D46, SPEC §10.31 "Remote Work", step 25.12): a client of a hosted workspace sets a breakpoint in its editor,
// starts a session, steps it and stops it, all with the commands of its own view, and the host's window sees the one session of the
// workspace the whole time. The variant with the real dlv is in debugremote_dlv_test.go (tag emetos_dlv).

// clientView makes the headless view of a client of the rig's window and waits until it draws its debug panes.
func (r *debugRig) clientView() *Workspace {
	r.t.Helper()
	v, err := r.win.makeHeadlessView(extension.Size{Width: 900, Height: 560}, rigFrameRate)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(v.Close)
	var frames atomic.Int64
	v.OnFrame(func(*image.RGBA) { frames.Add(1) })
	r.waitFor("the view to draw", func() bool { return frames.Load() > 0 && v.ws.shell != nil && v.ws.shell.debug.panes != nil })
	if v.ws.DebugService() != r.svc() {
		r.t.Fatal("the client's view has a debug service of its own")
	}
	return v.ws
}

// runOn runs a command in the registry of a screen, as its keys and menus do.
func runOn(t *testing.T, w *Workspace, id string) {
	t.Helper()
	if err := w.Commands.Run(command.Context{Ctx: context.Background(), Keys: w.Keys, Source: command.SourceAPI}, id, nil); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
}

func TestAClientSetsABreakpointStartsStepsAndStopsAndTheHostSeesTheSameSession(t *testing.T) {
	// three stops: the one at the breakpoint and two steps, one asked by the client and one by the host
	steps := joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{daptest.Reply("next", nil)}, stopSteps("step", "47.5"),
		[]daptest.Step{daptest.Reply("next", nil)}, stopSteps("step", "71.25"))
	r := newDebugRigWith(t, debugFolder(t, scriptOf(steps...)), rigOpts{open: []string{"main.go"}})
	host := r.mainEditor()
	client := r.clientView()
	client.shell.openPathAt(client.shell.binding, filepath.Join(r.folder, "main.go"), 21, 1, openOptions{TakeFocus: true})
	r.waitFor("the client's editor", func() bool { e := client.shell.activeEditor(); return e != nil && e.dbg != nil })
	mine := client.shell.activeEditor()
	r.cursorAt(mine, 20)

	// the breakpoint: F9 in the client's editor is the host's breakpoint, and both editors show it
	runOn(t, client, command.DebugToggleBreakpoint)
	r.waitFor("the breakpoint on both screens", func() bool {
		return len(r.bps()) == 1 && r.bps()[0].Line == 21 && len(host.view.DebugBreakpoints()) == 1 && len(mine.view.DebugBreakpoints()) == 1
	})

	// the client starts the session; there is one, the host's, and both screens stop at the line
	runOn(t, client, command.DebugStartCommand)
	r.waitFor("the stop on both screens", func() bool {
		return len(host.view.DebugFrames()) == 1 && len(mine.view.DebugFrames()) == 1 &&
			r.win.Keys.Get("debugState") == "stopped" && client.Keys.Get("debugState") == "stopped"
	})
	sessions := r.svc().Model().Sessions(false)
	if len(sessions) != 1 || sessions[0].Name() != "Debug the order" {
		t.Fatalf("the host's service has %d sessions after the client's start", len(sessions))
	}
	session := sessions[0]
	if h, c := host.view.DebugFrames()[0], mine.view.DebugFrames()[0]; h.Line != 20 || c.Line != 20 {
		t.Errorf("the stopped line is %d on the host's screen and %d on the client's, want 20 (line 21)", h.Line, c.Line)
	}
	r.waitFor("the variables on both screens", func() bool {
		return r.sum() == "23.75" && len(client.shell.debug.panes.Variables.Model().Rows()) >= 3
	})
	if !r.win.shell.debug.debugging || !client.shell.debug.debugging {
		t.Errorf("the status bars do not both show the session: host %v, client %v", r.win.shell.debug.debugging, client.shell.debug.debugging)
	}

	// the client steps: the host's Variables view shows the new value, and so does the client's
	runOn(t, client, command.DebugStepOverCommand)
	r.waitFor("the client's step", func() bool { return r.sum() == "47.5" })
	// and the host may step the client's session just as well
	runOn(t, r.win, command.DebugStepOverCommand)
	r.waitFor("the host's step", func() bool { return r.sum() == "71.25" })
	if now := r.svc().Model().Sessions(false); len(now) != 1 || now[0] != session {
		t.Errorf("the steps changed the session: %v", now)
	}
	for name, w := range map[string]*Workspace{"host": r.win, "client": client} {
		r.waitFor("the Variables view of the "+name, func() bool {
			for _, n := range w.shell.debug.panes.Variables.Model().Rows() {
				if row, ok := n.Data.(dbgviewRow); ok && row.Variable != nil && row.Variable.Name == "sum" {
					return row.Variable.Value() == "71.25"
				}
			}
			return false
		})
	}

	// the client stops it, and both screens go back
	runOn(t, client, command.DebugStopCommand)
	r.waitFor("the end on both screens", func() bool {
		return r.win.Keys.Get("debugState") == "inactive" && client.Keys.Get("debugState") == "inactive" &&
			len(host.view.DebugFrames()) == 0 && len(mine.view.DebugFrames()) == 0
	})
	if n := len(r.svc().Model().Sessions(false)); n != 0 {
		t.Errorf("%d sessions are left after the client's stop", n)
	}
	// the breakpoint stays, as the host's
	if len(r.bps()) != 1 {
		t.Errorf("%d breakpoints are left", len(r.bps()))
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

// pickerExtension is a test extension with a configuration provider for the fake debugger that asks a question in a quick pick, as the Go
// extension's process picker does, with the origin of the start it was given (decision 1500).
type pickerExtension struct{ asked chan extension.Origin }

func (*pickerExtension) Manifest() extension.Manifest {
	return extension.Manifest{ID: "test.picker", Name: "Picker (test)", Version: "0.1.0"}
}

func (p *pickerExtension) Activate(_ context.Context, api extension.API) error {
	_, err := api.Debug().RegisterConfigurationProvider("dev.debug.fake", extension.DebugConfigurationProvider{
		Resolve: func(ctx context.Context, _ string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
			origin := extension.OriginFromContext(ctx)
			ch := make(chan int, 1)
			if err := api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Pick a process", Origin: origin, IgnoreFocusOut: true,
				Items: []extension.QuickPickItem{{Label: "worker"}}}, func(i int) { ch <- i }); err != nil {
				return nil, err
			}
			p.asked <- origin
			select {
			case i := <-ch:
				if i < 0 {
					return nil, nil
				}
				return cfg, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}, extension.DebugTriggerInitial)
	return err
}

// The picker of a configuration provider shows on the screen of whoever pressed F5 (decision 1500, which settles 1310): a client's start
// asks the client, the host's start asks the host.
func TestAProvidersPickerShowsOnTheScreenThatStartedTheSession(t *testing.T) {
	steps := stopSteps("breakpoint", "23.75")
	ext := &pickerExtension{asked: make(chan extension.Origin, 4)}
	r := newDebugRigWith(t, debugFolder(t, scriptOf(steps...)), rigOpts{extensions: []extension.Factory{func() extension.Extension { return ext }}})
	r.win.Extensions().Activate("test.picker")
	r.pump()
	r.breakpoint()
	client := r.clientView()

	for _, tc := range []struct {
		name    string
		starter *Workspace
		other   *Workspace
	}{
		{"a client's F5", client, r.win},
		{"the host's F5", r.win, client},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runOn(t, tc.starter, command.DebugStartCommand)
			r.waitFor("the question on the screen that started the session", func() bool { return tc.starter.QuickInput.Visible() })
			if tc.other.QuickInput.Visible() {
				t.Error("the question is also shown on the other screen")
			}
			select {
			case got := <-ext.asked:
				if got != tc.starter.origin {
					t.Errorf("the provider was asked for the origin %d, want %d", got, tc.starter.origin)
				}
			default:
				t.Error("the provider did not ask")
			}
			tc.starter.QuickInput.Accept(quickinput.KeyMods{})
			r.waitFor("the stop", func() bool { return r.win.Keys.Get("debugState") == "stopped" })
			if n := len(r.svc().Model().Sessions(false)); n != 1 {
				t.Fatalf("%d sessions", n)
			}
			runOn(t, tc.starter, command.DebugStopCommand)
			r.waitFor("the end", func() bool { return r.win.Keys.Get("debugState") == "inactive" })
		})
	}
}
