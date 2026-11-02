package javascript

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The JavaScript debugger over the fake API (SPEC §10.31 "The debuggers", JavaScript and TypeScript; step 25.10): the manifest, the
// configuration providers, the adapter descriptor, the process picker and the download. No js-debug is run here: the tests of the real one
// carry the build tag emetos_jsdebug (debug_jsdebug_test.go).

// result is the answer of a provider that ran on a goroutine.
type result struct {
	cfg cfg
	err error
}

// resolveAsync runs the first resolution on a goroutine, as the host does (a provider may wait for the user).
func (r *rig) resolveAsync(c cfg) <-chan result {
	ch := make(chan result, 1)
	go func() {
		out, err := r.api.ResolveDebugConfiguration(context.Background(), DebugType, r.folder, c)
		ch <- result{out, err}
	}()
	return ch
}

// wait waits for the answer of a goroutine, pumping the posted work meanwhile.
func (r *rig) wait(ch <-chan result) result {
	r.t.Helper()
	var got result
	done := false
	r.api.WaitFor("the provider to answer", func() bool {
		select {
		case got = <-ch:
			done = true
		default:
		}
		return done
	})
	return got
}

// resolve is the first resolution of a configuration of the type node.
func (r *rig) resolve(c cfg) result { r.t.Helper(); return r.wait(r.resolveAsync(c)) }

// substitute is the second resolution, after the variables are substituted.
func (r *rig) substitute(c cfg) result {
	r.t.Helper()
	out, err := r.api.ResolveDebugConfigurationSubstituted(context.Background(), adapterType, r.folder, c)
	return result{out, err}
}

// adapter asks the factory for the adapter of a session with the configuration.
func (r *rig) adapter(c cfg) (*extension.DebugAdapter, error) {
	r.t.Helper()
	s := r.api.NewDebugSession("s1", adapterType, "js", c)
	s.SetFolder(r.folder)
	return r.api.DebugAdapter(context.Background(), adapterType, s, nil)
}

func TestTheNodeDebuggersAreDeclared(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatalf("the manifest is refused: %v", err)
	}
	if len(m.Contributes.Debuggers) != 2 {
		t.Fatalf("%d debuggers", len(m.Contributes.Debuggers))
	}
	d, old := m.Contributes.Debuggers[0], m.Contributes.Debuggers[1]
	if d.Type != "node" || d.Label != "Node.js" || !reflect.DeepEqual(d.Languages, []string{"javascript", "javascriptreact", "typescript", "typescriptreact"}) || !d.DynamicConfigurations {
		t.Errorf("debugger = %s %s %v %v", d.Type, d.Label, d.Languages, d.DynamicConfigurations)
	}
	// the old name serves no language, so that F5 with no launch.json has one debugger to choose
	if old.Type != "pwa-node" || len(old.Languages) != 0 || old.DynamicConfigurations || len(old.ConfigurationSnippets) != 0 {
		t.Errorf("old name = %+v", old)
	}
	if !reflect.DeepEqual(d.ConfigurationAttributes, old.ConfigurationAttributes) {
		t.Error("the two types do not have the same attributes")
	}
	props := func(request string) map[string]map[string]any {
		var schema struct {
			Properties map[string]map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(d.ConfigurationAttributes[request], &schema); err != nil {
			t.Fatalf("the %s attributes: %v", request, err)
		}
		return schema.Properties
	}
	launch, attach := props("launch"), props("attach")
	tests := []struct {
		request, name string
		have          map[string]map[string]any
		def           any
	}{
		{"launch", "program", launch, ""},
		{"launch", "console", launch, "internalConsole"},
		{"launch", "cwd", launch, "${workspaceFolder}"},
		{"launch", "runtimeExecutable", launch, "node"},
		{"launch", "runtimeArgs", launch, []any{}},
		{"launch", "args", launch, []any{}},
		{"launch", "outFiles", launch, []any{"${workspaceFolder}/**/*.(m|c|)js", "!**/node_modules/**"}},
		{"launch", "sourceMaps", launch, true},
		{"launch", "autoAttachChildProcesses", launch, true},
		{"attach", "port", attach, float64(9229)},
		{"attach", "address", attach, "localhost"},
		{"attach", "remoteRoot", attach, nil},
		{"attach", "localRoot", attach, nil},
	}
	for _, tt := range tests {
		p, ok := tt.have[tt.name]
		if !ok {
			t.Errorf("%s: no attribute %s", tt.request, tt.name)
			continue
		}
		if tt.def != nil && !reflect.DeepEqual(p["default"], tt.def) {
			t.Errorf("%s: %s defaults to %#v, want %#v", tt.request, tt.name, p["default"], tt.def)
		}
	}
	if enum, _ := launch["console"]["enum"].([]any); !reflect.DeepEqual(enum, []any{"internalConsole", "integratedTerminal", "externalTerminal"}) {
		t.Errorf("console enum = %v", enum)
	}
	// the snippets of js-debug's package.json, with its ^"..." strings, and the type of the debugger
	var labels []string
	for _, s := range d.ConfigurationSnippets {
		labels = append(labels, s.Label)
		if s.Body["type"] != "node" || s.Body["name"] == "" || (s.Body["request"] != "launch" && s.Body["request"] != "attach") {
			t.Errorf("snippet %q: body = %v", s.Label, s.Body)
		}
	}
	want := []string{"Node.js: Attach", "Node.js: Attach to Remote Program", "Node.js: Attach to Process", "Node.js: Launch Program", "Node.js: Launch via npm",
		"Node.js: Nodemon Setup", "Node.js: Mocha Tests", "Node.js: Yeoman generator", "Node.js: Gulp task", "Node.js: Electron Main"}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("snippets = %q", labels)
	}
	var program any
	for _, s := range d.ConfigurationSnippets {
		if s.Label == "Node.js: Launch Program" {
			program = s.Body["program"]
		}
	}
	if program != "^\"\\${workspaceFolder}/${1:app.js}\"" {
		t.Errorf("the program of the launch snippet = %#v", program)
	}
	// the setting, the command, the breakpoints and the topic
	var setting *extension.SettingSpec
	for i := range m.Contributes.Settings {
		if m.Contributes.Settings[i].Key == "debug.javascript.adapterPath" {
			setting = &m.Contributes.Settings[i]
		}
	}
	if setting == nil || setting.Type != "string" || setting.Default != "" {
		t.Errorf("setting = %+v", setting)
	}
	if !slices.ContainsFunc(m.Contributes.Commands, func(c extension.CommandSpec) bool {
		return c.ID == DownloadCommand && c.Title == "Download" && c.Category == "JavaScript Debugger"
	}) {
		t.Errorf("commands = %+v", m.Contributes.Commands)
	}
	var langs []string
	for _, b := range m.Contributes.Breakpoints {
		langs = append(langs, b.Language)
	}
	if !reflect.DeepEqual(langs, []string{"javascript", "javascriptreact", "typescript", "typescriptreact"}) {
		t.Errorf("breakpoint languages = %v", langs)
	}
	if !slices.Contains(m.Events.Subscribes, extension.TopicEditorActiveChanged) {
		t.Errorf("subscribes = %v", m.Events.Subscribes)
	}
}

func TestTheDebuggerIsRegisteredForBothTypes(t *testing.T) {
	r := newRig(t, rigOptions{})
	if got := r.api.DebugFactoryTypes(); !slices.Equal(got, []string{"node", "pwa-node"}) {
		t.Errorf("factories = %v", got)
	}
}

func TestAHostWithNoDebugServiceGivesTheExtensionNoDebugger(t *testing.T) {
	r := newRig(t, rigOptions{noDebug: true}) // the activation does not fail
	if got := r.api.DebugFactoryTypes(); len(got) != 0 {
		t.Errorf("factories = %v, want none", got)
	}
}

// ---- the first configuration, and the dynamic ones

func TestTheConfigurationANewLaunchFileStartsWith(t *testing.T) {
	tests := []struct {
		name             string
		files            map[string]string
		editor, language string
		want             cfg
	}{
		{"nothing to go by: the file in front, which is asked for at the start", nil, "", "",
			cfg{"name": "Launch Program", "type": "node", "request": "launch", "program": "${file}", "skipFiles": []any{"<node_internals>/**"}}},
		{"index.js", map[string]string{"index.js": ""}, "", "", cfg{"program": "${workspaceFolder}/index.js"}},
		{"main.js comes after index.js", map[string]string{"main.js": "", "index.js": ""}, "", "", cfg{"program": "${workspaceFolder}/index.js"}},
		{"main.js", map[string]string{"main.js": ""}, "", "", cfg{"program": "${workspaceFolder}/main.js"}},
		{"the main of package.json", map[string]string{"package.json": `{"main": "lib/app.js"}`, "lib/app.js": ""}, "", "", cfg{"program": "${workspaceFolder}/lib/app.js"}},
		{"a main without the extension", map[string]string{"package.json": `{"main": "lib/app"}`, "lib/app.js": ""}, "", "", cfg{"program": "${workspaceFolder}/lib/app"}},
		{"a main that is not there", map[string]string{"package.json": `{"main": "gone.js"}`, "index.js": ""}, "", "", cfg{"program": "${workspaceFolder}/index.js"}},
		{"the start script", map[string]string{"package.json": `{"scripts": {"start": "node server.js"}}`, "server.js": ""}, "", "", cfg{"program": "${workspaceFolder}/server.js"}},
		{"package.json that is not JSON", map[string]string{"package.json": `{`, "index.js": ""}, "", "", cfg{"program": "${workspaceFolder}/index.js"}},
		{"the JavaScript file in front", nil, "src/run.js", "javascript", cfg{"program": "${workspaceFolder}/src/run.js"}},
		{"the TypeScript file in front: its output is in outFiles", map[string]string{"tsconfig.json": `{"compilerOptions": {"outDir": "./out"}}`}, "src/run.ts", "typescript",
			cfg{"program": "${workspaceFolder}/src/run.ts", "outFiles": []any{"${workspaceFolder}/out/**/*.js"}}},
		{"TypeScript with a tsconfig with comments", map[string]string{"tsconfig.json": "{\n // where it goes\n \"compilerOptions\": {\"outDir\": \"dist/\", /* x */},\n}"}, "a.ts", "typescript",
			cfg{"program": "${workspaceFolder}/a.ts", "outFiles": []any{"${workspaceFolder}/dist/**/*.js"}}},
		{"TypeScript with no tsconfig", nil, "a.tsx", "typescriptreact", cfg{"program": "${workspaceFolder}/a.tsx", "outFiles": []any{"${workspaceFolder}/**/*.js"}}},
		{"a file that is not a program", map[string]string{"index.js": ""}, "notes.md", "markdown", cfg{"program": "${workspaceFolder}/index.js"}},
		{"package.json before the file in front", map[string]string{"package.json": `{"main": "index.js"}`, "index.js": ""}, "other.js", "javascript", cfg{"program": "${workspaceFolder}/index.js"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{files: tt.files, editor: tt.editor, language: tt.language})
			got, err := r.api.ProvideDebugConfigurations(context.Background(), DebugType, r.folder, extension.DebugTriggerInitial)
			if err != nil || len(got) != 1 {
				t.Fatalf("configurations = %v, %v", got, err)
			}
			for k, v := range tt.want {
				if !reflect.DeepEqual(got[0][k], v) {
					t.Errorf("%s = %#v, want %#v", k, got[0][k], v)
				}
			}
			if got[0]["name"] != "Launch Program" || got[0]["type"] != "node" || got[0]["request"] != "launch" {
				t.Errorf("configuration = %v", got[0])
			}
		})
	}
}

func TestTheDynamicConfigurationsListTheScriptsAndTheFileInFront(t *testing.T) {
	tests := []struct {
		name             string
		files            map[string]string
		editor, language string
		want             []string
		check            func(t *testing.T, list []extension.DebugConfiguration)
	}{
		{"nothing", nil, "", "", nil, nil},
		{"the scripts, in the order of the file", map[string]string{"package.json": `{"scripts": {"start": "node a.js", "build": "tsc", "test": "mocha"}}`}, "", "",
			[]string{"Run Script: start", "Run Script: build", "Run Script: test"}, func(t *testing.T, list []extension.DebugConfiguration) {
				c := list[1]
				if c["type"] != "node" || c["request"] != "launch" || c["runtimeExecutable"] != "npm" || !reflect.DeepEqual(c["runtimeArgs"], []any{"run", "build"}) || c["console"] != "integratedTerminal" {
					t.Errorf("configuration = %v", c)
				}
				if _, has := c["cwd"]; has {
					t.Errorf("a script of the folder has a cwd: %v", c)
				}
			}},
		{"a script that is not text is no script", map[string]string{"package.json": `{"scripts": {"a": "x", "b": 3, "c": {"d": 1}}}`}, "", "", []string{"Run Script: a"}, nil},
		{"yarn", map[string]string{"package.json": `{"scripts": {"dev": "x"}}`, "yarn.lock": ""}, "", "", []string{"Run Script: dev"}, func(t *testing.T, list []extension.DebugConfiguration) {
			if list[0]["runtimeExecutable"] != "yarn" {
				t.Errorf("runner = %v", list[0]["runtimeExecutable"])
			}
		}},
		{"pnpm", map[string]string{"package.json": `{"scripts": {"dev": "x"}}`, "pnpm-lock.yaml": ""}, "", "", []string{"Run Script: dev"}, func(t *testing.T, list []extension.DebugConfiguration) {
			if list[0]["runtimeExecutable"] != "pnpm" {
				t.Errorf("runner = %v", list[0]["runtimeExecutable"])
			}
		}},
		{"bun", map[string]string{"package.json": `{"scripts": {"dev": "x"}}`, "bun.lock": ""}, "", "", []string{"Run Script: dev"}, func(t *testing.T, list []extension.DebugConfiguration) {
			if list[0]["runtimeExecutable"] != "bun" {
				t.Errorf("runner = %v", list[0]["runtimeExecutable"])
			}
		}},
		{"several packages: the folder's own first, the others named by their folders", map[string]string{
			"package.json": `{"scripts": {"a": "x"}}`, "web/package.json": `{"scripts": {"b": "x"}}`, "api/package.json": `{"scripts": {"c": "x"}}`,
			"node_modules/dep/package.json": `{"scripts": {"never": "x"}}`, ".hidden/package.json": `{"scripts": {"never": "x"}}`,
		}, "", "", []string{"Run Script: a (work)", "Run Script: c (api)", "Run Script: b (web)"}, func(t *testing.T, list []extension.DebugConfiguration) {
			if list[0]["cwd"] != nil || !strings.HasPrefix(list[1]["cwd"].(string), "${workspaceFolder}/api") {
				t.Errorf("cwd = %v and %v", list[0]["cwd"], list[1]["cwd"])
			}
		}},
		{"the file in front", nil, "src/a.js", "javascript", []string{"Run Current File"}, func(t *testing.T, list []extension.DebugConfiguration) {
			if list[0]["program"] != "${workspaceFolder}/src/a.js" || list[0]["type"] != "node" {
				t.Errorf("configuration = %v", list[0])
			}
		}},
		{"a file that cannot be debugged", nil, "a.md", "markdown", nil, nil},
		{"scripts, then the file", map[string]string{"package.json": `{"scripts": {"a": "x"}}`}, "a.ts", "typescript", []string{"Run Script: a", "Run Current File"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{files: tt.files, editor: tt.editor, language: tt.language})
			list, err := r.api.ProvideDebugConfigurations(context.Background(), DebugType, r.folder, extension.DebugTriggerDynamic)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, c := range list {
				names = append(names, c["name"].(string))
			}
			if !slices.Equal(names, tt.want) {
				t.Fatalf("names = %q, want %q", names, tt.want)
			}
			if tt.check != nil {
				tt.check(t, list)
			}
		})
	}
}

func TestADirectoryTreeThatIsHugeIsNotWalkedForever(t *testing.T) {
	r := newRig(t, rigOptions{})
	for i := 0; i < 30; i++ {
		if err := os.MkdirAll(filepath.Join(r.folder, "d"+strconv.Itoa(i), "e", "f", "g", "h"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, r.folder, "d3/e/f/g/package.json", `{"scripts": {"deep": "x"}}`, 0o644)
	writeFile(t, r.folder, "d4/e/f/g/h/package.json", `{"scripts": {"too-deep": "x"}}`, 0o644)
	got := findScripts(r.folder)
	if len(got) != 1 || got[0].name != "deep" {
		t.Errorf("scripts = %v, want only deep (four levels down is the limit)", got)
	}
}

// ---- the first resolution

func TestTheFirstResolutionCompletesAConfiguration(t *testing.T) {
	tests := []struct {
		name             string
		files            map[string]string
		editor, language string
		in               cfg
		want             cfg
		absent           []string
	}{
		{"the type is the adapter's", nil, "", "", cfg{"name": "L", "type": "node", "request": "launch", "program": "a.js"},
			cfg{"type": "pwa-node", "cwd": "@folder", "program": "a.js"}, []string{"internalConsoleOptions", "localRoot"}},
		{"the old name stays what the adapter dispatches on", nil, "", "", cfg{"name": "L", "type": "pwa-node", "request": "launch", "program": "a.js"}, cfg{"type": "pwa-node"}, nil},
		{"a cwd the user wrote stays", nil, "", "", cfg{"name": "L", "type": "node", "request": "launch", "cwd": "${workspaceFolder}/sub"}, cfg{"cwd": "${workspaceFolder}/sub"}, nil},
		{"the local root is the cwd of a remote attach", nil, "", "", cfg{"name": "A", "type": "node", "request": "attach", "localRoot": "/src", "remoteRoot": "/app"},
			cfg{"cwd": "/src", "localRoot": "/src"}, nil},
		{"a remote root gets the folder as its local root", nil, "", "", cfg{"name": "A", "type": "node", "request": "attach", "remoteRoot": "/app"},
			cfg{"localRoot": "${workspaceFolder}"}, nil},
		{"the terminal does not open the Debug Console", nil, "", "", cfg{"name": "L", "type": "node", "request": "launch", "program": "a.js", "console": "integratedTerminal"},
			cfg{"internalConsoleOptions": "neverOpen"}, nil},
		{"unless the user said", nil, "", "", cfg{"name": "L", "type": "node", "request": "launch", "program": "a.js", "console": "integratedTerminal", "internalConsoleOptions": "openOnSessionStart"},
			cfg{"internalConsoleOptions": "openOnSessionStart"}, nil},
		{"--inspect-brk is stopOnEntry", nil, "", "", cfg{"name": "L", "type": "node", "request": "launch", "program": "a.js", "runtimeArgs": []any{"--inspect-brk", "--no-warnings", "--inspect-brk=9230"}},
			cfg{"stopOnEntry": true, "runtimeArgs": []any{"--no-warnings"}}, nil},
		{"--inspect-brk stays when there is a port to attach to", nil, "", "", cfg{"name": "L", "type": "node", "request": "launch", "attachSimplePort": float64(9230), "runtimeArgs": []any{"--inspect-brk=9230"}},
			cfg{"runtimeArgs": []any{"--inspect-brk=9230"}}, []string{"stopOnEntry"}},
		{"a child session is left as it is", nil, "", "", cfg{"name": "child", "type": "pwa-node", "request": "launch", "__pendingTargetId": "t1", "console": "integratedTerminal"},
			cfg{"__pendingTargetId": "t1"}, []string{"cwd", "internalConsoleOptions"}},
		{"F5 with no launch.json launches the file in front", nil, "run.js", "javascript", cfg{},
			cfg{"name": "Launch Program", "type": "pwa-node", "request": "launch", "program": "${workspaceFolder}/run.js", "cwd": "@folder"}, nil},
		{"F5 launches the index of the project", map[string]string{"index.js": ""}, "", "", cfg{"noDebug": true},
			cfg{"program": "${workspaceFolder}/index.js", "noDebug": true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{files: tt.files, editor: tt.editor, language: tt.language})
			got := r.resolve(tt.in)
			if got.err != nil || got.cfg == nil {
				t.Fatalf("resolved to %v, %v", got.cfg, got.err)
			}
			for k, v := range tt.want {
				if s, ok := v.(string); ok {
					v = strings.ReplaceAll(s, "@folder", r.folder)
				}
				if !reflect.DeepEqual(got.cfg[k], v) {
					t.Errorf("%s = %#v, want %#v", k, got.cfg[k], v)
				}
			}
			for _, k := range tt.absent {
				if _, has := got.cfg[k]; has {
					t.Errorf("%s is there: %v", k, got.cfg[k])
				}
			}
			if _, had := tt.in["name"]; had && tt.in["name"] != got.cfg["name"] {
				t.Errorf("name = %v", got.cfg["name"])
			}
		})
	}
}

func TestTheFirstResolutionOfAnEmptyConfigurationNeedsAProgram(t *testing.T) {
	r := newRig(t, rigOptions{})
	got := r.resolve(cfg{})
	appErr, ok := errors.FromError(got.err)
	if !ok || !appErr.HasRoute("resolveDebug.JsDebugNoProgram") {
		t.Errorf("err = %v, want the route resolveDebug.JsDebugNoProgram (and cfg %v)", got.err, got.cfg)
	}
}

// ---- the second resolution

func TestTheSecondResolution(t *testing.T) {
	tests := []struct {
		name   string
		files  map[string]string
		in     cfg
		want   cfg
		absent []string
		route  string
	}{
		{"the workspace folder is told to js-debug", nil, cfg{"request": "launch", "program": "a.js"}, cfg{"__workspaceFolder": "@folder"}, nil, ""},
		{"a workspace folder the user gave stays", nil, cfg{"request": "launch", "__workspaceFolder": "/mine"}, cfg{"__workspaceFolder": "/mine"}, nil, ""},
		{"a port that is text is a number", nil, cfg{"request": "attach", "port": " 9230 "}, cfg{"port": float64(9230)}, nil, ""},
		{"attachSimplePort that is text is a number", nil, cfg{"request": "launch", "attachSimplePort": "9231"}, cfg{"attachSimplePort": float64(9231)}, nil, ""},
		{"a port that is not a number", nil, cfg{"request": "attach", "port": "http"}, nil, nil, "resolveDebugSubstituted.JsDebugPortInvalid"},
		{"a cwd that is a folder", map[string]string{"sub/x": ""}, cfg{"request": "launch", "cwd": "@folder/sub"}, cfg{"cwd": "@folder/sub"}, nil, ""},
		{"a cwd that is not there", nil, cfg{"request": "launch", "cwd": "@folder/nope"}, nil, nil, "resolveDebugSubstituted.JsDebugCwdInvalid"},
		{"a cwd that is a file", map[string]string{"f": ""}, cfg{"request": "launch", "cwd": "@folder/f"}, nil, nil, "resolveDebugSubstituted.JsDebugCwdInvalid"},
		{"an attach to a process whose debug port is known", nil, cfg{"request": "attach", "processId": "4242:9230"}, cfg{"port": float64(9230)}, []string{"processId"}, ""},
		{"an attach to a process that is no number", nil, cfg{"request": "attach", "processId": "node"}, nil, nil, "resolveProcessID.JsDebugProcessInvalid"},
		{"an attach to a process with a port that is none", nil, cfg{"request": "attach", "processId": "42:http"}, nil, nil, "resolveProcessID.JsDebugProcessInvalid"},
		{"an attach to a process with a port out of range", nil, cfg{"request": "attach", "processId": "42:70000"}, nil, nil, "resolveProcessID.JsDebugProcessInvalid"},
		{"an attach to a process id that is not a process id", nil, cfg{"request": "attach", "processId": true}, nil, nil, "resolveProcessID.JsDebugProcessInvalid"},
		{"a child session is left as it is", nil, cfg{"request": "launch", "__pendingTargetId": "t", "cwd": "@folder/nope", "port": "x"}, cfg{"port": "x"}, []string{"__workspaceFolder"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{files: tt.files})
			subst := func(v any) any {
				if s, ok := v.(string); ok {
					return strings.ReplaceAll(s, "@folder", r.folder)
				}
				return v
			}
			in := cfg{}
			for k, v := range tt.in {
				in[k] = subst(v)
			}
			got := r.substitute(in)
			if tt.route != "" {
				appErr, ok := errors.FromError(got.err)
				if !ok || !appErr.HasRoute(tt.route) {
					t.Errorf("err = %v, want the route %s", got.err, tt.route)
				}
				return
			}
			if got.err != nil {
				t.Fatal(got.err)
			}
			for k, v := range tt.want {
				if !reflect.DeepEqual(got.cfg[k], subst(v)) {
					t.Errorf("%s = %#v, want %#v", k, got.cfg[k], subst(v))
				}
			}
			for _, k := range tt.absent {
				if _, has := got.cfg[k]; has {
					t.Errorf("%s is there", k)
				}
			}
		})
	}
}

func TestAnAttachToAProcessPutsItIntoDebugMode(t *testing.T) {
	// the process is the test itself: SIGUSR1 is what node answers by listening on 9229, here it is only caught
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	defer signal.Stop(ch)
	r := newRig(t, rigOptions{})
	got := r.substitute(cfg{"request": "attach", "processId": strconv.Itoa(os.Getpid())})
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.cfg["port"] != float64(9229) {
		t.Errorf("port = %v, want 9229", got.cfg["port"])
	}
	if _, has := got.cfg["processId"]; has {
		t.Error("processId is still there")
	}
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Error("the process was not sent SIGUSR1")
	}
	// a number in the configuration is the same
	got = r.substitute(cfg{"request": "attach", "processId": float64(os.Getpid())})
	if got.err != nil || got.cfg["port"] != float64(9229) {
		t.Errorf("a numeric processId gave %v, %v", got.cfg, got.err)
	}
	// a process that is not there cannot be put into debug mode
	got = r.substitute(cfg{"request": "attach", "processId": "2147483646"})
	appErr, ok := errors.FromError(got.err)
	if !ok || !appErr.HasRoute("resolveProcessID.JsDebugProcessDebugFailed") {
		t.Errorf("err = %v, want the route resolveProcessID.JsDebugProcessDebugFailed", got.err)
	}
}

func TestAnalyseArguments(t *testing.T) {
	tests := []struct {
		args []string
		want int
	}{
		{nil, 0},
		{[]string{"app.js"}, 0},
		{[]string{"--inspect", "app.js"}, 9229},
		{[]string{"--inspect-brk", "app.js"}, 9229},
		{[]string{"--inspect=9230", "app.js"}, 9230},
		{[]string{"--inspect-brk=0.0.0.0:9231"}, 9231},
		{[]string{"--inspect=127.0.0.1:9232"}, 9232},
		{[]string{"--inspect-port=9233"}, 9233},
		{[]string{"--debug-port=5858"}, 5858},
		{[]string{"--inspect", "--inspect-port=9234"}, 9234},
		{[]string{"--inspect=notaport"}, 0},
		{[]string{"--inspect-port"}, 0},
		{[]string{"--inspect=99999"}, 0},
		{[]string{"app.js", "--inspect=9235"}, 9235},
	}
	for _, tt := range tests {
		if got := analyseArguments(tt.args); got != tt.want {
			t.Errorf("analyseArguments(%q) = %d, want %d", tt.args, got, tt.want)
		}
	}
}

// ---- the process picker

// fakeProc writes a process below the fake /proc.
func (r *rig) fakeProc(pid int, words ...string) {
	r.t.Helper()
	writeFile(r.t, r.proc, strconv.Itoa(pid)+"/cmdline", strings.Join(words, "\x00")+"\x00", 0o644)
}

func TestTheProcessPickerListsTheNodeProcesses(t *testing.T) {
	tests := []struct {
		name      string
		processes func(r *rig)
		choose    int
		want      string
	}{
		{"a node process", func(r *rig) {
			r.fakeProc(10, "/usr/bin/bash")
			r.fakeProc(20, "/usr/bin/node", "server.js")
			r.fakeProc(30, "/usr/bin/python3", "x.py")
		}, 0, "20"},
		{"a process with a debug port that is not called node", func(r *rig) {
			r.fakeProc(30, "/opt/electron/electron", "--inspect=9333", "main.js")
			r.fakeProc(40, "/usr/bin/node", "app.js")
		}, 0, "30:9333"},
		{"the second row, in the order of the process ids", func(r *rig) {
			r.fakeProc(300, "node", "b.js")
			r.fakeProc(200, "node", "a.js")
		}, 1, "300"},
		{"nodejs and iojs are node", func(r *rig) {
			r.fakeProc(50, "iojs", "a.js")
		}, 0, "50"},
		{"a process whose name only starts like node is not one", func(r *rig) {
			r.fakeProc(50, "/usr/bin/nodemon-helper")
			r.fakeProc(60, "node", "x.js")
		}, 0, "60"},
		{"a bare --inspect is 9229", func(r *rig) {
			r.fakeProc(70, "node", "--inspect", "a.js")
		}, 0, "70:9229"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			tt.processes(r)
			ch := r.resolveAsync(cfg{"name": "A", "type": "node", "request": "attach", "processId": "${command:PickProcess}"})
			r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
			pick := r.api.LastPick()
			if pick.Options().Placeholder != "Pick the node.js process to attach to" || !pick.Options().MatchOnDescription {
				t.Errorf("options = %+v", pick.Options())
			}
			pick.Choose(tt.choose)
			got := r.wait(ch)
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.cfg["processId"] != tt.want {
				t.Errorf("processId = %v, want %s", got.cfg["processId"], tt.want)
			}
		})
	}
}

func TestTheProcessPickerShowsWhatItListsAndWhatItFails(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.fakeProc(20, "/usr/bin/node", "--inspect=9230", "server.js", "--port", "80")
	r.fakeProc(os.Getpid(), "/usr/bin/node", "the-emetos-process.js")
	ch := r.resolveAsync(cfg{"request": "attach", "processId": "${command:PickProcess}"})
	r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
	items := r.api.LastPick().Options().Items
	if len(items) != 1 {
		t.Fatalf("items = %+v, want one (this process is not listed)", items)
	}
	if items[0].Label != "node" || items[0].Description != "--inspect=9230 server.js --port 80" || items[0].Detail != "process id: 20, debug port: 9230 (SIGUSR1)" {
		t.Errorf("item = %+v", items[0])
	}
	r.api.LastPick().Close()
	if got := r.wait(ch); got.err != nil || got.cfg != nil {
		t.Errorf("a closed list gave %v, %v, want a launch that is cancelled", got.cfg, got.err)
	}

	r2 := newRig(t, rigOptions{})
	got := r2.resolve(cfg{"request": "attach", "processId": "${command:PickProcess}"})
	appErr, ok := errors.FromError(got.err)
	if !ok || !appErr.HasRoute("pickProcess.JsDebugNoProcess") {
		t.Errorf("err = %v, want the route pickProcess.JsDebugNoProcess", got.err)
	}
}

func TestTheProcessPickerFailsWhenTheHostCannotShowAList(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.fakeProc(20, "node", "a.js")
	failing := failingAPI{API: r.api}
	r.ext.api = failing
	_, _, err := r.ext.pickProcess(context.Background())
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("pickProcess.JsDebugPickFailed") {
		t.Errorf("err = %v, want the route pickProcess.JsDebugPickFailed", err)
	}
}

// ---- the adapter

func TestTheAdapterIsJsDebugsServerRunByNode(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, r *rig) (setting string)
	}{
		{"js-debug is at the setting, as the program", func(t *testing.T, r *rig) string {
			return writeFile(t, r.base, "x/dapDebugServer.js", "", 0o644)
		}},
		{"js-debug is at the setting, as a folder", func(t *testing.T, r *rig) string {
			fakeJsDebug(t, filepath.Join(r.base, "x"))
			return filepath.Join(r.base, "x", "js-debug")
		}},
		{"js-debug is at the setting, as the folder that holds the release", func(t *testing.T, r *rig) string {
			fakeJsDebug(t, filepath.Join(r.base, "x"))
			return filepath.Join(r.base, "x")
		}},
		{"js-debug is at the setting, relative to the folder", func(t *testing.T, r *rig) string {
			fakeJsDebug(t, r.folder)
			return "js-debug"
		}},
		{"js-debug is the download", func(t *testing.T, r *rig) string {
			fakeJsDebug(t, filepath.Join(r.dataDir(), JsDebugVersion))
			return ""
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			if setting := tt.setup(t, r); setting != "" {
				r.api.SetSetting(AdapterPathSetting, setting)
			}
			a, err := r.adapter(cfg{"request": "launch"})
			if err != nil {
				t.Fatal(err)
			}
			if a.Kind != extension.DebugAdapterExecutableServer || a.Command != filepath.Join(r.bin, "node") || !a.ServesChildren {
				t.Errorf("adapter = %+v", a)
			}
			if len(a.Args) != 3 || filepath.Base(a.Args[0]) != "dapDebugServer.js" || !filepath.IsAbs(a.Args[0]) || a.Args[1] != "0" || a.Args[2] != "127.0.0.1" {
				t.Errorf("args = %q, want the server program, a free port and this machine", a.Args)
			}
			if a.Cwd != filepath.Dir(a.Args[0]) {
				t.Errorf("cwd = %s", a.Cwd)
			}
			if len(r.noteList()) != 0 {
				t.Errorf("notifications = %v", r.noteList())
			}
		})
	}
}

func TestTheNodeThatRunsJsDebug(t *testing.T) {
	tests := []struct {
		name     string
		opts     rigOptions
		setting  string
		makeNode func(t *testing.T, r *rig) string
		want     string
		route    string
		note     string
		copyCmd  string
	}{
		{"the first node in PATH", rigOptions{}, "", nil, "@bin/node", "", "", ""},
		{"typescript.nodePath as a path", rigOptions{}, "@base/other/node", func(t *testing.T, r *rig) string {
			return writeFile(t, r.base, "other/node", "#!/bin/sh\necho v20.5.0\n", 0o755)
		}, "@base/other/node", "", "", ""},
		{"typescript.nodePath as a name in PATH", rigOptions{}, "node", nil, "@bin/node", "", "", ""},
		{"typescript.nodePath that is not there", rigOptions{os: "ID=fedora\n"}, "@base/missing/node", nil, "",
			"findNode.JsDebugNodeInvalid", "does not exist", "sudo dnf install nodejs"},
		{"typescript.nodePath that is not executable", rigOptions{}, "@base/plain", func(t *testing.T, r *rig) string {
			return writeFile(t, r.base, "plain", "x", 0o644)
		}, "", "findNode.JsDebugNodeInvalid", "is not executable", ""},
		{"typescript.nodePath that is a name not in PATH", rigOptions{os: "ID=ubuntu\n"}, "nodejs", nil, "", "findNode.JsDebugNodeInvalid", "not a program in PATH", "sudo apt install nodejs"},
		{"no node", rigOptions{noNode: true, os: "ID=arch\n"}, "", nil, "", "findNode.JsDebugNodeNotFound", "node was not found", "sudo pacman -S nodejs"},
		{"a node that is too old", rigOptions{node: "v16.20.2", os: "ID=opensuse-leap\nID_LIKE=\"suse opensuse\"\n"}, "", nil, "", "checkNode.JsDebugNodeTooOld", "too old", "sudo zypper install nodejs"},
		{"node 18 is enough", rigOptions{node: "v18.0.0"}, "", nil, "@bin/node", "", "", ""},
		{"a node that prints no version", rigOptions{node: "oops"}, "", nil, "", "checkNode.JsDebugNodeFailed", "cannot be run", ""},
		{"a system we have no command for", rigOptions{noNode: true, os: "ID=plan9\n"}, "", nil, "", "findNode.JsDebugNodeNotFound", "node was not found", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.opts)
			fakeJsDebug(t, filepath.Join(r.dataDir(), JsDebugVersion))
			if tt.makeNode != nil {
				tt.makeNode(t, r)
			}
			if tt.setting != "" {
				r.api.SetSetting(NodePathSetting, strings.ReplaceAll(strings.ReplaceAll(tt.setting, "@base", r.base), "@bin", r.bin))
			}
			a, err := r.adapter(cfg{"request": "launch"})
			if tt.route != "" {
				appErr, ok := errors.FromError(err)
				if !ok || !appErr.HasRoute(tt.route) {
					t.Fatalf("err = %v, want the route %s", err, tt.route)
				}
				if !r.noteContaining(tt.note) {
					t.Errorf("notifications = %v, want one with %q", r.noteList(), tt.note)
				}
				notes := r.api.Notes()
				if len(notes) != 1 {
					t.Fatalf("%d notifications", len(notes))
				}
				if tt.copyCmd != "" {
					if !slices.Equal(notes[0].Actions, []string{"Copy Install Command"}) {
						t.Fatalf("actions = %v", notes[0].Actions)
					}
					r.api.RunNoteAction(0, "Copy Install Command")
					if got := r.api.ClipboardTexts(); !slices.Equal(got, []string{tt.copyCmd}) {
						t.Errorf("clipboard = %q, want %q", got, tt.copyCmd)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.ReplaceAll(strings.ReplaceAll(tt.want, "@base", r.base), "@bin", r.bin); a.Command != want {
				t.Errorf("node = %s, want %s", a.Command, want)
			}
		})
	}
}

func TestAMissingJsDebugIsToldWithAButtonThatDownloadsIt(t *testing.T) {
	r := newRig(t, rigOptions{})
	rel, count := serve(t, archiveOf(t, goodMembers()...), http.StatusOK)
	r.ext.release = rel
	a, err := r.adapter(cfg{"request": "launch"})
	if a != nil {
		t.Errorf("adapter = %+v", a)
	}
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("debugAdapter.JsDebugNotFound") {
		t.Fatalf("err = %v, want the route debugAdapter.JsDebugNotFound", err)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "js-debug was not found") ||
		!strings.Contains(notes[0].Message, "debug.javascript.adapterPath") || !slices.Equal(notes[0].Actions, []string{"Download"}) {
		t.Fatalf("notifications = %+v", notes)
	}
	// nothing was fetched: only the click does
	if n := count.Load(); n != 0 {
		t.Fatalf("%d requests before the click", n)
	}
	r.api.RunNoteAction(0, "Download")
	r.api.WaitFor("the download to end", func() bool { r.api.Pump(); return r.noteContaining("was downloaded") })
	if count.Load() != 1 {
		t.Errorf("%d requests after the click, want 1", count.Load())
	}
	// the release is below the data folder of the test
	want := filepath.Join(r.dataDir(), "9.9.9", "js-debug", "src", "dapDebugServer.js")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the release is not at %s: %v", want, err)
	}
	if !r.noteContaining("Downloading js-debug 9.9.9") || !r.noteContaining(filepath.Join(r.dataDir(), "9.9.9", "js-debug")) {
		t.Errorf("notifications = %v", r.noteList())
	}
}

func TestAJsDebugSettingThatIsNotJsDebugIsToldToo(t *testing.T) {
	r := newRig(t, rigOptions{settings: map[string]any{AdapterPathSetting: "/nowhere/js-debug"}})
	_, err := r.adapter(cfg{"request": "launch"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("debugAdapter->locate.JsDebugPathInvalid") {
		t.Fatalf("err = %v, want the route debugAdapter->locate.JsDebugPathInvalid", err)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || !strings.Contains(notes[0].Message, "/nowhere/js-debug") || !slices.Equal(notes[0].Actions, []string{"Download"}) {
		t.Errorf("notifications = %+v", notes)
	}
}

// ---- the download command

func TestTheDownloadCommandFetchesTheReleaseOnTheUsersClickOnly(t *testing.T) {
	good := archiveOf(t, goodMembers()...)
	tests := []struct {
		name   string
		status int
		bad    bool
		note   string
	}{
		{"a good release", http.StatusOK, false, "js-debug 9.9.9 was downloaded to"},
		{"a checksum that does not match", http.StatusOK, true, "JsDebugChecksumMismatch"},
		{"a server that says no", http.StatusNotFound, false, "JsDebugDownloadFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			rel, hits := serve(t, good, tt.status)
			if tt.bad {
				rel.SHA256 = strings.Repeat("a", 64)
			}
			r.ext.release = rel
			r.api.Pump()
			if hits.Load() != 0 {
				t.Fatalf("%d requests before the command ran", hits.Load())
			}
			if err := r.api.Run(DownloadCommand, nil); err != nil {
				t.Fatal(err)
			}
			r.api.WaitFor("the download to end", func() bool { r.api.Pump(); return r.noteContaining(tt.note) })
			if hits.Load() != 1 {
				t.Errorf("%d requests, want 1", hits.Load())
			}
			_, statErr := os.Stat(filepath.Join(r.dataDir(), "9.9.9", "js-debug", "src", "dapDebugServer.js"))
			if (statErr == nil) != (tt.note[0] == 'j') {
				t.Errorf("the release is there: %v, notifications %v", statErr == nil, r.noteList())
			}
			if tt.note[0] != 'j' {
				found := false
				for _, n := range r.api.Notes() {
					found = found || (n.Severity == "error" && strings.Contains(n.Message, tt.note))
				}
				if !found {
					t.Errorf("no error notification with %s: %v", tt.note, r.noteList())
				}
			}
		})
	}
}

func TestASecondClickWhileTheDownloadRunsOnlySaysSo(t *testing.T) {
	r := newRig(t, rigOptions{})
	open := make(chan struct{})
	var hits atomic.Int32
	r.ext.release = newBlockingServer(t, &hits, open)
	r.ext.startDownload()
	r.api.WaitFor("the request", func() bool { return hits.Load() >= 1 })
	r.ext.startDownload()
	if !r.noteContaining("is being downloaded already") {
		t.Errorf("notifications = %v", r.noteList())
	}
	close(open)
	r.api.WaitFor("the download to end", func() bool { r.api.Pump(); return !r.ext.downloading.Load() })
	r.api.Pump()
	if hits.Load() != 1 {
		t.Errorf("%d requests, want 1", hits.Load())
	}
}

func TestTheDataFolderFollowsTheXDGVariable(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "xdg"))
	got, err := dataDirFromEnv()
	if err != nil || got != filepath.Join(base, "xdg", "emetos", "js-debug") {
		t.Errorf("dataDirFromEnv = %q, %v", got, err)
	}
	t.Setenv("XDG_DATA_HOME", "relative/path")
	t.Setenv("HOME", filepath.Join(base, "home"))
	got, err = dataDirFromEnv()
	if err != nil || got != filepath.Join(base, "home", ".local", "share", "emetos", "js-debug") {
		t.Errorf("with a relative XDG_DATA_HOME: %q, %v", got, err)
	}
}

// failingAPI is the fake API whose quick pick cannot be shown.
type failingAPI struct {
	extension.API
}

type failingPick struct{ extension.QuickPick }

func (failingPick) Show(extension.QuickPickOptions, func(int)) error {
	return errors.New("TestPickUnavailable", "no quick pick")
}

// QuickPick implements extension.API.
func (f failingAPI) QuickPick() extension.QuickPick { return failingPick{f.API.QuickPick()} }

// newBlockingServer serves a good release, but only once release is closed; it counts the requests.
func newBlockingServer(t *testing.T, hits *atomic.Int32, open <-chan struct{}) release {
	t.Helper()
	body := archiveOf(t, goodMembers()...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-open:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(body)
	return release{Version: "9.9.9", URL: srv.URL + "/js-debug.tar.gz", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}
}

func TestAFileThatCannotBeReadIsToldWithItsTag(t *testing.T) {
	dir := t.TempDir()
	big := writeFile(t, dir, "big.json", strings.Repeat(" ", maxPackageBytes+1), 0o644)
	tests := []struct {
		name  string
		path  string
		route string
	}{
		{"a file that is not there", filepath.Join(dir, "nope.json"), "readLimited.JsDebugFileReadFailed"},
		{"a folder", dir, "readLimited.JsDebugFileReadFailed"},
		{"a file that is too big for a package.json", big, "readLimited.JsDebugFileTooLarge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readLimited(tt.path)
			if appErr := routeOf(t, err); !appErr.HasRoute(tt.route) {
				t.Errorf("route = %s, want %s", appErr.Route(), tt.route)
			}
		})
	}
}

// The process picker is shown on the screen that started the session (decision 1500).
func TestTheProcessPickerCarriesTheOriginOfTheStart(t *testing.T) {
	const view extension.Origin = 6
	r := newRig(t, rigOptions{})
	r.fakeProc(30, "node", "a.js")
	ch := make(chan result, 1)
	go func() {
		out, err := r.api.ResolveDebugConfiguration(extension.WithOrigin(context.Background(), view), DebugType, r.folder, cfg{"name": "A", "type": "node", "request": "attach", "processId": "${command:PickProcess}"})
		ch <- result{out, err}
	}()
	r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
	if got := r.api.LastPick().Options().Origin; got != view {
		t.Errorf("the picker is shown for the origin %d, want %d", got, view)
	}
	r.api.LastPick().Choose(0)
	if got := r.wait(ch); got.err != nil {
		t.Fatal(got.err)
	}
}
