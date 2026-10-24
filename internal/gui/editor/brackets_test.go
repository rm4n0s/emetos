package editor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// curlyBrackets is the one language of these tests: {} [] ().
type curlyBrackets struct{}

func (curlyBrackets) Language(code int) *brackets.Language { return curlyLanguage }

var curlyLanguage = brackets.NewLanguage(1, []brackets.Pair{{Open: "{", Close: "}"}, {Open: "[", Close: "]"}, {Open: "(", Close: ")"}}, nil, false)

// withBrackets makes a view model over a text, with its bracket pairs and the cursors.
func withBrackets(t *testing.T, text string, sels ...buffer.Selection) *ViewModel {
	t.Helper()
	vm := NewViewModel(newTestDoc(text), testOptions(), nil)
	ix, stop := brackets.Attach(vm.Document(), nil, curlyBrackets{}, 1)
	t.Cleanup(stop)
	vm.SetBrackets(ix)
	vm.SetFocused(true)
	vm.Cursors().SetSelections(sels)
	return vm
}

func TestBracketCommandsMoveAndSelect(t *testing.T) {
	const text = "f(a, [b, {c}], d) x"
	tests := []struct {
		name string
		id   string
		args any
		from buffer.Selection
		want buffer.Selection
	}{
		{"go to the matching bracket", "editor.action.jumpToBracket", nil, cur(0, 1), cur(0, 16)},
		{"and back", "editor.action.jumpToBracket", nil, cur(0, 16), cur(0, 1)},
		{"go to the closing bracket of the pair around", "editor.action.jumpToBracket", nil, cur(0, 10), cur(0, 11)},
		{"select to bracket", "editor.action.selectToBracket", nil, cur(0, 3), sel(0, 1, 0, 17)},
		{"select to bracket from the closing one", "editor.action.selectToBracket", nil, cur(0, 16), sel(0, 17, 0, 1)},
		{"select inside the brackets", "editor.action.selectToBracket", map[string]any{"selectBrackets": false}, cur(0, 3), sel(0, 2, 0, 16)},
		{"select with the brackets when asked", "editor.action.selectToBracket", map[string]any{"selectBrackets": true}, cur(0, 3), sel(0, 1, 0, 17)},
		{"nothing to select", "editor.action.selectToBracket", nil, cur(0, 18), cur(0, 18)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := withBrackets(t, text, tc.from)
			if err := vm.RunCommand(tc.id, tc.args); err != nil {
				t.Fatal(err)
			}
			if got := vm.Cursors().Primary().Selection; got != tc.want {
				t.Errorf("selection = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRemoveBracketsCommand(t *testing.T) {
	vm := withBrackets(t, "f(a, [b])", cur(0, 7))
	if err := vm.RunCommand("editor.action.removeBrackets", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "f(a, b)" {
		t.Errorf("text = %q", got)
	}
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "f(a, [b])" {
		t.Errorf("after undo: %q", got)
	}
}

func TestBracketCommandsWithoutBracketsDoNothing(t *testing.T) {
	vm := NewViewModel(newTestDoc("f(a)"), testOptions(), nil)
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 1)})
	for _, id := range []string{"editor.action.jumpToBracket", "editor.action.selectToBracket", "editor.action.removeBrackets"} {
		if err := vm.RunCommand(id, nil); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if vm.Document().Text() != "f(a)" || vm.Cursors().Primary().Selection != cur(0, 1) {
		t.Error("a document without bracket pairs is left alone")
	}
	// A document in large file mode has an index that is disabled.
	vm = withBrackets(t, "f(a)", cur(0, 1))
	vm.SetBrackets(brackets.New(plainSource{"f(a)"}, curlyBrackets{}, false))
	if err := vm.RunCommand("editor.action.jumpToBracket", nil); err != nil || vm.Cursors().Primary().Selection != cur(0, 1) {
		t.Error("a disabled index finds nothing")
	}
}

type plainSource struct{ text string }

func (s plainSource) LineCount() int                { return 1 }
func (s plainSource) LineContent(int) string        { return s.text }
func (s plainSource) LineTokens(int) []tokens.Token { return nil }
func (s plainSource) Language() int                 { return 1 }
func (s plainSource) TokensComplete() bool          { return true }

func TestExpandSelectionStepsThroughBrackets(t *testing.T) {
	vm := withBrackets(t, "foo(bar, [1, 2])", cur(0, 12))
	var got []buffer.Selection
	for range 5 {
		if err := vm.RunCommand("editor.action.smartSelect.expand", nil); err != nil {
			t.Fatal(err)
		}
		got = append(got, vm.Cursors().Primary().Selection)
	}
	// The inside of [ ], the pair, the inside of ( ), the pair, then the whole text.
	want := []buffer.Selection{sel(0, 10, 0, 14), sel(0, 9, 0, 15), sel(0, 4, 0, 15), sel(0, 3, 0, 16), sel(0, 0, 0, 16)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selections = %v\nwant %v", got, want)
	}
	for range 5 {
		if err := vm.RunCommand("editor.action.smartSelect.shrink", nil); err != nil {
			t.Fatal(err)
		}
	}
	if vm.Cursors().Primary().Selection != cur(0, 12) {
		t.Errorf("shrinking goes back to the cursor: %v", vm.Cursors().Primary().Selection)
	}
}

func TestMatchedBracketsFollowTheOption(t *testing.T) {
	const text = "f(a, [b])"
	tests := []struct {
		name  string
		mode  string
		focus bool
		sels  []buffer.Selection
		want  []buffer.Range
	}{
		{"next to a bracket", "always", true, []buffer.Selection{cur(0, 2)}, []buffer.Range{rng(0, 1, 0, 2), rng(0, 8, 0, 9)}},
		{"after a closing bracket", "near", true, []buffer.Selection{cur(0, 9)}, []buffer.Range{rng(0, 1, 0, 2), rng(0, 8, 0, 9)}},
		{"inside a pair: always shows the pair around", "always", true, []buffer.Selection{cur(0, 4)}, []buffer.Range{rng(0, 1, 0, 2), rng(0, 8, 0, 9)}},
		{"inside a pair: near shows nothing", "near", true, []buffer.Selection{cur(0, 4)}, nil},
		{"never", "never", true, []buffer.Selection{cur(0, 2)}, nil},
		{"not focused", "always", false, []buffer.Selection{cur(0, 2)}, nil},
		{"a selection with text has none", "always", true, []buffer.Selection{sel(0, 2, 0, 4)}, nil},
		{"several cursors", "always", true, []buffer.Selection{cur(0, 2), cur(0, 6)}, []buffer.Range{rng(0, 1, 0, 2), rng(0, 8, 0, 9), rng(0, 5, 0, 6), rng(0, 7, 0, 8)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := withBrackets(t, text, tc.sels...)
			opts := vm.Options()
			opts.MatchBrackets = tc.mode
			vm.SetOptions(opts)
			vm.SetFocused(tc.focus)
			if got := vm.matchedBrackets(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("matched = %v, want %v", got, tc.want)
			}
		})
	}
	// More cursors than VS Code looks at: none.
	vm := withBrackets(t, "(a)")
	sels := make([]buffer.Selection, 0, maxBracketMatchCursors+1)
	for range maxBracketMatchCursors + 1 {
		sels = append(sels, cur(0, 1))
	}
	vm.Cursors().SetSelections(sels)
	if vm.Cursors().Len() > maxBracketMatchCursors && vm.matchedBrackets() != nil {
		t.Error("a hundred cursors or more are not matched")
	}
}

func rng(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: positionOf(l1, c1), End: positionOf(l2, c2)}
}

func TestBracketColorsFollowTheNestingLevel(t *testing.T) {
	colors := BracketColors{Unexpected: ui.NewColor(0xFFFF0000)}
	for i := range colors.Levels {
		colors.Levels[i] = ui.NewColor(0xFF000000 | uint32(i+1)*0x111111)
	}
	colors.Levels[3] = ui.NewColor(0) // the fourth level has no color: the token's stays
	vm := withBrackets(t, "( ( ( ( ( ( ( ) ) ) ) ) ) )\n(]\n)\n{[]}\n(")
	opts := vm.Options()
	opts.BracketPairColorization = true
	vm.SetOptions(opts)
	deep := vm.bracketSpans(colors, 0, 0, len("( ( ( ( ( ( ( ) ) ) ) ) ) )"))
	// 14 brackets; the fourth level (and the fourth from the end) has no color, so 12 spans.
	if len(deep) != 12 {
		t.Fatalf("%d spans: %+v", len(deep), deep)
	}
	if deep[0].start != 0 || deep[0].color != colors.Levels[0] || deep[1].start != 2 || deep[1].color != colors.Levels[1] {
		t.Errorf("first levels: %+v", deep[:2])
	}
	if last := deep[len(deep)-1]; last.start != 26 || last.color != colors.Levels[0] {
		t.Errorf("the last closing bracket matches the first: %+v", last)
	}
	// Levels repeat after the sixth.
	if deep[6].color != colors.Levels[0] {
		t.Errorf("the seventh level uses the first color again: %+v", deep[6])
	}
	// The ] that closes nothing and the ( that is never closed are unexpected; the ( of the
	// line above is closed by the ) below it.
	line1 := vm.bracketSpans(colors, 1, 0, 2)
	if len(line1) != 2 || line1[0].color != colors.Levels[0] || line1[1].color != colors.Unexpected {
		t.Errorf("a ] that closes nothing is red: %+v", line1)
	}
	if line2 := vm.bracketSpans(colors, 2, 0, 1); len(line2) != 1 || line2[0].color != colors.Levels[0] {
		t.Errorf("the ) closes the ( of the line above: %+v", line2)
	}
	if line4 := vm.bracketSpans(colors, 4, 0, 1); len(line4) != 1 || line4[0].color != colors.Unexpected {
		t.Errorf("a ( that is never closed is red: %+v", line4)
	}
	// A row of a wrapped line: offsets are in the row and brackets outside it are left out.
	row := vm.bracketSpans(colors, 3, 1, 3)
	if len(row) != 2 || row[0].start != 0 || row[1].start != 1 {
		t.Errorf("a part of a line: %+v", row)
	}
	// With the option off, or the colors of independent pools.
	opts.BracketPairColorization = false
	vm.SetOptions(opts)
	if vm.bracketSpans(colors, 3, 0, 4) != nil {
		t.Error("colorization is off")
	}
	opts.BracketPairColorization, opts.IndependentColorPool = true, true
	vm.SetOptions(opts)
	pool := vm.bracketSpans(colors, 3, 0, 4)
	// {[]}: with independent pools each bracket type counts its own levels, all 0.
	if len(pool) != 4 || pool[1].color != colors.Levels[0] || pool[0].color != colors.Levels[0] {
		t.Errorf("independent pools: %+v", pool)
	}
	// No index, no spans.
	vm.SetBrackets(nil)
	if vm.bracketSpans(colors, 0, 0, 5) != nil || vm.selectionProviders() != nil {
		t.Error("without an index there are no bracket spans or steps")
	}
}

func TestOverlaySpansKeepTheFontStyle(t *testing.T) {
	red, blue, green := ui.NewColor(0xFFFF0000), ui.NewColor(0xFF0000FF), ui.NewColor(0xFF00FF00)
	base := []span{{start: 0, end: 4, color: red, style: 1}, {start: 4, end: 8, color: blue, style: 2}}
	over := []span{{start: 3, end: 5, color: green}, {start: 7, end: 8, color: green}}
	got := overlaySpans(base, over)
	want := []span{
		{0, 3, red, 1}, {3, 5, green, 1}, {5, 7, blue, 2}, {7, 8, green, 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("spans = %+v\nwant %+v", got, want)
	}
	if !reflect.DeepEqual(overlaySpans(base, nil), base) {
		t.Error("nothing over, nothing changes")
	}
	// Brackets of a row with no tokens come with no base.
	if got := overlaySpans(nil, over); !reflect.DeepEqual(got, over) {
		t.Errorf("over without base: %+v", got)
	}
}

func TestBracketColorsOfTheBundledThemes(t *testing.T) {
	dark := kit.DarkModern()
	if dark.EditorBracketHighlightForeground[0] != ui.NewColor(0xFFFFD700) || dark.EditorBracketHighlightForeground[1] != ui.NewColor(0xFFDA70D6) || dark.EditorBracketHighlightForeground[2] != ui.NewColor(0xFF179FFF) {
		t.Errorf("dark levels: %v", dark.EditorBracketHighlightForeground)
	}
	if dark.EditorBracketHighlightForeground[3].Alpha() != 0 {
		t.Error("the fourth level has no color in the default themes")
	}
	if dark.EditorBracketMatchBackground == ui.NewColor(0) || dark.EditorBracketMatchBorder != ui.NewColor(0xFF888888) {
		t.Errorf("match: %v %v", dark.EditorBracketMatchBackground, dark.EditorBracketMatchBorder)
	}
	if dark.EditorBracketUnexpectedForeground.Alpha() == 0 {
		t.Error("unexpected brackets are colored")
	}
}

func TestBracketsOfARegisteredLanguageAreFound(t *testing.T) {
	reg, err := language.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	vm := NewViewModel(newTestDoc("x := f(a[1])\n"), testOptions(), nil)
	ix, stop := brackets.Attach(vm.Document(), nil, brackets.NewRegistryLanguages(reg), reg.Code("go"))
	defer stop()
	vm.SetBrackets(ix)
	if got := ix.Pairs(rng(0, 0, 1, 0)); len(got) != 2 {
		t.Errorf("%d pairs", len(got))
	}
}

var _ = testutil.MonoFamily

// bracketSample is Go with nested brackets of every kind, a string and a comment that hold some,
// and a bracket that closes nothing.
const bracketSample = `package main

func f(a []int, m map[string][]int) (int, error) {
	if len(a) > 0 && (a[0] > 1 || m["}"][0] < (2+3)*4) {
		return a[m["k"][0]], nil // ) ] }
	}
	return 0, nil
}}
`

// highlightedBrackets is highlighted, with the bracket pairs of the document too, the options of
// bracket matching and colors on, and a cursor at a position.
func highlightedBrackets(t *testing.T, text string, at buffer.Position) (*ViewModel, *tokens.Tokenizer) {
	t.Helper()
	vm, tok := highlighted(t, text)
	reg, err := language.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	ix, stop := brackets.Attach(vm.Document(), tok, brackets.NewRegistryLanguages(reg), reg.Code("go"))
	t.Cleanup(stop)
	vm.SetBrackets(ix)
	opts := vm.Options()
	opts.BracketPairColorization, opts.MatchBrackets = true, "always"
	vm.SetOptions(opts)
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(at)})
	tok.Settle()
	return vm, tok
}

func TestGoldenBracketColors(t *testing.T) {
	// The cursor is right after the ( of (2+3), so that pair is boxed.
	line := strings.Split(bracketSample, "\n")[3]
	at := positionOf(3, strings.Index(line, "(2+3)")+1)
	for _, tc := range []struct {
		golden string
		theme  string
	}{
		{"brackets_dark", "Dark Modern"},
		{"brackets_light", "Light Modern"},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			vm, _ := highlightedBrackets(t, bracketSample, at)
			th, err := theme.LoadBundled(tc.theme)
			if err != nil {
				t.Fatal(err)
			}
			ts := testutil.New(t, ui.Size{Width: 720, Height: 260})
			pumpHighlighted(t, ts, vm, bundledColors(t, tc.theme), th.TokenTheme(nil))
			noErrors(t, ts)
			if !vm.Focused() {
				t.Fatal("the editor has the focus")
			}
			if got := vm.matchedBrackets(); len(got) != 2 {
				t.Fatalf("matched brackets = %v", got)
			}
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestExpandSelectionStepsThroughStringsOfTheTokens(t *testing.T) {
	vm, tok := highlighted(t, "x := f(\"hello world\")\n")
	tok.Settle()
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 15)})
	var got []buffer.Selection
	for range 4 {
		if err := vm.RunCommand("editor.action.smartSelect.expand", nil); err != nil {
			t.Fatal(err)
		}
		got = append(got, vm.Cursors().Primary().Selection)
	}
	// The word, the text inside the quotes, the string, the line.
	want := []buffer.Selection{sel(0, 14, 0, 19), sel(0, 8, 0, 19), sel(0, 7, 0, 20), sel(0, 0, 0, 21)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selections = %v\nwant %v", got, want)
	}
}
