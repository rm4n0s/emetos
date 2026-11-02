package testing

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// RunTarget is the tests one profile runs.
type RunTarget struct {
	ControllerID string
	ProfileID    int
	// TestIDs are the items to run; the id of the controller itself runs all its tests.
	TestIDs []string
}

// RunRequest is what is run (VS Code's ResolvedTestRunRequest): the targets, and the items left out.
type RunRequest struct {
	Targets []RunTarget
	// Exclude are the items not to run; only those below an included item reach the profile that runs it.
	Exclude []string
	// Group is the kind of the profiles of the targets.
	Group Group
	// Continuous says that the run starts again when files change (Service.StartContinuous makes such requests).
	Continuous bool
	// PreserveFocus says that the run was not asked for with the Testing view in front.
	PreserveFocus bool
}

func (r RunRequest) clone() RunRequest {
	r.Exclude = slices.Clone(r.Exclude)
	r.Targets = slices.Clone(r.Targets)
	for i := range r.Targets {
		r.Targets[i].TestIDs = slices.Clone(r.Targets[i].TestIDs)
	}
	return r
}

// ProfileRequest is what a profile's Run function gets: the part of a RunRequest that is its own, with the items checked against the
// tree (VS Code's runControllerTestRequest).
type ProfileRequest struct {
	// ResultID is the result the run reports to; the controller makes its tasks with Service.Result(ResultID).AddTask. Empty for none.
	ResultID  string
	ProfileID int
	// Include are the ids of the items to run that are in the tree; the controller's own id is among them when all its tests run.
	Include []string
	// Exclude are the excluded items that are in the tree below an included one.
	Exclude       []string
	Continuous    bool
	PreserveFocus bool
}

// AmbiguousRequest asks to run tests of a group with whatever profiles the group has for them (VS Code's AmbiguousRunTestsRequest).
type AmbiguousRequest struct {
	Group Group
	// Tests are the ids of the items to run; the id of a controller stands for all its tests.
	Tests []string
	// Exclude are the items not to run.
	Exclude       []string
	Continuous    bool
	PreserveFocus bool
}

// usable tells whether the profile may run the item: it is of the item's controller and its tag, if it has one, is one of the item's
// (VS Code's canUseProfileWithTest). The id of a controller is an item with no tags.
func (s *Service) usableLocked(p *profile, id string) bool {
	if p.controller != ControllerOf(id) {
		return false
	}
	if p.spec.Tag == "" {
		return true
	}
	it, ok := s.itemLocked(id)
	return ok && it.HasTag(p.spec.Tag)
}

// ResolveGroup chooses the profiles that run the tests (VS Code's TestService.runTests): the default profiles of the group take the
// tests they can use; when none is taken, each controller's first profile of the group that can run a test takes it. It returns a
// request with no targets when nothing can run the tests.
func (s *Service) ResolveGroup(req AmbiguousRequest) RunRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := RunRequest{Group: req.Group, Exclude: slices.Clone(req.Exclude), Continuous: req.Continuous, PreserveFocus: req.PreserveFocus}
	for _, def := range s.groupDefaultsLocked(req.Group, "") {
		p := s.profileLocked(def.Ref())
		var ids []string
		for _, id := range req.Tests {
			if s.usableLocked(p, id) {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			out.Targets = append(out.Targets, RunTarget{ControllerID: p.controller, ProfileID: p.id, TestIDs: ids})
		}
	}
	if len(out.Targets) > 0 {
		return out
	}
	for _, cid := range s.ctrlOrder {
		ctl := s.controllers[cid]
		byProfile := map[int][]string{}
		var order []int
		for _, id := range req.Tests {
			if ControllerOf(id) != cid {
				continue
			}
			for _, p := range ctl.profiles {
				if p.spec.Group == req.Group && s.usableLocked(p, id) {
					if _, seen := byProfile[p.id]; !seen {
						order = append(order, p.id)
					}
					byProfile[p.id] = append(byProfile[p.id], id)
					break
				}
			}
		}
		for _, pid := range order {
			out.Targets = append(out.Targets, RunTarget{ControllerID: cid, ProfileID: pid, TestIDs: byProfile[pid]})
		}
	}
	return out
}

// targetRequestLocked is the request of a target as the controller gets it: the included ids that are in the tree (the controller's id
// among them runs everything), and the excluded ids that are in the tree below an included one. It reports false when no included id is
// in the tree.
func (s *Service) targetRequestLocked(t RunTarget, exclude []string) (include, excl []string, ok bool) {
	for _, id := range t.TestIDs {
		if _, found := s.itemLocked(id); found || (id == t.ControllerID && s.controllers[id] != nil) {
			if !slices.Contains(include, id) {
				include = append(include, id)
			}
		}
	}
	if len(include) == 0 {
		return nil, nil, false
	}
	for _, id := range exclude {
		if _, found := s.itemLocked(id); !found {
			continue
		}
		if slices.ContainsFunc(include, func(in string) bool { return IsAncestorID(in, id) }) && !slices.Contains(excl, id) {
			excl = append(excl, id)
		}
	}
	return include, excl, true
}

// RunTests runs the request and returns when every profile has returned and every task its profile started has ended. It makes one
// result for the whole request, runs the targets at the same time and calls their Run functions with no lock held. The result is
// completed when the call returns, also when a profile failed or the run was cancelled (the tasks left are ended by force
// Options.CancelDeadline after the cancel). The error is the first failure of a profile, which keeps the result; an error before
// anything started returns no result. It fails with TestingRunInvalid for a request with no targets or an unknown group, and with
// TestingProfileUnknown for a target whose profile is not registered.
func (s *Service) RunTests(ctx context.Context, req RunRequest) (*Result, error) {
	if len(req.Targets) == 0 || !req.Group.Valid() {
		return nil, errors.New("TestingRunInvalid", "a run request needs targets and a group of profiles")
	}
	s.mu.Lock()
	for _, t := range req.Targets {
		if s.profileLocked(ProfileRef{t.ControllerID, t.ProfileID}) == nil {
			s.mu.Unlock()
			return nil, errors.New("TestingProfileUnknown", "no profile has this id", "controller", t.ControllerID, "profile", t.ProfileID)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	res := s.newResultLocked(req, true, false)
	res.cancel = cancel
	s.unlock()

	var wg sync.WaitGroup
	var errMu sync.Mutex
	var first error
	for _, t := range req.Targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.runTarget(runCtx, res, t, req); err != nil {
				errMu.Lock()
				if first == nil {
					first = err
				}
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()

	s.mu.Lock()
	res.completeLocked()
	res.cancel = nil
	s.unlock()
	return res, first
}

// runTarget runs one target: it calls the profile and waits for the tasks the profile started to end.
func (s *Service) runTarget(ctx context.Context, res *Result, t RunTarget, req RunRequest) error {
	s.mu.Lock()
	p := s.profileLocked(ProfileRef{t.ControllerID, t.ProfileID})
	var include, exclude []string
	ok := false
	var run func(context.Context, ProfileRequest) error
	if p != nil {
		include, exclude, ok = s.targetRequestLocked(t, req.Exclude)
		run = p.spec.Run
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	err := run(ctx, ProfileRequest{
		ResultID: res.id, ProfileID: t.ProfileID, Include: include, Exclude: exclude, Continuous: req.Continuous, PreserveFocus: req.PreserveFocus,
	})
	// the tasks go on after the profile returned: wait for them, as VS Code does unless the run was cancelled, and then only for the
	// deadline
	res.waitTasks(ctx, s.opt.CancelDeadline)
	if err == nil {
		return nil
	}
	if _, isOurs := errors.FromError(err); isOurs {
		return err
	}
	return errors.NewErr("TestingRunFailed", err).SetMetadata("controller", t.ControllerID, "profile", t.ProfileID)
}

// waitTasks waits until no task of the result runs. Once ctx ends it waits for the deadline more, and then ends the tasks that are left.
func (r *Result) waitTasks(ctx context.Context, deadline time.Duration) {
	s := r.svc
	var timeout <-chan time.Time
	done := ctx.Done()
	for {
		s.mu.Lock()
		running := r.anyRunningLocked()
		idle := r.idle
		s.mu.Unlock()
		if !running {
			return
		}
		select {
		case <-idle:
		case <-done:
			done = nil
			timer := time.NewTimer(deadline)
			defer timer.Stop()
			timeout = timer.C
		case <-timeout:
			s.mu.Lock()
			for _, t := range r.tasks {
				r.endTaskLocked(t.id)
			}
			s.unlock()
			return
		}
	}
}

// CancelRun cancels the run that makes the result: the context its profiles run with ends. It does nothing for a result that is not
// being run by RunTests.
func (s *Service) CancelRun(resultID string) {
	s.mu.Lock()
	var cancel context.CancelFunc
	if r := s.resultLocked(resultID); r != nil {
		cancel = r.cancel
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// CancelAll cancels every run.
func (s *Service) CancelAll() {
	s.mu.Lock()
	var cancels []context.CancelFunc
	for _, r := range s.results {
		if r.cancel != nil {
			cancels = append(cancels, r.cancel)
		}
	}
	s.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// ExternalRun is a run a controller starts on its own, without being asked: a watcher that runs tests when files change.
type ExternalRun struct {
	Request RunRequest
	// Persist keeps the result in the saved state.
	Persist bool
}

// StartExternalRun makes a result for a run a controller started (VS Code's $startedExtensionTestRun). The result completes when its
// last task ends, so the controller adds a task to it with AddTask at once.
func (s *Service) StartExternalRun(ext ExternalRun) *Result {
	s.mu.Lock()
	res := s.newResultLocked(ext.Request, ext.Persist, true)
	s.unlock()
	return res
}
