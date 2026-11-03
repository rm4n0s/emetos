package rust

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// Debugging Rust programs (SPEC §10.31 "The debuggers", C, C++ and Rust; step 25.9). The debug types gdb and lldb-dap are declared and
// served by the C and C++ extension (a debug type has one owner, extensions/internal/nativedebug); this extension adds to them what Rust
// needs: the dynamic configurations of the dropdown (the targets of `cargo metadata`) and, before a launch, the build: a configuration
// with the attribute `cargo` has cargo run with `--message-format=json` and the executable it names becomes the program (rust-analyzer's
// way of debugging).

// Ported from rust-analyzer: editors/code/src/debug.ts (getDebugConfiguration, getDebugExecutable) and editors/code/src/toolchain.ts
// (Cargo.executableFromArgs, getCargoArtifacts), and the runnables of crates/rust-analyzer/src/target_spec.rs for the names.

// cargoTimeout ends a cargo that takes longer than this to build; the target list of `cargo metadata` gets metadataTimeout.
const (
	cargoTimeout    = 30 * time.Minute
	metadataTimeout = 30 * time.Second
	// maxCargoOutput bounds what is read of the output of cargo.
	maxCargoOutput = 256 << 20
)

// debugTypes are the debug types the extension adds its providers to.
var debugTypes = []string{nativedebug.TypeGDB, nativedebug.TypeLLDB, nativedebug.TypeLLDBOld}

// registerDebug registers the providers of the Rust targets. A host with no debug service has none: the extension then adds nothing to
// the debuggers.
func (r *rust) registerDebug() {
	d := r.api.Debug()
	for _, typ := range debugTypes {
		typ := typ
		build := extension.DebugConfigurationProvider{
			ResolveSubstituted: func(ctx context.Context, folder string, c extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				return r.resolveCargo(ctx, folder, c)
			},
		}
		if _, err := d.RegisterConfigurationProvider(typ, build, extension.DebugTriggerInitial); err != nil {
			r.api.Log().Debug("the Rust debugger has no configuration provider", "type", typ, "err", err)
			return
		}
		if typ == nativedebug.TypeLLDBOld {
			continue
		}
		dynamic := extension.DebugConfigurationProvider{
			Provide: func(ctx context.Context, folder string) ([]extension.DebugConfiguration, error) {
				return r.provideTargets(ctx, folder, typ)
			},
		}
		if _, err := d.RegisterConfigurationProvider(typ, dynamic, extension.DebugTriggerDynamic); err != nil {
			r.api.Log().Debug("the Rust debugger has no dynamic configurations", "type", typ, "err", err)
		}
	}
}

// findCargo finds cargo: PATH, then the folder rustup and cargo install into.
func findCargo() (string, error) {
	if p, err := exec.LookPath("cargo"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".cargo", "bin", "cargo")
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errors.New("RustDebugCargoNotFound", "cargo was not found in PATH or in ~/.cargo/bin; the Rust debugger needs it to build the program")
}

// ---- the targets of the dropdown

// cargoMetadata is the part of the output of `cargo metadata --no-deps` the targets are made from.
type cargoMetadata struct {
	Packages []cargoPackage `json:"packages"`
	// WorkspaceRoot is the folder the paths of the compiler's messages are relative to.
	WorkspaceRoot string `json:"workspace_root"`
}

// cargoPackage is a package of the workspace.
type cargoPackage struct {
	Name    string        `json:"name"`
	Targets []cargoTarget `json:"targets"`
	// ManifestPath is the absolute path of the package's Cargo.toml.
	ManifestPath string `json:"manifest_path"`
}

// cargoTarget is a target of a package: a library, a binary, an example, a test.
type cargoTarget struct {
	Name       string   `json:"name"`
	Kind       []string `json:"kind"`
	CrateTypes []string `json:"crate_types"`
	// Test says that `cargo test` builds the unit tests of the target, and Doctest that it runs the documentation tests of a library.
	Test    bool `json:"test"`
	Doctest bool `json:"doctest"`
	// SrcPath is the absolute path of the root file of the target.
	SrcPath string `json:"src_path"`
}

// hasKind tells whether the target is of the kind.
func (t cargoTarget) hasKind(kinds ...string) bool {
	return slices.ContainsFunc(t.Kind, func(k string) bool { return slices.Contains(kinds, k) })
}

// libraryKinds are the kinds of a library target.
var libraryKinds = []string{"lib", "rlib", "dylib", "cdylib", "staticlib", "proc-macro"}

// target is a configuration of the dropdown in the making.
type target struct {
	rank  int
	name  string
	cargo map[string]any
}

// targetConfigurations makes the dynamic configurations of the debug type from the packages of the workspace: the executables, the
// examples, the unit tests of the libraries and the executables, and the integration tests, each with the `cargo` attribute that builds
// it (rust-analyzer's runnables). With several packages the name of the package follows the name of the target.
func targetConfigurations(meta cargoMetadata, typ string) []extension.DebugConfiguration {
	many := len(meta.Packages) > 1
	var list []target
	add := func(rank int, title, name, pkg string, args []string, kind string) {
		if many {
			title += " (" + pkg + ")"
		}
		list = append(list, target{rank: rank, name: title, cargo: map[string]any{
			"args":   stringsToAny(append(args, "--package="+pkg)),
			"filter": map[string]any{"name": name, "kind": kind},
		}})
	}
	for _, pkg := range meta.Packages {
		for _, t := range pkg.Targets {
			switch {
			case t.hasKind("bin"):
				add(0, "Debug executable '"+t.Name+"'", t.Name, pkg.Name, []string{"build", "--bin=" + t.Name}, "bin")
				if t.Test {
					add(2, "Debug unit tests in executable '"+t.Name+"'", t.Name, pkg.Name, []string{"test", "--no-run", "--bin=" + t.Name}, "bin")
				}
			case t.hasKind("example"):
				add(3, "Debug example '"+t.Name+"'", t.Name, pkg.Name, []string{"build", "--example=" + t.Name}, "example")
			case t.hasKind(libraryKinds...):
				if t.Test {
					kind := "lib"
					if len(t.Kind) > 0 {
						kind = t.Kind[0]
					}
					add(1, "Debug unit tests in library '"+t.Name+"'", t.Name, pkg.Name, []string{"test", "--no-run", "--lib"}, kind)
				}
			case t.hasKind("test"):
				add(4, "Debug integration test '"+t.Name+"'", t.Name, pkg.Name, []string{"test", "--no-run", "--test=" + t.Name}, "test")
			}
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].rank < list[j].rank })
	out := make([]extension.DebugConfiguration, 0, len(list))
	for _, tg := range list {
		out = append(out, extension.DebugConfiguration{
			"name": tg.name, "type": typ, "request": "launch", "cargo": tg.cargo, "args": []any{}, "cwd": "${workspaceFolder}",
		})
	}
	return out
}

// stringsToAny makes the list JSON holds.
func stringsToAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// findCargoToml tells whether the folder or a folder above it holds a Cargo.toml.
func findCargoToml(folder string) bool {
	for dir := filepath.Clean(folder); dir != "" && dir != "."; {
		if info, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil && !info.IsDir() {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
	return false
}

// readMetadata runs `cargo metadata --no-deps` in the folder and reads the packages. It fails with RustDebugCargoNotFound and
// RustDebugMetadataFailed.
func readMetadata(ctx context.Context, folder string, env map[string]string) (cargoMetadata, error) {
	var meta cargoMetadata
	cargo, err := findCargo()
	if err != nil {
		return meta, err
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	out, stderr, err := runCargo(ctx, cargo, []string{"metadata", "--no-deps", "--format-version", "1"}, folder, env)
	if err != nil {
		return meta, errors.New("RustDebugMetadataFailed", "cargo metadata failed: "+err.Error()+lastLines(stderr, 6), "folder", folder)
	}
	if err := json.Unmarshal(out, &meta); err != nil {
		return meta, errors.NewErr("RustDebugMetadataFailed", err).SetMetadata("folder", folder)
	}
	return meta, nil
}

// provideTargets gives the configurations of the dropdown: a configuration for each target of the workspace. A folder with no
// Cargo.toml has none, and a failure of cargo is logged and gives none (the dropdown lists every debugger, and a broken Cargo.toml must
// not take it down).
func (r *rust) provideTargets(ctx context.Context, folder, typ string) ([]extension.DebugConfiguration, error) {
	if folder == "" {
		folder = r.api.Scope().Folder
	}
	if folder == "" || !findCargoToml(folder) {
		return nil, nil
	}
	meta, err := readMetadata(ctx, folder, nil)
	if err != nil {
		r.api.Log().Debug("the Rust targets cannot be listed", "folder", folder, "err", err)
		return nil, nil
	}
	return targetConfigurations(meta, typ), nil
}

// ---- the build before a launch

// cargoRequest is the attribute `cargo` of a configuration.
type cargoRequest struct {
	args   []string
	cwd    string
	env    map[string]string
	filter struct{ name, kind string }
}

// readCargoRequest reads the attribute `cargo`. It fails with RustDebugCargoInvalid.
func readCargoRequest(v any) (cargoRequest, error) {
	var req cargoRequest
	m, ok := v.(map[string]any)
	if !ok {
		return req, errors.New("RustDebugCargoInvalid", "\"cargo\" is an object with the arguments of cargo: {\"args\": [\"build\"]}")
	}
	list, ok := m["args"].([]any)
	if !ok || len(list) == 0 {
		return req, errors.New("RustDebugCargoInvalid", "\"cargo\" needs \"args\", the arguments of cargo, for example [\"build\", \"--bin=app\"]")
	}
	for _, a := range list {
		s, ok := a.(string)
		if !ok {
			return req, errors.New("RustDebugCargoInvalid", "the \"args\" of \"cargo\" are texts")
		}
		req.args = append(req.args, s)
	}
	req.cwd, _ = m["cwd"].(string)
	if env, ok := m["env"].(map[string]any); ok {
		req.env = map[string]string{}
		for k, val := range env {
			if s, ok := val.(string); ok && k != "" {
				req.env[k] = s
			}
		}
	}
	if f, ok := m["filter"].(map[string]any); ok {
		req.filter.name, _ = f["name"].(string)
		req.filter.kind, _ = f["kind"].(string)
	}
	return req, nil
}

// cargoMessage is a line of the JSON cargo prints with --message-format=json that matters here: a compiled artifact, or a message of
// the compiler.
type cargoMessage struct {
	Reason     string  `json:"reason"`
	Executable *string `json:"executable"`
	Target     struct {
		Name string   `json:"name"`
		Kind []string `json:"kind"`
	} `json:"target"`
	Message struct {
		Level    string `json:"level"`
		Rendered string `json:"rendered"`
	} `json:"message"`
}

// artifact is an executable cargo built.
type artifact struct {
	path, name string
	kinds      []string
}

// parseCargoOutput reads the lines cargo printed and returns the executables it built (build scripts are left out) and the rendered
// errors of the compiler.
func parseCargoOutput(out []byte) (artifacts []artifact, problems []string) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var m cargoMessage
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch m.Reason {
		case "compiler-artifact":
			if m.Executable != nil && *m.Executable != "" && !slices.Contains(m.Target.Kind, "custom-build") {
				artifacts = append(artifacts, artifact{path: *m.Executable, name: m.Target.Name, kinds: m.Target.Kind})
			}
		case "compiler-message":
			if m.Message.Level == "error" && m.Message.Rendered != "" {
				problems = append(problems, strings.TrimRight(m.Message.Rendered, "\n"))
			}
		}
	}
	return artifacts, problems
}

// chooseArtifact picks the executable the filter names among the ones cargo built. It fails with RustDebugNoExecutable when there is
// none and RustDebugManyExecutables when more than one is left (the filter, or narrower arguments, choose).
func chooseArtifact(list []artifact, name, kind string) (string, error) {
	var left []artifact
	for _, a := range list {
		if (name == "" || a.name == name) && (kind == "" || slices.Contains(a.kinds, kind)) {
			left = append(left, a)
		}
	}
	switch len(left) {
	case 0:
		why := "cargo built no executable"
		if len(list) > 0 {
			why = "none of the executables cargo built matches the filter"
		}
		return "", errors.New("RustDebugNoExecutable", why+" (name \""+name+"\", kind \""+kind+"\"). Check the arguments of \"cargo\".", "name", name, "kind", kind)
	case 1:
		return left[0].path, nil
	}
	names := make([]string, len(left))
	for i, a := range left {
		names[i] = a.name + " (" + strings.Join(a.kinds, ", ") + ")"
	}
	return "", errors.New("RustDebugManyExecutables", "cargo built more than one executable: "+strings.Join(names, ", ")+
		". Narrow the arguments (--bin, --test...) or set \"filter\" in \"cargo\".", "count", len(left))
}

// withMessageFormat adds --message-format=json after the subcommand unless the arguments name a format.
func withMessageFormat(args []string) []string {
	for _, a := range args {
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "--message-format") {
			return args
		}
	}
	out := slices.Clone(args)
	i := slices.Index(out, "--")
	if i < 0 {
		i = len(out)
	}
	return slices.Insert(out, min(1, i), "--message-format=json")
}

// resolveCargo runs the cargo of a configuration that has the attribute `cargo` and sets program to the executable it names. It runs
// after the variables are substituted and the task before the launch ran. The build can take long, so it runs off the UI thread. A
// configuration without `cargo` is left as it is. It fails with RustDebugCargoInvalid, RustDebugCargoNotFound, RustDebugCargoFailed,
// RustDebugNoExecutable and RustDebugManyExecutables.
func (r *rust) resolveCargo(ctx context.Context, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	if cfg == nil || cfg[nativedebug.CargoAttribute] == nil || cfg["request"] != "launch" {
		return cfg, nil
	}
	cfg = maps.Clone(cfg)
	req, err := readCargoRequest(cfg[nativedebug.CargoAttribute])
	if err != nil {
		return nil, err
	}
	if p, _ := cfg["program"].(string); strings.TrimSpace(p) != "" {
		delete(cfg, nativedebug.CargoAttribute) // the program is named: nothing to build
		return cfg, nil
	}
	if folder == "" {
		folder = r.api.Scope().Folder
	}
	dir := req.cwd
	switch {
	case dir == "":
		dir = folder
	case !filepath.IsAbs(dir) && folder != "":
		dir = filepath.Join(folder, dir)
	}
	cargo, err := findCargo()
	if err != nil {
		return nil, err
	}
	args := withMessageFormat(req.args)
	r.consoleLine("> cargo " + strings.Join(req.args, " "))
	ctx, cancel := context.WithTimeout(ctx, cargoTimeout)
	defer cancel()
	out, stderr, runErr := runCargo(ctx, cargo, args, dir, req.env)
	artifacts, problems := parseCargoOutput(out)
	if runErr != nil {
		text := strings.Join(problems, "\n")
		if text == "" {
			text = lastLines(stderr, 12)
		}
		r.consoleLine(strings.TrimSpace(text))
		return nil, errors.New("RustDebugCargoFailed", "cargo "+strings.Join(req.args, " ")+" failed: "+runErr.Error()+"\n"+firstLines(text, 12), "args", strings.Join(req.args, " "))
	}
	program, err := chooseArtifact(artifacts, req.filter.name, req.filter.kind)
	if err != nil {
		return nil, err
	}
	cfg["program"] = program
	delete(cfg, nativedebug.CargoAttribute)
	return cfg, nil
}

// consoleLine writes a line to the Debug Console.
func (r *rust) consoleLine(text string) {
	if text == "" {
		return
	}
	if c := r.api.Debug().Console(); c != nil {
		c.AppendLine(text)
	}
}

// runCargo runs cargo with the arguments in the folder, with the environment of Emetos and env added, in a process group of its own
// that is killed when ctx ends. It returns what cargo printed on its standard output and error, and the error of the run; no
// environment variable of the user's cargo is set here.
func runCargo(ctx context.Context, cargo string, args []string, dir string, env map[string]string) (stdout []byte, stderr string, err error) {
	cmd := exec.Command(cargo, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for _, k := range sortedKeys(env) {
		cmd.Env = append(cmd.Env, k+"="+env[k])
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out, errOut bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: maxCargoOutput}
	cmd.Stderr = &limitedWriter{w: &errOut, left: 1 << 20}
	if err := cmd.Start(); err != nil {
		return nil, "", errors.NewErr("RustDebugCargoFailed", err).SetMetadata("cargo", cargo)
	}
	pgid := cmd.Process.Pid
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		case <-done:
		}
	}()
	waitErr := cmd.Wait()
	close(done)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	if waitErr != nil && ctx.Err() != nil {
		waitErr = ctx.Err()
	}
	return out.Bytes(), errOut.String(), waitErr
}

// sortedKeys lists the keys of a map in order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// limitedWriter writes at most left bytes and drops the rest.
type limitedWriter struct {
	w    io.Writer
	left int
}

// Write implements io.Writer.
func (l *limitedWriter) Write(b []byte) (int, error) {
	n := len(b)
	if n > l.left {
		b = b[:l.left]
	}
	l.left -= len(b)
	if _, err := l.w.Write(b); err != nil {
		return 0, err
	}
	return n, nil
}

// lastLines is the end of a text: a newline and its last n lines, "" when it has none.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "\n" + strings.Join(lines, "\n")
}

// firstLines is the start of a text: its first n lines.
func firstLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}
