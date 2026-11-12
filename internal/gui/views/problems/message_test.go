package problems

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The message that stands in place of the tree stays on its one line in a panel narrower than it (the panel on the
// left of the window): it ends in an ellipsis and the row does not overflow, which the tester would report.
func TestTheMessageFitsANarrowPanel(t *testing.T) {
	tests := []struct {
		name  string
		width float64
	}{
		{"wide enough for the message", 600},
		{"narrower than the message", 160},
		{"narrower than the padding", 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := NewView(NewService(), ViewOpts{})
			colors := kit.DarkModern()
			ts := testutil.New(t, ui.Size{Width: tt.width, Height: 100})
			ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) {
				c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12
			})
			ts.PumpWidget(widgets.NewWidgetsApp(colors.PanelBackground, widgets.WidgetsAppOpts{
				DebugShowCheckedModeBanner: foundation.Some(false),
				OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
					return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
						return kit.NewTheme(colors, NewBody(view), kit.ThemeOpts{UIFont: testutil.MonoFamily})
					}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
				},
			}))
			ts.Pump(time.Millisecond)
			if !ts.Exists(gluttertest.ByText(MessageNoProblems)) {
				t.Fatalf("the message %q is not shown", MessageNoProblems)
			}
			if got := ts.Size(gluttertest.ByKey(MessageKey)).Width; got > tt.width {
				t.Errorf("the message is %v wide in a panel of %v", got, tt.width)
			}
		})
	}
}
