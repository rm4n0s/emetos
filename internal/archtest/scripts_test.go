package archtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const repoRoot = "../.."

// runScript runs scripts/<name> from the repository root with env added to the
// current environment, and returns its output and exit status.
func runScript(t *testing.T, env []string, name string, args ...string) (stdout, stderr string, status int) {
	t.Helper()
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(root, "scripts", name), args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return out.String(), errOut.String(), exit.ExitCode()
	}
	if err != nil {
		t.Fatalf("running %s: %v", name, err)
	}
	return out.String(), errOut.String(), 0
}

// envOf parses the output of env into a map.
func envOf(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			m[k] = v
		}
	}
	return m
}

var isolatedVariables = []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"}

// realLooking are values that stand for the user's own folders. The run must
// not see any of them.
func realLooking() []string {
	var env []string
	for _, v := range isolatedVariables {
		env = append(env, v+"=/nonexistent-real/"+strings.ToLower(v))
	}
	return env
}

func TestIsolatedHidesTheUsersFolders(t *testing.T) {
	stdout, stderr, status := runScript(t, realLooking(), "isolated.sh", "--", "env")
	if status != 0 {
		t.Fatalf("status %d: %s", status, stderr)
	}
	got := envOf(stdout)
	// Go's caches are kept on purpose (TestIsolatedKeepsGoCaches), and with these
	// stand-in folders they derive from them; nothing else may carry one.
	for k, v := range got {
		if strings.Contains(v, "nonexistent-real") && !strings.HasPrefix(k, "GO") {
			t.Errorf("the run can see the user's folders: %s=%s", k, v)
		}
	}
	// Every folder is under one temporary folder, and the run's runtime folder is short.
	root := filepath.Dir(got["XDG_CONFIG_HOME"])
	if !strings.Contains(filepath.Base(root), "emetos-iso.") {
		t.Fatalf("the folders are not in an emetos-iso folder: %q", root)
	}
	for _, v := range isolatedVariables {
		if !strings.HasPrefix(got[v], root+"/") {
			t.Errorf("%s = %q, want it under %q", v, got[v], root)
		}
	}
	if n := len(got["XDG_RUNTIME_DIR"] + "/emetos-1000.sock"); n > 70 {
		t.Errorf("the runtime folder is too long for a socket path (%d bytes): %s", n, got["XDG_RUNTIME_DIR"])
	}
	// The folder is gone once the command has ended.
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("the temporary folder %q was not removed (%v)", root, err)
	}
}

func TestIsolatedKeepsGoCaches(t *testing.T) {
	want := map[string]string{}
	for _, name := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		out, err := exec.Command("go", "env", name).Output()
		if err != nil {
			t.Fatalf("go env %s: %v", name, err)
		}
		want[name] = strings.TrimSpace(string(out))
	}
	stdout, stderr, status := runScript(t, nil, "isolated.sh", "--", "env")
	if status != 0 {
		t.Fatalf("status %d: %s", status, stderr)
	}
	got := envOf(stdout)
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want the real %q", name, got[name], w)
		}
	}
}

func TestIsolatedCopiesTheConfig(t *testing.T) {
	fixture := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture, "snippets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "settings.json"), []byte(`{"editor.fontSize": 17}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "snippets", "go.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, status := runScript(t, nil, "isolated.sh", "--config", fixture, "--", "sh", "-c",
		`cat "$XDG_CONFIG_HOME/emetos/settings.json"; echo; ls "$XDG_CONFIG_HOME/emetos/snippets"`)
	if status != 0 {
		t.Fatalf("status %d: %s", status, stderr)
	}
	if !strings.Contains(stdout, `{"editor.fontSize": 17}`) || !strings.Contains(stdout, "go.json") {
		t.Errorf("the configuration was not copied:\n%s", stdout)
	}
	// The fixture itself is left alone.
	if _, err := os.Stat(filepath.Join(fixture, "settings.json")); err != nil {
		t.Errorf("the fixture was changed: %v", err)
	}
}

func TestIsolatedCopiesTheStateWithPrivateModes(t *testing.T) {
	fixture := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"remote-work.json", "sub/other.json"} {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte(`{"version":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, status := runScript(t, nil, "isolated.sh", "--state", fixture, "--", "sh", "-c",
		`cd "$XDG_STATE_HOME/emetos" && cat remote-work.json && stat -c '%a %n' . remote-work.json sub sub/other.json`)
	if status != 0 {
		t.Fatalf("status %d: %s", status, stderr)
	}
	for _, want := range []string{`{"version":1}`, "700 .", "600 remote-work.json", "700 sub", "600 sub/other.json"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the state is missing %q:\n%s", want, stdout)
		}
	}
	// the fixture keeps its own modes
	if info, err := os.Stat(filepath.Join(fixture, "remote-work.json")); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("the fixture was changed: %v, %v", info, err)
	}
	if _, _, status := runScript(t, nil, "isolated.sh", "--state", "/nonexistent-real/state", "--", "true"); status != 64 {
		t.Errorf("a state that is not a folder: status %d, want 64", status)
	}
	if _, _, status := runScript(t, nil, "isolated.sh", "--state"); status != 64 {
		t.Errorf("state without a value: status %d, want 64", status)
	}
}

func TestIsolatedExitStatusAndUsage(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		status int
	}{
		{"the command's status is passed on", []string{"--", "sh", "-c", "exit 7"}, 7},
		{"no command", []string{"--"}, 64},
		{"no separator", []string{"env"}, 64},
		{"config without a value", []string{"--config"}, 64},
		{"config that is not a folder", []string{"--config", "/nonexistent-real/config", "--", "true"}, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, stderr, status := runScript(t, nil, "isolated.sh", tt.args...); status != tt.status {
				t.Errorf("status = %d, want %d (stderr: %s)", status, tt.status, stderr)
			}
		})
	}
}

// TestIsolatedKeepsTheCompositorReachable makes a socket file the way a
// compositor does, and checks that the run finds it through its private runtime
// folder.
func TestIsolatedKeepsTheCompositorReachable(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to make a socket file")
	}
	runtime, err := os.MkdirTemp("", "emr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(runtime); err != nil {
			t.Errorf("removing %s: %v", runtime, err)
		}
	})
	sock := filepath.Join(runtime, "wayland-test")
	if out, err := exec.Command("python3", "-c",
		"import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])", sock).CombinedOutput(); err != nil {
		t.Fatalf("making a socket: %v: %s", err, out)
	}

	env := []string{"XDG_RUNTIME_DIR=" + runtime, "WAYLAND_DISPLAY=wayland-test"}
	stdout, stderr, status := runScript(t, env, "isolated.sh", "--", "sh", "-c",
		`echo "display=$WAYLAND_DISPLAY"; echo "runtime=$XDG_RUNTIME_DIR"; test -S "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY" && echo reachable`)
	if status != 0 {
		t.Fatalf("status %d: %s", status, stderr)
	}
	got := envOf(strings.ReplaceAll(stdout, "reachable", "reachable=yes"))
	if got["display"] != "wayland-test" || got["reachable"] != "yes" {
		t.Errorf("the compositor is not reachable:\n%s", stdout)
	}
	if got["runtime"] == runtime || got["runtime"] == "" {
		t.Errorf("the run uses the real runtime folder %q", got["runtime"])
	}
}

func TestProofUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no arguments", nil},
		{"the output is not a png", []string{"out.txt"}},
		{"an unknown option", []string{"out.png", "--nope"}},
		{"after without a value", []string{"out.png", "--after"}},
		{"commands without a value", []string{"out.png", "--commands"}},
		{"config that is not a folder", []string{"out.png", "--config", "/nonexistent-real/config"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A usage mistake is found before anything is built or started.
			if _, stderr, status := runScript(t, nil, "proof.sh", tt.args...); status != 64 {
				t.Errorf("status = %d, want 64 (stderr: %s)", status, stderr)
			}
		})
	}
}

// scripts/error-tags.sh lists the Tags, and with --untested prints nothing when every Tag has a route test
// (CLAUDE.md "Definition of done").
func TestErrorTagsScript(t *testing.T) {
	stdout, stderr, status := runScript(t, nil, "error-tags.sh", "--untested")
	if status != 0 || stdout != "" || stderr != "" {
		t.Errorf("--untested: status %d, stdout %q, stderr %q, want nothing", status, stdout, stderr)
	}
	stdout, stderr, status = runScript(t, nil, "error-tags.sh")
	if status != 0 || stderr != "" {
		t.Errorf("status %d, stderr %q", status, stderr)
	}
	for _, line := range []string{"ArchWalkFailed\tinternal/archtest/archtest.go:", "ErrorTagsExceptionsReadFailed\tinternal/archtest/errortags.go:"} {
		if !strings.Contains(stdout, line) {
			t.Errorf("the list lacks %q", line)
		}
	}
}
