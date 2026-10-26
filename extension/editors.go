package extension

import (
	"context"
	"encoding/json"

	"github.com/rm4n0s/glutter/widgets"
)

// EditorKindSpec declares a kind of editor tab: a tab in the editor area with no file, of which several can be open
// (SPEC §10.15 "Editor tab"). The extension gives the kind its content with Editors().Provide and opens tabs of it with
// Editors().Open; the tabs come back when Emetos restarts, with the state they saved.
type EditorKindSpec struct {
	// ID identifies the kind. It starts with the id of the extension and a dot (emetos.notes.note).
	ID string
	// Name is the name of the kind, the title of a tab that has not set one.
	Name string
	// Icon is the name of the Codicon in the tab ("note", "comment-discussion"); may be empty.
	Icon string
}

// EditorActionSpec declares an editor title action: a micro icon at the right end of the tab strip of every editor group
// (SPEC §10.15 "Editor title action"). The icons are ordered by extension (in the order of activation) and Order; those
// that do not fit go into a "..." menu. The command is run with the argument {"groupId": n}, the group whose strip the
// icon is in (GroupOfArgs reads it), so that "New Note" opens its tab in that group.
type EditorActionSpec struct {
	// Command is the id of the command the icon runs: a command of the extension or of the editor.
	Command string
	// Icon is the name of the Codicon of the icon.
	Icon string
	// Tooltip is the text of the hover and the label in the "..." menu; the title of the command when empty.
	Tooltip string
	// When is a when-clause under which the icon is shown, read over the keys of the window and those that describe the
	// editor the group shows: activeEditor (the type of the input), resourceLangId, resourceExtname, resourceFilename,
	// resourceScheme and activeEditorIsDirty. Empty is always.
	When string
	// Order orders the icons of one extension, the lowest first.
	Order float64
}

// FileViewSpec declares a file view: micro-tabs inside the editor tab of a file (SPEC §10.15 "File view"). The tab of a
// file the spec selects shows Text first and then the views of the extensions, in the order of Order.
type FileViewSpec struct {
	// ID identifies the view. It starts with the id of the extension and a dot (emetos.markdown.preview).
	ID string
	// Name is the label of the micro-tab ("Preview").
	Name string
	// Icon is the name of the Codicon of the micro-tab; may be empty.
	Icon string
	// Languages are the ids of the languages of the files the view is for (markdown); Globs are patterns of file names
	// (*.csv, docs/**/*.md) matched against the base name and the path relative to the folder. A file is selected by one
	// of either. A spec with neither is invalid.
	Languages []string
	Globs     []string
	// Order orders the views among those of one file, the lowest first (0 sorts after the ones with an order).
	Order int
}

// GroupOfArgs reads the group an editor title action was run for from the argument of its command: {"groupId": n}. It is
// 0 for any other argument (the command was not run from an icon), which means the active group.
func GroupOfArgs(args any) int {
	if m, ok := args.(map[string]any); ok {
		if n, ok := m["groupId"].(float64); ok {
			return int(n)
		}
		if n, ok := m["groupId"].(int); ok {
			return n
		}
	}
	return 0
}

// EditorTab is one tab of a kind the extension provides. Its methods run on the UI thread.
type EditorTab interface {
	// ID is the instance id of the tab, unique in the workspace; it is kept across restarts.
	ID() string
	// Kind is the id of the kind of the tab.
	Kind() string
	// SetTitle sets the name on the tab; SetHint the text of its tooltip (the title when empty).
	SetTitle(title string)
	SetHint(hint string)
	// SetDirty puts the dot of unsaved work on the tab and SetBusy the mark of work that goes on. Both are marks and
	// nothing else: closing a dirty tab asks nothing, so an extension keeps its data in the state it saves.
	SetDirty(dirty bool)
	SetBusy(busy bool)
	// Refresh builds the widget of the tab again.
	Refresh()
	// Reveal makes the tab the active tab of its group (and the group the active group); with focus the keyboard focus
	// goes into it.
	Reveal(focus bool)
	// Group is the id of the group that shows the tab, or 0 when it is in none.
	Group() int
	// Close closes the tab, without asking.
	Close()
}

// EditorTabContent is what an extension puts in a tab. All the functions run on the UI thread, and a panic in any of them
// deactivates the extension in the scope, as for a view (ViewContent).
type EditorTabContent struct {
	// Build makes the widget of the tab; like the widget of a view it is built inside a box that gives it the constraints
	// of the editor area, clips what it paints, keeps the keyboard focus and catches a panic.
	Build func(bc widgets.BuildContext) widgets.Widget
	// SaveState returns the state of the tab that is kept in the state of the window (a value that encodes to JSON, nil for
	// none). It is called when the tab goes off screen, when the extension is deactivated and when the window state is
	// written. The state is given to the kind's Create when the tab comes back, and is copied when the tab is split.
	SaveState func() (any, error)
	// OnClose is called when the tab is closed for good (not when it moves to another group, nor when the extension is
	// deactivated); the state of the tab is dropped after it.
	OnClose func()
}

// EditorKind is how an extension makes the content of the tabs of one kind.
type EditorKind struct {
	// Create makes the content of a tab: for a tab that Open made, with the state Open was given (as JSON), and for a tab
	// that comes back after a restart, with the state it saved (nil when it saved none). The title and the marks may be
	// set on tab at once. It is called once for each tab, on the UI thread.
	Create func(tab EditorTab, state json.RawMessage) (EditorTabContent, error)
}

// EditorOpenOptions say where Editors().Open puts a tab.
type EditorOpenOptions struct {
	// Group is the id of the group to open the tab in (see GroupOfArgs); 0 is the active group. A group that is gone
	// means the active group.
	Group int
	// Inactive opens the tab without making it the active tab of the group.
	Inactive bool
}

// Editors gives the extension the tabs of the editor area.
type Editors interface {
	// Provide sets how the tabs of a kind the manifest declares are made, and makes the tabs of the kind that came back
	// from the last session (and wait for the extension). It fails with ExtensionEditorKindNotDeclared when the manifest
	// does not declare the kind, ExtensionEditorKindProvided when the activation provided it already and
	// ExtensionEditorKindInvalid when Create is nil.
	Provide(kindID string, kind EditorKind) error
	// Open opens a new tab of the kind, whose Create gets state (any value that encodes to JSON, or nil), and returns it.
	// It fails with ExtensionEditorKindNotProvided when the activation did not provide the kind, and with the error of
	// Create.
	Open(kindID string, state any, opts EditorOpenOptions) (EditorTab, error)
	// Tabs are the open tabs of the kind that have their content, in the order they were made.
	Tabs(kindID string) []EditorTab
	// OpenProposedChange shows a change that is proposed to a file in the diff editor and waits for the user's answer (SPEC
	// §10.16 "As built (step 13.3)"). The tab has the current text of the file on the left, read-only, and the proposed
	// text on the right, which the user may edit; its title has Accept and Reject. The call returns when the user accepts
	// or rejects, or closes the tab, which is a rejection, and gives the text of the right side then. Nothing is written
	// to a file: the right side is a document of its own, and what happens to the file is up to the caller. It
	// blocks, so it is called from a goroutine of the extension and never from the UI thread. When ctx ends, or the
	// extension is deactivated, the tab is closed and the call fails with the error of the context
	// (ExtensionProposedChangeCancelled); it fails with ExtensionProposedChangeInvalid for a change with no URI,
	// ExtensionDeactivated and ExtensionEditorsUnavailable (the scope has no editor area).
	OpenProposedChange(ctx context.Context, change ProposedChange) (ProposedResult, error)
}

// ProposedChange is a change to a file that an extension asks the user to review (Editors.OpenProposedChange).
type ProposedChange struct {
	// URI is the URI of the file (file:///path/to/file); its name and language come from it. It is required.
	URI string
	// Original is the current text of the file, shown on the left; empty for a file that does not exist yet.
	Original string
	// Proposed is the text the change would give, shown on the right.
	Proposed string
	// Title is the title of the tab; "<file name> (proposed)" when empty.
	Title string
	// Language is the name or id of the language of both sides when the file name does not say it (a name that
	// Languages().Resolve knows); empty takes the language from the name of the file.
	Language string
}

// ProposedResult is the user's answer to a ProposedChange.
type ProposedResult struct {
	// Accepted is true when the user pressed Accept, and false for Reject and for closing the tab.
	Accepted bool
	// Text is the text of the right side at the time of the answer: the proposed text, or what the user made of it. It is
	// empty for a rejection.
	Text string
	// Edited tells that the user changed the proposed text before accepting.
	Edited bool
}

// TextEdit replaces a range of a document with text. Lines and columns count from 0 and a column counts the bytes of the
// UTF-8 line, as the core events do (§10.15 "As built (step 11.2)").
type TextEdit struct {
	StartLine, StartColumn, EndLine, EndColumn int
	Text                                       string
}

// ViewDocumentChange tells a file view that its document changed.
type ViewDocumentChange struct {
	// Version is the version of the document after the change.
	Version int
	// Cause is edit, undo, redo, lineEnding or setText, as in emetos.document.changed.
	Cause string
}

// DocumentHandle is the document a file view shows. Every method runs on the UI thread. Edits go through the document of
// the editor, so they are undone with its undo, make the document dirty, are saved with it and kept by the hot exit.
type DocumentHandle interface {
	// URI is the URI of the document (file:///path/to/file); it changes when the file is saved under another name.
	URI() string
	// LanguageID is the id of the language of the document.
	LanguageID() string
	// Version counts the changes of the text.
	Version() int
	// Text is the whole text, with its unsaved changes.
	Text() string
	// ReadOnly tells whether the document cannot be changed.
	ReadOnly() bool
	// Edit applies the edits as one undo step. It fails for a read-only document and for edits that overlap.
	Edit(edits []TextEdit) error
	// OnChange calls fn after every change of the text; the calls stop with the returned function and when the view
	// ends.
	OnChange(fn func(ViewDocumentChange)) (cancel func())
	// TopLine is the first line (counted from 0) that the Text view of the document shows at the top, as the last text
	// editor of the document scrolled; 0 when there is none.
	TopLine() int
	// OnScroll calls fn when a text editor of the document scrolls, with its first line; the calls stop with the returned
	// function and when the view ends.
	OnScroll(fn func(topLine int)) (cancel func())
	// OpenFile opens a file of the folder (a path relative to the folder or absolute) in an editor of the active group;
	// it is what a link to a file of the workspace does.
	OpenFile(path string) error
	// Folder is the folder of the workspace, or empty.
	Folder() string
}

// FileViewContent is the content of one file view in one editor tab.
type FileViewContent struct {
	// Build makes the widget of the view, inside a box (see EditorTabContent.Build). It is built again after Refresh.
	Build func(bc widgets.BuildContext) widgets.Widget
	// Close is called when the editor tab closes, the extension is turned off or the view fails; it releases what the
	// view subscribed to.
	Close func()
}

// FileViewTab is the tab a file view is open in.
type FileViewTab interface {
	// Refresh builds the widget of the view again.
	Refresh()
}

// FileViewProvider is how an extension makes the view of a document.
type FileViewProvider struct {
	// Open makes the content of the view for the document of an editor tab, the first time the tab shows the view. A
	// returned error makes the tab fall back to the Text view.
	Open func(doc DocumentHandle, tab FileViewTab) (FileViewContent, error)
}

// FileViewOpenOptions say how FileViews().Open shows a document.
type FileViewOpenOptions struct {
	// Side opens it in the group on the side of the active group (made when there is none), as "Open to the Side" does;
	// otherwise the active group shows it.
	Side bool
	// Group is the id of the group whose active text editor shows the document when the uri is empty (see GroupOfArgs); 0
	// is the active group.
	Group int
}

// FileViews gives the file views the manifest declares their content.
type FileViews interface {
	// Provide sets how the view is made. It fails with ExtensionFileViewNotDeclared, ExtensionFileViewProvided and
	// ExtensionFileViewInvalid (Open is nil).
	Provide(viewID string, p FileViewProvider) error
	// Open shows the open document or the file with the URI in the view (an editor tab of the file is made when there is
	// none in the group); an empty uri is the document of the active text editor of opts.Group. It fails when the scope
	// cannot show editors or there is no such document; a view that is not available for the file leaves the file in Text.
	Open(uri, viewID string, opts FileViewOpenOptions) error
}

// Token is a token of a line of highlighted text: the bytes [Start, End) of the line and the TextMate scopes of the token,
// outermost first. The color is the theme's (package ui resolves it).
type Token struct {
	Start, End int
	Scopes     []string
}

// Languages gives the extension the grammars of the languages Emetos knows, and takes the language feature providers it
// writes in Go (SPEC §9.12 "Providers in Go"; the diagnostics of such a provider are set with Problems).
type Languages interface {
	// Resolve gives the id of the language called name (an id, an alias or a file extension such as go, golang or py); false
	// when none is.
	Resolve(name string) (languageID string, ok bool)
	// Tokenize splits text into lines and tokens with the grammar of the language; the lines are the lines of text. It fails
	// with ExtensionLanguageUnknown for a language that has no grammar. It belongs to the UI thread and costs a
	// tokenization of the text, so a caller keeps the answer.
	Tokenize(languageID, text string) ([][]Token, error)

	// RegisterCompletionProvider adds a provider of completion items for the documents the selector matches;
	// triggerCharacters are the characters that start a request when typed. Each Register method returns the function
	// that takes the provider away; it also goes with the activation. They fail with ExtensionDeactivated and
	// ExtensionProviderInvalid (a nil provider or a selector that selects nothing).
	RegisterCompletionProvider(sel DocumentSelector, p CompletionProvider, triggerCharacters ...string) (dispose func(), err error)
	// RegisterHoverProvider adds a hover provider.
	RegisterHoverProvider(sel DocumentSelector, p HoverProvider) (dispose func(), err error)
	// RegisterSignatureHelpProvider adds a signature help provider; triggerCharacters start a request when typed.
	RegisterSignatureHelpProvider(sel DocumentSelector, p SignatureHelpProvider, triggerCharacters ...string) (dispose func(), err error)
	// RegisterDefinitionProvider adds a definition provider.
	RegisterDefinitionProvider(sel DocumentSelector, p DefinitionProvider) (dispose func(), err error)
	// RegisterReferenceProvider adds a references provider.
	RegisterReferenceProvider(sel DocumentSelector, p ReferenceProvider) (dispose func(), err error)
	// RegisterDocumentSymbolProvider adds a provider of the symbols of a document.
	RegisterDocumentSymbolProvider(sel DocumentSelector, p DocumentSymbolProvider) (dispose func(), err error)
	// RegisterDocumentHighlightProvider adds a provider of document highlights.
	RegisterDocumentHighlightProvider(sel DocumentSelector, p DocumentHighlightProvider) (dispose func(), err error)
	// RegisterCodeActionProvider adds a code action provider.
	RegisterCodeActionProvider(sel DocumentSelector, p CodeActionProvider) (dispose func(), err error)
	// RegisterFormattingProvider adds a formatter; it formats ranges too when it implements RangeFormattingProvider.
	RegisterFormattingProvider(sel DocumentSelector, p FormattingProvider) (dispose func(), err error)
	// RegisterRenameProvider adds a rename provider.
	RegisterRenameProvider(sel DocumentSelector, p RenameProvider) (dispose func(), err error)
}
