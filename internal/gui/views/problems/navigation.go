package problems

import (
	"slices"
	"strings"
)

// MarkerList is the list that Go to Next Problem and Go to Previous Problem go through: the errors, warnings and infos of one
// file or of every file, ordered by file, then by seriousness (errors first), then by position (VS Code's default of
// problems.sortOrder, "severity"), with the place of the problem shown last. It follows the markers: when they change, the
// place is forgotten.
//
// Ported from VS Code: src/vs/editor/contrib/gotoError/browser/markerNavigationService.ts (MarkerList)
type MarkerList struct {
	svc *Service
	// resource is the file the list is for; empty is every file.
	resource string
	markers  []Marker
	// next is the index of the marker shown last, or -1.
	next int
}

// NewMarkerList makes the list of the problems of a file (a path), or of every file when the path is empty.
func NewMarkerList(svc *Service, resource string) *MarkerList {
	l := &MarkerList{svc: svc, resource: resource, next: -1}
	l.markers = l.read()
	return l
}

// Matches tells whether the list is for the file (or for every file when the path is empty), as VS Code's MarkerList.matches.
func (l *MarkerList) Matches(resource string) bool { return l.resource == resource }

// read gives the markers of the list in the order it goes through them.
func (l *MarkerList) read() []Marker {
	out := l.svc.Read(ReadFilter{Resource: l.resource, Severities: All})
	slices.SortStableFunc(out, func(a, b Marker) int {
		if c := strings.Compare(a.Resource, b.Resource); c != 0 {
			return c
		}
		if a.Severity != b.Severity {
			return int(b.Severity) - int(a.Severity)
		}
		return compareStarts(a, b)
	})
	return out
}

// compareStarts orders markers by the start of their range, then by its end.
func compareStarts(a, b Marker) int {
	switch {
	case a.StartLine != b.StartLine:
		return a.StartLine - b.StartLine
	case a.StartColumn != b.StartColumn:
		return a.StartColumn - b.StartColumn
	case a.EndLine != b.EndLine:
		return a.EndLine - b.EndLine
	}
	return a.EndColumn - b.EndColumn
}

// sameMarker is the comparison VS Code uses to see whether the markers changed.
func sameMarker(a, b Marker) bool {
	return a.Resource == b.Resource && a.StartLine == b.StartLine && a.StartColumn == b.StartColumn &&
		a.EndLine == b.EndLine && a.EndColumn == b.EndColumn && a.Severity == b.Severity && a.Message == b.Message
}

// Refresh reads the markers again; when they are not the same the place is forgotten. It says whether they changed.
func (l *MarkerList) Refresh() bool {
	now := l.read()
	if slices.EqualFunc(now, l.markers, sameMarker) {
		return false
	}
	l.markers, l.next = now, -1
	return true
}

// Total is the number of markers in the list.
func (l *MarkerList) Total() int { return len(l.markers) }

// Selected is the marker shown last, its place in the list counted from 1 and the number of markers; false when none is.
func (l *MarkerList) Selected() (m Marker, index, total int, ok bool) {
	if l.next < 0 || l.next >= len(l.markers) {
		return Marker{}, 0, 0, false
	}
	return l.markers[l.next], l.next + 1, len(l.markers), true
}

// ResetIndex forgets the place: the next move starts from the position it is given.
func (l *MarkerList) ResetIndex() { l.next = -1 }

// holds tells whether a position (a line and a column counted from 1) is in the range of a marker, its ends included.
func (m Marker) holds(line, column int) bool {
	afterStart := line > m.StartLine || (line == m.StartLine && column >= m.StartColumn)
	beforeEnd := line < m.EndLine || (line == m.EndLine && column <= m.EndColumn)
	return afterStart && beforeEnd
}

// Holds tells whether a position of a file (a line and a column counted from 1, the column in UTF-16 units) is in the range of
// the marker.
func (m Marker) Holds(resource string, line, column int) bool {
	return m.Resource == resource && m.holds(line, column)
}

// Move goes to the next or the previous marker, wrapping around, and says whether the place moved. The first move, or the first
// after ResetIndex, starts from the position (a line and a column counted from 1) in the file with the path: the first marker
// at or after it, or the last one before it going back; a file with no markers starts at the file that follows it.
//
// Ported from VS Code: markerNavigationService.ts (MarkerList._initIdx, move)
func (l *MarkerList) Move(forward bool, resource string, line, column int) bool {
	n := len(l.markers)
	if n == 0 {
		return false
	}
	old := l.next
	switch {
	case l.next == -1:
		l.initIndex(forward, resource, line, column)
	case forward:
		l.next = (l.next + 1) % n
	default:
		l.next = (l.next - 1 + n) % n
	}
	return old != l.next
}

// initIndex finds the marker the first move goes to.
func (l *MarkerList) initIndex(forward bool, resource string, line, column int) {
	n := len(l.markers)
	idx := slices.IndexFunc(l.markers, func(m Marker) bool { return m.Resource == resource })
	if idx < 0 {
		// a file with no markers: go to where its markers would be, by the order of the paths
		idx, _ = slices.BinarySearchFunc(l.markers, resource, func(m Marker, path string) int { return strings.Compare(m.Resource, path) })
		if forward {
			l.next = idx
		} else {
			l.next = (n + idx - 1) % n
		}
	} else {
		found, wentPast := false, false
		// VS Code also looks at the first marker of the next file here, and compares the position of this file with it; the
		// markers of the file alone are looked at here
		for i := idx; i < n && l.markers[i].Resource == resource; i++ {
			m := l.markers[i]
			if m.holds(line, column) || line < m.StartLine || (line == m.StartLine && column <= m.StartColumn) {
				l.next, found, wentPast = i, true, !m.holds(line, column)
				break
			}
		}
		switch {
		case !found:
			// after the last marker of the file
			if forward {
				l.next = 0
			} else {
				l.next = n - 1
			}
		case wentPast && !forward:
			// went past the position: one back
			l.next--
		}
	}
	if l.next < 0 {
		l.next = n - 1
	}
	if l.next >= n {
		l.next = 0 // the file that follows the last one is the first
	}
}
