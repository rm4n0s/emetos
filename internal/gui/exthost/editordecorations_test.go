package exthost

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/editordecorationstest"
)

// editorDecorationsManifest is the manifest of the extension of the tests: it has the command the suite's links name.
func editorDecorationsManifest(id string) extension.Manifest {
	m := manifest(id)
	m.Contributes.Commands = []extension.CommandSpec{{ID: editordecorationstest.OwnCommand, Title: "Run"}, {ID: "test.registered", Title: "Registered"}}
	return m
}

// editorDecorationsHarness makes the harness of the conformance suite over a tab of the host.
func editorDecorationsHarness(t *testing.T) *editordecorationstest.Harness {
	t.Helper()
	var ran []any
	var api extension.API
	f, _ := factory(editorDecorationsManifest("emetos.annotest"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error {
			api = a
			return a.Commands().Handle(editordecorationstest.OwnCommand, func(_ context.Context, args any) error { ran = append(ran, args); return nil })
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	if api == nil {
		t.Fatal("the extension was not activated")
	}
	return &editordecorationstest.Harness{
		API: api,
		Seen: func(uri string) []extension.LineAnnotation {
			var out []extension.LineAnnotation
			for _, an := range tb.annotations.For(uri) {
				out = append(out, extension.LineAnnotation{Line: an.Line, Text: an.Text, Color: an.Color, Hover: an.Hover})
			}
			return out
		},
		Follow: func(uri string, line int, dest string) {
			for _, an := range tb.annotations.For(uri) {
				if an.Line == line && an.Link != nil {
					an.Link(dest)
					return
				}
			}
		},
		Ran:        func() []any { return ran },
		Pump:       tb.pump,
		Deactivate: func() { tb.disabled = []string{"emetos.annotest"}; tb.scope.Refresh() },
	}
}

// TestEditorDecorationsConformance runs the suite that the fake of the tests of the extensions passes too.
func TestEditorDecorationsConformance(t *testing.T) {
	editordecorationstest.Run(t, editorDecorationsHarness)
}

func TestEditorDecorationsFailWithTheirOwnTags(t *testing.T) {
	t.Run("a scope with no editors", func(t *testing.T) {
		var api extension.API
		f, _ := factory(editorDecorationsManifest("emetos.annotest"), func(f *fake) {
			f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
		})
		r := newRig(t, f)
		r.noAnnotations = map[string]bool{"one": true}
		r.newTab("one").scope.Start()
		mustRoute(t, api.EditorDecorations().Set("k", "file:///a", nil), "editorDecorationsAPI.Set.ExtensionEditorDecorationsUnavailable")
		api.EditorDecorations().Clear("k") // does nothing
	})
	t.Run("an invalid set", func(t *testing.T) {
		_, _, api := grabAPIWith(t, editorDecorationsManifest("emetos.annotest"))
		mustRoute(t, api.EditorDecorations().Set("", "file:///a", nil), "editorDecorationsAPI.Set.ExtensionEditorDecorationsInvalid")
	})
	t.Run("a destination that is not a command link", func(t *testing.T) {
		_, _, err := extension.ParseCommandLink("https://example.com")
		mustRoute(t, err, "ParseCommandLink.ExtensionCommandLinkInvalid")
		_, _, err = extension.ParseCommandLink("command:x?{")
		mustRoute(t, err, "ParseCommandLink.ExtensionCommandLinkInvalid")
	})
}

func TestCommandLinksOfAHoverRunTheExtensionsOwnCommandsOnly(t *testing.T) {
	var ran []string
	var api extension.API
	f, _ := factory(editorDecorationsManifest("emetos.annotest"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error {
			api = a
			if err := a.Commands().Handle(editordecorationstest.OwnCommand, func(context.Context, any) error { ran = append(ran, "declared"); return nil }); err != nil {
				return err
			}
			if err := a.Commands().Handle("test.registered", func(context.Context, any) error { ran = append(ran, "handled"); return nil }); err != nil {
				return err
			}
			return a.Commands().Register(extension.CommandSpec{ID: "emetos.annotest.late", Title: "Late"}, func(context.Context, any) error { ran = append(ran, "registered"); return nil })
		}
	})
	other, _ := factory(editorDecorationsManifest("emetos.other"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error {
			return a.Commands().Register(extension.CommandSpec{ID: "emetos.other.cmd", Title: "Other"}, func(context.Context, any) error { ran = append(ran, "other"); return nil })
		}
	})
	r := newRig(t, f, other)
	tb := r.newTab("one")
	tb.scope.Start()
	if err := api.EditorDecorations().Set("k", "file:///a", []extension.LineAnnotation{{Line: 0, Text: "x", Hover: "h"}}); err != nil {
		t.Fatal(err)
	}
	an := tb.annotations.For("file:///a")[0]
	for _, dest := range []string{"command:emetos.annotest.late", "command:test.registered", "command:emetos.other.cmd", "command:workbench.action.files.save"} {
		an.Link(dest)
	}
	if want := []string{"registered", "handled"}; !slices.Equal(ran, want) {
		t.Errorf("ran %v, want %v", ran, want)
	}
}

func TestTheEndOfTheActivationClearsTheStore(t *testing.T) {
	_, tb, api := grabAPIWith(t, editorDecorationsManifest("emetos.annotest"))
	if err := api.EditorDecorations().Set("k", "file:///a", []extension.LineAnnotation{{Line: 0, Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	var told []string
	stop := tb.annotations.Subscribe(func(uris []string) { told = append(told, uris...) })
	defer stop()
	tb.disabled = []string{"emetos.annotest"}
	tb.scope.Refresh()
	if tb.annotations.Has("emetos.annotest", "k") || !slices.Equal(told, []string{"file:///a"}) {
		t.Errorf("the set is still there or the editors were not told: told %v", told)
	}
}
