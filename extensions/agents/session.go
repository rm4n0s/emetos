package agents

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// phase is what a session tab knows about its program.
type phase int

const (
	// phaseIdle: no program runs. A tab that was restored, or whose program ended, starts one when the user sends a message.
	phaseIdle phase = iota
	// phaseStarting: the program is being started (the login is checked first, then the handshake is done).
	phaseStarting
	// phaseReady: the program runs and has answered the handshake.
	phaseReady
	// phaseMissing: the program was not found.
	phaseMissing
	// phaseLoggedOut: the user is not logged in.
	phaseLoggedOut
	// phaseFailed: the program could not be started, for the reason in problem.
	phaseFailed
)

// defaultMode is the permission mode of a new session.
const defaultMode = claude.ModeManual

// commandInfo is a slash command of the program, as the tab saves it.
type commandInfo struct {
	Name        string `json:"name"`
	Description string `json:"desc,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

// modelInfo is a model of the program, as the tab saves it.
type modelInfo struct {
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
}

// tabState is the state of a session tab: what comes back after a restart, and what Open starts a tab with.
type tabState struct {
	// Session is the session ID, a UUID that Emetos made and gave the program.
	Session string `json:"session"`
	// Used says that the program has got a message of this session: the next start resumes it instead of making it.
	Used bool `json:"used,omitempty"`
	// Model is the model chosen ("" is the default) and Mode the permission mode.
	Model string `json:"model,omitempty"`
	Mode  string `json:"mode,omitempty"`
	// Items is the conversation.
	Items []*item `json:"items,omitempty"`
	// Commands and Models are the last handshake, so that a restored tab offers them before the program runs.
	Commands []commandInfo `json:"commands,omitempty"`
	Models   []modelInfo   `json:"models,omitempty"`
	// Start asks for the program to start at once (a new session); a restored tab never has it.
	Start bool `json:"start,omitempty"`
}

// queued is an event of the program, with the start (generation) it belongs to.
type queued struct {
	gen int
	ev  claude.Event
	// notice, when ev is nil, is a line for the conversation that the tab itself wants in order with the events.
	notice string
}

// sessionTab is one session tab. Its fields are for the UI thread, except the queue, which the goroutine of the program's
// events fills.
type sessionTab struct {
	ext *agents
	tab extension.EditorTab
	id  string

	used, closed bool
	// forget says that the session was deleted from the index: closing the tab does not put it back.
	forget bool

	tr    transcript
	phase phase
	// problem is the text of the banner of a missing, logged-out or failed tab.
	problem string

	// turns counts the messages the program has not answered with a result yet; busy is turns > 0.
	turns        int
	busy         bool
	interrupting bool

	sess   *claude.Session
	cancel context.CancelFunc
	gen    int
	// pending are the messages that wait for the program to be ready.
	pending []string

	commands []commandInfo
	models   []modelInfo
	// model is the model chosen ("" the default), running the model the program says it uses.
	model, running string
	mode           claude.PermissionMode

	// replay says that the tab shows a recorded stream and starts no program (the dev command).
	replay bool

	// perms are the permission requests that wait for the user, the first being the one that is asked (permissions.go).
	perms []*permission
	// visible says that the widget of the tab is on screen (tabview.go); a tab that is not is told about by notifications.
	visible bool
	// rewinding is the message whose button asks "Restore the files?" and restoring says that a rewind is under way (rewind.go).
	rewinding *item
	restoring bool

	qmu         sync.Mutex
	queue       []queued
	flushPosted bool

	input  *ui.InputBoxController
	comp   completion
	scroll ui.ScrollHandle
	// stick says that the conversation follows its end.
	stick bool
}

// createTab makes the content of a tab, for a new tab and for one that came back after a restart.
func (a *agents) createTab(tab extension.EditorTab, raw json.RawMessage) (extension.EditorTabContent, error) {
	var st tabState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return extension.EditorTabContent{}, errors.NewErr("SessionTabStateInvalid", err)
		}
	}
	if st.Session == "" {
		return extension.EditorTabContent{}, errors.New("SessionTabStateInvalid", "the state of a session tab has no session ID")
	}
	for _, other := range a.tabs {
		if other.id == st.Session {
			return extension.EditorTabContent{}, errors.New("SessionTabDuplicate", "a tab of this session is open already", "session", st.Session)
		}
	}
	mode := claude.PermissionMode(st.Mode)
	if !mode.Valid() {
		mode = defaultMode
	}
	t := &sessionTab{
		ext: a, tab: tab, id: st.Session, used: st.Used, model: st.Model, mode: mode, commands: st.Commands, models: st.Models,
		input: ui.NewInputBoxController(""), stick: true, tr: transcript{items: st.Items, folder: a.api.Scope().Folder},
	}
	t.input.FocusNode().SetOnKeyEvent(t.onKey)
	a.tabs = append(a.tabs, t)
	a.active = t
	t.retitle()
	tab.SetHint("Agent session " + t.id)
	if st.Start {
		t.ensureStarted()
	}
	return extension.EditorTabContent{
		Build: t.build,
		SaveState: func() (any, error) {
			return tabState{
				Session: t.id, Used: t.used, Model: t.model, Mode: string(t.mode), Items: t.tr.saved(maxSavedBytes),
				Commands: t.commands, Models: t.models,
			}, nil
		},
		OnClose: t.close,
	}, nil
}

// retitle sets the title of the tab from the index.
func (t *sessionTab) retitle() {
	title := untitled
	if e := t.ext.entry(t.id); e != nil {
		title = e.label()
	}
	t.tab.SetTitle(title)
}

// changed shows what changed: the busy mark and a new build of the tab.
func (t *sessionTab) changed() {
	t.tab.SetBusy(t.busy)
	t.tab.SetDirty(t.waiting()) // the dot of a tab that waits for the user
	t.tab.Refresh()
	t.ext.refreshStatus()
}

// close ends the tab: the program is told to end and the tab forgets itself. The conversation is kept in the index.
func (t *sessionTab) close() {
	if t.closed {
		return
	}
	t.closed = true
	a := t.ext
	for i, x := range a.tabs {
		if x == t {
			a.tabs = append(a.tabs[:i:i], a.tabs[i+1:]...)
			break
		}
	}
	if a.active == t {
		a.active = nil
		if n := len(a.tabs); n > 0 {
			a.active = a.tabs[n-1]
		}
	}
	if !t.forget {
		a.remember(t)
	}
	t.stopProgram()
	t.input.Dispose()
	t.pending = nil
	a.refreshStatus()
}

// stopProgram ends the program of the tab, if there is one, without waiting: a goroutine closes the session (its standard
// input first, then the signals) and the extension can wait for it.
func (t *sessionTab) stopProgram() {
	t.gen++ // whatever the old program says now is not for this tab
	for _, p := range slices.Clone(t.perms) {
		t.withdraw(p) // the requests of the program that ends are not answered
	}
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	s := t.sess
	t.sess = nil
	if s == nil {
		return
	}
	a := t.ext
	a.closers.Add(1)
	go func() {
		defer a.closers.Done()
		_ = s.Close() // a program that ended already: nothing more to do
	}()
}

// ---- starting the program ----

// ensureStarted starts the program unless it runs or is starting. It checks first that the program exists, then (in a
// goroutine) that the user is logged in, then does the handshake.
func (t *sessionTab) ensureStarted() {
	if t.closed || t.replay || t.phase == phaseStarting || t.phase == phaseReady {
		return
	}
	a := t.ext
	if a.api.Scope().Folder == "" {
		t.setProblem(phaseFailed, "Open a folder first: a session runs in the folder of the workspace.")
		return
	}
	adapter := a.adapter()
	if _, err := adapter.Find(); err != nil {
		t.setProblem(phaseMissing, "")
		return
	}
	t.phase, t.problem = phaseStarting, ""
	t.gen++
	gen := t.gen
	ctx, cancel := context.WithCancel(a.ctx)
	t.cancel = cancel
	opts := claude.SessionOptions{
		PermissionMode: t.mode, Model: t.model,
		OnEvent: func(ev claude.Event) { t.push(gen, ev) },
		CanUseTool: func(ctx context.Context, req claude.ToolRequest) (claude.Decision, error) {
			return t.canUseTool(ctx, gen, req)
		},
		Stderr: func(line string) {
			a.api.Log().Debug("agent stderr", "session", t.id, "line", line)
			a.logf("[%s] stderr: %s", shortID(t.id), line)
		},
	}
	if t.used {
		opts.Resume = t.id
	} else {
		opts.SessionID = t.id
	}
	t.changed()
	go func() {
		if a.loginState.Load() != loginIn {
			info, err := adapter.AuthStatus(ctx)
			switch {
			case err != nil:
				a.api.Post(func() { t.startFailed(gen, nil, err) })
				return
			case !info.LoggedIn:
				a.loginState.Store(loginOut)
				a.api.Post(func() { t.notLoggedIn(gen) })
				return
			}
			a.loginState.Store(loginIn)
		}
		sess, err := adapter.Start(ctx, opts)
		a.api.Post(func() { t.started(gen, sess, err) })
	}()
}

// setProblem puts the tab in a phase that shows a banner.
func (t *sessionTab) setProblem(p phase, text string) {
	t.phase, t.problem = p, text
	t.changed()
}

// notLoggedIn is the answer of the check of the login, when the user is not.
func (t *sessionTab) notLoggedIn(gen int) {
	if t.closed || gen != t.gen {
		return
	}
	t.setProblem(phaseLoggedOut, "")
}

// startFailed handles a program that could not be started (or whose login could not be checked).
func (t *sessionTab) startFailed(gen int, sess *claude.Session, err error) {
	if sess != nil {
		go func() { _ = sess.Close() }()
	}
	if t.closed || gen != t.gen {
		return
	}
	t.ext.api.Log().Warn("the agent did not start", "session", t.id, "err", err)
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ProgramNotFound" {
		t.setProblem(phaseMissing, "")
		return
	}
	t.ext.loginState.Store(loginUnknown)
	t.setProblem(phaseFailed, "The program could not be started: "+errorText(err))
}

// errorText is the message of an error with the last lines of the standard error of the program, when the error has them.
func errorText(err error) string {
	text := err.Error()
	if appErr, ok := errors.FromError(err); ok {
		if tail, ok := appErr.Metadata["stderr"].(string); ok && strings.TrimSpace(tail) != "" {
			lines := strings.Split(strings.TrimSpace(tail), "\n")
			if len(lines) > 5 {
				lines = lines[len(lines)-5:]
			}
			text += "\n" + strings.Join(lines, "\n")
		}
	}
	return text
}

// started handles the answer of Start.
func (t *sessionTab) started(gen int, sess *claude.Session, err error) {
	if err != nil {
		t.startFailed(gen, sess, err)
		return
	}
	if t.closed || gen != t.gen {
		a := t.ext
		a.closers.Add(1)
		go func() {
			defer a.closers.Done()
			_ = sess.Close() // the tab was closed while the program started
		}()
		return
	}
	t.sess = sess
	t.phase, t.problem = phaseReady, ""
	hs := sess.Handshake()
	if len(hs.Commands) > 0 {
		t.commands = t.commands[:0]
		for _, c := range hs.Commands {
			t.commands = append(t.commands, commandInfo{Name: c.Name, Description: c.Description, Hint: c.ArgumentHint})
		}
	}
	if len(hs.Models) > 0 {
		t.models = t.models[:0]
		for _, m := range hs.Models {
			t.models = append(t.models, modelInfo{Value: m.Value, Name: m.DisplayName})
		}
	}
	pending := t.pending
	t.pending = nil
	t.sendAll(pending)
	t.changed()
}

// ---- events ----

// push takes an event from the goroutine of the program. The events of a frame are applied together, on the UI thread, with
// one build of the tab at the end.
func (t *sessionTab) push(gen int, ev claude.Event) { t.enqueue(queued{gen: gen, ev: ev}) }

// enqueue puts an entry in the queue of the frame.
func (t *sessionTab) enqueue(q queued) {
	t.qmu.Lock()
	t.queue = append(t.queue, q)
	post := !t.flushPosted
	t.flushPosted = true
	t.qmu.Unlock()
	if post {
		t.ext.api.Post(t.flush)
	}
}

// flush applies the events that came since the last frame.
func (t *sessionTab) flush() {
	t.qmu.Lock()
	batch := t.queue
	t.queue = nil
	t.flushPosted = false
	t.qmu.Unlock()
	if t.closed || len(batch) == 0 {
		return
	}
	t.stick = t.atEnd()
	for _, q := range batch {
		switch {
		case q.gen != t.gen:
		case q.ev == nil:
			t.tr.add(&item{Kind: kindNotice, Text: q.notice})
		default:
			t.apply(q.ev)
		}
	}
	t.changed()
}

// atEnd tells whether the conversation is scrolled to its end (or does not scroll).
func (t *sessionTab) atEnd() bool {
	_, top := t.scroll.Position()
	_, max := t.scroll.MaxPosition()
	return max-top < 8
}

// apply takes one event of the program into the conversation.
func (t *sessionTab) apply(ev claude.Event) {
	switch m := ev.(type) {
	case *claude.InitMessage:
		t.running = m.Model
		if len(t.commands) == 0 {
			for _, name := range m.SlashCommands {
				t.commands = append(t.commands, commandInfo{Name: name})
			}
		}
	case *claude.StreamEvent:
		if m.ParentToolUseID == "" {
			t.tr.partials.Apply(m)
		}
	case *claude.AssistantMessage:
		t.tr.assistant(m)
		if m.Error != "" {
			t.assistantError(m.Error)
		}
	case *claude.UserMessage:
		if m.IsReplay && t.replay {
			// a recording: the echo of a message of the user is the message
			if text := m.Text(); text != "" {
				t.tr.add(&item{Kind: kindUser, Text: text, ID: m.UUID})
				t.noteTitle(text)
			}
			return
		}
		t.tr.results(m)
	case *claude.ResultMessage:
		t.result(m)
	case *claude.ProtocolErrorEvent:
		t.ext.api.Log().Warn("a line of the agent could not be used", "session", t.id, "err", m.Err)
	case *claude.ExitEvent:
		t.exited(m)
	}
}

// assistantError shows an error that the program reports on a message of the agent. An authentication error is the login.
func (t *sessionTab) assistantError(kind string) {
	if kind == "authentication_failed" {
		t.ext.loginState.Store(loginOut)
		t.phase = phaseLoggedOut
		t.tr.add(&item{Kind: kindError, Text: "The agent is not logged in."})
		return
	}
	t.tr.add(&item{Kind: kindError, Text: "The agent reported an error: " + kind})
}

// result ends a turn.
func (t *sessionTab) result(m *claude.ResultMessage) {
	if t.turns > 0 {
		t.turns--
	}
	if m.IsError {
		switch {
		case t.interrupting:
			t.tr.add(&item{Kind: kindNotice, Text: "Stopped."})
		case len(m.Errors) > 0:
			t.tr.add(&item{Kind: kindError, Text: strings.Join(m.Errors, "\n")})
		case m.Result != "":
			t.tr.add(&item{Kind: kindError, Text: m.Result})
		default:
			t.tr.add(&item{Kind: kindError, Text: "The turn failed (" + m.Subtype + ")."})
		}
	}
	stopped := t.interrupting
	t.interrupting = false
	t.busy = t.turns > 0
	if !t.busy {
		t.tr.partials.Reset()
		t.tr.finishTools()
		if !stopped {
			t.notifyDone(m.IsError)
		}
	}
	t.ext.touch(t)
	t.ext.remember(t)
	t.ext.sync()
}

// exited handles the end of the program.
func (t *sessionTab) exited(m *claude.ExitEvent) {
	t.sess = nil
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	if t.phase == phaseReady || t.phase == phaseStarting {
		t.phase = phaseIdle
	}
	t.turns, t.busy, t.interrupting = 0, false, false
	t.tr.partials.Reset()
	t.tr.finishTools()
	if m.Err != nil {
		t.tr.add(&item{Kind: kindError, Text: "The program ended unexpectedly: " + errorText(m.Err)})
	}
}

// ---- sending ----

// submit sends the text of the input as a message.
func (t *sessionTab) submit(text string) {
	text = strings.TrimSpace(text)
	if text == "" || t.closed {
		return
	}
	switch t.phase {
	case phaseMissing, phaseLoggedOut:
		return // the banner says what to do
	case phaseFailed:
		t.phase, t.problem = phaseIdle, ""
	}
	t.stick = true
	t.tr.add(&item{Kind: kindUser, Text: text})
	t.input.SetValue("")
	t.comp = completion{}
	t.noteTitle(text)
	t.ext.sync()
	payload := text + t.ext.activeFileNote(t.ext.ctx, text)
	t.turns++
	t.busy = true
	if t.phase == phaseReady && t.sess != nil {
		t.sendAll([]string{payload})
	} else {
		t.pending = append(t.pending, payload)
		t.ensureStarted()
	}
	t.changed()
}

// noteTitle records the session in the index, with the first line of its first message as its title.
func (t *sessionTab) noteTitle(text string) {
	e := t.ext.touch(t)
	if e.Title == "" {
		first, _, _ := strings.Cut(text, "\n")
		e.Title = clip(strings.TrimSpace(first), 120)
		t.retitle()
	}
}

// sendAll writes messages to the program, in order, in a goroutine of their own so that the UI never waits for the pipe.
func (t *sessionTab) sendAll(payloads []string) {
	if len(payloads) == 0 || t.sess == nil {
		return
	}
	sess, gen := t.sess, t.gen
	a := t.ext
	t.used = true
	go func() {
		for _, p := range payloads {
			uuid, err := sess.Send(a.ctx, p)
			a.api.Post(func() {
				if t.closed || gen != t.gen {
					return
				}
				if err != nil {
					t.turns = max(t.turns-1, 0)
					t.busy = t.turns > 0
					t.tr.add(&item{Kind: kindError, Text: "The message was not sent: " + errorText(err)})
					t.changed()
					return
				}
				t.setUUID(uuid)
			})
			if err != nil {
				return
			}
		}
	}()
}

// setUUID gives the oldest user message that has none the UUID the program echoes (what a rewind takes in step 13.3).
func (t *sessionTab) setUUID(uuid string) {
	for _, it := range t.tr.items {
		if it.Kind == kindUser && it.ID == "" {
			it.ID = uuid
			return
		}
	}
}

// stop interrupts the turn that runs.
func (t *sessionTab) stop() {
	if t.sess == nil || !t.busy || t.interrupting {
		return
	}
	t.interrupting = true
	sess, a := t.sess, t.ext
	go func() {
		if err := sess.Interrupt(a.ctx); err != nil {
			a.api.Post(func() {
				if t.closed {
					return
				}
				t.interrupting = false
				t.tr.add(&item{Kind: kindError, Text: "The agent could not be stopped: " + errorText(err)})
				t.changed()
			})
		}
	}()
}

// chooseModel switches to a model; "" is the default model.
func (t *sessionTab) chooseModel(value string) {
	if value == t.model {
		return
	}
	t.model = value
	t.ext.touch(t)
	if sess := t.sess; sess != nil {
		a := t.ext
		go func() {
			if err := sess.SetModel(a.ctx, value); err != nil {
				a.api.Post(func() { t.problemNote("The model was not changed: " + errorText(err)) })
			}
		}()
	}
	t.changed()
}

// chooseMode switches the permission mode.
func (t *sessionTab) chooseMode(mode claude.PermissionMode) {
	if mode == t.mode || !mode.Valid() {
		return
	}
	t.mode = mode
	t.ext.touch(t)
	if sess := t.sess; sess != nil {
		a := t.ext
		go func() {
			if err := sess.SetPermissionMode(a.ctx, mode); err != nil {
				a.api.Post(func() { t.problemNote("The permission mode was not changed: " + errorText(err)) })
			}
		}()
	}
	t.changed()
}

// problemNote adds an error line to the conversation.
func (t *sessionTab) problemNote(text string) {
	if t.closed {
		return
	}
	t.tr.add(&item{Kind: kindError, Text: text})
	t.changed()
}

// retry starts the program again after a failure, a missing program or a login.
func (t *sessionTab) retry() {
	if t.phase == phaseMissing || t.phase == phaseFailed || t.phase == phaseLoggedOut {
		t.phase, t.problem = phaseIdle, ""
	}
	t.ensureStarted()
	t.changed()
}
