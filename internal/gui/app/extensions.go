package app

import (
	"context"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
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
		Info: extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: w.extensionName(), Folder: w.Folder},
		Ctx:  w.ctx, Commands: w.Commands, Menus: w.Menus, Keybindings: w.extKeys, Settings: w.Settings,
		Notify: w.Notify, StatusBar: w.StatusBar, Documents: extensionDocuments{w: w},
		Disabled: w.disabledExtensions, Files: w.extensionFiles, Post: w.onUI, Frame: w.extensionFrame, After: w.extensionAfter,
		Logger: p.Logger,
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
	scope.Start()
}

// stopExtensions closes the scope of the workspace: the extensions are deactivated and what they contributed is removed.
func (w *Workspace) stopExtensions() {
	w.stopCoreEvents()
	if scope := w.extScope.Swap(nil); scope != nil {
		scope.Close()
	}
}

// extensionFiles gives the files of the folder (paths relative to it) to the activation event onFile.
func (w *Workspace) extensionFiles() []string {
	if idx := w.FileIndex(); idx != nil {
		return idx.Snapshot().Files
	}
	return nil
}

// watchExtensionFiles makes a change of the list of files check the onFile events of the extensions.
func (w *Workspace) watchExtensionFiles(idx *workspace.FileIndex) {
	idx.OnChange(func(workspace.IndexEvent) {
		if scope := w.extScope.Load(); scope != nil {
			w.onUI(scope.CheckFiles)
		}
	})
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
