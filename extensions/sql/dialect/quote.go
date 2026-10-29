package dialect

import "strings"

// quoteWith quotes an identifier between open and close; a close inside it is doubled, and a NUL (no engine takes one) is dropped.
func quoteWith(identifier string, open, close byte) string {
	identifier = strings.ReplaceAll(identifier, "\x00", "")
	var b strings.Builder
	b.WriteByte(open)
	for i := 0; i < len(identifier); i++ {
		if identifier[i] == close {
			b.WriteByte(close)
		}
		b.WriteByte(identifier[i])
	}
	b.WriteByte(close)
	return b.String()
}

// join writes the quoted non-empty parts joined by dots.
func join(d Dialect, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, d.Quote(p))
		}
	}
	return strings.Join(out, ".")
}

// terminate ends a statement with a semicolon unless it has one.
func terminate(s string) string {
	s = strings.TrimRight(s, " \t\r\n")
	if strings.HasSuffix(s, ";") {
		return s + "\n"
	}
	return s + ";\n"
}

// digits keeps the digits of a session id, so that nothing else reaches a statement that is built from it.
func digits(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "0"
	}
	return b.String()
}

// replaceAll is strings.ReplaceAll.
func replaceAll(s, old, replacement string) string { return strings.ReplaceAll(s, old, replacement) }
