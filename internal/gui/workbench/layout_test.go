package workbench

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestDefaultLayoutState(t *testing.T) {
	st := DefaultLayoutState()
	want := LayoutState{
		SideBarVisible: true, SideBarWidth: 300, SideBarPosition: SideBarLeft, ActivityBarVisible: true, StatusBarVisible: true,
		PanelVisible: false, PanelPosition: PanelBottom, PanelHeight: 300, PanelWidth: 300,
	}
	if st != want {
		t.Errorf("default state %+v, want %+v", st, want)
	}
	l := NewLayout(st)
	for _, tc := range []struct {
		part Part
		want bool
	}{
		{PartMenuBar, true}, {PartActivityBar, true}, {PartSideBar, true}, {PartEditor, true}, {PartPanel, false}, {PartStatusBar, true},
	} {
		if got := l.Visible(tc.part); got != tc.want {
			t.Errorf("Visible(%s) = %v, want %v", tc.part, got, tc.want)
		}
	}
}

func TestLayoutTransitions(t *testing.T) {
	cases := []struct {
		name  string
		start func(*LayoutState)
		steps func(*Layout)
		want  func(*LayoutState)
	}{
		{"toggle the side bar off", nil, func(l *Layout) { l.ToggleSideBar() }, func(s *LayoutState) { s.SideBarVisible = false }},
		{"toggle the side bar twice", nil, func(l *Layout) { l.ToggleSideBar(); l.ToggleSideBar() }, func(*LayoutState) {}},
		{"toggle the panel on", nil, func(l *Layout) { l.TogglePanel() }, func(s *LayoutState) { s.PanelVisible = true }},
		{"activity bar off", nil, func(l *Layout) { l.SetActivityBarVisible(false) }, func(s *LayoutState) { s.ActivityBarVisible = false }},
		{"status bar off", nil, func(l *Layout) { l.SetStatusBarVisible(false) }, func(s *LayoutState) { s.StatusBarVisible = false }},
		{"side bar to the right", nil, func(l *Layout) { l.SetSideBarPosition(SideBarRight) }, func(s *LayoutState) { s.SideBarPosition = SideBarRight }},
		{"an unknown side bar position is the left", func(s *LayoutState) { s.SideBarPosition = SideBarRight },
			func(l *Layout) { l.SetSideBarPosition("middle") }, func(s *LayoutState) { s.SideBarPosition = SideBarLeft }},
		{"moving the panel shows it", nil, func(l *Layout) { l.SetPanelPosition(PanelRight) },
			func(s *LayoutState) { s.PanelVisible, s.PanelPosition = true, PanelRight }},
		{"moving the panel to the top", nil, func(l *Layout) { l.SetPanelPosition(PanelTop) },
			func(s *LayoutState) { s.PanelVisible, s.PanelPosition = true, PanelTop }},
		{"an unknown panel position changes nothing", nil, func(l *Layout) { l.SetPanelPosition("middle") }, func(*LayoutState) {}},
		{"maximize a hidden panel shows it maximized", nil, func(l *Layout) { l.ToggleMaximizedPanel() },
			func(s *LayoutState) { s.PanelVisible, s.PanelMaximized = true, true }},
		{"maximize and restore", func(s *LayoutState) { s.PanelVisible = true },
			func(l *Layout) { l.ToggleMaximizedPanel(); l.ToggleMaximizedPanel() }, func(s *LayoutState) { s.PanelVisible = true }},
		{"hiding a maximized panel keeps it maximized", func(s *LayoutState) { s.PanelVisible, s.PanelMaximized = true, true },
			func(l *Layout) { l.TogglePanel() }, func(s *LayoutState) { s.PanelVisible = false }},
		{"showing it again maximizes it again", func(s *LayoutState) { s.PanelMaximized = true },
			func(l *Layout) { l.TogglePanel() }, func(s *LayoutState) { s.PanelVisible, s.PanelMaximized = true, true }},
		{"sizes below the least are the least", nil, func(l *Layout) { l.SetSideBarWidth(100) }, func(s *LayoutState) { s.SideBarWidth = SideBarMinWidth }},
		{"a side bar width", nil, func(l *Layout) { l.SetSideBarWidth(420) }, func(s *LayoutState) { s.SideBarWidth = 420 }},
		{"a height of the panel at the bottom", func(s *LayoutState) { s.PanelVisible = true },
			func(l *Layout) { l.SetPanelSize(180) }, func(s *LayoutState) { s.PanelVisible, s.PanelHeight = true, 180 }},
		{"a width of the panel at a side", func(s *LayoutState) { s.PanelVisible, s.PanelPosition = true, PanelLeft },
			func(l *Layout) { l.SetPanelSize(450) }, func(s *LayoutState) { s.PanelVisible, s.PanelPosition, s.PanelWidth = true, PanelLeft, 450 }},
		{"a panel height below the least", func(s *LayoutState) { s.PanelVisible = true },
			func(l *Layout) { l.SetPanelSize(10) }, func(s *LayoutState) { s.PanelVisible, s.PanelHeight = true, PanelMinHeight }},
		{"the size of a hidden panel is not its own", nil, func(l *Layout) { l.SetPanelSize(180) }, func(*LayoutState) {}},
		{"the size of a maximized panel is not its own", func(s *LayoutState) { s.PanelVisible, s.PanelMaximized = true, true },
			func(l *Layout) { l.SetPanelSize(700) }, func(s *LayoutState) { s.PanelVisible, s.PanelMaximized = true, true }},
		{"a size that is not a size", nil, func(l *Layout) { l.SetSideBarWidth(-5); l.SetSideBarWidth(0) }, func(*LayoutState) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := DefaultLayoutState()
			if tc.start != nil {
				tc.start(&start)
			}
			// what a case wants is what its start gives, changed by its want
			want := start
			tc.want(&want)
			l := NewLayout(start)
			tc.steps(l)
			if got := l.State(); got != want {
				t.Errorf("state %+v\nwant  %+v", got, want)
			}
		})
	}
}

func TestPanelOpensMaximized(t *testing.T) {
	cases := []struct {
		mode      PanelOpensMaximized
		wasMax    bool
		wantAfter bool
	}{
		{OpensMaximizedPreserve, true, true}, {OpensMaximizedPreserve, false, false},
		{OpensMaximizedAlways, false, true}, {OpensMaximizedAlways, true, true},
		{OpensMaximizedNever, true, false}, {OpensMaximizedNever, false, false},
		{"anything else", true, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			st := DefaultLayoutState()
			st.PanelMaximized = tc.wasMax
			l := NewLayout(st)
			l.SetPanelOpensMaximized(tc.mode)
			l.SetPanelVisible(true)
			if got := l.State().PanelMaximized; got != tc.wantAfter {
				t.Errorf("maximized after showing = %v, want %v", got, tc.wantAfter)
			}
			if l.PanelMaximized() != tc.wantAfter {
				t.Error("PanelMaximized disagrees with the state")
			}
		})
	}
}

func TestPanelSizeFollowsTheOrientation(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelHeight, st.PanelWidth = 210, 420
	for _, tc := range []struct {
		position PanelPosition
		want     float64
	}{{PanelBottom, 210}, {PanelTop, 210}, {PanelLeft, 420}, {PanelRight, 420}} {
		st.PanelPosition = tc.position
		if got := NewLayout(st).PanelSize(); got != tc.want {
			t.Errorf("%s: PanelSize = %v, want %v", tc.position, got, tc.want)
		}
		if tc.position.Horizontal() != (tc.want == 210) {
			t.Errorf("%s: Horizontal = %v", tc.position, tc.position.Horizontal())
		}
	}
}

func TestStateRoundTrip(t *testing.T) {
	st := LayoutState{
		SideBarVisible: false, SideBarWidth: 333, SideBarPosition: SideBarRight, ActivityBarVisible: false, StatusBarVisible: false,
		PanelVisible: true, PanelPosition: PanelLeft, PanelHeight: 123, PanelWidth: 456, PanelMaximized: true,
	}
	l := NewLayout(DefaultLayoutState())
	var changes int
	l.OnChange(func() { changes++ })
	l.Restore(st)
	if got := l.State(); got != st {
		t.Errorf("State after Restore = %+v, want %+v", got, st)
	}
	if changes != 1 {
		t.Errorf("Restore notified %d times", changes)
	}
	l.Restore(st)
	if changes != 1 {
		t.Errorf("restoring the same state notified again (%d)", changes)
	}
	other := NewLayout(l.State())
	if other.State() != st {
		t.Error("a layout made of a state has another one")
	}
}

func TestRestoreRepairsAStateFromAFile(t *testing.T) {
	cases := []struct {
		name string
		in   LayoutState
		want func(*LayoutState)
	}{
		{"nothing at all is the default sizes and positions", LayoutState{},
			func(s *LayoutState) { s.SideBarVisible, s.ActivityBarVisible, s.StatusBarVisible = false, false, false }},
		{"unknown positions", LayoutState{SideBarPosition: "top", PanelPosition: "diagonal", SideBarWidth: 300, PanelHeight: 300, PanelWidth: 300},
			func(s *LayoutState) { s.SideBarVisible, s.ActivityBarVisible, s.StatusBarVisible = false, false, false }},
		{"sizes below the least", LayoutState{SideBarPosition: SideBarLeft, PanelPosition: PanelBottom, SideBarWidth: 10, PanelHeight: 5, PanelWidth: 20},
			func(s *LayoutState) {
				s.SideBarVisible, s.ActivityBarVisible, s.StatusBarVisible = false, false, false
				s.SideBarWidth, s.PanelHeight, s.PanelWidth = SideBarMinWidth, PanelMinHeight, PanelMinWidth
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := DefaultLayoutState()
			tc.want(&want)
			l := NewLayout(DefaultLayoutState())
			l.Restore(tc.in)
			if got := l.State(); got != want {
				t.Errorf("state %+v\nwant  %+v", got, want)
			}
		})
	}
}

func TestOnChangeTellsOfRealChangesOnly(t *testing.T) {
	l := NewLayout(DefaultLayoutState())
	var n int
	remove := l.OnChange(func() { n++ })
	l.SetSideBarVisible(true) // it is
	l.SetStatusBarVisible(true)
	l.SetSideBarPosition(SideBarLeft)
	if n != 0 {
		t.Fatalf("%d notifications for no change", n)
	}
	l.SetSideBarVisible(false)
	l.SetPanelPosition(PanelRight) // shows the panel and moves it: two changes
	if n != 3 {
		t.Errorf("%d notifications, want 3", n)
	}
	remove()
	l.ToggleSideBar()
	if n != 3 {
		t.Error("a listener that was removed is still told")
	}
}

func TestSlotsFollowThePositions(t *testing.T) {
	order := func(slots []slot) []Part {
		var out []Part
		for _, s := range slots {
			out = append(out, s.part)
		}
		return out
	}
	cases := []struct {
		name       string
		mutate     func(*LayoutState)
		main       []Part
		center     []Part
		horizontal bool
	}{
		{"default", func(*LayoutState) {}, []Part{PartActivityBar, PartSideBar, partCenter}, []Part{PartEditor, PartPanel}, false},
		{"side bar right", func(s *LayoutState) { s.SideBarPosition = SideBarRight }, []Part{partCenter, PartSideBar, PartActivityBar}, []Part{PartEditor, PartPanel}, false},
		{"panel top", func(s *LayoutState) { s.PanelPosition = PanelTop }, []Part{PartActivityBar, PartSideBar, partCenter}, []Part{PartPanel, PartEditor}, false},
		{"panel left", func(s *LayoutState) { s.PanelPosition = PanelLeft }, []Part{PartActivityBar, PartSideBar, partCenter}, []Part{PartPanel, PartEditor}, true},
		{"panel right", func(s *LayoutState) { s.PanelPosition = PanelRight }, []Part{PartActivityBar, PartSideBar, partCenter}, []Part{PartEditor, PartPanel}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := DefaultLayoutState()
			tc.mutate(&st)
			l := NewLayout(st)
			if got := order(l.mainSlots()); !slices.Equal(got, tc.main) {
				t.Errorf("main row %v, want %v", got, tc.main)
			}
			orientation, center := l.centerSlots()
			if got := order(center); !slices.Equal(got, tc.center) {
				t.Errorf("center %v, want %v", got, tc.center)
			}
			if (orientation == kit.SplitHorizontal) != tc.horizontal {
				t.Errorf("orientation %v", orientation)
			}
		})
	}
}

func TestSlotsHideWhatIsHidden(t *testing.T) {
	st := DefaultLayoutState()
	st.SideBarVisible, st.ActivityBarVisible = false, false
	l := NewLayout(st)
	hidden := func(slots []slot) map[Part]bool {
		out := map[Part]bool{}
		for _, s := range slots {
			out[s.part] = s.item.Hidden
		}
		return out
	}
	if h := hidden(l.mainSlots()); !h[PartActivityBar] || !h[PartSideBar] || h[partCenter] {
		t.Errorf("main row hidden: %v", h)
	}
	_, center := l.centerSlots()
	if h := hidden(center); !h[PartPanel] || h[PartEditor] {
		t.Errorf("center hidden: %v", h)
	}
	l.ToggleMaximizedPanel()
	_, center = l.centerSlots()
	if h := hidden(center); h[PartPanel] || !h[PartEditor] {
		t.Errorf("maximized: center hidden: %v", h)
	}
	if l.Visible(PartEditor) {
		t.Error("the editor area is shown while the panel is maximized")
	}
}

// sizesAt lays the panes of a split out in a model of a total size and gives the sizes.
func sizesAt(slots []slot, total float64) (*kit.SplitModel, []float64) {
	slots = fitSlots(slots, total, partCenter)
	items := make([]kit.SplitItem, len(slots))
	for i, s := range slots {
		items[i] = s.item
	}
	m := kit.NewSplitModel(items, total)
	m.Layout(total)
	return m, m.Sizes()
}

func TestFitSlots(t *testing.T) {
	l := NewLayout(DefaultLayoutState())
	for _, total := range []float64{1000, 1280, 600} {
		_, sizes := sizesAt(l.mainSlots(), total)
		want := []float64{48, 300, total - 348}
		if !slices.Equal(sizes, want) {
			t.Errorf("at %v: sizes %v, want %v", total, sizes, want)
		}
	}
	// a hidden pane is not counted
	st := DefaultLayoutState()
	st.SideBarVisible = false
	_, sizes := sizesAt(NewLayout(st).mainSlots(), 1000)
	if !slices.Equal(sizes, []float64{48, 0, 952}) {
		t.Errorf("without the side bar: %v", sizes)
	}
}

func TestWindowResizeIsTakenByTheEditorArea(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	l := NewLayout(st)
	cases := []struct {
		name  string
		total float64
		want  []float64
	}{
		{"grown", 1600, []float64{48, 300, 1252}},
		{"shrunk a little", 900, []float64{48, 300, 552}},
		{"the editor area is at its least", 568, []float64{48, 300, 220}},
		{"then the side bar gives", 500, []float64{48, 232, 220}},
		{"down to its least", 438, []float64{48, 170, 220}},
	}
	m, _ := sizesAt(l.mainSlots(), 1280)
	for _, tc := range cases {
		m.Layout(tc.total)
		if got := m.Sizes(); !slices.Equal(got, tc.want) {
			t.Errorf("%s (%v): %v, want %v", tc.name, tc.total, got, tc.want)
		}
	}
	// and it comes back to where it was
	m.Layout(1280)
	if got := m.Sizes(); got[0] != 48 || got[2] < 220 {
		t.Errorf("grown again: %v", got)
	}

	// the same in the center: the editor area takes the height, down to its least, then the panel
	orientation, center := l.centerSlots()
	if orientation != kit.SplitVertical {
		t.Fatal("the panel is at the bottom")
	}
	cm, sizes := sizesAt(centerAsMain(center), 700)
	if !slices.Equal(sizes, []float64{400, 300}) {
		t.Errorf("center at 700: %v", sizes)
	}
	cm.Layout(500)
	if got := cm.Sizes(); !slices.Equal(got, []float64{200, 300}) {
		t.Errorf("center at 500: %v", got)
	}
	cm.Layout(300)
	if got := cm.Sizes(); !slices.Equal(got, []float64{70, 230}) {
		t.Errorf("center at 300: %v", got)
	}
}

// centerAsMain makes the panes of the center fit sizesAt, which fits the pane that is named partCenter.
func centerAsMain(slots []slot) []slot {
	out := slices.Clone(slots)
	for i := range out {
		if out[i].part == PartEditor {
			out[i].part = partCenter
		}
	}
	return out
}

func TestLayoutStateFromSettings(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		want  func(*LayoutState)
	}{
		{"defaults", nil, func(*LayoutState) {}},
		{"side bar on the right", map[string]any{"workbench.sideBar.location": "right"}, func(s *LayoutState) { s.SideBarPosition = SideBarRight }},
		{"activity bar hidden", map[string]any{"workbench.activityBar.location": "hidden"}, func(s *LayoutState) { s.ActivityBarVisible = false }},
		{"activity bar on top is still beside the side bar", map[string]any{"workbench.activityBar.location": "top"}, func(*LayoutState) {}},
		{"status bar hidden", map[string]any{"workbench.statusBar.visible": false}, func(s *LayoutState) { s.StatusBarVisible = false }},
		{"panel at the right", map[string]any{"workbench.panel.defaultLocation": "right"}, func(s *LayoutState) { s.PanelPosition = PanelRight }},
		{"panel at the top", map[string]any{"workbench.panel.defaultLocation": "top"}, func(s *LayoutState) { s.PanelPosition = PanelTop }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := testutil.Settings(t, testutil.Isolate(t), tc.extra)
			want := DefaultLayoutState()
			tc.want(&want)
			if got := LayoutStateFromSettings(store); got != want {
				t.Errorf("state %+v\nwant  %+v", got, want)
			}
		})
	}
	if got := LayoutStateFromSettings(nil); got != DefaultLayoutState() {
		t.Error("without settings the state is the default")
	}
}

func TestFollowSettings(t *testing.T) {
	store := testutil.Settings(t, testutil.Isolate(t), nil)
	l := NewLayout(DefaultLayoutState())
	var posted int
	stop := l.FollowSettings(store, func(f func()) { posted++; f() })
	update := func(key string, value any) {
		t.Helper()
		if err := store.Update(t.Context(), key, value, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
	}
	update("workbench.statusBar.visible", false)
	update("workbench.sideBar.location", "right")
	update("workbench.activityBar.location", "hidden")
	update("workbench.panel.opensMaximized", "always")
	st := l.State()
	if st.StatusBarVisible || st.SideBarPosition != SideBarRight || st.ActivityBarVisible {
		t.Errorf("the layout did not follow: %+v", st)
	}
	l.SetPanelVisible(true)
	if !l.PanelMaximized() {
		t.Error("workbench.panel.opensMaximized always did not maximize the panel")
	}
	if posted != 4 {
		t.Errorf("%d changes posted, want 4", posted)
	}
	update("editor.fontSize", 15)
	if posted != 4 {
		t.Error("a setting of the editor was posted")
	}
	update("workbench.activityBar.location", "default")
	if !l.State().ActivityBarVisible {
		t.Error("the activity bar did not come back")
	}
	stop()
	update("workbench.statusBar.visible", true)
	if l.State().StatusBarVisible {
		t.Error("the layout follows the settings after stop")
	}
	// without a store there is nothing to follow
	NewLayout(DefaultLayoutState()).FollowSettings(nil, nil)()
}
