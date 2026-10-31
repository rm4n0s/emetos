package settings

import (
	"reflect"
	"testing"
)

// TestTimelineSettingsAreRegistered checks the timeline.* settings against VS Code's timeline.contribution.ts: key, types, default,
// scope and group.
func TestTimelineSettingsAreRegistered(t *testing.T) {
	r := DefaultRegistry()
	tests := []struct {
		key   string
		types []Type
		def   any
	}{
		{"timeline.pageSize", []Type{Number, Null}, 50.0},
		{"timeline.pageOnScroll", []Type{Boolean}, true},
	}
	for _, tt := range tests {
		s, ok := r.Lookup(tt.key)
		if !ok {
			t.Errorf("%s is not registered", tt.key)
			continue
		}
		if !reflect.DeepEqual(s.Types, tt.types) || !reflect.DeepEqual(s.Default, tt.def) {
			t.Errorf("%s = types %v default %v", tt.key, s.Types, s.Default)
		}
		if s.Scope != ScopeWindow || s.Category != "Timeline" || s.Description == "" {
			t.Errorf("%s: scope %v category %q description %q", tt.key, s.Scope, s.Category, s.Description)
		}
	}
	// the page size may be null (the view chooses), and refuses a text
	s, _ := r.Lookup("timeline.pageSize")
	if msg := s.validate(nil); msg != "" {
		t.Errorf("a null page size: %s", msg)
	}
	if msg := s.validate("many"); msg == "" {
		t.Error("a text is a page size")
	}
}
