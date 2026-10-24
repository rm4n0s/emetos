package command

import (
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// ViewLocation is the part of the workbench a view container is shown in.
type ViewLocation string

// The locations of view containers.
const (
	// ViewLocationSideBar is the primary side bar, switched by the activity bar.
	ViewLocationSideBar ViewLocation = "sidebar"
	// ViewLocationPanel is the panel.
	ViewLocationPanel ViewLocation = "panel"
)

// ViewContainer holds views and is shown as a whole: one icon of the activity bar and the title of
// the side bar, or a tab of the panel.
//
// Ported from VS Code: src/vs/workbench/common/views.ts (ViewContainer, IViewContainersRegistry)
type ViewContainer struct {
	// ID identifies the container ("workbench.view.explorer").
	ID string
	// Title is the name of the container: the title of the side bar, and the tooltip of its icon.
	Title string
	// Icon is the code point of the Codicon of the container.
	Icon int
	// Order orders the containers in the activity bar, the lowest first; containers of an equal
	// order keep the order they were registered in.
	Order int
	// Location is where the container is shown; the side bar when empty.
	Location ViewLocation
}

// View is a section of a view container: a header with a title that collapses the body below it.
//
// Ported from VS Code: src/vs/workbench/common/views.ts (IViewDescriptor, IViewsRegistry)
type View struct {
	// ID identifies the view ("workbench.explorer.fileView").
	ID string
	// Name is the title of the view, shown in its header.
	Name string
	// Container is the id of the container the view is in.
	Container string
	// When is the condition under which the view is shown; nil is always.
	When contextkey.Expr
	// Order orders the views of a container, the lowest first; views of an equal order keep the
	// order they were registered in.
	Order int
	// Collapsed tells that the view starts with its body folded away.
	Collapsed bool
	// HideByDefault tells that the view is not shown until the user (or a command) shows it, such as
	// Open Editors in the Explorer.
	HideByDefault bool
}

// ViewRegistry holds the view containers and the views in them (SPEC §10.4). It is safe for
// concurrent use. Make one with NewViewRegistry; there is no global instance.
//
// Ported from VS Code: src/vs/workbench/common/views.ts (ViewContainersRegistry, ViewsRegistry)
type ViewRegistry struct {
	mu         sync.RWMutex
	containers []ViewContainer
	views      []View
}

// NewViewRegistry makes an empty registry.
func NewViewRegistry() *ViewRegistry { return &ViewRegistry{} }

// RegisterContainer adds a view container. It needs an id and a title, and its id must be new.
func (r *ViewRegistry) RegisterContainer(c ViewContainer) error {
	if c.ID == "" || c.Title == "" {
		return errors.New("ViewContainerInvalid", "a view container needs an id and a title", "id", c.ID)
	}
	if c.Location == "" {
		c.Location = ViewLocationSideBar
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if slices.ContainsFunc(r.containers, func(o ViewContainer) bool { return o.ID == c.ID }) {
		return errors.New("ViewContainerDuplicate", "the view container is already registered", "id", c.ID)
	}
	r.containers = append(r.containers, c)
	return nil
}

// RegisterViews adds views to their containers, which have to be registered. A view needs an id and a
// name, and its id must be new.
func (r *ViewRegistry) RegisterViews(views ...View) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range views {
		if v.ID == "" || v.Name == "" {
			return errors.New("ViewInvalid", "a view needs an id and a name", "id", v.ID)
		}
		if !slices.ContainsFunc(r.containers, func(c ViewContainer) bool { return c.ID == v.Container }) {
			return errors.New("ViewContainerUnknown", "the container of the view is not registered", "view", v.ID, "container", v.Container)
		}
		if slices.ContainsFunc(r.views, func(o View) bool { return o.ID == v.ID }) {
			return errors.New("ViewDuplicate", "the view is already registered", "id", v.ID)
		}
		r.views = append(r.views, v)
	}
	return nil
}

// MustRegisterContainer is RegisterContainer for the table of built-in containers; it panics on a
// failure.
func (r *ViewRegistry) MustRegisterContainer(c ViewContainer) {
	if err := r.RegisterContainer(c); err != nil {
		panic(err)
	}
}

// MustRegisterViews is RegisterViews for the table of built-in views; it panics on a failure.
func (r *ViewRegistry) MustRegisterViews(views ...View) {
	if err := r.RegisterViews(views...); err != nil {
		panic(err)
	}
}

// Containers lists the containers of a location in the order of the activity bar.
func (r *ViewRegistry) Containers(loc ViewLocation) []ViewContainer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ViewContainer
	for _, c := range r.containers {
		if c.Location == loc {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b ViewContainer) int { return a.Order - b.Order })
	return out
}

// Container returns a container by id.
func (r *ViewRegistry) Container(id string) (ViewContainer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.containers {
		if c.ID == id {
			return c, true
		}
	}
	return ViewContainer{}, false
}

// Views lists the views of a container whose When holds in the context (all of them for a nil
// context), in order.
func (r *ViewRegistry) Views(container string, ctx contextkey.Context) []View {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []View
	for _, v := range r.views {
		if v.Container == container && (v.When == nil || ctx == nil || v.When.Evaluate(ctx)) {
			out = append(out, v)
		}
	}
	slices.SortStableFunc(out, func(a, b View) int { return a.Order - b.Order })
	return out
}

// View returns a view by id.
func (r *ViewRegistry) View(id string) (View, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.views {
		if v.ID == id {
			return v, true
		}
	}
	return View{}, false
}
