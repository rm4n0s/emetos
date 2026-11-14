package pdf

import (
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The widget of the viewer (SPEC §10.37 "PDF"): the toolbar at the top (Previous and Next page, the page box, Zoom Out, Zoom In and the
// zoom pick) and the pages under it, on the editor's background, with the editor's overlay scrollbars.

// toolbarHeight is the height of the toolbar, and pageBoxWidth the width of the page box.
const (
	toolbarHeight = 32
	pageBoxWidth  = 52
)

// pageColor is the color of a page before it is drawn.
var pageColor = gui.NewColorFromARGB(0xff, 0xff, 0xff, 0xff)

// build is the Build of the tab.
func (v *viewer) build(widgets.BuildContext) widgets.Widget {
	return &viewerView{StatefulWidget: widgets.NewStatefulWidgetBase(), v: v}
}

// pagesScroll is the scroll position of the pages as the editor's scrollbars see it.
type pagesScroll struct {
	*foundation.ChangeNotifier
	v *viewer
}

func newPagesScroll(v *viewer) *pagesScroll {
	return &pagesScroll{ChangeNotifier: foundation.NewChangeNotifier(), v: v}
}

// Metrics implements ui.ScrollModel.
func (s *pagesScroll) Metrics() ui.ScrollMetrics {
	v := s.v
	return ui.ScrollMetrics{ViewportWidth: v.viewW, ViewportHeight: v.viewH, ContentWidth: v.lay.width, ContentHeight: v.lay.height,
		Left: v.left, Top: v.top}
}

// ScrollTo implements ui.ScrollModel.
func (s *pagesScroll) ScrollTo(left, top float64) { s.v.scrollTo(left, top) }

// viewerView is the widget of a tab; its state is the tab's.
type viewerView struct {
	widgets.StatefulWidget
	v *viewer
}

// CreateState implements widgets.StatefulWidget.
func (w *viewerView) CreateState() widgets.State { return &viewerViewState{} }

// viewerViewState is the state of the widget: the focus of the keys, the password box and the timers of the binding.
type viewerViewState struct {
	widgets.StateOf[*viewerView]
	focus    *widgets.FocusNode
	password *ui.InputBoxController
	// afterFunc runs a function on the UI thread after a time (the binding's timers); nil before the first build.
	afterFunc func(time.Duration, func()) func()
	// asked tells that the scheduling of the drawing was posted.
	asked bool
}

// InitState implements widgets.State.
func (s *viewerViewState) InitState() {
	s.Widget().v.view = s
	s.focus = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "pdf"})
	s.password = ui.NewInputBoxController("")
}

// DidUpdateWidget implements widgets.State.
func (s *viewerViewState) DidUpdateWidget(old *viewerView) {
	if old.v != s.Widget().v {
		if old.v.view == s {
			old.v.view = nil
		}
		s.Widget().v.view = s
	}
}

// Dispose implements widgets.State.
func (s *viewerViewState) Dispose() {
	if v := s.Widget().v; v.view == s {
		v.view = nil
	}
	s.focus.Dispose()
	s.password.Dispose()
}

// changed builds the widget again.
func (s *viewerViewState) changed() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// Build implements widgets.State.
func (s *viewerViewState) Build(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget().v
	colors := ui.Theme(bc)
	if s.afterFunc == nil {
		painting := widgets.WidgetsBindingOf(bc).Painting()
		s.afterFunc = func(d time.Duration, fn func()) func() { return painting.AfterFunc(d, fn) }
	}
	var body widgets.Widget
	switch {
	case v.deleted:
		body = messageWithButtons(bc, "The file was deleted.", nil)
	case v.problem != nil:
		body = v.x.problemView(bc, v.problem, v.load)
	case v.locked:
		body = s.passwordBox(bc)
	case len(v.pages) == 0:
		body = widgets.NewSizedBox(nil)
	default:
		return widgets.NewFocus(widgets.NewColumn([]widgets.Widget{
			s.toolbar(bc),
			widgets.NewExpanded(s.pagesArea()),
		}), widgets.FocusOpts{FocusNode: s.focus, Autofocus: true, OnKeyEvent: s.key})
	}
	return widgets.NewColoredBox(colors.EditorBackground, body)
}

// passwordBox asks for the password of the document.
func (s *viewerViewState) passwordBox(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget().v
	colors := ui.Theme(bc)
	text := "This document is protected by a password. Enter the password to open it."
	submit := func() {
		pw := s.password.Value()
		s.password.SetValue("")
		v.submitPassword(pw)
	}
	field := ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: s.password, Placeholder: "Password", Obscure: true,
		Width: foundation.Some(260.0), Invalid: v.wrongPassword, OnSubmit: func(string) { submit() }})
	children := []widgets.Widget{
		widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.Foreground), TextAlign: gui.TextAlignCenter}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(12.0)}),
		widgets.NewRow([]widgets.Widget{
			field,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
			ui.NewButton("Open", ui.ButtonOpts{Disabled: v.loading, OnPressed: submit}),
		}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}),
	}
	if v.wrongPassword {
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(8.0)}),
			widgets.NewText("The password is not correct.", widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.ErrorForeground)}))
	}
	return widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(16),
		widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})))
}

// toolbar is the bar above the pages.
func (s *viewerViewState) toolbar(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget().v
	colors := ui.Theme(bc)
	cur, n := v.currentPage(), len(v.pages)
	options := zoomOptions()
	labels := make([]ui.DropdownOption, len(options))
	selected := 0
	for i, z := range options {
		labels[i] = ui.DropdownOption{Label: z.Label()}
		if z == v.zoom {
			selected = i
		}
	}
	gap := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)})
	scale := v.shownScale()
	_, canZoomIn := zoomIn(scale)
	_, canZoomOut := zoomOut(scale)
	row := widgets.NewRow([]widgets.Widget{
		ui.NewIconButton("arrow-up", ui.IconButtonOpts{Tooltip: "Previous Page", Disabled: cur <= 0, OnPressed: func() { v.goToPage(v.currentPage() - 1) }}),
		ui.NewIconButton("arrow-down", ui.IconButtonOpts{Tooltip: "Next Page", Disabled: cur >= n-1, OnPressed: func() { v.goToPage(v.currentPage() + 1) }}),
		gap,
		ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: v.pageBox, Width: foundation.Some(float64(pageBoxWidth)), OnSubmit: v.submitPage,
			OnFocusChange: func(focused bool) {
				if focused {
					v.pageBox.SelectAll()
				} else {
					v.syncPageBox()
				}
			}}),
		gap,
		widgets.NewText("/ "+strconv.Itoa(n), widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.Foreground)}),
		widgets.NewSpacer(),
		ui.NewIconButton("zoom-out", ui.IconButtonOpts{Tooltip: "Zoom Out (Ctrl+-)", Disabled: !canZoomOut, OnPressed: v.zoomOut}),
		ui.NewIconButton("zoom-in", ui.IconButtonOpts{Tooltip: "Zoom In (Ctrl+=)", Disabled: !canZoomIn, OnPressed: v.zoomIn}),
		gap,
		ui.NewDropdown(labels, ui.DropdownOpts{Selected: selected, Width: foundation.Some(110.0), OnChanged: func(i int) {
			v.setZoom(options[i])
			s.focus.RequestFocus()
		}}),
	})
	return widgets.NewContainer(row, widgets.ContainerOpts{
		Height: foundation.Some(float64(toolbarHeight)), Color: foundation.Some(colors.EditorBackground),
		Padding: painting.NewEdgeInsetsSymmetric(0, 8),
	})
}

// pagesArea is the pages in view, with the editor's scrollbars, the wheel and a click that takes the focus.
func (s *viewerViewState) pagesArea() widgets.Widget {
	v := s.Widget().v
	pages := widgets.NewLayoutBuilder(func(lbc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		colors := ui.Theme(lbc)
		if v.setViewport(c.MaxWidth, c.MaxHeight, widgets.MediaQueryDevicePixelRatioOf(lbc)) {
			v.scroll.NotifyListeners()
		}
		if !s.asked {
			s.asked = true
			widgets.WidgetsBindingOf(lbc).Post(func() { // not during a layout
				s.asked = false
				if s.Mounted() {
					v.syncPageBox()
					v.schedule()
				}
			})
		}
		var children []widgets.Widget
		first, last := v.lay.visible(v.top, v.top+v.viewH)
		for i := first; i >= 0 && i <= last; i++ {
			r := v.lay.rects[i]
			children = append(children, widgets.NewPositioned(s.page(lbc, i), widgets.PositionedOpts{
				Left: foundation.Some(r.x - v.left), Top: foundation.Some(r.y - v.top), Width: foundation.Some(r.w), Height: foundation.Some(r.h),
			}))
		}
		return widgets.NewColoredBox(colors.EditorBackground, widgets.NewClipRect(widgets.NewSizedBox(
			widgets.NewStack(children), widgets.SizedBoxOpts{Width: foundation.Some(c.MaxWidth), Height: foundation.Some(c.MaxHeight)})))
	})
	listener := widgets.NewListener(widgets.NewGestureDetector(pages, widgets.GestureDetectorOpts{OnTap: func() { s.focus.RequestFocus() }}),
		widgets.ListenerOpts{OnPointerSignal: s.pointerSignal})
	return ui.NewScrollable(listener, ui.ScrollableOpts{Model: v.scroll, HandleMouseWheel: foundation.Some(true)})
}

// page is one page: its picture at the resolution of the scale, else a picture of another resolution scaled, else white; a page that
// cannot be drawn says so.
func (s *viewerViewState) page(bc widgets.BuildContext, i int) widgets.Widget {
	v := s.Widget().v
	d := v.cache.get(i, v.dpiFor(i))
	if d == nil {
		d = v.cache.best(i)
	}
	var child widgets.Widget = widgets.NewSizedBox(nil)
	if d != nil {
		child = widgets.NewRawImage(d.img, widgets.RawImageOpts{Scale: 1, Fit: painting.BoxFitFill, FilterQuality: gui.FilterQualityMedium})
	} else if msg, failed := v.failed[i]; failed {
		child = widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(8), widgets.NewText("This page cannot be drawn: "+msg,
			widgets.TextOpts{Style: ui.TextStyle(bc, 12, gui.NewColorFromARGB(0xff, 0x61, 0x61, 0x61)), TextAlign: gui.TextAlignCenter})))
	}
	return widgets.NewDecoratedBox(&painting.BoxDecoration{
		Color:     foundation.Some(pageColor),
		BoxShadow: []painting.BoxShadow{{Shadow: gui.Shadow{Color: ui.Theme(bc).WidgetShadow, BlurRadius: 4, Offset: gui.Offset{Dy: 1}}}},
	}, child)
}

// pointerSignal zooms with Ctrl and the wheel (a pinch of a touchpad comes as Ctrl and the wheel); the scrollbars' element scrolls
// otherwise.
func (s *viewerViewState) pointerSignal(e gestures.PointerSignalEvent) {
	scroll, ok := e.(*gestures.PointerScrollEvent)
	binding := widgets.WidgetsBindingOf(s.Context())
	if !ok || !binding.Keyboard().IsControlPressed() || scroll.ScrollDelta().Dy == 0 {
		return
	}
	v := s.Widget().v
	binding.Gestures().PointerSignalResolver.Register(e, func(gestures.PointerSignalEvent) {
		if scroll.ScrollDelta().Dy < 0 {
			v.zoomIn()
		} else {
			v.zoomOut()
		}
		scroll.Respond(false)
	})
}

// key is the keys of the pages: the arrows, Page Up and Page Down, Space and Shift+Space, Home and End scroll.
func (s *viewerViewState) key(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	v := s.Widget().v
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	if kb.IsControlPressed() || kb.IsAltPressed() || kb.IsMetaPressed() {
		return widgets.KeyEventResultIgnored
	}
	page := max(v.viewH-pageOverlap, lineStep)
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyArrowUp:
		v.scrollTo(v.left, v.top-lineStep)
	case services.LogicalKeyboardKeyArrowDown:
		v.scrollTo(v.left, v.top+lineStep)
	case services.LogicalKeyboardKeyArrowLeft:
		v.scrollTo(v.left-lineStep, v.top)
	case services.LogicalKeyboardKeyArrowRight:
		v.scrollTo(v.left+lineStep, v.top)
	case services.LogicalKeyboardKeyPageUp:
		v.scrollTo(v.left, v.top-page)
	case services.LogicalKeyboardKeyPageDown:
		v.scrollTo(v.left, v.top+page)
	case services.LogicalKeyboardKeySpace:
		if kb.IsShiftPressed() {
			v.scrollTo(v.left, v.top-page)
		} else {
			v.scrollTo(v.left, v.top+page)
		}
	case services.LogicalKeyboardKeyHome:
		v.scrollTo(v.left, 0)
	case services.LogicalKeyboardKeyEnd:
		v.scrollTo(v.left, v.lay.height)
	default:
		return widgets.KeyEventResultIgnored
	}
	return widgets.KeyEventResultHandled
}

// messageWithButtons is a message in the middle of the view with a row of buttons under it.
func messageWithButtons(bc widgets.BuildContext, text string, buttons []widgets.Widget) widgets.Widget {
	colors := ui.Theme(bc)
	children := []widgets.Widget{widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.Foreground), TextAlign: gui.TextAlignCenter})}
	if len(buttons) > 0 { // in rows that wrap in a narrow view
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(12.0)}),
			widgets.NewWrap(buttons, widgets.WrapOpts{Alignment: rendering.WrapAlignmentCenter, Spacing: 8, RunSpacing: 8}))
	}
	return widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(16),
		widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})))
}

// problemView is the message of what keeps the document from being shown, with the buttons of the requirement when Poppler is missing
// (Copy Install Command, Set Path..., Instructions) and Check Again.
func (x *Extension) problemView(bc widgets.BuildContext, err error, reload func()) widgets.Widget {
	text, docs, needsPoppler := problemText(err)
	var buttons []widgets.Widget
	if needsPoppler {
		if _, ok := requirements()[0].Install.CommandFor(extension.SystemFamily()); ok {
			buttons = append(buttons, ui.NewButton("Copy Install Command", ui.ButtonOpts{OnPressed: x.copyInstallCommand}))
		}
		buttons = append(buttons, ui.NewButton("Set Path...", ui.ButtonOpts{Secondary: true, OnPressed: func() { x.setPopplerPath(reload) }}))
	}
	if docs != "" {
		buttons = append(buttons, ui.NewButton("Instructions", ui.ButtonOpts{Secondary: true, OnPressed: func() { x.openDocs(docs) }}))
	}
	buttons = append(buttons, ui.NewButton("Check Again", ui.ButtonOpts{Secondary: !needsPoppler || len(buttons) > 0, OnPressed: reload}))
	return messageWithButtons(bc, text, buttons)
}

// copyInstallCommand copies the command that installs Poppler on this system.
func (x *Extension) copyInstallCommand() {
	cmd, ok := requirements()[0].Install.CommandFor(extension.SystemFamily())
	if !ok {
		return
	}
	if err := x.api.Clipboard().WriteText(cmd.Command); err != nil {
		x.api.Log().Warn("the install command cannot be copied", "err", err)
		return
	}
	x.api.Notifications().Info("Copied: " + cmd.Command)
}

// openDocs opens instructions in the browser.
func (x *Extension) openDocs(uri string) {
	if err := x.api.Workspace().OpenURI(x.ctx, uri); err != nil {
		x.api.Log().Warn("the instructions cannot be opened", "uri", uri, "err", err)
	}
}

// setPopplerPath asks for the folder of Poppler's programs and saves it in pdf.popplerPath; then reload runs.
func (x *Extension) setPopplerPath(reload func()) {
	if x.api.Origins().Current() != extension.HostOrigin {
		x.api.Notifications().Info("The folder of Poppler is a setting of the host and cannot be changed from here.")
		return
	}
	current, _ := extension.SettingValue[string](x.api.Settings(), "pdf.popplerPath")
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Folder of Poppler's programs", Value: current,
		Message: "Saved as pdf.popplerPath in your settings. Leave it empty to look in PATH.",
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		if err := x.api.Settings().Update("pdf.popplerPath", strings.TrimSpace(value)); err != nil {
			x.api.Log().Warn("the folder of Poppler cannot be saved", "err", err)
			return
		}
		reload()
	})
	if err != nil {
		x.api.Log().Warn("the folder of Poppler cannot be asked", "err", err)
	}
}
