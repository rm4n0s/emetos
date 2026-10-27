package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/views/explorer"
	outlineview "github.com/rm4n0s/emetos/internal/gui/views/outline"
)

// The Explorer of the window (SPEC §10.8): the tree of the folder, the Open Editors, and the welcome
// content of a window with no folder, with the commands that reveal files in them and open and close the
// folder.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/explorerViewlet.ts,
// src/vs/workbench/contrib/files/browser/fileActions.ts and fileCommands.ts, and
// src/vs/workbench/browser/actions/workspaceActions.ts

// fileIcons keeps the file icon theme the window uses, loaded when workbench.iconTheme names it.
type fileIcons struct {
	mu    sync.Mutex
	id    string
	theme *theme.FileIconTheme
	// failed is the id of a theme that could not be loaded, which is not tried again.
	failed string
}

// FileIcons is the file icon theme of the window (workbench.iconTheme), or nil for none ("null", or a theme
// that cannot be loaded, which is reported once). While the file icon theme picker previews a theme, it is that
// one. It is safe for concurrent use.
func (w *Workspace) FileIcons() *theme.FileIconTheme {
	id, _ := settings.Get[any](w.Settings, "workbench.iconTheme", "").(string)
	if previewed, on := w.Process.FileIconThemePreview(); on {
		id = previewed
	}
	c := &w.icons
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" {
		c.id, c.theme = "", nil
		return nil
	}
	if id == c.id {
		return c.theme
	}
	if id == c.failed {
		return nil
	}
	th, err := theme.BundledFileIconTheme(id)
	if err != nil {
		c.failed = id
		w.Notify.Warn("Emetos", "Cannot use the file icon theme "+id+": "+describe(err))
		return nil
	}
	c.id, c.theme = id, th
	return th
}

// languageOf is the language of the file at a path, found from its name as a document would.
func (w *Workspace) languageOf(path string) string {
	return w.Process.Languages.Detect(path, "")
}

// explorerState is what the shell keeps of the Explorer.
type explorerState struct {
	// view is the tree of the folder, nil while the window has no folder; editors is the Open Editors.
	view    *explorer.View
	editors *explorer.OpenEditors
	// outline is the model of the Outline view (outline.go).
	outline *outlineview.Outline
	// focus tells which views of the Explorer have the keyboard focus.
	focus map[string]bool
	// revealedFor is the file of the active editor that was last revealed, and wasVisible whether the
	// folder view was on screen then.
	revealedFor string
	wasVisible  bool
	// stops end what the Explorer follows; removeCommands give its commands back.
	stops          []func()
	removeCommands []func()
	// clip is the internal clipboard of the file operations, and removeUndo gives back undo and redo, which the
	// tree takes while it has the focus.
	clip       fileClip
	removeUndo []func()
	// stopState stops following the expanded folders for the UI state of the window.
	stopState func()
}

// initExplorer makes the Explorer's views, follows the folder and the settings, and binds the commands.
func (s *shellState) initExplorer(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	ex := &s.explorer
	ex.focus = map[string]bool{}
	ex.editors = explorer.NewOpenEditors(explorer.OpenEditorsOpts{
		Grid: s.group.grid,
		SortOrder: func() string {
			return settings.Get[string](win.Settings, "explorer.openEditors.sortOrder", "")
		},
		Visible: func() int {
			return int(settings.Get[float64](win.Settings, "explorer.openEditors.visible", ""))
		},
		DecorationColors: func() bool { return settings.Get[bool](win.Settings, "workbench.editor.decorations.colors", "") },
		Icons:            win.FileIcons, Language: win.languageOf, Keys: win.Keys,
		OnOpen: func(g *editorgroup.Group, input editorgroup.EditorInput, pinned bool) {
			s.groupFocused(g)
			g.Model.SetActive(input)
			if pinned {
				g.Model.Pin(input)
			}
		},
		OnActivateGroup: s.groupFocused,
		OnClose: func(g *editorgroup.Group, input editorgroup.EditorInput) {
			s.closeEditors(g, []editorgroup.EditorInput{input})
		},
	})
	s.makeExplorerView(binding)
	s.initOutline(binding)
	ex.stops = append(ex.stops,
		win.OnFolderChange(func() { s.folderChanged(binding) }),
		win.OnFileEvents(func(events []workspace.Event) {
			binding.Post(func() {
				if s.Mounted() {
					s.externalFileEvents(events)
				}
			})
		}),
		win.Settings.OnChange(func(e settings.ChangeEvent) {
			if e.Affects("files.enableTrash", "") {
				binding.Post(func() {
					if s.Mounted() && ex.view != nil {
						ex.view.SyncKeys()
					}
				})
			}
			if e.Affects("explorer.openEditors", "") || e.Affects("workbench.editor.decorations", "") || e.Affects("workbench.iconTheme", "") || e.Affects("explorer.autoReveal", "") {
				binding.Post(func() {
					if !s.Mounted() {
						return
					}
					ex.editors.Refresh()
					s.SetState(func() {})
				})
			}
		}),
		// the file icon theme picker previews a theme
		win.Process.OnFileIconThemePreview(func() {
			binding.Post(func() {
				if !s.Mounted() {
					return
				}
				ex.editors.Refresh()
				s.SetState(func() {})
			})
		}),
		win.Views.OnChange(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		}),
		// the tree is revealed when it comes on screen, and the keys follow the side bar
		win.Layout.OnChange(func() {
			binding.Post(func() {
				if s.Mounted() {
					s.syncExplorer(binding, win)
				}
			})
		}),
	)
	s.bindExplorerCommands(binding)
}

// disposeExplorer ends the views of the Explorer and gives the commands back.
func (s *shellState) disposeExplorer() {
	ex := &s.explorer
	for _, stop := range ex.stops {
		stop()
	}
	for _, remove := range ex.removeCommands {
		remove()
	}
	if ex.stopState != nil {
		ex.stopState()
	}
	if ex.view != nil {
		ex.view.Dispose()
	}
	if ex.editors != nil {
		ex.editors.Dispose()
	}
	win := s.win
	for _, f := range ex.removeUndo {
		f()
	}
	for _, key := range []string{"explorerViewletVisible", "foldersViewVisible", "explorerViewletFocus", "filesExplorerFocus", "openEditorsFocus", "fileCopied"} {
		win.Keys.Reset(key)
	}
	*ex = explorerState{}
}

// makeExplorerView makes the tree of the folder of the window, or drops it when the window has none.
func (s *shellState) makeExplorerView(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	ex := &s.explorer
	if ex.view != nil {
		ex.view.Dispose()
		ex.view = nil
	}
	if win.Workspace == nil {
		return
	}
	ex.view = explorer.NewView(win.ctx, win.Workspace, explorer.ViewOpts{
		Post: binding.Post, Binding: binding, Keys: win.Keys, Icons: win.FileIcons, Language: win.languageOf,
		OnOpen: func(path string, pinned bool) { s.openPath(binding, nil, path, !pinned) },
		OnLoadFailed: func(path string, err error) {
			win.Notify.Error("Emetos", "Cannot read the folder "+path+": "+describe(err))
		},
		Logger:       win.Process.Logger,
		AfterFrame:   explorer.AfterFrame(binding),
		TrashEnabled: func() bool { return settings.Get[bool](win.Settings, "files.enableTrash", "") },
		DragEnabled:  func() bool { return settings.Get[bool](win.Settings, "explorer.enableDragAndDrop", "") },
		Ctrl:         func() bool { return binding.Keyboard().IsControlPressed() },
		OnCommitEdit: func(r explorer.EditResult) { s.editCommitted(binding, r) },
		OnDropItems: func(sources []string, folder string, copying bool) {
			s.dropItems(binding, sources, folder, copying)
		},
		OnImport:      func(paths []string, folder string) { s.importFiles(binding, paths, folder) },
		OnContextMenu: s.openExplorerMenu,
	})
	// the folders the saved state expanded (once: a folder opened later starts with none)
	if dirs := win.takeExpanded(); len(dirs) > 0 {
		ex.view.Tree().SetExpandedKeys(dirs)
	}
	s.followExplorerState()
	win.FileOps.ClearHistory()
	ex.clip = fileClip{}
	win.Keys.Set("fileCopied", false)
}

// folderChanged follows SetFolder: the tree is made for the new folder, the Problems show paths relative to it
// and the window is built again (its title, the status bar and the header of the folder view).
func (s *shellState) folderChanged(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	s.makeExplorerView(binding)
	s.explorer.revealedFor = ""
	if s.panels.problems != nil {
		s.panels.problems.Model().SetRoot(win.Folder)
	}
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// explorerShown tells whether the folder view is on screen: the side bar shows the Explorer and the view is
// not folded.
func (s *shellState) explorerShown(win *Workspace) bool {
	return win.Layout.State().SideBarVisible && win.Views.Active() == command.ViewContainerExplorer &&
		!win.Views.IsHidden(command.ViewExplorerFolders) && !win.Views.IsCollapsed(command.ViewExplorerFolders)
}

// activePath is the file of the active editor, or "".
func (s *shellState) activePath() string {
	if active := s.activeEditor(); active != nil {
		if path, ok := fileResource(active.input); ok {
			return path
		}
	}
	return ""
}

// syncExplorer sets the context keys of the Explorer and reveals the file of the active editor in the tree
// when it changed or when the tree comes on screen (explorer.autoReveal). It is called while the shell
// builds, so the reveal is made after it.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerView.ts (selectActiveFile)
func (s *shellState) syncExplorer(binding *widgets.WidgetsBinding, win *Workspace) {
	ex := &s.explorer
	shown := s.explorerShown(win) && ex.view != nil
	win.Keys.Batch(func() {
		win.Keys.Set("explorerViewletVisible", win.Layout.State().SideBarVisible && win.Views.Active() == command.ViewContainerExplorer)
		win.Keys.Set("foldersViewVisible", shown)
	})
	path := s.activePath()
	mode, on := autoRevealMode(win)
	if shown && on && path != "" && (!ex.wasVisible || path != ex.revealedFor) {
		ex.revealedFor = path
		binding.Post(func() {
			if s.Mounted() && ex.view != nil {
				ex.view.Reveal(path, mode, nil)
			}
		})
	}
	ex.wasVisible = shown
}

// autoRevealMode reads explorer.autoReveal: true selects and scrolls, "focusNoScroll" selects without
// scrolling, false does nothing.
func autoRevealMode(win *Workspace) (mode explorer.RevealMode, on bool) {
	switch v := settings.Get[any](win.Settings, "explorer.autoReveal", "").(type) {
	case bool:
		return explorer.RevealSelect, v
	case string:
		if v == "focusNoScroll" {
			return explorer.RevealNoScroll, true
		}
	}
	return explorer.RevealSelect, false
}

// viewBody is the body of a view of the Explorer.
func (s *shellState) viewBody(_ widgets.BuildContext, viewID string) widgets.Widget {
	ex := &s.explorer
	binding := widgets.WidgetsBindingOf(s.Context())
	switch viewID {
	case command.ViewExplorerFolders:
		if ex.view == nil {
			return nil
		}
		// a right click on a row opens the context menu of the Explorer
		return kit.NewContextMenu(explorer.NewBody(ex.view, explorer.BodyOpts{AfterFrame: explorer.AfterFrame(binding)}),
			kit.ContextMenuOpts{
				Controller: s.explorerMenu, OnRun: s.menus.Run,
				OnHide: func() {
					s.afterFrame(binding, func() {
						if ex.view != nil {
							ex.view.Focus()
						}
					})
				},
			})
	case command.ViewExplorerEmpty:
		// the warning that opening a folder closes the editors holds only when the folder replaces this tab
		win := PeekWorkspace(s.Context())
		replaces := win.Process.Workspaces.setting(win, "window.openFoldersInNewWindow") == "off"
		return explorer.NewWelcome(explorer.WelcomeOpts{HasEditors: replaces && s.editorCount() > 0, OnOpenFolder: s.pickFolder})
	case command.ViewOpenEditors:
		return explorer.NewOpenEditorsBody(ex.editors)
	case command.ViewOutline:
		return outlineview.NewBody(ex.outline)
	}
	// a view an extension declared is the box of its content
	if scope := PeekWorkspace(s.Context()).Extensions(); scope != nil {
		return scope.ViewBody(viewID)
	}
	return nil
}

// viewName is the title of the folder view: the name of the folder, as VS Code shows it.
func (s *shellState) viewName(viewID string) string {
	if viewID == command.ViewExplorerFolders {
		if ws := PeekWorkspace(s.Context()).Workspace; ws != nil {
			return ws.Name()
		}
	}
	return ""
}

// viewActions are the buttons in the header of the folder view.
func (s *shellState) viewActions(_ widgets.BuildContext, viewID string) widgets.Widget {
	win := PeekWorkspace(s.Context())
	if viewID != command.ViewExplorerFolders {
		return s.extensionViewActions(win, viewID)
	}
	return explorer.NewActions(explorer.ActionsOpts{OnRun: func(id string) {
		if err := win.Commands.Run(command.Context{Ctx: win.ctx, Keys: win.Keys, Source: command.SourceMenu}, id, nil); err != nil {
			win.Process.Logger.Warn("explorer action failed", "command", id, "err", err)
		}
	}})
}

// viewBodyHeight gives the Open Editors a body of the height of its rows.
func (s *shellState) viewBodyHeight(viewID string) (float64, bool) {
	if viewID == command.ViewOpenEditors && s.explorer.editors != nil {
		return s.explorer.editors.BodyHeight(), true
	}
	return 0, false
}

// viewFocused sets the context keys that say where in the Explorer the focus is.
func (s *shellState) viewFocused(viewID string, has bool) {
	win := PeekWorkspace(s.Context())
	ex := &s.explorer
	if ex.focus == nil {
		return
	}
	ex.focus[viewID] = has
	any := false
	for _, id := range []string{command.ViewExplorerFolders, command.ViewExplorerEmpty, command.ViewOpenEditors, command.ViewOutline} {
		any = any || ex.focus[id]
	}
	win.Keys.Batch(func() {
		win.Keys.Set("explorerViewletFocus", any)
		win.Keys.Set("filesExplorerFocus", ex.focus[command.ViewExplorerFolders])
		win.Keys.Set("openEditorsFocus", ex.focus[command.ViewOpenEditors])
		win.Keys.Set("outlineFocus", ex.focus[command.ViewOutline])
	})
	// the tree takes undo and redo for the file operations while it has the focus
	if viewID == command.ViewExplorerFolders {
		s.bindUndoRedo(widgets.WidgetsBindingOf(s.Context()), has)
	}
}

// showExplorer shows the Explorer in the side bar, with its folder view unfolded, and gives the keyboard
// focus to the tree when focus is set.
func (s *shellState) showExplorer(binding *widgets.WidgetsBinding, focus bool) {
	win := PeekWorkspace(s.Context())
	win.Views.Open(command.ViewContainerExplorer)
	win.Views.SetCollapsed(command.ViewExplorerFolders, false)
	if focus {
		s.afterFrame(binding, func() {
			if s.explorer.view != nil {
				s.explorer.view.Focus()
			}
		})
	}
}

// afterFrame runs f after the next frame, if the shell is still on screen.
func (s *shellState) afterFrame(binding *widgets.WidgetsBinding, f func()) {
	scheduler := binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() {
			f()
		}
	})
}

// insideFolder tells whether a path is in the folder of the window.
func insideFolder(win *Workspace, path string) bool {
	if win.Folder == "" {
		return false
	}
	rel, err := filepath.Rel(win.Folder, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// revealInExplorer shows a file in the tree of the folder and gives it the focus; a file that is not in the
// folder is looked for in the Open Editors, when they are shown.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileCommands.ts (REVEAL_IN_EXPLORER_COMMAND_ID)
func (s *shellState) revealInExplorer(binding *widgets.WidgetsBinding, path string) {
	win := PeekWorkspace(s.Context())
	ex := &s.explorer
	if path == "" {
		return
	}
	if ex.view != nil && insideFolder(win, path) {
		s.showExplorer(binding, false)
		ex.view.Reveal(path, explorer.RevealSelect, func(found bool) {
			if found {
				s.afterFrame(binding, ex.view.Focus)
			}
		})
		return
	}
	if !win.Views.IsHidden(command.ViewOpenEditors) {
		win.Views.Open(command.ViewContainerExplorer)
		win.Views.SetCollapsed(command.ViewOpenEditors, false)
		s.afterFrame(binding, ex.editors.Focus)
	}
}

// bindExplorerCommands takes over the commands of the Explorer in the registry while the shell is on screen.
func (s *shellState) bindExplorerCommands(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	ex := &s.explorer
	run := func(f func()) command.Handler {
		return func(command.Context, any) error { f(); return nil }
	}
	focusFolders := run(func() { s.showExplorer(binding, true) })
	handlers := map[string]command.Handler{
		"workbench.view.explorer":                   focusFolders,
		"workbench.explorer.fileView.focus":         focusFolders,
		"workbench.files.action.focusFilesExplorer": focusFolders,
		"workbench.files.action.focusOpenEditorsView": run(func() {
			win.Views.SetHidden(command.ViewOpenEditors, false)
			win.Views.Open(command.ViewContainerExplorer)
			s.afterFrame(binding, ex.editors.Focus)
		}),
		"workbench.files.action.showActiveFileInExplorer": run(func() { s.revealInExplorer(binding, s.activePath()) }),
		"revealInExplorer": func(_ command.Context, args any) error {
			path, _ := args.(string)
			if path == "" {
				if _, e := s.commandTarget(args); e != nil {
					path, _ = fileResource(e)
				}
			}
			s.revealInExplorer(binding, path)
			return nil
		},
		"workbench.files.action.refreshFilesExplorer": run(func() {
			win.Views.Open(command.ViewContainerExplorer)
			if ex.view != nil {
				ex.view.Refresh()
			}
		}),
		"workbench.files.action.collapseExplorerFolders": run(func() {
			if ex.view != nil {
				ex.view.CollapseAll()
			}
		}),
		"workbench.action.files.openFolder": run(s.pickFolder),
		"workbench.action.closeFolder":      run(func() { s.openFolder("") }),
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		ex.removeCommands = append(ex.removeCommands, remove)
	}
	s.bindFileOpsCommands(binding)
	s.bindRecentCommands(binding)
	s.bindOutlineCommands(binding)
}

// chooseFolder shows the folder dialog of the desktop (a file chooser portal that picks folders) and waits for
// the user. It must be called from a goroutine, as Glutter's dialogs wait for the user. A dialog that is closed
// without a choice gives "" and no error. The error has the tag OpenFolderDialogFailed.
func chooseFolder(ctx context.Context, bc widgets.BuildContext, current string) (string, error) {
	res, err := desktop.OpenFiles(ctx, bc, desktop.OpenFilesOpts{
		Title: "Open Folder", AcceptLabel: "Open", Directory: true, CurrentFolder: current,
	})
	if err != nil {
		return "", errors.NewErr("OpenFolderDialogFailed", err)
	}
	if res.Cancelled || len(res.Paths) == 0 {
		return "", nil
	}
	return res.Paths[0], nil
}

// pickFolder asks the user for a folder, with the dialog of the desktop on a goroutine, and opens the one
// chosen in the window. A dialog that fails is reported as a notification.
func (s *shellState) pickFolder() {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	bc := s.Context()
	s.Go(func(life context.Context) {
		path, err := chooseFolder(life, bc, win.Folder)
		binding.Post(func() {
			if s.Mounted() {
				s.folderChosen(path, err)
			}
		})
	})
}

// folderChosen is what the window does with the answer of the folder dialog, on the UI thread: it opens the
// folder, does nothing when the dialog was cancelled (an empty path), and tells the user when it failed.
func (s *shellState) folderChosen(path string, err error) {
	win := PeekWorkspace(s.Context())
	if err != nil {
		win.Process.Logger.Warn("the folder dialog failed", "err", err)
		win.Notify.Error("Emetos", "Cannot show the dialog to open a folder: "+describe(err))
		return
	}
	if path != "" {
		s.openFolder(path)
	}
}

// openFolder opens a folder picked inside the application (Open Folder, Open Recent, the button of the empty Explorer;
// Close Folder with an empty path) by the rules of SPEC §10.14, the ones a launch follows: a tab that has the folder
// becomes the active tab; else window.openFoldersInNewWindow decides: in a new tab ("on"), in the active tab ("off":
// its editors close first, and when some have unsaved changes a dialog asks whether to save them, and Cancel leaves the
// tab as it is), or, with "default", in a new tab unless the active tab is an untouched No Folder tab, which is
// replaced. An empty path turns the active tab into a No Folder tab.
//
// Ported from VS Code: src/vs/workbench/browser/actions/workspaceActions.ts (OpenFolderAction,
// CloseWorkspaceAction) and SPEC §10.14
func (s *shellState) openFolder(path string) { s.openFolderIn(path, false) }

// openFolderIn is openFolder that opens the folder in a new tab when newTab is set (the Ctrl key of the Open Recent
// picker).
func (s *shellState) openFolderIn(path string, newTab bool) {
	win := PeekWorkspace(s.Context())
	win.Process.Workspaces.OpenFolder(win, path, newTab, false, nil)
}

// openFolderThen replaces the folder of this window, whatever window.openFoldersInNewWindow says, and tells done, on
// the UI thread, whether the window has the folder once the question about unsaved changes is answered and the
// folder opened (true also for the folder the window has already); false when the user cancelled or the folder
// could not be opened. done may be nil. A window that the manager hosts is made again for the folder, so done
// belongs to a window that is gone by then: it is for a window on its own.
func (s *shellState) openFolderThen(path string, done func(opened bool)) {
	win := PeekWorkspace(s.Context())
	if path != "" {
		path = filepath.Clean(path)
	}
	win.Process.Workspaces.replaceFolder(win, path, "", func(w *Workspace) {
		if done != nil {
			done(w != nil)
		}
	})
}

// closeAllEditors closes every editor of every group (the sticky ones too), asking about unsaved changes first;
// done is told whether they closed.
func (s *shellState) closeAllEditors(done func(closed bool)) {
	s.closeConfirmed(editorgroup.AllTargets(s.group.grid), done)
}
