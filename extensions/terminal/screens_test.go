package terminal

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The terminals on the screens of a hosted workspace (step 24.25, SPEC §10.25 "Clients of a shared workspace", D51): the view is built once for
// the window of the host and once for the view of a client, each with a pane of its own over the same terminal.

// twoScreens shows the view of the extension on the window of the host (600 wide) and on the view of a client with the origin 3 (300 wide).
func twoScreens(t *testing.T) *panelRig {
	t.Helper()
	r := &panelRig{extRig: newExtRigAt(t, t.TempDir(), nil, "")}
	root := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		r.rebuild = func() { set(func() {}) }
		host := widgets.NewSizedBox(r.ext.build(bc), widgets.SizedBoxOpts{Width: foundation.Some(600.0), Height: foundation.Some(300.0)})
		client := widgets.NewSizedBox(uitest.OnScreen(3, widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.ext.build(bc) })),
			widgets.SizedBoxOpts{Width: foundation.Some(300.0), Height: foundation.Some(300.0)})
		return widgets.NewRow([]widgets.Widget{host, client}, widgets.RowOpts{})
	})
	r.ts = uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 900, Height: 300}, root)
	r.settle()
	return r
}

func TestTheViewOfAClientHasItsOwnPaneOverTheSameTerminal(t *testing.T) {
	r := twoScreens(t)
	e := r.ext.activeEntry()
	if e == nil {
		t.Fatal("no terminal")
	}
	client := e.views[3]
	if client == nil || client == e.pv || client.term != e.pv.term {
		t.Fatalf("the client's pane is %v, the host's %v: want two panes over one terminal", client, e.pv)
	}
	if e.pv.render == nil || client.render == nil {
		t.Fatalf("a pane is not on screen: host %v, client %v", e.pv.render != nil, client.render != nil)
	}
	if client.cols >= e.pv.cols {
		t.Errorf("the client's pane has %d columns, the host's %d: the client's is narrower", client.cols, e.pv.cols)
	}
	if len(r.ext.shown) != 2 {
		t.Errorf("shown on %v", r.ext.shown)
	}
	// what the shell prints is on both
	e.term.Send([]byte("echo seen-by-both\r"))
	waitText(t, e.term, "seen-by-both")
	r.settle()
	if e.pv.paints == 0 || client.paints == 0 {
		t.Errorf("paints: host %d, client %d", e.pv.paints, client.paints)
	}
	// the two readers of the screen each see all of it: the emulator keeps what changed for one of them only (SnapshotCopy)
	if client.frame.Cursor != e.pv.frame.Cursor || client.frame.Cursor.Y == 0 {
		t.Errorf("the client's pane has the cursor %+v, the host's %+v", client.frame.Cursor, e.pv.frame.Cursor)
	}
}

func TestTheTerminalTakesTheSizeOfThePaneOfTheScreenThatTypedLast(t *testing.T) {
	r := twoScreens(t)
	e := r.ext.activeEntry()
	client := e.views[3]
	cols := func() int { c, _ := e.term.Size(); return c }
	if got := cols(); got != e.pv.cols {
		t.Fatalf("the terminal has %d columns before anybody typed, the host's pane %d", got, e.pv.cols)
	}
	client.userInput([]byte("x"))
	if got := cols(); got != client.cols {
		t.Errorf("the terminal has %d columns after the client typed, its pane %d", got, client.cols)
	}
	if got := r.api.Origins().Recall("terminal:" + itoa(e.term.ID())); got != 3 {
		t.Errorf("the terminal remembers the typist %d, want 3", got)
	}
	e.pv.userInput([]byte("y"))
	if got := cols(); got != e.pv.cols {
		t.Errorf("the terminal has %d columns after the host typed, its pane %d", got, e.pv.cols)
	}
	if got := r.api.Origins().Recall("terminal:" + itoa(e.term.ID())); got != extension.HostOrigin {
		t.Errorf("the terminal remembers the typist %d, want the host's", got)
	}
	// the layout of the pane of a screen that is not the typist does not resize the terminal
	client.layout(gui.Size{Width: 200, Height: 100})
	if got := cols(); got != e.pv.cols {
		t.Errorf("a layout of the client's pane resized the terminal to %d columns", got)
	}
}

func TestTheKeyboardFocusOfAClientIsNotTheHosts(t *testing.T) {
	r := twoScreens(t)
	e := r.ext.activeEntry()
	client := e.views[3]
	e.pv.focusChanged(false) // a new terminal takes the keyboard of the host's window; the client has it on its own screen
	client.focusChanged(true)
	r.settle()
	if got := r.api.ContextKeyFor(3, keyFocus); got != true {
		t.Errorf("terminalFocus on the client's screen is %v", got)
	}
	if got := r.api.ContextKey(keyFocus); got == true {
		t.Error("the client's focus is on the host's screen")
	}
	if r.ext.paneFocus != nil {
		t.Error("the host's pane has the focus")
	}
	// a command of the client acts on the terminal that has its focus
	if got := r.ext.targetFor(3); got != e {
		t.Errorf("the client's target is %v", got)
	}
	client.focusChanged(false)
	r.settle()
	if got := r.api.ContextKeyFor(3, keyFocus); got != false {
		t.Errorf("terminalFocus on the client's screen is %v after it left", got)
	}
}

func TestTheClientsPanesGoWhenItsViewDoes(t *testing.T) {
	r := twoScreens(t)
	e := r.ext.activeEntry()
	if e.views[3] == nil {
		t.Fatal("no pane for the client")
	}
	r.ext.viewGone(3)
	if len(e.views) != 0 || r.ext.screens[3] != nil || r.ext.shown[3] {
		t.Errorf("the client's view left panes %v, screen %v, shown %v", e.views, r.ext.screens[3], r.ext.shown)
	}
	time.Sleep(10 * time.Millisecond)
	if e.pv.render == nil {
		t.Error("the host's pane went with the client's")
	}
}

// The view of a client at its own size (300 wide) beside the host's (600 wide): the same terminal in two panes.
func TestGoldenTheTerminalOnTheScreenOfAClient(t *testing.T) {
	for _, th := range uitest.Themes()[:1] {
		t.Run(th.Name, func(t *testing.T) {
			r := twoScreens(t)
			e := r.ext.activeEntry()
			// the shell has its prompt before the line is typed, or the line is echoed twice, depending on the speed of the machine
			waitText(t, e.term, "ready\n$")
			e.term.Send([]byte("echo shown on both screens\r"))
			// the typed line is echoed first: wait for the output, a line of its own, and for the prompt after it
			waitText(t, e.term, "\nshown on both screens\n$")
			r.settle()
			uitest.MatchesGolden(r.ts, "panel_client_screen_"+th.Name)
		})
	}
}
