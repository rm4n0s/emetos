package gitscm

import (
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The lists of branches, remote branches and tags that the checkout, the branch, the delete and the tag commands show in a quick pick:
// a separator ("branches", "remote branches", "tags") before each kind, and per ref the label with its icon, the description (how far
// ahead and behind, how long ago) and the detail (author, short hash and subject) when git.showReferenceDetails is on.
//
// Ported from VS Code: extensions/git/src/commands.ts (RefItem, BranchItem, CheckoutItem, RefProcessor, RefItemsProcessor,
// RefItemSeparator, createCheckoutItems)

// refEntry is a row of a list of refs: the row, and the ref it stands for (nil for a separator and a command).
type refEntry struct {
	item extension.QuickPickItem
	ref  *git.Ref
	// command names the entry of a picker that is a command (the checkout picker's "Create new branch..."), "" for a ref.
	command string
}

// refListOptions say which refs a list has and how they look.
type refListOptions struct {
	// types are the kinds of ref in the order of their groups.
	types []git.RefType
	// skipCurrentBranch and skipCurrentBranchRemote leave out the branch HEAD is on and the remote branch it tracks.
	skipCurrentBranch, skipCurrentBranchRemote bool
	// keepRemoteHEAD keeps "origin/HEAD", which a list leaves out unless it is for a detached checkout.
	keepRemoteHEAD bool
	// lockProtected gives the protected local branches the lock icon (the checkout list).
	lockProtected bool
	// remoteTags describes the tags as the tags of a remote (the delete list).
	remoteTags bool
}

// refSeparator is the heading of a group of refs.
func refSeparator(t git.RefType) string {
	switch t {
	case git.RefHead:
		return "branches"
	case git.RefRemoteHead:
		return "remote branches"
	}
	return "tags"
}

// shortHash is the first characters of a hash, as many as git.commitShortHashLength says.
func (x *gitscm) shortHash(hash string) string {
	if n := x.cfg().commitShortHashLength; n > 0 && len(hash) > n {
		return hash[:n]
	}
	return hash
}

// refEntries makes the rows of a list of refs: for each kind in o.types, in that order, a separator and the refs of that kind.
func (x *gitscm) refEntries(r *repository, refs []git.Ref, o refListOptions) []refEntry {
	skip := map[string]bool{}
	if o.skipCurrentBranch && r.head.name != "" {
		skip[r.head.name] = true
	}
	if o.skipCurrentBranchRemote && r.head.upstream != nil {
		skip[r.head.upstream.Remote+"/"+r.head.upstream.Name] = true
	}
	now := time.Now()
	var out []refEntry
	for _, t := range o.types {
		var group []refEntry
		for i := range refs {
			ref := &refs[i]
			if ref.Type != t || (ref.Name == "" && ref.Commit == "") || skip[ref.Name] {
				continue
			}
			if t == git.RefRemoteHead && !o.keepRemoteHEAD && strings.HasSuffix(ref.Name, "/HEAD") {
				continue
			}
			group = append(group, refEntry{item: x.refItem(r, *ref, now, o), ref: ref})
		}
		if len(group) == 0 {
			continue
		}
		out = append(out, refEntry{item: extension.QuickPickItem{Label: refSeparator(t), Separator: true}})
		out = append(out, group...)
	}
	return out
}

// refItem is the row of one ref.
func (x *gitscm) refItem(r *repository, ref git.Ref, now time.Time, o refListOptions) extension.QuickPickItem {
	name := ref.Name
	if name == "" {
		name = x.shortHash(ref.Commit)
	}
	short := x.shortHash(ref.Commit)
	detailed := ref.Subject != "" && ref.AuthorName != "" && !ref.CommitDate.IsZero()
	var item extension.QuickPickItem
	switch ref.Type {
	case git.RefHead:
		icon := "git-branch"
		if o.lockProtected && ref.Type == git.RefHead && r.branchNameProtected(ref.Name) {
			icon = "lock"
		}
		item.Label = "$(" + icon + ") " + name
		var parts []string
		if ref.Gone {
			parts = append(parts, "0↓ 0↑")
		} else if ref.Upstream != nil && (ref.Behind > 0 || ref.Ahead > 0) {
			parts = append(parts, strconv.Itoa(ref.Behind)+"↓ "+strconv.Itoa(ref.Ahead)+"↑")
		}
		if detailed {
			parts = append(parts, fromNow(ref.CommitDate, now, true))
		}
		item.Description = short
		if len(parts) > 0 {
			item.Description = strings.Join(parts, " • ")
		}
	case git.RefRemoteHead:
		item.Label = "$(cloud) " + name
		item.Description = "Remote branch at " + short
	default:
		item.Label = "$(tag) " + name
		item.Description = "Tag at " + short
		if o.remoteTags {
			item.Description = "Remote tag at " + short
		}
	}
	if detailed {
		if ref.Type != git.RefHead {
			item.Description = fromNow(ref.CommitDate, now, true)
		}
		item.Detail = ref.AuthorName + " • " + short + " • " + ref.Subject
	}
	return item
}

// pickItems are the rows of the entries.
func pickItems(entries []refEntry) []extension.QuickPickItem {
	out := make([]extension.QuickPickItem, len(entries))
	for i, e := range entries {
		out[i] = e.item
	}
	return out
}
