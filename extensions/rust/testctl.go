package rust

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The controller of Rust's tests (SPEC §10.32 "The providers", Rust; "As built (step 26.7)"): the tree of packages, targets (the library,
// each binary, each integration test, the documentation tests), modules and tests that the Testing view shows. The names come from
// `cargo test -p <package> <target> -- --list --format terse`, the places from a light scan of the sources (testsrc.go) and, for the
// documentation tests, from their names. The tests are found when the pack starts in a folder with a Cargo.toml (the Testing view never asks a controller for them), and again after a Rust file or a
// Cargo.toml is saved, created or deleted.

// The names of the controller and of its two run profiles.
const (
	// TestControllerID is the id of the controller and TestControllerLabel its name.
	TestControllerID    = "cargo"
	TestControllerLabel = "Cargo"
	runProfileLabel     = "cargo test"
	debugProfileLabel   = "cargo test (debug)"
	// RunnablesExtraArgsSetting holds the arguments added to the cargo of a run (rust-analyzer's setting of that name).
	RunnablesExtraArgsSetting = "rust-analyzer.runnables.extraArgs"
)

// The roles of an item of the tree.
const (
	rolePackage = iota
	roleTarget
	roleModule
	roleTest
)

// discoveryTimeout ends a discovery that does not finish; saveDebounce is how long the tests wait for more saves and file events before
// they are found again.
const (
	discoveryTimeout = 10 * time.Minute
	saveDebounce     = 400 * time.Millisecond
)

// targetKind is what a cargo target is, for the arguments that select it.
type targetKind int

const (
	kindLib targetKind = iota
	kindBin
	kindTest
	kindExample
	kindDoc
)

// testTarget is a target of cargo that has tests.
type testTarget struct {
	pkg, name string
	kind      targetKind
	// artifactKind is the kind cargo names the target by in its JSON ("lib", "bin", "test", "example"), used to find the test binary of a Debug.
	artifactKind string
	// manifest is the Cargo.toml of the package, dir its folder and src the root file of the target.
	manifest, dir, src string
}

// key identifies the target among those of its package.
func (t testTarget) key() string {
	switch t.kind {
	case kindLib:
		return "lib"
	case kindBin:
		return "bin:" + t.name
	case kindTest:
		return "test:" + t.name
	case kindExample:
		return "example:" + t.name
	}
	return "doc"
}

// label is what the Testing view shows for the target.
func (t testTarget) label() string {
	switch t.kind {
	case kindLib:
		return "lib"
	case kindBin:
		return "bin " + t.name
	case kindTest:
		return "test " + t.name
	case kindExample:
		return "example " + t.name
	}
	return "doc tests"
}

// flags are the arguments of `cargo test` that select the target.
func (t testTarget) flags() []string {
	switch t.kind {
	case kindLib:
		return []string{"--lib"}
	case kindBin:
		return []string{"--bin", t.name}
	case kindTest:
		return []string{"--test", t.name}
	case kindExample:
		return []string{"--example", t.name}
	}
	return []string{"--doc"}
}

// testItemInfo says what an item of the tree is.
type testItemInfo struct {
	role int
	// target is the target of a target, module or test; name is the name libtest reports a test by ("tests::adds", or "src/lib.rs - add
	// (line 5)" for a documentation test).
	target testTarget
	name   string
}

// testCtl is the controller and the state behind its items. Everything in it is safe for concurrent use.
type testCtl struct {
	r        *rust
	ctl      extension.TestController
	profiles map[extension.TestRunKind]extension.TestRunProfile

	mu   sync.Mutex
	info map[*extension.TestItem]testItemInfo
	byID map[string]*extension.TestItem
	// byRun finds the item of a test a run reported, by the id of its target and its name.
	byRun map[string]*extension.TestItem
	// root is the root folder of the Cargo workspace, the folder the paths of the compiler's messages are relative to.
	root string

	// discMu serializes discoveries; the rest is the queue of the worker.
	discMu  sync.Mutex
	wmu     sync.Mutex
	pending bool
	loud    bool
	working bool
	timer   *time.Timer
}

// newTestCtl makes the controller and its profiles. It fails with ExtensionTestingUnavailable in a scope with no testing service.
func newTestCtl(r *rust) (*testCtl, error) {
	ctl, err := r.api.Tests().CreateTestController(TestControllerID, TestControllerLabel)
	if err != nil {
		return nil, err
	}
	tc := &testCtl{
		r: r, ctl: ctl, profiles: map[extension.TestRunKind]extension.TestRunProfile{}, info: map[*extension.TestItem]testItemInfo{},
		byID: map[string]*extension.TestItem{}, byRun: map[string]*extension.TestItem{},
	}
	for _, pr := range []struct {
		label string
		kind  extension.TestRunKind
	}{{runProfileLabel, extension.TestKindRun}, {debugProfileLabel, extension.TestKindDebug}} {
		opts := extension.TestRunProfileOptions{IsDefault: true, SupportsContinuousRun: pr.kind == extension.TestKindRun, ContinuousTrigger: tc.continuousTrigger}
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

// continuousTrigger says that a change of a Rust file or a Cargo.toml of the folder of the workspace runs the continuous run again.
func (tc *testCtl) continuousTrigger(_ *extension.TestItem, uri string) bool {
	path, ok := filePath(uri)
	return ok && isRustSource(path) && within(path, tc.r.api.Scope().Folder)
}

// isRustSource tells whether a path is a Rust file or a Cargo.toml: what a change of finds the tests again.
func isRustSource(path string) bool {
	return strings.HasSuffix(path, ".rs") || filepath.Base(path) == "Cargo.toml"
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

// resolvePath is the absolute path of a file a message names: the compiler's paths are relative to the root of the workspace, those of a
// documentation test sometimes to the package. The first that exists wins; a path that exists in neither is joined to the root.
func (tc *testCtl) resolvePath(file, pkgDir string) string {
	tc.mu.Lock()
	root := tc.root
	tc.mu.Unlock()
	return resolveIn(root, file, pkgDir)
}

// resolveIn is resolvePath for a known root of the workspace.
func resolveIn(root, file, pkgDir string) string {
	if filepath.IsAbs(file) {
		return file
	}
	for _, base := range []string{root, pkgDir} {
		if base == "" {
			continue
		}
		if p := filepath.Join(base, file); fileExists(p) {
			return p
		}
	}
	if root != "" {
		return filepath.Join(root, file)
	}
	return filepath.Join(pkgDir, file)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ---- the programs ----

// cargoEnv is the environment of a cargo of the provider: Emetos's, without colors (they would break the parser of the output).
func cargoEnv() []string {
	return append(os.Environ(), "CARGO_TERM_COLOR=never", "CARGO_TERM_PROGRESS_WHEN=never")
}

// extraArgs are the arguments rust-analyzer.runnables.extraArgs adds to cargo.
func (tc *testCtl) extraArgs() []string {
	args, err := extension.SettingValue[[]string](tc.r.api.Settings(), RunnablesExtraArgsSetting)
	if err != nil {
		return nil
	}
	return args
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
	tc.timer = time.AfterFunc(tc.r.testDebounce, tc.startWorker)
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
	r := tc.r
	r.work.Add(1)
	go func() {
		defer r.work.Done()
		for {
			tc.wmu.Lock()
			if !tc.pending || r.ctx.Err() != nil {
				tc.pending, tc.working = false, false
				tc.wmu.Unlock()
				return
			}
			loud := tc.loud
			tc.pending, tc.loud = false, false
			tc.wmu.Unlock()
			tc.discover(r.ctx, loud)
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

// testTargets are the targets of the workspace that have tests: the library, the binaries, the integration tests and the examples that
// `cargo test` builds with the test harness, and the documentation tests of a library.
func testTargets(meta cargoMetadata) []testTarget {
	var out []testTarget
	for _, pkg := range meta.Packages {
		dir := filepath.Dir(pkg.ManifestPath)
		var doc *testTarget
		for _, t := range pkg.Targets {
			base := testTarget{pkg: pkg.Name, name: t.Name, manifest: pkg.ManifestPath, dir: dir, src: t.SrcPath}
			switch {
			case t.hasKind("custom-build", "bench"):
				continue
			case t.hasKind(libraryKinds...):
				if t.Test {
					base.kind, base.artifactKind = kindLib, t.Kind[0]
					out = append(out, base)
				}
				if t.Doctest && t.hasKind("lib", "rlib", "proc-macro") {
					d := base
					d.kind, d.artifactKind = kindDoc, ""
					doc = &d
				}
			case t.hasKind("bin") && t.Test:
				base.kind, base.artifactKind = kindBin, "bin"
				out = append(out, base)
			case t.hasKind("test") && t.Test:
				base.kind, base.artifactKind = kindTest, "test"
				out = append(out, base)
			case t.hasKind("example") && t.Test:
				base.kind, base.artifactKind = kindExample, "example"
				out = append(out, base)
			}
		}
		if doc != nil {
			out = append(out, *doc)
		}
	}
	return out
}

// listedTarget is a target with the tests `--list` found in it.
type listedTarget struct {
	testTarget
	names []string
	// errText is why the tests could not be listed (the target does not compile); empty when they could.
	errText string
}

var listRe = regexp.MustCompile(`^(.+): (test|benchmark)$`)

// discover finds the tests with cargo and makes the tree what it found. With loud a failure is told to the user.
func (tc *testCtl) discover(ctx context.Context, loud bool) {
	tc.discMu.Lock()
	defer tc.discMu.Unlock()
	r := tc.r
	folder := r.api.Scope().Folder
	if folder == "" || !findCargoToml(folder) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	meta, err := readMetadata(ctx, folder, nil)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.api.Log().Debug("the Rust tests cannot be found", "err", err)
		if loud {
			r.api.Notifications().Error("The Rust tests cannot be found: " + describe(err))
		}
		return
	}
	cargo, err := findCargo()
	if err != nil {
		return
	}
	var listed []listedTarget
	for _, t := range testTargets(meta) {
		if ctx.Err() != nil {
			return
		}
		lt := listedTarget{testTarget: t}
		args := append([]string{"test", "-p", t.pkg}, t.flags()...)
		args = append(args, tc.extraArgs()...)
		args = append(args, "--", "--list", "--format", "terse")
		var all []string
		runErr := r.runner(ctx, cmdSpec{Program: cargo, Args: args, Dir: folder, Env: cargoEnv()}, func(line string) {
			if m := listRe.FindStringSubmatch(line); m != nil {
				if m[2] == "test" {
					lt.names = append(lt.names, m[1])
				}
				return
			}
			all = append(all, line)
		})
		if ctx.Err() != nil {
			return
		}
		if runErr != nil && len(lt.names) == 0 {
			lt.errText = firstError(all)
			r.api.Log().Debug("the tests of a target cannot be listed", "package", t.pkg, "target", t.label(), "err", runErr)
		}
		listed = append(listed, lt)
	}
	tc.apply(meta, listed)
}

// firstError is the first line of a cargo output that is an error, else its last line.
func firstError(lines []string) string {
	last := ""
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "error") {
			return clip(l, 200)
		}
		if l != "" {
			last = l
		}
	}
	if last == "" {
		return "cargo failed"
	}
	return clip(last, 200)
}

// clip shortens a text to n bytes.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}

// ---- the tree ----

// apply makes the tree what the discovery found: items that are still there are kept (with their results), the others go.
func (tc *testCtl) apply(meta cargoMetadata, listed []listedTarget) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.root = meta.WorkspaceRoot
	seen := map[string]bool{}
	byRun := map[string]*extension.TestItem{}
	pkgs := map[string]*extension.TestItem{}
	for _, lt := range listed {
		pkg := pkgs[lt.pkg]
		if pkg == nil {
			pkg = tc.ensure(tc.ctl.Items(), "cargo:"+lt.pkg, lt.pkg, lt.manifest, 0, testItemInfo{role: rolePackage, target: lt.testTarget}, seen, "")
			pkgs[lt.pkg] = pkg
		}
		tid := "cargo:" + lt.pkg + ":" + lt.key()
		src := lt.src
		if lt.kind == kindDoc || src == "" {
			src = lt.manifest
		}
		tgt := tc.ensure(pkg.Children(), tid, lt.label(), src, 0, testItemInfo{role: roleTarget, target: lt.testTarget}, seen, lt.errText)
		var idx srcIndex
		if lt.kind != kindDoc && lt.src != "" {
			idx = scanTarget(lt.src)
		}
		for _, name := range lt.names {
			byRun[tid+"|"+name] = tc.addTest(tgt, tid, lt, idx, name, seen)
		}
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
		tc.r.api.Log().Warn("a test cannot be added to the tree", "id", id, "err", err)
	}
	return it
}

var docNameRe = regexp.MustCompile(`^(.*) - (.*) \(line (\d+)\)$`)

// addTest puts a test of the target in the tree, below the items of its modules, and returns its item. tc.mu is held.
func (tc *testCtl) addTest(tgt *extension.TestItem, tid string, lt listedTarget, idx srcIndex, name string, seen map[string]bool) *extension.TestItem {
	info := testItemInfo{role: roleTest, target: lt.testTarget, name: name}
	if lt.kind == kindDoc {
		file, label, line := lt.src, name, 0
		if m := docNameRe.FindStringSubmatch(name); m != nil {
			file, line = resolveIn(tc.root, m[1], lt.dir), atoi(m[3])
			label = m[2] + " (line " + m[3] + ")"
		}
		it := tc.ensure(tgt.Children(), tid+"/test/"+name, label, file, line, info, seen, "")
		return it
	}
	parts := strings.Split(name, "::")
	cur := tgt
	for i := range parts[:len(parts)-1] {
		path := strings.Join(parts[:i+1], "::")
		place := idx.mods[path]
		file := lt.src
		if place.File != "" {
			file = place.File
		}
		cur = tc.ensure(cur.Children(), tid+"/mod/"+path, parts[i], file, place.Line, testItemInfo{role: roleModule, target: lt.testTarget, name: path}, seen, "")
	}
	place := idx.tests[name]
	file := lt.src
	if place.File != "" {
		file = place.File
	}
	return tc.ensure(cur.Children(), tid+"/test/"+name, parts[len(parts)-1], file, place.Line, info, seen, "")
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// itemForRun is the item of the test a run of the target reported by name.
func (tc *testCtl) itemForRun(tid, name string) *extension.TestItem {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.byRun[tid+"|"+name]
}

// ---- files changing ----

// documentSaved finds the tests again when the saved file is a Rust file or a Cargo.toml and the Testing view has asked for the tests.
func (r *rust) documentSaved(ev extension.Event) {
	if r.tests == nil {
		return
	}
	var saved extension.DocumentSaved
	if ev.Decode(&saved) != nil {
		return
	}
	path, ok := filePath(saved.URI)
	if !ok || !isRustSource(path) || !within(path, r.api.Scope().Folder) {
		return
	}
	r.tests.requestDiscovery(false)
}

// filesChanged finds the tests again when a Rust file is created, deleted or moved.
func (r *rust) filesChanged(ev extension.Event) {
	if r.tests == nil {
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
		if c.Kind == extension.FileRescan || isRustSource(c.Path) || isRustSource(c.OldPath) {
			r.tests.requestDiscovery(false)
			return
		}
	}
}

// registerTests makes the controller and follows the files. A window with no Testing view has no controller.
func (r *rust) registerTests() error {
	tc, err := newTestCtl(r)
	if err != nil {
		if appErr, ok := errorsFrom(err); !ok || appErr != "ExtensionTestingUnavailable" {
			return err
		}
		r.api.Log().Debug("the Testing view is not available; the Rust tests cannot be run")
		return nil
	}
	r.tests = tc
	// the Testing view never asks a controller for its tests, so they are found at the start (and again when files change)
	if folder := r.api.Scope().Folder; folder != "" && findCargoToml(folder) {
		tc.requestDiscovery(false)
	}
	for topic, fn := range map[string]func(extension.Event){extension.TopicDocumentSaved: r.documentSaved, extension.TopicFilesChanged: r.filesChanged} {
		if _, err := r.api.Events().Subscribe(topic, fn); err != nil {
			return err
		}
	}
	return nil
}

// errorsFrom is the tag of an error of the errors package.
func errorsFrom(err error) (string, bool) {
	appErr, ok := errors.FromError(err)
	if !ok {
		return "", false
	}
	return appErr.Tag, true
}
