package poppler

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// outlineXML is pdftohtml's XML of a document with a nested outline (as Poppler 26 writes it for the sample of the proof).
const outlineXML = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pdf2xml SYSTEM "pdf2xml.dtd">

<pdf2xml producer="poppler" version="26.08.0">
<page number="1" position="absolute" top="0" left="0" height="1188" width="918">
	<fontspec id="0" size="45" family="Helvetica" color="#ffffff"/>
<text top="61" left="108" width="340" height="42" font="0"><b>The PDF Viewer</b></text>
</page>
<outline>
<item page="1">The PDF Viewer</item>
<outline>
<item page="1">What the toolbar does</item>
<outline>
<item page="2">Deeper &amp; deeper</item>
</outline>
</outline>
<item page="2">A drawing</item>
<item>Nowhere</item>
</outline>
</pdf2xml>
`

func TestParseOutline(t *testing.T) {
	tests := []struct {
		name, xml string
		want      []OutlineItem
	}{
		{"nested", outlineXML, []OutlineItem{
			{Title: "The PDF Viewer", Page: 1, Children: []OutlineItem{
				{Title: "What the toolbar does", Page: 1, Children: []OutlineItem{{Title: "Deeper & deeper", Page: 2}}},
			}},
			{Title: "A drawing", Page: 2},
			{Title: "Nowhere"},
		}},
		{"none", `<?xml version="1.0"?><pdf2xml><page number="1"><text>Hi</text></page></pdf2xml>`, nil},
		{"odd entities and characters", "<pdf2xml><outline><item page=\"3\">Caf&eacute; &nbsp;&lt;b&gt; \x01 &bogus; and\n  more</item></outline></pdf2xml>",
			[]OutlineItem{{Title: "Café <b> &bogus; and more", Page: 3}}},
		{"cut short", `<pdf2xml><outline><item page="1">One</item><item page="2">Two`, []OutlineItem{{Title: "One", Page: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOutline([]byte(tt.xml))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// wordsXHTML is pdftotext's -bbox-layout XHTML of a page.
const wordsXHTML = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd"><html xmlns="http://www.w3.org/1999/xhtml">
<head>
<title>The PDF Viewer</title>
<meta name="Producer" content="a test"/>
</head>
<body>
<doc>
  <page width="612.000000" height="792.000000">
    <flow>
      <block xMin="72.000000" yMin="57.640000" xMax="168.680000" yMax="76.140000">
        <line xMin="72.000000" yMin="57.640000" xMax="168.680000" yMax="76.140000">
          <word xMin="72.000000" yMin="57.640000" xMax="86.440000" yMax="76.140000">A</word>
          <word xMin="92.000000" yMin="57.640000" xMax="168.680000" yMax="76.140000">drawing</word>
        </line>
        <line xMin="72" yMin="80" xMax="120" yMax="90">
          <word xMin="72" yMin="80" xMax="120" yMax="90">R&amp;D&nbsp;</word>
        </line>
      </block>
    </flow>
  </page>
  <page width="100" height="100"><flow><block><line><word xMin="1" yMin="1" xMax="2" yMax="2">next</word></line></block></flow></page>
</doc>
</body>
</html>`

func TestParseWords(t *testing.T) {
	got, err := parseWords([]byte(wordsXHTML))
	if err != nil {
		t.Fatal(err)
	}
	want := PageWords{Width: 612, Height: 792, Lines: []Line{
		{Words: []Word{{"A", 72, 57.64, 86.44, 76.14}, {"drawing", 92, 57.64, 168.68, 76.14}}},
		{Words: []Word{{"R&D", 72, 80, 120, 90}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	empty, err := parseWords([]byte(`<html><body><doc><page width="10" height="20"></page></doc></body></html>`))
	if err != nil || empty.Width != 10 || len(empty.Lines) != 0 {
		t.Errorf("an empty page: %+v, %v", empty, err)
	}
}

func TestTheTextPrograms(t *testing.T) {
	bin := fakeDir(t)
	p := found(t, bin)
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_POPPLER_ARGS", args)
	t.Setenv("FAKE_PDF_PASSWORD", "secret")
	readArgs := func() []string {
		data, err := os.ReadFile(args)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(string(data), "\n")
	}
	items, err := Outline(t.Context(), p, "/w/a.pdf", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Title != "The PDF Viewer" || len(items[0].Children) != 1 {
		t.Errorf("outline %+v", items)
	}
	if got := readArgs(); !slices.Equal(got, []string{"-xml", "-i", "-q", "-stdout", "-f", "1", "-l", "1", "-upw", "secret", "/w/a.pdf"}) {
		t.Errorf("pdftohtml's arguments %q", got)
	}
	words, err := Words(t.Context(), p, "/w/a.pdf", "secret", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(words.Lines) != 2 || words.Lines[0].Words[1].Text != "drawing" {
		t.Errorf("words %+v", words)
	}
	if got := readArgs(); !slices.Equal(got, []string{"-f", "2", "-l", "2", "-bbox-layout", "-upw", "secret", "/w/a.pdf", "-"}) {
		t.Errorf("pdftotext's arguments %q", got)
	}
	text, err := PageText(t.Context(), p, "/w/a.pdf", "secret", 3)
	if err != nil {
		t.Fatal(err)
	}
	if text != "The text of page 3\n    laid out\n" {
		t.Errorf("text %q", text)
	}
	if got := readArgs(); !slices.Equal(got, []string{"-f", "3", "-l", "3", "-layout", "-enc", "UTF-8", "-upw", "secret", "/w/a.pdf", "-"}) {
		t.Errorf("pdftotext's arguments %q", got)
	}
}

func TestTheTextProgramsFail(t *testing.T) {
	bin := fakeDir(t)
	p := found(t, bin)
	gone := p
	gone.Dir = t.TempDir()
	outline := func(p Programs, pw string) error { _, err := Outline(t.Context(), p, "/w/a.pdf", pw); return err }
	words := func(p Programs, pw string) error { _, err := Words(t.Context(), p, "/w/a.pdf", pw, 1); return err }
	pageText := func(p Programs, pw string) error { _, err := PageText(t.Context(), p, "/w/a.pdf", pw, 1); return err }
	tests := []struct {
		name     string
		call     func(Programs, string) error
		env      map[string]string
		programs Programs
		password string
		route    string
	}{
		{"Outline, a password needed", outline, map[string]string{"FAKE_PDF_PASSWORD": "secret"}, p, "", "Outline.PDFPasswordNeeded"},
		{"Outline, a wrong password", outline, map[string]string{"FAKE_PDF_PASSWORD": "secret"}, p, "guess", "Outline.PDFPasswordWrong"},
		{"Outline, a damaged file", outline, map[string]string{"FAKE_PDF_DAMAGED": "Couldn't read xref table"}, p, "", "Outline.PDFOutlineFailed"},
		{"Outline, XML that cannot be read", outline, map[string]string{"FAKE_TEXT": "garbage"}, p, "", "Outline.PDFOutlineInvalid"},
		{"Outline, too much", outline, map[string]string{"FAKE_TEXT": "flood"}, p, "", "Outline.PDFOutlineInvalid"},
		{"Outline, no pdftohtml", outline, nil, gone, "", "Outline.PopplerStartFailed"},
		{"Words, a password needed", words, map[string]string{"FAKE_PDF_PASSWORD": "secret"}, p, "", "Words.PDFPasswordNeeded"},
		{"Words, a wrong password", words, map[string]string{"FAKE_PDF_PASSWORD": "secret"}, p, "guess", "Words.PDFPasswordWrong"},
		{"Words, a damaged file", words, map[string]string{"FAKE_PDF_DAMAGED": "Couldn't read xref table"}, p, "", "Words.PDFTextFailed"},
		{"Words, XHTML that cannot be read", words, map[string]string{"FAKE_TEXT": "garbage"}, p, "", "Words.PDFTextInvalid"},
		{"Words, too much", words, map[string]string{"FAKE_TEXT": "flood"}, p, "", "Words.PDFTextInvalid"},
		{"Words, no pdftotext", words, nil, gone, "", "Words.PopplerStartFailed"},
		{"PageText, a password needed", pageText, map[string]string{"FAKE_PDF_PASSWORD": "secret"}, p, "", "PageText.PDFPasswordNeeded"},
		{"PageText, a wrong password", pageText, map[string]string{"FAKE_PDF_PASSWORD": "secret"}, p, "guess", "PageText.PDFPasswordWrong"},
		{"PageText, a damaged file", pageText, map[string]string{"FAKE_PDF_DAMAGED": "Couldn't read xref table"}, p, "", "PageText.PDFTextFailed"},
		{"PageText, too much", pageText, map[string]string{"FAKE_TEXT": "flood"}, p, "", "PageText.PDFTextInvalid"},
		{"PageText, no pdftotext", pageText, nil, gone, "", "PageText.PopplerStartFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			route(t, tt.call(tt.programs, tt.password), tt.route)
		})
	}
}
