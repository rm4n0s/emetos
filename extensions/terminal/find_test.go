package terminal

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// bufferOf writes text to an emulator of rows and cols and returns it.
func bufferOf(t testing.TB, rows, cols int, text string) *emulator.Emulator {
	t.Helper()
	em := emulator.New(rows, cols, emulator.Options{Scrollback: 5000})
	if _, err := em.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	return em
}

// span is a match as the tests write it: line and column of the start, line and column after the end.
type span struct{ l0, c0, l1, c1 int }

func spansOf(ms []findMatch, first int64) []span {
	var out []span
	for _, m := range ms {
		out = append(out, span{int(m.start.line - first), m.start.col, int(m.end.line - first), m.end.col})
	}
	return out
}

func TestSearchFindsTheMatchesOfTheBuffer(t *testing.T) {
	const sep = " ()[]{}',\"`─‘’“”|"
	tests := []struct {
		name  string
		rows  int
		cols  int
		text  string
		query findQuery
		want  []span
	}{
		{"a word on two lines", 5, 20, "one foo\r\ntwo bar foo\r\n", findQuery{text: "foo"}, []span{{0, 4, 0, 7}, {1, 8, 1, 11}}},
		{"the case is ignored by default", 5, 20, "Foo FOO foo\r\n", findQuery{text: "foo"}, []span{{0, 0, 0, 3}, {0, 4, 0, 7}, {0, 8, 0, 11}}},
		{"match case", 5, 20, "Foo FOO foo\r\n", findQuery{text: "Foo", matchCase: true}, []span{{0, 0, 0, 3}}},
		{"a whole word (a full stop is no separator)", 5, 30, "foo foobar (foo) barfoo foo.\r\n", findQuery{text: "foo", wholeWord: true, separators: sep}, []span{{0, 0, 0, 3}, {0, 12, 0, 15}}},
		{"a regular expression", 5, 30, "a1 b22 c333\r\n", findQuery{text: `[a-z]\d+`, regex: true}, []span{{0, 0, 0, 2}, {0, 3, 0, 6}, {0, 7, 0, 11}}},
		{"special characters are plain text without the switch", 5, 30, "a.b a+b axb\r\n", findQuery{text: "a.b"}, []span{{0, 0, 0, 3}}},
		{"a match across a soft wrap", 5, 10, "abcdefgh foo bar\r\n", findQuery{text: "h foo"}, []span{{0, 7, 1, 2}}},
		{"a match across a soft wrap with a regular expression", 5, 10, "1234567890ab\r\n", findQuery{text: `90a`, regex: true}, []span{{0, 8, 1, 1}}},
		{"a match at the end of a full row", 5, 10, "abcdefgh foo\r\n", findQuery{text: "foo"}, []span{{0, 9, 1, 2}}},
		{"a line that was cleared by the program", 5, 20, "x\x1b[2K\rhello\r\n", findQuery{text: "x"}, nil},
		{"wide characters take two cells", 5, 20, "日本語 foo\r\n", findQuery{text: "foo"}, []span{{0, 7, 0, 10}}},
		{"a wide character in the match", 5, 20, "ab日本cd\r\n", findQuery{text: "日本"}, []span{{0, 2, 0, 6}}},
		{"no text finds nothing", 5, 20, "foo\r\n", findQuery{text: ""}, nil},
		{"an empty match is no match", 5, 20, "foo\r\n", findQuery{text: "x*", regex: true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := bufferOf(t, tt.rows, tt.cols, tt.text)
			ms, truncated, err := searchBuffer(context.Background(), em, tt.query, findLimit)
			if err != nil || truncated {
				t.Fatalf("searchBuffer: %v, truncated %v", err, truncated)
			}
			if got := spansOf(ms, em.FirstLine()); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("matches of %q = %+v, want %+v", tt.query.text, got, tt.want)
			}
		})
	}
}

func TestSearchReadsTheScrollbackInBlocks(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&sb, "line %d needle\r\n", i)
	}
	em := bufferOf(t, 24, 40, sb.String())
	if em.LineCount() < 3000 {
		t.Fatalf("the buffer has %d lines", em.LineCount())
	}
	ms, truncated, err := searchBuffer(context.Background(), em, findQuery{text: "needle"}, findLimit)
	if err != nil || truncated || len(ms) != 3000 {
		t.Fatalf("%d matches, truncated %v, %v", len(ms), truncated, err)
	}
	for i := 1; i < len(ms); i++ {
		if !ms[i-1].start.before(ms[i].start) {
			t.Fatalf("the matches are not in order at %d", i)
		}
	}
	// only some lines
	ms, _, err = searchBuffer(context.Background(), em, findQuery{text: `line 12\d\d `, regex: true}, findLimit)
	if err != nil || len(ms) != 100 {
		t.Errorf("%d matches of line 12dd, %v", len(ms), err)
	}
}

func TestALogicalLineThatCrossesABlockIsSearchedWhole(t *testing.T) {
	// a very long line that wraps over several blocks of rows, with a word that is cut by the border of two blocks
	cols := 20
	var sb strings.Builder
	for i := 0; i < 2*findBlock+10; i++ {
		sb.WriteString("0123456789abcdefghij")
	}
	text := sb.String()
	em := bufferOf(t, 24, cols, text+"\r\n")
	ms, _, err := searchBuffer(context.Background(), em, findQuery{text: "ij0123"}, findLimit)
	if err != nil {
		t.Fatal(err)
	}
	// the line is cut at findMaxLine characters: the matches are those before it
	if want := findMaxLine/cols - 1; len(ms) != want && len(ms) != want+1 {
		t.Errorf("%d matches, want about %d", len(ms), want)
	}
}

func TestSearchStopsAtTheLimitAndWhenCanceled(t *testing.T) {
	em := bufferOf(t, 24, 20, strings.Repeat("aaaa\r\n", 100))
	ms, truncated, err := searchBuffer(context.Background(), em, findQuery{text: "a"}, 50)
	if err != nil || !truncated || len(ms) != 50 {
		t.Errorf("limit: %d matches, truncated %v, %v", len(ms), truncated, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = searchBuffer(ctx, em, findQuery{text: "a"}, findLimit)
	routeHas(t, err, "searchBuffer.TerminalFindCanceled")
}

func TestASearchThatDoesNotCompileFails(t *testing.T) {
	em := bufferOf(t, 5, 20, "foo\r\n")
	// RE2 has no lookahead
	_, _, err := searchBuffer(context.Background(), em, findQuery{text: `foo(?=bar)`, regex: true}, findLimit)
	routeHas(t, err, "compile.TerminalFindPatternInvalid")
	if _, _, err := searchBuffer(context.Background(), em, findQuery{text: `foo(`}, findLimit); err != nil {
		t.Errorf("an unbalanced bracket is plain text without the switch: %v", err)
	}
}

func TestMatchSegmentsCutTheMatchesToTheRowsShown(t *testing.T) {
	m := findMatch{start: pos{10, 7}, end: pos{12, 3}}
	got := m.segments(10, 5, 10, 1, nil)
	want := []mark{{10, 7, 10, 1}, {11, 0, 10, 1}, {12, 0, 3, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("segments = %+v, want %+v", got, want)
	}
	if got := m.segments(11, 1, 10, 1, nil); !reflect.DeepEqual(got, []mark{{11, 0, 10, 1}}) {
		t.Errorf("segments of one row = %+v", got)
	}
	if got := m.segments(20, 5, 10, 1, nil); len(got) != 0 {
		t.Errorf("segments of rows below the match = %+v", got)
	}
}

func TestNavigationFindsTheNextAndPreviousMatch(t *testing.T) {
	ms := []findMatch{{start: pos{1, 0}}, {start: pos{1, 5}}, {start: pos{3, 2}}}
	tests := []struct {
		at                   pos
		atOrBefore, aft, bef int
	}{
		{pos{0, 0}, -1, 0, -1},
		{pos{1, 0}, 0, 1, -1},
		{pos{1, 3}, 0, 1, 0},
		{pos{1, 5}, 1, 2, 0},
		{pos{3, 2}, 2, -1, 1},
		{pos{9, 0}, 2, -1, 2},
	}
	for _, tt := range tests {
		if got := matchAtOrBefore(ms, tt.at); got != tt.atOrBefore {
			t.Errorf("matchAtOrBefore(%v) = %d, want %d", tt.at, got, tt.atOrBefore)
		}
		if got := matchAfter(ms, tt.at); got != tt.aft {
			t.Errorf("matchAfter(%v) = %d, want %d", tt.at, got, tt.aft)
		}
		if got := matchBefore(ms, tt.at); got != tt.bef {
			t.Errorf("matchBefore(%v) = %d, want %d", tt.at, got, tt.bef)
		}
	}
}
