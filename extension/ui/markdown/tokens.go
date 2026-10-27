package markdown

import "github.com/rm4n0s/emetos/extension"

// maxCachedHighlights is the number of highlighted code blocks a view remembers: a document with hundreds of code blocks is
// highlighted for the ones near the viewport, and the highlighting of one that was scrolled away is dropped (the oldest first).
const maxCachedHighlights = 256

// tokenCache remembers the tokens of code blocks by language and text, so a block that is built again (the window moves back to
// it, the theme changes) is not highlighted again. A block the highlighter said no to is remembered too, as nil lines.
type tokenCache struct {
	lines map[string][][]extension.Token
	order []string
}

func newTokenCache() *tokenCache { return &tokenCache{lines: map[string][][]extension.Token{}} }

// get gives the lines of a key (nil for a block with no highlighting) and whether the key is known.
func (c *tokenCache) get(key string) ([][]extension.Token, bool) {
	lines, ok := c.lines[key]
	return lines, ok
}

// put remembers the lines of a key, dropping the oldest when there are too many.
func (c *tokenCache) put(key string, lines [][]extension.Token) {
	if _, ok := c.lines[key]; !ok {
		c.order = append(c.order, key)
		if len(c.order) > maxCachedHighlights {
			delete(c.lines, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.lines[key] = lines
}
