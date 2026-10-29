//go:build emetos_docker

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

// The workflows of step 18.3 against the user's Docker, with the rules of CLAUDE.md "Testing notes": the image is built from a
// Dockerfile of the test that carries the label dev.emetos.test=1, under a name emetos-test-<random>, and removed when the test ends
// (after looking at the label); the Compose project is named emetos-test-<random> by its folder, has one labelled service and a
// labelled network, and is taken down by name when the test ends. Nothing else of the engine is listed, changed or removed. Run it
// on its own:
//
//	scripts/heavy.sh --alone -- go test -p 1 -tags emetos_docker ./extensions/internal/containerui -run RealDocker -v

// realWorkflowRig is the extension over the real engine, listing only what the tests made, with the real docker program.
func realWorkflowRig(t *testing.T, h *containerapitest.Harness) *rig {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("the docker program is not on PATH")
	}
	cfg := testConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: h.Client, Source: "the test"}, nil
	})
	cfg.ListFilters = ListFilters{
		Containers: containerapitest.LabelFilter(), Networks: containerapitest.LabelFilter(), Volumes: containerapitest.LabelFilter(),
		Images: containerapitest.LabelFilter(),
	}
	cfg.Debounce, cfg.EventsRetry = 20*time.Millisecond, 50*time.Millisecond
	r := startRig(t, nil, cfg)
	r.wait("the first lists", func() bool { return r.ext.status == statusConnected && r.ext.loads[resContainers].loaded })
	return r
}

func TestBuildImageOverTheRealDocker(t *testing.T) {
	h := containerapitest.RealDocker(t)
	ctx := h.Context(5 * time.Minute)
	h.EnsureImage(ctx, containerapitest.ImageBusyBox)
	r := realWorkflowRig(t, h)
	tag := h.ImageName()
	h.RemoveImageAtEnd(tag)

	dockerfile := r.file("Dockerfile", "FROM "+containerapitest.ImageBusyBox+"\nLABEL dev.emetos.test=1\nCMD [\"true\"]\n")
	r.run("images.build", explorerArgs(dockerfile))
	r.api.LastInput().Accept(tag)
	s := r.cliTab()
	if s.status != "Finished" {
		t.Fatalf("the build ended with %q:\n%s", s.status, strings.Join(linesOf(s), "\n"))
	}
	if lines := linesOf(s); lines[0] != "$ docker build --pull --rm -f Dockerfile -t "+tag+" ." {
		t.Errorf("the command line: %q", lines[0])
	}
	info, err := h.Client.InspectImage(ctx, tag)
	if err != nil {
		t.Fatalf("the image was not built: %v", err)
	}
	if info.Labels[containerapitest.LabelKey] != containerapitest.LabelValue {
		t.Errorf("the image has no label: %v", info.Labels)
	}
	r.wait("the image in the view", func() bool {
		for _, im := range r.ext.images {
			if im.ID == info.ID {
				return true
			}
		}
		return false
	})
	if r.api.MarkerCount() != 0 {
		t.Errorf("problems %+v", r.api.Markers(r.cfg.buildProblemsOwner()))
	}

	// a Dockerfile with a mistake: the real BuildKit's message goes to Problems with its line
	bad := r.file("bad/Dockerfile", "FROM "+containerapitest.ImageBusyBox+"\nLABEL dev.emetos.test=1\nFOO bar\n")
	r.run("images.build", explorerArgs(bad))
	r.api.LastInput().Accept(h.ImageName())
	r.wait("the second tab", func() bool { return len(r.ext.logs.sessions) == 2 && r.ext.logs.sessions[1].ended })
	r.settle()
	ms := r.api.Markers(r.cfg.buildProblemsOwner())["file://"+bad]
	if len(ms) == 0 || ms[0].StartLine != 3 || !strings.Contains(ms[0].Message, "FOO") {
		t.Errorf("the markers of the bad Dockerfile: %+v\noutput:\n%s", r.api.Markers(r.cfg.buildProblemsOwner()), strings.Join(linesOf(r.ext.logs.sessions[1]), "\n"))
	}
}

func TestComposeUpAndDownOverTheRealDocker(t *testing.T) {
	h := containerapitest.RealDocker(t)
	ctx := h.Context(5 * time.Minute)
	h.EnsureImage(ctx, containerapitest.ImageBusyBox)
	r := realWorkflowRig(t, h)
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("docker compose is not installed")
	}
	// the project is named by the folder of its file: emetos-test-<random>
	project := containerapitest.RandomName(t)
	compose := r.file(project+"/compose.yaml", `services:
  app:
    image: `+containerapitest.ImageBusyBox+`
    command: ["sh", "-c", "trap 'exit 0' TERM; while true; do sleep 1 & wait $$!; done"]
    labels:
      dev.emetos.test: "1"
networks:
  default:
    labels:
      dev.emetos.test: "1"
`)
	t.Cleanup(func() {
		// the project of this test, by its name and its file, and nothing else
		cmd := exec.Command("docker", "compose", "-p", project, "-f", compose, "down", "--volumes", "--remove-orphans")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("cleanup: docker compose down of %s: %v\n%s", project, err, out)
		}
	})

	containers := func() []containerapi.Container {
		cs, err := h.Client.ListContainers(ctx, containerapi.ListContainersOptions{All: true, Filters: containerapitest.LabelFilter()})
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	r.run("compose.up", explorerArgs(compose))
	s := r.cliTab()
	if s.status != "Finished" {
		t.Fatalf("Compose Up ended with %q:\n%s", s.status, strings.Join(linesOf(s), "\n"))
	}
	if cs := containers(); len(cs) != 1 || cs[0].State != "running" || cs[0].ComposeProject() != project {
		t.Fatalf("the containers after Compose Up: %+v", cs)
	}

	// Down asks first, with the container of the project
	r.run("compose.down", explorerArgs(compose))
	r.wait("the container in the question", func() bool {
		r.api.Pump()
		return r.ext.confirm != nil && len(r.ext.confirm.lines) == 2
	})
	c := r.ext.confirm
	if !strings.Contains(c.lines[0], "1 container of it: "+project+"-app-1") {
		t.Errorf("the question: %q", c.lines)
	}
	r.ext.cancelConfirm()
	r.settle()
	if len(containers()) != 1 {
		t.Fatal("Cancel took the project down")
	}
	r.run("compose.down", explorerArgs(compose))
	r.ext.answer(r.asked(), 0)
	r.wait("the second tab", func() bool { return len(r.ext.logs.sessions) == 2 && r.ext.logs.sessions[1].ended })
	if cs := containers(); len(cs) != 0 {
		t.Errorf("the containers after Compose Down: %+v", cs)
	}
}
