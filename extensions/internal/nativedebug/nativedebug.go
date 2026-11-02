// Package nativedebug is the debugger of native programs shared by the C and C++ extension and the Rust extension (SPEC §10.31 "The
// debuggers", C, C++ and Rust; step 25.9): the debug types gdb (gdb 14 or newer, started as `gdb -i dap`) and lldb-dap (LLVM's DAP
// server; lldb-vscode, its old name, is accepted), their attributes, snippets and settings, the configuration providers and the
// descriptors of the adapters. The extension cpp declares the debuggers and registers them (a debug type has one owner); the extension
// rust adds its own providers to the same types (targets of cargo, the build before the launch).
//
// Emetos never installs a debugger (D40): a missing one is told to the user, with a button that copies the usual install command.
//
// The attributes of lldb-dap are those of its package.json (lldb-dap-attributes.json, written by scripts/update-assets.sh); those of gdb
// are written from the chapter "Debugger Adapter Protocol" of the GDB manual (gdb-attributes.json).
package nativedebug

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The debug types.
const (
	// TypeGDB is the debug type of gdb, TypeLLDB that of lldb-dap and TypeLLDBOld the name lldb-dap had (lldb-vscode): a launch.json
	// that says lldb-vscode is served by the same debugger.
	TypeGDB     = "gdb"
	TypeLLDB    = "lldb-dap"
	TypeLLDBOld = "lldb-vscode"
)

// The settings of the debuggers (the names of lldb-dap's VS Code extension; gdb's follow them).
const (
	// GDBPathSetting is the gdb program, GDBArgumentsSetting the arguments it gets after `-i dap` and GDBEnvironmentSetting the
	// environment added to its own.
	GDBPathSetting        = "gdb.path"
	GDBArgumentsSetting   = "gdb.arguments"
	GDBEnvironmentSetting = "gdb.environment"
	// LLDBPathSetting is the lldb-dap program, LLDBArgumentsSetting its arguments and LLDBEnvironmentSetting the environment added
	// to its own.
	LLDBPathSetting        = "lldb-dap.executable-path"
	LLDBArgumentsSetting   = "lldb-dap.arguments"
	LLDBEnvironmentSetting = "lldb-dap.environment"
)

// MinGDBVersion is the oldest gdb that speaks the Debugger Adapter Protocol well enough (it came in gdb 14).
const MinGDBVersion = "14"

// CargoAttribute is the attribute of a launch configuration that asks cargo for the program (rust-analyzer's `cargo`): the Rust
// extension builds and fills in `program`. The debuggers accept it so that launch.json is not checked against a missing `program`.
const CargoAttribute = "cargo"

// Languages are the languages the debuggers serve: F5 with no launch.json asks them for a configuration.
var Languages = []string{"c", "cpp", "cuda-cpp", "rust"}

// BreakpointLanguages are the languages of C and C++ where breakpoints may be set (the Rust extension adds rust).
var BreakpointLanguages = []string{"c", "cpp", "cuda-cpp"}

// Category is the group of the settings in the settings editor.
const Category = "Native debugging"

//go:embed gdb-attributes.json
var gdbAttributes []byte

//go:embed lldb-dap-attributes.json
var lldbAttributes []byte

// cargoSchema is the attribute `cargo`.
const cargoSchema = `{
  "type": "object",
  "description": "Ask cargo for the program: its arguments (for example [\"build\", \"--bin\", \"app\"] or [\"test\", \"--no-run\", \"--lib\"]) are run with --message-format=json in the folder cwd (the workspace by default), and the executable it names becomes program. filter picks one executable by name and kind when cargo builds several. Handled by the Rust extension.",
  "properties": {
    "args": {"type": "array", "items": {"type": "string"}, "description": "The arguments of cargo."},
    "cwd": {"type": "string", "description": "The folder cargo runs in; the workspace by default."},
    "env": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Environment variables for cargo (CARGO_HOME, CARGO_TARGET_DIR...), added to the environment of Emetos."},
    "filter": {"type": "object", "description": "Picks the executable: its name and the kind of its target (bin, example, test, lib...).", "properties": {"name": {"type": "string"}, "kind": {"type": "string"}}}
  },
  "required": ["args"]
}`

// attributeSchemas reads the embedded attributes: the JSON schema of each request, with the attribute `cargo` added to the launch. A
// program is required by a launch unless cargo gives it.
func attributeSchemas(raw []byte) map[string]json.RawMessage {
	var out map[string]map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	launch := out["launch"]
	if launch != nil {
		props, _ := launch["properties"].(map[string]any)
		if props == nil {
			props = map[string]any{}
			launch["properties"] = props
		}
		var cargo any
		if err := json.Unmarshal([]byte(cargoSchema), &cargo); err == nil {
			props[CargoAttribute] = cargo
		}
		if req, ok := launch["required"].([]any); ok {
			delete(launch, "required")
			var alts []any
			for _, r := range req {
				alts = append(alts, map[string]any{"required": []any{r}})
			}
			alts = append(alts, map[string]any{"required": []any{CargoAttribute}})
			launch["anyOf"] = alts
		}
	}
	res := map[string]json.RawMessage{}
	for req, schema := range out {
		data, err := json.Marshal(schema)
		if err != nil {
			return nil
		}
		res[req] = data
	}
	return res
}

// Specs are the declarations of the three debug types. gdb and lldb-dap serve the languages and have dynamic configurations; the old
// name of lldb-dap is for the launch.json files that use it.
func Specs() []extension.DebuggerSpec {
	gdb := extension.DebuggerSpec{
		Type: TypeGDB, Label: "GDB", Languages: Languages, ConfigurationAttributes: attributeSchemas(gdbAttributes),
		InitialConfigurations: []extension.DebugConfiguration{
			{"name": "(gdb) Launch", "type": TypeGDB, "request": "launch", "program": "${workspaceFolder}/a.out", "args": []any{}, "cwd": "${workspaceFolder}"},
			{"name": "(gdb) Attach", "type": TypeGDB, "request": "attach", "program": "${workspaceFolder}/a.out", "pid": pickProcessVariable},
		},
		ConfigurationSnippets: []extension.DebugSnippet{
			{Label: "GDB: Launch", Description: "Debug a program with gdb", Body: extension.DebugConfiguration{
				"name": "${2:(gdb) Launch}", "type": TypeGDB, "request": "launch", "program": "${1:a.out}", "args": []any{}}},
			{Label: "GDB: Attach to a process", Description: "Attach gdb to a running process", Body: extension.DebugConfiguration{
				"name": "${2:(gdb) Attach}", "type": TypeGDB, "request": "attach", "program": "${1:a.out}", "pid": pickProcessVariable}},
			{Label: "GDB: Connect to a gdbserver", Description: "Attach gdb to a remote target", Body: extension.DebugConfiguration{
				"name": "${3:(gdb) Remote}", "type": TypeGDB, "request": "attach", "program": "${1:a.out}", "target": "${2:localhost:2345}"}},
			{Label: "GDB: Load a core file", Description: "Debug a core dump", Body: extension.DebugConfiguration{
				"name": "${3:(gdb) Core}", "type": TypeGDB, "request": "attach", "program": "${1:a.out}", "coreFile": "${2:core}"}},
		},
		DynamicConfigurations: true,
	}
	lldb := extension.DebuggerSpec{
		Type: TypeLLDB, Label: "LLDB DAP Debugger", Languages: Languages, ConfigurationAttributes: attributeSchemas(lldbAttributes),
		InitialConfigurations: []extension.DebugConfiguration{
			{"name": "Debug", "type": TypeLLDB, "request": "launch", "program": "${workspaceFolder}/<your program>", "args": []any{}, "env": []any{}, "cwd": "${workspaceFolder}"},
		},
		ConfigurationSnippets: []extension.DebugSnippet{
			{Label: "LLDB: Launch", Description: "Debug a program with lldb-dap", Body: extension.DebugConfiguration{
				"name": "${2:Launch}", "type": TypeLLDB, "request": "launch", "program": "${1:<your program>}", "args": []any{}, "env": []any{}}},
			{Label: "LLDB: Attach", Description: "Attach lldb-dap to a program that starts", Body: extension.DebugConfiguration{
				"name": "${2:Attach}", "type": TypeLLDB, "request": "attach", "program": "${1:<your program>}", "waitFor": true}},
			{Label: "LLDB: Load Coredump", Description: "Debug a core dump", Body: extension.DebugConfiguration{
				"name": "${2:Core}", "type": TypeLLDB, "request": "attach", "program": "${1:<your program>}", "coreFile": "${1:<your program>}.core"}},
		},
		DynamicConfigurations: true,
	}
	old := extension.DebuggerSpec{Type: TypeLLDBOld, Label: "LLDB (the old name of lldb-dap)", ConfigurationAttributes: attributeSchemas(lldbAttributes)}
	return []extension.DebuggerSpec{gdb, lldb, old}
}

// SettingSpecs are the settings the debuggers read.
func SettingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{
		{Key: GDBPathSetting, Type: "string", Category: Category,
			Description: "The gdb program (version 14 or newer) that debugs C, C++ and Rust programs with the type `gdb`. A name is looked for in `PATH`; empty uses `gdb` from `PATH`."},
		{Key: GDBArgumentsSetting, Type: "array", Default: []any{}, Category: Category,
			Description: "More arguments for gdb, after `-i dap` (for example `-nx` to skip the gdbinit files)."},
		{Key: GDBEnvironmentSetting, Type: "object", Default: map[string]any{}, Category: Category,
			Description: "Environment variables added to the environment of the gdb process (not of the program it debugs: that is `env` of the configuration)."},
		{Key: LLDBPathSetting, Type: "string", Category: Category,
			Description: "The lldb-dap program, e.g. /usr/local/bin/lldb-dap. A name is looked for in `PATH`; empty uses `lldb-dap` (then `lldb-vscode`) from `PATH`."},
		{Key: LLDBArgumentsSetting, Type: "array", Default: []any{}, Category: Category,
			Description: "The list of additional arguments used to launch the debug adapter executable."},
		{Key: LLDBEnvironmentSetting, Type: "object", Default: map[string]any{}, Category: Category,
			Description: "The environment of the lldb-dap process."},
	}
}

// Debugger is the debugger of one activation of an extension.
type Debugger struct {
	api extension.API
	// osRelease reads the text that names the system, for the install command; the tests give their own.
	osRelease func() string
}

// New makes the debugger over the API of the extension.
func New(api extension.API) *Debugger {
	return &Debugger{api: api, osRelease: langserver.ReadOSRelease}
}

// SetOSRelease replaces the reader of the text that names the system (the tests).
func (d *Debugger) SetOSRelease(read func() string) { d.osRelease = read }

// Register registers, for the three types, the configuration providers and the descriptor factory, and for gdb and lldb-dap the
// provider of the dynamic configurations. The registrations end with the activation. A host with no debug service has none and the
// extension then has no debugger: that is not a failure.
func (d *Debugger) Register() {
	dbg := d.api.Debug()
	for _, typ := range []string{TypeGDB, TypeLLDB, TypeLLDBOld} {
		typ := typ
		provider := extension.DebugConfigurationProvider{
			Resolve: func(ctx context.Context, folder string, c extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				return d.resolve(ctx, folder, typ, c)
			},
			ResolveSubstituted: func(ctx context.Context, folder string, c extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				return d.resolveSubstituted(ctx, folder, typ, c)
			},
		}
		if _, err := dbg.RegisterConfigurationProvider(typ, provider, extension.DebugTriggerInitial); err != nil {
			d.api.Log().Debug("the native debugger has no configuration provider", "type", typ, "err", err)
			return
		}
		if typ != TypeLLDBOld {
			dynamic := extension.DebugConfigurationProvider{Provide: func(ctx context.Context, folder string) ([]extension.DebugConfiguration, error) {
				return d.provideDynamic(ctx, folder, typ)
			}}
			if _, err := dbg.RegisterConfigurationProvider(typ, dynamic, extension.DebugTriggerDynamic); err != nil {
				d.api.Log().Debug("the native debugger has no dynamic configurations", "type", typ, "err", err)
			}
		}
		if _, err := dbg.RegisterAdapterDescriptorFactory(typ, d.adapter); err != nil {
			d.api.Log().Debug("the native debugger has no adapter", "type", typ, "err", err)
		}
	}
}

// setting reads a setting of a type; the zero value when it is not there.
func setting[T any](d *Debugger, key string) T {
	v, _ := extension.SettingValue[T](d.api.Settings(), key)
	return v
}

// stringList reads a list of strings from a setting or an attribute; members that are not text are left out.
func stringList(v any) []string {
	var out []string
	switch list := v.(type) {
	case []any:
		for _, e := range list {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, list...)
	}
	return out
}

// stringMap reads an object of strings from a setting or an attribute; members that are not text are left out.
func stringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok && k != "" {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// folderOf is the folder of a session or of the tab.
func (d *Debugger) folderOf(folder string) string {
	if strings.TrimSpace(folder) != "" {
		return folder
	}
	return d.api.Scope().Folder
}
