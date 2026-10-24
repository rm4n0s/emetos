package explorer

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Drag and drop in the Explorer: the rows are dragged and dropped on folders to move them (Control makes it a
// copy), and files dragged from the file manager are dropped on folders to copy them in (SPEC §10.5).
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerViewer.ts (FileDragAndDrop) and
// src/vs/workbench/contrib/files/browser/fileImportExport.ts (ExternalFileImport)

func (v *View) isCopy() bool { return v.opts.Ctrl != nil && v.opts.Ctrl() }

// dragAndDrop is the drag and drop of the rows, or nil while explorer.enableDragAndDrop is off.
func (v *View) dragAndDrop() *kit.DragAndDrop {
	if v.opts.DragEnabled != nil && !v.opts.DragEnabled() {
		return nil
	}
	return &kit.DragAndDrop{
		CanDrag: func(keys []string) bool { return v.edit == nil && len(v.sourcesOf(keys)) > 0 },
		DragLabel: func(keys []string) string {
			if srcs := v.sourcesOf(keys); len(srcs) == 1 {
				return srcs[0].Name()
			}
			return strconv.Itoa(len(keys))
		},
		CanDrop: func(target string, _ kit.DropPosition, keys []string) kit.DropEffect {
			return v.dropEffect(target, keys)
		},
		OnDrop: func(target string, _ kit.DropPosition, keys []string) {
			folder, ok := v.dropFolder(target)
			if !ok || v.opts.OnDropItems == nil {
				return
			}
			var paths []string
			for _, it := range v.sourcesOf(keys) {
				paths = append(paths, it.Path())
			}
			v.opts.OnDropItems(paths, folder.Path(), v.isCopy())
		},
	}
}

// sourcesOf are the items the rows of some keys stand for, without the ones inside another of them (moving
// a folder moves what is in it).
func (v *View) sourcesOf(keys []string) []*workspace.Item {
	var items []*workspace.Item
	for _, k := range keys {
		if it := v.itemOf(k); it != nil {
			items = append(items, it)
		}
	}
	var out []*workspace.Item
	for _, it := range items {
		inside := false
		for _, other := range items {
			if other != it && strings.HasPrefix(it.Path(), other.Path()+string(filepath.Separator)) {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, it)
		}
	}
	return out
}

// dropFolder is the folder a drop on a row goes into: the folder itself, the folder a file is in, or the folder of the
// window for the empty part of the tree.
func (v *View) dropFolder(target string) (*workspace.Item, bool) {
	if target == "" {
		return v.ws.Root(), true
	}
	it := v.itemOf(target)
	if it == nil {
		return nil, false
	}
	if it.IsDir() {
		return it, true
	}
	if p := it.Parent(); p != nil {
		return p, true
	}
	return nil, false
}

// dropEffect is what dropping the rows of some keys on a row does: nothing for a folder onto itself or into
// a folder below it, and for a move into the folder an item is in already; a copy with Control held.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerViewer.ts (FileDragAndDrop.handleDragOver)
func (v *View) dropEffect(target string, keys []string) kit.DropEffect {
	folder, ok := v.dropFolder(target)
	srcs := v.sourcesOf(keys)
	if !ok || len(srcs) == 0 || v.edit != nil {
		return kit.DropEffectNone
	}
	copying := v.isCopy()
	dir := folder.Path()
	for _, src := range srcs {
		p := src.Path()
		switch {
		case p == dir || strings.HasPrefix(dir, p+string(filepath.Separator)):
			return kit.DropEffectNone // onto itself, or a folder into its own child
		case target != "" && v.itemOf(target) != nil && v.itemOf(target).Path() == p:
			return kit.DropEffectNone
		case !copying && filepath.Dir(p) == dir:
			return kit.DropEffectNone // already there; a copy makes "name copy"
		}
	}
	if copying {
		return kit.DropEffectCopy
	}
	return kit.DropEffectMove
}

// contextMenu follows a press of the secondary button in the tree: the row is selected (unless it is part of the
// selection) and the owner shows the menu.
func (v *View) contextMenu(key string, at ui.Offset) {
	path := ""
	if key == "" {
		v.tree.ClearSelection()
		v.tree.ClearFocus()
	} else {
		if !v.tree.IsSelected(key) {
			v.tree.SetSelected(key)
		}
		if it := v.itemOf(key); it != nil {
			path = it.Path()
		}
	}
	v.syncKeys()
	if v.opts.OnContextMenu != nil {
		v.opts.OnContextMenu(path, at)
	}
}

// importTarget wraps the tree in the target of the drags of other applications: files dropped on a folder are
// copied into it.
func (v *View) importTarget(tree widgets.Widget) widgets.Widget {
	setOver := func(key string) {
		if v.dropOver != key {
			v.dropOver = key
			v.notify()
		}
	}
	rowFolder := func(d widgets.DragTargetDetails[*widgets.ExternalDragData]) (*workspace.Item, string) {
		key, ok := v.ctl.KeyAt(d.Offset)
		if !ok {
			return v.ws.Root(), ""
		}
		folder, ok := v.dropFolder(key)
		if !ok {
			return v.ws.Root(), ""
		}
		if folder == v.ws.Root() {
			return folder, ""
		}
		return folder, v.KeyOf(folder.Path())
	}
	return widgets.NewDragTarget(func(widgets.BuildContext, []foundation.Opt[*widgets.ExternalDragData], []any) widgets.Widget {
		return tree
	}, widgets.DragTargetOpts[*widgets.ExternalDragData]{
		OnWillAcceptWithDetails: func(d widgets.DragTargetDetails[*widgets.ExternalDragData]) bool {
			return d.Data != nil && d.Data.HasFiles() && v.edit == nil && v.opts.OnImport != nil
		},
		OnMove: func(d widgets.DragTargetDetails[*widgets.ExternalDragData]) {
			_, key := rowFolder(d)
			setOver(key)
		},
		OnLeave: func(foundation.Opt[*widgets.ExternalDragData]) { setOver("") },
		OnAcceptWithDetails: func(d widgets.DragTargetDetails[*widgets.ExternalDragData]) {
			setOver("")
			folder, _ := rowFolder(d)
			if files := d.Data.Files(); len(files) > 0 && v.opts.OnImport != nil {
				v.opts.OnImport(files, folder.Path())
			}
		},
	})
}

// DropOver is the key of the row that a drag from another application is over, empty when none is.
func (v *View) DropOver() string { return v.dropOver }
