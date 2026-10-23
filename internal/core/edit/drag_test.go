package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func TestDragAndDrop(t *testing.T) {
	cases := []struct {
		name   string
		lines  []string
		sel    sel4
		target [2]int // VS Code line and column
		copy   bool
		text   string
		want   sel4
	}{
		{"one line moved to the right", []string{"hello world foo"}, sel4{1, 1, 1, 7}, [2]int{1, 16}, false, "world foohello ", sel4{1, 10, 1, 16}},
		{"one line moved to the left", []string{"hello world foo"}, sel4{1, 7, 1, 12}, [2]int{1, 1}, false, "worldhello  foo", sel4{1, 1, 1, 6}},
		{"a reversed selection moves too", []string{"hello world foo"}, sel4{1, 12, 1, 7}, [2]int{1, 1}, false, "worldhello  foo", sel4{1, 1, 1, 6}},
		{"one line copied", []string{"hello world foo"}, sel4{1, 1, 1, 6}, [2]int{1, 12}, true, "hello worldhello foo", sel4{1, 12, 1, 17}},
		{"copied to the start of the line", []string{"hello world"}, sel4{1, 7, 1, 12}, [2]int{1, 1}, true, "worldhello world", sel4{1, 1, 1, 6}},
		{"lines moved down", []string{"aa", "bb", "cc", "dd"}, sel4{1, 2, 2, 2}, [2]int{4, 2}, false, "ab\ncc\nda\nbd", sel4{3, 2, 4, 2}},
		{"lines moved up", []string{"aa", "bb", "cc", "dd"}, sel4{3, 1, 3, 3}, [2]int{1, 2}, false, "acca\nbb\n\ndd", sel4{1, 2, 1, 4}},
		{"two lines moved up", []string{"aa", "bb", "cc", "dd"}, sel4{3, 2, 4, 2}, [2]int{1, 2}, false, "ac\nda\nbb\ncd", sel4{1, 2, 2, 2}},
		{"lines copied", []string{"aa", "bb", "cc"}, sel4{1, 2, 2, 2}, [2]int{3, 2}, true, "aa\nbb\nca\nbc", sel4{3, 2, 4, 2}},
		{"copy beside the selection, after it", []string{"abc"}, sel4{1, 1, 1, 3}, [2]int{1, 3}, true, "ababc", sel4{1, 3, 1, 5}},
		{"copy beside the selection, before it", []string{"abc"}, sel4{1, 2, 1, 4}, [2]int{1, 2}, true, "abcbc", sel4{1, 2, 1, 4}},
		{"to the same line, after a multi-line selection", []string{"aa", "bbxyz"}, sel4{1, 2, 2, 2}, [2]int{2, 4}, false, "abxa\nbyz", sel4{1, 4, 2, 2}},
		{"dropped inside the selection", []string{"abcdef"}, sel4{1, 2, 1, 5}, [2]int{1, 3}, false, "abcdef", sel4{1, 2, 1, 5}},
		{"dropped inside the selection with copy", []string{"abcdef"}, sel4{1, 2, 1, 5}, [2]int{1, 3}, true, "abcdef", sel4{1, 2, 1, 5}},
		{"moved to its own edge changes nothing", []string{"abcdef"}, sel4{1, 2, 1, 5}, [2]int{1, 5}, false, "abcdef", sel4{1, 2, 1, 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tc.lines...)
			s := e.sel(tc.sel)
			target := e.pos(tc.target[0], tc.target[1])
			e.run(func(c Commands, _ []buffer.Selection) Result { return c.DragAndDrop(s, target, tc.copy) })
			e.assertText(tc.text)
			e.assertSels(tc.want)
		})
	}
}
