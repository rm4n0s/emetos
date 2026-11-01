package git

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// nul joins entries the way git ls-files -z does.
func nul(entries ...string) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e)
		b.WriteByte(0)
	}
	return b.String()
}

// Ported from VS Code: extensions/git/src/test/worktreeInclude.test.ts (2 cases, 0 skipped)

func TestSanitizeWorktreeIncludePatternsDropsPatternsContainingLineBreaks(t *testing.T) {
	got := SanitizeWorktreeIncludePatterns([]string{".env", "a.json\n!x", "b\r", "node_modules"})
	if want := []string{".env", "node_modules"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := SanitizeWorktreeIncludePatterns(nil); len(got) != 0 {
		t.Errorf("nil gave %q", got)
	}
}

func TestResolveWorktreeIncludePathsSelectsIncludedFilesCollapsesWholeDirectoriesAndSkipsCollisions(t *testing.T) {
	ignored := nul(".env", "app/.env", "node_modules/a/index.js", "node_modules/b/index.js", "partial/keep.txt", "partial/skip.log", "mixed/ignored.txt",
		"tracked.txt", "dir-in-worktree", "file-in-worktree/child.txt", "secrets/one.txt")
	included := nul(".env", "app/.env", "node_modules/a/index.js", "node_modules/b/index.js", "partial/keep.txt", "mixed/ignored.txt",
		"tracked.txt", "dir-in-worktree", "file-in-worktree/child.txt")
	directories := nul(".env", "app/.env", "node_modules/", "partial/", "mixed/ignored.txt", "secrets/")
	worktree := nul("tracked.txt", "dir-in-worktree/file.txt", "file-in-worktree", "mixed/tracked.txt")

	tests := []struct {
		name                                     string
		ignored, included, directories, worktree string
		excluded                                 []string
		want                                     []string
	}{
		{"all", ignored, included, directories, worktree, nil,
			[]string{"node_modules", ".env", "app/.env", "partial/keep.txt", "mixed/ignored.txt"}},
		{"a folder that is linked already", ignored, included, directories, worktree, []string{"node_modules/a"},
			[]string{".env", "app/.env", "node_modules/b/index.js", "partial/keep.txt", "mixed/ignored.txt"}},
		{"the same folder with a trailing slash", ignored, included, directories, worktree, []string{"node_modules/a/"},
			[]string{".env", "app/.env", "node_modules/b/index.js", "partial/keep.txt", "mixed/ignored.txt"}},
		{"no directories listing", ignored, included, "", "", nil,
			[]string{".env", "app/.env", "node_modules/a/index.js", "node_modules/b/index.js", "partial/keep.txt", "mixed/ignored.txt", "tracked.txt", "dir-in-worktree", "file-in-worktree/child.txt"}},
		{"nothing ignored", "", included, directories, worktree, nil, nil},
		{"nothing included", ignored, "", directories, worktree, nil, nil},
		{"everything the included files hold is under a linked folder", nul("node_modules/a/x.js"), nul("node_modules/a/x.js"), nul("node_modules/"), "", []string{"node_modules"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveWorktreeIncludePaths(tc.ignored, tc.included, tc.directories, tc.worktree, tc.excluded)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// The ignored files of a real repository, as git lists them, give the paths to copy.
func TestLsIgnoredAndResolveOnARealRepository(t *testing.T) {
	_, r, repo := worktreeBase(t)
	r.Write("node_modules/a/index.js", "a\n")
	r.Write("node_modules/b/index.js", "b\n")
	r.Write(".env", "SECRET=1\n")
	r.Write("debug.log", "log\n")
	r.Write("untracked-and-not-ignored.txt", "x\n")
	ctx := t.Context()

	ignored, err := repo.LsIgnored(ctx, LsIgnoredOptions{ExcludeStandard: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := splitNulString(ignored); !reflect.DeepEqual(got, []string{".env", "debug.log", "node_modules/a/index.js", "node_modules/b/index.js"}) {
		t.Errorf("ignored = %q", got)
	}
	included, err := repo.LsIgnored(ctx, LsIgnoredOptions{ExcludePatterns: []string{"# comment", "", ".env", "node_modules/"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := splitNulString(included); !reflect.DeepEqual(got, []string{".env", "node_modules/a/index.js", "node_modules/b/index.js"}) {
		t.Errorf("included = %q", got)
	}
	dirs, err := repo.LsIgnored(ctx, LsIgnoredOptions{ExcludeStandard: true, Directory: true, NoEmptyDirectory: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := splitNulString(dirs); !reflect.DeepEqual(got, []string{".env", "debug.log", "node_modules/"}) {
		t.Errorf("directories = %q", got)
	}
	tracked, err := repo.LsFilesIn(ctx, r.Dir)
	if err != nil || !reflect.DeepEqual(splitNulString(tracked), []string{".gitignore", "a.txt"}) {
		t.Errorf("tracked = %q, %v", tracked, err)
	}
	if got := ResolveWorktreeIncludePaths(ignored, included, dirs, tracked, nil); !reflect.DeepEqual(got, []string{"node_modules", ".env"}) {
		t.Errorf("paths = %q", got)
	}
	// a folder inside an ignored one is ignored too
	out, err := repo.CheckIgnoreFolders(ctx, []string{"node_modules/a/", "src/", "node_modules/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := splitNulString(out); !reflect.DeepEqual(got, []string{"node_modules/a/", "node_modules/"}) {
		t.Errorf("check-ignore = %q", got)
	}
	if out, err := repo.CheckIgnoreFolders(ctx, nil); out != "" || err != nil {
		t.Errorf("no folders: %q, %v", out, err)
	}
	if out, err := repo.CheckIgnoreFolders(ctx, []string{"src/"}); out != "" || err != nil {
		t.Errorf("a folder that is not ignored: %q, %v (git exits with 1)", out, err)
	}
}

func TestLsIgnoredWithPatternsNeedsAPlaceForThePatternsFile(t *testing.T) {
	_, _, repo := worktreeBase(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	_, err := repo.LsIgnored(t.Context(), LsIgnoredOptions{ExcludePatterns: []string{".env"}})
	appErr := routeOf(t, err, "Repo.LsIgnored.GitWorktreePatternsFailed")
	if appErr != nil && appErr.Metadata["step"] != "mkdir" {
		t.Errorf("step = %v", appErr.Metadata["step"])
	}
}
