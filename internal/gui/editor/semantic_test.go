package editor

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func stok(line, col, n int) langfeatures.SemanticToken {
	return langfeatures.SemanticToken{Line: line, Col: col, Len: n}
}

func change(oldR, newR buffer.Range, newText string) document.Change {
	return document.Change{OldRange: oldR, NewRange: newR, NewText: newText}
}

func TestSemanticTokensFollowTheEdits(t *testing.T) {
	// three lines of tokens: line 0 `foo bar baz`, line 1 `qux`, line 2 `end`
	base := func() []langfeatures.SemanticToken {
		return []langfeatures.SemanticToken{stok(0, 0, 3), stok(0, 4, 3), stok(0, 8, 3), stok(1, 0, 3), stok(2, 0, 3)}
	}
	tests := []struct {
		name    string
		changes []document.Change
		want    []langfeatures.SemanticToken
	}{
		{"typing after a token", []document.Change{change(rg(0, 3, 0, 3), rg(0, 3, 0, 4), "x")},
			[]langfeatures.SemanticToken{stok(0, 0, 3), stok(0, 5, 3), stok(0, 9, 3), stok(1, 0, 3), stok(2, 0, 3)}},
		{"typing inside a token drops it", []document.Change{change(rg(0, 5, 0, 5), rg(0, 5, 0, 6), "x")},
			[]langfeatures.SemanticToken{stok(0, 0, 3), stok(0, 9, 3), stok(1, 0, 3), stok(2, 0, 3)}},
		{"typing before a token", []document.Change{change(rg(0, 0, 0, 0), rg(0, 0, 0, 2), "  ")},
			[]langfeatures.SemanticToken{stok(0, 2, 3), stok(0, 6, 3), stok(0, 10, 3), stok(1, 0, 3), stok(2, 0, 3)}},
		{"deleting text between tokens", []document.Change{change(rg(0, 3, 0, 4), rg(0, 3, 0, 3), "")},
			[]langfeatures.SemanticToken{stok(0, 0, 3), stok(0, 3, 3), stok(0, 7, 3), stok(1, 0, 3), stok(2, 0, 3)}},
		{"replacing a token", []document.Change{change(rg(0, 4, 0, 7), rg(0, 4, 0, 6), "xy")},
			[]langfeatures.SemanticToken{stok(0, 0, 3), stok(0, 7, 3), stok(1, 0, 3), stok(2, 0, 3)}},
		{"a line break in the middle of the first line moves the rest to the next", []document.Change{change(rg(0, 7, 0, 7), rg(0, 7, 1, 0), "\n")},
			[]langfeatures.SemanticToken{stok(0, 0, 3), stok(0, 4, 3), stok(1, 1, 3), stok(2, 0, 3), stok(3, 0, 3)}},
		{"a line break before the first token", []document.Change{change(rg(0, 0, 0, 0), rg(0, 0, 1, 0), "\n")},
			[]langfeatures.SemanticToken{stok(1, 0, 3), stok(1, 4, 3), stok(1, 8, 3), stok(2, 0, 3), stok(3, 0, 3)}},
		{"joining two lines", []document.Change{change(rg(0, 11, 1, 0), rg(0, 11, 0, 11), "")},
			[]langfeatures.SemanticToken{stok(0, 0, 3), stok(0, 4, 3), stok(0, 8, 3), stok(0, 11, 3), stok(1, 0, 3)}},
		{"deleting a line", []document.Change{change(rg(1, 0, 2, 0), rg(1, 0, 1, 0), "")},
			[]langfeatures.SemanticToken{stok(0, 0, 3), stok(0, 4, 3), stok(0, 8, 3), stok(2-1, 0, 3)}},
		{"replacing several lines by one", []document.Change{change(rg(0, 5, 2, 1), rg(0, 5, 0, 6), "x")},
			[]langfeatures.SemanticToken{stok(0, 0, 3)}},
		{"two changes of one batch, in the coordinates before it", []document.Change{
			change(rg(0, 0, 0, 0), rg(0, 0, 0, 1), "a"), change(rg(1, 0, 1, 0), rg(1, 0, 1, 1), "b")},
			[]langfeatures.SemanticToken{stok(0, 1, 3), stok(0, 5, 3), stok(0, 9, 3), stok(1, 1, 3), stok(2, 0, 3)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tk := &langfeatures.SemanticTokens{Tokens: base()}
			moveTokens(tk, tt.changes)
			// the batch of two changes: the second is on line 1 of the text before the batch, and after it on line 1 as well
			if !reflect.DeepEqual(tk.Tokens, tt.want) {
				t.Errorf("tokens = %v\nwant %v", tk.Tokens, tt.want)
			}
		})
	}
}

func TestMergeSemantic(t *testing.T) {
	red, blue, green, base := ui.NewColor(0xFFFF0000), ui.NewColor(0xFF0000FF), ui.NewColor(0xFF00FF00), ui.NewColor(0xFFCCCCCC)
	italic := theme.SemanticStyle{Set: theme.FontStyleItalic, On: theme.FontStyleItalic}
	tm := []span{{start: 0, end: 4, color: red, style: theme.FontStyleBold}, {start: 6, end: 10, color: blue}}
	tests := []struct {
		name string
		sem  []semanticSpan
		want []span
	}{
		{"no tokens", nil, tm},
		{"a color replaces the TextMate one and the font style stays", []semanticSpan{{start: 1, end: 3, color: green, hasColor: true}},
			[]span{{start: 0, end: 1, color: red, style: theme.FontStyleBold}, {start: 1, end: 3, color: green, style: theme.FontStyleBold},
				{start: 3, end: 4, color: red, style: theme.FontStyleBold}, {start: 4, end: 6, color: base}, {start: 6, end: 10, color: blue}}},
		{"a font style alone keeps the color", []semanticSpan{{start: 6, end: 8, style: italic}},
			[]span{{start: 0, end: 4, color: red, style: theme.FontStyleBold}, {start: 4, end: 6, color: base}, {start: 6, end: 8, color: blue, style: theme.FontStyleItalic}, {start: 8, end: 10, color: blue}}},
		{"text no TextMate span covers is in the color of the editor", []semanticSpan{{start: 4, end: 6, color: green, hasColor: true}, {start: 12, end: 14, style: italic}},
			[]span{{start: 0, end: 4, color: red, style: theme.FontStyleBold}, {start: 4, end: 6, color: green}, {start: 6, end: 10, color: blue},
				{start: 10, end: 12, color: base}, {start: 12, end: 14, color: base, style: theme.FontStyleItalic}}},
		{"a token over two spans", []semanticSpan{{start: 2, end: 8, color: green, hasColor: true, style: italic}},
			[]span{{start: 0, end: 2, color: red, style: theme.FontStyleBold}, {start: 2, end: 4, color: green, style: theme.FontStyleBold | theme.FontStyleItalic},
				{start: 4, end: 6, color: green, style: theme.FontStyleItalic}, {start: 6, end: 8, color: green, style: theme.FontStyleItalic}, {start: 8, end: 10, color: blue}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeSemantic(tm, tt.sem, base)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("spans = %+v\nwant %+v", got, tt.want)
			}
			for i := 1; i < len(got); i++ {
				if got[i].start < got[i-1].end {
					t.Errorf("spans overlap: %+v", got)
				}
			}
		})
	}
}

// semanticProvider answers the tokens the test gives, holding the request while its gate is closed.
type semanticProvider struct {
	mu      sync.Mutex
	data    []uint32
	asked   int
	gate    chan struct{}
	rangeOK bool
	ranges  int
}

func (p *semanticProvider) SemanticLegend() langfeatures.SemanticLegend {
	return langfeatures.SemanticLegend{Types: []string{"namespace", "function", "variable"}, Modifiers: []string{"declaration", "readonly"}}
}

func (p *semanticProvider) ProvideSemanticTokens(ctx context.Context, _ langfeatures.Document, _ string) (*langfeatures.SemanticUpdate, error) {
	p.mu.Lock()
	p.asked++
	gate, data := p.gate, append([]uint32(nil), p.data...)
	p.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &langfeatures.SemanticUpdate{Data: data, Encoding: lsp.UTF16}, nil
}

func (p *semanticProvider) ProvideSemanticTokensRange(context.Context, langfeatures.Document, langfeatures.Range) (*langfeatures.SemanticUpdate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ranges++
	if !p.rangeOK {
		return nil, nil
	}
	return &langfeatures.SemanticUpdate{Data: []uint32{0, 8, 4, 1, 0}, Encoding: lsp.UTF16}, nil
}

func (p *semanticProvider) setData(d []uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data = d
}

func (p *semanticProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.asked
}

const semanticText = "package main\n\nfunc main() {\n}\n"

// semanticRig is an editor with the token theme of Dark Modern.
func semanticRig(t *testing.T, tweak func(*Options)) (*lang129Rig, *semanticProvider) {
	t.Helper()
	r := newLang129Rig(t, semanticText, tweak)
	p := &semanticProvider{data: []uint32{0, 0, 7, 0, 0, 2, 0, 4, 1, 1, 0, 5, 4, 1, 0}}
	r.reg.RegisterSemanticTokens("test", r.selector(), p)
	r.vm.SetSemanticTheme(darkModernTokens(t))
	return r, p
}

func TestSemanticTokensAreAskedForAndDrawnOverTheGrammar(t *testing.T) {
	r, p := semanticRig(t, nil)
	r.host()
	r.settle()
	if p.count() != 1 || r.vm.SemanticTokenCount() != 3 {
		t.Fatalf("%d requests, %d tokens", p.count(), r.vm.SemanticTokenCount())
	}
	// the types of the legend are the classes of the theme: namespace and function color as the grammar's scopes do
	tt := darkModernTokens(t)
	want := func(scope string) ui.Color {
		m, ok := tt.Match([]string{scope})
		if !ok {
			t.Fatalf("Dark Modern has no rule for %s", scope)
		}
		return hexColor(t, tt.Color(m.Foreground))
	}
	spans := r.vm.semanticSpans(0, 0, 12)
	if len(spans) != 1 || spans[0].color != want("entity.name.namespace") {
		t.Errorf("line 0 = %+v, want the color of entity.name.namespace", spans)
	}
	spans = r.vm.semanticSpans(2, 0, 11)
	if len(spans) != 2 || spans[0].color != want("entity.name.function") || spans[1].start != 5 || spans[1].end != 9 {
		t.Errorf("line 2 = %+v", spans)
	}
	if got := r.vm.semanticSpans(2, 6, 9); len(got) != 1 || got[0].start != 0 || got[0].end != 3 {
		t.Errorf("a row that starts inside a token has it cut: %+v", got)
	}
	// and nothing is drawn with semantic highlighting off
	o := r.vm.Options()
	o.Lens.Semantic = SemanticOff
	r.vm.SetOptions(o)
	if r.vm.SemanticHighlightingOn() || r.vm.semanticSpans(0, 0, 12) != nil {
		t.Error("tokens are drawn although the setting is off")
	}
}

func TestSemanticHighlightingFollowsTheSettingAndTheTheme(t *testing.T) {
	tests := []struct {
		name  string
		mode  string
		theme string
		want  bool
	}{
		{"on", SemanticOn, "Dark Modern", true},
		{"off", SemanticOff, "Dark Modern", false},
		{"configured by a theme that asks for it", SemanticByTheme, "Dark Modern", true},
		{"configured by a theme that does not", SemanticByTheme, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newLang129Rig(t, semanticText, func(o *Options) { o.Lens.Semantic = tt.mode })
			if tt.theme != "" {
				r.vm.SetSemanticTheme(darkModernTokens(t))
			} else {
				th := &theme.Theme{Type: theme.TypeDark, Colors: map[string]string{}, SemanticTokenColors: map[string]any{}}
				r.vm.SetSemanticTheme(th.TokenTheme(nil))
			}
			if got := r.vm.SemanticHighlightingOn(); got != tt.want {
				t.Errorf("on = %v, want %v", got, tt.want)
			}
		})
	}
	// the rules of the settings win over the theme
	r, _ := semanticRig(t, func(o *Options) {
		o.Lens.SemanticRules = map[string]any{"namespace": "#123456"}
	})
	r.host()
	r.settle()
	spans := r.vm.semanticSpans(0, 0, 12)
	if len(spans) != 1 || spans[0].color != hexColor(t, "#123456") {
		t.Errorf("namespace with a rule of the settings = %+v", spans)
	}
}

func TestStaleSemanticTokenAnswersAreDropped(t *testing.T) {
	r, p := semanticRig(t, nil)
	p.gate = make(chan struct{})
	r.host()
	deadline := time.Now().Add(5 * time.Second)
	for p.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	r.typeAt(0, 0, "// c\n")
	p.setData([]uint32{1, 0, 7, 0, 0, 2, 0, 4, 1, 1, 0, 5, 4, 1, 0})
	close(p.gate)
	r.settle()
	if r.vm.SemanticTokenCount() != 0 {
		t.Errorf("the answer for an older version was taken: %d tokens", r.vm.SemanticTokenCount())
	}
	r.clock.advance(2 * time.Second)
	r.settle()
	if r.vm.SemanticTokenCount() != 3 || p.count() != 2 {
		t.Errorf("%d tokens after %d requests", r.vm.SemanticTokenCount(), p.count())
	}
}

func TestSemanticTokensMoveWithTheTextUntilTheNextAnswer(t *testing.T) {
	r, p := semanticRig(t, nil)
	r.host()
	r.settle()
	// a line is inserted above: the tokens of the lines below go down, and nothing is asked until the delay
	r.typeAt(0, 0, "// c\n")
	if got := r.vm.semanticSpans(3, 0, 11); len(got) != 2 {
		t.Errorf("the tokens did not follow the line: %+v", got)
	}
	if got := r.vm.semanticSpans(2, 0, 11); len(got) != 0 {
		t.Errorf("the old line has tokens: %+v", got)
	}
	if p.count() != 1 {
		t.Errorf("a request before the delay: %d", p.count())
	}
	// typing inside `main` drops its token (it is not what the server classified) and moves nothing else
	r.typeAt(3, 7, "x")
	p.setData([]uint32{1, 0, 7, 0, 0, 2, 0, 4, 1, 1, 0, 5, 5, 1, 0})
	if got := r.vm.semanticSpans(3, 0, 12); len(got) != 1 {
		t.Errorf("tokens of the line with a letter typed in `main`: %+v", got)
	}
	r.clock.advance(2 * time.Second)
	r.settle()
	if p.count() != 2 || r.vm.SemanticTokenCount() != 3 {
		t.Errorf("%d requests, %d tokens", p.count(), r.vm.SemanticTokenCount())
	}
}

func TestTheRefreshOfTheServerAsksAgain(t *testing.T) {
	r, p := semanticRig(t, nil)
	r.host()
	r.settle()
	r.vm.FeaturesRefreshed(langfeatures.FeatureSemanticTokens)
	r.settle()
	if p.count() != 2 {
		t.Errorf("%d requests after a refresh: the cache had the tokens for this version", p.count())
	}
}

func TestTheFirstViewAsksForTheVisibleRangeToo(t *testing.T) {
	r, p := semanticRig(t, nil)
	p.rangeOK = true
	p.gate = make(chan struct{})
	r.host()
	deadline := time.Now().Add(5 * time.Second)
	for p.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	// the full request is held; the range answers first and is drawn
	for end := time.Now().Add(2 * time.Second); r.vm.SemanticTokenCount() == 0 && time.Now().Before(end); {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(5 * time.Millisecond):
		}
	}
	if r.vm.SemanticTokenCount() != 1 || r.vm.sem.full {
		t.Fatalf("range tokens: %d, full %v", r.vm.SemanticTokenCount(), r.vm.sem.full)
	}
	close(p.gate)
	r.settle()
	if r.vm.SemanticTokenCount() != 3 || !r.vm.sem.full {
		t.Errorf("after the full answer: %d tokens, full %v", r.vm.SemanticTokenCount(), r.vm.sem.full)
	}
	// once the whole is known, later requests do not ask for ranges
	rangesBefore := p.ranges
	r.typeAt(0, 0, "x")
	r.clock.advance(2 * time.Second)
	r.settle()
	if p.ranges != rangesBefore {
		t.Errorf("a range was asked for although the whole is known: %d", p.ranges)
	}
}

func semanticGoldenText() (string, []uint32) {
	text := "package main\n\nimport \"fmt\"\n\ntype Point struct{ X, Y int }\n\nfunc (p Point) Dist(q Point) int {\n\tdx := p.X - q.X\n\treturn dx*dx + fmt.Sprint(q.Y)\n}\n\nfunc main() {\n\tp := Point{1, 2}\n\tfmt.Println(p.Dist(p))\n}\n"
	type tk struct{ line, col, n, typ, mods int }
	// types: 0 namespace, 1 type, 2 function, 3 method, 4 variable, 5 parameter, 6 property
	toks := []tk{
		{0, 8, 4, 0, 0}, {2, 7, 5, 0, 0}, {4, 5, 5, 1, 1}, {4, 19, 1, 6, 1}, {4, 22, 1, 6, 1}, {6, 7, 1, 5, 1}, {6, 9, 5, 1, 0}, {6, 16, 4, 3, 1},
		{6, 21, 1, 5, 1}, {6, 23, 5, 1, 0}, {7, 1, 2, 4, 1}, {7, 7, 1, 5, 0}, {7, 9, 1, 6, 0}, {7, 13, 1, 5, 0}, {7, 15, 1, 6, 0},
		{8, 8, 2, 4, 0}, {8, 11, 2, 4, 0}, {8, 16, 3, 0, 0}, {8, 20, 6, 2, 0}, {8, 27, 1, 5, 0}, {8, 29, 1, 6, 0},
		{11, 5, 4, 2, 1}, {12, 1, 1, 4, 1}, {12, 6, 5, 1, 0}, {13, 1, 3, 0, 0}, {13, 5, 7, 2, 0}, {13, 13, 1, 4, 0}, {13, 15, 4, 3, 0}, {13, 20, 1, 4, 0},
	}
	var data []uint32
	pl, pc := 0, 0
	for _, x := range toks {
		dl, dc := x.line-pl, x.col
		if dl == 0 {
			dc = x.col - pc
		}
		data = append(data, uint32(dl), uint32(dc), uint32(x.n), uint32(x.typ), uint32(x.mods))
		pl, pc = x.line, x.col
	}
	return text, data
}

// semanticLegendGo is the legend of the tokens of semanticGoldenText.
func semanticLegendGo() langfeatures.SemanticLegend {
	return langfeatures.SemanticLegend{
		Types:     []string{"namespace", "type", "function", "method", "variable", "parameter", "property"},
		Modifiers: []string{"declaration", "readonly"},
	}
}

func TestGoldenSemanticTokens(t *testing.T) {
	text, data := semanticGoldenText()
	for _, name := range []string{"Dark Modern", "Light Modern"} {
		t.Run(name, func(t *testing.T) {
			vm, tok := highlighted(t, text)
			tok.Settle() // a golden must not depend on how far the budget got
			o := vm.Options()
			o.Lens.Semantic = SemanticOn
			vm.SetOptions(o)
			th, err := theme.LoadBundled(name)
			if err != nil {
				t.Fatal(err)
			}
			tt := th.TokenTheme(nil)
			vm.SetSemanticTheme(tt)
			decoded := langfeatures.DecodeSemanticTokens(data, lsp.UTF16, vm.Document().Snapshot())
			if len(decoded) != len(data)/5 {
				t.Fatalf("%d tokens decoded of %d", len(decoded), len(data)/5)
			}
			vm.semanticSet(&langfeatures.SemanticTokens{Version: vm.Document().Version(), Legend: semanticLegendGo(), Tokens: decoded}, true)
			ts := testutil.New(t, ui.Size{Width: 640, Height: 320})
			colors := kit.DarkModern()
			if name == "Light Modern" {
				colors = bundledColors(t, name)
			}
			pumpHighlighted(t, ts, vm, colors, tt)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, "semantic_tokens_"+strings.ToLower(strings.ReplaceAll(name, " ", "_")))
		})
	}
}

// BenchmarkSemanticTokensEditCost measures what a typed key costs the tokens of a big document: they follow the edit. The
// numbers are in SPEC §9.12 ("As built (step 12.9)").
func BenchmarkSemanticTokensEditCost(b *testing.B) {
	const lines, perLine = 20000, 5
	tk := &langfeatures.SemanticTokens{Tokens: make([]langfeatures.SemanticToken, 0, lines*perLine)}
	for l := range lines {
		for i := range perLine {
			tk.Tokens = append(tk.Tokens, langfeatures.SemanticToken{Line: l, Col: i * 6, Len: 4})
		}
	}
	b.Run("a letter in a line", func(b *testing.B) {
		c := change(rg(10000, 3, 10000, 3), rg(10000, 3, 10000, 4), "x")
		for range b.N {
			moveTokensBy(tk, c)
			// undo it, so that the document keeps its size
			moveTokensBy(tk, change(rg(10000, 3, 10000, 4), rg(10000, 3, 10000, 3), ""))
		}
	})
	b.Run("a line break near the top", func(b *testing.B) {
		c := change(rg(10, 3, 10, 3), rg(10, 3, 11, 0), "\n")
		for range b.N {
			moveTokensBy(tk, c)
			moveTokensBy(tk, change(rg(10, 3, 11, 0), rg(10, 3, 10, 3), ""))
		}
	})
}

func TestTheCostOfAKeyIsBounded(t *testing.T) {
	const lines, perLine = 20000, 5 // 100000 tokens
	tk := &langfeatures.SemanticTokens{Tokens: make([]langfeatures.SemanticToken, 0, lines*perLine)}
	for l := range lines {
		for i := range perLine {
			tk.Tokens = append(tk.Tokens, langfeatures.SemanticToken{Line: l, Col: i * 6, Len: 4})
		}
	}
	start := time.Now()
	const keys = 200
	for i := range keys {
		line := 10 + i%10
		moveTokensBy(tk, change(rg(line, 3, line, 3), rg(line, 3, line+1, 0), "\n")) // the worst key: a line break near the top
	}
	per := time.Since(start) / keys
	// a line break moves every token below it: a pass over the slice, well under a frame (the budget here is generous for a race build)
	if per > 8*time.Millisecond {
		t.Errorf("a line break near the top of %d tokens costs %v", len(tk.Tokens), per)
	}
	t.Logf("a line break at the top of %d tokens: %v per key", lines*perLine, per)
	_ = fmt.Sprint
}
