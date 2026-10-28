package app

import (
	"context"
	"sync"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// viewClipboard is the clipboard of a headless view (SPEC §10.18): what the user of the view copied and what its client
// sent as its own clipboard, kept in the view, never on the clipboard of the desktop of the host.
//
// It is the offscreen.ViewOpts.Clipboard of the view: every clipboard call of the view's tree (copy, cut, paste, the primary
// selection, the check whether there is something to paste) goes to it and none to the clipboard of the application or of the
// desktop, and the client hears of the text its user copies.
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
