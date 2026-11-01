package gitscm

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The worktrees against real repositories (step 24.19): each command runs through the registry of the fake API, and the test looks at what
// git says afterwards.

// menuArg is the argument a menu of the Source Control view gives a command for a repository.
func menuArg(r *repository) []any { return []any{sourceArg(r)} }

// wtWorld is a repository "app" with two commits, a tag v1 and the branches topic and other, the extension open on it.
type wtWorld struct {
	t    *testing.T
	env  *gittest.Env
	repo *gittest.Repo
	r    *rig
	rp   *repository
	// root is where the first worktree of the repository goes: <parent>/app.worktrees
	root string
}

func newWtWorld(t *testing.T, o rigOptions) *wtWorld {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := e.Init("app")
	repo.Write("a.txt", "one\n")
	repo.Write(".gitignore", "node_modules\n.env\ncache/\n*.secret\n")
	repo.Write("src/b.txt", "b\n")
	repo.Commit("first commit")
	repo.Write("a.txt", "one\ntwo\n")
	repo.Commit("second commit")
	repo.Git("tag", "v1")
	repo.Branch("topic", "")
	repo.Branch("other", "")
	if o.folder == "" {
		o.folder = repo.Dir
	}
	w := &wtWorld{t: t, env: e, repo: repo, root: filepath.Join(filepath.Dir(repo.Dir), "app.worktrees")}
	w.r = newRig(t, e, o)
	w.r.settle()
	w.rp = w.r.repo(repo.Dir)
	return w
}

// worktreeList is "git worktree list --porcelain" as the paths and branches ("path branch").
func (w *wtWorld) worktreeList() []string {
	var out []string
	for _, wt := range git.ParseWorktrees([]byte(w.repo.Git("worktree", "list", "--porcelain") + "\n")) {
		out = append(out, wt.Path+" "+wt.Branch())
	}
	return out
}

// pickIndex is the index of the item of the pick whose label has the text.
func pickIndex(t *testing.T, p *fakeapi.Pick, text string) int {
	t.Helper()
	for i, l := range p.Labels() {
		if strings.Contains(l, text) {
			return i
		}
	}
	t.Fatalf("no item has %q: %q", text, p.Labels())
	return -1
}

// startCreate runs git.createWorktree for the repository and returns the picker of the start.
func (w *wtWorld) startCreate() *fakeapi.Pick {
	w.t.Helper()
	return w.r.startPick("git.createWorktree", menuArg(w.rp))
}

// create makes a worktree for the branch or tag in the picker, accepting the default path, and waits for it.
func (w *wtWorld) create(label, path string) {
	w.t.Helper()
	before := len(w.r.api.Inputs())
	p := w.startCreate()
	p.Choose(pickIndex(w.t, p, label))
	in := w.r.nextInput(before)
	if path == "" {
		path = in.Options().Value
	}
	in.Accept(path)
	w.r.idle(w.rp)
}

func TestCreateWorktreeFromABranchAsksWhereAndMakesIt(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	p := w.startCreate()
	if got := p.Options().Placeholder; got != "Select a branch or tag to create the new worktree from" {
		t.Errorf("placeholder %q", got)
	}
	labels := p.Labels()
	if labels[0] != "$(plus) Create new branch..." || !p.Options().Items[1].Separator || p.Options().Items[1].Label != "" {
		t.Errorf("the first rows are %q", labels)
	}
	for _, want := range []string{"$(git-branch) main", "$(git-branch) topic", "$(git-branch) other", "$(tag) v1", "branches", "tags"} {
		if !slices.ContainsFunc(labels, func(l string) bool { return l == want }) {
			t.Errorf("no row %q in %q", want, labels)
		}
	}
	p.Choose(pickIndex(t, p, "topic"))
	in := w.r.nextInput(0)
	want := filepath.Join(w.root, "topic")
	o := in.Options()
	if o.Value != want || o.Prompt != "Worktree path" || o.Message != "Please provide a worktree path" || !o.IgnoreFocusOut {
		t.Errorf("the box is %+v, want the value %q", o, want)
	}
	if len(o.Buttons) != 1 || o.Buttons[0].Icon != "folder" || o.Buttons[0].Tooltip != "Select Worktree Destination" {
		t.Errorf("buttons %+v", o.Buttons)
	}
	in.Accept(want)
	w.r.idle(w.rp)
	if got := w.worktreeList(); !slices.Equal(got, []string{w.repo.Dir + " main", want + " topic"}) {
		t.Errorf("worktrees %q", got)
	}
	if _, err := os.Stat(filepath.Join(want, "a.txt")); err != nil {
		t.Error(err)
	}
	note := w.r.nextNote(0)
	if note.Severity != "info" || !strings.Contains(note.Message, `The worktree "topic" was created at "`+want+`". Open the worktree?`) ||
		strings.Join(note.Actions, ",") != "Open Worktree in Current Window,Open Worktree in New Window" {
		t.Errorf("notification %+v", note)
	}
	// the default root was used, so nothing is remembered
	if roots := w.rp.m.x.state.get().WorktreeRoots; len(roots) != 0 {
		t.Errorf("remembered %v", roots)
	}
	// the worktree is not a repository of the view: git.detectWorktrees is off
	if w.r.x.model.byRoot(want) != nil {
		t.Error("the new worktree was opened as a repository without being asked")
	}
}

func TestCreateWorktreeFromATagATrackedBranchAndANewBranch(t *testing.T) {
	tests := []struct {
		name  string
		setup func(w *wtWorld)
		label string
		// folder is the name of the folder of the new worktree; branch the branch it has checked out ("" for a detached HEAD)
		folder, branch string
		commit         string // a rev of the repository the work tree is at
	}{
		{"a tag, detached", func(*wtWorld) {}, "v1", "v1", "", "v1^{commit}"},
		{"a remote branch, detached, in a folder without the slash", func(w *wtWorld) {
			w.repo.AddRemote("origin")
			w.repo.Git("push", "-q", "origin", "topic")
			w.repo.Git("fetch", "-q", "origin")
		}, "origin/topic", "origin-topic", "", "origin/topic"},
		{"a branch with a slash", func(w *wtWorld) { w.repo.Branch("feature/login", "") }, "feature/login", "feature-login", "feature/login", "feature/login"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWtWorld(t, rigOptions{})
			tc.setup(w)
			w.r.refresh(w.rp)
			w.create(tc.label, "")
			dir := filepath.Join(w.root, tc.folder)
			if got := strings.TrimSpace(w.repo.Git("-C", dir, "branch", "--show-current")); got != tc.branch {
				t.Errorf("branch %q, want %q", got, tc.branch)
			}
			if got, want := strings.TrimSpace(w.repo.Git("-C", dir, "rev-parse", "HEAD")), strings.TrimSpace(w.repo.Git("rev-parse", tc.commit)); got != want {
				t.Errorf("HEAD %s, want %s", got, want)
			}
		})
	}
}

func TestCreateWorktreeOnANewBranch(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		typed    string
		folder   string
		branch   string
	}{
		{"a plain name", nil, "login", "login", "login"},
		{"a name with a slash", nil, "feature/login", "feature-login", "feature/login"},
		{"the branch prefix is left out of the folder", map[string]any{"git.branchPrefix": "dev/"}, "dev/login", "login", "dev/login"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWtWorld(t, rigOptions{settings: tc.settings})
			p := w.startCreate()
			p.Choose(0)
			name := w.r.nextInput(0)
			if prefix, _ := tc.settings["git.branchPrefix"].(string); name.Options().Value != prefix || name.Options().Prompt != "Branch name" {
				t.Errorf("the branch box is %+v", name.Options())
			}
			name.Accept(tc.typed)
			in := w.r.nextInput(1)
			want := filepath.Join(w.root, tc.folder)
			if in.Options().Value != want {
				t.Errorf("path %q, want %q", in.Options().Value, want)
			}
			in.Accept(want)
			w.r.idle(w.rp)
			if got := strings.TrimSpace(w.repo.Git("-C", want, "branch", "--show-current")); got != tc.branch {
				t.Errorf("branch %q, want %q", got, tc.branch)
			}
			if got, want := w.repo.Git("-C", want, "rev-parse", "HEAD"), w.repo.Git("rev-parse", "HEAD"); got != want {
				t.Errorf("HEAD %s, want %s", got, want)
			}
			// the new branch tracks nothing
			if out, _, err := w.repo.GitErr("-C", want, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil {
				t.Errorf("the branch tracks %q", out)
			}
		})
	}
}

func TestCreateWorktreeWithABranchThatIsCheckedOutHere(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	p := w.startCreate()
	p.Choose(pickIndex(t, p, "main"))
	d := w.r.theOnlyDialog()
	if d.Spec().Message != `Branch "main" is already checked out in the current repository.` || strings.Join(buttons(d), ",") != "Create New Branch,Cancel" {
		t.Fatalf("dialog %+v", d.Spec())
	}
	d.Press("Create New Branch")
	w.r.api.Pump()
	name := w.r.nextInput(0)
	name.Accept("from-main")
	in := w.r.nextInput(1)
	if want := filepath.Join(w.root, "from-main"); in.Options().Value != want {
		t.Errorf("path %q", in.Options().Value)
	}
	in.Accept(in.Options().Value)
	w.r.idle(w.rp)
	if got := w.worktreeList(); len(got) != 2 || !strings.HasSuffix(got[1], " from-main") {
		t.Errorf("worktrees %q", got)
	}
	t.Run("Cancel makes nothing", func(t *testing.T) {
		w := newWtWorld(t, rigOptions{})
		p := w.startCreate()
		p.Choose(pickIndex(t, p, "main"))
		w.r.theOnlyDialog().Cancel()
		w.r.api.Pump()
		if len(w.r.api.Inputs()) != 0 || len(w.worktreeList()) != 1 {
			t.Error("a worktree was made")
		}
	})
}

func TestCreateWorktreeWithABranchThatAnotherWorktreeHas(t *testing.T) {
	for _, tc := range []struct {
		button  string
		newTab  bool
		restore bool
	}{{"Open Worktree in Current Window", false, false}, {"Open Worktree in New Window", true, false}} {
		t.Run(tc.button, func(t *testing.T) {
			w := newWtWorld(t, rigOptions{})
			other := filepath.Join(w.root, "elsewhere")
			w.repo.Git("worktree", "add", "-q", other, "topic")
			p := w.startCreate()
			p.Choose(pickIndex(t, p, "topic"))
			d := w.r.theOnlyDialog()
			if want := `Branch "topic" is already checked out in the worktree at "` + other + `".`; d.Spec().Message != want ||
				strings.Join(buttons(d), ",") != "Open Worktree in Current Window,Open Worktree in New Window,Cancel" {
				t.Fatalf("dialog %+v", d.Spec())
			}
			d.Press(tc.button)
			w.r.api.Pump()
			if got := w.r.api.OpenedFolders(); len(got) != 1 || got[0] != (fakeapi.OpenedFolder{Path: other, NewTab: tc.newTab}) {
				t.Errorf("opened %v", got)
			}
			// the worktree is a repository of the view now
			if r := w.r.x.model.byRoot(other); r == nil || r.kind() != git.KindWorktree {
				t.Errorf("repository %v", r)
			}
			if len(w.r.api.Inputs()) != 0 {
				t.Error("a path was asked for")
			}
		})
	}
}

func TestCreateWorktreeWhereAWorktreeIsGetsANumberedFolder(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	taken := filepath.Join(w.root, "topic")
	w.repo.Git("worktree", "add", "-q", "--detach", taken, "v1")
	w.repo.Git("worktree", "add", "-q", "--detach", taken+"-1", "v1")
	w.create("$(git-branch) other", taken)
	want := taken + "-2"
	if got := w.worktreeList(); !slices.Contains(got, want+" other") {
		t.Errorf("worktrees %q, want %s", got, want)
	}
	found := false
	for _, n := range w.r.api.Notes() {
		if strings.Contains(n.Message, `A worktree already exists at "`+taken+`". The new worktree was made at "`+want+`".`) {
			found = true
		}
	}
	if !found {
		t.Errorf("notifications %q", w.r.noteTexts())
	}
}

func TestCreateWorktreeOutsideTheParentRemembersTheFolder(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	elsewhere := filepath.Join(w.env.Root, "somewhere", "else")
	w.create("$(git-branch) topic", filepath.Join(elsewhere, "mine"))
	if got := w.worktreeList(); !slices.Contains(got, filepath.Join(elsewhere, "mine")+" topic") {
		t.Errorf("worktrees %q", got)
	}
	if roots := w.rp.m.x.state.get().WorktreeRoots; roots[w.repo.Dir] != elsewhere {
		t.Errorf("remembered %v", roots)
	}
	state, err := os.ReadFile(filepath.Join(w.env.StateHome, "emetos", stateFile))
	if err != nil || !strings.Contains(string(state), `"worktreeRoots"`) {
		t.Errorf("state file %q, %v", state, err)
	}
	// the next one starts there, with the folder chosen by the name
	p := w.startCreate()
	p.Choose(pickIndex(t, p, "other"))
	in := w.r.nextInput(1)
	if want := filepath.Join(elsewhere, "other"); in.Options().Value != want {
		t.Errorf("path %q, want %q", in.Options().Value, want)
	}
	in.Cancel()
}

func TestTheBrowseButtonOfTheWorktreePathChoosesTheFolder(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	p := w.startCreate()
	p.Choose(pickIndex(t, p, "topic"))
	in := w.r.nextInput(0)
	start := in.Value()
	chosen := filepath.Join(w.env.Root, "chosen")
	w.r.api.AnswerFileChooser(chosen, nil)
	in.PressButton(0)
	w.r.api.WaitFor("the folder to be put in the box", func() bool { w.r.api.Pump(); return in.Value() != start })
	if want := filepath.Join(chosen, "topic"); in.Value() != want {
		t.Errorf("the box has %q, want %q", in.Value(), want)
	}
	calls := w.r.api.FileChooserCalls()
	if len(calls) != 1 || calls[0].Options.Folder != filepath.Dir(w.repo.Dir) || !calls[0].Options.Directory ||
		calls[0].Options.AcceptLabel != "Select as Worktree Destination" || calls[0].Options.Title != "Select Worktree Destination" {
		t.Errorf("chooser calls %+v", calls)
	}
	// a dialog that is closed leaves the text as it is (VS Code empties the box)
	w.r.api.AnswerFileChooser("", nil)
	in.PressButton(0)
	w.r.api.WaitFor("the second dialog", func() bool { w.r.api.Pump(); return len(w.r.api.FileChooserCalls()) == 2 })
	w.r.api.Pump()
	if want := filepath.Join(chosen, "topic"); in.Value() != want {
		t.Errorf("after a closed dialog the box has %q", in.Value())
	}
	in.Accept(in.Value())
	w.r.idle(w.rp)
	if got := w.worktreeList(); !slices.Contains(got, filepath.Join(chosen, "topic")+" topic") {
		t.Errorf("worktrees %q", got)
	}
}
