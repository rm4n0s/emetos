package pdf

import (
	"unicode"

	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// Finding text (SPEC §10.37 "PDF"): a match is looked for in each line of a page, its words joined by single spaces, so that a match
// may run across the words of a line; a match has the boxes of the parts of the words it covers, in points from the top left of the
// page.

// box is a rectangle on a page, in points from its top left.
type box struct {
	x0, y0, x1, y1 float64
}

// match is a place where the text was found.
type match struct {
	page  int
	boxes []box
}

// foldRune is a rune as a search without match case compares it.
func foldRune(r rune) rune { return unicode.ToLower(unicode.ToUpper(r)) }

// findInPage finds every place of the text in the lines of a page (from 0), left to right and top to bottom; matches do not overlap.
func findInPage(page int, words poppler.PageWords, text string, matchCase bool) []match {
	query := []rune(text)
	if len(query) == 0 {
		return nil
	}
	if !matchCase {
		for i, r := range query {
			query[i] = foldRune(r)
		}
	}
	var out []match
	for _, line := range words.Lines {
		// the line's runes, and for each the word it is in (-1 for the space between two words)
		var runes []rune
		var owner []int
		var offset []int // the rune's place in its word
		for wi, w := range line.Words {
			if wi > 0 {
				runes, owner, offset = append(runes, ' '), append(owner, -1), append(offset, 0)
			}
			for ri, r := range []rune(w.Text) {
				if !matchCase {
					r = foldRune(r)
				}
				runes, owner, offset = append(runes, r), append(owner, wi), append(offset, ri)
			}
		}
		for i := 0; i+len(query) <= len(runes); {
			if !equalRunes(runes[i:i+len(query)], query) {
				i++
				continue
			}
			out = append(out, match{page: page, boxes: boxesOf(line, owner[i:i+len(query)], offset[i:i+len(query)])})
			i += len(query)
		}
	}
	return out
}

func equalRunes(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// boxesOf are the boxes of the parts of the words that the runes of a match cover: each word's box cut in proportion to the runes of
// it that are in the match.
func boxesOf(line poppler.Line, owner, offset []int) []box {
	var out []box
	for i := 0; i < len(owner); {
		wi := owner[i]
		if wi < 0 {
			i++
			continue
		}
		first := offset[i]
		j := i
		for j+1 < len(owner) && owner[j+1] == wi {
			j++
		}
		last := offset[j]
		w := line.Words[wi]
		n := float64(len([]rune(w.Text)))
		width := w.XMax - w.XMin
		out = append(out, box{x0: w.XMin + width*float64(first)/n, y0: w.YMin, x1: w.XMin + width*float64(last+1)/n, y1: w.YMax})
		i = j + 1
	}
	return out
}
