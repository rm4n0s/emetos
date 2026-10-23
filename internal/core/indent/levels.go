package indent

import (
	"math"
	"sync"
)

// LineSource is the text that the guides and their cache read. Lines count from zero.
type LineSource interface {
	// LineCount is the number of lines (at least one).
	LineCount() int
	// LineContent is the text of a line without its line break.
	LineContent(line int) string
}

// levelBlock is the number of lines whose smallest indentation Levels keeps.
const levelBlock = 64

// noLevel is the level of a line that is empty or has nothing but white space.
const noLevel = -1

// unknown marks what Levels has not computed.
const unknown = -2

// Levels is the indentation of the lines of a text, in columns (a tab goes to the next tab stop),
// computed when it is asked for and kept until an edit touches the line; it also keeps the
// smallest indentation of each block of 64 lines, so that the smallest indentation of a long
// range (the inside of a bracket pair that spans most of a file) costs a few hundred steps. It is
// VS Code's computeIndentLevel with a cache. It is safe for use from several goroutines.
type Levels struct {
	mu      sync.Mutex
	src     LineSource
	tabSize int
	level   []int32
	min     []int32
}

// NewLevels makes the Levels of a text for a tab size.
func NewLevels(src LineSource, tabSize int) *Levels {
	l := &Levels{src: src, tabSize: max(tabSize, 1)}
	l.resetLocked()
	return l
}

func (l *Levels) resetLocked() {
	n := max(l.src.LineCount(), 1)
	l.level = make([]int32, n)
	for i := range l.level {
		l.level[i] = unknown
	}
	l.min = make([]int32, (n+levelBlock-1)/levelBlock)
	for i := range l.min {
		l.min[i] = unknown
	}
}

// Reset forgets every line, as when the text is replaced.
func (l *Levels) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resetLocked()
}

// SetTabSize changes the tab size, which changes the level of lines indented with tabs.
func (l *Levels) SetTabSize(tabSize int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if tabSize = max(tabSize, 1); tabSize != l.tabSize {
		l.tabSize = tabSize
		l.resetLocked()
	}
}

// TabSize is the tab size the levels are in.
func (l *Levels) TabSize() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tabSize
}

// LineChange tells that the lines From to To (zero-based, both included) were replaced by Count
// lines.
type LineChange struct {
	From, To, Count int
}

// Splice tells that the lines from to to (zero-based, both included) were replaced by count lines.
func (l *Levels) Splice(from, to, count int) {
	l.Apply([]LineChange{{from, to, count}})
}

// Apply tells of a batch of changes: in order of position, each in the lines of the text before
// the batch. If the line count does not agree with the text's afterwards, everything is
// forgotten.
func (l *Levels) Apply(changes []LineChange) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if c.From < 0 || c.To < c.From || c.To >= len(l.level) || c.Count < 1 {
			l.resetLocked()
			return
		}
		level := make([]int32, 0, len(l.level)-(c.To-c.From+1)+c.Count)
		level = append(level, l.level[:c.From]...)
		for j := 0; j < c.Count; j++ {
			level = append(level, unknown)
		}
		level = append(level, l.level[c.To+1:]...)
		l.level = level
		// Blocks from the first changed one on are forgotten (their lines have moved).
		l.min = l.min[:min(c.From/levelBlock, len(l.min))]
	}
	if len(l.level) != l.src.LineCount() {
		l.resetLocked()
		return
	}
	for len(l.min) < (len(l.level)+levelBlock-1)/levelBlock {
		l.min = append(l.min, unknown)
	}
}

// Invalidate forgets the lines from to to (zero-based, both included), whose text changed with
// their number staying.
func (l *Levels) Invalidate(from, to int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := max(from, 0); i <= to && i < len(l.level); i++ {
		l.level[i] = unknown
	}
	if from >= 0 {
		for b := from / levelBlock; b <= to/levelBlock && b < len(l.min); b++ {
			l.min[b] = unknown
		}
	}
}

// Level is the indentation of a line in columns, or -1 when the line is empty or has only white
// space (a line out of the text counts as empty).
func (l *Levels) Level(line int) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.levelLocked(line)
}

func (l *Levels) levelLocked(line int) int {
	if line < 0 || line >= len(l.level) {
		return noLevel
	}
	if v := l.level[line]; v != unknown {
		return int(v)
	}
	v := computeLevel(l.src.LineContent(line), l.tabSize)
	l.level[line] = int32(v)
	return v
}

// Level is the indentation of a line in columns (a tab goes to the next multiple of the tab size),
// or -1 for a line of white space only.
func Level(line string, tabSize int) int { return computeLevel(line, tabSize) }

// computeLevel is the indentation of a line, or -1 for a line of white space only.
//
// Ported from VS Code: src/vs/editor/common/model/utils.ts (computeIndentLevel)
func computeLevel(line string, tabSize int) int {
	indent := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			indent++
		case '\t':
			indent = indent - indent%tabSize + tabSize
		default:
			return indent
		}
	}
	return noLevel
}

// MinLevel is the smallest indentation among the lines from to to (zero-based, both included) that
// have text, or math.MaxInt when none has.
func (l *Levels) MinLevel(from, to int) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	from, to = max(from, 0), min(to, len(l.level)-1)
	result := math.MaxInt
	line := from
	for line <= to {
		if line%levelBlock == 0 && line+levelBlock-1 <= to {
			result = min(result, l.blockMinLocked(line/levelBlock))
			line += levelBlock
			continue
		}
		if v := l.levelLocked(line); v >= 0 {
			result = min(result, v)
		}
		line++
	}
	return result
}

// blockMinLocked is the smallest indentation among the lines of a whole block.
func (l *Levels) blockMinLocked(block int) int {
	if v := l.min[block]; v != unknown {
		if v == math.MaxInt32 {
			return math.MaxInt
		}
		return int(v)
	}
	result := math.MaxInt
	for line := block * levelBlock; line < (block+1)*levelBlock && line < len(l.level); line++ {
		if v := l.levelLocked(line); v >= 0 {
			result = min(result, v)
		}
	}
	if result == math.MaxInt {
		l.min[block] = math.MaxInt32
	} else {
		l.min[block] = int32(result)
	}
	return result
}
