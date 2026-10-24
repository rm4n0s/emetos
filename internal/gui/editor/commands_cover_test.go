package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// appCommands are the commands of the grid of editor groups and of the paths of files that have no
// "workbench." id: gui/app binds them, and its tests check that it does.
var appCommands = map[string]bool{
	"moveActiveEditor": true, "copyActiveEditor": true, "layoutEditorGroups": true,
	"copyFilePath": true, "copyRelativeFilePath": true,
}

func TestEveryEditorCommandHasAHandler(t *testing.T) {
	reg := command.DefaultRegistry()
	have := CommandIDs()
	var missing []string
	for _, meta := range reg.All() {
		if strings.HasPrefix(meta.ID, "workbench.") || strings.HasPrefix(meta.ID, "notifications.") ||
			strings.HasPrefix(meta.ID, "problems.") {
			// the commands of the workbench, of the notifications and of the filter of the Problems panel are the workbench's and the window's (gui/workbench and gui/app bind them, and their tests check that they do)
			continue
		}
		if appCommands[meta.ID] {
			continue
		}
		if !slices.Contains(have, meta.ID) {
			missing = append(missing, meta.ID)
		}
	}
	if len(missing) > 0 {
		t.Errorf("commands without a handler: %v", missing)
	}
	for _, id := range have {
		if strings.HasPrefix(id, "dev.") {
			continue // registered only in debug and emetos_dev builds (SPEC D15)
		}
		if _, ok := reg.Lookup(id); !ok {
			t.Errorf("%s is implemented but not registered", id)
		}
	}
}
