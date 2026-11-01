package gittest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// FixtureKinds are the names Fixture accepts.
var FixtureKinds = []string{"small", "conflict", "history", "many"}

// FixtureOptions tune a fixture.
type FixtureOptions struct {
	// Files is the number of changed files of the fixture "many"; the default is 100,000.
	Files int
}

// person is an author of the fixtures: invented names at example.com (RFC 2606), no real person.
type person struct{ name, email string }

var fixtureAuthors = []person{
	{AuthorName, AuthorEmail},
	{"Bo Example", "bo@example.com"},
	{"Cy Example", "cy@example.com"},
}

// Fixture builds the fixture of the given kind in the folder dir (which must be missing or empty) and returns its
// repository. The fixtures are deterministic: two runs give the same hashes and the same files. All have src/loader.go of
// 40 lines. The kinds (docs/IMPLEMENTATION.md step 24.1):
//
//   - small: README.md, docs/notes.md, src/loader.go and src/loader_test.go in two commits, and a change in every group of
//     the Source Control view: a staged edit (README.md), an unstaged edit at lines 12 and 20 of src/loader.go, an
//     untracked file (docs/todo.md), a deleted file (docs/notes.md) and a staged rename (src/loader_test.go);
//   - conflict: the same files, stopped in the middle of a merge with src/loader.go conflicted;
//   - history: 300 commits on main, two feature branches with merges, tags, a bare origin (dir + ".origin.git") it is
//     cloned from that is 1 commit ahead and 2 behind, src/loader.go changed by three authors, and a linked worktree
//     dir + ".worktrees/feature" on the branch feature/a;
//   - many: one commit of opts.Files files (100,000 by default) that are all modified in the work tree.
func Fixture(t TB, kind, dir string, opts FixtureOptions) *Repo {
	t.Helper()
	e := NewEnv(t)
	switch kind {
	case "small":
		return e.fixtureSmall(dir)
	case "conflict":
		return e.fixtureConflict(dir)
	case "history":
		return e.fixtureHistory(dir)
	case "many":
		n := opts.Files
		if n <= 0 {
			n = 100000
		}
		return e.fixtureMany(dir, n)
	}
	t.Fatalf("gittest: unknown fixture %q (want one of %s)", kind, strings.Join(FixtureKinds, ", "))
	return nil
}

// persistIdentity writes the test identity into the repository's own configuration, so that the fixture is usable in an
// environment that has no identity of its own (a proof).
func (r *Repo) persistIdentity() {
	r.Git("config", "user.name", AuthorName)
	r.Git("config", "user.email", AuthorEmail)
	// The folder of core.hooksPath belongs to the world of this run, which is gone when the fixture is used.
	r.unsetHooksPath()
}

// unsetHooksPath removes core.hooksPath from the local configuration; a repository that never had it is fine (git exits 5).
func (r *Repo) unsetHooksPath() {
	r.Env.t.Helper()
	if _, errOut, err := r.GitErr("config", "--unset", "core.hooksPath"); err != nil && exitCode(err) != 5 {
		r.Env.t.Fatalf("gittest: git config --unset core.hooksPath in %s: %v\n%s", r.Dir, err, errOut)
	}
}

// exitCode returns the exit status of a failed command, or -1.
func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// writeBase writes the four files that every fixture starts with.
func (r *Repo) writeBase() {
	r.Write("README.md", "# Loader\n\nA small library that reads the files of a project.\n")
	r.Write("docs/notes.md", "# Notes\n\n- the loader retries a read\n- paths are cleaned first\n")
	r.Write("src/loader.go", LoaderSource(0))
	r.Write("src/loader_test.go", "package loader\n\nimport \"testing\"\n\nfunc TestExists(t *testing.T) {\n\tif Exists(\"/no/such/file\") {\n\t\tt.Fatal(\"the file should not exist\")\n\t}\n}\n")
}

func (e *Env) fixtureSmall(dir string) *Repo {
	r := e.InitAt(dir)
	r.persistIdentity()
	r.writeBase()
	r.Commit("Add the loader")
	r.Write("docs/notes.md", "# Notes\n\n- the loader retries a read\n- paths are cleaned first\n- errors are returned as they are\n")
	r.Commit("Describe the errors")
	// A staged edit.
	r.Write("README.md", r.Read("README.md")+"\nSee docs/notes.md.\n")
	r.Git("add", "README.md")
	// An unstaged edit at lines 12 and 20 of src/loader.go.
	r.Write("src/loader.go", editLines(r.Read("src/loader.go"), map[int]string{12: "\tretries := 2", 20: "\treturn data, err"}))
	// An untracked file, a deleted file and a renamed file.
	r.Write("docs/todo.md", "- cache the files\n")
	r.Remove("docs/notes.md")
	r.Git("mv", "src/loader_test.go", "src/loader_check_test.go")
	return r
}

func (e *Env) fixtureConflict(dir string) *Repo {
	r := e.InitAt(dir)
	r.persistIdentity()
	r.writeBase()
	r.Commit("Add the loader")
	r.ConflictingBranches()
	if err := r.Merge("theirs"); err == nil {
		e.t.Fatalf("gittest: the merge of the conflict fixture did not stop")
	}
	return r
}

// importer writes a git fast-import stream.
type importer struct {
	b strings.Builder
}

type fileChange struct{ path, content string }

func (im *importer) data(s string) {
	im.b.WriteString("data " + strconv.Itoa(len(s)) + "\n" + s + "\n")
}

// commit adds a commit on ref with the given mark (0 for none), author, time and message; from and merge are marks (":12")
// or revisions, empty for none.
func (im *importer) commit(ref string, mark int, who person, at time.Time, message, from, merge string, files ...fileChange) {
	im.b.WriteString("commit " + ref + "\n")
	if mark > 0 {
		im.b.WriteString("mark :" + strconv.Itoa(mark) + "\n")
	}
	stamp := strconv.FormatInt(at.Unix(), 10) + " +0000"
	im.b.WriteString("author " + who.name + " <" + who.email + "> " + stamp + "\n")
	im.b.WriteString("committer " + who.name + " <" + who.email + "> " + stamp + "\n")
	im.data(message)
	if from != "" {
		im.b.WriteString("from " + from + "\n")
	}
	if merge != "" {
		im.b.WriteString("merge " + merge + "\n")
	}
	for _, f := range files {
		im.b.WriteString("M 100644 inline " + f.path + "\n")
		im.data(f.content)
	}
}

// run feeds the stream to "git fast-import" in the repository.
func (im *importer) run(r *Repo) {
	r.Env.t.Helper()
	if _, errOut, err := r.Env.GitInput(r.Dir, im.b.String(), "fast-import", "--quiet", "--force"); err != nil {
		r.Env.t.Fatalf("gittest: git fast-import in %s: %v\n%s", r.Dir, err, errOut)
	}
}

func (e *Env) fixtureHistory(dir string) *Repo {
	origin := e.InitBareAt(dir + ".origin.git")
	at := func(i int) time.Time { return baseDate.Add(time.Duration(i) * time.Minute) }
	who := func(i int) person { return fixtureAuthors[i%len(fixtureAuthors)] }
	base := []fileChange{
		{"README.md", "# Loader\n\nA small library that reads the files of a project.\n"},
		{"docs/notes.md", "# Notes\n\n- the loader retries a read\n- paths are cleaned first\n"},
		{"src/loader.go", LoaderSource(0)},
		{"src/loader_test.go", "package loader\n\nimport \"testing\"\n\nfunc TestExists(t *testing.T) {\n\tif Exists(\"/no/such/file\") {\n\t\tt.Fatal(\"the file should not exist\")\n\t}\n}\n"},
	}
	var im importer
	// main: 300 commits; marks 1..300 are the commits of main. 200 and 250 are merges of the feature branches.
	const featureA, featureB = 1001, 2001 // the first mark of each feature branch
	const lastA, lastB = featureA + 19, featureB + 14
	for i := 1; i <= 300; i++ {
		from := ""
		if i > 1 {
			from = ":" + strconv.Itoa(i-1)
		}
		files := []fileChange{{"src/loader.go", LoaderSource(i)}}
		if i == 1 {
			files = base
		}
		// A fast-import merge keeps the tree of its first parent and the changes given, so the merged branch's file is given.
		switch i {
		case 200:
			files = append(files, fileChange{"docs/feature-a.md", "step 20\n"})
		case 250:
			files = append(files, fileChange{"docs/feature-b.md", "step 15\n"})
		}
		message := "Update the loader, revision " + strconv.Itoa(i)
		merge := ""
		switch i {
		case 200:
			merge, message = ":"+strconv.Itoa(lastA), "Merge feature/a"
		case 250:
			merge, message = ":"+strconv.Itoa(lastB), "Merge feature/b"
		}
		im.commit("refs/heads/main", i, who(i), at(i), message, from, merge, files...)
		switch i {
		case 100: // feature/a starts here and runs for 20 commits
			for j := 0; j < 20; j++ {
				f := ":100"
				if j > 0 {
					f = ":" + strconv.Itoa(featureA+j-1)
				}
				im.commit("refs/heads/feature/a", featureA+j, who(j+1), at(1000+j), "Feature A, step "+strconv.Itoa(j+1), f, "",
					fileChange{"docs/feature-a.md", "step " + strconv.Itoa(j+1) + "\n"})
			}
		case 150: // feature/b starts here: 15 commits are merged at 250, 3 come after the merge
			for j := 0; j < 15; j++ {
				f := ":150"
				if j > 0 {
					f = ":" + strconv.Itoa(featureB+j-1)
				}
				im.commit("refs/heads/feature/b", featureB+j, who(j+2), at(2000+j), "Feature B, step "+strconv.Itoa(j+1), f, "",
					fileChange{"docs/feature-b.md", "step " + strconv.Itoa(j+1) + "\n"})
			}
		case 50:
			im.b.WriteString("reset refs/tags/v0.1.0\nfrom :50\n")
		case 200:
			im.b.WriteString("tag v1.0.0\nfrom :200\ntagger " + AuthorName + " <" + AuthorEmail + "> " + strconv.FormatInt(at(200).Unix(), 10) + " +0000\n")
			im.data("Version 1.0.0")
		}
	}
	for j := 15; j < 18; j++ {
		im.commit("refs/heads/feature/b", featureB+j, who(j+2), at(2000+j), "Feature B, step "+strconv.Itoa(j+1), ":"+strconv.Itoa(featureB+j-1), "",
			fileChange{"docs/feature-b.md", "step " + strconv.Itoa(j+1) + "\n"})
	}
	im.run(origin)
	origin.Git("symbolic-ref", "HEAD", "refs/heads/main")

	r := &Repo{Env: e, Dir: dir}
	e.Git(filepath.Dir(dir), "clone", "-q", origin.Dir, dir)
	e.Trust(dir)
	r.persistIdentity()
	origin.unsetHooksPath()
	for _, b := range []string{"feature/a", "feature/b"} {
		r.Git("branch", "--track", b, "origin/"+b)
	}
	// The origin gets 2 more commits on main; the clone fetches them and then makes 1 commit of its own: main is 1 ahead and 2 behind.
	var more importer
	more.commit("refs/heads/main", 1, who(1), at(301), "Update the loader on the server, revision 301", "refs/heads/main^0", "",
		fileChange{"src/loader.go", LoaderSource(301)})
	more.commit("refs/heads/main", 2, who(2), at(302), "Update the loader on the server, revision 302", ":1", "",
		fileChange{"src/loader.go", LoaderSource(302)})
	more.run(origin)
	r.Git("fetch", "-q", "origin")
	r.Write("src/loader.go", LoaderSource(303))
	e.SkipDates(303 - e.ticksSoFar())
	r.Commit("Update the loader here, revision 303")
	wt := dir + ".worktrees"
	if err := os.MkdirAll(wt, 0o755); err != nil {
		e.t.Fatalf("gittest: %v", err)
	}
	r.Git("worktree", "add", "-q", filepath.Join(wt, "feature"), "feature/a")
	return r
}

func (e *Env) fixtureMany(dir string, n int) *Repo {
	r := e.InitAt(dir)
	r.persistIdentity()
	var im importer
	files := []fileChange{{"README.md", "# Many files\n"}, {"src/loader.go", LoaderSource(0)}}
	for i := 0; i < n; i++ {
		files = append(files, fileChange{manyPath(i), "line " + strconv.Itoa(i) + "\n"})
	}
	im.commit("refs/heads/main", 0, fixtureAuthors[0], baseDate, "Add the files", "", "", files...)
	im.run(r)
	r.Git("reset", "-q", "--hard")
	for i := 0; i < n; i++ {
		if err := os.WriteFile(r.Path(manyPath(i)), []byte("changed "+strconv.Itoa(i)+"\n"), 0o644); err != nil {
			e.t.Fatalf("gittest: %v", err)
		}
	}
	return r
}

func manyPath(i int) string {
	return fmt.Sprintf("gen/d%03d/f%05d.txt", i/1000, i)
}

// Manifest returns a hash of everything that makes a fixture what it is: the references and their objects of the repository
// (and of its origin, when dir + ".origin.git" exists), the index, the status, and the content and mode of every file of the
// work tree. Two runs of a fixture give the same manifest.
func Manifest(t TB, dir string) string {
	t.Helper()
	e := NewEnv(t)
	h := sha256.New()
	section := func(name, body string) { _, _ = fmt.Fprintf(h, "== %s\n%s\n", name, body) }
	section("refs", e.Git(dir, "for-each-ref", "--format=%(refname) %(objectname) %(*objectname)"))
	section("head", e.Git(dir, "rev-parse", "HEAD"))
	section("index", e.Git(dir, "ls-files", "-s"))
	out, _, err := e.GitErr(dir, "status", "--porcelain=v1", "-uall")
	if err != nil {
		t.Fatalf("gittest: git status in %s: %v", dir, err)
	}
	section("status", out)
	if _, err := os.Stat(dir + ".origin.git"); err == nil {
		section("origin", e.Git(dir+".origin.git", "for-each-ref", "--format=%(refname) %(objectname) %(*objectname)"))
	}
	var paths []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		t.Fatalf("gittest: %v", err)
	}
	sort.Strings(paths)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("gittest: %v", err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("gittest: %v", err)
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(dir, p)
		_, _ = fmt.Fprintf(h, "%s %o %s\n", rel, info.Mode().Perm(), hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// LongHistory builds a repository in dir with a branch main of n commits, as the Graph's tests need it: every 50th commit
// merges a branch of two commits made 10 commits before, so lanes open and close all along the history. A commit changes one
// small file, so building 100,000 of them takes a few seconds ("git fast-import"). The commits are at one minute from each other.
func (e *Env) LongHistory(dir string, n int) *Repo {
	r := e.InitAt(dir)
	r.persistIdentity()
	var im importer
	at := func(i int) time.Time { return baseDate.Add(time.Duration(i) * time.Minute) }
	side := n + 10 // the marks of the side branches come after those of main
	for i := 1; i <= n; i++ {
		from, merge := "", ""
		if i > 1 {
			from = ":" + strconv.Itoa(i-1)
		}
		file := fileChange{"data/f" + strconv.Itoa(i%97) + ".txt", "revision " + strconv.Itoa(i) + "\n"}
		message := "Change the data, revision " + strconv.Itoa(i)
		if i%50 == 0 && i > 20 {
			// two side commits made on top of the commit 10 before, merged here
			base := ":" + strconv.Itoa(i-10)
			for j := 0; j < 2; j++ {
				f := base
				if j > 0 {
					f = ":" + strconv.Itoa(side-1)
				}
				im.commit("refs/heads/side", side, fixtureAuthors[(j+1)%len(fixtureAuthors)], at(i-10+j), "Side work "+strconv.Itoa(j+1)+" of "+strconv.Itoa(i), f, "",
					fileChange{"data/side" + strconv.Itoa(i) + ".txt", "side " + strconv.Itoa(j) + "\n"})
				side++
			}
			merge, message = ":"+strconv.Itoa(side-1), "Merge side/"+strconv.Itoa(i)
		}
		im.commit("refs/heads/main", i, fixtureAuthors[i%len(fixtureAuthors)], at(i), message, from, merge, file)
	}
	im.run(r)
	r.Git("symbolic-ref", "HEAD", "refs/heads/main")
	if n > 20 {
		r.Git("branch", "-q", "-D", "side")
	}
	r.Git("reset", "-q", "--hard")
	return r
}
