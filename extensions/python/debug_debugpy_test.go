//go:build emetos_debugpy

package python

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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// The tests against the real debugpy (SPEC §10.31 "Tests", D43; step 25.8). They run the providers and the descriptor of the Python
// debugger over the fake API, start the adapter the descriptor describes (or the debuggee that listens, for an attach) and speak DAP
// to it with a small client of their own (an extension may not import the debug core). The programs they debug are in testdata/debugpy,
// copied into a temporary folder. They are skipped when the interpreter of the tools folder (scripts/fetch-tools.sh python-tools) or its
// debugpy is not there. Every process they start runs under `timeout -k` in a process group of its own and is killed when the test ends.
//
//	go test -tags emetos_debugpy ./extensions/python/... -v

// pinnedPython is the interpreter of the venv that scripts/fetch-tools.sh python-tools makes.
const pinnedPython = "/var/tmp/emetos-tools/venv/bin/python"

// debugpyEnv requires the interpreter and its debugpy and the timeout program, and returns the path of timeout (the rig narrows PATH).
func debugpyEnv(t *testing.T) (timeout string) {
	t.Helper()
	if st, err := os.Stat(pinnedPython); err != nil || st.IsDir() {
		t.Skipf("the interpreter of the tools folder is not at %s (scripts/fetch-tools.sh python-tools)", pinnedPython)
	}
	timeout, err := exec.LookPath("timeout")
	if err != nil {
		t.Skip("timeout is not installed")
	}
	if _, err := os.Stat("/var/tmp/emetos-tools/venv/lib"); err != nil {
		t.Skip("the venv has no packages")
	}
	return timeout
}

// debugpyRig is the extension activated in a folder with the interpreter of the tools folder as python.defaultInterpreterPath.
func debugpyRig(t *testing.T, settings map[string]any) (*rig, string) {
	t.Helper()
	timeout := debugpyEnv(t)
	if settings == nil {
		settings = map[string]any{}
	}
	settings[DefaultInterpreterSetting] = pinnedPython
	r := newRig(t, rigOptions{settings: settings, noPython: true})
	// the venv must have debugpy: the check the extension itself makes
	if err := r.ext.checkDebugpy(context.Background(), pinnedPython); err != nil {
		t.Skipf("the venv has no debugpy: %v", err)
	}
	r.api.Pump()
	return r, timeout
}

// copyProgram copies the programs of testdata/debugpy into the folder of the rig.
func (r *rig) copyPrograms() {
	r.t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", "debugpy"))
	if err != nil {
		r.t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("testdata", "debugpy", e.Name()))
		if err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.folder, e.Name()), data, 0o600); err != nil {
			r.t.Fatal(err)
		}
	}
}

// ---- a small DAP client ----

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

// String is the text so far.
func (l *lockedBuilder) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// dap is a connection to debugpy: it sends requests and reads the messages in the order they come.
type dap struct {
	t      *testing.T
	w      io.Writer
	msgs   chan map[string]any
	seq    int
	events []map[string]any
	// responses are the responses that came while the test waited for something else, by request number.
	responses map[int]map[string]any
	// output is the text of the output events seen so far.
	output strings.Builder
	// stderr is what the adapter wrote to its standard error.
	stderr *lockedBuilder
	// terminal is how the client answers runInTerminal: nil refuses it.
	terminal func(args map[string]any) (pid int)
	// terminalRequests are the runInTerminal requests that came.
	terminalRequests []map[string]any
}

// newDAP reads the messages of r into a client that writes to w.
func newDAP(t *testing.T, r io.Reader, w io.Writer, stderr *lockedBuilder) *dap {
	d := &dap{t: t, w: w, msgs: make(chan map[string]any, 256), responses: map[int]map[string]any{}, stderr: stderr}
	go d.read(r)
	return d
}

// terminate asks debugpy to end the program it debugs and waits a few seconds for its answer. It runs when a test ends, whatever
// state it left the session in: debugpy starts the program in a process group of its own, so killing the group of the adapter would
// leave the program running.
func (d *dap) terminate() {
	d.seq++
	data, err := json.Marshal(map[string]any{"seq": d.seq, "type": "request", "command": "disconnect", "arguments": map[string]any{"terminateDebuggee": true}})
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(d.w, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
		return
	}
	end := time.After(8 * time.Second)
	for {
		select {
		case m, ok := <-d.msgs:
			if !ok {
				return
			}
			if seq, _ := m["request_seq"].(float64); m["type"] == "response" && int(seq) == d.seq {
				return
			}
		case <-end:
			return
		}
	}
}

// startAdapter starts the adapter the extension describes, as the host does (the process group of its own, the working folder of the
// descriptor), under timeout -k, and speaks DAP on its standard input and output.
func startAdapter(t *testing.T, timeout string, a *extension.DebugAdapter) *dap {
	t.Helper()
	if a.Kind != extension.DebugAdapterExecutable || a.Command != pinnedPython {
		t.Fatalf("adapter = %+v", a)
	}
	cmd := exec.Command(timeout, append([]string{"-k", "5", "240", a.Command}, a.Args...)...)
	cmd.Dir = a.Cwd
	cmd.Env = os.Environ()
	for k, v := range a.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &lockedBuilder{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	d := newDAP(t, stdout, stdin, stderr)
	t.Cleanup(d.terminate) // runs before the kill above
	return d
}

// dial connects to a debugpy that listens, trying again while it starts.
func dial(t *testing.T, host string, port int) *dap {
	t.Helper()
	var conn net.Conn
	var err error
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err = net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), time.Second); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("debugpy does not listen on %s:%d: %v", host, port, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	d := newDAP(t, conn, conn, &lockedBuilder{})
	t.Cleanup(d.terminate) // runs before the connection is closed
	return d
}

// read reads the messages of the adapter into the channel.
func (d *dap) read(rd io.Reader) {
	defer close(d.msgs)
	r := bufio.NewReader(rd)
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

// write sends a message.
func (d *dap) write(msg map[string]any) {
	d.t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		d.t.Fatal(err)
	}
	if _, err := fmt.Fprintf(d.w, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
		d.t.Fatal(err)
	}
}

// note keeps what a message says for the test to read; a request of the adapter is answered.
func (d *dap) note(m map[string]any) {
	switch m["type"] {
	case "response":
		if seq, ok := m["request_seq"].(float64); ok {
			d.responses[int(seq)] = m
		}
		return
	case "request":
		d.answer(m)
		return
	}
	if m["event"] == "output" {
		if body, ok := m["body"].(map[string]any); ok {
			if s, ok := body["output"].(string); ok {
				d.output.WriteString(s)
			}
		}
	}
	d.events = append(d.events, m)
}

// answer answers a request of the adapter: runInTerminal is run by the test (as the terminal of the window would), anything else is
// refused.
func (d *dap) answer(m map[string]any) {
	d.t.Helper()
	seq, _ := m["seq"].(float64)
	reply := map[string]any{"seq": 0, "type": "response", "request_seq": int(seq), "command": m["command"], "success": true}
	if m["command"] == "runInTerminal" && d.terminal != nil {
		args, _ := m["arguments"].(map[string]any)
		d.terminalRequests = append(d.terminalRequests, args)
		reply["body"] = map[string]any{"processId": d.terminal(args)}
	} else {
		reply["success"], reply["message"] = false, "unsupported"
	}
	d.write(reply)
}

// send writes a request and returns its number.
func (d *dap) send(command string, args any) int {
	d.t.Helper()
	d.seq++
	msg := map[string]any{"seq": d.seq, "type": "request", "command": command}
	if args != nil {
		msg["arguments"] = args
	}
	d.write(msg)
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
			d.t.Fatalf("no response to %s (output: %q, stderr: %q)", command, d.output.String(), d.stderr.String())
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
			d.t.Fatalf("no %s event (output: %q, stderr: %q)", name, d.output.String(), d.stderr.String())
		}
		d.note(m)
	}
}

// start runs initialize and the request ("launch" or "attach") with the configuration, then the breakpoints in the file (lines), the
// exception filters, configurationDone, and returns what the request answered ("" when it succeeded).
func (d *dap) start(request string, c extension.DebugConfiguration, file string, filters []string, lines ...int) (failed string) {
	d.t.Helper()
	d.call("initialize", map[string]any{"clientID": "emetos", "adapterID": "debugpy", "linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path", "supportsRunInTerminalRequest": true})
	// debugpy answers launch and attach once configurationDone came; initialized follows the request
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
	if filters == nil {
		filters = []string{}
	}
	d.call("setExceptionBreakpoints", map[string]any{"filters": filters})
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

// top returns the name, file and line of the top frame of the thread and its id.
func (d *dap) top(thread int) (name, file string, line, frame int) {
	d.t.Helper()
	body := d.call("stackTrace", map[string]any{"threadId": thread, "startFrame": 0, "levels": 5})
	frames := body["stackFrames"].([]any)
	if len(frames) == 0 {
		d.t.Fatal("the stack is empty")
	}
	f := frames[0].(map[string]any)
	if src, ok := f["source"].(map[string]any); ok {
		file, _ = src["path"].(string)
	}
	return f["name"].(string), file, int(f["line"].(float64)), int(f["id"].(float64))
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

// finish disconnects, ending the program when terminate is true.
func (d *dap) finish(terminate bool) {
	d.t.Helper()
	_, msg := d.response(d.send("disconnect", map[string]any{"terminateDebuggee": terminate}), "disconnect")
	if msg != "" {
		d.t.Fatalf("disconnect failed: %s", msg)
	}
}

// ---- the sessions ----

// session resolves the configuration as the host does (the providers, then the second resolution), asks the factory for the adapter
// and starts it. It returns the connection and the configuration debugpy is sent.
func (r *rig) session(timeout string, c extension.DebugConfiguration) (*dap, extension.DebugConfiguration) {
	r.t.Helper()
	got := r.resolve(c)
	if got.err != nil || got.cfg == nil {
		r.t.Fatalf("resolve = %+v", got)
	}
	sub := r.substitute(got.cfg)
	if sub.err != nil || sub.cfg == nil {
		r.t.Fatalf("substitute = %+v", sub)
	}
	s := r.api.NewDebugSession("s1", DebugType, "py", sub.cfg)
	s.SetFolder(r.folder)
	adapter, err := r.api.DebugAdapter(context.Background(), DebugType, s, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	return startAdapter(r.t, timeout, adapter), sub.cfg
}

// programOutput is the output of the program under the debugger.
func (d *dap) programOutput() string { return d.output.String() }

func TestDebugpyRunsAProgramToABreakpoint(t *testing.T) {
	r, timeout := debugpyRig(t, nil)
	r.copyPrograms()
	prog := filepath.Join(r.folder, "hello.py")
	d, c := r.session(timeout, extension.DebugConfiguration{
		"name": "Launch", "type": "debugpy", "request": "launch", "program": prog, "args": []any{"4"}, "console": "internalConsole",
	})
	if c["python"] != pinnedPython || c["cwd"] != r.folder || c["justMyCode"] != true {
		t.Fatalf("configuration = %v", c)
	}
	if msg := d.start("launch", c, prog, []string{"uncaught"}, 6, 14); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	reason, thread := d.stopped()
	if reason != "breakpoint" {
		t.Errorf("stopped for %q", reason)
	}

	// the stack and the variables of compute
	name, file, line, frame := d.top(thread)
	if name != "compute" || line != 6 || filepath.Base(file) != "hello.py" {
		t.Errorf("top frame = %s %s:%d, want compute hello.py:6", name, file, line)
	}
	vars := d.locals(frame)
	if vars["n"] != "4" || vars["squares"] != "[1, 4, 9, 16]" {
		t.Errorf("locals = %v", vars)
	}
	if res := d.call("evaluate", map[string]any{"expression": "n * 2", "frameId": frame, "context": "repl"}); res["result"] != "8" {
		t.Errorf("n * 2 = %v", res["result"])
	}
	threads := d.call("threads", nil)["threads"].([]any)
	var mainSeen bool
	for _, th := range threads {
		if n, _ := th.(map[string]any)["name"].(string); strings.Contains(n, "MainThread") {
			mainSeen = true
		}
	}
	if !mainSeen {
		t.Errorf("threads = %v", threads)
	}

	// step over: the next line, and the sum is there
	d.call("next", map[string]any{"threadId": thread})
	if reason, _ := d.stopped(); reason != "step" {
		t.Errorf("after next stopped for %q", reason)
	}
	name, _, line, frame = d.top(thread)
	if name != "compute" || line != 7 {
		t.Errorf("after next = %s:%d, want compute:7", name, line)
	}
	if vars := d.locals(frame); vars["total"] != "30" {
		t.Errorf("total = %q, want 30 (the squares of 1 to 4)", vars["total"])
	}

	// the second breakpoint: the module's variables, a dictionary among them
	d.call("continue", map[string]any{"threadId": thread})
	if reason, _ := d.stopped(); reason != "breakpoint" {
		t.Errorf("second stop for %q", reason)
	}
	_, _, line, frame = d.top(thread)
	vars = d.locals(frame)
	if line != 14 || vars["result"] != "30" || !strings.Contains(vars["config"], "'name': 'hello'") {
		t.Errorf("module level at %d: %v", line, vars)
	}

	// the end: the program runs out and says so
	d.call("continue", map[string]any{"threadId": thread})
	d.event("terminated")
	d.drain(time.Second)
	if out := d.programOutput(); !strings.Contains(out, "result 30") {
		t.Errorf("the program's output = %q", out)
	}
	d.finish(true)
}

func TestDebugpyRunsInTheTerminalOfTheWindow(t *testing.T) {
	r, timeout := debugpyRig(t, nil)
	r.copyPrograms()
	prog := filepath.Join(r.folder, "hello.py")
	// no console in the configuration: the default of the debugger is the integrated terminal
	d, c := r.session(timeout, extension.DebugConfiguration{"name": "Launch", "type": "debugpy", "request": "launch", "program": prog, "args": []any{"3"}})
	if c["console"] != "integratedTerminal" || c["internalConsoleOptions"] != "neverOpen" {
		t.Fatalf("console = %v, %v", c["console"], c["internalConsoleOptions"])
	}
	terminalOut := &lockedBuilder{}
	d.terminal = func(args map[string]any) int {
		var argv []string
		for _, a := range args["args"].([]any) {
			argv = append(argv, a.(string))
		}
		cmd := exec.Command(timeout, append([]string{"-k", "5", "120"}, argv...)...)
		cmd.Dir, _ = args["cwd"].(string)
		cmd.Env = os.Environ()
		if env, ok := args["env"].(map[string]any); ok {
			for k, v := range env {
				if s, ok := v.(string); ok {
					cmd.Env = append(cmd.Env, k+"="+s)
				}
			}
		}
		cmd.Stdout, cmd.Stderr = terminalOut, terminalOut
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Errorf("the terminal cannot run %v: %v", argv, err)
			return 0
		}
		pgid := cmd.Process.Pid
		t.Cleanup(func() {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			_ = cmd.Wait()
		})
		return cmd.Process.Pid
	}
	if msg := d.start("launch", c, prog, nil, 6); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	reason, thread := d.stopped()
	name, _, line, frame := d.top(thread)
	if reason != "breakpoint" || name != "compute" || line != 6 || d.locals(frame)["n"] != "3" {
		t.Errorf("stopped for %q at %s:%d", reason, name, line)
	}
	if len(d.terminalRequests) != 1 || d.terminalRequests[0]["kind"] != "integrated" || d.terminalRequests[0]["cwd"] != r.folder {
		t.Fatalf("terminal requests = %v", d.terminalRequests)
	}
	if argv := d.terminalRequests[0]["args"].([]any); argv[0] != pinnedPython {
		t.Errorf("the terminal runs %v, want the interpreter of the configuration first", argv)
	}
	d.call("continue", map[string]any{"threadId": thread})
	d.event("terminated")
	d.finish(true)
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(terminalOut.String(), "result 14") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if out := terminalOut.String(); !strings.Contains(out, "result 14") {
		t.Errorf("the output in the terminal = %q (the squares of 1 to 3 add up to 14)", out)
	}
}

func TestDebugpyStopsAtAnExceptionWithItsData(t *testing.T) {
	r, timeout := debugpyRig(t, nil)
	r.copyPrograms()
	prog := filepath.Join(r.folder, "boom.py")
	d, c := r.session(timeout, extension.DebugConfiguration{"name": "Launch", "type": "debugpy", "request": "launch", "program": prog, "console": "internalConsole"})
	if msg := d.start("launch", c, "", []string{"uncaught"}); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	reason, thread := d.stopped()
	if reason != "exception" {
		t.Fatalf("stopped for %q", reason)
	}
	name, _, line, _ := d.top(thread)
	if name != "fail" || line != 3 {
		t.Errorf("top frame = %s:%d, want fail:3", name, line)
	}
	info := d.call("exceptionInfo", map[string]any{"threadId": thread})
	details, _ := info["details"].(map[string]any)
	if info["exceptionId"] != "ValueError" || !strings.Contains(fmt.Sprint(info["description"]), "boom at depth 0") || info["breakMode"] == nil || info["breakMode"] == "" {
		t.Errorf("exception info = %v", info)
	}
	if details["typeName"] != "ValueError" || !strings.Contains(fmt.Sprint(details["stackTrace"]), "boom.py") {
		t.Errorf("details = %v", details)
	}
	d.finish(true)
	if !strings.Contains(d.programOutput(), "starting") {
		t.Errorf("the program's output = %q", d.programOutput())
	}
}

func TestDebugpyJustMyCode(t *testing.T) {
	tests := []struct {
		name       string
		justMyCode bool
		// the frame after a step into json.dumps
		wantName, wantFile string
		wantLine           int
	}{
		{"on: the library is stepped over", true, "<module>", "jmc.py", 5},
		{"off: the step goes into the library", false, "dumps", "__init__.py", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, timeout := debugpyRig(t, nil)
			r.copyPrograms()
			prog := filepath.Join(r.folder, "jmc.py")
			d, c := r.session(timeout, extension.DebugConfiguration{
				"name": "Launch", "type": "debugpy", "request": "launch", "program": prog, "console": "internalConsole", "justMyCode": tt.justMyCode,
			})
			if c["justMyCode"] != tt.justMyCode {
				t.Fatalf("justMyCode = %v", c["justMyCode"])
			}
			if msg := d.start("launch", c, prog, nil, 4); msg != "" {
				t.Fatalf("launch failed: %s", msg)
			}
			_, thread := d.stopped()
			if name, _, line, _ := d.top(thread); name != "<module>" || line != 4 {
				t.Fatalf("stopped at %s:%d", name, line)
			}
			d.call("stepIn", map[string]any{"threadId": thread})
			d.stopped()
			name, file, line, _ := d.top(thread)
			if name != tt.wantName || filepath.Base(file) != tt.wantFile || (tt.wantLine != 0 && line != tt.wantLine) {
				t.Errorf("after the step in: %s %s:%d, want %s %s:%d", name, file, line, tt.wantName, tt.wantFile, tt.wantLine)
			}
			if !tt.justMyCode && !strings.Contains(file, "json") {
				t.Errorf("the library file is %s", file)
			}
			d.finish(true)
		})
	}
}

func TestDebugpyJustMyCodeComesFromTheSetting(t *testing.T) {
	r, _ := debugpyRig(t, map[string]any{DebugJustMyCodeSetting: false})
	got := r.substitute(extension.DebugConfiguration{"request": "launch", "program": "a.py"})
	if got.err != nil || got.cfg["justMyCode"] != false {
		t.Errorf("justMyCode = %v, %v", got.cfg["justMyCode"], got.err)
	}
}

// freePort finds a port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func TestDebugpyAttachesByPort(t *testing.T) {
	r, timeout := debugpyRig(t, nil)
	r.copyPrograms()
	prog := filepath.Join(r.folder, "attach_target.py")
	port := freePort(t)

	// the program listens for a debugger and waits for one, under timeout -k in a process group of its own
	target := exec.Command(timeout, "-k", "5", "200", pinnedPython, "-m", "debugpy", "--listen", "127.0.0.1:"+strconv.Itoa(port), "--wait-for-client", prog)
	target.Dir = r.folder
	targetOut := &lockedBuilder{}
	target.Stdout, target.Stderr = targetOut, targetOut
	target.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := target.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = target.Wait()
	})

	// the configuration of the attach: the provider and the factory turn it into a connection to the port
	got := r.resolve(extension.DebugConfiguration{"name": "Attach", "type": "debugpy", "request": "attach", "connect": map[string]any{"host": "127.0.0.1", "port": float64(port)}})
	if got.err != nil || got.cfg == nil {
		t.Fatalf("resolve = %+v", got)
	}
	sub := r.substitute(got.cfg)
	if sub.err != nil {
		t.Fatal(sub.err)
	}
	s := r.api.NewDebugSession("s1", DebugType, "py", sub.cfg)
	adapter, err := r.api.DebugAdapter(context.Background(), DebugType, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Kind != extension.DebugAdapterServer || adapter.Host != "127.0.0.1" || adapter.Port != port {
		t.Fatalf("adapter = %+v", adapter)
	}
	d := dial(t, adapter.Host, adapter.Port)
	if msg := d.start("attach", sub.cfg, prog, nil, 5); msg != "" {
		t.Fatalf("attach failed: %s (target: %s)", msg, targetOut.String())
	}
	reason, thread := d.stopped()
	name, _, line, frame := d.top(thread)
	if reason != "breakpoint" || name != "work" || line != 5 {
		t.Errorf("stopped for %q at %s:%d (target: %s)", reason, name, line, targetOut.String())
	}
	if vars := d.locals(frame); vars["round_number"] == "" {
		t.Errorf("locals = %v", vars)
	}
	// leaving the program running: the debugger lets it go
	d.finish(false)
	time.Sleep(300 * time.Millisecond)
	if err := syscall.Kill(pgid, 0); err != nil {
		t.Errorf("the target is gone after the debugger left it: %v", err)
	}
}

func TestDebugpyLaunchOfAModuleThatDoesNotExistFails(t *testing.T) {
	r, timeout := debugpyRig(t, nil)
	d, c := r.session(timeout, extension.DebugConfiguration{"name": "Launch", "type": "debugpy", "request": "launch", "module": "no_such_module_for_emetos", "console": "internalConsole"})
	if msg := d.start("launch", c, "", nil); msg != "" {
		// the launch itself failed: also fine, the message says why
		t.Logf("launch failed as it may: %s", msg)
		return
	}
	d.event("terminated")
	d.drain(time.Second)
	if out := d.programOutput(); !strings.Contains(out, "No module named") {
		t.Errorf("the output = %q, want Python's words about the module", out)
	}
	d.finish(true)
}
