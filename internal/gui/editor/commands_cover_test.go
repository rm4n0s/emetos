package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// appCommands are the commands of the grid of editor groups that have no "workbench." id: gui/app binds
// them, and its tests check that it does.
var appCommands = map[string]bool{"moveActiveEditor": true, "copyActiveEditor": true, "layoutEditorGroups": true}

func TestEveryEditorCommandHasAHandler(t *testing.T) {
	reg := command.DefaultRegistry()
	have := CommandIDs()
	var missing []string
	for _, meta := range reg.All() {
		if strings.HasPrefix(meta.ID, "workbench.") {
			// the commands of the workbench are the workbench's (gui/workbench binds them, and its tests check that it does)
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
