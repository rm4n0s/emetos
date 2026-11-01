package git

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// Linked worktrees (SPEC §10.30 "Worktrees"): the list of the worktrees of a repository, and the commands that add and remove one.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.getWorktrees, addWorktree, deleteWorktree) and
// extensions/git/src/repository.ts (getWorktreeDetails)

// Worktree is a work tree of a repository, the main one or a linked one.
type Worktree struct {
	// Name is the name of the worktree's folder.
	Name string
	// Path is the absolute path of the work tree, as git records it.
	Path string
	// Head is the commit the work tree has checked out (HEAD).
	Head string
	// Ref is the full name of the branch the work tree has checked out ("refs/heads/feature"); empty for a detached HEAD.
	Ref string
	// Detached says that HEAD names a commit, not a branch.
	Detached bool
	// Main says that this is the main work tree, the one with the .git folder.
	Main bool
	// Locked says that "git worktree lock" protects the worktree from being pruned or removed; LockReason is the reason given.
	Locked     bool
	LockReason string
	// Prunable says that the worktree's folder is gone, so that "git worktree prune" would remove its record.
	Prunable bool
	// Subject is the first line of the message of the commit HEAD names, set by WorktreeDetails.
	Subject string
}

// Branch is the short name of the branch the work tree has checked out, "" for a detached HEAD.
func (w Worktree) Branch() string { return strings.TrimPrefix(w.Ref, "refs/heads/") }

// ParseWorktrees reads the output of "git worktree list --porcelain": records of "worktree <path>", "HEAD <hash>", "branch <ref>" or
// "detached", "locked [<reason>]" and "prunable [<reason>]" lines, apart by an empty line. The first record is the main work tree; the
// record of a bare repository ("bare") is left out, as VS Code leaves it out.
func ParseWorktrees(data []byte) []Worktree {
	var out []Worktree
	var cur *Worktree
	bare := false
	flush := func() {
		if cur != nil && !bare && cur.Path != "" {
			cur.Name = filepath.Base(cur.Path)
			out = append(out, *cur)
		}
		cur, bare = nil, false
	}
	first := true
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		if key == "worktree" {
			flush()
			cur = &Worktree{Path: unquotePath(value), Main: first}
			first = false
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "HEAD":
			cur.Head = value
		case "branch":
			cur.Ref = value
		case "detached":
			cur.Detached = true
		case "bare":
			bare = true
		case "locked":
			cur.Locked, cur.LockReason = true, value
		case "prunable":
			cur.Prunable = true
		}
	}
	flush()
	return out
}

// unquotePath reads a path as git prints it: a path with a line break, a quote or a control character is in double quotes with C escapes.
func unquotePath(s string) string {
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// Worktrees lists the work trees of the repository, the main one first ("git worktree list --porcelain"). It lists the same worktrees
// whichever of them the repository is.
func (r *Repo) Worktrees(ctx context.Context) ([]Worktree, error) {
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"worktree", "list", "--porcelain"}})
	if err != nil {
		return nil, err
	}
	return ParseWorktrees(res.Stdout), nil
}

// WorktreeDetails is Worktrees with the subject of the commit each work tree has checked out.
//
// Ported from VS Code: extensions/git/src/repository.ts (getWorktreeDetails)
func (r *Repo) WorktreeDetails(ctx context.Context) ([]Worktree, error) {
	list, err := r.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Head == "" {
			continue
		}
		if c, err := r.CommitAt(ctx, list[i].Head); err == nil {
			subject, _, _ := strings.Cut(c.Message, "\n")
			list[i].Subject = subject
		}
	}
	return list, nil
}

// AddWorktreeOptions say what AddWorktree adds.
type AddWorktreeOptions struct {
	// Path is the folder of the new work tree; one that is not absolute is relative to the root of the repository.
	Path string
	// Commitish is the branch, tag or commit the work tree starts at; HEAD when empty.
	Commitish string
	// Branch, when not empty, is a new branch made at Commitish and checked out in the work tree ("-b").
	Branch string
	// NoTrack makes the new branch track nothing ("--no-track").
	NoTrack bool
}

// AddWorktree adds a linked work tree ("git worktree add [-b <branch>] [--no-track] -- <path> <commitish>"). The error has the tags
// WorktreeAlreadyExists (the path is taken), WorktreeBranchAlreadyUsed (the branch is checked out in another work tree),
// BranchAlreadyExists and InvalidBranchName.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.addWorktree)
func (r *Repo) AddWorktree(ctx context.Context, o AddWorktreeOptions) error {
	if o.Path == "" {
		return errors.New("GitWorktreeAddPathEmpty", "a worktree needs a folder", "root", r.Root)
	}
	commitish := o.Commitish
	if commitish == "" {
		commitish = "HEAD"
	}
	if err := refName(commitish); err != nil {
		return err
	}
	args := []string{"worktree", "add"}
	if o.Branch != "" {
		if err := refName(o.Branch); err != nil {
			return err
		}
		args = append(args, "-b", o.Branch)
	}
	if o.NoTrack {
		args = append(args, "--no-track")
	}
	path := o.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Root, path)
	}
	args = append(args, "--", filepath.Clean(path), commitish)
	_, err := r.write(ctx, ScopeAll, Command{Args: args})
	return err
}

// RemoveWorktree removes a linked work tree ("git worktree remove [--force] -- <path>"). One with changes or untracked files fails with
// the tag WorktreeContainsChanges unless force is set.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.deleteWorktree)
func (r *Repo) RemoveWorktree(ctx context.Context, path string, force bool) error {
	if path == "" {
		return errors.New("GitWorktreeRemovePathEmpty", "a worktree to remove needs a folder", "root", r.Root)
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", path)
	_, err := r.write(ctx, ScopeAll, Command{Args: args})
	return err
}

// FindWorktree returns the work tree of the list whose folder is path, comparing the real paths of the two.
func FindWorktree(list []Worktree, path string) (Worktree, bool) {
	want := realPath(path)
	for _, w := range list {
		if realPath(w.Path) == want {
			return w, true
		}
	}
	return Worktree{}, false
}

// realPath cleans a path and resolves its symbolic links when it can.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// LsIgnoredOptions say what LsIgnored lists.
type LsIgnoredOptions struct {
	// ExcludeStandard takes the ignore rules git reads on its own (.gitignore files, .git/info/exclude and core.excludesFile).
	ExcludeStandard bool
	// ExcludePatterns are .gitignore patterns used in their place or besides: git reads them as a file, so comments, blank lines and
	// trailing spaces are handled as in a .gitignore. None means the option is not given.
	ExcludePatterns []string
	// Directory shows a wholly ignored folder as one entry with a trailing slash, NoEmptyDirectory leaves out the folders with nothing in
	// them.
	Directory, NoEmptyDirectory bool
}

// LsIgnored lists the untracked files that are ignored, NUL separated ("git ls-files --others --ignored -z"). The patterns are written to
// a file in a private folder of the temporary files for the time of the command: git reads them from a file (a pipe is an empty file to
// it), so they are parsed as a .gitignore is. The error has the tag GitWorktreePatternsFailed when the file cannot be made.
//
// Ported from VS Code: extensions/git/src/repository.ts (_getWorktreeIncludePaths, _getWorktreeSymlinkFolders)
func (r *Repo) LsIgnored(ctx context.Context, o LsIgnoredOptions) (string, error) {
	args := []string{"ls-files", "--others", "--ignored", "-z"}
	if o.ExcludeStandard {
		args = append(args, "--exclude-standard")
	}
	if len(o.ExcludePatterns) > 0 {
		dir, err := os.MkdirTemp("", "emetos-patterns-")
		if err != nil {
			return "", errors.NewErr("GitWorktreePatternsFailed", err).SetMetadata("step", "mkdir")
		}
		defer func() { _ = os.RemoveAll(dir) }() // a temporary folder: nothing is lost when it stays
		file := filepath.Join(dir, "patterns")
		if err := os.WriteFile(file, []byte(strings.Join(o.ExcludePatterns, "\n")+"\n"), 0o600); err != nil {
			return "", errors.NewErr("GitWorktreePatternsFailed", err).SetMetadata("step", "write")
		}
		args = append(args, "--exclude-from="+file)
	}
	if o.Directory {
		args = append(args, "--directory")
	}
	if o.NoEmptyDirectory {
		args = append(args, "--no-empty-directory")
	}
	res, err := r.read(ctx, ScopeWorkTree, Command{Args: args})
	return string(res.Stdout), err
}

// LsFilesIn lists the tracked files of the work tree in dir, NUL separated ("git ls-files -z"): the files a new worktree has.
func (r *Repo) LsFilesIn(ctx context.Context, dir string) (string, error) {
	var res Result
	err := r.Queue.Do(ctx, Op{Scope: ScopeWorkTree}, func(ctx context.Context) error {
		var err error
		res, err = r.Runner.Run(ctx, Command{Dir: dir, Args: []string{"ls-files", "-z"}, Background: true})
		return err
	})
	return string(res.Stdout), err
}

// CheckIgnoreFolders says which of the folders (written with a trailing slash) the ignore rules of the repository match whatever the
// index says, NUL separated ("git check-ignore --no-index -z --stdin"). A folder inside an ignored folder is ignored.
func (r *Repo) CheckIgnoreFolders(ctx context.Context, folders []string) (string, error) {
	if len(folders) == 0 {
		return "", nil
	}
	res, err := r.read(ctx, ScopeWorkTree|ScopeConfig, Command{
		Args: []string{"check-ignore", "--no-index", "-z", "--stdin"}, Stdin: strings.NewReader(strings.Join(folders, "\x00")), AllowExit: []int{1},
	})
	return string(res.Stdout), err
}
