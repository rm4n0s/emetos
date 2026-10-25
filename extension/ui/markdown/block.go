package markdown

import (
	"strings"
)

// The block parser follows the algorithm of the CommonMark reference implementation (commonmark.js): every line is
// matched against the open containers (block quotes, list items), then against the block starts, and what remains is
// added to the innermost open leaf. Inlines are parsed after the whole tree is built, because reference definitions
// can come after their uses.

const (
	// codeIndent is the indentation, in columns, of an indented code block.
	codeIndent = 4
	// maxDepth bounds the nesting of containers, so a line of ">>>>..." cannot build a tree as deep as the line is long.
	maxDepth = 64
)

type nodeKind int

const (
	kDoc nodeKind = iota
	kQuote
	kList
	kItem
	kPara
	kHeading
	kCode
	kRule
	kHTML
)

// bnode is a node of the block tree under construction.
type bnode struct {
	kind          nodeKind
	parent        *bnode
	children      []*bnode
	open          bool
	lastLineBlank bool
	depth         int
	// start and end are the 0-based source lines of the block, end exclusive.
	start, end int
	// lines is the text of a paragraph, heading, code block or HTML block. For a fenced code block lines[0] is the
	// info string.
	lines []string

	// list and item
	ordered      bool
	startNum     int
	marker       byte // the bullet character, or the delimiter of an ordered list
	tight        bool
	padding      int
	markerOffset int

	// fenced code
	fenced      bool
	fenceChar   byte
	fenceLen    int
	fenceOffset int

	// html block
	htmlType int

	// heading
	level int
}

func (n *bnode) lastChild() *bnode {
	if len(n.children) == 0 {
		return nil
	}
	return n.children[len(n.children)-1]
}

func acceptsLines(k nodeKind) bool { return k == kPara || k == kCode || k == kHTML }

func canContain(parent, child nodeKind) bool {
	switch parent {
	case kDoc, kQuote:
		return child != kItem
	case kItem:
		return child != kItem
	case kList:
		return child == kItem
	}
	return false
}

// refDef is a link reference definition.
type refDef struct{ dest, title string }

type blockParser struct {
	doc, tip, oldtip, lastMatched *bnode
	allClosed                     bool
	line                          string
	lineNo                        int
	offset, column                int
	nextNonspace                  int
	nextNonspaceColumn            int
	indent                        int
	indented, blank, partialTab   bool
	refs                          map[string]refDef
}

// Parse parses src into blocks. It never fails and never panics, whatever the input (arbitrary bytes, CRLF, tabs, NULs).
func Parse(src string) (blocks []Block) {
	defer func() {
		if recover() != nil {
			// A bug in the parser must not take the editor down: show the text as paragraphs.
			blocks = fallbackBlocks(src)
		}
	}()
	return parse(src)
}

// fallbackBlocks is the result of Parse when the parser itself failed: one paragraph per line of text.
func fallbackBlocks(src string) []Block {
	var out []Block
	for i, l := range splitLines(src) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, Paragraph{Inlines: []Inline{Text{S: l}}, Pos: Span{StartLine: i, EndLine: i + 1}})
	}
	return out
}

func parse(src string) []Block {
	lines := splitLines(src)
	p := &blockParser{refs: map[string]refDef{}}
	p.doc = &bnode{kind: kDoc, open: true}
	p.tip = p.doc
	for i, ln := range lines {
		p.lineNo = i
		p.incorporate(ln)
	}
	p.lineNo = len(lines)
	for p.tip != nil {
		p.finalize(p.tip)
	}
	c := &converter{refs: p.refs}
	return c.blocks(p.doc.children)
}

// splitLines splits src on "\n", "\r\n" and "\r". A final line terminator does not start another line, and NUL
// characters are replaced with U+FFFD.
func splitLines(src string) []string {
	if src == "" {
		return nil
	}
	if strings.IndexByte(src, 0) >= 0 {
		src = strings.ReplaceAll(src, "\x00", "�")
	}
	var lines []string
	start := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c != '\n' && c != '\r' {
			continue
		}
		lines = append(lines, src[start:i])
		if c == '\r' && i+1 < len(src) && src[i+1] == '\n' {
			i++
		}
		start = i + 1
	}
	if start < len(src) {
		lines = append(lines, src[start:])
	}
	return lines
}

func isSpaceOrTab(c byte) bool { return c == ' ' || c == '\t' }

func isBlankString(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isSpaceOrTab(s[i]) {
			return false
		}
	}
	return true
}

func (p *blockParser) findNextNonspace() {
	i := p.offset
	cols := p.column
	for i < len(p.line) {
		c := p.line[i]
		if c != ' ' && c != '\t' {
			break
		}
		i++
		if c == ' ' {
			cols++
		} else {
			cols += 4 - cols%4
		}
	}
	p.blank = i >= len(p.line)
	p.nextNonspace = i
	p.nextNonspaceColumn = cols
	p.indent = cols - p.column
	p.indented = p.indent >= codeIndent
}

func (p *blockParser) advanceNextNonspace() {
	p.offset = p.nextNonspace
	p.column = p.nextNonspaceColumn
	p.partialTab = false
}

func (p *blockParser) advanceOffset(count int, columns bool) {
	for count > 0 && p.offset < len(p.line) {
		if p.line[p.offset] == '\t' {
			toTab := 4 - p.column%4
			if columns {
				p.partialTab = toTab > count
				adv := min(toTab, count)
				p.column += adv
				if !p.partialTab {
					p.offset++
				}
				count -= adv
			} else {
				p.partialTab = false
				p.column += toTab
				p.offset++
				count--
			}
		} else {
			p.partialTab = false
			p.offset++
			p.column++
			count--
		}
	}
}

func (p *blockParser) peekAt(i int) byte {
	if i < 0 || i >= len(p.line) {
		return 0
	}
	return p.line[i]
}

// addLine adds the rest of the current line to the tip.
func (p *blockParser) addLine() {
	s := ""
	if p.partialTab {
		p.offset++
		s = strings.Repeat(" ", 4-p.column%4)
	}
	if p.offset < len(p.line) {
		s += p.line[p.offset:]
	}
	p.tip.lines = append(p.tip.lines, s)
	p.tip.end = p.lineNo + 1
}

func (p *blockParser) addChild(kind nodeKind) *bnode {
	for !canContain(p.tip.kind, kind) {
		p.finalize(p.tip)
	}
	n := &bnode{kind: kind, parent: p.tip, open: true, start: p.lineNo, end: p.lineNo + 1, depth: p.tip.depth + 1}
	p.tip.children = append(p.tip.children, n)
	p.tip = n
	return n
}

func (p *blockParser) closeUnmatched() {
	if p.allClosed {
		return
	}
	for p.oldtip != p.lastMatched {
		parent := p.oldtip.parent
		p.finalize(p.oldtip)
		p.oldtip = parent
	}
	p.allClosed = true
}

// finalize closes a block and makes its parent the tip.
func (p *blockParser) finalize(n *bnode) {
	above := n.parent
	n.open = false
	switch n.kind {
	case kPara:
		p.finalizePara(n)
	case kCode:
		if !n.fenced {
			for len(n.lines) > 0 && isBlankString(n.lines[len(n.lines)-1]) {
				n.lines = n.lines[:len(n.lines)-1]
			}
			n.end = n.start + len(n.lines)
		}
	case kHTML:
		for len(n.lines) > 0 && isBlankString(n.lines[len(n.lines)-1]) {
			n.lines = n.lines[:len(n.lines)-1]
		}
		n.end = n.start + max(len(n.lines), 1)
	case kList:
		n.tight = listIsTight(n)
		fallthrough
	case kItem, kQuote, kDoc:
		for _, c := range n.children {
			n.end = max(n.end, c.end)
		}
	}
	p.tip = above
}

func endsWithBlankLine(n *bnode) bool {
	for n != nil {
		if n.lastLineBlank {
			return true
		}
		if n.kind == kList || n.kind == kItem {
			n = n.lastChild()
		} else {
			break
		}
	}
	return false
}

func listIsTight(list *bnode) bool {
	for i, item := range list.children {
		last := i == len(list.children)-1
		if endsWithBlankLine(item) && !last {
			return false
		}
		for j, sub := range item.children {
			subLast := j == len(item.children)-1
			if endsWithBlankLine(sub) && (!last || !subLast) {
				return false
			}
		}
	}
	return true
}

// finalizePara extracts the link reference definitions at the start of the paragraph and removes the paragraph when
// nothing else is left.
func (p *blockParser) finalizePara(n *bnode) {
	p.extractRefs(n)
	if len(n.lines) == 0 {
		if par := n.parent; par != nil && par.lastChild() == n {
			par.children = par.children[:len(par.children)-1]
		}
		return
	}
	n.end = n.start + len(n.lines)
}

func (p *blockParser) extractRefs(n *bnode) {
	if len(n.lines) == 0 || !strings.HasPrefix(n.lines[0], "[") {
		return
	}
	s := strings.Join(n.lines, "\n")
	changed := false
	for strings.HasPrefix(s, "[") {
		cons, ok := p.parseRefDef(s)
		if !ok {
			break
		}
		consumed := strings.Count(s[:cons], "\n")
		if cons == len(s) {
			consumed++
		}
		n.start += consumed
		s = s[cons:]
		changed = true
	}
	if !changed {
		return
	}
	if s == "" {
		n.lines = nil
		return
	}
	n.lines = strings.Split(s, "\n")
}

// parseRefDef parses a link reference definition at the start of s, registers it (the first definition of a label
// wins) and returns the number of bytes consumed, up to and including the line ending.
func (p *blockParser) parseRefDef(s string) (int, bool) {
	sc := &scanner{s: s}
	n := sc.label()
	if n == 0 {
		return 0, false
	}
	label := s[:n]
	if sc.pos >= len(s) || s[sc.pos] != ':' {
		return 0, false
	}
	sc.pos++
	sc.spnl()
	angle := sc.pos < len(s) && s[sc.pos] == '<'
	dest, ok := sc.dest()
	if !ok || (dest == "" && !angle) {
		return 0, false
	}
	beforeTitle := sc.pos
	hadSpace := sc.spnl()
	title := ""
	hasTitle := false
	if hadSpace && sc.pos < len(s) {
		if t, tok := sc.title(); tok {
			title = t
			hasTitle = true
		}
	}
	end := sc.pos
	if hasTitle {
		if e, eol := restOfLineBlank(s, end); eol {
			end = e
		} else {
			hasTitle = false
		}
	}
	if !hasTitle {
		title = ""
		e, eol := restOfLineBlank(s, beforeTitle)
		if !eol {
			return 0, false
		}
		end = e
	}
	key := normalizeLabel(label)
	if key == "" {
		return 0, false
	}
	if _, dup := p.refs[key]; !dup {
		p.refs[key] = refDef{dest: dest, title: title}
	}
	return end, true
}

// restOfLineBlank reports whether s[pos:] up to the next line ending is blank, and returns the position after the
// line ending.
func restOfLineBlank(s string, pos int) (int, bool) {
	i := pos
	for i < len(s) && isSpaceOrTab(s[i]) {
		i++
	}
	if i >= len(s) {
		return len(s), true
	}
	if s[i] == '\n' {
		return i + 1, true
	}
	return 0, false
}

// incorporate adds one line to the tree.
func (p *blockParser) incorporate(ln string) {
	allMatched := true
	container := p.doc
	p.oldtip = p.tip
	p.line = ln
	p.offset = 0
	p.column = 0
	p.blank = false
	p.partialTab = false

	for {
		last := container.lastChild()
		if last == nil || !last.open {
			break
		}
		container = last
		p.findNextNonspace()
		switch p.continueBlock(container) {
		case 0:
		case 1:
			allMatched = false
		case 2:
			return
		}
		if !allMatched {
			container = container.parent
			break
		}
	}
	p.allClosed = container == p.oldtip
	p.lastMatched = container

	matchedLeaf := container.kind != kPara && acceptsLines(container.kind)
	for !matchedLeaf {
		p.findNextNonspace()
		if !p.indented && !maybeSpecial(ln, p.nextNonspace) {
			p.advanceNextNonspace()
			break
		}
		matched := false
		for _, start := range blockStarts {
			res := start(p, container)
			if res == 1 {
				container = p.tip
				matched = true
				break
			}
			if res == 2 {
				container = p.tip
				matched = true
				matchedLeaf = true
				break
			}
		}
		if !matched {
			p.advanceNextNonspace()
			break
		}
	}

	if !p.allClosed && !p.blank && p.tip.kind == kPara {
		// lazy paragraph continuation
		p.addLine()
		return
	}
	p.closeUnmatched()
	if p.blank {
		if lc := container.lastChild(); lc != nil {
			lc.lastLineBlank = true
		}
	}
	t := container.kind
	// Block quote lines are never blank, blank lines in fenced code do not count, and neither does the first line of
	// an empty list item.
	lastLineBlank := p.blank && t != kQuote &&
		(t != kCode || !container.fenced) &&
		(t != kItem || len(container.children) != 0 || container.start != p.lineNo)
	for c := container; c != nil; c = c.parent {
		c.lastLineBlank = lastLineBlank
	}
	if acceptsLines(t) {
		p.addLine()
		if t == kHTML && container.htmlType >= 1 && container.htmlType <= 5 {
			rest := ""
			if p.offset < len(ln) {
				rest = ln[p.offset:]
			}
			if htmlBlockEnds(container.htmlType, rest) {
				p.finalize(container)
			}
		}
	} else if p.offset < len(ln) && !p.blank {
		p.addChild(kPara)
		p.advanceNextNonspace()
		p.addLine()
	}
}

func maybeSpecial(ln string, i int) bool {
	if i >= len(ln) {
		return false
	}
	switch c := ln[i]; {
	case c >= '0' && c <= '9':
		return true
	case strings.IndexByte("#`~*+_=<>-", c) >= 0:
		return true
	}
	return false
}

// continueBlock tells whether the open container matches the current line: 0 matched, 1 not matched, 2 the line was
// consumed (the closing fence of a code block).
func (p *blockParser) continueBlock(n *bnode) int {
	switch n.kind {
	case kQuote:
		if !p.indented && p.peekAt(p.nextNonspace) == '>' {
			p.advanceNextNonspace()
			p.advanceOffset(1, false)
			if isSpaceOrTab(p.peekAt(p.offset)) {
				p.advanceOffset(1, true)
			}
			n.end = p.lineNo + 1
			return 0
		}
		return 1
	case kItem:
		if p.blank {
			if len(n.children) == 0 {
				return 1
			}
			p.advanceNextNonspace()
		} else if p.indent >= n.markerOffset+n.padding {
			p.advanceOffset(n.markerOffset+n.padding, true)
		} else {
			return 1
		}
		return 0
	case kList:
		return 0
	case kCode:
		if n.fenced {
			if p.indent <= 3 && p.peekAt(p.nextNonspace) == n.fenceChar {
				run := 0
				for i := p.nextNonspace; i < len(p.line) && p.line[i] == n.fenceChar; i++ {
					run++
				}
				if run >= n.fenceLen && isBlankString(p.line[p.nextNonspace+run:]) {
					n.end = p.lineNo + 1
					p.finalize(n)
					return 2
				}
			}
			i := n.fenceOffset
			for i > 0 && isSpaceOrTab(p.peekAt(p.offset)) {
				p.advanceOffset(1, true)
				i--
			}
			return 0
		}
		if p.indent >= codeIndent {
			p.advanceOffset(codeIndent, true)
		} else if p.blank {
			p.advanceNextNonspace()
		} else {
			return 1
		}
		return 0
	case kHTML:
		if p.blank && (n.htmlType == 6 || n.htmlType == 7) {
			return 1
		}
		return 0
	case kPara:
		if p.blank {
			return 1
		}
		return 0
	}
	return 1
}

// blockStarts are tried in order on the rest of a line. They return 0 for no match, 1 for a container that was opened
// and 2 for a leaf that was opened.
var blockStarts = []func(p *blockParser, container *bnode) int{
	startBlockQuote,
	startATXHeading,
	startFencedCode,
	startHTMLBlock,
	startSetextHeading,
	startThematicBreak,
	startListItem,
	startIndentedCode,
}

func (p *blockParser) rest() string {
	if p.nextNonspace >= len(p.line) {
		return ""
	}
	return p.line[p.nextNonspace:]
}

func startBlockQuote(p *blockParser, container *bnode) int {
	if p.indented || p.peekAt(p.nextNonspace) != '>' || container.depth >= maxDepth {
		return 0
	}
	p.advanceNextNonspace()
	p.advanceOffset(1, false)
	if isSpaceOrTab(p.peekAt(p.offset)) {
		p.advanceOffset(1, true)
	}
	p.closeUnmatched()
	p.addChild(kQuote)
	return 1
}

func startATXHeading(p *blockParser, _ *bnode) int {
	if p.indented {
		return 0
	}
	rest := p.rest()
	level := 0
	for level < len(rest) && rest[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || (level < len(rest) && !isSpaceOrTab(rest[level])) {
		return 0
	}
	p.advanceNextNonspace()
	p.advanceOffset(level, false)
	for isSpaceOrTab(p.peekAt(p.offset)) {
		p.advanceOffset(1, true)
	}
	p.closeUnmatched()
	h := p.addChild(kHeading)
	h.level = level
	content := ""
	if p.offset < len(p.line) {
		content = p.line[p.offset:]
	}
	h.lines = []string{atxContent(content)}
	p.advanceOffset(len(p.line)-p.offset, false)
	return 2
}

// atxContent removes the optional closing sequence of #s of an ATX heading, and the spaces around the content.
func atxContent(s string) string {
	t := strings.TrimRight(s, " \t")
	u := strings.TrimRight(t, "#")
	if len(u) < len(t) && (u == "" || isSpaceOrTab(u[len(u)-1])) {
		t = strings.TrimRight(u, " \t")
	}
	return strings.TrimLeft(t, " \t")
}

func startFencedCode(p *blockParser, _ *bnode) int {
	if p.indented {
		return 0
	}
	rest := p.rest()
	if rest == "" || (rest[0] != '`' && rest[0] != '~') {
		return 0
	}
	n := 0
	for n < len(rest) && rest[n] == rest[0] {
		n++
	}
	if n < 3 || (rest[0] == '`' && strings.IndexByte(rest[n:], '`') >= 0) {
		return 0
	}
	p.closeUnmatched()
	c := p.addChild(kCode)
	c.fenced = true
	c.fenceChar = rest[0]
	c.fenceLen = n
	c.fenceOffset = p.indent
	p.advanceNextNonspace()
	p.advanceOffset(n, false)
	return 2
}

func startHTMLBlock(p *blockParser, container *bnode) int {
	if p.indented || p.peekAt(p.nextNonspace) != '<' {
		return 0
	}
	rest := p.rest()
	canType7 := container.kind != kPara && (p.allClosed || p.blank || p.tip.kind != kPara)
	t := htmlBlockType(rest, canType7)
	if t == 0 {
		return 0
	}
	p.closeUnmatched()
	b := p.addChild(kHTML)
	b.htmlType = t
	return 2
}

func startSetextHeading(p *blockParser, container *bnode) int {
	if p.indented || container.kind != kPara {
		return 0
	}
	rest := strings.TrimRight(p.rest(), " \t")
	if rest == "" || (rest[0] != '=' && rest[0] != '-') || strings.Trim(rest, rest[:1]) != "" {
		return 0
	}
	p.closeUnmatched()
	p.extractRefs(container)
	if len(container.lines) == 0 {
		// Only reference definitions: the paragraph disappears and the underline is ordinary text.
		return 0
	}
	container.kind = kHeading
	container.level = 1
	if rest[0] == '-' {
		container.level = 2
	}
	container.end = p.lineNo + 1
	p.tip = container
	p.advanceOffset(len(p.line)-p.offset, false)
	return 2
}

func startThematicBreak(p *blockParser, _ *bnode) int {
	if p.indented || !isThematicBreak(p.rest()) {
		return 0
	}
	p.closeUnmatched()
	p.addChild(kRule)
	p.advanceOffset(len(p.line)-p.offset, false)
	return 2
}

func isThematicBreak(s string) bool {
	if s == "" || (s[0] != '*' && s[0] != '-' && s[0] != '_') {
		return false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == s[0]:
			n++
		case isSpaceOrTab(s[i]):
		default:
			return false
		}
	}
	return n >= 3
}

func startListItem(p *blockParser, container *bnode) int {
	if (p.indented && container.kind != kList) || container.depth >= maxDepth {
		return 0
	}
	data, ok := p.parseListMarker(container)
	if !ok {
		return 0
	}
	p.closeUnmatched()
	if p.tip.kind != kList || !listsMatch(p.tip, data) {
		l := p.addChild(kList)
		l.ordered = data.ordered
		l.startNum = data.startNum
		l.marker = data.marker
	}
	item := p.addChild(kItem)
	item.ordered = data.ordered
	item.marker = data.marker
	item.padding = data.padding
	item.markerOffset = data.markerOffset
	return 1
}

func listsMatch(list *bnode, d *bnode) bool {
	return list.ordered == d.ordered && list.marker == d.marker
}

// parseListMarker parses a list marker at the next non-space character. On success it consumes the marker and the
// spaces after it and returns the data of the item.
func (p *blockParser) parseListMarker(container *bnode) (*bnode, bool) {
	if p.indent >= codeIndent {
		return nil, false
	}
	rest := p.rest()
	if rest == "" {
		return nil, false
	}
	d := &bnode{markerOffset: p.indent}
	mlen := 0
	switch c := rest[0]; {
	case c == '*' || c == '+' || c == '-':
		d.marker = c
		mlen = 1
	case c >= '0' && c <= '9':
		i := 0
		num := 0
		for i < len(rest) && i < 9 && rest[i] >= '0' && rest[i] <= '9' {
			num = num*10 + int(rest[i]-'0')
			i++
		}
		if i >= len(rest) || (rest[i] != '.' && rest[i] != ')') {
			return nil, false
		}
		if container.kind == kPara && num != 1 {
			return nil, false
		}
		d.ordered = true
		d.startNum = num
		d.marker = rest[i]
		mlen = i + 1
	default:
		return nil, false
	}
	if mlen < len(rest) && !isSpaceOrTab(rest[mlen]) {
		return nil, false
	}
	if container.kind == kPara && isBlankString(rest[mlen:]) {
		return nil, false
	}
	p.advanceNextNonspace()
	p.advanceOffset(mlen, true)
	spacesStartCol := p.column
	spacesStartOffset := p.offset
	for {
		p.advanceOffset(1, true)
		if p.column-spacesStartCol >= 5 || !isSpaceOrTab(p.peekAt(p.offset)) {
			break
		}
	}
	blankItem := p.offset >= len(p.line)
	spaces := p.column - spacesStartCol
	if spaces >= 5 || spaces < 1 || blankItem {
		d.padding = mlen + 1
		p.column = spacesStartCol
		p.offset = spacesStartOffset
		p.partialTab = false
		if isSpaceOrTab(p.peekAt(p.offset)) {
			p.advanceOffset(1, true)
		}
	} else {
		d.padding = mlen + spaces
	}
	return d, true
}

func startIndentedCode(p *blockParser, _ *bnode) int {
	if p.indented && p.tip.kind != kPara && !p.blank {
		p.advanceOffset(codeIndent, true)
		p.closeUnmatched()
		p.addChild(kCode)
		return 2
	}
	return 0
}

// --- HTML blocks ---

// htmlBlockTags are the tag names that start an HTML block of kind 6 (CommonMark 0.31).
var htmlBlockTags = map[string]struct{}{
	"address": {}, "article": {}, "aside": {}, "base": {}, "basefont": {}, "blockquote": {}, "body": {}, "caption": {},
	"center": {}, "col": {}, "colgroup": {}, "dd": {}, "details": {}, "dialog": {}, "dir": {}, "div": {}, "dl": {},
	"dt": {}, "fieldset": {}, "figcaption": {}, "figure": {}, "footer": {}, "form": {}, "frame": {}, "frameset": {},
	"h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {}, "head": {}, "header": {}, "hr": {}, "html": {},
	"iframe": {}, "legend": {}, "li": {}, "link": {}, "main": {}, "menu": {}, "menuitem": {}, "nav": {}, "noframes": {},
	"ol": {}, "optgroup": {}, "option": {}, "p": {}, "param": {}, "search": {}, "section": {}, "summary": {},
	"table": {}, "tbody": {}, "td": {}, "tfoot": {}, "th": {}, "thead": {}, "title": {}, "tr": {}, "track": {}, "ul": {},
}

// htmlBlockType returns the kind (1 to 7) of the HTML block that starts with s, or 0.
func htmlBlockType(s string, canType7 bool) int {
	if s == "" || s[0] != '<' {
		return 0
	}
	for _, name := range [...]string{"script", "pre", "style", "textarea"} {
		if hasPrefixFold(s[1:], name) {
			after := s[1+len(name):]
			if after == "" || after[0] == ' ' || after[0] == '\t' || after[0] == '>' {
				return 1
			}
		}
	}
	switch {
	case strings.HasPrefix(s, "<!--"):
		return 2
	case strings.HasPrefix(s, "<?"):
		return 3
	case strings.HasPrefix(s, "<![CDATA["):
		return 5
	case len(s) > 2 && s[1] == '!' && isASCIILetter(s[2]):
		return 4
	}
	// kind 6
	i := 1
	if i < len(s) && s[i] == '/' {
		i++
	}
	j := i
	for j < len(s) && (isASCIILetter(s[j]) || (j > i && s[j] >= '0' && s[j] <= '9')) {
		j++
	}
	if j > i {
		if _, ok := htmlBlockTags[strings.ToLower(s[i:j])]; ok {
			after := s[j:]
			if after == "" || after[0] == ' ' || after[0] == '\t' || after[0] == '>' || strings.HasPrefix(after, "/>") {
				return 6
			}
		}
	}
	if canType7 {
		end := scanOpenTag(s, 0)
		if end < 0 {
			end = scanCloseTag(s, 0)
		}
		if end > 0 && isBlankString(s[end:]) {
			return 7
		}
	}
	return 0
}

// htmlBlockEnds reports whether the line contains the end condition of an HTML block of kind 1 to 5.
func htmlBlockEnds(kind int, line string) bool {
	switch kind {
	case 1:
		l := strings.ToLower(line)
		return strings.Contains(l, "</script>") || strings.Contains(l, "</pre>") ||
			strings.Contains(l, "</style>") || strings.Contains(l, "</textarea>")
	case 2:
		return strings.Contains(line, "-->")
	case 3:
		return strings.Contains(line, "?>")
	case 4:
		return strings.Contains(line, ">")
	case 5:
		return strings.Contains(line, "]]>")
	}
	return false
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// --- conversion of the block tree into the public AST ---

type converter struct {
	refs map[string]refDef
}

func (c *converter) blocks(nodes []*bnode) []Block {
	var out []Block
	for _, n := range nodes {
		out = c.appendBlock(out, n)
	}
	return out
}

func (c *converter) appendBlock(out []Block, n *bnode) []Block {
	pos := Span{StartLine: n.start, EndLine: n.end}
	switch n.kind {
	case kQuote:
		return append(out, Quote{Blocks: c.blocks(n.children), Pos: pos})
	case kList:
		l := List{Ordered: n.ordered, Tight: n.tight, Pos: pos}
		if n.ordered {
			l.Start = n.startNum
		}
		for _, it := range n.children {
			l.Items = append(l.Items, c.item(it))
		}
		return append(out, l)
	case kHeading:
		return append(out, Heading{Level: n.level, Inlines: c.inlines(strings.Join(n.lines, "\n")), Pos: pos})
	case kRule:
		return append(out, Rule{Pos: pos})
	case kHTML:
		return append(out, HTMLBlock{Text: strings.Join(n.lines, "\n"), Pos: pos})
	case kCode:
		return append(out, codeBlock(n, pos))
	case kPara:
		return c.appendParagraph(out, n)
	}
	return out
}

func codeBlock(n *bnode, pos Span) CodeBlock {
	if !n.fenced {
		return CodeBlock{Text: strings.Join(n.lines, "\n"), Pos: pos}
	}
	info := ""
	var body []string
	if len(n.lines) > 0 {
		info = unescapeString(strings.TrimSpace(n.lines[0]))
		body = n.lines[1:]
	}
	if f := strings.Fields(info); len(f) > 0 {
		info = f[0]
	} else {
		info = ""
	}
	return CodeBlock{Info: info, Text: strings.Join(body, "\n"), Fenced: true, Pos: pos}
}

func (c *converter) item(n *bnode) ListItem {
	it := ListItem{Pos: Span{StartLine: n.start, EndLine: n.end}}
	if len(n.children) > 0 && n.children[0].kind == kPara && len(n.children[0].lines) > 0 {
		para := n.children[0]
		l0 := para.lines[0]
		if len(l0) > 3 && l0[0] == '[' && l0[2] == ']' && (l0[1] == ' ' || l0[1] == 'x' || l0[1] == 'X') &&
			isSpaceOrTab(l0[3]) && (len(para.lines) > 1 || strings.TrimSpace(l0[3:]) != "") {
			it.Task = true
			it.Checked = l0[1] != ' '
			para.lines[0] = strings.TrimLeft(l0[3:], " \t")
		}
	}
	it.Blocks = c.blocks(n.children)
	return it
}

func (c *converter) inlines(s string) []Inline {
	return parseInlines(strings.TrimSpace(s), c.refs)
}

// appendParagraph converts a paragraph. A GFM table whose header row is directly above its delimiter row splits the
// paragraph: the lines above the header stay a paragraph.
func (c *converter) appendParagraph(out []Block, n *bnode) []Block {
	lines := n.lines
	for i := 0; i+1 < len(lines); i++ {
		aligns, ok := tableDelimiter(lines[i+1])
		if !ok {
			continue
		}
		header := splitTableRow(lines[i])
		if len(header) != len(aligns) {
			continue
		}
		if i > 0 {
			out = append(out, Paragraph{
				Inlines: c.inlines(strings.Join(lines[:i], "\n")),
				Pos:     Span{StartLine: n.start, EndLine: n.start + i},
			})
		}
		t := Table{Align: aligns, Header: c.cells(header, len(aligns)),
			Pos: Span{StartLine: n.start + i, EndLine: n.end}}
		for _, row := range lines[i+2:] {
			t.Rows = append(t.Rows, c.cells(splitTableRow(row), len(aligns)))
		}
		return append(out, t)
	}
	return append(out, Paragraph{Inlines: c.inlines(strings.Join(lines, "\n")), Pos: Span{StartLine: n.start, EndLine: n.end}})
}

func (c *converter) cells(raw []string, n int) []TableCell {
	cells := make([]TableCell, n)
	for i := 0; i < n && i < len(raw); i++ {
		cells[i] = TableCell{Inlines: c.inlines(raw[i])}
	}
	return cells
}

// tableDelimiter parses a GFM delimiter row ("| :--- | ---: |").
func tableDelimiter(line string) ([]Align, bool) {
	if !strings.Contains(line, "|") && !strings.Contains(line, ":") {
		return nil, false
	}
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '|', ':', '-', ' ', '\t':
		default:
			return nil, false
		}
	}
	cells := splitTableRow(line)
	if len(cells) == 0 {
		return nil, false
	}
	aligns := make([]Align, len(cells))
	for i, cell := range cells {
		cell = strings.TrimSpace(cell)
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":") && len(cell) > 1
		core := strings.TrimSuffix(strings.TrimPrefix(cell, ":"), ":")
		if core == "" || strings.Trim(core, "-") != "" {
			return nil, false
		}
		switch {
		case left && right:
			aligns[i] = AlignCenter
		case left:
			aligns[i] = AlignLeft
		case right:
			aligns[i] = AlignRight
		}
	}
	return aligns, true
}

// splitTableRow splits a table row into the raw text of its cells. A leading and a trailing pipe are optional, and
// `\|` is a pipe inside a cell.
func splitTableRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	var cells []string
	var cur strings.Builder
	endedWithPipe := false
	for i := 0; i < len(s); i++ {
		endedWithPipe = false
		switch c := s[i]; c {
		case '\\':
			if i+1 < len(s) {
				if s[i+1] == '|' {
					cur.WriteByte('|')
				} else {
					cur.WriteByte('\\')
					cur.WriteByte(s[i+1])
				}
				i++
			} else {
				cur.WriteByte('\\')
			}
		case '|':
			cells = append(cells, cur.String())
			cur.Reset()
			endedWithPipe = true
		default:
			cur.WriteByte(c)
		}
	}
	if !endedWithPipe {
		cells = append(cells, cur.String())
	}
	return cells
}
