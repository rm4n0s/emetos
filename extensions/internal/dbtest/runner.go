// Package dbtest runs throwaway database servers in containers for the tests of the database extensions (SQL, MongoDB, Redis) that
// need a real server (build tag emetos_docker, CLAUDE.md "Testing notes"); it was extensions/sql/sqltest until step 20.1, which
// moved it here because an extension may not import another one. The containers are made through the harness of the container engine client
// (extensions/internal/containerapi/containerapitest), which keeps the rules of those tests:
//
//   - only the pinned images of containerapitest/images.go are pulled, never "latest";
//   - every container is named emetos-test-<random>, carries the label dev.emetos.test=1, publishes its port on 127.0.0.1 only and
//     mounts nothing of the host;
//   - every container is removed when the test ends, also when it fails, and the harness fails the test when something labelled is
//     left; scripts/container-test-cleanup.sh removes what a killed run left (it removes only what has the label);
//   - the user's own containers, images, networks and volumes are never listed, stopped, removed or changed;
//   - the password of a server is random for each test and goes to the engine in the body of the create call, so that it is in no
//     command line and in no file.
//
// There is no server for SQL Server: its image starts only when the license terms are accepted, and a test must not accept terms
// for the user. SQL Server's driver and dialect are tested against fakes.
//
// The containers are started through the Runner interface: Docker and Podman run them on the user's engine, OnHarness on any
// harness (a fake engine in the tests of this package).
package dbtest

import (
	"context"
	"crypto/rand"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// Spec is a server to run.
type Spec struct {
	// Image is one of containerapitest.PinnedImages.
	Image string
	// Port is the port the server listens on inside the container.
	Port int
	// Env is the environment of the container.
	Env map[string]string
	// Args follow the image name (the arguments of the server).
	Args []string
}

// Server is a running container.
type Server struct {
	// Name is the container's name.
	Name string
	// Host and Port are where the server's port is published: on the loopback address.
	Host string
	Port int
}

// Runner starts a server for a test and removes it when the test ends.
type Runner interface {
	// Start runs the server and returns where it is published. It makes the test fail, or skips it, when it cannot.
	Start(t testing.TB, spec Spec) Server
}

// RandomName returns emetos-test-<random>.
func RandomName(t testing.TB) string {
	t.Helper()
	return containerapitest.RandomName(t)
}

// RandomPassword returns a password for a server of one test: 20 random letters and digits and a suffix that satisfies the strictest
// rule of the servers (a password with upper and lower case letters, a digit and a symbol).
func RandomPassword(t testing.TB) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var b strings.Builder
	for range 20 {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			t.Fatalf("no random bytes for a password: %v", err)
		}
		b.WriteByte(alphabet[n.Int64()])
	}
	return b.String() + "aZ9_"
}

// engineRunner is the Runner over a harness.
type engineRunner struct {
	// harness makes the harness of a test; it may skip the test.
	harness func(testing.TB) *containerapitest.Harness
}

// Docker returns the Runner on the user's Docker (as the Docker CLI would find it). It skips the test when no local engine answers.
func Docker() Runner { return engineRunner{harness: containerapitest.RealDocker} }

// Podman returns the Runner on the user's Podman. It skips the test when there is no Podman.
func Podman() Runner { return engineRunner{harness: containerapitest.RealPodman} }

// OnHarness returns the Runner on an existing harness, for example one on a fake engine.
func OnHarness(h *containerapitest.Harness) Runner {
	return engineRunner{harness: func(testing.TB) *containerapitest.Harness { return h }}
}

// ContainerSpec builds the container the harness makes for a spec. The harness adds the name, the label and the loopback address.
// It is separate so that the tests check what is asked without an engine: the environment in a stable order, the port as a
// container port only, no mount.
func ContainerSpec(spec Spec) containerapi.ContainerSpec {
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+spec.Env[k])
	}
	return containerapi.ContainerSpec{
		Image: spec.Image,
		Cmd:   slices.Clone(spec.Args),
		Env:   env,
		Ports: []containerapi.PortMapping{{ContainerPort: uint16(spec.Port)}},
	}
}

// Start implements Runner.
func (r engineRunner) Start(t testing.TB, spec Spec) Server {
	t.Helper()
	if !containerapitest.IsPinned(spec.Image) {
		t.Fatalf("%s is not one of the pinned images of containerapitest/images.go; the tests pull only those", spec.Image)
	}
	h := r.harness(t)
	ctx := h.Context(15 * time.Minute)
	h.EnsureImage(ctx, spec.Image)
	id := h.StartedContainer(ctx, ContainerSpec(spec))
	// registered after the container's own cleanup, so that it runs before the removal
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the last lines of the log of the server of %s:\n%s", spec.Image, lastLines(h, id, 40))
		}
	})
	info, err := h.Client.InspectContainer(ctx, id)
	if err != nil {
		t.Fatalf("looking at the container %s: %v", id, err)
	}
	port, ok := publishedPort(info, spec.Port)
	if !ok {
		t.Fatalf("port %d of %s is not published on %s: %+v", spec.Port, info.Name, containerapitest.Loopback, info.Ports)
	}
	return Server{Name: info.Name, Host: containerapitest.Loopback, Port: port}
}

// publishedPort finds the host port that the container's port is published on, on the loopback address.
func publishedPort(info *containerapi.ContainerInspect, containerPort int) (int, bool) {
	key := strconv.Itoa(containerPort) + "/tcp"
	for _, b := range info.Ports[key] {
		if b.HostIP != containerapitest.Loopback {
			continue
		}
		if port, err := strconv.Atoi(b.HostPort); err == nil && port > 0 {
			return port, true
		}
	}
	return 0, false
}

// lastLines reads the last n lines of a container's log, for a failed test.
func lastLines(h *containerapitest.Harness, id string, n int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var lines []string
	err := h.Client.Logs(ctx, id, containerapi.LogsOptions{Tail: n}, func(l containerapi.LogLine) error {
		lines = append(lines, l.Text)
		return nil
	})
	if err != nil {
		lines = append(lines, "(no log: "+err.Error()+")")
	}
	return strings.Join(lines, "\n")
}
