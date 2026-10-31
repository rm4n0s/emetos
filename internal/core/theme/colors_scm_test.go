package theme

import "testing"

// TestGitDecorationColors checks the ten gitDecoration colors against the values of the package.json of VS Code's git extension, for
// the four kinds of theme (dark, light, high contrast and high contrast light).
//
// Ported from VS Code: extensions/git/package.json (contributes.colors)
func TestGitDecorationColors(t *testing.T) {
	r := DefaultColorRegistry()
	// id, dark, light, high contrast, high contrast light
	table := []struct{ id, dark, light, hc, hcLight string }{
		{"gitDecoration.addedResourceForeground", "#81b88b", "#587c0c", "#a1e3ad", "#374e06"},
		{"gitDecoration.modifiedResourceForeground", "#e2c08d", "#895503", "#e2c08d", "#895503"},
		{"gitDecoration.deletedResourceForeground", "#c74e39", "#ad0707", "#c74e39", "#ad0707"},
		{"gitDecoration.renamedResourceForeground", "#73c991", "#007100", "#73c991", "#007100"},
		{"gitDecoration.untrackedResourceForeground", "#73c991", "#007100", "#73c991", "#007100"},
		{"gitDecoration.ignoredResourceForeground", "#8c8c8c", "#8e8e90", "#a7a8a9", "#8e8e90"},
		{"gitDecoration.stageModifiedResourceForeground", "#e2c08d", "#895503", "#e2c08d", "#895503"},
		{"gitDecoration.stageDeletedResourceForeground", "#c74e39", "#ad0707", "#c74e39", "#ad0707"},
		{"gitDecoration.conflictingResourceForeground", "#e4676b", "#ad0707", "#c74e39", "#ad0707"},
		{"gitDecoration.submoduleResourceForeground", "#8db9e2", "#1258a7", "#8db9e2", "#1258a7"},
	}
	kinds := []struct {
		name string
		typ  Type
	}{{"dark", TypeDark}, {"light", TypeLight}, {"high contrast", TypeHCDark}, {"high contrast light", TypeHCLight}}
	for _, row := range table {
		want := map[Type]string{TypeDark: row.dark, TypeLight: row.light, TypeHCDark: row.hc, TypeHCLight: row.hcLight}
		for _, k := range kinds {
			t.Run(row.id+" "+k.name, func(t *testing.T) {
				c, ok := colorTheme(k.typ, nil, r).Color(row.id)
				if !ok {
					t.Fatalf("%s is not registered", row.id)
				}
				if got, wantColor := c.String(), want[k.typ]; got != wantColor {
					t.Errorf("%s = %s, want %s", row.id, got, wantColor)
				}
			})
		}
	}
	if len(table) != 10 {
		t.Errorf("%d colors in the table, want 10", len(table))
	}
}
