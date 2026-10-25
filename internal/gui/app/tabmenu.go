package app

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The context menu of a tab (SPEC §10.3) and the commands that copy the path of a file.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorTabsControl.ts (onTabContextMenu)
// and src/vs/workbench/contrib/files/browser/fileCommands.ts (resourcesToClipboard)

// tabMenuContext is the context the menu of the tab of an editor is evaluated in: the keys of the
// window, with those that describe the active editor set for the editor of the tab, as VS Code sets
// them while it shows the menu.
func (s *shellState) tabMenuContext(group *editorgroup.Group, index int) contextkey.Context {
	win := PeekWorkspace(s.Context())
	extra := map[string]any{
		"splitEditorsVertically": s.sideBySideDirection() == editorgroup.DirDown,
		"editorTabsVisible":      win.ShowTabs() == "multiple",
	}
	m := group.Model
	if e := m.EditorAt(index); e != nil {
		_, isFile := fileResource(e)
		extra["activeEditorIsNotPreview"] = m.IsPinned(e)
		extra["activeEditorIsPinned"] = m.IsSticky(e)
		extra["activeEditorIsDirty"] = e.Dirty()
		extra["activeEditorIsFirstInGroup"] = index == 0
		extra["activeEditorIsLastInGroup"] = index == m.Count()-1
		extra["groupEditorsCount"] = float64(m.Count())
		extra["isFileSystemResource"] = isFile
	}
	return menuContext{base: win.Keys, settings: win.Settings, extra: extra}
}

// tabMenuItems are the items of the context menu of the tab at an index of a group. Each item runs its
// command for that tab: the argument is the context {"groupId", "editorIndex"}.
func (s *shellState) tabMenuItems(group *editorgroup.Group, index int) []kit.MenuItem {
	items := s.menus.Items(command.EditorTitleContext, s.tabMenuContext(group, index))
	withArgs(items, tabContext(group, index))
	return items
}

// withArgs sets the argument of the commands of items, and of their submenus, that have none.
func withArgs(items []kit.MenuItem, args any) {
	for i := range items {
		switch {
		case items[i].Separator:
		case items[i].Submenu:
			withArgs(items[i].Items, args)
		case items[i].Data == nil:
			items[i].Data = args
		}
	}
}

// openTabMenu shows the context menu of a tab at a position of the window.
func (s *shellState) openTabMenu(group *editorgroup.Group, index int, at ui.Offset) {
	if group.Model.EditorAt(index) == nil {
		return
	}
	s.view(group).tabMenu.Show(at, s.tabMenuItems(group, index))
}

// fileResource is the path of the file an editor shows; false for an editor that shows another
// resource or none.
func fileResource(e editorgroup.EditorInput) (string, bool) {
	uri, ok := e.Resource()
	if !ok || uri.Scheme != document.SchemeFile {
		return "", false
	}
	return uri.Path, true
}

// pathText is the text the commands that copy a path put on the clipboard: the path of the file, or
// its path relative to the folder of the window when it is in it.
func pathText(path, folder string, relative bool) string {
	if !relative || folder == "" {
		return path
	}
	rel, err := filepath.Rel(folder, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

// pathCommands are the commands that copy the path of a file: copyFilePath and copyRelativeFilePath for the
// files their argument names (a path, a list of paths, or the context of the tab that was clicked), else for the
// selected items of the Explorer when the folder view has the focus, else for the active editor;
// workbench.action.files.copyPathOfActiveFile is for the active editor.
func (s *shellState) pathCommands() map[string]command.Handler {
	copyOf := func(relative, active bool) command.Handler {
		return func(_ command.Context, args any) error {
			if active {
				args = nil
				if e := s.activeGroup().Model.ActiveEditor(); e != nil {
					if path, ok := fileResource(e); ok {
						args = path
					}
				}
				if args == nil {
					return nil
				}
			}
			s.copyPaths(s.commandPaths(args), relative)
			return nil
		}
	}
	return map[string]command.Handler{
		"copyFilePath":         copyOf(false, false),
		"copyRelativeFilePath": copyOf(true, false),
		"workbench.action.files.copyPathOfActiveFile": copyOf(false, true),
	}
}

// copyText puts a text on the clipboard; a clipboard that fails is reported.
func (s *shellState) copyText(text string) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	editor.NewClipboard(win.ctx, binding.Clipboard()).Write(text, func(err error) {
		if err != nil {
			win.Process.Logger.Warn("cannot copy to the clipboard", "err", err)
			win.Notify.Warn("Emetos", "Cannot copy to the clipboard: "+describe(err))
		}
	})
}
