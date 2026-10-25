// Package uitest is the test harness for the widgets of an extension's views: it shows a widget in a window with the theme
// of Dark Modern or Light Modern and the test fonts, so a widget test or a golden test of an extension does not depend on
// the machine's fonts. It is for tests only (SPEC §10.15 "As built (step 11.5)").
package uitest

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// Theme is a theme a widget is shown in.
type Theme struct {
	// Name is "dark" or "light".
	Name string
	// Colors are the colors of the theme.
	Colors ui.Colors
}

// Themes are the two themes goldens are made in.
func Themes() []Theme {
	return []Theme{{Name: "dark", Colors: kit.DarkModern()}, {Name: "light", Colors: kit.LightModern()}}
}

// MonoFamily is the family of the test font that stands for the UI font.
const MonoFamily = testutil.MonoFamily

// Pump shows the widget in a window of a size, in the colors and with the text style of a side bar, the way the box of a
// view does, and returns the tester. The tester unmounts the tree when the test ends.
func Pump(t testing.TB, th Theme, size gui.Size, child widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	// the monospace font of the desktop is the test font, so that code does not depend on the machine's fonts
	ts.SetPlatformConfiguration(func(c *gui.PlatformConfiguration) { c.MonospaceFontFamily, c.MonospaceFontSize = MonoFamily, 13 })
	colors := th.Colors
	ts.PumpWidget(widgets.NewWidgetsApp(colors.SideBarBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				body := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
					style := ui.TextStyle(bc, 13, colors.SideBarForeground)
					return widgets.NewDefaultTextStyle(style, widgets.NewColoredBox(colors.SideBarBackground, widgets.NewAlign(
						widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}),
						widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})))
				})
				// the colors of tokens are those of the bundled theme of the same name: code is highlighted as in the window
				tokens, _ := kit.BundledTokens(map[string]string{"dark": "Dark Modern", "light": "Light Modern"}[th.Name])
				return kit.NewTheme(colors, body, kit.ThemeOpts{UIFont: MonoFamily, Tokens: tokens})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts
}

// MatchesGolden compares the picture of the tester with the golden image of the name in the testdata/goldens folder of the
// package under test.
func MatchesGolden(ts *gluttertest.Tester, name string) { testutil.MatchesGolden(ts, name) }
