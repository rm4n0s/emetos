package editorgroup

import (
	"slices"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func fileDoc(path, text string) *document.Document {
	return document.New(document.FileURI(path), text, document.NewOptions())
}

func TestDiffInputLabels(t *testing.T) {
	opts := TextInputOpts{Folder: "/w", Home: "/home/me"}
	for _, tc := range []struct {
		name                        string
		original, modified          string
		wantName, wantDesc, wantTip string
	}{
		{"two files in one folder", "/w/src/old.go", "/w/src/new.go", "old.go ↔ new.go", "src", "/w/src/old.go ↔ /w/src/new.go"},
		{"files in different folders", "/w/a/x.go", "/w/b/x.go", "x.go ↔ x.go", "a ↔ b", "/w/a/x.go ↔ /w/b/x.go"},
		{"a file outside the folder", "/home/me/old.go", "/w/new.go", "old.go ↔ new.go", "~", "~/old.go ↔ /w/new.go"},
		{"the folder itself and another", "/w/a.go", "/home/me/b.go", "a.go ↔ b.go", "~", "/w/a.go ↔ ~/b.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewDiffInput(fileDoc(tc.original, ""), fileDoc(tc.modified, ""), opts)
			if in.Name() != tc.wantName || in.Description() != tc.wantDesc || in.Title() != tc.wantTip {
				t.Errorf("name %q description %q title %q", in.Name(), in.Description(), in.Title())
			}
			if in.TypeID() != "emetos.diff" {
				t.Errorf("type %q", in.TypeID())
			}
			if _, ok := in.Resource(); ok {
				t.Error("a diff input shows a resource")
			}
		})
	}
	// a virtual side is named by its label
	virtual := document.NewVirtual(document.VirtualURI("/saved/a.go"), document.VirtualContent{Text: "x", Label: "a.go (Saved)"})
	in := NewDiffInput(virtual, fileDoc("/w/a.go", ""), opts)
	if in.Name() != "a.go (Saved) ↔ a.go" || in.Title() != "a.go (Saved) ↔ /w/a.go" {
		t.Errorf("name %q title %q", in.Name(), in.Title())
	}
	if caps := in.Capabilities(); !caps.Has(CapCanSplit) || caps.Has(CapReadonly) || caps.Has(CapUntitled) {
		t.Errorf("capabilities %b", caps)
	}
	ro := NewDiffInput(fileDoc("/w/a.go", ""), virtual, opts)
	if !ro.Capabilities().Has(CapReadonly) {
		t.Error("a read-only modified side is not read-only")
	}
	untitled := NewDiffInput(fileDoc("/w/a.go", ""), document.New(document.UntitledURI(2), "", document.NewOptions()), opts)
	if !untitled.Capabilities().Has(CapUntitled) {
		t.Error("an untitled modified side is not untitled")
	}
}

func TestDiffInputFollowsTheModifiedDocument(t *testing.T) {
	orig, mod := fileDoc("/w/a.go", "a\n"), fileDoc("/w/b.go", "b\n")
	in := NewDiffInput(orig, mod, TextInputOpts{})
	var events []InputEventKind
	in.Subscribe(func(e InputEvent) { events = append(events, e.Kind) })
	if in.Dirty() || EditedDocument(in) != mod || in.Modified() != mod || in.Original() != orig {
		t.Fatal("the input is dirty at first or edits the wrong document")
	}
	edit := func(d *document.Document) {
		t.Helper()
		if _, err := d.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	// the original side is not what the input saves: editing it does not make the input dirty
	edit(orig)
	if in.Dirty() || len(events) != 0 {
		t.Errorf("an edit of the original made the input dirty: %v %v", in.Dirty(), events)
	}
	edit(mod)
	if !in.Dirty() || !slices.Equal(events, []InputEventKind{InputDirty}) {
		t.Errorf("after an edit of the modified side: dirty %v, events %v", in.Dirty(), events)
	}
	// a rename changes the label
	mod.SetURI(document.FileURI("/w/c.go"))
	if in.Name() != "a.go ↔ c.go" || events[len(events)-1] != InputLabel {
		t.Errorf("after a rename: %q %v", in.Name(), events)
	}
	in.Dispose()
	if got := events[len(events)-1]; got != InputWillDispose {
		t.Errorf("the last event is %v, want InputWillDispose", got)
	}
	n := len(events)
	edit(mod)
	if len(events) != n {
		t.Error("a disposed input still sends events")
	}
}

func TestDiffInputMatches(t *testing.T) {
	a, b, c := fileDoc("/w/a.go", ""), fileDoc("/w/b.go", ""), fileDoc("/w/c.go", "")
	in := NewDiffInput(a, b, TextInputOpts{})
	for _, tc := range []struct {
		name  string
		other EditorInput
		want  bool
	}{
		{"the same comparison", NewDiffInput(a, b, TextInputOpts{}), true},
		{"a comparison of the same files in another document", NewDiffInput(fileDoc("/w/a.go", ""), fileDoc("/w/b.go", ""), TextInputOpts{}), true},
		{"the sides swapped", NewDiffInput(b, a, TextInputOpts{}), false},
		{"another modified side", NewDiffInput(a, c, TextInputOpts{}), false},
		{"a text input of the modified file", NewTextInput(b, TextInputOpts{}), false},
		{"a settings input", NewSettingsInput(SettingsViewState{}), false},
		{"nil", (*DiffInput)(nil), false},
	} {
		if got := in.Matches(tc.other); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
	if _, ok := any(in).(Copier); !ok {
		t.Fatal("a diff input is not a Copier")
	}
	cp := in.CopyInput().(*DiffInput)
	if cp == in || !cp.Matches(in) || cp.Original() != a || cp.Modified() != b {
		t.Errorf("copy %+v", cp)
	}
	if k := in.TabKey(); k == "" || k == NewDiffInput(a, c, TextInputOpts{}).TabKey() || k != cp.TabKey() {
		t.Errorf("tab key %q", k)
	}
}

func TestDiffInputSerialization(t *testing.T) {
	for _, tc := range []struct {
		name               string
		original, modified *document.Document
	}{
		{"two files", fileDoc("/w/a.go", ""), fileDoc("/w/b.go", "")},
		{"a virtual original", document.NewVirtual(document.VirtualURI("/saved/a.go"), document.VirtualContent{Text: "x"}), fileDoc("/w/a.go", "")},
		{"an untitled modified side", fileDoc("/w/a.go", ""), document.New(document.UntitledURI(3), "", document.NewOptions())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewDiffInput(tc.original, tc.modified, TextInputOpts{})
			value, ok := in.Serialize()
			if !ok {
				t.Fatal("not serializable")
			}
			orig, mod, err := ParseDiffInput(value)
			if err != nil {
				t.Fatal(err)
			}
			if orig != tc.original.URI() || mod != tc.modified.URI() {
				t.Errorf("parsed %v and %v", orig, mod)
			}
		})
	}
	// a side that cannot come back
	odd := document.New(document.URI{Scheme: "other", Path: "x"}, "", document.NewOptions())
	if _, ok := NewDiffInput(odd, fileDoc("/w/a.go", ""), TextInputOpts{}).Serialize(); ok {
		t.Error("a side with an unknown scheme was serialized")
	}
}

func TestParseDiffInputFailures(t *testing.T) {
	for _, tc := range []struct {
		name, value, route string
	}{
		{"not JSON", "{nope", "ParseDiffInput.DiffInputInvalid"},
		{"no sides", `{}`, "ParseDiffInput.DiffInputInvalid"},
		{"one side", `{"original":"file:///a"}`, "ParseDiffInput.DiffInputInvalid"},
		{"a bad original", `{"original":"nope","modified":"file:///b"}`, "ParseURI.URIInvalid"},
		{"a bad modified", `{"original":"file:///a","modified":"file://relative"}`, "ParseURI.URIInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseDiffInput(tc.value)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestPlanCloseOfDiffInputs(t *testing.T) {
	// the modified document of a diff editor that a text editor shows too is not asked about twice
	e := newPlanEnv(t, 2)
	e.dirty("new.go")
	orig, mod := e.doc("old.go"), e.doc("new.go")
	diffIn, _ := e.grid.Group(1).Model.OpenEditor(NewDiffInput(orig, mod, TextInputOpts{}), OpenOptions{Pinned: true})
	text := e.open(2, "new.go")
	for _, tc := range []struct {
		name    string
		targets []CloseTarget
		want    []string
	}{
		{"the diff alone, while a text editor shows the document", []CloseTarget{{Group: e.grid.Group(1), Editors: []EditorInput{diffIn}}}, nil},
		{"the text editor alone, while the diff shows the document", []CloseTarget{{Group: e.grid.Group(2), Editors: []EditorInput{text}}}, nil},
		{"both", AllTargets(e.grid), []string{"old.go ↔ new.go"}},
	} {
		if got := e.names(PlanClose(e.grid, tc.targets)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	// a diff whose modified side is dirty asks on its own; a dirty original does not
	e2 := newPlanEnv(t, 1)
	e2.dirty("old.go")
	e2.grid.Group(1).Model.OpenEditor(NewDiffInput(e2.doc("old.go"), e2.doc("new.go"), TextInputOpts{}), OpenOptions{Pinned: true})
	if got := e2.names(PlanClose(e2.grid, AllTargets(e2.grid))); len(got) != 0 {
		t.Errorf("a dirty original asks: %v", got)
	}
	e2.dirty("new.go")
	if got := e2.names(PlanClose(e2.grid, AllTargets(e2.grid))); !slices.Equal(got, []string{"old.go ↔ new.go"}) {
		t.Errorf("a dirty modified side: %v", got)
	}
}

func TestEditedDocument(t *testing.T) {
	doc := fileDoc("/w/a.go", "")
	if EditedDocument(NewTextInput(doc, TextInputOpts{})) != doc {
		t.Error("a text input edits its document")
	}
	if EditedDocument(NewSettingsInput(SettingsViewState{})) != nil {
		t.Error("a settings input edits no document")
	}
}
