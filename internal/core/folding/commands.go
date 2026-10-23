package folding

import (
	"math"

	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/contrib/folding/browser/foldingModel.ts (the functions after
// FoldingModel) and folding.ts (the actions)

// AllLevels is the number of levels that means all of them.
const AllLevels = math.MaxInt

// ToggleAt folds the open region at each line and unfolds a folded one; with levels above 1 the
// regions inside it (down to that many levels) that are in the other state follow.
func ToggleAt(m *Model, levels int, lines []int) {
	var toggle []Region
	for _, line := range lines {
		region, ok := m.RegionAtLine(line)
		if !ok {
			continue
		}
		collapse := !region.Collapsed()
		toggle = append(toggle, region)
		if levels > 1 {
			toggle = append(toggle, m.RegionsInside(region, true, func(r Region, level int) bool {
				return r.Collapsed() != collapse && level < levels
			})...)
		}
	}
	m.ToggleCollapseState(toggle)
}

// SetCollapseStateLevelsDown folds or unfolds the regions at the lines and the regions inside them,
// down to levels levels (1 is only the regions at the lines); with no lines, all the regions of the
// model.
func SetCollapseStateLevelsDown(m *Model, collapse bool, levels int, lines []int) {
	var toggle []Region
	if len(lines) > 0 {
		for _, line := range lines {
			region, ok := m.RegionAtLine(line)
			if !ok {
				continue
			}
			if region.Collapsed() != collapse {
				toggle = append(toggle, region)
			}
			if levels > 1 {
				toggle = append(toggle, m.RegionsInside(region, true, func(r Region, level int) bool {
					return r.Collapsed() != collapse && level < levels
				})...)
			}
		}
	} else {
		toggle = m.RegionsInside(Region{}, false, func(r Region, level int) bool {
			return r.Collapsed() != collapse && level < levels
		})
	}
	m.ToggleCollapseState(toggle)
}

// SetCollapseStateLevelsUp folds or unfolds the regions at the lines and the regions they are
// inside of, up to levels levels.
func SetCollapseStateLevelsUp(m *Model, collapse bool, levels int, lines []int) {
	var toggle []Region
	for _, line := range lines {
		toggle = append(toggle, m.AllRegionsAtLine(line, func(r Region, level int) bool {
			return r.Collapsed() != collapse && level <= levels
		})...)
	}
	m.ToggleCollapseState(toggle)
}

// SetCollapseStateUp folds or unfolds the innermost region at each line that is not in that state
// already, which may be one that contains it.
func SetCollapseStateUp(m *Model, collapse bool, lines []int) {
	var toggle []Region
	for _, line := range lines {
		regions := m.AllRegionsAtLine(line, func(r Region, _ int) bool { return r.Collapsed() != collapse })
		if len(regions) > 0 {
			toggle = append(toggle, regions[0])
		}
	}
	m.ToggleCollapseState(toggle)
}

// SetCollapseStateAtLevel folds or unfolds all the regions of a level (1 is the top), except those
// that have one of the blocked lines.
func SetCollapseStateAtLevel(m *Model, level int, collapse bool, blocked []int) {
	m.ToggleCollapseState(m.RegionsInside(Region{}, false, func(r Region, l int) bool {
		if l != level || r.Collapsed() == collapse {
			return false
		}
		for _, line := range blocked {
			if r.ContainsLine(line) {
				return false
			}
		}
		return true
	}))
}

// SetCollapseStateForRest folds or unfolds all the regions except those that contain, or are
// contained by, the innermost region of one of the blocked lines.
func SetCollapseStateForRest(m *Model, collapse bool, blocked []int) {
	var kept []Region
	for _, line := range blocked {
		if regions := m.AllRegionsAtLine(line, nil); len(regions) > 0 {
			kept = append(kept, regions[0])
		}
	}
	m.ToggleCollapseState(m.RegionsInside(Region{}, false, func(r Region, _ int) bool {
		for _, k := range kept {
			if k.ContainedBy(r.Start(), r.End()) || r.ContainedBy(k.Start(), k.End()) {
				return false
			}
		}
		return r.Collapsed() != collapse
	}))
}

// MatchesRegex makes a line test of a regular expression, for SetCollapseStateForMatchingLines.
func MatchesRegex(re *regex.Regexp) func(line string) bool {
	return func(line string) bool { return matches(re, line) }
}

// SetCollapseStateForMatchingLines folds or unfolds the regions whose first line matches.
func SetCollapseStateForMatchingLines(m *Model, match func(line string) bool, collapse bool) {
	var toggle []Region
	regions := m.Regions()
	snap := m.doc.Snapshot()
	for i := regions.Len() - 1; i >= 0; i-- {
		if collapse == regions.Collapsed(i) {
			continue
		}
		line, _ := snap.LineContent(regions.Start(i))
		if match(line) {
			toggle = append(toggle, regions.Region(i))
		}
	}
	m.ToggleCollapseState(toggle)
}

// SetCollapseStateForType folds or unfolds the regions of a kind.
func SetCollapseStateForType(m *Model, typ string, collapse bool) {
	var toggle []Region
	regions := m.Regions()
	for i := regions.Len() - 1; i >= 0; i-- {
		if collapse != regions.Collapsed(i) && typ == regions.Type(i) {
			toggle = append(toggle, regions.Region(i))
		}
	}
	m.ToggleCollapseState(toggle)
}

// ParentFoldLine is the line to go to for the parent fold of a line: the first line of the region it
// is in, or, when it is on that line, of the region around it.
func ParentFoldLine(line int, m *Model) (int, bool) {
	region, ok := m.RegionAtLine(line)
	if !ok {
		return 0, false
	}
	if line != region.Start() {
		return region.Start(), true
	}
	if parent := region.Parent(); parent != -1 {
		return m.Regions().Start(parent), true
	}
	return 0, false
}

// PreviousFoldLine is the line to go to for the previous fold at the level of a line.
func PreviousFoldLine(line int, m *Model) (int, bool) {
	regions := m.Regions()
	region, ok := m.RegionAtLine(line)
	if ok && region.Start() == line {
		// On the first line of a region: its previous sibling.
		expected := region.Parent()
		minLine := -1
		if expected != -1 {
			minLine = regions.Start(expected)
		}
		for index := region.Index(); index > 0; {
			index--
			prev := regions.Region(index)
			if prev.Start() <= minLine {
				return 0, false
			}
			if prev.Parent() == expected {
				return prev.Start(), true
			}
		}
		return 0, false
	}
	// The last fold that starts before the line.
	for index := regions.Len() - 1; index >= 0; index-- {
		if regions.Start(index) < line {
			return regions.Start(index), true
		}
	}
	return 0, false
}

// NextFoldLine is the line to go to for the next fold at the level of a line.
func NextFoldLine(line int, m *Model) (int, bool) {
	regions := m.Regions()
	region, ok := m.RegionAtLine(line)
	if ok && region.Start() == line {
		// On the first line of a region: its next sibling.
		expected := region.Parent()
		var maxLine int
		switch {
		case expected != -1:
			maxLine = regions.End(expected)
		case regions.Len() == 0:
			return 0, false
		default:
			maxLine = regions.End(regions.Len() - 1)
		}
		for index := region.Index() + 1; index < regions.Len(); index++ {
			next := regions.Region(index)
			if next.Start() >= maxLine {
				return 0, false
			}
			if next.Parent() == expected {
				return next.Start(), true
			}
		}
		return 0, false
	}
	// The first fold that starts after the line.
	for index := 0; index < regions.Len(); index++ {
		if regions.Start(index) > line {
			return regions.Start(index), true
		}
	}
	return 0, false
}
