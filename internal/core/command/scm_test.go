package command

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// TestSourceControlCommands checks the commands of the Source Control model: VS Code's ids and titles, the precondition of the
// sort keys of the list, and that the palette lists none of them.
func TestSourceControlCommands(t *testing.T) {
	r := DefaultRegistry()
	menus := DefaultMenus(r)
	tests := []struct {
		id, title string
		onlyList  bool // enabled only while the Changes view is a list
	}{
		{"scm.setActiveProvider", "", false},
		{"workbench.scm.action.setListViewMode", "View as List", false},
		{"workbench.scm.action.setTreeViewMode", "View as Tree", false},
		{"workbench.scm.action.setSortKey.name", "Sort Changes by Name", true},
		{"workbench.scm.action.setSortKey.path", "Sort Changes by Path", true},
		{"workbench.scm.action.setSortKey.status", "Sort Changes by Status", true},
		{"workbench.scm.action.repositories.setSortKey.discoveryTime", "Sort by Discovery Time", false},
		{"workbench.scm.action.repositories.setSortKey.name", "Sort by Name", false},
		{"workbench.scm.action.repositories.setSortKey.path", "Sort by Path", false},
		{"workbench.scm.action.repositories.setSelectionMode.single", "Select Single Repository", false},
		{"workbench.scm.action.repositories.setSelectionMode.multiple", "Select Multiple Repositories", false},
	}
	// a row of the CommandPalette menu whose When does not hold hides its command from the palette
	hides := map[string]bool{}
	for _, it := range menus.All() {
		if it.Menu == CommandPalette && it.When != nil && !it.When.Evaluate(contextkey.MapContext{}) {
			hides[it.Command] = true
		}
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			meta, ok := r.Lookup(tt.id)
			if !ok {
				t.Fatal("not registered")
			}
			if meta.Title != tt.title {
				t.Errorf("title = %q, want %q", meta.Title, tt.title)
			}
			if tt.title != "" && !hides[tt.id] {
				t.Error("the command palette lists it")
			}
			list, tree := contextkey.MapContext{"scmViewMode": "list"}, contextkey.MapContext{"scmViewMode": "tree"}
			switch {
			case tt.onlyList && (meta.Precondition == nil || !meta.Precondition.Evaluate(list) || meta.Precondition.Evaluate(tree)):
				t.Errorf("precondition = %v", meta.Precondition)
			case !tt.onlyList && meta.Precondition != nil:
				t.Errorf("precondition = %v", meta.Precondition)
			}
		})
	}
}
