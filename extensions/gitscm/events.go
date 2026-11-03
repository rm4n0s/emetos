package gitscm

import (
	"context"
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// RepositoryState is the payload of the event TopicRepositoryChanged and the reply to the request TopicStatus (SPEC §10.30): where the
// repository is, where HEAD is, how it stands against its upstream, and how many changes it has in each group.
type RepositoryState struct {
	// Root is the top level of the work tree.
	Root string `json:"root"`
	// Kind is "repository", "submodule" or "worktree".
	Kind string `json:"kind"`
	// Trusted says that git runs in the repository; an untrusted repository has no state but its root.
	Trusted bool `json:"trusted"`
	// Branch is the checked out branch, empty for a detached HEAD or an untrusted repository; Commit the commit HEAD names; Tag the tag of a
	// detached HEAD.
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
	Tag    string `json:"tag,omitempty"`
	// Upstream is the branch Branch tracks ("origin/main"), Ahead and Behind count the commits against it.
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	// Changes counts the changes of each group, Count is the number the badge shows.
	Changes ChangeCounts `json:"changes"`
	Count   int          `json:"count"`
	// Merging, Rebasing and CherryPicking say which operation is in progress.
	Merging       bool `json:"merging,omitempty"`
	Rebasing      bool `json:"rebasing,omitempty"`
	CherryPicking bool `json:"cherryPicking,omitempty"`
	// Stashes is the number of stashes.
	Stashes int `json:"stashes"`
	// Limited says that the repository has more changes than git.statusLimit and only a subset is listed.
	Limited bool `json:"limited,omitempty"`
}

// ChangeCounts are the numbers of changes of the groups.
type ChangeCounts struct {
	Merge     int `json:"merge"`
	Index     int `json:"index"`
	Working   int `json:"workingTree"`
	Untracked int `json:"untracked"`
}

// state is the state of the repository as the event and the request tell it.
func (r *repository) state() RepositoryState {
	s := RepositoryState{Root: r.root, Kind: r.kind().String(), Trusted: r.trusted && r.repo != nil}
	if !s.Trusted {
		return s
	}
	h := r.head
	s.Branch, s.Commit, s.Tag = h.name, h.commit, h.tag
	if h.upstream != nil {
		s.Upstream = h.upstream.Remote + "/" + h.upstream.Name
	}
	s.Ahead, s.Behind = h.ahead, h.behind
	s.Changes = ChangeCounts{Merge: len(r.res.merge), Index: len(r.res.index), Working: len(r.res.working), Untracked: len(r.res.untracked)}
	s.Count = countFor(r.res, r.m.x.cfg().countBadge)
	s.Merging, s.Rebasing, s.CherryPicking = r.ops.merge, r.ops.rebase, r.ops.cherryPick
	s.Stashes = r.stashes
	s.Limited = r.hugeLimit > 0
	return s
}

// publish tells the other extensions that the repository changed, when its state is not what they were told last.
func (r *repository) publish() {
	x := r.m.x
	s := r.state()
	if r.published != nil && *r.published == s {
		return
	}
	r.published = &s
	if err := x.api.Events().Publish(TopicRepositoryChanged, s); err != nil {
		x.log.Warn("cannot publish the state of a repository", "root", r.root, "err", err)
	}
}

// handleStatus answers the request TopicStatus {"root": path}: the state of the repository with that root, or of the repository
// the only one open when the request names none. The reply is a RepositoryState; EventHandlerFailed with the tag GitscmNoRepository
// is the answer when there is none.
func (x *gitscm) handleStatus(_ context.Context, ev extension.Event) (any, error) {
	var req struct {
		Root string `json:"root"`
	}
	if len(ev.Data) > 0 {
		if err := json.Unmarshal(ev.Data, &req); err != nil {
			return nil, errors.NewErr("GitscmStatusRequestInvalid", err)
		}
	}
	var r *repository
	switch {
	case req.Root != "":
		r = x.model.byRoot(req.Root)
	case len(x.model.order) == 1:
		r = x.model.order[0]
	}
	if r == nil {
		return nil, errors.New("GitscmNoRepository", "no repository is open with this root", "root", req.Root)
	}
	return r.state(), nil
}
