package scm

import (
	"math"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The input: the box in which the message of the commit is written, under the header of a repository, and the validation message
// under it. It is a row of the list like VS Code's (the tree of scmViewPane.ts has the input and the action button as elements),
// as high as the box wants to be between scm.inputMinLineCount and scm.inputMaxLineCount lines.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmInput.ts (SCMInputWidget) and scmViewPane.ts (InputRenderer)

const (
	// inputRowMargin is the room above and below the box in its row (the row is the content height plus 10 px, 8 of them outside
	// the border).
	inputRowMargin = 4.0
	// inputPaddingV is the room above and below the lines of the box (`padding: { top: 2, bottom: 2 }`).
	inputPaddingV = 2.0
	// inputRadius is the radius of the corners of the box (`border-radius: 4px`).
	inputRadius = 4.0
	// defaultInputFontSize is scm.inputFontSize.
	defaultInputFontSize = 13.0
)

// InputFont is the font of the text of the input (scm.inputFontFamily and scm.inputFontSize).
type InputFont struct {
	// Family is the family of the font; empty is the UI font of the workbench (the value "default").
	Family string
	// Mono is the monospace font of the desktop: the value "editor" with no editor.fontFamily.
	Mono bool
	// Size is the size in pixels; 13 when it is not set.
	Size float64
}

// inputUI is what the view keeps for the input of a repository, for as long as the repository is there: the text and the focus node
// of the box (the box itself is built only while its row is on screen), its validation, its height and its context keys.
type inputUI struct {
	src *scmcore.Source
	ctl *kit.InputBoxController
	// button is the focus node of the action button and dropdown the controller that opens its dropdown.
	button   *widgets.FocusNode
	dropdown splitButtonController

	// height is the height of the box as the box last said; 0 before it did.
	height float64
	// val is the result of the last validation, dismissed says that Escape took its message away (the border keeps the color).
	val       *scmcore.Validation
	dismissed bool
	focused   bool
	// validated is the validation generation, and asks the number of focus requests, that the view saw.
	validated uint64
	asks      uint64

	scope      *contextkey.Scope
	unregister func()
	selection  foundation.ListenerHandle
	// pushing is set while the view puts the text of the model in the box.
	pushing bool
}

// inputOf is the state of the input of a source control; it is made the first time it is asked for.
func (v *View) inputOf(src *scmcore.Source) *inputUI {
	if ui, ok := v.inputs[src]; ok {
		return ui
	}
	in := src.Input()
	u := &inputUI{
		src: src, ctl: kit.NewInputBoxController(in.Value()),
		button:    widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "SCMActionButton"}),
		validated: in.ValidationGeneration(), asks: in.FocusRequests(),
	}
	if v.opts.Keys != nil {
		u.scope = v.opts.Keys.Child()
		u.scope.Set("scmRepository", src.Key())
		if v.opts.Scopes != nil {
			u.unregister = v.opts.Scopes.Register(u.ctl.FocusNode(), u.scope)
		}
	}
	u.selection = u.ctl.TextController().AddListener(func() { v.updateInputKeys(u) })
	v.inputs[src] = u
	v.validate(u)
	return u
}

// dropInput forgets the input of a source control that is gone.
func (v *View) dropInput(src *scmcore.Source) {
	u, ok := v.inputs[src]
	if !ok {
		return
	}
	delete(v.inputs, src)
	u.ctl.TextController().RemoveListener(u.selection)
	if u.unregister != nil {
		u.unregister()
	}
	if u.scope != nil {
		u.scope.Dispose()
	}
	u.button.Dispose()
	u.ctl.Dispose()
}

// pruneInputs forgets the inputs of the source controls that were removed.
func (v *View) pruneInputs() {
	for src := range v.inputs {
		if src.Removed() {
			v.dropInput(src)
		}
	}
}

// lineHeight is the height of a line of the box: VS Code's `Math.round(fontSize * 1.5)`.
func lineHeight(fontSize float64) float64 { return math.Round(fontSize * 1.5) }

// inputLines are scm.inputMinLineCount and scm.inputMaxLineCount, each clamped to 1 to 50 as VS Code does, the maximum not under
// the minimum.
func (v *View) inputLines() (minLines, maxLines int) {
	minLines, maxLines = 1, 10
	if v.opts.InputLines != nil {
		minLines, maxLines = v.opts.InputLines()
	}
	minLines = min(max(minLines, 1), 50)
	maxLines = min(max(maxLines, 1), 50)
	return minLines, max(maxLines, minLines)
}

func (v *View) inputFont() InputFont {
	f := InputFont{Size: defaultInputFontSize}
	if v.opts.InputFont != nil {
		f = v.opts.InputFont()
	}
	if f.Size <= 0 {
		f.Size = defaultInputFontSize
	}
	return f
}

// boxHeight is the height of the box when it has the lines of a number: its border, its padding and the lines.
func (v *View) boxHeight(lines int) float64 {
	return 2 + 2*inputPaddingV + float64(lines)*lineHeight(v.inputFont().Size)
}

// inputRowHeight is the height of the row of an input.
func (v *View) inputRowHeight(src *scmcore.Source) float64 {
	minLines, maxLines := v.inputLines()
	h := v.boxHeight(minLines)
	if u, ok := v.inputs[src]; ok && u.height > 0 {
		h = min(max(u.height, h), v.boxHeight(maxLines))
	}
	return h + 2*inputRowMargin
}

// inputHeight is what the box says about its height (the lines that its text needs, between the minimum and the maximum).
func (v *View) inputHeight(u *inputUI, h float64) {
	if v.disposed || u.height == h {
		return
	}
	u.height = h
	v.list.RefreshHeights()
}

// placeholder is the text shown in an empty box: the provider's, with {0} replaced by the key of the accept command.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmInput.ts (getPlaceholderText)
func (v *View) placeholder(src *scmcore.Source) string {
	label := "Ctrl+Enter"
	if v.opts.AcceptKeyLabel != nil {
		if l := v.opts.AcceptKeyLabel(); l != "" {
			label = l
		}
	}
	return strings.ReplaceAll(src.Input().Placeholder(), "{0}", label)
}

// message is the validation message as the box shows it: the border has the color of the level while there is a validation, and
// the text is under the box while the box has the focus and Escape did not take it away.
func (u *inputUI) message() *kit.InputMessage {
	if u.val == nil {
		return nil
	}
	t := kit.ValidationInfo
	switch u.val.Level {
	case scmcore.ValidationError:
		t = kit.ValidationError
	case scmcore.ValidationWarning:
		t = kit.ValidationWarning
	}
	m := &kit.InputMessage{Content: u.val.Message, Type: t}
	if u.dismissed {
		m.Content = ""
	}
	return m
}

// showsMessage tells whether the validation message is on screen: the context key scmInputHasValidationMessage.
func (u *inputUI) showsMessage() bool {
	return u.focused && u.val != nil && u.val.Message != "" && !u.dismissed
}

// validate asks the provider for the validation of the text of the box and shows the answer (VS Code does it on each change of the
// text, throttled by 200 ms).
func (v *View) validate(u *inputUI) {
	in := u.src.Input()
	u.validated = in.ValidationGeneration()
	u.val = in.Validate(u.ctl.Value())
	u.dismissed = false
	v.updateInputKeys(u)
	if !v.disposed {
		v.notify()
	}
}

// updateInputKeys sets the context keys of the input: whether the caret is at the very start or the very end of the text (the
// keys of the history), whether some text is selected and whether the validation message is shown.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmInput.ts (scmInputIsInFirstPosition, scmInputIsInLastPosition,
// scmInputHasValidationMessage)
func (v *View) updateInputKeys(u *inputUI) {
	if u.scope == nil {
		return
	}
	text, sel := u.ctl.Value(), u.ctl.TextController().Selection()
	caret, selected := len(text), false
	if sel.IsValid() {
		caret, selected = min(sel.ExtentOffset, len(text)), !sel.IsCollapsed()
	}
	u.scope.Batch(func() {
		u.scope.Set("scmInputIsInFirstPosition", caret == 0)
		u.scope.Set("scmInputIsInLastPosition", caret == len(text))
		u.scope.Set("editorHasSelection", selected)
		u.scope.Set("scmInputHasValidationMessage", u.showsMessage())
	})
}

// inputFocused follows the focus into the box and out of it. Focusing the input makes its repository the focused one and takes the
// focus off the rows (the input is not one of them), and shows the validation again.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (open of an SCMInput)
func (v *View) inputFocused(u *inputUI, has bool) {
	if u.focused == has {
		return
	}
	u.focused = has
	u.dismissed = false
	if has {
		v.list.ClearFocus()
		v.model.Focus(u.src)
	}
	v.updateInputKeys(u)
	v.notify()
}

// inputChanged follows the text of the box: the user typed, or the view put the text of the model in.
func (v *View) inputChanged(u *inputUI, text string) {
	u.src.Input().SetTransientValue(text)
	v.validate(u)
}

// syncInputs brings the boxes up to date with the model: the text the provider or the history set, a new validation function, and
// the requests for the focus.
func (v *View) syncInputs() {
	for src, u := range v.inputs {
		if src.Removed() {
			continue
		}
		in := src.Input()
		if value := in.Value(); u.ctl.Value() != value {
			u.pushing = true
			u.ctl.SetValue(value)
			if in.ChangeReason() == scmcore.ChangedByHistoryPrevious {
				u.ctl.Select(0, 0)
			}
			u.pushing = false
			v.validate(u)
		} else if in.ValidationGeneration() != u.validated {
			v.validate(u)
		}
		if asks := in.FocusRequests(); asks != u.asks {
			u.asks = asks
			v.focusInput(u)
		}
	}
}

// focusInput gives the box the keyboard focus once its row is on screen.
func (v *View) focusInput(u *inputUI) {
	if v.opts.Reveal != nil {
		v.opts.Reveal()
	}
	focus := func() {
		if !v.disposed && !u.src.Removed() {
			u.ctl.Focus()
		}
	}
	if v.opts.AfterFrame != nil {
		v.opts.AfterFrame(focus)
		return
	}
	focus()
}

// FocusInput gives the keyboard focus to the input of a source control, when its row is on screen.
func (v *View) FocusInput(src *scmcore.Source) {
	if u, ok := v.inputs[src]; ok {
		v.focusInput(u)
	}
}

// ClearValidation takes the validation message of the input of the source control away (scm.clearValidation); the border keeps the
// color of the level until the text changes.
func (v *View) ClearValidation(src *scmcore.Source) {
	u, ok := v.inputs[src]
	if !ok || u.dismissed {
		return
	}
	u.dismissed = true
	v.updateInputKeys(u)
	v.notify()
}

// ValidationShown tells whether the validation message of an input is on screen.
func (v *View) ValidationShown(src *scmcore.Source) bool {
	u, ok := v.inputs[src]
	return ok && u.showsMessage()
}

// InputFocused tells whether the box of a source control has the keyboard focus.
func (v *View) InputFocused(src *scmcore.Source) bool {
	u, ok := v.inputs[src]
	return ok && u.ctl.HasFocus()
}

// InputText is the text of the box of a source control, as the box has it (tests).
func (v *View) InputText(src *scmcore.Source) string {
	if u, ok := v.inputs[src]; ok {
		return u.ctl.Value()
	}
	return ""
}

// SetInputSelection selects a range of the text of the box of a source control, in bytes; a caret when the ends are equal.
func (v *View) SetInputSelection(src *scmcore.Source, start, end int) {
	if u, ok := v.inputs[src]; ok {
		u.ctl.Select(start, end)
	}
}

// SetInputCaret puts the caret of the box of a source control at a byte of its text.
func (v *View) SetInputCaret(src *scmcore.Source, at int) { v.SetInputSelection(src, at, at) }

// InputSelection is the selected range of the box of a source control (equal ends for a caret).
func (v *View) InputSelection(src *scmcore.Source) (start, end int) {
	if u, ok := v.inputs[src]; ok {
		start, end, _ = u.ctl.Selection()
	}
	return start, end
}

// renderInput builds the row of an input: the box with the validation message that opens under it.
func (v *View) renderInput(_ widgets.BuildContext, r *row) widgets.Widget {
	u := v.inputOf(r.src)
	in := r.src.Input()
	minLines, maxLines := v.inputLines()
	font := v.inputFont()
	box := kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Controller: u.ctl, MultiLine: true, Placeholder: v.placeholder(r.src), Disabled: !in.Enabled(),
		MinLines: minLines, MaxLines: maxLines,
		FontSize: foundation.Some(font.Size), LineHeight: foundation.Some(lineHeight(font.Size)), PaddingV: foundation.Some(inputPaddingV),
		FontFamily: font.Family, Monospace: font.Mono, Radius: foundation.Some(inputRadius),
		Message:          u.message(),
		OnChanged:        func(text string) { v.inputChanged(u, text) },
		OnFocusChange:    func(has bool) { v.inputFocused(u, has) },
		OnHeightChange:   func(h float64) { v.inputHeight(u, h) },
		SelectAllOnFocus: foundation.Some(false), CollapseSelectionOnEscape: true,
	})
	return widgets.NewKeyedSubtree(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(rowInsetLeft, inputRowMargin, rowInsetRight, inputRowMargin), box),
		widgets.WidgetOpts{Key: InputKey(r.src)})
}

// renderActionButton builds the row of the action button: a wide button with the primary command of the provider and, when it has
// more commands, the arrow of a dropdown.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (SCMActionButton.setButton)
func (v *View) renderActionButton(_ widgets.BuildContext, r *row) widgets.Widget {
	b := r.src.ActionButton()
	if b == nil {
		return widgets.NewSizedBox(nil)
	}
	u := v.inputOf(r.src)
	tooltip := b.Command.Tooltip
	if tooltip == "" {
		tooltip = b.Description
	}
	cmd := b.Command
	btn := newSplitButton(splitButtonOpts{
		Label: cmd.Title, Tooltip: tooltip, Disabled: !b.Enabled || cmd.ID == "",
		Dropdown: len(b.Secondary) > 0, FocusNode: u.button, Controller: &u.dropdown,
		OnPress: func() {
			v.model.Focus(r.src)
			v.run(cmd.ID, cmd.Args)
		},
		OnDropdown: func(at ui.Offset) {
			v.model.Focus(r.src)
			if v.opts.ShowItems != nil {
				v.opts.ShowItems(dropdownItems(b.Secondary), at)
			}
		},
	})
	return widgets.NewKeyedSubtree(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(rowInsetLeft, (actionButtonRowHeight-actionButtonHeight)/2, rowInsetRight, (actionButtonRowHeight-actionButtonHeight)/2), btn),
		widgets.WidgetOpts{Key: ActionButtonKey(r.src)})
}

// dropdownItems are the menu items of the commands in the dropdown of the button: the groups in order with a separator between two
// of them (empty groups leave no separator).
func dropdownItems(groups [][]scmcore.Command) []kit.MenuItem {
	var items []kit.MenuItem
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if len(items) > 0 {
			items = append(items, kit.SeparatorItem())
		}
		for _, c := range g {
			items = append(items, kit.MenuItem{ID: c.ID, Label: c.Title, Data: c.Args})
		}
	}
	return items
}

// OpenActionDropdown opens the dropdown of the action button of a source control, when its row is on screen and the button has one.
func (v *View) OpenActionDropdown(src *scmcore.Source) bool {
	u, ok := v.inputs[src]
	return ok && u.dropdown.Open()
}
