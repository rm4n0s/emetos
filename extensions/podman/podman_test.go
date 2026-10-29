package podman_test

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/podman"
)

// TestTheManifestIsPodman holds the extension to what SPEC §10.20 says Podman's is: the id, the commands under podman.*, the setting
// podman.host, the Pods and Connections sections and no Contexts, the note about a rootful socket, and the engine Podman.
func TestTheManifestIsPodman(t *testing.T) {
	m := podman.New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "emetos.podman" || m.Name != "Podman" {
		t.Errorf("%s %s", m.ID, m.Name)
	}
	for _, c := range m.Contributes.Commands {
		if !strings.HasPrefix(c.ID, "podman.") {
			t.Errorf("the command %s", c.ID)
		}
	}
	if len(m.Contributes.Settings) < 1 || m.Contributes.Settings[0].Key != "podman.host" || !strings.Contains(m.Contributes.Settings[0].Description, "unix:///run/user/1000/podman/podman.sock") {
		t.Errorf("settings %+v", m.Contributes.Settings)
	}
	views := map[string]bool{}
	for _, v := range m.Contributes.Views {
		views[strings.TrimPrefix(v.ID, "emetos.podman.")] = true
	}
	for _, want := range []string{"containers", "pods", "images", "networks", "volumes", "connections"} {
		if !views[want] {
			t.Errorf("no view %s in %v", want, views)
		}
	}
	if views["contexts"] {
		t.Error("Podman has a Contexts view")
	}
	cfg := podman.Config()
	if cfg.Kind != containerapi.KindPodman || !cfg.Pods || !cfg.Connections || cfg.Contexts || cfg.LanguageServers {
		t.Errorf("config %+v", cfg)
	}
	if !strings.Contains(cfg.FirstRunNote, "rootful") || !strings.Contains(cfg.FirstRunNote, "power over this computer") {
		t.Errorf("the note %q", cfg.FirstRunNote)
	}
	if len(m.Contributes.LanguageServers) != 0 {
		t.Errorf("Podman declares language servers, which Docker's extension does for the same languages: %+v", m.Contributes.LanguageServers)
	}
	if podman.New() == podman.New() {
		t.Error("the factory made one extension twice")
	}
}
