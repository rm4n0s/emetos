package terminal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// linkRig is a pane over a folder with a few files, recording what a click on a link opens.
type linkRig struct {
	*paneRig
	dir     string
	files   []openedFile
	links   []openedLink
	altMode bool
}

type openedFile struct {
	path     string
	row, col int
}

type openedLink struct {
	target      string
	fromProgram bool
}

// newLinkRig makes the folder (main.go, sub/util.go, a link out of the folder, a folder) and a pane whose shell is in it.
func newLinkRig(t *testing.T, th uitest.Theme) *linkRig {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"main.go", "sub/util.go", "README.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(dir, "escape.go")); err != nil {
		t.Fatal(err)
	}
	r := &linkRig{dir: dir}
	r.paneRig = newFakePane(t, th, gui.Size{Width: 700, Height: 260}, nil)
	r.env.folder = func() string { return dir }
	r.env.home = outside
	r.env.altClick = func() bool { return r.altMode }
	r.env.openFile = func(path string, row, col int) { r.files = append(r.files, openedFile{path, row, col}) }
	r.env.openLink = func(target string, fromProgram bool) { r.links = append(r.links, openedLink{target, fromProgram}) }
	// the shell reports the folder it is in, as bash does with the integration of the next steps
	r.write(t, "\x1b]7;file://"+dir+"\x1b\\")
	return r
}

// hover moves the pointer over a cell and waits for the answer about the files of the line.
func (r *linkRig) hover(t *testing.T, col, row int, wantLink bool) {
	t.Helper()
	r.ts.Hover(r.cell(col, row))
	deadline := time.Now().Add(5 * time.Second)
	for (r.pv.hover.link != nil) != wantLink {
		if time.Now().After(deadline) {
			t.Fatalf("the link under the pointer: %+v, want a link %v", r.pv.hover.link, wantLink)
		}
		time.Sleep(2 * time.Millisecond)
		r.settle()
	}
	r.settle()
}

// ctrlClick clicks a cell with the key of links held and waits for the link to open; expectNone waits a moment only.
func (r *linkRig) ctrlClick(t *testing.T, col, row int, mod services.LogicalKeyboardKey, expectNone ...bool) {
	t.Helper()
	r.ts.SendKeyDownEvent(mod)
	r.ts.TapAt(r.cell(col, row))
	r.ts.SendKeyUpEvent(mod)
	wait := 5 * time.Second
	if len(expectNone) > 0 {
		wait = 100 * time.Millisecond
	}
	deadline := time.Now().Add(wait)
	for len(r.files)+len(r.links) == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
		r.settle()
	}
	r.settle()
}

func TestAFileLinkIsUnderlinedWithATooltipAndOpensWithCtrlClick(t *testing.T) {
	r := newLinkRig(t, uitest.Themes()[0])
	r.write(t, "$ go build ./...\r\nmain.go:3:1: undefined: x\r\n./sub/util.go:12: too many arguments\r\n$ ")
	r.hover(t, 2, 1, true)
	l := r.pv.hover.link
	if l.kind != linkFile || l.path != filepath.Join(r.dir, "main.go") || l.row != 3 || l.col != 1 {
		t.Fatalf("link = %+v", l)
	}
	// the whole "main.go:3:1" is underlined, nothing else
	if len(l.segs) != 1 || l.segs[0].x0 != 0 || l.segs[0].x1 != len("main.go:3:1") {
		t.Errorf("the cells of the link = %+v", l.segs)
	}
	// the tooltip shows after a moment
	if r.ts.Exists(gluttertest.ByText("Follow link (ctrl + click)")) {
		t.Error("the tooltip shows at once")
	}
	r.ts.Pump(tooltipDelay + 100*time.Millisecond)
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("Follow link (ctrl + click)")) {
		t.Error("the tooltip does not show after the delay")
	}
	// a click without Ctrl selects (nothing opens)
	r.ts.TapAt(r.cell(2, 1))
	r.settle()
	if len(r.files) != 0 {
		t.Errorf("a click without Ctrl opened %+v", r.files)
	}
	// Ctrl+click opens the file at the line and column
	r.ctrlClick(t, 2, 1, services.LogicalKeyboardKeyControl)
	if want := (openedFile{filepath.Join(r.dir, "main.go"), 3, 1}); len(r.files) != 1 || r.files[0] != want {
		t.Errorf("opened %+v, want %+v", r.files, want)
	}
	// a relative path with a line and no column
	r.files = nil
	r.hover(t, 5, 2, true)
	r.ctrlClick(t, 5, 2, services.LogicalKeyboardKeyControl)
	if want := (openedFile{filepath.Join(r.dir, "sub", "util.go"), 12, none}); len(r.files) != 1 || r.files[0] != want {
		t.Errorf("opened %+v, want %+v", r.files, want)
	}
	// leaving the link ends the underline and the tooltip
	r.ts.Hover(r.cell(40, 5))
	r.settle()
	if r.pv.hover.link != nil || r.ts.Exists(gluttertest.ByText("Follow link (ctrl + click)")) {
		t.Errorf("the link stays after the pointer left: %+v", r.pv.hover.link)
	}
}

func TestOnlyFilesOfTheWorkspaceAreLinks(t *testing.T) {
	r := newLinkRig(t, uitest.Themes()[0])
	r.write(t, "missing.go:1:1\r\nescape.go:2:2\r\nsub:1\r\n/etc/hostname:1\r\n~/secret.go:1\r\nREADME.md\r\n")
	for row, name := range []string{"a file that does not exist", "a link that leaves the folder", "a folder", "a file outside the folder", "a file in the home folder outside the folder"} {
		r.ts.Hover(r.cell(2, row))
		time.Sleep(30 * time.Millisecond)
		r.settle()
		r.settle()
		if r.pv.hover.link != nil {
			t.Errorf("%s is a link: %+v", name, r.pv.hover.link)
		}
	}
	r.hover(t, 2, 5, true)
	if l := r.pv.hover.link; l.path != filepath.Join(r.dir, "README.md") || l.row != none {
		t.Errorf("README.md = %+v", l)
	}
	// with no folder open there are no file links
	r.env.folder = func() string { return "" }
	r.pv.linkCache = map[string]*lineLinks{}
	r.pv.leaveHover()
	r.ts.Hover(r.cell(2, 5))
	time.Sleep(30 * time.Millisecond)
	r.settle()
	if r.pv.hover.link != nil {
		t.Errorf("a link with no folder: %+v", r.pv.hover.link)
	}
}

func TestWebAndHyperlinksOpenWithCtrlClick(t *testing.T) {
	r := newLinkRig(t, uitest.Themes()[0])
	r.write(t, "see https://example.com/docs?a=b.\r\n\x1b]8;;https://emetos.dev/guide\x1b\\the guide\x1b]8;;\x1b\\ and more\r\n$ ")
	r.hover(t, 8, 0, true)
	if l := r.pv.hover.link; l.kind != linkWeb || l.url != "https://example.com/docs?a=b" {
		t.Fatalf("link = %+v", l)
	}
	r.ctrlClick(t, 8, 0, services.LogicalKeyboardKeyControl)
	if want := (openedLink{"https://example.com/docs?a=b", false}); len(r.links) != 1 || r.links[0] != want {
		t.Errorf("opened %+v, want %+v", r.links, want)
	}
	r.links = nil
	r.hover(t, 3, 1, true)
	if l := r.pv.hover.link; l.kind != linkOSC8 || l.url != "https://emetos.dev/guide" || len(l.segs) != 1 || l.segs[0].x1 != len("the guide") {
		t.Fatalf("hyperlink = %+v", l)
	}
	r.ctrlClick(t, 3, 1, services.LogicalKeyboardKeyControl)
	if want := (openedLink{"https://emetos.dev/guide", true}); len(r.links) != 1 || r.links[0] != want {
		t.Errorf("opened %+v, want %+v", r.links, want)
	}
	// the text after the hyperlink is no link
	r.ts.Hover(r.cell(14, 1))
	r.settle()
	if r.pv.hover.link != nil {
		t.Errorf("a link on the text after the hyperlink: %+v", r.pv.hover.link)
	}
}

func TestAltClickFollowsLinksWhenTheMultiCursorModifierIsCtrl(t *testing.T) {
	r := newLinkRig(t, uitest.Themes()[0])
	r.altMode = true
	r.write(t, "https://example.com\r\n$ ")
	r.hover(t, 3, 0, true)
	r.ctrlClick(t, 3, 0, services.LogicalKeyboardKeyControl, true) // Ctrl does nothing now: it selects
	if len(r.links) != 0 {
		t.Fatalf("Ctrl+click opened %+v", r.links)
	}
	r.ts.Pump(tooltipDelay + 100*time.Millisecond)
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("Follow link (alt + click)")) {
		t.Error("the tooltip does not name the Alt key")
	}
	r.ctrlClick(t, 3, 0, services.LogicalKeyboardKeyAlt)
	if len(r.links) != 1 {
		t.Errorf("Alt+click opened %+v", r.links)
	}
}

func TestAProgramThatTracksTheMouseHasNoLinks(t *testing.T) {
	r := newLinkRig(t, uitest.Themes()[0])
	r.write(t, "https://example.com\x1b[?1003h\x1b[?1006h")
	r.ts.Hover(r.cell(3, 0))
	time.Sleep(30 * time.Millisecond)
	r.settle()
	if r.pv.hover.link != nil {
		t.Errorf("a link while the program tracks the mouse: %+v", r.pv.hover.link)
	}
}

func TestGoldenALinkUnderTheMouse(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newLinkRig(t, th)
			r.write(t, "$ go vet ./...\r\nmain.go:3:1: undefined: x\r\nsee https://example.com/guide for more\r\n$ ")
			r.hover(t, 12, 2, true)
			r.ts.Pump(tooltipDelay + 100*time.Millisecond)
			r.settle()
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "pane_link_"+th.Name)
		})
	}
}
