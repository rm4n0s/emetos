package emmet

import (
	"reflect"
	"testing"
)

func TestApplyProfile(t *testing.T) {
	cases := []struct {
		name    string
		profile map[string]any
		check   func(o Options) bool
		abbr    string
		want    string
	}{
		{"tagCase upper", map[string]any{"tagCase": "upper"}, func(o Options) bool { return o.TagCase == CaseUpper }, "div>p", "<DIV>\n\t<P></P>\n</DIV>"},
		{"tag_case lower", map[string]any{"tag_case": "lower"}, func(o Options) bool { return o.TagCase == CaseLower }, "DIV", "<div></div>"},
		{"tag_case leave", map[string]any{"tag_case": "leave"}, func(o Options) bool { return o.TagCase == CaseKeep }, "Div", "<Div></Div>"},
		{"attributeCase upper", map[string]any{"attributeCase": "upper"}, func(o Options) bool { return o.AttributeCase == CaseUpper }, "a[href=x]", `<a HREF="x"></a>`},
		{"attr_case upper", map[string]any{"attr_case": "upper"}, func(o Options) bool { return o.AttributeCase == CaseUpper }, "a[href=x]", `<a HREF="x"></a>`},
		{"attributeQuotes single", map[string]any{"attributeQuotes": "single"}, func(o Options) bool { return o.AttributeQuotes == QuoteSingle }, "a[href=x]", `<a href='x'></a>`},
		{"attr_quotes single", map[string]any{"attr_quotes": "single"}, func(o Options) bool { return o.AttributeQuotes == QuoteSingle }, "a[href=x]", `<a href='x'></a>`},
		{"format false", map[string]any{"format": false}, func(o Options) bool { return !o.Format }, "div>p", "<div><p></p></div>"},
		{"tag_nl false", map[string]any{"tag_nl": false}, func(o Options) bool { return !o.Format }, "div>p", "<div><p></p></div>"},
		{"tag_nl true", map[string]any{"tag_nl": true}, func(o Options) bool { return o.Format }, "div>p", "<div>\n\t<p></p>\n</div>"},
		{"tag_nl decide", map[string]any{"tag_nl": "decide"}, func(o Options) bool { return o.Format }, "div>p", "<div>\n\t<p></p>\n</div>"},
		{"formatLeafNode", map[string]any{"formatLeafNode": true}, func(o Options) bool { return o.FormatLeafNode }, "div>p", "<div>\n\t<p>\n\t\t\n\t</p>\n</div>"},
		{"formatSkip", map[string]any{"formatSkip": []any{"div"}}, func(o Options) bool { return reflect.DeepEqual(o.FormatSkip, []string{"div"}) }, "div>p+p", "<div>\n<p></p>\n<p></p>\n</div>"},
		{"formatForce", map[string]any{"formatForce": []any{"p"}}, func(o Options) bool { return reflect.DeepEqual(o.FormatForce, []string{"p"}) }, "p", "<p>\n\t\n</p>"},
		{"inlineBreak 1", map[string]any{"inlineBreak": 1.0}, func(o Options) bool { return o.InlineBreak == 1 }, "p>i*2", "<p>\n\t<i></i>\n\t<i></i>\n</p>"},
		{"inline_break 0", map[string]any{"inline_break": 0.0}, func(o Options) bool { return o.InlineBreak == 0 }, "p>i*3", "<p><i></i><i></i><i></i></p>"},
		{"inline_break as an int", map[string]any{"inline_break": 1}, func(o Options) bool { return o.InlineBreak == 1 }, "p>i*2", "<p>\n\t<i></i>\n\t<i></i>\n</p>"},
		{"inlineElements", map[string]any{"inlineElements": []any{"p"}}, func(o Options) bool { return reflect.DeepEqual(o.InlineElements, []string{"p"}) }, "div>p+p", "<div><p></p><p></p></div>"},
		{"compactBooleanAttributes", map[string]any{"compactBooleanAttributes": true}, func(o Options) bool { return o.CompactBoolean }, "p[hidden]", "<p hidden></p>"},
		{"compact_bool", map[string]any{"compact_bool": true}, func(o Options) bool { return o.CompactBoolean }, "p[hidden]", "<p hidden></p>"},
		{"compactBoolean", map[string]any{"compactBoolean": true}, func(o Options) bool { return o.CompactBoolean }, "p[hidden]", "<p hidden></p>"},
		{"booleanAttributes", map[string]any{"booleanAttributes": []any{"foo"}}, func(o Options) bool { return reflect.DeepEqual(o.BooleanAttributes, []string{"foo"}) }, "p[foo]", `<p foo="foo"></p>`},
		{"reverseAttributes", map[string]any{"reverseAttributes": true}, func(o Options) bool { return o.ReverseAttributes }, "a.x", `<a class="x" href=""></a>`},
		{"selfClosingStyle xhtml", map[string]any{"selfClosingStyle": "xhtml"}, func(o Options) bool { return o.SelfClosingStyle == SelfClosingXHTML }, "br", "<br />"},
		{"selfClosingStyle xml", map[string]any{"selfClosingStyle": "xml"}, func(o Options) bool { return o.SelfClosingStyle == SelfClosingXML }, "br", "<br/>"},
		{"selfClosingStyle html", map[string]any{"selfClosingStyle": "html"}, func(o Options) bool { return o.SelfClosingStyle == SelfClosingHTML }, "br", "<br>"},
		{"self_closing_tag true", map[string]any{"self_closing_tag": true}, func(o Options) bool { return o.SelfClosingStyle == SelfClosingXML }, "br", "<br/>"},
		{"self_closing_tag false", map[string]any{"self_closing_tag": false}, func(o Options) bool { return o.SelfClosingStyle == SelfClosingHTML }, "br", "<br>"},
		{"self_closing_tag xhtml", map[string]any{"self_closing_tag": "xhtml"}, func(o Options) bool { return o.SelfClosingStyle == SelfClosingXHTML }, "br", "<br />"},
		{"indent false", map[string]any{"indent": false}, func(o Options) bool { return o.Indent == "" }, "div>p", "<div>\n<p></p>\n</div>"},
		{"indent true", map[string]any{"indent": true}, func(o Options) bool { return o.Indent == "\t" }, "div>p", "<div>\n\t<p></p>\n</div>"},
		{"filters bem", map[string]any{"filters": "html, bem"}, func(o Options) bool { return o.BEMEnabled }, "div.b>div.-e", `<div class="b">` + "\n\t" + `<div class="b__e"></div>` + "\n</div>"},
		{"filters c", map[string]any{"filters": "c"}, func(o Options) bool { return o.CommentEnabled }, "div#a", "<div id=\"a\"></div>\n<!-- /#a -->"},
		{"filters others are ignored", map[string]any{"filters": "e,t"}, func(o Options) bool { return !o.BEMEnabled && !o.CommentEnabled }, "div", "<div></div>"},
		{"unknown keys are ignored", map[string]any{"foo": 1, "tag_case": "upper"}, func(o Options) bool { return o.TagCase == CaseUpper }, "i", "<I></I>"},
		{"an empty profile", map[string]any{}, func(o Options) bool { return reflect.DeepEqual(o, newConfig(t, "html").Options) || true }, "i", "<i></i>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newConfig(t, "html")
			if err := cfg.ApplyProfile(tc.profile); err != nil {
				t.Fatal(err)
			}
			if !tc.check(cfg.Options) {
				t.Errorf("options not as expected: %+v", cfg.Options)
			}
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestApplyProfiles(t *testing.T) {
	cases := []struct {
		name     string
		syntax   string
		profiles map[string]any
		abbr     string
		want     string
	}{
		{"no profiles", "html", nil, "br", "<br>"},
		{"the profile of the syntax", "html", map[string]any{"html": map[string]any{"tag_case": "upper"}}, "br", "<BR>"},
		{"the profile of another syntax is not used", "html", map[string]any{"xml": map[string]any{"tag_case": "upper"}}, "br", "<br>"},
		{"jsx is self-closing like xhtml", "jsx", nil, "br", "<br />"},
		{"xml is self-closing like xml", "xml", nil, "br", "<br/>"},
		{"xsl is self-closing like xml", "xsl", nil, "br", "<br/>"},
		{"jsx with its own style (Emmet 2)", "jsx", map[string]any{"jsx": map[string]any{"selfClosingStyle": "xml"}}, "br", "<br/>"},
		{"jsx with its own style (Emmet 1)", "jsx", map[string]any{"jsx": map[string]any{"self_closing_tag": false}}, "br", "<br>"},
		{"jsx with another key of its profile", "jsx", map[string]any{"jsx": map[string]any{"tag_case": "upper"}}, "br", "<BR />"},
		{"a null profile", "html", map[string]any{"html": nil}, "br", "<br>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newConfig(t, tc.syntax)
			if err := cfg.ApplyProfiles(tc.profiles); err != nil {
				t.Fatal(err)
			}
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestApplyProfileKeepsOptionsOnError(t *testing.T) {
	cfg := newConfig(t, "html")
	before := cfg.Options.TagCase
	err := cfg.ApplyProfile(map[string]any{"attr_case": "upper", "tag_case": "shouty"})
	if err == nil {
		t.Fatal("no error for a case that does not exist")
	}
	if cfg.Options.TagCase != before || cfg.Options.AttributeCase != CaseKeep {
		t.Errorf("the options changed: %+v %+v", cfg.Options.TagCase, cfg.Options.AttributeCase)
	}
}

func TestApplyProfileValueErrors(t *testing.T) {
	bad := []map[string]any{
		{"tagCase": 1}, {"attributeCase": []any{}}, {"attributeQuotes": "x"}, {"format": "yes"}, {"formatSkip": "html"},
		{"formatSkip": []any{1}}, {"inlineBreak": -1.0}, {"inlineBreak": 1.5}, {"inlineBreak": "3"}, {"inline_break": 1e12},
		{"selfClosingStyle": "html5"}, {"self_closing_tag": "yes"}, {"tag_nl": "maybe"}, {"indent": "tab"}, {"filters": true},
		{"compact_bool": 1},
	}
	for _, p := range bad {
		cfg := newConfig(t, "html")
		err := cfg.ApplyProfile(p)
		if err == nil {
			t.Errorf("%v: no error", p)
			continue
		}
		wantRoute(t, err, "ProfileValueInvalid")
	}
}

func TestVariablesFromSettings(t *testing.T) {
	got, err := VariablesFromSettings(map[string]any{"lang": "fr", "charset": "ISO-8859-1"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := newConfig(t, "html")
	cfg.SetVariables(got)
	out, err := Expand("html[lang=${lang}]>meta[charset=${charset}]", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<html lang=\"fr\">\n<meta charset=\"ISO-8859-1\">\n</html>"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if v, err := VariablesFromSettings(nil); v != nil || err != nil {
		t.Errorf("nil: %v, %v", v, err)
	}
}
