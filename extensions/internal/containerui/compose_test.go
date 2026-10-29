package containerui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// composeRig is a rig with a fake docker program and a Compose file in the workspace.
func composeRig(t *testing.T) (*rig, *fakeCLI, string) {
	t.Helper()
	cli := newFakeCLI(t, "docker")
	r := newRig(t, sampleEngine)
	p := r.file("stack/compose.yaml", "services:\n  web:\n    image: nginx\n")
	return r, cli, p
}

func TestComposeUpRunsTheCLIInTheFolderOfTheWorkspace(t *testing.T) {
	r, cli, p := composeRig(t)
	cli.answer("compose_up", "Container web Started\n", "", 0)
	r.run("compose.up", explorerArgs(p))
	s := r.cliTab()
	if got := cli.lines(); !reflect.DeepEqual(got, []string{"compose version", "compose -f stack/compose.yaml up -d --build"}) {
		t.Errorf("calls %q", got)
	}
	if c := cli.calls()[1]; c.cwd != r.folderOf() {
		t.Errorf("Compose runs in %s, want %s", c.cwd, r.folderOf())
	}
	if s.title != "Compose Up: stack/compose.yaml" || s.status != "Finished" || !strings.Contains(strings.Join(linesOf(s), "\n"), "Container web Started") {
		t.Errorf("tab %q %q %q", s.title, s.status, linesOf(s))
	}

	// the settings change the command line, and the program of Compose is asked once
	r.api.SetSetting("containers.composeDetached", false)
	r.api.SetSetting("containers.composeBuild", false)
	r.run("compose.up", explorerArgs(p))
	r.wait("the second run", func() bool { return len(cli.without("compose version")) == 2 })
	if got := cli.without("compose version"); got[1] != "compose -f stack/compose.yaml up" {
		t.Errorf("calls %q", got)
	}
	if n := len(cli.lines()) - len(cli.without("compose version")); n != 1 {
		t.Errorf("Compose was asked for %d times", n)
	}
}

func TestComposeUpFailureIsToldAndKeepsTheOutput(t *testing.T) {
	r, cli, p := composeRig(t)
	cli.answer("compose_up", "", "service \"web\" refers to undefined network x\n", 1)
	r.api.SetActive(&extension.ActiveDocument{URI: "file://" + p, LanguageID: "dockercompose"})
	r.run("compose.up", nil)
	s := r.cliTab()
	r.settle()
	if s.status != "Exited with status 1" || !r.hasNote("Compose Up of stack/compose.yaml failed.") {
		t.Errorf("status %q, notes %v", s.status, r.notes())
	}
}

func TestComposeNeedsComposeAndSaysWhatIsMissing(t *testing.T) {
	r, cli, p := composeRig(t)
	cli.answer("compose_version", "", "docker: 'compose' is not a docker command.\n", 1)
	r.run("compose.up", explorerArgs(p))
	r.wait("the note", func() bool { return r.hasNote("Cannot run Compose: Docker Compose was not found") })
	for _, l := range cli.lines() {
		if strings.Contains(l, " up") {
			t.Errorf("Compose ran: %q", l)
		}
	}
	_, err := r.ext.resolveCompose(r.ext.ctx)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ext.resolveCompose.ContainerUIComposeNotFound") {
		t.Errorf("the route of the missing Compose: %v", err)
	}

	// docker-compose is the way out
	cli.alias("docker-compose")
	r.run("compose.up", explorerArgs(p))
	s := r.cliTab()
	r.wait("the end", func() bool { return s.ended })
	got := cli.lines()
	if last := got[len(got)-1]; last != "(docker-compose) -f stack/compose.yaml up -d --build" {
		t.Errorf("calls %q", got)
	}
	if lines := linesOf(s); lines[0] != "$ docker-compose -f stack/compose.yaml up -d --build" {
		t.Errorf("the tab shows %q", lines[0])
	}
}

func TestComposeDownAsksFirstAndCancelChangesNothing(t *testing.T) {
	r, cli, p := composeRig(t)
	// the containers of the file the engine knows
	r.fake.AddContainer("stack-web-1", "nginx:1.27", map[string]string{
		containerapi.LabelCompose: "stack", labelComposeFiles: p, labelComposeWorkingDir: filepath.Dir(p)}, "running")
	r.run("containers.refresh", nil)
	r.wait("the list", func() bool {
		for _, c := range r.ext.containers {
			if c.Name() == "stack-web-1" {
				return true
			}
		}
		return false
	})
	r.run("compose.down", explorerArgs(p))
	c := r.asked()
	if c.title != "Take down the Compose project of stack/compose.yaml?" || len(c.lines) != 2 || !strings.Contains(c.lines[0], "1 container of it: stack-web-1") {
		t.Errorf("the question %q %q", c.title, c.lines)
	}
	if got := choiceLabels(c); !reflect.DeepEqual(got, []string{"Down"}) {
		t.Errorf("choices %v", got)
	}
	r.settle()
	if len(cli.lines()) != 0 {
		t.Fatalf("something ran before the answer: %q", cli.lines())
	}
	r.ext.cancelConfirm()
	r.settle()
	if len(cli.lines()) != 0 || r.ext.confirm != nil || len(r.ext.logs.sessions) != 0 {
		t.Errorf("Cancel did something: %q", cli.lines())
	}
	if d := destructive(r.fake); len(d) != 0 {
		t.Errorf("the engine was asked: %v", d)
	}

	r.run("compose.down", explorerArgs(p))
	r.ext.answer(r.asked(), 0)
	s := r.cliTab()
	if got := cli.without("compose version"); !reflect.DeepEqual(got, []string{"compose -f stack/compose.yaml down"}) {
		t.Errorf("calls %q", got)
	}
	if s.title != "Compose Down: stack/compose.yaml" {
		t.Errorf("title %q", s.title)
	}
}

func TestComposeRestartIsDownThenUpInOneTab(t *testing.T) {
	r, cli, p := composeRig(t)
	r.run("compose.restart", explorerArgs(p))
	r.ext.answer(r.asked(), 0)
	s := r.cliTab()
	want := []string{"compose -f stack/compose.yaml down", "compose -f stack/compose.yaml up -d --build"}
	if got := cli.without("compose version"); !reflect.DeepEqual(got, want) {
		t.Errorf("calls %q, want %q", got, want)
	}
	if s.title != "Compose Restart: stack/compose.yaml" || len(r.ext.logs.sessions) != 1 {
		t.Errorf("title %q, %d tabs", s.title, len(r.ext.logs.sessions))
	}
	// a failed down stops the restart
	cli.answer("compose_down", "", "cannot stop\n", 1)
	r.run("compose.restart", explorerArgs(p))
	r.ext.answer(r.asked(), 0)
	r.wait("the second tab", func() bool { return len(r.ext.logs.sessions) == 2 && r.ext.logs.sessions[1].ended })
	if got := cli.without("compose version"); len(got) != 3 {
		t.Errorf("up ran after a down that failed: %q", got)
	}
}

func TestComposeDownNeedsTheEngine(t *testing.T) {
	cli := newFakeCLI(t, "docker")
	r := newRig(t, nil)
	r.ext.client = nil
	p := r.file("compose.yaml", "services: {}\n")
	r.run("compose.down", explorerArgs(p))
	if !r.hasNote("Docker is not running or cannot be reached") || r.ext.confirm != nil || len(cli.lines()) != 0 {
		t.Errorf("notes %v", r.notes())
	}
}

func TestComposeLogsFollowsInATabPerFile(t *testing.T) {
	r, cli, p := composeRig(t)
	cli.answer("compose_logs", "web-1  | ready\n", "", 0)
	q := r.file("compose.override.yaml", "services: {}\n")
	r.run("compose.logs", explorerArgs(p, q))
	r.wait("two tabs", func() bool {
		return len(r.ext.logs.sessions) == 2 && r.ext.logs.sessions[0].ended && r.ext.logs.sessions[1].ended
	})
	got := cli.without("compose version")
	if len(got) != 2 {
		t.Fatalf("calls %q", got)
	}
	for _, want := range []string{"compose -f stack/compose.yaml logs --tail 1000 -f", "compose -f compose.override.yaml logs --tail 1000 -f"} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("no call %q in %q", want, got)
		}
	}
}

func TestComposeUpSelectServices(t *testing.T) {
	r, cli, p := composeRig(t)
	cli.answer("compose_config", "web\ndb\ncache\n", "", 0)
	r.run("compose.up.subset", explorerArgs(p))
	r.wait("the pick", func() bool { return r.api.LastPick() != nil })
	pick := r.api.LastPick()
	want := []string{"$(check) Done: 3 of 3 chosen", "$(pass-filled) web", "$(pass-filled) db", "$(pass-filled) cache"}
	if !reflect.DeepEqual(pick.Labels(), want) {
		t.Fatalf("labels %q", pick.Labels())
	}
	if got := cli.without("compose version"); !reflect.DeepEqual(got, []string{"compose -f stack/compose.yaml config --services", "compose -f stack/compose.yaml config --profiles"}) {
		t.Errorf("calls %q", got)
	}
	pick.Choose(2) // db is left out
	pick = r.api.LastPick()
	if pick.Labels()[2] != "$(circle-large-outline) db" || pick.Labels()[0] != "$(check) Done: 2 of 3 chosen" {
		t.Fatalf("labels after a toggle %q", pick.Labels())
	}
	pick.Choose(0)
	s := r.cliTab()
	if got := cli.without("compose version"); got[len(got)-1] != "compose -f stack/compose.yaml up -d --build web cache" {
		t.Errorf("calls %q", got)
	}
	if s.title != "Compose Up: stack/compose.yaml (web, cache)" {
		t.Errorf("title %q", s.title)
	}
	// the choice is remembered for the file
	r.run("compose.up.subset", explorerArgs(p))
	r.wait("the second pick", func() bool { return len(r.api.Picks()) == 3 })
	if got := r.api.LastPick().Labels()[0]; got != "$(check) Done: 2 of 3 chosen" {
		t.Errorf("the second pick starts with %q", got)
	}
	r.api.LastPick().Close()
}

func TestComposeSelectServicesOrProfilesAndNothingChosen(t *testing.T) {
	r, cli, p := composeRig(t)
	cli.answer("compose_config", "web\n", "", 0)
	cli.answer("compose_config_profiles", "debug\n", "", 0)
	r.run("compose.up.subset", explorerArgs(p))
	r.wait("the pick", func() bool { return r.api.LastPick() != nil })
	pick := r.api.LastPick()
	if !reflect.DeepEqual(pick.Labels(), []string{"Services", "Profiles"}) {
		t.Fatalf("labels %q", pick.Labels())
	}
	pick.Choose(1)
	pick = r.api.LastPick()
	if pick.Labels()[1] != "$(pass-filled) debug" {
		t.Fatalf("labels %q", pick.Labels())
	}
	pick.Choose(1) // debug off
	r.api.LastPick().Choose(0)
	if !r.hasNote("Nothing is chosen.") {
		t.Errorf("notes %v", r.notes())
	}
	// profiles go before the files
	r.run("compose.up.subset", explorerArgs(p))
	r.wait("the kind pick", func() bool { return len(r.api.Picks()) == 4 })
	r.api.LastPick().Choose(1)
	r.api.LastPick().Choose(0)
	r.cliTab()
	if got := cli.without("compose version"); got[len(got)-1] != "compose --profile debug -f stack/compose.yaml up -d --build" {
		t.Errorf("calls %q", got)
	}
}

func TestComposeSelectServicesFailures(t *testing.T) {
	r, cli, p := composeRig(t)
	cli.answer("compose_config", "", "yaml: line 3: mapping values are not allowed\n", 1)
	r.run("compose.up.subset", explorerArgs(p))
	r.wait("the note", func() bool {
		return r.hasNote("Cannot read the services of stack/compose.yaml: Compose could not read the file: yaml: line 3")
	})
	cli.answer("compose_config", "", "", 0)
	r.run("compose.up.subset", explorerArgs(p))
	r.wait("the note", func() bool { return r.hasNote("No services were found in stack/compose.yaml.") })
	if r.api.LastPick() != nil {
		t.Error("a pick was shown with nothing to pick")
	}
	_, err := r.ext.composeNames(r.ext.ctx, []string{"docker", "compose"}, composeTarget{files: []string{p}}, filepath.Dir(p), "services")
	if err != nil {
		t.Fatal(err)
	}
	cli.answer("compose_config", "", "boom\n", 2)
	_, err = r.ext.composeNames(r.ext.ctx, []string{"docker", "compose"}, composeTarget{files: []string{p}}, filepath.Dir(p), "services")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ext.composeNames.ContainerUIComposeConfigFailed") {
		t.Errorf("route %v", err)
	}
}

func TestComposeDownSelectServicesAsksAndRunsDown(t *testing.T) {
	r, cli, p := composeRig(t)
	cli.answer("compose_config", "web\ndb\n", "", 0)
	r.run("compose.down.subset", explorerArgs(p))
	r.wait("the pick", func() bool { return r.api.LastPick() != nil })
	r.api.LastPick().Choose(2) // db off
	r.api.LastPick().Choose(0)
	c := r.asked()
	if c.title != "Take down web of stack/compose.yaml?" {
		t.Errorf("the question %q", c.title)
	}
	for _, l := range cli.without("compose version") {
		if strings.Contains(l, " down") {
			t.Errorf("down ran before the answer: %q", l)
		}
	}
	r.ext.answer(c, 0)
	r.cliTab()
	if got := cli.without("compose version"); got[len(got)-1] != "compose -f stack/compose.yaml down web" {
		t.Errorf("calls %q", got)
	}
}

func TestComposeUsesTheFilesOfTheMenusOnlyForComposeFiles(t *testing.T) {
	r, cli, _ := composeRig(t)
	other := r.file("notes.txt", "x")
	r.run("compose.up", explorerArgs(other)) // not a Compose file: the palette's way, the only Compose file is used
	r.cliTab()
	if got := cli.without("compose version"); got[0] != "compose -f stack/compose.yaml up -d --build" {
		t.Errorf("calls %q", got)
	}
}

// ---- the Compose group ----

func groupRig(t *testing.T) (*rig, *fakeCLI, string) {
	t.Helper()
	cli := newFakeCLI(t, "docker")
	dir := t.TempDir()
	labels := func(service string) map[string]string {
		return map[string]string{
			containerapi.LabelCompose: "shop", containerapi.LabelComposeService: service,
			labelComposeWorkingDir: dir, labelComposeFiles: dir + "/compose.yaml," + dir + "/compose.prod.yaml", labelComposeEnvFile: dir + "/.env",
		}
	}
	r := newRig(t, func(tb testing.TB, f *containerapitest.Fake) {
		f.AddImage(1000, "nginx:1.27")
		f.AddContainer("shop-web-1", "nginx:1.27", labels("web"), "running")
		f.AddContainer("shop-db-1", "nginx:1.27", labels("db"), "running")
		f.AddContainer("lone", "nginx:1.27", nil, "running")
		f.AddContainer("nolabels-1", "nginx:1.27", map[string]string{containerapi.LabelCompose: "nolabels"}, "running")
	})
	return r, cli, dir
}

func TestComposeGroupCommands(t *testing.T) {
	r, cli, dir := groupRig(t)
	files := "-f " + dir + "/compose.yaml -f " + dir + "/compose.prod.yaml -p shop --env-file " + dir + "/.env"
	tests := []struct {
		command, title, want string
	}{
		{"containers.composeGroup.logs", "Compose Logs: shop", "compose " + files + " logs --tail 1000 -f"},
		{"containers.composeGroup.start", "Compose Start: shop", "compose " + files + " start"},
		{"containers.composeGroup.stop", "Compose Stop: shop", "compose " + files + " stop"},
		{"containers.composeGroup.restart", "Compose Restart: shop", "compose " + files + " restart"},
	}
	for i, tc := range tests {
		r.run(tc.command, key(keyGroup+"shop"))
		r.wait("the tab", func() bool { return len(r.ext.logs.sessions) == i+1 && r.ext.logs.sessions[i].ended })
		got := cli.without("compose version")
		if got[len(got)-1] != tc.want || r.ext.logs.sessions[i].title != tc.title {
			t.Errorf("%s: %q (title %q), want %q", tc.command, got[len(got)-1], r.ext.logs.sessions[i].title, tc.want)
		}
		if c := cli.calls(); c[len(c)-1].cwd != func() string { p, _ := filepath.EvalSymlinks(dir); return p }() {
			t.Errorf("%s ran in %s", tc.command, c[len(c)-1].cwd)
		}
	}
	// a container of the project names it too
	r.run("containers.composeGroup.stop", key(keyOfContainer(r.id("shop-db-1"))))
	r.wait("the tab", func() bool { return len(r.ext.logs.sessions) == 5 })
}

func TestComposeGroupDownAsksWithTheContainers(t *testing.T) {
	r, cli, dir := groupRig(t)
	r.run("containers.composeGroup.down", key(keyGroup+"shop"))
	c := r.asked()
	if c.title != "Take down the Compose project shop?" || !strings.HasPrefix(c.lines[0], "2 containers: shop-db-1, shop-web-1") ||
		!reflect.DeepEqual(choiceLabels(c), []string{"Down 2 containers"}) {
		t.Errorf("the question %q %q %v", c.title, c.lines, choiceLabels(c))
	}
	r.settle()
	if len(cli.lines()) != 0 {
		t.Fatal("down ran before the answer")
	}
	r.ext.cancelConfirm()
	r.settle()
	if len(cli.lines()) != 0 || len(destructive(r.fake)) != 0 {
		t.Error("Cancel did something")
	}
	r.run("containers.composeGroup.down", key(keyGroup+"shop"))
	r.ext.answer(r.asked(), 0)
	r.cliTab()
	got := cli.without("compose version")
	if want := "compose -f " + dir + "/compose.yaml -f " + dir + "/compose.prod.yaml -p shop --env-file " + dir + "/.env down"; got[len(got)-1] != want {
		t.Errorf("calls %q", got)
	}
}

func TestComposeGroupWithoutLabelsAndWithoutAGroup(t *testing.T) {
	r, cli, _ := groupRig(t)
	r.run("containers.composeGroup.logs", key(keyGroup+"nolabels"))
	if !r.hasNote("Unable to determine the Compose project info of the group 'nolabels'.") || len(cli.lines()) != 0 {
		t.Errorf("notes %v", r.notes())
	}
	// with no row the user picks the project among those the containers belong to
	r.run("containers.composeGroup.stop", nil)
	pick := r.api.LastPick()
	if pick == nil || !reflect.DeepEqual(pick.Labels(), []string{"nolabels", "shop"}) {
		t.Fatalf("pick %+v", pick)
	}
	pick.Choose(1)
	r.cliTab()
	if got := cli.without("compose version"); !strings.HasSuffix(got[len(got)-1], "-p shop --env-file "+r.ext.containers[0].Labels[labelComposeEnvFile]+" stop") {
		t.Errorf("calls %q", got)
	}
}
