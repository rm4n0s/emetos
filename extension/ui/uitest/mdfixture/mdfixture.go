// Package mdfixture makes the big Markdown document that the tests and the benchmark of the preview use (SPEC §14 "Markdown
// preview", step 17b.2): a document of about 700 KB with 150 images, the size of docs/IMPLEMENTED.md in October 2026, made from
// small generated PNG files, so that no test depends on a document that changes with every step of the plan.
package mdfixture

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The shape of the document.
const (
	// ImageCount is the number of images the document shows.
	ImageCount = 150
	// TargetBytes is the size of the document, about.
	TargetBytes = 700 << 10
	// ImageWidth is the width of every image, in pixels (a screenshot of a small window).
	ImageWidth = 960
	// ImageHeight is the height of every image, in pixels.
	ImageHeight = 540
	// DocName and ImageDir are the names of the document and of the folder of the images, relative to the folder it is made in.
	DocName  = "big.md"
	ImageDir = "img"
)

// Document is the Markdown document that Generate writes.
type Document struct {
	// Path is the absolute path of the document.
	Path string
	// Text is the document.
	Text string
	// Sections is the number of sections (headings of level 2) the document has.
	Sections int
}

// Generate writes the document and its images into dir (which must exist) and returns the document; it fails the test when a
// file cannot be written. It is deterministic: the same content comes out every time.
func Generate(tb testing.TB, dir string) Document {
	tb.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ImageDir), 0o755); err != nil {
		tb.Fatalf("mdfixture: %v", err)
	}
	// a few different pictures, written under many names: the cache is keyed by the path, so each name is an image
	const kinds = 10
	var pictures [kinds][]byte
	for k := range pictures {
		pictures[k] = encodePicture(tb, k)
	}
	for i := range ImageCount {
		if err := os.WriteFile(filepath.Join(dir, ImageDir, imageName(i)), pictures[i%kinds], 0o644); err != nil {
			tb.Fatalf("mdfixture: %v", err)
		}
	}
	text, sections := text()
	path := filepath.Join(dir, DocName)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		tb.Fatalf("mdfixture: %v", err)
	}
	return Document{Path: path, Text: text, Sections: sections}
}

func imageName(i int) string { return "shot-" + pad(i, 3) + ".png" }

func pad(n, width int) string {
	s := strconv.Itoa(n)
	return strings.Repeat("0", max(width-len(s), 0)) + s
}

// encodePicture makes picture k: a gradient with a few bands, so it compresses to a few kilobytes and decodes to
// ImageWidth*ImageHeight*4 bytes.
func encodePicture(tb testing.TB, k int) []byte {
	tb.Helper()
	img := image.NewRGBA(image.Rect(0, 0, ImageWidth, ImageHeight))
	for y := range ImageHeight {
		for x := range ImageWidth {
			band := uint8(0)
			if (x/80+y/60+k)%7 == 0 {
				band = 60
			}
			img.SetRGBA(x, y, color.RGBA{R: uint8(x*255/ImageWidth) + band, G: uint8(y*255/ImageHeight) + uint8(k*20), B: uint8(40+k*18) + band, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		tb.Fatalf("mdfixture: %v", err)
	}
	return b.Bytes()
}

var words = strings.Fields(`the editor opens a document and shows its text; each change is applied to the buffer, the views follow it,
and the language server is told about it after a short delay; a step is finished when its checks pass and the proof image has been
looked at, then it is merged and tagged; scrolling stays smooth because only what is on the screen is built, measured and drawn`)

// sentence is a sentence of n words that starts at word i.
func sentence(i, n int) string {
	var b strings.Builder
	for j := range n {
		if j > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(words[(i+j*7)%len(words)])
	}
	return b.String() + "."
}

// text builds the document: sections of a heading, paragraphs, a list, a code block, a table and a quote, with an image every
// few blocks, until the document has TargetBytes and ImageCount images.
func text() (string, int) {
	var b strings.Builder
	b.WriteString("# Implemented\n\nA log of finished steps, with a proof image for each.\n\n")
	images, section := 0, 0
	for b.Len() < TargetBytes || images < ImageCount {
		section++
		b.WriteString("## Step " + strconv.Itoa(section) + ": " + sentence(section, 5) + "\n\n")
		b.WriteString("**Summary:** " + sentence(section, 60) + " See [the spec](docs/SPEC.md) and `go test ./...`.\n\n")
		b.WriteString("- " + sentence(section+1, 12) + "\n- " + sentence(section+2, 14) + "\n- [x] " + sentence(section+3, 9) + "\n\n")
		b.WriteString("```go\nfunc step" + strconv.Itoa(section) + "(ctx context.Context) error {\n\tif err := run(ctx); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n```\n\n")
		b.WriteString(sentence(section+4, 90) + "\n\n")
		b.WriteString("| Check | Result | Time |\n|---|---|---|\n| vet | ok | 4 s |\n| lint | ok | 31 s |\n| test | ok | " + strconv.Itoa(section%90+10) + " s |\n\n")
		if images < ImageCount && (section%2 == 0 || b.Len() >= TargetBytes) {
			b.WriteString("![Proof " + strconv.Itoa(images) + "](" + ImageDir + "/" + imageName(images) + ")\n\n")
			images++
		}
		b.WriteString("> " + sentence(section+5, 30) + "\n\n")
		b.WriteString(sentence(section+6, 70) + "\n\n")
	}
	return b.String(), section
}
