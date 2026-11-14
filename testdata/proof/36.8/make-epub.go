//go:build ignore

// make-epub writes project/sample.epub, the book of the proof of step 36.8: the first two chapters of Lewis Carroll's "Alice's Adventures in
// Wonderland" (1865, in the public domain), shortened, with a picture drawn here, an EPUB 3 navigation document and an EPUB 2 NCX.
// Run it from this folder: go run make-epub.go
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const chapter1 = `<h1>Chapter I.<br/>Down the Rabbit-Hole</h1>
<p>Alice was beginning to get very tired of sitting by her sister on the bank, and of having nothing to do: once or twice she had
peeped into the book her sister was reading, but it had no pictures or conversations in it, “and what is the use of a book,” thought
Alice “without pictures or conversations?”</p>
<p>So she was considering in her own mind (as well as she could, for the hot day made her feel very sleepy and stupid), whether the
pleasure of making a daisy-chain would be worth the trouble of getting up and picking the daisies, when suddenly a
<em>White Rabbit with pink eyes</em> ran close by her.</p>
<p>There was nothing so <em>very</em> remarkable in that; nor did Alice think it so <em>very</em> much out of the way to hear the
Rabbit say to itself, “Oh dear! Oh dear! I shall be late!”</p>
<img src="rabbit-hole.png" alt="The rabbit-hole"/>
<p>In another moment down went Alice after it, never once considering how in the world she was to get out again.</p>
<blockquote><p>“Well!” thought Alice to herself, “after such a fall as this, I shall think nothing of tumbling down stairs!”</p></blockquote>
<p>Down, down, down. Would the fall <em>never</em> come to an end? See <a href="chapter2.xhtml#pool">the pool of tears</a>.</p>`

const chapter2 = `<h1>Chapter II.<br/>The Pool of Tears</h1>
<p>“Curiouser and curiouser!” cried Alice (she was so much surprised, that for the moment she quite forgot how to speak good
English); “now I’m opening out like the largest telescope that ever was! Good-bye, feet!”</p>
<p id="pool">Just then her head struck against the roof of the hall: in fact she was now more than nine feet high, and she at once took
up the little golden key and hurried off to the garden door.</p>
<p>Poor Alice! It was as much as she could do, lying down on one side, to look through into the garden with one eye; but to get
through was more hopeless than ever: she sat down and began to cry again.</p>`

func xhtml(title, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" lang="en">
<head><title>` + title + `</title></head>
<body>` + body + `</body></html>`
}

// picture draws a rabbit-hole: a dark ellipse in green grass, with rings.
func picture() []byte {
	const w, h = 480, 220
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{R: 96, G: 160, B: 72, A: 255}
			dx, dy := (float64(x)-w/2)/180, (float64(y)-h/2)/70
			d := math.Sqrt(dx*dx + dy*dy)
			switch {
			case d < 0.6:
				c = color.NRGBA{R: 30, G: 20, B: 14, A: 255}
			case d < 1:
				k := uint8(60 + 60*(d-0.6)/0.4)
				c = color.NRGBA{R: k + 40, G: k, B: k / 2, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func main() {
	files := []struct {
		name string
		body []byte
	}{
		{"mimetype", []byte("application/epub+zip")},
		{"META-INF/container.xml", []byte(`<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)},
		{"OEBPS/content.opf", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="id">emetos-proof-36.8</dc:identifier>
<dc:title>Alice’s Adventures in Wonderland</dc:title><dc:creator>Lewis Carroll</dc:creator><dc:language>en</dc:language></metadata>
<manifest><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
<item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/><item id="c2" href="chapter2.xhtml" media-type="application/xhtml+xml"/>
<item id="pic" href="rabbit-hole.png" media-type="image/png"/></manifest>
<spine toc="ncx"><itemref idref="c1"/><itemref idref="c2"/></spine></package>`)},
		{"OEBPS/nav.xhtml", []byte(xhtml("Contents", `<nav epub:type="toc"><h1>Contents</h1><ol>
<li><a href="chapter1.xhtml">Down the Rabbit-Hole</a></li>
<li><a href="chapter2.xhtml">The Pool of Tears</a><ol><li><a href="chapter2.xhtml#pool">Nine Feet High</a></li></ol></li></ol></nav>`))},
		{"OEBPS/toc.ncx", []byte(`<?xml version="1.0" encoding="UTF-8"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
<navPoint id="p1"><navLabel><text>Down the Rabbit-Hole</text></navLabel><content src="chapter1.xhtml"/></navPoint>
<navPoint id="p2"><navLabel><text>The Pool of Tears</text></navLabel><content src="chapter2.xhtml"/></navPoint></navMap></ncx>`)},
		{"OEBPS/chapter1.xhtml", []byte(xhtml("Down the Rabbit-Hole", chapter1))},
		{"OEBPS/chapter2.xhtml", []byte(xhtml("The Pool of Tears", chapter2))},
		{"OEBPS/rabbit-hole.png", picture()},
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i, f := range files {
		method := zip.Deflate
		if i == 0 {
			method = zip.Store // the mimetype first and stored, as EPUB asks
		}
		fw, err := w.CreateHeader(&zip.FileHeader{Name: f.name, Method: method})
		if err != nil {
			panic(err)
		}
		if _, err := fw.Write(f.body); err != nil {
			panic(err)
		}
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	if err := os.WriteFile("project/sample.epub", buf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
