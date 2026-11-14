package book

import (
	"encoding/xml"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// BlockKind is the kind of a block of a chapter.
type BlockKind int

// The kinds of blocks.
const (
	// Paragraph is text: a <p>, or the text and inline elements directly in a container.
	Paragraph BlockKind = iota
	// Heading is an <h1> to <h6>; Level is its number.
	Heading
	// Quote is a <blockquote>; Children are its blocks.
	Quote
	// List is a <ul> or an <ol>; Items are the blocks of each <li>, Ordered tells an <ol> and Start its first number.
	List
	// Pre is preformatted text (<pre>), in Text.
	Pre
	// Rule is an <hr>.
	Rule
	// Table is a <table>, as a plain grid of the cells' text in Rows.
	Table
	// Image is an <img>, or an <image> inside an <svg>.
	Image
)

// Align is the alignment of a block's own style.
type Align int

// The alignments.
const (
	AlignStart Align = iota
	AlignCenter
	AlignRight
)

// Style is the style of an inline run, as flags.
type Style int

// The styles of inline runs.
const (
	Emphasis Style = 1 << iota
	Strong
	Code
	Sub
	Sup
)

// Inline is a run of text with one style and link. A link inside the book is a path in the zip with an optional "#anchor" (the
// chapter's own path for an anchor of the same chapter); a link to the web is its URL, with External set. A line break is a run of
// "\n".
type Inline struct {
	Text     string
	Style    Style
	Link     string
	External bool
}

// ImageRef is an image of the book: its path in the zip and its text.
type ImageRef struct {
	Path string
	Alt  string
}

// Block is a block of a chapter. IDs are the ids of the elements in it, which links point at.
type Block struct {
	Kind     BlockKind
	Level    int
	Align    Align
	Inlines  []Inline
	Children []Block
	Items    [][]Block
	Ordered  bool
	Start    int
	Text     string
	Rows     [][]string
	Image    ImageRef
	IDs      []string
}

// Chapter is a spine item parsed: its blocks, where its anchors are (the index of the top block that holds each id), and its notes
// (the bodies of <aside epub:type="footnote">, by id, shown only through their links).
type Chapter struct {
	Path    string
	Title   string
	Blocks  []Block
	Anchors map[string]int
	Notes   map[string][]Block
}

// Chapter reads and parses the spine item i. It fails with EPUBChapterMissing for an index out of the spine, EPUBChapterUnreadable
// when it cannot be read and EPUBChapterInvalid when it is not XHTML at all.
func (b *Book) Chapter(i int) (*Chapter, error) {
	if i < 0 || i >= len(b.Spine) {
		return nil, errors.New("EPUBChapterMissing", "This book has no chapter "+strconv.Itoa(i+1)+".", "index", i, "chapters", len(b.Spine))
	}
	p := b.Spine[i].Path
	data, err := readEntry(b.entries[p])
	if err != nil {
		return nil, errors.NewErr("EPUBChapterUnreadable", err).SetMetadata("chapter", p)
	}
	ch, err := parseChapter(data, p)
	if err != nil {
		return nil, errors.NewErr("EPUBChapterInvalid", err).SetMetadata("chapter", p)
	}
	return ch, nil
}

// parseChapter parses a chapter's XHTML, at its path in the zip.
func parseChapter(data []byte, chapterPath string) (*Chapter, error) {
	p := &parser{d: lenientDecoder(data), dir: path.Dir(chapterPath), self: chapterPath, notes: map[string][]Block{}}
	ch := &Chapter{Path: chapterPath, Anchors: map[string]int{}, Notes: p.notes}
	for {
		tok, err := p.d.Token()
		if err == io.EOF {
			return ch, nil // no body: an empty chapter
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "title":
			var t strings.Builder
			if err := readText(p.d, &t); err != nil {
				return nil, err
			}
			ch.Title = collapse(t.String())
		case "body":
			blocks, err := p.blocks("body", AlignStart)
			if err != nil && !cutShort(err) {
				return nil, err
			}
			ch.Blocks = blocks
			for i, b := range blocks {
				collectIDs(b, i, ch.Anchors)
			}
			return ch, nil
		}
	}
}

// collectIDs maps the ids of a block and of the blocks in it to the top block's index.
func collectIDs(b Block, top int, anchors map[string]int) {
	for _, id := range b.IDs {
		if _, seen := anchors[id]; !seen {
			anchors[id] = top
		}
	}
	for _, c := range b.Children {
		collectIDs(c, top, anchors)
	}
	for _, item := range b.Items {
		for _, c := range item {
			collectIDs(c, top, anchors)
		}
	}
}

// cutShort tells whether a decoder's error is the end of a document that was cut short.
func cutShort(err error) bool {
	var se *xml.SyntaxError
	return err == io.EOF || (errors.As(err, &se) && se.Msg == "unexpected EOF")
}

// parser parses the XHTML of a chapter.
type parser struct {
	d     *xml.Decoder
	dir   string
	self  string
	notes map[string][]Block
}

// The elements by what they are to the parser.
var (
	containers = set("div", "section", "article", "main", "header", "footer", "nav", "figure", "figcaption", "center", "hgroup",
		"address", "details", "summary", "dl", "dd", "dt", "body", "html", "aside", "li", "td", "th", "form", "fieldset", "picture")
	skipped = set("head", "script", "style", "noscript", "template", "object", "embed", "iframe", "video", "audio", "math", "button",
		"input", "select", "textarea", "map", "canvas")
	blockStarts = set("p", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "ul", "ol", "pre", "hr", "table", "img", "svg")
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// hiddenElement tells whether an element is left out: the hidden attribute, display:none in its own style.
func hiddenElement(se xml.StartElement) bool {
	for _, a := range se.Attr {
		if a.Name.Local == "hidden" {
			return true
		}
	}
	return strings.Contains(strings.ReplaceAll(strings.ToLower(attr(se, "style")), " ", ""), "display:none")
}

// alignOf is the alignment of an element's own style, or the one it inherits.
func alignOf(se xml.StartElement, inherited Align) Align {
	style := strings.ReplaceAll(strings.ToLower(attr(se, "style")), " ", "")
	switch {
	case strings.Contains(style, "text-align:center"):
		return AlignCenter
	case strings.Contains(style, "text-align:right"):
		return AlignRight
	case strings.Contains(style, "text-align:left"), strings.Contains(style, "text-align:start"), strings.Contains(style, "text-align:justify"):
		return AlignStart
	case se.Name.Local == "center" || strings.EqualFold(attr(se, "align"), "center"):
		return AlignCenter
	}
	return inherited
}

// isNote tells whether an element is the body of a note.
func isNote(se xml.StartElement) bool {
	t := attr(se, "type")
	return se.Name.Local == "aside" && (hasWord(t, "footnote") || hasWord(t, "endnote") || hasWord(t, "rearnote"))
}

// blocks reads the blocks of a container whose start was read, through its end. The text and inline elements directly in it make
// paragraphs.
func (p *parser) blocks(end string, align Align) ([]Block, error) {
	var out []Block
	pending := &Block{Kind: Paragraph, Align: align}
	flush := func() {
		pending.Inlines = tidy(pending.Inlines)
		if len(pending.Inlines) > 0 {
			out = append(out, *pending)
		} else if len(pending.IDs) > 0 && len(out) > 0 { // ids with no text go to the block before
			out[len(out)-1].IDs = append(out[len(out)-1].IDs, pending.IDs...)
		} else if len(pending.IDs) > 0 {
			out = append(out, Block{Kind: Paragraph, Align: align, IDs: pending.IDs})
		}
		pending = &Block{Kind: Paragraph, Align: align}
	}
	for {
		tok, err := p.d.Token()
		if err != nil {
			flush()
			return out, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			pending.Inlines = append(pending.Inlines, Inline{Text: string(t)})
		case xml.EndElement:
			if t.Name.Local == end {
				flush()
				return out, nil
			}
		case xml.StartElement:
			name := t.Name.Local
			switch {
			case isNote(t):
				notes, err := p.blocks(name, AlignStart)
				if err != nil {
					return out, err
				}
				p.addNote(t, notes)
				continue
			case hiddenElement(t) || skipped[name]:
				if err := p.d.Skip(); err != nil {
					return out, err
				}
				continue
			}
			if !blockStarts[name] && !containers[name] {
				// an inline element in the container: part of the pending paragraph
				if id := attr(t, "id"); id != "" {
					pending.IDs = append(pending.IDs, id)
				}
				runs, ids, imgs, err := p.inlines(t, 0, "")
				pending.Inlines = append(pending.Inlines, runs...)
				pending.IDs = append(pending.IDs, ids...)
				if len(imgs) > 0 {
					flush()
					out = append(out, imgs...)
				}
				if err != nil {
					flush()
					return out, err
				}
				continue
			}
			flush()
			made, err := p.block(t, align)
			out = append(out, made...)
			if err != nil {
				return out, err
			}
		}
	}
}

// addNote keeps the blocks of a note by its id (or by the first id in it).
func (p *parser) addNote(se xml.StartElement, blocks []Block) {
	id := attr(se, "id")
	if id == "" {
		for _, b := range blocks {
			if len(b.IDs) > 0 {
				id = b.IDs[0]
				break
			}
		}
	}
	if id != "" {
		p.notes[id] = blocks
	}
}

// block reads a block element whose start was read, through its end.
func (p *parser) block(se xml.StartElement, inherited Align) ([]Block, error) {
	name := se.Name.Local
	align := alignOf(se, inherited)
	var ids []string
	if id := attr(se, "id"); id != "" {
		ids = append(ids, id)
	}
	withIDs := func(blocks []Block) []Block {
		if len(ids) == 0 {
			return blocks
		}
		if len(blocks) == 0 {
			return []Block{{Kind: Paragraph, Align: align, IDs: ids}}
		}
		blocks[0].IDs = append(ids, blocks[0].IDs...)
		return blocks
	}
	switch {
	case containers[name]:
		blocks, err := p.blocks(name, align)
		return withIDs(blocks), err
	case name == "p" || len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6':
		runs, inner, imgs, err := p.inlines(se, 0, "")
		b := Block{Kind: Paragraph, Align: align, Inlines: tidy(runs), IDs: append(ids, inner...)}
		if name != "p" {
			b.Kind, b.Level = Heading, int(name[1]-'0')
		}
		var out []Block
		if len(b.Inlines) > 0 || len(b.IDs) > 0 {
			out = append(out, b)
		}
		return append(out, imgs...), err
	case name == "blockquote":
		children, err := p.blocks(name, align)
		return []Block{{Kind: Quote, Align: align, Children: children, IDs: ids}}, err
	case name == "ul" || name == "ol":
		return p.list(se, align, ids)
	case name == "pre":
		var t strings.Builder
		err := readText(p.d, &t)
		return []Block{{Kind: Pre, Text: strings.TrimRight(strings.TrimPrefix(t.String(), "\n"), " \n"), IDs: ids}}, err
	case name == "hr":
		return []Block{{Kind: Rule, IDs: ids}}, p.d.Skip()
	case name == "table":
		return p.table(ids)
	case name == "img":
		img, ok := p.image(se)
		if err := p.d.Skip(); err != nil {
			return nil, err
		}
		if !ok {
			return withIDs(nil), nil
		}
		return withIDs([]Block{{Kind: Image, Align: align, Image: img}}), nil
	case name == "svg":
		img, ok, err := p.svgImage()
		if !ok {
			return withIDs(nil), err
		}
		return withIDs([]Block{{Kind: Image, Align: align, Image: img}}), err
	}
	return nil, p.d.Skip()
}

// list reads a <ul> or <ol> whose start was read: the blocks of each <li>.
func (p *parser) list(se xml.StartElement, align Align, ids []string) ([]Block, error) {
	b := Block{Kind: List, Align: align, Ordered: se.Name.Local == "ol", Start: 1, IDs: ids}
	if n, err := strconv.Atoi(attr(se, "start")); err == nil {
		b.Start = n
	}
	for {
		tok, err := p.d.Token()
		if err != nil {
			return []Block{b}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if hiddenElement(t) {
				if err := p.d.Skip(); err != nil {
					return []Block{b}, err
				}
				continue
			}
			if t.Name.Local != "li" {
				// a list's text outside an item, or a nested list without an item: an item of its own
				made, err := p.block(t, align)
				if len(made) > 0 {
					b.Items = append(b.Items, made)
				}
				if err != nil {
					return []Block{b}, err
				}
				continue
			}
			item, err := p.blocks("li", align)
			if id := attr(t, "id"); id != "" {
				if len(item) == 0 {
					item = []Block{{Kind: Paragraph}}
				}
				item[0].IDs = append([]string{id}, item[0].IDs...)
			}
			b.Items = append(b.Items, item)
			if err != nil {
				return []Block{b}, err
			}
		case xml.EndElement:
			if t.Name.Local == se.Name.Local {
				return []Block{b}, nil
			}
		}
	}
}

// table reads a <table> whose start was read into the text of its cells, row by row.
func (p *parser) table(ids []string) ([]Block, error) {
	b := Block{Kind: Table, IDs: ids}
	var row []string
	var cell *strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := p.d.Token()
		if err != nil {
			return []Block{b}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "tr":
				row = nil
			case "td", "th":
				cell = &strings.Builder{}
			case "br":
				if cell != nil {
					cell.WriteString(" ")
				}
			}
			if id := attr(t, "id"); id != "" {
				b.IDs = append(b.IDs, id)
			}
		case xml.CharData:
			if cell != nil {
				cell.Write(t)
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
			case "td", "th":
				if cell != nil {
					row = append(row, collapse(cell.String()))
					cell = nil
				}
			case "tr":
				if len(row) > 0 {
					b.Rows = append(b.Rows, row)
				}
				row = nil
			}
		}
	}
	return []Block{b}, nil
}

// image is the image of an <img>; false for a remote one (not loaded, as the Markdown preview's) or one outside the book.
func (p *parser) image(se xml.StartElement) (ImageRef, bool) {
	src := attr(se, "src")
	full, _, ok := resolve(p.dir, src)
	if !ok || full == "" {
		return ImageRef{}, false
	}
	return ImageRef{Path: full, Alt: collapse(attr(se, "alt"))}, true
}

// svgImage reads an <svg> whose start was read and gives the first <image> in it (xlink:href or href).
func (p *parser) svgImage() (ImageRef, bool, error) {
	var img ImageRef
	found := false
	depth := 1
	for depth > 0 {
		tok, err := p.d.Token()
		if err != nil {
			return img, found, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Local == "image" && !found {
				if full, _, ok := resolve(p.dir, attr(t, "href")); ok && full != "" {
					img, found = ImageRef{Path: full}, true
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	return img, found, nil
}

// inlines reads the inline content of an element whose start was read, through its end, with the style and link it inherits: the
// runs, the ids in it, and the images in it (made blocks of their own after the text).
func (p *parser) inlines(se xml.StartElement, style Style, link string) ([]Inline, []string, []Block, error) {
	external := false
	switch se.Name.Local {
	case "em", "i", "cite", "dfn", "var":
		style |= Emphasis
	case "strong", "b":
		style |= Strong
	case "code", "tt", "kbd", "samp":
		style |= Code
	case "sub":
		style |= Sub
	case "sup":
		style |= Sup
	case "a":
		if href := attr(se, "href"); href != "" {
			link, external = p.link(href)
		}
	}
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "mailto:") {
		external = true
	}
	var runs []Inline
	var ids []string
	var imgs []Block
	for {
		tok, err := p.d.Token()
		if err != nil {
			return runs, ids, imgs, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			runs = append(runs, Inline{Text: string(t), Style: style, Link: link, External: external})
		case xml.EndElement:
			if t.Name.Local == se.Name.Local {
				return runs, ids, imgs, nil
			}
		case xml.StartElement:
			name := t.Name.Local
			switch {
			case isNote(t):
				notes, err := p.blocks(name, AlignStart)
				if err != nil {
					return runs, ids, imgs, err
				}
				p.addNote(t, notes)
				continue
			case hiddenElement(t) || skipped[name]:
				if err := p.d.Skip(); err != nil {
					return runs, ids, imgs, err
				}
				continue
			case name == "br":
				runs = append(runs, Inline{Text: "\n", Style: style, Link: link, External: external})
				if err := p.d.Skip(); err != nil {
					return runs, ids, imgs, err
				}
				continue
			case name == "img":
				if img, ok := p.image(t); ok {
					imgs = append(imgs, Block{Kind: Image, Image: img})
				}
				if err := p.d.Skip(); err != nil {
					return runs, ids, imgs, err
				}
				continue
			case name == "svg":
				img, ok, err := p.svgImage()
				if ok {
					imgs = append(imgs, Block{Kind: Image, Image: img})
				}
				if err != nil {
					return runs, ids, imgs, err
				}
				continue
			}
			if id := attr(t, "id"); id != "" {
				ids = append(ids, id)
			}
			r, i, m, err := p.inlines(t, style, link)
			runs, ids, imgs = append(runs, r...), append(ids, i...), append(imgs, m...)
			if err != nil {
				return runs, ids, imgs, err
			}
			if blockStarts[name] || containers[name] { // a block inside text (broken HTML): a space keeps the words apart
				runs = append(runs, Inline{Text: " ", Style: style})
			}
		}
	}
}

// link is where a link goes: a path in the book with its anchor, or a URL of the web; "" for a link that goes nowhere the reader
// can follow.
func (p *parser) link(href string) (string, bool) {
	if isURL(href) {
		lower := strings.ToLower(href)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") {
			return href, true
		}
		return "", false
	}
	full, frag, ok := resolve(p.dir, href)
	if !ok {
		return "", false
	}
	if full == "" {
		full = p.self
	}
	if frag != "" {
		return full + "#" + frag, false
	}
	return full, false
}

// tidy makes the runs of a block as they read: white space collapsed across runs, trimmed at the ends (and around line breaks), and
// runs of the same style and link joined.
func tidy(runs []Inline) []Inline {
	var out []Inline
	space := true // at the start, white space is dropped
	for _, r := range runs {
		if r.Text == "\n" {
			if len(out) > 0 {
				last := &out[len(out)-1]
				last.Text = strings.TrimRight(last.Text, " ")
			}
			out = append(out, r)
			space = true
			continue
		}
		var b strings.Builder
		for _, c := range r.Text {
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' {
				if !space {
					b.WriteByte(' ')
					space = true
				}
				continue
			}
			b.WriteRune(c)
			space = false
		}
		if b.Len() == 0 {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Style == r.Style && out[n-1].Link == r.Link && out[n-1].Text != "\n" {
			out[n-1].Text += b.String()
			continue
		}
		out = append(out, Inline{Text: b.String(), Style: r.Style, Link: r.Link, External: r.External})
	}
	// trailing space and line breaks
	for len(out) > 0 {
		last := &out[len(out)-1]
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text == "" || last.Text == "\n" {
			out = out[:len(out)-1]
			continue
		}
		break
	}
	return out
}
