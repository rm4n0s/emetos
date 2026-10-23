package emmet

import "strings"

// Ported from Emmet: src/extract-abbreviation/index.ts, brackets.ts, quotes.ts, reader.ts and is-html.ts
//
// The functions work on bytes, so the places they give are byte offsets. A byte of a multi-byte
// character is never one of the characters of an abbreviation, and is treated as text.

// MaxExtractLength is the longest abbreviation ExtractAbbreviation looks for before the cursor, in
// bytes. Emmet has no limit; the search is linear in the size of the line, which can be a megabyte
// in a minified file, and an abbreviation is never that long. An abbreviation that would be longer
// is not found.
const MaxExtractLength = 4096

// ExtractOptions are the options of ExtractAbbreviation.
type ExtractOptions struct {
	// LookAhead lets the search look past the position for the parts an editor has most likely
	// closed (`]`, `}`, `)` and a quote right after the cursor). Emmet's default is true.
	LookAhead bool
	// Type is the kind of abbreviation. In a stylesheet only parentheses are balanced: `[]` and `{}`
	// are not part of it.
	Type SyntaxType
	// Prefix, if not empty, has to be before the abbreviation: the abbreviation is searched from the
	// nearest occurrence of the prefix before the position.
	Prefix string
}

// DefaultExtractOptions gives the default options of Emmet: markup, with a look ahead and no prefix.
func DefaultExtractOptions() ExtractOptions {
	return ExtractOptions{LookAhead: true, Type: TypeMarkup}
}

// Extracted is an abbreviation found in a line.
type Extracted struct {
	// Abbreviation is the text of the abbreviation.
	Abbreviation string
	// Location is its start in the line, and End its end (byte offsets).
	Location, End int
	// Start is the start of the match including the prefix.
	Start int
}

// ExtractAbbreviation finds the abbreviation that ends at pos in the line (clamped to the line), the way
// an editor needs it: `<span>.foo[title=bar|]</span>` gives `.foo[title=bar]`, where | is pos.
func ExtractAbbreviation(line string, pos int, opts ExtractOptions) (Extracted, bool) {
	pos = min(len(line), max(0, pos))
	if opts.Type == "" {
		opts.Type = TypeMarkup
	}

	if opts.LookAhead {
		pos = offsetPastAutoClosed(line, pos, opts.Type)
	}

	start := 0
	if opts.Prefix != "" {
		start = getStartOffset(line, pos, opts.Prefix)
		if start == -1 {
			return Extracted{}, false
		}
	}
	// The search never goes further back than the limit
	limit := max(start, pos-MaxExtractLength)

	sc := &backwardScanner{text: line, start: limit, pos: pos}
	var stack []int
	curlyR, squareR := 0, 0 // how many of those the stack has (the stack is searched for them)
	hitLimit := false

loop:
	for !sc.sol() {
		ch := sc.peek()

		if curlyR > 0 {
			if ch == '}' {
				stack = append(stack, ch)
				curlyR++
				sc.pos--
				continue
			}
			if ch != '{' {
				sc.pos--
				continue
			}
		}

		switch {
		case isCloseBrace(ch, opts.Type):
			stack = append(stack, ch)
			switch ch {
			case '}':
				curlyR++
			case ']':
				squareR++
			}
		case isOpenBrace(ch, opts.Type):
			if len(stack) == 0 {
				break loop // an unexpected brace
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch top {
			case '}':
				curlyR--
			case ']':
				squareR--
			}
			if top != closingBrace(ch) {
				break loop
			}
		case squareR > 0 || curlyR > 0:
			// Every character inside the attributes and the text is respected
			sc.pos--
			continue
		case isAtHTMLTag(sc) || !isAbbreviationChar(ch):
			break loop
		}
		sc.pos--
	}
	if sc.sol() && limit > start {
		hitLimit = true
	}

	if len(stack) == 0 && sc.pos != pos && !hitLimit {
		// Something was found: remove the symbols at its start that are not valid there
		abbreviation := strings.TrimLeft(line[sc.pos:pos], "*+>^")
		res := Extracted{Abbreviation: abbreviation, Location: pos - len(abbreviation), End: pos, Start: pos - len(abbreviation)}
		if opts.Prefix != "" {
			res.Start = start - len(opts.Prefix)
		}
		return res, true
	}
	return Extracted{}, false
}

// offsetPastAutoClosed gives the position right after the characters past pos that an editor
// closed by itself: } ] ) and a quote.
func offsetPastAutoClosed(line string, pos int, typ SyntaxType) int {
	// A closing quote is allowed only as the next character
	if isQuoteChar(byteAt(line, pos)) {
		pos++
	}
	for isCloseBrace(byteAt(line, pos), typ) {
		pos++
	}
	return pos
}

// getStartOffset gives the place from where the search stops going left: the position after
// the nearest occurrence of the prefix. It gives -1 if there is none.
func getStartOffset(line string, pos int, prefix string) int {
	sc := &backwardScanner{text: line, pos: pos}
	for !sc.sol() {
		if consumePair(sc, ']', '[') || consumePair(sc, '}', '{') {
			continue
		}
		result := sc.pos
		if consumeString(sc, prefix) {
			return result
		}
		sc.pos--
	}
	return -1
}

// consumePair consumes a whole pair of characters, with what is between them.
func consumePair(sc *backwardScanner, closeCh, openCh int) bool {
	start := sc.pos
	if sc.consume(closeCh) {
		for !sc.sol() {
			if sc.consume(openCh) {
				return true
			}
			sc.pos--
		}
	}
	sc.pos = start
	return false
}

// consumeString consumes the bytes of s, from right to left.
func consumeString(sc *backwardScanner, s string) bool {
	start := sc.pos
	consumed := false
	for i := len(s) - 1; i >= 0 && !sc.sol(); i-- {
		if !sc.consume(int(s[i])) {
			break
		}
		consumed = i == 0
	}
	if !consumed {
		sc.pos = start
	}
	return consumed
}

// isAbbreviationChar tells if the character may be a part of an abbreviation: a letter, a digit or one of #.*:$-_!@%^+>/
func isAbbreviationChar(ch int) bool {
	return (ch > 64 && ch < 91) || (ch > 96 && ch < 123) || (ch > 47 && ch < 58) || (ch > 0 && strings.IndexByte("#.*:$-_!@%^+>/", byte(ch)) >= 0)
}

func isOpenBrace(ch int, typ SyntaxType) bool {
	return ch == '(' || (typ != TypeStylesheet && (ch == '[' || ch == '{'))
}

func isCloseBrace(ch int, typ SyntaxType) bool {
	return ch == ')' || (typ != TypeStylesheet && (ch == ']' || ch == '}'))
}

// closingBrace gives the brace that closes an opening one.
func closingBrace(open int) int {
	switch open {
	case '[':
		return ']'
	case '(':
		return ')'
	case '{':
		return '}'
	}
	return -1
}

// byteAt gives the byte of the string at the place, or -1 outside of it.
func byteAt(s string, i int) int {
	if i < 0 || i >= len(s) {
		return -1
	}
	return int(s[i])
}

// backwardScanner reads a text from right to left, down to start.
type backwardScanner struct {
	text  string
	start int
	pos   int
}

// sol tells if the scanner is at the start of the text.
func (b *backwardScanner) sol() bool { return b.pos <= b.start }

// peek gives the byte before the position, without moving.
func (b *backwardScanner) peek() int { return byteAt(b.text, b.pos-1) }

// previous gives the byte before the position and moves before it; -1 at the start.
func (b *backwardScanner) previous() int {
	if b.sol() {
		return -1
	}
	b.pos--
	return int(b.text[b.pos])
}

// consume moves over the byte before the position if it is the given one.
func (b *backwardScanner) consume(ch int) bool {
	if b.sol() {
		return false
	}
	if b.peek() == ch {
		b.pos--
		return true
	}
	return false
}

// consumeIf moves over the byte before the position if the function accepts it.
func (b *backwardScanner) consumeIf(match func(int) bool) bool {
	if b.sol() {
		return false
	}
	if match(b.peek()) {
		b.pos--
		return true
	}
	return false
}

func (b *backwardScanner) consumeWhile(match func(int) bool) bool {
	start := b.pos
	for b.consumeIf(match) {
	}
	return b.pos < start
}

// consumeQuoted moves over a string in quotes that ends at the position, if there is one.
func consumeQuoted(sc *backwardScanner) bool {
	start := sc.pos
	quote := sc.previous()
	if isQuoteChar(quote) {
		for !sc.sol() {
			if sc.previous() == quote && sc.peek() != '\\' {
				return true
			}
		}
	}
	sc.pos = start
	return false
}

// isAtHTMLTag tells if the position is at the end of an HTML tag.
func isAtHTMLTag(sc *backwardScanner) bool {
	start := sc.pos

	if !sc.consume('>') {
		return false
	}

	ok := false
	sc.consume('/') // maybe an element that closes itself

	for !sc.sol() {
		sc.consumeWhile(isTagSpace)

		if consumeIdent(sc) {
			// An identifier: the name of a tag, a boolean attribute or an unquoted value
			if sc.consume('/') {
				// A closing tag, or an invalid one
				ok = sc.consume('<')
				break
			} else if sc.consume('<') {
				// An opening tag
				ok = true
				break
			} else if sc.consumeIf(isTagSpace) {
				// A boolean attribute
				continue
			} else if sc.consume('=') {
				// A simple unquoted value, or an invalid attribute
				if consumeIdent(sc) {
					continue
				}
				break
			} else if consumeAttributeWithUnquotedValue(sc) {
				// The identifier was a part of an unquoted value
				ok = true
				break
			}
			// An invalid tag
			break
		}

		if consumeAttributeWithQuotedValue(sc) || consumeAttributeWithUnquotedValue(sc) {
			continue
		}
		break
	}

	sc.pos = start
	return ok
}

func consumeAttributeWithQuotedValue(sc *backwardScanner) bool {
	start := sc.pos
	if consumeQuoted(sc) && sc.consume('=') && consumeIdent(sc) {
		return true
	}
	sc.pos = start
	return false
}

func consumeAttributeWithUnquotedValue(sc *backwardScanner) bool {
	start := sc.pos
	var stack []int
	for !sc.sol() {
		ch := sc.peek()
		if ch == '}' || ch == ')' || ch == ']' {
			stack = append(stack, ch)
		} else if ch == '{' || ch == '(' || ch == '[' {
			if len(stack) == 0 || stack[len(stack)-1] != closingBrace(ch) {
				break // an open bracket that is not expected
			}
			stack = stack[:len(stack)-1]
		} else if !isUnquotedValue(ch) {
			break
		}
		sc.pos--
	}

	if start != sc.pos && sc.consume('=') && consumeIdent(sc) {
		return true
	}
	sc.pos = start
	return false
}

func consumeIdent(sc *backwardScanner) bool { return sc.consumeWhile(isHTMLIdent) }

// isHTMLIdent tells if the byte belongs to an HTML identifier.
func isHTMLIdent(ch int) bool { return ch == ':' || ch == '-' || isAlpha(ch) || isNumber(ch) }

func isTagSpace(ch int) bool { return ch == ' ' || ch == '\t' }

// isUnquotedValue tells if the byte may belong to the value of an attribute with no quotes.
func isUnquotedValue(ch int) bool {
	return ch != -1 && ch != '=' && !isTagSpace(ch) && !isQuoteChar(ch)
}
