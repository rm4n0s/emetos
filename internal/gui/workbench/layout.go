package workbench

import (
	"math"
	"slices"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The sizes of the parts, in logical pixels: VS Code's.
//
// Ported from VS Code: src/vs/workbench/browser/parts/{activitybar/activitybarPart.ts,
// sidebar/sidebarPart.ts, panel/panelPart.ts, statusbar/statusbarPart.ts, editor/editorPart.ts},
// src/vs/workbench/browser/layout.ts (LayoutStateKeys)
const (
	// ActivityBarWidth is the width of the activity bar, which has no other.
	ActivityBarWidth = 48.0
	// SideBarDefaultWidth and SideBarMinWidth are the width the side bar starts with and the least it
	// can have.
	SideBarDefaultWidth = 300.0
	SideBarMinWidth     = 170.0
	// PanelDefaultSize is the height of a panel above or below the editors, or the width of a panel
	// beside them, when nothing else was set. PanelMinHeight and PanelMinWidth are the least each can be.
	PanelDefaultSize = 300.0
	PanelMinHeight   = 77.0
	PanelMinWidth    = 300.0
	// EditorMinWidth and EditorMinHeight are the least size of the editor area.
	EditorMinWidth  = 220.0
	EditorMinHeight = 70.0
	// StatusBarHeight is the height of the status bar.
	StatusBarHeight = 22.0
	// MenuBarHeight is the height of the menu bar area.
	MenuBarHeight = 30.0
	// PartTitleHeight is the height of the title of the side bar and of the panel.
	PartTitleHeight = 35.0
)

// Part names a part of the workbench (SPEC §10.1).
type Part int

// The parts of the workbench.
const (
	// PartMenuBar is the menu bar area at the top.
	PartMenuBar Part = iota
	// PartActivityBar is the activity bar, on the side of the side bar.
	PartActivityBar
	// PartSideBar is the primary side bar.
	PartSideBar
	// PartEditor is the editor area.
	PartEditor
	// PartPanel is the panel.
	PartPanel
	// PartStatusBar is the status bar at the bottom.
	PartStatusBar
	// partCenter is the editor area with the panel, which the main row lays out as one pane.
	partCenter
)

// String is the name of the part.
func (p Part) String() string {
	switch p {
	case PartMenuBar:
		return "menuBar"
	case PartActivityBar:
		return "activityBar"
	case PartSideBar:
		return "sideBar"
	case PartEditor:
		return "editor"
	case PartPanel:
		return "panel"
	case PartStatusBar:
		return "statusBar"
	case partCenter:
		return "center"
	}
	return "unknown"
}

// SideBarPosition is the side of the window the side bar (and the activity bar) are on; the values are
// those of workbench.sideBar.location.
type SideBarPosition string

// The positions of the side bar.
const (
	SideBarLeft  SideBarPosition = "left"
	SideBarRight SideBarPosition = "right"
)

// PanelPosition is where the panel is, from the editor area; the values are those of
// workbench.panel.defaultLocation and of the context key panelPosition.
type PanelPosition string

// The positions of the panel.
const (
	PanelBottom PanelPosition = "bottom"
	PanelTop    PanelPosition = "top"
	PanelLeft   PanelPosition = "left"
	PanelRight  PanelPosition = "right"
)

// Valid says whether p is one of the positions.
func (p PanelPosition) Valid() bool {
	return p == PanelBottom || p == PanelTop || p == PanelLeft || p == PanelRight
}

// Horizontal says whether the panel is above or below the editors (VS Code's isHorizontal): its
// size is then a height, and it is a width when the panel is at a side.
func (p PanelPosition) Horizontal() bool { return p == PanelBottom || p == PanelTop }

// LayoutState is the part of the layout that is kept between runs: which parts are shown, how large
// the side bar and the panel are, where they are, and whether the panel is maximized (persisted in
// step 7.5). The zero value is not a layout; DefaultLayoutState is.
type LayoutState struct {
	// SideBarVisible says whether the primary side bar is shown.
	SideBarVisible bool `json:"sideBarVisible"`
	// SideBarWidth is the width of the side bar when it is shown, and what it comes back to.
	SideBarWidth float64 `json:"sideBarWidth"`
	// SideBarPosition is the side of the window of the side bar and the activity bar.
	SideBarPosition SideBarPosition `json:"sideBarPosition"`
	// ActivityBarVisible says whether the activity bar is shown (workbench.activityBar.location is
	// not hidden).
	ActivityBarVisible bool `json:"activityBarVisible"`
	// StatusBarVisible says whether the status bar is shown (workbench.statusBar.visible).
	StatusBarVisible bool `json:"statusBarVisible"`
	// PanelVisible says whether the panel is shown.
	PanelVisible bool `json:"panelVisible"`
	// PanelPosition is where the panel is.
	PanelPosition PanelPosition `json:"panelPosition"`
	// PanelHeight is the height of the panel when it is above or below the editors, and PanelWidth
	// its width when it is at a side. Both are the sizes the panel has when it is not maximized,
	// which it comes back to: the last non-maximized size.
	PanelHeight float64 `json:"panelHeight"`
	PanelWidth  float64 `json:"panelWidth"`
	// PanelMaximized says that the panel takes the place of the editor area while it is shown. It
	// is kept while the panel is hidden, and workbench.panel.opensMaximized says whether showing the
	// panel again brings it back.
	PanelMaximized bool `json:"panelMaximized"`
	// CenteredLayout says that the user (or zen mode) asked for the editor area to be centered between two margins
	// (step 8.5). It is the request: whether the margins show also depends on the groups (see CenteredLayoutActive of
	// the shell). A state written before step 8.5 has none.
	CenteredLayout bool `json:"centeredLayout,omitempty"`
	// Zen is zen mode: whether it is on, and what it did, to be undone when it ends (a state without it is not in zen
	// mode).
	Zen ZenState `json:"zen,omitzero"`
}

// ZenState is what zen mode keeps of the layout it found, as VS Code's zenModeExitInfo does (SPEC §10.9). It is
// saved with the layout, so that a window that closes in zen mode starts in it again (zenMode.restore) or leaves it
// with the parts it had.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (zenModeExitInfo, toggleZenMode)
type ZenState struct {
	// Active says that zen mode is on.
	Active bool `json:"active,omitempty"`
	// HidSideBar, HidPanel, HidActivityBar and HidStatusBar say that the part was shown when zen mode began and zen mode
	// hid it; only those come back when it ends.
	HidSideBar     bool `json:"hidSideBar,omitempty"`
	HidPanel       bool `json:"hidPanel,omitempty"`
	HidActivityBar bool `json:"hidActivityBar,omitempty"`
	HidStatusBar   bool `json:"hidStatusBar,omitempty"`
	// ChangedSideBar, ChangedPanel, ChangedActivityBar and ChangedStatusBar say that the user showed or hid the part
	// while zen mode was on: it stays as the user left it when zen mode ends.
	ChangedSideBar     bool `json:"changedSideBar,omitempty"`
	ChangedPanel       bool `json:"changedPanel,omitempty"`
	ChangedActivityBar bool `json:"changedActivityBar,omitempty"`
	ChangedStatusBar   bool `json:"changedStatusBar,omitempty"`
	// FullScreen, Centered and DoNotDisturb say that zen mode turned full screen, the centered layout and do not
	// disturb on (they were off), so that ending it turns them off again (VS Code's transitionedToFullScreen,
	// transitionedToCenteredEditorLayout and handleNotificationsDoNotDisturbMode).
	FullScreen   bool `json:"fullScreen,omitempty"`
	Centered     bool `json:"centered,omitempty"`
	DoNotDisturb bool `json:"doNotDisturb,omitempty"`
}

// DefaultLayoutState is the layout of a window that has nothing saved: the side bar, the activity bar
// and the status bar shown on the left and at the bottom, the panel at the bottom and hidden (as in
// VS Code), and the default sizes.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (LayoutStateKeys)
func DefaultLayoutState() LayoutState {
	return LayoutState{
		SideBarVisible: true, SideBarWidth: SideBarDefaultWidth, SideBarPosition: SideBarLeft,
		ActivityBarVisible: true, StatusBarVisible: true,
		PanelVisible: false, PanelPosition: PanelBottom, PanelHeight: PanelDefaultSize, PanelWidth: PanelDefaultSize,
	}
}

// LayoutStateFromSettings is DefaultLayoutState with what the settings say: the position of the side
// bar, whether the activity bar and the status bar are shown, and where the panel is.
func LayoutStateFromSettings(s *settings.Store) LayoutState {
	st := DefaultLayoutState()
	if s == nil {
		return st
	}
	st.SideBarPosition = sideBarPosition(settings.Get[string](s, "workbench.sideBar.location", ""))
	st.ActivityBarVisible = settings.Get[string](s, "workbench.activityBar.location", "") != "hidden"
	st.StatusBarVisible = settings.Get[bool](s, "workbench.statusBar.visible", "")
	if p := PanelPosition(settings.Get[string](s, "workbench.panel.defaultLocation", "")); p.Valid() {
		st.PanelPosition = p
	}
	return st
}

func sideBarPosition(v string) SideBarPosition {
	if v == string(SideBarRight) {
		return SideBarRight
	}
	return SideBarLeft
}

// sanitize makes a state that was read from a file fit for use: positions that are not known become the
// defaults, and sizes that are not numbers or are below the least become the defaults or the least.
func (s LayoutState) sanitize() LayoutState {
	d := DefaultLayoutState()
	if s.SideBarPosition != SideBarLeft && s.SideBarPosition != SideBarRight {
		s.SideBarPosition = d.SideBarPosition
	}
	if !s.PanelPosition.Valid() {
		s.PanelPosition = d.PanelPosition
	}
	s.SideBarWidth = sanitizeSize(s.SideBarWidth, d.SideBarWidth, SideBarMinWidth)
	s.PanelHeight = sanitizeSize(s.PanelHeight, d.PanelHeight, PanelMinHeight)
	s.PanelWidth = sanitizeSize(s.PanelWidth, d.PanelWidth, PanelMinWidth)
	return s
}

func sanitizeSize(v, def, least float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return def
	}
	return max(v, least)
}

// PanelOpensMaximized is the value of workbench.panel.opensMaximized.
type PanelOpensMaximized string

// The values of workbench.panel.opensMaximized.
const (
	// OpensMaximizedPreserve opens the panel in the state it was in when it was hidden (the default).
	OpensMaximizedPreserve PanelOpensMaximized = "preserve"
	// OpensMaximizedAlways maximizes the panel whenever it opens.
	OpensMaximizedAlways PanelOpensMaximized = "always"
	// OpensMaximizedNever opens the panel as it is not maximized.
	OpensMaximizedNever PanelOpensMaximized = "never"
)

// Layout is the model of the layout of the workbench: which parts are shown, the sizes of the side
// bar and the panel, and where they are. The widgets show it and the commands change it; it has no
// widget. Its methods belong to the UI thread; State and Restore are how it is kept between runs
// (step 7.5 writes it).
type Layout struct {
	state LayoutState
	opens PanelOpensMaximized
	// restores counts the calls of Restore that changed the layout: the split views start again from
	// the sizes of the state after one, since they keep their own sizes otherwise.
	restores int
	// inZen is true while EnterZen and ExitZen change the parts, which are not the user's changes.
	inZen bool

	mu        sync.Mutex
	listeners []*func()
}

// NewLayout makes a layout from a state (see Restore for what is made of a state that is not
// right).
func NewLayout(state LayoutState) *Layout {
	return &Layout{state: state.sanitize(), opens: OpensMaximizedPreserve}
}

// State is the part of the layout that is kept.
func (l *Layout) State() LayoutState { return l.state }

// Restore replaces the layout by a state that was kept: positions it does not know become the
// defaults and sizes below the least become the least.
func (l *Layout) Restore(state LayoutState) {
	state = state.sanitize()
	if state != l.state {
		l.restores++
	}
	l.set(state)
}

// OnChange registers fn to be called, on the UI thread, after the layout changed. The returned
// function removes it.
func (l *Layout) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	l.mu.Lock()
	l.listeners = append(l.listeners, p)
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.listeners = slices.DeleteFunc(l.listeners, func(x *func()) bool { return x == p })
	}
}

// set replaces the state and tells the listeners when it is another one.
func (l *Layout) set(s LayoutState) {
	if s == l.state {
		return
	}
	if old := l.state; old.Zen.Active && s.Zen.Active && !l.inZen {
		// a part that changes while zen mode is on is the user's doing: zen mode leaves it as it is
		s.Zen.ChangedSideBar = s.Zen.ChangedSideBar || old.SideBarVisible != s.SideBarVisible
		s.Zen.ChangedPanel = s.Zen.ChangedPanel || old.PanelVisible != s.PanelVisible
		s.Zen.ChangedActivityBar = s.Zen.ChangedActivityBar || old.ActivityBarVisible != s.ActivityBarVisible
		s.Zen.ChangedStatusBar = s.Zen.ChangedStatusBar || old.StatusBarVisible != s.StatusBarVisible
	}
	l.state = s
	l.mu.Lock()
	listeners := slices.Clone(l.listeners)
	l.mu.Unlock()
	for _, f := range listeners {
		(*f)()
	}
}

// SetPanelOpensMaximized sets what showing the panel does to its maximized state
// (workbench.panel.opensMaximized).
func (l *Layout) SetPanelOpensMaximized(v PanelOpensMaximized) {
	switch v {
	case OpensMaximizedAlways, OpensMaximizedNever:
		l.opens = v
	default:
		l.opens = OpensMaximizedPreserve
	}
}

// Visible says whether a part is shown. The editor area is shown unless the panel is maximized;
// the menu bar area is the window's, and whether it is shown is the menu bar's own business.
func (l *Layout) Visible(p Part) bool {
	s := l.state
	switch p {
	case PartActivityBar:
		return s.ActivityBarVisible
	case PartSideBar:
		return s.SideBarVisible
	case PartEditor:
		return !l.PanelMaximized()
	case PartPanel:
		return s.PanelVisible
	}
	return true
}

// PanelMaximized says whether the panel is shown and takes the place of the editor area.
func (l *Layout) PanelMaximized() bool { return l.state.PanelVisible && l.state.PanelMaximized }

// PanelSize is the size of the panel along the axis it is laid out on: its height above or below the
// editors, its width beside them. While the panel is maximized it is the size it comes back to.
func (l *Layout) PanelSize() float64 {
	if l.state.PanelPosition.Horizontal() {
		return l.state.PanelHeight
	}
	return l.state.PanelWidth
}

// SetSideBarVisible shows or hides the side bar.
func (l *Layout) SetSideBarVisible(v bool) {
	s := l.state
	s.SideBarVisible = v
	l.set(s)
}

// ToggleSideBar shows the side bar when it is hidden and hides it when it is shown.
func (l *Layout) ToggleSideBar() { l.SetSideBarVisible(!l.state.SideBarVisible) }

// SetActivityBarVisible shows or hides the activity bar.
func (l *Layout) SetActivityBarVisible(v bool) {
	s := l.state
	s.ActivityBarVisible = v
	l.set(s)
}

// SetStatusBarVisible shows or hides the status bar.
func (l *Layout) SetStatusBarVisible(v bool) {
	s := l.state
	s.StatusBarVisible = v
	l.set(s)
}

// SetSideBarPosition moves the side bar and the activity bar to a side.
func (l *Layout) SetSideBarPosition(p SideBarPosition) {
	s := l.state
	s.SideBarPosition = sideBarPosition(string(p))
	l.set(s)
}

// SetPanelVisible shows or hides the panel. Showing it brings back its maximized state, or sets it
// as workbench.panel.opensMaximized says.
func (l *Layout) SetPanelVisible(v bool) {
	s := l.state
	if v {
		s = showPanel(s, l.opens)
	} else {
		s.PanelVisible = false
	}
	l.set(s)
}

// TogglePanel shows the panel when it is hidden and hides it when it is shown.
func (l *Layout) TogglePanel() { l.SetPanelVisible(!l.state.PanelVisible) }

// SetPanelPosition moves the panel (and shows it, as VS Code does). Ignored for a position that is not
// one of the four.
func (l *Layout) SetPanelPosition(p PanelPosition) {
	if !p.Valid() {
		return
	}
	l.SetPanelVisible(true)
	s := l.state
	s.PanelPosition = p
	l.set(s)
}

// ToggleMaximizedPanel maximizes the panel, or brings it back to the size it had; a hidden panel is
// shown maximized (VS Code's workbench.action.toggleMaximizedPanel).
func (l *Layout) ToggleMaximizedPanel() {
	s := l.state
	switch {
	case !s.PanelVisible:
		s.PanelVisible = true
		s.PanelMaximized = true
	default:
		s.PanelMaximized = !s.PanelMaximized
	}
	l.set(s)
}

// SetSideBarWidth records the width the side bar has now (a drag of its sash); a width below the
// least is the least.
func (l *Layout) SetSideBarWidth(w float64) {
	if w <= 0 || math.IsNaN(w) {
		return
	}
	s := l.state
	s.SideBarWidth = max(w, SideBarMinWidth)
	l.set(s)
}

// SetPanelSize records the size the panel has now along its axis (a drag of its sash). It is
// ignored while the panel is hidden or maximized, when its size is not its own.
func (l *Layout) SetPanelSize(size float64) {
	if size <= 0 || math.IsNaN(size) || !l.state.PanelVisible || l.state.PanelMaximized {
		return
	}
	s := l.state
	if s.PanelPosition.Horizontal() {
		s.PanelHeight = max(size, PanelMinHeight)
	} else {
		s.PanelWidth = max(size, PanelMinWidth)
	}
	l.set(s)
}

// FollowSettings applies the settings that decide the layout now, and whenever they change: the
// position of the side bar, the visibility of the activity bar (workbench.activityBar.location) and
// of the status bar, and what showing the panel does to its maximized state. apply runs on the
// goroutine that changed the settings, so a caller that is not on the UI thread posts it. It returns
// the function that stops following.
func (l *Layout) FollowSettings(s *settings.Store, post func(func())) (stop func()) {
	if s == nil {
		return func() {}
	}
	if post == nil {
		post = func(f func()) { f() }
	}
	apply := func() {
		l.SetSideBarPosition(sideBarPosition(settings.Get[string](s, "workbench.sideBar.location", "")))
		// a part that zen mode holds hidden stays hidden; a setting that now hides it takes it out of what zen mode
		// brings back
		if visible := settings.Get[string](s, "workbench.activityBar.location", "") != "hidden"; !l.zenHolds(PartActivityBar, visible) {
			l.SetActivityBarVisible(visible)
		}
		if visible := settings.Get[bool](s, "workbench.statusBar.visible", ""); !l.zenHolds(PartStatusBar, visible) {
			l.SetStatusBarVisible(visible)
		}
		l.SetPanelOpensMaximized(PanelOpensMaximized(settings.Get[string](s, "workbench.panel.opensMaximized", "")))
	}
	apply()
	return s.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("workbench.sideBar", "") || e.Affects("workbench.activityBar", "") ||
			e.Affects("workbench.statusBar", "") || e.Affects("workbench.panel", "") {
			post(apply)
		}
	})
}

// zenHolds says that zen mode keeps a part hidden while the settings want it shown (visible): it hid the part and
// the user has not touched it since. When the settings want it hidden zen mode has nothing to bring back, and the
// part is not held.
func (l *Layout) zenHolds(p Part, visible bool) bool {
	z := &l.state.Zen
	if !z.Active {
		return false
	}
	switch p {
	case PartActivityBar:
		if z.HidActivityBar && !z.ChangedActivityBar {
			if !visible {
				z.HidActivityBar = false
			}
			return visible
		}
	case PartStatusBar:
		if z.HidStatusBar && !z.ChangedStatusBar {
			if !visible {
				z.HidStatusBar = false
			}
			return visible
		}
	}
	return false
}

// ZenActive says whether zen mode is on.
func (l *Layout) ZenActive() bool { return l.state.Zen.Active }

// EnterZen turns zen mode on: it remembers which of the side bar, the panel, the activity bar and the status bar are
// shown, and hides the side bar and the panel always and the activity bar and the status bar when the arguments say
// so (zenMode.hideActivityBar, zenMode.hideStatusBar). It does nothing when zen mode is on already. The caller sets
// what else it did with SetZenTransitions.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (toggleZenMode)
func (l *Layout) EnterZen(hideActivityBar, hideStatusBar bool) {
	if l.state.Zen.Active {
		return
	}
	s := l.state
	s.Zen = ZenState{
		Active: true, HidSideBar: s.SideBarVisible, HidPanel: s.PanelVisible,
		HidActivityBar: hideActivityBar && s.ActivityBarVisible, HidStatusBar: hideStatusBar && s.StatusBarVisible,
	}
	s.SideBarVisible, s.PanelVisible = false, false
	if hideActivityBar {
		s.ActivityBarVisible = false
	}
	if hideStatusBar {
		s.StatusBarVisible = false
	}
	l.setByZen(s)
}

// setByZen sets a state that zen mode made, which the user did not (so no part is marked as changed).
func (l *Layout) setByZen(s LayoutState) {
	l.inZen = true
	defer func() { l.inZen = false }()
	l.set(s)
}

// UpdateZen makes the activity bar and the status bar follow zenMode.hideActivityBar and zenMode.hideStatusBar while
// zen mode is on: a part that is to be hidden is hidden, and one that zen mode hid and is not to be any more comes
// back (a part the user changed is left as it is).
func (l *Layout) UpdateZen(hideActivityBar, hideStatusBar bool) {
	if !l.state.Zen.Active {
		return
	}
	s := l.state
	if !s.Zen.ChangedActivityBar {
		switch {
		case hideActivityBar && s.ActivityBarVisible:
			s.ActivityBarVisible, s.Zen.HidActivityBar = false, true
		case !hideActivityBar && s.Zen.HidActivityBar:
			s.ActivityBarVisible, s.Zen.HidActivityBar = true, false
		}
	}
	if !s.Zen.ChangedStatusBar {
		switch {
		case hideStatusBar && s.StatusBarVisible:
			s.StatusBarVisible, s.Zen.HidStatusBar = false, true
		case !hideStatusBar && s.Zen.HidStatusBar:
			s.StatusBarVisible, s.Zen.HidStatusBar = true, false
		}
	}
	l.setByZen(s)
}

// SetZenTransitions records what the caller did when it turned zen mode on: went to full screen, turned the centered
// layout on, turned do not disturb on. Ending zen mode asks for them to be undone (ZenState).
func (l *Layout) SetZenTransitions(fullScreen, centered, doNotDisturb bool) {
	s := l.state
	if !s.Zen.Active {
		return
	}
	s.Zen.FullScreen, s.Zen.Centered, s.Zen.DoNotDisturb = fullScreen, centered, doNotDisturb
	l.setByZen(s)
}

// ExitZen turns zen mode off and shows again what zen mode hid and the user has not changed since. It returns what
// zen mode had recorded (ZenState), for the caller to undo what is not the layout's. It does nothing, and returns
// the zero state, when zen mode is off.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (toggleZenMode)
func (l *Layout) ExitZen() ZenState {
	z := l.state.Zen
	if !z.Active {
		return ZenState{}
	}
	s := l.state
	if z.HidSideBar && !z.ChangedSideBar {
		s.SideBarVisible = true
	}
	if z.HidPanel && !z.ChangedPanel {
		s = showPanel(s, l.opens)
	}
	if z.HidActivityBar && !z.ChangedActivityBar {
		s.ActivityBarVisible = true
	}
	if z.HidStatusBar && !z.ChangedStatusBar {
		s.StatusBarVisible = true
	}
	s.Zen = ZenState{}
	l.set(s)
	return z
}

// showPanel is the state with the panel shown, with the maximized state SetPanelVisible gives it.
func showPanel(s LayoutState, opens PanelOpensMaximized) LayoutState {
	if !s.PanelVisible {
		switch opens {
		case OpensMaximizedAlways:
			s.PanelMaximized = true
		case OpensMaximizedNever:
			s.PanelMaximized = false
		}
	}
	s.PanelVisible = true
	return s
}

// CenteredLayout says whether the centered layout was asked for.
func (l *Layout) CenteredLayout() bool { return l.state.CenteredLayout }

// SetCenteredLayout asks for the editor area to be centered between two margins, or not. Whether the margins show
// depends on the groups too (VS Code's workbench.editor.centeredLayoutAutoResize, which the shell applies).
func (l *Layout) SetCenteredLayout(v bool) {
	s := l.state
	s.CenteredLayout = v
	l.set(s)
}

// slot is a pane of a split view of the workbench: the part in it and how the split view treats it.
type slot struct {
	part Part
	item kit.SplitItem
}

// mainSlots are the panes of the row below the menu bar: the activity bar, the side bar and the
// center (the editor area with the panel), in the order the side bar position gives. The center
// takes what the window resizes by, the side bar gives it up after it.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (createGridDescriptor)
func (l *Layout) mainSlots() []slot {
	s := l.state
	slots := []slot{
		{PartActivityBar, kit.SplitItem{Size: ActivityBarWidth, MinSize: ActivityBarWidth, MaxSize: ActivityBarWidth, Hidden: !s.ActivityBarVisible}},
		{PartSideBar, kit.SplitItem{Size: s.SideBarWidth, MinSize: SideBarMinWidth, Priority: kit.LayoutLow, Hidden: !s.SideBarVisible, PreferredSize: SideBarDefaultWidth}},
		{partCenter, kit.SplitItem{MinSize: EditorMinWidth, Priority: kit.LayoutHigh}},
	}
	if s.SideBarPosition == SideBarRight {
		slices.Reverse(slots)
	}
	return slots
}

// centerSlots are the panes of the center and the way they lie: the editor area and the panel in
// the order the panel position gives. While the panel is maximized the editor area is hidden.
func (l *Layout) centerSlots() (orientation kit.SplitOrientation, slots []slot) {
	s := l.state
	editor := slot{PartEditor, kit.SplitItem{Priority: kit.LayoutHigh, Hidden: l.PanelMaximized()}}
	panel := slot{PartPanel, kit.SplitItem{Priority: kit.LayoutLow, Hidden: !s.PanelVisible}}
	if s.PanelPosition.Horizontal() {
		editor.item.MinSize = EditorMinHeight
		panel.item.Size, panel.item.MinSize = s.PanelHeight, PanelMinHeight
		orientation = kit.SplitVertical
	} else {
		editor.item.MinSize = EditorMinWidth
		panel.item.Size, panel.item.MinSize = s.PanelWidth, PanelMinWidth
		orientation = kit.SplitHorizontal
	}
	panel.item.PreferredSize = PanelDefaultSize
	if s.PanelPosition == PanelBottom || s.PanelPosition == PanelRight {
		return orientation, []slot{editor, panel}
	}
	return orientation, []slot{panel, editor}
}

// indexOf is the index of a part among the slots, or -1.
func indexOf(slots []slot, p Part) int {
	return slices.IndexFunc(slots, func(s slot) bool { return s.part == p })
}

// fitSlots gives the pane that takes what the others leave (the center of the main row, the editor
// area of the center) the size that makes the panes add up to the size of the split view, which is
// what a split view starts from. The panes that are shown keep their sizes.
func fitSlots(slots []slot, total float64, rest Part) []slot {
	out := slices.Clone(slots)
	var others float64
	for _, sl := range out {
		if sl.part != rest && !sl.item.Hidden {
			others += sl.item.Size
		}
	}
	if i := indexOf(out, rest); i >= 0 {
		out[i].item.Size = max(total-others, 0)
	}
	return out
}
