package editor

import (
	"unicode/utf8"

	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/edit"
)

// Preedit is the text an input method is composing: shown at the primary cursor, underlined,
// until the method commits it. It is not in the document, so it never enters the undo history
// (SPEC §9.3).
type Preedit struct {
	// Text is the composed text.
	Text string
	// Begin and End are the selection inside it in bytes: the part the method highlights, or
	// where its cursor is (equal for a caret).
	Begin, End int
}

// Preedit is the text being composed, and whether there is any.
func (vm *ViewModel) Preedit() (Preedit, bool) {
	if vm.preedit == nil {
		return Preedit{}, false
	}
	return *vm.preedit, true
}

// SetPreedit shows the text an input method composes; an empty text ends it. The layers that
// draw the cursors and the caret are told.
func (vm *ViewModel) SetPreedit(p Preedit) {
	switch {
	case p.Text == "" && vm.preedit == nil:
		return
	case p.Text == "":
		vm.preedit = nil
	default:
		p.Begin = min(max(p.Begin, 0), len(p.Text))
		p.End = min(max(p.End, 0), len(p.Text))
		if vm.preedit != nil && *vm.preedit == p {
			return
		}
		vm.preedit = &p
	}
	vm.SelectionsChanged()
}

// textInput is the editor's client of the platform's text input (SPEC §9.3): it speaks the
// delta model, and the editing value it gives the input method is the current line of the
// primary cursor (Glutter's input layer keeps a full copy of the text it is given, so the
// whole document would be copied on each key), with line-local offsets. The input method's
// changes become typing, composition and deletion at every cursor; its preedit becomes
// the view model's Preedit.
type textInput struct {
	services.TextInputClientDefaults
	state *editorState
	conn  *services.TextInputConnection
	// value is what the input method was last given.
	value services.TextEditingValue
	// line is the document line of that value.
	line int
}

// textInputConfiguration is the configuration of the editor's connection: multiline text,
// deltas, and the Enter key adds a line.
func textInputConfiguration() services.TextInputConfiguration {
	return services.NewTextInputConfiguration(services.TextInputConfigurationOpts{
		InputType:          services.TextInputTypeMultiline,
		InputAction:        services.TextInputActionNewline,
		EnableDeltaModel:   true,
		DisableAutocorrect: true,
	})
}

// attach connects to the text input and gives it the current line.
func (t *textInput) attach(binding *widgets.WidgetsBinding) {
	if t.conn != nil && t.conn.Attached() {
		return
	}
	t.conn = binding.TextInput().Attach(t, textInputConfiguration())
	t.conn.Show()
	t.value, t.line = services.TextEditingValue{}, -1
	t.sync()
}

// detach closes the connection.
func (t *textInput) detach() {
	if t.conn != nil && t.conn.Attached() {
		t.conn.Close()
	}
	t.conn = nil
}

// desired is the value the input method should have: the line of the primary cursor, with its
// selection on that line (a selection that goes over other lines is a caret at its active end,
// since the input method cannot see the other lines).
func (t *textInput) desired() (services.TextEditingValue, int) {
	vm := t.state.Widget().vm
	primary := vm.Cursors().Primary().Selection
	line := primary.Active.Line
	text, err := vm.Document().Snapshot().LineContent(line)
	if err != nil {
		return services.TextEditingValue{Composing: ui.TextRangeEmpty}, line
	}
	anchor, active := min(primary.Anchor.Col, len(text)), min(primary.Active.Col, len(text))
	if primary.Anchor.Line != line {
		anchor = active
	}
	return services.TextEditingValue{
		Text: text, Selection: services.NewTextSelection(anchor, active), Composing: ui.TextRangeEmpty,
	}, line
}

// sync gives the input method the current line when it is not the one it has, which moves its
// window when the primary cursor changes line.
func (t *textInput) sync() {
	if t.conn == nil || !t.conn.Attached() {
		return
	}
	value, line := t.desired()
	if line == t.line && value.Text == t.value.Text && value.Selection == t.value.Selection {
		return
	}
	t.value, t.line = value, line
	t.conn.SetEditingState(value)
}

// CurrentTextEditingValue implements services.TextInputClient.
func (t *textInput) CurrentTextEditingValue() (services.TextEditingValue, bool) { return t.value, true }

// CurrentAutofillScope implements services.TextInputClient.
func (t *textInput) CurrentAutofillScope() services.AutofillScope { return nil }

// UpdateEditingValue implements services.TextInputClient for an input method that sends
// whole values.
func (t *textInput) UpdateEditingValue(v services.TextEditingValue) { t.apply(v) }

// PerformAction implements services.TextInputClient: the Enter key has typed its new line.
func (t *textInput) PerformAction(services.TextInputAction) {}

// PerformPrivateCommand implements services.TextInputClient.
func (t *textInput) PerformPrivateCommand(string, map[string]any) {}

// UpdateFloatingCursor implements services.TextInputClient.
func (t *textInput) UpdateFloatingCursor(services.RawFloatingCursorPoint) {}

// ShowAutocorrectionPromptRect implements services.TextInputClient.
func (t *textInput) ShowAutocorrectionPromptRect(int, int) {}

// ConnectionClosed implements services.TextInputClient.
func (t *textInput) ConnectionClosed() { t.conn = nil }

// UpdateEditingValueWithDeltas implements services.DeltaTextInputClient.
func (t *textInput) UpdateEditingValueWithDeltas(deltas []services.TextEditingDelta) {
	for _, d := range deltas {
		t.apply(d.Apply(services.TextEditingValue{Text: d.OldText(), Composing: ui.TextRangeEmpty}))
	}
}

// apply takes the value the input method has now: the text it holds apart from the preedit
// replaces what the line was, at every cursor, and the preedit is shown.
func (t *textInput) apply(v services.TextEditingValue) {
	vm := t.state.Widget().vm
	committed, preedit := v.Text, Preedit{}
	if v.IsComposingRangeValid() && !v.Composing.IsCollapsed() {
		c := v.Composing
		committed = v.Text[:c.Start] + v.Text[c.End:]
		preedit = Preedit{
			Text:  v.Text[c.Start:c.End],
			Begin: min(max(v.Selection.Start()-c.Start, 0), c.End-c.Start),
			End:   min(max(v.Selection.End()-c.Start, 0), c.End-c.Start),
		}
	}
	if committed != t.value.Text {
		if err := vm.typeInput(t.value, committed); err != nil {
			t.state.reportInputError(err)
		}
		// The input method now has a text of its own: give it the line the document has, even
		// when that is the line it had (the edit was refused).
		t.line = -1
	}
	vm.SetPreedit(preedit)
	t.sync()
}

// alignedAffixes is how a text turned into another by replacing one stretch: the byte range
// [from, to) of the old text and what replaced it, with the ends moved to characters.
func alignedAffixes(old, now string) (from, to int, replacement string) {
	from = 0
	for from < len(old) && from < len(now) && old[from] == now[from] {
		from++
	}
	for from > 0 && from < len(old) && !utf8.RuneStart(old[from]) {
		from--
	}
	tail := 0
	for tail < len(old)-from && tail < len(now)-from && old[len(old)-1-tail] == now[len(now)-1-tail] {
		tail++
	}
	for tail > 0 && !utf8.RuneStart(old[len(old)-tail]) {
		tail--
	}
	return from, len(old) - tail, now[from : len(now)-tail]
}

// typeInput does what an input method did to the line it was given: it replaced the stretch
// between two columns by a text. At a cursor the stretch is made of the characters before and
// after it that the text replaces (a composition); a stretch that is the selection is typed
// over. Every cursor gets the same change.
//
// Ported from VS Code: src/vs/editor/browser/controller/editContext/textArea/textAreaEditContextInput.ts
// (the difference between two texts as typing, deleting and replacing)
func (vm *ViewModel) typeInput(was services.TextEditingValue, now string) error {
	from, to, text := alignedAffixes(was.Text, now)
	pre := vm.suggestBefore()
	vm.suggestWillType(text)
	s, e := was.Selection.Start(), was.Selection.End()
	var res edit.Result
	switch {
	case from == s && to == e:
		res = vm.commands().Type(vm.cursors.Selections(), text)
	case s == e:
		prev := utf8.RuneCountInString(was.Text[min(from, s):s])
		next := utf8.RuneCountInString(was.Text[e:max(to, e)])
		res = vm.commands().CompositionType(vm.cursors.Selections(), text, prev, next, 0)
	default:
		res = vm.commands().Type(vm.cursors.Selections(), text)
	}
	if err := vm.apply(res); err != nil {
		return err
	}
	vm.sessionSelections = vm.cursors.Selections()
	vm.columns, vm.occurrences, vm.expansion = nil, nil, nil
	vm.suggestAfter(pre, runEdit, text)
	vm.SelectionsChanged()
	vm.RevealPrimary()
	return nil
}

// caretRect is the rectangle the input method puts its window by, in the text layer's
// coordinates: the caret of the primary cursor, or the caret inside the preedit.
func (r *RenderEditorText) caretRect() ui.Rect {
	g := newGeometry(r.vm, r.factory)
	primary := r.vm.Cursors().Primary().Position()
	rect := g.rectOf(primary)
	if p, ok := r.vm.Preedit(); ok {
		para := g.style.paragraph(r.vm.Paragraphs(), p.Text)
		offset := 0.0
		for _, b := range para.GetBoxesForRange(0, p.End) {
			offset = max(offset, b.Right)
		}
		rect = ui.NewRectFromLTRB(rect.Left+offset, rect.Top, rect.Left+offset+1, rect.Bottom)
	}
	return rect.Shift(ui.Offset{Dx: -r.vm.ScrollLeft(), Dy: -r.top()})
}
