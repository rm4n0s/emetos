package containerui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// reqOf finds a requirement of a manifest.
func reqOf(t *testing.T, m extension.Manifest, id string) extension.Requirement {
	t.Helper()
	for _, r := range m.Requirements {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no requirement %s", id)
	return extension.Requirement{}
}

// The checks never ask the engines of this computer: the host is a socket of the test, or the default sockets are moved into its folder.
func TestTheRequirementsOfTheContainerExtensions(t *testing.T) {
	dir := t.TempDir()
	isolated := func(ctx context.Context, opts containerapi.FindOptions) (*containerapi.Found, error) {
		opts.DockerSocket = filepath.Join(dir, "docker.sock")
		opts.RootfulPodmanSocket = filepath.Join(dir, "podman.sock")
		opts.Getenv = func(k string) string {
			if k == "XDG_RUNTIME_DIR" {
				return dir
			}
			return ""
		}
		return containerapi.Find(ctx, opts)
	}
	lookPath := func(have bool) func(string) (string, error) {
		return func(name string) (string, error) {
			if have {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("NotFound", "not found")
		}
	}
	tests := []struct {
		name   string
		kind   containerapi.Kind
		engine bool // a fake engine answers on the socket the setting names
		host   string
		cli    bool
		state  extension.RequirementState
		detail string
	}{
		{"Docker answers", containerapi.KindDocker, true, "", true, extension.RequirementFound, ""},
		{"Docker is installed but does not answer", containerapi.KindDocker, false, "unix:///no/docker.sock", true, extension.RequirementUnusable, "docker is installed, but no docker engine answers"},
		{"no Docker at all", containerapi.KindDocker, false, "unix:///no/docker.sock", false, extension.RequirementMissing, "no docker engine answers"},
		{"Podman answers", containerapi.KindPodman, true, "", true, extension.RequirementFound, ""},
		{"no Podman socket, but podman starts a service of Emetos's own", containerapi.KindPodman, false, "", true, extension.RequirementFound, "no Podman socket answers; the view starts a podman system service of Emetos's own"},
		{"no Podman", containerapi.KindPodman, false, "", false, extension.RequirementMissing, "no podman engine answers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Kind: tt.kind, ID: "emetos.x", Name: "X", Engine: string(tt.kind), Prefix: "x", Dial: isolated, LookPath: lookPath(tt.cli)}
			host := tt.host
			var fake *containerapitest.Fake
			if tt.engine {
				fake = containerapitest.NewFake(t, tt.kind, containerapitest.FakeOptions{EngineVersion: "28.5.1"})
				host = "unix://" + fake.Socket()
			}
			env := extension.RequirementEnv{Folder: t.TempDir(), Setting: func(k string) (any, bool) {
				if k == cfg.HostSetting() && host != "" {
					return host, true
				}
				return nil, false
			}}
			m := Manifest(cfg)
			if err := extension.CheckManifest(m); err != nil {
				t.Fatal(err)
			}
			got := reqOf(t, m, RequirementEngine).Check(context.Background(), env)
			if got.State != tt.state || !strings.HasPrefix(got.Detail, tt.detail) {
				t.Errorf("got %+v, want %s %q", got, tt.state, tt.detail)
			}
			if tt.engine {
				if got.Version != "28.5.1" || got.Path != "unix://"+fake.Socket() {
					t.Errorf("version %q path %q", got.Version, got.Path)
				}
				for _, r := range fake.Requests() {
					if p := strings.TrimPrefix(r.Path, "/libpod"); p != "/_ping" && p != "/version" {
						t.Errorf("the check asked the engine for %s %s", r.Method, p)
					}
				}
			}
		})
	}
}
