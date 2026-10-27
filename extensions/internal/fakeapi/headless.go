package fakeapi

import (
	"image"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// HeadlessView is the fake of extension.HeadlessView: it keeps what the extension sent it, and a test makes it draw
// (Emit), copy (Copy) and look at the input it got.
type HeadlessView struct {
	mu        sync.Mutex
	size      extension.Size
	frame     func(*image.RGBA)
	copied    func(string)
	notified  func(extension.ViewNotification)
	pressed   []NotificationPress
	keys      []extension.KeyEvent
	pointers  []extension.PointerEvent
	scrolls   []extension.ScrollEvent
	clipboard string
	refreshes int
	closed    bool
	done      chan struct{}
}

// NewHeadlessView implements extension.Workspace: the view is a fake that a test finds with HeadlessViews. Like the real
// one it refuses a size that is not allowed.
func (w workspace) NewHeadlessView(size extension.Size) (extension.HeadlessView, error) {
	w.a.mu.Lock()
	failure := w.a.headlessErr
	w.a.mu.Unlock()
	if failure != nil {
		return nil, failure
	}
	if size.Width <= 0 || size.Height <= 0 || size.Width > extension.MaxHeadlessSide || size.Height > extension.MaxHeadlessSide ||
		size.Width*size.Height > extension.MaxHeadlessPixels {
		return nil, errors.New("ExtensionHeadlessSize", "a headless view is at most extension.MaxHeadlessSide a side and extension.MaxHeadlessPixels in all",
			"width", size.Width, "height", size.Height)
	}
	w.a.mu.Lock()
	defer w.a.mu.Unlock()
	v := &HeadlessView{size: size, done: make(chan struct{})}
	w.a.headless = append(w.a.headless, v)
	return v, nil
}

// FailHeadlessViews makes NewHeadlessView fail with err from now on; nil makes it work again.
func (a *API) FailHeadlessViews(err error) {
	a.mu.Lock()
	a.headlessErr = err
	a.mu.Unlock()
}

// HeadlessViews are the views the extension made, in order.
func (a *API) HeadlessViews() []*HeadlessView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*HeadlessView(nil), a.headless...)
}

// Size implements extension.HeadlessView.
func (v *HeadlessView) Size() extension.Size {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.size
}

// OnFrame implements extension.HeadlessView.
func (v *HeadlessView) OnFrame(fn func(*image.RGBA)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.frame = fn
}

// Refresh implements extension.HeadlessView: the call is counted (Refreshes).
func (v *HeadlessView) Refresh() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refreshes++
}

// Resize implements extension.HeadlessView.
func (v *HeadlessView) Resize(size extension.Size) error {
	if size.Width <= 0 || size.Height <= 0 || size.Width > extension.MaxHeadlessSide || size.Height > extension.MaxHeadlessSide ||
		size.Width*size.Height > extension.MaxHeadlessPixels {
		return errors.New("ExtensionHeadlessSize", "a headless view is at most extension.MaxHeadlessSide a side and extension.MaxHeadlessPixels in all",
			"width", size.Width, "height", size.Height)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.size = size
	return nil
}

// SendKey implements extension.HeadlessView: the event is kept (Keys).
func (v *HeadlessView) SendKey(e extension.KeyEvent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.keys = append(v.keys, e)
}

// SendPointer implements extension.HeadlessView: the event is kept (Pointers).
func (v *HeadlessView) SendPointer(e extension.PointerEvent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.pointers = append(v.pointers, e)
}

// SendScroll implements extension.HeadlessView: the event is kept (Scrolls).
func (v *HeadlessView) SendScroll(e extension.ScrollEvent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.scrolls = append(v.scrolls, e)
}

// SetClipboard implements extension.HeadlessView: the text is kept (Clipboard).
func (v *HeadlessView) SetClipboard(text string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.clipboard = text
}

// OnCopy implements extension.HeadlessView.
func (v *HeadlessView) OnCopy(fn func(string)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.copied = fn
}

// NotificationPress is a call of RunNotificationAction.
type NotificationPress struct {
	// ID and Action are the notification and the place of the button.
	ID     uint64
	Action int
}

// OnNotification implements extension.HeadlessView.
func (v *HeadlessView) OnNotification(fn func(extension.ViewNotification)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.notified = fn
}

// RunNotificationAction implements extension.HeadlessView: the call is kept (Presses).
func (v *HeadlessView) RunNotificationAction(id uint64, action int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.pressed = append(v.pressed, NotificationPress{ID: id, Action: action})
}

// Notify tells the function of OnNotification that the workspace raised a notification; false when there is none or the view
// is closed.
func (v *HeadlessView) Notify(n extension.ViewNotification) bool {
	v.mu.Lock()
	fn, closed := v.notified, v.closed
	v.mu.Unlock()
	if fn == nil || closed {
		return false
	}
	fn(n)
	return true
}

// Presses are the calls of RunNotificationAction, in order.
func (v *HeadlessView) Presses() []NotificationPress {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]NotificationPress(nil), v.pressed...)
}

// Done implements extension.HeadlessView.
func (v *HeadlessView) Done() <-chan struct{} { return v.done }

// Close implements extension.HeadlessView.
func (v *HeadlessView) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.closed {
		v.closed = true
		close(v.done)
	}
}

// Emit hands a picture to the function of OnFrame, as the view does when it changed; false when there is none or the
// view is closed.
func (v *HeadlessView) Emit(img *image.RGBA) bool {
	v.mu.Lock()
	fn, closed := v.frame, v.closed
	v.mu.Unlock()
	if fn == nil || closed {
		return false
	}
	fn(img)
	return true
}

// Copy tells the function of OnCopy that the user copied the text in the view.
func (v *HeadlessView) Copy(text string) bool {
	v.mu.Lock()
	fn, closed := v.copied, v.closed
	v.mu.Unlock()
	if fn == nil || closed {
		return false
	}
	fn(text)
	return true
}

// Keys are the key events the extension sent, in order.
func (v *HeadlessView) Keys() []extension.KeyEvent {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]extension.KeyEvent(nil), v.keys...)
}

// Pointers are the mouse events the extension sent.
func (v *HeadlessView) Pointers() []extension.PointerEvent {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]extension.PointerEvent(nil), v.pointers...)
}

// Scrolls are the scroll events the extension sent.
func (v *HeadlessView) Scrolls() []extension.ScrollEvent {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]extension.ScrollEvent(nil), v.scrolls...)
}

// Clipboard is the text the extension last set with SetClipboard.
func (v *HeadlessView) Clipboard() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.clipboard
}

// Refreshes is how many times the extension called Refresh.
func (v *HeadlessView) Refreshes() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.refreshes
}

// Closed tells whether the extension closed the view.
func (v *HeadlessView) Closed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.closed
}
