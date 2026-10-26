package langfeatures

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
)

// Formatting (SPEC §9.12 "Formatting", "As built (step 12.8)"): the edits a formatter answers are made as small as the change they
// make, the lines that changed since the last save are the ranges of "modifications" mode, and the characters that start
// formatting while typing are those of the providers.
//
// Ported from VS Code: src/vs/editor/contrib/format/browser/format.ts, src/vs/editor/common/services/editorWorker.ts
// (computeMoreMinimalEdits)

// maxMinimalLines is the number of lines of a replaced text over which no line diff is made.
const maxMinimalLines = 4000

// MinimalEdits makes the edits as small as the change they make: the text that an edit leaves as it was, at its start and at its
// end, is taken out, and an edit that replaces several lines becomes an edit for each block of lines that changed. An edit that
// changes nothing is dropped. The edits are in the text of snap, and apply as the originals would.
//
// Ported from VS Code: editorWorker.ts (computeMoreMinimalEdits)
func MinimalEdits(snap buffer.Snapshot, edits []TextEdit) []TextEdit {
	out := make([]TextEdit, 0, len(edits))
	for _, e := range edits {
		out = append(out, minimalEdit(snap, e)...)
	}
	return out
}

// minimalEdit shrinks one edit.
func minimalEdit(snap buffer.Snapshot, e TextEdit) []TextEdit {
	r := snap.ValidateRange(e.Range)
	old := snap.Text(r)
	if old == e.Text {
		return nil
	}
	pre := commonPrefix(old, e.Text)
	suf := commonSuffix(old[pre:], e.Text[pre:])
	oldMid, newMid := old[pre:len(old)-suf], e.Text[pre:len(e.Text)-suf]
	start := advance(r.Start, old[:pre])
	if strings.Contains(oldMid, "\n") && strings.Contains(newMid, "\n") {
		if parts, ok := lineEdits(start, oldMid, newMid); ok {
			return parts
		}
	}
	return []TextEdit{{Range: buffer.Range{Start: start, End: advance(start, oldMid)}, Text: newMid}}
}

// commonPrefix is the length in bytes of the longest prefix the texts share, at a character boundary.
func commonPrefix(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	for i > 0 && i < len(a) && !utf8.RuneStart(a[i]) {
		i--
	}
	for i > 0 && i < len(b) && !utf8.RuneStart(b[i]) {
		i--
	}
	return i
}

// commonSuffix is the length in bytes of the longest suffix the texts share, at a character boundary.
func commonSuffix(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	for i > 0 && !utf8.RuneStart(a[len(a)-i]) {
		i--
	}
	for i > 0 && !utf8.RuneStart(b[len(b)-i]) {
		i--
	}
	return i
}

// advance is the position after text, written from start.
func advance(start Position, text string) Position {
	n := strings.Count(text, "\n")
	if n == 0 {
		return Position{Line: start.Line, Col: start.Col + len(text)}
	}
	return Position{Line: start.Line + n, Col: len(text) - strings.LastIndex(text, "\n") - 1}
}

// lineEdits splits the replacement of oldText by newText into one edit for each block of lines the line diff says changed. It
// says false when the diff does not give the new text back (the caller then keeps one edit).
func lineEdits(start Position, oldText, newText string) ([]TextEdit, bool) {
	oldLines, newLines := strings.Split(oldText, "\n"), strings.Split(newText, "\n")
	if len(oldLines) > maxMinimalLines || len(newLines) > maxMinimalLines {
		return nil, false
	}
	d, err := diff.ComputeLines(context.Background(), oldLines, newLines, diff.Options{})
	if err != nil || d.HitTimeout || len(d.Changes) == 0 {
		return nil, false
	}
	if c := d.Changes[0]; len(d.Changes) == 1 && c.Original.Len() == len(oldLines) && c.Modified.Len() == len(newLines) {
		return nil, false // the diff found nothing the single edit does not say
	}
	lineOffset := func(lines []string, n int) int {
		off := 0
		for i := 0; i < n && i < len(lines); i++ {
			off += len(lines[i]) + 1
		}
		return min(off, len(strings.Join(lines, "\n")))
	}
	type span struct {
		oldStart, oldEnd int
		text             string
	}
	var spans []span
	for _, c := range d.Changes {
		a, b := c.Original.StartLine-1, c.Original.EndLine-1
		ma, mb := c.Modified.StartLine-1, c.Modified.EndLine-1
		atEnd := b == len(oldLines)
		// lines added or removed at the end of the text take the line before them: the line break between them is part of the change
		if atEnd && (a == b || ma == mb) && a > 0 && ma > 0 {
			a--
			ma--
		}
		os, oe := lineOffset(oldLines, a), lineOffset(oldLines, b)
		ns, ne := lineOffset(newLines, ma), lineOffset(newLines, mb)
		// lines that are replaced by lines keep their line break
		if !atEnd && a < b && ma < mb {
			oe--
			ne--
		}
		spans = append(spans, span{oldStart: os, oldEnd: oe, text: newText[ns:ne]})
	}
	// the blocks must give the new text back
	var rebuilt strings.Builder
	at := 0
	for _, s := range spans {
		if s.oldStart < at {
			return nil, false
		}
		rebuilt.WriteString(oldText[at:s.oldStart])
		rebuilt.WriteString(s.text)
		at = s.oldEnd
	}
	rebuilt.WriteString(oldText[at:])
	if rebuilt.String() != newText {
		return nil, false
	}
	out := make([]TextEdit, 0, len(spans))
	for _, s := range spans {
		out = append(out, TextEdit{Range: buffer.Range{Start: advance(start, oldText[:s.oldStart]), End: advance(start, oldText[:s.oldEnd])}, Text: s.text})
	}
	return out, true
}

// ModifiedRanges are the ranges of lines of the text that differ from the base: the places that "modifications" mode formats.
// Deleted lines make no range. The base and the text are given by lines; a diff that runs over its time limit gives the whole
// text.
//
// Ported from VS Code: src/vs/workbench/contrib/format/browser/formatModified.ts (getModifiedRanges)
func ModifiedRanges(ctx context.Context, base, text []string) ([]Range, error) {
	d, err := diff.ComputeLines(ctx, base, text, diff.Options{MaxComputationTime: time.Second})
	if err != nil {
		return nil, err
	}
	var out []Range
	for _, c := range d.Changes {
		if c.Modified.IsEmpty() {
			continue
		}
		last := c.Modified.EndLine - 2
		out = append(out, Range{
			Start: Position{Line: c.Modified.StartLine - 1},
			End:   Position{Line: last, Col: len(text[last])},
		})
	}
	return out, nil
}

// OnTypeFormattingTriggers are the characters that ask for formatting after they are typed, of the providers that match the
// document, without duplicates.
func (r *Registry) OnTypeFormattingTriggers(doc Document) []string {
	var out []string
	for _, reg := range r.onType.ordered(doc) {
		if !offered(reg.Provider, FeatureOnTypeFormatting) {
			continue
		}
		for _, c := range reg.Provider.OnTypeTriggers() {
			if c != "" && !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	return out
}

// FormatOnType asks the first provider that has the character as a trigger for the edits and returns the first non-empty
// answer (VS Code asks the first provider of the order).
func (r *Registry) FormatOnType(ctx context.Context, doc Document, pos Position, ch string, opts FormattingOptions) ([]TextEdit, error) {
	for _, reg := range r.onType.ordered(doc) {
		if !offered(reg.Provider, FeatureOnTypeFormatting) || !slices.Contains(reg.Provider.OnTypeTriggers(), ch) {
			continue
		}
		edits, err := reg.Provider.ProvideOnTypeFormatting(ctx, doc, pos, ch, opts)
		if err != nil || len(edits) > 0 {
			return edits, err
		}
	}
	return nil, nil
}
