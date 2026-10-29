package editor

import (
	"context"
	"fmt"
	"strings"
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

// crumbScrollRig shows a breadcrumbs bar whose file and width the test changes.
type crumbScrollRig struct {
	ts   *gluttertest.Tester
	file *foundation.ValueNotifier[string]
}

func newCrumbScrollRig(t *testing.T, width float64, file string) *crumbScrollRig {
	t.Helper()
	r := &crumbScrollRig{ts: testutil.New(t, ui.Size{Width: width, Height: 340}), file: foundation.NewValueNotifier(file)}
	read := func(_ context.Context, dir string) ([]BreadcrumbEntry, error) {
		return []BreadcrumbEntry{{Name: "sibling-one", Path: dir + "/sibling-one"}, {Name: "sibling-two", Path: dir + "/sibling-two"}}, nil
	}
	r.ts.PumpWidget(widgets.NewWidgetsApp(kit.DarkModern().EditorBackground, widgets.WidgetsAppOpts{
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				return kit.NewTheme(kit.DarkModern(), widgets.NewValueListenableBuilder(r.file, func(_ widgets.BuildContext, file string, _ widgets.Widget) widgets.Widget {
					return widgets.NewAlign(NewBreadcrumbs(context.Background(), file, onOptions, BreadcrumbsOpts{ReadDir: read}),
						widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
				}, nil), kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	r.settle()
	return r
}

// settle lets the frames that follow a change run: the bar scrolls after the frame.
func (r *crumbScrollRig) settle() {
	for range 6 {
		r.ts.FlushPosted()
		r.ts.Pump(time.Millisecond)
	}
}

func (r *crumbScrollRig) setFile(file string) {
	r.file.SetValue(file)
	r.settle()
}

func (r *crumbScrollRig) setWidth(width float64) {
	r.ts.SetViewSize(ui.Size{Width: width, Height: 340})
	r.settle()
}

// span is the left and right edge of a segment's name, in the window.
func (r *crumbScrollRig) span(name string) (left, right float64) {
	f := gluttertest.ByText(name)
	return r.ts.TopLeft(f).Dx, r.ts.BottomRight(f).Dx
}

// visible checks that the name of a segment is wholly inside the bar.
func (r *crumbScrollRig) visible(t *testing.T, name string) {
	t.Helper()
	left, right := r.span(name)
	bar := r.ts.Size(gluttertest.ByType[*Breadcrumbs]()).Width
	if left < -0.01 || right > bar+0.01 {
		t.Errorf("%q is at %.1f..%.1f, outside the bar of %.0f px", name, left, right, bar)
	}
}

// scrolled is how far the bar is scrolled: where the first folder is in a bar that is wide enough, minus where it is.
func (r *crumbScrollRig) scrolled(t *testing.T) float64 {
	t.Helper()
	left, _ := r.span(firstFolder)
	return firstFolderLeft(t) - left
}

// firstFolder is the name of the first folder of deepPath.
var firstFolder = "dir0_" + strings.Repeat("x", 12)

// firstFolderLeft is where firstFolder is when the bar does not scroll.
func firstFolderLeft(t *testing.T) float64 {
	t.Helper()
	r := newCrumbScrollRig(t, 3000, deepPath(1, "a.go"))
	left, _ := r.span(firstFolder)
	return left
}

func (r *crumbScrollRig) noErrors(t *testing.T) {
	t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// deepPath is a file below folders named dir0, dir1, ...
func deepPath(folders int, file string) string {
	parts := []string{""}
	for i := range folders {
		parts = append(parts, fmt.Sprintf("dir%d_%s", i, strings.Repeat("x", 12)))
	}
	return strings.Join(append(parts, file), "/")
}

func TestBreadcrumbsScrollALongPath(t *testing.T) {
	cases := []struct {
		name     string
		width    float64
		file     string
		last     string
		scrolled bool
	}{
		{"a deep path in a narrow bar", 300, deepPath(10, "main.go"), "main.go", true},
		{"a very narrow bar", 120, deepPath(4, "main.go"), "main.go", true},
		{"a path that fits", 800, deepPath(2, "main.go"), "main.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newCrumbScrollRig(t, tc.width, tc.file)
			r.noErrors(t)
			if h := r.ts.Size(gluttertest.ByType[*Breadcrumbs]()); h.Height != BreadcrumbsHeight || h.Width != tc.width {
				t.Errorf("the bar is %v", h)
			}
			r.visible(t, tc.last)
			got := r.scrolled(t)
			if tc.scrolled && got <= 0 {
				t.Errorf("the bar did not scroll (%.1f)", got)
			}
			if !tc.scrolled && (got < -0.01 || got > 0.01) {
				t.Errorf("the bar scrolled by %.1f although the path fits", got)
			}
		})
	}
	t.Run("a last segment wider than the bar starts at the left edge", func(t *testing.T) {
		file := deepPath(3, strings.Repeat("long_file_name_", 6)+".go")
		r := newCrumbScrollRig(t, 200, file)
		r.noErrors(t)
		left, _ := r.span(strings.Repeat("long_file_name_", 6) + ".go")
		if want := 16 + 2 + breadcrumbsPad; left < want-0.5 || left > want+0.5 {
			t.Errorf("the name starts at %.1f, want %.1f (its icon at the left edge)", left, want)
		}
	})
}

func TestBreadcrumbsScrollFollowsTheFileAndTheWidth(t *testing.T) {
	t.Run("another file reveals its last segment", func(t *testing.T) {
		r := newCrumbScrollRig(t, 300, deepPath(10, "main.go"))
		r.setFile(deepPath(10, "another_file.go"))
		r.noErrors(t)
		r.visible(t, "another_file.go")
		if r.scrolled(t) <= 0 {
			t.Error("the bar is not scrolled")
		}
	})
	t.Run("a short path scrolls the bar back", func(t *testing.T) {
		r := newCrumbScrollRig(t, 300, deepPath(10, "main.go"))
		r.setFile(deepPath(1, "main.go"))
		r.noErrors(t)
		r.visible(t, "main.go")
		if got := r.scrolled(t); got < -0.01 || got > 0.01 {
			t.Errorf("the bar is scrolled by %.1f", got)
		}
	})
	t.Run("a narrower bar keeps the last segment in view", func(t *testing.T) {
		r := newCrumbScrollRig(t, 1200, deepPath(5, "main.go"))
		if got := r.scrolled(t); got < -0.01 || got > 0.01 {
			t.Fatalf("the bar is scrolled by %.1f although it fits", got)
		}
		r.setWidth(250)
		r.noErrors(t)
		r.visible(t, "main.go")
		if r.scrolled(t) <= 0 {
			t.Error("the bar is not scrolled")
		}
		r.setWidth(1200)
		r.noErrors(t)
		r.visible(t, "main.go")
		if got := r.scrolled(t); got < -0.01 || got > 0.01 {
			t.Errorf("the bar is scrolled by %.1f after it was widened", got)
		}
	})
}

func TestBreadcrumbsPickerOfAScrolledBar(t *testing.T) {
	r := newCrumbScrollRig(t, 480, deepPath(10, "main.go"))
	// a folder that is in view with room on its left, to see that the picker follows the scroll
	var name string
	var segLeft float64
	for i := 9; i >= 0; i-- {
		n := fmt.Sprintf("dir%d_%s", i, strings.Repeat("x", 12))
		if l, rt := r.span(n); l >= 20 && l <= 110 && rt <= 480 {
			name, segLeft = n, l
			break
		}
	}
	if name == "" {
		t.Fatal("no folder in view near the left edge")
	}
	r.ts.Tap(gluttertest.ByText(name))
	for range 100 {
		r.ts.FlushPosted()
		r.ts.Pump(time.Millisecond)
		if r.text("sibling-one") {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !r.text("sibling-one") {
		t.Fatal("the picker did not open")
	}
	left := r.ts.TopLeft(gluttertest.ByText("sibling-one"))
	// the picker starts under the segment (its icon is a little left of the name) and below the bar
	if left.Dx < segLeft-30 || left.Dx > segLeft+pickerWidth {
		t.Errorf("the picker rows start at %.1f, the segment at %.1f", left.Dx, segLeft)
	}
	if left.Dy < BreadcrumbsHeight {
		t.Errorf("the picker starts at %.1f, inside the bar", left.Dy)
	}
	r.noErrors(t)
}

func (r *crumbScrollRig) text(s string) bool { return r.ts.Exists(gluttertest.ByText(s)) }
