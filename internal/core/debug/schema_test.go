package debug_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/debug"
)

// goSchema is the attribute schema of a debugger of the tests.
func goSchemas() []debug.DebuggerSchema {
	launch := map[string]any{}
	attach := map[string]any{}
	must := func(s string, into *map[string]any) {
		if err := json.Unmarshal([]byte(s), into); err != nil {
			panic(err)
		}
	}
	must(`{
		"required": ["program"],
		"properties": {
			"program": {"type": "string", "description": "Path to the program.", "default": "${workspaceFolder}"},
			"mode": {"type": "string", "enum": ["debug", "test", "exec"], "default": "debug"},
			"stopOnEntry": {"type": "boolean", "default": false},
			"port": {"type": "integer", "minimum": 1, "maximum": 65535},
			"args": {"type": "array", "items": {"type": "string"}},
			"env": {"type": "object", "additionalProperties": {"type": "string"}},
			"dlvFlags": {"type": ["array", "string"]},
			"showLog": {"anyOf": [{"type": "boolean"}, {"type": "string", "enum": ["verbose"]}]},
			"substitutePath": {"type": "array", "items": {"type": "object", "required": ["from", "to"], "properties": {"from": {"type": "string"}, "to": {"type": "string"}}}}
		},
		"additionalProperties": false
	}`, &launch)
	must(`{"required": ["processId"], "properties": {"processId": {"type": ["integer", "string"]}, "mode": {"type": "string", "enum": ["local", "remote"]}}}`, &attach)
	return []debug.DebuggerSchema{
		{Type: "go", Label: "Go", Attributes: map[string]map[string]any{"launch": launch, "attach": attach},
			Snippets: []debug.Snippet{{Label: "Go: Launch Package", Description: "Debug the package in the folder.",
				Body: debug.Config{"name": "Launch Package", "type": "go", "request": "launch", "mode": "auto", "program": "${1:${workspaceFolder}}"}}}},
		{Type: "plain", Label: "Plain"},
	}
}

func TestCheckAttributes(t *testing.T) {
	tests := []struct {
		name string
		text string
		// want are substrings of the messages, in order of the problems; the places are checked by the text each one underlines
		want []problem
	}{
		{"a valid launch configuration", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "${workspaceFolder}", "mode": "test", "port": 4000}]}`, nil},
		{"a missing required attribute is reported on the object", `{"configurations": [{"name": "a", "type": "go", "request": "launch"}]}`,
			[]problem{{`Missing property "program".`, "{"}}},
		{"an unknown type", `{"configurations": [{"name": "a", "type": "nope", "request": "launch"}]}`,
			[]problem{{"Unknown debug type 'nope'. The debuggers are: go, plain.", `"nope"`}}},
		{"a request the debugger does not have", `{"configurations": [{"name": "a", "type": "go", "request": "run"}]}`,
			[]problem{{"The debugger 'go' does not support the request 'run'. Use one of: attach, launch.", `"run"`}}},
		{"a value outside the enum", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "mode": "fast"}]}`,
			[]problem{{`Value is not accepted. Valid values: "debug", "test", "exec".`, `"fast"`}}},
		{"a value of the wrong type", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "stopOnEntry": "yes"}]}`,
			[]problem{{`Incorrect type. Expected "boolean".`, `"yes"`}}},
		{"an integer that is not one", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "port": 1.5}]}`,
			[]problem{{`Incorrect type. Expected "integer".`, `1.5`}}},
		{"a number below the minimum", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "port": 0}]}`,
			[]problem{{"Value is below the minimum of 1.", `0`}}},
		{"a number above the maximum", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "port": 70000}]}`,
			[]problem{{"Value is above the maximum of 65535.", `70000`}}},
		{"an attribute the schema does not allow", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "colour": 1}]}`,
			[]problem{{"Property colour is not allowed.", `"colour"`}}},
		{"the common attributes are always allowed", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "preLaunchTask": "build", "presentation": {"hidden": true}, "internalConsoleOptions": "neverOpen"}]}`, nil},
		{"a wrong common attribute", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "internalConsoleOptions": "always"}]}`,
			[]problem{{`Value is not accepted. Valid values: "neverOpen", "openOnSessionStart", "openOnFirstSessionStart".`, `"always"`}}},
		{"items of an array", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "args": ["x", 2]}]}`,
			[]problem{{`Incorrect type. Expected "string".`, `2`}}},
		{"additional properties with a schema", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "env": {"A": "1", "B": 2}}]}`,
			[]problem{{`Incorrect type. Expected "string".`, `2`}}},
		{"one of several types", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "dlvFlags": "-x"}, {"name": "b", "type": "go", "request": "launch", "program": "p", "dlvFlags": 3}]}`,
			[]problem{{`Incorrect type. Expected one of "array", "string".`, `3`}}},
		{"anyOf accepts either alternative", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "showLog": true}, {"name": "b", "type": "go", "request": "launch", "program": "p", "showLog": "verbose"}]}`, nil},
		{"anyOf reports the closest alternative", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "showLog": "loud"}]}`,
			[]problem{{`Value is not accepted. Valid values: "verbose".`, `"loud"`}}},
		{"a required member of an item", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "substitutePath": [{"from": "/a"}]}]}`,
			[]problem{{`Missing property "to".`, `{`}}},
		{"a variable stands for any type", `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p", "port": "${input:port}", "stopOnEntry": "${env:STOP}"}]}`, nil},
		{"an attach configuration", `{"configurations": [{"name": "a", "type": "go", "request": "attach", "mode": "sideways"}]}`,
			[]problem{{`Missing property "processId".`, `{`}, {`Value is not accepted. Valid values: "local", "remote".`, `"sideways"`}}},
		{"a debugger without attributes accepts anything", `{"configurations": [{"name": "a", "type": "plain", "request": "launch", "whatever": [1]}]}`, nil},
		{"what ParseLaunch reports is not repeated", `{"configurations": [{"name": "a", "request": "launch"}]}`, nil},
		{"comments and trailing commas", "{\n// c\n\"configurations\": [{\"name\": \"a\", \"type\": \"go\", \"request\": \"launch\", \"mode\": \"fast\", \"program\": \"p\",},],}", []problem{{`Value is not accepted. Valid values: "debug", "test", "exec".`, `"fast"`}}},
		{"no configurations", `{"version": "0.2.0"}`, nil},
		{"text that is not a launch file", `[1, 2]`, nil},
		{"empty text", ``, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := debug.CheckAttributes(tt.text, goSchemas())
			if len(got) != len(tt.want) {
				t.Fatalf("got %d problems, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				p := got[i]
				if p.Message != w.message {
					t.Errorf("problem %d message = %q, want %q", i, p.Message, w.message)
				}
				if p.Severity != debug.SeverityWarning {
					t.Errorf("problem %d severity = %v, want a warning", i, p.Severity)
				}
				under := tt.text[p.Offset : p.Offset+p.Length]
				if under != w.under {
					t.Errorf("problem %d underlines %q, want %q", i, under, w.under)
				}
				if p.Line < 0 || p.Column < 0 {
					t.Errorf("problem %d has no line and column: %+v", i, p)
				}
			}
		})
	}
}

type problem struct{ message, under string }

func TestCheckAttributesPaths(t *testing.T) {
	text := `{"configurations": [{"name": "a", "type": "go", "request": "launch", "program": "p"}, {"name": "b", "type": "go", "request": "launch", "program": "p", "args": [1]}]}`
	got := debug.CheckAttributes(text, goSchemas())
	if len(got) != 1 || got[0].Path != "configurations[1].args[0]" {
		t.Fatalf("problems = %+v", got)
	}
}

func completeAt(t *testing.T, textWithCursor string) []debug.LaunchCompletion {
	t.Helper()
	i := strings.Index(textWithCursor, "|")
	if i < 0 {
		t.Fatal("no cursor")
	}
	return debug.CompleteLaunch(textWithCursor[:i]+textWithCursor[i+1:], i, goSchemas())
}

func labels(items []debug.LaunchCompletion) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func TestCompleteLaunch(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
		// only checks that the labels are included, not that they are all
		includes bool
	}{
		{"the keys of the file", `{ | }`, []string{"version", "configurations", "compounds", "inputs"}, false},
		{"the keys of the file leave out those that are there", `{"version": "0.2.0", | }`, []string{"configurations", "compounds", "inputs"}, false},
		{"snippets where a configuration starts", `{"configurations": [ | ]}`, []string{"Go: Launch Package"}, false},
		{"the debug types", `{"configurations": [{"type": |}]}`, []string{"go", "plain"}, false},
		{"the debug types inside the quotes", `{"configurations": [{"type": "g|"}]}`, []string{"go", "plain"}, false},
		{"the requests of a debugger", `{"configurations": [{"type": "go", "request": |}]}`, []string{"attach", "launch"}, false},
		{"the requests of a type that is not known", `{"configurations": [{"type": "x", "request": |}]}`, []string{"launch", "attach"}, false},
		{"the attributes of the launch request", `{"configurations": [{"type": "go", "request": "launch", "name": "a", | }]}`,
			[]string{"args", "dlvFlags", "env", "mode", "port", "program", "showLog", "stopOnEntry", "substitutePath", "presentation", "preLaunchTask", "postDebugTask", "internalConsoleOptions", "suppressMultipleSessionWarning"}, true},
		{"the attributes of the attach request", `{"configurations": [{"type": "go", "request": "attach", | }]}`, []string{"mode", "processId", "name", "presentation", "preLaunchTask", "postDebugTask", "internalConsoleOptions", "suppressMultipleSessionWarning"}, true},
		{"without a type only the common attributes", `{"configurations": [{ | }]}`, []string{"debugServer", "internalConsoleOptions", "linux", "name", "noDebug", "osx", "postDebugTask", "postRestartTask", "preLaunchTask", "preRestartTask", "presentation", "request", "suppressMultipleSessionWarning", "type", "windows"}, true},
		{"the values of an enum", `{"configurations": [{"type": "go", "request": "launch", "mode": |}]}`, []string{"debug", "test", "exec"}, false},
		{"true and false", `{"configurations": [{"type": "go", "request": "launch", "stopOnEntry": |}]}`, []string{"false", "true"}, false},
		{"the default of a string", `{"configurations": [{"type": "go", "request": "launch", "program": |}]}`, []string{"${workspaceFolder}"}, false},
		{"the names inside a nested object", `{"configurations": [{"type": "go", "request": "launch", "substitutePath": [{ | }]}]}`, []string{"from", "to"}, false},
		{"nothing in a key that is not known", `{"other": |}`, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := labels(completeAt(t, tt.text))
			want := append([]string(nil), tt.want...)
			if !tt.includes {
				sort.Strings(got)
				sort.Strings(want)
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Fatalf("labels = %v, want %v", got, want)
				}
				return
			}
			for _, w := range want {
				found := false
				for _, g := range got {
					found = found || g == w
				}
				if !found {
					t.Errorf("%q is not suggested: %v", w, got)
				}
			}
		})
	}
}

func TestCompleteLaunchInsertText(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		label string
		want  string
		snip  bool
	}{
		{"a string attribute with a default", `{"configurations": [{"type": "go", "request": "launch", | }]}`, "program", `"program": "${1:\${workspaceFolder\}}"`, true},
		{"an enum attribute is a choice", `{"configurations": [{"type": "go", "request": "launch", | }]}`, "mode", `"mode": "${1:debug}"`, true},
		{"a boolean with a default", `{"configurations": [{"type": "go", "request": "launch", | }]}`, "stopOnEntry", `"stopOnEntry": ${1:false}`, true},
		{"an integer", `{"configurations": [{"type": "go", "request": "launch", | }]}`, "port", `"port": ${1:0}`, true},
		{"an array", `{"configurations": [{"type": "go", "request": "launch", | }]}`, "args", `"args": [$1]`, true},
		{"an object", `{"configurations": [{"type": "go", "request": "launch", | }]}`, "env", `"env": {$1}`, true},
		{"a name that has its colon already is only the name", `{"configurations": [{"type": "go", "request": "launch", "pro|": "x" }]}`, "program", `"program"`, false},
		{"a type value is a JSON string", `{"configurations": [{"type": |}]}`, "go", `"go"`, false},
		{"an enum value", `{"configurations": [{"type": "go", "request": "launch", "mode": |}]}`, "test", `"test"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, it := range completeAt(t, tt.text) {
				if it.Label != tt.label {
					continue
				}
				if it.InsertText != tt.want || it.Snippet != tt.snip {
					t.Errorf("insert = %q (snippet %v), want %q (%v)", it.InsertText, it.Snippet, tt.want, tt.snip)
				}
				return
			}
			t.Fatalf("%q is not suggested", tt.label)
		})
	}
}

func TestCompleteLaunchRange(t *testing.T) {
	// the quoted word under the cursor is replaced
	text := `{"configurations": [{"type": "go", "request": "launch", "pro": 1}]}`
	i := strings.Index(text, `"pro"`) + 4 // inside the quotes, after "pro"
	items := debug.CompleteLaunch(text, i, goSchemas())
	for _, it := range items {
		if it.Label == "program" {
			if got := text[it.Start:it.End]; got != `"pro"` {
				t.Errorf("replaces %q, want \"pro\"", got)
			}
			return
		}
	}
	t.Fatalf("program is not suggested: %v", labels(items))
}

func TestCompleteLaunchSnippetBody(t *testing.T) {
	text := `{"configurations": [  ]}`
	items := debug.CompleteLaunch(text, strings.Index(text, "  ]")+1, goSchemas())
	if len(items) != 1 || items[0].Kind != debug.CompleteSnippet || !items[0].Snippet {
		t.Fatalf("items = %+v", items)
	}
	want := "{\n\t\"name\": \"Launch Package\",\n\t\"type\": \"go\",\n\t\"request\": \"launch\",\n\t\"mode\": \"auto\",\n\t\"program\": \"${1:${workspaceFolder}}\"\n}"
	if items[0].InsertText != want {
		t.Errorf("snippet = %q, want %q", items[0].InsertText, want)
	}
	if items[0].Detail != "Go" || items[0].Documentation != "Debug the package in the folder." {
		t.Errorf("detail and documentation = %q, %q", items[0].Detail, items[0].Documentation)
	}
}

func TestCompleteLaunchOutOfRange(t *testing.T) {
	if got := debug.CompleteLaunch("{}", 9, goSchemas()); got != nil {
		t.Errorf("an offset past the end gave %v", got)
	}
	if got := debug.CompleteLaunch("", 0, goSchemas()); got != nil {
		t.Errorf("empty text gave %v", got)
	}
}
