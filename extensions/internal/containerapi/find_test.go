package containerapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// env is an isolated environment: the tests of Find pass it as Getenv, so they never read the user's variables, home or
// sockets.
type env map[string]string

func (e env) get(k string) string { return e[k] }

// writeContext writes a Docker context into the store of a Docker configuration folder, the way the Docker CLI does:
// contexts/meta/<sha256 of the name>/meta.json, and the TLS files under contexts/tls/<sha256>/docker/.
func writeContext(t *testing.T, configDir, name, host string, tlsFiles map[string][]byte) {
	t.Helper()
	sum := sha256.Sum256([]byte(name))
	sub := hex.EncodeToString(sum[:])
	dir := filepath.Join(configDir, "contexts", "meta", sub)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	meta := `{"Name":"` + name + `","Metadata":{"Description":"the ` + name + ` context"},"Endpoints":{"docker":{"Host":"` + host + `","SkipTLSVerify":false}}}`
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(tlsFiles) > 0 {
		tdir := filepath.Join(configDir, "contexts", "tls", sub, "docker")
		if err := os.MkdirAll(tdir, 0o700); err != nil {
			t.Fatal(err)
		}
		for n, d := range tlsFiles {
			if err := os.WriteFile(filepath.Join(tdir, n), d, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// missingSocket returns the path of a socket that does not exist, in a folder of the test's own: the default sockets of the
// computer are never asked.
func missingSocket(t *testing.T) string {
	t.Helper()
	return filepath.Join(shortSocketDir(t), "none.sock")
}

func writeConfig(t *testing.T, configDir, content string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindDockerSearchOrder(t *testing.T) {
	// three engines that answer, each on its own socket, told apart by their version
	mk := func(version string) *containerapitest.Fake {
		return containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{EngineVersion: version})
	}
	setting, fromEnv, fromContext, fromPicker := mk("1.0.0-setting"), mk("2.0.0-env"), mk("3.0.0-context"), mk("4.0.0-picker")
	home := t.TempDir()
	cfg := filepath.Join(home, ".docker")
	writeContext(t, cfg, "work", "unix://"+fromContext.Socket(), nil)
	writeContext(t, cfg, "other", "unix://"+fromPicker.Socket(), nil)
	writeConfig(t, cfg, `{"currentContext":"work"}`)

	for _, tc := range []struct {
		name    string
		opts    containerapi.FindOptions
		env     env
		version string
		source  string
	}{
		{name: "the picked context wins over everything", opts: containerapi.FindOptions{Host: "unix://" + setting.Socket(), Context: "other"}, env: env{"HOME": home, "DOCKER_HOST": "unix://" + fromEnv.Socket()}, version: "4.0.0-picker", source: "the Docker context other"},
		{name: "the setting wins over DOCKER_HOST and the context", opts: containerapi.FindOptions{Host: "unix://" + setting.Socket()}, env: env{"HOME": home, "DOCKER_HOST": "unix://" + fromEnv.Socket()}, version: "1.0.0-setting", source: "the setting containers.host"},
		{name: "DOCKER_HOST wins over the current context", env: env{"HOME": home, "DOCKER_HOST": "unix://" + fromEnv.Socket()}, version: "2.0.0-env", source: "DOCKER_HOST"},
		{name: "the current context of config.json", env: env{"HOME": home}, version: "3.0.0-context", source: "the current Docker context work"},
		{name: "DOCKER_CONTEXT wins over config.json", env: env{"HOME": home, "DOCKER_CONTEXT": "other"}, version: "4.0.0-picker", source: "the current Docker context other"},
		{name: "DOCKER_CONFIG names the folder", env: env{"DOCKER_CONFIG": cfg}, version: "3.0.0-context", source: "the current Docker context work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Kind = containerapi.KindDocker
			tc.opts.Getenv = tc.env.get
			found, err := containerapi.Find(context.Background(), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer found.Close()
			if found.Info.Version != tc.version || found.Source != tc.source || found.Service != nil {
				t.Errorf("found version %q from %q (service %v), want %q from %q", found.Info.Version, found.Source, found.Service, tc.version, tc.source)
			}
			if len(found.Attempts) != 1 || found.Attempts[0].Err != "" {
				t.Errorf("attempts = %+v", found.Attempts)
			}
		})
	}
}

func TestFindDockerNeverFallsThroughFromAnEngineTheUserNamed(t *testing.T) {
	// the default socket is not reachable in a test (and is never looked at when something is named), so an unreachable named
	// engine must be reported, not replaced
	alive := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	dead := filepath.Join(shortSocketDir(t), "dead.sock")
	for _, tc := range []struct {
		name string
		opts containerapi.FindOptions
		env  env
		want string
	}{
		{"the setting", containerapi.FindOptions{Host: "unix://" + dead}, env{"DOCKER_HOST": "unix://" + alive.Socket()}, "the setting containers.host"},
		{"DOCKER_HOST", containerapi.FindOptions{}, env{"DOCKER_HOST": "unix://" + dead}, "DOCKER_HOST"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Kind = containerapi.KindDocker
			tc.opts.Getenv = tc.env.get
			_, err := containerapi.Find(context.Background(), tc.opts)
			appErr := routeOf(t, err, "Find.ContainerAPIEngineNotFound")
			if !strings.Contains(appErr.Message, tc.want) || !strings.Contains(appErr.Message, dead) {
				t.Errorf("message = %q, want it to name %q and %s", appErr.Message, tc.want, dead)
			}
			attempts, _ := appErr.Metadata["attempts"].([]string)
			if len(attempts) != 1 {
				t.Errorf("attempts = %v, want exactly the one the user named", attempts)
			}
		})
	}
}

func TestFindDockerDefaultSocketIsOnlyUsedWhenNothingIsNamed(t *testing.T) {
	// nothing named: the default socket is asked; in the sandbox of a test it may or may not exist, so the test only checks what
	// was asked, through the error when it does not answer
	// the default socket is a folder of the test's own: the computer's Docker is never asked
	missing := filepath.Join(shortSocketDir(t), "docker.sock")
	_, err := containerapi.Find(context.Background(), containerapi.FindOptions{Kind: containerapi.KindDocker, Getenv: env{"HOME": t.TempDir()}.get, ProbeTimeout: 2 * time.Second, DockerSocket: missing})
	appErr := routeOf(t, err, "Find.ContainerAPIEngineNotFound")
	if !strings.Contains(appErr.Message, "the default socket") || !strings.Contains(appErr.Message, missing) {
		t.Errorf("message = %q", appErr.Message)
	}
	alive := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{EngineVersion: "8.0.0-default"})
	found, err := containerapi.Find(context.Background(), containerapi.FindOptions{Kind: containerapi.KindDocker, Getenv: env{"HOME": t.TempDir()}.get, DockerSocket: alive.Socket()})
	if err != nil {
		t.Fatal(err)
	}
	defer found.Close()
	if found.Info.Version != "8.0.0-default" || found.Source != "the default socket" {
		t.Errorf("found %q from %q", found.Info.Version, found.Source)
	}
}

func TestFindDockerContextProblems(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".docker")
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T)
		env   env
		opts  containerapi.FindOptions
		route string
	}{
		{name: "a context that does not exist", env: env{"HOME": home, "DOCKER_CONTEXT": "ghost"}, route: "Find->dockerCandidates->contextCandidate.ContainerAPIContextNotFound"},
		{name: "a context without a home to look in", env: env{"DOCKER_CONTEXT": "ghost"}, route: "Find->dockerCandidates->contextCandidate.ContainerAPIContextNotFound"},
		{name: "a meta.json that is not JSON", setup: func(t *testing.T) {
			sum := sha256.Sum256([]byte("broken"))
			dir := filepath.Join(cfg, "contexts", "meta", hex.EncodeToString(sum[:]))
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, env: env{"HOME": home, "DOCKER_CONTEXT": "broken"}, route: "contextCandidate->readContext.ContainerAPIContextInvalid"},
		{name: "a context with no docker endpoint", setup: func(t *testing.T) {
			sum := sha256.Sum256([]byte("noendpoint"))
			dir := filepath.Join(cfg, "contexts", "meta", hex.EncodeToString(sum[:]))
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(`{"Name":"noendpoint","Endpoints":{"kubernetes":{"Host":"x"}}}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}, env: env{"HOME": home, "DOCKER_CONTEXT": "noendpoint"}, route: "contextCandidate->readContext.ContainerAPIContextInvalid"},
		{name: "a config.json that is not JSON", setup: func(t *testing.T) { writeConfig(t, cfg, "{") }, env: env{"HOME": home}, route: "dockerCandidates->readDockerConfig.ContainerAPIContextInvalid"},
		{name: "a DOCKER_HOST that is not a host", env: env{"DOCKER_HOST": "nonsense"}, route: "dockerCandidates.func1->ParseHost.ContainerAPIHostInvalid"},
		{name: "a setting that is not a host", opts: containerapi.FindOptions{Host: "ssh://me@box"}, env: env{}, route: "dockerCandidates.func1->ParseHost.ContainerAPIHostUnsupported"},
		{name: "a context whose host is not a host", setup: func(t *testing.T) { writeContext(t, cfg, "badhost", "carrier-pigeon://x", nil) }, env: env{"HOME": home, "DOCKER_CONTEXT": "badhost"}, route: "contextCandidate->ParseHost.ContainerAPIHostInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			tc.opts.Kind = containerapi.KindDocker
			tc.opts.Getenv = tc.env.get
			_, err := containerapi.Find(context.Background(), tc.opts)
			wantRoute(t, err, tc.route)
		})
	}
}

func TestFindDockerContextWithTLSFiles(t *testing.T) {
	// a context whose host is tcp:// with its certificates in the store gets TLS (and the right server name)
	home := t.TempDir()
	cfg := filepath.Join(home, ".docker")
	ca, cert, key := containerapitest.Certificates(t)
	writeContext(t, cfg, "remote", "tcp://127.0.0.1:1", map[string][]byte{"ca.pem": ca, "cert.pem": cert, "key.pem": key})
	_, err := containerapi.Find(context.Background(), containerapi.FindOptions{Kind: containerapi.KindDocker, Getenv: env{"HOME": home, "DOCKER_CONTEXT": "remote"}.get, ProbeTimeout: time.Second})
	appErr := routeOf(t, err, "Find.ContainerAPIEngineNotFound")
	if !strings.Contains(appErr.Message, "tcp://127.0.0.1:1") {
		t.Errorf("message = %q", appErr.Message)
	}
	ctxs, err := containerapi.DockerContexts(env{"HOME": home}.get)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxs) != 2 || ctxs[1].Name != "remote" || ctxs[1].TLSDir == "" {
		t.Errorf("contexts = %+v", ctxs)
	}
}

func TestDockerContexts(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".docker")
	writeContext(t, cfg, "zeta", "unix:///run/user/1000/docker.sock", nil)
	writeContext(t, cfg, "alpha", "tcp://10.0.0.1:2376", nil)
	// a context that cannot be read is left out of the list
	sum := sha256.Sum256([]byte("broken"))
	if err := os.MkdirAll(filepath.Join(cfg, "contexts", "meta", hex.EncodeToString(sum[:])), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		env     env
		config  string
		names   []string
		current string
		host    string
	}{
		{name: "no home", env: env{}, names: []string{"default"}, current: "default", host: "unix:///var/run/docker.sock"},
		{name: "DOCKER_HOST is the default context", env: env{"DOCKER_HOST": "tcp://h:1"}, names: []string{"default"}, current: "default", host: "tcp://h:1"},
		{name: "sorted, the default first", env: env{"HOME": home}, config: `{}`, names: []string{"default", "alpha", "zeta"}, current: "default", host: "unix:///var/run/docker.sock"},
		{name: "the current one from config.json", env: env{"HOME": home}, config: `{"currentContext":"zeta"}`, names: []string{"default", "alpha", "zeta"}, current: "zeta", host: "unix:///var/run/docker.sock"},
		{name: "DOCKER_CONTEXT", env: env{"HOME": home, "DOCKER_CONTEXT": "alpha"}, config: `{"currentContext":"zeta"}`, names: []string{"default", "alpha", "zeta"}, current: "alpha", host: "unix:///var/run/docker.sock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.config != "" {
				writeConfig(t, cfg, tc.config)
			}
			ctxs, err := containerapi.DockerContexts(tc.env.get)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			current := ""
			for _, c := range ctxs {
				names = append(names, c.Name)
				if c.Current {
					current = c.Name
				}
			}
			if strings.Join(names, ",") != strings.Join(tc.names, ",") || current != tc.current || ctxs[0].Host != tc.host {
				t.Errorf("contexts = %v (current %q, default host %q), want %v (current %q, host %q)", names, current, ctxs[0].Host, tc.names, tc.current, tc.host)
			}
		})
	}
	writeConfig(t, cfg, "{")
	_, err := containerapi.DockerContexts(env{"HOME": home}.get)
	wantRoute(t, err, "DockerContexts->readDockerConfig.ContainerAPIContextInvalid")
}

func TestFindPodmanSearchOrder(t *testing.T) {
	mk := func(version string) *containerapitest.Fake {
		return containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{EngineVersion: version})
	}
	setting, fromEnv := mk("5.1.0-setting"), mk("5.2.0-env")
	// the user's socket: $XDG_RUNTIME_DIR/podman/podman.sock, made by a fake served there
	rt := shortSocketDir(t)
	if err := os.MkdirAll(filepath.Join(rt, "podman"), 0o700); err != nil {
		t.Fatal(err)
	}
	user, err := containerapitest.ServeUnix(containerapi.KindPodman, containerapitest.FakeOptions{EngineVersion: "5.3.0-user"}, filepath.Join(rt, "podman", "podman.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer user.Close()

	for _, tc := range []struct {
		name    string
		opts    containerapi.FindOptions
		env     env
		version string
		source  string
	}{
		{name: "the setting wins", opts: containerapi.FindOptions{Host: "unix://" + setting.Socket()}, env: env{"CONTAINER_HOST": "unix://" + fromEnv.Socket(), "XDG_RUNTIME_DIR": rt}, version: "5.1.0-setting", source: "the setting podman.host"},
		{name: "CONTAINER_HOST wins over the user's socket", env: env{"CONTAINER_HOST": "unix://" + fromEnv.Socket(), "XDG_RUNTIME_DIR": rt}, version: "5.2.0-env", source: "CONTAINER_HOST"},
		{name: "the user's socket", env: env{"XDG_RUNTIME_DIR": rt}, version: "5.3.0-user", source: "the user's socket"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Kind = containerapi.KindPodman
			tc.opts.RootfulPodmanSocket = missingSocket(t)
			tc.opts.Getenv = tc.env.get
			tc.opts.NoService = true
			found, err := containerapi.Find(context.Background(), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer found.Close()
			if found.Info.Version != tc.version || found.Source != tc.source || found.Service != nil {
				t.Errorf("found %q from %q (service %v), want %q from %q", found.Info.Version, found.Source, found.Service, tc.version, tc.source)
			}
		})
	}
}

func TestFindPodmanStartsItsOwnServiceWhenNoSocketAnswers(t *testing.T) {
	dir, info, penv := fakePodmanDir(t, "serve")
	rt := shortSocketDir(t) // an XDG_RUNTIME_DIR with no podman socket in it
	e := env{"XDG_RUNTIME_DIR": rt, "PATH": dir}
	found, err := containerapi.Find(context.Background(), containerapi.FindOptions{
		Kind: containerapi.KindPodman, Getenv: e.get, RootfulPodmanSocket: missingSocket(t),
		Service: containerapi.ServiceOptions{Env: penv},
	})
	if err != nil {
		t.Fatal(err)
	}
	if found.Service == nil || found.Source != "a podman system service of Emetos's own" {
		t.Fatalf("found from %q with service %v", found.Source, found.Service)
	}
	if !strings.HasPrefix(found.Service.Socket, filepath.Join(rt, "emetos")+string(filepath.Separator)) {
		t.Errorf("the service's socket %s is not under %s/emetos", found.Service.Socket, rt)
	}
	if len(found.Attempts) < 3 {
		t.Errorf("attempts = %+v, want the user's socket, the rootful one and the service", found.Attempts)
	}
	pid, _, _ := readInfo(t, info)
	if _, err := found.Client.Negotiate(context.Background()); err != nil {
		t.Fatal(err)
	}
	found.Close() // stops the service
	select {
	case <-found.Service.Done():
	case <-time.After(5 * time.Second):
		t.Error("the service did not end with Close")
	}
	time.Sleep(50 * time.Millisecond)
	if alive(pid) {
		t.Errorf("process %d still runs after Close", pid)
	}
}

func TestFindPodmanDoesNotStartAServiceWhenTheUserNamedASocket(t *testing.T) {
	dir, info, penv := fakePodmanDir(t, "serve")
	dead := filepath.Join(shortSocketDir(t), "dead.sock")
	for _, tc := range []struct {
		name string
		opts containerapi.FindOptions
		env  env
	}{
		{"the setting", containerapi.FindOptions{Host: "unix://" + dead}, env{"PATH": dir}},
		{"CONTAINER_HOST", containerapi.FindOptions{}, env{"PATH": dir, "CONTAINER_HOST": "unix://" + dead}},
		{"NoService", containerapi.FindOptions{NoService: true}, env{"PATH": dir, "XDG_RUNTIME_DIR": shortSocketDir(t)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Kind = containerapi.KindPodman
			tc.opts.RootfulPodmanSocket = missingSocket(t)
			tc.opts.Getenv = tc.env.get
			tc.opts.Service = containerapi.ServiceOptions{Env: penv}
			_, err := containerapi.Find(context.Background(), tc.opts)
			wantRoute(t, err, "Find.ContainerAPIEngineNotFound")
			if _, statErr := os.Stat(info); statErr == nil {
				t.Error("a podman service was started")
			}
		})
	}
}

func TestFindPodmanReportsAServiceThatFailsToStart(t *testing.T) {
	dir, _, penv := fakePodmanDir(t, "exit")
	_, err := containerapi.Find(context.Background(), containerapi.FindOptions{
		Kind: containerapi.KindPodman, Getenv: env{"XDG_RUNTIME_DIR": shortSocketDir(t), "PATH": dir}.get, RootfulPodmanSocket: missingSocket(t),
		Service: containerapi.ServiceOptions{Env: penv},
	})
	appErr := routeOf(t, err, "Find.ContainerAPIEngineNotFound")
	if !strings.Contains(appErr.Message, "boom") || !strings.Contains(appErr.Message, "podman system service of Emetos's own") {
		t.Errorf("message = %q, want the service's own error", appErr.Message)
	}
}

func TestFindPodmanNeedsPodmanOnPATHToStartAService(t *testing.T) {
	_, err := containerapi.Find(context.Background(), containerapi.FindOptions{
		Kind: containerapi.KindPodman, Getenv: env{"XDG_RUNTIME_DIR": shortSocketDir(t), "PATH": t.TempDir()}.get, RootfulPodmanSocket: missingSocket(t),
	})
	appErr := routeOf(t, err, "Find.ContainerAPIEngineNotFound")
	if !strings.Contains(appErr.Message, "not on PATH") {
		t.Errorf("message = %q", appErr.Message)
	}
}

func TestFindStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := containerapi.Find(ctx, containerapi.FindOptions{Kind: containerapi.KindPodman, Getenv: env{"XDG_RUNTIME_DIR": shortSocketDir(t), "PATH": t.TempDir()}.get, RootfulPodmanSocket: missingSocket(t)})
	wantRoute(t, err, "Find.ContainerAPIEngineNotFound")
}

func TestFindDockerDefaultContext(t *testing.T) {
	// the context named "default" is DOCKER_HOST, or the default socket
	alive := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{EngineVersion: "7.0.0-default"})
	found, err := containerapi.Find(context.Background(), containerapi.FindOptions{
		Kind: containerapi.KindDocker, Context: "default", Getenv: env{"DOCKER_HOST": "unix://" + alive.Socket()}.get,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer found.Close()
	if found.Info.Version != "7.0.0-default" || found.Source != "the Docker context default" {
		t.Errorf("found %q from %q", found.Info.Version, found.Source)
	}
	_, err = containerapi.Find(context.Background(), containerapi.FindOptions{
		Kind: containerapi.KindDocker, Context: "default", ProbeTimeout: time.Second, Getenv: env{"DOCKER_HOST": "carrier-pigeon://x"}.get,
	})
	wantRoute(t, err, "contextCandidate->ParseHost.ContainerAPIHostInvalid")
	missing := filepath.Join(shortSocketDir(t), "docker.sock")
	_, err = containerapi.Find(context.Background(), containerapi.FindOptions{Kind: containerapi.KindDocker, Context: "default", ProbeTimeout: time.Second, Getenv: env{}.get, DockerSocket: missing})
	appErr := routeOf(t, err, "Find.ContainerAPIEngineNotFound")
	if !strings.Contains(appErr.Message, missing) {
		t.Errorf("message = %q", appErr.Message)
	}
}
