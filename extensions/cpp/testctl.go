package cpp

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The controller of C and C++ tests (SPEC §10.32 "The providers", C and C++; "As built (step 26.9)"): the tests of a CMake project as
// `ctest` lists them in the build folder (`ctest --show-only=json-v1`), a flat list or, with a suite delimiter, groups. The tests are
// found when the pack starts in a folder whose build folder has a CTestTestfile.cmake (the Testing view never asks a controller for
// them), on Refresh Tests, and again after a CMake file or a CTestTestfile.cmake is saved, created, deleted or renamed.

// The names of the controller and of its two run profiles.
const (
	// TestControllerID is the id of the controller and TestControllerLabel its name.
	TestControllerID    = "ctest"
	TestControllerLabel = "CTest"
	runProfileLabel     = "ctest"
	debugProfileLabel   = "ctest (debug)"
)

// The roles of an item of the tree.
const (
	roleGroup = iota
	roleTest
)

// discoveryTimeout ends a discovery that does not finish; saveDebounce is how long the tests wait for more saves and file events before
// they are found again.
const (
	discoveryTimeout = 5 * time.Minute
	saveDebounce     = 400 * time.Millisecond
)

// testItemInfo says what an item of the tree is.
type testItemInfo struct {
	role int
	// test is the test CTest listed, for a test.
	test ctestTest
}

// testCtl is the controller and the state behind its items. Everything in it is safe for concurrent use.
type testCtl struct {
	c        *cpp
	ctl      extension.TestController
	profiles map[extension.TestRunKind]extension.TestRunProfile

	mu   sync.Mutex
	info map[*extension.TestItem]testItemInfo
	byID map[string]*extension.TestItem
	// byName finds the item of a test by its name, for the results of a run.
	byName map[string]*extension.TestItem

	// discMu serializes discoveries; the rest is the queue of the worker.
	discMu  sync.Mutex
	wmu     sync.Mutex
	pending bool
	loud    bool
	working bool
	timer   *time.Timer
}

// newTestCtl makes the controller and its profiles. It fails with ExtensionTestingUnavailable in a scope with no testing service.
func newTestCtl(c *cpp) (*testCtl, error) {
	ctl, err := c.api.Tests().CreateTestController(TestControllerID, TestControllerLabel)
	if err != nil {
		return nil, err
	}
	tc := &testCtl{
		c: c, ctl: ctl, profiles: map[extension.TestRunKind]extension.TestRunProfile{}, info: map[*extension.TestItem]testItemInfo{},
		byID: map[string]*extension.TestItem{}, byName: map[string]*extension.TestItem{},
	}
	for _, pr := range []struct {
		label string
		kind  extension.TestRunKind
	}{{runProfileLabel, extension.TestKindRun}, {debugProfileLabel, extension.TestKindDebug}} {
		prof, err := ctl.CreateRunProfile(pr.label, pr.kind, tc.handler(pr.kind), extension.TestRunProfileOptions{IsDefault: true})
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

// ---- paths ----

// fileURI is the file: URI of a path.
func fileURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// filePath is the path of a file: URI.
func filePath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return "", false
	}
	return u.Path, true
}

// within tells whether the path is the folder or inside it.
func within(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// clip shortens a text to n bytes.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}

// ---- the programs ----

// findCTest finds ctest in the PATH. It fails with CppCTestNotFound.
func findCTest() (string, error) {
	p, err := exec.LookPath("ctest")
	if err != nil {
		return "", errors.New("CppCTestNotFound", "ctest was not found in the PATH; it comes with CMake")
	}
	return p, nil
}

// hasBuildFolder tells whether the build folder is one CMake configured for a project with tests.
func hasBuildFolder(build string) bool {
	return fileExists(filepath.Join(build, "CTestTestfile.cmake"))
}

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
	tc.timer = time.AfterFunc(tc.c.testDebounce, tc.startWorker)
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
	c := tc.c
	c.work.Add(1)
	go func() {
		defer c.work.Done()
		for {
			tc.wmu.Lock()
			if !tc.pending || c.ctx.Err() != nil {
				tc.pending, tc.working = false, false
				tc.wmu.Unlock()
				return
			}
			loud := tc.loud
			tc.pending, tc.loud = false, false
			tc.wmu.Unlock()
			tc.discover(c.ctx, loud)
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

// discover finds the tests with ctest and makes the tree what it found. With loud a failure is told to the user.
func (tc *testCtl) discover(ctx context.Context, loud bool) {
	tc.discMu.Lock()
	defer tc.discMu.Unlock()
	c := tc.c
	folder := c.api.Scope().Folder
	if folder == "" {
		return
	}
	build := c.buildDirectory()
	if !hasBuildFolder(build) {
		c.api.Log().Debug("the build folder has no CTestTestfile.cmake", "build", build)
		if loud {
			c.api.Notifications().Info("There are no CTest tests: " + build + " is not a configured CMake build folder with tests. " +
				"Configure the project (cmake -S . -B build) or set " + BuildDirectorySetting + ".")
		}
		tc.apply(nil, "")
		return
	}
	ctest, err := findCTest()
	if err != nil {
		c.api.Log().Debug("the CTest tests cannot be found", "err", err)
		if loud {
			c.api.Notifications().Error("The CTest tests cannot be found: " + describe(err))
		}
		return
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	var lines []string
	runErr := c.runner(ctx, cmdSpec{Program: ctest, Args: []string{"--show-only=json-v1"}, Dir: build, Env: os.Environ()}, func(l string) { lines = append(lines, l) })
	if ctx.Err() != nil {
		return
	}
	tests, err := readTestList(strings.Join(lines, "\n"))
	if err != nil {
		c.api.Log().Debug("the CTest tests cannot be found", "err", err, "ctest", runErr)
		if loud {
			c.api.Notifications().Error("The CTest tests cannot be found: " + describe(err) + " " + firstLine(lines))
		}
		return
	}
	tc.apply(tests, c.suiteDelimiter())
}

// firstLine is the first line of the output that has text.
func firstLine(lines []string) string {
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			return clip(l, 200)
		}
	}
	return ""
}

// ---- the tree ----

// apply makes the tree what the discovery found: items that are still there are kept (with their results), the others go.
func (tc *testCtl) apply(tests []ctestTest, delimiter string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	seen := map[string]bool{}
	byName := map[string]*extension.TestItem{}
	for _, t := range tests {
		cur := tc.ctl.Items()
		parts := []string{t.Name}
		if delimiter != "" {
			if split := strings.Split(t.Name, delimiter); allNotEmpty(split) {
				parts = split
			}
		}
		for i, part := range parts[:len(parts)-1] {
			path := strings.Join(parts[:i+1], delimiter)
			g := tc.ensure(cur, "ctest:group:"+path, part, "", "", 0, "", testItemInfo{role: roleGroup}, seen)
			cur = g.Children()
		}
		desc := strings.Join(t.Labels, ", ")
		byName[t.Name] = tc.ensure(cur, "ctest:test:"+t.Name, parts[len(parts)-1], desc, t.File, t.Line, "", testItemInfo{role: roleTest, test: t}, seen)
	}
	tc.byName = byName
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

func allNotEmpty(parts []string) bool {
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

// ensure returns the item with the id, made when it is new, in the collection, with the label, the description and the place (line from
// 1, 0 when unknown). tc.mu is held.
func (tc *testCtl) ensure(coll *extension.TestItemCollection, id, label, desc, file string, line int, errText string, info testItemInfo, seen map[string]bool) *extension.TestItem {
	uri := ""
	if file != "" {
		uri = fileURI(file)
	}
	it := tc.byID[id]
	if it == nil {
		it = tc.ctl.CreateTestItem(id, label, uri)
		tc.byID[id] = it
	}
	// an item that moved (the delimiter changed) leaves its old collection first
	if holder := tc.holder(it); holder != coll {
		holder.Delete(id)
	}
	tc.info[it] = info
	seen[id] = true
	var rng *extension.Range
	if line > 0 {
		rng = &extension.Range{Start: extension.Position{Line: line - 1}, End: extension.Position{Line: line - 1}}
	}
	d := it.Data()
	if d.Label != label || d.Description != desc || d.Error != errText || d.URI != uri || (rng == nil) != (d.Range == nil) || (rng != nil && *rng != *d.Range) {
		it.Update(func(d *extension.TestItemData) {
			d.Label, d.Description, d.Error, d.Range, d.URI = label, desc, errText, rng, uri
		})
	}
	if err := coll.Add(it); err != nil {
		tc.c.api.Log().Warn("a test cannot be added to the tree", "id", id, "err", err)
	}
	return it
}

// itemForName is the item of the test with the name.
func (tc *testCtl) itemForName(name string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.byName[name]
}

// ---- files changing ----

// isCMakeFile tells whether a path is a file whose change can change the tests: a CMakeLists.txt, a .cmake file (CTestTestfile.cmake
// among them).
func isCMakeFile(path string) bool {
	return path != "" && (filepath.Base(path) == "CMakeLists.txt" || strings.HasSuffix(path, ".cmake"))
}

// documentSaved finds the tests again when the saved file is a CMake file.
func (c *cpp) documentSaved(ev extension.Event) {
	if c.tests == nil {
		return
	}
	var saved extension.DocumentSaved
	if ev.Decode(&saved) != nil {
		return
	}
	path, ok := filePath(saved.URI)
	if !ok || !isCMakeFile(path) || !within(path, c.api.Scope().Folder) {
		return
	}
	c.tests.requestDiscovery(false)
}

// filesChanged finds the tests again when a CMake file is created, deleted or moved.
func (c *cpp) filesChanged(ev extension.Event) {
	if c.tests == nil {
		return
	}
	var fc extension.FilesChanged
	if ev.Decode(&fc) != nil {
		return
	}
	for _, ch := range fc.Changes {
		switch ch.Kind {
		case extension.FileCreated, extension.FileDeleted, extension.FileRenamed, extension.FileRescan:
		default:
			continue
		}
		if ch.Kind == extension.FileRescan || isCMakeFile(ch.Path) || isCMakeFile(ch.OldPath) {
			c.tests.requestDiscovery(false)
			return
		}
	}
}

// settingsChanged finds the tests again when the build folder or the delimiter changed.
func (c *cpp) testSettingsChanged() {
	if c.tests == nil {
		return
	}
	build, delim := c.buildDirectory(), c.suiteDelimiter()
	if build != c.lastBuild || delim != c.lastDelimiter {
		c.lastBuild, c.lastDelimiter = build, delim
		c.tests.requestDiscovery(false)
	}
}

// registerTests makes the controller and follows the files. A window with no Testing view has no controller.
func (c *cpp) registerTests() error {
	tc, err := newTestCtl(c)
	if err != nil {
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionTestingUnavailable" {
			return err
		}
		c.api.Log().Debug("the Testing view is not available; the CTest tests cannot be run")
		return nil
	}
	c.tests = tc
	c.lastBuild, c.lastDelimiter = c.buildDirectory(), c.suiteDelimiter()
	// the Testing view never asks a controller for its tests, so they are found at the start (and again when files change)
	if folder := c.api.Scope().Folder; folder != "" && hasBuildFolder(c.lastBuild) {
		tc.requestDiscovery(false)
	}
	for topic, fn := range map[string]func(extension.Event){extension.TopicDocumentSaved: c.documentSaved, extension.TopicFilesChanged: c.filesChanged} {
		if _, err := c.api.Events().Subscribe(topic, fn); err != nil {
			return err
		}
	}
	c.api.Settings().OnChange(c.testSettingsChanged)
	return nil
}

// holder is the collection the item is in: the children of its parent, or the top items.
func (tc *testCtl) holder(it *extension.TestItem) *extension.TestItemCollection {
	if parent := it.Parent(); parent != nil {
		return parent.Children()
	}
	return tc.ctl.Items()
}
