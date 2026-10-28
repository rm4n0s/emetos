package app

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// DevInput is one text field of the window, as dev.inputs reports it.
type DevInput struct {
	// Text is what the field holds.
	Text string `json:"text"`
	// SelectionStart and SelectionEnd are the byte offsets of the selection (equal for a caret), -1 when it has none.
	SelectionStart int `json:"selectionStart"`
	SelectionEnd   int `json:"selectionEnd"`
	// Focused tells whether the field has the primary focus.
	Focused bool `json:"focused"`
	// ReadOnly tells whether the user cannot edit the field.
	ReadOnly bool `json:"readOnly"`
	// X, Y, Width and Height are the field's place in window coordinates (logical pixels).
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// DevEditor is the editor in front of the user, as dev.editor reports it.
type DevEditor struct {
	// Path is the file of the document ("" for a document with no file), URI its URI.
	Path string `json:"path"`
	URI  string `json:"uri"`
	// Text is the whole text of the document.
	Text string `json:"text"`
	// Dirty tells whether the document has changes that are not saved.
	Dirty bool `json:"dirty"`
	// Line and Column are the primary cursor, counted from 1.
	Line   int `json:"line"`
	Column int `json:"column"`
	// InSnippet tells whether a snippet session runs, and TabStops how many tab stops of it the document holds (they are drawn
	// as gray boxes).
	InSnippet bool `json:"inSnippet"`
	TabStops  int  `json:"tabStops"`
	// SuggestVisible and SignatureVisible tell whether the suggest widget and the signature help show.
	SuggestVisible   bool `json:"suggestVisible"`
	SignatureVisible bool `json:"signatureVisible"`
	// HoverVisible tells whether the hover box shows.
	HoverVisible bool `json:"hoverVisible"`
}

// DevMarkdown is one Markdown preview on screen, as dev.markdown reports it.
type DevMarkdown struct {
	markdown.Stats
	// X, Y, Width and Height are the place of the preview in window coordinates (logical pixels).
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// registerDevQueries registers the read-only dev commands (SPEC §15.2): they give their answer to the dev server (setDevAnswer),
// and do nothing where a script of EMETOS_DEV_COMMANDS runs them. dev.inputs lists the text fields on screen, dev.editor gives
// the active document and its cursor, dev.markdown lists the Markdown previews on screen with the blocks they have built.
func registerDevQueries(p *Process) {
	inputs := command.Meta{ID: "dev.inputs", Description: "Report every text field on screen: its text, selection, focus and place. Read-only; the dev server answers with the list."}
	editor := command.Meta{ID: "dev.editor", Description: "Report the document of the active editor: its path, text and cursor. Read-only; the dev server answers with it."}
	terminals := command.Meta{ID: "dev.terminals", Description: "Report every terminal on screen: its place, the text of its rows, the selection, the scrolling and the focus. Read-only; the dev server answers with the list."}
	preview := command.Meta{ID: "dev.markdown", Description: "Report every Markdown preview on screen: its blocks, the blocks built, the top line, the scroll position and the image cache. Read-only; the dev server answers with the list."}
	for _, q := range []struct {
		meta command.Meta
		run  func(p *Process, ctx command.Context) error
	}{{inputs, devInputs}, {editor, devEditor}, {preview, devMarkdown}, {terminals, devTerminals}} {
		err := RegisterDev(p.Commands, q.meta, func(ctx command.Context, _ any) error { return q.run(p, ctx) })
		if err != nil {
			// the ids are fixed, new and start with dev., so a failure is a mistake here
			panic(err)
		}
	}
}

// devInputs answers dev.inputs.
func devInputs(p *Process, ctx command.Context) error {
	binding := p.Workspaces.binding
	if binding == nil || binding.RootElement() == nil {
		return errors.New("DevInputsNoWindow", "the window is not up yet")
	}
	list := []DevInput{}
	var visit func(el widgets.Element)
	visit = func(el widgets.Element) {
		if stateful, ok := el.(*widgets.StatefulElement); ok {
			if state, ok := stateful.State().(*widgets.EditableTextState); ok {
				list = append(list, devInputOf(state))
			}
		}
		el.DebugVisitOnstageChildren(visit)
	}
	binding.RootElement().DebugVisitOnstageChildren(visit)
	setDevAnswer(ctx.Ctx, list)
	return nil
}

// devMarkdown answers dev.markdown.
func devMarkdown(p *Process, ctx command.Context) error {
	binding := p.Workspaces.binding
	if binding == nil || binding.RootElement() == nil {
		return errors.New("DevMarkdownNoWindow", "the window is not up yet")
	}
	list := []DevMarkdown{}
	var visit func(el widgets.Element)
	visit = func(el widgets.Element) {
		if stateful, ok := el.(*widgets.StatefulElement); ok {
			if state, ok := stateful.State().(markdown.Inspector); ok {
				m := DevMarkdown{Stats: state.MarkdownStats()}
				if r, ok := el.RenderObject().(rendering.Box); ok && r != nil && r.HasSize() {
					at := r.LocalToGlobal(zeroOffset, nil)
					size := r.Size()
					m.X, m.Y, m.Width, m.Height = at.Dx, at.Dy, size.Width, size.Height
				}
				list = append(list, m)
			}
		}
		el.DebugVisitOnstageChildren(visit)
	}
	binding.RootElement().DebugVisitOnstageChildren(visit)
	setDevAnswer(ctx.Ctx, list)
	return nil
}

// DevTerminal is one terminal on screen, as dev.terminals reports it. State is what the terminal's render object says about it
// (the terminal extension's own type, which the app does not import).
type DevTerminal struct {
	// X, Y, Width and Height are the place of the terminal in window coordinates (logical pixels).
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	// State is the terminal's report: text, selected, scroll, scrollback, cols, rows, focused, alt.
	State any `json:"state"`
}

// terminalInspector is a render object that draws a terminal and reports it.
type terminalInspector interface{ DevTerminal() any }

// devTerminals answers dev.terminals.
func devTerminals(p *Process, ctx command.Context) error {
	binding := p.Workspaces.binding
	if binding == nil || binding.RootElement() == nil {
		return errors.New("DevTerminalsNoWindow", "the window is not up yet")
	}
	list := []DevTerminal{}
	seen := map[terminalInspector]bool{}
	var visit func(el widgets.Element)
	visit = func(el widgets.Element) {
		if ro, ok := el.RenderObject().(terminalInspector); ok && !seen[ro] {
			seen[ro] = true
			t := DevTerminal{State: ro.DevTerminal()}
			if r, ok := el.RenderObject().(rendering.Box); ok && r != nil && r.HasSize() {
				at := r.LocalToGlobal(zeroOffset, nil)
				size := r.Size()
				t.X, t.Y, t.Width, t.Height = at.Dx, at.Dy, size.Width, size.Height
			}
			list = append(list, t)
		}
		el.DebugVisitOnstageChildren(visit)
	}
	binding.RootElement().DebugVisitOnstageChildren(visit)
	setDevAnswer(ctx.Ctx, list)
	return nil
}

// devInputOf describes one field.
func devInputOf(state *widgets.EditableTextState) DevInput {
	w := state.Widget()
	value := state.TextEditingValue()
	in := DevInput{
		Text: value.Text, SelectionStart: -1, SelectionEnd: -1,
		Focused: w.FocusNode().HasPrimaryFocus(), ReadOnly: w.ReadOnly(),
	}
	if sel := value.Selection; sel.IsValid() {
		in.SelectionStart, in.SelectionEnd = sel.Start(), sel.End()
	}
	if r := state.RenderEditable(); r != nil && r.Attached() && r.HasSize() {
		at := r.LocalToGlobal(zeroOffset, nil)
		size := r.Size()
		in.X, in.Y, in.Width, in.Height = at.Dx, at.Dy, size.Width, size.Height
	}
	return in
}

// devEditor answers dev.editor.
func devEditor(p *Process, ctx command.Context) error {
	active := p.Workspaces.Active()
	if active == nil || active.shell == nil {
		return errors.New("DevEditorNoWorkspace", "no workspace tab is active")
	}
	shell := active.shell
	doc := shell.activeDocument()
	if doc == nil {
		return errors.New("DevEditorNone", "no editor shows a document")
	}
	out := DevEditor{URI: doc.URI().String(), Text: doc.Text(), Dirty: doc.Dirty(), Line: 1, Column: 1}
	if uri := doc.URI(); uri.Scheme == document.SchemeFile {
		out.Path = uri.Path
	}
	e := shell.activeEditor()
	if e == nil {
		// a diff editor: the cursor and the modes are those of its modified side
		if p, ok := shell.group.panes[shell.activeGroup().Model.ActiveEditor()].(*diffPane); ok {
			e = p.sides[1]
		}
	}
	if e != nil && e.view != nil {
		pos := e.view.Cursors().Primary().Position()
		out.Line, out.Column = pos.Line+1, pos.Col+1
		st := e.view.DevState()
		out.InSnippet, out.TabStops, out.SuggestVisible, out.SignatureVisible = st.InSnippet, st.TabStops, st.SuggestVisible, st.SignatureVisible
		out.HoverVisible = st.HoverVisible
	}
	setDevAnswer(ctx.Ctx, out)
	return nil
}

// zeroOffset is the origin of a render object.
var zeroOffset = ui.Offset{}
