package containerapitest

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/errors"
)

// busyScript is what the suite's container runs: it prints a line on each output, then a line every second.
const busyScript = "echo out; echo err >&2; while true; do sleep 1; echo tick; done"

// wantTag is tagOf without the result.
func wantTag(t testing.TB, err error, tag string) {
	t.Helper()
	_ = tagOf(t, err, tag)
}

// eventually polls cond until it is true or d has passed.
func eventually(t testing.TB, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout after %s waiting for %s", d, what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// tagOf fails the test unless err is an error of this package with the tag, and returns it.
func tagOf(t testing.TB, err error, tag string) *errors.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", tag)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("error %T (%v) is not an *errors.Error", err, err)
	}
	if appErr.Tag != tag {
		t.Fatalf("error tag = %s (%v), want %s", appErr.Tag, err, tag)
	}
	return appErr
}

// TestEngine runs the suite of the engine client against h: the same calls against the fake engine (in the default tests)
// and against the user's real Docker and Podman (build tags emetos_docker and emetos_podman). It runs a labelled container of
// the pinned BusyBox image and exercises the list, inspect, logs, exec, events, stop, start, restart, kill, rename and remove
// calls, a network, a volume, an image tag, "system df" and, for Podman, a pod. Everything it makes carries the label
// dev.emetos.test=1 and is removed when the test ends; the harness fails the test when something labelled is left.
// Prune calls are not part of it: against a real engine they act on whatever the engine decides is unused.
func TestEngine(t *testing.T, h *Harness) {
	t.Helper()
	c := h.Client
	ctx := h.Context(5 * time.Minute)

	t.Run("negotiate", func(t *testing.T) {
		info, err := c.Negotiate(ctx)
		if err != nil {
			t.Fatalf("Negotiate: %v", err)
		}
		if info.Kind != h.Kind || info.Version == "" || info.APIVersion == "" || info.Engine == "" {
			t.Errorf("ServerInfo = %+v", info)
		}
	})
	h.EnsureImage(ctx, ImageBusyBox)

	var id string
	name := RandomName(t)
	t.Run("create and start with a published port", func(t *testing.T) {
		id = h.StartedContainer(ctx, containerapi.ContainerSpec{
			Name: name, Image: ImageBusyBox, Cmd: []string{"sh", "-c", busyScript},
			Env:   []string{"EMETOS_TEST=1"},
			Ports: []containerapi.PortMapping{{ContainerPort: 80}},
		})
		if id == "" {
			t.Fatal("no container id")
		}
	})
	if id == "" {
		return
	}

	t.Run("list finds it by label", func(t *testing.T) {
		list, err := c.ListContainers(ctx, containerapi.ListContainersOptions{Filters: LabelFilter()})
		if err != nil {
			t.Fatal(err)
		}
		var found *containerapi.Container
		for i := range list {
			if list[i].ID == id {
				found = &list[i]
			}
		}
		if found == nil {
			t.Fatalf("the running container %s is not in %d listed", id, len(list))
		}
		if found.Name() != name || found.State != "running" || found.Labels[LabelKey] != LabelValue {
			t.Errorf("listed container = %+v", *found)
		}
		published := false
		for _, p := range found.Ports {
			if p.PrivatePort == 80 && p.IP == Loopback && p.PublicPort != 0 {
				published = true
			}
		}
		if !published {
			t.Errorf("port 80 is not published on %s: %+v", Loopback, found.Ports)
		}
	})

	t.Run("inspect", func(t *testing.T) {
		info, err := c.InspectContainer(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if info.ID != id || info.Name != name || !info.State.Running || info.Labels[LabelKey] != LabelValue || len(info.Raw) == 0 {
			t.Errorf("inspect = %+v", info)
		}
		if len(info.Ports["80/tcp"]) != 1 || info.Ports["80/tcp"][0].HostIP != Loopback {
			t.Errorf("published ports = %+v", info.Ports)
		}
	})

	t.Run("logs are split into stdout and stderr", func(t *testing.T) {
		var mu sync.Mutex
		got := map[containerapi.Stream][]string{}
		eventually(t, 20*time.Second, "the first lines of the logs", func() bool {
			mu.Lock()
			got = map[containerapi.Stream][]string{}
			mu.Unlock()
			err := c.Logs(ctx, id, containerapi.LogsOptions{Timestamps: true}, func(l containerapi.LogLine) error {
				if l.Time.IsZero() {
					t.Errorf("line %q has no timestamp", l.Text)
				}
				mu.Lock()
				got[l.Stream] = append(got[l.Stream], l.Text)
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			return len(got[containerapi.StreamStdout]) > 0 && len(got[containerapi.StreamStderr]) > 0
		})
		if got[containerapi.StreamStdout][0] != "out" || got[containerapi.StreamStderr][0] != "err" {
			t.Errorf("lines = %v", got)
		}
	})

	t.Run("logs follow and end with the context", func(t *testing.T) {
		fctx, cancel := context.WithCancel(ctx)
		defer cancel()
		ticks := make(chan struct{}, 8)
		done := make(chan error, 1)
		go func() {
			done <- c.Logs(fctx, id, containerapi.LogsOptions{Follow: true, Tail: 5}, func(l containerapi.LogLine) error {
				if l.Text == "tick" {
					select {
					case ticks <- struct{}{}:
					default:
					}
				}
				return nil
			})
		}()
		select {
		case <-ticks:
		case <-time.After(20 * time.Second):
			t.Fatal("no tick line arrived while following")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Logs after cancel = %v, want nil", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Logs did not return after its context was cancelled")
		}
	})

	t.Run("exec captures output and the exit code", func(t *testing.T) {
		res, err := c.Exec(ctx, id, containerapi.ExecOptions{Cmd: []string{"sh", "-c", "echo hi; echo bad >&2; exit 3"}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(res.Stdout) != "hi" || strings.TrimSpace(res.Stderr) != "bad" || res.ExitCode != 3 || res.Running || res.Truncated {
			t.Errorf("exec = %+v", res)
		}
	})

	t.Run("events carry the stop, and end with the context", func(t *testing.T) {
		ectx, cancel := context.WithCancel(ctx)
		defer cancel()
		ready := make(chan struct{})
		seen := make(chan containerapi.Event, 64)
		done := make(chan error, 1)
		go func() {
			done <- c.Events(ectx, containerapi.EventsOptions{
				Filters: containerapi.Filters{"type": {"container"}, "label": {LabelKey + "=" + LabelValue}},
				Ready:   func() { close(ready) },
			}, func(ev containerapi.Event) error {
				select {
				case seen <- ev:
				default:
				}
				return nil
			})
		}()
		select {
		case <-ready:
		case err := <-done:
			t.Fatalf("Events ended before it was ready: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("the event stream was not ready")
		}
		if err := c.StopContainer(ctx, id, time.Second); err != nil {
			t.Fatal(err)
		}
		deadline := time.After(30 * time.Second)
		for stopped := false; !stopped; {
			select {
			case ev := <-seen:
				if ev.ID == id && (ev.Action == "die" || ev.Action == "died" || ev.Action == "stop") {
					stopped = true
				}
			case <-deadline:
				t.Fatal("no die or stop event arrived")
			}
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Events after cancel = %v, want nil", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Events did not return after its context was cancelled")
		}
	})

	t.Run("stop, start, restart, kill and rename", func(t *testing.T) {
		running := func(want bool) func() bool {
			return func() bool {
				info, err := c.InspectContainer(ctx, id)
				return err == nil && info.State.Running == want
			}
		}
		eventually(t, 15*time.Second, "the container to be stopped", running(false))
		if err := c.StopContainer(ctx, id, time.Second); err != nil {
			t.Errorf("stopping a stopped container: %v (want no error)", err)
		}
		if err := c.StartContainer(ctx, id); err != nil {
			t.Fatal(err)
		}
		eventually(t, 15*time.Second, "the container to run", running(true))
		if err := c.StartContainer(ctx, id); err != nil {
			t.Errorf("starting a running container: %v (want no error)", err)
		}
		if err := c.RestartContainer(ctx, id, time.Second); err != nil {
			t.Fatal(err)
		}
		eventually(t, 15*time.Second, "the container to run after the restart", running(true))
		renamed := RandomName(t)
		if err := c.RenameContainer(ctx, id, renamed); err != nil {
			t.Fatal(err)
		}
		if info, err := c.InspectContainer(ctx, id); err != nil || info.Name != renamed {
			t.Errorf("after the rename: %v, %v", info, err)
		}
		if err := c.KillContainer(ctx, id, "SIGKILL"); err != nil {
			t.Fatal(err)
		}
		eventually(t, 15*time.Second, "the container to die", running(false))
	})

	t.Run("remove needs force for a running container", func(t *testing.T) {
		other := h.StartedContainer(ctx, containerapi.ContainerSpec{Image: ImageBusyBox, Cmd: []string{"sh", "-c", busyScript}})
		err := c.RemoveContainer(ctx, other, containerapi.RemoveOptions{})
		if h.Kind == containerapi.KindPodman {
			wantTag(t, err, "ContainerAPIEngineError") // libpod answers 500 for this
		} else {
			wantTag(t, err, "ContainerAPIConflict")
		}
		if err := c.RemoveContainer(ctx, other, containerapi.RemoveOptions{Force: true}); err != nil {
			t.Fatal(err)
		}
		_, err = c.InspectContainer(ctx, other)
		appErr := tagOf(t, err, "ContainerAPINotFound")
		if appErr.Message == "" {
			t.Error("the not-found error has no message from the engine")
		}
	})

	t.Run("a network", func(t *testing.T) {
		netName := h.Network(ctx)
		list, err := c.ListNetworks(ctx, LabelFilter())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, n := range list {
			if n.Name == netName && n.Labels[LabelKey] == LabelValue {
				found = true
			}
		}
		if !found {
			t.Errorf("the network %s is not listed: %+v", netName, list)
		}
		n, err := c.InspectNetwork(ctx, netName)
		if err != nil || n.Name != netName || len(n.Raw) == 0 {
			t.Errorf("InspectNetwork = %+v, %v", n, err)
		}
		if err := c.RemoveNetwork(ctx, netName); err != nil {
			t.Fatal(err)
		}
		_, err = c.InspectNetwork(ctx, netName)
		wantTag(t, err, "ContainerAPINotFound")
	})

	t.Run("a volume", func(t *testing.T) {
		volName := h.Volume(ctx)
		list, err := c.ListVolumes(ctx, LabelFilter())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range list {
			if v.Name == volName && v.Labels[LabelKey] == LabelValue {
				found = true
			}
		}
		if !found {
			t.Errorf("the volume %s is not listed: %+v", volName, list)
		}
		v, err := c.InspectVolume(ctx, volName)
		if err != nil || v.Name != volName || len(v.Raw) == 0 {
			t.Errorf("InspectVolume = %+v, %v", v, err)
		}
		if err := c.RemoveVolume(ctx, volName, false); err != nil {
			t.Fatal(err)
		}
		_, err = c.InspectVolume(ctx, volName)
		wantTag(t, err, "ContainerAPINotFound")
	})

	t.Run("images: inspect, history, list, tag and untag", func(t *testing.T) {
		img, err := c.InspectImage(ctx, ImageBusyBox)
		if err != nil || img.ID == "" || len(img.Raw) == 0 {
			t.Fatalf("InspectImage = %+v, %v", img, err)
		}
		hist, err := c.ImageHistory(ctx, ImageBusyBox)
		if err != nil || len(hist) == 0 {
			t.Errorf("ImageHistory = %v, %v", hist, err)
		}
		repo := "localhost/" + RandomName(t)
		if err := c.TagImage(ctx, ImageBusyBox, repo, "1"); err != nil {
			t.Fatal(err)
		}
		// the new name is taken off again below; the image itself stays
		list, err := c.ListImages(ctx, containerapi.ListImagesOptions{Filters: containerapi.Filters{"reference": {repo + ":1"}}})
		if err != nil || len(list) != 1 {
			t.Errorf("ListImages by reference = %v, %v", list, err)
		}
		res, err := c.RemoveImage(ctx, repo+":1", containerapi.RemoveImageOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Untagged) == 0 || len(res.Deleted) != 0 {
			t.Errorf("RemoveImage of a name = %+v, want the name untagged and no image deleted", res)
		}
		if _, err := c.InspectImage(ctx, ImageBusyBox); err != nil {
			t.Errorf("the pinned image is gone after taking off the new name: %v", err)
		}
	})

	t.Run("disk usage", func(t *testing.T) {
		du, err := c.DiskUsage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if du.Images.Count < 1 || du.Images.Size <= 0 {
			t.Errorf("DiskUsage images = %+v", du.Images)
		}
	})

	if h.Kind == containerapi.KindPodman {
		t.Run("a pod with a container", func(t *testing.T) {
			podName := h.Pod(ctx)
			pid := h.Container(ctx, containerapi.ContainerSpec{Image: ImageBusyBox, Cmd: []string{"sh", "-c", busyScript}, Pod: podName})
			if err := c.PodDo(ctx, podName, containerapi.PodStart); err != nil {
				t.Fatal(err)
			}
			p, err := c.InspectPod(ctx, podName)
			if err != nil || p.Name != podName || len(p.Raw) == 0 {
				t.Fatalf("InspectPod = %+v, %v", p, err)
			}
			listed, err := c.ListPods(ctx, LabelFilter())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, lp := range listed {
				for _, pc := range lp.Containers {
					if lp.Name == podName && pc.ID == pid {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("the container %s is not listed in the pod %s: %+v", pid, podName, listed)
			}
			if err := c.StopPod(ctx, podName, time.Second); err != nil {
				t.Fatal(err)
			}
			if err := c.RemovePod(ctx, podName, false); err != nil {
				t.Fatal(err)
			}
			_, err = c.InspectPod(ctx, podName)
			wantTag(t, err, "ContainerAPINotFound")
		})
	}
}
