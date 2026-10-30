package git

import "bytes"

// Status is what a file's state is called in the Source Control view: the values of VS Code's Status enum, in its order
// (extensions/git/src/api/git.d.ts), so that the icons, letters, colors and sort orders can be ported unchanged.
type Status int

// The statuses. The zero value is StatusNone, which VS Code has no name for: "no status in this group".
const (
	// StatusNone is no status.
	StatusNone Status = iota - 1
	// StatusIndexModified is "M" in the first column: modified and staged.
	StatusIndexModified
	// StatusIndexAdded is "A" in the first column: added to the index.
	StatusIndexAdded
	// StatusIndexDeleted is "D" in the first column: deleted from the index.
	StatusIndexDeleted
	// StatusIndexRenamed is "R" in the first column: renamed in the index.
	StatusIndexRenamed
	// StatusIndexCopied is "C" in the first column: copied in the index.
	StatusIndexCopied
	// StatusModified is "M" in the second column: modified in the work tree.
	StatusModified
	// StatusDeleted is "D" in the second column: deleted in the work tree.
	StatusDeleted
	// StatusUntracked is "??".
	StatusUntracked
	// StatusIgnored is "!!".
	StatusIgnored
	// StatusIntentToAdd is "A" in the second column (git add -N).
	StatusIntentToAdd
	// StatusIntentToRename is "R" in the second column.
	StatusIntentToRename
	// StatusTypeChanged is "T" in the second column.
	StatusTypeChanged
	// StatusAddedByUs is "AU": unmerged, added by us.
	StatusAddedByUs
	// StatusAddedByThem is "UA": unmerged, added by them.
	StatusAddedByThem
	// StatusDeletedByUs is "DU": unmerged, deleted by us.
	StatusDeletedByUs
	// StatusDeletedByThem is "UD": unmerged, deleted by them.
	StatusDeletedByThem
	// StatusBothAdded is "AA": unmerged, added by both.
	StatusBothAdded
	// StatusBothDeleted is "DD": unmerged, deleted by both.
	StatusBothDeleted
	// StatusBothModified is "UU": unmerged, modified by both.
	StatusBothModified
)

// StatusEntry is one record of "git status -z" (porcelain version 1): the two status columns, the path, and for a rename or
// a copy the path it came from.
type StatusEntry struct {
	// X is the status of the index (the first column), Y the status of the work tree (the second); a space is no change.
	X, Y byte
	// Path is the path of the file (for a rename, the new path).
	Path string
	// OrigPath is the path a renamed or copied file came from, empty for the others. VS Code calls the new path "rename" and
	// the old one "path" in its IFileStatus; here the names say what they are.
	OrigPath string
}

// Statuses maps the two columns to the statuses of the groups of the Source Control view, as VS Code's
// Repository.updateModelState does (extensions/git/src/repository.ts): merge is set for the seven unmerged codes (DD, AU,
// UD, UA, DU, AA, UU) and then the other two are StatusNone; untracked and ignored files have only work set; otherwise index
// is set by the first column (M A D R C) and work by the second (M D A R T). A column that gives no status is StatusNone.
func (e StatusEntry) Statuses() (merge, index, work Status) {
	switch string([]byte{e.X, e.Y}) {
	case "??":
		return StatusNone, StatusNone, StatusUntracked
	case "!!":
		return StatusNone, StatusNone, StatusIgnored
	case "DD":
		return StatusBothDeleted, StatusNone, StatusNone
	case "AU":
		return StatusAddedByUs, StatusNone, StatusNone
	case "UD":
		return StatusDeletedByThem, StatusNone, StatusNone
	case "UA":
		return StatusAddedByThem, StatusNone, StatusNone
	case "DU":
		return StatusDeletedByUs, StatusNone, StatusNone
	case "AA":
		return StatusBothAdded, StatusNone, StatusNone
	case "UU":
		return StatusBothModified, StatusNone, StatusNone
	}
	index, work = StatusNone, StatusNone
	switch e.X {
	case 'M':
		index = StatusIndexModified
	case 'A':
		index = StatusIndexAdded
	case 'D':
		index = StatusIndexDeleted
	case 'R':
		index = StatusIndexRenamed
	case 'C':
		index = StatusIndexCopied
	}
	switch e.Y {
	case 'M':
		work = StatusModified
	case 'D':
		work = StatusDeleted
	case 'A':
		work = StatusIntentToAdd
	case 'R':
		work = StatusIntentToRename
	case 'T':
		work = StatusTypeChanged
	}
	return StatusNone, index, work
}

// StatusParser reads the output of "git status -z" as a stream: Write it the chunks as they arrive, cut anywhere, and read
// Entries. A record that is not complete yet waits for the next chunk; one that never completes (a stream cut short) is
// dropped. It implements io.Writer, so io.Copy(parser, stdout) feeds it.
//
// Ported from VS Code: extensions/git/src/git.ts (GitStatusParser).
type StatusParser struct {
	rest    []byte
	entries []StatusEntry
}

// Write adds a chunk of the output.
func (p *StatusParser) Write(chunk []byte) (int, error) {
	data := chunk
	if len(p.rest) > 0 {
		data = append(p.rest, chunk...)
	}
	i := 0
	for {
		next, ok := p.parseEntry(data, i)
		if !ok {
			break
		}
		i = next
	}
	p.rest = append([]byte(nil), data[i:]...)
	return len(chunk), nil
}

// parseEntry reads the record at data[i:]; it returns the index after it, or false when the record is not complete.
func (p *StatusParser) parseEntry(data []byte, i int) (int, bool) {
	if i+4 >= len(data) {
		return i, false
	}
	e := StatusEntry{X: data[i], Y: data[i+1]}
	pos := i + 3 // the two columns and the space
	end := bytes.IndexByte(data[pos:], 0)
	if end < 0 {
		return i, false
	}
	e.Path = string(data[pos : pos+end])
	pos += end + 1
	if e.X == 'R' || e.Y == 'R' || e.X == 'C' {
		end = bytes.IndexByte(data[pos:], 0)
		if end < 0 {
			return i, false
		}
		e.OrigPath = string(data[pos : pos+end])
		pos += end + 1
	}
	// A path that ends with a slash is a nested git repository.
	if e.Path != "" && e.Path[len(e.Path)-1] != '/' {
		p.entries = append(p.entries, e)
	}
	return pos, true
}

// Entries returns the records read so far.
func (p *StatusParser) Entries() []StatusEntry { return p.entries }

// ParseStatus parses a whole output of "git status -z".
func ParseStatus(data []byte) []StatusEntry {
	var p StatusParser
	_, _ = p.Write(data)
	return p.Entries()
}
