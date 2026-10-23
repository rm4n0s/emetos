package editor

import (
	"context"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Where the find widget sits: below the top of the editor and left of the vertical scrollbar.
const (
	findWidgetTop   = 4.0
	findWidgetRight = kit.DefaultVerticalScrollbarSize + 4
)

// textFactory is the text engine of the window.
func textFactory(bc widgets.BuildContext) ui.ParagraphFactory {
	return widgets.WidgetsBindingOf(bc).PlatformDispatcher().TextEngine()
}

// textLayer is the widget of RenderEditorText.
type textLayer struct {
	widgets.LeafRenderObjectWidget
	vm     *ViewModel
	offset rendering.ViewportOffset
	color  ui.Color
	// tokens colors the tokens of the document; nil draws the text in color.
	tokens *theme.TokenTheme
	// brackets are the colors of the brackets by level.
	brackets BracketColors
	// created is told of the render object when it is made.
	created func(*RenderEditorText)
}

func newTextLayer(vm *ViewModel, offset rendering.ViewportOffset, color ui.Color, tokens *theme.TokenTheme, brackets BracketColors, created func(*RenderEditorText)) *textLayer {
	return &textLayer{LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(), vm: vm, offset: offset, color: color, tokens: tokens, brackets: brackets, created: created}
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *textLayer) CreateRenderObject(bc widgets.BuildContext) rendering.RenderObject {
	r := NewRenderEditorText(w.vm, w.offset, textFactory(bc), w.color)
	r.SetTokenTheme(w.tokens)
	r.SetBracketColors(w.brackets)
	if w.created != nil {
		w.created(r)
	}
	return r
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *textLayer) UpdateRenderObject(bc widgets.BuildContext, ro rendering.RenderObject) {
	r := ro.(*RenderEditorText)
	r.SetViewModel(w.vm)
	r.SetOffset(w.offset)
	r.SetColor(w.color)
	r.SetTokenTheme(w.tokens)
	r.SetBracketColors(w.brackets)
	r.SetFactory(textFactory(bc))
}

// gutterLayer is the widget of RenderEditorGutter.
type gutterLayer struct {
	widgets.LeafRenderObjectWidget
	vm     *ViewModel
	offset rendering.ViewportOffset
	colors GutterColors
}

func newGutterLayer(vm *ViewModel, offset rendering.ViewportOffset, colors GutterColors) *gutterLayer {
	return &gutterLayer{LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(), vm: vm, offset: offset, colors: colors}
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *gutterLayer) CreateRenderObject(bc widgets.BuildContext) rendering.RenderObject {
	return NewRenderEditorGutter(w.vm, w.offset, textFactory(bc), w.colors)
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *gutterLayer) UpdateRenderObject(bc widgets.BuildContext, ro rendering.RenderObject) {
	ro.(*RenderEditorGutter).Update(w.vm, w.offset, textFactory(bc), w.colors)
}

// overlayLayer is the widget of RenderEditorOverlay.
type overlayLayer struct {
	widgets.LeafRenderObjectWidget
	kind   overlayKind
	vm     *ViewModel
	offset rendering.ViewportOffset
	colors OverlayColors
}

func newOverlayLayer(kind overlayKind, vm *ViewModel, offset rendering.ViewportOffset, colors OverlayColors) *overlayLayer {
	return &overlayLayer{LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(), kind: kind, vm: vm, offset: offset, colors: colors}
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *overlayLayer) CreateRenderObject(bc widgets.BuildContext) rendering.RenderObject {
	return NewRenderEditorOverlay(w.kind, w.vm, w.offset, textFactory(bc), w.colors)
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *overlayLayer) UpdateRenderObject(bc widgets.BuildContext, ro rendering.RenderObject) {
	ro.(*RenderEditorOverlay).Update(w.vm, w.offset, textFactory(bc), w.colors)
}

// focusObserver tells the editor when the window gets or loses the keyboard focus.
type focusObserver struct {
	widgets.WidgetsBindingObserverBase
	onFocus func(focused bool)
}

// DidChangeViewFocus implements widgets.WidgetsBindingObserver.
func (o *focusObserver) DidChangeViewFocus(e ui.ViewFocusEvent) {
	o.onFocus(e.State == ui.ViewFocusStateFocused)
}

// editorScroll is the kit.ScrollModel of an editor: the vertical position is the scroll
// controller's, the horizontal one the view model's, and the sizes are what the text layer
// measured. It forwards the changes of all three to its own listeners.
type editorScroll struct {
	foundation.ChangeNotifier
	vm         *ViewModel
	controller *widgets.ScrollControllerBase
	handles    [3]foundation.ListenerHandle
}

func newEditorScroll(vm *ViewModel, controller *widgets.ScrollControllerBase) *editorScroll {
	m := &editorScroll{ChangeNotifier: *foundation.NewChangeNotifier(), vm: vm, controller: controller}
	m.handles[0] = controller.AddListener(m.NotifyListeners)
	m.handles[1] = vm.AddMetricsListener(m.NotifyListeners)
	m.handles[2] = vm.AddListener(m.NotifyListeners) // the horizontal position
	return m
}

// dispose stops following the controller and the view model.
func (m *editorScroll) dispose() {
	m.controller.RemoveListener(m.handles[0])
	m.vm.RemoveMetricsListener(m.handles[1])
	m.vm.RemoveListener(m.handles[2])
}

// Metrics implements kit.ScrollModel.
func (m *editorScroll) Metrics() kit.ScrollMetrics {
	width, height := m.vm.ViewportSize()
	top := m.vm.ScrollTop()
	if m.controller.HasClients() {
		top = m.controller.Offset()
	}
	return kit.ScrollMetrics{
		ViewportWidth: width, ViewportHeight: height,
		ContentWidth: m.vm.ContentWidth(), ContentHeight: m.vm.ContentHeight(),
		Left: m.vm.ScrollLeft(), Top: top,
	}
}

// ScrollTo implements kit.ScrollModel.
func (m *editorScroll) ScrollTo(left, top float64) {
	m.vm.SetScrollLeft(left)
	if !m.controller.HasClients() {
		return
	}
	pos := m.controller.Position()
	m.controller.JumpTo(min(max(top, pos.MinScrollExtent()), pos.MaxScrollExtent()))
}

// Editor is the editor widget: the text of a view model with its gutter, its selections, its
// current line and its cursors, scrolled together by a Scrollable. Input comes with later
// steps.
type Editor struct {
	widgets.StatefulWidget
	vm   *ViewModel
	opts EditorOpts
}

// EditorOpts configure an Editor; every field is optional.
type EditorOpts struct {
	// Commands is the registry whose editor commands the editor takes over while it has the
	// focus (see BindCommands).
	Commands *command.Registry
	// Keys is the scope of the context keys the editor makes its own scope in: it sets
	// editorTextFocus, editorFocus, textInputFocus, editorHasSelection,
	// editorHasMultipleSelections, editorColumnSelection, editorReadonly, editorTabMovesFocus
	// and editorLangId there. Without it the editor sets no context keys.
	Keys *contextkey.Scope
	// Scopes registers the editor's focus node with its scope, so that the key dispatcher
	// of the window resolves keys in it while the editor has the focus.
	Scopes *kit.FocusScopes
	// Autofocus asks for the focus when the editor is first shown.
	Autofocus bool
	// OnError is told about the errors of the keyboard input, such as typing into a
	// read-only document. Without it they are dropped.
	OnError func(error)
	// Context is the context of what the editor asks of the desktop (the clipboard); without
	// it that is context.Background().
	Context context.Context
	// Zoom is the zoom of the font that the editors of a window share; without it the editor has
	// its own.
	Zoom *Zoom
	// ClipboardMemory is what the editors of a window remember of their copies, so that a copy
	// in one can be pasted in another as it was made; without it the editor has its own.
	ClipboardMemory *ClipboardMemory
	// FindMemory is what the editors of a window share of their searches (the switches of the
	// find widget); without it the editor has its own.
	FindMemory *FindMemory
}

// NewEditor makes the editor of a view model.
func NewEditor(vm *ViewModel, opts ...EditorOpts) *Editor {
	e := &Editor{StatefulWidget: widgets.NewStatefulWidgetBase(), vm: vm}
	if len(opts) > 0 {
		e.opts = opts[0]
	}
	return e
}

// ViewModel is the view model the editor shows.
func (e *Editor) ViewModel() *ViewModel { return e.vm }

// CreateState implements widgets.StatefulWidget.
func (e *Editor) CreateState() widgets.State { return &editorState{} }

type editorState struct {
	widgets.StateOf[*Editor]
	controller   *widgets.ScrollControllerBase
	vmHandle     foundation.ListenerHandle
	scrollHandle foundation.ListenerHandle
	binding      *widgets.WidgetsBinding
	observer     *focusObserver
	scroll       *editorScroll

	input           *textInput
	mouse           mouseState
	auto            autoScroll
	smoothTarget    *float64
	smooth          smoothScroll
	gutterWidth     float64
	primaryTimer    func()
	primaryText     string
	lastSelections  []buffer.Selection
	cursorAnim      cursorAnimation
	dragStyle       string
	textRO          *RenderEditorText
	geometryPending bool
	node            *widgets.FocusNode
	scope           *contextkey.Scope
	unregisterScope func()
	unbindCommands  func()
	selHandle       foundation.ListenerHandle
	metricsHandle   foundation.ListenerHandle
	// The find widget: the focus nodes and the text of its inputs, the scope its inputs have their
	// keys in, and whether one of the inputs has the focus.
	findNode, replaceNode *widgets.FocusNode
	findText, replaceText *widgets.TextEditingController
	findScope             *contextkey.Scope
	unregisterFind        []func()
	findFocused           bool
	findHandle            foundation.ListenerHandle
	// windowFocused and editorFocused make the focus of the view model: the editor has it
	// when it has the focus of the window's tree and the window has the keyboard.
	windowFocused, editorFocused bool
}

// InitState starts the scroll controller at the position the view model remembers, follows
// the view model (its changes rebuild the editor, since the width of the gutter depends on the
// number of lines), and makes the editor's focus node, its context keys and its timers.
func (s *editorState) InitState() {
	vm := s.Widget().vm
	opts := s.Widget().opts
	s.controller = widgets.NewScrollController(widgets.ScrollControllerOpts{
		InitialScrollOffset: vm.ScrollTop(),
		KeepScrollOffset:    foundation.Some(false),
	})
	s.scrollHandle = s.controller.AddListener(func() {
		s.Widget().vm.SetScrollTop(s.controller.Offset())
		s.scheduleInputGeometry()
	})
	s.vmHandle = vm.AddListener(s.viewModelChanged)
	s.scroll = newEditorScroll(vm, s.controller)
	vm.SetScroller(s.scrollTo)

	s.binding = widgets.WidgetsBindingOf(s.Context())
	vm.SetPoster(s.binding.Post)
	s.windowFocused = true
	s.observer = &focusObserver{onFocus: func(focused bool) {
		s.windowFocused = focused
		s.applyFocus()
	}}
	s.binding.AddObserver(s.observer)
	s.useTimers(vm)
	s.useClipboard(vm)
	vm.SetAnimator(s.startCursorAnimation)
	vm.SetFramer(s.runPerFrame)
	vm.SetZoom(opts.Zoom)

	// The focus: the editor's node, with the scope of its context keys.
	s.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Editor"})
	if opts.Keys != nil {
		s.scope = opts.Keys.Child()
		if opts.Scopes != nil {
			s.unregisterScope = opts.Scopes.Register(s.node, s.scope)
		}
	}
	s.initFind(vm)
	s.input = &textInput{state: s}
	s.selHandle = vm.AddSelectionListener(s.selectionChanged)
	s.metricsHandle = vm.AddMetricsListener(s.scheduleInputGeometry)
	s.applyFocus()
}

// viewModelChanged rebuilds the editor, since the width of the gutter depends on the number of
// lines, and tells the input method where its caret went.
func (s *editorState) viewModelChanged() {
	s.SetState(func() {})
	s.scheduleInputGeometry()
}

// selectionChanged follows the selections: the context keys, the line the input method has
// and where it puts its window.
func (s *editorState) selectionChanged() {
	s.updateKeys()
	s.input.sync()
	s.scheduleInputGeometry()
	s.schedulePrimarySelection()
}

// useClipboard gives the view model the clipboard of the window.
func (s *editorState) useClipboard(vm *ViewModel) {
	ctx := s.Widget().opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	vm.SetClipboard(NewClipboard(ctx, s.binding.Clipboard()), s.Widget().opts.ClipboardMemory, s.reportInputError)
}

// cursorAnimation is the frame loop of the animated cursor styles.
type cursorAnimation struct {
	// running is whether a frame callback is waiting; started and begin are the time of the
	// first frame since the cursors were last shown.
	running, started bool
	begin            time.Duration
}

// startCursorAnimation begins the animation of the cursors again from their first frame, and
// starts the frame loop when it is not running.
func (s *editorState) startCursorAnimation() {
	a := &s.cursorAnim
	a.started = false
	if a.running {
		return
	}
	a.running = true
	s.scheduleCursorFrame()
}

// scheduleCursorFrame asks for the next frame of the animation.
func (s *editorState) scheduleCursorFrame() {
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(s.cursorFrame)
}

// cursorFrame moves the animation to the time of a frame, and goes on while it has more.
func (s *editorState) cursorFrame(stamp time.Duration) {
	a := &s.cursorAnim
	vm := s.Widget().vm
	if !s.Mounted() {
		a.running = false
		return
	}
	if !a.started {
		a.begin, a.started = stamp, true
	}
	if !vm.blink.advance(stamp - a.begin) {
		a.running = false
		return
	}
	s.scheduleCursorFrame()
}

// primarySelectionDelay is how long after the last change of the selections the primary
// selection of the desktop gets them.
const primarySelectionDelay = 100 * time.Millisecond

// schedulePrimarySelection makes the selected text the primary selection of the desktop soon
// after the selections stop changing, when every cursor has a selection (editor.selectionClipboard
// on Linux). Only the editor that has the focus does it, so that an editor in the background
// does not take the selection away from the one the user works in.
//
// Ported from VS Code: the SelectionClipboard contribution (src/vs/editor/contrib/selectionClipboard, before it was removed)
func (s *editorState) schedulePrimarySelection() {
	vm := s.Widget().vm
	sels := vm.Cursors().Selections()
	changed := !buffer.SelectionsEqual(sels, s.lastSelections)
	s.lastSelections = sels
	if !changed {
		return // the focus or the text moved the notification, not the selections
	}
	if s.primaryTimer != nil {
		s.primaryTimer()
		s.primaryTimer = nil
	}
	if !vm.Options().SelectionClipboard || !vm.Focused() {
		return
	}
	s.primaryTimer = s.binding.Gestures().NewTimer(primarySelectionDelay, func() {
		s.primaryTimer = nil
		vm := s.Widget().vm
		text, ok := vm.selectedText()
		if !ok || text == s.primaryText || vm.clip == nil {
			return
		}
		s.primaryText = text
		vm.clip.clipboard.WritePrimary(text, vm.clip.failed)
	}).Cancel
}

// reportInputError passes an error of the keyboard input on.
func (s *editorState) reportInputError(err error) {
	if report := s.Widget().opts.OnError; report != nil && err != nil {
		report(err)
	}
}

// scheduleInputGeometry tells the input method, after the frame, where the editor is and where
// its caret is (Glutter's SetComposingRect does not reach the platform, so the caret rect is
// the input method's anchor, including while it composes). Several changes in a frame make
// one call.
func (s *editorState) scheduleInputGeometry() {
	if s.geometryPending || s.input == nil || s.input.conn == nil {
		return
	}
	s.geometryPending = true
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		s.geometryPending = false
		conn := s.input.conn
		if conn == nil || !conn.Attached() || s.textRO == nil || !s.textRO.Attached() {
			return
		}
		conn.SetEditableSizeAndTransform(s.textRO.Size(), s.textRO.GetTransformTo(nil))
		conn.SetCaretRect(s.textRO.caretRect())
	})
}

// scrollTo moves the editor vertically to a position, within the content.
func (s *editorState) scrollTo(top float64) {
	if !s.controller.HasClients() {
		return
	}
	s.smoothTarget = nil
	pos := s.controller.Position()
	s.controller.JumpTo(min(max(top, pos.MinScrollExtent()), pos.MaxScrollExtent()))
}

// focusChanged is called when the editor's node gains or loses the focus: the editor takes
// over its commands while it has it.
func (s *editorState) focusChanged(has bool) {
	s.editorFocused = has
	if has {
		s.input.attach(s.binding)
		s.scheduleInputGeometry()
	} else {
		s.input.detach()
		s.Widget().vm.SetPreedit(Preedit{})
	}
	s.refreshCommands()
	s.applyFocus()
}

// refreshCommands makes the editor take over its commands while the editor or its find widget has
// the focus, and give them back otherwise.
func (s *editorState) refreshCommands() {
	cmds := s.Widget().opts.Commands
	if cmds == nil {
		return
	}
	want := s.editorFocused || s.findFocused
	switch {
	case want && s.unbindCommands == nil:
		s.unbindCommands = BindCommands(cmds, s.Widget().vm)
	case !want && s.unbindCommands != nil:
		s.unbindCommands()
		s.unbindCommands = nil
	}
}

// applyFocus gives the view model its focus (the editor's and the window's) and brings the
// context keys up to date.
func (s *editorState) applyFocus() {
	s.Widget().vm.SetFocused(s.editorFocused && s.windowFocused)
	s.updateKeys()
}

// updateKeys sets the context keys of the editor from its focus, selections and document.
//
// Ported from VS Code: src/vs/editor/common/editorContextKeys.ts
func (s *editorState) updateKeys() {
	if s.scope == nil {
		return
	}
	vm := s.Widget().vm
	sels := vm.Cursors().Selections()
	s.scope.Batch(func() {
		s.scope.Set("editorTextFocus", s.editorFocused)
		s.scope.Set("editorFocus", s.editorFocused)
		s.scope.Set("textInputFocus", s.editorFocused)
		s.scope.Set("editorHasSelection", !allEmpty(sels))
		s.scope.Set("editorHasMultipleSelections", len(sels) > 1)
		s.scope.Set("editorColumnSelection", vm.columns != nil)
		s.scope.Set("editorReadonly", vm.Document().ReadOnly())
		s.scope.Set("editorTabMovesFocus", false)
		s.scope.Set("editorLangId", vm.Document().LanguageID())
		s.scope.Set("foldingEnabled", vm.foldingEnabled())
		s.scope.Set("findWidgetVisible", vm.FindRevealed())
	})
}

// useTimers gives the view model the timers of the binding.
func (s *editorState) useTimers(vm *ViewModel) {
	gestures := s.binding.Gestures()
	vm.SetTimer(func(d time.Duration, fn func()) func() { return gestures.NewTimer(d, fn).Cancel })
}

// DidUpdateWidget follows a new view model.
func (s *editorState) DidUpdateWidget(old *Editor) {
	if old.vm != s.Widget().vm {
		if s.unbindCommands != nil {
			s.unbindCommands()
			s.unbindCommands = nil
		}
		s.releaseFind(old.vm)
		old.vm.RemoveListener(s.vmHandle)
		old.vm.RemoveSelectionListener(s.selHandle)
		old.vm.RemoveMetricsListener(s.metricsHandle)
		old.vm.SetPreedit(Preedit{})
		old.vm.SetTimer(nil)
		old.vm.SetScroller(nil)
		old.vm.SetClipboard(nil, nil, nil)
		old.vm.SetAnimator(nil)
		old.vm.SetFramer(nil)
		vm := s.Widget().vm
		vm.SetPoster(s.binding.Post)
		s.vmHandle = vm.AddListener(s.viewModelChanged)
		s.selHandle = vm.AddSelectionListener(s.selectionChanged)
		s.metricsHandle = vm.AddMetricsListener(s.scheduleInputGeometry)
		s.scroll.dispose()
		s.scroll = newEditorScroll(vm, s.controller)
		vm.SetScroller(s.scrollTo)
		s.useTimers(vm)
		s.useClipboard(vm)
		vm.SetAnimator(s.startCursorAnimation)
		vm.SetFramer(s.runPerFrame)
		vm.SetZoom(s.Widget().opts.Zoom)
		s.refreshCommands()
		s.useFind(vm)
		s.applyFocus()
	}
}

// Dispose lets go of the controller, the listeners, the focus node and the context keys.
func (s *editorState) Dispose() {
	s.endPress()
	s.disposeFind()
	if s.unbindCommands != nil {
		s.unbindCommands()
	}
	if s.unregisterScope != nil {
		s.unregisterScope()
	}
	if s.scope != nil {
		s.scope.Dispose()
	}
	s.node.Dispose()
	s.scroll.dispose()
	s.binding.RemoveObserver(s.observer)
	s.input.detach()
	vm := s.Widget().vm
	vm.RemoveSelectionListener(s.selHandle)
	vm.RemoveMetricsListener(s.metricsHandle)
	vm.SetPreedit(Preedit{})
	vm.SetTimer(nil)
	vm.SetScroller(nil)
	vm.SetClipboard(nil, nil, nil)
	vm.SetAnimator(nil)
	vm.SetFramer(nil)
	if s.primaryTimer != nil {
		s.primaryTimer()
	}
	vm.RemoveListener(s.vmHandle)
	s.controller.RemoveListener(s.scrollHandle)
	s.controller.Dispose()
}

// Build implements widgets.State: the editor with the find widget over its top right corner. The
// widget is not inside the editor's Focus, so the focus of its inputs is not the editor's.
func (s *editorState) Build(bc widgets.BuildContext) widgets.Widget {
	vm := s.Widget().vm
	colors := kit.ThemeOf(bc)
	editor := widgets.NewFocus(s.body(bc, vm, colors), widgets.FocusOpts{
		FocusNode: s.node, Autofocus: s.Widget().opts.Autofocus, OnFocusChange: s.focusChanged,
	})
	findWidget := newFindWidget(vm, s.findNode, s.replaceNode, s.findText, s.replaceText, s.findFocusChanged, s.reportInputError)
	return widgets.NewStack([]widgets.Widget{
		editor,
		widgets.NewPositioned(widgets.NewAlign(findWidget, widgets.AlignOpts{Alignment: painting.AlignmentTopRight}),
			widgets.PositionedOpts{Top: foundation.Some(findWidgetTop), Left: foundation.Some(0.0), Right: foundation.Some(findWidgetRight)}),
	}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// body is the editor's content: the scrollable text with its gutter, the decorations and the
// scrollbars.
func (s *editorState) body(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	gutterWidth := vm.GutterWidths(textFactory(bc)).Total()
	s.gutterWidth = gutterWidth
	tokenTheme := kit.TokenThemeOf(bc)
	gutterColors := GutterColors{
		LineNumber: colors.EditorLineNumberForeground, ActiveLineNumber: colors.EditorLineNumberActiveForeground,
		LineHighlight: colors.EditorLineHighlightBackground, LineHighlightBorder: colors.EditorLineHighlightBorder,
		FoldingControl: colors.EditorGutterFoldingControlForeground,
	}
	overlayColors := OverlayColors{
		Background: colors.EditorBackground, Selection: colors.EditorSelectionBackground,
		InactiveSelection: colors.EditorInactiveSelectionBackground, LineHighlight: colors.EditorLineHighlightBackground,
		LineHighlightBorder: colors.EditorLineHighlightBorder, Cursor: colors.EditorCursorForeground,
		CursorText: oppositeColor(colors.EditorCursorForeground), Foreground: colors.EditorForeground,
		BracketMatch: colors.EditorBracketMatchBackground, BracketMatchBorder: colors.EditorBracketMatchBorder,
		FoldBackground: colors.EditorFoldBackground, FoldPlaceholder: colors.EditorFoldPlaceholderForeground,
		FindMatch: colors.EditorFindMatchBackground, FindMatchBorder: colors.EditorFindMatchBorder,
		FindMatchHighlight: colors.EditorFindMatchHighlightBackground, FindMatchHighlightBorder: colors.EditorFindMatchHighlightBorder,
		FindRangeHighlight: colors.EditorFindRangeHighlightBackground, FindRangeHighlightBorder: colors.EditorFindRangeHighlightBorder,
		Whitespace: colors.EditorWhitespaceForeground, Ruler: colors.EditorRulerForeground,
		SelectionHighlight: colors.EditorSelectionHighlightBackground, SelectionHighlightBorder: colors.EditorSelectionHighlightBorder,
		WordHighlight: colors.EditorWordHighlightTextBackground, WordHighlightBorder: colors.EditorWordHighlightTextBorder,
		Guides: NewGuideColors(colors.EditorIndentGuideBackground, colors.EditorIndentGuideActiveBackground,
			colors.EditorBracketPairGuideBackground, colors.EditorBracketPairGuideActiveBackground, colors.EditorBracketHighlightForeground),
	}
	bracketColors := BracketColors{Levels: colors.EditorBracketHighlightForeground, Unexpected: colors.EditorBracketUnexpectedForeground}
	scrollable := widgets.NewScrollable(func(bc widgets.BuildContext, offset rendering.ViewportOffset) widgets.Widget {
		return s.pointerListener(widgets.NewRow([]widgets.Widget{
			// The open folds show their controls while the pointer is over the gutter.
			widgets.NewMouseRegion(widgets.NewColoredBox(colors.EditorGutterBackground,
				widgets.NewSizedBox(newGutterLayer(vm, offset, gutterColors), widgets.SizedBoxOpts{Width: foundation.Some(gutterWidth)})),
				widgets.MouseRegionOpts{
					OnEnter: func(*gestures.PointerEnterEvent) { vm.setGutterHover(true) },
					OnExit:  func(*gestures.PointerExitEvent) { vm.setGutterHover(false) },
				}),
			widgets.NewExpanded(widgets.NewMouseRegion(widgets.NewStack([]widgets.Widget{
				newOverlayLayer(overlayUnder, vm, offset, overlayColors),
				newTextLayer(vm, offset, colors.EditorForeground, tokenTheme, bracketColors, func(r *RenderEditorText) { s.textRO = r }),
				newOverlayLayer(overlayCursors, vm, offset, overlayColors),
			}, widgets.StackOpts{Fit: rendering.StackFitExpand}), widgets.MouseRegionOpts{Cursor: mouseCursor(s.mouseStyle(vm))})),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), gutterWidth)
	}, widgets.ScrollableOpts{
		AxisDirection: painting.AxisDirectionDown,
		Controller:    s.controller,
	})
	// The editor scrolls with the wheel and with a touchpad itself (SPEC §9.3); only a finger
	// or a pen on the screen drags the Scrollable.
	behavior := widgets.NewScrollBehavior().CopyWith(widgets.ScrollBehaviorCopy{
		DragDevices: []ui.PointerDeviceKind{ui.PointerDeviceKindTouch, ui.PointerDeviceKindStylus},
	})
	ruler := widgets.NewPositioned(widgets.NewIgnorePointer(newRulerLayer(vm, RulerColors{
		FindMatch: colors.EditorOverviewRulerFindMatch, RangeHighlight: colors.EditorOverviewRulerRangeHighlight,
	})), widgets.PositionedOpts{Top: foundation.Some(0.0), Bottom: foundation.Some(0.0), Right: foundation.Some(0.0), Width: foundation.Some(kit.DefaultVerticalScrollbarSize)})
	return widgets.NewColoredBox(colors.EditorBackground, kit.NewScrollableElement(
		widgets.NewStack([]widgets.Widget{widgets.NewScrollConfiguration(behavior, scrollable), ruler}, widgets.StackOpts{Fit: rendering.StackFitExpand}),
		s.scroll, kit.ScrollableElementOpts{InsetLeft: gutterWidth, UseShadows: true}))
}

// oppositeColor is the color with every channel inverted, which VS Code gives the text under a
// block cursor when the theme does not (editorCursor.background).
//
// Ported from VS Code: src/vs/editor/browser/viewParts/viewCursors/viewCursors.ts (registerThemingParticipant)
func oppositeColor(c ui.Color) ui.Color {
	return ui.NewColorFromARGB(c.Alpha(), 255-c.Red(), 255-c.Green(), 255-c.Blue())
}
