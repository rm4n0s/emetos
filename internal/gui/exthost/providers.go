package exthost

import (
	"context"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// The providers an extension writes in Go (SPEC §9.12 "Providers in Go", "As built (step 12.3)"): the extension gives a
// provider of the types of package extension; the host registers an adapter in the registry of the scope, which converts
// between the types of the two packages and guards every call into the provider like every other call into an extension.

// guardProvider calls fn, a call into a provider of the extension, off the UI thread. A panic fails the extension in this
// scope for the rest of the session (the failure is made on the UI thread's next turn) and gives the error
// ExtensionProviderPanicked, so the answer of the provider is dropped; an error of the provider becomes
// ExtensionProviderFailed, which the registry reports with the others and which does not fail the extension.
func (s *Scope) guardProvider(inst *instance, feature string, fn func() error) (err error) {
	if inst.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", inst.id)
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionProviderPanicked", "it panicked in a %s provider: %v", feature, r).SetMetadata("extension", inst.id, "feature", feature, "scope", s.opts.Info.Name)
			s.post(func() { s.fail(inst.slot, failure, feature+" provider") })
			err = failure
		}
	}()
	if e := fn(); e != nil {
		return errors.NewErr("ExtensionProviderFailed", e).SetMetadata("extension", inst.id, "feature", feature, "scope", s.opts.Info.Name)
	}
	return nil
}

// register is what every Register...Provider method does around its adapter: the checks, then the registration in the
// registry (add), which the activation takes away.
func (l languagesAPI) register(feature string, sel extension.DocumentSelector, isNil bool, add func(owner string, sel langfeatures.Selector) func()) (func(), error) {
	in := l.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if isNil {
		return nil, errors.New("ExtensionProviderInvalid", "a provider is nil", "extension", in.id, "feature", feature)
	}
	remove := add(in.id, langfeatures.Selector{Languages: sel.Languages, Pattern: sel.Pattern, Folder: in.scope.opts.Info.Folder})
	in.track(remove)
	return remove, nil
}

// adapter is what the adapters of the providers of one activation share.
type adapter struct{ inst *instance }

func (a adapter) call(feature string, fn func() error) error {
	return a.inst.scope.guardProvider(a.inst, feature, fn)
}

// RegisterCompletionProvider implements extension.Languages.
func (l languagesAPI) RegisterCompletionProvider(sel extension.DocumentSelector, p extension.CompletionProvider, triggers ...string) (func(), error) {
	return l.register("completion", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterCompletion(owner, s, completionAdapter{adapter(l), p}, triggers...)
	})
}

// RegisterHoverProvider implements extension.Languages.
func (l languagesAPI) RegisterHoverProvider(sel extension.DocumentSelector, p extension.HoverProvider) (func(), error) {
	return l.register("hover", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterHover(owner, s, hoverAdapter{adapter(l), p})
	})
}

// RegisterSignatureHelpProvider implements extension.Languages.
func (l languagesAPI) RegisterSignatureHelpProvider(sel extension.DocumentSelector, p extension.SignatureHelpProvider, triggers ...string) (func(), error) {
	return l.register("signatureHelp", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterSignatureHelp(owner, s, signatureAdapter{adapter(l), p}, triggers...)
	})
}

// RegisterDefinitionProvider implements extension.Languages.
func (l languagesAPI) RegisterDefinitionProvider(sel extension.DocumentSelector, p extension.DefinitionProvider) (func(), error) {
	return l.register("definition", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterNavigation(owner, s, definitionAdapter{adapter(l), p})
	})
}

// RegisterReferenceProvider implements extension.Languages.
func (l languagesAPI) RegisterReferenceProvider(sel extension.DocumentSelector, p extension.ReferenceProvider) (func(), error) {
	return l.register("references", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterReferences(owner, s, referenceAdapter{adapter(l), p})
	})
}

// RegisterDocumentSymbolProvider implements extension.Languages.
func (l languagesAPI) RegisterDocumentSymbolProvider(sel extension.DocumentSelector, p extension.DocumentSymbolProvider) (func(), error) {
	return l.register("documentSymbols", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterSymbols(owner, s, symbolAdapter{adapter(l), p})
	})
}

// RegisterDocumentHighlightProvider implements extension.Languages.
func (l languagesAPI) RegisterDocumentHighlightProvider(sel extension.DocumentSelector, p extension.DocumentHighlightProvider) (func(), error) {
	return l.register("documentHighlights", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterHighlights(owner, s, highlightAdapter{adapter(l), p})
	})
}

// RegisterCodeActionProvider implements extension.Languages.
func (l languagesAPI) RegisterCodeActionProvider(sel extension.DocumentSelector, p extension.CodeActionProvider) (func(), error) {
	return l.register("codeActions", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterCodeActions(owner, s, codeActionAdapter{adapter(l), p})
	})
}

// RegisterFormattingProvider implements extension.Languages. A provider that formats ranges too is registered so that the
// registry sees it does.
func (l languagesAPI) RegisterFormattingProvider(sel extension.DocumentSelector, p extension.FormattingProvider) (func(), error) {
	return l.register("formatting", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		fa := formattingAdapter{adapter(l), p}
		if rp, ok := p.(extension.RangeFormattingProvider); ok {
			return l.inst.scope.features.RegisterFormatting(owner, s, rangeFormattingAdapter{fa, rp})
		}
		return l.inst.scope.features.RegisterFormatting(owner, s, fa)
	})
}

// RegisterRenameProvider implements extension.Languages.
func (l languagesAPI) RegisterRenameProvider(sel extension.DocumentSelector, p extension.RenameProvider) (func(), error) {
	return l.register("rename", sel, p == nil, func(owner string, s langfeatures.Selector) func() {
		return l.inst.scope.features.RegisterRename(owner, s, renameAdapter{adapter(l), p})
	})
}

// ---- conversions ----

func toTextDocument(d langfeatures.Document) extension.TextDocument {
	return extension.TextDocument{URI: d.URI.String(), LanguageID: d.LanguageID, Version: d.Version, Text: d.Text.String()}
}

func toPos(p langfeatures.Position) extension.Position {
	return extension.Position{Line: p.Line, Col: p.Col}
}

func fromPos(p extension.Position) langfeatures.Position {
	return langfeatures.Position{Line: p.Line, Col: p.Col}
}

func toRange(r langfeatures.Range) extension.Range {
	return extension.Range{Start: toPos(r.Start), End: toPos(r.End)}
}

func fromRange(r extension.Range) langfeatures.Range {
	return buffer.NewRange(fromPos(r.Start), fromPos(r.End))
}

func fromRangePtr(r *extension.Range) *langfeatures.Range {
	if r == nil {
		return nil
	}
	out := fromRange(*r)
	return &out
}

func fromMarkup(m *extension.Markup) *langfeatures.Markup {
	if m == nil {
		return nil
	}
	return &langfeatures.Markup{Value: m.Value, Markdown: m.Markdown}
}

func fromMarkups(ms []extension.Markup) []langfeatures.Markup {
	out := make([]langfeatures.Markup, len(ms))
	for i, m := range ms {
		out[i] = langfeatures.Markup{Value: m.Value, Markdown: m.Markdown}
	}
	return out
}

func fromEdits(es []extension.RangeEdit) []langfeatures.TextEdit {
	out := make([]langfeatures.TextEdit, len(es))
	for i, e := range es {
		out[i] = langfeatures.TextEdit{Range: fromRange(e.Range), Text: e.Text}
	}
	return out
}

func fromCommand(c *extension.Command, owner string) *langfeatures.Command {
	if c == nil {
		return nil
	}
	return &langfeatures.Command{Title: c.Title, ID: c.ID, Arguments: c.Arguments, Owner: owner}
}

// fromLocations converts locations; a location whose URI is not one the editor knows fails with the error of
// document.ParseURI.
func fromLocations(ls []extension.Location) ([]langfeatures.Location, error) {
	out := make([]langfeatures.Location, 0, len(ls))
	for _, l := range ls {
		u, err := document.ParseURI(l.URI)
		if err != nil {
			return nil, err
		}
		out = append(out, langfeatures.Location{URI: u, Range: fromRange(l.Range)})
	}
	return out, nil
}

func fromSymbols(ss []extension.DocumentSymbol) []langfeatures.DocumentSymbol {
	out := make([]langfeatures.DocumentSymbol, len(ss))
	for i, s := range ss {
		out[i] = langfeatures.DocumentSymbol{
			Name: s.Name, Detail: s.Detail, Kind: s.Kind, Range: fromRange(s.Range), SelectionRange: fromRange(s.SelectionRange),
			Children: fromSymbols(s.Children),
		}
	}
	return out
}

func toDiagnostics(ds []langfeatures.Diagnostic) []extension.Diagnostic {
	out := make([]extension.Diagnostic, len(ds))
	for i, d := range ds {
		out[i] = extension.Diagnostic{Range: toRange(d.Range), Severity: extension.Severity(d.Severity), Message: d.Message, Source: d.Source, Code: d.Code}
	}
	return out
}

func fromDiagnostics(ds []extension.Diagnostic) []langfeatures.Diagnostic {
	out := make([]langfeatures.Diagnostic, len(ds))
	for i, d := range ds {
		out[i] = langfeatures.Diagnostic{Range: fromRange(d.Range), Severity: langfeatures.Severity(d.Severity), Message: d.Message, Source: d.Source, Code: d.Code}
	}
	return out
}

// fromWorkspaceEdit converts an edit of an extension; a URI the editor does not know fails with the error of
// document.ParseURI, and a kind that is not one of edit, create, rename and delete with ExtensionEditInvalid.
func fromWorkspaceEdit(we *extension.WorkspaceEdit) (*langfeatures.WorkspaceEdit, error) {
	if we == nil {
		return nil, nil
	}
	out := &langfeatures.WorkspaceEdit{}
	for _, c := range we.Changes {
		ch := langfeatures.Change{
			Edits: fromEdits(c.Edits), Overwrite: c.Overwrite, IgnoreIfExists: c.IgnoreIfExists, Recursive: c.Recursive, IgnoreIfNotExists: c.IgnoreIfNotExists,
		}
		switch c.Kind {
		case "edit", "":
			ch.Kind = langfeatures.ChangeText
		case "create":
			ch.Kind = langfeatures.ChangeCreate
		case "rename":
			ch.Kind = langfeatures.ChangeRename
		case "delete":
			ch.Kind = langfeatures.ChangeDelete
		default:
			return nil, errors.New("ExtensionEditInvalid", "the kind of a change is edit, create, rename or delete", "kind", c.Kind)
		}
		u, err := document.ParseURI(c.URI)
		if err != nil {
			return nil, err
		}
		ch.URI = u
		if ch.Kind == langfeatures.ChangeRename {
			if ch.NewURI, err = document.ParseURI(c.NewURI); err != nil {
				return nil, err
			}
		}
		out.Changes = append(out.Changes, ch)
	}
	return out, nil
}

// ---- adapters ----

type completionAdapter struct {
	adapter
	p extension.CompletionProvider
}

func (a completionAdapter) ProvideCompletion(ctx context.Context, doc langfeatures.Document, pos langfeatures.Position, trig langfeatures.CompletionTrigger) (res langfeatures.CompletionList, err error) {
	err = a.call("completion", func() error {
		kind := "invoked"
		switch trig.Kind {
		case langfeatures.TriggerCharacter:
			kind = "character"
		case langfeatures.TriggerIncomplete:
			kind = "incomplete"
		}
		list, e := a.p.ProvideCompletion(ctx, toTextDocument(doc), toPos(pos), extension.CompletionTrigger{Kind: kind, Character: trig.Character})
		if e != nil {
			return e
		}
		res.Incomplete = list.Incomplete
		for _, it := range list.Items {
			res.Items = append(res.Items, langfeatures.CompletionItem{
				Label: it.Label, Detail: it.Detail, Documentation: fromMarkup(it.Documentation), Kind: langfeatures.CompletionKind(it.Kind),
				Deprecated: it.Deprecated, Preselect: it.Preselect, SortText: it.SortText, FilterText: it.FilterText,
				InsertText: it.InsertText, Snippet: it.Snippet, Insert: fromRangePtr(it.Range), AdditionalEdits: fromEdits(it.AdditionalEdits),
				CommitCharacters: it.CommitCharacters, Command: fromCommand(it.Command, a.inst.id),
			})
		}
		return nil
	})
	return res, err
}

type hoverAdapter struct {
	adapter
	p extension.HoverProvider
}

func (a hoverAdapter) ProvideHover(ctx context.Context, doc langfeatures.Document, pos langfeatures.Position) (res *langfeatures.Hover, err error) {
	err = a.call("hover", func() error {
		h, e := a.p.ProvideHover(ctx, toTextDocument(doc), toPos(pos))
		if e != nil || h == nil {
			return e
		}
		res = &langfeatures.Hover{Contents: fromMarkups(h.Contents), Range: fromRangePtr(h.Range)}
		return nil
	})
	return res, err
}

type signatureAdapter struct {
	adapter
	p extension.SignatureHelpProvider
}

func (a signatureAdapter) ProvideSignatureHelp(ctx context.Context, doc langfeatures.Document, pos langfeatures.Position, _ langfeatures.SignatureContext) (res *langfeatures.SignatureHelp, err error) {
	err = a.call("signatureHelp", func() error {
		h, e := a.p.ProvideSignatureHelp(ctx, toTextDocument(doc), toPos(pos))
		if e != nil || h == nil {
			return e
		}
		res = &langfeatures.SignatureHelp{ActiveSignature: h.ActiveSignature, ActiveParameter: h.ActiveParameter}
		for _, s := range h.Signatures {
			sig := langfeatures.Signature{Label: s.Label, Documentation: fromMarkup(s.Documentation)}
			for _, p := range s.Parameters {
				sig.Parameters = append(sig.Parameters, langfeatures.Parameter{Label: p.Label, Documentation: fromMarkup(p.Documentation)})
			}
			res.Signatures = append(res.Signatures, sig)
		}
		return nil
	})
	return res, err
}

type definitionAdapter struct {
	adapter
	p extension.DefinitionProvider
}

// Offers implements langfeatures' featureOffers: a definition provider of an extension offers Go to Definition only.
func (a definitionAdapter) Offers(f langfeatures.Feature) bool {
	return f == langfeatures.FeatureDefinition
}

func (a definitionAdapter) ProvideLocations(ctx context.Context, doc langfeatures.Document, pos langfeatures.Position, kind langfeatures.NavKind) (res []langfeatures.LocationLink, err error) {
	if kind != langfeatures.NavDefinition {
		return nil, nil
	}
	err = a.call("definition", func() error {
		ls, e := a.p.ProvideDefinition(ctx, toTextDocument(doc), toPos(pos))
		if e != nil {
			return e
		}
		locs, e := fromLocations(ls)
		if e != nil {
			return e
		}
		for _, l := range locs {
			res = append(res, langfeatures.LocationLink{Target: l, TargetSelection: l.Range})
		}
		return nil
	})
	return res, err
}

type referenceAdapter struct {
	adapter
	p extension.ReferenceProvider
}

func (a referenceAdapter) ProvideReferences(ctx context.Context, doc langfeatures.Document, pos langfeatures.Position, includeDecl bool) (res []langfeatures.Location, err error) {
	err = a.call("references", func() error {
		ls, e := a.p.ProvideReferences(ctx, toTextDocument(doc), toPos(pos), includeDecl)
		if e != nil {
			return e
		}
		res, e = fromLocations(ls)
		return e
	})
	return res, err
}

type symbolAdapter struct {
	adapter
	p extension.DocumentSymbolProvider
}

func (a symbolAdapter) ProvideDocumentSymbols(ctx context.Context, doc langfeatures.Document) (res []langfeatures.DocumentSymbol, err error) {
	err = a.call("documentSymbols", func() error {
		ss, e := a.p.ProvideDocumentSymbols(ctx, toTextDocument(doc))
		if e != nil {
			return e
		}
		res = fromSymbols(ss)
		return nil
	})
	return res, err
}

type highlightAdapter struct {
	adapter
	p extension.DocumentHighlightProvider
}

func (a highlightAdapter) ProvideDocumentHighlights(ctx context.Context, doc langfeatures.Document, pos langfeatures.Position) (res []langfeatures.DocumentHighlight, err error) {
	err = a.call("documentHighlights", func() error {
		hs, e := a.p.ProvideDocumentHighlights(ctx, toTextDocument(doc), toPos(pos))
		if e != nil {
			return e
		}
		for _, h := range hs {
			res = append(res, langfeatures.DocumentHighlight{Range: fromRange(h.Range), Kind: langfeatures.HighlightKind(max(h.Kind-1, 0))})
		}
		return nil
	})
	return res, err
}

type codeActionAdapter struct {
	adapter
	p extension.CodeActionProvider
}

func (a codeActionAdapter) ProvideCodeActions(ctx context.Context, doc langfeatures.Document, req langfeatures.CodeActionRequest) (res []langfeatures.CodeAction, err error) {
	err = a.call("codeActions", func() error {
		acts, e := a.p.ProvideCodeActions(ctx, toTextDocument(doc), extension.CodeActionRequest{
			Range: toRange(req.Range), Diagnostics: toDiagnostics(req.Diagnostics), Only: req.Only,
		})
		if e != nil {
			return e
		}
		for _, act := range acts {
			edit, e := fromWorkspaceEdit(act.Edit)
			if e != nil {
				return e
			}
			res = append(res, langfeatures.CodeAction{
				Title: act.Title, Kind: act.Kind, Diagnostics: fromDiagnostics(act.Diagnostics), Preferred: act.Preferred,
				Disabled: act.Disabled, Edit: edit, Command: fromCommand(act.Command, a.inst.id),
			})
		}
		return nil
	})
	return res, err
}

type formattingAdapter struct {
	adapter
	p extension.FormattingProvider
}

func (a formattingAdapter) ProvideFormatting(ctx context.Context, doc langfeatures.Document, opts langfeatures.FormattingOptions) (res []langfeatures.TextEdit, err error) {
	err = a.call("formatting", func() error {
		es, e := a.p.ProvideFormatting(ctx, toTextDocument(doc), extension.FormattingOptions{TabSize: opts.TabSize, InsertSpaces: opts.InsertSpaces})
		if e != nil {
			return e
		}
		res = fromEdits(es)
		return nil
	})
	return res, err
}

// rangeFormattingAdapter is a formattingAdapter whose provider formats ranges too.
type rangeFormattingAdapter struct {
	formattingAdapter
	rp extension.RangeFormattingProvider
}

func (a rangeFormattingAdapter) ProvideRangeFormatting(ctx context.Context, doc langfeatures.Document, r langfeatures.Range, opts langfeatures.FormattingOptions) (res []langfeatures.TextEdit, err error) {
	err = a.call("rangeFormatting", func() error {
		es, e := a.rp.ProvideRangeFormatting(ctx, toTextDocument(doc), toRange(r), extension.FormattingOptions{TabSize: opts.TabSize, InsertSpaces: opts.InsertSpaces})
		if e != nil {
			return e
		}
		res = fromEdits(es)
		return nil
	})
	return res, err
}

type renameAdapter struct {
	adapter
	p extension.RenameProvider
}

func (a renameAdapter) PrepareRename(ctx context.Context, doc langfeatures.Document, pos langfeatures.Position) (res *langfeatures.PrepareRename, err error) {
	err = a.call("rename", func() error {
		r, e := a.p.PrepareRename(ctx, toTextDocument(doc), toPos(pos))
		if e != nil {
			return e
		}
		if r == nil {
			res = &langfeatures.PrepareRename{DefaultBehavior: true}
			return nil
		}
		res = &langfeatures.PrepareRename{Range: fromRange(r.Range), Placeholder: r.Placeholder}
		return nil
	})
	return res, err
}

func (a renameAdapter) ProvideRename(ctx context.Context, doc langfeatures.Document, pos langfeatures.Position, newName string) (res *langfeatures.WorkspaceEdit, err error) {
	err = a.call("rename", func() error {
		we, e := a.p.ProvideRename(ctx, toTextDocument(doc), toPos(pos), newName)
		if e != nil {
			return e
		}
		res, e = fromWorkspaceEdit(we)
		return e
	})
	return res, err
}
