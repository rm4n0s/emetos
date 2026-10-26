package claude

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
)

// Session is one conversation: one child process of the claude program, talked to through its standard input and
// output. Start makes it; Close ends it. All its methods are safe for concurrent use.
type Session struct {
	adapter *Adapter
	proc    *process

	onEvent        func(Event)
	canUseTool     CanUseToolFunc
	requestTimeout time.Duration
	maxLine        int
	closeGrace     time.Duration
	killGrace      time.Duration

	// hctx is the context of the tool request handlers; it ends when the session does.
	hctx       context.Context
	cancelAll  context.CancelFunc
	reqCounter atomic.Uint64
	// toolSeq numbers the can_use_tool requests as they come in (ToolRequest.Seq).
	toolSeq atomic.Uint64

	writeMu      sync.Mutex
	stdinClosed  bool
	closing      atomic.Bool
	closeOnce    sync.Once
	queue        *eventQueue
	done         chan struct{}
	stderrDone   chan struct{}
	stderrOutput func(line string)

	mu        sync.Mutex
	pending   map[string]*pending
	handlers  map[string]context.CancelFunc
	finished  bool
	init      *InitMessage
	sessionID string
	handshake Handshake
	tail      []string // the last lines of the standard error
}

// maxStderrLine is the longest line of the standard error that is kept whole; the rest of a longer line is dropped.
const maxStderrLine = 64 * 1024

// stderrTailLines is how many of the last lines of the standard error the ProgramExited error carries.
const stderrTailLines = 20

// Start starts the program for a conversation in Dir and does the handshake (the initialize request): when it returns,
// Handshake has the commands and models. The program runs in a process group of its own; when ctx ends the group is told
// to terminate and, after two seconds, killed, and the session ends (the last event is an ExitEvent with no error). It
// fails with the tags InvalidOptions, ProgramNotFound, ProcessStartFailed, and the tags of the initialize request
// (ControlRequestTimeout, ProgramExited, HandshakeInvalid, ...); the process is gone when it fails.
func (a *Adapter) Start(ctx context.Context, o SessionOptions) (*Session, error) {
	args, err := Args(o)
	if err != nil {
		return nil, err
	}
	path, err := a.Find()
	if err != nil {
		return nil, err
	}
	killGrace := orDefault(a.killGrace, defaultKillGrace)
	proc, err := startProcess(path, args, a.Dir, Environment(os.Environ()), killGrace)
	if err != nil {
		return nil, err
	}
	hctx, cancel := context.WithCancel(ctx)
	s := &Session{
		adapter: a, proc: proc, onEvent: o.OnEvent, canUseTool: o.CanUseTool, stderrOutput: o.Stderr,
		requestTimeout: o.RequestTimeout, maxLine: o.MaxLineBytes,
		closeGrace: orDefault(a.closeGrace, defaultCloseGrace), killGrace: killGrace,
		hctx: hctx, cancelAll: cancel, queue: newEventQueue(), done: make(chan struct{}), stderrDone: make(chan struct{}),
		pending: map[string]*pending{}, handlers: map[string]context.CancelFunc{}, sessionID: o.SessionID,
	}
	if s.requestTimeout <= 0 {
		s.requestTimeout = DefaultRequestTimeout
	}
	go s.watchContext(ctx)
	go s.readStderr()
	go s.dispatch()
	go s.readLoop()
	hs, err := s.initialize(ctx)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	s.mu.Lock()
	s.handshake = hs
	s.mu.Unlock()
	return s, nil
}

// watchContext ends the program when the context of Start ends.
func (s *Session) watchContext(ctx context.Context) {
	select {
	case <-ctx.Done():
		s.closing.Store(true)
		s.proc.terminate(s.killGrace)
	case <-s.proc.exited:
	}
}

// Handshake is the answer to the initialize request: the commands, the models and the account.
func (s *Session) Handshake() Handshake {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handshake
}

// Init is the system/init message of the conversation, nil until the program has written it.
func (s *Session) Init() *InitMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.init
}

// Capabilities are the capabilities of the program from system/init; empty until that message came. Features of the UI
// (rewind, the interrupt receipt) depend on them, not on a version.
func (s *Session) Capabilities() Capabilities {
	if m := s.Init(); m != nil {
		return m.Capabilities
	}
	return nil
}

// SessionID is the ID of the conversation: the one given in the options, else the one system/init reported ("" before
// that). It is what a later Start resumes.
func (s *Session) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

// PID is the process ID of the program, which is also the ID of its process group.
func (s *Session) PID() int { return s.proc.pgid }

// Done is closed when the session is over and its last event, the ExitEvent, was handed to OnEvent.
func (s *Session) Done() <-chan struct{} { return s.done }

// Exited is closed when the program and its group are gone.
func (s *Session) Exited() <-chan struct{} { return s.proc.exited }

// userLine is the line a user message is written as.
type userLine struct {
	Type            string      `json:"type"`
	UUID            string      `json:"uuid"`
	SessionID       string      `json:"session_id"`
	Message         userContent `json:"message"`
	ParentToolUseID *string     `json:"parent_tool_use_id"`
}

type userContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Send writes a user message and returns its UUID, which the program replays in the UserMessage it echoes and which
// RewindFiles takes. The program queues messages sent while a turn runs. It fails with the tags SessionClosed and SendFailed.
func (s *Session) Send(ctx context.Context, text string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.NewErr("SendFailed", err)
	}
	id, err := newUUID()
	if err != nil {
		return "", errors.NewErr("SendFailed", err)
	}
	line := userLine{Type: "user", UUID: id, Message: userContent{Role: "user", Content: text}}
	if err := s.write(line); err != nil {
		return "", err
	}
	return id, nil
}

// newUUID makes a random (version 4) UUID.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// write writes one JSON line to the standard input of the program. It fails with the tags SessionClosed (the input is
// closed) and SendFailed.
func (s *Session) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return errors.NewErr("SendFailed", err)
	}
	b = append(b, '\n')
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.stdinClosed {
		return errors.New("SessionClosed", "the input of the program is closed")
	}
	if _, err := s.proc.stdin.Write(b); err != nil {
		return errors.NewErr("SendFailed", err)
	}
	return nil
}

// Close ends the session: it closes the input of the program, which makes it end, and when it has not ended after three
// seconds terminates its process group, and after two more kills it. It returns when the program and its group are gone.
// Pending requests fail with SessionClosed, tool requests are cancelled. It is safe to call more than once.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.closing.Store(true)
		s.cancelAll()
		s.writeMu.Lock()
		s.stdinClosed = true
		_ = s.proc.stdin.Close() // a program that already ended: nothing to close
		s.writeMu.Unlock()
	})
	select {
	case <-s.proc.exited:
	case <-time.After(s.closeGrace):
		s.proc.terminate(s.killGrace)
	}
	<-s.proc.exited
	return nil
}

// readStderr reads the standard error of the program line by line.
func (s *Session) readStderr() {
	defer close(s.stderrDone)
	r := bufio.NewReaderSize(s.proc.stderr, 4096)
	for {
		line, err := readBoundedLine(r, maxStderrLine)
		if line != "" {
			s.mu.Lock()
			s.tail = append(s.tail, line)
			if len(s.tail) > stderrTailLines {
				s.tail = s.tail[len(s.tail)-stderrTailLines:]
			}
			s.mu.Unlock()
			if s.stderrOutput != nil {
				s.stderrOutput(line)
			}
		}
		if err != nil {
			return
		}
	}
}

// readBoundedLine reads a line and drops what is beyond limit bytes of it.
func readBoundedLine(r *bufio.Reader, limit int) (string, error) {
	var sb strings.Builder
	for {
		frag, err := r.ReadSlice('\n')
		if room := limit - sb.Len(); room > 0 {
			sb.Write(frag[:min(room, len(frag))])
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return strings.TrimRight(sb.String(), "\r\n"), err
	}
}

// readLoop reads the output of the program until it ends, then finishes the session.
func (s *Session) readLoop() {
	defer s.finish()
	lr := newLineReader(s.proc.stdout, s.maxLine)
	for {
		line, tooLong, err := lr.next()
		if tooLong {
			s.protocolError(errors.New("LineTooLong", "a line of the program is longer than the limit", "limit", lr.max), line)
			if err != nil {
				return
			}
			continue
		}
		if len(line) > 0 {
			s.handleLine(line)
		}
		if err != nil {
			return
		}
	}
}

// protocolError reports a problem with a line to OnEvent; the line is cut to 1 KiB.
func (s *Session) protocolError(err error, line []byte) {
	s.queue.push(&ProtocolErrorEvent{Err: err, Line: append([]byte(nil), line[:min(len(line), 1024)]...)})
}

// handleLine takes one line of the output: a control message, or a message that becomes an event.
func (s *Session) handleLine(line []byte) {
	env, err := parseEnvelope(line)
	if err != nil {
		s.protocolError(err, line)
		return
	}
	switch env.Type {
	case "control_response":
		err = s.handleControlResponse(env)
	case "control_request":
		err = s.handleControlRequest(env)
	case "control_cancel_request":
		s.cancelHandler(env.RequestID)
	default:
		var msg Message
		if msg, err = buildMessage(env, line); err == nil {
			s.note(msg)
			s.queue.push(msg)
		}
	}
	if err != nil {
		s.protocolError(err, line)
	}
}

// note remembers what the session needs to know of a message before its event is delivered: the init message.
func (s *Session) note(m Message) {
	if in, ok := m.(*InitMessage); ok {
		s.mu.Lock()
		s.init = in
		if in.SessionID != "" {
			s.sessionID = in.SessionID
		}
		s.mu.Unlock()
	}
}

// finish ends the session after the output of the program ended: it waits for the program, fails what waits for an
// answer, and delivers the ExitEvent.
func (s *Session) finish() {
	<-s.proc.exited
	<-s.stderrDone
	code := s.proc.exitCode()
	var exitErr, pendingErr error
	if s.closing.Load() {
		pendingErr = errors.New("SessionClosed", "the session was closed", "exit", code)
	} else {
		s.mu.Lock()
		tail := strings.Join(s.tail, "\n")
		s.mu.Unlock()
		exitErr = errors.New("ProgramExited", "the claude program ended", "exit", code, "stderr", tail)
		pendingErr = exitErr
	}
	s.mu.Lock()
	s.finished = true
	for id, p := range s.pending {
		p.ch <- controlResult{err: pendingErr}
		delete(s.pending, id)
	}
	s.mu.Unlock()
	s.cancelAll()
	s.queue.push(&ExitEvent{Code: code, Err: exitErr})
	s.queue.close()
}

// dispatch hands the events to OnEvent, in order, then closes done.
func (s *Session) dispatch() {
	defer close(s.done)
	for {
		ev, ok := s.queue.pop()
		if !ok {
			return
		}
		if s.onEvent != nil {
			s.onEvent(ev)
		}
	}
}

// eventQueue is an unbounded queue of events: the reader of the output never waits for OnEvent.
type eventQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []Event
	closed bool
}

func newEventQueue() *eventQueue {
	q := &eventQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *eventQueue) push(ev Event) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, ev)
	}
	q.mu.Unlock()
	q.cond.Signal()
}

func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

// pop waits for the next event; false when the queue is closed and empty.
func (q *eventQueue) pop() (Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return nil, false
	}
	ev := q.items[0]
	q.items[0] = nil
	q.items = q.items[1:]
	return ev, true
}
