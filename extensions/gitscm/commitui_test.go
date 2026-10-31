package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The commit box and the action button (step 24.10): their states in tables of state, text, enablement and command.

func inputOf(r *rig, rp *repository) *fakeapi.FakeInput {
	return r.source(rp.root).Input().(*fakeapi.FakeInput)
}

func TestTheBoxOfARepository(t *testing.T) {
	r, repo, rp := commitRig(t, rigOptions{})
	src := r.source(rp.root)
	in := inputOf(r, rp)
	if got := in.Placeholder(); got != `Message ({0} to commit on "main")` {
		t.Errorf("placeholder = %q", got)
	}
	if !in.Visible() || !in.Enabled() {
		t.Errorf("visible %v enabled %v", in.Visible(), in.Enabled())
	}
	acc := src.AcceptInputCommand()
	if acc.ID != "git.commit" || acc.Title != "Commit" {
		t.Errorf("accept command %+v", acc)
	}
	args := extension.SCMArgs(acc.Args)
	if len(args) != 1 || args[0].SourceControl != rp.handle || !args[0].IsSourceControl() {
		t.Errorf("accept arguments %+v", acc.Args)
	}
	// the placeholder follows HEAD
	tests := []struct {
		name  string
		setup func()
		want  string
	}{
		{"another branch", func() { repo.Git("checkout", "-q", "-b", "topic") }, `Message ({0} to commit on "topic")`},
		{"a tag", func() { repo.Git("tag", "v1"); repo.Git("checkout", "-q", "--detach", "v1") }, `Message ({0} to commit on "v1")`},
		{"a detached commit", func() { repo.Git("tag", "-d", "v1"); repo.Git("checkout", "-q", "--detach", "HEAD") }, `Message ({0} to commit on "` + repo.Head()[:8] + `")`},
	}
	for _, tc := range tests {
		tc.setup()
		r.refresh(rp)
		if got := in.Placeholder(); got != tc.want {
			t.Errorf("%s: placeholder = %q, want %q", tc.name, got, tc.want)
		}
	}
	t.Run("git.showCommitInput hides the box", func(t *testing.T) {
		r.api.SetSetting("git.showCommitInput", false)
		if in.Visible() {
			t.Error("the box is shown")
		}
		r.api.SetSetting("git.showCommitInput", true)
		if !in.Visible() {
			t.Error("the box is hidden")
		}
	})
}

func TestTheCheckOfTheMessage(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	tests := []struct {
		name     string
		settings map[string]any
		text     string
		want     string
	}{
		{"off by default", nil, long(200), ""},
		{"a short subject", map[string]any{"git.inputValidation": true}, long(50), ""},
		{"a subject one over", map[string]any{"git.inputValidation": true}, long(51), "1 character over 50 in current line"},
		{"a subject many over", map[string]any{"git.inputValidation": true}, long(60), "10 characters over 50 in current line"},
		{"a body line at the limit", map[string]any{"git.inputValidation": true}, "Subject\n\n" + long(72), ""},
		{"a body line one over", map[string]any{"git.inputValidation": true}, "Subject\n\n" + long(73), "1 character over 72 in current line"},
		{"a body line many over", map[string]any{"git.inputValidation": true}, "Subject\n\n" + long(80), "8 characters over 72 in current line"},
		{"the subject length can be null", map[string]any{"git.inputValidation": true, "git.inputValidationSubjectLength": nil}, long(72), ""},
		{"a null subject length inherits the length", map[string]any{"git.inputValidation": true, "git.inputValidationSubjectLength": nil, "git.inputValidationLength": 20}, long(21), "1 character over 20 in current line"},
		{"another subject length", map[string]any{"git.inputValidation": true, "git.inputValidationSubjectLength": 10}, long(11), "1 character over 10 in current line"},
		{"white space only", map[string]any{"git.inputValidation": true}, "  \n ", "Current commit message only contains whitespace characters"},
		{"an empty box is fine", map[string]any{"git.inputValidation": true}, "", ""},
		{"windows line breaks are not counted", map[string]any{"git.inputValidation": true}, long(50) + "\r\nbody", ""},
		{"characters of two units count twice", map[string]any{"git.inputValidation": true, "git.inputValidationSubjectLength": 10}, strings.Repeat("😀", 6), "2 characters over 10 in current line"},
		{"characters of one unit count once", map[string]any{"git.inputValidation": true, "git.inputValidationSubjectLength": 10}, strings.Repeat("é", 10), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _, rp := commitRig(t, rigOptions{settings: tc.settings})
			got := ""
			level := extension.ValidationWarning
			if v := inputOf(r, rp).Validate(tc.text); v != nil {
				got, level = v.Message, v.Level
			}
			if got != tc.want || (got != "" && level != extension.ValidationWarning) {
				t.Errorf("message %q level %v, want %q warning", got, level, tc.want)
			}
		})
	}
	t.Run("a setting that changes while the view is open", func(t *testing.T) {
		r, _, rp := commitRig(t, rigOptions{})
		in := inputOf(r, rp)
		if v := in.Validate(long(80)); v != nil {
			t.Fatalf("%+v", v)
		}
		r.api.SetSetting("git.inputValidation", true)
		if v := in.Validate(long(80)); v == nil {
			t.Error("no message after the setting was turned on")
		}
	})
	t.Run("a repository with too many changes", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.statusLimit": 1, "git.inputValidation": true}})
		for i := 0; i < 3; i++ {
			repo.Write("many"+string(rune('0'+i))+".txt", "x\n")
		}
		r.refresh(rp)
		v := inputOf(r, rp).Validate("fine")
		if v == nil || v.Message != "Too many changes were detected. Only the first 1 changes will be shown below." || v.Level != extension.ValidationWarning {
			t.Errorf("message %+v", v)
		}
	})
}

// buttonCase builds a repository in some state and says what the button shows.
type buttonCase struct {
	name      string
	settings  map[string]any
	setup     func(e *gittest.Env, repo *gittest.Repo)
	title     string
	tooltip   string
	cmd       string
	enabled   bool
	secondary []string // the titles of the commands of the dropdown, the groups apart with "|"
	none      bool     // no button at all
}

// state makes a repository with a commit; a has the commit's file.
func buttonRepo(e *gittest.Env) *gittest.Repo {
	r := e.Init("button")
	r.Write("a.txt", "one\n")
	r.Commit("first")
	return r
}

// withRemote gives the repository a remote it tracks, with n commits more locally and m more on the remote.
func withRemote(ahead, behind int) func(e *gittest.Env, repo *gittest.Repo) {
	return func(e *gittest.Env, repo *gittest.Repo) {
		bare := repo.AddRemote("origin")
		repo.Git("push", "-q", "-u", "origin", "main")
		for i := 0; i < behind; i++ {
			other := bare.Clone("other" + string(rune('a'+i)))
			other.Write("remote"+string(rune('a'+i))+".txt", "r\n")
			other.Commit("remote change")
			other.Git("push", "-q", "origin", "main")
		}
		if behind > 0 {
			repo.Git("fetch", "-q", "origin")
		}
		for i := 0; i < ahead; i++ {
			repo.Write("local"+string(rune('a'+i))+".txt", "l\n")
			repo.Commit("local change")
		}
	}
}

func TestTheActionButton(t *testing.T) {
	commitGroups := []string{"$(check) Commit", "$(check) Commit (Amend)", "$(arrow-up) Commit & Push", "$(sync) Commit & Sync"}
	staged := func(e *gittest.Env, repo *gittest.Repo) { repo.Write("a.txt", "two\n"); repo.Git("add", "a.txt") }
	unstaged := func(e *gittest.Env, repo *gittest.Repo) { repo.Write("a.txt", "two\n") }
	tests := []buttonCase{
		{name: "staged changes", setup: staged, title: "$(check) Commit", tooltip: `Commit Changes on "main"`, cmd: "git.commit", enabled: true, secondary: commitGroups},
		{name: "unstaged changes, with the suggestion of smart commit", setup: unstaged, title: "$(check) Commit", tooltip: `Commit Changes on "main"`, cmd: "git.commit", enabled: true, secondary: commitGroups},
		{name: "unstaged changes, with the suggestion off", settings: map[string]any{"git.suggestSmartCommit": false},
			setup: func(e *gittest.Env, repo *gittest.Repo) { unstaged(e, repo); withRemote(0, 0)(e, repo) },
			title: "$(check) Commit", cmd: "git.commit", tooltip: `Commit Changes on "main"`, enabled: false, secondary: commitGroups},
		{name: "unstaged changes, with smart commit", settings: map[string]any{"git.enableSmartCommit": true}, setup: unstaged,
			title: "$(check) Commit", tooltip: `Commit Changes on "main"`, cmd: "git.commit", enabled: true, secondary: commitGroups},
		{name: "only untracked files, with smart commit for tracked files", settings: map[string]any{"git.enableSmartCommit": true, "git.smartCommitChanges": "tracked"},
			setup: func(e *gittest.Env, repo *gittest.Repo) { repo.Write("new.txt", "n\n"); withRemote(0, 0)(e, repo) },
			title: "$(check) Commit", tooltip: `Commit Changes on "main"`, cmd: "git.commit", enabled: false, secondary: commitGroups},
		{name: "nothing to commit, no remote", setup: func(*gittest.Env, *gittest.Repo) {},
			title: "$(cloud-upload) Publish Branch", tooltip: `Publish Branch "main"`, cmd: "git.publish", enabled: true},
		{name: "nothing to commit, in step with the remote", setup: withRemote(0, 0),
			title: "$(check) Commit", tooltip: `Commit Changes on "main"`, cmd: "git.commit", enabled: false, secondary: commitGroups},
		{name: "ahead of the remote", setup: withRemote(2, 0), title: "$(sync) Sync Changes 2$(arrow-up)", cmd: "git.sync", enabled: true, tooltip: "Push 2 commits to origin/main"},
		{name: "behind the remote", setup: withRemote(0, 1), title: "$(sync) Sync Changes 1$(arrow-down)", cmd: "git.sync", enabled: true, tooltip: "Pull 1 commits from origin/main"},
		{name: "ahead and behind", setup: withRemote(2, 1), title: "$(sync) Sync Changes 1$(arrow-down) 2$(arrow-up)", cmd: "git.sync", enabled: true, tooltip: "Pull 1 and push 2 commits between origin/main"},
		{name: "changes beat the sync", setup: func(e *gittest.Env, repo *gittest.Repo) { withRemote(1, 0)(e, repo); staged(e, repo) },
			title: "$(check) Commit", cmd: "git.commit", enabled: true, tooltip: `Commit Changes on "main"`, secondary: commitGroups},
		{name: "the commit button is hidden, the sync one shows", settings: map[string]any{"git.showActionButton": map[string]any{"commit": false}},
			setup: func(e *gittest.Env, repo *gittest.Repo) { withRemote(1, 0)(e, repo); staged(e, repo) },
			title: "$(sync) Sync Changes 1$(arrow-up)", cmd: "git.sync", enabled: true, tooltip: "Push 1 commits to origin/main"},
		{name: "every button is hidden", settings: map[string]any{"git.showActionButton": map[string]any{"commit": false, "publish": false, "sync": false}}, setup: staged, none: true},
		{name: "the publish button is hidden", settings: map[string]any{"git.showActionButton": map[string]any{"publish": false}}, setup: func(*gittest.Env, *gittest.Repo) {},
			title: "$(check) Commit", cmd: "git.commit", enabled: false, tooltip: `Commit Changes on "main"`, secondary: commitGroups},
		{name: "the sync button is hidden", settings: map[string]any{"git.showActionButton": map[string]any{"sync": false}}, setup: withRemote(1, 0),
			title: "$(check) Commit", cmd: "git.commit", enabled: false, tooltip: `Commit Changes on "main"`, secondary: commitGroups},
		{name: "a detached HEAD", setup: func(e *gittest.Env, repo *gittest.Repo) {
			staged(e, repo)
			repo.Git("checkout", "-q", "--detach", "HEAD")
		},
			title: "$(check) Commit", tooltip: "Commit Changes", cmd: "git.commit", enabled: true},
		{name: "a tag", setup: func(e *gittest.Env, repo *gittest.Repo) {
			staged(e, repo)
			repo.Git("tag", "v1")
			repo.Git("checkout", "-q", "--detach", "v1")
		},
			title: "$(check) Commit", tooltip: "Commit Changes", cmd: "git.commit", enabled: true},
		{name: "a protected branch asks every time", settings: map[string]any{"git.branchProtection": []any{"main"}}, setup: staged,
			title: "$(lock) Commit", tooltip: `Commit Changes on "main"`, cmd: "git.commit", enabled: true,
			secondary: []string{"$(lock) Commit", "$(lock) Commit (Amend)", "$(lock) Commit & Push", "$(lock) Commit & Sync"}},
		{name: "a protected branch always goes to a new one", settings: map[string]any{"git.branchProtection": []any{"main"}, "git.branchProtectionPrompt": "alwaysCommitToNewBranch"}, setup: staged,
			title: "$(git-branch) Commit", tooltip: "Commit Changes to New Branch", cmd: "git.commit", enabled: true,
			secondary: []string{"$(git-branch) Commit", "$(git-branch) Commit (Amend)", "$(git-branch) Commit & Push", "$(git-branch) Commit & Sync"}},
		{name: "a protected branch that is committed to anyway", settings: map[string]any{"git.branchProtection": []any{"main"}, "git.branchProtectionPrompt": "alwaysCommit"}, setup: staged,
			title: "$(check) Commit", tooltip: `Commit Changes on "main"`, cmd: "git.commit", enabled: true, secondary: commitGroups},
		{name: "the setting names push", settings: map[string]any{"git.postCommitCommand": "push"}, setup: staged,
			title: "$(arrow-up) Commit & Push", tooltip: "Commit & Push Changes", cmd: "git.commit", enabled: true, secondary: commitGroups},
		{name: "the setting names sync", settings: map[string]any{"git.postCommitCommand": "sync"}, setup: staged,
			title: "$(sync) Commit & Sync", tooltip: "Commit & Sync Changes", cmd: "git.commit", enabled: true, secondary: commitGroups},
		{name: "the setting names nothing", settings: map[string]any{"git.postCommitCommand": "none"}, setup: staged,
			title: "$(check) Commit", tooltip: `Commit Changes on "main"`, cmd: "git.commit", enabled: true, secondary: commitGroups},
		{name: "a merge to finish", setup: func(e *gittest.Env, repo *gittest.Repo) {
			repo.Git("checkout", "-q", "-b", "other")
			repo.Write("b.txt", "other\n")
			repo.Commit("on other")
			repo.Git("checkout", "-q", "main")
			repo.Write("c.txt", "main\n")
			repo.Commit("on main")
			repo.Git("merge", "--no-commit", "--no-ff", "other")
		}, title: "$(check) Continue", tooltip: "Continue Merge", cmd: "git.commit", enabled: true},
		{name: "a merge with conflicts", setup: func(e *gittest.Env, repo *gittest.Repo) {
			repo.Git("checkout", "-q", "-b", "other")
			repo.Write("a.txt", "other\n")
			repo.Commit("on other")
			repo.Git("checkout", "-q", "main")
			repo.Write("a.txt", "main\n")
			repo.Commit("on main")
			_ = repo.Merge("other")
		}, title: "$(check) Continue", tooltip: "Continue Merge", cmd: "git.commit", enabled: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := buttonRepo(e)
			tc.setup(e, repo)
			r, _, rp := commitRigOf(t, e, repo, rigOptions{settings: tc.settings})
			b := r.source(rp.root).ActionButton()
			if tc.none {
				if b != nil {
					t.Errorf("button %+v", b)
				}
				return
			}
			if b == nil {
				t.Fatal("no button")
			}
			if tc.cmd == "" {
				// only the title matters here
				if b.Command.Title != tc.title {
					t.Errorf("title %q, want %q", b.Command.Title, tc.title)
				}
				return
			}
			if b.Command.Title != tc.title || b.Command.ID != tc.cmd || b.Enabled != tc.enabled {
				t.Errorf("button %q (%s) enabled %v, want %q (%s) enabled %v", b.Command.Title, b.Command.ID, b.Enabled, tc.title, tc.cmd, tc.enabled)
			}
			if tc.tooltip != "" && b.Command.Tooltip != tc.tooltip {
				t.Errorf("tooltip %q, want %q", b.Command.Tooltip, tc.tooltip)
			}
			var got []string
			for _, g := range b.Secondary {
				for _, c := range g {
					got = append(got, c.Title)
				}
			}
			if !reflect.DeepEqual(got, tc.secondary) {
				t.Errorf("dropdown %v, want %v", got, tc.secondary)
			}
			// the commands carry the repository, and a commit from the dropdown says what follows it
			for _, c := range append([]extension.Command{b.Command}, flatten(b.Secondary)...) {
				args := extension.SCMArgs(c.Args)
				if len(args) == 0 || args[0].SourceControl != rp.handle {
					t.Errorf("%q has arguments %+v", c.Title, c.Args)
				}
			}
		})
	}
}

func flatten(groups [][]extension.Command) []extension.Command {
	var out []extension.Command
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func TestTheDropdownOfTheCommitButton(t *testing.T) {
	r, _, rp := commitRig(t, rigOptions{})
	b := r.source(rp.root).ActionButton()
	if len(b.Secondary) != 2 || len(b.Secondary[0]) != 2 || len(b.Secondary[1]) != 2 {
		t.Fatalf("groups %+v", b.Secondary)
	}
	want := [][]struct{ id, post string }{
		{{"git.commit", "<none>"}, {"git.commitAmend", "<none>"}},
		{{"git.commit", "git.push"}, {"git.commit", "git.sync"}},
	}
	for i, g := range b.Secondary {
		for j, c := range g {
			list := c.Args.([]any)
			post := "<none>"
			if s, ok := list[1].(string); ok {
				post = s
			}
			if c.ID != want[i][j].id || post != want[i][j].post {
				t.Errorf("group %d command %d is %s %v, want %v", i, j, c.ID, post, want[i][j])
			}
		}
	}
	// the arguments tell git.commit what follows, and git.commitAmend ignores them
	for _, tc := range []struct {
		args any
		want postCommitSpec
	}{
		{nil, postCommitSpec{}},
		{[]any{sourceArg(rp)}, postCommitSpec{}},
		{[]any{sourceArg(rp), nil}, postCommitSpec{set: true}},
		{[]any{sourceArg(rp), "git.push"}, postCommitSpec{set: true, command: "git.push"}},
		{[]any{sourceArg(rp), "git.sync"}, postCommitSpec{set: true, command: "git.sync"}},
		{[]any{sourceArg(rp), "git.pull"}, postCommitSpec{}},
		{[]any{sourceArg(rp), 3.0}, postCommitSpec{}},
		{"text", postCommitSpec{}},
	} {
		if got := postCommitArg(tc.args); got != tc.want {
			t.Errorf("postCommitArg(%v) = %+v, want %+v", tc.args, got, tc.want)
		}
	}
}

func TestThePostCommitCommand(t *testing.T) {
	// the real push and sync come with step 24.15: the test puts handlers that record the call in their place
	record := func(r *rig) *[]string {
		var calls []string
		for _, id := range []string{"git.push", "git.sync"} {
			id := id
			r.x.handlers[id] = func(_ context.Context, args any) error {
				a := extension.SCMArgs(args)
				if len(a) != 1 || a[0].SourceControl == "" {
					calls = append(calls, id+" with "+"bad arguments")
					return nil
				}
				calls = append(calls, id)
				return nil
			}
		}
		return &calls
	}
	tests := []struct {
		name     string
		settings map[string]any
		args     func(rp *repository) any
		calls    []string
		primary  string // the title of the button after the commit
	}{
		{"none", nil, func(rp *repository) any { return []any{sourceArg(rp)} }, nil, ""},
		{"the button says push", nil, func(rp *repository) any { return []any{sourceArg(rp), "git.push"} }, []string{"git.push"}, ""},
		{"the button says sync", nil, func(rp *repository) any { return []any{sourceArg(rp), "git.sync"} }, []string{"git.sync"}, ""},
		{"the button says none", map[string]any{"git.postCommitCommand": "push"}, func(rp *repository) any { return []any{sourceArg(rp), nil} }, nil, ""},
		{"the setting says push", map[string]any{"git.postCommitCommand": "push"}, func(rp *repository) any { return []any{sourceArg(rp)} }, []string{"git.push"}, ""},
		{"the setting says sync", map[string]any{"git.postCommitCommand": "sync"}, func(rp *repository) any { return []any{sourceArg(rp)} }, []string{"git.sync"}, ""},
		{"the choice is remembered", map[string]any{"git.rememberPostCommitCommand": true}, func(rp *repository) any { return []any{sourceArg(rp), "git.push"} },
			[]string{"git.push"}, "$(arrow-up) Commit & Push"},
		{"the choice is forgotten", nil, func(rp *repository) any { return []any{sourceArg(rp), "git.push"} }, []string{"git.push"}, "$(check) Commit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := commitRig(t, rigOptions{settings: tc.settings})
			calls := record(r)
			// the button is shown before the commit, so the choice is the one the test makes
			repo.Write("c.txt", "c\n")
			r.refresh(rp)
			r.typeMessage(rp, "With a follow-up")
			r.commitBoxWith(rp, tc.args(rp))
			if count(repo) != "2" {
				t.Fatalf("%s commits; notes %+v", count(repo), r.api.Notes())
			}
			if !reflect.DeepEqual(*calls, tc.calls) {
				t.Errorf("calls %v, want %v", *calls, tc.calls)
			}
			if tc.primary != "" {
				// something to commit again, so that the button is the commit one
				repo.Write("c.txt", "d\n")
				repo.Git("add", "c.txt")
				r.refresh(rp)
				if b := r.source(rp.root).ActionButton(); b == nil || b.Command.Title != tc.primary {
					t.Errorf("button %+v, want %q", b, tc.primary)
				}
			}
		})
	}
	t.Run("a failed commit runs nothing", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{})
		calls := record(r)
		repo.Hook("pre-commit", "#!/bin/sh\nexit 1\n")
		r.typeMessage(rp, "x")
		r.commitBoxWith(rp, []any{sourceArg(rp), "git.push"})
		if len(*calls) != 0 || count(repo) != "1" {
			t.Errorf("calls %v, %s commits", *calls, count(repo))
		}
	})
}

// commitBoxWith runs git.commit with arguments.
func (r *rig) commitBoxWith(rp *repository, args any) {
	r.t.Helper()
	r.run(rp, "git.commit", args)
}

func TestTheTemplateOfTheBox(t *testing.T) {
	t.Run("the box starts with the commit template", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		repo.Write("tpl.txt", "Subject\n# a comment\n\nBody\n")
		repo.Git("config", "commit.template", "tpl.txt")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
		if got := r.message(rp); got != "Subject\n\nBody" {
			t.Fatalf("the box holds %q", got)
		}
		// a commit with the template as it is makes the commit, and the box has the template again
		r.commitBox(rp)
		if count(repo) != "2" || strings.TrimSpace(repo.Git("log", "-1", "--format=%B")) != "Subject\n\nBody" {
			t.Errorf("%s commits, %q", count(repo), repo.Git("log", "-1", "--format=%B"))
		}
		if got := r.message(rp); got != "Subject\n\nBody" {
			t.Errorf("after the commit the box holds %q", got)
		}
		// Restore Commit Template
		r.typeMessage(rp, "something else")
		r.run(rp, "git.restoreCommitTemplate", []any{sourceArg(rp)})
		r.api.WaitFor("the template", func() bool { return r.message(rp) == "Subject\n\nBody" })
	})
	t.Run("a text of the user is not replaced", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		repo.Write("tpl.txt", "Template\n")
		repo.Git("config", "commit.template", "tpl.txt")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
		r.typeMessage(rp, "My own words")
		repo.Git("checkout", "-q", "-b", "topic")
		repo.Write("tpl.txt", "New template\n")
		r.refresh(rp)
		if got := r.message(rp); got != "My own words" {
			t.Errorf("the box holds %q", got)
		}
	})
	t.Run("the message of a merge", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		repo.Git("add", "-A")
		repo.Commit("everything")
		repo.Git("checkout", "-q", "-b", "other")
		repo.Write("o.txt", "o\n")
		repo.Commit("on other")
		repo.Git("checkout", "-q", "main")
		repo.Write("m.txt", "m\n")
		repo.Commit("on main")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
		if got := r.message(rp); got != "" {
			t.Fatalf("the box holds %q", got)
		}
		repo.Git("merge", "--no-commit", "--no-ff", "other")
		r.refresh(rp)
		if got := r.message(rp); got != "Merge branch 'other'" {
			t.Fatalf("the box holds %q", got)
		}
		// Continue finishes the merge with that message
		r.commitBox(rp)
		if got := repo.Git("log", "-1", "--format=%s|%P"); !strings.HasPrefix(got, "Merge branch 'other'|") || len(strings.Fields(strings.SplitN(got, "|", 2)[1])) != 2 {
			t.Errorf("last commit %q", got)
		}
		if got := r.message(rp); got != "" {
			t.Errorf("after the merge the box holds %q", got)
		}
	})
	t.Run("the stash message leaves the template alone", func(t *testing.T) {
		tests := []struct {
			name  string
			typed string // what the box holds; "" is the template
			want  string
		}{
			{"the template is not a message", "", ""},
			{"the text of the user is", "Work on the loader", "Work on the loader"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				e := gittest.NewEnv(t)
				repo := commitRepo(t, e)
				repo.Write("tpl.txt", "Template\n")
				repo.Git("config", "commit.template", "tpl.txt")
				r, _, rp := commitRigOf(t, e, repo, rigOptions{settings: map[string]any{"git.useCommitInputAsStashMessage": true}})
				if r.message(rp) != "Template" {
					t.Fatalf("the box holds %q", r.message(rp))
				}
				if tc.typed != "" {
					r.typeMessage(rp, tc.typed)
				}
				if err := r.api.Run("git.stash", nil); err != nil {
					t.Fatal(err)
				}
				if got := r.api.LastInput().Options().Value; got != tc.want {
					t.Errorf("the stash box starts with %q, want %q", got, tc.want)
				}
			})
		}
	})
}

func TestUndoCommit(t *testing.T) {
	t.Run("the message comes back and the changes stay staged", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{})
		first := repo.Head()
		r.typeMessage(rp, "Fix the cache key\n\nWith a body.")
		r.commitBox(rp)
		if r.message(rp) != "" {
			t.Fatal("the box was not cleared")
		}
		r.run(rp, "git.undoCommit", []any{sourceArg(rp)})
		if repo.Head() != first {
			t.Errorf("HEAD is %s, want %s", repo.Head(), first)
		}
		if got := r.message(rp); got != "Fix the cache key\n\nWith a body." {
			t.Errorf("the box holds %q", got)
		}
		if got := strings.Join(porcelain(repo), "|"); got != " M b.txt|?? u.txt|M  a.txt" {
			t.Errorf("status %q", got)
		}
	})
	t.Run("the first commit goes and its files are untracked", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("first")
		repo.Write("a.txt", "a\n")
		repo.Commit("The one commit")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
		r.run(rp, "git.undoCommit", []any{sourceArg(rp)})
		if _, _, err := repo.GitErr("rev-parse", "--verify", "-q", "HEAD"); err == nil {
			t.Error("HEAD still names a commit")
		}
		if got := r.message(rp); got != "The one commit" {
			t.Errorf("the box holds %q", got)
		}
		if got := strings.Join(porcelain(repo), "|"); got != "?? a.txt" {
			t.Errorf("status %q", got)
		}
	})
	t.Run("a repository with no commit", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("empty")
		r, _, rp := commitRigOf(t, e, repo, rigOptions{})
		r.run(rp, "git.undoCommit", []any{sourceArg(rp)})
		if n := r.lastNote(); n.Severity != "warn" || n.Message != "Can't undo because HEAD doesn't point to any commit." {
			t.Errorf("note %+v", n)
		}
	})
	t.Run("a merge commit asks", func(t *testing.T) {
		for _, answer := range []string{"Undo merge commit", "Cancel"} {
			e := gittest.NewEnv(t)
			repo := e.Init("merge")
			repo.Write("a.txt", "a\n")
			repo.Commit("base")
			repo.Git("checkout", "-q", "-b", "other")
			repo.Write("o.txt", "o\n")
			repo.Commit("on other")
			repo.Git("checkout", "-q", "main")
			repo.Write("m.txt", "m\n")
			repo.Commit("on main")
			if err := repo.Merge("other"); err != nil {
				t.Fatal(err)
			}
			merged := repo.Head()
			r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
			r.start(rp, "git.undoCommit", []any{sourceArg(rp)})
			d := r.theOnlyDialog()
			if d.Spec().Message != "The last commit was a merge commit. Are you sure you want to undo it?" {
				t.Errorf("question %q", d.Spec().Message)
			}
			r.answer(answer)
			r.idle(rp)
			if (answer == "Cancel") != (repo.Head() == merged) {
				t.Errorf("%s: HEAD %s, merge %s", answer, repo.Head(), merged)
			}
			if answer == "Cancel" && r.message(rp) != "" {
				t.Errorf("the box holds %q", r.message(rp))
			}
		}
	})
}

func TestContinueARebase(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("rebase")
	ours, theirs := repo.ConflictingBranches()
	_, _, err := repo.GitErr("rebase", theirs)
	if err == nil {
		t.Fatal("the rebase did not stop")
	}
	_ = ours
	r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
	b := r.source(rp.root).ActionButton()
	if b == nil || b.Command.Title != "$(check) Continue" || b.Command.Tooltip != "Continue Rebase" || b.Enabled {
		t.Fatalf("the button while the conflicts are not resolved: %+v", b)
	}
	// resolve and stage, then Continue
	repo.Write("src/loader.go", gittest.LoaderSource(0))
	repo.Git("add", "src/loader.go")
	r.refresh(rp)
	if b := r.source(rp.root).ActionButton(); b == nil || !b.Enabled {
		t.Fatalf("the button after the conflicts are resolved: %+v", b)
	}
	r.run(rp, "git.commit", []any{sourceArg(rp), nil})
	if _, err := os.Stat(filepath.Join(repo.Dir, ".git", "rebase-merge")); err == nil {
		t.Errorf("the rebase is still in progress; notes %+v", r.api.Notes())
	}
	if got := repo.Git("log", "-1", "--format=%s"); got != "Retry three times" {
		t.Errorf("last commit %q", got)
	}
}
