package playground

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Quote writes a string as a literal in double quotes that Parse reads back as the same string.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\v':
			b.WriteString(`\v`)
		default:
			if r < ' ' || r == 0x7f || r == utf8.RuneError || r == 0x2028 || r == 0x2029 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// isBareKey tells whether a key of a document can be written without quotes.
func isBareKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		letter := r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// Format writes a value on one line as the literal Parse reads, so that a statement can be made from values: documents as
// { key: value }, the BSON types with the helpers of the shell. A float64 that is a whole number in the range of int32 is written
// as that number and reads back as an int32, as in mongosh.
func Format(v any) string {
	var b strings.Builder
	formatValue(&b, v)
	return b.String()
}

func formatValue(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		b.WriteString(Quote(x))
	case int32:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString("NumberLong(" + Quote(strconv.FormatInt(x, 10)) + ")")
	case float64:
		switch {
		case math.IsNaN(x):
			b.WriteString("NaN")
		case math.IsInf(x, 1):
			b.WriteString("Infinity")
		case math.IsInf(x, -1):
			b.WriteString("-Infinity")
		default:
			if a := math.Abs(x); a >= 1e21 || (a < 1e-6 && a != 0) {
				b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
			} else {
				b.WriteString(strconv.FormatFloat(x, 'f', -1, 64))
			}
		}
	case bson.D:
		if len(x) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{ ")
		for i, e := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			if isBareKey(e.Key) {
				b.WriteString(e.Key)
			} else {
				b.WriteString(Quote(e.Key))
			}
			b.WriteString(": ")
			formatValue(b, e.Value)
		}
		b.WriteString(" }")
	case bson.A:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			formatValue(b, e)
		}
		b.WriteByte(']')
	case bson.ObjectID:
		b.WriteString("ObjectId(" + Quote(x.Hex()) + ")")
	case bson.DateTime:
		b.WriteString("ISODate(" + Quote(x.Time().UTC().Format("2006-01-02T15:04:05.000Z")) + ")")
	case bson.Decimal128:
		b.WriteString("NumberDecimal(" + Quote(x.String()) + ")")
	case bson.Binary:
		if x.Subtype == bson.TypeBinaryUUID && len(x.Data) == 16 {
			h := make([]byte, 0, 36)
			const digits = "0123456789abcdef"
			for i, c := range x.Data {
				if i == 4 || i == 6 || i == 8 || i == 10 {
					h = append(h, '-')
				}
				h = append(h, digits[c>>4], digits[c&15])
			}
			b.WriteString("UUID(" + Quote(string(h)) + ")")
			return
		}
		b.WriteString("BinData(" + strconv.Itoa(int(x.Subtype)) + ", " + Quote(base64.StdEncoding.EncodeToString(x.Data)) + ")")
	case bson.Timestamp:
		b.WriteString("Timestamp(" + strconv.FormatUint(uint64(x.T), 10) + ", " + strconv.FormatUint(uint64(x.I), 10) + ")")
	case bson.MinKey:
		b.WriteString("MinKey()")
	case bson.MaxKey:
		b.WriteString("MaxKey()")
	case bson.Regex:
		b.WriteByte('/')
		escaped := false
		for _, r := range x.Pattern {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '/':
				b.WriteByte('\\')
			case r == '\n':
				b.WriteString(`\n`)
				continue
			}
			b.WriteRune(r)
		}
		b.WriteString("/" + x.Options)
	default:
		b.WriteString(Quote("<" + typeName(v) + ">"))
	}
}

// typeName names the type of a value that Format does not know.
func typeName(v any) string { return fmt.Sprintf("%T", v) }
