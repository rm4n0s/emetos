package app

import (
	"context"
	"image"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/offscreen"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/notify"
)

// The headless view of a workspace (SPEC §10.18 "As built (Glutter 1.9.1)"; extension.HeadlessView): a second workbench of
// the same workspace, drawn off screen at a size of its own. It is a workspace of its own over the documents of the parent
// (newViewWorkspace) whose shell runs in an offscreen.View of Glutter, so it has its own focus, cursors, scroll positions,
// side bar, panel, quick input and dialogs, and an edit in one view is in the other. The UI thread of the application
// builds, lays out and paints the tree; the view draws the scene on a goroutine of its own and hands the pixels to the
// function of OnFrame. Input goes in through the dispatcher of the binding of the view, as the platform delivers it.

// headlessFrameRate is the most frames a headless view makes in a second (SPEC §10.18).
const headlessFrameRate = 30

// headlessView is the extension.HeadlessView of one view.
type headlessView struct {
	parent *Workspace
	ws     *Workspace
	ov     *offscreen.View
	cancel context.CancelFunc
	// main is the binding of the application, whose UI thread runs the view.
	main *widgets.WidgetsBinding
	// start is when the view was made: the clock of the time stamps of its input.
	start time.Time
	// clip is the clipboard of the view.
	clip *viewClipboard

	// ptr is the state of the pointer; UI thread only
	ptr headlessPointer
	// stopNotify stops the follow of the notifications of the workspace (OnNotification); UI thread only
	stopNotify func()
	// ended is set when the view is closed, and read by the input; UI thread only
	ended bool
	once  sync.Once
	done  chan struct{}

	// frames counts the pictures the view made.
	frames atomic.Int64

	// callMu is held while a picture is handed to the function of OnFrame, so that it is never called twice at once and a
	// Refresh cannot hand over an older picture than a frame that is on its way.
	callMu sync.Mutex
	mu     sync.Mutex
	size   extension.Size
	// onFrame is the function of OnFrame; last is the latest picture (what Refresh hands over again, and what waits for a
	// function that is not set yet); both are guarded by mu
	onFrame func(*image.RGBA)
	last    *image.RGBA
	waiting bool
	closed  bool
}

// newHeadlessView makes a headless view of the workspace: the workspace of the view, its shell with the editors the parent
// shows, and the offscreen view. It runs on the UI thread. It is the NewHeadlessView of the scope of the workspace.
//
// The error has the tag ExtensionHeadlessUnavailable.
func (w *Workspace) newHeadlessView(size extension.Size) (extension.HeadlessView, error) {
	v, err := w.makeHeadlessView(size, headlessFrameRate)
	if err != nil {
		return nil, err // not a *headlessView in an interface that would be non-nil
	}
	return v, nil
}

// makeHeadlessView is newHeadlessView with the most frames a second given: the tests make them as fast as they can.
//
// The error has the tag ExtensionHeadlessUnavailable, or one of offscreen.NewView (ViewSizeInvalid for a size it cannot draw).
func (w *Workspace) makeHeadlessView(size extension.Size, frameRate int) (*headlessView, error) {
	s := w.shell
	if w.isView() || w.ext != nil || s == nil || !s.Mounted() {
		return nil, errors.New("ExtensionHeadlessUnavailable", "the workspace is not on screen, so it has nothing to show in a second view", "folder", w.Folder)
	}
	main := widgets.WidgetsBindingOf(s.Context())
	ctx, cancel := context.WithCancel(w.ctx)
	v := &headlessView{
		parent: w, main: main, cancel: cancel, size: size, start: time.Now(), done: make(chan struct{}),
		clip: newViewClipboard(main),
	}
	v.ws = w.newViewWorkspace(ctx)
	docs := s.openDocuments()
	root := newViewSetup(v.configure, newAppWithPage(v.ws, func() widgets.Widget { return newShell(ctx, docs, nil) }))
	ov, err := offscreen.NewView(newClipboardParent(ctx, main, v.clip), ui.Size{Width: float64(size.Width), Height: float64(size.Height)}, root, v.frame,
		offscreen.ViewOpts{DevicePixelRatio: 1, MaxFrameRate: frameRate})
	if err != nil {
		cancel()
		v.ws.releaseView()
		return nil, err // an error of Glutter's own package: its tag and route stay
	}
	v.ov = ov
	w.views = append(w.views, v)
	// the view goes with the folder it shows
	stop := w.OnFolderChange(func() { v.Close() })
	v.ws.stops = append(v.ws.stops, stop)
	return v, nil
}

// viewSetup is the root of a headless view. Glutter mounts the tree as the view is made, so what the shell reads when it
// starts (the fonts, the window it is in) is set by the state of this widget, which starts first.
type viewSetup struct {
	widgets.StatefulWidget
	configure func(*ui.PlatformDispatcher)
	child     widgets.Widget
}

func newViewSetup(configure func(*ui.PlatformDispatcher), child widgets.Widget) *viewSetup {
	return &viewSetup{StatefulWidget: widgets.NewStatefulWidgetBase(), configure: configure, child: child}
}

// CreateState implements widgets.StatefulWidget.
func (*viewSetup) CreateState() widgets.State { return &viewSetupState{} }

type viewSetupState struct{ widgets.StateOf[*viewSetup] }

// InitState implements widgets.State.
func (s *viewSetupState) InitState() {
	fixGestureArena(widgets.WidgetsBindingOf(s.Context()))
	s.Widget().configure(widgets.WidgetsBindingOf(s.Context()).PlatformDispatcher())
}

// Build implements widgets.State.
func (s *viewSetupState) Build(widgets.BuildContext) widgets.Widget { return s.Widget().child }

// configure gives the dispatcher of the view the configuration of the application, so that a headless view looks like the
// window: the fonts, the locale, the text scale and the brightness; and the window it is in. It runs on the UI thread, when
// the tree of the view is mounted.
//
// Glutter: an offscreen.View starts with the default platform configuration and a window that is not activated, and
// View.SetSize replaces the window with a blank one again (windowInfo is applied again after it). Glutter should copy the
// parent's platform configuration and take the window of the view in ViewOpts (docs/GLUTTER-ISSUES.md).
func (v *headlessView) configure(d *ui.PlatformDispatcher) {
	md := v.main.PlatformDispatcher()
	uiFamily, uiSize := md.UIFont()
	monoFamily, monoSize := md.MonospaceFont()
	d.UpdatePlatformConfiguration(ui.PlatformConfiguration{
		Locales: md.Locales(), TextScaleFactor: md.TextScaleFactor(), PlatformBrightness: md.PlatformBrightness(),
		UIFontFamily: uiFamily, UIFontSize: uiSize, MonospaceFontFamily: monoFamily, MonospaceFontSize: monoSize,
		// the caret of a text field of a headless view does not blink: a blink is a picture twice a second for a client that
		// sees none of it (the caret of the editors is solid too, see editors.go)
		CaretBlinkTime: ui.CaretBlinkNever,
	})
	v.applyWindow(d)
}

// applyWindow tells the view that it is a window without a title bar that is maximized and has the focus: nothing can move,
// minimize or close it, and the menu bar has a row of its own. It runs on the UI thread.
func (v *headlessView) applyWindow(d *ui.PlatformDispatcher) {
	view := d.ImplicitView()
	if view == nil {
		return
	}
	d.UpdateViewConfiguration(ui.ImplicitViewID, ui.ViewConfiguration{
		PhysicalSize: view.PhysicalSize(), DevicePixelRatio: 1,
		Window: ui.WindowInfo{Decoration: ui.DecorationNone, Activated: true, Maximized: true},
	})
}

// dispatcher is the dispatcher of the binding of the view, the way in of its input. UI thread only.
func (v *headlessView) dispatcher() *ui.PlatformDispatcher {
	return v.ov.Binding().PlatformDispatcher()
}

// openDocuments are the documents the editors of the active group show, the one in front first: what a headless view starts
// with. It runs on the UI thread.
func (s *shellState) openDocuments() []*document.Document {
	if s.group.grid == nil {
		return nil
	}
	group := s.activeGroup()
	var docs []*document.Document
	for _, e := range group.Model.Editors(editorgroup.Sequential, false) {
		if t, ok := e.(*editorgroup.TextInput); ok {
			docs = append(docs, t.Document())
		}
	}
	if active, ok := group.Model.ActiveEditor().(*editorgroup.TextInput); ok {
		if i := slices.Index(docs, active.Document()); i > 0 {
			docs[0], docs[i] = docs[i], docs[0]
		}
	}
	return docs
}

// frame is the function of the offscreen view, called on the goroutine of the view with the pixels of each frame that changed
// some. The pixels are the view's own and are drawn into again when the call returns, so the picture is copied; the function of
// OnFrame gets the copy. A function that is slow keeps the view from drawing the next frame, which it replaces by a newer one:
// the view drops the pictures in between.
func (v *headlessView) frame(f offscreen.Frame) {
	v.mu.Lock()
	closed := v.closed
	v.mu.Unlock()
	if closed {
		return
	}
	img := &image.RGBA{Pix: slices.Clone(f.Image.Pix), Stride: f.Image.Stride, Rect: f.Image.Rect}
	v.frames.Add(1)
	v.callMu.Lock()
	defer v.callMu.Unlock()
	v.mu.Lock()
	fn, closed := v.onFrame, v.closed
	if closed {
		v.mu.Unlock()
		return
	}
	v.last, v.waiting = img, fn == nil
	v.mu.Unlock()
	if fn != nil {
		fn(img)
	}
}

// onUI runs f on the UI thread: now when the caller is on it, else posted to the main binding.
func (v *headlessView) onUI(f func()) {
	if v.main.UIThread().IsCurrent() {
		f()
		return
	}
	v.main.Post(f)
}

// Size implements extension.HeadlessView.
func (v *headlessView) Size() extension.Size {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.size
}

// OnFrame implements extension.HeadlessView: the picture that was drawn before the function was set is handed over at
// once.
func (v *headlessView) OnFrame(fn func(*image.RGBA)) {
	v.mu.Lock()
	v.onFrame = fn
	waiting := fn != nil && v.waiting
	v.mu.Unlock()
	if waiting {
		go v.handOver(false)
	}
}

// handOver gives the latest picture to the function of OnFrame, on the goroutine that calls it. A picture is handed over by
// the frame that made it, so this one is for the picture that waited for the function and for Refresh (again = true).
func (v *headlessView) handOver(again bool) {
	v.callMu.Lock()
	defer v.callMu.Unlock()
	v.mu.Lock()
	fn, img, closed := v.onFrame, v.last, v.closed
	if !again && !v.waiting {
		img = nil // a frame handed it over meanwhile
	}
	v.waiting = false
	v.mu.Unlock()
	if fn != nil && img != nil && !closed {
		fn(img)
	}
}

// Refresh implements extension.HeadlessView: the latest picture is handed over again (nothing changed since it was made).
func (v *headlessView) Refresh() { go v.handOver(true) }

// Resize implements extension.HeadlessView.
func (v *headlessView) Resize(size extension.Size) error {
	if size.Width <= 0 || size.Height <= 0 || size.Width > extension.MaxHeadlessSide || size.Height > extension.MaxHeadlessSide ||
		size.Width*size.Height > extension.MaxHeadlessPixels {
		return errors.New("ExtensionHeadlessSize", "a headless view is at most extension.MaxHeadlessSide a side and extension.MaxHeadlessPixels in all",
			"width", size.Width, "height", size.Height)
	}
	v.mu.Lock()
	changed := v.size != size
	v.size = size
	v.mu.Unlock()
	if changed {
		v.onUI(func() {
			if v.ended {
				return
			}
			if err := v.ov.SetSize(ui.Size{Width: float64(size.Width), Height: float64(size.Height)}); err != nil {
				v.parent.Process.Logger.Warn("a headless view cannot change its size", "err", err)
				return
			}
			v.applyWindow(v.dispatcher()) // SetSize replaced the window with a blank one
		})
	}
	return nil
}

// SendKey implements extension.HeadlessView: the event goes to the dispatcher of the binding of the view as a platform's key
// event does, so the focus decides who takes it: the shortcuts of the workspace, the editor, a menu, a dialog or a text field.
func (v *headlessView) SendKey(e extension.KeyEvent) {
	v.onUI(func() {
		if v.ended {
			return
		}
		typ := ui.KeyEventTypeDown
		switch e.Kind {
		case extension.KeyUp:
			typ = ui.KeyEventTypeUp
		case extension.KeyRepeat:
			typ = ui.KeyEventTypeRepeat
		}
		data := ui.KeyData{TimeStamp: time.Since(v.start), Type: typ, Physical: e.Physical, Logical: e.Logical}
		if typ != ui.KeyEventTypeUp {
			data.Character = e.Text
		}
		v.dispatcher().DispatchKeyData(data)
	})
}

// SendPointer implements extension.HeadlessView.
func (v *headlessView) SendPointer(e extension.PointerEvent) {
	v.onUI(func() { v.dispatchPointer(v.ptr.pointer(time.Since(v.start), e)) })
}

// SendScroll implements extension.HeadlessView.
func (v *headlessView) SendScroll(e extension.ScrollEvent) {
	v.onUI(func() { v.dispatchPointer(v.ptr.scroll(time.Since(v.start), e)) })
}

// dispatchPointer gives the platform's pointer data to the binding of the view.
func (v *headlessView) dispatchPointer(data []ui.PointerData) {
	if v.ended || len(data) == 0 {
		return
	}
	v.dispatcher().DispatchPointerDataPacket(ui.PointerDataPacket{Data: data})
}

// SetClipboard implements extension.HeadlessView.
func (v *headlessView) SetClipboard(text string) { v.clip.setText(text) }

// OnCopy implements extension.HeadlessView.
func (v *headlessView) OnCopy(fn func(text string)) { v.clip.setCopyHandler(fn) }

// OnNotification implements extension.HeadlessView: the notifications that the workspace raises are handed to fn, and the view
// makes no toast of them while it is set (the toasts of the view are in its pictures, and its client shows them its own way).
func (v *headlessView) OnNotification(fn func(extension.ViewNotification)) {
	v.onUI(func() {
		if v.ended {
			return
		}
		if v.stopNotify != nil {
			v.stopNotify()
			v.stopNotify = nil
		}
		v.ws.Notifications.Mute(fn != nil)
		if fn == nil {
			return
		}
		v.stopNotify = v.ws.Notify.OnChange(func(e notify.Event) {
			// a silent one goes to the center only, as in the window
			if e.Kind != notify.Added || e.Item.Silent {
				return
			}
			n := extension.ViewNotification{ID: e.Item.ID, Source: e.Item.Source, Message: e.Item.Message}
			switch e.Item.Severity {
			case notify.Warning:
				n.Severity = extension.NotificationWarning
			case notify.Error:
				n.Severity = extension.NotificationError
			}
			for _, a := range e.Item.Actions {
				n.Actions = append(n.Actions, a.Label)
			}
			v.onUI(func() {
				if !v.ended {
					fn(n)
				}
			})
		})
	})
}

// RunNotificationAction implements extension.HeadlessView: the button is pressed as the toast's would be, in the workspace.
func (v *headlessView) RunNotificationAction(id uint64, action int) {
	v.onUI(func() {
		if v.ended {
			return
		}
		ws := v.ws
		ws.Notify.RunAction(id, action, func(cmd string, args any) error {
			return ws.Commands.Run(command.Context{Ctx: ws.ctx, Keys: ws.Keys, Source: command.SourceAPI}, cmd, args)
		})
	})
}

// Done implements extension.HeadlessView: it is closed once the widgets of the view are gone and its documents released.
func (v *headlessView) Done() <-chan struct{} { return v.done }

// Close implements extension.HeadlessView. The widgets of the view are taken away on the UI thread, so that the editors
// release their documents, and the goroutine that draws ends.
func (v *headlessView) Close() {
	v.once.Do(func() {
		v.mu.Lock()
		v.closed = true
		v.onFrame, v.last = nil, nil
		v.mu.Unlock()
		v.onUI(func() {
			v.ended = true
			if v.stopNotify != nil {
				v.stopNotify()
				v.stopNotify = nil
			}
			v.ov.Close()
			v.cancel()
			v.ws.releaseView()
			v.parent.views = slices.DeleteFunc(v.parent.views, func(x *headlessView) bool { return x == v })
			close(v.done)
		})
	})
}

// closeHeadlessViews closes the views of the workspace; the workspace calls it as it goes. UI thread only.
func (w *Workspace) closeHeadlessViews() {
	for _, v := range slices.Clone(w.views) {
		v.Close()
	}
}

// headlessPointer turns the mouse events of a headless view into the pointer data the platform makes, with the same rules as
// Glutter's Linux backend (the pointer is added at its first event, a hover is a move when a button is down, the first
// button is a down, the last one an up).
type headlessPointer struct {
	added   bool
	x, y    float64
	buttons int
}

func (p *headlessPointer) data(now time.Duration, change ui.PointerChange) ui.PointerData {
	return ui.PointerData{
		TimeStamp: now, Change: change, Kind: ui.PointerDeviceKindMouse,
		PhysicalX: p.x, PhysicalY: p.y, Buttons: p.buttons,
	}
}

// pointer is the data of a mouse event.
func (p *headlessPointer) pointer(now time.Duration, e extension.PointerEvent) []ui.PointerData {
	var out []ui.PointerData
	if !p.added {
		p.added = true
		p.x, p.y, p.buttons = e.X, e.Y, 0
		out = append(out, p.data(now, ui.PointerChangeAdd))
	}
	was := p.buttons
	p.buttons = int(e.Buttons)
	d := p.data(now, ui.PointerChangeHover)
	d.PhysicalX, d.PhysicalY = e.X, e.Y
	d.PhysicalDeltaX, d.PhysicalDeltaY = e.X-p.x, e.Y-p.y
	moved := d.PhysicalDeltaX != 0 || d.PhysicalDeltaY != 0
	p.x, p.y = e.X, e.Y
	d.Buttons = p.buttons
	switch {
	case was == 0 && p.buttons != 0:
		d.Change = ui.PointerChangeDown
		out = append(out, d)
	case was != 0 && p.buttons == 0:
		d.Change = ui.PointerChangeUp
		out = append(out, d)
	case was != p.buttons:
		d.Change = ui.PointerChangeMove
		out = append(out, d)
	case moved && p.buttons != 0:
		d.Change = ui.PointerChangeMove
		out = append(out, d)
	case moved:
		out = append(out, d)
	}
	return out
}

// scroll is the data of a scroll: the pointer is moved there first, and the scroll is a signal of a hover, as Glutter's
// backend makes it.
func (p *headlessPointer) scroll(now time.Duration, e extension.ScrollEvent) []ui.PointerData {
	out := p.pointer(now, extension.PointerEvent{Kind: extension.PointerMove, Buttons: uint8(p.buttons), X: e.X, Y: e.Y})
	d := p.data(now, ui.PointerChangeHover)
	d.SignalKind = ui.PointerSignalKindScroll
	d.ScrollDeltaX, d.ScrollDeltaY = e.DX, e.DY
	return append(out, d)
}

// initView is the InitState of the shell of a headless view: the editor group with the documents the view starts with, the
// menus, the panels, the Explorer, the quick input, the status bar and the save commands. What belongs to the window of the
// host is left out: the tabs of the window, the backups, the auto save, the changes of files on disk, the state files,
// closing, the command line, the folder's watcher (the parent's keeps both up to date) and the dev commands.
func (s *shellState) initView(binding *widgets.WidgetsBinding) {
	s.initGroup(binding, s.Widget().docs)
	s.initMenus(binding)
	s.bindGroupCommands()
	s.initPanels(binding)
	s.initExplorer(binding)
	s.initQuickInput(binding)
	s.initStatus(binding)
	s.initSave(binding)
}

// disposeView is the Dispose of the shell of a headless view.
func (s *shellState) disposeView() {
	s.disposeSave()
	s.disposeStatus()
	s.disposeQuickInput()
	s.disposeExplorer()
	s.disposePanels()
	s.disposeGroup()
}
