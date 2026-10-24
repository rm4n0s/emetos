package quickinput

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/edit"
)

// Ported from VS Code: src/vs/editor/contrib/quickAccess/browser/gotoLineQuickAccess.ts,
// src/vs/editor/contrib/quickAccess/browser/editorNavigationQuickAccess.ts and
// src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoLineQuickAccess.ts

// The prefixes of the go to line modes and the ids of the commands that open them.
const (
	// GoToLinePrefix is the prefix of the mode that goes to a line and a column.
	GoToLinePrefix = ":"
	// GoToOffsetPrefix is the prefix of the mode that goes to a character position.
	GoToOffsetPrefix = "::"
	// GoToLineCommand is Go to Line/Column.
	GoToLineCommand = "workbench.action.gotoLine"
	// GoToOffsetCommand is Go to Offset.
	GoToOffsetCommand = "workbench.action.gotoOffset"
	// goToLinePlaceholder is the hint of both modes.
	goToLinePlaceholder = "Type the line number and optional column to go to (e.g. :42:5 for line 42, column 5). Type :: to go to a character offset (e.g. ::1024 for character 1024 from the start of the file). Use negative values to navigate backwards."
	// noEditorLabel is the only pick without a text editor.
	noEditorLabel = "Open a text editor first to go to a line or an offset."
)

// GoToLineEditor is the editor the go to line modes work on. Lines and columns count from 1; a
// column is a byte column of the line. The provider reveals a line while the user types and puts
// the editor back as it was when the user gives up.
type GoToLineEditor interface {
	// LineCount is the number of lines of the text.
	LineCount() int
	// LineContent is the text of a line, without its line break.
	LineContent(line int) string
	// TabSize is the width of a tab, for the visible column the user types.
	TabSize() int
	// Length is the length of the text, in the units of an offset (bytes).
	Length() int
	// PositionAt is the line and the column of an offset.
	PositionAt(offset int) (line, col int)
	// Reveal scrolls to put the position in the middle of the editor and highlights its line.
	Reveal(line, col int)
	// ClearHighlight takes the highlight of the line away.
	ClearHighlight()
	// Go puts the cursor at the position, reveals it and gives the focus to the editor.
	Go(line, col int)
	// Restore puts back the cursors and the scroll the editor had when the mode opened.
	Restore()
}

// GoToLinePosition is what the text typed in the go to line mode means.
type GoToLinePosition struct {
	// Line and Column are the position to go to, from 1; Line is 0 when the text does not name one.
	Line, Column int
	// InOffsetMode tells that the text started with a second colon.
	InOffsetMode bool
	// Label is the text of the one pick.
	Label string
}

// ParseGoToLine reads the text typed after the first colon: a line and an optional column
// ("12", "12:5", "12,5", "12#5", a negative number counts from the end), or, after a second colon,
// a character offset ("::1024"). The column the user types is a visible column (tabs count to the
// next tab stop); the result is a byte column of the line.
//
// Ported from VS Code: gotoLineQuickAccess.ts (parsePosition)
func ParseGoToLine(ed GoToLineEditor, value string) GoToLinePosition {
	if ed == nil {
		return GoToLinePosition{Label: noEditorLabel}
	}
	if rest, ok := strings.CutPrefix(value, ":"); ok {
		maxOffset := ed.Length()
		offset, err := strconv.Atoi(parseIntPrefix(rest))
		if err != nil {
			return GoToLinePosition{InOffsetMode: true, Label: "Type a character position to go to (from 1 to " + strconv.Itoa(maxOffset) + ")."}
		}
		reverse := offset < 0
		// 1-based offsets: convert to the 0-based one of the text
		switch {
		case offset > 0:
			offset--
		case offset < 0:
			offset++
		}
		if reverse {
			offset += maxOffset
		}
		offset = min(max(offset, 0), maxOffset)
		line, col := ed.PositionAt(offset)
		visible := edit.VisibleColumn(ed.LineContent(line), col-1, ed.TabSize()) + 1
		return GoToLinePosition{Line: line, Column: col, InOffsetMode: true,
			Label: "Press 'Enter' to go to line " + strconv.Itoa(line) + " at column " + strconv.Itoa(visible) + "."}
	}
	// line and column are separated by a comma, a colon or a hash
	parts := splitKeepEmpty(value)
	maxLine := ed.LineCount()
	lineNumber, err := strconv.Atoi(parseIntPrefix(strings.TrimSpace(parts[0])))
	if err != nil {
		return GoToLinePosition{Label: "Type a line number to go to (from 1 to " + strconv.Itoa(maxLine) + ")."}
	}
	// negative line numbers count from the end, and the line is clipped to the text
	if lineNumber < 0 {
		lineNumber = maxLine + 1 + lineNumber
	}
	lineNumber = min(max(lineNumber, 1), maxLine)
	content := ed.LineContent(lineNumber)
	tab := ed.TabSize()
	maxColumn := edit.VisibleColumn(content, len(content), tab) + 1
	ln := strconv.Itoa(lineNumber)
	var column int
	columnErr := error(nil)
	if len(parts) >= 2 {
		column, columnErr = strconv.Atoi(parseIntPrefix(strings.TrimSpace(parts[1])))
	}
	if len(parts) < 2 || columnErr != nil {
		label := "Press 'Enter' to go to line " + ln + " or enter colon : to add a column number."
		if len(parts) >= 2 {
			label = "Press 'Enter' to go to line " + ln + " or enter a column number (from 1 to " + strconv.Itoa(maxColumn) + ")."
		}
		return GoToLinePosition{Line: lineNumber, Column: 1, Label: label}
	}
	if column < 0 {
		column = maxColumn + column
	}
	column = min(max(column, 1), maxColumn)
	real := edit.ColumnFromVisible(content, column-1, tab) + 1
	return GoToLinePosition{Line: lineNumber, Column: real,
		Label: "Press 'Enter' to go to line " + ln + " at column " + strconv.Itoa(column) + "."}
}

// splitKeepEmpty splits at ',' ':' and '#' and keeps the empty pieces, as String.split does.
func splitKeepEmpty(s string) []string {
	var parts []string
	start := 0
	for i, r := range s {
		if r == ',' || r == ':' || r == '#' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// parseIntPrefix keeps the leading integer of a text, as parseInt does ("12abc" is 12); it gives
// "" when there is none.
func parseIntPrefix(s string) string {
	s = strings.TrimLeft(s, " \t")
	end := 0
	for i, r := range s {
		if (r == '-' || r == '+') && i == 0 {
			end = i + 1
			continue
		}
		if r < '0' || r > '9' {
			break
		}
		end = i + len(string(r))
	}
	if end == 0 || (end == 1 && (s[0] == '-' || s[0] == '+')) {
		return ""
	}
	return strings.TrimPrefix(s[:end], "+")
}

// GoToLineProvider is the provider of the go to line modes: the one pick says what Enter does, the
// editor shows the line while the user types, and giving up puts the editor back.
type GoToLineProvider struct {
	prefix string
	// Editor gives the editor to work on when the mode opens, or nil when no text editor is open.
	Editor func() GoToLineEditor
}

// NewGoToLineProvider makes the provider of ":" or of "::" (they share what they do).
func NewGoToLineProvider(prefix string, editor func() GoToLineEditor) *GoToLineProvider {
	return &GoToLineProvider{prefix: prefix, Editor: editor}
}

// Info implements Provider.
func (p *GoToLineProvider) Info() ProviderInfo {
	help := HelpEntry{Description: "Go to Line/Column", Command: GoToLineCommand}
	if p.prefix == GoToOffsetPrefix {
		help = HelpEntry{Description: "Go to Offset", Command: GoToOffsetCommand}
	}
	return ProviderInfo{Prefix: p.prefix, Placeholder: goToLinePlaceholder, Help: []HelpEntry{help}}
}

// Provide implements Provider.
func (p *GoToLineProvider) Provide(s *Session) Handler {
	pk := s.Picker()
	pk.MatchOnLabel, pk.MatchOnDescription, pk.MatchOnDetail, pk.SortByLabel = false, false, false, false
	var ed GoToLineEditor
	if p.Editor != nil {
		ed = p.Editor()
	}
	return &goToLineHandler{s: s, ed: ed}
}

type goToLineHandler struct {
	s  *Session
	ed GoToLineEditor
}

// ValueChanged implements Handler: it shows the line the text names and reveals it.
func (h *goToLineHandler) ValueChanged(string) {
	text := strings.TrimSpace(h.s.Picker().Value())
	text = strings.TrimPrefix(text, GoToLinePrefix)
	pos := ParseGoToLine(h.ed, text)
	h.s.Picker().SetItems([]Item{{Label: pos.Label, Data: pos}})
	if h.ed == nil {
		return
	}
	if pos.Line == 0 {
		h.ed.ClearHighlight()
		return
	}
	h.ed.Reveal(pos.Line, pos.Column)
}

// Accept implements Handler: it goes to the position and closes the picker.
func (h *goToLineHandler) Accept(row Row, _ KeyMods) {
	pos, ok := row.Item.Data.(GoToLinePosition)
	if !ok || pos.Line == 0 || h.ed == nil {
		return
	}
	h.ed.Go(pos.Line, pos.Column)
	h.s.c.hide(true)
}

// Done implements Handler: giving up puts the editor back; the highlight always goes.
func (h *goToLineHandler) Done(accepted bool) {
	if h.ed == nil {
		return
	}
	h.ed.ClearHighlight()
	if !accepted {
		h.ed.Restore()
	}
}
