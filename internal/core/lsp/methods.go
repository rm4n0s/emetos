package lsp

// The methods of the protocol that area 3 uses (LSP 3.17).
const (
	MethodInitialize  = "initialize"
	MethodInitialized = "initialized"
	MethodShutdown    = "shutdown"
	MethodExit        = "exit"

	MethodCancelRequest = "$/cancelRequest"
	MethodProgress      = "$/progress"

	MethodDidOpen     = "textDocument/didOpen"
	MethodDidChange   = "textDocument/didChange"
	MethodWillSave    = "textDocument/willSave"
	MethodWillSaveWU  = "textDocument/willSaveWaitUntil"
	MethodDidSave     = "textDocument/didSave"
	MethodDidClose    = "textDocument/didClose"
	MethodPublishDiag = "textDocument/publishDiagnostics"

	MethodCompletion          = "textDocument/completion"
	MethodCompletionResolve   = "completionItem/resolve"
	MethodHover               = "textDocument/hover"
	MethodSignatureHelp       = "textDocument/signatureHelp"
	MethodDeclaration         = "textDocument/declaration"
	MethodDefinition          = "textDocument/definition"
	MethodTypeDefinition      = "textDocument/typeDefinition"
	MethodImplementation      = "textDocument/implementation"
	MethodReferences          = "textDocument/references"
	MethodDocumentHighlight   = "textDocument/documentHighlight"
	MethodDocumentSymbol      = "textDocument/documentSymbol"
	MethodCodeAction          = "textDocument/codeAction"
	MethodCodeActionResolve   = "codeAction/resolve"
	MethodCodeLens            = "textDocument/codeLens"
	MethodCodeLensResolve     = "codeLens/resolve"
	MethodFormatting          = "textDocument/formatting"
	MethodRangeFormatting     = "textDocument/rangeFormatting"
	MethodOnTypeFormatting    = "textDocument/onTypeFormatting"
	MethodRename              = "textDocument/rename"
	MethodPrepareRename       = "textDocument/prepareRename"
	MethodFoldingRange        = "textDocument/foldingRange"
	MethodSelectionRange      = "textDocument/selectionRange"
	MethodSemanticTokensFull  = "textDocument/semanticTokens/full"
	MethodSemanticTokensDelta = "textDocument/semanticTokens/full/delta"
	MethodSemanticTokensRange = "textDocument/semanticTokens/range"
	MethodInlayHint           = "textDocument/inlayHint"
	MethodInlayHintResolve    = "inlayHint/resolve"
	MethodDocumentDiagnostic  = "textDocument/diagnostic"

	MethodWorkspaceSymbol        = "workspace/symbol"
	MethodExecuteCommand         = "workspace/executeCommand"
	MethodApplyEdit              = "workspace/applyEdit"
	MethodConfiguration          = "workspace/configuration"
	MethodWorkspaceFolders       = "workspace/workspaceFolders"
	MethodDidChangeConfiguration = "workspace/didChangeConfiguration"
	MethodDidChangeWatchedFiles  = "workspace/didChangeWatchedFiles"
	MethodSemanticTokensRefresh  = "workspace/semanticTokens/refresh"
	MethodCodeLensRefresh        = "workspace/codeLens/refresh"
	MethodInlayHintRefresh       = "workspace/inlayHint/refresh"
	MethodDiagnosticRefresh      = "workspace/diagnostic/refresh"

	MethodShowMessage        = "window/showMessage"
	MethodShowMessageRequest = "window/showMessageRequest"
	MethodShowDocument       = "window/showDocument"
	MethodLogMessage         = "window/logMessage"
	MethodProgressCreate     = "window/workDoneProgress/create"
	MethodProgressCancel     = "window/workDoneProgress/cancel"

	MethodRegisterCapability   = "client/registerCapability"
	MethodUnregisterCapability = "client/unregisterCapability"
)

// TextDocumentSyncKind says how a server wants the changes of a document (LSP "TextDocumentSyncKind").
const (
	SyncNone        = 0
	SyncFull        = 1
	SyncIncremental = 2
)

// DiagnosticSeverity values and DiagnosticTag values.
const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4

	DiagnosticTagUnnecessary = 1
	DiagnosticTagDeprecated  = 2
)

// The values of the small enumerations of the protocol, by the name of each one.
const (
	// CompletionTriggerKind.
	TriggerInvoked                  = 1
	TriggerCharacter                = 2
	TriggerForIncompleteCompletions = 3

	// InsertTextFormat and InsertTextMode.
	InsertTextFormatPlainText       = 1
	InsertTextFormatSnippet         = 2
	InsertTextModeAsIs              = 1
	InsertTextModeAdjustIndentation = 2

	// CompletionItemTag.
	CompletionItemTagDeprecated = 1

	// SignatureHelpTriggerKind.
	SignatureHelpInvoked       = 1
	SignatureHelpTriggerChar   = 2
	SignatureHelpContentChange = 3

	// CodeActionTriggerKind.
	CodeActionTriggerInvoked   = 1
	CodeActionTriggerAutomatic = 2

	// DocumentHighlightKind.
	DocumentHighlightText  = 1
	DocumentHighlightRead  = 2
	DocumentHighlightWrite = 3

	// InlayHintKind.
	InlayHintKindType      = 1
	InlayHintKindParameter = 2

	// FileChangeType.
	FileCreated = 1
	FileChanged = 2
	FileDeleted = 3

	// TextDocumentSaveReason.
	SaveReasonManual     = 1
	SaveReasonAfterDelay = 2
	SaveReasonFocusOut   = 3

	// MessageType.
	MessageTypeError   = 1
	MessageTypeWarning = 2
	MessageTypeInfo    = 3
	MessageTypeLog     = 4
)

// CompletionItemKind values (LSP "CompletionItemKind").
const (
	CompletionText = iota + 1
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

// SymbolKind values (LSP "SymbolKind").
const (
	SymbolFile = iota + 1
	SymbolModule
	SymbolNamespace
	SymbolPackage
	SymbolClass
	SymbolMethod
	SymbolProperty
	SymbolField
	SymbolConstructor
	SymbolEnum
	SymbolInterface
	SymbolFunction
	SymbolVariable
	SymbolConstant
	SymbolString
	SymbolNumber
	SymbolBoolean
	SymbolArray
	SymbolObject
	SymbolKey
	SymbolNull
	SymbolEnumMember
	SymbolStruct
	SymbolEvent
	SymbolOperator
	SymbolTypeParameter
)

// CodeActionKind values (the strings of LSP "CodeActionKind").
const (
	KindQuickFix              = "quickfix"
	KindRefactor              = "refactor"
	KindRefactorExtract       = "refactor.extract"
	KindRefactorInline        = "refactor.inline"
	KindRefactorRewrite       = "refactor.rewrite"
	KindSource                = "source"
	KindSourceOrganizeImports = "source.organizeImports"
	KindSourceFixAll          = "source.fixAll"
)
