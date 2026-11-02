//go:build emetos_gdb || emetos_lldbdap

package cpp

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

// The tests against the real debuggers (SPEC §10.31 "Tests", D43; step 25.9): the debugger of the extension over the fake API, the
// adapter started as the descriptor says (gdb -i dap, lldb-dap) and DAP spoken to it with extensions/internal/dapprobe. The C program
// is built by the test with `cc -g -O0` into a temporary folder. They are skipped when a program is not there (cc, gdb 14 or newer,
// lldb-dap). The adapters get a home folder of the test, so ~/.gdbinit and ~/.lldbinit are never read, and gdb is started with -nx.
//
//	go test -tags emetos_gdb ./extensions/cpp/...
//	go test -tags emetos_lldbdap ./extensions/cpp/...

// cSource is the program: square is called for 1 to n, the breakpoint is in line 5.
const cSource = `#include <stdio.h>
#include <stdlib.h>

static int square(int x) {
    int result = x * x;
    return result;
}

int main(int argc, char **argv) {
    int n = argc > 1 ? atoi(argv[1]) : 4;
    int total = 0;
    for (int i = 1; i <= n; i++) {
        total += square(i);
    }
    printf("total %d\n", total);
    return 0;
}
`

// buildC writes sum.c in a folder of the test and builds it with debug information; it returns the folder, the source and the program.
func buildC(t *testing.T) (dir, src, exe string) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("cc is not installed")
	}
	dir = t.TempDir()
	src, exe = filepath.Join(dir, "sum.c"), filepath.Join(dir, "sum")
	if err := os.WriteFile(src, []byte(cSource), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cc, "-g", "-O0", "-o", exe, src)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cc failed: %v\n%s", err, out)
	}
	return dir, src, exe
}

// debugRig is the extension activated in the folder, with the PATH of the machine (the other tests narrow it) so that gdb and lldb-dap
// are found.
func debugRig(t *testing.T, folder string, settings map[string]any) *rig {
	t.Helper()
	path := os.Getenv("PATH")
	r := newRig(t, false, nil)
	t.Setenv("PATH", path)
	r.folder = folder
	for k, v := range settings {
		r.api.SetSetting(k, v)
	}
	return r
}

// session resolves the configuration as the host does (the providers, then the second resolution), asks the factory for the adapter and
// starts it. A gdb that is too old skips the test.
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
	s := r.api.NewDebugSession("s1", typ, "native", sub)
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

// runCToABreakpoint runs the C program with the debugger to a breakpoint, reads the locals, steps over a line and lets it end.
func runCToABreakpoint(t *testing.T, typ string, settings map[string]any) {
	dir, src, exe := buildC(t)
	r := debugRig(t, dir, settings)
	d, c := r.session(extension.DebugConfiguration{"name": "Launch", "type": typ, "request": "launch", "program": exe, "args": "5"})
	if c["cwd"] != dir {
		t.Fatalf("configuration = %v", c)
	}
	if msg := d.Launch("launch", c, src, 5); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	reason, thread := d.Stopped()
	if reason != "breakpoint" {
		t.Errorf("stopped for %q", reason)
	}
	if body := d.Call("threads", nil); body != nil {
		if list, _ := body["threads"].([]any); len(list) > 0 {
			thread = int(list[0].(map[string]any)["id"].(float64))
		}
	}
	name, file, line, frame := d.Top(thread)
	if name != "square" || line != 5 || filepath.Base(file) != "sum.c" {
		t.Errorf("top frame = %s %s:%d, want square sum.c:5", name, file, line)
	}
	if vars := d.Locals(frame); vars["x"] != "1" {
		t.Errorf("locals = %v, want x = 1", vars)
	}
	if res := d.Call("evaluate", map[string]any{"expression": "x * 2", "frameId": frame, "context": "watch"}); !strings.Contains(res["result"].(string), "2") {
		t.Errorf("x * 2 = %v", res["result"])
	}

	// step over: the next line, and result is there
	if reason := d.Step("next", thread); reason != "step" {
		t.Errorf("after next stopped for %q", reason)
	}
	name, _, line, frame = d.Top(thread)
	if name != "square" || line != 6 {
		t.Errorf("after next = %s:%d, want square:6", name, line)
	}
	if vars := d.Locals(frame); vars["result"] != "1" || vars["x"] != "1" {
		t.Errorf("locals after next = %v, want x = 1 and result = 1", vars)
	}

	// the breakpoint is hit again for 2, with the frame of main below
	d.Call("continue", map[string]any{"threadId": thread})
	if reason, _ := d.Stopped(); reason != "breakpoint" {
		t.Errorf("second stop for %q", reason)
	}
	_, _, _, frame = d.Top(thread)
	if vars := d.Locals(frame); vars["x"] != "2" {
		t.Errorf("second locals = %v, want x = 2", vars)
	}

	// the end: without the breakpoint the program runs out and says so
	d.SetBreakpoints(src)
	d.Call("continue", map[string]any{"threadId": thread})
	end := time.Now().Add(60 * time.Second)
	for !strings.Contains(d.Output(), "total 55") && time.Now().Before(end) {
		d.Drain(200 * time.Millisecond)
	}
	d.Event("terminated")
	d.Drain(time.Second)
	if out := d.Output(); !strings.Contains(out, "total 55") {
		t.Errorf("the program's output = %q (stderr %q)", out, d.Stderr())
	}
	d.Finish(true)
}
