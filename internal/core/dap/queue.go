package dap

import "sync"

// queue is an unbounded FIFO of values for one consumer goroutine: pushing never blocks, so a caller on the UI
// thread is never held up by an adapter that does not read. The messages in it are small requests.
type queue[T any] struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []T
	closed bool
}

func newQueue[T any]() *queue[T] {
	q := &queue[T]{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push appends v and reports false when the queue is closed.
func (q *queue[T]) push(v T) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	q.items = append(q.items, v)
	q.cond.Signal()
	return true
}

// pop waits for the first value and reports false when the queue is closed.
func (q *queue[T]) pop() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	var zero T
	if q.closed {
		return zero, false
	}
	v := q.items[0]
	q.items[0] = zero
	q.items = q.items[1:]
	return v, true
}

// close wakes the consumer; no value can be pushed afterwards and the values that are left are dropped.
func (q *queue[T]) close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}
