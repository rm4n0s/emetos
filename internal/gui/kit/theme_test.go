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
		{"scrollbar.background", c.ScrollbarBackground, 0x00000000},
		{"sash.hoverBorder", c.SashHoverBorder, 0xFF0078D4},
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

// The colors of buttons, toolbar actions and hovers in the two bundled themes: what the themes set
// and, where they set nothing, the defaults of the color registry.
func TestModernThemesColorsOfKitWidgets(t *testing.T) {
	for _, tc := range []struct {
		theme  string
		colors Colors
		want   [11]uint32
	}{
		{"Dark Modern", DarkModern(), [11]uint32{0xFFCCCCCC, 0x00000000, 0xFF2B2B2B, 0x26CCCCCC, 0x80CCCCCC, 0x50636667, 0xFF383A49, 0xFF202020, 0xFFCCCCCC, 0x33CCCCCC, 0xFF0078D4}},
		{"Light Modern", LightModern(), [11]uint32{0xFF3B3B3B, 0xFFE5E5E5, 0xFFCCCCCC, 0x263B3B3B, 0x80616161, 0x50A6A6A6, 0xFFDDDDDD, 0xFFF8F8F8, 0xFF3B3B3B, 0x333B3B3B, 0xFF005FB8}},
	} {
		c := tc.colors
		got := [11]ui.Color{
			c.ButtonSecondaryForeground, c.ButtonSecondaryBackground, c.ButtonSecondaryHoverBackground, c.ButtonSecondaryBorder,
			c.DisabledForeground, c.ToolbarActiveBackground, c.ActionBarToggledBackground,
			c.EditorHoverWidgetBackground, c.EditorHoverWidgetForeground, c.EditorHoverWidgetBorder, c.FocusBorder,
		}
		names := [11]string{"button.secondaryForeground", "button.secondaryBackground", "button.secondaryHoverBackground", "button.secondaryBorder",
			"disabledForeground", "toolbar.activeBackground", "actionBar.toggledBackground",
			"editorHoverWidget.background", "editorHoverWidget.foreground", "editorHoverWidget.border", "focusBorder"}
		for i := range got {
			if got[i] != ui.NewColor(tc.want[i]) {
				t.Errorf("%s %s = %#08x, want %#08x", tc.theme, names[i], got[i].ToARGB32(), tc.want[i])
			}
		}
	}
}

// The colors of menus and of the menu bar in the two bundled themes: what the themes set (Dark
// Modern sets the menu background and the selection; the border and the separator come from the
// theme it includes) and, where they set nothing, the defaults of the color registry.
func TestModernThemesColorsOfMenus(t *testing.T) {
	names := [11]string{"menu.foreground", "menu.background", "menu.border", "menu.selectionForeground", "menu.selectionBackground",
		"menu.selectionBorder", "menu.separatorBackground", "titleBar.activeForeground", "menubar.selectionForeground",
		"menubar.selectionBackground", "menubar.selectionBorder"}
	for _, tc := range []struct {
		theme  string
		colors Colors
		want   [11]uint32
	}{
		{"Dark Modern", DarkModern(), [11]uint32{0xFFCCCCCC, 0xFF1F1F1F, 0xFF454545, 0xFFFFFFFF, 0xFF0078D4, 0x00000000, 0xFF454545, 0xFFCCCCCC, 0xFFCCCCCC, 0x505A5D5E, 0x00000000}},
		{"Light Modern", LightModern(), [11]uint32{0xFF3B3B3B, 0xFFFFFFFF, 0xFFCECECE, 0xFFFFFFFF, 0xFF005FB8, 0x00000000, 0x333B3B3B, 0xFF1E1E1E, 0xFF1E1E1E, 0x50B8B8B8, 0x00000000}},
	} {
		c := tc.colors
		got := [11]ui.Color{
			c.MenuForeground, c.MenuBackground, c.MenuBorder, c.MenuSelectionForeground, c.MenuSelectionBackground, c.MenuSelectionBorder,
			c.MenuSeparatorBackground, c.TitleBarActiveForeground, c.MenubarSelectionForeground, c.MenubarSelectionBackground, c.MenubarSelectionBorder,
		}
		for i := range got {
			if got[i] != ui.NewColor(tc.want[i]) {
				t.Errorf("%s %s = %#08x, want %#08x", tc.theme, names[i], got[i].ToARGB32(), tc.want[i])
			}
		}
	}
}

func TestTreeColorsOfTheBundledThemes(t *testing.T) {
	dark, light := DarkModern(), LightModern()
	cases := []struct {
		name string
		got  ui.Color
		want uint32
	}{
		{"dark list.inactiveSelectionBackground", dark.ListInactiveSelectionBackground, 0xFF37373D},
		{"dark list.activeSelectionBackground", dark.ListActiveSelectionBackground, 0xFF04395E},
		{"dark tree.indentGuidesStroke", dark.TreeIndentGuidesStroke, 0xFF585858},
		{"dark tree.inactiveIndentGuidesStroke", dark.TreeInactiveIndentGuidesStroke, 0x66585858},
		{"dark list.focusBackground (unset)", dark.ListFocusBackground, 0},
		{"dark list.inactiveFocusOutline (unset)", dark.ListInactiveFocusOutline, 0},
		{"light list.inactiveSelectionBackground", light.ListInactiveSelectionBackground, 0xFFE4E6F1},
		{"light list.activeSelectionBackground", light.ListActiveSelectionBackground, 0xFFE8E8E8},
		{"light list.focusAndSelectionOutline", light.ListFocusAndSelectionOutline, 0xFF005FB8},
		{"light tree.indentGuidesStroke", light.TreeIndentGuidesStroke, 0xFFA9A9A9},
		{"light tree.inactiveIndentGuidesStroke", light.TreeInactiveIndentGuidesStroke, 0x66A9A9A9},
	}
	for _, tc := range cases {
		if tc.got != ui.NewColor(tc.want) {
			t.Errorf("%s = %v, want %#08x", tc.name, tc.got, tc.want)
		}
	}
}

func TestListInteractionColorsOfTheBundledThemes(t *testing.T) {
	dark, light := DarkModern(), LightModern()
	cases := []struct {
		name string
		got  ui.Color
		want uint32
	}{
		{"dark list.dropBackground (the theme sets it)", dark.ListDropBackground, 0xFF383B3D},
		{"dark list.dropBetweenBackground (icon.foreground of the theme)", dark.ListDropBetweenBackground, 0xFFCCCCCC},
		{"dark list.highlightForeground", dark.ListHighlightForeground, 0xFF2AAAFF},
		{"dark list.filterMatchBackground", dark.ListFilterMatchBackground, 0x55EA5C00},
		{"dark listFilterWidget.noMatchesOutline", dark.ListFilterWidgetNoMatchesOutline, 0xFFBE1100},
		{"light list.dropBetweenBackground", light.ListDropBetweenBackground, 0xFF3B3B3B},
		{"light list.highlightForeground", light.ListHighlightForeground, 0xFF0066BF},
		{"light list.focusHighlightForeground", light.ListFocusHighlightForeground, 0xFF0066BF},
	}
	for _, tc := range cases {
		if tc.got != ui.NewColor(tc.want) {
			t.Errorf("%s = %v, want %#08x", tc.name, tc.got, tc.want)
		}
	}
}

func TestPartColorsOfTheModernThemes(t *testing.T) {
	cases := []struct {
		name  string
		got   func(Colors) ui.Color
		dark  uint32
		light uint32
	}{
		{"statusBar.background", func(c Colors) ui.Color { return c.StatusBarBackground }, 0xFF181818, 0xFFF8F8F8},
		{"statusBar.foreground", func(c Colors) ui.Color { return c.StatusBarForeground }, 0xFFCCCCCC, 0xFF3B3B3B},
		{"statusBar.border", func(c Colors) ui.Color { return c.StatusBarBorder }, 0xFF2B2B2B, 0xFFE5E5E5},
		{"statusBar.noFolderBackground", func(c Colors) ui.Color { return c.StatusBarNoFolderBackground }, 0xFF1F1F1F, 0xFFF8F8F8},
		{"statusBar.noFolderForeground", func(c Colors) ui.Color { return c.StatusBarNoFolderForeground }, 0xFFCCCCCC, 0xFF3B3B3B},
		{"statusBar.noFolderBorder", func(c Colors) ui.Color { return c.StatusBarNoFolderBorder }, 0xFF2B2B2B, 0xFFE5E5E5},
		{"activityBar.background", func(c Colors) ui.Color { return c.ActivityBarBackground }, 0xFF181818, 0xFFF8F8F8},
		{"activityBar.border", func(c Colors) ui.Color { return c.ActivityBarBorder }, 0xFF2B2B2B, 0xFFE5E5E5},
		{"activityBar.foreground", func(c Colors) ui.Color { return c.ActivityBarForeground }, 0xFFD7D7D7, 0xFF1F1F1F},
		{"activityBar.inactiveForeground", func(c Colors) ui.Color { return c.ActivityBarInactiveForeground }, 0xFF868686, 0xFF616161},
		{"activityBar.activeBorder", func(c Colors) ui.Color { return c.ActivityBarActiveBorder }, 0xFF0078D4, 0xFF005FB8},
		{"sideBarSectionHeader.background", func(c Colors) ui.Color { return c.SideBarSectionHeaderBackground }, 0xFF181818, 0xFFF8F8F8},
		{"sideBarSectionHeader.foreground", func(c Colors) ui.Color { return c.SideBarSectionHeaderForeground }, 0xFFCCCCCC, 0xFF3B3B3B},
		{"sideBarSectionHeader.border", func(c Colors) ui.Color { return c.SideBarSectionHeaderBorder }, 0xFF2B2B2B, 0xFFE5E5E5},
		{"sideBar.background", func(c Colors) ui.Color { return c.SideBarBackground }, 0xFF181818, 0xFFF8F8F8},
		{"sideBar.foreground", func(c Colors) ui.Color { return c.SideBarForeground }, 0xFFCCCCCC, 0xFF3B3B3B},
		{"sideBar.border", func(c Colors) ui.Color { return c.SideBarBorder }, 0xFF2B2B2B, 0xFFE5E5E5},
		{"sideBarTitle.foreground", func(c Colors) ui.Color { return c.SideBarTitleForeground }, 0xFFCCCCCC, 0xFF3B3B3B},
		{"panel.background", func(c Colors) ui.Color { return c.PanelBackground }, 0xFF181818, 0xFFF8F8F8},
		{"panel.border", func(c Colors) ui.Color { return c.PanelBorder }, 0xFF2B2B2B, 0xFFE5E5E5},
		{"panelTitle.activeForeground", func(c Colors) ui.Color { return c.PanelTitleActiveForeground }, 0xFFCCCCCC, 0xFF3B3B3B},
		{"panelTitle.inactiveForeground", func(c Colors) ui.Color { return c.PanelTitleInactiveForeground }, 0xFF9D9D9D, 0xFF3B3B3B},
		{"panelTitle.activeBorder", func(c Colors) ui.Color { return c.PanelTitleActiveBorder }, 0xFF0078D4, 0xFF005FB8},
		{"titleBar.activeBackground", func(c Colors) ui.Color { return c.TitleBarActiveBackground }, 0xFF181818, 0xFFF8F8F8},
		{"titleBar.border", func(c Colors) ui.Color { return c.TitleBarBorder }, 0xFF2B2B2B, 0xFFE5E5E5},
		{"notifications.foreground", func(c Colors) ui.Color { return c.NotificationsForeground }, 0xFFCCCCCC, 0xFF3B3B3B},
		{"notifications.background", func(c Colors) ui.Color { return c.NotificationsBackground }, 0xFF1F1F1F, 0xFFFFFFFF},
		{"notificationToast.border", func(c Colors) ui.Color { return c.NotificationToastBorder }, 0xFF313131, 0xFFE5E5E5},
		{"notificationCenter.border", func(c Colors) ui.Color { return c.NotificationCenterBorder }, 0xFF313131, 0xFFE5E5E5},
		{"notificationCenterHeader.foreground", func(c Colors) ui.Color { return c.NotificationCenterHeaderForeground }, 0xFFCCCCCC, 0xFF3B3B3B},
		{"notificationCenterHeader.background", func(c Colors) ui.Color { return c.NotificationCenterHeaderBackground }, 0xFF1F1F1F, 0xFFFFFFFF},
		{"notifications.border", func(c Colors) ui.Color { return c.NotificationsBorder }, 0xFF2B2B2B, 0xFFE5E5E5},
		{"notificationsErrorIcon.foreground", func(c Colors) ui.Color { return c.NotificationsErrorIconForeground }, 0xFFF14C4C, 0xFFE51400},
		{"notificationsWarningIcon.foreground", func(c Colors) ui.Color { return c.NotificationsWarningIconForeground }, 0xFFCCA700, 0xFFBF8803},
		{"notificationsInfoIcon.foreground", func(c Colors) ui.Color { return c.NotificationsInfoIconForeground }, 0xFF59A4F9, 0xFF0063D3},
	}
	for _, tc := range cases {
		if got := tc.got(DarkModern()); got != ui.NewColor(tc.dark) {
			t.Errorf("Dark Modern %s = %v, want %#x", tc.name, got, tc.dark)
		}
		if got := tc.got(LightModern()); got != ui.NewColor(tc.light) {
			t.Errorf("Light Modern %s = %v, want %#x", tc.name, got, tc.light)
		}
	}
}
