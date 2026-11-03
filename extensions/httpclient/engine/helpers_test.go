package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want the route %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Fatalf("unexpected failure path %s, want it to contain %s", appErr.Route(), route)
	}
}

// fakeState is an extension.State in memory.
type fakeState struct {
	data map[string]json.RawMessage
	err  error
}

func newState() *fakeState { return &fakeState{data: map[string]json.RawMessage{}} }

func (s *fakeState) Get(key string) (json.RawMessage, bool) {
	v, ok := s.data[key]
	return v, ok
}

func (s *fakeState) Set(key string, value any) error {
	if s.err != nil {
		return s.err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.data[key] = raw
	return nil
}

func pair(name, value string) format.Pair {
	return format.Pair{Name: name, Value: value, Enabled: true}
}

func filePair(name, file string) format.Pair {
	return format.Pair{Name: name, File: file, Enabled: true}
}

// newEngine makes an engine with its cache folder in a temporary folder.
func newEngine(t *testing.T, cfg Config) *Engine {
	t.Helper()
	if cfg.CacheDir == "" {
		cfg.CacheDir = filepath.Join(t.TempDir(), "cache")
	}
	e := New(cfg)
	t.Cleanup(e.Close)
	return e
}

func newServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

// get makes the input of a GET request.
func get(url string) Input {
	return Input{Request: format.Request{Method: "GET", URL: url}}
}

// send sends and fails the test on an error.
func send(t *testing.T, e *Engine, in Input) *Response {
	t.Helper()
	r, err := e.Send(context.Background(), in)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	return r
}

func bodyString(t *testing.T, r *Response) string {
	t.Helper()
	b, err := r.Body.Bytes()
	if err != nil {
		t.Fatalf("Body.Bytes: %v", err)
	}
	return string(b)
}

// project makes a workspace folder with the files (name to content) and returns it.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func resolverOf(t *testing.T, locals map[string]string) *variables.Resolver {
	t.Helper()
	r, err := variables.Build(context.Background(), variables.Input{Locals: locals})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
