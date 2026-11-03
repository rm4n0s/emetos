package python

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The controller of Python's tests (SPEC §10.32 "The providers", Python; "As built (step 26.6)"): the tree of folders, files, classes,
// functions and parametrized cases that the Testing view shows, found by the helper scripts with pytest or unittest and kept up to date
// when a Python file is saved, created or deleted.

// Modelled on vscode-python's test controller: the shape of the tree (folders, files, classes, functions, parameters) and the settings python.testing.*,
// as remembered; the source of vscode-python was not at hand, so nothing here is a port (decision 1640).

// The names of the controller and of its three run profiles (VS Code's Python profiles are "Run Tests", "Debug Tests" and "Coverage").
const (
	// TestControllerID is the id of the controller and TestControllerLabel its name.
	TestControllerID    = "python"
	TestControllerLabel = "Python"
	runProfileLabel     = "Run Tests"
	debugProfileLabel   = "Debug Tests"
	coverProfileLabel   = "Run Tests with Coverage"
)

// The frameworks.
const (
	frameworkPytest   = "pytest"
	frameworkUnittest = "unittest"
)

// The roles of an item of the tree.
const (
	roleFolder = iota
	roleFile
	roleClass
	roleFunc
	roleParam
)

// discoveryTimeout ends a discovery that does not finish.
const discoveryTimeout = 3 * time.Minute

// saveDebounce is how long the tests wait for more saves and file events before they are found again.
const saveDebounce = 400 * time.Millisecond

// testItemInfo says what an item of the tree is.
type testItemInfo struct {
	role int
	// path is the folder or file of the item.
	path string
	// arg is what a run names the item by: a pytest argument (/path/test_a.py::TestA::test_b[1]) or a unittest name (pkg.mod.TestA).
	arg string
	// runID is the id the framework reports the test by (leaves only).
	runID string
}

// testCtl is the controller and the state behind its items. Everything in it is safe for concurrent use.
type testCtl struct {
	p        *python
	ctl      extension.TestController
	profiles map[extension.TestRunKind]extension.TestRunProfile

	mu    sync.Mutex
	info  map[*extension.TestItem]testItemInfo
	byID  map[string]*extension.TestItem
	byRun map[string]*extension.TestItem
	// rootdir is the root folder pytest reported (node ids are relative to it), and framework the one the tree was found with.
	rootdir, framework string
	// pytestOf caches whether an interpreter has pytest.
	pytestOf map[string]bool
	runs     map[int]context.CancelFunc
	nrun     int
	last     *lastTest

	// discMu serializes discoveries; the rest is the queue of the worker.
	discMu  sync.Mutex
	wmu     sync.Mutex
	pending bool
	loud    bool
	working bool
	timer   *time.Timer
}

// lastTest is the run Test Previous runs again.
type lastTest struct {
	ids  []string
	kind extension.TestRunKind
}

// newTestCtl makes the controller and its profiles. It fails with ExtensionTestingUnavailable in a scope with no testing service.
func newTestCtl(p *python) (*testCtl, error) {
	ctl, err := p.api.Tests().CreateTestController(TestControllerID, TestControllerLabel)
	if err != nil {
		return nil, err
	}
	tc := &testCtl{
		p: p, ctl: ctl, profiles: map[extension.TestRunKind]extension.TestRunProfile{}, info: map[*extension.TestItem]testItemInfo{},
		byID: map[string]*extension.TestItem{}, byRun: map[string]*extension.TestItem{}, pytestOf: map[string]bool{}, runs: map[int]context.CancelFunc{},
	}
	for _, pr := range []struct {
		label string
		kind  extension.TestRunKind
	}{{runProfileLabel, extension.TestKindRun}, {debugProfileLabel, extension.TestKindDebug}, {coverProfileLabel, extension.TestKindCoverage}} {
		opts := extension.TestRunProfileOptions{IsDefault: true, SupportsContinuousRun: pr.kind == extension.TestKindRun, ContinuousTrigger: tc.continuousTrigger}
		if pr.kind == extension.TestKindCoverage {
			opts.LoadDetailedCoverage = loadPythonCoverage
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

// continuousTrigger says that a change of a Python file of the folder of the workspace runs the continuous run of the item again.
func (tc *testCtl) continuousTrigger(_ *extension.TestItem, uri string) bool {
	path, ok := filePath(uri)
	return ok && strings.HasSuffix(path, ".py") && within(path, tc.p.api.Scope().Folder)
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

// ---- the framework ----

// testInterpreter is the path of the interpreter the tests run with. With notify it tells the user when there is none. It fails with
// PythonTestNoInterpreter.
func (p *python) testInterpreter(notify bool) (string, error) {
	i, _, ok := p.interps.Resolve(p.interps.environ(p.interps.snapshot()))
	if !ok {
		if notify {
			p.api.Notifications().Warn("No Python interpreter was found. Choose one with Python: Select Interpreter, or set python.defaultInterpreterPath.")
		}
		return "", errors.New("PythonTestNoInterpreter", "no Python interpreter was found: choose one with Python: Select Interpreter or set python.defaultInterpreterPath")
	}
	return i.Path, nil
}

// frameworkFor is the framework the settings choose: pytest when it is enabled, else unittest when it is, else pytest if the interpreter
// has it and unittest if not.
func (tc *testCtl) frameworkFor(ctx context.Context, cfg testConfig, python string) string {
	switch {
	case cfg.pytestOn:
		return frameworkPytest
	case cfg.unittestOn:
		return frameworkUnittest
	}
	tc.mu.Lock()
	have, known := tc.pytestOf[python]
	tc.mu.Unlock()
	if !known {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		have = tc.p.runner(ctx, cmdSpec{Program: python, Args: []string{"-c", "import pytest"}, Dir: cfg.cwd, Env: tc.env(cfg, "")}, nil) == nil
		tc.mu.Lock()
		tc.pytestOf[python] = have
		tc.mu.Unlock()
	}
	if have {
		return frameworkPytest
	}
	return frameworkUnittest
}

// env is the environment of a program of the provider: Emetos's, the variables of python.envFile, the folder of the helpers on
// PYTHONPATH and unbuffered UTF-8 output.
func (tc *testCtl) env(cfg testConfig, helpers string) []string {
	env := os.Environ()
	base := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			base[k] = v
		}
	}
	if file, _ := extension.SettingValue[string](tc.p.api.Settings(), EnvFileSetting); file != "" {
		file = strings.ReplaceAll(file, "${workspaceFolder}", tc.p.api.Scope().Folder)
		if vars, err := readEnvFile(file, base); err != nil {
			tc.p.api.Log().Warn("the environment file cannot be read for the tests", "file", file, "err", err)
		} else {
			for k, v := range vars {
				env = append(env, k+"="+v)
			}
		}
	}
	if helpers != "" {
		path := helpers
		if old := base["PYTHONPATH"]; old != "" {
			path += string(os.PathListSeparator) + old
		}
		env = append(env, "PYTHONPATH="+path)
	}
	return append(env, "PYTHONUNBUFFERED=1", "PYTHONIOENCODING=utf-8")
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
	tc.timer = time.AfterFunc(tc.p.testDebounce, tc.startWorker)
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
	p := tc.p
	p.closers.Add(1)
	go func() {
		defer p.closers.Done()
		for {
			tc.wmu.Lock()
			if !tc.pending || p.ctx.Err() != nil {
				tc.pending, tc.working = false, false
				tc.wmu.Unlock()
				return
			}
			loud := tc.loud
			tc.pending, tc.loud = false, false
			tc.wmu.Unlock()
			tc.discover(p.ctx, loud)
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

// discover finds the tests with the framework and makes the tree what the helper says. With loud a failure is told to the user.
func (tc *testCtl) discover(ctx context.Context, loud bool) {
	tc.discMu.Lock()
	defer tc.discMu.Unlock()
	p := tc.p
	cfg := p.readTestConfig()
	if cfg.cwd == "" {
		return
	}
	python, err := p.testInterpreter(loud)
	if err != nil {
		p.api.Log().Debug("the tests cannot be found", "err", err)
		return
	}
	if loud {
		// Refresh Tests asks again whether the interpreter has pytest: the user may have installed it
		tc.mu.Lock()
		delete(tc.pytestOf, python)
		tc.mu.Unlock()
	}
	helpers, err := helperDir()
	if err != nil {
		p.api.Log().Warn("the helper scripts of the tests cannot be written", "err", err)
		if loud {
			p.api.Notifications().Error("The Python tests cannot be found: " + describe(err))
		}
		return
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	fw := tc.frameworkFor(ctx, cfg, python)
	spec := cmdSpec{Program: python, Dir: cfg.cwd, Env: tc.env(cfg, helpers)}
	if fw == frameworkPytest {
		spec.Args = append([]string{"-m", "pytest", "-p", pytestPlugin}, cfg.pytestArgs...)
		spec.Args = append(spec.Args, "--collect-only", "-q", "--continue-on-collection-errors")
	} else {
		start, pattern, top := unittestDiscovery(cfg.unittestArgs)
		spec.Args = append([]string{filepath.Join(helpers, unittestScript), "discover"}, unittestFlags(start, pattern, top)...)
	}
	var found *testEvent
	var errs []testEvent
	var stray []string
	runErr := p.runner(ctx, spec, func(line string) {
		_, ev, ok, err := parseEvent(line)
		switch {
		case err != nil:
			p.api.Log().Debug("a line of the test discovery is not an event", "err", err)
		case !ok:
			if strings.TrimSpace(line) != "" {
				stray = append(stray, line)
			}
		case ev.Type == "discovery":
			e := ev
			found = &e
		case ev.Type == "collect_error":
			errs = append(errs, ev)
		}
	})
	if ctx.Err() != nil {
		return
	}
	if found == nil {
		why := "the helper reported nothing"
		if len(stray) > 0 {
			why = stray[len(stray)-1]
		} else if runErr != nil {
			why = describe(runErr)
		}
		p.api.Log().Warn("the Python tests cannot be found", "framework", fw, "why", why)
		if loud {
			p.api.Notifications().Error("The Python tests cannot be found with " + fw + ": " + why)
		}
		return
	}
	tc.apply(fw, *found, errs)
}

// unittestFlags are the arguments of the helper for the discovery arguments.
func unittestFlags(start, pattern, top string) []string {
	args := []string{"-s", start, "-p", pattern}
	if top != "" {
		args = append(args, "-t", top)
	}
	return args
}

// ---- the tree ----

// apply makes the tree what the discovery found: items that are still there are kept (with their results), the others go.
func (tc *testCtl) apply(fw string, found testEvent, errs []testEvent) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.rootdir, tc.framework = found.Rootdir, fw
	seen := map[string]bool{}
	byRun := map[string]*extension.TestItem{}
	for _, rec := range found.Tests {
		if it := tc.addRecord(rec, seen); it != nil {
			byRun[rec.ID] = it
		}
	}
	for _, e := range errs {
		tc.ensureFile(testRecord{File: e.File}, seen, firstLine(e.Message))
	}
	tc.byRun = byRun
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

// ensure returns the item with the id, made when it is new, in the collection, with the label and the line of its place (0 when unknown).
// tc.mu is held.
func (tc *testCtl) ensure(coll *extension.TestItemCollection, id, label, uri string, line int, info testItemInfo, seen map[string]bool, errText string) *extension.TestItem {
	it := tc.byID[id]
	if it == nil {
		it = tc.ctl.CreateTestItem(id, label, uri)
		tc.byID[id] = it
	}
	tc.info[it] = info
	seen[id] = true
	var rng *extension.Range
	if line > 0 {
		rng = &extension.Range{Start: extension.Position{Line: line - 1}, End: extension.Position{Line: line - 1}}
	}
	d := it.Data()
	if d.Label != label || d.Error != errText || (rng == nil) != (d.Range == nil) || (rng != nil && *rng != *d.Range) {
		it.Update(func(d *extension.TestItemData) { d.Label, d.Error, d.Range = label, errText, rng })
	}
	if err := coll.Add(it); err != nil {
		tc.p.api.Log().Warn("a test cannot be added to the tree", "id", id, "err", err)
	}
	return it
}

// folderCollection is the children of the item of the folder of the file, made with the folders above it down from the folder of the
// workspace; a file outside the workspace is at the top. tc.mu is held.
func (tc *testCtl) folderCollection(file string, seen map[string]bool) *extension.TestItemCollection {
	coll := tc.ctl.Items()
	root := tc.p.api.Scope().Folder
	dir := filepath.Dir(file)
	if root == "" || !within(dir, root) || dir == root {
		return coll
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return coll
	}
	cur := root
	for _, el := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, el)
		it := tc.ensure(coll, fileURI(cur)+"?folder", el, fileURI(cur), 0, testItemInfo{role: roleFolder, path: cur, arg: cur}, seen, "")
		coll = it.Children()
	}
	return coll
}

// ensureFile is the item of the file; errText is the message of a file that could not be collected. tc.mu is held.
func (tc *testCtl) ensureFile(rec testRecord, seen map[string]bool, errText string) *extension.TestItem {
	arg := rec.File
	if tc.framework == frameworkUnittest {
		arg = rec.Module
	}
	return tc.ensure(tc.folderCollection(rec.File, seen), fileURI(rec.File), filepath.Base(rec.File), fileURI(rec.File), 0,
		testItemInfo{role: roleFile, path: rec.File, arg: arg}, seen, errText)
}

// addRecord puts a test found by the discovery in the tree, with the file, the classes and the function above it, and returns its item.
// tc.mu is held.
func (tc *testCtl) addRecord(rec testRecord, seen map[string]bool) *extension.TestItem {
	if rec.File == "" || rec.Func == "" {
		return nil
	}
	uri := fileURI(rec.File)
	unit := tc.framework == frameworkUnittest
	cur := tc.ensureFile(rec, seen, "")
	id, arg := uri, rec.File
	if unit {
		arg = rec.Module
	}
	for _, c := range rec.Classes {
		id += "::" + c.Name
		if unit {
			arg += "." + c.Name
		} else {
			arg += "::" + c.Name
		}
		cur = tc.ensure(cur.Children(), id, c.Name, uri, c.Line, testItemInfo{role: roleClass, path: rec.File, arg: arg}, seen, "")
	}
	id += "::" + rec.Func
	if unit {
		arg += "." + rec.Func
	} else {
		arg += "::" + rec.Func
	}
	if rec.Param == "" {
		return tc.ensure(cur.Children(), id, rec.Func, uri, rec.Line, testItemInfo{role: roleFunc, path: rec.File, arg: arg, runID: rec.ID}, seen, "")
	}
	fn := tc.ensure(cur.Children(), id, rec.Func, uri, rec.Line, testItemInfo{role: roleFunc, path: rec.File, arg: arg}, seen, "")
	return tc.ensure(fn.Children(), id+rec.Param, rec.Param, uri, rec.Line, testItemInfo{role: roleParam, path: rec.File, arg: arg + rec.Param, runID: rec.ID}, seen, "")
}

// recordFromNodeID makes the record of a test a run reported that the discovery did not: pytest's node id says the file (relative to the
// root folder), the classes, the function and the parameters.
func (tc *testCtl) recordFromNodeID(id string) (testRecord, bool) {
	file, rest, ok := strings.Cut(id, "::")
	if !ok || file == "" || rest == "" {
		return testRecord{}, false
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(tc.rootdir, filepath.FromSlash(file))
	}
	param := ""
	if i := strings.IndexByte(rest, '['); i >= 0 {
		rest, param = rest[:i], rest[i:]
	}
	parts := strings.Split(rest, "::")
	rec := testRecord{ID: id, File: file, Func: parts[len(parts)-1], Param: param}
	for _, c := range parts[:len(parts)-1] {
		rec.Classes = append(rec.Classes, classRecord{Name: c})
	}
	return rec, true
}

// itemForRun is the item of the test the framework reported by id; a pytest test the discovery did not show is added.
func (tc *testCtl) itemForRun(id string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if it := tc.byRun[id]; it != nil {
		return it
	}
	if tc.framework != frameworkPytest {
		return nil
	}
	rec, ok := tc.recordFromNodeID(id)
	if !ok {
		return nil
	}
	it := tc.addRecord(rec, map[string]bool{})
	if it != nil {
		tc.byRun[id] = it
	}
	return it
}

// fileItemFor is the item of the file, made when the discovery did not show it (a file that failed to import during a run).
func (tc *testCtl) fileItemFor(path string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if it := tc.byID[fileURI(path)]; it != nil {
		return it
	}
	return tc.ensureFile(testRecord{File: path}, map[string]bool{}, "")
}

// holderItem is the item of the class or module a failed fixture belongs to, named by "setUpClass (pkg.mod.TestA)".
func (tc *testCtl) holderItem(id string) *extension.TestItem {
	open, closing := strings.LastIndexByte(id, '('), strings.LastIndexByte(id, ')')
	if open < 0 || closing < open {
		return nil
	}
	name := id[open+1 : closing]
	tc.mu.Lock()
	defer tc.mu.Unlock()
	for it, info := range tc.info {
		if info.arg == name && (info.role == roleClass || info.role == roleFile) {
			return it
		}
	}
	return nil
}

// ---- files changing ----

// testSettingsChanged finds the tests again when a setting that decides them changed.
func (p *python) testSettingsChanged() {
	if p.tests == nil {
		return
	}
	cfg := p.readTestConfig()
	key := strings.Join(append(append([]string{cfg.cwd, boolText(cfg.pytestOn), boolText(cfg.unittestOn)}, cfg.pytestArgs...), cfg.unittestArgs...), "\x00")
	if key == p.testKey {
		return
	}
	first := p.testKey == ""
	p.testKey = key
	if !first {
		p.tests.requestDiscovery(false)
	}
}

func boolText(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// documentSaved finds the tests again when the saved file is a Python file the setting's pattern names.
func (p *python) documentSaved(ev extension.Event) {
	if p.tests == nil {
		return
	}
	var saved extension.DocumentSaved
	if ev.Decode(&saved) != nil {
		return
	}
	path, ok := filePath(saved.URI)
	if !ok || !strings.HasSuffix(path, ".py") {
		return
	}
	if on, err := extension.SettingValue[bool](p.api.Settings(), AutoDiscoverSetting); err == nil && !on {
		return
	}
	pattern, _ := extension.SettingValue[string](p.api.Settings(), AutoDiscoverPatternSetting)
	root := p.api.Scope().Folder
	if !within(path, root) {
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || (pattern != "" && !globMatch(pattern, filepath.ToSlash(rel))) {
		return
	}
	p.tests.requestDiscovery(false)
}

// filesChanged finds the tests again when a Python file is created, deleted or moved.
func (p *python) filesChanged(ev extension.Event) {
	if p.tests == nil {
		return
	}
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
		if c.Kind == extension.FileRescan || strings.HasSuffix(c.Path, ".py") || strings.HasSuffix(c.OldPath, ".py") {
			p.tests.requestDiscovery(false)
			return
		}
	}
}

// globMatch matches a slash-separated relative path against a pattern with *, ? and **.
func globMatch(pattern, name string) bool {
	return globParts(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func globParts(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if globParts(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if ok, err := filepath.Match(pat[0], name[0]); err != nil || !ok {
		return false
	}
	return globParts(pat[1:], name[1:])
}

// describe is the text of an error for a message to the user.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		if appErr.Message != "" {
			return appErr.Message
		}
		return appErr.Tag
	}
	return err.Error()
}

// registerTests makes the controller and follows the files and the settings that decide the tests. A window with no Testing view has no
// controller. The tests are found at once when the settings enable a framework; otherwise when the Testing view first asks for them.
func (p *python) registerTests() error {
	tc, err := newTestCtl(p)
	if err != nil {
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionTestingUnavailable" {
			return err
		}
		p.api.Log().Debug("the Testing view is not available; the Python tests cannot be run")
		return nil
	}
	p.tests = tc
	for topic, fn := range map[string]func(extension.Event){extension.TopicDocumentSaved: p.documentSaved, extension.TopicFilesChanged: p.filesChanged} {
		if _, err := p.api.Events().Subscribe(topic, fn); err != nil {
			return err
		}
	}
	p.testSettingsChanged()
	if cfg := p.readTestConfig(); cfg.pytestOn || cfg.unittestOn {
		tc.requestDiscovery(false)
	}
	return nil
}

// wait waits for the goroutines of the provider to end (the tests use it).
func (p *python) wait() {
	if p.tests != nil {
		p.tests.stop()
	}
	p.closers.Wait()
}

// firstLine is the first line of the text, for the error row of an item.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
