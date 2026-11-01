package daptest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/errors"
)

// The requests the fake adapter answers by itself, whatever the script does (even after OpStall): they are how a test asks a
// program what it saw.
const (
	// MethodTraffic answers the messages the adapter has read and written, as []Message.
	MethodTraffic = "$daptest/traffic"
	// MethodViolations answers the violations noticed so far, as []string.
	MethodViolations = "$daptest/violations"
	// MethodSent answers how many events OpBurst has written so far, as a number.
	MethodSent = "$daptest/sent"
)

// Message is a message the fake adapter read or wrote.
type Message struct {
	// Direction is "in" for a message of the client and "out" for the adapter's.
	Direction string `json:"direction"`
	// Type is "request", "response" or "event".
	Type string `json:"type"`
	// Name is the command of a request or response, or the name of an event.
	Name string `json:"name"`
	// Seq is the sequence number of the message.
	Seq int `json:"seq"`
	// RequestSeq is, for a response, the number of its request.
	RequestSeq int `json:"requestSeq,omitempty"`
	// Success is, for a response, its success member.
	Success bool `json:"success,omitempty"`
	// Body is the arguments of a request, the body of a response or an event.
	Body json.RawMessage `json:"body,omitempty"`
}

// ServerOptions are the settings of a Server.
type ServerOptions struct {
	// Crash is called by OpCrash with the exit status; the fake adapter program exits with it. Nil closes the stream and ends
	// the script, which is what the death of a process looks like to the client.
	Crash func(code int)
	// Stderr is where OpStderr writes; nil discards it.
	Stderr io.Writer
}

// Server is the fake debug adapter. It follows its script over any stream.
type Server struct {
	script Script
	opts   ServerOptions

	wmu sync.Mutex // the writer and the numbers: a message is numbered and written under it
	w   io.Writer
	seq int

	rec     sync.Mutex // what is recorded
	traffic []Message
	viol    []string

	imu     sync.Mutex // the messages read and not yet used
	icond   *sync.Cond
	inbox   []incoming
	eof     bool
	aborted bool
	clients int // the last sequence number of the client

	remembered map[string]incoming // requests of the client by label
	lastLabel  string
	sentReq    map[string]int // the numbers of the requests this adapter sent, by label
	burstSent  atomic.Int64
	exitCode   int
}

// incoming is a message read from the client.
type incoming struct {
	Type       string          `json:"type"`
	Seq        int             `json:"seq"`
	Command    string          `json:"command"`
	Arguments  json.RawMessage `json:"arguments"`
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
	Body       json.RawMessage `json:"body"`
}

// NewServer makes the fake adapter for a script.
func NewServer(script Script, opts ServerOptions) *Server {
	s := &Server{script: script, opts: opts, remembered: map[string]incoming{}, sentReq: map[string]int{}}
	s.icond = sync.NewCond(&s.imu)
	return s
}

// Traffic returns the messages the adapter has read and written. The events of OpBurst are not in it (see Sent).
func (s *Server) Traffic() []Message {
	s.rec.Lock()
	defer s.rec.Unlock()
	return append(make([]Message, 0, len(s.traffic)), s.traffic...)
}

// Violations returns what the adapter noticed that is wrong: a sequence number that did not go up by one, a request that was not
// the expected one, a request after the end of the script.
func (s *Server) Violations() []string {
	s.rec.Lock()
	defer s.rec.Unlock()
	return append(make([]string, 0, len(s.viol)), s.viol...)
}

// Sent is how many events OpBurst has written.
func (s *Server) Sent() int { return int(s.burstSent.Load()) }

// Requests returns the arguments of the requests of the client for command, in the order they came.
func (s *Server) Requests(command string) []json.RawMessage {
	var out []json.RawMessage
	for _, m := range s.Traffic() {
		if m.Direction == "in" && m.Type == dap.TypeRequest && m.Name == command {
			out = append(out, m.Body)
		}
	}
	return out
}

// ExitCode is the status OpExit or OpCrash named; zero when neither ran.
func (s *Server) ExitCode() int {
	s.rec.Lock()
	defer s.rec.Unlock()
	return s.exitCode
}

func (s *Server) violate(format string, a ...any) {
	s.rec.Lock()
	s.viol = append(s.viol, fmt.Sprintf(format, a...))
	s.rec.Unlock()
}

// Serve follows the script over rwc and returns when it is done and the client has closed the stream (or an exit or crash step
// ended it). It closes rwc. It fails with the tags ServeWriteFailed (a write that failed while the client still read; a closed
// stream is the end, not a failure).
func (s *Server) Serve(rwc io.ReadWriteCloser) error {
	s.w = rwc
	go s.readLoop(rwc)
	err := s.run(rwc)
	if cerr := rwc.Close(); cerr != nil && err == nil && !errors.Is(cerr, io.ErrClosedPipe) && !errors.Is(cerr, os.ErrClosed) {
		s.violate("closing the stream: %v", cerr)
	}
	return err
}

// readLoop reads the messages of the client into the inbox, answers the requests of the test, and checks the numbers.
func (s *Server) readLoop(r io.Reader) {
	br := bufio.NewReaderSize(r, 64<<10)
	defer func() {
		s.imu.Lock()
		s.eof = true
		s.icond.Broadcast()
		s.imu.Unlock()
	}()
	for {
		body, err := dap.ReadFrame(br, 0)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, os.ErrClosed) {
				s.violate("reading the client: %v", err)
			}
			return
		}
		var in incoming
		if err := json.Unmarshal(body, &in); err != nil {
			s.violate("the client sent a message that is not JSON: %v", err)
			continue
		}
		if in.Seq != s.clients+1 {
			s.violate("the client numbered a message %d after %d", in.Seq, s.clients)
		}
		s.clients = in.Seq
		name := in.Command
		bodyOrArgs := in.Arguments
		if in.Type == dap.TypeResponse {
			bodyOrArgs = in.Body
		}
		s.record(Message{Direction: "in", Type: in.Type, Name: name, Seq: in.Seq, RequestSeq: in.RequestSeq, Success: in.Success, Body: bodyOrArgs})
		if in.Type == dap.TypeRequest && strings.HasPrefix(in.Command, "$daptest/") {
			s.answerTest(in)
			continue
		}
		s.imu.Lock()
		s.inbox = append(s.inbox, in)
		s.icond.Broadcast()
		s.imu.Unlock()
	}
}

// answerTest answers a request about the adapter itself.
func (s *Server) answerTest(in incoming) {
	var result any
	switch in.Command {
	case MethodTraffic:
		result = s.Traffic()
	case MethodViolations:
		result = s.Violations()
	case MethodSent:
		result = s.Sent()
	default:
		s.respond(in, nil, "unknown request "+in.Command)
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		s.respond(in, nil, "cannot encode: "+err.Error())
		return
	}
	s.respond(in, raw, "")
}

// take waits for a message of the inbox that pred accepts and removes it; it reports false when the stream ended or the script
// was aborted first.
func (s *Server) take(pred func(incoming) bool) (incoming, bool) {
	s.imu.Lock()
	defer s.imu.Unlock()
	for {
		for i, in := range s.inbox {
			if pred(in) {
				s.inbox = append(s.inbox[:i], s.inbox[i+1:]...)
				return in, true
			}
		}
		if s.eof || s.aborted {
			return incoming{}, false
		}
		s.icond.Wait()
	}
}

func (s *Server) abort() {
	s.imu.Lock()
	s.aborted = true
	s.icond.Broadcast()
	s.imu.Unlock()
}

func isRequest(in incoming) bool { return in.Type == dap.TypeRequest }

// run executes the steps, then answers with errors until the stream ends.
func (s *Server) run(rwc io.Closer) error {
	for i, st := range s.script {
		stop, err := s.exec(i, st, rwc)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	for {
		in, ok := s.take(isRequest)
		if !ok {
			return nil
		}
		s.violate("request %q after the end of the script", in.Command)
		s.respond(in, nil, "unexpected request "+in.Command)
	}
}

// exec runs one step; stop says the script is over.
func (s *Server) exec(i int, st Step, rwc io.Closer) (stop bool, err error) {
	switch st.Op {
	case OpExpect:
		_, ok := s.expect(i, st.Command, st.Args, labelOf(st.As, st.Command))
		return !ok, nil
	case OpReply:
		in, ok := s.expect(i, st.Command, st.Args, labelOf(st.As, st.Command))
		if !ok {
			return true, nil
		}
		s.pause(st.Ms)
		s.answer(in, st)
	case OpRespond:
		label := st.To
		if label == "" {
			label = s.lastLabel
		}
		in, ok := s.remembered[label]
		if !ok {
			s.violate("step %d: no request was remembered as %q", i, label)
			return false, nil
		}
		s.pause(st.Ms)
		s.answer(in, st)
	case OpCollect:
		want := map[string]bool{}
		for _, c := range st.Commands {
			want[c] = true
		}
		for len(want) > 0 {
			in, ok := s.take(isRequest)
			if !ok {
				return true, nil
			}
			if !want[in.Command] {
				s.violate("step %d: got request %q while it collects %v", i, in.Command, st.Commands)
				s.respond(in, nil, "unexpected request "+in.Command)
				continue
			}
			delete(want, in.Command)
			s.remembered[in.Command] = in
			s.lastLabel = in.Command
		}
	case OpEvent:
		return false, s.sendEvent(st.Event, st.Body)
	case OpRequest:
		label := labelOf(st.As, st.Command)
		var seq int
		seq, err = s.send(func(seq int) any {
			return struct {
				Seq       int             `json:"seq"`
				Type      string          `json:"type"`
				Command   string          `json:"command"`
				Arguments json.RawMessage `json:"arguments,omitempty"`
			}{seq, dap.TypeRequest, st.Command, st.Arguments}
		}, Message{Type: dap.TypeRequest, Name: st.Command, Body: st.Arguments})
		s.sentReq[label] = seq
		return false, err
	case OpAwait:
		seq, known := s.sentReq[st.As]
		if !known {
			s.violate("step %d: no request was sent as %q", i, st.As)
			return false, nil
		}
		_, ok := s.take(func(in incoming) bool { return in.Type == dap.TypeResponse && in.RequestSeq == seq })
		return !ok, nil
	case OpDelay:
		s.pause(st.Ms)
	case OpBurst:
		return false, s.burst(st)
	case OpSend:
		body := []byte(st.Text)
		if st.Text == "" {
			body = st.Body
		}
		return false, s.writeRaw(frame(body))
	case OpRaw:
		return false, s.writeRaw([]byte(st.Text))
	case OpStderr:
		if s.opts.Stderr != nil {
			if _, werr := io.WriteString(s.opts.Stderr, st.Text+"\n"); werr != nil {
				s.violate("writing to stderr: %v", werr)
			}
		}
	case OpCrash:
		s.setExit(st.Code)
		s.abort()
		if s.opts.Crash != nil {
			s.opts.Crash(st.Code)
		} else if cerr := rwc.Close(); cerr != nil {
			s.violate("closing the stream to crash: %v", cerr)
		}
		return true, nil
	case OpExit:
		s.setExit(st.Code)
		s.abort()
		return true, nil
	case OpStall:
		for {
			if _, ok := s.take(func(incoming) bool { return false }); !ok {
				return true, nil
			}
		}
	case OpWaitClose:
		for {
			if _, ok := s.take(func(incoming) bool { return false }); !ok {
				return false, nil
			}
		}
	default:
		s.violate("step %d: unknown op %q", i, st.Op)
	}
	return false, nil
}

func labelOf(as, command string) string {
	if as != "" {
		return as
	}
	return command
}

func (s *Server) pause(ms int) {
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

// expect waits for the request the step names, answering the others with errors; it remembers the request under label.
func (s *Server) expect(i int, command string, args json.RawMessage, label string) (incoming, bool) {
	for {
		in, ok := s.take(isRequest)
		if !ok {
			return incoming{}, false
		}
		if in.Command != command {
			s.violate("step %d: expected request %q, got %q", i, command, in.Command)
			s.respond(in, nil, "unexpected request "+in.Command)
			continue
		}
		if len(args) > 0 && !contains(args, in.Arguments) {
			s.violate("step %d: the arguments of %q are %s, which do not contain %s", i, command, compact(in.Arguments), compact(args))
		}
		s.remembered[label] = in
		s.lastLabel = label
		return in, true
	}
}

func compact(raw json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

// contains reports whether every member of want is in got with an equal value, recursively for objects.
func contains(want, got json.RawMessage) bool {
	var w, g any
	if err := json.Unmarshal(want, &w); err != nil {
		return false
	}
	if err := json.Unmarshal(got, &g); err != nil {
		return false
	}
	return subset(w, g)
}

func subset(want, got any) bool {
	wm, ok := want.(map[string]any)
	if !ok {
		return reflect.DeepEqual(want, got)
	}
	gm, ok := got.(map[string]any)
	if !ok {
		return false
	}
	for k, wv := range wm {
		gv, present := gm[k]
		if !present || !subset(wv, gv) {
			return false
		}
	}
	return true
}

// answer writes the response a step describes.
func (s *Server) answer(in incoming, st Step) {
	if st.Fail {
		msg := st.Message
		if msg == "" {
			msg = "the fake adapter fails " + in.Command
		}
		s.respond(in, st.Body, msg)
		return
	}
	s.respond(in, st.Body, "")
}

// wireResponse is a response as the fake adapter writes it.
type wireResponse struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
	Command    string          `json:"command"`
	Message    string          `json:"message,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// respond writes the response to in; a non-empty failure makes it unsuccessful.
func (s *Server) respond(in incoming, body json.RawMessage, failure string) {
	_, err := s.send(func(seq int) any {
		return wireResponse{Seq: seq, Type: dap.TypeResponse, RequestSeq: in.Seq, Success: failure == "", Command: in.Command, Message: failure, Body: body}
	}, Message{Type: dap.TypeResponse, Name: in.Command, RequestSeq: in.Seq, Success: failure == "", Body: body})
	if err != nil {
		s.violate("writing a response: %v", err)
	}
}

func (s *Server) sendEvent(name string, body json.RawMessage) error {
	_, err := s.send(func(seq int) any {
		return struct {
			Seq   int             `json:"seq"`
			Type  string          `json:"type"`
			Event string          `json:"event"`
			Body  json.RawMessage `json:"body,omitempty"`
		}{seq, dap.TypeEvent, name, body}
	}, Message{Type: dap.TypeEvent, Name: name, Body: body})
	return err
}

// burst writes st.Count output events.
func (s *Server) burst(st Step) error {
	pad := strings.Repeat("x", max(st.Bytes, 0))
	for n := range st.Count {
		body, err := json.Marshal(map[string]any{"category": "stdout", "output": fmt.Sprintf("line %d %s", n, pad), "n": n})
		if err != nil {
			return errors.NewErr("ServeWriteFailed", err).SetMetadata("event", n)
		}
		if err := s.writeBurst(body); err != nil {
			return err
		}
		s.burstSent.Add(1)
	}
	return nil
}

// writeBurst writes one event of a burst, which is not kept in the traffic.
func (s *Server) writeBurst(body []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.seq++
	msg, err := json.Marshal(struct {
		Seq   int             `json:"seq"`
		Type  string          `json:"type"`
		Event string          `json:"event"`
		Body  json.RawMessage `json:"body"`
	}{s.seq, dap.TypeEvent, dap.EventOutput, body})
	if err != nil {
		return errors.NewErr("ServeWriteFailed", err)
	}
	return s.writeFramed(msg)
}

// send numbers a message, records it and writes it, all under the lock of the writer, so that the numbers go up in the order of
// the wire. build makes the message from its number. It returns the number.
func (s *Server) send(build func(seq int) any, rec Message) (int, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.seq++
	seq := s.seq
	msg, err := json.Marshal(build(seq))
	if err != nil {
		return seq, errors.NewErr("ServeWriteFailed", err).SetMetadata("seq", seq)
	}
	rec.Direction, rec.Seq = "out", seq
	s.record(rec)
	return seq, s.writeFramed(msg)
}

// record keeps a message in the traffic.
func (s *Server) record(m Message) {
	s.rec.Lock()
	s.traffic = append(s.traffic, m)
	s.rec.Unlock()
}

// setExit keeps the exit status a step named.
func (s *Server) setExit(code int) {
	s.rec.Lock()
	s.exitCode = code
	s.rec.Unlock()
}

// writeFramed writes a framed message with the lock of the writer held; a stream the client closed is the end of the session and
// not a failure.
func (s *Server) writeFramed(body []byte) error {
	if err := dap.WriteFrame(s.w, body); err != nil {
		if isClosed(err) {
			s.abort() // the client is gone: nothing more to say
			return nil
		}
		return err // already tagged by dap.WriteFrame
	}
	return nil
}

// writeRaw writes bytes to the stream as they are.
func (s *Server) writeRaw(b []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if _, err := s.w.Write(b); err != nil {
		if isClosed(err) {
			s.abort()
			return nil
		}
		return errors.NewErr("ServeWriteFailed", err)
	}
	return nil
}

// isClosed reports whether a write failed because the other end is gone.
func isClosed(err error) bool {
	return errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}

// frame frames a body.
func frame(body []byte) []byte {
	return append(fmt.Appendf(nil, "Content-Length: %d\r\n\r\n", len(body)), body...)
}
