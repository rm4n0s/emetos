package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// featureWidgetRig is the rig of the language features with its editor on screen, in a theme.
func featureWidgetRig(t *testing.T, size ui.Size, colors kit.Colors) (*featureRig, *gluttertest.Tester) {
	t.Helper()
	r := newFeatureRig(t, func(o *Options) { o.Minimap.Enabled = false })
	ts := testutil.New(t, size)
	pumpEditorColors(t, ts, r.vm, EditorOpts{Autofocus: true}, colors)
	return r, ts
}

// until pumps the window until the condition holds.
func until(t *testing.T, ts *gluttertest.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		ts.FlushPosted()
		ts.Pump(time.Millisecond)
	}
	if !cond() {
		t.Fatalf("%s did not happen", what)
	}
	for range 3 {
		ts.Pump(time.Millisecond)
	}
}

func TestGoldenCodeActionList(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"code_actions_dark", "Dark Modern"},
		{"code_actions_light", "Light Modern"},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			r, ts := featureWidgetRig(t, ui.Size{Width: 700, Height: 380}, bundledColors(t, tc.theme))
			r.p.with(func(f *fakeFeatures) {
				f.actions = []langfeatures.CodeAction{
					{Title: "Remove unused call", Kind: langfeatures.KindQuickFix, Preferred: true, Edit: greetRename("Hello")},
					{Title: "Add a doc comment", Kind: langfeatures.KindQuickFix, Edit: greetRename("Hello")},
					{Title: "Extract to function", Kind: langfeatures.KindRefactorExtract, Edit: greetRename("Hello")},
					{Title: "Inline call", Kind: langfeatures.KindRefactorInline, Edit: greetRename("Hello")},
					{Title: "Organize Imports", Kind: langfeatures.KindSourceOrganize, Edit: greetRename("Hello")},
					{Title: "Rewrite (not here)", Kind: langfeatures.KindRefactorRewrite, Disabled: "No rewrite at the cursor"},
				}
			})
			r.greetDiagnostic()
			until(t, ts, "the light bulb", func() bool { return r.vm.LightBulb() != nil })
			if err := r.vm.RunCommand("editor.action.quickFix", nil); err != nil {
				t.Fatal(err)
			}
			until(t, ts, "the list", r.vm.CodeActionMenuVisible)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestGoldenLightBulb(t *testing.T) {
	r, ts := featureWidgetRig(t, ui.Size{Width: 500, Height: 200}, bundledColors(t, "Dark Modern"))
	r.p.with(func(f *fakeFeatures) {
		f.actions = []langfeatures.CodeAction{{Title: "Fix", Kind: langfeatures.KindQuickFix, Preferred: true, Edit: greetRename("Hello")}}
	})
	r.greetDiagnostic()
	until(t, ts, "the light bulb", func() bool { return r.vm.LightBulb() != nil })
	noErrors(t, ts)
	testutil.MatchesGolden(ts, "light_bulb_dark")
}

func TestGoldenRenameBox(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"rename_box_dark", "Dark Modern"},
		{"rename_box_light", "Light Modern"},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			r, ts := featureWidgetRig(t, ui.Size{Width: 600, Height: 240}, bundledColors(t, tc.theme))
			r.p.with(func(f *fakeFeatures) {
				f.prepare = &langfeatures.PrepareRename{Range: rg(5, 1, 5, 6), Placeholder: "Greet"}
			})
			if !r.vm.RenameStart() {
				t.Fatal("no request")
			}
			until(t, ts, "the box", r.vm.RenameVisible)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestGoldenRefactorPreview(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"refactor_preview_dark", "Dark Modern"},
		{"refactor_preview_light", "Light Modern"},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			r, ts := featureWidgetRig(t, ui.Size{Width: 700, Height: 400}, bundledColors(t, tc.theme))
			r.p.with(func(f *fakeFeatures) { f.rename = greetRename("Hello") })
			r.vm.RenameStart()
			until(t, ts, "the box", r.vm.RenameVisible)
			r.vm.RenameSetInput("Hello")
			r.vm.RenameAccept(true)
			until(t, ts, "the preview", func() bool { return r.vm.RefactorPreview() != nil })
			r.vm.PreviewToggle(3)
			ts.Pump(time.Millisecond)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestClosingTheWidgetsReportsNoFrameworkError(t *testing.T) {
	r, ts := featureWidgetRig(t, ui.Size{Width: 700, Height: 380}, bundledColors(t, "Dark Modern"))
	r.p.with(func(f *fakeFeatures) {
		f.actions = []langfeatures.CodeAction{{Title: "Fix", Kind: langfeatures.KindQuickFix, Edit: greetRename("Hello")}}
	})
	r.p.with(func(f *fakeFeatures) { f.rename = greetRename("Hello") })
	r.greetDiagnostic()
	until(t, ts, "the light bulb", func() bool { return r.vm.LightBulb() != nil })
	if err := r.vm.RunCommand("editor.action.quickFix", nil); err != nil {
		t.Fatal(err)
	}
	until(t, ts, "the list", r.vm.CodeActionMenuVisible)
	r.vm.CodeActionHide()
	until(t, ts, "the list to close", func() bool { return !r.vm.CodeActionMenuVisible() })
	noErrors(t, ts)

	r.vm.RenameStart()
	until(t, ts, "the box", r.vm.RenameVisible)
	r.vm.RenameCancel()
	until(t, ts, "the box to close", func() bool { return !r.vm.RenameVisible() })
	noErrors(t, ts)

	r.vm.RenameStart()
	until(t, ts, "the box", r.vm.RenameVisible)
	r.vm.RenameSetInput("Hello")
	r.vm.RenameAccept(true)
	until(t, ts, "the preview", func() bool { return r.vm.RefactorPreview() != nil })
	r.vm.PreviewDiscard()
	until(t, ts, "the preview to close", func() bool { return r.vm.RefactorPreview() == nil })
	noErrors(t, ts)
}

func TestLeavingTheWidgetsOpenAtTheEndReportsNoFrameworkError(t *testing.T) {
	for _, what := range []string{"list", "rename", "preview"} {
		t.Run(what, func(t *testing.T) {
			r, ts := featureWidgetRig(t, ui.Size{Width: 700, Height: 380}, bundledColors(t, "Dark Modern"))
			r.p.with(func(f *fakeFeatures) {
				f.actions = []langfeatures.CodeAction{{Title: "Fix", Kind: langfeatures.KindQuickFix, Edit: greetRename("Hello")}}
			})
			r.p.with(func(f *fakeFeatures) { f.rename = greetRename("Hello") })
			switch what {
			case "list":
				r.vm.TriggerCodeActions(langfeatures.CodeActionTrigger{Type: langfeatures.TriggerInvokedAction, Source: langfeatures.SourceQuickFix}, "none")
				until(t, ts, "the list", r.vm.CodeActionMenuVisible)
			default:
				r.vm.RenameStart()
				until(t, ts, "the box", r.vm.RenameVisible)
				if what == "preview" {
					r.vm.RenameSetInput("Hello")
					r.vm.RenameAccept(true)
					until(t, ts, "the preview", func() bool { return r.vm.RefactorPreview() != nil })
				}
			}
		})
	}
}
