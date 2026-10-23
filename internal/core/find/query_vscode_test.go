package find

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/vstest"
)

// Ported from VS Code: src/vs/editor/test/common/model/textModelSearch.test.ts
//
// VS Code's lines and columns are one-based and count UTF-16 units; vstest turns them into the
// zero-based lines and byte columns of Emetos. The tests that run a search over a CRLF text are
// not ported (SPEC §6.1: the text is held with line feeds only), nor is the check of the compiled
// JavaScript regular expression in parseSearchRequest (the same facts are checked on Query).

const usualWordSeparators = edit.DefaultWordSeparators

func snapshotOf(text string) buffer.Snapshot { return buffer.New(text).Snapshot() }

func endOf(snap buffer.Snapshot) buffer.Position { return snap.PositionAt(snap.Len()) }

// span is VS Code's one-based range, as the tests write it.
type span [4]int

func rangesOf(t *testing.T, text string, spans []span) []buffer.Range {
	t.Helper()
	out := make([]buffer.Range, len(spans))
	for i, s := range spans {
		out[i] = vstest.Range(t, text, s[0], s[1], s[2], s[3])
	}
	return out
}

// assertFindMatches checks FindMatches, FindNext and FindPrevious against the expected ranges.
func assertFindMatches(t *testing.T, text, search string, isRegex, matchCase bool, words string, expected []span) {
	t.Helper()
	want := rangesOf(t, text, expected)
	snap := snapshotOf(text)
	q := Params{Search: search, IsRegex: isRegex, MatchCase: matchCase, WordSeparators: words}.Query()
	if q == nil {
		t.Fatalf("%q cannot be searched", search)
	}
	var got []buffer.Range
	for _, m := range q.FindMatches(snap, buffer.Range{End: endOf(snap)}, false, 1000) {
		got = append(got, m.Range)
	}
	if !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("FindMatches = %v, want %v", got, want)
	}

	// FindNext
	if len(want) > 0 {
		if m, ok := q.FindNext(snap, buffer.Position{}, false); !ok || m.Range != want[0] {
			t.Errorf("FindNext from the start = %v %v, want %v", m.Range, ok, want[0])
		}
	}
	for _, w := range want {
		if m, ok := q.FindNext(snap, w.Start, false); !ok || m.Range != w {
			t.Errorf("FindNext from %v = %v %v, want %v", w.Start, m.Range, ok, w)
		}
	}

	// FindPrevious
	if len(want) > 0 {
		if m, ok := q.FindPrevious(snap, endOf(snap), false); !ok || m.Range != want[len(want)-1] {
			t.Errorf("FindPrevious from the end = %v %v, want %v", m.Range, ok, want[len(want)-1])
		}
	}
	for _, w := range want {
		if m, ok := q.FindPrevious(snap, w.End, false); !ok || m.Range != w {
			t.Errorf("FindPrevious from %v = %v %v, want %v", w.End, m.Range, ok, w)
		}
	}
}

var regularText = strings.Join([]string{
	"This is some foo - bar text which contains foo and bar - as in Barcelona.",
	"Now it begins a word fooBar and now it is caps Foo-isn't this great?",
	"And here's a dull line with nothing interesting in it",
	"It is also interesting if it's part of a word like amazingFooBar",
	"Again nothing interesting here",
}, "\n")

func TestFindMatchesVSCode(t *testing.T) {
	foo := []span{{1, 14, 1, 17}, {1, 44, 1, 47}, {2, 22, 2, 25}, {2, 48, 2, 51}, {4, 59, 4, 62}}
	cases := []struct {
		name      string
		text      string
		search    string
		isRegex   bool
		matchCase bool
		words     string
		want      []span
	}{
		{"Simple find", regularText, "foo", false, false, "", foo},
		{"Case sensitive find", regularText, "foo", false, true, "", []span{{1, 14, 1, 17}, {1, 44, 1, 47}, {2, 22, 2, 25}}},
		{"Whole words find", regularText, "foo", false, false, usualWordSeparators, []span{{1, 14, 1, 17}, {1, 44, 1, 47}, {2, 48, 2, 51}}},
		{"/^/ find", regularText, "^", true, false, "", []span{{1, 1, 1, 1}, {2, 1, 2, 1}, {3, 1, 3, 1}, {4, 1, 4, 1}, {5, 1, 5, 1}}},
		{"/$/ find", regularText, "$", true, false, "", []span{{1, 74, 1, 74}, {2, 69, 2, 69}, {3, 54, 3, 54}, {4, 65, 4, 65}, {5, 31, 5, 31}}},
		{"/.*/ find", regularText, ".*", true, false, "", []span{{1, 1, 1, 74}, {2, 1, 2, 69}, {3, 1, 3, 54}, {4, 1, 4, 65}, {5, 1, 5, 31}}},
		{"/^$/ find", strings.Join([]string{
			"This is some foo - bar text which contains foo and bar - as in Barcelona.",
			"",
			"And here's a dull line with nothing interesting in it",
			"",
			"Again nothing interesting here",
		}, "\n"), "^$", true, false, "", []span{{2, 1, 2, 1}, {4, 1, 4, 1}}},
		{"multiline find 1", "Just some text text\nJust some text text\nsome text again\nagain some text", "text\\n", true, false, "",
			[]span{{1, 16, 2, 1}, {2, 16, 3, 1}}},
		{"multiline find 2", "Just some text text\nJust some text text\nsome text again\nagain some text", "text\\nJust", true, false, "",
			[]span{{1, 16, 2, 5}}},
		{"multiline find 3", "Just some text text\nJust some text text\nsome text again\nagain some text", "\\nagain", true, false, "",
			[]span{{3, 16, 4, 6}}},
		{"multiline find 4", "Just some text text\nJust some text text\nsome text again\nagain some text", ".*\\nJust.*\\n", true, false, "",
			[]span{{1, 1, 3, 1}}},
		{"multiline find with line beginning regex", "if\nelse\n\nif\nelse", "^if\\nelse", true, false, "",
			[]span{{1, 1, 2, 5}, {4, 1, 5, 5}}},
		{"matching empty lines using boundary expression", "if\n\nelse\n  \nif\n \nelse", "^\\s*$\\n", true, false, "",
			[]span{{2, 1, 3, 1}, {4, 1, 5, 1}, {6, 1, 7, 1}}},
		{"matching lines starting with A and ending with B", "a if b\na\nab\neb", "^a.*b$", true, false, "",
			[]span{{1, 1, 1, 7}, {3, 1, 3, 3}}},
		{"multiline find with line ending regex", "if\nelse\n\nif\nelseif\nelse", "if\\nelse$", true, false, "",
			[]span{{1, 1, 2, 5}, {5, 5, 6, 5}}},
		{"issue #4836 - ^.*$", "Just some text text\n\nsome text again\n\nagain some text", "^.*$", true, false, "",
			[]span{{1, 1, 1, 20}, {2, 1, 2, 1}, {3, 1, 3, 16}, {4, 1, 4, 1}, {5, 1, 5, 16}}},
		{"multiline find for non-regex string", "Just some text text\nsome text text\nsome text again\nagain some text\nbut not some", "text\nsome", false, false, "",
			[]span{{1, 16, 2, 5}, {2, 11, 3, 5}}},
		{"issue #3623: Match whole word does not work for not latin characters", "я\nкомпилятор\nобфускация\n:я-я", "я", false, false, usualWordSeparators,
			[]span{{1, 1, 1, 2}, {4, 2, 4, 3}, {4, 4, 4, 5}}},
		{"issue #27459: Match whole words regression",
			"this._register(this._textAreaInput.onKeyDown((e: IKeyboardEvent) => {\n\tthis._viewController.emitKeyDown(e);\n}));",
			"((e: ", false, false, usualWordSeparators, []span{{1, 45, 1, 50}}},
		{"issue #27594: Search results disappear", "this.server.listen(0);", "listen(", false, false, usualWordSeparators, []span{{1, 13, 1, 20}}},
		{"issue #53415: \\W after digits", "text\n180702-\n180703-180704", "\\d{6}-\\W", true, false, "", []span{{2, 1, 3, 1}}},
		{"issue #53415: \\W matches a line break", "Just some text\n\nJust", "\\W", true, false, "",
			[]span{{1, 5, 1, 6}, {1, 10, 1, 11}, {1, 15, 2, 1}, {2, 1, 3, 1}}},
		{"issue #53415: \\W and a tab", "Just some text\n\tJust\nJust", "\\W", true, false, "",
			[]span{{1, 5, 1, 6}, {1, 10, 1, 11}, {1, 15, 2, 1}, {2, 1, 2, 2}, {2, 6, 3, 1}}},
		{"issue #53415: a line break is one non-word character", "Just  some text\n\nJust", "\\W{2}", true, false, "",
			[]span{{1, 5, 1, 7}, {1, 16, 3, 1}}},
		{"Simple find using unicode escape sequences", regularText, "\\u{0066}\\u006f\\u006F", true, false, "", foo},
		// 1[Laptop]1: there are no matches inside the emoji
		{"issue #100134: surrogate pairs", "1\U0001F4BB1", "()", true, false, "", []span{{1, 1, 1, 1}, {1, 2, 1, 2}, {1, 4, 1, 4}, {1, 5, 1, 5}}},
		// 1[Cat Face][ZWJ][Laptop]1: there are matches between the emoji and the ZWJ
		{"issue #100134: zero width joiner", "1\U0001F431\u200d\U0001F4BB1", "()", true, false, "",
			[]span{{1, 1, 1, 1}, {1, 2, 1, 2}, {1, 4, 1, 4}, {1, 5, 1, 5}, {1, 7, 1, 7}, {1, 8, 1, 8}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFindMatches(t, tc.text, tc.search, tc.isRegex, tc.matchCase, tc.words, tc.want)
		})
	}
}

// at is a zero-based position in ASCII text from VS Code's one-based line and column.
func at(line, col int) buffer.Position { return buffer.Position{Line: line - 1, Col: col - 1} }

func rng(sl, sc, el, ec int) buffer.Range { return buffer.Range{Start: at(sl, sc), End: at(el, ec)} }

func mustQuery(t *testing.T, search string, isRegex, matchCase bool) *Query {
	t.Helper()
	q := Params{Search: search, IsRegex: isRegex, MatchCase: matchCase}.Query()
	if q == nil {
		t.Fatalf("%q cannot be searched", search)
	}
	return q
}

func TestFindNextMatchStepsVSCode(t *testing.T) {
	type step struct {
		from buffer.Position
		want buffer.Range
	}
	cases := []struct {
		name    string
		text    string
		search  string
		isRegex bool
		steps   []step
	}{
		{"without regex", "line line one\nline two\nthree", "line", false, []step{
			{at(1, 1), rng(1, 1, 1, 5)}, {at(1, 5), rng(1, 6, 1, 10)}, {at(1, 3), rng(1, 6, 1, 10)},
			{at(1, 10), rng(2, 1, 2, 5)}, {at(2, 5), rng(1, 1, 1, 5)},
		}},
		{"with beginning boundary regex", "line one\nline two\nthree", "^line", true, []step{
			{at(1, 1), rng(1, 1, 1, 5)}, {at(1, 5), rng(2, 1, 2, 5)}, {at(1, 3), rng(2, 1, 2, 5)}, {at(2, 5), rng(1, 1, 1, 5)},
		}},
		{"with beginning boundary regex and repetitive beginnings", "line line one\nline two\nthree", "^line", true, []step{
			{at(1, 1), rng(1, 1, 1, 5)}, {at(1, 5), rng(2, 1, 2, 5)}, {at(1, 3), rng(2, 1, 2, 5)}, {at(2, 5), rng(1, 1, 1, 5)},
		}},
		{"with beginning boundary multiline regex and repetitive beginnings", "line line one\nline two\nline three\nline four", "^line.*\\nline", true, []step{
			{at(1, 1), rng(1, 1, 2, 5)}, {at(2, 5), rng(3, 1, 4, 5)}, {at(2, 1), rng(2, 1, 3, 5)},
		}},
		{"with ending boundary regex", "one line line\ntwo line\nthree", "line$", true, []step{
			{at(1, 1), rng(1, 10, 1, 14)}, {at(1, 4), rng(1, 10, 1, 14)}, {at(1, 14), rng(2, 5, 2, 9)}, {at(2, 9), rng(1, 10, 1, 14)},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := snapshotOf(tc.text)
			q := mustQuery(t, tc.search, tc.isRegex, false)
			for i, s := range tc.steps {
				m, ok := q.FindNext(snap, s.from, false)
				if !ok || m.Range != s.want || m.Groups != nil {
					t.Errorf("step %d: FindNext(%v) = %v %v, want %v", i, s.from, m.Range, ok, s.want)
				}
			}
		})
	}
}

func TestFindCapturingMatchesVSCode(t *testing.T) {
	snap := snapshotOf("one line line\ntwo line\nthree")
	full := buffer.Range{End: endOf(snap)}
	line := []string{"line", "line", "in"}
	multi := []string{"line\n", "line", "in"}

	q := mustQuery(t, "(l(in)e)", true, false)
	want := []Match{{rng(1, 5, 1, 9), line}, {rng(1, 10, 1, 14), line}, {rng(2, 5, 2, 9), line}}
	if got := q.FindMatches(snap, full, true, 100); !reflect.DeepEqual(got, want) {
		t.Errorf("findMatches with capturing matches: %v", got)
	}
	if m, ok := q.FindNext(snap, at(1, 1), true); !ok || !reflect.DeepEqual(m, want[0]) {
		t.Errorf("findNextMatch with capturing matches: %v", m)
	}
	if m, ok := q.FindPrevious(snap, at(1, 1), true); !ok || !reflect.DeepEqual(m, want[2]) {
		t.Errorf("findPreviousMatch with capturing matches: %v", m)
	}

	q = mustQuery(t, "(l(in)e)\\n", true, false)
	wantMulti := []Match{{rng(1, 10, 2, 1), multi}, {rng(2, 5, 3, 1), multi}}
	if got := q.FindMatches(snap, full, true, 100); !reflect.DeepEqual(got, wantMulti) {
		t.Errorf("findMatches multiline with capturing matches: %v", got)
	}
	if m, ok := q.FindNext(snap, at(1, 1), true); !ok || !reflect.DeepEqual(m, wantMulti[0]) {
		t.Errorf("findNextMatch multiline with capturing matches: %v", m)
	}
	if m, ok := q.FindPrevious(snap, at(1, 1), true); !ok || !reflect.DeepEqual(m, wantMulti[1]) {
		t.Errorf("findPreviousMatch multiline with capturing matches: %v", m)
	}
}

func TestFindEmptyMatchesAreStepOver(t *testing.T) {
	// issue #74715: \d* finds an empty string and goes on searching
	snap := snapshotOf("10.243.30.10")
	q := mustQuery(t, "\\d*", true, false)
	got := q.FindMatches(snap, buffer.Range{End: endOf(snap)}, true, 100)
	want := []Match{
		{rng(1, 1, 1, 3), []string{"10"}}, {rng(1, 3, 1, 3), []string{""}}, {rng(1, 4, 1, 7), []string{"243"}},
		{rng(1, 7, 1, 7), []string{""}}, {rng(1, 8, 1, 10), []string{"30"}}, {rng(1, 10, 1, 10), []string{""}},
		{rng(1, 11, 1, 13), []string{"10"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestCarriageReturnIsNeverFound(t *testing.T) {
	// \r can never be found: the text holds line feeds only. (VS Code's test has a CRLF text.)
	snap := snapshotOf("a\nb\nc\nd\ne\nf\ng\nh\ni")
	q := mustQuery(t, "\\r\\n", true, false)
	if m, ok := q.FindNext(snap, at(1, 1), true); ok {
		t.Errorf("found %v", m)
	}
	if got := q.FindMatches(snap, buffer.Range{End: endOf(snap)}, true, 1000); len(got) != 0 {
		t.Errorf("found %v", got)
	}
	// \n matches the end of a line
	q = mustQuery(t, "g\\nh\\n", true, false)
	m, ok := q.FindNext(snap, at(1, 1), true)
	if !ok || m.Range != rng(7, 1, 9, 1) || !reflect.DeepEqual(m.Groups, []string{"g\nh\n"}) {
		t.Errorf("g\\nh\\n = %v %v", m, ok)
	}
	q = mustQuery(t, "\\ni", true, false)
	if m, ok := q.FindMatches(snap, buffer.Range{End: endOf(snap)}, true, 1000), true; !ok || len(m) != 1 || m[0].Range != rng(8, 2, 9, 2) {
		t.Errorf("\\ni = %v", m)
	}
}

func TestParseSearchRequestVSCode(t *testing.T) {
	// the facts parseSearchRequest's tests check, on Query: invalid searches, the simple search,
	// the multiline flag and the word separators
	cases := []struct {
		name      string
		p         Params
		invalid   bool
		simple    string
		multiline bool
		words     bool
	}{
		{"empty", Params{Search: "", IsRegex: true, MatchCase: true, WordSeparators: usualWordSeparators}, true, "", false, false},
		{"invalid regex", Params{Search: "(", IsRegex: true}, true, "", false, false},
		{"foo", Params{Search: "foo"}, false, "", false, false},
		{"foo, words", Params{Search: "foo", WordSeparators: usualWordSeparators}, false, "", false, true},
		{"foo, case", Params{Search: "foo", MatchCase: true}, false, "foo", false, false},
		{"foo, case and words", Params{Search: "foo", MatchCase: true, WordSeparators: usualWordSeparators}, false, "foo", false, true},
		{"foo\\n text", Params{Search: "foo\\n"}, false, "", false, false},
		{"foo\\\\n text", Params{Search: "foo\\\\n"}, false, "", false, false},
		{"foo\\r text", Params{Search: "foo\\r"}, false, "", false, false},
		{"digits are not cased", Params{Search: "123"}, false, "123", false, false},
		{"regex foo", Params{Search: "foo", IsRegex: true}, false, "", false, false},
		{"regex foo, case", Params{Search: "foo", IsRegex: true, MatchCase: true}, false, "", false, false},
		{"regex foo\\n", Params{Search: "foo\\n", IsRegex: true}, false, "", true, false},
		{"regex foo\\\\n", Params{Search: "foo\\\\n", IsRegex: true}, false, "", false, false},
		{"regex foo\\r", Params{Search: "foo\\r", IsRegex: true}, false, "", true, false},
		{"regex foo\\\\r", Params{Search: "foo\\\\r", IsRegex: true}, false, "", false, false},
		{"text with a line feed", Params{Search: "a\nb"}, false, "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := tc.p.Query()
			if tc.invalid {
				if q != nil {
					t.Fatalf("a query was made")
				}
				return
			}
			if q == nil {
				t.Fatal("no query")
			}
			if q.simple != tc.simple || q.multiline != tc.multiline || (q.words != nil) != tc.words {
				t.Errorf("simple %q multiline %v words %v, want %q %v %v", q.simple, q.multiline, q.words != nil, tc.simple, tc.multiline, tc.words)
			}
		})
	}
}

func TestIsMultilineRegexSourceVSCode(t *testing.T) {
	for _, s := range []string{"foo", "", "foo\\sbar", "\\\\notnewline", "MARK:\\s*(?<label>.*)$", "^// Header$", "\\s*[-=]+\\s*"} {
		if IsMultilineRegexSource(s) {
			t.Errorf("%q is not multiline", s)
		}
	}
	for _, s := range []string{
		"foo\\nbar", "foo\\nbar\\s", "foo\\r\\n", "\\n", "foo\\W", "foo\n", "foo\r\n",
		"^// =+\\n^// (?<label>[^\\n]+?)\\n^// =+$", "header\\r\\nfooter", "start\\r|\\nend", "top\nmiddle\r\nbottom",
	} {
		if !IsMultilineRegexSource(s) {
			t.Errorf("%q is multiline", s)
		}
	}
}
