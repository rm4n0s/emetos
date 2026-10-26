package lsp

import (
	"encoding/json"

	"github.com/rm4n0s/errors"
)

// ClientCapabilities is the "capabilities" of the initialize request, as nested JSON objects.
type ClientCapabilities map[string]any

// DefaultClientCapabilities are the capabilities Emetos advertises (SPEC §9.12 "As built (step 12.2)"):
// exactly the features area 3 builds, so that a server does not do work for ones the editor cannot show.
// Left out on purpose: linked editing, document colors, document links, call and type hierarchies, inline
// values, monikers, notebooks, file operation notifications, show document, and the 3.18 additions.
func DefaultClientCapabilities() ClientCapabilities {
	dynamic := map[string]any{"dynamicRegistration": true}
	linkable := map[string]any{"dynamicRegistration": true, "linkSupport": true}
	markup := []string{MarkupMarkdown, MarkupPlainText}
	completionKinds := make([]int, 0, 25)
	for k := CompletionText; k <= CompletionTypeParameter; k++ {
		completionKinds = append(completionKinds, k)
	}
	symbolKinds := make([]int, 0, 26)
	for k := SymbolFile; k <= SymbolTypeParameter; k++ {
		symbolKinds = append(symbolKinds, k)
	}
	actionKinds := []string{
		KindQuickFix, KindRefactor, KindRefactorExtract, KindRefactorInline, KindRefactorRewrite,
		KindSource, KindSourceOrganizeImports, KindSourceFixAll,
	}
	return ClientCapabilities{
		"general": map[string]any{
			"positionEncodings": []string{EncodingNameUTF8, EncodingNameUTF16},
			"staleRequestSupport": map[string]any{
				"cancel": true,
				"retryOnContentModified": []string{
					MethodCompletion, MethodHover, MethodSignatureHelp, MethodCodeAction, MethodCodeLens,
					MethodInlayHint, MethodSemanticTokensFull, MethodSemanticTokensDelta, MethodSemanticTokensRange,
					MethodDocumentHighlight, MethodDocumentSymbol, MethodFoldingRange, MethodSelectionRange,
				},
			},
			"markdown": map[string]any{"parser": "marked", "version": "1.1.0"},
		},
		"window": map[string]any{
			"workDoneProgress": true,
			"showMessage":      map[string]any{"messageActionItem": map[string]any{"additionalPropertiesSupport": false}},
		},
		"workspace": map[string]any{
			"applyEdit": true,
			"workspaceEdit": map[string]any{
				"documentChanges":    true,
				"resourceOperations": []string{ChangeCreateFile, ChangeRenameFile, ChangeDeleteFile},
				"failureHandling":    "textOnlyTransactional",
				"changeAnnotationSupport": map[string]any{
					"groupsOnLabel": false,
				},
			},
			"didChangeConfiguration": map[string]any{"dynamicRegistration": false},
			"didChangeWatchedFiles":  map[string]any{"dynamicRegistration": true, "relativePatternSupport": true},
			"symbol": map[string]any{
				"dynamicRegistration": true,
				"symbolKind":          map[string]any{"valueSet": symbolKinds},
				"tagSupport":          map[string]any{"valueSet": []int{1}},
			},
			"executeCommand":   dynamic,
			"workspaceFolders": true,
			"configuration":    true,
			"semanticTokens":   map[string]any{"refreshSupport": true},
			"codeLens":         map[string]any{"refreshSupport": true},
			"inlayHint":        map[string]any{"refreshSupport": true},
			"diagnostics":      map[string]any{"refreshSupport": true},
		},
		"textDocument": map[string]any{
			"synchronization": map[string]any{
				"dynamicRegistration": true, "willSave": true, "willSaveWaitUntil": true, "didSave": true,
			},
			"completion": map[string]any{
				"dynamicRegistration": true,
				"contextSupport":      true,
				"insertTextMode":      InsertTextModeAdjustIndentation,
				"completionItem": map[string]any{
					"snippetSupport":          true,
					"commitCharactersSupport": true,
					"documentationFormat":     markup,
					"deprecatedSupport":       true,
					"preselectSupport":        true,
					"tagSupport":              map[string]any{"valueSet": []int{CompletionItemTagDeprecated}},
					"insertReplaceSupport":    true,
					"resolveSupport":          map[string]any{"properties": []string{"documentation", "detail", "additionalTextEdits"}},
					"insertTextModeSupport":   map[string]any{"valueSet": []int{InsertTextModeAsIs, InsertTextModeAdjustIndentation}},
					"labelDetailsSupport":     true,
				},
				"completionItemKind": map[string]any{"valueSet": completionKinds},
				"completionList": map[string]any{
					"itemDefaults": []string{"commitCharacters", "editRange", "insertTextFormat", "insertTextMode", "data"},
				},
			},
			"hover": map[string]any{"dynamicRegistration": true, "contentFormat": markup},
			"signatureHelp": map[string]any{
				"dynamicRegistration": true,
				"signatureInformation": map[string]any{
					"documentationFormat":    markup,
					"parameterInformation":   map[string]any{"labelOffsetSupport": true},
					"activeParameterSupport": true,
				},
				"contextSupport": true,
			},
			"declaration":       linkable,
			"definition":        linkable,
			"typeDefinition":    linkable,
			"implementation":    linkable,
			"references":        dynamic,
			"documentHighlight": dynamic,
			"documentSymbol": map[string]any{
				"dynamicRegistration":               true,
				"symbolKind":                        map[string]any{"valueSet": symbolKinds},
				"hierarchicalDocumentSymbolSupport": true,
				"tagSupport":                        map[string]any{"valueSet": []int{1}},
				"labelSupport":                      true,
			},
			"codeAction": map[string]any{
				"dynamicRegistration": true,
				"codeActionLiteralSupport": map[string]any{
					"codeActionKind": map[string]any{"valueSet": actionKinds},
				},
				"isPreferredSupport": true,
				"disabledSupport":    true,
				"dataSupport":        true,
				"resolveSupport":     map[string]any{"properties": []string{"edit"}},
			},
			"codeLens":         map[string]any{"dynamicRegistration": true},
			"formatting":       dynamic,
			"rangeFormatting":  dynamic,
			"onTypeFormatting": dynamic,
			"rename": map[string]any{
				"dynamicRegistration":           true,
				"prepareSupport":                true,
				"prepareSupportDefaultBehavior": 1,
			},
			"publishDiagnostics": map[string]any{
				"relatedInformation":     true,
				"tagSupport":             map[string]any{"valueSet": []int{DiagnosticTagUnnecessary, DiagnosticTagDeprecated}},
				"versionSupport":         true,
				"codeDescriptionSupport": true,
				"dataSupport":            true,
			},
			"diagnostic": map[string]any{"dynamicRegistration": true, "relatedDocumentSupport": false},
			"foldingRange": map[string]any{
				"dynamicRegistration": true,
				"lineFoldingOnly":     true,
				"foldingRangeKind":    map[string]any{"valueSet": []string{"comment", "imports", "region"}},
				"foldingRange":        map[string]any{"collapsedText": true},
			},
			"selectionRange": dynamic,
			"semanticTokens": map[string]any{
				"dynamicRegistration": true,
				"requests":            map[string]any{"range": true, "full": map[string]any{"delta": true}},
				"tokenTypes": []string{
					"namespace", "type", "class", "enum", "interface", "struct", "typeParameter", "parameter",
					"variable", "property", "enumMember", "event", "function", "method", "macro", "keyword",
					"modifier", "comment", "string", "number", "regexp", "operator", "decorator",
				},
				"tokenModifiers": []string{
					"declaration", "definition", "readonly", "static", "deprecated", "abstract", "async",
					"modification", "documentation", "defaultLibrary",
				},
				"formats":                 []string{"relative"},
				"overlappingTokenSupport": false,
				"multilineTokenSupport":   false,
				"serverCancelSupport":     true,
				"augmentsSyntaxTokens":    true,
			},
			"inlayHint": map[string]any{
				"dynamicRegistration": true,
				"resolveSupport": map[string]any{
					"properties": []string{"tooltip", "textEdits", "label.tooltip", "label.location", "label.command"},
				},
			},
		},
	}
}

// ClientInfo names the client in the initialize request.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ServerInfo names the server in the initialize result.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// InitializeParams are the params of the initialize request. RootURI and WorkspaceFolders are null
// (nil) when no folder is open.
type InitializeParams struct {
	ProcessID             int                `json:"processId"`
	ClientInfo            *ClientInfo        `json:"clientInfo,omitempty"`
	Locale                string             `json:"locale,omitempty"`
	RootURI               *string            `json:"rootUri"`
	InitializationOptions json.RawMessage    `json:"initializationOptions,omitempty"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	Trace                 string             `json:"trace,omitempty"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders"`
}

// InitializeResult is the answer to initialize.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

// Provider is a capability a server states as a boolean or as an options object. Enabled is true for
// true and for an object; Options keeps the object as JSON.
type Provider struct {
	Enabled bool
	Options json.RawMessage
}

// UnmarshalJSON reads null, a boolean and an object.
func (p *Provider) UnmarshalJSON(data []byte) error {
	*p = Provider{}
	switch {
	case isNull(data):
	case isBool(data):
		p.Enabled = string(trimmed(data)) == "true"
	default:
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return errors.NewErr("CapabilitiesInvalid", err)
		}
		p.Enabled = true
		p.Options = append(json.RawMessage(nil), data...)
	}
	return nil
}

// MarshalJSON writes the options object, or the boolean.
func (p Provider) MarshalJSON() ([]byte, error) {
	if p.Options != nil {
		return p.Options, nil
	}
	return json.Marshal(p.Enabled)
}

// Bool reads a boolean option such as "prepareProvider" or "resolveProvider"; false when it is absent.
func (p Provider) Bool(key string) bool {
	var obj map[string]json.RawMessage
	if p.Options == nil || json.Unmarshal(p.Options, &obj) != nil {
		return false
	}
	return string(trimmed(obj[key])) == "true"
}

// Strings reads a list of strings option such as "codeActionKinds"; nil when it is absent.
func (p Provider) Strings(key string) []string {
	var obj map[string]json.RawMessage
	if p.Options == nil || json.Unmarshal(p.Options, &obj) != nil {
		return nil
	}
	var out []string
	if json.Unmarshal(obj[key], &out) != nil {
		return nil
	}
	return out
}

// TextDocumentSyncOptions says how the server wants documents: a bare number in the protocol is the
// Change kind with OpenClose true.
type TextDocumentSyncOptions struct {
	OpenClose         bool
	Change            int
	WillSave          bool
	WillSaveWaitUntil bool
	// Save is true when the server wants didSave, and SaveIncludeText when it wants the text with it.
	Save            bool
	SaveIncludeText bool
}

// UnmarshalJSON reads the number and the object.
func (o *TextDocumentSyncOptions) UnmarshalJSON(data []byte) error {
	*o = TextDocumentSyncOptions{}
	if isNull(data) {
		return nil
	}
	var kind int
	if err := json.Unmarshal(data, &kind); err == nil {
		o.OpenClose, o.Change = true, kind
		return nil
	}
	var raw struct {
		OpenClose         bool     `json:"openClose"`
		Change            int      `json:"change"`
		WillSave          bool     `json:"willSave"`
		WillSaveWaitUntil bool     `json:"willSaveWaitUntil"`
		Save              Provider `json:"save"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.NewErr("CapabilitiesInvalid", err)
	}
	*o = TextDocumentSyncOptions{
		OpenClose: raw.OpenClose, Change: raw.Change, WillSave: raw.WillSave, WillSaveWaitUntil: raw.WillSaveWaitUntil,
		Save: raw.Save.Enabled, SaveIncludeText: raw.Save.Bool("includeText"),
	}
	return nil
}

// CompletionOptions are the options of completionProvider.
type CompletionOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	AllCommitCharacters []string `json:"allCommitCharacters,omitempty"`
	ResolveProvider     bool     `json:"resolveProvider,omitempty"`
}

// SignatureHelpOptions are the options of signatureHelpProvider.
type SignatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	RetriggerCharacters []string `json:"retriggerCharacters,omitempty"`
}

// CodeLensOptions are the options of codeLensProvider.
type CodeLensOptions struct {
	ResolveProvider bool `json:"resolveProvider,omitempty"`
}

// OnTypeFormattingOptions are the options of documentOnTypeFormattingProvider.
type OnTypeFormattingOptions struct {
	FirstTriggerCharacter string   `json:"firstTriggerCharacter"`
	MoreTriggerCharacter  []string `json:"moreTriggerCharacter,omitempty"`
}

// ExecuteCommandOptions are the options of executeCommandProvider.
type ExecuteCommandOptions struct {
	Commands []string `json:"commands"`
}

// SemanticTokensOptions are the options of semanticTokensProvider; Range and Full are booleans or
// objects, and an object for Full may say "delta".
type SemanticTokensOptions struct {
	Legend SemanticTokensLegend `json:"legend"`
	Range  Provider             `json:"range"`
	Full   Provider             `json:"full"`
}

// DiagnosticOptions are the options of diagnosticProvider (pull diagnostics).
type DiagnosticOptions struct {
	Identifier            string `json:"identifier,omitempty"`
	InterFileDependencies bool   `json:"interFileDependencies"`
	WorkspaceDiagnostics  bool   `json:"workspaceDiagnostics"`
}

// ServerCapabilities are what the server can do, as far as the features of area 3 look at them.
type ServerCapabilities struct {
	// PositionEncoding is the encoding the server chose; empty means UTF-16.
	PositionEncoding string                   `json:"positionEncoding,omitempty"`
	TextDocumentSync *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`

	CompletionProvider    *CompletionOptions    `json:"completionProvider,omitempty"`
	HoverProvider         Provider              `json:"hoverProvider"`
	SignatureHelpProvider *SignatureHelpOptions `json:"signatureHelpProvider,omitempty"`

	DeclarationProvider       Provider `json:"declarationProvider"`
	DefinitionProvider        Provider `json:"definitionProvider"`
	TypeDefinitionProvider    Provider `json:"typeDefinitionProvider"`
	ImplementationProvider    Provider `json:"implementationProvider"`
	ReferencesProvider        Provider `json:"referencesProvider"`
	DocumentHighlightProvider Provider `json:"documentHighlightProvider"`
	DocumentSymbolProvider    Provider `json:"documentSymbolProvider"`
	WorkspaceSymbolProvider   Provider `json:"workspaceSymbolProvider"`

	// CodeActionProvider options: Strings("codeActionKinds"), Bool("resolveProvider").
	CodeActionProvider Provider         `json:"codeActionProvider"`
	CodeLensProvider   *CodeLensOptions `json:"codeLensProvider,omitempty"`

	DocumentFormattingProvider       Provider                 `json:"documentFormattingProvider"`
	DocumentRangeFormattingProvider  Provider                 `json:"documentRangeFormattingProvider"`
	DocumentOnTypeFormattingProvider *OnTypeFormattingOptions `json:"documentOnTypeFormattingProvider,omitempty"`

	// RenameProvider options: Bool("prepareProvider").
	RenameProvider Provider `json:"renameProvider"`

	FoldingRangeProvider   Provider               `json:"foldingRangeProvider"`
	SelectionRangeProvider Provider               `json:"selectionRangeProvider"`
	ExecuteCommandProvider *ExecuteCommandOptions `json:"executeCommandProvider,omitempty"`
	SemanticTokensProvider *SemanticTokensOptions `json:"semanticTokensProvider,omitempty"`
	InlayHintProvider      Provider               `json:"inlayHintProvider"`
	DiagnosticProvider     *DiagnosticOptions     `json:"diagnosticProvider,omitempty"`

	// Workspace and Experimental are kept as the server sent them.
	Workspace    json.RawMessage `json:"workspace,omitempty"`
	Experimental json.RawMessage `json:"experimental,omitempty"`
}

// Encoding is the position encoding the server chose.
func (c ServerCapabilities) Encoding() (Encoding, bool) {
	if c.PositionEncoding == "" {
		return UTF16, true
	}
	return ParseEncoding(c.PositionEncoding)
}
