package javascript

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Which runners a folder uses and which files are tests (SPEC §10.32 "The providers", JavaScript and TypeScript; "As built (step 26.8)"):
// a project is a folder with a package.json; it uses Jest or Vitest when its dependencies or its config files say so, and Node's own test
// runner when its test script runs `node --test` or one of its files imports node:test.

// The ids of the three runners (and of their controllers).
const (
	RunnerNode   = "node-test"
	RunnerJest   = "jest"
	RunnerVitest = "vitest"
)

// runners lists them in the order their controllers are made.
var runners = []string{RunnerNode, RunnerJest, RunnerVitest}

// runnerLabels are the names of the controllers.
var runnerLabels = map[string]string{RunnerNode: "Node.js Test Runner", RunnerJest: "Jest", RunnerVitest: "Vitest"}

// jsProject is a folder with a package.json and the runners it uses.
type jsProject struct {
	dir     string
	name    string
	runners map[string]bool
}

// maxWalkEntries bounds the files looked at in a folder, so that a huge repository does not stall the start.
const maxWalkEntries = 20000

// skipDirs are the folders no test is looked for in.
func skipDir(name string) bool {
	return name == "node_modules" || name == "coverage" || strings.HasPrefix(name, ".")
}

// testPackage is the part of a package.json the detection reads.
type testPackage struct {
	Name            string            `json:"name"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Jest            json.RawMessage   `json:"jest"`
}

func readTestPackage(dir string) (testPackage, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return testPackage{}, false
	}
	var p testPackage
	if json.Unmarshal(data, &p) != nil {
		return testPackage{}, true
	}
	return p, true
}

func (p testPackage) hasDep(name string) bool {
	_, a := p.Dependencies[name]
	_, b := p.DevDependencies[name]
	return a || b
}

var nodeTestScript = regexp.MustCompile(`(^|[\s;&|(])(node|tsx|ts-node|nodemon)\b[^;&|]*\s--test(\s|$)`)

// hasConfig tells whether the folder has a file named base with one of the extensions.
func hasConfig(dir, base string, exts ...string) bool {
	for _, e := range exts {
		if _, err := os.Stat(filepath.Join(dir, base+e)); err == nil {
			return true
		}
	}
	return false
}

// detectRunners says which runners the project in dir uses. A folder with no package.json uses none.
func detectRunners(dir string) (map[string]bool, string) {
	pkg, ok := readTestPackage(dir)
	if !ok {
		return nil, ""
	}
	out := map[string]bool{}
	script := strings.Join(mapValues(pkg.Scripts), "\n")
	jsExts := []string{".js", ".cjs", ".mjs", ".ts", ".cts", ".mts", ".json"}
	if pkg.hasDep("jest") || len(pkg.Jest) > 0 || hasConfig(dir, "jest.config", jsExts...) {
		out[RunnerJest] = true
	}
	if pkg.hasDep("vitest") || hasConfig(dir, "vitest.config", jsExts...) || hasConfig(dir, "vitest.workspace", jsExts...) {
		out[RunnerVitest] = true
	}
	if nodeTestScript.MatchString(script) || (len(out) == 0 && usesNodeTest(dir)) {
		out[RunnerNode] = true
	}
	return out, pkg.Name
}

func mapValues(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = m[k]
	}
	return out
}

// usesNodeTest tells whether a file of the folder that Node's runner would pick imports node:test.
func usesNodeTest(dir string) bool {
	for _, f := range walkFiles(dir, matchNode) {
		if data := readHead(f, 8192); strings.Contains(data, "node:test") {
			return true
		}
	}
	return false
}

// readHead reads at most n bytes of a file, none when it cannot be read.
func readHead(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	got, _ := f.Read(buf)
	return string(buf[:got])
}

// detectProjects finds the projects below the folder: the folder itself when it is one that uses a runner, else its sub-folders (one level,
// and the folders of a "workspaces" layout such as packages/x are not looked into) that are.
func detectProjects(folder string) []jsProject {
	if folder == "" {
		return nil
	}
	if rs, name := detectRunners(folder); len(rs) > 0 {
		return []jsProject{{dir: folder, name: name, runners: rs}}
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}
	var out []jsProject
	for _, e := range entries {
		if !e.IsDir() || skipDir(e.Name()) {
			continue
		}
		dir := filepath.Join(folder, e.Name())
		if rs, name := detectRunners(dir); len(rs) > 0 {
			out = append(out, jsProject{dir: dir, name: name, runners: rs})
		}
	}
	return out
}

// ---- the files ----

var (
	jestTestRe   = regexp.MustCompile(`\.(spec|test)\.[jt]sx?$`)
	vitestTestRe = regexp.MustCompile(`\.(spec|test)\.[cm]?[jt]sx?$`)
	nodeTestRe   = regexp.MustCompile(`(\.test|[-_]test)\.[cm]?[jt]s$|(^|/)test-[^/]*\.[cm]?[jt]s$|(^|/)test\.[cm]?[jt]s$|(^|/)test/.*\.[cm]?[jt]s$`)
)

// matchJest tells whether the path (relative, with slashes) is a file Jest runs by default (its testMatch).
func matchJest(rel string) bool {
	return jestTestRe.MatchString(rel) || (strings.Contains("/"+rel, "/__tests__/") && regexp.MustCompile(`\.[jt]sx?$`).MatchString(rel))
}

// matchVitest tells whether the path is a file Vitest runs by default (its include).
func matchVitest(rel string) bool { return vitestTestRe.MatchString(rel) }

// matchNode tells whether the path is a file `node --test` runs when it is given none (Node's patterns).
func matchNode(rel string) bool { return nodeTestRe.MatchString(rel) }

// matchers are the default patterns of the runners.
var matchers = map[string]func(string) bool{RunnerNode: matchNode, RunnerJest: matchJest, RunnerVitest: matchVitest}

// walkFiles lists the files below dir (absolute paths, sorted) whose relative path the match accepts, skipping node_modules and the
// folders that start with a dot; at most maxWalkEntries files are looked at.
func walkFiles(dir string, match func(rel string) bool) []string {
	var out []string
	seen := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // a folder that cannot be read has no tests we can find
		}
		if d.IsDir() {
			if path != dir && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if seen++; seen > maxWalkEntries {
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(dir, path)
		if err == nil && match(filepath.ToSlash(rel)) {
			out = append(out, path)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// isTestSource tells whether a path is a JavaScript or TypeScript file or a package.json or config file of a runner: what a change of
// finds the tests again.
func isTestSource(path string) bool {
	base := filepath.Base(path)
	switch filepath.Ext(base) {
	case ".js", ".cjs", ".mjs", ".jsx", ".ts", ".cts", ".mts", ".tsx":
		return true
	}
	return base == "package.json"
}

// ---- the programs ----

// findTool finds the program of a runner for a project: node_modules/.bin/<name> of the project or of a folder above it, else the first
// <name> on the PATH. When the link resolves to a JavaScript file, script is that file (to be run by node), else program is the file to
// run.
func findTool(dir, name, pathEnv string) (script, program string, ok bool) {
	for d := dir; ; d = filepath.Dir(d) {
		bin := filepath.Join(d, "node_modules", ".bin", name)
		if _, err := os.Stat(bin); err == nil {
			return resolveTool(bin)
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	for _, p := range filepath.SplitList(pathEnv) {
		if p == "" {
			continue
		}
		bin := filepath.Join(p, name)
		if info, err := os.Stat(bin); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return resolveTool(bin)
		}
	}
	return "", "", false
}

// resolveTool follows a link of .bin to the JavaScript file it names.
func resolveTool(bin string) (script, program string, ok bool) {
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return "", "", false
	}
	switch filepath.Ext(real) {
	case ".js", ".cjs", ".mjs":
		return real, "", true
	}
	return "", real, true
}
