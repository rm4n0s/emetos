package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// A provoked failure: a real git in a repository the case prepares, run through the Runner, or a fixed text where git cannot be
// made to print the message here.
type provocation struct {
	tag string
	// real prepares the world and returns the command to run; the failure is git's own message.
	real func(t *testing.T, e *gittest.Env) Command
	// env changes the environment of the world before the command is prepared (a missing identity).
	env func(e *gittest.Env)
	// runnerEnv are variables only the command under test gets, not the preparation of the world (an owner that differs).
	runnerEnv []string
	// fixed is the command name, standard output and error output of a message copied from git or from VS Code's tests.
	fixedName, fixedStdout, fixedStderr string
	fixedArgs                           []string
}

func fixed(tag, name, stdout, stderr string, args ...string) provocation {
	return provocation{tag: tag, fixedName: name, fixedStdout: stdout, fixedStderr: stderr, fixedArgs: args}
}

// base makes a repository with a committed file a.txt and src/loader.go.
func base(e *gittest.Env) *gittest.Repo {
	r := e.Init("repo")
	r.Write("a.txt", "one\n")
	r.Write("src/loader.go", gittest.LoaderSource(0))
	r.Commit("base")
	return r
}

func in(r *gittest.Repo, args ...string) Command { return Command{Dir: r.Dir, Args: args} }

var provocations = []provocation{
	{tag: "RepositoryIsLocked", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Write(".git/index.lock", "")
		r.Write("b.txt", "b\n")
		return in(r, "add", "b.txt")
	}},
	{tag: "NotAGitRepository", real: func(t *testing.T, e *gittest.Env) Command {
		dir := filepath.Join(e.Work, "plain")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return Command{Dir: dir, Args: []string{"status"}}
	}},
	{tag: "CantAccessRemote", real: func(t *testing.T, e *gittest.Env) Command {
		return Command{Dir: e.Work, Args: []string{"ls-remote", "http://127.0.0.1:1/none.git"}} // nothing listens on port 1
	}},
	{tag: "BranchNotFullyMerged", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Branch("topic", "")
		r.Checkout("topic")
		r.Write("t.txt", "t\n")
		r.Commit("topic")
		r.Checkout("main")
		return in(r, "branch", "-d", "topic")
	}},
	{tag: "NoRemoteReference", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.AddRemote("origin")
		r.Git("push", "-q", "origin", "main")
		return in(r, "fetch", "origin", "nonexistent")
	}},
	{tag: "BranchAlreadyExists", real: func(t *testing.T, e *gittest.Env) Command { return in(base(e), "branch", "main") }},
	{tag: "InvalidBranchName", real: func(t *testing.T, e *gittest.Env) Command { return in(base(e), "branch", "a b") }},
	{tag: "DirtyWorkTree", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Branch("other", "")
		r.Checkout("other")
		r.Write("a.txt", "other\n")
		r.Commit("other")
		r.Checkout("main")
		r.Write("a.txt", "local change\n")
		return in(r, "checkout", "other")
	}},
	{tag: "UnsafeRepository", runnerEnv: []string{"GIT_TEST_ASSUME_DIFFERENT_OWNER=1"},
		real: func(t *testing.T, e *gittest.Env) Command { return in(base(e), "status") }},
	// git names the email first when both are missing ("no email was given"); a missing name alone is "no name was given".
	{tag: "NoUserNameConfigured", runnerEnv: []string{"GIT_AUTHOR_EMAIL=ada@example.com", "GIT_COMMITTER_EMAIL=ada@example.com"},
		real: func(t *testing.T, e *gittest.Env) Command {
			r := base(e)
			e.Identity = false // from here on git is told not to guess an identity
			r.Write("b.txt", "b\n")
			r.Git("add", "b.txt")
			return in(r, "commit", "-m", "no name")
		}},
	{tag: "NoUserEmailConfigured", runnerEnv: []string{"GIT_AUTHOR_NAME=Ada Example", "GIT_COMMITTER_NAME=Ada Example"},
		real: func(t *testing.T, e *gittest.Env) Command {
			r := base(e)
			e.Identity = false
			r.Write("b.txt", "b\n")
			r.Git("add", "b.txt")
			return in(r, "commit", "-m", "no email")
		}},
	{tag: "UnmergedChanges", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		ours, theirs := r.ConflictingBranches()
		_ = ours
		_ = r.Merge(theirs)
		return in(r, "commit", "-m", "while unmerged")
	}},
	{tag: "EmptyCommitMessage", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Write("b.txt", "b\n")
		r.Git("add", "b.txt")
		return in(r, "commit", "-m", "")
	}},
	{tag: "Conflict", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		_, theirs := r.ConflictingBranches()
		return in(r, "merge", theirs)
	}},
	{tag: "Conflict", real: func(t *testing.T, e *gittest.Env) Command { // pull
		r := base(e)
		_, theirs := r.ConflictingBranches()
		return in(r, "pull", "--no-rebase", "--no-edit", ".", theirs)
	}},
	{tag: "Conflict", real: func(t *testing.T, e *gittest.Env) Command { // rebase
		r := base(e)
		_, theirs := r.ConflictingBranches()
		return in(r, "rebase", theirs)
	}},
	{tag: "CherryPickConflict", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		_, theirs := r.ConflictingBranches()
		return in(r, "cherry-pick", theirs)
	}},
	{tag: "StashConflict", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Write("a.txt", "stashed\n")
		r.Stash("s", false)
		r.Write("a.txt", "committed\n")
		r.Commit("conflicting")
		return in(r, "stash", "pop")
	}},
	{tag: "LocalChangesOverwritten", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Write("a.txt", "stashed\n")
		r.Stash("s", false)
		r.Write("a.txt", "dirty again\n")
		return in(r, "stash", "apply")
	}},
	{tag: "NoStashFound", real: func(t *testing.T, e *gittest.Env) Command { return in(base(e), "stash", "pop") }},
	{tag: "PushRejected", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		bare := r.AddRemote("origin")
		r.Git("push", "-q", "-u", "origin", "main")
		other := bare.Clone("other")
		other.Write("x.txt", "x\n")
		other.Commit("ahead")
		other.Git("push", "-q", "origin", "main")
		r.Write("y.txt", "y\n")
		r.Commit("diverged")
		return in(r, "push")
	}},
	{tag: "RemoteRejected", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		bare := r.AddRemote("origin")
		bare.Hook("pre-receive", "#!/bin/sh\necho 'protected branch: main cannot be updated' >&2\nexit 1\n")
		r.Write("y.txt", "y\n")
		r.Commit("more")
		return in(r, "push", "origin", "main")
	}},
	{tag: "NoUpstreamBranch", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.AddRemote("origin")
		r.Branch("topic", "")
		r.Checkout("topic")
		return in(r, "push")
	}},
	{tag: "RemoteConnectionError", real: func(t *testing.T, e *gittest.Env) Command { // fetch
		r := base(e)
		return in(r, "fetch", filepath.Join(e.Root, "no-such-repository"))
	}},
	{tag: "RepositoryNotFound", real: func(t *testing.T, e *gittest.Env) Command { // clone of a path that is not there
		return Command{Dir: e.Work, Args: []string{"clone", filepath.Join(e.Root, "no-such-repository"), filepath.Join(e.Work, "c")}}
	}},
	{tag: "TagConflict", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		bare := r.AddRemote("origin")
		r.Tag("v1", "")
		r.Git("push", "-q", "-u", "origin", "main", "--tags")
		other := bare.Clone("other")
		other.Write("x.txt", "x\n")
		other.Commit("move the tag")
		other.Git("tag", "-f", "v1")
		other.Git("push", "-q", "origin", "main", "-f", "--tags")
		return in(r, "pull", "--tags", "--no-rebase", "origin", "main")
	}},
	{tag: "PatchDoesNotApply", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		patch := filepath.Join(e.Root, "bad.patch")
		if err := os.WriteFile(patch, []byte("--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-not in the file\n+replacement\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return in(r, "apply", patch)
	}},
	{tag: "NoPathFound", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		return Command{Dir: r.Dir, Args: []string{"blame", "--incremental"}, Paths: []string{"nonexistent.txt"}}
	}},
	{tag: "WrongCase", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Write("untracked.txt", "u\n")
		return in(r, "show", "HEAD:untracked.txt")
	}},
	{tag: "CantOpenResource", real: func(t *testing.T, e *gittest.Env) Command { return in(base(e), "show", "HEAD:no-such-file.txt") }},
	{tag: "CantOpenResource", real: func(t *testing.T, e *gittest.Env) Command {
		return in(base(e), "cat-file", "blob", "HEAD:no-such-file.txt")
	}},
	{tag: "CantOpenResource", real: func(t *testing.T, e *gittest.Env) Command {
		return in(base(e), "cat-file", "blob", ":no-such-file.txt") // a path that is not in the index
	}},
	{tag: "CantOpenResource", real: func(t *testing.T, e *gittest.Env) Command {
		return in(base(e), "cat-file", "blob", ":3:a.txt") // a stage the file does not have
	}},
	{tag: "IsInSubmodule", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		sub := e.Init("lib")
		sub.Write("lib.go", "package lib\n")
		sub.Commit("lib")
		r.Submodule("vendor/lib", sub)
		return Command{Dir: r.Dir, Args: []string{"check-ignore", "-v", "-z", "--stdin"}, Stdin: strings.NewReader("vendor/lib/lib.go\x00"), AllowExit: nil}
	}},
	{tag: "AuthenticationFailed", real: func(t *testing.T, e *gittest.Env) Command {
		src := base(e)
		src.AddRemote("origin")
		src.Git("push", "-q", "origin", "main")
		srv := e.HTTPBackend(e.Work, "ada", "right")
		return Command{Dir: e.Work, Args: []string{"ls-remote", strings.Replace(srv.URL, "http://", "http://ada:wrong@", 1) + "/origin.git"}}
	}},
	{tag: "RepositoryNotFound", real: func(t *testing.T, e *gittest.Env) Command {
		srv := e.HTTPBackend(e.Work, "", "")
		return Command{Dir: e.Work, Args: []string{"ls-remote", srv.URL + "/nonexistent.git"}}
	}},

	// Messages that cannot be provoked here: fixed text from git and from VS Code's catch blocks.
	fixed("NoRemoteRepositorySpecified", "fetch", "", "fatal: No remote repository specified.  Please, specify either a URL or a remote name from which new revisions should be fetched.\n", "fetch"),
	fixed("PermissionDenied", "push", "", "git@example.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n", "push"),
	fixed("PermissionDenied", "push", "", "remote: Permission to a/b.git denied to user.\nfatal: unable to access 'https://example.com/a/b.git/': The requested URL returned error: 403\n", "push"),
	fixed("CantLockRef", "pull", "", "error: cannot lock ref 'refs/remotes/origin/main': is at aaa but expected bbb\n", "pull"),
	fixed("CantLockRef", "pull", "", "error: some local refs could not be updated; try running\nunable to update local ref\n", "pull"),
	fixed("CantRebaseMultipleBranches", "pull", "", "Cannot rebase onto multiple branches.\n", "pull", "-r"),
	fixed("CantRebaseMultipleBranches", "rebase", "", "fatal: Cannot rebase onto multiple branches.\n", "rebase", "x"),
	fixed("CantCreatePipe", "commit", "", "git-sh-setup: cannot make pipe for command substitution: Too many open files\n", "commit"),
	fixed("BranchNotYetBorn", "checkout", "", "fatal: You are on a branch yet to be born\n", "checkout", "-q", "x"),
	fixed("DirtyWorkTree", "pull", "", "error: Your local changes to the following files would be overwritten by merge:\n\ta.txt\nPlease commit your changes or stash them before you merge.\n", "pull"),
	fixed("DirtyWorkTree", "pull", "", "Pulling is not possible because you have unmerged files.\n", "pull"),
	fixed("DirtyWorkTree", "pull", "", "fatal: Cannot pull with rebase: You have unstaged changes.\n", "pull", "-r"),
	fixed("NoUserNameConfigured", "pull", "", "Committer identity unknown\n\n*** Please tell me who you are.\n\nRun\n\n  git config --global user.email \"you@example.com\"\n", "pull"),
	fixed("NoUserNameConfigured", "commit", "", "Author identity unknown\n\n*** Please tell me who you are.\n", "commit"),
	fixed("NoUserEmailConfigured", "commit", "", "fatal: unable to auto-detect email address (got 'ada@host.(none)')\n", "commit"),
	fixed("RepositoryNotFound", "clone", "", "remote: Repository not found.\nfatal: repository 'https://example.com/a/b.git/' not found\n", "clone"),
	fixed("NoStashFound", "stash", "", "No stash found.\n", "stash", "pop"),
	fixed("NoStashFound", "stash", "", "error: stash@{4} is not a valid reference\n", "stash", "drop", "stash@{4}"),
	fixed("StashConflict", "stash", "Auto-merging a.txt\nCONFLICT (content): Merge conflict in a.txt\n", "", "stash", "apply"),
	fixed("Conflict", "merge", "Auto-merging a.txt\nCONFLICT (content): Merge conflict in a.txt\nAutomatic merge failed; fix conflicts and then commit the result.\n", "", "merge", "x"),
	fixed("UnmergedChanges", "rebase", "", "error: Committing is not possible because you have unmerged files.\n", "rebase", "--continue"),
	fixed("UnsafeRepository", "status", "", "fatal: detected dubious ownership in repository at '/srv/repo'\nTo add an exception for this directory, call:\n", "status"),
	fixed("AuthenticationFailed", "fetch", "", "remote: Invalid username or password.\nfatal: Authentication failed for 'https://example.com/a/b.git/'\n", "fetch"),
	fixed("InvalidBranchName", "branch", "", "fatal: 'a..b' is not a valid branch name\n", "branch", "a..b"),
	fixed("BranchAlreadyExists", "checkout", "", "fatal: A branch named 'x' already exists.\n", "checkout", "-b", "x"),
	fixed("NoRemoteReference", "pull", "", "fatal: Couldn't find remote ref refs/heads/x\n", "pull", "origin", "x"),
	fixed("CherryPickEmpty", "cherry-pick", "", "The previous cherry-pick is now empty, possibly due to conflict resolution.\nIf you wish to commit it anyway, use:\n", "cherry-pick", "x"),
	fixed("CherryPickConflict", "cherry-pick", "Auto-merging a.txt\nCONFLICT (content): Merge conflict in a.txt\n", "error: could not apply 5b1618f... topic\n", "cherry-pick", "x"),
	fixed("UnmergedChanges", "rebase", "a.txt: needs merge\nYou must edit all merge conflicts and then\nmark them as resolved using git add\n", "", "rebase", "--continue"),
	fixed("DirtyWorkTree", "rebase", "", "error: cannot rebase: You have unstaged changes.\nerror: Please commit or stash them.\n", "rebase", "x"),
	fixed("ForcePushWithLeaseRejected", "push", "", "To /srv/x.git\n ! [rejected]        main -> main (stale info)\nerror: failed to push some refs to '/srv/x.git'\n", "push", "--force-with-lease", "--", "origin", "main:main"),
	fixed("ForcePushWithLeaseRejected", "push", "", " ! [rejected]        main -> main (stale info)\nerror: failed to push some refs to '/srv/x.git'\n", "push", "--force-with-lease=refs/heads/main:abc", "origin"),
	fixed("ForcePushWithLeaseIfIncludesRejected", "push", "", " ! [rejected]        main -> main (remote ref updated since checkout)\nerror: failed to push some refs to '/srv/x.git'\n", "push", "--force-with-lease", "--force-if-includes", "--", "origin", "main:main"),
	fixed("PushRejected", "push", "", " ! [rejected]        main -> main (stale info)\nerror: failed to push some refs to '/srv/x.git'\n", "push", "--", "origin", "main:main"), // no lease asked: a plain rejection
	fixed("PushRejected", "push", "", " ! [rejected]        main -> main (fetch first)\n ! [remote rejected] topic -> topic (pre-receive hook declined)\nerror: failed to push some refs to '/srv/x.git'\n", "push", "--all"),
	fixed("RemoteRejected", "push", "", "remote: error: GH006: Protected branch update failed for refs/heads/main.\nTo https://example.com/a/b.git\n ! [remote rejected] main -> main (protected branch hook declined)\nerror: failed to push some refs to 'https://example.com/a/b.git'\n", "push"),
	fixed("BranchFastForwardRejected", "fetch", "", "From /srv/x\n ! [rejected]        main       -> topic      (non-fast-forward)\n", "fetch", "--", "origin", "main:topic"),
	{tag: "WorktreeContainsChanges", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		wt := filepath.Join(e.Work, "wt")
		r.Git("worktree", "add", "-q", "-b", "topic", wt)
		r.Write("../wt/untracked.txt", "x\n")
		return in(r, "worktree", "remove", wt)
	}},
	{tag: "WorktreeAlreadyExists", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		taken := filepath.Join(e.Work, "taken")
		r.Write("../taken/file.txt", "x\n")
		return in(r, "worktree", "add", taken, "-b", "topic")
	}},
	{tag: "WorktreeBranchAlreadyUsed", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Git("worktree", "add", "-q", "-b", "topic", filepath.Join(e.Work, "wt"))
		return in(r, "checkout", "topic")
	}},
	{tag: "WorktreeBranchAlreadyUsed", real: func(t *testing.T, e *gittest.Env) Command {
		r := base(e)
		r.Git("worktree", "add", "-q", "-b", "topic", filepath.Join(e.Work, "wt"))
		return in(r, "worktree", "add", filepath.Join(e.Work, "other"), "topic")
	}},
	fixed("WorktreeBranchAlreadyUsed", "checkout", "", "fatal: 'topic' is already checked out at '/srv/app.worktrees/topic'\n", "checkout", "topic"), // git before 2.42
	fixed("NoLocalChanges", "stash", "", "No local changes to save\n", "stash", "push"),                                                              // not an exit status: see TestStashPushWithNothingToStash
}

func TestEveryFailureIsClassifiedUnderItsOwnTag(t *testing.T) {
	seen := map[string]bool{}
	for i, p := range provocations {
		kind := "real"
		if p.real == nil {
			kind = "fixed"
		}
		t.Run(p.tag+"/"+kind+"/"+itoa(i), func(t *testing.T) {
			seen[p.tag] = true
			if p.real == nil {
				if p.tag == "NoLocalChanges" {
					return // exit status 0: TestStashPushWithNothingToStash
				}
				err := classify(p.fixedName, p.fixedArgs, 1, p.fixedStdout, p.fixedStderr)
				appErr := routeOf(t, err, "classify."+p.tag)
				if appErr != nil && appErr.Tag != p.tag {
					t.Errorf("tag = %s, want %s\nstderr: %s", appErr.Tag, p.tag, p.fixedStderr)
				}
				return
			}
			e := gittest.NewEnv(t)
			if p.env != nil {
				p.env(e)
			}
			c := p.real(t, e)
			runner := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: withVars(e.Vars(), p.runnerEnv...)})
			_, err := runner.Run(context.Background(), c)
			appErr := routeOf(t, err, "Runner.Run->Runner.execute->classify."+p.tag)
			if appErr == nil {
				return
			}
			if appErr.Tag != p.tag {
				t.Fatalf("tag = %s, want %s\nmessage: %s\nstderr: %v", appErr.Tag, p.tag, appErr.Message, appErr.Metadata["stderr"])
			}
			if _, ok := appErr.Metadata["exitCode"]; !ok {
				t.Errorf("no exit status in the metadata: %v", appErr.Metadata)
			}
			if s, _ := appErr.Metadata["stderr"].(string); len(s) > StderrKept {
				t.Errorf("stderr of %d bytes in the metadata", len(s))
			}
			args, _ := appErr.Metadata["args"].([]string)
			if len(args) != len(c.Args) {
				t.Errorf("args = %q", args)
			}
		})
	}
	// Every tag of the SPEC's list is covered by a case of this table or by the routes noted.
	for _, tag := range []string{
		"AuthenticationFailed", "NoUserNameConfigured", "NoUserEmailConfigured", "NoRemoteRepositorySpecified", "NotAGitRepository", "Conflict",
		"StashConflict", "UnmergedChanges", "PushRejected", "RemoteConnectionError", "DirtyWorkTree", "CantOpenResource", "CantCreatePipe",
		"PermissionDenied", "CantAccessRemote", "RepositoryNotFound", "RepositoryIsLocked", "BranchNotFullyMerged", "NoRemoteReference",
		"InvalidBranchName", "BranchAlreadyExists", "NoLocalChanges", "NoStashFound", "LocalChangesOverwritten", "NoUpstreamBranch", "IsInSubmodule",
		"WrongCase", "CantLockRef", "CantRebaseMultipleBranches", "PatchDoesNotApply", "NoPathFound", "EmptyCommitMessage", "BranchNotYetBorn",
		"TagConflict", "UnsafeRepository", "CherryPickConflict", "CherryPickEmpty", "ForcePushWithLeaseRejected", "ForcePushWithLeaseIfIncludesRejected",
		"RemoteRejected", "BranchFastForwardRejected", "WorktreeContainsChanges", "WorktreeAlreadyExists", "WorktreeBranchAlreadyUsed",
	} {
		if !seen[tag] {
			t.Errorf("the tag %s has no case", tag)
		}
	}
}

func TestAMessageThatIsNotKnownIsGitCommandFailed(t *testing.T) {
	for _, tc := range []struct {
		name, stdout, stderr string
		exit                 int
	}{
		{"nothing printed", "", "", 1},
		{"an unknown message", "", "fatal: something nobody has seen\n", 128},
		{"a signal", "", "", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := classify("fetch", []string{"fetch"}, tc.exit, tc.stdout, tc.stderr)
			appErr := routeOf(t, err, "classify.GitCommandFailed")
			if appErr != nil && (appErr.Metadata["exitCode"] != tc.exit || !strings.HasPrefix(appErr.Message, "git fetch failed")) {
				t.Errorf("metadata %v message %q", appErr.Metadata, appErr.Message)
			}
		})
	}
}

// The rules of a command apply to that command only: the same text is something else, or nothing, for another.
func TestTheRulesOfACommandApplyToThatCommand(t *testing.T) {
	for _, tc := range []struct {
		name, command, stdout, stderr, want string
	}{
		{"CONFLICT in the output of status is not a conflict", "status", "CONFLICT (content): x\n", "", "GitCommandFailed"},
		{"CONFLICT in the output of stash apply is a stash conflict", "stash", "CONFLICT (content): x\n", "", "StashConflict"},
		{"CONFLICT in the output of merge is a conflict", "merge", "CONFLICT (content): x\n", "", "Conflict"},
		{"CONFLICT in the output of pull needs a kind in parentheses", "pull", "CONFLICT x\n", "", "GitCommandFailed"},
		{"a pick that could not apply is a cherry-pick conflict", "cherry-pick", "CONFLICT (content): x\n", "", "CherryPickConflict"},
		{"a pick of a commit that is not there is nothing known", "cherry-pick", "", "fatal: bad revision 'x'\n", "GitCommandFailed"},
		{"the same output for rebase is a rebase conflict", "rebase", "CONFLICT (content): x\n", "", "Conflict"},
		{"a rejected push is a push rejection", "push", "", "error: failed to push some refs to '/srv/x.git'\n", "PushRejected"},
		{"the same text for fetch is nothing known", "fetch", "", "error: failed to push some refs to '/srv/x.git'\n", "GitCommandFailed"},
		{"a connection error is the same for fetch, pull and push", "fetch", "", "fatal: Could not read from remote repository.\n", "RemoteConnectionError"},
		{"unable to access beats could not read", "push", "", "fatal: unable to access 'https://x/': y\nfatal: Could not read from remote repository.\n", "CantAccessRemote"},
		{"a dirty tree for checkout", "checkout", "", "Please commit your changes or stash them before you switch branches.\n", "DirtyWorkTree"},
		{"a dirty tree for any command", "merge", "", "Please, commit your changes or stash them before you can merge.\n", "DirtyWorkTree"},
		{"no such path matters for blame only", "log", "", "fatal: no such path x in HEAD\n", "GitCommandFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := classify(tc.command, []string{tc.command}, 1, tc.stdout, tc.stderr)
			wantRoute(t, err, "classify."+tc.want)
		})
	}
}

// The git of today and the git of VS Code's tests word some messages differently (the case of the first letter).
func TestTheMessagesOfOlderAndNewerGitAreBothKnown(t *testing.T) {
	for _, tc := range []struct{ stderr, want string }{
		{"fatal: couldn't find remote ref x\n", "NoRemoteReference"},
		{"fatal: Couldn't find remote ref x\n", "NoRemoteReference"},
		{"fatal: a branch named 'x' already exists\n", "BranchAlreadyExists"},
		{"fatal: A branch named 'x' already exists.\n", "BranchAlreadyExists"},
		{"fatal: not a git repository (or any of the parent directories): .git\n", "NotAGitRepository"},
		{"fatal: Not a git repository\n", "NotAGitRepository"},
		{"fatal: Authentication failed for 'https://x/'\n", "AuthenticationFailed"},
		{"remote: Authentication failed\n", "AuthenticationFailed"},
		// the askpass program answered with an exit status (Escape in the box of Emetos): the text of git 2.56
		{"error: unable to read askpass response from '/run/user/1000/emetos/git-1/askpass.sh'\nfatal: could not read Password for 'http://ada@127.0.0.1:37619': terminal prompts disabled\n", "AuthenticationFailed"},
		{"fatal: could not read Username for 'https://example.com': terminal prompts disabled\n", "AuthenticationFailed"},
	} {
		wantRoute(t, classify("fetch", []string{"fetch"}, 128, "", tc.stderr), "classify."+tc.want)
	}
}
