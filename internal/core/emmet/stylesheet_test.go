package emmet

import (
	"testing"
)

// Ports of reference/emmet/test/stylesheet.ts.

// stylesheetConfig is the configuration of the tests of Emmet (test/stylesheet.ts): fields are
// written as ${1:name}, the fuzzy search takes any score, and four snippets are added.
func stylesheetConfig(t testing.TB) Config {
	t.Helper()
	cfg := newConfig(t, "css")
	cfg.Options.Field = SnippetField
	cfg.Options.StylesheetFuzzySearchMinScore = 0
	cfg.SetSnippets(map[string]string{
		"mten": "margin: 10px;",
		"fsz":  "font-size",
		"gt":   "grid-template: repeat(2,auto) / repeat(auto-fit, minmax(250px, 1fr))",
		"bxsh": "box-shadow: var(--bxsh-${1})",
	})
	return cfg
}

// plainStylesheetConfig is resolveConfig({type: 'stylesheet'}) of Emmet: fields are their placeholders.
func plainStylesheetConfig(t testing.TB) Config {
	t.Helper()
	return newConfig(t, "css")
}

type cssCase struct {
	abbr, want string
	note       string
}

func runExpandCases(t *testing.T, cfg Config, cases []cssCase) {
	t.Helper()
	for _, tc := range cases {
		name := tc.abbr
		if tc.note != "" {
			name += " (" + tc.note + ")"
		}
		t.Run(name, func(t *testing.T) {
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatalf("Expand(%q): %v", tc.abbr, err)
			}
			if got != tc.want {
				t.Errorf("Expand(%q) = %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestStylesheetScoring(t *testing.T) {
	score := func(a, b string) float64 { return scoreMatch(a, b, false) }
	partial := func(a, b string) float64 { return scoreMatch(a, b, true) }

	t.Run("compare scores", func(t *testing.T) {
		if score("aaa", "aaa") != 1 {
			t.Error("aaa and aaa")
		}
		if score("baa", "aaa") != 0 {
			t.Error("baa and aaa")
		}
		truths := []struct {
			name string
			ok   bool
		}{
			{"b does not match aaa", score("b", "aaa") == 0},
			{"a matches aaa", score("a", "aaa") != 0},
			{"a matches abc", score("a", "abc") != 0},
			{"ac matches abc", score("ac", "abc") != 0},
			{"a < aa for aaa", score("a", "aaa") < score("aa", "aaa")},
			{"ab: abc > acb", score("ab", "abc") > score("ab", "acb")},
			{"acronym bonus", score("ab", "a-b") > score("ab", "acb")},
		}
		for _, tr := range truths {
			if !tr.ok {
				t.Errorf("failed: %s", tr.name)
			}
		}
	})

	t.Run("pick padding or position", func(t *testing.T) {
		items := []string{"p", "pb", "pl", "pos", "pa", "oa", "soa", "pr", "pt"}
		pick := func(abbr string) string {
			best, bestScore := "", 0.0
			for _, item := range items {
				// stable sort by descending score: the first of the best is picked
				if s := partial(abbr, item); s != 0 && s > bestScore {
					best, bestScore = item, s
				}
			}
			return best
		}
		for abbr, want := range map[string]string{"p": "p", "poa": "pos"} {
			if got := pick(abbr); got != want {
				t.Errorf("pick(%q) = %q, want %q", abbr, got, want)
			}
		}
	})
}

func TestStylesheetKeywords(t *testing.T) {
	runExpandCases(t, stylesheetConfig(t), []cssCase{
		{abbr: "bd1-s", want: "border: 1px solid;"},
		{abbr: "dib", want: "display: inline-block;"},
		{abbr: "bxsz", want: "box-sizing: ${1:border-box};"},
		{abbr: "bxz", want: "box-sizing: ${1:border-box};"},
		{abbr: "bxzc", want: "box-sizing: content-box;"},
		{abbr: "fl", want: "float: ${1:left};"},
		{abbr: "fll", want: "float: left;"},

		{abbr: "pos", want: "position: ${1:relative};"},
		{abbr: "poa", want: "position: absolute;"},
		{abbr: "por", want: "position: relative;"},
		{abbr: "pof", want: "position: fixed;"},
		{abbr: "pos-a", want: "position: absolute;"},

		{abbr: "m", want: "margin: ${0};"},
		{abbr: "m0", want: "margin: 0;"},

		{abbr: "m0-a", want: "margin: 0 auto;", note: "auto is a global keyword"},
		{abbr: "m-a", want: "margin: auto;"},

		{abbr: "bg", want: "background: ${1:#000};"},

		{abbr: "bd", want: "border: ${1:1px} ${2:solid} ${3:#000};"},
		{abbr: "bd0-s#fc0", want: "border: 0 solid #fc0;"},
		{abbr: "bd0-dd#fc0", want: "border: 0 dot-dash #fc0;"},
		{abbr: "bd0-h#fc0", want: "border: 0 hidden #fc0;"},

		{abbr: "trf-trs", want: "transform: translate(${1:x}, ${2:y});"},

		{abbr: "c", want: "color: ${1:#000};", note: "emmet issue 610"},
		{abbr: "cr", want: "color: rgb(${1:0}, ${2:0}, ${3:0});"},
		{abbr: "cra", want: "color: rgba(${1:0}, ${2:0}, ${3:0}, ${4:.5});"},

		{abbr: "gtc", want: "grid-template-columns: repeat(${0});", note: "emmet issue 647"},
		{abbr: "gtr", want: "grid-template-rows: repeat(${0});"},

		{abbr: "lis:n", want: "list-style: none;"},
		{abbr: "list:n", want: "list-style-type: none;"},
		{abbr: "bdt:n", want: "border-top: none;"},
		{abbr: "bgi:n", want: "background-image: none;"},
		{abbr: "q:n", want: "quotes: none;"},

		{abbr: "bg:n", want: "background: none;", note: "emmet issue 628"},
		{abbr: "d:c", want: "display: contents;", note: "emmet issue 691"},

		{abbr: "bxsh", want: "box-shadow: var(--bxsh-${1});", note: "custom properties, emmet issue 692"},
	})
}

func TestStylesheetNumeric(t *testing.T) {
	runExpandCases(t, stylesheetConfig(t), []cssCase{
		{abbr: "p0", want: "padding: 0;", note: "No unit for 0"},
		{abbr: "p10", want: "padding: 10px;", note: "`px` unit for integers"},
		{abbr: "p.4", want: "padding: 0.4em;", note: "`em` for floats"},
		{abbr: "fz10", want: "font-size: 10px;", note: "`px` for integers"},
		{abbr: "fz1.", want: "font-size: 1em;", note: "`em` for explicit float"},
		{abbr: "p10p", want: "padding: 10%;", note: "unit alias"},
		{abbr: "z10", want: "z-index: 10;", note: "Unitless property"},
		{abbr: "p10r", want: "padding: 10rem;", note: "unit alias"},
		{abbr: "mten", want: "margin: 10px;", note: "Ignore terminating `;` in snippet"},

		{abbr: "fz", want: "font-size: ${0};", note: "vscode issue 59951"},
		{abbr: "fz12", want: "font-size: 12px;"},
		{abbr: "fsz", want: "font-size: ${0};"},
		{abbr: "fsz12", want: "font-size: 12px;"},
		{abbr: "fs", want: "font-style: ${1:italic};"},

		{abbr: "us", want: "user-select: none;", note: "emmet issue 558"},

		{abbr: "opa1", want: "opacity: 1;", note: "Unitless property, vscode issue 105697"},
		{abbr: "opa.1", want: "opacity: 0.1;", note: "Unitless property"},
		{abbr: "opa.a", want: "opacity: .a;", note: "Unitless property"},
	})
}

func TestStylesheetNumericWithFormatOptions(t *testing.T) {
	cfg := plainStylesheetConfig(t)
	cfg.Options.StylesheetIntUnit = "pt"
	cfg.Options.StylesheetFloatUnit = "vh"
	cfg.Options.StylesheetUnitAliases = map[string]string{"e": "em", "p": "%", "x": "ex", "r": " / @rem"}
	runExpandCases(t, cfg, []cssCase{
		{abbr: "p0", want: "padding: 0;", note: "No unit for 0"},
		{abbr: "p10", want: "padding: 10pt;", note: "`pt` unit for integers"},
		{abbr: "p.4", want: "padding: 0.4vh;", note: "`vh` for floats"},
		{abbr: "p10p", want: "padding: 10%;", note: "unit alias"},
		{abbr: "z10", want: "z-index: 10;", note: "Unitless property"},
		{abbr: "p10r", want: "padding: 10 / @rem;", note: "unit alias"},
	})
}

func TestStylesheetImportantColorSnippets(t *testing.T) {
	cfg := stylesheetConfig(t)
	t.Run("important", func(t *testing.T) {
		runExpandCases(t, cfg, []cssCase{
			{abbr: "!", want: "!important"},
			{abbr: "p!", want: "padding: ${0} !important;"},
			{abbr: "p0!", want: "padding: 0 !important;"},
		})
	})
	t.Run("color", func(t *testing.T) {
		runExpandCases(t, cfg, []cssCase{
			{abbr: "c", want: "color: ${1:#000};"},
			{abbr: "c#", want: "color: #000;"},
			{abbr: "c#f.5", want: "color: rgba(255, 255, 255, 0.5);"},
			{abbr: "c#f.5!", want: "color: rgba(255, 255, 255, 0.5) !important;"},
			{abbr: "bgc", want: "background-color: ${1:#fff};"},
		})
	})
	t.Run("snippets", func(t *testing.T) {
		runExpandCases(t, cfg, []cssCase{
			{abbr: "@", want: "@media ${1:screen} {\n\t${0}\n}"},
			{abbr: "@k-name", want: "@keyframes name {\n\t${2}\n}", note: "Insert value into snippet fields"},
			{abbr: "@k-name10", want: "@keyframes name {\n\t10\n}"},
			{abbr: "gt", want: "grid-template: repeat(2, auto) / repeat(auto-fit, minmax(250px, 1fr));"},
		})
	})
	t.Run("multiple properties", func(t *testing.T) {
		runExpandCases(t, cfg, []cssCase{
			{abbr: "p10+m10-20", want: "padding: 10px;\nmargin: 10px 20px;"},
			{abbr: "p+bd", want: "padding: ${0};\nborder: ${1:1px} ${2:solid} ${3:#000};"},
		})
	})
	t.Run("functions", func(t *testing.T) {
		runExpandCases(t, cfg, []cssCase{
			{abbr: "trf-s(2)", want: "transform: scale(2, ${2:y});"},
			{abbr: "trf-s(2, 3)", want: "transform: scale(2, 3);"},
		})
	})
	t.Run("case insensitive matches", func(t *testing.T) {
		runExpandCases(t, cfg, []cssCase{{abbr: "trf:rx", want: "transform: rotateX(${1:angle});"}})
	})
	t.Run("gradient resolver", func(t *testing.T) {
		runExpandCases(t, cfg, []cssCase{
			{abbr: "lg", want: "background-image: linear-gradient(${0});"},
			{abbr: "lg(to right, #0, #f00.5)", want: "background-image: linear-gradient(to right, #000, rgba(255, 0, 0, 0.5));"},
		})
	})
	t.Run("unmatched abbreviation", func(t *testing.T) {
		runExpandCases(t, cfg, []cssCase{{abbr: "auto", want: "auto: ${0};"}})
	})
}

func TestStylesheetCSSInJS(t *testing.T) {
	cfg := plainStylesheetConfig(t)
	cfg.Options.StylesheetJSON = true
	cfg.Options.StylesheetBetween = ": "
	runExpandCases(t, cfg, []cssCase{
		{abbr: "p10+mt10-20", want: "padding: 10,\nmarginTop: '10px 20px',"},
		{abbr: "bgc", want: "backgroundColor: '#fff',"},
	})
	cfg.Options.StylesheetJSONDoubleQuotes = true
	runExpandCases(t, cfg, []cssCase{{abbr: "bgc", want: `backgroundColor: "#fff",`, note: "Emetos: double quotes"}})
}

func TestStylesheetContext(t *testing.T) {
	t.Run("resolve context value", func(t *testing.T) {
		cfg := plainStylesheetConfig(t)
		cfg.Context = &Context{Name: "align-content"}
		runExpandCases(t, cfg, []cssCase{
			{abbr: "s", want: "start"},
			{abbr: "a", want: "auto"},
		})
	})

	t.Run("limit snippets by scope", func(t *testing.T) {
		snippets := map[string]string{
			"mten":            "margin: 10px;",
			"fsz":             "font-size",
			"myCenterAwesome": "body {\n\tdisplay: grid;\n}",
		}
		section := plainStylesheetConfig(t)
		section.Context = &Context{Name: ScopeSection}
		section.SetSnippets(snippets)
		property := plainStylesheetConfig(t)
		property.Context = &Context{Name: ScopeProperty}
		property.SetSnippets(snippets)

		runExpandCases(t, section, []cssCase{
			{abbr: "m", want: "body {\n\tdisplay: grid;\n}"},
			{abbr: "b", want: ""},
		})
		runExpandCases(t, property, []cssCase{{abbr: "m", want: "margin: ;"}})
	})
}

func TestStylesheetLogicalProperties(t *testing.T) {
	var cases []cssCase
	for _, c := range [][2]string{
		{"bbs", "border-block-start: ${0};"},
		{`bbe"`, `border-block-end: "";`},
		{"bis", "border-inline-start: ${0};"},
		{"bie", "border-inline-end: ${0};"},
		{"bs", "block-size: ${0};"},
		{"is", "inline-size: ${0};"},
		{"mbs", "margin-block-start: ${0};"},
		{"mbe", "margin-block-end: ${0};"},
		{"mis", "margin-inline-start: ${0};"},
		{"mie", "margin-inline-end: ${0};"},
		{"pbs", "padding-block-start: ${0};"},
		{"pbe", "padding-block-end: ${0};"},
		{"pis", "padding-inline-start: ${0};"},
		{"pie", "padding-inline-end: ${0};"},
		{"spbs", "scroll-padding-block-start: ${0};"},
		{"spbe", "scroll-padding-block-end: ${0};"},
		{"spis", "scroll-padding-inline-start: ${0};"},
		{"spie", "scroll-padding-inline-end: ${0};"},
	} {
		cases = append(cases, cssCase{abbr: c[0], want: c[1]})
	}
	runExpandCases(t, stylesheetConfig(t), cases)
}

func TestStylesheetStrictMatch(t *testing.T) {
	runExpandCases(t, stylesheetConfig(t), []cssCase{{abbr: "max-inline-size:", want: "max: inline size;"}})
	strict := plainStylesheetConfig(t)
	strict.Options.StylesheetStrictMatch = true
	runExpandCases(t, strict, []cssCase{{abbr: "max-inline-size:", want: ""}})
}

// The syntaxes of Emmet that change the output of a property.
func TestStylesheetSyntaxes(t *testing.T) {
	for _, tc := range []struct {
		syntax, abbr, want string
	}{
		{"css", "p10", "padding: 10px;"},
		{"scss", "p10", "padding: 10px;"},
		{"less", "p10", "padding: 10px;"},
		{"sss", "p10", "padding: 10px;"},
		{"sass", "p10", "padding: 10px"},
		{"stylus", "p10", "padding 10px"},
		{"sass", "p10+m10", "padding: 10px\nmargin: 10px"},
		{"stylus", "c#f!", "color #fff !important"},
	} {
		t.Run(tc.syntax+" "+tc.abbr, func(t *testing.T) {
			cfg := newConfig(t, tc.syntax)
			if cfg.Type != TypeStylesheet {
				t.Fatalf("type %q", cfg.Type)
			}
			got, err := Expand(tc.abbr, cfg)
			if err != nil || got != tc.want {
				t.Errorf("Expand(%q) = %q, %v; want %q", tc.abbr, got, err, tc.want)
			}
		})
	}
}

// An indented output (a base indentation and an indentation of spaces) for the properties and for
// the raw snippets, which keep their tabs; the text is escaped for a snippet.
func TestStylesheetOutputOptions(t *testing.T) {
	cfg := newConfig(t, "css")
	cfg.SetOutput("  ", "\r\n", "    ")
	cfg.Options.Field = SnippetField
	cfg.Options.Text = SnippetText
	runExpandCases(t, cfg, []cssCase{
		{abbr: "p10+m10", want: "padding: 10px;\r\n    margin: 10px;"},
		{abbr: "@m", want: "@media ${1:screen} {\r\n    \t${0}\r\n    }"},
		{abbr: "w$a-b", want: `width: \$a-b;`, note: "a $ of the text is escaped"},
		{abbr: "bg$x+m$y", want: "background: \\$x;\r\n    margin: \\$y;"},
	})

	cfg.Options.Format = false
	runExpandCases(t, cfg, []cssCase{{abbr: "p10+m10", want: "padding: 10px;margin: 10px;", note: "no format"}})
}
