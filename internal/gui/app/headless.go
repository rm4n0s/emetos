package app

import (
	"context"
	"image"
	"slices"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// The headless view of a workspace (SPEC §10.18 "As built (step 15.1)"; extension.HeadlessView): a second workbench of the
// same workspace, drawn off screen at a size of its own. It is a workspace of its own over the documents of the parent
// (newViewWorkspace) whose shell runs in a second Glutter binding (headlessEngine), so it has its own focus, cursors,
// scroll positions, side bar, panel, quick input and dialogs, and an edit in one view is in the other. Input goes in through
// the dispatcher of the binding, as the platform delivers it, and the pictures come out of the software renderer.

// headlessView is the extension.HeadlessView of one view.
type headlessView struct {
	parent *Workspace
	ws     *Workspace
	eng    *headlessEngine
	cancel context.CancelFunc
	// main is the binding of the application, whose UI thread runs the view.
	main *widgets.WidgetsBinding

	// ptr is the state of the pointer; UI thread only
	ptr headlessPointer

	// pictures hands the pictures to the goroutine that delivers them; it holds the latest only.
	pictures chan *image.RGBA
	quit     chan struct{}
	done     chan struct{}
	once     sync.Once

	mu      sync.Mutex
	size    extension.Size
	onFrame func(*image.RGBA)
	pending *image.RGBA
	raster  *rasterizer
	// closed is set when the view ended; the goroutine of the pictures reads it under mu
	closed bool
}

// newHeadlessView makes a headless view of the workspace: the workspace of the view, its shell with the editors the parent
// shows, and the engine. It runs on the UI thread. It is the NewHeadlessView of the scope of the workspace.
//
// The error has the tag ExtensionHeadlessUnavailable.
func (w *Workspace) newHeadlessView(size extension.Size) (extension.HeadlessView, error) {
	v, err := w.makeHeadlessView(size, headlessPacing{interval: headlessFrameInterval, factor: headlessPaceFactor})
	if err != nil {
		return nil, err // not a *headlessView in an interface that would be non-nil
	}
	return v, nil
}

// makeHeadlessView is newHeadlessView with the pacing of the frames given: the tests make frames at once.
func (w *Workspace) makeHeadlessView(size extension.Size, pacing headlessPacing) (*headlessView, error) {
	s := w.shell
	if w.isView() || w.ext != nil || s == nil || !s.Mounted() {
		return nil, errors.New("ExtensionHeadlessUnavailable", "the workspace is not on screen, so it has nothing to show in a second view", "folder", w.Folder)
	}
	main := widgets.WidgetsBindingOf(s.Context())
	ctx, cancel := context.WithCancel(w.ctx)
	v := &headlessView{
		parent: w, main: main, cancel: cancel, size: size,
		pictures: make(chan *image.RGBA, 1), quit: make(chan struct{}), done: make(chan struct{}),
	}
	v.ws = w.newViewWorkspace(ctx)
	v.raster = &rasterizer{log: w.Process.Logger}
	v.eng = newHeadlessEngine(ctx, main, w.Process.Logger, ui.Size{Width: float64(size.Width), Height: float64(size.Height)}, pacing, nil)
	v.eng.onScene = v.draw
	docs := s.openDocuments()
	v.eng.attach(newAppWithPage(v.ws, func() widgets.Widget { return newShell(ctx, docs, nil) }))
	w.views = append(w.views, v)
	// the view goes with the folder it shows
	stop := w.OnFolderChange(func() { v.Close() })
	v.ws.stops = append(v.ws.stops, stop)
	go v.deliverLoop()
	return v, nil
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

// draw turns the scene of a frame into a picture and hands it to the goroutine that delivers it, in place of one it has not
// taken yet. It runs on the UI thread: the paragraphs of a scene are laid out by the text engine of the application, whose
// caches only the UI thread may use, and the software renderer asks them for the glyphs.
func (v *headlessView) draw(scene *ui.Scene, width, height int) {
	img, err := v.raster.render(scene, width, height)
	if err != nil {
		v.parent.Process.Logger.Warn("a headless view cannot draw", "err", err)
		return
	}
	for {
		select {
		case v.pictures <- img:
			return
		default:
		}
		select {
		case <-v.pictures: // the older picture is not wanted any more
		default:
		}
	}
}

// deliverLoop hands the pictures to the function of OnFrame, one at a time, off the UI thread: the function encodes and
// sends them, and a slow one costs the view its in-between pictures, never time on the UI thread.
func (v *headlessView) deliverLoop() {
	for {
		select {
		case <-v.quit:
			return
		case img := <-v.pictures:
			v.mu.Lock()
			fn, closed := v.onFrame, v.closed
			if fn == nil && !closed {
				v.pending = img
			}
			v.mu.Unlock()
			if fn != nil && !closed {
				fn(img)
			}
		}
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
	pending := v.pending
	if fn != nil {
		v.pending = nil
	}
	v.mu.Unlock()
	if fn != nil && pending != nil {
		go fn(pending)
	}
}

// Refresh implements extension.HeadlessView.
func (v *headlessView) Refresh() {
	v.onUI(func() { v.eng.resend() })
}

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
		v.onUI(func() { v.eng.resize(ui.Size{Width: float64(size.Width), Height: float64(size.Height)}) })
	}
	return nil
}

// SendKey implements extension.HeadlessView: the event goes to the dispatcher of the binding of the view as a platform's key
// event does, so the focus decides who takes it: the shortcuts of the workspace, the editor, a menu, a dialog or a text field.
func (v *headlessView) SendKey(e extension.KeyEvent) {
	v.onUI(func() {
		if v.eng.closed {
			return
		}
		typ := ui.KeyEventTypeDown
		switch e.Kind {
		case extension.KeyUp:
			typ = ui.KeyEventTypeUp
		case extension.KeyRepeat:
			typ = ui.KeyEventTypeRepeat
		}
		data := ui.KeyData{TimeStamp: time.Since(v.eng.start), Type: typ, Physical: e.Physical, Logical: e.Logical}
		if typ != ui.KeyEventTypeUp {
			data.Character = e.Text
		}
		v.eng.disp.DispatchKeyData(data)
		v.eng.host.runPosted()
		v.eng.wake()
	})
}

// SendPointer implements extension.HeadlessView.
func (v *headlessView) SendPointer(e extension.PointerEvent) {
	v.onUI(func() { v.dispatchPointer(v.ptr.pointer(v.eng.sinceStart(), e)) })
}

// SendScroll implements extension.HeadlessView.
func (v *headlessView) SendScroll(e extension.ScrollEvent) {
	v.onUI(func() { v.dispatchPointer(v.ptr.scroll(v.eng.sinceStart(), e)) })
}

// dispatchPointer gives the platform's pointer data to the binding of the view.
func (v *headlessView) dispatchPointer(data []ui.PointerData) {
	if v.eng.closed || len(data) == 0 {
		return
	}
	v.eng.disp.DispatchPointerDataPacket(ui.PointerDataPacket{Data: data})
	v.eng.host.runPosted()
	v.eng.wake()
}

// SetClipboard implements extension.HeadlessView.
func (v *headlessView) SetClipboard(text string) {
	h := v.eng.host
	h.mu.Lock()
	h.clipboard, h.hasClip = text, true
	h.mu.Unlock()
}

// OnCopy implements extension.HeadlessView.
func (v *headlessView) OnCopy(fn func(text string)) {
	h := v.eng.host
	h.mu.Lock()
	h.onCopy = fn
	h.mu.Unlock()
}

// Done implements extension.HeadlessView: it is closed once the widgets of the view are gone and its documents released.
func (v *headlessView) Done() <-chan struct{} { return v.done }

// Close implements extension.HeadlessView. The widgets of the view are taken away on the UI thread, so that the editors
// release their documents, and the goroutine that draws ends.
func (v *headlessView) Close() {
	v.once.Do(func() {
		v.mu.Lock()
		v.closed = true
		v.onFrame, v.pending = nil, nil
		v.mu.Unlock()
		close(v.quit)
		v.onUI(func() {
			v.eng.close()
			v.raster.reset()
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
