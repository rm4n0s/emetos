package exthost

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/python"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The Python debugger's attributes (those of the Python Debugger of VS Code) read by the checks and the completion of launch.json (step
// 25.8): the subset of JSON Schema Emetos checks (decision 1162) has to understand what its package.json says.

// pythonSchemas are the declarations of the two Python debug types as the host turns them into the schemas of launch.json.
func pythonSchemas(t *testing.T) []debug.DebuggerSchema {
	t.Helper()
	m := python.New().Manifest()
	if len(m.Contributes.Debuggers) != 2 {
		t.Fatalf("%d debuggers", len(m.Contributes.Debuggers))
	}
	return []debug.DebuggerSchema{schemaOf(m.Contributes.Debuggers[0], nil), schemaOf(m.Contributes.Debuggers[1], nil)}
}

func TestTheAttributesOfThePythonDebuggerCheckALaunchFile(t *testing.T) {
	tests := []struct {
		name string
		text string
		// problem is a part of the message of the only problem; empty when there is none
		problem string
	}{
		{"the current file", `{"configurations": [{"name": "F", "type": "debugpy", "request": "launch", "program": "${file}", "console": "integratedTerminal"}]}`, ""},
		{"the old name", `{"configurations": [{"name": "F", "type": "python", "request": "launch", "program": "${file}"}]}`, ""},
		{"a module with arguments", `{"configurations": [{"name": "M", "type": "debugpy", "request": "launch", "module": "flask", "env": {"FLASK_APP": "app.py"}, "args": ["run", "--no-reload"], "jinja": true, "justMyCode": false}]}`, ""},
		{"arguments asked for", `{"configurations": [{"name": "F", "type": "debugpy", "request": "launch", "program": "${file}", "args": "${command:pickArgs}"}]}`, ""},
		{"the interpreter of the extension", `{"configurations": [{"name": "F", "type": "debugpy", "request": "launch", "program": "${file}", "python": "${command:python.interpreterPath}", "pythonArgs": ["-X", "dev"]}]}`, ""},
		{"path mappings", `{"configurations": [{"name": "A", "type": "debugpy", "request": "attach", "connect": {"host": "localhost", "port": 5678}, "pathMappings": [{"localRoot": "${workspaceFolder}", "remoteRoot": "."}]}]}`, ""},
		{"an attach to a process picked from the list", `{"configurations": [{"name": "A", "type": "debugpy", "request": "attach", "processId": "${command:pickProcess}"}]}`, ""},
		{"an attach to a number", `{"configurations": [{"name": "A", "type": "debugpy", "request": "attach", "processId": 4242}]}`, ""},
		{"an attach that listens", `{"configurations": [{"name": "A", "type": "debugpy", "request": "attach", "listen": {"port": 5678}}]}`, ""},
		{"a console that is none", `{"configurations": [{"name": "F", "type": "debugpy", "request": "launch", "program": "x", "console": "bogus"}]}`, "console"},
		{"a program that is a number", `{"configurations": [{"name": "F", "type": "debugpy", "request": "launch", "program": 7}]}`, "program"},
		{"justMyCode that is text", `{"configurations": [{"name": "F", "type": "debugpy", "request": "launch", "program": "x", "justMyCode": "yes"}]}`, "justMyCode"},
		{"connect with no port", `{"configurations": [{"name": "A", "type": "debugpy", "request": "attach", "connect": {"host": "localhost"}}]}`, "port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := debug.CheckAttributes(tt.text, pythonSchemas(t))
			switch {
			case tt.problem == "" && len(problems) != 0:
				t.Errorf("problems = %+v, want none", problems)
			case tt.problem != "" && (len(problems) != 1 || !strings.Contains(problems[0].Message+problems[0].Path, tt.problem)):
				t.Errorf("problems = %+v, want one about %s", problems, tt.problem)
			}
		})
	}
}

func TestTheSnippetsOfThePythonDebuggerAreOfferedInALaunchFile(t *testing.T) {
	text := "{\"configurations\": [  ]}"
	items := debug.CompleteLaunch(text, strings.Index(text, "  ]")+1, pythonSchemas(t))
	labels := map[string]bool{}
	for _, it := range items {
		if it.Kind == debug.CompleteSnippet {
			labels[it.Label] = true
		}
	}
	for _, want := range []string{"Python Debugger: Python File", "Python Debugger: Python File with Arguments", "Python Debugger: Module", "Python Debugger: Remote Attach", "Python Debugger: Attach using Process ID"} {
		if !labels[want] {
			t.Errorf("snippet %q is not offered: %v", want, labels)
		}
	}
}
