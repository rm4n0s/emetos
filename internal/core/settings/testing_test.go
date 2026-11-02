package settings

import (
	"slices"
	"testing"
)

// The settings of the Test Explorer (step 26.2): the name, the values and the default of VS Code's testing.countBadge.
func TestTestingCountBadgeSetting(t *testing.T) {
	s, ok := DefaultRegistry().Lookup("testing.countBadge")
	if !ok {
		t.Fatal("testing.countBadge is not registered")
	}
	if s.Default != "failed" || s.Category != "Testing" || s.Scope != ScopeWindow {
		t.Errorf("default %v, category %q, scope %v", s.Default, s.Category, s.Scope)
	}
	var values []string
	for _, v := range s.Enum {
		values = append(values, v.(string))
	}
	if !slices.Equal(values, []string{"failed", "off", "passed", "skipped"}) || len(s.EnumDescriptions) != 4 {
		t.Errorf("values %v with %d descriptions", values, len(s.EnumDescriptions))
	}
}
