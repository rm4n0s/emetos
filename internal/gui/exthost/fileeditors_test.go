package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	apperrors "github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
)

// The file editors of the extensions in a scope (step 36.1).

// feManifest declares an image preview (default, for PNG and JPEG) and a hex view (an option, for every file).
func feManifest() extension.Manifest {
	m := extension.Manifest{ID: "fe", Name: "Viewers", Version: "1.0.0"}
	m.Contributes.FileEditors = []extension.FileEditorSpec{
		{ID: "fe.image", Name: "Image Preview", Icon: "file-media", Globs: []string{"*.png", "*.{jpg,jpeg}"}, Priority: extension.FileEditorDefault},
		{ID: "fe.hex", Name: "Hex", Globs: []string{"*"}, Priority: extension.FileEditorOption},
	}
	return m
}

// imageEditor is what the provider of the tests made for a tab.
type imageEditor struct {
	file    extension.FileEditorFile
	tab     extension.FileEditorTab
	zoom    int
	changes []extension.FileEditorChange
	closed  int
}

// feTab is a tab of a scope with the extension fe; open is the Open of fe.image (nil: one that makes an imageEditor).
func feTab(t *testing.T, open func(extension.FileEditorFile, extension.FileEditorTab) (extension.FileEditorContent, error)) (*tab, *[]*imageEditor) {
	t.Helper()
	var made []*imageEditor
	if open == nil {
		open = func(file extension.FileEditorFile, tab extension.FileEditorTab) (extension.FileEditorContent, error) {
			e := &imageEditor{file: file, tab: tab, zoom: 1}
			var saved struct{ Zoom int }
			if json.Unmarshal(file.State(), &saved) == nil && saved.Zoom != 0 {
				e.zoom = saved.Zoom
			}
			file.OnChange(func(c extension.FileEditorChange) { e.changes = append(e.changes, c) })
			made = append(made, e)
			return extension.FileEditorContent{
				Build:     func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) },
				SaveState: func() (any, error) { return map[string]int{"Zoom": e.zoom}, nil },
				OnClose:   func() { e.closed++ },
			}, nil
		}
	}
	f, _ := factory(feManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			return api.FileEditors().Provide("fe.image", extension.FileEditorProvider{Open: open})
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	tb.pump()
	return tb, &made
}

func editorIDs(infos []FileEditorInfo) []string {
	var out []string
	for _, i := range infos {
		out = append(out, i.ID)
	}
	return out
}

func TestTheFileEditorsOfAFile(t *testing.T) {
	tb, _ := feTab(t, nil)
	tests := []struct {
		path string
		want []string
	}{
		{"/w/cat.png", []string{"fe.image", "fe.hex"}},
		{"/w/CAT.JPG", []string{"fe.image", "fe.hex"}},
		{"/w/notes.txt", []string{"fe.hex"}},
	}
	for _, tt := range tests {
		if got := editorIDs(tb.scope.FileEditorsFor(tt.path)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.path, got, tt.want)
		}
	}
	infos := tb.scope.FileEditorsFor("/w/cat.png")
	if infos[0].Name != "Image Preview" || infos[0].Extension != "Viewers" || !infos[0].Default || infos[0].Icon == 0 || infos[1].Default {
		t.Errorf("infos %+v", infos)
	}
	// a turned-off extension has none
	tb.disabled = []string{"fe"}
	tb.scope.Refresh()
	if got := tb.scope.FileEditorsFor("/w/cat.png"); len(got) != 0 {
		t.Errorf("a disabled extension still has file editors: %v", got)
	}
}

func TestTheEditorAFileOpensIn(t *testing.T) {
	tb, _ := feTab(t, nil)
	tests := []struct {
		name  string
		path  string
		assoc map[string]string
		want  string
	}{
		{"the default editor", "/w/cat.png", nil, "fe.image"},
		{"an option is not the default", "/w/notes.txt", nil, ""},
		{"text by association", "/w/cat.png", map[string]string{"*.png": "default"}, ""},
		{"an option by association", "/w/notes.txt", map[string]string{"*.txt": "fe.hex"}, "fe.hex"},
		{"the most specific key wins", "/w/cat.png", map[string]string{"*": "fe.hex", "cat.png": "default"}, ""},
		{"a key with a slash matches the path", "/w/pics/cat.png", map[string]string{"**/pics/*.png": "fe.hex"}, "fe.hex"},
		{"an editor that does not match the file is ignored", "/w/notes.txt", map[string]string{"*.txt": "fe.image"}, ""},
		{"an unknown editor is ignored", "/w/cat.png", map[string]string{"*.png": "nobody.editor"}, "fe.image"},
		{"keys without regard to case", "/w/CAT.PNG", map[string]string{"*.png": "fe.hex"}, "fe.hex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tb.scope.ResolveFileEditor(tt.path, tt.assoc); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAFileEditorTab(t *testing.T) {
	tb, made := feTab(t, nil)
	key := new(int)
	req := FileEditorRequest{Key: key, EditorID: "fe.image", Path: "/w/cat.png", Folder: "/w", State: json.RawMessage(`{"Zoom":3}`)}
	body, ok, err := tb.scope.FileEditorBody(req)
	if !ok || err != nil || body == nil {
		t.Fatalf("body %v %v %v", body, ok, err)
	}
	// showing the box activates the extension, which provides the editor and opens the tab
	tb.scope.Fire(extension.OnFileEditor("fe.image"))
	tb.pump()
	if tb.scope.State("fe") != StateActive || len(*made) != 1 {
		t.Fatalf("fe is %v, %d tabs made", tb.scope.State("fe"), len(*made))
	}
	e := (*made)[0]
	if e.file.Path() != "/w/cat.png" || e.file.URI() != "file:///w/cat.png" || e.file.Folder() != "/w" || e.zoom != 3 {
		t.Errorf("file %q %q %q, zoom %d", e.file.Path(), e.file.URI(), e.file.Folder(), e.zoom)
	}
	if e.tab.Group() != 7 {
		t.Errorf("group %d", e.tab.Group())
	}
	// the same tab asks again: the same instance
	if _, ok, _ := tb.scope.FileEditorBody(req); !ok || len(*made) != 1 {
		t.Errorf("a second ask made another instance")
	}
	// the state is what SaveState gives
	e.zoom = 5
	if got := string(tb.scope.FileEditorState(key)); got != `{"Zoom":5}` {
		t.Errorf("state %s", got)
	}
	// the window asks which files are shown before it hands their changes over
	if !tb.scope.ShowsFile("/w/cat.png") || tb.scope.ShowsFile("/w/dog.png") {
		t.Errorf("ShowsFile: cat %v, dog %v", tb.scope.ShowsFile("/w/cat.png"), tb.scope.ShowsFile("/w/dog.png"))
	}
	// changes on disk reach the tabs of that file only
	tb.scope.FileEditorChanged("/w/cat.png", false)
	tb.scope.FileEditorChanged("/w/dog.png", true)
	tb.scope.FileEditorChanged("/w/cat.png", true)
	if len(e.changes) != 2 || e.changes[0].Deleted || !e.changes[1].Deleted {
		t.Errorf("changes %+v", e.changes)
	}
	// the status bar items show while the tab is the active editor
	e.tab.SetStatus([]extension.StatusItem{{ID: "size", Text: "1920x1080", Alignment: extension.StatusRight}, {ID: "zoom", Text: "Whole Image", Alignment: extension.StatusRight}})
	shown := func() int {
		n := 0
		for _, id := range tb.statusIDs() {
			if strings.HasSuffix(id, ":size") || strings.HasSuffix(id, ":zoom") {
				n++
			}
		}
		return n
	}
	if shown() != 0 {
		t.Error("the items show while the tab is not active")
	}
	tb.scope.SetActiveFileEditor(key)
	if shown() != 2 {
		t.Errorf("%d items show while the tab is active", shown())
	}
	e.tab.SetStatus([]extension.StatusItem{{ID: "size", Text: "800x600", Alignment: extension.StatusRight}})
	if shown() != 1 {
		t.Errorf("%d items after one was taken away", shown())
	}
	tb.scope.SetActiveFileEditor(nil)
	if shown() != 0 {
		t.Error("the items stay when the tab is not active")
	}
	// busy
	e.tab.SetBusy(true)
	if !tb.scope.FileEditorBusy(key) {
		t.Error("the busy mark is not kept")
	}
	// closing: OnClose, and the items go
	tb.scope.SetActiveFileEditor(key)
	tb.scope.ReleaseFileEditor(key)
	if e.closed != 1 || shown() != 0 || tb.scope.ShowsFile("/w/cat.png") {
		t.Errorf("closed %d, %d items left, shown %v", e.closed, shown(), tb.scope.ShowsFile("/w/cat.png"))
	}
	// an editor that is not declared is not available
	if _, ok, _ := tb.scope.FileEditorBody(FileEditorRequest{Key: key, EditorID: "fe.nope", Path: "/w/cat.png"}); ok {
		t.Error("an unknown editor is available")
	}
}

func TestAFileEditorThatFailsShowsItsError(t *testing.T) {
	cases := []struct {
		name  string
		open  func(extension.FileEditorFile, extension.FileEditorTab) (extension.FileEditorContent, error)
		route string
	}{
		{"an error", func(extension.FileEditorFile, extension.FileEditorTab) (extension.FileEditorContent, error) {
			return extension.FileEditorContent{}, apperrors.New("ImageUnreadable", "the image cannot be read")
		}, ""},
		{"no Build", func(extension.FileEditorFile, extension.FileEditorTab) (extension.FileEditorContent, error) {
			return extension.FileEditorContent{}, nil
		}, "Scope.openFileEditor.ExtensionFileEditorContentInvalid"},
		{"a panic", func(extension.FileEditorFile, extension.FileEditorTab) (extension.FileEditorContent, error) {
			panic("open")
		}, "Scope.openFileEditor.func1.1.ExtensionFileEditorPanicked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tb, _ := feTab(t, tc.open)
			tb.scope.Fire(extension.OnFileEditor("fe.image"))
			tb.pump()
			_, ok, err := tb.scope.FileEditorBody(FileEditorRequest{Key: new(int), EditorID: "fe.image", Path: "/w/cat.png"})
			if !ok || err == nil {
				t.Fatalf("ok %v err %v", ok, err)
			}
			if tc.route != "" {
				route(t, err, tc.route)
			}
		})
	}
}

func TestProvidingAFileEditor(t *testing.T) {
	var errs []error
	f, _ := factory(feManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			open := func(extension.FileEditorFile, extension.FileEditorTab) (extension.FileEditorContent, error) {
				return extension.FileEditorContent{}, nil
			}
			errs = append(errs,
				api.FileEditors().Provide("fe.nope", extension.FileEditorProvider{Open: open}),
				api.FileEditors().Provide("fe.image", extension.FileEditorProvider{}),
				api.FileEditors().Provide("fe.image", extension.FileEditorProvider{Open: open}),
				api.FileEditors().Provide("fe.image", extension.FileEditorProvider{Open: open}),
			)
			return nil
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	tb.scope.Fire(extension.OnFileEditor("fe.image"))
	tb.pump()
	if len(errs) != 4 || errs[2] != nil {
		t.Fatalf("errors %v", errs)
	}
	route(t, errs[0], "fileEditorsAPI.Provide.ExtensionFileEditorNotDeclared")
	route(t, errs[1], "fileEditorsAPI.Provide.ExtensionFileEditorInvalid")
	route(t, errs[3], "fileEditorsAPI.Provide.ExtensionFileEditorProvided")
}

func TestAPanicInACallbackOfAFileEditorFailsTheExtension(t *testing.T) {
	cases := []struct {
		name  string
		do    func(tb *tab, key any)
		route string
	}{
		{"SaveState", func(tb *tab, key any) { tb.scope.FileEditorState(key) }, "Scope.FileEditorState.func1.1.ExtensionFileEditorPanicked"},
		{"OnClose", func(tb *tab, key any) { tb.scope.ReleaseFileEditor(key) }, "Scope.closeFileEditor.func1.ExtensionFileEditorPanicked"},
		{"OnChange", func(tb *tab, _ any) { tb.scope.FileEditorChanged("/w/cat.png", false) }, "Scope.FileEditorChanged.func1.1.ExtensionFileEditorPanicked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tb, _ := feTab(t, func(file extension.FileEditorFile, _ extension.FileEditorTab) (extension.FileEditorContent, error) {
				file.OnChange(func(extension.FileEditorChange) { panic("change") })
				return extension.FileEditorContent{
					Build:     func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) },
					SaveState: func() (any, error) { panic("save") },
					OnClose:   func() { panic("close") },
				}, nil
			})
			tb.scope.Fire(extension.OnFileEditor("fe.image"))
			tb.pump()
			key := new(int)
			if _, ok, err := tb.scope.FileEditorBody(FileEditorRequest{Key: key, EditorID: "fe.image", Path: "/w/cat.png"}); !ok || err != nil {
				t.Fatalf("ok %v err %v", ok, err)
			}
			tc.do(tb, key)
			tb.pump()
			if tb.scope.State("fe") != StateFailed {
				t.Fatalf("fe is %v after the panic", tb.scope.State("fe"))
			}
			route(t, tb.scope.Failure("fe").Err, tc.route)
		})
	}
}
