package gittest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureContent(t *testing.T) {
	small := filepath.Join(t.TempDir(), "small")
	Fixture(t, "small", small, FixtureOptions{})
	conflict := filepath.Join(t.TempDir(), "conflict")
	Fixture(t, "conflict", conflict, FixtureOptions{})
	history := filepath.Join(t.TempDir(), "history")
	Fixture(t, "history", history, FixtureOptions{})
	many := filepath.Join(t.TempDir(), "many")
	Fixture(t, "many", many, FixtureOptions{Files: 1200})
	e := NewEnv(t)

	status := func(dir string) string {
		out, _, err := e.GitErr(dir, "status", "--porcelain=v1", "-uall")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	t.Run("every fixture has a loader of 40 lines", func(t *testing.T) {
		for _, dir := range []string{small, conflict, history, many} {
			data, err := os.ReadFile(filepath.Join(dir, "src", "loader.go"))
			if err != nil {
				t.Fatal(err)
			}
			if lines := strings.Count(string(data), "\n"); lines < 38 || lines > 42 {
				if !strings.Contains(dir, "conflict") { // the conflict markers add lines
					t.Errorf("%s: src/loader.go has %d lines, want about 40", dir, lines)
				}
			}
		}
	})

	t.Run("small has a change in every group", func(t *testing.T) {
		want := []string{
			"M  README.md",
			" D docs/notes.md",
			"R  src/loader_test.go -> src/loader_check_test.go",
			" M src/loader.go",
			"?? docs/todo.md",
		}
		got := strings.Split(status(small), "\n")
		if strings.Join(sortedCopy(got), "|") != strings.Join(sortedCopy(want), "|") {
			t.Errorf("status = %q, want %q", got, want)
		}
		// The unstaged edit is at lines 12 and 20.
		diff := e.Git(small, "diff", "-U0", "src/loader.go")
		if !strings.Contains(diff, "@@ -12 +12 @@") || !strings.Contains(diff, "@@ -20 +20 @@") {
			t.Errorf("the edits of src/loader.go are not at lines 12 and 20:\n%s", diff)
		}
		if n := strings.Count(e.Git(small, "log", "--format=%H"), "\n") + 1; n != 2 {
			t.Errorf("small has %d commits, want 2", n)
		}
	})

	t.Run("conflict is stopped in the middle of a merge", func(t *testing.T) {
		if got := status(conflict); got != "UU src/loader.go" {
			t.Errorf("status = %q", got)
		}
		if _, err := os.Stat(filepath.Join(conflict, ".git", "MERGE_HEAD")); err != nil {
			t.Errorf("no MERGE_HEAD: %v", err)
		}
		if data, _ := os.ReadFile(filepath.Join(conflict, "src", "loader.go")); !strings.Contains(string(data), "<<<<<<<") {
			t.Error("src/loader.go has no conflict marker")
		}
	})

	t.Run("history has 300 commits on main, branches, tags, an origin and a worktree", func(t *testing.T) {
		if got := e.Git(history, "rev-list", "--count", "--first-parent", "origin/main~2"); got != "300" {
			t.Errorf("the commits of main before the last two of origin = %s, want 300", got)
		}
		if got := e.Git(history, "rev-list", "--left-right", "--count", "main...origin/main"); got != "1\t2" {
			t.Errorf("main...origin/main = %q, want 1 ahead and 2 behind", got)
		}
		branches := e.Git(history, "for-each-ref", "--format=%(refname:short)", "refs/heads")
		if branches != "feature/a\nfeature/b\nmain" {
			t.Errorf("branches = %q", branches)
		}
		tags := e.Git(history, "tag", "-l")
		if tags != "v0.1.0\nv1.0.0" {
			t.Errorf("tags = %q", tags)
		}
		if got := e.Git(history, "cat-file", "-t", "v1.0.0"); got != "tag" {
			t.Errorf("v1.0.0 is a %s, want an annotated tag", got)
		}
		if got := e.Git(history, "rev-list", "--merges", "--count", "main"); got != "2" {
			t.Errorf("merge commits = %s, want 2", got)
		}
		authors := e.Git(history, "log", "--format=%an", "--", "src/loader.go")
		seen := map[string]bool{}
		for _, a := range strings.Split(authors, "\n") {
			seen[a] = true
		}
		if len(seen) != 3 || seen["Ada Example"] == false {
			t.Errorf("authors of src/loader.go = %v", seen)
		}
		if got := status(history); got != "" {
			t.Errorf("history is not clean: %q", got)
		}
		list := e.Git(history, "worktree", "list", "--porcelain")
		if !strings.Contains(list, history+".worktrees/feature") || !strings.Contains(list, "branch refs/heads/feature/a") {
			t.Errorf("worktrees:\n%s", list)
		}
		if got := e.Git(history, "remote", "get-url", "origin"); got != history+".origin.git" {
			t.Errorf("origin = %q", got)
		}
		if got := e.Git(history, "config", "user.email"); got != AuthorEmail {
			t.Errorf("the fixture has no identity of its own: %q", got)
		}
	})

	t.Run("many has its files changed", func(t *testing.T) {
		out := status(many)
		if n := strings.Count(out, "\n") + 1; n != 1200 {
			t.Errorf("changed files = %d, want 1200", n)
		}
		if !strings.HasPrefix(out, " M gen/d000/f00000.txt") {
			t.Errorf("first line = %q", strings.SplitN(out, "\n", 2)[0])
		}
	})
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestFixturesAreByteForByteReproducible builds each fixture twice, in different folders, and compares the manifests: every
// reference with its object, the index, the status and the content and mode of every file.
func TestFixturesAreByteForByteReproducible(t *testing.T) {
	for _, kind := range FixtureKinds {
		t.Run(kind, func(t *testing.T) {
			opts := FixtureOptions{Files: 300}
			a := filepath.Join(t.TempDir(), "first", kind)
			b := filepath.Join(t.TempDir(), "second", kind)
			Fixture(t, kind, a, opts)
			Fixture(t, kind, b, opts)
			ma, mb := Manifest(t, a), Manifest(t, b)
			if ma != mb {
				t.Errorf("two runs of %s differ: %s and %s", kind, ma, mb)
			}
			// A change makes the manifest differ (the manifest is not constant).
			if err := os.WriteFile(filepath.Join(b, "README.md"), []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if Manifest(t, b) == ma {
				t.Error("the manifest does not see a changed file")
			}
		})
	}
}

func TestUnknownFixtureKind(t *testing.T) {
	fake := &recordingTB{TB: t}
	defer func() {
		if recover() == nil || !strings.Contains(fake.message, "unknown fixture") {
			t.Errorf("an unknown kind did not fail: %q", fake.message)
		}
	}()
	Fixture(fake, "nope", t.TempDir(), FixtureOptions{})
}

// recordingTB is a TB whose Fatalf panics with the message, so a test can see a failure of the harness.
type recordingTB struct {
	TB
	message string
}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.message = format
	panic("fatal")
}
