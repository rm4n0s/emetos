package main

import "slices"

// The parser of the subset of TypeScript the completion specs are written in: it reads the object literals, arrays, strings,
// numbers and the names of constants, and skips what is code (functions, calls, operators) as one opaque value. A spec's
// generator that runs a shell command is code, so it is skipped and never run (SPEC §10.25, D117).

// object is a JavaScript object literal with its keys in the order of the source.
type object struct {
	keys []string
	vals map[string]any
}

func newObject() *object { return &object{vals: map[string]any{}} }

func (o *object) set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *object) get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

// opaque is a value that is code: a function, a call, an expression with operators.
type opaque struct{}

// parser reads the tokens of one file.
type parser struct {
	toks []token
	pos  int
	// vars are the constants of the file, by name; defaultExport is the value of `export default`.
	vars          map[string]any
	defaultExport any
	hasDefault    bool
}

// parseFile parses a spec file and returns its constants and the value it exports as default.
func parseFile(src string) *parser {
	p := &parser{toks: lex(src), vars: map[string]any{}}
	for p.pos < len(p.toks) {
		before := p.pos
		p.statement()
		if p.pos == before {
			p.pos++
		}
	}
	return p
}

func (p *parser) peek() token {
	if p.pos >= len(p.toks) {
		return token{}
	}
	return p.toks[p.pos]
}

func (p *parser) peekAt(n int) token {
	if p.pos+n >= len(p.toks) {
		return token{}
	}
	return p.toks[p.pos+n]
}

func (p *parser) next() token {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (t token) isPunct(s string) bool { return t.kind == tokPunct && t.text == s }
func (t token) isIdent(s string) bool { return t.kind == tokIdent && t.text == s }

// skipBalanced skips from an opening bracket to after its closing one.
func (p *parser) skipBalanced() {
	depth := 0
	for p.pos < len(p.toks) {
		t := p.next()
		if t.kind != tokPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
			if depth <= 0 {
				return
			}
		}
	}
}

// statement reads one statement of the top level.
func (p *parser) statement() {
	t := p.peek()
	if t.isPunct(";") {
		p.next()
		return
	}
	if t.isIdent("export") {
		p.next()
		if p.peek().isIdent("default") {
			p.next()
			v := p.expr()
			p.defaultExport, p.hasDefault = v, true
			p.endStatement()
			return
		}
		p.statement()
		return
	}
	switch {
	case t.isIdent("const") || t.isIdent("let") || t.isIdent("var"):
		p.next()
		p.declaration()
	case t.isIdent("import"):
		for p.pos < len(p.toks) && p.peek().kind != tokString {
			p.next()
		}
		p.next()
		p.endStatement()
	case t.isIdent("function") || t.isIdent("class") || t.isIdent("interface") || t.isIdent("enum") || t.isIdent("declare") ||
		t.isIdent("namespace") || t.isIdent("async") && p.peekAt(1).isIdent("function"):
		p.skipToBlock()
	case t.isIdent("type"):
		p.skipType()
		p.endStatement()
	default:
		p.expr()
		p.endStatement()
	}
}

// endStatement skips a closing semicolon.
func (p *parser) endStatement() {
	if p.peek().isPunct(";") {
		p.next()
	}
}

// skipToBlock skips a declaration that ends with a block: up to its `{`, and the block.
func (p *parser) skipToBlock() {
	for p.pos < len(p.toks) && !p.peek().isPunct("{") {
		if p.peek().isPunct("(") || p.peek().isPunct("[") {
			p.skipBalanced()
			continue
		}
		p.next()
	}
	if p.peek().isPunct("{") {
		p.skipBalanced()
	}
}

// declaration reads `NAME [: type] = value` after const, let or var.
func (p *parser) declaration() {
	for {
		name := p.next()
		if name.kind != tokIdent {
			// a destructuring: not a spec
			p.pos--
			p.skipExprStatement()
			return
		}
		// the type annotation, up to the =
		for p.pos < len(p.toks) && !p.peek().isPunct("=") && !p.peek().isPunct(";") && !p.peek().isPunct(",") {
			if p.peek().isPunct("{") || p.peek().isPunct("(") || p.peek().isPunct("[") {
				p.skipBalanced()
				continue
			}
			p.next()
		}
		if p.peek().isPunct("=") {
			p.next()
			p.vars[name.text] = p.expr()
		}
		if p.peek().isPunct(",") {
			p.next()
			continue
		}
		p.endStatement()
		return
	}
}

// skipExprStatement skips to the end of the statement.
func (p *parser) skipExprStatement() {
	for p.pos < len(p.toks) && !p.peek().isPunct(";") {
		if p.peek().isPunct("{") || p.peek().isPunct("(") || p.peek().isPunct("[") {
			p.skipBalanced()
			continue
		}
		p.next()
	}
	p.endStatement()
}

// typeStarters are the words that begin the next statement of a file.
var typeStarters = []string{"const", "let", "var", "function", "export", "interface", "type", "import", "class", "enum"}

// skipType skips a `type X = ...` alias.
func (p *parser) skipType() {
	p.next()
	seenEq := false
	for p.pos < len(p.toks) {
		t := p.peek()
		if t.isPunct(";") {
			return
		}
		if seenEq && t.kind == tokIdent && slices.Contains(typeStarters, t.text) {
			return
		}
		if t.isPunct("=") {
			seenEq = true
		}
		if t.isPunct("{") || t.isPunct("(") || t.isPunct("[") {
			p.skipBalanced()
			continue
		}
		p.next()
	}
}

// binaryOps are the operators that join two operands; the result is code.
var binaryOps = []string{"+", "-", "*", "/", "%", "&", "|", "^", "<", ">", "<=", ">=", "==", "!=", "===", "!==", "&&", "||", "??", "="}

// expr reads an expression and returns its value; an expression with an operator, a call or a function is opaque.
func (p *parser) expr() any {
	v := p.unary()
	for {
		t := p.peek()
		switch {
		case t.kind == tokPunct && slices.Contains(binaryOps, t.text):
			p.next()
			p.unary()
			v = opaque{}
		case t.isIdent("instanceof") || t.isIdent("in"):
			p.next()
			p.unary()
			v = opaque{}
		case t.isPunct("?"):
			p.next()
			p.expr()
			if p.peek().isPunct(":") {
				p.next()
				p.expr()
			}
			v = opaque{}
		case t.isIdent("as") || t.isIdent("satisfies"):
			p.next()
			p.typeExpr()
		default:
			return v
		}
	}
}

// typeExpr skips a type after `as`.
func (p *parser) typeExpr() {
	for {
		t := p.peek()
		switch {
		case t.kind == tokIdent:
			p.next()
			for p.peek().isPunct(".") {
				p.next()
				p.next()
			}
			if p.peek().isPunct("<") {
				depth := 0
				for p.pos < len(p.toks) {
					x := p.next()
					if x.isPunct("<") {
						depth++
					} else if x.isPunct(">") {
						depth--
						if depth == 0 {
							break
						}
					}
				}
			}
		case t.kind == tokString || t.kind == tokNumber:
			p.next()
		case t.isPunct("{") || t.isPunct("(") || t.isPunct("["):
			p.skipBalanced()
		default:
			return
		}
		for p.peek().isPunct("[") && p.peekAt(1).isPunct("]") {
			p.next()
			p.next()
		}
		if p.peek().isPunct("|") || p.peek().isPunct("&") {
			p.next()
			continue
		}
		return
	}
}

// unary reads the operand with its prefix operators.
func (p *parser) unary() any {
	t := p.peek()
	switch {
	case t.isPunct("-") && p.peekAt(1).kind == tokNumber:
		p.next()
		return -p.next().num
	case t.isPunct("!") || t.isPunct("-") || t.isPunct("+") || t.isPunct("~") || t.isIdent("typeof") || t.isIdent("await") || t.isIdent("void") || t.isIdent("new") || t.isIdent("delete"):
		p.next()
		p.unary()
		return opaque{}
	}
	return p.postfix(p.primary())
}

// postfix reads the member accesses and calls after a value.
func (p *parser) postfix(v any) any {
	for {
		t := p.peek()
		switch {
		case t.isPunct(".") || t.isPunct("?."):
			p.next()
			if p.peek().isPunct("(") || p.peek().isPunct("[") {
				continue
			}
			name := p.next()
			if o, ok := v.(*object); ok && name.kind == tokIdent {
				if x, ok := o.get(name.text); ok {
					v = x
					continue
				}
			}
			v = opaque{}
		case t.isPunct("["):
			p.next()
			key := p.expr()
			if p.peek().isPunct("]") {
				p.next()
			}
			if o, ok := v.(*object); ok {
				if s, ok := key.(string); ok {
					if x, ok := o.get(s); ok {
						v = x
						continue
					}
				}
			}
			v = opaque{}
		case t.isPunct("("):
			p.skipBalanced()
			v = opaque{}
		case t.isPunct("!") && !p.peekAt(1).isPunct("="):
			p.next() // a non-null assertion
		default:
			return v
		}
	}
}

// primary reads a literal, a name, a function or a parenthesized expression.
func (p *parser) primary() any {
	t := p.next()
	switch t.kind {
	case tokString:
		return t.text
	case tokTemplate, tokRegex:
		return opaque{}
	case tokNumber:
		return t.num
	case tokIdent:
		switch t.text {
		case "true":
			return true
		case "false":
			return false
		case "null", "undefined":
			return nil
		case "function":
			p.skipFunction()
			return opaque{}
		case "async":
			n := p.peek()
			switch {
			case n.isIdent("function"):
				p.next()
				p.skipFunction()
				return opaque{}
			case n.isPunct("(") && p.arrowAfterParen():
				p.skipArrow()
				return opaque{}
			case n.kind == tokIdent && p.peekAt(1).isPunct("=>"):
				p.next()
				p.next()
				p.arrowBody()
				return opaque{}
			}
		}
		if p.peek().isPunct("=>") {
			p.next()
			p.arrowBody()
			return opaque{}
		}
		if v, ok := p.vars[t.text]; ok {
			return v
		}
		return opaque{}
	case tokPunct:
		switch t.text {
		case "{":
			p.pos--
			return p.object()
		case "[":
			p.pos--
			return p.array()
		case "(":
			p.pos--
			if p.arrowAfterParen() {
				p.skipArrow()
				return opaque{}
			}
			p.next()
			v := p.expr()
			if p.peek().isPunct(")") {
				p.next()
			}
			return v
		case "<":
			// a type assertion before a value: <T>value
			depth := 1
			for p.pos < len(p.toks) && depth > 0 {
				x := p.next()
				if x.isPunct("<") {
					depth++
				} else if x.isPunct(">") {
					depth--
				}
			}
			return p.unary()
		}
	}
	return opaque{}
}

// arrowAfterParen tells whether the parenthesis at the cursor opens the parameters of an arrow function.
func (p *parser) arrowAfterParen() bool {
	depth := 0
	i := p.pos
	for ; i < len(p.toks); i++ {
		t := p.toks[i]
		if t.kind != tokPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth == 0 {
			break
		}
	}
	i++
	if i >= len(p.toks) {
		return false
	}
	if p.toks[i].isPunct("=>") {
		return true
	}
	if p.toks[i].isPunct(":") {
		// a return type: look for the arrow before the end of the expression
		d := 0
		for j := i + 1; j < len(p.toks) && j < i+60; j++ {
			t := p.toks[j]
			if t.kind != tokPunct {
				continue
			}
			switch t.text {
			case "(", "[", "{":
				d++
			case ")", "]", "}":
				d--
				if d < 0 {
					return false
				}
			case "=>":
				return d == 0
			case ",", ";":
				if d == 0 {
					return false
				}
			}
		}
	}
	return false
}

// skipArrow skips an arrow function whose parameters start at the cursor.
func (p *parser) skipArrow() {
	p.skipBalanced()
	for p.pos < len(p.toks) && !p.peek().isPunct("=>") {
		p.next()
	}
	p.next()
	p.arrowBody()
}

// arrowBody skips the body of an arrow function.
func (p *parser) arrowBody() {
	if p.peek().isPunct("{") {
		p.skipBalanced()
		return
	}
	p.expr()
}

// skipFunction skips a function after the word function.
func (p *parser) skipFunction() {
	if p.peek().isPunct("*") {
		p.next()
	}
	if p.peek().kind == tokIdent {
		p.next()
	}
	if p.peek().isPunct("(") {
		p.skipBalanced()
	}
	p.skipToBlock()
}

// object reads an object literal.
func (p *parser) object() any {
	o := newObject()
	p.next() // {
	for p.pos < len(p.toks) && !p.peek().isPunct("}") {
		if p.peek().isPunct(",") {
			p.next()
			continue
		}
		if p.peek().isPunct("...") {
			p.next()
			if src, ok := p.expr().(*object); ok {
				for _, k := range src.keys {
					o.set(k, src.vals[k])
				}
			}
			continue
		}
		key, ok := p.propertyKey()
		switch {
		case p.peek().isPunct(":"):
			p.next()
			v := p.expr()
			if ok {
				o.set(key, v)
			}
		case p.peek().isPunct("("):
			// a method
			p.skipBalanced()
			p.skipToBlock()
			if ok {
				o.set(key, opaque{})
			}
		default:
			// the shorthand {name}
			if ok {
				if v, found := p.vars[key]; found {
					o.set(key, v)
				} else {
					o.set(key, opaque{})
				}
			}
		}
	}
	p.next() // }
	return o
}

// propertyKey reads the name of a property; ok is false for a computed one.
func (p *parser) propertyKey() (string, bool) {
	t := p.next()
	switch t.kind {
	case tokString:
		return t.text, true
	case tokNumber:
		return t.text, true
	case tokIdent:
		// async, get and set before a method name are modifiers
		if (t.text == "async" || t.text == "get" || t.text == "set") && p.peek().kind == tokIdent {
			return p.propertyKey()
		}
		return t.text, true
	case tokPunct:
		if t.text == "[" {
			p.pos--
			p.skipBalanced()
			return "", false
		}
		if t.text == "*" {
			return p.propertyKey()
		}
	}
	return "", false
}

// array reads an array literal.
func (p *parser) array() any {
	var out []any
	p.next() // [
	for p.pos < len(p.toks) && !p.peek().isPunct("]") {
		if p.peek().isPunct(",") {
			p.next()
			continue
		}
		if p.peek().isPunct("...") {
			p.next()
			if src, ok := p.expr().([]any); ok {
				out = append(out, src...)
			}
			continue
		}
		before := p.pos
		out = append(out, p.expr())
		if p.pos == before {
			p.next()
		}
	}
	p.next() // ]
	return out
}
