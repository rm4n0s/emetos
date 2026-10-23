package edit

import (
	"slices"
	"strings"
	"testing"
)

// Ported from VS Code: lineCommentCommand.test.ts ('_analyzeLines').
func TestAnalyzeLines(t *testing.T) {
	preflight := func(strs ...string) []linePreflight {
		out := make([]linePreflight, len(strs))
		for i, s := range strs {
			out[i] = linePreflight{commentStr: s, commentStrLength: len(s)}
		}
		return out
	}
	lineOf := func(lines ...string) func(int) string { return func(i int) string { return lines[i] } }

	lines := preflight("//", "rem", "!@#", "!@#")
	remove := analyzeLines(CommentToggle, true, lineOf("\t\t", "    ", "    c", "\t\td"), lines, true, false, false)
	if remove {
		t.Error("shouldRemoveComments = true, want false")
	}
	wantIgnore := []bool{true, true, false, false}
	wantOffset := []int{2, 4, 4, 2}
	for i, l := range lines {
		if l.ignore != wantIgnore[i] || l.commentStrOffset != wantOffset[i] {
			t.Errorf("line %d = %+v, want ignore %v, offset %d", i, l, wantIgnore[i], wantOffset[i])
		}
	}
	for i, want := range []string{"//", "rem", "!@#", "!@#"} {
		if lines[i].commentStr != want {
			t.Errorf("line %d commentStr = %q", i, lines[i].commentStr)
		}
	}

	lines = preflight("//", "rem", "!@#", "!@#")
	remove = analyzeLines(CommentToggle, true, lineOf("\t\t", "    rem ", "    !@# c", "\t\t!@#d"), lines, true, false, false)
	if !remove {
		t.Error("shouldRemoveComments = false, want true")
	}
	wantIgnore = []bool{true, false, false, false}
	wantOffset = []int{2, 4, 4, 2}
	wantLength := []int{2, 4, 4, 3}
	for i, l := range lines {
		if l.ignore != wantIgnore[i] || l.commentStrOffset != wantOffset[i] || l.commentStrLength != wantLength[i] {
			t.Errorf("line %d = %+v, want ignore %v, offset %d, length %d", i, l, wantIgnore[i], wantOffset[i], wantLength[i])
		}
	}
}

// Ported from VS Code: lineCommentCommand.test.ts ('_normalizeInsertionPoint').
func TestNormalizeInsertionPoint(t *testing.T) {
	type entry struct {
		text   string
		offset int
	}
	tests := []struct {
		name    string
		entries []entry
		tabSize int
		want    []int
	}{
		{"Bug 16696", []entry{{"  XX", 2}, {"    YY", 4}}, 4, []int{0, 0}},
		{"Test1", []entry{{"\t\t\tXX", 3}, {"    \tYY", 5}, {"        ZZ", 8}, {"\t\tTT", 2}}, 4, []int{2, 5, 8, 2}},
		{"Test2", []entry{{"\t\t\t   XX", 6}, {"    \t\t\t\tYY", 8}, {"        ZZ", 8}, {"\t\t    TT", 6}}, 4, []int{2, 5, 8, 2}},
		{"Test3", []entry{{"\t\t", 2}, {"\t\t\t", 3}, {"\t\t\t\t", 4}, {"\t\t\t", 3}}, 4, []int{2, 2, 2, 2}},
		{"Test4", []entry{{"\t\t", 2}, {"\t\t\t", 3}, {"\t\t\t\t", 4}, {"\t\t\t", 3}, {"    ", 4}}, 2, []int{2, 2, 2, 2, 4}},
		{"Test5", []entry{{"\t\t", 2}, {"\t\t\t", 3}, {"\t\t\t\t", 4}, {"\t\t\t", 3}, {"    ", 4}}, 4, []int{1, 1, 1, 1, 4}},
		{"Test6", []entry{{" \t", 2}, {"  \t", 3}, {"   \t", 4}, {"    ", 4}, {"\t", 1}}, 4, []int{2, 3, 4, 4, 1}},
		{"Test7", []entry{{" \t\t", 3}, {"  \t\t", 4}, {"   \t\t", 5}, {"    \t", 5}, {"\t", 1}}, 4, []int{2, 3, 4, 4, 1}},
		{"Test8:4", []entry{{"\t", 1}, {"    ", 4}}, 4, []int{1, 4}},
		{"Test8:3", []entry{{"\t", 1}, {"   ", 3}}, 4, []int{0, 0}},
		{"Test8:2", []entry{{"\t", 1}, {"  ", 2}}, 4, []int{0, 0}},
		{"Test8:1", []entry{{"\t", 1}, {" ", 1}}, 4, []int{0, 0}},
		{"Test8:0", []entry{{"\t", 1}, {"", 0}}, 4, []int{0, 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			points := make([]insertionPoint, len(tc.entries))
			for i, e := range tc.entries {
				points[i].commentStrOffset = e.offset
			}
			normalizeInsertionPoint(func(i int) string { return tc.entries[i].text }, points, tc.tabSize)
			got := make([]int, len(points))
			for i, p := range points {
				got[i] = p.commentStrOffset
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("offsets = %v, want %v", got, tc.want)
			}
		})
	}
}

// Ported from VS Code: lineCommentCommand.test.ts ('Editor Contrib - Line Comment in mixed modes').
func TestVSCodeLineCommentInMixedModes(t *testing.T) {
	const inner = 1
	mixed := func(t *testing.T, lines ...string) *ed {
		e := newEd(t, DefaultOptions(), lines...)
		e.opts.Comments = commentLanguages{
			0:     commentRule{line: "//", bs: "/*", be: "*/"}.language(),
			inner: commentRule{bs: "{/*", be: "*/}"}.language(),
		}
		// A line that starts with two spaces is in the inner language.
		e.opts.Tokens = commentSource{of: func(line int) int {
			if strings.HasPrefix(e.line(line+1), "  ") {
				return inner
			}
			return 0
		}}
		return e
	}
	react := []string{"import React from 'react';", "const Loader = () => (", "  <div>", "    Loading...", "  </div>", ");", "export default Loader;"}
	t.Run("issue #24047 (part 1): commenting code in JSX files", func(t *testing.T) {
		e := mixed(t, react...)
		e.at(sel4{1, 1, 7, 22}).run(Commands.ToggleLineComment)
		e.assertLines("// import React from 'react';", "// const Loader = () => (", "//   <div>", "//     Loading...", "//   </div>", "// );", "// export default Loader;")
		e.assertSels(sel4{1, 4, 7, 25})
	})
	t.Run("issue #24047 (part 2): commenting code in JSX files", func(t *testing.T) {
		e := mixed(t, react...)
		e.at(sel4{3, 4, 3, 4}).run(Commands.ToggleLineComment)
		e.assertLines("import React from 'react';", "const Loader = () => (", "  {/* <div> */}", "    Loading...", "  </div>", ");", "export default Loader;")
		e.assertSels(sel4{3, 8, 3, 8})
	})
	t.Run("issue #36173: commenting code in a JSX tag body", func(t *testing.T) {
		e := mixed(t, "<div>", "  {123}", "</div>")
		e.at(sel4{2, 4, 2, 4}).run(Commands.ToggleLineComment)
		e.assertLines("<div>", "  {/* {123} */}", "</div>")
		e.assertSels(sel4{2, 8, 2, 8})
	})
}

func TestLineCommentsOfSeveralSelections(t *testing.T) {
	rule := commentRule{line: "//"}
	t.Run("a line that two selections share is commented once", func(t *testing.T) {
		e := commentEd(t, rule, true, true, "a", "b", "c")
		e.at(sel4{1, 1, 2, 1}, sel4{2, 1, 3, 1}, sel4{3, 1, 3, 1})
		e.run(Commands.ToggleLineComment)
		e.assertLines("// a", "// b", "// c")
	})
	t.Run("commenting twice undoes the comment of each", func(t *testing.T) {
		e := commentEd(t, rule, true, true, "a", "b", "c")
		e.at(sel4{1, 1, 1, 1}, sel4{3, 1, 3, 1})
		e.run(Commands.ToggleLineComment)
		e.assertLines("// a", "b", "// c")
		e.run(Commands.ToggleLineComment)
		e.assertLines("a", "b", "c")
		e.assertSels(sel4{1, 1, 1, 1}, sel4{3, 1, 3, 1})
	})
	t.Run("two cursors on one line", func(t *testing.T) {
		e := commentEd(t, rule, true, true, "abc")
		e.at(sel4{1, 1, 1, 1}, sel4{1, 3, 1, 3})
		e.run(Commands.ToggleLineComment)
		e.assertLines("// abc")
	})
	t.Run("remove and add", func(t *testing.T) {
		e := commentEd(t, rule, true, true, "// a", "b")
		e.at(sel4{1, 1, 2, 2}).run(Commands.RemoveLineComment)
		e.assertLines("a", "b")
		e.run(Commands.AddLineComment)
		e.assertLines("// a", "// b")
	})
	t.Run("a language with nothing to comment with", func(t *testing.T) {
		e := commentEd(t, commentRule{}, true, true, "a")
		e.at(sel4{1, 1, 1, 1}).run(Commands.ToggleLineComment)
		e.assertLines("a")
		e.run(Commands.ToggleBlockComment)
		e.assertLines("a")
	})
	t.Run("no comments registry", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a")
		e.run(Commands.ToggleLineComment)
		e.assertLines("a")
	})
}

func TestBlockCommentWithTokensInside(t *testing.T) {
	// A block comment that has an end token inside it is not one to remove: a new one is added.
	e := commentEd(t, commentRule{bs: "/*", be: "*/"}, true, true, "/* a */ b */")
	e.at(sel4{1, 1, 1, 13}).run(Commands.ToggleBlockComment)
	e.assertLines("/* /* a */ b */ */")
}
