package containerui

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestGoldenThePodmanViews looks at the sections of Podman, each in a view of its own, in both themes: the containers grouped under
// their pod (and a podman-compose project), the pods with their state, and the connections of a machine.
func TestGoldenThePodmanViews(t *testing.T) {
	for _, th := range uitest.Themes() {
		for _, sec := range []struct {
			name string
			size gui.Size
		}{{secContainers, gui.Size{Width: 380, Height: 250}}, {secPods, gui.Size{Width: 380, Height: 80}}, {secConnections, gui.Size{Width: 380, Height: 140}}} {
			t.Run(th.Name+"/"+sec.name, func(t *testing.T) {
				r, _ := newPodmanRigWith(t, samplePodman, connectionsJSON)
				r.ext.note.seen = true
				ts := r.show(th, sec.size, r.cfg.ViewID(sec.name))
				uitest.MatchesGolden(ts.Tester, "podman_"+sec.name+"_"+th.Name)
			})
		}
	}
}

// TestGoldenThePodRemovalQuestion looks at the question over the pods: the names, how many containers go, the running one told apart,
// and the choices.
func TestGoldenThePodRemovalQuestion(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r, _ := newPodmanRig(t, samplePodman)
			r.view(secPods).tree.SetSelection([]string{keyPod + r.podID("web"), keyPod + r.podID("batch")})
			r.run("pods.remove", nil)
			ts := r.show(th, gui.Size{Width: 380, Height: 330}, r.cfg.ViewID(secPods))
			ts.refresh()
			uitest.MatchesGolden(ts.Tester, "podman_pods_question_"+th.Name)
		})
	}
}

// TestGoldenKubeDownQuestion looks at the question before Kube Down.
func TestGoldenKubeDownQuestion(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r, _ := newPodmanRig(t, samplePodman)
			file := r.file("web.yaml", kubeYAML)
			r.run("kube.down", explorerArgs(file))
			ts := r.show(th, gui.Size{Width: 380, Height: 250}, r.cfg.ViewID(secPods))
			ts.refresh()
			uitest.MatchesGolden(ts.Tester, "podman_kube_down_question_"+th.Name)
		})
	}
}

// TestGoldenThePodmanMissingEngine looks at the Containers view of Podman when no socket answers and no service could be started.
func TestGoldenThePodmanMissingEngine(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			cli := newFakeCLI(t, "podman")
			cli.answer("system_connection", "[]", "", 0)
			fake := containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{})
			cfg := podmanConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
				return nil, errors.New("ContainerAPIEngineNotFound", "No Podman engine answered:\n- the socket of the user ($XDG_RUNTIME_DIR/podman/podman.sock): not set\n- the rootful socket (/run/podman/podman.sock): connect: no such file or directory\n- a podman system service of Emetos's own: the program podman is not on PATH")
			})
			r := startRig(t, fake, cfg)
			r.wait("the missing engine", func() bool { return r.ext.status == statusMissing })
			ts := r.show(th, gui.Size{Width: 380, Height: 200}, cfg.ViewID(secContainers))
			uitest.MatchesGolden(ts.Tester, "podman_missing_engine_"+th.Name)
		})
	}
}
