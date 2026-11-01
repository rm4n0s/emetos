package extension

import (
	"context"
	"encoding/json"
	"log/slog"
)

// API is what an extension may do in one scope. It is bound to the scope and to the activation: after the extension is
// deactivated every method that changes something fails. The methods may be called from the goroutines the extension
// started, except Documents().Read, which reads the text of a document and belongs to the UI thread (use Post).
type API interface {
	// Scope describes the scope the API is bound to.
	Scope() ScopeInfo
	// Log is the logger of the extension: its lines carry the id of the extension and the scope, and go to the log of
	// Emetos.
	Log() *slog.Logger
	// Commands registers commands and their handlers.
	Commands() Commands
	// Keybindings adds default keybindings.
	Keybindings() Keybindings
	// Menus adds menu items.
	Menus() Menus
	// Settings registers and reads settings.
	Settings() Settings
	// Notifications shows notifications.
	Notifications() Notifications
	// StatusBar adds items to the status bar of the scope.
	StatusBar() StatusBar
	// Documents reads the documents of the scope.
	Documents() Documents
	// Views gives the views the manifest declares (side bar and panel) their content.
	Views() Views
	// Output makes channels of the Output panel.
	Output() Output
	// Problems sets markers in the Problems panel.
	Problems() Problems
	// Workspace reads the folder of the workspace.
	Workspace() Workspace
	// Context sets context keys.
	Context() ContextKeys
	// Editors makes the tabs of the editor area of the kinds the manifest declares.
	Editors() Editors
	// FileViews gives the file views the manifest declares their content.
	FileViews() FileViews
	// Languages gives the grammars of the languages, for highlighting.
	Languages() Languages
	// LanguageServers follows and controls the language servers the manifest declares.
	LanguageServers() LanguageServers
	// QuickPick shows a list to choose from, the way the command palette does.
	QuickPick() QuickPick
	// InputBox asks the user for a line of text, the way the rename and the Save As of the window do.
	InputBox() InputBox
	// Banners shows banners across the top of the window tab of the scope.
	Banners() Banners
	// FileChooser asks the user for a file with the file dialog of the desktop.
	FileChooser() FileChooser
	// Clipboard writes text to the clipboard of the desktop.
	Clipboard() Clipboard
	// WindowTabs gives the content of the window tabs of the kinds the manifest declares; it works in the scope of such a
	// tab only.
	WindowTabs() WindowTabs
	// Events publishes and subscribes to events and sends requests to the other extensions of the scope.
	Events() Events
	// SourceControl creates source controls: the repositories, groups of changes and commit input of the Source Control view
	// (SPEC §10.30).
	SourceControl() SourceControls
	// Decorations registers the providers of the colors and badges of files (SPEC §10.30 "File decorations and the Explorer").
	Decorations() Decorations
	// Timeline registers the providers of the history of files that the Timeline view lists (SPEC §10.30 "The Timeline view").
	Timeline() Timelines
	// Dialogs asks the user a question in a modal dialog.
	Dialogs() Dialogs
	// EditorDecorations draws texts after the end of lines in the editors (SPEC §10.30 "Blame").
	EditorDecorations() EditorDecorations
	// TerminalEnvironment adds variables to the environment of the terminals the Terminal extension starts (SPEC §10.30 "Passwords and
	// editors").
	TerminalEnvironment() TerminalEnvironment
	// Origins tells where the command being handled came from, when it was run in the view of a client (SPEC §10.18, D50).
	Origins() Origins
	// Post runs fn on the UI thread, later; it is how a goroutine that the extension started hands its result to the
	// editor. fn is not run when the extension was deactivated before its turn comes, and a panic in it deactivates the
	// extension. Activate, command handlers, the callbacks of the API and the event handlers run on the UI thread already.
	Post(fn func())
}

// CommandSpec declares a command.
type CommandSpec struct {
	// ID is the id of the command, unique in Emetos (the editor's commands are taken): hello.greet.
	ID string
	// Title is the name in the command palette; a command with none is not listed there.
	Title string
	// Category groups commands in the palette ("Hello: Greet").
	Category string
	// Description says what the command does, for the keyboard shortcuts editor.
	Description string
	// When is the condition, a when-clause over the context keys, under which the command is enabled; empty is always.
	When string
	// Icon is the name of the Codicon ("add", "discard") the command shows as where a menu draws icons in place of titles: the
	// inline actions and the title actions of the Source Control view (VS Code's command icon). Empty for none.
	Icon string
}

// CommandHandler runs a command. ctx is cancelled when the command's work is no longer wanted, and args is a
// JSON-compatible value or nil. A returned error is the error of the command; a panic deactivates the extension.
type CommandHandler func(ctx context.Context, args any) error

// Commands registers the commands of an extension.
type Commands interface {
	// Handle sets the handler of a command that the Manifest declares. It fails when the extension did not declare it.
	Handle(id string, handler CommandHandler) error
	// Register adds a command that the Manifest did not declare, with its handler. It fails when the id is taken. A
	// command added here is not in the palette before the extension is activated.
	Register(spec CommandSpec, handler CommandHandler) error
}

// Keybinding binds a key to a command.
type Keybinding struct {
	// Command is the id of the command.
	Command string
	// Key is the key in the syntax of keybindings.json: ctrl+alt+h, or a chord such as ctrl+k ctrl+h.
	Key string
	// When is a when-clause over the context keys; empty is always. Use the context keys of the extension itself or
	// those of the editor.
	When string
	// Args are the JSON-compatible arguments the command is run with.
	Args any
}

// Keybindings adds the default keybindings of an extension; the user's keybindings.json still wins.
type Keybindings interface {
	// Add binds the key.
	Add(binding Keybinding) error
}

// The ids of the menus an extension may add items to.
const (
	// MenuCommandPalette is the command palette; an item whose When does not hold hides its command there.
	MenuCommandPalette = "CommandPalette"
	// MenuFile, MenuEdit, MenuSelection, MenuView, MenuGo and MenuHelp are the menus of the menu bar.
	MenuFile      = "MenubarFileMenu"
	MenuEdit      = "MenubarEditMenu"
	MenuSelection = "MenubarSelectionMenu"
	MenuView      = "MenubarViewMenu"
	MenuGo        = "MenubarGoMenu"
	MenuHelp      = "MenubarHelpMenu"
	// MenuTerminal is the Terminal menu of the menu bar, which has an item only when an extension adds one.
	MenuTerminal = "MenubarTerminalMenu"
	// MenuManage is the Manage menu: the gear at the bottom of the activity bar.
	MenuManage = "GlobalActivity"
	// MenuEditorContext is the context menu of the text editor.
	MenuEditorContext = "EditorContext"
	// MenuExplorerContext is the context menu of the Explorer. An item of an extension in it runs its command with
	// the arguments ExplorerArgs describes (ResourceURIs reads them), and its when-clause can use resourceLangId,
	// resourceFilename, resourceExtname and resourcePath of the row that was pressed.
	MenuExplorerContext = "ExplorerContext"
)

// MenuItem is an item of a menu.
type MenuItem struct {
	// Menu is the id of the menu, one of the Menu constants or the ID of a submenu the extension declares
	// (Contributes.Submenus).
	Menu string
	// Command is the id of the command the item runs. An item whose command does not exist is not shown.
	Command string
	// Submenu, instead of Command, makes the item open the submenu with this ID, which Contributes.Submenus declares (VS Code's
	// {"submenu": "git.commit"} entry); its title is the label of the submenu unless Title is set. CheckManifest refuses an item
	// that has both, and a submenu that is not declared.
	Submenu string
	// Title replaces the title of the command in this menu; empty keeps it.
	Title string
	// Group is the group of the item; the groups of a menu are ordered by name and separated by a line.
	Group string
	// Order orders the items of a group.
	Order float64
	// When is a when-clause under which the item is shown; empty is always.
	When string
	// Args are the JSON-compatible arguments the command is run with.
	Args any
}

// Menus adds the menu items of an extension.
type Menus interface {
	// Add adds the item.
	Add(item MenuItem) error
}

// SettingSpec declares a setting of an extension.
type SettingSpec struct {
	// Key is the dotted name. For Settings().Register it starts with the id of the extension and a dot: sqltools.connections.
	// A setting declared in Contributes.Settings may have any key the editor and the other extensions do not have (go.gopls).
	Key string
	// Type is the JSON type of the value: string, number, integer, boolean, array or object.
	Type string
	// Default is the value when nothing sets it; the zero value of Type when nil.
	Default any
	// Description says what the setting does; Markdown may be used.
	Description string
	// Enum lists the only values allowed, when it is not empty.
	Enum []any
	// Minimum and Maximum bound a number, when not nil.
	Minimum, Maximum *float64
	// Category is the group of the settings editor; the name of the extension when empty.
	Category string
	// AlsoTypes are the other JSON types the value may have besides Type, which stays the main one: "null", "string" and the others
	// (VS Code's type: ["string", "null", "array"]). Each is a JSON type or "null".
	AlsoTypes []string
	// EnumDescriptions describe the values of Enum, in the same order.
	EnumDescriptions []string
	// Scope says where the setting may be set (VS Code's ConfigurationScope): "window" (the default, also for ""), "resource" and
	// "language-overridable" (the user's and the workspace's settings), "machine" and "application" (the user's settings only).
	Scope string
}

// Settings registers and reads settings. A setting is read for the workspace of the scope, like every setting.
type Settings interface {
	// Register adds a setting. It fails when the key is not under the id of the extension, or is taken by a setting of
	// the editor.
	Register(spec SettingSpec) error
	// Get returns the value of a registered setting in the scope: a string, a float64, a bool, a []any or a
	// map[string]any, as the JSON of the settings file reads. Use SettingValue to get a Go type.
	Get(key string) (any, error)
	// OnChange calls fn, on the UI thread, when a setting changes in the scope, and returns the function that stops it.
	// The calls stop when the extension is deactivated.
	OnChange(fn func()) (cancel func())
	// Update writes the value of a setting of the extension to the user's settings file, keeping the comments and the layout
	// of the file, as the Settings editor does; the settings are read again and OnChange is called. The setting is one the
	// manifest declares in Contributes.Settings or the extension registered: it fails with ExtensionSettingNotOwned for any
	// other (an extension does not write the settings of the editor or of other extensions), with ExtensionDeactivated after
	// the activation ended, and returns the settings store's error when the value does not fit the setting or the file cannot
	// be written. Call it on the UI thread.
	Update(key string, value any) error
}

// SettingValue reads a setting as a Go type: a string, a bool, a float64, an int, a []string, or a struct or map that
// the JSON of the value decodes into.
func SettingValue[T any](s Settings, key string) (T, error) {
	var out T
	v, err := s.Get(key)
	if err != nil {
		return out, err
	}
	if t, ok := v.(T); ok {
		return t, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(data, &out)
	return out, err
}

// NotificationAction is a button of a notification.
type NotificationAction struct {
	// Label is the text of the button.
	Label string
	// Run is called on the UI thread when the button is pressed. A panic in it deactivates the extension.
	Run func()
}

// Notifications shows notifications in the window tab of the scope, with the name of the extension as their source.
type Notifications interface {
	// Info shows an information.
	Info(message string, actions ...NotificationAction)
	// Warn shows a warning.
	Warn(message string, actions ...NotificationAction)
	// Error shows an error.
	Error(message string, actions ...NotificationAction)
	// Progress shows a notification with a progress bar for work that takes a while (a clone, a sync that the user may cancel). It
	// stays until the returned handle's Done is called; a host that cannot show notifications (or the end of the activation) gives a
	// handle that does nothing, so the work goes on without a bar.
	Progress(opts ProgressOptions) NotificationProgress
}

// ProgressOptions describe a notification with a progress bar.
type ProgressOptions struct {
	// Title is the text of the notification; Report adds its message after it ("Cloning: Receiving objects").
	Title string
	// Cancellable adds a Cancel button. Pressing it calls OnCancel, on the UI thread, and the notification goes away at once; the work
	// stops when the extension acts on OnCancel.
	Cancellable bool
	// OnCancel is called when the Cancel button is pressed; it may be nil.
	OnCancel func()
}

// NotificationProgress is the handle of a notification with a progress bar.
type NotificationProgress interface {
	// Report says what the work is doing now and how far it is: percent is 0 to 100 of the whole, or negative when the amount is not
	// known (the bar then has no end). An empty message keeps the last one. It does nothing after Done.
	Report(message string, percent float64)
	// Done ends the work: the notification goes away. Calling it again does nothing.
	Done()
}

// StatusAlignment is the side of the status bar an item is on.
type StatusAlignment int

// The sides of the status bar.
const (
	// StatusLeft puts the item on the left side.
	StatusLeft StatusAlignment = iota
	// StatusRight puts the item on the right side.
	StatusRight
)

// StatusKind is the look of a status bar item.
type StatusKind int

// The looks of an item.
const (
	// StatusStandard is an item in the colors of the bar.
	StatusStandard StatusKind = iota
	// StatusRemote is an item on statusBarItem.remoteBackground, the color VS Code gives its remote indicator: an item that says
	// the workspace is shared with, or driven from, another computer (the Sharing item, SPEC §10.29).
	StatusRemote
)

// StatusItem is an item of the status bar.
type StatusItem struct {
	// ID identifies the item among the items of the extension.
	ID string
	// Name is the name for the user, in the list of the items one can hide; the name of the extension when empty.
	Name string
	// Text is the text of the item; a codicon is written $(name). An item with no text is not shown.
	Text string
	// Tooltip is the text of the hover.
	Tooltip string
	// Alignment is the side of the bar.
	Alignment StatusAlignment
	// Kind is the look of the item; StatusStandard when not set.
	Kind StatusKind
	// Color, when it is "#rrggbb" (or "#rgb"), is the background of the item, with a text color that reads on it (white or black).
	// It takes the place of Kind's background. It is how the SQL extension tints the item of a connection with the connection's
	// color; any other text is ignored.
	Color string
	// Priority orders the items of a side: the higher it is, the closer to the left end of the bar.
	Priority float64
	// Command is the id of the command a click runs, with Args; used when OnClick is nil.
	Command string
	Args    any
	// OnClick is called on the UI thread when the item is clicked. A panic in it deactivates the extension.
	OnClick func()
}

// StatusHandle changes or removes an item that was added to the status bar.
type StatusHandle interface {
	// Update replaces what the item shows and does; its ID is kept.
	Update(item StatusItem)
	// SetVisible shows or hides the item.
	SetVisible(visible bool)
	// Dispose removes the item.
	Dispose()
}

// StatusBar adds items to the status bar of the scope.
type StatusBar interface {
	// Add puts an item in the bar. It fails when the extension has an item with the same ID.
	Add(item StatusItem) (StatusHandle, error)
}

// Documents reads the documents of the scope: the open ones, and the files of the folder of a workspace scope.
type Documents interface {
	// Read returns the text of the document with the URI (file:///path/to/file, untitled:1): the text open in the
	// scope, with its unsaved changes, or the file of the folder on disk. It fails for a document the scope does not
	// have. With FromScope it reads from another window tab along a link (SPEC §10.15 "Event sharing").
	Read(ctx context.Context, uri string, opts ...ReadOption) (string, error)
	// Open lists the URIs of the documents open in the scope, in the order they were opened.
	Open() []string
	// Active describes the text editor that is in front in the scope (the one that has the focus, or had it last, in the
	// active editor group), with its selections as they are now; false when none is (the group is empty, or its tab shows
	// something other than a text). It is the answer TopicEditorActiveChanged and TopicEditorSelectionChanged give, for an
	// extension that is activated after them and needs it at once (a command that works on the cursor). Call it on the UI
	// thread.
	Active() (ActiveDocument, bool)
	// Show opens the file with the URI (or shows the open document) in the active editor group, with the cursor at the
	// line and column (counted from 1; a column counts UTF-16 units), as a click on a problem does; a click opens a
	// preview, preview false keeps the editor. It fails with ExtensionDocumentUnavailable when the scope cannot show
	// documents and returns the window's error for a file that cannot be opened. Call it on the UI thread.
	Show(uri string, line, column int, preview bool) error
	// OpenUntitled opens a new untitled document in the active editor group, kept (not a preview), in the language with the text
	// (the language of files.defaultLanguage when languageID is empty, plain text when the language does not exist), and returns
	// its URI. The text is the first state of the document: its undo history is empty, so that undoing never takes it away, and,
	// as with every untitled document that has a text, closing it asks to save. It fails with ExtensionDocumentUnavailable when
	// the scope cannot open documents, with ExtensionDeactivated after the activation ended, and returns the window's error when
	// the window tab is not on screen. Call it on the UI thread.
	OpenUntitled(languageID, text string) (uri string, err error)
	// Apply applies the edit to the documents and files of the scope, the way the rename of the editor applies its edit: the
	// changes are made in order, the edits of one document in a run of changes are one step of its undo history, a document
	// that is not open is opened in the scope (it stays open, with its changes unsaved), the cursors of the editors keep their
	// places in the text, and a create makes the file on disk (an empty one). The ranges of the edits of a document are in the
	// text as it is when Apply is called. It fails with ExtensionEditInvalid for a change whose kind or URI is not valid,
	// with ExtensionDocumentUnavailable when the scope cannot edit documents, with ExtensionDeactivated after the activation
	// ended, and returns the window's error when a change fails (an earlier change stays). Call it on the UI thread.
	Apply(ctx context.Context, edit WorkspaceEdit) error
	// ProvideContent registers the provider of the read-only documents of a scheme (SPEC §10.30; VS Code's
	// registerTextDocumentContentProvider). It fails with ExtensionContentSchemeInvalid for a scheme that is not a word or is one of
	// the core's, ExtensionContentProviderInvalid for a provider with no Text, ExtContentSchemeTaken when the window tab has
	// the scheme already, ExtensionDocumentUnavailable when the scope cannot show documents, and ExtensionDeactivated after the
	// activation ended.
	ProvideContent(p ContentProvider) (ContentRegistration, error)
	// OpenDiff opens a diff editor (SPEC §9.11) of two documents in the active editor group. The provided documents are made off the
	// UI thread; the diff opens when they are there, or the failure is reported to the user (a binary file with the notice of a
	// file that looks binary). A diff of the same two documents that is open is shown again, in its tab. It fails with
	// ExtensionDiffInvalid for a side that is empty, with the tags of document.ParseURI (URIInvalid) for a URI that does not parse,
	// with ExtContentUnknown for a provided URI that no provider makes, ExtDocumentShowUnavailable when the window tab is not on
	// screen, ExtensionDocumentUnavailable when the scope cannot open documents, and with ExtensionDeactivated. Call it on the UI
	// thread.
	OpenDiff(spec DiffSpec) error
	// OpenDocument opens a document in the active editor group with the title and the preview of the spec, reading a provided one
	// off the UI thread as OpenDiff does. It fails as OpenDiff does, with ExtensionDocumentInvalid for the URI. Call it on the UI
	// thread.
	OpenDocument(spec OpenSpec) error
	// ActiveMergeEditor describes the merge editor that is in front in the scope (SPEC §9.11): false when the active tab of the active
	// editor group is not one. It is how a command that works on the merge in front (Git's "Compute Conflicts With Git") finds its file.
	// It is false in a scope that has no editors. Call it on the UI thread.
	ActiveMergeEditor() (ActiveMerge, bool)
	// ProvideMultiDiff registers the provider of the sets of files of the multi-file diff editor of a kind (SPEC §10.30; VS Code's
	// IMultiDiffSourceResolver). The window asks it for the files of a set when the set is opened, when the provider says it changed,
	// and when a saved editor of the set comes back after a restart. It fails with ExtensionMultiDiffInvalid for a kind that is not a
	// word or a provider with no Resources, ExtMultiDiffKindTaken when the window tab has the kind already,
	// ExtensionDocumentUnavailable when the scope cannot show documents, and ExtensionDeactivated after the activation ended.
	ProvideMultiDiff(p MultiDiffProvider) (MultiDiffRegistration, error)
	// OpenMultiDiff opens the multi-file diff editor of a set (VS Code's _workbench.openMultiDiffEditor): one scrolling editor with a
	// section for each file. The editor opens at once and fills in when the provider has made the files off the UI thread; the same
	// set that is open is shown again, in its tab. It fails with ExtensionMultiDiffInvalid for a spec with a provider's set and a
	// fixed one, or with neither, ExtMultiDiffKindUnknown for a kind no provider has, ExtensionDocumentUnavailable when the scope
	// cannot open documents, ExtensionDeactivated, and returns ExtDocumentShowUnavailable when the window tab is not on screen. Call it
	// on the UI thread.
	OpenMultiDiff(spec MultiDiffSpec) error
	// Tabs lists the tabs of the text editors and diff editors of the window tab, group by group, in the order of the groups.
	Tabs() []DocumentTab
	// CloseTabs closes the tabs, the way the close button does: a tab with unsaved changes asks first. It returns the number of
	// tabs that were found (a tab that is gone is skipped). Call it on the UI thread.
	CloseTabs(tabs ...DocumentTab) int
	// CloseTabsAs closes the tabs the way how says: asking (CloseTabs), saving first, or dropping the unsaved changes. A save is
	// not finished when it returns: the tabs close when their documents are written, and the count is that of the tabs that were found.
	// Call it on the UI thread.
	CloseTabsAs(how TabClose, tabs ...DocumentTab) int
	// Dirty lists the URIs of the open files that have unsaved changes, in the order they were opened; an untitled document is not
	// a file and is left out. It is empty in a scope that cannot show documents. Call it on the UI thread.
	Dirty() []string
	// SaveAll saves the open documents with these URIs, the way the Save command does, and returns when every one is written
	// (VS Code's TextDocument.save of each). A document that is not open or has no unsaved changes is skipped. It blocks, so call it
	// off the UI thread. It fails with ExtensionDocumentUnavailable when the scope cannot save documents, with ExtDocumentSaveUnavailable
	// when the window tab is not on screen, with ExtDocumentNotSaved for the first document that was not saved (the window tab shows
	// why), with the error of ctx when it ends first, and with ExtensionDeactivated after the activation ended.
	SaveAll(ctx context.Context, uris []string) error
	// QuickDiffOriginal returns the text the primary quick diff provider of the file with the URI (file:///path) says it is compared with,
	// the one the marks of the editor use (SPEC §10.30 "Quick diff"): the index version of a file of a Git repository. ok is false when no
	// provider has an original for the file. The extension that makes the original needs no call to this; it is for an extension that
	// works on the modified lines (the format on save of modifications of the window uses it). It may block (it asks the providers), so
	// call it off the UI thread. It fails with ExtensionQuickDiffUnavailable in a scope that has no editors, with the error of ctx when
	// it ends first, and with ExtensionDeactivated after the activation ended.
	QuickDiffOriginal(ctx context.Context, uri string) (text string, ok bool, err error)
	// LineChanges compares two texts by lines, with the diff of the quick diff marks and of the diff editor, and returns the changed
	// blocks of the modified text against the original (VS Code's ILineChange; SPEC §10.30 "The peek"): the lines of a text that ends
	// with a line break have an empty last line, "\r\n" and "\n" end a line alike, and white space at the ends of lines counts. It is
	// how a command that stages or reverts the lines a selection touches finds them. It runs on the calling goroutine (the diff has a
	// time limit of one second, after which the texts are one change) and stops with ctx. It fails with ExtensionQuickDiffUnavailable
	// in a scope that has no editors, with the error of ctx when it ends first, and with ExtensionDeactivated after the activation
	// ended.
	LineChanges(ctx context.Context, original, modified string) ([]LineChange, error)
}

// ActiveDocument is the text editor in front, as Documents().Active describes it (SPEC §10.28).
type ActiveDocument struct {
	// URI is the document: file:///path/to/file, untitled:1.
	URI string
	// LanguageID is the language of the document.
	LanguageID string
	// Version is the version of the text the selections are in.
	Version int
	// Selections are all the selections of the editor, the primary one first; an empty one is a cursor. Positions are those of
	// the core events: zero-based lines and columns in bytes of the UTF-8 line.
	Selections []Selection
}

// Clipboard writes to the clipboard of the desktop. An extension cannot read it.
type Clipboard interface {
	// WriteText puts the text on the clipboard. It returns at once; a clipboard that fails is reported to the user by Emetos. It
	// fails with ExtensionDeactivated after the activation ended and with ExtensionClipboardUnavailable in a scope that has
	// no clipboard (a window tab that is not on screen). Call it on the UI thread.
	WriteText(text string) error
}
