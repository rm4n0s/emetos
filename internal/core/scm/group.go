package scm

import (
	"slices"
	"sync"
)

// Group is a group of resources of a source control (VS Code's ISCMResourceGroup): "Staged Changes", "Changes".
type Group struct {
	src          *Source
	id           string
	label        string
	hide         bool
	contextValue string
	removed      bool

	// guarded by the lock of the model
	resources []Resource
	version   uint64

	// sortMu guards the caches of the sorts and the tree, which are made outside the lock of the model.
	sortMu   sync.Mutex
	sorted   map[SortKey]sortedCache
	treeAt   uint64
	treeHave bool
	tree     *Tree
}

type sortedCache struct {
	version uint64
	list    []*Resource
}

// ID is the id of the group ("index"), the value of the context key scmResourceGroup.
func (g *Group) ID() string { return g.id }

// Source is the source control the group belongs to.
func (g *Group) Source() *Source { return g.src }

// Label is the title of the group.
func (g *Group) Label() string {
	g.src.m.mu.Lock()
	defer g.src.m.mu.Unlock()
	return g.label
}

// HideWhenEmpty says whether the group is hidden while it has no resources.
func (g *Group) HideWhenEmpty() bool {
	g.src.m.mu.Lock()
	defer g.src.m.mu.Unlock()
	return g.hide
}

// ContextValue is the value of the context key scmResourceGroupState.
func (g *Group) ContextValue() string {
	g.src.m.mu.Lock()
	defer g.src.m.mu.Unlock()
	return g.contextValue
}

// Removed tells whether the group was removed.
func (g *Group) Removed() bool {
	g.src.m.mu.Lock()
	defer g.src.m.mu.Unlock()
	return g.removed
}

// Visible tells whether the view shows the group: it has resources or is not hidden when empty.
func (g *Group) Visible() bool {
	g.src.m.mu.Lock()
	defer g.src.m.mu.Unlock()
	return !g.removed && (len(g.resources) > 0 || !g.hide)
}

// Len is the number of resources.
func (g *Group) Len() int {
	g.src.m.mu.Lock()
	defer g.src.m.mu.Unlock()
	return len(g.resources)
}

// Version counts the replacements of the resources; it tells a view whether what it drew is out of date.
func (g *Group) Version() uint64 {
	g.src.m.mu.Lock()
	defer g.src.m.mu.Unlock()
	return g.version
}

// Resources are the resources in the order the provider gave them. The slice is never changed after it is returned: a new
// SetResources makes another, so it can be read from any goroutine; do not modify it.
func (g *Group) Resources() []Resource {
	g.src.m.mu.Lock()
	defer g.src.m.mu.Unlock()
	return g.resources
}

// SetResources replaces the resources; the group fills in the path of each and the back pointer to itself. A removed group keeps
// none.
func (g *Group) SetResources(resources []Resource) {
	list := make([]Resource, len(resources))
	for i := range resources {
		r := resources[i]
		r.Group = g
		r.prepare()
		list[i] = r
	}
	m := g.src.m
	m.mu.Lock()
	if g.removed || g.src.removed {
		m.mu.Unlock()
		return
	}
	g.resources = list
	g.version++
	m.mu.Unlock()
	m.notify(Event{Kind: ResourcesChanged, Source: g.src, Group: g})
}

// SetLabel changes the title.
func (g *Group) SetLabel(label string) {
	g.update(func() bool { c := g.label != label; g.label = label; return c })
}

// SetHideWhenEmpty changes whether the group is hidden while empty.
func (g *Group) SetHideWhenEmpty(hide bool) {
	g.update(func() bool { c := g.hide != hide; g.hide = hide; return c })
}

// SetContextValue changes the value of the context key scmResourceGroupState.
func (g *Group) SetContextValue(v string) {
	g.update(func() bool { c := g.contextValue != v; g.contextValue = v; return c })
}

func (g *Group) update(fn func() bool) {
	m := g.src.m
	m.mu.Lock()
	if g.removed || !fn() {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.notify(Event{Kind: GroupsChanged, Source: g.src, Group: g})
}

// Remove takes the group out of its source control; it does nothing for one that was removed.
func (g *Group) Remove() {
	m := g.src.m
	m.mu.Lock()
	if g.removed {
		m.mu.Unlock()
		return
	}
	g.removed = true
	g.src.groups = slices.DeleteFunc(g.src.groups, func(o *Group) bool { return o == g })
	m.mu.Unlock()
	m.notify(Event{Kind: GroupsChanged, Source: g.src, Group: g})
}

// Sorted are the resources in the order of the list view for the sort key. The order is made once for each version of the
// resources (a sort of 100,000 resources by path takes a fraction of a second) and kept.
func (g *Group) Sorted(key SortKey) []*Resource {
	g.src.m.mu.Lock()
	resources, version := g.resources, g.version
	g.src.m.mu.Unlock()
	g.sortMu.Lock()
	defer g.sortMu.Unlock()
	if c, ok := g.sorted[key]; ok && c.version == version {
		return c.list
	}
	list := sortResources(resources, key)
	if g.sorted == nil {
		g.sorted = map[SortKey]sortedCache{}
	}
	g.sorted[key] = sortedCache{version: version, list: list}
	return list
}

// Tree is the tree of the resources for the tree view mode, made once for each version of the resources. The tree belongs to
// the goroutine that uses it (the UI thread); see Tree.
func (g *Group) Tree() *Tree {
	g.src.m.mu.Lock()
	resources, version := g.resources, g.version
	g.src.m.mu.Unlock()
	g.sortMu.Lock()
	defer g.sortMu.Unlock()
	if g.treeHave && g.treeAt == version {
		return g.tree
	}
	g.tree, g.treeAt, g.treeHave = NewTree(resources, g.src.rootPath), version, true
	return g.tree
}
