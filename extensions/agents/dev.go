package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strconv"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// registerDev registers the dev command of the proofs, in debug builds and builds with the emetos_dev tag only (SPEC §15.2).
func (a *agents) registerDev() error {
	if !foundation.DebugMode && !devBuild {
		return nil
	}
	return a.api.Commands().Register(extension.CommandSpec{
		ID:          ReplayCommand,
		Description: `Play a recorded stream of the agent's program into a session tab, with no program. Arguments: {"fixture": path}.`,
	}, a.replayCommand)
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

// parseFixture reads a recorded stream: a message per line; empty lines, the directives of the fake program (objects with a
// "$" key) and control messages are skipped. A line that is not a message fails with ReplayFixtureInvalid.
func parseFixture(data []byte) ([]claude.Message, error) {
	var out []claude.Message
	for i, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(line, &probe); err != nil {
			return nil, errors.NewErr("ReplayFixtureInvalid", err).SetMetadata("line", strconv.Itoa(i+1))
		}
		if _, directive := probe["$"]; directive {
			continue
		}
		msg, err := claude.ParseLine(line)
		if err != nil {
			return nil, err // the adapter's error says what is wrong with the message
		}
		if _, unknown := msg.(*claude.UnknownMessage); unknown {
			continue
		}
		out = append(out, msg)
	}
	return out, nil
}

// replayEvents plays recorded messages into the tab. The program of the tab, if one is starting or runs, is ended: the tab
// shows the recording and nothing else.
func (t *sessionTab) replayEvents(events []claude.Message) {
	t.stopProgram()
	t.replay = true
	t.phase, t.problem = phaseReady, ""
	t.turns = 0
	for _, ev := range events {
		t.apply(ev)
	}
	t.busy = false
	t.stick = true
	t.ext.sync()
	t.changed()
}
