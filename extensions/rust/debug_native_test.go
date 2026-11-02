//go:build emetos_gdb || emetos_lldbdap

package rust

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dapprobe"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// The tests against the real debuggers (SPEC §10.31 "Tests", D43; step 25.9) for Rust: the providers of the extension and the debugger
// of extensions/internal/nativedebug (the C and C++ extension owns the debug types, the tests register the same code on the fake API)
// over the fake API, the adapter started as the descriptor says, and DAP spoken to it with extensions/internal/dapprobe. The program is
// the crate of testdata/debug, copied into a temporary folder and built by the configuration itself (its attribute `cargo`) with
// the system's cargo, a CARGO_HOME and a CARGO_TARGET_DIR of the test (the crate has no dependencies, so nothing is downloaded). They are
// skipped when cargo, gdb 14 or newer or lldb-dap is not there.
//
//	go test -tags emetos_gdb ./extensions/rust/...
//	go test -tags emetos_lldbdap ./extensions/rust/...

// copyCrate copies the crate of testdata/debug into a folder of the test and returns the folder.
func copyCrate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range []string{"Cargo.toml", filepath.Join("src", "main.rs")} {
		data, err := os.ReadFile(filepath.Join("testdata", "debug", rel))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, rel, string(data), 0o644)
	}
	return dir
}

// cargoEnv is the environment of the cargo of a test: its own home and target folders, and no network.
func cargoEnv(t *testing.T) map[string]any {
	t.Helper()
	base := t.TempDir()
	return map[string]any{"CARGO_HOME": filepath.Join(base, "cargo-home"), "CARGO_TARGET_DIR": filepath.Join(base, "target"), "CARGO_NET_OFFLINE": "true"}
}

// debugRig is the extension activated in the folder with the PATH of the machine, and the native debugger registered on its API.
func debugRig(t *testing.T, folder string, settings map[string]any) *rig {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is not installed")
	}
	path := os.Getenv("PATH")
	r := newRig(t, false)
	t.Setenv("PATH", path)
	r.folder = folder
	for k, v := range settings {
		r.api.SetSetting(k, v)
	}
	nativedebug.New(r.api).Register()
	return r
}

// session resolves the configuration as the host does, builds the program with cargo, asks the factory for the adapter and starts it.
func (r *rig) session(c extension.DebugConfiguration) (*dapprobe.Client, extension.DebugConfiguration) {
	r.t.Helper()
	typ, _ := c["type"].(string)
	got, err := r.api.ResolveDebugConfiguration(context.Background(), typ, r.folder, c)
	if err != nil || got == nil {
		r.t.Fatalf("resolve = %v, %v", got, err)
	}
	sub, err := r.api.ResolveDebugConfigurationSubstituted(context.Background(), typ, r.folder, got)
	if err != nil || sub == nil {
		r.t.Fatalf("substitute = %v, %v", sub, err)
	}
	s := r.api.NewDebugSession("s1", typ, "rust", sub)
	s.SetFolder(r.folder)
	adapter, err := r.api.DebugAdapter(context.Background(), typ, s, nil)
	if appErr, ok := errors.FromError(err); ok && (appErr.Tag == "GdbTooOld" || appErr.Tag == "GdbNotFound" || appErr.Tag == "LldbDapNotFound") {
		r.t.Skip(appErr.Message)
	}
	if err != nil {
		r.t.Fatal(err)
	}
	return dapprobe.Start(r.t, adapter, typ), sub
}

// gdbSettings start gdb without the init files and without debuginfod.
var gdbSettings = map[string]any{nativedebug.GDBArgumentsSetting: []any{"-nx", "-iex", "set debuginfod enabled off"}}

// runRustToABreakpoint builds the crate with cargo as the configuration says, runs it with the debugger to a breakpoint in square, reads
// the locals, steps over a line and lets the program end. With test the program is the test executable that `cargo test --no-run`
// builds and the breakpoint stops the test `squares`.
func runRustToABreakpoint(t *testing.T, typ string, settings map[string]any, test bool) {
	dir := copyCrate(t)
	src := filepath.Join(dir, "src", "main.rs")
	r := debugRig(t, dir, settings)
	cargo := map[string]any{"args": []any{"build", "--bin=sums"}, "env": cargoEnv(t), "filter": map[string]any{"name": "sums", "kind": "bin"}}
	cfg := extension.DebugConfiguration{"name": "Launch", "type": typ, "request": "launch", "args": []any{"5"}}
	if test {
		cargo["args"] = []any{"test", "--no-run", "--bin=sums"}
		cfg["args"] = []any{"--test-threads=1", "squares"}
	}
	cfg["cargo"] = cargo
	d, c := r.session(cfg)
	if _, kept := c["cargo"]; kept || c["program"] == nil || c["cwd"] != dir {
		t.Fatalf("configuration = %v", c)
	}
	if msg := d.Launch("launch", c, src, 4); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	reason, thread := d.Stopped()
	if reason != "breakpoint" {
		t.Errorf("stopped for %q", reason)
	}
	name, file, line, frame := d.Top(thread)
	if !strings.HasSuffix(name, "square") || line != 4 || filepath.Base(file) != "main.rs" {
		t.Errorf("top frame = %s %s:%d, want square main.rs:4", name, file, line)
	}
	want := "1"
	if test {
		want = "3"
	}
	if vars := d.Locals(frame); vars["x"] != want {
		t.Errorf("locals = %v, want x = %s", vars, want)
	}
	if reason := d.Step("next", thread); reason != "step" {
		t.Errorf("after next stopped for %q", reason)
	}
	name, _, line, frame = d.Top(thread)
	if !strings.HasSuffix(name, "square") || line != 5 {
		t.Errorf("after next = %s:%d, want square:5", name, line)
	}
	wantResult := "1"
	if test {
		wantResult = "9"
	}
	if vars := d.Locals(frame); vars["result"] != wantResult {
		t.Errorf("locals after next = %v, want result = %s", vars, wantResult)
	}

	// the end: without the breakpoint the program runs out and says so
	d.SetBreakpoints(src)
	d.Call("continue", map[string]any{"threadId": thread})
	phrase := "total 55"
	if test {
		phrase = "1 passed"
	}
	end := time.Now().Add(60 * time.Second)
	for !strings.Contains(d.Output(), phrase) && time.Now().Before(end) {
		d.Drain(200 * time.Millisecond)
	}
	d.Event("terminated")
	d.Drain(time.Second)
	if out := d.Output(); !strings.Contains(out, phrase) {
		t.Errorf("the program's output = %q (stderr %q)", out, d.Stderr())
	}
	d.Finish(true)
}
