package editor

import (
	"math"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The two widgets the debugger puts in view zones of the editor (step 25.5): the breakpoint widget, where the condition, the hit count,
// the log message or the breakpoint to wait for is typed, and the exception widget, which shows the exception the program stopped on.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointWidget.ts and exceptionWidget.ts (and their css)

// ---- the breakpoint widget

// BreakpointContext is what the breakpoint widget edits; the numbers are the entries of its dropdown.
type BreakpointContext int

// The kinds the breakpoint widget edits.
const (
	// BreakpointCondition is an expression that must be true for the breakpoint to stop (the first entry, "Expression").
	BreakpointCondition BreakpointContext = iota
	// BreakpointHitCount is a condition on the number of hits ("Hit Count").
	BreakpointHitCount
	// BreakpointLogMessage is a message to log instead of stopping ("Log Message").
	BreakpointLogMessage
	// BreakpointTriggerPoint is another breakpoint to wait for ("Wait for Breakpoint").
	BreakpointTriggerPoint
)

// BreakpointTrigger is a breakpoint a new one can wait for.
type BreakpointTrigger struct {
	ID, Label, Description string
}

// BreakpointMode is a mode an adapter offers for source breakpoints.
type BreakpointMode struct {
	Mode, Label, Description string
}

// BreakpointWidgetSpec describes a breakpoint widget to show.
type BreakpointWidgetSpec struct {
	// Line is counted from 0; Inline and Col say that the breakpoint is at a column (the byte offset).
	Line, Col int
	Inline    bool
	// Context is the entry shown first.
	Context BreakpointContext
	// Condition, HitCondition, LogMessage and TriggeredBy are the values the breakpoint has now (the empty ones for a new one).
	Condition, HitCondition, LogMessage, TriggeredBy string
	// Triggers are the breakpoints that can be waited for and Modes the adapter's modes (the dropdown of modes shows with two or more).
	Triggers []BreakpointTrigger
	Modes    []BreakpointMode
	Mode     string
	// Accept is called with what was typed when Enter is pressed (or OK, for a trigger).
	Accept func(BreakpointWidgetResult)
	// Closed is called when the widget goes, accepted or not.
	Closed func()
}

// BreakpointWidgetResult is what the breakpoint widget accepted. A nil member is left as it is (or not set).
type BreakpointWidgetResult struct {
	Condition, HitCondition, LogMessage *string
	// Trigger says that the entry was "Wait for Breakpoint": TriggeredBy is the breakpoint chosen, empty for none, and the other three
	// are nil.
	Trigger     bool
	TriggeredBy string
	// Mode and ModeLabel are the adapter's mode chosen, when it has more than one.
	Mode, ModeLabel string
}

// breakpointWidget is the state of the breakpoint widget of a view model.
type breakpointWidget struct {
	spec    BreakpointWidgetSpec
	zone    ZoneID
	context BreakpointContext
	// condition, hits and log are what was typed for each entry, kept when the entry changes.
	condition, hits, log string
	trigger              int
	mode                 int
	text                 *widgets.TextEditingController
	node                 *widgets.FocusNode
	changed              *foundation.ChangeNotifier
}

// breakpointWidgetRows is the height of the widget in rows of the editor (VS Code: one line of the input plus one).
const breakpointWidgetRows = 2

// OpenBreakpointWidget shows the breakpoint widget in a zone under the line of the breakpoint, with the keyboard in its input; one
// that is open goes first.
func (vm *ViewModel) OpenBreakpointWidget(spec BreakpointWidgetSpec) error {
	vm.DebugCloseBreakpointWidget()
	w := &breakpointWidget{
		spec: spec, context: spec.Context,
		condition: spec.Condition, hits: spec.HitCondition, log: spec.LogMessage,
		text: widgets.NewTextEditingController(""), node: widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "BreakpointWidget"}),
		changed: foundation.NewChangeNotifier(),
	}
	for i, t := range spec.Triggers {
		if t.ID == spec.TriggeredBy {
			w.trigger = i + 1
		}
	}
	if w.trigger == 0 && spec.TriggeredBy == "" && len(spec.Triggers) > 0 {
		w.trigger = 1
	}
	for i, m := range spec.Modes {
		if m.Mode == spec.Mode {
			w.mode = i + 1
		}
	}
	w.syncText()
	id, err := vm.AddZone(ZoneSpec{AfterLine: spec.Line + 1, Rows: breakpointWidgetRows, ShowInHiddenAreas: true, Widget: w.body(vm)})
	if err != nil {
		w.text.Dispose()
		w.node.Dispose()
		return err
	}
	w.zone = id
	vm.dbg.bpw = w
	if f := vm.dbg.focusInput; f != nil {
		f(w.node)
	}
	vm.notifyKeys()
	return nil
}

// BreakpointWidgetOpen tells whether the breakpoint widget shows (the key breakpointWidgetVisible).
func (vm *ViewModel) BreakpointWidgetOpen() bool { return vm.dbg.bpw != nil }

// BreakpointWidgetFocused tells whether the input of the breakpoint widget has the keyboard (the key inBreakpointWidget).
func (vm *ViewModel) BreakpointWidgetFocused() bool {
	w := vm.dbg.bpw
	return w != nil && w.node.HasFocus()
}

// BreakpointWidgetContext is the entry the breakpoint widget shows now.
func (vm *ViewModel) BreakpointWidgetContext() BreakpointContext {
	if w := vm.dbg.bpw; w != nil {
		return w.context
	}
	return BreakpointCondition
}

// BreakpointWidgetSetText types into the input of the breakpoint widget (for the tests and the commands).
func (vm *ViewModel) BreakpointWidgetSetText(text string) {
	if w := vm.dbg.bpw; w != nil {
		w.text.SetText(text)
		w.remember()
	}
}

// BreakpointWidgetSetContext picks the entry of the dropdown of the breakpoint widget.
func (vm *ViewModel) BreakpointWidgetSetContext(c BreakpointContext) {
	if w := vm.dbg.bpw; w != nil {
		w.remember()
		w.context = c
		w.syncText()
		w.changed.NotifyListeners()
	}
}

// DebugCloseBreakpointWidget closes the breakpoint widget without accepting it.
func (vm *ViewModel) DebugCloseBreakpointWidget() {
	w := vm.dbg.bpw
	if w == nil {
		return
	}
	vm.dbg.bpw = nil
	if err := vm.RemoveZone(w.zone); err != nil {
		vm.dbg.bpw = nil
	}
	w.text.Dispose()
	w.node.Dispose()
	vm.notifyKeys()
	if f := vm.dbg.focus; f != nil {
		f()
	}
	if w.spec.Closed != nil {
		w.spec.Closed()
	}
}

// AcceptBreakpointWidget accepts what is typed in the breakpoint widget and closes it.
//
// Ported from VS Code: breakpointWidget.ts (close(true))
func (vm *ViewModel) AcceptBreakpointWidget() {
	w := vm.dbg.bpw
	if w == nil {
		return
	}
	w.remember()
	var res BreakpointWidgetResult
	str := func(s string) *string { return &s }
	if w.condition != "" || w.context == BreakpointCondition {
		res.Condition = str(w.condition)
	}
	if w.hits != "" || w.context == BreakpointHitCount {
		res.HitCondition = str(w.hits)
	}
	if w.log != "" || w.context == BreakpointLogMessage {
		res.LogMessage = str(w.log)
	}
	if m := w.mode; m > 0 && m <= len(w.spec.Modes) {
		res.Mode, res.ModeLabel = w.spec.Modes[m-1].Mode, w.spec.Modes[m-1].Label
	}
	if w.context == BreakpointTriggerPoint {
		// a trigger point takes no other condition
		res.Condition, res.HitCondition, res.LogMessage = nil, nil, nil
		res.Trigger = true
		if t := w.trigger; t > 0 && t <= len(w.spec.Triggers) {
			res.TriggeredBy = w.spec.Triggers[t-1].ID
		}
	}
	accept := w.spec.Accept
	vm.DebugCloseBreakpointWidget()
	if accept != nil {
		accept(res)
	}
}

// remember keeps what is typed for the entry that is shown (a trigger point has no text).
func (w *breakpointWidget) remember() {
	switch w.context {
	case BreakpointLogMessage:
		w.log = w.text.Text()
	case BreakpointHitCount:
		w.hits = w.text.Text()
	case BreakpointCondition:
		w.condition = w.text.Text()
	}
}

// syncText puts the text of the shown entry in the input.
func (w *breakpointWidget) syncText() {
	var text string
	switch w.context {
	case BreakpointLogMessage:
		text = w.log
	case BreakpointHitCount:
		text = w.hits
	case BreakpointCondition:
		text = w.condition
	}
	w.text.SetText(text)
	w.text.SetSelection(services.NewTextSelectionCollapsed(len(text)))
}

// placeholder is the text of the empty input, after the placeholders of VS Code.
func (w *breakpointWidget) placeholder() string {
	switch w.context {
	case BreakpointLogMessage:
		return "Message to log when breakpoint is hit. Expressions within {} are interpolated. 'Enter' to accept, 'Escape' to cancel."
	case BreakpointHitCount:
		return "Break when hit count condition is met. 'Enter' to accept, 'Escape' to cancel."
	}
	return "Break when expression evaluates to true. 'Enter' to accept, 'Escape' to cancel."
}

// body is the widget of the zone: the dropdown of the entries and the input (or the dropdown of the breakpoints to wait for and OK).
func (w *breakpointWidget) body(vm *ViewModel) widgets.Widget {
	return widgets.NewListenableBuilder(w.changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		colors := kit.ThemeOf(bc)
		entries := []kit.DropdownOption{{Label: "Expression"}, {Label: "Hit Count"}, {Label: "Log Message"}, {Label: "Wait for Breakpoint"}}
		kind := kit.NewDropdown(entries, kit.DropdownOpts{
			Selected: int(w.context), Width: foundation.Some(113.0), Rows: 4,
			OnChanged: func(i int) { vm.BreakpointWidgetSetContext(BreakpointContext(i)) },
		})
		row := []widgets.Widget{widgets.NewSizedBox(kind, widgets.SizedBoxOpts{Width: foundation.Some(113.0)}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)})}
		if len(w.spec.Modes) > 1 {
			modes := []kit.DropdownOption{{Label: "Mode", Disabled: true}}
			for _, m := range w.spec.Modes {
				modes = append(modes, kit.DropdownOption{Label: m.Label, Description: m.Description})
			}
			row = append(row, widgets.NewSizedBox(kit.NewDropdown(modes, kit.DropdownOpts{
				Selected: w.mode, Width: foundation.Some(113.0), Rows: 6, OnChanged: func(i int) { w.mode = i; w.changed.NotifyListeners() },
			}), widgets.SizedBoxOpts{Width: foundation.Some(113.0)}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
		}
		if w.context == BreakpointTriggerPoint {
			options := []kit.DropdownOption{{Label: "None", Disabled: true}}
			for _, t := range w.spec.Triggers {
				options = append(options, kit.DropdownOption{Label: t.Label, Description: t.Description})
			}
			row = append(row,
				widgets.NewExpanded(kit.NewDropdown(options, kit.DropdownOpts{
					Selected: w.trigger, Rows: 8, OnChanged: func(i int) { w.trigger = i; w.changed.NotifyListeners() },
				})),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
				kit.NewButton("OK", kit.ButtonOpts{OnPressed: vm.AcceptBreakpointWidget}),
			)
		} else {
			input := kit.NewInputBox(w.text, w.node, kit.InputBoxOpts{
				Placeholder: w.placeholder(), Autofocus: true, Monospace: true, OnSubmit: func(string) { vm.AcceptBreakpointWidget() },
				OnChanged: func(string) { w.remember() }, SelectAllOnFocus: foundation.Some(false),
			})
			row = append(row, widgets.NewExpanded(input))
		}
		line := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 8), widgets.NewCenter(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})))
		framed := widgets.NewContainer(line, widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(colors.EditorBackground),
			Border: painting.Border{
				Top:    painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.FocusBorder), Width: foundation.Some(1.0)}),
				Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.FocusBorder), Width: foundation.Some(1.0)}),
			},
		})})
		return widgets.NewFocus(framed, widgets.FocusOpts{
			CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true),
			OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
				if _, down := e.(*services.KeyDownEvent); !down {
					return widgets.KeyEventResultIgnored
				}
				if e.LogicalKey() == services.LogicalKeyboardKeyEscape {
					vm.DebugCloseBreakpointWidget()
					return widgets.KeyEventResultHandled
				}
				return widgets.KeyEventResultIgnored
			},
		})
	}, nil)
}

// ---- the exception widget

// DebugException is the exception the program stopped on.
type DebugException struct {
	// ID is the id of the exception (its type), Description what is wrong and StackTrace where.
	ID, Description, StackTrace string
}

// exceptionWidget is the exception widget of a view model.
type exceptionWidget struct {
	zone ZoneID
	info DebugException
	line int
}

// text is what Copy puts on the clipboard.
func (e DebugException) text() string {
	var parts []string
	if e.ID != "" {
		parts = append(parts, "Exception has occurred: "+e.ID)
	} else {
		parts = append(parts, "Exception has occurred.")
	}
	if e.Description != "" {
		parts = append(parts, e.Description)
	}
	if e.StackTrace != "" {
		parts = append(parts, e.StackTrace)
	}
	return strings.Join(parts, "\n")
}

// ExceptionText is the text of an exception as Copy puts it on the clipboard.
func (e DebugException) ExceptionText() string { return e.text() }

// exceptionMaxRows is the most rows the exception widget takes (VS Code sizes it to its text; a long stack trace is cut).
const exceptionMaxRows = 14

// DebugShowException shows the exception widget under a line (counted from 0): a title with the id of the exception, Copy and Close, the
// description and the stack trace. One that is open goes first. copy is what Copy does with the text; closed is called when the user
// closes the widget.
func (vm *ViewModel) DebugShowException(line int, info DebugException, copy func(text string), closed func()) error {
	vm.DebugCloseException()
	rows := 1
	for _, part := range []string{info.Description, info.StackTrace} {
		if part != "" {
			rows += len(strings.Split(strings.TrimRight(part, "\n"), "\n"))
		}
	}
	rows = min(rows+1, exceptionMaxRows)
	w := &exceptionWidget{info: info, line: line}
	spec := ZoneSpec{AfterLine: line + 1, Rows: rows, ShowInHiddenAreas: true}
	spec.Widget = w.body(vm, copy, func() {
		vm.DebugCloseException()
		if closed != nil {
			closed()
		}
	})
	id, err := vm.AddZone(spec)
	if err != nil {
		return err
	}
	w.zone = id
	vm.dbg.exc = w
	vm.notifyKeys()
	return nil
}

// DebugExceptionOpen tells whether the exception widget shows (the key exceptionWidgetVisible).
func (vm *ViewModel) DebugExceptionOpen() bool { return vm.dbg.exc != nil }

// DebugExceptionInfo is the exception the widget shows.
func (vm *ViewModel) DebugExceptionInfo() (DebugException, bool) {
	if e := vm.dbg.exc; e != nil {
		return e.info, true
	}
	return DebugException{}, false
}

// DebugCloseException closes the exception widget.
func (vm *ViewModel) DebugCloseException() {
	e := vm.dbg.exc
	if e == nil {
		return
	}
	vm.dbg.exc = nil
	if err := vm.RemoveZone(e.zone); err != nil {
		vm.dbg.exc = nil
	}
	vm.notifyKeys()
}

// body is the widget of the zone.
func (e *exceptionWidget) body(vm *ViewModel, copy func(string), closed func()) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		colors := kit.ThemeOf(bc)
		bg, border, fg := colors.EditorBackground, colors.EditorForeground, colors.EditorForeground
		if c, ok := colors.Named("debugExceptionWidget.background"); ok {
			bg = c
		}
		if c, ok := colors.Named("debugExceptionWidget.border"); ok {
			border = c
		}
		o := vm.Options()
		style := func(color ui.Color) *painting.TextStyle {
			return painting.NewTextStyle(painting.TextStyleOpts{
				FontFamily: o.FontFamilies[0], FontFamilyFallback: o.FontFamilies[1:], FontSize: foundation.Some(o.FontSize), Color: foundation.Some(color),
				Height: foundation.Some(o.LineHeight / math.Max(o.FontSize, 1)),
			})
		}
		label := "Exception has occurred."
		if e.info.ID != "" {
			label = "Exception has occurred: " + e.info.ID
		}
		actions := widgets.NewRow([]widgets.Widget{
			kit.NewIconButton(fonts.CodiconCopy, kit.IconButtonOpts{Tooltip: "Copy", OnPressed: func() {
				if copy != nil {
					copy(e.info.text())
				}
			}}),
			kit.NewIconButton(fonts.CodiconClose, kit.IconButtonOpts{Tooltip: "Close", OnPressed: closed}),
		}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin})
		rows := []widgets.Widget{
			widgets.NewRow([]widgets.Widget{widgets.NewExpanded(widgets.NewText(label, widgets.TextOpts{Style: style(fg), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})), actions},
				widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		}
		if e.info.Description != "" {
			rows = append(rows, widgets.NewText(expandTabs(e.info.Description), widgets.TextOpts{Style: style(fg)}))
		}
		if e.info.StackTrace != "" {
			rows = append(rows, widgets.NewText(expandTabs(strings.TrimRight(e.info.StackTrace, "\n")), widgets.TextOpts{Style: style(colors.DescriptionForeground)}))
		}
		col := widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
		return widgets.NewContainer(widgets.NewClipRect(widgets.NewSingleChildScrollView(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(2, 12), col))),
			widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color: foundation.Some(bg),
				Border: painting.Border{
					Top:    painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
					Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
				},
			})})
	})
}

// expandTabs puts four spaces for each tab: the font of the editor has no glyph for a tab.
func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }
