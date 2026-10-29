package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The terminal as events and requests (SPEC §10.25 "Events and requests"), for any extension. The
// events are published for every terminal; the requests are answered on the UI thread. Positions and sizes are in cells.

// The topics of the extension.
const (
	// TopicCreated: a terminal was made. TopicTitle: a program set its title. TopicOutput: the text a program wrote (a piece,
	// one at most every 50 ms or 64 KiB). TopicInput: what was typed (who typed it is in `by`). TopicResized, TopicExited (the
	// program ended; the terminal stays until it is closed) and TopicClosed.
	TopicCreated = "emetos.terminal.created"
	TopicTitle   = "emetos.terminal.title"
	TopicOutput  = "emetos.terminal.output"
	TopicInput   = "emetos.terminal.input"
	TopicResized = "emetos.terminal.resized"
	TopicExited  = "emetos.terminal.exited"
	TopicClosed  = "emetos.terminal.closed"
	// TopicList lists the terminals, TopicSnapshot serializes the screen of one, TopicCreate makes one, TopicWrite types bytes
	// in one, TopicRun types a line and Enter in one and TopicKill ends one.
	TopicList     = "emetos.terminal.list"
	TopicSnapshot = "emetos.terminal.snapshot"
	TopicCreate   = "emetos.terminal.create"
	TopicWrite    = "emetos.terminal.write"
	TopicRun      = "emetos.terminal.run"
	TopicKill     = "emetos.terminal.kill"
)

type createdEvent struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
	Cwd     string `json:"cwd"`
	// Location is "editor" for a terminal made in an editor tab; empty for one in the panel.
	Location string `json:"location,omitempty"`
}

type titleEvent struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type outputEvent struct {
	ID   int    `json:"id"`
	Seq  uint64 `json:"seq"`
	Data string `json:"data"`
}

type inputEvent struct {
	ID   int    `json:"id"`
	Data string `json:"data"`
	By   string `json:"by"`
	// Hidden says that the terminal did not echo what was typed (a password prompt), so Data is left out.
	Hidden bool `json:"hidden,omitempty"`
}

type resizedEvent struct {
	ID   int `json:"id"`
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type exitedEvent struct {
	ID   int `json:"id"`
	Code int `json:"code"`
}

type closedEvent struct {
	ID int `json:"id"`
}

type listReply struct {
	Terminals []listItem `json:"terminals"`
}

type listItem struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
	Exited bool   `json:"exited"`
	// Location is "editor" for a terminal in an editor tab; empty for one in the panel.
	Location string `json:"location,omitempty"`
}

type idRequest struct {
	ID int `json:"id"`
}

type snapshotReply struct {
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Seq  uint64 `json:"seq"`
	Data string `json:"data"`
}

type createRequest struct {
	Profile string `json:"profile"`
	Cwd     string `json:"cwd"`
	Name    string `json:"name"`
	// Show makes the terminal the active one, shows the panel and gives it the keyboard, as the person at the computer opening a
	// terminal does (Open Shell of the Containers extension).
	Show bool `json:"show"`
}

type createReply struct {
	ID int `json:"id"`
}

type writeRequest struct {
	ID   int    `json:"id"`
	Data string `json:"data"`
}

type runRequest struct {
	ID   *int   `json:"id"`
	Text string `json:"text"`
}

type killRequest struct {
	ID int `json:"id"`
}

// publish publishes an event of the extension; an extension that is gone publishes nothing.
func (t *terminals) publish(topic string, v any) {
	if err := t.api.Events().Publish(topic, v); err != nil {
		t.api.Log().Debug("a terminal event was not published", "topic", topic, "err", err)
	}
}

// observerFor is what a terminal tells the event bus: its output, the input, its size and its title. Its functions run on the
// goroutines of the terminal and only publish.
func (t *terminals) observerFor(id int) *observer {
	return &observer{
		output: func(seq uint64, data string) { t.publish(TopicOutput, outputEvent{ID: id, Seq: seq, Data: data}) },
		input: func(data, by string, hidden bool) {
			t.publish(TopicInput, inputEvent{ID: id, Data: data, By: by, Hidden: hidden})
		},
		resized: func(cols, rows int) { t.publish(TopicResized, resizedEvent{ID: id, Cols: cols, Rows: rows}) },
		title:   func(title string) { t.publish(TopicTitle, titleEvent{ID: id, Title: title}) },
	}
}

// registerRequests sets the handlers of the requests.
func (t *terminals) registerRequests() error {
	handlers := map[string]extension.EventHandler{
		TopicList: t.handleList, TopicSnapshot: t.handleSnapshot, TopicCreate: t.handleCreate, TopicWrite: t.handleWrite,
		TopicRun: t.handleRun, TopicKill: t.handleKill,
	}
	for topic, h := range handlers {
		if err := t.api.Events().Handle(topic, h); err != nil {
			return err
		}
	}
	return nil
}

// decodeRequest reads the payload of a request into a T; a payload that is not a JSON object of the request's type is
// TerminalPayloadInvalid. An empty payload or null is the empty request.
func decodeRequest[T any](ev extension.Event) (T, error) {
	var v T
	data := bytes.TrimSpace(ev.Data)
	if len(data) == 0 || string(data) == "null" {
		return v, nil
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, errors.NewErr("TerminalPayloadInvalid", err).SetMetadata("topic", ev.Topic)
	}
	return v, nil
}

// entryByID finds a terminal by its number; nil when there is none.
func (t *terminals) entryByID(id int) *entry {
	for _, e := range t.all() {
		if e.term.ID() == id {
			return e
		}
	}
	return nil
}

// handleList answers emetos.terminal.list.
func (t *terminals) handleList(_ context.Context, _ extension.Event) (any, error) {
	out := listReply{Terminals: []listItem{}}
	for _, e := range t.all() {
		cols, rows := e.term.Size()
		_, exited := e.term.ExitCode()
		item := listItem{ID: e.term.ID(), Name: t.nameOf(e), Title: t.title(e), Cols: cols, Rows: rows, Exited: exited}
		if e.inEditor() {
			item.Location = locEditor
		}
		out.Terminals = append(out.Terminals, item)
	}
	slices.SortFunc(out.Terminals, func(a, b listItem) int { return a.ID - b.ID })
	return out, nil
}

// nameOf is the name of a terminal: the one the user gave it, else the name of its profile.
func (t *terminals) nameOf(e *entry) string {
	if n := e.term.Name(); n != "" {
		return n
	}
	return e.term.Profile().name
}

// handleSnapshot answers emetos.terminal.snapshot.
func (t *terminals) handleSnapshot(_ context.Context, ev extension.Event) (any, error) {
	req, err := decodeRequest[idRequest](ev)
	if err != nil {
		return nil, err
	}
	e := t.entryByID(req.ID)
	if e == nil {
		return nil, errors.New("TerminalNotFound", "there is no terminal with this number", "id", req.ID)
	}
	data, cols, rows, seq := e.term.Snapshot()
	return snapshotReply{Cols: cols, Rows: rows, Seq: seq, Data: string(data)}, nil
}

// handleCreate answers emetos.terminal.create: a terminal in the profile and the folder asked for, which is a folder under the
// folder of the workspace; it is made without taking the keyboard or the panel from the user, unless the requester asks for `show`.
func (t *terminals) handleCreate(_ context.Context, ev extension.Event) (any, error) {
	req, err := decodeRequest[createRequest](ev)
	if err != nil {
		return nil, err
	}
	o := openOpts{name: req.Name, quiet: !req.Show}
	if req.Profile != "" {
		if o.profile = t.profileNamed(req.Profile); o.profile == nil {
			return nil, errors.New("TerminalProfileUnknown", "there is no such profile", "profile", req.Profile)
		}
	}
	if req.Cwd != "" {
		if t.folder == "" {
			return nil, errors.New("TerminalNoFolder", "a folder of a terminal is relative to the folder of the workspace, and this one has none", "cwd", req.Cwd)
		}
		if o.dir, err = t.confinedDir(req.Cwd); err != nil {
			return nil, err
		}
	}
	e, err := t.open(o)
	if err != nil {
		return nil, err // the error of the pseudo-terminal, with its own tag and route
	}
	return createReply{ID: e.term.ID()}, nil
}

// confinedDir turns a folder relative to the folder of the workspace into the folder to start a terminal in. It fails with
// TerminalCwdInvalid (an absolute path, an element .., a NUL or a path over 4096 bytes), TerminalCwdOutside (a link that leads
// out), TerminalCwdNotFound and TerminalCwdNotDirectory. The folder is opened as a root (SPEC §10.19 "Confinement"), so no
// link inside it can lead outside.
func (t *terminals) confinedDir(rel string) (string, error) {
	if len(rel) > 4096 || strings.ContainsRune(rel, 0) || filepath.IsAbs(rel) || slices.Contains(strings.Split(filepath.ToSlash(rel), "/"), "..") {
		return "", errors.New("TerminalCwdInvalid", "a folder is relative to the folder of the workspace, with no .. in it", "cwd", rel)
	}
	root, err := os.OpenRoot(t.folder)
	if err != nil {
		return "", errors.NewErr("TerminalCwdNotFound", err).SetMetadata("cwd", rel)
	}
	defer func() { _ = root.Close() }()
	info, err := root.Stat(filepath.FromSlash(rel))
	switch {
	case err != nil && strings.Contains(err.Error(), "path escapes from parent"):
		return "", errors.NewErr("TerminalCwdOutside", err).SetMetadata("cwd", rel)
	case err != nil:
		return "", errors.NewErr("TerminalCwdNotFound", err).SetMetadata("cwd", rel)
	case !info.IsDir():
		return "", errors.New("TerminalCwdNotDirectory", "this is not a folder", "cwd", rel)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(t.folder, filepath.FromSlash(rel)))
	if err != nil {
		return "", errors.NewErr("TerminalCwdNotFound", err).SetMetadata("cwd", rel)
	}
	if real != t.folder && !strings.HasPrefix(real, t.folder+string(filepath.Separator)) {
		return "", errors.New("TerminalCwdOutside", "the folder is outside the workspace", "cwd", rel)
	}
	return real, nil
}

// handleWrite answers emetos.terminal.write: bytes for the terminal, as typed.
func (t *terminals) handleWrite(_ context.Context, ev extension.Event) (any, error) {
	req, err := decodeRequest[writeRequest](ev)
	if err != nil {
		return nil, err
	}
	e := t.entryByID(req.ID)
	if e == nil {
		return nil, errors.New("TerminalNotFound", "there is no terminal with this number", "id", req.ID)
	}
	if _, exited := e.term.ExitCode(); exited {
		return nil, errors.New("TerminalExited", "the program of this terminal has ended", "id", req.ID)
	}
	if e.term.pendingInput() > maxPendingInput {
		return nil, errors.New("TerminalInputFull", "the program does not read its terminal; wait", "id", req.ID)
	}
	e.term.Type([]byte(req.Data), ev.Publisher)
	return struct{}{}, nil
}

// handleRun answers emetos.terminal.run: the text and Enter, in the terminal asked for or the active one, or in a new one when
// there is none (VS Code's sendText).
func (t *terminals) handleRun(_ context.Context, ev extension.Event) (any, error) {
	req, err := decodeRequest[runRequest](ev)
	if err != nil {
		return nil, err
	}
	var e *entry
	if req.ID != nil {
		if e = t.entryByID(*req.ID); e == nil {
			return nil, errors.New("TerminalNotFound", "there is no terminal with this number", "id", *req.ID)
		}
	} else if e = t.activeEntry(); e == nil {
		if e, err = t.open(openOpts{quiet: true}); err != nil {
			return nil, err
		}
	}
	if _, exited := e.term.ExitCode(); exited {
		return nil, errors.New("TerminalExited", "the program of this terminal has ended", "id", e.term.ID())
	}
	if e.term.pendingInput() > maxPendingInput {
		return nil, errors.New("TerminalInputFull", "the program does not read its terminal; wait", "id", e.term.ID())
	}
	text := strings.ReplaceAll(strings.ReplaceAll(req.Text, "\r\n", "\r"), "\n", "\r")
	if !strings.HasSuffix(text, "\r") {
		text += "\r"
	}
	e.term.Type([]byte(text), ev.Publisher)
	return createReply{ID: e.term.ID()}, nil
}

// handleKill answers emetos.terminal.kill.
func (t *terminals) handleKill(_ context.Context, ev extension.Event) (any, error) {
	req, err := decodeRequest[killRequest](ev)
	if err != nil {
		return nil, err
	}
	e := t.entryByID(req.ID)
	if e == nil {
		return nil, errors.New("TerminalNotFound", "there is no terminal with this number", "id", req.ID)
	}
	t.kill(e)
	return struct{}{}, nil
}
