package settings

import (
	"reflect"
	"testing"
)

// TestSourceControlSettingsAreRegistered checks the scm.* settings against VS Code's scm.contribution.ts: key, type, default,
// the values of an enum, the range of a number, the scope (all resource settings) and the group.
func TestSourceControlSettingsAreRegistered(t *testing.T) {
	r := DefaultRegistry()
	tests := []struct {
		key      string
		types    []Type
		def      any
		enum     []any
		min, max *float64
	}{
		{"scm.diffDecorations", []Type{String}, "all", []any{"all", "gutter", "overview", "minimap", "none"}, nil, nil},
		{"scm.diffDecorationsGutterWidth", []Type{Number}, 3.0, []any{1.0, 2.0, 3.0, 4.0, 5.0}, nil, nil},
		{"scm.diffDecorationsGutterVisibility", []Type{String}, "always", []any{"always", "hover"}, nil, nil},
		{"scm.diffDecorationsGutterAction", []Type{String}, "diff", []any{"diff", "none"}, nil, nil},
		{"scm.diffDecorationsGutterPattern", []Type{Object}, map[string]any{"added": false, "modified": true}, nil, nil, nil},
		{"scm.diffDecorationsIgnoreTrimWhitespace", []Type{String}, "false", []any{"true", "false", "inherit"}, nil, nil},
		{"scm.alwaysShowActions", []Type{Boolean}, false, nil, nil, nil},
		{"scm.countBadge", []Type{String}, "all", []any{"all", "focused", "off"}, nil, nil},
		{"scm.providerCountBadge", []Type{String}, "hidden", []any{"hidden", "auto", "visible"}, nil, nil},
		{"scm.defaultViewMode", []Type{String}, "list", []any{"tree", "list"}, nil, nil},
		{"scm.defaultViewSortKey", []Type{String}, "path", []any{"name", "path", "status"}, nil, nil},
		{"scm.autoReveal", []Type{Boolean}, true, nil, nil, nil},
		{"scm.inputFontFamily", []Type{String}, "default", nil, nil, nil},
		{"scm.inputFontSize", []Type{Number}, 13.0, nil, nil, nil},
		{"scm.inputMaxLineCount", []Type{Number}, 10.0, nil, Float(1), Float(50)},
		{"scm.inputMinLineCount", []Type{Number}, 1.0, nil, Float(1), Float(50)},
		{"scm.alwaysShowRepositories", []Type{Boolean}, false, nil, nil, nil},
		{"scm.repositories.sortOrder", []Type{String}, "discovery time", []any{"discovery time", "name", "path"}, nil, nil},
		{"scm.repositories.visible", []Type{Number}, 10.0, nil, nil, nil},
		{"scm.repositories.selectionMode", []Type{String}, "multiple", []any{"multiple", "single"}, nil, nil},
		{"scm.showActionButton", []Type{Boolean}, true, nil, nil, nil},
		{"scm.showInputActionButton", []Type{Boolean}, true, nil, nil, nil},
		{"scm.compactFolders", []Type{Boolean}, true, nil, nil, nil},
		{"scm.graph.pageOnScroll", []Type{Boolean}, true, nil, nil, nil},
		{"scm.graph.pageSize", []Type{Number}, 50.0, nil, Float(1), Float(1000)},
		{"scm.graph.badges", []Type{String}, "filter", []any{"all", "filter"}, nil, nil},
		{"scm.graph.showIncomingChanges", []Type{Boolean}, true, nil, nil, nil},
		{"scm.graph.showOutgoingChanges", []Type{Boolean}, true, nil, nil, nil},
	}
	for _, tt := range tests {
		s, ok := r.Lookup(tt.key)
		if !ok {
			t.Errorf("%s is not registered", tt.key)
			continue
		}
		if !reflect.DeepEqual(s.Types, tt.types) || !reflect.DeepEqual(s.Default, tt.def) || (len(s.Enum) > 0 || len(tt.enum) > 0) && !reflect.DeepEqual(s.Enum, tt.enum) {
			t.Errorf("%s = types %v default %v enum %v", tt.key, s.Types, s.Default, s.Enum)
		}
		if !reflect.DeepEqual(s.Minimum, tt.min) || !reflect.DeepEqual(s.Maximum, tt.max) {
			t.Errorf("%s range = %v %v", tt.key, s.Minimum, s.Maximum)
		}
		if s.Scope != ScopeResource || s.Category != "Source Control" || s.Description == "" || (len(s.EnumDescriptions) > 0 && len(s.EnumDescriptions) != len(tt.enum)) {
			t.Errorf("%s: scope %v category %q description %q enum descriptions %d", tt.key, s.Scope, s.Category, s.Description, len(s.EnumDescriptions))
		}
	}
	// the ones the spec leaves out are not registered
	for _, key := range []string{"scm.workingSets.enabled", "scm.workingSets.default", "scm.repositories.explorer"} {
		if _, ok := r.Lookup(key); ok {
			t.Errorf("%s is registered", key)
		}
	}
	// a value outside the enum or the range is refused
	for key, bad := range map[string]any{"scm.countBadge": "some", "scm.inputMaxLineCount": 51.0, "scm.diffDecorationsGutterWidth": 6.0, "scm.graph.pageSize": 0.0} {
		s, _ := r.Lookup(key)
		if s.validate(bad) == "" {
			t.Errorf("%s accepts %v", key, bad)
		}
	}
}
