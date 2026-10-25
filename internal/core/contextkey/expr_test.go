package contextkey

import (
	"regexp"
	"testing"

	"github.com/rm4n0s/errors"
)

func must(t testing.TB, e Expr, err error) Expr {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func rx(t testing.TB, key, source, flags string) Expr {
	t.Helper()
	e, err := Regex(key, source, flags)
	return must(t, e, err)
}

func parse(t testing.TB, s string) Expr {
	t.Helper()
	e, err := Parse(s)
	return must(t, e, err)
}

// Ported from VS Code: src/vs/platform/contextkey/test/common/contextkey.test.ts
// (the cases of Windows' case-insensitive file URIs are skipped: Emetos runs on Linux).
func TestVSCodeExpr(t *testing.T) {
	t.Run("ContextKeyExpr.equals", func(t *testing.T) {
		a := And(
			Has("a1"), And(Has("and.a")), Has("a2"), rx(t, "d3", "d.*", ""), rx(t, "d4", `\*\*3*`, ""),
			Equals("b1", "bb1"), Equals("b2", "bb2"), NotEquals("c1", "cc1"), NotEquals("c2", "cc2"), Not("d1"), Not("d2"),
		)
		b := And(
			Equals("b2", "bb2"), NotEquals("c1", "cc1"), Not("d1"), rx(t, "d4", `\*\*3*`, ""), NotEquals("c2", "cc2"),
			Has("a2"), Equals("b1", "bb1"), rx(t, "d3", "d.*", ""), Has("a1"), And(Equals("and.a", true)), Not("d2"),
		)
		if !a.Equals(b) {
			t.Error("expressions should be equal")
		}
	})
	t.Run("issue #134942: Equals in comparator expressions", func(t *testing.T) {
		for _, c := range []struct {
			expr Expr
			str  string
		}{
			{Greater("value", 0), "value > 0"}, {GreaterEquals("value", 0), "value >= 0"},
			{Smaller("value", 0), "value < 0"}, {SmallerEquals("value", 0), "value <= 0"},
		} {
			if !c.expr.Equals(parse(t, c.str)) {
				t.Errorf("%s differs from the parsed one", c.str)
			}
		}
	})
	t.Run("normalize", func(t *testing.T) {
		if !Equals("key1", true).Equals(Has("key1")) || !NotEquals("key1", false).Equals(Has("key1")) ||
			!Equals("key1", false).Equals(Not("key1")) || !NotEquals("key1", true).Equals(Not("key1")) {
			t.Error("booleans are not normalized")
		}
	})
	t.Run("evaluate", func(t *testing.T) {
		ctx := MapContext{"a": true, "b": false, "c": "5", "d": "d"}
		check := func(expr string, want bool) {
			t.Helper()
			if got := parse(t, expr).Evaluate(ctx); got != want {
				t.Errorf("%s = %v, want %v", expr, got, want)
			}
		}
		type batch struct {
			expr         string
			truthy       bool
			equalsFive   bool // value == '5'
			matchesDStar bool // /d.*/.test(value)
			matchesD     bool // /D/i.test(value)
		}
		for _, b := range []batch{
			{"a", true, false, false, false}, // true: "true" has no d... wait
			{"b", false, false, false, false},
			{"c", true, true, false, false},
			{"d", true, false, true, true},
			{"z", false, false, true, true}, // undefined is tested as the string "undefined"
		} {
			// "true" and "false": /d.*/ matches neither, /D/i neither.
			check(b.expr, b.truthy)
			check(b.expr+" == true", b.truthy)
			check(b.expr+" != true", !b.truthy)
			check(b.expr+" == false", !b.truthy)
			check(b.expr+" != false", b.truthy)
			check(b.expr+" == 5", b.equalsFive)
			check(b.expr+" != 5", !b.equalsFive)
			check("!"+b.expr, !b.truthy)
			check(b.expr+" =~ /d.*/", b.matchesDStar)
			check(b.expr+" =~ /D/i", b.matchesD)
		}
		check("true", true)
		check("false", false)
		check("a && !b", true)
		check("a && b", false)
		check("a && !b && c == 5", true)
		check("d =~ /e.*/", false)
		// && is evaluated before ||
		check("b && a || a", true)
		check("a || b", true)
		check("b || b", false)
		check("b && a || a && b", false)
	})
	t.Run("negate", func(t *testing.T) {
		for _, c := range [][2]string{
			{"true", "false"}, {"false", "true"}, {"a", "!a"},
			{"a && b || c", "!a && !c || !b && !c"},
			{"a && b || c || d", "!a && !c && !d || !b && !c && !d"},
			{"!a && !b || !c && !d", "a && c || a && d || b && c || b && d"},
			{"!a && !b || !c && !d || !e && !f", "a && c && e || a && c && f || a && d && e || a && d && f || b && c && e || b && c && f || b && d && e || b && d && f"},
		} {
			if got := parse(t, c[0]).Negate().Serialize(); got != c[1] {
				t.Errorf("negate(%s) = %s, want %s", c[0], got, c[1])
			}
		}
	})
	t.Run("false, true", func(t *testing.T) {
		for _, c := range [][2]string{
			{"true", "true"}, {"!true", "false"}, {"false", "false"}, {"!false", "true"},
			{"a && true", "a"}, {"a && false", "false"}, {"a || true", "true"}, {"a || false", "a"},
			{"isMac", "false"}, {"isLinux", "true"}, {"isWindows", "false"}, {"isChromeOS", "false"},
		} {
			if got := parse(t, c[0]).Serialize(); got != c[1] {
				t.Errorf("%s = %s, want %s", c[0], got, c[1])
			}
		}
	})
	t.Run("issue #101015: distribute OR", func(t *testing.T) {
		for _, c := range [][3]string{
			{"a", "b", "a && b"},
			{"a || b", "c", "a && c || b && c"},
			{"a || b", "c || d", "a && c || a && d || b && c || b && d"},
			{"a || b", "c && d", "a && c && d || b && c && d"},
			{"a || b", "c && d || e", "a && e || b && e || a && c && d || b && c && d"},
		} {
			if got := And(parse(t, c[0]), parse(t, c[1])).Serialize(); got != c[2] {
				t.Errorf("%s && %s = %s, want %s", c[0], c[1], got, c[2])
			}
		}
	})
	t.Run("ContextKeyInExpr", func(t *testing.T) {
		ainb := parse(t, "a in b")
		for _, c := range []struct {
			ctx  MapContext
			want bool
		}{
			{MapContext{"a": 3.0, "b": []any{3.0, 2.0, 1.0}}, true},
			{MapContext{"a": 3.0, "b": []any{1.0, 2.0, 3.0}}, true},
			{MapContext{"a": 3.0, "b": []any{1.0, 2.0}}, false},
			{MapContext{"a": 3.0}, false},
			{MapContext{"a": 3.0, "b": nil}, false},
			{MapContext{"a": "x", "b": []any{"x"}}, true},
			{MapContext{"a": "x", "b": []any{"y"}}, false},
			{MapContext{"a": "x", "b": map[string]any{}}, false},
			{MapContext{"a": "x", "b": map[string]any{"x": false}}, true},
			{MapContext{"a": "x", "b": map[string]any{"x": true}}, true},
			{MapContext{"a": "prototype", "b": map[string]any{}}, false},
		} {
			if got := ainb.Evaluate(c.ctx); got != c.want {
				t.Errorf("a in b with %v = %v", c.ctx, got)
			}
			if got := parse(t, "a not in b").Evaluate(c.ctx); got == c.want {
				t.Errorf("a not in b with %v = %v", c.ctx, got)
			}
		}
	})
	t.Run("issue #106524: distributing AND should normalize", func(t *testing.T) {
		actual := And(Or(Has("a"), Has("b")), Has("c"))
		expected := Or(And(Has("a"), Has("c")), And(Has("b"), Has("c")))
		if !actual.Equals(expected) {
			t.Errorf("%s != %s", actual.Serialize(), expected.Serialize())
		}
	})
	t.Run("issue #129625: duplicated terms", func(t *testing.T) {
		if got := Or(Has("A"), Has("B"), Has("A")).Serialize(); got != "A || B" {
			t.Errorf("or: %s", got)
		}
		if got := And(Has("A"), Has("B"), Has("A")).Serialize(); got != "A && B" {
			t.Errorf("and: %s", got)
		}
	})
	t.Run("constant OR and AND expressions", func(t *testing.T) {
		if got := Or(Has("A"), Not("A")).Serialize(); got != "true" {
			t.Errorf("or: %s", got)
		}
		if got := And(Has("A"), Not("A")).Serialize(); got != "false" {
			t.Errorf("and: %s", got)
		}
	})
	t.Run("issue #129625: remove duplicated terms when negating", func(t *testing.T) {
		expr := And(Has("A"), Or(Has("B1"), Has("B2")))
		for _, c := range []struct {
			name string
			got  Expr
			want string
		}{
			{"expr", expr, "A && B1 || A && B2"},
			{"negate", expr.Negate(), "!A || !A && !B1 || !A && !B2 || !B1 && !B2"},
			{"negate twice", expr.Negate().Negate(), "A && B1 || A && B2"},
			{"negate three times", expr.Negate().Negate().Negate(), "!A || !A && !B1 || !A && !B2 || !B1 && !B2"},
		} {
			if got := c.got.Serialize(); got != c.want {
				t.Errorf("%s = %s, want %s", c.name, got, c.want)
			}
		}
	})
	t.Run("implies", func(t *testing.T) {
		for _, c := range []struct {
			p, q string
			want bool
		}{
			{"a && b", "a", true}, {"a", "a && b", false},
			{"a", "a", true}, {"a", "a || b", true}, {"a", "a && b || a && c", false},
			{"a && b", "b", true}, {"a && b", "a && b || c", true}, {"a || b", "a || c", false},
			{"a || b", "a || b", true}, {"a && b", "a && b", true}, {"a || b", "a || b || c", true},
			{"c && a && b", "c && a", true},
		} {
			if got := Implies(parse(t, c.p), parse(t, c.q)); got != c.want {
				t.Errorf("%s implies %s = %v", c.p, c.q, got)
			}
		}
		if !Implies(False(), Has("a")) || !Implies(Has("a"), True()) || Implies(Or(Has("a"), Has("b")), Has("a")) {
			t.Error("implies of constants and ors")
		}
	})
	t.Run("Greater, GreaterEquals, Smaller, SmallerEquals evaluate", func(t *testing.T) {
		check := func(expr string, ctx MapContext, want bool) {
			t.Helper()
			if got := parse(t, expr).Evaluate(ctx); got != want {
				t.Errorf("%s with %v = %v", expr, ctx, got)
			}
		}
		check("a > 1", MapContext{}, false)
		check("a > 1", MapContext{"a": 0.0}, false)
		check("a > 1", MapContext{"a": 1.0}, false)
		check("a > 1", MapContext{"a": 2.0}, true)
		check("a > 1", MapContext{"a": "0"}, false)
		check("a > 1", MapContext{"a": "1"}, false)
		check("a > 1", MapContext{"a": "2"}, true)
		check("a > 1", MapContext{"a": "a"}, false)
		check("a > 10", MapContext{"a": 2.0}, false)
		check("a > 10", MapContext{"a": 11.0}, true)
		check("a > 10", MapContext{"a": "11"}, true)
		check("a > 10", MapContext{"a": "2"}, false)
		check("a > 1.1", MapContext{"a": 1.0}, false)
		check("a > 1.1", MapContext{"a": 2.0}, true)
		check("a > 1.1", MapContext{"a": 11.0}, true)
		check("a > 1.1", MapContext{"a": "1.1"}, false)
		check("a > 1.1", MapContext{"a": "2"}, true)
		check("a > 1.1", MapContext{"a": "11"}, true)
		check("a > b", MapContext{"a": "b"}, false)
		check("a > b", MapContext{"a": "c"}, false)
		check("a > b", MapContext{"a": 1000.0}, false)
		check("a >= 2", MapContext{"a": "1"}, false)
		check("a >= 2", MapContext{"a": "2"}, true)
		check("a >= 2", MapContext{"a": "3"}, true)
		check("a < 2", MapContext{"a": "1"}, true)
		check("a < 2", MapContext{"a": "2"}, false)
		check("a < 2", MapContext{"a": "3"}, false)
		check("a <= 2", MapContext{"a": "1"}, true)
		check("a <= 2", MapContext{"a": "2"}, true)
		check("a <= 2", MapContext{"a": "3"}, false)
	})
	t.Run("Greater, GreaterEquals, Smaller, SmallerEquals negate", func(t *testing.T) {
		for _, c := range [][2]string{
			{"a > 1", "a <= 1"}, {"a > 1.1", "a <= 1.1"}, {"a > b", "a <= b"},
			{"a >= 1", "a < 1"}, {"a >= 1.1", "a < 1.1"}, {"a >= b", "a < b"},
			{"a < 1", "a >= 1"}, {"a < 1.1", "a >= 1.1"}, {"a < b", "a >= b"},
			{"a <= 1", "a > 1"}, {"a <= 1.1", "a > 1.1"}, {"a <= b", "a > b"},
		} {
			if got := parse(t, c[0]).Negate().Serialize(); got != c[1] {
				t.Errorf("negate(%s) = %s, want %s", c[0], got, c[1])
			}
		}
	})
	t.Run("issue #111899: context keys can use < or >", func(t *testing.T) {
		actual := parse(t, "editorTextFocus && vim.active && vim.use<C-r>")
		if !actual.Equals(And(Has("editorTextFocus"), Has("vim.active"), Has("vim.use<C-r>"))) {
			t.Errorf("parsed as %s", actual.Serialize())
		}
	})
}

func TestExprMore(t *testing.T) {
	t.Run("keys", func(t *testing.T) {
		e := parse(t, "a && b == 1 || c in d && e =~ /x/ && !f && g > 2")
		assertKeys := func(e Expr, want ...string) {
			t.Helper()
			got := sortedCopy(e.Keys())
			if len(got) != len(want) {
				t.Fatalf("keys of %s = %v, want %v", e.Serialize(), got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("keys of %s = %v, want %v", e.Serialize(), got, want)
				}
			}
		}
		assertKeys(e, "a", "b", "c", "d", "e", "f", "g")
		assertKeys(parse(t, "a not in b"), "a", "b")
		assertKeys(parse(t, "!(a =~ /x/)"), "a")
		assertKeys(True())
		assertKeys(False())
	})
	t.Run("serialize is read back", func(t *testing.T) {
		for _, s := range []string{
			"a", "!a", "a == 'x y'", "a != 'x'", "a > 1", "a <= -2.5", "a in 'b'", "a not in 'b'", "a =~ /x\\/y/i",
			"a && b", "a || b", "a && b || c && d", "!(a =~ /x/)",
		} {
			e := parse(t, s)
			again := parse(t, e.Serialize())
			if !e.Equals(again) || e.Serialize() != again.Serialize() {
				t.Errorf("%s -> %s -> %s", s, e.Serialize(), again.Serialize())
			}
		}
	})
	t.Run("constants", func(t *testing.T) {
		for expr, want := range map[string]string{"isLinux == true": "true", "isLinux == false": "false", "isMac != 'true'": "true", "isLinux == 'true'": "true", "!isWindows": "true", "isWeb || a": "a", "isLinux && a": "a"} {
			if got := parse(t, expr).Serialize(); got != want {
				t.Errorf("%s = %s, want %s", expr, got, want)
			}
		}
		// Expressions made by the functions substitute constants too.
		if got := And(Has("a"), Equals("k", "v")).SubstituteConstants().Serialize(); got != "a && k == 'v'" {
			t.Errorf("%s", got)
		}
		e := &definedExpr{key: "isLinux", neg: &negation{}}
		if e.SubstituteConstants().Serialize() != "true" || (&notExpr{key: "isLinux", neg: &negation{}}).SubstituteConstants().Serialize() != "false" {
			t.Error("substitute")
		}
		eq := &equalsExpr{key: "isLinux", value: "true", neg: &negation{}}
		ne := &notEqualsExpr{key: "isLinux", value: "true", neg: &negation{}}
		if eq.SubstituteConstants().Serialize() != "true" || ne.SubstituteConstants().Serialize() != "false" {
			t.Error("substitute comparisons")
		}
		a := And(&definedExpr{key: "isLinux", neg: &negation{}}, Has("x"))
		_ = a
		or := Or(&definedExpr{key: "isWeb", neg: &negation{}}, Has("x"))
		if or.SubstituteConstants().Serialize() != "x" {
			t.Errorf("or: %s", or.SubstituteConstants().Serialize())
		}
	})
	t.Run("EqualWithConstants", func(t *testing.T) {
		if !EqualWithConstants(nil, nil) || EqualWithConstants(Has("a"), nil) || EqualWithConstants(nil, Has("a")) ||
			!EqualWithConstants(Has("a"), Has("a")) || EqualWithConstants(Has("a"), Has("b")) {
			t.Error("nil and plain cases")
		}
	})
	t.Run("And and Or of nothing", func(t *testing.T) {
		if And() != nil || Or() != nil || And(nil, nil) != nil || Or(nil) != nil {
			t.Error("no terms should give nil")
		}
		if And(nil, True()).Serialize() != "true" || Or(nil, False()).Serialize() != "false" {
			t.Error("only constants")
		}
	})
	t.Run("loose equality follows JavaScript", func(t *testing.T) {
		cases := []struct {
			ctx   any
			value string
			want  bool
		}{
			{"5", "5", true}, {5.0, "5", true}, {5.0, "05", true}, {5.0, "5.0", true}, {true, "1", true}, {false, "0", true},
			{false, "", true}, {"", "0", false}, {nil, "x", false}, {[]any{5.0}, "5", true}, {[]any{"a", "b"}, "a,b", true},
			{map[string]any{}, "[object Object]", true}, {5.0, " 5 ", true}, {" 5 ", "5", false}, {"abc", "ABC", false}, {5.0, "x", false},
		}
		for _, c := range cases {
			e := Equals("k", c.value)
			if got := e.Evaluate(MapContext{"k": c.ctx}); got != c.want {
				t.Errorf("%#v == '%s' is %v", c.ctx, c.value, got)
			}
		}
	})
	t.Run("a number as a value", func(t *testing.T) {
		if got := Equals("k", 5).Serialize(); got != "k == '5'" {
			t.Error(got)
		}
		if !Equals("k", 5).Evaluate(MapContext{"k": "5"}) || !Equals("k", 1.5).Evaluate(MapContext{"k": 1.5}) {
			t.Error("number values")
		}
		if got := Greater("k", 1e21).Serialize(); got != "k > 1e+21" {
			t.Error(got)
		}
		if got := Smaller("k", 0.0000001).Serialize(); got != "k < 1e-7" {
			t.Error(got)
		}
		if got := Greater("k", true); got.Type() != TypeFalse {
			t.Errorf("a boolean limit gave %s", got.Serialize())
		}
		if got := Greater("k", "1abc").Serialize(); got != "k > 1" {
			t.Error(got)
		}
		if got := Greater("k", []string{"x"}); got.Type() != TypeFalse {
			t.Error("an array limit")
		}
	})
	t.Run("truthiness", func(t *testing.T) {
		h := Has("k")
		for v, want := range map[string]bool{"": false, "0": true, "x": true} {
			if got := h.Evaluate(MapContext{"k": v}); got != want {
				t.Errorf("string %q: %v", v, got)
			}
		}
		for _, c := range []struct {
			v    any
			want bool
		}{{0.0, false}, {1.0, true}, {nil, false}, {[]any{}, true}, {map[string]any{}, true}, {false, false}} {
			if got := h.Evaluate(MapContext{"k": c.v}); got != c.want {
				t.Errorf("%#v: %v", c.v, got)
			}
		}
	})
	t.Run("regular expressions", func(t *testing.T) {
		e := rx(t, "k", "a/b", "gimy")
		if e.Serialize() != `k =~ /a\/b/im` {
			t.Error(e.Serialize())
		}
		if !e.Evaluate(MapContext{"k": "xA/B"}) || e.Evaluate(MapContext{"k": "ab"}) {
			t.Error("matching")
		}
		if !rx(t, "k", "x", "").Negate().Evaluate(MapContext{"k": "y"}) || rx(t, "k", "x", "").Negate().Negate().Serialize() != "k =~ /x/" {
			t.Error("negation")
		}
		if rx(t, "k", "", "").Serialize() != "k =~ /(?:)/" {
			t.Error("empty pattern")
		}
		if rx(t, "k", "[/]", "").Serialize() != "k =~ /[/]/" {
			t.Error("slash in a class")
		}
		if got := rx(t, "k", "a\nb", "").Serialize(); got != `k =~ /a\nb/` {
			t.Error(got)
		}
		// A pattern the engine cannot run is accepted and never matches.
		la := rx(t, "k", "a(?!b)", "")
		if la.Evaluate(MapContext{"k": "ac"}) || la.Serialize() != "k =~ /a(?!b)/" {
			t.Error("lookahead")
		}
		for _, bad := range []struct{ src, flags string }{{"(", ""}, {"a)", ""}, {"[a", ""}, {"a", "q"}, {"a", "ii"}, {`a\`, ""}} {
			_, err := Regex("k", bad.src, bad.flags)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("Regex->newRegex.WhenRegexInvalid") {
				t.Errorf("/%s/%s: %v", bad.src, bad.flags, err)
			}
		}
		if _, err := regexp.Compile("x"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("sorting orders the kinds", func(t *testing.T) {
		e := And(rx(t, "a", "x", ""), Greater("a", 1), In("a", "b"), NotEquals("a", "y"), Equals("a", "x"), Not("n"), Has("a"), SmallerEquals("a", 2), NotIn("a", "c"))
		want := "a && !n && a == 'x' && a != 'y' && a =~ /x/ && a in 'b' && a not in 'c' && a > 1 && a <= 2"
		if got := e.Serialize(); got != want {
			t.Errorf("%s\nwant %s", got, want)
		}
		if got := e.Negate().Negate().Serialize(); got != want {
			t.Errorf("double negation %s", got)
		}
	})
	t.Run("equality and ordering of every kind", func(t *testing.T) {
		all := []Expr{
			False(), True(), Has("a"), Not("a"), Equals("a", "1"), NotEquals("a", "1"), rx(t, "a", "x", ""), rx(t, "a", "x", "").Negate(),
			In("a", "b"), NotIn("a", "b"), Greater("a", 1), GreaterEquals("a", 1), Smaller("a", 1), SmallerEquals("a", 1),
			And(Has("a"), Has("b")), Or(Has("a"), Has("b")),
		}
		for i, x := range all {
			for j, y := range all {
				if (x.Equals(y)) != (i == j) {
					t.Errorf("%s equals %s: %v", x.Serialize(), y.Serialize(), x.Equals(y))
				}
				if (x.Cmp(y) == 0) != (i == j) {
					t.Errorf("%s cmp %s: %d", x.Serialize(), y.Serialize(), x.Cmp(y))
				}
				if x.Cmp(y) != -y.Cmp(x) && x.Cmp(y)*y.Cmp(x) >= 0 && i != j {
					t.Errorf("cmp not antisymmetric for %s and %s", x.Serialize(), y.Serialize())
				}
			}
		}
		// Same kind, different keys and values.
		for _, pair := range [][2]Expr{
			{Has("a"), Has("b")}, {Not("a"), Not("b")}, {Equals("a", "1"), Equals("a", "2")}, {Equals("a", "1"), Equals("b", "1")},
			{NotEquals("a", "1"), NotEquals("a", "2")}, {rx(t, "a", "x", ""), rx(t, "a", "y", "")}, {rx(t, "a", "x", ""), rx(t, "b", "x", "")},
			{rx(t, "a", "x", "").Negate(), rx(t, "a", "y", "").Negate()}, {In("a", "b"), In("a", "c")}, {NotIn("a", "b"), NotIn("a", "c")},
			{Greater("a", 1), Greater("a", 2)}, {Smaller("a", 1), Smaller("b", 1)}, {And(Has("a"), Has("b")), And(Has("a"), Has("c"))},
			{And(Has("a"), Has("b")), And(Has("a"), Has("b"), Has("c"))}, {Or(Has("a"), Has("b")), Or(Has("a"), Has("c"))},
			{Or(Has("a"), Has("b")), Or(Has("a"), Has("b"), Has("c"))},
		} {
			if pair[0].Cmp(pair[1]) >= 0 || pair[1].Cmp(pair[0]) <= 0 || pair[0].Equals(pair[1]) {
				t.Errorf("%s should sort before %s", pair[0].Serialize(), pair[1].Serialize())
			}
		}
	})
}
