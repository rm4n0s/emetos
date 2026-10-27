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
	preview := command.Meta{ID: "dev.markdown", Description: "Report every Markdown preview on screen: its blocks, the blocks built, the top line, the scroll position and the image cache. Read-only; the dev server answers with the list."}
	for _, q := range []struct {
		meta command.Meta
		run  func(p *Process, ctx command.Context) error
	}{{inputs, devInputs}, {editor, devEditor}, {preview, devMarkdown}} {
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
	if e := shell.activeEditor(); e != nil && e.view != nil {
		pos := e.view.Cursors().Primary().Position()
		out.Line, out.Column = pos.Line+1, pos.Col+1
	}
	setDevAnswer(ctx.Ctx, out)
	return nil
}

// zeroOffset is the origin of a render object.
var zeroOffset = ui.Offset{}
