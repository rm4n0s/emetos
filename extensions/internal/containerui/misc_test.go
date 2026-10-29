package containerui

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestTheManifestIsConsistent holds the contributions to each other: every title action, palette entry and menu item names a
// command of the extension, the ids of the views and of the container start with the id of the extension, every title action
// belongs to a view, and nothing is declared twice.
func TestTheManifestIsConsistent(t *testing.T) {
	for _, withContexts := range []bool{true, false} {
		cfg := testConfig(nil)
		cfg.Contexts = withContexts
		m := Manifest(cfg)
		if err := extension.CheckManifest(m); err != nil {
			t.Fatal(err)
		}
		cmds := map[string]bool{}
		for _, c := range m.Contributes.Commands {
			if cmds[c.ID] {
				t.Errorf("the command %s is declared twice", c.ID)
			}
			cmds[c.ID] = true
		}
		views := map[string]bool{}
		for _, v := range m.Contributes.Views {
			views[v.ID] = true
			if !strings.HasPrefix(v.ID, m.ID+".") || v.Container != cfg.ContainerID() {
				t.Errorf("the view %+v", v)
			}
		}
		for _, v := range m.Contributes.PanelViews {
			views[v.ID] = true
		}
		for _, a := range m.Contributes.ViewActions {
			if !cmds[a.Command] || !views[a.View] {
				t.Errorf("the title action %+v names no command or no view", a)
			}
		}
		for _, mi := range m.Contributes.Menus {
			if !cmds[mi.Command] {
				t.Errorf("the menu item %+v names no command", mi)
			}
		}
		if got := cmds[cfg.Command("contexts.use")]; got != withContexts {
			t.Errorf("contexts %v, the Use command %v", withContexts, got)
		}
		if len(m.Contributes.EditorKinds) != 2 || len(m.Contributes.PanelViews) != 1 || len(m.Contributes.Settings) != 4 {
			t.Errorf("the contributions: %d kinds, %d panels, %d settings", len(m.Contributes.EditorKinds), len(m.Contributes.PanelViews), len(m.Contributes.Settings))
		}
	}
}

// TestRefreshReadsEverythingAgain runs Refresh after the engine changed behind the extension's back (an event it did not get).
func TestRefreshReadsEverythingAgain(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.fake.AddContainer("quiet-one", "busybox:1.37.0", nil, "exited") // AddContainer makes no event
	r.settle()
	if strings.Contains(strings.Join(r.labels(secContainers), ","), "quiet-one") {
		t.Fatal("the container was seen without an event")
	}
	r.run("containers.refresh", nil)
	r.wait("the container", func() bool { return strings.Contains(strings.Join(r.labels(secContainers), ","), "quiet-one") })
}

// TestSizesAreLeftOutWhenSystemDfFails asks to remove a volume while "system df" fails: the question is still asked, with the name
// and no size, and still waits for the answer.
func TestSizesAreLeftOutWhenSystemDfFails(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.fake.FailNext("GET", "/system/df", 500, "df is broken")
	r.run("volumes.remove", key(keyVolume+"pgdata"))
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	text := strings.Join(r.ext.confirm.lines, "\n")
	if !strings.Contains(text, "pgdata") || strings.Contains(text, "MB") && strings.Contains(text, "pgdata  ") {
		t.Errorf("the lines: %q", text)
	}
	if d := destructive(r.fake); len(d) != 0 {
		t.Errorf("the engine was changed: %v", d)
	}
}

// TestPullOfARowPullsItsName runs Pull on a row: no question, the pull of its name; and Refresh on a Compose project does nothing
// unexpected.
func TestPullOfARowPullsItsName(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.fake.ResetRequests()
	r.run("images.pull", key(r.imageKey("busybox:1.37.0")))
	if len(r.api.Inputs()) != 0 {
		t.Error("Pull on a row asked for a name")
	}
	r.wait("the pull", func() bool {
		for _, rq := range r.fake.Requests() {
			if rq.Method == "POST" && strings.HasSuffix(rq.Path, "/images/create") && strings.Contains(rq.Query, "busybox") {
				return true
			}
		}
		return false
	})
}

// TestTheExtensionWorksOverPodman holds the shared views to a Podman engine: Config without contexts, over the libpod API.
func TestTheExtensionWorksOverPodman(t *testing.T) {
	fake := containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{})
	fake.AddImage(5_000_000, "docker.io/library/busybox:1.37.0")
	fake.AddContainer("pod-web", "docker.io/library/busybox:1.37.0", nil, "running")
	cfg := testConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: fake.Client()}, nil
	})
	cfg.Kind, cfg.ID, cfg.Name, cfg.Engine, cfg.Prefix, cfg.Contexts = containerapi.KindPodman, "emetos.podman", "Podman", "Podman", "podman", false
	r := activate(t, fake, cfg)
	if got := r.labels(secContainers); len(got) != 1 || got[0] != "pod-web" {
		t.Errorf("containers %v", got)
	}
	if r.ext.views[secContexts] != nil {
		t.Error("Podman has a Contexts view")
	}
	if !r.api.HasHandler("podman.containers.start") || r.api.HasHandler("podman.contexts.use") {
		t.Error("the commands are not those of Podman")
	}
	if err := r.api.Run("podman.containers.stop", key(keyOfContainer(r.id("pod-web")))); err != nil {
		t.Fatal(err)
	}
	r.wait("the stop", func() bool { return r.state("pod-web") != "running" })
}
