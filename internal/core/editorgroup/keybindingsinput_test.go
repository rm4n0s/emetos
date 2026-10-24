package editorgroup

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

func TestKeybindingsInput(t *testing.T) {
	in := NewKeybindingsInput(KeybindingsViewState{})
	if in.TypeID() != "emetos.keybindings" || in.Name() != "Keyboard Shortcuts" || in.Title() != "Keyboard Shortcuts" || in.Description() != "" {
		t.Errorf("labels %q %q %q", in.TypeID(), in.Name(), in.Title())
	}
	if _, ok := in.Resource(); ok {
		t.Error("the keybindings editor shows no resource")
	}
	if in.Dirty() || in.Capabilities() != 0 {
		t.Error("dirty or capable")
	}
	if !in.Matches(NewKeybindingsInput(KeybindingsViewState{Query: "copy"})) {
		t.Error("two keybindings inputs are the same editor")
	}
	if in.Matches(NewSettingsInput(SettingsViewState{})) || in.Matches(NewTextInput(document.New(document.FileURI("/a"), "", document.NewOptions()), TextInputOpts{})) {
		t.Error("a keybindings input matches another kind of input")
	}
	var none *KeybindingsInput
	if in.Matches(none) {
		t.Error("nil matches")
	}
	if _, ok := any(in).(Copier); ok {
		t.Error("a keybindings input is shown in one group only")
	}
}

func TestKeybindingsInputSerialization(t *testing.T) {
	for _, c := range []struct {
		name  string
		state KeybindingsViewState
	}{
		{"default", KeybindingsViewState{}},
		{"search", KeybindingsViewState{Query: "copy line"}},
		{"recording", KeybindingsViewState{Query: `"ctrl+k"`}},
		{"row", KeybindingsViewState{Query: "copy", Command: "editor.action.copyLinesDownAction", Key: "ctrl+alt+d"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			value, ok := NewKeybindingsInput(c.state).Serialize()
			if !ok {
				t.Fatal("not serializable")
			}
			back, err := ParseKeybindingsInput(value)
			if err != nil {
				t.Fatal(err)
			}
			if got := back.ViewState(); !reflect.DeepEqual(got, c.state) {
				t.Errorf("got %+v, want %+v", got, c.state)
			}
		})
	}
	in := NewKeybindingsInput(KeybindingsViewState{})
	in.SetViewState(KeybindingsViewState{Query: "x"})
	if in.ViewState().Query != "x" {
		t.Error("SetViewState")
	}
	if back, err := ParseKeybindingsInput(""); err != nil || back.ViewState() != (KeybindingsViewState{}) {
		t.Errorf("an empty value: %v %+v", err, back)
	}
	_, err := ParseKeybindingsInput("{nope")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ParseKeybindingsInput.KeybindingsInputInvalid") {
		t.Fatalf("got %v", err)
	}
}

func TestKeybindingsInputInAModel(t *testing.T) {
	m := NewModel(1, Options{})
	first := NewKeybindingsInput(KeybindingsViewState{})
	got, isNew := m.OpenEditor(first, OpenOptions{Pinned: true, Active: true})
	if !isNew || got != first {
		t.Fatal("not opened")
	}
	second := NewKeybindingsInput(KeybindingsViewState{Query: "x"})
	got, isNew = m.OpenEditor(second, OpenOptions{Pinned: true, Active: true})
	if isNew || got != first || m.Count() != 1 {
		t.Errorf("a second input opened: new %v count %d", isNew, m.Count())
	}
	ser := m.Serialize()
	if len(ser.Editors) != 1 || ser.Editors[0].TypeID != KeybindingsInputTypeID {
		t.Fatalf("serialized %+v", ser)
	}
	back := RestoreModel(ser, Options{}, func(typeID, value string) (EditorInput, bool) {
		if typeID != KeybindingsInputTypeID {
			return nil, false
		}
		in, err := ParseKeybindingsInput(value)
		return in, err == nil
	})
	if back.Count() != 1 || back.ActiveEditor().TypeID() != KeybindingsInputTypeID {
		t.Error("not restored")
	}
	labels := TabLabels(back.Editors(Sequential, false))
	if labels[0].Name != "Keyboard Shortcuts" || labels[0].Description != "" {
		t.Errorf("labels %+v", labels)
	}
}
