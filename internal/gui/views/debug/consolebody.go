package debug

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The widgets of the Debug Console: the body with its rows and its input, and the actions of the title strip of the panel.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/repl.ts, replViewer.ts and media/repl.css

// Keys of the widgets of the console, which tests find them by.
var (
	// ConsoleListKey is the key of the list of rows and ConsoleInputKey that of the input box.
	ConsoleListKey  = foundation.NewValueKey("debug:console:list")
	ConsoleInputKey = foundation.NewValueKey("debug:console:input")
	// ConsoleFilterKey is the key of the filter box, ConsoleClearKey that of the button that clears the console and ConsoleSessionsKey
	// that of the dropdown of the consoles.
	ConsoleFilterKey   = foundation.NewValueKey("debug:console:filter")
	ConsoleClearKey    = foundation.NewValueKey("debug:console:clear")
	ConsoleSessionsKey = foundation.NewValueKey("debug:console:sessions")
	// ConsoleSuggestKey is the key of the list of completions.
	ConsoleSuggestKey = foundation.NewValueKey("debug:console:suggest")
)

// ConsoleRowKey is the key of the widget of a row, by the key of the row.
func ConsoleRowKey(rowKey string) foundation.Key {
	return foundation.NewValueKey("debug:console:row:" + rowKey)
}

// Metrics of the rows (repl.css, tree.css).
const (
	consoleIndent  = 8.0
	consoleTwistie = 16.0
	consolePadding = 8.0
)

// ConsoleBodyOpts are what the body asks of its window.
type ConsoleBodyOpts struct {
	// Menu opens the context menu of a row at a position of the window; ok is false for the part below the rows.
	Menu func(row ConsoleRow, ok bool, at ui.Offset)
	// OnFocusChange is told when the input gets or loses the keyboard focus.
	OnFocusChange func(has bool)
}

// ConsoleBody is the body of the Debug Console: the rows of the output, and the input under them.
type ConsoleBody struct {
	widgets.StatefulWidget
	console *Console
	opts    ConsoleBodyOpts
}

// NewConsoleBody makes the body for a console.
func NewConsoleBody(c *Console, opts ...ConsoleBodyOpts) *ConsoleBody {
	b := &ConsoleBody{StatefulWidget: widgets.NewStatefulWidgetBase(), console: c}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// CreateState implements widgets.StatefulWidget.
func (b *ConsoleBody) CreateState() widgets.State { return &consoleBodyState{} }

type consoleBodyState struct {
	widgets.StateOf[*ConsoleBody]
	stop   func()
	scroll *widgets.ScrollControllerBase
	// model is what the scrollbars of the list follow.
	model *kit.ScrollControllerModel
	// reverse says that the list is a reversed one: row 0 of the list is the last row of the console and the end of the console is
	// the scroll offset 0. A list whose rows have no fixed height has to build every row between the top and the offset it is moved
	// to, so moving the list to its end would take time in proportion to the number of rows (100,000 of them is more than a minute);
	// at offset 0 it builds the rows in view only. A console that fits in its view is not reversed, as a reversed list puts its rows
	// at the bottom.
	// Glutter: a list of rows of unknown height builds every row between the top and the offset it is moved to; see docs/GLUTTER-ISSUES.md.
	reverse bool
	// inputHeight is the height of the input box as it last said, for the place of the list of completions.
	inputHeight float64
	endPending  bool
	modePending bool
	focused     bool
}

// InitState follows the console.
func (s *consoleBodyState) InitState() {
	c := s.Widget().console
	s.scroll = widgets.NewScrollController()
	s.model = kit.NewScrollControllerModel(s.scroll, nil)
	c.atEnd = s.isAtEnd
	s.stop = c.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
	// the console opens at its end
	c.KeepEnd(true)
}

// Dispose stops following the console.
func (s *consoleBodyState) Dispose() {
	c := s.Widget().console
	c.atEnd = nil
	s.stop()
	s.model.Dispose()
	s.scroll.Dispose()
}

// isAtEnd tells whether the list is scrolled to its end (within 2 pixels, as VS Code counts it).
func (s *consoleBodyState) isAtEnd() bool {
	if !s.reverse || !s.scroll.HasClients() {
		return true
	}
	p := s.scroll.Position()
	return !p.HasPixels() || p.Pixels() <= 2
}

// scrollToEnd moves a reversed list to its end after the next frame.
func (s *consoleBodyState) scrollToEnd() {
	if s.endPending || !s.reverse {
		return
	}
	s.endPending = true
	scheduler := widgets.WidgetsBindingOf(s.Context()).Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		s.endPending = false
		if !s.Mounted() || !s.reverse || !s.scroll.HasClients() {
			return
		}
		if p := s.scroll.Position(); p.HasPixels() && p.Pixels() != 0 {
			s.scroll.JumpTo(0)
		}
	})
}

// checkMode turns the list round when the rows stopped fitting in the view (the list is reversed from then on and shows its end) or
// started to fit.
func (s *consoleBodyState) checkMode() {
	if s.modePending {
		return
	}
	s.modePending = true
	scheduler := widgets.WidgetsBindingOf(s.Context()).Scheduler()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		s.modePending = false
		if !s.Mounted() || !s.scroll.HasClients() {
			return
		}
		p := s.scroll.Position()
		if !p.HasContentDimensions() {
			return
		}
		overflows := p.MaxScrollExtent() > 0.5
		if overflows == s.reverse {
			return
		}
		s.SetState(func() {
			s.reverse = overflows
			s.model.SetReverse(overflows)
		})
		s.Widget().console.KeepEnd(true)
	})
}

// Build implements widgets.State.
func (s *consoleBodyState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	c := w.console
	colors := kit.ThemeOf(bc)
	if c.WantsEnd() {
		s.scrollToEnd()
	}
	s.checkMode()
	list := s.list(bc)
	input := s.inputRow(bc, colors)
	layers := []widgets.Widget{widgets.NewColumn([]widgets.Widget{widgets.NewExpanded(list), input},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})}
	if pop := s.suggestPopup(bc, colors); pop != nil {
		layers = append(layers, pop)
	}
	return widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// rowStyle is the text style of the rows: the font of the setting, in the foreground.
func rowStyle(bc widgets.BuildContext, cfg ConsoleConfig, color ui.Color) *painting.TextStyle {
	family := cfg.FontFamily
	if family == "" || family == "default" {
		family = kit.MonoFontOf(bc)
	}
	return painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: family, FontFamilyFallback: []string{"monospace"}, FontSize: foundation.Some(cfg.FontSize), Color: foundation.Some(color),
		Height: foundation.Some(cfg.ConsoleLineHeight() / cfg.FontSize),
	})
}

// list is the rows, in a list that builds the rows in view only.
func (s *consoleBodyState) list(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	c := w.console
	colors := kit.ThemeOf(bc)
	n := c.Len()
	body := widgets.NewListViewBuilder(func(bc widgets.BuildContext, i int) widgets.Widget {
		rows := c.Rows()
		if i < 0 || i >= len(rows) {
			return nil
		}
		if s.reverse {
			i = len(rows) - 1 - i
		}
		return s.row(bc, rows[i])
	}, widgets.ListViewOpts{Key: ConsoleListKey, Controller: s.scroll, Reverse: s.reverse, ItemCount: foundation.Some(n)})
	view := kit.NewScrollableElement(body, kit.ScrollableElementOpts{Model: s.model})
	// the part below the rows is the console's own: a click there gives the input the keyboard, the right button opens the menu
	return widgets.NewGestureDetector(widgets.NewColoredBox(colors.PanelBackground, view), widgets.GestureDetectorOpts{
		Behavior: rendering.HitTestBehaviorTranslucent,
		OnSecondaryTapDown: func(d gestures.TapDownDetails) {
			if w.opts.Menu != nil {
				w.opts.Menu(ConsoleRow{}, false, ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy})
			}
		},
	})
}

// severityColor is the color of the text of a line: debugConsole.infoForeground, warningForeground or errorForeground; the lines of the
// console's own have the foreground.
func severityColor(colors kit.Colors, sev debug.ReplSeverity) ui.Color {
	id := ""
	switch sev {
	case debug.ReplInfo:
		id = "debugConsole.infoForeground"
	case debug.ReplWarning:
		id = "debugConsole.warningForeground"
	case debug.ReplError:
		id = "debugConsole.errorForeground"
	}
	return named(colors, id, colors.Foreground)
}

// row builds a row.
func (s *consoleBodyState) row(bc widgets.BuildContext, r ConsoleRow) widgets.Widget {
	w := s.Widget()
	c := w.console
	colors := kit.ThemeOf(bc)
	cfg := c.config()
	fg := colors.Foreground
	var content widgets.Widget
	var source *debug.ReplSource
	count := 0
	asLinks := &kit.AnsiLinks{Resolve: c.ResolveLink, Open: c.OpenLink}
	ansi := func(text string, sev debug.ReplSeverity, italic bool) widgets.Widget {
		style := rowStyle(bc, cfg, severityColor(colors, sev))
		if italic {
			style = style.CopyWith(painting.TextStyleOpts{FontStyle: ui.FontStyleItalic})
		}
		return kit.NewAnsiText(text, kit.AnsiTextOpts{Style: style, Links: asLinks, NoWrap: !cfg.WordWrap, TrimEndNewline: true})
	}
	switch x := r.Element.(type) {
	case *debug.ReplInput:
		if r.Kind == ConsoleInput {
			content = plainRich(rowStyle(bc, cfg, fg), []painting.InlineSpan{textSpan(x.Value)}, !cfg.WordWrap)
		}
	case *debug.ReplOutput:
		if r.Kind == ConsoleOutput {
			source, count = x.Src, x.Count()
			content = ansi(x.Value, x.Severity, x.Severity == debug.ReplIgnore)
		}
	case *debug.ReplGroup:
		if r.Kind == ConsoleGroup {
			source = x.Src
			content = kit.NewAnsiText(x.Name, kit.AnsiTextOpts{Style: rowStyle(bc, cfg, fg), Links: asLinks, NoWrap: !cfg.WordWrap, TrimEndNewline: true})
		}
	case *debug.ReplVariable:
		if r.Kind == ConsoleValue {
			source = x.Src
		}
	}
	switch r.Kind {
	case ConsoleResult, ConsoleValue, ConsoleChild:
		name := ""
		if v, ok := r.Node.(*debug.Variable); ok {
			name = v.Name
		}
		line := LineOfNode(r.Node, name, false)
		st := kit.RowState{Foreground: fg}
		style := rowStyle(bc, cfg, fg)
		content = plainRich(style, valueSpans(bc, line, st, style), !cfg.WordWrap)
	case ConsoleLoading:
		content = widgets.NewText("Loading...", widgets.TextOpts{Style: rowStyle(bc, cfg, colors.DescriptionForeground)})
	}
	if content == nil {
		content = widgets.NewSizedBox(nil)
	}
	// the twistie, or the arrow of an input
	var lead widgets.Widget = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(consoleTwistie)})
	lineH := cfg.ConsoleLineHeight()
	switch {
	case r.Expandable:
		code := fonts.CodiconChevronRight
		if r.Expanded {
			code = fonts.CodiconChevronDown
		}
		lead = twistie(code, colors.IconForeground, lineH)
	case r.Kind == ConsoleInput:
		lead = twistie(fonts.CodiconArrowSmallRight, named(colors, "debugConsoleInputIcon.foreground", fg), lineH)
	}
	children := []widgets.Widget{
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(consoleIndent + float64(r.Depth)*consoleIndent)}),
		lead,
	}
	if count >= 2 {
		children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 1, 4, 0), kit.NewBadge(count)))
	}
	children = append(children, widgets.NewExpanded(content))
	if source != nil {
		children = append(children, s.sourceLink(bc, colors, cfg, source))
	}
	row := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, consolePadding, 0),
		widgets.NewRow(children, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}))
	return widgets.NewKeyedSubtree(widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{
		Behavior: rendering.HitTestBehaviorTranslucent,
		OnTap: func() {
			if r.Expandable {
				c.Toggle(r.Key)
			}
			c.input.Focus()
		},
		OnSecondaryTapDown: func(d gestures.TapDownDetails) {
			if w.opts.Menu != nil {
				w.opts.Menu(r, true, ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy})
			}
		},
	}), widgets.WidgetOpts{Key: ConsoleRowKey(r.Key)})
}

func twistie(code int, color ui.Color, lineHeight float64) widgets.Widget {
	return widgets.NewSizedBox(widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)})),
		widgets.SizedBoxOpts{Width: foundation.Some(consoleTwistie), Height: foundation.Some(lineHeight)})
}

func textSpan(text string) painting.InlineSpan {
	return painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text)})
}

func plainRich(style *painting.TextStyle, spans []painting.InlineSpan, noWrap bool) widgets.Widget {
	opts := widgets.RichTextOpts{SoftWrap: foundation.Some(!noWrap)}
	if noWrap {
		opts.Overflow = painting.TextOverflowClip
	}
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans, Style: style}), opts)
}

// valueSpans are the spans of the line of a value, in the colors of the debug tokens.
func valueSpans(bc widgets.BuildContext, line ExpressionLine, st kit.RowState, base *painting.TextStyle) []painting.InlineSpan {
	return line.spansOn(bc, st, base)
}

// sourceLink is the name of the file and the line an output came from, at the end of its row.
func (s *consoleBodyState) sourceLink(bc widgets.BuildContext, colors kit.Colors, cfg ConsoleConfig, src *debug.ReplSource) widgets.Widget {
	c := s.Widget().console
	label := filepath.Base(src.Source.Name) + ":" + strconv.Itoa(src.Line)
	style := rowStyle(bc, cfg, named(colors, "debugConsole.sourceForeground", colors.Foreground)).CopyWith(painting.TextStyleOpts{
		Decoration: foundation.Some(ui.TextDecorationUnderline),
	})
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(4, 0, 0, 0), widgets.NewGestureDetector(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(400.0)}),
			widgets.NewText(label, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() {
			if c.opts.OpenSource != nil {
				c.opts.OpenSource(*src)
			}
		}}))
}

// inputRow is the input under the rows: the prompt and the box.
func (s *consoleBodyState) inputRow(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	c := w.console
	cfg := c.config()
	readOnly := c.ReadOnly()
	placeholder := ""
	if readOnly {
		placeholder = "Please start a debug session to evaluate expressions"
	}
	box := kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Key: ConsoleInputKey, Controller: c.input, Placeholder: placeholder, MultiLine: true, SubmitOnEnter: true, MinLines: 1, MaxLines: 10,
		Monospace: cfg.FontFamily == "" || cfg.FontFamily == "default", FontFamily: customFamily(cfg), FontSize: foundation.Some(cfg.FontSize),
		LineHeight: foundation.Some(cfg.ConsoleLineHeight()), History: c.history, Disabled: readOnly,
		OnChanged: c.sugg.changed,
		OnSubmit:  func(string) { c.Accept() },
		KeyHook: func(e services.KeyEvent) widgets.KeyEventResult {
			return c.sugg.key(e, widgets.WidgetsBindingOf(s.Context()).Keyboard())
		},
		OnFocusChange: func(has bool) {
			s.focused = has
			if !has {
				c.sugg.close()
			}
			if w.opts.OnFocusChange != nil {
				w.opts.OnFocusChange(has)
			}
		},
		OnHeightChange: func(h float64) {
			if s.Mounted() && h != s.inputHeight {
				s.SetState(func() { s.inputHeight = h })
			}
		},
	})
	border := named(colors, "input.border", ui.NewColorFromARGB(0x60, 0x80, 0x80, 0x80))
	if colors.InputBorder.Alpha() != 0 {
		border = colors.InputBorder
	}
	prompt := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 4, 6, 0),
		widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconChevronRight), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(named(colors, "debugConsoleInputIcon.foreground", colors.Foreground))}))
	return widgets.NewContainer(
		widgets.NewRow([]widgets.Widget{prompt, widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 2, 8, 2), box))},
			widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Border: painting.Border{Top: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)})},
		})})
}

func customFamily(cfg ConsoleConfig) string {
	if cfg.FontFamily == "" || cfg.FontFamily == "default" {
		return ""
	}
	return cfg.FontFamily
}

// suggestPopup is the list of completions, above the input.
func (s *consoleBodyState) suggestPopup(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	c := s.Widget().console
	if !c.sugg.open() {
		return nil
	}
	rows, focus, top := c.sugg.rows()
	const rowHeight = 20.0
	height := float64(len(rows))*rowHeight + 2
	list := kit.NewSuggestList(colors, kit.SuggestListOpts{
		Rows: rows, Focus: focus, Total: len(c.sugg.items), Top: top, Width: kit.SuggestWidth, RowHeight: rowHeight, FontSize: 13,
		ShowIcons: true, ShowInlineDetails: true,
		OnAccept: func(i int) {
			c.sugg.focus = top + i
			c.sugg.accept()
		},
		OnScroll: func(lines int) { c.sugg.move(lines) },
	})
	bottom := s.inputHeight + 6
	if bottom < 28 {
		bottom = 28
	}
	return widgets.NewPositioned(widgets.NewKeyedSubtree(widgets.NewSizedBox(list, widgets.SizedBoxOpts{Width: foundation.Some(kit.SuggestWidth + 2), Height: foundation.Some(height)}),
		widgets.WidgetOpts{Key: ConsoleSuggestKey}), widgets.PositionedOpts{Left: foundation.Some(30.0), Bottom: foundation.Some(bottom)})
}

// ---- the actions of the panel

// ConsoleActions is what the panel shows in its title strip while the Debug Console is the tab: the dropdown of the consoles when there
// are several, the filter box and the button that clears the console.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/repl.ts (the filter of FilterViewPane, clearReplAction, SelectReplActionViewItem)
type ConsoleActions struct {
	widgets.StatefulWidget
	console *Console
	opts    ConsoleActionsOpts
}

// ConsoleActionsOpts are what the actions ask of the window.
type ConsoleActionsOpts struct {
	// OnFilterFocus is told when the filter box gets or loses the keyboard focus.
	OnFilterFocus func(has bool)
}

// NewConsoleActions makes the actions for a console.
func NewConsoleActions(c *Console, opts ...ConsoleActionsOpts) *ConsoleActions {
	a := &ConsoleActions{StatefulWidget: widgets.NewStatefulWidgetBase(), console: c}
	if len(opts) > 0 {
		a.opts = opts[0]
	}
	return a
}

// CreateState implements widgets.StatefulWidget.
func (a *ConsoleActions) CreateState() widgets.State { return &consoleActionsState{} }

// Widths of the actions: the filter box at most consoleFilterWidth and at least consoleFilterMinWidth wide (VS Code's 200 and 150 pixels
// for the filter of a panel), the clear button, the dropdown of the consoles at most consoleSessionsWidth.
const (
	consoleFilterWidth    = 200.0
	consoleFilterMinWidth = 150.0
	consoleButtonsWidth   = 22.0 + 6
	consoleSessionsWidth  = 140.0
)

// TitleActionsWidth implements kit.TitleActions.
func (a *ConsoleActions) TitleActionsWidth(room float64) float64 {
	extra := 0.0
	if a.console.Multiple() {
		extra = consoleSessionsWidth + 6
	}
	switch {
	case room >= consoleButtonsWidth+consoleFilterMinWidth+extra:
		return min(room, consoleButtonsWidth+consoleFilterWidth+extra)
	case room >= kit.IconButtonSize:
		return kit.IconButtonSize
	}
	return 0
}

type consoleActionsState struct {
	widgets.StateOf[*ConsoleActions]
	stop func()
}

// InitState follows the console.
func (s *consoleActionsState) InitState() {
	s.stop = s.Widget().console.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// Dispose stops following the console.
func (s *consoleActionsState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *consoleActionsState) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewLayoutBuilder(func(bc widgets.BuildContext, cons rendering.BoxConstraints) widgets.Widget {
		return s.actions(bc, cons.MaxWidth)
	})
}

func (s *consoleActionsState) actions(bc widgets.BuildContext, room float64) widgets.Widget {
	w := s.Widget()
	c := w.console
	colors := kit.ThemeOf(bc)
	var children []widgets.Widget
	if c.Multiple() && room >= consoleButtonsWidth+consoleFilterMinWidth+consoleSessionsWidth+6 {
		sessions := c.Sessions()
		options := make([]kit.DropdownOption, len(sessions))
		selected := -1
		for i, ss := range sessions {
			options[i] = kit.DropdownOption{Label: ss.Label()}
			if ss == c.Session() {
				selected = i
			}
		}
		children = append(children,
			widgets.NewSizedBox(kit.NewDropdown(options, kit.DropdownOpts{
				Key: ConsoleSessionsKey, Selected: selected, Width: foundation.Some(consoleSessionsWidth),
				OnChanged: func(i int) { c.Select(sessions[i]) },
			}), widgets.SizedBoxOpts{Width: foundation.Some(consoleSessionsWidth)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
	}
	extra := 0.0
	if len(children) > 0 {
		extra = consoleSessionsWidth + 6
	}
	if width := min(room-consoleButtonsWidth-extra, consoleFilterWidth); width >= consoleFilterMinWidth {
		var trailing []widgets.Widget
		if total, shown := c.Counts(); total != shown && total != 0 {
			trailing = append(trailing, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 4, 0), widgets.NewText(
				"Showing "+strconv.Itoa(shown)+" of "+strconv.Itoa(total),
				widgets.TextOpts{Style: kit.TextStyleOf(bc, 11, colors.DescriptionForeground), MaxLines: foundation.Some(1)})))
		}
		children = append(children,
			widgets.NewSizedBox(kit.NewInputBox(nil, nil, kit.InputBoxOpts{
				Key: ConsoleFilterKey, Controller: c.filterC, Placeholder: "Filter (e.g. text, !exclude, \\escape)", Trailing: trailing,
				OnChanged: c.SetFilter,
				OnFocusChange: func(has bool) {
					if w.opts.OnFilterFocus != nil {
						w.opts.OnFilterFocus(has)
					}
				},
			}), widgets.SizedBoxOpts{Width: foundation.Some(width)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
	}
	children = append(children, kit.NewIconButton(fonts.CodiconClearAll, kit.IconButtonOpts{Key: ConsoleClearKey, Tooltip: "Clear Console", OnPressed: c.Clear}))
	return widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
