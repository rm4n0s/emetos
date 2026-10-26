package lsp

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
)

// Hover is the answer to textDocument/hover. Contents is always a list of Markdown or plain text
// parts: a MarkupContent is one part, a MarkedString (a string, which is Markdown, or a language and a
// value, which is a fenced code block) is one part each.
type Hover struct {
	Contents []MarkupContent `json:"contents"`
	Range    *Range          `json:"range,omitempty"`
}

// UnmarshalJSON reads MarkupContent, MarkedString and arrays of MarkedStrings.
func (h *Hover) UnmarshalJSON(data []byte) error {
	var raw struct {
		Contents json.RawMessage `json:"contents"`
		Range    *Range          `json:"range"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.NewErr("HoverInvalid", err)
	}
	h.Range = raw.Range
	h.Contents = nil
	var parts []json.RawMessage
	switch {
	case isNull(raw.Contents):
	case isArray(raw.Contents):
		if err := json.Unmarshal(raw.Contents, &parts); err != nil {
			return errors.NewErr("HoverInvalid", err)
		}
	default:
		parts = []json.RawMessage{raw.Contents}
	}
	for _, p := range parts {
		var s string
		if isString(p) {
			if err := json.Unmarshal(p, &s); err != nil {
				return errors.NewErr("HoverInvalid", err)
			}
			h.Contents = append(h.Contents, MarkupContent{Kind: MarkupMarkdown, Value: s})
			continue
		}
		var part struct {
			Language string `json:"language"`
			Value    string `json:"value"`
			Kind     string `json:"kind"`
		}
		if err := json.Unmarshal(p, &part); err != nil {
			return errors.NewErr("HoverInvalid", err)
		}
		if part.Kind == "" && part.Language != "" {
			h.Contents = append(h.Contents, MarkupContent{Kind: MarkupMarkdown, Value: "```" + part.Language + "\n" + part.Value + "\n```"})
			continue
		}
		kind := part.Kind
		if kind == "" {
			kind = MarkupMarkdown
		}
		h.Contents = append(h.Contents, MarkupContent{Kind: kind, Value: part.Value})
	}
	return nil
}

// CompletionContext says why completion was asked for.
type CompletionContext struct {
	TriggerKind      int    `json:"triggerKind"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

// CompletionParams are the params of textDocument/completion.
type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      *CompletionContext     `json:"context,omitempty"`
}

// CompletionItemLabelDetails are the faded parts after the label of a row.
type CompletionItemLabelDetails struct {
	Detail      string `json:"detail,omitempty"`
	Description string `json:"description,omitempty"`
}

// InsertReplaceEdit is a completion edit with two ranges: the one to use when the new text is inserted and
// the one to use when it replaces the word.
type InsertReplaceEdit struct {
	NewText string `json:"newText"`
	Insert  Range  `json:"insert"`
	Replace Range  `json:"replace"`
}

// CompletionTextEdit is the textEdit member of a completion item: a TextEdit (Range is set) or an
// InsertReplaceEdit (Insert and Replace are).
type CompletionTextEdit struct {
	NewText string
	// Range is the range of a plain TextEdit; nil for an InsertReplaceEdit.
	Range *Range
	// Insert and Replace are the ranges of an InsertReplaceEdit.
	Insert, Replace *Range
}

// UnmarshalJSON reads either form.
func (e *CompletionTextEdit) UnmarshalJSON(data []byte) error {
	var raw struct {
		NewText string `json:"newText"`
		Range   *Range `json:"range"`
		Insert  *Range `json:"insert"`
		Replace *Range `json:"replace"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.NewErr("CompletionInvalid", err)
	}
	*e = CompletionTextEdit{NewText: raw.NewText, Range: raw.Range, Insert: raw.Insert, Replace: raw.Replace}
	return nil
}

// MarshalJSON writes the form that was read.
func (e CompletionTextEdit) MarshalJSON() ([]byte, error) {
	if e.Range != nil {
		return json.Marshal(TextEdit{Range: *e.Range, NewText: e.NewText})
	}
	var ins, rep Range
	if e.Insert != nil {
		ins = *e.Insert
	}
	if e.Replace != nil {
		rep = *e.Replace
	}
	return json.Marshal(InsertReplaceEdit{NewText: e.NewText, Insert: ins, Replace: rep})
}

// CompletionItem is a row of the suggest widget.
type CompletionItem struct {
	Label               string                      `json:"label"`
	LabelDetails        *CompletionItemLabelDetails `json:"labelDetails,omitempty"`
	Kind                int                         `json:"kind,omitempty"`
	Tags                []int                       `json:"tags,omitempty"`
	Detail              string                      `json:"detail,omitempty"`
	Documentation       *MarkupContent              `json:"documentation,omitempty"`
	Deprecated          bool                        `json:"deprecated,omitempty"`
	Preselect           bool                        `json:"preselect,omitempty"`
	SortText            string                      `json:"sortText,omitempty"`
	FilterText          string                      `json:"filterText,omitempty"`
	InsertText          string                      `json:"insertText,omitempty"`
	InsertTextFormat    int                         `json:"insertTextFormat,omitempty"`
	InsertTextMode      int                         `json:"insertTextMode,omitempty"`
	TextEdit            *CompletionTextEdit         `json:"textEdit,omitempty"`
	TextEditText        string                      `json:"textEditText,omitempty"`
	AdditionalTextEdits []TextEdit                  `json:"additionalTextEdits,omitempty"`
	CommitCharacters    []string                    `json:"commitCharacters,omitempty"`
	Command             *Command                    `json:"command,omitempty"`
	Data                json.RawMessage             `json:"data,omitempty"`
}

// CompletionItemDefaults are the defaults of the items of a list (LSP 3.17 "itemDefaults").
type CompletionItemDefaults struct {
	CommitCharacters []string        `json:"commitCharacters,omitempty"`
	EditRange        json.RawMessage `json:"editRange,omitempty"`
	InsertTextFormat int             `json:"insertTextFormat,omitempty"`
	InsertTextMode   int             `json:"insertTextMode,omitempty"`
	Data             json.RawMessage `json:"data,omitempty"`
}

// CompletionList is the answer to textDocument/completion; a plain array of items is read as a complete
// list.
type CompletionList struct {
	IsIncomplete bool                    `json:"isIncomplete"`
	ItemDefaults *CompletionItemDefaults `json:"itemDefaults,omitempty"`
	Items        []CompletionItem        `json:"items"`
}

// UnmarshalJSON reads a list and an array of items.
func (l *CompletionList) UnmarshalJSON(data []byte) error {
	*l = CompletionList{}
	if isNull(data) {
		return nil
	}
	if isArray(data) {
		if err := json.Unmarshal(data, &l.Items); err != nil {
			return errors.NewErr("CompletionInvalid", err)
		}
		return nil
	}
	type plain CompletionList
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return errors.NewErr("CompletionInvalid", err)
	}
	*l = CompletionList(p)
	return nil
}

// SignatureHelpContext says why signature help was asked for.
type SignatureHelpContext struct {
	TriggerKind         int            `json:"triggerKind"`
	TriggerCharacter    string         `json:"triggerCharacter,omitempty"`
	IsRetrigger         bool           `json:"isRetrigger"`
	ActiveSignatureHelp *SignatureHelp `json:"activeSignatureHelp,omitempty"`
}

// SignatureHelpParams are the params of textDocument/signatureHelp.
type SignatureHelpParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      *SignatureHelpContext  `json:"context,omitempty"`
}

// ParameterLabel is the label of a parameter: a text to find in the label of the signature, or the
// offsets of the parameter in it, counted in UTF-16 code units whatever the position encoding is.
type ParameterLabel struct {
	Text    string
	Offsets *[2]int
}

// UnmarshalJSON reads a string and a pair of numbers.
func (l *ParameterLabel) UnmarshalJSON(data []byte) error {
	*l = ParameterLabel{}
	if isArray(data) {
		var pair [2]int
		if err := json.Unmarshal(data, &pair); err != nil {
			return errors.NewErr("SignatureInvalid", err)
		}
		l.Offsets = &pair
		return nil
	}
	if err := json.Unmarshal(data, &l.Text); err != nil {
		return errors.NewErr("SignatureInvalid", err)
	}
	return nil
}

// MarshalJSON writes the form that was read.
func (l ParameterLabel) MarshalJSON() ([]byte, error) {
	if l.Offsets != nil {
		return json.Marshal(*l.Offsets)
	}
	return json.Marshal(l.Text)
}

// ParameterInformation is a parameter of a signature.
type ParameterInformation struct {
	Label         ParameterLabel `json:"label"`
	Documentation *MarkupContent `json:"documentation,omitempty"`
}

// SignatureInformation is one signature of a function.
type SignatureInformation struct {
	Label           string                 `json:"label"`
	Documentation   *MarkupContent         `json:"documentation,omitempty"`
	Parameters      []ParameterInformation `json:"parameters,omitempty"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}

// SignatureHelp is the answer to textDocument/signatureHelp.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature *int                   `json:"activeSignature,omitempty"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}

// ReferenceContext is the context of a references request.
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ReferenceParams are the params of textDocument/references.
type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

// DocumentHighlight is an occurrence of the symbol under the cursor; Kind is one of DocumentHighlightText,
// DocumentHighlightRead and DocumentHighlightWrite (text when 0).
type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitempty"`
}

// DocumentSymbolParams are the params of textDocument/documentSymbol.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DocumentSymbol is a symbol of a document with the symbols inside it. ContainerName is not a member of the
// protocol's DocumentSymbol: it is filled when the server answered with flat SymbolInformation.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Tags           []int            `json:"tags,omitempty"`
	Deprecated     bool             `json:"deprecated,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
	ContainerName  string           `json:"containerName,omitempty"`
}

// DocumentSymbols is the answer to textDocument/documentSymbol: DocumentSymbols, or flat
// SymbolInformation read as symbols without children.
type DocumentSymbols []DocumentSymbol

// UnmarshalJSON reads both answers.
func (s *DocumentSymbols) UnmarshalJSON(data []byte) error {
	*s = nil
	if isNull(data) {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return errors.NewErr("SymbolInvalid", err)
	}
	out := make(DocumentSymbols, 0, len(items))
	for _, item := range items {
		var probe struct {
			Location *Location `json:"location"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			return errors.NewErr("SymbolInvalid", err)
		}
		if probe.Location != nil {
			var info struct {
				Name          string `json:"name"`
				Kind          int    `json:"kind"`
				Tags          []int  `json:"tags"`
				Deprecated    bool   `json:"deprecated"`
				ContainerName string `json:"containerName"`
			}
			if err := json.Unmarshal(item, &info); err != nil {
				return errors.NewErr("SymbolInvalid", err)
			}
			out = append(out, DocumentSymbol{
				Name: info.Name, Kind: info.Kind, Tags: info.Tags, Deprecated: info.Deprecated,
				Range: probe.Location.Range, SelectionRange: probe.Location.Range, ContainerName: info.ContainerName,
			})
			continue
		}
		var sym DocumentSymbol
		if err := json.Unmarshal(item, &sym); err != nil {
			return errors.NewErr("SymbolInvalid", err)
		}
		out = append(out, sym)
	}
	*s = out
	return nil
}

// WorkspaceSymbolParams are the params of workspace/symbol.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// WorkspaceSymbolLocation is where a workspace symbol is: Range is nil when the server leaves it to
// workspaceSymbol/resolve.
type WorkspaceSymbolLocation struct {
	URI   string `json:"uri"`
	Range *Range `json:"range,omitempty"`
}

// WorkspaceSymbol is an answer to workspace/symbol; the older SymbolInformation has the same members.
type WorkspaceSymbol struct {
	Name          string                  `json:"name"`
	Kind          int                     `json:"kind"`
	Tags          []int                   `json:"tags,omitempty"`
	ContainerName string                  `json:"containerName,omitempty"`
	Location      WorkspaceSymbolLocation `json:"location"`
	Data          json.RawMessage         `json:"data,omitempty"`
}

// CodeActionContext is the context of a code action request.
type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
	TriggerKind int          `json:"triggerKind,omitempty"`
}

// CodeActionParams are the params of textDocument/codeAction.
type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

// CodeActionDisabled says why an action cannot be applied.
type CodeActionDisabled struct {
	Reason string `json:"reason"`
}

// CodeAction is an action the server offers.
type CodeAction struct {
	Title       string              `json:"title"`
	Kind        string              `json:"kind,omitempty"`
	Diagnostics []Diagnostic        `json:"diagnostics,omitempty"`
	IsPreferred bool                `json:"isPreferred,omitempty"`
	Disabled    *CodeActionDisabled `json:"disabled,omitempty"`
	Edit        *WorkspaceEdit      `json:"edit,omitempty"`
	Command     *Command            `json:"command,omitempty"`
	Data        json.RawMessage     `json:"data,omitempty"`
}

// CodeActions is the answer to textDocument/codeAction: CodeActions and bare Commands, the latter read as
// actions that only have the command.
type CodeActions []CodeAction

// UnmarshalJSON reads both kinds of item.
func (a *CodeActions) UnmarshalJSON(data []byte) error {
	*a = nil
	if isNull(data) {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return errors.NewErr("CodeActionInvalid", err)
	}
	out := make(CodeActions, 0, len(items))
	for _, item := range items {
		var probe struct {
			Command json.RawMessage `json:"command"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			return errors.NewErr("CodeActionInvalid", err)
		}
		if isString(probe.Command) {
			var cmd Command
			if err := json.Unmarshal(item, &cmd); err != nil {
				return errors.NewErr("CodeActionInvalid", err)
			}
			out = append(out, CodeAction{Title: cmd.Title, Command: &cmd})
			continue
		}
		var act CodeAction
		if err := json.Unmarshal(item, &act); err != nil {
			return errors.NewErr("CodeActionInvalid", err)
		}
		out = append(out, act)
	}
	*a = out
	return nil
}

// ChangeAnnotation describes a group of edits for the preview of a workspace edit.
type ChangeAnnotation struct {
	Label             string `json:"label"`
	NeedsConfirmation bool   `json:"needsConfirmation,omitempty"`
	Description       string `json:"description,omitempty"`
}

// The kinds of a DocumentChange; a text document edit has none.
const (
	ChangeCreateFile = "create"
	ChangeRenameFile = "rename"
	ChangeDeleteFile = "delete"
)

// DocumentChange is one member of WorkspaceEdit.DocumentChanges: the edits of a document (Kind is empty:
// TextDocument and Edits are set), or a file operation (Kind is "create", "rename" or "delete").
type DocumentChange struct {
	Kind string
	// TextDocument and Edits are set for the edits of a document.
	TextDocument *OptionalVersionedTextDocumentIdentifier
	Edits        []TextEdit
	// URI is the file to create or delete; OldURI and NewURI are those of a rename.
	URI, OldURI, NewURI string
	// The options of the file operations.
	Overwrite, IgnoreIfExists, Recursive, IgnoreIfNotExists bool
	AnnotationID                                            string
}

type fileOptions struct {
	Overwrite         bool `json:"overwrite,omitempty"`
	IgnoreIfExists    bool `json:"ignoreIfExists,omitempty"`
	Recursive         bool `json:"recursive,omitempty"`
	IgnoreIfNotExists bool `json:"ignoreIfNotExists,omitempty"`
}

type documentChangeJSON struct {
	Kind         string                                   `json:"kind,omitempty"`
	TextDocument *OptionalVersionedTextDocumentIdentifier `json:"textDocument,omitempty"`
	Edits        []TextEdit                               `json:"edits,omitempty"`
	URI          string                                   `json:"uri,omitempty"`
	OldURI       string                                   `json:"oldUri,omitempty"`
	NewURI       string                                   `json:"newUri,omitempty"`
	Options      *fileOptions                             `json:"options,omitempty"`
	AnnotationID string                                   `json:"annotationId,omitempty"`
}

// UnmarshalJSON reads the four kinds of change.
func (c *DocumentChange) UnmarshalJSON(data []byte) error {
	var j documentChangeJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return errors.NewErr("WorkspaceEditInvalid", err)
	}
	*c = DocumentChange{Kind: j.Kind, TextDocument: j.TextDocument, Edits: j.Edits, URI: j.URI, OldURI: j.OldURI, NewURI: j.NewURI, AnnotationID: j.AnnotationID}
	if j.Options != nil {
		c.Overwrite, c.IgnoreIfExists, c.Recursive, c.IgnoreIfNotExists = j.Options.Overwrite, j.Options.IgnoreIfExists, j.Options.Recursive, j.Options.IgnoreIfNotExists
	}
	switch c.Kind {
	case "", ChangeCreateFile, ChangeRenameFile, ChangeDeleteFile:
		return nil
	}
	return errors.New("WorkspaceEditInvalid", "a document change has an unknown kind", "kind", c.Kind)
}

// MarshalJSON writes the change in its kind's shape.
func (c DocumentChange) MarshalJSON() ([]byte, error) {
	j := documentChangeJSON{Kind: c.Kind, TextDocument: c.TextDocument, Edits: c.Edits, URI: c.URI, OldURI: c.OldURI, NewURI: c.NewURI, AnnotationID: c.AnnotationID}
	opts := fileOptions{Overwrite: c.Overwrite, IgnoreIfExists: c.IgnoreIfExists, Recursive: c.Recursive, IgnoreIfNotExists: c.IgnoreIfNotExists}
	if opts != (fileOptions{}) {
		j.Options = &opts
	}
	if c.Kind == "" && j.Edits == nil {
		j.Edits = []TextEdit{}
	}
	return json.Marshal(j)
}

// WorkspaceEdit changes several documents and files. A server uses Changes or DocumentChanges; when it
// sends both, DocumentChanges is the one to apply.
type WorkspaceEdit struct {
	Changes           map[string][]TextEdit       `json:"changes,omitempty"`
	DocumentChanges   []DocumentChange            `json:"documentChanges,omitempty"`
	ChangeAnnotations map[string]ChangeAnnotation `json:"changeAnnotations,omitempty"`
}

// ApplyWorkspaceEditParams are the params of workspace/applyEdit.
type ApplyWorkspaceEditParams struct {
	Label string        `json:"label,omitempty"`
	Edit  WorkspaceEdit `json:"edit"`
}

// ApplyWorkspaceEditResult is the answer to workspace/applyEdit.
type ApplyWorkspaceEditResult struct {
	Applied       bool   `json:"applied"`
	FailureReason string `json:"failureReason,omitempty"`
	FailedChange  *int   `json:"failedChange,omitempty"`
}

// FormattingOptions are the options of the formatting requests.
type FormattingOptions struct {
	TabSize                int   `json:"tabSize"`
	InsertSpaces           bool  `json:"insertSpaces"`
	TrimTrailingWhitespace *bool `json:"trimTrailingWhitespace,omitempty"`
	InsertFinalNewline     *bool `json:"insertFinalNewline,omitempty"`
	TrimFinalNewlines      *bool `json:"trimFinalNewlines,omitempty"`
}

// DocumentFormattingParams are the params of textDocument/formatting.
type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

// DocumentRangeFormattingParams are the params of textDocument/rangeFormatting.
type DocumentRangeFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Options      FormattingOptions      `json:"options"`
}

// DocumentOnTypeFormattingParams are the params of textDocument/onTypeFormatting.
type DocumentOnTypeFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Ch           string                 `json:"ch"`
	Options      FormattingOptions      `json:"options"`
}

// RenameParams are the params of textDocument/rename.
type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

// PrepareRenameResult is the answer to textDocument/prepareRename: the Range to rename (with the
// Placeholder to show, when the server gave one), or DefaultBehavior to let the client choose the word.
// A null answer means the position cannot be renamed.
type PrepareRenameResult struct {
	Range           *Range
	Placeholder     string
	DefaultBehavior bool
}

// UnmarshalJSON reads the three shapes.
func (r *PrepareRenameResult) UnmarshalJSON(data []byte) error {
	*r = PrepareRenameResult{}
	if isNull(data) {
		return nil
	}
	var raw struct {
		Start           *Position `json:"start"`
		End             *Position `json:"end"`
		Range           *Range    `json:"range"`
		Placeholder     string    `json:"placeholder"`
		DefaultBehavior bool      `json:"defaultBehavior"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.NewErr("RenameInvalid", err)
	}
	switch {
	case raw.Start != nil && raw.End != nil:
		r.Range = &Range{Start: *raw.Start, End: *raw.End}
	case raw.Range != nil:
		r.Range, r.Placeholder = raw.Range, raw.Placeholder
	default:
		r.DefaultBehavior = raw.DefaultBehavior
	}
	return nil
}

// CodeLensParams are the params of textDocument/codeLens.
type CodeLensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// CodeLens is a command shown above a line; Command is nil until the lens is resolved.
type CodeLens struct {
	Range   Range           `json:"range"`
	Command *Command        `json:"command,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// InlayHintParams are the params of textDocument/inlayHint.
type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

// InlayHintLabelPart is a part of a label with its own tooltip, location or command.
type InlayHintLabelPart struct {
	Value    string         `json:"value"`
	Tooltip  *MarkupContent `json:"tooltip,omitempty"`
	Location *Location      `json:"location,omitempty"`
	Command  *Command       `json:"command,omitempty"`
}

// InlayHintLabel is a string, or a list of parts.
type InlayHintLabel struct {
	Text  string
	Parts []InlayHintLabelPart
}

// String is the text of the label, the parts joined.
func (l InlayHintLabel) String() string {
	if l.Parts == nil {
		return l.Text
	}
	var b strings.Builder
	for _, p := range l.Parts {
		b.WriteString(p.Value)
	}
	return b.String()
}

// UnmarshalJSON reads a string and an array of parts.
func (l *InlayHintLabel) UnmarshalJSON(data []byte) error {
	*l = InlayHintLabel{}
	if isArray(data) {
		l.Parts = []InlayHintLabelPart{}
		if err := json.Unmarshal(data, &l.Parts); err != nil {
			return errors.NewErr("InlayHintInvalid", err)
		}
		return nil
	}
	if err := json.Unmarshal(data, &l.Text); err != nil {
		return errors.NewErr("InlayHintInvalid", err)
	}
	return nil
}

// MarshalJSON writes the form that was read.
func (l InlayHintLabel) MarshalJSON() ([]byte, error) {
	if l.Parts != nil {
		return json.Marshal(l.Parts)
	}
	return json.Marshal(l.Text)
}

// InlayHint is a faded label between two characters.
type InlayHint struct {
	Position     Position        `json:"position"`
	Label        InlayHintLabel  `json:"label"`
	Kind         int             `json:"kind,omitempty"`
	TextEdits    []TextEdit      `json:"textEdits,omitempty"`
	Tooltip      *MarkupContent  `json:"tooltip,omitempty"`
	PaddingLeft  bool            `json:"paddingLeft,omitempty"`
	PaddingRight bool            `json:"paddingRight,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
}

// SemanticTokensParams are the params of textDocument/semanticTokens/full.
type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SemanticTokensRangeParams are the params of textDocument/semanticTokens/range.
type SemanticTokensRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

// SemanticTokensDeltaParams are the params of textDocument/semanticTokens/full/delta.
type SemanticTokensDeltaParams struct {
	TextDocument     TextDocumentIdentifier `json:"textDocument"`
	PreviousResultID string                 `json:"previousResultId"`
}

// SemanticTokens are the tokens of a document, five numbers each, relative to the previous one.
type SemanticTokens struct {
	ResultID string   `json:"resultId,omitempty"`
	Data     []uint32 `json:"data"`
}

// SemanticTokensEdit replaces DeleteCount numbers of the data at Start with Data.
type SemanticTokensEdit struct {
	Start       int      `json:"start"`
	DeleteCount int      `json:"deleteCount"`
	Data        []uint32 `json:"data,omitempty"`
}

// SemanticTokensDelta is a change of the tokens the server sent before.
type SemanticTokensDelta struct {
	ResultID string               `json:"resultId,omitempty"`
	Edits    []SemanticTokensEdit `json:"edits"`
}

// SemanticTokensDeltaResult is the answer to a delta request: new tokens (Full) or edits (Delta).
type SemanticTokensDeltaResult struct {
	Full  *SemanticTokens
	Delta *SemanticTokensDelta
}

// UnmarshalJSON reads both answers.
func (r *SemanticTokensDeltaResult) UnmarshalJSON(data []byte) error {
	*r = SemanticTokensDeltaResult{}
	if isNull(data) {
		return nil
	}
	var probe struct {
		Edits json.RawMessage `json:"edits"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return errors.NewErr("SemanticTokensInvalid", err)
	}
	if probe.Edits != nil {
		r.Delta = &SemanticTokensDelta{}
		if err := json.Unmarshal(data, r.Delta); err != nil {
			return errors.NewErr("SemanticTokensInvalid", err)
		}
		return nil
	}
	r.Full = &SemanticTokens{}
	if err := json.Unmarshal(data, r.Full); err != nil {
		return errors.NewErr("SemanticTokensInvalid", err)
	}
	return nil
}

// SemanticTokensLegend names the token types and modifiers the numbers of the data index.
type SemanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

// FoldingRangeParams are the params of textDocument/foldingRange.
type FoldingRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// FoldingRange is a range of lines to fold; the characters count in the position encoding.
type FoldingRange struct {
	StartLine      int    `json:"startLine"`
	StartCharacter *int   `json:"startCharacter,omitempty"`
	EndLine        int    `json:"endLine"`
	EndCharacter   *int   `json:"endCharacter,omitempty"`
	Kind           string `json:"kind,omitempty"`
	CollapsedText  string `json:"collapsedText,omitempty"`
}

// SelectionRangeParams are the params of textDocument/selectionRange.
type SelectionRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Positions    []Position             `json:"positions"`
}

// SelectionRange is a range with the larger range around it.
type SelectionRange struct {
	Range  Range           `json:"range"`
	Parent *SelectionRange `json:"parent,omitempty"`
}

// DidOpenTextDocumentParams are the params of textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidChangeTextDocumentParams are the params of textDocument/didChange.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// WillSaveTextDocumentParams are the params of textDocument/willSave and willSaveWaitUntil.
type WillSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Reason       int                    `json:"reason"`
}

// DidSaveTextDocumentParams are the params of textDocument/didSave; Text is set when the server asked
// for it.
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

// DidCloseTextDocumentParams are the params of textDocument/didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}
