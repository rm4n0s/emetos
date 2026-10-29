package containerui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestTheSectionsListWhatTheEngineHas holds the rows of every section to what the fake engine has: the containers under their
// Compose project (the ones of none under "Other Containers"), the images under their repository, the networks, the volumes and
// the contexts.
func TestTheSectionsListWhatTheEngineHas(t *testing.T) {
	r := newRig(t, sampleEngine)
	tests := []struct {
		section string
		want    []string
	}{
		{secContainers, []string{"blog", "blog-app-1", "shop", "shop-db-1", "shop-web-1", "Other Containers", "scratch"}},
		{secImages, []string{"busybox", "1.37.0", "nginx", "1.27", "latest", "postgres", "16", "<none>"}},
		{secNetworks, []string{"bridge", "shop_default"}},
		{secVolumes, []string{"pgdata"}},
		{secContexts, []string{"default"}},
	}
	for _, tc := range tests {
		got := r.labels(tc.section)
		if tc.section == secImages {
			got = got[:len(got)-1] // the label of the nameless image is its id
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: rows %v, want %v", tc.section, got, tc.want)
		}
	}
}

// TestTheViewsAreDeclaredAndShownOnlyWithAnEngine holds the manifest: the Containers view is always shown, the others and the
// actions that need an engine wait for the context key, and the extension is activated by showing a view.
func TestTheViewsAreDeclaredAndShownOnlyWithAnEngine(t *testing.T) {
	m := Manifest(testConfig(nil))
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	whens := map[string]string{}
	for _, v := range m.Contributes.Views {
		whens[v.ID] = v.When
	}
	want := map[string]string{
		"emetos.containers.containers": "", "emetos.containers.images": "emetos.containers.connected", "emetos.containers.networks": "emetos.containers.connected",
		"emetos.containers.volumes": "emetos.containers.connected", "emetos.containers.contexts": "",
	}
	if !reflect.DeepEqual(whens, want) {
		t.Errorf("views %v, want %v", whens, want)
	}
	if len(m.ActivationEvents) == 0 || m.ActivationEvents[0] != extension.OnView("emetos.containers.containers") {
		t.Errorf("activation events %v", m.ActivationEvents)
	}
	podman := testConfig(nil)
	podman.Contexts = false
	for _, v := range Manifest(podman).Contributes.Views {
		if strings.HasSuffix(v.ID, ".contexts") {
			t.Error("a Config without contexts has the Contexts view")
		}
	}
}

// TestEveryCommandOfTheManifestHasAHandler holds the manifest and the activation to the same table.
func TestEveryCommandOfTheManifestHasAHandler(t *testing.T) {
	r := newRig(t, nil)
	for _, c := range Manifest(r.cfg).Contributes.Commands {
		if !r.api.HasHandler(c.ID) {
			t.Errorf("no handler for %s", c.ID)
		}
		if !strings.HasPrefix(c.ID, "containers.") {
			t.Errorf("the id %s does not start with the prefix", c.ID)
		}
	}
}

// TestFilterNarrowsAndClears runs Filter on the containers and the images, and Clear Filter, and watches the context key that
// switches the two buttons.
func TestFilterNarrowsAndClears(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("containers.filter", nil)
	r.api.LastInput().Accept("web")
	if got := r.labels(secContainers); !reflect.DeepEqual(got, []string{"shop", "shop-web-1"}) {
		t.Errorf("filtered containers: %v", got)
	}
	if r.api.ContextKey("emetos.containers.containersFiltered") != true {
		t.Error("the filtered key is not set")
	}
	r.run("containers.filter", nil)
	r.api.LastInput().Accept("nothing-like-this")
	if r.view(secContainers).message != `No containers match "nothing-like-this".` {
		t.Errorf("the message: %q", r.view(secContainers).message)
	}
	r.run("containers.clearFilter", nil)
	if len(r.labels(secContainers)) != 7 || r.api.ContextKey("emetos.containers.containersFiltered") != false {
		t.Errorf("not cleared: %v", r.labels(secContainers))
	}

	r.run("images.filter", nil)
	r.api.LastInput().Accept("postgres")
	if got := r.labels(secImages); !reflect.DeepEqual(got, []string{"postgres", "16"}) {
		t.Errorf("filtered images: %v", got)
	}
	r.run("images.clearFilter", nil)
	if len(r.labels(secImages)) != 9 {
		t.Errorf("images not cleared: %v", r.labels(secImages))
	}
}

// TestDanglingImagesCanBeHidden shows the nameless image marked, hides it with Hide Dangling Images and shows it again.
func TestDanglingImagesCanBeHidden(t *testing.T) {
	r := newRig(t, sampleEngine)
	has := func() bool {
		for _, l := range r.labels(secImages) {
			if l == noRepository {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Fatalf("the nameless image is not listed: %v", r.labels(secImages))
	}
	r.run("images.hideDangling", nil)
	r.wait("the images listed again", func() bool { return !has() })
	if r.api.ContextKey("emetos.containers.danglingShown") != false {
		t.Error("the key still says they are shown")
	}
	state, err := r.ext.saveState()
	if err != nil || state == nil {
		t.Fatalf("the choice is not saved: %v %v", state, err)
	}
	r.run("images.showDangling", nil)
	r.wait("the nameless image back", has)
}

// TestTheStateIconsAndColors holds the icon of each state to Container Tools' getContainerStateIcon.
func TestTheStateIconsAndColors(t *testing.T) {
	colors := uitest.Themes()[0].Colors
	tests := []struct {
		c     containerapi.Container
		icon  string
		color gui.Color
	}{
		{containerapi.Container{State: "running"}, "debug-start", colors.DebugIconStartForeground},
		{containerapi.Container{State: "exited"}, "debug-stop", colors.DebugIconStopForeground},
		{containerapi.Container{State: "created"}, "debug-stop", colors.DebugIconStopForeground},
		{containerapi.Container{State: "paused"}, "debug-pause", colors.DebugIconPauseForeground},
		{containerapi.Container{State: "restarting"}, "debug-restart", colors.DebugIconRestartForeground},
		{containerapi.Container{State: "running", Status: "Up 1 hour (unhealthy)"}, "warning", colors.ProblemsWarningIconForeground},
		{containerapi.Container{State: "running", Status: "Up 1 second (health: starting)"}, "debug-rerun", colors.DebugIconStartForeground},
	}
	for _, tc := range tests {
		icon, color := stateIcon(colors, tc.c)
		if icon != tc.icon || color != tc.color {
			t.Errorf("%s %q: icon %s, color %v; want %s, %v", tc.c.State, tc.c.Status, icon, color, tc.icon, tc.color)
		}
	}
}

// TestNoEngineShowsTheReasonAndRetryConnects makes the engine not answer: the Containers view says why, with Retry, the other
// views are hidden, and Retry (pressed in the view) connects when the engine is there.
func TestNoEngineShowsTheReasonAndRetryConnects(t *testing.T) {
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	var up atomic.Bool
	var calls atomic.Int32
	cfg := testConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		calls.Add(1)
		if !up.Load() {
			return nil, errors.New("ContainerAPIEngineNotFound", "No Docker engine answered:\n- DOCKER_HOST: connection refused\n- /var/run/docker.sock: no such file")
		}
		return &containerapi.Found{Client: fake.Client()}, nil
	})
	r := activate(t, fake, cfg)
	if r.ext.status != statusMissing || !strings.Contains(r.ext.reason, "No Docker engine answered") {
		t.Fatalf("status %v, reason %q", r.ext.status, r.ext.reason)
	}
	if r.api.ContextKey("emetos.containers.connected") != false {
		t.Error("the connected key is set")
	}
	ts := r.show(uitest.Themes()[0], gui.Size{Width: 380, Height: 200}, cfg.ViewID(secContainers))
	for _, text := range []string{"Docker is not available", "Retry"} {
		if !ts.has(text) {
			t.Errorf("the view does not say %q", text)
		}
	}
	if !ts.hasPart("DOCKER_HOST: connection refused") {
		t.Error("the places that were asked are not listed")
	}
	up.Store(true)
	ts.tap("Retry")
	r.wait("the connection", func() bool { return r.ext.status == statusConnected && r.ext.loads[resContainers].loaded })
	if calls.Load() != 2 || r.api.ContextKey("emetos.containers.connected") != true {
		t.Errorf("calls %d, key %v", calls.Load(), r.api.ContextKey("emetos.containers.connected"))
	}
}

// TestAnActionWithNoEngineSaysSo runs an action while no engine is there: a notification, and nothing is asked of the engine.
func TestAnActionWithNoEngineSaysSo(t *testing.T) {
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	cfg := testConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return nil, errors.New("ContainerAPIEngineNotFound", "nothing answered")
	})
	r := activate(t, fake, cfg)
	r.run("containers.remove", nil)
	r.run("images.pull", nil)
	if !r.hasNote("Docker is not running") {
		t.Errorf("notes %v", r.notes())
	}
	if len(fake.Requests()) != 0 {
		t.Errorf("requests %v", fake.Requests())
	}
}

// TestTheFirstRunNoteShowsUntilItIsDismissed shows the note about the Docker socket, dismisses it with Got it, and finds it saved
// with the state of the view, so that it does not come back after a restart.
func TestTheFirstRunNoteShowsUntilItIsDismissed(t *testing.T) {
	r := newRig(t, sampleEngine)
	ts := r.show(uitest.Themes()[0], gui.Size{Width: 380, Height: 300}, r.cfg.ViewID(secContainers))
	if !ts.hasPart("Access to Docker is power over this computer") {
		t.Fatal("the note is not shown")
	}
	ts.tap("Got it")
	if ts.hasPart("power over this computer") {
		t.Error("the note is still shown")
	}
	state, err := r.ext.saveState()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(state)

	again := newRig(t, sampleEngine)
	if err := again.ext.restoreState(raw); err != nil {
		t.Fatal(err)
	}
	ts2 := again.show(uitest.Themes()[0], gui.Size{Width: 380, Height: 300}, again.cfg.ViewID(secContainers))
	if ts2.hasPart("power over this computer") {
		t.Error("the note came back after the state was restored")
	}
}

// TestTheContextPickerChoosesTheEngine lists the Docker contexts of an isolated configuration, picks one and watches the search
// for the engine start again with it; the pick is saved with the state of the view.
func TestTheContextPickerChoosesTheEngine(t *testing.T) {
	cfgDir := t.TempDir()
	writeContext(t, cfgDir, "remote", "unix:///tmp/none.sock")
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	var seen recorder[containerapi.FindOptions]
	cfg := testConfig(func(_ context.Context, o containerapi.FindOptions) (*containerapi.Found, error) {
		seen.add(o)
		return &containerapi.Found{Client: fake.Client()}, nil
	})
	cfg.Getenv = func(k string) string {
		if k == "DOCKER_CONFIG" {
			return cfgDir
		}
		return ""
	}
	r := activate(t, fake, cfg)
	if got := r.labels(secContexts); !reflect.DeepEqual(got, []string{"default", "remote"}) {
		t.Fatalf("contexts %v", got)
	}
	if seen.at(0).Context != "" {
		t.Errorf("the first search has the context %q", seen.at(0).Context)
	}
	r.run("contexts.use", nil)
	pick := r.api.LastPick()
	if pick == nil || !reflect.DeepEqual(pick.Labels(), []string{"default", "remote"}) {
		t.Fatalf("the pick: %v", pick)
	}
	pick.Choose(1)
	r.wait("the second search", func() bool { return seen.len() == 2 && r.ext.status == statusConnected })
	if seen.at(1).Context != "remote" {
		t.Errorf("the second search has the context %q", seen.at(1).Context)
	}
	if r.ext.currentContext() != "remote" {
		t.Errorf("the current context is %q", r.ext.currentContext())
	}
	state, _ := r.ext.saveState()
	if st, ok := state.(viewState); !ok || st.Context != "remote" {
		t.Errorf("the state %v", state)
	}
	// a context the CLI uses is not changed
	if _, err := os.Stat(filepath.Join(cfgDir, "config.json")); err == nil {
		t.Error("the configuration of Docker was written")
	}
	items := r.view(secContexts).sec.menu(r.view(secContexts), []string{keyContext + "default"})
	if len(items) != 2 || items[0].Label != "Use" || items[0].Disabled {
		t.Errorf("the menu of a context: %v", items)
	}
}

// writeContext makes a Docker context in a configuration folder.
func writeContext(t *testing.T, dir, name, host string) {
	t.Helper()
	sum := containerapiContextDir(name)
	meta := filepath.Join(dir, "contexts", "meta", sum)
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{"Name": name, "Metadata": map[string]any{"Description": "a test"}, "Endpoints": map[string]any{"docker": map[string]any{"Host": host}}}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(meta, "meta.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
