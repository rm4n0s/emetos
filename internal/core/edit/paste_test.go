package edit

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func TestCopy(t *testing.T) {
	cases := []struct {
		name   string
		lines  []string
		at     []sel4
		empty  bool // editor.emptySelectionClipboard
		want   CopyData
		pieces bool
	}{
		{"a selection", []string{"hello world"}, []sel4{{1, 1, 1, 6}}, true, CopyData{Text: "hello"}, false},
		{"a selection across lines", []string{"ab", "cd", "ef"}, []sel4{{1, 2, 3, 2}}, true, CopyData{Text: "b\ncd\ne"}, false},
		{"a reversed selection", []string{"hello"}, []sel4{{1, 4, 1, 2}}, false, CopyData{Text: "el"}, false},
		{"an empty selection copies the line", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}}, true, CopyData{Text: "ab\n", FromEmptySelection: true}, false},
		{"the last line too", []string{"ab", "cd"}, []sel4{{2, 1, 2, 1}}, true, CopyData{Text: "cd\n", FromEmptySelection: true}, false},
		{"an empty selection copies nothing without the setting", []string{"ab"}, []sel4{{1, 2, 1, 2}}, false, CopyData{}, false},
		{"several selections", []string{"abc", "def"}, []sel4{{2, 1, 2, 3}, {1, 1, 1, 2}}, true, CopyData{Text: "a\nde", Pieces: []string{"a", "de"}}, true},
		{"empty cursors on the same line copy it once", []string{"abc", "def"}, []sel4{{1, 1, 1, 1}, {1, 3, 1, 3}}, true, CopyData{Text: "abc\n", Pieces: nil}, false},
		{"empty cursors on two lines", []string{"abc", "def"}, []sel4{{1, 1, 1, 1}, {2, 2, 2, 2}}, true, CopyData{Text: "abc\n\ndef\n", Pieces: []string{"abc\n", "def\n"}}, true},
		{"a selection and a cursor", []string{"abc", "def"}, []sel4{{1, 1, 1, 3}, {2, 2, 2, 2}}, true, CopyData{Text: "ab\ndef\n", Pieces: []string{"ab", "def\n"}}, true},
		{"empty cursors are left out without the setting", []string{"abc", "def"}, []sel4{{1, 1, 1, 3}, {2, 2, 2, 2}}, false, CopyData{Text: "ab"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tc.lines...).at(tc.at...)
			got := e.commands().Copy(e.sels, tc.empty)
			if got.Text != tc.want.Text || got.FromEmptySelection != tc.want.FromEmptySelection || !slices.Equal(got.Pieces, tc.want.Pieces) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCut(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		at    []sel4
		empty bool
		text  string
		sels  []sel4
	}{
		{"a selection", []string{"hello world"}, []sel4{{1, 1, 1, 7}}, true, "world", []sel4{{1, 1, 1, 1}}},
		{"an empty selection cuts the line", []string{"ab", "cd", "ef"}, []sel4{{2, 2, 2, 2}}, true, "ab\nef", []sel4{{2, 1, 2, 1}}},
		{"the last line takes the break before it", []string{"ab", "cd"}, []sel4{{2, 2, 2, 2}}, true, "ab", []sel4{{1, 3, 1, 3}}},
		{"the only line is emptied", []string{"ab"}, []sel4{{1, 2, 1, 2}}, true, "", []sel4{{1, 1, 1, 1}}},
		{"nothing without the setting", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}}, false, "ab\ncd", []sel4{{1, 2, 1, 2}}},
		{"two cursors on two lines", []string{"a", "b", "c", "d"}, []sel4{{1, 1, 1, 1}, {3, 1, 3, 1}}, true, "b\nd", []sel4{{1, 1, 1, 1}, {2, 1, 2, 1}}},
		// The break before the last line is part of the cut of the line above it, so the last line is
		// emptied instead of taking the break too.
		{"the last two lines", []string{"a", "b", "c"}, []sel4{{2, 1, 2, 1}, {3, 1, 3, 1}}, true, "a\n", []sel4{{2, 1, 2, 1}}},
		{"a selection and a cursor", []string{"abc", "def", "ghi"}, []sel4{{1, 1, 1, 3}, {2, 2, 2, 2}}, true, "c\nghi", []sel4{{1, 1, 1, 1}, {2, 1, 2, 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tc.lines...).at(tc.at...)
			e.run(func(c Commands, s []buffer.Selection) Result { return c.Cut(s, tc.empty) })
			e.assertText(tc.text)
			e.assertSels(tc.sels...)
		})
	}
}

func TestPaste(t *testing.T) {
	spread := PasteOptions{Spread: true}
	cases := []struct {
		name  string
		lines []string
		at    []sel4
		text  string
		opts  PasteOptions
		want  string
		sels  []sel4
	}{
		{"into a caret", []string{"abc"}, []sel4{{1, 2, 1, 2}}, "XY", spread, "aXYbc", []sel4{{1, 4, 1, 4}}},
		{"over a selection", []string{"abcdef"}, []sel4{{1, 2, 1, 5}}, "-", spread, "a-ef", []sel4{{1, 3, 1, 3}}},
		{"with line breaks", []string{"ab"}, []sel4{{1, 2, 1, 2}}, "x\r\ny\rz", spread, "ax\ny\nzb", []sel4{{3, 2, 3, 2}}},
		{"the same text at every cursor", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}, {2, 2, 2, 2}}, "_", spread, "a_b\nc_d", []sel4{{1, 3, 1, 3}, {2, 3, 2, 3}}},
		{"one line per cursor with spread", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}, {2, 2, 2, 2}}, "X\nY", spread, "aXb\ncYd", []sel4{{1, 3, 1, 3}, {2, 3, 2, 3}}},
		{"a trailing line break is not a line", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}, {2, 2, 2, 2}}, "X\nY\n", spread, "aXb\ncYd", []sel4{{1, 3, 1, 3}, {2, 3, 2, 3}}},
		{"in order of the text, not of the cursors", []string{"ab", "cd"}, []sel4{{2, 2, 2, 2}, {1, 2, 1, 2}}, "X\nY", spread, "aXb\ncYd", []sel4{{2, 3, 2, 3}, {1, 3, 1, 3}}},
		{"without spread every cursor gets everything", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}, {2, 2, 2, 2}}, "X\nY", PasteOptions{}, "aX\nYb\ncX\nYd", []sel4{{2, 2, 2, 2}, {4, 2, 4, 2}}},
		{"a number of lines that is not the number of cursors", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}, {2, 2, 2, 2}}, "X\nY\nZ", spread, "aX\nY\nZb\ncX\nY\nZd", []sel4{{3, 2, 3, 2}, {6, 2, 6, 2}}},
		{"the pieces of a copy by as many cursors", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}, {2, 2, 2, 2}}, "P\nQ\nR", PasteOptions{Pieces: []string{"P", "Q\nR"}}, "aPb\ncQ\nRd", []sel4{{1, 3, 1, 3}, {3, 2, 3, 2}}},
		{"pieces of a copy by another number of cursors", []string{"ab"}, []sel4{{1, 2, 1, 2}}, "P\nQ", PasteOptions{Pieces: []string{"P", "Q"}}, "aP\nQb", []sel4{{2, 2, 2, 2}}},
		{"a whole line goes above the cursor", []string{"ab", "cd"}, []sel4{{2, 2, 2, 2}}, "XY\n", PasteOptions{OnNewLine: true, Spread: true}, "ab\nXY\ncd", []sel4{{3, 2, 3, 2}}},
		{"a whole line over a selection is pasted in place", []string{"abc"}, []sel4{{1, 1, 1, 3}}, "XY\n", PasteOptions{OnNewLine: true}, "XY\nc", []sel4{{2, 1, 2, 1}}},
		{"a whole line with two breaks is pasted in place", []string{"ab"}, []sel4{{1, 2, 1, 2}}, "X\nY\n", PasteOptions{OnNewLine: true}, "aX\nY\nb", []sel4{{3, 1, 3, 1}}},
		{"a whole line at two cursors", []string{"ab", "cd"}, []sel4{{1, 1, 1, 1}, {2, 2, 2, 2}}, "Z\n", PasteOptions{OnNewLine: true, Spread: true}, "Z\nab\nZ\ncd", []sel4{{2, 1, 2, 1}, {4, 2, 4, 2}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tc.lines...).at(tc.at...)
			e.run(func(c Commands, s []buffer.Selection) Result { return c.Paste(s, tc.text, tc.opts) })
			e.assertText(tc.want)
			e.assertSels(tc.sels...)
		})
	}
}

func TestPasteBlock(t *testing.T) {
	opts := PasteOptions{Block: true, Spread: true}
	cases := []struct {
		name  string
		lines []string
		at    []sel4
		text  string
		opts  PasteOptions
		want  string
		sels  []sel4
	}{
		{"down the lines at the column", []string{"abcd", "efgh", "ijkl"}, []sel4{{1, 2, 1, 2}}, "XY\nZW", opts, "aXYbcd\neZWfgh\nijkl", []sel4{{2, 4, 2, 4}}},
		{"padded to the widest line", []string{"abcd", "efgh"}, []sel4{{1, 2, 1, 2}}, "X\nYZ", opts, "aX bcd\neYZfgh", []sel4{{2, 4, 2, 4}}},
		{"short lines are padded to the column", []string{"abcd", "e", "fghi"}, []sel4{{1, 4, 1, 4}}, "X\nY\nZ", opts, "abcXd\ne  Y\nfghZi", []sel4{{3, 5, 3, 5}}},
		{"past the end of the text new lines are added", []string{"abcd"}, []sel4{{1, 3, 1, 3}}, "X\nY\nZ", opts, "abXcd\n  Y\n  Z", []sel4{{3, 4, 3, 4}}},
		{"over a selection", []string{"abcd", "efgh"}, []sel4{{1, 2, 1, 4}}, "X\nY", opts, "aXd\neYfgh", []sel4{{2, 3, 2, 3}}},
		{"without spread it is a plain paste", []string{"abcd"}, []sel4{{1, 2, 1, 2}}, "X\nY", PasteOptions{Block: true}, "aX\nYbcd", []sel4{{2, 2, 2, 2}}},
		{"with several cursors it is a plain paste", []string{"ab", "cd"}, []sel4{{1, 1, 1, 1}, {2, 1, 2, 1}}, "X\nY\nZ", opts, "X\nY\nZab\nX\nY\nZcd", []sel4{{3, 2, 3, 2}, {6, 2, 6, 2}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tc.lines...).at(tc.at...)
			e.run(func(c Commands, s []buffer.Selection) Result { return c.Paste(s, tc.text, tc.opts) })
			e.assertText(tc.want)
			e.assertSels(tc.sels...)
		})
	}
}

func TestPasteInOvertypeMode(t *testing.T) {
	opts := DefaultOptions()
	opts.Overtype = true
	e := newEd(t, opts, "abcdef").caret(1, 2)
	e.run(func(c Commands, s []buffer.Selection) Result { return c.Paste(s, "XY", PasteOptions{}) })
	e.assertText("aXYdef")
}

// TestPasteInOvertypeModeWithOvertypeOnPaste: in overtype mode a paste replaces the characters after the cursor only
// with editor.overtypeOnPaste; without it, or in insert mode, it inserts.
func TestPasteInOvertypeModeWithOvertypeOnPaste(t *testing.T) {
	tests := []struct {
		name              string
		overtype, onPaste bool
		line, text, want  string
	}{
		{"overtype and overtypeOnPaste", true, true, "abcdef", "XY", "aXYdef"},
		{"overtype without overtypeOnPaste", true, false, "abcdef", "XY", "aXYbcdef"},
		{"insert mode ignores overtypeOnPaste", false, true, "abcdef", "XY", "aXYbcdef"},
		{"at the end of the line it inserts", true, true, "ab", "XYZ", "abXYZ"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.Overtype, opts.OvertypeOnPaste = tc.overtype, tc.onPaste
			col := 2
			if tc.line == "ab" {
				col = 3
			}
			e := newEd(t, opts, tc.line).caret(1, col)
			e.run(func(c Commands, s []buffer.Selection) Result { return c.Paste(s, tc.text, PasteOptions{}) })
			e.assertText(tc.want)
		})
	}
}
