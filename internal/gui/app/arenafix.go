package app

import (
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/widgets"
)

// microtasks runs the functions that the gesture arena schedules "after the work that is running now" (Dart's
// scheduleMicrotask) before the next pointer event is dispatched, and otherwise on the next turn of the event loop, as
// Glutter's own scheduler does.
type microtasks struct {
	post   func(func())
	queue  []func()
	posted bool
}

// schedule queues fn and, when no turn of the loop is waiting to run the queue, posts one.
func (m *microtasks) schedule(fn func()) {
	m.queue = append(m.queue, fn)
	if !m.posted {
		m.posted = true
		m.post(m.drainPosted)
	}
}

// drainPosted is the turn of the event loop that runs the queue.
func (m *microtasks) drainPosted() {
	m.posted = false
	m.drain()
}

// drain runs the queued functions, including those that they queue.
func (m *microtasks) drain() {
	for len(m.queue) > 0 {
		fn := m.queue[0]
		m.queue[0] = nil
		m.queue = m.queue[1:]
		fn()
	}
}

// fixGestureArena makes the arena of the binding resolve its default winner before the next pointer event, as Flutter
// does, instead of on the next turn of the event loop. It runs on the UI thread, before the first pointer event.
//
// Glutter: GestureArenaManager schedules the resolution of an arena whose last member stays (tryToResolveArena ->
// resolveByDefault) with PlatformDispatcher.Post, which runs on the next turn of the event loop, and the Linux platform
// gives every mouse event the pointer id 0. When a frame is slow, the events that waited for it (a press, a move past the
// slop, a release and a second press) are dispatched in one go, before that turn: the member of the first arena rejects
// itself (it is a tap that the mouse moved away from), the arena is deleted, the second press opens a new arena for pointer
// 0, and the turn then finds that arena in the manager's map and resolves it with the members of the old one:
// `index out of range [0] with length 0` at gestures/arena.go:309, which ends the process. Flutter's microtask runs before
// the next event and its _resolveByDefault asserts that the arena in the map is the one it was scheduled for. Glutter
// should check, in resolveByDefault, that m.arenas[pointer] is still state and that state has a member, and its scheduler
// of microtasks should run before the next pointer event (docs/GLUTTER-ISSUES.md).
func fixGestureArena(binding *widgets.WidgetsBinding) {
	g := binding.Gestures()
	m := &microtasks{post: binding.Post}
	g.GestureArena = gestures.NewGestureArenaManager(m.schedule, gestures.GestureArenaManagerOpts{Debug: g.Debug})
	prev := g.OnDispatchEvent
	g.OnDispatchEvent = func(event gestures.PointerEvent, hit *gestures.HitTestResult) {
		m.drain() // before the event can open an arena for a pointer id that an arena still waits on
		if prev != nil {
			prev(event, hit)
		}
	}
}
