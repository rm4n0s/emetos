package gitscm

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// Autofetch and the state of the profile (SPEC §10.30 "Remotes, fetch, pull, push and sync", §12): git.autofetch is off by default,
// nothing is fetched on a schedule until the setting is on, and after the first remote command that worked Emetos asks once whether the
// user wants it.
//
// Ported from VS Code: extensions/git/src/autofetch.ts

const (
	// stateFile is the name of the state file in $XDG_STATE_HOME/emetos: what the user answered to questions that are asked once.
	stateFile = "git-state.json"
	// maxStateSize bounds the file when it is read.
	maxStateSize = 64 << 10
	// minAutofetchPeriod is the shortest time between two automatic fetches: a setting of 0 or less would fetch in a loop.
	minAutofetchPeriod = time.Second
)

// profileState is what the profile remembers between runs. It holds answers, never credentials or URLs.
type profileState struct {
	// AutofetchInformed says that the question "Would you like Emetos to periodically run git fetch?" was answered Yes or No (VS Code's
	// autofetch.didInformUser).
	AutofetchInformed bool `json:"autofetchInformed,omitempty"`
	// ConfirmBranchPublishOff says that the user chose "OK, Don't Ask Again" when asked to publish a branch (VS Code's
	// confirmBranchPublish, false).
	ConfirmBranchPublishOff bool `json:"confirmBranchPublishOff,omitempty"`
	// WorktreeRoots is, per repository (its root), the folder the worktrees made for it were last put in (VS Code's worktreeRoot:<root> in
	// the global state): the next one starts there.
	WorktreeRoots map[string]string `json:"worktreeRoots,omitempty"`
}

// stateStore keeps the profileState in a file, read when first needed and written atomically with mode 0600 when it changes. It is used
// on the UI thread.
type stateStore struct {
	path   string
	cur    profileState
	loaded bool
}

func newStateStore(stateDir string) *stateStore {
	return &stateStore{path: filepath.Join(stateDir, stateFile)}
}

// get returns the state; a file that cannot be read or is not JSON is an empty state, and the questions are asked again.
func (s *stateStore) get() profileState {
	if !s.loaded {
		s.loaded = true
		data, err := statefile.Read(s.path, maxStateSize, statefile.Writable)
		if err == nil {
			if jsonErr := json.Unmarshal(data, &s.cur); jsonErr != nil {
				s.cur = profileState{}
			}
		}
	}
	return s.cur
}

// update changes the state and writes it; a failure to write is logged and the answer holds for this session.
func (s *stateStore) update(x *gitscm, change func(*profileState)) {
	s.get()
	change(&s.cur)
	data, err := json.Marshal(s.cur)
	if err == nil {
		err = statefile.Write(s.path, append(data, '\n'))
	}
	if err != nil {
		x.log.Warn("cannot save the state of the Git extension", "err", err)
	}
}

// autofetchMode reads git.autofetch: off, on for the remote of the branch, or on for all remotes.
func (x *gitscm) autofetchMode() (enabled, all bool) {
	v, _ := x.api.Settings().Get("git.autofetch")
	switch t := v.(type) {
	case bool:
		return t, false
	case string:
		return t == "all", t == "all"
	}
	return false, false
}

// autofetcher fetches a repository now and then. It does nothing while it is off: no timer and no process. When it is on it waits for the
// repository to be idle (no command of Emetos is running) and the window to be focused, fetches silently (a fetch that never prompts for a
// password), and waits for the period; a failed authentication turns it off, so a missing credential does not ask every three minutes.
//
// Ported from VS Code: extensions/git/src/autofetch.ts (AutoFetcher)
type autofetcher struct {
	r *repository
	// enabled and all follow the setting.
	enabled, all bool
	// wanting says that a fetch is due and waits for the repository to be idle and the window focused; running that one is in flight.
	wanting, running bool
	// stop stops the timer of the period; gen counts the times the fetcher was turned on or off, so that a timer or a fetch of an earlier
	// run is ignored.
	stop func()
	gen  int
}

// configure follows the setting.
func (a *autofetcher) configure() {
	on, all := a.r.m.x.autofetchMode()
	a.all = all
	if on {
		a.enable()
	} else {
		a.disable()
	}
}

func (a *autofetcher) enable() {
	if a.enabled {
		return
	}
	a.enabled = true
	a.gen++
	a.wanting = true
	a.poke()
}

func (a *autofetcher) disable() {
	a.enabled, a.wanting = false, false
	a.gen++
	if a.stop != nil {
		a.stop()
		a.stop = nil
	}
}

// poke starts the fetch that is due when the repository is idle and the window focused. It is called when either may have changed.
func (a *autofetcher) poke() {
	r := a.r
	x := r.m.x
	if !a.enabled || !a.wanting || a.running || r.disposed || r.repo == nil || x.stopped.Load() || r.own != 0 || !x.focused {
		return
	}
	a.wanting, a.running = false, true
	gen := a.gen
	job := x.fetchJob(r, git.FetchOptions{All: a.all}, true)
	job.after = func() { a.fetched(gen, nil) }
	job.failed = func(err error) bool { a.fetched(gen, err); return true }
	x.runRemote(r, job)
}

// fetched is the end of a fetch: the next one is due after the period, unless the authentication failed.
func (a *autofetcher) fetched(gen int, err error) {
	a.running = false
	if gen != a.gen {
		return
	}
	if errTag(err) == "AuthenticationFailed" {
		a.disable()
		return
	}
	if !a.enabled {
		return
	}
	x := a.r.m.x
	period := time.Duration(read(x.api, "git.autofetchPeriod", 180)) * time.Second
	period = max(period, minAutofetchPeriod)
	a.stop = x.clock.AfterFunc(period, func() {
		x.api.Post(func() {
			if a.gen == gen && a.enabled {
				a.wanting = true
				a.poke()
			}
		})
	})
}

// startAutofetch makes the autofetcher of a repository that was opened.
func (r *repository) startAutofetch() {
	r.stopAutofetch()
	r.af = &autofetcher{r: r}
	r.af.configure()
}

// stopAutofetch turns the autofetcher of the repository off.
func (r *repository) stopAutofetch() {
	if r.af != nil {
		r.af.disable()
		r.af = nil
	}
}

// pokeAutofetch tells the autofetcher that the repository may be idle now.
func (r *repository) pokeAutofetch() {
	if r.af != nil {
		r.af.poke()
	}
}

// autofetchSettingChanged follows git.autofetch in every repository.
func (x *gitscm) autofetchSettingChanged() {
	for _, r := range x.model.order {
		if r.af != nil {
			r.af.configure()
		}
	}
}

// goodRemoteOperation is a fetch, pull, push or sync that worked: the first of them in a window asks the question about autofetch, when
// it was not answered and autofetch is off. When it is on already the question counts as answered.
//
// Ported from VS Code: extensions/git/src/autofetch.ts (onFirstGoodRemoteOperation)
func (x *gitscm) goodRemoteOperation(r *repository) {
	if x.autofetchAsked || x.stopped.Load() {
		return
	}
	x.autofetchAsked = true
	notInformed := !x.state.get().AutofetchInformed
	on, _ := x.autofetchMode()
	if on && notInformed {
		x.state.update(x, func(s *profileState) { s.AutofetchInformed = true })
	}
	if on || !notInformed {
		return
	}
	answered := func() { x.state.update(x, func(s *profileState) { s.AutofetchInformed = true }) }
	x.api.Notifications().Info(`Would you like Emetos to periodically run "git fetch"?`,
		extension.NotificationAction{Label: "Yes", Run: func() {
			x.saveSetting("git.autofetch", true)
			answered()
		}},
		extension.NotificationAction{Label: "No", Run: answered},
		extension.NotificationAction{Label: "Ask Me Later", Run: func() {}},
	)
}
