package exthost

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/cpp"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The attributes of the native debuggers (those of lldb-dap's package.json and those written from the GDB manual, with the attribute
// `cargo` of the Rust extension) read by the checks and the completion of launch.json (step 25.9): the subset of JSON Schema Emetos
// checks (decision 1162) has to understand what the package.json says, and a launch that names cargo has no need of a program.

// nativeSchemas are the declarations of the three native debug types as the host turns them into the schemas of launch.json.
func nativeSchemas(t *testing.T) []debug.DebuggerSchema {
	t.Helper()
	m := cpp.New().Manifest()
	if len(m.Contributes.Debuggers) != 3 {
		t.Fatalf("%d debuggers", len(m.Contributes.Debuggers))
	}
	var out []debug.DebuggerSchema
	for _, d := range m.Contributes.Debuggers {
		out = append(out, schemaOf(d, nil))
	}
	return out
}

func TestTheAttributesOfTheNativeDebuggersCheckALaunchFile(t *testing.T) {
	tests := []struct {
		name string
		text string
		// problem is a part of the message of the only problem; empty when there is none
		problem string
	}{
		{"gdb launch", `{"configurations": [{"name": "G", "type": "gdb", "request": "launch", "program": "${workspaceFolder}/a.out", "args": ["-v"], "cwd": "${workspaceFolder}", "env": {"A": "1"}, "stopAtBeginningOfMainSubprogram": true}]}`, ""},
		{"gdb launch with arguments as a text", `{"configurations": [{"name": "G", "type": "gdb", "request": "launch", "program": "a.out", "args": "-v 'two words'", "debuggerArgs": ["-nx"]}]}`, ""},
		{"gdb attach to a process picked from the list", `{"configurations": [{"name": "G", "type": "gdb", "request": "attach", "pid": "${command:pickProcess}"}]}`, ""},
		{"gdb attach to a number", `{"configurations": [{"name": "G", "type": "gdb", "request": "attach", "pid": 4242}]}`, ""},
		{"gdb attach to a gdbserver", `{"configurations": [{"name": "G", "type": "gdb", "request": "attach", "target": "localhost:2345", "program": "a.out"}]}`, ""},
		{"gdb with cargo and no program", `{"configurations": [{"name": "G", "type": "gdb", "request": "launch", "cargo": {"args": ["build", "--bin=app"], "filter": {"name": "app", "kind": "bin"}}}]}`, ""},
		{"lldb-dap launch", `{"configurations": [{"name": "L", "type": "lldb-dap", "request": "launch", "program": "${workspaceFolder}/a.out", "args": [], "env": ["A=1"], "cwd": "${workspaceFolder}", "initCommands": ["settings set x 1"]}]}`, ""},
		{"lldb-dap launch with an object environment", `{"configurations": [{"name": "L", "type": "lldb-dap", "request": "launch", "program": "a.out", "env": {"A": "1"}, "sourceMap": {"/b": "/w"}, "console": "integratedTerminal"}]}`, ""},
		{"lldb-dap launch with a list source map", `{"configurations": [{"name": "L", "type": "lldb-dap", "request": "launch", "program": "a.out", "sourceMap": [["/b", "/w"]]}]}`, ""},
		{"lldb-dap with cargo and no program", `{"configurations": [{"name": "L", "type": "lldb-dap", "request": "launch", "cargo": {"args": ["test", "--no-run", "--lib"], "env": {"CARGO_HOME": "/h"}}}]}`, ""},
		{"lldb-dap attach by name", `{"configurations": [{"name": "L", "type": "lldb-dap", "request": "attach", "program": "a.out", "waitFor": true}]}`, ""},
		{"lldb-dap attach to a debug server", `{"configurations": [{"name": "L", "type": "lldb-dap", "request": "attach", "program": "a.out", "gdb-remote-port": 2345, "gdb-remote-hostname": "h"}]}`, ""},
		{"the old name", `{"configurations": [{"name": "L", "type": "lldb-vscode", "request": "launch", "program": "a.out"}]}`, ""},
		{"lldb-dap launch with neither program nor cargo", `{"configurations": [{"name": "L", "type": "lldb-dap", "request": "launch"}]}`, "program"},
		{"a program that is a number", `{"configurations": [{"name": "G", "type": "gdb", "request": "launch", "program": 7}]}`, "program"},
		{"stopOnEntry that is text", `{"configurations": [{"name": "G", "type": "gdb", "request": "launch", "program": "a", "stopOnEntry": "yes"}]}`, "stopOnEntry"},
		{"a console that is none", `{"configurations": [{"name": "L", "type": "lldb-dap", "request": "launch", "program": "a", "console": "bogus"}]}`, "console"},
		{"cargo without arguments", `{"configurations": [{"name": "G", "type": "gdb", "request": "launch", "cargo": {}}]}`, "args"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := debug.CheckAttributes(tt.text, nativeSchemas(t))
			switch {
			case tt.problem == "" && len(problems) != 0:
				t.Errorf("problems = %+v, want none", problems)
			case tt.problem != "" && (len(problems) != 1 || !strings.Contains(problems[0].Message+problems[0].Path, tt.problem)):
				t.Errorf("problems = %+v, want one about %s", problems, tt.problem)
			}
		})
	}
}

func TestTheSnippetsOfTheNativeDebuggersAreOfferedInALaunchFile(t *testing.T) {
	text := "{\"configurations\": [  ]}"
	items := debug.CompleteLaunch(text, strings.Index(text, "  ]")+1, nativeSchemas(t))
	labels := map[string]bool{}
	for _, it := range items {
		if it.Kind == debug.CompleteSnippet {
			labels[it.Label] = true
		}
	}
	for _, want := range []string{"GDB: Launch", "GDB: Attach to a process", "GDB: Connect to a gdbserver", "GDB: Load a core file", "LLDB: Launch", "LLDB: Attach", "LLDB: Load Coredump"} {
		if !labels[want] {
			t.Errorf("snippet %q is not offered: %v", want, labels)
		}
	}
}
