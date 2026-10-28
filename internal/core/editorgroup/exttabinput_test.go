package editorgroup

import (
	"testing"

	"github.com/rm4n0s/errors"
)

func TestExtensionTabInput(t *testing.T) {
	in := NewExtensionTabInput("emetos.remotework", "emetos.remotework.page", "t1", "Groceries", "note")
	var events []InputEventKind
	in.Subscribe(func(e InputEvent) { events = append(events, e.Kind) })
	if in.TypeID() != ExtensionTabInputTypeID || in.Name() != "Groceries" || in.Title() != "Groceries" || in.Dirty() || in.Busy() {
		t.Fatalf("input = %+v", in)
	}
	if _, ok := in.Resource(); ok {
		t.Errorf("the tab has a resource")
	}
	if !in.Capabilities().Has(CapCanSplit) {
		t.Errorf("the tab cannot be split")
	}
	in.SetTitle("Shopping")
	in.SetHint("A note")
	in.SetDirty(true)
	in.SetDirty(true)
	in.SetBusy(true)
	if in.Name() != "Shopping" || in.Title() != "A note" || !in.Dirty() || !in.Busy() {
		t.Errorf("after the setters: %+v", in)
	}
	want := []InputEventKind{InputLabel, InputLabel, InputDirty, InputLabel}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("events = %v, want %v", events, want)
		}
	}
}

func TestExtensionTabInputMatchesAndRestores(t *testing.T) {
	a := NewExtensionTabInput("e", "e.k", "1", "A", "")
	b := NewExtensionTabInput("e", "e.k", "1", "B", "")
	c := NewExtensionTabInput("e", "e.k", "2", "A", "")
	if !a.Matches(b) || a.Matches(c) || a.Matches(NewExtensionsInput()) || a.Matches(nil) {
		t.Errorf("Matches compares the instance id")
	}
	if a.TabKey() == c.TabKey() {
		t.Errorf("two instances share a tab key")
	}
	value, ok := a.Serialize()
	if !ok {
		t.Fatal("Serialize failed")
	}
	back, err := ParseExtensionTabInput(value)
	if err != nil {
		t.Fatal(err)
	}
	if back.Extension() != "e" || back.Kind() != "e.k" || back.ID() != "1" || back.Name() != "A" {
		t.Errorf("restored = %+v", back)
	}
}

func TestParseExtensionTabInputErrors(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"not JSON", "nope"},
		{"no extension", `{"kind":"k","id":"1"}`},
		{"no id", `{"ext":"e","kind":"k"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseExtensionTabInput(tc.value)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("ParseExtensionTabInput.ExtensionTabInputInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestClosingADirtyExtensionTabAsksNothing(t *testing.T) {
	g := NewGrid(GridOptions{})
	in := NewExtensionTabInput("e", "e.k", "1", "A", "")
	g.Active().Model.OpenEditor(in, OpenOptions{Pinned: true, Active: true})
	in.SetDirty(true)
	targets := []CloseTarget{{Group: g.Active(), Editors: []EditorInput{in}}}
	if got := PlanClose(g, targets); len(got) != 0 {
		t.Errorf("a dirty extension tab is asked about: %v", got)
	}
}

func TestExtensionTabInputCopy(t *testing.T) {
	a := NewExtensionTabInput("e", "e.k", "1", "A", "")
	made := NewExtensionTabInput("e", "e.k", "2", "A", "")
	a.SetCopier(func() *ExtensionTabInput { return made })
	if got := a.CopyInput(); got != EditorInput(made) {
		t.Errorf("CopyInput = %v, want the one the host made", got)
	}
	var _ Copier = a
}
