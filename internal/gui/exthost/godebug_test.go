package exthost

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/golang"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The Go debugger's attributes (VS Code Go's) read by the checks and the completion of launch.json (step 25.7): the subset of JSON
// Schema Emetos checks (decision 1162) has to understand what VS Code Go's package.json says.

// goSchema is the declaration of the Go debugger as the host turns it into the schema of launch.json.
func goSchema(t *testing.T) debug.DebuggerSchema {
	t.Helper()
	m := golang.New().Manifest()
	if len(m.Contributes.Debuggers) != 1 {
		t.Fatalf("%d debuggers", len(m.Contributes.Debuggers))
	}
	return schemaOf(m.Contributes.Debuggers[0], nil)
}

func TestTheAttributesOfTheGoDebuggerCheckALaunchFile(t *testing.T) {
	tests := []struct {
		name string
		text string
		// problem is a part of the message of the only problem; empty when there is none
		problem string
	}{
		{"a launch of a package", `{"configurations": [{"name": "L", "type": "go", "request": "launch", "mode": "auto", "program": "${fileDirname}"}]}`, ""},
		{"a test with args as a list", `{"configurations": [{"name": "L", "type": "go", "request": "launch", "mode": "test", "program": "${workspaceFolder}", "args": ["-test.run", "TestX"], "buildFlags": "-tags=x", "env": {"A": "1"}, "envFile": "${workspaceFolder}/.env"}]}`, ""},
		{"args as a string", `{"configurations": [{"name": "L", "type": "go", "request": "launch", "program": "${workspaceFolder}", "args": "-v \"a b\""}]}`, ""},
		{"an attach to a process picked from the list", `{"configurations": [{"name": "A", "type": "go", "request": "attach", "mode": "local", "processId": "${command:pickProcess}"}]}`, ""},
		{"an attach to a number", `{"configurations": [{"name": "A", "type": "go", "request": "attach", "mode": "local", "processId": 4242}]}`, ""},
		{"a server", `{"configurations": [{"name": "S", "type": "go", "request": "attach", "mode": "remote", "port": 2345, "host": "127.0.0.1", "substitutePath": [{"from": "/a", "to": "/b"}]}]}`, ""},
		{"a mode that is none", `{"configurations": [{"name": "L", "type": "go", "request": "launch", "mode": "bogus", "program": "x"}]}`, "mode"},
		{"a program that is a number", `{"configurations": [{"name": "L", "type": "go", "request": "launch", "program": 7}]}`, "program"},
		{"args that are a number", `{"configurations": [{"name": "L", "type": "go", "request": "launch", "args": 5}]}`, "args"},
		{"a port that is text", `{"configurations": [{"name": "S", "type": "go", "request": "attach", "mode": "remote", "port": "x"}]}`, "port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := debug.CheckAttributes(tt.text, []debug.DebuggerSchema{goSchema(t)})
			switch {
			case tt.problem == "" && len(problems) != 0:
				t.Errorf("problems = %+v, want none", problems)
			case tt.problem != "" && (len(problems) != 1 || !strings.Contains(problems[0].Message+problems[0].Path, tt.problem)):
				t.Errorf("problems = %+v, want one about %s", problems, tt.problem)
			}
		})
	}
}

func TestTheSnippetsOfTheGoDebuggerAreOfferedInALaunchFile(t *testing.T) {
	text := "{\"configurations\": [  ]}"
	items := debug.CompleteLaunch(text, strings.Index(text, "  ]")+1, []debug.DebuggerSchema{goSchema(t)})
	labels := map[string]bool{}
	for _, it := range items {
		if it.Kind == debug.CompleteSnippet {
			labels[it.Label] = true
		}
	}
	for _, want := range []string{"Go: Launch package", "Go: Launch file", "Go: Launch test function", "Go: Attach to local process", "Go: Connect to server"} {
		if !labels[want] {
			t.Errorf("snippet %q is not offered: %v", want, labels)
		}
	}
}
