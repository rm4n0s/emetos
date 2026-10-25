package editor

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Background marks (SPEC §9.1): colored backgrounds that code outside the editor puts on its text, on the lines
// of a range across the editor and on the gutter, or on a range of characters. The diff editor marks its changed
// lines and characters with them (step 9.5). The colors are given with the marks (the caller resolves the theme
// colors), and the marks are decorations of the document, so they follow its edits until the caller sets
// new ones.
//
// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/components/diffEditorDecorations.ts and
// src/vs/editor/browser/viewParts/decorations/decorations.ts (the classes `line-insert`, `char-insert` and
// `gutter-insert` of the diff editor are the three fields of a mark)

// marksOwner is the slot of the decorations of the background marks (see ViewModel.owner).
const marksOwner = 7

// BackgroundMark colors part of the text of an editor.
type BackgroundMark struct {
	// Range is where the mark is. With WholeLine it is the lines from the one the range starts on to the one it
	// ends on, both included (an empty range is the one line it is on).
	Range buffer.Range
	// WholeLine fills the rows of the lines across the editor with Background; without it Background fills the
	// characters of the range.
	WholeLine bool
	// Background is the fill, Border the outline of a range of characters (drawn inside it); a transparent
	// color draws nothing.
	Background, Border ui.Color
	// Gutter, when it is not transparent, fills the gutter along the rows of the lines of a WholeLine mark.
	Gutter ui.Color
}

// markData is what a mark's decoration carries.
type markData struct {
	whole                      bool
	background, border, gutter ui.Color
}

// SetBackgroundMarks replaces the background marks of the editor. The marks are decorations of the document
// that this editor owns, so another editor of the same document does not show them.
func (vm *ViewModel) SetBackgroundMarks(marks []BackgroundMark) {
	var old []int
	for _, d := range vm.doc.DecorationsOf(vm.owner(marksOwner)) {
		old = append(old, d.ID)
	}
	add := make([]document.NewDecoration, len(marks))
	for i, m := range marks {
		add[i] = document.NewDecoration{Range: m.Range, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "backgroundMark",
			Data: markData{whole: m.WholeLine, background: m.Background, border: m.Border, gutter: m.Gutter},
		}}
	}
	vm.doc.DeltaDecorations(vm.owner(marksOwner), old, add)
	vm.markCount = len(marks)
	vm.NotifyListeners()
}

// BackgroundMarks are the marks of the editor as they are now (their ranges moved with the edits of the
// document), in order of position.
func (vm *ViewModel) BackgroundMarks() []BackgroundMark {
	var out []BackgroundMark
	for _, d := range vm.doc.DecorationsOf(vm.owner(marksOwner)) {
		if m, ok := markOf(d); ok {
			out = append(out, m)
		}
	}
	return out
}

// DisposeMarks takes the background marks off the document.
func (vm *ViewModel) DisposeMarks() {
	vm.doc.RemoveDecorationsOf(vm.owner(marksOwner))
	vm.markCount = 0
}

func markOf(d document.Decoration) (BackgroundMark, bool) {
	data, ok := d.Options.Data.(markData)
	if !ok {
		return BackgroundMark{}, false
	}
	return BackgroundMark{Range: d.Range, WholeLine: data.whole, Background: data.background, Border: data.border, Gutter: data.gutter}, true
}

// marksIn are the marks that touch the lines of the rows first to last.
func (vm *ViewModel) marksIn(firstRow, lastRow int) []BackgroundMark {
	if vm.markCount == 0 {
		return nil
	}
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(firstRow)
	lastLine, _, _ := proj.RowSpan(lastRow)
	span := buffer.Range{Start: buffer.Position{Line: firstLine}, End: buffer.Position{Line: lastLine + 1}}
	var out []BackgroundMark
	for _, d := range vm.doc.DecorationsIn(span, vm.owner(marksOwner)) {
		if m, ok := markOf(d); ok {
			out = append(out, m)
		}
	}
	return out
}

// wholeLineRows are the rows from the first row of the line a range starts on to the last row of the line it
// ends on (a view zone among them is left to the caller to skip).
func wholeLineRows(vm *ViewModel, r buffer.Range) (first, last int) {
	proj := vm.Projection()
	first, _ = proj.LineRows(r.Start.Line)
	lastFirst, count := proj.LineRows(r.End.Line)
	return first, lastFirst + max(count, 1) - 1
}

// paintBackgroundMarks draws the marks of the visible rows: the lines first, then the characters.
func (v overlayView) paintBackgroundMarks() {
	vm := v.geo.vm
	marks := vm.marksIn(v.first, v.last)
	if len(marks) == 0 {
		return
	}
	for _, m := range marks {
		if !m.WholeLine || m.Background.Alpha() == 0 {
			continue
		}
		first, last := wholeLineRows(vm, m.Range)
		for row := max(first, v.first); row <= min(last, v.last); row++ {
			if v.geo.isZoneRow(row) {
				continue
			}
			v.canvas.DrawRect(v.rowRect(row, vm.ScrollLeft(), vm.ScrollLeft()+v.width), fill(m.Background))
		}
	}
	for _, m := range marks {
		if m.WholeLine || m.Range.Start == m.Range.End {
			continue
		}
		if _, _, hidden := vm.HiddenSpan(m.Range.Start.Line); hidden {
			continue
		}
		v.paintMatch(m.Range, m.Background, m.Border)
	}
}

// paintMarkGutter fills the gutter along the rows of the lines of the marks that have a gutter color.
func (r *RenderEditorGutter) paintMarkGutter(canvas *ui.Canvas, offset ui.Offset, size ui.Size, top float64, first, last int) {
	for _, m := range r.vm.marksIn(first, last) {
		if !m.WholeLine || m.Gutter.Alpha() == 0 {
			continue
		}
		from, to := wholeLineRows(r.vm, m.Range)
		proj := r.vm.Projection()
		for row := max(from, first); row <= min(to, last); row++ {
			if _, _, zone := proj.RowZone(row); zone {
				continue
			}
			y := offset.Dy + proj.RowTop(row) - top
			canvas.DrawRect(ui.NewRectFromLTWH(offset.Dx, y, size.Width, proj.RowHeight(row)), fill(m.Gutter))
		}
	}
}
