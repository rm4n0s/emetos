//go:build ignore

// make-sample writes sample.pdf, the document of the proof of step 36.5: four US Letter pages of text and drawings, in the standard
// fonts every PDF reader has. Run it from this folder: go run make-sample.go
package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strings"
)

// page is the content stream of a page being written.
type page struct{ b strings.Builder }

func (p *page) text(x, y, size float64, font, s string) {
	s = strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(s)
	fmt.Fprintf(&p.b, "BT /%s %g Tf %g %g Td (%s) Tj ET\n", font, size, x, y, s)
}

func (p *page) color(r, g, b float64) {
	fmt.Fprintf(&p.b, "%g %g %g rg %g %g %g RG\n", r, g, b, r, g, b)
}

func (p *page) rect(x, y, w, h float64) { fmt.Fprintf(&p.b, "%g %g %g %g re f\n", x, y, w, h) }

func (p *page) line(x1, y1, x2, y2, width float64) {
	fmt.Fprintf(&p.b, "%g w %g %g m %g %g l S\n", width, x1, y1, x2, y2)
}

// circle fills a circle with four Bézier curves.
func (p *page) circle(cx, cy, r float64) {
	k := r * 0.5523
	fmt.Fprintf(&p.b, "%g %g m %g %g %g %g %g %g c %g %g %g %g %g %g c %g %g %g %g %g %g c %g %g %g %g %g %g c f\n",
		cx+r, cy, cx+r, cy+k, cx+k, cy+r, cx, cy+r, cx-k, cy+r, cx-r, cy+k, cx-r, cy, cx-r, cy-k, cx-k, cy-r, cx, cy-r, cx+k, cy-r, cx+r, cy-k, cx+r, cy)
}

// paragraph writes lines of text from a top, and gives the height under them.
func (p *page) paragraph(x, y float64, lines ...string) float64 {
	for _, l := range lines {
		p.text(x, y, 11, "F1", l)
		y -= 15
	}
	return y - 8
}

func main() {
	var pages []*page

	p := &page{}
	p.color(0.11, 0.37, 0.66)
	p.rect(0, 692, 612, 100)
	p.color(1, 1, 1)
	p.text(72, 730, 30, "F2", "The PDF Viewer")
	p.color(0.15, 0.15, 0.15)
	y := p.paragraph(72, 640,
		"Emetos opens PDF files in a tab of their own. The pages are drawn by Poppler, the free",
		"PDF library that most Linux desktops already have, and scrolled continuously, one",
		"under the other.",
	)
	y = p.paragraph(72, y,
		"Only the pages in view are drawn, with the one before and the one after them, at the",
		"resolution of the screen. A zoom scales the pictures it has at once and draws them",
		"again a moment later, so zooming stays smooth on long documents.",
	)
	p.text(72, y-10, 16, "F2", "What the toolbar does")
	y = p.paragraph(72, y-36,
		"Previous and Next go a page up or down. The page box shows the page in view; type a",
		"number and press Enter to go there. Zoom Out, Zoom In and the zoom pick change the",
		"size: Fit Width, Fit Page, or a level from 50% to 400%.",
	)
	p.color(0.93, 0.95, 0.98)
	p.rect(72, y-70, 468, 70)
	p.color(0.11, 0.37, 0.66)
	p.rect(72, y-70, 4, 70)
	p.color(0.15, 0.15, 0.15)
	p.text(90, y-28, 11, "F3", "Keys: Page Up and Page Down, Home and End, Ctrl+= and Ctrl+-,")
	p.text(90, y-46, 11, "F3", "and Ctrl+G to type a page number.")
	p.text(298, 40, 9, "F1", "1")
	pages = append(pages, p)

	p = &page{}
	p.color(0.15, 0.15, 0.15)
	p.text(72, 720, 20, "F2", "A drawing")
	p.paragraph(72, 690, "Vector shapes stay sharp at every zoom: they are drawn again at the new scale.")
	colors := [][3]float64{{0.86, 0.27, 0.22}, {0.95, 0.61, 0.07}, {0.18, 0.62, 0.35}, {0.11, 0.37, 0.66}, {0.55, 0.27, 0.68}}
	for i, c := range colors {
		p.color(c[0], c[1], c[2])
		p.circle(306+140*math.Cos(float64(i)*2*math.Pi/5-math.Pi/2), 470-140*math.Sin(float64(i)*2*math.Pi/5-math.Pi/2), 52)
	}
	p.color(0.15, 0.15, 0.15)
	for i := range 5 {
		a1, a2 := float64(i)*2*math.Pi/5-math.Pi/2, float64(i+2)*2*math.Pi/5-math.Pi/2
		p.line(306+140*math.Cos(a1), 470-140*math.Sin(a1), 306+140*math.Cos(a2), 470-140*math.Sin(a2), 1.5)
	}
	p.text(298, 40, 9, "F1", "2")
	pages = append(pages, p)

	p = &page{}
	p.color(0.15, 0.15, 0.15)
	p.text(72, 720, 20, "F2", "A chart")
	values := []float64{3.2, 4.8, 2.9, 6.1, 5.4, 7.3}
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun"}
	p.line(90, 420, 540, 420, 1)
	for i, v := range values {
		p.color(0.11, 0.37, 0.66)
		p.rect(110+float64(i)*72, 420, 44, v*36)
		p.color(0.15, 0.15, 0.15)
		p.text(118+float64(i)*72, 400, 11, "F1", months[i])
		p.text(118+float64(i)*72, 428+v*36, 10, "F1", fmt.Sprintf("%.1f", v))
	}
	p.text(298, 40, 9, "F1", "3")
	pages = append(pages, p)

	p = &page{}
	p.color(0.15, 0.15, 0.15)
	p.text(72, 720, 20, "F2", "The end")
	p.paragraph(72, 690, "This document was written by testdata/proof/36.5/make-sample.go.")
	p.text(298, 40, 9, "F1", "4")
	pages = append(pages, p)

	if err := os.WriteFile("sample.pdf", write(pages), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// write writes the PDF: the catalog, the pages, their contents and the three standard fonts.
func write(pages []*page) []byte {
	n := len(pages)
	objs := []string{"", ""}
	var kids []string
	fontsAt := 3 + 2*n
	for i, p := range pages {
		pageObj, contentObj := 3+2*i, 4+2*i
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObj))
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R /F2 %d 0 R /F3 %d 0 R >> >> >>",
			contentObj, fontsAt, fontsAt+1, fontsAt+2))
		s := p.b.String()
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)
	for _, f := range []string{"Helvetica", "Helvetica-Bold", "Courier"} {
		objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /"+f+" /Encoding /WinAnsiEncoding >>")
	}
	// the outline: a chapter for each page, and the toolbar's section under the first
	root := len(objs) + 1
	type entry struct {
		title    string
		page     int
		y        float64
		children []entry
	}
	outline := []entry{
		{"The PDF Viewer", 0, 792, []entry{{"What the toolbar does", 0, 560, nil}}},
		{"A drawing", 1, 792, nil}, {"A chart", 2, 792, nil}, {"The end", 3, 792, nil},
	}
	objs = append(objs, "")
	var add func(items []entry, parent int) (first, last int)
	add = func(items []entry, parent int) (int, int) {
		ids := make([]int, len(items))
		for i := range items {
			objs = append(objs, "")
			ids[i] = len(objs)
		}
		for i, it := range items {
			d := fmt.Sprintf("<< /Title (%s) /Parent %d 0 R /Dest [%d 0 R /XYZ 0 %g 0]", it.title, parent, 3+2*it.page, it.y)
			if i > 0 {
				d += fmt.Sprintf(" /Prev %d 0 R", ids[i-1])
			}
			if i+1 < len(items) {
				d += fmt.Sprintf(" /Next %d 0 R", ids[i+1])
			}
			if len(it.children) > 0 {
				f, l := add(it.children, ids[i])
				d += fmt.Sprintf(" /First %d 0 R /Last %d 0 R /Count %d", f, l, len(it.children))
			}
			objs[ids[i]-1] = d + " >>"
		}
		return ids[0], ids[len(ids)-1]
	}
	first, last := add(outline, root)
	objs[root-1] = fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count %d >>", first, last, len(outline))
	objs[0] = fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /Outlines %d 0 R /PageMode /UseOutlines >>", root)
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info << /Title (The PDF Viewer) >> >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}
