package gitscm

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// layout makes the repositories and folders of a case below a root and returns the folder of the window tab.
type layout struct {
	name string
	// make builds the case in the world and returns the folder of the window tab, the files open in editors (absolute) and the home folder.
	make func(t *testing.T, e *gittest.Env) (folder string, editors []string, home string)
	// the repositories (relative to the world's Work folder) that open, and those that wait in the parent folders, for each value of
	// git.autoRepositoryDetection (true, subFolders, openEditors, false) with git.openRepositoryInParentFolders prompt
	open    map[string][]string
	parents map[string][]string
}

func repoAt(t *testing.T, e *gittest.Env, rel string) string {
	t.Helper()
	r := e.InitAt(filepath.Join(e.Work, rel))
	r.Write("f.txt", "f\n")
	return r.Dir
}

func TestDiscoveryLayouts(t *testing.T) {
	all := []string{"true", "subFolders", "openEditors", "false"}
	same := func(v ...string) map[string][]string {
		m := map[string][]string{}
		for _, a := range all {
			m[a] = v
		}
		return m
	}
	with := func(base map[string][]string, mode string, v ...string) map[string][]string {
		out := map[string][]string{}
		for k, val := range base {
			out[k] = val
		}
		out[mode] = v
		return out
	}
	layouts := []layout{
		{name: "the folder is the root of a repository",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) { return repoAt(t, e, "a"), nil, e.Home },
			open: same("a"), parents: same()},
		{name: "the folder is inside a repository",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) {
				repoAt(t, e, "a")
				dir := filepath.Join(e.Work, "a", "src", "pkg")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				return dir, nil, e.Home
			},
			open: same(), parents: same("a")},
		{name: "two repositories in subfolders at depth 1 and 2",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) {
				repoAt(t, e, "ws/one")
				repoAt(t, e, "ws/group/two")
				return filepath.Join(e.Work, "ws"), nil, e.Home
			},
			open: with(with(same(), "true", "ws/one"), "subFolders", "ws/one"), parents: same()},
		{name: "a repository two levels down is not found at the default depth",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) {
				repoAt(t, e, "ws/group/two")
				return filepath.Join(e.Work, "ws"), nil, e.Home
			},
			open: same(), parents: same()},
		{name: "a repository inside an ignored folder",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) {
				repoAt(t, e, "ws/node_modules")
				repoAt(t, e, "ws/ok")
				return filepath.Join(e.Work, "ws"), nil, e.Home
			},
			open: with(with(same(), "true", "ws/ok"), "subFolders", "ws/ok"), parents: same()},
		{name: "a repository of a file open outside the folder",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) {
				repoAt(t, e, "other")
				if err := os.MkdirAll(filepath.Join(e.Work, "ws"), 0o755); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(e.Work, "ws"), []string{filepath.Join(e.Work, "other", "f.txt")}, e.Home
			},
			open: same(), parents: with(with(same(), "true", "other"), "openEditors", "other")},
		{name: "a folder with no repository",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) {
				if err := os.MkdirAll(filepath.Join(e.Work, "empty"), 0o755); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(e.Work, "empty"), nil, e.Home
			},
			open: same(), parents: same()},
		{name: "a bare repository is the folder",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) {
				return e.InitBare("bare.git").Dir, nil, e.Home
			},
			open: same("bare.git"), parents: same()},
		{name: "the repository above is the home folder's",
			make: func(t *testing.T, e *gittest.Env) (string, []string, string) {
				repoAt(t, e, "home")
				dir := filepath.Join(e.Work, "home", "proj")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				return dir, nil, filepath.Join(e.Work, "home")
			},
			open: same(), parents: same()},
	}
	for _, lay := range layouts {
		for _, mode := range all {
			t.Run(lay.name+"/"+mode, func(t *testing.T) {
				e := gittest.NewEnv(t)
				folder, editors, home := lay.make(t, e)
				cfg := config{autoDetection: mode, openInParentFolders: "prompt", scanMaxDepth: 1, scanIgnoredFolders: []string{"node_modules"}}
				got := discover(discoveryInput{folder: folder, editorDirs: dirsOf(editors), home: home, cfg: &cfg})
				rel := func(cs []candidate) []string {
					var out []string
					for _, c := range cs {
						r, _ := filepath.Rel(e.Work, c.loc.Root)
						out = append(out, r)
					}
					sort.Strings(out)
					return out
				}
				want := func(m map[string][]string) []string {
					v := append([]string(nil), m[mode]...)
					sort.Strings(v)
					return v
				}
				if g, w := rel(got.open), want(lay.open); !reflect.DeepEqual(g, w) && (len(g) != 0 || len(w) != 0) {
					t.Errorf("open = %v, want %v", g, w)
				}
				if g, w := rel(got.parents), want(lay.parents); !reflect.DeepEqual(g, w) && (len(g) != 0 || len(w) != 0) {
					t.Errorf("parents = %v, want %v", g, w)
				}
			})
		}
	}
}

func dirsOf(files []string) []string {
	var out []string
	for _, f := range files {
		out = append(out, filepath.Dir(f))
	}
	return out
}

func TestOpenRepositoryInParentFoldersModes(t *testing.T) {
	for mode, want := range map[string]struct{ open, parents int }{"always": {1, 0}, "never": {0, 0}, "prompt": {0, 1}} {
		t.Run(mode, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repoAt(t, e, "a")
			dir := filepath.Join(e.Work, "a", "sub")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := config{autoDetection: "true", openInParentFolders: mode, scanMaxDepth: 1}
			got := discover(discoveryInput{folder: dir, home: e.Home, cfg: &cfg})
			if len(got.open) != want.open || len(got.parents) != want.parents {
				t.Errorf("open %d parents %d", len(got.open), len(got.parents))
			}
		})
	}
}

func TestDiscoveryScansDeeperAndSkipsWhatTheSettingsSay(t *testing.T) {
	e := gittest.NewEnv(t)
	repoAt(t, e, "ws/one")
	repoAt(t, e, "ws/group/two")
	repoAt(t, e, "ws/group/deep/three")
	repoAt(t, e, "ws/skip/four")
	repoAt(t, e, "ws/excluded")
	repoAt(t, e, "ws/by-scan/five")
	repoAt(t, e, "ws/ignored")
	folder := filepath.Join(e.Work, "ws")
	cfg := config{autoDetection: "true", openInParentFolders: "prompt", scanMaxDepth: 2, scanIgnoredFolders: []string{"skip"}, scanRepositories: []string{"by-scan/five", "/abs/never", ".git"}}
	ignored := filepath.Join(folder, "ignored")
	got := discover(discoveryInput{folder: folder, home: e.Home, cfg: &cfg, excluded: excludedBy(map[string]bool{"**/excluded": true, "x": false}),
		ignored: func(root string) bool { return root == ignored }})
	var rels []string
	for _, c := range got.open {
		r, _ := filepath.Rel(folder, c.loc.Root)
		rels = append(rels, r)
	}
	sort.Strings(rels)
	want := []string{"by-scan/five", "group/two", "one"}
	if !reflect.DeepEqual(rels, want) {
		t.Errorf("got %v, want %v", rels, want)
	}
	// a depth of -1 has no limit
	cfg.scanMaxDepth = -1
	got = discover(discoveryInput{folder: folder, home: e.Home, cfg: &cfg, excluded: excludedBy(map[string]bool{"**/excluded": true}), ignored: func(root string) bool { return root == ignored }})
	var deep bool
	for _, c := range got.open {
		if filepath.Base(c.loc.Root) == "three" {
			deep = true
		}
	}
	if !deep {
		t.Error("a depth of -1 did not reach three levels")
	}
}

func TestExcludedByPatterns(t *testing.T) {
	ex := excludedBy(map[string]bool{"**/node_modules": true, "build": true, "out/**": true, "*.tmp": true, "off": false})
	tests := []struct {
		rel  string
		want bool
	}{{"node_modules", true}, {"a/node_modules", true}, {"build", true}, {"a/build", true}, {"out", true}, {"x.tmp", true}, {"off", false}, {"src", false}}
	for _, tc := range tests {
		if got := ex(tc.rel); got != tc.want {
			t.Errorf("excluded(%q) = %v", tc.rel, got)
		}
	}
	if excludedBy(nil) != nil || excludedBy(map[string]bool{"a": false}) != nil {
		t.Error("no patterns should give no function")
	}
}
