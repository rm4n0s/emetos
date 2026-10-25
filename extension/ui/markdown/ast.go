package markdown

import "strings"

// Span is the range of source lines (0-based, EndLine exclusive) a block was parsed from.
//
// Lines are counted after splitting the source on "\n", "\r\n" or a lone "\r". The lines of blocks nested in
// containers (list items, block quotes) are in the coordinates of the original source, not relative to the container.
type Span struct{ StartLine, EndLine int }

// Block is a block-level node of a parsed document. The concrete types are Heading, Paragraph, CodeBlock, Quote, List,
// Table, Rule and HTMLBlock; they are plain values (not pointers) so a type switch uses `case Heading:`.
type Block interface {
	// Span is the range of source lines the block was parsed from.
	Span() Span
	isBlock()
}

// Inline is an inline node: Text, Emph, Strong, Strike, Code, Link, Image, SoftBreak, HardBreak or HTMLInline. They are
// plain values (not pointers).
type Inline interface {
	isInline()
}

// Heading is an ATX (#) or Setext (=== / ---) heading.
type Heading struct {
	// Level is 1 to 6.
	Level int
	// Inlines is the content of the heading.
	Inlines []Inline
	// Pos is the source range of the heading.
	Pos Span
}

// Paragraph is a run of text lines.
type Paragraph struct {
	// Inlines is the content of the paragraph.
	Inlines []Inline
	// Pos is the source range of the paragraph.
	Pos Span
}

// CodeBlock is a fenced (``` or ~~~) or indented code block.
type CodeBlock struct {
	// Info is the first word of the info string of a fenced block, the language. It is empty for indented blocks.
	Info string
	// Text is the code, without the trailing newline.
	Text string
	// Fenced is true for a fenced block and false for an indented one.
	Fenced bool
	// Pos is the source range of the block, including the fences.
	Pos Span
}

// Quote is a block quote.
type Quote struct {
	// Blocks is the content of the quote.
	Blocks []Block
	// Pos is the source range of the quote.
	Pos Span
}

// List is a bullet or ordered list.
type List struct {
	// Ordered is true for a list numbered with "1." or "1)".
	Ordered bool
	// Start is the number of the first item of an ordered list. It is 0 for a bullet list.
	Start int
	// Tight is true when no item is separated from the next by a blank line and no item holds blank-separated blocks.
	Tight bool
	// Items are the items of the list.
	Items []ListItem
	// Pos is the source range of the list.
	Pos Span
}

// ListItem is an item of a List.
type ListItem struct {
	// Blocks is the content of the item.
	Blocks []Block
	// Task is true for "- [ ] x" and "- [x] x"; the marker is removed from the text.
	Task bool
	// Checked is true for "- [x] x". It is only meaningful when Task is true.
	Checked bool
	// Pos is the source range of the item.
	Pos Span
}

// Align is the alignment of a table column.
type Align int

// The alignments of a table column.
const (
	// AlignNone is a column without an alignment colon.
	AlignNone Align = iota
	// AlignLeft is a column marked ":---".
	AlignLeft
	// AlignCenter is a column marked ":---:".
	AlignCenter
	// AlignRight is a column marked "---:".
	AlignRight
)

// Table is a GitHub-flavored table.
type Table struct {
	// Align is the alignment of each column.
	Align []Align
	// Header is the header row. It has len(Align) cells.
	Header []TableCell
	// Rows are the body rows. Each has len(Align) cells: missing cells are empty and extra cells are dropped.
	Rows [][]TableCell
	// Pos is the source range of the table.
	Pos Span
}

// TableCell is a cell of a Table.
type TableCell struct {
	// Inlines is the content of the cell.
	Inlines []Inline
}

// Rule is a thematic break (---, *** or ___).
type Rule struct {
	// Pos is the source range of the rule.
	Pos Span
}

// HTMLBlock is a raw HTML block. The text is kept as it is; a renderer shows it as plain text.
type HTMLBlock struct {
	// Text is the raw HTML, without the trailing newline.
	Text string
	// Pos is the source range of the block.
	Pos Span
}

// Span returns the source range of the heading.
func (b Heading) Span() Span { return b.Pos }

// Span returns the source range of the paragraph.
func (b Paragraph) Span() Span { return b.Pos }

// Span returns the source range of the code block.
func (b CodeBlock) Span() Span { return b.Pos }

// Span returns the source range of the quote.
func (b Quote) Span() Span { return b.Pos }

// Span returns the source range of the list.
func (b List) Span() Span { return b.Pos }

// Span returns the source range of the table.
func (b Table) Span() Span { return b.Pos }

// Span returns the source range of the rule.
func (b Rule) Span() Span { return b.Pos }

// Span returns the source range of the HTML block.
func (b HTMLBlock) Span() Span { return b.Pos }

func (Heading) isBlock()   {}
func (Paragraph) isBlock() {}
func (CodeBlock) isBlock() {}
func (Quote) isBlock()     {}
func (List) isBlock()      {}
func (Table) isBlock()     {}
func (Rule) isBlock()      {}
func (HTMLBlock) isBlock() {}

// Text is plain text. Backslash escapes and character references are already decoded.
type Text struct{ S string }

// Emph is emphasis (* or _).
type Emph struct{ Children []Inline }

// Strong is strong emphasis (** or __).
type Strong struct{ Children []Inline }

// Strike is strikethrough (~~x~~).
type Strike struct{ Children []Inline }

// Code is a code span.
type Code struct{ S string }

// Link is a link: an inline link, a reference link, an autolink or a bare URL.
type Link struct {
	// Dest is the destination URL.
	Dest string
	// Title is the optional title; empty when there is none.
	Title string
	// Children is the text of the link.
	Children []Inline
}

// Image is an image.
type Image struct {
	// Dest is the URL of the image.
	Dest string
	// Title is the optional title; empty when there is none.
	Title string
	// Alt is the plain-text content of the alt text.
	Alt string
}

// SoftBreak is a line ending inside a paragraph.
type SoftBreak struct{}

// HardBreak is a forced line break: two trailing spaces or a backslash before the newline.
type HardBreak struct{}

// HTMLInline is a raw inline HTML tag, kept as text.
type HTMLInline struct{ S string }

func (Text) isInline()       {}
func (Emph) isInline()       {}
func (Strong) isInline()     {}
func (Strike) isInline()     {}
func (Code) isInline()       {}
func (Link) isInline()       {}
func (Image) isInline()      {}
func (SoftBreak) isInline()  {}
func (HardBreak) isInline()  {}
func (HTMLInline) isInline() {}

// PlainText is the text of inlines without markup (for alt text, ids and tests). Soft breaks become a space and hard
// breaks a newline; raw HTML tags are dropped.
func PlainText(in []Inline) string {
	var sb strings.Builder
	writePlain(&sb, in)
	return sb.String()
}

func writePlain(sb *strings.Builder, in []Inline) {
	for _, n := range in {
		switch v := n.(type) {
		case Text:
			sb.WriteString(v.S)
		case Emph:
			writePlain(sb, v.Children)
		case Strong:
			writePlain(sb, v.Children)
		case Strike:
			writePlain(sb, v.Children)
		case Code:
			sb.WriteString(v.S)
		case Link:
			writePlain(sb, v.Children)
		case Image:
			sb.WriteString(v.Alt)
		case SoftBreak:
			sb.WriteByte(' ')
		case HardBreak:
			sb.WriteByte('\n')
		}
	}
}
