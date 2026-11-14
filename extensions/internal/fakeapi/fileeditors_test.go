package fakeapi

import (
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheFakeFileEditors(t *testing.T) {
	a := New(t, extension.Manifest{ID: "v", Name: "V", Contributes: extension.Contributes{FileEditors: []extension.FileEditorSpec{
		{ID: "v.image", Name: "Image", Globs: []string{"*.png"}, Priority: extension.FileEditorDefault}}}}, extension.ScopeInfo{})
	if _, err := a.OpenFileEditor("v.image", "/w/cat.png", nil); err == nil {
		t.Fatal("an editor that was not provided opened")
	} else if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("API.OpenFileEditor.FakeFileEditorNotProvided") {
		t.Errorf("err = %v", err)
	}
	for _, tt := range []struct {
		id    string
		p     extension.FileEditorProvider
		route string
	}{
		{"v.nope", extension.FileEditorProvider{Open: func(extension.FileEditorFile, extension.FileEditorTab) (extension.FileEditorContent, error) {
			return extension.FileEditorContent{}, nil
		}}, "fileEditors.Provide.ExtensionFileEditorNotDeclared"},
		{"v.image", extension.FileEditorProvider{}, "fileEditors.Provide.ExtensionFileEditorInvalid"},
	} {
		err := a.FileEditors().Provide(tt.id, tt.p)
		if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute(tt.route) {
			t.Errorf("%s: err = %v", tt.id, err)
		}
	}
	var changes []extension.FileEditorChange
	closed := 0
	var tabOf extension.FileEditorTab
	if err := a.FileEditors().Provide("v.image", extension.FileEditorProvider{Open: func(file extension.FileEditorFile, tab extension.FileEditorTab) (extension.FileEditorContent, error) {
		tabOf = tab
		file.OnChange(func(c extension.FileEditorChange) { changes = append(changes, c) })
		tab.SetStatus([]extension.StatusItem{{ID: "size", Text: "1x1"}})
		tab.SetBusy(true)
		tab.Refresh()
		return extension.FileEditorContent{
			Build:     func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) },
			SaveState: func() (any, error) { return map[string]string{"path": file.Path(), "state": string(file.State())}, nil },
			OnClose:   func() { closed++ },
		}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	fe, err := a.OpenFileEditor("v.image", "/w/cat.png", []byte(`1`))
	if err != nil {
		t.Fatal(err)
	}
	fe.Change(true)
	if len(changes) != 1 || !changes[0].Deleted || len(fe.Status()) != 1 || !fe.Busy() || fe.Refreshes() != 1 {
		t.Errorf("changes %+v status %+v busy %v refreshes %d", changes, fe.Status(), fe.Busy(), fe.Refreshes())
	}
	if state, err := fe.SaveState(); err != nil || string(state) != `{"path":"/w/cat.png","state":"1"}` {
		t.Errorf("state %s %v", state, err)
	}
	// the tab: active as the window says, and the asks to show the file as text
	if tabOf == nil || tabOf.Active() {
		t.Fatal("the tab is active before SetActive")
	}
	fe.SetActive(true)
	tabOf.ReopenAsText()
	if !tabOf.Active() || fe.ReopenedAsText() != 1 {
		t.Errorf("active %v, reopened %d", tabOf.Active(), fe.ReopenedAsText())
	}
	// the clipboard keeps the images
	if err := a.Clipboard().WriteImage([]byte("png")); err != nil || len(a.ClipboardImages()) != 1 {
		t.Errorf("images %d, %v", len(a.ClipboardImages()), err)
	}
	fe.Close()
	fe.Change(false)
	if closed != 1 || len(changes) != 1 {
		t.Errorf("closed %d, changes after close %d", closed, len(changes))
	}
}
