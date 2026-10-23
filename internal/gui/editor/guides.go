package editor

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/indent"
)

// Ported from VS Code: src/vs/editor/browser/viewParts/indentGuides/indentGuides.ts (MIT)

// GuideColor is the color of the guides of a level and the one they have when highlighted.
type GuideColor struct {
	Color, Active ui.Color
}

// GuideColors are the colors of the indent guides and of the bracket pair guides, by level. Only
// the levels that have a color are listed, and the levels of the guides go round the list.
type GuideColors struct {
	Indent, Bracket []GuideColor
}

// guideColor is the color of the guides of a level, or false when no level has a color.
func guideColor(list []GuideColor, level int) (GuideColor, bool) {
	if len(list) == 0 {
		return GuideColor{}, false
	}
	return list[(level%indent.GuideColors)%len(list)], true
}

// transparentColor scales the opacity of a color.
func transparentColor(c ui.Color, factor float64) ui.Color {
	return ui.NewColorFromARGB(int(float64(c.Alpha())*factor+0.5), c.Red(), c.Green(), c.Blue())
}

// NewGuideColors makes the colors of the guides from the colors of a theme. A bracket pair guide
// with no color of its own takes the color of the brackets of its level, with 30% of its opacity
// when inactive; a level whose colors are transparent is left out, so that a theme that colors the
// first three levels has guides of three colors.
//
// Ported from VS Code: indentGuides.ts (registerThemingParticipant)
func NewGuideColors(indentColors, indentActive, pairColors, pairActive, bracketColors [6]ui.Color) GuideColors {
	var out GuideColors
	for i := range indentColors {
		if indentColors[i].Alpha() > 0 && indentActive[i].Alpha() > 0 {
			out.Indent = append(out.Indent, GuideColor{Color: indentColors[i], Active: indentActive[i]})
		}
	}
	for i := range pairColors {
		color, active := pairColors[i], pairActive[i]
		if color.Alpha() == 0 {
			color = transparentColor(bracketColors[i], 0.3)
		}
		if active.Alpha() == 0 {
			active = bracketColors[i]
		}
		if color.Alpha() > 0 && active.Alpha() > 0 {
			out.Bracket = append(out.Bracket, GuideColor{Color: color, Active: active})
		}
	}
	return out
}

// SetGuides gives the view model the indentation of the lines of its document, which the guides
// are drawn from, and whether the language is indented to nest (folding.offSide); nil takes the
// guides away. The view model does not own the levels: whoever made them (indent.AttachLevels)
// keeps them up to date.
func (vm *ViewModel) SetGuides(levels *indent.Levels, offSide bool) {
	vm.levels, vm.offSide = levels, offSide
	vm.NotifyListeners()
}

// GuideLevels are the indentation levels the guides are drawn from, or nil.
func (vm *ViewModel) GuideLevels() *indent.Levels { return vm.levels }

// Indenter is what applies the indentation rules of the languages, or nil.
func (vm *ViewModel) Indenter() *indent.Indenter { return vm.indenter }

// guides is the computation of the guides over the text of the view model.
func (vm *ViewModel) guides() indent.Guides {
	tabSize, _ := vm.doc.Indentation()
	// A change of the tab size changes the levels of lines indented with tabs.
	vm.levels.SetTabSize(tabSize)
	return indent.Guides{
		// Emetos has no separate editor.indentSize: an indent level is a tab size.
		Levels: vm.levels, Text: indent.TextLines(vm.doc), IndentSize: max(tabSize, 1),
		OffSide: vm.offSide, VisibleColumn: edit.VisibleColumn,
	}
}

// rowGuides gives the guides of the rows first to last: for each, the indent guides of its line,
// its bracket pair guides, and the highlight of the scope the cursor is in. A row that continues
// a wrapped line has none.
func (vm *ViewModel) rowGuides(first, last int) [][]indent.Guide {
	opts := vm.opts.Guides
	if vm.levels == nil || opts.Off() {
		return nil
	}
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(first)
	lastLine, _, _ := proj.RowSpan(last)
	// VS Code asks for one line more than the screen shows.
	endLine := min(lastLine+1, vm.doc.Snapshot().LineCount()-1)
	var pairs indent.PairSource
	if ix := vm.bracketIndex; ix != nil && ix.Enabled() {
		pairs = ix
	}
	cursor := vm.cursors.Primary().Selection.Active
	perLine := vm.guides().LineGuides(pairs, firstLine, endLine, &cursor, opts)
	out := make([][]indent.Guide, last-first+1)
	for row := first; row <= last; row++ {
		line, start, _ := proj.RowSpan(row)
		if start > 0 || line < firstLine || line-firstLine >= len(perLine) {
			continue
		}
		out[row-first] = perLine[line-firstLine]
	}
	return out
}
