package git

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rm4n0s/errors"
)

// The commands of the user's actions on the index and the work tree: stage, unstage, discard, remove and move (SPEC §10.30 "Stage and
// unstage", "Discard"). They go through the Queue as writes and are not background commands: the user asked for them.
//
// Every path is a literal path (GIT_LITERAL_PATHSPECS=1), so a file called a[1].go or *.txt is that file and not a pattern. A path is
// absolute or relative to the root of the repository; one outside the root is refused with GitPathOutsideRepository before git is
// started.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.add, rm, checkout, clean, revert, move, updateSubmodules).

// MaxCommandLineLength is the most characters of paths put on one command line for the commands that cannot read a pathspec file
// (clean, mv, submodule update): VS Code's MAX_CLI_LENGTH.
const MaxCommandLineLength = 30000

// literalPathspecs makes git read every path as a plain path.
const literalPathspecs = "GIT_LITERAL_PATHSPECS=1"

// mixedNeedsMerge is what "git reset" prints for a path that is unmerged: it exits with 1 and that is no failure of the command.
var mixedNeedsMerge = regexp.MustCompile(`(?m)^[^:\n]+: needs merge$`)

// write runs a command of the repository that changes it, as a write of the queue.
func (r *Repo) write(ctx context.Context, scope Scope, c Command) (Result, error) {
	c.Dir = r.Root
	if len(c.Paths) > 0 {
		c.Env = append(c.Env, literalPathspecs)
	}
	var res Result
	err := r.Queue.Do(ctx, Op{Write: true, Scope: scope}, func(ctx context.Context) error {
		var err error
		res, err = r.Runner.Run(ctx, c)
		return err
	})
	return res, err
}

// relPaths turns paths into paths relative to the root with forward slashes. An empty list gives ["."], the whole tree, when
// wholeTree is set. The error has the tags GitPathEmpty and GitPathOutsideRepository.
func (r *Repo) relPaths(paths []string, wholeTree bool) ([]string, error) {
	if len(paths) == 0 {
		if wholeTree {
			return []string{"."}, nil
		}
		return nil, nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			return nil, errors.New("GitPathEmpty", "a path is empty", "root", r.Root)
		}
		rel := p
		if filepath.IsAbs(p) {
			var err error
			rel, err = filepath.Rel(r.Root, p)
			if err != nil {
				return nil, errors.NewErr("GitPathOutsideRepository", err).SetMetadata("path", p, "root", r.Root)
			}
		}
		rel = filepath.Clean(rel)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("GitPathOutsideRepository", "the path is outside the repository", "path", p, "root", r.Root)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

// AddOptions are the options of Add.
type AddOptions struct {
	// Update stages only the files git already tracks ("git add -u"); otherwise new files and deletions are staged too ("-A").
	Update bool
}

// Add stages the paths ("git add -A -- <paths>", with Update "git add -u"): a changed file, a new one, a deleted one and a whole
// folder. With no path it stages the whole tree.
func (r *Repo) Add(ctx context.Context, paths []string, o AddOptions) error {
	rel, err := r.relPaths(paths, true)
	if err != nil {
		return err
	}
	flag := "-A"
	if o.Update {
		flag = "-u"
	}
	_, err = r.write(ctx, ScopeIndex, Command{Args: []string{"add", flag}, Paths: rel})
	return err
}

// AddIntentToAdd records that the untracked paths will be added later ("git add -N"): they show in the index as new files with no
// content, so their changes can be staged by ranges and a diff lists them.
func (r *Repo) AddIntentToAdd(ctx context.Context, paths []string) error {
	rel, err := r.relPaths(paths, false)
	if err != nil {
		return err
	}
	if len(rel) == 0 {
		return nil
	}
	_, err = r.write(ctx, ScopeIndex, Command{Args: []string{"add", "-N"}, Paths: rel})
	return err
}

// HasCommit says whether HEAD points to a commit: false in a repository before its first commit.
func (r *Repo) HasCommit(ctx context.Context) (bool, error) {
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"rev-parse", "--verify", "--quiet", "HEAD^{commit}"}, AllowExit: []int{1}})
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}

// Reset unstages the paths: their index entries go back to HEAD and the work tree is not touched ("git reset -q HEAD -- <paths>").
// Before the first commit there is no HEAD to go back to and the entries are removed from the index ("git rm --cached -r -q").
// With no path it unstages everything. Unmerged paths are left as they are, which git reports with a status of 1 that is no failure.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.revert).
func (r *Repo) Reset(ctx context.Context, paths []string) error {
	rel, err := r.relPaths(paths, true)
	if err != nil {
		return err
	}
	has, err := r.HasCommit(ctx)
	if err != nil {
		return err
	}
	if !has {
		_, err = r.write(ctx, ScopeIndex, Command{Args: []string{"rm", "--cached", "-r", "-q"}, Paths: rel})
		return err
	}
	res, err := r.write(ctx, ScopeIndex, Command{Args: []string{"reset", "-q", "HEAD"}, Paths: rel})
	if err != nil && mixedNeedsMerge.Match(res.Stdout) {
		return nil
	}
	return err
}

// Checkout puts the index version of the paths in the work tree ("git checkout -q -- <paths>"): modified files go back, deleted
// files come back. The error has the tag BranchNotYetBorn in a repository with no commit, where VS Code ignores it.
func (r *Repo) Checkout(ctx context.Context, paths []string) error {
	rel, err := r.relPaths(paths, false)
	if err != nil {
		return err
	}
	if len(rel) == 0 {
		return nil
	}
	_, err = r.write(ctx, ScopeWorkTree|ScopeIndex, Command{Args: []string{"checkout", "-q"}, Paths: rel})
	return err
}

// Clean deletes untracked files ("git clean -f -q -- <paths>"); tracked files are never touched, nor are ignored ones. The paths go
// in several commands when they do not fit on one command line.
func (r *Repo) Clean(ctx context.Context, paths []string) error {
	rel, err := r.relPaths(paths, false)
	if err != nil {
		return err
	}
	for _, chunk := range SplitInChunks(rel, MaxCommandLineLength) {
		if _, err := r.write(ctx, ScopeWorkTree, Command{Args: []string{"clean", "-f", "-q"}, Paths: chunk}); err != nil {
			return err
		}
	}
	return nil
}

// RmOptions are the options of Rm.
type RmOptions struct {
	// Cached removes the paths from the index and leaves the files ("--cached").
	Cached bool
	// Recursive allows folders ("-r").
	Recursive bool
	// Force removes a file that has changes ("-f"); with Cached it removes the index entry whatever the file holds.
	Force bool
}

// Rm removes the paths from the work tree and the index ("git rm -q -- <paths>"). git refuses a file that has changes, staged or
// not.
func (r *Repo) Rm(ctx context.Context, paths []string, o RmOptions) error {
	rel, err := r.relPaths(paths, false)
	if err != nil {
		return err
	}
	if len(rel) == 0 {
		return nil
	}
	args := []string{"rm", "-q"}
	if o.Cached {
		args = append(args, "--cached")
	}
	if o.Recursive {
		args = append(args, "-r")
	}
	if o.Force {
		args = append(args, "-f")
	}
	_, err = r.write(ctx, ScopeIndex|ScopeWorkTree, Command{Args: args, Paths: rel})
	return err
}

// Mv renames a file or folder, in the work tree and the index ("git mv -- <from> <to>").
func (r *Repo) Mv(ctx context.Context, from, to string) error {
	rel, err := r.relPaths([]string{from, to}, false)
	if err != nil {
		return err
	}
	_, err = r.write(ctx, ScopeIndex|ScopeWorkTree, Command{Args: []string{"mv"}, Paths: rel})
	return err
}

// UpdateSubmodules checks out the commit the index names in the submodules at the paths ("git submodule update -- <paths>"): the way
// to discard the changes of a submodule's HEAD.
func (r *Repo) UpdateSubmodules(ctx context.Context, paths []string) error {
	rel, err := r.relPaths(paths, false)
	if err != nil {
		return err
	}
	for _, chunk := range SplitInChunks(rel, MaxCommandLineLength) {
		if _, err := r.write(ctx, ScopeWorkTree|ScopeIndex, Command{Args: []string{"submodule", "update"}, Paths: chunk}); err != nil {
			return err
		}
	}
	return nil
}
