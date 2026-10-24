package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// DevCommandsEnv is the environment variable that holds the dev commands (SPEC §15.2).
const DevCommandsEnv = "EMETOS_DEV_COMMANDS"

// DevCommandsEnabled tells whether this build runs dev commands: debug builds and builds
// with the emetos_dev tag do, release builds never (SPEC D15).
func DevCommandsEnabled() bool { return foundation.DebugMode || devBuild }

// DevStep is one step of EMETOS_DEV_COMMANDS: a command to run with its arguments, or a
// pause.
type DevStep struct {
	// Command is the command id; empty for a pause.
	Command string
	// Args are the arguments of the command; nil when the step has none.
	Args any
	// Sleep is the length of the pause when Command is empty.
	Sleep time.Duration
}

// ParseDevCommands reads the value of EMETOS_DEV_COMMANDS: a JSON array of steps, each
// ["<command id>", <args>] (the arguments are optional) or ["sleep", "<duration>"], such as
// [["workbench.action.quickOpen", ">line"], ["sleep", "200ms"]]. The error has the tag
// DevCommandsInvalid and says which step is wrong. An empty text is no steps.
func ParseDevCommands(text string) ([]DevStep, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, errors.NewErr("DevCommandsInvalid", err).SetMetadata("reason", "not a JSON array")
	}
	steps := make([]DevStep, 0, len(raw))
	for i, item := range raw {
		var parts []json.RawMessage
		if err := json.Unmarshal(item, &parts); err != nil || len(parts) == 0 || len(parts) > 2 {
			return nil, errors.New("DevCommandsInvalid", "a step is not [command] or [command, argument]", "step", i)
		}
		var id string
		if err := json.Unmarshal(parts[0], &id); err != nil || id == "" {
			return nil, errors.New("DevCommandsInvalid", "the command of a step is not a string", "step", i)
		}
		step := DevStep{Command: id}
		if len(parts) == 2 {
			if err := json.Unmarshal(parts[1], &step.Args); err != nil {
				return nil, errors.NewErr("DevCommandsInvalid", err).SetMetadata("step", i)
			}
		}
		if id == "sleep" {
			s, ok := step.Args.(string)
			if !ok {
				return nil, errors.New("DevCommandsInvalid", "sleep needs a duration such as \"200ms\"", "step", i)
			}
			d, err := time.ParseDuration(s)
			if err != nil || d < 0 {
				return nil, errors.New("DevCommandsInvalid", "the duration of a sleep is not valid", "step", i, "duration", s)
			}
			step = DevStep{Sleep: d}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// RegisterDev registers a command that exists for dev commands only: its id must start with
// "dev.". Nothing is registered, and nil is returned, in a build that does not run dev
// commands (SPEC D15), so such a command is not found there.
func RegisterDev(r *command.Registry, meta command.Meta, h command.Handler) error {
	if !strings.HasPrefix(meta.ID, "dev.") {
		return errors.New("DevCommandIDInvalid", "the id of a dev command starts with dev.", "id", meta.ID)
	}
	if !DevCommandsEnabled() {
		return nil
	}
	if err := r.Register(meta); err != nil {
		return err
	}
	_, err := r.SetHandler(meta.ID, h)
	return err
}

// DevRunner runs the steps of the dev commands. Run is called on the goroutine that
// walks the steps; Post hands a function to the UI thread and returns when it has run.
type DevRunner struct {
	// Steps are the steps to run.
	Steps []DevStep
	// Post runs f on the UI thread and waits for it.
	Post func(f func())
	// Commands is where the commands run.
	Commands *command.Registry
	// OnError gets the error of a step; the next step still runs.
	OnError func(step DevStep, err error)
}

// Run walks the steps: a pause waits (and ends early when ctx is canceled), a command runs
// on the UI thread.
func (r *DevRunner) Run(ctx context.Context) {
	for _, step := range r.Steps {
		if ctx.Err() != nil {
			return
		}
		if step.Command == "" {
			select {
			case <-time.After(step.Sleep):
			case <-ctx.Done():
				return
			}
			continue
		}
		var err error
		r.Post(func() {
			err = r.Commands.Run(command.Context{Ctx: ctx, Source: command.SourceAPI}, step.Command, step.Args)
		})
		if err != nil && r.OnError != nil {
			r.OnError(step, err)
		}
	}
}

// registerDevCommands registers the dev commands that belong to the editor: dev.type and
// dev.scroll, which the editor in front of the user implements while it has the focus, and
// dev.openFile and dev.openMenu, which the window implements (they exist in debug and emetos_dev builds only;
// SPEC D15, §15.2).
func registerDevCommands(r *command.Registry) {
	noEditor := func(command.Context, any) error {
		return errors.New("DevNoEditor", "no editor has the focus")
	}
	for _, meta := range []command.Meta{
		{ID: "dev.type", Description: "Type a text into the focused editor, a character in every frame."},
		{ID: "dev.scroll", Description: "Scroll the focused editor by rows in every frame."},
		{ID: "dev.openFile", Description: "Open a file in the window, with the cursor at a line and a column."},
		{ID: "dev.openMenu", Description: "Open a menu of the window: one of the menu bar, the Manage menu or the context menu of the editor."},
		{ID: "dev.openFolderInNewWindow", Description: `Open a folder in a new window. Arguments: {"path": ...}.`},
	} {
		// The ids are fixed, new and start with dev., so a failure is a mistake in this list.
		if err := RegisterDev(r, meta, noEditor); err != nil {
			panic(err)
		}
	}
}
