package editorgroup

import "testing"

func TestEventSharingInput(t *testing.T) {
	a, b := NewEventSharingInput(), NewEventSharingInput()
	if !a.Matches(b) || !b.Matches(a) {
		t.Errorf("two Event Sharing inputs must be the same page")
	}
	if a.Matches(NewExtensionsInput()) || NewExtensionsInput().Matches(a) || a.Matches(nil) {
		t.Errorf("an Event Sharing input matched another kind of input")
	}
	if a.TypeID() != EventSharingInputTypeID || a.Name() != "Event Sharing" || a.Title() != "Extensions: Event Sharing" || a.Description() != "" || a.Dirty() || a.Capabilities() != 0 {
		t.Errorf("input = %+v", a)
	}
	if _, ok := a.Resource(); ok {
		t.Errorf("the page has a resource")
	}
	if v, ok := a.Serialize(); !ok || v != "{}" {
		t.Errorf("Serialize = %q, %v", v, ok)
	}
}
