package find

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// bigText is a text of lines lines, in which the lines in marks are "mark" and the line before
// each is "head"; the others are "filler".
func bigText(lines int, marks ...int) string {
	text := make([]string, lines)
	for i := range text {
		text[i] = "filler"
	}
	for _, m := range marks {
		text[m-1] = "head"
		text[m] = "mark"
	}
	return strings.Join(text, "\n")
}

func TestMultilineSearchOverWindows(t *testing.T) {
	// matches at the first line, around the border of the first window (5000 lines of its own and
	// 500 of lookahead), in the lookahead and far away
	marks := []int{1, 4998, 5001, 5498, 5501, 5998, 10000, 12000}
	text := bigText(12500, marks...)
	snap := snapshotOf(text)
	q := mustQuery(t, "head\\nmark", true, true)
	full := buffer.Range{End: endOf(snap)}

	var want []buffer.Range
	for _, m := range marks {
		want = append(want, buffer.Range{Start: buffer.Position{Line: m - 1}, End: buffer.Position{Line: m, Col: 4}})
	}
	var got []buffer.Range
	for _, m := range q.FindMatches(snap, full, false, 1000) {
		got = append(got, m.Range)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("FindMatches = %v\nwant %v", got, want)
	}
	// the limit counts across windows
	if got := q.FindMatches(snap, full, false, 4); len(got) != 4 || got[3].Range.Start.Line != 5497 {
		t.Errorf("limit 4: %v", got)
	}

	// next and previous, from everywhere between matches and on them
	for _, w := range want {
		if m, ok := q.FindNext(snap, w.Start, false); !ok || m.Range != w {
			t.Errorf("FindNext(%v) = %v %v", w.Start, m.Range, ok)
		}
		if m, ok := q.FindPrevious(snap, w.End, false); !ok || m.Range != w {
			t.Errorf("FindPrevious(%v) = %v %v", w.End, m.Range, ok)
		}
	}
	// between two matches, and wrapping round at both ends
	if m, ok := q.FindNext(snap, buffer.Position{Line: 7000}, false); !ok || m.Range != want[6] {
		t.Errorf("next after line 7000: %v %v", m.Range, ok)
	}
	if m, ok := q.FindPrevious(snap, buffer.Position{Line: 7000}, false); !ok || m.Range != want[5] {
		t.Errorf("previous before line 7000: %v %v", m.Range, ok)
	}
	if m, ok := q.FindNext(snap, buffer.Position{Line: 12100}, false); !ok || m.Range != want[0] {
		t.Errorf("next wraps to the first: %v %v", m.Range, ok)
	}
	if m, ok := q.FindPrevious(snap, buffer.Position{Line: 0, Col: 2}, false); !ok || m.Range != want[len(want)-1] {
		t.Errorf("previous wraps to the last: %v %v", m.Range, ok)
	}
	// a search that finds nothing
	none := mustQuery(t, "head\\nnothing", true, true)
	if _, ok := none.FindNext(snap, buffer.Position{}, false); ok {
		t.Error("found a match that is not there")
	}
	if _, ok := none.FindPrevious(snap, endOf(snap), false); ok {
		t.Error("found a match that is not there, backwards")
	}
}

func TestMultilineMatchRunningPastAWindow(t *testing.T) {
	// a match of many lines that starts in a window and ends in the next is found once
	lines := make([]string, 9000)
	for i := range lines {
		lines[i] = "x"
	}
	lines[4990] = "begin"
	lines[5300] = "end"
	snap := snapshotOf(strings.Join(lines, "\n"))
	q := mustQuery(t, "begin\\n[\\s\\S]*?end", true, true)
	ms := q.FindMatches(snap, buffer.Range{End: endOf(snap)}, false, 10)
	if len(ms) != 1 || ms[0].Range.Start.Line != 4990 || ms[0].Range.End.Line != 5300 {
		t.Errorf("matches %v", ms)
	}
}

func TestFindMatchesInSeveralRanges(t *testing.T) {
	snap := snapshotOf("foo foo\nfoo foo\nfoo foo\nfoo foo")
	q := mustQuery(t, "foo", false, false)
	got := q.FindMatchesIn(snap, []buffer.Range{rng(3, 1, 3, 8), rng(1, 5, 2, 4), rng(2, 3, 2, 8)}, false, 100)
	var spans []span
	for _, m := range got {
		spans = append(spans, spanOf(m.Range))
	}
	// the ranges that touch are one, and each match is found once
	want := []span{{1, 5, 1, 8}, {2, 1, 2, 4}, {2, 5, 2, 8}, {3, 1, 3, 4}, {3, 5, 3, 8}}
	if fmt.Sprint(spans) != fmt.Sprint(want) {
		t.Errorf("matches %v, want %v", spans, want)
	}
	if got := q.FindMatchesIn(snap, nil, false, 100); len(got) != 8 {
		t.Errorf("no ranges is the whole text: %d matches", len(got))
	}
	var nilQuery *Query
	if nilQuery.FindMatches(snap, rng(1, 1, 2, 1), false, 1) != nil || nilQuery.FindMatchesIn(snap, nil, false, 1) != nil {
		t.Error("no query, no matches")
	}
	if _, ok := nilQuery.FindNext(snap, buffer.Position{}, false); ok {
		t.Error("no query, no next")
	}
	if _, ok := nilQuery.FindPrevious(snap, buffer.Position{}, false); ok {
		t.Error("no query, no previous")
	}
}

func TestSearchInNonASCIIText(t *testing.T) {
	// the columns are byte columns
	text := "αβγ foo ΔΕΖ\nfoo"
	snap := snapshotOf(text)
	q := mustQuery(t, "foo", false, false)
	ms := q.FindMatches(snap, buffer.Range{End: endOf(snap)}, false, 10)
	want := []buffer.Range{
		{Start: buffer.Position{Col: len("αβγ ")}, End: buffer.Position{Col: len("αβγ foo")}},
		{Start: buffer.Position{Line: 1}, End: buffer.Position{Line: 1, Col: 3}},
	}
	if len(ms) != 2 || ms[0].Range != want[0] || ms[1].Range != want[1] {
		t.Errorf("matches %v", ms)
	}
	// the regular expression path gives the same columns, and the previous match too
	re := mustQuery(t, "f(o+)", true, false)
	m, ok := re.FindNext(snap, buffer.Position{}, true)
	if !ok || m.Range != want[0] || m.Groups[1] != "oo" {
		t.Errorf("regex next %v %v", m, ok)
	}
	if m, ok := re.FindPrevious(snap, buffer.Position{Line: 1, Col: 0}, true); !ok || m.Range != want[0] {
		t.Errorf("regex previous %v %v", m, ok)
	}
	// a greek whole word is a word: the separators are those of editor.wordSeparators
	words := Params{Search: "γ", WordSeparators: usualWordSeparators}.Query()
	if got := words.FindMatches(snap, buffer.Range{End: endOf(snap)}, false, 10); len(got) != 0 {
		t.Errorf("γ is not a whole word in αβγ: %v", got)
	}
	words = Params{Search: "αβγ", WordSeparators: usualWordSeparators}.Query()
	if got := words.FindMatches(snap, buffer.Range{End: endOf(snap)}, false, 10); len(got) != 1 {
		t.Errorf("αβγ is a whole word: %v", got)
	}
}

func TestSearchTimeoutDoesNotHang(t *testing.T) {
	// a pattern with catastrophic backtracking gives up instead of freezing the editor
	snap := snapshotOf(strings.Repeat("a", 40) + "!")
	q := mustQuery(t, "^(a+)+$", true, true)
	if ms := q.FindMatches(snap, buffer.Range{End: endOf(snap)}, false, 10); len(ms) != 0 {
		t.Errorf("matches %v", ms)
	}
}
