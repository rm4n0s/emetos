package containerui

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// Ported from vscode-containers: extensions/vscode-containers/src/commands/containers/removeContainer.ts, images/removeImage.ts and
// the prune commands (the question that comes before a removal; here it is a box in the view instead of a modal dialog).

// choice is a way to answer a confirmation.
type choice struct {
	// label is the text of the button; the first choice is the primary button.
	label string
	// run does what the choice says.
	run func()
}

// confirmation is the question that comes before anything is removed (SPEC §10.20 "Safety"): it is shown at the top of the view
// of its section, says what goes, and offers the choices and Cancel. Nothing is done before a choice is pressed.
type confirmation struct {
	// section is the view that shows it.
	section resource
	// title is the question, lines say what goes (the count, the names, the size).
	title string
	lines []string
	// choices are the buttons, Cancel is added after them.
	choices []choice
}

// answer ends the question and, for a choice, does what it says. A question that was replaced meanwhile does nothing.
func (e *ext) answer(c *confirmation, i int) {
	if e.confirm != c {
		return
	}
	e.confirm = nil
	e.refreshViews()
	if i >= 0 && i < len(c.choices) {
		c.choices[i].run()
	}
}

// cancelConfirm is Cancel: the question goes away and nothing is done.
func (e *ext) cancelConfirm() {
	if c := e.confirm; c != nil {
		e.answer(c, -1)
	}
}

// confirmBar builds the question: a box over the tree with the title, the lines and the buttons.
func (e *ext) confirmBar(bc widgets.BuildContext, c *confirmation) widgets.Widget {
	colors := ui.Theme(bc)
	body := []widgets.Widget{
		widgets.NewText(c.title, widgets.TextOpts{Style: bold(ui.TextStyle(bc, 13, colors.EditorWidgetForeground))}),
	}
	for _, l := range c.lines {
		body = append(body, vgap(3), widgets.NewText(l, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
	}
	body = append(body, vgap(8))
	for i, ch := range c.choices {
		i := i
		body = append(body, ui.NewButton(ch.label, ui.ButtonOpts{Small: true, Expand: true, OnPressed: func() { e.answer(c, i) }}), vgap(4))
	}
	body = append(body, ui.NewButton("Cancel", ui.ButtonOpts{Small: true, Secondary: true, Expand: true, OnPressed: func() { e.answer(c, -1) }}))
	box := widgets.NewColumn(body, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return panelBox(colors, colors.ProblemsWarningIconForeground, widgets.NewPadding(painting.NewEdgeInsetsAll(10), box))
}

// panelBox puts a child on the background of editor widgets, with a border of a color and a stripe of it on the left.
func panelBox(colors ui.Colors, accent gui_Color, child widgets.Widget) widgets.Widget {
	side := painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(accent), Width: foundation.Some(3.0)})
	return widgets.NewPadding(painting.NewEdgeInsetsAll(6), widgets.NewContainer(child, widgets.ContainerOpts{
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(colors.EditorWidgetBackground), Border: painting.Border{Left: side},
		}),
	}))
}
