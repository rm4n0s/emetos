package snippet

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/test/browser/snippetVariables.test.ts
// (the tests that do not need VS Code's services: the label service, the text model and the
// workspace are replaced by the plain values the resolvers of Emetos take; the positions are
// counted from 0, in bytes)

const defaultWordSeparators = "`~!@#$%^&*()-=+[{]}\\|;:'\",.<>/?"

const sampleText = "this is line one\nthis is line two\n    this is line three"

// at is the position of a 1-based line and column of VS Code's tests (ASCII text).
func at(line, col int) buffer.Position { return buffer.Position{Line: line - 1, Col: col - 1} }

// sel is VS Code's Selection(startLine, startCol, endLine, endCol): anchor first.
func sel(l1, c1, l2, c2 int) buffer.Selection {
	return buffer.Selection{Anchor: at(l1, c1), Active: at(l2, c2)}
}

func selectionResolver(t *testing.T, text string, s buffer.Selection, index int, overtyped *Overtyped) SelectionResolver {
	t.Helper()
	snap := buffer.New(text).Snapshot()
	return SelectionResolver{Values: SelectionValuesOf(snap, s, index, edit.NewWordClassifier(defaultWordSeparators), overtyped)}
}

// resolveVariable parses $name and resolves it; it gives the text, or ok false when the variable
// kept no value.
func resolveVariable(r VariableResolver, name string) (string, bool) {
	v := parse("$" + name).Children()[0].(*Variable)
	v.Resolve(r)
	if len(v.Children()) == 0 {
		return "", false
	}
	return v.String(), true
}

func assertResolves(t *testing.T, r VariableResolver, name, want string, wantOK bool) {
	t.Helper()
	got, ok := resolveVariable(r, name)
	if ok != wantOK || got != want {
		t.Errorf("$%s = %q (resolved %v), want %q (%v)", name, got, ok, want, wantOK)
	}
}

func TestEditorVariablesBasics(t *testing.T) {
	r := CompositeResolver{FileResolver{Path: "/foo/files/text.txt"}, selectionResolver(t, sampleText, sel(1, 1, 1, 1), 0, nil)}
	assertResolves(t, r, "TM_FILENAME", "text.txt", true)
	assertResolves(t, r, "something", "", false)
}

func TestEditorVariablesFileAndDirectory(t *testing.T) {
	cases := []struct {
		name string
		path string
		want map[string]string // a variable that has no entry resolves to nothing
	}{
		{"a file", "/foo/files/text.txt", map[string]string{"TM_FILENAME": "text.txt", "TM_DIRECTORY": "/foo/files", "TM_DIRECTORY_BASE": "files", "TM_FILEPATH": "/foo/files/text.txt"}},
		// VS Code's http://www.pb.o/abc/def/ghi, which has the path /abc/def/ghi
		{"a path of a document of another kind", "/abc/def/ghi", map[string]string{"TM_FILENAME": "ghi", "TM_DIRECTORY": "/abc/def", "TM_DIRECTORY_BASE": "def", "TM_FILEPATH": "/abc/def/ghi"}},
		// VS Code's mem:fff.ts, which has no folder (an untitled document is Untitled-1)
		{"no folder", "fff.ts", map[string]string{"TM_FILENAME": "fff.ts", "TM_DIRECTORY": "", "TM_DIRECTORY_BASE": "", "TM_FILEPATH": "fff.ts"}},
		{"a file in the root", "/fff.ts", map[string]string{"TM_FILENAME": "fff.ts", "TM_DIRECTORY": "/", "TM_DIRECTORY_BASE": "", "TM_FILEPATH": "/fff.ts"}},
		{"no file", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := FileResolver{Path: tc.path}
			for _, name := range []string{"TM_FILENAME", "TM_DIRECTORY", "TM_DIRECTORY_BASE", "TM_FILEPATH"} {
				want, ok := tc.want[name]
				assertResolves(t, r, name, want, ok && (want != "" || name == "TM_DIRECTORY" || name == "TM_DIRECTORY_BASE"))
			}
		})
	}
}

func TestFilenameBase(t *testing.T) { // #32737
	cases := []struct{ path, want string }{
		{"/foo/files/text.txt", "text"},
		{"http://www.pb.o/abc/def/ghi", "ghi"}, // a path of VS Code's test, with no scheme to cut
		{"/abc/def/ghi", "ghi"},
		{".git", ".git"},
		{"foo.", "foo"},
		{"a.b.c", "a.b"},
		{"/dir/", "dir"},
	}
	for _, tc := range cases {
		assertResolves(t, FileResolver{Path: tc.path}, "TM_FILENAME_BASE", tc.want, true)
	}
}

func TestRelativeFilepath(t *testing.T) { // #114208
	assertResolves(t, FileResolver{Path: "/foo/files/text.txt"}, "RELATIVE_FILEPATH", "/foo/files/text.txt", true)
	assertResolves(t, FileResolver{Path: "/foo/files/text.txt", RelativePath: "files/text.txt"}, "RELATIVE_FILEPATH", "files/text.txt", true)
	assertResolves(t, FileResolver{}, "RELATIVE_FILEPATH", "", false)
}

func TestEditorVariablesSelection(t *testing.T) {
	cases := []struct {
		name  string
		sel   buffer.Selection
		index int
		want  map[string]string
		none  []string
	}{
		{"a selection of two lines", sel(1, 2, 2, 3), 0, map[string]string{
			"TM_SELECTED_TEXT": "his is line one\nth", "TM_CURRENT_LINE": "this is line two", "TM_LINE_INDEX": "1", "TM_LINE_NUMBER": "2",
			"CURSOR_INDEX": "0", "CURSOR_NUMBER": "1", "SELECTION": "his is line one\nth",
		}, nil},
		{"the fifth cursor", sel(1, 2, 2, 3), 4, map[string]string{"CURSOR_INDEX": "4", "CURSOR_NUMBER": "5"}, nil},
		{"a backward selection", sel(2, 3, 1, 2), 0, map[string]string{
			"TM_SELECTED_TEXT": "his is line one\nth", "TM_CURRENT_LINE": "this is line one", "TM_LINE_INDEX": "0", "TM_LINE_NUMBER": "1",
		}, nil},
		{"a cursor in a word", sel(1, 2, 1, 2), 0, map[string]string{"TM_CURRENT_WORD": "this"}, []string{"TM_SELECTED_TEXT", "SELECTION"}},
		{"a cursor in white space", sel(3, 1, 3, 1), 0, nil, []string{"TM_CURRENT_WORD"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := selectionResolver(t, sampleText, tc.sel, tc.index, nil)
			for name, want := range tc.want {
				assertResolves(t, r, name, want, true)
			}
			for _, name := range tc.none {
				assertResolves(t, r, name, "", false)
			}
		})
	}
}

func TestSnippetResolveVariable(t *testing.T) {
	r := CompositeResolver{FileResolver{Path: "/foo/files/text.txt"}, selectionResolver(t, sampleText, sel(1, 1, 1, 1), 0, nil)}
	for _, tc := range []struct{ in, before, after string }{
		{`"$TM_CURRENT_WORD"`, `""`, `"this"`},
		{`"${TM_CURRENT_WORD:foo}"`, `"foo"`, `"this"`},
		{`name=${TM_FILENAME/(.*)\..+$/$1/}`, `name=`, `name=text`},
	} {
		s := NewParser().Parse(tc.in, ParseOpts{InsertFinalTabstop: true})
		if got := s.String(); got != tc.before {
			t.Errorf("%q before: %q, want %q", tc.in, got, tc.before)
		}
		s.ResolveVariables(r)
		if got := s.String(); got != tc.after {
			t.Errorf("%q after: %q, want %q", tc.in, got, tc.after)
		}
	}
}

// nameResolver gives every variable the value, or its name without one.
type nameResolver string

func (n nameResolver) Resolve(v *Variable) (string, bool) {
	if n != "" {
		return string(n), true
	}
	return v.Name, true
}

func TestVariableSnippetTransform(t *testing.T) {
	cases := []struct{ in, want, value string }{
		{`${ThisIsAVar/([A-Z]).*(Var)/$2/}`, `Var`, ""},
		{`${ThisIsAVar/([A-Z]).*(Var)/$2-${1:/downcase}/}`, `Var-t`, ""},
		{`${Foo/(.*)/${1:+Bar}/img}`, `Bar`, ""},
		// #33162
		{`export default class ${TM_FILENAME/(\w+)\.js/$1/g}`, `export default class FooFile`, "FooFile.js"},
		{`${foobarfoobar/(foo)/${1:+FAR}/g}`, `FARbarFARbar`, ""},  // global
		{`${foobarfoobar/(foo)/${1:+FAR}/}`, `FARbarfoobar`, ""},   // first match
		{`${foobarfoobar/(bazz)/${1:+FAR}/g}`, `foobarfoobar`, ""}, // no match, no else
		{`${foobarfoobar/(foo)/${2:+FAR}/g}`, `barbar`, ""},        // bad group reference
		// #36089: alternatives and optional matches
		{`${TM_FILENAME/^(.)|(?:-(.))|(\.js)/${1:/upcase}${2:/upcase}/g}`, `MyClass`, "my-class.js"},
		{`${TM_FILENAME/^(.)|(?:-(.))|(\.js)/${1:/upcase}${2:/upcase}/g}`, `Myclass`, "myclass.js"},
		{`${TM_FILENAME/^(.)|(?:-(.))|(\.js)/${1:/upcase}${2:/upcase}/g}`, `Myclass.foo`, "myclass.foo"},
		{`${TM_FILENAME/^(.)|(?:-(.))|(\.js)/${1:/upcase}${2:/upcase}/g}`, `ThisIsAFile`, "this-is-a-file.js"},
		{`${TM_FILENAME_BASE/([A-Z][a-z]+)([A-Z][a-z]+$)?/${1:/downcase}-${2:/downcase}/g}`, `capital-case`, "CapitalCase"},
		{`${TM_FILENAME_BASE/([A-Z][a-z]+)([A-Z][a-z]+$)?/${1:/downcase}-${2:/downcase}/g}`, `capital-case-more`, "CapitalCaseMore"},
	}
	for _, tc := range cases {
		s := parse(tc.in).ResolveVariables(nameResolver(tc.value))
		if got := s.String(); got != tc.want {
			t.Errorf("%q with %q: got %q, want %q", tc.in, tc.value, got, tc.want)
		}
	}
}

func TestClipboardResolver(t *testing.T) { // #40153
	cases := []struct {
		name string
		r    ClipboardResolver
		v    string
		want string
		ok   bool
	}{
		{"nothing on the clipboard", ClipboardResolver{Index: 1, Count: 0, Spread: true}, "CLIPBOARD", "", false},
		{"text", ClipboardResolver{Text: "foo", Index: 1, Count: 0, Spread: true}, "CLIPBOARD", "foo", true},
		{"another variable", ClipboardResolver{Text: "foo", Index: 1, Spread: true}, "foo", "", false},
		{"the case matters", ClipboardResolver{Text: "foo", Index: 1, Spread: true}, "cLIPBOARD", "", false},
		{"one line, two cursors", ClipboardResolver{Text: "line1", Index: 1, Count: 2, Spread: true}, "CLIPBOARD", "line1", true},
		{"three lines, two cursors", ClipboardResolver{Text: "line1\nline2\nline3", Index: 1, Count: 2, Spread: true}, "CLIPBOARD", "line1\nline2\nline3", true},
		{"a line each: the second", ClipboardResolver{Text: "line1\nline2", Index: 1, Count: 2, Spread: true}, "CLIPBOARD", "line2", true},
		{"a line each: the first", ClipboardResolver{Text: "line1\nline2", Index: 0, Count: 2, Spread: true}, "CLIPBOARD", "line1", true},
		{"not spread", ClipboardResolver{Text: "line1\nline2", Index: 0, Count: 2}, "CLIPBOARD", "line1\nline2", true},
		// not in VS Code's tests: blank lines do not count, and neither do the kinds of line breaks
		{"blank lines are not lines", ClipboardResolver{Text: "a\n\n  \nb\r\nc", Index: 2, Count: 3, Spread: true}, "CLIPBOARD", "c", true},
		{"a carriage return", ClipboardResolver{Text: "a\rb", Index: 1, Count: 2, Spread: true}, "CLIPBOARD", "b", true},
		{"an index past the lines", ClipboardResolver{Text: "a\nb", Index: 5, Count: 2, Spread: true}, "CLIPBOARD", "a\nb", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertResolves(t, tc.r, tc.v, tc.want, tc.ok)
		})
	}
}

var timeVariables = []string{
	"CURRENT_YEAR", "CURRENT_YEAR_SHORT", "CURRENT_MONTH", "CURRENT_DATE", "CURRENT_HOUR", "CURRENT_MINUTE",
	"CURRENT_SECOND", "CURRENT_MILLISECOND", "CURRENT_DAY_NAME", "CURRENT_DAY_NAME_SHORT", "CURRENT_MONTH_NAME",
	"CURRENT_MONTH_NAME_SHORT", "CURRENT_SECONDS_UNIX", "CURRENT_MILLISECONDS_UNIX", "CURRENT_TIMEZONE_OFFSET",
	"CURRENT_TIMEZONE_NAME",
}

func TestTimeVariablesResolve(t *testing.T) { // #41631, #43140
	r := TimeResolver{Now: time.Now()}
	for _, name := range timeVariables {
		if _, ok := resolveVariable(r, name); !ok {
			t.Errorf("%s did not resolve", name)
		}
	}
	assertResolves(t, r, "CURRENT_NOT_A_VARIABLE", "", false)
}

func TestTimeVariablesAreDeterministic(t *testing.T) {
	now := time.Date(2024, 4, 15, 12, 34, 56, 7_000_000, time.UTC)
	cases := []struct {
		now  time.Time
		zone string
		want map[string]string
	}{
		{now, "", map[string]string{
			"CURRENT_YEAR": "2024", "CURRENT_YEAR_SHORT": "24", "CURRENT_MONTH": "04", "CURRENT_DATE": "15", "CURRENT_HOUR": "12",
			"CURRENT_MINUTE": "34", "CURRENT_SECOND": "56", "CURRENT_MILLISECOND": "007", "CURRENT_DAY_NAME": "Monday",
			"CURRENT_DAY_NAME_SHORT": "Mon", "CURRENT_MONTH_NAME": "April", "CURRENT_MONTH_NAME_SHORT": "Apr",
			"CURRENT_SECONDS_UNIX": "1713184496", "CURRENT_MILLISECONDS_UNIX": "1713184496007",
			"CURRENT_TIMEZONE_OFFSET": "+00:00", "CURRENT_TIMEZONE_NAME": "UTC",
		}},
		{now.In(time.FixedZone("EST", -5*3600)), "America/New_York", map[string]string{
			"CURRENT_HOUR": "07", "CURRENT_TIMEZONE_OFFSET": "-05:00", "CURRENT_TIMEZONE_NAME": "America/New_York", "CURRENT_SECONDS_UNIX": "1713184496",
		}},
		{now.In(time.FixedZone("IST", 5*3600+30*60)), "", map[string]string{
			"CURRENT_HOUR": "18", "CURRENT_MINUTE": "04", "CURRENT_TIMEZONE_OFFSET": "+05:30", "CURRENT_TIMEZONE_NAME": "IST",
		}},
		{time.Date(2000, 12, 31, 23, 59, 59, 999_000_000, time.UTC), "", map[string]string{
			"CURRENT_YEAR": "2000", "CURRENT_YEAR_SHORT": "00", "CURRENT_MONTH": "12", "CURRENT_MILLISECOND": "999",
			"CURRENT_DAY_NAME": "Sunday", "CURRENT_MONTH_NAME": "December",
		}},
		{time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC), "", map[string]string{"CURRENT_MILLISECOND": "000", "CURRENT_MONTH": "01", "CURRENT_DATE": "01"}},
	}
	for _, tc := range cases {
		r := TimeResolver{Now: tc.now, TimezoneName: tc.zone}
		for name, want := range tc.want {
			assertResolves(t, r, name, want, true)
		}
	}
}

func TestTimeVariablesDoNotChange(t *testing.T) {
	// the resolver keeps one moment: a snippet resolved later says the same
	text := "$" + strings.Join(timeVariables, " $")
	r := TimeResolver{Now: time.Date(2024, 4, 15, 12, 34, 56, 7_000_000, time.UTC)}
	first := parse(text).ResolveVariables(r).String()
	second := parse(text).ResolveVariables(r).String()
	if first != second || strings.Contains(first, "$") {
		t.Errorf("%q != %q", first, second)
	}
}

func TestFormatConditionResolve(t *testing.T) { // #53617
	s := NewParser().Parse(`${TM_LINE_NUMBER/(10)/${1:?It is:It is not}/} line 10`, ParseOpts{InsertFinalTabstop: true})
	s.ResolveVariables(nameResolver("10"))
	if got := s.String(); got != "It is line 10" {
		t.Errorf("got %q", got)
	}
	s.ResolveVariables(nameResolver("11"))
	if got := s.String(); got != "It is not line 10" {
		t.Errorf("got %q", got)
	}
}

func TestWorkspaceResolver(t *testing.T) { // #68261
	cases := []struct {
		name string
		r    WorkspaceResolver
		want map[string]string
	}{
		{"no folder", WorkspaceResolver{}, nil},
		{"a folder", WorkspaceResolver{Name: "folderName", Folder: "/folderName"}, map[string]string{"WORKSPACE_NAME": "folderName", "WORKSPACE_FOLDER": "/folderName"}},
	}
	for _, tc := range cases {
		for _, name := range []string{"WORKSPACE_NAME", "WORKSPACE_FOLDER"} {
			want, ok := tc.want[name]
			assertResolves(t, tc.r, name, want, ok)
		}
	}
	assertResolves(t, WorkspaceResolver{Name: "a", Folder: "/a"}, "OTHER", "", false)
}

func TestCommentResolver(t *testing.T) {
	r := CommentResolver{LineComment: "//", BlockCommentStart: "/*", BlockCommentEnd: "*/"}
	assertResolves(t, r, "LINE_COMMENT", "//", true)
	assertResolves(t, r, "BLOCK_COMMENT_START", "/*", true)
	assertResolves(t, r, "BLOCK_COMMENT_END", "*/", true)
	assertResolves(t, r, "TM_FILENAME", "", false)
	none := CommentResolver{BlockCommentStart: "<!--"}
	assertResolves(t, none, "LINE_COMMENT", "", false)
	assertResolves(t, none, "BLOCK_COMMENT_END", "", false)
	assertResolves(t, none, "BLOCK_COMMENT_START", "<!--", true)
}

func TestRandomResolver(t *testing.T) {
	source := func() *bytes.Reader {
		return bytes.NewReader(bytes.Repeat([]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}, 8))
	}
	patterns := map[string]*regexp.Regexp{
		"RANDOM":     regexp.MustCompile(`^\d{6}$`),
		"RANDOM_HEX": regexp.MustCompile(`^[0-9a-f]{6}$`),
		"UUID":       regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`),
	}
	for name, re := range patterns {
		t.Run(name, func(t *testing.T) {
			a, ok := resolveVariable(RandomResolver{Reader: source()}, name)
			if !ok || !re.MatchString(a) {
				t.Fatalf("%q (resolved %v)", a, ok)
			}
			if b, _ := resolveVariable(RandomResolver{Reader: source()}, name); b != a {
				t.Errorf("the same source gave %q and %q", a, b)
			}
			// the real source: the right form, and different each time (for the 122 bits of a UUID)
			real, ok := resolveVariable(RandomResolver{}, name)
			if !ok || !re.MatchString(real) {
				t.Errorf("real: %q (resolved %v)", real, ok)
			}
			if name == "UUID" {
				if other, _ := resolveVariable(RandomResolver{}, name); other == real {
					t.Error("two UUIDs are the same")
				}
			}
			// a source that is empty resolves nothing
			if got, ok := resolveVariable(RandomResolver{Reader: bytes.NewReader(nil)}, name); ok {
				t.Errorf("an empty source gave %q", got)
			}
		})
	}
	assertResolves(t, RandomResolver{}, "OTHER", "", false)
}

func TestSelectionIndentation(t *testing.T) {
	// not in VS Code's tests: the lines of a selected text are indented as the variable is
	text := "func() {\n\tfoo()\n\tbar()\n}"
	cases := []struct {
		name, snippet string
		want          string
	}{
		{"no extra indentation", "${TM_SELECTED_TEXT}", "foo()\n\tbar()"},
		{"the variable is as indented as the cursor's line", "if x {\n\t$TM_SELECTED_TEXT\n}", "foo()\n\tbar()\n}"},
		{"the variable is one more tab in", "if x {\n\t\t$TM_SELECTED_TEXT\n}", "foo()\n\t\tbar()\n}"},
		{"the last text before the variable counts", "\t\t${1:a}\n\t\t\t$SELECTION", "foo()\n\t\t\tbar()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := selectionResolver(t, text, sel(2, 2, 3, 7), 0, nil)
			if got := parse(tc.snippet).ResolveVariables(r).String(); !strings.HasSuffix(got, tc.want) {
				t.Errorf("got %q, want a suffix %q", got, tc.want)
			}
		})
	}
	// the helper on its own
	for _, tc := range []struct{ value, varIndent, lineIndent, want string }{
		{"a\nb", "\t", "", "a\n\tb"},
		{"a\r\nb\rc\nd", "  ", "", "a\r\n  b\r  c\n  d"},
		{"a\nb", "\t", "\t", "a\nb"},
		{"a\n\nb", "\t\t", "\t", "a\n\t\n\tb"},
		{"a\nb", "", "\t", "a\nb"},
	} {
		if got := reindent(tc.value, tc.varIndent, tc.lineIndent); got != tc.want {
			t.Errorf("reindent(%q, %q, %q) = %q, want %q", tc.value, tc.varIndent, tc.lineIndent, got, tc.want)
		}
	}
}

func TestSelectionOvertyped(t *testing.T) {
	// not in VS Code's tests: a cursor with no selection takes the text it typed over last
	r := selectionResolver(t, sampleText, sel(1, 1, 1, 1), 0, &Overtyped{Value: "}", Multiline: false})
	assertResolves(t, r, "TM_SELECTED_TEXT", "}", true)
	assertResolves(t, r, "SELECTION", "}", true)
	// a real selection wins
	r = selectionResolver(t, sampleText, sel(1, 1, 1, 5), 0, &Overtyped{Value: "}"})
	assertResolves(t, r, "TM_SELECTED_TEXT", "this", true)
}

func TestCompositeResolverOrder(t *testing.T) {
	r := CompositeResolver{mapResolver{"A": "first"}, mapResolver{"A": "second", "B": "second"}}
	assertResolves(t, r, "A", "first", true)
	assertResolves(t, r, "B", "second", true)
	assertResolves(t, r, "C", "", false)
	assertResolves(t, CompositeResolver{}, "A", "", false)
}

func TestIsKnownVariable(t *testing.T) {
	for _, name := range append(timeVariables, "SELECTION", "CLIPBOARD", "TM_FILENAME", "UUID", "WORKSPACE_FOLDER", "LINE_COMMENT") {
		if !IsKnownVariable(name) {
			t.Errorf("%s is not known", name)
		}
	}
	for _, name := range []string{"", "foo", "tm_filename", "TM_FILENAMES"} {
		if IsKnownVariable(name) {
			t.Errorf("%q is known", name)
		}
	}
}
