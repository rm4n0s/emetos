package gitscm

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The first seven tests are the cases of extensions/git/src/test/repositoryCache.test.ts of VS Code, with the folders of the
// window tab in place of the workspace folders.

func workspacePaths(infos []repoCacheInfo) []string {
	var out []string
	for _, i := range infos {
		out = append(out, i.WorkspacePath)
	}
	return out
}

func cacheOf(folders ...string) *repoCache {
	return newRepoCache(repoCacheOptions{Folders: func() []string { return folders }})
}

// Ported from VS Code: extensions/git/src/test/repositoryCache.test.ts ('set & get basic')
func TestRepoCacheSetAndGetBasic(t *testing.T) {
	c := cacheOf("/workspace/repo")
	c.set("https://example.com/repo.git", "/workspace/repo")
	got, ok := c.get("https://example.com/repo.git")
	if !ok || !reflect.DeepEqual(workspacePaths(got), []string{"/workspace/repo"}) {
		t.Errorf("got %v %v", got, ok)
	}
}

// Ported from VS Code: extensions/git/src/test/repositoryCache.test.ts ('inner LRU capped at 10 entries')
func TestRepoCacheInnerLRUIsCappedAtTen(t *testing.T) {
	var folders []string
	for i := 1; i <= 12; i++ {
		folders = append(folders, fmt.Sprintf("/ws/folder-%02d", i))
	}
	c := cacheOf(folders...)
	for _, f := range folders {
		c.set("https://example.com/repo.git", f)
	}
	got, _ := c.get("https://example.com/repo.git")
	paths := workspacePaths(got)
	if len(paths) != 10 || slices.Contains(paths, "/ws/folder-01") || slices.Contains(paths, "/ws/folder-02") || !slices.Contains(paths, "/ws/folder-12") {
		t.Errorf("paths = %v", paths)
	}
}

// Ported from VS Code: extensions/git/src/test/repositoryCache.test.ts ('outer LRU capped at 30 repos')
func TestRepoCacheOuterLRUIsCappedAtThirty(t *testing.T) {
	var folders []string
	for i := 1; i <= 35; i++ {
		folders = append(folders, fmt.Sprintf("/ws/r%d", i))
	}
	c := cacheOf(folders...)
	for i, f := range folders {
		c.set(fmt.Sprintf("https://example.com/r%d.git", i+1), f)
	}
	if _, ok := c.get("https://example.com/r1.git"); ok {
		t.Error("the oldest repository was not trimmed")
	}
	if _, ok := c.get("https://example.com/r35.git"); !ok {
		t.Error("the newest repository is gone")
	}
	if c.lru.len() != 30 {
		t.Errorf("%d repositories", c.lru.len())
	}
}

// Ported from VS Code: extensions/git/src/test/repositoryCache.test.ts ('delete removes folder and prunes empty repo')
func TestRepoCacheDeleteRemovesTheFolderAndPrunesTheEmptyRepository(t *testing.T) {
	c := cacheOf("/ws/a", "/ws/b")
	repo := "https://example.com/repo.git"
	c.set(repo, "/ws/a")
	c.set(repo, "/ws/b")
	got, _ := c.get(repo)
	if p := workspacePaths(got); len(p) != 2 || !slices.Contains(p, "/ws/a") || !slices.Contains(p, "/ws/b") {
		t.Fatalf("paths = %v", p)
	}
	c.delete(repo, "/ws/a")
	got, _ = c.get(repo)
	if p := workspacePaths(got); !reflect.DeepEqual(p, []string{"/ws/b"}) {
		t.Fatalf("paths = %v", p)
	}
	c.delete(repo, "/ws/b")
	if _, ok := c.get(repo); ok {
		t.Error("the repository stayed after its last folder went")
	}
}

// Ported from VS Code: extensions/git/src/test/repositoryCache.test.ts ('normalizes URLs with trailing .git', 'normalizes URLs with
// trailing slashes and .git', 'handles URLs without .git correctly')
func TestRepoCacheNormalizesURLs(t *testing.T) {
	tests := []struct {
		name string
		set  string
		get  []string
	}{
		{"a trailing .git", "https://example.com/repo.git", []string{"https://example.com/repo.git", "https://example.com/repo"}},
		{"trailing slashes and .git", "https://example.com/repo.git///", []string{"https://example.com/repo.git///", "https://example.com/repo.git/", "https://example.com/repo.git", "https://example.com/repo/", "https://example.com/repo"}},
		{"no .git", "https://example.com/repo", []string{"https://example.com/repo", "https://example.com/repo.git"}},
		{"upper case and spaces", "  https://example.com/repo.GIT ", []string{"https://example.com/repo"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := cacheOf("/workspace/repo")
			c.set(tc.set, "/workspace/repo")
			first, ok := c.get(tc.get[0])
			if !ok {
				t.Fatalf("%q not found", tc.get[0])
			}
			for _, u := range tc.get[1:] {
				got, ok := c.get(u)
				if !ok || !reflect.DeepEqual(got, first) {
					t.Errorf("%q: %v %v, want %v", u, got, ok, first)
				}
			}
		})
	}
}

func TestRepoCacheIgnoresAClonesOutsideTheFolders(t *testing.T) {
	c := cacheOf("/ws/a")
	c.set("https://example.com/r.git", "/elsewhere/r")
	if _, ok := c.get("https://example.com/r.git"); ok {
		t.Error("a clone outside the window tab's folders was filed")
	}
	// a clone above the folder and below it are both related
	c.set("https://example.com/up.git", "/ws")
	c.set("https://example.com/down.git", "/ws/a/deeper")
	for _, u := range []string{"https://example.com/up.git", "https://example.com/down.git"} {
		if got, ok := c.get(u); !ok || got[0].WorkspacePath != "/ws/a" {
			t.Errorf("%s: %v %v", u, got, ok)
		}
	}
}

func TestRepoCacheKeepsItsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", repoCacheFile)
	now := int64(1000)
	open := func() *repoCache {
		return newRepoCache(repoCacheOptions{Path: path, Folders: func() []string { return []string{"/ws/a", "/ws/b"} }, Now: func() int64 { now++; return now }})
	}
	c := open()
	c.set("https://example.com/r.git", "/ws/a")
	c.set("https://example.com/r.git", "/ws/b")
	c.set("https://example.com/s.git", "/ws/a")
	again := open()
	got, ok := again.get("https://example.com/r.git")
	if !ok || len(got) != 2 || got[0].RepositoryPath != "/ws/a" || got[1].LastTouchedTime != 1002 {
		t.Fatalf("got %+v %v", got, ok)
	}
	recent := again.recent()
	if len(recent) != 2 || recent[0].RepositoryPath != "/ws/a" || recent[0].LastTouchedTime != 1003 || recent[1].RepositoryPath != "/ws/b" {
		t.Errorf("recent = %+v", recent)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the file: %v %v", info, err)
	}
}

func TestRepoCacheWithAFileThatCannotBeUsedIsEmpty(t *testing.T) {
	tests := []struct{ name, content string }{
		{"not JSON", "{{{"},
		{"the wrong shape", `{"a": 1}`},
		{"entries without paths", `[{"url":"u","folders":[{"folder":"/ws/a","info":{}}]}]`},
		{"huge", strings.Repeat(" ", maxCacheFileSize+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), repoCacheFile)
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			c := newRepoCache(repoCacheOptions{Path: path, Folders: func() []string { return []string{"/ws/a"} }})
			if c.lru.len() != 0 {
				t.Errorf("%d entries", c.lru.len())
			}
			c.set("https://example.com/r.git", "/ws/a") // and it still works
			if _, ok := c.get("https://example.com/r.git"); !ok {
				t.Error("the cache does not work after a bad file")
			}
		})
	}
}

func TestRepoCacheFollowsTheRemotesOfARepository(t *testing.T) {
	c := cacheOf("/ws/a")
	c.update([]git.Remote{{Name: "origin", FetchURL: "https://example.com/r.git"}, {Name: "none"}}, nil, "/ws/a")
	if _, ok := c.get("https://example.com/r.git"); !ok {
		t.Fatal("the added remote is not filed")
	}
	c.update(nil, []git.Remote{{Name: "origin", FetchURL: "https://example.com/r.git"}}, "/ws/a")
	if _, ok := c.get("https://example.com/r.git"); ok {
		t.Error("the removed remote is still filed")
	}
}

func TestTheKeyOfARepositoryURLHasNoPassword(t *testing.T) {
	tests := []struct {
		name, url, want string
	}{
		{"a plain address", "https://example.com/org/app", "https://example.com/org/app"},
		{"a .git suffix and slashes", "  https://example.com/org/app.git//  ", "https://example.com/org/app"},
		{"a user and a password", "https://ada:s3cret@example.com/org/app.git", "https://***@example.com/org/app"},
		{"a token as the user", "https://ghp_token@example.com/org/app.git", "https://***@example.com/org/app"},
		{"ssh", "ssh://git@example.com:2222/org/app.git", "ssh://***@example.com:2222/org/app"},
		{"an scp-like address has no password to hide", "git@example.com:org/app.git", "git@example.com:org/app"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeRepoURL(tc.url); got != tc.want {
				t.Errorf("normalizeRepoURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
	// two addresses that differ in the user only are the same repository
	if normalizeRepoURL("https://a:1@example.com/x.git") != normalizeRepoURL("https://b:2@example.com/x") {
		t.Error("the user information makes two keys of one repository")
	}
}
