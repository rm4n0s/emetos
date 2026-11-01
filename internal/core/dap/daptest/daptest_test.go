package daptest

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/errors"
)

func TestMain(m *testing.M) { os.Exit(Main(m)) }

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want it to contain %s", appErr.Route(), route)
	}
}

func TestScriptRoundTrip(t *testing.T) {
	script := Script{
		Reply("initialize", map[string]any{"supportsConfigurationDoneRequest": true}),
		ReplyArgs("launch", map[string]any{"program": "/p"}, nil),
		ReplyError("evaluate", "bad", map[string]any{"error": map[string]any{"id": 1}}),
		Collect("a", "b"), Respond("b", 1), RespondError("a", "no", nil),
		Event("stopped", map[string]any{"reason": "step"}), Request("rt", "runInTerminal", map[string]any{"cwd": "/"}), Await("rt"),
		Delay(5), Burst(3, 10), Send(map[string]any{"type": "event"}), SendText("{"), Raw("x\r\n"), Stderr("hi"),
		Crash(2), Exit(1), Stall(), WaitClose(), Expect("threads", nil), Reply("threads", nil).After(7),
	}
	data, err := script.Lines()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != len(script) {
		t.Errorf("%d lines for %d steps", n, len(script))
	}
	back, err := ParseScript(append([]byte("# a comment\n\n"), data...))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, script) {
		t.Errorf("the script changed in the round trip:\n%+v\n%+v", back, script)
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadScript(path)
	if err != nil || len(loaded) != len(script) {
		t.Errorf("LoadScript = %d steps, %v", len(loaded), err)
	}
}

func TestScriptFailures(t *testing.T) {
	_, err := ParseScript([]byte(`{"op":"event"}` + "\n" + `{not json}` + "\n"))
	wantRoute(t, err, "ParseScript.ScriptInvalid")
	if appErr, _ := errors.FromError(err); appErr.Metadata["line"] != 2 {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	_, err = ParseScript([]byte(`{"command":"x"}`))
	wantRoute(t, err, "ParseScript.ScriptInvalid")
	_, err = ParseScript([]byte(strings.Repeat("x", 70<<20)))
	wantRoute(t, err, "ParseScript.ScriptInvalid")
	_, err = LoadScript(filepath.Join(t.TempDir(), "missing"))
	wantRoute(t, err, "LoadScript.ScriptReadFailed")
	_, err = Script{{Op: OpEvent, Body: json.RawMessage("{bad")}}.Lines()
	wantRoute(t, err, "Script.Lines.ScriptInvalid")
	defer func() {
		if recover() == nil {
			t.Error("JSON of a channel did not panic")
		}
	}()
	JSON(make(chan int))
}

func TestSubset(t *testing.T) {
	tests := []struct {
		name      string
		want, got string
		ok        bool
	}{
		{"equal scalars", `1`, `1`, true},
		{"different scalars", `1`, `2`, false},
		{"a member", `{"a":1}`, `{"a":1,"b":2}`, true},
		{"a missing member", `{"c":1}`, `{"a":1,"b":2}`, false},
		{"nested", `{"a":{"b":[1,2]}}`, `{"a":{"b":[1,2],"c":3},"d":4}`, true},
		{"an array is compared whole", `{"a":[1]}`, `{"a":[1,2]}`, false},
		{"an object where a scalar is", `{"a":{"b":1}}`, `{"a":3}`, false},
		{"got is no object", `{"a":1}`, `[1]`, false},
		{"want is not JSON", `{`, `{}`, false},
		{"got is not JSON", `{}`, `{`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(json.RawMessage(tt.want), json.RawMessage(tt.got)); got != tt.ok {
				t.Errorf("contains = %v, want %v", got, tt.ok)
			}
		})
	}
}

// peer is a hand-made client for the fake adapter, to send what the real client never would.
type peer struct {
	t   *testing.T
	rw  io.ReadWriteCloser
	br  *bufio.Reader
	seq int
}

func newPeer(t *testing.T, script Script, opts ServerOptions) (*peer, *Server, <-chan struct{}) {
	t.Helper()
	c, s := Pipe()
	srv := NewServer(script, opts)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(s); err != nil {
			t.Errorf("Serve: %v", err)
		}
	}()
	p := &peer{t: t, rw: c, br: bufio.NewReader(c)}
	t.Cleanup(func() {
		_ = c.Close()
		select {
		case <-done:
		case <-time.After(Timeout):
			t.Error("the fake adapter did not stop")
		}
	})
	return p, srv, done
}

func (p *peer) sendSeq(seq int, command string, args any) {
	p.t.Helper()
	msg := map[string]any{"seq": seq, "type": "request", "command": command}
	if args != nil {
		msg["arguments"] = args
	}
	body, err := json.Marshal(msg)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := dap.WriteFrame(p.rw, body); err != nil {
		p.t.Fatal(err)
	}
}

func (p *peer) request(command string, args any) {
	p.t.Helper()
	p.seq++
	p.sendSeq(p.seq, command, args)
}

func (p *peer) read() map[string]any {
	p.t.Helper()
	body, err := dap.ReadFrame(p.br, 0)
	if err != nil {
		p.t.Fatalf("reading the adapter: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		p.t.Fatal(err)
	}
	return m
}

func hasViolation(srv *Server, substr string) bool {
	for _, v := range srv.Violations() {
		if strings.Contains(v, substr) {
			return true
		}
	}
	return false
}

func TestServerNotesWhatIsWrong(t *testing.T) {
	t.Run("a sequence number that does not go up by one", func(t *testing.T) {
		p, srv, _ := newPeer(t, Script{Reply("a", nil)}, ServerOptions{})
		p.sendSeq(1, "a", nil)
		p.read()
		p.sendSeq(5, "$daptest/violations", nil)
		p.read()
		if !hasViolation(srv, "numbered a message 5 after 1") {
			t.Errorf("violations = %q", srv.Violations())
		}
	})
	t.Run("a request that is not the expected one", func(t *testing.T) {
		p, srv, _ := newPeer(t, Script{Reply("a", nil)}, ServerOptions{})
		p.request("b", nil)
		if m := p.read(); m["success"] != false || !strings.Contains(m["message"].(string), "unexpected request b") {
			t.Errorf("answer = %v", m)
		}
		p.request("a", nil)
		if m := p.read(); m["success"] != true {
			t.Errorf("answer = %v", m)
		}
		if !hasViolation(srv, `expected request "a", got "b"`) || len(srv.Violations()) != 1 {
			t.Errorf("violations = %q", srv.Violations())
		}
	})
	t.Run("arguments that do not contain the expected ones", func(t *testing.T) {
		p, srv, _ := newPeer(t, Script{ReplyArgs("a", map[string]any{"x": 1}, nil)}, ServerOptions{})
		p.request("a", map[string]any{"x": 2})
		p.read()
		if !hasViolation(srv, `the arguments of "a" are {"x":2}`) {
			t.Errorf("violations = %q", srv.Violations())
		}
	})
	t.Run("a request after the end of the script", func(t *testing.T) {
		p, srv, _ := newPeer(t, Script{}, ServerOptions{})
		p.request("late", nil)
		if m := p.read(); m["success"] != false {
			t.Errorf("answer = %v", m)
		}
		if !hasViolation(srv, `request "late" after the end of the script`) {
			t.Errorf("violations = %q", srv.Violations())
		}
	})
	t.Run("an unknown step, and labels nobody remembered", func(t *testing.T) {
		p, srv, _ := newPeer(t, Script{{Op: "dance"}, Respond("nobody", nil), Await("nobody"), Reply("a", nil)}, ServerOptions{})
		p.request("a", nil)
		p.read()
		for _, want := range []string{`unknown op "dance"`, `no request was remembered as "nobody"`, `no request was sent as "nobody"`} {
			if !hasViolation(srv, want) {
				t.Errorf("no violation %q in %q", want, srv.Violations())
			}
		}
	})
	t.Run("a request while collecting that is not collected", func(t *testing.T) {
		p, srv, _ := newPeer(t, Script{Collect("a")}, ServerOptions{})
		p.request("z", nil)
		p.read()
		if !hasViolation(srv, `got request "z" while it collects`) {
			t.Errorf("violations = %q", srv.Violations())
		}
	})
	t.Run("a message that is not JSON", func(t *testing.T) {
		p, srv, _ := newPeer(t, Script{Stall()}, ServerOptions{})
		if err := dap.WriteFrame(p.rw, []byte("{nope")); err != nil {
			t.Fatal(err)
		}
		p.request("$daptest/violations", nil)
		p.read()
		if !hasViolation(srv, "not JSON") {
			t.Errorf("violations = %q", srv.Violations())
		}
	})
}

func TestServerAnswersTheTestRequestsEvenWhenStalled(t *testing.T) {
	p, srv, _ := newPeer(t, Script{Stall()}, ServerOptions{})
	p.request("anything", nil) // never answered
	p.request(MethodTraffic, nil)
	m := p.read()
	var traffic []Message
	if err := json.Unmarshal(mustRaw(t, m["body"]), &traffic); err != nil || len(traffic) < 2 || traffic[0].Name != "anything" {
		t.Errorf("traffic = %+v, %v", traffic, err)
	}
	p.request(MethodViolations, nil)
	if m := p.read(); string(mustRaw(t, m["body"])) != "[]" {
		t.Errorf("violations = %v", m["body"])
	}
	p.request(MethodSent, nil)
	if m := p.read(); string(mustRaw(t, m["body"])) != "0" {
		t.Errorf("sent = %v", m["body"])
	}
	p.request("$daptest/nothing", nil)
	if m := p.read(); m["success"] != false {
		t.Errorf("answer = %v", m)
	}
	if got := srv.Requests("anything"); len(got) != 1 {
		t.Errorf("Requests = %v", got)
	}
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestServerCrashAndExit(t *testing.T) {
	t.Run("a crash closes the stream", func(t *testing.T) {
		p, srv, done := newPeer(t, Script{Expect("a", nil), Crash(7)}, ServerOptions{})
		p.request("a", nil)
		if _, err := dap.ReadFrame(p.br, 0); !errors.Is(err, io.EOF) {
			t.Errorf("after the crash: %v, want the end of the stream", err)
		}
		<-done
		if srv.ExitCode() != 7 {
			t.Errorf("exit code = %d", srv.ExitCode())
		}
	})
	t.Run("a crash calls the function of the program", func(t *testing.T) {
		var mu sync.Mutex
		var code = -1
		p, _, done := newPeer(t, Script{Crash(9)}, ServerOptions{Crash: func(c int) {
			mu.Lock()
			code = c
			mu.Unlock()
		}})
		_ = p
		<-done
		mu.Lock()
		defer mu.Unlock()
		if code != 9 {
			t.Errorf("crash called with %d", code)
		}
	})
	t.Run("an exit ends the script", func(t *testing.T) {
		p, srv, done := newPeer(t, Script{Exit(4), Reply("never", nil)}, ServerOptions{})
		_ = p
		<-done
		if srv.ExitCode() != 4 {
			t.Errorf("exit code = %d", srv.ExitCode())
		}
	})
}

func TestServerStderrBurstAndRawSteps(t *testing.T) {
	var out strings.Builder
	p, srv, _ := newPeer(t, Script{Stderr("to the log"), Burst(5, 20), Raw("garbage"), SendText("{x")}, ServerOptions{Stderr: &out})
	for n := range 5 {
		m := p.read()
		if m["type"] != "event" || m["event"] != "output" || m["seq"] != float64(n+1) {
			t.Fatalf("event %d = %v", n, m)
		}
	}
	for deadline := time.Now().Add(Timeout); srv.Sent() != 5 && time.Now().Before(deadline); { // counted just after the write
		time.Sleep(time.Millisecond)
	}
	if srv.Sent() != 5 {
		t.Errorf("Sent = %d", srv.Sent())
	}
	for _, m := range srv.Traffic() {
		if m.Direction == "out" {
			t.Errorf("a burst event is in the traffic: %+v", m)
		}
	}
	buf := make([]byte, len("garbage"))
	if _, err := io.ReadFull(p.br, buf); err != nil || string(buf) != "garbage" {
		t.Errorf("raw = %q, %v", buf, err)
	}
	if body, err := dap.ReadFrame(p.br, 0); err != nil || string(body) != "{x" {
		t.Errorf("sent text = %q, %v", body, err)
	}
	if out.String() != "to the log\n" {
		t.Errorf("stderr = %q", out.String())
	}
}

// failingStream accepts reads that never come and fails every write.
type failingStream struct {
	closed chan struct{}
	once   sync.Once
	err    error
}

func (f *failingStream) Read([]byte) (int, error) {
	<-f.closed
	return 0, io.EOF
}
func (f *failingStream) Write([]byte) (int, error) { return 0, f.err }
func (f *failingStream) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

func TestServeFailsWhenItCannotWrite(t *testing.T) {
	boom := errors.New("Boom", "the pipe broke")
	tests := []struct {
		name   string
		script Script
		route  string
	}{
		{"raw bytes", Script{Raw("x")}, "Server.writeRaw.ServeWriteFailed"},
		{"an event", Script{Event("e", nil)}, "WriteFrame.DapFrameWriteFailed"},
		{"an event that cannot be encoded", Script{{Op: OpEvent, Event: "e", Body: json.RawMessage("{bad")}}, "Server.send.ServeWriteFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(tt.script, ServerOptions{})
			err := srv.Serve(&failingStream{closed: make(chan struct{}), err: boom})
			wantRoute(t, err, tt.route)
		})
	}
	t.Run("a closed stream is the end and not a failure", func(t *testing.T) {
		for _, closed := range []error{io.ErrClosedPipe, os.ErrClosed} {
			srv := NewServer(Script{Event("e", nil), Raw("x")}, ServerOptions{})
			if err := srv.Serve(&failingStream{closed: make(chan struct{}), err: closed}); err != nil {
				t.Errorf("%v: %v", closed, err)
			}
		}
	})
}

func TestHarnessProgramBuiltWithoutTestMain(t *testing.T) {
	t.Setenv(ProgramEnv, "")
	prog := Program(t)
	if info, err := os.Stat(prog); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("program %s: %v", prog, err)
	}
}

func TestHarnessInProcessHasNoProgram(t *testing.T) {
	h := Start(t, Options{Script: Script{Reply("threads", nil)}})
	if h.PID() != 0 || h.Exited() != nil || len(h.Stderr()) != 0 {
		t.Errorf("pid %d exited %v stderr %v", h.PID(), h.Exited(), h.Stderr())
	}
	if _, err := h.Client.Request(t.Context(), dap.CmdThreads, nil); err != nil {
		t.Fatal(err)
	}
	if got := h.Requests("threads"); len(got) != 1 {
		t.Errorf("Requests = %v", got)
	}
}

func TestHarnessProgramReportsItsStderrAndExit(t *testing.T) {
	h := Start(t, Options{Transport: Stdio, Script: Script{Stderr("hello from the adapter"), Expect("threads", nil), Exit(5)}})
	if _, err := h.Client.Request(t.Context(), dap.CmdThreads, nil); err == nil {
		t.Error("the adapter exited without answering, yet the request worked")
	}
	if code := h.WaitExit(); code != 5 {
		t.Errorf("exit status = %d, want 5", code)
	}
	found := false
	for _, l := range h.Stderr() {
		found = found || l == "hello from the adapter"
	}
	if !found {
		t.Errorf("stderr = %q", h.Stderr())
	}
	select {
	case <-h.Exited():
	default:
		t.Error("Exited is not closed")
	}
}
