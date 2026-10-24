package app

import (
	"math"

	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The centered layout (SPEC §10.9): the editor area has a margin on each side, in the color of the editor, so that the
// text is in the middle of a wide window. It is asked for by the command (Layout.CenteredLayout) and shows while the grid has
// one column of groups, or always with workbench.editor.centeredLayoutAutoResize off.
//
// Ported from VS Code: src/vs/base/browser/ui/centered/centeredViewLayout.ts and
// src/vs/workbench/browser/layout.ts (centerMainEditorLayout)

// The metrics of the centered layout (centeredViewLayout.ts).
const (
	// centeredMarginRatio is the share of the width of each margin (leftMarginRatio and rightMarginRatio of VS Code's
	// default state).
	centeredMarginRatio = 0.1909
	// centeredFixedWidth is the width of the editor with workbench.editor.centeredLayoutFixedWidth.
	centeredFixedWidth = 900.0
	// centeredMarginMin is the least width of a margin.
	centeredMarginMin = 60.0
)

// centeredMargins are the widths of the left and the right margin of an editor area of a width: each is 19.09% of the width
// (at least 60 px), or with the fixed width the room that the editor of 900 px leaves, shared between the two. The margins
// never leave the editor less than the least width of a group.
//
// Ported from VS Code: src/vs/base/browser/ui/centered/centeredViewLayout.ts (resizeSplitViews, and the minimum sizes
// of the margins)
func centeredMargins(width float64, fixedWidth bool) (left, right float64) {
	if width <= 0 || math.IsNaN(width) {
		return 0, 0
	}
	if fixedWidth {
		margin := (width - min(width, centeredFixedWidth)) / 2
		left, right = math.Floor(margin), math.Ceil(margin)
	} else {
		left = centeredMarginRatio * width
		right = left
	}
	left, right = max(left, centeredMarginMin), max(right, centeredMarginMin)
	if width-left-right < groupMinWidth {
		left = max((width-groupMinWidth)/2, 0)
		right = left
	}
	return left, right
}

// centeredActive says whether the margins show: the centered layout was asked for, and (with the auto resize on) the grid has
// no more than one column of groups, unless a group is maximized.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (centerMainEditorLayout)
func centeredActive(requested, autoResize bool, layout editorgroup.Layout, maximized bool) bool {
	if !requested {
		return false
	}
	var moreThanOneColumn bool
	if layout.Orientation == editorgroup.Horizontal {
		moreThanOneColumn = len(layout.Groups) > 1
	} else {
		for _, g := range layout.Groups {
			if len(g.Groups) > 1 {
				moreThanOneColumn = true
			}
		}
	}
	return !autoResize || !moreThanOneColumn || maximized
}

// centeredArea is the editor area between its margins. The structure is the same whether the margins show or not, so the
// editors keep their state when they come and go.
type centeredArea struct {
	widgets.StatefulWidget
	win   *Window
	grid  *editorgroup.Grid
	child widgets.Widget
}

func newCenteredArea(win *Window, grid *editorgroup.Grid, child widgets.Widget) *centeredArea {
	return &centeredArea{StatefulWidget: widgets.NewStatefulWidgetBase(), win: win, grid: grid, child: child}
}

// CreateState implements widgets.StatefulWidget.
func (a *centeredArea) CreateState() widgets.State { return &centeredAreaState{} }

type centeredAreaState struct {
	widgets.StateOf[*centeredArea]
	stops []func()
}

// InitState follows what decides whether the margins show.
func (s *centeredAreaState) InitState() {
	a := s.Widget()
	binding := widgets.WidgetsBindingOf(s.Context())
	rebuild := func() {
		binding.Post(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	}
	s.stops = []func(){
		a.win.Layout.OnChange(rebuild),
		a.grid.OnChange(func(editorgroup.GridEvent) { rebuild() }),
		a.win.Settings.OnChange(func(e settings.ChangeEvent) {
			if e.Affects("workbench.editor.centeredLayout", "") {
				rebuild()
			}
		}),
	}
}

// Dispose stops following.
func (s *centeredAreaState) Dispose() {
	for _, stop := range s.stops {
		stop()
	}
}

// DidUpdateWidget keeps following the grid of the window.
func (s *centeredAreaState) DidUpdateWidget(*centeredArea) {}

// Active says whether the margins show now.
func (s *centeredAreaState) active() bool {
	a := s.Widget()
	return centeredActive(a.win.Layout.CenteredLayout(),
		settings.Get[bool](a.win.Settings, "workbench.editor.centeredLayoutAutoResize", ""), a.grid.Layout(), a.grid.Maximized() != nil)
}

// Build implements widgets.State.
func (s *centeredAreaState) Build(bc widgets.BuildContext) widgets.Widget {
	a := s.Widget()
	active := s.active()
	fixed := settings.Get[bool](a.win.Settings, "workbench.editor.centeredLayoutFixedWidth", "")
	background := kit.ThemeOf(bc).EditorBackground
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		var left, right float64
		if active {
			left, right = centeredMargins(c.MaxWidth, fixed)
		}
		return widgets.NewColoredBox(background, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(left, 0, right, 0), a.child))
	})
}

// initModes makes the shell follow what the full screen mode and zen mode change in what it builds: the tabs of the
// groups (zen mode may show fewer) and the menu bar of the workbench.
func (s *shellState) initModes(binding *widgets.WidgetsBinding) {
	win := s.win
	zen := win.Layout.ZenActive()
	rebuild := func() {
		binding.Post(func() {
			if s.Mounted() {
				s.refreshKeys()
				s.SetState(func() {})
			}
		})
	}
	s.modesStops = append(s.modesStops,
		win.OnModesChange(rebuild),
		win.Layout.OnChange(func() {
			if now := win.Layout.ZenActive(); now != zen {
				zen = now
				rebuild()
			}
		}),
		win.OnZoomChange(func() { binding.Post(s.stateChangedIfMounted) }),
	)
}

// disposeModes stops following the modes.
func (s *shellState) disposeModes() {
	for _, stop := range s.modesStops {
		stop()
	}
	s.modesStops = nil
}
