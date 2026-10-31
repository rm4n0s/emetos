package gitscm

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The multi-file diff editor of the changes of a group and of a commit (SPEC §10.30 "Diffs of changes"): the extension provides the
// files of the sets it opens with Documents().OpenMultiDiff, and the window asks it again when a saved editor comes back after a
// restart and when the extension says a set changed. A set is named by a key, which is all the saved editor remembers:
//
//	group/<repository id>/<group>     the files of a group of the repository now ("all" is the work tree and the untracked files)
//	commit/<repository id>/<hash>     what a commit changed against its first parent
//
// Ported from VS Code: extensions/git/src/commands.ts (viewChanges, viewStagedChanges, viewUntrackedChanges, viewCommit),
// src/vs/workbench/contrib/multiDiffEditor/browser/scmMultiDiffSourceResolver.ts (the resolvers of a group and of a history item)

// MultiDiffKind is the kind of the sets of the extension.
const MultiDiffKind = "git.changes"

// groupAll is the set of the changes of the work tree and the untracked files of a repository: Open All Changes of a repository.
const groupAll = "all"

func groupKey(id, group string) string { return "group/" + id + "/" + group }

func commitKey(id, hash string) string { return "commit/" + id + "/" + hash }

// repoSnapshot is what the provider needs of a repository, copied on the UI thread after each status and read from any goroutine.
type repoSnapshot struct {
	root       string
	handle     string
	groups     groups
	submodules []string
}

// multiState is what the extension keeps for the multi-file diff editor.
type multiState struct {
	mu    sync.Mutex
	repos map[string]repoSnapshot
	// commits are the keys of the commit sets that were opened, by repository id, so that a repository that is closed can say they are gone.
	commits map[string]map[string]bool
	reg     extension.MultiDiffRegistration
}

func (m *multiState) set(id string, s repoSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.repos == nil {
		m.repos = map[string]repoSnapshot{}
	}
	m.repos[id] = s
}

func (m *multiState) get(id string) (repoSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.repos[id]
	return s, ok
}

// remove forgets a repository and returns the commit keys that were open for it.
func (m *multiState) remove(id string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.repos, id)
	var keys []string
	for k := range m.commits[id] {
		keys = append(keys, k)
	}
	delete(m.commits, id)
	return keys
}

func (m *multiState) opened(id, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.commits == nil {
		m.commits = map[string]map[string]bool{}
	}
	if m.commits[id] == nil {
		m.commits[id] = map[string]bool{}
	}
	m.commits[id][key] = true
}

// provideMultiDiff registers the provider of the sets. A scope that cannot show documents has no multi-file editor; the extension goes
// on without it.
func (x *gitscm) provideMultiDiff() {
	reg, err := x.api.Documents().ProvideMultiDiff(extension.MultiDiffProvider{Kind: MultiDiffKind, Resources: x.multiDiffResources})
	if err != nil {
		// a host without a workbench shows no editors: nothing to warn about
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionDocumentUnavailable" {
			x.log.Warn("the multi-file diff editor cannot be provided", "err", err)
		}
		return
	}
	x.multi.reg = reg
}

// multiDiffResources is the provider: the files of the set with a key. It runs on a goroutine of the window.
func (x *gitscm) multiDiffResources(ctx context.Context, key string) ([]extension.MultiDiffResource, error) {
	kind, rest, _ := strings.Cut(key, "/")
	id, name, ok := strings.Cut(rest, "/")
	if !ok || id == "" || name == "" || (kind != "group" && kind != "commit") {
		return nil, errors.New("GitMultiDiffKeyInvalid", "this is not the key of a set of changes", "key", key)
	}
	snap, open := x.multi.get(id)
	if !open {
		return nil, errors.New("MultiDiffGone", "the repository of these changes is not open", "key", key)
	}
	if kind == "group" {
		return groupMultiDiff(id, snap, name), nil
	}
	repo := x.content.repos.get(id)
	if repo == nil {
		return nil, errors.New("MultiDiffGone", "the repository of this commit is not open", "key", key)
	}
	ch, err := repo.CommitChanges(ctx, name)
	if err != nil {
		return nil, err
	}
	return commitMultiDiff(id, snap.root, ch), nil
}

// groupOf are the resources of a group of the snapshot, each with the group it is in.
func groupOf(g groups, group string) (list []resource, in []string) {
	add := func(rs []resource, name string) {
		for _, r := range rs {
			list, in = append(list, r), append(in, name)
		}
	}
	switch group {
	case groupMerge:
		add(g.merge, groupMerge)
	case groupIndex:
		add(g.index, groupIndex)
	case groupWorking:
		add(g.working, groupWorking)
	case groupUntracked:
		add(g.untracked, groupUntracked)
	case groupAll:
		add(g.working, groupWorking)
		add(g.untracked, groupUntracked)
	}
	return list, in
}

// groupMultiDiff are the files of a group, sorted by path, with the two documents of each (resolveSides), the letter and the color of its
// status, and what the actions of its header need.
//
// Ported from VS Code: src/vs/workbench/contrib/multiDiffEditor/browser/scmMultiDiffSourceResolver.ts (ScmResolvedMultiDiffSource)
func groupMultiDiff(id string, snap repoSnapshot, group string) []extension.MultiDiffResource {
	list, in := groupOf(snap.groups, group)
	type entry struct {
		res resource
		in  string
	}
	entries := make([]entry, len(list))
	for i := range list {
		entries[i] = entry{list[i], in[i]}
	}
	sort.SliceStable(entries, func(i, j int) bool { return comparePaths(entries[i].res.path, entries[j].res.path) < 0 })
	out := make([]extension.MultiDiffResource, 0, len(entries))
	for _, e := range entries {
		s := resolveSides(id, snap.root, e.in, e.res, snap.submodules)
		if s.Original == "" && s.Modified == "" {
			continue // both sides deleted: nothing to compare
		}
		uri := fileURI(e.res.path)
		file := ""
		if s.Modified != "" {
			file = uri
		}
		out = append(out, extension.MultiDiffResource{
			Original: s.Original, Modified: s.Modified, Path: relSlash(snap.root, e.res.path), File: file,
			Letter: statusLetter(e.res.status), Color: statusColor(e.res.status), Tooltip: statusText(e.res.status),
			Context: map[string]any{"scmProvider": "git", "scmResourceGroup": e.in},
			Args: []any{map[string]any{
				extension.SCMArgSourceControl: snap.handle, extension.SCMArgGroup: e.in, extension.SCMArgURI: uri,
			}},
		})
	}
	return out
}

// commitMultiDiff are the files of a commit: each against the same path (or the old path of a rename) in the base of the commit.
//
// Ported from VS Code: extensions/git/src/commands.ts (toMultiFileDiffEditorUris)
func commitMultiDiff(id, root string, ch git.CommitChanges) []extension.MultiDiffResource {
	hash := ch.Commit.Hash
	out := make([]extension.MultiDiffResource, 0, len(ch.Changes))
	for _, c := range ch.Changes {
		abs := filepath.Join(root, filepath.FromSlash(c.Path))
		from := abs
		if c.OrigPath != "" {
			from = filepath.Join(root, filepath.FromSlash(c.OrigPath))
		}
		r := extension.MultiDiffResource{
			Path:    c.Path,
			Context: map[string]any{"scmProvider": "git", "scmResourceGroup": "commit"},
		}
		if c.Status != 'A' {
			r.Original = toGitURI(id, root, from, ch.Base)
		}
		if c.Status != 'D' {
			r.Modified = toGitURI(id, root, abs, hash)
			r.File = fileURI(abs)
		}
		r.Letter, r.Color, r.Tooltip = commitStatus(c.Status)
		out = append(out, r)
	}
	return out
}

// commitStatus is the letter, the color and the tooltip of a status letter of "git diff --name-status".
func commitStatus(s byte) (letter, color, tooltip string) {
	switch s {
	case 'A':
		return "A", "gitDecoration.addedResourceForeground", "Added"
	case 'D':
		return "D", "gitDecoration.deletedResourceForeground", "Deleted"
	case 'R':
		return "R", "gitDecoration.renamedResourceForeground", "Renamed"
	case 'C':
		return "C", "gitDecoration.renamedResourceForeground", "Copied"
	case 'T':
		return "T", "gitDecoration.modifiedResourceForeground", "Type changed"
	case 'U':
		return "!", "gitDecoration.conflictingResourceForeground", "Unmerged"
	}
	return "M", "gitDecoration.modifiedResourceForeground", "Modified"
}

// relSlash is a path below root with slashes.
func relSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// comparePaths orders paths as VS Code's comparePaths does: segment by segment, a file before the folders at its level, names without
// regard to case.
//
// Ported from VS Code: src/vs/base/common/comparers.ts (comparePaths)
func comparePaths(a, b string) int {
	as, bs := strings.Split(a, string(filepath.Separator)), strings.Split(b, string(filepath.Separator))
	for i := 0; ; i++ {
		endA, endB := i == len(as)-1, i == len(bs)-1
		if c := compareNames(as[i], bs[i]); endA && endB {
			return c
		} else if endA {
			return -1
		} else if endB {
			return 1
		} else if c != 0 {
			return c
		}
	}
}

// compareNames compares two names without regard to case, and by case when they are otherwise equal.
func compareNames(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// multiChanged tells the window that the sets of the repository may have changed: the files of its groups after a status, and (gone) the
// commits that were opened when the repository closes.
func (r *repository) multiChanged() {
	x := r.m.x
	x.multi.set(r.id, repoSnapshot{root: r.root, handle: r.handle, groups: r.res, submodules: r.submodules})
	if x.multi.reg != nil {
		x.multi.reg.Changed(groupKey(r.id, groupMerge), groupKey(r.id, groupIndex), groupKey(r.id, groupWorking),
			groupKey(r.id, groupUntracked), groupKey(r.id, groupAll))
	}
}

// multiGone tells the window that the sets of a repository that was closed are gone.
func (r *repository) multiGone() {
	x := r.m.x
	keys := x.multi.remove(r.id)
	if x.multi.reg != nil {
		x.multi.reg.Changed(append(keys, groupKey(r.id, groupMerge), groupKey(r.id, groupIndex), groupKey(r.id, groupWorking),
			groupKey(r.id, groupUntracked), groupKey(r.id, groupAll))...)
	}
}
