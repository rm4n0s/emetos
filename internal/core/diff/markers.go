package diff

import (
	"strings"
	"unicode/utf8"
)

// Ported from VS Code: extensions/merge-conflict/src/mergeConflictParser.ts

// The conflict markers (VS Code's startHeaderMarker, commonAncestorsMarker,
// splitterMarker and endFooterMarker).
const (
	markerCurrent  = "<<<<<<<"
	markerAncestor = "|||||||"
	markerSplitter = "======="
	markerIncoming = ">>>>>>>"
)

// MarkerSection is a part of a conflict that has a header line: the current
// side (header "<<<<<<<"), a common ancestor (header "|||||||") or the
// incoming side (the footer ">>>>>>>", which is the header of the incoming
// content that comes before it).
type MarkerSection struct {
	// Header is the line of the marker (a range of one line).
	Header LineRange
	// Content is the lines of the section: those after the header up to the
	// next marker (before it for the incoming side, where Content lies between
	// the splitter and the footer). It can be empty.
	Content LineRange
	// Label is the text after the marker and one more character ("HEAD" in
	// "<<<<<<< HEAD"), as VS Code takes it (the text after "<<<<<<<HEAD"
	// without the space is "EAD"). It is empty when the marker has no text.
	Label string
}

// MarkerConflict is a conflict of a text with conflict markers. All line
// ranges are 1-based, end exclusive, in the lines that were scanned.
type MarkerConflict struct {
	// Range is the whole conflict, from the "<<<<<<<" line to the ">>>>>>>"
	// line.
	Range LineRange
	// Current is the section after "<<<<<<<" (the first side, "ours", HEAD).
	Current MarkerSection
	// CommonAncestors are the "|||||||" sections (diff3 style) in order; there
	// is none in a conflict without the base. There can be several (a merge
	// with several bases).
	CommonAncestors []MarkerSection
	// Splitter is the "=======" line (a range of one line).
	Splitter LineRange
	// Incoming is the section before ">>>>>>>" (the other side, "theirs"):
	// its Header is the footer line and its Content lies between the splitter
	// and the footer.
	Incoming MarkerSection
}

// ContainsConflictMarkers reports whether a text may contain a conflict: it
// has "<<<<<<<" and ">>>>>>>" somewhere (VS Code's containsConflict). It is a
// cheap test before ParseConflictMarkers.
func ContainsConflictMarkers(text string) bool {
	return strings.Contains(text, markerCurrent) && strings.Contains(text, markerIncoming)
}

// scanConflict is a conflict while it is scanned: the line numbers (0 when
// not seen) of its markers.
type scanConflict struct {
	start     int
	ancestors []int
	splitter  int
}

// ParseConflictMarkers finds the complete conflicts of a text given as lines
// without their line breaks. It is VS Code's MergeConflictParser.scanDocument:
//
//   - a marker line starts with the marker ("<<<<<<<", "|||||||",
//     ">>>>>>>") and may go on with anything, so a longer run of the
//     character ("<<<<<<<<") is a marker too; the splitter is the line
//     "=======" and nothing else; lines that are empty or only white space
//     are skipped;
//   - "|||||||" and "=======" count only inside a conflict and before its
//     splitter (a second splitter is text of the conflict);
//   - a ">>>>>>>" inside a conflict ends it; when it has no splitter the
//     conflict is dropped;
//   - a "<<<<<<<" inside a conflict (before its end) makes the scan give up:
//     the conflicts found so far are returned, and nothing after the line is
//     looked at;
//   - a conflict that is not closed at the end of the text is dropped.
//
// Markers outside a conflict ("=======", ">>>>>>>") are ignored, so the
// function has no error. The conflicts are sorted and do not overlap.
func ParseConflictMarkers(lines []string) []MarkerConflict {
	var (
		conflicts []MarkerConflict
		cur       *scanConflict
	)
scan:
	for i, text := range lines {
		n := i + 1
		if strings.TrimSpace(text) == "" {
			continue
		}
		switch {
		case strings.HasPrefix(text, markerCurrent):
			if cur != nil {
				break scan // a start before an end: give up
			}
			cur = &scanConflict{start: n}
		case cur != nil && cur.splitter == 0 && strings.HasPrefix(text, markerAncestor):
			cur.ancestors = append(cur.ancestors, n)
		case cur != nil && cur.splitter == 0 && text == markerSplitter:
			cur.splitter = n
		case cur != nil && strings.HasPrefix(text, markerIncoming):
			if cur.splitter != 0 {
				conflicts = append(conflicts, buildMarkerConflict(lines, *cur, n))
			}
			cur = nil
		}
	}
	return conflicts
}

// buildMarkerConflict makes the conflict out of the lines of its markers
// (VS Code's scanItemTolMergeConflictDescriptor).
func buildMarkerConflict(lines []string, s scanConflict, footer int) MarkerConflict {
	line := func(n int) LineRange { return LineRange{StartLine: n, EndLine: n + 1} }
	tokenAfterCurrent := s.splitter
	if len(s.ancestors) > 0 {
		tokenAfterCurrent = s.ancestors[0]
	}
	c := MarkerConflict{
		Range: LineRange{StartLine: s.start, EndLine: footer + 1},
		Current: MarkerSection{
			Header:  line(s.start),
			Content: LineRange{StartLine: s.start + 1, EndLine: tokenAfterCurrent},
			Label:   markerLabel(lines[s.start-1], markerCurrent),
		},
		Splitter: line(s.splitter),
		Incoming: MarkerSection{
			Header:  line(footer),
			Content: LineRange{StartLine: s.splitter + 1, EndLine: footer},
			Label:   markerLabel(lines[footer-1], markerIncoming),
		},
	}
	for i, a := range s.ancestors {
		next := s.splitter
		if i+1 < len(s.ancestors) {
			next = s.ancestors[i+1]
		}
		c.CommonAncestors = append(c.CommonAncestors, MarkerSection{
			Header:  line(a),
			Content: LineRange{StartLine: a + 1, EndLine: next},
			Label:   markerLabel(lines[a-1], markerAncestor),
		})
	}
	return c
}

// markerLabel is the text of a marker line after the marker and one more
// character (JavaScript's text.substring(marker.length + 1)).
func markerLabel(text, marker string) string {
	rest := text[len(marker):]
	if rest == "" {
		return ""
	}
	_, size := utf8.DecodeRuneInString(rest)
	return rest[size:]
}
