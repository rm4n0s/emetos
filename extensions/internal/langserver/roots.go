package langserver

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// maxClimb is how many folders above the folder of the tab ResolveRoot looks at.
const maxClimb = 12

// HasMarker tells whether the folder holds a file or folder that matches one of the markers (names, globs allowed).
func HasMarker(dir string, markers []string) bool {
	if len(markers) == 0 {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		for _, m := range markers {
			if ok, _ := path.Match(m, e.Name()); ok {
				return true
			}
		}
	}
	return false
}

// ResolveRoot is the folder a server works on for the folder of a tab: the folder itself when it holds a marker, else the nearest
// folder above it that does. The climb stops after the folder that holds a .git (a repository is a boundary), before the home folder
// of the user and the root of the file system (a Cargo.toml or package.json there is not a project), and after twelve folders.
// Without markers, or when none is found, it is the folder.
func ResolveRoot(folder string, markers []string, home string) string {
	if folder == "" || len(markers) == 0 {
		return folder
	}
	folder = filepath.Clean(folder)
	home = filepath.Clean(home)
	dir := folder
	for i := 0; i <= maxClimb; i++ {
		if HasMarker(dir, markers) {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return folder // the top of the repository holds no marker
		}
		if dir == home {
			return folder // nothing above the home folder is a project of this folder
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == string(filepath.Separator) || parent == home {
			return folder
		}
		dir = parent
	}
	return folder
}

// ProjectOptions bound FindProjects.
type ProjectOptions struct {
	// Depth is how many levels below the folder are looked at: 3 when zero, and a negative number looks at the folder only.
	Depth int
	// Limit is the most projects listed; 200 when zero.
	Limit int
	// Skip are names of folders not entered, besides the usual ones (SkipFolders).
	Skip []string
}

// SkipFolders are the folders FindProjects does not enter: they hold dependencies and build output, not projects.
var SkipFolders = []string{".git", ".hg", ".svn", "node_modules", "target", "bin", "obj", "build", "dist", "out", ".venv", "venv", "__pycache__", ".gradle", ".idea", ".vscode"}

// FindProjects lists the project roots at or below the folder: the folders that hold a marker, with the folder first when it is one,
// then the others by depth and name. A project's own subfolders are still looked at (a solution holds projects). It is how a pack
// lists the solutions or the workspaces to choose from.
func FindProjects(folder string, markers []string, opts ProjectOptions) []string {
	if folder == "" || len(markers) == 0 {
		return nil
	}
	depth, limit := opts.Depth, opts.Limit
	switch {
	case depth == 0:
		depth = 3
	case depth < 0:
		depth = 0
	}
	if limit <= 0 {
		limit = 200
	}
	skip := map[string]bool{}
	for _, s := range append(append([]string{}, SkipFolders...), opts.Skip...) {
		skip[s] = true
	}
	type level struct {
		dir   string
		depth int
	}
	var found []string
	queue := []level{{filepath.Clean(folder), 0}}
	for len(queue) > 0 && len(found) < limit {
		cur := queue[0]
		queue = queue[1:]
		if HasMarker(cur.dir, markers) {
			found = append(found, cur.dir)
		}
		if cur.depth >= depth {
			continue
		}
		entries, err := os.ReadDir(cur.dir)
		if err != nil {
			continue
		}
		var subs []string
		for _, e := range entries {
			if e.IsDir() && !skip[e.Name()] && !strings.HasPrefix(e.Name(), ".") {
				subs = append(subs, e.Name())
			}
		}
		sort.Strings(subs)
		for _, s := range subs {
			queue = append(queue, level{filepath.Join(cur.dir, s), cur.depth + 1})
		}
	}
	return found
}
