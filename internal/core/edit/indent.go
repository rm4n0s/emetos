package edit

import "strings"

// NormalizeIndentation rewrites the leading white space of str so that it uses
// tabs (up to the indent size) or only spaces, keeping its visual width. It is
// VS Code's normalizeIndentation.
//
// Ported from VS Code: src/vs/editor/common/core/misc/indentation.ts
func NormalizeIndentation(str string, indentSize int, insertSpaces bool) string {
	indentSize = tabSizeOf(indentSize)
	end := firstNonWhitespace(str)
	if end < 0 {
		end = len(str)
	}
	width := 0
	for i := 0; i < end; i++ {
		if str[i] == '\t' {
			width = NextIndentTabStop(width, indentSize)
		} else {
			width++
		}
	}
	var sb strings.Builder
	if !insertSpaces {
		sb.WriteString(strings.Repeat("\t", width/indentSize))
		width %= indentSize
	}
	sb.WriteString(strings.Repeat(" ", width))
	sb.WriteString(str[end:])
	return sb.String()
}

// leadingWhitespace is the spaces and tabs a line starts with.
func leadingWhitespace(line string) string {
	end := firstNonWhitespace(line)
	if end < 0 {
		return line
	}
	return line[:end]
}

// isBlank reports whether a line has nothing but white space.
func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

// shiftedIndent is the indentation that ends at byte column col of line, one
// level deeper (ShiftCommand.shiftIndent) or shallower (unshiftIndent): the
// visual column where the content starts is moved to the next or previous indent
// stop (render stop when indenting with tabs).
func shiftedIndent(line string, col, tabSize, indentSize int, insertSpaces, unshift bool) string {
	visible := VisibleColumn(line, col, tabSize)
	if insertSpaces {
		stop := NextIndentTabStop(visible, indentSize)
		if unshift {
			stop = PrevIndentTabStop(visible, indentSize)
		}
		return strings.Repeat(strings.Repeat(" ", indentSize), stop/indentSize)
	}
	stop := NextRenderTabStop(visible, tabSize)
	if unshift {
		stop = PrevRenderTabStop(visible, tabSize)
	}
	return strings.Repeat("\t", stop/tabSize)
}
