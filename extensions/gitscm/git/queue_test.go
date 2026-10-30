package git

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// waiting returns how many operations of the queue wait and how many run.
func (q *Queue) counts() (waiting, active int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.waiting), len(q.active)
}

// waitCounts waits until the queue has the given numbers of waiting and running operations.
func waitCounts(t *testing.T, q *Queue, waiting, active int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if w, a := q.counts(); w == waiting && a == active {
			return
		}
		time.Sleep(time.Millisecond)
	}
	w, a := q.counts()
	t.Fatalf("the queue has %d waiting and %d running, want %d and %d", w, a, waiting, active)
}

// never fails the test when the channel receives within a short time.
func never(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s", what)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestReadsRunInParallelUpToFour(t *testing.T) {
	q := NewQueue(0)
	release := make(chan struct{})
	started := make(chan struct{}, 20)
	var running, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := q.Do(context.Background(), Op{Scope: ScopeIndex}, func(context.Context) error {
				n := running.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				started <- struct{}{}
				<-release
				running.Add(-1)
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		<-started
	}
	never(t, started, "a fifth read started while four were running")
	waitCounts(t, q, 6, 4)
	close(release)
	wg.Wait()
	if peak.Load() != 4 {
		t.Errorf("the most reads at the same time was %d, want 4", peak.Load())
	}
	if w, a := q.counts(); w != 0 || a != 0 {
		t.Errorf("the queue is not empty: %d waiting, %d running", w, a)
	}
}

func TestTheNumberOfParallelReadsIsConfigurable(t *testing.T) {
	q := NewQueue(2)
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = q.Do(context.Background(), Op{}, func(context.Context) error { started <- struct{}{}; <-release; return nil })
		}()
	}
	<-started
	<-started
	never(t, started, "a third read started")
	close(release)
	wg.Wait()
}

func TestWritesRunOneAtATimeInTheOrderTheyWereAsked(t *testing.T) {
	q := NewQueue(0)
	var mu sync.Mutex
	var order []int
	var running, peak int
	gate := make(chan struct{})
	var wg sync.WaitGroup
	const writes = 8
	for i := 0; i < writes; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			err := q.Do(context.Background(), Op{Write: true, Scope: ScopeAll}, func(context.Context) error {
				mu.Lock()
				running++
				if running > peak {
					peak = running
				}
				order = append(order, i)
				mu.Unlock()
				if i == 0 {
					<-gate
				}
				mu.Lock()
				running--
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
		// The next write is asked after this one is in the queue: the order of the asks is the order of i.
		waitCounts(t, q, i, 1)
		if i == 0 {
			continue
		}
	}
	close(gate)
	wg.Wait()
	for i, got := range order {
		if got != i {
			t.Fatalf("the writes ran in the order %v", order)
		}
	}
	if peak != 1 {
		t.Errorf("%d writes ran at the same time", peak)
	}
}

func TestAReadWaitsForTheEarlierWritesThatChangeWhatItReads(t *testing.T) {
	q := NewQueue(0)
	release := make(chan struct{})
	writeStarted := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = q.Do(context.Background(), Op{Write: true, Scope: ScopeIndex | ScopeWorkTree}, func(context.Context) error {
			close(writeStarted)
			<-release
			return nil
		})
	}()
	<-writeStarted

	run := func(scope Scope) (done <-chan struct{}) {
		ch := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = q.Do(context.Background(), Op{Scope: scope}, func(context.Context) error { close(ch); return nil })
		}()
		return ch
	}
	unrelated := run(ScopeRefs) // a read of what the write does not change runs at once
	select {
	case <-unrelated:
	case <-time.After(20 * time.Second):
		t.Fatal("a read of refs waited for a write of the index")
	}
	related := run(ScopeIndex)
	everything := run(0) // zero is ScopeAll
	never(t, related, "a read of the index ran during a write of the index")
	never(t, everything, "a read of everything ran during a write of the index")
	close(release)
	for _, ch := range []<-chan struct{}{related, everything} {
		select {
		case <-ch:
		case <-time.After(20 * time.Second):
			t.Fatal("a read did not run after the write")
		}
	}
	wg.Wait()
}

func TestAReadIsNotHeldBackByAWriteThatCameAfterIt(t *testing.T) {
	q := NewQueue(0)
	readRelease, writeRelease := make(chan struct{}), make(chan struct{})
	readStarted, writeStarted := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = q.Do(context.Background(), Op{Scope: ScopeIndex}, func(context.Context) error { close(readStarted); <-readRelease; return nil })
	}()
	<-readStarted
	go func() {
		defer wg.Done()
		_ = q.Do(context.Background(), Op{Write: true, Scope: ScopeIndex}, func(context.Context) error { close(writeStarted); <-writeRelease; return nil })
	}()
	// A write does not wait for a read that is running: git's own locks keep the repository sound.
	select {
	case <-writeStarted:
	case <-time.After(20 * time.Second):
		t.Fatal("the write waited for a running read")
	}
	close(readRelease)
	close(writeRelease)
	wg.Wait()
}

func TestAWriteThatWaitsKeepsLaterReadsOfItsScopeBehindIt(t *testing.T) {
	q := NewQueue(0)
	w1Release := make(chan struct{})
	w1Started := make(chan struct{})
	var wg sync.WaitGroup
	order := make(chan string, 4)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = q.Do(context.Background(), Op{Write: true, Scope: ScopeRefs}, func(context.Context) error { close(w1Started); <-w1Release; order <- "w1"; return nil })
	}()
	<-w1Started
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = q.Do(context.Background(), Op{Write: true, Scope: ScopeIndex}, func(context.Context) error { order <- "w2"; return nil })
	}()
	waitCounts(t, q, 1, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = q.Do(context.Background(), Op{Scope: ScopeIndex}, func(context.Context) error { order <- "read of the index"; return nil })
	}()
	waitCounts(t, q, 2, 1)
	close(w1Release)
	wg.Wait()
	close(order)
	var got []string
	for s := range order {
		got = append(got, s)
	}
	if len(got) != 3 || got[0] != "w1" || got[1] != "w2" || got[2] != "read of the index" {
		t.Errorf("order = %v", got)
	}
}

func TestACancelledWaitLeavesTheQueueAndRunsNothing(t *testing.T) {
	q := NewQueue(0)
	release := make(chan struct{})
	started := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = q.Do(context.Background(), Op{Write: true}, func(context.Context) error { close(started); <-release; return nil })
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	var ran atomic.Bool
	errc := make(chan error, 1)
	go func() {
		errc <- q.Do(ctx, Op{Write: true}, func(context.Context) error { ran.Store(true); return nil })
	}()
	waitCounts(t, q, 1, 1)
	cancel()
	err := <-errc
	appErr := routeOf(t, err, "Queue.Do.GitQueueCancelled")
	if appErr != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("the cancellation is not the cause: %v", err)
	}
	waitCounts(t, q, 0, 1)
	close(release)
	wg.Wait()
	if ran.Load() {
		t.Error("the cancelled operation ran")
	}
	// The queue still works.
	if err := q.Do(context.Background(), Op{Write: true}, func(context.Context) error { return nil }); err != nil {
		t.Errorf("after a cancellation: %v", err)
	}
}

func TestTheErrorOfAnOperationComesBackUntouched(t *testing.T) {
	q := NewQueue(0)
	sentinel := &errors.Error{Tag: "TestFailed"}
	if err := q.Do(context.Background(), Op{}, func(context.Context) error { return sentinel }); err != error(sentinel) {
		t.Errorf("err = %v", err)
	}
	if w, a := q.counts(); w != 0 || a != 0 {
		t.Errorf("%d waiting, %d running after the failure", w, a)
	}
}

func TestAPanicGivesTheSlotBack(t *testing.T) {
	q := NewQueue(0)
	func() {
		defer func() { _ = recover() }()
		_ = q.Do(context.Background(), Op{Write: true}, func(context.Context) error { panic("boom") })
	}()
	done := make(chan struct{})
	go func() {
		_ = q.Do(context.Background(), Op{Write: true}, func(context.Context) error { close(done); return nil })
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the slot of the panicking write was not given back")
	}
}

// Many asks while a refresh runs are one more refresh (SPEC §10.30 "Queue").
func TestARefreshIsCoalesced(t *testing.T) {
	q := NewQueue(0)
	var runs atomic.Int32
	gate := make(chan struct{})
	firstStarted := make(chan struct{})
	fn := func(context.Context) error {
		n := runs.Add(1)
		if n == 1 {
			close(firstStarted)
			<-gate
		}
		if n == 2 {
			return &errors.Error{Tag: "SecondRunFailed"}
		}
		return nil
	}
	errs := make(chan error, 11)
	go func() { errs <- q.Coalesce(context.Background(), "refresh", Op{Scope: ScopeAll}, fn) }()
	<-firstStarted
	for i := 0; i < 10; i++ {
		go func() { errs <- q.Coalesce(context.Background(), "refresh", Op{Scope: ScopeAll}, fn) }()
	}
	// Wait until the ten have joined the run that waits behind the first.
	deadline := time.Now().Add(20 * time.Second)
	for {
		q.mu.Lock()
		joined := 0
		if c := q.coalesce["refresh"]; c != nil && c.next != nil {
			joined = c.next.joined
		}
		q.mu.Unlock()
		if joined == 10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d callers joined the next run, want 10", joined)
		}
		time.Sleep(time.Millisecond)
	}
	close(gate)
	var failed, ok int
	for i := 0; i < 11; i++ {
		select {
		case err := <-errs:
			if err == nil {
				ok++
			} else if e, isErr := errors.FromError(err); isErr && e.Tag == "SecondRunFailed" {
				failed++
			} else {
				t.Errorf("unexpected error %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("a caller did not return")
		}
	}
	if runs.Load() != 2 || ok != 1 || failed != 10 {
		t.Errorf("%d runs; %d callers got the first run's result and %d the second's; want 2 runs, 1 and 10", runs.Load(), ok, failed)
	}
	q.mu.Lock()
	left := len(q.coalesce)
	q.mu.Unlock()
	if left != 0 {
		t.Errorf("%d coalesced keys are left", left)
	}
	// A new ask after it all starts a new run.
	if err := q.Coalesce(context.Background(), "refresh", Op{}, fn); err != nil || runs.Load() != 3 {
		t.Errorf("a later ask: %v, %d runs", err, runs.Load())
	}
}

func TestCoalescedKeysAreIndependentAndACallerCanLeave(t *testing.T) {
	q := NewQueue(0)
	gate := make(chan struct{})
	started := make(chan struct{}, 2)
	var runs atomic.Int32
	fn := func(context.Context) error { runs.Add(1); started <- struct{}{}; <-gate; return nil }
	errs := make(chan error, 2)
	go func() { errs <- q.Coalesce(context.Background(), "a", Op{}, fn) }()
	go func() { errs <- q.Coalesce(context.Background(), "b", Op{}, fn) }()
	<-started
	<-started // two keys run at the same time

	ctx, cancel := context.WithCancel(context.Background())
	leaver := make(chan error, 1)
	go func() { leaver <- q.Coalesce(ctx, "a", Op{}, fn) }()
	deadline := time.Now().Add(20 * time.Second)
	for {
		q.mu.Lock()
		joined := 0
		if c := q.coalesce["a"]; c != nil && c.next != nil {
			joined = c.next.joined
		}
		q.mu.Unlock()
		if joined == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the caller did not join")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	wantRoute(t, <-leaver, "Queue.Coalesce.GitQueueCancelled")
	close(gate)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	// The run the caller left still happens (it was asked for); wait for it.
	select {
	case <-started:
	case <-time.After(20 * time.Second):
		t.Fatal("the run of the caller who left did not happen")
	}
}
