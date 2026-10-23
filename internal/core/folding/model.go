package folding

import (
	"hash/fnv"
	"sort"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/editor/contrib/folding/browser/foldingModel.ts (MIT)

// DecorationProvider gives the options of the decoration of a region: whether it is folded,
// whether it is hidden by a folded region that contains it, and whether the user made it.
type DecorationProvider func(collapsed, hidden, manual bool) document.DecorationOptions

// ChangeEvent tells that the model changed.
type ChangeEvent struct {
	// Collapsed lists the regions that were folded or unfolded; it is nil when the regions
	// themselves were replaced.
	Collapsed []Region
}

// Model is the folding of one document: its regions, kept as decorations that move with the text,
// and which of them are folded. Its methods are for the goroutine of the document.
type Model struct {
	doc      *document.Document
	owner    int
	decorate DecorationProvider

	regions *Regions
	ids     []int // the decoration of each region

	mu        sync.Mutex
	listeners map[int]func(ChangeEvent)
	nextID    int
}

// NewModel makes the folding model of a document, with no regions. The decorations of the regions
// belong to owner (1 or more) and get their options from decorate.
func NewModel(doc *document.Document, owner int, decorate DecorationProvider) *Model {
	empty, _ := NewRegions(nil, nil, nil)
	return &Model{doc: doc, owner: owner, decorate: decorate, regions: empty, listeners: map[int]func(ChangeEvent){}}
}

// Regions are the regions now.
func (m *Model) Regions() *Regions { return m.regions }

// Document is the document the model is of.
func (m *Model) Document() *document.Document { return m.doc }

// OnChange registers a function that is called when the regions or their folded state change.
func (m *Model) OnChange(fn func(ChangeEvent)) (unsubscribe func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	m.listeners[id] = fn
	return func() {
		m.mu.Lock()
		delete(m.listeners, id)
		m.mu.Unlock()
	}
}

func (m *Model) fire(ev ChangeEvent) {
	m.mu.Lock()
	ids := make([]int, 0, len(m.listeners))
	for id := range m.listeners {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	fns := make([]func(ChangeEvent), len(ids))
	for i, id := range ids {
		fns[i] = m.listeners[id]
	}
	m.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// ToggleCollapseState folds the regions that are open and unfolds the ones that are folded, and
// updates their decorations and those of the regions after them that a folded region hides.
func (m *Model) ToggleCollapseState(toggled []Region) {
	if len(toggled) == 0 {
		return
	}
	toggled = append([]Region(nil), toggled...)
	sort.SliceStable(toggled, func(i, j int) bool { return toggled[i].index < toggled[j].index })
	processed := map[int]bool{}
	k := 0           // the next region whose decoration is up to date
	dirtyEnd := -1   // the end of the range where decorations need to change
	lastHidden := -1 // the end of the last hidden lines
	updateUntil := func(index int) {
		for ; k < index; k++ {
			end := m.regions.End(k)
			collapsed := m.regions.Collapsed(k)
			if end <= dirtyEnd {
				manual := m.regions.Source(k) != FromProvider
				m.doc.SetDecorationOptions(m.ids[k], m.decorate(collapsed, end <= lastHidden, manual))
			}
			if collapsed && end > lastHidden {
				lastHidden = end
			}
		}
	}
	for _, region := range toggled {
		index := region.index
		if index >= len(m.ids) || processed[m.ids[index]] {
			continue
		}
		processed[m.ids[index]] = true
		updateUntil(index) // with the old dirtyEnd
		m.regions.SetCollapsed(index, !m.regions.Collapsed(index))
		dirtyEnd = max(dirtyEnd, m.regions.End(index))
	}
	updateUntil(m.regions.Len())
	m.fire(ChangeEvent{Collapsed: toggled})
}

// RemoveManualRanges removes the regions the user made that a range touches: for an empty range
// (a cursor) the innermost region of the user that has its line, or all of them when there is none;
// for a range that is not empty, the ones it overlaps.
func (m *Model) RemoveManualRanges(ranges []buffer.Range) error {
	remove := map[int]bool{}
	removeAll := false
	for _, r := range ranges {
		if !r.IsEmpty() {
			continue
		}
		index := m.regions.FindRange(r.Start.Line)
		for index != -1 && m.regions.Source(index) == FromProvider {
			index = m.regions.Parent(index)
		}
		if index == -1 {
			removeAll = true
		} else {
			remove[index] = true
		}
	}
	intersects := func(rg Range) bool {
		for _, r := range ranges {
			if !r.IsEmpty() && r.Start.Line <= rg.End && rg.Start <= r.End.Line {
				return true
			}
		}
		return false
	}
	var kept []Range
	for i := 0; i < m.regions.Len(); i++ {
		rg := m.regions.ToRange(i)
		if rg.Source == FromProvider || (!removeAll && !remove[i] && !intersects(rg)) {
			kept = append(kept, rg)
		}
	}
	regions, err := FromRanges(kept)
	if err != nil {
		return err
	}
	m.UpdatePost(regions)
	return nil
}

// Update replaces the regions by the ones a provider found now, keeping what is folded and the
// regions the user made: the range of each is where its decoration is now. A folded region that
// has a cursor inside it (sel) is unfolded.
func (m *Model) Update(found *Regions, sel SelectedLines) error {
	current := m.foldedOrManualRanges(sel)
	merged := SanitizeAndMerge(found.Ranges(), current, m.lastLine(), sel)
	regions, err := FromRanges(merged)
	if err != nil {
		return err
	}
	m.UpdatePost(regions)
	return nil
}

func (m *Model) lastLine() int { return m.doc.Snapshot().LineCount() - 1 }

// UpdatePost makes regions the regions of the model: their decorations replace the old ones.
func (m *Model) UpdatePost(regions *Regions) {
	snap := m.doc.Snapshot()
	add := make([]document.NewDecoration, regions.Len())
	lastHidden := -1
	for i := range add {
		start, end := regions.Start(i), regions.End(i)
		collapsed := regions.Collapsed(i)
		manual := regions.Source(i) != FromProvider
		add[i] = document.NewDecoration{
			Range: buffer.Range{
				Start: lineEndOf(snap, start),
				End:   lineEndOf(snap, end),
			},
			Options: m.decorate(collapsed, end <= lastHidden, manual),
		}
		if collapsed && end > lastHidden {
			lastHidden = end
		}
	}
	m.ids = m.doc.DeltaDecorations(m.owner, m.ids, add)
	m.regions = regions
	m.fire(ChangeEvent{})
}

// lineEndOf is the position at the end of a line, or of the last line when it is past it.
func lineEndOf(snap buffer.Snapshot, line int) buffer.Position {
	line = min(max(line, 0), snap.LineCount()-1)
	text, _ := snap.LineContent(line)
	return buffer.Position{Line: line, Col: len(text)}
}

// foldedOrManualRanges are the regions that are folded or were made by the user, with the lines
// their decorations are on now.
func (m *Model) foldedOrManualRanges(sel SelectedLines) []Range {
	var out []Range
	for i := 0; i < m.regions.Len(); i++ {
		collapsed := m.regions.Collapsed(i)
		source := m.regions.Source(i)
		if !collapsed && source == FromProvider {
			continue
		}
		dec, ok := m.doc.Decoration(m.ids[i])
		if !ok {
			continue
		}
		if collapsed && sel != nil && sel.StartsInside(dec.Range.Start.Line+1, dec.Range.End.Line) {
			collapsed = false // a cursor inside the region unfolds it
		}
		out = append(out, Range{
			Start: dec.Range.Start.Line, End: dec.Range.End.Line,
			Type: m.regions.Type(i), Collapsed: collapsed, Source: source,
		})
	}
	return out
}

// MementoEntry is a region in the state that is saved with the editor's view state: the lines, what
// is folded, where it came from, and a checksum of its second and last lines, so that a region
// that no longer looks the same is not restored.
type MementoEntry struct {
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Collapsed bool   `json:"collapsed"`
	Source    Source `json:"source,omitempty"`
	Checksum  uint32 `json:"checksum,omitempty"`
}

// Memento is the state of the folding for persistence.
type Memento []MementoEntry

func (m *Model) linesChecksum(first, second int) uint32 {
	snap := m.doc.Snapshot()
	a, _ := snap.LineContent(first)
	b, _ := snap.LineContent(second)
	h := fnv.New32a()
	_, _ = h.Write([]byte(a + b))
	return h.Sum32() % 1000000
}

// Memento is the folded and manual regions, or nil when there are none.
func (m *Model) Memento() Memento {
	var out Memento
	last := m.lastLine()
	for _, rg := range m.foldedOrManualRanges(nil) {
		if rg.Start >= rg.End || rg.Start < 0 || rg.End > last {
			continue
		}
		out = append(out, MementoEntry{
			Start: rg.Start, End: rg.End, Collapsed: rg.Collapsed, Source: rg.Source,
			Checksum: m.linesChecksum(rg.Start+1, rg.End),
		})
	}
	return out
}

// ApplyMemento folds the regions of a saved state that are still there: those whose checksum
// matches the text now.
func (m *Model) ApplyMemento(state Memento) error {
	var restore []Range
	last := m.lastLine()
	for _, e := range state {
		if e.Start >= e.End || e.Start < 0 || e.End > last {
			continue
		}
		if e.Checksum == 0 || e.Checksum == m.linesChecksum(e.Start+1, e.End) {
			restore = append(restore, Range{Start: e.Start, End: e.End, Collapsed: e.Collapsed, Source: e.Source})
		}
	}
	regions, err := FromRanges(SanitizeAndMerge(m.regions.Ranges(), restore, last, nil))
	if err != nil {
		return err
	}
	m.UpdatePost(regions)
	return nil
}

// Dispose removes the decorations of the regions.
func (m *Model) Dispose() {
	m.doc.RemoveDecorations(m.ids...)
	m.ids = nil
	m.mu.Lock()
	m.listeners = map[int]func(ChangeEvent){}
	m.mu.Unlock()
}

// AllRegionsAtLine are the regions that have a line, the innermost first; filter, when it is not
// nil, tells which to keep, given the region and its level (1 for the innermost).
func (m *Model) AllRegionsAtLine(line int, filter func(r Region, level int) bool) []Region {
	var out []Region
	index := m.regions.FindRange(line)
	for level := 1; index >= 0; level++ {
		current := m.regions.Region(index)
		if filter == nil || filter(current, level) {
			out = append(out, current)
		}
		index = current.Parent()
	}
	return out
}

// RegionAtLine is the innermost region that has a line.
func (m *Model) RegionAtLine(line int) (Region, bool) {
	if index := m.regions.FindRange(line); index >= 0 {
		return m.regions.Region(index), true
	}
	return Region{}, false
}

// RegionsInside are the regions inside a region (all of them when ok is false), in order; filter,
// when it is not nil, tells which to keep, given the region and its level inside the region (1 for
// the regions directly in it).
func (m *Model) RegionsInside(region Region, ok bool, filter func(r Region, level int) bool) []Region {
	index, end := 0, int(^uint(0)>>1)
	if ok {
		index, end = region.index+1, region.End()
	}
	var out []Region
	var stack []Region
	for i := index; i < m.regions.Len(); i++ {
		current := m.regions.Region(i)
		if current.Start() >= end {
			break
		}
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if current.ContainedBy(top.Start(), top.End()) {
				break
			}
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, current)
		if filter == nil || filter(current, len(stack)) {
			out = append(out, current)
		}
	}
	return out
}

// LineSpan is a span of lines, zero-based and inclusive at both ends.
type LineSpan struct {
	First, Last int
}

// HiddenLines are the lines the folded regions hide: the lines after the first line of each folded
// region up to its last, as sorted spans that do not touch. They are read from the decorations of
// the regions, so they follow the edits made since the regions were computed. A folded region whose
// decoration has shrunk to one line hides nothing.
func (m *Model) HiddenLines() []LineSpan {
	var spans []LineSpan
	for i := 0; i < m.regions.Len() && i < len(m.ids); i++ {
		if !m.regions.Collapsed(i) {
			continue
		}
		d, ok := m.doc.Decoration(m.ids[i])
		if !ok || d.Range.End.Line <= d.Range.Start.Line {
			continue
		}
		spans = append(spans, LineSpan{First: d.Range.Start.Line + 1, Last: d.Range.End.Line})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].First < spans[j].First })
	merged := spans[:0]
	for _, s := range spans {
		if n := len(merged); n > 0 && s.First <= merged[n-1].Last+1 {
			merged[n-1].Last = max(merged[n-1].Last, s.Last)
			continue
		}
		merged = append(merged, s)
	}
	return merged
}
