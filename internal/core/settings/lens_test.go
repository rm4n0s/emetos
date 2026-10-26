package settings

import "testing"

func TestLensSettingsAreRegistered(t *testing.T) {
	r := DefaultRegistry()
	tests := []struct {
		key   string
		types []Type
		def   any
		enum  int
	}{
		{"editor.codeLens", []Type{Boolean}, true, 0},
		{"editor.codeLensFontFamily", []Type{String}, "", 0},
		{"editor.codeLensFontSize", []Type{Number}, float64(0), 0},
		{"editor.inlayHints.enabled", []Type{String}, "on", 4},
		{"editor.inlayHints.fontSize", []Type{Number}, float64(0), 0},
		{"editor.inlayHints.fontFamily", []Type{String}, "", 0},
		{"editor.inlayHints.padding", []Type{Boolean}, false, 0},
		{"editor.inlayHints.maximumLength", []Type{Number}, float64(43), 0},
		{"editor.semanticHighlighting.enabled", []Type{Boolean, String}, "configuredByTheme", 3},
	}
	for _, tt := range tests {
		s, ok := r.Lookup(tt.key)
		if !ok || s.Default != tt.def || len(s.Enum) != tt.enum || len(s.Types) != len(tt.types) || s.Description == "" || s.Scope != ScopeLanguageOverridable {
			t.Errorf("%s: %+v, %v", tt.key, s, ok)
		}
	}
	s, ok := r.Lookup("editor.semanticTokenColorCustomizations")
	if m, isMap := s.Default.(map[string]any); !ok || !isMap || len(m) != 0 || len(s.Types) != 1 || s.Types[0] != Object {
		t.Errorf("editor.semanticTokenColorCustomizations: %+v, %v", s, ok)
	}
}
