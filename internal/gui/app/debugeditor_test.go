package app

import (
	"context"
	"encoding/json"
	"image"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The debugger in the editors of the window (step 25.5): the glyphs and the commands of the margin, the stopped line, the inline values, the
// hover and the exception widget, against the fake adapter of daptest. The goldens of the drawing are in the editor package; here the
// editor follows the model of the window.

// mainEditor is the editor of main.go of the rig (the first file that started open).
func (r *debugRig) mainEditor() *textEditor {
	r.t.Helper()
	e := r.win.shell.activeEditor()
	if e == nil || e.dbg == nil {
		r.t.Fatal("the active editor has no debugger")
	}
	return e
}

// cursorAt puts the cursor of the editor at a line (from 0).
func (r *debugRig) cursorAt(e *textEditor, line int) {
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: line, Col: 0})})
	r.pump()
}

func (r *debugRig) bps() []debug.Breakpoint {
	return r.svc().Model().Breakpoints(debug.BreakpointFilter{})
}

func TestF9TogglesABreakpointAndTheEditorShowsIt(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	r.cursorAt(e, 20)
	if !sendChord(r.ts, keybinding.Chord{Key: keybinding.F9}) {
		t.Fatal("F9 is bound to nothing in the editor")
	}
	r.waitFor("the breakpoint", func() bool { return len(r.bps()) == 1 })
	if got := r.bps()[0]; got.Line != 21 || filepath.Base(got.Path) != "main.go" {
		t.Errorf("breakpoint = %s:%d, want main.go:21", got.Path, got.Line)
	}
	r.waitFor("the mark in the editor", func() bool { return len(e.view.DebugBreakpoints()) == 1 })
	if m := e.view.DebugBreakpoints()[0]; m.Line != 20 || m.Kind != editor.DebugGlyphBreakpoint || !m.Enabled {
		t.Errorf("mark = %+v", m)
	}
	if !sendChord(r.ts, keybinding.Chord{Key: keybinding.F9}) {
		t.Fatal("F9 is bound to nothing in the editor the second time")
	}
	r.waitFor("the breakpoint to go", func() bool { return len(r.bps()) == 0 })
	r.waitFor("the mark to go", func() bool { return len(e.view.DebugBreakpoints()) == 0 })
	_ = context.Background
	_ = daptest.Script{}
}

// exceptionFolder makes the folder of the exception fixture of the proof (testdata/proof/25.5/project): src/loader.go and a script that
// stops on a panic at its line 18.
func exceptionFolder(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	copyTree(t, filepath.Join(testutil.RepoRoot(t), "testdata", "proof", "25.5", "project"), folder)
	return folder
}

// stoppedOnException opens src/loader.go, sets breakpoints at its lines 14 (F9) and 16, starts, and waits for the stop.
func stoppedOnException(t *testing.T, o rigOpts) (*debugRig, *textEditor) {
	t.Helper()
	o.open = []string{filepath.Join("src", "loader.go")}
	r := newDebugRigWith(t, exceptionFolder(t), o)
	e := r.mainEditor()
	r.cursorAt(e, 13)
	r.run(command.DebugToggleBreakpoint, nil)
	r.run("dev.debug.breakpoint", map[string]any{"path": "src/loader.go", "line": float64(16)})
	r.waitFor("both breakpoints", func() bool { return len(r.bps()) == 2 && len(e.view.DebugBreakpoints()) == 2 })
	r.run(command.DebugStartCommand, nil)
	r.waitFor("the stop", func() bool { return r.win.Keys.Get("debugState") == "stopped" && r.svc().Model().Focus().Frame != nil })
	return r, e
}

func TestAStopShowsTheFrameTheValuesAndTheExceptionInTheEditor(t *testing.T) {
	r, e := stoppedOnException(t, rigOpts{})
	r.waitFor("the stopped line", func() bool { return len(e.view.DebugFrames()) == 1 })
	if f := e.view.DebugFrames()[0]; f.Line != 17 || !f.Top || !f.Glyph {
		t.Errorf("frame = %+v, want the top frame at line 18", f)
	}
	r.waitFor("the exception widget", func() bool { return e.view.DebugExceptionOpen() })
	if ex, _ := e.view.DebugExceptionInfo(); ex.Description != "index out of range [3] with length 3" || ex.ID != "runtime error" {
		t.Errorf("exception = %+v", ex)
	}
	if got := e.view.InlineValues(); len(got) == 0 {
		t.Error("no inline values")
	}
	// the exception widget goes with Escape, and does not come back for the same stop
	r.run(command.DebugCloseExceptionWidget, nil)
	if e.view.DebugExceptionOpen() {
		t.Error("the exception widget stays after the command")
	}
	r.pump()
	r.svc().FocusStackFrame(r.svc().Model().Focus().Frame, nil, nil)
	r.pump()
	if e.view.DebugExceptionOpen() {
		t.Error("the exception widget came back for the same stop")
	}
}

// glyphPoint is the middle of the glyph margin on a line (from 0) of an editor, in the window.
func (r *debugRig) glyphPoint(e *textEditor, line int) ui.Offset {
	r.t.Helper()
	vm := e.view
	row := vm.RowOfPosition(buffer.Position{Line: line})
	top := r.ts.TopLeft(gluttertest.ByType[*editor.Editor]())
	h := vm.Options().LineHeight
	return ui.Offset{Dx: top.Dx + h/2, Dy: top.Dy + vm.Projection().RowTop(row) - vm.ScrollTop() + h/2}
}

func (r *debugRig) mouse(buttons ...int) gluttertest.GestureOpts {
	b := gestures.PrimaryButton
	if len(buttons) > 0 {
		b = buttons[0]
	}
	return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(b)}
}

func TestClicksInTheGlyphMarginSetAndRemoveBreakpoints(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	r.ts.TapAt(r.glyphPoint(e, 11), r.mouse())
	r.waitFor("the breakpoint of a click", func() bool { return len(r.bps()) == 1 && len(e.view.DebugBreakpoints()) == 1 })
	if got := r.bps()[0]; got.Line != 12 || !got.Enabled {
		t.Errorf("breakpoint = %+v, want line 12 on", got)
	}
	r.ts.TapAt(r.glyphPoint(e, 11), r.mouse())
	r.waitFor("the breakpoint to go", func() bool { return len(r.bps()) == 0 && len(e.view.DebugBreakpoints()) == 0 })
}

func TestTheModifiersAndTheMiddleButtonOfTheGlyphMargin(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	d := e.dbg
	d.glyphPress(11, editor.MouseModifiers{}, false)
	r.waitFor("the breakpoint", func() bool { return len(r.bps()) == 1 })
	// Shift turns it off, and on again
	d.glyphPress(11, editor.MouseModifiers{Shift: true}, false)
	r.waitFor("the breakpoint to be off", func() bool { return len(r.bps()) == 1 && !r.bps()[0].Enabled })
	r.waitFor("the mark to be off", func() bool { m := e.view.DebugBreakpoints(); return len(m) == 1 && !m[0].Enabled })
	d.glyphPress(11, editor.MouseModifiers{Shift: true}, false)
	r.waitFor("the breakpoint to be on", func() bool { return r.bps()[0].Enabled })
	// Alt opens the widget on the breakpoint, at its entry for a condition
	d.glyphPress(11, editor.MouseModifiers{Alt: true}, false)
	if !e.view.BreakpointWidgetOpen() || e.view.BreakpointWidgetContext() != editor.BreakpointCondition {
		t.Fatalf("Alt-click: the widget is open %v, in the entry %v", e.view.BreakpointWidgetOpen(), e.view.BreakpointWidgetContext())
	}
	e.view.DebugCloseBreakpointWidget()
	// the middle button does what debug.gutterMiddleClickAction says: a logpoint by default
	d.glyphPress(14, editor.MouseModifiers{}, true)
	if !e.view.BreakpointWidgetOpen() || e.view.BreakpointWidgetContext() != editor.BreakpointLogMessage {
		t.Errorf("middle click: the widget is open %v, in the entry %v", e.view.BreakpointWidgetOpen(), e.view.BreakpointWidgetContext())
	}
	if len(r.bps()) != 1 {
		t.Errorf("the middle click set a breakpoint before the widget was accepted: %v", r.bps())
	}
}

func TestTheBreakpointWidgetSetsTheConditionAndTheLogMessage(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	r.cursorAt(e, 13)
	r.run(command.DebugConditionalBreakpoint, nil)
	if !e.view.BreakpointWidgetOpen() {
		t.Fatal("the command opened no widget")
	}
	e.view.BreakpointWidgetSetText("p > 4")
	if !sendChord(r.ts, keybinding.Chord{Key: keybinding.Enter}) {
		e.view.AcceptBreakpointWidget()
	}
	r.waitFor("the conditional breakpoint", func() bool { return len(r.bps()) == 1 && r.bps()[0].Condition == "p > 4" })
	r.waitFor("the widget to close", func() bool { return !e.view.BreakpointWidgetOpen() })
	r.waitFor("the conditional mark", func() bool {
		m := e.view.DebugBreakpoints()
		return len(m) == 1 && m[0].Kind == editor.DebugGlyphConditional && strings.Contains(m[0].Hover, "Condition: p \\> 4")
	})
	// the logpoint: the same breakpoint, its entry is now the log message
	r.run(command.DebugLogPoint, nil)
	if got := e.view.BreakpointWidgetContext(); got != editor.BreakpointLogMessage {
		t.Errorf("the widget of the logpoint is in the entry %v", got)
	}
	e.view.BreakpointWidgetSetText("p is {p}")
	e.view.AcceptBreakpointWidget()
	r.waitFor("the logpoint", func() bool { return len(r.bps()) == 1 && r.bps()[0].LogMessage == "p is {p}" })
	r.waitFor("the log mark", func() bool { m := e.view.DebugBreakpoints(); return len(m) == 1 && m[0].Kind == editor.DebugGlyphLog })
	// Escape closes the widget and sets nothing
	r.run(command.DebugEditBreakpointAction, nil)
	e.view.BreakpointWidgetSetText("changed")
	r.run(command.DebugCloseBreakpointWidget, nil)
	if e.view.BreakpointWidgetOpen() || r.bps()[0].LogMessage != "p is {p}" {
		t.Errorf("Escape: the widget is open %v and the log message %q", e.view.BreakpointWidgetOpen(), r.bps()[0].LogMessage)
	}
}

func TestBreakpointsFollowTheEditsOfTheDocument(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	r.cursorAt(e, 20)
	r.run(command.DebugToggleBreakpoint, nil)
	r.waitFor("the breakpoint", func() bool { return len(r.bps()) == 1 && len(e.view.DebugBreakpoints()) == 1 })
	at := buffer.Position{Line: 3, Col: 0}
	if _, err := e.doc.Edit([]document.Edit{{Range: buffer.NewRange(at, at), Text: "// one\n// two\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if m := e.view.DebugBreakpoints(); len(m) != 1 || m[0].Line != 22 {
		t.Errorf("the mark is %+v, want line 23 (index 22) at once", m)
	}
	r.waitFor("the model to follow", func() bool { return len(r.bps()) == 1 && r.bps()[0].RequestedLine == 23 })
	r.waitFor("the mark to stay", func() bool { m := e.view.DebugBreakpoints(); return len(m) == 1 && m[0].Line == 22 })
}

func TestRunToCursorStartsTheSessionAndStopsAtTheCursor(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	r.cursorAt(e, 20)
	r.run(command.DebugRunToCursor, nil)
	r.waitState("stopped")
	// the breakpoint of the run is gone when the program stops
	r.waitFor("the temporary breakpoint to go", func() bool { return len(r.bps()) == 0 })
}

func TestJumpToCursorAsksTheAdapterForTargets(t *testing.T) {
	steps := joinSteps(stopSteps("breakpoint", "23.75"),
		[]daptest.Step{daptest.Reply("gotoTargets", map[string]any{"targets": []map[string]any{{"id": 7, "label": "line 23", "line": 23}}}),
			daptest.Reply("goto", nil)}, stopSteps("goto", "23.75"))
	r := newDebugRigWith(t, debugFolder(t, func(def daptest.Script) daptest.Script {
		out := scriptOf(steps...)(def)
		for i := range out {
			if out[i].Command == "initialize" {
				caps := map[string]any{}
				if err := json.Unmarshal(out[i].Body, &caps); err != nil {
					t.Fatal(err)
				}
				caps["supportsGotoTargetsRequest"] = true
				body, err := json.Marshal(caps)
				if err != nil {
					t.Fatal(err)
				}
				out[i].Body = body
			}
		}
		return out
	}), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	r.stopped()
	r.cursorAt(e, 22)
	r.waitFor("the key", func() bool { return r.win.Keys.Get("jumpToCursorSupported") == true })
	// the views have loaded the variables: the script answers the requests of a stop in order, and the jump comes after them
	r.waitFor("the variables", func() bool { return r.win.shell.debug.panes.Variables.Model().Len() >= 4 })
	r.run(command.DebugJumpToCursor, nil)
	r.waitFor("the stop that follows the jump", func() bool {
		th := r.svc().Model().Focus().Thread
		d := (*debug.StoppedDetails)(nil)
		if th != nil {
			d = th.StoppedDetails()
		}
		return d != nil && d.Reason == "goto"
	})
}

func TestAClientSetsABreakpointInItsEditorAndTheHostShowsIt(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)), rigOpts{open: []string{"main.go"}})
	host := r.mainEditor()
	v, err := r.win.makeHeadlessView(extension.Size{Width: 900, Height: 560}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	var frames atomic.Int64
	v.OnFrame(func(*image.RGBA) { frames.Add(1) })
	r.waitFor("the view to draw", func() bool { return frames.Load() > 0 && v.ws.shell != nil && v.ws.shell.debug.panes != nil })
	client := v.ws
	client.shell.openPathAt(client.shell.binding, filepath.Join(r.folder, "main.go"), 21, 1, openOptions{TakeFocus: true})
	r.waitFor("the client's editor", func() bool { e := client.shell.activeEditor(); return e != nil && e.dbg != nil })
	mine := client.shell.activeEditor()
	if mine.doc != host.doc {
		t.Fatal("the client's editor shows another document than the host's")
	}
	r.cursorAt(mine, 20)
	// F9 in the editor of the client: its own command handler
	if err := client.Commands.Run(command.Context{Ctx: context.Background(), Keys: client.Keys, Source: command.SourceAPI}, command.DebugToggleBreakpoint, nil); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the host's model", func() bool { return len(r.bps()) == 1 && r.bps()[0].Line == 21 })
	r.waitFor("both editors", func() bool { return len(host.view.DebugBreakpoints()) == 1 && len(mine.view.DebugBreakpoints()) == 1 })
	// the glyph of the host's screen is drawn: the mark is in the host's editor at the line of the client's press
	if m := host.view.DebugBreakpoints()[0]; m.Line != 20 || !m.Verified {
		t.Errorf("the host's mark = %+v", m)
	}
	// a session started by the client stops, and the stopped line and the values show on both screens
	if err := client.Commands.Run(command.Context{Ctx: context.Background(), Keys: client.Keys, Source: command.SourceAPI}, command.DebugStartCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the stop on both screens", func() bool { return len(host.view.DebugFrames()) == 1 && len(mine.view.DebugFrames()) == 1 })
	r.waitFor("the values on both screens", func() bool { return len(host.view.InlineValues()) > 0 && len(mine.view.InlineValues()) > 0 })
	if h, c := host.view.InlineValues(), mine.view.InlineValues(); len(h) != len(c) {
		t.Errorf("the host has %d lines of values and the client %d", len(h), len(c))
	}
	// the client removes the breakpoint with F9 again
	if err := client.Commands.Run(command.Context{Ctx: context.Background(), Keys: client.Keys, Source: command.SourceAPI}, command.DebugToggleBreakpoint, nil); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the breakpoint to go on both screens", func() bool {
		return len(r.bps()) == 0 && len(host.view.DebugBreakpoints()) == 0 && len(mine.view.DebugBreakpoints()) == 0
	})
}

func TestShiftF9TogglesAnInlineBreakpointAtTheCursor(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 20, Col: 9})})
	r.pump()
	chord := keybinding.Chord{Key: keybinding.F9, Shift: true}
	if !sendChord(r.ts, chord) {
		t.Fatal("Shift+F9 is bound to nothing in the editor")
	}
	r.waitFor("the inline breakpoint", func() bool { return len(r.bps()) == 1 && r.bps()[0].Column > 0 })
	if got := r.bps()[0]; got.Line != 21 || got.Column != 10 {
		t.Errorf("breakpoint = line %d column %d, want line 21 column 10", got.Line, got.Column)
	}
	r.waitFor("the inline mark", func() bool { m := e.view.DebugBreakpoints(); return len(m) == 1 && m[0].Inline && m[0].Col == 9 })
	if !sendChord(r.ts, chord) {
		t.Fatal("Shift+F9 is bound to nothing the second time")
	}
	r.waitFor("the inline breakpoint to go", func() bool { return len(r.bps()) == 0 && len(e.view.DebugBreakpoints()) == 0 })
}

func TestTheGlyphMarginMenuOffersTheBreakpointActions(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{open: []string{"main.go"}})
	e := r.mainEditor()
	r.ts.TapAt(r.glyphPoint(e, 11), r.mouse(gestures.SecondaryButton))
	r.pump()
	for _, label := range []string{"Add Breakpoint", "Add Conditional Breakpoint...", "Add Logpoint...", "Add Triggered Breakpoint..."} {
		if !r.ts.Exists(gluttertest.ByText(label)) {
			t.Errorf("the menu has no %q", label)
		}
	}
	// the item runs a command: the breakpoint is set
	r.ts.Tap(gluttertest.ByText("Add Breakpoint"))
	r.waitFor("the breakpoint of the menu", func() bool { return len(r.bps()) == 1 && r.bps()[0].Line == 12 })
	// and a line with a breakpoint has the others
	r.ts.TapAt(r.glyphPoint(e, 11), r.mouse(gestures.SecondaryButton))
	r.pump()
	for _, label := range []string{"Remove Breakpoint", "Edit Breakpoint...", "Disable Breakpoint"} {
		if !r.ts.Exists(gluttertest.ByText(label)) {
			t.Errorf("the menu of a breakpoint has no %q", label)
		}
	}
}
