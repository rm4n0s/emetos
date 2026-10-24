package workspace

import (
	"context"
	"io/fs"
	"path/filepath"
)

// Apply updates the tree with the events of the watcher and tells the listeners (OnChange). It
// changes only what was read: an event for a folder whose children were not read is ignored,
// since reading it later finds the new state. Events for paths outside the folder are ignored.
// What the tree has read of a renamed or moved folder stays read. It returns the changes it
// made. ctx stops the reading of a folder that an EventRescan asks for.
//
// A Created or Changed event for a path the tree does not have is an addition, and one for a
// path it has is an update: a file saved by renaming a temporary file over it (reported as
// Changed) is both.
func (w *Workspace) Apply(ctx context.Context, events []Event) []Change {
	var changes []Change
	var rescans []string
	w.mu.Lock()
	for _, e := range events {
		switch e.Kind {
		case EventCreated, EventChanged:
			changes = w.applyPresent(changes, e.Path)
		case EventDeleted:
			changes = w.applyDeleted(changes, e.Path)
		case EventRenamed:
			changes = w.applyRenamed(changes, e)
		case EventRescan:
			rescans = append(rescans, e.Path)
		}
	}
	w.mu.Unlock()
	w.notify(changes)
	for _, p := range rescans {
		if item := w.Find(p); item != nil {
			if err := w.Refresh(ctx, item); err != nil {
				return changes
			}
			changes = append(changes, Change{Kind: ChangeRefreshed, Path: p, Item: item})
		}
	}
	return changes
}

// applyPresent handles a path that exists now (or did a moment ago). The caller holds mu.
func (w *Workspace) applyPresent(changes []Change, path string) []Change {
	rel, ok := w.relative(path)
	if !ok || rel == "" {
		if ok { // the folder itself changed
			if info, err := w.fsys.Stat(path); err == nil {
				w.root.mtime = info.ModTime()
			}
		}
		return changes
	}
	parent := w.find(filepath.Dir(path))
	if parent == nil || !parent.resolved || !parent.isDir {
		return changes
	}
	name := filepath.Base(path)
	existing := parent.children[name]
	info, err := w.fsys.Lstat(path)
	if err != nil {
		return w.applyDeleted(changes, path) // gone again
	}
	if w.excludedRel(rel, func(sibling string) bool { return w.siblingExists(parent, sibling) }) {
		if existing != nil {
			return w.applyDeleted(changes, path)
		}
		return changes
	}
	c, ok := w.describe(path, fs.FileInfoToDirEntry(info))
	if !ok {
		return changes
	}
	if existing == nil {
		item := &Item{ws: w, parent: parent, name: name, path: path}
		item.setFrom(c)
		parent.children[name] = item
		return append(changes, Change{Kind: ChangeAdded, Path: path, Item: item, Parent: parent})
	}
	existing.setFrom(c)
	return append(changes, Change{Kind: ChangeUpdated, Path: path, Item: existing, Parent: parent})
}

// siblingExists tells whether a folder has a file of a name, on disk if it was not read. The
// caller holds mu.
func (w *Workspace) siblingExists(parent *Item, name string) bool {
	if _, ok := parent.children[name]; ok {
		return true
	}
	_, err := w.fsys.Lstat(filepath.Join(parent.path, name))
	return err == nil
}

// applyDeleted handles a path that is gone. The caller holds mu.
func (w *Workspace) applyDeleted(changes []Change, path string) []Change {
	item := w.find(path)
	if item == nil {
		return changes
	}
	parent := item.parent
	if item == w.root {
		// The folder itself is gone: the UI hears of it, the tree stays as it was.
		return append(changes, Change{Kind: ChangeRemoved, Path: path, Item: item})
	}
	w.detach(item)
	return append(changes, Change{Kind: ChangeRemoved, Path: path, Item: item, Parent: parent})
}

// applyRenamed handles an item that moved. The caller holds mu.
func (w *Workspace) applyRenamed(changes []Change, e Event) []Change {
	old := w.find(e.OldPath)
	if old == nil || old == w.root {
		return w.applyPresent(changes, e.Path)
	}
	rel, inside := w.relative(e.Path)
	newParent := w.find(filepath.Dir(e.Path))
	if !inside || rel == "" || newParent == nil || !newParent.resolved || !newParent.isDir ||
		w.excludedRel(rel, func(s string) bool { return w.siblingExists(newParent, s) }) {
		// it left the part of the tree that was read
		return w.applyDeleted(changes, e.OldPath)
	}
	w.detach(old)
	if replaced := newParent.children[filepath.Base(e.Path)]; replaced != nil {
		w.detach(replaced) // a file renamed over another one
	}
	old.name = filepath.Base(e.Path)
	old.parent = newParent
	newParent.children[old.name] = old
	old.updatePaths()
	if info, err := w.fsys.Lstat(e.Path); err == nil {
		if c, ok := w.describe(e.Path, fs.FileInfoToDirEntry(info)); ok {
			old.setFrom(c)
		}
	}
	return append(changes, Change{Kind: ChangeRenamed, Path: e.Path, OldPath: e.OldPath, Item: old, Parent: newParent})
}

// updatePaths makes the paths of an item and of what is below it follow its name and folder.
func (i *Item) updatePaths() {
	if i.parent != nil {
		i.path = filepath.Join(i.parent.path, i.name)
	}
	for _, c := range i.children {
		c.updatePaths()
	}
}
