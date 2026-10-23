package edit

import (
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// CopyData is what a copy puts on the clipboard.
type CopyData struct {
	// Text is the text for the clipboard: the pieces of the cursors, one after the other
	// with a line break between them.
	Text string
	// Pieces is the text of each cursor, in the order of the text, when there is more than one
	// piece; a paste of exactly as many cursors gives each its own piece. It is nil for one
	// piece.
	Pieces []string
	// FromEmptySelection says the text is a whole line copied from a cursor with no selection;
	// pasting it puts the line above the line of the cursor.
	FromEmptySelection bool
}

// Copy gets the text to copy for the selections. With emptySelectionClipboard a cursor with no
// selection copies its line (with the line break), once for the cursors on the same line, and
// a copy of nothing but empty selections without it copies nothing: Text is empty and Pieces
// nil.
//
// Ported from VS Code: src/vs/editor/common/viewModel/viewModelImpl.ts (getPlainTextToCopy) and
// src/vs/editor/browser/controller/editContext/clipboardUtils.ts (getDataToCopy)
func (c Commands) Copy(sels []buffer.Selection, emptySelectionClipboard bool) CopyData {
	ranges := make([]buffer.Range, len(sels))
	for i, s := range sels {
		ranges[i] = c.validRange(s)
	}
	sort.SliceStable(ranges, func(i, j int) bool { return buffer.CompareUsingStarts(ranges[i], ranges[j]) < 0 })

	hasEmpty, hasNonEmpty := false, false
	for _, r := range ranges {
		if r.IsEmpty() {
			hasEmpty = true
		} else {
			hasNonEmpty = true
		}
	}
	if !hasNonEmpty && !emptySelectionClipboard {
		return CopyData{}
	}

	var pieces []string
	if hasEmpty && emptySelectionClipboard {
		prev := -1
		for _, r := range ranges {
			line := r.Start.Line
			switch {
			case !r.IsEmpty():
				pieces = append(pieces, c.textIn(r))
			case line != prev:
				pieces = append(pieces, c.Model.LineContent(line)+"\n")
			}
			prev = line
		}
	} else {
		for _, r := range ranges {
			if !r.IsEmpty() {
				pieces = append(pieces, c.textIn(r))
			}
		}
	}
	data := CopyData{
		FromEmptySelection: emptySelectionClipboard && len(ranges) == 1 && ranges[0].IsEmpty(),
		Text:               strings.Join(pieces, "\n"),
	}
	if len(pieces) != 1 {
		data.Pieces = pieces
	}
	return data
}

// Cut deletes what Copy copies: the selections, and the line of a cursor with no selection
// when emptySelectionClipboard is set (with its line break; the last line takes the break
// before it). Cursors with no selection do nothing without it.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorDeleteOperations.ts (DeleteOperations.cut)
func (c Commands) Cut(sels []buffer.Selection, emptySelectionClipboard bool) Result {
	order := make([]int, len(sels))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return c.validRange(sels[order[i]]).Start.Compare(c.validRange(sels[order[j]]).End) < 0
	})
	plans := make([]plan, len(sels))
	var lastCut *buffer.Range
	last := c.Model.LineCount() - 1
	for _, i := range order {
		r := c.validRange(sels[i])
		if !r.IsEmpty() {
			plans[i] = replacePlan(r, "")
			continue
		}
		if !emptySelectionClipboard {
			continue
		}
		line := r.Start.Line
		var del buffer.Range
		switch {
		case line < last:
			del = buffer.Range{Start: posAt(line, 0), End: posAt(line+1, 0)}
		case line > 0 && (lastCut == nil || lastCut.End.Line != line):
			del = buffer.Range{Start: lineEnd(c.Model, line-1), End: lineEnd(c.Model, line)}
		default:
			del = buffer.Range{Start: posAt(line, 0), End: lineEnd(c.Model, line)}
		}
		lastCut = &del
		if !del.IsEmpty() {
			plans[i] = replacePlan(del, "")
		}
	}
	return build(sels, plans, document.KindOther)
}

// PasteOptions say how a paste goes.
type PasteOptions struct {
	// OnNewLine says the text is a whole line that was copied from a cursor with no selection:
	// it goes above the line of the cursor and the cursor stays where it was in its text.
	OnNewLine bool
	// Pieces is the text of each cursor of the copy the text comes from, when it came from
	// several cursors.
	Pieces []string
	// Block says the text was copied from a column selection.
	Block bool
	// Spread is editor.multiCursorPaste being "spread": a text with as many lines as there are
	// cursors goes line by line to the cursors, and a block goes down the lines under a single
	// cursor.
	Spread bool
}

// Paste puts a text at the selections, replacing them. With several cursors, the pieces of a
// copy by as many cursors go to the cursors from the top; so does, with Spread, a text that has
// as many lines as there are cursors. A block copied from a column selection into one cursor
// goes down the lines. Otherwise every cursor gets the whole text. The edit is an undo step of
// its own.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorTypeEditOperations.ts (PasteOperation)
func (c Commands) Paste(sels []buffer.Selection, text string, opts PasteOptions) Result {
	text = normalizeNewlines(text)
	if opts.Block && opts.Spread && len(sels) == 1 {
		return c.blockPaste(sels[0], splitLines(text))
	}
	if pieces := distributePaste(len(sels), text, opts); pieces != nil {
		return c.distributedPaste(sels, pieces)
	}
	return c.simplePaste(sels, text, opts.OnNewLine)
}

// distributePaste is the text for each cursor when it is not the same for all, or nil.
func distributePaste(cursors int, text string, opts PasteOptions) []string {
	if cursors == 1 {
		return nil
	}
	if len(opts.Pieces) == cursors {
		return opts.Pieces
	}
	if opts.OnNewLine || !opts.Spread {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\r")
	if lines := splitLines(text); len(lines) == cursors {
		return lines
	}
	return nil
}

// splitLines splits at "\r\n", "\r" and "\n".
func splitLines(text string) []string {
	return strings.Split(normalizeNewlines(text), "\n")
}

// replaceRange is the range that a paste replaces for a selection: the selection, grown in
// overtype mode as typing grows it.
func (c Commands) replaceRange(s buffer.Selection, text string) buffer.Range {
	r := c.validRange(s)
	if c.Options.Overtype {
		r = c.overtypeRange(r, text)
	}
	return r
}

// distributedPaste gives each cursor its own text, the first to the cursor nearest the top.
func (c Commands) distributedPaste(sels []buffer.Selection, pieces []string) Result {
	order := make([]int, len(sels))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return buffer.CompareUsingStarts(c.validRange(sels[order[i]]), c.validRange(sels[order[j]])) < 0
	})
	plans := make([]plan, len(sels))
	for rank, i := range order {
		plans[i] = replacePlan(c.replaceRange(sels[i], pieces[rank]), pieces[rank])
	}
	return build(sels, plans, document.KindOther)
}

// simplePaste gives every cursor the whole text. A whole line from an empty selection goes at
// the start of the line of an empty cursor, and the cursor keeps its place in the text it was
// in; it falls back to a plain paste for a cursor with a selection or text that is not exactly
// one line.
func (c Commands) simplePaste(sels []buffer.Selection, text string, onNewLine bool) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		if onNewLine && !s.IsEmpty() {
			onNewLine = false
		}
		if onNewLine && strings.Index(text, "\n") != len(text)-1 {
			onNewLine = false
		}
		if onNewLine {
			at := c.Model.ValidatePosition(s.Active)
			s := s
			plans[i] = plan{
				edits:     []document.Edit{{Range: buffer.Range{Start: posAt(at.Line, 0), End: posAt(at.Line, 0)}, Text: text}},
				selection: func(m *mapper, _ []buffer.Range) buffer.Selection { return m.track(s, false) },
			}
			continue
		}
		plans[i] = replacePlan(c.replaceRange(s, text), text)
	}
	return build(sels, plans, document.KindOther)
}

// blockPaste puts the lines of a block down the lines from the selection, each at the same
// visual column, padded to the width of the block, and past the end of the text when the
// lines run out; the cursor goes to the end of the last line.
//
// Ported from VS Code: src/vs/editor/common/commands/columnSelectionPasteCommand.ts
func (c Commands) blockPaste(sel buffer.Selection, lines []string) Result {
	tab := c.Options.tabSize()
	r := c.validRange(sel)
	visible := VisibleColumn(c.Model.LineContent(r.Start.Line), r.Start.Col, tab)
	prefix := strings.Repeat(" ", visible)

	// Pad every line to the width of the widest.
	ends := make([]int, len(lines))
	blockEnd := visible
	for i, text := range lines {
		withPrefix := prefix + text
		ends[i] = VisibleColumn(withPrefix, len(withPrefix), tab)
		blockEnd = max(blockEnd, ends[i])
	}
	padded := make([]string, len(lines))
	for i, text := range lines {
		padded[i] = text + strings.Repeat(" ", blockEnd-ends[i])
	}

	var edits []document.Edit
	lastLine := c.Model.LineCount() - 1
	for i, text := range padded {
		line := r.End.Line + i
		if line > lastLine {
			end := lineEnd(c.Model, lastLine)
			appended := "\n" + prefix + strings.Join(padded[i:], "\n"+prefix)
			edits = append(edits, document.Edit{Range: buffer.Range{Start: end, End: end}, Text: appended})
			break
		}
		rng := r
		if i > 0 {
			content := c.Model.LineContent(line)
			col := ColumnFromVisible(content, visible, tab)
			rng = buffer.Range{Start: posAt(line, col), End: posAt(line, col)}
			if col == len(content) {
				text = strings.Repeat(" ", max(0, visible-VisibleColumn(content, col, tab))) + text
			}
		}
		edits = append(edits, document.Edit{Range: rng, Text: text})
	}
	p := plan{
		edits:     edits,
		selection: func(_ *mapper, now []buffer.Range) buffer.Selection { return buffer.NewCursor(now[len(now)-1].End) },
	}
	return build([]buffer.Selection{sel}, []plan{p}, document.KindOther)
}
