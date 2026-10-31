package kit

import (
	"sort"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"
)

// Glutter: SliverVariedExtentList finds a row by adding up the heights of the rows before it, with a call of the height function for
// each: IndexToLayoutOffset for every child it lays out, and the search for the first row in view, are linear in the number of rows.
// A list of 100,000 rows of different heights (the Source Control view has rows for an input and a button among its files) then
// takes 40 to 60 ms for a frame at its end. This sliver is the same sliver with the hooks that RenderSliverFixedExtentBoxAdaptor
// has for a carousel (FixedExtentLayoutOverrides), answered from the tops of the rows by a binary search. Glutter should keep the
// offsets of a varied list in a prefix sum, or take them from the owner of the heights.

// sliverRows is a sliver list of rows whose tops the owner knows (tops[i] is the top of row i and tops[len-1] the end of the last
// one), with the layout answered in constant time and by binary search.
type sliverRows struct {
	widgets.SliverMultiBoxAdaptorWidget
	tops []float64
}

func newSliverRows(delegate widgets.SliverChildDelegate, tops []float64) *sliverRows {
	return &sliverRows{SliverMultiBoxAdaptorWidget: widgets.NewSliverMultiBoxAdaptorWidgetBase(delegate), tops: tops}
}

// CreateElement implements widgets.Widget.
func (w *sliverRows) CreateElement() widgets.Element {
	return widgets.NewSliverMultiBoxAdaptorElement(w, false)
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *sliverRows) CreateRenderObject(bc widgets.BuildContext) rendering.RenderObject {
	r := &renderSliverRows{tops: w.tops}
	rendering.InitRenderSliverFixedExtentBoxAdaptor(&r.RenderSliverFixedExtentBoxAdaptor, r, bc.(rendering.RenderSliverBoxChildManager))
	return r
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *sliverRows) UpdateRenderObject(_ widgets.BuildContext, ro rendering.RenderObject) {
	r := ro.(*renderSliverRows)
	r.tops = w.tops
	r.MarkNeedsLayout()
}

// exactExtentDelegate is a delegate that knows the end of the last row, so that the sliver does not estimate the extent of the list
// from the rows it built (which makes a jump to the end stop short of it).
type exactExtentDelegate struct {
	*widgets.SliverChildBuilderDelegate
	total float64
}

// EstimateMaxScrollOffset is the end of the last row.
func (d exactExtentDelegate) EstimateMaxScrollOffset(int, int, float64, float64) foundation.Opt[float64] {
	return foundation.Some(d.total)
}

// renderSliverRows is the render object of sliverRows.
type renderSliverRows struct {
	rendering.RenderSliverFixedExtentBoxAdaptor
	tops []float64
}

func (r *renderSliverRows) rows() int { return max(len(r.tops)-1, 0) }

// ItemExtent is none: the extent of a row comes from ItemExtentBuilder.
func (r *renderSliverRows) ItemExtent() foundation.Opt[float64] { return foundation.None[float64]() }

// ItemExtentBuilder gives the height of a row.
func (r *renderSliverRows) ItemExtentBuilder() rendering.ItemExtentBuilder {
	return func(i int, _ rendering.SliverLayoutDimensions) foundation.Opt[float64] {
		if i < 0 || i >= r.rows() {
			return foundation.None[float64]()
		}
		return foundation.Some(r.tops[i+1] - r.tops[i])
	}
}

// OverrideIndexToLayoutOffset is the top of a row.
func (r *renderSliverRows) OverrideIndexToLayoutOffset(index int) float64 {
	if len(r.tops) == 0 {
		return 0
	}
	return r.tops[min(max(index, 0), len(r.tops)-1)]
}

// OverrideMinChildIndexForScrollOffset is the row that holds the scroll offset: the first one that ends at or after it.
func (r *renderSliverRows) OverrideMinChildIndexForScrollOffset(offset float64) int {
	return r.rowAt(offset)
}

// OverrideMaxChildIndexForScrollOffset is the same row (Flutter's varied list finds both ends the same way).
func (r *renderSliverRows) OverrideMaxChildIndexForScrollOffset(offset float64) int {
	return r.rowAt(offset)
}

func (r *renderSliverRows) rowAt(offset float64) int {
	n := r.rows()
	if offset == 0 || n == 0 {
		return 0
	}
	i := sort.Search(n, func(i int) bool { return r.tops[i+1] >= offset })
	return min(i, n-1)
}

// OverrideComputeMaxScrollOffset is the end of the last row.
func (r *renderSliverRows) OverrideComputeMaxScrollOffset(rendering.SliverConstraints) float64 {
	if len(r.tops) == 0 {
		return 0
	}
	return r.tops[len(r.tops)-1]
}
