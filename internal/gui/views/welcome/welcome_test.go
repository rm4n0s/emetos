package welcome

import (
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name   string
		pieces []Piece
		want   []Block
	}{
		{"a paragraph and a button", []Piece{{Contents: "The folder has no repository.\n[Initialize Repository](command:git.init)"}},
			[]Block{{Text: "The folder has no repository."}, {Label: "Initialize Repository", Target: "command:git.init"}}},
		{"blank lines and spaces are dropped", []Piece{{Contents: "\n  one  \n\n  \ntwo\n"}}, []Block{{Text: "one"}, {Text: "two"}}},
		{"a link inside a sentence is not a button", []Piece{{Contents: "To learn more [read this](https://example.com/a)."}},
			[]Block{{Text: "To learn more [read this](https://example.com/a)."}}},
		{"a link alone is a button, a web address too", []Piece{{Contents: "[Learn More](https://example.com/x)"}},
			[]Block{{Label: "Learn More", Target: "https://example.com/x"}}},
		{"pieces come one after the other and keep their enablement", []Piece{{Contents: "a"}, {Contents: "[B](command:b)", Disabled: true}},
			[]Block{{Text: "a"}, {Label: "B", Target: "command:b", Disabled: true}}},
		{"nothing", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(tt.pieces); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCommandOf(t *testing.T) {
	tests := []struct {
		target, id, args string
		ok               bool
	}{
		{"command:git.init", "git.init", "", true},
		{"command:git.clone?%5B%22a%22%2C1%5D", "git.clone", `["a",1]`, true},
		{"command:", "", "", false},
		{"https://example.com", "", "", false},
	}
	for _, tt := range tests {
		id, args, ok := CommandOf(tt.target)
		if id != tt.id || args != tt.args || ok != tt.ok {
			t.Errorf("CommandOf(%q) = %q %q %v", tt.target, id, args, ok)
		}
	}
}

func pump(t testing.TB, colors kit.Colors, size ui.Size, child widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	ts.PumpWidget(widgets.NewWidgetsApp(colors.SideBarBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewColoredBox(colors.SideBarBackground, widgets.NewAlign(
					widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}),
					widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
				return kit.NewTheme(colors, page, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	for range 4 {
		ts.Pump(50 * time.Millisecond)
	}
	return ts
}

// TestGoldenWelcome looks at the content of a repository that is not trusted, the content with two buttons, and a disabled button.
func TestGoldenWelcome(t *testing.T) {
	pieces := []Piece{
		{Contents: "The folder currently open doesn't have a Git repository. You can initialize a repository which will enable source control features powered by Git."},
		{Contents: "[Initialize Repository](command:git.init)"},
		{Contents: "To learn more about how to use Git and source control in Emetos [read our docs](https://example.com/docs)."},
		{Contents: "[Clone Repository](command:git.clone)", Disabled: true},
	}
	for _, th := range []struct {
		name   string
		colors kit.Colors
	}{{"dark", kit.DarkModern()}, {"light", kit.LightModern()}} {
		t.Run(th.name, func(t *testing.T) {
			ts := pump(t, th.colors, ui.Size{Width: 300, Height: 260}, New(Opts{Pieces: pieces}))
			testutil.MatchesGolden(ts, "welcome_"+th.name)
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

// TestButtonsRunLinks checks that a button calls OnLink with its target, and that a disabled one does not.
func TestButtonsRunLinks(t *testing.T) {
	var got []string
	pieces := []Piece{{Contents: "Hello.\n[Go](command:a.go)"}, {Contents: "[Stay](command:a.stay)", Disabled: true}}
	ts := pump(t, kit.DarkModern(), ui.Size{Width: 300, Height: 200}, New(Opts{Pieces: pieces, OnLink: func(target string) { got = append(got, target) }}))
	ts.Tap(gluttertest.ByText("Go"))
	ts.Tap(gluttertest.ByText("Stay"))
	if !reflect.DeepEqual(got, []string{"command:a.go"}) {
		t.Errorf("links = %v", got)
	}
}
