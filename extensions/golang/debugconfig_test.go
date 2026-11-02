package golang

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The second resolution of a configuration (variables substituted), the adapter descriptor and the parsers of the debugger over the
// fake API (step 25.7).

// debugTree makes the files a launch refers to in the folder of the rig: a package pkg with a test, main.go, two environment files.
func debugTree(t *testing.T, r *rig) {
	t.Helper()
	files := map[string]string{
		"pkg/a.go":      "package pkg\n",
		"pkg/a_test.go": "package pkg\n",
		"notes.txt":     "text\n",
		"one.env":       "# a comment\nB=file\nexport C=${A}-x\nD=\"quoted\\nline\"\nE='single'\nF=${NOPE}end\nG=\\${A}\nH = spaced \nnot a line\n",
		"two.env":       "B=second\nI=2\n",
	}
	for name, text := range files {
		path := filepath.Join(r.folder, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheSecondResolutionCompletesAConfiguration(t *testing.T) {
	tools := map[string]any{ToolsEnvVarsSetting: map[string]any{"A": "tools", "B": "tools"}}
	tests := []struct {
		name string
		in   func(folder string) cfg
		// want is compared member by member; a member that is "@folder/x" is the path in the folder
		want cfg
	}{
		{"a package folder is built in place",
			func(f string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg")}
			},
			cfg{"__buildDir": "@pkg", "program": ".", "env": map[string]any{"A": "tools", "B": "tools"}}},
		{"a test is built in place too",
			func(f string) cfg {
				return cfg{"request": "launch", "mode": "test", "program": filepath.Join(f, "pkg")}
			},
			cfg{"__buildDir": "@pkg", "program": "."}},
		{"a file is built from its folder",
			func(f string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg", "a.go")}
			},
			cfg{"__buildDir": "@pkg", "program": "./a.go"}},
		{"relative paths are relative to the folder",
			func(string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": "pkg", "cwd": "pkg", "output": "bin/app"}
			},
			cfg{"__buildDir": "@pkg", "program": ".", "cwd": "@pkg", "output": "@bin/app"}},
		{"a package path that is not a folder is left to dlv",
			func(string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": "example.com/m/cmd/tool"}
			},
			cfg{"program": "example.com/m/cmd/tool", "__buildDir": nil}},
		{"a program that is run is not built",
			func(string) cfg { return cfg{"request": "launch", "mode": "exec", "program": "bin/app"} },
			cfg{"program": "@bin/app", "__buildDir": nil}},
		{"a dlv that runs already builds nothing",
			func(f string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"), "port": float64(2345)}
			},
			cfg{"program": "@pkg", "__buildDir": nil}},
		{"the environment: tools, files in order, then env",
			func(f string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"),
					"envFile": []any{filepath.Join(f, "one.env"), filepath.Join(f, "two.env")}, "env": map[string]any{"C": "own", "N": float64(7)}}
			},
			cfg{"env": map[string]any{"A": "tools", "B": "second", "C": "own", "D": "quoted\nline", "E": "single", "F": "end", "G": "${A}", "H": "spaced", "I": "2", "N": float64(7)}}},
		{"one environment file, with the variables of the tools in its values",
			func(f string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"), "envFile": filepath.Join(f, "one.env")}
			},
			cfg{"env": map[string]any{"A": "tools", "B": "file", "C": "tools-x", "D": "quoted\nline", "E": "single", "F": "end", "G": "${A}", "H": "spaced"}}},
		{"args given as a string are split",
			func(f string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"), "args": `-a "b c" 'd e' f\"g "" h`}
			},
			cfg{"args": []any{"-a", "b c", "d e", `f"g`, "", "h"}}},
		{"args given as a list stay",
			func(f string) cfg {
				return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"), "args": []any{"x y"}}
			},
			cfg{"args": []any{"x y"}}},
		{"a process id given as a string is a number",
			func(string) cfg { return cfg{"request": "attach", "mode": "local", "processId": "4242"} },
			cfg{"processId": float64(4242)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{settings: tools})
			debugTree(t, r)
			got := r.substitute(tt.in(r.folder))
			if got.err != nil {
				t.Fatal(got.err)
			}
			if _, has := got.cfg["envFile"]; has {
				t.Error("envFile is still there")
			}
			for k, want := range tt.want {
				want = r.expand(want)
				if !reflect.DeepEqual(got.cfg[k], want) {
					t.Errorf("%s = %#v, want %#v", k, got.cfg[k], want)
				}
			}
		})
	}
}

// expand replaces @pkg and @x/y in the expected values with paths in the folder of the rig.
func (r *rig) expand(v any) any {
	s, ok := v.(string)
	if !ok || !strings.HasPrefix(s, "@") {
		return v
	}
	return filepath.Join(r.folder, strings.TrimPrefix(s, "@"))
}

func TestTheSecondResolutionDefaultsTheEnvironment(t *testing.T) {
	r, _ := debugRig(t, rigOptions{})
	got := r.substitute(cfg{"request": "attach", "mode": "local", "processId": float64(1)})
	if got.err != nil || !reflect.DeepEqual(got.cfg["env"], map[string]any{}) {
		t.Errorf("env = %#v, %v", got.cfg["env"], got.err)
	}
}

func TestAnEmptyDebugAdapterCancelsTheLaunch(t *testing.T) {
	r, _ := debugRig(t, rigOptions{})
	if got := r.substitute(cfg{"request": "launch", "debugAdapter": ""}); got.err != nil || got.cfg != nil {
		t.Errorf("resolve = %+v", got)
	}
}

func TestTheSecondResolutionRefusesWhatCannotRun(t *testing.T) {
	tests := []struct {
		name  string
		in    func(folder string) cfg
		route string
	}{
		{"no program", func(string) cfg { return cfg{"request": "launch", "mode": "debug"} }, "parseDebugProgram.GoDebugProgramInvalid"},
		{"a program that is not a folder or a Go file", func(string) cfg { return cfg{"request": "launch", "mode": "debug", "program": "notes.txt"} }, "parseDebugProgram.GoDebugProgramInvalid"},
		{"a program that is not there", func(string) cfg { return cfg{"request": "launch", "mode": "test", "program": "nowhere"} }, "parseDebugProgram.GoDebugProgramInvalid"},
		{"args with an open quote", func(f string) cfg {
			return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"), "args": `a "b`}
		}, "parseArgsString.GoDebugArgsInvalid"},
		{"an environment file that is not there", func(f string) cfg {
			return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"), "envFile": filepath.Join(f, "missing.env")}
		}, "readDebugEnvFile.GoDebugEnvFileReadFailed"},
		{"an environment file that is a number", func(f string) cfg {
			return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"), "envFile": float64(3)}
		}, "envFileList.GoDebugEnvFileInvalid"},
		{"a list of environment files with a number in it", func(f string) cfg {
			return cfg{"request": "launch", "mode": "debug", "program": filepath.Join(f, "pkg"), "envFile": []any{"a.env", float64(3)}}
		}, "envFileList.GoDebugEnvFileInvalid"},
		{"a process id that is not a number", func(string) cfg { return cfg{"request": "attach", "mode": "local", "processId": "abc"} }, "resolveDebugSubstituted.GoDebugProcessInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{})
			debugTree(t, r)
			got := r.substitute(tt.in(r.folder))
			appErr, ok := errors.FromError(got.err)
			if !ok {
				t.Fatalf("err = %v, want an *errors.Error", got.err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("route = %s, want %s", appErr.Route(), tt.route)
			}
		})
	}
}

func TestAnEnvironmentFileThatIsTooBigIsRefused(t *testing.T) {
	r, _ := debugRig(t, rigOptions{})
	debugTree(t, r)
	big := filepath.Join(r.folder, "big.env")
	if err := os.WriteFile(big, []byte(strings.Repeat("A=1\n", maxDebugEnvFileBytes/4+10)), 0o600); err != nil {
		t.Fatal(err)
	}
	got := r.substitute(cfg{"request": "launch", "mode": "debug", "program": filepath.Join(r.folder, "pkg"), "envFile": big})
	appErr, ok := errors.FromError(got.err)
	if !ok || !appErr.HasRoute("readDebugEnvFile.GoDebugEnvFileReadFailed") || !strings.Contains(appErr.Message, "larger than 1 MiB") {
		t.Errorf("err = %v", got.err)
	}
}

func TestParseArgsString(t *testing.T) {
	tests := []struct {
		in   string
		want []any
		bad  bool
	}{
		{"", []any{}, false},
		{"   ", []any{}, false},
		{"a b  c", []any{"a", "b", "c"}, false},
		{`a "b c" d`, []any{"a", "b c", "d"}, false},
		{`'a b'"c d"`, []any{"a bc d"}, false},
		{`-d''`, []any{"-d"}, false},
		{`'' x`, []any{"", "x"}, false},
		{`say \"hi\"`, []any{"say", `"hi"`}, false},
		{`"a \"quoted\" b"`, []any{`a "quoted" b`}, false},
		{`a "b`, nil, true},
		{`a 'b`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseArgsString(tt.in)
			if (err != nil) != tt.bad {
				t.Fatalf("err = %v", err)
			}
			if !tt.bad && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("args = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRemoveGcflags(t *testing.T) {
	tests := []struct {
		in, want string
		removed  bool
	}{
		{"-race", "-race", false},
		{"-gcflags=-N", "", true},
		{"-gcflags -N", "", true},
		{"-race -gcflags=all=-N -l -tags x", "-race -l -tags x", true},
		{`-gcflags="all=-N -l" -race`, " -race", true},
		{"-gcflags='all=-N -l' -gcflags=-S", "", true},
		{"-asmflags=-S", "-asmflags=-S", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, removed := removeGcflags(tt.in)
			if got != tt.want || removed != tt.removed {
				t.Errorf("removeGcflags = %q, %v; want %q, %v", got, removed, tt.want, tt.removed)
			}
		})
	}
}

func TestParseDebugEnv(t *testing.T) {
	global := map[string]string{"G": "global", "A": "global-a"}
	tests := []struct {
		name, in string
		want     map[string]string
	}{
		{"plain and export", "A=1\nexport B=2\n", map[string]string{"A": "1", "B": "2"}},
		{"a byte order mark and carriage returns", "\xef\xbb\xbfA=1\r\nB=2\r\n", map[string]string{"A": "1", "B": "2"}},
		{"quotes", "A=\"x y\"\nB='x y'\nC=\"a\\nb\"\nD='a\\nb'\n", map[string]string{"A": "x y", "B": "x y", "C": "a\nb", "D": `a\nb`}},
		{"a variable of the file, then of the globals, then nothing", "A=file\nB=${A}-${G}-${NOPE}\n", map[string]string{"A": "file", "B": "file-global-"}},
		{"an escaped dollar", "A=\\${G}\nB=x\\$y\n", map[string]string{"A": "${G}", "B": "x$y"}},
		{"a ${ that is not a name stays", "A=${1x}\nB=${G:-d}\n", map[string]string{"A": "${1x}", "B": "${G:-d}"}},
		{"names with dots and hyphens", "a.b-c=1\n", map[string]string{"a.b-c": "1"}},
		{"lines that are not assignments", "# c\n\njust text\n=x\n", map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDebugEnv(tt.in, global); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("env = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTheAdapterIsDlvDapAsAnExecutableServer(t *testing.T) {
	tests := []struct {
		name   string
		cfg    func(r *rig, dlv string) cfg
		folder string
		want   func(r *rig, dlv string) *extension.DebugAdapter
	}{
		{"a launch is built in its package folder",
			func(_ *rig, dlv string) cfg {
				return cfg{"request": "launch", "dlvToolPath": dlv, "__buildDir": "/w/pkg", "env": map[string]any{"A": "1", "N": float64(2)}}
			}, "/w",
			func(_ *rig, dlv string) *extension.DebugAdapter {
				return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer, ConsoleOutput: true, Command: dlv, Args: []string{"dap", "--listen=127.0.0.1:0"}, Cwd: "/w/pkg", Env: map[string]string{"A": "1"}}
			}},
		{"the flags of dlv and its log",
			func(_ *rig, dlv string) cfg {
				return cfg{"request": "launch", "dlvToolPath": dlv, "dlvFlags": []any{"--check-go-version=false", ""}, "showLog": true, "logOutput": "dap,rpc"}
			}, "/w",
			func(_ *rig, dlv string) *extension.DebugAdapter {
				return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer, ConsoleOutput: true, Command: dlv, Cwd: "/w",
					Args: []string{"dap", "--check-go-version=false", "--log=true", "--log-output=dap,rpc", "--listen=127.0.0.1:0"}}
			}},
		{"the output of the log needs the log",
			func(_ *rig, dlv string) cfg {
				return cfg{"request": "launch", "dlvToolPath": dlv, "showLog": false, "logOutput": "dap"}
			}, "/w",
			func(_ *rig, dlv string) *extension.DebugAdapter {
				return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer, ConsoleOutput: true, Command: dlv, Args: []string{"dap", "--listen=127.0.0.1:0"}, Cwd: "/w"}
			}},
		{"an attach runs in the folder of the launch file, not the build folder",
			func(_ *rig, dlv string) cfg {
				return cfg{"request": "attach", "mode": "local", "dlvToolPath": dlv, "__buildDir": "/w/pkg", "processId": float64(7)}
			}, "/w",
			func(_ *rig, dlv string) *extension.DebugAdapter {
				return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer, ConsoleOutput: true, Command: dlv, Args: []string{"dap", "--listen=127.0.0.1:0"}, Cwd: "/w"}
			}},
		{"without a launch file it is the folder of the workspace",
			func(_ *rig, dlv string) cfg { return cfg{"request": "attach", "mode": "local", "dlvToolPath": dlv} }, "",
			func(r *rig, dlv string) *extension.DebugAdapter {
				return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer, ConsoleOutput: true, Command: dlv, Args: []string{"dap", "--listen=127.0.0.1:0"}, Cwd: r.folder}
			}},
		{"without dlvToolPath dlv is looked for",
			func(*rig, string) cfg { return cfg{"request": "launch"} }, "/w",
			func(_ *rig, dlv string) *extension.DebugAdapter {
				return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer, ConsoleOutput: true, Command: dlv, Args: []string{"dap", "--listen=127.0.0.1:0"}, Cwd: "/w"}
			}},
		{"a port is a server that runs already",
			func(*rig, string) cfg { return cfg{"request": "attach", "mode": "remote", "port": float64(2345)} }, "/w",
			func(*rig, string) *extension.DebugAdapter {
				return &extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: "127.0.0.1", Port: 2345}
			}},
		{"a port and a host",
			func(*rig, string) cfg {
				return cfg{"request": "attach", "mode": "remote", "port": float64(4040), "host": "10.1.2.3"}
			}, "/w",
			func(*rig, string) *extension.DebugAdapter {
				return &extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: "10.1.2.3", Port: 4040}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, dlv := debugRig(t, rigOptions{})
			s := r.api.NewDebugSession("s1", DebugType, "name", tt.cfg(r, dlv))
			s.SetFolder(tt.folder)
			got, err := r.api.DebugAdapter(context.Background(), DebugType, s, nil)
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(r, dlv); !reflect.DeepEqual(got, want) {
				t.Errorf("adapter = %+v, want %+v", got, want)
			}
		})
	}
}

func TestTheAdapterOfASessionWithoutDlvFailsAndTellsTheUser(t *testing.T) {
	r := newRig(t, rigOptions{settings: map[string]any{ToolsEnvVarsSetting: map[string]any{"GOPATH": t.TempDir()}}})
	s := r.api.NewDebugSession("s1", DebugType, "name", cfg{"request": "launch"})
	_, err := r.api.DebugAdapter(context.Background(), DebugType, s, nil)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("dlvProgram.GoDlvNotFound") {
		t.Fatalf("err = %v", err)
	}
	if notes := r.api.Notes(); len(notes) != 1 || notes[0].Severity != "warn" || !reflect.DeepEqual(notes[0].Actions, []string{"Install"}) {
		t.Errorf("notes = %+v", notes)
	}
}
