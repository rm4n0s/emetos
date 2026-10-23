package editor

import (
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
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
}

func newTextLayer(vm *ViewModel, offset rendering.ViewportOffset, color ui.Color) *textLayer {
	return &textLayer{LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(), vm: vm, offset: offset, color: color}
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *textLayer) CreateRenderObject(bc widgets.BuildContext) rendering.RenderObject {
	return NewRenderEditorText(w.vm, w.offset, textFactory(bc), w.color)
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *textLayer) UpdateRenderObject(bc widgets.BuildContext, ro rendering.RenderObject) {
	r := ro.(*RenderEditorText)
	r.SetViewModel(w.vm)
	r.SetOffset(w.offset)
	r.SetColor(w.color)
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

// focusObserver tells a view model when the window gets or loses the keyboard focus. Until
// the editor has a focus of its own, the editor has the focus when its window has.
type focusObserver struct {
	widgets.WidgetsBindingObserverBase
	vm func() *ViewModel
}

// DidChangeViewFocus implements widgets.WidgetsBindingObserver.
func (o *focusObserver) DidChangeViewFocus(e ui.ViewFocusEvent) {
	o.vm().SetFocused(e.State == ui.ViewFocusStateFocused)
}

// Editor is the editor widget: the text of a view model with its gutter, its selections, its
// current line and its cursors, scrolled together by a Scrollable. Input comes with later
// steps.
type Editor struct {
	widgets.StatefulWidget
	vm *ViewModel
}

// NewEditor makes the editor of a view model.
func NewEditor(vm *ViewModel) *Editor {
	return &Editor{StatefulWidget: widgets.NewStatefulWidgetBase(), vm: vm}
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
}

// InitState starts the scroll controller at the position the view model remembers, and
// follows the view model: its changes rebuild the editor, since the width of the gutter
// depends on the number of lines.
func (s *editorState) InitState() {
	vm := s.Widget().vm
	s.controller = widgets.NewScrollController(widgets.ScrollControllerOpts{
		InitialScrollOffset: vm.ScrollTop(),
		KeepScrollOffset:    foundation.Some(false),
	})
	s.scrollHandle = s.controller.AddListener(func() { s.Widget().vm.SetScrollTop(s.controller.Offset()) })
	s.vmHandle = vm.AddListener(func() { s.SetState(func() {}) })

	// The cursors blink with the timers of the window's binding, and stop with the focus.
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.observer = &focusObserver{vm: func() *ViewModel { return s.Widget().vm }}
	s.binding.AddObserver(s.observer)
	s.useTimers(vm)
}

// useTimers gives the view model the timers of the binding.
func (s *editorState) useTimers(vm *ViewModel) {
	gestures := s.binding.Gestures()
	vm.SetTimer(func(d time.Duration, fn func()) func() { return gestures.NewTimer(d, fn).Cancel })
}

// DidUpdateWidget follows a new view model.
func (s *editorState) DidUpdateWidget(old *Editor) {
	if old.vm != s.Widget().vm {
		old.vm.RemoveListener(s.vmHandle)
		old.vm.SetTimer(nil)
		s.vmHandle = s.Widget().vm.AddListener(func() { s.SetState(func() {}) })
		s.useTimers(s.Widget().vm)
	}
}

// Dispose lets go of the controller and the listener.
func (s *editorState) Dispose() {
	s.binding.RemoveObserver(s.observer)
	s.Widget().vm.SetTimer(nil)
	s.Widget().vm.RemoveListener(s.vmHandle)
	s.controller.RemoveListener(s.scrollHandle)
	s.controller.Dispose()
}

// Build implements widgets.State.
func (s *editorState) Build(bc widgets.BuildContext) widgets.Widget {
	vm := s.Widget().vm
	colors := kit.ThemeOf(bc)
	gutterWidth := vm.GutterWidths(textFactory(bc)).Total()
	gutterColors := GutterColors{
		LineNumber: colors.EditorLineNumberForeground, ActiveLineNumber: colors.EditorLineNumberActiveForeground,
		LineHighlight: colors.EditorLineHighlightBackground, LineHighlightBorder: colors.EditorLineHighlightBorder,
	}
	overlayColors := OverlayColors{
		Background: colors.EditorBackground, Selection: colors.EditorSelectionBackground,
		InactiveSelection: colors.EditorInactiveSelectionBackground, LineHighlight: colors.EditorLineHighlightBackground,
		LineHighlightBorder: colors.EditorLineHighlightBorder, Cursor: colors.EditorCursorForeground,
	}
	return widgets.NewColoredBox(colors.EditorBackground,
		widgets.NewScrollable(func(bc widgets.BuildContext, offset rendering.ViewportOffset) widgets.Widget {
			return widgets.NewRow([]widgets.Widget{
				widgets.NewColoredBox(colors.EditorGutterBackground,
					widgets.NewSizedBox(newGutterLayer(vm, offset, gutterColors), widgets.SizedBoxOpts{Width: foundation.Some(gutterWidth)})),
				widgets.NewExpanded(widgets.NewStack([]widgets.Widget{
					newOverlayLayer(overlayUnder, vm, offset, overlayColors),
					newTextLayer(vm, offset, colors.EditorForeground),
					newOverlayLayer(overlayCursors, vm, offset, overlayColors),
				}, widgets.StackOpts{Fit: rendering.StackFitExpand})),
			}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
		}, widgets.ScrollableOpts{
			AxisDirection: painting.AxisDirectionDown,
			Controller:    s.controller,
		}))
}
