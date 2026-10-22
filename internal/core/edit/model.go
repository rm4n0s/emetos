package edit

import "github.com/rm4n0s/emetos/internal/core/buffer"

// Model is the text the cursor functions read: lines by zero-based number,
// without their line feed (SPEC §7.2).
type Model interface {
	// LineCount is the number of lines, at least 1.
	LineCount() int
	// LineContent is the text of a line, which must be a valid line number.
	LineContent(line int) string
	// ValidatePosition moves a position into the text.
	ValidatePosition(p buffer.Position) buffer.Position
}

// SnapshotModel is a Model over a buffer snapshot.
type SnapshotModel struct {
	buffer.Snapshot
}

// NewModel makes a Model of a snapshot.
func NewModel(s buffer.Snapshot) SnapshotModel { return SnapshotModel{s} }

// LineContent is the text of a line; a line out of range gives "".
func (m SnapshotModel) LineContent(line int) string {
	text, _ := m.Snapshot.LineContent(line)
	return text
}

// lastLine is the number of the last line.
func lastLine(m Model) int { return m.LineCount() - 1 }

// lineEnd is the position at the end of a line.
func lineEnd(m Model, line int) buffer.Position {
	return buffer.Position{Line: line, Col: len(m.LineContent(line))}
}
