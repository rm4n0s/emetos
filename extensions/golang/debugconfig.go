package golang

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The configuration provider of the Go debugger: the configurations a new launch.json starts with, the defaults of a configuration
// before its variables are substituted, and its environment, paths and arguments after (SPEC §10.31 "The debuggers", Go).

// Ported from VS Code Go: extension/src/goDebugConfiguration.ts and extension/src/utils/{envUtils,argsUtil}.ts.

// provideDebugConfigurations asks which configuration a new launch.json starts with (VS Code Go's pickConfiguration): launching the
// package of the open file, attaching to a local process or connecting to a server. It blocks until the user chose, so it runs off the
// UI thread. A closed list gives no configuration.
func (g *golang) provideDebugConfigurations(ctx context.Context, _ string) ([]extension.DebugConfiguration, error) {
	choices := []struct {
		label, description string
		config             extension.DebugConfiguration
	}{
		{"Go: Launch Package", "Debug/test the package of the open file", extension.DebugConfiguration{
			"name": "Launch Package", "type": DebugType, "request": "launch", "mode": "auto", "program": "${fileDirname}"}},
		{"Go: Attach to local process", "Attach to an existing process by process ID", extension.DebugConfiguration{
			"name": "Attach to Process", "type": DebugType, "request": "attach", "mode": "local", "processId": float64(0)}},
		{"Go: Connect to server", "Connect to a remote headless debug server", extension.DebugConfiguration{
			"name": "Connect to server", "type": DebugType, "request": "attach", "mode": "remote", "remotePath": "${workspaceFolder}",
			"port": float64(2345), "host": "127.0.0.1"}},
	}
	items := make([]extension.QuickPickItem, len(choices))
	for i, c := range choices {
		items[i] = extension.QuickPickItem{Label: c.label, Description: c.description}
	}
	index, err := g.pickItem(ctx, extension.QuickPickOptions{Placeholder: "Choose debug configuration", Items: items, IgnoreFocusOut: true})
	if err != nil {
		return nil, err
	}
	if index < 0 {
		return nil, nil
	}
	cfg := maps.Clone(choices[index].config)
	if choices[index].config["mode"] == "remote" {
		host, ok, err := g.askText(ctx, "Enter hostname", "127.0.0.1", "")
		if err != nil {
			return nil, err
		}
		if ok && host != "" {
			cfg["host"] = host
		}
		portText, ok, err := g.askText(ctx, "Enter port", "2345", "number")
		if err != nil {
			return nil, err
		}
		if port, isNumber := intAttribute(portText); ok && isNumber && port != 0 {
			cfg["port"] = float64(port)
		}
	}
	return []extension.DebugConfiguration{cfg}, nil
}

// pickItem shows a quick pick from a goroutine and waits for the answer: the index of the item, or -1 when the list was closed. It
// stops waiting when ctx ends.
func (g *golang) pickItem(ctx context.Context, opts extension.QuickPickOptions) (int, error) {
	ch := make(chan int, 1)
	opts.Origin = extension.OriginFromContext(ctx) // the screen of whoever started the session (decision 1500)
	if err := g.api.QuickPick().Show(opts, func(i int) { ch <- i }); err != nil {
		return -1, errors.NewErr("GoDebugPickFailed", err)
	}
	select {
	case i := <-ch:
		return i, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// askText asks for a line of text from a goroutine; ok is false when the box was closed. kind "number" refuses text that is not a number.
func (g *golang) askText(ctx context.Context, prompt, value, kind string) (text string, ok bool, err error) {
	type answer struct {
		text string
		ok   bool
	}
	ch := make(chan answer, 1)
	opts := extension.InputBoxOptions{Prompt: prompt, Value: value}
	if kind == "number" {
		opts.Validate = func(s string) string {
			if _, isNumber := intAttribute(s); !isNumber {
				return "Please enter a number."
			}
			return ""
		}
	}
	opts.Origin = extension.OriginFromContext(ctx)
	if err := g.api.InputBox().Show(opts, func(s string, accepted bool) { ch <- answer{s, accepted} }); err != nil {
		return "", false, errors.NewErr("GoDebugPickFailed", err)
	}
	select {
	case a := <-ch:
		return a.text, a.ok, nil
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}

// resolveDebug completes a configuration before its variables are substituted (resolveDebugConfiguration). A configuration with no
// request is the "Debug" of F5 with no launch.json: it launches the package of the Go file in front, and there is none to launch
// when the editor in front is not a Go file (the launch is cancelled). It returns nil, nil when the launch is cancelled: dlv is
// missing, or the editor is not Go.
func (g *golang) resolveDebug(ctx context.Context, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	cfg = maps.Clone(cfg)
	ed := g.activeEditor()
	editorPath, editorIsFile := filePath(ed.uri)
	if cfg["request"] == nil || cfg["request"] == "" {
		if !editorIsFile || ed.language != "go" {
			g.api.Notifications().Info("Debug needs a Go file: open one, or add a configuration to launch.json.")
			return nil, nil
		}
		if cfg == nil {
			cfg = extension.DebugConfiguration{}
		}
		cfg["name"], cfg["request"], cfg["mode"], cfg["program"] = "Launch", "launch", "auto", filepath.Dir(editorPath)
	}
	if cfg["type"] == nil || cfg["type"] == "" {
		cfg["type"] = DebugType
	}
	request, _ := cfg["request"].(string)
	if cfg["mode"] == nil || cfg["mode"] == "" {
		switch request {
		case "launch":
			cfg["mode"] = "auto"
		case "attach":
			cfg["mode"] = "local"
		}
	}
	if adapter, ok := cfg["debugAdapter"].(string); ok && adapter != "" && adapter != debugAdapterName {
		g.warnOnce("legacy", "The debug adapter \""+adapter+"\" is not available; dlv dap is used.")
	}
	cfg["debugAdapter"] = debugAdapterName

	// the defaults of go.delveConfig for what the configuration does not set
	for _, name := range []string{"showRegisters", "showGlobalVariables", "substitutePath", "showLog", "logOutput", "dlvFlags", "hideSystemGoroutines"} {
		if _, has := cfg[name]; !has {
			cfg[name] = g.delveSetting(name)
		}
	}
	if cwd, ok := cfg["cwd"].(string); ok && cwd != "" {
		cfg["cwd"] = resolveHome(cwd)
	}

	dlv, err := g.dlvProgram()
	if err != nil {
		g.api.Log().Warn("dlv was not found", "err", err)
		g.promptMissingDlv()
		return nil, nil
	}
	cfg["dlvToolPath"] = dlv

	if flags, ok := cfg["buildFlags"]; ok {
		cleaned, removed := removeGcflagsFrom(flags)
		cfg["buildFlags"] = cleaned
		if removed {
			g.warnOnce("gcflags", "The build flag '-gcflags' in 'buildFlags' is ignored: dlv sets the flags that make the program debuggable.")
		}
	}
	if env, ok := cfg["env"].(map[string]any); ok {
		if flags, ok := env["GOFLAGS"].(string); ok {
			if cleaned, removed := removeGcflags(flags); removed {
				env = maps.Clone(env)
				env["GOFLAGS"] = cleaned
				cfg["env"] = env
				g.warnOnce("gcflags-goflags", "The build flag '-gcflags' in 'GOFLAGS' is ignored: dlv sets the flags that make the program debuggable.")
			}
		}
	}

	if cfg["mode"] == "auto" {
		// the file that decides is the program when it is a file, else the editor in front
		file := editorPath
		if p, _ := cfg["program"].(string); strings.HasSuffix(p, ".go") {
			file = p
		}
		if strings.HasSuffix(file, "_test.go") {
			cfg["mode"] = "test"
		} else {
			cfg["mode"] = "debug"
		}
	}
	if program, _ := cfg["program"].(string); cfg["mode"] == "test" && strings.HasSuffix(program, "_test.go") {
		// a test file cannot be run as a file: the package is
		cfg["program"] = filepath.Dir(program)
	}
	if request == "launch" && cfg["mode"] == "remote" {
		g.warnOnce("launchRemote", "The request type 'launch' with mode 'remote' is deprecated: use the request type 'attach' with mode 'remote'.")
	}

	if request == "attach" && cfg["mode"] == "local" {
		if err := g.resolveProcessID(ctx, cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// warnOnce shows a warning the first time the key is seen in the activation. It may be called from any goroutine.
func (g *golang) warnOnce(key, message string) {
	g.warnMu.Lock()
	seen := g.warned[key]
	g.warned[key] = true
	g.warnMu.Unlock()
	if !seen {
		g.api.Notifications().Warn(message)
	}
}

// resolveHome expands a leading ~ to the home folder, which dlv would not.
func resolveHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// gcflagsRe finds -gcflags and its value (VS Code Go's regular expression): the flag follows a space or the start, then comes = or a
// space, then a value of words and quoted strings.
var gcflagsRe = regexp.MustCompile(`(^|\s)(-gcflags)(=| )('[^']*'|"[^"]*"|[^'"\s]+)+`)

// removeGcflags drops every -gcflags flag of a string of build flags.
func removeGcflags(flags string) (string, bool) {
	removed := false
	for gcflagsRe.MatchString(flags) {
		flags = gcflagsRe.ReplaceAllString(flags, "")
		removed = true
	}
	return flags, removed
}

// removeGcflagsFrom drops the -gcflags flags of build flags given as a string or as a list ("-gcflags=x", or "-gcflags" and its
// value as two items).
func removeGcflagsFrom(flags any) (any, bool) {
	switch f := flags.(type) {
	case string:
		return removeGcflags(f)
	case []any:
		var out []any
		removed := false
		for i := 0; i < len(f); i++ {
			s, _ := f[i].(string)
			switch {
			case s == "-gcflags" && i+1 < len(f):
				i++
				removed = true
			case strings.HasPrefix(s, "-gcflags="):
				removed = true
			default:
				out = append(out, f[i])
			}
		}
		return out, removed
	}
	return flags, false
}

// resolveDebugSubstituted completes a configuration after its variables are substituted (resolveDebugConfigurationWithSubstitutedVariables):
// the environment is go.toolsEnvVars, then the files of envFile, then env; the relative paths of cwd, output and program are made
// absolute; a launch that builds the program (the modes debug, test and auto) gets the folder to build in (__buildDir) and a program
// relative to it; args given as a string become a list; a process id given as a string becomes a number.
func (g *golang) resolveDebugSubstituted(_ context.Context, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	if adapter, ok := cfg["debugAdapter"].(string); ok && adapter == "" {
		return nil, nil
	}
	cfg = maps.Clone(cfg)
	tools := g.toolsEnvVars()
	env := map[string]any{}
	for k, v := range tools {
		env[k] = v
	}
	files, err := envFileList(cfg["envFile"])
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		vars, err := readDebugEnvFile(file, tools)
		if err != nil {
			return nil, err
		}
		for k, v := range vars {
			env[k] = v
		}
	}
	if own, ok := cfg["env"].(map[string]any); ok {
		for k, v := range own {
			env[k] = v
		}
	}
	cfg["env"] = env
	delete(cfg, "envFile")

	if folder == "" {
		folder = g.api.Scope().Folder
	}
	makeRelative := func(s string) string {
		if folder == "" {
			g.warnOnce("relative", "The behavior of relative paths without a workspace folder for cwd, program or output is undefined.")
			return s
		}
		return filepath.Join(folder, s)
	}
	for _, attr := range []string{"cwd", "output", "program"} {
		value, _ := cfg[attr].(string)
		if value == "" || filepath.IsAbs(value) {
			continue
		}
		if attr != "program" || !isPackageURL(value) || isFSPath(value, folder) {
			cfg[attr] = makeRelative(value)
		}
	}
	request, _ := cfg["request"].(string)
	if request == "launch" {
		mode, _ := cfg["mode"].(string)
		if mode == "" {
			mode = "debug"
		}
		if mode == "debug" || mode == "test" || mode == "auto" {
			program, _ := cfg["program"].(string)
			dir, programIsDir, err := parseDebugProgram(program, folder)
			if err != nil {
				return nil, err
			}
			// an externally started dlv (port, debugServer) builds nothing here
			if dir != "" && cfg["port"] == nil && cfg["debugServer"] == nil {
				cfg["__buildDir"] = dir
				if programIsDir {
					cfg["program"] = "."
				} else {
					cfg["program"] = "." + string(filepath.Separator) + mustRel(dir, program)
				}
			}
		}
	}
	if args, ok := cfg["args"].(string); ok && request == "launch" {
		list, err := parseArgsString(args)
		if err != nil {
			return nil, err
		}
		cfg["args"] = list
	}
	if request == "attach" && cfg["mode"] == "local" {
		if s, ok := cfg["processId"].(string); ok {
			pid, isNumber := intAttribute(s)
			if !isNumber {
				return nil, errors.New("GoDebugProcessInvalid", "processId is not a process id: "+s, "processId", s)
			}
			cfg["processId"] = float64(pid)
		}
	}
	return cfg, nil
}

// mustRel is the path of target relative to base; the target itself when it cannot be made relative.
func mustRel(base, target string) string {
	if rel, err := filepath.Rel(base, target); err == nil {
		return rel
	}
	return target
}

// toolsEnvVars are the variables of go.toolsEnvVars.
func (g *golang) toolsEnvVars() map[string]string {
	out := map[string]string{}
	if m, err := extension.SettingValue[map[string]any](g.api.Settings(), ToolsEnvVarsSetting); err == nil {
		for k, v := range m {
			if s, ok := v.(string); ok && k != "" {
				out[k] = s
			}
		}
	}
	return out
}

// envFileList is the files of the envFile attribute: one path, or a list of them.
func envFileList(v any) ([]string, error) {
	switch f := v.(type) {
	case nil:
		return nil, nil
	case string:
		if f == "" {
			return nil, nil
		}
		return []string{f}, nil
	case []any:
		var out []string
		for _, e := range f {
			s, ok := e.(string)
			if !ok {
				return nil, errors.New("GoDebugEnvFileInvalid", "envFile is a path or a list of paths")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, errors.New("GoDebugEnvFileInvalid", "envFile is a path or a list of paths")
}

// maxDebugEnvFileBytes is the largest environment file that is read.
const maxDebugEnvFileBytes = 1 << 20

// readDebugEnvFile reads an environment file of a launch configuration.
func readDebugEnvFile(path string, global map[string]string) (map[string]string, error) {
	if st, err := os.Stat(path); err == nil && st.Size() > maxDebugEnvFileBytes {
		return nil, errors.New("GoDebugEnvFileReadFailed", "Cannot load environment variables from file "+path+": the file is larger than 1 MiB", "path", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.NewErr("GoDebugEnvFileReadFailed", err).SetMetadata("path", path)
	}
	return parseDebugEnv(string(data), global), nil
}

var (
	envLineRe  = regexp.MustCompile(`^\s*(export\s+)?([\w.\-]+)\s*=\s*(.*)?\s*$`)
	envQuoteRe = regexp.MustCompile(`(^['"]|['"]$)`)
	envSubstRe = regexp.MustCompile(`\$\{([a-zA-Z]\w*)?([^}\w][^}]*)?\}`)
)

// parseDebugEnv reads the lines NAME=value or export NAME=value of an environment file (VS Code Go's parseEnvFile): a value may be
// quoted ("\n" in double quotes is a line feed), and ${NAME} in a value is replaced by an earlier variable of the file, else by one
// of global, else by nothing. A backslash before the $ keeps it, and `\$` becomes `$`.
func parseDebugEnv(text string, global map[string]string) map[string]string {
	env := map[string]string{}
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	for _, line := range strings.Split(text, "\n") {
		m := envLineRe.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if m == nil {
			continue
		}
		value := m[3]
		if len(value) > 0 && value[0] == '"' && value[len(value)-1] == '"' {
			value = strings.ReplaceAll(value, `\n`, "\n")
		}
		value = envQuoteRe.ReplaceAllString(value, "")
		env[m[2]] = substituteEnvVars(strings.TrimRight(value, " \t"), env, global)
	}
	return env
}

// substituteEnvVars replaces ${NAME} in a value; a value with a ${...} that is not a plain name is left as it is.
func substituteEnvVars(value string, local, global map[string]string) string {
	invalid := false
	replaced := envSubstRe.ReplaceAllStringFunc(value, func(match string) string {
		at := strings.Index(value, match)
		if at > 0 && value[at-1] == '\\' {
			return match
		}
		sub := envSubstRe.FindStringSubmatch(match)
		if sub[2] != "" || sub[1] == "" {
			invalid = true
			return match
		}
		if v := local[sub[1]]; v != "" {
			return v
		}
		return global[sub[1]]
	})
	if !invalid && replaced != value {
		value = replaced
	}
	return strings.ReplaceAll(value, `\$`, "$")
}

// ---- the program ----

// packageURLRe is a program that looks like a package path: a domain, then a slash.
var packageURLRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9](\.[a-zA-Z]{2,})+/`)

// goFileRe is a name with no slash that ends in .go: a file, even when it is not there.
var goFileRe = regexp.MustCompile(`^[^/]*\.go$`)

// isPackageURL tells whether the program may be a package to build (github.com/x/y).
func isPackageURL(s string) bool { return goFileRe.MatchString(s) || packageURLRe.MatchString(s) }

// isFSPath tells whether the string is a file or folder: an absolute path, or one that exists below the folder (or the current folder).
func isFSPath(s, wd string) bool {
	if filepath.IsAbs(s) {
		return true
	}
	if wd != "" {
		s = filepath.Join(wd, s)
	}
	_, err := os.Lstat(s)
	return err == nil
}

// parseDebugProgram reads the program attribute of the launch modes that build: a folder (the build is there), a .go file (the build is
// in its folder) or a package path (dir is empty). It fails with GoDebugProgramInvalid for a missing attribute or a path that is neither
// a folder nor a .go file.
func parseDebugProgram(program, folder string) (dir string, isDir bool, err error) {
	if program == "" {
		return "", false, errors.New("GoDebugProgramInvalid", "The program attribute is missing in the debug configuration in launch.json")
	}
	if isPackageURL(program) && !isFSPath(program, folder) {
		return "", true, nil
	}
	st, statErr := os.Lstat(program)
	if statErr == nil {
		if st.IsDir() {
			return program, true, nil
		}
		if filepath.Ext(program) == ".go" {
			return filepath.Dir(program), false, nil
		}
	}
	return "", false, errors.New("GoDebugProgramInvalid", "The program attribute '"+program+"' must be a valid directory or .go file in debug/test/auto modes.", "program", program)
}

// ---- args ----

// parseArgsString splits a string of arguments as a shell would (VS Code Go's parseArgsString): spaces separate words, single and double
// quotes keep spaces in a word, a backslash before a quote is the quote, and an empty pair of quotes is an empty argument unless it is
// part of a word.
func parseArgsString(args string) ([]any, error) {
	result := []any{}
	var word strings.Builder
	buffered := false
	for i := 0; i < len(args); {
		c := args[i]
		switch {
		case c == '\'' || c == '"':
			j, k := i+1, i+1
			for k < len(args) && args[k] != c {
				if args[k] == '\\' && k+1 < len(args) && (args[k+1] == '\'' || args[k+1] == '"') {
					word.WriteString(args[j:k])
					word.WriteByte(args[k+1])
					j, k = k+2, k+2
				} else {
					k++
				}
			}
			if k >= len(args) {
				kind := "double quotes (\")"
				if c == '\'' {
					kind = "single quotes (')"
				}
				return nil, errors.New("GoDebugArgsInvalid", "args has unmatched "+kind+". starting index: "+strconv.Itoa(i), "index", i)
			}
			buffered = true
			word.WriteString(args[j:k])
			i = k + 1
		case c == '\\' && i+1 < len(args) && (args[i+1] == '\'' || args[i+1] == '"'):
			buffered = true
			word.WriteByte(args[i+1])
			i += 2
		case c != ' ':
			j := i + 1
			for j < len(args) && args[j] != ' ' && args[j] != '\'' && args[j] != '"' && args[j] != '\\' {
				j++
			}
			buffered = true
			word.WriteString(args[i:j])
			i = j
		case buffered:
			result = append(result, word.String())
			word.Reset()
			buffered = false
			i++
		default:
			i++
		}
	}
	if buffered {
		result = append(result, word.String())
	}
	return result, nil
}
