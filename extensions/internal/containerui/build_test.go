package containerui

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// folderOf is the folder of the workspace of the rig, with the links of its path resolved (the fake program reports the folder it is
// run in the same way).
func (r *rig) folderOf() string {
	r.t.Helper()
	f, err := filepath.EvalSymlinks(r.api.Scope().Folder)
	if err != nil {
		r.t.Fatal(err)
	}
	return f
}

// file writes a file of the workspace and returns its path.
func (r *rig) file(rel, text string) string {
	r.t.Helper()
	p := filepath.Join(r.api.Scope().Folder, filepath.FromSlash(rel))
	mustWrite(r.t, p, text)
	r.api.RescanFiles()
	return p
}

// explorerArgs is the argument of a command run from the context menu of the Explorer.
func explorerArgs(paths ...string) map[string]any {
	uris := make([]any, len(paths))
	for i, p := range paths {
		uris[i] = "file://" + p
	}
	return map[string]any{"uri": uris[0], "uris": uris}
}

// cliTab waits for the tab of a run of the CLI to end and returns it.
func (r *rig) cliTab() *logSession {
	r.t.Helper()
	var s *logSession
	r.wait("the tab of the run", func() bool {
		for _, o := range r.ext.logs.sessions {
			if o.kind == sessionCLI {
				s = o
			}
		}
		return s != nil
	})
	r.wait("the run to end", func() bool { return s.ended })
	return s
}

// published lists the events of a topic the extension published.
func published(a *fakeapi.API, topic string) []map[string]any {
	var out []map[string]any
	for _, p := range a.PublishedEvents() {
		if p.Topic == topic {
			var m map[string]any
			_ = json.Unmarshal(p.Data, &m)
			out = append(out, m)
		}
	}
	return out
}

func TestBuildImageRunsTheCLIAndStreamsItsOutputIntoATab(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	cli.answer("build", "#1 building\nSuccessfully built abc\n", "a warning on stderr\n", 0)
	r := newRig(t, sampleEngine)
	dockerfile := r.file("api/Dockerfile", "FROM busybox\n")

	r.run("images.build", explorerArgs(dockerfile))
	in := r.api.LastInput()
	if in == nil {
		t.Fatal("the tag of the image is not asked")
	}
	if want := "api:latest"; in.Options().Value != want {
		t.Errorf("the suggested tag is %q, want %q", in.Options().Value, want)
	}
	if in.Validate("has space") == "" || in.Validate("") == "" || in.Validate("app:1") != "" {
		t.Error("the tag is not checked")
	}
	if len(cli.calls()) != 0 {
		t.Fatal("the build started before the tag was given")
	}
	in.Accept("demo:1")
	s := r.cliTab()

	calls := cli.calls()
	if len(calls) != 1 {
		t.Fatalf("calls %v", cli.lines())
	}
	if want := []string{"build", "--pull", "--rm", "-f", "api/Dockerfile", "-t", "demo:1", "api"}; !reflect.DeepEqual(calls[0].args, want) {
		t.Errorf("the command line is %q, want %q", calls[0].args, want)
	}
	if calls[0].cwd != r.folderOf() {
		t.Errorf("the build runs in %s, want %s", calls[0].cwd, r.folderOf())
	}
	got := linesOf(s)
	if len(got) < 3 || got[0] != "$ docker build --pull --rm -f api/Dockerfile -t demo:1 api" {
		t.Errorf("lines %q", got)
	}
	if !strings.Contains(strings.Join(got, "\n"), "Successfully built abc") || !strings.Contains(strings.Join(got, "\n"), "a warning on stderr") {
		t.Errorf("the output is not in the tab: %q", got)
	}
	var stderr bool
	for _, l := range s.lines {
		stderr = stderr || (l.stderr && l.text == "a warning on stderr")
	}
	if !stderr {
		t.Error("what the program wrote to stderr is not marked")
	}
	if s.title != "Build demo:1" || s.status != "Finished" {
		t.Errorf("title %q status %q", s.title, s.status)
	}
	if !r.hasNote("Built demo:1.") {
		t.Errorf("notes %v", r.notes())
	}
	ev := published(r.api, r.cfg.BuildFinishedTopic())
	if len(ev) != 1 || ev[0]["image"] != "demo:1" || ev[0]["ok"] != true {
		t.Errorf("events %v", ev)
	}
	if r.api.MarkerCount() != 0 {
		t.Error("a build that worked left problems")
	}

	// the tag is remembered for the file
	r.run("images.build", explorerArgs(dockerfile))
	if got := r.api.LastInput().Options().Value; got != "demo:1" {
		t.Errorf("the second suggestion is %q", got)
	}
	r.api.LastInput().Cancel()
	if len(cli.calls()) != 1 {
		t.Error("a build ran after the box was closed")
	}
}

func TestBuildImageUsesTheContextSettingAndTheDocumentInFront(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	p := r.file("Dockerfile", "FROM busybox\n")
	r.api.SetSetting("containers.imageBuildContextPath", "ctx")
	r.api.SetActive(&extension.ActiveDocument{URI: "file://" + p, LanguageID: "dockerfile"})

	r.run("images.build", nil) // the editor's context menu gives no argument
	r.api.LastInput().Accept("front:dev")
	r.cliTab()
	if got := cli.lines(); len(got) != 1 || got[0] != "build --pull --rm -f Dockerfile -t front:dev ctx" {
		t.Errorf("calls %q", got)
	}
}

func TestBuildImageChoosesAmongTheDockerfilesOfTheWorkspace(t *testing.T) {
	newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	r.run("images.build", nil)
	if !r.hasNote("There is no Dockerfile in the workspace.") {
		t.Errorf("notes %v", r.notes())
	}
	r.file("web/Dockerfile", "FROM busybox\n")
	r.file("README.md", "x")
	r.run("images.build", nil)
	if in := r.api.LastInput(); in == nil || !strings.Contains(in.Options().Prompt, "web/Dockerfile") {
		t.Fatalf("one Dockerfile is used without a pick: %+v", in)
	}
	r.api.LastInput().Cancel()
	r.file("worker/Dockerfile.dev", "FROM busybox\n")
	r.run("images.build", nil)
	pick := r.api.LastPick()
	if pick == nil || !reflect.DeepEqual(pick.Labels(), []string{"web/Dockerfile", "worker/Dockerfile.dev"}) {
		t.Fatalf("pick %+v", pick)
	}
	pick.Choose(1)
	if in := r.api.LastInput(); in == nil || in.Options().Value != "worker:latest" {
		t.Errorf("the box after the pick: %+v", in)
	}
}

func TestBuildErrorsGoToProblemsAndAreCleared(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	cli.answer("build", "", `#5 [2/3] RUN nonexistent
Dockerfile:3
--------------------
   1 |     FROM busybox
   3 | >>> RUN nonexistent
--------------------
ERROR: failed to solve: process "/bin/sh -c nonexistent" did not complete successfully: exit code: 127
1 warning found (use docker --debug to expand):
 - FromAsCasing: 'as' and 'FROM' keywords' casing do not match (line 1)
`, 1)
	r := newRig(t, sampleEngine)
	dockerfile := r.file("Dockerfile", "FROM busybox AS base\n\nRUN nonexistent\n")

	r.run("images.build", explorerArgs(dockerfile))
	r.api.LastInput().Accept("bad:1")
	s := r.cliTab()
	if s.status != "Exited with status 1" {
		t.Errorf("status %q", s.status)
	}
	markers := r.api.Markers(r.cfg.buildProblemsOwner())["file://"+dockerfile]
	if len(markers) != 2 {
		t.Fatalf("markers %+v", r.api.Markers(r.cfg.buildProblemsOwner()))
	}
	m := markers[0]
	if m.Severity != extension.SeverityError || m.StartLine != 3 || m.StartColumn != 1 || m.EndColumn != 16 || m.Source != "docker build" ||
		!strings.HasPrefix(m.Message, `process "/bin/sh -c nonexistent" did not complete`) {
		t.Errorf("the error marker %+v", m)
	}
	if w := markers[1]; w.Severity != extension.SeverityWarning || w.StartLine != 1 || !strings.HasPrefix(w.Message, "FromAsCasing") {
		t.Errorf("the warning marker %+v", w)
	}
	if !r.hasNote("The build of bad:1 failed (status 1). 2 problems in Problems.") {
		t.Errorf("notes %v", r.notes())
	}
	if ev := published(r.api, r.cfg.BuildFinishedTopic()); len(ev) != 1 || ev[0]["ok"] != false {
		t.Errorf("events %v", ev)
	}

	// the next build starts with no problems; this one worked
	cli.answer("build", "done\n", "", 0)
	r.run("images.build", explorerArgs(dockerfile))
	r.api.LastInput().Accept("bad:1")
	r.wait("the second run", func() bool { return len(published(r.api, r.cfg.BuildFinishedTopic())) == 2 })
	if r.api.MarkerCount() != 0 {
		t.Errorf("the markers of the last build stayed: %+v", r.api.Markers(r.cfg.buildProblemsOwner()))
	}
}

func TestClosingTheTabStopsTheBuild(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	cli.sleep("build", 60)
	r := newRig(t, sampleEngine)
	dockerfile := r.file("Dockerfile", "FROM busybox\n")
	r.run("images.build", explorerArgs(dockerfile))
	r.api.LastInput().Accept("slow:1")
	r.wait("the build to run", func() bool { return cli.started("build") })
	s := r.ext.logs.active
	if s == nil || s.ended || !strings.Contains(s.status, "Close the tab to stop it") {
		t.Fatalf("the tab %+v", s)
	}
	r.run("logs.close", nil)
	r.wait("the run to end", func() bool { return s.ended })
	r.settle()
	if len(r.api.Notes()) != 0 {
		t.Errorf("a stopped build says something: %v", r.notes())
	}
	if len(published(r.api, r.cfg.BuildFinishedTopic())) != 0 {
		t.Error("a stopped build published its end")
	}
}

func TestBuildNeedsTheProgramOfTheEngineAndTellsHowToGetIt(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := newRig(t, sampleEngine)
	p := r.file("Dockerfile", "FROM busybox\n")
	r.run("images.build", explorerArgs(p))
	if !r.hasNote("the program docker is not on PATH") {
		t.Errorf("notes %v", r.notes())
	}
	if in := r.api.LastInput(); in != nil {
		t.Error("the tag was asked for a build that cannot run")
	}
	_, err := r.ext.findCLI()
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ext.findCLI.ContainerUICLINotFound") {
		t.Errorf("route of the missing program: %v", err)
	}
}

func TestBuildWithNoFolderSaysSo(t *testing.T) {
	newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	r.api.SetActive(&extension.ActiveDocument{URI: "file:///elsewhere/Dockerfile", LanguageID: "dockerfile"})
	r.ext.api = noFolderAPI{r.api}
	r.run("images.build", nil)
	if !r.hasNote("To build an image you must first open a folder.") {
		t.Errorf("notes %v", r.notes())
	}
}

func TestBuildImageWithAGivenTagAsksNothing(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	p := r.file("Dockerfile", "FROM busybox\n")
	args := explorerArgs(p)
	args["tag"] = "given:2"
	r.run("images.build", args)
	if r.api.LastInput() != nil {
		t.Error("the tag was asked though it was given")
	}
	r.cliTab()
	if got := cli.lines(); len(got) != 1 || !strings.HasSuffix(got[0], "-t given:2 .") {
		t.Errorf("calls %q", got)
	}
	args["tag"] = "bad tag"
	r.run("images.build", args)
	if !r.hasNote(`Cannot build with the name "bad tag"`) || len(cli.lines()) != 1 {
		t.Errorf("notes %v", r.notes())
	}
}
