package editorgroup

import (
	"slices"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/workbench/services/history/browser/historyService.ts
// (the recently closed editors: handleEditorCloseEventInReopen, takeLastClosedEditorsBatch,
// removeFromRecentlyClosedEditors; the recently used editors: ensureRecentlyUsedStack)

// MaxRecentlyClosedEditors is the number of closed editors that Reopen Closed Editor remembers (VS Code's
// MAX_RECENTLY_CLOSED_EDITORS).
const MaxRecentlyClosedEditors = 20

// ClosedEditor is an editor that was closed and can be opened again.
type ClosedEditor struct {
	// Resource is the file of the editor.
	Resource document.URI
	// GroupID is the group it was closed in and Index the place it had there.
	GroupID, Index int
	// Sticky tells that it was pinned (a tab with the pin icon).
	Sticky bool
	// Batch groups the editors that closed together: Close All Editors closes a batch, and Reopen Closed
	// Editor opens it again at once.
	Batch int
	// ViewState is what the window kept of the editor (the cursors, the scroll position, the folds), to give
	// back when it is opened again. The history does not look into it.
	ViewState any
}

// ClosedEditors is the list of the editors closed last, the newest last, at most MaxRecentlyClosedEditors.
type ClosedEditors struct {
	list      []ClosedEditor
	nextBatch int
}

// NewClosedEditors makes an empty list.
func NewClosedEditors() *ClosedEditors { return &ClosedEditors{} }

// Len is the number of closed editors.
func (c *ClosedEditors) Len() int { return len(c.list) }

// List is the closed editors, the oldest first.
func (c *ClosedEditors) List() []ClosedEditor { return slices.Clone(c.list) }

// NewBatch starts a batch: the editors recorded until the next call are one batch. It gives its number.
func (c *ClosedEditors) NewBatch() int {
	c.nextBatch++
	return c.nextBatch
}

// Record adds an editor as the last closed one. An older entry of the same file goes away first, and the
// oldest entry when the list is full.
func (c *ClosedEditors) Record(e ClosedEditor) {
	c.RemoveResource(e.Resource)
	c.list = append(c.list, e)
	if len(c.list) > MaxRecentlyClosedEditors {
		c.list = c.list[1:]
	}
}

// TakeLastBatch removes the editors of the last batch and gives them in the order they were closed.
func (c *ClosedEditors) TakeLastBatch() []ClosedEditor {
	if len(c.list) == 0 {
		return nil
	}
	batch := c.list[len(c.list)-1].Batch
	i := len(c.list)
	for i > 0 && c.list[i-1].Batch == batch {
		i--
	}
	out := slices.Clone(c.list[i:])
	c.list = c.list[:i]
	return out
}

// RemoveResource forgets the closed editors of a file.
func (c *ClosedEditors) RemoveResource(uri document.URI) {
	c.list = slices.DeleteFunc(c.list, func(e ClosedEditor) bool { return e.Resource == uri })
}

// Clear forgets every closed editor.
func (c *ClosedEditors) Clear() { c.list = nil }

// move gives the closed editors of a file the path it moved to.
func (c *ClosedEditors) move(follow func(document.URI) (document.URI, bool)) {
	for i := range c.list {
		if nu, ok := follow(c.list[i].Resource); ok {
			c.list[i].Resource = nu
		}
	}
}

// Ref names an editor of a group.
type Ref struct {
	GroupID int
	Input   EditorInput
}

// RecentStack is the list that Open Next Recently Used Editor and Open Previous Recently Used Editor walk:
// the editors in the order they were used, taken when the walk starts and kept while it goes on, with the
// place in it. Any other change of the order of use ends the walk (Reset), as VS Code's history service does
// unless the walk itself caused it (Navigating).
type RecentStack struct {
	items []Ref
	index int
	// navigating is set while the walk activates an editor, which changes the order of use too.
	navigating bool
}

// Reset ends the walk: the next step takes the list again. It does nothing while the walk activates an editor.
func (r *RecentStack) Reset() {
	if r.navigating {
		return
	}
	r.items, r.index = nil, 0
}

// SetNavigating says that the walk is activating an editor.
func (r *RecentStack) SetNavigating(on bool) { r.navigating = on }

// Step moves the place by delta (the next editor is -1, the previous one +1) in the list that fresh gives when
// no walk is on, keeping it inside the list, and gives the editor there; false when the list is empty.
func (r *RecentStack) Step(fresh func() []Ref, delta int) (Ref, bool) {
	if r.items == nil {
		r.items = fresh()
		r.index = 0
	}
	if len(r.items) == 0 {
		return Ref{}, false
	}
	r.index = min(max(r.index+delta, 0), len(r.items)-1)
	return r.items[r.index], true
}
