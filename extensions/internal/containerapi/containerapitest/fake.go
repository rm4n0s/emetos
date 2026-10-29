// Package containerapitest is the test side of containerapi (SPEC §10.20, step 18.1): a fake engine that speaks both the
// Docker Engine API and Podman's libpod API, for the default tests of the container extensions, and a harness for the
// tests against the user's real Docker and Podman (build tags emetos_docker and emetos_podman), which touches only the
// things it made itself: containers, networks, volumes and pods named emetos-test-<random> that carry the label
// dev.emetos.test=1, with ports published on 127.0.0.1 only, removed when the test ends (also when it fails). The images
// they may pull are listed in one place, images.go. TestEngine is the one suite that runs against the fake and, tagged,
// against the real engines.
package containerapitest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/errors"
)

// FakeOptions tune a Fake.
type FakeOptions struct {
	// APIVersion is the newest API version a Docker fake reports; "" is "1.52" (newer than the Client's 1.47). MinAPIVersion
	// is the oldest; "" is "1.24".
	APIVersion, MinAPIVersion string
	// EngineVersion is the engine's own version; "" is "29.0.0" for Docker and "5.8.0" for Podman.
	EngineVersion string
}

// Request is a request the Fake received.
type Request struct {
	// Method and Path are the request line with the version prefix cut off ("/containers/json", "/libpod/pods/json").
	Method, Path string
	// Prefix is the version part that was cut off ("/v1.47", "/v4.0.0"); empty when the path had none.
	Prefix string
	// Query is the raw query.
	Query string
	// Body is the request body (JSON of a create call).
	Body []byte
}

// override replaces the answer to the requests whose method and path match.
type override struct {
	method, contains string
	once             bool
	h                http.HandlerFunc
}

// Fake is an in-memory engine. It speaks the Docker Engine API (Kind Docker) or the libpod API (Kind Podman) over a Unix
// socket, with state: containers, images, networks, volumes, pods (Podman) and the event stream that follows their changes.
type Fake struct {
	kind containerapi.Kind
	opts FakeOptions
	sock string
	srv  *http.Server
	ln   net.Listener
	wg   sync.WaitGroup
	// stop ends the streams and the tickers when the Fake closes.
	stop     chan struct{}
	stopOnce sync.Once

	mu         sync.Mutex
	counter    int
	nextPort   int
	containers map[string]*fakeContainer
	images     []*fakeImage
	networks   map[string]*fakeNetwork
	volumes    map[string]*fakeVolume
	pods       map[string]*fakePod
	execs      map[string]*fakeExec
	history    []containerapi.Event
	subs       map[int]chan containerapi.Event
	subCount   int
	requests   []Request
	overrides  []override
	pullFails  map[string]string
	// ExecFunc runs a command for exec; nil uses the little shell of script.go.
	execFunc func(cmd []string) (stdout, stderr string, exit int)
}

// shortDir makes a folder with a short path for a socket (a Unix socket path is limited to about 100 bytes, and the
// temporary folder of a test can be longer).
func shortDir(t testing.TB) string {
	t.Helper()
	base := os.TempDir()
	if len(base) > 40 {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "ce")
	if err != nil {
		t.Fatalf("making a folder for the fake engine's socket: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// NewFake starts a fake engine of the given kind on a Unix socket and stops it when the test ends.
func NewFake(t testing.TB, kind containerapi.Kind, opts FakeOptions) *Fake {
	t.Helper()
	f, err := ServeUnix(kind, opts, filepath.Join(shortDir(t), "engine.sock"))
	if err != nil {
		t.Fatalf("starting the fake engine: %v", err)
	}
	t.Cleanup(f.Close)
	return f
}

// ServeUnix starts a fake engine of the given kind on the Unix socket at sock, for a program that is not a test (the fake
// podman of the tests of containerapi.StartService). Close stops it.
func ServeUnix(kind containerapi.Kind, opts FakeOptions, sock string) (*Fake, error) {
	f := &Fake{
		kind: kind, opts: opts, stop: make(chan struct{}), nextPort: 40000, sock: sock,
		containers: map[string]*fakeContainer{}, networks: map[string]*fakeNetwork{}, volumes: map[string]*fakeVolume{},
		pods: map[string]*fakePod{}, execs: map[string]*fakeExec{}, subs: map[int]chan containerapi.Event{},
		pullFails: map[string]string{},
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, errors.NewErr("FakeEngineListenFailed", err).SetMetadata("socket", sock)
	}
	f.ln = ln
	f.networks["bridge"] = &fakeNetwork{id: f.newID("net-bridge"), name: "bridge", driver: "bridge", created: time.Now()}
	f.srv = &http.Server{Handler: f.Handler(), ReadHeaderTimeout: 10 * time.Second}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		_ = f.srv.Serve(ln) // ends with ErrServerClosed when the engine closes
	}()
	return f, nil
}

// Close stops the engine and its streams. The test cleanup calls it; calling it again is harmless.
func (f *Fake) Close() {
	f.stopOnce.Do(func() {
		close(f.stop)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.srv.Shutdown(ctx); err != nil {
			_ = f.srv.Close() // streams that do not end in time are cut
		}
		f.wg.Wait()
	})
}

// Kind returns the engine the Fake speaks.
func (f *Fake) Kind() containerapi.Kind { return f.kind }

// Socket returns the path of the Unix socket.
func (f *Fake) Socket() string { return f.sock }

// Endpoint returns the endpoint of the socket.
func (f *Fake) Endpoint() containerapi.Endpoint { return containerapi.UnixEndpoint(f.sock) }

// Client returns a Client of the Fake's kind connected to it.
func (f *Fake) Client() *containerapi.Client {
	return containerapi.New(f.kind, f.Endpoint(), containerapi.Options{})
}

// Requests returns the requests received so far, oldest first.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// ResetRequests forgets the requests received so far.
func (f *Fake) ResetRequests() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
}

// Override answers the requests whose method is method and whose path (without the version prefix) contains pathContains
// with h, from now on; the engine's own handling is skipped for them.
func (f *Fake) Override(method, pathContains string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides = append(f.overrides, override{method: method, contains: pathContains, h: h})
}

// FailNext makes the next request with that method and a path that contains pathContains fail once with the given status
// and message, in the engine's error format.
func (f *Fake) FailNext(method, pathContains string, status int, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides = append(f.overrides, override{method: method, contains: pathContains, once: true, h: func(w http.ResponseWriter, _ *http.Request) {
		f.writeError(w, status, message)
	}})
}

// FailPull makes pulling ref fail with message in the progress stream, as a registry error does.
func (f *Fake) FailPull(ref, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullFails[ref] = message
}

// SetExec sets the function that runs a command for exec; nil goes back to the little shell.
func (f *Fake) SetExec(fn func(cmd []string) (stdout, stderr string, exit int)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execFunc = fn
}

// newID returns a 64-character hex id that is the same on every run for the same sequence of calls.
func (f *Fake) newID(seed string) string {
	f.counter++
	sum := sha256.Sum256([]byte("fake-engine/" + seed + "/" + strconv.Itoa(f.counter)))
	return hex.EncodeToString(sum[:])
}

// versionPrefix is the version part of a path: /v1.47 for Docker, /v4.0.0 for libpod.
var versionPrefix = regexp.MustCompile(`^/v[0-9]+(\.[0-9]+)*`)

// Handler returns the http.Handler of the engine, for a test that serves it on a listener of its own (TLS).
func (f *Fake) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := versionPrefix.FindString(r.URL.Path)
		path := strings.TrimPrefix(r.URL.Path, prefix)
		var body []byte
		if r.Body != nil && r.Method != http.MethodGet {
			buf := make([]byte, 0, 512)
			tmp := make([]byte, 4096)
			for {
				n, err := r.Body.Read(tmp)
				buf = append(buf, tmp[:n]...)
				if err != nil || len(buf) > 1<<20 {
					break
				}
			}
			body = buf
		}
		f.mu.Lock()
		f.requests = append(f.requests, Request{Method: r.Method, Path: path, Prefix: prefix, Query: r.URL.RawQuery, Body: body})
		var hit *override
		for i := range f.overrides {
			o := f.overrides[i]
			if o.method == r.Method && strings.Contains(path, o.contains) {
				hit = &o
				if o.once {
					f.overrides = append(f.overrides[:i], f.overrides[i+1:]...)
				}
				break
			}
		}
		f.mu.Unlock()
		if hit != nil {
			hit.h(w, r)
			return
		}
		f.route(w, r, path, body)
	})
}

// writeJSON writes v as the answer.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // a failed write is the client leaving
}

// writeError writes an error answer in the engine's format: {"message": ...} for Docker, libpod's {"cause", "message",
// "response"} for Podman.
func (f *Fake) writeError(w http.ResponseWriter, status int, message string) {
	if f.kind == containerapi.KindPodman {
		writeJSON(w, status, map[string]any{"cause": message, "message": message, "response": status})
		return
	}
	writeJSON(w, status, map[string]string{"message": message})
}

// noContent answers 204.
func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// engineVersion returns the engine's own version.
func (f *Fake) engineVersion() string {
	if f.opts.EngineVersion != "" {
		return f.opts.EngineVersion
	}
	if f.kind == containerapi.KindPodman {
		return "5.8.0"
	}
	return "29.0.0"
}

// apiVersion returns the newest compat API version.
func (f *Fake) apiVersion() string {
	if f.opts.APIVersion != "" {
		return f.opts.APIVersion
	}
	if f.kind == containerapi.KindPodman {
		return "1.41"
	}
	return "1.52"
}

// minAPIVersion returns the oldest compat API version.
func (f *Fake) minAPIVersion() string {
	if f.opts.MinAPIVersion != "" {
		return f.opts.MinAPIVersion
	}
	return "1.24"
}

// route dispatches a request by its path.
func (f *Fake) route(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	switch path {
	case "/_ping", "/libpod/_ping":
		w.Header().Set("Api-Version", f.apiVersion())
		if f.kind == containerapi.KindPodman {
			w.Header().Set("Libpod-Api-Version", f.engineVersion())
			w.Header().Set("Server", "Libpod/"+f.engineVersion()+" (linux)")
		} else {
			w.Header().Set("Server", "Docker/"+f.engineVersion()+" (linux)")
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("OK"))
		return
	case "/version", "/libpod/version":
		f.version(w)
		return
	}
	if f.kind == containerapi.KindPodman {
		if !strings.HasPrefix(path, "/libpod/") {
			f.writeError(w, http.StatusNotFound, "page not found")
			return
		}
		path = strings.TrimPrefix(path, "/libpod")
	} else if strings.HasPrefix(path, "/libpod/") {
		f.writeError(w, http.StatusNotFound, "page not found")
		return
	}
	switch {
	case strings.HasPrefix(path, "/containers/"):
		f.containersRoute(w, r, strings.TrimPrefix(path, "/containers/"), body)
	case strings.HasPrefix(path, "/exec/"):
		f.execRoute(w, r, strings.TrimPrefix(path, "/exec/"), body)
	case strings.HasPrefix(path, "/images/"):
		f.imagesRoute(w, r, strings.TrimPrefix(path, "/images/"))
	case strings.HasPrefix(path, "/networks"):
		f.networksRoute(w, r, strings.Trim(strings.TrimPrefix(path, "/networks"), "/"), body)
	case strings.HasPrefix(path, "/volumes"):
		f.volumesRoute(w, r, strings.Trim(strings.TrimPrefix(path, "/volumes"), "/"), body)
	case strings.HasPrefix(path, "/pods/"):
		f.podsRoute(w, r, strings.TrimPrefix(path, "/pods/"), body)
	case path == "/events":
		f.eventsStream(w, r)
	case path == "/system/df":
		f.systemDF(w)
	default:
		f.writeError(w, http.StatusNotFound, "page not found")
	}
}

// version answers /version.
func (f *Fake) version(w http.ResponseWriter) {
	body := map[string]any{
		"Version": f.engineVersion(), "ApiVersion": f.apiVersion(), "MinAPIVersion": f.minAPIVersion(),
		"Os": "linux", "Arch": "amd64", "GitCommit": "fake", "GoVersion": "go1.27",
	}
	if f.kind == containerapi.KindPodman {
		body["Components"] = []map[string]any{{"Name": "Podman Engine", "Version": f.engineVersion()}}
		body["Platform"] = map[string]string{"Name": "linux/amd64/fake"}
	} else {
		body["Platform"] = map[string]string{"Name": "Docker Engine - Community"}
		body["Components"] = []map[string]any{{"Name": "Engine", "Version": f.engineVersion()}}
	}
	writeJSON(w, http.StatusOK, body)
}

// parseFilters reads the "filters" parameter.
func parseFilters(r *http.Request) map[string][]string {
	out := map[string][]string{}
	raw := r.URL.Query().Get("filters")
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		// Docker's older form {"label": {"a=b": true}}
		var old map[string]map[string]bool
		if json.Unmarshal([]byte(raw), &old) == nil {
			for k, vs := range old {
				for v, on := range vs {
					if on {
						out[k] = append(out[k], v)
					}
				}
			}
		}
	}
	return out
}

// matchLabels reports whether labels satisfy every "key" or "key=value" in want.
func matchLabels(labels map[string]string, want []string) bool {
	for _, w := range want {
		k, v, hasV := strings.Cut(w, "=")
		got, ok := labels[k]
		if !ok || (hasV && got != v) {
			return false
		}
	}
	return true
}

// boolParam reads a boolean query parameter ("1", "true").
func boolParam(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "1" || v == "true"
}
