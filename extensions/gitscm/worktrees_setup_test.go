package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The setup of a new worktree (step 24.19): the links to ignored folders and the copies of ignored files.

// ignoredWorld is a world whose repository has an ignored folder node_modules and the ignored files .env and cache/build.bin.
func ignoredWorld(t *testing.T, settings map[string]any) *wtWorld {
	t.Helper()
	w := newWtWorld(t, rigOptions{settings: settings})
	w.repo.Write("node_modules/pkg/index.js", "module.exports = 1\n")
	w.repo.Write(".env", "TOKEN=1\n")
	w.repo.Write("cache/build.bin", "bin\n")
	w.repo.Write("app.secret", "s\n")
	return w
}

func (w *wtWorld) setupNote() string {
	w.t.Helper()
	w.r.api.WaitFor("the notification of the setup", func() bool { w.r.api.Pump(); return len(notesWith(w.r, "Worktree set up")) > 0 })
	return w.r.api.Notes()[notesWith(w.r, "Worktree set up")[0]].Message
}

func TestANewWorktreeGetsTheLinksAndTheCopiesTheSettingsAskFor(t *testing.T) {
	w := ignoredWorld(t, map[string]any{
		"git.worktreeSymlinkFolders": []any{"node_modules"},
		"git.worktreeIncludeFiles":   []any{".env", "cache/", "# a comment", "a\n!b"},
	})
	w.create("$(git-branch) topic", "")
	dir := filepath.Join(w.root, "topic")
	if got, want := w.setupNote(), "Worktree set up: linked 1 of 1 folder(s) and copied 2 of 2 folder(s)/file(s)."; got != want {
		t.Errorf("notification %q, want %q", got, want)
	}
	link, err := os.Readlink(filepath.Join(dir, "node_modules"))
	if err != nil || link != filepath.Join(w.repo.Dir, "node_modules") {
		t.Errorf("link %q, %v", link, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "node_modules", "pkg", "index.js")); err != nil || string(data) != "module.exports = 1\n" {
		t.Errorf("through the link: %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, ".env")); err != nil || string(data) != "TOKEN=1\n" {
		t.Errorf(".env %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "cache", "build.bin")); err != nil || string(data) != "bin\n" {
		t.Errorf("cache/build.bin %q, %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "app.secret")); err == nil {
		t.Error("a file no pattern names was copied")
	}
	// the links and the copies are not changes of the new worktree, and the creation did not fail
	if got := w.repo.Git("-C", dir, "status", "--porcelain"); got != "" {
		t.Errorf("status of the worktree: %q", got)
	}
	if !slices.ContainsFunc(w.r.gitLines(), func(l string) bool { return strings.Contains(l, "ignoring 1 pattern(s) containing line breaks") }) {
		t.Errorf("the pattern with a line break was not reported in the Git channel: %q", w.r.gitLines())
	}
}

func TestNoSettingsNoSetup(t *testing.T) {
	w := ignoredWorld(t, nil)
	w.create("$(git-branch) topic", "")
	w.r.api.Pump()
	if len(notesWith(w.r, "Worktree set up")) != 0 {
		t.Errorf("notifications %q", w.r.noteTexts())
	}
	if _, err := os.Lstat(filepath.Join(w.root, "topic", "node_modules")); err == nil {
		t.Error("a folder was linked")
	}
}

// setupRun runs the two parts of the setup on a worktree and returns what they counted.
func setupRun(w *wtWorld, worktree string, links, includes []string) worktreeSetup {
	w.t.Helper()
	var res worktreeSetup
	linked := w.r.x.symlinkWorktreeFolders(w.rp.ctx, w.rp.repo, w.repo.Dir, worktree, links, &res)
	w.r.x.copyWorktreeIncludeFiles(w.rp.ctx, w.rp.repo, w.repo.Dir, worktree, includes, linked, &res)
	return res
}

func TestTheLinksTheSetupSkipsAndTheOnesThatFail(t *testing.T) {
	t.Run("a folder that holds the worktree is not linked", func(t *testing.T) {
		w := ignoredWorld(t, nil)
		w.repo.Write(".gitignore", "node_modules/\n.env\ncache/\n*.secret\n.worktrees/\n")
		w.repo.Commit("ignore the worktrees")
		nested := filepath.Join(w.repo.Dir, ".worktrees", "inside")
		w.repo.Git("worktree", "add", "-q", "-b", "inside", nested)
		res := setupRun(w, nested, []string{".worktrees", "node_modules"}, nil)
		if res.linked != 1 || res.linkFailed != 0 {
			t.Errorf("%+v", res)
		}
		if _, err := os.Lstat(filepath.Join(nested, ".worktrees")); err == nil {
			t.Error("the loop was linked")
		}
		if !slices.ContainsFunc(w.r.gitLines(), func(l string) bool {
			return strings.Contains(l, "skipped") && strings.Contains(l, "sourceContainsWorktree")
		}) {
			t.Errorf("the Git channel: %q", w.r.gitLines())
		}
	})
	t.Run("a folder that is there already is left alone", func(t *testing.T) {
		w := ignoredWorld(t, nil)
		dir := w.linked("one", "one")
		writeFile(t, filepath.Join(dir, "node_modules", "mine.js"), "mine\n")
		res := setupRun(w, dir, []string{"node_modules"}, nil)
		if res.linkTotal != 1 || res.linked != 0 || res.linkFailed != 0 {
			t.Errorf("%+v", res)
		}
		if info, err := os.Lstat(filepath.Join(dir, "node_modules")); err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("the folder was replaced: %v", err)
		}
	})
	t.Run("a link that cannot be made is counted", func(t *testing.T) {
		w := ignoredWorld(t, nil)
		w.repo.Write(".gitignore", "node_modules/\n.env\ncache/\n*.secret\npackages/*/dist/\n")
		w.repo.Commit("ignore dist")
		w.repo.Write("packages/a/dist/out.js", "out\n")
		dir := w.linked("one", "one")
		// a folder above the link is a symbolic link in the worktree
		external := filepath.Join(w.env.Root, "external")
		if err := os.MkdirAll(external, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(dir, "packages")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(dir, "packages")); err != nil {
			t.Fatal(err)
		}
		res := setupRun(w, dir, []string{"packages/*/dist/", "node_modules"}, nil)
		if res.linkTotal != 2 || res.linked != 1 || res.linkFailed != 1 {
			t.Errorf("%+v", res)
		}
		w.r.x.reportWorktreeSetup(res)
		w.r.api.Pump()
		if got := w.r.noteTexts(); !slices.Contains(got, "warn: Failed to create 1 worktree folder symlink(s).") || !slices.Contains(got, "info: Worktree set up: linked 1 of 2 folder(s).") {
			t.Errorf("notifications %q", got)
		}
	})
	t.Run("only the folders that are ignored, match, and hold no tracked file", func(t *testing.T) {
		w := ignoredWorld(t, nil)
		w.repo.Write("tracked-cache/keep.txt", "tracked\n")
		w.repo.Write(".gitignore", "node_modules/\n.env\ncache/\n*.secret\ntracked-cache/drop/\n")
		w.repo.Commit("track one file in a folder and ignore a subfolder")
		w.repo.Write("tracked-cache/drop/data", "d\n")
		dir := w.linked("one", "one")
		res := setupRun(w, dir, []string{"cache", "tracked-cache", "tracked-cache/drop", "*.secret", "src"}, nil)
		// cache is ignored and matches; tracked-cache holds a tracked file; tracked-cache/drop is ignored and holds none (its parent is the
		// candidate that has one); *.secret matches files, not folders; src is not ignored
		if res.linkTotal != 2 || res.linked != 2 {
			t.Errorf("%+v", res)
		}
		for _, link := range []string{"cache", "tracked-cache/drop"} {
			if info, err := os.Lstat(filepath.Join(dir, link)); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s is not a link: %v", link, err)
			}
		}
	})
}

func TestTheCopiesTheSetupSkipsAndTheOnesThatFail(t *testing.T) {
	t.Run("never over a tracked path of the worktree", func(t *testing.T) {
		w := ignoredWorld(t, nil)
		// on this branch .env is a tracked file; in the main work tree it is ignored and untracked
		w.repo.Git("branch", "tracks-env", "main")
		tracked := filepath.Join(w.env.Work, "tracks-env")
		w.repo.Git("worktree", "add", "-q", tracked, "tracks-env")
		writeFile(t, filepath.Join(tracked, ".env"), "tracked version\n")
		w.repo.Git("-C", tracked, "add", "-f", ".env")
		w.repo.Git("-C", tracked, "commit", "-q", "-m", "track .env")
		res := setupRun(w, tracked, nil, []string{".env"})
		if res.copyTotal != 0 || res.copied != 0 {
			t.Errorf("%+v", res)
		}
		if data, _ := os.ReadFile(filepath.Join(tracked, ".env")); string(data) != "tracked version\n" {
			t.Errorf(".env %q", data)
		}
	})
	t.Run("a file that is there already is not copied over", func(t *testing.T) {
		w := ignoredWorld(t, nil)
		dir := w.linked("one", "one")
		writeFile(t, filepath.Join(dir, ".env"), "mine\n")
		res := setupRun(w, dir, nil, []string{".env"})
		if res.copyTotal != 1 || res.copied != 0 || res.copyFailed != 0 {
			t.Errorf("%+v", res)
		}
		if data, _ := os.ReadFile(filepath.Join(dir, ".env")); string(data) != "mine\n" {
			t.Errorf(".env %q", data)
		}
	})
	t.Run("a copy that fails is counted", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can read anything")
		}
		w := ignoredWorld(t, nil)
		dir := w.linked("one", "one")
		if err := os.Chmod(filepath.Join(w.repo.Dir, "app.secret"), 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(w.repo.Dir, "app.secret"), 0o644) })
		res := setupRun(w, dir, nil, []string{".env", "*.secret"})
		if res.copyTotal != 2 || res.copied != 1 || res.copyFailed != 1 {
			t.Errorf("%+v", res)
		}
		w.r.x.reportWorktreeSetup(res)
		w.r.api.Pump()
		if got := w.r.noteTexts(); !slices.Contains(got, "warn: Failed to copy 1 folder(s)/file(s) to the worktree.") {
			t.Errorf("notifications %q", got)
		}
		if _, err := os.Lstat(filepath.Join(dir, "app.secret")); err == nil {
			// the target of a failed copy: the file is created before it is read, so it can stay empty
			if data, _ := os.ReadFile(filepath.Join(dir, "app.secret")); len(data) != 0 {
				t.Errorf("app.secret %q", data)
			}
		}
	})
	t.Run("links are copied as links", func(t *testing.T) {
		w := ignoredWorld(t, nil)
		dir := w.linked("one", "one")
		if err := os.Symlink("pkg/index.js", filepath.Join(w.repo.Dir, "node_modules", "entry")); err != nil {
			t.Fatal(err)
		}
		res := setupRun(w, dir, nil, []string{"node_modules/"})
		if res.copied != 1 {
			t.Fatalf("%+v", res)
		}
		if to, err := os.Readlink(filepath.Join(dir, "node_modules", "entry")); err != nil || to != "pkg/index.js" {
			t.Errorf("link %q, %v", to, err)
		}
	})
	t.Run("a copy that is cancelled stops", func(t *testing.T) {
		w := ignoredWorld(t, nil)
		dir := w.linked("one", "one")
		ctx, cancel := context.WithCancel(w.rp.ctx)
		cancel()
		copied, err := copyIgnored(ctx, filepath.Join(w.repo.Dir, "node_modules"), filepath.Join(dir, "node_modules"))
		if copied || err == nil {
			t.Errorf("copied %v, err %v", copied, err)
		}
	})
}

func TestTheCopyOfAFileFailsWithTheRouteOfItsStep(t *testing.T) {
	dir := t.TempDir()
	_, err := copyIgnored(t.Context(), filepath.Join(dir, "missing"), filepath.Join(dir, "target"))
	appErr := stepOf(t, err, "copyIgnored.GitWorktreeCopyFailed")
	if appErr != nil && appErr.Metadata["step"] != "copy" {
		t.Errorf("step %v", appErr.Metadata["step"])
	}
	// the place of the target cannot be examined: a file stands where a folder should be
	writeFile(t, filepath.Join(dir, "file"), "x")
	_, err = copyIgnored(t.Context(), filepath.Join(dir, "missing"), filepath.Join(dir, "file", "inside"))
	if appErr := stepOf(t, err, "copyIgnored.GitWorktreeCopyFailed"); appErr != nil && appErr.Metadata["step"] != "lstat" {
		t.Errorf("step %v", appErr.Metadata["step"])
	}
	// and a place where no folder can be made
	_, err = copyIgnored(t.Context(), filepath.Join(dir, "file"), filepath.Join(dir, "a", "b", "c"))
	if err != nil {
		t.Fatalf("a copy into new folders: %v", err)
	}
}

// stepOf checks the route of an error of Emetos and returns its error.
func stepOf(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	routeOf(t, err, route)
	appErr, _ := errors.FromError(err)
	return appErr
}

func TestPatternsMatchFolders(t *testing.T) {
	tests := []struct {
		patterns []string
		folder   string
		want     bool
	}{
		{[]string{"node_modules"}, "node_modules", true},
		{[]string{"node_modules"}, "packages/a/node_modules", true},
		{[]string{"node_modules/"}, "node_modules/deep", true},
		{[]string{"/node_modules"}, "packages/node_modules", false},
		{[]string{"dist", "!dist"}, "dist", false},
		{[]string{"*.js"}, "app.js", true},
		{[]string{"cache/"}, "cache", true},
		{[]string{"cache/"}, "other", false},
		{[]string{"packages/*/dist"}, "packages/a/dist", true},
		{[]string{"packages/*/dist"}, "packages/dist", false},
		{nil, "anything", false},
	}
	for _, tc := range tests {
		got := patternsMatchFolder(parseIgnore(strings.Join(tc.patterns, "\n")), tc.folder)
		if got != tc.want {
			t.Errorf("%q matches %q = %v, want %v", tc.patterns, tc.folder, got, tc.want)
		}
	}
}

func TestWorktreeFolderNamesAndNumbers(t *testing.T) {
	tests := []struct{ prefix, ref, want string }{
		{"", "feature/login", "feature-login"},
		{"dev/", "dev/login", "login"},
		{"dev/", "other/login", "other-login"},
		{"", "origin/main", "origin-main"},
		{"", "v1", "v1"},
	}
	for _, tc := range tests {
		if got := worktreeFolderName(tc.prefix, tc.ref); got != tc.want {
			t.Errorf("worktreeFolderName(%q, %q) = %q, want %q", tc.prefix, tc.ref, got, tc.want)
		}
	}
	list := []git.Worktree{{Path: "/w/app"}, {Path: "/w/x/topic"}, {Path: "/w/x/topic-1"}}
	for _, tc := range []struct{ path, want string }{
		{"/w/x/other", "/w/x/other"},
		{"/w/x/topic", "/w/x/topic-2"},
		{"/w/x/topic-1", "/w/x/topic-1-1"},
	} {
		if got := uniqueWorktreePath(list, tc.path); got != tc.want {
			t.Errorf("uniqueWorktreePath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
