package multidiff

import (
	"fmt"
	"testing"
)

// sections makes n sections named f0, f1, ...
func sections(n int) []Section {
	out := make([]Section, n)
	for i := range out {
		out[i] = Section{Key: fmt.Sprintf("k%d", i), Name: fmt.Sprintf("f%d.go", i), Folder: "src"}
	}
	return out
}

// newTestModel is a model of n sections with the body height given to each, in a viewport of 500 pixels.
func newTestModel(n int, body float64) *Model {
	m := NewModel(100)
	m.SetSections(sections(n))
	for i := range n {
		m.SetBodyHeight(i, body)
	}
	m.SetViewport(800, 500)
	return m
}

func TestTheHeightsAddUp(t *testing.T) {
	tests := []struct {
		name      string
		collapsed []int
		total     float64
		tops      map[int]float64
	}{
		{"all open", nil, 4 * (HeaderHeight + 200), map[int]float64{0: 0, 1: HeaderHeight + 200, 3: 3 * (HeaderHeight + 200)}},
		{"one collapsed", []int{1}, 3*(HeaderHeight+200) + HeaderHeight, map[int]float64{2: HeaderHeight + 200 + HeaderHeight}},
		{"all collapsed", []int{0, 1, 2, 3}, 4 * HeaderHeight, map[int]float64{3: 3 * HeaderHeight}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(4, 200)
			for _, i := range tt.collapsed {
				m.SetCollapsed(i, true)
			}
			if got := m.Total(); got != tt.total {
				t.Errorf("Total = %v, want %v", got, tt.total)
			}
			for i, want := range tt.tops {
				if got := m.Top(i); got != want {
					t.Errorf("Top(%d) = %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestAnUnmeasuredBodyHasTheEstimate(t *testing.T) {
	m := NewModel(120)
	m.SetSections(sections(3))
	if m.Measured(0) || m.BodyHeight(0) != 120 || m.Total() != 3*(HeaderHeight+120) {
		t.Errorf("estimate: measured %v body %v total %v", m.Measured(0), m.BodyHeight(0), m.Total())
	}
	m.SetBodyHeight(1, 40.4)
	if !m.Measured(1) || m.BodyHeight(1) != 40 {
		t.Errorf("a measured height is rounded: %v", m.BodyHeight(1))
	}
	if NewModel(0).estimate != EstimatedBodyHeight {
		t.Errorf("the default estimate")
	}
}

func TestSetSectionsKeepsTheStateOfTheSectionsThatStay(t *testing.T) {
	m := newTestModel(4, 200)
	m.SetCollapsed(1, true)
	m.SetBodyHeight(2, 77)
	m.SetFocused(2)
	m.SetSections([]Section{{Key: "k2", Name: "f2.go"}, {Key: "new", Name: "n.go"}, {Key: "k1", Name: "f1.go"}})
	if m.Len() != 3 {
		t.Fatalf("Len = %d", m.Len())
	}
	if m.BodyHeight(0) != 77 || !m.Measured(0) || m.Collapsed(0) {
		t.Errorf("k2 lost its height: %v %v", m.BodyHeight(0), m.Measured(0))
	}
	if m.Measured(1) || m.BodyHeight(1) != 100 {
		t.Errorf("a new section has the estimate")
	}
	if !m.Collapsed(2) {
		t.Errorf("k1 lost its collapsed mark")
	}
	if m.Focused() != 0 {
		t.Errorf("the focus follows the key: %d", m.Focused())
	}
}

func TestACollapsedSectionKeepsItsMeasuredBody(t *testing.T) {
	m := newTestModel(3, 200)
	m.SetCollapsed(1, true)
	if changed := m.SetBodyHeight(1, 300); changed {
		t.Errorf("a collapsed section does not change the layout")
	}
	m.SetCollapsed(1, false)
	if m.BodyHeight(1) != 300 || m.Height(1) != HeaderHeight+300 {
		t.Errorf("the height measured while collapsed is kept: %v", m.BodyHeight(1))
	}
}

func TestCollapseAndExpandAll(t *testing.T) {
	m := newTestModel(3, 200)
	if m.AllCollapsed() {
		t.Error("AllCollapsed with nothing collapsed")
	}
	m.CollapseAll()
	if !m.AllCollapsed() || m.Total() != 3*HeaderHeight {
		t.Errorf("after CollapseAll: %v %v", m.AllCollapsed(), m.Total())
	}
	m.SetCollapsed(1, false)
	if m.AllCollapsed() {
		t.Error("one open")
	}
	m.ExpandAll()
	if m.AllCollapsed() || m.Total() != 3*(HeaderHeight+200) {
		t.Errorf("after ExpandAll: %v", m.Total())
	}
	if NewModel(0).AllCollapsed() {
		t.Error("no sections are not all collapsed")
	}
	m.Toggle(0)
	m.Toggle(0)
	m.Toggle(9) // out of range: nothing happens
	if m.Collapsed(0) {
		t.Error("Toggle twice")
	}
}

func TestSectionAtAndVisible(t *testing.T) {
	m := newTestModel(100, 100) // sections of 130 pixels
	h := HeaderHeight + 100
	if m.SectionAt(0) != 0 || m.SectionAt(h-1) != 0 || m.SectionAt(h) != 1 || m.SectionAt(1e9) != 99 || m.SectionAt(-5) != 0 {
		t.Errorf("SectionAt: %d %d %d %d", m.SectionAt(0), m.SectionAt(h-1), m.SectionAt(h), m.SectionAt(1e9))
	}
	if NewModel(0).SectionAt(10) != -1 {
		t.Error("no sections")
	}
	tests := []struct {
		name        string
		scroll      float64
		overscan    float64
		first, last int
	}{
		{"top, no overscan", 0, 0, 0, 3},
		{"top with overscan", 0, 300, 0, 6},
		{"middle", 50 * h, 0, 50, 53},
		{"middle with overscan", 50*h + 10, 300, 47, 56},
		{"end", 1e9, 0, 96, 99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.scrollTo(tt.scroll)
			first, last, ok := m.Visible(tt.overscan)
			if !ok || first != tt.first || last != tt.last {
				t.Errorf("Visible = %d..%d %v, want %d..%d", first, last, ok, tt.first, tt.last)
			}
		})
	}
	if _, _, ok := NewModel(0).Visible(10); ok {
		t.Error("an empty model has nothing to build")
	}
}

func TestScrollingToASectionStopsAtTheEnd(t *testing.T) {
	m := newTestModel(10, 100)
	h := HeaderHeight + 100
	m.ScrollToSection(3)
	if m.ScrollTop() != 3*h || m.Current() != 3 {
		t.Errorf("section 3: top %v current %d", m.ScrollTop(), m.Current())
	}
	m.ScrollToSection(9)
	if want := m.Total() - 500; m.ScrollTop() != want {
		t.Errorf("the last section is as far as the content goes: %v, want %v", m.ScrollTop(), want)
	}
	m.ScrollToSection(-1)
	m.ScrollToSection(77)
	m.ScrollToOffset(2, 40)
	if want := 2*h + HeaderHeight + 40 - 125; m.ScrollTop() != want {
		t.Errorf("ScrollToOffset = %v, want %v", m.ScrollTop(), want)
	}
}

func TestRevealSectionScrollsOnlyAsFarAsNeeded(t *testing.T) {
	m := newTestModel(20, 100)
	h := HeaderHeight + 100
	m.ScrollToSection(5)
	m.RevealSection(6) // in view
	if m.ScrollTop() != 5*h {
		t.Errorf("a header in view does not scroll: %v", m.ScrollTop())
	}
	m.RevealSection(2) // above
	if m.ScrollTop() != 2*h {
		t.Errorf("above: %v", m.ScrollTop())
	}
	m.RevealSection(12) // below
	if want := 12*h + HeaderHeight - 500; m.ScrollTop() != want {
		t.Errorf("below: %v, want %v", m.ScrollTop(), want)
	}
}

// The content in view stays where it is when the body of a section above it is measured.
func TestMeasuringASectionAboveTheViewportKeepsTheContentInPlace(t *testing.T) {
	h := HeaderHeight + 100
	tests := []struct {
		name       string
		section    int
		newHeight  float64
		wantScroll float64
	}{
		{"wholly above, taller", 1, 300, 6*h + 220},
		{"wholly above, shorter", 1, 40, 6*h - 40},
		{"straddling the top of the viewport", 6, 300, 6*h + 20},
		{"below the viewport", 12, 300, 6*h + 20},
		{"the same height", 1, 100, 6*h + 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(20, 100)
			m.scrollTo(6*h + 20)
			// the section in view before: where its header is on the screen
			before := m.Top(6) - m.ScrollTop()
			m.SetBodyHeight(tt.section, tt.newHeight)
			if got := m.ScrollTop(); got != tt.wantScroll {
				t.Errorf("ScrollTop = %v, want %v", got, tt.wantScroll)
			}
			if tt.section < 6 {
				if after := m.Top(6) - m.ScrollTop(); after != before {
					t.Errorf("section 6 moved on the screen from %v to %v", before, after)
				}
			}
		})
	}
}

func TestCollapsingASectionAboveTheViewportKeepsTheContentInPlace(t *testing.T) {
	m := newTestModel(20, 100)
	h := HeaderHeight + 100
	m.scrollTo(6*h + 20)
	before := m.Top(6) - m.ScrollTop()
	m.SetCollapsed(2, true)
	if after := m.Top(6) - m.ScrollTop(); after != before {
		t.Errorf("section 6 moved on the screen from %v to %v", before, after)
	}
}

func TestTheScrollPositionStaysInsideTheContent(t *testing.T) {
	m := newTestModel(10, 100)
	m.scrollTo(1e9)
	m.CollapseAll()
	if m.ScrollTop() != 0 {
		t.Errorf("all collapsed fits in the viewport: %v", m.ScrollTop())
	}
	m.ExpandAll()
	m.scrollTo(-10)
	if m.ScrollTop() != 0 {
		t.Errorf("negative: %v", m.ScrollTop())
	}
	m.scrollTo(1e9)
	m.SetViewport(800, 5000)
	if m.ScrollTop() != 0 {
		t.Errorf("a taller viewport shows everything: %v", m.ScrollTop())
	}
}

func TestTheScrollModelFollowsTheLayout(t *testing.T) {
	m := newTestModel(10, 100)
	sm := m.Scroll()
	calls := 0
	h := sm.AddListener(func() { calls++ })
	metrics := sm.Metrics()
	if metrics.ContentHeight != m.Total() || metrics.ViewportHeight != 500 || metrics.ViewportWidth != 800 || metrics.Top != 0 {
		t.Errorf("metrics %+v", metrics)
	}
	sm.ScrollTo(0, 200)
	if sm.Metrics().Top != 200 || calls != 1 {
		t.Errorf("ScrollTo: top %v calls %d", sm.Metrics().Top, calls)
	}
	sm.ScrollTo(0, 200) // the same place: no call
	m.SetFocused(3)
	if calls != 2 {
		t.Errorf("calls = %d", calls)
	}
	sm.RemoveListener(h)
	sm.ScrollTo(0, 10)
	if calls != 2 {
		t.Errorf("a removed listener is called")
	}
}

func TestFocusedStaysInRange(t *testing.T) {
	m := newTestModel(3, 100)
	g := m.Generation()
	m.SetFocused(7)
	if m.Focused() != 2 {
		t.Errorf("clamped to the last: %d", m.Focused())
	}
	m.SetFocused(-4)
	if m.Focused() != -1 || m.Generation() == g {
		t.Errorf("none: %d", m.Focused())
	}
	g = m.Generation()
	m.scrollTo(30)
	if m.Generation() != g {
		t.Errorf("a scroll does not change the generation")
	}
}
