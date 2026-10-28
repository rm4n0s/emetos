package terminal

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// Ported from VS Code: src/vs/editor/test/common/modes/linkComputer.test.ts, src/vs/workbench/contrib/terminalContrib/links/test/browser/terminalUriLinkDetector.test.ts and
// terminalLocalLinkDetector.test.ts
//
// VS Code's tests write the text to an xterm.js terminal of 80 columns and read the link ranges as cells; these write it to
// Emetos's emulator and read the cells of the logical line the same way. The two cases with Chinese text end one cell earlier
// than VS Code's expected range: its offset arithmetic (convertLinkRangeToBuffer) counts the wide characters before the link
// once too often, and here the link ends on the last cell it covers. Skipped: the Windows suites (drive letters, WSL),
// the link types and the file service of VS Code's harness (the tests here give the detector a map of the files that exist).

// lineOf writes text to an emulator of cols columns and returns the logical line that ends the text.
func lineOf(t testing.TB, text string, cols int) textLine {
	t.Helper()
	em := emulator.New(30, cols, emulator.Options{Scrollback: 100})
	if _, err := em.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	var f emulator.Frame
	var snap emulator.Frame
	em.Snapshot(&snap)
	l, ok := lineAround(em, &f, snap.Top+int64(snap.Cursor.Y), 4000)
	if !ok {
		t.Fatalf("no line for %q", text)
	}
	return l
}

// cellRange is the range [start, end] of cells (1-based, columns of the first row; the row of the end) a link covers, the way
// VS Code's tests write them.
type cellRange struct{ x0, y0, x1, y1 int }

func rangeOf(l textLine, start, end int) cellRange {
	segs := l.segments(start, end)
	first, last := segs[0], segs[len(segs)-1]
	return cellRange{first.x0 + 1, int(first.line-l.first) + 1, last.x1, int(last.line-l.first) + 1}
}

func TestWebLinks(t *testing.T) {
	tests := []struct {
		text string
		want []string
		// r is the range of the first link in 80 columns, when the test gives one: x0, y0, x1, y1
		r *cellRange
	}{
		{`x = "http://foo.bar";`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`x = (http://foo.bar);`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`x = 'http://foo.bar';`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`x =  http://foo.bar ;`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`x = <http://foo.bar>;`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`x = {http://foo.bar};`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`(see http://foo.bar)`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`[see http://foo.bar]`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`{see http://foo.bar}`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`<see http://foo.bar>`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`<url>http://foo.bar</url>`, []string{"http://foo.bar"}, &cellRange{6, 1, 19, 1}},
		{`// Click here to learn more. https://go.microsoft.com/fwlink/?LinkID=513275&clcid=0x409`, []string{"https://go.microsoft.com/fwlink/?LinkID=513275&clcid=0x409"}, &cellRange{30, 1, 7, 2}},
		{`// Click here to learn more. https://msdn.microsoft.com/en-us/library/windows/desktop/aa365247(v=vs.85).aspx`, []string{"https://msdn.microsoft.com/en-us/library/windows/desktop/aa365247(v=vs.85).aspx"}, &cellRange{30, 1, 28, 2}},
		{`// https://github.com/projectkudu/kudu/blob/master/Kudu.Core/Scripts/selectNodeVersion.js`, []string{"https://github.com/projectkudu/kudu/blob/master/Kudu.Core/Scripts/selectNodeVersion.js"}, &cellRange{4, 1, 9, 2}},
		{`<!-- !!! Do not remove !!!   WebContentRef(link:https://go.microsoft.com/fwlink/?LinkId=166007, area:Admin, updated:2015, nextUpdate:2016, tags:SqlServer)   !!! Do not remove !!! -->`, []string{"https://go.microsoft.com/fwlink/?LinkId=166007"}, &cellRange{49, 1, 14, 2}},
		{`For instructions, see https://go.microsoft.com/fwlink/?LinkId=166007.</value>`, []string{"https://go.microsoft.com/fwlink/?LinkId=166007"}, &cellRange{23, 1, 68, 1}},
		{`For instructions, see https://msdn.microsoft.com/en-us/library/windows/desktop/aa365247(v=vs.85).aspx.</value>`, []string{"https://msdn.microsoft.com/en-us/library/windows/desktop/aa365247(v=vs.85).aspx"}, &cellRange{23, 1, 21, 2}},
		{`x = "https://en.wikipedia.org/wiki/Zürich";`, []string{"https://en.wikipedia.org/wiki/Zürich"}, &cellRange{6, 1, 41, 1}},
		{`請參閱 http://go.microsoft.com/fwlink/?LinkId=761051。`, []string{"http://go.microsoft.com/fwlink/?LinkId=761051"}, &cellRange{8, 1, 52, 1}},
		{`（請參閱 http://go.microsoft.com/fwlink/?LinkId=761051）`, []string{"http://go.microsoft.com/fwlink/?LinkId=761051"}, &cellRange{10, 1, 54, 1}},
		{`x = "file:///foo.bar";`, []string{"file:///foo.bar"}, &cellRange{6, 1, 20, 1}},
		{`x = "file://c:/foo.bar";`, []string{"file://c:/foo.bar"}, &cellRange{6, 1, 22, 1}},
		{`x = "file://shares/foo.bar";`, []string{"file://shares/foo.bar"}, &cellRange{6, 1, 26, 1}},
		{`x = "file://shäres/foo.bar";`, []string{"file://shäres/foo.bar"}, &cellRange{6, 1, 26, 1}},
		{`Some text, then http://www.bing.com.`, []string{"http://www.bing.com"}, &cellRange{17, 1, 35, 1}},
		{"let url = `http://***/_api/web/lists/GetByTitle('Teambuildingaanvragen')/items`;", []string{"http://***/_api/web/lists/GetByTitle('Teambuildingaanvragen')/items"}, &cellRange{12, 1, 78, 1}},
		{`7. At this point, ServiceMain has been called.  There is no functionality presently in ServiceMain, but you can consult the [MSDN documentation](https://msdn.microsoft.com/en-us/library/windows/desktop/ms687414(v=vs.85).aspx) to add functionality as desired!`, []string{"https://msdn.microsoft.com/en-us/library/windows/desktop/ms687414(v=vs.85).aspx"}, &cellRange{66, 2, 64, 3}},
		{`let x = "http://[::1]:5000/connect/token"`, []string{"http://[::1]:5000/connect/token"}, &cellRange{10, 1, 40, 1}},
		{`2. Navigate to **https://portal.azure.com**`, []string{"https://portal.azure.com"}, &cellRange{18, 1, 41, 1}},
		{`POST|https://portal.azure.com|2019-12-05|`, []string{"https://portal.azure.com"}, &cellRange{6, 1, 29, 1}},
		{`aa  https://foo.bar/[this is foo site]  aa`, []string{"https://foo.bar/[this is foo site]"}, &cellRange{5, 1, 38, 1}},
		{`http://foo.bar http://bar.foo`, []string{"http://foo.bar", "http://bar.foo"}, &cellRange{1, 1, 14, 1}},
		{`file:///c:/folder/file:23`, []string{"file:///c:/folder/file:23"}, &cellRange{1, 1, 25, 1}},
		{`file:///c:/folder/file:23:10`, []string{"file:///c:/folder/file:23:10"}, &cellRange{1, 1, 28, 1}},
		{`Visit HTTPS://UPPER.CASE/path now`, []string{"HTTPS://UPPER.CASE/path"}, nil},
		{`ftp://not.supported/file and nothing else`, nil, nil},
		{`httpx://foo.bar`, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			l := lineOf(t, tt.text, 80)
			links := detectWebLinks(l.text)
			var got []string
			for _, wl := range links {
				got = append(got, wl.url)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("links of %q = %q, want %q", tt.text, got, tt.want)
			}
			if tt.r != nil {
				if r := rangeOf(l, links[0].start, links[0].end); r != *tt.r {
					t.Errorf("range = %+v, want %+v", r, *tt.r)
				}
			}
		})
	}
}

func TestWebLinksStopAtTheLimits(t *testing.T) {
	// VS Code's detector gives up on an address above 2048 characters; the line itself is capped at 2000 by the callers
	long := "https://" + strings.Repeat("foobarbaz/", 450)
	links := detectWebLinks(long)
	if len(links) != 1 || len(links[0].url) != len(long) {
		t.Fatalf("the detector cut the address: %d links", len(links))
	}
	if len(links[0].url) <= maxWebLinkLength {
		t.Fatalf("the test address is too short")
	}
}

// fileEnv is a world in which the files are the paths of the map; cwd and home are VS Code's test values.
func fileEnv(files ...string) linkEnv {
	set := map[string]bool{}
	for _, f := range files {
		set[f] = true
	}
	return linkEnv{cwd: "/parent/cwd", home: "/home", stat: func(p string) bool { return set[p] }}
}

// unixLinks are the links of VS Code's tests and the files they name.
var unixLinks = []struct{ link, path string }{
	{"/foo", "/foo"}, {"/foo/bar", "/foo/bar"}, {"/foo/[bar]", "/foo/[bar]"}, {"/foo/[bar].baz", "/foo/[bar].baz"},
	{"/foo/[bar]/baz", "/foo/[bar]/baz"}, {"/foo/bar+more", "/foo/bar+more"},
	{"file:///foo", "/foo"}, {"file:///foo/bar", "/foo/bar"}, {"file:///foo/bar%20baz", "/foo/bar baz"},
	{"~/foo", "/home/foo"},
	{"./foo", "/parent/cwd/foo"}, {"./$foo", "/parent/cwd/$foo"}, {"../foo", "/parent/foo"}, {"foo/bar", "/parent/cwd/foo/bar"},
	{"foo/bar+more", "/parent/cwd/foo/bar+more"},
}

// linkFormats are the formats of a link with a line and column that the detector understands; {0} is the link, {1} the line
// and {2} the column.
var linkFormats = []struct{ format, line, col string }{
	{"{0}", "", ""}, {`{0}" on line {1}`, "5", ""}, {`{0}" on line {1}, column {2}`, "5", "3"}, {`{0}":line {1}`, "5", ""},
	{`{0}":line {1}, column {2}`, "5", "3"}, {`{0}": line {1}`, "5", ""}, {`{0}": line {1}, col {2}`, "5", "3"},
	{"{0}({1})", "5", ""}, {"{0} ({1})", "5", ""}, {"{0}, {1}", "5", ""}, {"{0}({1},{2})", "5", "3"}, {"{0} ({1},{2})", "5", "3"},
	{"{0}: ({1},{2})", "5", "3"}, {"{0}({1}, {2})", "5", "3"}, {"{0} ({1}, {2})", "5", "3"}, {"{0}: ({1}, {2})", "5", "3"},
	{"{0}({1}:{2})", "5", "3"}, {"{0} ({1}:{2})", "5", "3"}, {"{0}:{1}", "5", ""}, {"{0}:{1}:{2}", "5", "3"}, {"{0} {1}:{2}", "5", "3"},
	{"{0}[{1}]", "5", ""}, {"{0} [{1}]", "5", ""}, {"{0}[{1},{2}]", "5", "3"}, {"{0} [{1},{2}]", "5", "3"}, {"{0}: [{1},{2}]", "5", "3"},
	{"{0}[{1}, {2}]", "5", "3"}, {"{0} [{1}, {2}]", "5", "3"}, {"{0}: [{1}, {2}]", "5", "3"}, {"{0}[{1}:{2}]", "5", "3"},
	{"{0} [{1}:{2}]", "5", "3"}, {`{0}",{1}`, "5", ""}, {"{0}',{1}", "5", ""}, {"{0}#{1}", "5", ""}, {"{0}#{1}:{2}", "5", "5"},
}

func formatLink(format, link, line, col string) string {
	return strings.NewReplacer("{0}", link, "{1}", line, "{2}", col).Replace(format)
}

func TestFileLinksInEveryFormat(t *testing.T) {
	for _, l := range unixLinks {
		for _, f := range linkFormats {
			formatted := formatLink(f.format, l.link, f.line, f.col)
			// the link on its own, with a space around it, in parentheses and in square brackets
			for _, wrap := range []struct{ before, after string }{{"", ""}, {" ", " "}, {"(", ")"}, {"[", "]"}} {
				text := wrap.before + formatted + wrap.after
				name := text
				t.Run(name, func(t *testing.T) {
					line := lineOf(t, text, 80)
					links := detectFileLinks(line.text, fileEnv(l.path))
					if len(links) != 1 {
						t.Fatalf("%d links in %q: %+v", len(links), text, links)
					}
					got := links[0]
					if got.path != l.path {
						t.Errorf("path = %q, want %q", got.path, l.path)
					}
					r := rangeOf(line, got.start, got.end)
					want := cellRange{1 + len(wrap.before), 1, len(formatted) + len(wrap.before), 1}
					if r != want {
						t.Errorf("range = %+v, want %+v", r, want)
					}
					wantRow := none
					if f.line != "" {
						wantRow = 5
					}
					if got.row != wantRow {
						t.Errorf("row = %d, want %d", got.row, wantRow)
					}
					if f.col != "" {
						wantCol := 3
						if f.col == "5" {
							wantCol = 5
						}
						if got.col != wantCol {
							t.Errorf("col = %d, want %d", got.col, wantCol)
						}
					}
				})
			}
		}
	}
}

func TestFileLinks(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		files []string
		want  []cellRange
	}{
		{"several links", "./foo ./bar", []string{"/parent/cwd/foo", "/parent/cwd/bar"}, []cellRange{{1, 1, 5, 1}, {7, 1, 11, 1}}},
		{"extra quotes are trimmed", `"foo"" on line 5`, []string{"/parent/cwd/foo"}, []cellRange{{1, 1, 16, 1}}},
		{"extra square brackets are trimmed", `"foo]" on line 5`, []string{"/parent/cwd/foo"}, []cellRange{{1, 1, 16, 1}}},
		{"a link after a bracket", `bar[foo:5`, []string{"/parent/cwd/foo"}, []cellRange{{5, 1, 9, 1}}},
		{"git diff", `diff --git a/foo/bar b/foo/bar`, []string{"/parent/cwd/foo/bar"}, []cellRange{{14, 1, 20, 1}, {24, 1, 30, 1}}},
		{"git diff old file", `--- a/foo/bar`, []string{"/parent/cwd/foo/bar"}, []cellRange{{7, 1, 13, 1}}},
		{"git diff new file", `+++ b/foo/bar`, []string{"/parent/cwd/foo/bar"}, []cellRange{{7, 1, 13, 1}}},
		{"a time stamp in the name", `./test-2025-04-28T11:03:09+02:00.log`, []string{"/parent/cwd/test-2025-04-28T11:03:09+02:00.log"}, []cellRange{{1, 1, 36, 1}}},
		{"a file that does not exist", `./nothing`, nil, nil},
		{"a link that wraps", strings.Repeat("x", 75) + " ./dir/file.go:12", []string{"/parent/cwd/dir/file.go"}, []cellRange{{77, 1, 12, 2}}},
		{"Python's File line", `File "/foo/bar.py", line 7, in <module>`, []string{"/foo/bar.py"}, []cellRange{{6, 1, 26, 1}}},
		{"the end of a sentence", `see ./foo.`, []string{"/parent/cwd/foo"}, []cellRange{{5, 1, 9, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := lineOf(t, tt.text, 80)
			var got []cellRange
			for _, l := range detectFileLinks(line.text, fileEnv(tt.files...)) {
				got = append(got, rangeOf(line, l.start, l.end))
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ranges of %q = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}

func TestFileLinkNeedsAFolderToResolveAgainst(t *testing.T) {
	env := fileEnv("/parent/cwd/foo")
	env.cwd = ""
	if got := detectFileLinks("./foo", env); len(got) != 0 {
		t.Errorf("a relative link with no working folder resolved: %+v", got)
	}
	env.home = ""
	if got := detectFileLinks("~/foo", env); len(got) != 0 {
		t.Errorf("a link into the home folder with no home resolved: %+v", got)
	}
}

func TestALineLongerThanTheLimitHasNoFileLinks(t *testing.T) {
	text := "./foo " + strings.Repeat("x", maxLinkLine)
	if got := detectFileLinks(text, fileEnv("/parent/cwd/foo")); len(got) != 0 {
		t.Errorf("links in a line of %d characters: %+v", len(text), got)
	}
}
