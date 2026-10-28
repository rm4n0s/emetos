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
	// MenuExplorerContext is the context menu of the Explorer.
	MenuExplorerContext = "ExplorerContext"
)

// MenuItem is an item of a menu.
type MenuItem struct {
	// Menu is the id of the menu, one of the Menu constants.
	Menu string
	// Command is the id of the command the item runs. An item whose command does not exist is not shown.
	Command string
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
	// Apply applies the edit to the documents and files of the scope, the way the rename of the editor applies its edit: the
	// changes are made in order, the edits of one document in a run of changes are one step of its undo history, a document
	// that is not open is opened in the scope (it stays open, with its changes unsaved), the cursors of the editors keep their
	// places in the text, and a create makes the file on disk (an empty one). The ranges of the edits of a document are in the
	// text as it is when Apply is called. It fails with ExtensionEditInvalid for a change whose kind or URI is not valid,
	// with ExtensionDocumentUnavailable when the scope cannot edit documents, with ExtensionDeactivated after the activation
	// ended, and returns the window's error when a change fails (an earlier change stays). Call it on the UI thread.
	Apply(ctx context.Context, edit WorkspaceEdit) error
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
