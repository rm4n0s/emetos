// Package dapprobe is a small DAP client for the tests that run a real debug adapter (gdb, lldb-dap; the tags emetos_gdb and
// emetos_lldbdap, D43). An extension may not import the debug core (internal/archtest), so the tests of an extension speak the protocol
// with this client. It starts the adapter a descriptor describes, under `timeout -k` and in a process group of its own, with a home
// folder and XDG folders of the test (the init files of the user, ~/.gdbinit and ~/.lldbinit, are never read), and ends the adapter and
// the program it debugs when the test ends.
package dapprobe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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

// Wait is how long the client waits for a response or an event.
const Wait = 90 * time.Second

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

// Client is a connection to an adapter: it sends requests and reads the messages in the order they come.
type Client struct {
	t         *testing.T
	w         io.Writer
	msgs      chan map[string]any
	seq       int
	events    []map[string]any
	responses map[int]map[string]any
	output    strings.Builder
	stderr    *lockedBuilder
	adapterID string
	// Capabilities are the capabilities of the adapter, from the response to initialize.
	Capabilities map[string]any
}

// Start starts the adapter as the host does (a process group of its own, the working folder and environment of the descriptor) under
// `timeout -k`, in a home folder of the test, and returns the client that speaks DAP to it. The test fails when timeout is not there.
func Start(t *testing.T, a *extension.DebugAdapter, adapterID string) *Client {
	t.Helper()
	if a.Kind != extension.DebugAdapterExecutable {
		t.Fatalf("adapter = %+v, want an executable", a)
	}
	timeout, err := exec.LookPath("timeout")
	if err != nil {
		t.Skip("timeout is not installed")
	}
	cmd := exec.Command(timeout, append([]string{"-k", "5", "300", a.Command}, a.Args...)...)
	cmd.Dir = a.Cwd
	home := t.TempDir()
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"), "DEBUGINFOD_URLS=")
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
	c := &Client{t: t, w: stdin, msgs: make(chan map[string]any, 256), responses: map[int]map[string]any{}, stderr: stderr, adapterID: adapterID}
	go c.read(stdout)
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	t.Cleanup(c.terminate) // runs before the kill above
	return c
}

// terminate asks the adapter to end the program it debugs and waits a few seconds for the answer. It runs when a test ends, whatever
// state the test left the session in.
func (c *Client) terminate() {
	c.seq++
	data, err := json.Marshal(map[string]any{"seq": c.seq, "type": "request", "command": "disconnect", "arguments": map[string]any{"terminateDebuggee": true}})
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
		return
	}
	end := time.After(8 * time.Second)
	for {
		select {
		case m, ok := <-c.msgs:
			if !ok {
				return
			}
			if seq, _ := m["request_seq"].(float64); m["type"] == "response" && int(seq) == c.seq {
				return
			}
		case <-end:
			return
		}
	}
}

// read reads the messages of the adapter into the channel.
func (c *Client) read(rd io.Reader) {
	defer close(c.msgs)
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
		c.msgs <- m
	}
}

// next reads the next message with a deadline.
func (c *Client) next(within time.Duration) (map[string]any, bool) {
	select {
	case m, ok := <-c.msgs:
		return m, ok
	case <-time.After(within):
		return nil, false
	}
}

// write sends a message.
func (c *Client) write(msg map[string]any) {
	c.t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
		c.t.Fatal(err)
	}
}

// note keeps what a message says for the test to read; a request of the adapter is refused.
func (c *Client) note(m map[string]any) {
	switch m["type"] {
	case "response":
		if seq, ok := m["request_seq"].(float64); ok {
			c.responses[int(seq)] = m
		}
		return
	case "request":
		seq, _ := m["seq"].(float64)
		c.write(map[string]any{"seq": 0, "type": "response", "request_seq": int(seq), "command": m["command"], "success": false, "message": "unsupported"})
		return
	}
	if m["event"] == "output" {
		if body, ok := m["body"].(map[string]any); ok {
			if s, ok := body["output"].(string); ok {
				c.output.WriteString(s)
			}
		}
	}
	c.events = append(c.events, m)
}

// Send writes a request and returns its number.
func (c *Client) Send(command string, args any) int {
	c.t.Helper()
	c.seq++
	msg := map[string]any{"seq": c.seq, "type": "request", "command": command}
	if args != nil {
		msg["arguments"] = args
	}
	c.write(msg)
	return c.seq
}

// Response waits for the response to the request; events that come meanwhile are kept. It returns the body, and the message of the
// adapter when the request failed.
func (c *Client) Response(seq int, command string) (map[string]any, string) {
	c.t.Helper()
	for {
		if m, ok := c.responses[seq]; ok {
			delete(c.responses, seq)
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
				if msg == "" {
					msg = "failed"
				}
				return body, msg
			}
			return body, ""
		}
		m, ok := c.next(Wait)
		if !ok {
			c.t.Fatalf("no response to %s (output: %q, stderr: %q)", command, c.output.String(), c.stderr.String())
		}
		c.note(m)
	}
}

// Call sends a request and requires it to succeed.
func (c *Client) Call(command string, args any) map[string]any {
	c.t.Helper()
	body, msg := c.Response(c.Send(command, args), command)
	if msg != "" {
		c.t.Fatalf("%s failed: %s (output: %q, stderr: %q)", command, msg, c.output.String(), c.stderr.String())
	}
	return body
}

// Event waits for an event with the name, and returns its body.
func (c *Client) Event(name string) map[string]any {
	c.t.Helper()
	for {
		for i, m := range c.events {
			if m["event"] == name {
				c.events = append(c.events[:i], c.events[i+1:]...)
				body, _ := m["body"].(map[string]any)
				return body
			}
		}
		m, ok := c.next(Wait)
		if !ok {
			c.t.Fatalf("no %s event (output: %q, stderr: %q)", name, c.output.String(), c.stderr.String())
		}
		c.note(m)
	}
}

// Drain reads what the adapter sends for the time given, so that the output of a program is there to read.
func (c *Client) Drain(within time.Duration) {
	end := time.Now().Add(within)
	for time.Until(end) > 0 {
		m, ok := c.next(time.Until(end))
		if !ok {
			return
		}
		c.note(m)
	}
}

// Output is the text of the output events seen so far.
func (c *Client) Output() string { return c.output.String() }

// Stderr is what the adapter wrote to its standard error.
func (c *Client) Stderr() string { return c.stderr.String() }

// Launch runs initialize and the request ("launch" or "attach") with the configuration, then the breakpoints of the file (lines), and
// configurationDone. It returns the message of the adapter when the request failed, "" when it succeeded.
func (c *Client) Launch(request string, cfg extension.DebugConfiguration, file string, lines ...int) (failed string) {
	c.t.Helper()
	c.Capabilities = c.Call("initialize", map[string]any{"clientID": "emetos", "adapterID": c.adapterID, "linesStartAt1": true, "columnsStartAt1": true,
		"pathFormat": "path", "supportsVariableType": true})
	seq := c.Send(request, cfg)
	// the adapters answer launch and attach when configurationDone came, or at once; initialized follows either way
	c.Event("initialized")
	if file != "" {
		c.setBreakpoints(file, false, lines...)
	}
	c.Call("setExceptionBreakpoints", map[string]any{"filters": []string{}})
	c.Call("configurationDone", nil)
	if _, msg := c.Response(seq, request); msg != "" {
		return msg
	}
	return ""
}

// SetBreakpoints replaces the breakpoints of the file with these lines and requires each to be verified.
func (c *Client) SetBreakpoints(file string, lines ...int) {
	c.t.Helper()
	c.setBreakpoints(file, true, lines...)
}

// setBreakpoints sends the breakpoints of the file; with strict each must be verified (before the program is loaded an adapter may
// answer that a breakpoint is pending: gdb loads the program when configurationDone comes).
func (c *Client) setBreakpoints(file string, strict bool, lines ...int) {
	c.t.Helper()
	bps := make([]map[string]any, len(lines))
	for i, l := range lines {
		bps[i] = map[string]any{"line": l}
	}
	body := c.Call("setBreakpoints", map[string]any{"source": map[string]any{"path": file}, "breakpoints": bps})
	list, _ := body["breakpoints"].([]any)
	if len(list) != len(lines) {
		c.t.Fatalf("setBreakpoints answered %d breakpoints for %d lines: %v", len(list), len(lines), body)
	}
	for i, b := range list {
		if strict && b.(map[string]any)["verified"] != true {
			c.t.Fatalf("the breakpoint of line %d is not verified: %v (output: %q, stderr: %q)", lines[i], b, c.output.String(), c.stderr.String())
		}
	}
}

// Stopped waits for a stopped event and returns its reason and thread.
func (c *Client) Stopped() (reason string, thread int) {
	c.t.Helper()
	body := c.Event("stopped")
	reason, _ = body["reason"].(string)
	id, _ := body["threadId"].(float64)
	return reason, int(id)
}

// Top returns the name, file and line of the top frame of the thread and its id.
func (c *Client) Top(thread int) (name, file string, line, frame int) {
	c.t.Helper()
	body := c.Call("stackTrace", map[string]any{"threadId": thread, "startFrame": 0, "levels": 5})
	frames, _ := body["stackFrames"].([]any)
	if len(frames) == 0 {
		c.t.Fatal("the stack is empty")
	}
	f := frames[0].(map[string]any)
	if src, ok := f["source"].(map[string]any); ok {
		file, _ = src["path"].(string)
	}
	return f["name"].(string), file, int(f["line"].(float64)), int(f["id"].(float64))
}

// Locals returns the variables of the Locals scope of the frame, and of the Arguments scope when the adapter has one (gdb), as name to
// value.
func (c *Client) Locals(frame int) map[string]string {
	c.t.Helper()
	scopes, _ := c.Call("scopes", map[string]any{"frameId": frame})["scopes"].([]any)
	out := map[string]string{}
	for _, s := range scopes {
		sc := s.(map[string]any)
		if name, _ := sc["name"].(string); !strings.HasPrefix(name, "Local") && name != "Arguments" {
			continue
		}
		vars, _ := c.Call("variables", map[string]any{"variablesReference": sc["variablesReference"]})["variables"].([]any)
		for _, v := range vars {
			vm := v.(map[string]any)
			out[vm["name"].(string)], _ = vm["value"].(string)
		}
	}
	return out
}

// Step sends a step request ("next", "stepIn" or "stepOut") and waits for the stop it ends with.
func (c *Client) Step(command string, thread int) (reason string) {
	c.t.Helper()
	c.Call(command, map[string]any{"threadId": thread})
	reason, _ = c.Stopped()
	return reason
}

// Finish disconnects, ending the program when terminate is true.
func (c *Client) Finish(terminate bool) {
	c.t.Helper()
	_, msg := c.Response(c.Send("disconnect", map[string]any{"terminateDebuggee": terminate}), "disconnect")
	if msg != "" {
		c.t.Fatalf("disconnect failed: %s", msg)
	}
}
