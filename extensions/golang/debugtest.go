package golang

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Debugging a test (SPEC §10.31 "The debuggers", Go; §10.17): the lenses "debug test" and "debug benchmark" and the command "Go: Debug Test
// At Cursor" start a session of the Go debugger with the mode test, whose arguments select the function.

// Ported from VS Code Go: extension/src/goTest.ts (debugTestAtCursor) and extension/src/testUtils.ts (getTestFunctionDebugArgs).

// debugTestAtCursorCommand is the command "Go: Debug Test At Cursor": the test, benchmark, example or fuzz function at the cursor.
func (g *golang) debugTestAtCursorCommand(ctx context.Context, args any) error {
	const what = "Debug Test At Cursor"
	file, ok := g.testTarget(ctx, args, what)
	if !ok {
		return nil
	}
	base := filepath.Base(file.path)
	if !strings.HasSuffix(file.path, "_test.go") {
		g.api.Notifications().Info(what + ": " + base + " is not a test file (its name does not end in _test.go).")
		return nil
	}
	file = g.refreshed(ctx, file)
	fn, found := scanTestFile(file.text).at(offsetLine(file.text, offsetOf(file.text, file.sel.Active)))
	if !found {
		g.api.Notifications().Info("No test function found at the cursor in " + base + ".")
		return nil
	}
	g.startDebugTest(filepath.Dir(file.path), fn.name)
	return nil
}

// debugTestFunctionCommand is the command the lenses run: {"uri": ..., "name": "TestAdd"}. A name that starts with Benchmark is debugged
// as a benchmark.
func (g *golang) debugTestFunctionCommand(_ context.Context, args any) error {
	m, _ := args.(map[string]any)
	uri, _ := m["uri"].(string)
	name, _ := m["name"].(string)
	path, isFile := filePath(uri)
	if !isFile || !strings.HasSuffix(path, "_test.go") || !identRe.MatchString(name) {
		return errors.New("DebugTestFunctionInvalid", "the command needs the uri of a _test.go file and the name of a function", "uri", uri, "name", name)
	}
	g.startDebugTest(filepath.Dir(path), name)
	return nil
}

// debugTestArgs are the arguments of the test binary that select the function: -test.run, or -test.bench with no test for a
// benchmark.
func debugTestArgs(name string) []any {
	if strings.HasPrefix(name, "Benchmark") {
		return []any{"-test.bench", "^" + name + "$", "-test.run", "a^"}
	}
	return []any{"-test.run", "^" + name + "$"}
}

// debugTestConfig is the configuration of a session that debugs the function of the package in dir: mode test, the environment of
// the tests (go.testEnvVars, go.testEnvFile), and go.testFlags, whose flags before -args go to the build and the ones after it to the
// test binary.
func (g *golang) debugTestConfig(dir, name string) extension.DebugConfiguration {
	args := debugTestArgs(name)
	var buildFlags []string
	afterArgs := false
	for _, f := range g.testFlags() {
		switch {
		case afterArgs:
			args = append(args, f)
		case f == "-args":
			afterArgs = true
		default:
			buildFlags = append(buildFlags, f)
		}
	}
	env := map[string]any{}
	if m, err := extension.SettingValue[map[string]any](g.api.Settings(), TestEnvVarsSetting); err == nil {
		for k, v := range m {
			env[k] = v
		}
	}
	cfg := extension.DebugConfiguration{
		"name": "Debug Test", "type": DebugType, "request": "launch", "mode": "test", "program": dir,
		"env": env, "args": args, "buildFlags": strings.Join(buildFlags, " "),
	}
	if file, _ := extension.SettingValue[string](g.api.Settings(), TestEnvFileSetting); strings.TrimSpace(file) != "" {
		file = strings.TrimSpace(file)
		if !filepath.IsAbs(file) && g.api.Scope().Folder != "" {
			file = filepath.Join(g.api.Scope().Folder, file)
		}
		cfg["envFile"] = file
	}
	return cfg
}

// startDebugTest starts the session on a goroutine of its own: the start waits for the providers and the adapter, which wait for the
// user and for dlv. A session that could not start is reported to the user, on the screen the command came from (the window of the host,
// or the view of a client of Remote Work: the session is the host's whoever started it).
func (g *golang) startDebugTest(dir, name string) {
	cfg := g.debugTestConfig(dir, name)
	folder := g.api.Scope().Folder
	origin := g.api.Origins().Current()
	g.closers.Add(1)
	go func() {
		defer g.closers.Done()
		_, err := g.api.Debug().StartDebugging(g.ctx, folder, extension.DebugTarget{Config: cfg}, extension.DebugStartOptions{})
		if err == nil {
			return
		}
		g.api.Log().Warn("the test cannot be debugged", "test", name, "err", err)
		g.api.Post(func() {
			g.api.Origins().Run(origin, func() { g.api.Notifications().Error("Debug " + name + ": " + describe(err)) })
		})
	}()
}
