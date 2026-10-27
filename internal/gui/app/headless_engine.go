package app

import (
	"context"
	"image"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// The engine of a headless view (SPEC §10.18 "As built (step 15.1)"): a second Glutter binding of its own, on a host that
// has no window. It is driven from the UI thread of the application: the one goroutine that runs both bindings, so the
// documents and the models they share are touched by one thread only. What the host is asked (post a function, schedule a
// frame) wakes the UI thread, which runs the work of the view between its own; a frame is made when the view asked for
// one and the least time between two frames has passed. The scene of a frame is turned into pixels by Glutter's software
// renderer on the UI thread too (the paragraphs of a scene belong to the text engine of the application, whose caches only
// the UI thread may use); the pictures go to the function of the view from a goroutine of its own.

// headlessFrameInterval is the least time between two frames of a headless view: at most 30 a second (SPEC §10.18).
const headlessFrameInterval = time.Second / 30

// headlessPaceFactor is how many times the time the software renderer took for the last picture the view waits before it
// makes the next frame. The renderer runs on the UI thread of the application (see below) and takes about 85 ms for a
// 1280x800 picture, so a view that drew as often as the interval allows would leave no time to the user at the keyboard of
// the host: with the factor of 2 the view takes a third of the UI thread at most, however large it is.
const headlessPaceFactor = 2

// headlessPacing says how often a view makes frames: at least interval between two, and at least factor times the time the
// last picture took to draw. The tests make a frame whenever one is asked for.
type headlessPacing struct {
	interval time.Duration
	factor   int
}

// headlessHost is the platform of a headless view: it has no window, keeps the latest scene instead of drawing it, runs
// what is posted on the UI thread of the application, and holds a clipboard of its own.
type headlessHost struct {
	// wake asks the UI thread to run the engine; it may be called from any goroutine and must not block.
	wake func()

	mu        sync.Mutex
	posted    []func()
	scheduled bool
	scene     *ui.Scene
	clipboard string
	hasClip   bool
	onCopy    func(text string)
	exits     int
}

// ScheduleFrame implements ui.PlatformHost.
func (h *headlessHost) ScheduleFrame() {
	h.mu.Lock()
	h.scheduled = true
	h.mu.Unlock()
	h.wake()
}

// RenderView implements ui.PlatformHost: only the latest scene is kept.
func (h *headlessHost) RenderView(_ ui.ViewID, scene *ui.Scene) error {
	h.mu.Lock()
	h.scene = scene
	h.mu.Unlock()
	return nil
}

// RequestAppExit implements ui.PlatformHost. A view cannot end the application: the request is counted and ignored.
func (h *headlessHost) RequestAppExit() {
	h.mu.Lock()
	h.exits++
	h.mu.Unlock()
}

// Post implements ui.PlatformHost.
func (h *headlessHost) Post(fn func()) {
	h.mu.Lock()
	h.posted = append(h.posted, fn)
	h.mu.Unlock()
	h.wake()
}

// SetClipboardText implements ui.ClipboardHost: the text is the clipboard of the view, and the owner of the view is told.
// done is called later on the UI thread, as the interface asks.
func (h *headlessHost) SetClipboardText(_ context.Context, text string, done func(error)) {
	h.mu.Lock()
	h.clipboard, h.hasClip = text, true
	onCopy := h.onCopy
	h.mu.Unlock()
	if onCopy != nil {
		onCopy(text)
	}
	if done != nil {
		h.Post(func() { done(nil) })
	}
}

// GetClipboardText implements ui.ClipboardHost.
func (h *headlessHost) GetClipboardText(_ context.Context, done func(string, bool, error)) {
	h.mu.Lock()
	text, ok := h.clipboard, h.hasClip
	h.mu.Unlock()
	h.Post(func() { done(text, ok, nil) })
}

// SetTextInput implements ui.TextInputHost. There is no input method behind a headless view: the text arrives with the
// key events.
func (h *headlessHost) SetTextInput(ui.TextInputState) {}

// runPosted runs what was posted, including what that posts, on the calling goroutine.
func (h *headlessHost) runPosted() {
	for {
		h.mu.Lock()
		if len(h.posted) == 0 {
			h.mu.Unlock()
			return
		}
		fn := h.posted[0]
		h.posted[0] = nil
		h.posted = h.posted[1:]
		h.mu.Unlock()
		fn()
	}
}

// takeScheduled says whether a frame was asked for, and forgets it.
func (h *headlessHost) takeScheduled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.scheduled
	h.scheduled = false
	return s
}

// takeScene returns the scene that was rendered since the last call, or nil.
func (h *headlessHost) takeScene() *ui.Scene {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.scene
	h.scene = nil
	return s
}

// headlessEngine is the binding of a headless view with the host under it and the renderer that makes its pixels.
type headlessEngine struct {
	log *slog.Logger
	// main is the binding of the application, whose UI thread runs the engine; the wake-ups are posted to it.
	main    *widgets.WidgetsBinding
	host    *headlessHost
	disp    *ui.PlatformDispatcher
	binding *widgets.WidgetsBinding
	view    *ui.View
	start   time.Time

	// the following belong to the UI thread
	cfg        ui.ViewConfiguration
	closed     bool
	lastFrame  time.Time
	timer      *time.Timer
	hasScene   bool
	lastScene  *ui.Scene
	lastW      int
	lastH      int
	onScene    func(scene *ui.Scene, width, height int)
	frames     int
	reportErrs func(error)
	// pacing is how often frames are made; the zero value makes a frame whenever one is asked for (tests). cost is the time
	// the last scene took to draw.
	pacing headlessPacing
	cost   time.Duration

	// queued says that a wake-up is on its way to the UI thread.
	queued atomic.Bool
}

// newHeadlessEngine makes the binding. It must be called on the UI thread of main. The text engine, the fonts and the
// brightness are those of main, so a headless view looks like the window; the size is in pixels (the view has a pixel ratio
// of 1). reportErr gets the errors the framework reports for the view (it logs them when nil).
func newHeadlessEngine(ctx context.Context, main *widgets.WidgetsBinding, log *slog.Logger, size ui.Size, pacing headlessPacing, reportErr func(error)) *headlessEngine {
	e := &headlessEngine{log: log, main: main, start: time.Now(), reportErrs: reportErr, pacing: pacing}
	e.host = &headlessHost{wake: e.wake}
	e.disp = ui.NewPlatformDispatcher(e.host)
	md := main.PlatformDispatcher()
	e.disp.SetTextEngine(md.TextEngine())
	uiFamily, uiSize := md.UIFont()
	monoFamily, monoSize := md.MonospaceFont()
	e.disp.UpdatePlatformConfiguration(ui.PlatformConfiguration{
		Locales: md.Locales(), TextScaleFactor: md.TextScaleFactor(), PlatformBrightness: md.PlatformBrightness(),
		UIFontFamily: uiFamily, UIFontSize: uiSize, MonospaceFontFamily: monoFamily, MonospaceFontSize: monoSize,
		// the caret of a headless view does not blink: a blink is a picture twice a second, which costs the software renderer
		// 17% of a UI thread for nothing, and a view that is not typed in costs none
		CaretBlinkTime: ui.CaretBlinkNever,
	})
	e.cfg = ui.ViewConfiguration{
		PhysicalSize: size, DevicePixelRatio: 1,
		// a headless view is a window without a title bar: nothing can move, minimize or close it, and the menu bar has a row
		// of its own
		Window: ui.WindowInfo{Decoration: ui.DecorationNone, Activated: true, Maximized: true},
	}
	e.view = e.disp.AddView(ui.ImplicitViewID, e.cfg)
	report := func(err error) {
		if e.reportErrs != nil {
			e.reportErrs(err)
			return
		}
		e.log.Warn("a headless view reported an error", "err", err)
	}
	e.binding = widgets.NewWidgetsBinding(ctx, e.disp, widgets.WidgetsBindingOpts{
		ErrorReporter: report,
		// one UI thread for both bindings: the goroutine that runs the application
		UIThread: main.UIThread(),
		Logger:   log,
	})
	e.disp.SetAppLifecycleState(ui.AppLifecycleStateResumed)
	return e
}

// attach makes the widget the root of the view and asks for the first frame.
func (e *headlessEngine) attach(root widgets.Widget) {
	e.binding.AttachRootWidget(e.binding.WrapWithDefaultView(root))
	e.binding.Scheduler().ScheduleFrame()
	e.wake()
}

// wake asks the UI thread to run the engine. Any goroutine may call it; it posts at most one function at a time.
func (e *headlessEngine) wake() {
	if e.queued.Swap(true) {
		return
	}
	e.main.Post(func() {
		e.queued.Store(false)
		e.pump()
	})
}

// pump runs what the view posted and, when it asked for a frame and the least time between two frames has passed, one
// frame. It runs on the UI thread. A frame that has to wait is made by a timer.
func (e *headlessEngine) pump() {
	if e.closed {
		return
	}
	e.host.runPosted()
	sched := e.binding.Scheduler()
	if !sched.HasScheduledFrame() {
		return
	}
	gap := max(e.pacing.interval, time.Duration(e.pacing.factor)*e.cost)
	if wait := gap - time.Since(e.lastFrame); wait > 0 && !e.lastFrame.IsZero() {
		if e.timer == nil {
			e.timer = time.AfterFunc(wait, e.wake)
		} else {
			e.timer.Reset(wait)
		}
		return
	}
	e.frame()
}

// frame makes one frame now and hands its scene on. It runs on the UI thread.
func (e *headlessEngine) frame() {
	sched := e.binding.Scheduler()
	e.host.takeScheduled()
	e.lastFrame = time.Now()
	sched.HandleBeginFrame(foundation.Some(time.Since(e.start)))
	e.host.runPosted()
	sched.HandleDrawFrame()
	e.host.runPosted()
	e.frames++
	if scene := e.host.takeScene(); scene != nil {
		e.lastScene = scene
		e.lastW, e.lastH = int(e.cfg.PhysicalSize.Width), int(e.cfg.PhysicalSize.Height)
		e.hasScene = true
		if e.onScene != nil {
			t0 := time.Now()
			e.onScene(scene, e.lastW, e.lastH)
			e.cost = time.Since(t0)
		}
	}
	// a frame that asked for the next one (an animation, the blink of the caret) is made after the interval
	if sched.HasScheduledFrame() {
		e.wake()
	}
}

// sinceStart is the time of an event: since the engine started, the clock the frames use.
func (e *headlessEngine) sinceStart() time.Duration { return time.Since(e.start) }

// resend hands the last scene on again.
func (e *headlessEngine) resend() {
	if e.hasScene && e.onScene != nil && !e.closed {
		e.onScene(e.lastScene, e.lastW, e.lastH)
	}
}

// resize gives the view a new size in pixels and asks for a frame.
func (e *headlessEngine) resize(size ui.Size) {
	e.cfg.PhysicalSize = size
	e.disp.UpdateViewConfiguration(ui.ImplicitViewID, e.cfg)
	e.binding.Scheduler().ScheduleFrame()
	e.wake()
}

// close takes the widgets away, so that every state is disposed, and stops the engine. It runs on the UI thread.
func (e *headlessEngine) close() {
	if e.closed {
		return
	}
	e.onScene = nil // the frame below draws nothing worth a picture
	// an empty collection of views in place of the root: the states below are disposed and their lifetimes cancelled
	e.binding.AttachRootWidget(widgets.NewViewCollection(nil, widgets.ViewCollectionOpts{Key: foundation.NewUniqueKey()}))
	e.binding.Scheduler().ScheduleFrame()
	e.host.runPosted()
	if e.binding.Scheduler().HasScheduledFrame() {
		e.frame()
	}
	e.closed = true
	if e.timer != nil {
		e.timer.Stop()
	}
	e.host.runPosted()
	e.lastScene = nil
}

// ---- the renderer ----

// rasterRecycle is the number of frames after which the rasterizer is made again: Glutter's fake platform keeps every
// scene it was given (the only public way to the software renderer is a gluttertest.Tester, whose host keeps them all),
// so the tester is replaced before that costs memory.
const rasterRecycle = 8

// rasterTB is the testing.TB of the tester the rasterizer holds: it logs what the tester reports and keeps its cleanups.
type rasterTB struct {
	log  *slog.Logger
	mu   sync.Mutex
	done []func()
}

func (*rasterTB) Helper() {}
func (t *rasterTB) Errorf(format string, args ...any) {
	t.log.Warn("the rasterizer of a headless view reported an error", "format", format, "args", args)
}
func (t *rasterTB) Fatalf(format string, args ...any) { t.Errorf(format, args...) }
func (t *rasterTB) Fatal(args ...any)                 { t.Errorf("fatal", args...) }
func (t *rasterTB) Logf(string, ...any)               {}
func (t *rasterTB) Cleanup(f func()) {
	t.mu.Lock()
	t.done = append(t.done, f)
	t.mu.Unlock()
}

// rasterizer turns scenes into pictures with Glutter's software renderer.
//
// Glutter: the software renderer (internal/gpu/soft) is not reachable from outside Glutter, and a scene can be drawn to
// pixels only by a gluttertest.Tester, whose fake platform keeps every scene it is given for the life of the tester.
// Emetos asks a Tester for the pixels of the scenes of another binding (Tester.View.Render, then Tester.Screenshot) and
// replaces the Tester every rasterRecycle frames. Glutter should offer a public ui.Rasterizer for the software renderer
// (Scene.ToImage takes one already) and then none of this is needed (docs/GLUTTER-ISSUES.md).
type rasterizer struct {
	log    *slog.Logger
	tb     *rasterTB
	tester *gluttertest.Tester
	w, h   int
	n      int
}

// render draws the scene into a new picture of the size.
//
// The error has the tag HeadlessRasterFailed.
func (r *rasterizer) render(scene *ui.Scene, width, height int) (*image.RGBA, error) {
	if r.tester == nil || r.n >= rasterRecycle || r.w != width || r.h != height {
		r.reset()
		r.tb = &rasterTB{log: r.log}
		r.tester = gluttertest.New(r.tb, gluttertest.Opts{Size: ui.Size{Width: float64(width), Height: float64(height)}})
		r.w, r.h, r.n = width, height, 0
	}
	if err := r.tester.View.Render(scene); err != nil {
		return nil, errors.NewErr("HeadlessRasterFailed", err).SetMetadata("width", width, "height", height)
	}
	r.n++
	img := r.tester.Screenshot()
	if img == nil {
		return nil, errors.New("HeadlessRasterFailed", "the software renderer made no picture", "width", width, "height", height)
	}
	return img, nil
}

// reset lets go of the tester.
func (r *rasterizer) reset() {
	if r.tb != nil {
		for i := len(r.tb.done) - 1; i >= 0; i-- {
			r.tb.done[i]()
		}
	}
	r.tester, r.tb = nil, nil
}
