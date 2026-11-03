package javascript

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Debugging tests (SPEC §10.32 "The providers", JavaScript and TypeScript; "As built (step 26.8)"): the tests are started by js-debug
// (the debuggers node and pwa-node of step 25.10) with the configuration a user would write: the test file itself for Node's runner (a
// file that imports node:test runs its tests when node runs it), and the script of Jest or Vitest with one process for all the files.

// debugTimeout is the timeout given to Jest and Vitest while debugging, so that a test stopped at a breakpoint does not fail; it is an
// hour, in milliseconds.
const debugTimeout = "3600000"

// debugRequest starts the debugger on the tests the request selects. It makes no result (the session is the debugger's).
func (tc *testCtl) debugRequest(_ context.Context, req *extension.TestRunRequest) {
	j := tc.j
	sels := tc.plan(req)
	if len(sels) == 0 {
		j.api.Notifications().Info("There are no " + runnerLabels[tc.runner] + " tests to debug.")
		return
	}
	project := sels[0].project
	var group []*fileSel
	for _, s := range sels {
		if s.project == project {
			group = append(group, s)
		}
	}
	if tc.runner == RunnerNode && len(group) > 1 {
		j.api.Notifications().Info("A debug session runs one test file; debugging " + group[0].rel + ".")
		group = group[:1]
	} else if len(group) < len(sels) {
		j.api.Notifications().Info("A debug session debugs the tests of one project; debugging those of " + filepath.Base(project) + ".")
	}
	cfg, err := tc.debugConfiguration(project, group)
	if err != nil {
		j.api.Log().Warn("the tests cannot be debugged", "runner", tc.runner, "err", err)
		j.api.Notifications().Error("Debug " + runnerLabels[tc.runner] + " tests: " + describe(err))
		return
	}
	j.api.Post(func() { j.startDebugConfig(cfg, "the "+runnerLabels[tc.runner]+" tests") })
}

// debugConfiguration is the launch configuration that debugs the files of a project.
func (tc *testCtl) debugConfiguration(project string, group []*fileSel) (extension.DebugConfiguration, error) {
	patterns := tc.namePatterns(group)
	cfg := extension.DebugConfiguration{
		"name": "Debug tests", "type": DebugType, "request": "launch", "cwd": project, "console": "internalConsole",
		"internalConsoleOptions": "neverOpen", "skipFiles": []any{"<node_internals>/**", "**/node_modules/**"},
	}
	if tc.runner == RunnerNode {
		s := group[0]
		cfg["program"] = s.abs
		var rt []any
		for _, p := range patterns {
			rt = append(rt, "--test-name-pattern="+p)
		}
		if rt != nil {
			cfg["runtimeArgs"] = rt
		}
		return cfg, nil
	}
	script, program, ok := findTool(project, tc.runner, os.Getenv("PATH"))
	if !ok {
		return nil, errors.New("JsTestToolNotFound", tc.runner+" was not found in node_modules/.bin of the project or in PATH", "tool", tc.runner, "project", project)
	}
	var args []string
	if tc.runner == RunnerJest {
		args = []string{"--runInBand", "--testTimeout=" + debugTimeout}
	} else {
		args = []string{"run", "--pool=forks", "--no-file-parallelism", "--testTimeout=" + debugTimeout, "--hookTimeout=" + debugTimeout}
	}
	if len(patterns) > 0 && anyPartial(group) {
		args = append(args, "-t", "("+strings.Join(patterns, "|")+")")
	}
	if tc.runner == RunnerJest {
		args = append(args, "--runTestsByPath")
	}
	for _, s := range group {
		args = append(args, s.rel)
	}
	if script != "" {
		cfg["program"] = script
	} else {
		cfg["runtimeExecutable"] = program
	}
	cfg["args"] = stringsToAny(args)
	return cfg, nil
}

func stringsToAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// startDebugConfig starts the session of the configuration; what names it for the message of a failure.
func (j *javascript) startDebugConfig(cfg extension.DebugConfiguration, what string) {
	folder := j.api.Scope().Folder
	origin := j.api.Origins().Current()
	j.work.Add(1)
	go func() {
		defer j.work.Done()
		_, err := j.api.Debug().StartDebugging(j.ctx, folder, extension.DebugTarget{Config: cfg}, extension.DebugStartOptions{})
		if err == nil {
			return
		}
		j.api.Log().Warn("the tests cannot be debugged", "tests", what, "err", err)
		j.api.Post(func() {
			j.api.Origins().Run(origin, func() { j.api.Notifications().Error("Debug " + what + ": " + describe(err)) })
		})
	}()
}

// vitestFileList reads the output of `vitest list --filesOnly --json`: a JSON array of the files, each a path or an object with a "file".
func vitestFileList(lines []string) []string {
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end < start {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal([]byte(text[start:end+1]), &items) != nil {
		return nil
	}
	var out []string
	for _, raw := range items {
		var s string
		var o struct {
			File string `json:"file"`
		}
		switch {
		case json.Unmarshal(raw, &s) == nil && s != "":
			out = append(out, s)
		case json.Unmarshal(raw, &o) == nil && o.File != "":
			out = append(out, o.File)
		}
	}
	return out
}
