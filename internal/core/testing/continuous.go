package testing

import (
	"context"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"
)

// Continuous runs (VS Code's testingContinuousRunService.ts): a profile that supports them runs the tests of an item, or of all the
// tests of its controller, and runs them again when a file of the item changes. The model calls the profile's Run function with
// ProfileRequest.Continuous set when the continuous run starts, with every test of the item, and again for the items whose files
// changed, each time with a context that ends when the next run starts or the continuous run stops. Which files belong to an item is the
// profile's to say (ProfileSpec.Trigger); without it a file belongs to the items whose URI is the file. The window feeds the changes of
// the files in with FilesChanged.

// contRef is the continuous runs of one test (testID empty: all the tests of the controllers) with their profiles.
type contRef struct {
	testID  string
	runners []*contRunner
}

// contRunner is the continuous run of one profile.
type contRunner struct {
	ref     ProfileRef
	testID  string
	cancel  context.CancelFunc
	trigger chan struct{}
	done    chan struct{}

	mu      sync.Mutex
	pending []string
}

// root is the id the first run includes: the test, or the controller for all of them.
func (r *contRunner) root() string {
	if r.testID != "" {
		return r.testID
	}
	return r.ref.ControllerID
}

func (r *contRunner) add(ids []string) {
	r.mu.Lock()
	for _, id := range ids {
		if !slices.Contains(r.pending, id) {
			r.pending = append(r.pending, id)
		}
	}
	r.mu.Unlock()
	select {
	case r.trigger <- struct{}{}:
	default:
	}
}

func (r *contRunner) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.pending
	r.pending = nil
	return out
}

// StartContinuous starts the continuous run of the tests of the item (all of them for an empty id) with the profiles, and runs them
// once at once. A profile that already runs the item continuously is left as it is. It fails with TestingProfileUnknown for a profile
// that is not registered, with TestingContinuousUnsupported for one that does not support continuous runs or is of another controller
// than the item, and with TestingItemUnknown for an id that is not in the tree.
func (s *Service) StartContinuous(profiles []ProfileRef, testID string) error {
	s.mu.Lock()
	for _, ref := range profiles {
		p := s.profileLocked(ref)
		switch {
		case p == nil:
			s.mu.Unlock()
			return errors.New("TestingProfileUnknown", "no profile has this id", "controller", ref.ControllerID, "profile", ref.ID)
		case !p.spec.SupportsContinuous || (testID != "" && ControllerOf(testID) != ref.ControllerID):
			s.mu.Unlock()
			return errors.New("TestingContinuousUnsupported", "the profile cannot run this test continuously", "controller", ref.ControllerID, "profile", ref.ID, "test", testID)
		}
	}
	if _, ok := s.itemLocked(testID); testID != "" && !ok && s.controllers[testID] == nil {
		s.mu.Unlock()
		return errors.New("TestingItemUnknown", "no item has this id", "id", testID)
	}
	var ref *contRef
	for _, c := range s.continuous {
		if c.testID == testID {
			ref = c
		}
	}
	if ref == nil {
		ref = &contRef{testID: testID}
		s.continuous = append(s.continuous, ref)
	}
	for _, pr := range profiles {
		if slices.ContainsFunc(ref.runners, func(r *contRunner) bool { return r.ref == pr }) {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		r := &contRunner{ref: pr, testID: testID, cancel: cancel, trigger: make(chan struct{}, 1), done: make(chan struct{})}
		ref.runners = append(ref.runners, r)
		group := s.profileLocked(pr).spec.Group
		go s.continuousLoop(ctx, r, group)
	}
	s.emitLocked(Event{Kind: EventContinuous, IDs: []string{testID}})
	s.unlock()
	return nil
}

// StartContinuousGroup starts the continuous run with the default profiles of the group that support it (VS Code's start with a group);
// with a test id only those of its controller. It fails with TestingContinuousUnsupported when there are none.
func (s *Service) StartContinuousGroup(group Group, testID string) error {
	s.mu.Lock()
	var refs []ProfileRef
	for _, p := range s.groupDefaultsLocked(group, "") {
		if p.SupportsContinuous && (testID == "" || p.ControllerID == ControllerOf(testID)) {
			refs = append(refs, p.Ref())
		}
	}
	s.mu.Unlock()
	if len(refs) == 0 {
		return errors.New("TestingContinuousUnsupported", "no default profile of the group supports continuous runs", "group", int(group), "test", testID)
	}
	return s.StartContinuous(refs, testID)
}

// continuousLoop runs the profile, and runs it again when it is triggered, until ctx ends.
func (s *Service) continuousLoop(ctx context.Context, r *contRunner, group Group) {
	defer close(r.done)
	include := []string{r.root()}
	for ctx.Err() == nil {
		runCtx, cancel := context.WithCancel(ctx)
		stop := make(chan struct{})
		again := make(chan bool, 1)
		go func() {
			select {
			case <-r.trigger:
				cancel()
				again <- true
			case <-stop:
				again <- false
			}
		}()
		_, err := s.RunTests(runCtx, RunRequest{
			Targets: []RunTarget{{ControllerID: r.ref.ControllerID, ProfileID: r.ref.ID, TestIDs: include}},
			Group:   group, Continuous: true, PreserveFocus: true,
		})
		close(stop)
		retriggered := <-again
		cancel()
		if err != nil && ctx.Err() == nil {
			s.log.Warn("a continuous run failed", "controller", r.ref.ControllerID, "profile", r.ref.ID, "err", err)
		}
		if ctx.Err() != nil {
			return
		}
		if !retriggered {
			select {
			case <-ctx.Done():
				return
			case <-r.trigger:
			}
		}
		if include = r.take(); len(include) == 0 {
			include = []string{r.root()}
		}
	}
}

// FilesChanged tells the continuous runs that files changed (file:///path): each run whose items have a changed file starts again with
// the topmost of those items. It returns how many runs were started again.
func (s *Service) FilesChanged(uris []string) int {
	type candidate struct {
		runner *contRunner
		spec   func(Item, string) bool
		items  []Item
	}
	s.mu.Lock()
	var cands []candidate
	for _, ref := range s.continuous {
		for _, r := range ref.runners {
			p := s.profileLocked(r.ref)
			if p == nil {
				continue
			}
			var items []Item
			var rec func(id string)
			rec = func(id string) {
				for _, c := range s.childrenLocked(id) {
					items = append(items, c)
					rec(c.ID)
				}
			}
			if it, ok := s.itemLocked(r.root()); ok {
				items = append(items, it)
			}
			rec(r.root())
			cands = append(cands, candidate{runner: r, spec: p.spec.Trigger, items: items})
		}
	}
	s.mu.Unlock()

	started := 0
	for _, c := range cands {
		var hit []string
		for _, it := range c.items {
			for _, uri := range uris {
				if (c.spec != nil && c.spec(it, uri)) || (c.spec == nil && it.URI != "" && it.URI == uri) {
					hit = append(hit, it.ID)
					break
				}
			}
		}
		top := slices.DeleteFunc(slices.Clone(hit), func(id string) bool {
			return slices.ContainsFunc(hit, func(other string) bool { return IsAncestorID(other, id) })
		})
		if len(top) > 0 {
			c.runner.add(top)
			started++
		}
	}
	return started
}

// stopRunnersLocked cancels runners and takes the empty refs away.
func (s *Service) stopRunnersLocked(match func(ref *contRef, r *contRunner) bool) bool {
	changed := false
	var keep []*contRef
	for _, ref := range s.continuous {
		var left []*contRunner
		for _, r := range ref.runners {
			if match(ref, r) {
				r.cancel()
				changed = true
			} else {
				left = append(left, r)
			}
		}
		ref.runners = left
		if len(left) > 0 {
			keep = append(keep, ref)
		}
	}
	s.continuous = keep
	return changed
}

// StopContinuous stops the continuous runs of the item and of the items below it; with an empty id, all of them.
func (s *Service) StopContinuous(testID string) {
	s.mu.Lock()
	if s.stopRunnersLocked(func(ref *contRef, _ *contRunner) bool {
		return testID == "" || ref.testID == testID || IsAncestorID(testID, ref.testID)
	}) {
		s.emitLocked(Event{Kind: EventContinuous, IDs: []string{testID}})
	}
	s.unlock()
}

// StopContinuousProfile stops the continuous runs that use the profile, wherever they run.
func (s *Service) StopContinuousProfile(ref ProfileRef) {
	s.mu.Lock()
	if s.stopProfileContinuousLocked(ref) {
		s.emitLocked(Event{Kind: EventContinuous})
	}
	s.unlock()
}

func (s *Service) stopProfileContinuousLocked(ref ProfileRef) bool {
	return s.stopRunnersLocked(func(_ *contRef, r *contRunner) bool { return r.ref == ref })
}

func (s *Service) stopControllerContinuousLocked(controllerID string) {
	s.stopRunnersLocked(func(_ *contRef, r *contRunner) bool { return r.ref.ControllerID == controllerID })
}

// Close stops the continuous runs and cancels the runs, and waits for the goroutines of the continuous runs to end.
func (s *Service) Close() {
	s.mu.Lock()
	var done []chan struct{}
	for _, ref := range s.continuous {
		for _, r := range ref.runners {
			done = append(done, r.done)
		}
	}
	s.stopRunnersLocked(func(*contRef, *contRunner) bool { return true })
	s.mu.Unlock()
	s.CancelAll()
	for _, d := range done {
		<-d
	}
}

// IsContinuousEnabled reports whether some continuous run is going (the context key testing.isContinuousModeOn).
func (s *Service) IsContinuousEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.continuous) > 0
}

// ContinuousSpecificallyFor reports whether the item itself runs continuously.
func (s *Service) ContinuousSpecificallyFor(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.continuous, func(r *contRef) bool { return r.testID == id })
}

// ContinuousForAParentOf reports whether the item, an item above it or all the tests run continuously.
func (s *Service) ContinuousForAParentOf(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.continuous, func(r *contRef) bool { return r.testID == "" || r.testID == id || IsAncestorID(r.testID, id) })
}

// ContinuousForAChildOf reports whether the item, an item below it or all the tests run continuously.
func (s *Service) ContinuousForAChildOf(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.continuous, func(r *contRef) bool { return r.testID == "" || r.testID == id || IsAncestorID(id, r.testID) })
}

// ContinuousForProfile reports whether some continuous run uses the profile.
func (s *Service) ContinuousForProfile(ref ProfileRef) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.continuous, func(c *contRef) bool {
		return slices.ContainsFunc(c.runners, func(r *contRunner) bool { return r.ref == ref })
	})
}
