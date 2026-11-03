package httpclient

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
)

func respOf(contentType string, body []byte) *engine.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &engine.Response{Status: "200 OK", StatusCode: 200, Header: h, Body: engine.NewMemoryBody(body)}
}

func TestClassifyTellsTheKindOfBody(t *testing.T) {
	png8 := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	for _, tc := range []struct {
		name, contentType string
		head              []byte
		kind, lang        string
	}{
		{"json", "application/json; charset=utf-8", []byte(`{"a":1}`), bodyKindJSON, "json"},
		{"a +json type", "application/problem+json", []byte(`{}`), bodyKindJSON, "json"},
		{"xml", "application/xml", []byte(`<a/>`), bodyKindXML, "xml"},
		{"text/xml", "text/xml", []byte(`<a/>`), bodyKindXML, "xml"},
		{"an svg is xml text", "image/svg+xml", []byte(`<svg/>`), bodyKindXML, "xml"},
		{"html", "text/html; charset=utf-8", []byte(`<p>`), bodyKindHTML, "html"},
		{"javascript", "text/javascript", []byte(`var a`), bodyKindJS, "javascript"},
		{"css", "text/css", []byte(`a{}`), bodyKindCSS, "css"},
		{"plain text", "text/plain", []byte(`hi`), bodyKindText, ""},
		{"png", "image/png", png8, bodyKindImage, ""},
		{"jpeg", "image/jpeg", nil, bodyKindImage, ""},
		{"no type, json looking", "", []byte(` {"a": 1}`), bodyKindJSON, "json"},
		{"no type, text", "", []byte("hello\n"), bodyKindText, ""},
		{"no type, an image", "", png8, bodyKindImage, ""},
		{"octet-stream with binary", "application/octet-stream", []byte{0, 1, 2, 3, 0xff}, bodyKindBinary, ""},
		{"a type with garbage", "///", []byte("abc"), bodyKindText, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.contentType != "" {
				h.Set("Content-Type", tc.contentType)
			}
			kind, lang, _ := classify(h, tc.head)
			if kind != tc.kind || lang != tc.lang {
				t.Errorf("classify = %q %q, want %q %q", kind, lang, tc.kind, tc.lang)
			}
		})
	}
}

func TestBuildDisplayFormatsEachKind(t *testing.T) {
	var img bytes.Buffer
	pic := image.NewRGBA(image.Rect(0, 0, 4, 3))
	pic.Set(1, 1, color.RGBA{R: 200, A: 255})
	if err := png.Encode(&img, pic); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		contentType string
		body        string
		kind        string
		pretty      string // the Pretty text, "" for none
		why         string // part of the reason there is a note
		hasPic      bool
	}{
		{"json is indented", "application/json", `{"a":[1,2],"b":{"c":null}}`, bodyKindJSON, "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": {\n    \"c\": null\n  }\n}", "", false},
		{"broken json stays as it came", "application/json", `{"a":`, bodyKindJSON, `{"a":`, "not valid JSON", false},
		{"xml is indented", "application/xml", `<?xml version="1.0"?><a x="1"><b>t</b><c/><d><e>1</e></d></a>`, bodyKindXML,
			"<?xml version=\"1.0\"?>\n<a x=\"1\">\n  <b>t</b>\n  <c/>\n  <d>\n    <e>1</e>\n  </d>\n</a>", "", false},
		{"html is indented", "text/html", `<!doctype html><html><body><p>Hi <b>you</b></p><br><script>if (a < b) {}</script></body></html>`, bodyKindHTML,
			"<!doctype html>\n<html>\n  <body>\n    <p>\n      Hi\n      <b>you</b>\n    </p>\n    <br>\n    <script>\n      if (a < b) {}\n    </script>\n  </body>\n</html>", "", false},
		{"text is not changed", "text/plain", "one\r\ntwo\n", bodyKindText, "one\ntwo", "", false},
		{"json without a type", "", `[1, 2]`, bodyKindJSON, "[\n  1,\n  2\n]", "", false},
		{"an image has a picture and no text", "image/png", img.String(), bodyKindImage, "", "", true},
		{"binary has no pretty text", "application/octet-stream", "\x00\x01\x02", bodyKindBinary, "", "", false},
		{"no body", "application/json", "", bodyKindEmpty, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := buildDisplay(respOf(tc.contentType, []byte(tc.body)))
			if d.kind != tc.kind {
				t.Fatalf("kind = %q, want %q", d.kind, tc.kind)
			}
			if got := strings.Join(d.pretty, "\n"); got != tc.pretty {
				t.Errorf("pretty =\n%s\nwant\n%s", got, tc.pretty)
			}
			if !strings.Contains(d.why, tc.why) {
				t.Errorf("why = %q, want it to hold %q", d.why, tc.why)
			}
			if (d.pic != nil) != tc.hasPic {
				t.Errorf("picture = %v (%q)", d.pic != nil, d.picNote)
			}
		})
	}
}

func TestIndentMarkupNeverChangesACharacter(t *testing.T) {
	for _, tc := range []struct {
		name string
		html bool
		in   string
	}{
		{"attributes with a greater-than sign", false, `<a title="x > y" b='1 > 0'><c/></a>`},
		{"cdata and comments", false, `<a><![CDATA[ <not> a tag ]]><!-- c --><b>x</b></a>`},
		{"an unclosed tag", false, `<a><b>`},
		{"a stray closing tag", false, `</a></b><c/>`},
		{"text with entities", false, `<a>1 &lt; 2 &amp;&amp; 3</a>`},
		{"unfinished tag at the end", false, `<a>text<b`},
		{"html void tags and pre", true, `<p>a<br>b<img src="x.png"></p><pre>  keep
   this</pre>`},
		{"html uppercase script", true, `<SCRIPT>var x = "</p>";</SCRIPT><P>x</P>`},
		{"a comment and a doctype", true, `<!DOCTYPE html><!-- c --><title>T</title>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := indentMarkup(tc.in, tc.html)
			if !ok {
				t.Fatalf("not indented: %q", tc.in)
			}
			if squeeze(out) != squeeze(tc.in) {
				t.Errorf("characters changed:\n%q\n%q", tc.in, out)
			}
		})
	}
}

func TestLongLinesAreCutOnACharacter(t *testing.T) {
	long := strings.Repeat("é", 3000)
	lines := splitLines("short\n" + long + "\r\nlast\n")
	if len(lines) != 3 || lines[0] != "short" || lines[2] != "last" {
		t.Fatalf("lines = %d", len(lines))
	}
	if !utf8.ValidString(lines[1]) || !strings.Contains(lines[1], "more bytes") || len(lines[1]) > maxLineChars+40 {
		t.Errorf("the long line = %d bytes, valid %v", len(lines[1]), utf8.ValidString(lines[1]))
	}
}

func TestPagesMeetWithoutLosingACharacter(t *testing.T) {
	data := []byte(strings.Repeat("é€😀 text\n", 30000))
	body := engine.NewMemoryBody(data)
	pages := int((body.Size() + pageBytes - 1) / pageBytes)
	var joined []byte
	for p := range pages {
		start, end := pageSpan(body, p)
		chunk := make([]byte, end-start)
		if n, _ := body.ReadAt(chunk, start); n != len(chunk) {
			t.Fatalf("page %d: read %d of %d", p, n, len(chunk))
		}
		if !utf8.Valid(chunk) {
			t.Fatalf("page %d starts or ends inside a character", p)
		}
		joined = append(joined, chunk...)
	}
	if !bytes.Equal(joined, data) {
		t.Errorf("the pages hold %d bytes of %d", len(joined), len(data))
	}
	lines, err := rawPage(body, 1, false)
	if err != nil || len(lines) < 1000 {
		t.Errorf("page 1: %d lines, %v", len(lines), err)
	}
}

func TestHexDump(t *testing.T) {
	got := hexDump([]byte("Hello, World!\x00\xff\x10Z"), 0)
	want := []string{
		"00000000  48 65 6c 6c 6f 2c 20 57  6f 72 6c 64 21 00 ff 10  |Hello, World!...|",
		"00000010  5a                                                |Z|",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("hexDump =\n%s", strings.Join(got, "\n"))
	}
}

func TestResponseFileName(t *testing.T) {
	for _, tc := range []struct {
		name, url, disposition, contentType string
		want                                string
	}{
		{"from the disposition", "http://h/download?id=4", `attachment; filename="report.pdf"`, "application/pdf", "report.pdf"},
		{"a disposition path is cut", "http://h/x", `attachment; filename="../../etc/passwd"`, "text/plain", "passwd.txt"},
		{"from the path", "http://h/v1/users/17.json", "", "application/json", "17.json"},
		{"an extension from the kind", "http://h/v1/users", "", "application/json", "users.json"},
		{"nothing in the path", "http://h/", "", "text/html", "response.html"},
		{"an image gets its extension", "http://h/logo", "", "image/png", "logo.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := respOf(tc.contentType, []byte("x"))
			resp.URL = tc.url
			if tc.disposition != "" {
				resp.Header.Set("Content-Disposition", tc.disposition)
			}
			d := &respDisplay{}
			d.kind, _, d.mediaType = classify(resp.Header, []byte("x"))
			if got := responseFileName(resp, d); got != tc.want {
				t.Errorf("name = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteBodyFailsWithItsOwnTags(t *testing.T) {
	dir := t.TempDir()
	body := engine.NewMemoryBody([]byte("the body"))
	to := filepath.Join(dir, "out.txt")
	if err := writeBody(context.Background(), body, to); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(to); err != nil || string(data) != "the body" {
		t.Errorf("file = %q, %v", data, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		to    string
		route string
	}{
		{"a folder that is not there", context.Background(), filepath.Join(dir, "missing", "out.txt"), "writeBody.HttpResponseFileCreateFailed"},
		{"a copy that is cancelled", cancelled, filepath.Join(dir, "cancelled.txt"), "writeBody.HttpResponseFileWriteFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := writeBody(tc.ctx, body, tc.to)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestTimingHoverListsThePhasesThatHappened(t *testing.T) {
	got := timingHover(engine.Timings{Connect: 2e6, Sent: 1e5, FirstByte: 35e6, Download: 4e6, Total: 41e6, Reused: true})
	for _, want := range []string{"TCP connection", "2 ms", "< 1 ms", "35 ms", "Total", "41 ms", "already open"} {
		if !strings.Contains(got, want) {
			t.Errorf("the hover lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "DNS") || strings.Contains(got, "TLS") {
		t.Errorf("the hover lists a phase that did not happen:\n%s", got)
	}
}
