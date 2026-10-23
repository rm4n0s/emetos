package emmet

import "testing"

// Ported from Emmet: test/output.ts

func TestOutputStream(t *testing.T) {
	opts := DefaultOptions()
	opts.BaseIndent = ">>"
	out := newOutputStream(&opts)

	type state struct {
		value                string
		line, column, offset int
	}
	check := func(step string, want state) {
		t.Helper()
		got := state{out.value(), out.line, out.column, out.offset}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", step, got, want)
		}
	}

	out.push("aaa")
	check("push", state{"aaa", 0, 3, 3})

	out.pushString("bbb")
	check("pushString", state{"aaabbb", 0, 6, 6})

	// text with line breaks
	out.pushString("ccc\nddd")
	check("pushString with a line break", state{"aaabbbccc\n>>ddd", 1, 5, 15})

	// a line break with indentation
	out.level++
	out.pushNewline(useLevel)
	check("pushNewline", state{"aaabbbccc\n>>ddd\n>>\t", 2, 3, 19})
}

func TestOutputProfile(t *testing.T) {
	t.Run("tag name", func(t *testing.T) {
		for _, tc := range []struct {
			style    StringCase
			in, want string
		}{
			{CaseKeep, "Foo", "Foo"}, {CaseKeep, "bAr", "bAr"},
			{CaseUpper, "Foo", "FOO"}, {CaseUpper, "bAr", "BAR"},
			{CaseLower, "Foo", "foo"}, {CaseLower, "bAr", "bar"},
		} {
			cfg := newConfig(t, "html")
			cfg.Options.TagCase = tc.style
			if got := tagName(tc.in, &cfg); got != tc.want {
				t.Errorf("tagName(%q) with %q: %q, want %q", tc.in, tc.style, got, tc.want)
			}
		}
	})
	t.Run("attribute name", func(t *testing.T) {
		for _, tc := range []struct {
			style    StringCase
			in, want string
		}{
			{CaseKeep, "Foo", "Foo"}, {CaseKeep, "bAr", "bAr"},
			{CaseUpper, "Foo", "FOO"}, {CaseUpper, "bAr", "BAR"},
			{CaseLower, "Foo", "foo"}, {CaseLower, "bAr", "bar"},
		} {
			cfg := newConfig(t, "html")
			cfg.Options.AttributeCase = tc.style
			if got := attrName(tc.in, &cfg); got != tc.want {
				t.Errorf("attrName(%q) with %q: %q, want %q", tc.in, tc.style, got, tc.want)
			}
		}
	})
	t.Run("self close", func(t *testing.T) {
		for style, want := range map[SelfClosingStyle]string{SelfClosingHTML: "", SelfClosingXHTML: " /", SelfClosingXML: "/"} {
			cfg := newConfig(t, "html")
			cfg.Options.SelfClosingStyle = style
			if got := selfClose(&cfg); got != want {
				t.Errorf("selfClose with %q: %q, want %q", style, got, want)
			}
		}
	})
	t.Run("inline elements", func(t *testing.T) {
		cfg := newConfig(t, "html")
		for name, want := range map[string]bool{"a": true, "b": true, "c": false, "A": true} {
			if got := isInlineName(name, &cfg); got != want {
				t.Errorf("isInline(%q) = %v, want %v", name, got, want)
			}
		}
	})
}

// Not in Emmet's tests: the hooks of the output.
func TestOutputHooks(t *testing.T) {
	cfg := newConfig(t, "html")
	var seen []string
	cfg.Options.Text = func(text string, offset, line, column int) string {
		seen = append(seen, text+"@"+itoa(offset)+","+itoa(line)+","+itoa(column))
		return text
	}
	cfg.Options.Field = func(index int, placeholder string, offset, _, _ int) string {
		return "<" + itoa(index) + placeholder + "@" + itoa(offset) + ">"
	}
	got, err := Expand("a[href=${1:x}]", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := `<a href="<2x@9>"><3@17></a>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(seen) == 0 || seen[0] != "<a@0,0,0" {
		t.Errorf("the text hook saw %v", seen)
	}
}
