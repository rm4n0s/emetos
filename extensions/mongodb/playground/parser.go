package playground

import (
	"crypto/rand"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Options say where the values that no literal gives come from. The zero value is the real clock and real random numbers; a test
// gives fixed ones.
type Options struct {
	// Now is the time of new Date() and ISODate() with no argument; time.Now when nil.
	Now func() time.Time
	// NewObjectID makes the value of ObjectId() with no argument; bson.NewObjectID when nil.
	NewObjectID func() bson.ObjectID
	// NewUUID makes the value of UUID() with no argument: 16 random bytes in the form of a version 4 UUID when nil.
	NewUUID func() [16]byte
}

// maxDepth is how deep documents and arrays may be nested; a playground deeper than this is refused rather than followed.
const maxDepth = 100

// Parse reads a playground. It never fails as a whole: the statements it could read are Items with a Statement, and what it could
// not read are Items with an Err, in the order of the text. opts may be omitted.
func Parse(src string, opts ...Options) *Script {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.NewObjectID == nil {
		o.NewObjectID = bson.NewObjectID
	}
	if o.NewUUID == nil {
		o.NewUUID = newUUID
	}
	idx := newLineIndex(src)
	p := &parser{src: src, opts: o, idx: idx, lex: lexer{src: src, idx: idx}}
	script := &Script{Source: src, idx: idx}
	for {
		t := p.peek()
		if t.kind == tkEOF {
			break
		}
		if t.kind == tkPunct && t.text == ";" {
			p.next()
			continue
		}
		p.begin()
		stmt, err := p.statement()
		stmtEnd := max(p.prevEnd, p.stmtStart)
		if err == nil {
			err = p.endStatement()
		}
		item := Item{}
		if err != nil {
			item.Err = err
			p.recover()
			item.Span = idx.span(p.stmtStart, max(p.prevEnd, p.stmtStart))
		} else {
			item.Statement = stmt
			item.Span = idx.span(p.stmtStart, stmtEnd)
		}
		script.Items = append(script.Items, item)
	}
	return script
}

// newUUID makes a random (version 4) UUID.
func newUUID() [16]byte {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		// the system has no random numbers; the all-zero UUID of a version 4 stays recognizable
		u = [16]byte{}
	}
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return u
}

// Place reads where a parse error is: the span of the text it is about. It is false for an error that is not from this package.
func Place(err error) (Span, bool) {
	appErr, ok := errors.FromError(err)
	if !ok {
		return Span{}, false
	}
	num := func(key string) (int, bool) {
		n, ok := appErr.Metadata[key].(int)
		return n, ok
	}
	off, ok1 := num("offset")
	end, ok2 := num("end")
	line, ok3 := num("line")
	col, ok4 := num("column")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return Span{}, false
	}
	endLine, _ := num("endLine")
	endCol, _ := num("endColumn")
	return Span{Start: Pos{Offset: off, Line: line, Column: col}, End: Pos{Offset: end, Line: endLine, Column: endCol}}, true
}

// parser reads the tokens of one script.
type parser struct {
	src  string
	opts Options
	idx  *lineIndex
	lex  lexer

	// buf holds the token that peek read and next has not given yet.
	buf    token
	hasBuf bool
	// prevEnd is the end of the token next gave last.
	prevEnd int
	// depth counts the brackets opened and not yet closed since the statement started; nest counts the documents and arrays inside
	// one another.
	depth, nest int
	// stmtStart is where the statement being read starts and taken counts the tokens it has taken.
	stmtStart, taken int
}

// begin starts a statement at the next token.
func (p *parser) begin() {
	t := p.peek()
	p.stmtStart, p.taken, p.depth, p.nest = t.start, 0, 0, 0
	p.prevEnd = t.start
}

// peek returns the next token without taking it.
func (p *parser) peek() token {
	if !p.hasBuf {
		p.buf, p.hasBuf = p.lex.next(), true
	}
	return p.buf
}

// next takes the next token.
func (p *parser) next() token {
	t := p.peek()
	p.hasBuf = false
	if t.kind != tkEOF {
		p.prevEnd = t.end
		p.taken++
	}
	if t.kind == tkPunct {
		switch t.text {
		case "(", "[", "{":
			p.depth++
		case ")", "]", "}":
			p.depth--
		}
	}
	return t
}

// meta is the metadata that says where an error is: the token t.
func (p *parser) meta(t token) []any { return p.lex.meta(t.start, t.end) }

// metaFound is the metadata for an error about the token t that was found where something else was expected. When t starts a new
// line (or is the end of the text) at the level of the statement, the statement is what ended too soon, so the place is the end of
// the last token of the statement, not the next statement.
func (p *parser) metaFound(t token) []any {
	if (t.nl || t.kind == tkEOF) && p.depth <= 0 && p.taken > 0 {
		return p.lex.meta(p.prevEnd, p.prevEnd)
	}
	return p.lex.meta(t.start, t.end)
}

// metaSpan is the metadata for the text from start to end.
func (p *parser) metaSpan(start, end int) []any { return p.lex.meta(start, end) }

// recover skips to the start of the next statement after an error: to the semicolon at the level of the statement, or to the
// first token on a later line when no bracket is open. A statement that was never started is skipped by one token at least.
func (p *parser) recover() {
	if p.taken == 0 && p.peek().kind != tkEOF {
		p.next()
	}
	for {
		t := p.peek()
		if t.kind == tkEOF {
			return
		}
		if t.nl && p.startsStatement(t) {
			p.depth = 0
			return
		}
		if p.depth <= 0 {
			if t.kind == tkPunct && t.text == ";" {
				p.next()
				return
			}
			if t.nl {
				return
			}
		}
		p.next()
	}
}

// startsStatement tells whether the token, the first on its line, looks like the start of a statement even though brackets are open:
// db. or db[, and use or show at the start of the line followed by a word. Nothing else that is valid starts a line like this inside
// a document, so after an error this is where reading goes on.
func (p *parser) startsStatement(t token) bool {
	if t.kind != tkIdent || t.end >= len(p.src) {
		return false
	}
	next := p.src[t.end]
	switch t.text {
	case "db":
		return next == '.' || next == '['
	case "use", "show":
		return (next == ' ' || next == '\t') && (t.start == 0 || p.src[t.start-1] == '\n')
	}
	return false
}

// endStatement checks that the statement is followed by a semicolon, the end of the line or the end of the text.
func (p *parser) endStatement() error {
	t := p.peek()
	switch {
	case t.kind == tkEOF, t.nl:
		return nil
	case t.kind == tkPunct && t.text == ";":
		p.next()
		return nil
	case t.kind == tkError:
		return t.err
	}
	return errors.New("MongoParseTrailing", "a statement ends at a semicolon or at the end of the line", append(p.meta(t), "found", describe(t))...)
}

// describe says what a token is, for a message.
func describe(t token) string {
	switch t.kind {
	case tkEOF:
		return "the end of the text"
	case tkIdent, tkNumber:
		return t.text
	case tkString:
		return "a string"
	case tkRegex:
		return "a regular expression"
	}
	return "\"" + t.text + "\""
}
