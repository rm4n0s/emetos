package workbench

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// The texts of the items of the editor in the status bar, as VS Code writes them.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorStatus.ts

// SelectionLabel is the text of the "Editor Selection" item: the line and the column of the cursor, with the
// number of characters selected, or the number of selections for several cursors. It is empty without a cursor.
func SelectionLabel(selections, line, column, characters int) string {
	switch {
	case selections <= 0:
		return ""
	case selections == 1 && characters > 0:
		return "Ln " + strconv.Itoa(line) + ", Col " + strconv.Itoa(column) + " (" + strconv.Itoa(characters) + " selected)"
	case selections == 1:
		return "Ln " + strconv.Itoa(line) + ", Col " + strconv.Itoa(column)
	case characters > 0:
		return strconv.Itoa(selections) + " selections (" + strconv.Itoa(characters) + " characters selected)"
	}
	return strconv.Itoa(selections) + " selections"
}

// SelectionStatus makes the text of the "Editor Selection" item for the selections of a text and its primary one. The column is the
// one the status bar shows (edit.StatusbarColumn: a tab goes to the next tab stop and every other code point is
// one column), and the characters are the code points of the selections, a line break counting as many as the
// line ending of the document has (two for CRLF).
func SelectionStatus(text buffer.Snapshot, selections []buffer.Selection, primary buffer.Selection, tabSize int, eol fileio.LineEnding) string {
	if len(selections) == 0 {
		return ""
	}
	characters := 0
	for _, s := range selections {
		characters += CharacterCount(text, s, eol)
	}
	active := primary.Active
	line, _ := text.LineContent(active.Line)
	return SelectionLabel(len(selections), active.Line+1, edit.StatusbarColumn(line, active.Col, tabSize), characters)
}

// CharacterCount is the number of characters in a selection, as VS Code counts them: every code point is one
// and every line break is the length of the line ending of the document (VS Code's getCharacterCountInRange).
func CharacterCount(text buffer.Snapshot, s buffer.Selection, eol fileio.LineEnding) int {
	from, to := s.Anchor, s.Active
	if to.Line < from.Line || (to.Line == from.Line && to.Col < from.Col) {
		from, to = to, from
	}
	n := 0
	for line := from.Line; line <= to.Line; line++ {
		content, err := text.LineContent(line)
		if err != nil {
			break
		}
		start, end := 0, len(content)
		if line == from.Line {
			start = min(max(from.Col, 0), len(content))
		}
		if line == to.Line {
			end = min(max(to.Col, 0), len(content))
		}
		if end > start {
			n += utf8.RuneCountInString(content[start:end])
		}
	}
	if eol == "" {
		eol = fileio.LF
	}
	return n + len(eol)*(to.Line-from.Line)
}

// IndentationLabel is the text of the "Editor Indentation" item: "Spaces: 4" for a document that indents with
// spaces and "Tab Size: 4" for one that indents with tabs. (VS Code adds the tab size in brackets when its
// indent size is another; Emetos has one size for both.)
func IndentationLabel(tabSize int, insertSpaces bool) string {
	if insertSpaces {
		return "Spaces: " + strconv.Itoa(tabSize)
	}
	return "Tab Size: " + strconv.Itoa(tabSize)
}

// EncodingLabel is the text of the "Editor Encoding" item: the short label of the encoding ("UTF-8",
// "Windows 1252"), or the id itself for an encoding that has none.
func EncodingLabel(id string) string {
	if e, ok := fileio.LookupEncoding(id); ok {
		return e.LabelShort
	}
	return id
}

// EOLLabel is the text of the "Editor End of Line" item: CRLF or LF.
func EOLLabel(le fileio.LineEnding) string {
	if le == fileio.CRLF {
		return "CRLF"
	}
	return "LF"
}

// ProblemCounts are the numbers the Problems item shows.
type ProblemCounts struct {
	Errors, Warnings, Infos int
}

// packNumber writes a count as the status bar does: "999", "1K" for 1000 to 9999 (the first digit) and "10K+".
func packNumber(n int) string {
	switch {
	case n > 9999:
		return "10K+"
	case n > 999:
		return strconv.Itoa(n)[:1] + "K"
	}
	return strconv.Itoa(n)
}

// ProblemsText is the text of the Problems item: the errors and the warnings, and the infos when there are any.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markers.contribution.ts (MarkersStatusBarContributions)
func ProblemsText(c ProblemCounts) string {
	text := "$(error) " + packNumber(c.Errors) + " $(warning) " + packNumber(c.Warnings)
	if c.Infos > 0 {
		text += " $(info) " + packNumber(c.Infos)
	}
	return text
}

// ProblemsTooltip is the hover text of the Problems item: the counts that are not zero, or "No Problems".
func ProblemsTooltip(c ProblemCounts) string {
	var titles []string
	if c.Errors > 0 {
		titles = append(titles, "Errors: "+strconv.Itoa(c.Errors))
	}
	if c.Warnings > 0 {
		titles = append(titles, "Warnings: "+strconv.Itoa(c.Warnings))
	}
	if c.Infos > 0 {
		titles = append(titles, "Infos: "+strconv.Itoa(c.Infos))
	}
	if len(titles) == 0 {
		return "No Problems"
	}
	return strings.Join(titles, ", ")
}
