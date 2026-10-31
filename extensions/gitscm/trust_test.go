package gitscm

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/gitscm/trust"
)

// untrustedRepo makes a repository that the harness did not trust, so that the extension meets the question.
func untrustedRepo(t *testing.T, e *gittest.Env, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	e.Git(dir, "init", "-q", "-b", "main")
	e.Git(dir, "config", "core.hooksPath", e.HooksDir)
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// recorder is a fake git that records every call it gets, and fails every command but --version.
type recorder struct {
	path  string
	env   string
	calls string
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()
	calls := filepath.Join(t.TempDir(), "calls.jsonl")
	path, env := gittest.FakeGit(t, "record "+calls+"\nversion 2.43.0\nstderr \"fatal: not a git repository\\n\"\nexit 128")
	return &recorder{path: path, env: env, calls: calls}
}

// recorded reads the calls so far.
func (rc *recorder) recorded(t *testing.T) []gittest.FakeCall {
	t.Helper()
	f, err := os.Open(rc.calls)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []gittest.FakeCall
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var c gittest.FakeCall
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// inRepo lists the calls that have the repository as the working directory, as -C, as --git-dir or as GIT_DIR or GIT_WORK_TREE.
func inRepo(calls []gittest.FakeCall, root string) []gittest.FakeCall {
	var out []gittest.FakeCall
	for _, c := range calls {
		hit := git.Within(c.Dir, root)
		for i, a := range c.Args {
			if (a == "-C" || a == "--git-dir" || a == "--work-tree") && i+1 < len(c.Args) && git.Within(c.Args[i+1], root) {
				hit = true
			}
			if strings.HasPrefix(a, "--git-dir=") || strings.HasPrefix(a, "--work-tree=") {
				hit = hit || strings.Contains(a, root)
			}
		}
		for _, kv := range c.Env {
			if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") {
				hit = hit || strings.Contains(kv, root)
			}
		}
		if hit {
			out = append(out, c)
		}
	}
	return out
}

func TestNoGitProcessInARepositoryBeforeTheAnswer(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := untrustedRepo(t, e, filepath.Join(e.Work, "repo"))
	rc := newRecorder(t)
	r := newRig(t, e, rigOptions{folder: repo, gitPath: rc.path, extraEnv: []string{rc.env}})
	r.settle()
	if n := len(inRepo(rc.recorded(t), repo)); n != 0 {
		t.Fatalf("%d git processes in the repository before the answer: %+v", n, inRepo(rc.recorded(t), repo))
	}
	// the question is asked, and says what it is about
	dialogs := r.api.OpenDialogs()
	if len(dialogs) != 1 {
		t.Fatalf("%d dialogs", len(dialogs))
	}
	spec := dialogs[0].Spec()
	if spec.Message != "Do you trust the authors of the Git repository in "+repo+"?" || !strings.Contains(spec.Detail, "Git can run programs that a repository names") {
		t.Errorf("dialog = %+v", spec)
	}
	var labels []string
	for _, b := range spec.Buttons {
		labels = append(labels, b.Label)
	}
	if strings.Join(labels, "|") != "Trust Repository|Trust All Repositories in work|Don't Trust" || !spec.Buttons[2].Cancel {
		t.Errorf("buttons = %v", labels)
	}
	// the repository has a source control with no groups, no status items, and the view is told it waits
	src := r.source(repo)
	if len(src.Groups()) != 0 || len(src.StatusBarCommands()) != 0 || src.Count() != -1 {
		t.Errorf("the untrusted source control has groups %d, items %d, count %d", len(src.Groups()), len(src.StatusBarCommands()), src.Count())
	}
	for key, want := range map[string]any{"git.untrustedRepositoryCount": 1, "gitOpenRepositoryCount": 0, "git.state": "initialized"} {
		if got := r.api.ContextKey(key); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	// the answer Don't Trust: still nothing runs, and the answer is not kept
	dialogs[0].Cancel()
	r.api.Pump()
	r.api.Pump()
	if n := len(inRepo(rc.recorded(t), repo)); n != 0 {
		t.Errorf("%d git processes in the repository after Don't Trust", n)
	}
	if !r.x.trust.Declined(repo) || r.x.trust.IsTrusted(repo) {
		t.Error("Don't Trust is not remembered for the session, or trusted the repository")
	}
	if again, _ := trust.Open(trust.Path(filepath.Join(e.StateHome, "emetos"))); again.IsTrusted(repo) || again.Declined(repo) {
		t.Error("Don't Trust is in the file")
	}
	// a scan again does not ask again
	r.x.model.scan()
	r.settle()
	if len(r.api.OpenDialogs()) != 0 {
		t.Error("the question came back in the same session")
	}
	if r.api.ContextKey("git.untrustedRepositoryCount") != 1 {
		t.Error("the repository is not counted as untrusted")
	}
}

func TestTrustRepositoryStartsGitInIt(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := untrustedRepo(t, e, filepath.Join(e.Work, "repo"))
	rc := newRecorder(t)
	r := newRig(t, e, rigOptions{folder: repo, gitPath: rc.path, extraEnv: []string{rc.env}})
	r.settle()
	r.api.OpenDialogs()[0].Press("Trust Repository")
	r.api.WaitFor("git to be started in the repository", func() bool { r.api.Pump(); return len(inRepo(rc.recorded(t), repo)) > 0 })
	first := inRepo(rc.recorded(t), repo)[0]
	if first.Dir != repo || first.Args[len(first.Args)-1] != "--is-shallow-repository" && first.Args[0] != "rev-parse" {
		t.Errorf("the first call after Trust: %+v", first)
	}
	// the answer is in the file after a restart
	if again, err := trust.Open(trust.Path(filepath.Join(e.StateHome, "emetos"))); err != nil || !again.IsTrusted(repo) {
		t.Errorf("Trust is not in the file: %v", err)
	}
	if _, err := os.Stat(e.TrustFile()); err != nil {
		t.Error(err)
	}
}

func TestTrustAllRepositoriesInTheParentCoversTheSiblings(t *testing.T) {
	e := gittest.NewEnv(t)
	parent := filepath.Join(e.Work, "projects")
	a := untrustedRepo(t, e, filepath.Join(parent, "a"))
	b := untrustedRepo(t, e, filepath.Join(parent, "b"))
	sibling := untrustedRepo(t, e, filepath.Join(e.Work, "projects2", "c"))
	_ = sibling
	r := newRig(t, e, rigOptions{folder: parent, settings: map[string]any{"git.repositoryScanMaxDepth": 1}})
	r.settle()
	// two repositories wait in one parent: the question is about the parent folder first
	dialogs := r.api.OpenDialogs()
	if len(dialogs) != 1 || !strings.Contains(dialogs[0].Spec().Message, "2 Git repositories in "+parent) {
		t.Fatalf("dialogs = %+v", dialogs)
	}
	dialogs[0].Press("Trust All Repositories in projects")
	r.api.WaitFor("both repositories to open", func() bool {
		r.api.Pump()
		return r.x.model.byRoot(a) != nil && r.x.model.byRoot(a).repo != nil && r.x.model.byRoot(b).repo != nil
	})
	if !r.x.trust.IsTrusted(a) || !r.x.trust.IsTrusted(b) || r.x.trust.IsTrusted(filepath.Join(e.Work, "projects2", "c")) {
		t.Error("the parent folder covers the wrong repositories")
	}
	if got := r.x.trust.Entries(); len(got) != 1 || got[0].Kind != trust.Parent || got[0].Path != parent {
		t.Errorf("entries = %+v", got)
	}
	if len(r.api.OpenDialogs()) != 0 {
		t.Error("another question was asked")
	}
}

func TestAskForEachRepositoryAsksOneAtATime(t *testing.T) {
	e := gittest.NewEnv(t)
	parent := filepath.Join(e.Work, "projects")
	a := untrustedRepo(t, e, filepath.Join(parent, "a"))
	b := untrustedRepo(t, e, filepath.Join(parent, "b"))
	r := newRig(t, e, rigOptions{folder: parent})
	r.settle()
	r.api.OpenDialogs()[0].Press("Ask for Each Repository")
	r.api.Pump()
	d := r.api.OpenDialogs()
	if len(d) != 1 || !strings.Contains(d[0].Spec().Message, "in "+a+"?") {
		t.Fatalf("the first question: %+v", d)
	}
	d[0].Press("Trust Repository")
	r.api.WaitFor("the second question", func() bool {
		r.api.Pump()
		d = r.api.OpenDialogs()
		return len(d) == 1 && strings.Contains(d[0].Spec().Message, "in "+b+"?")
	})
	d[0].Press("Don't Trust")
	r.api.Pump()
	if !r.x.trust.IsTrusted(a) || r.x.trust.IsTrusted(b) || !r.x.trust.Declined(b) {
		t.Error("the answers were not kept as given")
	}
}

func TestTheDialogListsTheProgramsTheConfigurationNames(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := untrustedRepo(t, e, filepath.Join(e.Work, "repo"))
	e.Git(repo, "config", "alias.sh", "!echo hi")
	e.Git(repo, "config", "core.sshCommand", "ssh -i /tmp/key")
	r := newRig(t, e, rigOptions{folder: repo})
	r.settle()
	detail := r.api.OpenDialogs()[0].Spec().Detail
	for _, want := range []string{"The configuration of this repository names these programs:", "core.hooksPath = " + programText(e.HooksDir), "alias.sh = !echo hi", "core.sshCommand = ssh -i /tmp/key"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the detail lacks %q:\n%s", want, detail)
		}
	}
}

func TestTheTrustFileThatCannotBeUsedIsReportedAndTheQuestionAsked(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := untrustedRepo(t, e, filepath.Join(e.Work, "repo"))
	file := e.TrustFile()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"version": 9, "trusted": ["`+repo+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, e, rigOptions{folder: repo})
	r.settle()
	if len(r.api.OpenDialogs()) != 1 {
		t.Error("the question was not asked")
	}
	var warned bool
	for _, n := range r.api.Notes() {
		if n.Severity == "warn" && strings.Contains(n.Message, "list of trusted Git repositories") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("no warning: %+v", r.api.Notes())
	}
	// the answer holds for the session and the file is left alone
	r.api.OpenDialogs()[0].Press("Trust Repository")
	r.api.Pump()
	if !r.x.trust.IsTrusted(repo) {
		t.Error("the answer did not hold")
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), `"version": 9`) {
		t.Errorf("the file was overwritten: %s", data)
	}
}

func TestADevTrustFolderTrustsForTheSessionOnly(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := untrustedRepo(t, e, filepath.Join(e.Work, "repo"))
	r := newRig(t, e, rigOptions{folder: repo, devTrust: e.Work})
	r.settle()
	if len(r.api.OpenDialogs()) != 0 {
		t.Error("the question was asked")
	}
	if got := r.letters(repo, groupWorking); len(got) != 1 {
		t.Errorf("the repository is not open: %v", got)
	}
	if _, err := os.Stat(e.TrustFile()); err == nil {
		t.Error("the dev knob wrote the trust file")
	}
}

func TestManageTrustedRepositoriesStopsTheRepositoryAtOnce(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "a\n")
	repo.Commit("Add a")
	repo.Write("a.txt", "b\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	if got := r.letters(repo.Dir, groupWorking); len(got) != 1 {
		t.Fatalf("working = %v", got)
	}
	if err := r.api.Run(CommandManageTrusted, nil); err != nil {
		t.Fatal(err)
	}
	picks := r.api.Picks()
	if len(picks) != 1 || len(picks[0].Labels()) != 1 || !strings.Contains(picks[0].Labels()[0], repo.Dir) {
		t.Fatalf("picks = %+v", picks)
	}
	picks[0].Choose(0)
	r.api.Pump()
	if r.x.trust.IsTrusted(repo.Dir) {
		t.Error("the repository is still trusted")
	}
	rep := r.repo(repo.Dir)
	if rep.trusted || rep.repo != nil {
		t.Error("the repository is still running")
	}
	src := r.source(repo.Dir)
	if len(src.Groups()) != 0 || src.Count() != -1 || len(src.StatusBarCommands()) != 0 {
		t.Errorf("the groups, count and items stayed: %d %d %d", len(src.Groups()), src.Count(), len(src.StatusBarCommands()))
	}
	if r.api.ContextKey("git.untrustedRepositoryCount") != 1 || r.api.ContextKey("gitOpenRepositoryCount") != 0 {
		t.Error("the context keys do not follow")
	}
	// the file no longer has it
	if again, _ := trust.Open(trust.Path(filepath.Join(e.StateHome, "emetos"))); again.IsTrusted(repo.Dir) {
		t.Error("the file still has it")
	}
	// trusting it again from the welcome button's command asks the question again
	if err := r.api.Run(CommandTrustRepository, nil); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if len(r.api.OpenDialogs()) != 1 {
		t.Error("Trust Repository did not ask")
	}
}

func TestSubmodulesAndWorktreesFollowTheirParent(t *testing.T) {
	e := gittest.NewEnv(t)
	main := e.Init("main")
	main.Write("a.txt", "a\n")
	main.Commit("Add a")
	sub := e.Init("sub")
	sub.Write("s.txt", "s\n")
	sub.Commit("Add s")
	main.Submodule("vendor/sub", sub)
	wt := filepath.Join(e.Work, "feature")
	main.Git("worktree", "add", "-q", wt, "-b", "feature")
	main.Write("vendor/sub/new.txt", "n\n")
	// only main is trusted: the submodule and the worktree are not in the file
	data, _ := os.ReadFile(e.TrustFile())
	if strings.Contains(string(data), "vendor") || strings.Contains(string(data), "feature") {
		t.Fatalf("the harness trusted more than the repository: %s", data)
	}
	r := newRig(t, e, rigOptions{folder: main.Dir, settings: map[string]any{"git.detectSubmodules": true}})
	r.settle()
	subRoot := filepath.Join(main.Dir, "vendor", "sub")
	r.api.WaitFor("the submodule to open", func() bool {
		r.api.Pump()
		rep := r.x.model.byRoot(subRoot)
		return rep != nil && rep.repo != nil && !rep.refreshing
	})
	if len(r.api.OpenDialogs()) != 0 {
		t.Error("a question was asked for the submodule")
	}
	if got := r.letters(subRoot, groupWorking); len(got) != 1 || got[0] != "new.txt U" {
		t.Errorf("the submodule's changes: %v", got)
	}
	if src := r.source(subRoot); src.ContextValue() != "submodule" {
		t.Errorf("context value %q", src.ContextValue())
	}
	// the worktree is a repository of a folder that is opened with the open command, and it is trusted with its parent
	if err := r.api.Run("git.openRepository", wt); err != nil {
		t.Fatal(err)
	}
	r.settle()
	r.api.WaitFor("the worktree to open", func() bool {
		r.api.Pump()
		rep := r.x.model.byRoot(wt)
		return rep != nil && rep.repo != nil && !rep.refreshing
	})
	if len(r.api.OpenDialogs()) != 0 {
		t.Error("a question was asked for the worktree")
	}
	if src := r.source(wt); src.ContextValue() != "worktree" {
		t.Errorf("context value %q", src.ContextValue())
	}
}

func TestProgramTextIsOneLineAndCutByCharacters(t *testing.T) {
	long := strings.Repeat("a", maxProgramText-1) + "éb"
	tests := []struct {
		name, value, want string
	}{
		{"short", "!echo hi", "!echo hi"},
		{"spaces and lines are one space", "ssh  -i\n /tmp/key", "ssh -i /tmp/key"},
		{"exactly the limit", strings.Repeat("x", maxProgramText), strings.Repeat("x", maxProgramText)},
		{"a letter of two bytes at the cut is kept whole", long, strings.Repeat("a", maxProgramText-1) + "é..."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := programText(tc.value); got != tc.want {
				t.Errorf("programText(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
