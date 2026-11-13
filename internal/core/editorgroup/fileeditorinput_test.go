package editorgroup

import (
	"encoding/json"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

func TestFileEditorInput(t *testing.T) {
	a := NewFileEditorInput("/w/pics/cat.png", "emetos.mediapreview.image")
	if a.TypeID() != FileEditorInputTypeID || a.Name() != "cat.png" || a.Description() != "/w/pics" || a.Title() != "/w/pics/cat.png" || a.Dirty() {
		t.Errorf("input = %+v", a)
	}
	if uri, ok := a.Resource(); !ok || uri != document.FileURI("/w/pics/cat.png") {
		t.Errorf("resource %v %v", uri, ok)
	}
	if a.Capabilities()&CapCanSplit == 0 {
		t.Error("a file editor cannot be split")
	}
	// the same file in another file editor is the same tab; another file is not, nor is another kind of input
	if !a.Matches(NewFileEditorInput("/w/pics/cat.png", "emetos.hex")) || a.Matches(NewFileEditorInput("/w/pics/dog.png", a.Editor())) || a.Matches(NewExtensionsInput()) {
		t.Error("Matches")
	}
	// the state comes from the editor, else from what the input was made with, and goes with Serialize and a copy
	a.SetState(json.RawMessage(`{"zoom":1}`))
	if string(a.State()) != `{"zoom":1}` {
		t.Errorf("state %s", a.State())
	}
	a.SetStateFunc(func() json.RawMessage { return json.RawMessage(`{"zoom":2}`) })
	v, ok := a.Serialize()
	if !ok {
		t.Fatal("Serialize failed")
	}
	b, err := ParseFileEditorInput(v)
	if err != nil {
		t.Fatal(err)
	}
	if b.Path() != a.Path() || b.Editor() != a.Editor() || string(b.RestoredState()) != `{"zoom":2}` {
		t.Errorf("restored %+v %s", b, b.RestoredState())
	}
	c := a.CopyInput().(*FileEditorInput)
	if c == a || c.Path() != a.Path() || string(c.RestoredState()) != `{"zoom":2}` {
		t.Errorf("copy %+v", c)
	}
}

func TestParseFileEditorInputRefuses(t *testing.T) {
	for _, tt := range []struct{ name, value string }{
		{"not JSON", "cat.png"},
		{"no editor", `{"path": "/w/cat.png"}`},
		{"a relative path", `{"path": "cat.png", "editor": "e.i"}`},
		{"no path", `{"editor": "e.i"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFileEditorInput(tt.value)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("ParseFileEditorInput.FileEditorInputInvalid") {
				t.Errorf("err = %v", err)
			}
		})
	}
}
