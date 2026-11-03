//go:build emetos_dlv

package golang

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// The tests against the real Delve (SPEC §10.31 "Tests", D43; step 25.7). They run the descriptor and the providers of the Go debugger
// over the fake API, start the `dlv dap` the descriptor describes and speak DAP to it with a small client of their own (an extension
// may not import the debug core). The programs they debug are in testdata/dlv, copied into a temporary folder. They are skipped when the
// pinned dlv (scripts/fetch-tools.sh dlv: /var/tmp/emetos-tools/bin/dlv) or go is not there. Every process they start runs under
// `timeout -k` in a process group of its own and is killed when the test ends.
//
//	go test -tags emetos_dlv ./extensions/golang/... -v

// pinnedDlv is where scripts/fetch-tools.sh dlv puts the pinned Delve.
const pinnedDlv = "/var/tmp/emetos-tools/bin/dlv"

// toolsDir is the folder of the pinned tools, whose Go caches the builds of the tests share.
const toolsDir = "/var/tmp/emetos-tools"

// dlvEnv requires the pinned dlv and go, and gives the test a Go environment of its own: the caches below the tools folder (nothing
// is written to the user's), no module download, and a temporary HOME and XDG folders.
func dlvEnv(t *testing.T) {
	t.Helper()
	if st, err := os.Stat(pinnedDlv); err != nil || st.IsDir() {
		t.Skipf("the pinned dlv is not at %s (scripts/fetch-tools.sh dlv)", pinnedDlv)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	if _, err := exec.LookPath("timeout"); err != nil {
		t.Skip("timeout is not installed")
	}
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": home + "/config", "XDG_DATA_HOME": home + "/data", "XDG_CACHE_HOME": home + "/cache", "XDG_STATE_HOME": home + "/state",
		"GOCACHE": toolsDir + "/gocache", "GOMODCACHE": toolsDir + "/gomodcache", "GOPATH": toolsDir + "/gopath", "GOFLAGS": "", "GOENV": "off",
		"GOTOOLCHAIN": "local", "GOPROXY": "off", "CGO_ENABLED": "0", "GOWORK": "off", "GOBIN": "",
	} {
		t.Setenv(k, v)
	}
}

// copyProgram copies a program of testdata/dlv into a temporary folder and returns the folder.
func copyProgram(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", "dlv", name)
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// dlvRig is the rig of the debugger with the pinned dlv.
func dlvRig(t *testing.T) *rig {
	t.Helper()
	dlvEnv(t)
	r := newRig(t, rigOptions{realGo: true, settings: map[string]any{AlternateToolsSetting: map[string]any{"dlv": pinnedDlv}}})
	return r
}

// ---- a small DAP client ----

// dap is a connection to `dlv dap`: it sends requests and reads the messages in the order they come.
type dap struct {
	t      *testing.T
	conn   net.Conn
	msgs   chan map[string]any
	seq    int
	events []map[string]any
	// responses are the responses that came while the test waited for something else, by request number.
	responses map[int]map[string]any
	// output is the text of the output events seen so far.
	output strings.Builder
	// stdio is what the dlv program wrote to its standard output and error after its port line: the output of the program it runs,
	// which the host writes to the Debug Console (DebugAdapter.ConsoleOutput).
	stdio *lockedBuilder
}

// startDlv starts the adapter the extension describes, as the host does (the process group of its own, the working folder and the
// environment of the descriptor), under timeout -k, and connects to the port it prints.
func startDlv(t *testing.T, a *extension.DebugAdapter) *dap {
	t.Helper()
	if a.Kind != extension.DebugAdapterExecutableServer || a.Command != pinnedDlv {
		t.Fatalf("adapter = %+v", a)
	}
	cmd := exec.Command("timeout", append([]string{"-k", "5", "240", a.Command}, a.Args...)...)
	cmd.Dir = a.Cwd
	cmd.Env = os.Environ()
	for k, v := range a.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdio := &lockedBuilder{}
	cmd.Stderr = stdio
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	re := regexp.MustCompile(`listening at: (\S+)`)
	var addr string
	deadline := time.After(60 * time.Second)
	for addr == "" {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("dlv ended without printing its port: %s", stdio.String())
			}
			if m := re.FindStringSubmatch(l); m != nil {
				addr = m[1]
			}
		case <-deadline:
			t.Fatalf("dlv did not print its port: %s", stdio.String())
		}
	}
	go func() {
		for l := range lines {
			stdio.WriteString(l + "\n")
		}
	}()
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	d := &dap{t: t, conn: conn, msgs: make(chan map[string]any, 256), responses: map[int]map[string]any{}, stdio: stdio}
	t.Cleanup(func() { _ = conn.Close() })
	go d.read()
	return d
}

// read reads the messages of the adapter into the channel.
func (d *dap) read() {
	defer close(d.msgs)
	r := bufio.NewReader(d.conn)
	for {
		length := -1
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
				length, _ = strconv.Atoi(v)
			}
		}
		if length < 0 {
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(body, &m) != nil {
			return
		}
		d.msgs <- m
	}
}

// next reads the next message with a deadline.
func (d *dap) next(within time.Duration) (map[string]any, bool) {
	select {
	case m, ok := <-d.msgs:
		return m, ok
	case <-time.After(within):
		return nil, false
	}
}

// note keeps the output of an event for the test to read.
func (d *dap) note(m map[string]any) {
	if m["event"] == "output" {
		if body, ok := m["body"].(map[string]any); ok {
			if s, ok := body["output"].(string); ok {
				d.output.WriteString(s)
			}
		}
	}
	if m["type"] == "response" {
		if seq, ok := m["request_seq"].(float64); ok {
			d.responses[int(seq)] = m
		}
		return
	}
	d.events = append(d.events, m)
}

// send writes a request and returns its number.
func (d *dap) send(command string, args any) int {
	d.t.Helper()
	d.seq++
	msg := map[string]any{"seq": d.seq, "type": "request", "command": command}
	if args != nil {
		msg["arguments"] = args
	}
	data, err := json.Marshal(msg)
	if err != nil {
		d.t.Fatal(err)
	}
	if _, err := fmt.Fprintf(d.conn, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
		d.t.Fatal(err)
	}
	return d.seq
}

// response waits for the response to the request; events that come meanwhile are kept. It returns the body, and the message of the
// adapter when the request failed.
func (d *dap) response(seq int, command string) (map[string]any, string) {
	d.t.Helper()
	for {
		if m, ok := d.responses[seq]; ok {
			delete(d.responses, seq)
			body, _ := m["body"].(map[string]any)
			if m["success"] == false {
				msg, _ := m["message"].(string)
				if body != nil {
					if e, ok := body["error"].(map[string]any); ok {
						if f, ok := e["format"].(string); ok {
							msg = f
						}
					}
				}
				return body, msg
			}
			return body, ""
		}
		m, ok := d.next(90 * time.Second)
		if !ok {
			d.t.Fatalf("no response to %s (output: %q)", command, d.output.String())
		}
		d.note(m)
	}
}

// drain reads what the adapter sends for the time given, so that the output of a failure is there to read.
func (d *dap) drain(within time.Duration) {
	end := time.Now().Add(within)
	for time.Until(end) > 0 {
		m, ok := d.next(time.Until(end))
		if !ok {
			return
		}
		d.note(m)
	}
}

// call sends a request and requires it to succeed.
func (d *dap) call(command string, args any) map[string]any {
	d.t.Helper()
	body, msg := d.response(d.send(command, args), command)
	if msg != "" {
		d.t.Fatalf("%s failed: %s", command, msg)
	}
	return body
}

// event waits for an event with the name, and returns its body.
func (d *dap) event(name string) map[string]any {
	d.t.Helper()
	for {
		for i, m := range d.events {
			if m["event"] == name {
				d.events = append(d.events[:i], d.events[i+1:]...)
				body, _ := m["body"].(map[string]any)
				return body
			}
		}
		m, ok := d.next(90 * time.Second)
		if !ok {
			d.t.Fatalf("no %s event (output: %q)", name, d.output.String())
		}
		d.note(m)
	}
}

// start runs initialize and the request ("launch" or "attach") with the configuration, then the breakpoints in the file (lines),
// configurationDone, and waits for the program to stop at the first. A request that fails gives its message.
func (d *dap) start(request string, c extension.DebugConfiguration, file string, lines ...int) (failed string) {
	d.t.Helper()
	d.call("initialize", map[string]any{"clientID": "emetos", "adapterID": "go", "linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path"})
	// dlv answers launch and attach once the program is built and started; initialized follows
	seq := d.send(request, c)
	d.event("initialized")
	if file != "" {
		bps := make([]map[string]any, len(lines))
		for i, l := range lines {
			bps[i] = map[string]any{"line": l}
		}
		body := d.call("setBreakpoints", map[string]any{"source": map[string]any{"path": file}, "breakpoints": bps})
		for i, b := range body["breakpoints"].([]any) {
			if b.(map[string]any)["verified"] != true {
				d.t.Fatalf("the breakpoint of line %d is not verified: %v", lines[i], b)
			}
		}
	}
	d.call("configurationDone", nil)
	if _, msg := d.response(seq, request); msg != "" {
		return msg
	}
	return ""
}

// stopped waits for a stopped event and returns its reason and the thread.
func (d *dap) stopped() (reason string, thread int) {
	d.t.Helper()
	body := d.event("stopped")
	reason, _ = body["reason"].(string)
	id, _ := body["threadId"].(float64)
	return reason, int(id)
}

// top returns the name and line of the top frame of the thread and its id.
func (d *dap) top(thread int) (name string, line, frame int) {
	d.t.Helper()
	body := d.call("stackTrace", map[string]any{"threadId": thread, "startFrame": 0, "levels": 5})
	frames := body["stackFrames"].([]any)
	if len(frames) == 0 {
		d.t.Fatal("the stack is empty")
	}
	f := frames[0].(map[string]any)
	return f["name"].(string), int(f["line"].(float64)), int(f["id"].(float64))
}

// locals returns the variables of the Locals scope of the frame as name to value.
func (d *dap) locals(frame int) map[string]string {
	d.t.Helper()
	scopes := d.call("scopes", map[string]any{"frameId": frame})["scopes"].([]any)
	out := map[string]string{}
	for _, s := range scopes {
		sc := s.(map[string]any)
		if sc["name"] != "Locals" {
			continue
		}
		vars := d.call("variables", map[string]any{"variablesReference": sc["variablesReference"]})["variables"].([]any)
		for _, v := range vars {
			vm := v.(map[string]any)
			out[vm["name"].(string)] = vm["value"].(string)
		}
	}
	return out
}

// finish disconnects, then waits for the adapter's end of the session.
func (d *dap) finish(terminate bool) {
	d.t.Helper()
	_, msg := d.response(d.send("disconnect", map[string]any{"terminateDebuggee": terminate}), "disconnect")
	if msg != "" {
		d.t.Fatalf("disconnect failed: %s", msg)
	}
}

// ---- the sessions ----

// session resolves the configuration as the host does (the providers, then the second resolution), asks the factory for the adapter
// and starts dlv. It returns the connection and the configuration `dlv` is sent.
func (r *rig) session(c extension.DebugConfiguration) (*dap, extension.DebugConfiguration) {
	r.t.Helper()
	got := r.resolve(c)
	if got.err != nil || got.cfg == nil {
		r.t.Fatalf("resolve = %+v", got)
	}
	sub := r.substitute(got.cfg)
	if sub.err != nil || sub.cfg == nil {
		r.t.Fatalf("substitute = %+v", sub)
	}
	s := r.api.NewDebugSession("s1", DebugType, "dlv", sub.cfg)
	s.SetFolder(r.folder)
	adapter, err := r.api.DebugAdapter(context.Background(), DebugType, s, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	return startDlv(r.t, adapter), sub.cfg
}

func TestDlvRunsAProgramToABreakpoint(t *testing.T) {
	r := dlvRig(t)
	prog := copyProgram(t, "hello")
	d, c := r.session(extension.DebugConfiguration{
		"name": "Launch", "type": "go", "request": "launch", "mode": "debug", "program": prog, "args": "7",
	})
	if c["__buildDir"] != prog || c["program"] != "." {
		t.Fatalf("the program is built in %v as %v", c["__buildDir"], c["program"])
	}
	if msg := d.start("launch", c, filepath.Join(prog, "main.go"), 39); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	reason, thread := d.stopped()
	if reason != "breakpoint" {
		t.Errorf("stopped for %q", reason)
	}

	// the stack, and the variables of main
	name, line, frame := d.top(thread)
	if name != "main.main" || line != 39 {
		t.Errorf("top frame = %s:%d, want main.main:39", name, line)
	}
	vars := d.locals(frame)
	if vars["n"] != "7" || !strings.Contains(vars["p"], "X: 7") || !strings.Contains(vars["p"], "Y: 14") {
		t.Errorf("locals = %v", vars)
	}
	if res := d.call("evaluate", map[string]any{"expression": "n * 2", "frameId": frame, "context": "repl"}); res["result"] != "14" {
		t.Errorf("n * 2 = %v", res["result"])
	}

	// the goroutines: the main one and the one that waits
	threads := d.call("threads", nil)["threads"].([]any)
	var mainSeen, waiterSeen bool
	for _, th := range threads {
		n, _ := th.(map[string]any)["name"].(string)
		mainSeen = mainSeen || strings.Contains(n, "main.main")
		waiterSeen = waiterSeen || strings.Contains(n, "main.main.func1")
	}
	if len(threads) < 2 || !mainSeen || !waiterSeen {
		t.Errorf("goroutines = %v", threads)
	}

	// step over: the line after, and the result is there
	d.call("next", map[string]any{"threadId": thread})
	if reason, _ := d.stopped(); reason != "step" {
		t.Errorf("after next stopped for %q", reason)
	}
	name, line, frame = d.top(thread)
	if name != "main.main" || line != 40 {
		t.Errorf("after next = %s:%d, want main.main:40", name, line)
	}
	if vars := d.locals(frame); vars["result"] != "140" {
		t.Errorf("result = %q, want 140 (the squares of 1 to 7)", vars["result"])
	}

	// the end: the program runs out and says so
	d.call("continue", map[string]any{"threadId": thread})
	d.event("terminated")
	d.drain(time.Second)
	if out := d.programOutput(); !strings.Contains(out, "result 140") {
		t.Errorf("the program's output = %q", out)
	}
	d.finish(true)
}

func TestDlvDebugsATestFromTheLens(t *testing.T) {
	r := dlvRig(t)
	prog := copyProgram(t, "hello")
	r.openGo("main.go", "package main\n")
	if err := r.api.Run(DebugTestFunctionCommand, map[string]any{"uri": fileURI(filepath.Join(prog, "hello_test.go")), "name": "TestCompute"}); err != nil {
		t.Fatal(err)
	}
	starts := r.debugStarts(1)
	d, c := r.session(starts[0].Target.Config)
	if c["mode"] != "test" || c["__buildDir"] != prog {
		t.Fatalf("configuration = %v", c)
	}
	// breakpoints in both tests: only the one the lens selected runs
	if msg := d.start("launch", c, filepath.Join(prog, "hello_test.go"), 10, 18); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	reason, thread := d.stopped()
	name, line, frame := d.top(thread)
	if reason != "breakpoint" || !strings.HasSuffix(name, ".TestCompute") || line != 10 {
		t.Errorf("stopped for %q at %s:%d", reason, name, line)
	}
	if vars := d.locals(frame); vars["want"] != "14" {
		t.Errorf("locals = %v", vars)
	}
	d.call("continue", map[string]any{"threadId": thread})
	d.event("terminated")
	d.drain(time.Second)
	out := d.programOutput()
	if !strings.Contains(out, "TestCompute ran") || strings.Contains(out, "TestOther ran") {
		t.Errorf("the test output = %q", out)
	}
	d.finish(true)
}

func TestDlvAttachesToARunningProcess(t *testing.T) {
	r := dlvRig(t)
	prog := copyProgram(t, "attach")
	bin := filepath.Join(t.TempDir(), "attach")
	build := exec.Command("go", "build", "-gcflags=all=-N -l", "-o", bin, ".")
	build.Dir = prog
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	// the target runs under timeout -k in a process group of its own, and prints its process id
	target := exec.Command("timeout", "-k", "5", "200", bin)
	target.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := target.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := target.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = target.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("the target printed no process id: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "pid ")))
	if err != nil {
		t.Fatalf("the target said %q", line)
	}

	d, c := r.session(extension.DebugConfiguration{"name": "Attach", "type": "go", "request": "attach", "processId": strconv.Itoa(pid)})
	if c["mode"] != "local" || c["processId"] != float64(pid) {
		t.Fatalf("configuration = %v", c)
	}
	if msg := d.start("attach", c, filepath.Join(prog, "main.go"), 16); msg != "" {
		// the kernel may forbid one process to trace another (ptrace_scope): dlv says so, and the test cannot go on
		low := strings.ToLower(msg)
		if strings.Contains(low, "not permitted") || strings.Contains(low, "permission denied") || strings.Contains(low, "ptrace") {
			t.Skipf("the kernel does not allow dlv to attach to a process (dlv: %s)", msg)
		}
		t.Fatalf("attach failed: %s", msg)
	}
	reason, thread := d.stopped()
	name, line2, frame := d.top(thread)
	if reason != "breakpoint" || name != "main.work" || line2 != 16 {
		t.Errorf("stopped for %q at %s:%d", reason, name, line2)
	}
	if vars := d.locals(frame); vars["round"] == "" {
		t.Errorf("locals = %v", vars)
	}
	// leaving the process running: the debugger lets it go
	d.finish(false)
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Errorf("the target is gone after the debugger left it: %v", err)
	}
}

func TestDlvLaunchOfAProgramThatDoesNotBuildFails(t *testing.T) {
	r := dlvRig(t)
	prog := copyProgram(t, "hello")
	if err := os.WriteFile(filepath.Join(prog, "main.go"), []byte("package main\n\nfunc main() { undefinedCall() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, c := r.session(extension.DebugConfiguration{"name": "Launch", "type": "go", "request": "launch", "mode": "debug", "program": prog})
	d.call("initialize", map[string]any{"clientID": "emetos", "adapterID": "go", "linesStartAt1": true, "pathFormat": "path"})
	_, msg := d.response(d.send("launch", c), "launch")
	d.drain(2 * time.Second)
	// the failure is the response; the compiler's words are in the output of the Debug Console
	if !strings.Contains(msg, "Build error") || !strings.Contains(d.output.String(), "undefinedCall") {
		t.Errorf("launch message = %q, output = %q; want a build error and the compiler's words", msg, d.output.String())
	}
}

// lockedBuilder is a strings.Builder that goroutines may write to.
type lockedBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

// Write implements io.Writer.
func (l *lockedBuilder) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// WriteString adds text.
func (l *lockedBuilder) WriteString(s string) { _, _ = l.Write([]byte(s)) }

// String is the text so far.
func (l *lockedBuilder) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// programOutput is the output of the program under the debugger: the output events of the adapter, and the lines dlv wrote to its own
// streams, which the host turns into the same (DebugAdapter.ConsoleOutput).
func (d *dap) programOutput() string { return d.output.String() + d.stdio.String() }

// TestDlvDebugsATestFromTheDebugProfileOfTheTestingView debugs a test the way the Testing view does (step 26.5): the Debug profile of the
// controller gets a request for one test of a folder with the program of testdata/dlv/hello, builds the configuration of the session
// (mode test, the folder of the package, -test.run for the test) and Delve stops at a breakpoint in it.
func TestDlvDebugsATestFromTheDebugProfileOfTheTestingView(t *testing.T) {
	dlvEnv(t)
	r := newRig(t, rigOptions{realGo: true, copyFrom: filepath.Join("testdata", "dlv", "hello"), settings: map[string]any{AlternateToolsSetting: map[string]any{"dlv": pinnedDlv}}})
	r.openGo("extra.go", "package main\n")
	r.discovered()
	item := find(t, r.ctl().Items().All()[0].Children(), "hello_test.go/TestCompute")
	if _, err := profileOf(r, extension.TestKindDebug).Run(context.Background(), []*extension.TestItem{item}, nil, false); err != nil {
		t.Fatal(err)
	}
	starts := r.debugStarts(1)
	d, c := r.session(starts[0].Target.Config)
	prog := r.folder
	if c["mode"] != "test" || c["__buildDir"] != prog {
		t.Fatalf("configuration = %v", c)
	}
	if args, _ := starts[0].Target.Config["args"].([]any); len(args) != 2 || args[0] != "-test.run" || args[1] != "^TestCompute$" {
		t.Errorf("args = %v", starts[0].Target.Config["args"])
	}
	if len(r.ctl().Runs()) != 0 {
		t.Error("the debug request made a run")
	}
	// breakpoints in both tests: only the one the item selected runs
	if msg := d.start("launch", c, filepath.Join(prog, "hello_test.go"), 10, 18); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	reason, thread := d.stopped()
	name, line, frame := d.top(thread)
	if reason != "breakpoint" || !strings.HasSuffix(name, ".TestCompute") || line != 10 {
		t.Errorf("stopped for %q at %s:%d", reason, name, line)
	}
	if vars := d.locals(frame); vars["want"] != "14" {
		t.Errorf("locals = %v", vars)
	}
	d.call("continue", map[string]any{"threadId": thread})
	d.event("terminated")
	d.drain(time.Second)
	out := d.programOutput()
	if !strings.Contains(out, "TestCompute ran") || strings.Contains(out, "TestOther ran") {
		t.Errorf("the test output = %q", out)
	}
	d.finish(true)
}
