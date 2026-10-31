package multidiff

import (
	"math"
	"sort"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Metrics of the layout, in logical pixels.
const (
	// HeaderHeight is the height of the header of a section (VS Code's `.header` is 38 px with a 1 px border; the rows of Emetos's lists
	// are 22 px, and a header carries icon buttons of 22 px).
	HeaderHeight = 30.0
	// EstimatedBodyHeight is the height of a body that has not been measured: the section is laid out with it until its diff is computed.
	EstimatedBodyHeight = 160.0
	// DefaultOverscan is how far beyond the viewport, above and below, the sections are built.
	DefaultOverscan = 300.0
)

// Section describes one file of the editor to the layout and to the header. The window makes them from the items of the input.
type Section struct {
	// Key identifies the section: a section keeps its collapsed mark and its measured height while the same key comes back.
	Key string
	// Name and Folder are the file name and its folder, as the header shows them.
	Name, Folder string
	// File is the file URI for the icon of the header; empty for none.
	File string
	// Letter, Color and Tooltip are the mark of the status at the end of the header; Color is the id of a theme color.
	Letter, Color, Tooltip string
}

// sectionState is a section with what the layout knows of it.
type sectionState struct {
	Section
	collapsed bool
	// body is the height of the body; measured says that the owner of the body reported it (otherwise it is the estimate).
	body     float64
	measured bool
}

// Model is the layout of the editor: the sections in order, which are collapsed, the height of every body, the scroll position and the
// size of the viewport. It answers which sections are near the viewport so that only those are built, and keeps the content that is in
// view where it is when the height of a section above it changes. It is used on the UI thread only; it is a ChangeNotifier that tells
// about every change of the sections, the heights, the scroll position and the viewport.
type Model struct {
	foundation.ChangeNotifier
	estimate float64
	secs     []sectionState
	tops     []float64
	topsOK   bool
	// scrollTop is the scroll position, viewport and viewportW the size of the area the sections are seen through.
	scrollTop, viewport, viewportW float64
	// gen counts the changes that alter what a section looks like or where it is, except the scroll position.
	gen uint64
	// focused is the section whose header has the keyboard, -1 for none.
	focused int
}

// NewModel makes an empty layout; estimate is the height of a body that is not measured yet (EstimatedBodyHeight when it is not
// positive).
func NewModel(estimate float64) *Model {
	if estimate <= 0 {
		estimate = EstimatedBodyHeight
	}
	return &Model{ChangeNotifier: *foundation.NewChangeNotifier(), estimate: estimate, focused: -1}
}

// Len is the number of sections.
func (m *Model) Len() int { return len(m.secs) }

// Section is the description of section i.
func (m *Model) Section(i int) Section { return m.secs[i].Section }

// Generation counts the changes of the sections, their collapsed marks and their heights; it does not change with the scroll position.
func (m *Model) Generation() uint64 { return m.gen }

// SetSections replaces the sections. A section whose key was there keeps its collapsed mark and its measured height; a section
// that is not any more is forgotten.
func (m *Model) SetSections(list []Section) {
	old := make(map[string]sectionState, len(m.secs))
	for _, s := range m.secs {
		old[s.Key] = s
	}
	focusedKey := ""
	if m.focused >= 0 && m.focused < len(m.secs) {
		focusedKey = m.secs[m.focused].Key
	}
	m.secs = make([]sectionState, len(list))
	m.focused = -1
	for i, s := range list {
		st := sectionState{Section: s, body: m.estimate}
		if prev, ok := old[s.Key]; ok {
			st.collapsed, st.body, st.measured = prev.collapsed, prev.body, prev.measured
		}
		m.secs[i] = st
		if s.Key == focusedKey {
			m.focused = i
		}
	}
	m.changed()
}

// changed marks the tops as stale, keeps the scroll position inside the content and tells the listeners.
func (m *Model) changed() {
	m.topsOK = false
	m.gen++
	m.scrollTop = m.clamp(m.scrollTop)
	m.NotifyListeners()
}

func (m *Model) clamp(top float64) float64 {
	return min(max(top, 0), m.maxTop())
}

func (m *Model) maxTop() float64 { return max(m.Total()-m.viewport, 0) }

// ensureTops makes the prefix sums of the heights: tops[i] is the top of section i and tops[len] the end of the last one.
func (m *Model) ensureTops() {
	if m.topsOK {
		return
	}
	if cap(m.tops) < len(m.secs)+1 {
		m.tops = make([]float64, len(m.secs)+1)
	}
	m.tops = m.tops[:len(m.secs)+1]
	y := 0.0
	for i := range m.secs {
		m.tops[i] = y
		y += m.height(i)
	}
	m.tops[len(m.secs)] = y
	m.topsOK = true
}

func (m *Model) height(i int) float64 {
	s := &m.secs[i]
	if s.collapsed {
		return HeaderHeight
	}
	return HeaderHeight + s.body
}

// Top is the top of section i in the content.
func (m *Model) Top(i int) float64 { m.ensureTops(); return m.tops[i] }

// Height is the height of section i: its header, and its body unless it is collapsed.
func (m *Model) Height(i int) float64 { return m.height(i) }

// BodyHeight is the height of the body of section i (the estimate until it is measured), whether or not it is collapsed.
func (m *Model) BodyHeight(i int) float64 { return m.secs[i].body }

// Measured tells whether the owner of the body reported its height.
func (m *Model) Measured(i int) bool { return m.secs[i].measured }

// Total is the height of the content.
func (m *Model) Total() float64 {
	if len(m.secs) == 0 {
		return 0
	}
	m.ensureTops()
	return m.tops[len(m.secs)]
}

// SetBodyHeight records the height the body of section i measured. The content in view stays where it is: when the section is wholly
// above the viewport the scroll position moves by the change. It reports whether anything changed.
func (m *Model) SetBodyHeight(i int, h float64) bool {
	if i < 0 || i >= len(m.secs) {
		return false
	}
	h = max(math.Round(h), 0)
	s := &m.secs[i]
	if s.measured && s.body == h {
		return false
	}
	top, was := m.Top(i), m.height(i)
	s.body, s.measured = h, true
	if s.collapsed {
		return false // nothing on screen changes; the height is kept for the next time it opens
	}
	if delta := m.height(i) - was; top+was <= m.scrollTop {
		m.scrollTop = max(m.scrollTop+delta, 0)
	}
	m.changed()
	return true
}

// Collapsed tells whether section i is collapsed.
func (m *Model) Collapsed(i int) bool { return m.secs[i].collapsed }

// SetCollapsed collapses or expands section i.
func (m *Model) SetCollapsed(i int, collapsed bool) {
	if i < 0 || i >= len(m.secs) || m.secs[i].collapsed == collapsed {
		return
	}
	top, was := m.Top(i), m.height(i)
	m.secs[i].collapsed = collapsed
	if delta := m.height(i) - was; top+was <= m.scrollTop {
		m.scrollTop = max(m.scrollTop+delta, 0)
	}
	m.changed()
}

// Toggle collapses section i when it is open and opens it when it is collapsed.
func (m *Model) Toggle(i int) {
	if i >= 0 && i < len(m.secs) {
		m.SetCollapsed(i, !m.secs[i].collapsed)
	}
}

// CollapseAll collapses every section, and ExpandAll opens every one.
func (m *Model) CollapseAll() { m.setAll(true) }

// ExpandAll opens every section.
func (m *Model) ExpandAll() { m.setAll(false) }

func (m *Model) setAll(collapsed bool) {
	any := false
	for i := range m.secs {
		if m.secs[i].collapsed != collapsed {
			m.secs[i].collapsed = collapsed
			any = true
		}
	}
	if any {
		m.changed()
	}
}

// AllCollapsed tells whether there are sections and every one is collapsed (VS Code's context key multiDiffEditorAllCollapsed).
func (m *Model) AllCollapsed() bool {
	for _, s := range m.secs {
		if !s.collapsed {
			return false
		}
	}
	return len(m.secs) > 0
}

// ScrollTop is the scroll position.
func (m *Model) ScrollTop() float64 { return m.scrollTop }

// ScrollTo moves the content to a position, kept inside the content.
func (m *Model) scrollTo(top float64) {
	top = m.clamp(top)
	if top == m.scrollTop {
		return
	}
	m.scrollTop = top
	m.NotifyListeners()
}

// SetViewport sets the size of the area the sections are seen through. It tells the listeners of the scroll metrics only when the size
// changed; it is called from the layout of the widget.
func (m *Model) SetViewport(w, h float64) {
	if w == m.viewportW && h == m.viewport {
		return
	}
	m.viewportW, m.viewport = w, h
	m.scrollTop = m.clamp(m.scrollTop)
	m.NotifyListeners()
}

// Viewport is the height of the viewport.
func (m *Model) Viewport() float64 { return m.viewport }

// SectionAt is the section that holds the position y of the content; the first or the last one beyond the ends, -1 with no sections.
func (m *Model) SectionAt(y float64) int {
	n := len(m.secs)
	if n == 0 {
		return -1
	}
	m.ensureTops()
	i := sort.Search(n, func(i int) bool { return m.tops[i+1] > y })
	return min(i, n-1)
}

// Visible gives the sections from first to last (inclusive) that touch the viewport widened by overscan above and below; ok is false
// when there is no section to build.
func (m *Model) Visible(overscan float64) (first, last int, ok bool) {
	n := len(m.secs)
	if n == 0 || m.viewport <= 0 {
		return 0, -1, false
	}
	top, bottom := m.scrollTop-overscan, m.scrollTop+m.viewport+overscan
	first = m.SectionAt(max(top, 0))
	last = m.SectionAt(bottom)
	return first, last, true
}

// Current is the section at the top of the viewport (the one the keys of the editor start from); -1 with no sections.
func (m *Model) Current() int { return m.SectionAt(m.scrollTop) }

// ScrollToSection scrolls so that the header of section i is at the top of the viewport (as far as the content allows).
func (m *Model) ScrollToSection(i int) {
	if i < 0 || i >= len(m.secs) {
		return
	}
	m.scrollTo(m.Top(i))
}

// ScrollToOffset scrolls to a position inside section i (the offset is from the top of its body), putting it a little below the top of
// the viewport; the owner of the body uses it to reveal a change.
func (m *Model) ScrollToOffset(i int, offset float64) {
	if i < 0 || i >= len(m.secs) {
		return
	}
	m.scrollTo(m.Top(i) + HeaderHeight + offset - m.viewport/4)
}

// RevealSection scrolls only as far as needed to bring the header of section i into view.
func (m *Model) RevealSection(i int) {
	if i < 0 || i >= len(m.secs) {
		return
	}
	top := m.Top(i)
	switch {
	case top < m.scrollTop:
		m.scrollTo(top)
	case top+HeaderHeight > m.scrollTop+m.viewport:
		m.scrollTo(top + HeaderHeight - m.viewport)
	}
}

// Focused is the section whose header has the keyboard, -1 for none.
func (m *Model) Focused() int { return m.focused }

// SetFocused gives the keyboard to the header of section i (-1 for none).
func (m *Model) SetFocused(i int) {
	if i >= len(m.secs) {
		i = len(m.secs) - 1
	}
	if i < -1 {
		i = -1
	}
	if m.focused == i {
		return
	}
	m.focused = i
	m.gen++
	m.NotifyListeners()
}

// Scroll is the kit.ScrollModel of the layout: the element that draws the scrollbar reads the metrics from it and moves it.
func (m *Model) Scroll() kit.ScrollModel { return scrollModel{m} }

type scrollModel struct{ m *Model }

func (s scrollModel) AddListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return s.m.AddListener(fn)
}

func (s scrollModel) RemoveListener(h foundation.ListenerHandle) { s.m.RemoveListener(h) }

func (s scrollModel) Metrics() kit.ScrollMetrics {
	m := s.m
	return kit.ScrollMetrics{
		ViewportWidth: m.viewportW, ViewportHeight: m.viewport, ContentWidth: m.viewportW, ContentHeight: m.Total(), Top: m.scrollTop,
	}
}

func (s scrollModel) ScrollTo(_, top float64) { s.m.scrollTo(top) }

// Refresh makes the widget build its sections again without a change of the layout (the message of an empty editor changed).
func (m *Model) Refresh() {
	m.gen++
	m.NotifyListeners()
}
