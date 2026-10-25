package editor

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/language/languagetest"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// guideSample is Go with tabs: a function, a block in it, a call that spans lines.
const guideSample = `package main

func main() {
	if ok {
		run(
			a,
			b,
		)
	}
	items := []int{
		1, 2,
	}
}
`

// withGuides makes a highlighted Go view model with the indentation levels of its document, the
// bracket pairs and the options of the guides.
func withGuides(t *testing.T, text string, opts indent.GuideOptions, at buffer.Position) *ViewModel {
	t.Helper()
	vm, tok := highlighted(t, text)
	reg, err := languagetest.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	ix, stop := brackets.Attach(vm.Document(), tok, brackets.NewRegistryLanguages(reg), reg.Code("go"))
	t.Cleanup(stop)
	vm.SetBrackets(ix)
	vm.Document().SetIndentation(4, false)
	levels, stopLevels := indent.AttachLevels(vm.Document(), 4)
	t.Cleanup(stopLevels)
	vm.SetGuides(levels, false)
	o := vm.Options()
	o.Guides = opts
	o.BracketPairColorization = true
	vm.SetOptions(o)
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(at)})
	tok.Settle()
	return vm
}

// guideCell is a guide as the tests of the view model compare them.
type guideCell struct {
	visible                int
	bracket, active, horiz bool
}

func cellsOf(guides [][]indent.Guide) [][]guideCell {
	out := make([][]guideCell, len(guides))
	for i, gs := range guides {
		for _, g := range gs {
			out[i] = append(out[i], guideCell{g.VisibleColumn, g.Bracket, g.Active, g.Horizontal != nil})
		}
	}
	return out
}

func TestRowGuidesIndentOnly(t *testing.T) {
	vm := withGuides(t, guideSample, indent.DefaultGuideOptions(), buffer.Position{Line: 5, Col: 4})
	got := cellsOf(vm.rowGuides(0, 12))
	// The cursor is in `a,`, whose scope is the arguments of run(.
	want := [][]guideCell{
		nil, nil, nil,
		{{1, false, false, false}},
		{{1, false, false, false}, {5, false, false, false}},
		{{1, false, false, false}, {5, false, false, false}, {9, false, true, false}},
		{{1, false, false, false}, {5, false, false, false}, {9, false, true, false}},
		{{1, false, false, false}, {5, false, false, false}},
		{{1, false, false, false}},
		{{1, false, false, false}},
		{{1, false, false, false}, {5, false, false, false}},
		{{1, false, false, false}},
		nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("guides:\n%v\nwant\n%v", got, want)
	}
}

func TestRowGuidesBracketPairs(t *testing.T) {
	opts := indent.DefaultGuideOptions()
	opts.BracketPairs = indent.PairsAll
	vm := withGuides(t, guideSample, opts, buffer.Position{Line: 5, Col: 4})
	got := vm.rowGuides(2, 12)
	// Row 2 holds `func main() {`: the pair of the function starts there, at the column of
	// its body's guide, and the `if` block's pair starts on row 3.
	if len(got[0]) == 0 || !got[0][0].Bracket || got[0][0].VisibleColumn != 1 {
		t.Errorf("row 2: %+v", got[0])
	}
	var bracketColumns []int
	for _, g := range got[4] { // row 6, `b,`, in run(
		if g.Bracket && g.Horizontal == nil {
			bracketColumns = append(bracketColumns, g.VisibleColumn)
		}
	}
	if !reflect.DeepEqual(bracketColumns, []int{1, 5, 9}) {
		t.Errorf("bracket guides on `b,`: %v", bracketColumns)
	}
	// The pair around the cursor is the active one.
	for _, g := range got[3] { // row 5, `a,`
		if g.Bracket && g.VisibleColumn == 9 && !g.Active {
			t.Errorf("the guide of run( is not active: %+v", g)
		}
	}
}

func TestRowGuidesOff(t *testing.T) {
	vm := withGuides(t, guideSample, indent.GuideOptions{}, buffer.Position{})
	if got := vm.rowGuides(0, 12); got != nil {
		t.Errorf("guides with the options off: %v", got)
	}
	vm = withGuides(t, guideSample, indent.DefaultGuideOptions(), buffer.Position{})
	vm.SetGuides(nil, false)
	if got := vm.rowGuides(0, 12); got != nil {
		t.Errorf("guides without levels: %v", got)
	}
}

func TestGuidesFollowEditsAndTheTabSize(t *testing.T) {
	vm := withGuides(t, "a\n\tb\n\t\tc\n", indent.DefaultGuideOptions(), buffer.Position{})
	counts := func() []int {
		var out []int
		for _, gs := range vm.rowGuides(0, 3) {
			out = append(out, len(gs))
		}
		return out
	}
	if got := counts(); !reflect.DeepEqual(got, []int{0, 1, 2, 0}) {
		t.Fatalf("guides per row: %v", got)
	}
	// An edit that adds a level.
	if _, err := vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 1}, End: buffer.Position{Line: 1}}, Text: "\t"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := counts(); !reflect.DeepEqual(got, []int{0, 2, 2, 0}) {
		t.Errorf("after the edit: %v", got)
	}
	// A line added above shifts the rest.
	if _, err := vm.Document().Edit([]document.Edit{{Range: buffer.Range{}, Text: "\t\t\td\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := counts(); !reflect.DeepEqual(got, []int{3, 0, 2, 2}) {
		t.Errorf("after inserting a line: %v", got)
	}
	// A smaller tab size changes what a tab is worth, and so the width of the guides' columns,
	// but not the number of levels of lines indented with tabs.
	vm.Document().SetIndentation(2, false)
	for i, gs := range vm.rowGuides(0, 0) {
		for j, g := range gs {
			if want := 2*j + 1; g.VisibleColumn != want {
				t.Errorf("row %d guide %d is at column %d, want %d", i, j, g.VisibleColumn, want)
			}
		}
	}
}

func TestGuideColors(t *testing.T) {
	colors := bundledColors(t, "Dark Modern")
	gc := NewGuideColors(colors.EditorIndentGuideBackground, colors.EditorIndentGuideActiveBackground,
		colors.EditorBracketPairGuideBackground, colors.EditorBracketPairGuideActiveBackground, colors.EditorBracketHighlightForeground)
	if len(gc.Indent) != 1 || gc.Indent[0].Color != ui.NewColor(0xFF404040) || gc.Indent[0].Active != ui.NewColor(0xFF707070) {
		t.Errorf("indent colors of Dark Modern: %+v", gc.Indent)
	}
	// The bracket pair guides take the colors of the first three levels of brackets (the others are
	// transparent), the inactive ones at 30% of their opacity.
	if len(gc.Bracket) != 3 {
		t.Fatalf("bracket colors: %+v", gc.Bracket)
	}
	gold := ui.NewColor(0xFFFFD700)
	if gc.Bracket[0].Active != gold || gc.Bracket[0].Color != ui.NewColorFromARGB(77, 0xFF, 0xD7, 0x00) {
		t.Errorf("level 1: %+v", gc.Bracket[0])
	}
	// A level goes round the list.
	if c, ok := guideColor(gc.Bracket, 4); !ok || c != gc.Bracket[1] {
		t.Errorf("level 4 of 3 colors: %+v", c)
	}
	if _, ok := guideColor(nil, 0); ok {
		t.Error("no colors")
	}
	// A theme's own guide colors win over the brackets'.
	own := [6]ui.Color{ui.NewColor(0x80112233)}
	gc = NewGuideColors([6]ui.Color{}, [6]ui.Color{}, own, own, colors.EditorBracketHighlightForeground)
	if len(gc.Bracket) != 3 || gc.Bracket[0].Color != own[0] || gc.Bracket[0].Active != own[0] {
		t.Errorf("own colors: %+v", gc.Bracket)
	}
	if len(gc.Indent) != 0 {
		t.Errorf("no indent colors: %+v", gc.Indent)
	}
}

func TestGoldenGuides(t *testing.T) {
	opts := indent.DefaultGuideOptions()
	opts.BracketPairs = indent.PairsAll
	opts.BracketPairsHorizontal = indent.HorizontalAll
	for _, tc := range []struct{ golden, theme string }{{"guides_dark", "Dark Modern"}, {"guides_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			// The cursor is in the arguments of run(, so that its pair's guides are highlighted.
			vm := withGuides(t, guideSample, opts, buffer.Position{Line: 5, Col: 4})
			th, err := theme.LoadBundled(tc.theme)
			if err != nil {
				t.Fatal(err)
			}
			ts := testutil.New(t, ui.Size{Width: 640, Height: 300})
			colors := bundledColors(t, tc.theme)
			colors.EditorBracketPairGuideBackground, colors.EditorBracketPairGuideActiveBackground = [6]ui.Color{}, [6]ui.Color{}
			pumpHighlighted(t, ts, vm, colors, th.TokenTheme(nil))
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}
