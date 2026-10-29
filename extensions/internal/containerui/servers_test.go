package containerui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// program makes an empty program in a folder and returns the folder.
func program(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func serversOf(cfg Config) []extension.LanguageServerSpec {
	return Manifest(cfg).Contributes.LanguageServers
}

// TestLanguageServersAreDeclaredOnlyWhenFoundOnPath holds SPEC §10.20 "Language support": the Dockerfile and the Compose servers are
// declared when their programs are on PATH, one by one, and never when the Config does not ask for them.
func TestLanguageServersAreDeclaredOnlyWhenFoundOnPath(t *testing.T) {
	tests := []struct {
		name  string
		progs []string
		on    bool
		want  []string // the ids of the servers
	}{
		{"none installed", nil, true, nil},
		{"the Dockerfile server", []string{"docker-langserver"}, true, []string{"dockerfile"}},
		{"the Compose server", []string{"docker-compose-langserver"}, true, []string{"compose"}},
		{"both", []string{"docker-langserver", "docker-compose-langserver"}, true, []string{"dockerfile", "compose"}},
		{"both, but not asked for", []string{"docker-langserver", "docker-compose-langserver"}, false, nil},
		{"programs of other names", []string{"langserver", "docker-compose"}, true, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATH", program(t, tc.progs...))
			cfg := testConfig(nil)
			cfg.LanguageServers = tc.on
			var ids []string
			for _, s := range serversOf(cfg) {
				ids = append(ids, s.ID)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Errorf("servers %v, want %v", ids, tc.want)
			}
			if err := extension.CheckManifest(Manifest(cfg)); err != nil {
				t.Errorf("the manifest: %v", err)
			}
		})
	}
}

func TestLanguageServersSpecs(t *testing.T) {
	t.Setenv("PATH", program(t, "docker-langserver", "docker-compose-langserver"))
	cfg := testConfig(nil)
	cfg.LanguageServers = true
	got := serversOf(cfg)
	want := []extension.LanguageServerSpec{
		{ID: "dockerfile", Name: "Dockerfile Language Server", Languages: []string{"dockerfile"}, Program: "docker-langserver", Args: []string{"--stdio"},
			PathSetting: "containers.dockerfileLanguageServer.path", Category: "Containers"},
		{ID: "compose", Name: "Compose Language Server", Languages: []string{"dockercompose"}, Program: "docker-compose-langserver", Args: []string{"--stdio"},
			PathSetting: "containers.composeLanguageServer.path", Category: "Containers"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("servers %+v\nwant %+v", got, want)
	}
	// the programs of the Config are looked for, and a test can name others
	cfg.DockerfileServer, cfg.ComposeServer = "my-dockerfile-ls", "my-compose-ls"
	if got := serversOf(cfg); len(got) != 0 {
		t.Errorf("programs that are not there are declared: %+v", got)
	}
	cfg.LookPath = func(name string) (string, error) { return "/x/" + name, nil }
	if got := serversOf(cfg); len(got) != 2 || got[0].Program != "my-dockerfile-ls" || got[1].Program != "my-compose-ls" {
		t.Errorf("servers %+v", got)
	}
}

// TestNothingIsInstalledByTheExtension holds that the extension only declares: with the programs missing it has no server, no
// command to install one, and says nothing.
func TestNothingIsInstalledByTheExtension(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg := testConfig(nil)
	cfg.LanguageServers = true
	m := Manifest(cfg)
	if len(m.Contributes.LanguageServers) != 0 {
		t.Fatal("a server without a program")
	}
	for _, c := range m.Contributes.Commands {
		if strings.Contains(strings.ToLower(c.Title), "install") {
			t.Errorf("the command %q installs something", c.Title)
		}
	}
	// the setting that holds the path is the host's: the extension declares the keys, not the settings
	if cfg.DockerfileServerPathSetting() != "containers.dockerfileLanguageServer.path" || cfg.ComposeServerPathSetting() != "containers.composeLanguageServer.path" {
		t.Error("the keys of the path settings")
	}
}

func TestTheDockerExtensionAsksForTheServers(t *testing.T) {
	if _, err := (Config{}).lookPath("definitely-not-a-program-x"); err == nil {
		t.Error("exec.LookPath found a program that is not there")
	}
	_, err := (&ext{cfg: Config{LookPath: func(string) (string, error) { return "", errors.New("X", "nope") }}}).findCLI()
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("ext.findCLI.ContainerUICLINotFound") {
		t.Errorf("route %v", err)
	}
}
