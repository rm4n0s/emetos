package regex

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func TestScannerFindsTheEarliestMatch(t *testing.T) {
	tests := []struct {
		name        string
		patterns    []string
		text        string
		pos         int
		anchorPos   int
		isFirstLine bool
		want        *ScanMatch
	}{
		{
			name: "the earliest start wins over the first pattern", patterns: []string{`b`, `a`}, text: "ab",
			want: &ScanMatch{Pattern: 1, Captures: []Capture{{0, 1}}},
		},
		{
			name: "a tie goes to the first pattern", patterns: []string{`a+`, `a`, `ab`}, text: "xab", pos: 0,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{1, 2}}},
		},
		{
			name: "a tie goes to the first pattern even when it is shorter", patterns: []string{`a`, `ab`}, text: "ab",
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{0, 1}}},
		},
		{
			name: "the search starts at pos", patterns: []string{`a`}, text: "aXa", pos: 1,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{2, 3}}},
		},
		{
			name: "no match", patterns: []string{`x`, `y`}, text: "abc",
		},
		{
			name: "no patterns", patterns: nil, text: "abc",
		},
		{
			name: "pos past the end", patterns: []string{`a`}, text: "a", pos: 2,
		},
		{
			name: "an empty match at the end", patterns: []string{`$`}, text: "ab", pos: 2, anchorPos: -1,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{2, 2}}},
		},
		{
			name: "groups, one of them unmatched", patterns: []string{`(a)(x)?(b)`}, text: "ab",
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{0, 2}, {0, 1}, {-1, -1}, {1, 2}}},
		},
		{
			name: "multi-byte text gives byte offsets", patterns: []string{`(ö)(r)`}, text: "héllo wörld",
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{8, 11}, {8, 10}, {10, 11}}},
		},
		{
			name: "pos is in bytes", patterns: []string{`l`}, text: "hé;l", pos: 4,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{4, 5}}},
		},
		{
			name: "a four-byte character is one rune", patterns: []string{`.b`}, text: "😀b",
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{0, 5}}},
		},
		{
			name: "an invalid byte is U+FFFD", patterns: []string{`\x{FFFD}`}, text: "a\xffb",
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{1, 2}}},
		},
		{
			name: "what follows an invalid byte keeps its offset", patterns: []string{`b`}, text: "a\xff\xfeb",
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{3, 4}}},
		},
		{
			name: "a truncated sequence is invalid bytes", patterns: []string{`\x{FFFD}+`}, text: "a\xe2\x82",
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{1, 3}}},
		},
		{
			name: "anchor position at pos", patterns: []string{`\Gb`}, text: "ab", pos: 1, anchorPos: 1,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{1, 2}}},
		},
		{
			name: "anchor position elsewhere", patterns: []string{`\Gb`}, text: "ab", pos: 1, anchorPos: 0,
		},
		{
			name: "no anchor position", patterns: []string{`\Gb`}, text: "ab", pos: 1, anchorPos: -1,
		},
		{
			name: "a pattern without the anchor still matches", patterns: []string{`\Gb`, `b`}, text: "ab", pos: 1, anchorPos: 0,
			want: &ScanMatch{Pattern: 1, Captures: []Capture{{1, 2}}},
		},
		{
			name: "start of text on the first line", patterns: []string{`\Aa`}, text: "a", isFirstLine: true, anchorPos: -1,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{0, 1}}},
		},
		{
			name: "start of text on another line", patterns: []string{`\Aa`}, text: "a", anchorPos: -1,
		},
		{
			name: "start of text and anchor position together", patterns: []string{`\A\Ga`}, text: "a", isFirstLine: true, anchorPos: 0,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{0, 1}}},
		},
		{
			name: "start of text allowed and anchor position not", patterns: []string{`\A\Ga`, `a`}, text: "a", isFirstLine: true, anchorPos: -1,
			want: &ScanMatch{Pattern: 1, Captures: []Capture{{0, 1}}},
		},
		{
			name: "a lookbehind reads before pos", patterns: []string{`(?<=a)b`}, text: "ab", pos: 1, anchorPos: -1,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{1, 2}}},
		},
		{
			name: "a pattern that does not compile never matches", patterns: []string{`(`, `b`}, text: "ab", anchorPos: -1,
			want: &ScanMatch{Pattern: 1, Captures: []Capture{{1, 2}}},
		},
		{
			name: "start of line after a break", patterns: []string{`^b`}, text: "a\nb", anchorPos: -1,
			want: &ScanMatch{Pattern: 0, Captures: []Capture{{2, 3}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, cache := range []*Cache{nil, NewCache(10)} {
				s := NewScanner(tc.patterns, ScannerOptions{Cache: cache})
				got, ok, err := s.Find(NewLine(tc.text), tc.pos, tc.anchorPos, tc.isFirstLine)
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == nil {
					if ok {
						t.Fatalf("matched %+v, want no match", got)
					}
					continue
				}
				if !ok {
					t.Fatalf("no match, want %+v", *tc.want)
				}
				if got.Pattern != tc.want.Pattern || len(got.Captures) != len(tc.want.Captures) {
					t.Fatalf("got %+v, want %+v", got, *tc.want)
				}
				for i, c := range tc.want.Captures {
					if got.Captures[i] != c {
						t.Fatalf("got %+v, want %+v", got, *tc.want)
					}
				}
			}
		})
	}
}

func TestScannerReportsThePatternsThatDoNotCompile(t *testing.T) {
	s := NewScanner([]string{`a`, `(`, `\p{Nope}`}, ScannerOptions{})
	if s.Len() != 3 {
		t.Fatalf("Len() = %d", s.Len())
	}
	if s.Err(0) != nil {
		t.Errorf("Err(0) = %v", s.Err(0))
	}
	for i, tag := range map[int]string{1: "RegexUnbalancedParen", 2: "RegexUnknownProperty"} {
		appErr, ok := errors.FromError(s.Err(i))
		if !ok || !appErr.HasRoute(tag) {
			t.Errorf("Err(%d) = %v, want %s", i, s.Err(i), tag)
		}
	}
}

func TestALineKeepsTheOffsetsOfItsRunes(t *testing.T) {
	tests := []struct {
		name string
		text string
		// runes is the number of runes; offsets the byte offset of each (and the end)
		runes   int
		offsets []int
	}{
		{"ASCII", "abc", 3, []int{0, 1, 2, 3}},
		{"empty", "", 0, []int{0}},
		{"two-byte", "aéb", 3, []int{0, 1, 3, 4}},
		{"invalid bytes", "a\xffb", 3, []int{0, 1, 2, 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLine(tc.text)
			if l.Text() != tc.text || len(l.runes) != tc.runes {
				t.Fatalf("runes = %d, want %d", len(l.runes), tc.runes)
			}
			for i, want := range tc.offsets {
				if got := l.byteOffset(i); got != want {
					t.Errorf("byteOffset(%d) = %d, want %d", i, got, want)
				}
				if got := l.runeIndex(want); got != i {
					t.Errorf("runeIndex(%d) = %d, want %d", want, got, i)
				}
			}
			if got := l.runeIndex(len(tc.text) + 5); got != tc.runes && l.offsets == nil {
				t.Errorf("runeIndex past the end = %d", got)
			}
		})
	}
}

func TestACatastrophicPatternTimesOut(t *testing.T) {
	tests := []struct {
		name    string
		opts    ScannerOptions
		longest time.Duration
	}{
		{"the default limit", ScannerOptions{}, 3 * time.Second},
		{"a shorter limit", ScannerOptions{Timeout: 30 * time.Millisecond}, time.Second},
	}
	text := NewLine(strings.Repeat("a", 64) + "!")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScanner([]string{`(a+)+$`}, tc.opts)
			start := time.Now()
			_, ok, err := s.Find(text, 0, -1, false)
			if ok {
				t.Fatal("matched")
			}
			appErr, isErr := errors.FromError(err)
			if !isErr || !appErr.HasRoute("Find.RegexTimeout") {
				t.Fatalf("err = %v", err)
			}
			if d := time.Since(start); d > tc.longest {
				t.Errorf("the search took %v", d)
			}
		})
	}
}

func TestAScannerIsSafeForConcurrentUse(t *testing.T) {
	s := NewScanner([]string{`\Gfoo`, `\Abar`, `baz`}, ScannerOptions{Cache: NewCache(10)})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				line := NewLine("xx foo bar baz")
				m, ok, err := s.Find(line, 3, 3-(i%2)*3, i%3 == 0)
				if err != nil || !ok {
					t.Errorf("Find: %v %v", ok, err)
					return
				}
				// The anchor position is allowed on the even rounds, and then \Gfoo comes first.
				if i%2 == 0 && m.Pattern != 0 {
					t.Errorf("pattern %d, want 0", m.Pattern)
					return
				}
			}
		}()
	}
	wg.Wait()
}
