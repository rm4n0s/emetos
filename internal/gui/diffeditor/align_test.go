package diffeditor

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

// change is a change of the tests: the original and the modified line ranges, counted from 1, the end excluded.
func change(o1, o2, m1, m2 int, inner ...diff.RangeMapping) diff.LineRangeMapping {
	return diff.LineRangeMapping{
		Original: diff.LineRange{StartLine: o1, EndLine: o2}, Modified: diff.LineRange{StartLine: m1, EndLine: m2}, Inner: inner,
	}
}

// inner is a character change from (line, column) to (line, column) on each side; columns are bytes from 1.
func inner(ol1, oc1, ol2, oc2, ml1, mc1, ml2, mc2 int) diff.RangeMapping {
	return diff.RangeMapping{
		Original: diff.Range{Start: diff.Position{Line: ol1, Column: oc1}, End: diff.Position{Line: ol2, Column: oc2}},
		Modified: diff.Range{Start: diff.Position{Line: ml1, Column: mc1}, End: diff.Position{Line: ml2, Column: mc2}},
	}
}

func TestZonesOfTheAlignment(t *testing.T) {
	// lineLengths of the original text of the cases that have inner changes: every line is 12 bytes
	twelve := func(int) (int, bool) { return 12, true }
	for _, tc := range []struct {
		name         string
		changes      []diff.LineRangeMapping
		orig, mod    []LineExtra
		inner        bool
		wantOrig     []Zone
		wantModified []Zone
	}{
		{name: "no changes"},
		{name: "insertion at the start", changes: []diff.LineRangeMapping{change(1, 1, 1, 3)}, wantOrig: []Zone{{0, 2}}},
		{name: "deletion at the start", changes: []diff.LineRangeMapping{change(1, 3, 1, 1)}, wantModified: []Zone{{0, 2}}},
		{name: "insertion in the middle", changes: []diff.LineRangeMapping{change(3, 3, 3, 5)}, wantOrig: []Zone{{2, 2}}},
		{name: "deletion in the middle", changes: []diff.LineRangeMapping{change(3, 6, 3, 3)}, wantModified: []Zone{{2, 3}}},
		{name: "insertion at the end", changes: []diff.LineRangeMapping{change(6, 6, 6, 7)}, wantOrig: []Zone{{5, 1}}},
		{name: "deletion at the end", changes: []diff.LineRangeMapping{change(4, 6, 4, 4)}, wantModified: []Zone{{3, 2}}},
		{name: "replacement by fewer lines", changes: []diff.LineRangeMapping{change(2, 5, 2, 3)}, wantModified: []Zone{{2, 2}}},
		{name: "replacement by more lines", changes: []diff.LineRangeMapping{change(2, 3, 2, 6)}, wantOrig: []Zone{{2, 3}}},
		{name: "replacement by as many lines", changes: []diff.LineRangeMapping{change(2, 4, 2, 4)}},
		{
			name:     "adjacent changes, one line apart",
			changes:  []diff.LineRangeMapping{change(2, 3, 2, 5), change(4, 6, 6, 7)},
			wantOrig: []Zone{{2, 2}}, wantModified: []Zone{{6, 1}},
		},
		{
			name:    "changes that cancel each other in the total",
			changes: []diff.LineRangeMapping{change(1, 1, 1, 2), change(3, 4, 4, 4)},
			// the second is a deletion of one line: filler in the modified text after its line 3
			wantOrig: []Zone{{0, 1}}, wantModified: []Zone{{3, 1}},
		},
		{
			name:     "change at the end of a text with an empty last line",
			changes:  []diff.LineRangeMapping{change(3, 3, 3, 5)}, // "a\nb\n" against "a\nb\nc\nd\n": the empty line moves
			wantOrig: []Zone{{2, 2}},
		},
		{
			name: "an unchanged line that is wrapped in one text only", orig: []LineExtra{{Line: 4, Rows: 2}},
			changes:      []diff.LineRangeMapping{change(8, 9, 8, 10)},
			wantModified: []Zone{{4, 2}}, wantOrig: []Zone{{8, 1}},
		},
		{
			name: "unchanged lines wrapped in both texts alike", orig: []LineExtra{{Line: 4, Rows: 2}}, mod: []LineExtra{{Line: 4, Rows: 2}},
			changes: []diff.LineRangeMapping{change(8, 9, 8, 9)},
		},
		{
			name: "a wrapped line that is shifted by an earlier change",
			// original line 5 is wrapped (2 more rows); the modified text has 2 lines more above its counterpart, line 7
			orig: []LineExtra{{Line: 5, Rows: 2}}, mod: []LineExtra{{Line: 7, Rows: 1}},
			changes:      []diff.LineRangeMapping{change(2, 2, 2, 4)},
			wantOrig:     []Zone{{1, 2}},
			wantModified: []Zone{{7, 1}},
		},
		{
			name: "wrapped lines inside a change", orig: []LineExtra{{Line: 3, Rows: 1}},
			changes: []diff.LineRangeMapping{change(2, 4, 2, 4)}, wantModified: []Zone{{3, 1}},
		},
		{
			name: "wrapped lines inside a change that has more lines on the other side", mod: []LineExtra{{Line: 3, Rows: 2}},
			changes: []diff.LineRangeMapping{change(2, 4, 2, 5)}, wantOrig: []Zone{{3, 3}},
		},
		{
			name:  "inner changes cut the hunk where the lines share text",
			inner: true,
			// original lines 2-3 against modified lines 2-5: the character change inside line 3 of the original is on
			// line 5 of the modified text, and both have text before it
			changes: []diff.LineRangeMapping{change(2, 4, 2, 6, inner(3, 3, 3, 5, 5, 3, 5, 5))},
			// the lines before it (original 2, modified 2-4) are lined up after the original's line 2
			wantOrig: []Zone{{2, 2}},
		},
		{
			name:  "inner changes cut after the text that follows them, not before the text at the start of a line",
			inner: true, changes: []diff.LineRangeMapping{change(2, 4, 2, 6, inner(3, 1, 3, 5, 5, 1, 5, 5))},
			// no text before the change on its line: no cut there; text after it (the line is 12 bytes): a cut after line 3
			// of the original text and line 5 of the modified one
			wantOrig: []Zone{{2, 2}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := AlignOptions{Original: tc.orig, Modified: tc.mod, InnerHunkAlignment: tc.inner, OriginalLineLength: twelve}
			gotOrig, gotMod := Zones(ComputeAlignment(tc.changes, opts))
			if !reflect.DeepEqual(gotOrig, tc.wantOrig) || !reflect.DeepEqual(gotMod, tc.wantModified) {
				t.Errorf("zones: original %v modified %v, want original %v modified %v", gotOrig, gotMod, tc.wantOrig, tc.wantModified)
			}
		})
	}
}

func TestAlignmentsCoverTheChanges(t *testing.T) {
	changes := []diff.LineRangeMapping{change(2, 4, 2, 5), change(7, 7, 8, 9)}
	got := ComputeAlignment(changes, AlignOptions{})
	want := []Alignment{
		{Original: diff.LineRange{StartLine: 2, EndLine: 4}, Modified: diff.LineRange{StartLine: 2, EndLine: 5}, OriginalRows: 2, ModifiedRows: 3, Change: &changes[0]},
		{Original: diff.LineRange{StartLine: 7, EndLine: 7}, Modified: diff.LineRange{StartLine: 8, EndLine: 9}, OriginalRows: 0, ModifiedRows: 1, Change: &changes[1]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("alignments %+v, want %+v", got, want)
	}
}

func TestZonesAtTheSameLineAreOne(t *testing.T) {
	// two alignments that end after the same original line: an insertion before and one inside a change that
	// the inner changes cut at its first line
	alignments := []Alignment{
		{Original: diff.LineRange{StartLine: 3, EndLine: 3}, Modified: diff.LineRange{StartLine: 3, EndLine: 5}, OriginalRows: 0, ModifiedRows: 2},
		{Original: diff.LineRange{StartLine: 3, EndLine: 3}, Modified: diff.LineRange{StartLine: 5, EndLine: 6}, OriginalRows: 0, ModifiedRows: 1},
	}
	orig, mod := Zones(alignments)
	if !reflect.DeepEqual(orig, []Zone{{2, 3}}) || len(mod) != 0 {
		t.Errorf("zones %v %v", orig, mod)
	}
}

// texts that the property test makes: a text and a version of it with lines changed, inserted and deleted.
func randomTexts(rng *rand.Rand) (original, modified []string) {
	n := rng.IntN(25)
	word := func() string {
		return strings.Repeat(string(rune('a'+rng.IntN(4))), 1+rng.IntN(3)) + " " + string(rune('a'+rng.IntN(3)))
	}
	for range n {
		original = append(original, word())
	}
	for _, l := range original {
		switch rng.IntN(8) {
		case 0: // deleted
		case 1:
			modified = append(modified, l+"!", word())
		case 2:
			modified = append(modified, word())
		default:
			modified = append(modified, l)
		}
	}
	if rng.IntN(5) == 0 {
		modified = append(modified, word())
	}
	if len(original) == 0 {
		original = []string{""}
	}
	if len(modified) == 0 {
		modified = []string{""}
	}
	return original, modified
}

// randomExtras makes some of the lines of a text wrapped.
func randomExtras(rng *rand.Rand, lines int) []LineExtra {
	var out []LineExtra
	for l := 1; l <= lines; l++ {
		if rng.IntN(6) == 0 {
			out = append(out, LineExtra{Line: l, Rows: 1 + rng.IntN(3)})
		}
	}
	return out
}

func TestAlignmentLinesUpBothSides(t *testing.T) {
	// For random texts and wrappings: after the zones, the row above the line after every change and above every
	// unchanged line is the same in both texts, and so is the total number of rows.
	for seed := range uint64(400) {
		rng := rand.New(rand.NewPCG(seed, 7))
		original, modified := randomTexts(rng)
		res, err := diff.Compute(t.Context(), original, modified, diff.Options{IgnoreTrimWhitespace: seed%2 == 0})
		if err != nil {
			t.Fatal(err)
		}
		var origExtra, modExtra []LineExtra
		if seed%3 != 0 {
			origExtra, modExtra = randomExtras(rng, len(original)), randomExtras(rng, len(modified))
		}
		opts := AlignOptions{
			Original: origExtra, Modified: modExtra, InnerHunkAlignment: seed%4 != 0,
			OriginalLineLength: func(l int) (int, bool) {
				if l < 1 || l > len(original) {
					return 0, false
				}
				return len(original[l-1]), true
			},
		}
		zo, zm := Zones(ComputeAlignment(res.Changes, opts))
		rows := func(line int, o bool) int {
			if o {
				return RowsBefore(line, origExtra, zo)
			}
			return RowsBefore(line, modExtra, zm)
		}
		check := func(what string, ol, ml int) {
			if a, b := rows(ol, true), rows(ml, false); a != b {
				t.Fatalf("seed %d: above %s (original line %d, modified line %d) there are %d and %d rows\noriginal %q\nmodified %q\nchanges %+v\nzones %v %v",
					seed, what, ol, ml, a, b, original, modified, res.Changes, zo, zm)
			}
		}
		oLine, mLine := 1, 1
		for _, c := range res.Changes {
			for oLine < c.Original.StartLine {
				check("an unchanged line", oLine, mLine)
				oLine++
				mLine++
			}
			oLine, mLine = c.Original.EndLine, c.Modified.EndLine
			check("the end of a change", oLine, mLine)
		}
		for oLine <= len(original) {
			check("an unchanged line", oLine, mLine)
			oLine++
			mLine++
		}
		check("the end of the text", len(original)+1, len(modified)+1)
		// the filler is after lines the texts have, or above the first
		for _, z := range zo {
			if z.AfterLine < 0 || z.AfterLine > len(original) || z.Rows < 1 {
				t.Fatalf("seed %d: original zone %+v", seed, z)
			}
		}
		for _, z := range zm {
			if z.AfterLine < 0 || z.AfterLine > len(modified) || z.Rows < 1 {
				t.Fatalf("seed %d: modified zone %+v", seed, z)
			}
		}
	}
}
