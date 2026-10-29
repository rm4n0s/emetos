package redis

import (
	"strings"

	"github.com/rm4n0s/errors"
)

// SplitArgs splits a command line into its arguments as redis-cli does (sdssplitargs of Redis):
//
//   - arguments are separated by white space;
//   - "double quotes" take the escapes \n \r \t \b \a, \xHH (two hex digits) and \" , and any other \c is c;
//   - 'single quotes' take only \' ;
//   - the closing quote must be followed by white space or the end of the line.
//
// It fails with RedisQuoteUnbalanced when a quote is not closed and with RedisQuoteSpacing when a closing quote is followed by
// something other than white space. A line with no arguments gives an empty list.
func SplitArgs(line string) ([]string, error) {
	var args []string
	i, n := 0, len(line)
	for {
		for i < n && isSpace(line[i]) {
			i++
		}
		if i >= n {
			return args, nil
		}
		var cur []byte
		inDouble, inSingle, done := false, false, false
		for !done {
			switch {
			case inDouble:
				if i >= n {
					return nil, errors.New("RedisQuoteUnbalanced", "the double quote is not closed")
				}
				c := line[i]
				switch {
				case c == '\\' && i+3 < n && line[i+1] == 'x' && isHex(line[i+2]) && isHex(line[i+3]):
					cur = append(cur, hexVal(line[i+2])<<4|hexVal(line[i+3]))
					i += 3
				case c == '\\' && i+1 < n:
					i++
					switch line[i] {
					case 'n':
						cur = append(cur, '\n')
					case 'r':
						cur = append(cur, '\r')
					case 't':
						cur = append(cur, '\t')
					case 'b':
						cur = append(cur, '\b')
					case 'a':
						cur = append(cur, '\a')
					default:
						cur = append(cur, line[i])
					}
				case c == '"':
					if i+1 < n && !isSpace(line[i+1]) {
						return nil, errors.New("RedisQuoteSpacing", "a closing quote is followed by white space or the end of the line")
					}
					done = true
				default:
					cur = append(cur, c)
				}
			case inSingle:
				if i >= n {
					return nil, errors.New("RedisQuoteUnbalanced", "the single quote is not closed")
				}
				c := line[i]
				switch {
				case c == '\\' && i+1 < n && line[i+1] == '\'':
					i++
					cur = append(cur, '\'')
				case c == '\'':
					if i+1 < n && !isSpace(line[i+1]) {
						return nil, errors.New("RedisQuoteSpacing", "a closing quote is followed by white space or the end of the line")
					}
					done = true
				default:
					cur = append(cur, c)
				}
			default:
				if i >= n {
					done = true
					break
				}
				switch c := line[i]; {
				case isSpace(c):
					done = true
				case c == '"':
					inDouble = true
				case c == '\'':
					inSingle = true
				default:
					cur = append(cur, c)
				}
			}
			if i < n {
				i++
			}
		}
		args = append(args, string(cur))
	}
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// QuoteArg writes an argument so that SplitArgs reads it back as it is: bare when it has nothing special, else in double quotes
// with escapes.
func QuoteArg(s string) string {
	plain := s != ""
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= ' ' || c >= 0x7f || c == '"' || c == '\'' || c == '\\' {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		default:
			if c < 0x20 || c == 0x7f {
				b.WriteString(`\x`)
				b.WriteByte("0123456789abcdef"[c>>4])
				b.WriteByte("0123456789abcdef"[c&15])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
