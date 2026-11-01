package git

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"
)

// Reflog lists the subjects of the reflog of a ref, newest first ("branch: Created from main", "checkout: moving from main to topic");
// a ref with no reflog gives none.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.reflog)
func (r *Repo) Reflog(ctx context.Context, ref string) ([]string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return nil, errors.New("GitRevisionInvalid", "this is not a revision git accepts here", "revision", ref)
	}
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"reflog", "show", "--format=%gs", "--end-of-options", ref}, AllowExit: []int{128}})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, nil
	}
	var out []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

// SymbolicRef reads where a symbolic ref points, short ("origin/main" for refs/remotes/origin/HEAD); ok is false when the ref is not a
// symbolic ref or does not exist.
func (r *Repo) SymbolicRef(ctx context.Context, ref string) (target string, ok bool, err error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return "", false, errors.New("GitRevisionInvalid", "this is not a revision git accepts here", "revision", ref)
	}
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"symbolic-ref", "--short", "--quiet", ref}, AllowExit: []int{1, 128}})
	if err != nil {
		return "", false, err
	}
	target = strings.TrimSpace(string(res.Stdout))
	return target, res.ExitCode == 0 && target != "", nil
}

// ResolveCommit finds the commit that a revision names ("a1b2c3" is the beginning of a hash); ok is false when it names none.
func (r *Repo) ResolveCommit(ctx context.Context, rev string) (hash string, ok bool, err error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", false, nil
	}
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"rev-parse", "--verify", "--quiet", rev + "^{commit}"}, AllowExit: []int{1, 128}})
	if err != nil {
		return "", false, err
	}
	hash = strings.TrimSpace(string(res.Stdout))
	return hash, res.ExitCode == 0 && hashPattern.MatchString(hash), nil
}

// RevListOptions are the options of RevList.
type RevListOptions struct {
	// Revs are the revisions the list starts from; none is HEAD. A revision that starts with "-" is refused with the tag
	// GitRevisionInvalid.
	Revs []string
	// TopoOrder lists the parents after all their children.
	TopoOrder bool
	// Author and Grep keep the commits whose author (name or address) or message has the text, as plain text and ignoring case.
	Author, Grep string
}

// RevList lists the hashes of the commits reachable from the revisions, newest first, with "git rev-list". It is the cheap way to ask
// which commits match a text: the Graph's search joins two lists with the order of a third.
func (r *Repo) RevList(ctx context.Context, o RevListOptions) ([]string, error) {
	args := []string{"rev-list"}
	if o.TopoOrder {
		args = append(args, "--topo-order")
	}
	if o.Author != "" {
		args = append(args, "--author="+o.Author, "--fixed-strings", "--regexp-ignore-case")
	}
	if o.Grep != "" {
		args = append(args, "--grep="+o.Grep, "--fixed-strings", "--regexp-ignore-case")
	}
	if len(o.Revs) == 0 {
		args = append(args, "HEAD")
	} else {
		for _, rev := range o.Revs {
			if rev == "" || strings.HasPrefix(rev, "-") {
				return nil, errors.New("GitRevisionInvalid", "this is not a revision git accepts here", "revision", rev)
			}
		}
		args = append(args, "--end-of-options")
		args = append(args, o.Revs...)
	}
	res, err := r.read(ctx, ScopeRefs, Command{Args: args})
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "GitCommandFailed" {
			if stderr, _ := appErr.Metadata["stderr"].(string); strings.Contains(stderr, "does not have any commits yet") {
				return nil, nil
			}
		}
		return nil, err
	}
	return strings.Fields(string(res.Stdout)), nil
}
