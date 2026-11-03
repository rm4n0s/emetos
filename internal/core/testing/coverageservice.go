package testing

import "sync"

// SelectedCoverage is the coverage that is open: the one the Test Coverage view, the editors and the Explorer show.
type SelectedCoverage struct {
	// Coverage is the coverage of one task of a result, ResultID and TaskID name that task.
	Coverage         *Coverage
	ResultID, TaskID string
}

// CoverageService keeps which coverage is open and whether the editors show it inline (VS Code's TestCoverageService). A result that
// ends with coverage opens it, one that ends without closes any; removing the result of the open coverage closes it. It is made over a
// Service and is safe for concurrent use; the listeners are called on the goroutine that changed it, outside its lock.
type CoverageService struct {
	svc  *Service
	stop func()

	mu       sync.Mutex
	selected *SelectedCoverage
	inline   bool
	subs     map[int]func()
	subSeq   int
}

// NewCoverageService makes a service that follows the results of svc. Dispose stops it.
func NewCoverageService(svc *Service) *CoverageService {
	c := &CoverageService{svc: svc, subs: map[int]func(){}}
	c.stop = svc.Subscribe(c.onEvent)
	return c
}

// Dispose stops following the results.
func (c *CoverageService) Dispose() {
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
}

func (c *CoverageService) onEvent(ev Event) {
	if ev.Kind != EventResult {
		return
	}
	switch ev.Change {
	case ResultCompleted:
		if ev.Result == nil {
			return
		}
		for _, t := range ev.Result.Tasks() {
			if t.HasCoverage {
				c.Open(ev.Result, t.ID)
				return
			}
		}
		c.Close()
	case ResultRemoved:
		c.mu.Lock()
		gone := false
		if c.selected != nil {
			for _, r := range ev.Removals {
				gone = gone || r.ID() == c.selected.ResultID
			}
		}
		c.mu.Unlock()
		if gone {
			c.Close()
		}
	}
}

// Open opens the coverage of a task of a result, and does nothing when the task has none.
func (c *CoverageService) Open(res *Result, taskID string) {
	cov := res.Coverage(taskID)
	if cov == nil {
		return
	}
	c.mu.Lock()
	c.selected = &SelectedCoverage{Coverage: cov, ResultID: res.ID(), TaskID: taskID}
	c.mu.Unlock()
	c.notify()
}

// Close closes the open coverage, if any.
func (c *CoverageService) Close() {
	c.mu.Lock()
	had := c.selected != nil
	c.selected = nil
	c.mu.Unlock()
	if had {
		c.notify()
	}
}

// Selected is the open coverage, and false when none is open.
func (c *CoverageService) Selected() (SelectedCoverage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.selected == nil {
		return SelectedCoverage{}, false
	}
	return *c.selected, true
}

// ShowInline says whether the editors show the coverage inline: the ranges of text, the counts and the branches, not only the gutter.
func (c *CoverageService) ShowInline() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inline
}

// SetShowInline sets whether the editors show the coverage inline.
func (c *CoverageService) SetShowInline(on bool) {
	c.mu.Lock()
	changed := c.inline != on
	c.inline = on
	c.mu.Unlock()
	if changed {
		c.notify()
	}
}

// OnChange registers a function called when the open coverage or the inline setting changes; the returned function removes it.
func (c *CoverageService) OnChange(fn func()) (stop func()) {
	c.mu.Lock()
	c.subSeq++
	id := c.subSeq
	c.subs[id] = fn
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.subs, id)
		c.mu.Unlock()
	}
}

func (c *CoverageService) notify() {
	c.mu.Lock()
	fns := make([]func(), 0, len(c.subs))
	for id := 1; id <= c.subSeq; id++ {
		if fn, ok := c.subs[id]; ok {
			fns = append(fns, fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}
