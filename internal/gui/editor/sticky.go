package editor

import (
	"math"
	"sort"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/folding"
)

// Ported from VS Code: src/vs/editor/contrib/stickyScroll/browser/stickyScrollProvider.ts,
// stickyScrollModelProvider.ts and stickyScrollController.ts

// StickyScrollOptions are the settings of sticky scroll (editor.stickyScroll.*).
type StickyScrollOptions struct {
	// Enabled is editor.stickyScroll.enabled.
	Enabled bool
	// MaxLineCount is editor.stickyScroll.maxLineCount.
	MaxLineCount int
	// ScrollWithEditor is editor.stickyScroll.scrollWithEditor: the lines follow the horizontal
	// scroll of the text.
	ScrollWithEditor bool
}

// stickyCandidate is a line that can stick to the top of the editor: the first line of a scope, the
// last line of the scope, and where the line would sit in the widget (the lines of the scopes
// around it are above it).
type stickyCandidate struct {
	Start, End  int
	Top, Height float64
}

// stickyTree is the scopes of a document as a tree, made from the folding regions: the regions
// are sorted and nested, and a scope is a region of two lines or more.
type stickyTree struct {
	regions *folding.Regions
	kids    [][]int32
	roots   []int32
}

func newStickyTree(r *folding.Regions) *stickyTree {
	t := &stickyTree{regions: r, kids: make([][]int32, r.Len())}
	for i := range r.Len() {
		if p := r.Parent(i); p >= 0 {
			t.kids[p] = append(t.kids[p], int32(i))
		} else {
			t.roots = append(t.roots, int32(i))
		}
	}
	return t
}

// candidates are the lines that can stick when the lines between two lines (0-based, inclusive)
// are on the screen: the scopes that touch them, outermost first, with the depth that says where
// each sits.
//
// Ported from VS Code: stickyScrollProvider.ts (getCandidateStickyLinesIntersectingFromStickyModel)
func (t *stickyTree) candidates(visFirst, visLast int, lineHeight float64) []stickyCandidate {
	var out []stickyCandidate
	t.collect(t.roots, visFirst, visLast, lineHeight, 0, -1<<30, &out)
	return out
}

func (t *stickyTree) collect(kids []int32, visFirst, visLast int, lineHeight, top float64, lastStart int, out *[]stickyCandidate) {
	if len(kids) == 0 {
		return
	}
	// the last child that starts at or before a line, or the first when none does
	bound := func(line int) int {
		i := sort.Search(len(kids), func(i int) bool { return t.regions.Start(int(kids[i])) >= line })
		if i < len(kids) && t.regions.Start(int(kids[i])) == line {
			return i
		}
		return max(i-1, 0)
	}
	lower, upper := bound(visFirst), bound(visLast)
	last := lastStart
	for i := lower; i <= upper; i++ {
		k := int(kids[i])
		start, end := t.regions.Start(k), t.regions.End(k)
		if end > start && visFirst <= end+2 && start <= visLast+1 && start != last {
			last = start
			*out = append(*out, stickyCandidate{Start: start, End: end, Top: top, Height: lineHeight})
			t.collect(t.kids[k], visFirst, visLast, lineHeight, top+lineHeight, start, out)
		}
	}
}

// stickyLines are the lines that stick to the top of the editor at a scroll position, from the
// outermost scope in.
type stickyLines struct {
	// Lines are the first lines of the scopes, and Ends the lines after their last (the line that
	// closes the scope, which a shift-click reveals).
	Lines, Ends []int
	// Offset is how far the last line has been pushed up by the end of its scope (0 or less).
	Offset float64
}

// Height is the height of the widget: a row for each line, less what the last one is pushed up.
func (s stickyLines) Height(rowHeight float64) float64 {
	if len(s.Lines) == 0 {
		return 0
	}
	return float64(len(s.Lines))*rowHeight + s.Offset
}

// stickyState is what the view model knows of sticky scroll.
type stickyState struct {
	tree     *stickyTree
	treeFor  *folding.Regions
	treeVer  int
	treeSize int
	// hover says that the pointer is over the widget, line which of its lines (from 0, -1 for none).
	hover   bool
	line    int
	changed *foundation.ChangeNotifier
}

// stickyTreeNow is the tree of scopes of the document, made again when the regions or the
// document changed.
func (vm *ViewModel) stickyTreeNow() *stickyTree {
	m := vm.fold.model
	if m == nil {
		return nil
	}
	r := m.Regions()
	s := &vm.sticky
	if r == nil {
		return nil
	}
	if s.tree == nil || s.treeFor != r || s.treeVer != vm.doc.Version() || s.treeSize != r.Len() {
		s.tree, s.treeFor, s.treeVer, s.treeSize = newStickyTree(r), r, vm.doc.Version(), r.Len()
	}
	return s.tree
}

// maxStickyLines is how many lines can stick: the setting, and no more than a quarter of the
// editor.
//
// Ported from VS Code: stickyScrollController.ts (_onDidResize, findScrollWidgetState)
func (vm *ViewModel) maxStickyLines() int {
	o := vm.opts
	if !o.StickyScroll.Enabled || o.LineHeight <= 0 || vm.doc.LargeFile() {
		return 0
	}
	byHeight := int(math.Round(vm.viewport / o.LineHeight * 0.25))
	return min(byHeight, o.StickyScroll.MaxLineCount)
}

// stickyLinesAt finds the lines that stick when the editor is scrolled to top: a scope sticks when
// its first line has scrolled above the place it would take in the widget and the place is not
// below the end of the scope; the last line is pushed up as the end of its scope goes by.
//
// Ported from VS Code: stickyScrollController.ts (findScrollWidgetState)
func (vm *ViewModel) stickyLinesAt(top float64) stickyLines {
	var out stickyLines
	limit := vm.maxStickyLines()
	tree := vm.stickyTreeNow()
	if limit <= 0 || tree == nil || vm.viewport <= 0 {
		return out
	}
	proj := vm.Projection()
	rows := proj.RowCount()
	firstLine, _, _ := proj.RowSpan(proj.RowAt(top))
	lastLine, _, _ := proj.RowSpan(proj.RowAt(top + vm.viewport))
	hidden := vm.hiddenLines()
	lines := vm.doc.Snapshot().LineCount()
	for _, c := range tree.candidates(firstLine, lastLine, vm.opts.LineHeight) {
		if stickyHidden(hidden, c) {
			continue
		}
		topOfElement := c.Top
		bottomOfElement := c.Top + c.Height
		startRow, _ := proj.LineRows(c.Start)
		firstEnd, countEnd := proj.LineRows(min(c.End, lines-1))
		endRow := min(firstEnd+max0(countEnd-1), rows-1)
		topOfStart := proj.RowTop(min(startRow, rows-1)) - top
		bottomOfEnd := proj.RowTop(endRow) + proj.RowHeight(endRow) - top
		if topOfElement > topOfStart && topOfElement <= bottomOfEnd {
			out.Lines = append(out.Lines, c.Start)
			out.Ends = append(out.Ends, min(c.End+1, lines-1))
			if bottomOfElement > bottomOfEnd {
				out.Offset = bottomOfEnd - bottomOfElement
			}
		}
		if len(out.Lines) == limit {
			break
		}
	}
	return out
}

func max0(n int) int { return max(n, 0) }

// stickyHidden tells whether a candidate lies in lines that a fold hides.
//
// Ported from VS Code: stickyScrollProvider.ts (filterHiddenRanges)
func stickyHidden(hidden []folding.LineSpan, c stickyCandidate) bool {
	for _, h := range hidden {
		if c.Start >= h.First && c.End <= h.Last+1 {
			return true
		}
	}
	return false
}

// StickyHover tells the view model that the pointer is over the sticky lines (at a line from 0,
// or -1 when it left them).
func (vm *ViewModel) StickyHover(line int) {
	s := &vm.sticky
	if hover := line >= 0; s.hover != hover || s.line != line {
		s.hover, s.line = hover, line
		s.changed.NotifyListeners()
	}
}

// stickyColumnAt is the position of the document at a line that sticks, for a point at a distance
// from the left edge of its text.
func (vm *ViewModel) stickyPositionAt(line int, geo geometry, x float64) buffer.Position {
	text, err := vm.doc.Snapshot().LineContent(line)
	if err != nil {
		return buffer.Position{Line: line}
	}
	return buffer.Position{Line: line, Col: geo.columnAt(text, x)}
}

// stickyLineAtHeight is the sticky line at a height of the widget (from 0), or -1.
func (vm *ViewModel) stickyLineAtHeight(y float64) int {
	lines := vm.stickyLinesAt(vm.ScrollTop())
	rowHeight := vm.opts.LineHeight
	if len(lines.Lines) == 0 || y < 0 || y >= lines.Height(rowHeight) || rowHeight <= 0 {
		return -1
	}
	return min(int(y/rowHeight), len(lines.Lines)-1)
}

// RevealStickyPosition moves the cursor to a position that a click on a sticky line chose and
// scrolls so that its row is in view and not under the sticky lines.
//
// Ported from VS Code: stickyScrollController.ts (_revealPosition)
func (vm *ViewModel) RevealStickyPosition(p buffer.Position) {
	vm.cursors.SetSelections([]buffer.Selection{buffer.NewCursor(vm.doc.Snapshot().ValidatePosition(p))})
	vm.SelectionsChanged()
	vm.RevealPosition(p)
}

// RevealLineInCenterIfOutsideViewport scrolls so that a line is in the middle of the editor, unless it
// is on the screen already.
func (vm *ViewModel) RevealLineInCenterIfOutsideViewport(line int) {
	if vm.scroller == nil || vm.viewport <= 0 {
		return
	}
	proj := vm.Projection()
	row := vm.RowOfPosition(buffer.Position{Line: line})
	top, bottom := proj.RowTop(row), proj.RowTop(row)+proj.RowHeight(row)
	if top >= vm.scrollTop && bottom <= vm.scrollTop+vm.viewport {
		return
	}
	vm.scroller(max(top+proj.RowHeight(row)/2-vm.viewport/2, 0))
}
