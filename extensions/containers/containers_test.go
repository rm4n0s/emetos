package containers

import (
	"os"
	"path/filepath"
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
	if len(m.Contributes.Settings) < 1 || m.Contributes.Settings[0].Key != "containers.host" {
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

// TestTheLanguageServersAreDeclaredWhenTheirProgramsAreOnPath holds that Docker's extension declares the Dockerfile and Compose
// language servers (SPEC §10.20 "Language support") only when they are found, and that nothing else changes.
func TestTheLanguageServersAreDeclaredWhenTheirProgramsAreOnPath(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"docker-langserver", "docker-compose-langserver"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	got := New().Manifest().Contributes.LanguageServers
	if len(got) != 2 || got[0].Languages[0] != "dockerfile" || got[1].Languages[0] != "dockercompose" {
		t.Errorf("servers %+v", got)
	}
	t.Setenv("PATH", t.TempDir())
	if got := New().Manifest().Contributes.LanguageServers; len(got) != 0 {
		t.Errorf("servers without programs: %+v", got)
	}
}
