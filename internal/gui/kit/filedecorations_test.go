package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/decorations"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// decoService is a service with a provider that decorates by URI.
func decoService(t testing.TB, files map[string]decorations.Data) (*decorations.Service, *decorations.Registration) {
	t.Helper()
	svc := decorations.New(decorations.Options{})
	var uris []string
	reg, err := svc.Register("test", decorations.ProviderFunc(func(uri string) (decorations.Data, bool) {
		d, ok := files[uri]
		return d, ok
	}))
	if err != nil {
		t.Fatal(err)
	}
	for uri := range files {
		uris = append(uris, uri)
	}
	reg.Fire(uris...)
	return svc, reg
}

func TestResolveFileLook(t *testing.T) {
	c := DarkModern()
	modified, ok := c.Named("gitDecoration.modifiedResourceForeground")
	if !ok {
		t.Fatal("the theme does not know gitDecoration.modifiedResourceForeground")
	}
	added, _ := c.Named("gitDecoration.addedResourceForeground")
	tests := []struct {
		name      string
		dec       decorations.Decoration
		opts      FileLookOpts
		color     ui.Color
		hasColor  bool
		badge     string
		hasBadgeC bool
		bubble    bool
	}{
		{"colors and badges", decorations.Decoration{Colors: []string{"gitDecoration.modifiedResourceForeground"}, Badge: "M"}, FileLookOpts{Colors: true, Badges: true}, modified, true, "M", true, false},
		{"colors off keeps the badge color", decorations.Decoration{Colors: []string{"gitDecoration.modifiedResourceForeground"}, Badge: "M"}, FileLookOpts{Badges: true}, ui.Color{}, false, "M", true, false},
		{"badges off keeps the name color", decorations.Decoration{Colors: []string{"gitDecoration.modifiedResourceForeground"}, Badge: "M"}, FileLookOpts{Colors: true}, modified, true, "", true, false},
		// VS Code's test 'Falls back to lower-weight decoration color when a theme color is undefined'
		{"a color the theme does not know falls back to the next", decorations.Decoration{Colors: []string{"highPriorityColor", "gitDecoration.addedResourceForeground"}, Badge: "H"}, FileLookOpts{Colors: true, Badges: true}, added, true, "H", true, false},
		{"no color the theme knows", decorations.Decoration{Colors: []string{"nothing.known"}, Badge: "X"}, FileLookOpts{Colors: true, Badges: true}, ui.Color{}, false, "X", false, false},
		{"the dot of a folder", decorations.Decoration{Colors: []string{"gitDecoration.modifiedResourceForeground"}, Bubble: true}, FileLookOpts{Colors: true, Badges: true}, modified, true, "", true, true},
		{"the dot is a badge", decorations.Decoration{Colors: []string{"gitDecoration.modifiedResourceForeground"}, Bubble: true}, FileLookOpts{Colors: true}, modified, true, "", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := c.ResolveFileLook(tc.dec, tc.opts)
			if l.HasColor != tc.hasColor || (tc.hasColor && l.Color != tc.color) {
				t.Errorf("color = %v, %v; want %v, %v", l.Color, l.HasColor, tc.color, tc.hasColor)
			}
			if l.Badge != tc.badge || l.HasBadgeColor != tc.hasBadgeC || l.Bubble != tc.bubble {
				t.Errorf("badge %q (color %v), bubble %v; want %q (%v), %v", l.Badge, l.HasBadgeColor, l.Bubble, tc.badge, tc.hasBadgeC, tc.bubble)
			}
		})
	}
	// the tooltip and the strike-through are drawn whatever the options say
	l := c.ResolveFileLook(decorations.Decoration{Tooltip: "Deleted", Strikethrough: true}, FileLookOpts{})
	if l.Tooltip != "Deleted" || !l.Strikethrough {
		t.Errorf("look = %+v", l)
	}
}

func TestSelectedRowOfAFocusedListKeepsTheSelectionColors(t *testing.T) {
	l := DarkModern().ResolveFileLook(decorations.Decoration{Colors: []string{"gitDecoration.modifiedResourceForeground"}}, FileLookOpts{Colors: true})
	for _, tc := range []struct {
		name string
		st   RowState
		want bool
	}{
		{"plain row", RowState{}, true},
		{"selected row of a list without the focus", RowState{Selected: true}, true},
		{"selected row of a list with the focus", RowState{Selected: true, Active: true}, false},
		{"focused but not selected", RowState{Focused: true, Active: true}, true},
	} {
		if got := l.ForRow(tc.st).HasColor; got != tc.want {
			t.Errorf("%s: HasColor = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// labelRig shows a decorated label and a badge for a URI, rebuilt when the service says so.
func labelRig(t *testing.T, svc *decorations.Service, uri string, opts FileLookOpts) *gluttertest.Tester {
	t.Helper()
	child := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		look, ok := FileLookOf(bc, uri, false, opts)
		if !ok {
			return widgets.NewText("plain", widgets.TextOpts{Style: TextStyleOf(bc, 13, ThemeOf(bc).Foreground)})
		}
		return widgets.NewRow([]widgets.Widget{
			NewDecoratedLabel(bc, "decorated", RowState{Foreground: ThemeOf(bc).Foreground}, look),
			look.BadgeWidget(bc, 13, ThemeOf(bc).Foreground),
		}, widgets.RowOpts{MainAxisSize: 1})
	})
	ts := pumpControl(t, NewFileDecorationsHost(svc, child))
	ts.Pump(time.Millisecond)
	return ts
}

func TestAViewIsBuiltAgainWhenTheServiceAnnouncesAChange(t *testing.T) {
	uri := decorations.FileURI("/w/main.go")
	files := map[string]decorations.Data{}
	svc, reg := decoService(t, files)
	ts := labelRig(t, svc, uri, FileLookOpts{Colors: true, Badges: true})
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("plain")); err != nil {
		t.Fatal(err)
	}
	files[uri] = decorations.Data{Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified"}
	reg.Fire(uri)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	for _, text := range []string{"decorated", "M"} {
		if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText(text)); err != nil {
			t.Errorf("after the change: %v", err)
		}
	}
	delete(files, uri)
	reg.Fire(uri)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("plain")); err != nil {
		t.Errorf("after the decoration went: %v", err)
	}
}

func TestAViewWithoutTheServiceIsPlain(t *testing.T) {
	ts := pumpControl(t, widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		if _, ok := FileLookOf(bc, "file:///w/a.go", false, FileLookOpts{Colors: true, Badges: true}); ok {
			return widgets.NewText("decorated")
		}
		return widgets.NewText("plain")
	}))
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("plain")); err != nil {
		t.Error(err)
	}
}

// TestGoldenTabsDecorated looks at tabs with the decorations of Git: the name in the color and the letter after it, a deleted file
// struck through, the colors off, the badges off, a compact pinned tab (no badge), and the single title.
func TestGoldenTabsDecorated(t *testing.T) {
	files := map[string]decorations.Data{
		decorations.FileURI("/w/main.go"):   {Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified", Weight: 12},
		decorations.FileURI("/w/util.go"):   {Letter: "U", Color: "gitDecoration.untrackedResourceForeground", Tooltip: "Untracked", Weight: 11},
		decorations.FileURI("/w/merge.go"):  {Letter: "!", Color: "gitDecoration.conflictingResourceForeground", Tooltip: "Conflict: Both Modified", Weight: 14},
		decorations.FileURI("/w/ignored.o"): {Color: "gitDecoration.ignoredResourceForeground", Weight: 13},
		decorations.FileURI("/w/old.go"):    {Letter: "D", Color: "gitDecoration.deletedResourceForeground", Tooltip: "Deleted", Weight: 11, Strikethrough: true},
	}
	items := []TabItem{
		{Key: "1", URI: decorations.FileURI("/w/main.go"), Name: "main.go", Title: "/w/main.go"},
		{Key: "2", URI: decorations.FileURI("/w/util.go"), Name: "util.go", Title: "/w/util.go", Dirty: true},
		{Key: "3", URI: decorations.FileURI("/w/merge.go"), Name: "merge.go", Title: "/w/merge.go", Preview: true},
		{Key: "4", URI: decorations.FileURI("/w/ignored.o"), Name: "ignored.o", Title: "/w/ignored.o"},
		{Key: "5", URI: decorations.FileURI("/w/old.go"), Name: "old.go", Title: "/w/old.go"},
		{Key: "6", URI: decorations.FileURI("/w/clean.go"), Name: "clean.go", Title: "/w/clean.go"},
		{Key: "7", URI: decorations.FileURI("/w/main.go"), Name: "main.go", Description: "src", Title: "/w/src/main.go", Sticky: true},
	}
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			svc, _ := decoService(t, files)
			content := padded(themed(func(bc widgets.BuildContext) widgets.Widget {
				return NewFileDecorations(svc, column(
					labelled(bc, "colors and badges", strip(640, items, TabsOpts{Active: 0, DecorationColors: true, DecorationBadges: true})), vgap(8),
					labelled(bc, "colors only", strip(640, items, TabsOpts{Active: 0, DecorationColors: true})), vgap(8),
					labelled(bc, "badges only", strip(640, items, TabsOpts{Active: 0, DecorationBadges: true})), vgap(8),
					labelled(bc, "pinned tabs compact (no badge)", strip(640, items[5:], TabsOpts{Active: 0, DecorationColors: true, DecorationBadges: true, PinnedSizing: PinnedCompact})), vgap(8),
					labelled(bc, "single title", strip(640, items, TabsOpts{Active: 1, Mode: TabsSingle, DecorationColors: true, DecorationBadges: true})),
				))
			}))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 800, Height: 280}, content)
			ts.Pump(time.Millisecond)
			testutil.MatchesGolden(ts, "tabs_decorated_"+th.name)
		})
	}
}

func TestTheTitleOfATabGetsTheTooltipOfItsDecoration(t *testing.T) {
	look := FileLook{Tooltip: "Modified"}
	for _, tc := range []struct {
		title     string
		decorated bool
		want      string
	}{
		{"/w/main.go", true, "/w/main.go • Modified"},
		{"", true, "Modified"},
		{"/w/main.go", false, "/w/main.go"},
	} {
		if got := tabTitle(tc.title, look, tc.decorated); got != tc.want {
			t.Errorf("tabTitle(%q, %v) = %q, want %q", tc.title, tc.decorated, got, tc.want)
		}
	}
	if got := tabTitle("/w/a", FileLook{}, true); got != "/w/a" {
		t.Errorf("a look with no tooltip: %q", got)
	}
}

func TestListOverlayRowsAreDecoratedWhenAsked(t *testing.T) {
	uri := decorations.FileURI("/w/src/main.go")
	svc, _ := decoService(t, map[string]decorations.Data{
		uri:                              {Letter: "M", Color: "gitDecoration.modifiedResourceForeground"},
		decorations.FileURI("/w/folder"): {Letter: "A", Color: "gitDecoration.addedResourceForeground", Bubble: true},
	})
	items := []ListItem{{Label: "main.go", Icon: fonts.CodiconSymbolFile, URI: uri}, {Label: "other.go", Icon: fonts.CodiconSymbolFile, URI: decorations.FileURI("/w/src/other.go")}, {Label: "plain"}}
	for _, tc := range []struct {
		name  string
		opts  foundation.Opt[FileLookOpts]
		wantM int
	}{
		{"with decorations", foundation.Some(FileLookOpts{Colors: true, Badges: true}), 1},
		{"without badges", foundation.Some(FileLookOpts{Colors: true}), 0},
		{"not asked for", foundation.None[FileLookOpts](), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, NewFileDecorations(svc, NewListOverlay(items, ListOverlayOpts{Key: foundation.NewValueKey(tc.name), FileDecorations: tc.opts, OnSelect: func(int) {}})))
			ts.Pump(time.Millisecond)
			if got := len(ts.Elements(gluttertest.ByText("M"))); got != tc.wantM {
				t.Errorf("%d badges, want %d", got, tc.wantM)
			}
			if got := len(ts.Elements(gluttertest.ByText("main.go"))); got != 1 {
				t.Errorf("the row is on screen %d times", got)
			}
		})
	}
}
