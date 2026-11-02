package devdebug

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// fakeSession is the part of a debug session the factory reads.
type fakeSession struct {
	cfg    extension.DebugConfiguration
	folder string
}

func (fakeSession) ID() string                                    { return "s1" }
func (fakeSession) Type() string                                  { return Type }
func (fakeSession) Name() string                                  { return "Fake" }
func (f fakeSession) Folder() string                              { return f.folder }
func (f fakeSession) Configuration() extension.DebugConfiguration { return f.cfg }
func (fakeSession) Parent() extension.DebugSession                { return nil }
func (fakeSession) CustomRequest(context.Context, string, any) (json.RawMessage, error) {
	return nil, nil
}

func TestTheManifestIsValid(t *testing.T) {
	if err := extension.CheckManifest(New().Manifest()); err != nil {
		t.Fatal(err)
	}
}

func TestTheAdapterRunsTheFakeProgramWithTheScriptOfTheConfiguration(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(script, []byte(`{"path": "${workspaceFolder}/main.go"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(AdapterEnv, "/bin/fake-adapter")
	ctx, cancel := context.WithCancel(context.Background())
	a, err := adapter(ctx, fakeSession{cfg: extension.DebugConfiguration{"script": script}, folder: "/work/p"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != extension.DebugAdapterExecutable || a.Command != "/bin/fake-adapter" || len(a.Args) != 2 || a.Args[0] != "-script" {
		t.Fatalf("adapter = %+v", a)
	}
	data, err := os.ReadFile(a.Args[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"/work/p/main.go"`) {
		t.Errorf("the script is %s", data)
	}
	cancel()
}

func TestTheAdapterFailures(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(good, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		env   string
		cfg   extension.DebugConfiguration
		route string
	}{
		{"no program", "", extension.DebugConfiguration{"script": good}, "adapter.DevDebugAdapterMissing"},
		{"no script", "/bin/x", extension.DebugConfiguration{}, "adapter.DevDebugScriptMissing"},
		{"a script that is not there", "/bin/x", extension.DebugConfiguration{"script": filepath.Join(dir, "none")}, "adapter.DevDebugScriptReadFailed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(AdapterEnv, tc.env)
			_, err := adapter(context.Background(), fakeSession{cfg: tc.cfg}, nil)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestTheAdapterCannotMakeItsFolder(t *testing.T) {
	good := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(good, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(AdapterEnv, "/bin/x")
	// a temporary folder that is not there: os.MkdirTemp fails
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	_, err := adapter(context.Background(), fakeSession{cfg: extension.DebugConfiguration{"script": good}}, nil)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("adapter.DevDebugScriptWriteFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
