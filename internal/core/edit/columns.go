package edit

// isFullWidth reports whether a code point takes two columns on a grid: the
// ranges of VS Code's isFullWidthCharacter (CJK, Hangul syllables, fullwidth
// forms).
func isFullWidth(r rune) bool {
	return (r >= 0x2E80 && r <= 0xD7AF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFF01 && r <= 0xFF5E) ||
		(r >= 0xFFE0 && r <= 0xFFE6)
}

// isEmojiImprecise is VS Code's quick, imprecise test for emoji code points.
func isEmojiImprecise(r rune) bool {
	return (r >= 0x1F1E6 && r <= 0x1F1FF) || r == 8986 || r == 8987 || r == 9200 ||
		r == 9203 || (r >= 9728 && r <= 10175) || r == 11088 || r == 11093 ||
		(r >= 127744 && r <= 128591) || (r >= 128640 && r <= 128764) ||
		(r >= 128992 && r <= 129008) || (r >= 129280 && r <= 129535) ||
		(r >= 129648 && r <= 129782)
}

// tabSizeOf makes a tab size usable: a size below 1 counts as 1.
func tabSizeOf(tabSize int) int {
	return max(tabSize, 1)
}

// nextVisibleColumn is the visual column after a grapheme cluster whose first
// code point is r, when it starts at visible.
func nextVisibleColumn(r rune, visible, tabSize int) int {
	switch {
	case r == '\t':
		return NextRenderTabStop(visible, tabSize)
	case isFullWidth(r) || isEmojiImprecise(r):
		return visible + 2
	}
	return visible + 1
}

// VisibleColumn is the visual column of the byte column col in line (SPEC §6.2,
// VS Code's CursorColumns.visibleColumnFromColumn): walk the grapheme clusters
// before col; for each take its first code point: a tab goes to the next tab
// stop, a full-width character or an emoji counts 2, anything else 1. A col
// outside the line is moved into it.
func VisibleColumn(line string, col, tabSize int) int {
	tabSize = tabSizeOf(tabSize)
	col = min(max(col, 0), len(line))
	visible := 0
	forEachGrapheme(line[:col], func(_, _ int, first rune) bool {
		visible = nextVisibleColumn(first, visible, tabSize)
		return true
	})
	return visible
}

// ColumnFromVisible is the byte column in line nearest to a visual column (VS
// Code's CursorColumns.columnFromVisibleColumn): a visual column inside a tab or
// a wide character goes to the nearer end of it, and to the one before when both
// are as near. A visual column at or before 0 gives 0; one past the end of the
// line gives len(line).
func ColumnFromVisible(line string, visible, tabSize int) int {
	if visible <= 0 {
		return 0
	}
	tabSize = tabSizeOf(tabSize)
	result := len(line)
	beforeVisible, beforeCol := 0, 0
	forEachGrapheme(line, func(_, end int, first rune) bool {
		afterVisible := nextVisibleColumn(first, beforeVisible, tabSize)
		if afterVisible >= visible {
			if afterVisible-visible < visible-beforeVisible {
				result = end
			} else {
				result = beforeCol
			}
			return false
		}
		beforeVisible, beforeCol = afterVisible, end
		return true
	})
	return result
}

// StatusbarColumn is the number the status bar shows as "Col" for the byte
// column col in line (VS Code's CursorColumns.toStatusbarColumn): every code
// point counts 1 and a tab goes to the next tab stop, then one is added. Unlike
// VisibleColumn it does not make wide characters and emoji count 2.
func StatusbarColumn(line string, col, tabSize int) int {
	tabSize = tabSizeOf(tabSize)
	col = min(max(col, 0), len(line))
	result := 0
	for _, r := range line[:col] {
		if r == '\t' {
			result = NextRenderTabStop(result, tabSize)
		} else {
			result++
		}
	}
	return result + 1
}

// NextRenderTabStop is the first tab stop after a visual column.
func NextRenderTabStop(visible, tabSize int) int {
	tabSize = tabSizeOf(tabSize)
	return visible + tabSize - visible%tabSize
}

// NextIndentTabStop is the first indent stop after a visual column; it is
// NextRenderTabStop with the indent size.
func NextIndentTabStop(visible, indentSize int) int {
	return NextRenderTabStop(visible, indentSize)
}

// PrevRenderTabStop is the last tab stop before a visual column, or 0.
func PrevRenderTabStop(visible, tabSize int) int {
	tabSize = tabSizeOf(tabSize)
	return max(0, visible-1-(visible-1)%tabSize)
}

// PrevIndentTabStop is the last indent stop before a visual column; it is
// PrevRenderTabStop with the indent size.
func PrevIndentTabStop(visible, indentSize int) int {
	return PrevRenderTabStop(visible, indentSize)
}
