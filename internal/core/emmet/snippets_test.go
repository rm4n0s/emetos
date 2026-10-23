package emmet

import (
	"slices"
	"testing"
)

// Ported from Emmet: test/snippets.ts

func TestSnippets(t *testing.T) {
	// every snippet is an abbreviation that parses
	for _, file := range []string{"html", "xsl", "pug"} {
		snippets, err := readStringMap("assets/" + file + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if len(snippets) == 0 {
			t.Errorf("%s.json has no snippets", file)
		}
		for name, abbr := range snippets {
			if _, err := ParseAbbreviation(abbr, ParserOptions{}); err != nil {
				t.Errorf("%s.json: %q (%q): %v", file, name, abbr, err)
			}
		}
	}

	t.Run("HTML", func(t *testing.T) {
		cfg := newConfig(t, "html")
		got, err := Expand("fset>input:c", cfg)
		if err != nil {
			t.Fatal(err)
		}
		if want := `<fieldset><input type="checkbox" name="" id=""></fieldset>`; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("invalid snippets", func(t *testing.T) {
		cfg := newConfig(t, "html")
		cfg.SetSnippets(map[string]string{"invalid": "invalid snippet", "valid": "button"})
		var warnings []string
		cfg.Warn = func(message string, err error) { warnings = append(warnings, message) }
		got, err := Expand("invalid+valid", cfg)
		if err != nil {
			t.Fatal(err)
		}
		if want := "<invalid></invalid>\n<button></button>"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if !slices.Equal(warnings, []string{`Unable to parse "invalid snippet" snippet`}) {
			t.Errorf("warnings: %q", warnings)
		}
	})
}

// Not in Emmet's tests: the names of snippets with several names, and the defaults per syntax.
func TestSnippetNames(t *testing.T) {
	cases := []struct {
		syntax, abbr, want string
	}{
		{"html", "acr", `<acronym title=""></acronym>`},
		{"html", "acronym", `<acronym title=""></acronym>`},
		{"html", "link:mf", `<link rel="manifest" href="manifest.json">`},
		{"html", "link:manifest", `<link rel="manifest" href="manifest.json">`},
		{"xsl", "tm", `<xsl:template match="" mode=""></xsl:template>`},
		{"xsl", "tmatch", `<xsl:template match="" mode=""></xsl:template>`},
		{"html", "tm", `<tm></tm>`},
		{"xml", "br/", `<br/>`},
	}
	for _, tc := range cases {
		t.Run(tc.syntax+" "+tc.abbr, func(t *testing.T) {
			got, err := Expand(tc.abbr, newConfig(t, tc.syntax))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSplitSnippetKeys(t *testing.T) {
	got := splitSnippetKeys(map[string]string{"a|b": "x", "c": "y"})
	want := map[string]string{"a": "x", "b": "x", "c": "y"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q = %q, want %q", k, got[k], v)
		}
	}
}
