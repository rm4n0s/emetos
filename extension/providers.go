package extension

import (
	"context"
	"encoding/json"

	"github.com/rm4n0s/errors"
)

// Providers in Go (SPEC §9.12 "Providers in Go"): an extension that has no language server, or wants to add to one, gives
// the editor the answers of a language feature. Emetos calls a provider on a goroutine of its own with a context that ends
// when the answer is no longer wanted (the newest request wins), guards the call like every call into an extension (a
// panic deactivates the extension in that tab and the provider's answer is dropped), and merges its answer with those of
// the other providers and the servers for the same document the way VS Code does: the lists of completion items are joined,
// hovers are stacked, definitions are joined, and so on.
//
// Positions and ranges are those of the core events: zero-based lines and columns counted in bytes of the UTF-8 line
// (Position, Range). A provider gets the text it works on in the TextDocument it is called with; it never touches the
// document of the editor.

// TextDocument is a document as a provider sees it: an immutable copy of the text at one version.
type TextDocument struct {
	// URI is file:///path/to/file or untitled:1.
	URI string
	// LanguageID is the language of the document.
	LanguageID string
	// Version is the version of the text; it goes up with every change.
	Version int
	// Text is the whole text with "\n" line ends.
	Text string
}

// DocumentSelector says which documents a provider is for. A document matches when its language is one of Languages or its
// path matches Pattern; with neither, every document matches (with a lower score than a language match, which decides the
// order of the providers).
type DocumentSelector struct {
	// Languages are language ids (go).
	Languages []string
	// Pattern is a glob (VS Code's syntax: **/*.go) matched against the path of the file; empty matches none.
	Pattern string
}

// Markup is text for the user: plain, or Markdown.
type Markup struct {
	Value    string
	Markdown bool
}

// RangeEdit replaces a range with text.
type RangeEdit struct {
	Range Range
	Text  string
}

// Location is a place in a document.
type Location struct {
	// URI is file:///path/to/file.
	URI   string
	Range Range
}

// Command is a command of the editor (not of a server) with its arguments, that an item asks to run.
type Command struct {
	Title     string
	ID        string
	Arguments []json.RawMessage
}

// CompletionKind is the kind of a completion item or a symbol; the numbers are those of the LSP (CompletionItemKind and
// SymbolKind), which VS Code's icons follow.
type CompletionKind int

// The completion kinds.
const (
	CompletionText CompletionKind = iota + 1
	CompletionMethod
	CompletionFunction
	CompletionConstructor
	CompletionField
	CompletionVariable
	CompletionClass
	CompletionInterface
	CompletionModule
	CompletionProperty
	CompletionUnit
	CompletionValue
	CompletionEnum
	CompletionKeyword
	CompletionSnippet
	CompletionColor
	CompletionFile
	CompletionReference
	CompletionFolder
	CompletionEnumMember
	CompletionConstant
	CompletionStruct
	CompletionEvent
	CompletionOperator
	CompletionTypeParameter
)

// CompletionTrigger says why completion is asked for.
type CompletionTrigger struct {
	// Kind is "invoked" (typing or the key), "character" (a trigger character, in Character) or "incomplete" (the list
	// of an earlier answer was incomplete).
	Kind      string
	Character string
}

// CompletionItem is a row of the suggest widget.
type CompletionItem struct {
	// Label is the text of the row and, without InsertText, what is inserted.
	Label string
	// Detail is shown after the label, Documentation in the details pane.
	Detail        string
	Documentation *Markup
	Kind          CompletionKind
	Deprecated    bool
	Preselect     bool
	// SortText and FilterText replace the label in sorting and filtering.
	SortText, FilterText string
	// InsertText is inserted instead of the label; with Snippet it is a snippet (SPEC §9.9).
	InsertText string
	Snippet    bool
	// Range, when not nil, is the text the item replaces (it must contain the cursor and be on its line); without it the
	// word before the cursor is replaced.
	Range *Range
	// AdditionalEdits are made together with the insertion (imports).
	AdditionalEdits  []RangeEdit
	CommitCharacters []string
	Command          *Command
}

// CompletionList is the answer of a completion provider; Incomplete asks for the request to be made again when the user
// types more.
type CompletionList struct {
	Items      []CompletionItem
	Incomplete bool
}

// CompletionProvider answers completion requests.
type CompletionProvider interface {
	ProvideCompletion(ctx context.Context, doc TextDocument, pos Position, trigger CompletionTrigger) (CompletionList, error)
}

// Hover is the answer of a hover provider.
type Hover struct {
	// Contents are shown one under the other.
	Contents []Markup
	// Range is the text the hover is for; the word at the position when nil.
	Range *Range
}

// HoverProvider answers hover requests; a nil Hover is no answer.
type HoverProvider interface {
	ProvideHover(ctx context.Context, doc TextDocument, pos Position) (*Hover, error)
}

// Parameter is a parameter of a signature: the text of the signature that is it (Label), and its documentation.
type Parameter struct {
	Label         string
	Documentation *Markup
}

// Signature is one signature of a function.
type Signature struct {
	Label         string
	Documentation *Markup
	Parameters    []Parameter
}

// SignatureHelp is the answer of a signature help provider.
type SignatureHelp struct {
	Signatures      []Signature
	ActiveSignature int
	ActiveParameter int
}

// SignatureHelpProvider answers signature help requests; a nil answer is none.
type SignatureHelpProvider interface {
	ProvideSignatureHelp(ctx context.Context, doc TextDocument, pos Position) (*SignatureHelp, error)
}

// DefinitionProvider answers go to definition.
type DefinitionProvider interface {
	ProvideDefinition(ctx context.Context, doc TextDocument, pos Position) ([]Location, error)
}

// ReferenceProvider answers find all references.
type ReferenceProvider interface {
	ProvideReferences(ctx context.Context, doc TextDocument, pos Position, includeDeclaration bool) ([]Location, error)
}

// DocumentSymbol is a symbol of a document with the symbols inside it.
type DocumentSymbol struct {
	Name   string
	Detail string
	// Kind is the LSP SymbolKind: File 1, Module 2, Namespace 3, Package 4, Class 5, Method 6, Property 7, Field 8,
	// Constructor 9, Enum 10, Interface 11, Function 12, Variable 13, Constant 14, String 15, ... Struct 23.
	Kind int
	// Range is the whole symbol, SelectionRange the part to select (its name).
	Range, SelectionRange Range
	Children              []DocumentSymbol
}

// DocumentSymbolProvider answers the outline of a document.
type DocumentSymbolProvider interface {
	ProvideDocumentSymbols(ctx context.Context, doc TextDocument) ([]DocumentSymbol, error)
}

// DocumentHighlight is an occurrence of the symbol under the cursor; Kind is 1 text, 2 read or 3 write.
type DocumentHighlight struct {
	Range Range
	Kind  int
}

// DocumentHighlightProvider answers the occurrences of the symbol under the cursor.
type DocumentHighlightProvider interface {
	ProvideDocumentHighlights(ctx context.Context, doc TextDocument, pos Position) ([]DocumentHighlight, error)
}

// Diagnostic is a problem of a document, as the context of a code action request.
type Diagnostic struct {
	Range    Range
	Severity Severity
	Message  string
	Source   string
	Code     string
}

// CodeActionRequest is what a code action provider is asked for.
type CodeActionRequest struct {
	// Range is the selection or the cursor.
	Range Range
	// Diagnostics are the problems that overlap Range.
	Diagnostics []Diagnostic
	// Only lists the kinds wanted (quickfix, refactor, source.organizeImports); empty is all.
	Only []string
}

// ResourceChange is one change of a WorkspaceEdit: the edits of a document, or the creation, renaming or deletion of a file.
type ResourceChange struct {
	// Kind is "edit" (the edits of URI), "create", "rename" (URI to NewURI) or "delete".
	Kind   string
	URI    string
	NewURI string
	// Edits are the edits of an "edit" change; their ranges are in the text as it is now.
	Edits []RangeEdit
	// Overwrite and IgnoreIfExists are the options of create and rename; Recursive and IgnoreIfNotExists those of delete.
	Overwrite, IgnoreIfExists, Recursive, IgnoreIfNotExists bool
}

// WorkspaceEdit changes several documents and files; Emetos applies it with one undo step per document.
type WorkspaceEdit struct {
	Changes []ResourceChange
}

// CodeAction is an action the user may choose.
type CodeAction struct {
	Title string
	// Kind is quickfix, refactor, refactor.extract, source.organizeImports, ...
	Kind        string
	Diagnostics []Diagnostic
	Preferred   bool
	// Disabled, when not empty, is why the action cannot be applied.
	Disabled string
	Edit     *WorkspaceEdit
	Command  *Command
}

// CodeActionProvider answers code action requests.
type CodeActionProvider interface {
	ProvideCodeActions(ctx context.Context, doc TextDocument, req CodeActionRequest) ([]CodeAction, error)
}

// FormattingOptions are the options of a formatting request.
type FormattingOptions struct {
	TabSize      int
	InsertSpaces bool
}

// FormattingProvider formats a whole document; a provider that also implements RangeFormattingProvider formats ranges. The
// formatter is known to editor.defaultFormatter by the id of the extension.
type FormattingProvider interface {
	ProvideFormatting(ctx context.Context, doc TextDocument, opts FormattingOptions) ([]RangeEdit, error)
}

// RangeFormattingProvider formats a range of a document.
type RangeFormattingProvider interface {
	ProvideRangeFormatting(ctx context.Context, doc TextDocument, r Range, opts FormattingOptions) ([]RangeEdit, error)
}

// PrepareRenameResult says what a rename would change: the Range of the symbol and the Placeholder to show in the input
// box (the text of the range when empty).
type PrepareRenameResult struct {
	Range       Range
	Placeholder string
}

// RenameProvider answers rename. PrepareRename may refuse with an error whose message is shown, and may answer nil to use
// the word at the position.
type RenameProvider interface {
	PrepareRename(ctx context.Context, doc TextDocument, pos Position) (*PrepareRenameResult, error)
	ProvideRename(ctx context.Context, doc TextDocument, pos Position, newName string) (*WorkspaceEdit, error)
}

// CodeLens is a command shown above a line of a document (SPEC §9.12 "Providers in Go", step 14.3).
type CodeLens struct {
	// Range is the line the lens is above; only its start line counts. The lenses of one line are shown on one row, in the
	// order the providers gave them.
	Range Range
	// Command is what the lens shows (its Title) and runs when it is clicked: a command of the editor with the first of its
	// Arguments. A lens with no title is not shown.
	Command Command
	// Disabled shows the title dimmed and runs nothing when it is clicked: an action that is announced and not available yet.
	Disabled bool
}

// CodeLensProvider answers the code lenses of a document. The editor asks again after the text changed (the newest request
// wins) and when the extension calls Languages().RefreshCodeLenses.
type CodeLensProvider interface {
	ProvideCodeLenses(ctx context.Context, doc TextDocument) ([]CodeLens, error)
}

// NewCommand makes a Command from a title, the id of a command and its arguments, which are encoded to JSON. It fails with
// ExtensionArgumentsInvalid for an argument that does not encode.
func NewCommand(title, id string, args ...any) (Command, error) {
	cmd := Command{Title: title, ID: id}
	for _, a := range args {
		data, err := json.Marshal(a)
		if err != nil {
			return Command{}, errors.NewErr("CommandArgumentInvalid", err).SetMetadata("command", id)
		}
		cmd.Arguments = append(cmd.Arguments, data)
	}
	return cmd, nil
}
