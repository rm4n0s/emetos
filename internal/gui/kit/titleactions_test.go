package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// A row of icon buttons takes the width of the buttons that fit the room, whole, and shows those and no more.
func TestIconButtonRow(t *testing.T) {
	tests := []struct {
		name    string
		buttons int
		room    float64
		want    int
	}{
		{"all fit", 3, 200, 3},
		{"exactly", 3, 3 * IconButtonSize, 3},
		{"a pixel short of the last", 3, 3*IconButtonSize - 1, 2},
		{"one", 3, IconButtonSize, 1},
		{"less than one", 3, IconButtonSize - 1, 0},
		{"no room", 3, 0, 0},
		{"a negative room", 3, -5, 0},
		{"no button", 0, 100, 0},
		{"an unbounded room", 3, 1e18, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buttons := make([]*IconButton, tc.buttons)
			for i := range buttons {
				buttons[i] = NewIconButton(fonts.CodiconClose, IconButtonOpts{Tooltip: "button", OnPressed: func() {}})
			}
			row := NewIconButtonRow(buttons)
			width := row.TitleActionsWidth(tc.room)
			if want := float64(tc.want) * IconButtonSize; width != want {
				t.Fatalf("TitleActionsWidth(%v) = %v, want %v", tc.room, width, want)
			}
			if again := row.TitleActionsWidth(width); again != width {
				t.Errorf("asked again with the width it returned: %v, want %v", again, width)
			}
			// built in a width tight to that, it shows those buttons
			ts := pumpThemed(t, DarkModern(), ui.Size{Width: 400, Height: 40},
				widgets.NewAlign(widgets.NewSizedBox(row, widgets.SizedBoxOpts{Width: foundation.Some(width)}), widgets.AlignOpts{}))
			if got := ts.Count(gluttertest.ByType[*IconButton]()); got != tc.want {
				t.Errorf("%d buttons on screen, want %d", got, tc.want)
			}
		})
	}
}
