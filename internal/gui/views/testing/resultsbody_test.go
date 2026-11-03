package testing

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// bodyRig shows the body of Test Results for a run whose output is out.
func bodyRig(t *testing.T, out string) *gluttertest.Tester {
	t.Helper()
	m := newModel(t)
	r := m.results(t)
	res := m.svc.StartExternalRun(coretesting.ExternalRun{Request: coretesting.RunRequest{Group: coretesting.GroupRun}, Persist: true})
	run, err := res.AddTask(coretesting.TaskSpec{Name: "t", ControllerID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	run.AppendOutput([]byte(out), nil, "")
	run.End()
	r.ShowLatest()
	colors := kit.DarkModern()
	ts := testutil.New(t, ui.Size{Width: 900, Height: 400})
	ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) {
		c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12
		c.MonospaceFontFamily, c.MonospaceFontSize = testutil.MonoFamily, 13
	})
	ts.PumpWidget(widgets.NewWidgetsApp(colors.PanelBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				return kit.NewTheme(colors, widgets.NewContainer(NewResultsBody(r), widgets.ContainerOpts{Color: foundation.Some(colors.PanelBackground)}),
					kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts
}

func TestResultsOutputOfHundredThousandLinesStaysInTheFrameBudget(t *testing.T) {
	var b strings.Builder
	for i := range 100_000 {
		b.WriteString("line " + strconv.Itoa(i) + " of the output, \x1b[32mwith color\x1b[0m\n")
	}
	ts := bodyRig(t, b.String())
	start := time.Now()
	const frames = 20
	for range frames {
		ts.Pump(16 * time.Millisecond)
	}
	if per := time.Since(start) / frames; per > 16*time.Millisecond {
		t.Errorf("a frame with 100000 lines takes %v", per)
	}
	rich := gluttertest.FindOpts{FindRichText: true}
	if !ts.Exists(gluttertest.ByText("line 0 of the output, with color", rich)) || ts.Exists(gluttertest.ByText("line 99999 of the output, with color", rich)) {
		t.Error("the output does not start at its first line")
	}
}

func TestAnsiLinesCarryTheStyleFromLineToLine(t *testing.T) {
	lines := kit.AnsiLines(kit.ParseANSI("a\n\x1b[31mb\nc\x1b[0m\n\nd"))
	var texts []string
	for _, l := range lines {
		var s string
		for _, sp := range l {
			s += sp.Text
		}
		texts = append(texts, s)
	}
	if got := strings.Join(texts, "|"); got != "a\n|b\n|c\n|\n|d" {
		t.Fatalf("lines %q", got)
	}
	if lines[2][0].Style.Fg.Kind != kit.AnsiColorTheme {
		t.Errorf("the second line of the red text has the style %+v", lines[2][0].Style)
	}
}
