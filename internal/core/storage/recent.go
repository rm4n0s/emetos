package storage

import (
	"os"
	"path/filepath"
	"slices"
)

// MaxRecentEntries is the most folders, and the most files, the recently opened lists keep.
//
// Ported from VS Code: src/vs/platform/workspaces/electron-main/workspacesHistoryMainService.ts (MAX_TOTAL_RECENT_ENTRIES)
const MaxRecentEntries = 500

// commonFilesFilter are the names of the files that are never put in the recently opened list: the files git
// makes for a commit message, a merge and a rebase.
//
// Ported from VS Code: src/vs/platform/workspaces/electron-main/workspacesHistoryMainService.ts (COMMON_FILES_FILTER)
var commonFilesFilter = []string{"COMMIT_EDITMSG", "MERGE_MSG", "git-rebase-todo"}

// RecentlyOpened are the folders and the files the user opened, the most recent first, that File > Open
// Recent and the Open Recent picker list (SPEC §10.10). The paths are absolute and cleaned. The lists keep at
// most MaxRecentEntries each, and a path is in a list once.
type RecentlyOpened struct {
	// Folders are the folders opened in a window.
	Folders []string `json:"folders"`
	// Files are the files that were opened in an editor.
	Files []string `json:"files"`
}

// AddFolder puts a folder first; a folder that is in the list moves there. A path that is not absolute is not
// added. It reports whether the list changed.
func (r *RecentlyOpened) AddFolder(path string) bool { return addFirst(&r.Folders, path) }

// AddFile puts a file first, like AddFolder. The files git makes for commit messages, merges and rebases
// (COMMIT_EDITMSG, MERGE_MSG, git-rebase-todo) are not added.
func (r *RecentlyOpened) AddFile(path string) bool {
	if slices.Contains(commonFilesFilter, filepath.Base(path)) {
		return false
	}
	return addFirst(&r.Files, path)
}

func addFirst(list *[]string, path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	path = filepath.Clean(path)
	if len(*list) > 0 && (*list)[0] == path {
		return false
	}
	if i := slices.Index(*list, path); i >= 0 {
		*list = slices.Delete(*list, i, i+1)
	}
	*list = slices.Insert(*list, 0, path)
	if len(*list) > MaxRecentEntries {
		*list = (*list)[:MaxRecentEntries]
	}
	return true
}

// Remove takes a path out of both lists and reports whether it was in one.
func (r *RecentlyOpened) Remove(path string) bool {
	path = filepath.Clean(path)
	removed := false
	for _, list := range []*[]string{&r.Folders, &r.Files} {
		if i := slices.Index(*list, path); i >= 0 {
			*list = slices.Delete(*list, i, i+1)
			removed = true
		}
	}
	return removed
}

// Clear empties both lists.
func (r *RecentlyOpened) Clear() { r.Folders, r.Files = nil, nil }

// Empty tells whether both lists are empty.
func (r RecentlyOpened) Empty() bool { return len(r.Folders) == 0 && len(r.Files) == 0 }

// Existing gives the lists without the paths that exists reports false for: a folder or a file that is gone is
// not offered. The stored lists are not changed, so a path that comes back is in them again.
func (r RecentlyOpened) Existing(exists func(path string) bool) RecentlyOpened {
	keep := func(list []string) []string {
		var out []string
		for _, p := range list {
			if exists(p) {
				out = append(out, p)
			}
		}
		return out
	}
	return RecentlyOpened{Folders: keep(r.Folders), Files: keep(r.Files)}
}

// Clone copies the lists.
func (r RecentlyOpened) Clone() RecentlyOpened {
	return RecentlyOpened{Folders: slices.Clone(r.Folders), Files: slices.Clone(r.Files)}
}

// DiskExists tells whether something is at path; it is the exists function of Existing.
func DiskExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
