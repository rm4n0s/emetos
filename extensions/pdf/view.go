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

// The sizes of the view: the height of the toolbar, the width of the page box, of the Outline panel, of the find widget and of its
// count.
const (
	toolbarHeight  = 32
	pageBoxWidth   = 52
	outlineWidth   = 220
	findWidth      = 360
	findCountWidth = 69
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
		main := s.pagesArea()
		if v.text.findShown {
			pages := main
			main = widgets.NewLayoutBuilder(func(lbc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
				// the find widget is findWidth wide, less in a narrow view: its box takes what is left
				width := max(min(float64(findWidth), c.MaxWidth-32), 120)
				return widgets.NewStack([]widgets.Widget{
					widgets.NewPositionedFill(pages),
					widgets.NewPositioned(s.findBar(lbc), widgets.PositionedOpts{Top: foundation.Some(0.0), Right: foundation.Some(16.0),
						Width: foundation.Some(width)}),
				})
			})
		}
		row := []widgets.Widget{widgets.NewExpanded(main)}
		if v.text.outlineShown {
			row = append([]widgets.Widget{s.outlinePanel(bc), widgets.NewContainer(nil, widgets.ContainerOpts{
				Width: foundation.Some(1.0), Color: foundation.Some(colors.WidgetBorder)})}, row...)
		}
		return widgets.NewFocus(widgets.NewColumn([]widgets.Widget{
			s.toolbar(bc),
			widgets.NewExpanded(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
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
		gap,
		ui.NewIconButton("list-tree", ui.IconButtonOpts{Tooltip: "Outline", Checked: v.text.outlineShown, OnPressed: v.toggleOutline}),
		ui.NewIconButton("search", ui.IconButtonOpts{Tooltip: "Find (Ctrl+F)", Checked: v.text.findShown, OnPressed: func() {
			if v.text.findShown {
				v.closeFind()
			} else {
				v.openFind("")
			}
		}}),
		ui.NewIconButton("copy", ui.IconButtonOpts{Tooltip: "Copy Page Text", OnPressed: v.copyPageText}),
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
		if marks := s.highlights(bc, i); len(marks) > 0 {
			child = widgets.NewStack(append([]widgets.Widget{widgets.NewPositionedFill(child)}, marks...))
		}
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

// highlightOpacity is the most opaque a highlight is drawn over a page.
const highlightOpacity = 0.4

// highlights are the boxes of the matches on a page, in the theme's find colors (the current match in its own).
func (s *viewerViewState) highlights(bc widgets.BuildContext, page int) []widgets.Widget {
	v := s.Widget().v
	if !v.text.findShown || len(v.text.matches) == 0 {
		return nil
	}
	colors := ui.Theme(bc)
	k := v.lay.rects[page].w / v.pages[page].Width
	var out []widgets.Widget
	for mi, m := range v.text.matches {
		if m.page != page {
			continue
		}
		color, border := colors.EditorFindMatchHighlightBackground, colors.EditorFindMatchHighlightBorder
		if mi == v.text.current {
			color, border = colors.EditorFindMatchBackground, colors.EditorFindMatchBorder
		}
		// the editor draws these behind the text; over the picture of a page, the word must show through
		color = color.WithOpacity(min(color.Opacity(), highlightOpacity))
		for _, b := range m.boxes {
			deco := &painting.BoxDecoration{Color: foundation.Some(color)}
			if border.Alpha() > 0 {
				deco.Border = painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)})
			}
			out = append(out, widgets.NewPositioned(widgets.NewDecoratedBox(deco, nil), widgets.PositionedOpts{
				Left: foundation.Some(b.x0 * k), Top: foundation.Some(b.y0 * k),
				Width: foundation.Some((b.x1 - b.x0) * k), Height: foundation.Some((b.y1 - b.y0) * k),
			}))
		}
	}
	return out
}

// outlinePanel is the Outline panel on the left of the pages.
func (s *viewerViewState) outlinePanel(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget().v
	colors := ui.Theme(bc)
	note := func(text string) widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 4, 12, 4), widgets.NewText(text,
			widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
	}
	var body widgets.Widget
	switch {
	case v.text.outline == outlineReading || v.text.outline == outlineUnread:
		body = note("Reading the outline...")
	case v.text.outline == outlineFailed:
		body = note("The outline cannot be read: " + v.text.outlineErr)
	case len(v.text.outlineItems) == 0:
		body = note("This document has no outline.")
	default:
		body = ui.NewTree(v.text.tree, ui.TreeOpts{Render: s.outlineRow, OnOpen: v.openOutlineEntry, OnSelect: v.openOutlineEntry})
	}
	header := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 12, 6), widgets.NewText("OUTLINE",
		widgets.TextOpts{Style: ui.TextStyle(bc, 11, colors.Foreground)}))
	return widgets.NewContainer(widgets.NewColumn([]widgets.Widget{header, widgets.NewExpanded(body)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), widgets.ContainerOpts{
		Width: foundation.Some(float64(outlineWidth)), Color: foundation.Some(colors.SideBarBackground)})
}

// outlineRow is a row of the outline: the title, and the page dimmed on the right.
func (s *viewerViewState) outlineRow(bc widgets.BuildContext, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	row, _ := node.Data.(outlineRow)
	title := widgets.NewExpanded(ui.NewHighlightedLabel(bc, node.Label, st))
	if row.page <= 0 {
		return widgets.NewRow([]widgets.Widget{title})
	}
	return widgets.NewRow([]widgets.Widget{title, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(6, 0, 8, 0),
		widgets.NewText(strconv.Itoa(row.page), widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))})
}

// findBar is the find widget over the top right of the pages: the box, Match Case, the count, Previous and Next Match, and Close.
// Enter goes to the next match, Shift+Enter to the previous, Escape closes it.
func (s *viewerViewState) findBar(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget().v
	colors := ui.Theme(bc)
	box := ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: v.text.findBox, Placeholder: "Find",
		OnChanged: v.setFindText,
		Trailing: []widgets.Widget{ui.NewToggle("case-sensitive", ui.ToggleOpts{Tooltip: "Match Case", Checked: v.text.matchCase,
			OnChanged: v.setMatchCase})},
		KeyHook: func(e services.KeyEvent) widgets.KeyEventResult {
			if _, down := e.(*services.KeyDownEvent); !down {
				if _, repeat := e.(*services.KeyRepeatEvent); !repeat {
					return widgets.KeyEventResultIgnored
				}
			}
			switch e.LogicalKey() {
			case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter:
				if widgets.WidgetsBindingOf(s.Context()).Keyboard().IsShiftPressed() {
					v.moveMatch(-1)
				} else {
					v.moveMatch(1)
				}
				return widgets.KeyEventResultHandled
			case services.LogicalKeyboardKeyEscape:
				v.closeFind()
				return widgets.KeyEventResultHandled
			}
			return widgets.KeyEventResultIgnored
		}})
	count := v.matchesLabel()
	countColor := colors.Foreground
	if count == "No results" {
		countColor = colors.ErrorForeground
	}
	none := len(v.text.matches) == 0
	row := widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(box),
		widgets.NewSizedBox(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(6, 0, 0, 0), widgets.NewText(count,
			widgets.TextOpts{Style: ui.TextStyle(bc, 12, countColor), MaxLines: foundation.Some(1)})),
			widgets.SizedBoxOpts{Width: foundation.Some(float64(findCountWidth))}),
		ui.NewIconButton("arrow-up", ui.IconButtonOpts{Tooltip: "Previous Match (Shift+Enter)", Disabled: none, OnPressed: func() { v.moveMatch(-1) }}),
		ui.NewIconButton("arrow-down", ui.IconButtonOpts{Tooltip: "Next Match (Enter)", Disabled: none, OnPressed: func() { v.moveMatch(1) }}),
		ui.NewIconButton("close", ui.IconButtonOpts{Tooltip: "Close (Escape)", OnPressed: v.closeFind}),
	})
	return widgets.NewContainer(row, widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsAll(4),
		Decoration: &painting.BoxDecoration{
			Color:        foundation.Some(colors.EditorWidgetBackground),
			BorderRadius: painting.NewBorderRadiusVertical(gui.Radius{}, gui.NewRadiusCircular(4)),
			BoxShadow:    []painting.BoxShadow{{Shadow: gui.Shadow{Color: colors.WidgetShadow, BlurRadius: 8, Offset: gui.Offset{Dy: 2}}}},
		},
	})
}
