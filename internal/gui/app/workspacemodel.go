package app

import (
	"context"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/snippet"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/views/output"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// Workspace holds what one window owns (SPEC §5.1): the folder, the settings with the
// workspace layer, the root of the context keys, and the documents. The same file open in
// two windows is two documents. A Workspace has no Glutter types: widgets reach it through
// WindowScope.
type Workspace struct {
	// Process is the process the window belongs to.
	Process *Process
	// Commands is the registry of commands of the window: a fork of the registry of the process (SPEC §5.1), so
	// that the handlers the shell of a window sets (the editor in front of the user, the Explorer, the save
	// commands) are the window's and a command run in one window never reaches another.
	Commands *command.Registry
	// Notify is the notification service of the window: what the window reports is shown in the window (the
	// Process has one for what belongs to no window).
	Notify *notify.Service
	// Folder is the absolute path of the folder open in the window, or empty.
	Folder string
	// Workspace is the model of the folder: its tree, read lazily and kept up to date by the watcher
	// (SPEC §10.5). It is nil when no folder is open.
	Workspace *workspace.Workspace
	// Settings is the store of the user's and the workspace's settings. Its user layer is the settings.json of the
	// profile of the window (SwitchProfile moves it).
	Settings *settings.Store
	// Snippets are the snippet files of the profile of the window, in the snippets folder of its configuration
	// (SPEC §9.9, §10.12). The watcher of the window reloads them when they change; SwitchProfile gives the
	// service the folder of the other profile.
	Snippets *snippet.Service
	// Themes is the theme service of the window: it picks the color theme from the settings of the window and the
	// system's brightness (SPEC §8.4). Each window has its own, since the profiles of two windows may choose
	// different themes.
	Themes *theme.Service
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
	// OnChordStatus, when set, is told the status message of a pending chord ("(Ctrl+K) was pressed.
	// Waiting for second key of chord...") and when it is cleared, in place of the status bar showing
	// it (tests use it to watch the dispatcher). It is called on the UI thread.
	OnChordStatus func(status string, cleared bool)
	// StatusBar holds the items of the status bar of the window (SPEC §10.1): the shell adds those of
	// the editor, the problems and the notifications, and a later step adds its own.
	StatusBar *workbench.StatusBar
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
	// QuickInput is the quick input of the window: the command palette, go to line, the views and the
	// pickers (SPEC §10.6). The workbench shows it; the shell registers the providers of its modes in
	// QuickAccess, and CommandHistory is the commands run from the palette, kept in memory.
	QuickInput     *quickinput.Controller
	QuickAccess    *quickinput.Registry
	CommandHistory *quickinput.CommandsHistory
	// Recent are the files opened in the window, the most recent first: the quick open shows them first
	// (SPEC §10.6). They are part of the UI state of the window (SPEC §10.10); AddRecentFile also notes the
	// file in the list of File > Open Recent.
	Recent *workspace.RecentFiles
	// FileOps does the file operations of the Explorer and keeps their history for undo and redo
	// (SPEC §10.5). Its trash is the user's, in $XDG_DATA_HOME/Trash.
	FileOps *workspace.FileOps
	// Glutter: gluttertest.FakeDesktop cannot make the file chooser portal unavailable, so tests replace the dialog
	// to reach the prompt that takes its place; Glutter should provide a fake desktop without a file chooser.
	//
	// SaveDialog asks the user for the file to save a document in (Save As); it is the file chooser of the desktop
	// when nil (chooseSavePath). It is called on a goroutine and gives "" for a dialog that was cancelled. A
	// desktop with no file chooser gives an error whose cause has the tag FileChooserUnavailable, and the window
	// asks for the path in the quick input instead. Tests replace it.
	SaveDialog func(ctx context.Context, bc widgets.BuildContext, opts SaveDialogOptions) (string, error)
	// OpenURI opens a URI with the desktop; it is desktop.OpenURI when nil. Tests replace it.
	OpenURI func(ctx context.Context, bc widgets.BuildContext, uri string) error
	// Quit ends the application once the window has been closed with RequestClose and the unsaved changes were
	// answered; the application is asked to exit when it is nil. Tests replace it.
	Quit func()

	ctx context.Context
	// profileMu guards profile and profileListeners: the file watchers and the other windows read the profile from
	// their own goroutines.
	profileMu sync.RWMutex
	// profile is the profile of the window (storage.DefaultProfile, the empty string, for the default one); it is
	// changed on the UI thread only (SwitchProfile).
	profile          string
	profileListeners []*func(profile string)
	// tab is the window tab that holds the workspace, in the list of the tabs of the process (Workspaces); nil for a
	// workspace that nothing holds yet.
	tab *WindowTab
	// keyNode is the focus node at the root of the tree of the workspace (the key dispatcher's): a keyboard focus below
	// it is the workspace's, and only the active tab's dispatcher uses a key.
	keyNode *widgets.FocusNode
	// shell is the state of the shell that shows the window; nil until it is made and after it is gone.
	shell *shellState
	// settingsDocs lets the settings store read and edit the settings files that are open in an editor.
	settingsDocs *settingsDocs
	// uiModel is the model of the settings editor (SettingsModel).
	uiModel *settings.UIModel
	// userKeys is the keybindings.json of the user: the window reads it and the keybindings editor edits it.
	userKeys *userKeys
	// recorder is the recorder that the dispatcher hands the key presses to while it records keys (startRecording).
	recorder keybinding.Recorder
	// stops end what the window follows (the settings of the other windows, ...) when it is gone.
	stops []func()
	// controller is the controller of the platform window that shows this one, as the shell found it; nil
	// where there is none.
	controller any
	// onReady holds what waits for the shell to be on screen (whenReady).
	onReady []func()
	// emptyID is the state id the window has while it has no folder, when it was made with one (a window that comes
	// back from the list of the open windows, or a new one); empty lets the state store pick the first one.
	emptyID string
	// dropState is true for a window that is gone for good (an extra window without a folder that was closed):
	// its state is not written.
	dropState bool
	// stateID is the id of the window's UI state file (initState); restored is what PrepareRestore found, until
	// the shell takes it, pendingExpanded the folders the Explorer expands when its tree exists, and flushState
	// the function the shell sets to save the state when the window closes.
	stateID         string
	geometry        *storage.Geometry
	restored        *restoredWindow
	pendingExpanded []string
	flushState      func()
	// backups are the backups of the unsaved documents of the window (backup.go).
	backups backupState
	// closer is the shell's answer to the questions of closing, and exit ends the application without asking
	// again; both are set while the shell is on screen (closing.go).
	closer func(CloseReason, func(closeResult))
	exit   func()
	// launcher is the shell's answer to HandleLaunch, set while the shell is on screen (launch.go), and startup
	// is the command line of the first instance until the shell takes it.
	launcher func(args ipc.OpenArgs, done func(LaunchOutcome)) (cancel func())
	startup  *ipc.OpenArgs
	// icons is the file icon theme in use.
	icons fileIcons
	// files is the list of the files of the folder for the quick open; nil without a folder. It is made when
	// the folder opens and walked once the shell starts it (FileIndex).
	files *workspace.FileIndex
	// folderMu guards Folder, Workspace and files against the goroutines that read them (the watcher, the
	// settings listeners); the UI thread, which changes them, reads the fields as they are.
	folderMu sync.RWMutex
	// folderListeners are told when SetFolder changed the folder.
	folderListeners []*func()
	// eventMu guards eventListeners, which are told of the events of the watcher.
	eventMu        sync.Mutex
	eventListeners []*func([]workspace.Event)
	// watch is the state of the file watcher (StartWatching).
	watch watchState
	// zoom is the zoom level of the window (zoom.go), and modes its full screen mode (zen.go).
	zoom  zoomState
	modes modeState
	// title is the title of the window as the shell last made it.
	title string
	// after runs a function on the UI thread once a time has passed and returns the function that
	// cancels it; the shell sets it while it is on screen.
	after func(d time.Duration, f func()) (stop func())
	// statusMessage is the item of the status message, and statusMessageStop cancels the end of its time;
	// chordTimeout cancels the timer that forgets a pending chord.
	statusMessage     *workbench.StatusAccessor
	statusMessageStop func()
	chordTimeout      func()
}

// Title is the title of the window: window.title filled in for the active editor and the folder
// (SPEC §10.1). It is empty until the window has been shown.
func (w *Workspace) Title() string { return w.title }

// chordStatus shows the status of a chord in the status bar, as VS Code's status message: "(Ctrl+K) was
// pressed. Waiting for second key of chord..." stays until the chord is completed or aborted (or the
// dispatcher forgets it after keybinding.ChordTimeout), and "The key combination (Ctrl+K, X) is not a command."
// shows for ten seconds. OnChordStatus gets it instead when it is set.
func (w *Workspace) chordStatus(status string, cleared bool) {
	if w.OnChordStatus != nil {
		w.OnChordStatus(status, cleared)
		return
	}
	w.Process.Logger.Debug("chord status", "status", status, "cleared", cleared)
	if cleared {
		w.stopChordTimeout()
		w.ClearStatusMessage()
	}
	switch {
	case status == "":
	case cleared:
		w.ShowStatusMessage(status, failedChordMessageTime)
	default:
		w.ShowStatusMessage(status, 0)
		w.stopChordTimeout()
		if w.after != nil {
			w.chordTimeout = w.after(keybinding.ChordTimeout, func() {
				w.chordTimeout = nil
				w.Dispatcher.LeaveChordMode()
				w.ClearStatusMessage()
			})
		}
	}
}

// failedChordMessageTime is how long the message of a chord that is not a command shows (VS Code's 10 s).
const failedChordMessageTime = 10 * time.Second

func (w *Workspace) stopChordTimeout() {
	if w.chordTimeout != nil {
		w.chordTimeout()
		w.chordTimeout = nil
	}
}

// ShowStatusMessage shows a message at the end of the left side of the status bar, in place of the one it
// showed, and takes it away after hideAfter (never when it is zero). It is called on the UI thread.
//
// Ported from VS Code: src/vs/workbench/browser/parts/notifications/notificationsStatus.ts (doSetStatusMessage)
func (w *Workspace) ShowStatusMessage(text string, hideAfter time.Duration) {
	w.ClearStatusMessage()
	entry := workbench.StatusEntry{Name: "Status Message", Text: text, AriaLabel: text}
	w.statusMessage = w.StatusBar.Add("status.message", workbench.StatusLeft, math.Inf(-1), entry)
	if hideAfter > 0 && w.after != nil {
		w.statusMessageStop = w.after(hideAfter, func() {
			w.statusMessageStop = nil
			w.ClearStatusMessage()
		})
	}
}

// ClearStatusMessage takes the status message away.
func (w *Workspace) ClearStatusMessage() {
	if w.statusMessageStop != nil {
		w.statusMessageStop()
		w.statusMessageStop = nil
	}
	if w.statusMessage != nil {
		w.statusMessage.Dispose()
		w.statusMessage = nil
	}
}

// NewWorkspace makes a window over a process, for a folder or for none. It reads the settings
// files; a file with errors does not fail the window (the store reports it as a problem,
// which is logged), but a file that cannot be read does.
func NewWorkspace(ctx context.Context, p *Process, folder string) (*Workspace, error) {
	return newWorkspace(ctx, p, workspaceOptions{folder: folder})
}

// workspaceOptions are the parts of a window that NewWindow leaves to the process.
type workspaceOptions struct {
	// folder is the folder of the window, or empty.
	folder string
	// stateID is the id of the state of a window without a folder (a window that comes back from the list of open
	// windows, or a new one); empty lets the process pick one.
	stateID string
	// profile is the profile of the window, as storage.Profiles gives it (empty is the default profile).
	profile string
	// from is the window whose test seams (Quit, SaveDialog, OpenURI, OnChordStatus) the new one takes over: the
	// window it replaces.
	from *Workspace
}

// newWorkspace is NewWindow with options.
func newWorkspace(ctx context.Context, p *Process, o workspaceOptions) (*Workspace, error) {
	folder := o.folder
	userPath, err := settings.UserSettingsPath(p.Paths, o.profile)
	if err != nil {
		return nil, err
	}
	snippetsDir, err := snippetsDirOf(p.Paths, o.profile)
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
	access := quickinput.NewRegistry()
	w := &Workspace{
		QuickAccess:    access,
		QuickInput:     quickinput.NewController(quickinput.ControllerOptions{Registry: access}),
		CommandHistory: quickinput.NewCommandsHistory(settings.Get[int](store, "workbench.commandPalette.history", "")),
		Process:        p,
		Commands:       p.Commands.Fork(),
		Notify:         notify.NewService(p.Logger, nil),
		Folder:         folder,
		Settings:       store,
		Snippets:       snippet.NewService(snippetsDir),
		Themes:         p.newThemes(),
		profile:        o.profile,
		Keys:           contextkey.NewRoot(),
		Documents:      NewDocuments(),
		Keybindings:    resolver,
		Dispatcher:     keybinding.NewDispatcher(resolver, time.Now),
		Scopes:         kit.NewFocusScopes(),
		Clipboard:      editor.NewClipboardMemory(),
		Find:           &editor.FindMemory{},
		Zoom:           editor.NewZoom(),
		Layout:         layout,
		Views:          workbench.NewViews(p.Views, layout),
		Dialogs:        kit.NewDialogController(),
		Recent:         workspace.NewRecentFiles(0),
		FileOps:        workspace.NewFileOps(workspace.FileOpsOptions{Trash: fileio.NewHomeTrash(p.Paths.TrashDir())}),
		Markers:        problems.NewService(),
		StatusBar:      workbench.NewStatusBar(),
		ctx:            ctx,
	}
	w.Notifications = notify.NewPresenter(w.Notify)
	w.Documents.SetVirtual(p.Virtual)
	if folder == "" {
		w.emptyID = o.stateID
	}
	if f := o.from; f != nil {
		w.Quit, w.SaveDialog, w.OpenURI, w.OnChordStatus = f.Quit, f.SaveDialog, f.OpenURI, f.OnChordStatus
	}
	w.followZoom()
	w.syncLayoutKeys()
	w.Layout.OnChange(w.syncLayoutKeys)
	w.followZen()
	w.registerWindowCommands()
	w.settingsDocs = newSettingsDocs(w)
	if err := w.Snippets.Reload(ctx); err != nil {
		p.Logger.Warn("snippets", "err", err)
	}
	keysPath, err := keybinding.UserKeybindingsPath(p.Paths, o.profile)
	if err != nil {
		return nil, err
	}
	w.userKeys = newUserKeys(w, keysPath)
	w.Documents.onFile = w.followFile
	w.Documents.onClose = w.unfollowFile
	if folder != "" {
		if err := w.openWorkspace(ctx, folder); err != nil {
			return nil, err
		}
	}
	w.initState(ctx)
	w.syncWorkbenchState()
	w.settingsDocs.install()
	w.userKeys.reload(ctx, false)
	w.rememberSettingsPaths(userPath, p)
	w.followWorkspaceSettings()
	w.followTheme()
	w.followLanguages()
	return w, nil
}

// snippetsDirOf is the folder of the snippet files of a profile.
func snippetsDirOf(paths storage.Paths, profile string) (string, error) {
	dir, err := paths.ConfigDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "snippets"), nil
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
	// onOpen and onDrop are told of every document that is added to the set and every one that is taken out
	// of it (the backup tracker of the window follows them, SPEC §10.10).
	onOpen, onDrop func(doc *document.Document)
	// closeWatchers are told of every document that is closed (WatchClose).
	closeWatchers []*func(doc *document.Document)
	// openWatchers are told of every document that is opened (WatchOpen).
	openWatchers []*func(doc *document.Document)
	// virtual makes the content of the `emetos-virtual:` documents (SetVirtual).
	virtual *document.VirtualRegistry
}

// notifyOpen tells the watchers of a document that was added to the set.
func (d *Documents) notifyOpen(doc *document.Document) {
	d.mu.Lock()
	watchers := slices.Clone(d.openWatchers)
	d.mu.Unlock()
	for _, w := range watchers {
		(*w)(doc)
	}
}

// WatchOpen makes the set tell fn of every document that is added to it from now on, in the goroutine that opened
// it, and returns the function that stops it. The documents that are open already are not told (see All).
func (d *Documents) WatchOpen(fn func(doc *document.Document)) (stop func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := &fn
	d.openWatchers = append(d.openWatchers, h)
	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.openWatchers = slices.DeleteFunc(d.openWatchers, func(x *func(*document.Document)) bool { return x == h })
	}
}

// WatchClose makes the set tell fn of every document that is closed from now on, in the goroutine that closed it,
// and returns the function that stops it. The command line's --wait follows its files with it (SPEC §10.11).
func (d *Documents) WatchClose(fn func(doc *document.Document)) (stop func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := &fn
	d.closeWatchers = append(d.closeWatchers, h)
	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.closeWatchers = slices.DeleteFunc(d.closeWatchers, func(x *func(*document.Document)) bool { return x == h })
	}
}

// SetObservers makes the set tell open of every document that is added to it and drop of every document that is
// closed. Either may be nil. They are called in the goroutine that opened or closed the document.
func (d *Documents) SetObservers(open, drop func(doc *document.Document)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onOpen, d.onDrop = open, drop
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
	return &Documents{open: map[document.URI]*document.Document{}, virtual: document.NewVirtualRegistry()}
}

// SetVirtual makes the set open the `emetos-virtual:` documents through the providers of a registry (SPEC §7.1).
// A set starts with a registry that has no provider.
func (d *Documents) SetVirtual(r *document.VirtualRegistry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.virtual = r
}

// OpenVirtual returns the read-only document of an `emetos-virtual:` URI, with the content its provider makes now:
// a document that is open already gets the new content (when it differs; the undo history is empty, as the
// document is never edited). Call it on the thread that owns the documents. It fails with the tags
// VirtualURIUnknown and VirtualGenerateFailed.
func (d *Documents) OpenVirtual(ctx context.Context, uri document.URI) (*document.Document, error) {
	d.mu.Lock()
	registry := d.virtual
	d.mu.Unlock()
	content, err := registry.Generate(ctx, uri)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	if doc, ok := d.open[uri]; ok {
		d.mu.Unlock()
		if doc.Text() != content.Text {
			doc.SetText(content.Text)
		}
		return doc, nil
	}
	doc := document.NewVirtual(uri, content)
	d.open[uri] = doc
	d.order = append(d.order, uri)
	onOpen := d.onOpen
	d.mu.Unlock()
	d.track(doc)
	if onOpen != nil {
		onOpen(doc)
	}
	d.notifyOpen(doc)
	return doc, nil
}

// AcquireVirtual is OpenVirtual for a document that an editor is about to show: it counts the editor as a user of
// the document (see Acquire).
func (d *Documents) AcquireVirtual(ctx context.Context, uri document.URI) (*document.Document, error) {
	doc, err := d.OpenVirtual(ctx, uri)
	if err != nil {
		return nil, err
	}
	d.Retain(doc)
	return doc, nil
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
	onFile, onOpen := d.onFile, d.onOpen
	d.mu.Unlock()
	d.track(doc)
	if onFile != nil {
		onFile(path)
	}
	if onOpen != nil {
		onOpen(doc)
	}
	d.notifyOpen(doc)
	return doc, nil
}

// track makes the set follow the URI of a document it holds: a Save As, or a move of its file, gives it another key.
func (d *Documents) track(doc *document.Document) {
	doc.OnURIChange(func(old, nu document.URI) { d.rekey(doc, old, nu) })
}

// rekey moves a document to its new URI in the set and tells the hooks that follow the files: the file it
// had is not followed any more and the one it has now is.
func (d *Documents) rekey(doc *document.Document, old, nu document.URI) {
	d.mu.Lock()
	if cur, ok := d.open[old]; ok && cur == doc {
		delete(d.open, old)
		d.open[nu] = doc
		for i, u := range d.order {
			if u == old {
				d.order[i] = nu
				break
			}
		}
	}
	onFile, onClose := d.onFile, d.onClose
	d.mu.Unlock()
	if old.Scheme == document.SchemeFile && onClose != nil {
		onClose(old.Path)
	}
	if nu.Scheme == document.SchemeFile && onFile != nil {
		onFile(nu.Path)
	}
}

// NewUntitled makes a document that has no file: the lowest free number gives its URI (untitled:1 is shown as
// Untitled-1; a number that was freed by a closed document is used again). It is empty, in the language and the
// encoding the options and the window's settings say, and counts as saved while it has no text.
func (d *Documents) NewUntitled(opts document.Options, encoding string) *document.Document {
	d.mu.Lock()
	uris := make([]document.URI, 0, len(d.open))
	for u := range d.open {
		uris = append(uris, u)
	}
	uri := document.LowestFreeUntitled(uris)
	policy := d.indentation
	doc := document.New(uri, "", opts)
	if encoding != "" {
		// an encoding that is not known keeps UTF-8: the setting is checked where it is read
		_ = doc.SetSaveEncoding(encoding)
	}
	d.open[uri] = doc
	d.order = append(d.order, uri)
	onOpen := d.onOpen
	d.mu.Unlock()
	if policy != nil {
		p := policy(doc.LanguageID())
		doc.SetIndentation(p.TabSize, p.InsertSpaces)
	}
	d.track(doc)
	if onOpen != nil {
		onOpen(doc)
	}
	d.notifyOpen(doc)
	return doc
}

// OpenBlank adds an empty document with the URI to the set, for the content of a backup to be put into
// (hot exit, SPEC §10.10): an untitled document with its own number, or a file that is gone. It has the
// indentation the window's policy gives its language. A document with that URI that is open already is
// returned as it is, with false.
func (d *Documents) OpenBlank(uri document.URI, opts document.Options) (doc *document.Document, added bool) {
	d.mu.Lock()
	if existing, ok := d.open[uri]; ok {
		d.mu.Unlock()
		return existing, false
	}
	doc = document.New(uri, "", opts)
	policy := d.indentation
	d.open[uri] = doc
	d.order = append(d.order, uri)
	onFile, onOpen := d.onFile, d.onOpen
	d.mu.Unlock()
	if policy != nil {
		p := policy(doc.LanguageID())
		doc.SetIndentation(p.TabSize, p.InsertSpaces)
	}
	d.track(doc)
	if onFile != nil && uri.Scheme == document.SchemeFile {
		onFile(uri.Path)
	}
	if onOpen != nil {
		onOpen(doc)
	}
	d.notifyOpen(doc)
	return doc, true
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
	d.mu.Unlock()
	doc.SetURI(uri) // the set follows it (track)
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
	doc, ok := d.open[uri]
	if !ok {
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
	onClose, onDrop := d.onClose, d.onDrop
	watchers := slices.Clone(d.closeWatchers)
	d.mu.Unlock()
	if onClose != nil && uri.Scheme == document.SchemeFile {
		onClose(uri.Path)
	}
	if onDrop != nil {
		onDrop(doc)
	}
	for _, w := range watchers {
		(*w)(doc)
	}
	return true
}
