package langfeatures

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// Semantic tokens (SPEC §9.12, "As built (step 12.9)"): the providers answer the tokens in the form of the protocol (five
// numbers per token, relative to the previous one, with columns in the position encoding of the server), the cache keeps the
// latest data of each document so that a server can answer the next request with edits, and the data is decoded into tokens
// with byte columns for the document's text.
//
// Ported from VS Code: src/vs/editor/common/services/semanticTokensProviderStyling.ts (toMultilineTokens2: the skipping of
// empty and overlapping tokens) and src/vs/editor/contrib/semanticTokens/common/getSemanticTokens.ts (the delta request and
// the applying of its edits)

// SemanticLegend names the token types and the token modifiers that the numbers of the tokens index: a token's type is an index
// of Types and its modifiers are the bits of a number, bit n for Modifiers[n].
type SemanticLegend struct {
	Types, Modifiers []string
}

// SemanticToken is a token with an absolute place: a run of Len bytes at the column Col of a line, of a type and modifiers
// named by the legend.
type SemanticToken struct {
	Line, Col, Len int
	// Type is the index of the type in the legend, Modifiers the set of modifier bits.
	Type      int
	Modifiers uint32
}

// SemanticEdit replaces DeleteCount numbers of the data of the previous result from Start with Data.
type SemanticEdit struct {
	Start, DeleteCount int
	Data               []uint32
}

// SemanticUpdate is the answer of a provider: the whole data (Delta false), or the edits that turn the data of the previous
// result into the new one (Delta true).
type SemanticUpdate struct {
	// ResultID is what the next request gives back as the previous result, empty when the provider does not do deltas.
	ResultID string
	Delta    bool
	Data     []uint32
	Edits    []SemanticEdit
	// Encoding is the position encoding of the columns in the data.
	Encoding lsp.Encoding
}

// SemanticTokensProvider answers the semantic tokens of a document.
type SemanticTokensProvider interface {
	// SemanticLegend is the legend of the tokens the provider answers.
	SemanticLegend() SemanticLegend
	// ProvideSemanticTokens answers all the tokens; with a previous result ID the provider may answer the edits since that
	// result. It answers nil when it has no tokens.
	ProvideSemanticTokens(ctx context.Context, doc Document, previousResultID string) (*SemanticUpdate, error)
}

// SemanticRangeProvider is a SemanticTokensProvider that also answers the tokens of a range of the document.
type SemanticRangeProvider interface {
	SemanticTokensProvider
	// ProvideSemanticTokensRange answers the tokens of a range, as data relative to the start of the document.
	ProvideSemanticTokensRange(ctx context.Context, doc Document, r Range) (*SemanticUpdate, error)
}

// RegisterSemanticTokens adds a semantic tokens provider.
func (r *Registry) RegisterSemanticTokens(owner string, sel Selector, p SemanticTokensProvider) (remove func()) {
	return r.semantic.add(owner, sel, p)
}

// SemanticTokensProviderFor is the provider of the semantic tokens of the document: the first, in the order of the providers,
// that offers them (VS Code asks the best provider only); nil when there is none.
func (r *Registry) SemanticTokensProviderFor(doc Document) SemanticTokensProvider {
	for _, reg := range r.semantic.ordered(doc) {
		if offered(reg.Provider, FeatureSemanticTokens) {
			return reg.Provider
		}
	}
	return nil
}

// SemanticTokens are the tokens of a document at a version, in the order of the text.
type SemanticTokens struct {
	// Version is the version of the document the tokens were made for.
	Version int
	Legend  SemanticLegend
	Tokens  []SemanticToken
	// Range is true for the tokens of a part of the document, asked while the whole was not known.
	Range bool
}

// OnLine gives the tokens of a line, which are sorted by column.
func (t *SemanticTokens) OnLine(line int) []SemanticToken {
	if t == nil || len(t.Tokens) == 0 {
		return nil
	}
	i := sort.Search(len(t.Tokens), func(i int) bool { return t.Tokens[i].Line >= line })
	j := i
	for j < len(t.Tokens) && t.Tokens[j].Line == line {
		j++
	}
	return t.Tokens[i:j]
}

// TypeName is the name of the type of a token in the legend, or "" when the legend does not have it.
func (t *SemanticTokens) TypeName(tok SemanticToken) string {
	if tok.Type < 0 || tok.Type >= len(t.Legend.Types) {
		return ""
	}
	return t.Legend.Types[tok.Type]
}

// ModifierNames are the names of the modifiers of a token that the legend has.
func (t *SemanticTokens) ModifierNames(tok SemanticToken) []string {
	var out []string
	for bit, set := 0, tok.Modifiers; set != 0 && bit < len(t.Legend.Modifiers); bit, set = bit+1, set>>1 {
		if set&1 != 0 {
			out = append(out, t.Legend.Modifiers[bit])
		}
	}
	return out
}

// ApplySemanticEdits makes the data a delta describes from the data of the previous result: the edits refer to the previous
// data, are applied in the order of their starts, and must not overlap or go beyond it. The previous data is not changed.
func ApplySemanticEdits(prev []uint32, edits []SemanticEdit) ([]uint32, error) {
	sorted := append([]SemanticEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	out := make([]uint32, 0, len(prev))
	at := 0
	for i, e := range sorted {
		if e.Start < at || e.Start > len(prev) || e.DeleteCount < 0 || e.Start+e.DeleteCount > len(prev) {
			return nil, errors.New("SemanticTokensEditInvalid", "an edit of the semantic tokens is outside the previous data or overlaps another",
				"edit", i, "start", e.Start, "deleteCount", e.DeleteCount, "previousLength", len(prev))
		}
		out = append(out, prev[at:e.Start]...)
		out = append(out, e.Data...)
		at = e.Start + e.DeleteCount
	}
	return append(out, prev[at:]...), nil
}

// DecodeSemanticTokens turns the data of the protocol into tokens with absolute lines and byte columns of the text. A token
// of no length, one that starts before the end of the one before it, and one on a line the text does not have are left out
// (VS Code warns about them and draws the rest). The columns of the data count in the encoding.
func DecodeSemanticTokens(data []uint32, enc lsp.Encoding, text lsp.Lines) []SemanticToken {
	out := make([]SemanticToken, 0, len(data)/5)
	line, startUnits := 0, 0
	prevLine, prevEnd := -1, 0
	// the text of the line of the tokens and whether its bytes are its characters
	curLine, content, ascii := -1, "", false
	lines := text.LineCount()
	for i := 0; i+4 < len(data); i += 5 {
		if data[i] > 0 {
			line += int(data[i])
			startUnits = int(data[i+1])
		} else {
			startUnits += int(data[i+1])
		}
		length := int(data[i+2])
		if length <= 0 || line >= lines {
			continue
		}
		if curLine != line {
			c, err := text.LineContent(line)
			if err != nil {
				continue
			}
			curLine, content = line, c
			ascii = isASCII(c)
		}
		start, end := startUnits, startUnits+length
		if !ascii {
			start, end = enc.ByteColumn(content, start), enc.ByteColumn(content, end)
		}
		start, end = min(start, len(content)), min(end, len(content))
		if end <= start || (prevLine == line && prevEnd > start) {
			continue
		}
		out = append(out, SemanticToken{Line: line, Col: start, Len: end - start, Type: int(data[i+3]), Modifiers: data[i+4]})
		prevLine, prevEnd = line, end
	}
	return out
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// SemanticTokensCache shares the semantic tokens of the documents of the window between the editors that show them. It keeps,
// for each document, the data and the result ID of the latest answer so that the next request is a delta request when the
// provider supports it, and the tokens decoded for the latest version asked for. It is safe for concurrent use.
type SemanticTokensCache struct {
	reg *Registry
	// epoch counts the refresh requests of the server: a result of an earlier epoch is stale.
	epoch atomic.Int64

	mu   sync.Mutex
	docs map[document.URI]*semanticDoc
}

// semanticDoc is the state of one document. The lock is a channel so that a request waits for the one before it and gives up
// when its context ends.
type semanticDoc struct {
	lock chan struct{}

	provider SemanticTokensProvider
	resultID string
	data     []uint32
	enc      lsp.Encoding
	result   *SemanticTokens
	// resultEpoch is the epoch of the cache when the result was made.
	resultEpoch int64
}

// NewSemanticTokensCache makes the cache of a registry.
func NewSemanticTokensCache(reg *Registry) *SemanticTokensCache {
	return &SemanticTokensCache{reg: reg, docs: map[document.URI]*semanticDoc{}}
}

func (c *SemanticTokensCache) doc(uri document.URI) *semanticDoc {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.docs[uri]
	if d == nil {
		d = &semanticDoc{lock: make(chan struct{}, 1)}
		c.docs[uri] = d
	}
	return d
}

// Forget drops what the cache knows of a document.
func (c *SemanticTokensCache) Forget(uri document.URI) {
	c.mu.Lock()
	delete(c.docs, uri)
	c.mu.Unlock()
}

// Refresh tells the cache that the server wants the tokens asked for again (workspace/semanticTokens/refresh): the tokens it
// holds are stale, but the data stays, to be the base of the next delta.
func (c *SemanticTokensCache) Refresh() { c.epoch.Add(1) }

// Get gives the tokens of the document at its version, asking the provider when the cache does not have them: with the
// previous result ID when it has one (the provider may answer with edits). It gives nil when no provider has semantic tokens
// for the document. A request waits for the one before it; ctx ends the wait and the request.
func (c *SemanticTokensCache) Get(ctx context.Context, doc Document) (*SemanticTokens, error) {
	p := c.reg.SemanticTokensProviderFor(doc)
	if p == nil {
		c.Forget(doc.URI)
		return nil, nil
	}
	d := c.doc(doc.URI)
	select {
	case d.lock <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-d.lock }()
	epoch := c.epoch.Load()
	if d.result != nil && d.result.Version == doc.Version && d.provider == p && d.resultEpoch == epoch {
		return d.result, nil
	}
	if d.provider != p {
		d.provider, d.resultID, d.data, d.result = p, "", nil, nil
	}
	prev := d.resultID
	update, err := p.ProvideSemanticTokens(ctx, doc, prev)
	if err != nil {
		return nil, err
	}
	if update == nil {
		d.resultID, d.data, d.result = "", nil, nil
		return nil, nil
	}
	data := update.Data
	if update.Delta {
		if prev == "" {
			return nil, errors.New("SemanticTokensDeltaUnexpected", "the provider answered edits to a result that was not asked for", "resultId", update.ResultID)
		}
		data, err = ApplySemanticEdits(d.data, update.Edits)
		if err != nil {
			// the data held is not the one the provider's edits are for: ask for all the tokens next time
			d.resultID, d.data, d.result = "", nil, nil
			return nil, err
		}
	}
	d.resultID, d.data, d.enc = update.ResultID, data, update.Encoding
	d.result = &SemanticTokens{Version: doc.Version, Legend: p.SemanticLegend(), Tokens: DecodeSemanticTokens(data, update.Encoding, doc.Text)}
	d.resultEpoch = epoch
	return d.result, nil
}

// GetRange gives the tokens of a range of the document, from a provider that answers ranges; nil when none does. They are
// not kept: a range is asked for while the tokens of the whole document are not known yet (VS Code does so for the first
// view of a document).
func (c *SemanticTokensCache) GetRange(ctx context.Context, doc Document, r Range) (*SemanticTokens, error) {
	p, ok := c.reg.SemanticTokensProviderFor(doc).(SemanticRangeProvider)
	if !ok {
		return nil, nil
	}
	update, err := p.ProvideSemanticTokensRange(ctx, doc, r)
	if err != nil || update == nil {
		return nil, err
	}
	return &SemanticTokens{Version: doc.Version, Legend: p.SemanticLegend(), Range: true,
		Tokens: DecodeSemanticTokens(update.Data, update.Encoding, doc.Text)}, nil
}

// semanticOptions are the options of the semantic tokens of the server: those it declared, else those of its first registration
// of the method; nil when it has none.
func semanticOptions(c Server) *lsp.SemanticTokensOptions {
	if o := c.Capabilities().SemanticTokensProvider; o != nil {
		return o
	}
	for _, reg := range c.Registered(lsp.MethodSemanticTokensFull) {
		var o lsp.SemanticTokensOptions
		if json.Unmarshal(reg.RegisterOptions, &o) == nil {
			return &o
		}
	}
	return nil
}

// SemanticLegend implements SemanticTokensProvider: the legend the server declared.
func (sp *ServerProvider) SemanticLegend() SemanticLegend {
	if c := sp.server(); c != nil {
		if o := semanticOptions(c); o != nil {
			return SemanticLegend{Types: o.Legend.TokenTypes, Modifiers: o.Legend.TokenModifiers}
		}
	}
	return SemanticLegend{}
}

// ProvideSemanticTokens implements SemanticTokensProvider: a delta request when the server does deltas and there is a previous
// result, else a full one.
func (sp *ServerProvider) ProvideSemanticTokens(ctx context.Context, doc Document, previousResultID string) (*SemanticUpdate, error) {
	c := sp.server()
	if c == nil || !sp.Offers(FeatureSemanticTokens) {
		return nil, nil
	}
	enc := c.Encoding()
	td := textDocument(doc)
	if o := semanticOptions(c); previousResultID != "" && o != nil && o.Full.Bool("delta") {
		var res lsp.SemanticTokensDeltaResult
		if err := c.Call(ctx, lsp.MethodSemanticTokensDelta, lsp.SemanticTokensDeltaParams{TextDocument: td, PreviousResultID: previousResultID}, &res); err != nil {
			return nil, err
		}
		switch {
		case res.Delta != nil:
			edits := make([]SemanticEdit, len(res.Delta.Edits))
			for i, e := range res.Delta.Edits {
				edits[i] = SemanticEdit{Start: e.Start, DeleteCount: e.DeleteCount, Data: e.Data}
			}
			return &SemanticUpdate{ResultID: res.Delta.ResultID, Delta: true, Edits: edits, Encoding: enc}, nil
		case res.Full != nil:
			return &SemanticUpdate{ResultID: res.Full.ResultID, Data: res.Full.Data, Encoding: enc}, nil
		}
		return nil, nil
	}
	var res *lsp.SemanticTokens
	if err := c.Call(ctx, lsp.MethodSemanticTokensFull, lsp.SemanticTokensParams{TextDocument: td}, &res); err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return &SemanticUpdate{ResultID: res.ResultID, Data: res.Data, Encoding: enc}, nil
}

// ProvideSemanticTokensRange implements SemanticRangeProvider; the server must offer range requests.
func (sp *ServerProvider) ProvideSemanticTokensRange(ctx context.Context, doc Document, r Range) (*SemanticUpdate, error) {
	c := sp.server()
	if c == nil || !sp.Offers(FeatureSemanticTokens) {
		return nil, nil
	}
	if o := semanticOptions(c); o == nil || !o.Range.Enabled {
		return nil, nil
	}
	enc := c.Encoding()
	var res *lsp.SemanticTokens
	params := lsp.SemanticTokensRangeParams{TextDocument: textDocument(doc), Range: enc.RangeToLSP(doc.Text, r)}
	if err := c.Call(ctx, lsp.MethodSemanticTokensRange, params, &res); err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return &SemanticUpdate{Data: res.Data, Encoding: enc}, nil
}
