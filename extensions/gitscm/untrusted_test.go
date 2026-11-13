package gitscm

import (
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The Not Trusted item of the status bar (step 35.1).

// shownItem is the Not Trusted item when it shows, else nil.
func shownItem(api *fakeapi.API) *fakeapi.StatusItem {
	it := api.StatusItem(UntrustedItemID)
	if it == nil || it.Hidden || it.Disposed {
		return nil
	}
	return it
}

func TestTheSpecOfTheNotTrustedItem(t *testing.T) {
	tests := []struct {
		name          string
		roots         []string
		ok            bool
		text, tooltip string
	}{
		{"none", nil, false, "", ""},
		{"one", []string{"/w/repo"}, true, "$(workspace-untrusted) Not Trusted", "Git runs nothing in /w/repo until you trust it. Click to decide."},
		{"two", []string{"/w/a", "/w/b"}, true, "$(workspace-untrusted) 2 Not Trusted", "Git runs nothing in these repositories until you trust them. Click to decide.\n/w/a\n/w/b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item, ok := untrustedItem(tt.roots)
			if ok != tt.ok {
				t.Fatalf("ok = %v", ok)
			}
			if !ok {
				return
			}
			if item.Text != tt.text || item.Tooltip != tt.tooltip {
				t.Errorf("text %q tooltip %q", item.Text, item.Tooltip)
			}
			if item.ID != UntrustedItemID || item.Alignment != extension.StatusLeft || item.Kind != extension.StatusWarning ||
				item.Priority != untrustedPriority || item.Command != CommandTrustRepository {
				t.Errorf("item %+v", item)
			}
		})
	}
}

func TestNoNotTrustedItemForATrustedRepository(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "a\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	if it := shownItem(r.api); it != nil {
		t.Errorf("the item shows for a trusted repository: %+v", it)
	}
}

func TestTheNotTrustedItemFollowsTheAnswers(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := untrustedRepo(t, e, filepath.Join(e.Work, "repo"))
	r := newRig(t, e, rigOptions{folder: repo})
	r.settle()
	// the question is not answered yet
	it := shownItem(r.api)
	if it == nil || it.Text != "$(workspace-untrusted) Not Trusted" || it.Tooltip != "Git runs nothing in "+repo+" until you trust it. Click to decide." {
		t.Fatalf("before the answer: %+v", it)
	}
	// Don't Trust keeps it
	r.api.OpenDialogs()[0].Press("Don't Trust")
	r.api.Pump()
	if shownItem(r.api) == nil {
		t.Fatal("Don't Trust took the item away")
	}
	// a click asks the question again, even after Don't Trust in this session
	if err := r.api.Run(shownItem(r.api).Command, nil); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	d := r.api.OpenDialogs()
	if len(d) != 1 {
		t.Fatalf("the click asked %d questions", len(d))
	}
	// Trust takes it away and starts git
	d[0].Press("Trust Repository")
	r.api.WaitFor("the repository to start", func() bool { r.api.Pump(); return len(r.api.SourceControls()) == 1 })
	if it := shownItem(r.api); it != nil {
		t.Errorf("the item stayed after Trust: %+v", it)
	}
	// Manage Trusted Repositories brings it back
	if err := r.api.Run(CommandManageTrusted, nil); err != nil {
		t.Fatal(err)
	}
	r.api.Picks()[0].Choose(0)
	r.api.Pump()
	if shownItem(r.api) == nil {
		t.Error("the item did not come back after the trust was taken away")
	}
}

func TestTheNotTrustedItemCountsTheRepositories(t *testing.T) {
	e := gittest.NewEnv(t)
	parent := filepath.Join(e.Work, "projects")
	a := untrustedRepo(t, e, filepath.Join(parent, "a"))
	b := untrustedRepo(t, e, filepath.Join(parent, "b"))
	r := newRig(t, e, rigOptions{folder: parent})
	r.settle()
	it := shownItem(r.api)
	if it == nil || it.Text != "$(workspace-untrusted) 2 Not Trusted" ||
		it.Tooltip != "Git runs nothing in these repositories until you trust them. Click to decide.\n"+a+"\n"+b {
		t.Fatalf("two repositories: %+v", it)
	}
}

func TestNoNotTrustedItemWithoutGit(t *testing.T) {
	tests := []struct {
		name   string
		opts   func(repo string) rigOptions
		settle bool // a disabled extension scans nothing, so it never settles
	}{
		{"git.enabled is false", func(repo string) rigOptions {
			return rigOptions{folder: repo, settings: map[string]any{"git.enabled": false}}
		}, false},
		{"git is not found", func(repo string) rigOptions { return rigOptions{folder: repo, noGit: true} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := untrustedRepo(t, e, filepath.Join(e.Work, "repo"))
			r := newRig(t, e, tt.opts(repo))
			r.api.Pump()
			if tt.settle {
				r.settle()
			}
			if it := shownItem(r.api); it != nil {
				t.Errorf("the item shows: %+v", it)
			}
		})
	}
	// turning Git off takes the item away
	e := gittest.NewEnv(t)
	repo := untrustedRepo(t, e, filepath.Join(e.Work, "repo"))
	r := newRig(t, e, rigOptions{folder: repo})
	r.settle()
	if shownItem(r.api) == nil {
		t.Fatal("no item before git.enabled is turned off")
	}
	r.api.SetSetting("git.enabled", false)
	r.api.Pump()
	if it := shownItem(r.api); it != nil {
		t.Errorf("the item stayed after git.enabled was turned off: %+v", it)
	}
}
