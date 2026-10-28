package emulator

import "slices"

// MarkerID names a marker: a line of the main buffer that the emulator keeps track of (SPEC §10.25 "Shell integration").
// A marker moves with its line as the scrollback grows and is trimmed, as the screen scrolls and when a resize reflows the
// lines; it is gone when its line is deleted or leaves the scrollback. 0 is no marker.
//
// This is the emulator's own design: the line of a marker is an absolute number (the one of Frame.Top), so that growing and
// trimming the scrollback need no work; only a scroll of a part of the screen (IL, DL, a scroll region) and a reflow move
// them.
type MarkerID uint64

// marker is one tracked line.
type marker struct {
	id   MarkerID
	line int64
}

// markSet is the markers of the main buffer, ordered by line.
type markSet struct {
	list []marker
	next MarkerID
}

// add records a marker at an absolute line and returns its id.
func (m *markSet) add(line int64) MarkerID {
	m.next++
	id := m.next
	i, _ := slices.BinarySearchFunc(m.list, line, func(x marker, l int64) int {
		switch {
		case x.line <= l:
			return -1
		default:
			return 1
		}
	})
	m.list = slices.Insert(m.list, i, marker{id: id, line: line})
	return id
}

// find returns the index of a marker, -1 when there is none.
func (m *markSet) find(id MarkerID) int {
	for i := range m.list {
		if m.list[i].id == id {
			return i
		}
	}
	return -1
}

// dropBefore forgets the markers of the lines before first (the scrollback dropped them).
func (m *markSet) dropBefore(first int64) {
	n := 0
	for n < len(m.list) && m.list[n].line < first {
		n++
	}
	if n > 0 {
		m.list = slices.Delete(m.list, 0, n)
	}
}

// dropFrom forgets the markers of the lines from line on (the rows are gone).
func (m *markSet) dropFrom(line int64) {
	n := len(m.list)
	for n > 0 && m.list[n-1].line >= line {
		n--
	}
	m.list = m.list[:n]
}

// rotate follows a scroll of the screen rows between the absolute lines top and bottom (both inclusive) by n lines: up when n
// is positive, down when it is negative. The lines that leave are deleted with their markers and the others move.
func (m *markSet) rotate(top, bottom int64, n int) {
	if len(m.list) == 0 {
		return
	}
	k := int64(n)
	if k < 0 {
		k = -k
	}
	out := m.list[:0]
	for _, x := range m.list {
		switch {
		case x.line < top || x.line > bottom:
		case n > 0 && x.line < top+k:
			continue
		case n > 0:
			x.line -= k
		case x.line > bottom-k:
			continue
		default:
			x.line += k
		}
		out = append(out, x)
	}
	m.list = out
}

// AddMarker adds a marker at the cursor's line of the main buffer and returns it; 0 on the alternate screen.
func (e *Emulator) AddMarker() MarkerID {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.addMarkerLocked()
}

// addMarkerLocked adds a marker at the cursor's line; the lock is held.
func (e *Emulator) addMarkerLocked() MarkerID {
	if e.scr != e.mainScr {
		return 0
	}
	s := e.mainScr
	e.marks.dropBefore(s.ring.trimmed)
	return e.marks.add(s.absLine(s.y))
}

// MarkerLine is the absolute line of a marker (the numbers of Frame.Top, FirstLine and TextRange). ok is false when the
// marker is gone: removed, deleted with its line, or trimmed from the scrollback.
func (e *Emulator) MarkerLine(id MarkerID) (line int64, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	i := e.marks.find(id)
	if i < 0 {
		return 0, false
	}
	s := e.mainScr
	l := e.marks.list[i].line
	if l < s.ring.trimmed || l >= s.ring.trimmed+int64(s.ring.n) {
		return 0, false
	}
	return l, true
}

// RemoveMarker forgets a marker.
func (e *Emulator) RemoveMarker(id MarkerID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i := e.marks.find(id); i >= 0 {
		e.marks.list = slices.Delete(e.marks.list, i, i+1)
	}
}

// Markers is the number of markers that are alive.
func (e *Emulator) Markers() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.marks.dropBefore(e.mainScr.ring.trimmed)
	return len(e.marks.list)
}

// MarkerLines gives the absolute lines of markers in one call: out[i] is the line of ids[i], or -1 when that marker is gone.
// out is reused when it is big enough. A host that draws many markers a frame asks once, not once for each.
func (e *Emulator) MarkerLines(ids []MarkerID, out []int64) []int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cap(out) < len(ids) {
		out = make([]int64, len(ids))
	}
	out = out[:len(ids)]
	s := e.mainScr
	first, end := s.ring.trimmed, s.ring.trimmed+int64(s.ring.n)
	var byID map[MarkerID]int64
	if len(ids) > 8 {
		byID = make(map[MarkerID]int64, len(e.marks.list))
		for _, m := range e.marks.list {
			byID[m.id] = m.line
		}
	}
	for i, id := range ids {
		line, ok := int64(0), false
		if byID != nil {
			line, ok = byID[id]
		} else if j := e.marks.find(id); j >= 0 {
			line, ok = e.marks.list[j].line, true
		}
		if !ok || id == 0 || line < first || line >= end {
			out[i] = -1
			continue
		}
		out[i] = line
	}
	return out
}
