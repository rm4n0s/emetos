package containerui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// envFakePodmanService makes this test binary act as `podman system service` (the fake podman of the tests of the service, as in
// containerapi's tests): it serves the fake Podman on the socket it is given until SIGTERM.
const envFakePodmanService = "EMETOS_FAKE_PODMAN_SERVICE"

func TestMain(m *testing.M) {
	if os.Getenv(envFakePodmanService) != "" {
		os.Exit(fakePodmanService(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakePodmanService is the fake podman program; it returns the exit status.
func fakePodmanService(args []string) int {
	if len(args) != 4 || args[0] != "system" || args[1] != "service" || args[2] != "--time=0" || !strings.HasPrefix(args[3], "unix://") {
		fmt.Fprintln(os.Stderr, "fake podman: unexpected arguments:", strings.Join(args, " "))
		return 2
	}
	fake, err := containerapitest.ServeUnix(containerapi.KindPodman, containerapitest.FakeOptions{}, strings.TrimPrefix(args[3], "unix://"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake podman:", err)
		return 2
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	<-term
	fake.Close()
	return 0
}

// TestPodmanStartsAServiceOfItsOwnWhenNoSocketAnswers holds SPEC §10.20 "its own service when no socket answers": the extension, with
// the real search of the engine over folders of the test (no socket in any of them), starts `podman system service` through a fake
// podman program, shows the pods of that engine, and stops the service when the extension ends.
func TestPodmanStartsAServiceOfItsOwnWhenNoSocketAnswers(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte("#!/bin/sh\nexec \""+self+"\" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	socks, err := os.MkdirTemp("/tmp", "svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socks) })
	newFakeCLI(t, "podman").answer("system_connection", "[]", "", 0)

	var found *containerapi.Found
	cfg := podmanConfig(func(ctx context.Context, o containerapi.FindOptions) (*containerapi.Found, error) {
		o.DockerSocket, o.RootfulPodmanSocket = filepath.Join(socks, "none-docker.sock"), filepath.Join(socks, "none-rootful.sock")
		o.Service = containerapi.ServiceOptions{
			Getenv: func(k string) string {
				if k == "PATH" {
					return bin
				}
				return ""
			},
			Env: []string{envFakePodmanService + "=1"}, SocketDir: socks, PID: 4343,
		}
		f, err := containerapi.Find(ctx, o)
		found = f
		return f, err
	})
	api := fakeapi.New(t, New(cfg).Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
	e := New(cfg).(*ext)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	api.WaitFor("the engine", func() bool { return e.status == statusConnected && e.loads[resPods].loaded })
	if found == nil || found.Service == nil || found.Service.Socket != filepath.Join(socks, "podman-4343.sock") {
		t.Fatalf("no service of Emetos's own: %+v", found)
	}
	if !strings.Contains(found.Source, "a podman system service of Emetos's own") {
		t.Errorf("the engine came from %q", found.Source)
	}
	// the service answers as an engine: a pod made through the view's client shows in the view
	if _, err := e.client.CreatePod(context.Background(), containerapi.PodSpec{Name: "own"}); err != nil {
		t.Fatal(err)
	}
	api.WaitFor("the pod", func() bool { return len(e.pods) == 1 && e.pods[0].Name == "own" })
	done := found.Service.Done()

	cancel() // the extension ends: the service is stopped
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the service was not stopped when the extension ended")
	}
	if _, err := os.Stat(found.Service.Socket); !os.IsNotExist(err) {
		t.Errorf("the socket of the service is still there: %v", err)
	}
}

// TestPodmanAskedForAMissingProgramShowsTheReason holds that with no socket and no podman program the view says why, with its
// places, and offers Retry; nothing is started.
func TestPodmanAskedForAMissingProgramShowsTheReason(t *testing.T) {
	socks, err := os.MkdirTemp("/tmp", "svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socks) })
	newFakeCLI(t, "podman").answer("system_connection", "[]", "", 0)
	cfg := podmanConfig(func(ctx context.Context, o containerapi.FindOptions) (*containerapi.Found, error) {
		o.DockerSocket, o.RootfulPodmanSocket = filepath.Join(socks, "none-docker.sock"), filepath.Join(socks, "none-rootful.sock")
		o.Service = containerapi.ServiceOptions{Getenv: func(string) string { return "" }, SocketDir: socks}
		return containerapi.Find(ctx, o)
	})
	r := startRig(t, nil, cfg)
	r.wait("the missing engine", func() bool { return r.ext.status == statusMissing })
	if !strings.Contains(r.ext.reason, "no podman engine answers") || !strings.Contains(r.ext.reason, "the program podman is not on PATH") {
		t.Errorf("reason: %s", r.ext.reason)
	}
	if r.api.ContextKey("emetos.podman.connected") != false {
		t.Error("the view says it is connected")
	}
	// the Pods and the Connections views: Connections stays, so that another engine can be picked
	r.wait("the connections", func() bool { return len(r.labels(secConnections)) == 1 })
	if got := r.labels(secConnections); got[0] != "Automatic" {
		t.Errorf("connections %v", got)
	}
}

// TestTheViewStateKeepsThePickedConnection restores a picked connection and connects to its address.
func TestTheViewStateKeepsThePickedConnection(t *testing.T) {
	cli := newFakeCLI(t, "podman")
	cli.answer("system_connection", connectionsJSON, "", 0)
	fake := containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{})
	var hosts recorder[string]
	cfg := podmanConfig(func(_ context.Context, o containerapi.FindOptions) (*containerapi.Found, error) {
		hosts.add(o.Host)
		return &containerapi.Found{Client: fake.Client(), Source: "the fake engine"}, nil
	})
	r := startRig(t, fake, cfg)
	r.wait("the first connection", func() bool { return r.ext.status == statusConnected })
	state, _ := json.Marshal(map[string]any{"connection": "local", "connectionHost": "unix:///run/user/1000/podman/podman.sock"})
	if err := r.ext.restoreState(state); err != nil {
		t.Fatal(err)
	}
	r.wait("the second connection", func() bool { return hosts.len() == 2 && r.ext.status == statusConnected })
	if hosts.at(1) != "unix:///run/user/1000/podman/podman.sock" || r.ext.picked != "local" {
		t.Errorf("hosts %v picked %q", hosts.all(), r.ext.picked)
	}
	if err := r.ext.restoreState(state); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if hosts.len() != 2 {
		t.Errorf("the same state connected again: %v", hosts.all())
	}
}
