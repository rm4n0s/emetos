package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/decorations"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// decorate registers a provider of the window's decorations service that decorates the given paths (relative to the folder) and fires
// them.
func (a *explorerApp) decorate(files map[string]decorations.Data) *decorations.Registration {
	a.t.Helper()
	byURI := map[string]decorations.Data{}
	var uris []string
	for rel, d := range files {
		uri := decorations.FileURI(filepath.Join(a.root, filepath.FromSlash(rel)))
		byURI[uri] = d
		uris = append(uris, uri)
	}
	reg, err := a.win.Decorations.Register("test", decorations.ProviderFunc(func(uri string) (decorations.Data, bool) {
		d, ok := byURI[uri]
		return d, ok
	}))
	if err != nil {
		a.t.Fatal(err)
	}
	reg.Fire(uris...)
	a.pump()
	return reg
}

func (a *explorerApp) badges(letter string) int {
	return len(a.ts.Elements(gluttertest.ByText(letter)))
}

func (a *explorerApp) setSetting(key string, value any) {
	a.t.Helper()
	if err := a.win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

// TestFileDecorationsReachTheExplorerTheOpenEditorsAndTheTabs checks that what a provider of the window's service says shows in the three
// places, follows Fire, and that the three settings (explorer.decorations.badges for the first two, workbench.editor.decorations.badges
// for the tabs) turn the badges off.
func TestFileDecorationsReachTheExplorerTheOpenEditorsAndTheTabs(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 500}, []string{"main.go", "docs/a.md", "src/x.go"}, "main.go")
	if got := a.badges("M"); got != 0 {
		t.Fatalf("%d badges before any decoration", got)
	}
	data := map[string]decorations.Data{"main.go": {Letter: "M", Tooltip: "Modified", Color: "gitDecoration.modifiedResourceForeground", Weight: 12, Bubble: true}}
	reg := a.decorate(data)
	// the tab, the row of the Explorer and the row of Open Editors
	if got := a.badges("M"); got < 2 {
		t.Fatalf("%d badges after the provider fired, want the tab and the Explorer's row at least", got)
	}
	all := a.badges("M")

	a.setSetting("explorer.decorations.badges", false)
	if got := a.badges("M"); got >= all || got < 1 {
		t.Errorf("%d badges with explorer.decorations.badges off, want fewer than %d and the tab's", got, all)
	}
	a.setSetting("workbench.editor.decorations.badges", false)
	if got := a.badges("M"); got != 0 {
		t.Errorf("%d badges with every badge setting off", got)
	}
	a.setSetting("explorer.decorations.badges", true)
	a.setSetting("workbench.editor.decorations.badges", true)
	if got := a.badges("M"); got != all {
		t.Errorf("%d badges after turning them on again, want %d", got, all)
	}

	// the decoration goes when the provider says so
	byURI := decorations.FileURI(filepath.Join(a.root, "main.go"))
	_ = byURI
	reg.Dispose()
	a.pump()
	if got := a.badges("M"); got != 0 {
		t.Errorf("%d badges after the provider was disposed", got)
	}
}
