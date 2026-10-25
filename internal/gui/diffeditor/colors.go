package diffeditor

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Colors are the colors the diff editor paints with, from the theme (SPEC §9.11). A color the theme leaves
// transparent takes the one VS Code's style sheet falls back to: the margin of a changed line and the mark on the
// overview ruler take the background of the line, and the line between the two editors takes the border between
// editor groups.
type Colors struct {
	// InsertedText and RemovedText are the backgrounds of the characters that were inserted and removed, and
	// InsertedTextBorder and RemovedTextBorder their outlines (the high contrast themes).
	InsertedText, RemovedText, InsertedTextBorder, RemovedTextBorder ui.Color
	// InsertedLine and RemovedLine are the backgrounds of the lines, InsertedGutter and RemovedGutter the colors of the
	// margin along them.
	InsertedLine, RemovedLine, InsertedGutter, RemovedGutter ui.Color
	// InsertedOverview and RemovedOverview are the marks on the overview ruler.
	InsertedOverview, RemovedOverview ui.Color
	// Border is the line between the two editors, DiagonalFill the lines of the filler.
	Border, DiagonalFill ui.Color
}

// ColorsOf takes the diff editor's colors from the workbench colors.
func ColorsOf(c kit.Colors) Colors {
	or := func(a, b ui.Color) ui.Color {
		if a.Alpha() == 0 {
			return b
		}
		return a
	}
	return Colors{
		InsertedText: c.DiffInsertedTextBackground, RemovedText: c.DiffRemovedTextBackground,
		InsertedTextBorder: c.DiffInsertedTextBorder, RemovedTextBorder: c.DiffRemovedTextBorder,
		InsertedLine: c.DiffInsertedLineBackground, RemovedLine: c.DiffRemovedLineBackground,
		InsertedGutter:   or(c.DiffInsertedLineGutter, c.DiffInsertedLineBackground),
		RemovedGutter:    or(c.DiffRemovedLineGutter, c.DiffRemovedLineBackground),
		InsertedOverview: or(c.DiffOverviewInserted, c.DiffInsertedLineBackground),
		RemovedOverview:  or(c.DiffOverviewRemoved, c.DiffRemovedLineBackground),
		Border:           or(c.DiffBorder, c.EditorGroupBorder), DiagonalFill: c.DiffDiagonalFill,
	}
}
