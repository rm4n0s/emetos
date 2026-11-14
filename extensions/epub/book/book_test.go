package book

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// route fails the test unless err is an *errors.Error whose route has want.
func route(t *testing.T, err error, want string) {
	t.Helper()
	e, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !e.HasRoute(want) {
		t.Errorf("route %s, want %s", e.Route(), want)
	}
}

// entry is a file of a book the test writes; stored entries are not compressed.
type entry struct {
	name, body string
	stored     bool
}

// writeBook writes a zip of the entries and gives its path.
func writeBook(t *testing.T, entries ...entry) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		method := zip.Deflate
		if e.stored {
			method = zip.Store
		}
		f, err := w.CreateHeader(&zip.FileHeader{Name: e.name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const container = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

// opf is a package document with the manifest items and spine of a test; nav and ncx add the navigation document and the NCX.
func opf(nav, ncx bool) string {
	var items, toc string
	if nav {
		items += `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`
	}
	if ncx {
		items += `<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`
		toc = ` toc="ncx"`
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>  A   Test Book </dc:title>
    <dc:creator>Ada Example</dc:creator>
    <dc:creator>Bo Example</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>` + items + `
    <item id="c1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="text/ch%202.xhtml" media-type="application/xhtml+xml"/>
    <item id="cover" href="images/cover.png" media-type="image/png"/>
    <item id="gone" href="text/gone.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine` + toc + `>
    <itemref idref="c1"/>
    <itemref idref="c2" linear="no"/>
    <itemref idref="gone"/>
    <itemref idref="nothing"/>
  </spine>
</package>`
}

const navDoc = `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>
<nav epub:type="landmarks"><ol><li><a href="text/ch1.xhtml">Not this</a></li></ol></nav>
<nav epub:type="toc"><h1>Contents</h1><ol>
  <li><a href="text/ch1.xhtml">Chapter <em>One</em></a>
    <ol><li><a href="text/ch1.xhtml#s2">A section</a></li></ol></li>
  <li><span>Part Two</span><ol><li><a href="text/ch%202.xhtml">Chapter Two</a></li></ol></li>
  <li><a href="https://example.com/">The web</a></li>
</ol></nav></body></html>`

const ncxDoc = `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
  <navPoint id="p1"><navLabel><text>NCX One</text></navLabel><content src="text/ch1.xhtml"/>
    <navPoint id="p2"><navLabel><text>NCX Section</text></navLabel><content src="text/ch1.xhtml#s2"/></navPoint>
  </navPoint>
  <navPoint id="p3"><navLabel><text>NCX Two</text></navLabel><content src="text/ch%202.xhtml"/></navPoint>
</navMap></ncx>`

// chapter wraps a body in an XHTML document.
func chapter(title, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>` + title + `</title><style>p { color: red }</style></head>
<body>` + body + `</body></html>`
}

// bookWith is a whole book with its navigation choices and the first chapter's body.
func bookWith(t *testing.T, nav, ncx bool, body string) string {
	entries := []entry{
		{name: "mimetype", body: "application/epub+zip", stored: true},
		{name: "META-INF/container.xml", body: container},
		{name: "OEBPS/content.opf", body: opf(nav, ncx)},
		{name: "OEBPS/text/ch1.xhtml", body: chapter("One", body)},
		{name: "OEBPS/text/ch 2.xhtml", body: chapter("Two", `<h1 id="top">Two</h1><p>The second.</p>`)},
		{name: "OEBPS/images/cover.png", body: "PNG"},
	}
	if nav {
		entries = append(entries, entry{name: "OEBPS/nav.xhtml", body: navDoc})
	}
	if ncx {
		entries = append(entries, entry{name: "OEBPS/toc.ncx", body: ncxDoc})
	}
	return writeBook(t, entries...)
}

func openBook(t *testing.T, p string) *Book {
	t.Helper()
	b, err := Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	return b
}

func TestTheBookAndItsContents(t *testing.T) {
	navTOC := []TOCEntry{
		{Title: "Chapter One", Path: "OEBPS/text/ch1.xhtml", Children: []TOCEntry{{Title: "A section", Path: "OEBPS/text/ch1.xhtml", Anchor: "s2"}}},
		{Title: "Part Two", Path: "OEBPS/text/ch 2.xhtml", Children: []TOCEntry{{Title: "Chapter Two", Path: "OEBPS/text/ch 2.xhtml"}}},
	}
	ncxTOC := []TOCEntry{
		{Title: "NCX One", Path: "OEBPS/text/ch1.xhtml", Children: []TOCEntry{{Title: "NCX Section", Path: "OEBPS/text/ch1.xhtml", Anchor: "s2"}}},
		{Title: "NCX Two", Path: "OEBPS/text/ch 2.xhtml"},
	}
	tests := []struct {
		name     string
		nav, ncx bool
		want     []TOCEntry
	}{
		{"EPUB 3 with a navigation document", true, false, navTOC},
		{"EPUB 2 with an NCX", false, true, ncxTOC},
		{"both: the navigation document", true, true, navTOC},
		{"neither", false, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := openBook(t, bookWith(t, tt.nav, tt.ncx, "<p>Hi</p>"))
			if b.Title != "A Test Book" || !reflect.DeepEqual(b.Authors, []string{"Ada Example", "Bo Example"}) || b.Language != "en" {
				t.Errorf("metadata %q %q %q", b.Title, b.Authors, b.Language)
			}
			wantSpine := []SpineItem{{Path: "OEBPS/text/ch1.xhtml", ID: "c1", Linear: true}, {Path: "OEBPS/text/ch 2.xhtml", ID: "c2"}}
			if !reflect.DeepEqual(b.Spine, wantSpine) {
				t.Errorf("spine %+v", b.Spine)
			}
			if !reflect.DeepEqual(b.TOC, tt.want) {
				t.Errorf("contents %+v\nwant %+v", b.TOC, tt.want)
			}
		})
	}
	b := openBook(t, bookWith(t, true, false, "<p>Hi</p>"))
	if data, err := b.Resource("OEBPS/images/cover.png"); err != nil || string(data) != "PNG" {
		t.Errorf("the cover %q, %v", data, err)
	}
	_, err := b.Resource("OEBPS/images/none.png")
	route(t, err, "Book.Resource.EPUBResourceMissing")
	_, err = b.Chapter(5)
	route(t, err, "Book.Chapter.EPUBChapterMissing")
}

func TestTheBlocksOfAChapter(t *testing.T) {
	body := `
<section id="start" style="text-align: center">
  <h1 id="h">The <em>first</em> chapter</h1>
  <p>Text with <strong>strong</strong>, <i>emphasis</i>, <code>code</code>, H<sub>2</sub>O and x<sup>2</sup>.</p>
</section>
<p style="TEXT-ALIGN:right">Right&nbsp;&mdash; caf&eacute; &amp; more</p>
<blockquote><p>A quote.</p><p>Two.</p></blockquote>
<ul><li>One</li><li id="two">Two <b>bold</b><ul><li>Nested</li></ul></li></ul>
<ol start="3"><li><p>Three</p></li></ol>
<pre>  line 1
  line 2
</pre>
<hr>
<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>two <br>lines</td></tr></table>
<p hidden="">Hidden</p>
<div style="display: none"><p>Not shown</p></div>
<p>Unclosed <br> break<img src="../images/cover.png" alt="The cover"></p>
<div>loose text and <a href="ch%202.xhtml#top">a link</a>, <a href="#h">back</a>, <a href="https://example.com/x">the web</a>,
  <a href="javascript:alert(1)">none</a> and a note<a href="#n1" epub:type="noteref">1</a>.<p>A paragraph in a div
</div>
<aside epub:type="footnote" id="n1"><p>The note's body.</p></aside>
<p><img src="https://example.com/remote.png" alt="remote">After a remote image.</p>
<svg xmlns:xlink="http://www.w3.org/1999/xlink"><image xlink:href="../images/cover.png"/></svg>
<p id="s2">The end<span id="inline-id"></span>.</p>
<script>alert("no")</script>`
	b := openBook(t, bookWith(t, true, false, body))
	ch, err := b.Chapter(0)
	if err != nil {
		t.Fatal(err)
	}
	cover := ImageRef{Path: "OEBPS/images/cover.png", Alt: "The cover"}
	want := []Block{
		{Kind: Heading, Level: 1, Align: AlignCenter, Inlines: []Inline{{Text: "The "}, {Text: "first", Style: Emphasis}, {Text: " chapter"}}, IDs: []string{"start", "h"}},
		{Kind: Paragraph, Align: AlignCenter, Inlines: []Inline{{Text: "Text with "}, {Text: "strong", Style: Strong}, {Text: ", "},
			{Text: "emphasis", Style: Emphasis}, {Text: ", "}, {Text: "code", Style: Code}, {Text: ", H"}, {Text: "2", Style: Sub},
			{Text: "O and x"}, {Text: "2", Style: Sup}, {Text: "."}}},
		{Kind: Paragraph, Align: AlignRight, Inlines: []Inline{{Text: "Right\u00a0— café & more"}}},
		{Kind: Quote, Children: []Block{{Kind: Paragraph, Inlines: []Inline{{Text: "A quote."}}}, {Kind: Paragraph, Inlines: []Inline{{Text: "Two."}}}}},
		{Kind: List, Start: 1, Items: [][]Block{
			{{Kind: Paragraph, Inlines: []Inline{{Text: "One"}}}},
			{{Kind: Paragraph, Inlines: []Inline{{Text: "Two "}, {Text: "bold", Style: Strong}}, IDs: []string{"two"}},
				{Kind: List, Start: 1, Items: [][]Block{{{Kind: Paragraph, Inlines: []Inline{{Text: "Nested"}}}}}}},
		}},
		{Kind: List, Ordered: true, Start: 3, Items: [][]Block{{{Kind: Paragraph, Inlines: []Inline{{Text: "Three"}}}}}},
		{Kind: Pre, Text: "  line 1\n  line 2"},
		{Kind: Rule},
		{Kind: Table, Rows: [][]string{{"A", "B"}, {"1", "two lines"}}},
		{Kind: Paragraph, Inlines: []Inline{{Text: "Unclosed"}, {Text: "\n"}, {Text: "break"}}},
		{Kind: Image, Image: cover},
		{Kind: Paragraph, Inlines: []Inline{{Text: "loose text and "}, {Text: "a link", Link: "OEBPS/text/ch 2.xhtml#top"}, {Text: ", "},
			{Text: "back", Link: "OEBPS/text/ch1.xhtml#h"}, {Text: ", "}, {Text: "the web", Link: "https://example.com/x", External: true},
			{Text: ", none and a note"}, {Text: "1", Link: "OEBPS/text/ch1.xhtml#n1"}, {Text: "."}}},
		{Kind: Paragraph, Inlines: []Inline{{Text: "A paragraph in a div"}}},
		{Kind: Paragraph, Inlines: []Inline{{Text: "After a remote image."}}},
		{Kind: Image, Image: ImageRef{Path: "OEBPS/images/cover.png"}},
		{Kind: Paragraph, Inlines: []Inline{{Text: "The end."}}, IDs: []string{"s2", "inline-id"}},
	}
	if len(ch.Blocks) != len(want) {
		for i, b := range ch.Blocks {
			t.Logf("%d: %+v", i, b)
		}
		t.Fatalf("%d blocks, want %d", len(ch.Blocks), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(ch.Blocks[i], want[i]) {
			t.Errorf("block %d:\n got %+v\nwant %+v", i, ch.Blocks[i], want[i])
		}
	}
	if ch.Title != "One" {
		t.Errorf("title %q", ch.Title)
	}
	anchors := map[string]int{"start": 0, "h": 0, "two": 4, "s2": 15, "inline-id": 15}
	if !reflect.DeepEqual(ch.Anchors, anchors) {
		t.Errorf("anchors %v", ch.Anchors)
	}
	if note := ch.Notes["n1"]; len(note) != 1 || note[0].Inlines[0].Text != "The note's body." {
		t.Errorf("notes %+v", ch.Notes)
	}
}

func TestBooksThatAreRefused(t *testing.T) {
	good := []entry{{name: "META-INF/container.xml", body: container}, {name: "OEBPS/content.opf", body: opf(false, false)},
		{name: "OEBPS/text/ch1.xhtml", body: chapter("One", "<p>x</p>")}}
	many := []entry{}
	for i := range MaxEntries + 1 {
		many = append(many, entry{name: "f" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + "/" + itoa(i), stored: true})
	}
	tests := []struct {
		name    string
		entries []entry
		route   string
		message string
	}{
		{"too many entries", many, "Open->open.EPUBTooManyEntries", TooLargeMessage},
		{"a zip bomb", append(good[:3:3], entry{name: "bomb", body: strings.Repeat("\x00", 4<<20)}), "Open->open.EPUBZipBomb", TooLargeMessage},
		{"a path with ..", append(good[:3:3], entry{name: "OEBPS/../../etc/passwd", body: "x"}), "Open->open.EPUBPathInvalid", ""},
		{"DRM", append(good[:3:3], entry{name: "META-INF/encryption.xml", body: `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/><CipherData><CipherReference URI="OEBPS/text/ch1.xhtml"/></CipherData></EncryptedData></encryption>`}),
			"Open->open.EPUBProtected", DRMMessage},
		{"no container", good[1:], "Open->open.EPUBNoContainer", ""},
		{"a container that names nothing", append([]entry{{name: "META-INF/container.xml", body: "<container/>"}}, good[1:]...), "Open->open.EPUBContainerInvalid", ""},
		{"no package document", []entry{good[0]}, "Open->open.EPUBPackageMissing", ""},
		{"a broken package document", []entry{good[0], {name: "OEBPS/content.opf", body: "<package><metadata><<"}}, "Open->open.EPUBPackageInvalid", ""},
		{"not a package document", []entry{good[0], {name: "OEBPS/content.opf", body: "<html/>"}}, "Open->open.EPUBPackageInvalid", ""},
		{"a package document without chapters", []entry{good[0], {name: "OEBPS/content.opf", body: "<package><spine/></package>"}}, "Open->open.EPUBPackageInvalid", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Open(context.Background(), writeBook(t, tt.entries...))
			route(t, err, tt.route)
			if e, ok := errors.FromError(err); ok && tt.message != "" && e.Message != tt.message {
				t.Errorf("message %q, want %q", e.Message, tt.message)
			}
		})
	}
	// fonts' obfuscation is not DRM
	fonts := append(good[:3:3], entry{name: "META-INF/encryption.xml", body: `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/></EncryptedData></encryption>`})
	openBook(t, writeBook(t, fonts...))
	// not a zip, no file, a context that ended
	notZip := filepath.Join(t.TempDir(), "x.epub")
	if err := os.WriteFile(notZip, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), notZip)
	route(t, err, "Open->open.EPUBNotZip")
	_, err = Open(context.Background(), filepath.Join(t.TempDir(), "none.epub"))
	route(t, err, "Open.EPUBOpenFailed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Open(ctx, writeBook(t, good...))
	route(t, err, "Open->open.EPUBStopped")
}

func TestEntriesOverTheLimits(t *testing.T) {
	// the headers are made to claim more than the limits without writing gigabytes: an entry stored with its size in the header
	tests := []struct {
		name  string
		sizes []uint64
		route string
	}{
		{"an entry too large", []uint64{MaxEntrySize + 1}, "Open->open.EPUBEntryTooLarge"},
		{"a book too large", []uint64{MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize,
			MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, MaxEntrySize, 1},
			"Open->open.EPUBTooLarge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Open(context.Background(), writeClaimed(t, tt.sizes))
			route(t, err, tt.route)
		})
	}
}

// writeClaimed writes a zip whose entries' headers claim sizes they do not have (stored, compressed size equal to it, so that the
// ratio is 1), with no data: Open looks only at the directory before it refuses.
func writeClaimed(t *testing.T, sizes []uint64) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i, size := range sizes {
		h := &zip.FileHeader{Name: "big" + itoa(i), Method: zip.Store, UncompressedSize64: size, CompressedSize64: size}
		if _, err := w.CreateRaw(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "claimed.epub")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func itoa(n int) string {
	s := ""
	for {
		s = string(rune('0'+n%10)) + s
		n /= 10
		if n == 0 {
			return s
		}
	}
}

// corrupt spoils the compressed data of an entry of a book file (after its local header and name).
func corrupt(t *testing.T, p, name string) {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(data, []byte(name))
	if at < 0 {
		t.Fatalf("no %s in the zip", name)
	}
	for i := at + len(name); i < at+len(name)+8 && i < len(data); i++ {
		data[i] ^= 0xff
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChaptersAndImagesThatCannotBeRead(t *testing.T) {
	p := bookWith(t, false, false, "<p>Hi there, a chapter long enough to be compressed by the writer of the test.</p>")
	corrupt(t, p, "OEBPS/text/ch1.xhtml")
	corrupt(t, p, "OEBPS/images/cover.png")
	b := openBook(t, p)
	_, err := b.Chapter(0)
	route(t, err, "Book.Chapter.EPUBChapterUnreadable")
	_, err = b.Resource("OEBPS/images/cover.png")
	route(t, err, "Book.Resource.EPUBResourceInvalid")
	b2 := openBook(t, bookWith(t, false, false, "<p>a <<</p>"))
	_, err = b2.Chapter(0)
	route(t, err, "Book.Chapter.EPUBChapterInvalid")
	// a chapter cut short gives what was read
	b3 := openBook(t, bookWith(t, false, false, "<p>Cut <em>short"))
	ch, err := b3.Chapter(0)
	if err != nil || len(ch.Blocks) != 1 {
		t.Errorf("a chapter cut short: %+v, %v", ch, err)
	}
}

func TestClosingTwice(t *testing.T) {
	b, err := Open(context.Background(), bookWith(t, false, false, "<p>x</p>"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	route(t, b.Close(), "Book.Close.EPUBCloseFailed")
}
