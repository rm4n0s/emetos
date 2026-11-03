package javascript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

const jestTree = `src|math.test.js
  math @3
    adds @4
    divides @8
    later @12
    nested @14
      throws on zero @15
  prints and passes @28
src|other.test.js
  other passes @1
  other crashes @5
`

const vitestTree = `src|math.test.ts
  math @4
    adds @5
    divides @9
    later @13
    nested @15
      throws on zero @16
  prints and passes @29
src|other.test.ts
  other passes @3
  other crashes @7
`

const nodeTree = `test|math.test.js
  math @5
    adds @6
    divides @10
    later @14
    nested @16
      throws on zero @17
  prints and passes @23
test|other.test.js
  other passes @4
  other crashes @8
`

func TestDiscoveryMakesTheTreeOfFilesSuitesAndTestsForEachRunner(t *testing.T) {
	for _, tc := range []struct {
		project, runner, tree string
		calls                 []string
	}{
		{"jest", RunnerJest, jestTree, []string{"node " + "{dir}/node_modules/jest/bin/jest.js --listTests"}},
		{"vitest", RunnerVitest, vitestTree, []string{"node {dir}/node_modules/vitest/vitest.mjs list --filesOnly --json"}},
		{"node", RunnerNode, nodeTree, nil},
	} {
		t.Run(tc.runner, func(t *testing.T) {
			r := newJSRig(t, tc.project, rigOptions{})
			if len(r.api.TestControllers()) != 1 {
				t.Fatalf("%d controllers", len(r.api.TestControllers()))
			}
			if got := treeDump(r.ctl(tc.runner).Items()); got != tc.tree {
				t.Errorf("tree:\n%s\nwant:\n%s", got, tc.tree)
			}
			if r.ctl(tc.runner).Label() != runnerLabels[tc.runner] {
				t.Errorf("label %q", r.ctl(tc.runner).Label())
			}
			var want []string
			for _, c := range tc.calls {
				want = append(want, strings.ReplaceAll(c, "{dir}", r.folder))
			}
			if got := r.fr.called(); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("called %q, want %q", got, want)
			}
			d := treeFind(t, r.ctl(tc.runner).Items(), map[string]string{"jest": "src|math.test.js/math/divides", "vitest": "src|math.test.ts/math/divides", "node": "test|math.test.js/math/divides"}[tc.project]).Data()
			if !strings.HasPrefix(d.URI, "file://"+r.folder+"/") {
				t.Errorf("the test is in %s", d.URI)
			}
		})
	}
}

func TestProfilesOfAController(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	got := map[extension.TestRunKind]string{}
	for _, p := range r.ctl(RunnerJest).Profiles() {
		got[p.Kind()] = p.Label()
		if !p.IsDefault() || p.SupportsContinuousRun() != (p.Kind() == extension.TestKindRun) {
			t.Errorf("%s: default %v, continuous %v", p.Label(), p.IsDefault(), p.SupportsContinuousRun())
		}
	}
	want := map[extension.TestRunKind]string{extension.TestKindRun: "jest", extension.TestKindDebug: "jest (debug)", extension.TestKindCoverage: "jest (coverage)"}
	if len(got) != 3 || got[extension.TestKindRun] != want[extension.TestKindRun] || got[extension.TestKindDebug] != want[extension.TestKindDebug] || got[extension.TestKindCoverage] != want[extension.TestKindCoverage] {
		t.Errorf("profiles %v", got)
	}
	run := r.profile(RunnerJest, extension.TestKindRun)
	for _, tc := range []struct {
		uri  string
		want bool
	}{
		{"file://" + r.folder + "/src/math.js", true}, {"file://" + r.folder + "/src/x.test.ts", true}, {"file://" + r.folder + "/package.json", true},
		{"file://" + r.folder + "/README.md", false}, {"file:///elsewhere/a.js", false},
	} {
		if got, err := run.Triggers(nil, tc.uri); err != nil || got != tc.want {
			t.Errorf("%s triggers %v (%v), want %v", tc.uri, got, err, tc.want)
		}
	}
}

func TestDetectionOfRunners(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no package.json", map[string]string{"a.test.js": "x"}, ""},
		{"an empty package.json", map[string]string{"package.json": "{}"}, ""},
		{"a broken package.json", map[string]string{"package.json": "{"}, ""},
		{"jest in devDependencies", map[string]string{"package.json": `{"devDependencies":{"jest":"^30"}}`}, "jest"},
		{"jest in dependencies", map[string]string{"package.json": `{"dependencies":{"jest":"^30"}}`}, "jest"},
		{"jest key", map[string]string{"package.json": `{"jest":{"verbose":true}}`}, "jest"},
		{"jest config", map[string]string{"package.json": `{}`, "jest.config.ts": ""}, "jest"},
		{"vitest dependency", map[string]string{"package.json": `{"devDependencies":{"vitest":"^5"}}`}, "vitest"},
		{"vitest config", map[string]string{"package.json": `{}`, "vitest.config.mts": ""}, "vitest"},
		{"vitest workspace", map[string]string{"package.json": `{}`, "vitest.workspace.js": ""}, "vitest"},
		{"both", map[string]string{"package.json": `{"devDependencies":{"vitest":"^5","jest":"^30"}}`}, "jest vitest"},
		{"node --test script", map[string]string{"package.json": `{"scripts":{"test":"node --test"}}`}, "node-test"},
		{"node --test with arguments", map[string]string{"package.json": `{"scripts":{"test":"NODE_ENV=test node --import tsx --test test/*.ts"}}`}, "node-test"},
		{"a script that is not node's", map[string]string{"package.json": `{"scripts":{"test":"mocha --test"}}`}, ""},
		{"a file that imports node:test", map[string]string{"package.json": `{}`, "test/a.test.mjs": "import test from 'node:test';\n"}, "node-test"},
		{"a file that imports node:test elsewhere is not found", map[string]string{"package.json": `{}`, "src/a.mjs": "import test from 'node:test';\n"}, ""},
		{"node:test in node_modules", map[string]string{"package.json": `{}`, "node_modules/x/a.test.js": "node:test"}, ""},
		{"jest wins over a file that imports node:test", map[string]string{"package.json": `{"devDependencies":{"jest":"1"}}`, "test/a.test.js": "node:test"}, "jest"},
	} {
		dir := t.TempDir()
		for name, text := range tc.files {
			writeFile(t, dir, name, text, 0o644)
		}
		rs, _ := detectRunners(dir)
		var got []string
		for _, r := range runners {
			if rs[r] {
				got = append(got, r)
			}
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestProjectsOfASubFolderAreItemsOfTheirOwn(t *testing.T) {
	r := newJSRig(t, "", rigOptions{files: map[string]string{
		"pkg/package.json": `{"name":"pkg","devDependencies":{"jest":"1"}}`, "pkg/src/a.test.js": "test('a', () => {});\n",
		"other/package.json": `{"name":"other","devDependencies":{"jest":"1"}}`, "other/b.test.js": "test('b', () => {});\n",
		"notes/readme.md": "x",
	}})
	want := "other\n  b.test.js\n    b @1\npkg\n  src|a.test.js\n    a @1\n"
	if got := treeDump(r.ctl(RunnerJest).Items()); got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
}

func TestAFolderWithNoProjectHasNoControllers(t *testing.T) {
	r := newJSRig(t, "", rigOptions{files: map[string]string{"a.test.js": "test('a', () => {});\n"}})
	if n := len(r.api.TestControllers()); n != 0 || len(r.fr.called()) != 0 {
		t.Errorf("%d controllers, calls %q", n, r.fr.called())
	}
}

func TestAWindowWithNoTestingViewHasNoControllersAndNoWork(t *testing.T) {
	fr := newFakeRunner(t)
	base := t.TempDir()
	copyProjectTo(t, "jest", base)
	fakeTools(t, base)
	ext := New().(*javascript)
	ext.runner, ext.testDebounce = fr.run, 5*time.Millisecond
	api := newNoTestingAPI(t, ext, base)
	if err := ext.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ext.wait)
	if len(api.TestControllers()) != 0 || ext.testingOn {
		t.Errorf("controllers %d, testingOn %v", len(api.TestControllers()), ext.testingOn)
	}
	for _, c := range fr.called() {
		if strings.Contains(c, base) {
			t.Errorf("called %s", c)
		}
	}
}

func TestWhenTheRunnerCannotListItsFilesTheDefaultPatternsAreUsed(t *testing.T) {
	fr := newFakeRunner(t)
	fr.fail["--listTests"] = true
	rig := newRig(t, rigOptions{project: "jest", runner: fr.run})
	t.Cleanup(rig.ext.wait)
	jr := &jsRig{rig: rig, fr: fr}
	jr.settled()
	if got := treeDump(jr.ctl(RunnerJest).Items()); got != jestTree {
		t.Errorf("tree:\n%s", got)
	}
	if len(jr.api.Notes()) != 0 {
		t.Errorf("a quiet discovery told the user: %v", jr.api.Notes())
	}
	if err := jr.ctl(RunnerJest).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	jr.settled()
	if got := jr.note(); !strings.Contains(got, "cannot list its test files") {
		t.Errorf("note %q", got)
	}
}

func TestVitestsListIsReadFromItsJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  []string
	}{
		{"objects", []string{`[{"file":"/a.test.ts"},{"file":"/b.test.ts","name":"x"}]`}, []string{"/a.test.ts", "/b.test.ts"}},
		{"strings", []string{`["/a.test.ts"]`}, []string{"/a.test.ts"}},
		{"text around it", []string{"warning: something", `[`, `  {"file": "/a.test.ts"}`, `]`}, []string{"/a.test.ts"}},
		{"nothing", []string{"no tests"}, nil},
		{"broken", []string{`[{"file":`}, nil},
	} {
		got := vitestFileList(tc.lines)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}

func TestSavesAndFileChangesFindTheTestsAgain(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	save := func(rel string) {
		r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(filepath.Join(r.folder, rel))})
		time.Sleep(30 * time.Millisecond)
		r.settled()
	}
	changed := func(kind string, path string) {
		r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: kind, Path: path}}})
		time.Sleep(30 * time.Millisecond)
		r.settled()
	}
	for _, tc := range []struct {
		name string
		do   func()
		more bool
	}{
		{"a test file is saved", func() { save("src/math.test.js") }, true},
		{"a TypeScript file is saved", func() { save("src/x.ts") }, true},
		{"package.json is saved", func() { save("package.json") }, true},
		{"another file is saved", func() { save("README.md") }, false},
		{"a file outside the folder is saved", func() {
			r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: "file:///elsewhere/x.js"})
			time.Sleep(30 * time.Millisecond)
			r.settled()
		}, false},
		{"a source is created", func() { changed(extension.FileCreated, "src/new.js") }, true},
		{"a source in node_modules is created", func() { changed(extension.FileCreated, "node_modules/x/index.js") }, false},
		{"a source is deleted", func() { changed(extension.FileDeleted, "src/old.js") }, true},
		{"a source is changed", func() { changed(extension.FileChanged, "src/math.js") }, false},
		{"a text file is created", func() { changed(extension.FileCreated, "notes.txt") }, false},
		{"everything is rescanned", func() { changed(extension.FileRescan, "") }, true},
	} {
		before := len(r.fr.called())
		tc.do()
		if got := len(r.fr.called()) > before; got != tc.more {
			t.Errorf("%s: found again = %v, want %v", tc.name, got, tc.more)
		}
	}
}

func TestANewTestFileAppearsAfterItIsCreated(t *testing.T) {
	r := newJSRig(t, "node", rigOptions{})
	writeFile(t, r.folder, "test/third.test.js", "const { it } = require('node:test');\nit('is new', () => {});\n", 0o644)
	r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileCreated, Path: "test/third.test.js"}}})
	time.Sleep(30 * time.Millisecond)
	r.settled()
	if got := treeDump(r.ctl(RunnerNode).Items()); !strings.Contains(got, "test|third.test.js\n  is new @2\n") {
		t.Errorf("tree:\n%s", got)
	}
}

func TestAControllerIsMadeWhenAProjectStartsUsingARunner(t *testing.T) {
	r := newJSRig(t, "", rigOptions{files: map[string]string{"package.json": "{}"}})
	if len(r.api.TestControllers()) != 0 {
		t.Fatal("a controller before the runner is used")
	}
	copyProjectTo(t, "vitest", r.folder)
	fakeTools(t, r.folder)
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(filepath.Join(r.folder, "package.json"))})
	r.api.WaitFor("the controller", func() bool { return len(r.api.TestControllers()) == 1 })
	time.Sleep(30 * time.Millisecond)
	r.settled()
	if got := treeDump(r.ctl(RunnerVitest).Items()); got != vitestTree {
		t.Errorf("tree:\n%s", got)
	}
}

func TestResolveAndRefreshFindTheTests(t *testing.T) {
	r := newJSRig(t, "node", rigOptions{})
	c := r.ctl(RunnerNode)
	if err := os.Remove(filepath.Join(r.folder, "test/other.test.js")); err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := treeDump(c.Items()); strings.Contains(got, "other") || !strings.Contains(got, "math") {
		t.Errorf("tree after Refresh:\n%s", got)
	}
	writeFile(t, r.folder, "test/other.test.js", "test('back', () => {});\n", 0o644)
	if err := c.Resolve(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := treeDump(c.Items()); !strings.Contains(got, "back @1") {
		t.Errorf("tree after Resolve:\n%s", got)
	}
}
