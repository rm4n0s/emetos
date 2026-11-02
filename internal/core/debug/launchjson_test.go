package debug_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/errors"
)

func wantRoute(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want it to contain %s", appErr.Route(), route)
	}
	return appErr
}

const fullLaunch = `// a comment
{
  "version": "0.2.0",
  /* block comment */
  "configurations": [
    {
      "name": "Run",
      "type": "go",
      "request": "launch",
      "program": "${workspaceFolder}/cmd",
      "args": ["-v", "x"],
      "env": {"A": "1"},
      "preLaunchTask": "build",
      "postDebugTask": {"type": "shell", "label": "clean"},
      "presentation": {"group": "main", "order": 2},
      "linux": {"program": "${workspaceFolder}/linux"},
    },
    {"name": "Attach", "type": "go", "request": "attach", "presentation": {"hidden": true}},
    {"name": "Run", "type": "python", "request": "launch"},
  ],
  "compounds": [
    {"name": "Both", "stopAll": true, "preLaunchTask": "prep", "configurations": ["Run", {"name": "Attach", "folder": "app"}], "presentation": {"group": "main", "order": 1}},
  ],
  "inputs": [
    {"id": "who", "type": "promptString", "description": "Who?", "default": "me", "password": true},
    {"id": "pick", "type": "pickString", "description": "Pick", "options": ["a", {"label": "B", "value": "b"}], "default": "a"},
    {"id": "cmd", "type": "command", "command": "ext.cmd", "args": {"x": 1}},
  ],
}`

func TestParseLaunchFullFile(t *testing.T) {
	f, problems := debug.ParseLaunch(fullLaunch)
	for _, p := range problems {
		if p.Severity == debug.SeverityError {
			t.Fatalf("unexpected error problem: %+v", p)
		}
	}
	if f.Version != "0.2.0" {
		t.Errorf("version = %q", f.Version)
	}
	if len(f.Configurations) != 3 || f.Configurations[0].Name() != "Run" || f.Configurations[0].Request() != "launch" {
		t.Fatalf("configurations = %v", f.Configurations)
	}
	if got := f.Configurations[0].Task("preLaunchTask").Label; got != "build" {
		t.Errorf("preLaunchTask = %q", got)
	}
	if got := f.Configurations[0].Task("postDebugTask").String(); got != "shell clean" {
		t.Errorf("postDebugTask = %q", got)
	}
	if len(f.Compounds) != 1 {
		t.Fatalf("compounds = %+v", f.Compounds)
	}
	c := f.Compounds[0]
	if !c.StopAll || c.PreLaunchTask.Label != "prep" || len(c.Configurations) != 2 || c.Configurations[1] != (debug.CompoundRef{Name: "Attach", Folder: "app"}) {
		t.Errorf("compound = %+v", c)
	}
	if len(f.Inputs) != 3 {
		t.Fatalf("inputs = %+v", f.Inputs)
	}
	if in := f.Inputs[0]; in.ID != "who" || in.Default != "me" || !in.Password || in.Description != "Who?" {
		t.Errorf("prompt input = %+v", in)
	}
	if in := f.Inputs[1]; len(in.Options) != 2 || in.Options[1] != (debug.InputOption{Label: "B", Value: "b"}) {
		t.Errorf("pick input = %+v", in)
	}
	if in := f.Inputs[2]; in.Command != "ext.cmd" || in.Args == nil {
		t.Errorf("command input = %+v", in)
	}
	// A duplicate name is a warning.
	dup := false
	for _, p := range problems {
		dup = dup || (p.Severity == debug.SeverityWarning && strings.Contains(p.Message, "Duplicate configuration name 'Run'"))
	}
	if !dup {
		t.Errorf("no warning for the repeated name: %+v", problems)
	}
}

func TestParseLaunchProblems(t *testing.T) {
	type want struct {
		msg  string
		path string
		// at is the text the problem starts at (its first occurrence in the file).
		at  string
		sev debug.Severity
	}
	tests := []struct {
		name  string
		text  string
		wants []want
	}{
		{"syntax error", "{\n  \"configurations\": [\n    {\"name\": \"x\" \"type\": \"go\"}\n  ]\n}", []want{{"Comma expected", "", `"type"`, debug.SeverityError}}},
		{"root is an array", "[]", []want{{`Expected "object"`, "", "[]", debug.SeverityError}}},
		{"empty", "  ", []want{{"Value expected", "", "", debug.SeverityError}}},
		{"missing attributes", `{"configurations": [{"name": "a"}]}`, []want{
			{`Missing property "type"`, "configurations[0].type", `{"name": "a"}`, debug.SeverityWarning},
			{`Missing property "request"`, "configurations[0].request", `{"name": "a"}`, debug.SeverityWarning},
		}},
		{"wrong types", `{"version": 2, "configurations": {}, "compounds": 1, "inputs": "x"}`, []want{
			{`Expected "string"`, "version", "2", debug.SeverityError},
			{`Expected "array"`, "configurations", "{}", debug.SeverityError},
			{`Expected "array"`, "compounds", "1", debug.SeverityError},
			{`Expected "array"`, "inputs", `"x"`, debug.SeverityError},
		}},
		{"bad compound", `{"compounds": [1, {"name": "c"}, {"name": "d", "configurations": 3}, {"name": "e", "configurations": [7]}]}`, []want{
			{`Expected "object"`, "compounds[0]", "1,", debug.SeverityError},
			{`Missing property "configurations"`, "compounds[1].configurations", `{"name": "c"}`, debug.SeverityWarning},
			{`Expected "array"`, "compounds[2].configurations", "3}", debug.SeverityError},
			{"Expected a configuration name", "compounds[3].configurations[0]", "7]", debug.SeverityError},
		}},
		{"bad inputs", `{"inputs": [{"id": "a", "type": "nope"}, {"type": "pickString", "id": "b", "description": "d"}, {"type": "command", "id": "c"}, {"type": "pickString", "id": "e", "description": "d", "options": [1, {"label": "x"}]}]}`, []want{
			{"can only be of type", "inputs[0].type", `"nope"`, debug.SeverityError},
			{`Missing property "options"`, "inputs[1].options", `{"type": "pickString", "id": "b"`, debug.SeverityWarning},
			{`Missing property "command"`, "inputs[2].command", `{"type": "command"`, debug.SeverityWarning},
			{"Expected a string or an object", "inputs[3].options[0]", "1,", debug.SeverityError},
			{`Missing property "value"`, "inputs[3].options[1].value", `{"label": "x"}`, debug.SeverityWarning},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, problems := debug.ParseLaunch(tt.text)
			for _, w := range tt.wants {
				offset := strings.Index(tt.text, w.at)
				if w.at == "" {
					offset = len(tt.text)
				}
				line := strings.Count(tt.text[:offset], "\n")
				column := offset - (strings.LastIndex(tt.text[:offset], "\n") + 1)
				found := false
				for _, p := range problems {
					if strings.Contains(p.Message, w.msg) && p.Path == w.path && p.Severity == w.sev {
						found = true
						if p.Offset != offset || p.Line != line || p.Column != column {
							t.Errorf("%q is at offset %d (%d:%d), want %d (%d:%d)", w.msg, p.Offset, p.Line, p.Column, offset, line, column)
						}
					}
				}
				if !found {
					t.Errorf("no problem %q at %q (%v) in %+v", w.msg, w.path, w.sev, problems)
				}
			}
		})
	}
}

func TestLaunchFromSetting(t *testing.T) {
	f, problems := debug.LaunchFromSetting(map[string]any{
		"version":        "0.2.0",
		"configurations": []any{map[string]any{"name": "a", "type": "go", "request": "launch"}, "oops"},
	})
	if len(f.Configurations) != 1 || f.Configurations[0].Name() != "a" {
		t.Errorf("configurations = %v", f.Configurations)
	}
	if len(problems) != 1 || problems[0].Offset != -1 || problems[0].Line != -1 || problems[0].Path != "configurations[1]" {
		t.Errorf("problems = %+v", problems)
	}
	if _, problems := debug.LaunchFromSetting("x"); len(problems) != 1 {
		t.Errorf("a value that is no object: %+v", problems)
	}
}

func TestReadLaunchFile(t *testing.T) {
	ctx := context.Background()
	folder := t.TempDir()

	// No file: not found, no error.
	if _, _, found, err := debug.ReadLaunchFile(ctx, folder); err != nil || found {
		t.Fatalf("missing file: found=%v err=%v", found, err)
	}
	// A file.
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := debug.LaunchFilePath(folder)
	if err := os.WriteFile(path, []byte(fullLaunch), 0o644); err != nil {
		t.Fatal(err)
	}
	f, _, found, err := debug.ReadLaunchFile(ctx, folder)
	if err != nil || !found || len(f.Configurations) != 3 {
		t.Fatalf("read: found=%v err=%v configurations=%d", found, err, len(f.Configurations))
	}
	// A launch.json that is a folder cannot be read.
	other := t.TempDir()
	if err := os.MkdirAll(debug.LaunchFilePath(other), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = debug.ReadLaunchFile(ctx, other)
	_ = wantRoute(t, err, "ReadLaunchFile.LaunchFileReadFailed")
	// A cancelled context.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, _, _, err = debug.ReadLaunchFile(cctx, folder)
	_ = wantRoute(t, err, "ReadLaunchFile.LaunchFileReadFailed")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error does not wrap context.Canceled: %v", err)
	}
}
