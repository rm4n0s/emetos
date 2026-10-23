package editor

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
)

func TestEveryEditorCommandHasAHandler(t *testing.T) {
	reg := command.DefaultRegistry()
	have := CommandIDs()
	var missing []string
	for _, meta := range reg.All() {
		if !slices.Contains(have, meta.ID) {
			missing = append(missing, meta.ID)
		}
	}
	if len(missing) > 0 {
		t.Errorf("commands without a handler: %v", missing)
	}
	for _, id := range have {
		if _, ok := reg.Lookup(id); !ok {
			t.Errorf("%s is implemented but not registered", id)
		}
	}
}
