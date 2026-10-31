package git

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"
)

// The commands of branches and tags (SPEC §10.30 "Branches, tags, merge and the states in between"): switch to a ref, make, rename and
// delete branches, make and delete tags, and delete a ref at a remote. The user asked for each of them, so none is a background command.
//
// A name that starts with a dash is refused with GitRefInvalid before git is started: git would read it as an option.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.checkout, branch, deleteBranch, renameBranch, tag, deleteTag,
// deleteRemoteRef, getRefs, getRemoteRefs, findTrackingBranches) and repository.ts (checkout, checkoutTracking).

// refName checks a name given to a command: it is not empty and does not start with a dash.
func refName(name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return errors.New("GitRefInvalid", "this is not the name of a branch, tag or commit", "name", name)
	}
	return nil
}

// CheckoutOptions say how CheckoutRef switches.
type CheckoutOptions struct {
	// Detached checks the ref out as a detached HEAD (--detach).
	Detached bool
	// Track makes a local branch that tracks a remote-tracking branch, named as git names it (--track): "origin/feature" gives
	// "feature".
	Track bool
	// Force throws away the local changes that the switch would overwrite (--force).
	Force bool
}

// CheckoutRef switches to a branch, a tag or a commit ("git checkout -q [--track] [--detach] [--force] <ref>"). A work tree whose changes the
// switch would overwrite fails with the tag DirtyWorkTree and an unborn branch with BranchNotYetBorn.
func (r *Repo) CheckoutRef(ctx context.Context, ref string, o CheckoutOptions) error {
	if err := refName(ref); err != nil {
		return err
	}
	args := []string{"checkout", "-q"}
	if o.Track {
		args = append(args, "--track")
	}
	if o.Detached {
		args = append(args, "--detach")
	}
	if o.Force {
		args = append(args, "--force")
	}
	args = append(args, ref)
	_, err := r.write(ctx, ScopeRefs|ScopeIndex|ScopeWorkTree, Command{Args: args})
	return err
}

// Branch makes a branch at ref (HEAD when empty), and switches to it when checkout is set, with the changes of the work tree and the index
// kept and the branch tracking nothing ("git checkout -q -b <name> --no-track [<ref>]", else "git branch -q <name> [<ref>]"). The error has
// the tags BranchAlreadyExists and InvalidBranchName.
func (r *Repo) Branch(ctx context.Context, name string, checkout bool, ref string) error {
	if err := refName(name); err != nil {
		return err
	}
	if ref != "" {
		if err := refName(ref); err != nil {
			return err
		}
	}
	args := []string{"branch", "-q", name}
	scope := ScopeRefs
	if checkout {
		args = []string{"checkout", "-q", "-b", name, "--no-track"}
		scope |= ScopeIndex | ScopeWorkTree
	}
	if ref != "" {
		args = append(args, ref)
	}
	_, err := r.write(ctx, scope, Command{Args: args})
	return err
}

// RenameBranch renames the current branch ("git branch -m <name>"). The error has the tags InvalidBranchName and BranchAlreadyExists.
func (r *Repo) RenameBranch(ctx context.Context, name string) error {
	if err := refName(name); err != nil {
		return err
	}
	_, err := r.write(ctx, ScopeRefs, Command{Args: []string{"branch", "-m", name}})
	return err
}

// DeleteBranch deletes a local branch ("git branch -d <name>", "-D" with force). A branch whose commits no other branch has fails with the
// tag BranchNotFullyMerged unless force is set; the current branch is refused by git.
func (r *Repo) DeleteBranch(ctx context.Context, name string, force bool) error {
	if err := refName(name); err != nil {
		return err
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := r.write(ctx, ScopeRefs, Command{Args: []string{"branch", flag, name}})
	return err
}

// DeleteRemoteRef deletes a branch or a tag at a remote ("git push <remote> --delete [--force] <ref>"); ref is the name at the remote
// ("feature", "v1.0"), not the remote-tracking name.
func (r *Repo) DeleteRemoteRef(ctx context.Context, remote, ref string, force bool) error {
	if err := refName(remote); err != nil {
		return err
	}
	if err := refName(ref); err != nil {
		return err
	}
	args := []string{"push", remote, "--delete"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, ref)
	_, err := r.write(ctx, ScopeRefs, Command{Args: args})
	return err
}

// Tag makes a tag at ref (HEAD when empty): a lightweight one, or an annotated one ("-a -m") when the message is not empty.
func (r *Repo) Tag(ctx context.Context, name, message, ref string) error {
	if err := refName(name); err != nil {
		return err
	}
	args := []string{"tag", name}
	if message != "" {
		args = []string{"tag", "-a", name, "-m", message}
	}
	if ref != "" {
		if err := refName(ref); err != nil {
			return err
		}
		args = append(args, ref)
	}
	_, err := r.write(ctx, ScopeRefs, Command{Args: args})
	return err
}

// DeleteTag deletes a local tag ("git tag -d <name>").
func (r *Repo) DeleteTag(ctx context.Context, name string) error {
	if err := refName(name); err != nil {
		return err
	}
	_, err := r.write(ctx, ScopeRefs, Command{Args: []string{"tag", "-d", name}})
	return err
}

// PushTags pushes every tag to a remote ("git push --tags [<remote>]"); with no remote git uses the one of the current branch. A remote that
// has the tags already is not a failure; a rejected push has the tag PushRejected.
func (r *Repo) PushTags(ctx context.Context, remote string) error {
	args := []string{"push", "--tags"}
	if remote != "" {
		if err := refName(remote); err != nil {
			return err
		}
		args = append(args, remote)
	}
	_, err := r.write(ctx, ScopeRefs, Command{Args: args})
	return err
}

// RefQuery says which refs RefsQuery lists.
type RefQuery struct {
	// Sort is "committerdate" (the newest commit first) or "alphabetically" (git's order); anything else is alphabetical.
	Sort string
	// Details adds the author and the subject of each commit (Ref.AuthorName, Ref.Subject).
	Details bool
	// Patterns are the ref prefixes ("refs/heads", "refs/remotes", "refs/tags"); none means all.
	Patterns []string
}

// RefsQuery lists the refs a query asks for, parsed as ParseRefs does.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.getRefs)
func (r *Repo) RefsQuery(ctx context.Context, q RefQuery) ([]Ref, error) {
	args := []string{"for-each-ref"}
	if q.Sort == "committerdate" {
		args = append(args, "--sort=-committerdate")
	}
	format := RefsFormat
	if q.Details {
		format = RefsDetailsFormat
	}
	args = append(args, "--format="+format)
	args = append(args, q.Patterns...)
	res, err := r.read(ctx, ScopeRefs, Command{Args: args})
	if err != nil {
		return nil, err
	}
	return ParseRefs(res.Stdout), nil
}

// FindTrackingBranches lists the local branches whose upstream is the remote-tracking branch ("origin/feature").
func (r *Repo) FindTrackingBranches(ctx context.Context, upstream string) ([]string, error) {
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"for-each-ref", "--format=%(refname:short)%00%(upstream:short)", "refs/heads"}})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		name, up, ok := strings.Cut(strings.TrimSpace(line), "\x00")
		if ok && up == upstream && name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}

// RemoteRef is a branch or a tag as a remote has it.
type RemoteRef struct {
	// Name is the short name: "feature", "v1.0".
	Name string
	// Commit is the object the remote names.
	Commit string
}

// RemoteTags lists the tags a remote has ("git ls-remote --tags <remote>"). An annotated tag is listed once, by its name. The command
// reaches the remote, so git may ask for a password and a failure can be AuthenticationFailed or RemoteConnectionError.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.getRemoteRefs)
func (r *Repo) RemoteTags(ctx context.Context, remote string) ([]RemoteRef, error) {
	if err := refName(remote); err != nil {
		return nil, err
	}
	var res Result
	err := r.Queue.Do(ctx, Op{Scope: ScopeRefs}, func(ctx context.Context) error {
		var err error
		res, err = r.Runner.Run(ctx, Command{Dir: r.Root, Args: []string{"ls-remote", "--tags", remote}})
		return err
	})
	if err != nil {
		return nil, err
	}
	return ParseRemoteTags(res.Stdout), nil
}

// ParseRemoteTags reads the output of "git ls-remote --tags": "<object>\trefs/tags/<name>" per line, the peeled "<name>^{}" lines of
// annotated tags folded into the tag.
func ParseRemoteTags(data []byte) []RemoteRef {
	var out []RemoteRef
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		hash, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		name, isTag := strings.CutPrefix(ref, "refs/tags/")
		if !ok || !isTag || hash == "" || strings.ContainsAny(name, " ") {
			continue
		}
		name = strings.TrimSuffix(name, "^{}")
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, RemoteRef{Name: name, Commit: hash})
	}
	return out
}
