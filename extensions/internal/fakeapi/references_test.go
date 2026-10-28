package fakeapi

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
)

// What the fake does for the References extension (step 17e.3): the active editor, the clipboard, the title of a view and the
// locations of the language server.

func TestTheFakeHasAnActiveEditorWhenATestSaysSo(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	if _, ok := a.Documents().Active(); ok {
		t.Fatal("a new fake has an active editor")
	}
	want := extension.ActiveDocument{URI: "file:///a.go", LanguageID: "go", Version: 2, Selections: []extension.Selection{{Active: extension.Position{Line: 1, Col: 2}}}}
	a.SetActive(&want)
	got, ok := a.Documents().Active()
	if !ok || got.URI != want.URI || !slices.Equal(got.Selections, want.Selections) {
		t.Errorf("active = %+v %v", got, ok)
	}
	a.SetActive(nil)
	if _, ok := a.Documents().Active(); ok {
		t.Error("the editor stayed active")
	}
}

func TestTheFakeClipboardRecordsTheTexts(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	if err := a.Clipboard().WriteText("one"); err != nil {
		t.Fatal(err)
	}
	a.FailClipboard(errors.New("Broken", "no clipboard"))
	route(t, a.Clipboard().WriteText("two"), "Broken")
	a.FailClipboard(nil)
	if err := a.Clipboard().WriteText("three"); err != nil {
		t.Fatal(err)
	}
	if got := a.ClipboardTexts(); !slices.Equal(got, []string{"one", "three"}) {
		t.Errorf("texts = %v", got)
	}
}

func TestTheFakeViewKeepsItsTitle(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	h, err := a.Views().Provide("fk.view", extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	h.SetTitle("Implementations")
	if got := a.View("fk.view").Title; got != "Implementations" {
		t.Errorf("title = %q", got)
	}
}

func TestTheFakeAnswersLocationRequests(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	want := []extension.Location{{URI: "file:///a.go", Range: extension.Range{End: extension.Position{Col: 3}}}}
	a.AnswerLocations("references", func(uri string, pos extension.Position) ([]extension.Location, error) {
		if uri != "file:///a.go" || pos.Line != 4 {
			t.Errorf("asked about %s %+v", uri, pos)
		}
		return want, nil
	})
	got := make(chan []extension.Location, 1)
	errs := make(chan error, 1)
	done := func(l []extension.Location, err error) { got <- l; errs <- err }
	if err := a.Languages().References(context.Background(), "file:///a.go", extension.Position{Line: 4}, done); err != nil {
		t.Fatal(err)
	}
	a.WaitFor("the answer", func() bool { return len(got) == 1 })
	if l, err := <-got, <-errs; err != nil || !slices.Equal(l, want) {
		t.Errorf("answer = %+v %v", l, err)
	}
	if reqs := a.LocationRequests(); len(reqs) != 1 || reqs[0].Feature != "references" || reqs[0].Position.Line != 4 {
		t.Errorf("requests = %+v", reqs)
	}

	// a feature nobody answers has no provider
	if err := a.Languages().Implementations(context.Background(), "file:///a.go", extension.Position{}, done); err != nil {
		t.Fatal(err)
	}
	a.WaitFor("the answer", func() bool { return len(got) == 1 })
	<-got
	route(t, <-errs, "languages.locations.func1.ExtensionNoProvider")

	// a cancelled request is answered with the error of its context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Languages().References(ctx, "file:///a.go", extension.Position{Line: 4}, done); err != nil {
		t.Fatal(err)
	}
	a.WaitFor("the answer", func() bool { return len(got) == 1 })
	<-got
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v", err)
	}

	route(t, a.Languages().References(context.Background(), "file:///a.go", extension.Position{}, nil), "languages.locations.FakeRequestInvalid")
}
