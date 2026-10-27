package markdown

import (
	"strings"
	"testing"
)

func TestEstimatesGrowWithTheTextAndShrinkWithTheWidth(t *testing.T) {
	wide, narrow := estimator{width: 800}, estimator{width: 300}
	short := Parse("one short line")[0]
	long := Parse(strings.Repeat("a line of words that goes on and on. ", 40))[0]
	if s, l := wide.blockHeight(short), wide.blockHeight(long); l <= s*3 {
		t.Errorf("a paragraph of 1500 characters is %v tall against %v for a line", l, s)
	}
	if w, n := wide.blockHeight(long), narrow.blockHeight(long); n <= w {
		t.Errorf("a narrower page makes the paragraph taller: %v against %v", n, w)
	}
	tests := []struct {
		name     string
		src      string
		min, max float64
	}{
		{"a line of text", "one line", 22, 23},
		{"a heading 1", "# Title", 55, 80},
		{"a heading 3", "### Title", 30, 50},
		{"a rule", "---", 15, 20},
		{"code of three lines", "```\na\nb\nc\n```", 3*13*1.4 + 24 - 1, 3*13*1.4 + 24 + 1},
		{"a quote", "> text", 22, 40},
		{"a list of three items", "- a\n- b\n- c", 66, 68},
		{"an image only", "![x](y.png)", imageEstimate, imageEstimate + 1},
		{"a table", "| a | b |\n|---|---|\n| c | d |", 60, 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := Parse(tt.src)
			if len(blocks) != 1 {
				t.Fatalf("%d blocks", len(blocks))
			}
			if h := wide.blockHeight(blocks[0]); h < tt.min || h > tt.max {
				t.Errorf("height %v, want %v to %v", h, tt.min, tt.max)
			}
		})
	}
}

func TestTokenCacheDropsTheOldestBeyondItsLimit(t *testing.T) {
	c := newTokenCache()
	for i := range maxCachedHighlights + 10 {
		c.put(string(rune('a'+i%26))+strings.Repeat("x", i), nil)
	}
	if len(c.lines) != maxCachedHighlights {
		t.Errorf("%d entries, want %d", len(c.lines), maxCachedHighlights)
	}
	if _, ok := c.get("a"); ok {
		t.Error("the first entry is still there")
	}
	last := string(rune('a'+(maxCachedHighlights+9)%26)) + strings.Repeat("x", maxCachedHighlights+9)
	if _, ok := c.get(last); !ok {
		t.Error("the last entry is gone")
	}
}
