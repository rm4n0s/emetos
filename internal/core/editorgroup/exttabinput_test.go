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

func TestClosingADirtyExtensionTabWithASaverAsks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		saver bool
		dirty bool
		asks  bool
	}{
		{"a dirty tab with a saver", true, true, true},
		{"a clean tab with a saver", true, false, false},
		{"a dirty tab without a saver", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid(GridOptions{})
			in := NewExtensionTabInput("e", "e.k", "1", "A", "")
			if tc.saver {
				in.SetSaver(func(done func(bool)) { done(true) })
			}
			g.Active().Model.OpenEditor(in, OpenOptions{Pinned: true, Active: true})
			in.SetDirty(tc.dirty)
			got := PlanClose(g, []CloseTarget{{Group: g.Active(), Editors: []EditorInput{in}}})
			if asks := len(got) == 1 && got[0].Input == EditorInput(in); asks != tc.asks {
				t.Errorf("PlanClose = %v, want asks %v", got, tc.asks)
			}
		})
	}
}

func TestExtensionTabInputSave(t *testing.T) {
	in := NewExtensionTabInput("e", "e.k", "1", "A", "")
	saved := true
	in.Save(func(ok bool) { saved = ok })
	if saved || in.CanSave() {
		t.Errorf("a tab without a saver saved: %v", saved)
	}
	in.SetSaver(func(done func(bool)) { done(true) })
	in.Save(func(ok bool) { saved = ok })
	if !saved || !in.CanSave() {
		t.Errorf("a tab with a saver did not save")
	}
}

// TestAMirrorOfAnExtensionTabFollowsItUntilItIsDisposed: the mirror of a tab on another screen is the same tab by its id, shows
// what the extension sets on the tab, tells its own listeners, and is neither split nor saved.
func TestAMirrorOfAnExtensionTabFollowsItUntilItIsDisposed(t *testing.T) {
	in := NewExtensionTabInput("emetos.terminal", "emetos.terminal.editor", "t1", "Terminal", "terminal")
	in.SetHint("bash")
	m := in.Mirror()
	if !m.IsMirror() || in.IsMirror() || !m.Matches(in) || !in.Matches(m) || m.TabKey() != in.TabKey() {
		t.Fatalf("the mirror is not the tab: mirror %v, matches %v, key %q", m.IsMirror(), m.Matches(in), m.TabKey())
	}
	if m.Name() != "Terminal" || m.Title() != "bash" || m.Icon() != "terminal" {
		t.Errorf("the mirror starts as %q, %q, %q", m.Name(), m.Title(), m.Icon())
	}
	if _, ok := m.Serialize(); ok || m.Capabilities()&CapCanSplit != 0 {
		t.Error("a mirror is saved or can be split")
	}
	var events []InputEventKind
	m.Subscribe(func(e InputEvent) { events = append(events, e.Kind) })
	tests := []struct {
		name string
		set  func()
		got  func(*ExtensionTabInput) any
		want any
		kind InputEventKind
	}{
		{"the title", func() { in.SetTitle("zsh") }, func(x *ExtensionTabInput) any { return x.Name() }, "zsh", InputLabel},
		{"the hint", func() { in.SetHint("zsh in /tmp") }, func(x *ExtensionTabInput) any { return x.Title() }, "zsh in /tmp", InputLabel},
		{"the icon", func() { in.SetIcon("debug", "terminal.ansiRed") }, func(x *ExtensionTabInput) any { return x.Icon() + " " + x.IconColor() }, "debug terminal.ansiRed", InputLabel},
		{"the busy mark", func() { in.SetBusy(true) }, func(x *ExtensionTabInput) any { return x.Busy() }, true, InputLabel},
		{"the dirty mark", func() { in.SetDirty(true) }, func(x *ExtensionTabInput) any { return x.Dirty() }, true, InputDirty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events = nil
			tt.set()
			if got := tt.got(m); got != tt.want || tt.got(in) != tt.want {
				t.Errorf("the mirror has %v and the tab %v, want %v", got, tt.got(in), tt.want)
			}
			if len(events) != 1 || events[0] != tt.kind {
				t.Errorf("the listeners of the mirror got %v", events)
			}
		})
	}
	// a mirror of a mirror is a mirror of the tab
	second := m.Mirror()
	in.SetTitle("fish")
	if second.Name() != "fish" {
		t.Errorf("the second mirror has %q", second.Name())
	}
	// a disposed mirror follows no more, and a disposed tab lets go of its mirrors
	m.Dispose()
	in.SetTitle("sh")
	if m.Name() != "fish" || second.Name() != "sh" {
		t.Errorf("after the dispose: %q and %q", m.Name(), second.Name())
	}
	in.Dispose()
	if second.IsMirror() {
		t.Error("the mirror of a disposed tab still follows it")
	}
}
