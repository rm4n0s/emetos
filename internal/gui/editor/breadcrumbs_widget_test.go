package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// crumbsFS is the folders of a fake file system: a folder's entries by its path.
var crumbsFS = map[string][]BreadcrumbEntry{
	"/home/me/proj": {
		{Name: "lib", Path: "/home/me/proj/lib", Dir: true}, {Name: "src", Path: "/home/me/proj/src", Dir: true},
		{Name: "README.md", Path: "/home/me/proj/README.md"}, {Name: "go.mod", Path: "/home/me/proj/go.mod"},
	},
	"/home/me/proj/src": {
		{Name: "util", Path: "/home/me/proj/src/util", Dir: true},
		{Name: "main.go", Path: "/home/me/proj/src/main.go"}, {Name: "main_test.go", Path: "/home/me/proj/src/main_test.go"},
	},
	"/home/me/proj/lib": {{Name: "lib.go", Path: "/home/me/proj/lib/lib.go"}},
	"/home/me":          {{Name: "proj", Path: "/home/me/proj", Dir: true}, {Name: "notes.txt", Path: "/home/me/notes.txt"}},
}

func fakeReadDir(_ context.Context, dir string) ([]BreadcrumbEntry, error) {
	if entries, ok := crumbsFS[dir]; ok {
		return entries, nil
	}
	return nil, errors.New("FakeReadDirFailed", "no such folder", "dir", dir)
}

// crumbsRig shows a breadcrumbs bar at the top of a window and records the files it opens.
type crumbsRig struct {
	ts     *gluttertest.Tester
	opened []string
	file   string
	mk     func(file string) widgets.Widget
}

func newCrumbsRig(t *testing.T, file string, options BreadcrumbsOptions, opts BreadcrumbsOpts) *crumbsRig {
	t.Helper()
	r := &crumbsRig{ts: testutil.New(t, ui.Size{Width: 480, Height: 340}), file: file}
	opts.OnOpen = func(p string) { r.opened = append(r.opened, p) }
	r.mk = func(file string) widgets.Widget { return NewBreadcrumbs(context.Background(), file, options, opts) }
	r.ts.PumpWidget(widgets.NewWidgetsApp(kit.DarkModern().EditorBackground, widgets.WidgetsAppOpts{
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				return kit.NewTheme(kit.DarkModern(), widgets.NewAlign(r.mk(r.file), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}),
					kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	r.ts.Pump(time.Millisecond)
	return r
}

// settle lets the reads of folders finish and the frames follow.
func (r *crumbsRig) settle() {
	for range 20 {
		time.Sleep(2 * time.Millisecond)
		r.ts.FlushPosted()
		r.ts.Pump(time.Millisecond)
	}
}

func (r *crumbsRig) waitFor(t *testing.T, text string) {
	t.Helper()
	for range 500 {
		r.ts.FlushPosted()
		r.ts.Pump(time.Millisecond)
		if r.ts.Exists(gluttertest.ByText(text)) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%q never showed", text)
}

func (r *crumbsRig) text(s string) bool { return r.ts.Exists(gluttertest.ByText(s)) }

func (r *crumbsRig) key(k services.LogicalKeyboardKey, char string) {
	r.ts.SendKeyDownEvent(k, gluttertest.KeyOpts{Character: char})
	r.ts.SendKeyUpEvent(k)
	r.ts.Pump(time.Millisecond)
}

var onOptions = BreadcrumbsOptions{Enabled: true, FilePath: "on", Icons: true}

const crumbsFile = "/home/me/proj/src/main.go"

func crumbsOpts() BreadcrumbsOpts {
	return BreadcrumbsOpts{Root: "/home/me/proj", ReadDir: fakeReadDir}
}

func TestBreadcrumbsShowTheSegments(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		options BreadcrumbsOptions
		root    string
		shown   []string
		hidden  []string
	}{
		{"with a root", crumbsFile, onOptions, "/home/me/proj", []string{"proj", "src", "main.go"}, []string{"home", "me"}},
		{"without a root", crumbsFile, onOptions, "", []string{"home", "me", "proj", "src", "main.go"}, nil},
		{"only the last", crumbsFile, BreadcrumbsOptions{Enabled: true, FilePath: "last"}, "", []string{"main.go"}, []string{"src", "proj"}},
		{"disabled", crumbsFile, BreadcrumbsOptions{FilePath: "on"}, "", nil, []string{"main.go", "src"}},
		{"path off", crumbsFile, BreadcrumbsOptions{Enabled: true, FilePath: "off"}, "", nil, []string{"main.go"}},
		{"a document without a path", "", onOptions, "", nil, []string{"main.go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newCrumbsRig(t, tc.file, tc.options, BreadcrumbsOpts{Root: tc.root, ReadDir: fakeReadDir})
			for _, s := range tc.shown {
				if !r.text(s) {
					t.Errorf("%q is not shown", s)
				}
			}
			for _, s := range tc.hidden {
				if r.text(s) {
					t.Errorf("%q is shown", s)
				}
			}
			if len(tc.shown) == 0 {
				if h := r.ts.Size(gluttertest.ByType[*Breadcrumbs]()).Height; h != 0 {
					t.Errorf("the empty bar is %v high", h)
				}
			} else if h := r.ts.Size(gluttertest.ByType[*Breadcrumbs]()).Height; h != BreadcrumbsHeight {
				t.Errorf("the bar is %v high", h)
			}
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestBreadcrumbsPicker(t *testing.T) {
	t.Run("a segment lists its siblings and a file is opened", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "README.md")
		if !r.text("lib") || !r.text("go.mod") {
			t.Error("the siblings of src are not listed")
		}
		r.ts.Tap(gluttertest.ByText("README.md"))
		r.settle()
		if len(r.opened) != 1 || r.opened[0] != "/home/me/proj/README.md" {
			t.Errorf("opened %v", r.opened)
		}
		if r.text("go.mod") {
			t.Error("the picker is still open")
		}
		if errs := r.ts.TakeErrors(); len(errs) != 0 {
			t.Errorf("errors: %v", errs)
		}
	})
	t.Run("the file segment lists the files next to it", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("main.go"))
		r.waitFor(t, "main_test.go")
		if !r.text("util") {
			t.Error("the folder next to the file is not listed")
		}
		r.key(services.LogicalKeyboardKeyEnter, "") // the file itself has the focus first
		r.settle()
		if len(r.opened) != 1 || r.opened[0] != crumbsFile {
			t.Errorf("opened %v", r.opened)
		}
	})
	t.Run("a folder is entered, and .. comes back", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "lib")
		r.ts.Tap(gluttertest.ByText("lib"))
		r.waitFor(t, "lib.go")
		if !r.text("..") || r.text("README.md") {
			t.Error("the picker did not go into lib")
		}
		r.ts.Tap(gluttertest.ByText(".."))
		r.waitFor(t, "README.md")
		if r.text("..") {
			t.Error(".. is offered at the top again")
		}
		if len(r.opened) != 0 {
			t.Errorf("opened %v", r.opened)
		}
	})
	t.Run("a second click on the segment closes the picker", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "README.md")
		r.ts.Tap(gluttertest.First(gluttertest.ByText("src")))
		r.settle()
		if r.text("README.md") {
			t.Error("still open")
		}
		// and a third opens it again
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "README.md")
	})
	t.Run("a click on another segment switches the picker", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "README.md")
		r.ts.Tap(gluttertest.First(gluttertest.ByText("proj")))
		r.waitFor(t, "notes.txt")
		if r.text("README.md") {
			t.Error("the first picker is still open")
		}
		if errs := r.ts.TakeErrors(); len(errs) != 0 {
			t.Errorf("errors: %v", errs)
		}
	})
	t.Run("a click outside closes it", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "README.md")
		r.ts.TapAt(ui.Offset{Dx: 440, Dy: 320})
		r.settle()
		if r.text("README.md") || len(r.opened) != 0 {
			t.Errorf("open: %v, opened %v", r.text("README.md"), r.opened)
		}
	})
	t.Run("Escape closes it", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "README.md")
		r.key(services.LogicalKeyboardKeyEscape, "")
		r.settle()
		if r.text("README.md") {
			t.Error("still open")
		}
	})
	t.Run("typing filters the picker", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "README.md")
		r.key(services.LogicalKeyboardKeyKeyG, "g")
		r.key(services.LogicalKeyboardKeyKeyO, "o")
		if !r.text("go.mod") || r.text("README.md") || r.text("lib") {
			t.Error("the filter did not apply")
		}
		r.key(services.LogicalKeyboardKeyEnter, "")
		r.settle()
		if len(r.opened) != 1 || r.opened[0] != "/home/me/proj/go.mod" {
			t.Errorf("opened %v", r.opened)
		}
	})
	t.Run("a folder that cannot be read says so", func(t *testing.T) {
		r := newCrumbsRig(t, "/nowhere/else/a.go", onOptions, BreadcrumbsOpts{ReadDir: fakeReadDir})
		r.ts.Tap(gluttertest.ByText("else"))
		r.waitFor(t, "Cannot read nowhere")
		r.ts.Tap(gluttertest.ByText("Cannot read nowhere"))
		r.settle()
		if len(r.opened) != 0 || !r.text("Cannot read nowhere") {
			t.Errorf("the message row was chosen: opened %v", r.opened)
		}
	})
	t.Run("another file closes the picker", func(t *testing.T) {
		r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
		r.ts.Tap(gluttertest.ByText("src"))
		r.waitFor(t, "README.md")
		r.file = "/home/me/proj/lib/lib.go"
		r.ts.PumpWidget(widgets.NewWidgetsApp(kit.DarkModern().EditorBackground, widgets.WidgetsAppOpts{
			OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
				return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return kit.NewTheme(kit.DarkModern(), widgets.NewAlign(r.mk(r.file), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}),
						kit.ThemeOpts{UIFont: testutil.MonoFamily})
				}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
			},
		}))
		r.settle()
		if r.text("README.md") || !r.text("lib.go") {
			t.Error("the picker stayed open or the bar did not change")
		}
		if errs := r.ts.TakeErrors(); len(errs) != 0 {
			t.Errorf("errors: %v", errs)
		}
	})
}

func TestBreadcrumbsReadTheRealFileSystem(t *testing.T) {
	dir := writeTree(t, "src/", "docs/", "go.mod", "src/main.go", "src/util.go")
	file := filepath.Join(dir, "src", "main.go")
	r := newCrumbsRig(t, file, onOptions, BreadcrumbsOpts{Root: dir})
	r.ts.Tap(gluttertest.ByText("src"))
	r.waitFor(t, "go.mod")
	if !r.text("docs") {
		t.Error("docs is not listed")
	}
	r.ts.Tap(gluttertest.ByText("go.mod"))
	r.settle()
	if len(r.opened) != 1 || r.opened[0] != filepath.Join(dir, "go.mod") {
		t.Errorf("opened %v", r.opened)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
}

func TestBreadcrumbsLongPathsDoNotOverflow(t *testing.T) {
	parts := []string{"home"}
	for range 10 {
		parts = append(parts, strings.Repeat("directory", 3))
	}
	file := "/" + strings.Join(parts, "/") + "/a_rather_long_file_name.go"
	r := newCrumbsRig(t, file, onOptions, BreadcrumbsOpts{ReadDir: fakeReadDir})
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
	if h := r.ts.Size(gluttertest.ByType[*Breadcrumbs]()); h.Width > 480 || h.Height != BreadcrumbsHeight {
		t.Errorf("size %v", h)
	}
}

func TestGoldenBreadcrumbs(t *testing.T) {
	r := newCrumbsRig(t, crumbsFile, onOptions, crumbsOpts())
	r.ts.Hover(r.ts.Center(gluttertest.ByText("proj")))
	r.ts.Pump(time.Millisecond)
	r.ts.MatchesGolden("breadcrumbs")
	r.ts.Tap(gluttertest.ByText("src"))
	r.waitFor(t, "README.md")
	r.ts.Pump(time.Millisecond)
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	r.ts.MatchesGolden("breadcrumbs_picker")
}

// TestProofStickyBreadcrumbs makes the proof image of step 5.7 with -proof: a scrolled Go file
// with two sticky lines under the breadcrumbs bar. Without the flag it checks that the two work
// together: the bar takes its height, the editor the rest, and the editor keeps its sticky lines.
func TestProofStickyBreadcrumbs(t *testing.T) {
	vm, tok := highlighted(t, stickySample())
	opts := vm.Options()
	opts.StickyScroll = StickyScrollOptions{Enabled: true, MaxLineCount: 2, ScrollWithEditor: true}
	opts.Folding, opts.ShowFoldingControls, opts.RenderLineHighlight = true, "always", "none"
	opts.LineHeight = 19
	opts.OccurrencesHighlight, opts.SelectionHighlight = "off", false
	vm.SetOptions(opts)
	vm.StartFolding(nil, 0)
	computeFolds(t, vm)
	t.Cleanup(vm.DisposeHighlights)
	colors := kit.DarkModern()
	ts := testutil.New(t, ui.Size{Width: 640, Height: 300})
	ts.PumpWidget(kit.NewTheme(colors, widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return widgets.NewColumn([]widgets.Widget{
						NewBreadcrumbs(context.Background(), "/home/me/proj/src/sample.go", onOptions, BreadcrumbsOpts{Root: "/home/me/proj", ReadDir: fakeReadDir}),
						widgets.NewExpanded(NewEditor(vm, EditorOpts{Autofocus: true})),
					}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}), kit.ThemeOpts{Tokens: darkModernTokens(t), UIFont: testutil.MonoFamily}))
	tok.Settle()
	pump := func() {
		for range 3 {
			ts.Pump(time.Millisecond)
		}
		ts.FlushPosted()
		for range 3 {
			ts.Pump(time.Millisecond)
		}
	}
	pump()
	vm.scroller(14 * 19)
	pump()
	if got := vm.stickyLinesAt(vm.ScrollTop()); len(got.Lines) != 2 {
		t.Errorf("sticky lines %v, want 2", got.Lines)
	}
	if h := ts.Size(gluttertest.ByType[*Breadcrumbs]()).Height; h != BreadcrumbsHeight {
		t.Errorf("the bar is %v high", h)
	}
	noErrors(t, ts)
	testutil.Proof(t, ts, "5.7-sticky-breadcrumbs.png")
}
