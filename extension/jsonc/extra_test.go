package jsonc

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// bom is U+FEFF, written out because source files cannot hold it.
var bom = string(rune(0xFEFF))

func TestScannerValuesAndOffsets(t *testing.T) {
	sc := NewScanner("  // c\n\t\"a\\u0041\\ud83d\\ude00\" -12.5e3 null", true)
	type tok struct {
		kind   SyntaxKind
		value  string
		offset int
		length int
	}
	var got []tok
	for k := sc.Scan(); k != EOF; k = sc.Scan() {
		got = append(got, tok{k, sc.Value(), sc.Offset(), sc.Length()})
	}
	want := []tok{
		{StringLiteral, "aA\U0001F600", 8, 21},
		{NumericLiteral, "-12.5e3", 30, 7},
		{NullKeyword, "null", 38, 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokens %+v, want %+v", got, want)
	}
	if sc.Offset() != len(sc.text) || sc.Length() != 0 {
		t.Errorf("EOF at %d length %d", sc.Offset(), sc.Length())
	}
}

func TestScannerEdgeCases(t *testing.T) {
	t.Run("a lone surrogate escape becomes the replacement character", func(t *testing.T) {
		for _, s := range []string{`"\ud83d"`, `"\ude00"`, `"\ud83dA"`} {
			sc := NewScanner(s, false)
			sc.Scan()
			if sc.Error() != ScanNone || !strings.ContainsRune(sc.Value(), utf8.RuneError) || !utf8.ValidString(sc.Value()) {
				t.Errorf("%s: value %q error %v", s, sc.Value(), sc.Error())
			}
		}
	})
	t.Run("an invalid escape drops a whole character", func(t *testing.T) {
		sc := NewScanner(`"a\éb"`, false)
		sc.Scan()
		if sc.Value() != "ab" || sc.Length() != len(`"a\éb"`) {
			t.Errorf("value %q length %d", sc.Value(), sc.Length())
		}
	})
	t.Run("errors in strings", func(t *testing.T) {
		assertScanError(t, `"\x"`, StringLiteral, ScanInvalidEscapeCharacter)
		assertScanError(t, `"\u12"`, StringLiteral, ScanInvalidUnicode)
		assertScanError(t, `"\é"`, StringLiteral, ScanInvalidEscapeCharacter)
		assertScanError(t, `"abc`, StringLiteral, ScanUnexpectedEndOfString)
		assertScanError(t, "\"abc\n\"", StringLiteral, ScanUnexpectedEndOfString)
		assertScanError(t, `"abc\`, StringLiteral, ScanUnexpectedEndOfString)
		assertScanError(t, `1.`, NumericLiteral, ScanUnexpectedEndOfNumber)
		assertScanError(t, `1e+`, NumericLiteral, ScanUnexpectedEndOfNumber)
		assertScanError(t, `/*`, BlockCommentTrivia, ScanUnexpectedEndOfComment)
	})
	t.Run("an unclosed comment ends with the text", func(t *testing.T) {
		for _, s := range []string{"/*", "/**", "/* a", "/* a *"} {
			sc := NewScanner(s, false)
			sc.Scan()
			if sc.Offset() != 0 || sc.Length() != len(s) {
				t.Errorf("%q: offset %d length %d", s, sc.Offset(), sc.Length())
			}
		}
	})
	t.Run("unicode white space and line breaks", func(t *testing.T) {
		assertKinds(t, "\u00a0\u2003"+bom+"\u3000", Trivia)
		assertKinds(t, " ", LineBreakTrivia)
		assertKinds(t, "  ", LineBreakTrivia, LineBreakTrivia)
		assertKinds(t, "// a b", LineCommentTrivia, LineBreakTrivia, Unknown)
	})
	t.Run("words of several bytes are one unknown token", func(t *testing.T) {
		sc := NewScanner("héllo wörld", false)
		sc.Scan()
		if sc.Token() != Unknown || sc.Value() != "héllo" || sc.Length() != len("héllo") {
			t.Errorf("token %v %q", sc.Token(), sc.Value())
		}
	})
	t.Run("invalid UTF-8 does not stop the scanner", func(t *testing.T) {
		assertKinds(t, "\xff\xfe", Unknown)
		assertKinds(t, "\"\xff\"", StringLiteral)
	})
	t.Run("SetPosition", func(t *testing.T) {
		sc := NewScanner("[1, 2]", true)
		sc.SetPosition(4)
		if k := sc.Scan(); k != NumericLiteral || sc.Value() != "2" {
			t.Errorf("scanned %v %q", k, sc.Value())
		}
		sc.SetPosition(100)
		if sc.Scan() != EOF {
			t.Error("a position past the end is not the end")
		}
		sc.SetPosition(-5)
		if sc.Scan() != OpenBracketToken {
			t.Error("a negative position is not the start")
		}
	})
	t.Run("kind names", func(t *testing.T) {
		if EOF.String() != "EOF" || SyntaxKind(99).String() != "SyntaxKind(99)" {
			t.Error("names")
		}
	})
}

func TestParseErrorPlaces(t *testing.T) {
	text := `{ "a": 1 "b": [1 2], "c" }`
	_, errs := ParseTree(text, ParseOptions{})
	want := []ParseError{
		{CommaExpected, 9, 3},
		{CommaExpected, 17, 1},
		{ColonExpected, 25, 1},
	}
	if !reflect.DeepEqual(errs, want) {
		t.Errorf("errors %+v, want %+v", errs, want)
	}
	for _, e := range errs {
		if e.Message() == "" {
			t.Errorf("no message for %d", e.Code)
		}
	}
	if InvalidCharacter.Message() != "Invalid character" || ParseErrorCode(0).Message() != "ParseErrorCode(0)" {
		t.Error("messages")
	}
}

func TestParseErrors(t *testing.T) {
	codes := func(text string, opts ParseOptions) []ParseErrorCode {
		_, errs := ParseTree(text, opts)
		var out []ParseErrorCode
		for _, e := range errs {
			out = append(out, e.Code)
		}
		return out
	}
	cases := []struct {
		text string
		opts ParseOptions
		want []ParseErrorCode
	}{
		{"", ParseOptions{}, []ParseErrorCode{ValueExpected}},
		{"  // only a comment", ParseOptions{}, []ParseErrorCode{ValueExpected}},
		{"", ParseOptions{AllowEmptyContent: true}, nil},
		{"1 2", ParseOptions{}, []ParseErrorCode{EndOfFileExpected}},
		{"{", ParseOptions{}, []ParseErrorCode{CloseBraceExpected}},
		{"[", ParseOptions{}, []ParseErrorCode{CloseBracketExpected}},
		{`{"a"`, ParseOptions{}, []ParseErrorCode{ColonExpected, CloseBraceExpected}},
		{`{"a":`, ParseOptions{}, []ParseErrorCode{ValueExpected, CloseBraceExpected}},
		{`nope`, ParseOptions{}, []ParseErrorCode{InvalidSymbol, ValueExpected}},
		{`1.`, ParseOptions{}, []ParseErrorCode{UnexpectedEndOfNumber, InvalidNumberFormat}},
		{`"\q"`, ParseOptions{}, []ParseErrorCode{InvalidEscapeCharacter}},
		{`"\u00"`, ParseOptions{}, []ParseErrorCode{InvalidUnicode}},
		{"\"\t\"", ParseOptions{}, []ParseErrorCode{InvalidCharacter}},
		{`"abc`, ParseOptions{}, []ParseErrorCode{UnexpectedEndOfString}},
		{`[1 /* x`, ParseOptions{}, []ParseErrorCode{UnexpectedEndOfComment, CloseBracketExpected}},
		{`[1 // x`, ParseOptions{DisallowComments: true}, []ParseErrorCode{InvalidCommentToken, CloseBracketExpected}},
		{`// c` + "\n" + `{}`, ParseOptions{}, nil},
		{`{"a": 1, "b": [true, false,],}`, ParseOptions{}, nil},
	}
	for _, c := range cases {
		if got := codes(c.text, c.opts); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: errors %v, want %v", c.text, got, c.want)
		}
	}
}

func TestParseTreeOfBrokenText(t *testing.T) {
	t.Run("a property with no value ends where its object does", func(t *testing.T) {
		text := `{"a": 1, "b":`
		root, _ := ParseTree(text, ParseOptions{})
		if root.Offset != 0 || root.Length != len(text) {
			t.Errorf("object at %d length %d", root.Offset, root.Length)
		}
		b := root.Children[1]
		if len(b.Children) != 1 || b.Offset != 9 || b.End() != len(text) || b.ColonOffset != 12 {
			t.Errorf("property %+v", b)
		}
		if v, _ := Parse(text, ParseOptions{}); !reflect.DeepEqual(v, obj{"a": 1.0}) {
			t.Errorf("value %#v", v)
		}
	})
	t.Run("an unclosed array ends with the text", func(t *testing.T) {
		root, _ := ParseTree(`[1, [2, {"a": [`, ParseOptions{})
		if root.End() != 15 {
			t.Errorf("array ends at %d", root.End())
		}
	})
	t.Run("no value gives no tree", func(t *testing.T) {
		if root, errs := ParseTree("// nothing", ParseOptions{AllowEmptyContent: true}); root != nil || len(errs) != 0 {
			t.Errorf("tree %v errors %v", root, errs)
		}
		if v, _ := Parse("", ParseOptions{}); v != nil {
			t.Errorf("value %v", v)
		}
	})
	t.Run("a repeated name keeps the last value", func(t *testing.T) {
		if v, _ := Parse(`{"a": 1, "a": 2}`, ParseOptions{}); !reflect.DeepEqual(v, obj{"a": 2.0}) {
			t.Errorf("value %#v", v)
		}
	})
}

func TestVisitCallbacks(t *testing.T) {
	var events []string
	Visit(`// c
{"a": [1, "x"], "b": null /* d */}`, &Visitor{
		ObjectBegin:    func(o, l int) { events = append(events, "{"+itoa(o)) },
		ObjectEnd:      func(o, l int) { events = append(events, "}"+itoa(o)) },
		ObjectProperty: func(n string, o, l int) { events = append(events, "p:"+n+"@"+itoa(o)) },
		ArrayBegin:     func(o, l int) { events = append(events, "["+itoa(o)) },
		ArrayEnd:       func(o, l int) { events = append(events, "]"+itoa(o)) },
		LiteralValue:   func(v any, o, l int) { events = append(events, "v@"+itoa(o)+"+"+itoa(l)) },
		Separator:      func(c string, o, l int) { events = append(events, c+itoa(o)) },
		Comment:        func(o, l int) { events = append(events, "c@"+itoa(o)+"+"+itoa(l)) },
	}, ParseOptions{})
	want := "c@0+4 {5 p:a@6 :9 [11 v@12+1 ,13 v@15+3 ]18 ,19 p:b@21 :24 v@26+4 c@31+7 }38"
	if got := strings.Join(events, " "); got != want {
		t.Errorf("events\n got %s\nwant %s", got, want)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestStopVisit(t *testing.T) {
	var seen int
	Visit(`[1, 2, 3, 4]`, &Visitor{LiteralValue: func(v any, o, l int) {
		seen++
		if seen == 2 {
			StopVisit()
		}
	}}, ParseOptions{})
	if seen != 2 {
		t.Errorf("saw %d values", seen)
	}
	defer func() {
		if recover() != "boom" {
			t.Error("another panic was swallowed")
		}
	}()
	Visit(`[1]`, &Visitor{LiteralValue: func(any, int, int) { panic("boom") }}, ParseOptions{})
}

func TestPathQueries(t *testing.T) {
	text := `{ "a": { "b": [10, {"c": true}] }, "d": "x" }`
	root, errs := ParseTree(text, ParseOptions{})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	t.Run("FindNodeAtLocation", func(t *testing.T) {
		n := FindNodeAtLocation(root, Path{"a", "b", 1, "c"})
		if n == nil || n.Type != BooleanNode || n.Value != true || text[n.Offset:n.End()] != "true" {
			t.Fatalf("node %+v", n)
		}
		if got := FindNodeAtLocation(root, Path{}); got != root {
			t.Error("the empty path is the root")
		}
		for _, p := range []Path{{"x"}, {"a", 0}, {"a", "b", 2}, {"a", "b", -1}, {"d", "e"}, {1.5}} {
			if got := FindNodeAtLocation(root, p); got != nil {
				t.Errorf("%v found %+v", p, got)
			}
		}
		if FindNodeAtLocation(nil, Path{"a"}) != nil {
			t.Error("a nil root has no nodes")
		}
	})
	t.Run("NodePath", func(t *testing.T) {
		n := FindNodeAtLocation(root, Path{"a", "b", 1, "c"})
		if got := NodePath(n); !reflect.DeepEqual(got, Path{"a", "b", 1, "c"}) {
			t.Errorf("path %v", got)
		}
		if got := NodePath(root); len(got) != 0 {
			t.Errorf("root path %v", got)
		}
	})
	t.Run("FindNodeAtOffset", func(t *testing.T) {
		at := func(sub string, right bool) *Node {
			return FindNodeAtOffset(root, strings.Index(text, sub), right)
		}
		if n := at("true", false); n == nil || n.Type != BooleanNode {
			t.Errorf("node at true: %+v", n)
		}
		if n := at("10", false); n == nil || n.Value != 10.0 {
			t.Errorf("node at 10: %+v", n)
		}
		if n := at(`"d"`, false); n == nil || n.Type != StringNode || n.Value != "d" {
			t.Errorf("node at the name: %+v", n)
		}
		if n := at(`: "x"`, false); n == nil || n.Type != PropertyNode {
			t.Errorf("node at the colon: %+v", n)
		}
		if n := FindNodeAtOffset(root, len(text), false); n != nil {
			t.Errorf("node past the end: %+v", n)
		}
		if n := FindNodeAtOffset(root, len(text), true); n != root {
			t.Errorf("node at the right bound: %+v", n)
		}
	})
	t.Run("NodeValue", func(t *testing.T) {
		want := obj{"a": obj{"b": arr{10.0, obj{"c": true}}}, "d": "x"}
		if got := NodeValue(root); !reflect.DeepEqual(got, want) {
			t.Errorf("value %#v", got)
		}
		if NodeValue(&Node{Type: PropertyNode}) != nil {
			t.Error("a property has no value of its own")
		}
	})
}

func TestDecode(t *testing.T) {
	type font struct {
		Family string  `json:"family"`
		Size   float64 `json:"size"`
	}
	type settings struct {
		Tab    int               `json:"editor.tabSize"`
		Fonts  []font            `json:"fonts"`
		Extra  map[string]string `json:"extra"`
		Maybe  *bool             `json:"maybe"`
		Number int64
	}
	text := `{
		// a comment
		"editor.tabSize": 4,
		"fonts": [{"family": "Mono", "size": 13.5,},],
		"extra": {"k": "v"},
		"maybe": null,
		"Number": 9007199254740992,
	}`
	root, errs := ParseTree(text, ParseOptions{})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var got settings
	if err := Decode(root, &got); err != nil {
		t.Fatal(err)
	}
	want := settings{Tab: 4, Fonts: []font{{"Mono", 13.5}}, Extra: map[string]string{"k": "v"}, Number: 9007199254740992}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded %+v, want %+v", got, want)
	}

	var anyValue any
	if err := Decode(FindNodeAtLocation(root, Path{"fonts", 0}), &anyValue); err != nil || !reflect.DeepEqual(anyValue, obj{"family": "Mono", "size": 13.5}) {
		t.Errorf("decoded %#v: %v", anyValue, err)
	}
	var tab string
	if err := Decode(FindNodeAtLocation(root, Path{"editor.tabSize"}), &tab); err == nil {
		t.Error("a number was decoded into a string")
	}
	if err := Decode(nil, &tab); err == nil {
		t.Error("a missing value was decoded")
	}
	big := FindNodeAtLocation(ParseTreeOnly("1e999"), Path{})
	if err := Decode(big, &anyValue); err == nil {
		t.Error("an infinity was decoded")
	}
}

func TestDecodeErrorRoutes(t *testing.T) {
	root := ParseTreeOnly(`{"editor.tabSize": 4}`)
	tests := []struct {
		name  string
		node  *Node
		into  any
		route string
	}{
		{"no node", nil, new(string), "Decode.JSONCNoValue"},
		{"a number into a string", FindNodeAtLocation(root, Path{"editor.tabSize"}), new(string), "Decode.JSONCDecodeFailed"},
		{"a value that cannot be written as JSON", FindNodeAtLocation(ParseTreeOnly("1e999"), Path{}), new(any), "Decode.JSONCDecodeFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Decode(tt.node, tt.into)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tt.route)
			}
		})
	}
}

// ParseTreeOnly is the tree of a text whose errors do not matter to the test.
func ParseTreeOnly(text string) *Node {
	root, _ := ParseTree(text, ParseOptions{})
	return root
}

func TestGetLocation(t *testing.T) {
	// The offset is where the pipe is.
	loc := func(text string) Location {
		i := strings.Index(text, "|")
		return GetLocation(strings.Replace(text, "|", "", 1), i)
	}
	cases := []struct {
		text   string
		path   Path
		atKey  bool
		prev   string
		prevOK bool
	}{
		{`{ "a": 1 }|`, Path{}, false, "", false},
		{`{|}`, Path{""}, true, "", false},
		{`{ "a|b": 1 }`, Path{"ab"}, true, "ab", true},
		{`{ "ab"|: 1 }`, Path{"ab"}, true, "ab", true},
		{`{ "ab": |}`, Path{"ab"}, false, "", false},
		{`{ "ab": 1|}`, Path{"ab"}, false, "", true},
		{`{ "ab": 1, | }`, Path{""}, true, "", false},
		{`{ "a": [1, |] }`, Path{"a", 1}, false, "", false},
		{`{ "a": [1, 2|] }`, Path{"a", 1}, false, "", true},
		{`{ "a": [ {"b": 1}, { |} ] }`, Path{"a", 1, ""}, true, "", false},
		{`{ "a": { "b": true|`, Path{"a", "b"}, false, "", true},
		{``, Path{}, false, "", false},
	}
	for _, c := range cases {
		got := loc(c.text)
		if !reflect.DeepEqual(got.Path, c.path) || got.IsAtPropertyKey != c.atKey || (got.PreviousNode != nil) != c.prevOK {
			t.Errorf("%s: path %#v key %v previous %+v; want %#v %v %v", c.text, got.Path, got.IsAtPropertyKey, got.PreviousNode, c.path, c.atKey, c.prevOK)
		}
		if got.PreviousNode != nil && c.prev != "" && got.PreviousNode.Value != c.prev {
			t.Errorf("%s: previous %+v", c.text, got.PreviousNode)
		}
	}
	t.Run("the colon is remembered by the name before it", func(t *testing.T) {
		text := `{ "ab" : |}`
		got := GetLocation(strings.Replace(text, "|", "", 1), strings.Index(text, "|"))
		if got.PreviousNode != nil || got.IsAtPropertyKey {
			t.Errorf("%+v", got)
		}
	})
	t.Run("Matches", func(t *testing.T) {
		l := Location{Path: Path{"a", "b", 2, "c"}}
		yes := []Path{{"a", "b", 2, "c"}, {"a", "*", 2, "c"}, {"a", "**", "c"}, {"**"}, {"a"}, {"*", "*"}, {"a", "**"}}
		no := []Path{{"b"}, {"a", "b", 3}, {"a", "b", 2, "c", "d"}, {"a", "x", 2, "c"}, {"**", "x"}}
		for _, p := range yes {
			if !l.Matches(p) {
				t.Errorf("%v does not match", p)
			}
		}
		for _, p := range no {
			if l.Matches(p) {
				t.Errorf("%v matches", p)
			}
		}
	})
}

// FuzzParse checks that the scanner and the parser end, whatever the text, and that every offset
// they give is in the text and in order.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		``, `{}`, `[1,2,]`, `{"a": [true, null, {"b": "é😀"}], // c
		}`, `{"a" 1 "b": }`, `/* x`, `"\`, `-`, `1.e`, `{{{{`, `]]]]`, `{"a":[{"b":`, "\x00\xff", `{,"a",:}`, " ",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		sc := NewScanner(text, false)
		prevEnd := 0
		for k := sc.Scan(); k != EOF; k = sc.Scan() {
			if sc.Offset() != prevEnd || sc.Length() <= 0 || sc.Offset()+sc.Length() > len(text) {
				t.Fatalf("token %v at %d length %d after %d in %q", k, sc.Offset(), sc.Length(), prevEnd, text)
			}
			prevEnd = sc.Offset() + sc.Length()
			if k == StringLiteral && !utf8.ValidString(sc.Value()) && utf8.ValidString(text) {
				t.Fatalf("invalid string value %q for %q", sc.Value(), text)
			}
		}
		if prevEnd != len(text) {
			t.Fatalf("the tokens stop at %d of %d in %q", prevEnd, len(text), text)
		}
		for _, opts := range []ParseOptions{{}, {DisallowComments: true, DisallowTrailingComma: true, AllowEmptyContent: true}} {
			root, errs := ParseTree(text, opts)
			for _, e := range errs {
				if e.Offset < 0 || e.Length < 0 || e.Offset+e.Length > len(text) {
					t.Fatalf("error %+v out of range in %q", e, text)
				}
			}
			if root == nil {
				continue
			}
			checkNode(t, root, nil, len(text), text)
			NodeValue(root)
			Parse(text, opts)
			FindNodeAtOffset(root, len(text)/2, true)
		}
		for _, off := range []int{0, len(text) / 2, len(text)} {
			GetLocation(text, off)
		}
	})
}

func checkNode(t *testing.T, n, parent *Node, size int, text string) {
	t.Helper()
	if n.Parent != parent {
		t.Fatalf("wrong parent of the node at %d in %q", n.Offset, text)
	}
	if n.Offset < 0 || n.Length < 0 || n.Offset+n.Length > size {
		t.Fatalf("node %s at %d length %d out of range in %q", n.Type, n.Offset, n.Length, text)
	}
	if parent != nil && (n.Offset < parent.Offset || n.End() > parent.End()) {
		t.Fatalf("node %s at %d..%d outside its parent %d..%d in %q", n.Type, n.Offset, n.End(), parent.Offset, parent.End(), text)
	}
	if n.Type == PropertyNode && (len(n.Children) == 0 || len(n.Children) > 2 || n.Children[0].Type != StringNode) {
		t.Fatalf("property with children %+v in %q", n.Children, text)
	}
	last := n.Offset
	for _, c := range n.Children {
		if c.Offset < last {
			t.Fatalf("children out of order in %q", text)
		}
		last = c.Offset
		checkNode(t, c, n, size, text)
	}
}
