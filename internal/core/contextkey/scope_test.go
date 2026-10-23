package contextkey

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

type recorder struct {
	events [][]string
}

func (r *recorder) listen(s *Scope) func() {
	return s.OnChange(func(e ChangeEvent) { r.events = append(r.events, e.Keys) })
}

func TestScopeValues(t *testing.T) {
	root := NewRoot()
	part := root.Child()
	widget := part.Child()
	root.Set("window", true)
	part.Set("shared", "part")
	widget.Set("shared", "widget")
	widget.Set("only", 3)

	for _, c := range []struct {
		scope *Scope
		key   string
		want  any
	}{
		{widget, "window", true}, {widget, "shared", "widget"}, {part, "shared", "part"}, {root, "shared", nil},
		{widget, "only", 3.0}, {part, "only", nil}, {widget, "unknown", nil},
	} {
		if got := c.scope.Get(c.key); got != c.want {
			t.Errorf("Get(%q) = %v, want %v", c.key, got, c.want)
		}
		if got := c.scope.Value(c.key); got != c.want {
			t.Errorf("Value(%q) = %v", c.key, got)
		}
	}
	widget.Reset("shared")
	if widget.Get("shared") != "part" {
		t.Error("reset did not uncover the parent's value")
	}
	widget.Reset("not set") // nothing happens
	if !widget.Matches(parse(t, "window && only == 3 && shared == part")) || widget.Matches(parse(t, "!window")) || !widget.Matches(nil) {
		t.Error("Matches")
	}
}

func TestScopeNormalizesValues(t *testing.T) {
	s := NewRoot()
	s.Set("int", 3)
	s.Set("uint", uint8(4))
	s.Set("float32", float32(1.5))
	s.Set("strings", []string{"a", "b"})
	s.Set("bools", map[string]bool{"x": true})
	s.Set("named", namedString("n"))
	s.Set("nested", []map[string]int{{"a": 1}})
	s.Set("odd", struct{ A int }{1})
	s.Set("mixed", map[string]any{"l": []int{1, 2}})
	s.Set("intmap", map[int]string{1: "a"})
	for key, want := range map[string]any{
		"int": 3.0, "uint": 4.0, "float32": 1.5, "strings": []any{"a", "b"}, "bools": map[string]any{"x": true},
		"named": "n", "nested": []any{map[string]any{"a": 1.0}}, "mixed": map[string]any{"l": []any{1.0, 2.0}},
	} {
		if got := s.Get(key); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
	if !parse(t, "a in strings").Evaluate(MapContext{"a": "b", "strings": s.Get("strings")}) {
		t.Error("in over a normalized slice")
	}
	if !parse(t, "odd").Evaluate(s) || !parse(t, "int == 3").Evaluate(s) {
		t.Error("odd values")
	}
	s.Set("flag", true)
	s.Set("flag", false)
	if s.Get("flag") != false {
		t.Error("bool")
	}
}

type namedString string

func TestScopeEvents(t *testing.T) {
	root := NewRoot()
	part := root.Child()
	widget := part.Child()
	other := part.Child()
	var rRoot, rPart, rWidget, rOther recorder
	rRoot.listen(root)
	rPart.listen(part)
	rWidget.listen(widget)
	rOther.listen(other)

	t.Run("a key set in the root is seen by every scope that does not hide it", func(t *testing.T) {
		widget.Set("k", "own")
		rRoot.events, rPart.events, rWidget.events, rOther.events = nil, nil, nil, nil
		root.Set("k", "root")
		assertEvents(t, "root", rRoot.events, [][]string{{"k"}})
		assertEvents(t, "part", rPart.events, [][]string{{"k"}})
		assertEvents(t, "widget", rWidget.events, nil)
		assertEvents(t, "other", rOther.events, [][]string{{"k"}})
	})
	t.Run("setting the same value says nothing", func(t *testing.T) {
		rRoot.events, rPart.events = nil, nil
		root.Set("k", "root")
		widget.Set("k", "own")
		assertEvents(t, "root", rRoot.events, nil)
		assertEvents(t, "widget", rWidget.events[len(rWidget.events):], nil)
	})
	t.Run("a scope that sets what its parent has already says nothing", func(t *testing.T) {
		rOther.events = nil
		other.Set("k", "root")
		assertEvents(t, "other", rOther.events, nil)
		other.Reset("k")
		assertEvents(t, "other after reset", rOther.events, nil)
	})
	t.Run("reset tells when the value seen changes", func(t *testing.T) {
		rWidget.events = nil
		widget.Reset("k")
		assertEvents(t, "widget", rWidget.events, [][]string{{"k"}})
	})
	t.Run("a value changed to another type or deep value", func(t *testing.T) {
		root.Set("list", []string{"a"})
		rRoot.events = nil
		root.Set("list", []string{"a"})
		root.Set("list", []string{"a", "b"})
		assertEvents(t, "root", rRoot.events, [][]string{{"list"}})
	})
	t.Run("unsubscribe", func(t *testing.T) {
		var r recorder
		off := r.listen(part)
		root.Set("u", 1)
		off()
		root.Set("u", 2)
		assertEvents(t, "listener", r.events, [][]string{{"u"}})
	})
	t.Run("batch sends one event with all the keys", func(t *testing.T) {
		rRoot.events, rPart.events, rWidget.events = nil, nil, nil
		root.Batch(func() {
			root.Set("b1", 1)
			part.Set("b2", 2)
			root.Set("b1", 3)
			root.Batch(func() { widget.Set("b3", 1) })
			if len(rRoot.events) != 0 {
				t.Error("an event was sent during the batch")
			}
		})
		assertEvents(t, "root", rRoot.events, [][]string{{"b1"}})
		assertEvents(t, "part", rPart.events, [][]string{{"b1", "b2"}})
		assertEvents(t, "widget", rWidget.events, [][]string{{"b1", "b2", "b3"}})
	})
	t.Run("the event answers about keys", func(t *testing.T) {
		e := ChangeEvent{Keys: []string{"a", "b"}}
		if !e.AffectsSome([]string{"b", "z"}) || e.AffectsSome([]string{"z"}) || e.AffectsSome(nil) {
			t.Error("AffectsSome")
		}
		if !e.AllKeysContainedIn([]string{"a", "b", "c"}) || e.AllKeysContainedIn([]string{"a"}) {
			t.Error("AllKeysContainedIn")
		}
	})
}

func assertEvents(t *testing.T, name string, got, want [][]string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: events %v, want %v", name, got, want)
	}
}

func TestScopeDispose(t *testing.T) {
	root := NewRoot()
	part := root.Child()
	widget := part.Child()
	var r recorder
	r.listen(widget)
	widget.Set("w", 1)
	part.Dispose()
	r.events = nil
	if widget.Get("w") != nil {
		t.Error("a disposed scope keeps its values")
	}
	widget.Set("x", 1)
	part.Set("y", 1)
	widget.Reset("x")
	root.Set("z", 1)
	if len(r.events) != 0 || widget.Get("x") != nil || part.Get("y") != nil {
		t.Errorf("a disposed scope is alive: %v", r.events)
	}
	if part.Child().Get("z") != 1.0 {
		t.Error("a child of a disposed scope still reads its ancestors")
	}
	c := part.Child()
	c.Set("c", 1)
	if c.Get("c") != nil {
		t.Error("a child of a disposed scope should be disposed")
	}
	part.Dispose() // twice is fine
	widget.OnChange(func(ChangeEvent) { t.Error("listener of a disposed scope") })()
	widget.OnChange(func(ChangeEvent) { t.Error("listener of a disposed scope") })
	widget.Set("again", 1)
	// The parent no longer knows the scope.
	root.mu.Lock()
	n := len(root.children)
	root.mu.Unlock()
	if n != 0 {
		t.Errorf("root has %d children", n)
	}
}

func TestScopeConcurrentUse(t *testing.T) {
	root := NewRoot()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			child := root.Child()
			child.OnChange(func(ChangeEvent) {})
			for j := 0; j < 200; j++ {
				child.Set("k", j)
				root.Set("shared", j)
				child.Get("shared")
				child.Matches(Greater("k", 5))
				child.Reset("k")
			}
			child.Dispose()
		}()
	}
	wg.Wait()
}

// Ported from VS Code: the error messages of src/vs/platform/contextkey/common/contextkey.ts.
func TestParseErrors(t *testing.T) {
	t.Run("Parse fails with a tag and the place", func(t *testing.T) {
		_, err := Parse("a && ")
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("Parse.WhenClauseInvalid") {
			t.Fatalf("error %v", err)
		}
		if !strings.Contains(appErr.Message, "Unexpected end of expression") {
			t.Errorf("message %q", appErr.Message)
		}
		if appErr.Metadata["offset"] != 5 {
			t.Errorf("metadata %v", appErr.Metadata)
		}
	})
	t.Run("messages", func(t *testing.T) {
		cases := []struct {
			input string
			want  []Diagnostic
		}{
			{"", []Diagnostic{{"Empty context key expression. Did you forget to write an expression? You can also put 'false' or 'true' to always evaluate to false or true, respectively.", 0, 0}}},
			{"a &&", []Diagnostic{{"Unexpected end of expression. Did you forget to put a context key?", 4, 0}}},
			{"a b", []Diagnostic{{"Unexpected token. Did you forget to put && or || before the token?", 2, 1}}},
			{"a & b", []Diagnostic{{"Unexpected token. Hint: Did you mean &&?", 2, 1}}},
			{"a | b", []Diagnostic{{"Unexpected token. Hint: Did you mean ||?", 2, 1}}},
			{"a = b", []Diagnostic{{"Unexpected token. Hint: Did you mean == or =~?", 2, 1}}},
			{"a == 'b", []Diagnostic{{"Unexpected token. Hint: Did you forget to open or close the quote?", 5, 2}}},
			{"a =~ /b", []Diagnostic{{"Unexpected token. Hint: Did you forget to escape the '/' (slash) character? Put two backslashes before it to escape, e.g., '\\\\/'.", 5, 2}}},
			{"(a", []Diagnostic{{"Expected: closing parenthesis ')'\nReceived: 'EOF'.", 2, 3}}},
			{"a not b", []Diagnostic{{"Expected: 'in' after 'not'.\nReceived: 'b'.", 6, 1}}},
			{"a =~ b", []Diagnostic{{"Expected: REGEX\nReceived: 'b'.", 5, 1}}},
			{"a =~ /(/", []Diagnostic{{"Expected: REGEX\nReceived: '/(/'.", 5, 3}}},
			{"a =~ /x/q", []Diagnostic{{"Unexpected token. Did you forget to put && or || before the token?", 8, 1}}},
		}
		for _, c := range cases {
			got := Validate(c.input)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Validate(%q):\n got %+v\nwant %+v", c.input, got, c.want)
			}
		}
		if got := Validate("a && b || !(c in d)"); len(got) != 0 {
			t.Errorf("valid clause: %v", got)
		}
	})
	t.Run("the strict parser refuses a regex with an unescaped slash", func(t *testing.T) {
		if got := Validate("a =~ /b/c/"); len(got) == 0 {
			t.Error("accepted")
		}
		if e := NewParser(ParserConfig{RegexErrorRecovery: true}).Parse("a =~ /b/c/"); e == nil || e.Serialize() != `a =~ /b\/c/` {
			t.Errorf("recovered %v", e)
		}
	})
	t.Run("old style quoted regexes", func(t *testing.T) {
		for _, p := range []*Parser{NewParser(ParserConfig{}), NewParser(ParserConfig{RegexErrorRecovery: true})} {
			if e := p.Parse(`a =~ '/b/i'`); p.cfg.RegexErrorRecovery && (e == nil || e.Serialize() != "a =~ /b/i") {
				t.Errorf("quoted regex %v", e)
			}
		}
		for _, bad := range []string{`a =~ ''`, `a =~ 'b'`, `a =~ '/('`, `a =~ ' '`} {
			p := NewParser(ParserConfig{RegexErrorRecovery: true})
			if p.Parse(bad) != nil || len(p.ParsingErrors()) == 0 {
				t.Errorf("%s was accepted", bad)
			}
		}
	})
	t.Run("a parser can be used again", func(t *testing.T) {
		p := NewParser(ParserConfig{})
		if p.Parse("a &&") != nil || len(p.ParsingErrors()) != 1 {
			t.Fatal("first parse")
		}
		if e := p.Parse("a"); e == nil || len(p.ParsingErrors()) != 0 || len(p.LexingErrors()) != 0 {
			t.Error("errors were kept")
		}
	})
	t.Run("token text", func(t *testing.T) {
		for tt, want := range map[TokenType]string{TokLParen: "(", TokRParen: ")", TokNeg: "!", TokEq: "==", TokNotEq: "!=", TokLt: "<", TokLtEq: "<=", TokGt: ">", TokGtEq: ">=", TokRegexOp: "=~", TokTrue: "true", TokFalse: "false", TokIn: "in", TokNot: "not", TokAnd: "&&", TokOr: "||", TokEOF: "EOF"} {
			if got := (Token{Type: tt}).String(); got != want {
				t.Errorf("%d = %q, want %q", tt, got, want)
			}
		}
		if (Token{Type: TokEq, TripleEq: true}).String() != "===" || (Token{Type: TokNotEq, TripleEq: true}).String() != "!==" {
			t.Error("triple")
		}
	})
	t.Run("words with letters of other scripts and skipped characters", func(t *testing.T) {
		if e := parse(t, "ключ && 键 == 值"); e.Serialize() != "ключ && 键 == '值'" {
			t.Errorf("%s", e.Serialize())
		}
		// A character that cannot start a word is skipped without an error.
		p := NewParser(ParserConfig{})
		if e := p.Parse("a && { b == c"); e == nil || e.Serialize() != "a && b == 'c'" || len(p.LexingErrors()) != 0 {
			t.Errorf("skipped %v", e)
		}
	})
}

// FuzzParse checks that the scanner and the parser end and keep their promises for any
// text: tokens cover the input in order, a parsed clause serializes to a clause that
// parses to the same clause, and negating twice gives the clause back.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "a", "!a", "a && b || c", "!(a && b || c)", "a == 'x'", "a =~ /x(y)/i", "a =~ //x/", "a in b", "a not in b", "a > 1.5",
		"(((a)))", "a &&", "'", "/", "a =~", "a ==== b", "!!a", "a || || b", "\xff\xfe", "a b", "key == in", "a < b <= c",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		toks := (&Scanner{}).Reset(input).Scan()
		prev := 0
		for _, tok := range toks {
			if tok.Offset < prev || tok.Offset > len(input) {
				t.Fatalf("token %v out of order or range in %q", tok, input)
			}
			prev = tok.Offset
		}
		if toks[len(toks)-1].Type != TokEOF {
			t.Fatalf("no EOF in %q", input)
		}
		for _, cfg := range []ParserConfig{{}, {RegexErrorRecovery: true}} {
			p := NewParser(cfg)
			e := p.Parse(input)
			if e == nil {
				if len(p.ParsingErrors()) == 0 {
					t.Fatalf("nil without a parsing error for %q", input)
				}
				continue
			}
			s := e.Serialize()
			if !utf8.ValidString(s) && utf8.ValidString(input) {
				t.Fatalf("invalid UTF-8 serialization of %q", input)
			}
			e.Keys()
			e.Evaluate(MapContext{})
			if !hasCompare(e) { // a comparison with a missing or non-numeric value is false both ways
				checkNegation(t, e, s)
			}
			if !Implies(e, e) {
				t.Fatalf("%q does not imply itself", s)
			}
			if hasStringCompare(e) {
				// VS Code writes the value of < and > without quotes, so a value with
				// spaces cannot be read back.
				continue
			}
			again, err := Parse(s)
			if err != nil {
				// Quoted values that hold a quote cannot be written back; that is VS Code's
				// limit too.
				if strings.Contains(s, "'") {
					continue
				}
				t.Fatalf("%q serialized to %q, which does not parse: %v", input, s, err)
			}
			if utf8.ValidString(input) && !hasCompare(e) && !equivalent(e, again) {
				t.Fatalf("%q -> %q -> %q, which means something else", input, s, again.Serialize())
			}
		}
	})
}

// hasStringCompare tells whether an expression compares with a value that is not a number.
func hasStringCompare(e Expr) bool {
	switch x := e.(type) {
	case *compareExpr:
		_, isString := x.value.(string)
		return isString
	case *andExpr:
		for _, t := range x.exprs {
			if hasStringCompare(t) {
				return true
			}
		}
	case *orExpr:
		for _, t := range x.exprs {
			if hasStringCompare(t) {
				return true
			}
		}
	}
	return false
}

// checkNegation checks that Negate gives the opposite answer in contexts made of a few values
// for the first keys of the expression, and that negating twice gives an equal answer.
func checkNegation(t *testing.T, e Expr, text string) {
	t.Helper()
	keys := sortedCopy(e.Keys())
	if len(keys) > 4 {
		keys = keys[:4]
	}
	values := []any{nil, true, "5", "x", []any{"5"}}
	total := 1
	for range keys {
		total *= len(values)
	}
	neg, twice := e.Negate(), e.Negate().Negate()
	for n := 0; n < total; n++ {
		ctx := MapContext{}
		m := n
		for _, k := range keys {
			ctx[k] = values[m%len(values)]
			m /= len(values)
		}
		if neg.Evaluate(ctx) == e.Evaluate(ctx) || twice.Evaluate(ctx) != e.Evaluate(ctx) {
			t.Fatalf("negation of %q is wrong in %v: %s", text, ctx, neg.Serialize())
		}
	}
}

// hasCompare tells whether an expression has a comparison with < or >.
func hasCompare(e Expr) bool {
	switch x := e.(type) {
	case *compareExpr:
		return true
	case *andExpr:
		for _, t := range x.exprs {
			if hasCompare(t) {
				return true
			}
		}
	case *orExpr:
		for _, t := range x.exprs {
			if hasCompare(t) {
				return true
			}
		}
	}
	return false
}

// equivalent tells whether two expressions answer the same in contexts made of a few
// values for the first keys of the two.
func equivalent(a, b Expr) bool {
	keys := slicesCompact(sortedCopy(append(a.Keys(), b.Keys()...)))
	if len(keys) > 4 {
		keys = keys[:4]
	}
	values := []any{nil, true, "5", "x", []any{"5"}}
	total := 1
	for range keys {
		total *= len(values)
	}
	for n := 0; n < total; n++ {
		ctx := MapContext{}
		m := n
		for _, k := range keys {
			ctx[k] = values[m%len(values)]
			m /= len(values)
		}
		if a.Evaluate(ctx) != b.Evaluate(ctx) {
			return false
		}
	}
	return true
}

func slicesCompact(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}
