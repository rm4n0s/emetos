package exthost

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The contributions to the built-in parts (step 11.5): Output channels, Problems markers, the folder of the workspace,
// context keys and showing a document.

// grabAPI makes a rig with an extension that keeps its API, starts a tab named name and returns the tab and the API.
func grabAPI(t *testing.T, id string, tweak func(r *rig)) (*rig, *tab, extension.API) {
	t.Helper()
	var api extension.API
	f, _ := factory(manifest(id), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	if tweak != nil {
		tweak(r)
	}
	tb := r.newTab("one")
	tb.scope.Start()
	if api == nil {
		t.Fatalf("the extension %s was not activated", id)
	}
	return r, tb, api
}

func mustRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestAnOutputChannelAppearsWritesAndGoesWithItsExtension(t *testing.T) {
	_, tb, api := grabAPI(t, "ext", nil)
	ch, err := api.Output().Channel("Ext Log")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tb.channels.Names(), "Ext Log") || ch.Name() != "Ext Log" {
		t.Errorf("a new channel is not listed: %v", tb.channels.Names())
	}
	ch.AppendLine("one")
	ch.AppendLine("two")
	if got := tb.channels.Lines("Ext Log"); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("lines = %v", got)
	}
	ch.Clear()
	if got := tb.channels.Lines("Ext Log"); len(got) != 0 {
		t.Errorf("after Clear: %v", got)
	}
	ch.Show(true)
	if !slices.Equal(tb.shownChannels, []string{"Ext Log"}) {
		t.Errorf("Show selected %v", tb.shownChannels)
	}
	ch.Dispose()
	ch.Dispose() // a second time does nothing
	if slices.Contains(tb.channels.Names(), "Ext Log") {
		t.Error("a disposed channel is listed")
	}
	// the same name can be opened again, and a channel that was not disposed goes with the activation
	if _, err := api.Output().Channel("Ext Log"); err != nil {
		t.Fatal(err)
	}
	ch2, _ := api.Output().Channel("Other")
	tb.disabled = []string{"ext"}
	tb.scope.Refresh()
	if names := tb.channels.Names(); slices.Contains(names, "Ext Log") || slices.Contains(names, "Other") {
		t.Errorf("channels stayed after the extension was turned off: %v", names)
	}
	ch2.AppendLine("late") // does nothing, and does not bring the channel back
	if slices.Contains(tb.channels.Names(), "Other") {
		t.Error("a channel of a deactivated extension was written to")
	}
}

func TestOutputChannelErrors(t *testing.T) {
	_, _, api := grabAPI(t, "ext", nil)
	if _, err := api.Output().Channel("Twice"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, channel, route string
	}{
		{"an empty name", "  ", "outputAPI.Channel.ExtensionOutputChannelInvalid"},
		{"the log of Emetos", logs.MainChannel, "outputAPI.Channel.ExtensionOutputChannelInvalid"},
		{"a channel the extension has open", "Twice", "outputAPI.Channel.ExtensionOutputChannelDuplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := api.Output().Channel(tt.channel)
			mustRoute(t, err, tt.route)
		})
	}
}

func TestOutputChannelsOfTwoTabsAreSharedAndGoWithTheLast(t *testing.T) {
	var apis []extension.API
	f, _ := factory(manifest("ext"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { apis = append(apis, a); return nil }
	})
	r := newRig(t, f)
	a, b := r.newTab("a"), r.newTab("b")
	b.channels = a.channels
	a.scope.opts.Channels, b.scope.opts.Channels = a.channels, a.channels
	a.scope.Start()
	b.scope.Start()
	for _, api := range apis {
		if _, err := api.Output().Channel("Shared"); err != nil {
			t.Fatal(err)
		}
	}
	a.disabled = []string{"ext"}
	a.scope.Refresh()
	if !slices.Contains(a.channels.Names(), "Shared") {
		t.Error("the channel went with the first tab")
	}
	b.scope.Close()
	if slices.Contains(a.channels.Names(), "Shared") {
		t.Error("the channel stayed after the last tab let go")
	}
}

func TestProblemsMarkersAreTheExtensionsAndGoWithIt(t *testing.T) {
	var other extension.API
	g, _ := factory(manifest("other"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { other = a; return nil }
	})
	var api extension.API
	f, _ := factory(manifest("ext"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f, g)
	tb := r.newTab("one")
	tb.scope.Start()
	uri := "file:///work/one/a.go"
	err := api.Problems().Set("lint", uri, []extension.Marker{
		{Message: "plain", StartLine: 3, StartColumn: 4},
		{Severity: extension.SeverityWarning, Message: "careful", Source: "mylint", Code: "W1", StartLine: 5, StartColumn: 1, EndLine: 5, EndColumn: 9},
		{Severity: extension.SeverityInfo, Message: "fyi", StartLine: 6, StartColumn: 1},
		{Message: ""}, // dropped
	})
	if err != nil {
		t.Fatal(err)
	}
	got := tb.markers.Read(problems.ReadFilter{Resource: "/work/one/a.go"})
	if len(got) != 3 {
		t.Fatalf("%d markers: %+v", len(got), got)
	}
	if got[0].Severity != problems.SeverityError || got[0].Source != "Ext ext" || got[0].Owner != "ext:lint" || got[0].StartLine != 3 {
		t.Errorf("first marker = %+v", got[0])
	}
	if got[1].Severity != problems.SeverityWarning || got[1].Source != "mylint" || got[1].Code != "W1" || got[1].EndColumn != 9 {
		t.Errorf("second marker = %+v", got[1])
	}
	if got[2].Severity != problems.SeverityInfo {
		t.Errorf("third marker = %+v", got[2])
	}
	// the same owner name in another extension is another owner
	if err := other.Problems().Set("lint", uri, []extension.Marker{{Message: "theirs"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(tb.markers.Read(problems.ReadFilter{Resource: "/work/one/a.go"})); n != 4 {
		t.Errorf("%d markers after the other extension set its own", n)
	}
	// setting none removes, Clear removes every file of the owner
	if err := api.Problems().Set("lint", uri, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(tb.markers.Read(problems.ReadFilter{Owner: "ext:lint"})); n != 0 {
		t.Errorf("%d markers after Set with none", n)
	}
	_ = api.Problems().Set("lint", uri, []extension.Marker{{Message: "a"}})
	_ = api.Problems().Set("lint", "file:///work/one/b.go", []extension.Marker{{Message: "b"}})
	api.Problems().Clear("lint")
	if n := len(tb.markers.Read(problems.ReadFilter{Owner: "ext:lint"})); n != 0 {
		t.Errorf("%d markers after Clear", n)
	}
	// they go when the extension is turned off, the other extension's stay
	_ = api.Problems().Set("lint", uri, []extension.Marker{{Message: "again"}})
	tb.disabled = []string{"ext"}
	tb.scope.Refresh()
	got = tb.markers.Read(problems.ReadFilter{})
	if len(got) != 1 || got[0].Owner != "other:lint" {
		t.Errorf("after turning the extension off: %+v", got)
	}
}

func TestProblemsErrors(t *testing.T) {
	_, _, api := grabAPI(t, "ext", nil)
	tests := []struct {
		name, owner, uri, route string
	}{
		{"no owner", " ", "file:///a.go", "problemsAPI.Set.ExtensionProblemsOwnerInvalid"},
		{"a URI that is not one", "o", "a.go", "ParseURI.URIInvalid"},
		{"an untitled document", "o", "untitled:1", "problemsAPI.Set.ExtensionProblemsURIInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustRoute(t, api.Problems().Set(tt.owner, tt.uri, []extension.Marker{{Message: "m"}}), tt.route)
		})
	}
}

func TestTheFolderOfTheWorkspaceIsReadOnlyAndStaysInside(t *testing.T) {
	folder := t.TempDir()
	outside := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		full := filepath.Join(folder, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "hello\n")
	write("sub/b.txt", "bee\n")
	write("binary.bin", "\xff\xfe\x00bad")
	write("big.txt", strings.Repeat("x", extension.MaxReadFileSize+1))
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(folder, "link")); err != nil {
		t.Skipf("no symbolic links: %v", err)
	}
	_, tb, api := grabAPI(t, "ext", func(r *rig) { r.folders["one"] = folder })
	tb.files = []string{"a.txt", "sub/b.txt"}
	tb.complete = false
	list, err := api.Workspace().Files()
	if err != nil || !slices.Equal(list.Files, []string{"a.txt", "sub/b.txt"}) || list.Complete {
		t.Errorf("Files = %+v, %v", list, err)
	}
	tb.complete = true
	if list, _ := api.Workspace().Files(); !list.Complete {
		t.Error("Files says the walk is not complete after it was")
	}
	list.Files[0] = "changed"
	if tb.files[0] != "a.txt" {
		t.Error("the list the extension got is the window's own")
	}
	ctx := context.Background()
	if text, err := api.Workspace().ReadFile(ctx, "sub/b.txt"); err != nil || text != "bee\n" {
		t.Errorf("ReadFile = %q, %v", text, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	tests := []struct {
		name, path, route string
		ctx               context.Context
	}{
		{"a path that leaves the folder", "../x", "workspaceAPI.ReadFile.ExtensionFilePathInvalid", ctx},
		{"an absolute path", filepath.Join(outside, "secret.txt"), "workspaceAPI.ReadFile.ExtensionFilePathInvalid", ctx},
		{"the folder itself", ".", "workspaceAPI.ReadFile.ExtensionFilePathInvalid", ctx},
		{"a path that goes up after it went down", "sub/../../x", "workspaceAPI.ReadFile.ExtensionFilePathInvalid", ctx},
		{"a link that points out of the folder", "link/secret.txt", "workspaceAPI.ReadFile.ExtensionFileReadFailed", ctx},
		{"a file that is not there", "missing.txt", "workspaceAPI.ReadFile.ExtensionFileReadFailed", ctx},
		{"a file that is too large", "big.txt", "workspaceAPI.ReadFile.ExtensionFileTooLarge", ctx},
		{"a file that is not text", "binary.bin", "workspaceAPI.ReadFile.ExtensionFileNotText", ctx},
		{"a cancelled context", "a.txt", "workspaceAPI.ReadFile.ExtensionFileReadFailed", cancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, err := api.Workspace().ReadFile(tt.ctx, tt.path)
			if text != "" {
				t.Errorf("read %q", text)
			}
			mustRoute(t, err, tt.route)
		})
	}
}

func TestAScopeWithNoFolderHasNoFiles(t *testing.T) {
	_, _, api := grabAPI(t, "ext", func(r *rig) { r.folders["one"] = "" })
	_, err := api.Workspace().Files()
	mustRoute(t, err, "workspaceAPI.Files.ExtensionNoFolder")
	_, err = api.Workspace().ReadFile(context.Background(), "a.txt")
	mustRoute(t, err, "workspaceAPI.ReadFile.ExtensionNoFolder")
}

func TestFilesChangedIsACoreEvent(t *testing.T) {
	var api extension.API
	var got []extension.FilesChanged
	f, _ := factory(manifest("ext"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error {
			api = a
			_, err := a.Events().Subscribe(extension.TopicFilesChanged, func(e extension.Event) {
				v, err := extension.EventValue[extension.FilesChanged](e)
				if err != nil {
					t.Error(err)
				}
				got = append(got, v)
			})
			return err
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	_ = api
	want := extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileRenamed, Path: "b.go", OldPath: "a.go"}, {Kind: extension.FileListed}}}
	if err := tb.scope.PublishCore(extension.TopicFilesChanged, want); err != nil {
		t.Fatal(err)
	}
	tb.pump()
	if len(got) != 1 || len(got[0].Changes) != 2 || got[0].Changes[0] != want.Changes[0] {
		t.Errorf("got %+v", got)
	}
}

func TestContextKeysBelongToTheExtension(t *testing.T) {
	_, tb, api := grabAPI(t, "emetos.ck", nil)
	if err := api.Context().Set("emetos.ck.running", true); err != nil {
		t.Fatal(err)
	}
	if tb.ctxKeys.Get("emetos.ck.running") != true {
		t.Error("the key is not set")
	}
	api.Context().Reset("emetos.ck.running")
	if tb.ctxKeys.Get("emetos.ck.running") != nil {
		t.Error("the key stayed after Reset")
	}
	tests := []struct{ name, key string }{
		{"a key of the editor", "editorFocus"},
		{"a key of another extension", "other.running"},
		{"the bare prefix", "emetos.ck."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustRoute(t, api.Context().Set(tt.key, true), "contextAPI.Set.ExtensionContextKeyForeign")
		})
	}
	api.Context().Reset("editorFocus") // does nothing to a key that is not the extension's
	_ = api.Context().Set("emetos.ck.a", "x")
	tb.disabled = []string{"emetos.ck"}
	tb.scope.Refresh()
	if tb.ctxKeys.Get("emetos.ck.a") != nil {
		t.Error("the key stayed after the extension was turned off")
	}
}

// showingDocs is a window that can show documents.
type showingDocs struct {
	docSource
	shown []string
}

func (d *showingDocs) Show(uri string, line, col int, preview bool) error {
	d.shown = append(d.shown, strings.Join([]string{uri, string(rune('0' + line)), string(rune('0' + col))}, " "))
	if !preview {
		d.shown = append(d.shown, "kept")
	}
	return nil
}

func TestADocumentIsShownThroughTheWindow(t *testing.T) {
	_, tb, api := grabAPI(t, "ext", nil)
	err := api.Documents().Show("file:///work/one/a.go", 3, 4, true)
	mustRoute(t, err, "documentsAPI.Show.ExtensionDocumentUnavailable")
	docs := &showingDocs{}
	tb.scope.opts.Documents = docs
	if err := api.Documents().Show("file:///work/one/a.go", 3, 4, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(docs.shown, []string{"file:///work/one/a.go 3 4", "kept"}) {
		t.Errorf("shown = %v", docs.shown)
	}
}

func TestViewsAndOutputAreUnavailableWithoutTheirServices(t *testing.T) {
	var api extension.API
	f, _ := factory(manifest("ext"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.opts.Channels, tb.scope.opts.Markers, tb.scope.opts.Keys = nil, nil, nil
	tb.scope.Start()
	_, err := api.Output().Channel("x")
	mustRoute(t, err, "outputAPI.Channel.ExtensionOutputUnavailable")
	mustRoute(t, api.Problems().Set("o", "file:///a", nil), "problemsAPI.Set.ExtensionProblemsUnavailable")
	mustRoute(t, api.Context().Set("ext.k", 1), "contextAPI.Set.ExtensionContextUnavailable")
}

func TestTheAPIOfADeactivatedExtensionRefusesTheNewParts(t *testing.T) {
	m := manifest("ext")
	m.Contributes.PanelViews = []extension.PanelViewSpec{{ID: "ext.v", Title: "V"}}
	var api extension.API
	f, _ := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	tb.scope.Close()
	for name, err := range map[string]error{
		"Output":   func() error { _, e := api.Output().Channel("c"); return e }(),
		"Problems": api.Problems().Set("o", "file:///a", nil),
		"Context":  api.Context().Set("ext.k", 1),
		"Views": func() error {
			_, e := api.Views().Provide("ext.v", extension.ViewContent{Build: func(w widgets.BuildContext) widgets.Widget { return nil }})
			return e
		}(),
		"Show": api.Documents().Show("file:///a", 1, 1, true),
	} {
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "ExtensionDeactivated" {
			t.Errorf("%s after deactivation: %v", name, err)
		}
	}
}
