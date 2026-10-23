package emmet

import (
	"reflect"
	"testing"
)

// Ported from Emmet: packages/abbreviation/test/tokenizer.ts

func lit(v string, s, e int) Token { return Token{Type: TokenLiteral, Value: v, Start: s, End: e} }
func op(o OperatorType, s, e int) Token {
	return Token{Type: TokenOperator, Operator: o, Start: s, End: e}
}
func br(ctx BracketType, open bool, s, e int) Token {
	return Token{Type: TokenBracket, Context: ctx, Open: open, Start: s, End: e}
}
func qt(single bool, s, e int) Token {
	return Token{Type: TokenQuote, Single: single, Start: s, End: e}
}
func rep(count int, s, e int) Token {
	return Token{Type: TokenRepeater, Count: count, Start: s, End: e}
}

func TestTokenizer(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Token
	}{
		{"basic: ul>li", "ul>li", []Token{lit("ul", 0, 2), op(OpChild, 2, 3), lit("li", 3, 5)}},
		{"basic: attributes, quotes and text", `ul[title="foo+bar'str'" (attr)=bar]{(some > text)}`, []Token{
			lit("ul", 0, 2), br(BracketAttribute, true, 2, 3), lit("title", 3, 8), op(OpEqual, 8, 9),
			qt(false, 9, 10), lit("foo+bar'str'", 10, 22), qt(false, 22, 23),
			{Type: TokenWhiteSpace, Start: 23, End: 24, Value: " "},
			br(BracketGroup, true, 24, 25), lit("attr", 25, 29), br(BracketGroup, false, 29, 30),
			op(OpEqual, 30, 31), lit("bar", 31, 34), br(BracketAttribute, false, 34, 35),
			br(BracketExpression, true, 35, 36), lit("(some > text)", 36, 49), br(BracketExpression, false, 49, 50),
		}},
		{"basic: numbering and field", "h${some${1:field placeholder}}", []Token{
			lit("h", 0, 1),
			{Type: TokenRepeaterNumber, Size: 1, Base: 1, Start: 1, End: 2},
			br(BracketExpression, true, 2, 3), lit("some", 3, 7),
			{Type: TokenField, Index: 1, HasIndex: true, Name: "field placeholder", Start: 7, End: 29},
			br(BracketExpression, false, 29, 30),
		}},
		{"basic: brackets in text", "div{[}+a{}", []Token{
			lit("div", 0, 3), br(BracketExpression, true, 3, 4), lit("[", 4, 5), br(BracketExpression, false, 5, 6),
			op(OpSibling, 6, 7), lit("a", 7, 8), br(BracketExpression, true, 8, 9), br(BracketExpression, false, 9, 10),
		}},
		{"repeater: id", "#sample*3", []Token{op(OpID, 0, 1), lit("sample", 1, 7), rep(3, 7, 9)}},
		{"repeater: not in attributes", "div[foo*3]", []Token{
			lit("div", 0, 3), br(BracketAttribute, true, 3, 4), lit("foo*3", 4, 9), br(BracketAttribute, false, 9, 10),
		}},
		{"repeater: not in text", "({a*2})*3", []Token{
			br(BracketGroup, true, 0, 1), br(BracketExpression, true, 1, 2), lit("a*2", 2, 5),
			br(BracketExpression, false, 5, 6), br(BracketGroup, false, 6, 7), rep(3, 7, 9),
		}},
		// not in Emmet's tests: the other tokens
		{"implicit repeater", "li*", []Token{lit("li", 0, 2), {Type: TokenRepeater, Count: 1, Implicit: true, Start: 2, End: 3}}},
		{"numbering modifiers", "a$$@-3", []Token{lit("a", 0, 1), {Type: TokenRepeaterNumber, Size: 2, Reverse: true, Base: 3, Start: 1, End: 6}}},
		{"numbering of the parent", "a$@^^5", []Token{lit("a", 0, 1), {Type: TokenRepeaterNumber, Size: 1, Parent: 2, Base: 5, Start: 1, End: 6}}},
		{"repeater placeholder", "li{$#}", []Token{
			lit("li", 0, 2), br(BracketExpression, true, 2, 3), {Type: TokenRepeaterPlaceholder, Start: 3, End: 5}, br(BracketExpression, false, 5, 6),
		}},
		{"variable", "[a=${lang}]", []Token{
			br(BracketAttribute, true, 0, 1), lit("a", 1, 2), op(OpEqual, 2, 3),
			{Type: TokenField, Name: "lang", Start: 3, End: 10}, br(BracketAttribute, false, 10, 11),
		}},
		{"slash between numbers", "a.w1/2", []Token{lit("a", 0, 1), op(OpClass, 1, 2), lit("w1/2", 2, 6)}},
		{"escaped", `a\+b`, []Token{lit("a+b", 0, 4)}},
		{"umlaut in a name", "äb", []Token{lit("äb", 0, 3)}},
		{"no-break space is white space", "a b", []Token{lit("a", 0, 1), {Type: TokenWhiteSpace, Value: " ", Start: 1, End: 3}, lit("b", 3, 4)}},
		{"a character with the same last byte as the no-break space is text", "[a=à]", []Token{
			br(BracketAttribute, true, 0, 1), lit("a", 1, 2), op(OpEqual, 2, 3), lit("à", 3, 5), br(BracketAttribute, false, 5, 6),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Tokenize(tc.in)
			if err != nil {
				t.Fatalf("Tokenize(%q): %v", tc.in, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Tokenize(%q)\n got %+v\nwant %+v", tc.in, got, tc.want)
			}
		})
	}
}
