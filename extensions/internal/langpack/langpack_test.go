package langpack

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestAPackDeclaresItsLanguages(t *testing.T) {
	assets := fstest.MapFS{"assets/package.json": {Data: []byte(`{"name": "x"}`)}}
	spec := Spec{ID: "emetos.x", Name: "X Language Basics", Description: "Highlighting for X.", Assets: assets}
	a, b := New(spec), New(spec)
	if a == b {
		t.Error("two calls made the same extension")
	}
	m := a.Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "emetos.x" || len(m.ActivationEvents) != 1 || m.ActivationEvents[0] != extension.OnStartup {
		t.Errorf("manifest = %+v", m)
	}
	// the package.json is at the root of the file system the manifest gives
	if _, err := fs.Stat(m.Contributes.Languages, "package.json"); err != nil {
		t.Errorf("the languages are not rooted at the assets folder: %v", err)
	}
	if err := a.Activate(context.Background(), nil); err != nil {
		t.Errorf("Activate: %v", err)
	}
}

func TestAPackWhoseFolderIsNotAPathFailsToActivate(t *testing.T) {
	ext := New(Spec{ID: "emetos.x", Name: "X", Assets: fstest.MapFS{}, Dir: "../outside"})
	if ext.Manifest().Contributes.Languages != nil {
		t.Error("a pack with no assets contributes languages")
	}
	err := ext.Activate(context.Background(), nil)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("New.LanguagePackAssetsInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
