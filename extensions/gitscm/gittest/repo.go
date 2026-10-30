package gittest

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Repo is a repository made by the harness, with helpers that build its history. Every helper fails the test when git fails.
type Repo struct {
	// Env is the world the repository lives in.
	Env *Env
	// Dir is the work tree (or, for a bare repository, the repository itself).
	Dir string
	// Bare says whether the repository has no work tree.
	Bare bool
}

// New makes a repository named "repo" in the world of the test, on the branch main, with the identity Ada Example, and
// trusts it. It skips the test when git is missing or older than 2.30 (Require). The repository has no commit.
func New(t TB) *Repo {
	t.Helper()
	return NewEnv(t).Init("repo")
}

// Init makes an empty repository with the given name in the Work folder of the Env, on the branch main, and trusts it.
func (e *Env) Init(name string) *Repo {
	e.t.Helper()
	return e.InitAt(filepath.Join(e.Work, name))
}

// InitAt makes an empty repository in the folder dir (made when it is missing) on the branch main, and trusts it.
func (e *Env) InitAt(dir string) *Repo {
	e.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatalf("gittest: %v", err)
	}
	e.Git(dir, "init", "-q", "-b", "main")
	e.Git(dir, "config", "core.hooksPath", e.HooksDir) // an empty folder: no hook runs unless Repo.Hook writes one
	e.Trust(dir)
	return &Repo{Env: e, Dir: dir}
}

// InitBare makes an empty bare repository with the given name in the Work folder of the Env. It is the remote of the
// tests: pushes, fetches and clones go to its path. HTTP access (HTTPBackend) is allowed to push to it.
func (e *Env) InitBare(name string) *Repo {
	e.t.Helper()
	return e.InitBareAt(filepath.Join(e.Work, name))
}

// InitBareAt makes an empty bare repository in the folder dir (made when it is missing).
func (e *Env) InitBareAt(dir string) *Repo {
	e.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatalf("gittest: %v", err)
	}
	e.Git(dir, "init", "-q", "--bare", "-b", "main")
	e.Git(dir, "config", "core.hooksPath", e.HooksDir)
	e.Git(dir, "config", "http.receivepack", "true")
	e.Trust(dir)
	return &Repo{Env: e, Dir: dir, Bare: true}
}

// Git runs git in the repository and returns its standard output without the final newline.
func (r *Repo) Git(args ...string) string {
	r.Env.t.Helper()
	return r.Env.Git(r.Dir, args...)
}

// GitErr is Git for a command that may fail.
func (r *Repo) GitErr(args ...string) (stdout, stderr string, err error) {
	r.Env.t.Helper()
	return r.Env.GitErr(r.Dir, args...)
}

// Path returns the absolute path of a file of the work tree.
func (r *Repo) Path(rel string) string { return filepath.Join(r.Dir, filepath.FromSlash(rel)) }

// Write writes a file of the work tree (and the folders above it).
func (r *Repo) Write(rel, content string) {
	r.Env.t.Helper()
	p := r.Path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.Env.t.Fatalf("gittest: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.Env.t.Fatalf("gittest: %v", err)
	}
}

// Read returns the content of a file of the work tree.
func (r *Repo) Read(rel string) string {
	r.Env.t.Helper()
	data, err := os.ReadFile(r.Path(rel))
	if err != nil {
		r.Env.t.Fatalf("gittest: %v", err)
	}
	return string(data)
}

// Remove deletes a file of the work tree (not from the index).
func (r *Repo) Remove(rel string) {
	r.Env.t.Helper()
	if err := os.Remove(r.Path(rel)); err != nil {
		r.Env.t.Fatalf("gittest: %v", err)
	}
}

// commitEnv is the environment of a commit: the world's, with the date of the next commit.
func (r *Repo) commitEnv() []string {
	d := r.Env.NextDate().Format("2006-01-02T15:04:05-0700")
	return mergeVars(r.Env.Vars(), []string{"GIT_AUTHOR_DATE=" + d, "GIT_COMMITTER_DATE=" + d})
}

// CommitAs commits what is staged with the given author, at the next date, and returns the hash of the new commit.
func (r *Repo) CommitAs(name, email, message string, args ...string) string {
	r.Env.t.Helper()
	env := mergeVars(r.commitEnv(), []string{"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email})
	a := append([]string{"commit", "-q", "-m", message}, args...)
	if _, errOut, err := r.Env.run(r.Dir, nil, env, a...); err != nil {
		r.Env.t.Fatalf("gittest: git commit in %s: %v\n%s", r.Dir, err, errOut)
	}
	return r.Head()
}

// Commit stages the given paths (everything, when there are none) and commits them as Ada Example at the next date of the
// world. It returns the hash of the new commit.
func (r *Repo) Commit(message string, paths ...string) string {
	r.Env.t.Helper()
	if len(paths) == 0 {
		r.Git("add", "-A")
	} else {
		r.Git(append([]string{"add", "--"}, paths...)...)
	}
	return r.CommitAs(AuthorName, AuthorEmail, message)
}

// CommitEmpty makes a commit with no change.
func (r *Repo) CommitEmpty(message string) string {
	r.Env.t.Helper()
	return r.CommitAs(AuthorName, AuthorEmail, message, "--allow-empty")
}

// Head returns the hash HEAD names.
func (r *Repo) Head() string {
	r.Env.t.Helper()
	return r.Git("rev-parse", "HEAD")
}

// Branch makes a branch at the given revision (HEAD when empty), without switching to it.
func (r *Repo) Branch(name, from string) {
	r.Env.t.Helper()
	args := []string{"branch", name}
	if from != "" {
		args = append(args, from)
	}
	r.Git(args...)
}

// Checkout switches to a branch, tag or commit.
func (r *Repo) Checkout(ref string) {
	r.Env.t.Helper()
	r.Git("checkout", "-q", ref)
}

// Merge merges a branch into the current one with a merge commit (--no-ff) at the next date. It returns git's error when
// the merge stops on a conflict, which leaves the repository in the middle of the merge.
func (r *Repo) Merge(branch string) error {
	r.Env.t.Helper()
	_, _, err := r.Env.run(r.Dir, nil, r.commitEnv(), "merge", "--no-ff", "-m", "Merge "+branch, branch)
	return err
}

// Tag makes a tag at HEAD: annotated when message is not empty, lightweight otherwise.
func (r *Repo) Tag(name, message string) {
	r.Env.t.Helper()
	if message == "" {
		r.Git("tag", name)
		return
	}
	if _, errOut, err := r.Env.run(r.Dir, nil, r.commitEnv(), "tag", "-a", "-m", message, name); err != nil {
		r.Env.t.Fatalf("gittest: git tag in %s: %v\n%s", r.Dir, err, errOut)
	}
}

// Stash stashes the changes of the work tree (with the untracked files when untracked is true) under the message.
func (r *Repo) Stash(message string, untracked bool) {
	r.Env.t.Helper()
	args := []string{"stash", "push", "-m", message}
	if untracked {
		args = append(args, "-u")
	}
	if _, errOut, err := r.Env.run(r.Dir, nil, r.commitEnv(), args...); err != nil {
		r.Env.t.Fatalf("gittest: git stash in %s: %v\n%s", r.Dir, err, errOut)
	}
}

// Hook writes an executable hook (name is "pre-commit", "commit-msg" and so on) and points core.hooksPath of the
// repository at a folder of its own that holds it. The script must start with a "#!" line.
func (r *Repo) Hook(name, script string) {
	r.Env.t.Helper()
	dir := filepath.Join(r.Env.Root, r.Env.UniqueName("hooks-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.Env.t.Fatalf("gittest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		r.Env.t.Fatalf("gittest: %v", err)
	}
	r.Git("config", "core.hooksPath", dir)
}

// Submodule adds the repository sub as a submodule at path and commits it. The URL of the submodule is the path of sub.
func (r *Repo) Submodule(path string, sub *Repo) {
	r.Env.t.Helper()
	r.Git("submodule", "add", "-q", sub.Dir, path)
	r.CommitAs(AuthorName, AuthorEmail, "Add submodule "+path)
}

// AddRemote makes a bare repository named name in the world, adds it to the repository as the remote name, and returns it.
// Nothing is pushed.
func (r *Repo) AddRemote(name string) *Repo {
	r.Env.t.Helper()
	bare := r.Env.InitBare(name + ".git")
	r.Git("remote", "add", name, bare.Dir)
	return bare
}

// Clone clones the repository (usually a bare remote) into a new folder named name of the world and returns the clone.
func (r *Repo) Clone(name string) *Repo {
	r.Env.t.Helper()
	dir := filepath.Join(r.Env.Work, name)
	r.Env.Git(r.Env.Work, "clone", "-q", r.Dir, dir)
	r.Env.Git(dir, "config", "core.hooksPath", r.Env.HooksDir)
	r.Env.Trust(dir)
	return &Repo{Env: r.Env, Dir: dir}
}

// Files returns the paths of the tracked files, sorted, as "git ls-files" prints them.
func (r *Repo) Files() []string {
	r.Env.t.Helper()
	out := r.Git("ls-files")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// ConflictingBranches prepares a merge that conflicts in src/loader.go: it commits the base (src/loader.go, 40 lines,
// when the repository has no commit yet), makes the branches "ours" and "theirs" from it, each with a commit that changes
// lines 12 and 20 of src/loader.go in its own way, and leaves "ours" checked out. Merging "theirs" stops with a conflict.
// It returns the names of the two branches.
func (r *Repo) ConflictingBranches() (ours, theirs string) {
	r.Env.t.Helper()
	if _, _, err := r.GitErr("rev-parse", "-q", "--verify", "HEAD"); err != nil {
		r.Write("src/loader.go", LoaderSource(0))
		r.Commit("Add the loader")
	}
	base := r.Git("rev-parse", "HEAD")
	r.Branch("theirs", base)
	r.Branch("ours", base)
	r.Checkout("theirs")
	r.Write("src/loader.go", editLines(r.Read("src/loader.go"), map[int]string{12: "\tretries := 5 // theirs", 20: "\treturn load(path, retries), nil // theirs"}))
	r.Commit("Retry five times")
	r.Checkout("ours")
	r.Write("src/loader.go", editLines(r.Read("src/loader.go"), map[int]string{12: "\tretries := 3 // ours", 20: "\treturn load(path, retries), err // ours"}))
	r.Commit("Retry three times")
	return "ours", "theirs"
}

// editLines replaces the numbered lines (counted from 1) of text.
func editLines(text string, edits map[int]string) string {
	lines := strings.Split(text, "\n")
	for n, line := range edits {
		lines[n-1] = line
	}
	return strings.Join(lines, "\n")
}

// LoaderSource returns the 40 lines of src/loader.go of the fixtures for revision rev: the same file for the same rev, with
// a comment on line 3 that tells the revision, so that every revision differs from the one before.
func LoaderSource(rev int) string {
	var b strings.Builder
	b.WriteString("// Package loader reads the files of a project.\n")
	b.WriteString("package loader\n")
	b.WriteString("// revision " + strconv.Itoa(rev) + "\n")
	b.WriteString("\nimport (\n\t\"os\"\n\t\"path/filepath\"\n)\n\n")
	b.WriteString("// Load reads the file at path and returns its content.\n") // line 11
	b.WriteString("func Load(path string) ([]byte, error) {\n")                // line 12
	b.WriteString("\tretries := 1\n")
	b.WriteString("\tclean := filepath.Clean(path)\n")
	b.WriteString("\tdata, err := os.ReadFile(clean)\n")
	b.WriteString("\t// A failed read is reported as it is.\n")
	b.WriteString("\tif err != nil {\n")
	b.WriteString("\t\treturn nil, err\n")
	b.WriteString("\t}\n")
	b.WriteString("\t_ = retries\n")
	b.WriteString("\treturn data, nil\n")
	b.WriteString("}\n\n")
	b.WriteString("// load is the helper of Load.\n")
	b.WriteString("func load(path string, retries int) []byte {\n")
	b.WriteString("\tfor i := 0; i < retries; i++ {\n")
	b.WriteString("\t\tdata, err := os.ReadFile(path)\n")
	b.WriteString("\t\tif err == nil {\n")
	b.WriteString("\t\t\treturn data\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
	b.WriteString("// Exists says whether path names a file.\n")
	b.WriteString("// It follows symbolic links.\n")
	b.WriteString("//\n")
	b.WriteString("func Exists(path string) bool {\n")
	b.WriteString("\t_, err := os.Stat(path)\n")
	b.WriteString("\treturn err == nil\n")
	b.WriteString("}\n")
	return b.String()
}
