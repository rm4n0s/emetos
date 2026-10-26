package agents

import (
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// The metrics of the tab.
const (
	// columnWidth is the widest the conversation gets; a wider tab centers it.
	columnWidth = 860.0
	// scrollEnd is a position past the end of any conversation: ScrollTo clamps it to the end.
	scrollEnd = 1e9
	// fontSize is the size of the text of the conversation.
	fontSize = 13.0
)

// build is the widget of the tab: the conversation, a banner when the program cannot run, and the input.
func (t *sessionTab) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	if t.stick {
		widgets.WidgetsBindingOf(bc).Scheduler().AddPostFrameCallback(func(time.Duration) {
			if t.stick && !t.closed {
				t.scroll.ScrollTo(0, scrollEnd)
			}
		})
	}
	children := []widgets.Widget{widgets.NewExpanded(t.conversation(bc, colors))}
	if banner := t.banner(bc, colors); banner != nil {
		children = append(children, banner)
	}
	children = append(children, t.inputArea(bc, colors))
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}

// column puts a widget in the centered column of the conversation.
func column(child widgets.Widget) widgets.Widget {
	return widgets.NewAlign(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(columnWidth)}), child),
		widgets.AlignOpts{Alignment: painting.AlignmentTopCenter})
}

// gap is empty space of a height.
func gap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

// hgap is empty space of a width.
func hgap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

// conversation is the scrolling list of the items.
func (t *sessionTab) conversation(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	items := append(append([]*item(nil), t.tr.items...), t.tr.live()...)
	var rows []widgets.Widget
	for _, it := range items {
		rows = append(rows, t.itemWidget(bc, colors, it))
	}
	if t.busy {
		rows = append(rows, widgets.NewRow([]widgets.Widget{
			ui.Icon("loading", 14, colors.DescriptionForeground), hgap(6),
			widgets.NewText("Working...", widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground)}),
		}))
	}
	if len(rows) == 0 {
		return widgets.NewCenter(widgets.NewColumn([]widgets.Widget{
			ui.Icon(agentIcon, 40, colors.DescriptionForeground), gap(10),
			widgets.NewText("Ask the agent about this project.", widgets.TextOpts{Style: ui.TextStyle(bc, 14, colors.DescriptionForeground)}),
			gap(4),
			widgets.NewText("Type / for commands and @ for files.", widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground)}),
		}, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin}))
	}
	spaced := make([]widgets.Widget, 0, len(rows)*2)
	for i, r := range rows {
		if i > 0 {
			spaced = append(spaced, gap(10))
		}
		spaced = append(spaced, r)
	}
	body := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 16, 20, 16),
		widgets.NewColumn(spaced, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	return ui.NewScrollView(column(body), &t.scroll)
}

// itemWidget is the widget of an item.
func (t *sessionTab) itemWidget(bc widgets.BuildContext, colors ui.Colors, it *item) widgets.Widget {
	switch it.Kind {
	case kindUser:
		return widgets.NewContainer(
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(7, 10), widgets.NewText(it.Text, widgets.TextOpts{
				Style: ui.TextStyle(bc, fontSize, colors.InputForeground).CopyWith(painting.TextStyleOpts{Height: foundation.Some(1.5)}),
			})),
			widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color: foundation.Some(colors.InputBackground), BorderRadius: painting.NewBorderRadiusCircular(5),
				Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.InputBorder), Width: foundation.Some(1.0)}),
			})})
	case kindText:
		return markdown.NewView(it.Text, markdown.Options{Flow: true, Tokenize: t.tokenize, OnLink: t.openLink})
	case kindThinking:
		return t.thinkingWidget(bc, colors, it)
	case kindTool:
		return t.toolWidget(bc, colors, it)
	case kindError:
		return widgets.NewRow([]widgets.Widget{
			ui.Icon("error", 14, colors.ErrorForeground), hgap(6),
			widgets.NewExpanded(widgets.NewText(it.Text, widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, colors.ErrorForeground)})),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	}
	return widgets.NewRow([]widgets.Widget{
		ui.Icon("info", 14, colors.DescriptionForeground), hgap(6),
		widgets.NewExpanded(widgets.NewText(it.Text, widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground)})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

// toggle is the widget that unfolds and folds an item on a click.
func (t *sessionTab) toggle(it *item, child widgets.Widget) widgets.Widget {
	return widgets.NewGestureDetector(child, widgets.GestureDetectorOpts{
		Behavior: rendering.HitTestBehaviorOpaque,
		OnTap: func() {
			it.open = !it.open
			t.tab.Refresh()
		},
	})
}

// thinkingWidget is the thinking of the agent, folded to one line.
func (t *sessionTab) thinkingWidget(bc widgets.BuildContext, colors ui.Colors, it *item) widgets.Widget {
	chevron := "chevron-right"
	if it.open {
		chevron = "chevron-down"
	}
	style := ui.TextStyle(bc, fontSize, colors.DescriptionForeground)
	head := t.toggle(it, widgets.NewRow([]widgets.Widget{
		ui.Icon(chevron, 14, colors.DescriptionForeground), hgap(4),
		ui.Icon("lightbulb", 14, colors.DescriptionForeground), hgap(6),
		widgets.NewText("Thinking", widgets.TextOpts{Style: style}),
	}))
	if !it.open {
		return head
	}
	return widgets.NewColumn([]widgets.Widget{head, gap(4), widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(24, 0, 0, 0),
		widgets.NewText(it.Text, widgets.TextOpts{Style: style.CopyWith(painting.TextStyleOpts{FontStyle: gui.FontStyleItalic})}))},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

// toolWidget is a tool call: one line with what it does and how it went, and its result when it is unfolded.
func (t *sessionTab) toolWidget(bc widgets.BuildContext, colors ui.Colors, it *item) widgets.Widget {
	status := ui.Icon("loading", 14, colors.DescriptionForeground)
	switch {
	case it.Done && it.Failed:
		status = ui.Icon("error", 14, colors.ErrorForeground)
	case it.Done:
		status = ui.Icon("check", 14, colors.DescriptionForeground)
	}
	name := ui.TextStyle(bc, 12, colors.Foreground).CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightW600})
	summary := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	line := widgets.NewRow([]widgets.Widget{
		ui.Icon(toolIcon(it.Tool), 14, colors.DescriptionForeground), hgap(6),
		widgets.NewText(it.Tool, widgets.TextOpts{Style: name}), hgap(8),
		widgets.NewExpanded(widgets.NewText(it.Summary, widgets.TextOpts{Style: summary, Overflow: painting.TextOverflowEllipsis, MaxLines: foundation.Some(1)})),
		hgap(8), status,
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	row := widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(3, 8), t.toggle(it, line)), widgets.ContainerOpts{
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(colors.TextPreformatBackground), BorderRadius: painting.NewBorderRadiusCircular(4)}),
	})
	var w widgets.Widget = row
	if it.open && it.Result != "" {
		fg := colors.Foreground
		if it.Failed {
			fg = colors.ErrorForeground
		}
		w = widgets.NewColumn([]widgets.Widget{row, gap(2), widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 2, 8, 2),
			widgets.NewText(strings.ReplaceAll(it.Result, "\t", "    "), widgets.TextOpts{
				Style: ui.TextStyle(bc, 12, fg).CopyWith(painting.TextStyleOpts{FontFamily: monoFamily(bc)}), MaxLines: foundation.Some(24), Overflow: painting.TextOverflowEllipsis,
			}))}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	if it.Depth > 0 {
		w = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16*float64(it.Depth), 0, 0, 0), w)
	}
	return w
}

// monoFamily is the monospace font of the window.
func monoFamily(bc widgets.BuildContext) string {
	family, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont()
	if family == "" {
		return "monospace"
	}
	return family
}

// tokenize highlights the code of a Markdown block with the grammar of the language of the info string.
func (t *sessionTab) tokenize(language, text string) ([][]extension.Token, bool) {
	langs := t.ext.api.Languages()
	id, ok := langs.Resolve(language)
	if !ok {
		return nil, false
	}
	lines, err := langs.Tokenize(id, text)
	return lines, err == nil
}

// openLink opens a link of the agent's text: an address goes to the browser, anything else is left alone.
func (t *sessionTab) openLink(dest string) {
	if !strings.HasPrefix(dest, "https://") && !strings.HasPrefix(dest, "http://") {
		return
	}
	a := t.ext
	go func() {
		if err := a.api.Workspace().OpenURI(a.ctx, dest); err != nil {
			a.api.Post(func() { a.api.Notifications().Warn("Cannot open the link: " + err.Error()) })
		}
	}()
}

// ---- the banner ----

// banner is the notice above the input when the program cannot run; nil when there is none.
func (t *sessionTab) banner(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	var icon, text string
	var buttons []widgets.Widget
	switch t.phase {
	case phaseMissing:
		icon, text = "warning", "The `claude` program was not found. Install it, or set `"+PathSetting+"` in the settings to its path."
		buttons = []widgets.Widget{ui.NewButton("Check Again", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: t.retry})}
	case phaseLoggedOut:
		icon, text = "key", "You are not logged in. Log in to the program to talk to the agent."
		buttons = []widgets.Widget{
			ui.NewButton("Log in", ui.ButtonOpts{Small: true, OnPressed: func() { t.ext.startLogin() }}),
			hgap(8), ui.NewButton("Check Again", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: t.retry}),
		}
	case phaseFailed:
		icon, text = "error", t.problem
		buttons = []widgets.Widget{ui.NewButton("Try Again", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: t.retry})}
	case phaseStarting:
		icon, text = "loading", "Starting the agent..."
	default:
		return nil
	}
	fg := colors.Foreground
	row := []widgets.Widget{
		ui.Icon(icon, 16, colors.EditorWarningForeground), hgap(8),
		widgets.NewExpanded(rich(bc, text, ui.TextStyle(bc, fontSize, fg), colors)),
	}
	if len(buttons) > 0 {
		row = append(row, hgap(12))
		row = append(row, buttons...)
	}
	return widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(8, 14), widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(colors.EditorWidgetBackground),
			Border: painting.Border{Top: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)})},
		})})
}

// rich is text in which the parts between backticks are code.
func rich(bc widgets.BuildContext, text string, style *painting.TextStyle, colors ui.Colors) widgets.Widget {
	mono := style.CopyWith(painting.TextStyleOpts{FontFamily: monoFamily(bc), Color: foundation.Some(colors.TextPreformatForeground)})
	var spans []painting.InlineSpan
	for i, part := range strings.Split(text, "`") {
		if part == "" {
			continue
		}
		st := style
		if i%2 == 1 {
			st = mono
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(part), Style: st}))
	}
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: spans}))
}

// ---- the input ----

// modeOptions are the permission modes of the picker, in order.
var modeOptions = []struct {
	mode  claude.PermissionMode
	label string
}{{claude.ModeManual, "Manual"}, {claude.ModeAcceptEdits, "Accept Edits"}, {claude.ModePlan, "Plan"}}

// inputArea is the completion list, the input box and the row of pickers and buttons under it.
func (t *sessionTab) inputArea(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	var children []widgets.Widget
	if list := t.completionList(bc, colors); list != nil {
		children = append(children, list, gap(4))
	}
	children = append(children, ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: t.input, Placeholder: "Message the agent (Enter sends, Shift+Enter makes a new line)", MultiLine: true,
		MaxHeight: foundation.Some(160.0), SelectAllOnFocus: foundation.Some(false),
		OnChanged: func(string) { t.inputChanged() }, OnSubmit: t.submit,
		OnFocusChange: func(focused bool) {
			if focused {
				t.ext.active = t
			}
		},
	}), gap(6))
	modes := make([]ui.DropdownOption, len(modeOptions))
	modeSel := 0
	for i, m := range modeOptions {
		modes[i] = ui.DropdownOption{Label: m.label}
		if m.mode == t.mode {
			modeSel = i
		}
	}
	models := []ui.DropdownOption{{Label: "Default model", Description: t.running}}
	modelSel := 0
	for i, m := range t.models {
		label := m.Name
		if label == "" {
			label = m.Value
		}
		models = append(models, ui.DropdownOption{Label: label})
		if m.Value == t.model {
			modelSel = i + 1
		}
	}
	buttons := []widgets.Widget{
		ui.NewDropdown(modes, ui.DropdownOpts{Selected: modeSel, Width: foundation.Some(130.0), OnChanged: func(i int) { t.chooseMode(modeOptions[i].mode) }}),
		hgap(8),
		ui.NewDropdown(models, ui.DropdownOpts{Selected: modelSel, Width: foundation.Some(190.0), OnChanged: func(i int) {
			if i == 0 {
				t.chooseModel("")
			} else {
				t.chooseModel(t.models[i-1].Value)
			}
		}}),
		widgets.NewSpacer(),
	}
	if t.busy {
		buttons = append(buttons, ui.NewButton("Stop", ui.ButtonOpts{Small: true, Secondary: true, Icon: iconCode("stop-circle"), OnPressed: t.stop, Tooltip: "Stop the turn that runs"}), hgap(6))
	}
	buttons = append(buttons, ui.NewButton("Send", ui.ButtonOpts{Small: true, Icon: iconCode("send"), OnPressed: func() { t.submit(t.input.Value()) },
		Disabled: t.phase == phaseMissing || t.phase == phaseLoggedOut}))
	children = append(children, widgets.NewRow(buttons, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	return widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(14, 8, 14, 10), column(widgets.NewColumn(children, widgets.ColumnOpts{
			CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin,
		}))),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(colors.EditorBackground),
			Border: painting.Border{Top: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})},
		})})
}

// iconCode is the code point of a Codicon, 0 when there is none.
func iconCode(name string) int {
	code, _ := ui.CodiconCode(name)
	return code
}

// completionList is the list of the commands or files the input offers; nil when it offers none.
func (t *sessionTab) completionList(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	if len(t.comp.items) == 0 {
		return nil
	}
	rows := make([]widgets.Widget, len(t.comp.items))
	for i, c := range t.comp.items {
		fg, bg := colors.EditorWidgetForeground, colors.EditorWidgetBackground
		dim := colors.DescriptionForeground
		if i == t.comp.sel {
			fg, bg, dim = colors.ListActiveSelectionForeground, colors.ListActiveSelectionBackground, colors.ListActiveSelectionForeground
		}
		row := widgets.NewColoredBox(bg, widgets.NewSizedBox(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 8), widgets.NewRow([]widgets.Widget{
			widgets.NewText(c.Label, widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, fg), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}),
			hgap(10),
			widgets.NewExpanded(widgets.NewText(c.Detail, widgets.TextOpts{Style: ui.TextStyle(bc, 12, dim), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})), widgets.SizedBoxOpts{Height: foundation.Some(22.0)}))
		idx := i
		rows[i] = widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnTap:    func() { t.comp.sel = idx; t.acceptCompletion(); t.input.Focus() },
		})
	}
	return widgets.NewContainer(widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin}),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(colors.EditorWidgetBackground),
			Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)}),
		})})
}

// onKey is the key handler of the input: the completion list takes the arrows, Tab, Enter and Escape while it is open, and
// Enter sends the message (Shift+Enter makes a new line).
func (t *sessionTab) onKey(node *widgets.FocusNode, ev services.KeyEvent) widgets.KeyEventResult {
	switch ev.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	ctx := node.Context()
	if ctx == nil {
		return widgets.KeyEventResultIgnored
	}
	kb := widgets.WidgetsBindingOf(ctx).Keyboard()
	key := ev.LogicalKey()
	enter := key == services.LogicalKeyboardKeyEnter || key == services.LogicalKeyboardKeyNumpadEnter
	if len(t.comp.items) > 0 && !kb.IsShiftPressed() {
		switch {
		case key == services.LogicalKeyboardKeyArrowDown:
			t.comp.sel = (t.comp.sel + 1) % len(t.comp.items)
		case key == services.LogicalKeyboardKeyArrowUp:
			t.comp.sel = (t.comp.sel + len(t.comp.items) - 1) % len(t.comp.items)
		case key == services.LogicalKeyboardKeyTab || enter:
			t.acceptCompletion()
		case key == services.LogicalKeyboardKeyEscape:
			t.comp = completion{}
		default:
			return widgets.KeyEventResultIgnored
		}
		t.tab.Refresh()
		return widgets.KeyEventResultHandled
	}
	if enter && !kb.IsShiftPressed() && !kb.IsAltPressed() && !kb.IsMetaPressed() && !kb.IsControlPressed() {
		if _, repeat := ev.(*services.KeyRepeatEvent); !repeat {
			t.submit(t.input.Value())
		}
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}
