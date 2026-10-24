package explorer

import (
	"github.com/rm4n0s/glutter/services"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

var goldenThemes = []struct {
	name   string
	colors kit.Colors
}{
	{"dark", kit.DarkModern()},
	{"light", kit.LightModern()},
}

// projectEntries are a small project, with files that have different icons.
var projectEntries = []string{
	"main.go", "go.mod", "README.md", "Makefile", ".gitignore", "LICENSE", "package.json",
	"assets/logo.png", "docs/guide.md", "cmd/tool/main.go",
	"internal/server/server.go", "internal/server/server_test.go", "internal/store/store.go",
}

// TestGoldenExplorer looks at the tree: collapsed, with the file of the active editor revealed (the tree has
// not the focus, so the selection is the inactive one), and with the focus on a row.
func TestGoldenExplorer(t *testing.T) {
	area := ui.Size{Width: 260, Height: 340}
	for _, th := range goldenThemes {
		t.Run(th.name+" collapsed", func(t *testing.T) {
			w := newWRig(t, th.colors, area, projectEntries...)
			w.ts.MatchesGolden("explorer_collapsed_" + th.name)
			w.noErrors()
		})
		t.Run(th.name+" revealed", func(t *testing.T) {
			w := newWRig(t, th.colors, area, projectEntries...)
			var done bool
			w.view.Reveal(w.path("internal/server/server.go"), RevealSelect, func(bool) { done = true })
			w.wait("the reveal", func() bool { return done })
			w.settle()
			w.ts.MatchesGolden("explorer_revealed_" + th.name)
			w.noErrors()
		})
		t.Run(th.name+" focused", func(t *testing.T) {
			w := newWRig(t, th.colors, area, projectEntries...)
			var done bool
			w.view.Reveal(w.path("internal/server/server.go"), RevealSelect, func(bool) { done = true })
			w.wait("the reveal", func() bool { return done })
			w.settle()
			w.tapRow(0) // opens assets and gives the tree the focus
			w.wait("assets", w.loaded)
			w.settle()
			w.key(keyDown)
			w.key(keyDown)
			w.ts.MatchesGolden("explorer_focused_" + th.name)
			w.noErrors()
		})
	}
}

func TestGoldenWelcome(t *testing.T) {
	for _, th := range goldenThemes {
		for _, editors := range []bool{false, true} {
			name := "welcome_" + th.name
			if editors {
				name = "welcome_editors_" + th.name
			}
			t.Run(name, func(t *testing.T) {
				ts := pump(t, th.colors, ui.Size{Width: 260, Height: 160}, NewWelcome(WelcomeOpts{HasEditors: editors, OnOpenFolder: func() {}}))
				ts.MatchesGolden("explorer_" + name)
			})
		}
	}
}

func TestGoldenOpenEditors(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newEditorsRig(t)
			g1 := r.grid.Active()
			ins := r.open(g1, "/p/main.go", "/p/README.md", "/p/docs/guide.md")
			ins[1].dirty = true
			ins[1].Emit(editorgroup.InputDirty)
			g2, err := r.grid.AddGroup(g1, editorgroup.DirRight)
			if err != nil {
				t.Fatal(err)
			}
			r.open(g2, "/p/internal/server/server.go", "/p/Makefile")
			preview := &fakeInput{path: "/p/go.mod"}
			t.Cleanup(preview.Dispose)
			g2.Model.OpenEditor(preview, editorgroup.OpenOptions{Active: true})
			if err := r.grid.Activate(g1); err != nil {
				t.Fatal(err)
			}
			ts := pump(t, th.colors, ui.Size{Width: 260, Height: float64(r.view.Count()) * 22}, NewOpenEditorsBody(r.view))
			ts.Pump(1)
			ts.MatchesGolden("explorer_open_editors_" + th.name)
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

var keyDown = services.LogicalKeyboardKeyArrowDown
