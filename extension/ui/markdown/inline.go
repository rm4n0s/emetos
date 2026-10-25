package markdown

import (
	"html"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The inline parser follows the algorithm of the CommonMark reference implementation (commonmark.js): a left to right
// scan that collects delimiter runs and brackets on two stacks, resolves links when a closing bracket is found and
// resolves emphasis with the delimiter stack. GFM strikethrough (~~) and the extended autolinks (bare URLs) are added.
//
// The scans that can fail (code spans, HTML comments, link titles) remember their failure or are bounded, so a
// hostile input cannot make the parser quadratic.

type ikind uint8

const (
	iText ikind = iota
	iEmph
	iStrong
	iStrike
	iCode
	iLink
	iImage
	iSoft
	iHard
	iHTML
)

// inode is a node of the inline tree under construction. Siblings form a doubly linked list so the emphasis and
// link algorithms can move runs of nodes under a new parent.
type inode struct {
	kind        ikind
	s           string // text, code or raw HTML; the destination of a link or image
	title       string
	parent      *inode
	prev, next  *inode
	first, last *inode
}

func (n *inode) appendChild(c *inode) {
	c.parent = n
	c.next = nil
	c.prev = n.last
	if n.last != nil {
		n.last.next = c
	} else {
		n.first = c
	}
	n.last = c
}

func (n *inode) unlink() {
	if n.prev != nil {
		n.prev.next = n.next
	} else if n.parent != nil {
		n.parent.first = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else if n.parent != nil {
		n.parent.last = n.prev
	}
	n.parent, n.prev, n.next = nil, nil, nil
}

// insertAfter inserts n after ref.
func (n *inode) insertAfter(ref *inode) {
	n.parent = ref.parent
	n.prev = ref
	n.next = ref.next
	if ref.next != nil {
		ref.next.prev = n
	} else if ref.parent != nil {
		ref.parent.last = n
	}
	ref.next = n
}

// delim is an entry of the delimiter stack: a run of *, _ or ~~.
type delim struct {
	ch                byte
	num, orig         int
	node              *inode
	prev, next        *delim
	canOpen, canClose bool
}

// bracket is an entry of the bracket stack: a [ or ![.
type bracket struct {
	node         *inode
	prev         *bracket
	prevDelim    *delim
	index        int
	image        bool
	active       bool
	bracketAfter bool
}

type inlineParser struct {
	scanner
	refs     map[string]refDef
	root     *inode
	delims   *delim
	brackets *bracket

	runs                                     map[int][]int // backtick runs of the source by length, built on demand
	failComment, failCDATA, failDecl, failPI bool
}

// parseInlines parses the inline content s (already trimmed) with the given link reference definitions.
func parseInlines(s string, refs map[string]refDef) []Inline {
	if s == "" {
		return nil
	}
	p := &inlineParser{scanner: scanner{s: s}, refs: refs, root: &inode{}}
	for p.pos < len(p.s) {
		p.parseOne()
	}
	p.processEmphasis(nil)
	return convertChildren(p.root, false)
}

func (p *inlineParser) addText(s string) {
	if s != "" {
		p.root.appendChild(&inode{kind: iText, s: s})
	}
}

func (p *inlineParser) parseOne() {
	switch p.s[p.pos] {
	case '\n':
		p.newline()
	case '\\':
		p.backslash()
	case '`':
		p.backticks()
	case '*', '_':
		p.delimRun(p.s[p.pos])
	case '~':
		p.tildes()
	case '[':
		p.openBracket(false)
	case '!':
		if p.pos+1 < len(p.s) && p.s[p.pos+1] == '[' {
			p.openBracket(true)
		} else {
			p.addText("!")
			p.pos++
		}
	case ']':
		p.closeBracket()
	case '<':
		p.angle()
	case '&':
		p.entity()
	default:
		p.textRun()
	}
}

func (p *inlineParser) textRun() {
	start := p.pos
	switch p.s[start] {
	case 'h', 'H', 'w', 'W':
		if p.bareURL() {
			return
		}
	}
	end := p.nextSpecial(start + 1)
	p.addText(p.s[start:end])
	p.pos = end
}

// nextSpecial returns the position of the next character at or after from that needs attention.
func (p *inlineParser) nextSpecial(from int) int {
	s := p.s
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '\n', '\\', '`', '*', '_', '~', '[', ']', '!', '<', '&':
			return i
		case 'h', 'H', 'w', 'W':
			if bareURLBoundary(s, i) {
				return i
			}
		}
	}
	return len(s)
}

func (p *inlineParser) skipSpaces() {
	for p.pos < len(p.s) && isSpaceOrTab(p.s[p.pos]) {
		p.pos++
	}
}

func (p *inlineParser) newline() {
	hard := false
	if last := p.root.last; last != nil && last.kind == iText && strings.HasSuffix(last.s, " ") {
		hard = strings.HasSuffix(last.s, "  ")
		last.s = strings.TrimRight(last.s, " ")
	}
	k := iSoft
	if hard {
		k = iHard
	}
	p.root.appendChild(&inode{kind: k})
	p.pos++
	p.skipSpaces()
}

func (p *inlineParser) backslash() {
	if p.pos+1 >= len(p.s) {
		p.addText("\\")
		p.pos++
		return
	}
	n := p.s[p.pos+1]
	switch {
	case n == '\n':
		p.root.appendChild(&inode{kind: iHard})
		p.pos += 2
		p.skipSpaces()
	case isEscapable(n):
		p.addText(p.s[p.pos+1 : p.pos+2])
		p.pos += 2
	default:
		p.addText("\\")
		p.pos++
	}
}

func (p *inlineParser) entity() {
	if dec, n := decodeEntity(p.s[p.pos:]); n > 0 {
		p.addText(dec)
		p.pos += n
		return
	}
	p.addText("&")
	p.pos++
}

// --- code spans ---

func (p *inlineParser) buildRuns() {
	p.runs = map[int][]int{}
	s := p.s
	for i := 0; i < len(s); i++ {
		if s[i] != '`' {
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		p.runs[j-i] = append(p.runs[j-i], i)
		i = j - 1
	}
}

func (p *inlineParser) backticks() {
	start := p.pos
	n := 0
	for p.pos < len(p.s) && p.s[p.pos] == '`' {
		p.pos++
		n++
	}
	after := p.pos
	if p.runs == nil {
		p.buildRuns()
	}
	list := p.runs[n]
	i := sort.SearchInts(list, after)
	if i >= len(list) {
		p.addText(p.s[start:after])
		return
	}
	closeAt := list[i]
	content := strings.ReplaceAll(p.s[after:closeAt], "\n", " ")
	if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.Trim(content, " ") != "" {
		content = content[1 : len(content)-1]
	}
	p.root.appendChild(&inode{kind: iCode, s: content})
	p.pos = closeAt + n
}

// --- emphasis ---

func isPunct(r rune) bool {
	if r < 128 {
		return strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", r)
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func isEscapable(c byte) bool {
	return c < 128 && strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// delimRun handles a run of * or _.
func (p *inlineParser) delimRun(c byte) {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] == c {
		p.pos++
	}
	p.pushDelim(c, start, p.pos-start)
}

// tildes handles a run of ~: exactly two make a strikethrough delimiter.
func (p *inlineParser) tildes() {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] == '~' {
		p.pos++
	}
	if p.pos-start != 2 {
		p.addText(p.s[start:p.pos])
		return
	}
	p.pushDelim('~', start, 2)
}

func (p *inlineParser) pushDelim(c byte, start, n int) {
	before, after := '\n', '\n'
	if start > 0 {
		before, _ = utf8.DecodeLastRuneInString(p.s[:start])
	}
	if start+n < len(p.s) {
		after, _ = utf8.DecodeRuneInString(p.s[start+n:])
	}
	afterWS := unicode.IsSpace(after)
	afterP := isPunct(after)
	beforeWS := unicode.IsSpace(before)
	beforeP := isPunct(before)
	left := !afterWS && (!afterP || beforeWS || beforeP)
	right := !beforeWS && (!beforeP || afterWS || afterP)
	canOpen, canClose := left, right
	if c == '_' {
		canOpen = left && (!right || beforeP)
		canClose = right && (!left || afterP)
	}
	node := &inode{kind: iText, s: p.s[start : start+n]}
	p.root.appendChild(node)
	if !canOpen && !canClose {
		return
	}
	d := &delim{ch: c, num: n, orig: n, node: node, prev: p.delims, canOpen: canOpen, canClose: canClose}
	if p.delims != nil {
		p.delims.next = d
	}
	p.delims = d
}

func (p *inlineParser) removeDelimiter(d *delim) {
	if d.prev != nil {
		d.prev.next = d.next
	}
	if d.next == nil {
		p.delims = d.prev
	} else {
		d.next.prev = d.prev
	}
}

func charIndex(c byte) int {
	switch c {
	case '*':
		return 0
	case '_':
		return 1
	}
	return 2
}

// processEmphasis resolves the delimiters above bottom into emphasis, strong emphasis and strikethrough.
func (p *inlineParser) processEmphasis(bottom *delim) {
	if p.delims == bottom {
		return // nothing above the bottom of the stack
	}
	var openersBottom [3][2][3]*delim
	for i := range openersBottom {
		for j := range openersBottom[i] {
			for k := range openersBottom[i][j] {
				openersBottom[i][j][k] = bottom
			}
		}
	}
	closer := p.delims
	for closer != nil && closer.prev != bottom {
		closer = closer.prev
	}
	for closer != nil {
		if !closer.canClose {
			closer = closer.next
			continue
		}
		ci := charIndex(closer.ch)
		co := 0
		if closer.canOpen {
			co = 1
		}
		ob := &openersBottom[ci][co][closer.orig%3]
		opener := closer.prev
		found := false
		for opener != nil && opener != bottom && opener != *ob {
			oddMatch := (closer.canOpen || opener.canClose) && closer.orig%3 != 0 && (opener.orig+closer.orig)%3 == 0
			if opener.ch == closer.ch && opener.canOpen && !oddMatch {
				found = true
				break
			}
			opener = opener.prev
		}
		oldCloser := closer
		if !found {
			closer = closer.next
			*ob = oldCloser.prev
			if !oldCloser.canOpen {
				p.removeDelimiter(oldCloser)
			}
			continue
		}
		use := 1
		if closer.num >= 2 && opener.num >= 2 {
			use = 2
		}
		openerN, closerN := opener.node, closer.node
		opener.num -= use
		closer.num -= use
		openerN.s = openerN.s[:len(openerN.s)-use]
		closerN.s = closerN.s[:len(closerN.s)-use]
		kind := iEmph
		switch {
		case closer.ch == '~':
			kind = iStrike
		case use == 2:
			kind = iStrong
		}
		emph := &inode{kind: kind}
		for tmp := openerN.next; tmp != nil && tmp != closerN; {
			next := tmp.next
			tmp.unlink()
			emph.appendChild(tmp)
			tmp = next
		}
		emph.insertAfter(openerN)
		if opener.next != closer {
			opener.next = closer
			closer.prev = opener
		}
		if opener.num == 0 {
			openerN.unlink()
			p.removeDelimiter(opener)
		}
		if closer.num == 0 {
			closerN.unlink()
			next := closer.next
			p.removeDelimiter(closer)
			closer = next
		}
	}
	for p.delims != nil && p.delims != bottom {
		p.removeDelimiter(p.delims)
	}
}

// --- links and images ---

func (p *inlineParser) openBracket(image bool) {
	start := p.pos
	text, index := "[", start
	if image {
		text, index = "![", start+1
		p.pos += 2
	} else {
		p.pos++
	}
	node := &inode{kind: iText, s: text}
	p.root.appendChild(node)
	if p.brackets != nil {
		p.brackets.bracketAfter = true
	}
	p.brackets = &bracket{node: node, prev: p.brackets, prevDelim: p.delims, index: index, image: image, active: true}
}

func (p *inlineParser) closeBracket() {
	p.pos++
	startpos := p.pos
	opener := p.brackets
	if opener == nil {
		p.addText("]")
		return
	}
	if !opener.active {
		p.addText("]")
		p.brackets = opener.prev
		return
	}
	save := p.pos
	matched := false
	var dest, title string

	if p.pos < len(p.s) && p.s[p.pos] == '(' {
		p.pos++
		p.spnl()
		if d, ok := p.dest(); ok {
			p.spnl()
			t := ""
			if p.pos < len(p.s) && isSpaceNL(p.s[p.pos-1]) {
				if tt, tok := p.title(); tok {
					t = tt
				}
			}
			p.spnl()
			if p.pos < len(p.s) && p.s[p.pos] == ')' {
				p.pos++
				matched = true
				dest, title = d, t
			}
		}
		if !matched {
			p.pos = save
		}
	}
	if !matched {
		before := p.pos
		n := p.label()
		reflabel := ""
		if n > 2 {
			reflabel = p.s[before : before+n]
		} else if !opener.bracketAfter {
			reflabel = p.s[opener.index:startpos]
		}
		if n == 0 {
			p.pos = save
		}
		if reflabel != "" {
			if key := normalizeLabel(reflabel); key != "" {
				if ref, ok := p.refs[key]; ok {
					matched = true
					dest, title = ref.dest, ref.title
				}
			}
		}
	}
	if !matched {
		p.brackets = opener.prev
		p.pos = startpos
		p.addText("]")
		return
	}
	kind := iLink
	if opener.image {
		kind = iImage
	}
	node := &inode{kind: kind, s: dest, title: title}
	for tmp := opener.node.next; tmp != nil; {
		next := tmp.next
		tmp.unlink()
		node.appendChild(tmp)
		tmp = next
	}
	p.root.appendChild(node)
	p.processEmphasis(opener.prevDelim)
	p.brackets = opener.prev
	opener.node.unlink()
	if !opener.image {
		// Links cannot contain links: deactivate the earlier link openers. Below an inactive one, all are inactive.
		for o := p.brackets; o != nil; o = o.prev {
			if !o.image {
				if !o.active {
					break
				}
				o.active = false
			}
		}
	}
}

func isSpaceNL(c byte) bool { return c == ' ' || c == '\t' || c == '\n' }

// normalizeLabel normalizes a link label ("[Foo  Bar]") for matching: whitespace is collapsed and case is folded.
// It returns "" for a label without content.
func normalizeLabel(label string) string {
	if len(label) >= 2 && label[0] == '[' && label[len(label)-1] == ']' {
		label = label[1 : len(label)-1]
	}
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		return ""
	}
	return strings.ToLower(strings.ToUpper(label))
}

// --- scanner shared by the inline parser and the reference definitions of the block parser ---

type scanner struct {
	s   string
	pos int
}

// spnl skips spaces, tabs and at most one line ending. It reports whether anything was skipped.
func (sc *scanner) spnl() bool {
	start := sc.pos
	for sc.pos < len(sc.s) && isSpaceOrTab(sc.s[sc.pos]) {
		sc.pos++
	}
	if sc.pos < len(sc.s) && sc.s[sc.pos] == '\n' {
		sc.pos++
		for sc.pos < len(sc.s) && isSpaceOrTab(sc.s[sc.pos]) {
			sc.pos++
		}
	}
	return sc.pos > start
}

// label parses a link label at pos. It returns the length of the label including the brackets and moves pos after
// it, or returns 0 and leaves pos alone.
func (sc *scanner) label() int {
	s := sc.s
	if sc.pos >= len(s) || s[sc.pos] != '[' {
		return 0
	}
	for i := sc.pos + 1; i < len(s) && i-sc.pos <= 1000; i++ {
		switch s[i] {
		case '\\':
			i++
		case '[':
			return 0
		case ']':
			n := i + 1 - sc.pos
			sc.pos = i + 1
			return n
		}
	}
	return 0
}

// dest parses a link destination: <...> or a run without spaces and with balanced parentheses.
func (sc *scanner) dest() (string, bool) {
	s := sc.s
	if sc.pos < len(s) && s[sc.pos] == '<' {
		for i := sc.pos + 1; i < len(s); i++ {
			switch c := s[i]; {
			case c == '\n' || c == '<':
				return "", false
			case c == '>':
				d := unescapeString(s[sc.pos+1 : i])
				sc.pos = i + 1
				return d, true
			case c == '\\' && i+1 < len(s) && isEscapable(s[i+1]):
				i++
			}
		}
		return "", false
	}
	start := sc.pos
	i := start
	open := 0
loop:
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isEscapable(s[i+1]):
			i += 2
		case c == '(':
			open++
			i++
			if open > 32 {
				return "", false
			}
		case c == ')':
			if open < 1 {
				break loop
			}
			open--
			i++
		case c <= 0x20 || c == 0x7f:
			break loop
		default:
			i++
		}
	}
	if i > len(s) {
		i = len(s)
	}
	if i == start && (i >= len(s) || s[i] != ')') {
		return "", false
	}
	if open != 0 {
		return "", false
	}
	sc.pos = i
	return unescapeString(s[start:i]), true
}

// title parses a link title in "...", '...' or (...).
func (sc *scanner) title() (string, bool) {
	s := sc.s
	if sc.pos >= len(s) {
		return "", false
	}
	open := s[sc.pos]
	var closeCh byte
	switch open {
	case '"', '\'':
		closeCh = open
	case '(':
		closeCh = ')'
	default:
		return "", false
	}
	limit := min(len(s), sc.pos+4000)
	for j := sc.pos + 1; j < limit; j++ {
		c := s[j]
		switch {
		case c == '\\' && j+1 < len(s) && isEscapable(s[j+1]):
			j++
		case c == closeCh:
			t := unescapeString(s[sc.pos+1 : j])
			sc.pos = j + 1
			return t, true
		case open == '(' && c == '(':
			return "", false
		}
	}
	return "", false
}

// unescapeString decodes backslash escapes and character references.
func unescapeString(s string) string {
	if strings.IndexByte(s, '\\') < 0 && strings.IndexByte(s, '&') < 0 {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\\' && i+1 < len(s) && isEscapable(s[i+1]) {
			sb.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == '&' {
			if dec, n := decodeEntity(s[i:]); n > 0 {
				sb.WriteString(dec)
				i += n
				continue
			}
		}
		sb.WriteByte(c)
		i++
	}
	return sb.String()
}

// decodeEntity decodes the character reference at the start of s (which starts with '&'). It returns the decoded text
// and the length of the reference, or 0 when s does not start with a valid reference.
func decodeEntity(s string) (string, int) {
	if len(s) < 3 || s[0] != '&' {
		return "", 0
	}
	if s[1] == '#' {
		j := 2
		hex := false
		if j < len(s) && (s[j] == 'x' || s[j] == 'X') {
			hex = true
			j++
		}
		start := j
		v := 0
		for j < len(s) && j-start <= 7 {
			d := hexDigit(s[j])
			if d < 0 || (!hex && d > 9) {
				break
			}
			if hex {
				v = v*16 + d
			} else {
				v = v*10 + d
			}
			j++
		}
		digits := j - start
		maxDigits := 7
		if hex {
			maxDigits = 6
		}
		if digits == 0 || digits > maxDigits || j >= len(s) || s[j] != ';' {
			return "", 0
		}
		if v == 0 || v > unicode.MaxRune || (v >= 0xD800 && v <= 0xDFFF) {
			v = unicode.ReplacementChar
		}
		return string(rune(v)), j + 1
	}
	if !isASCIILetter(s[1]) {
		return "", 0
	}
	j := 2
	for j < len(s) && j <= 33 && (isASCIILetter(s[j]) || (s[j] >= '0' && s[j] <= '9')) {
		j++
	}
	if j >= len(s) || s[j] != ';' {
		return "", 0
	}
	name := s[:j+1]
	u := html.UnescapeString(name)
	// html.UnescapeString also matches the legacy entities without a semicolon as a prefix ("&notit;" is "¬it;"). A real
	// reference decodes to at most two characters.
	if u == name || utf8.RuneCountInString(u) > 2 {
		return "", 0
	}
	return u, j + 1
}

func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// --- autolinks and raw HTML ---

func (p *inlineParser) angle() {
	if dest, text, end := scanAutolink(p.s, p.pos); end > 0 {
		link := &inode{kind: iLink, s: dest}
		link.appendChild(&inode{kind: iText, s: text})
		p.root.appendChild(link)
		p.pos = end
		return
	}
	if end := p.scanInlineHTML(); end > 0 {
		p.root.appendChild(&inode{kind: iHTML, s: p.s[p.pos:end]})
		p.pos = end
		return
	}
	p.addText("<")
	p.pos++
}

// scanAutolink scans a CommonMark autolink "<scheme:...>" or "<user@host>" at s[i]. It returns the destination, the
// text and the end position, or end 0.
func scanAutolink(s string, i int) (dest, text string, end int) {
	j := i + 1
	if j >= len(s) || !isASCIILetter(s[j]) {
		// An email address can start with a digit.
		return scanEmailAutolink(s, i)
	}
	// URI: scheme of 2 to 32 characters, ':', then no spaces, '<' or '>'.
	k := j
	for k < len(s) && k-j < 32 && (isASCIILetter(s[k]) || (s[k] >= '0' && s[k] <= '9') || s[k] == '+' || s[k] == '.' || s[k] == '-') {
		k++
	}
	if k-j >= 2 && k < len(s) && s[k] == ':' {
		m := k + 1
		for m < len(s) && s[m] > 0x20 && s[m] != '<' && s[m] != '>' && s[m] != 0x7f {
			m++
		}
		if m < len(s) && s[m] == '>' {
			return s[j:m], s[j:m], m + 1
		}
	}
	return scanEmailAutolink(s, i)
}

func scanEmailAutolink(s string, i int) (dest, text string, end int) {
	j := i + 1
	k := j
	for k < len(s) && (isASCIILetter(s[k]) || (s[k] >= '0' && s[k] <= '9') || strings.IndexByte(".!#$%&'*+/=?^_`{|}~-", s[k]) >= 0) {
		k++
	}
	if k == j || k >= len(s) || s[k] != '@' {
		return "", "", 0
	}
	m := k + 1
	for {
		// one label: alnum, then alnum or '-', ending with alnum, up to 63 characters
		ls := m
		for m < len(s) && m-ls < 63 && (isASCIILetter(s[m]) || (s[m] >= '0' && s[m] <= '9') || s[m] == '-') {
			m++
		}
		if m == ls || s[ls] == '-' || s[m-1] == '-' {
			return "", "", 0
		}
		if m < len(s) && s[m] == '.' {
			m++
			continue
		}
		break
	}
	if m < len(s) && s[m] == '>' {
		return "mailto:" + s[j:m], s[j:m], m + 1
	}
	return "", "", 0
}

// scanInlineHTML scans an inline raw HTML construct at pos and returns its end, or -1.
func (p *inlineParser) scanInlineHTML() int {
	s := p.s
	i := p.pos
	if i+1 >= len(s) {
		return -1
	}
	switch s[i+1] {
	case '/':
		return scanCloseTag(s, i)
	case '!':
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			if p.failComment {
				return -1
			}
			rest := s[i+4:]
			if strings.HasPrefix(rest, ">") {
				return i + 5
			}
			if strings.HasPrefix(rest, "->") {
				return i + 6
			}
			idx := strings.Index(rest, "-->")
			if idx < 0 {
				p.failComment = true
				return -1
			}
			return i + 4 + idx + 3
		case strings.HasPrefix(s[i:], "<![CDATA["):
			if p.failCDATA {
				return -1
			}
			idx := strings.Index(s[i+9:], "]]>")
			if idx < 0 {
				p.failCDATA = true
				return -1
			}
			return i + 9 + idx + 3
		case i+2 < len(s) && isASCIILetter(s[i+2]):
			if p.failDecl {
				return -1
			}
			idx := strings.IndexByte(s[i+2:], '>')
			if idx < 0 {
				p.failDecl = true
				return -1
			}
			return i + 2 + idx + 1
		}
		return -1
	case '?':
		if p.failPI {
			return -1
		}
		idx := strings.Index(s[i+2:], "?>")
		if idx < 0 {
			p.failPI = true
			return -1
		}
		return i + 2 + idx + 2
	}
	return scanOpenTag(s, i)
}

// maxTagLen bounds how far an HTML tag is scanned, so an unclosed quote cannot make the scans quadratic.
const maxTagLen = 8192

func isHTMLSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func isAlnum(c byte) bool { return isASCIILetter(c) || (c >= '0' && c <= '9') }

// scanOpenTag scans an HTML open tag at s[i] ('<') and returns its end, or -1.
func scanOpenTag(s string, i int) int {
	w := s
	if len(w)-i > maxTagLen {
		w = w[:i+maxTagLen]
	}
	j := i + 1
	if j >= len(w) || !isASCIILetter(w[j]) {
		return -1
	}
	for j < len(w) && (isAlnum(w[j]) || w[j] == '-') {
		j++
	}
	for {
		k := j
		for k < len(w) && isHTMLSpace(w[k]) {
			k++
		}
		hadSpace := k > j
		if k >= len(w) {
			return -1
		}
		if w[k] == '>' {
			return k + 1
		}
		if w[k] == '/' {
			if k+1 < len(w) && w[k+1] == '>' {
				return k + 2
			}
			return -1
		}
		if !hadSpace || (!isASCIILetter(w[k]) && w[k] != '_' && w[k] != ':') {
			return -1
		}
		k++
		for k < len(w) && (isAlnum(w[k]) || w[k] == '_' || w[k] == '.' || w[k] == ':' || w[k] == '-') {
			k++
		}
		m := k
		for m < len(w) && isHTMLSpace(w[m]) {
			m++
		}
		if m < len(w) && w[m] == '=' {
			m++
			for m < len(w) && isHTMLSpace(w[m]) {
				m++
			}
			if m >= len(w) {
				return -1
			}
			switch w[m] {
			case '"', '\'':
				idx := strings.IndexByte(w[m+1:], w[m])
				if idx < 0 {
					return -1
				}
				m += idx + 2
			default:
				vs := m
				for m < len(w) && !isHTMLSpace(w[m]) && strings.IndexByte("\"'=<>`", w[m]) < 0 {
					m++
				}
				if m == vs {
					return -1
				}
			}
			k = m
		}
		j = k
	}
}

// scanCloseTag scans an HTML closing tag at s[i] ('<') and returns its end, or -1.
func scanCloseTag(s string, i int) int {
	j := i + 2
	if i+1 >= len(s) || s[i+1] != '/' || j >= len(s) || !isASCIILetter(s[j]) {
		return -1
	}
	for j < len(s) && (isAlnum(s[j]) || s[j] == '-') {
		j++
	}
	for j < len(s) && isHTMLSpace(s[j]) {
		j++
	}
	if j < len(s) && s[j] == '>' {
		return j + 1
	}
	return -1
}

// --- GFM extended autolinks ---

func bareURLBoundary(s string, i int) bool {
	return i == 0 || strings.IndexByte(" \t\n*_~(", s[i-1]) >= 0
}

// bareURL turns a "http://", "https://" or "www." URL at the current position into a link.
func (p *inlineParser) bareURL() bool {
	s := p.s
	i := p.pos
	if !bareURLBoundary(s, i) {
		return false
	}
	rest := s[i:]
	prefix := 0
	www := false
	switch {
	case hasPrefixFold(rest, "https://"):
		prefix = 8
	case hasPrefixFold(rest, "http://"):
		prefix = 7
	case hasPrefixFold(rest, "www."):
		prefix = 4
		www = true
	default:
		return false
	}
	hostStart := i + prefix
	hostEnd := hostStart
	for hostEnd < len(s) && (isAlnum(s[hostEnd]) || s[hostEnd] == '-' || s[hostEnd] == '.' || s[hostEnd] == '_' || s[hostEnd] >= 0x80) {
		hostEnd++
	}
	if hostEnd == hostStart {
		return false
	}
	end := hostEnd
	for end < len(s) && s[end] > 0x20 && s[end] != '<' && s[end] != 0x7f {
		end++
	}
	opens := strings.Count(s[i:end], "(")
	closes := strings.Count(s[i:end], ")")
trim:
	for end > hostStart {
		c := s[end-1]
		switch {
		case strings.IndexByte("?!.,:*_~'\"", c) >= 0:
			end--
		case c == ')' && closes > opens:
			closes--
			end--
		case c == ';':
			j := end - 1
			for j > hostStart && isAlnum(s[j-1]) {
				j--
			}
			if j > hostStart && j < end-1 && s[j-1] == '&' {
				end = j - 1
			} else {
				break trim
			}
		default:
			break trim
		}
	}
	if end <= hostStart {
		return false
	}
	url := s[i:end]
	dest := url
	if www {
		dest = "http://" + url
	}
	link := &inode{kind: iLink, s: dest}
	link.appendChild(&inode{kind: iText, s: url})
	p.root.appendChild(link)
	p.pos = end
	return true
}

// --- conversion of the inline tree into the public AST ---

// convertChildren converts the children of n. Adjacent texts are merged, empty texts dropped, and the links nested in
// a link (autolinks inside link text) are replaced by their text, because links cannot contain links.
func convertChildren(n *inode, inLink bool) []Inline {
	var out []Inline
	var sb strings.Builder
	flush := func() {
		if sb.Len() > 0 {
			out = append(out, Text{S: sb.String()})
			sb.Reset()
		}
	}
	emit := func(in Inline) {
		if t, ok := in.(Text); ok {
			sb.WriteString(t.S)
			return
		}
		flush()
		out = append(out, in)
	}
	for c := n.first; c != nil; c = c.next {
		switch c.kind {
		case iText:
			sb.WriteString(c.s)
		case iEmph:
			emit(Emph{Children: convertChildren(c, inLink)})
		case iStrong:
			emit(Strong{Children: convertChildren(c, inLink)})
		case iStrike:
			emit(Strike{Children: convertChildren(c, inLink)})
		case iCode:
			emit(Code{S: c.s})
		case iLink:
			kids := convertChildren(c, true)
			if inLink {
				for _, k := range kids {
					emit(k)
				}
			} else {
				emit(Link{Dest: c.s, Title: c.title, Children: kids})
			}
		case iImage:
			emit(Image{Dest: c.s, Title: c.title, Alt: PlainText(convertChildren(c, false))})
		case iSoft:
			emit(SoftBreak{})
		case iHard:
			emit(HardBreak{})
		case iHTML:
			emit(HTMLInline{S: c.s})
		}
	}
	flush()
	return out
}
