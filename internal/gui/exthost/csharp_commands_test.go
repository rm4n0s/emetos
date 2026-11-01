package exthost

import (
	"testing"

	"github.com/rm4n0s/emetos/extension"
	csharppack "github.com/rm4n0s/emetos/extensions/csharp"
)

// The commands of the C# pack in the window's registry (SPEC §10.34 "C#"): "C#: Restart Language Server" is the command the host registers for
// every extension with a server, and "C#: Select Solution" is the pack's.
func TestTheCommandsOfTheCSharpPackAreInThePalette(t *testing.T) {
	r := newRig(t, csharppack.New)
	tb := r.newTab("one")
	tb.scope.Start()
	tests := []struct{ id, title string }{
		{extension.RestartServerCommand(csharppack.ID), "Restart Language Server"},
		{csharppack.SelectSolutionCommand, "Select Solution"},
	}
	for _, tt := range tests {
		meta, ok := tb.commands.Lookup(tt.id)
		if !ok || meta.Category != "C#" || meta.Title != tt.title {
			t.Errorf("%s = %+v, %v: want %q in the category C#", tt.id, meta, ok, tt.title)
		}
	}
}
