package folding

import (
	"strings"
	"unicode"

	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.ts (the actions)

// The commands below take the first lines of the selections of the editor (zero-based) as lines: the
// regions at them are the ones that are folded and unfolded.

// Fold is Fold (Ctrl+Shift+[): without levels and direction, the region at each line, or, if it is
// folded already, the first region around it that is not; with levels, that many levels down
// (or up, when up is set) from the region at the line.
func Fold(m *Model, lines []int, levels int, hasLevels, up bool) {
	switch {
	case !hasLevels && !up:
		SetCollapseStateUp(m, true, lines)
	case up:
		SetCollapseStateLevelsUp(m, true, max(levels, 1), lines)
	default:
		SetCollapseStateLevelsDown(m, true, max(levels, 1), lines)
	}
}

// Unfold is Unfold (Ctrl+Shift+]): the regions at the lines, levels levels down (1 when 0) or up.
func Unfold(m *Model, lines []int, levels int, up bool) {
	if up {
		SetCollapseStateLevelsUp(m, false, max(levels, 1), lines)
		return
	}
	SetCollapseStateLevelsDown(m, false, max(levels, 1), lines)
}

// Toggle is Toggle Fold: the region at each line is folded or unfolded.
func Toggle(m *Model, lines []int) { ToggleAt(m, 1, lines) }

// ToggleRecursively is Toggle Fold Recursively.
func ToggleRecursively(m *Model, lines []int) { ToggleAt(m, AllLevels, lines) }

// FoldRecursively is Fold Recursively: the regions at the lines and all those inside them.
func FoldRecursively(m *Model, lines []int) { SetCollapseStateLevelsDown(m, true, AllLevels, lines) }

// UnfoldRecursively is Unfold Recursively.
func UnfoldRecursively(m *Model, lines []int) { SetCollapseStateLevelsDown(m, false, AllLevels, lines) }

// FoldAll is Fold All.
func FoldAll(m *Model) { SetCollapseStateLevelsDown(m, true, AllLevels, nil) }

// UnfoldAll is Unfold All.
func UnfoldAll(m *Model) { SetCollapseStateLevelsDown(m, false, AllLevels, nil) }

// FoldLevel is Fold Level 1 to 7: all the regions of the level, except those with one of the
// lines.
func FoldLevel(m *Model, level int, lines []int) { SetCollapseStateAtLevel(m, level, true, lines) }

// FoldAllExcept is Fold All Except Selected: the regions that do not contain and are not inside
// the regions at the lines.
func FoldAllExcept(m *Model, lines []int) { SetCollapseStateForRest(m, true, lines) }

// UnfoldAllExcept is Unfold All Except Selected.
func UnfoldAllExcept(m *Model, lines []int) { SetCollapseStateForRest(m, false, lines) }

// FoldAllBlockComments is Fold All Block Comments: the regions of kind comment when the regions have
// kinds, else those whose first line starts, after white space, with the block comment token of the
// language ("" for none).
func FoldAllBlockComments(m *Model, blockCommentStart string) {
	if m.Regions().HasTypes() {
		SetCollapseStateForType(m, KindComment, true)
		return
	}
	if blockCommentStart == "" {
		return
	}
	SetCollapseStateForMatchingLines(m, func(line string) bool {
		return strings.HasPrefix(strings.TrimLeftFunc(line, unicode.IsSpace), blockCommentStart)
	}, true)
}

// FoldAllMarkerRegions is Fold All Regions: the regions of kind region, or those whose first line
// matches the start marker of the language (nil for none).
func FoldAllMarkerRegions(m *Model, markerStart *regex.Regexp) {
	setMarkerRegions(m, markerStart, true)
}

// UnfoldAllMarkerRegions is Unfold All Regions.
func UnfoldAllMarkerRegions(m *Model, markerStart *regex.Regexp) {
	setMarkerRegions(m, markerStart, false)
}

func setMarkerRegions(m *Model, markerStart *regex.Regexp, collapse bool) {
	if m.Regions().HasTypes() {
		SetCollapseStateForType(m, KindRegion, collapse)
		return
	}
	if markerStart != nil {
		SetCollapseStateForMatchingLines(m, MatchesRegex(markerStart), collapse)
	}
}
