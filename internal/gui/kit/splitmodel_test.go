package kit

import (
	"math"
	"reflect"
	"testing"
)

// view is a SplitItem with limits and a priority, as the views of VS Code's splitview tests.
func view(min, max float64, priority ...LayoutPriority) SplitItem {
	it := SplitItem{MinSize: min, MaxSize: max}
	if max == math.Inf(1) {
		it.MaxSize = 0
	}
	if len(priority) > 0 {
		it.Priority = priority[0]
	}
	return it
}

var inf = math.Inf(1)

// vsDefaults are the options of a VS Code split view that says nothing: it is proportional.
var vsDefaults = SplitModelOpts{ProportionalLayout: true}

func checkSizes(t *testing.T, m *SplitModel, want ...float64) {
	t.Helper()
	if got := m.Sizes(); !reflect.DeepEqual(got, want) {
		t.Errorf("sizes %v, want %v", got, want)
	}
}

// The cases of VS Code's splitview.test.ts that do not need its DOM, ported to the model: a case is
// a list of steps on a model of 200.
func TestSplitModelPortedFromVSCode(t *testing.T) {
	t.Run("stretches view to viewport", func(t *testing.T) {
		m := NewSplitModel(nil, 200, vsDefaults)
		m.AddView(view(20, inf), SizeOf(20), 0)
		checkSizes(t, m, 200)
		for _, tc := range []struct{ layout, want float64 }{{200, 200}, {100, 100}, {20, 20}, {10, 20}, {200, 200}} {
			m.Layout(tc.layout)
			checkSizes(t, m, tc.want)
		}
	})

	t.Run("can resize views", func(t *testing.T) {
		m := NewSplitModel(nil, 200, vsDefaults)
		for range 3 {
			m.AddView(view(20, inf), SizeOf(20), 3)
		}
		checkSizes(t, m, 160, 20, 20)
		for _, tc := range []struct {
			index int
			size  float64
			want  []float64
		}{
			{1, 40, []float64{140, 40, 20}},
			{0, 70, []float64{70, 40, 90}},
			{2, 40, []float64{70, 90, 40}},
		} {
			m.ResizeView(tc.index, tc.size)
			checkSizes(t, m, tc.want...)
		}
	})

	t.Run("reacts to view changes", func(t *testing.T) {
		m := NewSplitModel(nil, 200, vsDefaults)
		for range 3 {
			m.AddView(view(20, inf), SizeOf(20), 3)
		}
		checkSizes(t, m, 160, 20, 20)
		steps := []struct {
			index    int
			min, max float64
			want     []float64
		}{
			{0, 20, 20, []float64{20, 20, 160}},
			{2, 20, 40, []float64{20, 140, 40}},
			{1, 20, 200, []float64{20, 140, 40}},
			{2, 100, 0, []float64{20, 80, 100}},
		}
		for _, s := range steps {
			m.UpdateView(s.index, SplitItem{MinSize: s.min, MaxSize: s.max}, math.NaN())
			checkSizes(t, m, s.want...)
		}
	})

	t.Run("sashes are properly enabled and disabled", func(t *testing.T) {
		m := NewSplitModel(nil, 200, vsDefaults)
		for range 3 {
			m.AddView(view(20, inf), SizingDistribute(), 3)
		}
		states := func(want ...SashState) {
			t.Helper()
			if got := m.SashStates(); !reflect.DeepEqual(got, want) {
				t.Errorf("sash states %v, want %v", got, want)
			}
		}
		states(SashEnabled, SashEnabled)
		m.Layout(60)
		states(SashDisabled, SashDisabled)
		m.Layout(20)
		states(SashDisabled, SashDisabled)
		m.Layout(200)
		states(SashEnabled, SashEnabled)
		m.UpdateView(0, SplitItem{MinSize: 20, MaxSize: 20}, math.NaN())
		states(SashDisabled, SashEnabled)
		m.UpdateView(1, SplitItem{MinSize: 20, MaxSize: 20}, math.NaN())
		states(SashDisabled, SashDisabled)
		m.UpdateView(0, SplitItem{MinSize: 20, MaxSize: 300}, math.NaN())
		states(SashAtMinimum, SashAtMinimum)
		m.UpdateView(1, SplitItem{MinSize: 20, MaxSize: 200}, math.NaN())
		states(SashAtMinimum, SashAtMinimum)
		m.ResizeView(0, 40)
		states(SashEnabled, SashEnabled)
	})

	t.Run("issue 35497", func(t *testing.T) {
		m := NewSplitModel(nil, 986, vsDefaults)
		m.AddView(view(160, inf), SizeOf(142), 0)
		checkSizes(t, m, 986)
		m.AddView(view(66, 66), SizeOf(66), 0)
		checkSizes(t, m, 66, 986-66)
	})

	t.Run("automatic size distribution", func(t *testing.T) {
		m := NewSplitModel(nil, 200, vsDefaults)
		m.AddView(view(20, inf), SizingDistribute(), 0)
		checkSizes(t, m, 200)
		m.AddView(view(20, inf), SizeOf(50), 1)
		checkSizes(t, m, 150, 50)
		m.AddView(view(20, inf), SizingDistribute(), 2)
		checkSizes(t, m, 66, 66, 68)
		m.RemoveView(1, SizingDistribute())
		checkSizes(t, m, 100, 100)
	})

	t.Run("add views before layout", func(t *testing.T) {
		m := NewSplitModel(nil, 0, vsDefaults)
		m.AddView(view(20, inf), SizeOf(100), 0)
		m.AddView(view(20, inf), SizeOf(75), 1)
		m.AddView(view(20, inf), SizeOf(25), 2)
		m.Layout(200)
		checkSizes(t, m, 67, 67, 66)
	})

	t.Run("split sizing", func(t *testing.T) {
		m := NewSplitModel(nil, 200, vsDefaults)
		m.AddView(view(20, inf), SizingDistribute(), 0)
		checkSizes(t, m, 200)
		m.AddView(view(20, inf), SizingSplit(0), 1)
		checkSizes(t, m, 100, 100)
		m.AddView(view(20, inf), SizingSplit(1), 2)
		checkSizes(t, m, 100, 50, 50)
	})

	t.Run("split sizing 2", func(t *testing.T) {
		m := NewSplitModel(nil, 200, vsDefaults)
		m.AddView(view(20, inf), SizingDistribute(), 0)
		m.AddView(view(20, inf), SizingSplit(0), 1)
		checkSizes(t, m, 100, 100)
		m.AddView(view(20, inf), SizingSplit(0), 2)
		checkSizes(t, m, 50, 100, 50)
	})

	t.Run("proportional layout", func(t *testing.T) {
		m := NewSplitModel(nil, 200, vsDefaults)
		m.AddView(view(20, inf), SizingDistribute(), 0)
		m.AddView(view(20, inf), SizingDistribute(), 1)
		checkSizes(t, m, 100, 100)
		m.Layout(100)
		checkSizes(t, m, 50, 50)
	})

	t.Run("disable proportional layout", func(t *testing.T) {
		m := NewSplitModel(nil, 200)
		m.AddView(view(20, inf), SizingDistribute(), 0)
		m.AddView(view(20, inf), SizingDistribute(), 1)
		checkSizes(t, m, 100, 100)
		m.Layout(100)
		checkSizes(t, m, 80, 20)
	})

	priorityCase := func(t *testing.T, items [3]SplitItem, steps []struct {
		layout float64
		want   []float64
	}) {
		m := NewSplitModel(nil, 200)
		for _, it := range items {
			m.AddView(it, SizingDistribute(), 3)
		}
		checkSizes(t, m, 66, 68, 66)
		for _, s := range steps {
			m.Layout(s.layout)
			checkSizes(t, m, s.want...)
		}
	}

	t.Run("high layout priority", func(t *testing.T) {
		priorityCase(t, [3]SplitItem{view(20, inf), view(20, inf, LayoutHigh), view(20, inf)}, []struct {
			layout float64
			want   []float64
		}{{180, []float64{66, 48, 66}}, {124, []float64{66, 20, 38}}, {60, []float64{20, 20, 20}}, {200, []float64{20, 160, 20}}})
	})

	t.Run("low layout priority", func(t *testing.T) {
		m := NewSplitModel(nil, 200)
		m.AddView(view(20, inf), SizingDistribute(), 0)
		m.AddView(view(20, inf), SizingDistribute(), 1)
		m.AddView(view(20, inf, LayoutLow), SizingDistribute(), 2)
		checkSizes(t, m, 66, 68, 66)
		for _, s := range []struct {
			layout float64
			want   []float64
		}{{180, []float64{66, 48, 66}}, {132, []float64{46, 20, 66}}, {60, []float64{20, 20, 20}}, {200, []float64{20, 160, 20}}} {
			m.Layout(s.layout)
			checkSizes(t, m, s.want...)
		}
	})

	// The side bar is hidden and the fixed-size activity bar next to it changes its size from
	// within the hiding (VS Code does it in the view's setVisible, here it is done right after).
	t.Run("a view changing its constraints while a sibling is toggled (334167)", func(t *testing.T) {
		m := NewSplitModel(nil, 200)
		m.AddView(view(20, inf, LayoutHigh), SizeOf(120), 0)
		m.AddView(view(20, inf, LayoutLow), SizeOf(70), 1)
		m.AddView(view(10, 10), SizeOf(10), 2)
		m.ResizeView(1, 70)
		checkSizes(t, m, 120, 70, 10)
		m.SetViewVisible(1, false)
		m.UpdateView(2, SplitItem{MinSize: 12, MaxSize: 12}, math.NaN())
		checkSizes(t, m, 188, 0, 12)
		m.SetViewVisible(1, true)
		m.UpdateView(2, SplitItem{MinSize: 10, MaxSize: 10}, math.NaN())
		checkSizes(t, m, 120, 70, 10)
	})
}

// three is a model of three views of 100 with a least size of 20 in a split view of 300.
func three(items ...SplitItem) *SplitModel {
	if len(items) == 0 {
		items = []SplitItem{view(20, inf), view(20, inf), view(20, inf)}
	}
	for i := range items {
		items[i].Size = 100
	}
	m := NewSplitModel(items, 300)
	m.Layout(300)
	return m
}

func TestSplitModelSashDrag(t *testing.T) {
	cases := []struct {
		name  string
		items []SplitItem
		sash  int
		alt   bool
		moves []float64 // pointer positions after the press at 0
		want  []float64
	}{
		{"moves the sash", nil, 0, false, []float64{30}, []float64{130, 70, 100}},
		{"the second sash", nil, 1, false, []float64{-30}, []float64{100, 70, 130}},
		{"follows the pointer back", nil, 0, false, []float64{50, 10}, []float64{110, 90, 100}},
		{"the neighbour shrinks first, then the next", nil, 0, false, []float64{100}, []float64{200, 20, 80}},
		{"stops at the minimum of the views after it", nil, 0, false, []float64{500}, []float64{260, 20, 20}},
		{"stops at the minimum of the views before it", nil, 0, false, []float64{-500}, []float64{20, 180, 100}},
		{"stops at the maximum of the view before it", []SplitItem{view(20, 150), view(20, inf), view(20, inf)}, 0, false, []float64{100}, []float64{150, 50, 100}},
		{"stops at the maximum of the views after it", []SplitItem{view(20, inf), view(20, 120), view(20, 120)}, 0, false, []float64{-100}, []float64{60, 120, 120}},
		{"minimum of the first and the second view", []SplitItem{view(50, inf), view(60, inf), view(20, inf)}, 0, false, []float64{-80}, []float64{50, 150, 100}},
		{"alt resizes the view after the sash on both sides", nil, 0, true, []float64{20}, []float64{120, 60, 120}},
		{"alt on the last sash resizes the view before it", nil, 1, true, []float64{20}, []float64{80, 140, 80}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := three(tc.items...)
			m.SashStart(tc.sash, 0, tc.alt)
			for _, p := range tc.moves {
				m.SashChange(p)
			}
			if index := m.SashEnd(); index != tc.sash {
				t.Errorf("the drag of sash %d ended as sash %d", tc.sash, index)
			}
			checkSizes(t, m, tc.want...)
			if m.ContentSize() != 300 {
				t.Errorf("content size %v, want 300", m.ContentSize())
			}
		})
	}
}

func TestSplitModelSashDragWithoutStart(t *testing.T) {
	m := three()
	m.SashChange(40)
	if got := m.SashEnd(); got != -1 {
		t.Errorf("no drag started, ended %d", got)
	}
	m.SashStart(5, 0, false) // no such sash
	if m.Dragging() {
		t.Error("a sash that does not exist cannot be dragged")
	}
	checkSizes(t, m, 100, 100, 100)
}

func TestSplitModelAltPressedDuringDrag(t *testing.T) {
	m := three()
	m.SashStart(0, 0, false)
	m.SashChange(20) // [120 80 100]
	m.SashSetAlt(true)
	m.SashChange(40) // another 20 from where the pointer was, on both sides
	checkSizes(t, m, 140, 40, 120)
	m.SashSetAlt(false)
	m.SashChange(40)
	m.SashEnd()
	if sum := m.ContentSize(); sum != 300 {
		t.Errorf("content %v", sum)
	}
}

func TestSplitModelInverseAltBehavior(t *testing.T) {
	m := NewSplitModel([]SplitItem{{Size: 100, MinSize: 20}, {Size: 100, MinSize: 20}, {Size: 100, MinSize: 20}}, 300, SplitModelOpts{InverseAltBehavior: true})
	m.SashStart(0, 0, false) // without Alt it is the both-sides drag
	m.SashChange(20)
	m.SashEnd()
	checkSizes(t, m, 120, 60, 120)
}

func TestSplitModelSnap(t *testing.T) {
	// A side bar of 200 (least 100) that snaps, and an editor, in 600.
	newModel := func() *SplitModel {
		return NewSplitModel([]SplitItem{{Size: 200, MinSize: 100, Snap: true, Priority: LayoutLow}, {Size: 400, Priority: LayoutHigh}}, 600)
	}
	cases := []struct {
		name        string
		moves       []float64
		want        []float64
		wantVisible bool
	}{
		{"within the minimum it does not snap", []float64{-100}, []float64{100, 500}, true},
		{"half of the minimum past it, it still shows", []float64{-150}, []float64{100, 500}, true},
		{"past half of the minimum it hides", []float64{-151}, []float64{0, 600}, false},
		{"and shows again when dragged back", []float64{-300, -120}, []float64{100, 500}, true},
		{"dragged on to the right it grows", []float64{80}, []float64{280, 320}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel()
			m.SashStart(0, 0, false)
			for _, p := range tc.moves {
				m.SashChange(p)
			}
			m.SashEnd()
			checkSizes(t, m, tc.want...)
			if m.IsViewVisible(0) != tc.wantVisible {
				t.Errorf("visible %v, want %v", m.IsViewVisible(0), tc.wantVisible)
			}
		})
	}

	t.Run("a hidden side bar comes back with the size it had", func(t *testing.T) {
		m := newModel()
		m.SashStart(0, 0, false)
		m.SashChange(-300)
		m.SashEnd()
		if size, ok := m.CachedVisibleSize(0); !ok || size != 200 {
			t.Errorf("cached %v %v, want 200", size, ok)
		}
		m.SetViewVisible(0, true)
		checkSizes(t, m, 200, 400)
		if _, ok := m.CachedVisibleSize(0); ok {
			t.Error("a view that is shown has no cached size")
		}
	})

	t.Run("the sash of a hidden side bar can open it", func(t *testing.T) {
		m := newModel()
		m.SetViewVisible(0, false)
		checkSizes(t, m, 0, 600)
		if got := m.SashStates(); got[0] != SashAtMinimum {
			t.Errorf("sash state %v, want AtMinimum", got[0])
		}
		m.SetSnapping(false, true)
		if got := m.SashStates(); got[0] != SashDisabled {
			t.Errorf("without snapping at the start: %v, want Disabled", got[0])
		}
	})
}

func TestSplitModelVisibility(t *testing.T) {
	cases := []struct {
		name  string
		items []SplitItem
		steps func(m *SplitModel)
		want  []float64
	}{
		{"hide gives the space to the last view", nil, func(m *SplitModel) { m.SetViewVisible(1, false) }, []float64{100, 0, 200}},
		{"hide the last gives it to the one before", nil, func(m *SplitModel) { m.SetViewVisible(2, false) }, []float64{100, 200, 0}},
		{"high priority takes it", []SplitItem{view(20, inf), view(20, inf, LayoutHigh), view(20, inf)}, func(m *SplitModel) { m.SetViewVisible(0, false) }, []float64{0, 200, 100}},
		{"hide and show", nil, func(m *SplitModel) { m.SetViewVisible(1, false); m.SetViewVisible(1, true) }, []float64{100, 100, 100}},
		{"hide twice is the same", nil, func(m *SplitModel) { m.SetViewVisible(1, false); m.SetViewVisible(1, false) }, []float64{100, 0, 200}},
		{"started hidden", []SplitItem{view(20, inf), {MinSize: 20, Hidden: true}, view(20, inf)}, func(m *SplitModel) {}, []float64{100, 0, 200}},
		{"started hidden, then shown", []SplitItem{view(20, inf), {MinSize: 20, Hidden: true}, view(20, inf)}, func(m *SplitModel) { m.SetViewVisible(1, true) }, []float64{100, 100, 100}},
		{"out of range does nothing", nil, func(m *SplitModel) { m.SetViewVisible(7, false) }, []float64{100, 100, 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := three(tc.items...)
			tc.steps(m)
			checkSizes(t, m, tc.want...)
		})
	}
	t.Run("a hidden view is shown within its limits", func(t *testing.T) {
		m := three(view(20, inf), view(20, 60), view(20, inf))
		m.SetViewVisible(1, false)
		m.SetViewVisible(1, true)
		checkSizes(t, m, 140, 60, 100)
	})
}

func TestSplitModelWindowResize(t *testing.T) {
	// A side bar, an editor that absorbs, a panel of low priority.
	items := func() []SplitItem {
		return []SplitItem{{Size: 100, MinSize: 50}, {Size: 400, MinSize: 100, Priority: LayoutHigh}, {Size: 100, MinSize: 50, Priority: LayoutLow}}
	}
	cases := []struct {
		name         string
		proportional bool
		sizes        []float64
		want         [][]float64
	}{
		{"the high priority view takes the growth and the loss", false, []float64{800, 700, 300, 1000}, [][]float64{{100, 600, 100}, {100, 500, 100}, {100, 100, 100}, {100, 800, 100}}},
		{"when it is at its minimum the others give way, the low one last", false, []float64{200, 150}, [][]float64{{50, 100, 50}, {50, 100, 50}}},
		{"proportional keeps the shares", true, []float64{1200, 300}, [][]float64{{200, 800, 200}, {50, 200, 50}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewSplitModel(items(), 600, SplitModelOpts{ProportionalLayout: tc.proportional})
			for i, size := range tc.sizes {
				m.Layout(size)
				checkSizes(t, m, tc.want[i]...)
			}
		})
	}
	t.Run("narrower than the minimums the content overflows", func(t *testing.T) {
		m := NewSplitModel(items(), 600)
		m.Layout(120)
		checkSizes(t, m, 50, 100, 50)
		if m.ContentSize() != 200 || m.MinimumSize() != 200 {
			t.Errorf("content %v minimum %v, want 200", m.ContentSize(), m.MinimumSize())
		}
		m.Layout(600)
		checkSizes(t, m, 50, 500, 50)
	})
	t.Run("the low priority view keeps its size while the normal one can give way", func(t *testing.T) {
		m := NewSplitModel([]SplitItem{{Size: 100, MinSize: 50}, {Size: 200, MinSize: 200, Priority: LayoutHigh}, {Size: 100, MinSize: 50, Priority: LayoutLow}}, 400)
		m.Layout(350)
		checkSizes(t, m, 50, 200, 100)
	})
}

func TestSplitModelReset(t *testing.T) {
	newModel := func() *SplitModel {
		m := NewSplitModel([]SplitItem{{Size: 200, MinSize: 100, PreferredSize: 300, Priority: LayoutLow}, {Size: 400, MinSize: 100, Priority: LayoutHigh}, {Size: 0, MinSize: 50, PreferredSize: 150, Priority: LayoutLow}}, 600)
		m.Layout(600)
		return m
	}
	cases := []struct {
		name string
		sash int
		want []float64
	}{
		{"the view before the sash has a preferred size", 0, []float64{300, 250, 50}},
		{"the view after the sash has a preferred size", 1, []float64{200, 250, 150}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel()
			checkSizes(t, m, 200, 350, 50)
			m.ResetSash(tc.sash)
			checkSizes(t, m, tc.want...)
		})
	}
	t.Run("without preferred sizes the views are distributed", func(t *testing.T) {
		m := three()
		m.ResizeView(0, 160)
		m.ResetSash(0)
		checkSizes(t, m, 100, 100, 100)
	})
	t.Run("the edge of a hidden side bar does not reset", func(t *testing.T) {
		m := NewSplitModel([]SplitItem{{Size: 200, MinSize: 100, Snap: true, PreferredSize: 300}, {Size: 400}}, 600)
		m.SetViewVisible(0, false)
		if m.CanResetSash(0) {
			t.Error("a hidden side bar that snaps cannot be reset")
		}
		m.ResetSash(0)
		checkSizes(t, m, 0, 600)
	})
}

func TestSplitModelQueries(t *testing.T) {
	m := three(view(20, 150), view(30, inf), view(20, inf))
	if m.Len() != 3 || m.Size() != 300 {
		t.Errorf("len %d size %v", m.Len(), m.Size())
	}
	if got, want := m.Offset(2), 200.0; got != want {
		t.Errorf("offset %v, want %v", got, want)
	}
	if got := m.SashPosition(0); got != 100 {
		t.Errorf("sash position %v", got)
	}
	if got := m.ViewSize(9); got != -1 {
		t.Errorf("view size out of range %v", got)
	}
	if got := m.MinimumSize(); got != 70 {
		t.Errorf("minimum %v", got)
	}
	if got := m.MaximumSize(); !math.IsInf(got, 1) {
		t.Errorf("maximum %v", got)
	}
	if got := NewSplitModel(nil, 10).MaximumSize(); !math.IsInf(got, 1) {
		t.Errorf("an empty model is unbounded: %v", got)
	}
	if m.IsViewExpanded(0) {
		t.Error("the others are above their minimum")
	}
	m.ResizeView(2, 250)
	if !m.IsViewExpanded(2) {
		t.Errorf("the others are at their minimum: %v", m.Sizes())
	}
	if c := m.Constraints(0); c.MaxSize != 150 || c.MinSize != 20 {
		t.Errorf("constraints %+v", c)
	}
	if _, ok := m.CachedVisibleSize(0); ok || m.IsViewVisible(-1) {
		t.Error("out of range")
	}
}

func TestSplitModelAddAndRemove(t *testing.T) {
	cases := []struct {
		name  string
		steps func(m *SplitModel)
		want  []float64
	}{
		{"auto distributes equal views", func(m *SplitModel) { m.AddView(view(20, inf), SizingAuto(0), 3) }, []float64{75, 75, 75, 75}},
		{"auto splits unequal views", func(m *SplitModel) { m.ResizeView(0, 160); m.AddView(view(20, inf), SizingAuto(0), 1) }, []float64{80, 80, 100, 40}},
		{"invisible", func(m *SplitModel) { m.AddView(view(20, inf), SizingInvisible(80), 1) }, []float64{100, 0, 100, 100}},
		{"remove gives the space by priority", func(m *SplitModel) { m.RemoveView(1, Sizing{}) }, []float64{100, 200}},
		{"remove splits", func(m *SplitModel) { m.RemoveView(1, SizingSplit(0)) }, []float64{200, 100}},
		{"remove out of range", func(m *SplitModel) { m.RemoveView(8, Sizing{}) }, []float64{100, 100, 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := three()
			tc.steps(m)
			checkSizes(t, m, tc.want...)
		})
	}
}

func TestSplitModelUpdateViewKeepsNeighbours(t *testing.T) {
	m := three(view(20, inf), view(20, inf, LayoutHigh), view(20, inf))
	m.UpdateView(0, SplitItem{MinSize: 20, Priority: LayoutNormal}, 150)
	// The high priority view gives the 50 up, not the neighbour that was just resized.
	checkSizes(t, m, 150, 50, 100)
	// Unchanged limits change nothing.
	m.UpdateView(0, SplitItem{MinSize: 20}, math.NaN())
	checkSizes(t, m, 150, 50, 100)
}

func TestSplitModelDistribute(t *testing.T) {
	m := three(view(20, inf), view(20, 80), view(20, inf))
	m.ResizeView(0, 50)
	m.DistributeViewSizes()
	got := m.Sizes()
	if got[1] > 80 || got[0] < 20 || got[2] < 20 || got[0]+got[1]+got[2] != 300 {
		t.Errorf("sizes %v", got)
	}
}
