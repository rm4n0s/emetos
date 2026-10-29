package containerui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// explorerSuite is the test of the explorer against an engine, the fake one (TestTheExplorerOverTheFake) or the user's own (build
// tags emetos_docker and emetos_podman, real_engine_test.go): the extension is made to list only what the test made (the label
// dev.emetos.test=1), shows a container, stops it, starts it, follows its logs, and removes it through the question. It touches
// nothing else on the engine.
func explorerSuite(t *testing.T, h *containerapitest.Harness) {
	ctx := h.Context(3 * time.Minute)
	h.EnsureImage(ctx, containerapitest.ImageBusyBox)
	id := h.StartedContainer(ctx, containerapi.ContainerSpec{
		Image: containerapitest.ImageBusyBox,
		Cmd:   []string{"sh", "-c", `trap "exit 0" TERM; while true; do echo tick; sleep 1 & wait $!; done`},
	})
	cfg := testConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: h.Client, Source: "the test"}, nil
	})
	cfg.Kind = h.Kind
	if h.Kind == containerapi.KindPodman {
		// the Pods section too, listing only the pods of the test; not the Connections, which read the user's own configuration
		cfg.ID, cfg.Name, cfg.Engine, cfg.Prefix, cfg.Contexts, cfg.Pods = "emetos.podman", "Podman", "Podman", "podman", false, true
	}
	cfg.ListFilters = ListFilters{
		Containers: containerapitest.LabelFilter(), Networks: containerapitest.LabelFilter(), Volumes: containerapitest.LabelFilter(),
		Images: containerapi.Filters{"reference": {containerapitest.ImageBusyBox}}, Pods: containerapitest.LabelFilter(),
	}
	cfg.Debounce, cfg.EventsRetry = 20*time.Millisecond, 50*time.Millisecond
	r := startRig(t, nil, cfg)
	r.wait("the first lists", func() bool {
		return r.ext.status == statusConnected && r.ext.loads[resContainers].loaded && (!cfg.Pods || r.ext.loads[resPods].loaded)
	})

	state := func() string {
		cs, err := h.Client.ListContainers(ctx, containerapi.ListContainersOptions{All: true, Filters: containerapitest.LabelFilter()})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cs {
			if c.ID == id {
				return c.State
			}
		}
		return "(gone)"
	}
	row := keyOfContainer(id)
	r.wait("the container in the view", func() bool { return r.view(secContainers).tree.Has(row) })
	for _, c := range r.ext.containers {
		if c.Labels[containerapitest.LabelKey] != containerapitest.LabelValue {
			t.Fatalf("the view lists %s, which the test did not make", c.Name())
		}
	}
	r.run("containers.stop", key(row))
	r.wait("the container stopped", func() bool { return state() == "exited" })
	r.wait("the view to follow", func() bool { c, _ := r.ext.containerByID(id); return c.State == "exited" })
	r.run("containers.start", key(row))
	r.wait("the container running", func() bool { return state() == "running" })
	r.wait("the view to follow", func() bool { c, _ := r.ext.containerByID(id); return c.State == "running" })
	r.run("containers.viewLogs", key(row))
	s := r.ext.logs.active
	r.wait("a line of the log", func() bool { return strings.Contains(strings.Join(linesOf(s), "\n"), "tick") })

	r.run("containers.remove", key(row))
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	r.settle()
	if state() == "(gone)" {
		t.Fatal("the container was removed before the question was answered")
	}
	r.ext.answer(r.ext.confirm, 0)
	r.wait("the container removed", func() bool { return state() == "(gone)" })
	r.wait("the view to follow", func() bool { return !r.view(secContainers).tree.Has(row) })
}

// TestTheExplorerOverTheFake is explorerSuite against the fake engines.
func TestTheExplorerOverTheFake(t *testing.T) {
	for _, kind := range []containerapi.Kind{containerapi.KindDocker, containerapi.KindPodman} {
		t.Run(string(kind), func(t *testing.T) { explorerSuite(t, containerapitest.NewFakeHarness(t, kind)) })
	}
}
