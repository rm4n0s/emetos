package langfeatures

import (
	"encoding/json"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Position and Range are Emetos's: zero-based lines and columns counted in bytes of the UTF-8 line.
type (
	// Position is a place in a text.
	Position = buffer.Position
	// Range is the text from Start up to End.
	Range = buffer.Range
)

// Document is what a provider is asked about: an immutable snapshot of a document's text at one version. It is safe to use
// from any goroutine.
type Document struct {
	URI        document.URI
	LanguageID string
	Version    int
	Text       buffer.Snapshot
}

// Markup is text for the user, plain or Markdown.
type Markup struct {
	Value    string
	Markdown bool
}

// TextEdit replaces a range with text.
type TextEdit struct {
	Range Range
	Text  string
}

// Command is a command with its arguments. Owner says whose it is: the id of the language server (workspace/executeCommand)
// or of the extension (a command of the editor); empty for a command of the editor.
type Command struct {
	Title     string
	ID        string
	Arguments []json.RawMessage
	Owner     string
}

// Location is a place in a document.
type Location struct {
	URI   document.URI
	Range Range
}

// LocationLink is a location with the span of the symbol that led to it. Origin is nil when the provider did not say.
type LocationLink struct {
	Origin          *Range
	Target          Location
	TargetSelection Range
}

// NavKind is the kind of navigation a location provider answers.
type NavKind int

// The kinds of navigation.
const (
	NavDefinition NavKind = iota
	NavDeclaration
	NavTypeDefinition
	NavImplementation
)

// CompletionKind is the kind of a completion item; the numbers are the LSP's CompletionItemKind.
type CompletionKind int

// CompletionTriggerKind says why completion was asked for.
type CompletionTriggerKind int

// The reasons a completion is asked for.
const (
	// TriggerInvoked is typing or the key for the suggest widget.
	TriggerInvoked CompletionTriggerKind = iota
	// TriggerCharacter is a trigger character of a provider that was typed.
	TriggerCharacter
	// TriggerIncomplete is the repetition of a request whose answer was incomplete.
	TriggerIncomplete
)

// CompletionTrigger is the context of a completion request.
type CompletionTrigger struct {
	Kind      CompletionTriggerKind
	Character string
}

// CompletionItem is a row of the suggest widget.
type CompletionItem struct {
	Label            string
	LabelDetail      string
	LabelDescription string
	Detail           string
	Documentation    *Markup
	Kind             CompletionKind
	Deprecated       bool
	Preselect        bool
	SortText         string
	FilterText       string
	// InsertText is what is inserted, the label when empty; with Snippet it is a snippet.
	InsertText string
	Snippet    bool
	// Insert and Replace are the ranges the item replaces when it is inserted and when it replaces the word; both nil
	// means the word before the cursor; only Insert set is the range of a plain edit.
	Insert, Replace *Range
	// AdditionalEdits are made with the insertion.
	AdditionalEdits  []TextEdit
	CommitCharacters []string
	Command          *Command
	// Owner is the provider that made the item: the id of a language server or of an extension.
	Owner string
}

// CompletionList is the items of a completion request.
type CompletionList struct {
	Items []CompletionItem
	// Incomplete is true when a provider said its list was incomplete.
	Incomplete bool
}

// Hover is one hover content.
type Hover struct {
	Contents []Markup
	Range    *Range
	Owner    string
}

// Parameter is a parameter of a signature; Label is its text in the label of the signature.
type Parameter struct {
	Label         string
	Documentation *Markup
}

// Signature is a signature of a function.
type Signature struct {
	Label           string
	Documentation   *Markup
	Parameters      []Parameter
	ActiveParameter *int
}

// SignatureHelp is the signatures at a position with the active one.
type SignatureHelp struct {
	Signatures      []Signature
	ActiveSignature int
	ActiveParameter int
	Owner           string
}

// SignatureContext is the context of a signature help request.
type SignatureContext struct {
	Character   string
	IsRetrigger bool
}

// DocumentSymbol is a symbol of a document; Kind is the LSP's SymbolKind.
type DocumentSymbol struct {
	Name, Detail   string
	Kind           int
	Deprecated     bool
	Range          Range
	SelectionRange Range
	Children       []DocumentSymbol
	Owner          string
}

// HighlightKind is the kind of a document highlight.
type HighlightKind int

// The kinds of highlight.
const (
	HighlightText HighlightKind = iota
	HighlightRead
	HighlightWrite
)

// DocumentHighlight is an occurrence of the symbol under the cursor.
type DocumentHighlight struct {
	Range Range
	Kind  HighlightKind
}

// Severity is how serious a diagnostic is.
type Severity int

// The severities, most serious first.
const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityInfo
	SeverityHint
)

// Diagnostic is a problem of a document.
type Diagnostic struct {
	Range    Range
	Severity Severity
	Message  string
	Source   string
	Code     string
	// CodeHref is a link that describes the code.
	CodeHref string
	// Unnecessary and Deprecated are the tags that fade and strike the text.
	Unnecessary, Deprecated bool
	// Raw is the diagnostic as the server sent it, to send back in the context of a code action request.
	Raw json.RawMessage
}

// CodeActionRequest is what a code action provider is asked for.
type CodeActionRequest struct {
	Range       Range
	Diagnostics []Diagnostic
	// Only lists the kinds wanted; empty is all.
	Only []string
	// Automatic is true when the request is made by the editor itself (the light bulb) and not by the user.
	Automatic bool
}

// CodeAction is an action the user may choose.
type CodeAction struct {
	Title       string
	Kind        string
	Diagnostics []Diagnostic
	Preferred   bool
	Disabled    string
	Edit        *WorkspaceEdit
	Command     *Command
	Owner       string
}

// FormattingOptions are the options of a formatting request.
type FormattingOptions struct {
	TabSize      int
	InsertSpaces bool
}

// PrepareRename is what a rename provider says about a position: the range to rename and the text to show.
type PrepareRename struct {
	Range       Range
	Placeholder string
	// DefaultBehavior asks the editor to use the word at the position.
	DefaultBehavior bool
}
