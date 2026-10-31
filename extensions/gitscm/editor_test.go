package gitscm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
)

// The editor of git (step 24.9): the real git commit and git rebase -i start the real helper, the extension opens the file and
// answers when the test plays the user and saves or closes it (the events a window publishes), or presses the title actions.

// editorRig is a repository with a staged change and the extension over it, without the sequence editor of the harness.
func editorRig(t *testing.T) (*rig, *gittest.Repo) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := e.Init("ed")
	repo.Write("a.txt", "one\n")
	repo.Commit("first")
	repo.Write("a.txt", "one\ntwo\n")
	repo.Git("add", "-A")
	r := newRig(t, e, rigOptions{folder: repo.Dir, dropEnv: []string{"GIT_SEQUENCE_EDITOR"}})
	r.settle()
	return r, repo
}

// editorOpened waits for the extension to open the file git wants edited, and returns its path and its URI.
func (r *rig) editorOpened(n int) (path, uri string) {
	r.t.Helper()
	r.api.WaitFor("the editor to open", func() bool { r.api.Pump(); return len(r.api.ShownDocuments()) > n })
	shown := r.api.ShownDocuments()[n]
	p, ok := uriPath(shown.URI)
	if !ok || shown.Preview || shown.Line != 1 {
		r.t.Fatalf("the editor was opened as %+v", shown)
	}
	return p, shown.URI
}

// userSaves plays a save of the document, as the window publishes it, and userCloses plays the closing of its tab.
func (r *rig) userSaves(uri string) {
	r.t.Helper()
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: uri})
	r.api.Pump()
}

func (r *rig) userCloses(uri string) {
	r.t.Helper()
	r.api.Emit(extension.TopicDocumentClosed, extension.DocumentClosed{URI: uri})
	r.api.Pump()
}

func TestGitCommitOpensTheMessageInAnEditorTab(t *testing.T) {
	tests := []struct {
		name       string
		act        func(r *rig, path, uri string)
		wantCommit bool
	}{
		{name: "saved and closed", wantCommit: true, act: func(r *rig, path, uri string) {
			writeFile(r.t, path, "Add the second line\n")
			r.userSaves(uri)
			r.userCloses(uri)
		}},
		{name: "closed with Don't Save", act: func(r *rig, path, uri string) {
			writeFile(r.t, path, "Never saved by the user\n")
			r.userCloses(uri)
		}},
		{name: "closed without a change", act: func(r *rig, path, uri string) { r.userCloses(uri) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo := editorRig(t)
			before := repo.Head()
			ch := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"commit", "-q"}})
			path, uri := r.editorOpened(0)
			if want := filepath.Join(repo.Dir, ".git", "COMMIT_EDITMSG"); path != want {
				t.Fatalf("the editor opened %s, want %s", path, want)
			}
			tc.act(r, path, uri)
			got := r.wait(ch)
			if tc.wantCommit {
				if got.err != nil {
					t.Fatalf("commit: %v", got.err)
				}
				if subject := repo.Git("log", "-1", "--format=%s"); subject != "Add the second line" {
					t.Errorf("the commit subject is %q", subject)
				}
				return
			}
			if got.err == nil || repo.Head() != before {
				t.Errorf("the commit went through: err %v, head %s before %s", got.err, repo.Head(), before)
			}
		})
	}
}

func TestTheTitleActionsOfACommitMessageTab(t *testing.T) {
	tab := func(uri string, dirty bool) extension.DocumentTab {
		return extension.DocumentTab{ID: "1:" + uri, Group: 1, Kind: "text", URI: uri, Dirty: dirty}
	}
	t.Run("accept a message that was not changed (amend)", func(t *testing.T) {
		r, repo := editorRig(t)
		before := repo.Head()
		ch := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"commit", "-q", "--amend"}})
		_, uri := r.editorOpened(0)
		r.api.SetTabs(tab(uri, false))
		if err := r.api.Run("git.commitMessageAccept", map[string]any{"groupId": 1.0, "uri": uri}); err != nil {
			t.Fatal(err)
		}
		if how := r.api.ClosedHow(); len(how) != 1 || how[0] != extension.TabCloseSave {
			t.Errorf("the tab was closed %v", how)
		}
		r.userCloses(uri) // the window closes it
		if got := r.wait(ch); got.err != nil {
			t.Fatalf("amend: %v", got.err)
		}
		if subject := repo.Git("log", "-1", "--format=%s"); subject != "first" || repo.Git("rev-list", "--count", "HEAD") != "1" {
			t.Errorf("the amend left %q, head %s was %s", subject, repo.Head(), before)
		}
	})
	t.Run("accept a message with changes: the save comes first", func(t *testing.T) {
		r, repo := editorRig(t)
		ch := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"commit", "-q"}})
		path, uri := r.editorOpened(0)
		writeFile(t, path, "Typed in the tab\n")
		r.api.SetTabs(tab(uri, true))
		if err := r.api.Run("git.commitMessageAccept", map[string]any{"uri": uri}); err != nil {
			t.Fatal(err)
		}
		if how := r.api.ClosedHow(); len(how) != 1 || how[0] != extension.TabCloseSave {
			t.Errorf("the tab was closed %v", how)
		}
		r.userSaves(uri) // the window saves, then closes
		r.userCloses(uri)
		if got := r.wait(ch); got.err != nil {
			t.Fatalf("commit: %v", got.err)
		}
		if subject := repo.Git("log", "-1", "--format=%s"); subject != "Typed in the tab" {
			t.Errorf("the subject is %q", subject)
		}
	})
	t.Run("cancel closes without saving and git aborts", func(t *testing.T) {
		r, repo := editorRig(t)
		before := repo.Head()
		ch := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"commit", "-q"}})
		path, uri := r.editorOpened(0)
		writeFile(t, path, "Saved once, then cancelled\n")
		r.userSaves(uri) // saved, and still cancelled afterwards
		r.api.SetTabs(tab(uri, true))
		if err := r.api.Run("git.commitMessageDiscard", map[string]any{"uri": uri}); err != nil {
			t.Fatal(err)
		}
		if how := r.api.ClosedHow(); len(how) != 1 || how[0] != extension.TabCloseDiscard {
			t.Errorf("the tab was closed %v", how)
		}
		r.userCloses(uri)
		if got := r.wait(ch); got.err == nil || repo.Head() != before {
			t.Errorf("the commit went through: err %v", got.err)
		}
	})
	t.Run("the active editor is the default", func(t *testing.T) {
		r, repo := editorRig(t)
		ch := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"commit", "-q"}})
		_, uri := r.editorOpened(0)
		r.api.SetTabs(tab(uri, false))
		r.api.SetActive(&extension.ActiveDocument{URI: uri})
		if err := r.api.Run("git.commitMessageDiscard", nil); err != nil {
			t.Fatal(err)
		}
		if how := r.api.ClosedHow(); len(how) != 1 {
			t.Errorf("the tab was closed %v", how)
		}
		r.userCloses(uri)
		if got := r.wait(ch); got.err == nil {
			t.Error("the commit went through")
		}
	})
	t.Run("a tab that is not open is left alone", func(t *testing.T) {
		r, _ := editorRig(t)
		for _, id := range []string{"git.commitMessageAccept", "git.commitMessageDiscard"} {
			if err := r.api.Run(id, map[string]any{"uri": "file:///nowhere/COMMIT_EDITMSG"}); err != nil {
				t.Fatal(err)
			}
			if err := r.api.Run(id, nil); err != nil {
				t.Fatal(err)
			}
		}
		if how := r.api.ClosedHow(); len(how) != 0 {
			t.Errorf("tabs were closed: %v", how)
		}
	})
}

func TestGitRebaseInteractiveEditsTheTodoListInATab(t *testing.T) {
	setup := func(t *testing.T) (*rig, *gittest.Repo) {
		t.Helper()
		e := gittest.NewEnv(t)
		repo := e.Init("rb")
		for _, name := range []string{"one", "two", "three"} {
			repo.Write(name+".txt", name+"\n")
			repo.Commit(name)
		}
		r := newRig(t, e, rigOptions{folder: repo.Dir, dropEnv: []string{"GIT_SEQUENCE_EDITOR"}})
		r.settle()
		return r, repo
	}
	t.Run("the todo list is edited and saved", func(t *testing.T) {
		r, repo := setup(t)
		ch := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"rebase", "-i", "-q", "HEAD~2"}})
		path, uri := r.editorOpened(0)
		if filepath.Base(path) != "git-rebase-todo" {
			t.Fatalf("the editor opened %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "pick ") {
			t.Fatalf("the todo list is %q (%v)", data, err)
		}
		// drop the older of the two commits
		lines := strings.Split(string(data), "\n")
		lines[0] = strings.Replace(lines[0], "pick", "drop", 1)
		writeFile(t, path, strings.Join(lines, "\n"))
		r.userSaves(uri)
		r.userCloses(uri)
		if got := r.wait(ch); got.err != nil {
			t.Fatalf("rebase: %v", got.err)
		}
		if subjects := repo.Git("log", "--format=%s"); subjects != "three\none" {
			t.Errorf("the history is %q, want the second commit dropped", subjects)
		}
	})
	t.Run("the tab is closed without a save and git aborts", func(t *testing.T) {
		r, repo := setup(t)
		before := repo.Head()
		ch := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"rebase", "-i", "-q", "HEAD~2"}})
		_, uri := r.editorOpened(0)
		r.userCloses(uri)
		got := r.wait(ch)
		if got.err == nil {
			t.Error("the rebase went on")
		}
		if repo.Head() != before {
			t.Errorf("head moved to %s", repo.Head())
		}
		if _, err := os.Stat(filepath.Join(repo.Dir, ".git", "rebase-merge")); !os.IsNotExist(err) {
			t.Errorf("a rebase is in progress: %v", err)
		}
	})
}

// showFails is an API whose documents cannot be shown.
type showFails struct {
	extension.API
	err error
}

func (a showFails) Documents() extension.Documents { return showFailsDocs{a.API.Documents(), a.err} }

type showFailsDocs struct {
	extension.Documents
	err error
}

func (d showFailsDocs) Show(string, int, int, bool) error { return d.err }

func TestAnEditorThatCannotOpenAnswersGitWithAFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		wantWarn bool
	}{
		{"a host without documents is no matter for the log", errors.New("ExtensionDocumentUnavailable", "the scope cannot show documents"), false},
		{"any other failure is logged", errors.New("ExtensionOpenFailed", "the file cannot be opened"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := recordLogs(t)
			r, repo := editorRig(t)
			before := repo.Head()
			r.x.api = showFails{r.x.api, tc.err}
			ch := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"commit", "-q"}})
			if got := r.wait(ch); got.err == nil || repo.Head() != before {
				t.Errorf("the commit went through: %v", got.err)
			}
			if warned := strings.Contains(logs.String(), "cannot open the file git wants edited"); warned != tc.wantWarn {
				t.Errorf("logged = %v, want %v:\n%s", warned, tc.wantWarn, logs.String())
			}
			if len(r.x.editors) != 0 {
				t.Errorf("the extension still waits for %d files", len(r.x.editors))
			}
		})
	}
}

func TestTheSameFileAskedAgainLosesTheFirstEditor(t *testing.T) {
	r, repo := editorRig(t)
	first := r.runGit(git.Command{Dir: repo.Dir, Args: []string{"commit", "-q", "--amend"}})
	r.editorOpened(0)
	// the same file path again, as another command of the repository would ask for it
	reply := make(chan bool, 1)
	r.x.openEditor(editorReq("", filepath.Join(repo.Dir, ".git", "COMMIT_EDITMSG")), func(rep helper.Reply) { reply <- rep.OK })
	r.api.Pump()
	if got := r.wait(first); got.err == nil {
		t.Error("the first command went on although its editor was taken")
	}
	select {
	case <-reply:
		t.Error("the second editor was answered")
	default:
	}
}

func TestEditorRequestsNameAFile(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		dir  string
		want string
	}{
		{"relative to the directory of git", []string{".git/COMMIT_EDITMSG"}, "/work/repo", "/work/repo/.git/COMMIT_EDITMSG"},
		{"absolute", []string{"/work/repo/.git/MERGE_MSG"}, "/elsewhere", "/work/repo/.git/MERGE_MSG"},
		{"options before the file", []string{"--wait", "-n", "a/../b.txt"}, "/w", "/w/b.txt"},
		{"no arguments", nil, "/w", ""},
		{"an empty argument", []string{""}, "/w", ""},
		{"relative and no directory", []string{"x"}, "", ""},
	}
	for _, tc := range tests {
		if got := editorPath(editorReq(tc.dir, tc.argv...)); got != tc.want {
			t.Errorf("%s: editorPath = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// editorReq is the request of an editor for these arguments, run in dir.
func editorReq(dir string, argv ...string) helper.Request {
	return helper.Request{Kind: helper.KindEditor, Argv: argv, Dir: dir}
}
