package editorgroup

import (
	"testing"

	"github.com/rm4n0s/errors"
)

func TestSystemCheckInput(t *testing.T) {
	a, b := NewSystemCheckInput(), NewSystemCheckInput()
	if !a.Matches(b) || !b.Matches(a) {
		t.Errorf("two System Check inputs must be the same page")
	}
	if a.Matches(NewExtensionsInput()) || a.Matches(nil) {
		t.Errorf("a System Check input matched another kind of input")
	}
	if a.TypeID() != SystemCheckInputTypeID || a.Name() != "System Check" || a.Title() != "System Check" || a.Dirty() || a.Capabilities() != 0 {
		t.Errorf("input = %+v", a)
	}
	if _, ok := a.Resource(); ok {
		t.Errorf("the page has a resource")
	}
	v, ok := a.Serialize()
	if !ok || v != "{}" {
		t.Errorf("Serialize = %q, %v", v, ok)
	}
	if _, err := ParseSystemCheckInput(v); err != nil {
		t.Errorf("ParseSystemCheckInput(%q): %v", v, err)
	}
}

func TestParseSystemCheckInput(t *testing.T) {
	tests := []struct {
		name, value string
		ok          bool
	}{
		{"empty object", "{}", true},
		{"an object with a field of a later version", `{"group": "found"}`, true},
		{"spaces around", "  {}\n", true},
		{"null", "null", false},
		{"an array", "[]", false},
		{"not JSON", "system", false},
		{"nothing", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := ParseSystemCheckInput(tt.value)
			if tt.ok {
				if err != nil || in == nil {
					t.Fatalf("got %v, %v", in, err)
				}
				return
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("ParseSystemCheckInput.SystemCheckInputInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
