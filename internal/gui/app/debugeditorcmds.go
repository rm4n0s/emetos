package app

import (
	"context"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	dbgview "github.com/rm4n0s/emetos/internal/gui/views/debug"
)

// The commands of the debugger in the editor and what the glyph margin does (step 25.5).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointEditorContribution.ts (onMouseDown, the actions of the
// margin), debugEditorActions.ts and debugCommands.ts (TOGGLE_INLINE_BREAKPOINT_ID, JUMP_TO_CURSOR_ID)

// bpsAt are the breakpoints of the file on a line (counted from 0); with a column (byte offset, -1 for none) only the inline breakpoint
// there.
func (d *editorDebug) bpsAt(line, col int) []debug.Breakpoint {
	var out []debug.Breakpoint
	for _, bp := range d.svc.Model().Breakpoints(debug.BreakpointFilter{OriginalPath: d.path}) {
		if bp.Line-1 != line {
			continue
		}
		if col >= 0 {
			if !bp.Inline() || debug.ByteOfColumn(d.lineText(line), bp.Column) != col {
				continue
			}
		}
		out = append(out, bp)
	}
	return out
}

// request runs a request of the debug service on a goroutine of its own, reporting a failure on the screen of the editor.
func (d *editorDebug) request(what string, f func(ctx context.Context) error) { d.s.goDebug(what, f) }

// toggleLine adds a breakpoint to a line, or removes the ones it has.
func (d *editorDebug) toggleLine(line int) {
	if bps := d.bpsAt(line, -1); len(bps) > 0 {
		d.remove(bps)
		return
	}
	if !d.canSet() {
		return
	}
	d.add(line, 0)
}

func (d *editorDebug) add(line, column int) {
	path := d.path
	d.request("addBreakpoint", func(ctx context.Context) error {
		_, err := d.svc.AddBreakpoints(ctx, path, []debug.BreakpointData{{Line: line + 1, Column: column}})
		return err
	})
}

func (d *editorDebug) remove(bps []debug.Breakpoint) {
	ids := make([]string, len(bps))
	for i, bp := range bps {
		ids[i] = bp.ID
	}
	d.request("removeBreakpoint", func(ctx context.Context) error { return d.svc.RemoveBreakpoints(ctx, ids...) })
}

func (d *editorDebug) setEnabled(bps []debug.Breakpoint, enable bool) {
	d.request("enableBreakpoint", func(ctx context.Context) error {
		for _, bp := range bps {
			if err := d.svc.EnableOrDisableBreakpoints(ctx, enable, bp.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// cursorLines are the lines of the cursors of the editor, each once, in order.
func (d *editorDebug) cursorLines() []int {
	var lines []int
	for _, sel := range d.e.view.Cursors().Selections() {
		if !slices.Contains(lines, sel.Active.Line) {
			lines = append(lines, sel.Active.Line)
		}
	}
	sort.Ints(lines)
	return lines
}

// toggleBreakpoint is F9: every line with a cursor gets a breakpoint, or loses the ones it has.
func (d *editorDebug) toggleBreakpoint() {
	for _, line := range d.cursorLines() {
		d.toggleLine(line)
	}
}

// toggleInline is Shift+F9: a breakpoint at the column of the cursor, or none when it has one.
//
// Ported from VS Code: debugCommands.ts (TOGGLE_INLINE_BREAKPOINT_ID)
func (d *editorDebug) toggleInline() {
	p := d.positionOf()
	d.inlinePress(p.Line, p.Col, "")
}

// glyphPress is a click in the glyph margin of a line.
func (d *editorDebug) glyphPress(line int, mods editor.MouseModifiers, middle bool) {
	bps := d.bpsAt(line, -1)
	if len(bps) == 0 && !d.canSet() {
		return
	}
	switch {
	case middle:
		ctx, ok := middleContext(settings.Get[string](d.win.Settings, "debug.gutterMiddleClickAction", ""))
		if ok {
			d.openWidget(line, -1, ctx)
		}
	case mods.Alt:
		d.openWidget(line, -1, editor.BreakpointCondition)
	case mods.Shift:
		if len(bps) > 0 {
			d.setEnabled(bps, !bps[0].Enabled)
		}
	case len(bps) > 0:
		d.remove(bps)
	default:
		d.add(line, 0)
	}
}

// middleContext is the entry of the breakpoint widget that debug.gutterMiddleClickAction opens.
func middleContext(action string) (editor.BreakpointContext, bool) {
	switch action {
	case "logpoint":
		return editor.BreakpointLogMessage, true
	case "conditionalBreakpoint":
		return editor.BreakpointCondition, true
	case "triggeredBreakpoint":
		return editor.BreakpointTriggerPoint, true
	}
	return 0, false
}

// inlinePress is a click on an inline breakpoint or on a place where one can be set (column is a byte offset): the breakpoint goes, or
// comes.
func (d *editorDebug) inlinePress(line, col int, id string) {
	if existing := d.bpsAt(line, col); len(existing) > 0 {
		d.remove(existing)
		return
	}
	if id != "" {
		if bp, ok := d.svc.Model().Breakpoint(id); ok {
			d.remove([]debug.Breakpoint{bp})
			return
		}
	}
	if !d.canSet() {
		return
	}
	d.add(line, debug.ColumnOfByte(d.lineText(line), col))
}

// ---- the breakpoint widget

// openWidget shows the breakpoint widget for the breakpoint on a line (at a column when col is not negative), or for a new one.
//
// Ported from VS Code: breakpointWidget.ts and breakpointEditorContribution.ts (showBreakpointWidget)
func (d *editorDebug) openWidget(line, col int, kind editor.BreakpointContext) {
	if !d.canSet() {
		return
	}
	var existing *debug.Breakpoint
	if bps := d.bpsAt(line, col); len(bps) > 0 {
		existing = &bps[0]
	}
	spec := editor.BreakpointWidgetSpec{Line: line, Context: kind}
	if col >= 0 {
		spec.Inline, spec.Col = true, col
	}
	if existing != nil {
		spec.Condition, spec.HitCondition, spec.LogMessage, spec.TriggeredBy = existing.Condition, existing.HitCondition, existing.LogMessage, existing.TriggeredBy
		spec.Mode = existing.Mode
		if existing.Inline() {
			spec.Inline, spec.Col = true, debug.ByteOfColumn(d.lineText(line), existing.Column)
		}
		if kind == editor.BreakpointCondition {
			// the entry the breakpoint is about: a logpoint opens at its message, a hit count at the count
			switch {
			case existing.LogMessage != "":
				spec.Context = editor.BreakpointLogMessage
			case existing.Condition == "" && existing.HitCondition != "":
				spec.Context = editor.BreakpointHitCount
			case existing.Condition == "" && existing.TriggeredBy != "":
				spec.Context = editor.BreakpointTriggerPoint
			}
		}
	}
	for _, bp := range d.svc.Model().Breakpoints(debug.BreakpointFilter{}) {
		if existing != nil && bp.ID == existing.ID {
			continue
		}
		spec.Triggers = append(spec.Triggers, editor.BreakpointTrigger{
			ID: bp.ID, Label: filepath.Base(bp.Path) + ": " + strconv.Itoa(bp.Line), Description: bp.Path,
		})
	}
	for _, m := range d.svc.Model().BreakpointModes("source") {
		spec.Modes = append(spec.Modes, editor.BreakpointMode{Mode: m.Mode, Label: m.Label, Description: m.Description})
	}
	path := d.path
	column := 0
	if spec.Inline {
		column = debug.ColumnOfByte(d.lineText(line), spec.Col)
	}
	spec.Accept = func(res editor.BreakpointWidgetResult) {
		d.request("breakpointWidget", func(ctx context.Context) error {
			return d.acceptWidget(ctx, path, line, column, existing, spec.Modes, res)
		})
	}
	spec.Closed = func() {
		d.binding.Post(func() {
			if !d.disposed {
				d.e.focus.Request()
			}
		})
	}
	if err := d.e.view.OpenBreakpointWidget(spec); err != nil {
		d.win.Process.Logger.Warn("the breakpoint widget cannot show", "err", err)
	}
}

// acceptWidget sets what the widget accepted on the breakpoint it edits, or on a new one.
func (d *editorDebug) acceptWidget(ctx context.Context, path string, line, column int, existing *debug.Breakpoint, modes []editor.BreakpointMode, res editor.BreakpointWidgetResult) error {
	mode, label := res.Mode, res.ModeLabel
	if existing == nil {
		data := debug.BreakpointData{Line: line + 1, Column: column, Mode: mode, ModeLabel: label}
		if res.Trigger {
			data.TriggeredBy = res.TriggeredBy
		} else {
			data.Condition, data.HitCondition, data.LogMessage = deref(res.Condition), deref(res.HitCondition), deref(res.LogMessage)
		}
		_, err := d.svc.AddBreakpoints(ctx, path, []debug.BreakpointData{data})
		return err
	}
	u := debug.BreakpointUpdate{Condition: res.Condition, HitCondition: res.HitCondition, LogMessage: res.LogMessage}
	if res.Trigger {
		by := res.TriggeredBy
		empty := ""
		u.TriggeredBy, u.Condition, u.HitCondition, u.LogMessage = &by, &empty, &empty, &empty
	} else if existing.TriggeredBy != "" {
		empty := ""
		u.TriggeredBy = &empty
	}
	if mode != "" || len(modes) > 0 {
		u.Mode, u.ModeLabel = &mode, &label
	}
	return d.svc.UpdateBreakpoints(ctx, path, map[string]debug.BreakpointUpdate{existing.ID: u}, false)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---- the debug hover

// evaluate evaluates the expression under the pointer in the focused frame for the debug hover. A value with children gets a tree.
//
// Ported from VS Code: debugHover.ts (doShow)
func (d *editorDebug) evaluate(expr string, _ buffer.Range, done func(*editor.DebugHoverContent)) {
	f := d.svc.Model().Focus()
	if f.Session == nil || f.Frame == nil {
		done(nil)
		return
	}
	sess, frame := f.Session, f.Frame
	ctx := d.ctx
	go func() {
		x, err := sess.Evaluate(ctx, expr, frame, dap.ContextHover)
		var kids []*debug.Variable
		if err == nil && x.HasChildren() {
			kids, _ = x.Children(ctx)
		}
		d.binding.Post(func() {
			if d.disposed || err != nil || !x.Available() {
				done(nil)
				return
			}
			w, h := dbgview.HoverSize(x, len(kids))
			done(&editor.DebugHoverContent{
				Complex: x.HasChildren(), Width: w, Height: h,
				Body: func(widgets.BuildContext) widgets.Widget {
					return dbgview.NewHover(dbgview.HoverOpts{
						Ctx: ctx, Root: x, Post: d.binding.Post,
						ShowTypes: func() bool { return settings.Get[bool](d.win.Settings, "debug.showVariableTypes", "") },
					})
				},
			})
		})
	}()
}

// ---- the commands

// debugEditorHandlers are the handlers of the commands of the editor, for the screen of the shell.
func (s *shellState) debugEditorHandlers(binding *widgets.WidgetsBinding) map[string]command.Handler {
	ed := func(f func(d *editorDebug)) command.Handler {
		return func(command.Context, any) error {
			if e := s.activeEditor(); e != nil && e.dbg != nil {
				f(e.dbg)
			}
			return nil
		}
	}
	widget := func(kind editor.BreakpointContext) command.Handler {
		return ed(func(d *editorDebug) { d.openWidget(d.positionOf().Line, -1, kind) })
	}
	return map[string]command.Handler{
		command.DebugToggleBreakpoint:       ed((*editorDebug).toggleBreakpoint),
		command.DebugToggleInlineBreakpoint: ed((*editorDebug).toggleInline),
		command.DebugConditionalBreakpoint:  widget(editor.BreakpointCondition),
		command.DebugLogPoint:               widget(editor.BreakpointLogMessage),
		command.DebugTriggerPoint:           widget(editor.BreakpointTriggerPoint),
		command.DebugEditBreakpointAction:   widget(editor.BreakpointCondition),
		command.DebugRunToCursor:            ed((*editorDebug).runToCursor),
		command.DebugJumpToCursor:           ed((*editorDebug).jumpToCursor),
		command.DebugSelectionToWatch:       ed((*editorDebug).selectionToWatch),
		command.DebugShowHover: ed(func(d *editorDebug) {
			d.e.view.DebugShowHover(d.positionOf())
		}),
		command.DebugGoToNextBreakpoint:     ed(func(d *editorDebug) { d.goToBreakpoint(binding, 1) }),
		command.DebugGoToPreviousBreakpoint: ed(func(d *editorDebug) { d.goToBreakpoint(binding, -1) }),
		command.DebugCloseExceptionWidget:   ed(func(d *editorDebug) { d.e.view.DebugCloseException() }),
		command.DebugAcceptBreakpointWidget: ed(func(d *editorDebug) { d.e.view.AcceptBreakpointWidget() }),
		command.DebugCloseBreakpointWidget:  ed(func(d *editorDebug) { d.e.view.DebugCloseBreakpointWidget() }),
	}
}

// runToCursor lets the program run to the line of the cursor.
func (d *editorDebug) runToCursor() {
	if d.path == "" {
		return
	}
	path, line := d.path, d.positionOf().Line+1
	d.request("runToCursor", func(ctx context.Context) error { return d.svc.RunTo(ctx, path, line, 0) })
}

// jumpToCursor moves the execution point of the focused thread to the cursor, to the target the adapter names; with several targets the
// user picks one.
//
// Ported from VS Code: debugCommands.ts (JUMP_TO_CURSOR_ID)
func (d *editorDebug) jumpToCursor() {
	f := d.svc.Model().Focus()
	if d.path == "" || f.Session == nil || f.Thread == nil || !f.Session.Capabilities().SupportsGotoTargetsRequest {
		return
	}
	p := d.positionOf()
	line, col := p.Line+1, debug.ColumnOfByte(d.lineText(p.Line), p.Col)
	sess, thread, path := f.Session, f.Thread, d.path
	ctx := d.s.debugCtx()
	go func() {
		targets, err := sess.GotoTargets(ctx, path, line, col)
		d.win.onUI(func() {
			if err != nil || d.disposed {
				if err != nil && ctx.Err() == nil {
					d.win.Notify.Error("Emetos", describe(err))
				}
				return
			}
			jump := func(id int) {
				d.request("jumpToCursor", func(ctx context.Context) error { return sess.Goto(ctx, thread.ID, id) })
			}
			switch len(targets) {
			case 0:
				d.win.Notify.Warn("Emetos", "No executable code is associated at the current cursor position.")
			case 1:
				jump(targets[0].ID)
			default:
				var items []quickinput.Item
				for _, t := range targets {
					id := t.ID
					items = append(items, quickinput.Item{ID: strconv.Itoa(id), Label: t.Label, Accept: func(quickinput.KeyMods) { jump(id) }})
				}
				d.win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select where to jump to", Items: items})
			}
		})
	}()
}

// selectionToWatch adds the selected text, or the expression under the cursor, to the Watch view.
//
// Ported from VS Code: debugEditorActions.ts (SelectionToWatchExpressionsAction)
func (d *editorDebug) selectionToWatch() {
	sel := d.e.view.Cursors().Primary().Selection
	text := ""
	if !sel.IsEmpty() {
		text = d.e.doc.Snapshot().Text(sel.Range())
	} else {
		p := sel.Active
		line := d.lineText(p.Line)
		col := len([]rune(line[:min(p.Col, len(line))])) + 1
		text, _, _ = debug.EvaluatableExpression(line, col)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	d.svc.Model().AddWatchExpression(text)
	d.s.showDebug(d.binding, command.ViewDebugWatch)
}

// goToBreakpoint moves the cursor to the next (step 1) or the previous (-1) breakpoint that is on, in this file and then the others,
// round the end.
//
// Ported from VS Code: debugEditorActions.ts (GoToNextBreakpointAction, GoToPreviousBreakpointAction)
func (d *editorDebug) goToBreakpoint(binding *widgets.WidgetsBinding, step int) {
	var all []debug.Breakpoint
	for _, bp := range d.svc.Model().Breakpoints(debug.BreakpointFilter{}) {
		if bp.Enabled && bp.Line > 0 {
			all = append(all, bp)
		}
	}
	if len(all) == 0 {
		return
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Path != all[j].Path {
			return all[i].Path < all[j].Path
		}
		return all[i].Line < all[j].Line
	})
	cur := d.positionOf().Line + 1
	var target debug.Breakpoint
	found := false
	less := func(bp debug.Breakpoint) bool { // before the cursor
		return bp.Path < d.path || bp.Path == d.path && bp.Line <= cur
	}
	if step > 0 {
		for _, bp := range all {
			if !less(bp) {
				target, found = bp, true
				break
			}
		}
		if !found {
			target = all[0]
		}
	} else {
		for i := len(all) - 1; i >= 0; i-- {
			if bp := all[i]; bp.Path < d.path || bp.Path == d.path && bp.Line < cur {
				target, found = bp, true
				break
			}
		}
		if !found {
			target = all[len(all)-1]
		}
	}
	d.s.openPathAt(binding, target.Path, target.Line, max(target.Column, 1), openOptions{TakeFocus: true})
}

// ---- the menu of the glyph margin

// glyphMenu is the menu of the breakpoints of a line, opened with the secondary button in the margin or on an inline breakpoint.
//
// Ported from VS Code: breakpointEditorContribution.ts (the context menu of onMouseDown: getContextMenuActions)
func (d *editorDebug) glyphMenu(t editor.DebugMenuTarget) []kit.MenuItem {
	col := t.Col
	bps := d.bpsAt(t.Line, col)
	var items []kit.MenuItem
	item := func(label string, run func()) {
		// the menu gives the focus back to the editor when it is gone, two frames after the choice: what takes the focus (the breakpoint
		// widget) waits for that
		items = append(items, kit.MenuItem{Label: label, OnRun: func() {
			d.s.afterFrame(d.binding, func() { d.s.afterFrame(d.binding, run) })
		}})
	}
	if len(bps) > 0 {
		bp := bps[0]
		kind := "Breakpoint"
		if bp.IsLogpoint() {
			kind = "Logpoint"
		}
		item("Remove "+kind, func() { d.remove(bps) })
		item("Edit "+kind+"...", func() { d.openWidget(t.Line, col, editor.BreakpointCondition) })
		if bp.Enabled {
			item("Disable "+kind, func() { d.setEnabled(bps, false) })
		} else {
			item("Enable "+kind, func() { d.setEnabled(bps, true) })
		}
	} else if d.canSet() {
		label := "Add Breakpoint"
		if col >= 0 {
			label = "Add Inline Breakpoint"
		}
		item(label, func() { d.inlineOrLine(t.Line, col) })
		item("Add Conditional Breakpoint...", func() { d.openWidget(t.Line, col, editor.BreakpointCondition) })
		item("Add Logpoint...", func() { d.openWidget(t.Line, col, editor.BreakpointLogMessage) })
		item("Add Triggered Breakpoint...", func() { d.openWidget(t.Line, col, editor.BreakpointTriggerPoint) })
	}
	if d.svc.State() == debug.Stopped && d.path != "" {
		if len(items) > 0 {
			items = append(items, kit.SeparatorItem())
		}
		path, line := d.path, t.Line+1
		item("Run to Line", func() {
			d.request("runToLine", func(ctx context.Context) error { return d.svc.RunTo(ctx, path, line, 0) })
		})
	}
	return items
}

// inlineOrLine sets a breakpoint on a line, or at a column of it.
func (d *editorDebug) inlineOrLine(line, col int) {
	if col < 0 {
		d.add(line, 0)
		return
	}
	d.add(line, debug.ColumnOfByte(d.lineText(line), col))
}
