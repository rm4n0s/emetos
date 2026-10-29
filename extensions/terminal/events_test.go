package terminal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/pty"
)

// eventLog collects the events of the extension (the fake delivers them at once, on the goroutine that publishes).
type eventLog struct {
	mu     sync.Mutex
	events []extension.Event
}

func (l *eventLog) add(ev extension.Event) {
	l.mu.Lock()
	l.events = append(l.events, ev)
	l.mu.Unlock()
}

func (l *eventLog) topic(topic string) []extension.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []extension.Event
	for _, ev := range l.events {
		if ev.Topic == topic {
			out = append(out, ev)
		}
	}
	return out
}

// listen subscribes to the events of the terminals.
func (r *extRig) listen() *eventLog {
	r.t.Helper()
	l := &eventLog{}
	if _, err := r.api.Events().Subscribe("emetos.terminal.*", l.add); err != nil {
		r.t.Fatal(err)
	}
	return l
}

// ask makes a request as the extension with the id publisher and decodes the reply into out.
func (r *extRig) ask(publisher, topic string, payload any, out any) error {
	r.t.Helper()
	ev, err := r.api.AskAs(context.Background(), publisher, topic, payload)
	if err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(ev.Data, out); err != nil {
			r.t.Fatalf("the reply of %s is not what the test expects: %v (%s)", topic, err, ev.Data)
		}
	}
	return nil
}

// eventually waits for a condition, pumping what the extension posted.
func (r *extRig) eventually(what string, check func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		r.api.Pump()
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheExtensionIsActiveFromTheStartAndHandlesTheRequests(t *testing.T) {
	m := New().Manifest()
	if len(m.ActivationEvents) != 1 || m.ActivationEvents[0] != extension.OnStartup {
		t.Errorf("activation events = %v; a client asks for the terminals of a workspace whose panel was never opened", m.ActivationEvents)
	}
	r := newExtRig(t, "")
	for _, topic := range []string{TopicList, TopicSnapshot, TopicCreate, TopicWrite, TopicRun, TopicKill} {
		_, err := r.api.AskAs(context.Background(), "test", topic, map[string]any{})
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "EventNoHandler" {
			t.Errorf("nothing handles %s", topic)
		}
	}
}

// TestTheEventsOfATerminalFromItsStartToItsEnd runs the test shell with --tty-echo: it leaves the terminal's ECHO on, so what
// is typed is not hidden (decision 185) at any moment. TestInputIsHiddenOnlyAtAPasswordPrompt holds the other modes.
func TestTheEventsOfATerminalFromItsStartToItsEnd(t *testing.T) {
	r := newExtRig(t, "", "--tty-echo")
	r.ext.outputInterval = 5 * time.Millisecond
	log := r.listen()
	r.run(CmdNew, map[string]any{"name": "builder"})
	e := r.ext.activeEntry()
	id := e.term.ID()
	waitText(t, e.term, "ready") // the shell is running and has set up the terminal (it says "ready" after that)

	created := log.topic(TopicCreated)
	if len(created) != 1 {
		t.Fatalf("created events: %d", len(created))
	}
	var c createdEvent
	if err := created[0].Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id || c.Profile != "test" || c.Cwd != r.dir {
		t.Errorf("created = %+v", c)
	}

	// what the program writes arrives as output events, in order, and the title as a title event
	e.term.Type([]byte("echo seen\rtitle my title\r"), ByUser)
	r.eventually("the output and the title", func() bool {
		var text strings.Builder
		for _, ev := range log.topic(TopicOutput) {
			var o outputEvent
			_ = ev.Decode(&o)
			text.WriteString(o.Data)
		}
		return strings.Contains(text.String(), "seen") && len(log.topic(TopicTitle)) > 0
	})
	var last uint64
	for _, ev := range log.topic(TopicOutput) {
		var o outputEvent
		_ = ev.Decode(&o)
		if o.ID != id || o.Seq != last+1 {
			t.Errorf("output event %+v after seq %d", o, last)
		}
		last = o.Seq
	}
	var title titleEvent
	if err := log.topic(TopicTitle)[0].Decode(&title); err != nil || title.ID != id || title.Title != "my title" {
		t.Errorf("title = %+v, %v", title, err)
	}

	// what the user typed, with who typed it
	inputs := log.topic(TopicInput)
	if len(inputs) == 0 {
		t.Fatal("no input event")
	}
	var in inputEvent
	_ = inputs[0].Decode(&in)
	if in.ID != id || in.By != ByUser || in.Data != "echo seen\rtitle my title\r" {
		t.Errorf("input = %+v", in)
	}

	// a resize
	e.term.Resize(100, 30)
	resized := log.topic(TopicResized)
	if len(resized) != 1 {
		t.Fatalf("resized events: %d", len(resized))
	}
	var rs resizedEvent
	_ = resized[0].Decode(&rs)
	if rs.ID != id || rs.Cols != 100 || rs.Rows != 30 {
		t.Errorf("resized = %+v", rs)
	}

	// the program ends: exited with its status, the terminal goes: closed, and the output before them is out first
	e.term.Type([]byte("exit 3\r"), ByUser)
	r.eventually("exited and closed", func() bool { return len(log.topic(TopicClosed)) == 1 })
	exited := log.topic(TopicExited)
	if len(exited) != 1 {
		t.Fatalf("exited events: %d", len(exited))
	}
	var ex exitedEvent
	_ = exited[0].Decode(&ex)
	if ex.ID != id || ex.Code != 3 {
		t.Errorf("exited = %+v", ex)
	}
	order := map[string]int{}
	log.mu.Lock()
	for i, ev := range log.events {
		order[ev.Topic] = i
	}
	log.mu.Unlock()
	if order[TopicOutput] >= order[TopicExited] || order[TopicExited] >= order[TopicClosed] {
		t.Errorf("the last indexes: output %d, exited %d, closed %d", order[TopicOutput], order[TopicExited], order[TopicClosed])
	}
}

// TestInputIsHiddenOnlyAtAPasswordPrompt holds decision 185 with real pseudo-terminals: the test shell waits for the "ready" it
// says after it set the mode of its terminal, so the test does not depend on timing. A line editor (raw mode, ECHO off or on)
// has its input published; a password prompt (ECHO off, ICANON on) has it hidden, for the user and for an extension.
func TestInputIsHiddenOnlyAtAPasswordPrompt(t *testing.T) {
	tests := []struct {
		name       string
		arg        string
		by         string
		wantHidden bool
	}{
		{"raw mode with ECHO off, the default of the test shell (a line editor)", "", ByUser, false},
		{"raw mode with ECHO on", "--tty-echo", ByUser, false},
		{"canonical with ECHO off (a password prompt) typed by the user", "--password", ByUser, true},
		{"canonical with ECHO off (a password prompt) typed by an extension", "--password", "emetos.other", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args []string
			if tt.arg != "" {
				args = append(args, tt.arg)
			}
			r := newExtRig(t, "", args...)
			log := r.listen()
			r.run(CmdNew, nil)
			e := r.ext.activeEntry()
			waitText(t, e.term, "ready")
			var err error
			if tt.by == ByUser {
				e.term.Type([]byte("hunter2\r"), ByUser)
			} else {
				err = r.ask(tt.by, TopicWrite, map[string]any{"id": e.term.ID(), "data": "hunter2\r"}, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			inputs := log.topic(TopicInput)
			if len(inputs) != 1 {
				t.Fatalf("input events: %d", len(inputs))
			}
			var in inputEvent
			if err := json.Unmarshal(inputs[0].Data, &in); err != nil {
				t.Fatal(err)
			}
			wantData := "hunter2\r"
			if tt.wantHidden {
				wantData = ""
			}
			if in.Hidden != tt.wantHidden || in.Data != wantData || in.By != tt.by {
				t.Errorf("input = %+v, want hidden %v, data %q, by %q", in, tt.wantHidden, wantData, tt.by)
			}
			if tt.wantHidden {
				// nothing an extension or a log can read has the secret: not the input event, not the output (the prompt did not echo)
				waitText(t, e.term, "password received (7 bytes)")
				r.eventually("the output", func() bool { return len(log.topic(TopicOutput)) > 0 })
				log.mu.Lock()
				defer log.mu.Unlock()
				for _, ev := range log.events {
					if strings.Contains(string(ev.Data), "hunter2") {
						t.Errorf("the secret is in %s: %s", ev.Topic, ev.Data)
					}
				}
			}
		})
	}
}

// TestInputToARealBashAtItsPromptIsPublished holds that readline, which turns ECHO and ICANON off, does not make ordinary typing
// secret: what is typed at the prompt of bash reaches the events.
func TestInputToARealBashAtItsPromptIsPublished(t *testing.T) {
	r := bashExt(t)
	log := r.listen()
	e := r.openBash()
	e.term.Type([]byte("echo hi\r"), "emetos.other")
	inputs := log.topic(TopicInput)
	if len(inputs) != 1 {
		t.Fatalf("input events: %d", len(inputs))
	}
	var in inputEvent
	if err := json.Unmarshal(inputs[0].Data, &in); err != nil {
		t.Fatal(err)
	}
	if in.Hidden || in.Data != "echo hi\r" || in.By != "emetos.other" {
		t.Errorf("input = %+v, want the text, not hidden", in)
	}
}

func TestListAndSnapshot(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, map[string]any{"name": "one"})
	r.run(CmdNew, nil)
	first := r.ext.all()[0]
	first.term.Type([]byte("echo snapshot-text\r"), ByUser)
	waitText(t, first.term, "snapshot-text")

	var list listReply
	if err := r.ask("test", TopicList, nil, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Terminals) != 2 || list.Terminals[0].ID != first.term.ID() || list.Terminals[0].Name != "one" || list.Terminals[1].Name != "test" ||
		list.Terminals[0].Cols != 80 || list.Terminals[0].Rows != 24 || list.Terminals[0].Exited {
		t.Errorf("list = %+v", list)
	}

	var snap snapshotReply
	if err := r.ask("test", TopicSnapshot, map[string]any{"id": first.term.ID()}, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Cols != 80 || snap.Rows != 24 || !strings.Contains(snap.Data, "snapshot-text") {
		t.Errorf("snapshot = %d x %d, %q", snap.Cols, snap.Rows, snap.Data)
	}
	// the data makes the same screen in an emulator of that size, and seq counts the pieces of output it includes
	if snap.Seq == 0 {
		t.Errorf("seq = 0 for a terminal that wrote output")
	}
}

func TestRequestsFailWithTheirOwnTags(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	id := r.ext.activeEntry().term.ID()
	out := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r.dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(r.dir, "escape")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		topic   string
		payload any
		raw     string
		route   string
	}{
		{"a payload that is not an object", TopicWrite, nil, `[1,2]`, "TerminalPayloadInvalid"},
		{"write to no terminal", TopicWrite, map[string]any{"id": 999, "data": "x"}, "", "handleWrite.TerminalNotFound"},
		{"snapshot of no terminal", TopicSnapshot, map[string]any{"id": 999}, "", "handleSnapshot.TerminalNotFound"},
		{"kill of no terminal", TopicKill, map[string]any{"id": 999}, "", "handleKill.TerminalNotFound"},
		{"run in no terminal", TopicRun, map[string]any{"id": 999, "text": "x"}, "", "handleRun.TerminalNotFound"},
		{"a profile that does not exist", TopicCreate, map[string]any{"profile": "nope"}, "", "TerminalProfileUnknown"},
		{"an absolute folder", TopicCreate, map[string]any{"cwd": "/tmp"}, "", "TerminalCwdInvalid"},
		{"a folder with ..", TopicCreate, map[string]any{"cwd": "sub/../../x"}, "", "TerminalCwdInvalid"},
		{"a folder with a NUL", TopicCreate, map[string]any{"cwd": "sub\x00"}, "", "TerminalCwdInvalid"},
		{"a folder that is not there", TopicCreate, map[string]any{"cwd": "missing"}, "", "TerminalCwdNotFound"},
		{"a file, not a folder", TopicCreate, map[string]any{"cwd": "file.txt"}, "", "TerminalCwdNotDirectory"},
		{"a link that leads out", TopicCreate, map[string]any{"cwd": "escape"}, "", "TerminalCwdOutside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := tt.payload
			if tt.raw != "" {
				payload = json.RawMessage(tt.raw)
			}
			before := r.ext.count()
			err := r.ask("test", tt.topic, payload, nil)
			if err == nil {
				t.Fatal("the request succeeded")
			}
			routeHas(t, err, tt.route)
			if r.ext.count() != before {
				t.Errorf("a failed request changed the number of terminals: %d, was %d", r.ext.count(), before)
			}
		})
	}
	_ = id

	t.Run("a folder with no workspace folder", func(t *testing.T) {
		r.ext.folder = ""
		defer func() { r.ext.folder = r.dir }()
		err := r.ask("test", TopicCreate, map[string]any{"cwd": "sub"}, nil)
		routeHas(t, err, "TerminalNoFolder")
	})
}

func TestCreateMakesATerminalInAConfinedFolderWithoutTakingTheScreen(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil) // the person at the computer has a terminal
	user := r.ext.activeEntry()
	view := r.api.View(ViewID)
	reveals := len(view.Reveals)
	if err := os.MkdirAll(filepath.Join(r.dir, "a", "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	var made createReply
	if err := r.ask("emetos.other", TopicCreate, map[string]any{"cwd": "a/b", "name": "from afar"}, &made); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	e := r.ext.entryByID(made.ID)
	if e == nil {
		t.Fatalf("no terminal %d", made.ID)
	}
	if want := filepath.Join(real, "a", "b"); e.term.Cwd() != want || e.term.Name() != "from afar" {
		t.Errorf("cwd %q (want %q), name %q", e.term.Cwd(), want, e.term.Name())
	}
	if r.ext.activeEntry() != user || r.ext.paneFocus != user && r.ext.paneFocus != nil {
		t.Error("the new terminal became the active one")
	}
	if len(view.Reveals) != reveals {
		t.Error("the panel was revealed for a terminal an extension made")
	}
	// "." is the folder itself
	if err := r.ask("test", TopicCreate, map[string]any{"cwd": "."}, &made); err != nil {
		t.Fatal(err)
	}
	if e := r.ext.entryByID(made.ID); e == nil || e.term.Cwd() != real {
		t.Errorf("the terminal for \".\" is in %v", e)
	}
}

func TestCreateWithShowOpensTheTerminalForAnExtension(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	view := r.api.View(ViewID)
	tests := []struct {
		name      string
		publisher string
		payload   map[string]any
		shown     bool
	}{
		{"an extension that asks to show it", "emetos.containers", map[string]any{"name": "Shell", "show": true}, true},
		{"an extension that does not", "emetos.containers", map[string]any{"name": "Shell"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := r.ext.activeEntry()
			reveals := len(view.Reveals)
			var made createReply
			if err := r.ask(tc.publisher, TopicCreate, tc.payload, &made); err != nil {
				t.Fatal(err)
			}
			r.api.Pump()
			e := r.ext.entryByID(made.ID)
			if e == nil {
				t.Fatalf("no terminal %d", made.ID)
			}
			if got := r.ext.activeEntry() == e; got != tc.shown {
				t.Errorf("the new terminal is active = %v, want %v", got, tc.shown)
			}
			if got := len(view.Reveals) > reveals; got != tc.shown {
				t.Errorf("the panel was revealed = %v, want %v", got, tc.shown)
			}
			if !tc.shown && r.ext.activeEntry() != before {
				t.Error("the active terminal changed")
			}
		})
	}
}

func TestWriteAndRunTypeInATerminalAndSayWhoDid(t *testing.T) {
	// a shell whose terminal echoes, as a real shell's does (the test shell reads in raw mode, with no echo)
	r := newExtRig(t, "/bin/sh")
	r.ext.outputInterval = 5 * time.Millisecond
	r.run(CmdNew, nil)
	e := r.ext.activeEntry()
	log := r.listen()

	// an extension: the bus says who it is, and its payload cannot say another
	if err := r.ask("emetos.other", TopicWrite, map[string]any{"id": e.term.ID(), "data": "echo from-extension\r", "by": "user"}, nil); err != nil {
		t.Fatal(err)
	}
	waitText(t, e.term, "from-extension")
	inputs := log.topic(TopicInput)
	var in inputEvent
	if len(inputs) != 1 || inputs[0].Decode(&in) != nil || in.By != "emetos.other" || in.Data != "echo from-extension\r" {
		t.Fatalf("input events = %+v", inputs)
	}
	// run says who did it, too
	if err := r.ask("emetos.second", TopicRun, map[string]any{"text": "echo second"}, nil); err != nil {
		t.Fatal(err)
	}
	waitText(t, e.term, "second")
	inputs = log.topic(TopicInput)
	if len(inputs) < 2 {
		t.Fatalf("input events = %d", len(inputs))
	}
	_ = inputs[len(inputs)-1].Decode(&in)
	if in.By != "emetos.second" {
		t.Errorf("run was taken for %q", in.By)
	}
	// "run" with a new line inside becomes Enter, and ends with one
	if err := r.ask("test", TopicRun, map[string]any{"id": e.term.ID(), "text": "echo a\necho b"}, nil); err != nil {
		t.Fatal(err)
	}
	waitText(t, e.term, "\nb")

	// run with no terminal makes one
	r.run(CmdKill, nil)
	for r.ext.count() > 0 {
		r.run(CmdKill, nil)
	}
	var made createReply
	if err := r.ask("test", TopicRun, map[string]any{"text": "echo fresh"}, &made); err != nil {
		t.Fatal(err)
	}
	if e := r.ext.entryByID(made.ID); e == nil {
		t.Fatalf("run made no terminal")
	} else {
		waitText(t, e.term, "fresh")
	}
}

func TestKillClosesTheTerminalAndTellsIt(t *testing.T) {
	r := newExtRig(t, "")
	log := r.listen()
	r.run(CmdNew, nil)
	id := r.ext.activeEntry().term.ID()
	if err := r.ask("test", TopicKill, map[string]any{"id": id}, nil); err != nil {
		t.Fatal(err)
	}
	if r.ext.count() != 0 {
		t.Errorf("%d terminals after kill", r.ext.count())
	}
	closed := log.topic(TopicClosed)
	var c closedEvent
	if len(closed) != 1 || closed[0].Decode(&c) != nil || c.ID != id {
		t.Errorf("closed events = %+v", closed)
	}
}

func TestWritingToATerminalThatExitedOrThatDoesNotReadIsRefused(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	e := r.ext.activeEntry()
	e.term.Type([]byte("exit\r"), ByUser)
	select {
	case <-e.term.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("the terminal did not end")
	}
	// the exited program stays listed until the posted removal runs; a write to it is refused
	err := r.ask("test", TopicWrite, map[string]any{"id": e.term.ID(), "data": "x"}, nil)
	routeHas(t, err, "handleWrite.TerminalExited")
	if err := r.ask("test", TopicRun, map[string]any{"id": e.term.ID(), "text": "x"}, nil); err == nil {
		t.Error("run in an exited terminal succeeded")
	} else {
		routeHas(t, err, "handleRun.TerminalExited")
	}

	// a program that does not read: what a client writes piles up until the limit
	r2 := newExtRig(t, "")
	f := &stuckProcess{fakeProcess: newFakeProcess(), stuck: make(chan struct{})}
	t.Cleanup(func() { close(f.stuck) })
	r2.ext.start = func(context.Context, pty.Options) (process, error) { return f, nil }
	r2.run(CmdNew, nil)
	id := r2.ext.activeEntry().term.ID()
	big := strings.Repeat("x", 1<<20)
	var refused error
	for range maxPendingInput/(1<<20) + 3 {
		if refused = r2.ask("test", TopicWrite, map[string]any{"id": id, "data": big}, nil); refused != nil {
			break
		}
	}
	if refused == nil {
		t.Fatal("writes to a program that never reads were never refused")
	}
	routeHas(t, refused, "handleWrite.TerminalInputFull")
	if err := r2.ask("test", TopicRun, map[string]any{"id": id, "text": "y"}, nil); err == nil {
		t.Error("run was accepted while the input is full")
	} else {
		routeHas(t, err, "handleRun.TerminalInputFull")
	}
}

// stuckProcess is a program that does not read its terminal: a write waits until the test ends.
type stuckProcess struct {
	*fakeProcess
	stuck chan struct{}
}

func (s *stuckProcess) Write(p []byte) (int, error) {
	<-s.stuck
	return len(p), nil
}
