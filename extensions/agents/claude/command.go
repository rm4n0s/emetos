package claude

import (
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// ProgramName is the name of the program that is looked up in PATH.
const ProgramName = "claude"

// CheckpointingVariable is the one variable the adapter adds to the environment of the program: it turns on the file
// checkpoints that RewindFiles needs (SPEC §10.16).
const CheckpointingVariable = "CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"

// Adapter finds and runs the user's claude program for one workspace folder.
type Adapter struct {
	// Path is the setting agents.claude.path: the program to run. Empty means the program called "claude" in PATH.
	Path string
	// Dir is the workspace folder; the program runs in it. Empty is the folder of Emetos.
	Dir string
	// Logger gets the problems the adapter does not return (a handler that failed, a response nobody waits for). Nil is
	// slog.Default().
	Logger *slog.Logger

	// authTimeout, closeGrace and killGrace replace the defaults when set (tests).
	authTimeout time.Duration
	closeGrace  time.Duration
	killGrace   time.Duration
}

// Defaults of the time limits; see Adapter.AuthStatus, Session.Close and the end of a Session whose context ended.
const (
	defaultAuthTimeout = 20 * time.Second
	// defaultCloseGrace is how long a program has to end after its input was closed before its group is told to
	// terminate.
	defaultCloseGrace = 3 * time.Second
	// defaultKillGrace is how long a group has to end after SIGTERM before it is killed.
	defaultKillGrace = 2 * time.Second
)

func (a *Adapter) logger() *slog.Logger {
	if a.Logger != nil {
		return a.Logger
	}
	return slog.Default()
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Find returns the path of the program: Path when it is set, else the program called "claude" in PATH. It fails with the
// tag ProgramNotFound, with the name that was looked up in the metadata.
func (a *Adapter) Find() (string, error) {
	name := a.Path
	if name == "" {
		name = ProgramName
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.NewErr("ProgramNotFound", err).SetMetadata("program", name, "fromSetting", a.Path != "")
	}
	return path, nil
}

// PermissionMode is a mode of the program for asking about tools (--permission-mode).
type PermissionMode string

// The modes Emetos offers (SPEC §10.16): Manual, Accept Edits and Plan. The program has more (auto, dontAsk,
// bypassPermissions); the adapter never starts one of them.
const (
	// ModeManual is "Manual": every tool that is not allowed by a rule asks.
	ModeManual PermissionMode = "default"
	// ModeAcceptEdits is "Accept Edits": edits of files are allowed without asking.
	ModeAcceptEdits PermissionMode = "acceptEdits"
	// ModePlan is "Plan": the agent only reads and makes a plan.
	ModePlan PermissionMode = "plan"
)

// Valid reports whether m is a mode the adapter starts or switches to.
func (m PermissionMode) Valid() bool {
	return m == ModeManual || m == ModeAcceptEdits || m == ModePlan
}

// SessionOptions says how to start a session.
type SessionOptions struct {
	// PermissionMode is the mode the session starts in; empty is ModeManual.
	PermissionMode PermissionMode
	// SessionID is the UUID of a new conversation (--session-id); empty lets the program choose one. It cannot be set
	// together with Resume.
	SessionID string
	// Resume is the ID of an earlier conversation to continue (--resume).
	Resume string
	// Model is the model to start with (--model); empty is the program's default.
	Model string

	// OnEvent is called for each event of the session, in order, from one goroutine of the session's own (so a slow
	// OnEvent delays only the later events, never the reading of the program). It must not call Close or wait for the
	// session to end; to touch the UI it hands the event over (SetState, the binding's Post). Nil drops the events.
	OnEvent func(Event)
	// CanUseTool answers the permission requests of the program (can_use_tool). It is called from a goroutine per request, so
	// it may wait for the user; its context ends when the program cancels the request or the session ends. Nil denies
	// every request.
	CanUseTool CanUseToolFunc
	// Stderr is called with each line the program writes to its standard error, from a goroutine of its own. Nil drops
	// them.
	Stderr func(line string)

	// RequestTimeout is the time a control request has to be answered; zero is 60 seconds.
	RequestTimeout time.Duration
	// MaxLineBytes is the longest line of output read whole; zero is DefaultMaxLineBytes.
	MaxLineBytes int
}

// DefaultRequestTimeout is the time a control request has to be answered, as in the Agent SDK.
const DefaultRequestTimeout = 60 * time.Second

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Args returns the arguments of the program for a session (SPEC §10.16):
//
//	-p --input-format stream-json --output-format stream-json --verbose --include-partial-messages
//	--replay-user-messages --permission-prompt-tool stdio --permission-mode <mode>
//	[--session-id <uuid> | --resume <id>] [--model <model>]
//
// --permission-prompt-tool stdio is what makes the program ask Emetos (can_use_tool) instead of refusing the tools that need
// a permission. Never --bare. It fails with the tag InvalidOptions.
func Args(o SessionOptions) ([]string, error) {
	mode := o.PermissionMode
	if mode == "" {
		mode = ModeManual
	}
	if !mode.Valid() {
		return nil, errors.New("InvalidOptions", "unknown permission mode", "mode", string(mode))
	}
	if o.SessionID != "" && o.Resume != "" {
		return nil, errors.New("InvalidOptions", "a session cannot be new and resumed at once")
	}
	if o.SessionID != "" && !uuidPattern.MatchString(o.SessionID) {
		return nil, errors.New("InvalidOptions", "the session ID is not a UUID", "sessionID", o.SessionID)
	}
	// a value that starts with a dash would be read as a flag
	if strings.HasPrefix(o.Resume, "-") || strings.HasPrefix(o.Model, "-") {
		return nil, errors.New("InvalidOptions", "a session ID or model cannot start with a dash")
	}
	args := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--include-partial-messages", "--replay-user-messages",
		"--permission-prompt-tool", "stdio", "--permission-mode", string(mode),
	}
	switch {
	case o.SessionID != "":
		args = append(args, "--session-id", o.SessionID)
	case o.Resume != "":
		args = append(args, "--resume", o.Resume)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	return args, nil
}

// Environment returns the environment of the program: base (the environment of Emetos) unchanged, with CheckpointingVariable
// set to true. Nothing else is added or removed: the adapter never sets CLAUDE_CONFIG_DIR, a key or a token, and a
// variable of the user's own environment reaches the program as it is.
func Environment(base []string) []string {
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if name, _, _ := strings.Cut(kv, "="); name == CheckpointingVariable {
			continue
		}
		env = append(env, kv)
	}
	return append(env, CheckpointingVariable+"=true")
}
