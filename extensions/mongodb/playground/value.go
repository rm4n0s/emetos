package playground

import (
	"encoding/base64"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// expect takes the punctuation text, or fails; what says what was wanted in the message.
func (p *parser) expect(text, what string) (token, error) {
	t := p.peek()
	switch {
	case t.kind == tkPunct && t.text == text:
		return p.next(), nil
	case t.kind == tkError:
		return token{}, t.err
	case t.kind == tkEOF:
		return token{}, errors.New("MongoParseUnexpectedEnd", "the text ends where "+what+" is expected", append(p.metaFound(t), "expected", text)...)
	}
	return token{}, errors.New("MongoParseExpectedToken", "expected "+what+", found "+describe(t), append(p.metaFound(t), "expected", text, "found", describe(t))...)
}

// value reads a relaxed JavaScript literal or a call of one of the helpers of the shell.
func (p *parser) value() (any, error) {
	t := p.peek()
	switch t.kind {
	case tkEOF:
		return nil, errors.New("MongoParseUnexpectedEnd", "the text ends where a value is expected", p.meta(t)...)
	case tkError:
		return nil, t.err
	case tkString:
		p.next()
		return t.str, nil
	case tkNumber:
		p.next()
		return p.number(t, false)
	case tkIdent:
		return p.identValue(t)
	}
	switch t.text {
	case "{":
		return p.object()
	case "[":
		return p.array()
	case "-", "+":
		p.next()
		n := p.peek()
		switch {
		case n.kind == tkNumber:
			p.next()
			return p.number(n, t.text == "-")
		case n.kind == tkIdent && n.text == "Infinity":
			p.next()
			if t.text == "-" {
				return math.Inf(-1), nil
			}
			return math.Inf(1), nil
		case n.kind == tkError:
			return nil, n.err
		}
		return nil, errors.New("MongoParseExpectedToken", "a sign goes before a number", append(p.meta(t), "found", describe(n))...)
	case "/":
		p.hasBuf = false
		p.lex.pos = t.start
		r := p.lex.regex(t.start, t.nl)
		if r.kind == tkError {
			return nil, r.err
		}
		p.prevEnd = r.end
		p.taken++
		return bson.Regex{Pattern: r.str, Options: sortFlags(r.text)}, nil
	case "`":
		return nil, errors.New("MongoParseUnsupported", "template strings are not supported; use a string in quotes", p.meta(t)...)
	}
	return nil, errors.New("MongoParseExpectedToken", "expected a value, found "+describe(t), append(p.meta(t), "found", describe(t))...)
}

// sortFlags puts the flags of a regular expression in the order BSON writes them.
func sortFlags(flags string) string {
	var b strings.Builder
	for _, f := range "imsxu" {
		if strings.ContainsRune(flags, f) {
			b.WriteRune(f)
		}
	}
	return b.String()
}

// number reads a number literal as the value mongosh makes of it: an int32 for a whole number in the range of int32, else a float64.
func (p *parser) number(t token, negative bool) (any, error) {
	var f float64
	if strings.HasPrefix(t.text, "0x") || strings.HasPrefix(t.text, "0X") {
		u, err := strconv.ParseUint(t.text[2:], 16, 64)
		if err != nil {
			return nil, errors.New("MongoParseBadLiteral", "this hexadecimal number is too large", append(p.meta(t), "text", t.text)...)
		}
		f = float64(u)
	} else {
		var err error
		f, err = strconv.ParseFloat(t.text, 64)
		if err != nil || math.IsInf(f, 0) {
			return nil, errors.New("MongoParseBadLiteral", "this number is too large", append(p.meta(t), "text", t.text)...)
		}
	}
	if negative {
		f = -f
	}
	if f == math.Trunc(f) && f >= math.MinInt32 && f <= math.MaxInt32 {
		return int32(f), nil
	}
	return f, nil
}

// object reads {key: value, ...}.
func (p *parser) object() (any, error) {
	open, err := p.expect("{", "{")
	if err != nil {
		return nil, err
	}
	if p.nest++; p.nest > maxDepth {
		return nil, errors.New("MongoParseTooDeep", "documents and arrays are nested too deeply", append(p.meta(open), "limit", maxDepth)...)
	}
	defer func() { p.nest-- }()
	doc := bson.D{}
	for {
		t := p.peek()
		if t.kind == tkPunct && t.text == "}" {
			p.next()
			return doc, nil
		}
		var key string
		switch {
		case t.kind == tkIdent:
			key = t.text
		case t.kind == tkString:
			key = t.str
		case t.kind == tkNumber && isDecimalInteger(t.text):
			key = strings.TrimLeft(t.text, "0")
			if key == "" {
				key = "0"
			}
		case t.kind == tkError:
			return nil, t.err
		case t.kind == tkEOF:
			return nil, errors.New("MongoParseUnexpectedEnd", "the text ends inside a document", p.meta(open)...)
		default:
			return nil, errors.New("MongoParseBadKey", "a key of a document is a name, a string or a whole number, found "+describe(t), append(p.meta(t), "found", describe(t))...)
		}
		p.next()
		if _, err := p.expect(":", "a colon after the key \""+key+"\""); err != nil {
			return nil, err
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		replaced := false
		for i := range doc {
			if doc[i].Key == key {
				doc[i].Value, replaced = v, true // the later value of a key wins, as in JavaScript
				break
			}
		}
		if !replaced {
			doc = append(doc, bson.E{Key: key, Value: v})
		}
		n := p.peek()
		switch {
		case n.kind == tkPunct && n.text == ",":
			p.next()
		case n.kind == tkPunct && n.text == "}":
		case n.kind == tkError:
			return nil, n.err
		case n.kind == tkEOF:
			return nil, errors.New("MongoParseUnexpectedEnd", "the text ends inside a document", p.meta(open)...)
		default:
			return nil, errors.New("MongoParseExpectedToken", "expected a comma or } after the value of \""+key+"\", found "+describe(n), append(p.meta(n), "found", describe(n))...)
		}
	}
}

// isDecimalInteger tells whether the text is only digits.
func isDecimalInteger(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// array reads [value, ...].
func (p *parser) array() (any, error) {
	open, err := p.expect("[", "[")
	if err != nil {
		return nil, err
	}
	if p.nest++; p.nest > maxDepth {
		return nil, errors.New("MongoParseTooDeep", "documents and arrays are nested too deeply", append(p.meta(open), "limit", maxDepth)...)
	}
	defer func() { p.nest-- }()
	arr := bson.A{}
	for {
		t := p.peek()
		if t.kind == tkPunct && t.text == "]" {
			p.next()
			return arr, nil
		}
		if t.kind == tkPunct && t.text == "," {
			return nil, errors.New("MongoParseUnsupported", "an array with a gap ([1,,2]) is not supported", p.meta(t)...)
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		n := p.peek()
		switch {
		case n.kind == tkPunct && n.text == ",":
			p.next()
		case n.kind == tkPunct && n.text == "]":
		case n.kind == tkError:
			return nil, n.err
		case n.kind == tkEOF:
			return nil, errors.New("MongoParseUnexpectedEnd", "the text ends inside an array", p.meta(open)...)
		default:
			return nil, errors.New("MongoParseExpectedToken", "expected a comma or ] after the item, found "+describe(n), append(p.meta(n), "found", describe(n))...)
		}
	}
}

// unsupportedNames are the words of JavaScript that a playground has no use for, and the reason to say.
var unsupportedNames = map[string]string{
	"undefined": "undefined is not supported; leave the key out or use null",
	"var":       "variables are not supported", "let": "variables are not supported", "const": "variables are not supported",
	"function": "functions are not supported", "return": "functions are not supported",
	"if": "conditions are not supported", "else": "conditions are not supported", "for": "loops are not supported",
	"while": "loops are not supported", "do": "loops are not supported", "async": "functions are not supported",
	"await": "await is not supported", "this": "this is not supported", "typeof": "operators are not supported",
	"delete": "operators are not supported", "void": "operators are not supported", "class": "classes are not supported",
	"import": "import is not supported", "require": "require is not supported", "console": "console is not supported", "print": "print is not supported",
}

// identValue reads a value that starts with a name: true, false, null, NaN, Infinity, or a helper call.
func (p *parser) identValue(t token) (any, error) {
	switch t.text {
	case "true":
		p.next()
		return true, nil
	case "false":
		p.next()
		return false, nil
	case "null":
		p.next()
		return nil, nil
	case "NaN":
		p.next()
		return math.NaN(), nil
	case "Infinity":
		p.next()
		return math.Inf(1), nil
	case "new":
		p.next()
		n := p.peek()
		if n.kind != tkIdent {
			return nil, errors.New("MongoParseExpectedToken", "new is followed by the name of a helper (new Date(...))", append(p.meta(n), "found", describe(n))...)
		}
		p.next()
		return p.helper(n, true)
	}
	if reason, ok := unsupportedNames[t.text]; ok {
		return nil, errors.New("MongoParseUnsupported", reason, append(p.meta(t), "name", t.text)...)
	}
	p.next()
	return p.helper(t, false)
}

// helperArity is how many arguments each helper takes (the lowest and the highest).
var helperArity = map[string][2]int{
	"ObjectId": {0, 1}, "ISODate": {0, 1}, "Date": {0, 1}, "NumberLong": {1, 1}, "NumberInt": {1, 1}, "NumberDecimal": {1, 1},
	"UUID": {0, 1}, "BinData": {2, 2}, "Timestamp": {1, 2}, "MinKey": {0, 0}, "MaxKey": {0, 0},
}

// helper reads the call of the helper named by name, whose name was taken already; isNew says that "new" came before it.
func (p *parser) helper(name token, isNew bool) (any, error) {
	arity, known := helperArity[name.text]
	if !known {
		if n := p.peek(); n.kind == tkPunct && n.text == "(" {
			return nil, errors.New("MongoParseUnknownHelper", name.text+" is not one of the helpers a playground has (ObjectId, ISODate, Date, NumberLong, NumberInt, NumberDecimal, UUID, BinData, Timestamp, MinKey, MaxKey)", append(p.meta(name), "name", name.text)...)
		}
		return nil, errors.New("MongoParseUnsupported", name.text+" is not a value: variables and functions are not supported", append(p.meta(name), "name", name.text)...)
	}
	if name.text == "Date" && !isNew {
		return nil, errors.New("MongoParseUnsupported", "Date(...) without new gives a string; write new Date(...) or ISODate(...)", append(p.meta(name), "name", name.text)...)
	}
	var args []any
	end := name.end
	if n := p.peek(); n.kind == tkPunct && n.text == "(" {
		var err error
		args, _, err = p.args()
		if err != nil {
			return nil, err
		}
		end = p.prevEnd
	} else if name.text != "MinKey" && name.text != "MaxKey" {
		return nil, errors.New("MongoParseExpectedToken", name.text+" is called with ( ... )", append(p.meta(name), "found", describe(n))...)
	}
	whole := p.metaSpan(name.start, end)
	if len(args) < arity[0] || len(args) > arity[1] {
		return nil, errors.New("MongoParseBadArguments", name.text+" takes "+arityText(arity)+" argument(s), not "+strconv.Itoa(len(args)), append(whole, "helper", name.text)...)
	}
	switch name.text {
	case "ObjectId":
		if len(args) == 0 {
			return p.opts.NewObjectID(), nil
		}
		s, ok := args[0].(string)
		id, err := bson.ObjectIDFromHex(s)
		if !ok || err != nil {
			return nil, errors.New("MongoParseBadLiteral", "ObjectId takes a string of 24 hexadecimal digits", append(whole, "helper", name.text)...)
		}
		return id, nil
	case "ISODate", "Date":
		if len(args) == 0 {
			return bson.NewDateTimeFromTime(p.opts.Now()), nil
		}
		switch a := args[0].(type) {
		case string:
			if tm, ok := parseDate(a); ok {
				return bson.NewDateTimeFromTime(tm), nil
			}
			return nil, errors.New("MongoParseBadLiteral", "this is not a date; write it as \"2024-01-31\" or \"2024-01-31T10:30:00Z\"", append(whole, "helper", name.text, "value", a)...)
		case int32:
			if name.text == "Date" {
				return bson.DateTime(a), nil
			}
		case float64:
			if name.text == "Date" && a == math.Trunc(a) && math.Abs(a) < 1e15 {
				return bson.DateTime(int64(a)), nil
			}
		}
		if name.text == "Date" {
			return nil, errors.New("MongoParseBadLiteral", "Date takes a date string or a number of milliseconds", append(whole, "helper", name.text)...)
		}
		return nil, errors.New("MongoParseBadLiteral", "ISODate takes a date string", append(whole, "helper", name.text)...)
	case "NumberLong":
		n, ok := wholeNumber(args[0], math.MinInt64, math.MaxInt64)
		if !ok {
			return nil, errors.New("MongoParseBadLiteral", "NumberLong takes a whole number, or a string of digits for a large one", append(whole, "helper", name.text)...)
		}
		return n, nil
	case "NumberInt":
		n, ok := wholeNumber(args[0], math.MinInt32, math.MaxInt32)
		if !ok {
			return nil, errors.New("MongoParseBadLiteral", "NumberInt takes a whole number in the range of 32 bits", append(whole, "helper", name.text)...)
		}
		return int32(n), nil
	case "NumberDecimal":
		var text string
		switch a := args[0].(type) {
		case string:
			text = a
		case int32:
			text = strconv.FormatInt(int64(a), 10)
		case float64:
			text = strconv.FormatFloat(a, 'g', -1, 64)
		}
		d, err := bson.ParseDecimal128(text)
		if err != nil {
			return nil, errors.New("MongoParseBadLiteral", "NumberDecimal takes a decimal number, as a string (\"1.25\")", append(whole, "helper", name.text)...)
		}
		return d, nil
	case "UUID":
		if len(args) == 0 {
			u := p.opts.NewUUID()
			return bson.Binary{Subtype: bson.TypeBinaryUUID, Data: u[:]}, nil
		}
		s, _ := args[0].(string)
		raw, ok := decodeUUID(s)
		if !ok {
			return nil, errors.New("MongoParseBadLiteral", "UUID takes a string of 32 hexadecimal digits, with or without the dashes", append(whole, "helper", name.text)...)
		}
		return bson.Binary{Subtype: bson.TypeBinaryUUID, Data: raw}, nil
	case "BinData":
		sub, ok := wholeNumber(args[0], 0, 255)
		s, isString := args[1].(string)
		data, err := base64.StdEncoding.DecodeString(s)
		if !ok || !isString || err != nil {
			return nil, errors.New("MongoParseBadLiteral", "BinData takes a subtype from 0 to 255 and a string in base64", append(whole, "helper", name.text)...)
		}
		return bson.Binary{Subtype: byte(sub), Data: data}, nil
	case "Timestamp":
		var t, i int64
		var ok bool
		if len(args) == 1 {
			doc, isDoc := args[0].(bson.D)
			ok = isDoc && len(doc) == 2 && doc[0].Key == "t" && doc[1].Key == "i"
			if ok {
				var ok1, ok2 bool
				t, ok1 = wholeNumber(doc[0].Value, 0, math.MaxUint32)
				i, ok2 = wholeNumber(doc[1].Value, 0, math.MaxUint32)
				ok = ok1 && ok2
			}
		} else {
			var ok1, ok2 bool
			t, ok1 = wholeNumber(args[0], 0, math.MaxUint32)
			i, ok2 = wholeNumber(args[1], 0, math.MaxUint32)
			ok = ok1 && ok2
		}
		if !ok {
			return nil, errors.New("MongoParseBadLiteral", "Timestamp takes the seconds and the counter (Timestamp(1700000000, 1)), each a whole number from 0 to 4294967295", append(whole, "helper", name.text)...)
		}
		return bson.Timestamp{T: uint32(t), I: uint32(i)}, nil
	case "MinKey":
		return bson.MinKey{}, nil
	default: // MaxKey: helperArity lists no other name
		return bson.MaxKey{}, nil
	}
}

// arityText writes how many arguments a helper takes.
func arityText(a [2]int) string {
	if a[0] == a[1] {
		return strconv.Itoa(a[0])
	}
	return strconv.Itoa(a[0]) + " to " + strconv.Itoa(a[1])
}

// wholeNumber reads a whole number from the value of an argument (an int32, a float64 without a fraction, or a string of digits)
// and checks that it is between low and high.
func wholeNumber(v any, low, high int64) (int64, bool) {
	var n int64
	switch a := v.(type) {
	case int32:
		n = int64(a)
	case float64:
		if a != math.Trunc(a) || math.Abs(a) > 1<<53 {
			return 0, false
		}
		n = int64(a)
	case string:
		var err error
		n, err = strconv.ParseInt(strings.TrimSpace(a), 10, 64)
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return n, n >= low && n <= high
}

// decodeUUID reads 32 hexadecimal digits, with dashes or without, and gives the 16 bytes.
func decodeUUID(s string) ([]byte, bool) {
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return nil, false
	}
	raw, err := hex.DecodeString(s)
	return raw, err == nil
}

// dateLayouts are the forms of a date that ISODate reads; a date without a zone is in UTC.
var dateLayouts = []string{
	"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02T15:04:05.999999999-0700",
	"2006-01-02T15:04:05.999999999", "2006-01-02T15:04Z07:00", "2006-01-02T15:04", "2006-01-02",
}

// parseDate reads a date as ISODate does.
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// args reads (value, value, ...) and says where each value is.
func (p *parser) args() ([]any, []Span, error) {
	open, err := p.expect("(", "(")
	if err != nil {
		return nil, nil, err
	}
	var vals []any
	var spans []Span
	for {
		t := p.peek()
		if t.kind == tkPunct && t.text == ")" {
			p.next()
			return vals, spans, nil
		}
		if t.kind == tkEOF {
			return nil, nil, errors.New("MongoParseUnexpectedEnd", "the text ends before the ) that closes the arguments", p.meta(open)...)
		}
		start := t.start
		v, err := p.value()
		if err != nil {
			return nil, nil, err
		}
		vals = append(vals, v)
		spans = append(spans, p.idx.span(start, p.prevEnd))
		n := p.peek()
		switch {
		case n.kind == tkPunct && n.text == ",":
			p.next()
		case n.kind == tkPunct && n.text == ")":
		case n.kind == tkError:
			return nil, nil, n.err
		case n.kind == tkEOF:
			return nil, nil, errors.New("MongoParseUnexpectedEnd", "the text ends before the ) that closes the arguments", p.meta(open)...)
		default:
			return nil, nil, errors.New("MongoParseExpectedToken", "expected a comma or ) after the argument, found "+describe(n), append(p.meta(n), "found", describe(n))...)
		}
	}
}
