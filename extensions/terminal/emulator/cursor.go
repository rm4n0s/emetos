package emulator

// CursorPosition returns where the cursor is without taking a snapshot: its absolute line (the numbering of Frame.Top, which
// does not change when the scrollback drops lines) and its column, and whether the next printed character wraps to the next
// line first (the cursor is past the last column). The host reads the line being typed with it (step 17d.6).
func (e *Emulator) CursorPosition() (line int64, col int, wrapPending bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.scr
	return s.absLine(s.y), s.x, s.wrapNext
}
