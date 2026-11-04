package golang

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The controller of Go's tests (SPEC §10.32 "The providers", Go; §10.17 "As built (step 26.5)"): the tree of packages, files, test
// functions and subtests that the Testing view shows, found with go/parser as the lenses are and kept up to date when a _test.go file
// or a go.mod changes.

// Ported from VS Code Go: extension/src/goTest/ (the items of the tests: packages, files, functions; their ids are
// <uri>?<kind>#<name>, and a subtest's name is the whole name go test gives it).

// The names of the controller and of its three run profiles.
const (
	// TestControllerID is the id of the controller and TestControllerLabel its name.
	TestControllerID    = "go"
	TestControllerLabel = "Go"
	// The labels of the run profiles (VS Code Go's).
	runProfileLabel   = "go test"
	debugProfileLabel = "go test (debug)"
	coverProfileLabel = "go test (coverage)"
)

// The roles of an item of the tree.
const (
	rolePackage = iota
	roleFile
	roleFunc
)

// maxTestFiles is the most _test.go files a discovery reads; a workspace with more shows the first ones.
const maxTestFiles = 20000

// maxTestFileBytes is the largest test file that is read.
const maxTestFileBytes = 2 << 20

// testPkg is a package with tests: a folder with _test.go files.
type testPkg struct {
	dir string
	// importPath is the import path go test reports the package by ("example.com/p/sub"), empty outside a module, and modRoot the
	// folder of the go.mod above it.
	importPath, modRoot string
	item                *extension.TestItem
	files               map[string]*testFile
	// byName holds the test functions and the subtests by their whole name ("TestA", "TestA/b").
	byName map[string]*extension.TestItem
}

// testFile is a _test.go file of a package.
type testFile struct {
	sum  [sha256.Size]byte
	item *extension.TestItem
	// seq is the number of the load that made the item: discoverAll leaves alone what was loaded after it began.
	seq uint64
}

// testItemInfo says what an item of the tree is.
type testItemInfo struct {
	role int
	pkg  *testPkg
	// file is the path of the file of a file or a function item, name the whole name of a function or subtest, and kind the kind of
	// the function (kindTest, kindBenchmark, kindExample, kindFuzz).
	file, name string
	kind       int
}

// modInfo is what is known of a folder: the go.mod above it.
type modInfo struct {
	root, path string
	ok         bool
}

// testCtl is the controller and the state behind its items. Everything in it is safe for concurrent use.
type testCtl struct {
	g   *golang
	ctl extension.TestController
	// profiles by kind.
	profiles map[extension.TestRunKind]extension.TestRunProfile

	mu       sync.Mutex
	pkgs     map[string]*testPkg
	byImport map[string]*testPkg
	info     map[*extension.TestItem]testItemInfo
	byID     map[string]*extension.TestItem
	mods     map[string]modInfo
	// listed, when set (by a test), is called by discoverAll when it has the list of the files and before it takes out the ones that are not in it.
	listed func()
	// seq counts the loads of files, for testFile.seq.
	seq uint64
	// runs are the cancel functions of the runs the commands started, and last the request of the newest one.
	runs map[int]context.CancelFunc
	nrun int
	last *lastTest

	// the queue of the refresh worker
	wmu     sync.Mutex
	pending map[string]bool
	full    bool
	working bool
}

// lastTest is the run Test Previous runs again.
type lastTest struct {
	ids  []string
	kind extension.TestRunKind
}

// newTestCtl makes the controller and its profiles. It fails with ExtensionTestingUnavailable in a scope with no testing service.
func newTestCtl(g *golang) (*testCtl, error) {
	ctl, err := g.api.Tests().CreateTestController(TestControllerID, TestControllerLabel)
	if err != nil {
		return nil, err
	}
	tc := &testCtl{
		g: g, ctl: ctl, profiles: map[extension.TestRunKind]extension.TestRunProfile{},
		pkgs: map[string]*testPkg{}, byImport: map[string]*testPkg{}, info: map[*extension.TestItem]testItemInfo{},
		byID: map[string]*extension.TestItem{}, mods: map[string]modInfo{}, runs: map[int]context.CancelFunc{}, pending: map[string]bool{},
	}
	for _, p := range []struct {
		label string
		kind  extension.TestRunKind
	}{{runProfileLabel, extension.TestKindRun}, {debugProfileLabel, extension.TestKindDebug}, {coverProfileLabel, extension.TestKindCoverage}} {
		opts := extension.TestRunProfileOptions{IsDefault: true, SupportsContinuousRun: p.kind == extension.TestKindRun, ContinuousTrigger: tc.continuousTrigger}
		if p.kind == extension.TestKindCoverage {
			opts.LoadDetailedCoverage = loadGoCoverage
		}
		prof, err := ctl.CreateRunProfile(p.label, p.kind, tc.handler(p.kind), opts)
		if err != nil {
			return nil, err
		}
		tc.profiles[p.kind] = prof
	}
	ctl.SetResolveHandler(func(ctx context.Context, item *extension.TestItem) error {
		if item == nil {
			tc.discoverAll(ctx)
		}
		return nil
	})
	ctl.SetRefreshHandler(func(ctx context.Context) error {
		tc.discoverAll(ctx)
		return nil
	})
	return tc, nil
}

// continuousTrigger says that a change of a Go file of the folder of the package, or of its go.mod, runs the continuous run of the
// item again. The folder is the one in the id of the item (the URI before the ?), so no lock is needed.
func (tc *testCtl) continuousTrigger(item *extension.TestItem, uri string) bool {
	path, ok := filePath(uri)
	if !ok {
		return false
	}
	if base := filepath.Base(path); base == "go.mod" || base == "go.sum" {
		return true
	}
	itemURI, kind, _ := strings.Cut(item.ID(), "?")
	itemPath, ok := filePath(itemURI)
	if !ok {
		return false
	}
	dir := itemPath
	if !strings.HasPrefix(kind, "package") {
		dir = filepath.Dir(itemPath)
	}
	return strings.HasSuffix(path, ".go") && filepath.Dir(path) == dir
}

// ---- ids ----

func pkgID(dir string) string              { return fileURI(dir) + "?package" }
func fileID(path string) string            { return fileURI(path) + "?file" }
func testID(uri, kind, name string) string { return uri + "?" + kind + "#" + name }

// kindName is the word of the kind of a function in the id of its item.
func kindName(kind int) string {
	switch kind {
	case kindBenchmark:
		return "benchmark"
	case kindExample:
		return "example"
	case kindFuzz:
		return "fuzz"
	}
	return "test"
}

// kindOfName is the kind of a function by its name, for a test the run found that the source has not.
func kindOfName(name string) int {
	top, _, _ := strings.Cut(name, "/")
	switch {
	case strings.HasPrefix(top, "Benchmark"):
		return kindBenchmark
	case strings.HasPrefix(top, "Example"):
		return kindExample
	case strings.HasPrefix(top, "Fuzz"):
		return kindFuzz
	}
	return kindTest
}

// ---- modules ----

// moduleOf finds the go.mod above the folder and the import path of the folder in its module. The answers are kept until the next
// full discovery.
func (tc *testCtl) moduleOf(dir string) (root, importPath string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	for d := dir; ; {
		m, seen := tc.mods[d]
		if !seen {
			m = readModule(d)
			tc.mods[d] = m
		}
		if m.ok {
			rel, err := filepath.Rel(m.root, dir)
			if err != nil || rel == "." {
				return m.root, m.path
			}
			return m.root, m.path + "/" + filepath.ToSlash(rel)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", ""
		}
		d = parent
	}
}

// readModule reads the go.mod in the folder, if there is one. A go.mod that cannot be read is no module here: the go command will say
// so when the tests run.
func readModule(dir string) modInfo {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return modInfo{}
	}
	for _, l := range strings.Split(string(data), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "module" {
			return modInfo{root: dir, path: strings.Trim(f[1], `"`+"`"), ok: true}
		}
	}
	return modInfo{}
}

// ---- discovery ----

// skippedDir tells whether go skips the folder name when it walks for packages (and the tools' folders that hold none of the user's).
func skippedDir(name string) bool {
	return name == "testdata" || name == "vendor" || name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// testFilesOf are the _test.go files of the workspace, by their absolute paths, that go would look at: none below a folder it skips;
// complete says that the walk of the folder has ended, so that a file that is not in the list is gone.
func (tc *testCtl) testFilesOf() (files []string, complete bool) {
	root := tc.g.api.Scope().Folder
	if root == "" {
		return nil, false
	}
	list, err := tc.g.api.Workspace().Files()
	if err != nil {
		tc.g.api.Log().Debug("the files of the workspace cannot be listed", "err", err)
		return nil, false
	}
	for _, rel := range list.Files {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		skip := false
		for _, el := range strings.Split(rel, "/")[:strings.Count(rel, "/")] {
			if skippedDir(el) {
				skip = true
				break
			}
		}
		if !skip {
			files = append(files, filepath.Join(root, filepath.FromSlash(rel)))
		}
		if len(files) >= maxTestFiles {
			return files, false
		}
	}
	return files, list.Complete
}

// discoverAll reads the test files of the workspace and makes the tree what they say: files that are gone are taken out.
func (tc *testCtl) discoverAll(ctx context.Context) {
	tc.mu.Lock()
	tc.mods = map[string]modInfo{}
	began := tc.seq // what is loaded after this is newer than the list below
	tc.mu.Unlock()
	files, complete := tc.testFilesOf()
	if tc.listed != nil {
		tc.listed()
	}
	seen := make(map[string]bool, len(files))
	for _, path := range files {
		if ctx.Err() != nil {
			return
		}
		seen[path] = true
		tc.loadFile(path)
	}
	if !complete {
		return // the walk of the folder has not ended: a file that is not in the list may just not be there yet
	}
	var gone []string
	tc.mu.Lock()
	for _, p := range tc.pkgs {
		for path, f := range p.files {
			// a file that a command loaded from the editor while the list was made may be missing from the list, and is not gone
			if !seen[path] && f.seq <= began && within(tc.g.api.Scope().Folder, path) {
				gone = append(gone, path)
			}
		}
	}
	tc.mu.Unlock()
	for _, path := range gone {
		tc.removeFile(path)
	}
}

// loadFile reads the file from disk and puts its tests in the tree; a file that is gone is taken out.
func (tc *testCtl) loadFile(path string) {
	st, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		tc.removeFile(path)
		return
	case err != nil:
		tc.g.api.Log().Warn("the test file cannot be read", "path", path, "err", err)
		return
	case st.Size() > maxTestFileBytes:
		tc.g.api.Log().Warn("the test file is too large to be read for the tests", "path", path, "size", st.Size())
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		tc.g.api.Log().Warn("the test file cannot be read", "path", path, "err", err)
		return
	}
	tc.loadText(path, string(data))
}

// loadText puts the tests of the text of a file in the tree (the editor's text for a command at the cursor). A file whose text is what
// the tree has already is left alone, so that its items and results stay; a file with no tests is taken out.
func (tc *testCtl) loadText(path, text string) {
	scan := scanTestFile(text)
	if len(scan.funcs) == 0 {
		tc.removeFile(path)
		return
	}
	sum := sha256.Sum256([]byte(text))
	dir := filepath.Dir(path)
	tc.mu.Lock()
	pkg := tc.pkgs[dir]
	if pkg != nil {
		if f := pkg.files[path]; f != nil && f.sum == sum {
			tc.mu.Unlock()
			return
		}
	}
	tc.mu.Unlock()
	root, importPath := tc.moduleOf(dir)
	tc.mu.Lock()
	defer tc.mu.Unlock()
	isNew := false
	if pkg = tc.pkgs[dir]; pkg == nil {
		pkg = &testPkg{dir: dir, importPath: importPath, modRoot: root, files: map[string]*testFile{}, byName: map[string]*extension.TestItem{}}
		tc.pkgs[dir] = pkg
		if importPath != "" {
			tc.byImport[importPath] = pkg
		}
		pkg.item = tc.newItem(pkgID(dir), tc.packageLabel(dir), fileURI(dir), testItemInfo{role: rolePackage, pkg: pkg})
		isNew = true
	}
	if old := pkg.files[path]; old != nil {
		tc.unindexLocked(pkg, old.item)
	}
	tc.seq++
	file := &testFile{sum: sum, item: tc.fileItem(pkg, path, scan), seq: tc.seq}
	pkg.files[path] = file
	if err := pkg.item.Children().Add(file.item); err != nil {
		tc.g.api.Log().Warn("the tests of the file cannot be added", "path", path, "err", err)
	}
	if isNew {
		if err := tc.ctl.Items().Add(pkg.item); err != nil {
			tc.g.api.Log().Warn("the package cannot be added", "dir", dir, "err", err)
		}
	}
}

// packageLabel is the name of a package folder: its path from the folder of the workspace, the name of the folder for the folder itself.
func (tc *testCtl) packageLabel(dir string) string {
	if root := tc.g.api.Scope().Folder; root != "" && within(root, dir) {
		if rel, err := filepath.Rel(root, dir); err == nil && rel != "." {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(dir)
}

// newItem makes an item, remembers what it is and returns it. tc.mu is held.
func (tc *testCtl) newItem(id, label, uri string, info testItemInfo) *extension.TestItem {
	it := tc.ctl.CreateTestItem(id, label, uri)
	tc.info[it] = info
	tc.byID[id] = it
	return it
}

// fileItem makes the item of a file with the items of its functions and their subtests. tc.mu is held.
func (tc *testCtl) fileItem(pkg *testPkg, path string, scan goTestFile) *extension.TestItem {
	uri := fileURI(path)
	file := tc.newItem(fileID(path), filepath.Base(path), uri, testItemInfo{role: roleFile, pkg: pkg, file: path})
	for _, fn := range scan.funcs {
		it := tc.newItem(testID(uri, kindName(fn.kind), fn.name), fn.name, uri, testItemInfo{role: roleFunc, pkg: pkg, file: path, name: fn.name, kind: fn.kind})
		rng := extension.Range{Start: extension.Position{Line: fn.startLine, Col: fn.col}, End: extension.Position{Line: fn.endLine, Col: fn.endCol}}
		it.Update(func(d *extension.TestItemData) { d.Range = &rng })
		pkg.byName[fn.name] = it
		tc.addSubtests(pkg, it, path, fn.kind, fn.name, fn.subs)
		if err := file.Children().Add(it); err != nil {
			tc.g.api.Log().Warn("the test cannot be added", "test", fn.name, "err", err)
		}
	}
	return file
}

// addSubtests makes the items of the subtests found in the source below the item. tc.mu is held.
func (tc *testCtl) addSubtests(pkg *testPkg, parent *extension.TestItem, path string, kind int, parentName string, subs []goSubtest) {
	uri := fileURI(path)
	for _, s := range subs {
		name := parentName + "/" + s.name
		it := tc.newItem(parent.ID()+"/"+s.name, s.name, uri, testItemInfo{role: roleFunc, pkg: pkg, file: path, name: name, kind: kind})
		rng := extension.Range{Start: extension.Position{Line: s.startLine, Col: s.startCol}, End: extension.Position{Line: s.endLine, Col: s.endCol}}
		it.Update(func(d *extension.TestItemData) { d.Range = &rng })
		pkg.byName[name] = it
		tc.addSubtests(pkg, it, path, kind, name, s.subs)
		if err := parent.Children().Add(it); err != nil {
			tc.g.api.Log().Warn("the subtest cannot be added", "test", name, "err", err)
		}
	}
}

// unindexLocked forgets the item and everything below it. tc.mu is held.
func (tc *testCtl) unindexLocked(pkg *testPkg, it *extension.TestItem) {
	for _, c := range it.Children().All() {
		tc.unindexLocked(pkg, c)
	}
	if info, ok := tc.info[it]; ok && info.role == roleFunc && pkg.byName[info.name] == it {
		delete(pkg.byName, info.name)
	}
	delete(tc.info, it)
	if tc.byID[it.ID()] == it {
		delete(tc.byID, it.ID())
	}
}

// removeFile takes the tests of a file out of the tree, and the package when it has no other file.
func (tc *testCtl) removeFile(path string) {
	dir := filepath.Dir(path)
	tc.mu.Lock()
	defer tc.mu.Unlock()
	pkg := tc.pkgs[dir]
	if pkg == nil || pkg.files[path] == nil {
		return
	}
	old := pkg.files[path]
	tc.unindexLocked(pkg, old.item)
	pkg.item.Children().Delete(old.item.ID())
	delete(pkg.files, path)
	if len(pkg.files) > 0 {
		return
	}
	tc.ctl.Items().Delete(pkg.item.ID())
	tc.unindexLocked(pkg, pkg.item)
	delete(tc.pkgs, dir)
	if tc.byImport[pkg.importPath] == pkg {
		delete(tc.byImport, pkg.importPath)
	}
}

// ---- looking up ----

// packageByImport is the package with the import path go test reported, or nil.
func (tc *testCtl) packageByImport(path string) *testPkg {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.byImport[path]
}

// testItem is the item of the test with the whole name in the package. A test the source did not show (a table-driven subtest, a
// function in a file with build constraints) is made, below its parent, with the ids of the ones the source gave.
func (tc *testCtl) testItem(pkg *testPkg, name string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.testItemLocked(pkg, name)
}

func (tc *testCtl) testItemLocked(pkg *testPkg, name string) *extension.TestItem {
	if it := pkg.byName[name]; it != nil {
		return it
	}
	kind := kindOfName(name)
	i := strings.LastIndex(name, "/")
	if i < 0 {
		it := tc.newItem(testID(fileURI(pkg.dir), kindName(kind), name), name, "", testItemInfo{role: roleFunc, pkg: pkg, name: name, kind: kind})
		pkg.byName[name] = it
		if err := pkg.item.Children().Add(it); err != nil {
			tc.g.api.Log().Warn("the test cannot be added", "test", name, "err", err)
		}
		return it
	}
	parent := tc.testItemLocked(pkg, name[:i])
	pinfo := tc.info[parent]
	it := tc.newItem(parent.ID()+"/"+name[i+1:], name[i+1:], parent.Data().URI, testItemInfo{role: roleFunc, pkg: pkg, file: pinfo.file, name: name, kind: kind})
	pkg.byName[name] = it
	if err := parent.Children().Add(it); err != nil {
		tc.g.api.Log().Warn("the subtest cannot be added", "test", name, "err", err)
	}
	return it
}

// itemFor is the item of the function of the package folder with the name, or nil.
func (tc *testCtl) itemFor(dir, name string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if pkg := tc.pkgs[dir]; pkg != nil {
		return pkg.byName[name]
	}
	return nil
}

// fileItemOf is the item of the test file, or nil when it has no tests.
func (tc *testCtl) fileItemOf(path string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if pkg := tc.pkgs[filepath.Dir(path)]; pkg != nil && pkg.files[path] != nil {
		return pkg.files[path].item
	}
	return nil
}

// packageItemOf is the item of the package in the folder, or nil when it has no tests.
func (tc *testCtl) packageItemOf(dir string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if pkg := tc.pkgs[dir]; pkg != nil {
		return pkg.item
	}
	return nil
}

// loadDir reads the test files of a folder, for a command on a package nothing has looked at yet.
func (tc *testCtl) loadDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		tc.g.api.Log().Warn("the folder cannot be read", "dir", dir, "err", err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
			tc.loadFile(filepath.Join(dir, e.Name()))
		}
	}
}

// ---- keeping up ----

// queue asks the worker to read the files again (all of them with full), and starts it when it is not running. It may be called from
// any goroutine.
func (tc *testCtl) queue(paths []string, full bool) {
	tc.wmu.Lock()
	for _, p := range paths {
		tc.pending[p] = true
	}
	tc.full = tc.full || full
	if tc.working {
		tc.wmu.Unlock()
		return
	}
	tc.working = true
	tc.wmu.Unlock()
	tc.g.closers.Add(1)
	go func() {
		defer tc.g.closers.Done()
		for {
			tc.wmu.Lock()
			paths, full := tc.pending, tc.full
			tc.pending, tc.full = map[string]bool{}, false
			if len(paths) == 0 && !full {
				tc.working = false
				tc.wmu.Unlock()
				return
			}
			tc.wmu.Unlock()
			if tc.g.ctx.Err() != nil {
				tc.wmu.Lock()
				tc.working = false
				tc.wmu.Unlock()
				return
			}
			if full {
				tc.discoverAll(tc.g.ctx)
				continue
			}
			for p := range paths {
				tc.loadFile(p)
			}
		}
	}()
}

// idle tells whether the worker has nothing to do (the tests wait for it).
func (tc *testCtl) idle() bool {
	tc.wmu.Lock()
	defer tc.wmu.Unlock()
	return !tc.working && len(tc.pending) == 0 && !tc.full
}

// filesChanged is the subscriber of TopicFilesChanged: a test file or a go.mod that changed is read again.
func (g *golang) filesChanged(ev extension.Event) {
	if g.tests == nil {
		return
	}
	var fc extension.FilesChanged
	if ev.Decode(&fc) != nil {
		return
	}
	root := g.api.Scope().Folder
	if root == "" {
		return
	}
	var paths []string
	full := false
	for _, c := range fc.Changes {
		switch c.Kind {
		case extension.FileRescan, extension.FileListed:
			full = true
			continue
		}
		for _, rel := range []string{c.Path, c.OldPath} {
			switch {
			case rel == "":
			case strings.HasSuffix(rel, "_test.go"):
				paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
			case rel == "go.mod" || strings.HasSuffix(rel, "/go.mod"):
				full = true
			}
		}
	}
	if full || len(paths) > 0 {
		g.tests.queue(paths, full)
	}
}
