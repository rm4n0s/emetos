package snippet

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetVariables.ts

// IsKnownVariable tells whether a name is one of the variables that snippets can use. A name that
// is not known is left to the user's own variables; its default text is inserted.
func IsKnownVariable(name string) bool {
	switch name {
	case "CURRENT_YEAR", "CURRENT_YEAR_SHORT", "CURRENT_MONTH", "CURRENT_DATE", "CURRENT_HOUR",
		"CURRENT_MINUTE", "CURRENT_SECOND", "CURRENT_MILLISECOND", "CURRENT_DAY_NAME",
		"CURRENT_DAY_NAME_SHORT", "CURRENT_MONTH_NAME", "CURRENT_MONTH_NAME_SHORT",
		"CURRENT_SECONDS_UNIX", "CURRENT_MILLISECONDS_UNIX", "CURRENT_TIMEZONE_OFFSET",
		"CURRENT_TIMEZONE_NAME", "SELECTION", "CLIPBOARD", "TM_SELECTED_TEXT", "TM_CURRENT_LINE",
		"TM_CURRENT_WORD", "TM_LINE_INDEX", "TM_LINE_NUMBER", "TM_FILENAME", "TM_FILENAME_BASE",
		"TM_DIRECTORY", "TM_DIRECTORY_BASE", "TM_FILEPATH", "CURSOR_INDEX", "CURSOR_NUMBER",
		"RELATIVE_FILEPATH", "BLOCK_COMMENT_START", "BLOCK_COMMENT_END", "LINE_COMMENT",
		"WORKSPACE_NAME", "WORKSPACE_FOLDER", "RANDOM", "RANDOM_HEX", "UUID":
		return true
	}
	return false
}

// CompositeResolver asks resolvers one after the other and takes the first value.
type CompositeResolver []VariableResolver

// Resolve implements VariableResolver.
func (c CompositeResolver) Resolve(v *Variable) (string, bool) {
	for _, r := range c {
		if value, ok := r.Resolve(v); ok {
			return value, true
		}
	}
	return "", false
}

// Overtyped is the text that a cursor typed over last (the closing bracket that an auto-closed
// pair let the user type again), which a snippet can use as the selected text.
type Overtyped struct {
	// Value is the text and Multiline tells whether it had line breaks.
	Value     string
	Multiline bool
}

// SelectionValues are the values of the variables that depend on a cursor, gathered by the caller
// before a snippet is inserted (SelectionValuesOf does it for a document).
type SelectionValues struct {
	// Text is the selected text; empty for a cursor.
	Text string
	// Multiline tells whether the selection covers several lines.
	Multiline bool
	// StartIndent is the white space the line that the selection starts on begins with, as far as
	// the selection starts, to re-indent a selected text of several lines.
	StartIndent string
	// CurrentLine is the text of the line the active end of the selection is on and CurrentWord the
	// word there, which is empty with no word.
	CurrentLine, CurrentWord string
	// Line is the line of the active end, counted from 0.
	Line int
	// Index is the number of the cursor, counted from 0.
	Index int
	// Overtyped is what the cursor typed over last, or nil.
	Overtyped *Overtyped
}

// SelectionValuesOf gathers the values of a selection of a document. index is the number of the
// cursor, words finds the word at the cursor, and overtyped is what the cursor typed over last
// (nil for nothing).
func SelectionValuesOf(snap buffer.Snapshot, sel buffer.Selection, index int, words *edit.WordClassifier, overtyped *Overtyped) SelectionValues {
	start := sel.Start()
	v := SelectionValues{
		Text:      snap.Text(sel.Range()),
		Multiline: sel.Start().Line != sel.End().Line,
		Line:      sel.Active.Line,
		Index:     index,
		Overtyped: overtyped,
	}
	if line, err := snap.LineContent(start.Line); err == nil {
		v.StartIndent = leadingWhitespace(line[:min(start.Col, len(line))])
	}
	if line, err := snap.LineContent(sel.Active.Line); err == nil {
		v.CurrentLine = line
		if words != nil {
			if w, ok := words.WordAt(line, sel.Active.Col); ok {
				v.CurrentWord = line[w.Start:w.End]
			}
		}
	}
	return v
}

// leadingWhitespace is the spaces and tabs a text starts with.
func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

// SelectionResolver gives the variables that depend on a cursor: SELECTION, TM_SELECTED_TEXT,
// TM_CURRENT_LINE, TM_CURRENT_WORD, TM_LINE_INDEX, TM_LINE_NUMBER, CURSOR_INDEX and CURSOR_NUMBER.
type SelectionResolver struct {
	// Values are what the variables are.
	Values SelectionValues
}

// Resolve implements VariableResolver.
func (r SelectionResolver) Resolve(v *Variable) (string, bool) {
	val := r.Values
	switch v.Name {
	case "SELECTION", "TM_SELECTED_TEXT":
		value, multiline := val.Text, val.Multiline
		// with no selection, the text that was typed over last
		if value == "" && val.Overtyped != nil {
			value, multiline = val.Overtyped.Value, val.Overtyped.Multiline
		}
		if value == "" {
			return "", false
		}
		if multiline {
			if snippet := SnippetOf(v); snippet != nil {
				value = reindent(value, indentBefore(snippet, v, val.StartIndent), val.StartIndent)
			}
		}
		return value, true
	case "TM_CURRENT_LINE":
		return val.CurrentLine, true
	case "TM_CURRENT_WORD":
		return val.CurrentWord, val.CurrentWord != ""
	case "TM_LINE_INDEX":
		return strconv.Itoa(val.Line), true
	case "TM_LINE_NUMBER":
		return strconv.Itoa(val.Line + 1), true
	case "CURSOR_INDEX":
		return strconv.Itoa(val.Index), true
	case "CURSOR_NUMBER":
		return strconv.Itoa(val.Index + 1), true
	}
	return "", false
}

// indentBefore is the white space the line of a variable starts with: the one of the last text
// before it in the snippet, or the one of the line of the cursor.
func indentBefore(snippet *TextmateSnippet, v *Variable, lineIndent string) string {
	indent := lineIndent
	snippet.Walk(func(m Marker) bool {
		if m == Marker(v) {
			return false
		}
		if t, ok := m.(*Text); ok {
			last := t.Value
			if i := strings.LastIndexAny(last, "\r\n"); i >= 0 {
				last = last[i+1:]
			}
			indent = leadingWhitespace(last)
		}
		return true
	})
	return indent
}

// reindent adds the indentation that the variable has beyond the one of the line of the cursor
// to every line of a selected text after the first, so that it lines up with the snippet.
func reindent(value, varIndent, lineIndent string) string {
	common := 0
	for common < len(varIndent) && common < len(lineIndent) && varIndent[common] == lineIndent[common] {
		common++
	}
	extra := varIndent[common:]
	if extra == "" {
		return value
	}
	var sb strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		sb.WriteByte(c)
		if c == '\r' && i+1 < len(value) && value[i+1] == '\n' {
			sb.WriteByte('\n')
			i++
			sb.WriteString(extra)
		} else if c == '\r' || c == '\n' {
			sb.WriteString(extra)
		}
	}
	return sb.String()
}

// FileResolver gives the variables of the file of the document: TM_FILENAME, TM_FILENAME_BASE,
// TM_DIRECTORY, TM_DIRECTORY_BASE, TM_FILEPATH and RELATIVE_FILEPATH. A document that has no
// file gives its label (such as Untitled-1) as the path.
type FileResolver struct {
	// Path is the path of the file; empty resolves nothing.
	Path string
	// RelativePath is the path relative to the folder that is open, or empty when the file is not
	// in it (RELATIVE_FILEPATH is then the whole path).
	RelativePath string
}

// Resolve implements VariableResolver.
func (r FileResolver) Resolve(v *Variable) (string, bool) {
	if r.Path == "" {
		return "", false
	}
	switch v.Name {
	case "TM_FILENAME":
		return baseName(r.Path), true
	case "TM_FILENAME_BASE":
		name := baseName(r.Path)
		if i := strings.LastIndex(name, "."); i > 0 {
			return name[:i], true
		}
		return name, true
	case "TM_DIRECTORY":
		dir := dirName(r.Path)
		if dir == "." {
			return "", true
		}
		return dir, true
	case "TM_DIRECTORY_BASE":
		dir := dirName(r.Path)
		if dir == "." {
			return "", true
		}
		return baseName(dir), true
	case "TM_FILEPATH":
		return r.Path, true
	case "RELATIVE_FILEPATH":
		if r.RelativePath != "" {
			return r.RelativePath, true
		}
		return r.Path, true
	}
	return "", false
}

// baseName is the last part of a slash separated path, without trailing slashes.
func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// dirName is a slash separated path without its last part: "." for a path with no slash and "/"
// for a part of the root.
func dirName(p string) string {
	trimmed := strings.TrimRight(p, "/")
	i := strings.LastIndex(trimmed, "/")
	switch {
	case i < 0:
		return "."
	case i == 0:
		return "/"
	}
	return strings.TrimRight(trimmed[:i], "/")
}

// ClipboardResolver gives CLIPBOARD, from the text of the clipboard that the caller read before
// the snippet was inserted.
type ClipboardResolver struct {
	// Text is the text on the clipboard; empty resolves nothing.
	Text string
	// Index and Count are the number of the cursor and the number of cursors.
	Index, Count int
	// Spread gives each cursor one line of the clipboard when its lines (that are not blank) are
	// as many as the cursors.
	Spread bool
}

// Resolve implements VariableResolver.
func (r ClipboardResolver) Resolve(v *Variable) (string, bool) {
	if v.Name != "CLIPBOARD" || r.Text == "" {
		return "", false
	}
	if r.Spread {
		var lines []string
		for _, line := range splitLines(r.Text) {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) == r.Count && r.Index >= 0 && r.Index < len(lines) {
			return lines[r.Index], true
		}
	}
	return r.Text, true
}

// splitLines splits a text at \r\n, \n and \r.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.ReplaceAll(s, "\r", "\n"), "\n")
}

// CommentResolver gives the comment variables of the language at the cursor: LINE_COMMENT,
// BLOCK_COMMENT_START and BLOCK_COMMENT_END. A language that has none gives nothing.
type CommentResolver struct {
	LineComment, BlockCommentStart, BlockCommentEnd string
}

// Resolve implements VariableResolver.
func (r CommentResolver) Resolve(v *Variable) (string, bool) {
	var value string
	switch v.Name {
	case "LINE_COMMENT":
		value = r.LineComment
	case "BLOCK_COMMENT_START":
		value = r.BlockCommentStart
	case "BLOCK_COMMENT_END":
		value = r.BlockCommentEnd
	}
	return value, value != ""
}

var (
	dayNames        = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	dayNamesShort   = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	monthNames      = [...]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	monthNamesShort = [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// TimeResolver gives the CURRENT_* variables from one moment, so that every variable of a snippet
// (and of all the cursors) is from the same time.
type TimeResolver struct {
	// Now is the moment, in the time zone the variables are in.
	Now time.Time
	// TimezoneName is the name for CURRENT_TIMEZONE_NAME (an IANA name such as Europe/Athens);
	// when empty it is the name of the zone of Now.
	TimezoneName string
}

// Resolve implements VariableResolver.
func (r TimeResolver) Resolve(v *Variable) (string, bool) {
	t := r.Now
	pad := func(n, width int) string { return fmt.Sprintf("%0*d", width, n) }
	switch v.Name {
	case "CURRENT_YEAR":
		return strconv.Itoa(t.Year()), true
	case "CURRENT_YEAR_SHORT":
		y := strconv.Itoa(t.Year())
		return y[max(len(y)-2, 0):], true
	case "CURRENT_MONTH":
		return pad(int(t.Month()), 2), true
	case "CURRENT_DATE":
		return pad(t.Day(), 2), true
	case "CURRENT_HOUR":
		return pad(t.Hour(), 2), true
	case "CURRENT_MINUTE":
		return pad(t.Minute(), 2), true
	case "CURRENT_SECOND":
		return pad(t.Second(), 2), true
	case "CURRENT_MILLISECOND":
		return pad(t.Nanosecond()/int(time.Millisecond), 3), true
	case "CURRENT_DAY_NAME":
		return dayNames[t.Weekday()], true
	case "CURRENT_DAY_NAME_SHORT":
		return dayNamesShort[t.Weekday()], true
	case "CURRENT_MONTH_NAME":
		return monthNames[t.Month()-1], true
	case "CURRENT_MONTH_NAME_SHORT":
		return monthNamesShort[t.Month()-1], true
	case "CURRENT_SECONDS_UNIX":
		return strconv.FormatInt(t.Unix(), 10), true
	case "CURRENT_MILLISECONDS_UNIX":
		return strconv.FormatInt(t.UnixMilli(), 10), true
	case "CURRENT_TIMEZONE_OFFSET":
		_, east := t.Zone()
		minutes := east / 60
		sign := "+"
		if minutes < 0 {
			sign, minutes = "-", -minutes
		}
		return sign + pad(minutes/60, 2) + ":" + pad(minutes%60, 2), true
	case "CURRENT_TIMEZONE_NAME":
		if r.TimezoneName != "" {
			return r.TimezoneName, true
		}
		return t.Location().String(), true
	}
	return "", false
}

// WorkspaceResolver gives WORKSPACE_NAME and WORKSPACE_FOLDER. A window with no folder has
// neither.
type WorkspaceResolver struct {
	// Name is the name of the folder that is open and Folder its path.
	Name, Folder string
}

// Resolve implements VariableResolver.
func (r WorkspaceResolver) Resolve(v *Variable) (string, bool) {
	switch v.Name {
	case "WORKSPACE_NAME":
		return r.Name, r.Name != ""
	case "WORKSPACE_FOLDER":
		return r.Folder, r.Folder != ""
	}
	return "", false
}

// RandomResolver gives RANDOM (six random digits), RANDOM_HEX (six random hexadecimal digits) and
// UUID (a version 4 UUID).
type RandomResolver struct {
	// Reader is where the randomness comes from; the default is crypto/rand.Reader. A test gives
	// it a fixed source.
	Reader io.Reader
}

// Resolve implements VariableResolver. A reader that fails resolves nothing.
func (r RandomResolver) Resolve(v *Variable) (string, bool) {
	reader := r.Reader
	if reader == nil {
		reader = rand.Reader
	}
	switch v.Name {
	case "RANDOM":
		n, err := rand.Int(reader, big.NewInt(1_000_000))
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%06d", n.Int64()), true
	case "RANDOM_HEX":
		n, err := rand.Int(reader, big.NewInt(1<<24))
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%06x", n.Int64()), true
	case "UUID":
		var b [16]byte
		if _, err := io.ReadFull(reader, b[:]); err != nil {
			return "", false
		}
		b[6] = b[6]&0x0f | 0x40 // version 4
		b[8] = b[8]&0x3f | 0x80 // variant 10
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), true
	}
	return "", false
}
