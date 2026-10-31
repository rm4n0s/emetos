package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// fakeClock is the Clock of the tests: the timers fire when the test advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Duration
	f       func()
	stopped bool
	fired   bool
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now + d, f: f}
	c.timers = append(c.timers, t)
	return func() {
		c.mu.Lock()
		t.stopped = true
		c.mu.Unlock()
	}
}

// Advance moves the clock and runs the timers that are due, from this goroutine.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && t.at <= c.now {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// Pending is the number of timers that have not fired and were not stopped.
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// rig is the extension activated over the fake API in a world of gittest: its own HOME and XDG folders, its own trust file.
type rig struct {
	t      *testing.T
	env    *gittest.Env
	api    *fakeapi.API
	x      *gitscm
	clock  *fakeClock
	folder string
	cancel context.CancelFunc
}

// rigOptions tune a rig.
type rigOptions struct {
	// folder is the folder of the window tab; the "work" folder of the world when empty.
	folder string
	// settings are set before the extension starts.
	settings map[string]any
	// gitPath replaces the git program (a fake).
	gitPath string
	// noGit makes the git program a path that does not exist.
	noGit bool
	// home is the home folder the parent search stops at; the home of the world when empty.
	home string
	// devTrust is Options.DevTrust.
	devTrust string
	// noStart leaves the extension unactivated.
	noStart bool
	// extraEnv is added to the environment of git (the variable of a fake git).
	extraEnv []string
}

// newRig activates the extension in a world of gittest. The world's trust file is the extension's: the repositories the harness made
// are trusted already, and the tests that are about the question use a folder the harness did not make.
func newRig(t *testing.T, env *gittest.Env, o rigOptions) *rig {
	t.Helper()
	if o.folder == "" {
		o.folder = env.Work
	}
	if o.home == "" {
		o.home = env.Home
	}
	clock := &fakeClock{}
	e := NewWith(Options{Clock: clock, StateDir: filepath.Join(env.StateHome, "emetos"), Home: o.home, Env: append(env.Vars(), o.extraEnv...), DevTrust: o.devTrust}).(*gitscm)
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: filepath.Base(o.folder), Folder: o.folder, ID: o.folder})
	switch {
	case o.noGit:
		api.SetSetting("git.path", filepath.Join(env.Root, "no-such-git"))
		// PATH has the real git: hide it
		e.opts.Env = append(e.opts.Env, "PATH="+filepath.Join(env.Root, "empty"))
	case o.gitPath != "":
		api.SetSetting("git.path", o.gitPath)
		e.opts.Env = append(e.opts.Env, "PATH="+filepath.Join(env.Root, "empty")) // no other git to fall back to
	default:
		api.SetSetting("git.path", env.GitPath)
	}
	for k, v := range o.settings {
		api.SetSetting(k, v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &rig{t: t, env: env, api: api, x: e, clock: clock, folder: o.folder, cancel: cancel}
	t.Cleanup(r.stop)
	if !o.noStart {
		if err := e.Activate(ctx, api); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// stop ends the extension and waits for its processes.
func (r *rig) stop() {
	r.cancel()
	time.Sleep(10 * time.Millisecond)
}

// settle pumps the UI thread until the extension has nothing in flight: no scan, no refresh and no trust question.
func (r *rig) settle() {
	r.t.Helper()
	r.api.WaitFor("the extension to settle", func() bool {
		r.api.Pump()
		m := r.x.model
		if m.scanning || m.state != "initialized" && !r.x.missing {
			return false
		}
		for _, repo := range m.order {
			if repo.refreshing || repo.again || (repo.trusted && repo.repo == nil) {
				return false
			}
		}
		return true
	})
}

// repo returns the repository with this root.
func (r *rig) repo(root string) *repository {
	r.t.Helper()
	repo := r.x.model.byRoot(root)
	if repo == nil {
		r.t.Fatalf("no repository at %s (have %v)", root, r.roots())
	}
	return repo
}

func (r *rig) roots() []string {
	var out []string
	for _, repo := range r.x.model.order {
		out = append(out, repo.root)
	}
	return out
}

// source returns the fake source control of the repository.
func (r *rig) source(root string) *fakeapi.FakeSource {
	r.t.Helper()
	repo := r.repo(root)
	s := r.api.SourceControlByHandle(repo.handle)
	if s == nil {
		r.t.Fatalf("no source control for %s", root)
	}
	return s
}

// paths lists the paths (relative to the repository) of the resources of a group, in order.
func (r *rig) paths(root, group string) []string {
	r.t.Helper()
	g := r.source(root).Group(group)
	if g == nil {
		return nil
	}
	var out []string
	for _, res := range g.Resources() {
		p, _ := uriPath(res.URI)
		rel, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

// letters lists "path letter" of a group: the letter of the status of each resource, which the file decorations provider gives the
// file (the host reads it from there, not from the resource).
func (r *rig) letters(root, group string) []string {
	r.t.Helper()
	repo := r.repo(root)
	var list []resource
	switch group {
	case groupMerge:
		list = repo.res.merge
	case groupIndex:
		list = repo.res.index
	case groupWorking:
		list = repo.res.working
	case groupUntracked:
		list = repo.res.untracked
	}
	var out []string
	for _, res := range list {
		rel, _ := filepath.Rel(root, res.path)
		out = append(out, filepath.ToSlash(rel)+" "+statusLetter(res.status))
	}
	return out
}

// refresh asks for a status and waits for it.
func (r *rig) refresh(repo *repository) {
	r.t.Helper()
	before := repo.refreshes
	repo.refresh()
	r.api.WaitFor("the status", func() bool { r.api.Pump(); return repo.refreshes > before && !repo.refreshing })
}

// writeFile writes a file below a folder, with its folders.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
