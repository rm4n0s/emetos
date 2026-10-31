package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// LogFileOptions are the options of LogFile.
type LogFileOptions struct {
	// Path is the file: absolute, or relative to the root of the repository.
	Path string
	// Hash starts the log at this commit instead of at HEAD (a cursor of the Timeline view); empty for HEAD. It must be a full object
	// name.
	Hash string
	// MaxEntries is the most commits to list; 0 lists them all.
	MaxEntries int
	// Follow follows the file across renames.
	Follow bool
	// ShortStats adds the shortstat of each commit (the first-parent diff for merges).
	ShortStats bool
}

// LogFile lists the commits that changed a file, newest first, each with the path the file had in it (Commit.FilePath, which differs from
// Path before a rename when Follow is set). A file that git does not know, and a repository with no commit, give no commits. The log is run
// in the background (SPEC §10.30 "Running git").
//
// A merge commit that changed the file is listed with its path and its stat against its first parent (--diff-merges=first-parent, git
// 2.31), so that the Timeline can name the file of a merge.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.logFile)
func (r *Repo) LogFile(ctx context.Context, o LogFileOptions) ([]Commit, error) {
	if o.Hash != "" && !hashPattern.MatchString(o.Hash) {
		return nil, errors.New("GitLogFileRevisionInvalid", "the commit a file log starts at is a full object name", "revision", o.Hash)
	}
	paths, err := r.relPaths([]string{o.Path}, false)
	if err != nil {
		return nil, err
	}
	args := []string{"log", "--format=" + CommitFormat, "-z"}
	if o.MaxEntries > 0 {
		args = append(args, "-n"+strconv.Itoa(o.MaxEntries))
	}
	if o.Hash != "" {
		args = append(args, o.Hash)
	}
	if o.ShortStats {
		args = append(args, "--shortstat")
	}
	args = append(args, "--numstat")
	if r.Runner.Version().AtLeast(2, 31) {
		args = append(args, "--diff-merges=first-parent")
	}
	if o.Follow {
		args = append(args, "--follow")
	}
	res, err := r.read(ctx, ScopeRefs, Command{Args: args, Paths: paths, Env: []string{literalPathspecs}})
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "GitCommandFailed" {
			if stderr, _ := appErr.Metadata["stderr"].(string); strings.Contains(stderr, "does not have any commits yet") {
				return nil, nil
			}
		}
		return nil, err
	}
	return ParseFileLog(res.Stdout), nil
}
