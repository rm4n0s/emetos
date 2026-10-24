package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/views/output"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// Window holds what one window owns (SPEC §5.1): the folder, the settings with the
// workspace layer, the root of the context keys, and the documents. The same file open in
// two windows is two documents. A Window has no Glutter types: widgets reach it through
// WindowScope.
type Window struct {
	// Process is the process the window belongs to.
	Process *Process
	// Folder is the absolute path of the folder open in the window, or empty.
	Folder string
	// Workspace is the model of the folder: its tree, read lazily and kept up to date by the watcher
	// (SPEC §10.5). It is nil when no folder is open.
	Workspace *workspace.Workspace
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
	// Find is what the editors of the window share of their searches: the switches of the find
	// widget.
	Find *editor.FindMemory
	// Zoom is the zoom level of the font of the editors of the window.
	Zoom *editor.Zoom
	// Layout is the layout of the workbench of the window: which parts are shown, their sizes and
	// where they are (SPEC §10.1). It follows the settings of the layout.
	Layout *workbench.Layout
	// Views is which view container the side bar of the window shows and which views are collapsed
	// (SPEC §10.4).
	Views *workbench.Views
	// Notifications is what the window shows of the notifications of the process: the toasts, the
	// notification center and the do not disturb mode (SPEC §10.7). The workbench binds it to the UI
	// thread.
	Notifications *notify.Presenter
	// Markers holds the problems that producers (language servers, tasks) report for the files of the
	// window; the Problems panel shows them (SPEC §10.13).
	Markers *problems.Service
	// Problems and Output are the views of the Problems and Output panels. The shell makes them when
	// the window is shown, and they are nil before.
	Problems *problems.View
	Output   *output.View
	// Dialogs queues the dialogs of the window. The workbench shows them over everything, and the key
	// dispatcher runs no command while one is showing.
	Dialogs *kit.DialogController
	// FileOps does the file operations of the Explorer and keeps their history for undo and redo
	// (SPEC §10.5). Its trash is the user's, in $XDG_DATA_HOME/Trash.
	FileOps *workspace.FileOps
	// OpenURI opens a URI with the desktop; it is desktop.OpenURI when nil. Tests replace it.
	OpenURI func(ctx context.Context, bc widgets.BuildContext, uri string) error

	ctx context.Context
	// icons is the file icon theme in use.
	icons fileIcons
	// folderMu guards Folder and Workspace against the goroutines that read them (the watcher, the
	// settings listeners); the UI thread, which changes them, reads the fields as they are.
	folderMu sync.RWMutex
	// folderListeners are told when SetFolder changed the folder.
	folderListeners []*func()
	// eventMu guards eventListeners, which are told of the events of the watcher.
	eventMu        sync.Mutex
	eventListeners []*func([]workspace.Event)
	// watch is the state of the file watcher (StartWatching).
	watch watchState
	// title is the title of the window as the shell last made it.
	title string
}

// Title is the title of the window: window.title filled in for the active editor and the folder
// (SPEC §10.1). It is empty until the window has been shown.
func (w *Window) Title() string { return w.title }

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
	layout := workbench.NewLayout(workbench.LayoutStateFromSettings(store))
	w := &Window{
		Process:       p,
		Folder:        folder,
		Settings:      store,
		Keys:          contextkey.NewRoot(),
		Documents:     NewDocuments(),
		Keybindings:   resolver,
		Dispatcher:    keybinding.NewDispatcher(resolver, time.Now),
		Scopes:        kit.NewFocusScopes(),
		Clipboard:     editor.NewClipboardMemory(),
		Find:          &editor.FindMemory{},
		Zoom:          editor.NewZoom(),
		Layout:        layout,
		Views:         workbench.NewViews(p.Views, layout),
		Dialogs:       kit.NewDialogController(),
		FileOps:       workspace.NewFileOps(workspace.FileOpsOptions{Trash: fileio.NewHomeTrash(p.Paths.TrashDir())}),
		Markers:       problems.NewService(),
		Notifications: notify.NewPresenter(p.Notify),
		ctx:           ctx,
	}
	w.Documents.onFile = w.followFile
	w.Documents.onClose = w.unfollowFile
	if folder != "" {
		if err := w.openWorkspace(ctx, folder); err != nil {
			return nil, err
		}
	}
	w.syncWorkbenchState()
	w.rememberSettingsPaths(userPath, p)
	w.followWorkspaceSettings()
	w.followTheme()
	w.followLanguages()
	return w, nil
}

// Documents are the documents open in a window, by URI. It is safe for concurrent use,
// though documents themselves change on the UI thread only (SPEC §5.1).
type Documents struct {
	mu   sync.Mutex
	open map[document.URI]*document.Document
	// detect gives the language of a file from its path and first line; nil leaves the language
	// the options say.
	detect func(path, firstLine string) string
	// order is the URIs in the order they were opened.
	order []document.URI
	// indentation gives what a file opens with; nil leaves the options of OpenFile.
	indentation func(languageID string) Indentation
	// largeFiles tells whether large file mode applies (editor.largeFileOptimizations); nil is yes.
	largeFiles func() bool
	// announced are the large documents the user was told about.
	announced map[*document.Document]bool
	// refs counts the editors that show each document (Acquire, Retain, Release).
	refs map[*document.Document]int
	// onFile and onClose are told the path of a file that was opened and of one that was closed;
	// the window follows the files that are outside its folder with them.
	onFile, onClose func(path string)
}

// SetLargeFileOptimizations makes OpenFile open large files in large file mode only while enabled
// returns true (editor.largeFileOptimizations).
func (d *Documents) SetLargeFileOptimizations(enabled func() bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.largeFiles = enabled
}

// AnnounceLarge tells whether the user has still to be told that a document is in large file mode:
// true the first time it is asked of a large document, false after it and for other documents.
func (d *Documents) AnnounceLarge(doc *document.Document) bool {
	if !doc.LargeFile() {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.announced[doc] {
		return false
	}
	if d.announced == nil {
		d.announced = map[*document.Document]bool{}
	}
	d.announced[doc] = true
	return true
}

// Indentation is how a new document is indented: editor.tabSize and editor.insertSpaces for its
// language, and whether the file's own indentation overrides them (editor.detectIndentation).
type Indentation struct {
	TabSize      int
	InsertSpaces bool
	Detect       bool
}

// SetIndentation makes OpenFile give a document the indentation that policy says for its
// language, found from its text when that says so.
func (d *Documents) SetIndentation(policy func(languageID string) Indentation) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.indentation = policy
}

// docLines reads the lines of a document for indent.Guess.
type docLines struct{ doc *document.Document }

func (l docLines) LineCount() int { return l.doc.Snapshot().LineCount() }

func (l docLines) LineContent(line int) string {
	s, _ := l.doc.Snapshot().LineContent(line)
	return s
}

// SetLanguageDetector makes OpenFile give a document the language that detect says for its path
// and first line, unless the options of OpenFile name one.
func (d *Documents) SetLanguageDetector(detect func(path, firstLine string) string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.detect = detect
}

// NewDocuments makes an empty set.
func NewDocuments() *Documents {
	return &Documents{open: map[document.URI]*document.Document{}}
}

// OpenFile returns the document of a file, reading it if it is not open yet. The error
// has the tags of fileio.Read.
func (d *Documents) OpenFile(ctx context.Context, path string, opts document.Options) (*document.Document, error) {
	return d.OpenFileWith(ctx, path, opts, fileio.ReadOptions{})
}

// OpenFileWith is OpenFile with the options of the read: AllowBinary opens a file that looks
// binary ("Open Anyway"). A file that is open already is returned as it is.
func (d *Documents) OpenFileWith(ctx context.Context, path string, opts document.Options, read fileio.ReadOptions) (*document.Document, error) {
	uri := document.FileURI(path)
	d.mu.Lock()
	doc, ok := d.open[uri]
	d.mu.Unlock()
	if ok {
		return doc, nil
	}
	res, err := fileio.Read(ctx, path, read)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	if d.largeFiles != nil && !d.largeFiles() {
		res.LargeFile = false
	}
	detect := d.detect
	d.mu.Unlock()
	if detect != nil && (opts.LanguageID == "" || opts.LanguageID == document.DefaultLanguage) {
		firstLine, _, _ := strings.Cut(res.Text, "\n")
		opts.LanguageID = detect(path, firstLine)
	}
	doc = document.FromRead(uri, res, opts)
	d.mu.Lock()
	policy := d.indentation
	d.mu.Unlock()
	if policy != nil {
		p := policy(doc.LanguageID())
		doc.SetIndentation(p.TabSize, p.InsertSpaces)
		if p.Detect {
			g := indent.Guess(docLines{doc}, p.TabSize, p.InsertSpaces)
			doc.SetIndentation(g.TabSize, g.InsertSpaces)
		}
	}
	d.mu.Lock()
	if existing, ok := d.open[uri]; ok {
		d.mu.Unlock()
		return existing, nil // opened meanwhile
	}
	d.open[uri] = doc
	d.order = append(d.order, uri)
	onFile := d.onFile
	d.mu.Unlock()
	if onFile != nil {
		onFile(path)
	}
	return doc, nil
}

// Acquire is OpenFile for a document that an editor is about to show: it counts the editor as a user
// of the document, which stays open until every user has called Release. OpenFile counts nobody, so a
// document opened with it is never closed by Release unless an editor retained it.
func (d *Documents) Acquire(ctx context.Context, path string, opts document.Options) (*document.Document, error) {
	return d.AcquireWith(ctx, path, opts, fileio.ReadOptions{})
}

// AcquireWith is Acquire with the options of the read (see OpenFileWith).
func (d *Documents) AcquireWith(ctx context.Context, path string, opts document.Options, read fileio.ReadOptions) (*document.Document, error) {
	doc, err := d.OpenFileWith(ctx, path, opts, read)
	if err != nil {
		return nil, err
	}
	d.Retain(doc)
	return doc, nil
}

// Retain counts one more user of an open document.
func (d *Documents) Retain(doc *document.Document) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refs == nil {
		d.refs = map[*document.Document]int{}
	}
	d.refs[doc]++
}

// Release ends one use of a document. The last release closes it (see Close) and reports true; a
// document nobody retained is left as it is.
func (d *Documents) Release(doc *document.Document) (closed bool) {
	d.mu.Lock()
	n := d.refs[doc]
	if n == 0 {
		d.mu.Unlock()
		return false
	}
	if n > 1 {
		d.refs[doc] = n - 1
		d.mu.Unlock()
		return false
	}
	delete(d.refs, doc)
	delete(d.announced, doc)
	d.mu.Unlock()
	return d.Close(doc.URI())
}

// Refs is how many users a document has.
func (d *Documents) Refs(doc *document.Document) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.refs[doc]
}

// Rename gives an open document the URI of the file it moved to (SPEC §10.5): the document keeps its text,
// history and unsaved changes. It reports false when the document is not open or another document is open
// at the new path. The watch of a file outside the folder follows the move.
func (d *Documents) Rename(doc *document.Document, newPath string) bool {
	old := doc.URI()
	uri := document.FileURI(newPath)
	d.mu.Lock()
	if cur, ok := d.open[old]; !ok || cur != doc {
		d.mu.Unlock()
		return false
	}
	if other, ok := d.open[uri]; ok && other != doc {
		d.mu.Unlock()
		return false
	}
	delete(d.open, old)
	d.open[uri] = doc
	for i, u := range d.order {
		if u == old {
			d.order[i] = uri
			break
		}
	}
	onFile, onClose := d.onFile, d.onClose
	d.mu.Unlock()
	doc.SetURI(uri)
	if old.Scheme == document.SchemeFile && onClose != nil {
		onClose(old.Path)
	}
	if onFile != nil {
		onFile(newPath)
	}
	return true
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
	if _, ok := d.open[uri]; !ok {
		d.mu.Unlock()
		return false
	}
	delete(d.open, uri)
	for i, u := range d.order {
		if u == uri {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
	onClose := d.onClose
	d.mu.Unlock()
	if onClose != nil && uri.Scheme == document.SchemeFile {
		onClose(uri.Path)
	}
	return true
}

// errNoWindow is the error of code that needs a window and has none.
func errNoWindow() error {
	return errors.New("WindowMissing", "no window is above this widget; wrap it in WindowScope")
}
