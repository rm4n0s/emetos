package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"  // the decoders of the image preview
	_ "image/jpeg" // the decoders of the image preview
	_ "image/png"  // the decoders of the image preview
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	_ "golang.org/x/image/webp" // the decoder of WebP images

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
)

// The response pane shows a response in several ways; this file decides which and makes what they show. Nothing here touches a
// widget, and everything but the pages of a large body is made off the UI thread, when the request ends.

// The kinds of body the pane tells apart.
const (
	bodyKindEmpty  = "empty"
	bodyKindJSON   = "json"
	bodyKindXML    = "xml"
	bodyKindHTML   = "html"
	bodyKindJS     = "javascript"
	bodyKindCSS    = "css"
	bodyKindText   = "text"
	bodyKindImage  = "image"
	bodyKindBinary = "binary"
)

const (
	// maxPretty is the largest body that is formatted for the Pretty view; a larger one is shown in Raw, in pages.
	maxPretty = 2 << 20
	// maxImage is the largest body decoded for the image preview.
	maxImage = 32 << 20
	// maxImagePixels is the most pixels (width times height) an image may have to be previewed; a Picture holds its own copy of them.
	maxImagePixels = 32 << 20
	// pageBytes is the size of a page of the Raw view: a body larger than a page is read one page at a time.
	pageBytes = 64 << 10
	// maxLineChars is the longest line the views draw; the rest of a longer line is cut (Copy Body and Save have all of it).
	maxLineChars = 2000
	// maxCopy is the largest body Copy Body and Open in New Editor Tab take.
	maxCopy = 8 << 20
)

// respDisplay is what the pane shows of a response, made once when the response arrives.
type respDisplay struct {
	// kind is one of the bodyKind constants and lang the name the tokenizer knows it by ("" for none).
	kind, lang string
	// mediaType is the Content-Type without its parameters, as sent or, for a body that had none, as sniffed.
	mediaType string
	// pretty holds the lines of the Pretty view, nil when there are none; why says why a kind that could have them does not.
	pretty []string
	why    string
	// pic is the decoded image of the Preview, with its size; picNote says why an image has no Preview.
	pic          *ui.Picture
	picW, picH   int
	picNote      string
	size         int64
	pages        int
	binary, text bool
}

// hasPretty says that the Pretty view has a text.
func (d *respDisplay) hasPretty() bool { return d.pretty != nil }

// textual says that the body is text (Pretty and Raw, Copy Body and Open in New Editor Tab make sense).
func (d *respDisplay) textual() bool { return d.text }

// classify tells the kind of body from its Content-Type and its first bytes.
func classify(header http.Header, head []byte) (kind, lang, mediaType string) {
	mt, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(strings.SplitN(header.Get("Content-Type"), ";", 2)[0]))
	}
	switch {
	case mt == "application/json", mt == "text/json", strings.HasSuffix(mt, "+json"):
		return bodyKindJSON, "json", mt
	case mt == "application/xhtml+xml", mt == "text/html":
		return bodyKindHTML, "html", mt
	case mt == "application/xml", mt == "text/xml", strings.HasSuffix(mt, "+xml"):
		return bodyKindXML, "xml", mt
	case mt == "text/javascript", mt == "application/javascript", mt == "application/x-javascript", mt == "application/ecmascript":
		return bodyKindJS, "javascript", mt
	case mt == "text/css":
		return bodyKindCSS, "css", mt
	case mt == "image/png", mt == "image/jpeg", mt == "image/gif", mt == "image/webp":
		return bodyKindImage, "", mt
	case strings.HasPrefix(mt, "text/"), mt == "application/x-www-form-urlencoded", mt == "application/yaml", mt == "application/x-yaml":
		return bodyKindText, "", mt
	}
	if len(head) == 0 {
		return bodyKindEmpty, "", mt
	}
	if sniffed := http.DetectContentType(head); strings.HasPrefix(sniffed, "image/") && sniffed != "image/x-icon" && sniffed != "image/bmp" {
		return bodyKindImage, "", strings.SplitN(sniffed, ";", 2)[0]
	}
	if utf8.Valid(head) && !bytes.Contains(head, []byte{0}) {
		if trimmed := bytes.TrimSpace(head); len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			return bodyKindJSON, "json", mt
		}
		return bodyKindText, "", mt
	}
	return bodyKindBinary, "", mt
}

// buildDisplay makes what the pane shows of a response. It reads the body (the cache file of a large one only for its first bytes) and
// runs off the UI thread.
func buildDisplay(resp *engine.Response) *respDisplay {
	d := &respDisplay{kind: bodyKindEmpty, pages: 1}
	if resp == nil || resp.Body == nil || resp.Body.Size() == 0 {
		return d
	}
	body := resp.Body
	d.size = body.Size()
	d.pages = int((d.size + pageBytes - 1) / pageBytes)
	head := make([]byte, 512)
	n, _ := body.ReadAt(head, 0)
	d.kind, d.lang, d.mediaType = classify(resp.Header, head[:n])
	switch d.kind {
	case bodyKindImage:
		d.previewImage(body)
		return d
	case bodyKindBinary:
		d.binary = true
		return d
	}
	d.text = true
	if d.size > maxPretty || !body.InMemory() {
		d.why = "The body is " + byteSize(d.size) + ": too large to format here, shown as it is in pages."
		return d
	}
	data, err := body.Bytes()
	if err != nil {
		d.why = "The body cannot be read: " + err.Error()
		return d
	}
	text := string(data)
	if !utf8.Valid(data) {
		text = strings.ToValidUTF8(text, "�")
	}
	if d.kind == bodyKindText && d.lang == "" && json.Valid(data) && looksLikeJSON(data) {
		d.kind, d.lang = bodyKindJSON, "json"
	}
	pretty := text
	switch d.kind {
	case bodyKindJSON:
		var out bytes.Buffer
		if json.Indent(&out, []byte(text), "", "  ") != nil {
			d.why = "The body is not valid JSON: shown as it was received."
		} else {
			pretty = out.String()
		}
	case bodyKindXML:
		if formatted, ok := indentMarkup(text, false); ok {
			pretty = formatted
		} else {
			d.why = "The body is not well formed XML: shown as it was received."
		}
	case bodyKindHTML:
		if formatted, ok := indentMarkup(text, true); ok {
			pretty = formatted
		} else {
			d.why = "The HTML could not be indented: shown as it was received."
		}
	}
	d.pretty = splitLines(pretty)
	return d
}

// looksLikeJSON is true for a text that begins with an object or an array.
func looksLikeJSON(data []byte) bool {
	t := bytes.TrimSpace(data)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

// previewImage decodes the body of an image for the Preview.
func (d *respDisplay) previewImage(body *engine.Body) {
	if !body.InMemory() || body.Size() > maxImage {
		d.picNote = "The image is " + byteSize(body.Size()) + ": too large to preview here. Save it to a file."
		return
	}
	data, err := body.Bytes()
	if err != nil {
		d.picNote = "The image cannot be read: " + err.Error()
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		d.picNote = "The image cannot be shown: " + err.Error()
		return
	}
	d.picW, d.picH = cfg.Width, cfg.Height
	if cfg.Width > ui.MaxPictureSide || cfg.Height > ui.MaxPictureSide || cfg.Width*cfg.Height > maxImagePixels {
		d.picNote = fmt.Sprintf("The image is %d x %d pixels: too large to preview here. Save it to a file.", cfg.Width, cfg.Height)
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		d.picNote = "The image cannot be shown: " + err.Error()
		return
	}
	pic, err := ui.NewPicture(img)
	if err != nil {
		d.picNote = "The image cannot be shown: " + err.Error()
		return
	}
	d.pic = pic
	if d.mediaType == "" {
		d.mediaType = "image/" + format
	}
}

// splitLines cuts a text into the lines the views draw: line ends dropped, a line over maxLineChars cut with a mark.
func splitLines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if len(l) > maxLineChars {
			cut := maxLineChars
			for cut > 0 && !utf8.RuneStart(l[cut]) {
				cut--
			}
			l = fmt.Sprintf("%s … (%d more bytes)", l[:cut], len(l)-cut)
		}
		lines[i] = l
	}
	return lines
}

// pageSpan is the byte range of a page of a body: the pages meet where a character does not begin, so no character is cut in two or
// lost.
func pageSpan(body *engine.Body, page int) (start, end int64) {
	size := body.Size()
	align := func(off int64) int64 {
		if off <= 0 || off >= size {
			return min(max(off, 0), size)
		}
		buf := make([]byte, 4)
		n, _ := body.ReadAt(buf, off)
		for i := 0; i < n && i < 3 && !utf8.RuneStart(buf[i]); i++ {
			off++
		}
		return off
	}
	return align(int64(page) * pageBytes), align(int64(page+1) * pageBytes)
}

// rawPage returns the lines of a page of the Raw view: the text of the page, or a hexadecimal dump of it for a body that is not text.
func rawPage(body *engine.Body, page int, binary bool) ([]string, error) {
	if binary {
		start := int64(page) * pageBytes
		end := min(start+pageBytes, body.Size())
		buf := make([]byte, max(end-start, 0))
		n, err := body.ReadAt(buf, start)
		if err != nil && n < len(buf) && n == 0 {
			return nil, err //nolint:wrapcheck // the body's own error, already tagged
		}
		return hexDump(buf[:n], start), nil
	}
	start, end := pageSpan(body, page)
	buf := make([]byte, max(end-start, 0))
	n, err := body.ReadAt(buf, start)
	if err != nil && n == 0 && len(buf) > 0 {
		return nil, err //nolint:wrapcheck // the body's own error, already tagged
	}
	text := string(buf[:n])
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	if text == "" {
		return []string{""}, nil
	}
	return splitLines(text), nil
}

// hexDump writes bytes as lines of "offset  16 bytes  |text|".
func hexDump(data []byte, base int64) []string {
	var lines []string
	for i := 0; i < len(data); i += 16 {
		end := min(i+16, len(data))
		var hex, txt strings.Builder
		for j := i; j < i+16; j++ {
			if j < end {
				fmt.Fprintf(&hex, "%02x ", data[j])
				if c := data[j]; c >= 0x20 && c < 0x7f {
					txt.WriteByte(c)
				} else {
					txt.WriteByte('.')
				}
			} else {
				hex.WriteString("   ")
			}
			if j == i+7 {
				hex.WriteByte(' ')
			}
		}
		lines = append(lines, fmt.Sprintf("%08x  %s |%s|", base+int64(i), hex.String(), txt.String()))
	}
	return lines
}

// ---- indenting XML and HTML ----

// markupToken is a piece of XML or HTML text: a tag, a comment, a piece of text or the verbatim content of an element.
type markupToken struct {
	kind markupKind
	text string
	name string
}

type markupKind int

const (
	mkText markupKind = iota
	mkOpen
	mkClose
	mkSelf
	mkOther // a comment, a declaration, a processing instruction, CDATA
	mkRaw   // the content of script, style, pre and textarea, kept as it is
)

// voidElements are the HTML elements that have no end tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true, "link": true,
	"meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

// rawElements are the HTML elements whose content is not markup.
var rawElements = map[string]bool{"script": true, "style": true, "textarea": true, "pre": true}

// scanMarkup cuts markup into tokens without changing any character of it.
func scanMarkup(src string, html bool) []markupToken {
	var out []markupToken
	for i := 0; i < len(src); {
		if src[i] != '<' {
			j := strings.IndexByte(src[i:], '<')
			if j < 0 {
				j = len(src) - i
			}
			out = append(out, markupToken{kind: mkText, text: src[i : i+j]})
			i += j
			continue
		}
		end := tagEnd(src, i)
		tag := src[i:end]
		tok := markupToken{kind: mkOther, text: tag}
		switch {
		case strings.HasPrefix(tag, "<!--"), strings.HasPrefix(tag, "<![CDATA["), strings.HasPrefix(tag, "<?"), strings.HasPrefix(tag, "<!"):
		case strings.HasPrefix(tag, "</"):
			tok.kind, tok.name = mkClose, tagName(tag[2:], html)
		default:
			tok.name = tagName(tag[1:], html)
			switch {
			case strings.HasSuffix(tag, "/>"), html && voidElements[tok.name]:
				tok.kind = mkSelf
			default:
				tok.kind = mkOpen
			}
		}
		out = append(out, tok)
		i = end
		if html && tok.kind == mkOpen && rawElements[tok.name] {
			lower := strings.ToLower(src[i:])
			j := strings.Index(lower, "</"+tok.name)
			if j < 0 {
				j = len(src) - i
			}
			if j > 0 {
				out = append(out, markupToken{kind: mkRaw, text: src[i : i+j]})
				i += j
			}
		}
	}
	return out
}

// tagEnd is the offset after the end of the construct that starts at i (a '<'): the '>' of a tag that is not inside a quoted value, the
// end of a comment, of CDATA or of a processing instruction. An unfinished one runs to the end of the text.
func tagEnd(src string, i int) int {
	closer := func(end string) int {
		if j := strings.Index(src[i:], end); j >= 0 {
			return i + j + len(end)
		}
		return len(src)
	}
	switch {
	case strings.HasPrefix(src[i:], "<!--"):
		return closer("-->")
	case strings.HasPrefix(src[i:], "<![CDATA["):
		return closer("]]>")
	case strings.HasPrefix(src[i:], "<?"):
		return closer("?>")
	}
	var quote byte
	for j := i + 1; j < len(src); j++ {
		c := src[j]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return j + 1
		}
	}
	return len(src)
}

// tagName is the name at the start of a tag's text (lowercase for HTML).
func tagName(s string, html bool) string {
	end := strings.IndexAny(s, " \t\r\n/>")
	if end < 0 {
		end = len(s)
	}
	if html {
		return strings.ToLower(s[:end])
	}
	return s[:end]
}

// indentMarkup indents XML or HTML, two spaces for each level: an element with nothing but one line of text stays on one line. It
// returns false, and the caller shows the text as it is, when the result would not hold the same characters (whitespace aside) as the
// text; it never changes a character.
func indentMarkup(src string, html bool) (string, bool) {
	toks := scanMarkup(src, html)
	var out strings.Builder
	depth := 0
	line := func(s string) {
		for range depth {
			out.WriteString("  ")
		}
		out.WriteString(s)
		out.WriteByte('\n')
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t.kind {
		case mkText:
			for _, l := range strings.Split(t.text, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					line(l)
				}
			}
		case mkRaw:
			// verbatim, but one level in
			for _, l := range strings.Split(strings.Trim(t.text, "\r\n"), "\n") {
				if strings.TrimSpace(l) == "" {
					continue
				}
				line(strings.TrimRight(l, " \t\r"))
			}
		case mkOpen:
			// <a>text</a> on one line
			if i+2 < len(toks) && toks[i+1].kind == mkText && toks[i+2].kind == mkClose && toks[i+2].name == t.name {
				if txt := strings.TrimSpace(toks[i+1].text); txt != "" && !strings.Contains(txt, "\n") {
					line(t.text + txt + toks[i+2].text)
					i += 2
					continue
				}
			}
			if i+1 < len(toks) && toks[i+1].kind == mkClose && toks[i+1].name == t.name {
				line(t.text + toks[i+1].text)
				i++
				continue
			}
			line(t.text)
			depth++
		case mkClose:
			depth = max(depth-1, 0)
			line(t.text)
		default:
			line(t.text)
		}
	}
	res := out.String()
	if squeeze(res) != squeeze(src) {
		return "", false
	}
	return res, true
}

// squeeze drops the whitespace of a text.
func squeeze(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
}

// ---- the other tabs ----

// headerRows lists the headers sorted by name, one row for each value.
func headerRows(h http.Header) [][2]string {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	var rows [][2]string
	for _, k := range names {
		for _, v := range h[k] {
			rows = append(rows, [2]string{k, v})
		}
	}
	return rows
}

// maskedHeaderRows is headerRows for the headers that were sent: the authorization, the cookies and the other secrets are masked as in
// the history.
func maskedHeaderRows(h http.Header) [][2]string {
	rows := headerRows(h)
	for i, r := range rows {
		if history.Sensitive(r[0]) {
			rows[i][1] = history.MaskValue(r[1])
		}
	}
	return rows
}

// timingRows are the phases of an exchange for the Timeline and the hover of the status line; a phase that did not happen is left out.
func timingRows(t engine.Timings) [][2]string {
	rows := [][2]string{}
	add := func(name string, d time.Duration, show bool) {
		if show {
			rows = append(rows, [2]string{name, fmtDuration(d)})
		}
	}
	add("DNS lookup", t.DNS, t.DNS > 0)
	add("TCP connection", t.Connect, t.Connect > 0)
	add("TLS handshake", t.TLS, t.TLS > 0)
	add("Request sent", t.Sent, true)
	add("Waiting (first byte)", t.FirstByte, true)
	add("Download", t.Download, true)
	add("Total", t.Total, true)
	return rows
}

// fmtDuration is a phase of an exchange: "< 1 ms" below a millisecond, else as durationText.
func fmtDuration(d time.Duration) string {
	if d < time.Millisecond {
		return "< 1 ms"
	}
	return durationText(d)
}

// timingHover is the text of the hover of the status line.
func timingHover(t engine.Timings) string {
	var lines []string
	for _, r := range timingRows(t) {
		lines = append(lines, fmt.Sprintf("%-22s %8s", r[0], r[1]))
	}
	if t.Reused {
		lines = append(lines, "The connection was already open.")
	}
	return strings.Join(lines, "\n")
}
