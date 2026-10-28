package editor

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The suggest widget (SPEC §9.12 "As built (step 12.5)"): the list of items below the word being completed, above it when
// there is no room, and the details pane at the side of it.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestWidget.ts, suggestWidgetRenderer.ts, suggestWidgetDetails.ts
// and media/suggest.css

// Metrics of the widget (suggest.css, suggestWidget.ts getLayoutInfo); the ones of the list are in kit.
const (
	suggestWidth           = kit.SuggestWidth
	suggestDetailsWidth    = 330.0
	suggestDetailsMinWidth = 220.0
	suggestVerticalPadding = kit.SuggestVerticalPadding
)

// suggestRowHeight is the height of a row: editor.suggestLineHeight, else the line height of the editor (at least 8).
func (vm *ViewModel) suggestRowHeight() float64 {
	h := float64(vm.opts.Suggest.LineHeight)
	if h <= 0 {
		h = vm.opts.LineHeight
	}
	return math.Min(math.Max(h, 8), 1000)
}

// suggestFontSize is editor.suggestFontSize, else the font size of the editor.
func (vm *ViewModel) suggestFontSize() float64 {
	if s := vm.opts.Suggest.FontSize; s > 0 {
		return float64(s)
	}
	return vm.opts.FontSize
}

// suggestOpts are the options of the list of the widget: the fonts and the heights of the editor.
func (vm *ViewModel) suggestOpts() kit.SuggestListOpts {
	return kit.SuggestListOpts{
		RowHeight: vm.suggestRowHeight(), FontSize: vm.suggestFontSize(), FontFamilies: vm.opts.FontFamilies, FontWeight: vm.opts.FontWeight,
	}
}

// suggestStyle is the text style of the widget: the font of the editor at the size of the widget.
func (vm *ViewModel) suggestStyle(color ui.Color, size float64) *painting.TextStyle {
	return kit.SuggestTextStyle(vm.suggestOpts(), color, size)
}

// suggestLayer is the widget over the text, or nil while there is none: the list below the word being completed (above it
// when there is no room) and the details at its side.
func (s *editorState) suggestLayer(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	v := vm.SuggestView()
	if v == nil || s.textRO == nil || !s.textRO.Attached() || vm.viewport <= 0 {
		return nil
	}
	rect := s.textRO.RectOf(v.Anchor)
	// the cursor row is where the widget hangs from: it is the row of the anchor
	if rect.Bottom < 0 || rect.Top > vm.viewport {
		return nil
	}
	rowH := vm.suggestRowHeight()
	origin := s.gutterWidth
	if vm.mm.info.Show && vm.Options().Minimap.Side == "left" {
		origin += vm.mm.info.Width
	}
	viewW, _ := vm.ViewportSize()
	left := origin + rect.Left
	width := suggestWidth
	rows := 1
	if v.State == SuggestOpen || v.State == SuggestFrozen {
		rows = min(len(v.Items), suggestRows)
	} else {
		width = suggestWidth / 2
	}
	// the room below and above the line, as VS Code works it out: a cap of 12 rows, fewer when the room is short
	below := vm.viewport - rect.Bottom - suggestVerticalPadding
	above := rect.Top
	wanted := float64(rows)*rowH + 2
	placeAbove := wanted > below && above > below
	room := below
	if placeAbove {
		room = above
	}
	if wanted > room {
		rows = max(int(math.Floor((room-2)/rowH)), 1)
		wanted = float64(rows)*rowH + 2
	}
	vm.SetSuggestRows(rows)
	if viewW > 0 {
		left = math.Max(math.Min(left, origin+viewW-width), 0)
	}
	list := s.suggestList(bc, vm, colors, v, rows, width)
	pos := widgets.PositionedOpts{Left: foundation.Some(left)}
	top := math.Max(rect.Bottom, 0)
	if placeAbove {
		pos.Bottom = foundation.Some(vm.viewport - rect.Top)
		top = math.Max(rect.Top-wanted, 0)
	} else {
		pos.Top = foundation.Some(top)
	}
	children := []widgets.Widget{widgets.NewPositioned(list, pos)}
	if v.Details != nil {
		children = append(children, s.suggestDetailsPane(bc, vm, colors, v, left, top, wanted, width, placeAbove, origin, viewW))
	}
	return widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// suggestList is the box with the rows, or with the message of a state that has no rows.
func (s *editorState) suggestList(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, v *SuggestView, rows int, width float64) widgets.Widget {
	o := vm.suggestOpts()
	o.Width = width
	if v.State == SuggestLoading || v.State == SuggestEmpty {
		text := "Loading..."
		if v.State == SuggestEmpty {
			text = "No suggestions."
		}
		return kit.NewSuggestMessage(colors, text, o)
	}
	top := min(max(v.Top, 0), max(len(v.Items)-rows, 0))
	end := min(top+rows, len(v.Items))
	o.Rows = make([]kit.SuggestRow, 0, end-top)
	for i := top; i < end; i++ {
		it := v.Items[i]
		o.Rows = append(o.Rows, kit.SuggestRow{
			Label: it.Label, Positions: it.Score.Positions, LabelDetail: it.LabelDetail, LabelDescription: it.LabelDescription,
			Detail: it.Detail, Kind: int(it.Kind), Deprecated: it.Deprecated, CanExpand: canExpand(it),
		})
	}
	o.Focus = v.Focus - top
	o.Total, o.Top = len(v.Items), top
	o.ShowIcons, o.ShowInlineDetails, o.DetailsOpen, o.Frozen = v.Options.ShowIcons, v.Options.ShowInlineDetails, v.Details != nil, v.State == SuggestFrozen
	o.OnAccept = func(i int) { vm.SuggestAccept(top + i) }
	o.OnToggleDetails = vm.SuggestToggleDetails
	o.OnScroll = vm.SuggestScroll
	return kit.NewSuggestList(colors, o)
}

// suggestDetailsPane is the pane with the detail and the documentation of the focused item. It stands at the right of the list,
// at the left when there is no room at the right, aligned with the top of the list (with its bottom when the list is above the
// line).
func (s *editorState) suggestDetailsPane(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, v *SuggestView, left, top, listHeight, listWidth float64, above bool, origin, viewW float64) widgets.Widget {
	d := v.Details
	size := vm.suggestFontSize()
	rowH := vm.suggestRowHeight()
	var paneW float64
	right := left + listWidth
	avail := viewW + origin - right
	x := right - 1
	if avail < suggestDetailsMinWidth {
		if lw := left; lw >= suggestDetailsMinWidth {
			paneW = math.Min(suggestDetailsWidth, lw-1)
			x = left - paneW + 1
		} else {
			paneW = math.Max(suggestDetailsMinWidth, avail)
		}
	} else {
		paneW = math.Min(suggestDetailsWidth, avail)
	}
	fg := colors.EditorSuggestWidgetForeground
	var children []widgets.Widget
	textW := paneW - 2
	height := 2.0
	if d.Detail != "" {
		lines := estimateLines(d.Detail, textW-10, size*0.6)
		children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(5, 4, 24, 4),
			widgets.NewText(d.Detail, widgets.TextOpts{Style: vm.suggestStyle(fg.WithOpacity(fg.Opacity()*0.7), size)})))
		height += float64(lines)*rowH + 8
	}
	switch {
	case d.Loading && d.Documentation == nil:
		children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(5, 0, 5, 4), widgets.NewText("Loading...", widgets.TextOpts{Style: vm.suggestStyle(fg, size)})))
		height += rowH + 4
	case d.Documentation != nil && d.Documentation.Value != "":
		docs := d.Documentation
		lines := estimateLines(docs.Value, textW-16, 13*0.6)
		dh := float64(lines)*13*1.4 + 16
		var body widgets.Widget
		if docs.Markdown {
			body = markdown.NewView(docs.Value, markdown.Options{Compact: true, OnLink: func(dest string) {
				if vm.openURI != nil {
					vm.openURI(dest)
				}
			}})
		} else {
			body = widgets.NewPadding(painting.NewEdgeInsetsAll(8), widgets.NewText(docs.Value, widgets.TextOpts{Style: vm.suggestStyle(fg, 13)}))
		}
		children = append(children, widgets.NewExpanded(body))
		height += dh
	}
	maxH := math.Max(listHeight, 2*rowH)
	if _, vh := vm.ViewportSize(); vh > 0 {
		maxH = math.Max(vh-top-suggestVerticalPadding, 2*rowH)
		if above {
			maxH = math.Max(top+listHeight, 2*rowH)
		}
	}
	height = math.Min(math.Max(height, 2*rowH), math.Max(maxH, listHeight))
	column := widgets.NewColumn(children, widgetsColumnStretch())
	pane := kit.SuggestBox(colors, column, paneW, height)
	pos := widgets.PositionedOpts{Left: foundation.Some(math.Max(x, 0))}
	if above {
		pos.Top = foundation.Some(math.Max(top+listHeight-height, 0))
	} else {
		pos.Top = foundation.Some(top)
	}
	return widgets.NewPositioned(pane, pos)
}

func widgetsColumnStretch() widgets.ColumnOpts {
	return widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMax}
}

// estimateLines is the number of lines a text takes in a width, counting the characters of an average width.
func estimateLines(text string, width, charWidth float64) int {
	perLine := math.Max(math.Floor(width/charWidth), 1)
	lines := 0
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		lines += max(int(math.Ceil(float64(utf8.RuneCountInString(l))/perLine)), 1)
	}
	return lines
}
