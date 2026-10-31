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

// TestQuickDiffColors checks the colors of the marks of the quick diff against the registrations of VS Code's quickDiff.ts, for the four
// kinds of theme. The secondary colors are VS Code's darken and lighten of the primary ones.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/common/quickDiff.ts
func TestQuickDiffColors(t *testing.T) {
	r := DefaultColorRegistry()
	// id, dark, light, high contrast, high contrast light
	table := []struct{ id, dark, light, hc, hcLight string }{
		{"editorGutter.modifiedBackground", "#1b81a8", "#2090d3", "#1b81a8", "#2090d3"},
		{"editorGutter.addedBackground", "#487e02", "#48985d", "#487e02", "#48985d"},
		{"editorGutter.deletedBackground", "#f14c4c", "#e51400", "#f48771", "#b5200d"},
		{"minimapGutter.modifiedBackground", "#1b81a8", "#2090d3", "#1b81a8", "#2090d3"},
		{"minimapGutter.addedBackground", "#487e02", "#48985d", "#487e02", "#48985d"},
		{"minimapGutter.deletedBackground", "#f14c4c", "#e51400", "#f48771", "#b5200d"},
		{"editorOverviewRuler.modifiedForeground", "rgba(27, 129, 168, 0.6)", "rgba(32, 144, 211, 0.6)", "rgba(27, 129, 168, 0.6)", "rgba(32, 144, 211, 0.6)"},
		{"editorOverviewRuler.addedForeground", "rgba(72, 126, 2, 0.6)", "rgba(72, 152, 93, 0.6)", "rgba(72, 126, 2, 0.6)", "rgba(72, 152, 93, 0.6)"},
		{"editorOverviewRuler.deletedForeground", "rgba(241, 76, 76, 0.6)", "rgba(229, 20, 0, 0.6)", "rgba(244, 135, 113, 0.6)", "rgba(181, 32, 13, 0.6)"},
		{"editorGutter.modifiedSecondaryBackground", "#0d4054", "#aad8f2", "#1b81a8", "#2090d3"},
		{"editorGutter.addedSecondaryBackground", "#243f01", "#a7d5b3", "#487e02", "#48985d"},
		{"editorGutter.deletedSecondaryBackground", "#b00e0e", "#ff3d2b", "#f48771", "#b5200d"},
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
}
