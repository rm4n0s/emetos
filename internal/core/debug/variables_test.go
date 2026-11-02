package debug_test

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/debug"
	apperrors "github.com/rm4n0s/errors"
)

func testContext() *debug.VariableContext {
	return &debug.VariableContext{
		Folders:       []debug.Folder{{Name: "app", Path: "/work/app"}},
		ActiveFile:    "/work/app/src/pkg/main.test.go",
		SelectedText:  "sel",
		Line:          12,
		Column:        3,
		Cwd:           "/cwd",
		ExecPath:      "/usr/bin/emetos",
		InstallFolder: "/usr/share/emetos",
		UserHome:      "/home/ada",
		Env: func(name string) (string, bool) {
			v, ok := map[string]string{"HOME": "/home/ada", "EMPTY": ""}[name]
			return v, ok
		},
		Setting: func(folder, key string) (any, bool) {
			v, ok := map[string]any{"s.str": "text", "s.num": 4.0, "s.bool": true, "s.obj": map[string]any{"a": 1.0}, "s.null": nil}[key]
			return v, ok
		},
	}
}

func TestVariablesEveryVariable(t *testing.T) {
	vc := testContext()
	opts := debug.ResolveOptions{Folder: "/work/app"}
	tests := []struct{ in, want string }{
		{"${workspaceFolder}", "/work/app"},
		{"${workspaceRoot}", "/work/app"},
		{"${workspaceFolder:app}", "/work/app"},
		{"${workspaceFolderBasename}", "app"},
		{"${workspaceRootFolderName}", "app"},
		{"${file}", "/work/app/src/pkg/main.test.go"},
		{"${fileBasename}", "main.test.go"},
		{"${fileBasenameNoExtension}", "main.test"},
		{"${fileDirname}", "/work/app/src/pkg"},
		{"${fileDirnameBasename}", "pkg"},
		{"${fileExtname}", ".go"},
		{"${relativeFile}", "src/pkg/main.test.go"},
		{"${relativeFileDirname}", "src/pkg"},
		{"${fileWorkspaceFolder}", "/work/app"},
		{"${fileWorkspaceFolderBasename}", "app"},
		{"${lineNumber}", "12"},
		{"${columnNumber}", "3"},
		{"${selectedText}", "sel"},
		{"${cwd}", "/work/app"},
		{"${pathSeparator}", "/"},
		{"${/}", "/"},
		{"${env:HOME}", "/home/ada"},
		{"${env:EMPTY}", ""},
		{"${env:UNSET}", ""},
		{"${config:s.str}", "text"},
		{"${config:s.num}", "4"},
		{"${config:s.bool}", "true"},
		{"${userHome}", "/home/ada"},
		{"${execPath}", "/usr/bin/emetos"},
		{"${execInstallFolder}", "/usr/share/emetos"},
		// text around, several variables, repeated ones, nested braces
		{"a ${fileBasename} b ${fileBasename} c", "a main.test.go b main.test.go c"},
		{"${workspaceFolder}/${env:HOME}", "/work/app//home/ada"},
		{"no variable $ { } ${", "no variable $ { } ${"},
		{"${unknownThing}", "${unknownThing}"},
		{"${unknown:arg}", "${unknown:arg}"},
		{"${command:x}", "${command:x}"},
		{"${input:y}", "${input:y}"},
		{"${a${b}}", "${a${b}}"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok, err := debug.ResolveString(context.Background(), vc, tt.in, opts)
			if err != nil || !ok {
				t.Fatalf("ResolveString(%q) = %q, %v, %v", tt.in, got, ok, err)
			}
			if got != tt.want {
				t.Errorf("ResolveString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestVariablesWithoutFolderOrEditor(t *testing.T) {
	// ${cwd} without a folder is the working folder; the relative forms without a folder are the absolute path.
	vc := testContext()
	got, _, err := debug.ResolveString(context.Background(), vc, "${cwd} ${relativeFile} ${relativeFileDirname}", debug.ResolveOptions{})
	if err != nil || got != "/cwd /work/app/src/pkg/main.test.go /work/app/src/pkg" {
		t.Errorf("got %q, %v", got, err)
	}
	// execPath without a value stays as written.
	got, _, err = debug.ResolveString(context.Background(), &debug.VariableContext{}, "${execPath}|${execInstallFolder}", debug.ResolveOptions{})
	if err != nil || got != "${execPath}|${execInstallFolder}" {
		t.Errorf("got %q, %v", got, err)
	}
	// A nil context resolves with nothing.
	if got, _, err := debug.ResolveString(context.Background(), nil, "${pathSeparator}", debug.ResolveOptions{}); err != nil || got != "/" {
		t.Errorf("got %q, %v", got, err)
	}
	// The relative forms with a folder; the same folder gives ".".
	vc.ActiveFile = "/work/app/a.go"
	got, _, err = debug.ResolveString(context.Background(), vc, "[${relativeFile}][${relativeFileDirname}]", debug.ResolveOptions{Folder: "/work/app"})
	if err != nil || got != "[a.go][.]" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestVariablesErrors(t *testing.T) {
	multi := testContext()
	multi.Folders = append(multi.Folders, debug.Folder{Name: "lib", Path: "/work/lib"})
	noFile := testContext()
	noFile.ActiveFile = ""
	outside := testContext()
	outside.ActiveFile = "/elsewhere/x.go"
	tests := []struct {
		name  string
		vc    *debug.VariableContext
		opts  debug.ResolveOptions
		in    string
		route string
	}{
		{"file without an editor", noFile, debug.ResolveOptions{}, "${file}", "resolveBuiltin.VariableNoEditor"},
		{"relativeFile without an editor", noFile, debug.ResolveOptions{}, "${relativeFile}", "resolveBuiltin.VariableNoEditor"},
		{"fileWorkspaceFolder of a file outside", outside, debug.ResolveOptions{}, "${fileWorkspaceFolder}", "resolveBuiltin.VariableNoFolderOfFile"},
		{"workspaceFolder without a folder", testContext(), debug.ResolveOptions{}, "${workspaceFolder}", "resolveBuiltin.VariableNoFolder"},
		{"workspaceFolder in several folders", multi, debug.ResolveOptions{}, "${workspaceFolder}", "resolveBuiltin.VariableFolderAmbiguous"},
		{"workspaceFolder of a folder that does not exist", testContext(), debug.ResolveOptions{Folder: "/work/app"}, "${workspaceFolder:nope}", "resolveBuiltin.VariableFolderNotFound"},
		{"workspaceFolderBasename without a folder", testContext(), debug.ResolveOptions{}, "${workspaceFolderBasename}", "resolveBuiltin.VariableNoFolder"},
		{"cwd of a folder that does not exist", testContext(), debug.ResolveOptions{}, "${cwd:nope}", "resolveBuiltin.VariableFolderNotFound"},
		{"env without a name", testContext(), debug.ResolveOptions{}, "${env}", "resolveBuiltin.VariableEnvNameMissing"},
		{"config without a name", testContext(), debug.ResolveOptions{}, "${config}", "resolveBuiltin.VariableSettingNameMissing"},
		{"config of an unknown setting", testContext(), debug.ResolveOptions{}, "${config:s.nope}", "resolveBuiltin.VariableSettingNotFound"},
		{"config that is null", testContext(), debug.ResolveOptions{}, "${config:s.null}", "resolveBuiltin.VariableSettingNotFound"},
		{"config that is structured", testContext(), debug.ResolveOptions{}, "${config:s.obj}", "resolveBuiltin.VariableSettingStructured"},
		{"userHome without a value", &debug.VariableContext{}, debug.ResolveOptions{}, "${userHome}", "resolveBuiltin.VariableNoUserHome"},
		{"lineNumber without a line", &debug.VariableContext{}, debug.ResolveOptions{}, "${lineNumber}", "resolveBuiltin.VariableNoLine"},
		{"columnNumber without a column", &debug.VariableContext{}, debug.ResolveOptions{}, "${columnNumber}", "resolveBuiltin.VariableNoColumn"},
		{"selectedText without a selection", &debug.VariableContext{}, debug.ResolveOptions{}, "${selectedText}", "resolveBuiltin.VariableNoSelection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok, err := debug.ResolveString(context.Background(), tt.vc, tt.in, tt.opts)
			if err == nil || ok {
				t.Fatalf("ResolveString(%q) = ok %v, err %v; want an error", tt.in, ok, err)
			}
			_ = wantRoute(t, err, tt.route)
		})
	}
}

func TestResolveConfigWalksObjectsKeysAndPlatform(t *testing.T) {
	vc := testContext()
	cfg := debug.Config{
		"name": "n", "type": "go", "request": "launch",
		"program": "${workspaceFolder}/main.go",
		"args":    []any{"${fileBasename}", 3.0, true, nil, map[string]any{"deep": "${env:HOME}"}},
		"env":     map[string]any{"${env:HOME}-key": "${lineNumber}"},
		"linux":   map[string]any{"program": "${workspaceFolder}/linux.go", "extra": "${pathSeparator}"},
		"windows": map[string]any{"program": "win"},
		"osx":     map[string]any{"program": "mac"},
	}
	before := cfg.Clone()
	got, ok, err := debug.ResolveConfig(context.Background(), vc, cfg, debug.ResolveOptions{Folder: "/work/app"})
	if err != nil || !ok {
		t.Fatalf("ResolveConfig: %v %v", ok, err)
	}
	want := debug.Config{
		"name": "n", "type": "go", "request": "launch",
		"program": "/work/app/linux.go",
		"args":    []any{"main.test.go", 3.0, true, nil, map[string]any{"deep": "/home/ada"}},
		"env":     map[string]any{"/home/ada-key": "12"},
		"extra":   "/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
	if !reflect.DeepEqual(cfg, before) {
		t.Errorf("the configuration was changed: %#v", cfg)
	}
}

func TestResolveConfigDoesNotSubstituteItself(t *testing.T) {
	vc := testContext()
	vc.Env = func(name string) (string, bool) {
		return map[string]string{"A": "${env:B}", "B": "${env:A}", "SELF": "${env:SELF}", "OK": "${env:LEAF}", "LEAF": "leaf"}[name], true
	}
	for in, want := range map[string]string{
		"${env:SELF}": "${env:SELF}",
		"${env:A}":    "${env:A}",
		"${env:OK}":   "leaf",
	} {
		got, ok, err := debug.ResolveString(context.Background(), vc, in, debug.ResolveOptions{})
		if err != nil || !ok || got != want {
			t.Errorf("ResolveString(%q) = %q, %v, %v; want %q", in, got, ok, err, want)
		}
	}
}

type memory map[string]string

func (m memory) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memory) Set(k, v string)             { m[k] = v }

func TestInteractiveVariables(t *testing.T) {
	inputs := []debug.Input{
		{ID: "who", Type: "promptString", Description: "Who?", Default: "me"},
		{ID: "secret", Type: "promptString", Description: "Secret", Password: true},
		{ID: "pick", Type: "pickString", Description: "Pick one", Default: "b", Options: []debug.InputOption{{Value: "a"}, {Label: "Bee", Value: "b"}, {Value: "c"}}},
		{ID: "cmd", Type: "command", Command: "ext.input", Args: "arg"},
	}
	var prompts []debug.PromptRequest
	var picks []debug.PickRequest
	var commands []string
	vc := testContext()
	vc.PromptString = func(_ context.Context, r debug.PromptRequest) (string, bool, error) {
		prompts = append(prompts, r)
		return "typed-" + r.ID, true, nil
	}
	vc.PickString = func(_ context.Context, r debug.PickRequest) (string, bool, error) {
		picks = append(picks, r)
		return "c", true, nil
	}
	vc.Command = func(_ context.Context, id string, arg any) (any, error) {
		commands = append(commands, id)
		switch id {
		case "ext.input":
			return "from-input:" + arg.(string), nil
		case "mapped.pick":
			return "1234", nil
		}
		return nil, nil
	}
	mem := memory{"launch.who": "remembered"}
	opts := debug.ResolveOptions{
		Folder: "/work/app", Interactive: true, Inputs: inputs, Section: "launch", Memory: mem,
		Commands: map[string]string{"pickProcess": "mapped.pick"},
	}
	got, ok, err := debug.ResolveString(context.Background(), vc, "${input:who} ${input:who} ${input:secret} ${input:pick} ${input:cmd} ${command:pickProcess}", opts)
	if err != nil || !ok {
		t.Fatalf("ResolveString: %v %v", ok, err)
	}
	if want := "typed-who typed-who typed-secret c from-input:arg 1234"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A variable is asked once however often it appears; the remembered value starts the prompt, the default comes after it.
	if len(prompts) != 2 || prompts[0].Value != "remembered" || prompts[1].Value != "" || !prompts[1].Password {
		t.Errorf("prompts = %+v", prompts)
	}
	// The default is first in the list of a pickString, with its label.
	if len(picks) != 1 || picks[0].Items[0].Value != "b" || picks[0].Items[0].Label != "Bee: b" || picks[0].Items[0].Description != "(Default)" || picks[0].Items[1].Value != "a" {
		t.Errorf("picks = %+v", picks)
	}
	// The values are remembered, except a password.
	if mem["launch.who"] != "typed-who" || mem["launch.pick"] != "c" {
		t.Errorf("memory = %v", mem)
	}
	if _, has := mem["launch.secret"]; has {
		t.Errorf("a password was remembered: %v", mem)
	}
	if want := []string{"ext.input", "mapped.pick"}; !reflect.DeepEqual(commands, want) {
		t.Errorf("commands = %v, want %v", commands, want)
	}

	// Without Interactive the variables stay.
	got, _, err = debug.ResolveString(context.Background(), vc, "${input:who} ${command:x}", debug.ResolveOptions{Inputs: inputs})
	if err != nil || got != "${input:who} ${command:x}" {
		t.Errorf("not interactive: %q, %v", got, err)
	}
}

func TestInteractiveVariablesCancelled(t *testing.T) {
	inputs := []debug.Input{{ID: "who", Type: "promptString", Description: "Who?"}, {ID: "pick", Type: "pickString", Description: "P", Options: []debug.InputOption{{Value: "a"}}}, {ID: "cmd", Type: "command", Command: "c"}}
	vc := testContext()
	vc.PromptString = func(context.Context, debug.PromptRequest) (string, bool, error) { return "", false, nil }
	vc.PickString = func(context.Context, debug.PickRequest) (string, bool, error) { return "", false, nil }
	vc.Command = func(context.Context, string, any) (any, error) { return nil, nil }
	for _, in := range []string{"${input:who}", "${input:pick}", "${input:cmd}", "${command:x}"} {
		got, ok, err := debug.ResolveString(context.Background(), vc, in, debug.ResolveOptions{Interactive: true, Inputs: inputs})
		if err != nil || ok || got != "" {
			t.Errorf("%s: got %q, ok %v, err %v; want a cancelled launch", in, got, ok, err)
		}
	}
	cfg, ok, err := debug.ResolveConfig(context.Background(), vc, debug.Config{"a": "${input:who}"}, debug.ResolveOptions{Interactive: true, Inputs: inputs})
	if err != nil || ok || cfg != nil {
		t.Errorf("config: %v %v %v", cfg, ok, err)
	}
}

func TestInteractiveVariableErrors(t *testing.T) {
	failing := os.ErrPermission // a foreign error
	app := apperrors.New("WindowCommandFailed", "the command failed")
	base := func() *debug.VariableContext {
		vc := testContext()
		vc.PromptString = func(context.Context, debug.PromptRequest) (string, bool, error) { return "x", true, nil }
		vc.PickString = func(context.Context, debug.PickRequest) (string, bool, error) { return "a", true, nil }
		vc.Command = func(_ context.Context, id string, _ any) (any, error) {
			switch id {
			case "number":
				return 42, nil
			case "boom":
				return nil, failing
			case "apperr":
				return nil, app
			}
			return "v", nil
		}
		return vc
	}
	pick := []debug.InputOption{{Value: "a"}}
	tests := []struct {
		name   string
		in     string
		inputs []debug.Input
		vc     func() *debug.VariableContext
		route  string
	}{
		{"command result is no string", "${command:number}", nil, base, "command.VariableCommandResultInvalid"},
		{"command fails", "${command:boom}", nil, base, "command.VariableCommandFailed"},
		{"input section missing", "${input:x}", nil, base, "input.InputSectionMissing"},
		{"input undefined", "${input:x}", []debug.Input{{ID: "y", Type: "promptString", Description: "d"}}, base, "input.InputUndefined"},
		{"prompt without description", "${input:x}", []debug.Input{{ID: "x", Type: "promptString"}}, base, "input.InputAttributeMissing"},
		{"pick without description", "${input:x}", []debug.Input{{ID: "x", Type: "pickString", Options: pick}}, base, "input.InputAttributeMissing"},
		{"pick without options", "${input:x}", []debug.Input{{ID: "x", Type: "pickString", Description: "d"}}, base, "input.InputAttributeMissing"},
		{"command input without command", "${input:x}", []debug.Input{{ID: "x", Type: "command"}}, base, "input.InputAttributeMissing"},
		{"unknown input type", "${input:x}", []debug.Input{{ID: "x", Type: "other"}}, base, "input.InputTypeUnknown"},
		{"command input result is no string", "${input:x}", []debug.Input{{ID: "x", Type: "command", Command: "number"}}, base, "input.InputCommandResultInvalid"},
		{"command input fails", "${input:x}", []debug.Input{{ID: "x", Type: "command", Command: "boom"}}, base, "input.VariableCommandFailed"},
		{"prompt fails", "${input:x}", []debug.Input{{ID: "x", Type: "promptString", Description: "d"}}, func() *debug.VariableContext {
			vc := base()
			vc.PromptString = func(context.Context, debug.PromptRequest) (string, bool, error) { return "", false, failing }
			return vc
		}, "input.InputPromptFailed"},
		{"pick fails", "${input:x}", []debug.Input{{ID: "x", Type: "pickString", Description: "d", Options: pick}}, func() *debug.VariableContext {
			vc := base()
			vc.PickString = func(context.Context, debug.PickRequest) (string, bool, error) { return "", false, failing }
			return vc
		}, "input.InputPromptFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := tt.inputs
			if tt.name != "input section missing" && inputs == nil {
				inputs = []debug.Input{}
			}
			_, ok, err := debug.ResolveString(context.Background(), tt.vc(), tt.in, debug.ResolveOptions{Interactive: true, Inputs: inputs})
			if err == nil || ok {
				t.Fatalf("ok %v, err %v; want an error", ok, err)
			}
			_ = wantRoute(t, err, tt.route)
		})
	}
	// An error the window already tagged is passed up as it is.
	vc := base()
	_, _, err := debug.ResolveString(context.Background(), vc, "${command:apperr}", debug.ResolveOptions{Interactive: true})
	if appErr := wantRoute(t, err, "WindowCommandFailed"); appErr.Tag != "WindowCommandFailed" {
		t.Errorf("tag = %s", appErr.Tag)
	}
}

func TestVariablesList(t *testing.T) {
	got := debug.Variables("a ${x} ${y:z} ${x} ${a${b}} ${")
	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if want := []string{"${x}", "${y:z}", "${a${b}}"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("Variables = %v, want %v", ids, want)
	}
	if got[1].Name != "y" || got[1].Arg != "z" || !got[1].HasArg || got[0].HasArg {
		t.Errorf("parts = %+v", got)
	}
}

func TestVariableCwdFailsWhenTheWorkingFolderIsGone(t *testing.T) {
	gone := t.TempDir()
	t.Chdir(gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	_, _, err := debug.ResolveString(context.Background(), &debug.VariableContext{}, "${cwd}", debug.ResolveOptions{})
	_ = wantRoute(t, err, "resolveBuiltin.VariableCwdFailed")
}
