package snippet

import (
	"regexp"
	"strconv"
	"strings"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetParser.ts (SnippetParser)

// ParseOpts are the options of Parse.
type ParseOpts struct {
	// InsertFinalTabstop adds the final tab stop $0 at the end of a snippet that has tab stops
	// but no $0.
	InsertFinalTabstop bool
	// EnforceFinalTabstop adds $0 at the end of a snippet that has none, whatever else it has.
	EnforceFinalTabstop bool
}

// Parser reads snippets in VS Code's syntax. The syntax has tab stops ($1, ${1:default},
// ${1|a,b|}), variables ($name, ${name:default}), transforms (${name/regex/format/flags}) and
// the escapes \$, \} and \\. A parser is not safe for use by several goroutines; make one for
// each.
type Parser struct {
	scanner Scanner
	token   Token
}

// NewParser makes a parser.
func NewParser() *Parser { return &Parser{} }

var escapeUnescape = regexp.MustCompile(`\\(\$|}|\\)`)

// Parse reads a snippet. Text that is not valid syntax is kept as text: a snippet always parses.
func (p *Parser) Parse(value string, opts ...ParseOpts) *TextmateSnippet {
	var o ParseOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	snippet := NewTextmateSnippet()
	p.ParseFragment(value, snippet)
	p.EnsureFinalTabstop(snippet, o.EnforceFinalTabstop, o.InsertFinalTabstop)
	return snippet
}

// AsInsertText is the text a snippet inserts: without the tab stops, with the default text of
// the placeholders and the variables.
func AsInsertText(value string) string { return NewParser().Parse(value).String() }

// GuessNeedsClipboard tells whether a template mentions the variable CLIPBOARD, so that the
// clipboard is read before the snippet is inserted.
func GuessNeedsClipboard(template string) bool {
	return clipboardUse.MatchString(template)
}

var clipboardUse = regexp.MustCompile(`\$\{?CLIPBOARD`)

// ParseFragment reads a text into an existing snippet, after what it has, and returns the
// markers it added. The first placeholder with an index that has a default value gives it to the
// other placeholders of the index.
func (p *Parser) ParseFragment(value string, snippet *TextmateSnippet) []Marker {
	offset := len(snippet.children)
	p.scanner.Text(value)
	p.token = p.scanner.Next()
	for p.parse(snippet) {
	}

	// fill in values for placeholders: the first placeholder of an index that has a value defines
	// the value for all placeholders with that index
	defaults := map[int][]Marker{}
	seen := map[int]bool{}
	var incomplete []*Placeholder
	snippet.Walk(func(m Marker) bool {
		if ph, ok := m.(*Placeholder); ok {
			switch {
			case ph.IsFinalTabstop():
				seen[0] = true
				delete(defaults, 0)
			case !seen[ph.Index] && len(ph.children) > 0:
				seen[ph.Index] = true
				defaults[ph.Index] = ph.children
			default:
				incomplete = append(incomplete, ph)
			}
		}
		return true
	})

	var fill func(ph *Placeholder, stack map[int]bool)
	fill = func(ph *Placeholder, stack map[int]bool) {
		values := defaults[ph.Index]
		if values == nil {
			return
		}
		clone := NewPlaceholder(ph.Index)
		clone.Transform = ph.Transform
		for _, child := range values {
			c := child.Clone()
			clone.AppendChild(c)
			// "recurse" on children that are again placeholders
			if inner, ok := c.(*Placeholder); ok && defaults[inner.Index] != nil && !stack[inner.Index] {
				stack[inner.Index] = true
				fill(inner, stack)
				delete(stack, inner.Index)
			}
		}
		snippet.Replace(ph, []Marker{clone})
	}
	stack := map[int]bool{}
	for _, ph := range incomplete {
		fill(ph, stack)
	}
	return snippet.children[offset:]
}

// EnsureFinalTabstop adds the final tab stop at the end of a snippet that needs one: any snippet
// when enforce, one that has tab stops when insert.
func (p *Parser) EnsureFinalTabstop(snippet *TextmateSnippet, enforce, insert bool) {
	if enforce || insert && len(snippet.Placeholders()) > 0 {
		for _, ph := range snippet.Placeholders() {
			if ph.Index == 0 {
				return
			}
		}
		// the snippet uses placeholders but has no final tab stop: put it at the end
		snippet.AppendChild(NewPlaceholder(0))
	}
}

// accept takes the token when it is of the type.
func (p *Parser) accept(t TokenType) bool {
	if p.token.Type == t {
		p.token = p.scanner.Next()
		return true
	}
	return false
}

// acceptText takes the token when it is of the type, and gives its text.
func (p *Parser) acceptText(t TokenType) (string, bool) {
	if p.token.Type == t {
		text := p.scanner.TokenText(p.token)
		p.token = p.scanner.Next()
		return text, true
	}
	return "", false
}

// acceptAny takes the token, whatever it is, and gives its text, which is empty at the end.
func (p *Parser) acceptAny() string {
	text := p.scanner.TokenText(p.token)
	p.token = p.scanner.Next()
	return text
}

// backTo goes back to a token; it is false, so that a parse can end with it.
func (p *Parser) backTo(t Token) bool {
	p.scanner.Pos = t.Pos + t.Len
	p.token = t
	return false
}

// until takes tokens up to one of the type, and gives the text before it, with \$, \} and \\
// unescaped; ok is false at the end of the text or after a backslash that escapes nothing.
func (p *Parser) until(t TokenType) (string, bool) {
	start := p.token
	for p.token.Type != t {
		if p.token.Type == EOF {
			return "", false
		}
		if p.token.Type == Backslash {
			next := p.scanner.Next()
			if next.Type != Dollar && next.Type != CurlyClose && next.Type != Backslash {
				return "", false
			}
		}
		p.token = p.scanner.Next()
	}
	value := escapeUnescape.ReplaceAllString(p.scanner.value[start.Pos:p.token.Pos], "$1")
	p.token = p.scanner.Next()
	return value, true
}

func (p *Parser) parse(m Marker) bool {
	return p.parseEscaped(m) || p.parseTabstopOrVariableName(m) || p.parseComplexPlaceholder(m) ||
		p.parseComplexVariable(m) || p.parseAnything(m)
}

// parseEscaped reads \$, \\ and \} as text; a backslash before anything else is a backslash.
func (p *Parser) parseEscaped(m Marker) bool {
	value, ok := p.acceptText(Backslash)
	if !ok {
		return false
	}
	// saw a backslash, append the escaped token or that backslash
	for _, t := range []TokenType{Dollar, CurlyClose, Backslash} {
		if text, ok := p.acceptText(t); ok {
			value = text
			break
		}
	}
	m.AppendChild(NewText(value))
	return true
}

// parseTabstopOrVariableName reads $foo as a variable and $1 as a tab stop.
func (p *Parser) parseTabstopOrVariableName(parent Marker) bool {
	token := p.token
	if !p.accept(Dollar) {
		return p.backTo(token)
	}
	value, ok := p.acceptText(VariableName)
	if !ok {
		value, ok = p.acceptText(Int)
	}
	if !ok {
		return p.backTo(token)
	}
	if !isAllDigits(value) {
		parent.AppendChild(NewVariable(value))
		return true
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		// a number too big for an index is text
		return p.backTo(token)
	}
	parent.AppendChild(NewPlaceholder(n))
	return true
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return s != ""
}

// parseComplexPlaceholder reads ${1}, ${1:<children>}, ${1|a,b|} and ${1/regex/format/flags}.
func (p *Parser) parseComplexPlaceholder(parent Marker) bool {
	token := p.token
	if !p.accept(Dollar) || !p.accept(CurlyOpen) {
		return p.backTo(token)
	}
	text, ok := p.acceptText(Int)
	if !ok {
		return p.backTo(token)
	}
	index, err := strconv.Atoi(text)
	if err != nil {
		return p.backTo(token)
	}
	placeholder := NewPlaceholder(index)

	switch {
	case p.accept(Colon):
		// ${1:<children>}
		for {
			// ...} -> done
			if p.accept(CurlyClose) {
				parent.AppendChild(placeholder)
				return true
			}
			if p.parse(placeholder) {
				continue
			}
			// fallback: the text that was read is text
			parent.AppendChild(NewText("${" + text + ":"))
			for _, c := range placeholder.children {
				parent.AppendChild(c)
			}
			return true
		}

	case placeholder.Index > 0 && p.accept(Pipe):
		// ${1|one,two,three|}
		choice := &Choice{}
		for {
			if p.parseChoiceElement(choice) {
				if p.accept(Comma) {
					// opt, -> more
					continue
				}
				if p.accept(Pipe) {
					placeholder.AppendChild(choice)
					if p.accept(CurlyClose) {
						// ..|} -> done
						parent.AppendChild(placeholder)
						return true
					}
				}
			}
			return p.backTo(token)
		}

	case p.accept(Forwardslash):
		// ${1/<regex>/<format>/<options>}
		if t, ok := p.parseTransform(); ok {
			placeholder.Transform = t
			parent.AppendChild(placeholder)
			return true
		}
		return p.backTo(token)

	case p.accept(CurlyClose):
		// ${1}
		parent.AppendChild(placeholder)
		return true
	}
	// ${1 <- missing curly or colon
	return p.backTo(token)
}

func (p *Parser) parseChoiceElement(parent *Choice) bool {
	token := p.token
	var values []string
	for p.token.Type != Comma && p.token.Type != Pipe {
		var value string
		if text, ok := p.acceptText(Backslash); ok {
			// \, \| or \\
			value = text
			for _, t := range []TokenType{Comma, Pipe, Backslash} {
				if escaped, ok := p.acceptText(t); ok {
					value = escaped
					break
				}
			}
		} else {
			value = p.acceptAny()
		}
		if value == "" {
			// the end of the text
			return p.backTo(token)
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return p.backTo(token)
	}
	parent.AppendChild(NewText(strings.Join(values, "")))
	return true
}

// parseComplexVariable reads ${foo}, ${foo:<children>} and ${foo/regex/format/flags}.
func (p *Parser) parseComplexVariable(parent Marker) bool {
	token := p.token
	if !p.accept(Dollar) || !p.accept(CurlyOpen) {
		return p.backTo(token)
	}
	name, ok := p.acceptText(VariableName)
	if !ok {
		return p.backTo(token)
	}
	variable := NewVariable(name)

	switch {
	case p.accept(Colon):
		// ${foo:<children>}
		for {
			// ...} -> done
			if p.accept(CurlyClose) {
				parent.AppendChild(variable)
				return true
			}
			if p.parse(variable) {
				continue
			}
			// fallback
			parent.AppendChild(NewText("${" + name + ":"))
			for _, c := range variable.children {
				parent.AppendChild(c)
			}
			return true
		}

	case p.accept(Forwardslash):
		// ${foo/<regex>/<format>/<options>}
		if t, ok := p.parseTransform(); ok {
			variable.Transform = t
			parent.AppendChild(variable)
			return true
		}
		return p.backTo(token)

	case p.accept(CurlyClose):
		// ${foo}
		parent.AppendChild(variable)
		return true
	}
	// ${foo <- missing curly or colon
	return p.backTo(token)
}

// parseTransform reads regex/format/flags}, after the first slash of a transform.
func (p *Parser) parseTransform() (*Transform, bool) {
	var pattern, flags strings.Builder
	transform := &Transform{}

	// (1) /regex
	for !p.accept(Forwardslash) {
		if text, ok := p.acceptText(Backslash); ok {
			escaped, isSlash := p.acceptText(Forwardslash)
			if !isSlash {
				escaped = text
			}
			pattern.WriteString(escaped)
			continue
		}
		if p.token.Type != EOF {
			pattern.WriteString(p.acceptAny())
			continue
		}
		return nil, false
	}

	// (2) /format
	for !p.accept(Forwardslash) {
		if text, ok := p.acceptText(Backslash); ok {
			escaped := text
			if e, ok := p.acceptText(Backslash); ok {
				escaped = e
			} else if e, ok := p.acceptText(Forwardslash); ok {
				escaped = e
			}
			transform.AppendChild(NewText(escaped))
			continue
		}
		if p.parseFormatString(transform) || p.parseAnything(transform) {
			continue
		}
		return nil, false
	}

	// (3) /option
	for !p.accept(CurlyClose) {
		if p.token.Type != EOF {
			flags.WriteString(p.acceptAny())
			continue
		}
		return nil, false
	}

	if err := transform.init(pattern.String(), flags.String()); err != nil {
		// an invalid regular expression or flags: the text is text
		return nil, false
	}
	return transform, true
}

// parseFormatString reads $1, ${1}, ${1:/upcase}, ${1:+if}, ${1:-else}, ${1:?if:else} and
// ${1:else} in the format of a transform.
func (p *Parser) parseFormatString(parent Marker) bool {
	token := p.token
	if !p.accept(Dollar) {
		return false
	}
	complex := p.accept(CurlyOpen)
	text, ok := p.acceptText(Int)
	index, err := strconv.Atoi(text)
	switch {
	case !ok || err != nil:
		return p.backTo(token)
	case !complex:
		// $1
		parent.AppendChild(NewFormatString(index, "", nil, nil))
		return true
	case p.accept(CurlyClose):
		// ${1}
		parent.AppendChild(NewFormatString(index, "", nil, nil))
		return true
	case !p.accept(Colon):
		return p.backTo(token)
	}

	switch {
	case p.accept(Forwardslash):
		// ${1:/upcase}
		shorthand, ok := p.acceptText(VariableName)
		if !ok || !p.accept(CurlyClose) {
			return p.backTo(token)
		}
		parent.AppendChild(NewFormatString(index, shorthand, nil, nil))
		return true

	case p.accept(Plus):
		// ${1:+<if>}
		if v, ok := p.until(CurlyClose); ok && v != "" {
			parent.AppendChild(NewFormatString(index, "", &v, nil))
			return true
		}

	case p.accept(Dash):
		// ${2:-<else>}
		if v, ok := p.until(CurlyClose); ok && v != "" {
			parent.AppendChild(NewFormatString(index, "", nil, &v))
			return true
		}

	case p.accept(QuestionMark):
		// ${2:?<if>:<else>}
		if ifValue, ok := p.until(Colon); ok && ifValue != "" {
			if elseValue, ok := p.until(CurlyClose); ok && elseValue != "" {
				parent.AppendChild(NewFormatString(index, "", &ifValue, &elseValue))
				return true
			}
		}

	default:
		// ${1:<else>}
		if v, ok := p.until(CurlyClose); ok && v != "" {
			parent.AppendChild(NewFormatString(index, "", nil, &v))
			return true
		}
	}
	return p.backTo(token)
}

// parseAnything takes any token as text.
func (p *Parser) parseAnything(m Marker) bool {
	if p.token.Type == EOF {
		return false
	}
	m.AppendChild(NewText(p.acceptAny()))
	return true
}

// Escape puts a backslash before the characters that a snippet reads as syntax in plain text:
// $, } and \.
func Escape(value string) string { return EscapeText(value) }
