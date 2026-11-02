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

// The settings of the editor, the peek and the runs (step 26.3): the names, the values and the defaults are those of VS Code's
// configuration.ts at the pinned version; testing.openTesting is the old name of its testing.automaticallyOpenTestResults (decision 1581).
func TestTestingEditorSettings(t *testing.T) {
	tests := []struct {
		key  string
		def  any
		enum []string
	}{
		{"testing.automaticallyOpenPeekView", "never", []string{"failureAnywhere", "failureInVisibleDocument", "never"}},
		{"testing.showAllMessages", false, nil},
		{"testing.followRunningTest", false, nil},
		{"testing.defaultGutterClickAction", "run", []string{"run", "debug", "runWithCoverage", "contextMenu"}},
		{"testing.gutterEnabled", true, nil},
		{"testing.saveBeforeTest", true, nil},
		{"testing.openTesting", "openOnTestStart", []string{"neverOpen", "openOnTestStart", "openOnTestFailure", "openExplorerOnTestStart"}},
		{"testing.alwaysRevealTestOnStateChange", false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			s, ok := DefaultRegistry().Lookup(tc.key)
			if !ok {
				t.Fatalf("%s is not registered", tc.key)
			}
			if s.Default != tc.def || s.Category != "Testing" || s.Scope != ScopeWindow || s.Description == "" {
				t.Errorf("default %v, category %q, scope %v, description %q", s.Default, s.Category, s.Scope, s.Description)
			}
			var values []string
			for _, v := range s.Enum {
				values = append(values, v.(string))
			}
			if !slices.Equal(values, tc.enum) || len(s.EnumDescriptions) != len(tc.enum) {
				t.Errorf("values %v with %d descriptions, want %v", values, len(s.EnumDescriptions), tc.enum)
			}
		})
	}
}
