package fuzzy

import "unicode"

// appendRunes appends the runes of s to dst.
func appendRunes(dst []rune, s string) []rune {
	for _, r := range s {
		dst = append(dst, r)
	}
	return dst
}

// lowerRune is the lower-case form used everywhere in the package.
func lowerRune(r rune) rune {
	if r < 0x80 {
		if 'A' <= r && r <= 'Z' {
			return r + 32
		}
		return r
	}
	return unicode.ToLower(r)
}

// appendLower appends the lower-case form of every rune of src to dst.
func appendLower(dst, src []rune) []rune {
	for _, r := range src {
		dst = append(dst, lowerRune(r))
	}
	return dst
}

// jsSpace tells whether r matches \s of a JavaScript regular expression.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return 0x2000 <= r && r <= 0x200a
}
