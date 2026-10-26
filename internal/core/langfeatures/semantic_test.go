package langfeatures_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

func tok(line, col, n, typ int, mods uint32) SemanticToken {
	return SemanticToken{Line: line, Col: col, Len: n, Type: typ, Modifiers: mods}
}

func TestDecodeSemanticTokens(t *testing.T) {
	// "x😀y = 1" has y at byte 5 and at UTF-16 offset 3 (and at UTF-8 offset 5)
	text := buffer.New("package main\nx😀y = 1\n\tfoo()\nlast").Snapshot()
	tests := []struct {
		name string
		enc  lsp.Encoding
		data []uint32
		want []SemanticToken
	}{
		{"relative lines and columns", lsp.UTF16,
			[]uint32{0, 0, 7, 1, 0, 0, 8, 4, 2, 5, 1, 0, 1, 3, 0, 1, 1, 3, 12, 1<<1 | 1},
			[]SemanticToken{tok(0, 0, 7, 1, 0), tok(0, 8, 4, 2, 5), tok(1, 0, 1, 3, 0), tok(2, 1, 3, 12, 3)}},
		{"a column after an emoji, counted in UTF-16 units", lsp.UTF16,
			[]uint32{1, 3, 1, 5, 0}, []SemanticToken{tok(1, 5, 1, 5, 0)}},
		{"the same column in UTF-8 units is the byte column", lsp.UTF8,
			[]uint32{1, 5, 1, 5, 0}, []SemanticToken{tok(1, 5, 1, 5, 0)}},
		{"a token of no length and one on a line the text lacks are left out", lsp.UTF16,
			[]uint32{0, 0, 0, 1, 0, 0, 1, 2, 1, 0, 9, 0, 3, 1, 0}, []SemanticToken{tok(0, 1, 2, 1, 0)}},
		{"a token that starts inside the one before it is left out", lsp.UTF16,
			[]uint32{0, 0, 5, 1, 0, 0, 3, 2, 2, 0, 0, 2, 2, 3, 0}, []SemanticToken{tok(0, 0, 5, 1, 0), tok(0, 5, 2, 3, 0)}},
		{"a token past the end of its line is cut", lsp.UTF16,
			[]uint32{3, 2, 40, 7, 0}, []SemanticToken{tok(3, 2, 2, 7, 0)}},
		{"a partial last token is ignored", lsp.UTF16, []uint32{0, 0, 2, 1}, []SemanticToken{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeSemanticTokens(tt.data, tt.enc, text)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tokens = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestApplySemanticEdits(t *testing.T) {
	prev := []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	tests := []struct {
		name  string
		edits []SemanticEdit
		want  []uint32
	}{
		{"no edits", nil, prev},
		{"a replacement", []SemanticEdit{{Start: 2, DeleteCount: 3, Data: []uint32{20, 30}}}, []uint32{1, 2, 20, 30, 6, 7, 8, 9, 10}},
		{"an insertion", []SemanticEdit{{Start: 5, Data: []uint32{0, 0}}}, []uint32{1, 2, 3, 4, 5, 0, 0, 6, 7, 8, 9, 10}},
		{"a deletion at the end", []SemanticEdit{{Start: 8, DeleteCount: 2}}, []uint32{1, 2, 3, 4, 5, 6, 7, 8}},
		{"edits in the order of the previous data, whatever the order they come in",
			[]SemanticEdit{{Start: 7, DeleteCount: 1, Data: []uint32{70}}, {Start: 0, DeleteCount: 1}}, []uint32{2, 3, 4, 5, 6, 7, 70, 9, 10}},
		{"an insertion at the very end", []SemanticEdit{{Start: 10, Data: []uint32{11}}}, []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplySemanticEdits(prev, tt.edits)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("data = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
	if !reflect.DeepEqual(prev, []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
		t.Errorf("the previous data was changed: %v", prev)
	}
	bad := map[string][]SemanticEdit{
		"a start beyond the data":    {{Start: 11}},
		"a deletion beyond the data": {{Start: 8, DeleteCount: 3}},
		"edits that overlap":         {{Start: 0, DeleteCount: 4}, {Start: 2, DeleteCount: 1}},
		"a negative count":           {{Start: 1, DeleteCount: -1}},
	}
	for name, edits := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := ApplySemanticEdits(prev, edits)
			routeOf(t, err, "ApplySemanticEdits.SemanticTokensEditInvalid")
		})
	}
}

func TestSemanticTokensLookups(t *testing.T) {
	tk := &SemanticTokens{
		Legend: SemanticLegend{Types: []string{"namespace", "type"}, Modifiers: []string{"declaration", "readonly", "static"}},
		Tokens: []SemanticToken{tok(0, 0, 1, 0, 0), tok(2, 0, 1, 1, 0b101), tok(2, 4, 1, 9, 0), tok(5, 0, 1, 1, 0)},
	}
	if got := tk.OnLine(2); len(got) != 2 || got[0].Col != 0 || got[1].Col != 4 {
		t.Errorf("line 2 = %+v", got)
	}
	if got := tk.OnLine(3); len(got) != 0 {
		t.Errorf("line 3 = %+v", got)
	}
	var nilTokens *SemanticTokens
	if nilTokens.OnLine(0) != nil {
		t.Error("nil tokens have no lines")
	}
	if tk.TypeName(tk.Tokens[1]) != "type" || tk.TypeName(tk.Tokens[2]) != "" {
		t.Error("the type name comes from the legend")
	}
	if got := tk.ModifierNames(tk.Tokens[1]); !reflect.DeepEqual(got, []string{"declaration", "static"}) {
		t.Errorf("modifiers = %v", got)
	}
}

// fakeSemantic is a provider of semantic tokens the tests drive: it answers deltas when asked for them.
type fakeSemantic struct {
	mu      sync.Mutex
	data    []uint32
	id      int
	delta   bool
	asked   []string
	fail    error
	rangeOK bool
}

func (f *fakeSemantic) SemanticLegend() SemanticLegend {
	return SemanticLegend{Types: []string{"namespace", "function"}, Modifiers: []string{"declaration"}}
}

func (f *fakeSemantic) ProvideSemanticTokens(_ context.Context, _ Document, prev string) (*SemanticUpdate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, prev)
	if f.fail != nil {
		return nil, f.fail
	}
	f.id++
	id := string(rune('a' + f.id))
	if prev != "" && f.delta {
		// the first number of the data changed
		return &SemanticUpdate{ResultID: id, Delta: true, Edits: []SemanticEdit{{Start: 2, DeleteCount: 1, Data: []uint32{f.data[2]}}}, Encoding: lsp.UTF16}, nil
	}
	return &SemanticUpdate{ResultID: id, Data: append([]uint32(nil), f.data...), Encoding: lsp.UTF16}, nil
}

func (f *fakeSemantic) ProvideSemanticTokensRange(context.Context, Document, Range) (*SemanticUpdate, error) {
	if !f.rangeOK {
		return nil, nil
	}
	return &SemanticUpdate{Data: []uint32{0, 0, 7, 1, 0}, Encoding: lsp.UTF16}, nil
}

func TestSemanticTokensCache(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	p := &fakeSemantic{data: []uint32{0, 0, 7, 1, 0, 0, 8, 4, 0, 1}, delta: true, rangeOK: true}
	reg.RegisterSemanticTokens("test", Selector{Languages: []string{"go"}}, p)
	cache := NewSemanticTokensCache(reg)
	doc := docOf("go", "/work/a.go", "package main\n")

	got, err := cache.Get(ctx, doc)
	if err != nil || got == nil || len(got.Tokens) != 2 || got.Version != 1 {
		t.Fatalf("first answer = %+v, %v", got, err)
	}
	again, err := cache.Get(ctx, doc)
	if err != nil || again != got || len(p.asked) != 1 {
		t.Errorf("the same version is not asked twice: %d requests, same answer %v, %v", len(p.asked), again == got, err)
	}

	// a newer version: the provider is given the result ID of the last answer and answers edits
	p.data = []uint32{0, 0, 6, 1, 0, 0, 8, 4, 0, 1}
	doc.Version = 2
	got, err = cache.Get(ctx, doc)
	if err != nil || got == nil || got.Tokens[0].Len != 6 || len(got.Tokens) != 2 {
		t.Fatalf("delta answer = %+v, %v", got, err)
	}
	if want := []string{"", "b"}; !reflect.DeepEqual(p.asked, want) {
		t.Errorf("previous result IDs asked = %q, want %q", p.asked, want)
	}

	// the server asks for a refresh: the same version is asked again, as a delta
	cache.Refresh()
	if _, err := cache.Get(ctx, doc); err != nil || len(p.asked) != 3 || p.asked[2] != "c" {
		t.Errorf("after a refresh: %q, %v", p.asked, err)
	}

	// a range, for the first view
	r, err := cache.GetRange(ctx, doc, rng(0, 0, 1, 0))
	if err != nil || r == nil || !r.Range || len(r.Tokens) != 1 {
		t.Errorf("range = %+v, %v", r, err)
	}

	// a document with no provider has no tokens
	other := docOf("rust", "/work/a.rs", "fn main() {}\n")
	if got, err := cache.Get(ctx, other); got != nil || err != nil {
		t.Errorf("rust = %+v, %v", got, err)
	}
	if got, err := cache.GetRange(ctx, other, rng(0, 0, 1, 0)); got != nil || err != nil {
		t.Errorf("rust range = %+v, %v", got, err)
	}
}

func TestSemanticTokensCacheFailures(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	p := &fakeSemantic{data: []uint32{0, 0, 7, 1, 0, 0, 8, 4, 0, 1}}
	reg.RegisterSemanticTokens("test", Selector{Languages: []string{"go"}}, p)
	cache := NewSemanticTokensCache(reg)
	doc := docOf("go", "/work/a.go", "package main\n")
	if _, err := cache.Get(ctx, doc); err != nil {
		t.Fatal(err)
	}

	// edits for data the cache does not hold: it asks for everything the next time
	bad := &badDelta{}
	reg2 := NewRegistry()
	reg2.RegisterSemanticTokens("test", Selector{}, bad)
	c2 := NewSemanticTokensCache(reg2)
	if _, err := c2.Get(ctx, doc); err != nil {
		t.Fatal(err)
	}
	doc.Version = 2
	_, err := c2.Get(ctx, doc)
	routeOf(t, err, "SemanticTokensCache.Get->ApplySemanticEdits.SemanticTokensEditInvalid")
	doc.Version = 3
	bad.mode = 2
	if _, err := c2.Get(ctx, doc); err != nil || bad.lastPrev != "" {
		t.Errorf("after a bad delta everything is asked for: previous %q, %v", bad.lastPrev, err)
	}

	// edits where none were asked for
	c3 := NewSemanticTokensCache(reg2)
	_, err = c3.Get(ctx, docOf("go", "/work/b.go", "x\n"))
	if err != nil {
		t.Fatalf("the first answer: %v", err)
	}
	unexpected := &badDelta{mode: 3}
	reg3 := NewRegistry()
	reg3.RegisterSemanticTokens("test", Selector{}, unexpected)
	_, err = NewSemanticTokensCache(reg3).Get(ctx, doc)
	routeOf(t, err, "SemanticTokensCache.Get.SemanticTokensDeltaUnexpected")

	// a canceled request waits for nothing
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	p.fail = context.Canceled
	doc.Version = 5
	if _, err := cache.Get(cctx, doc); err == nil {
		t.Error("a canceled request failed to fail")
	}
}

// badDelta answers a full result, then edits that do not fit it, then a full result, then edits to nothing.
type badDelta struct {
	calls    int
	mode     int
	lastPrev string
}

func (b *badDelta) SemanticLegend() SemanticLegend { return SemanticLegend{Types: []string{"a"}} }

func (b *badDelta) ProvideSemanticTokens(_ context.Context, _ Document, prev string) (*SemanticUpdate, error) {
	b.calls++
	b.lastPrev = prev
	switch {
	case b.mode == 3:
		return &SemanticUpdate{ResultID: "x", Delta: true, Edits: []SemanticEdit{{Start: 0}}, Encoding: lsp.UTF16}, nil
	case b.calls == 2:
		return &SemanticUpdate{ResultID: "y", Delta: true, Edits: []SemanticEdit{{Start: 99}}, Encoding: lsp.UTF16}, nil
	}
	return &SemanticUpdate{ResultID: "z", Data: []uint32{0, 0, 1, 0, 0}, Encoding: lsp.UTF16}, nil
}

func TestServerProviderSemanticTokens(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	caps := `{"semanticTokensProvider":{"legend":{"tokenTypes":["variable","function"],"tokenModifiers":["declaration"]},"range":true,"full":{"delta":true}}}`
	c := connect(t, caps, map[string]lsptest.Response{
		lsp.MethodSemanticTokensFull:  result(`{"resultId":"1","data":[0,0,1,0,0,0,2,1,1,1]}`),
		lsp.MethodSemanticTokensRange: result(`{"data":[0,3,1,0,0]}`),
	})
	sp := c.sp
	if l := sp.SemanticLegend(); !reflect.DeepEqual(l.Types, []string{"variable", "function"}) || !reflect.DeepEqual(l.Modifiers, []string{"declaration"}) {
		t.Errorf("legend = %+v", l)
	}
	up, err := sp.ProvideSemanticTokens(ctx, doc, "")
	if err != nil || up == nil || up.Delta || up.ResultID != "1" || len(up.Data) != 10 || up.Encoding != lsp.UTF16 {
		t.Fatalf("full = %+v, %v", up, err)
	}
	if n := len(c.requests(lsp.MethodSemanticTokensDelta)); n != 0 {
		t.Errorf("%d delta requests for a first request", n)
	}
	rg, err := sp.ProvideSemanticTokensRange(ctx, doc, rng(0, 0, 1, 0))
	if err != nil || rg == nil || len(rg.Data) != 5 {
		t.Errorf("range = %+v, %v", rg, err)
	}
	var rp struct {
		Range lsp.Range `json:"range"`
	}
	if err := json.Unmarshal(c.requests(lsp.MethodSemanticTokensRange)[0], &rp); err != nil || rp.Range.End.Line != 1 {
		t.Errorf("range params = %+v, %v", rp, err)
	}

	// with a previous result the server is asked for a delta, which it may answer with edits or with everything
	d := connect(t, caps, map[string]lsptest.Response{
		lsp.MethodSemanticTokensDelta: result(`{"resultId":"2","edits":[{"start":1,"deleteCount":2,"data":[7]}]}`),
	})
	up, err = d.sp.ProvideSemanticTokens(ctx, doc, "1")
	if err != nil || up == nil || !up.Delta || up.ResultID != "2" || len(up.Edits) != 1 || up.Edits[0].Start != 1 {
		t.Errorf("delta = %+v, %v", up, err)
	}
	e := connect(t, caps, map[string]lsptest.Response{
		lsp.MethodSemanticTokensDelta: result(`{"resultId":"3","data":[0,0,1,0,0]}`),
	})
	up, err = e.sp.ProvideSemanticTokens(ctx, doc, "1")
	if err != nil || up == nil || up.Delta || up.ResultID != "3" || len(up.Data) != 5 {
		t.Errorf("a full answer to a delta request = %+v, %v", up, err)
	}

	// a server without the capability is not asked
	n := connect(t, `{}`, nil)
	if up, err := n.sp.ProvideSemanticTokens(ctx, doc, ""); up != nil || err != nil || len(n.requests(lsp.MethodSemanticTokensFull)) != 0 {
		t.Errorf("no capability: %+v, %v", up, err)
	}
	// and a server without ranges answers none
	f := connect(t, `{"semanticTokensProvider":{"legend":{"tokenTypes":[],"tokenModifiers":[]},"full":true}}`, nil)
	if up, err := f.sp.ProvideSemanticTokensRange(ctx, doc, rng(0, 0, 1, 0)); up != nil || err != nil {
		t.Errorf("range without the capability: %+v, %v", up, err)
	}
}
