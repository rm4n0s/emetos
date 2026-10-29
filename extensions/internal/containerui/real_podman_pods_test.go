//go:build emetos_podman

package containerui

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// Pods, Kube and Quadlet of step 18.4 against the user's Podman, with the rules of CLAUDE.md "Testing notes": the pod is named
// emetos-test-<random> and carries the label dev.emetos.test=1 (the harness makes and removes it, and its container, after looking at
// the label), the YAML that Podman generates for it is played under another emetos-test-<random> name after its label was seen in the
// YAML, the pod that was played is taken down by the extension (through its question) and removed again in t.Cleanup when that did
// not happen, and the view lists only what carries the label. Nothing else of the engine is listed, changed or removed; no prune. Run it
// on its own:
//
//	scripts/heavy.sh --alone -- go test -p 1 -tags emetos_podman ./extensions/internal/containerui -run RealPodman -v
//
// Afterwards `podman ps -a --filter label=dev.emetos.test=1` and `podman pod ps --filter label=dev.emetos.test=1` show nothing.

// realPodmanRig is the extension of Podman over the real engine, listing only what the tests made, with the real podman program. It
// does not read the connections (that is `podman system connection list`, the user's own configuration).
func realPodmanRig(t *testing.T, h *containerapitest.Harness) *rig {
	t.Helper()
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("the podman program is not on PATH")
	}
	cfg := podmanConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: h.Client, Source: "the test"}, nil
	})
	cfg.LookPath = nil // the real program
	cfg.Connections = false
	cfg.ListFilters = ListFilters{
		Containers: containerapitest.LabelFilter(), Networks: containerapitest.LabelFilter(), Volumes: containerapitest.LabelFilter(),
		Pods: containerapitest.LabelFilter(), Images: containerapitest.LabelFilter(),
	}
	cfg.Debounce, cfg.EventsRetry = 20*time.Millisecond, 50*time.Millisecond
	r := startRig(t, nil, cfg)
	r.wait("the first lists", func() bool {
		return r.ext.status == statusConnected && r.ext.loads[resContainers].loaded && r.ext.loads[resPods].loaded
	})
	return r
}

func TestPodsKubeAndQuadletOverTheRealPodman(t *testing.T) {
	h := containerapitest.RealPodman(t)
	ctx := h.Context(5 * time.Minute)
	h.EnsureImage(ctx, containerapitest.ImageBusyBox)
	r := realPodmanRig(t, h)

	// a labelled pod with one labelled container, started
	pod := h.Pod(ctx)
	ctr := h.Container(ctx, containerapi.ContainerSpec{
		Name: pod + "-main", Image: containerapitest.ImageBusyBox, Pod: pod,
		Cmd: []string{"sh", "-c", `trap "exit 0" TERM; while true; do sleep 1 & wait $!; done`},
	})
	if err := h.Client.PodDo(ctx, pod, containerapi.PodStart); err != nil {
		t.Fatal(err)
	}
	podAndContainer := func() bool {
		for _, p := range r.ext.pods {
			if p.Name == pod && p.Status == "Running" && containsString(containerNames(r.ext.containersOfKeys([]string{keyGroup + podGroup + pod})), pod+"-main") { // with the infra container
				return true
			}
		}
		return false
	}
	deadline := time.Now().Add(20 * time.Second)
	for !podAndContainer() && time.Now().Before(deadline) {
		r.api.Pump()
		time.Sleep(20 * time.Millisecond)
	}
	if !podAndContainer() {
		var pods, cs []string
		for _, p := range r.ext.pods {
			pods = append(pods, p.Name+"="+p.Status)
		}
		for _, c := range r.ext.containers {
			cs = append(cs, c.Name()+" pod="+c.PodName+" state="+c.State)
		}
		t.Fatalf("the pod %s and its container are not in the view: pods %v, containers %v", pod, pods, cs)
	}
	for _, p := range r.ext.pods {
		if p.Labels[containerapitest.LabelKey] != containerapitest.LabelValue {
			t.Fatalf("the view lists the pod %s, which the test did not make", p.Name)
		}
	}
	if !r.view(secContainers).tree.Has(keyGroup + podGroup + pod) {
		t.Errorf("the containers are not grouped under the pod: %v", r.labels(secContainers))
	}

	// Generate Kube YAML for the pod
	r.run("pods.generateKube", key(keyPod+r.podID(pod)))
	r.wait("the YAML", func() bool { return len(r.api.UntitledDocuments()) == 1 })
	yaml := r.api.UntitledDocuments()[0].Text
	res := parseKubeNames(yaml)
	if len(res) != 1 || res[0].kind != "Pod" || res[0].name != pod {
		t.Fatalf("the YAML names %v:\n%s", res, yaml)
	}
	if !strings.Contains(yaml, containerapitest.ImageBusyBox) || !strings.Contains(yaml, "\n  labels:\n") {
		t.Fatalf("the YAML is not what was expected of a pod with a container of the pinned image:\n%s", yaml)
	}

	// the Quadlet unit of the container
	r.run("containers.generateQuadlet", key(keyOfContainer(ctr)))
	r.wait("the unit", func() bool { return len(r.api.UntitledDocuments()) == 2 })
	unit := r.api.UntitledDocuments()[1].Text
	if !strings.Contains(unit, "ContainerName="+pod+"-main\n") || !strings.Contains(unit, "Image="+containerapitest.ImageBusyBox+"\n") ||
		!strings.Contains(unit, "[Install]") || !strings.Contains(unit, "Pod=") {
		t.Errorf("the unit:\n%s", unit)
	}

	// Play Kube under another name: everything of the pod's name in the YAML becomes the other name. Podman does not write the labels
	// of the pod into the YAML (it writes app: <name>), so the test adds the label of the tests to the labels of the pod: what is played
	// is then labelled, and the view, which lists only labelled things, shows it.
	other := containerapitest.RandomName(t)
	t.Cleanup(func() { removePodOfTheTest(t, h, other) })
	labelled := strings.Replace(yaml, "\n  labels:\n", "\n  labels:\n    "+containerapitest.LabelKey+": \""+containerapitest.LabelValue+"\"\n", 1)
	played := r.file("played.yaml", strings.ReplaceAll(labelled, pod, other))
	r.run("kube.play", explorerArgs(played))
	s := r.cliTab()
	if s.status != "Finished" {
		t.Fatalf("Kube Play ended with %q:\n%s", s.status, strings.Join(linesOf(s), "\n"))
	}
	r.wait("the played pod in the view", func() bool {
		for _, p := range r.ext.pods {
			if p.Name == other {
				return true
			}
		}
		return false
	})

	// Kube Down asks first; Cancel changes nothing; the answer takes the pod down
	r.run("kube.down", explorerArgs(played))
	q := r.asked()
	if !strings.Contains(strings.Join(q.lines, "\n"), other) {
		t.Errorf("the question: %q", q.lines)
	}
	r.ext.cancelConfirm()
	r.settle()
	if _, err := h.Client.InspectPod(ctx, other); err != nil {
		t.Fatalf("Cancel took the pod down: %v", err)
	}
	r.run("kube.down", explorerArgs(played))
	r.ext.answer(r.asked(), 0)
	r.wait("the end of Kube Down", func() bool { return len(r.ext.logs.sessions) == 2 && r.ext.logs.sessions[1].ended }) // after the tab of Kube Play
	if last := r.ext.logs.sessions[1]; last.status != "Finished" {
		t.Errorf("Kube Down ended with %q:\n%s", last.status, strings.Join(linesOf(last), "\n"))
	}
	r.wait("the pod gone from the view", func() bool {
		for _, p := range r.ext.pods {
			if p.Name == other {
				return false
			}
		}
		return true
	})

	// the pod of the test is removed through the question of the view, as a user would
	r.run("pods.remove", key(keyPod+r.podID(pod)))
	r.ext.answer(r.asked(), 0) // the pod runs: "Force remove"
	r.wait("the pod removed", func() bool {
		_, err := h.Client.InspectPod(ctx, pod)
		return err != nil
	})
}

// removePodOfTheTest removes the pod that the test played, by its name, when it is still there. The name is the random one the test
// made (emetos-test- and 48 random bits), so it can be no pod of the user's; the label is not asked, because a pod that Kube Play made
// without it must still go (the test says so with its own message when the label did not arrive).
func removePodOfTheTest(t *testing.T, h *containerapitest.Harness, name string) {
	t.Helper()
	if !strings.HasPrefix(name, containerapitest.NamePrefix) {
		t.Fatalf("a pod of the tests is named %s...; %q is not", containerapitest.NamePrefix, name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := h.Client.InspectPod(ctx, name); err != nil {
		return // not there
	}
	if err := h.Client.RemovePod(ctx, name, true); err != nil {
		t.Errorf("cleanup: removing the pod %s: %v", name, err)
	}
}

func TestBuildImageOverTheRealPodman(t *testing.T) {
	h := containerapitest.RealPodman(t)
	ctx := h.Context(5 * time.Minute)
	h.EnsureImage(ctx, containerapitest.ImageBusyBox)
	r := realPodmanRig(t, h)
	tag := h.ImageName()
	h.RemoveImageAtEnd(tag)

	containerfile := r.file("Containerfile", "FROM "+containerapitest.ImageBusyBox+"\nLABEL dev.emetos.test=1\nCMD [\"true\"]\n")
	r.run("images.build", explorerArgs(containerfile))
	r.api.LastInput().Accept(tag)
	s := r.cliTab()
	if s.status != "Finished" {
		t.Fatalf("the build ended with %q:\n%s", s.status, strings.Join(linesOf(s), "\n"))
	}
	if lines := linesOf(s); lines[0] != "$ podman build --pull --rm -f Containerfile -t "+tag+" ." {
		t.Errorf("the command line: %q", lines[0])
	}
	info, err := h.Client.InspectImage(ctx, tag)
	if err != nil {
		t.Fatalf("the image was not built: %v", err)
	}
	if info.Labels[containerapitest.LabelKey] != containerapitest.LabelValue {
		t.Errorf("the image has no label: %v", info.Labels)
	}
}
