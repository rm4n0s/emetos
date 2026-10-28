package workbench

import (
	"slices"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// Views is the state of the views of a window (SPEC §10.4): which view container the side bar shows
// and which views are collapsed. It is a model, with no widget in it, used on the UI thread only: the
// activity bar and the side bar follow it with OnChange.
//
// Ported from VS Code: src/vs/workbench/browser/parts/paneCompositePart.ts and
// src/vs/workbench/browser/parts/views/viewPaneContainer.ts
type Views struct {
	registry *command.ViewRegistry
	layout   *Layout
	active   string
	// activePanel is the container the panel shows.
	activePanel string
	// collapsed has the views the user collapsed or expanded; the others start as their View says.
	collapsed map[string]bool
	// hidden has the views the user (or a command) hid or showed; the others start as their View says.
	hidden    map[string]bool
	listeners []*func()
}

// NewViews makes the views of a window over a registry. The side bar shows the first container of the
// activity bar. layout is the layout of the window, whose side bar the activity bar shows and hides;
// it may be nil.
func NewViews(registry *command.ViewRegistry, layout *Layout) *Views {
	v := &Views{registry: registry, layout: layout, collapsed: map[string]bool{}, hidden: map[string]bool{}}
	if cs := registry.Containers(command.ViewLocationSideBar); len(cs) > 0 {
		v.active = cs[0].ID
	}
	if cs := registry.Containers(command.ViewLocationPanel); len(cs) > 0 {
		v.activePanel = cs[0].ID
	}
	// containers and views come and go with the extensions of the window
	registry.OnChange(v.registryChanged)
	return v
}

// registryChanged follows a change of the containers: the container that was shown and is gone is replaced by the first
// one of its place, and everybody who follows the views is told.
func (v *Views) registryChanged() {
	if _, ok := v.registry.Container(v.active); !ok {
		v.active = ""
		if cs := v.registry.Containers(command.ViewLocationSideBar); len(cs) > 0 {
			v.active = cs[0].ID
		}
	}
	if _, ok := v.registry.Container(v.activePanel); !ok {
		v.activePanel = ""
		if cs := v.registry.Containers(command.ViewLocationPanel); len(cs) > 0 {
			v.activePanel = cs[0].ID
		}
	}
	v.notify()
}

// Changed tells the listeners of OnChange that something the views show changed without the registry changing: a badge,
// a context key that a `when` reads, the title actions of a view.
func (v *Views) Changed() { v.notify() }

// Registry is the registry the views come from.
func (v *Views) Registry() *command.ViewRegistry { return v.registry }

// Active is the id of the view container the side bar shows, or "" when there is none.
func (v *Views) Active() string { return v.active }

// ActiveContainer is the container the side bar shows.
func (v *Views) ActiveContainer() (command.ViewContainer, bool) {
	return v.registry.Container(v.active)
}

// Open shows a view container in the side bar and shows the side bar, as the command of a container
// does. An id that is not a container of the side bar does nothing.
func (v *Views) Open(id string) {
	c, ok := v.registry.Container(id)
	if !ok || c.Location != command.ViewLocationSideBar {
		return
	}
	changed := v.active != id
	v.active = id
	if v.layout != nil {
		v.layout.SetSideBarVisible(true)
	}
	if changed {
		v.notify()
	}
}

// ActivePanel is the id of the view container the panel shows, or "" when the panel has none.
func (v *Views) ActivePanel() string { return v.activePanel }

// OpenPanel shows a view container in the panel and shows the panel, as the command of a view of the
// panel does. An id that is not a container of the panel does nothing.
func (v *Views) OpenPanel(id string) {
	c, ok := v.registry.Container(id)
	if !ok || c.Location != command.ViewLocationPanel {
		return
	}
	changed := v.activePanel != id
	v.activePanel = id
	if v.layout != nil {
		v.layout.SetPanelVisible(true)
	}
	if changed {
		v.notify()
	}
}

// TogglePanel is the command of a view of the panel: the panel is hidden when it shows the tab of the container, and shows
// the tab otherwise. It reports whether the tab is shown afterwards.
func (v *Views) TogglePanel(id string) (shown bool) {
	if v.layout != nil && v.layout.State().PanelVisible && v.activePanel == id {
		v.layout.SetPanelVisible(false)
		return false
	}
	v.OpenPanel(id)
	return v.activePanel == id
}

// ClosePanel hides the panel when it shows the tab of the container; it does nothing otherwise. It reports whether the panel was
// hidden.
func (v *Views) ClosePanel(id string) (hidden bool) {
	if v.layout != nil && v.layout.State().PanelVisible && v.activePanel == id {
		v.layout.SetPanelVisible(false)
		return true
	}
	return false
}

// Toggle is a click on an icon of the activity bar: the container of the icon opens, and the one that
// is open closes the side bar.
func (v *Views) Toggle(id string) {
	if v.layout != nil && v.layout.State().SideBarVisible && v.active == id {
		v.layout.SetSideBarVisible(false)
		return
	}
	v.Open(id)
}

// IsCollapsed tells whether a view shows only its header.
func (v *Views) IsCollapsed(viewID string) bool {
	if c, ok := v.collapsed[viewID]; ok {
		return c
	}
	view, ok := v.registry.View(viewID)
	return ok && view.Collapsed
}

// SetCollapsed folds a view away or unfolds it.
func (v *Views) SetCollapsed(viewID string, collapsed bool) {
	if v.IsCollapsed(viewID) == collapsed {
		return
	}
	v.collapsed[viewID] = collapsed
	v.notify()
}

// IsHidden tells whether a view is not shown in its container: it starts hidden when its View says
// HideByDefault (Open Editors) and shows when SetHidden says so.
func (v *Views) IsHidden(viewID string) bool {
	if h, ok := v.hidden[viewID]; ok {
		return h
	}
	view, ok := v.registry.View(viewID)
	return ok && view.HideByDefault
}

// SetHidden hides a view or shows it again, and unfolds a view that is shown.
func (v *Views) SetHidden(viewID string, hidden bool) {
	if v.IsHidden(viewID) == hidden {
		return
	}
	v.hidden[viewID] = hidden
	if !hidden {
		v.collapsed[viewID] = false
	}
	v.notify()
}

// OnChange calls fn when the active container or a view's state changes, and returns the function that
// stops it.
func (v *Views) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	v.listeners = append(v.listeners, p)
	return func() {
		if i := slices.Index(v.listeners, p); i >= 0 {
			v.listeners = slices.Delete(v.listeners, i, i+1)
		}
	}
}

func (v *Views) notify() {
	for _, f := range slices.Clone(v.listeners) {
		(*f)()
	}
}
