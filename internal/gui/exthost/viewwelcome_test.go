package exthost

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The welcome content of views (step 24.3): what a manifest declares for a view, in what order a view gets it, under what
// condition, and that it goes with its extension.

// welcomeManifest declares four pieces: one for the Changes view under its VS Code name, one for the same view under its id with
// a place in its group, one that needs a key, and one for the Explorer.
func welcomeManifest() extension.Manifest {
	m := manifest("wx")
	m.Contributes.ViewWelcome = []extension.ViewWelcomeSpec{
		{View: "scm", Contents: "second", Group: "5_scm@2"},
		{View: command.ViewSCM, Contents: "first", Group: "5_scm@1"},
		{View: "scm", Contents: "needs a folder", When: "config.git.enabled && workbenchState == empty", Group: "1_early", Enablement: "git.ready"},
		{View: "explorer", Contents: "open a folder"},
		{View: "scm", Contents: "last"},
	}
	return m
}

func welcomeTexts(cs []WelcomeContent) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Contents)
	}
	return out
}

func TestViewWelcome(t *testing.T) {
	f, _ := factory(welcomeManifest(), nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	scope := tb.scope
	for _, tc := range []struct {
		name string
		view string
		keys map[string]any
		want []string
	}{
		{"the view by its id, in the order of the groups and places", command.ViewSCM, map[string]any{"config.git.enabled": false}, []string{"first", "second", "last"}},
		{"a piece that holds comes first by its group", command.ViewSCM, map[string]any{"config.git.enabled": true, "workbenchState": "empty"}, []string{"needs a folder", "first", "second", "last"}},
		{"a piece whose condition fails is left out", command.ViewSCM, map[string]any{"config.git.enabled": true, "workbenchState": "folder"}, []string{"first", "second", "last"}},
		{"the Explorer by its VS Code name", command.ViewExplorerEmpty, nil, []string{"open a folder"}},
		{"a view that has none", command.ViewSCMRepositories, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := contextkey.NewRoot()
			for k, v := range tc.keys {
				keys.Set(k, v)
			}
			got := welcomeTexts(scope.ViewWelcome(tc.view, keys))
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
	if !scope.HasViewWelcome(command.ViewSCM) || !scope.HasViewWelcome(command.ViewExplorerEmpty) || scope.HasViewWelcome(command.ViewSCMRepositories) {
		t.Error("HasViewWelcome does not follow the manifests")
	}
}

func TestViewWelcomeEnablement(t *testing.T) {
	f, _ := factory(welcomeManifest(), nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	keys := contextkey.NewRoot()
	keys.Set("config.git.enabled", true)
	keys.Set("workbenchState", "empty")
	var piece WelcomeContent
	for _, c := range tb.scope.ViewWelcome(command.ViewSCM, keys) {
		if c.Contents == "needs a folder" {
			piece = c
		}
	}
	if piece.Enabled(keys) {
		t.Error("the buttons are enabled while git.ready is not set")
	}
	keys.Set("git.ready", true)
	if !piece.Enabled(keys) {
		t.Error("the buttons stay disabled after git.ready is set")
	}
	if !(WelcomeContent{}).Enabled(keys) {
		t.Error("a piece with no enablement is always enabled")
	}
}

func TestViewWelcomeFollowsTheExtension(t *testing.T) {
	f, _ := factory(welcomeManifest(), nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	if !tb.scope.HasViewWelcome(command.ViewSCM) {
		t.Fatal("the content is not there")
	}
	tb.disabled = []string{"wx"}
	tb.scope.Refresh()
	if tb.scope.HasViewWelcome(command.ViewSCM) || len(tb.scope.ViewWelcome(command.ViewSCM, nil)) != 0 {
		t.Error("the content of an extension that is turned off stayed")
	}
	tb.disabled = nil
	tb.scope.Refresh()
	if !tb.scope.HasViewWelcome(command.ViewSCM) {
		t.Error("the content did not come back with the extension")
	}
}

func TestViewWelcomeOfAFailedExtensionIsLeftOut(t *testing.T) {
	f, _ := factory(welcomeManifest(), func(f *fake) {
		f.activate = func(context.Context, extension.API) error { panic("boom") }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	waitFor(t, tb, "the extension to fail", func() bool { return tb.scope.State("wx") == StateFailed })
	if got := tb.scope.ViewWelcome(command.ViewSCM, nil); len(got) != 0 {
		t.Errorf("a failed extension shows %v", welcomeTexts(got))
	}
}

// gitWelcomeManifest declares the welcome content of VS Code's Git extension for the Source Control view, with its conditions as
// they are in its package.json (the contents are the names of the pieces).
func gitWelcomeManifest() extension.Manifest {
	m := manifest("gx")
	const base = "config.git.enabled && !git.missing"
	const none = " && git.parentRepositoryCount == 0 && git.unsafeRepositoryCount == 0 && git.closedRepositoryCount == 0"
	m.Contributes.ViewWelcome = []extension.ViewWelcomeSpec{
		{View: "scm", Contents: "disabled", When: "!config.git.enabled"},
		{View: "scm", Contents: "missing.remote", When: "config.git.enabled && git.missing && remoteName != ''"},
		{View: "scm", Contents: "missing.linux", When: "config.git.enabled && git.missing && remoteName == '' && isLinux"},
		{View: "scm", Contents: "missing.mac", When: "config.git.enabled && git.missing && remoteName == '' && isMac"},
		{View: "scm", Contents: "empty", When: base + " && workbenchState == empty" + none, Enablement: "git.state == initialized", Group: "2_open@1"},
		{View: "scm", Contents: "scanFolder", When: base + " && workbenchState == folder && workspaceFolderCount != 0 && git.state != initialized"},
		{View: "scm", Contents: "folder", When: base + " && git.state == initialized && workbenchState == folder && scm.providerCount == 0" + none + " && remoteName != 'codespaces'", Group: "5_scm@1"},
		{View: "scm", Contents: "parent", When: base + " && git.state == initialized && git.parentRepositoryCount == 1"},
		{View: "scm", Contents: "parents", When: base + " && git.state == initialized && git.parentRepositoryCount > 1"},
		{View: "scm", Contents: "unsafe", When: base + " && git.state == initialized && git.unsafeRepositoryCount == 1"},
		{View: "scm", Contents: "closed", When: base + " && git.state == initialized && git.closedRepositoryCount == 1"},
		{View: "explorer", Contents: "clone", When: "config.git.enabled && git.state == initialized && scm.providerCount == 0", Group: "5_scm@1"},
		{View: "explorer", Contents: "learnMore", When: "config.git.enabled && git.state == initialized && scm.providerCount == 0", Group: "5_scm@10"},
	}
	return m
}

// TestViewWelcomeOfGit checks which pieces of the Git extension's welcome content the Source Control view and the Explorer show for
// each state of Git and of the window (the context keys of the extension and of the workbench).
func TestViewWelcomeOfGit(t *testing.T) {
	f, _ := factory(gitWelcomeManifest(), nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	// the keys of a window with Git on and ready, a folder open and no repository found
	ready := map[string]any{
		"config.git.enabled": true, "git.missing": false, "git.state": "initialized", "workbenchState": "folder", "workspaceFolderCount": 1,
		"git.parentRepositoryCount": 0, "git.unsafeRepositoryCount": 0, "git.closedRepositoryCount": 0, "scm.providerCount": 0,
		"remoteName": "", "isLinux": true, "isMac": false,
	}
	with := func(over map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range ready {
			out[k] = v
		}
		for k, v := range over {
			out[k] = v
		}
		return out
	}
	for _, tc := range []struct {
		name         string
		view         string
		keys         map[string]any
		want         []string
		wantDisabled []string
	}{
		{"a folder with no repository", command.ViewSCM, ready, []string{"folder"}, nil},
		{"git turned off", command.ViewSCM, with(map[string]any{"config.git.enabled": false}), []string{"disabled"}, nil},
		{"git missing on Linux", command.ViewSCM, with(map[string]any{"git.missing": true}), []string{"missing.linux"}, nil},
		{"git missing in a remote window", command.ViewSCM, with(map[string]any{"git.missing": true, "remoteName": "ssh"}), []string{"missing.remote"}, nil},
		{"no folder, Git ready", command.ViewSCM, with(map[string]any{"workbenchState": "empty", "workspaceFolderCount": 0}), []string{"empty"}, nil},
		{"no folder, Git not ready yet", command.ViewSCM, with(map[string]any{"workbenchState": "empty", "workspaceFolderCount": 0, "git.state": "uninitialized"}), []string{"empty"}, []string{"empty"}},
		{"a folder that Git is still scanning", command.ViewSCM, with(map[string]any{"git.state": "uninitialized"}), []string{"scanFolder"}, nil},
		{"a repository in a parent folder", command.ViewSCM, with(map[string]any{"git.parentRepositoryCount": 1}), []string{"parent"}, nil},
		{"repositories in parent folders", command.ViewSCM, with(map[string]any{"git.parentRepositoryCount": 3}), []string{"parents"}, nil},
		{"an unsafe repository", command.ViewSCM, with(map[string]any{"git.unsafeRepositoryCount": 1}), []string{"unsafe"}, nil},
		{"a closed repository", command.ViewSCM, with(map[string]any{"git.closedRepositoryCount": 1}), []string{"closed"}, nil},
		{"a repository is open", command.ViewSCM, with(map[string]any{"scm.providerCount": 1}), nil, nil},
		{"the Explorer with no folder, Git ready", command.ViewExplorerEmpty, with(map[string]any{"workbenchState": "empty"}), []string{"clone", "learnMore"}, nil},
		{"the Explorer while a provider exists", command.ViewExplorerEmpty, with(map[string]any{"scm.providerCount": 1}), nil, nil},
		{"the Explorer with Git off", command.ViewExplorerEmpty, with(map[string]any{"config.git.enabled": false}), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := contextkey.NewRoot()
			for k, v := range tc.keys {
				keys.Set(k, v)
			}
			got := tb.scope.ViewWelcome(tc.view, keys)
			if names := welcomeTexts(got); !equalStrings(names, tc.want) {
				t.Fatalf("got %q, want %q", names, tc.want)
			}
			var disabled []string
			for _, c := range got {
				if !c.Enabled(keys) {
					disabled = append(disabled, c.Contents)
				}
			}
			if !equalStrings(disabled, tc.wantDisabled) {
				t.Errorf("buttons disabled in %q, want %q", disabled, tc.wantDisabled)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
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

// TestViewWelcomeOfTheGitExtensionItself is TestViewWelcomeOfGit for the pieces the Git extension of Emetos really declares (step 24.16): the
// buttons of a window with no repository, of the empty Explorer and of the states in between, and the links they run.
func TestViewWelcomeOfTheGitExtensionItself(t *testing.T) {
	m := manifest("gx")
	m.Contributes.ViewWelcome = gitscm.New().Manifest().Contributes.ViewWelcome
	f, _ := factory(m, nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	ready := map[string]any{
		"config.git.enabled": true, "git.missing": false, "git.state": "initialized", "workbenchState": "folder", "workspaceFolderCount": 1,
		"git.parentRepositoryCount": 0, "git.unsafeRepositoryCount": 0, "git.closedRepositoryCount": 0, "git.untrustedRepositoryCount": 0,
		"scm.providerCount": 0, "remoteName": "", "isLinux": true,
	}
	with := func(over map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range ready {
			out[k] = v
		}
		for k, v := range over {
			out[k] = v
		}
		return out
	}
	for _, tc := range []struct {
		name string
		view string
		keys map[string]any
		want []string // the start of the text of each piece shown
		link []string // a link each of the pieces has
	}{
		{"a folder with no repository has both buttons", command.ViewSCM, ready,
			[]string{"The folder currently open doesn't have a Git repository."}, []string{"[Initialize Repository](command:git.init?%5Btrue%5D)", "[Clone Repository](command:git.clone)"}},
		{"no folder at all", command.ViewSCM, with(map[string]any{"workbenchState": "empty", "workspaceFolderCount": 0}),
			[]string{"In order to use Git features"}, []string{"[Open Folder](command:workbench.action.files.openFolder)", "[Clone Repository](command:git.cloneRecursive)"}},
		{"a folder that is being scanned", command.ViewSCM, with(map[string]any{"git.state": "uninitialized"}), []string{"Scanning folder for Git repositories"}, nil},
		{"a repository in a parent folder", command.ViewSCM, with(map[string]any{"git.parentRepositoryCount": 1}), []string{"A Git repository was found in the parent folders"}, nil},
		{"a repository that is not trusted", command.ViewSCM, with(map[string]any{"git.untrustedRepositoryCount": 1, "scm.providerCount": 1}),
			[]string{"The Git repository in the folder is not trusted."}, []string{"[Trust Repository](command:git.trustRepository)"}},
		{"repositories that are not trusted", command.ViewSCM, with(map[string]any{"git.untrustedRepositoryCount": 2, "scm.providerCount": 2}),
			[]string{"The Git repositories in the folder are not trusted."}, []string{"[Trust Repository](command:git.trustRepository)"}},
		{"a repository is open and trusted", command.ViewSCM, with(map[string]any{"scm.providerCount": 1}), nil, nil},
		{"git is off", command.ViewSCM, with(map[string]any{"config.git.enabled": false}), []string{"If you would like to use Git features"}, nil},
		{"the Explorer with nothing in it", command.ViewExplorerEmpty, with(map[string]any{"workbenchState": "empty"}),
			[]string{"You can clone a repository locally."}, []string{"[Clone Repository](command:git.clone)"}},
		{"the Explorer while a repository exists", command.ViewExplorerEmpty, with(map[string]any{"scm.providerCount": 1}), nil, nil},
		{"the Explorer before Git has looked", command.ViewExplorerEmpty, with(map[string]any{"git.state": "uninitialized"}), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := contextkey.NewRoot()
			for k, v := range tc.keys {
				keys.Set(k, v)
			}
			got := tb.scope.ViewWelcome(tc.view, keys)
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want pieces starting %q", welcomeTexts(got), tc.want)
			}
			for i, c := range got {
				if !strings.HasPrefix(c.Contents, tc.want[i]) {
					t.Errorf("piece %d is %q, want it to start with %q", i, c.Contents, tc.want[i])
				}
				for _, link := range tc.link {
					if !strings.Contains(c.Contents, link) {
						t.Errorf("piece %d lacks the link %q:\n%s", i, link, c.Contents)
					}
				}
			}
		})
	}
}
