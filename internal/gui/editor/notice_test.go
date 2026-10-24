package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func pumpNotice(t *testing.T, ts *gluttertest.Tester, colors kit.Colors, n *Notice) {
	t.Helper()
	ts.PumpWidget(kit.NewTheme(colors, widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return n
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}), kit.ThemeOpts{UIFont: testutil.MonoFamily}))
	ts.Pump(time.Millisecond)
}

const binaryMessage = "The file is not displayed in the editor because it is either binary or uses an unsupported text encoding."

func TestNoticeButton(t *testing.T) {
	var calls int
	ts := testutil.New(t, ui.Size{Width: 640, Height: 200})
	pumpNotice(t, ts, bundledColors(t, "Dark Modern"), NewNotice(binaryMessage, NoticeOpts{Action: "Open Anyway", OnAction: func() { calls++ }}))
	if !ts.Exists(gluttertest.ByText(binaryMessage)) {
		t.Fatal("the message is not shown")
	}
	ts.Tap(gluttertest.ByText("Open Anyway"))
	ts.Pump(time.Millisecond)
	if calls != 1 {
		t.Errorf("a click called the action %d times", calls)
	}
	// the notice has the focus at first: Enter and Space press the button too
	for _, key := range []services.LogicalKeyboardKey{services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeySpace} {
		ts.SendKeyDownEvent(key, gluttertest.KeyOpts{})
		ts.SendKeyUpEvent(key)
		ts.Pump(time.Millisecond)
	}
	if calls != 3 {
		t.Errorf("after Enter and Space the action was called %d times", calls)
	}
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyEscape, gluttertest.KeyOpts{})
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyEscape)
	if calls != 3 {
		t.Error("another key pressed the button")
	}
}

func TestNoticeWithoutAction(t *testing.T) {
	ts := testutil.New(t, ui.Size{Width: 640, Height: 200})
	pumpNotice(t, ts, bundledColors(t, "Dark Modern"), NewNotice("Nothing to show."))
	if !ts.Exists(gluttertest.ByText("Nothing to show.")) {
		t.Fatal("no message")
	}
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyEnter, gluttertest.KeyOpts{})
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyEnter)
}

func TestGoldenNotice(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{{"notice_dark", "Dark Modern"}, {"notice_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			ts := testutil.New(t, ui.Size{Width: 640, Height: 200})
			pumpNotice(t, ts, bundledColors(t, tc.theme), NewNotice(binaryMessage, NoticeOpts{Action: "Open Anyway", OnAction: func() {}}))
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}
