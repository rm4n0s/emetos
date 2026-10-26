package langfeatures

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// Server is the part of an lsp.Client that a ServerProvider uses; *lsp.Client is one.
type Server interface {
	// Call sends a request and decodes its result.
	Call(ctx context.Context, method string, params, result any) error
	// Capabilities are the capabilities the server returned from initialize.
	Capabilities() lsp.ServerCapabilities
	// Encoding is the position encoding negotiated with the server.
	Encoding() lsp.Encoding
	// Registered lists the dynamic registrations of a method.
	Registered(method string) []lsp.Registration
}

// ServerProvider answers the feature requests of the documents with a language server (SPEC §9.12). One value implements
// every provider interface of this package; Register adds it to a registry. The server is asked for the current client at
// each request, so a server that restarts keeps its registration, and a request while there is no client (starting,
// crashed) answers nothing. A feature the server does not offer (neither in its capabilities nor by registration) answers
// nothing without asking.
type ServerProvider struct {
	// Name is the id of the server; the commands of its code actions are run through it.
	Name string
	// Client gives the client of the server, or nil while there is none. It is called from the goroutines of the requests.
	Client func() Server
	// Texts gives the text of the other documents that answers refer to; see TextSource.
	Texts TextSource
}

// Register adds the provider for every feature to the registry, for the documents the selector matches, under the owner (the
// id of the extension), and returns the function that removes them all. The characters that trigger completion and
// signature help are those the server declared: the registry asks the provider for them (CompletionTriggers and
// SignatureTriggers) when it is asked, so they are right once the server is up.
func (sp *ServerProvider) Register(r *Registry, owner string, sel Selector) (remove func()) {
	removers := []func(){
		r.RegisterCompletion(owner, sel, sp),
		r.RegisterHover(owner, sel, sp),
		r.RegisterSignatureHelp(owner, sel, sp),
		r.RegisterNavigation(owner, sel, sp),
		r.RegisterReferences(owner, sel, sp),
		r.RegisterSymbols(owner, sel, sp),
		r.RegisterHighlights(owner, sel, sp),
		r.RegisterCodeActions(owner, sel, sp),
		r.RegisterFormatting(owner, sel, sp),
		r.RegisterRename(owner, sel, sp),
		r.RegisterOnTypeFormatting(owner, sel, sp),
		r.RegisterWorkspaceSymbols(owner, sp),
		r.RegisterCodeLens(owner, sel, sp),
		r.RegisterInlayHints(owner, sel, sp),
		r.RegisterSemanticTokens(owner, sel, sp),
		r.RegisterFoldingRanges(owner, sel, sp),
		r.RegisterSelectionRanges(owner, sel, sp),
	}
	return func() {
		for _, rm := range removers {
			rm()
		}
	}
}

// server is the client now, or nil.
func (sp *ServerProvider) server() Server {
	if sp.Client == nil {
		return nil
	}
	return sp.Client()
}

// offers tells whether the server offers a feature: its capability is on, or it registered the method.
func offers(c Server, enabled bool, method string) bool {
	return enabled || len(c.Registered(method)) > 0
}

// textDocument names the document to the server.
func textDocument(doc Document) lsp.TextDocumentIdentifier {
	return lsp.TextDocumentIdentifier{URI: lsp.DocumentURI(doc.URI)}
}

// ProvideCompletion implements CompletionProvider.
func (sp *ServerProvider) ProvideCompletion(ctx context.Context, doc Document, pos Position, trigger CompletionTrigger) (CompletionList, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().CompletionProvider != nil, lsp.MethodCompletion) {
		return CompletionList{}, nil
	}
	enc := c.Encoding()
	params := lsp.CompletionParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos)}
	switch trigger.Kind {
	case TriggerCharacter:
		params.Context = &lsp.CompletionContext{TriggerKind: lsp.TriggerCharacter, TriggerCharacter: trigger.Character}
	case TriggerIncomplete:
		params.Context = &lsp.CompletionContext{TriggerKind: lsp.TriggerForIncompleteCompletions}
	default:
		params.Context = &lsp.CompletionContext{TriggerKind: lsp.TriggerInvoked}
	}
	var list lsp.CompletionList
	if err := c.Call(ctx, lsp.MethodCompletion, params, &list); err != nil {
		return CompletionList{}, err
	}
	out := CompletionFromLSP(enc, doc, list)
	for i := range out.Items {
		if out.Items[i].Command != nil {
			out.Items[i].Command.Owner = sp.Name
		}
	}
	return out, nil
}

// CanResolve implements CompletionResolver: the server offers completionItem/resolve and sent the item.
func (sp *ServerProvider) CanResolve(item CompletionItem) bool {
	c := sp.server()
	if c == nil || item.server == nil {
		return false
	}
	o := c.Capabilities().CompletionProvider
	return (o != nil && o.ResolveProvider) || len(c.Registered(lsp.MethodCompletionResolve)) > 0
}

// ResolveCompletion implements CompletionResolver: the item goes back to the server, which fills in its documentation, its
// detail and its additional edits. The item is returned as it was when the server cannot resolve it.
func (sp *ServerProvider) ResolveCompletion(ctx context.Context, doc Document, item CompletionItem) (CompletionItem, error) {
	if !sp.CanResolve(item) {
		return item, nil
	}
	c := sp.server()
	if c == nil {
		return item, nil
	}
	var resolved lsp.CompletionItem
	if err := c.Call(ctx, lsp.MethodCompletionResolve, *item.server, &resolved); err != nil {
		return item, err
	}
	list := CompletionFromLSP(c.Encoding(), doc, lsp.CompletionList{Items: []lsp.CompletionItem{resolved}})
	if len(list.Items) == 0 {
		return item, nil
	}
	merged := MergeResolvedCompletion(item, list.Items[0])
	if merged.Command != nil && merged.Command.Owner == "" {
		merged.Command.Owner = sp.Name
	}
	return merged, nil
}

// ExecuteCommand runs a command of the server (workspace/executeCommand), for the command of a completion item or a code
// action. It does nothing while there is no server.
func (sp *ServerProvider) ExecuteCommand(ctx context.Context, cmd Command) error {
	c := sp.server()
	if c == nil {
		return nil
	}
	var result json.RawMessage
	return c.Call(ctx, lsp.MethodExecuteCommand, lsp.ExecuteCommandParams{Command: cmd.ID, Arguments: cmd.Arguments}, &result)
}

// ProvideHover implements HoverProvider.
func (sp *ServerProvider) ProvideHover(ctx context.Context, doc Document, pos Position) (*Hover, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().HoverProvider.Enabled, lsp.MethodHover) {
		return nil, nil
	}
	enc := c.Encoding()
	var h *lsp.Hover
	err := c.Call(ctx, lsp.MethodHover, lsp.TextDocumentPositionParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos)}, &h)
	if err != nil {
		return nil, err
	}
	return HoverFromLSP(enc, doc, h), nil
}

// ProvideSignatureHelp implements SignatureHelpProvider.
func (sp *ServerProvider) ProvideSignatureHelp(ctx context.Context, doc Document, pos Position, sc SignatureContext) (*SignatureHelp, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().SignatureHelpProvider != nil, lsp.MethodSignatureHelp) {
		return nil, nil
	}
	enc := c.Encoding()
	kind := sc.Kind
	if kind == 0 {
		kind = SignatureInvoke
		if sc.Character != "" {
			kind = SignatureTriggerCharacter
		}
	}
	params := lsp.SignatureHelpParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos), Context: &lsp.SignatureHelpContext{
		TriggerKind: int(kind), TriggerCharacter: sc.Character, IsRetrigger: sc.IsRetrigger,
	}}
	if sc.Active != nil && sc.Active.server != nil {
		params.Context.ActiveSignatureHelp = activeForServer(sc.Active)
	}
	var h *lsp.SignatureHelp
	if err := c.Call(ctx, lsp.MethodSignatureHelp, params, &h); err != nil {
		return nil, err
	}
	out := SignatureHelpFromLSP(h)
	return out, nil
}

// activeForServer is the help the server gets back as the active one: the answer it sent, with the signature the user moved
// to (Up and Down) as the active signature.
func activeForServer(h *SignatureHelp) *lsp.SignatureHelp {
	out := *h.server
	active := h.ActiveSignature
	out.ActiveSignature = &active
	return &out
}

// ProvideLocations implements NavigationProvider.
func (sp *ServerProvider) ProvideLocations(ctx context.Context, doc Document, pos Position, kind NavKind) ([]LocationLink, error) {
	c := sp.server()
	if c == nil {
		return nil, nil
	}
	caps := c.Capabilities()
	method, enabled := lsp.MethodDefinition, caps.DefinitionProvider.Enabled
	switch kind {
	case NavDeclaration:
		method, enabled = lsp.MethodDeclaration, caps.DeclarationProvider.Enabled
	case NavTypeDefinition:
		method, enabled = lsp.MethodTypeDefinition, caps.TypeDefinitionProvider.Enabled
	case NavImplementation:
		method, enabled = lsp.MethodImplementation, caps.ImplementationProvider.Enabled
	}
	if !offers(c, enabled, method) {
		return nil, nil
	}
	enc := c.Encoding()
	var links lsp.LocationLinks
	err := c.Call(ctx, method, lsp.TextDocumentPositionParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos)}, &links)
	if err != nil {
		return nil, err
	}
	return LocationsFromLSP(ctx, enc, doc, sp.Texts, links), nil
}

// ProvideReferences implements ReferenceProvider.
func (sp *ServerProvider) ProvideReferences(ctx context.Context, doc Document, pos Position, includeDeclaration bool) ([]Location, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().ReferencesProvider.Enabled, lsp.MethodReferences) {
		return nil, nil
	}
	enc := c.Encoding()
	params := lsp.ReferenceParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos), Context: lsp.ReferenceContext{IncludeDeclaration: includeDeclaration}}
	var locs []lsp.Location
	if err := c.Call(ctx, lsp.MethodReferences, params, &locs); err != nil {
		return nil, err
	}
	return ReferencesFromLSP(ctx, enc, doc, sp.Texts, locs), nil
}

// ProvideDocumentSymbols implements SymbolProvider.
func (sp *ServerProvider) ProvideDocumentSymbols(ctx context.Context, doc Document) ([]DocumentSymbol, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().DocumentSymbolProvider.Enabled, lsp.MethodDocumentSymbol) {
		return nil, nil
	}
	var syms lsp.DocumentSymbols
	if err := c.Call(ctx, lsp.MethodDocumentSymbol, lsp.DocumentSymbolParams{TextDocument: textDocument(doc)}, &syms); err != nil {
		return nil, err
	}
	return SymbolsFromLSP(c.Encoding(), doc, syms), nil
}

// ProvideDocumentHighlights implements HighlightProvider.
func (sp *ServerProvider) ProvideDocumentHighlights(ctx context.Context, doc Document, pos Position) ([]DocumentHighlight, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().DocumentHighlightProvider.Enabled, lsp.MethodDocumentHighlight) {
		return nil, nil
	}
	enc := c.Encoding()
	var hs []lsp.DocumentHighlight
	err := c.Call(ctx, lsp.MethodDocumentHighlight, lsp.TextDocumentPositionParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos)}, &hs)
	if err != nil {
		return nil, err
	}
	return HighlightsFromLSP(enc, doc, hs), nil
}

// ProvideCodeActions implements CodeActionProvider.
func (sp *ServerProvider) ProvideCodeActions(ctx context.Context, doc Document, req CodeActionRequest) ([]CodeAction, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().CodeActionProvider.Enabled, lsp.MethodCodeAction) {
		return nil, nil
	}
	enc := c.Encoding()
	cx := lsp.CodeActionContext{Diagnostics: make([]lsp.Diagnostic, 0, len(req.Diagnostics)), Only: req.Only, TriggerKind: lsp.CodeActionTriggerInvoked}
	if req.Automatic {
		cx.TriggerKind = lsp.CodeActionTriggerAutomatic
	}
	for _, d := range req.Diagnostics {
		var ld lsp.Diagnostic
		if len(d.Raw) > 0 && json.Unmarshal(d.Raw, &ld) == nil {
			// the diagnostic as the server sent it, at the place it is now
			ld.Range = enc.RangeToLSP(doc.Text, d.Range)
			cx.Diagnostics = append(cx.Diagnostics, ld)
			continue
		}
		ld = lsp.Diagnostic{Range: enc.RangeToLSP(doc.Text, d.Range), Message: d.Message, Source: d.Source, Severity: int(d.Severity) + 1}
		if d.Code != "" {
			ld.Code, _ = json.Marshal(d.Code)
		}
		cx.Diagnostics = append(cx.Diagnostics, ld)
	}
	params := lsp.CodeActionParams{TextDocument: textDocument(doc), Range: enc.RangeToLSP(doc.Text, req.Range), Context: cx}
	var actions lsp.CodeActions
	if err := c.Call(ctx, lsp.MethodCodeAction, params, &actions); err != nil {
		return nil, err
	}
	return CodeActionsFromLSP(enc, doc, sp.Name, actions), nil
}

// CodeActionKinds implements CodeActionKinds: the kinds the server declared.
func (sp *ServerProvider) CodeActionKinds() []string {
	if c := sp.server(); c != nil {
		return slices.Clone(c.Capabilities().CodeActionProvider.Strings("codeActionKinds"))
	}
	return nil
}

// CanResolveAction implements CodeActionResolver: the server offers codeAction/resolve and sent the action.
func (sp *ServerProvider) CanResolveAction(a CodeAction) bool {
	c := sp.server()
	if c == nil || a.server == nil {
		return false
	}
	return c.Capabilities().CodeActionProvider.Bool("resolveProvider") || len(c.Registered(lsp.MethodCodeActionResolve)) > 0
}

// ResolveCodeAction implements CodeActionResolver: the action goes back to the server, which fills in its edit.
func (sp *ServerProvider) ResolveCodeAction(ctx context.Context, a CodeAction) (CodeAction, error) {
	c := sp.server()
	if c == nil || !sp.CanResolveAction(a) {
		return a, nil
	}
	var resolved lsp.CodeAction
	if err := c.Call(ctx, lsp.MethodCodeActionResolve, *a.server, &resolved); err != nil {
		return a, err
	}
	if resolved.Edit != nil {
		a.Edit = FromLSPEdit(c.Encoding(), *resolved.Edit)
	}
	return a, nil
}

// formattingOptions are the options of the protocol for ours.
func formattingOptions(o FormattingOptions) lsp.FormattingOptions {
	return lsp.FormattingOptions{TabSize: o.TabSize, InsertSpaces: o.InsertSpaces}
}

// ProvideFormatting implements FormattingProvider.
func (sp *ServerProvider) ProvideFormatting(ctx context.Context, doc Document, opts FormattingOptions) ([]TextEdit, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().DocumentFormattingProvider.Enabled, lsp.MethodFormatting) {
		return nil, nil
	}
	var edits []lsp.TextEdit
	params := lsp.DocumentFormattingParams{TextDocument: textDocument(doc), Options: formattingOptions(opts)}
	if err := c.Call(ctx, lsp.MethodFormatting, params, &edits); err != nil {
		return nil, err
	}
	return TextEditsFromLSP(c.Encoding(), doc, edits), nil
}

// ProvideRangeFormatting implements RangeFormattingProvider.
func (sp *ServerProvider) ProvideRangeFormatting(ctx context.Context, doc Document, r Range, opts FormattingOptions) ([]TextEdit, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().DocumentRangeFormattingProvider.Enabled, lsp.MethodRangeFormatting) {
		return nil, nil
	}
	enc := c.Encoding()
	var edits []lsp.TextEdit
	params := lsp.DocumentRangeFormattingParams{TextDocument: textDocument(doc), Range: enc.RangeToLSP(doc.Text, r), Options: formattingOptions(opts)}
	if err := c.Call(ctx, lsp.MethodRangeFormatting, params, &edits); err != nil {
		return nil, err
	}
	return TextEditsFromLSP(enc, doc, edits), nil
}

// OnTypeTriggers implements OnTypeFormattingProvider: the first trigger character the server declared and the others.
func (sp *ServerProvider) OnTypeTriggers() []string {
	c := sp.server()
	if c == nil {
		return nil
	}
	o := c.Capabilities().DocumentOnTypeFormattingProvider
	if o == nil {
		return nil
	}
	return append([]string{o.FirstTriggerCharacter}, o.MoreTriggerCharacter...)
}

// ProvideOnTypeFormatting implements OnTypeFormattingProvider.
func (sp *ServerProvider) ProvideOnTypeFormatting(ctx context.Context, doc Document, pos Position, ch string, opts FormattingOptions) ([]TextEdit, error) {
	c := sp.server()
	if c == nil || !sp.Offers(FeatureOnTypeFormatting) {
		return nil, nil
	}
	enc := c.Encoding()
	var edits []lsp.TextEdit
	params := lsp.DocumentOnTypeFormattingParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos), Ch: ch, Options: formattingOptions(opts)}
	if err := c.Call(ctx, lsp.MethodOnTypeFormatting, params, &edits); err != nil {
		return nil, err
	}
	return TextEditsFromLSP(enc, doc, edits), nil
}

// PrepareRename implements RenameProvider. A server that cannot prepare answers nil, which asks the editor to use the word
// at the position; a server that refuses (null) gives the error NotRenameable.
func (sp *ServerProvider) PrepareRename(ctx context.Context, doc Document, pos Position) (*PrepareRename, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().RenameProvider.Enabled, lsp.MethodRename) {
		return nil, nil
	}
	if !c.Capabilities().RenameProvider.Bool("prepareProvider") {
		return nil, nil
	}
	enc := c.Encoding()
	var res *lsp.PrepareRenameResult
	err := c.Call(ctx, lsp.MethodPrepareRename, lsp.TextDocumentPositionParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos)}, &res)
	if err != nil {
		return nil, err
	}
	switch {
	case res == nil || (res.Range == nil && !res.DefaultBehavior):
		return nil, errors.New("NotRenameable", "the symbol at the position cannot be renamed", "uri", doc.URI.String(), "line", pos.Line, "col", pos.Col)
	case res.DefaultBehavior:
		return &PrepareRename{DefaultBehavior: true}, nil
	}
	cv := newConverter(ctx, enc, doc, nil)
	return &PrepareRename{Range: cv.own(*res.Range), Placeholder: res.Placeholder}, nil
}

// ProvideRename implements RenameProvider.
func (sp *ServerProvider) ProvideRename(ctx context.Context, doc Document, pos Position, newName string) (*WorkspaceEdit, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().RenameProvider.Enabled, lsp.MethodRename) {
		return nil, nil
	}
	enc := c.Encoding()
	var we *lsp.WorkspaceEdit
	params := lsp.RenameParams{TextDocument: textDocument(doc), Position: enc.PositionToLSP(doc.Text, pos), NewName: newName}
	if err := c.Call(ctx, lsp.MethodRename, params, &we); err != nil {
		return nil, err
	}
	if we == nil {
		return nil, nil
	}
	return FromLSPEdit(enc, *we), nil
}

// CompletionTriggers are the trigger characters the server declared; none while there is no server.
func (sp *ServerProvider) CompletionTriggers() []string {
	if c := sp.server(); c != nil && c.Capabilities().CompletionProvider != nil {
		return c.Capabilities().CompletionProvider.TriggerCharacters
	}
	return nil
}

// SignatureCharacters are the trigger characters and the retrigger characters the server declared for signature help; none
// while there is no server.
func (sp *ServerProvider) SignatureCharacters() (triggers, retriggers []string) {
	if c := sp.server(); c != nil && c.Capabilities().SignatureHelpProvider != nil {
		o := c.Capabilities().SignatureHelpProvider
		return slices.Clone(o.TriggerCharacters), slices.Clone(o.RetriggerCharacters)
	}
	return nil, nil
}

// SignatureTriggers are the trigger and retrigger characters the server declared for signature help.
func (sp *ServerProvider) SignatureTriggers() []string {
	if c := sp.server(); c != nil && c.Capabilities().SignatureHelpProvider != nil {
		o := c.Capabilities().SignatureHelpProvider
		return append(append([]string(nil), o.TriggerCharacters...), o.RetriggerCharacters...)
	}
	return nil
}

// Offers implements featureOffers: whether the server offers a feature, by its capabilities or a registration. The editor
// asks it for the keys editorHasDefinitionProvider and its kin, so a server that cannot find references does not enable
// Go to References.
func (sp *ServerProvider) Offers(f Feature) bool {
	c := sp.server()
	if c == nil {
		return false
	}
	caps := c.Capabilities()
	switch f {
	case FeatureDefinition:
		return offers(c, caps.DefinitionProvider.Enabled, lsp.MethodDefinition)
	case FeatureDeclaration:
		return offers(c, caps.DeclarationProvider.Enabled, lsp.MethodDeclaration)
	case FeatureTypeDefinition:
		return offers(c, caps.TypeDefinitionProvider.Enabled, lsp.MethodTypeDefinition)
	case FeatureImplementation:
		return offers(c, caps.ImplementationProvider.Enabled, lsp.MethodImplementation)
	case FeatureReferences:
		return offers(c, caps.ReferencesProvider.Enabled, lsp.MethodReferences)
	case FeatureDocumentSymbols:
		return offers(c, caps.DocumentSymbolProvider.Enabled, lsp.MethodDocumentSymbol)
	case FeatureDocumentHighlights:
		return offers(c, caps.DocumentHighlightProvider.Enabled, lsp.MethodDocumentHighlight)
	case FeatureWorkspaceSymbols:
		return offers(c, caps.WorkspaceSymbolProvider.Enabled, lsp.MethodWorkspaceSymbol)
	case FeatureCodeActions:
		return offers(c, caps.CodeActionProvider.Enabled, lsp.MethodCodeAction)
	case FeatureFormatting:
		return offers(c, caps.DocumentFormattingProvider.Enabled, lsp.MethodFormatting)
	case FeatureRangeFormatting:
		return offers(c, caps.DocumentRangeFormattingProvider.Enabled, lsp.MethodRangeFormatting)
	case FeatureOnTypeFormatting:
		return caps.DocumentOnTypeFormattingProvider != nil || len(c.Registered(lsp.MethodOnTypeFormatting)) > 0
	case FeatureRename:
		return offers(c, caps.RenameProvider.Enabled, lsp.MethodRename)
	case FeatureCodeLens:
		return caps.CodeLensProvider != nil || len(c.Registered(lsp.MethodCodeLens)) > 0
	case FeatureInlayHints:
		return offers(c, caps.InlayHintProvider.Enabled, lsp.MethodInlayHint)
	case FeatureSemanticTokens:
		return caps.SemanticTokensProvider != nil || len(c.Registered(lsp.MethodSemanticTokensFull)) > 0
	case FeatureFoldingRanges:
		return offers(c, caps.FoldingRangeProvider.Enabled, lsp.MethodFoldingRange)
	case FeatureSelectionRanges:
		return offers(c, caps.SelectionRangeProvider.Enabled, lsp.MethodSelectionRange)
	}
	return false
}

// ProvideWorkspaceSymbols implements WorkspaceSymbolProvider (workspace/symbol). A symbol whose location has no range is
// left for workspaceSymbol/resolve (ResolveWorkspaceSymbol).
func (sp *ServerProvider) ProvideWorkspaceSymbols(ctx context.Context, query string) ([]WorkspaceSymbol, error) {
	c := sp.server()
	if c == nil || !offers(c, c.Capabilities().WorkspaceSymbolProvider.Enabled, lsp.MethodWorkspaceSymbol) {
		return nil, nil
	}
	var syms []lsp.WorkspaceSymbol
	if err := c.Call(ctx, lsp.MethodWorkspaceSymbol, lsp.WorkspaceSymbolParams{Query: query}, &syms); err != nil {
		return nil, err
	}
	return WorkspaceSymbolsFromLSP(ctx, c.Encoding(), sp.Texts, syms), nil
}

// CanResolveSymbol implements WorkspaceSymbolResolver: the server offers workspaceSymbol/resolve and sent the symbol.
func (sp *ServerProvider) CanResolveSymbol(sym WorkspaceSymbol) bool {
	c := sp.server()
	if c == nil || sym.server == nil {
		return false
	}
	return c.Capabilities().WorkspaceSymbolProvider.Bool("resolveProvider") || len(c.Registered(lsp.MethodWorkspaceSymbolResolve)) > 0
}

// ResolveWorkspaceSymbol implements WorkspaceSymbolResolver: the symbol goes back to the server, which fills in the range.
func (sp *ServerProvider) ResolveWorkspaceSymbol(ctx context.Context, sym WorkspaceSymbol) (WorkspaceSymbol, error) {
	c := sp.server()
	if c == nil || !sp.CanResolveSymbol(sym) {
		return sym, nil
	}
	var resolved lsp.WorkspaceSymbol
	if err := c.Call(ctx, lsp.MethodWorkspaceSymbolResolve, *sym.server, &resolved); err != nil {
		return sym, err
	}
	out := WorkspaceSymbolsFromLSP(ctx, c.Encoding(), sp.Texts, []lsp.WorkspaceSymbol{resolved})
	if len(out) == 0 {
		return sym, nil
	}
	got := out[0]
	got.Owner, got.source = sym.Owner, sym.source
	return got, nil
}
