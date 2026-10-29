package containerui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestThePodmanViewHasItsSections holds the views of the Podman container: Containers, Pods, Images, Networks, Volumes and
// Connections, and no Contexts.
func TestThePodmanViewHasItsSections(t *testing.T) {
	r, _ := newPodmanRig(t, samplePodman)
	var ids []string
	for _, v := range r.ext.Manifest().Contributes.Views {
		ids = append(ids, strings.TrimPrefix(v.ID, "emetos.podman."))
	}
	if want := []string{"containers", "pods", "images", "networks", "volumes", "connections"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("views %v, want %v", ids, want)
	}
	if got := r.labels(secPods); !reflect.DeepEqual(got, []string{"batch", "web"}) {
		t.Errorf("pods %v", got)
	}
	if err := extension.CheckManifest(r.ext.Manifest()); err != nil {
		t.Fatal(err)
	}
}

// TestTheContainersAreGroupedUnderTheirPod holds SPEC §10.20's "grouped by pod for Podman": the pods first, a podman-compose project
// by its label, then the containers that are in neither; a pod's group has the pod's menu, not the Compose group's.
func TestTheContainersAreGroupedUnderTheirPod(t *testing.T) {
	r, _ := newPodmanRig(t, samplePodman)
	want := []string{"batch", "batch-job", "web", "web-nginx", "web-redis", "shop", "shop-api-1", "Other Containers", "scratch"}
	if got := r.labels(secContainers); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	tree := r.view(secContainers).tree
	if !tree.Has(keyGroup+podGroup+"web") || !tree.Has(keyGroup+"shop") {
		t.Errorf("the keys of the groups are not %q and %q", keyGroup+podGroup+"web", keyGroup+"shop")
	}
	web := r.ext.containersOfKeys([]string{keyGroup + podGroup + "web"})
	if got := containerNames(web); !reflect.DeepEqual(got, []string{"web-nginx", "web-redis"}) {
		t.Errorf("the containers of the pod group: %v", got)
	}
	if got := containerNames(r.ext.containersOfKeys([]string{keyGroup + "shop"})); !reflect.DeepEqual(got, []string{"shop-api-1"}) {
		t.Errorf("the containers of the project group: %v", got)
	}
	if got := containerNames(r.ext.containersOfKeys([]string{keyGroup + otherGroup})); !reflect.DeepEqual(got, []string{"scratch"}) {
		t.Errorf("the containers of no group: %v", got)
	}
	podMenu := menuLabels(r.view(secContainers).sec.menu(r.view(secContainers), []string{keyGroup + podGroup + "web"}))
	for _, l := range []string{"Inspect", "Generate Kube YAML", "Stop", "Restart", "Pause", "Remove...", "Copy Container IDs"} {
		if !containsString(podMenu, l) {
			t.Errorf("the menu of a pod group has no %q: %v", l, podMenu)
		}
	}
	if containsString(podMenu, "Compose Down...") {
		t.Errorf("a pod group has the Compose menu: %v", podMenu)
	}
	projMenu := menuLabels(r.view(secContainers).sec.menu(r.view(secContainers), []string{keyGroup + "shop"}))
	if !containsString(projMenu, "Compose Down...") {
		t.Errorf("the group of a podman-compose project has no Compose menu: %v", projMenu)
	}
}

// menuLabels lists the labels of the items of a menu.
func menuLabels(items []ui.MenuItem) []string {
	var out []string
	for _, it := range items {
		if it.Label != "" {
			out = append(out, it.Label)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TestPodActionsReachTheEngine runs each action on a pod through its command and checks the request the engine got.
func TestPodActionsReachTheEngine(t *testing.T) {
	for _, tt := range []struct {
		command, pod, method, suffix string
	}{
		{"pods.start", "batch", "POST", "/start"},
		{"pods.stop", "web", "POST", "/stop"},
		{"pods.restart", "web", "POST", "/restart"},
		{"pods.pause", "web", "POST", "/pause"},
	} {
		t.Run(tt.command, func(t *testing.T) {
			r, _ := newPodmanRig(t, samplePodman)
			id := r.podID(tt.pod)
			r.fake.ResetRequests()
			r.run(tt.command, key(keyPod+id))
			r.wait("the request", func() bool { return sent(r, tt.method, "/pods/"+id+tt.suffix) })
			if len(r.api.Notes()) != 0 {
				t.Errorf("notes %v", r.notes())
			}
		})
	}
	t.Run("a pod that cannot be started says why", func(t *testing.T) {
		r, _ := newPodmanRig(t, samplePodman)
		r.fake.FailNext("POST", "/start", 500, "pod web: port 8080 is in use")
		r.run("pods.start", key(keyPod+r.podID("batch")))
		r.wait("the message", func() bool { return r.hasNote("Cannot start the pod batch: ") && r.hasNote("port 8080 is in use") })
	})
	t.Run("the state of a pod follows its containers", func(t *testing.T) {
		r, _ := newPodmanRig(t, samplePodman)
		r.run("pods.stop", key(keyPod+r.podID("web")))
		r.wait("the pod stopped", func() bool {
			p, _ := r.ext.podByID(r.podID("web"))
			return p.Status == "Exited"
		})
	})
	t.Run("the palette picks a pod when no row is named", func(t *testing.T) {
		r, _ := newPodmanRig(t, samplePodman)
		r.run("pods.inspect", nil)
		r.api.Pump()
		pick := r.api.LastPick()
		if pick == nil || !reflect.DeepEqual(pick.Labels(), []string{"batch", "web"}) {
			t.Fatalf("the pick: %+v", pick)
		}
		pick.Choose(1)
		r.wait("the tab", func() bool { return len(r.api.EditorTabs()) == 1 })
	})
}

// TestRemovingPodsAsksFirst holds SPEC §10.20 "Safety" for pods: the question names the pods and says how many containers go with
// them, nothing is sent before a choice is made, Cancel changes nothing, and a pod that runs is removed only by the choice that
// says it is killed.
func TestRemovingPodsAsksFirst(t *testing.T) {
	t.Run("a stopped pod", func(t *testing.T) {
		r, _ := newPodmanRig(t, samplePodman)
		r.fake.ResetRequests()
		r.run("pods.remove", key(keyPod+r.podID("batch")))
		q := r.asked()
		if q.title != "Remove 1 pod?" || !strings.Contains(strings.Join(q.lines, "\n"), "batch") || !strings.Contains(strings.Join(q.lines, "\n"), "1 container go with it.") {
			t.Errorf("the question: %q %q", q.title, q.lines)
		}
		if got := choiceLabels(q); !reflect.DeepEqual(got, []string{"Remove 1 pod"}) {
			t.Errorf("choices %v", got)
		}
		r.settle()
		if d := destructive(r.fake); len(d) != 0 {
			t.Fatalf("the engine got %v before an answer", d)
		}
		r.ext.cancelConfirm()
		r.settle()
		if d := destructive(r.fake); len(d) != 0 || r.ext.confirm != nil {
			t.Errorf("Cancel changed something: %v, question %v", d, r.ext.confirm)
		}
		if _, ok := r.enginePods()["batch"]; !ok {
			t.Error("the pod is gone after Cancel")
		}
		batch := r.podID("batch")
		r.run("pods.remove", key(keyPod+batch))
		r.ext.answer(r.asked(), 0)
		r.wait("the pod removed", func() bool { _, ok := r.enginePods()["batch"]; return !ok })
		if !sent(r, "DELETE", "/pods/"+batch) {
			t.Error("the pod was not deleted by id")
		}
	})
	t.Run("a pod that runs", func(t *testing.T) {
		r, _ := newPodmanRig(t, samplePodman)
		r.fake.ResetRequests()
		r.run("pods.remove", key(keyPod+r.podID("web")))
		q := r.asked()
		if got := choiceLabels(q); !reflect.DeepEqual(got, []string{"Force remove 1 (kills 1 running)"}) {
			t.Fatalf("choices %v", got)
		}
		if !strings.Contains(strings.Join(q.lines, "\n"), "2 containers go with it.") || !strings.Contains(strings.Join(q.lines, "\n"), "1 pod running: web") {
			t.Errorf("lines %q", q.lines)
		}
		r.ext.cancelConfirm()
		r.settle()
		if d := destructive(r.fake); len(d) != 0 {
			t.Errorf("Cancel sent %v", d)
		}
		r.run("pods.remove", key(keyPod+r.podID("web")))
		r.ext.answer(r.asked(), 0)
		r.wait("the pod removed", func() bool { _, ok := r.enginePods()["web"]; return !ok })
		r.wait("its containers", func() bool { return !containsString(r.engineNames(), "web-nginx") })
	})
	t.Run("a stopped and a running pod offer both ways", func(t *testing.T) {
		r, _ := newPodmanRig(t, samplePodman)
		r.view(secPods).tree.SetSelection([]string{keyPod + r.podID("web"), keyPod + r.podID("batch")})
		r.run("pods.remove", nil)
		q := r.asked()
		if got := choiceLabels(q); !reflect.DeepEqual(got, []string{"Force remove 2 (kills 1 running)", "Remove only the 1 stopped"}) {
			t.Fatalf("choices %v", got)
		}
		r.ext.answer(q, 1)
		r.wait("the stopped pod removed", func() bool { _, ok := r.enginePods()["batch"]; return !ok })
		r.settle()
		if _, ok := r.enginePods()["web"]; !ok {
			t.Error("the running pod was removed by the choice for the stopped ones")
		}
	})
	t.Run("from the group of a pod in the Containers section", func(t *testing.T) {
		r, _ := newPodmanRig(t, samplePodman)
		for _, it := range r.view(secContainers).sec.menu(r.view(secContainers), []string{keyGroup + podGroup + "batch"}) {
			if it.Label == "Remove..." {
				it.OnRun()
			}
		}
		q := r.asked()
		if q.title != "Remove 1 pod?" {
			t.Errorf("the question: %q", q.title)
		}
		r.ext.cancelConfirm()
	})
}

// TestPruningPodsAsksFirst holds that Prune names the stopped pods and Cancel sends nothing.
func TestPruningPodsAsksFirst(t *testing.T) {
	r, _ := newPodmanRig(t, samplePodman)
	r.fake.ResetRequests()
	r.run("pods.prune", nil)
	q := r.asked()
	if q.title != "Remove all stopped pods?" || !strings.Contains(strings.Join(q.lines, "\n"), "batch") || strings.Contains(strings.Join(q.lines, "\n"), "web") {
		t.Errorf("the question: %q %q", q.title, q.lines)
	}
	r.ext.cancelConfirm()
	r.settle()
	if d := destructive(r.fake); len(d) != 0 {
		t.Fatalf("Cancel sent %v", d)
	}
	r.run("pods.prune", nil)
	r.ext.answer(r.asked(), 0)
	r.wait("the prune", func() bool { _, ok := r.enginePods()["batch"]; return !ok })
	if _, ok := r.enginePods()["web"]; !ok {
		t.Error("the running pod was pruned")
	}
	// nothing to prune says so
	r.wait("the view to follow", func() bool { return !containsString(r.labels(secPods), "batch") })
	r.run("pods.prune", nil)
	if !r.hasNote("There are no stopped pods.") || r.ext.confirm != nil {
		t.Errorf("notes %v", r.notes())
	}
}

// TestCreatingAPod makes an empty pod, refusing a bad name.
func TestCreatingAPod(t *testing.T) {
	r, _ := newPodmanRig(t, nil)
	r.run("pods.create", nil)
	in := r.api.LastInput()
	if in.Validate("") == "" || in.Validate("a b") == "" || in.Validate("good") != "" {
		t.Errorf("the validation: %q %q %q", in.Validate(""), in.Validate("a b"), in.Validate("good"))
	}
	in.Accept("stack")
	r.wait("the pod", func() bool { return reflect.DeepEqual(r.labels(secPods), []string{"stack"}) })
	r.run("pods.create", nil)
	r.api.LastInput().Accept("stack")
	r.wait("the refusal", func() bool { return r.hasNote("Cannot create the pod stack: ") })
}

// TestCreatingAPodFromSelectedContainers makes a pod with new containers made from the chosen ones, publishes their ports on the pod,
// and leaves the chosen containers as they are.
func TestCreatingAPodFromSelectedContainers(t *testing.T) {
	r, _ := newPodmanRig(t, func(t testing.TB, f *containerapitest.Fake) {
		f.AddImage(187_000_000, "docker.io/library/nginx:1.27")
		f.AddImage(60_000_000, "docker.io/library/redis:7")
		c := f.Client()
		ctx := context.Background()
		for _, s := range []containerapi.ContainerSpec{
			{Name: "front", Image: "docker.io/library/nginx:1.27", Env: []string{"MODE=prod"}, Binds: []string{"/srv/site:/usr/share/nginx/html:ro"},
				Labels:     map[string]string{"tier": "front", containerapi.LabelPodmanCompose: "old"},
				Ports:      []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 8080, ContainerPort: 80}},
				WorkingDir: "/srv"},
			{Name: "cache", Image: "docker.io/library/redis:7", Cmd: []string{"redis-server", "--save", ""}},
		} {
			if _, err := c.CreateContainer(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
	})
	front, cache := r.id("front"), r.id("cache")
	r.fake.ResetRequests()
	r.view(secContainers).tree.SetSelection([]string{keyOfContainer(front), keyOfContainer(cache)})
	r.run("containers.createPod", nil)
	in := r.api.LastInput()
	if !strings.Contains(in.Options().Prompt, "2 containers made from cache, front") || !strings.Contains(in.Options().Prompt, "stay as they are") {
		t.Errorf("the prompt: %q", in.Options().Prompt)
	}
	in.Accept("site")
	r.wait("the notice", func() bool { return r.hasNote("Created the pod site with 2 containers.") })
	if d := destructive(r.fake); len(d) != 0 {
		t.Errorf("the engine got %v", d)
	}
	r.wait("the pod in the view", func() bool { return containsString(r.labels(secPods), "site") })
	r.wait("the copies in the view", func() bool {
		return containsString(r.engineNames(), "site-front") && containsString(r.engineNames(), "site-cache")
	})
	if !containsString(r.engineNames(), "front") || !containsString(r.engineNames(), "cache") {
		t.Errorf("the chosen containers are not as they were: %v", r.engineNames())
	}
	var podBody, frontBody map[string]any
	for _, rq := range r.fake.Requests() {
		if rq.Method == "POST" && strings.HasSuffix(rq.Path, "/pods/create") {
			jsonInto(t, rq.Body, &podBody)
		}
		if rq.Method == "POST" && strings.HasSuffix(rq.Path, "/containers/create") && strings.Contains(string(rq.Body), "site-front") {
			jsonInto(t, rq.Body, &frontBody)
		}
	}
	pm, _ := podBody["portmappings"].([]any)
	if podBody["name"] != "site" || len(pm) != 1 || !reflect.DeepEqual(pm[0], map[string]any{"container_port": 80.0, "host_port": 8080.0, "host_ip": "127.0.0.1", "protocol": "tcp"}) {
		t.Errorf("the pod was asked for as %v", podBody)
	}
	if frontBody["pod"] != "site" || frontBody["image"] != "docker.io/library/nginx:1.27" || frontBody["work_dir"] != "/srv" || frontBody["portmappings"] != nil {
		t.Errorf("the copy of front: %v", frontBody)
	}
	labels, _ := frontBody["labels"].(map[string]any)
	if labels["tier"] != "front" || labels[containerapi.LabelPodmanCompose] != nil {
		t.Errorf("labels of the copy: %v", labels)
	}
	if mounts, _ := frontBody["mounts"].([]any); len(mounts) != 1 {
		t.Errorf("mounts of the copy: %v", frontBody["mounts"])
	}
}

// TestAPodEventRefreshesThePods holds that a pod made outside the view shows up through the event stream, with no timer.
func TestAPodEventRefreshesThePods(t *testing.T) {
	r, _ := newPodmanRig(t, nil)
	if got := r.labels(secPods); len(got) != 0 {
		t.Fatalf("pods %v", got)
	}
	if r.ext.views[secPods].message != "No pods found." {
		t.Errorf("the message: %q", r.ext.views[secPods].message)
	}
	if _, err := r.fake.Client().CreatePod(context.Background(), containerapi.PodSpec{Name: "late"}); err != nil {
		t.Fatal(err)
	}
	r.wait("the pod", func() bool { return reflect.DeepEqual(r.labels(secPods), []string{"late"}) })
	r.wait("the event published", func() bool {
		for _, p := range r.api.PublishedEvents() {
			if p.Topic == "emetos.podman.changed" {
				return true
			}
		}
		return false
	})
}

// TestADockerViewHasNoPodsOrConnections holds that the Docker container has nothing of Podman's: no Pods section, no pod command,
// nothing asked of the CLI at the start.
func TestADockerViewHasNoPodsOrConnections(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	for _, v := range r.ext.Manifest().Contributes.Views {
		if strings.HasSuffix(v.ID, ".pods") || strings.HasSuffix(v.ID, ".connections") {
			t.Errorf("Docker has the view %s", v.ID)
		}
	}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		if strings.Contains(c.ID, ".pods.") || strings.Contains(c.ID, "kube") || strings.Contains(c.ID, "Quadlet") || strings.Contains(c.ID, "connections") {
			t.Errorf("Docker has the command %s", c.ID)
		}
	}
	for _, rq := range r.fake.Requests() {
		if strings.Contains(rq.Path, "/pods") {
			t.Errorf("Docker asked for %s", rq.Path)
		}
	}
	if len(cli.calls()) != 0 {
		t.Errorf("the CLI was run: %v", cli.lines())
	}
}

// TestThePodmanMenusAreTitledSoThatTheyAreToldApart holds that the Explorer's items of Podman say "Podman: ", and Docker's do not.
func TestThePodmanMenusAreTitledSoThatTheyAreToldApart(t *testing.T) {
	podman := Manifest(podmanConfig(nil)).Contributes.Menus
	docker := Manifest(testConfig(nil)).Contributes.Menus
	titleOf := func(items []extension.MenuItem, menu, command string) (string, bool) {
		for _, it := range items {
			if it.Menu == menu && it.Command == command {
				return it.Title, true
			}
		}
		return "", false
	}
	for _, tt := range []struct{ command, want string }{
		{"podman.images.build", "Podman: Build Image..."}, {"podman.compose.up", "Podman: Compose Up"}, {"podman.kube.play", "Podman: Play Kube..."},
		{"podman.kube.down", "Podman: Kube Down..."},
	} {
		got, ok := titleOf(podman, extension.MenuExplorerContext, tt.command)
		if !ok || got != tt.want {
			t.Errorf("%s in the Explorer's menu: %q %v, want %q", tt.command, got, ok, tt.want)
		}
	}
	if got, ok := titleOf(docker, extension.MenuExplorerContext, "containers.images.build"); !ok || got != "" {
		t.Errorf("Docker's Build Image is titled %q %v", got, ok)
	}
	for _, it := range docker {
		if strings.Contains(it.Command, "kube") {
			t.Errorf("Docker has the item %s", it.Command)
		}
	}
}

// jsonInto reads a JSON body of a request.
func jsonInto(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := jsonUnmarshalInto(data, v); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
}
