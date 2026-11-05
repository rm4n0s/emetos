package tutor

import (
	"io/fs"
	"testing"

	"github.com/rm4n0s/emetos/assets"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

// TestTheLessonsNameOnlyRealCommands checks that every {{key:...}} of the embedded lessons is a command of Emetos's
// own registry, so a lesson never shows a placeholder or a missing command.
func TestTheLessonsNameOnlyRealCommands(t *testing.T) {
	ids, err := Placeholders(assets.Tutor(), assets.TutorRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatal("the lessons name no key")
	}
	reg := command.DefaultRegistry()
	bound := map[string]bool{}
	for _, it := range keybinding.DefaultRegistry().Defaults() {
		bound[it.Command] = true
	}
	for _, id := range ids {
		if _, ok := reg.Lookup(id); !ok {
			t.Errorf("the lessons name %q, which is not a command", id)
		}
		// a lesson teaches a key: with the default keybindings every command it names has one
		if !bound[id] {
			t.Errorf("the lessons name %q, which has no default key", id)
		}
	}
}

func TestTheLessonsHaveTheMainFile(t *testing.T) {
	if _, err := fs.Stat(assets.Tutor(), assets.TutorRoot+"/"+MainFile); err != nil {
		t.Fatal(err)
	}
}
