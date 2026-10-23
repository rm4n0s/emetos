package regex

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// keepGroupName is the name of the empty group that marks where \K was.
const keepGroupName = "__keep"

// maxTranslatedLen is how much longer than the pattern itself the translation may be, in
// characters, before the expansion of group calls is made shallower.
const maxTranslatedLen = 1 << 18

// maxCallDepth is how many times a group may be called inside itself: regexp2 has no recursion,
// so a call is replaced by a copy of the group, and the deepest copy never matches.
const maxCallDepth = 6

// emitter writes the parsed pattern in regexp2's syntax.
type emitter struct {
	p         *parser
	root      node
	sb        strings.Builder
	depth     map[int]int // how many calls of each group are open
	maxDepth  int
	expanding int // above 0 while a called group is written: its groups do not capture
	disableA  bool
	disableG  bool
	limit     int  // the longest output allowed
	over      bool // the output is longer than allowed
}

// translateOnig writes the pattern of root in regexp2's syntax. disableA and disableG make \A
// and \G never match (the tokenizer asks for that when the position is not the start of the
// text or the anchor).
func translateOnig(p *parser, root node, disableA, disableG bool) (string, error) {
	depths := []int{0}
	if p.calls > 0 {
		depths = depths[:0]
		for d := maxCallDepth; d >= 1; d-- {
			depths = append(depths, d)
		}
	}
	for _, d := range depths {
		e := &emitter{p: p, root: root, depth: map[int]int{}, maxDepth: d, disableA: disableA, disableG: disableG, limit: maxTranslatedLen + 2*len(p.src)}
		e.emit(root)
		if !e.over {
			return e.sb.String(), nil
		}
	}
	return "", errors.New("RegexTooLarge", "the pattern with its group calls written out is too long", "pattern", string(p.src), "calls", p.calls)
}

// write appends text, and notes when the output is too long.
func (e *emitter) write(s string) {
	if e.over {
		return
	}
	e.sb.WriteString(s)
	if e.sb.Len() > e.limit {
		e.over = true
	}
}

// emit writes n.
func (e *emitter) emit(n node) {
	if e.over {
		return
	}
	switch n := n.(type) {
	case *litNode:
		e.write(patternLiteral(n.r))
	case *dotNode:
		if n.all {
			e.write(anyChar)
		} else {
			e.write(`[^\n]`)
		}
	case *anchorNode:
		e.anchor(n)
	case *setNode:
		e.write(n.set.expr())
	case *rawNode:
		e.write(n.text)
	case *seqNode:
		for _, it := range n.items {
			e.emit(it)
		}
	case *altNode:
		for i, b := range n.branches {
			if i > 0 {
				e.write("|")
			}
			e.emit(b)
		}
	case *keepNode:
		if e.expanding == 0 {
			e.write("(?<" + keepGroupName + ">)")
		}
	case *absentNode:
		// The text that does not hold the pattern, written as: any character that does not
		// start it. (It differs from Oniguruma's operator only for text that ends inside a
		// match of the pattern.)
		e.write("(?:(?!")
		e.emit(n.body)
		e.write(")" + anyChar + ")*")
	case *flagNode:
		e.write(icGroup(n.ic))
		e.emit(n.body)
		e.write(")")
	case *condNode:
		e.write("(?(" + strconv.Itoa(n.ref) + ")")
		e.emit(n.yes)
		if n.no != nil {
			e.write("|")
			e.emit(n.no)
		}
		e.write(")")
	case *quantNode:
		e.quant(n)
	case *backrefNode:
		e.backref(n)
	case *callNode:
		e.call(n)
	case *groupNode:
		e.group(n)
	}
}

// icGroup opens a group with the ignore-case flag on or off.
func icGroup(ic bool) string {
	if ic {
		return "(?i:"
	}
	return "(?-i:"
}

// anchor writes an anchor.
func (e *emitter) anchor(n *anchorNode) {
	switch n.kind {
	case 'A':
		if e.disableA {
			e.write("(?!)")
			return
		}
		e.write(`\A`)
	case 'G':
		if e.disableG {
			e.write("(?!)")
			return
		}
		e.write(`\G`)
	case '^':
		// The start of a line is the start of the text, or after a line break that is not the
		// last character: Oniguruma has no empty line after the final one.
		e.write(`(?:\A|^(?!\z))`)
	case '$':
		e.write("$")
	default:
		e.write(`\` + string(n.kind))
	}
}

// quant writes a repetition.
func (e *emitter) quant(n *quantNode) {
	atomic := n.mode == possessive
	if atomic {
		e.write("(?>")
	}
	_, nested := n.body.(*quantNode)
	if nested {
		e.write("(?:")
	}
	e.emit(n.body)
	if nested {
		e.write(")")
	}
	switch {
	case n.min == 0 && n.max == -1:
		e.write("*")
	case n.min == 1 && n.max == -1:
		e.write("+")
	case n.min == 0 && n.max == 1:
		e.write("?")
	case n.max == -1:
		e.write("{" + strconv.Itoa(n.min) + ",}")
	case n.min == n.max:
		e.write("{" + strconv.Itoa(n.min) + "}")
	default:
		e.write("{" + strconv.Itoa(n.min) + "," + strconv.Itoa(n.max) + "}")
	}
	if n.mode == lazy {
		e.write("?")
	}
	if atomic {
		e.write(")")
	}
}

// backref writes a reference to a group.
func (e *emitter) backref(n *backrefNode) {
	if n.digits != "" && n.index > e.p.groups {
		// \101 with fewer groups is the octal escape.
		v, _ := strconv.ParseUint(n.digits, 8, 32)
		e.write(patternLiteral(rune(v)))
		return
	}
	e.write(`\k<` + strconv.Itoa(n.index) + `>`)
}

// group writes a group. A group of a called copy does not capture.
func (e *emitter) group(n *groupNode) {
	switch n.kind {
	case capturing:
		if e.expanding > 0 {
			e.write("(?:")
		} else {
			e.write("(")
		}
	case nonCapturing:
		e.write("(?:")
	case atomic:
		e.write("(?>")
	case lookahead:
		e.write("(?=")
	case negLookahead:
		e.write("(?!")
	case lookbehind:
		e.write("(?<=")
	case negLookbehind:
		e.write("(?<!")
	}
	e.emit(n.body)
	e.write(")")
}

// call writes a copy of the group that is called, or a pattern that never matches when the group
// is already called as deep as it may be.
func (e *emitter) call(n *callNode) {
	if e.depth[n.target] >= e.maxDepth {
		e.write("(?!)")
		return
	}
	body, ic := e.root, false
	if n.target != 0 {
		g := e.p.defs[n.target]
		body, ic = g.body, g.ic
	}
	e.depth[n.target]++
	e.expanding++
	e.write(icGroup(ic))
	e.emit(body)
	e.write(")")
	e.expanding--
	e.depth[n.target]--
}
