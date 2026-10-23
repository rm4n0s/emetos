package editor

import (
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

// Editor is the editor widget: the gutter and the text of a view model, scrolled together
// by a Scrollable. Cursors, selections and input come with later steps.
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
}

// DidUpdateWidget follows a new view model.
func (s *editorState) DidUpdateWidget(old *Editor) {
	if old.vm != s.Widget().vm {
		old.vm.RemoveListener(s.vmHandle)
		s.vmHandle = s.Widget().vm.AddListener(func() { s.SetState(func() {}) })
	}
}

// Dispose lets go of the controller and the listener.
func (s *editorState) Dispose() {
	s.Widget().vm.RemoveListener(s.vmHandle)
	s.controller.RemoveListener(s.scrollHandle)
	s.controller.Dispose()
}

// Build implements widgets.State.
func (s *editorState) Build(bc widgets.BuildContext) widgets.Widget {
	vm := s.Widget().vm
	colors := kit.ThemeOf(bc)
	gutterWidth := vm.GutterWidths(textFactory(bc)).Total()
	gutterColors := GutterColors{LineNumber: colors.EditorLineNumberForeground, ActiveLineNumber: colors.EditorLineNumberActiveForeground}
	return widgets.NewColoredBox(colors.EditorBackground,
		widgets.NewScrollable(func(bc widgets.BuildContext, offset rendering.ViewportOffset) widgets.Widget {
			return widgets.NewRow([]widgets.Widget{
				widgets.NewColoredBox(colors.EditorGutterBackground,
					widgets.NewSizedBox(newGutterLayer(vm, offset, gutterColors), widgets.SizedBoxOpts{Width: foundation.Some(gutterWidth)})),
				widgets.NewExpanded(newTextLayer(vm, offset, colors.EditorForeground)),
			}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
		}, widgets.ScrollableOpts{
			AxisDirection: painting.AxisDirectionDown,
			Controller:    s.controller,
		}))
}
