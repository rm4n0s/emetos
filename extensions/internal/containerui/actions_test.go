package containerui

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/glutter/ui"
)

// TestContainerActionsAskTheEngine runs each action that needs no question on a container, by the key of its row (as the menu and
// the buttons of the row do), and holds the request the engine got and the state it ends in.
func TestContainerActionsAskTheEngine(t *testing.T) {
	tests := []struct {
		command, container string
		method, suffix     string
		state              string
	}{
		{"containers.start", "scratch", "POST", "/start", "running"},
		{"containers.stop", "shop-web-1", "POST", "/stop", "exited"},
		{"containers.restart", "shop-web-1", "POST", "/restart", "running"},
		{"containers.pause", "shop-web-1", "POST", "/pause", "paused"},
		{"containers.kill", "shop-db-1", "POST", "/kill", "exited"},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			r := newRig(t, sampleEngine)
			r.fake.ResetRequests()
			r.run(tc.command, key(keyOfContainer(r.id(tc.container))))
			r.wait("the engine's answer", func() bool { return hasRequest(r.fake, tc.method, tc.suffix) })
			r.wait("the new state", func() bool { return r.state(tc.container) == tc.state })
			r.wait("the view to follow the engine", func() bool {
				c, _ := r.ext.containerByID(r.id(tc.container))
				return c.State == tc.state
			})
		})
	}
}

// TestUnpauseAndTheKeyboardFallbacks holds Unpause, which needs a paused container, and the pick that a command with no row shows.
func TestUnpauseAndTheKeyboardFallbacks(t *testing.T) {
	r := newRig(t, sampleEngine)
	id := r.id("shop-web-1")
	if err := r.fake.Client().PauseContainer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.wait("the paused state", func() bool { c, _ := r.ext.containerByID(id); return c.State == "paused" })
	r.run("containers.unpause", nil) // no row, no selection: a pick of the paused containers
	pick := r.api.LastPick()
	if pick == nil || !reflect.DeepEqual(pick.Labels(), []string{"shop-web-1"}) {
		t.Fatalf("the pick: %v", pick)
	}
	pick.Choose(0)
	r.wait("the unpaused state", func() bool { return r.state("shop-web-1") == "running" })

	// the selection of the view is what a command with no argument works on
	v := r.view(secContainers)
	v.tree.SetSelected(keyOfContainer(r.id("scratch")))
	r.run("containers.start", nil)
	r.wait("scratch started", func() bool { return r.state("scratch") == "running" })

	// a pick with nothing to pick from says so
	r.wait("the follow of the engine", func() bool { c, _ := r.ext.containerByID(id); return c.State == "running" })
	n := len(r.api.Picks())
	r.run("containers.unpause", nil)
	if len(r.api.Picks()) != n || !r.hasNote("There is no container to unpause") {
		t.Errorf("notes %v", r.notes())
	}
}

// TestAFailingActionSaysWhyInANotification makes the engine refuse and finds its own message in the notification.
func TestAFailingActionSaysWhyInANotification(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.fake.FailNext("POST", "/start", 500, "driver failed programming external connectivity")
	r.run("containers.start", key(keyOfContainer(r.id("scratch"))))
	r.wait("the notification", func() bool { return r.hasNote("driver failed programming external connectivity") })
	if !r.hasNote("Cannot start scratch") {
		t.Errorf("notes %v", r.notes())
	}
}

// TestRemovingAsksFirstAndCancelChangesNothing is the safety of SPEC §10.20: Remove shows the question with the count and the
// names; nothing is sent to the engine before a choice; Cancel, pressed in the view, leaves the engine as it was.
func TestRemovingAsksFirstAndCancelChangesNothing(t *testing.T) {
	r := newRig(t, sampleEngine)
	ts := r.show(uitest.Themes()[0], ui.Size{Width: 380, Height: 400}, r.cfg.ViewID(secContainers))
	ts.refresh()
	before := r.engineNames()
	r.fake.ResetRequests()

	r.run("containers.remove", key(keyOfContainer(r.id("shop-web-1"))))
	c := r.asked()
	if c.title != "Remove 1 container?" || !strings.Contains(strings.Join(c.lines, " "), "shop-web-1") {
		t.Errorf("the question: %q %v", c.title, c.lines)
	}
	if want := []string{"Force remove 1 (kills 1 running)"}; !reflect.DeepEqual(choiceLabels(c), want) {
		t.Errorf("the choices %v, want %v", choiceLabels(c), want)
	}
	ts.refresh()
	for _, text := range []string{"Remove 1 container?", "Cancel"} {
		if !ts.has(text) {
			t.Errorf("the view does not show %q", text)
		}
	}
	r.settle()
	if d := destructive(r.fake); len(d) != 0 {
		t.Fatalf("the engine was changed before the answer: %v", d)
	}
	ts.tap("Cancel")
	r.settle()
	if r.ext.confirm != nil || ts.has("Remove 1 container?") {
		t.Error("the question is still there after Cancel")
	}
	if d := destructive(r.fake); len(d) != 0 {
		t.Errorf("Cancel changed the engine: %v", d)
	}
	if after := r.engineNames(); !reflect.DeepEqual(after, before) {
		t.Errorf("containers %v, want %v", after, before)
	}
}

// TestRemovingARunningContainerNeedsTheForceChoice answers the question with each of its choices: a running container goes only
// with the choice that says it is killed, and the other choice removes the stopped ones and leaves the running ones.
func TestRemovingARunningContainerNeedsTheForceChoice(t *testing.T) {
	t.Run("force", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		r.run("containers.remove", key(keyGroup+"shop"))
		c := r.asked()
		if c.title != "Remove 2 containers?" {
			t.Errorf("title %q", c.title)
		}
		r.ext.answer(c, 0)
		r.wait("the containers gone", func() bool { return sameStrings(r.engineNames(), []string{"blog-app-1", "scratch"}) })
		r.wait("the note", func() bool { return r.hasNote("Removed 2 containers.") })
		if !strings.Contains(strings.Join(requestQueries(r.fake, "DELETE"), " "), "force=1") && !strings.Contains(strings.Join(requestQueries(r.fake, "DELETE"), " "), "force=true") {
			t.Errorf("the removal was not forced: %v", requestQueries(r.fake, "DELETE"))
		}
	})
	t.Run("only the stopped ones", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		v := r.view(secContainers)
		v.tree.SetSelection([]string{keyOfContainer(r.id("shop-web-1")), keyOfContainer(r.id("scratch")), keyOfContainer(r.id("blog-app-1"))})
		r.run("containers.remove", nil)
		c := r.asked()
		want := []string{"Force remove 3 (kills 1 running)", "Remove only the 2 stopped"}
		if !reflect.DeepEqual(choiceLabels(c), want) {
			t.Fatalf("the choices %v, want %v", choiceLabels(c), want)
		}
		r.ext.answer(c, 1)
		r.wait("the stopped ones gone", func() bool { return sameStrings(r.engineNames(), []string{"shop-web-1", "shop-db-1"}) })
	})
	t.Run("stopped containers have one choice, without force", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		r.run("containers.remove", key(keyOfContainer(r.id("scratch"))))
		c := r.asked()
		if want := []string{"Remove 1 container"}; !reflect.DeepEqual(choiceLabels(c), want) {
			t.Fatalf("the choices %v, want %v", choiceLabels(c), want)
		}
		r.ext.answer(c, 0)
		r.wait("scratch gone", func() bool { return r.state("scratch") == "(gone)" })
		for _, q := range requestQueries(r.fake, "DELETE") {
			if strings.Contains(q, "force=1") || strings.Contains(q, "force=true") {
				t.Errorf("a stopped container was removed with force: %s", q)
			}
		}
	})
}

// requestQueries are the queries of the requests with a method.
func requestQueries(f *containerapitest.Fake, method string) []string {
	var out []string
	for _, rq := range f.Requests() {
		if rq.Method == method {
			out = append(out, rq.Query)
		}
	}
	return out
}

// TestAQuestionIsReplacedByTheNext shows a second question while the first waits: only the second can be answered.
func TestAQuestionIsReplacedByTheNext(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("containers.remove", key(keyOfContainer(r.id("scratch"))))
	first := r.asked()
	r.run("containers.remove", key(keyOfContainer(r.id("blog-app-1"))))
	second := r.asked()
	r.ext.answer(first, 0) // an answer to the question that is gone does nothing
	r.settle()
	if d := destructive(r.fake); len(d) != 0 || r.ext.confirm != second {
		t.Fatalf("the replaced question was answered: %v", d)
	}
	r.ext.answer(second, 0)
	r.wait("blog-app-1 gone", func() bool { return r.state("blog-app-1") == "(gone)" })
	if r.state("scratch") != "exited" {
		t.Error("scratch was removed")
	}
}

// TestPrunesAskWithCountsAndSizes runs every prune: the question names what goes with its count and size, Cancel leaves the engine
// as it is, and the choice does the prune.
func TestPrunesAskWithCountsAndSizes(t *testing.T) {
	tests := []struct {
		command string
		title   string
		lines   []string
		choice  string
		suffix  string
	}{
		{"containers.prune", "Remove all 2 stopped containers?", []string{"blog-app-1, scratch"}, "Remove 2 stopped containers", "/containers/prune"},
		{"networks.prune", "Remove all unused networks?", []string{"1 network no container uses: shop_default"}, "Remove 1 unused network", "/networks/prune"},
		{"images.prune", "Remove unused images?", nil, "Remove 1 dangling image (90MB)", "/images/prune"},
		{"pruneSystem", "Remove everything that is not in use?", []string{"2 stopped containers", "1 unused network", "1 dangling image, 90MB", "1 unused volume"}, "Prune the system", "/prune"},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			r := newRig(t, sampleEngine)
			r.fake.ResetRequests()
			r.run(tc.command, nil)
			r.wait("the question", func() bool { return r.ext.confirm != nil })
			c := r.ext.confirm
			if c.title != tc.title {
				t.Errorf("title %q, want %q", c.title, tc.title)
			}
			for _, l := range tc.lines {
				if !strings.Contains(strings.Join(c.lines, "\n"), l) {
					t.Errorf("the lines %q do not have %q", c.lines, l)
				}
			}
			found := false
			for _, l := range choiceLabels(c) {
				found = found || l == tc.choice
			}
			if !found {
				t.Fatalf("the choices %v do not have %q", choiceLabels(c), tc.choice)
			}
			r.ext.cancelConfirm()
			r.settle()
			if d := destructive(r.fake); len(d) != 0 {
				t.Fatalf("Cancel changed the engine: %v", d)
			}
			r.run(tc.command, nil)
			r.wait("the question again", func() bool { return r.ext.confirm != nil })
			for i, l := range choiceLabels(r.ext.confirm) {
				if l == tc.choice {
					r.ext.answer(r.ext.confirm, i)
				}
			}
			r.wait("the prune", func() bool { return hasRequest(r.fake, "POST", tc.suffix) })
		})
	}
}

// TestPruningWithNothingToRemoveSaysSo holds the prunes to a short answer when there is nothing to remove.
func TestPruningWithNothingToRemoveSaysSo(t *testing.T) {
	r := newRig(t, nil)
	for _, tc := range []struct{ command, note string }{
		{"containers.prune", "There are no stopped containers."}, {"networks.prune", "There are no unused networks."},
		{"images.prune", "There are no unused images."}, {"volumes.prune", "There are no unused volumes."},
	} {
		r.run(tc.command, nil)
		r.wait(tc.command, func() bool { return r.hasNote(tc.note) })
		if r.ext.confirm != nil {
			t.Errorf("%s asked a question about nothing", tc.command)
		}
	}
}

// TestRemovingVolumesAndNetworks asks, then removes, and the volume's question says its size and that its data cannot come back.
func TestRemovingVolumesAndNetworks(t *testing.T) {
	r := newRig(t, func(t testing.TB, f *containerapitest.Fake) {
		sampleEngine(t, f)
		f.SetVolumeSize("pgdata", 2_500_000)
	})
	r.run("volumes.remove", key(keyVolume+"pgdata"))
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	c := r.ext.confirm
	text := strings.Join(c.lines, "\n")
	if c.title != "Remove 1 volume?" || !strings.Contains(text, "pgdata  2.5MB") || !strings.Contains(text, "cannot be brought back") {
		t.Errorf("the question %q %q", c.title, text)
	}
	r.ext.cancelConfirm()
	r.settle()
	if d := destructive(r.fake); len(d) != 0 {
		t.Fatalf("Cancel changed the engine: %v", d)
	}
	r.run("volumes.remove", key(keyVolume+"pgdata"))
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	r.ext.answer(r.ext.confirm, 0)
	r.wait("the volume gone", func() bool { return len(r.labels(secVolumes)) == 0 })

	net := r.ext.networks[len(r.ext.networks)-1]
	r.run("networks.remove", key(keyNetwork+net.ID))
	r.wait("the question", func() bool { return r.ext.confirm != nil })
	if r.ext.confirm.title != "Remove 1 network?" {
		t.Errorf("title %q", r.ext.confirm.title)
	}
	r.ext.answer(r.ext.confirm, 0)
	r.wait("the network gone", func() bool { return !strings.Contains(strings.Join(r.labels(secNetworks), ","), "shop_default") })
}

// TestABuiltInNetworkCannotBeRemoved holds the menu of the bridge network: Remove is there and off.
func TestABuiltInNetworkCannotBeRemoved(t *testing.T) {
	r := newRig(t, sampleEngine)
	var bridge containerapi.Network
	for _, n := range r.ext.networks {
		if n.Name == "bridge" {
			bridge = n
		}
	}
	items := r.view(secNetworks).sec.menu(r.view(secNetworks), []string{keyNetwork + bridge.ID})
	if len(items) == 0 || items[len(items)-1].Label != "Remove..." || !items[len(items)-1].Disabled {
		t.Errorf("the menu of the bridge network: %v", items)
	}
}

// TestCreatingNetworksAndVolumes asks for a name and makes the thing; a bad name is refused by the input box.
func TestCreatingNetworksAndVolumes(t *testing.T) {
	r := newRig(t, nil)
	r.run("networks.create", nil)
	in := r.api.LastInput()
	if in.Validate("") == "" || in.Validate("a b") == "" || in.Validate("good") != "" {
		t.Errorf("the validation: %q %q %q", in.Validate(""), in.Validate("a b"), in.Validate("good"))
	}
	in.Accept("backend")
	r.wait("the network", func() bool { return strings.Contains(strings.Join(r.labels(secNetworks), ","), "backend") })
	r.run("volumes.create", nil)
	r.api.LastInput().Accept("cache")
	r.wait("the volume", func() bool { return reflect.DeepEqual(r.labels(secVolumes), []string{"cache"}) })
}

// TestInspectsOpenReadOnlyTabs inspects a container, an image, a network, a volume and a context and a layer history, and finds a
// tab of the document kind for each, with the JSON the engine sent.
func TestInspectsOpenReadOnlyTabs(t *testing.T) {
	r := newRig(t, sampleEngine)
	web := r.id("shop-web-1")
	img := r.ext.images[0]
	row := imageRow{image: img, ref: img.RepoTags[0]}
	if len(img.RepoTags) == 0 {
		row = imageRow{image: img}
	}
	var net containerapi.Network
	for _, n := range r.ext.networks {
		if n.Name == "shop_default" {
			net = n
		}
	}
	tests := []struct {
		name string
		do   func()
		want []string // title, then words of the text
	}{
		{"container", func() { r.run("containers.inspect", key(keyOfContainer(web))) }, []string{"shop-web-1 (inspect)", "\"Id\"", web}},
		{"image", func() { r.ext.inspectImage(row) }, []string{row.label() + " (inspect)", "\"Id\""}},
		{"network", func() { r.run("networks.inspect", key(keyNetwork+net.ID)) }, []string{"shop_default (inspect)", "\"Name\": \"shop_default\""}},
		{"volume", func() { r.run("volumes.inspect", key(keyVolume+"pgdata")) }, []string{"pgdata (inspect)", "\"Name\": \"pgdata\""}},
		{"context", func() { r.run("contexts.inspect", key(keyContext+"default")) }, []string{"default (inspect)", "\"Name\": \"default\""}},
		{"history", func() { r.ext.imageHistory(row) }, []string{row.label() + " (history)", "CREATED BY"}},
	}
	for _, tc := range tests {
		before := len(r.api.EditorTabs())
		tc.do()
		r.wait(tc.name+" tab", func() bool { return len(r.api.EditorTabs()) > before })
		tab := r.api.EditorTabs()[before]
		if tab.Kind() != r.cfg.DocumentKind() || tab.Title() != tc.want[0] {
			t.Errorf("%s: tab %s %q", tc.name, tab.Kind(), tab.Title())
		}
		var st docState
		if err := json.Unmarshal(tab.State(), &st); err != nil {
			t.Fatal(err)
		}
		for _, w := range tc.want[1:] {
			if !strings.Contains(st.Text, w) {
				t.Errorf("%s: the text does not have %q:\n%s", tc.name, w, st.Text)
			}
		}
	}
}

// TestCopyIdPutsTheIdsOnTheClipboard copies the id of one container and of two, one per line.
func TestCopyIdPutsTheIdsOnTheClipboard(t *testing.T) {
	r := newRig(t, sampleEngine)
	a, b := r.id("scratch"), r.id("blog-app-1")
	r.run("containers.copyId", key(keyOfContainer(a)))
	r.view(secContainers).tree.SetSelection([]string{keyOfContainer(a), keyOfContainer(b)})
	r.run("containers.copyId", nil)
	got := r.api.ClipboardTexts()
	if len(got) != 2 || got[0] != a || got[1] != b+"\n"+a && got[1] != a+"\n"+b {
		t.Errorf("clipboard %q", got)
	}
	r.run("images.copyFullTag", key(keyImage+r.ext.images[0].ID+":"+r.ext.images[0].RepoTags[0]))
}

// TestOpenInBrowserSendsThePublishedPort opens the address of the port a container publishes, or says it publishes none.
func TestOpenInBrowserSendsThePublishedPort(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("containers.browse", key(keyOfContainer(r.id("shop-web-1"))))
	r.wait("the address", func() bool { return len(r.api.OpenedURIs()) == 1 })
	if got := r.api.OpenedURIs()[0]; got != "http://127.0.0.1:8080" {
		t.Errorf("the address %q", got)
	}
	// a container that publishes nothing is not offered in the menu or the pick
	items := r.view(secContainers).sec.menu(r.view(secContainers), []string{keyOfContainer(r.id("shop-db-1"))})
	for _, it := range items {
		if it.Label == "Open in Browser" {
			t.Error("Open in Browser is offered for a container with no port")
		}
	}
	r.ext.openInBrowser(containerapi.Container{Names: []string{"quiet"}})
	if !r.hasNote("quiet publishes no port.") {
		t.Errorf("notes %v", r.notes())
	}
}

// TestTheMenusOfTheRows holds what a right click offers on each kind of row, by state.
func TestTheMenusOfTheRows(t *testing.T) {
	r := newRig(t, sampleEngine)
	labels := func(section string, keys ...string) []string {
		v := r.view(section)
		var out []string
		for _, it := range v.sec.menu(v, keys) {
			if !it.Separator {
				out = append(out, it.Label)
			}
		}
		return out
	}
	tests := []struct {
		name    string
		section string
		keys    []string
		want    []string
	}{
		{"a running container", secContainers, []string{keyOfContainer(r.id("shop-web-1"))},
			[]string{"View Logs", "Inspect", "Open in Browser", "Open Shell", "Run Command...", "Explore Files", "Copy ID", "Stop", "Restart", "Pause", "Kill", "Remove..."}},
		{"a stopped container", secContainers, []string{keyOfContainer(r.id("scratch"))}, []string{"View Logs", "Inspect", "Copy ID", "Start", "Remove..."}},
		{"two containers", secContainers, []string{keyOfContainer(r.id("scratch")), keyOfContainer(r.id("shop-web-1"))}, []string{"Copy ID", "Stop", "Remove..."}},
		{"a Compose project", secContainers, []string{keyGroup + "shop"},
			[]string{"Compose Logs", "Compose Start", "Compose Stop", "Compose Restart", "Compose Down...", "Copy ID", "Stop", "Restart", "Pause", "Kill", "Remove..."}},
		{"a volume", secVolumes, []string{keyVolume + "pgdata"}, []string{"Inspect", "Remove..."}},
	}
	for _, tc := range tests {
		if got := labels(tc.section, tc.keys...); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	img := r.ext.images[0]
	row := r.ext.imageRowsOfKeys([]string{keyImage + img.ID + ":" + img.RepoTags[0]})
	if len(row) != 1 {
		t.Fatalf("rows %v", row)
	}
	if got := labels(secImages, keyImage+img.ID+":"+img.RepoTags[0]); !reflect.DeepEqual(got, []string{"Run...", "Run Interactive", "Inspect", "History", "Pull", "Tag...", "Push...", "Copy Full Tag", "Remove..."}) {
		t.Errorf("an image: %v", got)
	}
}
