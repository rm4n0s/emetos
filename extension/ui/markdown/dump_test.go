package markdown

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// The tests compare the AST with a compact S-expression:
//
//	(h1 "Title")                       heading of level 1
//	(p "a " (em "b"))                  paragraph with emphasis
//	(quote BLOCK...)                   block quote
//	(ul tight|loose ITEM...)           bullet list; (ol N tight|loose ITEM...) for an ordered list
//	(li BLOCK...)                      list item; (li [ ] ...) and (li [x] ...) for task items
//	(fence "lang" "text")              fenced code; (indent "text") for indented code
//	(hr)  (html "text")                rule, HTML block
//	(table "lcr-" (head (c ..) ..) (row (c ..) ..))
//	inlines: "text" (em ..) (strong ..) (del ..) (code "x") (a "dest" [(title "t")] ..) (img "dest" "alt" [(title "t")])
//	         (sb) soft break, (br) hard break, (raw "<b>") inline HTML

func dumpBlocks(bs []Block) string {
	parts := make([]string, len(bs))
	for i, b := range bs {
		parts[i] = dumpBlock(b)
	}
	return strings.Join(parts, " ")
}

func dumpBlock(b Block) string {
	switch v := b.(type) {
	case Heading:
		return wrap("h"+strconv.Itoa(v.Level), dumpInlines(v.Inlines))
	case Paragraph:
		return wrap("p", dumpInlines(v.Inlines))
	case CodeBlock:
		if v.Fenced {
			return "(fence " + strconv.Quote(v.Info) + " " + strconv.Quote(v.Text) + ")"
		}
		return "(indent " + strconv.Quote(v.Text) + ")"
	case Quote:
		return wrap("quote", dumpBlocks(v.Blocks))
	case List:
		head := "ul"
		if v.Ordered {
			head = "ol " + strconv.Itoa(v.Start)
		}
		if v.Tight {
			head += " tight"
		} else {
			head += " loose"
		}
		items := make([]string, len(v.Items))
		for i, it := range v.Items {
			h := "li"
			if it.Task {
				h += " [ ]"
				if it.Checked {
					h = "li [x]"
				}
			}
			items[i] = wrap(h, dumpBlocks(it.Blocks))
		}
		return wrap(head, strings.Join(items, " "))
	case Table:
		var al strings.Builder
		for _, a := range v.Align {
			al.WriteByte("-lcr"[a])
		}
		parts := []string{strconv.Quote(al.String()), dumpRow("head", v.Header)}
		for _, r := range v.Rows {
			parts = append(parts, dumpRow("row", r))
		}
		return wrap("table", strings.Join(parts, " "))
	case Rule:
		return "(hr)"
	case HTMLBlock:
		return "(html " + strconv.Quote(v.Text) + ")"
	}
	return fmt.Sprintf("(?%T)", b)
}

func dumpRow(name string, cells []TableCell) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		parts[i] = wrap("c", dumpInlines(c.Inlines))
	}
	return wrap(name, strings.Join(parts, " "))
}

func wrap(head, body string) string {
	if body == "" {
		return "(" + head + ")"
	}
	return "(" + head + " " + body + ")"
}

func dumpInlines(in []Inline) string {
	parts := make([]string, len(in))
	for i, n := range in {
		parts[i] = dumpInline(n)
	}
	return strings.Join(parts, " ")
}

func dumpInline(n Inline) string {
	switch v := n.(type) {
	case Text:
		return strconv.Quote(v.S)
	case Emph:
		return wrap("em", dumpInlines(v.Children))
	case Strong:
		return wrap("strong", dumpInlines(v.Children))
	case Strike:
		return wrap("del", dumpInlines(v.Children))
	case Code:
		return "(code " + strconv.Quote(v.S) + ")"
	case Link:
		head := "a " + strconv.Quote(v.Dest)
		if v.Title != "" {
			head += " (title " + strconv.Quote(v.Title) + ")"
		}
		return wrap(head, dumpInlines(v.Children))
	case Image:
		s := "(img " + strconv.Quote(v.Dest) + " " + strconv.Quote(v.Alt)
		if v.Title != "" {
			s += " (title " + strconv.Quote(v.Title) + ")"
		}
		return s + ")"
	case SoftBreak:
		return "(sb)"
	case HardBreak:
		return "(br)"
	case HTMLInline:
		return "(raw " + strconv.Quote(v.S) + ")"
	}
	return fmt.Sprintf("(?%T)", n)
}

// dumpSpans prints the source lines of the blocks: "p[0,1] h1[2,3]", containers as "quote[0,2]{p[0,1]}".
func dumpSpans(bs []Block) string {
	parts := make([]string, len(bs))
	for i, b := range bs {
		s := b.Span()
		name := ""
		var kids []string
		switch v := b.(type) {
		case Heading:
			name = "h" + strconv.Itoa(v.Level)
		case Paragraph:
			name = "p"
		case CodeBlock:
			name = "code"
		case Quote:
			name = "quote"
			kids = append(kids, dumpSpans(v.Blocks))
		case List:
			name = "list"
			for _, it := range v.Items {
				kids = append(kids, fmt.Sprintf("li[%d,%d]{%s}", it.Pos.StartLine, it.Pos.EndLine, dumpSpans(it.Blocks)))
			}
		case Table:
			name = "table"
		case Rule:
			name = "hr"
		case HTMLBlock:
			name = "html"
		}
		parts[i] = fmt.Sprintf("%s[%d,%d]", name, s.StartLine, s.EndLine)
		if len(kids) > 0 {
			parts[i] += "{" + strings.Join(kids, " ") + "}"
		}
	}
	return strings.Join(parts, " ")
}

// runCases runs table cases of source and expected block dump.
func runCases(t *testing.T, cases []struct{ name, src, want string }) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dumpBlocks(parse(tc.src))
			if got != tc.want {
				t.Errorf("src %q\n got: %s\nwant: %s", tc.src, got, tc.want)
			}
		})
	}
}
