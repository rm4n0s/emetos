package regex

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/rm4n0s/errors"
)

// node is a piece of a parsed Oniguruma pattern.
type node interface{}

type (
	// litNode is one character.
	litNode struct{ r rune }
	// dotNode is the dot; all is true when it matches a line break too.
	dotNode struct{ all bool }
	// anchorNode is an anchor: one of A z Z b B G ^ $.
	anchorNode struct{ kind byte }
	// setNode is a class, or an escape that stands for a set of characters.
	setNode struct{ set *charSet }
	// rawNode is text of regexp2's syntax that is one unit already.
	rawNode struct{ text string }
	// seqNode is a concatenation.
	seqNode struct{ items []node }
	// altNode is an alternation.
	altNode struct{ branches []node }
	// keepNode is \K.
	keepNode struct{}
	// absentNode is the absence operator (?~...).
	absentNode struct{ body node }
	// flagNode is a group that changes the ignore-case flag: (?i:...) and (?-i:...), or the rest
	// of a group after (?i).
	flagNode struct {
		ic   bool
		body node
	}
	// condNode is a conditional (?(1)yes|no).
	condNode struct {
		ref     int
		yes, no node
	}
	// quantNode is a repetition.
	quantNode struct {
		body     node
		min, max int // max is -1 for no limit
		mode     quantMode
	}
	// backrefNode is \1 or \k<name>. digits is the text of \10, which is an octal escape when
	// there are not so many groups.
	backrefNode struct {
		index  int
		digits string
	}
	// callNode is a call of a group, \g<name>; target 0 is the whole pattern.
	callNode struct{ target int }
	// groupNode is a group.
	groupNode struct {
		kind  groupKind
		index int  // the number of a capturing group
		ic    bool // the ignore-case flag where the group is
		body  node
	}
)

// quantMode is how greedy a repetition is.
type quantMode int

const (
	greedy quantMode = iota
	lazy
	possessive
)

// groupKind is the kind of a group.
type groupKind int

const (
	capturing groupKind = iota
	nonCapturing
	atomic
	lookahead
	negLookahead
	lookbehind
	negLookbehind
)

// flags are the options in force at a point of the pattern.
type flags struct {
	ic     bool // ignore case
	ext    bool // extended: whitespace and comments are ignored
	dotAll bool // the dot matches a line break (Oniguruma's m)
}

// ref is a reference to a group that is resolved when the whole pattern is read, so that it can
// name a group that comes later.
type ref struct {
	name     string // a reference by name
	num      int    // a reference by number
	relative bool   // num counts from the groups opened so far: \k<-1>, \g<+1>
	opened   int    // the number of groups opened where the reference is
	set      func(index int)
}

// parser reads an Oniguruma pattern.
type parser struct {
	src      []rune
	pos      int
	groups   int
	names    map[string][]int
	defs     map[int]*groupNode
	refs     []ref
	usesA    bool
	usesG    bool
	usesKeep bool
	calls    int
}

// parseOnig reads pattern. Its syntax is Oniguruma's default (the Ruby syntax) with every
// group capturing, which is how vscode-oniguruma compiles grammars.
func parseOnig(pattern string, ic bool) (*parser, node, error) {
	p := &parser{src: []rune(pattern), names: map[string][]int{}, defs: map[int]*groupNode{}}
	root, err := p.alt(flags{ic: ic})
	if err != nil {
		return nil, nil, err
	}
	if p.pos < len(p.src) {
		return nil, nil, errors.New("RegexUnbalancedParen", "a closing parenthesis has no opening one",
			"offset", p.pos, "pattern", string(p.src))
	}
	for _, r := range p.refs {
		index, err := p.resolve(r)
		if err != nil {
			return nil, nil, err
		}
		r.set(index)
	}
	return p, root, nil
}

// resolve finds the number of the group a reference names.
func (p *parser) resolve(r ref) (int, error) {
	if r.name != "" {
		idx := p.names[r.name]
		if len(idx) == 0 {
			return 0, errors.New("RegexUnknownGroup", "no group has this name", "name", r.name, "pattern", string(p.src))
		}
		return idx[len(idx)-1], nil
	}
	index := r.num
	if r.relative && r.num < 0 {
		index = r.opened + r.num + 1
	} else if r.relative {
		index = r.opened + r.num
	}
	if index < 0 || index > p.groups || (r.relative && index < 1) {
		return 0, errors.New("RegexUnknownGroup", "no group has this number", "number", index, "pattern", string(p.src))
	}
	return index, nil
}

// peek returns the rune at the current position, or -1 at the end.
func (p *parser) peek() rune {
	if p.pos >= len(p.src) {
		return -1
	}
	return p.src[p.pos]
}

// has tells whether the text continues with s.
func (p *parser) has(s string) bool {
	rs := []rune(s)
	if p.pos+len(rs) > len(p.src) {
		return false
	}
	for i, r := range rs {
		if p.src[p.pos+i] != r {
			return false
		}
	}
	return true
}

// skipExtended skips whitespace and # comments when the extended flag is on.
func (p *parser) skipExtended(f flags) {
	if !f.ext {
		return
	}
	for p.pos < len(p.src) {
		r := p.src[p.pos]
		switch {
		case unicode.IsSpace(r):
			p.pos++
		case r == '#':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

// alt reads alternatives up to a closing parenthesis or the end.
func (p *parser) alt(f flags) (node, error) {
	var branches []node
	for {
		b, err := p.seq(f)
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
		if p.peek() != '|' {
			break
		}
		p.pos++
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &altNode{branches}, nil
}

// seq reads a concatenation up to |, ) or the end.
func (p *parser) seq(f flags) (node, error) {
	var items []node
	for {
		p.skipExtended(f)
		r := p.peek()
		if r == -1 || r == '|' || r == ')' {
			break
		}
		// (?i) and its kind change the options of the rest of the group, the alternatives that
		// follow included: a(?i)b|c is a(?i:b|c).
		if p.has("(?") {
			if nf, ok, err := p.flagsOnly(f); err != nil {
				return nil, err
			} else if ok {
				rest, err := p.alt(nf)
				if err != nil {
					return nil, err
				}
				items = append(items, flagged(f, nf, rest))
				break
			}
		}
		atom, err := p.atom(f)
		if err != nil {
			return nil, err
		}
		atom, err = p.quantifiers(f, atom)
		if err != nil {
			return nil, err
		}
		items = append(items, atom)
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return &seqNode{items}, nil
}

// flagsOnly reads a group such as (?i) or (?x-m) that holds no pattern. The second result is
// false, and nothing is read, when the text is another kind of group.
func (p *parser) flagsOnly(f flags) (flags, bool, error) {
	i := p.pos + 2
	for i < len(p.src) && strings.ContainsRune("imxWDSPyg{}wa-", p.src[i]) {
		i++
	}
	if i >= len(p.src) || p.src[i] != ')' || i == p.pos+2 {
		return f, false, nil
	}
	nf, err := p.applyFlags(f, string(p.src[p.pos+2:i]))
	if err != nil {
		return f, false, err
	}
	p.pos = i + 1
	return nf, true, nil
}

// applyFlags changes f by the letters of a group such as (?i-m).
func (p *parser) applyFlags(f flags, letters string) (flags, error) {
	on := true
	for _, r := range letters {
		switch r {
		case '-':
			on = false
		case 'i':
			f.ic = on
		case 'x':
			f.ext = on
		case 'm':
			f.dotAll = on
		default:
			return f, errors.New("RegexUnsupportedFeature", "this option is not supported", "option", string(r), "pattern", string(p.src))
		}
	}
	return f, nil
}

// flagged makes the group that holds body under the flags after. The ignore-case flag is the
// only one the group needs to keep.
func flagged(before, after flags, body node) node {
	if before.ic == after.ic {
		return &groupNode{kind: nonCapturing, ic: after.ic, body: body}
	}
	return &flagNode{ic: after.ic, body: body}
}

// quantifiers reads the repetitions that follow an atom.
func (p *parser) quantifiers(f flags, atom node) (node, error) {
	for {
		p.skipExtended(f)
		q, ok := p.quantifier()
		if !ok {
			return atom, nil
		}
		if !repeatable(atom) {
			return nil, errors.New("RegexNothingToRepeat", "the target of a repetition is not valid",
				"offset", p.pos, "pattern", string(p.src))
		}
		mode := greedy
		if q.max >= 0 && q.min > q.max {
			// Oniguruma reads {3,1} as {1,3} that does not give back.
			q.min, q.max = q.max, q.min
			mode = possessive
		}
		switch {
		case mode == possessive:
		case p.peek() == '?' && q.exact:
			// In the Ruby syntax a{n}? is (a{n})?, and not a lazy repetition.
			p.pos++
			atom = &quantNode{body: &quantNode{body: atom, min: q.min, max: q.max}, min: 0, max: 1}
			continue
		case p.peek() == '?':
			p.pos++
			mode = lazy
		case p.peek() == '+' && q.interval:
			// In the Ruby syntax a{n,m}+ is (a{n,m})+.
			p.pos++
			atom = &quantNode{body: &quantNode{body: atom, min: q.min, max: q.max}, min: 1, max: -1}
			continue
		case p.peek() == '+':
			p.pos++
			mode = possessive
		}
		atom = &quantNode{body: atom, min: q.min, max: q.max, mode: mode}
	}
}

// quantity is a repetition count read by quantifier.
type quantity struct {
	min, max int  // max is -1 for no limit
	interval bool // written as {n,m}
	exact    bool // written as {n}
}

// repeatable tells whether a repetition may follow n.
func repeatable(n node) bool {
	switch n.(type) {
	case *anchorNode, *keepNode:
		return false
	}
	return true
}

// quantifier reads * + ? or {n,m}. The second result is false, and nothing is read, when the
// text has no repetition.
func (p *parser) quantifier() (quantity, bool) {
	switch p.peek() {
	case '*':
		p.pos++
		return quantity{min: 0, max: -1}, true
	case '+':
		p.pos++
		return quantity{min: 1, max: -1}, true
	case '?':
		p.pos++
		return quantity{min: 0, max: 1}, true
	case '{':
		save := p.pos
		p.pos++
		lo, hasLo := p.number()
		hasComma := false
		hi, hasHi := 0, false
		if p.peek() == ',' {
			hasComma = true
			p.pos++
			hi, hasHi = p.number()
		}
		if p.peek() != '}' || (!hasLo && !hasHi) {
			p.pos = save
			return quantity{}, false
		}
		p.pos++
		switch {
		case !hasComma:
			return quantity{min: lo, max: lo, interval: true, exact: true}, true
		case !hasHi:
			return quantity{min: lo, max: -1, interval: true}, true
		default:
			return quantity{min: lo, max: hi, interval: true}, true
		}
	}
	return quantity{}, false
}

// number reads decimal digits.
func (p *parser) number() (int, bool) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if start == p.pos {
		return 0, false
	}
	n, err := strconv.Atoi(string(p.src[start:p.pos]))
	if err != nil {
		return 1<<31 - 1, true
	}
	return n, true
}

// atom reads one element: a character, an escape, a class, a group, an anchor or the dot.
func (p *parser) atom(f flags) (node, error) {
	r := p.src[p.pos]
	switch r {
	case '(':
		return p.group(f)
	case '[':
		set, err := p.class(f)
		if err != nil {
			return nil, err
		}
		return &setNode{set}, nil
	case '.':
		p.pos++
		return &dotNode{all: f.dotAll}, nil
	case '^', '$':
		p.pos++
		return &anchorNode{kind: byte(r)}, nil
	case '*', '+', '?':
		return nil, errors.New("RegexNothingToRepeat", "the target of a repetition is not specified",
			"offset", p.pos, "pattern", string(p.src))
	case '\\':
		return p.escape(f)
	}
	p.pos++
	return &litNode{r}, nil
}

// group reads a parenthesized group.
func (p *parser) group(f flags) (node, error) {
	open := p.pos
	p.pos++ // (
	kind, name, cond := capturing, "", false
	if p.peek() == '?' {
		switch {
		case p.has("?#"):
			for p.pos < len(p.src) && p.src[p.pos] != ')' {
				p.pos++
			}
			if p.pos >= len(p.src) {
				p.pos = open
				return nil, errors.New("RegexUnbalancedParen", "a comment is not closed",
					"offset", p.pos, "pattern", string(p.src))
			}
			p.pos++
			return &seqNode{}, nil
		case p.has("?:"):
			p.pos += 2
			kind = nonCapturing
		case p.has("?>"):
			p.pos += 2
			kind = atomic
		case p.has("?="):
			p.pos += 2
			kind = lookahead
		case p.has("?!"):
			p.pos += 2
			kind = negLookahead
		case p.has("?<="):
			p.pos += 3
			kind = lookbehind
		case p.has("?<!"):
			p.pos += 3
			kind = negLookbehind
		case p.has("?~"):
			p.pos += 2
			body, err := p.groupBody(f, open)
			if err != nil {
				return nil, err
			}
			return &absentNode{body}, nil
		case p.has("?<") || p.has("?'"):
			closer := '>'
			if p.src[p.pos+1] == '\'' {
				closer = '\''
			}
			p.pos += 2
			start := p.pos
			for p.pos < len(p.src) && p.src[p.pos] != closer {
				p.pos++
			}
			if p.pos >= len(p.src) || p.pos == start {
				p.pos = open
				return nil, errors.New("RegexInvalidGroupName", "the name of a group is missing or not closed",
					"offset", p.pos, "pattern", string(p.src))
			}
			name = string(p.src[start:p.pos])
			p.pos++
		case p.has("?("):
			cond = true
		default:
			// (?i:...) and (?-i:...)
			end := p.pos + 1
			for end < len(p.src) && strings.ContainsRune("imxWDSPyg{}wa-", p.src[end]) {
				end++
			}
			if end >= len(p.src) || p.src[end] != ':' {
				p.pos = open
				return nil, errors.New("RegexInvalidGroup", "the group is not valid",
					"offset", p.pos, "pattern", string(p.src))
			}
			nf, err := p.applyFlags(f, string(p.src[p.pos+1:end]))
			if err != nil {
				return nil, err
			}
			p.pos = end + 1
			body, err := p.groupBody(nf, open)
			if err != nil {
				return nil, err
			}
			return flagged(f, nf, body), nil
		}
	}
	if cond {
		return p.conditional(f, open)
	}
	g := &groupNode{kind: kind, ic: f.ic}
	if kind == capturing {
		p.groups++
		g.index = p.groups
		p.defs[g.index] = g
		if name != "" {
			p.names[name] = append(p.names[name], g.index)
		}
	}
	body, err := p.groupBody(f, open)
	if err != nil {
		return nil, err
	}
	g.body = body
	return g, nil
}

// groupBody reads the alternatives of a group and its closing parenthesis.
func (p *parser) groupBody(f flags, open int) (node, error) {
	body, err := p.alt(f)
	if err != nil {
		return nil, err
	}
	if p.peek() != ')' {
		p.pos = open
		return nil, errors.New("RegexUnbalancedParen", "a group is not closed",
			"offset", p.pos, "pattern", string(p.src))
	}
	p.pos++
	return body, nil
}

// conditional reads (?(1)yes|no) and (?(<name>)yes|no).
func (p *parser) conditional(f flags, open int) (node, error) {
	p.pos += 2 // ?(
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != ')' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		p.pos = open
		return nil, errors.New("RegexUnbalancedParen", "the condition of a group is not closed",
			"offset", p.pos, "pattern", string(p.src))
	}
	text := strings.Trim(string(p.src[start:p.pos]), "<>'")
	p.pos++
	c := &condNode{}
	if n, err := strconv.Atoi(text); err == nil {
		c.ref = n
	} else {
		p.refs = append(p.refs, ref{name: text, set: func(i int) { c.ref = i }})
	}
	body, err := p.groupBody(f, open)
	if err != nil {
		return nil, err
	}
	branches := []node{body}
	if a, ok := body.(*altNode); ok {
		branches = a.branches
	}
	if len(branches) > 2 {
		p.pos = open
		return nil, errors.New("RegexInvalidGroup", "a conditional has two branches at most",
			"offset", p.pos, "pattern", string(p.src))
	}
	c.yes = branches[0]
	if len(branches) == 2 {
		c.no = branches[1]
	}
	return c, nil
}

// escape reads a backslash escape outside a class.
func (p *parser) escape(f flags) (node, error) {
	if p.pos+1 >= len(p.src) {
		return nil, errors.New("RegexInvalidEscape", "the pattern ends with a backslash",
			"offset", p.pos, "pattern", string(p.src))
	}
	c := p.src[p.pos+1]
	switch c {
	case 'A', 'z', 'Z', 'b', 'B', 'G':
		p.pos += 2
		switch c {
		case 'A':
			p.usesA = true
		case 'G':
			p.usesG = true
		}
		return &anchorNode{kind: byte(c)}, nil
	case 'K':
		p.pos += 2
		p.usesKeep = true
		return &keepNode{}, nil
	case 'R':
		p.pos += 2
		return &rawNode{`(?>\r\n|[\n\v\f\r\u0085  ])`}, nil
	case 'X':
		p.pos += 2
		return &rawNode{`(?>\r\n|\P{M}\p{M}*|\p{M}+)`}, nil
	case 'N':
		p.pos += 2
		return &rawNode{`[^\n]`}, nil
	case 'O':
		p.pos += 2
		return &rawNode{anyChar}, nil
	case 'k', 'g':
		return p.reference(c)
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		start := p.pos + 1
		p.pos++
		n, _ := p.number()
		b := &backrefNode{index: n}
		if digits := string(p.src[start:p.pos]); len(digits) > 1 && octalDigits(digits) {
			// \10 is a reference only when there are that many groups; judged when it is written.
			b.digits = digits
		} else {
			p.refs = append(p.refs, ref{num: n, set: func(i int) { b.index = i }})
		}
		return b, nil
	}
	set, runes, err := p.charEscape(false)
	if err != nil {
		return nil, err
	}
	if set != nil {
		return &setNode{set}, nil
	}
	if len(runes) == 1 {
		return &litNode{runes[0]}, nil
	}
	seq := &seqNode{}
	for _, r := range runes {
		seq.items = append(seq.items, &litNode{r})
	}
	return &groupNode{kind: nonCapturing, body: seq}, nil
}

// octalDigits tells whether s is made of octal digits.
func octalDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '7' {
			return false
		}
	}
	return true
}

// reference reads \k<name> and \g<name>, with a number or a relative number instead of a name.
func (p *parser) reference(kind rune) (node, error) {
	start := p.pos
	p.pos += 2
	var closer rune
	switch p.peek() {
	case '<':
		closer = '>'
	case '\'':
		closer = '\''
	default:
		p.pos = start
		return nil, errors.New("RegexInvalidEscape", "\\"+string(kind)+" needs a name in <> or ''",
			"offset", p.pos, "pattern", string(p.src))
	}
	p.pos++
	begin := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != closer {
		p.pos++
	}
	if p.pos >= len(p.src) || p.pos == begin {
		p.pos = start
		return nil, errors.New("RegexInvalidGroupName", "the name of a reference is missing or not closed",
			"offset", p.pos, "pattern", string(p.src))
	}
	text := string(p.src[begin:p.pos])
	p.pos++
	// A level after a name (\k<name+1>) is not supported; it is read as part of the name.
	var set func(int)
	var out node
	if kind == 'k' {
		b := &backrefNode{}
		set = func(i int) { b.index = i }
		out = b
	} else {
		c := &callNode{}
		set = func(i int) { c.target = i }
		out = c
		p.calls++
	}
	r := ref{name: text, opened: p.groups, set: set}
	if n, err := strconv.Atoi(text); err == nil {
		r.name, r.num = "", n
		r.relative = text[0] == '-' || text[0] == '+'
	}
	p.refs = append(p.refs, r)
	return out, nil
}

// charEscape reads an escape that stands for a character or a set of characters, in or outside
// a class: \d \w \s \h \p{..} \n \t \xHH \x{H..} \uHHHH \0oo \cX and an escaped
// punctuation. The result is a set, or the characters the escape stands for (\x{A B} has
// several).
func (p *parser) charEscape(inClass bool) (set *charSet, runes []rune, err error) {
	c := p.src[p.pos+1]
	p.pos += 2
	shorthand := func(text, inv string) (*charSet, []rune, error) {
		return &charSet{items: []setItem{{text: text, inv: inv}}}, nil, nil
	}
	switch c {
	case 'd':
		return shorthand(`\d`, `\D`)
	case 'D':
		return shorthand(`\D`, `\d`)
	case 'w':
		return shorthand(`\w`, `\W`)
	case 'W':
		return shorthand(`\W`, `\w`)
	case 's':
		return shorthand(`\s`, `\S`)
	case 'S':
		return shorthand(`\S`, `\s`)
	case 'h':
		s := &charSet{}
		s.addRange('0', '9')
		s.addRange('A', 'F')
		s.addRange('a', 'f')
		return s, nil, nil
	case 'H':
		s := &charSet{neg: true}
		s.addRange('0', '9')
		s.addRange('A', 'F')
		s.addRange('a', 'f')
		return s, nil, nil
	case 'p', 'P':
		if p.peek() != '{' {
			// Without braces \p is the letter.
			return nil, []rune{c}, nil
		}
		s, err := p.property(c == 'P')
		return s, nil, err
	case 't':
		return nil, []rune{'\t'}, nil
	case 'n':
		return nil, []rune{'\n'}, nil
	case 'r':
		return nil, []rune{'\r'}, nil
	case 'f':
		return nil, []rune{'\f'}, nil
	case 'v':
		return nil, []rune{'\v'}, nil
	case 'a':
		return nil, []rune{7}, nil
	case 'e':
		return nil, []rune{0x1b}, nil
	case 'b':
		if inClass {
			return nil, []rune{8}, nil
		}
	case 'x':
		if p.peek() == '{' {
			p.pos++
			start := p.pos
			for p.pos < len(p.src) && p.src[p.pos] != '}' {
				p.pos++
			}
			if p.pos >= len(p.src) {
				return nil, nil, errors.New("RegexInvalidEscape", "\\x{ is not closed",
					"offset", p.pos, "pattern", string(p.src))
			}
			fields := strings.Fields(string(p.src[start:p.pos]))
			p.pos++
			var runes []rune
			for _, field := range fields {
				v, perr := strconv.ParseUint(field, 16, 32)
				if perr != nil || v > unicode.MaxRune {
					return nil, nil, errors.New("RegexInvalidEscape", "the code point of \\x{} is not valid",
						"offset", p.pos, "pattern", string(p.src))
				}
				runes = append(runes, rune(v))
			}
			if len(runes) == 0 {
				return nil, nil, errors.New("RegexInvalidEscape", "\\x{} has no code point",
					"offset", p.pos, "pattern", string(p.src))
			}
			return nil, runes, nil
		}
		v, n := p.hexDigits(2)
		if n == 0 {
			return nil, nil, errors.New("RegexInvalidEscape", "\\x needs hex digits",
				"offset", p.pos, "pattern", string(p.src))
		}
		return nil, []rune{rune(v)}, nil
	case 'u':
		v, n := p.hexDigits(4)
		if n != 4 {
			return nil, nil, errors.New("RegexInvalidEscape", "\\u needs four hex digits",
				"offset", p.pos, "pattern", string(p.src))
		}
		return nil, []rune{rune(v)}, nil
	case 'o':
		if p.peek() == '{' {
			p.pos++
			start := p.pos
			for p.pos < len(p.src) && p.src[p.pos] != '}' {
				p.pos++
			}
			if p.pos >= len(p.src) {
				return nil, nil, errors.New("RegexInvalidEscape", "\\o{ is not closed",
					"offset", p.pos, "pattern", string(p.src))
			}
			v, perr := strconv.ParseUint(string(p.src[start:p.pos]), 8, 32)
			p.pos++
			if perr != nil || v > unicode.MaxRune {
				return nil, nil, errors.New("RegexInvalidEscape", "the code point of \\o{} is not valid",
					"offset", p.pos, "pattern", string(p.src))
			}
			return nil, []rune{rune(v)}, nil
		}
	case '0':
		v := 0
		for i := 0; i < 2 && p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '7'; i++ {
			v = v*8 + int(p.src[p.pos]-'0')
			p.pos++
		}
		return nil, []rune{rune(v)}, nil
	case 'c':
		if p.pos < len(p.src) {
			x := p.src[p.pos]
			p.pos++
			return nil, []rune{x & 0x1f}, nil
		}
		return nil, nil, errors.New("RegexInvalidEscape", "\\c needs a character",
			"offset", p.pos, "pattern", string(p.src))
	case 'C', 'M':
		return nil, nil, errors.New("RegexUnsupportedFeature", "control and meta escapes are not supported", "escape", `\`+string(c), "pattern", string(p.src))
	case 'y', 'Y':
		return nil, nil, errors.New("RegexUnsupportedFeature", "text segment boundaries are not supported", "escape", `\`+string(c), "pattern", string(p.src))
	}
	// Any other escaped character stands for itself.
	return nil, []rune{c}, nil
}

// hexDigits reads up to max hex digits.
func (p *parser) hexDigits(max int) (value, n int) {
	for n < max && p.pos < len(p.src) {
		d := hexValue(p.src[p.pos])
		if d < 0 {
			break
		}
		value = value*16 + d
		p.pos++
		n++
	}
	return value, n
}

// hexValue is the value of a hex digit, or -1.
func hexValue(r rune) int {
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

// property reads the {name} of \p and \P, which may start with ^.
func (p *parser) property(negate bool) (*charSet, error) {
	if p.peek() != '{' {
		return nil, errors.New("RegexInvalidEscape", "\\p needs a name in braces",
			"offset", p.pos, "pattern", string(p.src))
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '}' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return nil, errors.New("RegexInvalidEscape", "\\p{ is not closed",
			"offset", p.pos, "pattern", string(p.src))
	}
	name := string(p.src[start:p.pos])
	p.pos++
	if strings.HasPrefix(name, "^") {
		negate = !negate
		name = name[1:]
	}
	set, ok := propertySet(name)
	if !ok {
		return nil, errors.New("RegexUnknownProperty", "this property name is not known", "name", name, "pattern", string(p.src))
	}
	if negate {
		return set.negated(), nil
	}
	return set, nil
}

// class reads a character class starting at [.
func (p *parser) class(f flags) (*charSet, error) {
	open := p.pos
	p.pos++ // [
	neg := false
	if p.peek() == '^' {
		neg = true
		p.pos++
	}
	// The operands of &&; the class is their intersection.
	var operands []*charSet
	cur := &charSet{}
	first := true
	for {
		if p.pos >= len(p.src) {
			p.pos = open
			return nil, errors.New("RegexUnterminatedClass", "a character class is not closed",
				"offset", p.pos, "pattern", string(p.src))
		}
		r := p.src[p.pos]
		switch {
		case r == ']' && !first:
			p.pos++
			operands = append(operands, cur)
			var set *charSet
			if len(operands) == 1 {
				set = operands[0]
			} else {
				set = &charSet{and: operands}
			}
			if neg {
				set = set.negated()
			}
			return set, nil
		case r == '&' && p.has("&&"):
			p.pos += 2
			operands = append(operands, cur)
			cur = &charSet{}
			first = false
			continue
		case r == '[':
			if sub, ok, err := p.posixBracket(); err != nil {
				return nil, err
			} else if ok {
				cur.addSet(sub)
			} else {
				sub, err := p.class(f)
				if err != nil {
					return nil, err
				}
				cur.addSet(sub)
			}
			first = false
			continue
		}
		first = false
		lo, loSet, err := p.classAtom()
		if err != nil {
			return nil, err
		}
		if loSet != nil {
			cur.addSet(loSet)
			continue
		}
		// A range: lo-hi, unless the dash is the last character or a set follows.
		if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			save := p.pos
			p.pos++
			if p.peek() == '[' || p.has("&&") {
				p.pos = save
			} else {
				hi, hiSet, err := p.classAtom()
				if err != nil {
					return nil, err
				}
				if hiSet != nil {
					// \d-\w style: the dash is a character.
					cur.addRange(lo, lo)
					cur.addRange('-', '-')
					cur.addSet(hiSet)
					continue
				}
				if hi < lo {
					return nil, errors.New("RegexInvalidRange", "the end of a character range is before its start",
						"offset", p.pos, "pattern", string(p.src))
				}
				cur.addRange(lo, hi)
				continue
			}
		}
		cur.addRange(lo, lo)
	}
}

// classAtom reads a character or an escape in a class: a rune, or a set for \d and the like.
func (p *parser) classAtom() (rune, *charSet, error) {
	r := p.src[p.pos]
	if r != '\\' {
		p.pos++
		return r, nil, nil
	}
	if p.pos+1 >= len(p.src) {
		return 0, nil, errors.New("RegexInvalidEscape", "the pattern ends with a backslash",
			"offset", p.pos, "pattern", string(p.src))
	}
	set, runes, err := p.charEscape(true)
	if err != nil {
		return 0, nil, err
	}
	if set != nil {
		return 0, set, nil
	}
	if len(runes) == 1 {
		return runes[0], nil, nil
	}
	// \x{85 2028} is a set of characters.
	multi := &charSet{}
	for _, r := range runes {
		multi.addRange(r, r)
	}
	return 0, multi, nil
}

// posixBracket reads [:alpha:] and [:^alpha:] in a class. The second result is false, and
// nothing is read, when the text is not one.
func (p *parser) posixBracket() (*charSet, bool, error) {
	if !p.has("[:") {
		return nil, false, nil
	}
	end := p.pos + 2
	for end < len(p.src) && (p.src[end] == '^' || (p.src[end] >= 'a' && p.src[end] <= 'z')) {
		end++
	}
	if end+1 >= len(p.src) || p.src[end] != ':' || p.src[end+1] != ']' {
		return nil, false, nil
	}
	name := string(p.src[p.pos+2 : end])
	neg := strings.HasPrefix(name, "^")
	name = strings.TrimPrefix(name, "^")
	set, ok := posixSet(name)
	if !ok {
		return nil, false, errors.New("RegexUnknownProperty", "this POSIX bracket name is not known", "name", name, "pattern", string(p.src))
	}
	p.pos = end + 2
	if neg {
		return set.negated(), true, nil
	}
	return set, true, nil
}
