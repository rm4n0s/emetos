package exthost

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// What the extensions need from the system (SPEC §10.36 "Checking"): the checks of the requirements the manifests declare, run off the
// UI thread when a page first asks for them, a few at a time and each with a limit, and kept for the window and the folder they were
// checked in.

// The defaults of RequirementsOptions.
const (
	// DefaultRequirementChecks is how many checks run at once.
	DefaultRequirementChecks = 4
	// DefaultRequirementTimeout is how long a check may take; past it the result is unknown.
	DefaultRequirementTimeout = 10 * time.Second
)

// RequirementsOptions are what NewRequirements needs.
type RequirementsOptions struct {
	// Extensions are the manifests of the extensions, in the order of the Extensions page; their Requirements are checked.
	Extensions []extension.Manifest
	// Logger receives the checks that panicked; slog.Default when nil.
	Logger *slog.Logger
	// Post runs a function on the UI thread; the listeners of OnChange are called through it. Nil calls them on the goroutine of the
	// check (tests).
	Post func(func())
	// Limit is how many checks run at once (DefaultRequirementChecks when 0), Timeout how long one may take
	// (DefaultRequirementTimeout when 0).
	Limit   int
	Timeout time.Duration
}

// RequirementRow is a requirement of an extension with what its check found.
type RequirementRow struct {
	// ExtensionID and ExtensionName say whose requirement it is.
	ExtensionID, ExtensionName string
	// Requirement is what the manifest declares.
	Requirement extension.Requirement
	// Result is what the last check in the current folder found; its State is empty when there is none yet.
	Result extension.RequirementResult
	// Checking tells that a check runs or waits for its turn.
	Checking bool
}

// Checked tells whether the row has a result.
func (r RequirementRow) Checked() bool { return r.Result.State != "" }

// reqKey names a requirement: the id of its extension and its own.
type reqKey struct{ ext, id string }

// reqEntry is a requirement as the checker knows it.
type reqEntry struct {
	key  reqKey
	name string
	req  extension.Requirement
}

// Requirements checks the requirements of the extensions for one window (SPEC §10.36 "Checking"). Its methods may be called from any
// goroutine.
type Requirements struct {
	log     *slog.Logger
	post    func(func())
	timeout time.Duration
	entries []reqEntry
	sem     chan struct{}
	ctx     context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup

	mu        sync.Mutex
	started   bool
	closed    bool
	folder    string
	setting   func(key string) (any, bool)
	results   map[string]map[reqKey]extension.RequirementResult
	gen       map[reqKey]int
	pending   map[reqKey]context.CancelFunc
	listeners []*func()
}

// NewRequirements makes the checker of a window. Nothing is checked until Start.
func NewRequirements(opts RequirementsOptions) *Requirements {
	limit, timeout := opts.Limit, opts.Timeout
	if limit <= 0 {
		limit = DefaultRequirementChecks
	}
	if timeout <= 0 {
		timeout = DefaultRequirementTimeout
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	ctx, stop := context.WithCancel(context.Background())
	r := &Requirements{
		log: log, post: opts.Post, timeout: timeout, sem: make(chan struct{}, limit), ctx: ctx, stop: stop,
		results: map[string]map[reqKey]extension.RequirementResult{}, gen: map[reqKey]int{}, pending: map[reqKey]context.CancelFunc{},
	}
	for _, m := range opts.Extensions {
		for _, req := range m.Requirements {
			r.entries = append(r.entries, reqEntry{key: reqKey{m.ID, req.ID}, name: m.Name, req: req})
		}
	}
	return r
}

// Start checks what has no result in the current folder yet; the first call starts the checks, the later ones do nothing more than
// that. A page calls it when it is shown.
func (r *Requirements) Start() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.checkMissing()
	r.mu.Unlock()
	r.changed()
}

// SetSource sets the folder of the window's active workspace tab and the reader of its settings (SettingReader). A new folder stops
// the checks that run and, once started, checks what has no result there yet; the results of the other folders are kept.
func (r *Requirements) SetSource(folder string, setting func(key string) (any, bool)) {
	r.mu.Lock()
	moved := folder != r.folder
	r.folder, r.setting = folder, setting
	if !moved || r.closed {
		r.mu.Unlock()
		return
	}
	for k, cancel := range r.pending {
		cancel()
		r.gen[k]++
		delete(r.pending, k)
	}
	if r.started {
		r.checkMissing()
	}
	r.mu.Unlock()
	r.changed()
}

// CheckAgain forgets the results of the current folder and checks everything again.
func (r *Requirements) CheckAgain() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.started = true
	delete(r.results, r.folder)
	for _, e := range r.entries {
		r.launch(e)
	}
	r.mu.Unlock()
	r.changed()
}

// SettingsChanged checks again, once started, the requirements whose path setting (the requirement's, or the one its last result
// named) is among the keys.
func (r *Requirements) SettingsChanged(keys []string) {
	r.mu.Lock()
	if !r.started || r.closed {
		r.mu.Unlock()
		return
	}
	launched := false
	for _, e := range r.entries {
		setting := e.req.PathSetting
		if res, ok := r.results[r.folder][e.key]; ok && res.PathSetting != "" {
			setting = res.PathSetting
		}
		if (setting != "" && slices.Contains(keys, setting)) || (e.req.PathSetting != "" && slices.Contains(keys, e.req.PathSetting)) {
			r.launch(e)
			launched = true
		}
	}
	r.mu.Unlock()
	if launched {
		r.changed()
	}
}

// Rows lists the requirements, in the order of the extensions and of their manifests, with the results of the current folder.
func (r *Requirements) Rows() []RequirementRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]RequirementRow, len(r.entries))
	for i, e := range r.entries {
		_, checking := r.pending[e.key]
		rows[i] = RequirementRow{ExtensionID: e.key.ext, ExtensionName: e.name, Requirement: e.req, Result: r.results[r.folder][e.key], Checking: checking}
	}
	return rows
}

// Folder is the folder the checks are for.
func (r *Requirements) Folder() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.folder
}

// OnChange calls fn (through Post) when the rows may have changed, and returns the function that stops it.
func (r *Requirements) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	r.mu.Lock()
	r.listeners = append(r.listeners, p)
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.listeners = slices.DeleteFunc(r.listeners, func(x *func()) bool { return x == p })
	}
}

// Close stops every check and waits for their goroutines; nothing is checked afterwards. A check that does not end with its ctx is
// not waited for past its limit.
func (r *Requirements) Close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.stop()
	r.wg.Wait()
}

// changed tells the listeners.
func (r *Requirements) changed() {
	r.mu.Lock()
	listeners := slices.Clone(r.listeners)
	r.mu.Unlock()
	call := func() {
		for _, f := range listeners {
			(*f)()
		}
	}
	if r.post != nil {
		r.post(call)
		return
	}
	call()
}

// checkMissing launches the checks of the requirements that have no result in the current folder and none running. r.mu is held.
func (r *Requirements) checkMissing() {
	for _, e := range r.entries {
		if _, done := r.results[r.folder][e.key]; done {
			continue
		}
		if _, running := r.pending[e.key]; running {
			continue
		}
		r.launch(e)
	}
}

// launch starts the check of a requirement in the current folder, stopping the one that runs. r.mu is held.
func (r *Requirements) launch(e reqEntry) {
	if cancel := r.pending[e.key]; cancel != nil {
		cancel()
	}
	r.gen[e.key]++
	gen, folder := r.gen[e.key], r.folder
	env := extension.RequirementEnv{Folder: folder, Setting: r.setting}
	ctx, cancel := context.WithCancel(r.ctx)
	r.pending[e.key] = cancel
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer cancel()
		select {
		case r.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		res, ok := r.run(ctx, e, env)
		<-r.sem
		if !ok {
			return
		}
		r.mu.Lock()
		if r.gen[e.key] != gen || r.closed {
			r.mu.Unlock()
			return
		}
		delete(r.pending, e.key)
		if r.results[folder] == nil {
			r.results[folder] = map[reqKey]extension.RequirementResult{}
		}
		r.results[folder][e.key] = res
		r.mu.Unlock()
		r.changed()
	}()
}

// run runs one check within the limit. It reports false when ctx ended before the check did (it was stopped, not timed out).
func (r *Requirements) run(ctx context.Context, e reqEntry, env extension.RequirementEnv) (extension.RequirementResult, bool) {
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	done := make(chan extension.RequirementResult, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				err := errors.New("ExtensionRequirementPanicked", "the check of a requirement panicked", "extension", e.key.ext, "requirement", e.key.id, "panic", fmt.Sprint(p))
				r.log.Error("the check of a requirement failed", "err", err)
				done <- extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check failed"}
			}
		}()
		res := e.req.Check(tctx, env)
		if res.State == "" {
			res.State = extension.RequirementUnknown
		}
		done <- res
	}()
	select {
	case res := <-done:
		if ctx.Err() != nil {
			return extension.RequirementResult{}, false
		}
		return res, true
	case <-tctx.Done():
		if ctx.Err() != nil {
			return extension.RequirementResult{}, false
		}
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: fmt.Sprintf("the check took longer than %s", r.timeout)}, true
	}
}

// SettingReader is the RequirementEnv.Setting of a window tab's settings (SPEC §10.36): a registered key gives its value as the
// settings give it (a folder's value only for a setting whose scope allows it), a key nobody registered gives the value of the
// user's settings file only, never a folder's, so that a folder cannot choose a program that a check runs. Nil settings give
// nothing. It is safe from any goroutine.
func SettingReader(st *settings.Store) func(key string) (any, bool) {
	return func(key string) (any, bool) {
		if st == nil {
			return nil, false
		}
		if _, ok := st.Registry().Lookup(key); ok {
			return settings.Get[any](st, key, ""), true
		}
		in := st.Inspect(key, "")
		return in.User.Value, in.User.Ok
	}
}
