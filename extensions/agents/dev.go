package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// registerDev registers the dev command of the proofs, in debug builds and builds with the emetos_dev tag only (SPEC §15.2).
func (a *agents) registerDev() error {
	if !foundation.DebugMode && !devBuild {
		return nil
	}
	if err := a.api.Commands().Register(extension.CommandSpec{
		ID:          ReplayCommand,
		Description: `Play a recorded stream of the agent's program into a session tab, with no program. Arguments: {"fixture": path}.`,
	}, a.replayCommand); err != nil {
		return err
	}
	return a.api.Commands().Register(extension.CommandSpec{
		ID:          SendCommand,
		Description: `Send a message in the session tab in front, as the Send button does (a program starts if the session has none). Arguments: {"text": message}.`,
	}, a.sendCommand)
}

// sendCommand is dev.agents.send {"text": message}: the text is sent as a message in the session in front, the same way the
// input box sends it, so a proof can drive a real session with the fake program (internal/fakeclaude) and show its conversation.
func (a *agents) sendCommand(_ context.Context, args any) error {
	m, _ := args.(map[string]any)
	text, _ := m["text"].(string)
	if strings.TrimSpace(text) == "" {
		return errors.New("SendTextInvalid", `dev.agents.send needs {"text": message}`)
	}
	t := a.target()
	if t == nil {
		return errors.New("SendNoSession", "there is no session tab to send the message in")
	}
	t.submit(text)
	return nil
}

// replayCommand is dev.agents.replay {"fixture": path}: the lines of the file (the output of the program, one JSON message per
// line; the directives of the fake program are skipped) are played into the session in front, or into a new one. The path is
// relative to the folder Emetos runs in. No program is started; the tab shows the conversation as if the program had said it.
func (a *agents) replayCommand(_ context.Context, args any) error {
	m, _ := args.(map[string]any)
	path, _ := m["fixture"].(string)
	if path == "" {
		return errors.New("ReplayFixtureInvalid", `dev.agents.replay needs {"fixture": path}`)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.NewErr("ReplayFixtureUnreadable", err).SetMetadata("path", path)
	}
	events, err := parseFixture(data)
	if err != nil {
		return err
	}
	t := a.target()
	if t == nil {
		tab, err := a.openNew(0)
		if err != nil {
			return err
		}
		for _, x := range a.tabs {
			if x.tab == tab {
				t = x
			}
		}
	}
	if t == nil {
		return errors.New("ReplayNoSession", "there is no session tab to play the stream into")
	}
	t.replayEvents(events)
	return nil
}

// fixtureEntry is one line of a recorded stream: a message of the program, or a permission request of it.
type fixtureEntry struct {
	msg claude.Message
	req *claude.ToolRequest
}

// parseFixture reads a recorded stream: a message per line; empty lines, the directives of the fake program (objects with a
// "$" key, except "ask", which is a permission request) and control messages (except can_use_tool requests, which are shown
// as the request of a real program is) are skipped. A line that is not a message fails with ReplayFixtureInvalid.
func parseFixture(data []byte) ([]fixtureEntry, error) {
	var out []fixtureEntry
	for i, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(line, &probe); err != nil {
			return nil, errors.NewErr("ReplayFixtureInvalid", err).SetMetadata("line", strconv.Itoa(i+1))
		}
		if d, directive := probe["$"]; directive {
			if string(d) != `"ask"` {
				continue
			}
			// the directive of the fake program that sends a request: the same request as a line of the stream
			asked, err := json.Marshal(map[string]any{"type": "control_request", "request_id": string(probe["id"]), "request": probe["request"]})
			if err != nil {
				return nil, errors.NewErr("ReplayFixtureInvalid", err).SetMetadata("line", strconv.Itoa(i+1))
			}
			line = asked
		}
		req, ok, err := claude.ParseToolRequest(line)
		if err != nil {
			return nil, err // the adapter's error says what is wrong with the request
		}
		if ok {
			out = append(out, fixtureEntry{req: &req})
			continue
		}
		msg, err := claude.ParseLine(line)
		if err != nil {
			return nil, err // the adapter's error says what is wrong with the message
		}
		if _, unknown := msg.(*claude.UnknownMessage); unknown {
			continue
		}
		out = append(out, fixtureEntry{msg: msg})
	}
	return out, nil
}

// replayEvents plays recorded messages into the tab. The program of the tab, if one is starting or runs, is ended: the tab
// shows the recording and nothing else. A permission request of the recording is shown as a real one is (the diff of an edit,
// the dialog of a tool); the answer is logged and goes nowhere.
func (t *sessionTab) replayEvents(events []fixtureEntry) {
	t.stopProgram()
	t.replay = true
	t.phase, t.problem = phaseReady, ""
	t.turns = 0
	for _, ev := range events {
		if ev.req != nil {
			t.replayRequest(*ev.req)
			continue
		}
		t.apply(ev.msg)
	}
	t.busy = false
	t.stick = true
	t.ext.sync()
	t.changed()
}

// replayRequest shows a permission request of a recording.
func (t *sessionTab) replayRequest(req claude.ToolRequest) {
	p := &permission{kind: classify(req.ToolName), req: req, reply: make(chan claude.Decision, 1), message: ui.NewInputBoxController("")}
	t.addPermission(t.gen, p)
}
