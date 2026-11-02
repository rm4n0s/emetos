package app

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// The terminal of a runInTerminal request (SPEC §10.31 "The session", step 25.11). A session that asks to run its program in a terminal
// gets the terminal of an earlier session that has ended, when there is one still open, and a new terminal otherwise (VS Code's reuse of
// its integrated terminals). A reused terminal gets Ctrl+C, so that whatever is left in it stops, and, with debug.terminal.clearBeforeReusing,
// the command clear; then the command line. The terminals are those of the Terminal extension, asked through its requests (SPEC §10.25
// "Events and requests"); their topics are written here, as the extensions do, so that the packages stay apart.
//
// Ported from VS Code: src/vs/workbench/api/node/extHostDebugService.ts ($runInTerminal)

// The requests of the Terminal extension.
const (
	terminalListTopic  = "emetos.terminal.list"
	terminalRunTopic   = "emetos.terminal.run"
	terminalWriteTopic = "emetos.terminal.write"
	// debugTerminalTimeout bounds a request to the Terminal extension.
	debugTerminalTimeout = 10 * time.Second
	// debugTerminalSettle is the time a terminal is given to take Ctrl+C and clear before the command comes (VS Code waits 200 ms).
	debugTerminalSettle = 200 * time.Millisecond
)

// debugTerminal is a terminal a debug session ran its program in.
type debugTerminal struct {
	id int
	// session is the id of the session that has the terminal; empty when the session ended and the terminal waits for the next.
	session string
}

// debugTerminals are the terminals of the sessions of a workspace.
type debugTerminals struct {
	mu    sync.Mutex
	items []*debugTerminal
}

// terminalListReply and the rest are the parts of the replies of the Terminal extension that are read here.
type terminalListReply struct {
	Terminals []struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Exited bool   `json:"exited"`
	} `json:"terminals"`
}

// askTerminal sends a request to the Terminal extension and waits for the reply, decoded into reply when it is not nil. It runs on a
// goroutine, never on the UI thread.
func (w *Workspace) askTerminal(ctx context.Context, topic string, req, reply any) error {
	scope := w.Extensions()
	if scope == nil {
		return errors.New("DebugTerminalNoScope", "this window runs no extensions, so it has no terminal", "topic", topic)
	}
	type answer struct {
		ev  extension.Event
		err error
	}
	ch := make(chan answer, 1)
	rctx, cancel := context.WithTimeout(ctx, debugTerminalTimeout)
	defer cancel()
	if err := scope.Ask(rctx, topic, req, func(ev extension.Event, err error) { ch <- answer{ev, err} }); err != nil {
		return err
	}
	select {
	case a := <-ch:
		if a.err != nil {
			return a.err
		}
		if reply == nil {
			return nil
		}
		return a.ev.Decode(reply)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// checkoutDebugTerminal gives a terminal of an ended session to the session, when one is still open and its program has ended. The
// terminals the user closed are forgotten.
func (w *Workspace) checkoutDebugTerminal(ctx context.Context, sessionID string) (int, bool) {
	var list terminalListReply
	if err := w.askTerminal(ctx, terminalListTopic, nil, &list); err != nil {

		return 0, false
	}
	alive := map[int]bool{}
	for _, t := range list.Terminals {
		alive[t.ID] = !t.Exited
	}
	w.debugTerms.mu.Lock()
	defer w.debugTerms.mu.Unlock()
	w.debugTerms.items = slices.DeleteFunc(w.debugTerms.items, func(t *debugTerminal) bool { return !alive[t.id] })
	for _, t := range w.debugTerms.items {
		if t.session == "" {
			t.session = sessionID
			return t.id, true
		}
	}
	return 0, false
}

// rememberDebugTerminal finds the terminal that was just made (the newest with the name) and keeps it for the session.
func (w *Workspace) rememberDebugTerminal(ctx context.Context, name, sessionID string) {
	var list terminalListReply
	if err := w.askTerminal(ctx, terminalListTopic, nil, &list); err != nil {

		w.Process.Logger.Debug("the terminal of a debug session cannot be found", "err", err)
		return
	}
	id, found := 0, false
	for _, t := range list.Terminals {
		if t.Name == name && !t.Exited && (!found || t.ID > id) {
			id, found = t.ID, true
		}
	}
	if !found {
		return
	}
	w.debugTerms.mu.Lock()
	defer w.debugTerms.mu.Unlock()
	if !slices.ContainsFunc(w.debugTerms.items, func(t *debugTerminal) bool { return t.id == id }) {
		w.debugTerms.items = append(w.debugTerms.items, &debugTerminal{id: id, session: sessionID})
	}
}

// freeDebugTerminals lets the terminals of a session that ended wait for the next one.
func (w *Workspace) freeDebugTerminals(sessionID string) {
	w.debugTerms.mu.Lock()
	defer w.debugTerms.mu.Unlock()
	for _, t := range w.debugTerms.items {
		if t.session == sessionID {
			t.session = ""
		}
	}
}

// forgetDebugTerminal drops a terminal that could not be used.
func (w *Workspace) forgetDebugTerminal(id int) {
	w.debugTerms.mu.Lock()
	defer w.debugTerms.mu.Unlock()
	w.debugTerms.items = slices.DeleteFunc(w.debugTerms.items, func(t *debugTerminal) bool { return t.id == id })
}

// followDebugTerminals frees the terminals of a session when it ends for good (not for a restart, which keeps its terminal).
func (w *Workspace) followDebugTerminals(svc *debug.Service) func() {
	return svc.Subscribe(func(ev debug.Event) {
		if ev.Kind == debug.EventSessionEnded && !ev.Restart && ev.Session != nil {
			w.freeDebugTerminals(ev.Session.ID())
		}
	})
}

// waitForTerminal waits for a terminal to take what was sent, or for ctx.
func waitForTerminal(ctx context.Context) error {
	select {
	case <-time.After(debugTerminalSettle):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runInReusedTerminal runs the command line in a terminal of an earlier session: Ctrl+C, the clear command when
// debug.terminal.clearBeforeReusing says so, and the line (after a cd to the folder of the request, because the shell was left wherever
// the earlier program left it).
func (w *Workspace) runInReusedTerminal(ctx context.Context, id int, args dap.RunInTerminalRequestArguments) error {
	if err := w.askTerminal(ctx, terminalWriteTopic, map[string]any{"id": id, "data": "\x03"}, nil); err != nil {
		return err
	}
	if err := waitForTerminal(ctx); err != nil {
		return err
	}
	if w.debugClearBeforeReusing() {
		if err := w.askTerminal(ctx, terminalRunTopic, map[string]any{"id": id, "text": "clear"}, nil); err != nil {
			return err
		}
		if err := waitForTerminal(ctx); err != nil {
			return err
		}
	}
	line := commandLine(args)
	if args.Cwd != "" {
		line = "cd " + shellQuote(args.Cwd) + " && " + line
	}
	return w.askTerminal(ctx, terminalRunTopic, map[string]any{"id": id, "text": line}, nil)
}

// debugClearBeforeReusing is debug.terminal.clearBeforeReusing.
func (w *Workspace) debugClearBeforeReusing() bool {
	return settings.Get[bool](w.Settings, "debug.terminal.clearBeforeReusing", "")
}

// debugRunInTerminal runs the program of a runInTerminal request in a terminal of the Terminal extension: the terminal of an ended session
// when there is one, else a new terminal in the folder with the command line typed into it. The host's terminal is the one that opens
// (D51), whatever screen asked. The process id of its shell is not known here: 0 (decision 1191).
func (w *Workspace) debugRunInTerminal(ctx context.Context, args dap.RunInTerminalRequestArguments, sessionID string) (int, error) {
	host := w
	if w.isView() {
		host = w.viewOf
	}
	if id, ok := host.checkoutDebugTerminal(ctx, sessionID); ok {
		err := host.runInReusedTerminal(ctx, id, args)
		if err == nil {
			return 0, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		host.Process.Logger.Warn("a terminal of an earlier debug session cannot be used again; a new one opens", "err", err)
		host.forgetDebugTerminal(id)
	}
	name := args.Title
	if name == "" {
		name = "Debug"
	}
	_, err := uiValue(ctx, host, func() error {
		cc := command.Context{Ctx: ctx, Keys: host.Keys, Source: command.SourceAPI}
		if err := host.Commands.Run(cc, "workbench.action.terminal.new", map[string]any{"cwd": args.Cwd, "name": name}); err != nil {
			return err
		}
		return host.Commands.Run(cc, "workbench.action.terminal.sendSequence", map[string]any{"text": commandLine(args) + "\n"})
	})
	if err != nil {
		return 0, err
	}
	host.rememberDebugTerminal(ctx, name, sessionID)
	return 0, nil
}
