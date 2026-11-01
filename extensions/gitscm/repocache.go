package gitscm

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// The limits of the repository cache (VS Code's RepositoryCache).
const (
	// maxCachedRepos is how many remote URLs the cache keeps.
	maxCachedRepos = 30
	// maxCachedFolders is how many folders it keeps for one URL.
	maxCachedFolders = 10
	// repoCacheFile is the name of the cache file in $XDG_STATE_HOME/emetos.
	repoCacheFile = "git-repositories.json"
	// maxCacheFileSize bounds the cache file when it is read.
	maxCacheFileSize = 1 << 20
)

// lru is a map that keeps the most recently used entries: Set and Get move an entry to the end, and Set drops the oldest entries past
// the limit. It iterates from the oldest.
//
// Ported from VS Code: extensions/git/src/cache.ts (LRUCache)
type lru[V any] struct {
	max  int
	keys []string
	m    map[string]V
}

func newLRU[V any](max int) *lru[V] { return &lru[V]{max: max, m: map[string]V{}} }

func (c *lru[V]) touch(key string) {
	if i := slices.Index(c.keys, key); i >= 0 {
		c.keys = slices.Delete(c.keys, i, i+1)
	}
	c.keys = append(c.keys, key)
}

func (c *lru[V]) get(key string) (V, bool) {
	v, ok := c.m[key]
	if ok {
		c.touch(key)
	}
	return v, ok
}

func (c *lru[V]) set(key string, v V) {
	c.m[key] = v
	c.touch(key)
	for len(c.keys) > c.max {
		delete(c.m, c.keys[0])
		c.keys = c.keys[1:]
	}
}

func (c *lru[V]) remove(key string) bool {
	if _, ok := c.m[key]; !ok {
		return false
	}
	delete(c.m, key)
	c.keys = slices.DeleteFunc(c.keys, func(k string) bool { return k == key })
	return true
}

func (c *lru[V]) len() int { return len(c.keys) }

// repoCacheInfo says where a remote repository is cloned (VS Code's RepositoryCacheInfo).
type repoCacheInfo struct {
	// RepositoryPath is the path of the local clone.
	RepositoryPath string `json:"repositoryPath"`
	// WorkspacePath is the folder of the window tab the clone belongs to.
	WorkspacePath string `json:"workspacePath"`
	// LastTouchedTime is when the clone was last used, in milliseconds since 1970.
	LastTouchedTime int64 `json:"lastTouchedTime,omitempty"`
}

// repoCache remembers, for each remote URL, the local folders that hold a clone of it, most recently used first out: 30 URLs and 10
// folders each. The Clone command asks it for an existing clone before it makes another. It is kept in a file of the
// state folder; a file that cannot be read gives an empty cache.
//
// Ported from VS Code: extensions/git/src/repositoryCache.ts
type repoCache struct {
	mu      sync.Mutex
	lru     *lru[*lru[repoCacheInfo]]
	path    string
	folders func() []string
	now     func() int64
	log     *slog.Logger
}

// repoCacheOptions configure a repoCache.
type repoCacheOptions struct {
	// Path is the file the cache is kept in; empty keeps it in memory.
	Path string
	// Folders gives the folders of the window tab (one, in Emetos); a clone is filed under the folder it is in or above.
	Folders func() []string
	// Now is the clock, in milliseconds; time.Now when nil.
	Now func() int64
	// Log gets the failures; slog.Default when nil.
	Log *slog.Logger
}

func newRepoCache(o repoCacheOptions) *repoCache {
	c := &repoCache{lru: newLRU[*lru[repoCacheInfo]](maxCachedRepos), path: o.Path, folders: o.Folders, now: o.Now, log: o.Log}
	if c.now == nil {
		c.now = func() int64 { return time.Now().UnixMilli() }
	}
	if c.log == nil {
		c.log = slog.Default()
	}
	if c.folders == nil {
		c.folders = func() []string { return nil }
	}
	c.load()
	return c
}

// openCache makes the cache that lives next to the trust file.
func openCache(trustFile string, log *slog.Logger) *repoCache {
	return newRepoCache(repoCacheOptions{Path: filepath.Join(filepath.Dir(trustFile), repoCacheFile), Log: log})
}

var trailingGit = regexp.MustCompile(`(?i)(?:\.git)?/*$`)

// normalizeRepoURL is the key of a URL: without spaces around it, a trailing ".git" and trailing slashes, and without the user
// information ("https://user:secret@host/x" is "https://***@host/x"), so that no password or token is written to the cache file or
// shown anywhere (SPEC §10.30 "Clone, init, open").
func normalizeRepoURL(url string) string {
	return git.MaskURLs(trailingGit.ReplaceAllString(strings.TrimSpace(url), ""))
}

// setFolders gives the cache the folders of the window tab.
func (c *repoCache) setFolders(f func() []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.folders = f
}

// findWorkspace is the folder of the window tab that the clone at rootPath is in or above, the longest first.
func (c *repoCache) findWorkspace(rootPath string) (string, bool) {
	folders := slices.Clone(c.folders())
	sort.SliceStable(folders, func(i, j int) bool { return len(folders[i]) > len(folders[j]) })
	for _, f := range folders {
		if git.Within(rootPath, f) || git.Within(f, rootPath) {
			return f, true
		}
	}
	return "", false
}

// set files the clone at rootPath under the URL, as the most recently used. It does nothing when no folder of the window tab is
// related to the clone.
func (c *repoCache) set(repoURL, rootPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setLocked(repoURL, rootPath)
}

func (c *repoCache) setLocked(repoURL, rootPath string) {
	key := normalizeRepoURL(repoURL)
	folder, ok := c.findWorkspace(rootPath)
	if !ok {
		return
	}
	inner, ok := c.lru.get(key)
	if !ok {
		inner = newLRU[repoCacheInfo](maxCachedFolders)
	}
	inner.set(folder, repoCacheInfo{RepositoryPath: rootPath, WorkspacePath: folder, LastTouchedTime: c.now()})
	c.lru.set(key, inner)
	c.save()
}

// update files the clone under the URLs of the remotes it gained and takes it from the URLs of the remotes it lost.
func (c *repoCache) update(added, removed []git.Remote, rootPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range removed {
		if r.FetchURL == "" {
			continue
		}
		if folder, ok := c.findWorkspace(rootPath); ok {
			c.deleteLocked(r.FetchURL, folder)
		}
	}
	for _, r := range added {
		if r.FetchURL != "" {
			c.setLocked(r.FetchURL, rootPath)
		}
	}
}

// get lists the clones of a URL, the oldest first; false when there is none.
func (c *repoCache) get(repoURL string) ([]repoCacheInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inner, ok := c.lru.get(normalizeRepoURL(repoURL))
	if !ok {
		return nil, false
	}
	out := make([]repoCacheInfo, 0, inner.len())
	for _, k := range inner.keys {
		out = append(out, inner.m[k])
	}
	return out, true
}

// delete forgets the clone of the URL filed under the folder; a URL left with no folder goes.
func (c *repoCache) delete(repoURL, folder string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteLocked(repoURL, folder)
}

func (c *repoCache) deleteLocked(repoURL, folder string) {
	key := normalizeRepoURL(repoURL)
	inner, ok := c.lru.m[key]
	if !ok || !inner.remove(folder) {
		return
	}
	if inner.len() == 0 {
		c.lru.remove(key)
	} else {
		c.lru.set(key, inner)
	}
	c.save()
}

// recent lists the clones by the time they were last used, the newest first.
func (c *repoCache) recent() []repoCacheInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	best := map[string]repoCacheInfo{}
	for _, k := range c.lru.keys {
		inner := c.lru.m[k]
		for _, ik := range inner.keys {
			info := inner.m[ik]
			if info.RepositoryPath == "" || info.LastTouchedTime == 0 {
				continue
			}
			if old, ok := best[info.RepositoryPath]; ok && info.LastTouchedTime <= old.LastTouchedTime {
				continue
			}
			best[info.RepositoryPath] = info
		}
	}
	out := make([]repoCacheInfo, 0, len(best))
	for _, v := range best {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastTouchedTime > out[j].LastTouchedTime })
	return out
}

// cacheEntry is a URL with its folders in the file.
type cacheEntry struct {
	URL     string        `json:"url"`
	Folders []cacheFolder `json:"folders"`
}

type cacheFolder struct {
	Folder string        `json:"folder"`
	Info   repoCacheInfo `json:"info"`
}

// save writes the cache, the oldest URL first. A failure is logged: the cache is a convenience.
func (c *repoCache) save() {
	if c.path == "" {
		return
	}
	var out []cacheEntry
	for _, k := range c.lru.keys {
		e := cacheEntry{URL: k}
		inner := c.lru.m[k]
		for _, ik := range inner.keys {
			e.Folders = append(e.Folders, cacheFolder{Folder: ik, Info: inner.m[ik]})
		}
		out = append(out, e)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err == nil {
		err = statefile.Write(c.path, append(data, '\n'))
	}
	if err != nil {
		c.log.Warn("cannot save the repository cache", "path", c.path, "err", err)
	}
}

// load reads the cache file; entries that are not what was written are skipped.
func (c *repoCache) load() {
	if c.path == "" {
		return
	}
	data, err := statefile.Read(c.path, maxCacheFileSize, statefile.Writable)
	if err != nil {
		if !statefile.Missing(err) {
			c.log.Warn("[CachedRepositories][load] Failed to load cached repositories", "path", c.path, "err", err)
		}
		return
	}
	var in []cacheEntry
	if err := json.Unmarshal(data, &in); err != nil {
		c.log.Warn("[CachedRepositories][load] Failed to load cached repositories", "path", c.path, "err", err)
		return
	}
	for _, e := range in {
		inner := newLRU[repoCacheInfo](maxCachedFolders)
		for _, f := range e.Folders {
			if f.Folder == "" || f.Info.RepositoryPath == "" || f.Info.WorkspacePath == "" {
				continue
			}
			inner.set(f.Folder, f.Info)
		}
		if inner.len() > 0 {
			c.lru.set(e.URL, inner)
		}
	}
}
