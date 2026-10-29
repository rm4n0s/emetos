package containerapitest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/errors"
)

// What marks a thing as made by a test (CLAUDE.md "Testing notes"): the label dev.emetos.test=1 and a name that starts with
// emetos-test-. scripts/container-test-cleanup.sh removes only things with the label.
const (
	// LabelKey and LabelValue are the label every test container, network, volume and pod carries.
	LabelKey   = "dev.emetos.test"
	LabelValue = "1"
	// NamePrefix starts the name of everything a test makes.
	NamePrefix = "emetos-test-"
	// Loopback is the only address a test publishes ports on.
	Loopback = "127.0.0.1"
)

// LabelFilter is the filter that lists only the things made by tests.
func LabelFilter() containerapi.Filters {
	return containerapi.Filters{"label": {LabelKey + "=" + LabelValue}}
}

// RandomName returns emetos-test-<random>.
func RandomName(t testing.TB) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("no random bytes for a test name: %v", err)
	}
	return NamePrefix + hex.EncodeToString(b[:])
}

// Harness runs a test against an engine, real or fake, and makes sure it touches only what it made itself.
type Harness struct {
	// T is the test.
	T testing.TB
	// Client talks to the engine.
	Client *containerapi.Client
	// Kind is the engine's kind.
	Kind containerapi.Kind
	// Real reports an engine of the user's, not a Fake.
	Real bool
	// Fake is the fake engine; nil for a real one.
	Fake *Fake
}

// NewFakeHarness makes a Harness on a fake engine.
func NewFakeHarness(t testing.TB, kind containerapi.Kind) *Harness {
	t.Helper()
	fake := NewFake(t, kind, FakeOptions{})
	h := &Harness{T: t, Client: fake.Client(), Kind: kind, Fake: fake}
	fake.AddImage(4<<20, ImageBusyBox)
	h.checkLeftoversAtEnd()
	t.Cleanup(h.Client.Close)
	return h
}

// RealDocker makes a Harness on the user's Docker, found as the Docker CLI would (the setting is none; DOCKER_HOST and the
// Docker context are honored). It skips the test when no engine answers, or when the engine is not on this computer (a
// tcp:// host): the tests create containers, and only a local engine is meant for that.
func RealDocker(t testing.TB) *Harness {
	t.Helper()
	return realEngine(t, containerapi.KindDocker)
}

// RealPodman makes a Harness on the user's Podman: its socket, or a podman system service of its own on a private socket
// when none answers (stopped when the test ends). It skips the test when there is no Podman.
func RealPodman(t testing.TB) *Harness {
	t.Helper()
	return realEngine(t, containerapi.KindPodman)
}

// realEngine implements RealDocker and RealPodman.
func realEngine(t testing.TB, kind containerapi.Kind) *Harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	found, err := containerapi.Find(ctx, containerapi.FindOptions{
		Kind: kind,
		Service: containerapi.ServiceOptions{
			SocketDir: shortDir(t),
		},
	})
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && (appErr.Tag == "ContainerAPIEngineNotFound") {
			t.Skipf("no %s engine answers, skipping: %v", kind, err)
		}
		t.Fatalf("looking for the %s engine: %v", kind, err)
	}
	t.Cleanup(found.Close) // registered first, so it runs after every cleanup of the things the test made
	if found.Client.Endpoint().Network != "unix" {
		t.Skipf("the %s engine is at %s, not a local socket; the tests make containers and run only on a local engine", kind, found.Client.Endpoint())
	}
	h := &Harness{T: t, Client: found.Client, Kind: kind, Real: true}
	h.checkLeftoversAtEnd()
	return h
}

// checkLeftoversAtEnd registers the last check of the test: nothing labelled is left. It is registered before any thing is
// made, so that it runs after their cleanups.
func (h *Harness) checkLeftoversAtEnd() {
	h.T.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		left, err := h.Leftovers(ctx)
		if err != nil {
			h.T.Errorf("listing what the test left behind: %v", err)
			return
		}
		if len(left) > 0 {
			h.T.Errorf("the test left labelled things behind: %s (scripts/container-test-cleanup.sh removes them)", strings.Join(left, ", "))
		}
	})
}

// Context returns a context that ends after d or when the test ends.
func (h *Harness) Context(d time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	h.T.Cleanup(cancel)
	return ctx
}

// EnsureImage makes sure an image is there, pulling it when it is not. It refuses an image that PinnedImages does not list.
func (h *Harness) EnsureImage(ctx context.Context, ref string) {
	h.T.Helper()
	if !IsPinned(ref) {
		h.T.Fatalf("%s is not one of the pinned images in images.go; the tests pull only those", ref)
	}
	if _, err := h.Client.InspectImage(ctx, ref); err == nil {
		return
	} else if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ContainerAPINotFound" {
		h.T.Fatalf("looking for the image %s: %v", ref, err)
	}
	if err := h.Client.PullImage(ctx, ref, nil); err != nil {
		h.T.Fatalf("pulling the image %s: %v", ref, err)
	}
}

// Container makes a container from spec, without starting it, and removes it when the test ends. The spec is made safe: the
// name starts with emetos-test- (a random one when none is given), the label is added, ports are published on 127.0.0.1
// only, and host paths are not mounted.
func (h *Harness) Container(ctx context.Context, spec containerapi.ContainerSpec) string {
	h.T.Helper()
	if spec.Name == "" {
		spec.Name = RandomName(h.T)
	}
	if !strings.HasPrefix(spec.Name, NamePrefix) {
		h.T.Fatalf("a test container is named %s...; %q is not", NamePrefix, spec.Name)
	}
	labels := map[string]string{LabelKey: LabelValue}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	spec.Labels = labels
	ports := make([]containerapi.PortMapping, len(spec.Ports))
	for i, p := range spec.Ports {
		if p.HostIP != "" && p.HostIP != Loopback {
			h.T.Fatalf("a test container publishes ports on %s only, not on %s", Loopback, p.HostIP)
		}
		p.HostIP = Loopback
		ports[i] = p
	}
	spec.Ports = ports
	for _, b := range spec.Binds {
		if src, _, _ := strings.Cut(b, ":"); strings.HasPrefix(src, "/") || strings.HasPrefix(src, ".") {
			h.T.Fatalf("a test container does not mount a path of the host (%s); use a volume made by the harness", b)
		}
	}
	res, err := h.Client.CreateContainer(ctx, spec)
	if err != nil {
		h.T.Fatalf("creating the container %s: %v", spec.Name, err)
	}
	id := res.ID
	h.T.Cleanup(func() { h.removeContainer(id) })
	return id
}

// StartedContainer is Container followed by the start.
func (h *Harness) StartedContainer(ctx context.Context, spec containerapi.ContainerSpec) string {
	h.T.Helper()
	id := h.Container(ctx, spec)
	if err := h.Client.StartContainer(ctx, id); err != nil {
		h.T.Fatalf("starting the container %s: %v", id, err)
	}
	return id
}

// cleanupContext is the context of a cleanup: the test's own has ended already.
func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// removeContainer removes a container the harness made, after checking that it carries the label.
func (h *Harness) removeContainer(id string) {
	ctx, cancel := cleanupContext()
	defer cancel()
	info, err := h.Client.InspectContainer(ctx, id)
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ContainerAPINotFound" {
			return // removed by the test already
		}
		h.T.Errorf("cleanup: looking at the container %s: %v", id, err)
		return
	}
	if info.Labels[LabelKey] != LabelValue {
		h.T.Errorf("cleanup: refusing to remove the container %s: it has no %s label", id, LabelKey)
		return
	}
	if err := h.Client.RemoveContainer(ctx, id, containerapi.RemoveOptions{Force: true, Volumes: true}); err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ContainerAPINotFound" {
			return
		}
		h.T.Errorf("cleanup: removing the container %s: %v", id, err)
	}
}

// ImageName returns emetos-test-<random>:latest, the name of an image a test builds with the CLI. The image has to carry the label
// in its Dockerfile (LABEL dev.emetos.test=1); RemoveImageAtEnd removes it by that name when the test ends, after looking at the label.
func (h *Harness) ImageName() string {
	h.T.Helper()
	return RandomName(h.T) + ":latest"
}

// RemoveImageAtEnd registers the removal of an image the test builds, by its name, for when the test ends (also when it fails). The
// image is looked at first and is removed only when it carries the label: a name that is the user's own is refused.
func (h *Harness) RemoveImageAtEnd(name string) {
	h.T.Helper()
	if !strings.HasPrefix(name, NamePrefix) {
		h.T.Fatalf("an image of a test is named %s...: %q", NamePrefix, name)
	}
	h.T.Cleanup(func() {
		ctx, cancel := cleanupContext()
		defer cancel()
		info, err := h.Client.InspectImage(ctx, name)
		if err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ContainerAPINotFound" {
				return // removed by the test already, or never built
			}
			h.T.Errorf("cleanup: looking at the image %s: %v", name, err)
			return
		}
		if info.Labels[LabelKey] != LabelValue {
			h.T.Errorf("cleanup: refusing to remove the image %s: it has no %s label", name, LabelKey)
			return
		}
		if _, err := h.Client.RemoveImage(ctx, info.ID, containerapi.RemoveImageOptions{Force: true}); err != nil {
			h.T.Errorf("cleanup: removing the image %s: %v", name, err)
		}
	})
}

// Network makes a labelled network and removes it when the test ends. It returns its name.
func (h *Harness) Network(ctx context.Context) string {
	h.T.Helper()
	name := RandomName(h.T)
	if _, err := h.Client.CreateNetwork(ctx, containerapi.NetworkSpec{Name: name, Labels: map[string]string{LabelKey: LabelValue}}); err != nil {
		h.T.Fatalf("creating the network %s: %v", name, err)
	}
	h.T.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		n, err := h.Client.InspectNetwork(cctx, name)
		if err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ContainerAPINotFound" {
				return
			}
			h.T.Errorf("cleanup: looking at the network %s: %v", name, err)
			return
		}
		if n.Labels[LabelKey] != LabelValue {
			h.T.Errorf("cleanup: refusing to remove the network %s: it has no %s label", name, LabelKey)
			return
		}
		if err := h.Client.RemoveNetwork(cctx, name); err != nil {
			h.T.Errorf("cleanup: removing the network %s: %v", name, err)
		}
	})
	return name
}

// Volume makes a labelled volume and removes it when the test ends. It returns its name.
func (h *Harness) Volume(ctx context.Context) string {
	h.T.Helper()
	name := RandomName(h.T)
	if _, err := h.Client.CreateVolume(ctx, containerapi.VolumeSpec{Name: name, Labels: map[string]string{LabelKey: LabelValue}}); err != nil {
		h.T.Fatalf("creating the volume %s: %v", name, err)
	}
	h.T.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		v, err := h.Client.InspectVolume(cctx, name)
		if err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ContainerAPINotFound" {
				return
			}
			h.T.Errorf("cleanup: looking at the volume %s: %v", name, err)
			return
		}
		if v.Labels[LabelKey] != LabelValue {
			h.T.Errorf("cleanup: refusing to remove the volume %s: it has no %s label", name, LabelKey)
			return
		}
		if err := h.Client.RemoveVolume(cctx, name, true); err != nil {
			h.T.Errorf("cleanup: removing the volume %s: %v", name, err)
		}
	})
	return name
}

// Pod makes a labelled pod (Podman) and removes it, with its containers, when the test ends. It returns its name.
func (h *Harness) Pod(ctx context.Context) string {
	h.T.Helper()
	name := RandomName(h.T)
	if _, err := h.Client.CreatePod(ctx, containerapi.PodSpec{Name: name, Labels: map[string]string{LabelKey: LabelValue}}); err != nil {
		h.T.Fatalf("creating the pod %s: %v", name, err)
	}
	h.T.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		p, err := h.Client.InspectPod(cctx, name)
		if err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ContainerAPINotFound" {
				return
			}
			h.T.Errorf("cleanup: looking at the pod %s: %v", name, err)
			return
		}
		if p.Labels[LabelKey] != LabelValue {
			h.T.Errorf("cleanup: refusing to remove the pod %s: it has no %s label", name, LabelKey)
			return
		}
		if err := h.Client.RemovePod(cctx, name, true); err != nil {
			h.T.Errorf("cleanup: removing the pod %s: %v", name, err)
		}
	})
	return name
}

// Leftovers lists the labelled containers, networks, volumes, images and pods that exist now, as "container emetos-test-ab12".
// Only the label filter is used to list, so nothing of the user's is looked at.
func (h *Harness) Leftovers(ctx context.Context) ([]string, error) {
	var out []string
	filter := LabelFilter()
	cs, err := h.Client.ListContainers(ctx, containerapi.ListContainersOptions{All: true, Filters: filter})
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		out = append(out, "container "+c.Name())
	}
	ns, err := h.Client.ListNetworks(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		out = append(out, "network "+n.Name)
	}
	vs, err := h.Client.ListVolumes(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, v := range vs {
		out = append(out, "volume "+v.Name)
	}
	ims, err := h.Client.ListImages(ctx, containerapi.ListImagesOptions{All: true, Filters: filter})
	if err != nil {
		return nil, err
	}
	for _, im := range ims {
		name := containerapi.ShortID(im.ID)
		if len(im.RepoTags) > 0 {
			name = im.RepoTags[0]
		}
		out = append(out, "image "+name)
	}
	if h.Kind == containerapi.KindPodman {
		ps, err := h.Client.ListPods(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			out = append(out, "pod "+p.Name)
		}
	}
	return out, nil
}
