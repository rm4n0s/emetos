package javascript

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/tsconfig"
)

// The configuration providers of the JavaScript debugger: the configuration a new launch.json starts with, the dynamic ones of the
// dropdown (the scripts of package.json and the file in front), the defaults of a configuration before its variables are substituted,
// and the attach to a process (SPEC §10.31 "The debuggers", JavaScript and TypeScript).

// Ported from vscode-js-debug: src/ui/configuration/nodeDebugConfigurationProvider.ts, nodeDebugConfigurationResolver.ts and
// baseConfigurationResolver.ts, src/ui/debugNpmScript.ts (findScripts), src/ui/processPicker.ts (the process picker, resolveProcessId).

// editorSnapshot is the editor in front, for the providers (they run off the UI thread).
type editorSnapshot struct{ uri, language string }

// The values the providers write or look for.
const (
	// adapterType is the type js-debug's adapter dispatches on: a configuration of the type node is rewritten to it before the launch, as
	// js-debug's own resolver does (the type of the session is then pwa-node, as in VS Code).
	adapterType = DebugTypeOld
	// nodeInternals is the glob of the files of node itself that stepping skips.
	nodeInternals = "<node_internals>/**"
	// pickProcessVariable asks the user which node process to attach to.
	pickProcessVariable = "${command:PickProcess}"
	// inspectorPort is where a node process listens after SIGUSR1 (its --inspect-port default).
	inspectorPort = 9229
	// maxPackageDirs bounds the search for package.json files of the dynamic configurations.
	maxPackageDirs = 2000
	// maxPackageDepth is how deep below the folder package.json files are looked for.
	maxPackageDepth = 4
	// maxPackageBytes bounds what is read of a package.json.
	maxPackageBytes = 4 << 20
)

// launchFromContext is the configuration a new launch.json starts with (createLaunchConfigFromContext): the program is the main or the
// start script of package.json, else the file in front, else index.js or main.js, else ${file}; for TypeScript the output folder of
// tsconfig.json is in outFiles.
func (j *javascript) launchFromContext(folder string) extension.DebugConfiguration {
	cfg := extension.DebugConfiguration{"type": DebugType, "request": "launch", "name": "Launch Program", "skipFiles": []any{nodeInternals}}
	program := ""
	if pkg, ok := readPackage(folder); ok {
		program = guessProgramFromPackage(folder, pkg)
	}
	useSourceMaps := false
	if program == "" {
		if ed := j.editorSnap.Load(); ed != nil && isBreakpointLanguage(ed.language) {
			if file, ok := filePath(ed.uri); ok {
				useSourceMaps = ed.language != "javascript"
				program = relativeToFolder(folder, file)
			}
		}
	}
	if program == "" && folder != "" {
		for _, name := range []string{"index.js", "main.js"} {
			if fileExists(filepath.Join(folder, name)) {
				program = "${workspaceFolder}/" + name
				break
			}
		}
	}
	if program == "" {
		program = "${file}"
	}
	cfg["program"] = program
	if useSourceMaps {
		dir := ""
		if out := tsOutDir(folder); out != "" && !filepath.IsAbs(out) {
			dir = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(out)), "./")
			dir = strings.TrimSuffix(dir, "/") + "/"
		}
		cfg["outFiles"] = []any{"${workspaceFolder}/" + dir + "**/*.js"}
	}
	return cfg
}

// provideInitial is the provider of the configurations a new launch.json starts with.
func (j *javascript) provideInitial(_ context.Context, folder string) ([]extension.DebugConfiguration, error) {
	return []extension.DebugConfiguration{j.launchFromContext(j.folderOf(folder))}, nil
}

// packageJSON is the part of a package.json the providers read.
type packageJSON struct {
	Name    string `json:"name"`
	Main    string `json:"main"`
	Scripts struct {
		Start string `json:"start"`
	} `json:"scripts"`
}

// readPackage reads the package.json of the folder.
func readPackage(folder string) (packageJSON, bool) {
	var pkg packageJSON
	if folder == "" {
		return pkg, false
	}
	data, err := readLimited(filepath.Join(folder, "package.json"))
	if err != nil || json.Unmarshal(data, &pkg) != nil {
		return pkg, false
	}
	return pkg, true
}

// readLimited reads a small file; a bigger one is not a package.json anybody wrote.
func readLimited(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, errors.NewErr("JsDebugFileReadFailed", err).SetMetadata("path", p)
	}
	defer f.Close() //nolint:errcheck // read only
	data, err := io.ReadAll(io.LimitReader(f, maxPackageBytes+1))
	if err != nil {
		return nil, errors.NewErr("JsDebugFileReadFailed", err).SetMetadata("path", p)
	}
	if len(data) > maxPackageBytes {
		return nil, errors.New("JsDebugFileTooLarge", "the file is larger than a package.json or tsconfig.json is", "path", p)
	}
	return data, nil
}

// guessProgramFromPackage finds the entry point in package.json: its main, else the last word of its start script (of the form `node
// server.js`). A program that is there neither as it is nor with .js is not taken. The result is relative to ${workspaceFolder} unless
// it was absolute.
func guessProgramFromPackage(folder string, pkg packageJSON) string {
	program := pkg.Main
	if program == "" && pkg.Scripts.Start != "" {
		if words := strings.Fields(pkg.Scripts.Start); len(words) > 0 {
			program = words[len(words)-1]
		}
	}
	if program == "" {
		return ""
	}
	target := program
	if !filepath.IsAbs(program) {
		target = filepath.Join(folder, program)
		program = "${workspaceFolder}/" + filepath.ToSlash(filepath.Clean(program))
	}
	if !fileExists(target) && !fileExists(target+".js") {
		return ""
	}
	return program
}

// tsOutDir is the outDir of the tsconfig.json of the folder, "" when there is none.
func tsOutDir(folder string) string {
	if folder == "" {
		return ""
	}
	return tsconfig.OutDir(filepath.Join(folder, "tsconfig.json"))
}

// fileExists tells whether the path is a file or folder.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// isBreakpointLanguage tells whether breakpoints can be set in the language.
func isBreakpointLanguage(id string) bool { return slices.Contains(breakpointLanguages, id) }

// filePath is the path of a file: URI.
func filePath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	return u.Path, u.Path != ""
}

// relativeToFolder writes a path as ${workspaceFolder}/relative when it is inside the folder, as it is otherwise.
func relativeToFolder(folder, file string) string {
	if folder == "" {
		return file
	}
	rel, err := filepath.Rel(folder, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return file
	}
	return "${workspaceFolder}/" + filepath.ToSlash(rel)
}

// ---- the dynamic configurations

// script is a script of a package.json.
type script struct {
	name, dir string
}

// provideDynamic gives the configurations the dropdown lists without a launch.json: a "Run Script" for each script of the package.json
// files of the folder, and "Run Current File" when the file in front can be debugged (provide of the dynamic provider of js-debug).
func (j *javascript) provideDynamic(_ context.Context, folder string) ([]extension.DebugConfiguration, error) {
	folder = j.folderOf(folder)
	var list []extension.DebugConfiguration
	scripts := findScripts(folder)
	multiDir := false
	for _, s := range scripts {
		if s.dir != scripts[0].dir {
			multiDir = true
		}
	}
	for _, s := range scripts {
		name := "Run Script: " + s.name
		if multiDir {
			name += " (" + filepath.Base(s.dir) + ")"
		}
		runner := packageManager(s.dir)
		args := []any{"run", s.name}
		cfg := extension.DebugConfiguration{
			"type": DebugType, "request": "launch", "name": name, "runtimeExecutable": runner, "runtimeArgs": args,
			"console": "integratedTerminal", "skipFiles": []any{nodeInternals},
		}
		if s.dir != folder {
			cfg["cwd"] = relativeToFolder(folder, s.dir)
		}
		list = append(list, cfg)
	}
	if ed := j.editorSnap.Load(); ed != nil && isBreakpointLanguage(ed.language) {
		if file, ok := filePath(ed.uri); ok {
			list = append(list, extension.DebugConfiguration{
				"type": DebugType, "request": "launch", "name": "Run Current File", "program": relativeToFolder(folder, file),
			})
		}
	}
	return list, nil
}

// findScripts lists the scripts of the package.json files below the folder, in the order of each file; the folder's own first, then the
// others by path. node_modules and hidden folders are not searched.
func findScripts(folder string) []script {
	if folder == "" {
		return nil
	}
	var dirs []string
	visited := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if visited++; visited > maxPackageDirs {
			return
		}
		if fileExists(filepath.Join(dir, "package.json")) {
			dirs = append(dirs, dir)
		}
		if depth >= maxPackageDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() && e.Name() != "node_modules" && !strings.HasPrefix(e.Name(), ".") {
				walk(filepath.Join(dir, e.Name()), depth+1)
			}
		}
	}
	walk(folder, 0)
	sort.SliceStable(dirs, func(a, b int) bool {
		if (dirs[a] == folder) != (dirs[b] == folder) {
			return dirs[a] == folder
		}
		return dirs[a] < dirs[b]
	})
	var out []script
	for _, dir := range dirs {
		data, err := readLimited(filepath.Join(dir, "package.json"))
		if err != nil {
			continue
		}
		for _, name := range scriptNames(data) {
			out = append(out, script{name: name, dir: dir})
		}
	}
	return out
}

// scriptNames are the names in the "scripts" object of a package.json, in the order of the file.
func scriptNames(data []byte) []string {
	var doc struct {
		Scripts json.RawMessage `json:"scripts"`
	}
	if json.Unmarshal(data, &doc) != nil || len(doc.Scripts) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(doc.Scripts))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var names []string
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return names
		}
		name, _ := key.(string)
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return names
		}
		if name != "" && len(value) > 0 && value[0] == '"' {
			names = append(names, name)
		}
	}
	return names
}

// packageManager is the program that runs the scripts of the package in the folder, by the lock file it has: yarn, pnpm, bun, else npm.
func packageManager(dir string) string {
	for _, c := range []struct{ file, runner string }{
		{"yarn.lock", "yarn"}, {"pnpm-lock.yaml", "pnpm"}, {"bun.lockb", "bun"}, {"bun.lock", "bun"}, {"package-lock.json", "npm"},
	} {
		if fileExists(filepath.Join(dir, c.file)) {
			return c.runner
		}
	}
	return "npm"
}

// ---- the first resolution

// resolveDebug completes a configuration before its variables are substituted (resolveDebugConfiguration). A configuration of a child
// session (it names a __pendingTargetId) is left as it is. One with no name, type and request is the "Debug" of F5 with no launch.json:
// it launches what launchFromContext finds. The type is made pwa-node, which js-debug's adapter dispatches on. A cwd is guessed, a
// remoteRoot gets its localRoot, and a launch in the integrated terminal does not open the Debug Console. It returns nil, nil when the
// launch is cancelled. It fails with JsDebugNoProgram when a launch has nothing to run.
func (j *javascript) resolveDebug(ctx context.Context, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	cfg = maps.Clone(cfg)
	if cfg == nil {
		cfg = extension.DebugConfiguration{}
	}
	if cfg["type"] != nil && cfg["type"] != "" {
		cfg["type"] = adapterType // aliased types are served by the one type of the adapter
	}
	if _, child := cfg["__pendingTargetId"]; child {
		return cfg, nil
	}
	folder = j.folderOf(folder)
	if isEmpty(cfg["name"]) && isEmpty(cfg["type"]) && isEmpty(cfg["request"]) {
		made := j.launchFromContext(folder)
		if noDebug, _ := cfg["noDebug"].(bool); noDebug {
			made["noDebug"] = true
		}
		cfg = made
		cfg["type"] = adapterType
		if cfg["program"] == "${file}" && !j.fileInFront() {
			return nil, errors.New("JsDebugNoProgram", "Cannot find a program to debug: open a JavaScript or TypeScript file, or add a configuration to launch.json")
		}
	}
	request, _ := cfg["request"].(string)
	if isEmpty(cfg["cwd"]) {
		if local, _ := cfg["localRoot"].(string); local != "" {
			cfg["cwd"] = local
		} else {
			program, _ := cfg["program"].(string)
			cfg["cwd"] = guessWorkingDirectory(request, program, folder)
		}
	}
	if remote, _ := cfg["remoteRoot"].(string); remote != "" && isEmpty(cfg["localRoot"]) {
		cfg["localRoot"] = "${workspaceFolder}"
	}
	if request == "launch" {
		if console, _ := cfg["console"].(string); console == "integratedTerminal" && isEmpty(cfg["internalConsoleOptions"]) {
			// the Debug Console is not wanted when the program has the terminal
			cfg["internalConsoleOptions"] = "neverOpen"
		}
		fixInspectFlags(cfg)
	}
	if request == "attach" {
		if s, ok := cfg["processId"].(string); ok && strings.TrimSpace(s) == pickProcessVariable {
			picked, ok, err := j.pickProcess(ctx)
			if err != nil || !ok {
				return nil, err
			}
			cfg["processId"] = picked
		}
	}
	return cfg, nil
}

// fileInFront tells whether the editor in front is a file that can be debugged.
func (j *javascript) fileInFront() bool {
	ed := j.editorSnap.Load()
	return ed != nil && isBreakpointLanguage(ed.language) && strings.HasPrefix(ed.uri, "file:")
}

// isEmpty tells whether an attribute has no value.
func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	}
	return false
}

// guessWorkingDirectory is the cwd of a configuration that has none (guessWorkingDirectory): the folder, else the folder of the
// program.
func guessWorkingDirectory(request, program, folder string) string {
	if folder != "" {
		return folder
	}
	if request == "launch" && program != "" {
		if program == "${file}" {
			return "${fileDirname}"
		}
		if filepath.IsAbs(program) {
			return filepath.Dir(program)
		}
	}
	return "${workspaceFolder}"
}

// fixInspectFlags removes --inspect-brk from the runtime arguments of a launch and asks for stopOnEntry instead, which js-debug does
// itself: the flag would break in its bootloader (fixInspectFlags).
func fixInspectFlags(cfg extension.DebugConfiguration) {
	args, ok := cfg["runtimeArgs"].([]any)
	if !ok || cfg["attachSimplePort"] != nil {
		return
	}
	kept := make([]any, 0, len(args))
	for _, a := range args {
		if s, ok := a.(string); ok && (s == "--inspect-brk" || strings.HasPrefix(s, "--inspect-brk=")) {
			if stop, _ := cfg["stopOnEntry"].(bool); !stop {
				cfg["stopOnEntry"] = true
			}
			continue
		}
		kept = append(kept, a)
	}
	cfg["runtimeArgs"] = kept
}

// ---- the second resolution

// resolveDebugSubstituted completes a configuration after its variables are substituted: an attach to a process becomes an attach to the
// port of the process, a port that is text becomes a number, the cwd must be a folder, and js-debug is told the folder of the workspace.
// It fails with JsDebugProcessInvalid, JsDebugProcessDebugFailed and JsDebugCwdInvalid.
func (j *javascript) resolveDebugSubstituted(_ context.Context, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	cfg = maps.Clone(cfg)
	if _, child := cfg["__pendingTargetId"]; child {
		return cfg, nil
	}
	request, _ := cfg["request"].(string)
	if request == "attach" && cfg["processId"] != nil {
		if err := j.resolveProcessID(cfg); err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"port", "attachSimplePort"} {
		if s, ok := cfg[key].(string); ok {
			n, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return nil, errors.New("JsDebugPortInvalid", key+" is "+strconv.Quote(s)+", which is not a port number", "attribute", key)
			}
			cfg[key] = float64(n)
		}
	}
	if cwd, _ := cfg["cwd"].(string); cwd != "" {
		info, err := os.Stat(cwd)
		switch {
		case err != nil:
			return nil, errors.New("JsDebugCwdInvalid", "The configured `cwd` "+cwd+" does not exist.", "cwd", cwd)
		case !info.IsDir():
			return nil, errors.New("JsDebugCwdInvalid", "The configured `cwd` "+cwd+" is not a folder.", "cwd", cwd)
		}
	}
	if folder = j.folderOf(folder); folder != "" && isEmpty(cfg["__workspaceFolder"]) {
		cfg["__workspaceFolder"] = folder
	}
	return cfg, nil
}

// ---- attaching to a process

// resolveProcessID turns the processId of an attach, "pid" or "pid:port", into a port: a process that is told its debug port is
// attached to it, any other is put into debug mode with SIGUSR1 and listens on 9229 (resolveProcessId).
func (j *javascript) resolveProcessID(cfg extension.DebugConfiguration) error {
	var text string
	switch v := cfg["processId"].(type) {
	case string:
		text = strings.TrimSpace(v)
	case float64:
		text = strconv.Itoa(int(v))
	default:
		return errors.New("JsDebugProcessInvalid", "Attach to process: the processId is not a process id", "processId", v)
	}
	pidText, portText, _ := strings.Cut(text, ":")
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return errors.New("JsDebugProcessInvalid", "Attach to process: '"+text+"' doesn't look like a process id.", "processId", text)
	}
	port := 0
	if portText != "" {
		if port, err = strconv.Atoi(portText); err != nil || port <= 0 || port > 65535 {
			return errors.New("JsDebugProcessInvalid", "Attach to process: '"+text+"' doesn't look like a process id.", "processId", text)
		}
	}
	if port == 0 {
		if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
			return errors.NewErr("JsDebugProcessDebugFailed", err).SetMetadata("pid", pid, "message", "Attach to process: cannot enable debug mode for process '"+strconv.Itoa(pid)+"' ("+err.Error()+").")
		}
		port = inspectorPort
	}
	cfg["port"] = float64(port)
	delete(cfg, "processId")
	return nil
}

// nodeProcess is a process the picker lists.
type nodeProcess struct {
	pid  int
	exe  string
	args string
	port int
}

// inspectFlag finds the port of --inspect, --inspect-brk, --inspect-port and --debug-port, with or without a host.
var inspectFlag = regexp.MustCompile(`^--(?:inspect(?:-brk)?|inspect-port|debug-port)(?:=(?:[^:=]*:)?(\d+))?$`)

// analyseArguments finds the debug port a process was started with: 0 when it has none, 9229 for a bare --inspect.
//
// Ported from vscode-js-debug: src/ui/processPicker.ts (analyseArguments)
func analyseArguments(args []string) (port int) {
	for _, a := range args {
		m := inspectFlag.FindStringSubmatch(a)
		if m == nil {
			continue
		}
		if m[1] != "" {
			p, err := strconv.Atoi(m[1])
			if err == nil && p > 0 && p < 65536 {
				port = p
			}
		} else if port == 0 && strings.HasPrefix(a, "--inspect") && a != "--inspect-port" {
			port = inspectorPort
		}
	}
	return port
}

// nodePattern matches the name of a node program.
var nodePattern = regexp.MustCompile(`(?i)^(?:node|iojs)(?:$|\b)`)

// listProcesses lists the node processes of this machine (and those started with a debug port): it reads the command lines under
// procRoot, as /proc has them. The process of Emetos and the ones that cannot be read are left out.
func (j *javascript) listProcesses() []nodeProcess {
	entries, err := os.ReadDir(j.procRoot)
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var out []nodeProcess
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 || pid == self {
			continue
		}
		data, err := os.ReadFile(filepath.Join(j.procRoot, e.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		words := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		exe := path.Base(filepath.ToSlash(words[0]))
		port := analyseArguments(words[1:])
		if port == 0 && !nodePattern.MatchString(exe) {
			continue
		}
		out = append(out, nodeProcess{pid: pid, exe: exe, args: strings.Join(words[1:], " "), port: port})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].pid < out[b].pid })
	return out
}

// pickProcess asks the user which node process to attach to and returns "pid" or "pid:port". ok is false when the list was closed.
// It fails with JsDebugPickFailed when the host has no quick pick and JsDebugNoProcess when there is no node process.
func (j *javascript) pickProcess(ctx context.Context) (picked string, ok bool, err error) {
	procs := j.listProcesses()
	if len(procs) == 0 {
		return "", false, errors.New("JsDebugNoProcess", "There is no node process to attach to: start the program, with --inspect if you like, and try again")
	}
	items := make([]extension.QuickPickItem, len(procs))
	for i, p := range procs {
		detail := "process id: " + strconv.Itoa(p.pid) + " (SIGUSR1)"
		if p.port != 0 {
			detail = "process id: " + strconv.Itoa(p.pid) + ", debug port: " + strconv.Itoa(p.port) + " (SIGUSR1)"
		}
		items[i] = extension.QuickPickItem{Label: p.exe, Description: p.args, Detail: detail}
	}
	ch := make(chan int, 1)
	opts := extension.QuickPickOptions{Placeholder: "Pick the node.js process to attach to", Items: items, MatchOnDescription: true}
	if err := j.api.QuickPick().Show(opts, func(i int) { ch <- i }); err != nil {
		return "", false, errors.NewErr("JsDebugPickFailed", err)
	}
	var index int
	select {
	case index = <-ch:
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
	if index < 0 || index >= len(procs) {
		return "", false, nil
	}
	p := procs[index]
	if p.port != 0 {
		return strconv.Itoa(p.pid) + ":" + strconv.Itoa(p.port), true, nil
	}
	return strconv.Itoa(p.pid), true, nil
}
