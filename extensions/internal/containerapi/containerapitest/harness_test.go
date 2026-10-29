package containerapitest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/errors"
)

// fatal is what the recorder panics with when the harness calls Fatalf.
type fatal struct{}

// recorder stands in for *testing.T so that a test can look at what the harness refuses: Fatalf and Errorf are recorded
// (Fatalf stops the code under test with a panic, as the real one stops the goroutine), and the cleanups are kept to be run
// by the test, last first.
type recorder struct {
	testing.TB
	msgs     []string
	cleanups []func()
}

func (r *recorder) Helper()                   {}
func (r *recorder) Errorf(f string, a ...any) { r.msgs = append(r.msgs, fmt.Sprintf(f, a...)) }
func (r *recorder) Fatalf(f string, a ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(f, a...))
	panic(fatal{})
}
func (r *recorder) Fatal(a ...any)    { r.msgs = append(r.msgs, fmt.Sprint(a...)); panic(fatal{}) }
func (r *recorder) Cleanup(fn func()) { r.cleanups = append(r.cleanups, fn) }
func (r *recorder) runCleanups() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
	r.cleanups = nil
}

// try runs fn and reports whether it was stopped by Fatalf.
func try(fn func()) (stopped bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(fatal); !ok {
				panic(r)
			}
			stopped = true
		}
	}()
	fn()
	return false
}

// harnessOn makes a harness on a fake engine whose Fatalf and Errorf are recorded.
func harnessOn(t *testing.T, kind containerapi.Kind) (*containerapitest.Harness, *recorder) {
	t.Helper()
	fake := containerapitest.NewFake(t, kind, containerapitest.FakeOptions{})
	rec := &recorder{TB: t}
	h := &containerapitest.Harness{T: rec, Client: fake.Client(), Kind: kind, Fake: fake}
	t.Cleanup(h.Client.Close)
	fake.AddImage(1, containerapitest.ImageBusyBox)
	return h, rec
}

func TestHarnessRefusesWhatItShouldNotTouch(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		do   func(h *containerapitest.Harness)
		msg  string
	}{
		{"an image that is not pinned", func(h *containerapitest.Harness) { h.EnsureImage(ctx, "docker.io/library/nginx:latest") }, "not one of the pinned images"},
		{"a container with a name that does not start with the prefix", func(h *containerapitest.Harness) {
			h.Container(ctx, containerapi.ContainerSpec{Name: "my-database", Image: containerapitest.ImageBusyBox})
		}, "is not"},
		{"a port published on every address", func(h *containerapitest.Harness) {
			h.Container(ctx, containerapi.ContainerSpec{Image: containerapitest.ImageBusyBox, Ports: []containerapi.PortMapping{{HostIP: "0.0.0.0", ContainerPort: 80}}})
		}, "127.0.0.1 only"},
		{"a bind mount of a path of the host", func(h *containerapitest.Harness) {
			h.Container(ctx, containerapi.ContainerSpec{Image: containerapitest.ImageBusyBox, Binds: []string{"/home:/home"}})
		}, "does not mount a path of the host"},
		{"a relative bind mount", func(h *containerapitest.Harness) {
			h.Container(ctx, containerapi.ContainerSpec{Image: containerapitest.ImageBusyBox, Binds: []string{"./data:/data"}})
		}, "does not mount a path of the host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, rec := harnessOn(t, containerapi.KindDocker)
			if !try(func() { tc.do(h) }) {
				t.Fatal("the harness let it through")
			}
			if len(rec.msgs) != 1 || !strings.Contains(rec.msgs[0], tc.msg) {
				t.Errorf("messages = %q, want one with %q", rec.msgs, tc.msg)
			}
			if list, _ := h.Client.ListContainers(ctx, containerapi.ListContainersOptions{All: true}); len(list) != 0 {
				t.Errorf("%d containers were made", len(list))
			}
		})
	}
}

func TestHarnessMakesSafeContainersAndRemovesThem(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []containerapi.Kind{containerapi.KindDocker, containerapi.KindPodman} {
		t.Run(string(kind), func(t *testing.T) {
			h, rec := harnessOn(t, kind)
			h.Fake.ResetRequests()
			id := h.StartedContainer(ctx, containerapi.ContainerSpec{
				Image: containerapitest.ImageBusyBox, Labels: map[string]string{"extra": "1"},
				Ports: []containerapi.PortMapping{{ContainerPort: 80}, {HostIP: "127.0.0.1", ContainerPort: 81}},
			})
			info, err := h.Client.InspectContainer(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(info.Name, containerapitest.NamePrefix) || len(info.Name) <= len(containerapitest.NamePrefix) {
				t.Errorf("name = %q", info.Name)
			}
			if info.Labels[containerapitest.LabelKey] != containerapitest.LabelValue || info.Labels["extra"] != "1" {
				t.Errorf("labels = %v", info.Labels)
			}
			for port, bindings := range info.Ports {
				for _, b := range bindings {
					if b.HostIP != "127.0.0.1" {
						t.Errorf("%s is published on %q", port, b.HostIP)
					}
				}
			}
			net := h.Network(ctx)
			vol := h.Volume(ctx)
			if !strings.HasPrefix(net, containerapitest.NamePrefix) || !strings.HasPrefix(vol, containerapitest.NamePrefix) {
				t.Errorf("network %q, volume %q", net, vol)
			}
			var pod string
			if kind == containerapi.KindPodman {
				pod = h.Pod(ctx)
				h.Container(ctx, containerapi.ContainerSpec{Image: containerapitest.ImageBusyBox, Pod: pod})
			}
			left, err := h.Leftovers(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := 3 // the container, the network, the volume
			if kind == containerapi.KindPodman {
				want = 5 // and the pod and its container
			}
			if len(left) != want {
				t.Errorf("leftovers = %v, want %d", left, want)
			}
			// the cleanups remove it all, and nothing is reported
			rec.runCleanups()
			if len(rec.msgs) != 0 {
				t.Errorf("the cleanup reported: %q", rec.msgs)
			}
			if left, _ := h.Leftovers(ctx); len(left) != 0 {
				t.Errorf("after the cleanup: %v", left)
			}
			// every request that removed something was made by id or name of a thing the harness had made: no list was asked
			// for without the label
			for _, r := range h.Fake.Requests() {
				if r.Method == "GET" && (strings.HasSuffix(r.Path, "/json") || r.Path == "/networks" || r.Path == "/volumes") && !strings.Contains(r.Path, "/containers/") && !strings.Contains(r.Path, "/pods/") && !strings.Contains(r.Path, "/networks/") && !strings.Contains(r.Path, "/volumes/") {
					if !strings.Contains(r.Query, "dev.emetos.test") {
						t.Errorf("an unfiltered list: %s %s ?%s", r.Method, r.Path, r.Query)
					}
				}
			}
		})
	}
}

func TestHarnessReportsWhatTheTestLeftBehind(t *testing.T) {
	// NewFakeHarness registers the check that runs after every other cleanup; a recorder takes the place of the test so that
	// the report can be read
	rec := &recorder{TB: t}
	h := containerapitest.NewFakeHarness(rec, containerapi.KindDocker)
	h.Fake.AddContainer("emetos-test-forgotten", containerapitest.ImageBusyBox, map[string]string{containerapitest.LabelKey: containerapitest.LabelValue}, "exited")
	h.Fake.AddContainer("users-own", containerapitest.ImageBusyBox, nil, "exited")
	rec.runCleanups()
	if len(rec.msgs) != 1 || !strings.Contains(rec.msgs[0], "container emetos-test-forgotten") || strings.Contains(rec.msgs[0], "users-own") {
		t.Errorf("messages = %q, want one that names the forgotten container and not the user's", rec.msgs)
	}
}

func TestHarnessCleanupLeavesTheUsersThingsAlone(t *testing.T) {
	ctx := context.Background()
	h, rec := harnessOn(t, containerapi.KindDocker)
	id := h.Container(ctx, containerapi.ContainerSpec{Image: containerapitest.ImageBusyBox})
	other := h.Fake.AddContainer("users-own", containerapitest.ImageBusyBox, nil, "running")
	rec.runCleanups()
	if len(rec.msgs) != 0 {
		t.Errorf("the cleanup reported %q", rec.msgs)
	}
	if _, err := h.Client.InspectContainer(ctx, other); err != nil {
		t.Errorf("the user's container is gone after the cleanup: %v", err)
	}
	if _, err := h.Client.InspectContainer(ctx, id); err == nil {
		t.Error("the harness's own container is still there")
	}
}

func TestPinnedImages(t *testing.T) {
	if len(containerapitest.PinnedImages) == 0 {
		t.Fatal("no pinned images")
	}
	for _, ref := range containerapitest.PinnedImages {
		name, tag, hasTag := strings.Cut(ref, ":")
		if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
			name, tag, hasTag = ref[:i], ref[i+1:], true
		}
		if !hasTag || tag == "latest" || tag == "" {
			t.Errorf("%s: a pinned image needs a version tag, not %q", ref, tag)
		}
		if !strings.HasPrefix(name, "docker.io/") && !strings.Contains(strings.SplitN(name, "/", 2)[0], ".") {
			t.Errorf("%s: write the registry, so that Podman does not ask which one", ref)
		}
		if !containerapitest.IsPinned(ref) {
			t.Errorf("IsPinned(%s) = false", ref)
		}
	}
	if containerapitest.IsPinned("docker.io/library/busybox:latest") || containerapitest.IsPinned("") {
		t.Error("IsPinned says yes to an image that is not listed")
	}
}

func TestServeUnixReportsASocketItCannotMake(t *testing.T) {
	_, err := containerapitest.ServeUnix(containerapi.KindDocker, containerapitest.FakeOptions{}, "/nonexistent/folder/engine.sock")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ServeUnix.FakeEngineListenFailed") {
		t.Fatalf("err = %v", err)
	}
}

func TestRandomNamesAreDistinctAndPrefixed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		n := containerapitest.RandomName(t)
		if !strings.HasPrefix(n, containerapitest.NamePrefix) || seen[n] {
			t.Fatalf("name %q (seen: %v)", n, seen[n])
		}
		seen[n] = true
	}
}
