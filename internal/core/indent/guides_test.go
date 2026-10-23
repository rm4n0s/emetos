package indent_test

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// lines is a text of lines, for the guides.
type lines []string

func (l lines) LineCount() int           { return len(l) }
func (l lines) LineContent(i int) string { return l[i] }

func guidesOf(text lines, tabSize, indentSize int, offSide bool) indent.Guides {
	return indent.Guides{Levels: indent.NewLevels(text, tabSize), Text: text, IndentSize: indentSize, OffSide: offSide, VisibleColumn: edit.VisibleColumn}
}

// guideRow is a line of a test of the guides as VS Code writes it: the number of indent guides the
// line has, the first and last line (counted from 1) of the active guide when the cursor is on the
// line, the indent of the active guide, and the text of the line.
type guideRow struct {
	guides, start, end, indent int
	text                       string
}

func assertIndentGuides(t *testing.T, rows []guideRow, indentSize int) {
	t.Helper()
	text := make(lines, len(rows))
	for i, r := range rows {
		text[i] = r.text
	}
	g := guidesOf(text, 4, indentSize, false)
	got := g.LinesIndentGuides(0, len(text)-1)
	for i, want := range rows {
		active := g.ActiveIndentGuide(i, 0, len(text)-1)
		have := guideRow{got[i], active.Start + 1, active.End + 1, active.Indent, text[i]}
		if have != want {
			t.Errorf("line %d: %+v, want %+v", i+1, have, want)
		}
	}
}

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/model/textModelWithTokens.test.ts
// (TextModel.getLineIndentGuide).
func TestVSCodeLineIndentGuides(t *testing.T) {
	tests := []struct {
		name       string
		indentSize int
		rows       []guideRow
	}{
		{"one level 2", 2, []guideRow{{0, 2, 4, 1, "A"}, {1, 2, 4, 1, "  A"}, {1, 2, 4, 1, "  A"}, {1, 2, 4, 1, "  A"}}},
		{"two levels", 2, []guideRow{{0, 2, 5, 1, "A"}, {1, 2, 5, 1, "  A"}, {1, 4, 5, 2, "  A"}, {2, 4, 5, 2, "    A"}, {2, 4, 5, 2, "    A"}}},
		{"three levels", 2, []guideRow{{0, 2, 4, 1, "A"}, {1, 3, 4, 2, "  A"}, {2, 4, 4, 3, "    A"}, {3, 4, 4, 3, "      A"}, {0, 5, 5, 0, "A"}}},
		{"decreasing indent", 2, []guideRow{{2, 1, 1, 2, "    A"}, {1, 1, 1, 2, "  A"}, {0, 1, 2, 1, "A"}}},
		{"Java", 2, []guideRow{
			{0, 2, 9, 1, "class A {"},
			{1, 3, 4, 2, "  void foo() {"},
			{2, 3, 4, 2, "    console.log(1);"},
			{2, 3, 4, 2, "    console.log(2);"},
			{1, 3, 4, 2, "  }"},
			{1, 2, 9, 1, ""},
			{1, 8, 8, 2, "  void bar() {"},
			{2, 8, 8, 2, "    console.log(3);"},
			{1, 8, 8, 2, "  }"},
			{0, 2, 9, 1, "}"},
			{0, 12, 12, 1, "interface B {"},
			{1, 12, 12, 1, "  void bar();"},
			{0, 12, 12, 1, "}"},
		}},
		{"Javadoc", 2, []guideRow{
			{0, 2, 3, 1, "/**"}, {1, 2, 3, 1, " * Comment"}, {1, 2, 3, 1, " */"},
			{0, 5, 6, 1, "class A {"}, {1, 5, 6, 1, "  void foo() {"}, {1, 5, 6, 1, "  }"}, {0, 5, 6, 1, "}"},
		}},
		{"Whitespace", 2, []guideRow{
			{0, 2, 7, 1, "class A {"}, {1, 2, 7, 1, ""}, {1, 4, 5, 2, "  void foo() {"}, {2, 4, 5, 2, "    "},
			{2, 4, 5, 2, "    return 1;"}, {1, 4, 5, 2, "  }"}, {1, 2, 7, 1, "      "}, {0, 2, 7, 1, "}"},
		}},
		{"Tabs", 4, []guideRow{
			{0, 2, 7, 1, "class A {"}, {1, 2, 7, 1, "\t\t"}, {1, 4, 5, 2, "\tvoid foo() {"}, {2, 4, 5, 2, "\t \t//hello"},
			{2, 4, 5, 2, "\t    return 2;"}, {1, 4, 5, 2, "  \t}"}, {1, 2, 7, 1, "      "}, {0, 2, 7, 1, "}"},
		}},
		{"checker.ts", 4, []guideRow{
			{0, 1, 1, 0, `/// <reference path="binder.ts"/>`},
			{0, 2, 2, 0, ""},
			{0, 3, 3, 0, "/* @internal */"},
			{0, 5, 16, 1, "namespace ts {"},
			{1, 5, 16, 1, "    let nextSymbolId = 1;"},
			{1, 5, 16, 1, "    let nextNodeId = 1;"},
			{1, 5, 16, 1, "    let nextMergeId = 1;"},
			{1, 5, 16, 1, "    let nextFlowId = 1;"},
			{1, 5, 16, 1, ""},
			{1, 11, 15, 2, "    export function getNodeId(node: Node): number {"},
			{2, 12, 13, 3, "        if (!node.id) {"},
			{3, 12, 13, 3, "            node.id = nextNodeId;"},
			{3, 12, 13, 3, "            nextNodeId++;"},
			{2, 12, 13, 3, "        }"},
			{2, 11, 15, 2, "        return node.id;"},
			{1, 11, 15, 2, "    }"},
			{0, 5, 16, 1, "}"},
		}},
		{"issue #8425 - Missing indentation lines for first level indentation", 4, []guideRow{
			{1, 2, 3, 2, "\tindent1"}, {2, 2, 3, 2, "\t\tindent2"}, {2, 2, 3, 2, "\t\tindent2"}, {1, 2, 3, 2, "\tindent1"},
		}},
		{"issue #8952 - Indentation guide lines going through text on .yml file", 4, []guideRow{
			{0, 2, 5, 1, "properties:"}, {1, 3, 5, 2, "    emailAddress:"}, {2, 3, 5, 2, "        - bla"},
			{2, 5, 5, 3, "        - length:"}, {3, 5, 5, 3, "            max: 255"}, {0, 6, 6, 0, "getters:"},
		}},
		{"issue #11892 - Indent guides look funny", 4, []guideRow{
			{0, 2, 7, 1, "function test(base) {"}, {1, 3, 6, 2, "\tswitch (base) {"}, {2, 4, 4, 3, "\t\tcase 1:"},
			{3, 4, 4, 3, "\t\t\treturn 1;"}, {2, 6, 6, 3, "\t\tcase 2:"}, {3, 6, 6, 3, "\t\t\treturn 2;"},
			{1, 2, 7, 1, "\t}"}, {0, 2, 7, 1, "}"},
		}},
		{"issue #12398 - Problem in indent guidelines", 4, []guideRow{
			{2, 2, 2, 3, "\t\t.bla"}, {3, 2, 2, 3, "\t\t\tlabel(for)"}, {0, 3, 3, 0, "include script"},
		}},
		{"tweaks - no active", 2, []guideRow{{0, 1, 1, 0, "A"}, {0, 2, 2, 0, "A"}}},
		{"tweaks - inside scope", 2, []guideRow{{0, 2, 2, 1, "A"}, {1, 2, 2, 1, "  A"}}},
		{"tweaks - scope start", 2, []guideRow{{0, 2, 2, 1, "A"}, {1, 2, 2, 1, "  A"}, {0, 2, 2, 1, "A"}}},
		{"tweaks - empty line", 2, []guideRow{{0, 2, 4, 1, "A"}, {1, 2, 4, 1, "  A"}, {1, 2, 4, 1, ""}, {1, 2, 4, 1, "  A"}, {0, 2, 4, 1, "A"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertIndentGuides(t, tt.rows, tt.indentSize) })
	}
}

// Ported from VS Code: textModelWithTokens.test.ts ('issue #49173').
func TestVSCodeActiveIndentGuideLimits(t *testing.T) {
	text := lines{
		"class A {", "\tpublic m1(): void {", "\t}", "\tpublic m2(): void {", "\t}", "\tpublic m3(): void {", "\t}",
		"\tpublic m4(): void {", "\t}", "\tpublic m5(): void {", "\t}", "}",
	}
	g := guidesOf(text, 4, 4, false)
	got := g.ActiveIndentGuide(1, 3, 8)
	if want := (indent.ActiveGuide{Start: 1, End: 8, Indent: 1}); got != want {
		t.Errorf("ActiveIndentGuide(2, 4, 9) = %+v, want %+v (counted from 0)", got, want)
	}
}

func TestGuidesOffSide(t *testing.T) {
	// A line of white space above a shallower region: the level of the region below with
	// folding.offSide (Python), inside the region that ends below without it.
	text := lines{"def f():", "    if a:", "        b", "", "    c"}
	if got := guidesOf(text, 4, 4, false).LinesIndentGuides(3, 3)[0]; got != 2 {
		t.Errorf("without offSide: %d, want 2", got)
	}
	if got := guidesOf(text, 4, 4, true).LinesIndentGuides(3, 3)[0]; got != 1 {
		t.Errorf("with offSide: %d, want 1", got)
	}
}

func TestLevels(t *testing.T) {
	text := lines{"a", "  b", "\tc", "", "   ", "\t  d", "    e"}
	lv := indent.NewLevels(text, 4)
	for i, want := range []int{0, 2, 4, -1, -1, 6, 4} {
		if got := lv.Level(i); got != want {
			t.Errorf("Level(%d) = %d, want %d", i, got, want)
		}
	}
	if got := lv.MinLevel(1, 6); got != 2 {
		t.Errorf("MinLevel(1, 6) = %d", got)
	}
	if got := lv.MinLevel(3, 4); got != math.MaxInt {
		t.Errorf("MinLevel of lines without text = %d", got)
	}
	if lv.Level(-1) != -1 || lv.Level(100) != -1 {
		t.Error("a line out of the text has no level")
	}
	lv.SetTabSize(2)
	if got := lv.Level(2); got != 2 {
		t.Errorf("after SetTabSize(2): Level(2) = %d", got)
	}
}

// mutable is a text that a test edits.
type mutable struct{ l []string }

func (m *mutable) LineCount() int           { return len(m.l) }
func (m *mutable) LineContent(i int) string { return m.l[i] }

func TestLevelsFollowEdits(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	randomLine := func() string {
		return strings.Repeat(" ", rnd.Intn(9)) + strings.Repeat("\t", rnd.Intn(2)) + strings.Repeat("x", rnd.Intn(2))
	}
	text := &mutable{}
	for i := 0; i < 300; i++ {
		text.l = append(text.l, randomLine())
	}
	lv := indent.NewLevels(text, 4)
	for round := 0; round < 200; round++ {
		// Replace the lines from..to by a random number of new ones.
		from := rnd.Intn(len(text.l))
		to := min(from+rnd.Intn(4), len(text.l)-1)
		count := 1 + rnd.Intn(4)
		fresh := make([]string, count)
		for i := range fresh {
			fresh[i] = randomLine()
		}
		text.l = append(append(append([]string{}, text.l[:from]...), fresh...), text.l[to+1:]...)
		lv.Splice(from, to, count)
		want := indent.NewLevels(text, 4)
		for probe := 0; probe < 5; probe++ {
			a := rnd.Intn(len(text.l))
			b := a + rnd.Intn(len(text.l)-a)
			if got, exp := lv.MinLevel(a, b), want.MinLevel(a, b); got != exp {
				t.Fatalf("round %d: MinLevel(%d, %d) = %d, want %d", round, a, b, got, exp)
			}
			if got, exp := lv.Level(a), want.Level(a); got != exp {
				t.Fatalf("round %d: Level(%d) = %d, want %d", round, a, got, exp)
			}
		}
	}
	// A line changed in place.
	text.l[100] = "\t\t\ty"
	lv.Invalidate(100, 100)
	if lv.Level(100) != 12 {
		t.Errorf("Level(100) = %d after Invalidate", lv.Level(100))
	}
}

// curly gives {} [] () to every language.
type curly struct{}

func (curly) Language(int) *brackets.Language { return curlyLanguage }

var curlyLanguage = brackets.NewLanguage(1, []brackets.Pair{{Open: "{", Close: "}"}, {Open: "[", Close: "]"}, {Open: "(", Close: ")"}}, nil, false)

// plainSource is a text with no tokens, in one language.
type plainSource struct{ lines }

func (plainSource) LineTokens(int) []tokens.Token { return nil }
func (plainSource) Language() int                 { return 1 }
func (plainSource) TokensComplete() bool          { return true }

func pairsOf(text lines) *brackets.Index { return brackets.New(plainSource{text}, curly{}, true) }

// guide is a bracket pair guide as the tests write it.
func guide(level, visible int, mods ...func(*indent.Guide)) indent.Guide {
	g := indent.Guide{Bracket: true, Level: level, VisibleColumn: visible, Column: -1, ForWrappedAfter: -1, ForWrappedBeforeOrAt: -1}
	for _, m := range mods {
		m(&g)
	}
	return g
}

func active(g *indent.Guide)            { g.Active = true }
func after(col int) func(*indent.Guide) { return func(g *indent.Guide) { g.ForWrappedAfter = col } }
func beforeOrAt(col int) func(*indent.Guide) {
	return func(g *indent.Guide) { g.ForWrappedBeforeOrAt = col }
}
func horizontal(top bool, end int) func(*indent.Guide) {
	return func(g *indent.Guide) { g.Horizontal = &indent.HorizontalLine{Top: top, EndColumn: end} }
}

func TestBracketGuides(t *testing.T) {
	block := lines{"function f() {", "    a();", "}"}
	hanging := lines{"foo({", "    a,", "    b })"}
	nested := lines{"{", "  (", "    {", "      x", "    }", "  )", "}"}
	all := indent.BracketGuideOptions{IncludeInactive: true, HighlightActive: true}
	pos := func(line, col int) *buffer.Position { return &buffer.Position{Line: line, Col: col} }
	tests := []struct {
		name        string
		text        lines
		first, last int
		cursor      *buffer.Position
		opts        indent.BracketGuideOptions
		want        [][]indent.Guide
	}{
		{"a block", block, 0, 2, nil, all, [][]indent.Guide{{guide(0, 1, after(13))}, {guide(0, 1)}, nil}},
		{"only the part on the screen", block, 1, 2, nil, all, [][]indent.Guide{{guide(0, 1)}, nil}},
		{"only the active pair", block, 0, 2, pos(1, 4), indent.BracketGuideOptions{HighlightActive: true}, [][]indent.Guide{{guide(0, 1, after(13), active)}, {guide(0, 1, active)}, nil}},
		{"no active pair with the cursor outside", block, 0, 2, pos(3, 0), indent.BracketGuideOptions{HighlightActive: true}, [][]indent.Guide{nil, nil, nil}},
		{"inactive pairs without highlight", block, 0, 2, pos(1, 4), indent.BracketGuideOptions{IncludeInactive: true}, [][]indent.Guide{{guide(0, 1, after(13))}, {guide(0, 1)}, nil}},
		{"text before the closing bracket", hanging, 0, 2, nil, all, [][]indent.Guide{
			{guide(0, 4, after(3)), guide(1, 5, after(4))},
			{guide(0, 4), guide(1, 5)},
			{guide(0, 4, beforeOrAt(7)), guide(1, 5, beforeOrAt(6))},
		}},
		{"horizontal guides", hanging, 0, 2, nil, indent.BracketGuideOptions{IncludeInactive: true, Horizontal: indent.HorizontalAll}, [][]indent.Guide{
			{guide(0, 4, after(3)), guide(1, 5, after(4))},
			{guide(0, 4), guide(1, 5)},
			{guide(0, 4, beforeOrAt(7)), guide(0, 4, horizontal(false, 7)), guide(1, 5, beforeOrAt(6)), guide(1, 5, horizontal(false, 6))},
		}},
		{"a pair on one line", lines{"f(a)"}, 0, 0, nil, indent.BracketGuideOptions{IncludeInactive: true, Horizontal: indent.HorizontalAll}, [][]indent.Guide{{
			func() indent.Guide { g := guide(0, -1, horizontal(false, 3)); g.Column = 2; return g }(),
		}}},
		{"the guides of the pair around the cursor are the active ones", nested, 0, 6, pos(3, 6), indent.BracketGuideOptions{IncludeInactive: true, Horizontal: indent.HorizontalActive, HighlightActive: true}, [][]indent.Guide{
			{guide(0, 1, after(0))},
			{guide(0, 1), guide(1, 3, after(2))},
			{guide(0, 1), guide(1, 3), guide(2, 5, after(4), active)},
			{guide(0, 1), guide(1, 3), guide(2, 5, active)},
			{guide(0, 1), guide(1, 3)},
			{guide(0, 1)},
			nil,
		}},
		{"independent color pools", lines{"{", " (", "  {", "  }", " )", "}"}, 0, 5, nil, indent.BracketGuideOptions{IncludeInactive: true, IndependentColorPool: true}, [][]indent.Guide{
			{guide(0, 1, after(0))},
			{guide(0, 1), guide(0, 2, after(1))},
			{guide(0, 1), guide(0, 2), guide(1, 3, after(2))},
			{guide(0, 1), guide(0, 2)},
			{guide(0, 1)},
			nil,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := guidesOf(tt.text, 4, 4, false)
			got := g.BracketGuides(pairsOf(tt.text), tt.first, tt.last, tt.cursor, tt.opts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("guides:\n%s\nwant:\n%s", dump(got), dump(tt.want))
			}
		})
	}
}

func dump(lines [][]indent.Guide) string {
	var sb strings.Builder
	for i, gs := range lines {
		fmt.Fprintf(&sb, "  line %d:", i)
		for _, g := range gs {
			fmt.Fprintf(&sb, " {lvl %d vis %d col %d active %v after %d before %d", g.Level, g.VisibleColumn, g.Column, g.Active, g.ForWrappedAfter, g.ForWrappedBeforeOrAt)
			if g.Horizontal != nil {
				fmt.Fprintf(&sb, " horizontal %+v", *g.Horizontal)
			}
			sb.WriteString("}")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestLineGuidesCombineIndentAndBracketGuides(t *testing.T) {
	text := lines{"func f() {", "\tif a {", "\t\tb()", "\t}", "}"}
	g := guidesOf(text, 4, 4, false)
	cursor := &buffer.Position{Line: 2, Col: 2}
	type cell struct {
		visible                       int
		bracket, activ, horizontalBar bool
	}
	cells := func(guides [][]indent.Guide) [][]cell {
		out := make([][]cell, len(guides))
		for i, gs := range guides {
			for _, x := range gs {
				out[i] = append(out[i], cell{x.VisibleColumn, x.Bracket, x.Active, x.Horizontal != nil})
			}
		}
		return out
	}

	// Indent guides only: the one of the scope the cursor is in is active.
	opts := indent.DefaultGuideOptions()
	got := cells(g.LineGuides(pairsOf(text), 0, 4, cursor, opts))
	want := [][]cell{nil, {{1, false, false, false}}, {{1, false, false, false}, {5, false, true, false}}, {{1, false, false, false}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("indent guides: %+v, want %+v", got, want)
	}

	// With bracket pair guides for every pair, the bracket guides take the place of the indent
	// guides at their columns, and the indent guide is not highlighted on a line that has them.
	opts.BracketPairs = indent.PairsAll
	got = cells(g.LineGuides(pairsOf(text), 0, 4, cursor, opts))
	want = [][]cell{
		{{1, true, false, false}},
		{{1, true, false, false}, {5, true, true, false}, {5, true, true, true}}, // and the horizontal line of the active pair
		{{1, true, false, false}, {5, true, true, false}},
		{{1, true, false, false}},
		nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("indent and bracket guides: %+v, want %+v", got, want)
	}

	// With HighlightAlways the indent guide is highlighted on a line that has bracket guides too:
	// here none is left to show, as the bracket guides stand at its columns.
	opts.HighlightActiveIndentation = indent.HighlightAlways
	got = cells(g.LineGuides(pairsOf(text), 0, 4, cursor, opts))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("highlight always: %+v, want %+v", got, want)
	}

	// Bracket guides off, indent guides off: nothing.
	none := indent.GuideOptions{}
	if !none.Off() {
		t.Error("no guides is Off")
	}
	for i, gs := range g.LineGuides(pairsOf(text), 0, 4, cursor, none) {
		if len(gs) != 0 {
			t.Errorf("line %d has guides with the options off: %v", i, gs)
		}
	}

	// highlightActiveIndentation off.
	opts = indent.DefaultGuideOptions()
	opts.HighlightActiveIndentation = indent.HighlightOff
	for i, gs := range g.LineGuides(pairsOf(text), 0, 4, cursor, opts) {
		for _, x := range gs {
			if x.Active {
				t.Errorf("line %d: a guide is highlighted with the highlight off", i)
			}
		}
	}
}

func TestAttachLevelsFollowTheDocument(t *testing.T) {
	rnd := rand.New(rand.NewSource(11))
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		sb.WriteString(strings.Repeat("\t", rnd.Intn(4)) + "x\n")
	}
	doc := document.New(document.UntitledURI(1), sb.String(), document.NewOptions())
	lv, stop := indent.AttachLevels(doc, 4)
	defer stop()
	check := func(round int) {
		want := indent.NewLevels(indent.TextLines(doc), 4)
		count := doc.Snapshot().LineCount()
		for i := 0; i < count; i += 7 {
			if lv.Level(i) != want.Level(i) {
				t.Fatalf("round %d: Level(%d) = %d, want %d", round, i, lv.Level(i), want.Level(i))
			}
		}
		for probe := 0; probe < 5; probe++ {
			a := rnd.Intn(count)
			b := a + rnd.Intn(count-a)
			if got, exp := lv.MinLevel(a, b), want.MinLevel(a, b); got != exp {
				t.Fatalf("round %d: MinLevel(%d, %d) = %d, want %d", round, a, b, got, exp)
			}
		}
	}
	check(0)
	for round := 1; round <= 60; round++ {
		snap := doc.Snapshot()
		count := snap.LineCount()
		// A batch of two or three edits on different lines, in order of position.
		lines := []int{rnd.Intn(count), rnd.Intn(count), rnd.Intn(count)}
		sort.Ints(lines)
		var edits []document.Edit
		for i, l := range lines {
			if i > 0 && l <= lines[i-1] {
				continue
			}
			text := []string{"\n\t\ty\n", "", "z", "\n"}[rnd.Intn(4)]
			end := buffer.Position{Line: l}
			if rnd.Intn(2) == 0 && l+1 < count {
				end = buffer.Position{Line: l + 1}
			}
			edits = append(edits, document.Edit{Range: buffer.Range{Start: buffer.Position{Line: l}, End: end}, Text: text})
			if end.Line > l {
				// The next edit must start after this one's end.
				lines[min(i+1, len(lines)-1)] = max(lines[min(i+1, len(lines)-1)], end.Line+1)
			}
		}
		if _, err := doc.Edit(edits, document.EditOptions{}); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		check(round)
	}
	doc.SetText("a\n\tb")
	if lv.Level(1) != 4 || lv.Level(5) != -1 {
		t.Errorf("after SetText: %d, %d", lv.Level(1), lv.Level(5))
	}
}
