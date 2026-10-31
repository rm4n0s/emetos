package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// summary is the changes of a commit as "S path" or "S old>new" lines.
func summary(changes []NameStatus) []string {
	var out []string
	for _, c := range changes {
		s := string(c.Status) + " " + c.Path
		if c.OrigPath != "" {
			s = string(c.Status) + " " + c.OrigPath + ">" + c.Path
		}
		out = append(out, s)
	}
	return out
}

func TestCommitChanges(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("history")
	ctx := context.Background()
	r.Write("a.txt", "one\ntwo\nthree\nfour\nfive\nsix\n")
	r.Write("b.txt", "bee\nbee\nbee\nbee\n")
	r.Write("run.sh", "echo hi\n")
	r.Write("gone.txt", "to be deleted\n")
	root := r.Commit("root commit")
	r.Write("a.txt", "one\n2\nthree\nfour\nfive\nsix\n")
	r.Remove("gone.txt")
	r.Write("new.txt", "new\n")
	r.Git("mv", "b.txt", "bb.txt")
	if err := os.Chmod(r.Path("run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	normal := r.Commit("a normal commit")
	trunk := strings.TrimSpace(r.Git("rev-parse", "--abbrev-ref", "HEAD"))
	r.Branch("side", "")
	r.Checkout("side")
	r.Write("side.txt", "side\n")
	r.Commit("side commit", "side.txt")
	r.Checkout(trunk)
	r.Write("main.txt", "main\n")
	r.Commit("main commit", "main.txt")
	if err := r.Merge("side"); err != nil {
		t.Fatal(err)
	}
	merge := r.Head()
	repo := open(t, e, r.Dir)
	emptyTree, err := repo.EmptyTree(ctx)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		id       string
		wantBase string
		root     bool
		message  string
		want     []string
	}{
		{"a root commit is against the empty tree", root, emptyTree, true, "root commit", []string{"A a.txt", "A b.txt", "A gone.txt", "A run.sh"}},
		{"a normal commit is against its parent: a modification, a deletion, an addition, a rename and a mode change", normal, root, false, "a normal commit",
			[]string{"M a.txt", "R b.txt>bb.txt", "D gone.txt", "A new.txt", "M run.sh"}},
		{"a merge commit is against its first parent", merge, "", false, "Merge side", []string{"A side.txt"}},
		{"a revision by name", "HEAD", "", false, "Merge side", []string{"A side.txt"}},
		{"a revision with an ancestor", "HEAD~1^0", "", false, "main commit", []string{"A main.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.CommitChanges(ctx, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantBase != "" && got.Base != tc.wantBase {
				t.Errorf("base %s, want %s", got.Base, tc.wantBase)
			}
			if got.Root != tc.root || !strings.HasPrefix(got.Commit.Message, tc.message) {
				t.Errorf("root %v message %q", got.Root, got.Commit.Message)
			}
			if tc.id == merge && got.Base != got.Commit.Parents[0] {
				t.Errorf("the merge is compared with %s, not its first parent %s", got.Base, got.Commit.Parents[0])
			}
			if s := summary(got.Changes); !equal(s, tc.want) {
				t.Errorf("changes %q, want %q", s, tc.want)
			}
		})
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCommitChangesFailures(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("history")
	r.Write("a.txt", "a\n")
	r.Commit("one")
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	for _, id := range []string{"", "--output=/tmp/x", "-1"} {
		_, err := repo.CommitChanges(ctx, id)
		_ = routeOf(t, err, "Repo.CommitChanges.GitRevisionInvalid")
	}
	if _, err := repo.CommitChanges(ctx, "no-such-branch"); err == nil {
		t.Error("a revision that names no commit did not fail")
	}
	// a repository with no commit has none to show
	empty := e.Init("empty")
	_, err := open(t, e, empty.Dir).CommitChanges(ctx, "HEAD")
	if err == nil {
		t.Error("a repository with no commit gave changes")
	}
}

func TestCommitChangesFailuresHaveTheirRoutes(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("history")
	r.Write("a.txt", "one\n")
	r.Commit("root commit")
	repo := open(t, e, r.Dir)
	ctx := context.Background()

	// a range that holds no commit
	_, err := repo.CommitChanges(ctx, "HEAD..HEAD")
	wantRoute(t, err, "Repo.CommitChanges.GitCommitNotFound")
	_, err = repo.CommitChanges(ctx, "--all")
	wantRoute(t, err, "Repo.CommitChanges.GitRevisionInvalid")

	// a git that prints something else for the empty tree
	script := filepath.Join(t.TempDir(), "git")
	body := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = hash-object ]; then echo not-an-object; exit 0; fi; done\nexec " + e.GitPath + " \"$@\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	odd, err := Open(ctx, NewRunner(Config{Trust: trustAll, Path: script, Env: e.Vars()}), nil, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = odd.EmptyTree(ctx)
	wantRoute(t, err, "Repo.EmptyTree.GitEmptyTreeInvalid")
}
