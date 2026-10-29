package redis

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ReplyKind is the type of a reply of the server, as RESP2 and RESP3 have them.
type ReplyKind uint8

// The kinds of reply.
const (
	// ReplyNil is a null: RESP2's nil bulk string and nil array, RESP3's _.
	ReplyNil ReplyKind = iota
	// ReplyStatus is a simple string such as OK.
	ReplyStatus
	// ReplyError is an error of the server (ERR ..., WRONGTYPE ...). It is a reply, not a failure of the connection.
	ReplyError
	// ReplyInt is an integer.
	ReplyInt
	// ReplyString is a bulk string (and a RESP3 verbatim string).
	ReplyString
	// ReplyDouble is a RESP3 double.
	ReplyDouble
	// ReplyBool is a RESP3 boolean.
	ReplyBool
	// ReplyBigNumber is a RESP3 big number, kept as its decimal text.
	ReplyBigNumber
	// ReplyArray is an array.
	ReplyArray
	// ReplyMap is a RESP3 map.
	ReplyMap
	// ReplySet is a RESP3 set.
	ReplySet
)

// Reply is a reply of the server, with the types of RESP2 and RESP3, so that the console shows it as redis-cli does. The client
// (go-redis) reads the wire into Go values; ConvertReply turns those into a Reply.
type Reply struct {
	Kind ReplyKind
	// Str is the text of a status, an error, a string and a big number.
	Str string
	// Int is the number of ReplyInt.
	Int int64
	// Float is the number of ReplyDouble.
	Float float64
	// Bool is the value of ReplyBool.
	Bool bool
	// Items are the elements of an array or a set.
	Items []Reply
	// Pairs are the entries of a map.
	Pairs []ReplyPair
}

// ReplyPair is an entry of a RESP3 map.
type ReplyPair struct{ Key, Value Reply }

// The limits of what the console shows of one reply. The client reads the whole reply (the server decides its size); these keep what
// the screen and the memory hold small (SPEC §14).
const (
	// MaxReplyItems is how many elements of one array, set or map are shown.
	MaxReplyItems = 1000
	// MaxReplyString is how many bytes of one string are shown.
	MaxReplyString = 64 << 10
	// MaxReplyLines is how many lines one reply makes at most.
	MaxReplyLines = 5000
)

// statusWords are the simple strings the client cannot tell from a bulk string; the console shows them without quotes.
var statusWords = map[string]bool{"OK": true, "PONG": true, "QUEUED": true}

// setCommands are the commands that answer with a set in RESP3 (go-redis reads a set as an array).
var setCommands = map[string]bool{"SMEMBERS": true, "SINTER": true, "SUNION": true, "SDIFF": true, "SPOP": true}

// ConvertReply turns a value read by go-redis into a Reply. args is the command, resp3 says that the connection speaks RESP3: go-redis
// returns a RESP3 set as an array, so the commands that answer with a set (SMEMBERS, SINTER, SUNION, SDIFF, SPOP) mark their array as a
// set on a RESP3 connection, and a RESP3 map (a Go map, whose order is lost) is sorted by the text of its keys.
func ConvertReply(args []string, v any, resp3 bool) Reply {
	r := convertValue(v)
	if resp3 && r.Kind == ReplyArray && len(args) > 0 && setCommands[strings.ToUpper(args[0])] {
		r.Kind = ReplySet
	}
	return r
}

// convertValue converts the Go value of one reply, and the values inside it.
func convertValue(v any) Reply {
	switch x := v.(type) {
	case nil:
		return Reply{Kind: ReplyNil}
	case string:
		if statusWords[x] {
			return Reply{Kind: ReplyStatus, Str: x}
		}
		return Reply{Kind: ReplyString, Str: x}
	case []byte:
		return Reply{Kind: ReplyString, Str: string(x)}
	case int64:
		return Reply{Kind: ReplyInt, Int: x}
	case int:
		return Reply{Kind: ReplyInt, Int: int64(x)}
	case float64:
		return Reply{Kind: ReplyDouble, Float: x}
	case bool:
		return Reply{Kind: ReplyBool, Bool: x}
	case *big.Int:
		if x == nil {
			return Reply{Kind: ReplyNil}
		}
		return Reply{Kind: ReplyBigNumber, Str: x.String()}
	case []any:
		items := make([]Reply, len(x))
		for i, e := range x {
			items[i] = convertValue(e)
		}
		return Reply{Kind: ReplyArray, Items: items}
	case map[any]any:
		pairs := make([]ReplyPair, 0, len(x))
		for k, e := range x {
			pairs = append(pairs, ReplyPair{Key: convertValue(k), Value: convertValue(e)})
		}
		sort.SliceStable(pairs, func(i, j int) bool { return plainText(pairs[i].Key) < plainText(pairs[j].Key) })
		return Reply{Kind: ReplyMap, Pairs: pairs}
	case error:
		// an error inside an array (the reply of EXEC, or of a script)
		return Reply{Kind: ReplyError, Str: x.Error()}
	default:
		return Reply{Kind: ReplyString, Str: fmt.Sprint(x)}
	}
}

// plainText is the text of a scalar reply, for sorting the keys of a map and for the tables of the server information.
func plainText(r Reply) string {
	switch r.Kind {
	case ReplyNil:
		return ""
	case ReplyInt:
		return strconv.FormatInt(r.Int, 10)
	case ReplyDouble:
		return formatDouble(r.Float)
	case ReplyBool:
		return strconv.FormatBool(r.Bool)
	case ReplyArray, ReplySet, ReplyMap:
		return strings.Join(FormatReply(r), " ")
	}
	return r.Str
}

// formatDouble writes a RESP3 double the way redis-cli does.
func formatDouble(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	return strconv.FormatFloat(f, 'g', 17, 64)
}

// IsError tells whether the reply is an error of the server.
func (r Reply) IsError() bool { return r.Kind == ReplyError }

// FormatReply writes a reply as redis-cli does in a terminal: one line for a scalar, a numbered list for an array (1) ...), a list of
// 1# key => value entries for a map and 1~ items for a set, nested replies indented under their number, "(nil)", "(integer) 5",
// "(error) ERR ...", "(empty array)". A string is quoted with the escapes of redis-cli, except that valid UTF-8 text is kept as it is;
// a status such as OK is bare. A string of several lines that is the whole reply (INFO, CLIENT LIST) is shown as it is, line by line,
// as redis-cli does when its output is not a terminal. A reply is cut at MaxReplyItems elements of a list, MaxReplyString bytes of a
// string and MaxReplyLines lines, with a line that says how much was left out.
func FormatReply(r Reply) []string {
	if r.Kind == ReplyString && strings.ContainsAny(r.Str, "\r\n") && utf8.ValidString(r.Str) {
		return formatText(r.Str)
	}
	lines := formatValue(r)
	if len(lines) > MaxReplyLines {
		more := len(lines) - MaxReplyLines
		lines = append(lines[:MaxReplyLines], "... "+plural(int64(more), "more line", "more lines"))
	}
	return lines
}

// formatText splits a long text into lines, cutting it like a string.
func formatText(s string) []string {
	cut := 0
	if len(s) > MaxReplyString {
		cut = len(s) - MaxReplyString
		s = strings.ToValidUTF8(s[:MaxReplyString], "")
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > MaxReplyLines {
		more := len(lines) - MaxReplyLines
		lines = append(lines[:MaxReplyLines], "... "+plural(int64(more), "more line", "more lines"))
	} else if cut > 0 {
		lines = append(lines, "... "+plural(int64(cut), "more byte", "more bytes"))
	}
	return lines
}

// formatValue writes the lines of one reply.
func formatValue(r Reply) []string {
	switch r.Kind {
	case ReplyNil:
		return []string{"(nil)"}
	case ReplyStatus:
		return []string{r.Str}
	case ReplyError:
		return []string{"(error) " + r.Str}
	case ReplyInt:
		return []string{"(integer) " + strconv.FormatInt(r.Int, 10)}
	case ReplyString:
		return []string{quoteReply(r.Str)}
	case ReplyDouble:
		return []string{"(double) " + formatDouble(r.Float)}
	case ReplyBool:
		return []string{"(" + strconv.FormatBool(r.Bool) + ")"}
	case ReplyBigNumber:
		return []string{"(big number) " + r.Str}
	case ReplyArray:
		if len(r.Items) == 0 {
			return []string{"(empty array)"}
		}
		return formatList(r.Items, ") ")
	case ReplySet:
		if len(r.Items) == 0 {
			return []string{"(empty set)"}
		}
		return formatList(r.Items, "~ ")
	case ReplyMap:
		if len(r.Pairs) == 0 {
			return []string{"(empty hash)"}
		}
		return formatEntries(r.Pairs)
	}
	return []string{"(unknown)"}
}

// list writes the elements of an array or a set under their numbers.
func formatList(items []Reply, sep string) []string {
	shown := items
	if len(shown) > MaxReplyItems {
		shown = shown[:MaxReplyItems]
	}
	width := len(strconv.Itoa(len(shown)))
	var out []string
	for i, it := range shown {
		label := fmt.Sprintf("%*d%s", width, i+1, sep)
		out = append(out, indentLines(label, formatValue(it))...)
		if len(out) > MaxReplyLines {
			return out
		}
	}
	if len(items) > len(shown) {
		out = append(out, "... "+plural(int64(len(items)-len(shown)), "more element", "more elements"))
	}
	return out
}

// entries writes the entries of a map.
func formatEntries(pairs []ReplyPair) []string {
	shown := pairs
	if len(shown) > MaxReplyItems {
		shown = shown[:MaxReplyItems]
	}
	width := len(strconv.Itoa(len(shown)))
	var out []string
	for i, p := range shown {
		label := fmt.Sprintf("%*d# ", width, i+1)
		key := formatValue(p.Key)
		val := formatValue(p.Value)
		keyLine := strings.Join(key, " ")
		out = append(out, indentLines(label, indentLinesAfter(keyLine+" => ", val))...)
		if len(out) > MaxReplyLines {
			return out
		}
	}
	if len(pairs) > len(shown) {
		out = append(out, "... "+plural(int64(len(pairs)-len(shown)), "more entry", "more entries"))
	}
	return out
}

// indentLines puts label before the first line and spaces of its width before the others.
func indentLines(label string, lines []string) []string {
	out := make([]string, len(lines))
	pad := strings.Repeat(" ", len(label))
	for i, l := range lines {
		if i == 0 {
			out[i] = label + l
		} else {
			out[i] = pad + l
		}
	}
	return out
}

// indentLinesAfter is indentLines for the text after the key of a map entry.
func indentLinesAfter(prefix string, lines []string) []string {
	if len(lines) == 0 {
		return []string{strings.TrimRight(prefix, " ")}
	}
	return indentLines(prefix, lines)
}

// quoteReply writes a string reply in double quotes with the escapes of redis-cli (\" \\ \n \r \t \a \b, \xNN for the other control
// characters and for bytes that are not UTF-8). Valid UTF-8 characters are kept: redis-cli shows their bytes as \xNN. A string longer
// than MaxReplyString is cut, with a note after the closing quote.
func quoteReply(s string) string {
	note := ""
	if len(s) > MaxReplyString {
		note = " ... (" + plural(int64(len(s)-MaxReplyString), "more byte", "more bytes") + ")"
		s = s[:MaxReplyString]
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			i++
			switch c {
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
					fmt.Fprintf(&b, `\x%02x`, c)
				} else {
					b.WriteByte(c)
				}
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 || !unicode.IsPrint(r) {
			for j := 0; j < size; j++ {
				fmt.Fprintf(&b, `\x%02x`, s[i+j])
			}
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
	return b.String() + note
}
