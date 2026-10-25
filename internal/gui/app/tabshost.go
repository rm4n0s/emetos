package app

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// The widgets that show the window tabs in the one Glutter window (SPEC §10.14).
//
// tabsHost builds an IndexedStack of the tabs, in the order of the strip, with the active one shown. Every tab keeps its
// widget tree mounted (its documents, scroll positions, focus and the state of every view stay), but only the shown one
// is painted and hit tested (RenderIndexedStack) and only its tickers run (a TickerMode around each tab). IndexedStack
// also excludes the tabs that are not shown from the keyboard focus; the manager remembers what had the focus in the tab
// that was left and gives it back (Workspaces.Activate, restoreFocus). The tabs are keyed by a GlobalKey of their own, so
// that moving a tab, or closing one in front of it, keeps the state of the others.

// hostWidget is the root of the application: the tabs of the window.
func (m *Workspaces) hostWidget() widgets.Widget { return newTabsHost(m) }

// tabsHost shows the tabs of the manager.
type tabsHost struct {
	widgets.StatefulWidget
	m *Workspaces
}

func newTabsHost(m *Workspaces) *tabsHost {
	return &tabsHost{StatefulWidget: widgets.NewStatefulWidgetBase(), m: m}
}

// CreateState implements widgets.StatefulWidget.
func (h *tabsHost) CreateState() widgets.State { return &tabsHostState{} }

type tabsHostState struct {
	widgets.StateOf[*tabsHost]
}

// InitState connects the manager to the state.
func (s *tabsHostState) InitState() { s.Widget().m.host = s }

// DidChangeDependencies binds the manager to the application the first time it has the binding.
func (s *tabsHostState) DidChangeDependencies() {
	s.Widget().m.bind(widgets.WidgetsBindingOf(s.Context()))
}

// Dispose lets go of the manager.
func (s *tabsHostState) Dispose() {
	if m := s.Widget().m; m.host == s {
		m.host = nil
	}
}

// Build shows the active tab and keeps the others mounted.
func (s *tabsHostState) Build(widgets.BuildContext) widgets.Widget {
	m := s.Widget().m
	active := m.ActiveTab()
	var children []widgets.Widget
	index := 0
	for _, t := range m.Tabs() {
		if !t.hosted {
			continue
		}
		if t == active {
			index = len(children)
		}
		children = append(children, newTabSlot(m, t, t.ws, t == active))
	}
	if len(children) == 0 {
		return widgets.NewSizedBoxShrink(nil)
	}
	// the tabs fill the window; the direction is given here because no Directionality is above the host (each tab's
	// WidgetsApp has its own)
	return widgets.NewIndexedStack(children, widgets.IndexedStackOpts{
		Alignment: painting.AlignmentTopLeft, TextDirection: ui.TextDirectionLTR,
		Sizing: rendering.StackFitExpand, Index: foundation.Some(index),
	})
}

// refresh makes the host build its subtree again.
func (s *tabsHostState) refresh() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// tabSlot is the place of one tab in the host: the page of its workspace, with the tickers on only while the tab is the
// active one. When the tab gets another workspace (opening a folder in the tab) the subtree is made again, so that the
// shell of the old workspace is disposed and the new one made, as if the tab had opened.
type tabSlot struct {
	widgets.StatefulWidget
	m      *Workspaces
	t      *WindowTab
	ws     *Workspace
	active bool
}

func newTabSlot(m *Workspaces, t *WindowTab, ws *Workspace, active bool) *tabSlot {
	return &tabSlot{StatefulWidget: widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: t.key}), m: m, t: t, ws: ws, active: active}
}

// CreateState implements widgets.StatefulWidget.
func (w *tabSlot) CreateState() widgets.State { return &tabSlotState{} }

type tabSlotState struct {
	widgets.StateOf[*tabSlot]
}

// Build shows the page of the workspace of the tab.
func (s *tabSlotState) Build(widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	return widgets.NewTickerMode(w.active,
		widgets.NewKeyedSubtree(newAppWithPage(w.ws, w.t.page), widgets.WidgetOpts{Key: foundation.NewObjectKey(w.ws)}))
}
