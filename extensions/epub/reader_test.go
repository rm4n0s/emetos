package epub

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// rig is the EPUB Reader activated over the fake API, with a folder for the books of the test.
type rig struct {
	t   *testing.T
	api *fakeapi.API
	x   *Extension
	dir string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	x := New().(*Extension)
	api := fakeapi.New(t, x.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: dir, ID: dir})
	if err := x.Activate(t.Context(), api); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, api: api, x: x, dir: dir}
}

// coverPNG is a picture of 120 x 60 pixels: blue with a red band.
func coverPNG(t *testing.T) string {
	img := image.NewNRGBA(image.Rect(0, 0, 120, 60))
	for y := range 60 {
		for x := range 120 {
			c := color.NRGBA{R: 40, G: 90, B: 200, A: 255}
			if y > 20 && y < 40 {
				c = color.NRGBA{R: 220, G: 40, B: 40, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// chapterXHTML wraps a body in an XHTML document with a title.
func chapterXHTML(title, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>` + title + `</title></head><body>` + body + `</body></html>`
}

// longText is paragraphs enough to scroll.
func longText(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(`<p id="p` + strconv.Itoa(i) + `">Paragraph ` + strconv.Itoa(i) + ` of a long chapter, with words enough to fill a line or two of the column of the reader.</p>`)
	}
	return b.String()
}

// writeBook writes a book of three chapters with contents, an image, a note and links, and gives its path.
func (r *rig) writeBook(name string) string {
	r.t.Helper()
	files := []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"OEBPS/content.opf", `<package><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>The Test Book</dc:title><dc:creator>Ada Example</dc:creator></metadata>
<manifest><item id="nav" href="nav.xhtml" properties="nav"/><item id="c1" href="c1.xhtml"/><item id="c2" href="c2.xhtml"/><item id="c3" href="c3.xhtml"/><item id="img" href="cover.png"/></manifest>
<spine><itemref idref="c1"/><itemref idref="c2"/><itemref idref="c3"/></spine></package>`},
		{"OEBPS/nav.xhtml", `<html><body><nav epub:type="toc"><ol><li><a href="c1.xhtml">The First Chapter</a></li>
<li><a href="c2.xhtml">The Second Chapter</a><ol><li><a href="c2.xhtml#p30">Its Middle</a></li></ol></li><li><a href="c3.xhtml">The Third Chapter</a></li></ol></nav></body></html>`},
		{"OEBPS/c1.xhtml", chapterXHTML("The First Chapter", `<h1>The First Chapter</h1>
<p>It begins with <em>emphasis</em>, <strong>strength</strong> and <code>code</code>, a <a href="c2.xhtml#p30">link to the middle of the next</a>,
a <a href="https://example.com/">link to the web</a> and a note<a href="#n1">1</a>.</p>
<img src="cover.png" alt="The cover"/>
<blockquote><p>A quote.</p></blockquote>
<ul><li>One</li><li>Two</li></ul>
<aside epub:type="footnote" id="n1"><p>The body of the note.</p></aside>`)},
		{"OEBPS/c2.xhtml", chapterXHTML("The Second Chapter", `<h1>The Second Chapter</h1>`+longText(60))},
		{"OEBPS/c3.xhtml", chapterXHTML("The Third Chapter", `<h1>The Third Chapter</h1><p>The end.</p>`)},
		{"OEBPS/cover.png", coverPNG(r.t)},
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range files {
		fw, err := w.Create(f.name)
		if err != nil {
			r.t.Fatal(err)
		}
		if _, err := fw.Write([]byte(f.body)); err != nil {
			r.t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		r.t.Fatal(err)
	}
	p := filepath.Join(r.dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// open opens a book in a view of a size and waits until its chapter is shown.
func (r *rig) open(p string, state json.RawMessage, size gui.Size) (*fakeapi.FakeFileEditor, *reader, *gluttertest.Tester) {
	r.t.Helper()
	fe, err := r.api.OpenFileEditor(ReaderID, p, state)
	if err != nil {
		r.t.Fatal(err)
	}
	fe.SetActive(true)
	rd := r.x.readers[len(r.x.readers)-1]
	ts := uitest.Pump(r.t, uitest.Themes()[0], size, widgets.NewBuilder(fe.Content.Build))
	r.settle(ts, "the chapter", func() bool { return !rd.loading && (rd.chapter != nil || rd.problem != nil) })
	return fe, rd, ts
}

// settle pumps the fake API and the tester until cond holds.
func (r *rig) settle(ts *gluttertest.Tester, what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.api.Pump()
		ts.Pump(time.Millisecond)
		if cond() {
			ts.Pump(time.Millisecond)
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

var viewSize = gui.Size{Width: 640, Height: 420}

// rich makes the text finders look in rich text too: the blocks of a chapter are rich text.
var rich = gluttertest.FindOpts{FindRichText: true}

func TestAChapterIsShownWithItsBlocks(t *testing.T) {
	r := newRig(t)
	_, rd, ts := r.open(r.writeBook("book.epub"), nil, viewSize)
	for _, text := range []string{"The First Chapter", "A quote.", "•", "One", "Two", "1 / 3", "The Test Book — The First Chapter"} {
		if !ts.Exists(gluttertest.ByText(text, rich)) {
			t.Errorf("%q is not shown", text)
		}
	}
	if !ts.Exists(gluttertest.ByType[*widgets.RawImage]()) || rd.images["OEBPS/cover.png"] == nil {
		t.Error("the image is not shown")
	}
	if ts.Exists(gluttertest.ByText("The body of the note.", rich)) {
		t.Error("the body of the note is shown before its link is clicked")
	}
}

func TestChaptersAndTheKeys(t *testing.T) {
	r := newRig(t)
	_, rd, ts := r.open(r.writeBook("book.epub"), nil, viewSize)
	steps := []struct {
		do   func()
		want int
	}{
		{func() { mustRun(t, r, CommandNextChapter) }, 1},
		{func() { ts.SendKeyEvent(services.LogicalKeyboardKeyArrowRight) }, 2},
		{func() { mustRun(t, r, CommandNextChapter) }, 2}, // the last
		{func() { ts.SendKeyEvent(services.LogicalKeyboardKeyArrowLeft) }, 1},
		{func() { mustRun(t, r, CommandPreviousChapter) }, 0},
		{func() { mustRun(t, r, CommandPreviousChapter) }, 0},
	}
	for i, st := range steps {
		st.do()
		r.settle(ts, "the chapter", func() bool { return !rd.loading })
		if rd.pos.Chapter != st.want {
			t.Errorf("step %d: chapter %d, want %d", i, rd.pos.Chapter+1, st.want+1)
		}
	}
	// the keys scroll a long chapter
	mustRun(t, r, CommandNextChapter)
	r.settle(ts, "the long chapter", func() bool { return !rd.loading && rd.pos.Chapter == 1 })
	view := rd.view
	ts.SendKeyEvent(services.LogicalKeyboardKeyPageDown)
	ts.Pump(time.Millisecond)
	if view.scroll.Offset() <= 0 {
		t.Error("Page Down did not scroll")
	}
	ts.SendKeyEvent(services.LogicalKeyboardKeyEnd)
	ts.Pump(time.Millisecond)
	if view.scroll.Offset() != view.scroll.Position().MaxScrollExtent() {
		t.Error("End did not go to the end")
	}
	ts.SendKeyEvent(services.LogicalKeyboardKeyHome)
	ts.Pump(time.Millisecond)
	if view.scroll.Offset() != 0 {
		t.Error("Home did not go to the top")
	}
}

func mustRun(t *testing.T, r *rig, id string) {
	t.Helper()
	if err := r.api.Run(id, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTheContentsGoToAnAnchor(t *testing.T) {
	r := newRig(t)
	_, rd, ts := r.open(r.writeBook("book.epub"), nil, viewSize)
	mustRun(t, r, CommandToggleContents)
	ts.Pump(time.Millisecond)
	for _, title := range []string{"CONTENTS", "The First Chapter", "The Second Chapter", "Its Middle", "The Third Chapter"} {
		if !ts.Exists(gluttertest.ByText(title)) {
			t.Errorf("the contents do not show %q", title)
		}
	}
	ts.Tap(gluttertest.ByText("Its Middle"))
	r.settle(ts, "the chapter", func() bool { return !rd.loading && rd.pos.Chapter == 1 })
	ts.Pump(time.Millisecond)
	// paragraph 30 is block 31 (after the heading), at the top of the view
	if top, ok := rd.view.topBlock(); !ok || top != 31 {
		t.Errorf("the top block is %d (%v), want 31", top, ok)
	}
	if rd.pos.Block != 31 {
		t.Errorf("the position is block %d", rd.pos.Block)
	}
}

func TestLinks(t *testing.T) {
	r := newRig(t)
	_, rd, ts := r.open(r.writeBook("book.epub"), nil, viewSize)
	// a note's link shows the note
	rd.openLink("OEBPS/c1.xhtml#n1", false)
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("The body of the note.", rich)) {
		t.Error("the note is not shown")
	}
	ts.Tap(iconButton("Close the Note"))
	ts.Pump(time.Millisecond)
	if ts.Exists(gluttertest.ByText("The body of the note.", rich)) {
		t.Error("the note did not close")
	}
	// a link of the web opens with the desktop
	rd.openLink("https://example.com/", true)
	if got := r.api.OpenedURIs(); len(got) != 1 || got[0] != "https://example.com/" {
		t.Errorf("opened %q", got)
	}
	// a link to another chapter's anchor
	rd.openLink("OEBPS/c2.xhtml#p30", false)
	r.settle(ts, "the chapter", func() bool { return !rd.loading && rd.pos.Chapter == 1 })
	ts.Pump(time.Millisecond)
	if top, _ := rd.view.topBlock(); top != 31 {
		t.Errorf("the link went to block %d, want 31", top)
	}
}

func TestTheFontSize(t *testing.T) {
	r := newRig(t)
	_, rd, ts := r.open(r.writeBook("book.epub"), nil, viewSize)
	h := func() float64 {
		els := ts.Elements(gluttertest.ByText("A quote.", rich))
		if len(els) == 0 {
			t.Fatal("no quote")
		}
		box, _ := els[0].FindRenderObject().(rendering.Box)
		return box.Size().Height
	}
	before := h()
	mustRun(t, r, CommandFontLarger)
	ts.Pump(time.Millisecond)
	if got, _ := extension.SettingValue[float64](r.api.Settings(), SettingFontSize); got != 17 {
		t.Errorf("epub.fontSize is %v, want 17", got)
	}
	if h() <= before {
		t.Errorf("the text did not grow: %v, then %v", before, h())
	}
	for range 30 {
		mustRun(t, r, CommandFontLarger)
	}
	if got := r.x.fontSize(); got != MaxFontSize {
		t.Errorf("the size went past its bound: %v", got)
	}
	_ = rd
}

func TestThePositionComesBackInANewTab(t *testing.T) {
	r := newRig(t)
	p := r.writeBook("book.epub")
	fe, rd, ts := r.open(p, nil, viewSize)
	rd.openLink("OEBPS/c2.xhtml#p30", false)
	r.settle(ts, "the chapter", func() bool { return !rd.loading && rd.pos.Chapter == 1 })
	ts.Pump(time.Millisecond)
	fe.Close()
	// a new tab, without the state of the first: the global state brings it back
	_, rd2, ts2 := r.open(p, nil, viewSize)
	ts2.Pump(time.Millisecond)
	if rd2.pos.Chapter != 1 {
		t.Fatalf("the new tab is at chapter %d", rd2.pos.Chapter+1)
	}
	if top, _ := rd2.view.topBlock(); top != 31 {
		t.Errorf("the new tab is at block %d, want 31", top)
	}
	data, err := rd2.saveState()
	if err != nil {
		t.Fatal(err)
	}
	if pos := data.(position); pos.Chapter != 1 || pos.Block != 31 {
		t.Errorf("saved %+v", pos)
	}
}

func TestABookThatIsRefused(t *testing.T) {
	r := newRig(t)
	p := filepath.Join(r.dir, "drm.epub")
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"META-INF/container.xml":  `<container><rootfiles><rootfile full-path="a.opf"/></rootfiles></container>`,
		"META-INF/encryption.xml": `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/></EncryptedData></encryption>`,
	} {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, rd, ts := r.open(p, nil, viewSize)
	if rd.problem == nil || !ts.Exists(gluttertest.ByText("This book is protected by DRM and cannot be opened.")) {
		t.Errorf("problem %v", rd.problem)
	}
	notZip := filepath.Join(r.dir, "x.epub")
	if err := os.WriteFile(notZip, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, ts2 := r.open(notZip, nil, viewSize)
	if !ts2.Exists(gluttertest.ByText("This file is not an EPUB book.")) || !ts2.Exists(gluttertest.ByText("Check Again")) {
		t.Error("a file that is not a book does not say so")
	}
}

func TestGoldens(t *testing.T) {
	for _, th := range uitest.Themes() {
		for _, contents := range []bool{false, true} {
			name := "epub_chapter_" + th.Name
			if contents {
				name = "epub_contents_" + th.Name
			}
			t.Run(name, func(t *testing.T) {
				r := newRig(t)
				fe, err := r.api.OpenFileEditor(ReaderID, r.writeBook("book.epub"), nil)
				if err != nil {
					t.Fatal(err)
				}
				fe.SetActive(true)
				rd := r.x.readers[0]
				ts := uitest.Pump(t, th, viewSize, widgets.NewBuilder(fe.Content.Build))
				r.settle(ts, "the chapter", func() bool { return !rd.loading && rd.chapter != nil })
				if contents {
					rd.toggleContents()
				}
				for range 5 {
					ts.Pump(100 * time.Millisecond)
				}
				uitest.MatchesGolden(ts, name)
			})
		}
	}
}

// iconButton finds the icon button with the tooltip.
func iconButton(tip string) gluttertest.Finder {
	return gluttertest.ByPredicate("icon button "+tip, func(w widgets.Widget) bool {
		b, ok := w.(*ui.IconButton)
		return ok && b.Tooltip() == tip
	})
}
