package jsonpath

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// Path is a compiled expression.
type Path struct {
	src  string
	segs []segment
}

type segKind int

const (
	segName  segKind = iota // .name, ['name']
	segIndex                // [n], [-n]
	segAll                  // .*, [*]
)

type segment struct {
	kind segKind
	name string
	idx  int
	// deep says that the selector is applied to the node and everything under it (the ".." before it).
	deep bool
}

// String is the expression as it was written.
func (p *Path) String() string { return p.src }

// Compile reads an expression. The subset it supports is exactly this:
//
//	$              the whole document (every expression starts with it)
//	.name          the member "name" of an object (letters, digits and any characters except . [ ] ' " * and white space)
//	['name']       the member "name"; "name" in double quotes works too; \\ \' \" \n \t \/ and \uXXXX are escapes
//	[n]            the element n of an array, counted from 0; [-1] is the last
//	[*] and .*     every member of an object (its keys in sorted order) or every element of an array
//	..name         the member "name" at any depth, the node itself included (..['name'], ..[n], ..[*] and ..* work the same way)
//
// Not supported, and refused: filters ([?(...)]), slices ([1:3]), unions ([0,1]), script expressions and functions. Eval gives all
// the matches; an extraction rule takes the first.
func Compile(expr string) (*Path, error) {
	c := &compiler{s: expr}
	c.skipSpace()
	if c.pos >= len(c.s) || c.s[c.pos] != '$' {
		return nil, errors.New("JSONPathNotRoot", "a JSONPath starts with $", "path", expr)
	}
	c.pos++
	for {
		c.skipSpace()
		if c.pos >= len(c.s) {
			break
		}
		deep := false
		switch {
		case strings.HasPrefix(c.s[c.pos:], ".."):
			deep = true
			c.pos += 2
		case c.s[c.pos] == '.':
			c.pos++
		case c.s[c.pos] == '[':
		default:
			return nil, errors.New("JSONPathUnexpected", "unexpected character in the JSONPath", "path", expr, "at", c.pos, "char", string(c.s[c.pos]))
		}
		if c.pos >= len(c.s) {
			return nil, errors.New("JSONPathEmptyName", "the JSONPath ends where a name was expected", "path", expr, "at", c.pos)
		}
		switch c.s[c.pos] {
		case '[':
			c.pos++
			end := c.closeBracket()
			if end < 0 {
				return nil, errors.New("JSONPathUnterminated", "a [ in the JSONPath is not closed", "path", expr, "at", c.pos-1)
			}
			inner := strings.TrimSpace(c.s[c.pos:end])
			at := c.pos
			c.pos = end + 1
			switch {
			case inner == "*":
				c.segs = append(c.segs, segment{kind: segAll, deep: deep})
			case len(inner) > 0 && (inner[0] == '\'' || inner[0] == '"'):
				name, ok := unquote(inner)
				if !ok {
					return nil, errors.New("JSONPathBadQuote", "a quoted name in the JSONPath is not closed or has a bad escape", "path", expr, "at", at)
				}
				c.segs = append(c.segs, segment{kind: segName, name: name, deep: deep})
			default:
				n, err := strconv.Atoi(inner)
				if err != nil {
					if inner != "" && strings.Trim(inner, "-0123456789") == "" {
						return nil, errors.New("JSONPathIndexRange", "the index in the JSONPath is out of range", "path", expr, "index", inner)
					}
					return nil, errors.New("JSONPathUnsupported", "this selector is not in the supported JSONPath: only names, indexes and * are", "path", expr, "selector", inner, "at", at)
				}
				c.segs = append(c.segs, segment{kind: segIndex, idx: n, deep: deep})
			}
		case '*':
			c.pos++
			c.segs = append(c.segs, segment{kind: segAll, deep: deep})
		default:
			start := c.pos
			for c.pos < len(c.s) && !strings.ContainsRune(".[]'\"* \t\r\n", rune(c.s[c.pos])) {
				c.pos++
			}
			if c.pos == start {
				return nil, errors.New("JSONPathEmptyName", "a name was expected in the JSONPath", "path", expr, "at", start)
			}
			c.segs = append(c.segs, segment{kind: segName, name: c.s[start:c.pos], deep: deep})
		}
	}
	return &Path{src: expr, segs: c.segs}, nil
}

type compiler struct {
	s    string
	pos  int
	segs []segment
}

func (c *compiler) skipSpace() {
	for c.pos < len(c.s) && strings.ContainsRune(" \t\r\n", rune(c.s[c.pos])) {
		c.pos++
	}
}

// closeBracket finds the ] that closes the [ before c.pos, skipping quoted text; -1 when there is none.
func (c *compiler) closeBracket() int {
	var quote byte
	for i := c.pos; i < len(c.s); i++ {
		ch := c.s[i]
		switch {
		case quote != 0:
			switch ch {
			case '\\':
				i++
			case quote:
				quote = 0
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case ch == ']':
			return i
		}
	}
	return -1
}

// unquote reads a name in single or double quotes with the escapes of the doc comment of Compile; ok is false when the quote is not
// closed at the end or an escape is bad.
func unquote(s string) (string, bool) {
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case q:
			return b.String(), i == len(s)-1
		case '\\':
			i++
			if i >= len(s) {
				return "", false
			}
			switch s[i] {
			case '\\', '\'', '"', '/':
				b.WriteByte(s[i])
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if i+4 >= len(s) {
					return "", false
				}
				n, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
				if err != nil {
					return "", false
				}
				b.WriteRune(rune(n))
				i += 4
			default:
				return "", false
			}
		default:
			b.WriteByte(ch)
		}
	}
	return "", false
}

// Eval gives every value the path selects in the document (a value decoded by Decode, or by encoding/json into any), in the order of
// the document; the members of an object are visited in the order of their sorted names. A path that selects nothing gives none.
func (p *Path) Eval(doc any) []any {
	nodes := []any{doc}
	for _, s := range p.segs {
		var next []any
		for _, n := range nodes {
			if s.deep {
				walk(n, func(x any) { next = s.apply(next, x) })
			} else {
				next = s.apply(next, n)
			}
		}
		nodes = next
		if len(nodes) == 0 {
			break
		}
	}
	return nodes
}

// First gives the first value the path selects.
func (p *Path) First(doc any) (any, bool) {
	if m := p.Eval(doc); len(m) > 0 {
		return m[0], true
	}
	return nil, false
}

// walk calls fn on the node and then on everything under it, parents first.
func walk(n any, fn func(any)) {
	fn(n)
	switch v := n.(type) {
	case map[string]any:
		for _, k := range sortedKeys(v) {
			walk(v[k], fn)
		}
	case []any:
		for _, x := range v {
			walk(x, fn)
		}
	}
}

func (s segment) apply(out []any, n any) []any {
	switch v := n.(type) {
	case map[string]any:
		switch s.kind {
		case segName:
			if x, ok := v[s.name]; ok {
				out = append(out, x)
			}
		case segAll:
			for _, k := range sortedKeys(v) {
				out = append(out, v[k])
			}
		}
	case []any:
		switch s.kind {
		case segIndex:
			i := s.idx
			if i < 0 {
				i += len(v)
			}
			if i >= 0 && i < len(v) {
				out = append(out, v[i])
			}
		case segAll:
			out = append(out, v...)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }

// Decode reads a JSON document so that numbers keep their text (json.Number). It fails with JSONPathBadDocument when the data is not
// one JSON value.
func Decode(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("JSONPathBadDocument", "the document is not valid UTF-8 JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, errors.NewErr("JSONPathBadDocument", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSONPathBadDocument", "there is more than one JSON value in the document")
	}
	return v, nil
}
