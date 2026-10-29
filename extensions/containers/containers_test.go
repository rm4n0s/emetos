package containers

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// TestTheManifestIsDocker holds the extension to what SPEC §10.20 says Docker's is: the id, the commands under containers.*, the
// setting containers.host, the Contexts section and the note about the socket.
func TestTheManifestIsDocker(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "emetos.containers" || m.Name != "Containers" {
		t.Errorf("%s %s", m.ID, m.Name)
	}
	for _, c := range m.Contributes.Commands {
		if !strings.HasPrefix(c.ID, "containers.") {
			t.Errorf("the command %s", c.ID)
		}
	}
	if len(m.Contributes.Settings) != 1 || m.Contributes.Settings[0].Key != "containers.host" {
		t.Errorf("settings %+v", m.Contributes.Settings)
	}
	hasContexts := false
	for _, v := range m.Contributes.Views {
		hasContexts = hasContexts || strings.HasSuffix(v.ID, ".contexts")
	}
	if !hasContexts || !strings.Contains(Config().FirstRunNote, "power over this computer") {
		t.Errorf("contexts %v, note %q", hasContexts, Config().FirstRunNote)
	}
	if New() == New() {
		t.Error("the factory made one extension twice")
	}
}
