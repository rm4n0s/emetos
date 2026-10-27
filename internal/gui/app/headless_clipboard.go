package app

import (
	"context"
	"sync"

	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// viewClipboard is the clipboard of a headless view (SPEC §10.18): what the user of the view copied and what its client
// sent as its own clipboard, kept in the view, never on the clipboard of the desktop of the host.
//
// Glutter: an offscreen.View has no clipboard of its own: its calls (every widget's, Glutter's EditableText included: copy, cut,
// paste, the primary selection, the check whether there is something to paste) go to the clipboard of the parent binding it was
// given. A view that a client of another machine types in must neither read the clipboard or the primary selection of the host
// nor replace them, and the client must hear of the text its user copies. So the parent of the view is a binding of its own that
// never draws (clipboardParent), whose platform host is this clipboard. Glutter should let ViewOpts hold a ui.ClipboardHost
// (docs/GLUTTER-ISSUES.md).
type viewClipboard struct {
	// post runs a function on the UI thread, later: Glutter's clipboard calls done "never before the method has returned".
	post func(func())

	mu      sync.Mutex
	system  ui.ClipboardContent
	primary ui.ClipboardContent
	onCopy  func(text string)
}

// newViewClipboard makes the clipboard of a view; main is the binding whose UI thread runs the view.
func newViewClipboard(main *widgets.WidgetsBinding) *viewClipboard {
	return &viewClipboard{post: main.Post}
}

var (
	_ ui.ClipboardHost     = (*viewClipboard)(nil)
	_ ui.RichClipboardHost = (*viewClipboard)(nil)
)

// clipboardHost is the platform host of the parent binding of a view: the clipboard of the view, and nothing else (the
// parent never makes a frame).
type clipboardHost struct {
	*viewClipboard
	main *widgets.WidgetsBinding
}

// ScheduleFrame implements ui.PlatformHost.
func (clipboardHost) ScheduleFrame() {}

// RenderView implements ui.PlatformHost.
func (clipboardHost) RenderView(ui.ViewID, *ui.Scene) error { return nil }

// RequestAppExit implements ui.PlatformHost: a view cannot end the application.
func (clipboardHost) RequestAppExit() {}

// Post implements ui.PlatformHost: the work goes to the UI thread of the application.
func (h clipboardHost) Post(fn func()) { h.main.Post(fn) }

// newClipboardParent makes the binding that an offscreen.View is given as its parent (see viewClipboard): it shares the UI thread,
// the error reporting, the debug flags, the assets and the text engine of the application, and its clipboard is the view's. It
// runs on the UI thread. ctx ends the work of its painting binding.
func newClipboardParent(ctx context.Context, main *widgets.WidgetsBinding, clip *viewClipboard) *widgets.WidgetsBinding {
	d := ui.NewPlatformDispatcher(clipboardHost{viewClipboard: clip, main: main})
	d.SetTextEngine(main.PlatformDispatcher().TextEngine())
	pp := main.Painting()
	return widgets.NewWidgetsBinding(ctx, d, widgets.WidgetsBindingOpts{
		Debug:              main.DebugOptions(),
		ErrorReporter:      main.ErrorReporter(),
		ErrorWidgetBuilder: main.BuildOwner().ErrorWidgetBuilder,
		UIThread:           main.UIThread(),
		Painting:           painting.PaintingBindingOpts{AssetBundle: pp.AssetBundle(), HTTPClient: pp.HTTPClient()},
	})
}

// SetClipboardText implements services.ClipboardBackend.
func (c *viewClipboard) SetClipboardText(ctx context.Context, text string, done func(error)) {
	c.SetClipboard(ctx, ui.ClipboardKindSystem, ui.ClipboardContent{Text: text, HasText: true}, done)
}

// GetClipboardText implements services.ClipboardBackend.
func (c *viewClipboard) GetClipboardText(ctx context.Context, done func(string, bool, error)) {
	c.GetClipboard(ctx, ui.ClipboardKindSystem, ui.ClipboardFormatText, func(content ui.ClipboardContent, err error) {
		done(content.Text, content.HasText, err)
	})
}

// SetClipboard implements services.RichClipboardBackend. A text put on the system clipboard is handed to the function of
// setCopyHandler; the primary selection stays in the view.
func (c *viewClipboard) SetClipboard(_ context.Context, kind ui.ClipboardKind, content ui.ClipboardContent, done func(error)) {
	c.mu.Lock()
	var onCopy func(string)
	if kind == ui.ClipboardKindPrimary {
		c.primary = content
	} else {
		c.system = content
		if content.HasText && content.Text != "" {
			onCopy = c.onCopy
		}
	}
	c.mu.Unlock()
	if onCopy != nil {
		onCopy(content.Text)
	}
	if done != nil {
		c.post(func() { done(nil) })
	}
}

// GetClipboard implements services.RichClipboardBackend.
func (c *viewClipboard) GetClipboard(_ context.Context, kind ui.ClipboardKind, format string, done func(ui.ClipboardContent, error)) {
	c.mu.Lock()
	content := c.system
	if kind == ui.ClipboardKindPrimary {
		content = c.primary
	}
	c.mu.Unlock()
	if !content.Has(format) {
		content = ui.ClipboardContent{}
	}
	c.post(func() { done(content, nil) })
}

// setText replaces the system clipboard of the view with a text: the clipboard of the client. It does not tell the function of
// setCopyHandler.
func (c *viewClipboard) setText(text string) {
	c.mu.Lock()
	c.system = ui.ClipboardContent{Text: text, HasText: true}
	c.mu.Unlock()
}

// setCopyHandler sets the function that gets the text copied in the view; it is called on the goroutine that copies, which
// is the UI thread.
func (c *viewClipboard) setCopyHandler(fn func(text string)) {
	c.mu.Lock()
	c.onCopy = fn
	c.mu.Unlock()
}
