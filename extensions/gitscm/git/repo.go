package git

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// MaxBlobSize is the largest file content that ReadBlob returns: 16 MiB (SPEC §10.30 "Bounds"); above it the diff shows
// "The file is too large to compare".
const MaxBlobSize = 16 << 20

// Repo is one repository: the Runner that starts its commands, the Queue that orders them, and what rev-parse said about it.
// The commands of its methods are reads; the commands of a user's action (stage, commit, checkout...) are added by the steps
// that need them, as methods that go through the same Queue as writes. A read of Repo is a background command (SPEC §10.30
// "Running git"): it runs with core.fsmonitor=false, no pager, no external diff or textconv and no optional locks.
type Repo struct {
	// Runner starts the commands.
	Runner *Runner
	// Queue orders them.
	Queue *Queue
	// Root is the top level of the work tree (the git directory of a bare repository).
	Root string
	// Info is what git rev-parse said when the repository was opened.
	Info RepoInfo
}

// Open opens the repository whose work tree is rooted at root: it runs git rev-parse in it and returns the Repo. The error has
// the tag NotAtRepositoryRoot when root is a folder inside a repository but not its top level, and the tags of the commands
// (NotAGitRepository, UnsafeRepository...) when git refuses. q may be nil, which gives the repository a Queue of its own.
func Open(ctx context.Context, r *Runner, q *Queue, root string) (*Repo, error) {
	if q == nil {
		q = NewQueue(0)
	}
	info, err := revParse(ctx, r, q, root)
	if err != nil {
		return nil, err
	}
	top := info.WorkTree
	if top == "" {
		top = info.GitDir
	}
	if !samePath(top, root) {
		return nil, errors.New("NotAtRepositoryRoot", "the folder is not the top level of its repository", "root", root, "topLevel", top)
	}
	return &Repo{Runner: r, Queue: q, Root: root, Info: info}, nil
}

// Discover finds the repository that dir is in (the work tree's top level) and opens it. The error has the tags of Open
// except NotAtRepositoryRoot.
func Discover(ctx context.Context, r *Runner, q *Queue, dir string) (*Repo, error) {
	if q == nil {
		q = NewQueue(0)
	}
	info, err := revParse(ctx, r, q, dir)
	if err != nil {
		return nil, err
	}
	root := info.WorkTree
	if root == "" {
		root = info.GitDir
	}
	return &Repo{Runner: r, Queue: q, Root: root, Info: info}, nil
}

// samePath says whether two paths name the same folder: equal after cleaning, or after resolving symbolic links.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// revParse runs the two rev-parse commands in dir.
func revParse(ctx context.Context, r *Runner, q *Queue, dir string) (RepoInfo, error) {
	var gitDirOut, topOut string
	err := q.Do(ctx, Op{Scope: ScopeConfig}, func(ctx context.Context) error {
		res, err := r.Run(ctx, Command{Dir: dir, Args: RevParseGitDirArgs, Background: true})
		if err != nil {
			return err
		}
		gitDirOut = string(res.Stdout)
		return nil
	})
	if err != nil {
		return RepoInfo{}, err
	}
	if lines := strings.Split(gitDirOut, "\n"); len(lines) > 2 && lines[2] == "true" {
		// The folder is inside a work tree: the top level exists.
		err = q.Do(ctx, Op{Scope: ScopeConfig}, func(ctx context.Context) error {
			res, err := r.Run(ctx, Command{Dir: dir, Args: RevParseTopLevelArgs, Background: true})
			if err != nil {
				return err
			}
			topOut = string(res.Stdout)
			return nil
		})
		if err != nil {
			return RepoInfo{}, err
		}
	}
	return ParseRevParse(gitDirOut, topOut, dir)
}

// read runs a command of the repository as a read of the given scope.
func (r *Repo) read(ctx context.Context, scope Scope, c Command) (Result, error) {
	c.Dir = r.Root
	c.Background = true
	var res Result
	err := r.Queue.Do(ctx, Op{Scope: scope}, func(ctx context.Context) error {
		var err error
		res, err = r.Runner.Run(ctx, c)
		return err
	})
	return res, err
}

// stream is read for a command whose output is consumed as a stream.
func (r *Repo) stream(ctx context.Context, scope Scope, c Command, consume func(io.Reader) error) error {
	c.Dir = r.Root
	c.Background = true
	return r.Queue.Do(ctx, Op{Scope: scope}, func(ctx context.Context) error {
		_, err := r.Runner.RunStream(ctx, c, consume)
		return err
	})
}

// UntrackedMode is git.untrackedChanges: how untracked files are listed.
type UntrackedMode int

const (
	// UntrackedAll lists every untracked file ("-uall"): the modes "mixed" and "separate".
	UntrackedAll UntrackedMode = iota
	// UntrackedNone lists none ("-uno"): the mode "hidden".
	UntrackedNone
)

// StatusOptions are the options of Status.
type StatusOptions struct {
	// Untracked is how untracked files are listed.
	Untracked UntrackedMode
	// Ignored adds the ignored files ("--ignored").
	Ignored bool
	// IgnoreSubmodules adds --ignore-submodules (git.ignoreSubmodules).
	IgnoreSubmodules bool
	// SimilarityThreshold is git.similarityThreshold; it is passed as --find-renames=N% when it is not 0 or 50 (git's default).
	SimilarityThreshold int
	// Limit stops the command once more than Limit entries were read (git.statusLimit, 10000 by default in the extension);
	// zero means no limit.
	Limit int
}

// StatusResult is the answer of Status.
type StatusResult struct {
	// Entries are the records, at most Limit of them.
	Entries []StatusEntry
	// Total is the number of records read: more than Limit when the limit was hit (the command was stopped there).
	Total int
	// HitLimit says that the command was stopped because it printed more than Limit records.
	HitLimit bool
}

// Status runs "git status -z" and parses it as a stream. At the limit the command is ended (SIGTERM to its process group)
// and the answer says so.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.getStatus).
func (r *Repo) Status(ctx context.Context, o StatusOptions) (StatusResult, error) {
	args := []string{"status", "-z"}
	if o.Untracked == UntrackedNone {
		args = append(args, "-uno")
	} else {
		args = append(args, "-uall")
	}
	if o.Ignored {
		args = append(args, "--ignored")
	}
	if o.IgnoreSubmodules {
		args = append(args, "--ignore-submodules")
	}
	if o.SimilarityThreshold != 0 && o.SimilarityThreshold != 50 {
		args = append(args, "--find-renames="+strconv.Itoa(o.SimilarityThreshold)+"%")
	}
	var p StatusParser
	var res StatusResult
	err := r.stream(ctx, ScopeIndex|ScopeWorkTree, Command{Args: args}, func(stdout io.Reader) error {
		buf := make([]byte, 64<<10)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				_, _ = p.Write(buf[:n])
				if o.Limit > 0 && len(p.Entries()) > o.Limit {
					res.HitLimit = true
					return ErrStopStream
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
		}
	})
	if err != nil {
		return StatusResult{}, err
	}
	res.Total = len(p.Entries())
	res.Entries = p.Entries()
	if res.HitLimit {
		res.Entries = res.Entries[:o.Limit]
	}
	return res, nil
}

// LogOptions are the options of Log.
type LogOptions struct {
	// Range is a revision or a range ("main..feature"); empty means HEAD.
	Range string
	// MaxEntries is the most commits to list; zero means 32 (VS Code's default), a negative number means no limit.
	MaxEntries int
	// Skip skips that many commits first.
	Skip int
	// Path limits the log to the commits that changed the path.
	Path string
	// Author and Grep filter by author and by a case-insensitive extended regular expression in the message.
	Author, Grep string
	// ShortStats adds --shortstat (the first-parent diff for merges).
	ShortStats bool
	// Reverse lists the oldest first, along the ancestry path.
	Reverse bool
}

// Log runs "git log -z" and parses it as a stream. A repository with no commit gives no commits.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.log).
func (r *Repo) Log(ctx context.Context, o LogOptions) ([]Commit, error) {
	args := []string{"log", "--format=" + CommitFormat, "-z"}
	if o.ShortStats {
		args = append(args, "--shortstat")
		if r.Runner.Version().AtLeast(2, 31) {
			args = append(args, "--diff-merges=first-parent")
		}
	}
	if o.Reverse {
		args = append(args, "--reverse", "--ancestry-path")
	}
	if o.MaxEntries >= 0 {
		n := o.MaxEntries
		if n == 0 {
			n = 32
		}
		args = append(args, "-n"+strconv.Itoa(n))
	}
	if o.Skip > 0 {
		args = append(args, "--skip="+strconv.Itoa(o.Skip))
	}
	if o.Author != "" {
		args = append(args, "--author="+o.Author)
	}
	if o.Grep != "" {
		args = append(args, "--grep="+o.Grep, "--extended-regexp", "--regexp-ignore-case")
	}
	if o.Range != "" {
		args = append(args, o.Range)
	}
	c := Command{Args: args}
	if o.Path != "" {
		c.Paths = []string{o.Path}
	}
	var commits []Commit
	err := r.stream(ctx, ScopeRefs, c, func(stdout io.Reader) error {
		return StreamCommits(stdout, func(cm Commit) error { commits = append(commits, cm); return nil })
	})
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "GitCommandFailed" {
			if stderr, _ := appErr.Metadata["stderr"].(string); strings.Contains(stderr, "does not have any commits yet") {
				return nil, nil
			}
		}
		return nil, err
	}
	return commits, nil
}

// Refs runs "git for-each-ref" for the patterns ("refs/heads", "refs/remotes", "refs/tags"; none means all) and parses it.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.getRefs).
func (r *Repo) Refs(ctx context.Context, patterns ...string) ([]Ref, error) {
	res, err := r.read(ctx, ScopeRefs, Command{Args: append([]string{"for-each-ref", "--format=" + RefsFormat}, patterns...)})
	if err != nil {
		return nil, err
	}
	return ParseRefs(res.Stdout), nil
}

// Config lists the settings of one scope ("local", "global", "system" or "worktree"; empty means local) with
// "git config --list -z".
func (r *Repo) Config(ctx context.Context, scope string) ([]ConfigEntry, error) {
	if scope == "" {
		scope = "local"
	}
	res, err := r.read(ctx, ScopeConfig, Command{Args: []string{"config", "--" + scope, "--list", "-z"}})
	if err != nil {
		return nil, err
	}
	return ParseConfigList(res.Stdout), nil
}

// ConfigValue reads one setting as git sees it, from every scope ("git config --get <key>"): the value without its final newline, and
// false when no scope has the key.
func (r *Repo) ConfigValue(ctx context.Context, key string) (string, bool, error) {
	res, err := r.read(ctx, ScopeConfig, Command{Args: []string{"config", "--get", key}, AllowExit: []int{1}})
	if err != nil {
		return "", false, err
	}
	if res.ExitCode != 0 {
		return "", false, nil
	}
	return strings.TrimSuffix(string(res.Stdout), "\n"), true, nil
}

// Remotes lists the remotes with "git remote -v".
func (r *Repo) Remotes(ctx context.Context) ([]Remote, error) {
	res, err := r.read(ctx, ScopeConfig, Command{Args: []string{"remote", "-v"}})
	if err != nil {
		return nil, err
	}
	return ParseRemoteVerbose(string(res.Stdout)), nil
}

// Stashes lists the stash with "git stash list -z".
func (r *Repo) Stashes(ctx context.Context) ([]Stash, error) {
	res, err := r.read(ctx, ScopeStash, Command{Args: []string{"stash", "list", "--format=" + StashFormat, "-z"}})
	if err != nil {
		return nil, err
	}
	return ParseStashes(res.Stdout), nil
}

// BlameOptions are the options of Blame.
type BlameOptions struct {
	// Path is the file, relative to the root.
	Path string
	// Rev is the revision to blame; empty means the work tree's version.
	Rev string
	// IgnoreWhitespace adds -w.
	IgnoreWhitespace bool
}

// Blame runs "git blame --root --incremental" and parses it as a stream.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.blame2).
func (r *Repo) Blame(ctx context.Context, o BlameOptions) ([]BlameCommit, error) {
	args := []string{"blame", "--root", "--incremental"}
	if o.IgnoreWhitespace {
		args = append(args, "-w")
	}
	if o.Rev != "" {
		args = append(args, o.Rev)
	}
	var out []BlameCommit
	err := r.stream(ctx, ScopeRefs|ScopeWorkTree, Command{Args: args, Paths: []string{o.Path}}, func(stdout io.Reader) error {
		var err error
		out, err = StreamBlame(stdout)
		return err
	})
	return out, err
}

// DiffSpec describes a "git diff --name-status" listing.
type DiffSpec struct {
	// Cached compares the index with HEAD (or with Revs[0]).
	Cached bool
	// Revs are the revisions: none compares the work tree with the index, one the work tree (or the index) with it, two
	// compare them.
	Revs []string
	// Filter is the letters of --diff-filter ("ADMR"); empty means all.
	Filter string
	// SimilarityThreshold is passed as --find-renames=N% when it is not 0.
	SimilarityThreshold int
	// Paths limit the listing.
	Paths []string
}

// NameStatus runs "git diff --name-status -z" and parses it.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.diffFiles, diffBetween).
func (r *Repo) NameStatus(ctx context.Context, d DiffSpec) ([]NameStatus, error) {
	args := []string{"diff", "--name-status", "-z"}
	if d.Filter != "" {
		args = append(args, "--diff-filter="+d.Filter)
	}
	if d.Cached {
		args = append(args, "--cached")
	}
	if d.SimilarityThreshold != 0 {
		args = append(args, "--find-renames="+strconv.Itoa(d.SimilarityThreshold)+"%")
	}
	args = append(args, d.Revs...)
	c := Command{Args: args, Paths: d.Paths}
	if len(c.Paths) == 0 {
		c.Args = append(c.Args, "--")
	}
	res, err := r.read(ctx, ScopeIndex|ScopeWorkTree|ScopeRefs, c)
	if err != nil {
		return nil, err
	}
	return ParseNameStatus(res.Stdout), nil
}

// ReadBlob returns the content of an object named like "HEAD:path/file", ":path/file" (the index) or ":2:path/file" (a stage
// of a conflict), read with "git cat-file blob". A file larger than MaxBlobSize is not read: the error has the tag
// GitContentTooLarge.
func (r *Repo) ReadBlob(ctx context.Context, object string) ([]byte, error) {
	var out []byte
	err := r.stream(ctx, ScopeRefs|ScopeIndex, Command{Args: []string{"cat-file", "blob", object}}, func(stdout io.Reader) error {
		data, err := io.ReadAll(io.LimitReader(stdout, MaxBlobSize+1))
		if err != nil {
			return err
		}
		if len(data) > MaxBlobSize {
			return errors.New("GitContentTooLarge", "the file is too large to compare", "object", object, "limit", MaxBlobSize)
		}
		out = data
		return nil
	})
	return out, err
}

// ObjectDetails is the mode, object and size of a path at a revision.
type ObjectDetails struct {
	Mode   string
	Object string
	Size   int64
}

// ObjectDetails describes a path at a revision: in the index when treeish is empty or ":1", ":2" or ":3" (the stages of a
// conflict), otherwise in that tree. The error has the tag UnknownPath when git does not know the path.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.getObjectDetails).
func (r *Repo) ObjectDetails(ctx context.Context, treeish, path string) (ObjectDetails, error) {
	if treeish == "" || treeish == ":1" || treeish == ":2" || treeish == ":3" {
		entries, err := r.LsFiles(ctx, path)
		if err != nil {
			return ObjectDetails{}, err
		}
		if len(entries) == 0 {
			return ObjectDetails{}, errors.New("UnknownPath", "path not known by git", "path", path, "treeish", treeish)
		}
		e := entries[0]
		if treeish != "" {
			for _, c := range entries {
				if c.Stage == treeish[1:] {
					e = c
					break
				}
			}
		}
		res, err := r.read(ctx, ScopeIndex, Command{Args: []string{"cat-file", "-s", e.Object}})
		if err != nil {
			return ObjectDetails{}, err
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(string(res.Stdout)), 10, 64)
		return ObjectDetails{Mode: e.Mode, Object: e.Object, Size: size}, nil
	}
	entries, err := r.LsTree(ctx, treeish, path, false)
	if err != nil {
		return ObjectDetails{}, err
	}
	if len(entries) == 0 {
		return ObjectDetails{}, errors.New("UnknownPath", "path not known by git", "path", path, "treeish", treeish)
	}
	size, _ := strconv.ParseInt(entries[0].Size, 10, 64)
	return ObjectDetails{Mode: entries[0].Mode, Object: entries[0].Object, Size: size}, nil
}

// LsFiles lists the entries of the index for the paths (all when none) with "git ls-files --stage -z".
func (r *Repo) LsFiles(ctx context.Context, paths ...string) ([]LsFilesEntry, error) {
	var out []LsFilesEntry
	err := r.stream(ctx, ScopeIndex, Command{Args: []string{"ls-files", "--stage", "-z"}, Paths: paths}, func(stdout io.Reader) error {
		return StreamLsFiles(stdout, func(e LsFilesEntry) error { out = append(out, e); return nil })
	})
	return out, err
}

// LsTree lists a tree with "git ls-tree -l -z".
func (r *Repo) LsTree(ctx context.Context, treeish, path string, recursive bool) ([]LsTreeEntry, error) {
	args := []string{"ls-tree", "-l", "-z"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, treeish)
	c := Command{Args: args}
	if path != "" {
		c.Paths = []string{path}
	}
	res, err := r.read(ctx, ScopeRefs, c)
	if err != nil {
		return nil, err
	}
	return ParseLsTree(res.Stdout), nil
}

// CheckIgnore returns the paths among the given ones that git ignores, with "git check-ignore -v -z --stdin" (a path matched
// by a negated pattern is not ignored). Paths outside the work tree make git fail; the error has the tag IsInSubmodule for a
// path inside a submodule.
//
// Ported from VS Code: extensions/git/src/repository.ts (checkIgnore).
func (r *Repo) CheckIgnore(ctx context.Context, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	input := strings.Join(paths, "\x00")
	res, err := r.read(ctx, ScopeWorkTree|ScopeConfig, Command{
		Args: []string{"check-ignore", "-v", "-z", "--stdin"}, Stdin: strings.NewReader(input), AllowExit: []int{1},
	})
	if err != nil {
		return nil, err
	}
	return ParseIgnored(res.Stdout), nil
}

// MergeBase returns the best common ancestor of the revisions with "git merge-base"; ok is false when they have none.
func (r *Repo) MergeBase(ctx context.Context, revs ...string) (hash string, ok bool, err error) {
	res, err := r.read(ctx, ScopeRefs, Command{Args: append([]string{"merge-base"}, revs...), AllowExit: []int{1}})
	if err != nil {
		return "", false, err
	}
	out := strings.TrimSpace(string(res.Stdout))
	return out, res.ExitCode == 0 && out != "", nil
}

// RevListCount counts the commits of a revision or a range with "git rev-list --count".
func (r *Repo) RevListCount(ctx context.Context, spec string) (int, error) {
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"rev-list", "--count", spec}})
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if convErr != nil {
		return 0, errors.NewErr("GitOutputUnexpected", convErr).SetMetadata("output", string(bytes.TrimSpace(res.Stdout)))
	}
	return n, nil
}

// AheadBehind counts the commits of a that b lacks (ahead) and of b that a lacks (behind), with
// "git rev-list --left-right --count a...b".
func (r *Repo) AheadBehind(ctx context.Context, a, b string) (ahead, behind int, err error) {
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"rev-list", "--left-right", "--count", a + "..." + b}})
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(string(res.Stdout))
	if len(f) != 2 {
		return 0, 0, errors.New("GitOutputUnexpected", "git rev-list printed an unexpected output", "output", strings.TrimSpace(string(res.Stdout)))
	}
	return atoi(f[0]), atoi(f[1]), nil
}

// InitOptions are the options of Runner.Init.
type InitOptions struct {
	// DefaultBranch is the name of the first branch ("-b"); empty leaves git's default.
	DefaultBranch string
	// Bare makes a repository with no work tree.
	Bare bool
}

// Init makes a repository in dir with "git init". It is a write command and not a background one: the user asked for it.
//
// Ported from VS Code: extensions/git/src/git.ts (Git.init).
func (r *Runner) Init(ctx context.Context, dir string, o InitOptions) error {
	args := []string{"init", "-q"}
	if o.DefaultBranch != "" {
		args = append(args, "-b", o.DefaultBranch)
	}
	if o.Bare {
		args = append(args, "--bare")
	}
	args = append(args, "--", dir)
	_, err := r.Run(ctx, Command{Args: args})
	return err
}
