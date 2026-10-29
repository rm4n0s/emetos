package resultgrid

import (
	"bytes"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Kind is the kind of a Value.
type Kind uint8

// The kinds of value.
const (
	// KindNull is NULL (SQL), null (a document), nil.
	KindNull Kind = iota
	// KindText is text.
	KindText
	// KindInt is a whole number.
	KindInt
	// KindFloat is a number with a fraction.
	KindFloat
	// KindBool is true or false.
	KindBool
	// KindBytes is binary data; it is shown as hex.
	KindBytes
	// KindTime is a date or a time; it is shown in ISO 8601.
	KindTime
	// KindJSON is text that is a JSON document; it is shown as text and exported as JSON.
	KindJSON
)

// Value is the content of a cell.
type Value struct {
	Kind Kind
	// S is the text of KindText and KindJSON.
	S string
	// I is the number of KindInt, and 0 or 1 for KindBool.
	I int64
	// F is the number of KindFloat.
	F float64
	// B is the data of KindBytes.
	B []byte
	// T is the time of KindTime.
	T time.Time
}

// Constructors of the values.

// Null is NULL.
func Null() Value { return Value{} }

// Text is a text value.
func Text(s string) Value { return Value{Kind: KindText, S: s} }

// JSON is a text value that holds a JSON document.
func JSON(s string) Value { return Value{Kind: KindJSON, S: s} }

// Int is a whole number.
func Int(n int64) Value { return Value{Kind: KindInt, I: n} }

// Float is a number with a fraction.
func Float(f float64) Value { return Value{Kind: KindFloat, F: f} }

// Bool is true or false.
func Bool(b bool) Value {
	v := Value{Kind: KindBool}
	if b {
		v.I = 1
	}
	return v
}

// Bytes is binary data.
func Bytes(b []byte) Value { return Value{Kind: KindBytes, B: b} }

// Time is a date or a time.
func Time(t time.Time) Value { return Value{Kind: KindTime, T: t} }

// IsNull tells whether the value is NULL.
func (v Value) IsNull() bool { return v.Kind == KindNull }

// IsNumber tells whether the value is shown at the right of its cell: a whole number or a fraction.
func (v Value) IsNumber() bool { return v.Kind == KindInt || v.Kind == KindFloat }

// DisplayOptions say how a value is written in a cell.
type DisplayOptions struct {
	// NullText is what NULL looks like; "NULL" when empty.
	NullText string
	// MaxRunes cuts a text after that many characters with "…"; 0 means DefaultMaxRunes.
	MaxRunes int
}

// The limits of the text of a cell.
const (
	// DefaultMaxRunes is where a long text is cut in a cell.
	DefaultMaxRunes = 200
	// MaxTipRunes is how much of a long value its hover shows; the whole of it is copied.
	MaxTipRunes = 2000
	// hexBytesShown is how many bytes of binary data a cell shows.
	hexBytesShown = 16
)

// Full is the whole text of a value, as a copy gives it: a text as it is, a number in its shortest form, binary data as "0x" and
// all of its hex, a time in ISO 8601 (RFC 3339 with the fraction it has), and NULL as nullText.
func (v Value) Full(nullText string) string {
	if nullText == "" {
		nullText = "NULL"
	}
	switch v.Kind {
	case KindNull:
		return nullText
	case KindText, KindJSON:
		return v.S
	case KindInt:
		return strconv.FormatInt(v.I, 10)
	case KindFloat:
		return formatFloat(v.F)
	case KindBool:
		if v.I != 0 {
			return "true"
		}
		return "false"
	case KindBytes:
		return "0x" + hex.EncodeToString(v.B)
	case KindTime:
		return v.T.Format(time.RFC3339Nano)
	}
	return ""
}

// formatFloat writes a float in its shortest exact form; NaN and the infinities as the words.
func formatFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "+Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// Display is the text of a value in a cell, on one line: line breaks and other control characters are shown as spaces, a long text is
// cut with "…", and binary data shows its first bytes in hex and its size. cut tells that the text shown is not the whole value
// (the cell then has a hover that shows more).
func (v Value) Display(o DisplayOptions) (text string, cut bool) {
	if o.MaxRunes <= 0 {
		o.MaxRunes = DefaultMaxRunes
	}
	switch v.Kind {
	case KindNull:
		if o.NullText == "" {
			return "NULL", false
		}
		return o.NullText, false
	case KindBytes:
		n := min(len(v.B), hexBytesShown)
		s := "0x" + hex.EncodeToString(v.B[:n])
		if len(v.B) > n {
			return s + "… (" + sizeText(len(v.B)) + ")", true
		}
		return s, false
	case KindText, KindJSON:
		return oneLine(v.S, o.MaxRunes)
	}
	return oneLine(v.Full(o.NullText), o.MaxRunes)
}

// oneLine shows s on one line and cuts it after max characters; cut says that what it returns is not the whole of s.
func oneLine(s string, maxRunes int) (text string, cut bool) {
	// a quick path: a short text with nothing to change
	if len(s) <= maxRunes && !needsFlattening(s) {
		return s, false
	}
	var b strings.Builder
	n := 0
	changed, truncated := false, false
	for _, r := range s {
		if n == maxRunes {
			b.WriteString("…")
			truncated = true
			break
		}
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			r, changed = ' ', true
		}
		b.WriteRune(r)
		n++
	}
	return b.String(), truncated || changed
}

// needsFlattening tells whether the text has a character that the cell shows as a space.
func needsFlattening(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return true
		}
	}
	return false
}

// sizeText writes a size of binary data: "37 bytes", "1.2 KB".
func sizeText(n int) string {
	switch {
	case n < 1024:
		return strconv.Itoa(n) + " bytes"
	case n < 1024*1024:
		return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " KB"
	}
	return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + " MB"
}

// Tip is the text of the hover of a value: its whole text up to MaxTipRunes characters (line breaks kept), with a note when there
// is more.
func (v Value) Tip(nullText string) string {
	full := v.Full(nullText)
	if utf8.RuneCountInString(full) <= MaxTipRunes {
		return full
	}
	runes := []rune(full)
	return string(runes[:MaxTipRunes]) + "…\n(" + strconv.Itoa(len(runes)) + " characters; copy gives all of them)"
}

// Compare orders two values for the sort of a column: NULL first; numbers (whole and fractions) by their value; text without
// regard to case, then by its bytes; binary data by its bytes; times by their instant; false before true; values of different
// kinds by kind. It returns -1, 0 or 1.
func Compare(a, b Value) int {
	if a.Kind == KindNull || b.Kind == KindNull {
		if a.Kind == b.Kind {
			return 0
		}
		if a.Kind == KindNull {
			return -1
		}
		return 1
	}
	if a.IsNumber() && b.IsNumber() {
		return compareNumbers(a, b)
	}
	if ra, rb := rank(a.Kind), rank(b.Kind); ra != rb {
		return cmpInt(int64(ra), int64(rb))
	}
	switch a.Kind {
	case KindText, KindJSON:
		return compareFold(a.S, b.S)
	case KindBool:
		return cmpInt(a.I, b.I)
	case KindBytes:
		return bytes.Compare(a.B, b.B)
	case KindTime:
		switch {
		case a.T.Before(b.T):
			return -1
		case a.T.After(b.T):
			return 1
		}
	}
	return 0
}

// rank orders the kinds that cannot be compared with each other.
func rank(k Kind) int {
	switch k {
	case KindBool:
		return 1
	case KindInt, KindFloat:
		return 2
	case KindTime:
		return 3
	case KindText, KindJSON:
		return 4
	case KindBytes:
		return 5
	}
	return 0
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareNumbers compares a whole number or a fraction with another; whole numbers are compared as such, so that no digit is lost
// to a float. NaN sorts before every number.
func compareNumbers(a, b Value) int {
	if a.Kind == KindInt && b.Kind == KindInt {
		return cmpInt(a.I, b.I)
	}
	fa, fb := toFloat(a), toFloat(b)
	switch {
	case math.IsNaN(fa) && math.IsNaN(fb):
		return 0
	case math.IsNaN(fa):
		return -1
	case math.IsNaN(fb):
		return 1
	case fa < fb:
		return -1
	case fa > fb:
		return 1
	}
	return 0
}

func toFloat(v Value) float64 {
	if v.Kind == KindInt {
		return float64(v.I)
	}
	return v.F
}

// compareFold compares two texts without regard to case; texts that differ only in case are ordered by their bytes, so that the
// order is total.
func compareFold(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ra, na := utf8.DecodeRuneInString(a[i:])
		rb, nb := utf8.DecodeRuneInString(b[j:])
		if ra != rb {
			fa, fb := unicode.ToLower(ra), unicode.ToLower(rb)
			if fa != fb {
				if fa < fb {
					return -1
				}
				return 1
			}
		}
		i, j = i+na, j+nb
	}
	switch {
	case i < len(a):
		return 1
	case j < len(b):
		return -1
	}
	return strings.Compare(a, b)
}
