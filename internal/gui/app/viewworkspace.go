package app

import (
	"context"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// isView says that the workspace is the second, off-screen view of another workspace (headless.go), not a tab.
func (w *Workspace) isView() bool { return w.viewOf != nil }

// newViewWorkspace makes the workspace a headless view is built on (SPEC §10.18 "As built (step 15.1)"): what belongs to
// the user's own screen is new, what belongs to the workspace is the parent's.
//
//   - Shared with the parent: the process, the folder and its model, the settings, the snippets, the theme, the
//     documents (so an edit in one view is in the other, and a document stays open while either view shows it), the
//     notifications, the problems, the keybindings, the file operations and what the editors copied.
//   - The view's own: the commands (a fork of the process's, so that what the shell of the view binds is the view's), the
//     menus, the context keys, the focus scopes, the key dispatcher (a pending chord is the view's), the layout, the
//     views, the dialogs, the quick input, the status bar, the find widget's switches, the zoom and the recent files.
//
// Not in the view: the extensions' contributions (their views, commands, menu items and status items live in the scope the
// parent holds), the state files, the backups, the watcher of the folder (the parent's keeps both views up to date), and
// the commands that act on Emetos itself (quit, the window tabs, other workspaces): the shell of a view does not bind them.
func (p *Workspace) newViewWorkspace(ctx context.Context) *Workspace {
	proc := p.Process
	commands := proc.Commands.Fork()
	layout := workbench.NewLayout(workbench.LayoutStateFromSettings(p.Settings))
	access := quickinput.NewRegistry()
	p.folderMu.RLock()
	ws, folder, files := p.Workspace, p.Folder, p.files
	p.folderMu.RUnlock()
	w := &Workspace{
		QuickAccess:    access,
		QuickInput:     quickinput.NewController(quickinput.ControllerOptions{Registry: access}),
		CommandHistory: quickinput.NewCommandsHistory(0),
		Process:        proc,
		Commands:       commands,
		Menus:          proc.Menus.Fork(commands),
		Notify:         p.Notify,
		Folder:         folder,
		Workspace:      ws,
		files:          files,
		Settings:       p.Settings,
		Snippets:       p.Snippets,
		Themes:         p.Themes,
		profile:        p.profile,
		Keys:           contextkey.NewRoot(),
		Documents:      p.Documents,
		Keybindings:    p.Keybindings,
		Dispatcher:     keybinding.NewDispatcher(p.Keybindings, time.Now),
		Scopes:         kit.NewFocusScopes(),
		Clipboard:      p.Clipboard,
		Find:           &editor.FindMemory{},
		Zoom:           editor.NewZoom(),
		InputMode:      p.InputMode,
		Layout:         layout,
		Views:          workbench.NewViews(proc.Views.Fork(), layout),
		Dialogs:        kit.NewDialogController(),
		Recent:         workspace.NewRecentFiles(0),
		FileOps:        p.FileOps,
		Markers:        p.Markers,
		StatusBar:      workbench.NewStatusBar(),
		ctx:            ctx,
		viewOf:         p,
		settingsDocs:   p.settingsDocs,
		userKeys:       p.userKeys,
		suggest:        p.suggest,
		symbols:        p.symbols,
		symbolsReg:     p.symbolsReg,
		semantic:       p.semantic,
		semanticReg:    p.semanticReg,
		// Save As in a view asks for the path in the quick input: a file chooser of the desktop would open on the screen of
		// the host, where the user of the view cannot see it
		SaveDialog: noFileChooser,
		// and a link opens nothing on the desktop of the host
		OpenURI: noOpenURI,
	}
	w.Notifications = notify.NewPresenter(w.Notify)
	w.followZoom()
	w.syncLayoutKeys()
	w.Layout.OnChange(w.syncLayoutKeys)
	w.followZen()
	w.syncWorkbenchState()
	w.registerViewCommands()
	return w
}

// releaseView lets go of what the workspace of a view holds: its listeners on the settings and the other shared services.
func (w *Workspace) releaseView() {
	for _, stop := range w.stops {
		stop()
	}
	w.stops = nil
}

// noFileChooser is the SaveDialog of a view: the desktop of the host has the chooser, so the window asks for the path in the
// quick input instead (dialogUnavailable).
func noFileChooser(context.Context, widgets.BuildContext, SaveDialogOptions) (string, error) {
	return "", errors.New("FileChooserUnavailable", "a headless view has no file chooser on the screen of its user")
}

// noOpenURI is the OpenURI of a view: a web address would open in the browser of the host, which the user of the view does
// not see.
func noOpenURI(_ context.Context, _ widgets.BuildContext, uri string) error {
	return errors.New("HeadlessOpenURIUnavailable", "a headless view cannot open a link on the desktop of the host", "uri", uri)
}

// registerViewCommands binds the commands of a view that need no shell: the modes of the layout and the zoom. The commands
// that act on Emetos itself are not bound, and the registry answers them as it answers a command nothing handles.
func (w *Workspace) registerViewCommands() {
	set := func(id string, h command.Handler) {
		if _, err := w.Commands.SetHandler(id, h); err != nil {
			w.Process.Logger.Error("a command of a headless view cannot be bound", "command", id, "err", err)
		}
	}
	w.registerModeCommands(set)
}
