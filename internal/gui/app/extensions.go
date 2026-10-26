package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The extensions in the window (SPEC §10.15, step 11.1). The process holds the host (Process.Extensions) with the
// manifests of the extensions that are compiled in; every workspace tab is a scope of it (Workspace.Extensions), made
// when the workspace is, started once its services exist and closed with it. This file binds a scope to the services of
// its workspace: the commands and menus (the workspace's own forks), the keybindings (a layer over the default keymap),
// the settings, the notifications, the status bar and the documents.

// disabledExtensionsSetting is the setting that turns extensions off.
const disabledExtensionsSetting = "extensions.disabled"

// noFolderName is the name of a workspace tab that has no folder, as the scope and the Extensions page call it.
const noFolderName = "No Folder"

// extensionKeys is the layer of default keybindings that the extensions of a workspace add over the default keymap of the
// process. The resolver of the workspace is made from the keymap and this layer, so the keys come and go with the
// extensions; the keybindings.json of the user still wins.
type extensionKeys struct {
	w *Workspace

	mu    sync.Mutex
	next  int
	items []extensionKey
}

type extensionKey struct {
	id   int
	item keybinding.Item
}

// Add implements exthost.KeybindingSink.
func (k *extensionKeys) Add(items ...keybinding.Item) (remove func()) {
	k.mu.Lock()
	var ids []int
	for _, it := range items {
		k.next++
		k.items = append(k.items, extensionKey{id: k.next, item: it})
		ids = append(ids, k.next)
	}
	k.mu.Unlock()
	k.w.userKeys.rebuild()
	return func() {
		k.mu.Lock()
		before := len(k.items)
		k.items = slices.DeleteFunc(k.items, func(e extensionKey) bool { return slices.Contains(ids, e.id) })
		changed := len(k.items) != before
		k.mu.Unlock()
		if changed {
			k.w.userKeys.rebuild()
		}
	}
}

// list is the keybindings of the extensions, in the order they were added.
func (k *extensionKeys) list() []keybinding.Item {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]keybinding.Item, len(k.items))
	for i, e := range k.items {
		out[i] = e.item
	}
	return out
}

// defaultKeys is the default keymap of the workspace: the one of the process, then the keys of its extensions, which
// win over the built-in keys of the same chord as VS Code's keys of extensions do.
func (w *Workspace) defaultKeys() []keybinding.Item {
	defaults := w.Process.Keymap.Defaults()
	if w.extKeys == nil {
		return defaults
	}
	return append(slices.Clone(defaults), w.extKeys.list()...)
}

// Extensions is the scope of the workspace in the extension host, or nil before the workspace is complete or after it was
// released.
func (w *Workspace) Extensions() *exthost.Scope { return w.extScope.Load() }

// extensionName is the name the workspace goes by for the extensions and the Extensions page: the base name of the
// folder.
func (w *Workspace) extensionName() string {
	if w.Folder == "" {
		return noFolderName
	}
	return filepath.Base(w.Folder)
}

// disabledExtensions are the ids in extensions.disabled for this workspace.
func (w *Workspace) disabledExtensions() []string {
	return settings.Get[[]string](w.Settings, disabledExtensionsSetting, "")
}

// startExtensions makes the scope of the workspace in the host and starts it: the extensions that wait for the start, for
// a language of a document that is open, or for a file of the folder, are activated. It is the last step of making a
// workspace.
func (w *Workspace) startExtensions() {
	p := w.Process
	if p.Extensions == nil {
		return
	}
	w.extKeys = &extensionKeys{w: w}
	scope := p.Extensions.NewScope(exthost.ScopeOptions{
		Info: extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: w.extensionName(), Folder: w.Folder, ID: w.sharingID()},
		Ctx:  w.ctx, Commands: w.Commands, Menus: w.Menus, Keybindings: w.extKeys, Settings: w.Settings,
		Notify: w.Notify, StatusBar: w.StatusBar, Documents: extensionDocuments{w: w},
		Disabled: w.disabledExtensions, Files: w.extensionFiles, Post: w.onUI, Frame: w.extensionFrame, After: w.extensionAfter,
		Logger: p.Logger, AskSharing: w.askSharing,
		Views: w.Views, Keys: w.Keys, Markers: w.Markers, Channels: p.Channels,
		ShowChannel: w.showChannel, FocusView: w.focusExtensionView, FilesComplete: w.extensionFilesComplete,
		OpenURI: w.openURI, Editors: extEditorHost{w: w}, Languages: extLanguages{w: w}, EditorsChanged: w.editorsChanged,
		LSP: workspaceLSP{w: w},
	})
	w.extScope.Store(scope)
	// a change of extensions.disabled turns extensions off and on in this workspace
	w.stops = append(w.stops, w.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects(disabledExtensionsSetting, "") {
			w.onUI(scope.Refresh)
		}
	}))
	// a document that opens may be one that an extension waits for by its language (the documents of a command line are
	// opened after the workspace is made, so they are seen here too)
	w.stops = append(w.stops, w.Documents.WatchOpen(func(doc *document.Document) {
		w.onUI(func() { scope.Fire(extension.OnLanguage(doc.LanguageID())) })
	}))
	w.startCoreEvents(scope)
	w.watchExtensionFileEvents()
	scope.Start()
}

// stopExtensions closes the scope of the workspace: the extensions are deactivated and what they contributed is removed.
// The state their views saved is kept for the state file of the window, which may be written after this.
func (w *Workspace) stopExtensions() {
	w.stopCoreEvents()
	if scope := w.extScope.Swap(nil); scope != nil {
		if w.ext != nil {
			w.ext.state = scope.WindowTabState() // what the next write of the list of the tabs keeps
		}
		states := scope.ViewStates()
		scope.Close()
		w.extViewMu.Lock()
		w.extViewStates = states
		w.extViewMu.Unlock()
	}
}

// extensionViewStates is the state the views of the extensions saved: asked of the live scope, or what it saved before it
// closed.
func (w *Workspace) extensionViewStates() map[string]json.RawMessage {
	if scope := w.extScope.Load(); scope != nil {
		return scope.ViewStates()
	}
	w.extViewMu.Lock()
	defer w.extViewMu.Unlock()
	return w.extViewStates
}

// restoreExtensionViews gives the scope the state the views saved in the session before.
func (w *Workspace) restoreExtensionViews(states map[string]json.RawMessage) {
	if scope := w.extScope.Load(); scope != nil && len(states) > 0 {
		scope.SetViewStates(states)
	}
}

// showChannel selects an Output channel in the Output panel and shows the panel, for an extension's channel.
func (w *Workspace) showChannel(name string, preserveFocus bool) {
	if w.Output != nil {
		w.Output.SetActive(name)
	}
	w.Views.OpenPanel(command.ViewContainerOutput)
	if !preserveFocus {
		w.runLogged("workbench.action.focusPanel")
	}
}

// focusExtensionView gives the keyboard focus to the part a view of an extension is in.
func (w *Workspace) focusExtensionView(viewID string) {
	if c, ok := w.Views.Registry().View(viewID); ok {
		if container, ok := w.Views.Registry().Container(c.Container); ok && container.Location == command.ViewLocationPanel {
			w.runLogged("workbench.action.focusPanel")
			return
		}
		w.runLogged("workbench.action.focusSideBar")
	}
}

// runLogged runs a command of the workspace for an extension's view; a failure is logged.
func (w *Workspace) runLogged(id string) {
	if err := w.runCommand(id, nil); err != nil {
		w.Process.Logger.Warn("a command of an extension's view failed", "command", id, "err", err)
	}
}

// extensionFilesComplete tells whether the walk of the folder has finished.
func (w *Workspace) extensionFilesComplete() bool {
	if idx := w.FileIndex(); idx != nil {
		return idx.Snapshot().Complete
	}
	return true
}

// extensionFiles gives the files of the folder (paths relative to it) to the activation event onFile.
func (w *Workspace) extensionFiles() []string {
	if idx := w.FileIndex(); idx != nil {
		return idx.Snapshot().Files
	}
	return nil
}

// watchExtensionFiles makes a change of the list of files check the onFile events of the extensions, and tell them with
// emetos.files.changed that the list changed.
func (w *Workspace) watchExtensionFiles(idx *workspace.FileIndex) {
	idx.OnChange(func(workspace.IndexEvent) {
		if scope := w.extScope.Load(); scope != nil {
			w.onUI(func() {
				scope.CheckFiles()
				w.publishFiles(scope, []extension.FileChange{{Kind: extension.FileListed}})
			})
		}
	})
}

// publishFiles sends emetos.files.changed to the scope; a failure is logged (the payload is made here, so it is a bug).
func (w *Workspace) publishFiles(scope *exthost.Scope, changes []extension.FileChange) {
	if w.Folder == "" || len(changes) == 0 {
		return
	}
	if err := scope.PublishCore(extension.TopicFilesChanged, extension.FilesChanged{Changes: changes}); err != nil {
		w.Process.Logger.Error("a core event cannot be published", "topic", extension.TopicFilesChanged, "err", err)
	}
}

// watchExtensionFileEvents makes the batches of the watcher events of the folder reach the extensions as
// emetos.files.changed, with the paths relative to the folder. A batch of more than extension.MaxFileChanges is one rescan
// of the folder.
func (w *Workspace) watchExtensionFileEvents() {
	w.stops = append(w.stops, w.OnFileEvents(func(events []workspace.Event) {
		folder := w.Folder
		if folder == "" {
			return
		}
		var changes []extension.FileChange
		for _, e := range events {
			c, ok := fileChange(folder, e)
			if !ok {
				continue
			}
			changes = append(changes, c)
		}
		if len(changes) > extension.MaxFileChanges {
			changes = []extension.FileChange{{Kind: extension.FileRescan, Path: "."}}
		}
		if len(changes) == 0 {
			return
		}
		w.onUI(func() {
			if scope := w.extScope.Load(); scope != nil {
				w.publishFiles(scope, changes)
			}
		})
	}))
}

// fileChange is a watcher event as the extensions see it; false for an event outside the folder.
func fileChange(folder string, e workspace.Event) (extension.FileChange, bool) {
	rel := func(p string) (string, bool) {
		if p == "" {
			return "", true
		}
		if p == folder {
			return ".", true
		}
		if !isBelow(p, folder) {
			return "", false
		}
		r, err := filepath.Rel(folder, p)
		if err != nil {
			return "", false
		}
		return filepath.ToSlash(r), true
	}
	path, ok := rel(e.Path)
	if !ok || path == "" {
		return extension.FileChange{}, false
	}
	old, _ := rel(e.OldPath)
	kind := extension.FileChanged
	switch e.Kind {
	case workspace.EventCreated:
		kind = extension.FileCreated
	case workspace.EventDeleted:
		kind = extension.FileDeleted
	case workspace.EventRenamed:
		kind = extension.FileRenamed
	case workspace.EventRescan:
		kind = extension.FileRescan
	}
	return extension.FileChange{Kind: kind, Path: path, OldPath: old}, true
}

// extensionDocuments is what the workspace offers its extensions of its documents.
type extensionDocuments struct{ w *Workspace }

// Read implements exthost.DocumentSource: the text of an open document, with its unsaved changes, or a file of the
// folder of the workspace. The error has the tag ExtDocumentOutsideFolder, or those of document.ParseURI (URIInvalid) and, for a
// file that cannot be read, of fileio.Read.
func (d extensionDocuments) Read(ctx context.Context, uri string) (string, error) {
	u, err := document.ParseURI(uri)
	if err != nil {
		return "", err
	}
	if doc, ok := d.w.Documents.Get(u); ok {
		return doc.Text(), nil
	}
	folder := d.w.Folder
	if u.Scheme != document.SchemeFile || folder == "" || !isBelow(filepath.Clean(u.Path), folder) {
		return "", errors.New("ExtDocumentOutsideFolder", "the document is not open and is not a file of the folder of the workspace", "uri", uri)
	}
	res, err := fileio.Read(ctx, filepath.Clean(u.Path), fileio.ReadOptions{})
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

// Open implements exthost.DocumentSource.
func (d extensionDocuments) Open() []string {
	var out []string
	for _, doc := range d.w.Documents.All() {
		out = append(out, doc.URI().String())
	}
	return out
}

// Show implements exthost.DocumentShower: the file opens in the active group, as a click on a problem opens it.
func (d extensionDocuments) Show(uri string, line, column int, preview bool) error {
	u, err := document.ParseURI(uri)
	if err != nil {
		return err
	}
	if u.Scheme != document.SchemeFile {
		return errors.New("ExtDocumentShowUnsupported", "only a file can be shown from an extension", "uri", uri)
	}
	s := d.w.shell
	if s == nil || !s.Mounted() {
		return errors.New("ExtDocumentShowUnavailable", "the window tab is not on screen", "uri", uri)
	}
	s.openMarker(widgets.WidgetsBindingOf(s.Context()), problems.Marker{Resource: u.Path, StartLine: line, StartColumn: column, EndLine: line, EndColumn: column}, !preview)
	return nil
}

// setExtensionEnabled turns an extension on or off by writing extensions.disabled in the settings.json of the user. Every
// workspace follows the file, so the extension goes (or comes) in all the tabs of the profile.
func (w *Workspace) setExtensionEnabled(id string, enable bool) error {
	list := slices.Clone(w.disabledExtensions())
	has := slices.Contains(list, id)
	switch {
	case enable && has:
		list = slices.DeleteFunc(list, func(s string) bool { return s == id })
	case !enable && !has:
		list = append(list, id)
	default:
		return nil
	}
	slices.Sort(list)
	if len(list) == 0 {
		return w.Settings.Remove(w.ctx, disabledExtensionsSetting, settings.TargetUser)
	}
	return w.Settings.Update(w.ctx, disabledExtensionsSetting, toAnyList(list), settings.TargetUser)
}

// toAnyList is a list of strings as the settings store takes an array.
func toAnyList(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// editorsChanged makes the editor area build again: the title actions, the file views or the tabs of the extensions changed.
func (w *Workspace) editorsChanged() {
	w.onUI(func() {
		s := w.shell
		if s == nil || !s.Mounted() {
			return
		}
		s.SetState(func() {})
		s.refreshKeys()
	})
}

// openURI opens a URI with the desktop for an extension: the OpenURI portal, or the test seam of the workspace.
func (w *Workspace) openURI(ctx context.Context, uri string) error {
	open := w.OpenURI
	if open == nil {
		open = desktop.OpenURI
	}
	s := w.shell
	if s == nil || !s.Mounted() {
		return errors.New("ExtOpenURIUnavailable", "the window tab is not on screen", "uri", uri)
	}
	return open(ctx, s.Context(), uri)
}
