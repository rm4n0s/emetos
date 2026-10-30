package git

import (
	"context"
	"sync"

	"github.com/rm4n0s/errors"
)

// Scope is the part of a repository that an operation reads or changes. A read waits only for the earlier writes whose scope
// overlaps its own ("a read waits for a write that changes what it reads", SPEC §10.30 "Queue").
type Scope uint8

// The scopes.
const (
	// ScopeIndex is the index (staging area).
	ScopeIndex Scope = 1 << iota
	// ScopeWorkTree is the files of the work tree.
	ScopeWorkTree
	// ScopeRefs is the branches, tags, HEAD and the commits.
	ScopeRefs
	// ScopeConfig is the configuration and the remotes.
	ScopeConfig
	// ScopeStash is the stash.
	ScopeStash
	// ScopeAll is everything; use it when unsure.
	ScopeAll = ScopeIndex | ScopeWorkTree | ScopeRefs | ScopeConfig | ScopeStash
)

// Op describes an operation of the queue.
type Op struct {
	// Write says that the operation changes the repository: writes run one at a time per repository, in the order they were
	// asked. Reads run in parallel.
	Write bool
	// Scope is what a read reads or a write changes; zero means ScopeAll.
	Scope Scope
}

func (o Op) scope() Scope {
	if o.Scope == 0 {
		return ScopeAll
	}
	return o.Scope
}

// DefaultMaxReads is how many reads of one repository run at the same time.
const DefaultMaxReads = 4

// Queue orders the operations of one repository (SPEC §10.30 "Queue", ported from VS Code's Repository.run and the operation
// kinds of operation.ts): reads run in parallel, at most four at a time; writes run one at a time, in the order they were
// asked; a read that comes after a write that changes what it reads waits for that write. A read never waits for a write that
// came after it. The queue starts no goroutine of its own for Do; Coalesce starts one per coalesced key while it runs.
//
// Ported from VS Code: extensions/git/src/operation.ts (OperationKind.readOnly) and repository.ts (run).
type Queue struct {
	mu       sync.Mutex
	maxReads int
	waiting  []*entry
	active   []*entry
	coalesce map[string]*coalescer
}

type entry struct {
	op      Op
	started bool
	ready   chan struct{}
}

// NewQueue makes a Queue that runs at most maxReads reads at a time (DefaultMaxReads when maxReads is not positive).
func NewQueue(maxReads int) *Queue {
	if maxReads <= 0 {
		maxReads = DefaultMaxReads
	}
	return &Queue{maxReads: maxReads, coalesce: map[string]*coalescer{}}
}

// Do waits for the turn of the operation and runs fn. When ctx ends while the operation waits, Do returns an error with the
// tag GitQueueCancelled and fn is not run. The error of fn is returned untouched.
func (q *Queue) Do(ctx context.Context, op Op, fn func(ctx context.Context) error) error {
	e := &entry{op: op, ready: make(chan struct{})}
	q.mu.Lock()
	q.waiting = append(q.waiting, e)
	q.scheduleLocked()
	q.mu.Unlock()

	select {
	case <-e.ready:
	case <-ctx.Done():
		q.mu.Lock()
		if !e.started {
			q.removeWaitingLocked(e)
			q.scheduleLocked()
			q.mu.Unlock()
			return errors.NewErr("GitQueueCancelled", ctx.Err())
		}
		q.mu.Unlock()
		// The turn came at the same moment: run, so that the slot is given back; fn sees the ended context itself.
	}
	defer q.finish(e)
	return fn(ctx)
}

func (q *Queue) finish(e *entry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, a := range q.active {
		if a == e {
			q.active = append(q.active[:i], q.active[i+1:]...)
			break
		}
	}
	q.scheduleLocked()
}

func (q *Queue) removeWaitingLocked(e *entry) {
	for i, w := range q.waiting {
		if w == e {
			q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
			return
		}
	}
}

// scheduleLocked starts every waiting operation that may run now. The waiting list is in the order the operations were asked.
func (q *Queue) scheduleLocked() {
	reads, writeActive := 0, false
	var activeWrites Scope
	for _, a := range q.active {
		if a.op.Write {
			writeActive = true
			activeWrites |= a.op.scope()
		} else {
			reads++
		}
	}
	var earlierWrites Scope // the scopes of the writes ahead of the entry that stay waiting
	earlierWaitingWrite := false
	remaining := q.waiting[:0:0]
	for _, w := range q.waiting {
		start := false
		if w.op.Write {
			start = !writeActive && !earlierWaitingWrite
		} else {
			start = reads < q.maxReads && w.op.scope()&(activeWrites|earlierWrites) == 0
		}
		if start {
			w.started = true
			q.active = append(q.active, w)
			close(w.ready)
			if w.op.Write {
				writeActive = true
				activeWrites |= w.op.scope()
			} else {
				reads++
			}
			continue
		}
		if w.op.Write {
			earlierWaitingWrite = true
			earlierWrites |= w.op.scope()
		}
		remaining = append(remaining, w)
	}
	q.waiting = remaining
}

// coalescer is the state of one key of Coalesce: the round that runs and the one that waits behind it.
type coalescer struct {
	running *round
	next    *round
}

type round struct {
	joined int // the number of callers that wait for the round
	ctx    context.Context
	op     Op
	fn     func(ctx context.Context) error
	done   chan struct{}
	err    error
}

// Coalesce runs fn as an operation of the queue, merging the asks that come while it runs: the first ask starts a run; the
// asks that arrive while it runs (or while the next one waits) share one more run that starts when it ends. So many file
// changes during a status refresh give one more refresh, not many (SPEC §10.30 "Queue"). Every caller gets the error of the run
// it joined, and the run does not stop when one caller's ctx ends (it uses a context without the cancellation of the ask that
// started it); a caller whose ctx ends stops waiting and gets an error with the tag GitQueueCancelled. A run uses the fn of the
// first ask that joined it.
func (q *Queue) Coalesce(ctx context.Context, key string, op Op, fn func(ctx context.Context) error) error {
	q.mu.Lock()
	c := q.coalesce[key]
	if c == nil {
		c = &coalescer{}
		q.coalesce[key] = c
	}
	var r *round
	if c.running == nil {
		r = &round{ctx: context.WithoutCancel(ctx), op: op, fn: fn, done: make(chan struct{})}
		c.running = r
		go q.runRounds(key, c, r)
	} else {
		if c.next == nil {
			c.next = &round{ctx: context.WithoutCancel(ctx), op: op, fn: fn, done: make(chan struct{})}
		}
		r = c.next
	}
	r.joined++
	q.mu.Unlock()

	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return errors.NewErr("GitQueueCancelled", ctx.Err())
	}
}

// runRounds runs the round, then the one that waited behind it, until none is left.
func (q *Queue) runRounds(key string, c *coalescer, r *round) {
	for r != nil {
		r.err = q.Do(r.ctx, r.op, r.fn)
		close(r.done)
		q.mu.Lock()
		r, c.running, c.next = c.next, c.next, nil
		if r == nil {
			delete(q.coalesce, key)
		}
		q.mu.Unlock()
	}
}
