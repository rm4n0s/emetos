package git

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// Fetch, pull, push and the remotes against real git and local bare repositories (step 24.15). Nothing here touches a network: the
// remotes are folders of the test's world.

// remoteWorld is a repository on main whose origin is a bare repository, a second clone of that origin that plays another developer, and
// the first commit pushed.
type remoteWorld struct {
	env   *gittest.Env
	repo  *gittest.Repo
	bare  *gittest.Repo
	other *gittest.Repo
	git   *Repo
}

func newRemoteWorld(t *testing.T) *remoteWorld {
	t.Helper()
	e := gittest.NewEnv(t)
	r := e.Init("work")
	r.Write("a.txt", "one\n")
	r.Commit("one")
	bare := r.AddRemote("origin")
	r.Git("push", "-q", "-u", "origin", "main")
	other := bare.Clone("other")
	return &remoteWorld{env: e, repo: r, bare: bare, other: other, git: open(t, e, r.Dir)}
}

// push makes a commit in the other clone and pushes it to origin.
func (w *remoteWorld) push(file, content string) string {
	w.other.Write(file, content)
	head := w.other.Commit("other " + file)
	w.other.Git("push", "-q", "origin", "main")
	return head
}

func TestParseProgress(t *testing.T) {
	tests := []struct {
		name, line string
		want       Progress
		ok         bool
	}{
		{"receiving", "Receiving objects:  45% (9/20), 1.2 KiB | 1 KiB/s", Progress{Phase: "Receiving objects", Percent: 45}, true},
		{"done", "Receiving objects: 100% (20/20), 2.00 KiB | 2.00 MiB/s, done.", Progress{Phase: "Receiving objects", Percent: 100}, true},
		{"the remote counting", "remote: Counting objects:  50% (5/10)", Progress{Phase: "Counting objects", Percent: 50, Remote: true}, true},
		{"compressing", "remote: Compressing objects: 100% (2/2), done.", Progress{Phase: "Compressing objects", Percent: 100, Remote: true}, true},
		{"writing", "Writing objects: 100% (3/3), 250 bytes | 250.00 KiB/s, done.", Progress{Phase: "Writing objects", Percent: 100}, true},
		{"resolving", "Resolving deltas:  10% (1/10)", Progress{Phase: "Resolving deltas", Percent: 10}, true},
		{"unpacking", "Unpacking objects: 100% (3/3), 280 bytes | 280.00 KiB/s, done.", Progress{Phase: "Unpacking objects", Percent: 100}, true},
		{"enumerating has a count only", "Enumerating objects: 5, done.", Progress{Phase: "Enumerating objects", Percent: -1}, true},
		{"a carriage return at the end", "Receiving objects:   1% (1/100)\r", Progress{Phase: "Receiving objects", Percent: 1}, true},
		{"a message is not progress", "From /srv/origin\n", Progress{}, false},
		{"a remote message is not progress", "remote: Total 3 (delta 0), reused 0 (delta 0)", Progress{}, false},
		{"an error", "fatal: unable to access 'x': y", Progress{}, false},
		{"a phase git does not have", "Frobnicating objects: 10% (1/10)", Progress{}, false},
		{"empty", "", Progress{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseProgress(tc.line)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ParseProgress(%q) = %+v, %v; want %+v, %v", tc.line, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestProgressOverall(t *testing.T) {
	tests := []struct {
		p    Progress
		want int
	}{
		{Progress{Phase: "Counting objects", Percent: 0}, 0},
		{Progress{Phase: "Counting objects", Percent: 100}, 10},
		{Progress{Phase: "Compressing objects", Percent: 50}, 15},
		{Progress{Phase: "Receiving objects", Percent: 0}, 20},
		{Progress{Phase: "Receiving objects", Percent: 100}, 60},
		{Progress{Phase: "Writing objects", Percent: 100}, 100},
		{Progress{Phase: "Resolving deltas", Percent: 50}, 80},
		{Progress{Phase: "Resolving deltas", Percent: 100}, 100},
		{Progress{Phase: "Enumerating objects", Percent: -1}, -1},
		{Progress{Phase: "Filtering content", Percent: 10}, -1},
	}
	for _, tc := range tests {
		if got := tc.p.Overall(); got != tc.want {
			t.Errorf("%+v.Overall() = %d, want %d", tc.p, got, tc.want)
		}
	}
}

// The writer splits git's output at carriage returns and line feeds, hands the progress to the function and keeps the rest.
func TestProgressWriter(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   []string // the percentages seen
		kept   string
	}{
		{"carriage returns", []string{"Receiving objects:   1% (1/100)\rReceiving objects:  50% (50/100)\rReceiving objects: 100% (100/100), done.\n"}, []string{"1", "50", "100"}, ""},
		{"a line split across writes", []string{"Receiving obj", "ects:  50% (5/10)\r", "Receiving objects: 100% (10/10)\n"}, []string{"50", "100"}, ""},
		{"messages are kept", []string{"From /srv/x\n", "Writing objects: 100% (3/3), done.\n", "remote: hello\n"}, []string{"100"}, "From /srv/x\nremote: hello\n"},
		{"a last line with no end", []string{"fatal: it broke"}, nil, "fatal: it broke\n"},
		{"carriage return and line feed", []string{"Counting objects: 100% (2/2)\r\nfatal: x\r\n"}, []string{"100"}, "fatal: x\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tail := &tailBuffer{max: 1 << 10}
			var seen []string
			w := &progressWriter{next: tail, progress: func(p Progress) { seen = append(seen, itoa(p.Percent)) }}
			for _, c := range tc.chunks {
				if n, err := w.Write([]byte(c)); err != nil || n != len(c) {
					t.Fatalf("Write = %d, %v", n, err)
				}
			}
			w.flush()
			if strings.Join(seen, ",") != strings.Join(tc.want, ",") {
				t.Errorf("percentages %v, want %v", seen, tc.want)
			}
			if tail.String() != tc.kept {
				t.Errorf("kept %q, want %q", tail.String(), tc.kept)
			}
		})
	}
}

// The progress that a real git prints with --progress is read, and the error output that classifies a failure is left without it.
func TestFetchProgressFromARealGit(t *testing.T) {
	w := newRemoteWorld(t)
	for i := 0; i < 3; i++ {
		w.push("f"+itoa(i)+".txt", "content\n")
	}
	var mu sync.Mutex
	var seen []Progress
	err := w.git.Fetch(context.Background(), FetchOptions{Progress: func(p Progress) {
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no progress line was read from git's --progress output")
	}
	known := map[string]bool{"Enumerating objects": true, "Counting objects": true, "Compressing objects": true, "Receiving objects": true,
		"Unpacking objects": true, "Resolving deltas": true}
	for _, p := range seen {
		if !known[p.Phase] || p.Percent > 100 {
			t.Errorf("progress %+v", p)
		}
	}
	// the lines of the progress are not in the error output that classifies a failure and fills the Git channel
	w.push("late.txt", "late\n")
	err = w.git.Fetch(context.Background(), FetchOptions{Remote: "nowhere", Progress: func(Progress) {}})
	appErr := routeOf(t, err, "classify.RemoteConnectionError")
	if s, _ := appErr.Metadata["stderr"].(string); strings.Contains(s, "%") || strings.Contains(s, "\r") {
		t.Errorf("stderr of the failure has progress: %q", s)
	}
}

func TestFetch(t *testing.T) {
	ctx := context.Background()
	t.Run("new commits and tags", func(t *testing.T) {
		w := newRemoteWorld(t)
		head := w.push("b.txt", "bee\n")
		w.other.Tag("v1", "")
		w.other.Git("push", "-q", "origin", "v1")
		if err := w.git.Fetch(ctx, FetchOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-parse", "origin/main"); got != head {
			t.Errorf("origin/main = %s, want %s", got, head)
		}
		if got := w.repo.Git("tag", "--list", "v1"); got != "v1" {
			t.Errorf("tags = %q", got)
		}
		if got := w.repo.Git("rev-parse", "HEAD"); got == head {
			t.Error("a fetch moved the branch")
		}
	})
	t.Run("a named remote", func(t *testing.T) {
		w := newRemoteWorld(t)
		head := w.push("b.txt", "bee\n")
		if err := w.git.Fetch(ctx, FetchOptions{Remote: "origin"}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-parse", "origin/main"); got != head {
			t.Errorf("origin/main = %s", got)
		}
	})
	t.Run("all remotes", func(t *testing.T) {
		w := newRemoteWorld(t)
		second := w.repo.AddRemote("second")
		w.repo.Git("push", "-q", "second", "main")
		clone := second.Clone("second-clone")
		clone.Write("s.txt", "s\n")
		secondHead := clone.Commit("second")
		clone.Git("push", "-q", "origin", "main")
		head := w.push("b.txt", "bee\n")
		if err := w.git.Fetch(ctx, FetchOptions{All: true}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-parse", "origin/main"); got != head {
			t.Errorf("origin/main = %s", got)
		}
		if got := w.repo.Git("rev-parse", "second/main"); got != secondHead {
			t.Errorf("second/main = %s, want %s", got, secondHead)
		}
	})
	t.Run("prune removes the branches the remote lost", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.other.Git("push", "-q", "origin", "main:topic")
		if err := w.git.Fetch(ctx, FetchOptions{}); err != nil {
			t.Fatal(err)
		}
		w.other.Git("push", "-q", "origin", "--delete", "topic")
		if err := w.git.Fetch(ctx, FetchOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("branch", "-r", "--list", "origin/topic"); !strings.Contains(got, "origin/topic") {
			t.Fatalf("a fetch without prune lost origin/topic: %q", got)
		}
		if err := w.git.Fetch(ctx, FetchOptions{Prune: true}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("branch", "-r", "--list", "origin/topic"); got != "" {
			t.Errorf("origin/topic stayed: %q", got)
		}
	})
	t.Run("a ref fast-forwards a branch that is not checked out", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("branch", "--track", "topic", "origin/main")
		head := w.push("b.txt", "bee\n")
		if err := w.git.Fetch(ctx, FetchOptions{Remote: "origin", Ref: "main:topic"}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-parse", "topic"); got != head {
			t.Errorf("topic = %s, want %s", got, head)
		}
	})
	t.Run("a ref that is not a fast-forward is refused with BranchFastForwardRejected", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("branch", "--track", "topic", "origin/main")
		w.repo.Checkout("topic")
		w.repo.Write("mine.txt", "mine\n")
		w.repo.Commit("mine")
		w.repo.Checkout("main")
		w.push("b.txt", "bee\n")
		err := w.git.Fetch(ctx, FetchOptions{Remote: "origin", Ref: "main:topic"})
		appErr := routeOf(t, err, "Repo.Fetch"+writeRoute+"BranchFastForwardRejected")
		if appErr != nil && appErr.Tag != "BranchFastForwardRejected" {
			t.Errorf("tag %s", appErr.Tag)
		}
	})
	t.Run("a remote that does not exist", func(t *testing.T) {
		w := newRemoteWorld(t)
		wantRoute(t, w.git.Fetch(ctx, FetchOptions{Remote: "nowhere"}), "Repo.Fetch"+writeRoute+"RemoteConnectionError")
	})
	t.Run("a remote that starts with a dash", func(t *testing.T) {
		w := newRemoteWorld(t)
		wantRoute(t, w.git.Fetch(ctx, FetchOptions{Remote: "--upload-pack=x"}), "remoteArg.GitRemoteInvalid")
	})
}

// A fetch the user did not ask for carries the variable that makes the password prompt answer with a cancel.
func TestASilentFetchMarksItself(t *testing.T) {
	record := filepath.Join(t.TempDir(), "calls.jsonl")
	path, fakeEnv := gittest.FakeGit(t, "record "+record)
	e := gittest.NewEnv(t)
	dir := e.Init("repo").Dir
	runner := NewRunner(Config{Trust: trustAll, Path: path, Env: append(e.Vars(), fakeEnv)})
	repo := &Repo{Runner: runner, Queue: NewQueue(0), Root: dir}
	for _, silent := range []bool{false, true} {
		if err := repo.Fetch(context.Background(), FetchOptions{Silent: silent}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	var calls []gittest.FakeCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var c gittest.FakeCall
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, c)
	}
	if len(calls) != 2 {
		t.Fatalf("%d calls", len(calls))
	}
	has := func(c gittest.FakeCall, kv string) bool {
		for _, e := range c.Env {
			if e == kv {
				return true
			}
		}
		return false
	}
	if has(calls[0], "EMETOS_GIT_FETCH_SILENT=true") || !has(calls[0], "EMETOS_GIT_COMMAND=fetch") {
		t.Error("a fetch of the user is marked silent")
	}
	if !has(calls[1], "EMETOS_GIT_FETCH_SILENT=true") || !has(calls[1], "EMETOS_GIT_COMMAND=fetch") {
		t.Error("a silent fetch is not marked")
	}
}

func TestFetchTags(t *testing.T) {
	ctx := context.Background()
	w := newRemoteWorld(t)
	w.other.Tag("v1", "")
	w.other.Git("push", "-q", "origin", "v1")
	w.repo.Git("fetch", "-q", "origin")
	// the remote's v1 moves
	w.other.Write("b.txt", "bee\n")
	moved := w.other.Commit("moved")
	w.other.Git("tag", "-f", "v1", moved)
	w.other.Git("push", "-q", "-f", "origin", "v1")
	if err := w.git.FetchTags(ctx, "origin", []string{"v1"}, false); err == nil {
		t.Fatal("a tag that differs was replaced without force")
	}
	if err := w.git.FetchTags(ctx, "origin", []string{"v1"}, true); err != nil {
		t.Fatal(err)
	}
	if got := w.repo.Git("rev-parse", "v1^{commit}"); got != moved {
		t.Errorf("v1 = %s, want %s", got, moved)
	}
	wantRoute(t, w.git.FetchTags(ctx, "origin", []string{"--x"}, true), "refName.GitRefInvalid")
	wantRoute(t, w.git.FetchTags(ctx, "", []string{"v1"}, true), "remoteArg.GitRemoteInvalid")
}

func TestPull(t *testing.T) {
	ctx := context.Background()
	t.Run("a fast-forward", func(t *testing.T) {
		w := newRemoteWorld(t)
		head := w.push("b.txt", "bee\n")
		changed, err := w.git.Pull(ctx, PullOptions{})
		if err != nil || !changed {
			t.Fatalf("changed %v, err %v", changed, err)
		}
		if got := w.repo.Git("rev-parse", "HEAD"); got != head {
			t.Errorf("HEAD = %s, want %s", got, head)
		}
	})
	t.Run("nothing to pull", func(t *testing.T) {
		w := newRemoteWorld(t)
		changed, err := w.git.Pull(ctx, PullOptions{})
		if err != nil || changed {
			t.Fatalf("changed %v, err %v", changed, err)
		}
	})
	t.Run("a merge", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("b.txt", "bee\n")
		w.repo.Write("c.txt", "see\n")
		w.repo.Commit("mine")
		w.repo.Git("config", "pull.rebase", "false")
		if _, err := w.git.Pull(ctx, PullOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-list", "--parents", "-n", "1", "HEAD"); len(strings.Fields(got)) != 3 {
			t.Errorf("HEAD is not a merge: %q", got)
		}
	})
	t.Run("a rebase", func(t *testing.T) {
		w := newRemoteWorld(t)
		head := w.push("b.txt", "bee\n")
		w.repo.Write("c.txt", "see\n")
		w.repo.Commit("mine")
		if _, err := w.git.Pull(ctx, PullOptions{Rebase: true}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-parse", "HEAD~1"); got != head {
			t.Errorf("HEAD~1 = %s, want the pulled commit %s", got, head)
		}
		if got := w.repo.Git("rev-list", "--merges", "--count", "HEAD"); got != "0" {
			t.Errorf("%s merges after a rebase", got)
		}
	})
	t.Run("a remote and a branch", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.other.Git("push", "-q", "origin", "main:topic")
		w.other.Checkout("topic")
		w.other.Git("checkout", "-q", "-B", "topic", "origin/topic")
		w.other.Write("t.txt", "t\n")
		topic := w.other.Commit("topic")
		w.other.Git("push", "-q", "origin", "topic")
		w.repo.Git("config", "pull.rebase", "false")
		if _, err := w.git.Pull(ctx, PullOptions{Remote: "origin", Branch: "topic"}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-parse", "HEAD"); got != topic {
			t.Errorf("HEAD = %s, want %s", got, topic)
		}
	})
	t.Run("conflicts are a Conflict", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("a.txt", "theirs\n")
		w.repo.Write("a.txt", "mine\n")
		w.repo.Commit("mine")
		w.repo.Git("config", "pull.rebase", "false")
		_, err := w.git.Pull(ctx, PullOptions{})
		appErr := routeOf(t, err, "Repo.Pull"+writeRoute+"Conflict")
		if appErr != nil && appErr.Tag != "Conflict" {
			t.Errorf("tag %s", appErr.Tag)
		}
		if _, statErr := os.Stat(filepath.Join(w.repo.Dir, ".git", "MERGE_HEAD")); statErr != nil {
			t.Errorf("no merge in progress: %v", statErr)
		}
	})
	t.Run("local changes in the way are a DirtyWorkTree", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("a.txt", "theirs\n")
		w.repo.Write("a.txt", "edited\n")
		_, err := w.git.Pull(ctx, PullOptions{})
		wantRoute(t, err, "Repo.Pull"+writeRoute+"DirtyWorkTree")
		if got := w.repo.Read("a.txt"); got != "edited\n" {
			t.Errorf("a.txt = %q", got)
		}
	})
	t.Run("an autostash keeps them", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("b.txt", "bee\n")
		w.repo.Write("a.txt", "edited\n")
		if _, err := w.git.Pull(ctx, PullOptions{AutoStash: true}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Read("a.txt"); got != "edited\n" {
			t.Errorf("a.txt = %q, the change did not come back", got)
		}
		if got := w.repo.Read("b.txt"); got != "bee\n" {
			t.Errorf("b.txt = %q, the pull did not happen", got)
		}
		if got := w.repo.Git("stash", "list"); got != "" {
			t.Errorf("stash left: %q", got)
		}
	})
	t.Run("tags that differ are a TagConflict with their names", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("tag", "v1")
		w.other.Tag("v1", "")
		w.other.Git("push", "-q", "origin", "v1")
		w.other.Write("b.txt", "bee\n")
		w.other.Commit("later")
		w.other.Git("tag", "-f", "v1")
		w.other.Git("push", "-q", "-f", "origin", "v1", "main")
		_, err := w.git.Pull(ctx, PullOptions{Tags: true})
		appErr := routeOf(t, err, "Repo.Pull"+writeRoute+"TagConflict")
		if appErr == nil {
			return
		}
		stderr, _ := appErr.Metadata["stderr"].(string)
		if tags := ConflictingTags(stderr); len(tags) != 1 || tags[0] != "v1" {
			t.Errorf("conflicting tags %v from %q", tags, stderr)
		}
	})
	t.Run("a remote without a branch is ignored", func(t *testing.T) {
		w := newRemoteWorld(t)
		head := w.push("b.txt", "bee\n")
		if _, err := w.git.Pull(ctx, PullOptions{Remote: "origin"}); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-parse", "HEAD"); got != head {
			t.Errorf("HEAD = %s", got)
		}
	})
	t.Run("a branch that starts with a dash", func(t *testing.T) {
		w := newRemoteWorld(t)
		_, err := w.git.Pull(ctx, PullOptions{Remote: "origin", Branch: "--x"})
		wantRoute(t, err, "refName.GitRefInvalid")
	})
}

func TestConflictingTags(t *testing.T) {
	tests := []struct {
		name, stderr string
		want         []string
	}{
		{"one", "From /srv/x\n ! [rejected]        v1         -> v1         (would clobber existing tag)\n", []string{"v1"}},
		{"two", " ! [rejected]        v1 -> v1  (would clobber existing tag)\n ! [rejected]        rel/2 -> rel/2  (would clobber existing tag)\n", []string{"v1", "rel/2"}},
		{"a rejected branch is not a tag", " ! [rejected]        main -> origin/main  (non-fast-forward)\n", nil},
		{"nothing", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ConflictingTags(tc.stderr)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("tags %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMaybeRebased(t *testing.T) {
	ctx := context.Background()
	t.Run("the upstream has the same change under another hash", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Write("b.txt", "bee\n")
		w.repo.Commit("add b")
		w.repo.Git("push", "-q", "origin", "main")
		// the branch is rebased: the same change, a new commit
		w.repo.Git("reset", "-q", "--hard", "HEAD~1")
		w.repo.Write("z.txt", "zed\n")
		w.repo.Commit("new base")
		w.repo.Write("b.txt", "bee\n")
		w.repo.Commit("add b")
		w.repo.Git("fetch", "-q", "origin")
		if !w.git.MaybeRebased(ctx, "main") {
			t.Error("a rebased branch was not noticed")
		}
	})
	t.Run("a branch that is behind", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("b.txt", "bee\n")
		w.repo.Git("fetch", "-q", "origin")
		if w.git.MaybeRebased(ctx, "main") {
			t.Error("a branch that is only behind looks rebased")
		}
	})
	t.Run("a branch with no upstream", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("branch", "lonely")
		if w.git.MaybeRebased(ctx, "lonely") {
			t.Error("no upstream looks rebased")
		}
	})
	t.Run("a name that is an option", func(t *testing.T) {
		w := newRemoteWorld(t)
		if w.git.MaybeRebased(ctx, "--all") {
			t.Error("an option was run")
		}
	})
}

func TestPush(t *testing.T) {
	ctx := context.Background()
	t.Run("the current branch", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Write("b.txt", "bee\n")
		head := w.repo.Commit("two")
		if err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "main:main"}); err != nil {
			t.Fatal(err)
		}
		if got := w.bare.Git("rev-parse", "main"); got != head {
			t.Errorf("origin/main = %s, want %s", got, head)
		}
	})
	t.Run("a new branch with -u", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("checkout", "-q", "-b", "topic")
		w.repo.Write("t.txt", "t\n")
		head := w.repo.Commit("topic")
		if err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "topic", SetUpstream: true}); err != nil {
			t.Fatal(err)
		}
		if got := w.bare.Git("rev-parse", "topic"); got != head {
			t.Errorf("topic at the remote = %s", got)
		}
		if got := w.repo.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); got != "origin/topic" {
			t.Errorf("upstream = %q", got)
		}
	})
	t.Run("a branch with no upstream is a NoUpstreamBranch", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("checkout", "-q", "-b", "topic")
		w.repo.Write("t.txt", "t\n")
		w.repo.Commit("topic")
		w.repo.Git("config", "push.autoSetupRemote", "false")
		wantRoute(t, w.git.Push(ctx, PushOptions{}), "Repo.Push"+writeRoute+"NoUpstreamBranch")
	})
	t.Run("a rejected push", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("b.txt", "bee\n")
		w.repo.Write("c.txt", "see\n")
		w.repo.Commit("mine")
		err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "main:main"})
		wantRoute(t, err, "Repo.Push"+writeRoute+"PushRejected")
	})
	t.Run("force with lease replaces what the branch has seen", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("commit", "-q", "--amend", "-m", "one, amended")
		head := w.repo.Head()
		if err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "main:main"}); err == nil {
			t.Fatal("a plain push of a rewritten history went through")
		}
		for _, mode := range []ForcePushMode{ForceWithLease, ForceWithLeaseIfIncludes} {
			if err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "main:main", Force: mode}); err != nil {
				t.Fatalf("mode %d: %v", mode, err)
			}
			if got := w.bare.Git("rev-parse", "main"); got != head {
				t.Errorf("mode %d: origin/main = %s, want %s", mode, got, head)
			}
			w.repo.Git("commit", "-q", "--amend", "-m", "one, amended again "+itoa(int(mode)))
			head = w.repo.Head()
		}
	})
	t.Run("force with lease is refused when the remote moved", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("b.txt", "bee\n") // the remote has a commit this clone has not fetched
		w.repo.Git("commit", "-q", "--amend", "-m", "one, amended")
		err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "main:main", Force: ForceWithLease})
		wantRoute(t, err, "Repo.Push"+writeRoute+"ForcePushWithLeaseRejected")
	})
	t.Run("force with lease and if-includes is refused when the tracking branch has commits the branch lacks", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("b.txt", "bee\n")
		w.repo.Git("fetch", "-q", "origin") // the tracking branch is current, but the branch does not include its new commit
		w.repo.Git("commit", "-q", "--amend", "-m", "one, amended")
		err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "main:main", Force: ForceWithLeaseIfIncludes})
		wantRoute(t, err, "Repo.Push"+writeRoute+"ForcePushWithLeaseIfIncludesRejected")
	})
	t.Run("a plain force", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.push("b.txt", "bee\n")
		w.repo.Git("commit", "-q", "--amend", "-m", "one, amended")
		head := w.repo.Head()
		if err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "main:main", Force: ForceAlways}); err != nil {
			t.Fatal(err)
		}
		if got := w.bare.Git("rev-parse", "main"); got != head {
			t.Errorf("origin/main = %s", got)
		}
	})
	t.Run("tags", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Tag("v1", "")
		w.repo.Tag("v2", "release two")
		if err := w.git.Push(ctx, PushOptions{Remote: "origin", Tags: true}); err != nil {
			t.Fatal(err)
		}
		if got := w.bare.Git("tag", "--list"); got != "v1\nv2" {
			t.Errorf("tags at the remote: %q", got)
		}
	})
	t.Run("follow tags pushes the annotated ones only", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Write("b.txt", "bee\n")
		w.repo.Commit("two")
		w.repo.Tag("light", "")
		w.repo.Tag("annotated", "release")
		if err := w.git.Push(ctx, PushOptions{FollowTags: true}); err != nil {
			t.Fatal(err)
		}
		if got := w.bare.Git("tag", "--list"); got != "annotated" {
			t.Errorf("tags at the remote: %q", got)
		}
	})
	t.Run("a remote that starts with a dash", func(t *testing.T) {
		w := newRemoteWorld(t)
		wantRoute(t, w.git.Push(ctx, PushOptions{Remote: "--mirror"}), "remoteArg.GitRemoteInvalid")
		wantRoute(t, w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "--delete"}), "refName.GitRefInvalid")
	})
	t.Run("the progress of a push", func(t *testing.T) {
		w := newRemoteWorld(t)
		for i := 0; i < 5; i++ {
			w.repo.Write("p"+itoa(i)+".txt", strings.Repeat("data\n", 50))
		}
		w.repo.Commit("many")
		var mu sync.Mutex
		var phases []string
		err := w.git.Push(ctx, PushOptions{Remote: "origin", Refspec: "main:main", Progress: func(p Progress) {
			mu.Lock()
			phases = append(phases, p.Phase)
			mu.Unlock()
		}})
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		found := false
		for _, p := range phases {
			found = found || p == "Writing objects"
		}
		if !found {
			t.Errorf("phases %v: no \"Writing objects\"", phases)
		}
	})
}

func TestSetUpstream(t *testing.T) {
	ctx := context.Background()
	w := newRemoteWorld(t)
	w.repo.Git("checkout", "-q", "-b", "topic")
	w.repo.Git("push", "-q", "origin", "topic")
	if err := w.git.SetUpstream(ctx, "", "origin/topic"); err != nil {
		t.Fatal(err)
	}
	if got := w.repo.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); got != "origin/topic" {
		t.Errorf("upstream = %q", got)
	}
	w.repo.Git("branch", "other")
	if err := w.git.SetUpstream(ctx, "other", "origin/main"); err != nil {
		t.Fatal(err)
	}
	if got := w.repo.Git("rev-parse", "--abbrev-ref", "other@{upstream}"); got != "origin/main" {
		t.Errorf("upstream of other = %q", got)
	}
	wantRoute(t, w.git.SetUpstream(ctx, "", "origin/nope"), "Repo.SetUpstream"+writeRoute+"GitCommandFailed")
	wantRoute(t, w.git.SetUpstream(ctx, "", "-x"), "refName.GitRefInvalid")
}

func TestFastForwardBranch(t *testing.T) {
	ctx := context.Background()
	t.Run("a branch without commits of its own is moved", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("branch", "--track", "topic", "origin/main")
		head := w.push("b.txt", "bee\n")
		if err := w.git.FastForwardBranch(ctx, "topic"); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("rev-parse", "topic"); got != head {
			t.Errorf("topic = %s, want %s", got, head)
		}
	})
	t.Run("a branch with commits of its own stays", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("checkout", "-q", "-b", "topic", "--track", "origin/main")
		w.repo.Write("mine.txt", "mine\n")
		mine := w.repo.Commit("mine")
		w.repo.Checkout("main")
		w.push("b.txt", "bee\n")
		if err := w.git.FastForwardBranch(ctx, "topic"); err != nil {
			t.Fatalf("a branch that cannot be fast-forwarded is not an error: %v", err)
		}
		if got := w.repo.Git("rev-parse", "topic"); got != mine {
			t.Errorf("topic = %s, want %s", got, mine)
		}
	})
	t.Run("a branch with no upstream, a tag and a name that is not there", func(t *testing.T) {
		w := newRemoteWorld(t)
		w.repo.Git("branch", "lonely")
		w.repo.Tag("v1", "")
		for _, name := range []string{"lonely", "v1", "nope"} {
			if err := w.git.FastForwardBranch(ctx, name); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}

func TestRemotes(t *testing.T) {
	ctx := context.Background()
	t.Run("add and remove", func(t *testing.T) {
		w := newRemoteWorld(t)
		second := w.env.InitBare("second.git")
		if err := w.git.AddRemote(ctx, "second", second.Dir); err != nil {
			t.Fatal(err)
		}
		remotes, err := w.git.Remotes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]string{}
		for _, r := range remotes {
			names[r.Name] = r.FetchURL
		}
		if names["second"] != second.Dir || names["origin"] == "" {
			t.Errorf("remotes %v", names)
		}
		w.repo.Git("fetch", "-q", "second")
		if err := w.git.RemoveRemote(ctx, "second"); err != nil {
			t.Fatal(err)
		}
		if got := w.repo.Git("remote"); got != "origin" {
			t.Errorf("remotes after the removal: %q", got)
		}
	})
	t.Run("a name that exists", func(t *testing.T) {
		w := newRemoteWorld(t)
		wantRoute(t, w.git.AddRemote(ctx, "origin", w.bare.Dir), "Repo.AddRemote"+writeRoute+"GitCommandFailed")
	})
	t.Run("a remote that is not there", func(t *testing.T) {
		w := newRemoteWorld(t)
		wantRoute(t, w.git.RemoveRemote(ctx, "nope"), "Repo.RemoveRemote"+writeRoute+"GitCommandFailed")
	})
	t.Run("names that are options", func(t *testing.T) {
		w := newRemoteWorld(t)
		wantRoute(t, w.git.AddRemote(ctx, "--tags", "x"), "remoteArg.GitRemoteInvalid")
		wantRoute(t, w.git.AddRemote(ctx, "x", "-ufoo"), "remoteArg.GitRemoteInvalid")
		wantRoute(t, w.git.RemoveRemote(ctx, ""), "remoteArg.GitRemoteInvalid")
	})
}

func TestValidateRemoteURL(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{"https://example.com/team/repo.git", true},
		{"http://127.0.0.1:8080/repo.git", true},
		{"https://user@example.com/repo.git", true},
		{"ssh://git@example.com/team/repo.git", true},
		{"ssh://git@example.com:2222/team/repo.git", true},
		{"git://example.com/repo.git", true},
		{"git@example.com:team/repo.git", true},
		{"example.com:team/repo.git", true},
		{"[::1]:repo.git", true},
		{"file:///srv/git/repo.git", true},
		{"/srv/git/repo.git", true},
		{"/srv/git with spaces/repo.git", true},
		{"../repo.git", true},
		{"./repo.git", true},
		{"~/repo.git", true},
		{"repo.git", true},
		{"  https://example.com/repo.git  ", true},
		{"", false},
		{"   ", false},
		{"-uevil", false},
		{"--upload-pack=touch /tmp/x", false},
		{"https://", false},
		{"https:///repo.git", false},
		{"https://exa mple.com/repo.git", false},
		{"ftp://example.com/repo.git", false},
		{"file://relative/repo.git", false},
		{"a:b c", false},
		{"https://example.com/repo\n.git", false},
		{"host:", false},
	}
	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			msg := ValidateRemoteURL(tc.url)
			if (msg == "") != tc.ok {
				t.Errorf("ValidateRemoteURL(%q) = %q, ok want %v", tc.url, msg, tc.ok)
			}
		})
	}
}
