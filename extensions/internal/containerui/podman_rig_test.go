package containerui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// podmanConfig is the configuration of the Podman tests: what extensions/podman has, with the engine given. The program podman is
// found only in the folder of the fake CLI of a test (newFakeCLI), never on the PATH of the computer: no test runs the user's own
// podman, which would read the user's connections and containers.conf.
func podmanConfig(dial DialFunc) Config {
	cfg := testConfig(dial)
	cfg.Kind = containerapi.KindPodman
	cfg.ID, cfg.Name, cfg.Engine, cfg.Prefix, cfg.Icon, cfg.Order = "emetos.podman", "Podman", "Podman", "podman", "server-environment", 61
	cfg.Description = "Containers, pods and so on of Podman"
	cfg.Contexts, cfg.Pods, cfg.Connections = false, true, true
	cfg.MenuTitlePrefix = "Podman: "
	cfg.FirstRunNote = "A rootful Podman socket is power over this computer."
	cfg.LookPath = func(name string) (string, error) {
		dir := os.Getenv("FAKE_CLI_DIR")
		if dir == "" {
			return "", exec.ErrNotFound
		}
		return exec.LookPath(filepath.Join(dir, "bin", name))
	}
	return cfg
}

// newPodmanRig starts a fake Podman engine with what setup adds, puts a fake podman program on PATH, and activates the extension of
// Podman over them. The fake program answers `system connection list` with no connection until the test says otherwise.
func newPodmanRig(t *testing.T, setup func(t testing.TB, f *containerapitest.Fake)) (*rig, *fakeCLI) {
	t.Helper()
	return newPodmanRigWith(t, setup, "[]")
}

// newPodmanRigWith is newPodmanRig with the connections the fake program lists (the JSON of `system connection list`).
func newPodmanRigWith(t *testing.T, setup func(t testing.TB, f *containerapitest.Fake), connections string) (*rig, *fakeCLI) {
	t.Helper()
	cli := newFakeCLI(t, "podman")
	cli.answer("system_connection", connections, "", 0)
	fake := containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{})
	if setup != nil {
		setup(t, fake)
	}
	cfg := podmanConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: fake.Client(), Source: "the fake engine"}, nil
	})
	r := startRig(t, fake, cfg)
	r.wait("the first lists", func() bool {
		x := r.ext
		return x.status == statusConnected && x.loads[resContainers].loaded && x.loads[resImages].loaded && x.loads[resNetworks].loaded &&
			x.loads[resVolumes].loaded && x.loads[resPods].loaded && x.loads[resConnections].loaded
	})
	return r, cli
}

// samplePodman fills a fake Podman with two pods (web, which runs, with its containers, and batch, which has run and stopped), a
// container of a podman-compose project, one of no pod and no project, and images.
func samplePodman(t testing.TB, f *containerapitest.Fake) {
	t.Helper()
	ctx := context.Background()
	f.AddImage(187_000_000, "docker.io/library/nginx:1.27")
	f.AddImage(60_000_000, "docker.io/library/redis:7")
	f.AddImage(5_000_000, "docker.io/library/busybox:1.37.0")
	c := f.Client()
	if _, err := c.CreatePod(ctx, containerapi.PodSpec{Name: "web", Ports: []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 8080, ContainerPort: 80}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreatePod(ctx, containerapi.PodSpec{Name: "batch"}); err != nil {
		t.Fatal(err)
	}
	start := func(name, image, pod string, labels map[string]string, stop bool) {
		res, err := c.CreateContainer(ctx, containerapi.ContainerSpec{Name: name, Image: image, Pod: pod, Labels: labels, Env: []string{"MODE=test"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.StartContainer(ctx, res.ID); err != nil {
			t.Fatal(err)
		}
		if stop {
			if err := c.StopContainer(ctx, res.ID, 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	start("web-nginx", "docker.io/library/nginx:1.27", "web", nil, false)
	start("web-redis", "docker.io/library/redis:7", "web", nil, false)
	start("batch-job", "docker.io/library/busybox:1.37.0", "batch", nil, true)
	f.AddContainer("shop-api-1", "docker.io/library/nginx:1.27", map[string]string{containerapi.LabelPodmanCompose: "shop"}, "running")
	f.AddContainer("scratch", "docker.io/library/busybox:1.37.0", nil, "exited")
}

// podID is the id of the pod with a name in the engine.
func (r *rig) podID(name string) string {
	r.t.Helper()
	for _, p := range r.ext.pods {
		if p.Name == name {
			return p.ID
		}
	}
	r.t.Fatalf("no pod %s in %v", name, podNames(r.ext.pods))
	return ""
}

// enginePods lists the pods the engine has, by name.
func (r *rig) enginePods() map[string]containerapi.Pod {
	r.t.Helper()
	ps, err := r.fake.Client().ListPods(context.Background(), nil)
	if err != nil {
		r.t.Fatal(err)
	}
	out := map[string]containerapi.Pod{}
	for _, p := range ps {
		out[p.Name] = p
	}
	return out
}

// requests lists the requests of the fake as "METHOD path", for a look at what an action sent.
func requests(r *rig) []string {
	var out []string
	for _, rq := range r.fake.Requests() {
		out = append(out, rq.Method+" "+rq.Path)
	}
	return out
}

// sent tells whether a request ("POST /libpod/pods/<id>/start" without the prefix) was sent.
func sent(r *rig, method, suffix string) bool {
	for _, l := range requests(r) {
		if strings.HasPrefix(l, method+" ") && strings.HasSuffix(l, suffix) {
			return true
		}
	}
	return false
}

// testCtx is the context of the calls a test makes to the fake engine itself.
var testCtx = context.Background()

// podSpecNamed is the spec of an empty pod.
func podSpecNamed(name string) containerapi.PodSpec { return containerapi.PodSpec{Name: name} }
