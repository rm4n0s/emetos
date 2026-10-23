package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

const (
	// snippetPickerRows is the number of rows of the list of snippets and choices in view.
	snippetPickerRows = 10
	// snippetPickerWidth is the width of that list.
	snippetPickerWidth = 320.0
	// snippetPickerRow is the height of a row, as kit.ListOverlay's.
	snippetPickerRow = 22.0
)

// snippetPicker is the list of the choices of a snippet, of the snippets the Tab key could insert,
// or of the snippets to insert, below the caret (above it when it does not fit). It takes the
// focus while it is open and gives it back to the editor.
func (s *editorState) snippetPicker(vm *ViewModel, p *SnippetPicker) widgets.Widget {
	items := make([]kit.ListItem, len(p.Items))
	for i, it := range p.Items {
		items[i] = kit.ListItem{Label: it.Label, Description: it.Description}
	}
	height := float64(min(len(items), snippetPickerRows))*snippetPickerRow + 2
	left, top := s.gutterWidth, 0.0
	bottom := foundation.None[float64]()
	if s.textRO != nil && s.textRO.Attached() {
		rect := s.textRO.caretRect()
		left += rect.Left
		top = rect.Bottom
		if vm.viewport > 0 && top+height > vm.viewport && rect.Top-height >= 0 {
			bottom = foundation.Some(vm.viewport - rect.Top)
		}
	}
	list := kit.NewListOverlay(items, kit.ListOverlayOpts{
		Key: foundation.NewValueKey(p), StealFocus: true, Filterable: p.Filterable, Rows: snippetPickerRows, Width: foundation.Some(snippetPickerWidth),
		OnSelect: func(i int) {
			p.Accept(i)
			s.node.RequestFocus()
		},
		OnDismiss: func() {
			p.Dismiss()
			s.node.RequestFocus()
		},
	})
	pos := widgets.PositionedOpts{Left: foundation.Some(left)}
	if bottom.IsSome() {
		pos.Bottom = bottom
	} else {
		pos.Top = foundation.Some(top)
	}
	return widgets.NewStack([]widgets.Widget{widgets.NewPositioned(list, pos)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}
