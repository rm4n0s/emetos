package markdown

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The widget of the preview (step 11.6): every element of the sample is drawn, in both themes; links call back, the code is
// highlighted with the tokens it is given, and the view scrolls to a line of the source.

// sample is the text of testdata/sample.md.
func sample(t testing.TB) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sample.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// banner makes a small PNG in a folder and returns the resolver of images for it.
func banner(t testing.TB) func(string) string {
	t.Helper()
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 160, 48))
	for y := range 48 {
		for x := range 160 {
			img.Set(x, y, color.RGBA{uint8(40 + x), uint8(100 + y), 200, 255})
		}
	}
	path := filepath.Join(dir, "banner.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return func(dest string) string {
		if dest == "banner.png" {
			return path
		}
		return ""
	}
}

// tokenize is a tokenizer of Go for the tests: words, strings and comments get the scopes the grammar gives them.
func tokenize(language, text string) ([][]extension.Token, bool) {
	if language != "go" {
		return nil, false
	}
	var out [][]extension.Token
	for _, line := range strings.Split(text, "\n") {
		var toks []extension.Token
		add := func(s, e int, scope string) {
			toks = append(toks, extension.Token{Start: s, End: e, Scopes: []string{"source.go", scope}})
		}
		for i := 0; i < len(line); {
			switch {
			case strings.HasPrefix(line[i:], "//"):
				add(i, len(line), "comment.line.double-slash.go")
				i = len(line)
			case line[i] == '"':
				j := strings.IndexByte(line[i+1:], '"') + i + 2
				add(i, j, "string.quoted.double.go")
				i = j
			case isWord(line[i]):
				j := i
				for j < len(line) && isWord(line[j]) {
					j++
				}
				scope := "source.go"
				switch line[i:j] {
				case "package", "func":
					scope = "keyword.control.go"
				case "main", "println":
					scope = "entity.name.function.go"
				}
				add(i, j, scope)
				i = j
			default:
				add(i, i+1, "source.go")
				i++
			}
		}
		out = append(out, toks)
	}
	return out, true
}

func isWord(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func TestGoldenThePreviewOfEveryElement(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			v := NewView(sample(t), Options{Tokenize: tokenize, ResolveImage: banner(t)})
			ts := uitest.Pump(t, th, gui.Size{Width: 560, Height: 1100}, v)
			for range 4 {
				ts.Pump()
			}
			uitest.MatchesGolden(ts, "preview_"+th.Name)
		})
	}
}

func TestALinkCallsBackWithItsDestination(t *testing.T) {
	var got []string
	v := NewView("see [the docs](docs/a.md) now", Options{OnLink: func(d string) { got = append(got, d) }})
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 100}, v)
	ts.Pump()
	// the text of the link is the second and third word of the line, 8.4 px a character in the test font
	at := ts.TopLeft(gluttertest.ByType[*widgets.RichText]())
	ts.TapAt(gui.Offset{Dx: at.Dx + 4*8.4 + 25, Dy: at.Dy + 8})
	ts.Pump()
	if len(got) != 1 || got[0] != "docs/a.md" {
		t.Errorf("OnLink got %v", got)
	}
}

func TestCodeIsTokenizedByItsLanguageAndAnUnknownLanguageIsPlain(t *testing.T) {
	var asked []string
	opts := Options{Tokenize: func(language, text string) ([][]extension.Token, bool) {
		asked = append(asked, language)
		return tokenize(language, text)
	}}
	v := NewView("```go\nfunc main() {}\n```\n\n```klingon\nqapla\n```\n\n```\nno language\n```\n", opts)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 300}, v)
	ts.Pump()
	ts.Pump()
	// go once, klingon once (it said no), and nothing for the block with no language; a build again asks nobody
	if strings.Join(asked, ",") != "go,klingon" {
		t.Errorf("Tokenize was asked for %v", asked)
	}
}

func TestAnImageThatIsNotLocalShowsItsAltText(t *testing.T) {
	v := NewView("![the alt](https://example.com/x.png)", Options{ResolveImage: func(string) string { return "" }})
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 100}, v)
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("[the alt]")) {
		t.Error("the alt text is not shown")
	}
	v = NewView("![the alt](x.png)", Options{})
	ts = uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 100}, v)
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("[the alt]")) {
		t.Error("with no resolver the alt text is not shown")
	}
}

func TestTheControllerScrollsToTheBlockOfALine(t *testing.T) {
	var b strings.Builder
	for i := range 60 {
		b.WriteString("## Section " + string(rune('A'+i%26)) + string(rune('a'+i/26)) + "\n\nParagraph of the section.\n\n")
	}
	ctl := NewController()
	v := NewView(b.String(), Options{Controller: ctl})
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 300}, v)
	ts.Pump()
	ts.Pump()
	if ctl.TopLine() != 0 {
		t.Errorf("the top line at the start is %d", ctl.TopLine())
	}
	// the section 30 starts at the line 4*30
	ctl.ScrollToLine(4 * 30)
	ts.Pump()
	ts.Pump()
	if got := ctl.TopLine(); got != 4*30 {
		t.Errorf("after the scroll the top line is %d, want %d", got, 4*30)
	}
	_, top := ctl.scroll.Position()
	if top <= 0 {
		t.Errorf("the scroll position is %v", top)
	}
	// a scroll before the view is laid out waits for it
	ctl2 := NewController()
	ctl2.ScrollToLine(4 * 20)
	if !ctl2.hasPending || ctl2.pending != 80 {
		t.Errorf("the scroll did not wait: %+v", ctl2)
	}
	ts2 := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 300}, NewView(b.String(), Options{Controller: ctl2}))
	for range 4 {
		ts2.Pump()
	}
	if got := ctl2.TopLine(); got != 80 {
		t.Errorf("the waiting scroll left the top line at %d, want 80", got)
	}
	// above the first block: the top
	ctl.ScrollToLine(-1)
	ts.Pump()
	if ctl.TopLine() != 0 {
		t.Errorf("a line before the first block did not scroll to the top: %d", ctl.TopLine())
	}
}

func TestANewTextKeepsTheScrollPosition(t *testing.T) {
	text := strings.Repeat("A paragraph of text that is long enough to take a line or two of the preview.\n\n", 40)
	ctl := NewController()
	source := foundation.NewValueNotifier(text)
	v := widgets.NewValueListenableBuilder(source, func(_ widgets.BuildContext, s string, _ widgets.Widget) widgets.Widget {
		return NewView(s, Options{Controller: ctl})
	}, nil)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 200}, v)
	ts.Pump()
	ctl.ScrollToLine(60)
	ts.Pump()
	_, before := ctl.scroll.Position()
	source.SetValue(text + "\nmore\n")
	ts.Pump()
	ts.Pump()
	_, after := ctl.scroll.Position()
	if before <= 0 || after != before {
		t.Errorf("the scroll position went from %v to %v", before, after)
	}
}

func TestFlowIsAtItsNaturalHeightWithCodeOnItsOwnBackground(t *testing.T) {
	src := "Some text\n\n```go\nx := 1\n```\n\nmore"
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			ts := uitest.Pump(t, th, gui.Size{Width: 400, Height: 400}, widgets.NewAlign(NewView(src, Options{Flow: true}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
			ts.Pump()
			// no scrolling: the view is as high as its blocks, not the window
			if h := ts.Size(gluttertest.ByType[*view]()).Height; h <= 0 || h >= 300 {
				t.Errorf("height = %v", h)
			}
			uitest.MatchesGolden(ts, "flow_"+th.Name)
		})
	}
}
