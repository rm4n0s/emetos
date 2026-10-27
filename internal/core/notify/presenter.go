package notify

import (
	"slices"
	"time"
)

// What decides which notifications show as toasts (SPEC §10.7).
const (
	// MaxToasts is the most toasts on screen at once (VS Code's MAX_NOTIFICATIONS). The newest ones show;
	// an older one waits until a toast goes away.
	MaxToasts = 3
	// SpamInterval and SpamLimit protect the screen from a flood: of the notifications added within
	// SpamInterval of the first one, only SpamLimit get a toast. The others are in the center.
	SpamInterval = 800 * time.Millisecond
	// SpamLimit is the most toasts made within SpamInterval.
	SpamLimit = 3
)

// PurgeTimeout is how long a toast of a severity stays before it goes away, unless it is sticky, the
// pointer is over it or it has the focus (VS Code's PURGE_TIMEOUT).
func PurgeTimeout(s Severity) time.Duration {
	switch s {
	case Warning:
		return 12 * time.Second
	case Error:
		return 15 * time.Second
	}
	return 10 * time.Second
}

// toast is a notification that shows as a toast.
type toast struct {
	id           uint64
	stop         func()
	hover, focus bool
}

// Presenter decides what the notification UI shows: the toasts, whether the notification center is open
// and the do not disturb mode of the service it presents. It lives on the UI thread: every method must be
// called from it. The UI binds it to the thread with Bind, which also makes it follow the service.
//
// Ported from VS Code: src/vs/workbench/browser/parts/notifications/notificationsToasts.ts and
// notificationsCenter.ts (the state of both, not their DOM)
type Presenter struct {
	svc *Service
	// after calls f on the UI thread once d has passed and returns the function that cancels it; Bind
	// sets it.
	after func(d time.Duration, f func()) (stop func())
	// unsubscribe stops following the service.
	unsubscribe func()

	toasts []*toast // oldest first
	center bool
	// muted makes no toast: someone else shows the notifications (a headless view whose client does).
	muted bool

	spamStart time.Time
	spamCount int

	listeners map[int]func()
	nextL     int
}

// NewPresenter makes the presenter of a service. It shows nothing until Bind gives it the UI thread.
func NewPresenter(svc *Service) *Presenter {
	return &Presenter{svc: svc, listeners: map[int]func(){}}
}

// Service is the service the presenter shows.
func (p *Presenter) Service() *Service { return p.svc }

// Bind makes the presenter follow its service on the UI thread. post hands a function to the UI thread
// (the events of the service come from any goroutine; nil runs them at once, for a caller that is
// on the UI thread already) and after is the timer of the UI thread: it runs f there once d has passed
// and returns the function that cancels it. The notifications that are in the service already get a
// toast, as VS Code does for those that came before the window was ready. The result stops following
// and takes the toasts away.
func (p *Presenter) Bind(post func(func()), after func(d time.Duration, f func()) (stop func())) (unbind func()) {
	if p.unsubscribe != nil {
		p.unsubscribe()
	}
	if post == nil {
		post = func(f func()) { f() }
	}
	p.after = after
	p.unsubscribe = p.svc.OnChange(func(e Event) { post(func() { p.Handle(e) }) })
	for _, item := range p.svc.Items() {
		p.addToast(item)
	}
	p.changed()
	return func() {
		if p.unsubscribe != nil {
			p.unsubscribe()
			p.unsubscribe = nil
		}
		p.after = nil
		p.stopToasts()
	}
}

// Handle applies an event of the service. Bind makes the service send them here; a caller that does not
// bind calls it itself.
func (p *Presenter) Handle(e Event) {
	switch e.Kind {
	case Added:
		if _, ok := p.svc.Get(e.Item.ID); ok {
			p.addToast(e.Item)
		}
	case Dismissed:
		p.removeToast(e.Item.ID)
	case DoNotDisturbChanged:
		if e.DoNotDisturb {
			p.stopToasts() // VS Code hides every toast, even those of errors
		}
	}
	p.changed()
}

// addToast makes a toast for an item, unless the center is open, the item is silent or too many toasts
// were made lately.
func (p *Presenter) addToast(item Item) {
	if p.center || p.muted || item.Silent || p.findToast(item.ID) >= 0 {
		return
	}
	if item.Time.Sub(p.spamStart) > SpamInterval || p.spamCount == 0 {
		p.spamStart, p.spamCount = item.Time, 0
	}
	p.spamCount++
	if p.spamCount > SpamLimit {
		return
	}
	t := &toast{id: item.ID}
	p.toasts = append(p.toasts, t)
	p.arm(t, item.Severity)
}

// arm starts the timer that takes a toast away.
func (p *Presenter) arm(t *toast, severity Severity) {
	if p.after == nil {
		return
	}
	t.stop = p.after(PurgeTimeout(severity), func() { p.purge(t.id) })
}

// purge takes a toast away when its time is up, unless it must stay: then the time starts again.
func (p *Presenter) purge(id uint64) {
	i := p.findToast(id)
	if i < 0 {
		return
	}
	t := p.toasts[i]
	t.stop = nil
	if item, ok := p.svc.Get(id); ok && (item.IsSticky() || t.hover || t.focus) {
		p.arm(t, item.Severity)
		return
	}
	p.removeToast(id)
	p.changed()
}

func (p *Presenter) findToast(id uint64) int {
	return slices.IndexFunc(p.toasts, func(t *toast) bool { return t.id == id })
}

func (p *Presenter) removeToast(id uint64) {
	i := p.findToast(id)
	if i < 0 {
		return
	}
	if stop := p.toasts[i].stop; stop != nil {
		stop()
	}
	p.toasts = slices.Delete(p.toasts, i, i+1)
}

// stopToasts takes every toast away and stops their timers.
func (p *Presenter) stopToasts() {
	for _, t := range p.toasts {
		if t.stop != nil {
			t.stop()
		}
	}
	p.toasts = nil
}

// Mute stops the presenter from making toasts, and takes away those that show, or lets it make them again. The notifications
// stay in the service and in the center. A headless view whose client shows the notifications in its own way is muted
// (SPEC §10.18 "As built (step 15.4)").
func (p *Presenter) Mute(muted bool) {
	if p.muted == muted {
		return
	}
	p.muted = muted
	if muted {
		p.stopToasts()
	}
	p.changed()
}

// Toasts lists the toasts that show, the newest first: at most MaxToasts, the newest ones. A toast that
// does not show yet (it is older than MaxToasts others) comes back when a newer one goes away.
func (p *Presenter) Toasts() []Item {
	var out []Item
	for _, t := range slices.Backward(p.toasts) {
		if len(out) == MaxToasts {
			break
		}
		if item, ok := p.svc.Get(t.id); ok {
			out = append(out, item)
		}
	}
	return out
}

// ToastsVisible says whether a toast shows (the context key notificationToastsVisible).
func (p *Presenter) ToastsVisible() bool { return len(p.Toasts()) > 0 }

// HideToasts takes every toast away; the notifications stay in the center (notifications.hideToasts).
func (p *Presenter) HideToasts() {
	if len(p.toasts) == 0 {
		return
	}
	p.stopToasts()
	p.changed()
}

// SetHovered tells that the pointer is over the toast of an item (or has left it): a toast under the
// pointer does not go away.
func (p *Presenter) SetHovered(id uint64, over bool) {
	if i := p.findToast(id); i >= 0 {
		p.toasts[i].hover = over
	}
}

// SetFocused tells that the focus is in the toast of an item (or has left it): a toast that has the
// focus does not go away.
func (p *Presenter) SetFocused(id uint64, focused bool) {
	if i := p.findToast(id); i >= 0 {
		p.toasts[i].focus = focused
	}
}

// CenterVisible says whether the notification center is open (the context key
// notificationCenterVisible).
func (p *Presenter) CenterVisible() bool { return p.center }

// ShowCenter opens the notification center; the toasts go away, and no toast shows while it is open
// (notifications.showList).
func (p *Presenter) ShowCenter() {
	if p.center {
		return
	}
	p.center = true
	p.stopToasts()
	p.changed()
}

// HideCenter closes the notification center (notifications.hideList).
func (p *Presenter) HideCenter() {
	if !p.center {
		return
	}
	p.center = false
	p.changed()
}

// ToggleCenter opens the center when it is closed and closes it when it is open
// (notifications.toggleList).
func (p *Presenter) ToggleCenter() {
	if p.center {
		p.HideCenter()
	} else {
		p.ShowCenter()
	}
}

// ClearAll closes the center and dismisses every notification (notifications.clearAll).
func (p *Presenter) ClearAll() {
	p.HideCenter()
	p.svc.ClearAll()
}

// ToggleDoNotDisturb turns do not disturb on when it is off and off when it is on
// (notifications.toggleDoNotDisturbMode).
func (p *Presenter) ToggleDoNotDisturb() { p.svc.SetDoNotDisturb(!p.svc.DoNotDisturb()) }

// OnChange registers a listener that is called, on the UI thread, whenever what the UI shows changes,
// and returns the function that removes it.
func (p *Presenter) OnChange(f func()) (remove func()) {
	id := p.nextL
	p.nextL++
	p.listeners[id] = f
	return func() { delete(p.listeners, id) }
}

func (p *Presenter) changed() {
	ids := make([]int, 0, len(p.listeners))
	for id := range p.listeners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if f, ok := p.listeners[id]; ok {
			f()
		}
	}
}
