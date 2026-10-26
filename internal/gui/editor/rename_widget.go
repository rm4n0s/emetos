package editor

import (
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The rename box, the preview of a refactoring and the way the editor gives them the keyboard (SPEC §9.12 "As built (step 12.8)").
// The box and the preview have focus nodes of their own, with scopes of context keys like the find widget's inputs, so that the
// keys of the editor's commands (Enter, Escape, Ctrl+Enter) reach the view model while they have the focus.
//
// Ported from VS Code: src/vs/editor/contrib/rename/browser/renameWidget.ts and renameWidget.css

// Metrics of the rename box.
const (
	renameMinWidth = 200.0
	renamePad      = 3.0
	renameRadius   = 4.0
	renameLabelH   = 18.0
)

// auxUI is the focus nodes, texts and scopes of the widgets that take the keyboard from the editor: the rename box and the
// preview of a refactoring.
type auxUI struct {
	renameNode, previewNode       *widgets.FocusNode
	renameText                    *widgets.TextEditingController
	renameScope, previewScope     *contextkey.Scope
	unregister                    []func()
	renameFocused, previewFocused bool
	caHandle, rnHandle, pvHandle  foundation.ListenerHandle
}

// initAuxUI makes the nodes and the scopes of the rename box and of the preview.
func (s *editorState) initAuxUI(vm *ViewModel) {
	opts := s.Widget().opts
	a := &s.aux
	a.renameNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "RenameInput"})
	a.previewNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "RefactorPreview"})
	a.renameText = widgets.NewTextEditingController("")
	if s.scope != nil {
		a.renameScope, a.previewScope = s.scope.Child(), s.scope.Child()
		for _, sc := range []*contextkey.Scope{a.renameScope, a.previewScope} {
			sc.Batch(func() {
				sc.Set("editorFocus", true)
				sc.Set("editorTextFocus", false)
				sc.Set("textInputFocus", false)
			})
		}
		a.renameScope.Set("renameInputFocused", false)
		if opts.Scopes != nil {
			a.unregister = append(a.unregister, opts.Scopes.Register(a.renameNode, a.renameScope), opts.Scopes.Register(a.previewNode, a.previewScope))
		}
	}
	s.useAuxUI(vm)
}

// useAuxUI connects a view model to the widgets: the view model asks them to move the focus, and they follow its state.
func (s *editorState) useAuxUI(vm *ViewModel) {
	a := &s.aux
	vm.SetRenameUI(RenameUI{Focus: s.focusRename, Return: func() { s.node.RequestFocus() }})
	vm.SetPreviewUI(PreviewUI{Focus: s.focusPreview, Return: s.returnFocus})
	a.caHandle = vm.AddCodeActionListener(s.hoverChanged)
	a.rnHandle = vm.AddRenameListener(s.hoverChanged)
	a.pvHandle = vm.AddPreviewListener(s.hoverChanged)
}

// releaseAuxUI disconnects a view model from the widgets.
func (s *editorState) releaseAuxUI(vm *ViewModel) {
	a := &s.aux
	vm.RemoveCodeActionListener(a.caHandle)
	vm.RemoveRenameListener(a.rnHandle)
	vm.RemovePreviewListener(a.pvHandle)
	vm.SetRenameUI(RenameUI{})
	vm.SetPreviewUI(PreviewUI{})
	// the widgets are going: the context keys are not updated for them (the listeners of the selection belong to a window that
	// may be going too)
	vm.quietKeys = true
	vm.RenameCancel()
	vm.PreviewDiscard()
	vm.CodeActionHide()
	vm.quietKeys = false
}

// notifyKeys tells the listeners of the selection that a widget which changes the context keys opened or closed.
func (vm *ViewModel) notifyKeys() {
	if !vm.quietKeys {
		vm.sel.NotifyListeners()
	}
}

// disposeAuxUI lets go of the nodes.
func (s *editorState) disposeAuxUI() {
	a := &s.aux
	s.releaseAuxUI(s.Widget().vm)
	for _, unregister := range a.unregister {
		unregister()
	}
	a.unregister = nil
	a.renameNode.Dispose()
	a.previewNode.Dispose()
	a.renameText.Dispose()
}

// auxFocusChanged follows the focus of the rename input and of the preview: the context keys, and the editor's commands that
// keep working while they have it.
func (s *editorState) auxFocusChanged() {
	if !s.Mounted() {
		return // a node that loses the focus as the editor goes away
	}
	a := &s.aux
	a.renameFocused, a.previewFocused = a.renameNode.HasFocus(), a.previewNode.HasFocus()
	if a.renameScope != nil {
		a.renameScope.Set("renameInputFocused", a.renameFocused)
	}
	s.findFocusChanged()
}

// focusRename gives the focus to the input of the rename box as soon as it is on screen, with the name selected.
func (s *editorState) focusRename() {
	a := &s.aux
	vm := s.Widget().vm
	if b := vm.RenameBox(); b != nil {
		a.renameText.SetText(b.Input)
	}
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() && vm.RenameVisible() {
			a.renameNode.RequestFocus()
			a.renameText.SetSelection(services.NewTextSelection(0, len(a.renameText.Text())))
		}
	})
}

// focusPreview gives the focus to the preview when it is on screen.
func (s *editorState) focusPreview() {
	vm := s.Widget().vm
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() && vm.RefactorPreview() != nil {
			s.aux.previewNode.RequestFocus()
		}
	})
}

// returnFocus gives the focus back to the editor when the rename box or the preview is gone.
func (s *editorState) returnFocus() {
	if s.Mounted() {
		s.node.RequestFocus()
	}
}

// keyLabel is the label of the key of a command, from the host, or the default.
func (vm *ViewModel) keyLabel(command, def string) string {
	if h := vm.sg.host; h != nil && h.KeyLabel != nil {
		if k := h.KeyLabel(command); k != "" {
			return k
		}
	}
	return def
}

// renameLayer is the box over the symbol that is renamed: the input with the name, and, with the preview on, a line that says
// which keys rename and which preview.
func (s *editorState) renameLayer(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	b := vm.RenameBox()
	if b == nil || s.textRO == nil || !s.textRO.Attached() || vm.viewport <= 0 {
		return nil
	}
	rect := s.textRO.RectOf(b.Range.Start)
	if rect.Bottom < 0 || rect.Top > vm.viewport {
		return nil
	}
	origin := s.gutterWidth
	if vm.mm.info.Show && vm.Options().Minimap.Side == "left" {
		origin += vm.mm.info.Width
	}
	charW := math.Max(vm.opts.FontSize*0.6, 1)
	chars := max(utf8.RuneCountInString(b.Text), utf8.RuneCountInString(b.Input))
	width := math.Max(renameMinWidth, float64(chars)*charW+48)
	hint := ""
	if b.Preview {
		hint = vm.keyLabel("acceptRenameInput", "Enter") + " to Rename, " + vm.keyLabel("acceptRenameInputWithPreview", "Ctrl+Enter") + " to Preview"
		// the line under the name is shown whole: the box grows to hold it
		width = math.Max(width, float64(utf8.RuneCountInString(hint))*12*0.62+2*renamePad+8)
	}
	left := origin + rect.Left - renamePad - 5
	if viewW, _ := vm.ViewportSize(); viewW > 0 {
		left = math.Max(math.Min(left, origin+viewW-width), 0)
	}
	input := kit.NewInputBox(s.aux.renameText, s.aux.renameNode, kit.InputBoxOpts{
		OnChanged: vm.RenameSetInput, OnFocusChange: func(bool) { s.auxFocusChanged() },
	})
	rows := []widgets.Widget{input}
	if b.Preview {
		rows = append(rows, widgets.NewOpacity(0.8, widgets.NewSizedBox(
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(2, 0, 2, 0),
				widgets.NewAlign(actionText(bc, hint, 12, colors.EditorWidgetForeground, 0), widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})),
			widgets.SizedBoxOpts{Height: foundation.Some(renameLabelH)})))
	}
	box := widgets.NewContainer(
		widgets.NewColumn(rows, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
		widgets.ContainerOpts{Width: foundation.Some(width), Padding: painting.NewEdgeInsetsAll(renamePad), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.EditorWidgetBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(renameRadius),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), Offset: ui.Offset{Dy: 2}, BlurRadius: 8})},
		})})
	return widgets.NewStack([]widgets.Widget{
		widgets.NewPositioned(widgets.NewGestureDetector(box, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque}),
			widgets.PositionedOpts{Left: foundation.Some(left), Top: foundation.Some(math.Max(rect.Top-renamePad-1, 0))}),
	}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// Metrics of the preview.
const (
	previewRowHeight = 22.0
	previewTitleH    = 34.0
	previewIndent    = 22.0
	previewFont      = 12.0
)

// previewLayer is the preview of a refactoring: a panel over the lower part of the editor with the title, the buttons and the
// list of files and changes with their check boxes.
func (s *editorState) previewLayer(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	p := vm.RefactorPreview()
	if p == nil || vm.viewport <= 0 {
		return nil
	}
	rows := p.Rows()
	var list []widgets.Widget
	for i, r := range rows {
		list = append(list, s.previewRow(bc, vm, colors, p, i, r))
	}
	want := previewTitleH + float64(len(rows))*previewRowHeight + 6
	height := math.Min(want, math.Max(vm.viewport*0.7, 120))
	title := widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(10, 0, 6, 0), widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(actionText(bc, p.Label, 12, colors.EditorWidgetForeground, ui.FontWeightBold)),
			kit.NewButton("Apply", kit.ButtonOpts{Small: true, OnPressed: vm.PreviewApply}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			kit.NewButton("Discard", kit.ButtonOpts{Small: true, Secondary: true, OnPressed: vm.PreviewDiscard}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.ContainerOpts{Height: foundation.Some(previewTitleH), Alignment: painting.AlignmentCenterLeft,
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)})},
			})})
	body := widgets.NewColumn([]widgets.Widget{
		title,
		widgets.NewExpanded(widgets.NewSingleChildScrollView(widgets.NewColumn(list, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
			widgets.SingleChildScrollViewOpts{Primary: foundation.Some(false)})),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	panel := widgets.NewContainer(widgets.NewFocus(body, widgets.FocusOpts{FocusNode: s.aux.previewNode, OnFocusChange: func(bool) { s.auxFocusChanged() }}),
		widgets.ContainerOpts{Height: foundation.Some(height), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:     foundation.Some(colors.EditorWidgetBackground),
			Border:    painting.Border{Top: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)})},
			BoxShadow: []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), Offset: ui.Offset{Dy: -2}, BlurRadius: 8})},
		})})
	return widgets.NewStack([]widgets.Widget{
		widgets.NewPositioned(widgets.NewGestureDetector(panel, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque}),
			widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Bottom: foundation.Some(0.0)}),
	}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// previewRow is a row of the preview: a file with its folder, or a change with the text around it.
func (s *editorState) previewRow(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, p *RefactorPreview, i int, r PreviewRow) widgets.Widget {
	f := p.Files[r.File]
	checked, partial := p.Checked(r)
	focused := i == p.Focus
	fg := colors.EditorWidgetForeground
	bg := ui.Color{}
	if focused {
		bg = colors.ListHoverBackground
	}
	box := kit.NewCheckbox("", kit.CheckboxOpts{Checked: checked && !partial, OnChanged: func(bool) { vm.PreviewToggle(i) }})
	var content []widgets.Widget
	indent := 8.0
	if r.Change < 0 {
		label := f.Name
		switch f.Operation {
		case "create":
			label = "Create " + f.Name
		case "delete":
			label = "Delete " + f.Name
		case "rename":
			label = "Rename " + f.Name + " to " + f.Target
		}
		content = append(content, actionText(bc, label, previewFont, fg, ui.FontWeightBold))
		if f.Dir != "" {
			content = append(content, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}), actionText(bc, f.Dir, previewFont, colors.DescriptionForeground, 0))
		}
		if n := len(f.Changes); n > 0 {
			count := "1 change"
			if n > 1 {
				count = strconv.Itoa(n) + " changes"
			}
			if partial {
				count = "partly, " + count
			}
			content = append(content, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}), actionText(bc, count, previewFont, colors.DescriptionForeground, 0))
		}
	} else {
		indent += previewIndent
		c := f.Changes[r.Change]
		mono := vm.suggestStyle(fg, previewFont)
		removed := colors.EditorWarningForeground
		spans := []painting.InlineSpan{
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("Ln " + strconv.Itoa(c.Line) + "  "), Style: mono.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.DescriptionForeground)})}),
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(c.Before)}),
		}
		if c.Old != "" {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(c.Old), Style: mono.CopyWith(painting.TextStyleOpts{
				Decoration: foundation.Some(ui.TextDecorationLineThrough), Color: foundation.Some(removed)})}))
		}
		if c.New != "" {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(c.New), Style: mono.CopyWith(painting.TextStyleOpts{
				FontWeight: ui.FontWeightBold, Color: foundation.Some(colors.TextLinkForeground)})}))
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(c.After)}))
		content = append(content, widgets.NewExpanded(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: mono, Children: spans}),
			widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
	}
	row := widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(indent, 0, 8, 0), widgets.NewRow(append([]widgets.Widget{
			widgets.NewSizedBox(widgets.NewCenter(box, widgets.CenterOpts{}), widgets.SizedBoxOpts{Width: foundation.Some(22.0)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
		}, content...), widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.ContainerOpts{Height: foundation.Some(previewRowHeight), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg)})})
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { vm.PreviewFocus(i) }}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click, OnEnter: func(*gestures.PointerEnterEvent) {}})
}
