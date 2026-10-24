package editorgroup

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/errors"
)

func TestSettingsInput(t *testing.T) {
	in := NewSettingsInput(SettingsViewState{})
	if in.TypeID() != "emetos.settings" || in.Name() != "Settings" || in.Title() != "Settings" || in.Description() != "" {
		t.Errorf("labels %q %q %q", in.TypeID(), in.Name(), in.Title())
	}
	if _, ok := in.Resource(); ok {
		t.Error("the settings editor shows no resource")
	}
	if in.Dirty() || in.Capabilities() != 0 {
		t.Error("dirty or capable")
	}
	if !in.Matches(NewSettingsInput(SettingsViewState{Target: SettingsWorkspace})) {
		t.Error("two settings inputs are the same editor")
	}
	if in.Matches(NewTextInput(document.New(document.FileURI("/a"), "", document.NewOptions()), TextInputOpts{})) {
		t.Error("a settings input matches a text input")
	}
	var none *SettingsInput
	if in.Matches(none) {
		t.Error("nil matches")
	}
	if _, ok := any(in).(Copier); ok {
		t.Error("a settings input is shown in one group only")
	}
}

func TestSettingsInputSerialization(t *testing.T) {
	for _, c := range []struct {
		name  string
		state SettingsViewState
	}{
		{"default", SettingsViewState{}},
		{"workspace", SettingsViewState{Target: SettingsWorkspace, Category: "editor/font", Scroll: 123.5}},
		{"query", SettingsViewState{Target: SettingsUser, Query: "tab size"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := NewSettingsInput(c.state)
			value, ok := in.Serialize()
			if !ok {
				t.Fatal("not serializable")
			}
			back, err := ParseSettingsInput(value)
			if err != nil {
				t.Fatal(err)
			}
			want := c.state
			if want.Target == "" {
				want.Target = SettingsUser
			}
			if got := back.ViewState(); !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
	in := NewSettingsInput(SettingsViewState{})
	in.SetViewState(SettingsViewState{Category: "window"})
	if in.ViewState().Category != "window" {
		t.Error("SetViewState")
	}
	// an empty value and an unknown tab give the User tab
	for _, v := range []string{"", `{"target":"bogus"}`} {
		back, err := ParseSettingsInput(v)
		if err != nil || back.ViewState().Target != SettingsUser {
			t.Errorf("%q: %v %+v", v, err, back)
		}
	}
	_, err := ParseSettingsInput("{nope")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ParseSettingsInput.SettingsInputInvalid") {
		t.Fatalf("got %v", err)
	}
}

func TestSettingsInputInAModel(t *testing.T) {
	m := NewModel(1, Options{})
	first := NewSettingsInput(SettingsViewState{})
	got, isNew := m.OpenEditor(first, OpenOptions{Pinned: true, Active: true})
	if !isNew || got != first {
		t.Fatal("not opened")
	}
	second := NewSettingsInput(SettingsViewState{Target: SettingsWorkspace})
	got, isNew = m.OpenEditor(second, OpenOptions{Pinned: true, Active: true})
	if isNew || got != first || m.Count() != 1 {
		t.Errorf("a second input opened: new %v count %d", isNew, m.Count())
	}
	ser := m.Serialize()
	if len(ser.Editors) != 1 || ser.Editors[0].TypeID != SettingsInputTypeID {
		t.Fatalf("serialized %+v", ser)
	}
	back := RestoreModel(ser, Options{}, func(typeID, value string) (EditorInput, bool) {
		if typeID != SettingsInputTypeID {
			return nil, false
		}
		in, err := ParseSettingsInput(value)
		return in, err == nil
	})
	if back.Count() != 1 || back.ActiveEditor().TypeID() != SettingsInputTypeID {
		t.Error("not restored")
	}
	labels := TabLabels(back.Editors(Sequential, false))
	if labels[0].Name != "Settings" || labels[0].Description != "" {
		t.Errorf("labels %+v", labels)
	}
}
