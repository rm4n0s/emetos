package jsonc

import (
	"reflect"
	"testing"
)

func assertKinds(t *testing.T, text string, kinds ...SyntaxKind) {
	t.Helper()
	sc := NewScanner(text, false)
	var got []SyntaxKind
	for k := sc.Scan(); k != EOF; k = sc.Scan() {
		got = append(got, k)
	}
	if !reflect.DeepEqual(got, append([]SyntaxKind(nil), kinds...)) && (len(got) != 0 || len(kinds) != 0) {
		t.Errorf("%q: kinds %v, want %v", text, got, kinds)
	}
}

func assertScanError(t *testing.T, text string, kind SyntaxKind, want ScanError) {
	t.Helper()
	sc := NewScanner(text, false)
	sc.Scan()
	if sc.Token() != kind || sc.Error() != want {
		t.Errorf("%q: token %v error %v, want %v %v", text, sc.Token(), sc.Error(), kind, want)
	}
}

// Ported from VS Code: src/vs/base/test/common/json.test.ts ('tokens', 'comments', 'strings',
// 'numbers', 'keywords: true, false, null', 'trivia').
func TestVSCodeScanner(t *testing.T) {
	t.Run("tokens", func(t *testing.T) {
		assertKinds(t, "{", OpenBraceToken)
		assertKinds(t, "}", CloseBraceToken)
		assertKinds(t, "[", OpenBracketToken)
		assertKinds(t, "]", CloseBracketToken)
		assertKinds(t, ":", ColonToken)
		assertKinds(t, ",", CommaToken)
	})
	t.Run("comments", func(t *testing.T) {
		assertKinds(t, "// this is a comment", LineCommentTrivia)
		assertKinds(t, "// this is a comment\n", LineCommentTrivia, LineBreakTrivia)
		assertKinds(t, "/* this is a comment*/", BlockCommentTrivia)
		assertKinds(t, "/* this is a \r\ncomment*/", BlockCommentTrivia)
		assertKinds(t, "/* this is a \ncomment*/", BlockCommentTrivia)
		// unexpected end
		assertKinds(t, "/* this is a", BlockCommentTrivia)
		assertKinds(t, "/* this is a \ncomment", BlockCommentTrivia)
		// broken comment
		assertKinds(t, "/ ttt", Unknown, Trivia, Unknown)
	})
	t.Run("strings", func(t *testing.T) {
		for _, s := range []string{`"test"`, `"\""`, `"\/"`, `"\b"`, `"\f"`, `"\n"`, `"\r"`, `"\t"`, `"\v"`, "\"\u88ff\"", "\"\u200b\u2028\""} {
			assertKinds(t, s, StringLiteral)
		}
		// unexpected end
		assertKinds(t, `"test`, StringLiteral)
		assertKinds(t, "\"test\n\"", StringLiteral, LineBreakTrivia, StringLiteral)
		// invalid characters
		assertScanError(t, "\"\t\"", StringLiteral, ScanInvalidCharacter)
		assertScanError(t, "\"\t \"", StringLiteral, ScanInvalidCharacter)
	})
	t.Run("numbers", func(t *testing.T) {
		for _, s := range []string{"0", "0.1", "-0.1", "-1", "1", "123456789", "10", "90", "90E+123", "90e+123", "90e-123", "90E-123", "90E123", "90e123"} {
			assertKinds(t, s, NumericLiteral)
		}
		// zero handling
		assertKinds(t, "01", NumericLiteral, NumericLiteral)
		assertKinds(t, "-01", NumericLiteral, NumericLiteral)
		// unexpected end
		assertKinds(t, "-", Unknown)
		assertKinds(t, ".0", Unknown)
	})
	t.Run("keywords: true, false, null", func(t *testing.T) {
		assertKinds(t, "true", TrueKeyword)
		assertKinds(t, "false", FalseKeyword)
		assertKinds(t, "null", NullKeyword)
		assertKinds(t, "true false null", TrueKeyword, Trivia, FalseKeyword, Trivia, NullKeyword)
		// invalid words
		assertKinds(t, "nulllll", Unknown)
		assertKinds(t, "True", Unknown)
		assertKinds(t, "foo-bar", Unknown)
		assertKinds(t, "foo bar", Unknown, Trivia, Unknown)
	})
	t.Run("trivia", func(t *testing.T) {
		assertKinds(t, " ", Trivia)
		assertKinds(t, "  \t  ", Trivia)
		assertKinds(t, "  \t  \n  \t  ", Trivia, LineBreakTrivia, Trivia)
		assertKinds(t, "\r\n", LineBreakTrivia)
		assertKinds(t, "\r", LineBreakTrivia)
		assertKinds(t, "\n", LineBreakTrivia)
		assertKinds(t, "\n\r", LineBreakTrivia, LineBreakTrivia)
		assertKinds(t, "\n   \n", LineBreakTrivia, Trivia, LineBreakTrivia)
	})
}

func assertValidParse(t *testing.T, input string, want any, opts ...ParseOptions) {
	t.Helper()
	var o ParseOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	got, errs := Parse(input, o)
	if len(errs) != 0 {
		t.Errorf("%q: error %q at %d", input, errs[0].Message(), errs[0].Offset)
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q: got %#v, want %#v", input, got, want)
	}
}

func assertInvalidParse(t *testing.T, input string, want any, opts ...ParseOptions) {
	t.Helper()
	var o ParseOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	got, errs := Parse(input, o)
	if len(errs) == 0 {
		t.Errorf("%q: no error", input)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q: got %#v, want %#v", input, got, want)
	}
}

type obj = map[string]any
type arr = []any

// Ported from VS Code: json.test.ts ('parse: literals', 'parse: objects', 'parse: arrays',
// 'parse: objects with errors', 'parse: array with errors', 'parse: disallow commments',
// 'parse: trailing comma'; the __proto__ test holds for maps).
func TestVSCodeParse(t *testing.T) {
	t.Run("literals", func(t *testing.T) {
		assertValidParse(t, "true", true)
		assertValidParse(t, "false", false)
		assertValidParse(t, "null", nil)
		assertValidParse(t, `"foo"`, "foo")
		assertValidParse(t, `"\"-\\-\/-\b-\f-\n-\r-\t"`, "\"-\\-/-\b-\f-\n-\r-\t")
		assertValidParse(t, `"\u00DC"`, "Ü")
		assertValidParse(t, "9", 9.0)
		assertValidParse(t, "-9", -9.0)
		assertValidParse(t, "0.129", 0.129)
		assertValidParse(t, "23e3", 23e3)
		assertValidParse(t, "1.2E+3", 1.2e+3)
		assertValidParse(t, "1.2E-3", 1.2e-3)
		assertValidParse(t, "1.2E-3 // comment", 1.2e-3)
	})
	t.Run("objects", func(t *testing.T) {
		assertValidParse(t, "{}", obj{})
		assertValidParse(t, `{ "foo": true }`, obj{"foo": true})
		assertValidParse(t, `{ "bar": 8, "xoo": "foo" }`, obj{"bar": 8.0, "xoo": "foo"})
		assertValidParse(t, `{ "hello": [], "world": {} }`, obj{"hello": arr{}, "world": obj{}})
		assertValidParse(t, `{ "a": false, "b": true, "c": [ 7.4 ] }`, obj{"a": false, "b": true, "c": arr{7.4}})
		assertValidParse(t, `{ "lineComment": "//", "blockComment": ["/*", "*/"], "brackets": [ ["{", "}"], ["[", "]"], ["(", ")"] ] }`,
			obj{"lineComment": "//", "blockComment": arr{"/*", "*/"}, "brackets": arr{arr{"{", "}"}, arr{"[", "]"}, arr{"(", ")"}}})
		assertValidParse(t, `{ "hello": { "again": { "inside": 5 }, "world": 1 }}`, obj{"hello": obj{"again": obj{"inside": 5.0}, "world": 1.0}})
		assertValidParse(t, `{ "foo": /*hello*/true }`, obj{"foo": true})
	})
	t.Run("__proto__ is an ordinary property", func(t *testing.T) {
		assertValidParse(t, `{ "__proto__": { "polluted": true } }`, obj{"__proto__": obj{"polluted": true}})
	})
	t.Run("arrays", func(t *testing.T) {
		assertValidParse(t, "[]", arr{})
		assertValidParse(t, "[ [],  [ [] ]]", arr{arr{}, arr{arr{}}})
		assertValidParse(t, "[ 1, 2, 3 ]", arr{1.0, 2.0, 3.0})
		assertValidParse(t, `[ { "a": null } ]`, arr{obj{"a": nil}})
	})
	t.Run("objects with errors", func(t *testing.T) {
		assertInvalidParse(t, "{,}", obj{})
		assertInvalidParse(t, `{ "foo": true, }`, obj{"foo": true}, ParseOptions{DisallowTrailingComma: true})
		assertInvalidParse(t, `{ "bar": 8 "xoo": "foo" }`, obj{"bar": 8.0, "xoo": "foo"})
		assertInvalidParse(t, `{ ,"bar": 8 }`, obj{"bar": 8.0})
		assertInvalidParse(t, `{ ,"bar": 8, "foo" }`, obj{"bar": 8.0})
		assertInvalidParse(t, `{ "bar": 8, "foo": }`, obj{"bar": 8.0})
		assertInvalidParse(t, `{ 8, "foo": 9 }`, obj{"foo": 9.0})
	})
	t.Run("array with errors", func(t *testing.T) {
		assertInvalidParse(t, "[,]", arr{})
		assertInvalidParse(t, "[ 1, 2, ]", arr{1.0, 2.0}, ParseOptions{DisallowTrailingComma: true})
		assertInvalidParse(t, "[ 1 2, 3 ]", arr{1.0, 2.0, 3.0})
		assertInvalidParse(t, "[ ,1, 2, 3 ]", arr{1.0, 2.0, 3.0})
		assertInvalidParse(t, "[ ,1, 2, 3, ]", arr{1.0, 2.0, 3.0}, ParseOptions{DisallowTrailingComma: true})
	})
	t.Run("disallow comments", func(t *testing.T) {
		o := ParseOptions{DisallowComments: true}
		assertValidParse(t, `[ 1, 2, null, "foo" ]`, arr{1.0, 2.0, nil, "foo"}, o)
		assertValidParse(t, `{ "hello": [], "world": {} }`, obj{"hello": arr{}, "world": obj{}}, o)
		assertInvalidParse(t, `{ "foo": /*comment*/ true }`, obj{"foo": true}, o)
	})
	t.Run("trailing comma", func(t *testing.T) {
		// the default is to allow it
		assertValidParse(t, `{ "hello": [], }`, obj{"hello": arr{}})
		assertValidParse(t, `{ "hello": [], }`, obj{"hello": arr{}}, ParseOptions{})
		assertValidParse(t, `{ "hello": [] }`, obj{"hello": arr{}})
		assertValidParse(t, `{ "hello": [], "world": {}, }`, obj{"hello": arr{}, "world": obj{}})
		assertValidParse(t, `{ "hello": [1,] }`, obj{"hello": arr{1.0}})
		o := ParseOptions{DisallowTrailingComma: true}
		assertInvalidParse(t, `{ "hello": [], }`, obj{"hello": arr{}}, o)
		assertInvalidParse(t, `{ "hello": [], "world": {}, }`, obj{"hello": arr{}, "world": obj{}}, o)
	})
}

// shape is a node without its parent, to compare trees.
type shape struct {
	Type     NodeType
	Offset   int
	Length   int
	Colon    int
	Value    any
	Children []shape
}

func shapeOf(t *testing.T, n *Node) shape {
	t.Helper()
	s := shape{Type: n.Type, Offset: n.Offset, Length: n.Length, Colon: n.ColonOffset, Value: n.Value}
	for _, c := range n.Children {
		if c.Parent != n {
			t.Errorf("the parent of the node at %d is not the node at %d", c.Offset, n.Offset)
		}
		s.Children = append(s.Children, shapeOf(t, c))
	}
	return s
}

func lit(typ NodeType, offset, length int, value any) shape {
	return shape{Type: typ, Offset: offset, Length: length, Colon: -1, Value: value}
}

func container(typ NodeType, offset, length int, children ...shape) shape {
	return shape{Type: typ, Offset: offset, Length: length, Colon: -1, Children: children}
}

func prop(offset, length, colon int, name, value shape) shape {
	return shape{Type: PropertyNode, Offset: offset, Length: length, Colon: colon, Children: []shape{name, value}}
}

func assertTree(t *testing.T, input string, want shape, wantErrors []ParseErrorCode, opts ...ParseOptions) {
	t.Helper()
	var o ParseOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	root, errs := ParseTree(input, o)
	var codes []ParseErrorCode
	for _, e := range errs {
		codes = append(codes, e.Code)
	}
	if !reflect.DeepEqual(codes, wantErrors) && (len(codes) != 0 || len(wantErrors) != 0) {
		t.Errorf("%q: errors %v, want %v", input, codes, wantErrors)
	}
	if root == nil {
		t.Fatalf("%q: no tree", input)
	}
	if root.Parent != nil {
		t.Errorf("%q: the root has a parent", input)
	}
	if got := shapeOf(t, root); !reflect.DeepEqual(got, want) {
		t.Errorf("%q:\n got %+v\nwant %+v", input, got, want)
	}
}

// Ported from VS Code: json.test.ts ('tree: literals', 'tree: arrays', 'tree: objects').
func TestVSCodeTree(t *testing.T) {
	t.Run("literals", func(t *testing.T) {
		assertTree(t, "true", lit(BooleanNode, 0, 4, true), nil)
		assertTree(t, "false", lit(BooleanNode, 0, 5, false), nil)
		assertTree(t, "null", lit(NullNode, 0, 4, nil), nil)
		assertTree(t, "23", lit(NumberNode, 0, 2, 23.0), nil)
		assertTree(t, "-1.93e-19", lit(NumberNode, 0, 9, -1.93e-19), nil)
		assertTree(t, `"hello"`, lit(StringNode, 0, 7, "hello"), nil)
	})
	t.Run("arrays", func(t *testing.T) {
		assertTree(t, "[]", container(ArrayNode, 0, 2), nil)
		assertTree(t, "[ 1 ]", container(ArrayNode, 0, 5, lit(NumberNode, 2, 1, 1.0)), nil)
		assertTree(t, `[ 1,"x"]`, container(ArrayNode, 0, 8, lit(NumberNode, 2, 1, 1.0), lit(StringNode, 4, 3, "x")), nil)
		assertTree(t, "[[]]", container(ArrayNode, 0, 4, container(ArrayNode, 1, 2)), nil)
	})
	t.Run("objects", func(t *testing.T) {
		assertTree(t, "{ }", container(ObjectNode, 0, 3), nil)
		assertTree(t, `{ "val": 1 }`, container(ObjectNode, 0, 12,
			prop(2, 8, 7, lit(StringNode, 2, 5, "val"), lit(NumberNode, 9, 1, 1.0))), nil)
		assertTree(t, `{"id": "$", "v": [ null, null] }`, container(ObjectNode, 0, 32,
			prop(1, 9, 5, lit(StringNode, 1, 4, "id"), lit(StringNode, 7, 3, "$")),
			prop(12, 18, 15, lit(StringNode, 12, 3, "v"), container(ArrayNode, 17, 13,
				lit(NullNode, 19, 4, nil), lit(NullNode, 25, 4, nil)))), nil)
		assertTree(t, `{  "id": { "foo": { } } , }`, container(ObjectNode, 0, 27,
			prop(3, 20, 7, lit(StringNode, 3, 4, "id"), container(ObjectNode, 9, 14,
				prop(11, 10, 16, lit(StringNode, 11, 5, "foo"), container(ObjectNode, 18, 3))))),
			[]ParseErrorCode{PropertyNameExpected, ValueExpected}, ParseOptions{DisallowTrailingComma: true})
	})
}
