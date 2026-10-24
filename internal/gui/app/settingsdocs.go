package app

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

// Settings in an open editor (SPEC §11.1): a settings.json that has a document open in the window is read and
// edited through the document, not the file, as VS Code's JSONEditingService does. A change from the settings
// editor (or the Auto Save toggle) is an edit of the document, which is saved when the document was not dirty before
// it; a dirty document stays dirty and the file is left alone. The store reads the document's text for the layer.
//
// Ported from VS Code: src/vs/workbench/services/configuration/common/jsonEditingService.ts (write: the model is
// edited, and saved only when it was not dirty)

// settingsDocs is the settings.TextSource of the store of a window, over the documents of the window. It keeps a
// copy of the text of each document that shows a settings file, updated on every change of the document, so that
// the store can read it from the goroutine of the file watcher.
type settingsDocs struct {
	win *Window

	mu    sync.Mutex
	texts map[string]string
	docs  map[string]*document.Document
	stops map[*document.Document][]func()
}

func newSettingsDocs(win *Window) *settingsDocs {
	return &settingsDocs{win: win, texts: map[string]string{}, docs: map[string]*document.Document{}, stops: map[*document.Document][]func(){}}
}

// install makes the store use the documents and follows the documents that open and close.
func (d *settingsDocs) install() {
	d.win.Settings.SetTextSource(d)
	d.win.stops = append(d.win.stops,
		d.win.Documents.WatchOpen(d.opened),
		d.win.Documents.WatchClose(d.closed))
	d.rescan()
}

// wants tells whether a path is one of the settings files of the store.
func (d *settingsDocs) wants(path string) bool {
	user, workspace := d.win.Settings.Paths()
	path = filepath.Clean(path)
	return path != "." && ((user != "" && filepath.Clean(user) == path) || (workspace != "" && filepath.Clean(workspace) == path))
}

// rescan follows the documents that show a settings file and lets go of those that do not any more (the folder of
// the window changed).
func (d *settingsDocs) rescan() {
	d.mu.Lock()
	before := len(d.docs)
	var drop []*document.Document
	for path, doc := range d.docs {
		if !d.wants(path) {
			drop = append(drop, doc)
		}
	}
	d.mu.Unlock()
	for _, doc := range drop {
		d.untrack(doc)
	}
	for _, doc := range d.win.Documents.All() {
		d.opened(doc)
	}
	d.mu.Lock()
	followsMore := len(d.docs) > before-len(drop)
	d.mu.Unlock()
	if followsMore {
		d.reload()
	}
}

// opened follows a document that was opened, when it shows a settings file.
func (d *settingsDocs) opened(doc *document.Document) {
	uri := doc.URI()
	if uri.Scheme != document.SchemeFile || !d.wants(uri.Path) {
		return
	}
	path := filepath.Clean(uri.Path)
	d.mu.Lock()
	if _, ok := d.stops[doc]; ok {
		d.mu.Unlock()
		return
	}
	d.docs[path], d.texts[path] = doc, doc.Text()
	d.stops[doc] = []func(){
		doc.Subscribe(func(document.ChangeEvent) { d.changed(path, doc) }),
		// a Save As gives the document another file: it is not a settings file any more
		doc.OnURIChange(func(_, _ document.URI) { d.untrack(doc) }),
	}
	d.mu.Unlock()
}

// closed lets go of a document that was closed.
func (d *settingsDocs) closed(doc *document.Document) { d.untrack(doc) }

func (d *settingsDocs) untrack(doc *document.Document) {
	d.mu.Lock()
	stops, ok := d.stops[doc]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.stops, doc)
	for path, x := range d.docs {
		if x == doc {
			delete(d.docs, path)
			delete(d.texts, path)
		}
	}
	d.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
	// the layer follows the file again
	d.reload()
}

// changed keeps the text of a document and, when the document is not dirty any more (it was reverted, reloaded, or
// an undo came back to the saved text), reads the settings again. A dirty document is read when the store edits it
// or reloads for another reason, not at every key: VS Code does not apply what is typed in settings.json before it
// is saved either.
func (d *settingsDocs) changed(path string, doc *document.Document) {
	d.mu.Lock()
	d.texts[path] = doc.Text()
	d.mu.Unlock()
	if !doc.Dirty() {
		d.reload()
	}
}

func (d *settingsDocs) reload() {
	w := d.win
	if _, err := w.Settings.Reload(w.ctx); err != nil {
		w.Process.Logger.Warn("reading the settings again failed", "err", err)
	}
}

// OpenText implements settings.TextSource.
func (d *settingsDocs) OpenText(path string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	text, ok := d.texts[filepath.Clean(path)]
	return text, ok
}

// EditText implements settings.TextSource: one undo step in the document, and a save when it was not dirty.
func (d *settingsDocs) EditText(ctx context.Context, path string, edits []jsonc.Edit) (bool, error) {
	d.mu.Lock()
	doc := d.docs[filepath.Clean(path)]
	d.mu.Unlock()
	if doc == nil {
		return false, nil
	}
	snap := doc.Snapshot()
	batch := make([]document.Edit, len(edits))
	for i, e := range edits {
		batch[i] = document.Edit{
			Range: buffer.Range{Start: snap.PositionAt(e.Offset), End: snap.PositionAt(e.Offset + e.Length)},
			Text:  e.Content,
		}
	}
	wasDirty := doc.Dirty()
	doc.PushStackElement()
	_, err := doc.Edit(batch, document.EditOptions{})
	doc.PushStackElement()
	if err != nil {
		return false, err
	}
	if wasDirty {
		return true, nil
	}
	return true, d.win.saveSettingsDocument(ctx, doc)
}

// saveSettingsDocument saves a settings document the way Ctrl+S does (the save participants of its language run,
// a failure is reported by the window); a window without a shell on screen writes it at once.
func (w *Window) saveSettingsDocument(ctx context.Context, doc *document.Document) error {
	if s := w.shell; s != nil && s.Mounted() {
		s.saveDocument(doc, saveRequest{}, nil)
		return nil
	}
	_, err := doc.Save(ctx, document.SaveOptions{DetectLanguage: w.Process.Languages.Detect})
	return err
}
