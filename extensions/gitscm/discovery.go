package gitscm

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Why a repository was found.
const (
	foundFolder    = "folder"
	foundParent    = "parent"
	foundSubfolder = "subfolder"
	foundScan      = "scan"
	foundEditor    = "editor"
	foundSubmodule = "submodule"
	foundWorktree  = "worktree"
)

// candidate is a repository the discovery found.
type candidate struct {
	loc git.Location
	// why is one of the found constants.
	why string
}

// discoveryInput is what the discovery looks at: the folder of the window tab, the folders of the files the editors have open, and
// the settings.
type discoveryInput struct {
	folder string
	// editorDirs are the folders of the open files that are outside the folder (SPEC §10.30 "Repositories").
	editorDirs []string
	home       string
	cfg        *config
	// excluded says whether files.exclude hides the folder (its name, relative path); nil hides none.
	excluded func(rel string) bool
	// ignored says whether git.ignoredRepositories lists the root.
	ignored func(root string) bool
}

// discovered is what the discovery found.
type discovered struct {
	// open are the repositories to open now, in the order of the SPEC: the folder, subfolders and scanned folders, editors.
	open []candidate
	// parents are the repositories of the parent folders and of the open files outside the folder that git.openRepositoryInParentFolders
	// has not let in (the mode "prompt"); "always" puts them in open and "never" drops them.
	parents []candidate
}

// discover looks for the repositories with the file system only: it starts no process and reads no configuration (SPEC §10.30
// "Repositories": a repository is trusted before git runs in it). The order is that of the SPEC: the folder itself, the parent folders,
// the subfolders (git.autoRepositoryDetection true or subFolders, git.repositoryScanMaxDepth, git.repositoryScanIgnoredFolders,
// git.scanRepositories), then the repositories of the files open outside the folder (true or openEditors).
//
// Ported from VS Code: extensions/git/src/model.ts (doInitialScan, scanWorkspaceFolders, traverseWorkspaceFolder, openRepository,
// onDidChangeVisibleTextEditors).
func discover(in discoveryInput) discovered {
	var out discovered
	seen := map[string]bool{}
	add := func(list *[]candidate, c candidate) {
		if seen[c.loc.Root] || (in.ignored != nil && in.ignored(c.loc.Root)) {
			return
		}
		seen[c.loc.Root] = true
		*list = append(*list, c)
	}
	// where puts a repository found outside the folder by the mode of git.openRepositoryInParentFolders
	outside := func(loc git.Location, why string) {
		switch in.cfg.openInParentFolders {
		case "always":
			add(&out.open, candidate{loc: loc, why: why})
		case "never":
		default:
			add(&out.parents, candidate{loc: loc, why: why})
		}
	}
	autoAll := in.cfg.autoDetection == "true"
	if in.folder != "" {
		folder := resolve(in.folder)
		if loc, ok := git.FindRepository(folder); ok && !aboveHome(loc.Root, folder, in.home) {
			if loc.Root == folder {
				add(&out.open, candidate{loc: loc, why: foundFolder})
			} else if !git.Within(loc.Root, folder) {
				outside(loc, foundParent)
			}
		}
		if autoAll || in.cfg.autoDetection == "subFolders" {
			for _, dir := range subfolders(folder, in) {
				if loc, ok := git.RepositoryAt(dir); ok {
					add(&out.open, candidate{loc: loc, why: foundSubfolder})
				}
			}
			for _, rel := range in.cfg.scanRepositories {
				if rel == ".git" || filepath.IsAbs(rel) {
					continue // not supported, as in VS Code
				}
				if loc, ok := git.RepositoryAt(filepath.Join(folder, rel)); ok {
					add(&out.open, candidate{loc: loc, why: foundScan})
				}
			}
		}
	}
	if autoAll || in.cfg.autoDetection == "openEditors" {
		for _, dir := range in.editorDirs {
			dir = resolve(dir)
			loc, ok := git.FindRepository(dir)
			if !ok || seen[loc.Root] || (in.folder != "" && git.Within(loc.Root, resolve(in.folder))) {
				continue
			}
			if aboveHome(loc.Root, dir, in.home) {
				continue
			}
			outside(loc, foundEditor)
		}
	}
	return out
}

// resolve resolves the symbolic links of a folder when it can.
func resolve(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return filepath.Clean(dir)
}

// aboveHome says whether a repository is the home folder of the user or above it, seen from a folder below the home folder: such a
// repository is not "in the parent folders" (SPEC §10.30: the search goes up to the home folder), it is the user's dotfiles or the
// file system.
func aboveHome(root, from, home string) bool {
	if home == "" {
		return false
	}
	home = resolve(home)
	return git.Within(from, home) && git.Within(home, root)
}

// subfolders lists the folders below the folder to a depth of git.repositoryScanMaxDepth (-1 is unlimited), breadth first, without
// the folder itself, without ".git" folders and the names of git.repositoryScanIgnoredFolders, and without the folders that
// files.exclude hides.
func subfolders(folder string, in discoveryInput) []string {
	type item struct {
		dir   string
		depth int
	}
	var out []string
	queue := []item{{folder, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(cur.dir)
		if err != nil {
			continue
		}
		if cur.depth != 0 {
			out = append(out, cur.dir)
		}
		if in.cfg.scanMaxDepth != -1 && cur.depth >= in.cfg.scanMaxDepth {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || name == ".git" || slices.Contains(in.cfg.scanIgnoredFolders, name) {
				continue
			}
			dir := filepath.Join(cur.dir, name)
			if in.excluded != nil {
				if rel, err := filepath.Rel(folder, dir); err == nil && in.excluded(filepath.ToSlash(rel)) {
					continue
				}
			}
			queue = append(queue, item{dir, cur.depth + 1})
		}
	}
	return out
}

// excludedBy makes the function that tells whether files.exclude hides a folder: a pattern is a name or a glob of one path element,
// optionally with the prefix "**/" or the suffix "/**", which is all that hides a folder from the repository scan.
func excludedBy(patterns map[string]bool) func(rel string) bool {
	if len(patterns) == 0 {
		return nil
	}
	var names []string
	for p, on := range patterns {
		if !on {
			continue
		}
		p = strings.TrimSuffix(strings.TrimPrefix(p, "**/"), "/**")
		if p != "" {
			names = append(names, p)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return func(rel string) bool {
		base := path.Base(rel)
		for _, p := range names {
			if ok, _ := path.Match(p, base); ok {
				return true
			}
			if ok, _ := path.Match(p, rel); ok {
				return true
			}
		}
		return false
	}
}
