package agents

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/markdown"
)

// The panel above the input that asks the user (SPEC §10.16 "As built (step 13.3)"): the request that is first in the queue.

// The part of the panel that can be long (the plan, the questions) has a height of its own, as high as its text is up to a
// maximum, and scrolls past that: a scroll view takes all the height it is given, so the height is estimated from the text.
const (
	planMaxHeight     = 240.0
	questionMaxHeight = 300.0
)

// scrolled gives a child a height of at most max, estimated as h, and a scroll bar when it is longer.
func scrolled(child widgets.Widget, h, max float64, handle *ui.ScrollHandle) widgets.Widget {
	if h >= max {
		return widgets.NewSizedBox(ui.NewScrollView(child, handle), widgets.SizedBoxOpts{Height: foundation.Some(max)})
	}
	return child
}

// planHeight estimates the height of the plan in the box that shows it: the lines of its text, long ones wrapped at about a
// hundred characters.
func planHeight(plan string) float64 {
	lines := 0
	for _, l := range strings.Split(plan, "\n") {
		lines += 1 + len(l)/100
	}
	return float64(lines)*21 + 24
}

// questionsHeight estimates the height of the questions: a heading, the options with their descriptions, the box for an own answer.
func questionsHeight(qs []*question) float64 {
	h := 0.0
	for _, q := range qs {
		h += 22 + 36 + 8
		if q.header != "" {
			h += 14
		}
		for _, o := range q.options {
			h += 26
			if o.description != "" {
				h += 16
			}
		}
	}
	return h
}

// permissionPanel is the panel of the request being asked; nil when there is none.
func (t *sessionTab) permissionPanel(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	p := t.current()
	if p == nil {
		return nil
	}
	var body []widgets.Widget
	switch p.kind {
	case permEdit:
		body = t.editBody(bc, colors, p)
	case permPlan:
		body = t.planBody(bc, colors, p)
	case permQuestion:
		body = t.questionBody(bc, colors, p)
	default:
		body = t.toolBody(bc, colors, p)
	}
	head := t.panelHead(bc, colors, p)
	content := widgets.NewColumn(append([]widgets.Widget{head, gap(8)}, body...), widgets.ColumnOpts{
		CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin,
	})
	return widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(14, 10, 14, 10), column(content)),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(colors.EditorWidgetBackground),
			Border: painting.Border{Top: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)})},
		})})
}

// panelHead is the title line of the panel: an icon, what is asked, and how many requests wait behind it.
func (t *sessionTab) panelHead(bc widgets.BuildContext, colors ui.Colors, p *permission) widgets.Widget {
	title := p.req.Title
	name := p.req.DisplayName
	if name == "" {
		name = p.req.ToolName
	}
	switch p.kind {
	case permEdit:
		title = "Review the proposed change to " + relative(editTarget(p), t.tr.folder)
	case permPlan:
		title = "The agent has a plan"
	case permQuestion:
		title = "The agent has a question"
	default:
		if title == "" {
			title = "The agent wants to use " + name
		}
	}
	icon := toolIcon(p.req.ToolName)
	switch p.kind {
	case permPlan:
		icon = "checklist"
	case permQuestion:
		icon = "question"
	}
	row := []widgets.Widget{
		ui.Icon(icon, 16, colors.EditorWarningForeground), hgap(8),
		widgets.NewExpanded(widgets.NewText(title, widgets.TextOpts{
			Style:    ui.TextStyle(bc, fontSize, colors.Foreground).CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightW600}),
			MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis,
		})),
	}
	if n := len(t.perms) - 1; n > 0 {
		row = append(row, hgap(8), widgets.NewText(strconv.Itoa(n)+" more waiting", widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
	}
	return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// editTarget is the file an edit request is about.
func editTarget(p *permission) string {
	for _, key := range []string{"file_path", "notebook_path"} {
		if s, ok := p.input[key].(string); ok && s != "" {
			return s
		}
	}
	return p.req.ToolName
}

// dim is a line of dimmed text.
func dim(bc widgets.BuildContext, colors ui.Colors, text string) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})
}

// reasonBox is the box for what the user tells the agent with a refusal.
func (t *sessionTab) reasonBox(p *permission, placeholder string) widgets.Widget {
	return ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: p.message, Placeholder: placeholder, SelectAllOnFocus: foundation.Some(false)})
}

// buttons is a row of buttons.
func buttons(bs ...widgets.Widget) widgets.Widget {
	var row []widgets.Widget
	for i, b := range bs {
		if i > 0 {
			row = append(row, hgap(8))
		}
		row = append(row, b)
	}
	return widgets.NewRow(row)
}

// detailBox is the command, the address or the input that a request is about, in the font of code.
func detailBox(bc widgets.BuildContext, colors ui.Colors, text string) widgets.Widget {
	return widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(6, 8), widgets.NewText(strings.ReplaceAll(text, "\t", "    "), widgets.TextOpts{
			Style:    ui.TextStyle(bc, 12, colors.TextPreformatForeground).CopyWith(painting.TextStyleOpts{FontFamily: monoFamily(bc)}),
			MaxLines: foundation.Some(12), Overflow: painting.TextOverflowEllipsis,
		})),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(colors.TextPreformatBackground), BorderRadius: painting.NewBorderRadiusCircular(4)})})
}

// toolDetail is the text a dialog shows for a tool: the command of Bash, the address of WebFetch, the search of WebSearch, and
// the input as JSON for any other tool.
func toolDetail(p *permission) string {
	str := func(key string) string { s, _ := p.input[key].(string); return s }
	var text string
	switch p.req.ToolName {
	case "Bash":
		text = str("command")
	case "WebFetch":
		text = str("url")
		if prompt := str("prompt"); prompt != "" {
			text += "\n" + prompt
		}
	case "WebSearch":
		text = str("query")
	}
	if strings.TrimSpace(text) == "" {
		data, err := json.MarshalIndent(p.input, "", "  ")
		if err != nil {
			return string(p.req.Input)
		}
		text = string(data)
	}
	return clip(text, 4096)
}

// toolBody is the dialog of a tool: what it will do, and Allow once, Allow for this session and Deny.
func (t *sessionTab) toolBody(bc widgets.BuildContext, colors ui.Colors, p *permission) []widgets.Widget {
	var out []widgets.Widget
	if p.note != "" {
		out = append(out, dim(bc, colors, p.note), gap(6))
	}
	if d := strings.TrimSpace(p.req.Description); d != "" {
		out = append(out, widgets.NewText(d, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.Foreground)}), gap(6))
	}
	out = append(out, detailBox(bc, colors, toolDetail(p)), gap(6))
	if r := strings.TrimSpace(p.req.DecisionReason); r != "" {
		out = append(out, dim(bc, colors, r), gap(6))
	}
	out = append(out, t.reasonBox(p, "Tell the agent why, or what to do instead (optional)"), gap(8))
	bs := []widgets.Widget{ui.NewButton("Allow once", ui.ButtonOpts{Small: true, OnPressed: func() { t.allowOnce(p) }})}
	if len(p.req.Suggestions) > 0 {
		bs = append(bs, ui.NewButton("Allow for this session", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { t.allowSession(p) },
			Tooltip: "Allow it, and the uses like it, until this session ends"}))
	}
	bs = append(bs, ui.NewButton("Deny", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { t.deny(p) }}))
	return append(out, buttons(bs...))
}

// editBody is the panel of an edit: the diff is in the editor, and the box takes what the user says if they reject it.
func (t *sessionTab) editBody(bc widgets.BuildContext, colors ui.Colors, p *permission) []widgets.Widget {
	return []widgets.Widget{
		widgets.NewText("The change is open in the diff editor. Accept it or reject it there; you can edit the text on the right before you accept.",
			widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.Foreground)}),
		gap(6),
		t.reasonBox(p, "If you reject it, tell the agent why (optional)"),
	}
}

// planBody is the plan as Markdown, with Approve and Keep Planning.
func (t *sessionTab) planBody(bc widgets.BuildContext, colors ui.Colors, p *permission) []widgets.Widget {
	plan := p.plan
	if strings.TrimSpace(plan) == "" {
		plan = "(The agent gave no text for the plan.)"
	}
	view := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(6, 10), markdown.NewView(plan, markdown.Options{Flow: true, Tokenize: t.tokenize, OnLink: t.openLink}))
	return []widgets.Widget{
		widgets.NewContainer(
			scrolled(view, planHeight(plan), planMaxHeight, &p.scroll),
			widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color: foundation.Some(colors.EditorBackground), BorderRadius: painting.NewBorderRadiusCircular(4),
				Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)}),
			})}),
		gap(6),
		t.reasonBox(p, "If you keep planning, tell the agent what to change (optional)"), gap(8),
		buttons(
			ui.NewButton("Approve", ui.ButtonOpts{Small: true, OnPressed: func() { t.approvePlan(p) }}),
			ui.NewButton("Keep Planning", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { t.keepPlanning(p) }}),
		),
	}
}

// questionBody is the questions with their options, an own answer for each, and Answer and Skip.
func (t *sessionTab) questionBody(bc widgets.BuildContext, colors ui.Colors, p *permission) []widgets.Widget {
	var list []widgets.Widget
	for _, q := range p.questions {
		if q.header != "" {
			list = append(list, widgets.NewText(strings.ToUpper(q.header), widgets.TextOpts{Style: ui.TextStyle(bc, 11, colors.DescriptionForeground)}), gap(2))
		}
		list = append(list, widgets.NewText(q.text, widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, colors.Foreground)}), gap(4))
		for i, o := range q.options {
			idx := i
			row := []widgets.Widget{ui.NewCheckbox(o.label, ui.CheckboxOpts{Checked: q.picked[i], OnChanged: func(on bool) {
				q.pick(idx, on)
				t.tab.Refresh()
			}})}
			if o.description != "" {
				row = append(row, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(27, 0, 0, 0), dim(bc, colors, o.description)))
			}
			list = append(list, widgets.NewColumn(row, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin}), gap(4))
		}
		list = append(list, ui.NewInputBox(nil, nil, ui.InputBoxOpts{
			Controller: q.other, Placeholder: "Or write your own answer", SelectAllOnFocus: foundation.Some(false),
			OnChanged: func(string) { t.tab.Refresh() },
		}), gap(10))
	}
	questions := widgets.NewColumn(list, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
	return []widgets.Widget{
		scrolled(questions, questionsHeight(p.questions), questionMaxHeight, &p.scroll),
		buttons(
			ui.NewButton("Answer", ui.ButtonOpts{Small: true, Disabled: !p.answered(), OnPressed: func() { t.submitAnswers(p) }}),
			ui.NewButton("Skip", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { t.skipQuestions(p) }}),
		),
	}
}
