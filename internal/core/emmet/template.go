package emmet

// Ported from Emmet: src/markup/format/template.ts

// templateToken is a piece of a template: a string (Text) or a placeholder.
type templateToken struct {
	text string
	// placeholder is set for a placeholder, with the text before and after its name.
	placeholder bool
	before      string
	after       string
	name        string
}

// parseTemplate splits a string into template tokens. A placeholder is an upper case name
// between `[` and `]`, like `[ID]`; it may have other characters before and after the name
// (`[%ID.]`), which are written only if there is data for the name.
func parseTemplate(text string) []templateToken {
	var tokens []templateToken
	pos, offset := 0, 0
	for pos < len(text) {
		start := pos
		ph, end, ok := consumeTemplatePlaceholder(text, pos)
		if ok {
			if offset != end {
				tokens = append(tokens, templateToken{text: text[offset:start]})
			}
			tokens = append(tokens, ph)
			offset = end
			pos = end
		} else {
			// A `[` that is never closed has read to the end of the text
			pos = end + 1
		}
	}
	if offset != len(text) {
		tokens = append(tokens, templateToken{text: text[offset:]})
	}
	return tokens
}

// consumeTemplatePlaceholder reads `[#ID]` at a position; end is the position after it, or after what was read when there is no placeholder.
func consumeTemplatePlaceholder(text string, pos int) (templateToken, int, bool) {
	if text[pos] != '[' {
		return templateToken{}, pos, false
	}
	pos++
	start := pos
	namePos, afterPos := start, start
	stack := 1

	for pos < len(text) {
		code := text[pos]
		if isTemplateTokenStart(code) {
			namePos = pos
			for pos < len(text) && isTemplateToken(text[pos]) {
				pos++
			}
			afterPos = pos
			continue
		}
		switch code {
		case '[':
			stack++
		case ']':
			stack--
			if stack == 0 {
				return templateToken{
					placeholder: true,
					before:      text[start:namePos],
					after:       text[afterPos:pos],
					name:        text[namePos:afterPos],
				}, pos + 1, true
			}
		}
		pos++
	}
	return templateToken{}, pos, false
}

func isTemplateTokenStart(c byte) bool { return c >= 'A' && c <= 'Z' }

func isTemplateToken(c byte) bool {
	return isTemplateTokenStart(c) || (c >= '0' && c <= '9') || c == '_' || c == '-'
}
