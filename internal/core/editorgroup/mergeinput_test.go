package editorgroup

import (
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func TestMergeInputLabels(t *testing.T) {
	opts := TextInputOpts{Folder: "/w", Home: "/home/me"}
	for _, tc := range []struct {
		name                        string
		result                      string
		wantName, wantDesc, wantTip string
	}{
		{"a file in the folder", "/w/src/main.go", "Merging: main.go", "src", "Merging: /w/src/main.go"},
		{"a file of the folder itself", "/w/main.go", "Merging: main.go", "", "Merging: /w/main.go"},
		{"a file in the home folder", "/home/me/main.go", "Merging: main.go", "~", "Merging: ~/main.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewMergeInput(fileDoc(tc.result, ""), MergeSource{Kind: MergeMarkers, Result: tc.result}, opts)
			if in.Name() != tc.wantName || in.Description() != tc.wantDesc || in.Title() != tc.wantTip {
				t.Errorf("name %q description %q title %q", in.Name(), in.Description(), in.Title())
			}
			if in.TypeID() != "emetos.merge" {
				t.Errorf("type %q", in.TypeID())
			}
			if _, ok := in.Resource(); ok {
				t.Error("a merge input shows a resource")
			}
			if in.Capabilities().Has(CapCanSplit) {
				t.Error("a merge input can be split")
			}
		})
	}
}

func TestMergeInputFollowsTheResult(t *testing.T) {
	res := fileDoc("/w/a.go", "a\n")
	in := NewMergeInput(res, MergeSource{Kind: MergeMarkers, Result: "/w/a.go"}, TextInputOpts{})
	var events []InputEventKind
	in.Subscribe(func(e InputEvent) { events = append(events, e.Kind) })
	if in.Dirty() || EditedDocument(in) != res || in.Result() != res {
		t.Fatal("the input is dirty at first or edits the wrong document")
	}
	if _, err := res.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if !in.Dirty() || len(events) != 1 || events[0] != InputDirty {
		t.Errorf("dirty %v events %v after an edit", in.Dirty(), events)
	}
	if _, err := res.Undo(); err != nil {
		t.Fatal(err)
	}
	if in.Dirty() || len(events) != 2 {
		t.Errorf("dirty %v events %v after an undo", in.Dirty(), events)
	}
	// a document renamed gives the tab another name
	res.SetURI(document.FileURI("/w/b.go"))
	if in.Name() != "Merging: b.go" || events[len(events)-1] != InputLabel {
		t.Errorf("name %q events %v", in.Name(), events)
	}
	in.Dispose()
	before := len(events) // Dispose tells that the input will be disposed
	if _, err := res.Edit([]document.Edit{{Range: buffer.Range{}, Text: "y"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(events) != before {
		t.Errorf("a disposed input sent %v", events)
	}
}

func TestMergeInputMatchesAndTabKey(t *testing.T) {
	a := NewMergeInput(fileDoc("/w/a.go", ""), MergeSource{Kind: MergeMarkers, Result: "/w/a.go"}, TextInputOpts{})
	a2 := NewMergeInput(fileDoc("/w/a.go", ""), MergeSource{Kind: MergeMarkers, Result: "/w/a.go"}, TextInputOpts{})
	b := NewMergeInput(fileDoc("/w/b.go", ""), MergeSource{Kind: MergeMarkers, Result: "/w/b.go"}, TextInputOpts{})
	text := NewTextInput(fileDoc("/w/a.go", ""), TextInputOpts{})
	if !a.Matches(a2) || a.Matches(b) || a.Matches(text) || text.Matches(a) {
		t.Error("Matches is wrong")
	}
	if a.TabKey() == b.TabKey() {
		t.Error("two merges share a tab key")
	}
}

func TestMergeInputSerializeRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source MergeSource
	}{
		{"markers", MergeSource{Kind: MergeMarkers, Result: "/w/a.go"}},
		{"files", MergeSource{Kind: MergeFiles, Input1: "/t/in1", Input2: "/t/in2", Base: "/t/base", Result: "/w/out.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewMergeInput(fileDoc(tc.source.Result, ""), tc.source, TextInputOpts{})
			value, ok := in.Serialize()
			if !ok {
				t.Fatal("cannot serialize")
			}
			source, uri, err := ParseMergeInput(value)
			if err != nil {
				t.Fatal(err)
			}
			if source != tc.source || uri != document.FileURI(tc.source.Result) {
				t.Errorf("source %+v uri %v from %s", source, uri, value)
			}
		})
	}
	untitled := NewMergeInput(document.New(document.UntitledURI(1), "", document.NewOptions()), MergeSource{Kind: MergeMarkers}, TextInputOpts{})
	if _, ok := untitled.Serialize(); ok {
		t.Error("a merge into an untitled document serializes")
	}
}

func TestParseMergeInputErrors(t *testing.T) {
	for _, tc := range []struct {
		name, value, route string
	}{
		{"not JSON", "{", "ParseMergeInput.MergeInputInvalid"},
		{"no result", `{"kind":"markers"}`, "ParseMergeInput.MergeInputIncomplete"},
		{"unknown kind", `{"kind":"other","result":"file:///a"}`, "ParseMergeInput.MergeInputIncomplete"},
		{"files without a base", `{"kind":"files","input1":"/1","input2":"/2","result":"file:///a"}`, "ParseMergeInput.MergeInputFilesIncomplete"},
		{"a result that is not a file", `{"kind":"markers","result":"untitled:1"}`, "ParseMergeInput.MergeInputResultNotFile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseMergeInput(tc.value)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
