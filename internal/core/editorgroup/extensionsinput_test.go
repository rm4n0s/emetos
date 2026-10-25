package editorgroup

import "testing"

func TestExtensionsInput(t *testing.T) {
	a, b := NewExtensionsInput(), NewExtensionsInput()
	if !a.Matches(b) || !b.Matches(a) {
		t.Errorf("two Extensions inputs must be the same page")
	}
	if a.Matches(NewSettingsInput(SettingsViewState{})) || a.Matches(nil) {
		t.Errorf("an Extensions input matched another kind of input")
	}
	if a.TypeID() != ExtensionsInputTypeID || a.Name() != "Extensions" || a.Title() != "Extensions" || a.Dirty() || a.Capabilities() != 0 {
		t.Errorf("input = %+v", a)
	}
	if _, ok := a.Resource(); ok {
		t.Errorf("the page has a resource")
	}
	if v, ok := a.Serialize(); !ok || v != "{}" {
		t.Errorf("Serialize = %q, %v", v, ok)
	}
}
