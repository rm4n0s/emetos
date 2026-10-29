package containerui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// The workflows of 18.3 for Podman: the same code, the program podman. Every test puts a fake podman program first on PATH, which
// records what it was run as; the program of the computer is never run.

func TestPodmanBuildImageRunsPodmanBuildAndReadsItsProblems(t *testing.T) {
	r, cli := newPodmanRig(t, samplePodman)
	cli.answer("build", "STEP 1/2: FROM busybox\nError: parsing "+"Containerfile: dockerfile parse error on line 2: unknown instruction: FRM\n", "", 125)
	containerfile := r.file("api/Containerfile", "FROM busybox\nFRM x\n")
	r.run("images.build", explorerArgs(containerfile))
	in := r.api.LastInput()
	if in.Options().Value != "api:latest" {
		t.Errorf("tag %q", in.Options().Value)
	}
	in.Accept("api:1")
	s := r.cliTab()
	if got := cli.without("system"); !reflect.DeepEqual(got, []string{"build --pull --rm -f api/Containerfile -t api:1 api"}) {
		t.Errorf("calls %q", got)
	}
	if got := linesOf(s); got[0] != "$ podman build --pull --rm -f api/Containerfile -t api:1 api" || s.status != "Exited with status 125" {
		t.Errorf("tab %q %q", got, s.status)
	}
	r.wait("the problem", func() bool { return r.api.MarkerCount() == 1 })
	m := r.api.Markers(r.cfg.buildProblemsOwner())["file://"+containerfile]
	if len(m) != 1 || m[0].StartLine != 2 || m[0].Source != "podman build" || m[0].Message != "unknown instruction: FRM" {
		t.Errorf("markers %+v", m)
	}
	if got := published(r.api, "emetos.podman.buildFinished"); len(got) != 1 || got[0]["ok"] != false {
		t.Errorf("events %v", got)
	}
}

func TestPodmanComposeUsesPodmanComposeOrPodmanDashCompose(t *testing.T) {
	r, cli := newPodmanRig(t, samplePodman)
	p := r.file("stack/compose.yaml", "services:\n  web:\n    image: nginx\n")
	cli.answer("compose_up", "started\n", "", 0)

	r.run("compose.up", explorerArgs(p))
	r.cliTab()
	if got := cli.without("system"); !reflect.DeepEqual(got, []string{"compose version", "compose -f stack/compose.yaml up -d --build"}) {
		t.Errorf("calls %q", got)
	}

	// `podman compose` that has no provider fails; podman-compose is the way out
	cli.answer("compose_version", "", "Error: looking up compose provider failed\n", 125)
	r.ext.composeBase = nil
	r.run("compose.up", explorerArgs(p))
	r.wait("the note", func() bool {
		return r.hasNote("Cannot run Compose: Podman Compose was not found: install the compose plugin of podman (or podman-compose)")
	})
	_, err := r.ext.resolveCompose(r.ext.ctx)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("ext.resolveCompose.ContainerUIComposeNotFound") {
		t.Errorf("the route: %v", err)
	}
	cli.alias("podman-compose")
	r.run("compose.up", explorerArgs(p))
	r.wait("the fallback ran", func() bool {
		got := cli.lines()
		return strings.HasPrefix(got[len(got)-1], "(docker-compose)") || strings.Contains(got[len(got)-1], "-f stack/compose.yaml up -d --build") && len(got) > 3
	})
	got := cli.lines()
	// the fake script marks only docker-compose; podman-compose runs through the same script as "compose ..."
	if last := got[len(got)-1]; !strings.HasSuffix(last, "-f stack/compose.yaml up -d --build") {
		t.Errorf("calls %q", got)
	}
}

func TestPodmanOpenShellAndRunInteractiveTypeTheCommandInTheTerminal(t *testing.T) {
	r, _ := newPodmanRig(t, samplePodman)
	term := &terminalDouble{}
	term.install(r.api)
	r.fake.SetExec(func([]string) (string, string, int) { return "/bin/bash\n", "", 0 })
	id := r.id("web-nginx")
	r.run("containers.attachShell", key(keyOfContainer(id)))
	r.wait("typed", func() bool { return len(term.lines()) == 1 })
	if want := "podman exec -it " + containerapi.ShortID(id) + " bash"; term.lines()[0] != want {
		t.Errorf("typed %q, want %q", term.lines()[0], want)
	}
	r.run("images.runInteractive", key(r.imageKey("docker.io/library/nginx:1.27")))
	r.wait("typed", func() bool { return len(term.lines()) == 2 })
	if got := term.lines()[1]; !strings.HasPrefix(got, "podman run --rm -it ") || !strings.HasSuffix(got, " docker.io/library/nginx:1.27") {
		t.Errorf("typed %q", got)
	}
	// with an engine the user named, the program is told where it is
	r.ext.pickedHost = "unix:///run/user/1000/podman/podman.sock"
	r.run("containers.attachShell", key(keyOfContainer(id)))
	r.wait("typed", func() bool { return len(term.lines()) == 3 })
	if want := "podman --url unix:///run/user/1000/podman/podman.sock exec -it " + containerapi.ShortID(id) + " bash"; term.lines()[2] != want {
		t.Errorf("typed %q, want %q", term.lines()[2], want)
	}
}

func TestPodmanPushAndTheEnvironmentOfTheCLI(t *testing.T) {
	r, cli := newPodmanRig(t, samplePodman)
	r.ext.pickedHost = "unix:///run/user/1000/podman/podman.sock"
	r.run("images.push", key(r.imageKey("docker.io/library/nginx:1.27")))
	r.api.LastInput().Accept("docker.io/library/nginx:1.27")
	r.cliTab()
	if got := cli.without("system"); !reflect.DeepEqual(got, []string{"push docker.io/library/nginx:1.27"}) {
		t.Errorf("calls %q", got)
	}
}

func TestAddPodmanFilesWritesAContainerfile(t *testing.T) {
	r, _ := newPodmanRig(t, samplePodman)
	r.run("configure", nil)
	pickLabel(t, r, "Go")
	r.api.LastInput().Accept("8080")
	pickLabel(t, r, "No")
	if df := r.text("Containerfile"); !strings.Contains(df, "FROM golang:alpine") || r.text("Dockerfile") != "" {
		t.Errorf("Containerfile:\n%q", df)
	}
	if !r.hasNote("Added .dockerignore, Containerfile.") {
		t.Errorf("notes %v", r.notes())
	}
	titles := map[string]string{}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		titles[c.ID] = c.Title
	}
	if titles["podman.configure"] != "Add Podman Files to Workspace..." {
		t.Errorf("title %q", titles["podman.configure"])
	}
}

func TestPodmanComposeGroupOfAPodmanComposeProject(t *testing.T) {
	r, cli := newPodmanRig(t, func(t testing.TB, f *containerapitest.Fake) {
		samplePodman(t, f)
		dir := t.TempDir()
		f.AddContainer("shop-db-1", "docker.io/library/nginx:1.27", map[string]string{
			containerapi.LabelPodmanCompose: "shop", containerapi.LabelCompose: "shop",
			labelComposeWorkingDir: dir, labelComposeFiles: dir + "/compose.yaml"}, "running")
	})
	r.run("containers.composeGroup.logs", key(keyGroup+"shop"))
	r.cliTab()
	if got := cli.without("system"); len(got) < 2 || !strings.Contains(got[len(got)-1], "-p shop") {
		t.Errorf("calls %q", got)
	}
}

// TestPodmanBuildFailureOfAStepMarksTheLineOfTheInstruction holds that Buildah's "building at STEP" error, which has no line, marks
// the line of that instruction in the Containerfile.
func TestPodmanBuildFailureOfAStepMarksTheLineOfTheInstruction(t *testing.T) {
	r, cli := newPodmanRig(t, samplePodman)
	cli.answer("build", "STEP 1/3: FROM busybox\nSTEP 2/3: COPY a /a\nSTEP 3/3: RUN nonexistent --flag\nError: building at STEP \"RUN nonexistent --flag\": while running runtime: exit status 127\n", "", 127)
	containerfile := r.file("Containerfile", "FROM busybox\n# a comment\nCOPY a /a\nRUN nonexistent --flag\n")
	r.run("images.build", explorerArgs(containerfile))
	r.api.LastInput().Accept("x:1")
	r.cliTab()
	r.wait("the problem", func() bool { return r.api.MarkerCount() == 1 })
	m := r.api.Markers(r.cfg.buildProblemsOwner())["file://"+containerfile]
	if len(m) != 1 || m[0].StartLine != 4 || m[0].EndColumn != len("RUN nonexistent --flag")+1 || !strings.Contains(m[0].Message, `the step "RUN nonexistent --flag" failed: while running runtime: exit status 127`) {
		t.Errorf("markers %+v", m)
	}
}
