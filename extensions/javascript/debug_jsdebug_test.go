//go:build emetos_jsdebug

package javascript

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dapprobe"
)

// The tests of the real js-debug (tag emetos_jsdebug, D43): the pinned release of scripts/tool-versions.txt, unpacked by
// scripts/fetch-tools.sh into /var/tmp/emetos-tools/js-debug (EMETOS_TOOLS names another folder), run by the system's node, with a home
// folder of the test. The tests are skipped when js-debug or node is not there; they never download anything. Each starts the adapter as
// the descriptor of the extension says, speaks DAP to it with extensions/internal/dapprobe, and ends the program it debugged.

// toolsFolder is where scripts/fetch-tools.sh puts the tools.
func toolsFolder() string {
	if d := os.Getenv("EMETOS_TOOLS"); d != "" {
		return d
	}
	return "/var/tmp/emetos-tools"
}

// jsdebugRig is the pack on a machine with the system's node and the pinned js-debug, in a folder with a copy of the programs of testdata.
func jsdebugRig(t *testing.T) *rig {
	t.Helper()
	path := os.Getenv("PATH")
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	adapter := filepath.Join(toolsFolder(), "js-debug")
	if _, ok := serverIn(adapter); !ok {
		t.Skip("js-debug is not in " + adapter + ": scripts/fetch-tools.sh js-debug")
	}
	r := newRig(t, rigOptions{noNode: true, settings: map[string]any{AdapterPathSetting: adapter}})
	t.Setenv("PATH", path) // newRig gave the machine a PATH of its own; node is the system's here
	copyTree(t, filepath.Join("testdata", "jsdebug"), r.folder)
	return r
}

// copyTree copies the files of a folder (no sub-folders) so that the debuggee never writes into testdata.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	list, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.IsDir() {
			continue
		}
		in, err := os.Open(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(to, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, in); err != nil {
			t.Fatal(err)
		}
		if err := errorsJoin(in.Close(), out.Close()); err != nil {
			t.Fatal(err)
		}
	}
}

// errorsJoin is the first error of the arguments.
func errorsJoin(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// session resolves the configuration as the host does and starts js-debug as the descriptor of the factory says. It returns the client,
// the resolved configuration and the session type.
func (r *rig) session(c cfg) (*dapprobe.Client, cfg) {
	r.t.Helper()
	ctx := context.Background()
	got, err := r.api.ResolveDebugConfiguration(ctx, DebugType, r.folder, c)
	if err != nil || got == nil {
		r.t.Fatalf("resolve = %v, %v", got, err)
	}
	typ, _ := got["type"].(string)
	if typ != adapterType {
		r.t.Fatalf("the type of the resolved configuration is %q, want %q", typ, adapterType)
	}
	sub, err := r.api.ResolveDebugConfigurationSubstituted(ctx, typ, r.folder, got)
	if err != nil || sub == nil {
		r.t.Fatalf("substitute = %v, %v", sub, err)
	}
	s := r.api.NewDebugSession("s1", typ, "js", sub)
	s.SetFolder(r.folder)
	adapter, err := r.api.DebugAdapter(ctx, typ, s, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	if adapter.Kind != extension.DebugAdapterExecutableServer || !adapter.ServesChildren {
		r.t.Fatalf("adapter = %+v, want an executable server that serves the children of its session", adapter)
	}
	return dapprobe.StartServer(r.t, adapter, typ), sub
}

// starts collects the sessions js-debug asks for with startDebugging. The handler runs on the test's goroutine, inside the calls that
// read from the clients.
type starts struct{ asked []map[string]any }

// handle answers startDebugging and refuses the rest.
func (s *starts) handle(command string, args map[string]any) (map[string]any, string) {
	if command != "startDebugging" {
		return nil, "unsupported"
	}
	s.asked = append(s.asked, args)
	return map[string]any{}, ""
}

// child waits for the next session js-debug asks for (reading from the clients that may carry the request), connects to the server of
// the parent as the host does (DebugAdapter.ServesChildren), resolves its configuration as the host does for a child, and starts it:
// the breakpoints are the lines of the file.
func (r *rig) child(st *starts, parent *dapprobe.Client, readers []*dapprobe.Client, file string, lines ...int) *dapprobe.Client {
	r.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for len(st.asked) == 0 && time.Now().Before(deadline) {
		for _, c := range readers {
			c.Drain(100 * time.Millisecond)
		}
	}
	if len(st.asked) == 0 {
		r.t.Fatalf("js-debug asked for no session (output %q, adapter %q)", parent.Output(), parent.Stderr())
	}
	req := st.asked[0]
	st.asked = st.asked[1:]
	want, _ := req["configuration"].(map[string]any)
	if want["__pendingTargetId"] == nil || want["type"] != adapterType {
		r.t.Fatalf("startDebugging = %v, want a configuration with a __pendingTargetId of the type %s", req, adapterType)
	}
	request, _ := req["request"].(string)
	c := cfg{"request": request}
	for k, v := range want {
		c[k] = v
	}
	resolved, err := r.api.ResolveDebugConfiguration(context.Background(), adapterType, r.folder, c)
	if err != nil || resolved["__pendingTargetId"] != want["__pendingTargetId"] {
		r.t.Fatalf("resolve of the child = %v, %v", resolved, err)
	}
	sub, err := r.api.ResolveDebugConfigurationSubstituted(context.Background(), adapterType, r.folder, resolved)
	if err != nil || sub == nil {
		r.t.Fatalf("substitute of the child = %v, %v", sub, err)
	}
	kid := dapprobe.Connect(r.t, parent.Addr, adapterType)
	kid.OnRequest = st.handle
	if msg := kid.Launch(request, sub, file, lines...); msg != "" {
		r.t.Fatalf("the child session failed to start: %s", msg)
	}
	return kid
}

func TestJsDebugRunsANodeScriptToABreakpoint(t *testing.T) {
	r := jsdebugRig(t)
	file := filepath.Join(r.folder, "hello.js")
	d, c := r.session(cfg{"name": "Launch", "type": DebugType, "request": "launch", "program": file})
	if c["cwd"] != r.folder || c["__workspaceFolder"] != r.folder {
		t.Fatalf("configuration = %v", c)
	}
	st := &starts{}
	d.OnRequest = st.handle
	if msg := d.Launch("launch", c, "", 0); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	// the session of the launch only starts the program: the program is a session of its own, a child
	kid := r.child(st, d, []*dapprobe.Client{d}, file, 3)
	reason, thread := kid.Stopped()
	if reason != "breakpoint" {
		t.Errorf("stopped for %q", reason)
	}
	name, topFile, line, frame := kid.Top(thread)
	if !strings.HasSuffix(name, "discount") || line != 3 || filepath.Base(topFile) != "hello.js" {
		t.Errorf("top frame = %s %s:%d, want discount hello.js:3", name, topFile, line)
	}
	vars := kid.Locals(frame)
	if vars["rate"] != "0.25" || !strings.HasPrefix(vars["order"], "{") {
		t.Errorf("locals = %v, want rate 0.25 and order {...}", vars)
	}
	eval := kid.Call("evaluate", map[string]any{"expression": "order.total * rate", "frameId": frame, "context": "repl"})
	if eval["result"] != "10" {
		t.Errorf("order.total * rate = %v, want 10", eval)
	}
	if got := kid.Step("next", thread); got != "step" {
		t.Errorf("a step stopped for %q", got)
	}
	_, _, line, frame = kid.Top(thread)
	if line != 4 {
		t.Errorf("after a step the line is %d, want 4", line)
	}
	if vars := kid.Locals(frame); vars["amount"] != "10" {
		t.Errorf("locals after the step = %v, want amount 10", vars)
	}
	kid.Call("continue", map[string]any{"threadId": thread})
	kid.Event("terminated")
	kid.Drain(500 * time.Millisecond)
	if out := kid.Output(); !strings.Contains(out, "result 30") {
		t.Errorf("output of the program = %q, want it to hold the line \"result 30\"", out)
	}
	d.Event("terminated")
}

func TestJsDebugRunsATypeScriptFileThroughItsSourceMap(t *testing.T) {
	r := jsdebugRig(t)
	ts := filepath.Join(r.folder, "orders.ts")
	js := filepath.Join(r.folder, "orders.js")
	d, c := r.session(cfg{"name": "Launch TypeScript", "type": DebugType, "request": "launch", "program": js, "sourceMaps": true,
		"outFiles": []any{r.folder + "/**/*.js"}})
	st := &starts{}
	d.OnRequest = st.handle
	if msg := d.Launch("launch", c, "", 0); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	// the breakpoint is in the TypeScript source: line 10 is "const amount: number = order.total * rate;"
	kid := r.child(st, d, []*dapprobe.Client{d}, ts, 10)
	reason, thread := kid.Stopped()
	if reason != "breakpoint" {
		t.Errorf("stopped for %q", reason)
	}
	name, topFile, line, frame := kid.Top(thread)
	if !strings.HasSuffix(name, "discount") || line != 10 || filepath.Base(topFile) != "orders.ts" {
		t.Errorf("top frame = %s %s:%d, want discount orders.ts:10 (the source map was not followed)", name, topFile, line)
	}
	vars := kid.Locals(frame)
	if vars["rate"] != "0.25" || !strings.HasPrefix(vars["order"], "{") {
		t.Errorf("locals = %v, want rate 0.25 and order {...}", vars)
	}
	kid.Call("continue", map[string]any{"threadId": thread})
	kid.Event("terminated")
	kid.Drain(500 * time.Millisecond)
	if out := kid.Output(); !strings.Contains(out, "result 30") {
		t.Errorf("output of the program = %q, want it to hold the line \"result 30\"", out)
	}
	d.Event("terminated")
}

func TestJsDebugDebugsAChildProcessInASessionOfItsOwn(t *testing.T) {
	r := jsdebugRig(t)
	parentFile := filepath.Join(r.folder, "parent.js")
	childFile := filepath.Join(r.folder, "child.js")
	d, c := r.session(cfg{"name": "Launch Parent", "type": DebugType, "request": "launch", "program": parentFile})
	st := &starts{}
	d.OnRequest = st.handle
	if msg := d.Launch("launch", c, "", 0); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	// the program of the launch is a session, and the child process the program starts is another one, with a breakpoint of its own
	program := r.child(st, d, []*dapprobe.Client{d}, parentFile)
	program.OnRequest = st.handle
	grand := r.child(st, d, []*dapprobe.Client{d, program}, childFile, 3)
	reason, thread := grand.Stopped()
	if reason != "breakpoint" {
		t.Errorf("the child process stopped for %q", reason)
	}
	name, topFile, line, frame := grand.Top(thread)
	if !strings.HasSuffix(name, "greet") || line != 3 || filepath.Base(topFile) != "child.js" {
		t.Errorf("top frame of the child process = %s %s:%d, want greet child.js:3", name, topFile, line)
	}
	if vars := grand.Locals(frame); vars["who"] != "'world'" {
		t.Errorf("locals of the child process = %v, want who 'world'", vars)
	}
	grand.Call("continue", map[string]any{"threadId": thread})
	grand.Event("terminated")
	grand.Drain(500 * time.Millisecond)
	if out := grand.Output(); !strings.Contains(out, "hello world") {
		t.Errorf("output of the child process = %q, want it to hold the line \"hello world\"", out)
	}
	program.Event("terminated")
	d.Event("terminated")
}
