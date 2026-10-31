package gitscm

import (
	"net/url"
	"path/filepath"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// fileURI is the URI of a file as the documents of Emetos spell it.
func fileURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// statusLetter is the letter of a status in the Source Control view and the Explorer.
//
// Ported from VS Code: extensions/git/src/repository.ts (Resource.getStatusLetter)
func statusLetter(s git.Status) string {
	switch s {
	case git.StatusIndexModified, git.StatusModified:
		return "M"
	case git.StatusIndexAdded, git.StatusIntentToAdd:
		return "A"
	case git.StatusIndexDeleted, git.StatusDeleted:
		return "D"
	case git.StatusIndexRenamed, git.StatusIntentToRename:
		return "R"
	case git.StatusTypeChanged:
		return "T"
	case git.StatusUntracked:
		return "U"
	case git.StatusIgnored:
		return "I"
	case git.StatusIndexCopied:
		return "C"
	case git.StatusBothDeleted, git.StatusAddedByUs, git.StatusDeletedByThem, git.StatusAddedByThem, git.StatusDeletedByUs, git.StatusBothAdded, git.StatusBothModified:
		return "!"
	}
	return ""
}

// statusText is the tooltip of a status.
//
// Ported from VS Code: extensions/git/src/repository.ts (Resource.getStatusText)
func statusText(s git.Status) string {
	switch s {
	case git.StatusIndexModified:
		return "Index Modified"
	case git.StatusModified:
		return "Modified"
	case git.StatusIndexAdded:
		return "Index Added"
	case git.StatusIndexDeleted:
		return "Index Deleted"
	case git.StatusDeleted:
		return "Deleted"
	case git.StatusIndexRenamed:
		return "Index Renamed"
	case git.StatusIndexCopied:
		return "Index Copied"
	case git.StatusUntracked:
		return "Untracked"
	case git.StatusIgnored:
		return "Ignored"
	case git.StatusIntentToAdd:
		return "Intent to Add"
	case git.StatusIntentToRename:
		return "Intent to Rename"
	case git.StatusTypeChanged:
		return "Type Changed"
	case git.StatusBothDeleted:
		return "Conflict: Both Deleted"
	case git.StatusAddedByUs:
		return "Conflict: Added By Us"
	case git.StatusDeletedByThem:
		return "Conflict: Deleted By Them"
	case git.StatusAddedByThem:
		return "Conflict: Added By Them"
	case git.StatusDeletedByUs:
		return "Conflict: Deleted By Us"
	case git.StatusBothAdded:
		return "Conflict: Both Added"
	case git.StatusBothModified:
		return "Conflict: Both Modified"
	}
	return ""
}

// statusColor is the id of the theme color of a status.
//
// Ported from VS Code: extensions/git/src/repository.ts (Resource.getStatusColor)
func statusColor(s git.Status) string {
	switch s {
	case git.StatusIndexModified:
		return "gitDecoration.stageModifiedResourceForeground"
	case git.StatusModified, git.StatusTypeChanged:
		return "gitDecoration.modifiedResourceForeground"
	case git.StatusIndexDeleted:
		return "gitDecoration.stageDeletedResourceForeground"
	case git.StatusDeleted:
		return "gitDecoration.deletedResourceForeground"
	case git.StatusIndexAdded, git.StatusIntentToAdd:
		return "gitDecoration.addedResourceForeground"
	case git.StatusIndexCopied, git.StatusIndexRenamed, git.StatusIntentToRename:
		return "gitDecoration.renamedResourceForeground"
	case git.StatusUntracked:
		return "gitDecoration.untrackedResourceForeground"
	case git.StatusIgnored:
		return "gitDecoration.ignoredResourceForeground"
	case git.StatusBothDeleted, git.StatusAddedByUs, git.StatusDeletedByThem, git.StatusAddedByThem, git.StatusDeletedByUs, git.StatusBothAdded, git.StatusBothModified:
		return "gitDecoration.conflictingResourceForeground"
	}
	return ""
}

// strikeThrough tells whether the status draws the name struck through (a deleted file).
func strikeThrough(s git.Status) bool {
	switch s {
	case git.StatusDeleted, git.StatusBothDeleted, git.StatusDeletedByThem, git.StatusDeletedByUs, git.StatusIndexDeleted:
		return true
	}
	return false
}

// The ids of the groups of a repository (VS Code's resource groups).
const (
	groupMerge     = "merge"
	groupIndex     = "index"
	groupWorking   = "workingTree"
	groupUntracked = "untracked"
)

// resource is a changed file of a group with its status.
type resource struct {
	path   string // absolute
	status git.Status
	// from is the path a renamed or copied file came from (absolute), empty for the others; path is where it is now.
	from string
}

// groupsOf is the groups of a repository as the extension keeps them.
type groups struct {
	merge, index, working, untracked []resource
}

// total is the number of resources in all the groups.
func (g groups) total() int { return len(g.merge) + len(g.index) + len(g.working) + len(g.untracked) }

// splitStatus sorts the entries of git status into the groups, as the model of the repository does: the seven unmerged codes go
// to Merge Changes, the first column to Staged Changes and the second to Changes; untracked files go to Changes ("mixed"), to
// Untracked Changes ("separate") or nowhere ("hidden"), and the same for ignored ones.
//
// Ported from VS Code: extensions/git/src/repository.ts (Repository.updateModelState)
func splitStatus(root string, entries []git.StatusEntry, untracked string) groups {
	var g groups
	for _, e := range entries {
		abs := filepath.Join(root, filepath.FromSlash(e.Path))
		merge, index, work := e.Statuses()
		switch {
		case work == git.StatusUntracked || work == git.StatusIgnored:
			switch untracked {
			case "separate":
				g.untracked = append(g.untracked, resource{path: abs, status: work})
			case "hidden":
			default:
				g.working = append(g.working, resource{path: abs, status: work})
			}
			continue
		case merge != git.StatusNone:
			g.merge = append(g.merge, resource{path: abs, status: merge})
			continue
		}
		var origin string
		if e.OrigPath != "" {
			origin = filepath.Join(root, filepath.FromSlash(e.OrigPath))
		}
		if index != git.StatusNone {
			if index == git.StatusIndexRenamed && origin == "" {
				// a rename that did not say where from is not shown as one
			} else {
				r := resource{path: abs, status: index}
				if index == git.StatusIndexRenamed {
					r.from = origin
				}
				g.index = append(g.index, r)
			}
		}
		if work != git.StatusNone {
			r := resource{path: abs, status: work}
			if origin != "" {
				r.from = origin
			}
			g.working = append(g.working, r)
		}
	}
	return g
}

// toStates turns resources into the resource states of the source control API.
func toStates(list []resource, kind git.Kind) []extension.ResourceState {
	states := make([]extension.ResourceState, len(list))
	for i, r := range list {
		states[i] = extension.ResourceState{
			URI: fileURI(r.path), ContextValue: kind.String(),
			Decorations: extension.ResourceDecorations{
				Tooltip: statusText(r.status), StrikeThrough: strikeThrough(r.status), Letter: statusLetter(r.status), Color: statusColor(r.status),
			},
		}
	}
	return states
}

// countFor is the count of the badge of a repository for git.countBadge: "all" counts every change, "tracked" leaves out the
// untracked files, "off" none.
//
// Ported from VS Code: extensions/git/src/repository.ts (Repository.setCount)
func countFor(g groups, mode string) int {
	count := g.total()
	switch mode {
	case "off":
		return 0
	case "tracked":
		count -= len(g.untracked)
		for _, r := range g.working {
			if r.status == git.StatusUntracked {
				count--
			}
		}
	}
	return count
}
