package emmet

import (
	"strings"
)

// Ported from Emmet: src/stylesheet/format.ts

// StringifyStylesheet writes the properties of a stylesheet abbreviation as the configuration says.
func StringifyStylesheet(props []*CSSProperty, cfg Config) string {
	out := newOutputStream(&cfg.Options)
	format := cfg.Options.Format

	if cfg.Context != nil && cfg.Context.Name == ScopeSection {
		// In a section the properties that matched no snippet are left out
		kept := make([]*CSSProperty, 0, len(props))
		for _, p := range props {
			if p.snippet != nil {
				kept = append(kept, p)
			}
		}
		props = kept
	}

	for i, p := range props {
		if format && i != 0 {
			out.pushNewline(useLevel)
		}
		writeProperty(p, out, &cfg)
	}
	return out.value()
}

// writeProperty writes a property.
func writeProperty(node *CSSProperty, out *outputStream, cfg *Config) {
	isJSON := cfg.Options.StylesheetJSON
	if node.Name != "" {
		// A CSS property
		name := node.Name
		if isJSON {
			name = toCamelCase(name)
		}
		out.pushString(name + cfg.Options.StylesheetBetween)

		if len(node.Value) > 0 {
			writePropertyValue(node, out, cfg)
		} else {
			out.pushField(0, "")
		}

		if isJSON {
			// For CSS-in-JS a property always ends with a comma (`important` does not exist there)
			out.push(",")
		} else {
			writeImportant(node, out, true)
			out.push(cfg.Options.StylesheetAfter)
		}
		return
	}

	// A plain snippet: the tokens are written with no more formatting
	for _, cssVal := range node.Value {
		for _, v := range cssVal.Value {
			writeToken(v, out, cfg)
		}
	}
	writeImportant(node, out, len(node.Value) > 0)
}

func writePropertyValue(node *CSSProperty, out *outputStream, cfg *Config) {
	isJSON := cfg.Options.StylesheetJSON
	var num *CSSToken
	if isJSON {
		num = singleNumeric(node)
	}

	if num != nil && (num.Unit == "" || num.Unit == "px") {
		// In CSS-in-JS a single number is a number of JavaScript
		out.push(jsNumber(num.Number))
		return
	}
	quote := "'"
	if cfg.Options.StylesheetJSONDoubleQuotes {
		quote = `"`
	}
	if isJSON {
		out.push(quote)
	}
	for i, v := range node.Value {
		if i != 0 {
			out.push(", ")
		}
		writeValue(v, out, cfg)
	}
	if isJSON {
		out.push(quote)
	}
}

func writeImportant(node *CSSProperty, out *outputStream, separator bool) {
	if node.Important {
		if separator {
			out.push(" ")
		}
		out.push("!important")
	}
}

func writeValue(value *CSSValue, out *outputStream, cfg *Config) {
	var prev *CSSToken
	for i, tok := range value.Value {
		// A field written close to the token before it (`foo${bar}`) has no separator. As in
		// Emmet, two tokens that have no place in the abbreviation are close
		if i != 0 && (tok.Type != CSSField || !closeTokens(prev, tok)) {
			out.push(" ")
		}
		writeToken(tok, out, cfg)
		prev = tok
	}
}

func writeToken(tok *CSSToken, out *outputStream, cfg *Config) {
	switch tok.Type {
	case CSSColor:
		out.push(cssColor(tok, cfg.Options.StylesheetShortHex))
	case CSSLiteral, CSSCustomProperty:
		out.pushString(tok.Value)
	case CSSNumber:
		out.pushString(frac(tok.Number, 4) + tok.Unit)
	case CSSString:
		quote := "'"
		if !tok.Single {
			quote = `"`
		}
		out.pushString(quote + tok.Value + quote)
	case CSSField:
		out.pushField(tok.Index, tok.Name)
	case CSSFunction:
		out.push(tok.Name + "(")
		for i, arg := range tok.Arguments {
			if i != 0 {
				out.push(", ")
			}
			writeValue(arg, out, cfg)
		}
		out.push(")")
	}
}

// singleNumeric gives the number if it is the only value of the property.
func singleNumeric(node *CSSProperty) *CSSToken {
	if len(node.Value) == 1 {
		if v := node.Value[0]; len(v.Value) == 1 && v.Value[0].Type == CSSNumber {
			return v.Value[0]
		}
	}
	return nil
}

// toCamelCase turns kebab-case into camelCase.
func toCamelCase(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '-' && i+1 < len(s) && isWordChar(s[i+1]) {
			c := s[i+1]
			if c >= 'a' && c <= 'z' {
				c -= 32
			}
			sb.WriteByte(c)
			i++
			continue
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// isWordChar tells if the byte is a letter, a digit or an underscore (\w).
func isWordChar(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// closeTokens tells if the second token starts where the first ends in the abbreviation. Tokens
// with no place are close to each other only (Emmet compares undefined values).
func closeTokens(prev, tok *CSSToken) bool {
	if prev.noPos || tok.noPos {
		return prev.noPos && tok.noPos
	}
	return tok.Start == prev.End
}
