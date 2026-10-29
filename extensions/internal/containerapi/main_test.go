package containerapi_test

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/errors"
)

// The tests of StartService run a fake podman: this very test binary, started with EMETOS_FAKE_PODMAN set to a mode. Its
// arguments are the ones of the real command ("system service --time=0 unix://<socket>"). It writes "pid pgid ppid" to the
// file named by EMETOS_FAKE_PODMAN_INFO, and then acts by its mode:
//
//	serve     listens on the socket with the fake engine and ends at SIGTERM
//	stubborn  the same, but ignores SIGTERM, so that only SIGKILL ends it
//	exit      prints an error and ends with status 3 at once
//	hang      never opens the socket
const (
	envFakePodman     = "EMETOS_FAKE_PODMAN"
	envFakePodmanInfo = "EMETOS_FAKE_PODMAN_INFO"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(envFakePodman); mode != "" {
		os.Exit(fakePodman(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakePodman is the fake podman program; it returns the exit status.
func fakePodman(mode string, args []string) int {
	if len(args) != 4 || args[0] != "system" || args[1] != "service" || args[2] != "--time=0" || !strings.HasPrefix(args[3], "unix://") {
		fmt.Fprintln(os.Stderr, "fake podman: unexpected arguments:", strings.Join(args, " "))
		return 2
	}
	sock := strings.TrimPrefix(args[3], "unix://")
	if info := os.Getenv(envFakePodmanInfo); info != "" {
		pgid, _ := syscall.Getpgid(0)
		line := strconv.Itoa(os.Getpid()) + " " + strconv.Itoa(pgid) + " " + strconv.Itoa(os.Getppid())
		if err := os.WriteFile(info, []byte(line), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "fake podman:", err)
			return 2
		}
	}
	switch mode {
	case "exit":
		fmt.Fprintln(os.Stderr, "Error: cannot start the service: boom")
		return 3
	case "hang":
		time.Sleep(time.Hour)
		return 0
	case "serve", "stubborn":
		fake, err := containerapitest.ServeUnix(containerapi.KindPodman, containerapitest.FakeOptions{}, sock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fake podman:", err)
			return 2
		}
		term := make(chan os.Signal, 1)
		if mode == "stubborn" {
			signal.Ignore(syscall.SIGTERM)
			select {} // only SIGKILL ends it
		}
		signal.Notify(term, syscall.SIGTERM)
		<-term
		fake.Close()
		return 0
	}
	return 2
}

// routeOf fails the test unless err is an *errors.Error whose route contains route, and returns it for a look at its message
// and metadata. wantRoute is the same when only the route matters.
func routeOf(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want a failure at %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("error %T (%v) is not an *errors.Error", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Fatalf("route = %s, want %s", appErr.Route(), route)
	}
	return appErr
}

// x509Pool makes a certificate pool from PEM.
func x509Pool(t *testing.T, pemData []byte) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemData) {
		t.Fatal("no certificate in the PEM")
	}
	return pool
}

// jsonUnmarshal is json.Unmarshal; the tests use it for request bodies.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// wantRoute is routeOf without the result.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = routeOf(t, err, route)
}
