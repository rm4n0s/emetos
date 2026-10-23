package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func TestPaste(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		at    []sel4
		text  string
		want  string
		sels  []sel4
	}{
		{"into a caret", []string{"abc"}, []sel4{{1, 2, 1, 2}}, "XY", "aXYbc", []sel4{{1, 4, 1, 4}}},
		{"over a selection", []string{"abcdef"}, []sel4{{1, 2, 1, 5}}, "-", "a-ef", []sel4{{1, 3, 1, 3}}},
		{"with line breaks", []string{"ab"}, []sel4{{1, 2, 1, 2}}, "x\r\ny\rz", "ax\ny\nzb", []sel4{{3, 2, 3, 2}}},
		{"at every cursor", []string{"ab", "cd"}, []sel4{{1, 2, 1, 2}, {2, 2, 2, 2}}, "_", "a_b\nc_d", []sel4{{1, 3, 1, 3}, {2, 3, 2, 3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tc.lines...).at(tc.at...)
			e.run(func(c Commands, s []buffer.Selection) Result { return c.Paste(s, tc.text) })
			e.assertText(tc.want)
			if len(tc.sels) > 0 {
				e.assertSels(tc.sels...)
			}
		})
	}
}
