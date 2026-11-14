package book

import (
	"encoding/xml"
	"io"
	"path"
	"strings"
)

// manifestItem is an item of the package document's manifest.
type manifestItem struct {
	href, mediaType, properties string
}

// spineRef is an itemref of the spine.
type spineRef struct {
	idref  string
	linear bool
}

// packageDoc is what the package document says.
type packageDoc struct {
	title, language string
	authors         []string
	manifest        map[string]manifestItem
	spine           []spineRef
	tocID           string
}

// parsePackage reads the package document: dc:title, dc:creator, dc:language, the manifest and the spine.
func parsePackage(data []byte) (packageDoc, error) {
	d := lenientDecoder(data)
	pkg := packageDoc{manifest: map[string]manifestItem{}}
	sawPackage := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return packageDoc{}, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "package":
			sawPackage = true
		case "title", "creator", "language":
			var b strings.Builder
			if err := readText(d, &b); err != nil {
				return packageDoc{}, err
			}
			text := collapse(b.String())
			switch {
			case text == "":
			case se.Name.Local == "title" && pkg.title == "":
				pkg.title = text
			case se.Name.Local == "creator":
				pkg.authors = append(pkg.authors, text)
			case se.Name.Local == "language" && pkg.language == "":
				pkg.language = text
			}
		case "item":
			if id := attr(se, "id"); id != "" {
				pkg.manifest[id] = manifestItem{href: attr(se, "href"), mediaType: attr(se, "media-type"), properties: attr(se, "properties")}
			}
		case "spine":
			pkg.tocID = attr(se, "toc")
		case "itemref":
			pkg.spine = append(pkg.spine, spineRef{idref: attr(se, "idref"), linear: attr(se, "linear") != "no"})
		}
	}
	if !sawPackage {
		return packageDoc{}, errNotPackage
	}
	return pkg, nil
}

// errNotPackage is a package document with no <package>; it is wrapped by Open.
var errNotPackage = &xml.SyntaxError{Msg: "no <package> element", Line: 1}

// read fills the book from its package document, whose folder is dir: the spine (the items it names that are in the manifest and in
// the zip) and the table of contents.
func (b *Book) read(pkg packageDoc, dir string) {
	b.Title, b.Authors, b.Language = pkg.title, pkg.authors, pkg.language
	for _, ref := range pkg.spine {
		item, ok := pkg.manifest[ref.idref]
		if !ok {
			continue
		}
		p, _, ok := resolve(dir, item.href)
		if !ok || p == "" || b.entries[p] == nil {
			continue
		}
		b.Spine = append(b.Spine, SpineItem{Path: p, ID: ref.idref, Linear: ref.linear})
	}
	// EPUB 3's navigation document, else EPUB 2's NCX
	for _, item := range pkg.manifest {
		if hasWord(item.properties, "nav") {
			if p, _, ok := resolve(dir, item.href); ok && b.entries[p] != nil {
				if data, err := readEntry(b.entries[p]); err == nil {
					b.TOC = parseNav(data, path.Dir(p))
				}
			}
			break
		}
	}
	if len(b.TOC) > 0 {
		return
	}
	ncx, ok := pkg.manifest[pkg.tocID]
	if !ok {
		for _, item := range pkg.manifest {
			if item.mediaType == "application/x-dtbncx+xml" {
				ncx, ok = item, true
				break
			}
		}
	}
	if !ok {
		return
	}
	if p, _, ok := resolve(dir, ncx.href); ok && b.entries[p] != nil {
		if data, err := readEntry(b.entries[p]); err == nil {
			b.TOC = parseNCX(data, path.Dir(p))
		}
	}
}

// hasWord tells whether a space-separated list has a word.
func hasWord(list, word string) bool {
	for f := range strings.FieldsSeq(list) {
		if f == word {
			return true
		}
	}
	return false
}

// tocEntry makes an entry of a title and a reference of a document at dir; false for a reference outside the book.
func tocEntry(title, ref, dir string) (TOCEntry, bool) {
	p, frag, ok := resolve(dir, ref)
	if !ok || p == "" {
		return TOCEntry{}, false
	}
	return TOCEntry{Title: collapse(title), Path: p, Anchor: frag}, true
}

// parseNav reads the <nav epub:type="toc"> of a navigation document: its <ol> of <li>s, each an <a> (or a <span> heading) and an
// <ol> of the entries under it.
func parseNav(data []byte, dir string) []TOCEntry {
	d := lenientDecoder(data)
	for {
		tok, err := d.Token()
		if err != nil {
			return nil
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "nav" && hasWord(attr(se, "type"), "toc") {
			return navList(d, "nav", dir)
		}
	}
}

// navList reads the entries of the <ol> in an element whose start was read, through its end.
func navList(d *xml.Decoder, end, dir string) []TOCEntry {
	var out []TOCEntry
	for {
		tok, err := d.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "li" {
				if e, ok := navItem(d, dir); ok {
					out = append(out, e)
				}
			}
		case xml.EndElement:
			if t.Name.Local == end {
				return out
			}
		}
	}
}

// navItem reads an <li> whose start was read: its link or heading and the list under it. An entry without a link keeps its children
// as its own when it has a title (a heading of a group); otherwise it is dropped.
func navItem(d *xml.Decoder, dir string) (TOCEntry, bool) {
	var e TOCEntry
	var title strings.Builder
	href, hasLink := "", false
	for ended := false; !ended; {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "a", "span":
				if t.Name.Local == "a" && !hasLink {
					href, hasLink = attr(t, "href"), true
				}
				if err := readText(d, &title); err != nil {
					return e, false
				}
			case "ol":
				e.Children = append(e.Children, navList(d, "ol", dir)...)
			}
		case xml.EndElement:
			ended = t.Name.Local == "li"
		}
	}
	if hasLink {
		if entry, ok := tocEntry(title.String(), href, dir); ok {
			entry.Children = e.Children
			return entry, true
		}
	}
	if t := collapse(title.String()); t != "" && len(e.Children) > 0 {
		first := e.Children[0]
		return TOCEntry{Title: t, Path: first.Path, Anchor: first.Anchor, Children: e.Children}, true
	}
	return e, false
}

// parseNCX reads the navMap of an NCX: nested navPoints with a navLabel's text and a content's src.
func parseNCX(data []byte, dir string) []TOCEntry {
	d := lenientDecoder(data)
	for {
		tok, err := d.Token()
		if err != nil {
			return nil
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "navMap" {
			return navPoints(d, "navMap", dir)
		}
	}
}

// navPoints reads the navPoints of an element whose start was read, through its end.
func navPoints(d *xml.Decoder, end, dir string) []TOCEntry {
	var out []TOCEntry
	for {
		tok, err := d.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "navPoint" {
				if e, ok := navPoint(d, dir); ok {
					out = append(out, e)
				}
			}
		case xml.EndElement:
			if t.Name.Local == end {
				return out
			}
		}
	}
}

// navPoint reads a navPoint whose start was read.
func navPoint(d *xml.Decoder, dir string) (TOCEntry, bool) {
	var title strings.Builder
	var src string
	var children []TOCEntry
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "text":
				if title.Len() == 0 {
					if err := readText(d, &title); err != nil {
						return TOCEntry{}, false
					}
				}
			case "content":
				if src == "" {
					src = attr(t, "src")
				}
			case "navPoint":
				if e, ok := navPoint(d, dir); ok {
					children = append(children, e)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "navPoint" {
				e, ok := tocEntry(title.String(), src, dir)
				e.Children = children
				return e, ok
			}
		}
	}
	e, ok := tocEntry(title.String(), src, dir)
	e.Children = children
	return e, ok
}

// readText reads the text of an element whose start was read, with the text of the elements in it, through its end.
func readText(d *xml.Decoder, b *strings.Builder) error {
	depth := 1
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			b.Write(t)
		}
	}
	return nil
}

// collapse makes every run of white space one space, and trims the ends.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
