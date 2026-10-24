package keybindingsui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// jsonString is the JSON text of a string.
func jsonString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// byteRanges turns matches in runes of a text into ranges in its bytes.
func byteRanges(text string, matches []fuzzy.Match) []kit.TextRange {
	if len(matches) == 0 {
		return nil
	}
	offsets := make([]int, 0, len(text)+1)
	for i := range text {
		offsets = append(offsets, i)
	}
	offsets = append(offsets, len(text))
	at := func(r int) int { return offsets[min(max(r, 0), len(offsets)-1)] }
	out := make([]kit.TextRange, 0, len(matches))
	for _, m := range matches {
		out = append(out, kit.TextRange{Start: at(m.Start), End: at(m.End)})
	}
	return out
}

// highlighted is a text with the parts a search matched in bold and in the highlight color, ending in an ellipsis
// where it does not fit.
func highlighted(text string, matches []fuzzy.Match, style *painting.TextStyle, st kit.RowState) widgets.Widget {
	ranges := byteRanges(text, matches)
	if len(ranges) == 0 {
		return widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	marked := style.CopyWith(painting.TextStyleOpts{Color: foundation.Some(st.HighlightForeground), FontWeight: ui.FontWeightBold})
	var spans []painting.InlineSpan
	at := 0
	for _, r := range ranges {
		r.Start, r.End = min(max(r.Start, at), len(text)), min(r.End, len(text))
		if r.End <= r.Start {
			continue
		}
		if r.Start > at {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text[at:r.Start])}))
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text[r.Start:r.End]), Style: marked}))
		at = r.End
	}
	if at < len(text) {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text[at:])}))
	}
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: spans}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// chordNames are the names of the keys of each chord of a keybinding as the key caps show them: the modifiers
// in the order of the labels, then the key.
func chordNames(kb keybinding.Keybinding) [][]string {
	out := make([][]string, len(kb))
	for i, c := range kb {
		var names []string
		if c.Ctrl {
			names = append(names, "Ctrl")
		}
		if c.Shift {
			names = append(names, "Shift")
		}
		if c.Alt {
			names = append(names, "Alt")
		}
		if c.Meta {
			names = append(names, "Super")
		}
		if !c.IsModifierKey() {
			names = append(names, c.Key.String())
		}
		out[i] = names
	}
	return out
}

// keyHighlights says for each key of the caps whether a search matched it.
func keyHighlights(kb keybinding.Keybinding, m *keybinding.KeyMatches) func(chord, key int) bool {
	if m == nil {
		return nil
	}
	return func(ci, ki int) bool {
		if ci >= len(kb) || ci > 1 {
			return false
		}
		match := m.First
		if ci == 1 {
			match = m.Chord
		}
		c := kb[ci]
		i := 0
		for _, set := range []struct{ has, matched bool }{{c.Ctrl, match.Ctrl}, {c.Shift, match.Shift}, {c.Alt, match.Alt}, {c.Meta, match.Meta}} {
			if !set.has {
				continue
			}
			if i == ki {
				return set.matched
			}
			i++
		}
		return i == ki && match.Key
	}
}

// renderRow draws a row of the table: the actions, then the command, the keybinding, the when-clause and the
// source.
func (s *editorState) renderRow(bc widgets.BuildContext, row kit.ListViewRow, st kit.RowState) widgets.Widget {
	colors := kit.ThemeOf(bc)
	res, ok := s.resultFor(row.Key)
	if !ok {
		return widgets.NewSizedBox(nil)
	}
	r := res.Row
	height := s.heightOf(row.Index)
	fg := st.Foreground
	// the text that says less than the rest (the id, the when-clause) is dimmer
	dim := fg.WithOpacity(0.8)

	// the command
	command := s.commandCell(bc, res, fg, dim, st)
	// the keybinding
	var key widgets.Widget = widgets.NewSizedBox(nil)
	if r.Bound() {
		key = kit.NewKeyCaps(bc, chordNames(r.Keybinding), kit.KeyCapsOpts{
			Lit: st.Selected || st.Focused || st.Hovered, Foreground: fg, Highlight: keyHighlights(r.Keybinding, res.Keybinding),
		})
	}
	// the when-clause
	when := s.whenCell(bc, res, dim, st)
	// the source
	source := highlighted(r.Source.String(), res.Source, kit.TextStyleOf(bc, fontSize, fg), st)

	cells := []widgets.Widget{s.actionsCell(res, st)}
	cells = append(cells, s.cells(
		columnCell(command, commandFlex), columnCell(widgets.NewFittedBox(key, widgets.FittedBoxOpts{Fit: painting.BoxFitScaleDown, Alignment: painting.AlignmentCenterLeft}), keybindingFlex),
		columnCell(when, whenFlex), columnCell(source, sourceFlex))...)
	var content widgets.Widget = widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	if !st.Selected && !st.Focused && !st.Hovered && row.Index%2 == 1 {
		content = widgets.NewColoredBox(colors.KeybindingTableRowsBackground, content)
	}
	return widgets.NewKeyedSubtree(widgets.NewSizedBox(content, widgets.SizedBoxOpts{Height: foundation.Some(height)}),
		widgets.WidgetOpts{Key: RowKey(r.ID)})
}

// actionsCell is the button of the row that defines its keybinding (a pencil, a plus for a command without one),
// shown while the row is selected, focused or under the pointer.
func (s *editorState) actionsCell(res keybinding.Result, st kit.RowState) widgets.Widget {
	cell := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(actionsWidth)})
	if !st.Selected && !st.Focused && !st.Hovered {
		return cell
	}
	r := res.Row
	icon, tip, id := fonts.CodiconEdit, "Change Keybinding", "keybindings.editor.defineKeybinding"
	if !r.Bound() {
		icon, tip = fonts.CodiconAdd, "Add Keybinding"
	}
	if l := s.keyLabel(id); l != "" {
		tip += " (" + l + ")"
	}
	button := kit.NewIconButton(icon, kit.IconButtonOpts{Tooltip: tip, OnPressed: func() { s.openDefine(r.ID, false) }})
	return widgets.NewSizedBox(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 0, 0, 0),
		widgets.NewAlign(button, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})), widgets.SizedBoxOpts{Width: foundation.Some(actionsWidth)})
}

// commandCell is the name of the command; under it, the id when the search matched it, or alone when the command
// has no title. The hover has both.
func (s *editorState) commandCell(bc widgets.BuildContext, res keybinding.Result, fg, dim ui.Color, st kit.RowState) widgets.Widget {
	r := res.Row
	code := kit.TextStyleOf(bc, fontSize*0.9, dim).CopyWith(painting.TextStyleOpts{FontFamily: s.codeFont(), FontFamilyFallback: []string{"monospace"}})
	var content widgets.Widget
	switch {
	case r.Label == "":
		content = highlighted(r.Command, res.ID, code, st)
	case s.twoLines(res):
		content = widgets.NewColumn([]widgets.Widget{
			highlighted(r.Label, res.Label, kit.TextStyleOf(bc, fontSize, fg), st),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(2.0)}),
			highlighted(r.Command, res.ID, code, st),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin})
	default:
		content = highlighted(r.Label, res.Label, kit.TextStyleOf(bc, fontSize, fg), st)
	}
	tip := r.Command
	if r.Label != "" {
		tip = r.Label + " (" + r.Command + ")"
	}
	return kit.NewHover(content, kit.HoverOpts{Text: tip, Compact: true})
}

// whenCell is the when-clause of the row in the code font, "-" when there is none; while it is edited, the box
// that edits it.
func (s *editorState) whenCell(bc widgets.BuildContext, res keybinding.Result, dim ui.Color, st kit.RowState) widgets.Widget {
	r := res.Row
	if s.whenRow == r.ID {
		return kit.NewInputBox(nil, nil, kit.InputBoxOpts{
			Key: foundation.NewValueKey("keybindingsui-when"), Controller: s.whenInput, Validate: validateWhen,
			OnSubmit: s.acceptWhen, Autofocus: true,
			OnFocusChange: func(focused bool) {
				if focused {
					return
				}
				// a click in the box moves the focus to the table and back: only a box that did not get it back
				// is left
				binding := widgets.WidgetsBindingOf(s.Context())
				binding.Scheduler().ScheduleFrame()
				binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
					if s.Mounted() && s.whenRow == r.ID && !s.whenInput.HasFocus() {
						s.whenRow = ""
						s.SetState(func() {})
					}
				})
			},
		})
	}
	code := kit.TextStyleOf(bc, fontSize*0.9, dim).CopyWith(painting.TextStyleOpts{FontFamily: s.codeFont(), FontFamilyFallback: []string{"monospace"}})
	if r.When == "" {
		return widgets.NewText("-", widgets.TextOpts{Style: kit.TextStyleOf(bc, fontSize, dim)})
	}
	return kit.NewHover(highlighted(r.When, res.When, code, st), kit.HoverOpts{Text: r.When, Compact: true})
}

// showMenu opens the context menu of a row at the pointer.
func (s *editorState) showMenu(row kit.ListViewRow, at ui.Offset) {
	res, ok := s.resultFor(row.Key)
	if !ok {
		return
	}
	r := res.Row
	item := func(label, command string, enabled bool, run func()) kit.MenuItem {
		return kit.MenuItem{ID: command, Label: label, Keybinding: s.keyLabel(command), Disabled: !enabled, OnRun: run}
	}
	define := "Change Keybinding..."
	items := []kit.MenuItem{
		item("Copy", "keybindings.editor.copyKeybindingEntry", true, func() { s.copyRow(r, "entry") }),
		item("Copy Command ID", "keybindings.editor.copyCommandKeybindingEntry", true, func() { s.copyRow(r, "command") }),
		item("Copy Command Title", "keybindings.editor.copyCommandTitle", r.Label != "", func() { s.copyRow(r, "title") }),
		kit.SeparatorItem(),
	}
	if !r.Bound() {
		define = "Add Keybinding..."
		items = append(items, item(define, "keybindings.editor.defineKeybinding", true, func() { s.openDefine(r.ID, false) }))
	} else {
		items = append(items,
			item(define, "keybindings.editor.defineKeybinding", true, func() { s.openDefine(r.ID, false) }),
			item("Add Keybinding...", "keybindings.editor.addKeybinding", true, func() { s.openDefine(r.ID, true) }))
	}
	items = append(items,
		kit.SeparatorItem(),
		item("Remove Keybinding", "keybindings.editor.removeKeybinding", r.Bound(), func() { s.remove(r) }),
		item("Reset Keybinding", "keybindings.editor.resetKeybinding", r.Source == keybinding.SourceUser, func() { s.reset(r) }),
		kit.SeparatorItem(),
		item("Change When Expression", "keybindings.editor.defineWhenExpression", r.Bound(), func() { s.openWhen(r) }),
		kit.SeparatorItem(),
		item("Show Same Keybindings", "keybindings.editor.showConflicts", r.Bound(), func() { s.showSame(r) }),
	)
	s.menu.Show(at, items)
}
