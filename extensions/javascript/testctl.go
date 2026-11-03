package javascript

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The controllers of JavaScript's and TypeScript's tests (SPEC §10.32 "The providers", JavaScript and TypeScript; "As built (step 26.8)"):
// one for each runner a project of the folder uses (`node-test`, `jest`, `vitest`), each with a tree of projects (only when the folder is not
// itself the project), the test files, the suites and the tests that the Testing view shows. The files come from the runner (`jest
// --listTests`, `vitest list --filesOnly --json`) or, when it cannot say, from its default patterns; the suites and tests from a light scan of
// the files (testscan.go), and the ones that only a run reveals (`test.each`) are added when it reports them. The tests are found when the
// pack starts in a folder with a project (the Testing view never asks a controller for them), on Refresh Tests and after a JavaScript or
// TypeScript file or a package.json is saved, created, deleted or renamed.

// The names of the run profiles of a controller: the runner's name, and the name with "(debug)" or "(coverage)".
const (
	debugSuffix    = " (debug)"
	coverageSuffix = " (coverage)"
)

// The roles of an item of the tree.
const (
	roleProject = iota
	roleFile
	roleSuite
	roleTest
)

// discoveryTimeout ends a discovery that does not finish; saveDebounce is how long the tests wait for more saves and file events before
// they are found again.
const (
	discoveryTimeout = 5 * time.Minute
	saveDebounce     = 400 * time.Millisecond
)

// maxTestFiles bounds the files a controller reads, and maxTestFileBytes the size of a file that is scanned.
const (
	maxTestFiles     = 3000
	maxTestFileBytes = 2 << 20
)

// testItemInfo says what an item of the tree is.
type testItemInfo struct {
	role int
	// project is the folder of the project, file the path of the file relative to it (with slashes) and path the titles of the suites and
	// the test from the file down to the item.
	project, file string
	path          []string
}

// testCtl is the controller of one runner and the state behind its items. Everything in it is safe for concurrent use.
type testCtl struct {
	j        *javascript
	runner   string
	ctl      extension.TestController
	profiles map[extension.TestRunKind]extension.TestRunProfile

	mu   sync.Mutex
	info map[*extension.TestItem]testItemInfo
	byID map[string]*extension.TestItem
	// byRun finds the item of a test a run reported, by its project, file and titles; files finds the item of a file.
	byRun map[string]*extension.TestItem
	files map[string]*extension.TestItem

	// discMu serializes discoveries; the rest is the queue of the worker.
	discMu  sync.Mutex
	wmu     sync.Mutex
	pending bool
	loud    bool
	working bool
	timer   *time.Timer
}

// newTestCtl makes the controller of the runner and its profiles. It fails with ExtensionTestingUnavailable in a scope with no testing
// service.
func newTestCtl(j *javascript, runner string) (*testCtl, error) {
	ctl, err := j.api.Tests().CreateTestController(runner, runnerLabels[runner])
	if err != nil {
		return nil, err
	}
	tc := &testCtl{
		j: j, runner: runner, ctl: ctl, profiles: map[extension.TestRunKind]extension.TestRunProfile{}, info: map[*extension.TestItem]testItemInfo{},
		byID: map[string]*extension.TestItem{}, byRun: map[string]*extension.TestItem{}, files: map[string]*extension.TestItem{},
	}
	for _, pr := range []struct {
		label string
		kind  extension.TestRunKind
	}{{runner, extension.TestKindRun}, {runner + debugSuffix, extension.TestKindDebug}, {runner + coverageSuffix, extension.TestKindCoverage}} {
		opts := extension.TestRunProfileOptions{IsDefault: true, SupportsContinuousRun: pr.kind == extension.TestKindRun, ContinuousTrigger: tc.continuousTrigger}
		if pr.kind == extension.TestKindCoverage {
			opts.LoadDetailedCoverage = loadJSCoverage
		}
		prof, err := ctl.CreateRunProfile(pr.label, pr.kind, tc.handler(pr.kind), opts)
		if err != nil {
			return nil, err
		}
		tc.profiles[pr.kind] = prof
	}
	ctl.SetResolveHandler(func(ctx context.Context, item *extension.TestItem) error {
		if item == nil {
			tc.discover(ctx, false)
		}
		return nil
	})
	ctl.SetRefreshHandler(func(ctx context.Context) error {
		tc.discover(ctx, true)
		return nil
	})
	return tc, nil
}

// continuousTrigger says that a change of a JavaScript or TypeScript file of the folder runs the continuous run again.
func (tc *testCtl) continuousTrigger(_ *extension.TestItem, uri string) bool {
	path, ok := filePath(uri)
	return ok && isTestSource(path) && within(tc.j.api.Scope().Folder, path)
}

// ---- paths ----

// fileURI is the file: URI of a path.
func fileURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// runKey is the key of a test in byRun.
func runKey(project, file string, path []string) string {
	return project + "\x00" + file + "\x00" + strings.Join(path, "\x1f")
}

// fileKey is the key of a file in files.
func fileKey(project, file string) string { return project + "\x00" + file }

// ---- discovery ----

// requestDiscovery asks for the tests to be found again soon; requests that come close together make one discovery. It may be called
// from any goroutine.
func (tc *testCtl) requestDiscovery(loud bool) {
	tc.wmu.Lock()
	tc.pending = true
	tc.loud = tc.loud || loud
	if tc.timer != nil {
		tc.timer.Stop()
	}
	tc.timer = time.AfterFunc(tc.j.testDebounce, tc.startWorker)
	tc.wmu.Unlock()
}

// startWorker starts the worker that finds the tests while there are requests.
func (tc *testCtl) startWorker() {
	tc.wmu.Lock()
	if tc.working || !tc.pending {
		tc.wmu.Unlock()
		return
	}
	tc.working = true
	tc.wmu.Unlock()
	j := tc.j
	j.work.Add(1)
	go func() {
		defer j.work.Done()
		for {
			tc.wmu.Lock()
			if !tc.pending || j.ctx.Err() != nil {
				tc.pending, tc.working = false, false
				tc.wmu.Unlock()
				return
			}
			loud := tc.loud
			tc.pending, tc.loud = false, false
			tc.wmu.Unlock()
			tc.discover(j.ctx, loud)
		}
	}()
}

// idle tells whether nothing is being found and nothing is waiting to be (the tests wait for it).
func (tc *testCtl) idle() bool {
	tc.wmu.Lock()
	busy := tc.working || tc.pending
	tc.wmu.Unlock()
	if busy || !tc.discMu.TryLock() {
		return false
	}
	tc.discMu.Unlock()
	return true
}

// stop ends the timer of the worker (the end of the activation).
func (tc *testCtl) stop() {
	tc.wmu.Lock()
	defer tc.wmu.Unlock()
	if tc.timer != nil {
		tc.timer.Stop()
	}
}

// foundFile is a test file of a project with the suites and tests the scan found in it.
type foundFile struct {
	project jsProject
	abs     string
	rel     string
	nodes   []*scanNode
}

// discover finds the test files of the projects that use the runner and scans them, then makes the tree what it found. With loud a failure
// is told to the user.
func (tc *testCtl) discover(ctx context.Context, loud bool) {
	tc.discMu.Lock()
	defer tc.discMu.Unlock()
	j := tc.j
	folder := j.api.Scope().Folder
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	var projects []jsProject
	for _, p := range detectProjects(folder) {
		if p.runners[tc.runner] {
			projects = append(projects, p)
		}
	}
	var found []foundFile
	for _, p := range projects {
		if ctx.Err() != nil {
			return
		}
		files, err := tc.listFiles(ctx, p)
		if err != nil {
			j.api.Log().Debug("the test files cannot be listed by the runner; its default patterns are used", "runner", tc.runner, "project", p.dir, "err", err)
			if loud {
				j.api.Notifications().Warn(runnerLabels[tc.runner] + " cannot list its test files (" + describe(err) + "); the default file patterns are used.")
			}
		}
		for _, f := range files {
			if len(found) >= maxTestFiles {
				break
			}
			rel, err := filepath.Rel(p.dir, f)
			if err != nil {
				continue
			}
			ff := foundFile{project: p, abs: f, rel: filepath.ToSlash(rel)}
			if info, err := os.Stat(f); err == nil && info.Size() <= maxTestFileBytes {
				if data, err := os.ReadFile(f); err == nil {
					ff.nodes = scanTests(string(data))
				}
			}
			found = append(found, ff)
		}
	}
	tc.apply(folder, projects, found)
}

// listFiles lists the test files of the project: the runner's own list when it can give one, else the files that match its default
// patterns (and the reason, when the runner was tried and failed).
func (tc *testCtl) listFiles(ctx context.Context, p jsProject) ([]string, error) {
	j := tc.j
	var files []string
	var err error
	switch tc.runner {
	case RunnerJest:
		files, err = j.listWithRunner(ctx, p, "jest", []string{"--listTests"}, nil)
	case RunnerVitest:
		files, err = j.listWithRunner(ctx, p, "vitest", []string{"list", "--filesOnly", "--json"}, vitestFileList)
	}
	if len(files) == 0 {
		files = walkFiles(p.dir, matchers[tc.runner])
	}
	files = slices.DeleteFunc(files, func(f string) bool {
		return !within(p.dir, f) || strings.Contains(f, string(filepath.Separator)+"node_modules"+string(filepath.Separator))
	})
	slices.Sort(files)
	return slices.Compact(files), err
}

// ---- the tree ----

// apply makes the tree what the discovery found: items that are still there are kept (with their results), the others go.
func (tc *testCtl) apply(folder string, projects []jsProject, found []foundFile) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	seen := map[string]bool{}
	byRun := map[string]*extension.TestItem{}
	files := map[string]*extension.TestItem{}
	layered := len(projects) > 1 || (len(projects) == 1 && projects[0].dir != folder)
	projItems := map[string]*extension.TestItem{}
	for _, ff := range found {
		root := tc.ctl.Items()
		if layered {
			pi := projItems[ff.project.dir]
			if pi == nil {
				rel, err := filepath.Rel(folder, ff.project.dir)
				if err != nil || rel == "" {
					rel = "."
				}
				label := rel
				if ff.project.name != "" && rel != "." {
					label = filepath.ToSlash(rel)
				}
				pi = tc.ensure(root, tc.runner+":"+filepath.ToSlash(rel), label, filepath.Join(ff.project.dir, "package.json"), 0, testItemInfo{role: roleProject, project: ff.project.dir}, seen, "")
				projItems[ff.project.dir] = pi
			}
			root = pi.Children()
		}
		fid := tc.runner + ":" + ff.project.dir + "|" + ff.rel
		fi := tc.ensure(root, fid, ff.rel, ff.abs, 0, testItemInfo{role: roleFile, project: ff.project.dir, file: ff.rel}, seen, "")
		files[fileKey(ff.project.dir, ff.rel)] = fi
		tc.addNodes(fi, fid, ff, nil, ff.nodes, byRun, seen)
	}
	tc.byRun, tc.files = byRun, files
	for id, it := range tc.byID {
		if seen[id] {
			continue
		}
		if parent := it.Parent(); parent != nil {
			parent.Children().Delete(id)
		} else {
			tc.ctl.Items().Delete(id)
		}
		delete(tc.byID, id)
		delete(tc.info, it)
	}
}

// addNodes puts the suites and tests of a file in the tree below parent; path is the titles above them. tc.mu is held.
func (tc *testCtl) addNodes(parent *extension.TestItem, fid string, ff foundFile, path []string, nodes []*scanNode, byRun map[string]*extension.TestItem, seen map[string]bool) {
	for _, n := range nodes {
		if n.Each {
			continue // the names of a template are known when it runs
		}
		p := append(slices.Clone(path), n.Name)
		role, tag := roleTest, "#t:"
		if n.Suite {
			role, tag = roleSuite, "#s:"
		}
		id := fid + tag + strings.Join(p, " > ")
		for dup := 2; seen[id]; dup++ {
			id = fid + tag + strings.Join(p, " > ") + "~" + strconv.Itoa(dup)
		}
		it := tc.ensure(parent.Children(), id, n.Name, ff.abs, n.Line, testItemInfo{role: role, project: ff.project.dir, file: ff.rel, path: p}, seen, "")
		if key := runKey(ff.project.dir, ff.rel, p); byRun[key] == nil {
			byRun[key] = it
		}
		if n.Suite {
			tc.addNodes(it, fid, ff, p, n.Children, byRun, seen)
		}
	}
}

// ensure returns the item with the id, made when it is new, in the collection, with the label and the place (line from 1, 0 when
// unknown). tc.mu is held.
func (tc *testCtl) ensure(coll *extension.TestItemCollection, id, label, file string, line int, info testItemInfo, seen map[string]bool, errText string) *extension.TestItem {
	it := tc.byID[id]
	if it == nil {
		it = tc.ctl.CreateTestItem(id, label, fileURI(file))
		tc.byID[id] = it
	}
	tc.info[it] = info
	seen[id] = true
	var rng *extension.Range
	if line > 0 {
		rng = &extension.Range{Start: extension.Position{Line: line - 1}, End: extension.Position{Line: line - 1}}
	}
	d := it.Data()
	if d.Label != label || d.Error != errText || d.URI != fileURI(file) || (rng == nil) != (d.Range == nil) || (rng != nil && *rng != *d.Range) {
		it.Update(func(d *extension.TestItemData) { d.Label, d.Error, d.Range, d.URI = label, errText, rng, fileURI(file) })
	}
	if err := coll.Add(it); err != nil {
		tc.j.api.Log().Warn("a test cannot be added to the tree", "id", id, "err", err)
	}
	return it
}

// itemForRun is the item of the test a run reported by its titles, nil when the tree has none.
func (tc *testCtl) itemForRun(project, file string, path []string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.byRun[runKey(project, file, path)]
}

// addReported puts a test that only a run knows (the names a `test.each` made) in the tree below its suites and its file, and returns its
// item; nil when the file is not in the tree. line is the place the runner gave (from 1, 0 when it gave none).
func (tc *testCtl) addReported(project, file string, path []string, line int) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if it := tc.byRun[runKey(project, file, path)]; it != nil {
		return it
	}
	fi := tc.files[fileKey(project, file)]
	if fi == nil || len(path) == 0 {
		return nil
	}
	fid := fi.ID()
	abs := filepath.Join(project, filepath.FromSlash(file))
	parent := fi
	seen := map[string]bool{}
	for i := range path {
		p := path[:i+1]
		last := i == len(path)-1
		if it := tc.byRun[runKey(project, file, p)]; it != nil {
			parent = it
			continue
		}
		role, tag, ln := roleSuite, "#s:", 0
		if last {
			role, tag, ln = roleTest, "#t:", line
		}
		id := fid + tag + strings.Join(p, " > ")
		it := tc.ensure(parent.Children(), id, path[i], abs, ln, testItemInfo{role: role, project: project, file: file, path: slices.Clone(p)}, seen, "")
		tc.byRun[runKey(project, file, p)] = it
		parent = it
	}
	return parent
}

// ---- files changing ----

// documentSaved finds the tests again when the saved file is a source of tests or a package.json.
func (j *javascript) documentSaved(ev extension.Event) {
	var saved extension.DocumentSaved
	if ev.Decode(&saved) != nil {
		return
	}
	if path, ok := filePath(saved.URI); ok && isTestSource(path) && within(j.api.Scope().Folder, path) {
		j.testsChanged()
	}
}

// filesChanged finds the tests again when a source of tests is created, deleted or moved.
func (j *javascript) filesChanged(ev extension.Event) {
	var fc extension.FilesChanged
	if ev.Decode(&fc) != nil {
		return
	}
	for _, c := range fc.Changes {
		switch c.Kind {
		case extension.FileCreated, extension.FileDeleted, extension.FileRenamed, extension.FileRescan:
		default:
			continue
		}
		if c.Kind == extension.FileRescan || (isTestSource(c.Path) || isTestSource(c.OldPath)) && !inNodeModules(c.Path) {
			j.testsChanged()
			return
		}
	}
}

// testsChanged makes the controllers of the runners the folder now uses and finds the tests again. UI thread.
func (j *javascript) testsChanged() {
	if !j.testingOn {
		return
	}
	j.makeControllers()
	for _, tc := range j.tests {
		tc.requestDiscovery(false)
	}
}

// makeControllers makes the controllers of the runners the projects of the folder use that have none yet. UI thread.
func (j *javascript) makeControllers() {
	used := map[string]bool{}
	for _, p := range detectProjects(j.api.Scope().Folder) {
		for r := range p.runners {
			used[r] = true
		}
	}
	for _, r := range runners {
		if !used[r] || j.tests[r] != nil {
			continue
		}
		tc, err := newTestCtl(j, r)
		if err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ExtensionTestingUnavailable" {
				j.testingOn = false
				j.api.Log().Debug("the Testing view is not available; the JavaScript tests cannot be run")
				return
			}
			j.api.Log().Warn("the controller of the tests cannot be made", "runner", r, "err", err)
			continue
		}
		j.tests[r] = tc
	}
}

// registerTests makes the controllers of the runners the folder uses and follows its files. A window with no Testing view has none.
func (j *javascript) registerTests() error {
	j.tests = map[string]*testCtl{}
	j.testingOn = true
	j.makeControllers()
	// the Testing view never asks a controller for its tests, so they are found at the start (and again when files change)
	for _, tc := range j.tests {
		tc.requestDiscovery(false)
	}
	for topic, fn := range map[string]func(extension.Event){extension.TopicDocumentSaved: j.documentSaved, extension.TopicFilesChanged: j.filesChanged} {
		if _, err := j.api.Events().Subscribe(topic, fn); err != nil {
			return err
		}
	}
	return nil
}

// testsIdle tells whether no controller is finding its tests (the tests wait for it).
func (j *javascript) testsIdle() bool {
	for _, tc := range j.tests {
		if !tc.idle() {
			return false
		}
	}
	return true
}

// inNodeModules tells whether a path (relative with slashes, or absolute) is inside a node_modules folder.
func inNodeModules(path string) bool {
	return strings.HasPrefix(path, "node_modules/") || strings.Contains(path, "/node_modules/")
}
