package langpack

import (
	"context"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
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

func TestAPackDeclaresItsLanguageServers(t *testing.T) {
	assets := fstest.MapFS{"assets/package.json": {Data: []byte(`{"name": "x"}`)}}
	servers := []extension.LanguageServerSpec{{ID: "xls", Name: "X LS", Languages: []string{"x"}, Program: "xls"}}
	m := New(Spec{ID: "emetos.x", Name: "X", Assets: assets, LanguageServers: servers}).Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.LanguageServers) != 1 || m.Contributes.LanguageServers[0].ID != "xls" {
		t.Errorf("servers = %+v", m.Contributes.LanguageServers)
	}
}

func testServer() *langserver.Config {
	return &langserver.Config{
		ID: "xls", Name: "X LS", Languages: []string{"x"}, Category: "X", TraceKey: "xls.trace.server",
		Programs: []langserver.Program{{Name: "xls", PathSetting: "xls.path"}},
	}
}

func TestAPackWithAServerDeclaresItAndItsSettings(t *testing.T) {
	assets := fstest.MapFS{"assets/package.json": {Data: []byte(`{"name": "x"}`)}}
	spec := Spec{ID: "emetos.x", Name: "X", Assets: assets,
		LanguageServers: []extension.LanguageServerSpec{{ID: "other", Name: "Other", Languages: []string{"x"}, Program: "other"}},
		Server:          testServer()}
	m := New(spec).Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range m.Contributes.LanguageServers {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []string{"other", "xls"}) {
		t.Errorf("servers = %v", ids)
	}
	sv := m.Contributes.LanguageServers[1]
	if !sv.OwnUI || sv.TraceKey != "xls.trace.server" || sv.Program != "xls" {
		t.Errorf("server = %+v", sv)
	}
	if len(m.Contributes.Settings) != 1 || m.Contributes.Settings[0].Key != "xls.path" {
		t.Errorf("settings = %+v", m.Contributes.Settings)
	}
	if !slices.Contains(m.Events.Subscribes, extension.TopicEditorActiveChanged) {
		t.Errorf("subscribes = %v", m.Events.Subscribes)
	}
	// the spec's own list is not changed by the manifest, and the manifest does not share it
	if len(spec.LanguageServers) != 1 {
		t.Errorf("Spec.LanguageServers = %v", spec.LanguageServers)
	}
	if again := New(spec).Manifest(); len(again.Contributes.LanguageServers) != 2 {
		t.Errorf("the second manifest has %d servers", len(again.Contributes.LanguageServers))
	}
}

func TestAPackWithAServerAttachesItWhenItIsActivated(t *testing.T) {
	assets := fstest.MapFS{"assets/package.json": {Data: []byte(`{"name": "x"}`)}}
	ext := New(Spec{ID: "emetos.x", Name: "X", Assets: assets, Server: testServer()})
	if Controller(ext) != nil {
		t.Error("a controller before the activation")
	}
	api := fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Name: "tab", Folder: t.TempDir()})
	if err := ext.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	if Controller(ext) == nil || api.Launcher("xls") == nil || api.StatusItem(langserver.StatusID("xls")) == nil {
		t.Errorf("controller %v, launcher %v, item %v", Controller(ext), api.Launcher("xls"), api.StatusItem(langserver.StatusID("xls")))
	}
	if Controller(&notAPack{}) != nil {
		t.Error("a controller for an extension that is not a pack")
	}
}

type notAPack struct{ extension.Extension }

func TestAPackWithABadServerFailsToActivate(t *testing.T) {
	assets := fstest.MapFS{"assets/package.json": {Data: []byte(`{"name": "x"}`)}}
	bad := testServer()
	bad.Programs = nil
	ext := New(Spec{ID: "emetos.x", Name: "X", Assets: assets, Server: bad})
	api := fakeapi.New(t, extension.Manifest{ID: "emetos.x", Name: "X"}, extension.ScopeInfo{})
	err := ext.Activate(context.Background(), api)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Attach.ServerConfigInvalid") {
		t.Errorf("err = %v", err)
	}
}
