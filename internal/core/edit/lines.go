package edit

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func posAt(line, col int) buffer.Position { return buffer.Position{Line: line, Col: col} }

func rangeOf(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: posAt(l1, c1), End: posAt(l2, c2)}
}

// lineSpan is the lines a selection works on: its first and last line. A selection
// of several lines that ends at the start of a line leaves that line out.
func lineSpan(s buffer.Selection) (first, last int, endLeftOut bool) {
	first, last = s.Start().Line, s.End().Line
	if first < last && s.End().Col == 0 {
		return first, last - 1, true
	}
	return first, last, false
}

// shiftLines moves both ends of a selection by a number of lines.
func shiftLines(s buffer.Selection, delta int) buffer.Selection {
	return buffer.Selection{Anchor: s.Anchor.Delta(delta, 0), Active: s.Active.Delta(delta, 0)}
}

// snapCol moves col back to the start of a character and not past the end of line.
func snapCol(line string, col int) int {
	col = min(col, len(line))
	for col > 0 && col < len(line) && !utf8.RuneStart(line[col]) {
		col--
	}
	return col
}

// MoveLines moves the lines the selections touch one line up or down (Alt+Up,
// Alt+Down). The selections move with the text. Moving the first line up or the
// last line down does nothing. An empty line swaps its place with the line next to
// it and the cursor goes to that line.
//
// Ported from VS Code: src/vs/editor/contrib/linesOperations/browser/moveLinesCommand.ts,
// without the re-indentation by language rules (the language steps add it).
func (c Commands) MoveLines(sels []buffer.Selection, down bool) Result {
	last := lastLine(c.Model)
	plans := make([]plan, len(sels))
	for i, raw := range sels {
		s := c.validSelection(raw)
		if down && s.End().Line == last || !down && s.Start().Line == 0 {
			continue
		}
		first, end, endLeftOut := lineSpan(s)
		if first == end && c.Model.LineContent(first) == "" {
			other := first - 1
			if down {
				other = first + 1
			}
			p := plan{custom: true, selection: func(*mapper, []buffer.Range) buffer.Selection {
				return buffer.NewCursor(posAt(other, 0))
			}}
			if text := c.Model.LineContent(other); text != "" {
				p.edits = []document.Edit{
					{Range: rangeOf(first, 0, first, 0), Text: text},
					{Range: rangeOf(other, 0, other, len(text)), Text: ""},
				}
			}
			plans[i] = p
			continue
		}
		tracked := s
		if endLeftOut {
			tracked = s.WithEnd(posAt(end, len(c.Model.LineContent(end))))
		}
		atLineEnd := s.IsEmpty() && s.Active.Col == len(c.Model.LineContent(s.Active.Line))
		var edits []document.Edit
		if down {
			m := end + 1
			text := c.Model.LineContent(m)
			edits = []document.Edit{
				{Range: rangeOf(end, len(c.Model.LineContent(end)), m, len(text))},
				{Range: rangeOf(first, 0, first, 0), Text: text + "\n"},
			}
		} else {
			m := first - 1
			text := c.Model.LineContent(m)
			edits = []document.Edit{
				{Range: rangeOf(m, 0, m+1, 0)},
				{Range: rangeOf(end, len(c.Model.LineContent(end)), end, len(c.Model.LineContent(end))), Text: "\n" + text},
			}
		}
		plans[i] = plan{edits: edits, selection: func(m *mapper, _ []buffer.Range) buffer.Selection {
			result := m.track(tracked, atLineEnd)
			if endLeftOut {
				result = result.WithEnd(posAt(result.End().Line+1, 0))
			}
			return result
		}}
	}
	return build(sels, plans, document.KindOther)
}

// CopyLines copies the lines the selections touch above or below themselves
// (Shift+Alt+Up, Shift+Alt+Down); the selection ends on the copy below, or stays
// on the upper block when copying up. Selections that would copy the same line copy
// it once. The selections of the result are in the order of the text.
//
// Ported from VS Code: copyLinesCommand.ts and AbstractCopyLinesAction.
func (c Commands) CopyLines(sels []buffer.Selection, down bool) Result {
	if len(sels) == 0 {
		return Result{}
	}
	type item struct {
		sel    buffer.Selection
		index  int
		ignore bool
	}
	items := make([]*item, len(sels))
	for i, s := range sels {
		items[i] = &item{sel: c.validSelection(s), index: i}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return buffer.CompareUsingStarts(items[i].sel.Range(), items[j].sel.Range()) < 0
	})
	prev := items[0]
	for _, cur := range items[1:] {
		if prev.sel.End().Line == cur.sel.Start().Line {
			if prev.index < cur.index {
				cur.ignore = true
			} else {
				prev.ignore = true
				prev = cur
			}
		}
	}

	sorted := make([]buffer.Selection, len(items))
	plans := make([]plan, len(items))
	for i, it := range items {
		sorted[i] = it.sel
		plans[i] = c.copyLinesPlan(it.sel, down, it.ignore)
	}
	return build(sorted, plans, document.KindOther)
}

func (c Commands) copyLinesPlan(orig buffer.Selection, down, noop bool) plan {
	first, last, endLeftOut := lineSpan(orig)
	s := orig
	startDelta, endDelta := 0, 0
	if endLeftOut {
		endDelta = 1
		s = s.WithEnd(posAt(last, len(c.Model.LineContent(last))))
	}
	lines := make([]string, 0, last-first+1)
	for l := first; l <= last; l++ {
		lines = append(lines, c.Model.LineContent(l))
	}
	source := strings.Join(lines, "\n")
	if source == "" && down {
		startDelta++
		endDelta++
	}
	var edits []document.Edit
	if !noop {
		if down {
			edits = []document.Edit{{Range: rangeOf(first, 0, first, 0), Text: source + "\n"}}
		} else {
			end := len(c.Model.LineContent(last))
			edits = []document.Edit{{Range: rangeOf(last, end, last, end), Text: "\n" + source}}
		}
	}
	atLineEnd := s.IsEmpty() && s.Active.Col == len(c.Model.LineContent(s.Active.Line))
	return plan{edits: edits, custom: true, selection: func(m *mapper, _ []buffer.Range) buffer.Selection {
		result := m.track(s, atLineEnd)
		if startDelta != 0 || endDelta != 0 {
			start, end := result.Start(), result.End()
			if startDelta != 0 {
				start = posAt(start.Line+startDelta, 0)
			}
			if endDelta != 0 {
				end = posAt(end.Line+endDelta, 0)
			}
			result = buffer.SelectionFromRange(buffer.Range{Start: start, End: end}, orig.Direction())
		}
		return result
	}}
}

// DeleteLines deletes the lines the selections touch (Ctrl+Shift+K), the lines of
// cursors that touch or overlap once. The cursor of a deleted line goes to the line
// that takes its place, in the same column when it can. The selections of the
// result are in the order of the text. An empty text has nothing to delete.
//
// Ported from VS Code: linesOperations.ts (DeleteLinesAction).
func (c Commands) DeleteLines(sels []buffer.Selection) Result {
	if c.Model.LineCount() == 1 && c.Model.LineContent(0) == "" {
		return Result{Selections: append([]buffer.Selection(nil), sels...), Kind: document.KindOther}
	}
	type op struct{ start, end, col int }
	ops := make([]op, 0, len(sels))
	for _, raw := range sels {
		s := c.validSelection(raw)
		first, last, _ := lineSpan(s)
		ops = append(ops, op{first, last, s.Active.Col})
	}
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].start == ops[j].start {
			return ops[i].end < ops[j].end
		}
		return ops[i].start < ops[j].start
	})
	merged := ops[:1]
	for _, o := range ops[1:] {
		if prev := &merged[len(merged)-1]; prev.end+1 >= o.start {
			prev.end = max(prev.end, o.end)
		} else {
			merged = append(merged, o)
		}
	}

	last := lastLine(c.Model)
	res := Result{Kind: document.KindOther}
	deleted := 0
	for _, o := range merged {
		start, end := o.start, o.end
		startCol, endCol := 0, len(c.Model.LineContent(end))
		var target string // the line of the old text that is where the cursor goes
		switch {
		case end < last:
			end++
			endCol = 0
			target = c.Model.LineContent(end)
		case start > 0:
			start--
			startCol = len(c.Model.LineContent(start))
			target = c.Model.LineContent(start)
		}
		res.Edits = append(res.Edits, document.Edit{Range: rangeOf(start, startCol, end, endCol)})
		res.Selections = append(res.Selections, buffer.NewCursor(posAt(start-deleted, snapCol(target, o.col))))
		deleted += o.end - o.start + 1
	}
	return res
}

// InsertLineBefore inserts a line above the line of each cursor, with the
// indentation of the line above, and moves the cursor to it (Ctrl+Shift+Enter).
//
// Ported from VS Code: cursorTypeEditOperations.ts (EnterOperation.lineInsertBefore).
func (c Commands) InsertLineBefore(sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		line := c.validSelection(s).Active.Line
		if line == 0 {
			plans[i] = plan{
				edits:     []document.Edit{{Range: rangeOf(0, 0, 0, 0), Text: "\n"}},
				selection: func(_ *mapper, now []buffer.Range) buffer.Selection { return buffer.NewCursor(now[0].Start) },
			}
			continue
		}
		end := len(c.Model.LineContent(line - 1))
		plans[i] = c.newLinePlan(rangeOf(line-1, end, line-1, end))
	}
	return build(sels, plans, document.KindOther)
}

// InsertLineAfter inserts a line below the line of each cursor, with the
// indentation of that line, and moves the cursor to it (Ctrl+Enter).
func (c Commands) InsertLineAfter(sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		line := c.validSelection(s).Active.Line
		end := len(c.Model.LineContent(line))
		plans[i] = c.newLinePlan(rangeOf(line, end, line, end))
	}
	return build(sels, plans, document.KindOther)
}

// isJSSpace is what JavaScript's \s matches, plus the byte order mark.
func isJSSpace(r rune) bool { return unicode.IsSpace(r) || r == 0xFEFF }

// JoinLines joins each selected line with the next one (the lines of a selection
// into one), replacing the white space that separates them by one space. The
// selections of the result have the primary one first, then the others in the order
// of the text; a cursor on a line with nothing to join to is left out.
//
// Ported from VS Code: linesOperations.ts (JoinLinesAction).
func (c Commands) JoinLines(sels []buffer.Selection) Result {
	if len(sels) == 0 {
		return Result{}
	}
	primary := c.validSelection(sels[0])
	sorted := make([]buffer.Selection, len(sels))
	for i, s := range sels {
		sorted[i] = c.validSelection(s)
	}
	sort.SliceStable(sorted, func(i, j int) bool { return buffer.CompareUsingStarts(sorted[i].Range(), sorted[j].Range()) < 0 })

	var reduced []buffer.Selection
	prev := sorted[0]
	for _, cur := range sorted[1:] {
		join := func() buffer.Selection { return buffer.Selection{Anchor: prev.Start(), Active: cur.End()} }
		switch {
		case prev.IsEmpty() && prev.End().Line == cur.Start().Line:
			if primary == prev {
				primary = cur
			}
			prev = cur
		case prev.IsEmpty() && cur.Start().Line > prev.End().Line+1, !prev.IsEmpty() && cur.Start().Line > prev.End().Line:
			reduced = append(reduced, prev)
			prev = cur
		default:
			prev = join()
		}
	}
	reduced = append(reduced, prev)

	last := lastLine(c.Model)
	res := Result{Kind: document.KindOther}
	var others []buffer.Selection
	endPrimary, primarySet := primary, false
	lineOffset := 0
	var deleted []buffer.Range
	for _, s := range reduced {
		startLine := s.Start().Line
		endLine, endCol := s.End().Line, 0
		if s.IsEmpty() || startLine == s.End().Line {
			if startLine < last {
				endLine = startLine + 1
			} else {
				endLine = startLine
			}
		}
		endCol = len(c.Model.LineContent(endLine))
		endDistance := len(c.Model.LineContent(s.End().Line)) - s.End().Col

		joined := c.Model.LineContent(startLine)
		columnDelta := 0
		for l := startLine + 1; l <= endLine; l++ {
			text := c.Model.LineContent(l)
			first := firstNonWhitespace(text)
			if first < 0 {
				columnDelta = 0
				continue
			}
			space := joined != ""
			if space && (joined[len(joined)-1] == ' ' || joined[len(joined)-1] == '\t') {
				space = false
				joined = strings.TrimRightFunc(joined, isJSSpace) + " "
			}
			chunk := text[first:]
			if space {
				joined += " "
			}
			joined += chunk
			columnDelta = len(chunk)
			if space {
				columnDelta++
			}
		}

		del := rangeOf(startLine, 0, endLine, endCol)
		if !del.IsEmpty() {
			var result buffer.Selection
			switch {
			case s.IsEmpty():
				result = buffer.NewCursor(posAt(startLine-lineOffset, len(joined)-columnDelta))
			case s.Start().Line == s.End().Line:
				result = buffer.Selection{Anchor: s.Start().Delta(-lineOffset, 0), Active: s.End().Delta(-lineOffset, 0)}
			default:
				result = buffer.Selection{Anchor: s.Start().Delta(-lineOffset, 0), Active: posAt(s.Start().Line-lineOffset, len(joined)-endDistance)}
			}
			if n := len(res.Edits); n == 0 || res.Edits[n-1].Range.End.IsBeforeOrEqual(del.Start) {
				if c.textIn(del) != joined {
					res.Edits = append(res.Edits, document.Edit{Range: del, Text: joined})
				}
				deleted = append(deleted, del)
				if _, ok := del.Intersect(primary.Range()); ok {
					endPrimary, primarySet = result, true
				} else {
					others = append(others, result)
				}
			}
		}
		lineOffset += del.End.Line - del.Start.Line
	}
	if !primarySet {
		// The primary cursor was not joined: it only moves up with the lines joined above it.
		removed := 0
		for _, d := range deleted {
			if d.End.Line < primary.Start().Line {
				removed += d.End.Line - d.Start.Line
			}
		}
		endPrimary = shiftLines(primary, -removed)
	}
	res.Selections = append([]buffer.Selection{endPrimary}, others...)
	return res
}

// Transpose swaps the characters before and after each empty cursor (Ctrl+T on
// macOS, unbound on Linux): at the end of a line it swaps the last character and the
// line break. The cursor goes after the swapped pair. Cursors with a selection, and
// at the end of the text, are left alone.
//
// Ported from VS Code: linesOperations.ts (TransposeAction).
func (c Commands) Transpose(sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, raw := range sels {
		s := c.validSelection(raw)
		if !s.IsEmpty() {
			continue
		}
		pos := s.Active
		line := c.Model.LineContent(pos.Line)
		var r buffer.Range // the characters that are swapped
		var after buffer.Position
		if pos.Col >= len(line) {
			if pos.Line == lastLine(c.Model) {
				continue
			}
			start := pos.Col
			if pos.Col > 0 {
				_, w := utf8.DecodeLastRuneInString(line)
				start = pos.Col - w
			}
			r = buffer.Range{Start: posAt(pos.Line, start), End: posAt(pos.Line+1, 0)}
			after = posAt(pos.Line+1, len(line)-start)
		} else {
			start := pos.Col
			if pos.Col > 0 {
				_, w := utf8.DecodeLastRuneInString(line[:pos.Col])
				start = pos.Col - w
			}
			_, nw := utf8.DecodeRuneInString(line[pos.Col:])
			r = buffer.Range{Start: posAt(pos.Line, start), End: posAt(pos.Line, pos.Col+nw)}
			after = r.End
		}
		old := c.textIn(r)
		swapped := reverseRunes(old)
		if swapped == old {
			// Nothing changes but the cursor, which still goes after the pair.
			plans[i] = plan{custom: true, selection: func(m *mapper, _ []buffer.Range) buffer.Selection {
				return buffer.NewCursor(m.old(after))
			}}
			continue
		}
		plans[i] = replacePlan(r, swapped)
	}
	return build(sels, plans, document.KindOther)
}

func reverseRunes(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// SortLines sorts the lines the selections touch, in the order of the locale
// (Intl.Collator in VS Code), ascending or descending. A single selection inside one
// line sorts the whole text. When any selection has nothing to sort (fewer than two
// lines, or already in order), nothing changes. Selections keep the offsets they
// had in the sorted text.
//
// Ported from VS Code: sortLinesCommand.ts and AbstractSortLinesAction.
func (c Commands) SortLines(sels []buffer.Selection, descending bool) Result {
	return c.sortLines(sels, descending, true)
}

// sortLines is SortLines; with wholeText false a single selection inside one line
// has nothing to sort, which is what VS Code's SortLinesCommand does on its own.
func (c Commands) sortLines(sels []buffer.Selection, descending, wholeText bool) Result {
	res := Result{Kind: document.KindOther}
	valid := make([]buffer.Selection, len(sels))
	for i, s := range sels {
		valid[i] = c.validSelection(s)
	}
	if wholeText && len(valid) == 1 && valid[0].Start().Line == valid[0].End().Line {
		last := lastLine(c.Model)
		valid[0] = buffer.Selection{Anchor: posAt(0, 0), Active: posAt(last, len(c.Model.LineContent(last)))}
	}
	type job struct {
		first, last   int
		before, after []string
	}
	jobs := make([]job, len(valid))
	col := collate.New(language.Und)
	for i, s := range valid {
		first, last := s.Start().Line, s.End().Line
		if s.End().Col == 0 {
			last--
		}
		if first >= last {
			res.Selections = append([]buffer.Selection(nil), valid...)
			return res
		}
		before := make([]string, 0, last-first+1)
		for l := first; l <= last; l++ {
			before = append(before, c.Model.LineContent(l))
		}
		after := append([]string(nil), before...)
		sort.SliceStable(after, func(a, b int) bool { return col.CompareString(after[a], after[b]) < 0 })
		if descending {
			for a, b := 0, len(after)-1; a < b; a, b = a+1, b-1 {
				after[a], after[b] = after[b], after[a]
			}
		}
		same := true
		for k := range before {
			if before[k] != after[k] {
				same = false
				break
			}
		}
		jobs[i] = job{first, last, before, after}
		if same {
			res.Selections = append([]buffer.Selection(nil), valid...)
			return res
		}
	}
	for _, j := range jobs {
		res.Edits = append(res.Edits, document.Edit{
			Range: rangeOf(j.first, 0, j.last, len(j.before[len(j.before)-1])),
			Text:  strings.Join(j.after, "\n"),
		})
	}
	// The selections stay at the same offsets of the lines they are in.
	keep := func(p buffer.Position, j job) buffer.Position {
		if p.Line < j.first || p.Line > j.last {
			return p
		}
		off := p.Col
		for l := j.first; l < p.Line; l++ {
			off += len(j.before[l-j.first]) + 1
		}
		line := j.first
		for _, text := range j.after {
			if off <= len(text) {
				break
			}
			off -= len(text) + 1
			line++
		}
		return posAt(line, snapCol(j.after[line-j.first], off))
	}
	for i, s := range valid {
		j := jobs[i]
		res.Selections = append(res.Selections, buffer.Selection{Anchor: keep(s.Anchor, j), Active: keep(s.Active, j)})
	}
	return res
}

// TrimTrailingWhitespace deletes the spaces and tabs at the end of every line of the
// text. With keepCursors, the white space a cursor is in front of, on its line,
// stays (as when a file is trimmed while it is being typed in). Strings and regular
// expressions are trimmed too, as with files.trimTrailingWhitespaceInRegexAndStrings
// on, which is its default.
//
// Ported from VS Code: src/vs/editor/common/commands/trimTrailingWhitespaceCommand.ts
func (c Commands) TrimTrailingWhitespace(sels []buffer.Selection, keepCursors bool) Result {
	// The rightmost cursor of each line: the white space before it stays.
	cursorCol := map[int]int{}
	if keepCursors {
		for _, s := range sels {
			p := c.validSelection(s).Active
			cursorCol[p.Line] = max(cursorCol[p.Line], p.Col)
		}
	}
	var edits []document.Edit
	for line := 0; line < c.Model.LineCount(); line++ {
		text := c.Model.LineContent(line)
		from := lastNonBlank(text, len(text)-1) + 1 // where the trailing white space starts
		if col, ok := cursorCol[line]; ok {
			from = max(from, col)
		}
		if from < len(text) {
			edits = append(edits, document.Edit{Range: rangeOf(line, from, line, len(text))})
		}
	}
	return buildThrough(sels, edits, document.KindOther)
}

// buildThrough is build for edits that belong to no cursor: the selections are
// moved through them.
func buildThrough(sels []buffer.Selection, edits []document.Edit, kind document.EditKind) Result {
	// An extra cursor owns the edits; it is dropped from the result.
	all := append(append([]buffer.Selection(nil), sels...), buffer.Selection{})
	plans := make([]plan, len(all))
	plans[len(sels)] = plan{edits: edits, selection: func(*mapper, []buffer.Range) buffer.Selection { return buffer.Selection{} }}
	res := build(all, plans, kind)
	if n := len(res.Selections); n > 0 {
		res.Selections = res.Selections[:n-1]
	}
	return res
}

// Case is a change of case.
type Case int

const (
	// CaseUpper turns text into upper case.
	CaseUpper Case = iota
	// CaseLower turns text into lower case.
	CaseLower
	// CaseTitle makes the first letter of each word upper case and the others lower case.
	CaseTitle
)

var titleBoundary = regexp.MustCompile(`(?m)(^|[^\p{L}\p{N}']|((^|\P{L})'))\p{L}`)

// changeCase applies a change of case with the rules of Unicode, not of a locale.
func changeCase(text string, kind Case) string {
	switch kind {
	case CaseUpper:
		return cases.Upper(language.Und).String(text)
	case CaseLower:
		return cases.Lower(language.Und).String(text)
	}
	lower := cases.Lower(language.Und).String(text)
	return titleBoundary.ReplaceAllStringFunc(lower, func(b string) string { return cases.Upper(language.Und).String(b) })
}

// TransformCase changes the case of the selected text, or of the word at an empty
// cursor (Transform to Uppercase, Lowercase, Title Case). The selections keep their
// start and end around the changed text.
//
// Ported from VS Code: linesOperations.ts (AbstractCaseAction and its subclasses).
func (c Commands) TransformCase(sels []buffer.Selection, kind Case) Result {
	plans := make([]plan, len(sels))
	for i, raw := range sels {
		s := c.validSelection(raw)
		r := s.Range()
		if s.IsEmpty() {
			line := c.Model.LineContent(s.Active.Line)
			w, ok := c.Options.words().WordAt(line, s.Active.Col)
			if !ok {
				continue
			}
			r = buffer.Range{Start: posAt(s.Active.Line, w.Start), End: posAt(s.Active.Line, w.End)}
		}
		old := c.textIn(r)
		text := changeCase(old, kind)
		if text == old {
			continue
		}
		plans[i] = plan{
			edits: []document.Edit{{Range: r, Text: text}},
			selection: func(_ *mapper, now []buffer.Range) buffer.Selection {
				inside := func(p buffer.Position) buffer.Position {
					switch p {
					case r.Start:
						return now[0].Start
					case r.End:
						return now[0].End
					}
					// Keep the same number of characters from the start of the text.
					n := utf8.RuneCountInString(c.textIn(buffer.Range{Start: r.Start, End: p}))
					off := 0
					for ; n > 0 && off < len(text); n-- {
						_, w := utf8.DecodeRuneInString(text[off:])
						off += w
					}
					return advance(now[0].Start, text[:off])
				}
				return buffer.Selection{Anchor: inside(s.Anchor), Active: inside(s.Active)}
			},
		}
	}
	return build(sels, plans, document.KindOther)
}
