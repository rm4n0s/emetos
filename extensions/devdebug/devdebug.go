package devdebug

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The names of the extension.
const (
	// ID is the id of the extension.
	ID = "dev.debug"
	// Type is the debug type it contributes.
	Type = "dev.debug.fake"
	// AdapterEnv is the environment variable that names the fake adapter program (the build of internal/core/dap/daptest/fakeadapter).
	AdapterEnv = "EMETOS_DEV_FAKE_ADAPTER"
)

// attributes is the schema of the attributes of a configuration of the type.
const attributes = `{"properties": {"script": {"type": "string", "description": "The JSON lines script the fake adapter follows."}}}`

// New makes the extension. It is a factory of extension.Factory.
func New() extension.Extension { return &devDebug{} }

type devDebug struct{}

// Manifest implements extension.Extension.
func (*devDebug) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Fake Debugger (development)", Version: "0.1.0",
		Description:      "The fake debug adapter of the tests, as a debugger of debug builds.",
		ActivationEvents: []string{extension.OnDebug(Type)},
		Contributes: extension.Contributes{
			Debuggers: []extension.DebuggerSpec{{
				Type: Type, Label: "Fake Debugger (dev)", Languages: []string{"plaintext"},
				ConfigurationAttributes: map[string]json.RawMessage{"launch": json.RawMessage(attributes), "attach": json.RawMessage(attributes)},
				InitialConfigurations:   []extension.DebugConfiguration{{"name": "Fake: Launch", "request": "launch", "script": "${workspaceFolder}/fake.jsonl"}},
			}},
			Breakpoints: []extension.BreakpointSpec{{Language: "plaintext"}, {Language: "go"}},
		},
	}
}

// Activate implements extension.Extension: it registers the factory of the adapter.
func (*devDebug) Activate(_ context.Context, api extension.API) error {
	_, err := api.Debug().RegisterAdapterDescriptorFactory(Type, adapter)
	return err
}

// Deactivate implements extension.Extension.
func (*devDebug) Deactivate(context.Context) error { return nil }

// adapter finds the fake adapter: the program EMETOS_DEV_FAKE_ADAPTER names, run with the script of the configuration. ${workspaceFolder}
// in the script is the folder of the launch file, so that one script serves any folder; the script with it replaced is a file in a
// temporary folder that is removed when the session's context ends. A configuration without a script, or a build without the program,
// fails the session with a message that says what is missing.
func adapter(ctx context.Context, session extension.DebugSession, _ *extension.DebugAdapter) (*extension.DebugAdapter, error) {
	program := os.Getenv(AdapterEnv)
	if program == "" {
		return nil, errors.New("DevDebugAdapterMissing", "the fake adapter is not set: "+AdapterEnv+" names the program", "env", AdapterEnv)
	}
	script, _ := session.Configuration()["script"].(string)
	if script == "" {
		return nil, errors.New("DevDebugScriptMissing", "the configuration has no \"script\" for the fake adapter", "session", session.Name())
	}
	data, err := os.ReadFile(script)
	if err != nil {
		return nil, errors.NewErr("DevDebugScriptReadFailed", err).SetMetadata("script", script)
	}
	dir, err := os.MkdirTemp("", "emetos-dev-debug-")
	if err != nil {
		return nil, errors.NewErr("DevDebugScriptWriteFailed", err)
	}
	go func() {
		<-ctx.Done()
		_ = os.RemoveAll(dir)
	}()
	out := filepath.Join(dir, "script.jsonl")
	text := strings.ReplaceAll(string(data), "${workspaceFolder}", session.Folder())
	if err := os.WriteFile(out, []byte(text), 0o600); err != nil {
		return nil, errors.NewErr("DevDebugScriptWriteFailed", err).SetMetadata("script", out)
	}
	return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: program, Args: []string{"-script", out}}, nil
}
