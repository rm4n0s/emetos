package app

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/annotations"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The line annotations in the window (step 24.22): the editor of a document shows what the store has for its URI.

func annotationURI(a *groupApp, name string) string { return document.FileURI(a.files[name]).String() }

func TestLineAnnotationsOfTheStoreAreInTheEditorOfTheirDocument(t *testing.T) {
	a := diagnosticsApp(t)
	uri := annotationURI(a, "a.go")
	var followed []string
	a.win.Annotations.Set("emetos.test", "blame", uri, []annotations.Annotation{
		{Line: 2, Text: "Ada, 3 days ago", Color: "git.blame.editorDecorationForeground", Hover: "h", Link: func(d string) { followed = append(followed, d) }},
	})
	a.win.Annotations.Set("emetos.test", "other", annotationURI(a, "b.go"), []annotations.Annotation{{Line: 1, Text: "not here"}})
	a.pump()
	got := a.vm().LineAnnotations()
	if len(got) != 1 || got[0].Line != 2 || got[0].Text != "Ada, 3 days ago" || got[0].Color != "git.blame.editorDecorationForeground" || got[0].Hover != "h" {
		t.Fatalf("annotations %+v", got)
	}
	got[0].Link("command:x")
	if len(followed) != 1 || followed[0] != "command:x" {
		t.Errorf("the link does not reach the store's function: %v", followed)
	}

	// a new list replaces the old one, and removing the set takes the annotations away
	a.win.Annotations.Set("emetos.test", "blame", uri, []annotations.Annotation{{Line: 4, Text: "later"}})
	a.pump()
	if got := a.vm().LineAnnotations(); len(got) != 1 || got[0].Line != 4 {
		t.Errorf("after the update: %+v", got)
	}
	a.win.Annotations.Clear("emetos.test", "blame")
	a.pump()
	if got := a.vm().LineAnnotations(); len(got) != 0 {
		t.Errorf("after Clear: %+v", got)
	}
}

func TestLineAnnotationsStayInTheStoreWhenTheEditorClosesAndComeBackWithTheNextOne(t *testing.T) {
	a := diagnosticsApp(t)
	uri := annotationURI(a, "a.go")
	a.win.Annotations.Set("emetos.test", "blame", uri, []annotations.Annotation{{Line: 0, Text: "kept in the store"}})
	a.pump()
	if len(a.vm().LineAnnotations()) != 1 {
		t.Fatal("no annotation")
	}
	a.run("workbench.action.closeActiveEditor")
	if !a.win.Annotations.Has("emetos.test", "blame") {
		t.Error("closing the editor took the set away: it belongs to the extension")
	}
	a.show("a.go", false)
	a.pump()
	if got := a.vm().LineAnnotations(); len(got) != 1 || got[0].Text != "kept in the store" {
		t.Errorf("a new editor of the file does not show the annotations: %+v", got)
	}
}
