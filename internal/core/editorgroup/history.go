package editorgroup

import (
	"slices"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/workbench/services/history/browser/historyService.ts
// (HistoryService: the navigation stacks, their scopes, the context keys, the recently used editors and the
// recently closed editors, EditorNavigationStacks)

// NavigationScope is workbench.editor.navigationScope: how far one history of locations reaches.
type NavigationScope string

// The values of workbench.editor.navigationScope.
const (
	// ScopeDefault is one history for the window.
	ScopeDefault NavigationScope = "default"
	// ScopeEditorGroup is one history for each group.
	ScopeEditorGroup NavigationScope = "editorGroup"
	// ScopeEditor is one history for each editor of a group.
	ScopeEditor NavigationScope = "editor"
)

// NavState tells what the history can do now; the window sets the context keys canNavigateBack,
// canNavigateForward, canNavigateBackInNavigationLocations, canNavigateForwardInNavigationLocations,
// canNavigateToLastNavigationLocation, canNavigateBackInEditLocations, canNavigateForwardInEditLocations,
// canNavigateToLastEditLocation and canReopenClosedEditor from it.
type NavState struct {
	CanBack, CanForward               bool
	CanBackInNavigation               bool
	CanForwardInNavigation            bool
	CanLastNavigation                 bool
	CanBackInEdits, CanForwardInEdits bool
	CanLastEdit                       bool
	CanReopenClosed                   bool
}

// stacks are the three stacks of a scope: every selection that counts, the edits, and the jumps and
// navigations (EditorNavigationStacks).
type stacks struct {
	selections, edits, navigations *navStack
}

func newStacks(opener Opener, changed func()) *stacks {
	return &stacks{
		selections:  newNavStack(GoAll, opener, changed),
		edits:       newNavStack(GoEdits, opener, changed),
		navigations: newNavStack(GoNavigation, opener, changed),
	}
}

func (s *stacks) all() []*navStack { return []*navStack{s.selections, s.edits, s.navigations} }

func (s *stacks) get(f GoFilter) *navStack {
	switch f {
	case GoEdits:
		return s.edits
	case GoNavigation:
		return s.navigations
	}
	return s.selections
}

// activeEditorChanged tells the stacks that another editor is shown: only the selections stack takes it.
func (s *stacks) activeEditorChanged(p *Pane) { s.selections.notify(p, ReasonNone) }

// selectionChanged tells the stacks that the selection of the active editor changed.
func (s *stacks) selectionChanged(p *Pane, reason SelectionReason) {
	previous := s.selections.currentEntry()
	var prev *NavLocation
	if previous != nil {
		cp := *previous
		prev = &cp
	}
	s.selections.notify(p, reason)
	switch {
	case reason == ReasonEdit:
		s.edits.notify(p, reason)
	// A change made while the selections stack navigates would put the same place in the stack again.
	case (reason == ReasonNavigation || reason == ReasonJump) && !s.selections.navigating:
		// A jump has a source and a target: the stack gets the place it left as well, unless the user
		// is navigating the stack of navigations.
		if reason == ReasonJump && !s.navigations.navigating && prev != nil {
			s.navigations.add(*prev)
		}
		s.navigations.notify(p, reason)
	}
}

// History is the history service of a window: the locations that Back, Forward and Go to Last Edit NavLocation
// go to, the editors that were used last, and the editors that were closed last (Reopen Closed Editor). It is
// used on the UI thread only, and it follows what the window tells it: ActiveEditorChanged,
// SelectionChanged, EditorClosed, GroupRemoved, the file operations.
type History struct {
	opener Opener
	scope  NavigationScope

	global   *stacks
	byGroup  map[int]*stacks
	byEditor map[int]map[EditorInput]*stacks

	lastActive struct {
		groupID int
		input   EditorInput
		set     bool
	}

	closed *ClosedEditors
	// Recent is the recently used editors across the groups, and RecentInGroup those of one group.
	Recent, RecentInGroup RecentStack

	listeners []*historyListener
}

type historyListener struct{ fn func() }

// NewHistory makes the history of a window; the opener shows the locations.
func NewHistory(opener Opener, scope NavigationScope) *History {
	h := &History{opener: opener, scope: normalizeScope(scope), closed: NewClosedEditors()}
	h.reset()
	return h
}

func normalizeScope(s NavigationScope) NavigationScope {
	switch s {
	case ScopeEditorGroup, ScopeEditor:
		return s
	}
	return ScopeDefault
}

// reset forgets the stacks.
func (h *History) reset() {
	h.global = nil
	h.byGroup = map[int]*stacks{}
	h.byEditor = map[int]map[EditorInput]*stacks{}
}

// Scope is the scope of the history.
func (h *History) Scope() NavigationScope { return h.scope }

// SetScope changes workbench.editor.navigationScope; the histories start empty.
func (h *History) SetScope(scope NavigationScope) {
	scope = normalizeScope(scope)
	if scope == h.scope {
		return
	}
	h.scope = scope
	h.reset()
	h.fire()
}

// OnChange calls fn when what the history can do may have changed.
func (h *History) OnChange(fn func()) (unsubscribe func()) {
	l := &historyListener{fn: fn}
	h.listeners = append(h.listeners, l)
	return func() { h.listeners = slices.DeleteFunc(h.listeners, func(x *historyListener) bool { return x == l }) }
}

func (h *History) fire() {
	for _, l := range slices.Clone(h.listeners) {
		l.fn()
	}
}

// Closed are the editors that Reopen Closed Editor brings back.
func (h *History) Closed() *ClosedEditors { return h.closed }

// stacksFor are the stacks of an editor of a group, by the scope. It is nil for the scope of the editor and
// no editor.
func (h *History) stacksFor(groupID int, input EditorInput, create bool) *stacks {
	switch h.scope {
	case ScopeEditor:
		if input == nil {
			return nil
		}
		m := h.byEditor[groupID]
		if m == nil {
			if !create {
				return nil
			}
			m = map[EditorInput]*stacks{}
			h.byEditor[groupID] = m
		}
		st := m[input]
		if st == nil && create {
			st = newStacks(h.opener, h.fire)
			m[input] = st
		}
		return st
	case ScopeEditorGroup:
		st := h.byGroup[groupID]
		if st == nil && create {
			st = newStacks(h.opener, h.fire)
			h.byGroup[groupID] = st
		}
		return st
	}
	if h.global == nil && create {
		h.global = newStacks(h.opener, h.fire)
	}
	return h.global
}

// active are the stacks of the active editor of the active group.
func (h *History) active(create bool) *stacks {
	p := h.opener.ActivePane()
	return h.stacksFor(p.GroupID, p.Input, create)
}

// each runs fn for every stack that exists.
func (h *History) each(fn func(*stacks)) {
	if h.global != nil {
		fn(h.global)
	}
	for _, st := range h.byGroup {
		fn(st)
	}
	for _, m := range h.byEditor {
		for _, st := range m {
			fn(st)
		}
	}
}

// ActiveEditorChanged tells the history that another editor is shown in the active group (or none, with a
// nil input). It does nothing when the editor is the same one as the last time.
func (h *History) ActiveEditorChanged(p Pane) {
	l := &h.lastActive
	if l.set && l.groupID == p.GroupID && sameInput(l.input, p.Input) {
		return
	}
	l.groupID, l.input, l.set = p.GroupID, p.Input, true
	if st := h.stacksFor(p.GroupID, p.Input, true); st != nil {
		st.activeEditorChanged(&p)
	}
	h.fire()
}

func sameInput(a, b EditorInput) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b || a.Matches(b)
}

// SelectionChanged tells the history that the selection of the active editor changed.
func (h *History) SelectionChanged(p Pane, reason SelectionReason) {
	if st := h.stacksFor(p.GroupID, p.Input, true); st != nil {
		st.selectionChanged(&p, reason)
	}
	h.fire()
}

// EditorClosed tells the history that an editor closed in a group: with the scope of the editor its history
// goes away.
func (h *History) EditorClosed(groupID int, input EditorInput) {
	m := h.byEditor[groupID]
	if m == nil {
		return
	}
	delete(m, input)
	if len(m) == 0 {
		delete(h.byEditor, groupID)
	}
	h.fire()
}

// GroupRemoved tells the history that a group is gone: its locations are dropped.
func (h *History) GroupRemoved(groupID int) {
	if h.global != nil {
		for _, n := range h.global.all() {
			n.remove(func(l NavLocation) bool { return l.GroupID == groupID })
		}
	}
	delete(h.byGroup, groupID)
	delete(h.byEditor, groupID)
	if h.lastActive.set && h.lastActive.groupID == groupID {
		h.lastActive.set = false
	}
	h.fire()
}

// InputDisposed drops the locations of an editor that has no file and is disposed.
func (h *History) InputDisposed(input EditorInput) {
	if _, ok := input.Resource(); ok {
		return
	}
	h.each(func(st *stacks) {
		for _, n := range st.all() {
			n.remove(func(l NavLocation) bool { return l.matchesInput(input) })
		}
	})
}

// RemoveResource drops what the history knows of a file: its locations, and its closed editors (the file
// was deleted).
func (h *History) RemoveResource(uri document.URI) {
	h.each(func(st *stacks) {
		for _, n := range st.all() {
			n.remove(func(l NavLocation) bool { return l.HasResource && l.Resource == uri })
		}
	})
	h.closed.RemoveResource(uri)
	h.fire()
}

// Follow gives the locations and the closed editors of the files the path that follow says they moved to:
// follow returns the new URI of a URI (the same URI when it did not move) and false when the file is gone.
// A file that is gone is removed.
func (h *History) Follow(follow func(document.URI) (nu document.URI, exists bool)) {
	var gone []document.URI
	seen := map[document.URI]bool{}
	note := func(u document.URI) {
		if seen[u] {
			return
		}
		seen[u] = true
		if _, ok := follow(u); !ok {
			gone = append(gone, u)
		}
	}
	h.each(func(st *stacks) {
		for _, n := range st.all() {
			for _, l := range n.stack {
				if l.HasResource {
					note(l.Resource)
				}
			}
		}
	})
	for _, c := range h.closed.list {
		note(c.Resource)
	}
	for _, u := range gone {
		h.RemoveResource(u)
	}
	moved := func(u document.URI) (document.URI, bool) {
		nu, ok := follow(u)
		return nu, ok
	}
	h.each(func(st *stacks) {
		for _, n := range st.all() {
			n.move(moved)
		}
	})
	h.closed.move(moved)
	h.fire()
}

// Clear forgets the locations and the closed editors (Clear Editor History).
func (h *History) Clear() {
	h.each(func(st *stacks) {
		for _, n := range st.all() {
			n.clear()
		}
	})
	h.closed.Clear()
	h.fire()
}

// stack is the stack of a filter of the active editor, nil when there is none.
func (h *History) stack(f GoFilter) *navStack {
	if st := h.active(true); st != nil {
		return st.get(f)
	}
	return nil
}

// Back goes to the location before the place, in the stack of a filter.
func (h *History) Back(f GoFilter) {
	if n := h.stack(f); n != nil {
		n.goBack()
	}
}

// Forward goes to the location after the place.
func (h *History) Forward(f GoFilter) {
	if n := h.stack(f); n != nil {
		n.goForward()
	}
}

// Previous goes to the location the place was at before it moved (VS Code's goPrevious).
func (h *History) Previous(f GoFilter) {
	if n := h.stack(f); n != nil {
		n.goPrevious()
	}
}

// Last goes to the last location of the stack: Go to Last Edit NavLocation with GoEdits.
func (h *History) Last(f GoFilter) {
	if n := h.stack(f); n != nil {
		n.goLast()
	}
}

// State tells what the history can do for the active editor.
func (h *History) State() NavState {
	s := NavState{CanReopenClosed: h.closed.Len() > 0}
	st := h.active(false)
	if st == nil {
		return s
	}
	s.CanBack, s.CanForward = st.selections.canGoBack(), st.selections.canGoForward()
	s.CanBackInNavigation, s.CanForwardInNavigation, s.CanLastNavigation =
		st.navigations.canGoBack(), st.navigations.canGoForward(), st.navigations.canGoLast()
	s.CanBackInEdits, s.CanForwardInEdits, s.CanLastEdit =
		st.edits.canGoBack(), st.edits.canGoForward(), st.edits.canGoLast()
	return s
}

// Locations are the locations of the selections stack of the active editor and the place in it; tests and
// the window's logs read them.
func (h *History) Locations(f GoFilter) (locs []NavLocation, index int) {
	st := h.active(false)
	if st == nil {
		return nil, -1
	}
	n := st.get(f)
	return slices.Clone(n.stack), n.index
}
