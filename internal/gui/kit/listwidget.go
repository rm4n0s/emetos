package kit

// Ported from VS Code: src/vs/base/browser/ui/tree/abstractTree.ts (the find widget: its input, the toggles of
// the mode and the match type, the close button, the "No results found." message),
// src/vs/base/browser/ui/highlightedlabel/highlightedLabel.ts (the matches of a label in bold, in
// `list.highlightForeground`) and src/vs/workbench/browser/actions/listCommands.ts (the keys that List and
// Tree share). The find box is driven by the key events of the list, which keeps the keyboard focus (as
// ListOverlay's filter is), in place of an input that takes it: a letter typed starts it.

import (
	"unicode"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of the find box of a list or tree (`.monaco-tree-type-filter`).
const (
	findBoxMargin  = 6.0
	findBoxPadding = 3.0
	findBoxWidth   = 200.0
	findBoxRadius  = 6.0
	findBoxShadow  = 8.0
)

// RowState is what a row renderer is told about the look of its row.
type RowState struct {
	// Selected is whether the row is a selected one, and Focused whether it is the focused one.
	Selected, Focused bool
	// Hovered is whether the pointer is over the row.
	Hovered bool
	// Active is whether the list has the keyboard focus (a selected row is in the colors of
	// `list.activeSelection*` then, in those of `list.inactiveSelection*` otherwise).
	Active bool
	// Foreground is the text color the row should use, from the colors of its state.
	Foreground ui.Color
	// Highlights are the parts of the row's label that the find matches (see NewHighlightedLabel), empty
	// while no pattern is in use.
	Highlights []TextRange
	// HighlightForeground is the color of the matching text and HighlightBackground the one behind it (set
	// only while the find filters; transparent otherwise).
	HighlightForeground, HighlightBackground ui.Color
}

// TreeRowState is what a tree's row renderer is told (RowState).
type TreeRowState = RowState

// ListRowState is what a list's row renderer is told (RowState).
type ListRowState = RowState

// rowStateOf is the state of the row of a key.
func rowStateOf(colors Colors, nav *rowNav, key, hover string, active bool) RowState {
	focusKey, _ := nav.Focused()
	st := RowState{Selected: nav.IsSelected(key), Focused: key == focusKey, Hovered: key == hover, Active: active}
	st.HighlightForeground = colors.ListHighlightForeground
	if active && (st.Focused || st.Selected) {
		st.HighlightForeground = colors.ListFocusHighlightForeground
	}
	if ranges, ok := nav.FindMatch(key); ok {
		st.Highlights = ranges
		if nav.find.mode == FindModeFilter {
			st.HighlightBackground = colors.ListFilterMatchBackground
		}
	}
	return st
}

// NewHighlightedLabel is a line of text in the foreground of the row's state with the parts the find matched
// in bold and in the highlight colors of the state (VS Code's `HighlightedLabel`). It is what the default
// renderers of List and Tree use; a renderer with an icon puts it after the icon.
func NewHighlightedLabel(bc widgets.BuildContext, text string, st RowState) widgets.Widget {
	base := TextStyleOf(bc, listFontSize, st.Foreground)
	if len(st.Highlights) == 0 {
		return widgets.NewText(text, widgets.TextOpts{Style: base, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	opts := painting.TextStyleOpts{Color: foundation.Some(st.HighlightForeground), FontWeight: ui.FontWeightBold}
	if st.HighlightBackground.Alpha() != 0 {
		opts.BackgroundColor = foundation.Some(st.HighlightBackground)
	}
	marked := base.CopyWith(opts)
	var spans []painting.InlineSpan
	at := 0
	for _, r := range st.Highlights {
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
	root := painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: spans})
	return widgets.NewRichText(root, widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// navKey is a key command that lists and trees share.
type navKey int

const (
	navKeyNone navKey = iota
	navKeyDown
	navKeyUp
	navKeyFirst
	navKeyLast
	navKeyPageDown
	navKeyPageUp
	navKeySelect
	navKeyToggle
	navKeyClear
	navKeyExpandDown
	navKeyExpandUp
	navKeySelectAll
	navKeyToggleSelection
)

// keyMods are the modifiers held while a key is pressed.
type keyMods struct{ ctrl, shift, alt, meta bool }

// commonKey is the command that a key is, among the keys that List and Tree share (Ctrl+Left and the
// arrows Left and Right are the tree's).
func commonKey(key services.LogicalKeyboardKey, m keyMods, multi bool) navKey {
	enter := key == services.LogicalKeyboardKeyEnter || key == services.LogicalKeyboardKeyNumpadEnter
	switch {
	case m.alt || m.meta:
		return navKeyNone
	case m.ctrl && m.shift:
		if enter && multi {
			return navKeyToggleSelection
		}
	case m.ctrl:
		switch {
		case key == services.LogicalKeyboardKeyKeyA && multi:
			return navKeySelectAll
		case key == services.LogicalKeyboardKeySpace && multi:
			return navKeyToggleSelection
		}
	case m.shift:
		switch {
		case key == services.LogicalKeyboardKeyArrowDown && multi:
			return navKeyExpandDown
		case key == services.LogicalKeyboardKeyArrowUp && multi:
			return navKeyExpandUp
		}
	default:
		switch {
		case key == services.LogicalKeyboardKeyArrowDown:
			return navKeyDown
		case key == services.LogicalKeyboardKeyArrowUp:
			return navKeyUp
		case key == services.LogicalKeyboardKeyHome:
			return navKeyFirst
		case key == services.LogicalKeyboardKeyEnd:
			return navKeyLast
		case key == services.LogicalKeyboardKeyPageDown:
			return navKeyPageDown
		case key == services.LogicalKeyboardKeyPageUp:
			return navKeyPageUp
		case enter:
			return navKeySelect
		case key == services.LogicalKeyboardKeySpace:
			return navKeyToggle
		case key == services.LogicalKeyboardKeyEscape:
			return navKeyClear
		}
	}
	return navKeyNone
}

// printable says that a key press typed text: it has characters and they are all printable.
func printable(ch string) bool {
	if ch == "" {
		return false
	}
	for _, r := range ch {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// findKey is the part of the keyboard that is the find's: Escape closes the find, Backspace and printable
// characters edit the pattern (a letter starts it), Enter ends the typing and goes on as a selection. enabled
// says that typing starts a find. It reports whether the key was used and the scroll position that shows the
// focus.
func findKey(nav *rowNav, e services.KeyEvent, m keyMods, enabled bool) (bool, foundation.Opt[float64]) {
	none := foundation.None[float64]()
	key := e.LogicalKey()
	f := &nav.find
	if f.open {
		switch {
		case key == services.LogicalKeyboardKeyEscape:
			nav.CloseFind()
			return true, none
		case key == services.LogicalKeyboardKeyBackspace && !m.ctrl && !m.alt && !m.meta:
			nav.SetFindTyping(true)
			runes := []rune(f.pattern)
			if len(runes) == 0 {
				return true, none
			}
			return true, nav.SetFindPattern(string(runes[:len(runes)-1]))
		case (key == services.LogicalKeyboardKeyEnter || key == services.LogicalKeyboardKeyNumpadEnter) && f.typing:
			nav.SetFindTyping(false)
			return false, none
		}
	}
	if !enabled || m.ctrl || m.alt || m.meta {
		return false, none
	}
	ch := e.Character()
	if !printable(ch) || ch == " " && (!f.open || !f.typing) {
		return false, none
	}
	nav.OpenFind()
	nav.SetFindTyping(true)
	return true, nav.SetFindPattern(f.pattern + ch)
}

// findBoxState holds the input of the find box, which shows the pattern.
type findBoxState struct {
	controller *widgets.TextEditingController
	node       *widgets.FocusNode
}

func newFindBoxState() *findBoxState {
	return &findBoxState{
		controller: widgets.NewTextEditingController(""),
		node:       widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "FindBox", CannotRequestFocus: true, SkipTraversal: true}),
	}
}

// sync shows the pattern in the input.
func (f *findBoxState) sync(pattern string) {
	if f.controller.Text() != pattern {
		f.controller.SetText(pattern)
	}
}

func (f *findBoxState) dispose() {
	f.controller.Dispose()
	f.node.Dispose()
}

// buildFindBox builds the find box of a list or tree, or nil when no find is open: an input showing the
// pattern, the toggles of the filter mode and the fuzzy match, a close button and, when nothing matches, the
// message "No results found.". onChanged is called after the box changed the model.
func buildFindBox(bc widgets.BuildContext, nav *rowNav, fb *findBoxState, onChanged func(), onClose func()) widgets.Widget {
	st := nav.Find()
	if !st.Open {
		return nil
	}
	colors := ThemeOf(bc)
	placeholder := "Type to search"
	if st.Mode == FindModeFilter {
		placeholder = "Type to filter"
	}
	input := widgets.NewIgnorePointer(NewInputBox(fb.controller, fb.node, InputBoxOpts{Placeholder: placeholder}))
	filter := NewToggle(fonts.CodiconListFilter, ToggleOpts{Checked: st.Mode == FindModeFilter, OnChanged: func(on bool) {
		if on {
			nav.SetFindMode(FindModeFilter)
		} else {
			nav.SetFindMode(FindModeHighlight)
		}
		onChanged()
	}})
	fuzzy := NewToggle(fonts.CodiconSearchFuzzy, ToggleOpts{Checked: st.MatchType == FindMatchFuzzy, OnChanged: func(on bool) {
		if on {
			nav.SetFindMatchType(FindMatchFuzzy)
		} else {
			nav.SetFindMatchType(FindMatchContiguous)
		}
		onChanged()
	}})
	closeButton := NewIconButton(fonts.CodiconClose, IconButtonOpts{Tooltip: "Close", OnPressed: onClose})
	row := widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(input), findGap(), filter, findGap(), fuzzy, findGap(), closeButton,
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	var body widgets.Widget = row
	outline := colors.ListFilterWidgetOutline
	if st.NoResults() {
		outline = colors.ListFilterWidgetNoMatchesOutline
		message := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(2, 3, 2, 1),
			widgets.NewText("No results found.", widgets.TextOpts{Style: TextStyleOf(bc, 12, colors.Foreground), MaxLines: foundation.Some(1)}))
		body = widgets.NewColumn([]widgets.Widget{row, message}, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	box := widgets.NewContainer(body, widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsAll(findBoxPadding),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.ListFilterWidgetBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(outline), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusVertical(ui.Radius{}, ui.NewRadiusCircular(findBoxRadius)),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.ListFilterWidgetShadow), BlurRadius: findBoxShadow, SpreadRadius: 2})},
		}),
	})
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(findBoxMargin, 0, findBoxMargin, 4),
		widgets.NewAlign(widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(findBoxWidth)}), box),
			widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
}

// findGap is the room between the parts of the find box.
func findGap() widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)})
}

// rowDecoration is the decoration of a row: the background and outline of its state, the background of a row
// that is a drop target (inside) and the lines above and below a row that is dropped before or after.
func rowDecoration(colors Colors, background, outline ui.Color, inside bool) *painting.BoxDecoration {
	if inside {
		background = colors.ListDropBackground
	}
	return painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:  foundation.Some(background),
		Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(outline), Width: foundation.Some(1.0)}),
	})
}

// composeArea puts the pieces of a list or tree together: the find box above the rows, the background of a
// drop on the whole view, and the drag image over everything.
func composeArea(bc widgets.BuildContext, colors Colors, drag *dragController, header, element widgets.Widget, record func(widgets.BuildContext)) widgets.Widget {
	area := widgets.NewBuilder(func(inner widgets.BuildContext) widgets.Widget {
		record(inner)
		layers := []widgets.Widget{}
		if drag.rootDrop() {
			layers = append(layers, widgets.NewColoredBox(colors.ListDropBackground, widgets.NewSizedBox(nil)))
		}
		layers = append(layers, element)
		if fb := drag.feedback(inner, colors); fb != nil {
			layers = append(layers, fb)
		}
		return widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitExpand, ClipBehavior: ui.ClipNone})
	})
	if header == nil {
		header = widgets.NewSizedBox(nil) // the same tree of widgets with or without the box, so the rows keep their state
	}
	return widgets.NewColumn([]widgets.Widget{header, widgets.NewExpanded(area)}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}
