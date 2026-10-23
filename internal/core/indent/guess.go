package indent

// Ported from VS Code: reference/vscode/src/vs/editor/common/model/indentationGuesser.ts (MIT)

// Lines is the text that Guess reads: lines by zero-based number, without their line break.
type Lines interface {
	// LineCount is the number of lines, at least 1.
	LineCount() int
	// LineContent is the text of a line.
	LineContent(line int) string
}

// Guessed is what Guess found.
type Guessed struct {
	// TabSize is the number of spaces that make an indent, when the indentation is spaces; the
	// default otherwise.
	TabSize int
	// InsertSpaces tells that the indentation is made of spaces.
	InsertSpaces bool
}

// maxGuessLines is how many lines Guess looks at.
const maxGuessLines = 10000

// maxAllowedTabSizeGuess is the largest tab size Guess finds.
const maxAllowedTabSizeGuess = 8

// allowedTabSizeGuesses are the tab sizes Guess finds, in the order it prefers them: even ones
// first, and no more than 8.
var allowedTabSizeGuesses = [...]int{2, 4, 6, 8, 3, 5, 7}

// spacesDiffResult is the difference in spaces between the indentation of two lines.
type spacesDiffResult struct {
	spacesDiff         int
	looksLikeAlignment bool
}

// spacesDiff computes the difference in spaces between the indentation of two lines: a is the
// first aLength bytes of the line above, b the first bLength of the one below.
func spacesDiff(a string, aLength int, b string, bLength int) spacesDiffResult {
	// This can go both ways, as in a: "\t" and b: "\t    ", which is 1 tab and 4 spaces.
	i := 0
	for i < aLength && i < bLength && a[i] == b[i] {
		i++
	}
	aSpaces, aTabs := 0, 0
	for j := i; j < aLength; j++ {
		if a[j] == ' ' {
			aSpaces++
		} else {
			aTabs++
		}
	}
	bSpaces, bTabs := 0, 0
	for j := i; j < bLength; j++ {
		if b[j] == ' ' {
			bSpaces++
		} else {
			bTabs++
		}
	}
	if (aSpaces > 0 && aTabs > 0) || (bSpaces > 0 && bTabs > 0) {
		return spacesDiffResult{}
	}
	tabsDiff := abs(aTabs - bTabs)
	spaces := abs(aSpaces - bSpaces)
	if tabsDiff == 0 {
		// The difference may come from alignment, which is no hint: sometimes folks align code,
		//   const a = b + c,
		//         d = b - c;
		res := spacesDiffResult{spacesDiff: spaces}
		if spaces > 0 && 0 <= bSpaces-1 && bSpaces-1 < len(a) && bSpaces < len(b) {
			if b[bSpaces] != ' ' && a[bSpaces-1] == ' ' && a[len(a)-1] == ',' {
				res.looksLikeAlignment = true
			}
		}
		return res
	}
	if spaces%tabsDiff == 0 {
		return spacesDiffResult{spacesDiff: spaces / tabsDiff}
	}
	return spacesDiffResult{}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Guess finds the indentation a text uses, from its first 10,000 lines: whether it is made of
// spaces (the lines that start with tabs against those that start with spaces; a tie keeps the
// default) and, when it is, how many spaces make an indent (the difference between the
// indentation of lines that follow each other, the most common one in the order 2, 4, 6, 8, 3,
// 5, 7; 2 wins over 4 when it has two thirds of its occurrences). With no clue it returns the
// defaults.
//
// Ported from VS Code: indentationGuesser.ts (guessIndentation)
func Guess(src Lines, defaultTabSize int, defaultInsertSpaces bool) Guessed {
	lines := min(src.LineCount(), maxGuessLines)
	tabLines, spaceLines := 0, 0
	previousText, previousIndentation := "", 0
	var diffCount [maxAllowedTabSizeGuess + 1]int
	for n := 0; n < lines; n++ {
		text := src.LineContent(n)
		hasContent, indentation, spaces, tabs := false, 0, 0, 0
		for j := 0; j < len(text); j++ {
			switch text[j] {
			case '\t':
				tabs++
				continue
			case ' ':
				spaces++
				continue
			}
			hasContent, indentation = true, j
			break
		}
		// Empty and white-space-only lines say nothing.
		if !hasContent {
			continue
		}
		if tabs > 0 {
			tabLines++
		} else if spaces > 1 {
			spaceLines++
		}
		diff := spacesDiff(previousText, previousIndentation, text, indentation)
		if diff.looksLikeAlignment {
			// With spaces as the default and a difference that is the tab size, it may count as
			// an indentation (a list in Markdown); otherwise the line is skipped.
			if !defaultInsertSpaces || defaultTabSize != diff.spacesDiff {
				continue
			}
		}
		if diff.spacesDiff <= maxAllowedTabSizeGuess {
			diffCount[diff.spacesDiff]++
		}
		previousText, previousIndentation = text, indentation
	}
	insertSpaces := defaultInsertSpaces
	if tabLines != spaceLines {
		insertSpaces = tabLines < spaceLines
	}
	tabSize := defaultTabSize
	if insertSpaces {
		score := 0
		for _, possible := range allowedTabSizeGuesses {
			if diffCount[possible] > score {
				score = diffCount[possible]
				tabSize = possible
			}
		}
		// 2 wins over 4 when it has at least two thirds of its occurrences: a YAML file with
		// 2 spaces has some deeper 4-space differences.
		if tabSize == 4 && diffCount[4] > 0 && diffCount[2] > 0 && float64(diffCount[2]) >= float64(diffCount[4])*2/3 {
			tabSize = 2
		}
	}
	return Guessed{TabSize: tabSize, InsertSpaces: insertSpaces}
}
