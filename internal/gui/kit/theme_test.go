package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func TestDarkModernValues(t *testing.T) {
	c := DarkModern()
	cases := []struct {
		name string
		got  ui.Color
		want uint32
	}{
		{"editor.background", c.EditorBackground, 0xFF1F1F1F},
		{"editor.foreground", c.EditorForeground, 0xFFCCCCCC},
		{"editor.lineHighlightBorder", c.EditorLineHighlightBorder, 0xFF282828},
		{"editor.selectionBackground", c.EditorSelectionBackground, 0xFF264F78},
		{"editor.inactiveSelectionBackground", c.EditorInactiveSelectionBackground, 0xFF3A3D41},
		{"editorCursor.foreground", c.EditorCursorForeground, 0xFFAEAFAD},
		{"editorLineNumber.foreground", c.EditorLineNumberForeground, 0xFF6E7681},
		{"editorLineNumber.activeForeground", c.EditorLineNumberActiveForeground, 0xFFCCCCCC},
		{"editorGutter.background", c.EditorGutterBackground, 0xFF1F1F1F},
		{"editorWhitespace.foreground", c.EditorWhitespaceForeground, 0x29E3E4E2},
		{"editorIndentGuide.background1", c.EditorIndentGuideBackground1, 0xFF404040},
		{"editorIndentGuide.background1 by level", c.EditorIndentGuideBackground[0], 0xFF404040},
		{"editorIndentGuide.activeBackground1", c.EditorIndentGuideActiveBackground[0], 0xFF707070},
		{"editorIndentGuide.background2", c.EditorIndentGuideBackground[1], 0x00000000},
		{"editorBracketPairGuide.background1", c.EditorBracketPairGuideBackground[0], 0x00000000},
		{"editorBracketPairGuide.activeBackground3", c.EditorBracketPairGuideActiveBackground[2], 0x00000000},
		{"editorRuler.foreground", c.EditorRulerForeground, 0xFF5A5A5A},
		{"scrollbarSlider.background", c.ScrollbarSliderBackground, 0x66797979},
		{"scrollbarSlider.hoverBackground", c.ScrollbarSliderHoverBackground, 0xB3646464},
		{"scrollbarSlider.activeBackground", c.ScrollbarSliderActiveBackground, 0x66BFBFBF},
		{"scrollbar.shadow", c.ScrollbarShadow, 0xFF000000},
		{"focusBorder", c.FocusBorder, 0xFF0078D4},
		{"foreground", c.Foreground, 0xFFCCCCCC},
		{"widget.shadow", c.WidgetShadow, 0x5C000000},
	}
	for _, tc := range cases {
		if tc.got != ui.NewColor(tc.want) {
			t.Errorf("%s = %v, want %#08x", tc.name, tc.got, tc.want)
		}
	}
	if c.EditorLineHighlightBackground.Alpha() != 0 {
		t.Error("editor.lineHighlightBackground has no color in Dark Modern")
	}
}

// probe is a widget that reads the theme and counts its builds.
type probe struct {
	widgets.StatelessWidget
	got    *Colors
	builds *int
}

func (p *probe) Build(bc widgets.BuildContext) widgets.Widget {
	*p.got = ThemeOf(bc)
	*p.builds++
	return widgets.NewSizedBox(nil)
}

func TestThemeOfFindsTheNearestTheme(t *testing.T) {
	var got Colors
	builds := 0
	custom := DarkModern()
	custom.EditorBackground = ui.NewColor(0xFF112233)
	ts := gluttertest.New(t, gluttertest.Opts{})
	child := &probe{StatelessWidget: widgets.NewStatelessWidgetBase(), got: &got, builds: &builds}
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, NewTheme(DarkModern(), NewTheme(custom, child))))
	if got.EditorBackground != custom.EditorBackground {
		t.Errorf("got %v, want the inner theme %v", got.EditorBackground, custom.EditorBackground)
	}
}

func TestThemeOfWithoutThemeIsDarkModern(t *testing.T) {
	var got Colors
	builds := 0
	ts := gluttertest.New(t, gluttertest.Opts{})
	ts.PumpWidget(&probe{StatelessWidget: widgets.NewStatelessWidgetBase(), got: &got, builds: &builds})
	if got != DarkModern() {
		t.Errorf("got %+v", got)
	}
}

func TestThemeNotifiesOnlyWhenColorsChange(t *testing.T) {
	var got Colors
	builds := 0
	child := &probe{StatelessWidget: widgets.NewStatelessWidgetBase(), got: &got, builds: &builds}
	ts := gluttertest.New(t, gluttertest.Opts{})
	ts.PumpWidget(NewTheme(DarkModern(), child))
	first := builds
	ts.PumpWidget(NewTheme(DarkModern(), child))
	if builds != first {
		t.Errorf("same colors rebuilt the dependent: %d builds", builds)
	}
	changed := DarkModern()
	changed.Foreground = ui.NewColor(0xFFFFFFFF)
	ts.PumpWidget(NewTheme(changed, child))
	if builds != first+1 || got.Foreground != changed.Foreground {
		t.Errorf("changed colors: %d builds, foreground %v", builds, got.Foreground)
	}
}
