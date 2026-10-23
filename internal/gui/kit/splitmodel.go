package kit

import (
	"math"
	"slices"
)

// LayoutPriority says in which order the views of a split view take up or give back space when
// the split view is resized: High first, then Normal, then Low.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (LayoutPriority)
type LayoutPriority int

const (
	// LayoutNormal is the priority of a view that says nothing.
	LayoutNormal LayoutPriority = iota
	// LayoutLow views absorb a change of size last.
	LayoutLow
	// LayoutHigh views absorb a change of size first: the editor area, which takes the whole
	// change when the window is resized.
	LayoutHigh
)

// SashState says what a sash can do, and so which cursor it shows.
//
// Ported from VS Code: src/vs/base/browser/ui/sash/sash.ts (SashState)
type SashState int

const (
	// SashEnabled lets the sash be dragged both ways.
	SashEnabled SashState = iota
	// SashDisabled ignores the pointer.
	SashDisabled
	// SashAtMinimum can only be dragged down or to the right: the view before it is as small as
	// it can be.
	SashAtMinimum
	// SashAtMaximum can only be dragged up or to the left: the view before it is as large as
	// it can be.
	SashAtMaximum
)

// SplitItem describes one view of a SplitModel: its size and the limits and priority the layout
// keeps to.
type SplitItem struct {
	// Size is the size the view starts with along the axis of the split view. For a Hidden view
	// it is the size it comes back with when it is shown.
	Size float64
	// Hidden starts the view out of sight, with no size.
	Hidden bool
	// MinSize is the least size of the view; 0 is none.
	MinSize float64
	// MaxSize is the greatest size of the view; 0 is none.
	MaxSize float64
	// Priority is the order in which the view takes up or gives back space when the split view
	// is resized; it is only used when the split view is not proportional.
	Priority LayoutPriority
	// Snap hides the view when a sash is dragged past half of its minimum size, and shows it
	// again when it is dragged back.
	Snap bool
	// PreferredSize is the size a double click on a sash next to the view gives it; 0 is none.
	PreferredSize float64
}

// splitItem is a view of the model with its size now.
type splitItem struct {
	SplitItem
	size float64
	// cached is the size a hidden view comes back with.
	cached float64
	hidden bool
}

func (i *splitItem) visible() bool { return !i.hidden }

// viewMax is the greatest size of the view, hidden or not.
func (i *splitItem) viewMax() float64 {
	if i.MaxSize <= 0 {
		return math.Inf(1)
	}
	return i.MaxSize
}

// minimum is the least size of the view now: a hidden view has none.
func (i *splitItem) minimum() float64 {
	if i.hidden {
		return 0
	}
	return i.MinSize
}

// maximum is the greatest size of the view now: a hidden view has none.
func (i *splitItem) maximum() float64 {
	if i.hidden {
		return 0
	}
	return i.viewMax()
}

// setVisible shows or hides the view. A view that is hidden remembers its size (the one given,
// or else the one it had).
func (i *splitItem) setVisible(visible bool, size float64) {
	if visible == i.visible() {
		return
	}
	if visible {
		i.size = clampSize(i.cached, i.MinSize, i.viewMax())
		i.hidden = false
		return
	}
	i.cached = size
	if math.IsNaN(size) {
		i.cached = i.size
	}
	i.size = 0
	i.hidden = true
}

// clampSize is JavaScript's Math.min(Math.max(v, lo), hi).
func clampSize(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

type sizingKind int

const (
	sizingNone sizingKind = iota
	sizingFixed
	sizingDistribute
	sizingSplit
	sizingAuto
	sizingInvisible
)

// Sizing says how the sizes of the other views change when a view is added to a SplitModel or
// removed from it.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (Sizing)
type Sizing struct {
	kind  sizingKind
	index int
	size  float64
}

// SizeOf adds a view with a size of its own.
func SizeOf(size float64) Sizing { return Sizing{kind: sizingFixed, size: size} }

// SizingDistribute shares the whole size equally among the views.
func SizingDistribute() Sizing { return Sizing{kind: sizingDistribute} }

// SizingSplit makes room for an added view by halving the view at index, or gives the space of a
// removed view to it.
func SizingSplit(index int) Sizing { return Sizing{kind: sizingSplit, index: index} }

// SizingAuto is SizingDistribute when the views have equal sizes, and else SizingSplit of index.
func SizingAuto(index int) Sizing { return Sizing{kind: sizingAuto, index: index} }

// SizingInvisible adds a view that is hidden and comes back with the size.
func SizingInvisible(cachedVisibleSize float64) Sizing {
	return Sizing{kind: sizingInvisible, size: cachedVisibleSize}
}

// snapState is the view that a drag of a sash hides or shows, with the drag distance at which it
// does.
type snapState struct {
	index      int
	limitDelta float64
	size       float64
}

// sashDrag is a sash being dragged.
type sashDrag struct {
	index              int
	start, current     float64
	sizes              []float64
	minDelta, maxDelta float64
	alt                bool
	snapBefore         *snapState
	snapAfter          *snapState
}

// SplitModelOpts configure a SplitModel.
type SplitModelOpts struct {
	// ProportionalLayout keeps the views in proportion to each other when the split view is
	// resized, instead of giving the change to the views by priority.
	ProportionalLayout bool
	// InverseAltBehavior makes dragging a sash with Alt held the ordinary way and without Alt the
	// way that resizes the view on both sides.
	InverseAltBehavior bool
}

// SplitModel is the layout of a split view without any widget: views in a line with sizes, limits
// and priorities, the sashes between them, and what changes when a sash is dragged, the whole
// size changes or a view is shown or hidden. All sizes are along the axis of the split view.
// A SplitModel is used from one goroutine.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (SplitView)
type SplitModel struct {
	items       []*splitItem
	size        float64
	contentSize float64
	opts        SplitModelOpts
	// proportions are the share of each view in the content, set once the model has been laid
	// out in proportional mode; a negative share is none (the view is hidden).
	proportions []float64
	sashStates  []SashState
	drag        *sashDrag

	startSnapping, endSnapping bool
}

// NewSplitModel makes a model of views in a split view of a size. The views take the sizes they
// are given; the first Layout makes them fit the size.
func NewSplitModel(items []SplitItem, size float64, opts ...SplitModelOpts) *SplitModel {
	m := &SplitModel{size: size, startSnapping: true, endSnapping: true}
	if len(opts) > 0 {
		m.opts = opts[0]
	}
	for _, it := range items {
		sizing := SizeOf(it.Size)
		if it.Hidden {
			sizing = SizingInvisible(it.Size)
		}
		m.items = append(m.items, newSplitItem(it, sizing))
	}
	m.contentSize = m.sum()
	m.saveProportions()
	m.updateSashStates()
	return m
}

func newSplitItem(it SplitItem, size Sizing) *splitItem {
	item := &splitItem{SplitItem: it}
	if size.kind == sizingInvisible {
		item.hidden, item.cached = true, size.size
		return item
	}
	item.size = size.size
	return item
}

// Len is the number of views.
func (m *SplitModel) Len() int { return len(m.items) }

// Size is the size the split view has.
func (m *SplitModel) Size() float64 { return m.size }

// ContentSize is the sum of the sizes of the views. It is more than Size when the views cannot
// all be made small enough, and less when they cannot be made large enough.
func (m *SplitModel) ContentSize() float64 { return m.contentSize }

// Sizes are the sizes of the views, 0 for a hidden one.
func (m *SplitModel) Sizes() []float64 {
	out := make([]float64, len(m.items))
	for i, it := range m.items {
		out[i] = it.size
	}
	return out
}

// ViewSize is the size of a view, or -1 for an index out of range.
func (m *SplitModel) ViewSize(index int) float64 {
	if index < 0 || index >= len(m.items) {
		return -1
	}
	return m.items[index].size
}

// Offset is where a view starts, from the start of the split view.
func (m *SplitModel) Offset(index int) float64 {
	var offset float64
	for i := 0; i < index && i < len(m.items); i++ {
		offset += m.items[i].size
	}
	return offset
}

// IsViewVisible tells whether a view is shown; a view out of range is not.
func (m *SplitModel) IsViewVisible(index int) bool {
	return index >= 0 && index < len(m.items) && m.items[index].visible()
}

// CachedVisibleSize is the size a hidden view comes back with; ok is false for a view that is
// shown.
func (m *SplitModel) CachedVisibleSize(index int) (size float64, ok bool) {
	if index < 0 || index >= len(m.items) || m.items[index].visible() {
		return 0, false
	}
	return m.items[index].cached, true
}

// MinimumSize is the least size the split view can have: the sum of the minimums of its views.
func (m *SplitModel) MinimumSize() float64 {
	var total float64
	for _, it := range m.items {
		total += it.minimum()
	}
	return total
}

// MaximumSize is the greatest size the split view can have (infinite when a view has no limit).
func (m *SplitModel) MaximumSize() float64 {
	if len(m.items) == 0 {
		return math.Inf(1)
	}
	var total float64
	for _, it := range m.items {
		total += it.maximum()
	}
	return total
}

// SashStates are the states of the sashes, one between each two views.
func (m *SplitModel) SashStates() []SashState { return slices.Clone(m.sashStates) }

// SashPosition is the position of a sash: the end of the view before it.
func (m *SplitModel) SashPosition(index int) float64 { return m.Offset(index + 1) }

// SetSnapping turns snapping at the start and at the end of the split view on or off (it is on
// at the start): a sash next to a hidden view that snaps is disabled when there is nothing
// beyond it to snap to.
func (m *SplitModel) SetSnapping(start, end bool) {
	if m.startSnapping == start && m.endSnapping == end {
		return
	}
	m.startSnapping, m.endSnapping = start, end
	m.updateSashStates()
}

// Layout gives the split view a size. The views take up or give back the change by priority, or
// keep their proportions in a proportional model.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (layout)
func (m *SplitModel) Layout(size float64) {
	previous := math.Max(m.size, m.contentSize)
	m.size = size
	if m.proportions == nil {
		low, high := m.priorityIndexes(nil)
		m.resize(len(m.items)-1, size-previous, nil, low, high, math.Inf(-1), math.Inf(1), nil, nil)
	} else {
		var total float64
		for i, it := range m.items {
			if p := m.proportions[i]; p >= 0 {
				total += p
			} else {
				size -= it.size
			}
		}
		for i, it := range m.items {
			if p := m.proportions[i]; p >= 0 && total > 0 {
				it.size = clampSize(round(p*size/total), it.minimum(), it.maximum())
			}
		}
	}
	m.distributeEmptySpace(-1)
	m.layoutViews()
}

// priorityIndexes are the indexes of the views of Low and of High priority, among the views
// except skip (which may be nil).
func (m *SplitModel) priorityIndexes(skip *int) (low, high []int) {
	for i, it := range m.items {
		if skip != nil && i == *skip {
			continue
		}
		switch it.Priority {
		case LayoutLow:
			low = append(low, i)
		case LayoutHigh:
			high = append(high, i)
		}
	}
	return low, high
}

func (m *SplitModel) sum() float64 {
	var total float64
	for _, it := range m.items {
		total += it.size
	}
	return total
}

func (m *SplitModel) saveProportions() {
	if !m.opts.ProportionalLayout || m.contentSize <= 0 {
		return
	}
	m.proportions = make([]float64, len(m.items))
	for i, it := range m.items {
		m.proportions[i] = -1
		if it.visible() {
			m.proportions[i] = it.size / m.contentSize
		}
	}
}

// relayout fits the views to the size after something changed.
func (m *SplitModel) relayout(low, high []int) {
	m.resize(len(m.items)-1, m.size-m.sum(), nil, low, high, math.Inf(-1), math.Inf(1), nil, nil)
	m.distributeEmptySpace(-1)
	m.layoutViews()
	m.saveProportions()
}

// rangeInts is the indexes from from towards to, to excluded, in either direction.
func rangeInts(from, to int) []int {
	var out []int
	step := 1
	if from > to {
		step = -1
	}
	for i := from; i != to; i += step {
		out = append(out, i)
	}
	return out
}

// pushToStart moves a value to the start of a list, if it is in it.
func pushToStart(list []int, value int) []int {
	i := slices.Index(list, value)
	if i < 0 {
		return list
	}
	list = slices.Delete(list, i, i+1)
	return append([]int{value}, list...)
}

// pushToEnd moves a value to the end of a list, if it is in it.
func pushToEnd(list []int, value int) []int {
	i := slices.Index(list, value)
	if i < 0 {
		return list
	}
	list = slices.Delete(list, i, i+1)
	return append(list, value)
}

// resize moves the sash after the view at index by delta: the views before it, from the nearest,
// take the change, and the views after it take the opposite. The change is cut down to what the
// limits of the views allow, and (for a snap) hides or shows a view; it returns the change made.
// The sizes the views start from are sizes, or their sizes now.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (resize)
func (m *SplitModel) resize(index int, delta float64, sizes []float64, lowPriority, highPriority []int, overloadMin, overloadMax float64, snapBefore, snapAfter *snapState) float64 {
	if index < 0 || index >= len(m.items) {
		return 0
	}
	if sizes == nil {
		sizes = m.Sizes()
	}
	up := rangeInts(index, -1)
	down := rangeInts(index+1, len(m.items))
	for _, i := range highPriority {
		up = pushToStart(up, i)
		down = pushToStart(down, i)
	}
	for _, i := range lowPriority {
		up = pushToEnd(up, i)
		down = pushToEnd(down, i)
	}
	var minDeltaUp, maxDeltaUp, maxDeltaDown, minDeltaDown float64
	for _, i := range up {
		minDeltaUp += m.items[i].minimum() - sizes[i]
		maxDeltaUp += m.items[i].maximum() - sizes[i]
	}
	if len(down) == 0 {
		maxDeltaDown, minDeltaDown = math.Inf(1), math.Inf(-1)
	}
	for _, i := range down {
		maxDeltaDown += sizes[i] - m.items[i].minimum()
		minDeltaDown += sizes[i] - m.items[i].maximum()
	}
	minDelta := math.Max(math.Max(minDeltaUp, minDeltaDown), overloadMin)
	maxDelta := math.Min(math.Min(maxDeltaDown, maxDeltaUp), overloadMax)

	snapped := false
	if snapBefore != nil {
		view := m.items[snapBefore.index]
		visible := delta >= snapBefore.limitDelta
		snapped = visible != view.visible()
		view.setVisible(visible, snapBefore.size)
	}
	if !snapped && snapAfter != nil {
		view := m.items[snapAfter.index]
		visible := delta < snapAfter.limitDelta
		snapped = visible != view.visible()
		view.setVisible(visible, snapAfter.size)
	}
	if snapped {
		return m.resize(index, delta, sizes, lowPriority, highPriority, overloadMin, overloadMax, nil, nil)
	}

	delta = clampSize(delta, minDelta, maxDelta)
	deltaUp := delta
	for _, i := range up {
		it := m.items[i]
		size := clampSize(sizes[i]+deltaUp, it.minimum(), it.maximum())
		deltaUp -= size - sizes[i]
		it.size = size
	}
	deltaDown := delta
	for _, i := range down {
		it := m.items[i]
		size := clampSize(sizes[i]-deltaDown, it.minimum(), it.maximum())
		deltaDown += size - sizes[i]
		it.size = size
	}
	return delta
}

// distributeEmptySpace gives what is left of the size to the views, by priority; the view at
// lowPriorityIndex (when it is not negative) goes last.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (distributeEmptySpace)
func (m *SplitModel) distributeEmptySpace(lowPriorityIndex int) {
	empty := m.size - m.sum()
	indexes := rangeInts(len(m.items)-1, -1)
	low, high := m.priorityIndexes(nil)
	for _, i := range high {
		indexes = pushToStart(indexes, i)
	}
	for _, i := range low {
		indexes = pushToEnd(indexes, i)
	}
	if lowPriorityIndex >= 0 {
		indexes = pushToEnd(indexes, lowPriorityIndex)
	}
	for i := 0; empty != 0 && i < len(indexes); i++ {
		it := m.items[indexes[i]]
		size := clampSize(it.size+empty, it.minimum(), it.maximum())
		empty -= size - it.size
		it.size = size
	}
}

// layoutViews takes the new content size and the states of the sashes.
func (m *SplitModel) layoutViews() {
	m.contentSize = m.sum()
	m.updateSashStates()
}

// updateSashStates works out what each sash can do: whether the views before it can shrink and
// the views after it grow, and the other way.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (updateSashEnablement)
func (m *SplitModel) updateSashStates() {
	n := len(m.items)
	if n < 2 {
		m.sashStates = nil
		return
	}
	run := func(f func(*splitItem) bool, reverse bool) []bool {
		out := make([]bool, n)
		previous := false
		for k := 0; k < n; k++ {
			i := k
			if reverse {
				i = n - 1 - k
			}
			previous = f(m.items[i]) || previous
			out[i] = previous
		}
		return out
	}
	collapses := func(it *splitItem) bool { return it.size-it.minimum() > 0 }
	expands := func(it *splitItem) bool { return it.maximum()-it.size > 0 }
	collapsesDown, expandsDown := run(collapses, false), run(expands, false)
	collapsesUp, expandsUp := run(collapses, true), run(expands, true)

	m.sashStates = make([]SashState, n-1)
	var position float64
	for index := range m.sashStates {
		position += m.items[index].size
		atMin := !collapsesDown[index] || !expandsUp[index+1]
		atMax := !expandsDown[index] || !collapsesUp[index+1]
		switch {
		case atMin && atMax:
			snapBefore := m.findFirstSnapIndex(rangeInts(index, -1))
			snapAfter := m.findFirstSnapIndex(rangeInts(index+1, n))
			snappedBefore := snapBefore >= 0 && !m.items[snapBefore].visible()
			snappedAfter := snapAfter >= 0 && !m.items[snapAfter].visible()
			switch {
			case snappedBefore && collapsesUp[index] && (position > 0 || m.startSnapping):
				m.sashStates[index] = SashAtMinimum
			case snappedAfter && collapsesDown[index] && (position < m.contentSize || m.endSnapping):
				m.sashStates[index] = SashAtMaximum
			default:
				m.sashStates[index] = SashDisabled
			}
		case atMin && !atMax:
			m.sashStates[index] = SashAtMinimum
		case !atMin && atMax:
			m.sashStates[index] = SashAtMaximum
		default:
			m.sashStates[index] = SashEnabled
		}
	}
}

// findFirstSnapIndex is the view among the indexes that a drag of a sash hides or shows, or -1:
// a visible view that snaps, else a hidden one that snaps with no flexible visible view before it.
func (m *SplitModel) findFirstSnapIndex(indexes []int) int {
	for _, i := range indexes {
		if it := m.items[i]; it.visible() && it.Snap {
			return i
		}
	}
	for _, i := range indexes {
		it := m.items[i]
		if it.visible() && it.maximum()-it.minimum() > 0 {
			return -1
		}
		if !it.visible() && it.Snap {
			return i
		}
	}
	return -1
}

// SashStart begins dragging the sash after the view at index, with the pointer at start along
// the axis of the split view. With alt the sash resizes the view on both sides alike.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (onSashStart)
func (m *SplitModel) SashStart(index int, start float64, alt bool) {
	if index < 0 || index >= len(m.items)-1 {
		return
	}
	m.startDrag(index, start, alt)
}

// SashSetAlt says that Alt was pressed or released during a drag; the drag goes on from where the
// pointer is.
func (m *SplitModel) SashSetAlt(alt bool) {
	if m.drag != nil {
		m.startDrag(m.drag.index, m.drag.current, alt)
	}
}

func (m *SplitModel) startDrag(index int, start float64, alt bool) {
	sizes := m.Sizes()
	minDelta, maxDelta := math.Inf(-1), math.Inf(1)
	if m.opts.InverseAltBehavior {
		alt = !alt
	}
	if alt {
		// With the last sash the view before it is resized (to the left or up, not right or
		// down as usual), and the other sashes the view after it.
		if index == len(m.items)-2 {
			it := m.items[index]
			minDelta = (it.minimum() - it.size) / 2
			maxDelta = (it.maximum() - it.size) / 2
		} else {
			it := m.items[index+1]
			minDelta = (it.size - it.maximum()) / 2
			maxDelta = (it.size - it.minimum()) / 2
		}
	}
	var snapBefore, snapAfter *snapState
	if !alt {
		up := rangeInts(index, -1)
		down := rangeInts(index+1, len(m.items))
		var minDeltaUp, maxDeltaUp, maxDeltaDown, minDeltaDown float64
		for _, i := range up {
			minDeltaUp += m.items[i].minimum() - sizes[i]
			maxDeltaUp += m.items[i].viewMax() - sizes[i]
		}
		for _, i := range down {
			maxDeltaDown += sizes[i] - m.items[i].minimum()
			minDeltaDown += sizes[i] - m.items[i].viewMax()
		}
		if len(down) == 0 {
			maxDeltaDown, minDeltaDown = math.Inf(1), math.Inf(-1)
		}
		minDelta = math.Max(minDeltaUp, minDeltaDown)
		maxDelta = math.Min(maxDeltaDown, maxDeltaUp)
		if i := m.findFirstSnapIndex(up); i >= 0 {
			it := m.items[i]
			half := math.Floor(it.MinSize / 2)
			limit := minDelta + half
			if it.visible() {
				limit = minDelta - half
			}
			snapBefore = &snapState{index: i, limitDelta: limit, size: it.size}
		}
		if i := m.findFirstSnapIndex(down); i >= 0 {
			it := m.items[i]
			half := math.Floor(it.MinSize / 2)
			limit := maxDelta - half
			if it.visible() {
				limit = maxDelta + half
			}
			snapAfter = &snapState{index: i, limitDelta: limit, size: it.size}
		}
	}
	m.drag = &sashDrag{index: index, start: start, current: start, sizes: sizes, minDelta: minDelta, maxDelta: maxDelta, alt: alt, snapBefore: snapBefore, snapAfter: snapAfter}
}

// SashChange moves the sash being dragged to where the pointer is now, along the axis of the
// split view.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (onSashChange)
func (m *SplitModel) SashChange(current float64) {
	d := m.drag
	if d == nil {
		return
	}
	d.current = current
	newDelta := m.resize(d.index, current-d.start, d.sizes, nil, nil, d.minDelta, d.maxDelta, d.snapBefore, d.snapAfter)
	if d.alt {
		last := d.index == len(m.items)-2
		viewIndex := d.index + 1
		if last {
			viewIndex = d.index
		}
		it := m.items[viewIndex]
		resizeIndex := d.index + 1
		if last {
			resizeIndex = d.index - 1
		}
		m.resize(resizeIndex, -newDelta, m.Sizes(), nil, nil, it.size-it.maximum(), it.size-it.minimum(), nil, nil)
	}
	m.distributeEmptySpace(-1)
	m.layoutViews()
}

// SashEnd ends the drag of a sash; it returns the index of the sash, or -1 when none was dragged.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (onSashEnd)
func (m *SplitModel) SashEnd() int {
	if m.drag == nil {
		return -1
	}
	index := m.drag.index
	m.drag = nil
	m.saveProportions()
	return index
}

// Dragging tells whether a sash is being dragged.
func (m *SplitModel) Dragging() bool { return m.drag != nil }

// CanResetSash tells whether a double click on the sash may reset the views: it may not when a
// view next to it that snaps is hidden, as a double click on the edge of a collapsed side bar
// must not open it.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (the onDidReset handler of a sash)
func (m *SplitModel) CanResetSash(index int) bool {
	if index < 0 || index >= len(m.items)-1 {
		return false
	}
	if i := m.findFirstSnapIndex(rangeInts(index, -1)); i >= 0 && !m.items[i].visible() {
		return false
	}
	if i := m.findFirstSnapIndex(rangeInts(index+1, len(m.items))); i >= 0 && !m.items[i].visible() {
		return false
	}
	return true
}

// ResetSash gives the views on both sides of a sash their preferred sizes (the view before the
// sash first); when neither has one, the sizes of the views are distributed.
func (m *SplitModel) ResetSash(index int) {
	if !m.CanResetSash(index) {
		return
	}
	before, after := m.items[index], m.items[index+1]
	if before.PreferredSize <= 0 && after.PreferredSize <= 0 {
		m.DistributeViewSizes()
		return
	}
	if before.PreferredSize > 0 {
		m.ResizeView(index, before.PreferredSize)
	}
	if after.PreferredSize > 0 {
		m.ResizeView(index+1, after.PreferredSize)
	}
}

// ResizeView sets the size of a view (within its limits and the size of the split view); the other
// views give or take the difference, by priority, the view's own neighbours last.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (resizeView)
func (m *SplitModel) ResizeView(index int, size float64) {
	if index < 0 || index >= len(m.items) {
		return
	}
	low, high := m.priorityIndexes(&index)
	low = append(low, index)
	it := m.items[index]
	size = clampSize(round(size), it.minimum(), math.Min(it.maximum(), m.size))
	it.size = size
	m.relayout(low, high)
}

// IsViewExpanded tells whether all the other views are at their minimum size.
func (m *SplitModel) IsViewExpanded(index int) bool {
	if index < 0 || index >= len(m.items) {
		return false
	}
	for i, it := range m.items {
		if i != index && it.size > it.minimum() {
			return false
		}
	}
	return true
}

// DistributeViewSizes shares the whole size equally among the views that can be resized.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (distributeViewSizes)
func (m *SplitModel) DistributeViewSizes() {
	var flexible []*splitItem
	var flexibleSize float64
	for _, it := range m.items {
		if it.maximum()-it.minimum() > 0 {
			flexible = append(flexible, it)
			flexibleSize += it.size
		}
	}
	if len(flexible) > 0 {
		size := math.Floor(flexibleSize / float64(len(flexible)))
		for _, it := range flexible {
			it.size = clampSize(size, it.minimum(), it.maximum())
		}
	}
	low, high := m.priorityIndexes(nil)
	m.relayout(low, high)
}

// SetViewVisible shows or hides a view; the others take up or give back the space.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (setViewVisible)
func (m *SplitModel) SetViewVisible(index int, visible bool) {
	if index < 0 || index >= len(m.items) {
		return
	}
	m.items[index].setVisible(visible, math.NaN())
	m.distributeEmptySpace(index)
	m.layoutViews()
	m.saveProportions()
}

// UpdateView tells the model that the limits, the priority or the snapping of a view changed. The
// view's size is kept within the new limits (or becomes size, unless it is NaN), and the other
// views take up or give back the difference, by priority.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (onViewChange)
func (m *SplitModel) UpdateView(index int, item SplitItem, size float64) {
	if index < 0 || index >= len(m.items) {
		return
	}
	it := m.items[index]
	it.MinSize, it.MaxSize, it.Priority, it.Snap, it.PreferredSize = item.MinSize, item.MaxSize, item.Priority, item.Snap, item.PreferredSize
	if math.IsNaN(size) {
		size = it.size
	}
	size = clampSize(size, it.minimum(), it.maximum())
	if m.opts.InverseAltBehavior && index > 0 {
		// The view grows or shrinks on both sides alike: resize its neighbour by half.
		m.resize(index-1, math.Floor((it.size-size)/2), nil, nil, nil, math.Inf(-1), math.Inf(1), nil, nil)
		m.distributeEmptySpace(-1)
		m.layoutViews()
		return
	}
	it.size = size
	low, high := m.priorityIndexes(&index)
	low = append(low, index)
	m.relayout(low, high)
}

// Constraints are the limits, priority, snapping and preferred size of a view.
func (m *SplitModel) Constraints(index int) SplitItem {
	if index < 0 || index >= len(m.items) {
		return SplitItem{}
	}
	return m.items[index].SplitItem
}

// areViewsDistributed tells whether the sizes of the views differ by at most 2.
func (m *SplitModel) areViewsDistributed() bool {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, it := range m.items {
		lo, hi = math.Min(lo, it.size), math.Max(hi, it.size)
		if hi-lo > 2 {
			return false
		}
	}
	return true
}

// AddView adds a view at index (the end when index is out of range). The size says how the other
// views make room: a size of its own, an equal share, half of another view, or no size at all
// (hidden).
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (addView)
func (m *SplitModel) AddView(item SplitItem, size Sizing, index int) {
	if index < 0 || index > len(m.items) {
		index = len(m.items)
	}
	if size.kind == sizingAuto {
		size = SizingSplit(size.index)
		if m.areViewsDistributed() {
			size = SizingDistribute()
		}
	}
	var viewSize Sizing
	switch size.kind {
	case sizingFixed:
		viewSize = size
	case sizingSplit:
		viewSize = SizeOf(m.ViewSize(size.index) / 2)
	case sizingInvisible:
		viewSize = size
	default:
		viewSize = SizeOf(item.MinSize)
	}
	m.items = slices.Insert(m.items, index, newSplitItem(item, viewSize))
	m.proportions = nil
	var high []int
	if size.kind == sizingSplit {
		high = []int{size.index}
	}
	m.relayout([]int{index}, high)
	if size.kind == sizingDistribute || size.kind == sizingNone {
		m.DistributeViewSizes()
	}
}

// RemoveView removes the view at index. The size says what happens to its space: none gives it to
// the views by priority, SizingSplit to one view, SizingDistribute to all of them.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts (removeView)
func (m *SplitModel) RemoveView(index int, size Sizing) {
	if index < 0 || index >= len(m.items) {
		return
	}
	if size.kind == sizingAuto {
		size = SizingSplit(size.index)
		if m.areViewsDistributed() {
			size = SizingDistribute()
		}
	}
	var reference *splitItem
	if size.kind == sizingSplit && size.index >= 0 && size.index < len(m.items) {
		reference = m.items[size.index]
	}
	removed := m.items[index]
	m.items = slices.Delete(m.items, index, index+1)
	if reference != nil {
		reference.size += removed.size
	}
	m.proportions = nil
	m.relayout(nil, nil)
	if size.kind == sizingDistribute {
		m.DistributeViewSizes()
	}
}
