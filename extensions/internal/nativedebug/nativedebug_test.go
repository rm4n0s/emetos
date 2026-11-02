package nativedebug

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The native debuggers over the fake API (SPEC §10.31 "The debuggers", C, C++ and Rust; step 25.9): the declarations, the adapters with
// fake gdb and lldb-dap programs (scripts that print a version), the providers, the process picker with a fake ps. No debugger is run
// here: the real ones are tested by the packages of the extensions under the tags emetos_gdb and emetos_lldbdap.

// cfg is a configuration.
type cfg = extension.DebugConfiguration

// rig is the debugger registered on a fake API with a folder and a PATH of the test.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	d      *Debugger
	folder string
	bin    string
}

// manifest is the manifest of an extension that declares the debuggers, as the C and C++ extension does.
func manifest() extension.Manifest {
	m := extension.Manifest{ID: "emetos.nativetest", Name: "Native test"}
	m.Contributes.Debuggers = Specs()
	m.Contributes.Settings = SettingSpecs()
	return m
}

func newRig(t *testing.T) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin")}
	for _, dir := range []string{r.folder, r.bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", r.bin)
	t.Setenv("HOME", filepath.Join(base, "home"))
	if err := extension.CheckManifest(manifest()); err != nil {
		t.Fatal(err)
	}
	r.api = fakeapi.New(t, manifest(), extension.ScopeInfo{Name: "tab", Folder: r.folder})
	r.d = New(r.api)
	r.d.SetOSRelease(func() string { return "ID=fedora\n" })
	r.d.Register()
	return r
}

// write writes a file under dir and returns its path.
func write(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// script writes an executable script and runs it once: a goroutine of an earlier test that forks while the script is still open for
// writing gives the child a copy of the file descriptor until it execs, and running the script then fails with "text file busy" (the
// run of the code under test must not meet that).
func script(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := write(t, dir, name, "#!/bin/sh\n"+body+"\n", 0o755)
	for i := 0; ; i++ {
		err := exec.Command(p).Run()
		if !errors.Is(err, syscall.ETXTBSY) {
			break
		}
		if i == 100 {
			t.Fatalf("the script %s stays busy: %v", p, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return p
}

// fakeGDB puts a gdb that prints the text as its version first in PATH.
func (r *rig) fakeGDB(banner string) string {
	r.t.Helper()
	return script(r.t, r.bin, "gdb", "echo '"+banner+"'")
}

// adapter asks the factory of the type for the adapter of a session with the configuration.
func (r *rig) adapter(typ string, c cfg) (*extension.DebugAdapter, error) {
	r.t.Helper()
	s := r.api.NewDebugSession("s1", typ, "n", c)
	s.SetFolder(r.folder)
	return r.api.DebugAdapter(context.Background(), typ, s, nil)
}

// notes lists the notifications with their severity.
func (r *rig) notes() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

// wantRoute checks the route and the tag of a failure.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestTheDebuggersAreDeclared(t *testing.T) {
	specs := Specs()
	if len(specs) != 3 {
		t.Fatalf("%d debuggers", len(specs))
	}
	tests := []struct {
		typ       string
		label     string
		languages []string
		dynamic   bool
		initial   int
		snippets  int
		// launch and attach are attributes the schemas must have
		launch, attach []string
	}{
		{TypeGDB, "GDB", Languages, true, 2, 4, []string{"program", "args", "cwd", "env", "stopAtBeginningOfMainSubprogram", "stopOnEntry", "debuggerPath", "debuggerArgs", "cargo"}, []string{"pid", "target", "coreFile", "program"}},
		{TypeLLDB, "LLDB DAP Debugger", Languages, true, 1, 3, []string{"program", "args", "env", "cwd", "initCommands", "sourceMap", "stopOnEntry", "console", "cargo"}, []string{"pid", "program", "waitFor", "coreFile", "attachCommands", "gdb-remote-port"}},
		{TypeLLDBOld, "LLDB (the old name of lldb-dap)", nil, false, 0, 0, []string{"program", "cargo"}, []string{"pid"}},
	}
	for i, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			s := specs[i]
			if s.Type != tt.typ || s.Label != tt.label || !slices.Equal(s.Languages, tt.languages) || s.DynamicConfigurations != tt.dynamic ||
				len(s.InitialConfigurations) != tt.initial || len(s.ConfigurationSnippets) != tt.snippets {
				t.Errorf("spec = %+v", s)
			}
			for request, want := range map[string][]string{"launch": tt.launch, "attach": tt.attach} {
				var schema struct {
					Properties map[string]json.RawMessage `json:"properties"`
				}
				if err := json.Unmarshal(s.ConfigurationAttributes[request], &schema); err != nil {
					t.Fatalf("%s: %v", request, err)
				}
				for _, name := range want {
					if _, ok := schema.Properties[name]; !ok {
						t.Errorf("%s has no attribute %s", request, name)
					}
				}
			}
		})
	}
}

func TestALaunchNeedsAProgramOrCargo(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(Specs()[1].ConfigurationAttributes["launch"], &schema); err != nil {
		t.Fatal(err)
	}
	if _, has := schema["required"]; has {
		t.Error("the launch of lldb-dap still requires program")
	}
	alts, _ := schema["anyOf"].([]any)
	var required []string
	for _, a := range alts {
		for _, r := range a.(map[string]any)["required"].([]any) {
			required = append(required, r.(string))
		}
	}
	if !slices.Equal(required, []string{"program", CargoAttribute}) {
		t.Errorf("one of %v is required, want program or cargo", required)
	}
	var gdb map[string]any
	if err := json.Unmarshal(Specs()[0].ConfigurationAttributes["launch"], &gdb); err != nil {
		t.Fatal(err)
	}
	if _, has := gdb["anyOf"]; has {
		t.Error("the launch of gdb has a requirement")
	}
}

func TestTheSnippetsAndInitialConfigurationsStartADebugSession(t *testing.T) {
	for _, s := range Specs() {
		for _, c := range s.InitialConfigurations {
			if c["type"] != s.Type || (c["request"] != "launch" && c["request"] != "attach") {
				t.Errorf("%s initial %v", s.Type, c)
			}
		}
		for _, sn := range s.ConfigurationSnippets {
			if sn.Body["type"] != s.Type || sn.Label == "" {
				t.Errorf("%s snippet %+v", s.Type, sn)
			}
		}
	}
}

func TestTheSettingsAreDeclared(t *testing.T) {
	var keys []string
	for _, s := range SettingSpecs() {
		keys = append(keys, s.Key)
	}
	want := []string{GDBPathSetting, GDBArgumentsSetting, GDBEnvironmentSetting, LLDBPathSetting, LLDBArgumentsSetting, LLDBEnvironmentSetting}
	if !slices.Equal(keys, want) {
		t.Errorf("settings = %v, want %v", keys, want)
	}
}

func TestEveryTypeHasProvidersAndAFactory(t *testing.T) {
	r := newRig(t)
	got := r.api.DebugFactoryTypes()
	slices.Sort(got)
	if want := []string{TypeGDB, TypeLLDB, TypeLLDBOld}; !slices.Equal(got, want) {
		t.Errorf("factories = %v, want %v", got, want)
	}
	for _, typ := range []string{TypeGDB, TypeLLDB} {
		list, err := r.api.ProvideDebugConfigurations(context.Background(), typ, r.folder, extension.DebugTriggerDynamic)
		if err != nil || len(list) != 1 || list[0]["type"] != typ {
			t.Errorf("%s dynamic = %v, %v", typ, list, err)
		}
	}
	list, err := r.api.ProvideDebugConfigurations(context.Background(), TypeLLDBOld, r.folder, extension.DebugTriggerDynamic)
	if err != nil || len(list) != 0 {
		t.Errorf("the old name has dynamic configurations: %v, %v", list, err)
	}
}

func TestParseGdbVersion(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"fedora", "GNU gdb (Fedora Linux) 18.1-1.fc44\nCopyright", "18.1"},
		{"ubuntu", "GNU gdb (Ubuntu 12.1-0ubuntu1~22.04.2) 12.1\nCopyright", "12.1"},
		{"a distribution with a number in its name", "GNU gdb (GDB) Red Hat Enterprise Linux 8.2-3.el9", "8.2"},
		{"upstream", "GNU gdb (GDB) 14.2", "14.2"},
		{"a name with a number in front of the version", "GNU gdb (Debian 10.1-1.7) 10.1.90.20210103-git", "10.1.90.20210103"},
		{"empty lines first", "\n\nGNU gdb (GDB) 15.1\n", "15.1"},
		{"the line ends with no number", "GNU gdb 13.2 (custom)", "13.2"},
		{"no version", "gdb: command not found", ""},
		{"nothing", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseGdbVersion(tt.in)
			if got != tt.want || ok != (tt.want != "") {
				t.Errorf("version = %q, %v, want %q", got, ok, tt.want)
			}
		})
	}
}

func TestTheGdbAdapterIsGdbInDapMode(t *testing.T) {
	tests := []struct {
		name     string
		banner   string
		settings map[string]any
		cfg      cfg
		args     []string
		env      map[string]string
	}{
		{"gdb 18", "GNU gdb (Fedora Linux) 18.1-1.fc44", nil, cfg{}, []string{"-i", "dap"}, nil},
		{"gdb 14, the oldest", "GNU gdb (GDB) 14.1", nil, cfg{}, []string{"-i", "dap"}, nil},
		{"the arguments of the setting come first", "GNU gdb (GDB) 15.2",
			map[string]any{GDBArgumentsSetting: []any{"-nx", 7.0}}, cfg{"debuggerArgs": []any{"-iex", "set confirm off"}},
			[]string{"-i", "dap", "-nx", "-iex", "set confirm off"}, nil},
		{"the environment of the setting", "GNU gdb (GDB) 15.2",
			map[string]any{GDBEnvironmentSetting: map[string]any{"LANG": "C", "N": 3.0}}, cfg{}, []string{"-i", "dap"}, map[string]string{"LANG": "C"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			gdb := r.fakeGDB(tt.banner)
			for k, v := range tt.settings {
				r.api.SetSetting(k, v)
			}
			a, err := r.adapter(TypeGDB, tt.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if a.Kind != extension.DebugAdapterExecutable || a.Command != gdb || !slices.Equal(a.Args, tt.args) || a.Cwd != r.folder ||
				!reflect.DeepEqual(a.Env, tt.env) || !a.ConsoleOutput {
				t.Errorf("adapter = %+v", a)
			}
			if len(r.api.Notes()) != 0 {
				t.Errorf("notifications = %v", r.notes())
			}
		})
	}
}

func TestTheGdbThatIsUsedIsTheOneNamed(t *testing.T) {
	r := newRig(t)
	r.fakeGDB("GNU gdb (GDB) 14.1")
	other := script(t, t.TempDir(), "mygdb", "echo 'GNU gdb (GDB) 16.3'")
	tests := []struct {
		name     string
		settings string
		attr     string
		want     string
	}{
		{"the setting", other, "", other},
		{"the attribute is stronger", "/does/not/exist", other, other},
		{"a name in PATH", "gdb", "", filepath.Join(r.bin, "gdb")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.api.SetSetting(GDBPathSetting, tt.settings)
			c := cfg{}
			if tt.attr != "" {
				c["debuggerPath"] = tt.attr
			}
			a, err := r.adapter(TypeGDB, c)
			if err != nil || a.Command != tt.want {
				t.Errorf("adapter = %+v, %v, want the program %s", a, err, tt.want)
			}
		})
	}
}

func TestAGdbThatCannotBeUsedFailsAndTellsTheUser(t *testing.T) {
	notExecutable := write(t, t.TempDir(), "gdb", "#!/bin/sh\n", 0o644)
	tests := []struct {
		name     string
		banner   string // "" puts no gdb in PATH
		silent   bool   // the program prints nothing
		setting  string
		route    string
		message  string
		hasCopy  bool
		noteText string
	}{
		{name: "gdb 13 is too old", banner: "GNU gdb (GDB) 13.2", route: "gdbAdapter.GdbTooOld", message: "gdb 13.2 is too old", hasCopy: true, noteText: "gdb 13.2"},
		{name: "gdb 12 of a distribution", banner: "GNU gdb (Ubuntu 12.1-0ubuntu1~22.04.2) 12.1", route: "gdbAdapter.GdbTooOld", message: "gdb 12.1 is too old", hasCopy: true, noteText: "too old"},
		{name: "no gdb", route: "gdbAdapter.GdbNotFound", message: "gdb was not found", hasCopy: true, noteText: "gdb was not found"},
		{name: "a setting that is not a file", banner: "GNU gdb (GDB) 15.1", setting: "/does/not/exist/gdb", route: "gdbAdapter.GdbPathInvalid", message: "does not exist", hasCopy: true, noteText: "does not exist"},
		{name: "a setting that is not executable", banner: "GNU gdb (GDB) 15.1", setting: notExecutable, route: "gdbAdapter.GdbPathInvalid", message: "not executable", hasCopy: true, noteText: "not executable"},
		{name: "a name that is not in PATH", banner: "GNU gdb (GDB) 15.1", setting: "gdb-nightly", route: "gdbAdapter.GdbPathInvalid", message: "not a program in PATH", hasCopy: true, noteText: "PATH"},
		{name: "a gdb that prints no version", banner: "x", silent: true, route: "gdbAdapter.GdbVersionFailed", message: "did not tell its version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			switch {
			case tt.silent:
				script(t, r.bin, "gdb", "echo 'no version here'")
			case tt.banner != "":
				r.fakeGDB(tt.banner)
			}
			if tt.setting != "" {
				r.api.SetSetting(GDBPathSetting, tt.setting)
			}
			a, err := r.adapter(TypeGDB, cfg{})
			if a != nil || err == nil {
				t.Fatalf("adapter = %+v, err = %v", a, err)
			}
			wantRoute(t, err, tt.route)
			if !strings.Contains(err.Error(), tt.message) {
				t.Errorf("error = %v, want %q", err, tt.message)
			}
			notes := r.api.Notes()
			if tt.noteText == "" {
				if len(notes) != 0 {
					t.Errorf("notifications = %v", r.notes())
				}
				return
			}
			if len(notes) != 1 || notes[0].Severity != "warning" && notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, tt.noteText) {
				t.Fatalf("notifications = %v", r.notes())
			}
			if tt.hasCopy {
				if !slices.Equal(notes[0].Actions, []string{"Copy Install Command"}) {
					t.Fatalf("actions = %v", notes[0].Actions)
				}
				r.api.RunNoteAction(0, "Copy Install Command")
				if got := r.api.ClipboardTexts(); !slices.Equal(got, []string{"sudo dnf install gdb"}) {
					t.Errorf("clipboard = %v", got)
				}
			}
		})
	}
}

func TestTheInstallCommandIsThatOfTheSystem(t *testing.T) {
	tests := []struct{ os, want string }{
		{"ID=ubuntu\nID_LIKE=debian\n", "sudo apt install gdb"},
		{"ID=arch\n", "sudo pacman -S gdb"},
		{"ID=opensuse-tumbleweed\n", "sudo zypper install gdb"},
		{"ID=nixos\n", "sudo dnf install gdb"}, // a system that is none of ours gets the first command
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			r := newRig(t)
			r.d.SetOSRelease(func() string { return tt.os })
			if _, err := r.adapter(TypeGDB, cfg{}); err == nil {
				t.Fatal("no error")
			}
			r.api.RunNoteAction(0, "Copy Install Command")
			if got := r.api.ClipboardTexts(); !slices.Equal(got, []string{tt.want}) {
				t.Errorf("clipboard = %v", got)
			}
		})
	}
}

func TestACopyThatFailsSaysSo(t *testing.T) {
	r := newRig(t)
	if _, err := r.adapter(TypeGDB, cfg{}); err == nil {
		t.Fatal("no error")
	}
	r.api.FailClipboard(errors.New("ClipboardFailed", "no clipboard"))
	r.api.RunNoteAction(0, "Copy Install Command")
	notes := r.notes()
	if len(notes) != 2 || !strings.HasPrefix(notes[1], "error: The command cannot be copied") || !strings.Contains(notes[1], "sudo dnf install gdb") {
		t.Errorf("notifications = %v", notes)
	}
}

func TestTheLldbDapAdapter(t *testing.T) {
	tests := []struct {
		name     string
		programs []string // the programs in PATH
		settings map[string]any
		cfg      cfg
		want     extension.DebugAdapter
	}{
		{"lldb-dap in PATH", []string{"lldb-dap"}, nil, cfg{}, extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: "$bin/lldb-dap", ConsoleOutput: true}},
		{"lldb-vscode, the old name, when it is alone", []string{"lldb-vscode"}, nil, cfg{}, extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: "$bin/lldb-vscode", ConsoleOutput: true}},
		{"lldb-dap is preferred", []string{"lldb-vscode", "lldb-dap"}, nil, cfg{}, extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: "$bin/lldb-dap", ConsoleOutput: true}},
		{"the setting names the program, the arguments and the environment", []string{"lldb-dap", "other"},
			map[string]any{LLDBPathSetting: "other", LLDBArgumentsSetting: []any{"--repl-mode=command"}, LLDBEnvironmentSetting: map[string]any{"LLDB_DEBUGSERVER_PATH": "/x"}},
			cfg{"debugAdapterArgs": []any{"--pre-init-command", "settings set target.x 1"}},
			extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: "$bin/other", Args: []string{"--repl-mode=command", "--pre-init-command", "settings set target.x 1"},
				Env: map[string]string{"LLDB_DEBUGSERVER_PATH": "/x"}, ConsoleOutput: true}},
		{"debugAdapterExecutable is stronger than the setting", []string{"lldb-dap", "mine"}, map[string]any{LLDBPathSetting: "lldb-dap"}, cfg{"debugAdapterExecutable": "$bin/mine"},
			extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: "$bin/mine", ConsoleOutput: true}},
		{"a port is a server", nil, nil, cfg{"debugAdapterPort": 4711.0},
			extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: "127.0.0.1", Port: 4711}},
		{"a port as text and a host", nil, nil, cfg{"debugAdapterPort": "4712", "debugAdapterHostname": " 10.0.0.2 "},
			extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: "10.0.0.2", Port: 4712}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			for _, p := range tt.programs {
				script(t, r.bin, p, "exit 0")
			}
			for k, v := range tt.settings {
				r.api.SetSetting(k, v)
			}
			c := tt.cfg
			for k, v := range c {
				if s, ok := v.(string); ok {
					c[k] = strings.ReplaceAll(s, "$bin", r.bin)
				}
			}
			a, err := r.adapter(TypeLLDB, c)
			if err != nil {
				t.Fatal(err)
			}
			want := tt.want
			want.Command = strings.ReplaceAll(want.Command, "$bin", r.bin)
			if want.Kind == extension.DebugAdapterExecutable {
				want.Cwd = r.folder
			}
			if len(a.Args) == 0 {
				a.Args = nil
			}
			if !reflect.DeepEqual(*a, want) {
				t.Errorf("adapter = %+v, want %+v", *a, want)
			}
		})
	}
	t.Run("the old type is served by the same factory", func(t *testing.T) {
		r := newRig(t)
		script(t, r.bin, "lldb-dap", "exit 0")
		if a, err := r.adapter(TypeLLDBOld, cfg{}); err != nil || a.Command != filepath.Join(r.bin, "lldb-dap") {
			t.Errorf("adapter = %+v, %v", a, err)
		}
	})
}

func TestAnLldbDapThatCannotBeUsedFailsAndTellsTheUser(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		attr    string
		route   string
		message string
	}{
		{"no lldb-dap", "", "", "lldbAdapter.LldbDapNotFound", "lldb-dap (or lldb-vscode) was not found"},
		{"a setting that does not exist", "/does/not/exist", "", "lldbAdapter.LldbDapPathInvalid", "does not exist"},
		{"an attribute that is a folder", "", "/", "lldbAdapter.LldbDapPathInvalid", "is a folder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			if tt.setting != "" {
				r.api.SetSetting(LLDBPathSetting, tt.setting)
			}
			c := cfg{}
			if tt.attr != "" {
				c["debugAdapterExecutable"] = tt.attr
			}
			a, err := r.adapter(TypeLLDB, c)
			if a != nil || err == nil {
				t.Fatalf("adapter = %+v, err = %v", a, err)
			}
			wantRoute(t, err, tt.route)
			if !strings.Contains(err.Error(), tt.message) {
				t.Errorf("error = %v, want %q", err, tt.message)
			}
			notes := r.api.Notes()
			if len(notes) != 1 || !strings.Contains(notes[0].Message, "lldb-dap") || !slices.Equal(notes[0].Actions, []string{"Copy Install Command"}) {
				t.Fatalf("notifications = %v", r.notes())
			}
			r.api.RunNoteAction(0, "Copy Install Command")
			if got := r.api.ClipboardTexts(); !slices.Equal(got, []string{"sudo dnf install lldb"}) {
				t.Errorf("clipboard = %v", got)
			}
		})
	}
}

// ---- the providers

func TestTheDynamicConfigurationLaunchesTheProgramOfTheFileInFront(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"a folder of C sources", map[string]string{"main.c": ""}, 1},
		{"a cmake project", map[string]string{"CMakeLists.txt": "", "Cargo.toml": ""}, 1},
		{"a Rust project", map[string]string{"Cargo.toml": ""}, 0},
		{"a Rust project that builds C", map[string]string{"Cargo.toml": "", "Makefile": ""}, 1},
		{"an empty folder", nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			for name, text := range tt.files {
				write(t, r.folder, name, text, 0o644)
			}
			for _, typ := range []string{TypeGDB, TypeLLDB} {
				list, err := r.api.ProvideDebugConfigurations(context.Background(), typ, "", extension.DebugTriggerDynamic)
				if err != nil || len(list) != tt.want {
					t.Fatalf("%s: %v, %v", typ, list, err)
				}
				if tt.want == 1 {
					want := cfg{"name": DynamicName, "type": typ, "request": "launch", "program": "${fileDirname}/${fileBasenameNoExtension}", "args": []any{}, "cwd": "${workspaceFolder}"}
					if !reflect.DeepEqual(list[0], want) {
						t.Errorf("%s: %v, want %v", typ, list[0], want)
					}
				}
			}
		})
	}
}

// resolveAsync runs the first resolution on a goroutine, as the host does (it may wait for the user).
func (r *rig) resolveAsync(typ string, c cfg) <-chan result {
	ch := make(chan result, 1)
	go func() {
		out, err := r.api.ResolveDebugConfiguration(context.Background(), typ, r.folder, c)
		ch <- result{out, err}
	}()
	return ch
}

// result is the answer of a provider that ran on a goroutine.
type result struct {
	cfg cfg
	err error
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

func (r *rig) substitute(typ string, c cfg) (cfg, error) {
	return r.api.ResolveDebugConfigurationSubstituted(context.Background(), typ, r.folder, c)
}

func TestTheSecondResolutionOfALaunch(t *testing.T) {
	t.Setenv("EMETOS_NATIVE_TEST_VAR", "from-emetos")
	tests := []struct {
		name string
		typ  string
		cfg  func(r *rig) cfg
		// want is the configuration the debugger is sent ($folder is the folder of the tab)
		want func(r *rig) cfg
	}{
		{"the working folder and the program", TypeGDB,
			func(r *rig) cfg { return cfg{"request": "launch", "program": filepath.Join(r.folder, "app")} },
			func(r *rig) cfg {
				return cfg{"request": "launch", "program": filepath.Join(r.folder, "app"), "cwd": r.folder}
			}},
		{"a relative program and cwd", TypeLLDB,
			func(r *rig) cfg { return cfg{"request": "launch", "program": "bin/app", "cwd": "sub"} },
			func(r *rig) cfg {
				return cfg{"request": "launch", "program": filepath.Join(r.folder, "sub", "bin", "app"), "cwd": filepath.Join(r.folder, "sub")}
			}},
		{"a program found in PATH is left alone", TypeGDB,
			func(r *rig) cfg { return cfg{"request": "launch", "program": "ls"} },
			func(r *rig) cfg { return cfg{"request": "launch", "program": "ls", "cwd": r.folder} }},
		{"the arguments of a text are split", TypeGDB,
			func(r *rig) cfg {
				return cfg{"request": "launch", "program": "ls", "args": `one "two words" 'three four' five\ six ""`}
			},
			func(r *rig) cfg {
				return cfg{"request": "launch", "program": "ls", "cwd": r.folder, "args": []any{"one", "two words", "three four", "five six", ""}}
			}},
		{"cargo gives the program later", TypeGDB,
			func(r *rig) cfg { return cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build"}}} },
			func(r *rig) cfg {
				return cfg{"request": "launch", "cargo": map[string]any{"args": []any{"build"}}, "cwd": r.folder}
			}},
		{"lldb-dap launches by its own commands", TypeLLDB,
			func(r *rig) cfg { return cfg{"request": "launch", "launchCommands": []any{"process launch"}} },
			func(r *rig) cfg {
				return cfg{"request": "launch", "launchCommands": []any{"process launch"}, "cwd": r.folder}
			}},
		{"lldb-dap takes the environment as it is", TypeLLDB,
			func(r *rig) cfg { return cfg{"request": "launch", "program": "ls", "env": []any{"A=1"}} },
			func(r *rig) cfg {
				return cfg{"request": "launch", "program": "ls", "env": []any{"A=1"}, "cwd": r.folder}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			if strings.Contains(tt.name, "relative") {
				write(t, r.folder, "sub/bin/app", "", 0o755)
			}
			if tt.name == "the working folder and the program" {
				write(t, r.folder, "app", "", 0o755)
			}
			got, err := r.substitute(tt.typ, tt.cfg(r))
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(r); !reflect.DeepEqual(got, want) {
				t.Errorf("configuration = %v, want %v", got, want)
			}
		})
	}
}

func TestTheEnvironmentOfGdbIsWholeAndThatOfTheConfigurationIsAddedToEmetos(t *testing.T) {
	t.Setenv("EMETOS_NATIVE_TEST_VAR", "from-emetos")
	tests := []struct {
		name string
		env  any
		want map[string]string
	}{
		{"an object", map[string]any{"A": "1", "EMETOS_NATIVE_TEST_VAR": "mine", "N": 2.0, "T": true, "Z": nil},
			map[string]string{"A": "1", "EMETOS_NATIVE_TEST_VAR": "mine", "N": "2", "T": "true", "Z": ""}},
		{"a list of NAME=value", []any{"A=1", "B", "C=x=y"}, map[string]string{"A": "1", "B": "", "C": "x=y", "EMETOS_NATIVE_TEST_VAR": "from-emetos"}},
		{"an empty object leaves the environment to gdb", map[string]any{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			got, err := r.substitute(TypeGDB, cfg{"request": "launch", "program": "ls", "env": tt.env})
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == nil {
				if !reflect.DeepEqual(got["env"], tt.env) {
					t.Errorf("env = %v", got["env"])
				}
				return
			}
			env, _ := got["env"].(map[string]any)
			for k, v := range tt.want {
				if env[k] != v {
					t.Errorf("env[%s] = %v, want %q", k, env[k], v)
				}
			}
			if env["PATH"] != os.Getenv("PATH") {
				t.Errorf("PATH = %v, want that of Emetos", env["PATH"])
			}
		})
	}
}

func TestTheSecondResolutionFailures(t *testing.T) {
	tests := []struct {
		name  string
		typ   string
		cfg   cfg
		route string
		text  string
	}{
		{"a launch without a program", TypeGDB, cfg{"name": "L", "request": "launch"}, "launchSubstituted.NativeDebugProgramInvalid", "needs a \"program\""},
		{"lldb-dap without a program or commands", TypeLLDB, cfg{"request": "launch", "cargo": nil}, "launchSubstituted.NativeDebugProgramInvalid", "needs a \"program\""},
		{"a program that is not built", TypeGDB, cfg{"request": "launch", "program": "build/app"}, "launchSubstituted.NativeDebugProgramMissing", "Build it with debug information"},
		{"a quote that is not closed", TypeGDB, cfg{"request": "launch", "program": "ls", "args": `a "b`}, "splitArgs.NativeDebugArgsInvalid", "not closed"},
		{"an environment variable that is a list", TypeGDB, cfg{"request": "launch", "program": "ls", "env": map[string]any{"A": []any{}}}, "gdbEnvironment.NativeDebugEnvInvalid", "must be text"},
		{"an environment that is a number", TypeGDB, cfg{"request": "launch", "program": "ls", "env": 3.0}, "gdbEnvironment.NativeDebugEnvInvalid", "object of variables"},
		{"an environment list of numbers", TypeGDB, cfg{"request": "launch", "program": "ls", "env": []any{1.0}}, "gdbEnvironment.NativeDebugEnvInvalid", "NAME=value"},
		{"a pid that is text", TypeGDB, cfg{"request": "attach", "pid": "abc"}, "attachSubstituted.NativeDebugAttachInvalid", "must be a process id"},
		{"a pid of zero", TypeLLDB, cfg{"request": "attach", "pid": "0"}, "attachSubstituted.NativeDebugAttachInvalid", "must be a process id"},
		{"a gdb attach with nothing to attach to", TypeGDB, cfg{"request": "attach", "program": "x"}, "attachSubstituted.NativeDebugAttachInvalid", "one of \"pid\", \"target\" or \"coreFile\""},
		{"an lldb-dap attach with nothing to attach to", TypeLLDB, cfg{"request": "attach"}, "attachSubstituted.NativeDebugAttachInvalid", "\"gdb-remote-port\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			if tt.cfg["cargo"] == nil {
				delete(tt.cfg, "cargo")
			}
			got, err := r.substitute(tt.typ, tt.cfg)
			if got != nil || err == nil {
				t.Fatalf("configuration = %v, err = %v", got, err)
			}
			wantRoute(t, err, tt.route)
			if !strings.Contains(err.Error(), tt.text) {
				t.Errorf("error = %v, want %q", err, tt.text)
			}
		})
	}
}

func TestTheSecondResolutionOfAnAttach(t *testing.T) {
	tests := []struct {
		name string
		typ  string
		cfg  cfg
		want cfg
	}{
		{"a pid as text", TypeGDB, cfg{"request": "attach", "pid": " 4242 "}, cfg{"request": "attach", "pid": 4242.0}},
		{"a pid as a number", TypeLLDB, cfg{"request": "attach", "pid": 7.0}, cfg{"request": "attach", "pid": 7.0}},
		{"a remote target", TypeGDB, cfg{"request": "attach", "target": "localhost:2345", "program": "bin/app"}, cfg{"request": "attach", "target": "localhost:2345", "program": "$folder/bin/app"}},
		{"a core file", TypeGDB, cfg{"request": "attach", "coreFile": "core"}, cfg{"request": "attach", "coreFile": "core"}},
		{"lldb-dap by the name of the program", TypeLLDB, cfg{"request": "attach", "program": "/tmp/a.out", "waitFor": true}, cfg{"request": "attach", "program": "/tmp/a.out", "waitFor": true}},
		{"lldb-dap to a debug server", TypeLLDB, cfg{"request": "attach", "gdb-remote-port": 2345.0}, cfg{"request": "attach", "gdb-remote-port": 2345.0}},
		{"a request that is none", TypeLLDB, cfg{"program": "x"}, cfg{"program": "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			want := cfg{}
			for k, v := range tt.want {
				if s, ok := v.(string); ok {
					v = strings.ReplaceAll(s, "$folder", r.folder)
				}
				want[k] = v
			}
			got, err := r.substitute(tt.typ, tt.cfg)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("configuration = %v, %v, want %v", got, err, want)
			}
		})
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a b  c", []string{"a", "b", "c"}},
		{`a\ b "c d" 'e f'`, []string{"a b", "c d", "e f"}},
		{`"a\"b" 'c\d'`, []string{`a"b`, `c\d`}},
		{`x=""`, []string{"x="}},
		{"tab\tnew\nline", []string{"tab", "new", "line"}},
	}
	for _, tt := range tests {
		got, err := splitArgs(tt.in)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("splitArgs(%q) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
}

// ---- the process picker

func psLine(pid int, name, command string) string {
	return fmt.Sprintf("%5d %-*s %s", pid, secondColumn-1, name, command)
}

func psListing(rows ...string) string {
	return "    " + strings.Repeat("a", secondColumn) + "\n" + strings.Join(rows, "\n") + "\n"
}

// fakePS puts a ps that prints the listing (or fails with the status) first in PATH.
func fakePS(t *testing.T, r *rig, listing string, status int) {
	t.Helper()
	write(t, r.bin, "ps.out", listing, 0o644)
	script(t, r.bin, "ps", "while IFS= read -r line; do printf '%s\\n' \"$line\"; done < \"${0%/*}/ps.out\"\nexit "+strconv.Itoa(status))
}

func TestParsePS(t *testing.T) {
	long := strings.Repeat("x", 60)
	tests := []struct {
		name, in string
		want     []process
	}{
		{"the header is skipped", psListing(psLine(1, "init", "/sbin/init splash"), psLine(22, "bash", "-bash")),
			[]process{{1, "init", "/sbin/init splash"}, {22, "bash", "-bash"}}},
		{"a command line with no arguments", psListing(psLine(7, "kthreadd", "")), []process{{7, "kthreadd", ""}}},
		{"a name of 15 characters and a long command line", psListing(psLine(9, "systemd-journal", "/usr/lib/systemd/systemd-journald "+long)),
			[]process{{9, "systemd-journal", "/usr/lib/systemd/systemd-journald " + long}}},
		{"lines that are not rows", psListing("garbage", "", psLine(3, "ok", "run")), []process{{3, "ok", "run"}}},
		{"nothing", "", nil},
		{"only the header", psListing(), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePS(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("processes = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTheProcessesAreSortedByName(t *testing.T) {
	list := []process{{4, "nginx", ""}, {2, "gdb", ""}, {3, "Bash", ""}, {1, "bash", ""}, {5, "apache", ""}}
	sortProcesses(list)
	var got []int
	for _, p := range list {
		got = append(got, p.pid)
	}
	if want := []int{5, 1, 3, 2, 4}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTheProcessPickerAnswersThePid(t *testing.T) {
	listing := psListing(
		psLine(30, "nginx", "nginx: worker"),
		psLine(10, "app", "/srv/app --serve"),
		psLine(20, "bash", "-bash"),
	)
	tests := []struct {
		name   string
		typ    string
		pid    any
		shown  bool
		choose int
		want   any
	}{
		{"the variable pickProcess", TypeGDB, "${command:pickProcess}", true, 0, float64(10)},
		{"the second row", TypeGDB, " ${command:pickProcess} ", true, 1, float64(20)},
		{"lldb-dap's own name for it", TypeLLDB, "${command:pickMyProcess}", true, 2, float64(30)},
		{"a number is left", TypeGDB, float64(77), false, 0, float64(77)},
		{"a number as text is left for the substitution", TypeLLDB, "77", false, 0, "77"},
		{"another variable is left for the substitution", TypeGDB, "${input:pid}", false, 0, "${input:pid}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			fakePS(t, r, listing, 0)
			ch := r.resolveAsync(tt.typ, cfg{"name": "A", "request": "attach", "pid": tt.pid})
			if tt.shown {
				r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
				items := r.api.LastPick().Options().Items
				if len(items) != 3 || items[0].Label != "app" || items[0].Description != "10  /srv/app --serve" {
					t.Errorf("items = %+v", items)
				}
				if !r.api.LastPick().Options().MatchOnDescription {
					t.Error("the command line is not searched")
				}
				r.api.LastPick().Choose(tt.choose)
			}
			got := r.wait(ch)
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.cfg["pid"] != tt.want || got.cfg["type"] != tt.typ {
				t.Errorf("configuration = %#v, want pid %#v", got.cfg, tt.want)
			}
			if !tt.shown && len(r.api.Picks()) != 0 {
				t.Error("a picker was shown")
			}
		})
	}
}

func TestTheProcessPickerFailsWithoutAChoiceOrAList(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, r *rig)
		closePick bool
		route     string
	}{
		{"the list is closed", func(t *testing.T, r *rig) { fakePS(t, r, psListing(psLine(10, "a", "b")), 0) }, true, "pickProcess.NativeDebugNoProcessSelected"},
		{"there is no process", func(t *testing.T, r *rig) { fakePS(t, r, psListing(), 0) }, false, "pickProcess.NativeDebugNoProcessSelected"},
		{"ps is not there", func(t *testing.T, r *rig) {}, false, "listProcesses.NativeDebugProcessListFailed"},
		{"ps fails", func(t *testing.T, r *rig) { fakePS(t, r, "", 3) }, false, "listProcesses.NativeDebugProcessListFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			tt.setup(t, r)
			ch := r.resolveAsync(TypeGDB, cfg{"request": "attach", "pid": "${command:pickProcess}"})
			if tt.closePick {
				r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
				r.api.LastPick().Close()
			}
			got := r.wait(ch)
			wantRoute(t, got.err, tt.route)
		})
	}
}

// failingPick is a quick pick that cannot be shown.
type failingPick struct{ extension.QuickPick }

func (failingPick) Show(extension.QuickPickOptions, func(int)) error {
	return errors.New("TestPickRefused", "no quick pick here")
}

// noPickAPI is an API whose quick pick fails.
type noPickAPI struct{ extension.API }

func (a noPickAPI) QuickPick() extension.QuickPick { return failingPick{a.API.QuickPick()} }

func TestAPickerThatCannotBeShownFailsTheResolution(t *testing.T) {
	r := newRig(t)
	fakePS(t, r, psListing(psLine(10, "a", "b")), 0)
	d := New(noPickAPI{r.api})
	_, err := d.resolve(context.Background(), r.folder, TypeGDB, cfg{"request": "attach", "pid": "${command:pickProcess}"})
	wantRoute(t, err, "pickProcess.NativeDebugPickFailed")
}

func TestTheFirstResolutionSetsTheType(t *testing.T) {
	r := newRig(t)
	got := r.wait(r.resolveAsync(TypeLLDBOld, cfg{"name": "x", "request": "launch", "program": "p"}))
	if got.err != nil || got.cfg["type"] != TypeLLDBOld {
		t.Errorf("resolution = %+v", got)
	}
	got = r.wait(r.resolveAsync(TypeGDB, cfg{"type": "gdb", "request": "launch", "program": "p"}))
	if got.err != nil || got.cfg["type"] != TypeGDB {
		t.Errorf("resolution = %+v", got)
	}
}

func TestAHostWithNoDebugServiceHasNoDebugger(t *testing.T) {
	r := newRig(t)
	r.api.SetDebugUnavailable(true)
	d := New(r.api)
	d.Register() // no failure, no panic
}
