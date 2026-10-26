package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/agents/claude/claudetest"
	"github.com/rm4n0s/errors"
)

// waitLimit is how long a test waits for something that should happen at once; the fake ends itself after 90 seconds.
const waitLimit = 15 * time.Second

// rig is an Adapter over the fake program with a script, a log and the events of the sessions it starts.
type rig struct {
	t       *testing.T
	adapter *Adapter
	dir     string
	log     string
	pidfile string

	mu     sync.Mutex
	events []Event
	signal chan struct{}
}

// newRig makes a folder and sets the environment of the fake: the script testdata/<script>, a log, a pid file. The
// adapter has short grace times so that a test of the kill does not wait.
func newRig(t *testing.T, script string) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{
		t: t, dir: dir, log: filepath.Join(dir, "fake.log"), pidfile: filepath.Join(dir, "child.pid"),
		signal: make(chan struct{}, 1),
		adapter: &Adapter{Path: fakeProgram, Dir: dir, closeGrace: 300 * time.Millisecond, killGrace: 300 * time.Millisecond,
			authTimeout: time.Second},
	}
	t.Setenv(claudetest.EnvScript, filepath.Join("testdata", script))
	if abs, err := filepath.Abs(filepath.Join("testdata", script)); err == nil {
		t.Setenv(claudetest.EnvScript, abs)
	}
	t.Setenv(claudetest.EnvLog, r.log)
	t.Setenv(claudetest.EnvPidfile, r.pidfile)
	return r
}

// opts returns options that collect the events into the rig.
func (r *rig) opts() SessionOptions {
	return SessionOptions{OnEvent: r.collect, RequestTimeout: 5 * time.Second, MaxLineBytes: 2048}
}

func (r *rig) collect(ev Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
	select {
	case r.signal <- struct{}{}:
	default:
	}
}

// start starts a session and closes it when the test ends.
func (r *rig) start(ctx context.Context, o SessionOptions) *Session {
	r.t.Helper()
	s, err := r.adapter.Start(ctx, o)
	if err != nil {
		r.t.Fatalf("Start: %v", err)
	}
	r.t.Cleanup(func() {
		_ = s.Close()
		select {
		case <-s.Done():
		case <-time.After(waitLimit):
			r.t.Errorf("the session did not end")
		}
	})
	return s
}

// snapshot is the events so far.
func (r *rig) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

// waitFor waits until an event satisfies pred and returns the first one that does.
func (r *rig) waitFor(desc string, pred func(Event) bool) Event {
	r.t.Helper()
	deadline := time.After(waitLimit)
	seen := 0
	for {
		evs := r.snapshot()
		for ; seen < len(evs); seen++ {
			if pred(evs[seen]) {
				return evs[seen]
			}
		}
		select {
		case <-r.signal:
		case <-deadline:
			r.t.Fatalf("no event: %s (got %d events: %s)", desc, len(evs), describe(evs))
			return nil
		}
	}
}

// waitResult waits for the first ResultMessage.
func (r *rig) waitResult() *ResultMessage {
	r.t.Helper()
	ev := r.waitFor("a result", func(e Event) bool { _, ok := e.(*ResultMessage); return ok })
	return ev.(*ResultMessage)
}

// describe lists the types of events.
func describe(evs []Event) string {
	var parts []string
	for _, e := range evs {
		switch m := e.(type) {
		case *ProtocolErrorEvent:
			parts = append(parts, "protocol-error("+tagOf(m.Err)+")")
		case *ExitEvent:
			parts = append(parts, "exit")
		default:
			parts = append(parts, strings.TrimPrefix(strings.TrimPrefix(typeName(e), "*claude."), "*"))
		}
	}
	return strings.Join(parts, " ")
}

func typeName(v any) string {
	return strings.TrimPrefix(strings.TrimPrefix(sprintType(v), "*"), "claude.")
}

func tagOf(err error) string {
	if e, ok := errors.FromError(err); ok {
		return e.Tag
	}
	return "untagged"
}

// logRecord is one line of the fake's log.
type logRecord struct {
	Kind string            `json:"kind"`
	Argv []string          `json:"argv"`
	PID  int               `json:"pid"`
	PGID int               `json:"pgid"`
	CWD  string            `json:"cwd"`
	Env  map[string]string `json:"env"`
	Msg  json.RawMessage   `json:"msg"`
}

// records reads the log of the fake.
func (r *rig) records() []logRecord {
	r.t.Helper()
	f, err := os.Open(r.log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		r.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []logRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		var rec logRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			r.t.Fatalf("log line %q: %v", sc.Text(), err)
		}
		out = append(out, rec)
	}
	return out
}

// incoming returns the lines the fake read of kind type (and subtype for control requests), as maps.
func (r *rig) incoming(typ string) []map[string]any {
	r.t.Helper()
	var out []map[string]any
	for _, rec := range r.records() {
		if rec.Kind != "in" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Msg, &m); err != nil {
			r.t.Fatal(err)
		}
		if m["type"] == typ {
			out = append(out, m)
		}
	}
	return out
}

// controlRequests returns the bodies of the control requests of a subtype the fake read.
func (r *rig) controlRequests(subtype string) []map[string]any {
	r.t.Helper()
	var out []map[string]any
	for _, m := range r.incoming("control_request") {
		if req, _ := m["request"].(map[string]any); req["subtype"] == subtype {
			out = append(out, req)
		}
	}
	return out
}

// answerTo returns the body of the control_response the fake read for the request ID, polling until it is in the log.
func (r *rig) answerTo(id string) map[string]any {
	r.t.Helper()
	deadline := time.Now().Add(waitLimit)
	for {
		for _, m := range r.incoming("control_response") {
			if resp, _ := m["response"].(map[string]any); resp["request_id"] == id {
				return resp
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("the fake never read an answer to %s", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// wantRoute checks that err is a tagged error whose route has the substring.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = routeOf(t, err, route)
}

// routeOf is wantRoute that also returns the tagged error, for its metadata.
func routeOf(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Fatalf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
	return appErr
}

// processGone reports whether the process is gone: it does not exist, or it is a zombie nobody reaped yet.
func processGone(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	// the state is the first field after the command name, which is in parentheses
	s := string(data)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] == 'Z'
	}
	return false
}

// waitGone waits until the process is gone.
func waitGone(t *testing.T, what string, pid int) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for !processGone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%s (pid %d) is still running", what, pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readPID reads the PID the fake wrote to its pid file, waiting for the file.
func (r *rig) readPID() int {
	r.t.Helper()
	deadline := time.Now().Add(waitLimit)
	for {
		data, err := os.ReadFile(r.pidfile)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatal("the fake never wrote the PID of its child")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// groupOf is the process group of a process, 0 when it is gone.
func groupOf(pid int) int {
	g, err := syscall.Getpgid(pid)
	if err != nil {
		return 0
	}
	return g
}

func sprintType(v any) string { return fmt.Sprintf("%T", v) }
