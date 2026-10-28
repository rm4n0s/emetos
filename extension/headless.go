package extension

import "image"

// Size is a size in pixels.
type Size struct {
	// Width and Height are the pixels across and down.
	Width, Height int
}

// MaxHeadlessPixels is the area of the largest headless view, in pixels (a 4K display), and MaxHeadlessSide the longest
// side it may have.
const (
	MaxHeadlessPixels = 3840 * 2160
	MaxHeadlessSide   = 7680
)

// KeyKind is what a key did.
type KeyKind uint8

// The kinds of a key event.
const (
	// KeyDown is a key that went down.
	KeyDown KeyKind = iota
	// KeyUp is a key that was released.
	KeyUp
	// KeyRepeat is a key that is held down and repeats.
	KeyRepeat
)

// KeyEvent is a key event of a headless view, with the text it produced, so that the layout and the input method of the
// keyboard it came from decide what is typed.
type KeyEvent struct {
	// Kind is whether the key went down, up or repeats.
	Kind KeyKind
	// Physical is the USB HID usage of the key (Glutter's PhysicalKeyboardKey), which does not depend on the layout.
	Physical uint64
	// Logical is the ID of the key (Glutter's LogicalKeyboardKey), which does.
	Logical uint64
	// Text is what the key typed; empty for a release or a key that types nothing.
	Text string
}

// PointerKind is what the pointer did.
type PointerKind uint8

// The kinds of a pointer event.
const (
	// PointerMove is a pointer that moved, with or without buttons down.
	PointerMove PointerKind = iota
	// PointerDown is a button that was pressed.
	PointerDown
	// PointerUp is a button that was released.
	PointerUp
)

// The bits of PointerEvent.Buttons.
const (
	// ButtonPrimary is the left button.
	ButtonPrimary uint8 = 1 << iota
	// ButtonSecondary is the right button.
	ButtonSecondary
	// ButtonMiddle is the wheel button.
	ButtonMiddle
)

// PointerEvent is a mouse event of a headless view, at a position in the pixels of the view.
type PointerEvent struct {
	// Kind is whether the pointer moved or a button went down or up.
	Kind PointerKind
	// Buttons are the buttons that are down after the event, as bits of the Button constants.
	Buttons uint8
	// X and Y are the position of the pointer.
	X, Y float64
}

// ScrollEvent is a scroll of the wheel or the trackpad over a headless view.
type ScrollEvent struct {
	// X and Y are the position of the pointer.
	X, Y float64
	// DX and DY are the distance scrolled, in pixels, positive towards the right and the bottom.
	DX, DY float64
}

// NotificationSeverity is how serious a notification of the workspace is.
type NotificationSeverity uint8

// The severities of ViewNotification.
const (
	// NotificationInfo is for news the user may like to know.
	NotificationInfo NotificationSeverity = iota
	// NotificationWarning is for something that may not work as the user expects.
	NotificationWarning
	// NotificationError is for a failure.
	NotificationError
)

// ViewNotification is a notification of the workspace as a headless view hands it over to its client (OnNotification).
type ViewNotification struct {
	// ID identifies the notification for RunNotificationAction; it is not the same in another run of Emetos.
	ID uint64
	// Severity says how serious it is, Source which part of Emetos raised it ("Git") and Message what it says.
	Severity NotificationSeverity
	Source   string
	Message  string
	// Actions are the labels of the buttons, in order; RunNotificationAction takes the place in this list.
	Actions []string
}

// HeadlessView is a second view of the workspace, drawn off screen at a size of its own: a workbench with its own
// focus, cursors and scroll positions, its own side bar, panel and quick input, over the same documents, so that an edit in
// one view shows in the other. Emetos draws it with Glutter's software renderer when something in it changes (at most 30
// times a second; the UI thread only builds and paints it, the drawing is on a goroutine of the view) and hands the pictures
// to the extension; the extension hands the input of its user back. Remote Work
// (SPEC §10.18) is made of it.
//
// A view keeps the commands of the workspace but not the contributions of the extensions to it (their views, menu
// items and status bar items), and none of the commands that act on Emetos itself: quitting, the window tabs, other
// workspaces. Its methods may be called from any goroutine.
type HeadlessView interface {
	// Size is the size of the view in pixels.
	Size() Size
	// OnFrame sets the function that gets the pictures of the view: one when the view is made, and one each time the view
	// changed. fn runs on a goroutine of the view, never on the UI thread and never twice at once, so it may encode and
	// send; a view whose pictures are not taken fast enough drops the ones in between, the latest is always the next. The
	// image belongs to fn. It is called until Close: a call that is running when Close is called finishes (Close waits for it,
	// at most a quarter of a second, so that a function that closes the view itself does not wait for itself), and none starts
	// after Close has returned.
	OnFrame(fn func(frame *image.RGBA))
	// Refresh draws the view again, even if nothing changed, and hands the picture over: the client lost its picture.
	Refresh()
	// Resize gives the view a new size. It fails with ExtensionHeadlessSize for a size that is not allowed.
	Resize(size Size) error
	// SendKey delivers a key event to the view as the platform would: through the focus and the key handling of Glutter,
	// so the editor, the menus and the dialogs of the view take it.
	SendKey(event KeyEvent)
	// SendPointer delivers a mouse event, and SendScroll a scroll.
	SendPointer(event PointerEvent)
	SendScroll(event ScrollEvent)
	// SetClipboard sets the clipboard of the view, the text a paste in the view takes (the client's clipboard).
	SetClipboard(text string)
	// OnCopy sets the function that gets the text the user copies or cuts in the view. It runs on the UI thread: hand the
	// text to a goroutine and return.
	OnCopy(fn func(text string))
	// OnNotification sets the function that gets the notifications the workspace raises from now on (the ones that would
	// show as a toast: a silent one, made while do not disturb is on, is left out). While a function is set the view draws
	// no toast for them, so the client shows them in its own way, and the buttons they carry are pressed with
	// RunNotificationAction. fn runs on the UI thread: hand the notification to a goroutine and return. nil takes the
	// function away, and the view draws the toasts again.
	OnNotification(fn func(n ViewNotification))
	// RunNotificationAction presses the button of a notification as its user would in the view: the action runs, in the
	// workspace, and the notification closes unless the action keeps it open. It does nothing for a notification that is gone
	// or a button that is not there.
	RunNotificationAction(id uint64, action int)
	// Done is closed when the view ended, whether the extension closed it or Emetos did (the workspace closed, or it opened
	// another folder): its widgets are gone and it holds no document. Pictures stop coming before that.
	Done() <-chan struct{}
	// Close ends the view and frees what it holds: its widgets, its pictures and the editors it opened, which release their
	// documents. It does nothing after the first call. The activation ending closes the views it made.
	Close()
}
