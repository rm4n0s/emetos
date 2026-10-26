package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"
)

// The colors of the suggest widget (step 12.5) in the two modern themes, and the colors of the symbol icons.
func TestSuggestWidgetColorsOfTheModernThemes(t *testing.T) {
	for _, tc := range []struct {
		theme  string
		colors Colors
		want   [10]uint32
		icons  map[string]uint32
	}{
		{"Dark Modern", DarkModern(), [10]uint32{0xFF202020, 0xFFCCCCCC, 0x33CCCCCC, 0xFFFFFFFF, 0xFFFFFFFF, 0xFF04395E, 0x00000000, 0xFF2AAAFF, 0xFF2AAAFF, 0x80CCCCCC},
			map[string]uint32{"method": 0xFFB180D7, "class": 0xFFEE9D28, "field": 0xFF75BEFF, "snippet": 0xFFCCCCCC}},
		{"Light Modern", LightModern(), [10]uint32{0xFFF8F8F8, 0xFF3B3B3B, 0x333B3B3B, 0xFF000000, 0xFF000000, 0xFFE8E8E8, 0x00000000, 0xFF0066BF, 0xFF0066BF, 0x803B3B3B},
			map[string]uint32{"method": 0xFF652D90, "class": 0xFFD67E00, "field": 0xFF007ACC, "snippet": 0xFF3B3B3B}},
	} {
		c := tc.colors
		got := [10]ui.Color{
			c.EditorSuggestWidgetBackground, c.EditorSuggestWidgetForeground, c.EditorSuggestWidgetBorder, c.EditorSuggestWidgetSelectedForeground,
			c.EditorSuggestWidgetSelectedIconForeground, c.EditorSuggestWidgetSelectedBackground, c.EditorSuggestWidgetFocusOutline,
			c.EditorSuggestWidgetHighlightForeground, c.EditorSuggestWidgetFocusHighlightForeground, c.EditorSuggestWidgetStatusForeground,
		}
		names := [10]string{"background", "foreground", "border", "selectedForeground", "selectedIconForeground", "selectedBackground", "focusOutline",
			"highlightForeground", "focusHighlightForeground", "status.foreground"}
		for i := range got {
			if got[i] != ui.NewColor(tc.want[i]) {
				t.Errorf("%s editorSuggestWidget.%s = %#08x, want %#08x", tc.theme, names[i], got[i].ToARGB32(), tc.want[i])
			}
		}
		for name, want := range tc.icons {
			if got := c.SymbolIconColor(name); got != ui.NewColor(want) {
				t.Errorf("%s symbolIcon.%sForeground = %#08x, want %#08x", tc.theme, name, got.ToARGB32(), want)
			}
		}
	}
}

func TestSymbolIconColorOfAnUnknownNameIsTheEditorForeground(t *testing.T) {
	c := DarkModern()
	if got := c.SymbolIconColor("nothing"); got != c.EditorForeground {
		t.Errorf("got %#08x", got.ToARGB32())
	}
	if len(SymbolIconNames) != len(c.SymbolIcons) {
		t.Fatal("the names and the colors do not have the same count")
	}
	for i, n := range SymbolIconNames {
		if c.SymbolIcons[i] == (ui.Color{}) {
			t.Errorf("symbolIcon.%sForeground has no color", n)
		}
	}
}
