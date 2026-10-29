package containerapi_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// kinds are the two engines every table below runs against.
var kinds = []containerapi.Kind{containerapi.KindDocker, containerapi.KindPodman}

// engine starts a fake engine of the kind and returns it with a Client on it.
func engine(t *testing.T, kind containerapi.Kind) (*containerapitest.Fake, *containerapi.Client) {
	t.Helper()
	f := containerapitest.NewFake(t, kind, containerapitest.FakeOptions{})
	c := f.Client()
	t.Cleanup(c.Close)
	f.AddImage(4<<20, containerapitest.ImageBusyBox)
	return f, c
}

func TestNegotiateChoosesTheVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind containerapi.Kind
		opts containerapitest.FakeOptions
		want containerapi.ServerInfo
	}{
		{"Docker newer than the client", containerapi.KindDocker, containerapitest.FakeOptions{APIVersion: "1.52", EngineVersion: "29.0.0"},
			containerapi.ServerInfo{Kind: containerapi.KindDocker, Engine: "Docker Engine - Community", Version: "29.0.0", ServerAPIVersion: "1.52", MinAPIVersion: "1.24", APIVersion: containerapi.MaxDockerAPI, OS: "linux", Arch: "amd64"}},
		{"Docker at the client's version", containerapi.KindDocker, containerapitest.FakeOptions{APIVersion: "1.47"},
			containerapi.ServerInfo{Kind: containerapi.KindDocker, Engine: "Docker Engine - Community", Version: "29.0.0", ServerAPIVersion: "1.47", MinAPIVersion: "1.24", APIVersion: "1.47", OS: "linux", Arch: "amd64"}},
		{"Docker older than the client but new enough", containerapi.KindDocker, containerapitest.FakeOptions{APIVersion: "1.43"},
			containerapi.ServerInfo{Kind: containerapi.KindDocker, Engine: "Docker Engine - Community", Version: "29.0.0", ServerAPIVersion: "1.43", MinAPIVersion: "1.24", APIVersion: "1.43", OS: "linux", Arch: "amd64"}},
		{"Docker at the oldest version", containerapi.KindDocker, containerapitest.FakeOptions{APIVersion: containerapi.MinDockerAPI},
			containerapi.ServerInfo{Kind: containerapi.KindDocker, Engine: "Docker Engine - Community", Version: "29.0.0", ServerAPIVersion: "1.41", MinAPIVersion: "1.24", APIVersion: "1.41", OS: "linux", Arch: "amd64"}},
		{"Podman 5", containerapi.KindPodman, containerapitest.FakeOptions{EngineVersion: "5.8.9"},
			containerapi.ServerInfo{Kind: containerapi.KindPodman, Engine: "Podman Engine", Version: "5.8.9", ServerAPIVersion: "5.8.9", MinAPIVersion: "1.24", APIVersion: containerapi.LibpodAPI, OS: "linux", Arch: "amd64"}},
		{"Podman 4", containerapi.KindPodman, containerapitest.FakeOptions{EngineVersion: "4.0.0"},
			containerapi.ServerInfo{Kind: containerapi.KindPodman, Engine: "Podman Engine", Version: "4.0.0", ServerAPIVersion: "4.0.0", MinAPIVersion: "1.24", APIVersion: containerapi.LibpodAPI, OS: "linux", Arch: "amd64"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := containerapitest.NewFake(t, tc.kind, tc.opts)
			c := f.Client()
			defer c.Close()
			got, err := c.Negotiate(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("info = %+v\nwant   %+v", got, tc.want)
			}
			// the paths of later calls carry the negotiated version
			if _, err := c.ListContainers(context.Background(), containerapi.ListContainersOptions{}); err != nil {
				t.Fatal(err)
			}
			reqs := f.Requests()
			last := reqs[len(reqs)-1]
			wantPrefix := "/v" + tc.want.APIVersion
			if tc.kind == containerapi.KindPodman {
				wantPrefix = "/v4.0.0"
			}
			if last.Prefix != wantPrefix {
				t.Errorf("the call went to %q, want the prefix %q", last.Prefix, wantPrefix)
			}
		})
	}
}

func TestNegotiateFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  containerapi.Kind
		fake  containerapi.Kind
		opts  containerapitest.FakeOptions
		route string
	}{
		{"Docker 1.40 is too old", containerapi.KindDocker, containerapi.KindDocker, containerapitest.FakeOptions{APIVersion: "1.40"}, "Negotiate.ContainerAPIVersionTooOld"},
		{"Podman 3 is too old", containerapi.KindPodman, containerapi.KindPodman, containerapitest.FakeOptions{EngineVersion: "3.4.4"}, "Negotiate.ContainerAPIVersionTooOld"},
		{"an engine whose oldest version is newer than the client's", containerapi.KindDocker, containerapi.KindDocker, containerapitest.FakeOptions{APIVersion: "1.70", MinAPIVersion: "1.60"}, "Negotiate.ContainerAPIVersionTooNew"},
		{"a Podman client on Docker", containerapi.KindPodman, containerapi.KindDocker, containerapitest.FakeOptions{}, "Negotiate.ContainerAPIEngineMismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := containerapitest.NewFake(t, tc.fake, tc.opts)
			c := containerapi.New(tc.kind, f.Endpoint(), containerapi.Options{})
			defer c.Close()
			_, err := c.Negotiate(context.Background())
			wantRoute(t, err, tc.route)
			// the same error comes from the first call, and nothing is cached
			_, err = c.ListContainers(context.Background(), containerapi.ListContainersOptions{})
			wantRoute(t, err, "Negotiate.ContainerAPI")
		})
	}
}

func TestNegotiateWithAnEngineThatIsNotThere(t *testing.T) {
	c := containerapi.New(containerapi.KindDocker, containerapi.UnixEndpoint(filepath.Join(shortSocketDir(t), "none.sock")), containerapi.Options{})
	defer c.Close()
	_, err := c.Negotiate(context.Background())
	appErr := routeOf(t, err, "Negotiate->Client.do.ContainerAPIConnectFailed")
	if appErr.Metadata["endpoint"] == nil {
		t.Errorf("metadata = %v, want the endpoint", appErr.Metadata)
	}
}

func TestNegotiateIsDoneOnceAndRetriedAfterAFailure(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	f.FailNext(http.MethodGet, "/_ping", http.StatusInternalServerError, "daemon is starting")
	_, err := c.Negotiate(context.Background())
	appErr := routeOf(t, err, "Negotiate->Client.do.ContainerAPIEngineError")
	if appErr.Message != "daemon is starting" {
		t.Errorf("message = %q, want the engine's", appErr.Message)
	}
	for i := 0; i < 3; i++ {
		if _, err := c.ListContainers(context.Background(), containerapi.ListContainersOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	pings := 0
	for _, r := range f.Requests() {
		if r.Path == "/_ping" {
			pings++
		}
	}
	if pings != 2 {
		t.Errorf("%d pings, want 2 (the failed one and the one that worked)", pings)
	}
}

func TestAClientIsSafeForSeveralGoroutines(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			f.AddContainer("one", containerapitest.ImageBusyBox, nil, "running")
			var wg sync.WaitGroup
			errs := make(chan error, 16)
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					list, err := c.ListContainers(context.Background(), containerapi.ListContainersOptions{})
					if err == nil && len(list) != 1 {
						err = http.ErrAbortHandler
					}
					errs <- err
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Error(err)
				}
			}
			pings := 0
			for _, r := range f.Requests() {
				if r.Path == "/_ping" || r.Path == "/libpod/_ping" {
					pings++
				}
			}
			if pings != 1 {
				t.Errorf("%d pings from 16 concurrent first calls, want 1", pings)
			}
		})
	}
}

func TestTLSEndpointWithClientCertificates(t *testing.T) {
	ca, cert, key := containerapitest.Certificates(t)
	dir := t.TempDir()
	for n, d := range map[string][]byte{"ca.pem": ca, "cert.pem": cert, "key.pem": key} {
		if err := os.WriteFile(filepath.Join(dir, n), d, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509Pool(t, ca)
	srv := httptest.NewUnstartedServer(fake.Handler())
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	host := "tcp://" + srv.Listener.Addr().String()

	t.Run("with the certificates", func(t *testing.T) {
		ep, err := containerapi.ParseHost(host, containerapi.TLSOptions{Verify: true, CertDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		c := containerapi.New(containerapi.KindDocker, ep, containerapi.Options{})
		defer c.Close()
		info, err := c.Negotiate(context.Background())
		if err != nil || info.Version == "" {
			t.Fatalf("Negotiate over TLS = %+v, %v", info, err)
		}
	})
	t.Run("without the client certificate the server refuses", func(t *testing.T) {
		if err := os.Remove(filepath.Join(dir, "cert.pem")); err != nil {
			t.Fatal(err)
		}
		ep, err := containerapi.ParseHost(host, containerapi.TLSOptions{Verify: true, CertDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		c := containerapi.New(containerapi.KindDocker, ep, containerapi.Options{})
		defer c.Close()
		_, err = c.Negotiate(context.Background())
		wantRoute(t, err, "Negotiate->Client.do.ContainerAPIRequestFailed")
	})
	t.Run("plain HTTP to a TLS server is refused with the server's own words", func(t *testing.T) {
		ep, err := containerapi.ParseHost(host, containerapi.TLSOptions{})
		if err != nil {
			t.Fatal(err)
		}
		c := containerapi.New(containerapi.KindDocker, ep, containerapi.Options{})
		defer c.Close()
		_, err = c.Negotiate(context.Background())
		appErr := routeOf(t, err, "Client.do.ContainerAPIBadRequest")
		if !strings.Contains(appErr.Message, "HTTPS") {
			t.Errorf("message = %q", appErr.Message)
		}
	})
}

func TestPlainTCPEndpoint(t *testing.T) {
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	srv := httptest.NewServer(fake.Handler())
	defer srv.Close()
	ep, err := containerapi.ParseHost("tcp://"+srv.Listener.Addr().String(), containerapi.TLSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := containerapi.New(containerapi.KindDocker, ep, containerapi.Options{})
	defer c.Close()
	if _, err := c.Negotiate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Endpoint().Network != "tcp" || c.Kind() != containerapi.KindDocker || c.Endpoint().String() != "tcp://"+srv.Listener.Addr().String() {
		t.Errorf("Endpoint() = %v, Kind() = %v", c.Endpoint(), c.Kind())
	}
}
