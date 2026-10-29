package containerui

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// listCalls counts the list requests of the containers that the fake received.
func listCalls(f *containerapitest.Fake) int {
	n := 0
	for _, rq := range f.Requests() {
		if rq.Method == "GET" && rq.Path == "/containers/json" {
			n++
		}
	}
	return n
}

// TestTheViewsFollowTheEnginesEvents makes things in the engine, not in the view: the views show them after the event, and the
// extension publishes what changed.
func TestTheViewsFollowTheEnginesEvents(t *testing.T) {
	r := newRig(t, sampleEngine)
	ctx := context.Background()
	c := r.fake.Client()
	res, err := c.CreateContainer(ctx, containerapi.ContainerSpec{Name: "late-comer", Image: "busybox:1.37.0"})
	if err != nil {
		t.Fatal(err)
	}
	r.wait("the new container", func() bool { return strings.Contains(strings.Join(r.labels(secContainers), ","), "late-comer") })
	if _, err := c.CreateNetwork(ctx, containerapi.NetworkSpec{Name: "later_net"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateVolume(ctx, containerapi.VolumeSpec{Name: "latervol"}); err != nil {
		t.Fatal(err)
	}
	r.wait("the network and the volume", func() bool {
		return strings.Contains(strings.Join(r.labels(secNetworks), ","), "later_net") && strings.Contains(strings.Join(r.labels(secVolumes), ","), "latervol")
	})
	if err := c.RemoveContainer(ctx, res.ID, containerapi.RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	r.wait("the container gone", func() bool { return !strings.Contains(strings.Join(r.labels(secContainers), ","), "late-comer") })

	var got []changed
	for _, p := range r.api.PublishedEvents() {
		if p.Topic == "emetos.containers.changed" {
			var ch changed
			if err := json.Unmarshal(p.Data, &ch); err != nil {
				t.Fatal(err)
			}
			got = append(got, ch)
		}
	}
	want := map[changed]bool{{"container", res.ID, "create"}: true, {"container", res.ID, "destroy"}: true}
	for _, ch := range got {
		delete(want, ch)
	}
	if len(want) != 0 {
		t.Errorf("the events published: %v, missing %v", got, want)
	}
}

// TestABurstOfEventsIsOneRefresh makes many containers at once and counts the lists: far fewer than the events.
func TestABurstOfEventsIsOneRefresh(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.ext.cfg.Debounce = 150 * time.Millisecond
	r.fake.ResetRequests()
	c := r.fake.Client()
	for i := 0; i < 20; i++ {
		if _, err := c.CreateContainer(context.Background(), containerapi.ContainerSpec{Name: "burst-" + string(rune('a'+i)), Image: "busybox:1.37.0"}); err != nil {
			t.Fatal(err)
		}
	}
	r.wait("all of them", func() bool { return len(r.ext.containers) == 24 })
	if n := listCalls(r.fake); n > 3 {
		t.Errorf("%d lists for 20 events", n)
	}
}

// TestNothingIsPolled waits with no event and no command: the engine gets no list request.
func TestNothingIsPolled(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.settle()
	r.fake.ResetRequests()
	time.Sleep(300 * time.Millisecond)
	r.api.Pump()
	var polled []string
	for _, rq := range r.fake.Requests() {
		polled = append(polled, rq.Method+" "+rq.Path)
	}
	if len(polled) != 0 {
		t.Errorf("requests with nothing to do: %v", polled)
	}
}

// TestAnEventThatChangesNothingIsIgnored sends an exec event: no list is read.
func TestAnEventThatChangesNothingIsIgnored(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.settle()
	r.fake.ResetRequests()
	r.fake.Emit(containerapi.Event{Type: "container", Action: "exec_start: sh", ID: r.id("shop-web-1")})
	r.fake.Emit(containerapi.Event{Type: "container", Action: "attach", ID: r.id("shop-web-1")})
	time.Sleep(100 * time.Millisecond)
	r.api.Pump()
	if n := listCalls(r.fake); n != 0 {
		t.Errorf("%d lists for events that change nothing", n)
	}
	r.fake.Emit(containerapi.Event{Type: "image", Action: "pull", ID: "x"})
	r.wait("the lists after an image event", func() bool { return listCalls(r.fake) == 1 })
}

// TestWhenTheEngineGoesAwayTheViewSaysSo closes the fake: the event stream ends, is opened again a few times, and then the views
// show the missing engine with its reason.
func TestWhenTheEngineGoesAwayTheViewSaysSo(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.fake.Close()
	r.wait("the missing engine", func() bool { return r.ext.status == statusMissing })
	if r.ext.reason == "" || r.api.ContextKey("emetos.containers.connected") != false {
		t.Errorf("reason %q, key %v", r.ext.reason, r.api.ContextKey("emetos.containers.connected"))
	}
	if len(r.labels(secContainers)) != 0 {
		t.Errorf("the rows are still there: %v", r.labels(secContainers))
	}
	r.run("containers.start", nil)
	if !r.hasNote("Docker is not running") {
		t.Errorf("notes %v", r.notes())
	}
}

// TestChangingTheHostSettingLooksAgain sets containers.host: the search starts again with it, and the same value does nothing.
func TestChangingTheHostSettingLooksAgain(t *testing.T) {
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	var opts recorder[containerapi.FindOptions]
	var n atomic.Int32
	cfg := testConfig(func(_ context.Context, o containerapi.FindOptions) (*containerapi.Found, error) {
		opts.add(o)
		n.Add(1)
		return &containerapi.Found{Client: fake.Client()}, nil
	})
	r := activate(t, fake, cfg)
	if opts.len() != 1 || opts.at(0).Host != "" || opts.at(0).Kind != containerapi.KindDocker {
		t.Fatalf("the first search %+v", opts.all())
	}
	r.api.SetSetting("containers.host", "tcp://box:2376")
	r.wait("the second search", func() bool { return opts.len() == 2 && r.ext.status == statusConnected })
	if opts.at(1).Host != "tcp://box:2376" {
		t.Errorf("the second search %+v", opts.at(1))
	}
	r.api.SetSetting("containers.host", "tcp://box:2376")
	r.settle()
	if opts.len() != 2 {
		t.Errorf("the same host looked again: %d", opts.len())
	}
}

// TestAnEngineThatAnswersLateIsNotAdoptedAfterAnotherSearch starts a search that is slow and another that is quick: the answer of
// the old one is dropped, and its engine is closed.
func TestAnEngineThatAnswersLateIsNotAdoptedAfterAnotherSearch(t *testing.T) {
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	release := make(chan struct{})
	var calls atomic.Int32
	cfg := testConfig(func(ctx context.Context, _ containerapi.FindOptions) (*containerapi.Found, error) {
		if calls.Add(1) == 1 {
			<-release
		}
		return &containerapi.Found{Client: fake.Client(), Source: "call " + string(rune('0'+calls.Load()))}, nil
	})
	r := startRig(t, fake, cfg)
	r.wait("the first search to start", func() bool { return calls.Load() == 1 })
	r.ext.connect() // a second search while the first is out
	r.wait("the second answer", func() bool { return r.ext.status == statusConnected })
	close(release)
	r.settle()
	if r.ext.found == nil || r.ext.found.Source != "call 2" {
		t.Errorf("the engine in use: %+v", r.ext.found)
	}
}
