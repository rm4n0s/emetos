package gitscm

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The questions that come before a commit (step 24.10): unsaved files, the diagnostics of the files, a protected branch, the author.

func TestUnsavedFilesBeforeACommit(t *testing.T) {
	// a.txt is staged, b.txt is changed and not staged, outside.txt is not in the repository; each is open with unsaved changes
	files := func(repo *gittest.Repo, outside string) []string {
		return []string{fileURI(repo.Path("a.txt")), fileURI(repo.Path("b.txt")), fileURI(outside)}
	}
	tests := []struct {
		name     string
		mode     string
		nothing  bool     // nothing is staged
		dirty    []string // the files that are open and unsaved, by name
		asks     string   // the question, "" for none
		detail   string
		savedAll []string // what SaveAll was given
	}{
		{name: "always, one staged file", mode: "always", dirty: []string{"a.txt"},
			asks: "The following file has unsaved changes which won't be included in the commit if you proceed: a.txt.", detail: "Would you like to save it before committing?",
			savedAll: []string{"a.txt"}},
		{name: "always with something staged asks only about the staged files", mode: "always", dirty: []string{"a.txt", "b.txt"},
			asks: "The following file has unsaved changes which won't be included in the commit if you proceed: a.txt.", detail: "Would you like to save it before committing?",
			savedAll: []string{"a.txt"}},
		{name: "always with nothing staged asks about every file of the repository", mode: "always", nothing: true, dirty: []string{"a.txt", "b.txt", "outside.txt"},
			asks: "There are 2 unsaved files.", detail: "Would you like to save them before committing?", savedAll: []string{"a.txt", "b.txt"}},
		{name: "staged, with nothing staged, asks about no file", mode: "staged", nothing: true, dirty: []string{"b.txt"}},
		{name: "staged, asks about the staged files only", mode: "staged", dirty: []string{"a.txt", "b.txt"},
			asks: "The following file has unsaved changes which won't be included in the commit if you proceed: a.txt.", detail: "Would you like to save it before committing?",
			savedAll: []string{"a.txt"}},
		{name: "never asks nothing", mode: "never", dirty: []string{"a.txt", "b.txt"}},
		{name: "a file outside the repository is not asked about", mode: "always", dirty: []string{"outside.txt"}},
		{name: "nothing unsaved asks nothing", mode: "always"},
	}
	for _, tc := range tests {
		for _, button := range []string{"Save All & Commit Changes", "Commit Changes", "Cancel"} {
			if tc.asks == "" && button != "Commit Changes" {
				continue
			}
			t.Run(tc.name+" / "+button, func(t *testing.T) {
				e := gittest.NewEnv(t)
				repo := commitRepo(t, e)
				if tc.nothing {
					repo.Git("reset", "-q")
				}
				r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: map[string]any{"git.promptToSaveFilesBeforeCommit": tc.mode, "git.enableSmartCommit": tc.nothing}})
				outside := filepath.Join(e.Root, "outside.txt")
				var dirty []string
				for _, name := range tc.dirty {
					if name == "outside.txt" {
						dirty = append(dirty, fileURI(outside))
					} else {
						dirty = append(dirty, fileURI(repo.Path(name)))
					}
				}
				r.api.SetDirty(dirty...)
				_ = files
				r.typeMessage(rp, "Save first")
				r.start(rp, "git.commit", []any{sourceArg(rp)})
				if tc.asks != "" {
					d := r.theOnlyDialog()
					if d.Spec().Message != tc.asks || d.Spec().Detail != tc.detail || d.Spec().Type != extension.DialogWarning {
						t.Errorf("question %q / %q", d.Spec().Message, d.Spec().Detail)
					}
					if got := strings.Join(buttons(d), ","); got != "Save All & Commit Changes,Commit Changes,Cancel" {
						t.Errorf("buttons %s", got)
					}
					if button == "Save All & Commit Changes" {
						// the save writes the new text of the staged file; the extension stages it again
						repo.Write("a.txt", "one\ntwo\nthree, saved by the commit\n")
					}
					r.answer(button)
				}
				r.idle(rp)
				saved := r.api.SavedAll()
				var got []string
				for _, call := range saved {
					for _, u := range call {
						p, _ := uriPath(u)
						got = append(got, filepath.Base(p))
					}
				}
				switch button {
				case "Save All & Commit Changes":
					if !slices.Equal(got, tc.savedAll) {
						t.Errorf("SaveAll got %v, want %v", got, tc.savedAll)
					}
					if count(repo) != "2" || repo.Git("show", "HEAD:a.txt") != "one\ntwo\nthree, saved by the commit" {
						t.Errorf("%s commits; a.txt in the commit = %q", count(repo), repo.Git("show", "HEAD:a.txt"))
					}
				case "Commit Changes":
					if len(got) != 0 {
						t.Errorf("SaveAll was called with %v", got)
					}
					if count(repo) != "2" {
						t.Errorf("%s commits; notes %+v", count(repo), r.api.Notes())
					}
				case "Cancel":
					if count(repo) != "1" || len(got) != 0 {
						t.Errorf("%s commits, saved %v", count(repo), got)
					}
				}
			})
		}
	}
	t.Run("a document that cannot be saved stops the commit", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{})
		r.api.SetDirty(fileURI(repo.Path("a.txt")))
		r.api.FailSave(os.ErrPermission)
		r.typeMessage(rp, "x")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		r.answer("Save All & Commit Changes")
		r.idle(rp)
		if count(repo) != "1" || r.lastNote().Severity != "error" {
			t.Errorf("%s commits, notes %+v", count(repo), r.api.Notes())
		}
	})
	t.Run("an empty commit asks nothing about files", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		repo.Git("reset", "-q")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: map[string]any{"git.confirmEmptyCommits": false}})
		r.api.SetDirty(fileURI(repo.Path("a.txt")))
		r.typeMessage(rp, "Empty")
		r.run(rp, "git.commitEmpty", []any{sourceArg(rp)})
		if count(repo) != "2" || !r.quiet() {
			t.Errorf("%s commits", count(repo))
		}
	})
}

func TestTheDiagnosticsHookOfTheCommit(t *testing.T) {
	marker := func(sev extension.Severity, source string) extension.Marker {
		return extension.Marker{Severity: sev, Source: source, Message: "problem", StartLine: 1, StartColumn: 1}
	}
	tests := []struct {
		name     string
		settings map[string]any
		markers  map[string][]extension.Marker // by file name
		asks     string
		wantAsk  bool
	}{
		{name: "an error in a staged file", markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityError, "go vet")}},
			wantAsk: true, asks: "The following file has unresolved diagnostics: 'a.txt'."},
		{name: "a warning is below the default", markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityWarning, "go vet")}}},
		{name: "a warning counts when the setting says so", settings: map[string]any{"git.diagnosticsCommitHook.sources": map[string]any{"*": "warning"}},
			markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityWarning, "go vet")}}, wantAsk: true, asks: "The following file has unresolved diagnostics: 'a.txt'."},
		{name: "information counts at information", settings: map[string]any{"git.diagnosticsCommitHook.sources": map[string]any{"*": "information"}},
			markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityInfo, "go vet")}}, wantAsk: true, asks: "The following file has unresolved diagnostics: 'a.txt'."},
		{name: "a hint counts at hint", settings: map[string]any{"git.diagnosticsCommitHook.sources": map[string]any{"*": "hint"}},
			markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityHint, "go vet")}}, wantAsk: true, asks: "The following file has unresolved diagnostics: 'a.txt'."},
		{name: "a hint is below an error", markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityHint, "go vet")}}},
		{name: "a marker with no source is ignored", markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityError, "")}}},
		{name: "a source set to none is ignored", settings: map[string]any{"git.diagnosticsCommitHook.sources": map[string]any{"*": "error", "go vet": "none"}},
			markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityError, "go vet")}}},
		{name: "a source with its own level", settings: map[string]any{"git.diagnosticsCommitHook.sources": map[string]any{"*": "error", "eslint": "warning"}},
			markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityWarning, "eslint")}}, wantAsk: true, asks: "The following file has unresolved diagnostics: 'a.txt'."},
		{name: "the same warning from another source", settings: map[string]any{"git.diagnosticsCommitHook.sources": map[string]any{"*": "error", "eslint": "warning"}},
			markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityWarning, "go vet")}}},
		{name: "an error in a file that is not staged is not looked at", markers: map[string][]extension.Marker{"b.txt": {marker(extension.SeverityError, "go vet")}}},
		{name: "the hook is off", settings: map[string]any{"git.diagnosticsCommitHook.enabled": false},
			markers: map[string][]extension.Marker{"a.txt": {marker(extension.SeverityError, "go vet")}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			settings := map[string]any{"git.diagnosticsCommitHook.enabled": true}
			for k, v := range tc.settings {
				settings[k] = v
			}
			r, repo, rp := commitRig(t, rigOptions{settings: settings})
			for name, ms := range tc.markers {
				if err := r.api.Problems().Set("test", fileURI(repo.Path(name)), ms); err != nil {
					t.Fatal(err)
				}
			}
			r.typeMessage(rp, "With problems")
			r.start(rp, "git.commit", []any{sourceArg(rp)})
			if tc.wantAsk {
				d := r.theOnlyDialog()
				if d.Spec().Message != tc.asks || d.Spec().Detail != "How would you like to proceed?" {
					t.Errorf("question %q / %q", d.Spec().Message, d.Spec().Detail)
				}
				if got := strings.Join(buttons(d), ","); got != "Commit Anyway,View Problems,Cancel" {
					t.Errorf("buttons %s", got)
				}
				// Cancel and View Problems commit nothing; Commit Anyway does
				d.Press("Cancel")
				r.api.Pump()
				r.idle(rp)
				if count(repo) != "1" {
					t.Fatalf("Cancel committed")
				}
				r.start(rp, "git.commit", []any{sourceArg(rp)})
				r.answer("View Problems")
				r.idle(rp)
				if count(repo) != "1" || r.api.ProblemsShown() != 1 {
					t.Fatalf("View Problems: %s commits, shown %d", count(repo), r.api.ProblemsShown())
				}
				r.start(rp, "git.commit", []any{sourceArg(rp)})
				r.answer("Commit Anyway")
				r.idle(rp)
			} else {
				r.idle(rp)
			}
			if count(repo) != "2" || !r.quiet() {
				t.Errorf("%s commits; notes %+v", count(repo), r.api.Notes())
			}
		})
	}
	t.Run("several files", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		repo.Git("add", "-A")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: map[string]any{"git.diagnosticsCommitHook.enabled": true}})
		for _, name := range []string{"a.txt", "b.txt", "u.txt"} {
			_ = r.api.Problems().Set("test", fileURI(repo.Path(name)), []extension.Marker{marker(extension.SeverityError, "x")})
		}
		r.typeMessage(rp, "Many")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		if d := r.theOnlyDialog(); d.Spec().Message != "There are 3 files that have unresolved diagnostics." {
			t.Errorf("question %q", d.Spec().Message)
		}
	})
	t.Run("with nothing staged, commit all looks at every changed file", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		repo.Git("reset", "-q")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: map[string]any{"git.diagnosticsCommitHook.enabled": true}})
		_ = r.api.Problems().Set("test", fileURI(repo.Path("u.txt")), []extension.Marker{marker(extension.SeverityError, "x")})
		r.typeMessage(rp, "All")
		r.start(rp, "git.commitAll", []any{sourceArg(rp)})
		if d := r.theOnlyDialog(); d.Spec().Message != "The following file has unresolved diagnostics: 'u.txt'." {
			t.Errorf("question %q", d.Spec().Message)
		}
	})
	t.Run("a window without a Problems panel commits without a word", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.diagnosticsCommitHook.enabled": true}})
		r.api.SetProblemsUnavailable(true)
		r.typeMessage(rp, "No panel")
		r.run(rp, "git.commit", []any{sourceArg(rp)})
		if count(repo) != "2" || !r.quiet() {
			t.Errorf("%s commits", count(repo))
		}
	})
}

func TestUnresolvedMarkers(t *testing.T) {
	m := func(sev extension.Severity, source string) extension.Marker {
		return extension.Marker{Severity: sev, Source: source, Message: "m"}
	}
	tests := []struct {
		name    string
		markers []extension.Marker
		sources map[string]string
		want    int
	}{
		{"none", nil, map[string]string{"*": "error"}, 0},
		{"two errors", []extension.Marker{m(0, "a"), m(0, "b")}, map[string]string{"*": "error"}, 2},
		{"an error and a warning at error", []extension.Marker{m(0, "a"), m(1, "a")}, map[string]string{"*": "error"}, 1},
		{"an error and a warning at warning", []extension.Marker{m(0, "a"), m(1, "a")}, map[string]string{"*": "warning"}, 2},
		{"no wildcard and a source not listed", []extension.Marker{m(0, "a")}, map[string]string{"b": "error"}, 0},
		{"a listed source at error", []extension.Marker{m(0, "b"), m(1, "b")}, map[string]string{"b": "error"}, 1},
		{"none beats the wildcard", []extension.Marker{m(0, "b")}, map[string]string{"*": "hint", "b": "none"}, 0},
		{"a level the settings do not know", []extension.Marker{m(0, "a")}, map[string]string{"*": "loud"}, 0},
		{"no source", []extension.Marker{m(0, "")}, map[string]string{"*": "hint"}, 0},
	}
	for _, tc := range tests {
		if got := unresolved(tc.markers, tc.sources); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestBranchProtection(t *testing.T) {
	protected := map[string]any{"git.branchProtection": []any{"main", "release/*"}}
	t.Run("the question and Commit Anyway", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: protected})
		r.typeMessage(rp, "On main")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		d := r.theOnlyDialog()
		if d.Spec().Message != "You are trying to commit to a protected branch." || d.Spec().Detail != "How would you like to proceed?" {
			t.Errorf("question %q / %q", d.Spec().Message, d.Spec().Detail)
		}
		if got := strings.Join(buttons(d), ","); got != "Commit to a New Branch,Commit Anyway,Cancel" {
			t.Errorf("buttons %s", got)
		}
		d.Press("Commit Anyway")
		r.api.Pump()
		r.idle(rp)
		if repo.Git("branch", "--show-current") != "main" || count(repo) != "2" {
			t.Errorf("on %s with %s commits", repo.Git("branch", "--show-current"), count(repo))
		}
	})
	t.Run("Cancel", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: protected})
		r.typeMessage(rp, "x")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		r.answer("Cancel")
		r.idle(rp)
		if count(repo) != "1" {
			t.Errorf("%s commits", count(repo))
		}
	})
	t.Run("Commit to a New Branch", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: protected})
		r.typeMessage(rp, "Moved")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		r.answer("Commit to a New Branch")
		in := r.nextInput(0)
		o := in.Options()
		if o.Prompt != "Branch name" || o.Message != "Please provide a new branch name" || !o.IgnoreFocusOut {
			t.Errorf("input box %+v", o)
		}
		// the name is checked as it is typed
		if msg := in.Validate("main"); msg != `Branch "main" already exists` {
			t.Errorf("validation of an existing branch: %q", msg)
		}
		if msg := in.Validate("  "); msg == "" {
			t.Error("a blank name is accepted")
		}
		in.Accept("fix cache key")
		r.idle(rp)
		if got := repo.Git("branch", "--show-current"); got != "fix-cache-key" {
			t.Errorf("on %q", got)
		}
		if repo.Git("log", "-1", "--format=%s") != "Moved" || repo.Git("rev-list", "--count", "main") != "1" {
			t.Errorf("the commit is not on the new branch only: main has %s", repo.Git("rev-list", "--count", "main"))
		}
	})
	t.Run("closing the box of the name commits nothing", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: protected})
		r.typeMessage(rp, "x")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		r.answer("Commit to a New Branch")
		r.nextInput(0).Cancel()
		r.idle(rp)
		if count(repo) != "1" || repo.Git("branch", "--show-current") != "main" {
			t.Errorf("%s commits on %s", count(repo), repo.Git("branch", "--show-current"))
		}
	})
	t.Run("a branch name that git refuses is not made", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.branchProtection": []any{"main"}, "git.branchValidationRegex": "^feat/"}})
		r.typeMessage(rp, "x")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		r.answer("Commit to a New Branch")
		in := r.nextInput(0)
		if msg := in.Validate("fix"); msg != "Branch name needs to match regex: ^feat/" {
			t.Errorf("validation: %q", msg)
		}
		in.Accept("feat/x")
		r.idle(rp)
		if repo.Git("branch", "--show-current") != "feat/x" {
			t.Errorf("on %s", repo.Git("branch", "--show-current"))
		}
	})
	t.Run("the prefix of branch names starts the box", func(t *testing.T) {
		r, _, rp := commitRig(t, rigOptions{settings: map[string]any{"git.branchProtection": []any{"main"}, "git.branchPrefix": "ada/"}})
		r.typeMessage(rp, "x")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		r.answer("Commit to a New Branch")
		if v := r.nextInput(0).Options().Value; v != "ada/" {
			t.Errorf("the box starts with %q", v)
		}
	})
	t.Run("always commit asks nothing", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.branchProtection": []any{"main"}, "git.branchProtectionPrompt": "alwaysCommit"}})
		r.typeMessage(rp, "x")
		r.run(rp, "git.commit", []any{sourceArg(rp)})
		if count(repo) != "2" || !r.quiet() {
			t.Errorf("%s commits", count(repo))
		}
	})
	t.Run("always commit to a new branch asks for the name at once", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.branchProtection": []any{"main"}, "git.branchProtectionPrompt": "alwaysCommitToNewBranch"}})
		r.typeMessage(rp, "x")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		if !r.quiet() {
			t.Fatal("a question was asked")
		}
		r.nextInput(0).Accept("topic")
		r.idle(rp)
		if repo.Git("branch", "--show-current") != "topic" || repo.Git("log", "-1", "--format=%s") != "x" {
			t.Errorf("on %s", repo.Git("branch", "--show-current"))
		}
	})
	t.Run("another branch is not protected", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		repo.Git("checkout", "-q", "-b", "topic")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: protected})
		r.typeMessage(rp, "x")
		r.run(rp, "git.commit", []any{sourceArg(rp)})
		if count(repo) != "2" || !r.quiet() {
			t.Errorf("%s commits", count(repo))
		}
	})
	t.Run("a pattern with a folder", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		repo.Git("checkout", "-q", "-b", "release/1.0")
		r, _, rp := commitRigOf(t, e, repo, rigOptions{settings: protected})
		r.typeMessage(rp, "x")
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		if d := r.theOnlyDialog(); !strings.HasPrefix(d.Spec().Message, "You are trying to commit to a protected branch") {
			t.Errorf("question %q", d.Spec().Message)
		}
	})
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct{ in, ws, want string }{
		{"fix cache key", "-", "fix-cache-key"},
		{"  padded  ", "-", "padded"},
		{"--leading", "-", "leading"},
		{"a..b", "-", "a-b"},
		{"a~b^c:d", "_", "a_b_c_d"},
		{"a.lock", "-", "a-"},
		{"x/", "-", "x-"},
		{"a[b]", "-", "a-b-"},
		{"feat/ok-name", "-", "feat/ok-name"},
		{"", "-", ""},
		{"tab\there", "-", "tab-here"},
		{"ünï cödé", "-", "ünï-cödé"},
	}
	for _, tc := range tests {
		if got := sanitizeBranchName(tc.in, tc.ws); got != tc.want {
			t.Errorf("sanitizeBranchName(%q, %q) = %q, want %q", tc.in, tc.ws, got, tc.want)
		}
	}
}

func TestTheGuidedSettingOfTheAuthor(t *testing.T) {
	// the world has no author: git is told not to guess one, and its global configuration is the empty file of the world
	noAuthor := func(t *testing.T) (*rig, *gittest.Repo, *repository, *gittest.Env) {
		t.Helper()
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		e.Identity = false
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
		return r, repo, rp, e
	}
	global := func(e *gittest.Env) string {
		data, err := os.ReadFile(e.GlobalConfig)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Run("the whole way", func(t *testing.T) {
		r, repo, rp, e := noAuthor(t)
		r.typeMessage(rp, "Who am I")
		r.commitBox(rp)
		if count(repo) != "1" {
			t.Fatal("the commit went through")
		}
		n := r.lastNote()
		if n.Severity != "error" || n.Message != `Make sure you configure your "user.name" and "user.email" in git.` ||
			strings.Join(n.Actions, ",") != "Set Name and E-mail...,Show Git Output" {
			t.Fatalf("note %+v", n)
		}
		if r.message(rp) != "Who am I" {
			t.Errorf("the box holds %q", r.message(rp))
		}
		r.api.RunNoteAction(len(r.api.Notes())-1, "Set Name and E-mail...")
		name := r.nextInput(0)
		if name.Options().Prompt != "Your name" || name.Validate(" ") == "" {
			t.Errorf("name box %+v", name.Options())
		}
		name.Accept("Ada Lovelace")
		email := r.nextInput(1)
		if email.Validate("nobody") == "" || email.Validate("ada@example.org") != "" {
			t.Errorf("e-mail box %+v", email.Options())
		}
		// nothing is changed before the question is answered
		email.Accept("ada@example.org")
		d := r.theOnlyDialog()
		if d.Spec().Message != "Set your name and e-mail for Git?" ||
			d.Spec().Detail != "Emetos will run these commands, which change your global Git configuration:\n\ngit config --global user.name 'Ada Lovelace'\ngit config --global user.email ada@example.org" {
			t.Errorf("question %q / %q", d.Spec().Message, d.Spec().Detail)
		}
		if strings.TrimSpace(global(e)) != "" {
			t.Fatalf("the global configuration changed before the answer: %q", global(e))
		}
		r.answer("Run the Commands")
		r.api.WaitFor("the configuration", func() bool { return strings.Contains(global(e), "ada@example.org") })
		cfg := global(e)
		if !strings.Contains(cfg, "name = Ada Lovelace") || !strings.Contains(cfg, "email = ada@example.org") {
			t.Errorf("the global configuration is %q", cfg)
		}
		r.api.WaitFor("the notification", func() bool { return strings.Contains(r.lastNote().Message, "are set") })
		n = r.lastNote()
		if n.Severity != "info" || strings.Join(n.Actions, ",") != "Commit Again" {
			t.Fatalf("note %+v", n)
		}
		// now the commit goes through, with the new author
		r.api.RunNoteAction(len(r.api.Notes())-1, "Commit Again")
		r.idle(rp)
		if count(repo) != "2" || repo.Git("log", "-1", "--format=%an <%ae>") != "Ada Lovelace <ada@example.org>" {
			t.Errorf("%s commits, author %q; notes %+v", count(repo), repo.Git("log", "-1", "--format=%an <%ae>"), r.api.Notes())
		}
	})
	t.Run("a name with a quote and a dash", func(t *testing.T) {
		r, _, rp, e := noAuthor(t)
		r.typeMessage(rp, "x")
		r.commitBox(rp)
		r.api.RunNoteAction(len(r.api.Notes())-1, "Set Name and E-mail...")
		r.nextInput(0).Accept("-Dan O'Neil")
		r.nextInput(1).Accept("dan@example.org")
		d := r.theOnlyDialog()
		if !strings.Contains(d.Spec().Detail, `git config --global user.name '-Dan O'\''Neil'`) {
			t.Errorf("detail %q", d.Spec().Detail)
		}
		r.answer("Run the Commands")
		r.api.WaitFor("the configuration", func() bool { return strings.Contains(global(e), "dan@example.org") })
		if cfg := global(e); !strings.Contains(cfg, `name = -Dan O'Neil`) {
			t.Errorf("the global configuration is %q", cfg)
		}
	})
	t.Run("only what is missing is asked", func(t *testing.T) {
		r, repo, rp, e := noAuthor(t)
		repo.Git("config", "user.name", "Ada")
		r.typeMessage(rp, "x")
		r.commitBox(rp)
		r.api.RunNoteAction(len(r.api.Notes())-1, "Set Name and E-mail...")
		in := r.nextInput(0)
		if in.Options().Prompt != "Your e-mail address" {
			t.Errorf("the first question is %q", in.Options().Prompt)
		}
		in.Accept("ada@example.org")
		d := r.theOnlyDialog()
		if strings.Contains(d.Spec().Detail, "user.name") || !strings.Contains(d.Spec().Detail, "user.email") {
			t.Errorf("detail %q", d.Spec().Detail)
		}
		d.Press("Cancel")
		r.api.Pump()
		if strings.TrimSpace(global(e)) != "" {
			t.Errorf("the global configuration changed: %q", global(e))
		}
	})
	for _, step := range []string{"the name", "the e-mail", "the confirmation"} {
		t.Run("cancelling at "+step+" changes nothing", func(t *testing.T) {
			r, _, rp, e := noAuthor(t)
			r.typeMessage(rp, "x")
			r.commitBox(rp)
			r.api.RunNoteAction(len(r.api.Notes())-1, "Set Name and E-mail...")
			in := r.nextInput(0)
			if step == "the name" {
				in.Cancel()
			} else {
				in.Accept("Ada")
				in = r.nextInput(1)
				if step == "the e-mail" {
					in.Cancel()
				} else {
					in.Accept("ada@example.org")
					r.theOnlyDialog().Cancel()
					r.api.Pump()
				}
			}
			if strings.TrimSpace(global(e)) != "" {
				t.Errorf("the global configuration changed: %q", global(e))
			}
			if len(r.api.Inputs()) > 2 || len(r.api.OpenDialogs()) != 0 {
				t.Errorf("more questions were asked")
			}
		})
	}
	t.Run("the commit is not asked to guess when the setting is off", func(t *testing.T) {
		// git.requireGitUserConfig false lets git guess the author from the system: it is not an error of Emetos
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		e.Identity = false
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: map[string]any{"git.requireGitUserConfig": false}})
		r.typeMessage(rp, "x")
		r.commitBox(rp)
		if line := lastCommitLine(r); strings.Contains(line, "useConfigOnly") {
			t.Errorf("the command was %q", line)
		}
		_ = repo
	})
}
