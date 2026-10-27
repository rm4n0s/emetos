package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// DevSocketEnv is the environment variable that names the Unix socket of the dev server (SPEC §15.2, step 17b.1). Where dev
// commands run (a debug build or one with the emetos_dev tag), Emetos listens on that socket for the whole life of the
// process, so that a display test that types through a virtual keyboard can run dev commands, and read the answer of the
// read-only ones, while the application runs.
const DevSocketEnv = "EMETOS_DEV_SOCKET"

// DevRequest is one line the dev server reads: a command id and its argument (null when it has none).
type DevRequest struct {
	// Command is the command id.
	Command string `json:"command"`
	// Args are the arguments of the command.
	Args json.RawMessage `json:"args,omitempty"`
}

// DevResponse is the line the dev server answers with.
type DevResponse struct {
	// OK is true when the command ran without an error.
	OK bool `json:"ok"`
	// Result is what a read-only dev command gave (dev.inputs, dev.editor); null for the others.
	Result json.RawMessage `json:"result,omitempty"`
	// Tag and Error are the tag and the message of the error of the command, or of the request.
	Tag   string `json:"tag,omitempty"`
	Error string `json:"error,omitempty"`
}

// devAnswerKey is the key of the context value that holds where a read-only dev command puts its answer.
type devAnswerKey struct{}

// devAnswer is where the answer of a dev command goes.
type devAnswer struct {
	value any
	set   bool
}

// setDevAnswer gives the answer of the dev command that runs with ctx, when the request came through the dev server; it does
// nothing otherwise (the command ran from a script of EMETOS_DEV_COMMANDS, which has nowhere to read it).
func setDevAnswer(ctx context.Context, v any) {
	if a, ok := ctx.Value(devAnswerKey{}).(*devAnswer); ok {
		a.value, a.set = v, true
	}
}

// startDevServer listens on the socket DevSocketEnv names, when a build that runs dev commands is given one. It runs on the
// UI thread, once the manager is bound to the application; the listener ends with the context of Run. A socket that cannot be
// made is logged and leaves the process without a dev server (the test that wanted one fails on its own).
func (m *Workspaces) startDevServer() {
	path := os.Getenv(DevSocketEnv)
	if path == "" || !DevCommandsEnabled() || m.ctx == nil {
		return
	}
	// a stale file of a process that was killed would make the bind fail
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		m.p.Logger.Warn("the dev socket cannot be replaced", "path", path, "err", err)
		return
	}
	var lc net.ListenConfig
	l, err := lc.Listen(m.ctx, "unix", path)
	if err != nil {
		m.p.Logger.Warn("the dev server cannot listen", "path", path, "err", err)
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		m.p.Logger.Warn("the dev socket cannot be made private", "path", path, "err", err)
		_ = l.Close()
		return
	}
	go func() {
		<-m.ctx.Done()
		_ = l.Close()
	}()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go m.serveDev(conn)
		}
	}()
}

// serveDev answers the requests of one connection, a JSON line each, one at a time.
func (m *Workspaces) serveDev(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	enc := json.NewEncoder(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if encErr := enc.Encode(m.runDevRequest(line)); encErr != nil {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				m.p.Logger.Warn("the dev server cannot read", "err", err)
			}
			return
		}
	}
}

// runDevRequest runs one request on the UI thread, waits for the frames after it (as the steps of EMETOS_DEV_COMMANDS do), and
// makes the response.
func (m *Workspaces) runDevRequest(line []byte) DevResponse {
	var req DevRequest
	if err := json.Unmarshal(line, &req); err != nil || req.Command == "" {
		return DevResponse{Tag: "DevRequestInvalid", Error: "a request is {\"command\": \"<id>\", \"args\": <argument>}"}
	}
	var args any
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return DevResponse{Tag: "DevRequestInvalid", Error: "the arguments are not JSON: " + err.Error()}
		}
	}
	answer := &devAnswer{}
	ctx := context.WithValue(m.ctx, devAnswerKey{}, answer)
	var runErr error
	finished := make(chan struct{})
	m.binding.Post(func() {
		registry := m.p.Commands
		if active := m.Active(); active != nil {
			registry = active.Commands
		}
		runErr = registry.Run(command.Context{Ctx: ctx, Source: command.SourceAPI}, req.Command, args)
		scheduler := m.binding.Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			scheduler.ScheduleFrame()
			scheduler.AddPostFrameCallback(func(time.Duration) { close(finished) })
		})
	})
	select {
	case <-finished:
	case <-m.ctx.Done():
		return DevResponse{Tag: "DevRequestCanceled", Error: "the application is ending"}
	}
	if runErr != nil {
		resp := DevResponse{Error: describe(runErr)}
		if appErr, ok := errors.FromError(runErr); ok {
			resp.Tag = appErr.Tag
		}
		return resp
	}
	resp := DevResponse{OK: true}
	if answer.set {
		raw, err := json.Marshal(answer.value)
		if err != nil {
			return DevResponse{Tag: "DevAnswerInvalid", Error: err.Error()}
		}
		resp.Result = raw
	}
	return resp
}
