package gitscm

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The decoration of every status, from the methods of VS Code's Resource (getStatusLetter, getStatusText, getStatusColor, priority and
// resourceDecoration).
func TestEveryStatusHasItsDecoration(t *testing.T) {
	tests := []struct {
		status    git.Status
		letter    string
		tooltip   string
		color     string
		priority  int
		propagate bool
	}{
		{git.StatusIndexModified, "M", "Index Modified", "stageModified", 2, true},
		{git.StatusModified, "M", "Modified", "modified", 2, true},
		{git.StatusIndexAdded, "A", "Index Added", "added", 1, true},
		{git.StatusIndexDeleted, "D", "Index Deleted", "stageDeleted", 1, false},
		{git.StatusDeleted, "D", "Deleted", "deleted", 1, false},
		{git.StatusIndexRenamed, "R", "Index Renamed", "renamed", 1, true},
		{git.StatusIndexCopied, "C", "Index Copied", "renamed", 2, true},
		{git.StatusUntracked, "U", "Untracked", "untracked", 1, true},
		{git.StatusIgnored, "I", "Ignored", "ignored", 3, true},
		{git.StatusIntentToAdd, "A", "Intent to Add", "added", 1, true},
		{git.StatusIntentToRename, "R", "Intent to Rename", "renamed", 1, true},
		{git.StatusTypeChanged, "T", "Type Changed", "modified", 2, true},
		{git.StatusBothDeleted, "!", "Conflict: Both Deleted", "conflicting", 4, true},
		{git.StatusAddedByUs, "!", "Conflict: Added By Us", "conflicting", 4, true},
		{git.StatusDeletedByThem, "!", "Conflict: Deleted By Them", "conflicting", 4, true},
		{git.StatusAddedByThem, "!", "Conflict: Added By Them", "conflicting", 4, true},
		{git.StatusDeletedByUs, "!", "Conflict: Deleted By Us", "conflicting", 4, true},
		{git.StatusBothAdded, "!", "Conflict: Both Added", "conflicting", 4, true},
		{git.StatusBothModified, "!", "Conflict: Both Modified", "conflicting", 4, true},
	}
	// Ported from VS Code: extensions/git/src/repository.ts (Resource)
	for _, tc := range tests {
		t.Run(tc.tooltip, func(t *testing.T) {
			d := decorationOf(tc.status)
			want := extension.FileDecoration{
				Badge: tc.letter, Tooltip: tc.tooltip, Color: "gitDecoration." + tc.color + "ResourceForeground",
				Propagate: tc.propagate, Priority: tc.priority,
			}
			if d != want {
				t.Errorf("decoration = %+v, want %+v", d, want)
			}
			if d.IsEmpty() || !extension.ValidBadge(d.Badge) {
				t.Errorf("the decoration %+v would be refused by the host", d)
			}
		})
	}
}

func TestDecorationDataCollectsTheGroupsInVSCodesOrder(t *testing.T) {
	root := "/r"
	g := groups{
		index:     []resource{{path: "/r/a.go", status: git.StatusIndexModified}, {path: "/r/new.go", status: git.StatusIndexRenamed, from: "/r/old.go"}},
		untracked: []resource{{path: "/r/u.txt", status: git.StatusUntracked}},
		working:   []resource{{path: "/r/a.go", status: git.StatusModified}, {path: "/r/gone.go", status: git.StatusDeleted}},
		merge:     []resource{{path: "/r/c.go", status: git.StatusBothModified}},
	}
	got := decorationData(g, root, []string{"vendor/lib"})
	check := func(path string, want extension.FileDecoration) {
		t.Helper()
		if got[path] != want {
			t.Errorf("%s: %+v, want %+v", path, got[path], want)
		}
	}
	check("/r/a.go", decorationOf(git.StatusModified)) // the work tree wins over the index
	check("/r/new.go", decorationOf(git.StatusIndexRenamed))
	check("/r/old.go", decorationOf(git.StatusIndexRenamed)) // the old path of a rename is decorated too
	check("/r/u.txt", decorationOf(git.StatusUntracked))
	check("/r/gone.go", decorationOf(git.StatusDeleted))
	check("/r/c.go", decorationOf(git.StatusBothModified))
	check("/r/vendor/lib", submoduleDecoration)
	if len(got) != 7 {
		t.Errorf("%d decorations, want 7: %v", len(got), got)
	}
	// a file in two groups: the later group is the one that shows, and the merge group beats the work tree
	g = groups{working: []resource{{path: "/r/x", status: git.StatusModified}}, merge: []resource{{path: "/r/x", status: git.StatusBothAdded}}}
	if got := decorationData(g, root, nil)["/r/x"]; got != decorationOf(git.StatusBothAdded) {
		t.Errorf("x = %+v", got)
	}
}

func TestDecoPathTakesBothSpellings(t *testing.T) {
	tests := []struct {
		uri  string
		path string
		ok   bool
	}{
		{"file:///home/a/b.go", "/home/a/b.go", true},
		{"file:///home/a%20b/c.go", "/home/a b/c.go", true},
		{"file:///home/a b/c.go", "/home/a b/c.go", true},
		{"file:///100%/x", "/100%/x", true},
		{"git:/home/a", "", false},
		{"untitled:1", "", false},
	}
	for _, tc := range tests {
		path, ok := decoPath(tc.uri)
		if path != tc.path || ok != tc.ok {
			t.Errorf("decoPath(%q) = %q, %v; want %q, %v", tc.uri, path, ok, tc.path, tc.ok)
		}
	}
}

// ---- the files git ignores ----

// firedRegistration records what the provider fires.
type firedRegistration struct {
	mu    sync.Mutex
	fired [][]string
}

func (f *firedRegistration) Fire(uris ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fired = append(f.fired, slices.Clone(uris))
}
func (f *firedRegistration) Dispose() {}
func (f *firedRegistration) all() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.fired)
}

// fakeIgnoreRepo is a repository whose check-ignore says that every path is ignored (the repository of VS Code's test).
type fakeIgnoreRepo struct {
	mu      sync.Mutex
	checked [][]string
	ignored func(path string) bool
	err     error
}

func (r *fakeIgnoreRepo) CheckIgnoreFunc() func(context.Context, []string) ([]string, error) {
	return func(_ context.Context, paths []string) ([]string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.checked = append(r.checked, slices.Clone(paths))
		if r.err != nil {
			return nil, r.err
		}
		var out []string
		for _, p := range paths {
			if r.ignored == nil || r.ignored(p) {
				out = append(out, p)
			}
		}
		return out, nil
	}
}

func (r *fakeIgnoreRepo) calls() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.checked)
}

// fakeIgnoreHost gives the provider a repository for the paths below its root, the fake clock and a UI thread that runs what was
// posted when the test says so.
type fakeIgnoreHost struct {
	root  string
	repo  *fakeIgnoreRepo
	clock *fakeClock
	posts chan func()
}

func (h *fakeIgnoreHost) RepositoryOf(path string) ignoreRepo {
	if git.Within(path, h.root) {
		return h.repo
	}
	return nil
}
func (h *fakeIgnoreHost) AfterFunc(d time.Duration, f func()) func() { return h.clock.AfterFunc(d, f) }
func (h *fakeIgnoreHost) Post(f func())                              { h.posts <- f }

// run runs the n functions posted (the goroutine of check-ignore posts one), waiting a little for them.
func (h *fakeIgnoreHost) run(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case f := <-h.posts:
			f()
		case <-time.After(5 * time.Second):
			t.Fatalf("waited for post %d of %d", i+1, n)
		}
	}
}

func newFakeIgnore(t *testing.T) (*ignoreProvider, *fakeIgnoreHost, *firedRegistration, *[]string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repository")
	host := &fakeIgnoreHost{root: root, repo: &fakeIgnoreRepo{}, clock: &fakeClock{}, posts: make(chan func(), 16)}
	var logs []string
	p := newIgnoreProvider(context.Background(), host, func(msg string, args ...any) { logs = append(logs, msg) })
	p.reg = &firedRegistration{}
	return p, host, p.reg.(*firedRegistration), &logs
}

// Ported from VS Code: extensions/git/src/test/decorationProvider.test.ts ('checks pending paths once the debounce elapses'). The
// provider answers at once here, so the answer is a Fire once git has said.
func TestPortedIgnoreChecksPendingPathsOnceTheDebounceElapses(t *testing.T) {
	p, host, reg, _ := newFakeIgnore(t)
	path := filepath.Join(host.root, "ignored.txt")
	uri := fileURI(path)
	if d, ok := p.Decoration(uri); ok {
		t.Fatalf("decoration %+v before git was asked", d)
	}
	host.clock.Advance(499 * time.Millisecond)
	if got := host.repo.calls(); len(got) != 0 {
		t.Fatalf("git was asked after 499 ms: %v", got)
	}
	host.clock.Advance(time.Millisecond)
	host.run(t, 2) // the timer hands the check to the UI thread, and the check hands its answer there
	if got, want := host.repo.calls(), [][]string{{path}}; !reflect.DeepEqual(got, want) {
		t.Errorf("checked paths = %v, want %v", got, want)
	}
	if got, want := reg.all(), [][]string{{uri}}; !reflect.DeepEqual(got, want) {
		t.Errorf("fired = %v, want %v", got, want)
	}
	if d, ok := p.Decoration(uri); !ok || d.Color != "gitDecoration.ignoredResourceForeground" || d.Badge != "" {
		t.Errorf("decoration = %+v, %v; want the ignored color and no badge", d, ok)
	}
	if d, ok := p.Decoration(uri + "x"); ok {
		t.Errorf("a path nobody asked about is decorated: %+v", d)
	}
}

// Ported from VS Code: extensions/git/src/test/decorationProvider.test.ts ('does not run check-ignore for a repository closed before
// the debounce elapses').
func TestPortedIgnoreDoesNotRunCheckIgnoreForARepositoryClosedBeforeTheDebounceElapses(t *testing.T) {
	p, host, _, _ := newFakeIgnore(t)
	uri := fileURI(filepath.Join(host.root, "ignored.txt"))
	if _, ok := p.Decoration(uri); ok {
		t.Fatal("decorated before git was asked")
	}
	p.repositoryClosed(host.repo)
	host.clock.Advance(time.Second)
	if got := host.repo.calls(); len(got) != 0 {
		t.Errorf("git was asked for a closed repository: %v", got)
	}
	if d, ok := p.Decoration(uri + "2"); ok {
		t.Errorf("decoration %+v", d)
	}
}

func TestIgnoreProviderBatchesAndKeepsAnswers(t *testing.T) {
	p, host, reg, logs := newFakeIgnore(t)
	host.repo.ignored = func(path string) bool { return strings.HasSuffix(path, ".log") }
	names := []string{"a.log", "b.go", "c.log"}
	for _, n := range names {
		p.Decoration(fileURI(filepath.Join(host.root, n)))
		p.Decoration(fileURI(filepath.Join(host.root, n))) // asked twice: queued once
	}
	p.Decoration(fileURI(filepath.Join(t.TempDir(), "elsewhere.log"))) // not in the repository: never asked
	if host.clock.Pending() != 1 {
		t.Fatalf("%d timers, want one for the batch", host.clock.Pending())
	}
	host.clock.Advance(500 * time.Millisecond)
	host.run(t, 2)
	calls := host.repo.calls()
	if len(calls) != 1 {
		t.Fatalf("%d git calls, want 1: %v", len(calls), calls)
	}
	sort.Strings(calls[0])
	if want := []string{filepath.Join(host.root, "a.log"), filepath.Join(host.root, "b.go"), filepath.Join(host.root, "c.log")}; !reflect.DeepEqual(calls[0], want) {
		t.Errorf("batch = %v, want %v", calls[0], want)
	}
	fired := reg.all()
	if len(fired) != 1 || len(fired[0]) != 2 {
		t.Errorf("fired = %v; want the two .log files in one call", fired)
	}
	// a path that git said no to is known, and not asked again
	p.Decoration(fileURI(filepath.Join(host.root, "b.go")))
	if host.clock.Pending() != 0 {
		t.Error("a known path started a timer")
	}
	// a change of the ignore rules forgets everything and fires all
	p.reset()
	if all := reg.all(); len(all[len(all)-1]) != 0 {
		t.Errorf("reset fired %v, want no URIs (everything)", all[len(all)-1])
	}
	if _, ok := p.Decoration(fileURI(filepath.Join(host.root, "a.log"))); ok {
		t.Error("an answer survived the reset")
	}
	if len(*logs) != 0 {
		t.Errorf("logged %v", *logs)
	}
}

func TestIgnoreProviderLogsAFailureExceptForSubmodules(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		logged bool
	}{
		{"a failure is logged", errors.New("GitCommandFailed", "boom"), true},
		{"a path in a submodule is not", errors.New("IsInSubmodule", "in submodule"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, host, reg, logs := newFakeIgnore(t)
			host.repo.err = tc.err
			p.Decoration(fileURI(filepath.Join(host.root, "x")))
			host.clock.Advance(time.Second)
			host.run(t, 2)
			if got := len(*logs) > 0; got != tc.logged {
				t.Errorf("logged = %v (%v), want %v", got, *logs, tc.logged)
			}
			if len(reg.all()) != 0 {
				t.Errorf("fired %v after a failure", reg.all())
			}
		})
	}
}

func TestDisposedIgnoreProviderDoesNothing(t *testing.T) {
	p, host, _, _ := newFakeIgnore(t)
	p.Decoration(fileURI(filepath.Join(host.root, "x")))
	p.dispose()
	host.clock.Advance(time.Second)
	if len(host.repo.calls()) != 0 {
		t.Error("git was asked by a disposed provider")
	}
	if _, ok := p.Decoration(fileURI(filepath.Join(host.root, "y"))); ok {
		t.Error("a disposed provider decorated")
	}
}

// ---- the real extension over repositories of gittest ----

// decoOf is the decoration of a path as the host would get it from the providers of the extension.
func (r *rig) decoOf(path string) (extension.FileDecoration, bool) {
	r.t.Helper()
	return r.api.FileDecoration(fileURI(path))
}

func TestTheProviderFollowsTheStatusOfARealRepository(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("tracked.go", "one")
	repo.Write("src/inner/deep.go", "one")
	repo.Write("src/keep.go", "one")
	repo.Write("staged.go", "one")
	repo.Write("gone.go", "one")
	repo.Write("old.txt", "some text that git can match when it is renamed\nmore\nmore\nmore\n")
	repo.Commit("initial commit")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rep := r.repo(repo.Dir)
	if got := r.api.FileDecorationProviders(); len(got) != 2 {
		t.Fatalf("%d providers (the ignored files and the repository), want 2", len(got))
	}
	if _, ok := r.decoOf(repo.Path("tracked.go")); ok {
		t.Fatal("a clean file is decorated")
	}

	repo.Write("tracked.go", "two")
	repo.Write("src/inner/deep.go", "two")
	repo.Write("staged.go", "two")
	repo.Git("add", "staged.go")
	repo.Remove("gone.go")
	repo.Write("brand-new.txt", "x")
	repo.Git("mv", "old.txt", "renamed.txt")
	r.refresh(rep)

	tests := []struct {
		path      string
		letter    string
		tooltip   string
		color     string
		propagate bool
	}{
		{"tracked.go", "M", "Modified", "modified", true},
		{"src/inner/deep.go", "M", "Modified", "modified", true},
		{"staged.go", "M", "Index Modified", "stageModified", true},
		{"gone.go", "D", "Deleted", "deleted", false},
		{"brand-new.txt", "U", "Untracked", "untracked", true},
		{"renamed.txt", "R", "Index Renamed", "renamed", true},
		{"old.txt", "R", "Index Renamed", "renamed", true},
	}
	for _, tc := range tests {
		d, ok := r.decoOf(repo.Path(tc.path))
		want := extension.FileDecoration{Badge: tc.letter, Tooltip: tc.tooltip, Color: "gitDecoration." + tc.color + "ResourceForeground", Propagate: tc.propagate, Priority: decorationOf(statusOfLetter(tc.tooltip)).Priority}
		if !ok || d != want {
			t.Errorf("%s: %+v, %v; want %+v", tc.path, d, ok, want)
		}
	}
	if _, ok := r.decoOf(repo.Path("src/keep.go")); ok {
		t.Error("an unchanged file next to changed ones is decorated")
	}

	// the extension fired the files that changed, once
	fired := r.firedURIs()
	for _, tc := range tests {
		if !slices.Contains(fired, fileURI(repo.Path(tc.path))) {
			t.Errorf("%s was not fired (fired: %v)", tc.path, fired)
		}
	}
	if slices.Contains(fired, fileURI(repo.Path("src/keep.go"))) {
		t.Error("an unchanged file was fired")
	}

	// committing everything removes the decorations, and fires the files again so that the folders above them are drawn again
	before := len(fired)
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "second commit")
	r.refresh(rep)
	for _, tc := range tests {
		if d, ok := r.decoOf(repo.Path(tc.path)); ok {
			t.Errorf("%s is still decorated after the commit: %+v", tc.path, d)
		}
	}
	after := r.firedURIs()[before:]
	for _, tc := range tests {
		if !slices.Contains(after, fileURI(repo.Path(tc.path))) {
			t.Errorf("%s was not fired when it was committed", tc.path)
		}
	}
}

// statusOfLetter is the status that a test row names by its tooltip.
func statusOfLetter(tooltip string) git.Status {
	for s := git.StatusIndexModified; s <= git.StatusBothModified; s++ {
		if statusText(s) == tooltip {
			return s
		}
	}
	return git.StatusNone
}

// firedURIs lists every URI the providers of the extension fired, in order.
func (r *rig) firedURIs() []string {
	var out []string
	for _, p := range r.api.FileDecorationProviders() {
		for _, uris := range p.Fired() {
			out = append(out, uris...)
		}
	}
	return out
}

func TestGitDecorationsEnabledTurnsTheProvidersOffAndOn(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.go", "one")
	repo.Commit("initial commit")
	r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.decorations.enabled": false}})
	r.settle()
	rep := r.repo(repo.Dir)
	repo.Write("a.go", "two")
	r.refresh(rep)
	if got := len(r.api.FileDecorationProviders()); got != 1 {
		t.Fatalf("%d providers with the setting off, want only the one for ignored files", got)
	}
	if _, ok := r.decoOf(repo.Path("a.go")); ok {
		t.Error("decorated with git.decorations.enabled off")
	}
	r.api.SetSetting("git.decorations.enabled", true)
	r.x.settingsChanged()
	if d, ok := r.decoOf(repo.Path("a.go")); !ok || d.Badge != "M" {
		t.Errorf("after turning it on: %+v, %v", d, ok)
	}
	r.api.SetSetting("git.decorations.enabled", false)
	r.x.settingsChanged()
	if _, ok := r.decoOf(repo.Path("a.go")); ok {
		t.Error("still decorated after turning it off")
	}
	if got := len(r.api.FileDecorationProviders()); got != 1 {
		t.Errorf("%d providers after turning it off, want 1", got)
	}
}

func TestSubmodulesAreDecorated(t *testing.T) {
	e := gittest.NewEnv(t)
	sub := e.Init("sub")
	sub.Write("lib.go", "x")
	sub.Commit("sub commit")
	repo := e.Init("repo")
	repo.Write("a.go", "one")
	repo.Commit("initial commit")
	repo.Submodule("vendor/sub", sub)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rep := r.repo(repo.Dir)
	r.refresh(rep)
	d, ok := r.decoOf(repo.Path("vendor/sub"))
	if !ok || d != submoduleDecoration {
		t.Errorf("submodule: %+v, %v; want %+v", d, ok, submoduleDecoration)
	}
}

func TestIgnoredFilesAreAskedForInBatchesAndFired(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write(".gitignore", "*.log\nbuild/\n")
	repo.Write("a.go", "one")
	repo.Commit("initial commit")
	repo.Write("debug.log", "x")
	repo.Write("build/out.o", "x")
	repo.Write("keep.txt", "x")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	// the views ask for the rows on screen
	for _, name := range []string{"debug.log", "build", "build/out.o", "keep.txt", "a.go"} {
		if d, ok := r.x.ignores.Decoration(fileURI(repo.Path(name))); ok {
			t.Errorf("%s decorated before git was asked: %+v", name, d)
		}
	}
	before := len(r.firedURIs())
	r.clock.Advance(500 * time.Millisecond)
	r.api.WaitFor("git check-ignore", func() bool { r.api.Pump(); return len(r.firedURIs()) > before })
	for name, want := range map[string]bool{"debug.log": true, "build": true, "build/out.o": true, "keep.txt": false, "a.go": false} {
		d, ok := r.x.ignores.Decoration(fileURI(repo.Path(name)))
		if ok != want || (ok && d.Color != "gitDecoration.ignoredResourceForeground") {
			t.Errorf("%s: %+v, %v; ignored should be %v", name, d, ok, want)
		}
	}
	// the user saves the ignore file: everything is asked again
	repo.Write(".gitignore", "*.txt\n")
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(repo.Path(".gitignore")), Version: 2})
	if len(r.x.ignores.known) != 0 {
		t.Error("the answers survived the change of the ignore file")
	}
	r.x.ignores.Decoration(fileURI(repo.Path("keep.txt")))
	r.clock.Advance(500 * time.Millisecond)
	r.api.WaitFor("the second check-ignore", func() bool {
		r.api.Pump()
		d, ok := r.x.ignores.Decoration(fileURI(repo.Path("keep.txt")))
		return ok && d.Color == "gitDecoration.ignoredResourceForeground"
	})
}
