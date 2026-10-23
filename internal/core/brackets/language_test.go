package brackets

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/language"
)

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/model/bracketPairColorizer/brackets.test.ts (MIT)

func TestLanguageBracketsOfVerilogAndLaTeX(t *testing.T) {
	l := NewLanguage(1, pairs(
		"{", "}", "[", "]", "(", ")",
		"begin", "end", "case", "endcase", "casez", "endcase", // Verilog
		`\left(`, `\right)`, `\left(`, `\right.`, `\left.`, `\right)`, // LaTeX Parentheses
		`\left[`, `\right]`, `\left[`, `\right.`, `\left.`, `\right]`, // LaTeX Brackets
	), nil, false)
	tests := []struct {
		text    string
		opening bool
		closes  []string // for a closing bracket: the openings it closes
	}{
		{"{", true, nil}, {"[", true, nil}, {"(", true, nil}, {"begin", true, nil}, {"case", true, nil},
		{"casez", true, nil}, {`\left(`, true, nil}, {`\left.`, true, nil}, {`\left[`, true, nil},
		{"}", false, []string{"{"}}, {"]", false, []string{"["}}, {")", false, []string{"("}},
		{"end", false, []string{"begin"}},
		{"endcase", false, []string{"case", "casez"}},
		{`\right)`, false, []string{`\left(`, `\left.`}},
		{`\right.`, false, []string{`\left(`, `\left[`}},
		{`\right]`, false, []string{`\left[`, `\left.`}},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			b := l.Lookup(tc.text)
			if b == nil {
				t.Fatalf("%q is not a bracket", tc.text)
			}
			if b.Opening != tc.opening || b.Text != tc.text || b.Language() != l {
				t.Errorf("bracket = %+v", b)
			}
			var closes []string
			for _, o := range b.Openings() {
				closes = append(closes, o.Text)
			}
			if !reflect.DeepEqual(closes, tc.closes) {
				t.Errorf("closes %v, want %v", closes, tc.closes)
			}
			for _, o := range b.Openings() {
				if !b.Closes(o) {
					t.Errorf("Closes(%q) = false", o.Text)
				}
			}
		})
	}
	if got := len(l.Brackets()); got != 17 {
		t.Errorf("%d brackets, want 17", got)
	}
	if l.Lookup("nope") != nil || (*Language)(nil).Lookup("{") != nil {
		t.Error("an unknown text is not a bracket")
	}
	if !(*Language)(nil).Empty() || l.Empty() || !NewLanguage(2, nil, nil, false).Empty() {
		t.Error("Empty")
	}
}

func TestBracketsAreFoundWhereTheyAreWordsOrSymbols(t *testing.T) {
	l := NewLanguage(1, pairs("{", "}", "begin", "end", "case", "endcase", "if", "end if"), nil, false)
	tests := []struct {
		text string
		want []string // the brackets found, with their offsets: "text@offset"
	}{
		{"{ }", []string{"{@0", "}@2"}},
		{"begin end", []string{"begin@0", "end@6"}},
		{"BEGIN End", []string{"begin@0", "end@6"}},
		{"friend", nil},
		{"ending", nil},
		{"beginning", nil},
		{"x{y}", []string{"{@1", "}@3"}},
		{"endcase", []string{"endcase@0"}},
		{"end if", []string{"end if@0"}},
		{"if end if", []string{"if@0", "end if@3"}},
		{"elif", nil},
		{"a_end", nil},
		{"end_", nil},
		{"end.", []string{"end@0"}},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			var got []string
			rest, base := tc.text, 0
			for {
				at, b := l.find(rest)
				if b == nil {
					break
				}
				got = append(got, b.Text+"@"+itoa(base+at))
				rest, base = rest[at+len(b.Text):], base+at+len(b.Text)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("found %v, want %v", got, tc.want)
			}
		})
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestColorizedBracketPairsOfALanguage(t *testing.T) {
	angle := pairs("{", "}", "<", ">")
	tests := []struct {
		name                       string
		brackets, colorized        []Pair
		defined                    bool
		colorizedBrace, colorizedA bool // whether `}` and `>` close a colorized pair
		hasAngle                   bool
	}{
		{"default: all but <>", angle, nil, false, true, false, true},
		{"defined: only those", angle, pairs("<", ">"), true, false, true, true},
		{"defined empty: none", angle, []Pair{}, true, false, false, true},
		{"colorized pairs are brackets too", nil, pairs("(", ")"), true, false, false, false},
		{"empty texts are not brackets", pairs("", "}", "{", ""), nil, false, false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLanguage(1, tc.brackets, tc.colorized, tc.defined)
			brace, openBrace := l.Lookup("}"), l.Lookup("{")
			if brace != nil && openBrace != nil && brace.ClosesColorized(openBrace) != tc.colorizedBrace {
				t.Errorf("} closes { colorized = %v", !tc.colorizedBrace)
			}
			angleClose, angleOpen := l.Lookup(">"), l.Lookup("<")
			if (angleClose != nil) != tc.hasAngle {
				t.Fatalf("> is a bracket = %v", angleClose != nil)
			}
			if tc.hasAngle && angleClose.ClosesColorized(angleOpen) != tc.colorizedA {
				t.Errorf("> closes < colorized = %v", !tc.colorizedA)
			}
		})
	}
	if l := NewLanguage(1, nil, pairs("(", ")"), true); l.Lookup("(") == nil || !l.Lookup("(").Opening || !l.Lookup(")").ClosesColorized(l.Lookup("(")) {
		t.Error("a colorized pair is a bracket pair")
	}
}

func TestATextThatIsBothOpeningAndClosingIsClosing(t *testing.T) {
	l := NewLanguage(1, pairs("|", "|"), nil, false)
	if b := l.Lookup("|"); b == nil || b.Opening {
		t.Errorf("| = %+v", b)
	}
}

func TestFromConfiguration(t *testing.T) {
	if !FromConfiguration(3, nil).Empty() {
		t.Error("no configuration, no brackets")
	}
	c := &language.Configuration{Brackets: pairs("(", ")", "<", ">")}
	l := FromConfiguration(3, c)
	if l.Code() != 3 || l.Lookup("(") == nil || l.Lookup(">").ClosesColorized(l.Lookup("<")) {
		t.Error("the default colorized pairs leave out <>")
	}
	c.ColorizedBracketPairs = pairs("<", ">")
	if l := FromConfiguration(3, c); !l.Lookup(">").ClosesColorized(l.Lookup("<")) || l.Lookup(")").ClosesColorized(l.Lookup("(")) {
		t.Error("a defined list is the colorized pairs")
	}
}
