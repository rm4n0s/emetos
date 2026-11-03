package javascript

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The light scan of a test file (SPEC §10.32 "The providers", JavaScript and TypeScript; "As built (step 26.8)"): the calls of describe,
// it and test (and their aliases and modifiers) whose first argument is a string, with their nesting and their places. It is a lexer
// (comments, strings, templates, regular expressions) and a walk over the brackets, not a parser: a call whose name is built at run time
// is not found here, and the runners' reports add it to the tree when a run reports it.

// scanNode is a suite or a test found in a file.
type scanNode struct {
	// Suite says that the node holds other nodes (describe), else it is a test.
	Suite bool
	// Name is the string the call was given; Line and Col are the place of the call, counted from 1.
	Name      string
	Line, Col int
	// Skip, Only and Todo are the modifiers `.skip` (and xit, xdescribe), `.only` (and fit, fdescribe) and `.todo`; Each says that the
	// call is a template for many (`.each`, `.for`), whose real names are only known when it runs.
	Skip, Only, Todo, Each bool
	Children               []*scanNode
}

type tokKind int

const (
	tokIdent tokKind = iota
	tokString
	tokTemplate
	tokPunct
	tokOther
)

// token is a piece of the source: an identifier, a string (its decoded text), a template (its text when it has no substitution), or one
// punctuation character.
type token struct {
	kind      tokKind
	text      string
	subst     bool
	line, col int
}

// calls are the names that start a test or a suite: whether they are a suite, and the modifier the name itself means.
var calls = map[string]struct{ suite, skip, only bool }{
	"describe": {suite: true}, "suite": {suite: true}, "xdescribe": {suite: true, skip: true}, "fdescribe": {suite: true, only: true},
	"it": {}, "test": {}, "xit": {skip: true}, "xtest": {skip: true}, "fit": {only: true}, "ftest": {only: true},
}

// modifiers are the members of a call that keep it a call: they mark it, or take arguments of their own before the arguments of the call.
var modifiers = map[string]string{
	"skip": "skip", "only": "only", "todo": "todo", "concurrent": "", "sequential": "", "parallel": "", "serial": "", "fails": "", "failing": "",
	"shuffle": "", "each": "each", "for": "each", "runIf": "args", "skipIf": "args",
}

// scanTests finds the suites and tests of a JavaScript or TypeScript source.
func scanTests(src string) []*scanNode {
	toks := lex(src)
	var out []*scanNode
	walk(toks, 0, len(toks), &out)
	return out
}

// walk looks for calls in toks[from:to] and adds the nodes it finds to out; the nodes of the callback of a suite go to the suite.
func walk(toks []token, from, to int, out *[]*scanNode) {
	for i := from; i < to; {
		t := toks[i]
		info, ok := calls[t.text]
		if t.kind != tokIdent || !ok || (i > 0 && (toks[i-1].text == "." || toks[i-1].text == "function")) {
			i++
			continue
		}
		node, open, closeTok, ok := parseCall(toks, i, to)
		if !ok {
			i++
			continue
		}
		node.Suite = info.suite
		node.Skip = node.Skip || info.skip
		node.Only = node.Only || info.only
		node.Line, node.Col = t.line, t.col
		if node.Name == "" {
			// a name made at run time: the tests below a suite like that are the enclosing suite's, as far as the scan can tell
			if node.Suite && !node.Each {
				walk(toks, open+1, closeTok, out)
			}
			i = closeTok + 1
			continue
		}
		if node.Suite && !node.Each {
			walk(toks, open+1, closeTok, &node.Children)
		}
		*out = append(*out, node)
		i = closeTok + 1
	}
}

// parseCall reads the call that starts at toks[i]: `name.modifier(...)...(` and the arguments. It returns the node (its Name empty when the
// first argument is not a string), the index of the opening bracket of the arguments and of the closing one.
func parseCall(toks []token, i, to int) (*scanNode, int, int, bool) {
	node := &scanNode{}
	j := i + 1
	for j+1 < to && toks[j].text == "." && toks[j].kind == tokPunct && toks[j+1].kind == tokIdent {
		kind, known := modifiers[toks[j+1].text]
		if !known {
			return nil, 0, 0, false
		}
		j += 2
		switch kind {
		case "skip":
			node.Skip = true
		case "only":
			node.Only = true
		case "todo":
			node.Todo = true
		case "each", "args":
			if kind == "each" {
				node.Each = true
			}
			switch {
			case j < to && toks[j].text == "(" && toks[j].kind == tokPunct:
				end := matching(toks, j, to)
				if end < 0 {
					return nil, 0, 0, false
				}
				j = end + 1
			case j < to && toks[j].kind == tokTemplate:
				j++
			default:
				return nil, 0, 0, false
			}
		}
	}
	if j < to && toks[j].text == "<" && toks[j].kind == tokPunct {
		// type arguments of a TypeScript call: it<Context>('name', ...)
		depth := 0
		for k := j; k < to && k < j+40; k++ {
			if toks[k].kind != tokPunct {
				continue
			}
			switch toks[k].text {
			case "<":
				depth++
			case ">":
				depth--
			}
			if depth == 0 {
				j = k + 1
				break
			}
		}
	}
	if j >= to || toks[j].text != "(" || toks[j].kind != tokPunct {
		return nil, 0, 0, false
	}
	end := matching(toks, j, to)
	if end < 0 {
		return nil, 0, 0, false
	}
	if j+1 < end {
		if first := toks[j+1]; first.kind == tokString || (first.kind == tokTemplate && !first.subst) {
			// the name is the whole first argument: a string followed by `+` is a name made at run time
			if next := toks[j+2]; next.kind == tokPunct && (next.text == "," || next.text == ")") {
				node.Name = first.text
			}
		}
	}
	return node, j, end, true
}

// matching is the index of the bracket that closes the one at toks[open], else -1.
func matching(toks []token, open, to int) int {
	depth := 0
	for k := open; k < to; k++ {
		if toks[k].kind != tokPunct {
			continue
		}
		switch toks[k].text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return -1
}

// ---- the lexer ----

type lexer struct {
	src       string
	pos       int
	line, col int
	toks      []token
}

// regexAfter are the words after which a slash starts a regular expression.
var regexAfter = map[string]bool{"return": true, "typeof": true, "case": true, "in": true, "of": true, "new": true, "delete": true, "void": true, "throw": true, "else": true, "do": true}

func lex(src string) []token {
	l := &lexer{src: src, line: 1, col: 1}
	for l.pos < len(l.src) {
		l.next()
	}
	return l.toks
}

// advance moves over n bytes, keeping the line and the column.
func (l *lexer) advance(n int) {
	for k := 0; k < n && l.pos < len(l.src); k++ {
		if l.src[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}
}

func (l *lexer) add(kind tokKind, text string, subst bool, line, col int) {
	l.toks = append(l.toks, token{kind: kind, text: text, subst: subst, line: line, col: col})
}

// regexAllowed tells whether a slash at this place starts a regular expression: it does not after a value.
func (l *lexer) regexAllowed() bool {
	if len(l.toks) == 0 {
		return true
	}
	p := l.toks[len(l.toks)-1]
	switch p.kind {
	case tokString, tokTemplate, tokOther:
		return false
	case tokIdent:
		return regexAfter[p.text]
	}
	return p.text != ")" && p.text != "]" && p.text != "}"
}

func (l *lexer) next() {
	c := l.src[l.pos]
	line, col := l.line, l.col
	switch {
	case c == '\n' || c == ' ' || c == '\t' || c == '\r':
		l.advance(1)
	case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.advance(1)
		}
	case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
		l.advance(2)
		for l.pos < len(l.src) && !strings.HasPrefix(l.src[l.pos:], "*/") {
			l.advance(1)
		}
		l.advance(2)
	case c == '/' && l.regexAllowed():
		l.regex()
		l.add(tokOther, "/re/", false, line, col)
	case c == '\'' || c == '"':
		text := l.quoted(c)
		l.add(tokString, text, false, line, col)
	case c == '`':
		text, subst := l.template()
		l.add(tokTemplate, text, subst, line, col)
	case c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= utf8.RuneSelf:
		start := l.pos
		for l.pos < len(l.src) {
			r, size := utf8.DecodeRuneInString(l.src[l.pos:])
			if r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				break
			}
			l.advance(size)
		}
		if l.pos == start { // a character that is neither: skip it
			l.advance(1)
			return
		}
		l.add(tokIdent, l.src[start:l.pos], false, line, col)
	case c >= '0' && c <= '9':
		for l.pos < len(l.src) && (l.src[l.pos] == '.' || l.src[l.pos] == '_' || isAlnum(l.src[l.pos])) {
			l.advance(1)
		}
		l.add(tokOther, "0", false, line, col)
	default:
		l.advance(1)
		l.add(tokPunct, string(c), false, line, col)
	}
}

func isAlnum(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// quoted reads a string in quotes (the quote is at pos) and returns its text; a string that is not closed ends with its line.
func (l *lexer) quoted(q byte) string {
	l.advance(1)
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == q:
			l.advance(1)
			return sb.String()
		case c == '\n':
			return sb.String()
		case c == '\\' && l.pos+1 < len(l.src):
			sb.WriteString(l.escape())
		default:
			sb.WriteByte(c)
			l.advance(1)
		}
	}
	return sb.String()
}

// escape reads an escape sequence (the backslash is at pos) and returns what it means.
func (l *lexer) escape() string {
	e := l.src[l.pos+1]
	l.advance(2)
	switch e {
	case 'n':
		return "\n"
	case 't':
		return "\t"
	case 'r':
		return "\r"
	case '\n':
		return ""
	case 'u':
		if l.pos+4 <= len(l.src) {
			var r rune
			valid := true
			for _, h := range l.src[l.pos : l.pos+4] {
				d := hexDigit(h)
				if d < 0 {
					valid = false
					break
				}
				r = r*16 + rune(d)
			}
			if valid {
				l.advance(4)
				return string(r)
			}
		}
		return "u"
	}
	return string(e)
}

func hexDigit(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10
	}
	return -1
}

// template reads a template literal (the backtick is at pos): its text, and whether it has a substitution.
func (l *lexer) template() (string, bool) {
	l.advance(1)
	var sb strings.Builder
	subst := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '`':
			l.advance(1)
			return sb.String(), subst
		case c == '\\' && l.pos+1 < len(l.src):
			sb.WriteString(l.escape())
		case c == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{':
			subst = true
			l.advance(2)
			l.skipSubstitution()
		default:
			sb.WriteByte(c)
			l.advance(1)
		}
	}
	return sb.String(), subst
}

// skipSubstitution moves over the expression of a `${` up to its closing brace, past the strings and templates in it.
func (l *lexer) skipSubstitution() {
	depth := 1
	for l.pos < len(l.src) && depth > 0 {
		switch c := l.src[l.pos]; c {
		case '{':
			depth++
			l.advance(1)
		case '}':
			depth--
			l.advance(1)
		case '\'', '"':
			l.quoted(c)
		case '`':
			l.template()
		default:
			l.advance(1)
		}
	}
}

// regex moves over a regular expression literal (the slash is at pos), its flags included.
func (l *lexer) regex() {
	l.advance(1)
	inClass := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			return
		case c == '\\':
			l.advance(2)
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			l.advance(1)
			for l.pos < len(l.src) && isAlnum(l.src[l.pos]) {
				l.advance(1)
			}
			return
		}
		l.advance(1)
	}
}
