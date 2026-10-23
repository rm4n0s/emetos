package app

import (
	"context"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Window holds what one window owns (SPEC §5.1): the folder, the settings with the
// workspace layer, the root of the context keys, and the documents. The same file open in
// two windows is two documents. A Window has no Glutter types: widgets reach it through
// WindowScope.
type Window struct {
	// Process is the process the window belongs to.
	Process *Process
	// Folder is the folder open in the window, or empty.
	Folder string
	// Settings is the store of the user's and the workspace's settings.
	Settings *settings.Store
	// Keys is the root scope of the context keys of the window.
	Keys *contextkey.Scope
	// Documents are the documents open in the window.
	Documents *Documents
	// Keybindings resolve key presses in the window.
	Keybindings *keybinding.Resolver
	// Dispatcher turns key presses into commands: it keeps the chords of a sequence that
	// are pending (SPEC §5.4).
	Dispatcher *keybinding.Dispatcher
	// Scopes tells the context key scope of each focus node.
	Scopes *kit.FocusScopes
	// OnChordStatus is told the status message of a pending chord ("(Ctrl+K) was pressed.
	// Waiting for second key of chord...") and when it is cleared. The status bar shows it
	// (step 6.20); until then it is logged. It is called on the UI thread.
	OnChordStatus func(status string, cleared bool)
	// Clipboard is what the editors of the window remember of what they copied, so that a copy
	// in one editor pastes in another as it was made.
	Clipboard *editor.ClipboardMemory
	// Zoom is the zoom level of the font of the editors of the window.
	Zoom *editor.Zoom

	ctx context.Context
}

// chordStatus passes the status of a chord on, to OnChordStatus or the log.
func (w *Window) chordStatus(status string, cleared bool) {
	if w.OnChordStatus != nil {
		w.OnChordStatus(status, cleared)
		return
	}
	w.Process.Logger.Debug("chord status", "status", status, "cleared", cleared)
}

// NewWindow makes a window over a process, for a folder or for none. It reads the settings
// files; a file with errors does not fail the window (the store reports it as a problem,
// which is logged), but a file that cannot be read does.
func NewWindow(ctx context.Context, p *Process, folder string) (*Window, error) {
	userPath, err := settings.UserSettingsPath(p.Paths, storage.DefaultProfile)
	if err != nil {
		return nil, err
	}
	opts := settings.StoreOptions{UserPath: userPath}
	if folder != "" {
		opts.WorkspacePath = settings.WorkspaceSettingsPath(folder)
	}
	store := settings.NewStore(p.Settings, opts)
	if _, err := store.Reload(ctx); err != nil {
		return nil, err
	}
	for _, problem := range store.Problems() {
		p.Logger.Warn("settings problem", "target", problem.Target, "key", problem.Key, "message", problem.Message)
	}
	resolver := keybinding.NewResolver(p.Keymap.Defaults(), nil, nil)
	return &Window{
		Process:     p,
		Folder:      folder,
		Settings:    store,
		Keys:        contextkey.NewRoot(),
		Documents:   NewDocuments(),
		Keybindings: resolver,
		Dispatcher:  keybinding.NewDispatcher(resolver, time.Now),
		Scopes:      kit.NewFocusScopes(),
		Clipboard:   editor.NewClipboardMemory(),
		Zoom:        editor.NewZoom(),
		ctx:         ctx,
	}, nil
}

// Documents are the documents open in a window, by URI. It is safe for concurrent use,
// though documents themselves change on the UI thread only (SPEC §5.1).
type Documents struct {
	mu   sync.Mutex
	open map[document.URI]*document.Document
	// order is the URIs in the order they were opened.
	order []document.URI
}

// NewDocuments makes an empty set.
func NewDocuments() *Documents {
	return &Documents{open: map[document.URI]*document.Document{}}
}

// OpenFile returns the document of a file, reading it if it is not open yet. The error
// has the tags of fileio.Read.
func (d *Documents) OpenFile(ctx context.Context, path string, opts document.Options) (*document.Document, error) {
	uri := document.FileURI(path)
	d.mu.Lock()
	doc, ok := d.open[uri]
	d.mu.Unlock()
	if ok {
		return doc, nil
	}
	res, err := fileio.Read(ctx, path, fileio.ReadOptions{})
	if err != nil {
		return nil, err
	}
	doc = document.FromRead(uri, res, opts)
	d.mu.Lock()
	defer d.mu.Unlock()
	if existing, ok := d.open[uri]; ok {
		return existing, nil // opened meanwhile
	}
	d.open[uri] = doc
	d.order = append(d.order, uri)
	return doc, nil
}

// Get returns an open document.
func (d *Documents) Get(uri document.URI) (*document.Document, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	doc, ok := d.open[uri]
	return doc, ok
}

// All lists the open documents in the order they were opened.
func (d *Documents) All() []*document.Document {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*document.Document, len(d.order))
	for i, uri := range d.order {
		out[i] = d.open[uri]
	}
	return out
}

// Close forgets a document and reports whether it was open.
func (d *Documents) Close(uri document.URI) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.open[uri]; !ok {
		return false
	}
	delete(d.open, uri)
	for i, u := range d.order {
		if u == uri {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
	return true
}

// errNoWindow is the error of code that needs a window and has none.
func errNoWindow() error {
	return errors.New("WindowMissing", "no window is above this widget; wrap it in WindowScope")
}
