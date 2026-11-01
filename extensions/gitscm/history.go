package gitscm

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The history of a repository for the Source Control Graph (SPEC §10.30 "The Source Control Graph", step 24.20): the provider that gives
// the Graph its commits in pages (git log --topo-order --decorate=full with --shortstat), the references that name them, the common
// ancestor of references, and the current references (the branch, the one it tracks and the one it was made from). The Graph draws them
// and runs the commands of this file's menus.
//
// Ported from VS Code: extensions/git/src/historyProvider.ts (GitHistoryProvider) and repository.ts (getBranchBase,
// getBranchBaseFromReflog, getDefaultBranch). Not built: the avatars of the authors, the emoji codes of a message, the links in a message
// and the hover commands of a remote (they need GitHub, D33), the chat context of a commit (Copilot), and the writing of
// branch.<name>.vscode-merge-base into the configuration of the repository.

// The Codicons of the references (VS Code's Icons: head, branch, remoteBranch, tag).
const (
	iconHead         = "target"
	iconBranch       = "git-branch"
	iconRemoteBranch = "cloud"
	iconTag          = "tag"
)

// defaultHistoryPage is the page the Graph asks for when it does not say (VS Code's 50).
const defaultHistoryPage = 50

// historyState is what the history provider of a repository keeps; the provider's functions run on other goroutines.
type historyState struct {
	mu  sync.Mutex
	reg extension.HistoryRegistration
	// current, remote and base are the references the Graph was told last.
	current, remote, base *extension.HistoryItemRef
	// baseFor is the branch the base was looked for, so that it is looked for again only when the branch changes (VS Code does the same);
	// gen counts the lookups, so that the answer of an old one is dropped.
	baseFor string
	gen     int
	// revs are the revisions of the references the last time, to find which were added, removed or moved. nil before the first status.
	revs map[string]string
}

// addHistory gives the source control its history provider, once. A scope with no Graph view (a host without a workbench) has none and
// the extension goes on without it.
func (r *repository) addHistory() {
	if r.disposed || r.hist.reg != nil || r.repo == nil {
		return
	}
	x := r.m.x
	repo, root := r.repo, r.root
	reg, err := r.src.SetHistoryProvider(extension.HistoryProvider{
		OpenCommand: "git.viewCommit",
		Items: func(ctx context.Context, o extension.HistoryOptions) ([]extension.HistoryItem, error) {
			return x.historyItems(ctx, repo, o)
		},
		Refs: func(ctx context.Context, ids []string) ([]extension.HistoryItemRef, error) {
			return x.historyRefs(ctx, repo, ids)
		},
		CommonAncestor: func(ctx context.Context, refs []string) (string, error) { return r.historyAncestor(ctx, repo, refs) },
	})
	if err != nil {
		if tag := errTag(err); tag != "ExtensionHistoryUnavailable" && tag != "ExtensionDeactivated" {
			x.log.Warn("cannot set the history provider", "root", root, "err", err)
		}
		return
	}
	r.hist.mu.Lock()
	r.hist.reg = reg
	r.hist.mu.Unlock()
}

// dropHistory takes the provider away (the repository is not trusted any more, or was closed).
func (r *repository) dropHistory() {
	r.hist.mu.Lock()
	reg := r.hist.reg
	r.hist.reg, r.hist.current, r.hist.remote, r.hist.base, r.hist.baseFor, r.hist.revs = nil, nil, nil, nil, "", nil
	r.hist.gen++
	r.hist.mu.Unlock()
	if reg != nil {
		reg.Dispose()
	}
}

// historySync is called after each status of the repository, on the UI thread: it works out the current references, tells the Graph
// when they changed, and tells it which references were added, removed or moved. A status that an automatic fetch caused is silent.
//
// Ported from VS Code: historyProvider.ts (onDidRunWriteOperation)
func (r *repository) historySync() {
	h := &r.hist
	h.mu.Lock()
	reg := h.reg
	h.mu.Unlock()
	if reg == nil || r.repo == nil {
		return
	}
	x := r.m.x
	cur, remote := r.currentRefs()
	// the references by id
	revs := map[string]string{}
	var all []extension.HistoryItemRef
	for _, ref := range r.refs {
		if hr, ok := x.toHistoryRef(ref); ok {
			revs[hr.ID] = hr.Revision
			all = append(all, hr)
		}
	}
	h.mu.Lock()
	before := h.revs
	h.revs = revs
	branchChanged := r.head.name != h.baseFor
	var base *extension.HistoryItemRef
	if branchChanged {
		h.baseFor = r.head.name
		h.gen++
		h.base = nil
	} else if h.base != nil {
		// the base moved with a fetch
		if rev, ok := revs[h.base.ID]; ok && rev != h.base.Revision {
			b := *h.base
			b.Revision = rev
			h.base = &b
		}
	}
	base = h.base
	gen, branch := h.gen, r.head.name
	h.current, h.remote = cur, remote
	h.mu.Unlock()
	reg.SetCurrentRefs(cur, remote, base)
	if branchChanged && branch != "" && cur != nil {
		r.findBase(gen, branch)
	}
	if before == nil {
		return
	}
	var change extension.HistoryRefsChange
	for _, ref := range all {
		old, had := before[ref.ID]
		switch {
		case !had:
			change.Added = append(change.Added, ref)
		case old != ref.Revision:
			change.Modified = append(change.Modified, ref)
		}
	}
	for id := range before {
		if _, kept := revs[id]; !kept {
			change.Removed = append(change.Removed, extension.HistoryItemRef{ID: id, Name: strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(id, "refs/heads/"), "refs/remotes/"), "refs/tags/")})
		}
	}
	if len(change.Added)+len(change.Removed)+len(change.Modified) == 0 {
		return
	}
	change.Silent = r.af != nil && r.af.running
	reg.RefsChanged(change)
}

// currentRefs are the reference of the checked out branch (or of the commit or the tag HEAD is detached at) and the one it tracks.
func (r *repository) currentRefs() (cur, remote *extension.HistoryItemRef) {
	h := r.head
	switch {
	case h.name != "":
		cur = &extension.HistoryItemRef{ID: "refs/heads/" + h.name, Name: h.name, Revision: h.commit, Icon: iconHead}
		if up := h.upstream; up != nil {
			ref := extension.HistoryItemRef{}
			if up.Remote == "." {
				ref.ID, ref.Name, ref.Icon = "refs/heads/"+up.Name, up.Name, iconBranch
			} else {
				ref.ID, ref.Name, ref.Icon = "refs/remotes/"+up.Remote+"/"+up.Name, up.Remote+"/"+up.Name, iconRemoteBranch
			}
			ref.Revision = r.refCommitFull(ref.ID)
			remote = &ref
		}
	case h.tag != "":
		cur = &extension.HistoryItemRef{ID: "refs/tags/" + h.tag, Name: h.tag, Revision: h.commit, Icon: iconHead}
	case h.commit != "":
		cur = &extension.HistoryItemRef{ID: h.commit, Name: h.commit, Revision: h.commit, Icon: iconHead}
	}
	return cur, remote
}

// refCommitFull is the commit a ref is at, from the last status.
func (r *repository) refCommitFull(full string) string {
	for _, ref := range r.refs {
		if ref.FullName == full {
			return ref.Commit
		}
	}
	return ""
}

// findBase looks for the branch the current one was made from, off the UI thread, and hands it to the Graph when the branch is still the
// current one.
func (r *repository) findBase(gen int, branch string) {
	x := r.m.x
	repo, ctx := r.repo, r.ctx
	refs := slices.Clone(r.refs)
	var upstream *git.Upstream
	if r.head.upstream != nil {
		u := *r.head.upstream
		upstream = &u
	}
	remotes := slices.Clone(r.remotes)
	go func() {
		base := x.branchBase(ctx, repo, branch, refs, remotes)
		x.api.Post(func() {
			h := &r.hist
			h.mu.Lock()
			if r.disposed || h.gen != gen || h.reg == nil {
				h.mu.Unlock()
				return
			}
			// the base is shown only when it is not the branch the current one tracks
			if base != nil && upstream != nil && base.ID == "refs/remotes/"+upstream.Remote+"/"+upstream.Name {
				base = nil
			}
			h.base = base
			reg, cur, remote := h.reg, h.current, h.remote
			h.mu.Unlock()
			reg.SetCurrentRefs(cur, remote, base)
		})
	}()
}

// branchBase finds the remote branch a branch was made from: the one the configuration says (the key VS Code writes), the one the
// reflog of the branch says it was created from (or, when it was created from HEAD, the branch HEAD was on), and the default branch of the
// default remote. A local branch the branch was made from counts as the branch it tracks. nil when nothing is found.
//
// Ported from VS Code: repository.ts (getBranchBase, getBranchBaseFromReflog, getDefaultBranch, getUpstreamBranch)
func (x *gitscm) branchBase(ctx context.Context, repo *git.Repo, branch string, refs []git.Ref, remotes []git.Remote) *extension.HistoryItemRef {
	remoteRef := func(name string) *extension.HistoryItemRef {
		for _, ref := range refs {
			if ref.Type == git.RefRemoteHead && ref.Name == name {
				hr, _ := x.toHistoryRef(ref)
				return &hr
			}
		}
		return nil
	}
	// resolve turns the name of a branch the reflog gave into the remote branch to use
	resolve := func(name string) *extension.HistoryItemRef {
		if r := remoteRef(name); r != nil {
			return r
		}
		for _, ref := range refs {
			if ref.Type == git.RefHead && ref.Name == name && ref.Upstream != nil && ref.Upstream.Remote != "." {
				return remoteRef(ref.Upstream.Remote + "/" + ref.Upstream.Name)
			}
		}
		return nil
	}
	if v, ok, err := repo.ConfigValue(ctx, "branch."+branch+".vscode-merge-base"); err == nil && ok && v != "" {
		if r := remoteRef(v); r != nil {
			return r
		}
	}
	if from := reflogBase(ctx, repo, branch); from != "" {
		if r := resolve(from); r != nil {
			return r
		}
	}
	// the default branch of the default remote
	var def *git.Remote
	for i := range remotes {
		if remotes[i].Name == "origin" {
			def = &remotes[i]
		}
	}
	if def == nil && len(remotes) > 0 {
		def = &remotes[0]
	}
	if def == nil {
		return nil
	}
	if target, ok, err := repo.SymbolicRef(ctx, "refs/remotes/"+def.Name+"/HEAD"); err == nil && ok {
		return remoteRef(target)
	}
	return nil
}

var (
	reCreatedFrom   = regexp.MustCompile(`^branch: Created from (.*)$`)
	reCheckoutMoved = regexp.MustCompile(`^checkout: moving from (\S+)\s`)
)

// reflogBase is the name of the branch that the reflog says a branch was created from; empty when it does not say.
func reflogBase(ctx context.Context, repo *git.Repo, branch string) string {
	entries, err := repo.Reflog(ctx, "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	var created []string
	for _, e := range entries {
		if m := reCreatedFrom.FindStringSubmatch(e); m != nil {
			created = append(created, m[1])
		}
	}
	if len(created) != 1 {
		return ""
	}
	if created[0] != "HEAD" {
		return created[0]
	}
	// created from HEAD: the branch HEAD was on when the branch was checked out
	head, err := repo.Reflog(ctx, "HEAD")
	if err != nil {
		return ""
	}
	moved := regexp.MustCompile(`^checkout: moving from .* to ` + regexp.QuoteMeta(branch) + `$`)
	var last string
	for _, e := range head {
		if moved.MatchString(e) {
			last = e
		}
	}
	if m := reCheckoutMoved.FindStringSubmatch(last); m != nil {
		return m[1]
	}
	return ""
}

// toHistoryRef turns a ref of git into a reference of the Graph; ok is false for a ref that is not a branch, a remote branch or a tag.
//
// Ported from VS Code: historyProvider.ts (toSourceControlHistoryItemRef)
func (x *gitscm) toHistoryRef(ref git.Ref) (extension.HistoryItemRef, bool) {
	short := ""
	if ref.Commit != "" {
		short = x.shortHash(ref.Commit)
	}
	out := extension.HistoryItemRef{Name: ref.Name, Revision: ref.Commit}
	switch ref.Type {
	case git.RefRemoteHead:
		out.ID, out.Icon, out.Category = "refs/remotes/"+ref.Name, iconRemoteBranch, "remote branches"
		if short != "" {
			out.Description = "Remote branch at " + short
		}
	case git.RefTag:
		out.ID, out.Icon, out.Category = "refs/tags/"+ref.Name, iconTag, "tags"
		if short != "" {
			out.Description = "Tag at " + short
		}
	case git.RefHead:
		out.ID, out.Icon, out.Category = "refs/heads/"+ref.Name, iconBranch, "branches"
		out.Description = short
	default:
		return extension.HistoryItemRef{}, false
	}
	return out, true
}

// historyRefs lists the references: the branches, then the remote branches, then the tags. With ids only the ones those prefixes name.
//
// Ported from VS Code: historyProvider.ts (provideHistoryItemRefs)
func (x *gitscm) historyRefs(ctx context.Context, repo *git.Repo, ids []string) ([]extension.HistoryItemRef, error) {
	var patterns []string
	for _, id := range ids {
		if id != "" && !strings.HasPrefix(id, "-") {
			patterns = append(patterns, id)
		}
	}
	if len(ids) > 0 && len(patterns) == 0 {
		return nil, nil
	}
	refs, err := repo.RefsQuery(ctx, x.refQuery(patterns...))
	if err != nil {
		return nil, err
	}
	var branches, remotes, tags []extension.HistoryItemRef
	for _, ref := range refs {
		hr, ok := x.toHistoryRef(ref)
		if !ok {
			continue
		}
		switch ref.Type {
		case git.RefRemoteHead:
			remotes = append(remotes, hr)
		case git.RefTag:
			tags = append(tags, hr)
		default:
			branches = append(branches, hr)
		}
	}
	return append(append(branches, remotes...), tags...), nil
}

// historyItems makes a page of commits of the history reachable from the references, or, with a filter text, of the commits whose
// author or message has the text (or whose hash starts with it).
//
// Ported from VS Code: historyProvider.ts (provideHistoryItems, _searchHistoryItems)
func (x *gitscm) historyItems(ctx context.Context, repo *git.Repo, o extension.HistoryOptions) ([]extension.HistoryItem, error) {
	if len(o.RefIDs) == 0 {
		return nil, nil
	}
	var revs []string
	for _, id := range o.RefIDs {
		if !slices.Contains(revs, id) {
			revs = append(revs, id)
		}
	}
	if slices.Contains(revs, "HEAD") {
		// A repository with no commit has no HEAD: its history is empty and git would answer with a usage error.
		if _, ok, err := repo.ResolveCommit(ctx, "HEAD"); err != nil || !ok {
			return nil, err
		}
	}
	limit := o.Limit
	if limit <= 0 {
		limit = defaultHistoryPage
	}
	var commits []git.Commit
	var err error
	if text := strings.TrimSpace(o.FilterText); text != "" {
		commits, err = searchCommits(ctx, repo, revs, text, max(o.Skip, 0), limit)
	} else {
		commits, err = repo.Log(ctx, git.LogOptions{Revs: revs, MaxEntries: limit, Skip: max(o.Skip, 0), ShortStats: true, Decorate: true, TopoOrder: true})
	}
	if err != nil {
		return nil, err
	}
	items := make([]extension.HistoryItem, 0, len(commits))
	for _, c := range commits {
		subject, _, _ := strings.Cut(c.Message, "\n")
		item := extension.HistoryItem{
			ID: c.Hash, ParentIDs: c.Parents, Subject: subject, Message: c.Message, DisplayID: x.shortHash(c.Hash),
			Author: c.AuthorName, AuthorEmail: c.AuthorEmail, Timestamp: c.AuthorDate,
			Statistics: &extension.HistoryStatistics{}, References: x.commitRefs(c),
		}
		if c.ShortStat != nil {
			item.Statistics = &extension.HistoryStatistics{Files: c.ShortStat.Files, Insertions: c.ShortStat.Insertions, Deletions: c.ShortStat.Deletions}
		}
		items = append(items, item)
	}
	return items, nil
}

var reHexPrefix = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// searchCommits finds the commits of the references whose author or message has the text, or whose hash starts with it, in the order of
// the whole history (git's --topo-order), and reads the page. git filters by author and by message natively and only the hashes come
// back, so a search over a long history costs three lists of hashes and one read of the page. Every page asks for the same lists, so
// the pages join.
func searchCommits(ctx context.Context, repo *git.Repo, revs []string, text string, skip, limit int) ([]git.Commit, error) {
	all, err := repo.RevList(ctx, git.RevListOptions{Revs: revs, TopoOrder: true})
	if err != nil {
		return nil, err
	}
	byAuthor, err := repo.RevList(ctx, git.RevListOptions{Revs: revs, Author: text})
	if err != nil {
		return nil, err
	}
	byMessage, err := repo.RevList(ctx, git.RevListOptions{Revs: revs, Grep: text})
	if err != nil {
		return nil, err
	}
	match := make(map[string]bool, len(byAuthor)+len(byMessage))
	for _, h := range byAuthor {
		match[h] = true
	}
	for _, h := range byMessage {
		match[h] = true
	}
	prefix := ""
	if reHexPrefix.MatchString(text) {
		prefix = strings.ToLower(text)
	}
	var page []string
	n := 0
	for _, h := range all {
		if !match[h] && (prefix == "" || !strings.HasPrefix(h, prefix)) {
			continue
		}
		if n++; n > skip {
			page = append(page, h)
		}
		if len(page) == limit {
			break
		}
	}
	if len(page) == 0 {
		return nil, nil
	}
	return repo.Log(ctx, git.LogOptions{Revs: page, NoWalk: true, MaxEntries: -1, ShortStats: true, Decorate: true})
}

// commitRefs are the references that point at a commit, from the full names git decorated it with, in the order branches, remote
// branches, tags and then by name. The remote HEAD ("origin/HEAD") is left out.
//
// Ported from VS Code: historyProvider.ts (_resolveHistoryItemRefs, compareSourceControlHistoryItemRef)
func (x *gitscm) commitRefs(c git.Commit) []extension.HistoryItemRef {
	var refs []extension.HistoryItemRef
	for _, name := range c.RefNames {
		switch {
		case name == "refs/remotes/origin/HEAD":
			continue
		case strings.HasPrefix(name, "HEAD -> refs/heads/"):
			refs = append(refs, extension.HistoryItemRef{ID: strings.TrimPrefix(name, "HEAD -> "), Name: strings.TrimPrefix(name, "HEAD -> refs/heads/"), Revision: c.Hash, Category: "branches", Icon: iconHead})
		case strings.HasPrefix(name, "refs/heads/"):
			refs = append(refs, extension.HistoryItemRef{ID: name, Name: strings.TrimPrefix(name, "refs/heads/"), Revision: c.Hash, Category: "branches", Icon: iconBranch})
		case strings.HasPrefix(name, "refs/remotes/"):
			if strings.HasSuffix(name, "/HEAD") {
				continue
			}
			refs = append(refs, extension.HistoryItemRef{ID: name, Name: strings.TrimPrefix(name, "refs/remotes/"), Revision: c.Hash, Category: "remote branches", Icon: iconRemoteBranch})
		case strings.HasPrefix(name, "tag: refs/tags/"):
			refs = append(refs, extension.HistoryItemRef{ID: strings.TrimPrefix(name, "tag: "), Name: strings.TrimPrefix(name, "tag: refs/tags/"), Revision: c.Hash, Category: "tags", Icon: iconTag})
		}
	}
	order := func(id string) int {
		switch {
		case strings.HasPrefix(id, "refs/heads/"):
			return 1
		case strings.HasPrefix(id, "refs/remotes/"):
			return 2
		case strings.HasPrefix(id, "refs/tags/"):
			return 3
		}
		return 99
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if oi, oj := order(refs[i].ID), order(refs[j].ID); oi != oj {
			return oi < oj
		}
		return strings.ToLower(refs[i].Name) < strings.ToLower(refs[j].Name)
	})
	return refs
}

// historyAncestor is the best common ancestor of references (names or ids): of one that is the current branch, the commit it and the
// branch it tracks, or it and the branch it was made from, have in common, else the first commit; of several, their merge base.
//
// Ported from VS Code: historyProvider.ts (resolveHistoryItemRefsCommonAncestor)
func (r *repository) historyAncestor(ctx context.Context, repo *git.Repo, refs []string) (string, error) {
	for _, ref := range refs {
		if ref == "" || strings.HasPrefix(ref, "-") {
			return "", errors.New("GitRevisionInvalid", "this is not a revision git accepts here", "revision", ref)
		}
	}
	r.hist.mu.Lock()
	cur, remote, base := r.hist.current, r.hist.remote, r.hist.base
	r.hist.mu.Unlock()
	mergeBase := func(revs ...string) (string, error) {
		hash, ok, err := repo.MergeBase(ctx, revs...)
		if err != nil || !ok {
			return "", err
		}
		return hash, nil
	}
	switch {
	case len(refs) == 0:
		return "", nil
	case len(refs) == 1 && cur != nil && refs[0] == cur.ID:
		if remote != nil {
			return mergeBase(refs[0], remote.ID)
		}
		if base != nil {
			return mergeBase(refs[0], base.ID)
		}
		if _, ok, err := repo.ResolveCommit(ctx, "HEAD"); err != nil || !ok {
			return "", err
		}
		roots, err := repo.Log(ctx, git.LogOptions{Revs: []string{"HEAD"}, RootsOnly: true, MaxEntries: 1})
		if err != nil || len(roots) == 0 {
			return "", err
		}
		return roots[0].Hash, nil
	case len(refs) > 1:
		return mergeBase(refs...)
	}
	return "", nil
}
