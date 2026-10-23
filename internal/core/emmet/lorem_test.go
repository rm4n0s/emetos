package emmet

import (
	"regexp"
	"strings"
	"testing"
)

// Ported from Emmet: test/lorem.ts

func wordCount(s string) int { return len(strings.Split(s, " ")) }

func expandLorem(t *testing.T, abbr string, mod func(*Config)) string {
	t.Helper()
	cfg := newConfig(t, "html")
	if mod != nil {
		mod(&cfg)
	}
	out, err := Expand(abbr, cfg)
	if err != nil {
		t.Fatalf("%q: %v", abbr, err)
	}
	return out
}

// loremCheck is a check of the output of an abbreviation.
type loremCheck struct {
	abbr string
	// re must match the output (empty is no check).
	re string
	// words is the exact number of words (0 is no check), minWords and maxWords a range.
	words, minWords, maxWords int
}

func TestLoremSingle(t *testing.T) {
	cases := []loremCheck{
		{abbr: "lorem", re: `^Lorem,?\sipsum`, minWords: 21},
		{abbr: "lorem5", re: `^Lorem,?\sipsum`, words: 5},
		{abbr: "lorem5-10", re: `^Lorem,?\sipsum`, minWords: 5, maxWords: 10},
		{abbr: "loremru4", re: `^Далеко-далеко,?\sза,?\sсловесными`, words: 4},
		{abbr: "p>lorem", re: `^<p>Lorem,?\sipsum`},
		// https://github.com/emmetio/expand-abbreviation/issues/24
		{abbr: "(p)lorem2", re: `^<p></p>\nLorem,?\sipsum`},
		{abbr: "p(lorem10)", re: `^<p></p>\nLorem,?\sipsum`},
	}
	for _, tc := range cases {
		// with the real source of random numbers and with a fixed one
		for _, rnd := range []func() float64{nil, fixedRand()} {
			t.Run(tc.abbr, func(t *testing.T) {
				out := expandLorem(t, tc.abbr, func(c *Config) { c.Rand = rnd })
				if tc.re != "" && !regexp.MustCompile(tc.re).MatchString(out) {
					t.Errorf("%q: %q does not match %s", tc.abbr, out, tc.re)
				}
				if n := wordCount(out); tc.words != 0 && n != tc.words {
					t.Errorf("%q: %d words, want %d", tc.abbr, n, tc.words)
				}
				if n := wordCount(out); (tc.minWords != 0 && n < tc.minWords) || (tc.maxWords != 0 && n > tc.maxWords) {
					t.Errorf("%q: %d words, want %d to %d", tc.abbr, n, tc.minWords, tc.maxWords)
				}
			})
		}
	}
}

func TestLoremMultiple(t *testing.T) {
	t.Run("lorem6*3", func(t *testing.T) {
		out := expandLorem(t, "lorem6*3", nil)
		if !regexp.MustCompile(`^Lorem,?\sipsum`).MatchString(out) {
			t.Errorf("%q", out)
		}
		if n := len(strings.Split(out, "\n")); n != 3 {
			t.Errorf("%d lines, want 3: %q", n, out)
		}
	})
	t.Run("lorem6*2", func(t *testing.T) {
		out := expandLorem(t, "lorem6*2", nil)
		if !regexp.MustCompile(`^Lorem,?\sipsum`).MatchString(out) {
			t.Errorf("%q", out)
		}
		if n := len(strings.Split(out, "\n")); n != 2 {
			t.Errorf("%d lines, want 2: %q", n, out)
		}
	})
	t.Run("p*3>lorem", func(t *testing.T) {
		lines := strings.Split(expandLorem(t, "p*3>lorem", nil), "\n")
		re := regexp.MustCompile(`^<p>Lorem,?\sipsum`)
		if !re.MatchString(lines[0]) || re.MatchString(lines[1]) {
			t.Errorf("%q", lines)
		}
	})
	t.Run("ul>lorem5*3", func(t *testing.T) {
		out := expandLorem(t, "ul>lorem5*3", func(c *Config) { c.Options.Indent = "" })
		lines := strings.Split(out, "\n")
		re := regexp.MustCompile(`^<li>Lorem,?\sipsum`)
		if len(lines) != 5 || !re.MatchString(lines[1]) || re.MatchString(lines[2]) {
			t.Errorf("%q", lines)
		}
	})
}

// Not in Emmet's tests: the vocabularies, the limit, and the fixed source of random numbers.
func TestLoremMore(t *testing.T) {
	t.Run("languages", func(t *testing.T) {
		// (some of the common words of a language are several words, so the count is only a minimum)
		for _, lang := range []string{"ru", "sp", "latin", "xx"} {
			out := expandLorem(t, "lorem"+lang+"12", nil)
			if n := wordCount(out); n < 12 {
				t.Errorf("lorem%s12: %d words: %q", lang, n, out)
			}
		}
		if out := expandLorem(t, "loremsp3", nil); !strings.HasPrefix(out, "Mujer") && !strings.HasPrefix(out, "Lorem") {
			t.Errorf("loremsp3: %q", out)
		}
	})
	t.Run("the limit", func(t *testing.T) {
		out := expandLorem(t, "lorem99999999999", func(c *Config) { c.Rand = fixedRand() })
		if n := wordCount(out); n != MaxLoremWords {
			t.Errorf("%d words, want the limit %d", n, MaxLoremWords)
		}
	})
	t.Run("a fixed source gives the same text", func(t *testing.T) {
		a := expandLorem(t, "lorem40", func(c *Config) { c.Rand = fixedRand() })
		b := expandLorem(t, "lorem40", func(c *Config) { c.Rand = fixedRand() })
		if a != b || a == "" {
			t.Errorf("%q != %q", a, b)
		}
	})
	t.Run("only the first has the common words", func(t *testing.T) {
		lines := strings.Split(expandLorem(t, "lorem6*2", func(c *Config) { c.Rand = fixedRand() }), "\n")
		if !strings.HasPrefix(lines[0], "Lorem ipsum") || strings.HasPrefix(lines[1], "Lorem ipsum") {
			t.Errorf("%q", lines)
		}
	})
}
