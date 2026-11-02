package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"
)

func grayOf(v int) ui.Color { return ui.NewColorFromARGB(255, v, v, v) }

// Ported from VS Code: src/vs/base/test/common/color.test.ts (the suite 'ensureContrastRatio', from xterm.js): the cases with a gray
// text on a black and on a white background.
func TestEnsureContrast(t *testing.T) {
	cases := []struct {
		name     string
		bg       int
		ratio    float64
		wantGray int // -1: the color is kept as it is
	}{
		{"black, met", 0x00, 1, -1}, {"black, met 2", 0x00, 2, -1}, {"black, met 3", 0x00, 3, -1},
		{"black 4", 0x00, 4, 0x70}, {"black 5", 0x00, 5, 0x7f}, {"black 6", 0x00, 6, 0x8c}, {"black 7", 0x00, 7, 0x98}, {"black 8", 0x00, 8, 0xa3},
		{"black 9", 0x00, 9, 0xad}, {"black 10", 0x00, 10, 0xb6}, {"black 12", 0x00, 12, 0xc5}, {"black 14", 0x00, 14, 0xd6},
		{"black 16", 0x00, 16, 0xe3}, {"black 18", 0x00, 18, 0xee}, {"black 20", 0x00, 20, 0xfa}, {"black 21", 0x00, 21, 0xff},
		{"white, met 4", 0xff, 4, -1}, {"white, met 6", 0xff, 6, -1},
		{"white 7", 0xff, 7, 0x56}, {"white 8", 0xff, 8, 0x4d}, {"white 9", 0xff, 9, 0x45}, {"white 10", 0xff, 10, 0x3e}, {"white 12", 0xff, 12, 0x31},
		{"white 14", 0xff, 14, 0x27}, {"white 16", 0xff, 16, 0x1f}, {"white 18", 0xff, 18, 0x15}, {"white 20", 0xff, 20, 0x08}, {"white 21", 0xff, 21, 0x00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fg := grayOf(0x60)
			want := fg
			if tc.wantGray >= 0 {
				want = grayOf(tc.wantGray)
			}
			if got := EnsureContrast(grayOf(tc.bg), fg, tc.ratio); got != want {
				t.Errorf("EnsureContrast = #%02x%02x%02x, want #%02x%02x%02x", got.Red(), got.Green(), got.Blue(), want.Red(), want.Green(), want.Blue())
			}
		})
	}
}
