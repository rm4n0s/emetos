package editor

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// bundledColors are the colors of a bundled theme, as the theme service gives them to the UI.
func bundledColors(t testing.TB, name string) kit.Colors {
	t.Helper()
	th, err := theme.LoadBundled(name)
	if err != nil {
		t.Fatal(err)
	}
	return kit.ColorsOf(theme.NewColorTheme(th, theme.DefaultColorRegistry()))
}

// themeSample is the text the theme goldens show: a few lines of Go, with the current line and a
// selection so that the colors of both are on the picture.
func themeSample(t *testing.T) *ViewModel {
	t.Helper()
	text := strings.Join(strings.Split(goldenSample(t), "\n")[:9], "\n") + "\n"
	vm := NewViewModel(newTestDoc(text), goOptions(), nil)
	if err := vm.RunCommand("cursorDown", nil); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := vm.RunCommand("cursorWordRightSelect", nil); err != nil {
			t.Fatal(err)
		}
	}
	return vm
}

func TestGoldenBundledThemes(t *testing.T) {
	for _, name := range []string{"Dark Modern", "Light Modern", "Dark+", "Light+", "Dark High Contrast", "Light High Contrast"} {
		slug := strings.NewReplacer(" ", "_", "+", "_plus").Replace(strings.ToLower(name))
		t.Run(name, func(t *testing.T) {
			vm := themeSample(t)
			ts := testutil.New(t, ui.Size{Width: 520, Height: 220})
			pumpEditorColors(t, ts, vm, EditorOpts{Autofocus: true}, bundledColors(t, name))
			noErrors(t, ts)
			testutil.MatchesGolden(ts, "theme_"+slug)
		})
	}
}

// TestProofThemes writes the proof image of step 4.8: the same text in Dark Modern, Light Modern
// and Dark High Contrast side by side, each editor under its own kit.Theme.
func TestProofThemes(t *testing.T) {
	if !testutil.ProofEnabled() {
		t.Skip("the run has no -proof flag")
	}
	names := []string{"Dark Modern", "Light Modern", "Dark High Contrast"}
	var panes []widgets.Widget
	for _, name := range names {
		panes = append(panes, widgets.NewExpanded(kit.NewTheme(bundledColors(t, name), NewEditor(themeSample(t), EditorOpts{}))))
	}
	ts := testutil.New(t, ui.Size{Width: 1260, Height: 300})
	ts.PumpWidget(widgets.NewWidgetsApp(ui.NewColor(0xFF000000), widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return widgets.NewRow(panes)
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	noErrors(t, ts)
	testutil.Proof(t, ts, "4.8-themes.png")
}
