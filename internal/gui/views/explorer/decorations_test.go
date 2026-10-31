package explorer

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/decorations"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// gitLook is the decoration of a status of Git, as the Git extension gives it: its letter, tooltip, color, whether it bubbles and its
// weight.
func gitLook(letter, tooltip, color string, bubble bool, weight int) decorations.Data {
	return decorations.Data{Letter: letter, Tooltip: tooltip, Color: "gitDecoration." + color + "ResourceForeground", Bubble: bubble, Weight: weight}
}

// decorate registers a provider that decorates the files of the rig by their paths relative to its folder.
func (w *wrig) decorate(files map[string]decorations.Data) *decorations.Registration {
	w.t.Helper()
	byURI := map[string]decorations.Data{}
	var uris []string
	for rel, d := range files {
		uri := decorations.FileURI(w.path(rel))
		byURI[uri] = d
		uris = append(uris, uri)
	}
	reg, err := w.decos.Register("test", decorations.ProviderFunc(func(uri string) (decorations.Data, bool) {
		d, ok := byURI[uri]
		return d, ok
	}))
	if err != nil {
		w.t.Fatal(err)
	}
	reg.Fire(uris...)
	w.settle()
	return reg
}

// statusFiles are the files of the golden: one in each state Git has a color for.
var statusFiles = []string{
	"main.go", "go.mod", "README.md", "notes.txt", "scratch.tmp", "old.go", "merge.go", "cmd/tool/main.go", "docs/guide.md", "src/lib/util.go", "src/lib/more.go",
}

func goldenDecorations() map[string]decorations.Data {
	return map[string]decorations.Data{
		"main.go":          gitLook("M", "Modified", "modified", true, 12),
		"go.mod":           gitLook("A", "Index Added", "added", true, 11),
		"README.md":        gitLook("U", "Untracked", "untracked", true, 11),
		"scratch.tmp":      {Color: "gitDecoration.ignoredResourceForeground", Weight: 13},
		"old.go":           {Letter: "D", Tooltip: "Index Deleted", Color: "gitDecoration.stageDeletedResourceForeground", Weight: 11, Strikethrough: true},
		"merge.go":         gitLook("!", "Conflict: Both Modified", "conflicting", true, 14),
		"cmd/tool/main.go": gitLook("M", "Modified", "modified", true, 12),
		"src/lib/util.go":  gitLook("R", "Index Renamed", "renamed", true, 11),
	}
}

// TestGoldenExplorerDecorations looks at the tree with the decorations of Git: a modified, an added, an untracked, an ignored, a
// deleted (struck through) and a conflicted file; the folders above them take the color of the heaviest mark and the dot; a folder with
// no marked file is plain.
func TestGoldenExplorerDecorations(t *testing.T) {
	area := ui.Size{Width: 260, Height: 340}
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			w := newWRig(t, th.colors, area, statusFiles...)
			w.decorate(goldenDecorations())
			// open the folders to see the files below the dots
			for _, rel := range []string{"cmd", "src"} {
				w.view.Tree().Expand(w.path(rel))
				w.settle()
			}
			w.wait("the folders", w.loaded)
			w.settle()
			testutil.MatchesGolden(w.ts, "explorer_decorations_"+th.name)
			w.noErrors()
		})
	}
}

func TestDecorationsDrawColorsBadgesAndFolders(t *testing.T) {
	w := newWRig(t, kit.DarkModern(), ui.Size{Width: 260, Height: 340}, statusFiles...)
	w.decorate(goldenDecorations())
	for _, rel := range []string{"cmd", "src"} {
		w.view.Tree().Expand(w.path(rel))
		w.settle()
	}
	for _, letter := range []string{"M", "A", "U", "D", "!"} {
		if got := len(w.ts.Elements(gluttertest.ByText(letter))); got < 1 {
			t.Errorf("no badge %q on screen", letter)
		}
	}
	// the ignored file has a color and no badge, and a folder with a changed file below it has the dot (Codicon circle-small-filled)
	dots := len(w.ts.Elements(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleSmallFilled))))
	if dots < 2 {
		t.Errorf("%d dots, want one for cmd and one for src at least", dots)
	}
	if got := len(w.ts.Elements(gluttertest.ByText("docs"))); got != 1 {
		t.Errorf("docs is on screen %d times", got)
	}
}

func TestExplorerDecorationSettingsTurnColorsAndBadgesOff(t *testing.T) {
	area := ui.Size{Width: 260, Height: 340}
	tests := []struct {
		name           string
		colors, badges bool
		wantBadge      bool
		wantColor      bool
	}{
		{"both", true, true, true, true},
		{"colors only", true, false, false, true},
		{"badges only", false, true, true, false},
		{"neither", false, false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWRig(t, kit.DarkModern(), area, "main.go", "other.go")
			w.view.opts.DecorationColors = func() bool { return tc.colors }
			w.view.opts.DecorationBadges = func() bool { return tc.badges }
			w.decorate(map[string]decorations.Data{"main.go": gitLook("M", "Modified", "modified", false, 12)})
			w.view.Redraw()
			w.settle()
			badges := len(w.ts.Elements(gluttertest.ByText("M")))
			if (badges > 0) != tc.wantBadge {
				t.Errorf("badge on screen = %v, want %v", badges > 0, tc.wantBadge)
			}
			color, ok := w.nameColor("main.go")
			if !ok {
				t.Fatal("main.go is not on screen")
			}
			plain, _ := w.nameColor("other.go")
			if (color != plain) != tc.wantColor {
				t.Errorf("main.go color %v, other.go %v: colored = %v, want %v", color, plain, color != plain, tc.wantColor)
			}
		})
	}
}

// nameColor is the color the text of a row is drawn in.
func (w *wrig) nameColor(name string) (ui.Color, bool) {
	for _, e := range w.ts.Elements(gluttertest.ByText(name)) {
		if rt, ok := e.Widget().(*widgets.Text); ok {
			if style := rt.Style(); style != nil {
				if c, ok := style.Color.Get(); ok {
					return c, true
				}
			}
		}
		if rt, ok := e.Widget().(*widgets.RichText); ok {
			_ = rt
		}
	}
	return ui.Color{}, false
}

func TestExplorerFollowsTheServiceWhenAProviderFires(t *testing.T) {
	w := newWRig(t, kit.DarkModern(), size, "main.go", "sub/a.go")
	data := map[string]decorations.Data{}
	var reg *decorations.Registration
	reg, err := w.decos.Register("test", decorations.ProviderFunc(func(uri string) (decorations.Data, bool) {
		d, ok := data[uri]
		return d, ok
	}))
	if err != nil {
		t.Fatal(err)
	}
	w.settle()
	count := func(s string) int { return len(w.ts.Elements(gluttertest.ByText(s))) }
	if count("M") != 0 {
		t.Fatal("a badge before any decoration")
	}
	data[decorations.FileURI(w.path("main.go"))] = gitLook("M", "Modified", "modified", false, 12)
	reg.Fire(decorations.FileURI(w.path("main.go")))
	w.settle()
	if count("M") != 1 {
		t.Errorf("%d badges after the provider fired, want 1", count("M"))
	}
	delete(data, decorations.FileURI(w.path("main.go")))
	reg.Fire(decorations.FileURI(w.path("main.go")))
	w.settle()
	if count("M") != 0 {
		t.Errorf("%d badges after the decoration went, want 0", count("M"))
	}
}

// A provider that answers slowly does not freeze the tree: after the slow answers the service allows (20) it is not asked any more
// (SPEC §10.30), however many rows come on screen, and the rows are all there.
func TestASlowProviderDoesNotFreezeTheExplorer(t *testing.T) {
	var files []string
	for i := 0; i < 60; i++ {
		files = append(files, "f"+string(rune('a'+i/26))+string(rune('a'+i%26))+".go")
	}
	w := newWRig(t, kit.DarkModern(), ui.Size{Width: 260, Height: 600}, files...)
	calls := 0
	if _, err := w.decos.Register("slow", decorations.ProviderFunc(func(uri string) (decorations.Data, bool) {
		calls++
		time.Sleep(2 * time.Millisecond)
		return decorations.Data{Letter: "S"}, true
	})); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	w.view.Redraw()
	w.settle()
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the rows took %v", took)
	}
	if calls == 0 || calls > 20 {
		t.Errorf("the slow provider was asked %d times, want between 1 and 20", calls)
	}
	if got := len(w.ts.Elements(gluttertest.ByText("faa.go"))); got != 1 {
		t.Errorf("the first file is on screen %d times, want 1", got)
	}
}

// TestGoldenOpenEditorsDecorated looks at Open Editors with the decorations of Git: the colors, the letters, and a file struck through;
// explorer.decorations.colors and .badges are the settings of this view too.
func TestGoldenOpenEditorsDecorated(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newEditorsRig(t)
			g1 := r.grid.Active()
			ins := r.open(g1, "/p/main.go", "/p/README.md", "/p/old.go", "/p/merge.go", "/p/clean.go")
			ins[1].dirty = true
			svc := decorations.New(decorations.Options{})
			files := map[string]decorations.Data{
				decorations.FileURI("/p/main.go"):   gitLook("M", "Modified", "modified", false, 12),
				decorations.FileURI("/p/README.md"): gitLook("U", "Untracked", "untracked", false, 11),
				decorations.FileURI("/p/old.go"):    {Letter: "D", Tooltip: "Deleted", Color: "gitDecoration.deletedResourceForeground", Weight: 11, Strikethrough: true},
				decorations.FileURI("/p/merge.go"):  gitLook("!", "Conflict: Both Modified", "conflicting", false, 14),
			}
			if _, err := svc.Register("test", decorations.ProviderFunc(func(uri string) (decorations.Data, bool) { d, ok := files[uri]; return d, ok })); err != nil {
				t.Fatal(err)
			}
			ts := pump(t, th.colors, ui.Size{Width: 260, Height: float64(r.view.Count()) * 22}, kit.NewFileDecorationsHost(svc, NewOpenEditorsBody(r.view)))
			ts.Pump(1)
			testutil.MatchesGolden(ts, "explorer_open_editors_decorated_"+th.name)
			for _, letter := range []string{"M", "U", "!"} {
				if got := len(ts.Elements(gluttertest.ByText(letter))); got != 1 {
					t.Errorf("badge %q is on screen %d times, want 1", letter, got)
				}
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
