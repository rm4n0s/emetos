// Package gitscm is the Git extension of Emetos (SPEC §10.30, D33): VS Code's built-in git extension ported to Go, a provider of the
// Source Control view of the workbench. This is its first part (step 24.5): it asks whether to trust a repository (D37), finds the
// repositories of the folder of the window tab by looking at the file system, shows their state in the Source Control view (the
// groups of changes with their letters, the branch and sync items, the count), refreshes it when files change, when the window gets the
// focus and on request, and publishes it to the other extensions. Since step 24.8 it stages, unstages and discards changes (stage.go,
// discard.go, pathcmds.go); committing and the rest come in the next steps, whose commands are declared and say they are not available yet
// until then.
//
// The extension runs the user's git program with the client of extensions/gitscm/git, which refuses to start a process in a
// repository before the user trusted it, and keeps the answers in extensions/gitscm/trust. Everything it keeps is used on the UI thread;
// the commands of git run on goroutines and hand their results back with api.Post.
//
// Ported from VS Code: extensions/git/src/main.ts, model.ts, repository.ts, statusbar.ts, watch.ts and repositoryCache.ts.
package gitscm

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
	"github.com/rm4n0s/emetos/extensions/gitscm/trust"
)

// The ids of the extension.
const (
	// ID is the id of the extension.
	ID = "emetos.gitscm"
	// ChannelName is the Output channel of the extension, the Git channel (git.showOutput).
	ChannelName = "Git"
	// TopicRepositoryChanged is the event the extension publishes when the state of a repository changed.
	TopicRepositoryChanged = ID + ".repositoryChanged"
	// TopicStatus is the request that answers with the state of a repository: {"root": path}.
	TopicStatus = ID + ".status"
)

// DebounceDelay is how long file changes are gathered before the status is run again (SPEC §10.30 "Refresh").
const DebounceDelay = 500 * time.Millisecond

// Clock is the timer of the debounce; the tests give it a fake one.
type Clock interface {
	// AfterFunc calls f after d, from its own goroutine, and returns the function that stops the call.
	AfterFunc(d time.Duration, f func()) (stop func())
}

type realClock struct{}

func (realClock) AfterFunc(d time.Duration, f func()) func() {
	t := time.AfterFunc(d, f)
	return func() { t.Stop() }
}

// Options configure the extension; the zero value is what Emetos runs.
type Options struct {
	// Clock times the debounce of the watcher; the real one when nil.
	Clock Clock
	// StateDir is the folder of the trust file and the repository cache ($XDG_STATE_HOME/emetos); read from the environment when empty.
	StateDir string
	// Home is the folder where the search for the repositories of the parent folders stops (the home folder of the user); read from the
	// environment when empty.
	Home string
	// Env is the environment git runs in ("KEY=VALUE"); the environment of Emetos when nil. The tests give the isolated world of gittest.
	Env []string
	// DevTrust is the folder EMETOS_DEV_GIT_TRUST names: every repository below it is trusted for the session (debug and emetos_dev builds).
	// Empty reads the variable in those builds.
	DevTrust string
	// RuntimeDir is $XDG_RUNTIME_DIR, where the server of git's askpass and editor programs keeps its socket
	// ($XDG_RUNTIME_DIR/emetos/git-<pid>/); read from the environment when empty. The tests give a folder of their own.
	RuntimeDir string
	// HelperBinary is the program the askpass and editor scripts start (emetos --git-askpass); the running executable when empty. The tests
	// give the test binary, which answers the three flags in its TestMain.
	HelperBinary string
}

// New is the Factory of the extension.
func New() extension.Extension { return NewWith(Options{}) }

// FactoryOf makes the Factory of an extension with the options.
func FactoryOf(opts Options) extension.Factory {
	return func() extension.Extension { return NewWith(opts) }
}

// NewWith makes the extension with the options.
func NewWith(opts Options) extension.Extension { return &gitscm{opts: opts} }

// contextKeys are the context keys the extension sets whose names do not start with its id (VS Code's names, which the
// when-clauses of its menus and views read).
var contextKeys = []string{
	"git.state", "git.missing", "git.untrustedRepositoryCount", "gitOpenRepositoryCount", "git.parentRepositoryCount",
	"git.unsafeRepositoryCount", "git.closedRepositoryCount", "git.mergeChanges", "git.activeResourceHasUnstagedChanges",
	"git.activeResourceHasStagedChanges", "git.activeResourceHasMergeConflicts", "git.timeline.selectedForCompare",
	"gitFreshRepository", "git.commitInProgress", "gitMergeInProgress", "gitRebaseInProgress", "gitCherryPickInProgress", "gitVersion235",
}

// Manifest implements extension.Extension.
func (*gitscm) Manifest() extension.Manifest {
	cmds := append(append([]extension.CommandSpec(nil), commandSpecs...), trustCommands...)
	menus := paletteMenuItems()
	menus = append(menus, extraMenus...)
	menus = append(menus, diffMenus()...)
	menus = append(menus, stageMenus()...)
	menus = append(menus, lineMenus()...)
	menus = append(menus, multiDiffMenus()...)
	menus = append(menus, editorMenus()...)
	menus = append(menus, stashMenus()...)
	menus = append(menus, timelineMenus()...)
	menus = append(menus, commitMenus()...)
	menus = append(menus, branchMenus()...)
	menus = append(menus, remoteMenus()...)
	menus = append(menus, mergeMenus()...)
	menus = append(menus, cloneMenus()...)
	return extension.Manifest{
		ID: ID, Name: "Git", Version: "0.1.0",
		Description:      "The Source Control view for Git repositories: trust, status, and the repositories of the folder.",
		ActivationEvents: []string{extension.OnStartup},
		Events: extension.EventDecl{
			Publishes: []extension.EventSpec{{Topic: TopicRepositoryChanged}},
			Handles:   nil,
			Subscribes: []string{extension.TopicWindowFocusChanged, extension.TopicDocumentSaved, extension.TopicDocumentClosed, extension.TopicWorkspaceClosed,
				extension.TopicEditorActiveChanged, extension.TopicEditorSelectionChanged, extension.TopicDocumentChanged, extension.TopicMergeEditorCompleted},
		},
		Contributes: extension.Contributes{
			Commands:    cmds,
			Menus:       menus,
			Keybindings: lineKeybindings(),
			Submenus: []extension.SubmenuSpec{{ID: stashSubmenu, Label: "Stash"}, {ID: commitSubmenu, Label: "Commit"}, {ID: branchSubmenu, Label: "Branch"},
				{ID: tagsSubmenu, Label: "Tags"}, {ID: pullPushSubmenu, Label: "Pull, Push"}, {ID: remotesSubmenu, Label: "Remote"}},
			Settings:    settingSpecs,
			ContextKeys: contextKeys,
			ViewWelcome: welcomePieces(),
		},
	}
}

// paletteMenuItems puts the commands into the command palette under the conditions of VS Code's commandPalette menu.
func paletteMenuItems() []extension.MenuItem {
	ids := make([]string, 0, len(paletteWhens))
	for id := range paletteWhens {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]extension.MenuItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, extension.MenuItem{Menu: extension.MenuCommandPalette, Command: id, When: paletteWhens[id]})
	}
	return items
}

// floatPtr points to a number, for the bounds of a setting.
func floatPtr(v float64) *float64 { return &v }

// gitscm is the extension of one scope.
type gitscm struct {
	opts Options

	api    extension.API
	ctx    context.Context
	log    *slog.Logger
	folder string
	clock  Clock
	home   string

	channel extension.OutputChannel
	trust   *trust.Store
	cache   *repoCache
	cfg     config
	model   *model

	// runner starts git; it is nil until git is found, and install says which git it is.
	runner  *git.Runner
	install git.Installation
	// missing says that no usable git was found (the context key git.missing).
	missing bool
	started bool
	stopped atomic.Bool

	// state is what the profile remembers (git-state.json); focused says whether the window has the keyboard focus (the autofetch waits for
	// it); autofetchAsked says that the question about autofetch was considered in this window.
	state          *stateStore
	focused        bool
	autofetchAsked bool

	// handlers are the handlers of the commands, by id.
	handlers map[string]extension.CommandHandler

	// content is what the provided documents of the versions of files need (content.go).
	content contentState

	// owners are the roots of the open repositories, for the quick diff providers (quickdiff.go).
	owners ownerRoots

	// multi is what the multi-file diff editor needs (multidiff.go).
	multi multiState

	// stash is what the stash editors need (stash.go).
	stash stashState
	// tl is what the Timeline needs (timeline.go).
	tl timelineState

	// ignores is the provider of the file decorations of the files git ignores (decorations.go).
	ignores *ignoreProvider

	// blame is the blame text of the editor and the status bar (blame.go).
	blame *blameController
	// stateDir is the folder of Emetos's state ($XDG_STATE_HOME/emetos), where the socket of the helper goes when there is no runtime folder.
	stateDir string
	// askpassOn and editorOn follow git.useIntegratedAskPass and git.useEditorAsCommitInput for the goroutines of the commands.
	askpassOn, editorOn atomic.Bool
	// helperMu guards the server of git's programs, which the first command that may ask starts (askpass.go).
	helperMu     sync.Mutex
	helperSrv    *helper.Server
	helperFailed bool
	// prompts, prompting and editors are the questions waiting for the user and the files git waits for; the UI thread only.
	prompts   []promptJob
	prompting bool
	editors   map[string]*editorWait
}

// homeDir is the home folder of the user, "" when it is not known.
func homeDir(opts Options) string {
	if opts.Home != "" {
		return opts.Home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// baseName is the last element of a path, or the path for the root.
func baseName(path string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 && i < len(path)-1 {
		return path[i+1:]
	}
	if path == "" {
		return "/"
	}
	return path
}
