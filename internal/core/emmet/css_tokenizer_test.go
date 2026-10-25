package emmet

import (
	"strconv"
	"strings"
	"testing"
)

// Ports of reference/emmet/packages/css-abbreviation/test/tokenizer.ts and parser.ts. A token is
// written in a short form: the type, its data, and its place (start-end), the way the expected values
// of Emmet's tests list them. A field of Emmet without an index is written idx=-.

func summarizeCSSToken(t *CSSToken) string {
	pos := " " + strconv.Itoa(t.Start) + "-" + strconv.Itoa(t.End)
	switch t.Type {
	case CSSLiteral:
		return "Literal " + strconv.Quote(t.Value) + pos
	case CSSNumber:
		return "Number " + jsNumber(t.Number) + " " + strconv.Quote(t.RawValue) + " " + strconv.Quote(t.Unit) + pos
	case CSSOperator:
		return "Operator " + string(t.Operator) + pos
	case CSSColor:
		return "Color " + strconv.Itoa(t.R) + " " + strconv.Itoa(t.G) + " " + strconv.Itoa(t.B) + " " + jsNumber(t.A) + " " + strconv.Quote(t.Raw) + pos
	case CSSBracket:
		if t.Open {
			return "Bracket open" + pos
		}
		return "Bracket close" + pos
	case CSSWhiteSpace:
		return "WhiteSpace" + pos
	case CSSString:
		quote := "double"
		if t.Single {
			quote = "single"
		}
		return "String " + strconv.Quote(t.Value) + " " + quote + pos
	case CSSField:
		idx := "-"
		if t.HasIndex {
			idx = strconv.Itoa(t.Index)
		}
		return "Field idx=" + idx + " " + strconv.Quote(t.Name) + pos
	case CSSCustomProperty:
		return "CustomProperty " + strconv.Quote(t.Value) + pos
	}
	return "?"
}

func summarizeCSSTokens(tokens []*CSSToken) []string {
	out := make([]string, len(tokens))
	for i, tok := range tokens {
		out[i] = summarizeCSSToken(tok)
	}
	return out
}

func cLit(v string, s, e int) string {
	return "Literal " + strconv.Quote(v) + " " + itoa(s) + "-" + itoa(e)
}
func cNum(v, raw, unit string, s, e int) string {
	return "Number " + v + " " + strconv.Quote(raw) + " " + strconv.Quote(unit) + " " + itoa(s) + "-" + itoa(e)
}
func cOp(o string, s, e int) string { return "Operator " + o + " " + itoa(s) + "-" + itoa(e) }
func cClr(r, g, b, a, raw string, s, e int) string {
	return "Color " + r + " " + g + " " + b + " " + a + " " + strconv.Quote(raw) + " " + itoa(s) + "-" + itoa(e)
}

func TestCSSTokenizer(t *testing.T) {
	cases := []struct {
		group, abbr string
		want        []string
	}{
		// numeric values
		{"numeric", "p10", []string{cLit("p", 0, 1), cNum("10", "10", "", 1, 3)}},
		{"numeric", "p-10", []string{cLit("p", 0, 1), cNum("-10", "-10", "", 1, 4)}},
		{"numeric", "p-10-", []string{cLit("p", 0, 1), cNum("-10", "-10", "", 1, 4), cOp("-", 4, 5)}},
		{"numeric", "p-10-20", []string{cLit("p", 0, 1), cNum("-10", "-10", "", 1, 4), cOp("-", 4, 5), cNum("20", "20", "", 5, 7)}},
		{"numeric", "p-10--20", []string{cLit("p", 0, 1), cNum("-10", "-10", "", 1, 4), cOp("-", 4, 5), cNum("-20", "-20", "", 5, 8)}},
		{"numeric", "p-10-20--30", []string{
			cLit("p", 0, 1), cNum("-10", "-10", "", 1, 4), cOp("-", 4, 5), cNum("20", "20", "", 5, 7), cOp("-", 7, 8), cNum("-30", "-30", "", 8, 11)}},
		{"numeric", "p-10p-20--30", []string{
			cLit("p", 0, 1), cNum("-10", "-10", "p", 1, 5), cNum("-20", "-20", "", 5, 8), cOp("-", 8, 9), cNum("-30", "-30", "", 9, 12)}},
		{"numeric", "p-10%-20--30", []string{
			cLit("p", 0, 1), cNum("-10", "-10", "%", 1, 5), cNum("-20", "-20", "", 5, 8), cOp("-", 8, 9), cNum("-30", "-30", "", 9, 12)}},

		// float values
		{"float", "p.5", []string{cLit("p", 0, 1), cNum("0.5", ".5", "", 1, 3)}},
		{"float", "p-.5", []string{cLit("p", 0, 1), cNum("-0.5", "-.5", "", 1, 4)}},
		{"float", "p.1.2.3", []string{cLit("p", 0, 1), cNum("0.1", ".1", "", 1, 3), cNum("0.2", ".2", "", 3, 5), cNum("0.3", ".3", "", 5, 7)}},
		{"float", "p.1-.2.3", []string{cLit("p", 0, 1), cNum("0.1", ".1", "", 1, 3), cOp("-", 3, 4), cNum("0.2", ".2", "", 4, 6), cNum("0.3", ".3", "", 6, 8)}},
		{"float", "p.1--.2.3", []string{
			cLit("p", 0, 1), cNum("0.1", ".1", "", 1, 3), cOp("-", 3, 4), cNum("-0.2", "-.2", "", 4, 7), cNum("0.3", ".3", "", 7, 9)}},
		{"float", "10", []string{cNum("10", "10", "", 0, 2)}},
		{"float", ".1", []string{cNum("0.1", ".1", "", 0, 2)}},

		// color values
		{"color", "c#", []string{cLit("c", 0, 1), cClr("0", "0", "0", "1", "", 1, 2)}},
		{"color", "c#1", []string{cLit("c", 0, 1), cClr("17", "17", "17", "1", "1", 1, 3)}},
		{"color", "c#.", []string{cLit("c", 0, 1), cClr("0", "0", "0", "1", ".", 1, 3)}},
		{"color", "c#f", []string{cLit("c", 0, 1), cClr("255", "255", "255", "1", "f", 1, 3)}},
		{"color", "c#a#b#c", []string{
			cLit("c", 0, 1), cClr("170", "170", "170", "1", "a", 1, 3), cClr("187", "187", "187", "1", "b", 3, 5), cClr("204", "204", "204", "1", "c", 5, 7)}},
		{"color", "c#af", []string{cLit("c", 0, 1), cClr("175", "175", "175", "1", "af", 1, 4)}},
		{"color", "c#fc0", []string{cLit("c", 0, 1), cClr("255", "204", "0", "1", "fc0", 1, 5)}},
		{"color", "c#11.5", []string{cLit("c", 0, 1), cClr("17", "17", "17", "0.5", "11.5", 1, 6)}},
		{"color", "c#.99", []string{cLit("c", 0, 1), cClr("0", "0", "0", "0.99", ".99", 1, 5)}},
		{"color", "c#t", []string{cLit("c", 0, 1), cClr("0", "0", "0", "0", "t", 1, 3)}},
		{"color", "c#${fff}", []string{cLit("c", 0, 1), cLit("#", 1, 2), "Field idx=- \"fff\" 2-8"}},

		// keywords
		{"keywords", "m:a", []string{cLit("m", 0, 1), cOp(":", 1, 2), cLit("a", 2, 3)}},
		{"keywords", "m-a", []string{cLit("m", 0, 1), cOp("-", 1, 2), cLit("a", 2, 3)}},
		{"keywords", "m-abc", []string{cLit("m", 0, 1), cOp("-", 1, 2), cLit("abc", 2, 5)}},
		{"keywords", "m-a0", []string{cLit("m", 0, 1), cOp("-", 1, 2), cLit("a", 2, 3), cNum("0", "0", "", 3, 4)}},
		{"keywords", "m-a0-a", []string{cLit("m", 0, 1), cOp("-", 1, 2), cLit("a", 2, 3), cNum("0", "0", "", 3, 4), cOp("-", 4, 5), cLit("a", 5, 6)}},

		// arguments
		{"arguments", `lg(top, "red, black", rgb(0, 0, 0) 10%)`, []string{
			cLit("lg", 0, 2), "Bracket open 2-3", cLit("top", 3, 6), cOp(",", 6, 7), "WhiteSpace 7-8",
			`String "red, black" double 8-20`, cOp(",", 20, 21), "WhiteSpace 21-22",
			cLit("rgb", 22, 25), "Bracket open 25-26", cNum("0", "0", "", 26, 27), cOp(",", 27, 28), "WhiteSpace 28-29",
			cNum("0", "0", "", 29, 30), cOp(",", 30, 31), "WhiteSpace 31-32", cNum("0", "0", "", 32, 33),
			"Bracket close 33-34", "WhiteSpace 34-35", cNum("10", "10", "%", 35, 38), "Bracket close 38-39"}},

		// important
		{"important", "!", []string{cOp("!", 0, 1)}},
		{"important", "p!", []string{cLit("p", 0, 1), cOp("!", 1, 2)}},
		{"important", "p10!", []string{cLit("p", 0, 1), cNum("10", "10", "", 1, 3), cOp("!", 3, 4)}},

		// mixed
		{"mixed", "bd1-s#fc0", []string{cLit("bd", 0, 2), cNum("1", "1", "", 2, 3), cOp("-", 3, 4), cLit("s", 4, 5), cClr("255", "204", "0", "1", "fc0", 5, 9)}},
		{"mixed", "bd#fc0-1", []string{cLit("bd", 0, 2), cClr("255", "204", "0", "1", "fc0", 2, 6), cOp("-", 6, 7), cNum("1", "1", "", 7, 8)}},
		{"mixed", "p0+m0", []string{cLit("p", 0, 1), cNum("0", "0", "", 1, 2), cOp("+", 2, 3), cLit("m", 3, 4), cNum("0", "0", "", 4, 5)}},
		{"mixed", "p0!+m0!", []string{
			cLit("p", 0, 1), cNum("0", "0", "", 1, 2), cOp("!", 2, 3), cOp("+", 3, 4), cLit("m", 4, 5), cNum("0", "0", "", 5, 6), cOp("!", 6, 7)}},
		{"mixed", "${2:0}%", []string{`Field idx=2 "0" 0-6`, cLit("%", 6, 7)}},
		{"mixed", ".${1:5}", []string{cLit(".", 0, 1), `Field idx=1 "5" 1-7`}},

		// embedded variables
		{"variables", "foo$bar", []string{cLit("foo", 0, 3), cLit("$bar", 3, 7)}},
		{"variables", "foo$bar-2", []string{cLit("foo", 0, 3), cLit("$bar-2", 3, 9)}},
		{"variables", "foo$bar@bam", []string{cLit("foo", 0, 3), cLit("$bar", 3, 7), cLit("@bam", 7, 11)}},
		{"variables", "@k10", []string{cLit("@k", 0, 2), cNum("10", "10", "", 2, 4)}},
	}
	for _, tc := range cases {
		t.Run(tc.group+"/"+tc.abbr, func(t *testing.T) {
			tokens, err := TokenizeCSS(tc.abbr, false)
			if err != nil {
				t.Fatal(err)
			}
			got := summarizeCSSTokens(tokens)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("tokens of %q:\n%s\nwant:\n%s", tc.abbr, strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// stringifyCSSProperty is the stringify of the tests of Emmet (packages/css-abbreviation/test/assets/stringify.ts).
func stringifyCSSProperty(t testing.TB, p *CSSProperty) string {
	t.Helper()
	name := p.Name
	if name == "" {
		name = "?"
	}
	values := make([]string, len(p.Value))
	for i, v := range p.Value {
		values[i] = stringifyCSSValue(t, v)
	}
	important := ""
	if p.Important {
		important = " !important"
	}
	return name + ": " + strings.Join(values, ", ") + important + ";"
}

func stringifyCSSValue(t testing.TB, v *CSSValue) string {
	t.Helper()
	parts := make([]string, len(v.Value))
	for i, tok := range v.Value {
		switch tok.Type {
		case CSSColor:
			switch {
			case tok.R == 0 && tok.G == 0 && tok.B == 0 && tok.A == 0:
				parts[i] = "transparent"
			case tok.A == 1:
				parts[i] = "#" + hex2(tok.R) + hex2(tok.G) + hex2(tok.B)
			default:
				parts[i] = "rgba(" + itoa(tok.R) + ", " + itoa(tok.G) + ", " + itoa(tok.B) + ", " + jsNumber(tok.A) + ")"
			}
		case CSSNumber:
			parts[i] = jsNumber(tok.Number) + tok.Unit
		case CSSString:
			parts[i] = `"` + tok.Value + `"`
		case CSSLiteral:
			parts[i] = tok.Value
		case CSSFunction:
			args := make([]string, len(tok.Arguments))
			for j, a := range tok.Arguments {
				args[j] = stringifyCSSValue(t, a)
			}
			parts[i] = tok.Name + "(" + strings.Join(args, ", ") + ")"
		default:
			t.Fatalf("Unexpected token %v", tok.Type)
		}
	}
	return strings.Join(parts, " ")
}

func expandCSSProps(t testing.TB, abbr string) string {
	t.Helper()
	props, err := ParseCSS(abbr, false)
	if err != nil {
		t.Fatalf("ParseCSS(%q): %v", abbr, err)
	}
	var sb strings.Builder
	for _, p := range props {
		sb.WriteString(stringifyCSSProperty(t, p))
	}
	return sb.String()
}

func TestCSSParser(t *testing.T) {
	cases := []struct{ group, abbr, want string }{
		{"basic", "p10!", "p: 10 !important;"},
		{"basic", "p-10-20", "p: -10 20;"},
		{"basic", "p-10%-20--30", "p: -10% -20 -30;"},
		{"basic", "p.5", "p: 0.5;"},
		{"basic", "p-.5", "p: -0.5;"},
		{"basic", "p.1.2.3", "p: 0.1 0.2 0.3;"},
		{"basic", "p.1-.2.3", "p: 0.1 0.2 0.3;"},
		{"basic", "10", "?: 10;"},
		{"basic", ".1", "?: 0.1;"},
		{"basic", "lh1.", "lh: 1;"},

		{"color", "c#", "c: #000000;"},
		{"color", "c#1", "c: #111111;"},
		{"color", "c#f", "c: #ffffff;"},
		{"color", "c#a#b#c", "c: #aaaaaa #bbbbbb #cccccc;"},
		{"color", "c#af", "c: #afafaf;"},
		{"color", "c#fc0", "c: #ffcc00;"},
		{"color", "c#11.5", "c: rgba(17, 17, 17, 0.5);"},
		{"color", "c#.99", "c: rgba(0, 0, 0, 0.99);"},
		{"color", "c#t", "c: transparent;"},

		{"keywords", "m:a", "m: a;"},
		{"keywords", "m-a", "m: a;"},
		{"keywords", "m-abc", "m: abc;"},
		{"keywords", "m-a0", "m: a 0;"},
		{"keywords", "m-a0-a", "m: a 0 a;"},

		{"functions", `bg-lg(top,   "red, black",rgb(0, 0, 0) 10%)`, `bg: lg(top, "red, black", rgb(0, 0, 0) 10%);`},
		{"functions", `lg(top,   "red, black",rgb(0, 0, 0) 10%)`, `?: lg(top, "red, black", rgb(0, 0, 0) 10%);`},

		{"mixed", "bd1-s#fc0", "bd: 1 s #ffcc00;"},
		{"mixed", "bd#fc0-1", "bd: #ffcc00 1;"},
		{"mixed", "p0+m0", "p: 0;m: 0;"},
		{"mixed", "p0!+m0!", "p: 0 !important;m: 0 !important;"},

		{"variables", "foo$bar", "foo: $bar;"},
		{"variables", "foo$bar-2", "foo: $bar-2;"},
		{"variables", "foo$bar@bam", "foo: $bar @bam;"},
	}
	for _, tc := range cases {
		t.Run(tc.group+"/"+tc.abbr, func(t *testing.T) {
			if got := expandCSSProps(t, tc.abbr); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCSSParserValue(t *testing.T) {
	// parse value
	props, err := ParseCSS("${1:foo} ${2:bar}, baz", true)
	if err != nil {
		t.Fatal(err)
	}
	prop := props[0]
	if prop.Name != "" || len(prop.Value) != 2 || len(prop.Value[0].Value) != 2 || prop.Value[0].Value[0].Type != CSSField {
		t.Errorf("fields: %+v", prop)
	}

	props, err = ParseCSS("scale3d(${1:x}, ${2:y}, ${3:z})", true)
	if err != nil {
		t.Fatal(err)
	}
	prop = props[0]
	if len(prop.Value) != 1 || len(prop.Value[0].Value) != 1 {
		t.Fatalf("function: %+v", prop)
	}
	if fn := prop.Value[0].Value[0]; fn.Type != CSSFunction || fn.Name != "scale3d" {
		t.Errorf("function: %+v", fn)
	}

	props, err = ParseCSS("repeat(2,auto) / repeat(auto-fit, minmax(250px, 1fr))", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(props[0].Value) != 1 {
		t.Errorf("grid: %d values", len(props[0].Value))
	}
}

func TestCSSParserErrors(t *testing.T) {
	_, err := ParseCSS("p10 ", false)
	if err == nil || !strings.Contains(err.Error(), "Unexpected token") {
		t.Fatalf("p10 with a space: %v", err)
	}
	wantRoute(t, err, "ParseCSS->ParseCSSTokens.CSSUnexpectedToken")
}
