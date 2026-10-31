package gitscm

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The documents and titles of the changes of every status in every group, as VS Code's ResourceCommandResolver finds them. VS Code
// has no test of the resolver, so the table is written from its methods (getLeftResource, getRightResource, getTitle, getResources).
//
// Ported from VS Code: extensions/git/src/repository.ts (ResourceCommandResolver)
func TestTheResolverFindsTheDocumentsAndTheTitleOfEveryStatusInEveryGroup(t *testing.T) {
	const (
		id   = "0123456789ab"
		root = "/work/repo"
		p    = "emetos-virtual:/git/" + id + "/"
		file = "file:///work/repo/src/a.go"
	)
	git1 := func(ref string) string { return p + ref + "/src/a.go" }
	tests := []struct {
		name   string
		group  string
		status git.Status
		from   string
		// the expected sides
		left, right, original, modified, title string
	}{
		{name: "staged modification", group: groupIndex, status: git.StatusIndexModified,
			left: git1("HEAD"), right: git1(""), original: git1("HEAD"), modified: git1(""), title: "a.go (Index)"},
		{name: "staged addition", group: groupIndex, status: git.StatusIndexAdded,
			right: git1(""), modified: git1(""), title: "a.go (Index)"},
		{name: "staged deletion", group: groupIndex, status: git.StatusIndexDeleted,
			right: git1("HEAD"), original: git1("HEAD"), title: "a.go (Deleted)"},
		{name: "staged rename", group: groupIndex, status: git.StatusIndexRenamed, from: "/work/repo/src/old.go",
			left: p + "HEAD/src/old.go", right: git1(""), original: p + "HEAD/src/old.go", modified: git1(""), title: "a.go (Index)"},
		{name: "staged copy", group: groupIndex, status: git.StatusIndexCopied,
			right: git1(""), modified: git1("")},
		{name: "modified in the work tree", group: groupWorking, status: git.StatusModified,
			left: git1("~"), right: file, original: git1("~"), modified: file, title: "a.go (Working Tree)"},
		{name: "modified after a staged rename", group: groupWorking, status: git.StatusModified, from: "/work/repo/src/old.go",
			left: git1("~"), right: file, original: git1("~"), modified: file, title: "a.go (Working Tree)"},
		{name: "deleted in the work tree", group: groupWorking, status: git.StatusDeleted,
			right: git1("HEAD"), original: git1("HEAD"), title: "a.go (Deleted)"},
		{name: "untracked", group: groupUntracked, status: git.StatusUntracked,
			right: file, modified: file, title: "a.go (Untracked)"},
		{name: "untracked in the changes group", group: groupWorking, status: git.StatusUntracked,
			right: file, modified: file, title: "a.go (Untracked)"},
		{name: "ignored", group: groupWorking, status: git.StatusIgnored,
			right: file, modified: file},
		{name: "intent to add", group: groupWorking, status: git.StatusIntentToAdd,
			right: file, modified: file, title: "a.go (Intent to add)"},
		{name: "intent to rename", group: groupWorking, status: git.StatusIntentToRename, from: "/work/repo/src/old.go",
			left: p + "HEAD/src/old.go", right: file, original: p + "HEAD/src/old.go", modified: file, title: "a.go (Intent to add)"},
		{name: "type changed", group: groupWorking, status: git.StatusTypeChanged,
			left: git1("HEAD"), right: file, original: git1("HEAD"), modified: file, title: "a.go (Type changed)"},
		{name: "both deleted", group: groupMerge, status: git.StatusBothDeleted},
		{name: "added by us", group: groupMerge, status: git.StatusAddedByUs},
		{name: "added by them", group: groupMerge, status: git.StatusAddedByThem},
		{name: "deleted by us", group: groupMerge, status: git.StatusDeletedByUs,
			left: git1("~1"), right: git1("~3"), original: git1("~1"), title: "a.go (Theirs)"},
		{name: "deleted by them", group: groupMerge, status: git.StatusDeletedByThem,
			left: git1("~1"), right: git1("~2"), original: git1("~1"), title: "a.go (Ours)"},
		{name: "both added", group: groupMerge, status: git.StatusBothAdded,
			right: file, modified: file, title: "a.go (Working Tree)"},
		{name: "both modified", group: groupMerge, status: git.StatusBothModified,
			right: file, modified: file, title: "a.go (Working Tree)"},
	}
	seen := map[git.Status]bool{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seen[tc.status] = true
			got := resolveSides(id, root, tc.group, resource{path: "/work/repo/src/a.go", status: tc.status, from: tc.from}, nil)
			want := sides{Left: tc.left, Right: tc.right, Original: tc.original, Modified: tc.modified, Title: tc.title}
			if got != want {
				t.Errorf("sides\n got %+v\nwant %+v", got, want)
			}
		})
	}
	// every status of VS Code's list is in the table
	for s := git.StatusIndexModified; s <= git.StatusBothModified; s++ {
		if !seen[s] {
			t.Errorf("status %d is not in the table", s)
		}
	}
}

func TestTheDiffOfASubmoduleIsItsDiffText(t *testing.T) {
	const id, root = "0123456789ab", "/work/repo"
	sub := []string{"/work/repo/libs/sub"}
	for _, tc := range []struct {
		group, want string
	}{
		{groupIndex, "emetos-virtual:/git/0123456789ab/index/libs/sub.diff"},
		{groupWorking, "emetos-virtual:/git/0123456789ab/wt/libs/sub.diff"},
	} {
		got := resolveSides(id, root, tc.group, resource{path: "/work/repo/libs/sub", status: git.StatusModified}, sub)
		if got.Left != "" || got.Right != tc.want || got.Modified != tc.want || got.Original != "" || got.Title != "sub (Working Tree)" {
			t.Errorf("%s: %+v", tc.group, got)
		}
	}
	// a file with the name of the submodule's folder in another repository is not one
	got := resolveSides(id, root, groupWorking, resource{path: "/work/repo/libs/sub", status: git.StatusModified}, []string{"/work/other/libs/sub"})
	if got.Left == "" {
		t.Errorf("a file that is no submodule: %+v", got)
	}
}
