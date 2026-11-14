package poppler

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// The text of a document (SPEC §10.37 "PDF"): its outline from pdftohtml, the words of a page and their boxes from pdftotext
// -bbox-layout, and the text of a page as it is laid out from pdftotext -layout.

// OutlineItem is an entry of the outline: its title, its page (from 1; 0 when it goes nowhere) and the entries under it.
type OutlineItem struct {
	Title    string
	Page     int
	Children []OutlineItem
}

// Outline reads the outline of a document with pdftohtml (only the first page's text comes with it). It fails as Info does, with
// PDFOutlineFailed in place of PDFDamaged and PDFOutlineInvalid when the XML cannot be read; a document without an outline has none.
func Outline(ctx context.Context, p Programs, file, password string) ([]OutlineItem, error) {
	var out limitedBuffer
	args := append(append([]string{"-xml", "-i", "-q", "-stdout", "-f", "1", "-l", "1"}, passwordArgs(password)...), file)
	res := run(ctx, p.Path(ProgramHTML), args, &out)
	switch {
	case res.startErr != nil:
		return nil, errors.NewErr("PopplerStartFailed", res.startErr).SetMetadata("program", p.Path(ProgramHTML))
	case res.stopped:
		return nil, errors.NewErr("PopplerStopped", ctx.Err()).SetMetadata("program", ProgramHTML, "file", file)
	case out.over:
		return nil, errors.New("PDFOutlineInvalid", "pdftohtml wrote more than 64 MiB", "file", file)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage) && password == "":
		return nil, errors.New("PDFPasswordNeeded", "This document is protected by a password.", "file", file)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage):
		return nil, errors.New("PDFPasswordWrong", "The password is not correct.", "file", file)
	case res.exitErr != nil:
		return nil, errors.NewErr("PDFOutlineFailed", res.exitErr).SetMetadata("file", file, "message", lastLine(res.stderr))
	}
	items, err := parseOutline(out.Bytes())
	if err != nil {
		return nil, errors.NewErr("PDFOutlineInvalid", err).SetMetadata("file", file)
	}
	return items, nil
}

// lenientDecoder is a decoder of the XML of Poppler that forgives what an odd document puts in it: HTML's entities, control characters
// that XML does not allow (made spaces), and elements left open.
func lenientDecoder(data []byte) *xml.Decoder {
	clean := bytes.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return ' '
		}
		return r
	}, data)
	d := xml.NewDecoder(bytes.NewReader(clean))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	return d
}

// parseOutline reads the <outline> of pdftohtml's XML: <item page="n">title</item>, each followed by an <outline> of its children.
func parseOutline(data []byte) ([]OutlineItem, error) {
	d := lenientDecoder(data)
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, nil // no outline
		}
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "outline" {
			return readOutline(d)
		}
	}
}

// cutShort tells whether a decoder's error is the end of a document that was cut short.
func cutShort(err error) bool {
	var se *xml.SyntaxError
	return err == io.EOF || (errors.As(err, &se) && se.Msg == "unexpected EOF")
}

// readOutline reads the entries of an <outline> whose start was read, through its end; a document cut short gives what was read.
func readOutline(d *xml.Decoder) ([]OutlineItem, error) {
	var items []OutlineItem
	for {
		tok, err := d.Token()
		if cutShort(err) {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "item":
				item := OutlineItem{}
				for _, a := range t.Attr {
					if a.Name.Local == "page" {
						item.Page, _ = strconv.Atoi(a.Value)
					}
				}
				var text strings.Builder
				if err := readText(d, &text); cutShort(err) {
					return items, nil
				} else if err != nil {
					return nil, err
				}
				item.Title = strings.Join(strings.Fields(text.String()), " ")
				items = append(items, item)
			case "outline":
				children, err := readOutline(d)
				if err != nil {
					return nil, err
				}
				if len(items) > 0 {
					items[len(items)-1].Children = append(items[len(items)-1].Children, children...)
				} else {
					items = append(items, children...)
				}
			default:
				if err := d.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "outline" {
				return items, nil
			}
		}
	}
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

// Word is a word of a page and its box, in points from the top left of the page.
type Word struct {
	Text                   string
	XMin, YMin, XMax, YMax float64
}

// Line is a line of words, in their order.
type Line struct {
	Words []Word
}

// PageWords are the lines of a page.
type PageWords struct {
	Width, Height float64
	Lines         []Line
}

// Words reads the words of a page (from 1) and their boxes with pdftotext -bbox-layout. It fails as Info does, with PDFTextFailed in
// place of PDFDamaged and PDFTextInvalid when its XHTML cannot be read.
func Words(ctx context.Context, p Programs, file, password string, page int) (PageWords, error) {
	var out limitedBuffer
	n := strconv.Itoa(page)
	args := append(append([]string{"-f", n, "-l", n, "-bbox-layout"}, passwordArgs(password)...), file, "-")
	res := run(ctx, p.Path(ProgramText), args, &out)
	switch {
	case res.startErr != nil:
		return PageWords{}, errors.NewErr("PopplerStartFailed", res.startErr).SetMetadata("program", p.Path(ProgramText))
	case res.stopped:
		return PageWords{}, errors.NewErr("PopplerStopped", ctx.Err()).SetMetadata("program", ProgramText, "file", file, "page", page)
	case out.over:
		return PageWords{}, errors.New("PDFTextInvalid", "pdftotext wrote more than 64 MiB", "file", file, "page", page)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage) && password == "":
		return PageWords{}, errors.New("PDFPasswordNeeded", "This document is protected by a password.", "file", file)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage):
		return PageWords{}, errors.New("PDFPasswordWrong", "The password is not correct.", "file", file)
	case res.exitErr != nil:
		return PageWords{}, errors.NewErr("PDFTextFailed", res.exitErr).SetMetadata("file", file, "page", page, "message", lastLine(res.stderr))
	}
	words, err := parseWords(out.Bytes())
	if err != nil {
		return PageWords{}, errors.NewErr("PDFTextInvalid", err).SetMetadata("file", file, "page", page)
	}
	return words, nil
}

// parseWords reads the first <page> of pdftotext's -bbox-layout XHTML: its size, and its <line>s of <word>s with their boxes.
func parseWords(data []byte) (PageWords, error) {
	d := lenientDecoder(data)
	var pw PageWords
	var line *Line
	inPage := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return pw, nil
		}
		if err != nil {
			return PageWords{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "page":
				if inPage {
					return pw, nil // the next page: one is asked at a time
				}
				inPage = true
				pw.Width, pw.Height = attrFloat(t, "width"), attrFloat(t, "height")
			case "line":
				pw.Lines = append(pw.Lines, Line{})
				line = &pw.Lines[len(pw.Lines)-1]
			case "word":
				var text strings.Builder
				if err := readText(d, &text); err != nil {
					return PageWords{}, err
				}
				w := Word{Text: strings.TrimSpace(text.String()), XMin: attrFloat(t, "xMin"), YMin: attrFloat(t, "yMin"),
					XMax: attrFloat(t, "xMax"), YMax: attrFloat(t, "yMax")}
				if w.Text == "" {
					continue
				}
				if line == nil { // a word outside a line: a line of its own
					pw.Lines = append(pw.Lines, Line{})
					line = &pw.Lines[len(pw.Lines)-1]
				}
				line.Words = append(line.Words, w)
			}
		case xml.EndElement:
			if t.Name.Local == "line" {
				line = nil
			}
		}
	}
}

// attrFloat is the number of an attribute; 0 when it is missing or not a number.
func attrFloat(se xml.StartElement, name string) float64 {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			f, _ := strconv.ParseFloat(a.Value, 64)
			return f
		}
	}
	return 0
}

// PageText is the text of a page (from 1) as it is laid out, from pdftotext -layout. It fails as Info does, with PDFTextFailed in
// place of PDFDamaged.
func PageText(ctx context.Context, p Programs, file, password string, page int) (string, error) {
	var out limitedBuffer
	n := strconv.Itoa(page)
	args := append(append([]string{"-f", n, "-l", n, "-layout", "-enc", "UTF-8"}, passwordArgs(password)...), file, "-")
	res := run(ctx, p.Path(ProgramText), args, &out)
	switch {
	case res.startErr != nil:
		return "", errors.NewErr("PopplerStartFailed", res.startErr).SetMetadata("program", p.Path(ProgramText))
	case res.stopped:
		return "", errors.NewErr("PopplerStopped", ctx.Err()).SetMetadata("program", ProgramText, "file", file, "page", page)
	case out.over:
		return "", errors.New("PDFTextInvalid", "pdftotext wrote more than 64 MiB", "file", file, "page", page)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage) && password == "":
		return "", errors.New("PDFPasswordNeeded", "This document is protected by a password.", "file", file)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage):
		return "", errors.New("PDFPasswordWrong", "The password is not correct.", "file", file)
	case res.exitErr != nil:
		return "", errors.NewErr("PDFTextFailed", res.exitErr).SetMetadata("file", file, "page", page, "message", lastLine(res.stderr))
	}
	// pdftotext ends a page with a form feed
	return strings.TrimRight(strings.ReplaceAll(out.String(), "\f", ""), "\n") + "\n", nil
}
