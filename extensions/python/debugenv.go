package python

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rm4n0s/errors"
)

// The environment of a debug session (SPEC §10.31 "The debuggers", Python): the file of environment variables (`.env`) and the
// variables of the debuggee, merged as the Python Debugger of VS Code does.

// Ported from vscode-python-debugger: src/extension/common/variables/environment.ts (parseEnvFile, parseEnvLine, substituteEnvVars,
// appendPaths, mergeVariables) and src/extension/debugger/configuration/resolvers/helper.ts (getDebugEnvironmentVariables).

// maxEnvFile is the largest environment file that is read.
const maxEnvFile = 1 << 20

// readEnvFile reads the environment file at path with the variables of base as the values `${NAME}` falls back to. A file that is not
// there gives no variables, as in VS Code. It fails with PythonDebugEnvFileReadFailed when the file is there and cannot be read, or is
// larger than a mebibyte.
func readEnvFile(path string, base map[string]string) (map[string]string, error) {
	if path == "" {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, nil
	}
	if info.Size() > maxEnvFile {
		return nil, errors.New("PythonDebugEnvFileReadFailed", "the environment file is larger than 1 MiB", "path", path, "size", info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.NewErr("PythonDebugEnvFileReadFailed", err).SetMetadata("path", path)
	}
	return parseEnvFile(string(data), base), nil
}

// envLineRe is a line of the file: a name, an equals sign and the rest. The s flag lets the value hold the line feeds of a quoted value
// that spans lines.
var envLineRe = regexp.MustCompile(`(?s)^\s*(_*[a-zA-Z]\w*)\s*=\s*(.*?)\s*$`)

// parseEnvFile reads the lines of an environment file. A value in quotes may span lines; `${NAME}` is replaced by an earlier variable of
// the file, else by a variable of base, else by nothing, unless a backslash comes before it.
func parseEnvFile(content string, base map[string]string) map[string]string {
	if base == nil {
		base = map[string]string{}
	}
	vars := map[string]string{}
	var line strings.Builder
	var inQuotes, afterEquals bool
	var quote byte
	flush := func() {
		if strings.TrimSpace(line.String()) == "" {
			line.Reset()
			return
		}
		if name, value := parseEnvLine(line.String()); name != "" {
			vars[name] = substituteEnvVars(value, vars, base)
		}
		line.Reset()
	}
	for i := 0; i < len(content); i++ {
		c := content[i]
		switch {
		case c == '=' && !inQuotes:
			afterEquals = true
			line.WriteByte(c)
		case (c == '"' || c == '\'') && afterEquals:
			backslashes := 0
			for j := i - 1; j >= 0 && content[j] == '\\'; j-- {
				backslashes++
			}
			if backslashes%2 == 0 {
				if !inQuotes {
					inQuotes, quote = true, c
				} else if c == quote {
					inQuotes, quote = false, 0
				}
			}
			line.WriteByte(c)
		case c == '\n':
			if inQuotes {
				line.WriteByte(c)
				continue
			}
			flush()
			afterEquals = false
		default:
			line.WriteByte(c)
		}
	}
	flush()
	return vars
}

// parseEnvLine splits a line into the name and the value, without the quotes around the value (inside quotes \n is a line feed).
func parseEnvLine(line string) (name, value string) {
	m := envLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", ""
	}
	value = m[2]
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		value = strings.ReplaceAll(value[1:len(value)-1], `\n`, "\n")
	}
	return m[1], value
}

// substRe is `${NAME}`: the name, then anything that makes the reference invalid, up to the last closing brace of the value.
var substRe = regexp.MustCompile(`\$\{([a-zA-Z]\w*)?([^}\w].*)?\}`)

// substituteEnvVars replaces the references to variables in a value of the file.
func substituteEnvVars(value string, local, global map[string]string) string {
	invalid := false
	var out strings.Builder
	last := 0
	for _, loc := range substRe.FindAllStringSubmatchIndex(value, -1) {
		out.WriteString(value[last:loc[0]])
		last = loc[1]
		match := value[loc[0]:loc[1]]
		switch {
		case loc[0] > 0 && value[loc[0]-1] == '\\':
			out.WriteString(match)
		case loc[4] >= 0 || loc[2] < 0:
			invalid = true
			out.WriteString(match)
		default:
			name := value[loc[2]:loc[3]]
			if v := local[name]; v != "" {
				out.WriteString(v)
			} else {
				out.WriteString(global[name])
			}
		}
	}
	out.WriteString(value[last:])
	if !invalid && out.String() != value {
		value = out.String()
	}
	return strings.ReplaceAll(value, `\$`, "$")
}

// appendPath adds paths to the variable name of vars after what it holds, separated as PATH is (blank ones are left out).
func appendPath(vars map[string]string, name string, paths ...string) {
	var add []string
	for _, p := range paths {
		if p = strings.TrimSpace(p); p != "" {
			add = append(add, p)
		}
	}
	if len(add) == 0 {
		return
	}
	value := strings.Join(add, string(filepath.ListSeparator))
	if old := vars[name]; old != "" {
		value = old + string(filepath.ListSeparator) + value
	}
	vars[name] = value
}

// debugEnvironment is the environment of the debuggee of a launch: the file's variables, then those of env (which win), with the PATH and
// PYTHONPATH of env added to the file's rather than replacing them and the PATH and PYTHONPATH of Emetos added after them, the encoding
// and buffering Python needs for its output to reach the Debug Console, and GEVENT_SUPPORT for gevent. getenv reads the environment of
// Emetos.
func debugEnvironment(env map[string]string, file map[string]string, gevent bool, getenv func(string) string) map[string]string {
	out := map[string]string{}
	for k, v := range file {
		out[k] = v
	}
	for k, v := range env {
		if k != "PYTHONPATH" && k != "PATH" {
			out[k] = v
		}
	}
	appendPath(out, "PATH", env["PATH"])
	appendPath(out, "PYTHONPATH", env["PYTHONPATH"])
	if out["PATH"] != "" {
		appendPath(out, "PATH", getenv("PATH"))
	}
	if out["PYTHONPATH"] != "" {
		appendPath(out, "PYTHONPATH", getenv("PYTHONPATH"))
	}
	if _, has := out["PYTHONIOENCODING"]; !has {
		out["PYTHONIOENCODING"] = "UTF-8"
	}
	if _, has := out["PYTHONUNBUFFERED"]; !has {
		out["PYTHONUNBUFFERED"] = "1"
	}
	if gevent {
		out["GEVENT_SUPPORT"] = "True"
	}
	return out
}
