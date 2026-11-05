package main

import (
	"testing"

	"github.com/rm4n0s/emetos/assets"
	"github.com/rm4n0s/emetos/extensions"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/tutor"
)

// TestTheTutorTeachesOnlyKeysEmetosHas checks every {{key:...}} of the lessons of the Emetos Tutor (SPEC §10.35)
// against the whole program: a command of the core or of a compiled-in extension, with a default key of either. The
// lessons of Search and of the terminal name commands of extensions, which the core's own test cannot see.
func TestTheTutorTeachesOnlyKeysEmetosHas(t *testing.T) {
	ids, err := tutor.Placeholders(assets.Tutor(), assets.TutorRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatal("the lessons name no key")
	}
	core := command.DefaultRegistry()
	commands, bound := map[string]bool{}, map[string]bool{}
	for _, it := range keybinding.DefaultRegistry().Defaults() {
		bound[it.Command] = true
	}
	for _, f := range extensions.All() {
		m := f().Manifest()
		for _, c := range m.Contributes.Commands {
			commands[c.ID] = true
		}
		for _, k := range m.Contributes.Keybindings {
			bound[k.Command] = true
		}
	}
	for _, id := range ids {
		if _, ok := core.Lookup(id); !ok && !commands[id] {
			t.Errorf("the lessons name %q, which is not a command", id)
		}
		if !bound[id] {
			t.Errorf("the lessons name %q, which has no default key", id)
		}
	}
}
