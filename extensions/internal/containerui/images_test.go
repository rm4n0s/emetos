package containerui

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// imageKey is the key of the row of a name of an image of the fake.
func (r *rig) imageKey(ref string) string {
	r.t.Helper()
	for _, img := range r.ext.images {
		for _, tag := range img.RepoTags {
			if tag == ref {
				return keyImage + img.ID + ":" + ref
			}
		}
	}
	r.t.Fatalf("no image %s", ref)
	return ""
}

// engineImages are the names of the images the engine has.
func (r *rig) engineImages() []string {
	r.t.Helper()
	is, err := r.fake.Client().ListImages(context.Background(), containerapi.ListImagesOptions{})
	if err != nil {
		r.t.Fatal(err)
	}
	var out []string
	for _, i := range is {
		out = append(out, i.RepoTags...)
	}
	return out
}

// TestPullAsksForTheNameAndShowsItsProgress pulls an image the user names: the Images section shows the operation, the list
// follows the engine's event, and a notification says it is done.
func TestPullAsksForTheNameAndShowsItsProgress(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("images.pull", nil)
	in := r.api.LastInput()
	if in.Validate("") == "" || in.Validate("two words") == "" || in.Validate("alpine:3.20") != "" {
		t.Errorf("the validation: %q %q", in.Validate(""), in.Validate("alpine:3.20"))
	}
	in.Accept("alpine:3.20")
	if len(r.ext.ops) != 1 || r.ext.ops[0].title != "Pulling alpine:3.20" {
		t.Fatalf("operations %v", r.ext.ops)
	}
	r.wait("the end of the pull", func() bool { return len(r.ext.ops) == 0 && r.hasNote("Pulled alpine:3.20.") })
	r.wait("the image in the list", func() bool { return strings.Contains(strings.Join(r.labels(secImages), ","), "alpine") })
}

// TestAPullThatFailsTellsTheRegistrysWords makes the pull fail and finds the engine's message in a notification, and nothing is
// left of the operation.
func TestAPullThatFailsTellsTheRegistrysWords(t *testing.T) {
	r := newRig(t, nil)
	r.fake.FailPull("nope:1", "pull access denied for nope, repository does not exist")
	r.ext.pullImage("nope:1")
	r.wait("the failure", func() bool { return r.hasNote("pull access denied for nope") })
	if len(r.ext.ops) != 0 {
		t.Errorf("operations left: %v", r.ext.ops)
	}
}

// TestAPullCanBeCancelled cancels the operation from its button and finds no image and a note that says it was cancelled.
func TestAPullCanBeCancelled(t *testing.T) {
	r := newRig(t, nil)
	r.ext.startPull("slow:1")
	r.ext.ops[0].cancel()
	r.wait("the end", func() bool { return len(r.ext.ops) == 0 })
	if r.hasNote("Pulled slow:1") {
		t.Errorf("a cancelled pull is reported as done: %v", r.notes())
	}
}

// TestTheProgressOfAPullCountsTheLayers holds the percentage of an operation to the sizes the layers told.
func TestTheProgressOfAPullCountsTheLayers(t *testing.T) {
	op := &operation{}
	if op.percent() != -1 {
		t.Errorf("percent with no layer: %v", op.percent())
	}
	r := newRig(t, nil)
	op.progress(r.ext, containerapi.PullProgress{ID: "a", Status: "Downloading", Current: 50, Total: 100})
	op.progress(r.ext, containerapi.PullProgress{ID: "b", Status: "Downloading", Current: 150, Total: 300})
	op.progress(r.ext, containerapi.PullProgress{ID: "a", Status: "Downloading", Current: 100, Total: 100})
	if got := op.percent(); got < 62.4 || got > 62.6 {
		t.Errorf("percent %v, want 62.5", got)
	}
	if op.statusLine() != "a Downloading" {
		t.Errorf("status %q", op.statusLine())
	}
}

// TestTagGivesAnImageAnotherName tags an image and finds the new name in the engine and in the view.
func TestTagGivesAnImageAnotherName(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("images.tag", key(r.imageKey("postgres:16")))
	in := r.api.LastInput()
	if in.Options().Value != "postgres:16" || in.Validate("") == "" {
		t.Errorf("the box: %v", in.Options())
	}
	in.Accept("registry.local/db:stable")
	r.wait("the new name", func() bool { return strings.Contains(strings.Join(r.engineImages(), ","), "registry.local/db:stable") })
	r.wait("the view", func() bool { return strings.Contains(strings.Join(r.labels(secImages), ","), "registry.local/db") })
}

// TestRemovingAnImageAsksWithItsSize shows the size in the question, offers force only for an image a container uses, and takes only
// the name off an image that has two.
func TestRemovingAnImageAsksWithItsSize(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.fake.ResetRequests()
	r.run("images.remove", key(r.imageKey("nginx:latest")))
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	c := r.ext.confirm
	text := strings.Join(c.lines, "\n")
	if c.title != "Remove 1 image?" || !strings.Contains(text, "nginx:latest  187MB") {
		t.Errorf("the question %q %q", c.title, text)
	}
	r.ext.cancelConfirm()
	r.settle()
	if d := destructive(r.fake); len(d) != 0 {
		t.Fatalf("Cancel changed the engine: %v", d)
	}
	r.run("images.remove", key(r.imageKey("nginx:latest")))
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	r.ext.answer(r.ext.confirm, 0)
	r.wait("the name gone", func() bool { return !strings.Contains(strings.Join(r.engineImages(), ","), "nginx:latest") })
	if !strings.Contains(strings.Join(r.engineImages(), ","), "nginx:1.27") {
		t.Errorf("the other name went too: %v", r.engineImages())
	}
}

// TestRemovingTheNamelessImageAndAGroup removes the image that has no name by its id, and a repository at once.
func TestRemovingTheNamelessImageAndAGroup(t *testing.T) {
	r := newRig(t, sampleEngine)
	rows := r.ext.imageRowsOfKeys([]string{keyGroup + "image:" + noRepository})
	if len(rows) != 1 || rows[0].ref != "" || rows[0].target() != rows[0].image.ID {
		t.Fatalf("rows %v", rows)
	}
	r.ext.askRemoveImages(rows)
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	if !strings.Contains(strings.Join(r.ext.confirm.lines, "\n"), "(no name)") {
		t.Errorf("lines %v", r.ext.confirm.lines)
	}
	r.ext.answer(r.ext.confirm, 0)
	r.wait("the image gone", func() bool { return len(r.ext.imageRowsOfKeys([]string{keyGroup + "image:" + noRepository})) == 0 })

	r.run("images.remove", key(keyGroup+"image:nginx"))
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	if r.ext.confirm.title != "Remove 2 images?" {
		t.Errorf("title %q", r.ext.confirm.title)
	}
}

// TestFindingAnImageByPick runs Run on no row: the pick lists the names of the images.
func TestFindingAnImageByPick(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("images.inspect", nil)
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no pick")
	}
	labels := pick.Labels()
	if len(labels) != 5 || labels[0] != "busybox:1.37.0" {
		t.Errorf("the pick %v", labels)
	}
	pick.Choose(0)
	r.wait("the tab", func() bool { return len(r.api.EditorTabs()) == 1 })
}

// TestRunningAnImageMakesAContainerFromTheForm fills the form of Run and presses Run: the container is made with the ports, the
// environment, the volume, the name and --rm, started, and the tab closes.
func TestRunningAnImageMakesAContainerFromTheForm(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("images.run", key(r.imageKey("busybox:1.37.0")))
	tab := r.api.EditorTabs()[0]
	if tab.Kind() != r.cfg.RunKind() || tab.Title() != "Run busybox:1.37.0" {
		t.Fatalf("the tab: %s %q", tab.Kind(), tab.Title())
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 640, Height: 760}, widgetFrom(tab.Content.Build))
	for _, text := range []string{"Run busybox:1.37.0", "Name", "Ports", "Environment", "Volumes", "Network", "Command", "Remove the container when it stops (--rm)"} {
		if !hasText(ts, text) {
			t.Errorf("the form has no %q", text)
		}
	}
	f := r.ext.runs.forms[0]
	f.name.SetValue("emetos-run-1")
	f.ports.SetValue("127.0.0.1:18080:80\n5432")
	f.env.SetValue("MODE=test\nGREETING=hello world")
	f.volumes.SetValue("scratchvol:/data")
	f.command.SetValue(`sh -c "echo hi"`)
	f.run()
	r.wait("the container", func() bool { return tab.Closed })
	r.wait("the list", func() bool { return strings.Contains(strings.Join(r.labels(secContainers), ","), "emetos-run-1") })
	var create []byte
	for _, rq := range r.fake.Requests() {
		if rq.Method == "POST" && strings.HasSuffix(rq.Path, "/containers/create") {
			create = rq.Body
		}
	}
	var body struct {
		Image string
		Cmd   []string
		Env   []string
		Host  struct {
			AutoRemove   bool
			Binds        []string
			PortBindings map[string][]struct{ HostIp, HostPort string }
		} `json:"HostConfig"`
	}
	if err := json.Unmarshal(create, &body); err != nil {
		t.Fatalf("the create body: %v\n%s", err, create)
	}
	if body.Image != "busybox:1.37.0" || !reflect.DeepEqual(body.Cmd, []string{"sh", "-c", "echo hi"}) || !body.Host.AutoRemove ||
		!reflect.DeepEqual(body.Env, []string{"MODE=test", "GREETING=hello world"}) || !reflect.DeepEqual(body.Host.Binds, []string{"scratchvol:/data"}) {
		t.Errorf("the container: %+v", body)
	}
	if pb := body.Host.PortBindings["80/tcp"]; len(pb) != 1 || pb[0].HostIp != "127.0.0.1" || pb[0].HostPort != "18080" {
		t.Errorf("the ports: %+v", body.Host.PortBindings)
	}
	if !r.hasNote("Started a container of busybox:1.37.0.") {
		t.Errorf("notes %v", r.notes())
	}
}

// TestTheFormSaysWhatIsWrongAndWhatTheEngineRefuses holds the message line of the form: a bad port is told before anything is
// sent, and the engine's own refusal (a name that is taken) is shown, with the form kept for another try.
func TestTheFormSaysWhatIsWrongAndWhatTheEngineRefuses(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("images.run", key(r.imageKey("busybox:1.37.0")))
	tab := r.api.EditorTabs()[0]
	f := r.ext.runs.forms[0]
	r.fake.ResetRequests()
	f.ports.SetValue("eighty")
	f.run()
	if !f.message.Bad || !strings.Contains(f.message.Text, "a port is written") {
		t.Errorf("message %+v", f.message)
	}
	if hasRequest(r.fake, "POST", "/containers/create") {
		t.Error("the engine was asked with a port that is not one")
	}
	f.ports.SetValue("")
	f.name.SetValue("scratch") // a container of that name exists
	f.run()
	r.wait("the engine's answer", func() bool { return !f.busy && f.message.Bad })
	if !strings.Contains(f.message.Text, "scratch") || tab.Closed {
		t.Errorf("message %+v, closed %v", f.message, tab.Closed)
	}
	if f.cancel != nil {
		t.Error("the run is not over")
	}
}

// TestTheFormFieldsAreLetGoOfWhenTheTabCloses closes a tab with a run that goes on: its context ends.
func TestTheFormFieldsAreLetGoOfWhenTheTabCloses(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("images.run", key(r.imageKey("busybox:1.37.0")))
	tab := r.api.EditorTabs()[0]
	tab.Close()
	if !tab.Closed {
		t.Error("the tab is not closed")
	}
}

// TestRestoredTabsReadTheirState holds the tabs that come back after a restart, and the route of the error of a state that is
// not JSON.
func TestRestoredTabsReadTheirState(t *testing.T) {
	r := newRig(t, nil)
	tab := r.api.RestoreTab(r.cfg.DocumentKind(), "d1", []byte(`{"title":"web (inspect)","hint":"h","text":"{\n  \"a\": 1\n}\n","language":"json"}`))
	if tab.Title() != "web (inspect)" || tab.Hint() != "h" {
		t.Errorf("the tab: %q %q", tab.Title(), tab.Hint())
	}
	var st docState
	if err := json.Unmarshal(tab.State(), &st); err != nil || st.Text != "{\n  \"a\": 1\n}\n" {
		t.Errorf("the state %+v %v", st, err)
	}
	form := r.api.RestoreTab(r.cfg.RunKind(), "r1", []byte(`{"image":"busybox:1.37.0","label":"busybox"}`))
	if form.Title() != "Run busybox" {
		t.Errorf("the form tab: %q", form.Title())
	}
	_, err := r.ext.docs.create(r.api.EditorTabs()[0], []byte("{"))
	wantRoute(t, err, "documents.create.ContainerUIDocumentStateInvalid")
	_, err = r.ext.runs.create(r.api.EditorTabs()[0], []byte("{"))
	wantRoute(t, err, "runForms.create.ContainerUIRunStateInvalid")
	if err := r.ext.restoreState([]byte("{")); err == nil {
		t.Error("a state that is not JSON was accepted")
	} else {
		wantRoute(t, err, "ext.restoreState.ContainerUIStateInvalid")
	}
}
