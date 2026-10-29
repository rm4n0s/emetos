package containerui

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// terminalDouble stands for the Terminal extension: it answers the two requests of Open Shell and keeps what it was asked.
type terminalDouble struct {
	mu      sync.Mutex
	created []map[string]any
	ran     []map[string]any
}

func (d *terminalDouble) install(a *fakeapi.API) {
	fakeapi.OnRequestValue(a, terminalCreate, func(_ context.Context, p map[string]any) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.created = append(d.created, p)
		return map[string]any{"id": 7}, nil
	})
	fakeapi.OnRequestValue(a, terminalRun, func(_ context.Context, p map[string]any) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.ran = append(d.ran, p)
		return map[string]any{"id": 7}, nil
	})
}

func (d *terminalDouble) lines() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, p := range d.ran {
		out = append(out, p["text"].(string))
	}
	return out
}

func TestOpenShellTypesTheCommandInTheTerminalExtension(t *testing.T) {
	newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	term := &terminalDouble{}
	term.install(r.api)
	id := r.id("shop-web-1")
	hasBash := true
	r.fake.SetExec(func(cmd []string) (string, string, int) {
		if strings.Contains(strings.Join(cmd, " "), "which bash") && !hasBash {
			return "", "", 1
		}
		return "/bin/bash\n", "", 0
	})

	r.run("containers.attachShell", key(keyOfContainer(id)))
	r.wait("the command typed", func() bool { return len(term.lines()) == 1 })
	if want := "docker exec -it " + containerapi.ShortID(id) + " bash"; term.lines()[0] != want {
		t.Errorf("typed %q, want %q", term.lines()[0], want)
	}
	if c := term.created[0]; c["name"] != "Shell: shop-web-1" || c["show"] != true {
		t.Errorf("the terminal was asked for as %v", c)
	}
	if term.ran[0]["id"] != float64(7) {
		t.Errorf("typed in %v", term.ran[0]["id"])
	}

	hasBash = false
	r.run("containers.attachShell", key(keyOfContainer(id)))
	r.wait("the second command", func() bool { return len(term.lines()) == 2 })
	if want := "docker exec -it " + containerapi.ShortID(id) + " sh"; term.lines()[1] != want {
		t.Errorf("typed %q, want %q", term.lines()[1], want)
	}
	if len(r.api.Notes()) != 0 {
		t.Errorf("notes %v", r.notes())
	}

	// the menu of a running container has it, that of a stopped one has not (the labels are in sections_test)
	for _, it := range r.view(secContainers).sec.menu(r.view(secContainers), []string{keyOfContainer(r.id("scratch"))}) {
		if it.Label == "Open Shell" {
			t.Error("Open Shell is offered for a stopped container")
		}
	}
}

func TestOpenShellWithNoTerminalGivesTheCommandLine(t *testing.T) {
	newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	id := r.id("shop-web-1")
	r.run("containers.attachShell", key(keyOfContainer(id)))
	want := "docker exec -it " + containerapi.ShortID(id)
	r.wait("the note", func() bool { return r.hasNote("Cannot open a terminal:") && r.hasNote(want) })
	if clip := r.api.ClipboardTexts(); len(clip) != 1 || !strings.HasPrefix(clip[0], want) {
		t.Errorf("clipboard %q", clip)
	}
}

func TestOpenShellNeedsTheProgramAndAnEngine(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := newRig(t, sampleEngine)
	r.run("containers.attachShell", key(keyOfContainer(r.id("shop-web-1"))))
	if !r.hasNote("Cannot open a shell: the program docker is not on PATH") {
		t.Errorf("notes %v", r.notes())
	}
}

func TestRunInteractiveOpensATerminalWithThePortsOfTheImage(t *testing.T) {
	newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	term := &terminalDouble{}
	term.install(r.api)
	img := r.ext.images[0]
	row := keyImage + img.ID + ":" + img.RepoTags[0]
	r.run("images.runInteractive", key(row))
	r.wait("the command typed", func() bool { return len(term.lines()) == 1 })
	if want := "docker run --rm -it -p 80:80/tcp " + img.RepoTags[0]; term.lines()[0] != want {
		t.Errorf("typed %q, want %q", term.lines()[0], want)
	}
	if c := term.created[0]; c["name"] != "Run: "+img.RepoTags[0] {
		t.Errorf("name %v", c["name"])
	}
}

func TestPushRunsTheCLIAndTagsFirstWhenTheNameChanges(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	cli.answer("push", "The push refers to repository\n", "", 0)
	r := newRig(t, sampleEngine)
	img := r.ext.images[0]
	ref := img.RepoTags[0]
	row := keyImage + img.ID + ":" + ref

	r.run("images.push", key(row))
	in := r.api.LastInput()
	if in == nil || in.Options().Value != ref || in.Validate("bad name") == "" {
		t.Fatalf("input %+v", in)
	}
	in.Cancel()
	r.settle()
	if len(cli.lines()) != 0 {
		t.Fatal("a push ran after Escape")
	}

	r.run("images.push", key(row))
	r.api.LastInput().Accept(ref)
	s := r.cliTab()
	if got := cli.lines(); len(got) != 1 || got[0] != "push "+ref {
		t.Errorf("calls %q", got)
	}
	if s.title != "Push "+ref || !r.hasNote("Pushed "+ref+".") {
		t.Errorf("title %q, notes %v", s.title, r.notes())
	}
	if hasRequest(r.fake, "POST", "/tag") {
		t.Error("the image was tagged for the same name")
	}

	cli.answer("push", "", "denied: requested access to the resource is denied\n", 1)
	r.run("images.push", key(row))
	r.api.LastInput().Accept("registry.example.com/team/app:1.0")
	r.wait("the second tab", func() bool { return len(r.ext.logs.sessions) == 2 && r.ext.logs.sessions[1].ended })
	r.settle()
	if !hasRequest(r.fake, "POST", "/tag") {
		t.Error("the image was not tagged with the new name")
	}
	got := cli.lines()
	if got[len(got)-1] != "push registry.example.com/team/app:1.0" {
		t.Errorf("calls %q", got)
	}
	if !r.hasNote("The push of registry.example.com/team/app:1.0 failed.") {
		t.Errorf("notes %v", r.notes())
	}
}

func TestPushOfAnImageWithNoNameAsksToTagItFirst(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	var dangling containerapi.Image
	for _, im := range r.ext.images {
		if len(im.RepoTags) == 0 {
			dangling = im
		}
	}
	r.run("images.push", key(keyImage+dangling.ID+":"))
	if !r.hasNote("This image has no name. Tag it first") || r.api.LastInput() != nil || len(cli.lines()) != 0 {
		t.Errorf("notes %v", r.notes())
	}
}

func TestTheJSONOfAnExplorerArgumentIsRead(t *testing.T) {
	var args any
	if err := json.Unmarshal([]byte(`{"uri":"file:///w/Dockerfile","uris":["file:///w/Dockerfile","file:///w/b/Dockerfile"]}`), &args); err != nil {
		t.Fatal(err)
	}
	newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	var paths []string
	r.ext.pickFiles(args, "Dockerfile", isDockerfile, "dockerfile", func(p []string) { paths = p })
	if len(paths) != 2 || paths[0] != "/w/Dockerfile" || paths[1] != "/w/b/Dockerfile" {
		t.Errorf("paths %v", paths)
	}
}
