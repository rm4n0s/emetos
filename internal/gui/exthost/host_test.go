package exthost

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestNewHostKeepsValidExtensionsAndReportsTheRest(t *testing.T) {
	good, _ := factory(manifest("emetos.good"), nil)
	dup, _ := factory(manifest("emetos.good"), nil)
	nilExt := func() extension.Extension { return nil }
	panicFactory := func() extension.Extension { panic("no") }
	panicManifest := func() extension.Extension { return panickyManifest{} }
	tests := []struct {
		name     string
		factory  extension.Factory
		manifest extension.Manifest
		route    string
	}{
		{"an id with capitals", nil, extension.Manifest{ID: "Emetos.Bad", Name: "Bad"}, "validateManifest->CheckManifest.ExtensionIDInvalid"},
		{"a Manifest that panics", panicManifest, extension.Manifest{}, "probe.func1.ExtensionProbePanicked"},
		{"no name", nil, extension.Manifest{ID: "emetos.noname"}, "validateManifest->CheckManifest.ExtensionNameMissing"},
		{"the same id twice", dup, extension.Manifest{}, "validateManifest.ExtensionIDDuplicate"},
		{"an unknown event", nil, extension.Manifest{ID: "emetos.ev", Name: "Ev", ActivationEvents: []string{"onSunday"}}, "validateManifest->CheckManifest.ExtensionEventInvalid"},
		{"an event without its argument", nil, extension.Manifest{ID: "emetos.ev", Name: "Ev", ActivationEvents: []string{"onLanguage:"}}, "validateManifest->CheckManifest.ExtensionEventInvalid"},
		{"onStartup with an argument", nil, extension.Manifest{ID: "emetos.ev", Name: "Ev", ActivationEvents: []string{"onStartup:now"}}, "validateManifest->CheckManifest.ExtensionEventInvalid"},
		{"a nil factory", func() extension.Factory { return nil }(), extension.Manifest{}, "probe.ExtensionFactoryNil"},
		{"a factory that makes nothing", nilExt, extension.Manifest{}, "probe.ExtensionFactoryNil"},
		{"a factory that panics", panicFactory, extension.Manifest{}, "probe.func1.ExtensionProbePanicked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.factory
			if tt.manifest.ID != "" || tt.manifest.Name != "" || len(tt.manifest.ActivationEvents) > 0 {
				f, _ = factory(tt.manifest, nil)
			}
			h := NewHost(HostOptions{Factories: []extension.Factory{good, f}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if got := h.Manifests(); len(got) != 1 || got[0].ID != "emetos.good" {
				t.Fatalf("manifests = %+v, want only emetos.good", got)
			}
			problems := h.Problems()
			if len(problems) != 1 {
				t.Fatalf("problems = %v, want one", problems)
			}
			appErr, ok := errors.FromError(problems[0])
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", problems[0])
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestManifestsAreInActivationOrder(t *testing.T) {
	var fs []extension.Factory
	for _, id := range []string{"emetos.b", "emetos.a", "emetos.c"} {
		f, _ := factory(manifest(id), nil)
		fs = append(fs, f)
	}
	h := NewHost(HostOptions{Factories: fs})
	var ids []string
	for _, m := range h.Manifests() {
		ids = append(ids, m.ID)
	}
	if got := strings.Join(ids, ","); got != "emetos.b,emetos.a,emetos.c" {
		t.Errorf("order = %s", got)
	}
}

func TestMenuConstantsAreTheIDsOfTheMenus(t *testing.T) {
	// the public package cannot import the core, so it repeats the ids; this keeps them equal
	r := newRig(t)
	tb := r.newTab("one")
	for _, id := range []string{
		extension.MenuCommandPalette, extension.MenuFile, extension.MenuEdit, extension.MenuSelection, extension.MenuView,
		extension.MenuGo, extension.MenuHelp, extension.MenuManage, extension.MenuEditorContext, extension.MenuExplorerContext,
	} {
		known := false
		for _, it := range tb.menus.All() {
			known = known || it.Menu == id || it.Submenu == id
		}
		if !known {
			t.Errorf("no menu has the id %q", id)
		}
	}
}

// panickyManifest is an extension whose Manifest panics.
type panickyManifest struct{}

func (panickyManifest) Manifest() extension.Manifest { panic("no manifest") }

func (panickyManifest) Activate(context.Context, extension.API) error { return nil }
