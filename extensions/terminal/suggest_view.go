package terminal

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// The widget of the suggest list of a terminal (SPEC §10.25 "Terminal IntelliSense"): the editor's list (extension/ui's
// SuggestList), at the word being typed, below the cursor's row or above it when there is no room.

// suggestIcons are the Codicon and the color name of the icon of each kind of item (VS Code's terminalSymbolIcons.ts).
var suggestIcons = map[itemKind]struct{ icon, color string }{
	kindFile:          {"symbol-file", "file"},
	kindFolder:        {"folder", "folder"},
	kindMethod:        {"symbol-method", "method"},
	kindAlias:         {"symbol-method", "method"},
	kindArgument:      {"symbol-variable", "variable"},
	kindOption:        {"symbol-enum", "enumerator"},
	kindOptionValue:   {"symbol-enum-member", "enumeratorMember"},
	kindFlag:          {"flag", "enumerator"},
	kindSymlinkFile:   {"file-symlink-file", "file"},
	kindSymlinkFolder: {"file-symlink-directory", "folder"},
	kindHistory:       {"history", "keyword"},
}

// suggestDetailsWidth is the width of the details pane.
const suggestDetailsWidth = 300.0

// suggestLayers are the widgets of the open list (none when it is closed or has no place on the screen).
func (pv *paneView) suggestLayers(bc widgets.BuildContext) []widgets.Widget {
	s := pv.sug.sess
	if s == nil || len(s.items) == 0 || pv.factory == nil || pv.render == nil || pv.cols == 0 {
		return nil
	}
	a, _ := pv.atlases()
	cw, ch := a.m.w, a.m.h
	size := pv.render.Size()
	cfg := pv.env.cfg()
	rowH := math.Max(ch, 20)
	fontSize := math.Max(cfg.fontSize-1, 9)

	// where the cursor is on the screen: the screen starts at the line after all the lines of the scrollback
	em := pv.term.Emulator()
	screenTop := em.FirstLine() + int64(em.LineCount()-pv.rows)
	row := int(s.in.cursorLine - screenTop)
	if row < 0 || row >= pv.rows || pv.scroll > 0 {
		return nil
	}
	wordCols := utf8.RuneCountInString(s.lc.line[min(s.lc.cur.start, s.lc.cursor):s.lc.cursor])
	col := max(s.in.cursorX-wordCols, 0)
	cursorTop := float64(row) * ch
	cursorBottom := cursorTop + ch

	rows := min(len(s.items), suggestRowsMax)
	need := float64(rows)*rowH + 2
	below := size.Height - cursorBottom - 4
	above := cursorTop - 4
	placeAbove := need > below && above > below
	room := below
	if placeAbove {
		room = above
	}
	if need > room {
		rows = max(int(math.Floor((room-2)/rowH)), 1)
		need = float64(rows)*rowH + 2
	}
	s.rows = rows
	s.top = min(max(s.top, 0), max(len(s.items)-rows, 0))
	if s.focus >= 0 {
		if s.focus < s.top {
			s.top = s.focus
		}
		if s.focus >= s.top+rows {
			s.top = s.focus - rows + 1
		}
	}

	families := append([]string(nil), cfg.fontFamilies...)
	if len(families) == 0 && pv.desktop != "" {
		families = append(families, pv.desktop)
	}
	families = append(families, "monospace")
	end := min(s.top+rows, len(s.items))
	opts := ui.SuggestListOpts{
		Rows: make([]ui.SuggestRow, 0, end-s.top), Focus: s.focus - s.top, Total: len(s.items), Top: s.top,
		Width: ui.SuggestWidth, RowHeight: rowH, FontSize: fontSize, FontFamilies: families,
		ShowIcons: true, ShowInlineDetails: true, DetailsOpen: s.details, DetailShare: 0.5,
	}
	for _, it := range s.items[s.top:end] {
		ic := suggestIcons[it.kind]
		opts.Rows = append(opts.Rows, ui.SuggestRow{
			Label: it.label, Positions: it.positions, LabelDetail: it.labelDetail, LabelDescription: it.detail,
			Icon: ic.icon, IconColor: ic.color, CanExpand: it.detail != "" || it.docs != "",
		})
	}
	top := s.top
	opts.OnAccept = func(i int) { pv.suggestAccept(top+i, false) }
	opts.OnToggleDetails = pv.suggestToggleDetails
	opts.OnScroll = pv.suggestScroll

	x := padLeft - pv.hscroll + float64(col)*cw
	x = math.Max(math.Min(x, size.Width-scrollbarW-ui.SuggestWidth), 0)
	y := cursorBottom
	if placeAbove {
		y = cursorTop - need
	}
	y = math.Max(y, 0)
	s.x, s.y = x, y
	layers := []widgets.Widget{widgets.NewPositioned(ui.NewSuggestList(bc, opts), widgets.PositionedOpts{Left: foundation.Some(x), Top: foundation.Some(y)})}
	if s.details && s.focus >= 0 && s.focus < len(s.items) {
		if pane := pv.suggestDetails(bc, s.items[s.focus], opts, x, y, size.Width); pane != nil {
			layers = append(layers, pane)
		}
	}
	return layers
}

// suggestDetails is the pane at the side of the list with the description and the longer text of the selected item.
func (pv *paneView) suggestDetails(bc widgets.BuildContext, it sugItem, o ui.SuggestListOpts, x, y, width float64) widgets.Widget {
	var parts []string
	if it.detail != "" {
		parts = append(parts, it.detail)
	}
	if it.docs != "" {
		parts = append(parts, it.docs)
	}
	if len(parts) == 0 {
		return nil
	}
	text := strings.Join(parts, "\n\n")
	colors := ui.Theme(bc)
	style := ui.TextStyle(bc, o.FontSize, colors.EditorSuggestWidgetForeground)
	paneW := suggestDetailsWidth
	left := x + ui.SuggestWidth - 1
	if left+paneW > width-scrollbarW {
		left = math.Max(x-paneW+1, 0)
	}
	lines := 0
	perLine := math.Max(math.Floor((paneW-16)/(o.FontSize*0.6)), 1)
	for _, l := range strings.Split(text, "\n") {
		lines += max(int(math.Ceil(float64(utf8.RuneCountInString(l))/perLine)), 1)
	}
	height := math.Min(float64(lines)*o.RowHeight+12, 12*o.RowHeight)
	body := widgets.NewPadding(painting.NewEdgeInsetsAll(6), widgets.NewText(text, widgets.TextOpts{Style: style}))
	return widgets.NewPositioned(ui.NewSuggestBox(bc, body, paneW, height), widgets.PositionedOpts{Left: foundation.Some(left), Top: foundation.Some(y)})
}
