package containerui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

const kubeYAML = `# Save the output of this file and use kubectl create -f to import
# it into Kubernetes.
#
# Created with podman-5.8.0
apiVersion: v1
kind: Pod
metadata:
  creationTimestamp: "2026-10-29T10:00:00Z"
  labels:
    app: web
  name: web
spec:
  containers:
  - image: docker.io/library/nginx:1.27
    name: nginx
`

func TestParseKubeNames(t *testing.T) {
	for _, tt := range []struct {
		name, in string
		want     []kubeResource
	}{
		{"a pod", kubeYAML, []kubeResource{{"Pod", "web"}}},
		{"a pod, a deployment and a service in one stream", `apiVersion: v1
kind: Service
metadata:
  name: web-svc
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: api
  name: "api"   # quoted, with a comment
spec:
  template:
    metadata:
      name: not-this-one
---
kind: Pod
metadata:
    name: 'second'
`, []kubeResource{{"Service", "web-svc"}, {"Deployment", "api"}, {"Pod", "second"}}},
		{"a document with no name", "kind: Pod\nmetadata:\n  labels:\n    a: b\n", nil},
		{"nothing", "", nil},
		{"not Kubernetes", "services:\n  web:\n    image: x\n", nil},
		{"windows line ends", "kind: Pod\r\nmetadata:\r\n  name: w\r\n", []kubeResource{{"Pod", "w"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseKubeNames(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateKubeOpensTheYAMLInAnUntitledEditor(t *testing.T) {
	r, cli := newPodmanRig(t, samplePodman)
	cli.answer("kube_generate", kubeYAML, "Warning: something on stderr\n", 0)
	t.Run("a container", func(t *testing.T) {
		r.run("containers.generateKube", key(keyOfContainer(r.id("web-nginx"))))
		r.wait("the document", func() bool { return len(r.api.UntitledDocuments()) == 1 })
		doc := r.api.UntitledDocuments()[0]
		if doc.LanguageID != "yaml" || doc.Text != kubeYAML {
			t.Errorf("the document: %q %q", doc.LanguageID, doc.Text)
		}
		if got := cli.without("system"); !reflect.DeepEqual(got, []string{"kube generate web-nginx"}) {
			t.Errorf("calls %v", got)
		}
	})
	t.Run("a pod", func(t *testing.T) {
		r.run("pods.generateKube", key(keyPod+r.podID("web")))
		r.wait("the second document", func() bool { return len(r.api.UntitledDocuments()) == 2 })
		if got := cli.without("system"); got[len(got)-1] != "kube generate web" {
			t.Errorf("calls %v", got)
		}
	})
	t.Run("several containers are one file", func(t *testing.T) {
		r.view(secContainers).tree.SetSelection([]string{keyOfContainer(r.id("web-nginx")), keyOfContainer(r.id("web-redis"))})
		r.run("containers.generateKube", nil)
		r.wait("the third document", func() bool { return len(r.api.UntitledDocuments()) == 3 })
		if got := cli.without("system"); got[len(got)-1] != "kube generate web-nginx web-redis" {
			t.Errorf("calls %v", got)
		}
	})
	t.Run("the menu of a container offers it", func(t *testing.T) {
		labels := menuLabels(r.view(secContainers).sec.menu(r.view(secContainers), []string{keyOfContainer(r.id("web-nginx"))}))
		for _, want := range []string{"Generate Kube YAML", "Generate Quadlet...", "Create Pod from Selected Containers..."} {
			if !containsString(labels, want) {
				t.Errorf("no %q in %v", want, labels)
			}
		}
	})
	t.Run("an editor that cannot open falls back to a read-only tab", func(t *testing.T) {
		r.api.FailUntitled(errors.New("ExtensionDocumentUnavailable", "no editor"))
		before := len(r.api.EditorTabs())
		r.run("pods.generateKube", key(keyPod+r.podID("batch")))
		r.wait("the tab", func() bool { return len(r.api.EditorTabs()) > before })
		var st docState
		if err := json.Unmarshal(r.api.EditorTabs()[before].State(), &st); err != nil {
			t.Fatal(err)
		}
		if st.Language != "yaml" || st.Text != kubeYAML || !strings.HasSuffix(st.Title, "(kube)") {
			t.Errorf("the tab: %+v", st)
		}
		r.api.FailUntitled(nil)
	})
}

func TestGenerateKubeFailures(t *testing.T) {
	for _, tt := range []struct {
		name, out, err string
		code           int
		note           string
	}{
		{"the program fails", "", "Error: no container with name or ID \"web-nginx\" found\n", 125, "Cannot generate Kube YAML for web-nginx: Error: no container with name or ID"},
		{"it fails without a message", "", "", 3, "the command ended with status 3"},
		{"it prints nothing", "\n", "", 0, "Podman generated nothing for web-nginx."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, cli := newPodmanRig(t, samplePodman)
			cli.answer("kube_generate", tt.out, tt.err, tt.code)
			r.run("containers.generateKube", key(keyOfContainer(r.id("web-nginx"))))
			r.wait("the message", func() bool { return r.hasNote(tt.note) })
			if len(r.api.UntitledDocuments()) != 0 {
				t.Errorf("a document was opened: %v", r.api.UntitledDocuments())
			}
		})
	}
	t.Run("without the program", func(t *testing.T) {
		r, _ := newPodmanRig(t, samplePodman)
		t.Setenv("FAKE_CLI_DIR", "")
		r.run("containers.generateKube", key(keyOfContainer(r.id("web-nginx"))))
		if !r.hasNote("Cannot generate Kube YAML: the program podman is not on PATH") {
			t.Errorf("notes %v", r.notes())
		}
	})
}

func TestPlayKubeRunsThePodmanProgramInATab(t *testing.T) {
	r, cli := newPodmanRig(t, samplePodman)
	cli.answer("kube_play", "Pod:\nabc\nContainers:\ndef\n", "", 0)
	file := r.file("k8s/pod.yaml", kubeYAML)
	r.file("compose.yaml", "services: {}\n")

	r.run("kube.play", explorerArgs(file))
	s := r.cliTab()
	if got := cli.without("system"); !reflect.DeepEqual(got, []string{"kube play k8s/pod.yaml"}) {
		t.Errorf("calls %v", got)
	}
	if c := cli.calls()[len(cli.calls())-1]; c.cwd != r.folderOf() {
		t.Errorf("run in %s", c.cwd)
	}
	got := linesOf(s)
	if got[0] != "$ podman kube play k8s/pod.yaml" || !strings.Contains(strings.Join(got, "\n"), "Containers:") || s.title != "Kube Play: k8s/pod.yaml" || s.status != "Finished" {
		t.Errorf("tab %q %q %q", s.title, s.status, got)
	}
	r.wait("the notice", func() bool { return r.hasNote("Played k8s/pod.yaml.") })

	t.Run("a failure says where to look", func(t *testing.T) {
		cli.answer("kube_play", "", "Error: image not found\n", 125)
		r.run("kube.play", explorerArgs(file))
		r.wait("the failure", func() bool {
			return r.hasNote("Kube Play of k8s/pod.yaml failed. The output is in the Podman Logs tab.")
		})
	})
	t.Run("the palette offers the YAML files that are not Compose files", func(t *testing.T) {
		r.file("other.yml", "a: b\n")
		r.run("kube.play", nil)
		r.api.Pump()
		pick := r.api.LastPick()
		if pick == nil || !reflect.DeepEqual(pick.Labels(), []string{"k8s/pod.yaml", "other.yml"}) {
			t.Fatalf("pick %+v", pick)
		}
	})
}

func TestKubeDownAsksFirst(t *testing.T) {
	r, cli := newPodmanRig(t, samplePodman)
	file := r.file("web.yaml", kubeYAML) // the pod "web"
	r.fake.ResetRequests()

	r.run("kube.down", explorerArgs(file))
	q := r.asked()
	if q.title != "Take 1 pod down?" || !strings.Contains(strings.Join(q.lines, "\n"), "web") || !strings.Contains(strings.Join(q.lines, "\n"), "2 containers go with it. Volumes are kept.") ||
		!strings.Contains(strings.Join(q.lines, "\n"), "1 pod running.") {
		t.Errorf("the question: %q %q", q.title, q.lines)
	}
	if got := choiceLabels(q); !reflect.DeepEqual(got, []string{"Take down 1 pod"}) {
		t.Errorf("choices %v", got)
	}
	r.settle()
	if got := cli.without("system"); len(got) != 0 {
		t.Fatalf("the program was run before an answer: %v", got)
	}
	r.ext.cancelConfirm()
	r.settle()
	if got := cli.without("system"); len(got) != 0 || len(destructive(r.fake)) != 0 {
		t.Fatalf("Cancel ran %v and sent %v", got, destructive(r.fake))
	}
	r.run("kube.down", explorerArgs(file))
	r.ext.answer(r.asked(), 0)
	s := r.cliTab()
	if got := cli.without("system"); !reflect.DeepEqual(got, []string{"kube down web.yaml"}) || s.title != "Kube Down: web.yaml" {
		t.Errorf("calls %v, tab %q", got, s.title)
	}
	r.wait("the notice", func() bool { return r.hasNote("Took web.yaml down.") })
}

func TestKubeDownNamesThePodsOfTheFile(t *testing.T) {
	r, cli := newPodmanRig(t, samplePodman)
	deployment := r.file("dep.yaml", "kind: Deployment\nmetadata:\n  name: batch\n")
	r.run("kube.down", explorerArgs(deployment))
	// a deployment "batch" makes a pod "batch-pod", which this engine does not have: no pod of the file is here
	if !r.hasNote("No pod of dep.yaml is in Podman.") || r.ext.confirm != nil {
		t.Errorf("notes %v, question %v", r.notes(), r.ext.confirm)
	}
	if _, err := r.fake.Client().CreatePod(testCtx, podSpecNamed("batch-pod-1")); err != nil {
		t.Fatal(err)
	}
	r.wait("the new pod", func() bool { return containsString(r.labels(secPods), "batch-pod-1") })
	r.run("kube.down", explorerArgs(deployment))
	if q := r.asked(); !strings.Contains(strings.Join(q.lines, "\n"), "batch-pod-1") {
		t.Errorf("the question: %q", q.lines)
	}
	r.ext.cancelConfirm()
	if got := cli.without("system"); len(got) != 0 {
		t.Errorf("calls %v", got)
	}
	t.Run("a file that cannot be read", func(t *testing.T) {
		r.run("kube.down", explorerArgs(r.folderOf()+"/missing.yaml"))
		if !r.hasNote("Cannot read missing.yaml: ") {
			t.Errorf("notes %v", r.notes())
		}
	})
	t.Run("the route of a read that fails", func(t *testing.T) {
		_, err := readSmallFile(r.folderOf() + "/missing.yaml")
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("readSmallFile.ContainerUIFileUnreadable") {
			t.Errorf("error %v", err)
		}
		big := r.file("big.yaml", strings.Repeat("x: y\n", maxKubeFile/4))
		_, err = readSmallFile(big)
		appErr, ok = errors.FromError(err)
		if !ok || !appErr.HasRoute("readSmallFile.ContainerUIFileUnreadable") || !strings.Contains(appErr.Message, "larger than 1 MiB") {
			t.Errorf("error %v", err)
		}
	})
}
