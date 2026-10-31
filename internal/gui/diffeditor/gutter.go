package diffeditor

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

// The text a toolbar in the gutter of the diff editor hands to a command (SPEC §10.30 "Staging", "The peek"): the original text with the
// lines of a hunk, or of the selected lines, taken from the modified text. Git's Stage Block and Stage Selection write it to the index.
//
// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/features/gutterFeature.ts (the context of the hunk and selection toolbars)
// and src/vs/editor/common/diff/rangeMapping.ts (DetailedLineRangeMapping.toTextEdit)

// HunkAt is the change whose modified lines hold the line (counted from 1); false when none does. A change that only deletes lines has no
// modified lines and holds none.
func (m *Model) HunkAt(line int) (int, bool) {
	for i, c := range m.changes {
		if !c.Modified.IsEmpty() && line >= c.Modified.StartLine && line < c.Modified.EndLine {
			return i, true
		}
	}
	return 0, false
}

// TextWithHunk is the original text with change i applied: its original lines replaced by its modified lines.
func (m *Model) TextWithHunk(i int) string {
	if i < 0 || i >= len(m.changes) {
		return m.vms[Original].Document().Text()
	}
	return m.textWith([]diff.LineRangeMapping{m.changes[i]})
}

// TextWithLines is the original text with the changes the modified lines first to last (counted from 1, both included) touch applied. A
// change with as many lines on both sides is cut to the selected lines, line by line; any other is applied whole. It reports false when
// no change touches the lines.
func (m *Model) TextWithLines(first, last int) (string, bool) {
	var parts []diff.LineRangeMapping
	for _, c := range m.changes {
		if c.Modified.IsEmpty() || c.Modified.EndLine <= first || c.Modified.StartLine > last {
			continue
		}
		if c.Original.Len() == c.Modified.Len() {
			from, to := max(c.Modified.StartLine, first), min(c.Modified.EndLine, last+1)
			delta := from - c.Modified.StartLine
			c = diff.LineRangeMapping{
				Original: diff.LineRange{StartLine: c.Original.StartLine + delta, EndLine: c.Original.StartLine + delta + (to - from)},
				Modified: diff.LineRange{StartLine: from, EndLine: to},
			}
		}
		parts = append(parts, c)
	}
	if len(parts) == 0 {
		return "", false
	}
	return m.textWith(parts), true
}

// textWith is the original text with the parts applied, which are in order and do not overlap.
func (m *Model) textWith(parts []diff.LineRangeMapping) string {
	orig, _ := diff.SplitLines(m.vms[Original].Document().Text())
	mod, _ := diff.SplitLines(m.vms[Modified].Document().Text())
	var out []string
	at := 0
	for _, p := range parts {
		from := min(max(p.Original.StartLine-1, at), len(orig))
		out = append(out, orig[at:from]...)
		out = append(out, mod[min(p.Modified.StartLine-1, len(mod)):min(p.Modified.EndLine-1, len(mod))]...)
		at = min(max(p.Original.EndLine-1, from), len(orig))
	}
	out = append(out, orig[at:]...)
	return strings.Join(out, "\n")
}
