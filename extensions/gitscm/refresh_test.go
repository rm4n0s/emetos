package gitscm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// repoWithFile makes a trusted repository with one commit and a modified file, and the rig on it.
func repoWithChange(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("src/a.go", "package a\n")
	repo.Write("README.md", "# r\n")
	repo.Commit("Add the files")
	repo.Write("src/a.go", "package a // changed\n")
	o.folder = repo.Dir
	r := newRig(t, e, o)
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

func change(path string) extension.FileChange {
	return extension.FileChange{Kind: extension.FileChanged, Path: path}
}

func TestABurstOfFileChangesMakesOneStatus(t *testing.T) {
	r, repo, rep := repoWithChange(t, rigOptions{})
	watches := r.api.Watches()
	if len(watches) != 1 || watches[0].Path() != repo.Dir || !watches[0].Recursive() {
		t.Fatalf("watches = %+v", watches)
	}
	found := false
	for _, ex := range watches[0].Excludes() {
		found = found || ex == ".git/objects"
	}
	if !found {
		t.Errorf("the objects folder is watched: %v", watches[0].Excludes())
	}
	before := rep.refreshes
	var burst []extension.FileChange
	for i := 0; i < 1000; i++ {
		burst = append(burst, change(repo.Path(fmt.Sprintf("src/gen%d.go", i))))
	}
	watches[0].Fire(burst...)
	r.api.Pump()
	if r.clock.Pending() != 1 {
		t.Fatalf("%d timers pending, want 1", r.clock.Pending())
	}
	watches[0].Fire(change(repo.Path("src/a.go")))
	r.api.Pump()
	if r.clock.Pending() != 1 {
		t.Errorf("a second change in the window made %d timers", r.clock.Pending())
	}
	r.clock.Advance(DebounceDelay - time.Millisecond)
	r.api.Pump()
	if rep.refreshes != before || rep.refreshing {
		t.Fatal("the status ran before 500 ms")
	}
	r.clock.Advance(time.Millisecond)
	r.api.WaitFor("the one status", func() bool { r.api.Pump(); return rep.refreshes == before+1 && !rep.refreshing })
	// the next burst is a new window
	watches[0].Fire(change(repo.Path("src/a.go")))
	r.api.Pump()
	r.clock.Advance(DebounceDelay)
	r.api.WaitFor("the next status", func() bool { r.api.Pump(); return rep.refreshes == before+2 && !rep.refreshing })
}

func TestWhichChangesTriggerAStatus(t *testing.T) {
	r, repo, rep := repoWithChange(t, rigOptions{})
	repo.Write(".gitignore", "build/\n*.log\n!keep.log\n")
	repo.Write("nested/.gitignore", "tmp-*\n")
	tests := []struct {
		name string
		path string
		kind string
		want bool
	}{
		{"a file of the work tree", repo.Path("src/new.go"), extension.FileCreated, true},
		{"a deleted file", repo.Path("src/a.go"), extension.FileDeleted, true},
		{"the index", repo.Path(".git/index"), extension.FileChanged, true},
		{"HEAD", repo.Path(".git/HEAD"), extension.FileChanged, true},
		{"a ref", repo.Path(".git/refs/heads/main"), extension.FileChanged, true},
		{"the packed refs", repo.Path(".git/packed-refs"), extension.FileChanged, true},
		{"MERGE_HEAD", repo.Path(".git/MERGE_HEAD"), extension.FileCreated, true},
		{"the rebase folder", repo.Path(".git/rebase-merge/head-name"), extension.FileCreated, true},
		{"the configuration", repo.Path(".git/config"), extension.FileChanged, true},
		{"info/exclude", repo.Path(".git/info/exclude"), extension.FileChanged, true},
		{"a rescan", repo.Path("src"), extension.FileRescan, true},
		{"index.lock", repo.Path(".git/index.lock"), extension.FileCreated, false},
		{"a lock of a ref", repo.Path(".git/refs/heads/main.lock"), extension.FileCreated, false},
		{"an object", repo.Path(".git/objects/ab/cdef"), extension.FileCreated, false},
		{"the commit message", repo.Path(".git/COMMIT_EDITMSG"), extension.FileChanged, false},
		{"a reflog", repo.Path(".git/logs/HEAD"), extension.FileChanged, false},
		{"FETCH_HEAD", repo.Path(".git/FETCH_HEAD"), extension.FileChanged, false},
		{"a watchman cookie", repo.Path(".watchman-cookie-host-1"), extension.FileCreated, false},
		{"an ignored folder", repo.Path("build/out.o"), extension.FileCreated, false},
		{"an ignored pattern", repo.Path("src/debug.log"), extension.FileChanged, false},
		{"a pattern un-ignored", repo.Path("src/keep.log"), extension.FileChanged, true},
		{"a nested ignore file", repo.Path("nested/tmp-1"), extension.FileCreated, false},
		{"a nested file that is not ignored", repo.Path("nested/real"), extension.FileCreated, true},
		{"a path outside the repository", filepath.Join(filepath.Dir(repo.Dir), "elsewhere", "f"), extension.FileChanged, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep.debounce = nil
			pending := r.clock.Pending()
			rep.onChanges([]extension.FileChange{{Kind: tc.kind, Path: tc.path}})
			got := r.clock.Pending() > pending
			if got != tc.want {
				t.Errorf("a status was %v, want %v", got, tc.want)
			}
			if rep.debounce != nil {
				rep.debounce() // stop it
			}
			rep.debounce = nil
		})
	}
}

func TestAnIgnoredFileThatIsTrackedStillTriggersAStatus(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("tracked.log", "a\n")
	repo.Commit("Add a log")
	repo.Write(".gitignore", "*.log\n")
	repo.Write("tracked.log", "b\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rep := r.repo(repo.Dir)
	if got := r.letters(repo.Dir, groupWorking); len(got) < 1 || got[0] != "tracked.log M" {
		t.Fatalf("working = %v", got)
	}
	rep.onChanges([]extension.FileChange{change(repo.Path("tracked.log"))})
	if r.clock.Pending() != 1 {
		t.Error("the change of a tracked file that a rule ignores was dropped")
	}
}

func TestEmetosOwnCommandsDoNotRetriggerTheStatus(t *testing.T) {
	r, repo, rep := repoWithChange(t, rigOptions{})
	rep.beginOwn()
	r.api.Watches()[0].Fire(change(repo.Path(".git/index")), change(repo.Path("src/a.go")))
	r.api.Pump()
	if r.clock.Pending() != 0 {
		t.Fatal("the changes of an own command made a status")
	}
	before := rep.refreshes
	rep.endOwn()
	r.api.WaitFor("the one status at the end", func() bool { r.api.Pump(); return rep.refreshes == before+1 && !rep.refreshing })
}

func TestRefreshOnFocusAndOnSave(t *testing.T) {
	r, repo, rep := repoWithChange(t, rigOptions{})
	before := rep.refreshes
	r.api.Emit(extension.TopicWindowFocusChanged, extension.WindowFocus{Focused: false})
	r.api.Pump()
	if r.clock.Pending() != 0 {
		t.Fatal("losing the focus made a status")
	}
	r.api.Emit(extension.TopicWindowFocusChanged, extension.WindowFocus{Focused: true})
	r.api.Pump()
	if r.clock.Pending() != 1 {
		t.Fatal("getting the focus did not schedule a status")
	}
	r.clock.Advance(DebounceDelay)
	r.api.WaitFor("the status after the focus", func() bool { r.api.Pump(); return rep.refreshes == before+1 && !rep.refreshing })
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: uriOf(repo.Path("src/a.go")), Version: 2})
	r.api.Pump()
	if r.clock.Pending() != 1 {
		t.Fatal("saving a file did not schedule a status")
	}
	r.clock.Advance(DebounceDelay)
	r.api.WaitFor("the status after the save", func() bool { r.api.Pump(); return rep.refreshes == before+2 && !rep.refreshing })
	// a file of another place says nothing
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: "file:///elsewhere/x.go"})
	r.api.Pump()
	if r.clock.Pending() != 0 {
		t.Error("a file outside every repository made a status")
	}
}

func TestAutorefreshOffWatchesNothing(t *testing.T) {
	r, _, rep := repoWithChange(t, rigOptions{settings: map[string]any{"git.autorefresh": false}})
	if len(r.api.Watches()) != 0 {
		t.Errorf("%d watches with autorefresh off", len(r.api.Watches()))
	}
	r.api.Emit(extension.TopicWindowFocusChanged, extension.WindowFocus{Focused: true})
	r.api.Pump()
	if r.clock.Pending() != 0 {
		t.Error("the focus made a status")
	}
	// turning it on starts the watch
	r.api.SetSetting("git.autorefresh", true)
	r.api.WaitFor("the watch", func() bool { r.api.Pump(); return len(r.api.Watches()) == 1 })
	// turning it off stops it, also a watch that was still being placed when the setting changed (it is ended when it arrives)
	r.api.SetSetting("git.autorefresh", false)
	r.api.WaitFor("the watch to stop", func() bool { r.api.Pump(); return len(r.api.Watches()) == 0 })
	_ = rep
}

// TestAWatchPlacedAfterTheStopIsEnded stops the watches while their goroutines are still placing them: the watches they place
// afterwards are ended when they arrive, not kept with nothing to stop them.
func TestAWatchPlacedAfterTheStopIsEnded(t *testing.T) {
	r, _, rep := repoWithChange(t, rigOptions{})
	r.api.WaitFor("the watches", func() bool { return len(r.api.Watches()) > 0 })
	rep.stopWatching()
	r.api.WaitFor("no watch", func() bool { return len(r.api.Watches()) == 0 })
	rep.startWatching()
	rep.stopWatching() // the goroutines of startWatching have not reported their watches yet: the test is on the UI thread
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		r.api.Pump()
		time.Sleep(5 * time.Millisecond)
	}
	if n := len(r.api.Watches()); n != 0 {
		t.Errorf("%d watches are left after the stop", n)
	}
}

func TestIdleDoesNothing(t *testing.T) {
	r, _, rep := repoWithChange(t, rigOptions{})
	time.Sleep(50 * time.Millisecond)
	goroutines := runtime.NumGoroutine()
	refreshes := rep.refreshes
	for i := 0; i < 20; i++ { // ten minutes of the fake clock
		r.clock.Advance(30 * time.Second)
		r.api.Pump()
	}
	time.Sleep(50 * time.Millisecond)
	if rep.refreshes != refreshes || r.clock.Pending() != 0 {
		t.Errorf("the idle extension ran %d statuses and has %d timers", rep.refreshes-refreshes, r.clock.Pending())
	}
	if n := runtime.NumGoroutine(); n > goroutines {
		t.Errorf("goroutines grew from %d to %d", goroutines, n)
	}
}

func TestClosingARepositoryStopsItsWatchesAndItsProcesses(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "a\n")
	calls := filepath.Join(t.TempDir(), "calls.jsonl")
	// a git that hangs until it is ended, in the repository
	path, env := gittest.FakeGit(t, "record "+calls+"\nversion 2.43.0\nhang")
	r := newRig(t, e, rigOptions{folder: repo.Dir, gitPath: path, extraEnv: []string{env}})
	r.api.WaitFor("the repository", func() bool { r.api.Pump(); return r.x.model.byRoot(repo.Dir) != nil })
	rep := r.repo(repo.Dir)
	var pid int
	r.api.WaitFor("the process in the repository", func() bool {
		r.api.Pump()
		data, _ := os.ReadFile(calls)
		for _, line := range strings.Split(string(data), "\n") {
			var c gittest.FakeCall
			if json.Unmarshal([]byte(line), &c) == nil && c.Dir == repo.Dir {
				pid = c.PID
			}
		}
		return pid != 0
	})
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("the process is not running: %v", err)
	}
	r.x.model.close(rep)
	r.api.WaitFor("the process to end", func() bool { return syscall.Kill(pid, 0) != nil })
	if r.api.ContextKey("git.closedRepositoryCount") != 1 || len(r.api.SourceControls()) != 0 {
		t.Error("the repository is still shown")
	}
	// a scan does not bring it back, reopening does
	r.x.model.scan()
	r.api.Pump()
	if r.x.model.byRoot(repo.Dir) != nil {
		t.Error("the closed repository came back")
	}
}

func TestInProgressOperationsFromRealStops(t *testing.T) {
	tests := []struct {
		name  string
		stop  func(r *gittest.Repo, ours, theirs string)
		key   string
		icon  string
		label string
		file  string
	}{
		{"merge", func(r *gittest.Repo, ours, theirs string) { _ = r.Merge(theirs) }, "gitMergeInProgress", "git-branch-conflicts", "ours!", "MERGE_HEAD"},
		{"cherry-pick", func(r *gittest.Repo, ours, theirs string) { _, _, _ = r.GitErr("cherry-pick", theirs) }, "gitCherryPickInProgress", "git-branch-conflicts", "", "CHERRY_PICK_HEAD"},
		{"rebase", func(r *gittest.Repo, ours, theirs string) { _, _, _ = r.GitErr("rebase", theirs) }, "gitRebaseInProgress", "git-branch-conflicts", "", "REBASE_HEAD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := e.Init("repo")
			ours, theirs := repo.ConflictingBranches()
			tc.stop(repo, ours, theirs)
			r := newRig(t, e, rigOptions{folder: repo.Dir})
			r.settle()
			if got := r.api.ContextKey(tc.key); got != true {
				t.Errorf("%s = %v", tc.key, got)
			}
			if got := r.letters(repo.Dir, groupMerge); len(got) != 1 || got[0] != "src/loader.go !" {
				t.Errorf("merge group = %v", got)
			}
			items := r.source(repo.Dir).StatusBarCommands()
			if len(items) == 0 || !strings.HasPrefix(items[0].Text, "$("+tc.icon+") ") {
				t.Fatalf("branch item = %+v", items)
			}
			if tc.label != "" && !strings.HasSuffix(items[0].Text, tc.label) {
				t.Errorf("branch item = %q", items[0].Text)
			}
			if tc.name == "rebase" && !strings.Contains(items[0].Text, "(Rebasing)") {
				t.Errorf("a rebase does not say so: %q", items[0].Text)
			}
			st := r.repo(repo.Dir).state()
			if (tc.name == "merge" && !st.Merging) || (tc.name == "rebase" && !st.Rebasing) || (tc.name == "cherry-pick" && !st.CherryPicking) {
				t.Errorf("state = %+v", st)
			}
		})
	}
}

func TestAheadBehindAndTheUpstreamFromALocalRemote(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "1\n")
	repo.Commit("one")
	bare := repo.AddRemote("origin")
	repo.Git("push", "-q", "-u", "origin", "main")
	other := bare.Clone("other")
	other.Write("b.txt", "b\n")
	other.Commit("two")
	other.Write("c.txt", "c\n")
	other.Commit("three")
	other.Git("push", "-q", "origin", "main")
	repo.Git("fetch", "-q", "origin")
	repo.Write("d.txt", "d\n")
	repo.Commit("four")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	items := r.source(repo.Dir).StatusBarCommands()
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Text != "$(git-branch) main" || items[0].Command != "git.checkout" || items[0].Tooltip != "main, Checkout Branch/Tag..." {
		t.Errorf("branch item = %+v", items[0])
	}
	if items[1].Text != "$(sync) 2↓ 1↑" || items[1].Command != "git.sync" || items[1].Tooltip != "Pull 2 and push 1 commits between origin/main" {
		t.Errorf("sync item = %+v", items[1])
	}
	st := r.repo(repo.Dir).state()
	if st.Branch != "main" || st.Upstream != "origin/main" || st.Ahead != 1 || st.Behind != 2 {
		t.Errorf("state = %+v", st)
	}
	// the handle in the arguments is the repository's source control
	args := extension.SCMArgs(items[0].Args)
	if len(args) != 1 || args[0].SourceControl != r.repo(repo.Dir).handle {
		t.Errorf("args = %+v", items[0].Args)
	}
}

func TestStatusBarItemsForTheStatesOfHEAD(t *testing.T) {
	t.Run("changes, staged changes", func(t *testing.T) {
		r, repo, rep := repoWithChange(t, rigOptions{})
		repo.Git("add", "src/a.go")
		repo.Write("README.md", "# changed\n")
		r.refresh(rep)
		items := r.source(repo.Dir).StatusBarCommands()
		if items[0].Text != "$(git-branch-staged-changes) main*+" {
			t.Errorf("item = %q", items[0].Text)
		}
		repo.Git("restore", "--staged", "src/a.go")
		repo.Git("checkout", "--", "src/a.go", "README.md")
		repo.Write("README.md", "# changed\n")
		r.refresh(rep)
		if got := r.source(repo.Dir).StatusBarCommands()[0].Text; got != "$(git-branch-changes) main*" {
			t.Errorf("item = %q", got)
		}
		repo.Git("checkout", "--", "README.md")
		r.refresh(rep)
		if got := r.source(repo.Dir).StatusBarCommands()[0].Text; got != "$(git-branch) main" {
			t.Errorf("item = %q", got)
		}
	})
	t.Run("a detached HEAD shows the short commit", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		repo.Write("a.txt", "a\n")
		hash := repo.Commit("one")
		repo.Git("checkout", "-q", "--detach", hash)
		r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.commitShortHashLength": 9}})
		r.settle()
		if got := r.source(repo.Dir).StatusBarCommands()[0].Text; got != "$(git-commit) "+hash[:9] {
			t.Errorf("item = %q", got)
		}
		repo.Tag("v1", "")
		r.refresh(r.repo(repo.Dir))
		if got := r.source(repo.Dir).StatusBarCommands()[0].Text; got != "$(tag) v1" {
			t.Errorf("item = %q", got)
		}
	})
	t.Run("a branch with no commit has no sync item and a fresh repository", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		repo.Write("a.txt", "a\n")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		items := r.source(repo.Dir).StatusBarCommands()
		if len(items) != 1 || items[0].Text != "$(git-branch-changes) main*" {
			t.Errorf("items = %+v", items)
		}
		if r.api.ContextKey("gitFreshRepository") != true {
			t.Error("gitFreshRepository is not set")
		}
	})
	t.Run("no upstream publishes, no remote publishes too", func(t *testing.T) {
		r, repo, _ := repoWithChange(t, rigOptions{})
		items := r.source(repo.Dir).StatusBarCommands()
		if len(items) != 2 || items[1].Text != "$(cloud-upload)" || items[1].Command != "git.publish" || items[1].Tooltip != "Publish Branch" {
			t.Errorf("items = %+v", items)
		}
		r.api.SetSetting("git.enableStatusBarSync", false)
		r.api.WaitFor("the item to go", func() bool { r.api.Pump(); return len(r.source(repo.Dir).StatusBarCommands()) == 1 })
	})
}

func TestTheLimitWarningIsShownOnce(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "a\n")
	repo.Commit("one")
	for i := 0; i < 8; i++ {
		repo.Write(fmt.Sprintf("n%d.txt", i), "n\n")
	}
	r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.statusLimit": 3}})
	r.settle()
	rep := r.repo(repo.Dir)
	if got := r.paths(repo.Dir, groupWorking); len(got) != 3 {
		t.Fatalf("working = %v", got)
	}
	if !rep.state().Limited {
		t.Error("the state does not say the list is limited")
	}
	count := func() int {
		n := 0
		for _, note := range r.api.Notes() {
			if strings.Contains(note.Message, "has too many active changes") {
				n++
			}
		}
		return n
	}
	if count() != 1 {
		t.Fatalf("%d warnings", count())
	}
	r.refresh(rep)
	r.refresh(rep)
	if count() != 1 {
		t.Errorf("%d warnings after two more statuses", count())
	}
	for i, note := range r.api.Notes() {
		if strings.Contains(note.Message, "too many") {
			if note.Message != "The git repository at \""+repo.Dir+"\" has too many active changes, only a subset of Git features will be enabled." {
				t.Errorf("message = %q", note.Message)
			}
			r.api.RunNoteAction(i, "Ignore")
		}
	}
	if ups := r.api.SettingUpdates(); len(ups) != 1 || ups[0].Key != "git.ignoreLimitWarning" || ups[0].Value != true {
		t.Errorf("updates = %+v", ups)
	}
	// with the warning ignored a second repository does not warn either
	if !reflect.DeepEqual(r.x.cfg().ignoreLimitWarning, true) {
		t.Error("the setting is not read")
	}
}

func TestTheRepositoryChangedEventAndTheStatusRequest(t *testing.T) {
	r, repo, rep := repoWithChange(t, rigOptions{})
	events := r.api.PublishedEvents()
	var last RepositoryState
	n := 0
	for _, ev := range events {
		if ev.Topic == TopicRepositoryChanged {
			n++
			if err := json.Unmarshal(ev.Data, &last); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n == 0 || last.Root != repo.Dir || last.Branch != "main" || last.Changes.Working != 1 || last.Count != 1 || !last.Trusted || last.Kind != "repository" {
		t.Fatalf("event = %+v (%d events)", last, n)
	}
	// an unchanged state says nothing again
	before := len(r.api.PublishedEvents())
	r.refresh(rep)
	if len(r.api.PublishedEvents()) != before {
		t.Error("an unchanged state was published again")
	}
	repo.Write("new.txt", "n\n")
	r.refresh(rep)
	if len(r.api.PublishedEvents()) != before+1 {
		t.Error("a changed state was not published")
	}
	reply, err := r.api.Ask(r.x.ctx, TopicStatus, map[string]string{"root": repo.Dir})
	if err != nil {
		t.Fatal(err)
	}
	var st RepositoryState
	if err := json.Unmarshal(reply.Data, &st); err != nil || st.Root != repo.Dir || st.Changes.Working != 2 {
		t.Errorf("reply = %s (%v)", reply.Data, err)
	}
	if _, err := r.api.Ask(r.x.ctx, TopicStatus, map[string]string{"root": "/nowhere"}); err == nil {
		t.Error("a request for a repository that is not open succeeded")
	}
}

func TestOptimisticStageAndUnstageMoveTheResourcesAtOnce(t *testing.T) {
	r, repo, rep := repoWithChange(t, rigOptions{})
	repo.Write("new.txt", "n\n")
	r.refresh(rep)
	if got := r.letters(repo.Dir, groupWorking); len(got) != 2 {
		t.Fatalf("working = %v", got)
	}
	rep.optimisticStage([]string{repo.Path("src/a.go"), repo.Path("new.txt")})
	if got, want := r.letters(repo.Dir, groupIndex), []string{"src/a.go M", "new.txt A"}; !reflect.DeepEqual(got, want) {
		t.Errorf("index = %v, want %v", got, want)
	}
	if got := r.letters(repo.Dir, groupWorking); len(got) != 0 {
		t.Errorf("working = %v", got)
	}
	if got := r.source(repo.Dir).StatusBarCommands()[0].Text; got != "$(git-branch-staged-changes) main+" {
		t.Errorf("the status bar was not updated: %q", got)
	}
	rep.optimisticUnstage([]string{repo.Path("src/a.go")})
	if got, want := r.letters(repo.Dir, groupIndex), []string{"new.txt A"}; !reflect.DeepEqual(got, want) {
		t.Errorf("index = %v, want %v", got, want)
	}
	if got, want := r.letters(repo.Dir, groupWorking), []string{"src/a.go M"}; !reflect.DeepEqual(got, want) {
		t.Errorf("working = %v, want %v", got, want)
	}
	// the confirming status puts what git says in place (nothing was staged in the repository)
	r.refresh(rep)
	if len(r.letters(repo.Dir, groupIndex)) != 0 || len(r.letters(repo.Dir, groupWorking)) != 2 {
		t.Errorf("after the status: index %v, working %v", r.letters(repo.Dir, groupIndex), r.letters(repo.Dir, groupWorking))
	}
}

func TestOptimisticUpdatesDoNothingWhenTheSettingIsOff(t *testing.T) {
	r, repo, rep := repoWithChange(t, rigOptions{settings: map[string]any{"git.optimisticUpdate": false}})
	rep.optimisticStage([]string{repo.Path("src/a.go")})
	if len(r.letters(repo.Dir, groupIndex)) != 0 || len(r.letters(repo.Dir, groupWorking)) != 1 {
		t.Errorf("index %v, working %v", r.letters(repo.Dir, groupIndex), r.letters(repo.Dir, groupWorking))
	}
}
