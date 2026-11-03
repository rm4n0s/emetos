package javascript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Debug (step 26.8): the configuration of js-debug that a request makes.

func TestDebugStartsJsDebugOnTheTestFiles(t *testing.T) {
	for _, tc := range []struct {
		name, project, runner string
		include               []string
		program               string
		runtimeArgs, args     []any
	}{
		{"node, a file", "node", RunnerNode, []string{"test|math.test.js"}, "test/math.test.js", nil, nil},
		{"node, a test", "node", RunnerNode, []string{"test|math.test.js/math/adds"}, "test/math.test.js", []any{"--test-name-pattern=^math adds$"}, nil},
		{"node, a suite and a test", "node", RunnerNode, []string{"test|math.test.js/math/nested", "test|math.test.js/prints and passes"}, "test/math.test.js",
			[]any{"--test-name-pattern=^math nested( |$)", "--test-name-pattern=^prints and passes$"}, nil},
		{"node, two files debug the first", "node", RunnerNode, []string{"test|math.test.js", "test|other.test.js"}, "test/math.test.js", nil, nil},
		{"jest, everything", "jest", RunnerJest, nil, "node_modules/jest/bin/jest.js", nil,
			[]any{"--runInBand", "--testTimeout=3600000", "--runTestsByPath", "src/math.test.js", "src/other.test.js"}},
		{"jest, a test", "jest", RunnerJest, []string{"src|math.test.js/math/divides"}, "node_modules/jest/bin/jest.js", nil,
			[]any{"--runInBand", "--testTimeout=3600000", "-t", "(^math divides$)", "--runTestsByPath", "src/math.test.js"}},
		{"vitest, a file", "vitest", RunnerVitest, []string{"src|other.test.ts"}, "node_modules/vitest/vitest.mjs", nil,
			[]any{"run", "--pool=forks", "--no-file-parallelism", "--testTimeout=3600000", "--hookTimeout=3600000", "src/other.test.ts"}},
		{"vitest, a suite", "vitest", RunnerVitest, []string{"src|math.test.ts/math/nested"}, "node_modules/vitest/vitest.mjs", nil,
			[]any{"run", "--pool=forks", "--no-file-parallelism", "--testTimeout=3600000", "--hookTimeout=3600000", "-t", "(^math > nested( > |$))", "src/math.test.ts"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newJSRig(t, tc.project, rigOptions{})
			cfgs := r.debugOf(tc.runner, r.items(tc.runner, tc.include...), nil)
			if len(cfgs) != 1 {
				t.Fatalf("%d sessions started", len(cfgs))
			}
			c := cfgs[0]
			if c["type"] != "node" || c["request"] != "launch" || c["cwd"] != r.folder || c["program"] != filepath.Join(r.folder, tc.program) {
				t.Errorf("configuration %+v", c)
			}
			if !reflect.DeepEqual(c["runtimeArgs"], tc.runtimeArgs) && (tc.runtimeArgs != nil || c["runtimeArgs"] != nil) {
				t.Errorf("runtimeArgs %v, want %v", c["runtimeArgs"], tc.runtimeArgs)
			}
			if tc.args != nil && !reflect.DeepEqual(c["args"], tc.args) {
				t.Errorf("args %v, want %v", c["args"], tc.args)
			}
			if skip, _ := c["skipFiles"].([]any); len(skip) == 0 {
				t.Errorf("skipFiles %v", c["skipFiles"])
			}
		})
	}
}

func TestDebugOfTwoNodeFilesTellsTheUserWhichOne(t *testing.T) {
	r := newJSRig(t, "node", rigOptions{})
	r.debugOf(RunnerNode, r.items(RunnerNode, "test|math.test.js", "test|other.test.js"), nil)
	if !r.noteContaining("A debug session runs one test file; debugging test/math.test.js.") {
		t.Errorf("notes %v", r.noteList())
	}
}

func TestDebugOfNothingTellsTheUser(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	for _, it := range r.ctl(RunnerJest).Items().All() {
		r.ctl(RunnerJest).Items().Delete(it.ID())
	}
	if cfgs := r.debugOf(RunnerJest, nil, nil); len(cfgs) != 0 || !r.noteContaining("There are no Jest tests to debug.") {
		t.Errorf("%d sessions, notes %v", len(cfgs), r.noteList())
	}
}

func TestDebugWithoutTheToolTellsTheUser(t *testing.T) {
	r := newJSRig(t, "vitest", rigOptions{})
	if err := os.RemoveAll(filepath.Join(r.folder, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if cfgs := r.debugOf(RunnerVitest, nil, nil); len(cfgs) != 0 || !r.noteContaining("Debug Vitest tests:") {
		t.Errorf("%d sessions, notes %v", len(cfgs), r.noteList())
	}
}

func TestDebugOfProjectsStartsTheFirst(t *testing.T) {
	r := newJSRig(t, "", rigOptions{files: map[string]string{
		"a/package.json": `{"devDependencies":{"jest":"1"}}`, "a/a.test.js": "test('a', () => {});\n",
		"b/package.json": `{"devDependencies":{"jest":"1"}}`, "b/b.test.js": "test('b', () => {});\n",
	}})
	for _, p := range []string{"a", "b"} {
		fakeTools(t, filepath.Join(r.folder, p))
	}
	cfgs := r.debugOf(RunnerJest, nil, nil)
	if len(cfgs) != 1 || cfgs[0]["cwd"] != filepath.Join(r.folder, "a") || !r.noteContaining("one project") {
		t.Errorf("configurations %v, notes %v", cfgs, r.noteList())
	}
}
