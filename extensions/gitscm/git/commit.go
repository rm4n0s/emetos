package git

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rm4n0s/errors"
)

// Commit and the commands around it (SPEC §10.30 "Staging, discarding and committing"): the commit itself, the soft reset that undoes
// one, the message templates git keeps, and the commands the protected branch and the rebase need. They go through the Queue as writes
// and are not background commands: the user asked for them, and git may run hooks, ask for the passphrase of a signing key and start
// the editor through the programs of the Runner's Config.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.commit, rebaseContinue, branch, deleteRef, reset, getCommitTemplate,
// getMergeMessage, getSquashMessage, stripCommitMessageComments).

// CommitOptions are the options of Commit.
type CommitOptions struct {
	// Message is the commit message, given to git on its standard input ("--file -"). Empty means no message: with UseEditor git asks
	// the editor, an Amend keeps the message it has ("--no-edit"), and otherwise the message is empty ("--allow-empty-message").
	Message string
	// UseEditor lets git start its editor when there is no Message (git.useEditorAsCommitInput); without it git never does.
	UseEditor bool
	// Verbose adds the diff to the message the editor shows ("--verbose"; git.verboseCommit, which VS Code reads only with the editor).
	Verbose bool
	// Amend replaces the last commit ("--amend").
	Amend bool
	// Signoff adds a Signed-off-by trailer ("--signoff").
	Signoff bool
	// Sign is -S when it points to true and --no-gpg-sign when it points to false; nil leaves the decision to the configuration
	// (commit.gpgsign). The passphrase is asked by the program git runs for the key, never by Emetos.
	Sign *bool
	// Empty allows a commit with no changes ("--allow-empty").
	Empty bool
	// NoVerify skips the pre-commit and commit-msg hooks ("--no-verify").
	NoVerify bool
	// RequireUserConfig makes git fail when user.name or user.email are not configured instead of guessing them from the system
	// ("-c user.useConfigOnly=true"; git.requireGitUserConfig).
	RequireUserConfig bool
}

// Commit makes a commit of the index ("git commit --quiet ..."). The error has the tags UnmergedChanges (the index has unmerged paths),
// EmptyCommitMessage (the editor left an empty message), NoUserNameConfigured and NoUserEmailConfigured (with RequireUserConfig and no
// identity), and GitCommandFailed with the output of the hook that stopped the commit in its message and its stderr metadata.
func (r *Repo) Commit(ctx context.Context, o CommitOptions) error {
	args := []string{"commit", "--quiet"}
	c := Command{}
	if o.Message != "" {
		c.Stdin = strings.NewReader(o.Message)
		args = append(args, "--allow-empty-message", "--file", "-")
	}
	if o.Verbose {
		args = append(args, "--verbose")
	}
	if o.Amend {
		args = append(args, "--amend")
	}
	if !o.UseEditor {
		if o.Message == "" {
			if o.Amend {
				args = append(args, "--no-edit")
			} else {
				c.Stdin = strings.NewReader("")
				args = append(args, "--file", "-")
			}
		}
		args = append(args, "--allow-empty-message")
	}
	if o.Signoff {
		args = append(args, "--signoff")
	}
	if o.Sign != nil {
		if *o.Sign {
			args = append(args, "-S")
		} else {
			args = append(args, "--no-gpg-sign")
		}
	}
	if o.Empty {
		args = append(args, "--allow-empty")
	}
	if o.NoVerify {
		args = append(args, "--no-verify")
	}
	if o.RequireUserConfig {
		// stops git from guessing the user and the e-mail
		args = append([]string{"-c", "user.useConfigOnly=true"}, args...)
	}
	c.Args = args
	_, err := r.write(ctx, ScopeIndex|ScopeWorkTree|ScopeRefs, c)
	return err
}

// ResetSoft moves HEAD, and the branch it names, to the commit, and leaves the index and the work tree as they are ("git reset --soft
// <commit>"): the changes of the commits it leaves behind stay staged.
func (r *Repo) ResetSoft(ctx context.Context, commit string) error {
	_, err := r.write(ctx, ScopeRefs, Command{Args: []string{"reset", "--soft", commit}})
	return err
}

// DeleteRef deletes a reference ("git update-ref -d <ref>"): "HEAD" on a branch with one commit leaves the branch with none.
func (r *Repo) DeleteRef(ctx context.Context, ref string) error {
	_, err := r.write(ctx, ScopeRefs, Command{Args: []string{"update-ref", "-d", ref}})
	return err
}

// CreateBranch makes a branch at HEAD and switches to it, keeping the changes of the work tree and the index ("git checkout -q -b
// <name> --no-track"). The error has the tags BranchAlreadyExists and InvalidBranchName.
func (r *Repo) CreateBranch(ctx context.Context, name string) error {
	_, err := r.write(ctx, ScopeRefs|ScopeIndex|ScopeWorkTree, Command{Args: []string{"checkout", "-q", "-b", name, "--no-track"}})
	return err
}

// reCommentLine is a line of a message template that git drops: the ones that start with # after white space.
var reCommentLine = regexp.MustCompile(`(?m)^\s*#.*$\n?`)

// StripCommitMessageComments removes the comment lines of a message and the white space around it. (core.commentChar is not read, as
// VS Code does not.)
func StripCommitMessageComments(message string) string {
	return strings.TrimSpace(reCommentLine.ReplaceAllString(message, ""))
}

// readMessageFile reads a message file of the git directory (MERGE_MSG, SQUASH_MSG) without its comment lines; "" when the file is
// missing or unreadable.
func (r *Repo) readMessageFile(name string) string {
	dir := r.Info.GitDir
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return StripCommitMessageComments(string(data))
}

// MergeMessage is the message git prepared for the merge that is in progress, from MERGE_MSG; "" when there is none.
func (r *Repo) MergeMessage() string { return r.readMessageFile("MERGE_MSG") }

// SquashMessage is the message git prepared for a squash merge, from SQUASH_MSG; "" when there is none.
func (r *Repo) SquashMessage() string { return r.readMessageFile("SQUASH_MSG") }

// CommitTemplate is the text of the file commit.template names, without its comment lines; "" when the setting is not set or the file
// cannot be read (VS Code ignores both). "~/" and "~user/" at the start of the path are expanded as git does, with home as the home
// folder; a relative path is relative to the root of the repository. It reads the setting with "git config --get", so the
// configuration of every scope counts.
func (r *Repo) CommitTemplate(ctx context.Context, home string) string {
	path, ok, err := r.ConfigValue(ctx, "commit.template")
	if err != nil || !ok || strings.TrimSpace(path) == "" {
		return ""
	}
	path = expandTilde(strings.TrimSpace(path), home)
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return StripCommitMessageComments(string(data))
}

// InputTemplate is the text the commit box starts with: the message git prepared for a merge or a squash, else the commit template.
//
// Ported from VS Code: extensions/git/src/repository.ts (getInputTemplate).
func (r *Repo) InputTemplate(ctx context.Context, home string) string {
	if msg := r.MergeMessage(); msg != "" {
		return msg
	}
	if msg := r.SquashMessage(); msg != "" {
		return msg
	}
	return r.CommitTemplate(ctx, home)
}

// expandTilde expands "~/" and "~user/" at the start of a path, as git does (path.c, expand_user_path): the home folder of another
// user is taken to be next to the home folder of this one.
func expandTilde(path, home string) string {
	if !strings.HasPrefix(path, "~") || home == "" {
		return path
	}
	rest := path[1:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return path
	}
	user, tail := rest[:slash], rest[slash+1:]
	if user == "" {
		return filepath.Join(home, tail)
	}
	return filepath.Join(filepath.Dir(home), user, tail)
}

// LastCommit is the commit HEAD names, with its message and parents. The error has the tag GitNoCommit in a repository with no commit.
func (r *Repo) LastCommit(ctx context.Context) (Commit, error) {
	commits, err := r.Log(ctx, LogOptions{MaxEntries: 1})
	if err != nil {
		return Commit{}, err
	}
	if len(commits) == 0 {
		return Commit{}, errors.New("GitNoCommit", "HEAD does not point to any commit", "root", r.Root)
	}
	return commits[0], nil
}
