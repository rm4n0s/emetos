package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// Commit and the commands around it against real repositories (step 24.10).

// commitBase makes a repository with one commit and a staged change to a.txt.
func commitBase(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("commit")
	r.Write("a.txt", "one\n")
	r.Write("b.txt", "bee\n")
	r.Commit("Add the files")
	r.Write("a.txt", "two\n")
	r.Git("add", "a.txt")
	return r
}

// last reads a field of the last commit with git itself.
func last(r *gittest.Repo, format string) string { return r.Git("log", "-1", "--format="+format) }

func ptr[T any](v T) *T { return &v }

func TestCommit(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(r *gittest.Repo)
		opts    CommitOptions
		message string   // %B of the last commit, without the final newline
		count   string   // the number of commits afterwards
		files   []string // the files the last commit changed
		trailer string   // a line that the message must contain
	}{
		{"a message", nil, CommitOptions{Message: "Fix the cache key"}, "Fix the cache key", "2", []string{"a.txt"}, ""},
		{"a message with a body and non-ASCII text", nil, CommitOptions{Message: "Corrige la clé\n\nLe schéma était ignoré: ünï €"}, "Corrige la clé\n\nLe schéma était ignoré: ünï €", "2", []string{"a.txt"}, ""},
		{"a message that starts with a dash and has quotes", nil, CommitOptions{Message: `-n "quoted" 'single' $HOME`}, `-n "quoted" 'single' $HOME`, "2", []string{"a.txt"}, ""},
		{"no message and no editor gives an empty message", nil, CommitOptions{}, "", "2", []string{"a.txt"}, ""},
		{"signed off", nil, CommitOptions{Message: "Fix", Signoff: true}, "Fix\n\nSigned-off-by: Ada Example <ada@example.com>", "2", []string{"a.txt"}, "Signed-off-by: Ada Example <ada@example.com>"},
		{"amend with a new message", nil, CommitOptions{Message: "Better words", Amend: true}, "Better words", "1", []string{"a.txt", "b.txt"}, ""},
		{"amend keeps the message when none is given", nil, CommitOptions{Amend: true}, "Add the files", "1", []string{"a.txt", "b.txt"}, ""},
		{"an empty commit", func(r *gittest.Repo) { r.Git("reset", "-q", "a.txt"); r.Git("checkout", "--", "a.txt") }, CommitOptions{Message: "Nothing here", Empty: true}, "Nothing here", "2", nil, ""},
		{"the editor of git writes the message", nil, CommitOptions{UseEditor: true}, "From the editor", "2", []string{"a.txt"}, ""},
		{"the message beats the editor", nil, CommitOptions{UseEditor: true, Message: "Given"}, "Given", "2", []string{"a.txt"}, ""},
		{"verbose with the editor", nil, CommitOptions{UseEditor: true, Verbose: true}, "From the editor", "2", []string{"a.txt"}, ""},
		{"required user configuration that is there", nil, CommitOptions{Message: "Configured", RequireUserConfig: true}, "Configured", "2", []string{"a.txt"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := commitBase(t, e)
			if tc.setup != nil {
				tc.setup(r)
			}
			// the editor of git is a script that writes the message, as the helper of the Git extension does
			editor := filepath.Join(e.BinDir, "editor")
			if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf 'From the editor\\n' > \"$1\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			runner := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: withVars(e.Vars(), "GIT_EDITOR="+editor)})
			repo, err := Open(context.Background(), runner, nil, r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Commit(context.Background(), tc.opts); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimRight(last(r, "%B"), "\n"); got != tc.message {
				t.Errorf("message = %q, want %q", got, tc.message)
			}
			if got := r.Git("rev-list", "--count", "HEAD"); got != tc.count {
				t.Errorf("%s commits, want %s", got, tc.count)
			}
			var files []string
			if out := r.Git("show", "--format=", "--name-only", "HEAD"); out != "" {
				files = strings.Split(out, "\n")
			}
			if strings.Join(files, ",") != strings.Join(tc.files, ",") {
				t.Errorf("changed files = %v, want %v", files, tc.files)
			}
			if tc.trailer != "" && !strings.Contains(last(r, "%B"), tc.trailer) {
				t.Errorf("message %q lacks %q", last(r, "%B"), tc.trailer)
			}
			if got := last(r, "%an <%ae>"); got != "Ada Example <ada@example.com>" {
				t.Errorf("author = %q", got)
			}
			if status := r.Git("status", "--porcelain"); strings.Contains(status, "a.txt") {
				t.Errorf("a.txt is still changed: %q", status)
			}
		})
	}
}

func TestCommitAmendKeepsTheAuthorAndTheFiles(t *testing.T) {
	e := gittest.NewEnv(t)
	r := commitBase(t, e)
	r.Write("c.txt", "see\n")
	r.Git("add", "c.txt")
	repo := open(t, e, r.Dir)
	if err := repo.Commit(context.Background(), CommitOptions{Amend: true}); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("show", "--format=", "--name-only", "HEAD"); got != "a.txt\nb.txt\nc.txt" {
		t.Errorf("the amended commit has %q", got)
	}
}

func TestCommitFailures(t *testing.T) {
	const route = "Repo.Commit->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify."
	t.Run("nothing to commit", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := commitBase(t, e)
		r.Git("reset", "-q", "a.txt")
		repo := open(t, e, r.Dir)
		err := repo.Commit(context.Background(), CommitOptions{Message: "x"})
		appErr := routeOf(t, err, route+"GitCommandFailed")
		if !strings.Contains(appErr.Message, "git commit failed") {
			t.Errorf("message = %q", appErr.Message)
		}
	})
	t.Run("a hook that stops the commit says why", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := commitBase(t, e)
		r.Hook("pre-commit", "#!/bin/sh\necho 'lint: a.txt has trailing spaces' >&2\necho 'second line of the hook'\nexit 1\n")
		repo := open(t, e, r.Dir)
		err := repo.Commit(context.Background(), CommitOptions{Message: "x"})
		appErr := routeOf(t, err, route+"GitCommandFailed")
		stderr, _ := appErr.Metadata["stderr"].(string)
		for _, want := range []string{"lint: a.txt has trailing spaces", "second line of the hook"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("the error output %q lacks %q", stderr, want)
			}
		}
		if got := r.Git("rev-list", "--count", "HEAD"); got != "1" {
			t.Errorf("%s commits after a hook stopped the commit", got)
		}
	})
	t.Run("the hook that passes runs, and no-verify skips the one that fails", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := commitBase(t, e)
		marker := filepath.Join(e.Root, "hook-ran")
		r.Hook("pre-commit", "#!/bin/sh\necho ran > '"+marker+"'\nexit 0\n")
		repo := open(t, e, r.Dir)
		if err := repo.Commit(context.Background(), CommitOptions{Message: "with the hook"}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("the pre-commit hook did not run: %v", err)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		r.Hook("pre-commit", "#!/bin/sh\necho ran > '"+marker+"'\nexit 1\n")
		r.Hook("commit-msg", "#!/bin/sh\necho ran > '"+marker+"'\nexit 1\n")
		r.Write("b.txt", "bee two\n")
		r.Git("add", "b.txt")
		if err := repo.Commit(context.Background(), CommitOptions{Message: "skips the hooks", NoVerify: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Error("a hook ran with NoVerify")
		}
		if got := last(r, "%s"); got != "skips the hooks" {
			t.Errorf("last commit = %q", got)
		}
	})
	t.Run("unmerged files", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := e.Init("merge")
		r.Write("f.txt", "base\n")
		r.Commit("base")
		ours, theirs := "", ""
		r.Branch("other", "")
		r.Write("f.txt", "main side\n")
		r.Commit("main side")
		r.Checkout("other")
		r.Write("f.txt", "other side\n")
		r.Commit("other side")
		r.Checkout("main")
		_ = ours + theirs
		if err := r.Merge("other"); err == nil {
			t.Fatal("the merge did not conflict")
		}
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Commit(context.Background(), CommitOptions{Message: "x"}), route+"UnmergedChanges")
	})
	t.Run("the editor leaves an empty message", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := commitBase(t, e)
		repo := open(t, e, r.Dir) // GIT_EDITOR=true leaves the message file as git wrote it: only comments
		wantRoute(t, repo.Commit(context.Background(), CommitOptions{UseEditor: true}), route+"EmptyCommitMessage")
	})
	t.Run("the identity", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(e *gittest.Env, r *gittest.Repo)
			tag   string
		}{
			// git names the e-mail first when it has neither; the Git extension asks for both in either case
			{"neither name nor e-mail", func(e *gittest.Env, r *gittest.Repo) {}, "NoUserEmailConfigured"},
			{"a name and no e-mail", func(e *gittest.Env, r *gittest.Repo) { r.Git("config", "user.name", "Ada") }, "NoUserEmailConfigured"},
			{"an e-mail and no name", func(e *gittest.Env, r *gittest.Repo) { r.Git("config", "user.email", "ada@example.com") }, "NoUserNameConfigured"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				e := gittest.NewEnv(t)
				r := commitBase(t, e)
				e.Identity = false // from here the world has no author, no committer and no user.name or user.email
				tc.setup(e, r)
				repo := open(t, e, r.Dir)
				wantRoute(t, repo.Commit(context.Background(), CommitOptions{Message: "x", RequireUserConfig: true}), route+tc.tag)
				if got := r.Git("rev-list", "--count", "HEAD"); got != "1" {
					t.Errorf("%s commits after the failure", got)
				}
			})
		}
	})
}

// logRunner makes a Runner whose Git channel lines are collected.
func logRunner(e *gittest.Env) (*Runner, func() string) {
	var mu sync.Mutex
	var lines []string
	r := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars(), Logger: func(line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	}})
	return r, func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(lines, "\n")
	}
}

func TestCommitPassesTheOptionsToGit(t *testing.T) {
	tests := []struct {
		name  string
		opts  CommitOptions
		want  []string
		avoid []string
	}{
		{"signing", CommitOptions{Message: "x", Sign: ptr(true)}, []string{"commit --quiet --allow-empty-message --file - --allow-empty-message -S"}, []string{"--no-gpg-sign"}},
		{"signing off by option", CommitOptions{Message: "x", Sign: ptr(false)}, []string{"--no-gpg-sign"}, []string{" -S"}},
		{"the configuration decides when the option is nil", CommitOptions{Message: "x"}, nil, []string{" -S", "--no-gpg-sign"}},
		{"verbose with the editor", CommitOptions{UseEditor: true, Verbose: true}, []string{"commit --quiet --verbose"}, []string{"--allow-empty-message"}},
		{"no verify", CommitOptions{Message: "x", NoVerify: true}, []string{"--no-verify"}, nil},
		{"empty", CommitOptions{Message: "x", Empty: true}, []string{"--allow-empty "}, nil},
		{"the user configuration is required", CommitOptions{Message: "x", RequireUserConfig: true}, []string{"-c user.useConfigOnly=true commit"}, nil},
		{"the user configuration is not required", CommitOptions{Message: "x"}, nil, []string{"useConfigOnly"}},
		{"amend with no message", CommitOptions{Amend: true}, []string{"--amend --no-edit"}, []string{"--file"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := commitBase(t, e)
			// a signing program that fails: the command line is the thing under test, no key is made
			r.Git("config", "gpg.program", "false")
			runner, logged := logRunner(e)
			repo, err := Open(context.Background(), runner, nil, r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			_ = repo.Commit(context.Background(), tc.opts) // a signed commit fails here, and that is fine
			var line string
			for _, l := range strings.Split(logged(), "\n") {
				if strings.HasPrefix(l, "> git") && strings.Contains(l, " commit ") {
					line = l
				}
			}
			if line == "" {
				t.Fatalf("no git commit in the log:\n%s", logged())
			}
			for _, w := range tc.want {
				if !strings.Contains(line+" ", w) {
					t.Errorf("%q lacks %q", line, w)
				}
			}
			for _, a := range tc.avoid {
				if strings.Contains(line, a) {
					t.Errorf("%q has %q", line, a)
				}
			}
		})
	}
}

func TestResetSoftAndDeleteRef(t *testing.T) {
	e := gittest.NewEnv(t)
	r := commitBase(t, e)
	r.Git("commit", "-q", "-m", "Second")
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	if err := repo.ResetSoft(ctx, "HEAD~"); err != nil {
		t.Fatal(err)
	}
	if got := last(r, "%s"); got != "Add the files" {
		t.Errorf("HEAD is %q after the soft reset", got)
	}
	if got := strings.Join(porcelain(r), "|"); got != "M  a.txt" {
		t.Errorf("the changes of the commit are not staged: %q", got)
	}
	wantRoute(t, repo.ResetSoft(ctx, "no-such-commit"), "Repo.ResetSoft->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.GitCommandFailed")
	// the first commit goes: the branch has none, and the files stay in the index
	if err := repo.DeleteRef(ctx, "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.GitErr("rev-parse", "--verify", "-q", "HEAD"); err == nil {
		t.Error("HEAD still names a commit")
	}
	wantRoute(t, repo.DeleteRef(ctx, "bad..ref"), "Repo.DeleteRef->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.GitCommandFailed")
}

func TestCreateBranch(t *testing.T) {
	e := gittest.NewEnv(t)
	r := commitBase(t, e)
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	if err := repo.CreateBranch(ctx, "feature/x"); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("branch", "--show-current"); got != "feature/x" {
		t.Errorf("on %q", got)
	}
	if got := strings.Join(porcelain(r), "|"); got != "M  a.txt" {
		t.Errorf("the staged change did not come along: %q", got)
	}
	wantRoute(t, repo.CreateBranch(ctx, "feature/x"), "Repo.CreateBranch->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.BranchAlreadyExists")
	wantRoute(t, repo.CreateBranch(ctx, "bad name~"), "Repo.CreateBranch->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.InvalidBranchName")
}

func TestLastCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("fresh")
	repo := open(t, e, r.Dir)
	wantRoute(t, errOf(repo.LastCommit(context.Background())), "Repo.LastCommit.GitNoCommit")
	r.Write("a.txt", "a\n")
	r.Commit("First\n\nBody line")
	c, err := repo.LastCommit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Message != "First\n\nBody line" || len(c.Parents) != 0 || c.AuthorName != "Ada Example" {
		t.Errorf("commit = %+v", c)
	}
	r.Write("a.txt", "b\n")
	r.Commit("Second")
	if c, _ := repo.LastCommit(context.Background()); c.Message != "Second" || len(c.Parents) != 1 {
		t.Errorf("commit = %+v", c)
	}
}

func errOf[T any](_ T, err error) error { return err }

func TestStripCommitMessageComments(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"no comment", "Fix the thing\n", "Fix the thing"},
		{"comment lines go", "# Title\nFix\n# more\n\nbody\n", "Fix\n\nbody"},
		{"an indented comment goes", "Fix\n   # not me\n", "Fix"},
		{"a hash inside a line stays", "Fix #123 now\n", "Fix #123 now"},
		{"only comments", "# a\n# b\n", ""},
		{"white space around", "\n\n  Fix  \n\n", "Fix"},
		{"windows line breaks", "Fix\r\n# no\r\nmore\r\n", "Fix\r\nmore"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripCommitMessageComments(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommitTemplate(t *testing.T) {
	e := gittest.NewEnv(t)
	home := e.Home
	tests := []struct {
		name  string
		value string // the setting; "-" leaves it unset
		files map[string]string
		want  string
	}{
		{"not set", "-", nil, ""},
		{"a file in the repository", "tpl.txt", map[string]string{"tpl.txt": "Subject\n# comment\n\nBody\n"}, "Subject\n\nBody"},
		{"a file in a folder of the repository", "dir/tpl.txt", map[string]string{"dir/tpl.txt": "From a folder\n"}, "From a folder"},
		{"the home folder", "~/tpl.txt", map[string]string{"../home/tpl.txt": "From home\n"}, "From home"},
		{"a missing file", "missing.txt", nil, ""},
		{"an empty setting", "", nil, ""},
		{"a file of only comments", "tpl.txt", map[string]string{"tpl.txt": "# nothing\n"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := e.Init("tpl")
			r.Write("a.txt", "a\n")
			r.Commit("base")
			if tc.value != "-" {
				r.Git("config", "commit.template", tc.value)
			}
			for rel, text := range tc.files {
				path := filepath.Join(r.Dir, filepath.FromSlash(rel))
				if strings.HasPrefix(rel, "../home/") {
					path = filepath.Join(e.Home, strings.TrimPrefix(rel, "../home/"))
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			repo := open(t, e, r.Dir)
			if got := repo.CommitTemplate(context.Background(), e.Home); got != tc.want {
				t.Errorf("template = %q, want %q", got, tc.want)
			}
		})
	}
	_ = home
}

func TestExpandTilde(t *testing.T) {
	tests := []struct{ path, home, want string }{
		{"~/t.txt", "/home/ada", "/home/ada/t.txt"},
		{"~bob/t.txt", "/home/ada", "/home/bob/t.txt"},
		{"~", "/home/ada", "~"},
		{"/abs/t.txt", "/home/ada", "/abs/t.txt"},
		{"rel/t.txt", "/home/ada", "rel/t.txt"},
		{"~/t.txt", "", "~/t.txt"},
	}
	for _, tc := range tests {
		if got := expandTilde(tc.path, tc.home); got != tc.want {
			t.Errorf("expandTilde(%q, %q) = %q, want %q", tc.path, tc.home, got, tc.want)
		}
	}
}

func TestInputTemplate(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("input")
	r.Write("f.txt", "base\n")
	r.Commit("base")
	r.Write("tpl.txt", "From the template\n")
	r.Git("config", "commit.template", "tpl.txt")
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	if got := repo.InputTemplate(ctx, e.Home); got != "From the template" {
		t.Errorf("template = %q", got)
	}
	// a squash message beats the template, a merge message beats both
	gitDir := filepath.Join(r.Dir, ".git")
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(gitDir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("SQUASH_MSG", "Squashed commit of the following:\n# Conflicts:\n#\tf.txt\n")
	if got := repo.InputTemplate(ctx, e.Home); got != "Squashed commit of the following:" {
		t.Errorf("with a squash message: %q", got)
	}
	write("MERGE_MSG", "Merge branch 'other'\n\n# Conflicts:\n#\tf.txt\n")
	if got := repo.InputTemplate(ctx, e.Home); got != "Merge branch 'other'" {
		t.Errorf("with a merge message: %q", got)
	}
	if repo.MergeMessage() != "Merge branch 'other'" || repo.SquashMessage() != "Squashed commit of the following:" {
		t.Errorf("merge %q squash %q", repo.MergeMessage(), repo.SquashMessage())
	}
}
