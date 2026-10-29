package sql

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// The bounds of what an editor keeps of its runs (SPEC §10.21 "The results panel": the last 10 runs, bounded in rows and bytes).
const (
	// historyRuns is how many runs an editor keeps.
	historyRuns = 10
	// editorBytes is the most memory the result sets of one editor's runs may hold; when a run makes it more, the oldest runs lose
	// their rows (their messages stay).
	editorBytes = 256 << 20
)

// runRecord is a run (Run or Run All) of one editor, as the panel shows it: the statements, their messages and their result sets. It
// is made and changed on the UI thread only.
type runRecord struct {
	// n numbers the runs of the editor from 1.
	n       int
	started time.Time
	// elapsed is how long the whole run took; zero while it runs.
	elapsed time.Duration
	// connection and database are what the run was for.
	connection, database string
	engine               string
	// text is the script the statements were cut from, and base the offset of the part that ran in the document, for the places of
	// errors.
	text string
	base int
	// total is how many statements the run was given; steps are those that ran.
	total int
	steps []*stepRecord
	sets  []*setRecord
	// running says that the run goes on; current is the number of the statement that is running.
	running bool
	current int
	// cancelled says that the user stopped the run, and skipped how many statements were not run because of an error or a cancel.
	cancelled bool
	skipped   int
	// tab is the tab of the panel that shows: an index in sets, or len(sets) for the Messages.
	tab int
	// discarded says that the rows of the run were dropped to keep the memory of the editor bounded.
	discarded bool
}

// stepRecord is one statement of a run and what it gave.
type stepRecord struct {
	res *StatementResult
	// line and column place the error in the document (counted from 1, in UTF-16 units); line is 0 when the error has no place.
	line, column int
	// endLine and endColumn end the mark of the error.
	endLine, endColumn int
}

// setRecord is a result set with its grid.
type setRecord struct {
	step  int
	index int
	set   *ResultSet
	grid  *resultgrid.Grid
}

// title is the text of the tab of a result set: "Result 2 (25 rows)".
func (s *setRecord) title(nsets int) string {
	name := "Result"
	if nsets > 1 {
		name += " " + strconv.Itoa(s.index+1)
	}
	return name + " (" + rowsText(s.set.Table.Len(), s.set.More()) + ")"
}

// rowsText writes a count of rows: "1 row", "1,000 rows", "1,000+ rows" when there are more.
func rowsText(n int, more bool) string {
	s := groupDigits(n)
	if more {
		s += "+"
	}
	if n == 1 && !more {
		return s + " row"
	}
	return s + " rows"
}

// groupDigits writes a whole number with a comma between the thousands.
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// bytes is the memory the result sets of the run hold.
func (r *runRecord) bytes() int64 {
	var n int64
	for _, s := range r.sets {
		n += s.set.Bytes()
	}
	return n
}

// release drops the rows of the run and lets go of its grids and cursors.
func (r *runRecord) release() {
	for _, s := range r.sets {
		if s.grid != nil {
			s.grid.Dispose()
			s.grid = nil
		}
		s.set.Release()
	}
	r.discarded = true
}

// rows is the number of rows the run read, and affected the rows it changed.
func (r *runRecord) rows() (read int, affected int64) {
	for _, st := range r.steps {
		for _, s := range st.res.Sets {
			read += s.Table.Len()
		}
		if st.res.HasAffected {
			affected += st.res.Affected
		}
	}
	return read, affected
}

// failed tells whether a statement of the run failed (or was cancelled).
func (r *runRecord) failed() bool {
	for _, st := range r.steps {
		if st.res.Err != nil {
			return true
		}
	}
	return false
}

// summary is the one line the toolbar and the history list say of the run.
func (r *runRecord) summary() string {
	var parts []string
	switch {
	case r.running:
		parts = append(parts, "Running statement "+strconv.Itoa(r.current)+" of "+strconv.Itoa(r.total)+"...")
	case r.cancelled:
		parts = append(parts, "Cancelled")
	case r.failed():
		parts = append(parts, "Failed")
	}
	if !r.running {
		parts = append(parts, plural(len(r.steps), "statement"))
		if rows, affected := r.rows(); rows > 0 {
			parts = append(parts, rowsText(rows, false))
		} else if affected > 0 {
			parts = append(parts, groupDigits(int(affected))+" affected")
		}
		parts = append(parts, formatElapsed(r.elapsed))
	}
	return strings.Join(parts, " · ")
}

// plural writes a count and its noun.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return groupDigits(n) + " " + noun + "s"
}

// formatElapsed writes a duration for a person: 0.4 ms, 12 ms, 1.25 s, 2 min 5 s.
func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64) + " ms"
	case d < time.Second:
		return strconv.Itoa(int(d/time.Millisecond)) + " ms"
	case d < time.Minute:
		return strconv.FormatFloat(d.Seconds(), 'f', 2, 64) + " s"
	}
	return strconv.Itoa(int(d/time.Minute)) + " min " + strconv.Itoa(int((d%time.Minute)/time.Second)) + " s"
}

// firstLine is the text of a statement on one line, cut at a length: the words of a message that names the statement.
func firstLine(text string, limit int) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = strings.TrimSpace(text[:i]) + " ..."
	}
	if utf8.RuneCountInString(text) > limit {
		runes := []rune(text)
		text = strings.TrimSpace(string(runes[:limit])) + " ..."
	}
	return text
}

// lineColumn converts a byte offset in the text to the line and column the editor's markers use: from 1, the column in UTF-16 units.
func lineColumn(text string, offset int) (line, column int) {
	offset = min(max(offset, 0), len(text))
	line = 1 + strings.Count(text[:offset], "\n")
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	for _, r := range text[start:offset] {
		column += len(utf16.Encode([]rune{r}))
	}
	return line, column + 1
}

// offsetOf converts a position of the core events (a line and a column in bytes, both counted from 0) to a byte offset in the text.
func offsetOf(text string, line, col int) int {
	off := 0
	for ; line > 0; line-- {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	end := strings.IndexByte(text[off:], '\n')
	if end < 0 {
		end = len(text) - off
	}
	return off + min(col, end)
}
