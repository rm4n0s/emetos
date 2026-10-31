package gitscm

import (
	"path/filepath"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The two documents of a change and the title of its diff editor, for every status and group (SPEC §10.30 "Diffs of changes").
//
// Ported from VS Code: extensions/git/src/repository.ts (ResourceCommandResolver: getResources, getLeftResource, getRightResource,
// getTitle)

// sides are what resolve finds for a change.
type sides struct {
	// Left and Right are the documents the diff editor compares, as the click on the row opens them: a file or a version in git. Left is
	// empty when the change is opened as one document (the right one), and Right is empty when there is nothing to open.
	Left, Right string
	// Original and Modified are the two sides of the file in the multi-file diff editor (an empty one is a file that does not exist on
	// that side: an added or a deleted file).
	Original, Modified string
	// Title is the name of the tab: "loader.go (Working Tree)".
	Title string
}

// resolveSides finds the documents of the change of a resource. root is the root of the repository and id its id in the URIs; group is
// the id of the group the row is in (groupIndex, groupWorking, groupMerge, groupUntracked); submodules are the paths of the submodules
// (absolute). The path of the resource is where the file is now; for a renamed file r.from is where it came from.
func resolveSides(id, root, group string, r resource, submodules []string) sides {
	title := titleOf(r)
	for _, sm := range submodules {
		if sm == r.path {
			ref := refSubWorkTre
			if group == groupIndex {
				ref = refSubIndex
			}
			modified := toSubmoduleURI(id, root, r.path, ref)
			return sides{Right: modified, Modified: modified, Title: title}
		}
	}
	original := r.from
	if original == "" {
		original = r.path
	}
	var left, right struct{ original, modified string }
	// getLeftResource
	switch r.status {
	case git.StatusIndexModified, git.StatusIndexRenamed, git.StatusIntentToRename, git.StatusTypeChanged:
		left.original = toGitURI(id, root, original, refHEAD)
	case git.StatusModified:
		left.original = toGitURI(id, root, r.path, refWorkTree)
	case git.StatusDeletedByUs, git.StatusDeletedByThem:
		left.original = toGitURI(id, root, r.path, refBase)
	}
	// getRightResource
	switch r.status {
	case git.StatusIndexModified, git.StatusIndexAdded, git.StatusIndexCopied, git.StatusIndexRenamed:
		right.modified = toGitURI(id, root, r.path, refIndex)
	case git.StatusIndexDeleted, git.StatusDeleted:
		right.original = toGitURI(id, root, r.path, refHEAD)
	case git.StatusDeletedByUs:
		right.original = toGitURI(id, root, r.path, refTheirs)
	case git.StatusDeletedByThem:
		right.original = toGitURI(id, root, r.path, refOurs)
	case git.StatusModified, git.StatusUntracked, git.StatusIgnored, git.StatusIntentToAdd, git.StatusIntentToRename, git.StatusTypeChanged,
		git.StatusBothAdded, git.StatusBothModified:
		right.modified = fileURI(r.path)
	}
	return sides{
		Left:     first(left.original, left.modified),
		Right:    first(right.original, right.modified),
		Original: first(left.original, right.original),
		Modified: first(left.modified, right.modified),
		Title:    title,
	}
}

// first is the first of two strings that is not empty.
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// titleOf is the name of the tab of a change's diff editor.
//
// Ported from VS Code: extensions/git/src/repository.ts (ResourceCommandResolver.getTitle)
func titleOf(r resource) string {
	base := filepath.Base(r.path)
	switch r.status {
	case git.StatusIndexModified, git.StatusIndexRenamed, git.StatusIndexAdded:
		return base + " (Index)"
	case git.StatusModified, git.StatusBothAdded, git.StatusBothModified:
		return base + " (Working Tree)"
	case git.StatusIndexDeleted, git.StatusDeleted:
		return base + " (Deleted)"
	case git.StatusDeletedByUs:
		return base + " (Theirs)"
	case git.StatusDeletedByThem:
		return base + " (Ours)"
	case git.StatusUntracked:
		return base + " (Untracked)"
	case git.StatusIntentToAdd, git.StatusIntentToRename:
		return base + " (Intent to add)"
	case git.StatusTypeChanged:
		return base + " (Type changed)"
	}
	return ""
}
