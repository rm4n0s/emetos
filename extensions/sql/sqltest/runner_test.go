package sqltest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/emetos/extensions/sql/sqltest"
)

func TestTheImagesAreFullyPinnedAndHaveNoSQLServer(t *testing.T) {
	for _, ref := range []string{containerapitest.ImagePostgres, containerapitest.ImageMySQL, containerapitest.ImageMariaDB} {
		i := strings.LastIndex(ref, ":")
		if i < 0 || strings.Contains(ref[i+1:], "latest") || !strings.ContainsAny(ref[i+1:], "0123456789") {
			t.Errorf("%s is not pinned to a version", ref)
		}
		if !containerapitest.IsPinned(ref) {
			t.Errorf("%s is not in the list of pinned images", ref)
		}
	}
	for _, ref := range containerapitest.PinnedImages {
		if strings.Contains(ref, "mssql") || strings.Contains(ref, "microsoft") {
			t.Errorf("%s: a test never starts a server whose terms the user has not accepted", ref)
		}
	}
}

func TestTheContainerSpecFollowsTheRulesOfTheTests(t *testing.T) {
	spec := sqltest.Spec{Image: containerapitest.ImageMySQL, Port: 3306, Env: map[string]string{"MYSQL_ROOT_PASSWORD": "hunter2", "A_FIRST": "x"}, Args: []string{"--character-set-server=utf8mb4"}}
	got := sqltest.ContainerSpec(spec)
	if got.Image != spec.Image || len(got.Cmd) != 1 || got.Cmd[0] != "--character-set-server=utf8mb4" {
		t.Errorf("image and arguments: %+v", got)
	}
	if len(got.Env) != 2 || got.Env[0] != "A_FIRST=x" || got.Env[1] != "MYSQL_ROOT_PASSWORD=hunter2" {
		t.Errorf("the environment is not in a stable order: %v", got.Env)
	}
	if len(got.Ports) != 1 || got.Ports[0].ContainerPort != 3306 || got.Ports[0].HostIP != "" || got.Ports[0].HostPort != 0 {
		t.Errorf("ports: %+v (the harness picks the host port and makes the address 127.0.0.1)", got.Ports)
	}
	if len(got.Binds) != 0 || got.Network != "" || got.Name != "" || len(got.Labels) != 0 {
		t.Errorf("the spec asks for more than the image and the port: %+v", got)
	}
}

func TestNamesAndPasswordsAreRandom(t *testing.T) {
	a, b := sqltest.RandomName(t), sqltest.RandomName(t)
	if a == b || !strings.HasPrefix(a, containerapitest.NamePrefix) || len(a) != len(containerapitest.NamePrefix)+12 {
		t.Errorf("names = %q %q", a, b)
	}
	p, q := sqltest.RandomPassword(t), sqltest.RandomPassword(t)
	if p == q || len(p) != 24 || !strings.HasSuffix(p, "aZ9_") {
		t.Errorf("passwords = %q %q", p, q)
	}
}

func TestAServerStartsOnTheHarness(t *testing.T) {
	for _, kind := range []containerapi.Kind{containerapi.KindDocker, containerapi.KindPodman} {
		t.Run(string(kind), func(t *testing.T) {
			// the harness removes the container when this test ends and fails the test when something labelled is left
			h := containerapitest.NewFakeHarness(t, kind)
			srv := sqltest.OnHarness(h).Start(t, sqltest.Spec{
				Image: containerapitest.ImagePostgres, Port: 5432, Env: map[string]string{"POSTGRES_PASSWORD": "secret-for-the-test"},
			})
			if !strings.HasPrefix(srv.Name, containerapitest.NamePrefix) || srv.Host != containerapitest.Loopback || srv.Port == 0 {
				t.Errorf("server = %+v", srv)
			}
			info, err := h.Client.InspectContainer(h.Context(10*time.Second), srv.Name)
			if err != nil {
				t.Fatal(err)
			}
			if info.Labels[containerapitest.LabelKey] != containerapitest.LabelValue || !info.State.Running || len(info.Mounts) != 0 {
				t.Errorf("container = %+v", info)
			}
			if got := info.Ports["5432/tcp"]; len(got) != 1 || got[0].HostIP != containerapitest.Loopback {
				t.Errorf("ports = %+v", info.Ports)
			}
		})
	}
}
