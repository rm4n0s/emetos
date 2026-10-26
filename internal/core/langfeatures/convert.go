package langfeatures

import (
	"context"
	"encoding/json"
	"unicode/utf16"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// The conversion of what a language server answers (core/lsp's wire types) to the types of this package. The positions of
// the server count in the position encoding it negotiated; they are converted to byte columns with the text of the document
// they are in.

// TextSource gives the text of a document that is not the one a request is about, for the conversion of the positions of a
// location in it: the snapshot of an open document, or the file read from disk. A nil source, or an error, makes the
// conversion take the columns of that document for byte columns (right for ASCII and for servers that count bytes).
type TextSource func(ctx context.Context, uri document.URI) (lsp.Lines, error)

// converter converts the positions of one answer.
type converter struct {
	ctx   context.Context
	enc   lsp.Encoding
	doc   Document
	texts TextSource
	cache map[document.URI]lsp.Lines
}

func newConverter(ctx context.Context, enc lsp.Encoding, doc Document, texts TextSource) *converter {
	return &converter{ctx: ctx, enc: enc, doc: doc, texts: texts, cache: map[document.URI]lsp.Lines{}}
}

// lines is the text of the document with the URI, or nil when it cannot be had.
func (c *converter) lines(uri document.URI) lsp.Lines {
	if uri == c.doc.URI {
		return c.doc.Text
	}
	if l, ok := c.cache[uri]; ok {
		return l
	}
	var l lsp.Lines
	if c.texts != nil {
		if got, err := c.texts(c.ctx, uri); err == nil {
			l = got
		}
	}
	c.cache[uri] = l
	return l
}

// position converts a position of the server in the document with the URI.
func (c *converter) position(uri document.URI, p lsp.Position) Position {
	if l := c.lines(uri); l != nil {
		return c.enc.PositionFromLSP(l, p)
	}
	return Position{Line: p.Line, Col: p.Character}
}

// rng converts a range of the server in the document with the URI.
func (c *converter) rng(uri document.URI, r lsp.Range) Range {
	return buffer.NewRange(c.position(uri, r.Start), c.position(uri, r.End))
}

// own converts a range of the document the request is about.
func (c *converter) own(r lsp.Range) Range { return c.rng(c.doc.URI, r) }

// location converts a location; false for a URI the editor has no documents for.
func (c *converter) location(l lsp.Location) (Location, bool) {
	uri, err := lsp.ParseDocumentURI(l.URI)
	if err != nil {
		return Location{}, false
	}
	return Location{URI: uri, Range: c.rng(uri, l.Range)}, true
}

// markup converts the documentation of an item.
func markup(m *lsp.MarkupContent) *Markup {
	if m == nil || m.Value == "" {
		return nil
	}
	return &Markup{Value: m.Value, Markdown: m.Kind == lsp.MarkupMarkdown}
}

// HoverFromLSP converts the answer to a hover request about the document.
func HoverFromLSP(enc lsp.Encoding, doc Document, h *lsp.Hover) *Hover {
	if h == nil {
		return nil
	}
	c := newConverter(context.Background(), enc, doc, nil)
	out := &Hover{}
	for _, part := range h.Contents {
		if part.Value != "" {
			out.Contents = append(out.Contents, Markup{Value: part.Value, Markdown: part.Kind == lsp.MarkupMarkdown})
		}
	}
	if h.Range != nil {
		r := c.own(*h.Range)
		out.Range = &r
	}
	return out
}

// CompletionFromLSP converts the answer to a completion request about the document. The defaults of the list (LSP 3.17
// itemDefaults) are applied to the items.
func CompletionFromLSP(enc lsp.Encoding, doc Document, l lsp.CompletionList) CompletionList {
	c := newConverter(context.Background(), enc, doc, nil)
	out := CompletionList{Incomplete: l.IsIncomplete, Items: make([]CompletionItem, 0, len(l.Items))}
	var defRange, defInsert, defReplace *Range
	if d := l.ItemDefaults; d != nil && len(d.EditRange) > 0 {
		var single lsp.Range
		var pair struct {
			Insert  lsp.Range `json:"insert"`
			Replace lsp.Range `json:"replace"`
		}
		if json.Unmarshal(d.EditRange, &pair) == nil && (pair.Insert != lsp.Range{} || pair.Replace != lsp.Range{}) {
			i, r := c.own(pair.Insert), c.own(pair.Replace)
			defInsert, defReplace = &i, &r
		} else if json.Unmarshal(d.EditRange, &single) == nil {
			r := c.own(single)
			defRange = &r
		}
	}
	for i, it := range l.Items {
		item := CompletionItem{
			Label: it.Label, Detail: it.Detail, Documentation: markup(it.Documentation), Kind: CompletionKind(it.Kind),
			Deprecated: it.Deprecated, Preselect: it.Preselect, SortText: it.SortText, FilterText: it.FilterText,
			InsertText: it.InsertText, CommitCharacters: it.CommitCharacters,
		}
		for _, t := range it.Tags {
			item.Deprecated = item.Deprecated || t == lsp.CompletionItemTagDeprecated
		}
		if ld := it.LabelDetails; ld != nil {
			item.LabelDetail, item.LabelDescription = ld.Detail, ld.Description
		}
		format := it.InsertTextFormat
		if d := l.ItemDefaults; d != nil {
			if format == 0 {
				format = d.InsertTextFormat
			}
			if len(item.CommitCharacters) == 0 {
				item.CommitCharacters = d.CommitCharacters
			}
		}
		item.Snippet = format == lsp.InsertTextFormatSnippet
		mode := it.InsertTextMode
		if d := l.ItemDefaults; d != nil && mode == 0 {
			mode = d.InsertTextMode
		}
		item.KeepWhitespace = mode == lsp.InsertTextModeAsIs
		switch te := it.TextEdit; {
		case te != nil && te.Range != nil:
			r := c.own(*te.Range)
			item.Insert, item.InsertText = &r, te.NewText
		case te != nil && te.Insert != nil && te.Replace != nil:
			i, r := c.own(*te.Insert), c.own(*te.Replace)
			item.Insert, item.Replace, item.InsertText = &i, &r, te.NewText
		case defRange != nil:
			item.Insert = defRange
			if it.TextEditText != "" {
				item.InsertText = it.TextEditText
			}
		case defInsert != nil:
			item.Insert, item.Replace = defInsert, defReplace
			if it.TextEditText != "" {
				item.InsertText = it.TextEditText
			}
		}
		for _, e := range it.AdditionalTextEdits {
			item.AdditionalEdits = append(item.AdditionalEdits, TextEdit{Range: c.own(e.Range), Text: e.NewText})
		}
		if it.Command != nil {
			item.Command = commandFromLSP(it.Command, "")
		}
		// the item goes back to the server unchanged to resolve it, with the data its list gave it when it has none
		item.server = &l.Items[i]
		if d := l.ItemDefaults; d != nil && len(it.Data) == 0 && len(d.Data) > 0 {
			withData := it
			withData.Data = d.Data
			item.server = &withData
		}
		out.Items = append(out.Items, item)
	}
	return out
}

// commandFromLSP converts a command of a server; owner is the id of the server.
func commandFromLSP(c *lsp.Command, owner string) *Command {
	if c == nil {
		return nil
	}
	return &Command{Title: c.Title, ID: c.Command, Arguments: c.Arguments, Owner: owner}
}

// SignatureHelpFromLSP converts the answer to a signature help request.
func SignatureHelpFromLSP(h *lsp.SignatureHelp) *SignatureHelp {
	if h == nil || len(h.Signatures) == 0 {
		return nil
	}
	out := &SignatureHelp{server: h}
	if h.ActiveSignature != nil {
		out.ActiveSignature = *h.ActiveSignature
	}
	if h.ActiveParameter != nil {
		out.ActiveParameter = *h.ActiveParameter
	}
	for _, s := range h.Signatures {
		sig := Signature{Label: s.Label, Documentation: markup(s.Documentation), ActiveParameter: s.ActiveParameter}
		for _, p := range s.Parameters {
			param := Parameter{Label: p.Label.Text, Documentation: markup(p.Documentation)}
			if p.Label.Offsets != nil {
				from, to := utf16Bytes(s.Label, p.Label.Offsets[0], p.Label.Offsets[1])
				param.Label, param.Offsets = s.Label[from:to], &[2]int{from, to}
			}
			sig.Parameters = append(sig.Parameters, param)
		}
		out.Signatures = append(out.Signatures, sig)
	}
	return out
}

// utf16Bytes is the byte offsets in s of two offsets counted in UTF-16 code units (the offsets of a parameter label, which the
// protocol counts so whatever the position encoding is), moved into s; both are the end of s for offsets past it.
func utf16Bytes(s string, from, to int) (int, int) {
	units := 0
	start, end := -1, len(s)
	for i, r := range s {
		if start < 0 && units >= from {
			start = i
		}
		if units >= to {
			end = i
			break
		}
		units += max(utf16.RuneLen(r), 1)
	}
	if start < 0 {
		return len(s), len(s)
	}
	return start, max(end, start)
}

// LocationsFromLSP converts the links of a definition-like answer. Links the editor cannot open (another URI scheme) are
// left out.
func LocationsFromLSP(ctx context.Context, enc lsp.Encoding, doc Document, texts TextSource, links lsp.LocationLinks) []LocationLink {
	c := newConverter(ctx, enc, doc, texts)
	out := make([]LocationLink, 0, len(links))
	for _, l := range links {
		uri, err := lsp.ParseDocumentURI(l.TargetURI)
		if err != nil {
			continue
		}
		link := LocationLink{
			Target:          Location{URI: uri, Range: c.rng(uri, l.TargetRange)},
			TargetSelection: c.rng(uri, l.TargetSelectionRange),
		}
		if l.OriginSelectionRange != nil {
			r := c.own(*l.OriginSelectionRange)
			link.Origin = &r
		}
		out = append(out, link)
	}
	return out
}

// ReferencesFromLSP converts the locations of a references answer.
func ReferencesFromLSP(ctx context.Context, enc lsp.Encoding, doc Document, texts TextSource, locs []lsp.Location) []Location {
	c := newConverter(ctx, enc, doc, texts)
	out := make([]Location, 0, len(locs))
	for _, l := range locs {
		if loc, ok := c.location(l); ok {
			out = append(out, loc)
		}
	}
	return out
}

// SymbolsFromLSP converts the answer to a document symbol request.
func SymbolsFromLSP(enc lsp.Encoding, doc Document, symbols lsp.DocumentSymbols) []DocumentSymbol {
	c := newConverter(context.Background(), enc, doc, nil)
	return symbolsFrom(c, symbols)
}

func symbolsFrom(c *converter, symbols []lsp.DocumentSymbol) []DocumentSymbol {
	out := make([]DocumentSymbol, 0, len(symbols))
	for _, s := range symbols {
		sym := DocumentSymbol{
			Name: s.Name, Detail: s.Detail, Kind: s.Kind, Range: c.own(s.Range), SelectionRange: c.own(s.SelectionRange),
			Deprecated: s.Deprecated,
		}
		for _, t := range s.Tags {
			sym.Deprecated = sym.Deprecated || t == 1
		}
		if sym.Detail == "" {
			sym.Detail = s.ContainerName
		}
		sym.Children = symbolsFrom(c, s.Children)
		out = append(out, sym)
	}
	return out
}

// HighlightsFromLSP converts the answer to a document highlight request.
func HighlightsFromLSP(enc lsp.Encoding, doc Document, hs []lsp.DocumentHighlight) []DocumentHighlight {
	c := newConverter(context.Background(), enc, doc, nil)
	out := make([]DocumentHighlight, 0, len(hs))
	for _, h := range hs {
		kind := HighlightText
		switch h.Kind {
		case lsp.DocumentHighlightRead:
			kind = HighlightRead
		case lsp.DocumentHighlightWrite:
			kind = HighlightWrite
		}
		out = append(out, DocumentHighlight{Range: c.own(h.Range), Kind: kind})
	}
	return out
}

// DiagnosticsFromLSP converts the diagnostics a server published for the document (publishDiagnostics, or a pull report).
func DiagnosticsFromLSP(enc lsp.Encoding, doc Document, ds []lsp.Diagnostic) []Diagnostic {
	c := newConverter(context.Background(), enc, doc, nil)
	out := make([]Diagnostic, 0, len(ds))
	for _, d := range ds {
		diag := Diagnostic{Range: c.own(d.Range), Message: d.Message, Source: d.Source, Code: d.CodeText(), Severity: SeverityError}
		switch d.Severity {
		case lsp.SeverityWarning:
			diag.Severity = SeverityWarning
		case lsp.SeverityInformation:
			diag.Severity = SeverityInfo
		case lsp.SeverityHint:
			diag.Severity = SeverityHint
		}
		if d.CodeDescription != nil {
			diag.CodeHref = d.CodeDescription.Href
		}
		for _, t := range d.Tags {
			diag.Unnecessary = diag.Unnecessary || t == lsp.DiagnosticTagUnnecessary
			diag.Deprecated = diag.Deprecated || t == lsp.DiagnosticTagDeprecated
		}
		if raw, err := json.Marshal(d); err == nil {
			diag.Raw = raw
		}
		out = append(out, diag)
	}
	return out
}

// TextEditsFromLSP converts the edits a server answered with for the document.
func TextEditsFromLSP(enc lsp.Encoding, doc Document, edits []lsp.TextEdit) []TextEdit {
	c := newConverter(context.Background(), enc, doc, nil)
	out := make([]TextEdit, 0, len(edits))
	for _, e := range edits {
		out = append(out, TextEdit{Range: c.own(e.Range), Text: e.NewText})
	}
	return out
}

// CodeActionsFromLSP converts the answer to a code action request about the document; owner is the id of the server, which
// the commands of the actions are run through.
func CodeActionsFromLSP(enc lsp.Encoding, doc Document, owner string, actions lsp.CodeActions) []CodeAction {
	out := make([]CodeAction, 0, len(actions))
	for _, a := range actions {
		act := CodeAction{
			Title: a.Title, Kind: a.Kind, Preferred: a.IsPreferred, Diagnostics: DiagnosticsFromLSP(enc, doc, a.Diagnostics),
			Command: commandFromLSP(a.Command, owner),
		}
		if a.Disabled != nil {
			act.Disabled = a.Disabled.Reason
			if act.Disabled == "" {
				act.Disabled = "disabled"
			}
		}
		if a.Edit != nil {
			act.Edit = FromLSPEdit(enc, *a.Edit)
		}
		out = append(out, act)
	}
	return out
}

// WorkspaceSymbolsFromLSP converts the answer to a workspace/symbol request. The positions are converted with the text of the
// file the symbol is in (texts). A symbol of a URI the editor has no documents for is left out; one whose location has no range
// is Unresolved, at the start of its file.
func WorkspaceSymbolsFromLSP(ctx context.Context, enc lsp.Encoding, texts TextSource, syms []lsp.WorkspaceSymbol) []WorkspaceSymbol {
	c := newConverter(ctx, enc, Document{}, texts)
	out := make([]WorkspaceSymbol, 0, len(syms))
	for i := range syms {
		s := &syms[i]
		uri, err := lsp.ParseDocumentURI(s.Location.URI)
		if err != nil {
			continue
		}
		sym := WorkspaceSymbol{Name: s.Name, ContainerName: s.ContainerName, Kind: s.Kind, Location: Location{URI: uri}, server: s}
		for _, t := range s.Tags {
			sym.Deprecated = sym.Deprecated || t == 1
		}
		if s.Location.Range == nil {
			sym.Unresolved = true
		} else {
			sym.Location.Range = c.rng(uri, *s.Location.Range)
		}
		out = append(out, sym)
	}
	return out
}
