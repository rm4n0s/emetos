package nativedebug

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The configuration providers of the three types: the dynamic configurations of C and C++, the first resolution (before the variables
// are substituted: the process that an attach asks for) and the second (after: the working folder, the program, the arguments and the
// environment).

// pickProcessVariable asks for a process: gdb's and lldb-dap's `pid` is a number, which a command of an extension cannot give, so the
// provider answers the variable itself (decision 1302). lldb-dap's README calls the same variable ${command:pickMyProcess}.
const (
	pickProcessVariable   = "${command:pickProcess}"
	pickMyProcessVariable = "${command:pickMyProcess}"
)

// DynamicName is the name of the dynamic configuration of C and C++.
const DynamicName = "Debug the program named like the file in front"

// provideDynamic gives the configurations the dropdown of Run and Debug lists with no launch.json: the program that has the name of the
// file in front, without its extension, next to it (what `cc -g -o prog prog.c` makes). A folder that holds only a Cargo.toml is a Rust
// project: the Rust extension gives its targets and this gives none.
func (d *Debugger) provideDynamic(_ context.Context, folder, typ string) ([]extension.DebugConfiguration, error) {
	if rustOnly(d.folderOf(folder)) {
		return nil, nil
	}
	return []extension.DebugConfiguration{{
		"name": DynamicName, "type": typ, "request": "launch",
		"program": "${fileDirname}/${fileBasenameNoExtension}", "args": []any{}, "cwd": "${workspaceFolder}",
	}}, nil
}

// rustOnly tells whether the folder is a Cargo project that has no file of a C or C++ build.
func rustOnly(folder string) bool {
	if folder == "" || !isFile(filepath.Join(folder, "Cargo.toml")) {
		return false
	}
	for _, name := range []string{"CMakeLists.txt", "Makefile", "meson.build", "compile_commands.json", "compile_flags.txt"} {
		if isFile(filepath.Join(folder, name)) {
			return false
		}
	}
	return true
}

// isFile tells whether path is a file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// resolve completes a configuration before its variables are substituted: the type, and the process of an attach that asks for one.
// It blocks on the user, so it runs off the UI thread.
func (d *Debugger) resolve(ctx context.Context, _ string, typ string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	cfg = maps.Clone(cfg)
	if cfg == nil {
		cfg = extension.DebugConfiguration{}
	}
	if cfg["type"] == nil || cfg["type"] == "" {
		cfg["type"] = typ
	}
	if cfg["request"] == "attach" {
		if s, ok := cfg["pid"].(string); ok {
			if s = strings.TrimSpace(s); s == pickProcessVariable || s == pickMyProcessVariable {
				pid, err := d.pickProcess(ctx)
				if err != nil {
					return nil, err
				}
				cfg["pid"] = float64(pid)
			}
		}
	}
	return cfg, nil
}

// resolveSubstituted completes a configuration after its variables are substituted (and after the task that builds the program ran): a
// launch gets its working folder, an absolute program that exists, its arguments as a list and, for gdb, an environment that is whole; an
// attach gets its process id as a number. It fails with NativeDebugProgramInvalid, NativeDebugProgramMissing, NativeDebugArgsInvalid,
// NativeDebugAttachInvalid and NativeDebugEnvInvalid.
func (d *Debugger) resolveSubstituted(_ context.Context, folder, typ string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	cfg = maps.Clone(cfg)
	if cfg == nil {
		return nil, nil
	}
	folder = d.folderOf(folder)
	switch cfg["request"] {
	case "launch":
		return launchSubstituted(typ, folder, cfg)
	case "attach":
		return attachSubstituted(typ, folder, cfg)
	}
	return cfg, nil
}

// launchSubstituted is resolveSubstituted for a launch.
func launchSubstituted(typ, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	cwd, _ := cfg["cwd"].(string)
	cwd = strings.TrimSpace(cwd)
	if cwd != "" && !filepath.IsAbs(cwd) && folder != "" {
		cwd = filepath.Join(folder, cwd)
	}
	if cwd == "" {
		cwd = folder
	}
	if cwd != "" {
		cfg["cwd"] = cwd
	}
	program, _ := cfg["program"].(string)
	program = strings.TrimSpace(program)
	switch {
	case isSet(cfg, CargoAttribute) && program == "":
		// the Rust extension builds it and names the executable
	case program == "" && typ != TypeGDB && isSet(cfg, "launchCommands"):
		// lldb-dap launches by its own commands
	case program == "":
		return nil, errors.New("NativeDebugProgramInvalid", "The configuration needs a \"program\": the executable to debug, built with debug information (-g).",
			"name", cfg["name"])
	case strings.Contains(program, "/"):
		if !filepath.IsAbs(program) && cwd != "" {
			program = filepath.Join(cwd, program)
		}
		if !isFile(program) {
			return nil, errors.New("NativeDebugProgramMissing", "The program "+program+" was not found. Build it with debug information (-g) first.", "program", program)
		}
		cfg["program"] = program
	}
	if s, ok := cfg["args"].(string); ok {
		args, err := splitArgs(s)
		if err != nil {
			return nil, err
		}
		list := make([]any, len(args))
		for i, a := range args {
			list[i] = a
		}
		cfg["args"] = list
	}
	if typ == TypeGDB {
		env, err := gdbEnvironment(cfg["env"])
		if err != nil {
			return nil, err
		}
		if env != nil {
			cfg["env"] = env
		}
	}
	return cfg, nil
}

// attachSubstituted is resolveSubstituted for an attach.
func attachSubstituted(typ, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	if s, ok := cfg["pid"].(string); ok {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n <= 0 {
			return nil, errors.New("NativeDebugAttachInvalid", "\"pid\" must be a process id, or ${command:pickProcess}; it is \""+s+"\"", "pid", s)
		}
		cfg["pid"] = float64(n)
	}
	if program, _ := cfg["program"].(string); strings.Contains(program, "/") && !filepath.IsAbs(program) && folder != "" {
		cfg["program"] = filepath.Join(folder, program)
	}
	switch typ {
	case TypeGDB:
		if !isSet(cfg, "pid") && !isSet(cfg, "target") && !isSet(cfg, "coreFile") {
			return nil, errors.New("NativeDebugAttachInvalid", "\"request\": \"attach\" requires one of \"pid\", \"target\" or \"coreFile\"", "name", cfg["name"])
		}
	default:
		if !isSet(cfg, "pid") && !isSet(cfg, "program") && !isSet(cfg, "coreFile") && !isSet(cfg, "attachCommands") && !isSet(cfg, "gdb-remote-port") {
			return nil, errors.New("NativeDebugAttachInvalid",
				"\"request\": \"attach\" requires one of \"pid\", \"program\", \"coreFile\", \"attachCommands\" or \"gdb-remote-port\"", "name", cfg["name"])
		}
	}
	return cfg, nil
}

// isSet tells whether an attribute has a value: it is there and not null.
func isSet(cfg extension.DebugConfiguration, name string) bool { return cfg[name] != nil }

// gdbEnvironment makes the environment gdb takes: gdb sets the environment of the program exactly as it is given, so the variables of
// the configuration (an object, or a list of NAME=value as lldb-dap takes) are added to those of Emetos. It returns nil when the
// configuration has none. It fails with NativeDebugEnvInvalid.
func gdbEnvironment(v any) (map[string]any, error) {
	var own map[string]string
	switch e := v.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		own = map[string]string{}
		for k, val := range e {
			switch s := val.(type) {
			case string:
				own[k] = s
			case float64, bool:
				own[k] = formatScalar(s)
			case nil:
				own[k] = ""
			default:
				return nil, errors.New("NativeDebugEnvInvalid", "The value of the environment variable "+k+" must be text.", "variable", k)
			}
		}
	case []any:
		own = map[string]string{}
		for _, item := range e {
			s, ok := item.(string)
			if !ok || s == "" {
				return nil, errors.New("NativeDebugEnvInvalid", "\"env\" as a list holds NAME=value texts.")
			}
			name, value, _ := strings.Cut(s, "=")
			own[name] = value
		}
	default:
		return nil, errors.New("NativeDebugEnvInvalid", "\"env\" is an object of variables or a list of NAME=value texts.")
	}
	if len(own) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for _, kv := range os.Environ() {
		if name, value, ok := strings.Cut(kv, "="); ok && name != "" {
			out[name] = value
		}
	}
	for k, val := range own {
		out[k] = val
	}
	return out, nil
}

// formatScalar writes a number or a boolean of an environment variable.
func formatScalar(v any) string {
	switch s := v.(type) {
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(s)
	}
	return ""
}

// splitArgs splits the text of "args" into words as a shell does: spaces separate, single and double quotes group, a backslash
// escapes the next character. It fails with NativeDebugArgsInvalid for a quote that is not closed.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	started := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, started = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("NativeDebugArgsInvalid", "The arguments have a "+string(quote)+" that is not closed: "+s, "args", s)
	}
	if started {
		args = append(args, cur.String())
	}
	return args, nil
}
