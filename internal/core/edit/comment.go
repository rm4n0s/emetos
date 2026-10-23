package edit

import (
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/comment"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// CommentKind is what a line comment command does with the lines it is given.
type CommentKind int

// The kinds of line comment command.
const (
	// CommentToggle removes the comments when every line has one, and adds them otherwise.
	CommentToggle CommentKind = iota
	// CommentAdd adds a comment to every line, whatever it has.
	CommentAdd
	// CommentRemove removes the comments the lines have.
	CommentRemove
)

// ToggleLineComment is Toggle Line Comment (Ctrl+/): the lines the selections touch get a line
// comment, or lose theirs when they all have one. A language with no line comment uses its block
// comment (SPEC §8.6.5).
//
// Ported from VS Code: src/vs/editor/contrib/comment/browser/comment.ts (CommentLineAction)
func (c Commands) ToggleLineComment(sels []buffer.Selection) Result {
	return c.lineComments(sels, CommentToggle)
}

// AddLineComment is Add Line Comment: every line the selections touch gets a line comment.
func (c Commands) AddLineComment(sels []buffer.Selection) Result {
	return c.lineComments(sels, CommentAdd)
}

// RemoveLineComment is Remove Line Comment: the line comments of the lines the selections touch
// are taken off.
func (c Commands) RemoveLineComment(sels []buffer.Selection) Result {
	return c.lineComments(sels, CommentRemove)
}

// ToggleBlockComment is Toggle Block Comment (Ctrl+Shift+A): each selection is wrapped in a block
// comment, or, when it is in one, the comment is taken off.
//
// Ported from VS Code: comment.ts (BlockCommentAction)
func (c Commands) ToggleBlockComment(sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		plans[i] = c.blockCommentPlan(c.validSelection(s))
	}
	return build(sels, plans, document.KindOther)
}

// lineComments runs a line comment command for each selection. The selections are taken in the
// order of their starts, and a line that two of them share is left to the one that came first.
func (c Commands) lineComments(sels []buffer.Selection, kind CommentKind) Result {
	type item struct {
		sel         buffer.Selection
		index       int
		ignoreFirst bool
	}
	items := make([]item, len(sels))
	for i, s := range sels {
		items[i] = item{sel: c.validSelection(s), index: i}
	}
	sort.SliceStable(items, func(i, j int) bool { return buffer.CompareUsingStarts(items[i].sel.Range(), items[j].sel.Range()) < 0 })
	// Selections that would comment the same line twice.
	if len(items) > 0 {
		prev := 0
		for i := 1; i < len(items); i++ {
			if items[prev].sel.End().Line == items[i].sel.Start().Line {
				if items[prev].index < items[i].index {
					items[i].ignoreFirst = true
				} else {
					items[prev].ignoreFirst = true
					prev = i
				}
			}
		}
	}
	ordered := make([]buffer.Selection, len(items))
	plans := make([]plan, len(items))
	for i, it := range items {
		ordered[i] = it.sel
		plans[i] = c.lineCommentPlan(it.sel, kind, it.ignoreFirst)
	}
	return build(ordered, plans, document.KindOther)
}

// languageAt is the code of the language at a position: that of the token there, else of the
// document.
func (c Commands) languageAt(line, col int) int {
	src := c.Options.Tokens
	if src == nil {
		return 0
	}
	if !src.CheapToTokenize(line) {
		return src.Language()
	}
	toks := src.LineTokens(line)
	if len(toks) == 0 {
		return src.Language()
	}
	for _, t := range toks {
		if col < int(t.End) {
			return int(t.Language)
		}
	}
	return int(toks[len(toks)-1].Language)
}

// commentsAt are the comment tokens of the language at a position.
func (c Commands) commentsAt(line, col int) *comment.Language {
	if c.Options.Comments == nil {
		return &comment.Language{}
	}
	return c.Options.Comments.Language(c.languageAt(line, col))
}

// linePreflight is what the analysis of a line decides.
type linePreflight struct {
	// ignore leaves the line alone.
	ignore bool
	// commentStr is the line comment token.
	commentStr string
	// commentStrOffset is the byte column the token goes at, or is at.
	commentStrOffset int
	// commentStrLength is the length of the token to remove, with the space after it.
	commentStrLength int
}

// insertionPoint is what normalizeInsertionPoint reads and changes of a line.
type insertionPoint struct {
	ignore           bool
	commentStrOffset int
}

// haystackHasNeedleAtOffset reports whether haystack has needle at a byte offset, ignoring the case
// of ASCII letters.
//
// Ported from VS Code: blockCommentCommand.ts (_haystackHasNeedleAtOffset)
func haystackHasNeedleAtOffset(haystack, needle string, offset int) bool {
	if offset < 0 || offset+len(needle) > len(haystack) {
		return false
	}
	for i := 0; i < len(needle); i++ {
		a, b := haystack[offset+i], needle[i]
		if a == b {
			continue
		}
		if a >= 'A' && a <= 'Z' && a+32 == b || b >= 'A' && b <= 'Z' && b+32 == a {
			continue
		}
		return false
	}
	return true
}

// analyzeLines decides which lines count and whether the command removes comments or adds them,
// and fills in where each comment goes. line gives the content of the lines.
//
// Ported from VS Code: lineCommentCommand.ts (_analyzeLines)
func analyzeLines(kind CommentKind, insertSpace bool, line func(i int) string, lines []linePreflight, ignoreEmptyLines, ignoreFirstLine, noIndent bool) (shouldRemove bool) {
	onlyWhitespace := true
	shouldRemove = kind != CommentAdd
	for i := range lines {
		data := &lines[i]
		if i == 0 && ignoreFirstLine {
			data.ignore = true
			continue
		}
		content := line(i)
		start := firstNonWhitespace(content)
		if start < 0 {
			// An empty line, or one with white space only.
			data.ignore = ignoreEmptyLines
			data.commentStrOffset = len(content)
			if noIndent {
				data.commentStrOffset = 0
			}
			continue
		}
		onlyWhitespace = false
		offset := start
		if noIndent {
			offset = 0
		}
		data.ignore = false
		data.commentStrOffset = offset
		if shouldRemove && !haystackHasNeedleAtOffset(content, data.commentStr, offset) {
			switch kind {
			case CommentToggle:
				// Every line so far has had a comment, but this one has none.
				shouldRemove = false
			case CommentRemove:
				data.ignore = true
			}
		}
		if shouldRemove && insertSpace {
			// A space after the token goes with it.
			if end := start + data.commentStrLength; end < len(content) && content[end] == ' ' {
				data.commentStrLength++
			}
		}
	}
	if kind == CommentToggle && onlyWhitespace {
		// Lines of white space get comments.
		shouldRemove = false
		for i := range lines {
			lines[i].ignore = false
		}
	}
	return shouldRemove
}

// commentNextColumn is the visible column after a character.
func commentNextColumn(column, indentSize int, isTab bool) int {
	if isTab {
		return column + (indentSize - column%indentSize)
	}
	return column + 1
}

// normalizeInsertionPoint moves the insertion points so that the comments line up: at the
// smallest indentation of the lines, rounded down to a multiple of the indent size.
//
// Ported from VS Code: lineCommentCommand.ts (_normalizeInsertionPoint)
func normalizeInsertionPoint(line func(i int) string, lines []insertionPoint, indentSize int) {
	const maxSmall = 1<<30 - 1
	minVisible := maxSmall
	for i := range lines {
		if lines[i].ignore {
			continue
		}
		content := line(i)
		current := 0
		for j := 0; current < minVisible && j < lines[i].commentStrOffset; j++ {
			current = commentNextColumn(current, indentSize, content[j] == '\t')
		}
		minVisible = min(minVisible, current)
	}
	minVisible = minVisible / indentSize * indentSize
	for i := range lines {
		if lines[i].ignore {
			continue
		}
		content := line(i)
		current, j := 0, 0
		for ; current < minVisible && j < lines[i].commentStrOffset; j++ {
			current = commentNextColumn(current, indentSize, content[j] == '\t')
		}
		if current > minVisible {
			lines[i].commentStrOffset = j - 1
		} else {
			lines[i].commentStrOffset = j
		}
	}
}

// lineCommentPlan is the line comment command for one selection.
//
// Ported from VS Code: lineCommentCommand.ts (LineCommentCommand)
func (c Commands) lineCommentPlan(s buffer.Selection, kind CommentKind, ignoreFirstLine bool) plan {
	o := c.Options
	if s.Start().Line == s.End().Line && ignoreFirstLine {
		// The line belongs to another selection: this one only keeps its place.
		line := s.Start().Line
		end := lineEnd(c.Model, line)
		edit := document.Edit{Range: buffer.Range{Start: end, End: end}}
		if line < lastLine(c.Model) {
			edit = document.Edit{Range: buffer.Range{Start: end, End: buffer.Position{Line: line + 1}}, Text: "\n"}
		}
		return plan{edits: []document.Edit{edit}, selection: c.trackedSelection(s, false, 0)}
	}
	moveEndDown := false
	if s.Start().Line < s.End().Line && s.End().Col == 0 {
		moveEndDown = true
		s = s.WithEnd(lineEnd(c.Model, s.End().Line-1))
	}
	first, last := s.Start().Line, s.End().Line
	lang := c.commentsAt(first, 0)
	if lang.Line == "" {
		return c.blockFallbackPlan(s, moveEndDown)
	}
	lines := make([]linePreflight, last-first+1)
	for i := range lines {
		lines[i] = linePreflight{commentStr: lang.Line, commentStrLength: len(lang.Line)}
	}
	text := func(i int) string { return c.Model.LineContent(first + i) }
	remove := analyzeLines(kind, o.CommentInsertSpace, text, lines, o.CommentIgnoreEmptyLines, ignoreFirstLine, lang.NoIndent)

	var edits []document.Edit
	if remove {
		for i, l := range lines {
			if l.ignore {
				continue
			}
			edits = append(edits, document.Edit{Range: buffer.Range{
				Start: buffer.Position{Line: first + i, Col: l.commentStrOffset},
				End:   buffer.Position{Line: first + i, Col: l.commentStrOffset + l.commentStrLength},
			}})
		}
	} else {
		points := make([]insertionPoint, len(lines))
		for i, l := range lines {
			points[i] = insertionPoint{ignore: l.ignore, commentStrOffset: l.commentStrOffset}
		}
		normalizeInsertionPoint(text, points, o.indentSize())
		after := ""
		if o.CommentInsertSpace {
			after = " "
		}
		for i, l := range lines {
			if l.ignore {
				continue
			}
			at := buffer.Position{Line: first + i, Col: points[i].commentStrOffset}
			edits = append(edits, document.Edit{Range: buffer.Range{Start: at, End: at}, Text: l.commentStr + after})
		}
	}
	// A cursor at the end of its line, where a comment is added, goes after the comment.
	delta := 0
	cursor := s.Active
	for _, e := range edits {
		if e.Range.IsEmpty() && e.Range.Start == cursor && len(c.Model.LineContent(cursor.Line)) == cursor.Col {
			delta = len(e.Text)
		}
	}
	return plan{edits: edits, custom: true, selection: c.trackedSelection(s, moveEndDown, delta)}
}

// trackedSelection is the selection after a comment command: the selection follows the edits (an
// empty one at the end of its line stays before text typed there), moves down to the start of
// the line after it if it ended there, and shifts by delta columns.
func (c Commands) trackedSelection(s buffer.Selection, moveEndDown bool, delta int) func(*mapper, []buffer.Range) buffer.Selection {
	atLineEnd := s.IsEmpty() && s.Active.Col == len(c.Model.LineContent(s.Active.Line))
	return func(m *mapper, _ []buffer.Range) buffer.Selection {
		r := m.track(s, atLineEnd)
		if moveEndDown {
			r = r.WithEnd(buffer.Position{Line: r.End().Line + 1})
		}
		r.Anchor.Col += delta
		r.Active.Col += delta
		return r
	}
}

// blockFallbackPlan comments a selection with the block comment of the language when it has no
// line comment, or takes the block comment off.
//
// Ported from VS Code: lineCommentCommand.ts (_executeBlockComment, _attemptRemoveBlockComment)
func (c Commands) blockFallbackPlan(s buffer.Selection, moveEndDown bool) plan {
	lang := c.commentsAt(s.Start().Line, 0)
	if !lang.HasBlock() {
		return plan{}
	}
	startToken, endToken := lang.BlockStart, lang.BlockEnd
	edits := c.attemptRemoveBlockComment(s, startToken, endToken)
	delta := 0
	if edits == nil {
		var r buffer.Range
		if s.IsEmpty() {
			line := c.Model.LineContent(s.Start().Line)
			from := firstNonWhitespace(line)
			if from < 0 {
				from = len(line)
			}
			r = buffer.Range{Start: buffer.Position{Line: s.Start().Line, Col: from}, End: buffer.Position{Line: s.Start().Line, Col: len(line)}}
		} else {
			from := max(firstNonWhitespace(c.Model.LineContent(s.Start().Line)), 0)
			r = buffer.Range{Start: buffer.Position{Line: s.Start().Line, Col: from}, End: lineEnd(c.Model, s.End().Line)}
		}
		edits = createAddBlockCommentEdits(r, startToken, endToken, c.Options.CommentInsertSpace)
		if len(edits) == 1 {
			// The cursor stays after the token and the space.
			delta = len(startToken) + 1
		}
	}
	return plan{edits: edits, custom: true, selection: c.trackedSelection(s, moveEndDown, delta)}
}

// attemptRemoveBlockComment finds the block comment a selection is in, and the edits that remove it.
func (c Commands) attemptRemoveBlockComment(s buffer.Selection, startToken, endToken string) []document.Edit {
	startLine, endLine := s.Start().Line, s.End().Line
	startText := c.Model.LineContent(startLine)
	// VS Code's getLineFirstNonWhitespaceColumn is 0 for a line of white space, else a column from 1.
	firstColumn := 0
	if f := firstNonWhitespace(startText); f >= 0 {
		firstColumn = f + 1
	}
	allowedBefore := len(endToken) + max(firstColumn, s.Start().Col+1) - 1
	startIndex := lastIndexFrom(startText, startToken, allowedBefore)
	endIndex := indexFrom(c.Model.LineContent(endLine), endToken, s.End().Col-len(startToken))
	if startIndex >= 0 && endIndex < 0 {
		endIndex = indexFrom(startText, endToken, startIndex+len(startToken))
		endLine = startLine
	}
	if startIndex < 0 && endIndex >= 0 {
		startIndex = lastIndexFrom(c.Model.LineContent(endLine), startToken, endIndex)
		startLine = endLine
	}
	if s.IsEmpty() && (startIndex < 0 || endIndex < 0) {
		startIndex = strings.Index(c.Model.LineContent(startLine), startToken)
		if startIndex >= 0 {
			endIndex = indexFrom(c.Model.LineContent(startLine), endToken, startIndex+len(startToken))
		}
	}
	// White space inside the tokens goes with them.
	if startIndex >= 0 {
		if line := c.Model.LineContent(startLine); startIndex+len(startToken) < len(line) && line[startIndex+len(startToken)] == ' ' {
			startToken += " "
		}
	}
	if endIndex >= 0 {
		if line := c.Model.LineContent(endLine); endIndex > 0 && line[endIndex-1] == ' ' {
			endToken = " " + endToken
			endIndex--
		}
	}
	if startIndex >= 0 && endIndex >= 0 {
		return createRemoveBlockCommentEdits(buffer.Range{
			Start: buffer.Position{Line: startLine, Col: startIndex + len(startToken)},
			End:   buffer.Position{Line: endLine, Col: endIndex},
		}, startToken, endToken)
	}
	return nil
}

// indexFrom is JavaScript's indexOf with a start: the first index of sub at or after from.
func indexFrom(s, sub string, from int) int {
	from = min(max(from, 0), len(s))
	i := strings.Index(s[from:], sub)
	if i < 0 {
		return -1
	}
	return from + i
}

// lastIndexFrom is JavaScript's lastIndexOf with a start: the last index of sub at or before from.
func lastIndexFrom(s, sub string, from int) int {
	if from < 0 {
		from = 0
	}
	return strings.LastIndex(s[:min(len(s), from+len(sub))], sub)
}

// createRemoveBlockCommentEdits are the edits that take the tokens off the two ends of the text r
// (the text between them).
//
// Ported from VS Code: blockCommentCommand.ts (_createRemoveBlockCommentOperations)
func createRemoveBlockCommentEdits(r buffer.Range, startToken, endToken string) []document.Edit {
	if !r.IsEmpty() {
		return []document.Edit{
			{Range: buffer.Range{Start: r.Start.Delta(0, -len(startToken)), End: r.Start}},
			{Range: buffer.Range{Start: r.End, End: r.End.Delta(0, len(endToken))}},
		}
	}
	return []document.Edit{{Range: buffer.Range{Start: r.Start.Delta(0, -len(startToken)), End: r.End.Delta(0, len(endToken))}}}
}

// createAddBlockCommentEdits are the edits that put the tokens around the text r; for an empty
// range the tokens and two spaces replace it.
//
// Ported from VS Code: blockCommentCommand.ts (_createAddBlockCommentOperations)
func createAddBlockCommentEdits(r buffer.Range, startToken, endToken string, insertSpace bool) []document.Edit {
	if !r.IsEmpty() {
		space := ""
		if insertSpace {
			space = " "
		}
		return []document.Edit{
			{Range: buffer.Range{Start: r.Start, End: r.Start}, Text: startToken + space},
			{Range: buffer.Range{Start: r.End, End: r.End}, Text: space + endToken},
		}
	}
	return []document.Edit{{Range: r, Text: startToken + "  " + endToken}}
}

// blockCommentPlan is the block comment command for one selection.
//
// Ported from VS Code: blockCommentCommand.ts (BlockCommentCommand)
func (c Commands) blockCommentPlan(s buffer.Selection) plan {
	start := s.Start()
	lang := c.commentsAt(start.Line, start.Col)
	if !lang.HasBlock() {
		return plan{}
	}
	startToken, endToken := lang.BlockStart, lang.BlockEnd
	end := s.End()
	insertSpace := c.Options.CommentInsertSpace
	startText, endText := c.Model.LineContent(start.Line), c.Model.LineContent(end.Line)
	startIndex := lastIndexFrom(startText, startToken, start.Col+len(startToken))
	endIndex := indexFrom(endText, endToken, end.Col-len(endToken))
	if startIndex >= 0 && endIndex >= 0 {
		// A block comment that has an end token inside it is not one to remove.
		if start.Line == end.Line {
			if strings.Contains(startText[min(startIndex+len(startToken), endIndex):max(startIndex+len(startToken), endIndex)], endToken) {
				startIndex, endIndex = -1, -1
			}
		} else if strings.Contains(startText[startIndex+len(startToken):], endToken) || strings.Contains(endText[:endIndex], endToken) {
			startIndex, endIndex = -1, -1
		}
	}
	if startIndex >= 0 && endIndex >= 0 {
		// White space next to the tokens counts as part of them.
		if insertSpace && startIndex+len(startToken) < len(startText) && startText[startIndex+len(startToken)] == ' ' {
			startToken += " "
		}
		if insertSpace && endIndex > 0 && endText[endIndex-1] == ' ' {
			endToken = " " + endToken
			endIndex--
		}
		edits := createRemoveBlockCommentEdits(buffer.Range{
			Start: buffer.Position{Line: start.Line, Col: startIndex + len(startToken)},
			End:   buffer.Position{Line: end.Line, Col: endIndex},
		}, startToken, endToken)
		return plan{edits: edits, selection: blockCommentSelection(edits, "")}
	}
	edits := createAddBlockCommentEdits(buffer.Range{Start: start, End: end}, startToken, endToken, insertSpace)
	used := ""
	if len(edits) == 1 {
		used = endToken
	}
	return plan{edits: edits, selection: blockCommentSelection(edits, used)}
}

// blockCommentSelection is the selection after a block comment command: between the two tokens,
// or, when the tokens were added to an empty range, between their two spaces.
func blockCommentSelection(edits []document.Edit, usedEndToken string) func(*mapper, []buffer.Range) buffer.Selection {
	return func(_ *mapper, now []buffer.Range) buffer.Selection {
		if len(edits) == 2 {
			return buffer.Selection{Anchor: now[0].End, Active: now[1].Start}
		}
		at := now[0].End
		if usedEndToken != "" {
			at = at.Delta(0, -len(usedEndToken)-1)
		}
		return buffer.NewCursor(at)
	}
}
