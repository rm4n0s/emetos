package editor

import (
	"math"
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The widget of the peek view: the zone under the line of the cursor, with a title, an editor over the selected location (left,
// seven tenths of the width) and the list of locations grouped by file (right). The keys go to the editor that has the keyboard
// (SPEC §9.12 "As built (step 12.7)"); the mouse selects a location with a click and opens it with a double click.
//
// Ported from VS Code: src/vs/editor/contrib/gotoSymbol/browser/peek/referencesWidget.ts and
// src/vs/editor/contrib/peekView/browser/peekView.ts

// The metrics of the peek view.
const (
	peekFont       = 13.0
	peekRowHeight  = 22.0
	peekFrame      = 2.0
	peekEditorFlex = 7
	peekResultFlex = 3
)

// peekWidget is the widget of the zone.
type peekWidget struct {
	widgets.StatefulWidget
	vm *ViewModel
}

func newPeekWidget(vm *ViewModel) widgets.Widget {
	return &peekWidget{StatefulWidget: widgets.NewStatefulWidgetBase(), vm: vm}
}

// CreateState implements widgets.StatefulWidget.
func (w *peekWidget) CreateState() widgets.State { return &peekState{} }

type peekState struct {
	widgets.StateOf[*peekWidget]
	handle foundation.ListenerHandle
	scroll *widgets.ScrollControllerBase
	// shown is the row the list last scrolled to.
	shown int
}

// InitState follows the peek model.
func (s *peekState) InitState() {
	vm := s.Widget().vm
	s.scroll = widgets.NewScrollController(widgets.ScrollControllerOpts{KeepScrollOffset: foundation.Some(false)})
	s.handle = vm.AddPeekListener(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
	s.shown = -1
}

// Dispose stops following the model.
func (s *peekState) Dispose() {
	s.Widget().vm.RemovePeekListener(s.handle)
	s.scroll.Dispose()
}

// peekColors are the colors of the editor of the peek view: the theme's with the background of the peek view.
func peekColors(c kit.Colors) kit.Colors {
	c.EditorBackground = c.PeekViewEditorBackground
	c.EditorGutterBackground = c.PeekViewEditorGutterBackground
	return c
}

// Build implements widgets.State.
func (s *peekState) Build(bc widgets.BuildContext) widgets.Widget {
	vm := s.Widget().vm
	v := vm.Peek()
	if v == nil {
		return widgets.NewSizedBox(nil)
	}
	colors := kit.ThemeOf(bc)
	head := math.Ceil(vm.Options().LineHeight * 1.2)
	title := s.title(bc, colors, v, head)
	var editorPane widgets.Widget
	switch {
	case v.Current != nil:
		// the editor never takes the keyboard from the editor of the zone (ExcludeFocus): its keys are the commands of the peek
		editorPane = widgets.NewKeyedSubtree(widgets.NewExcludeFocus(kit.NewTheme(peekColors(colors), NewEditor(v.Current), kit.ThemeOpts{Tokens: kit.TokenThemeOf(bc), UIFont: kit.UIFontOf(bc)})), widgets.WidgetOpts{Key: foundation.NewValueKey("peek:" + v.CurrentURI.String())})
	default:
		text := "Loading..."
		if v.Failed != "" {
			text = "Cannot show the location: " + v.Failed
		}
		editorPane = widgets.NewColoredBox(colors.PeekViewEditorBackground, widgets.NewPadding(painting.NewEdgeInsetsAll(8),
			widgets.NewText(text, widgets.TextOpts{Style: kit.TextStyleOf(bc, peekFont, colors.PeekViewTitleDescriptionForeground)})))
	}
	body := []widgets.Widget{widgets.NewExpanded(editorPane, widgets.ExpandedOpts{Flex: foundation.Some(peekEditorFlex)})}
	if len(v.Refs) > 1 {
		body = append(body, widgets.NewExpanded(s.results(bc, colors, v), widgets.ExpandedOpts{Flex: foundation.Some(peekResultFlex)}))
	}
	side := painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PeekViewBorder), Width: foundation.Some(peekFrame)})
	// the zone is as wide as the text area, which the scrollbar (and with a minimap the start of it) lies over: the view stops short
	// of it, as VS Code's zone widgets are as wide as the content
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, kit.DefaultVerticalScrollbarSize, 0), widgets.NewContainer(
		widgets.NewColumn([]widgets.Widget{title, widgets.NewExpanded(widgets.NewRow(body, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))},
			widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(colors.PeekViewTitleBackground),
			Border: painting.Border{Top: side, Bottom: side},
		})}))
}

// title is the title area: the name of the file of the selected location, its folder, the number of locations, and the button
// that closes the view.
func (s *peekState) title(bc widgets.BuildContext, colors kit.Colors, v *PeekView, height float64) widgets.Widget {
	vm := s.Widget().vm
	label := kit.TextStyleOf(bc, peekFont, colors.PeekViewTitleLabelForeground)
	dim := kit.TextStyleOf(bc, peekFont, colors.PeekViewTitleDescriptionForeground)
	spans := []painting.InlineSpan{
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(v.Title), Style: label}),
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + v.Dir), Style: dim}),
	}
	meta := painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + v.Meta), Style: dim})
	close := widgets.NewMouseRegion(
		widgets.NewGestureDetector(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(6, 0), widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconClose),
			widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(colors.PeekViewTitleLabelForeground)})),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: vm.PeekClose}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
	return widgets.NewSizedBox(widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(16.0)}),
		widgets.NewExpanded(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: append(spans, meta)}),
			widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		close,
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.SizedBoxOpts{Height: foundation.Some(height)})
}

// results is the list of locations: a row for each file when there are several, and a row for each location.
func (s *peekState) results(bc widgets.BuildContext, colors kit.Colors, v *PeekView) widgets.Widget {
	vm := s.Widget().vm
	var rows []widgets.Widget
	selectedTop := 0.0
	for _, f := range v.Files {
		if len(v.Files) > 1 {
			rows = append(rows, s.fileRow(bc, colors, f))
		}
		for i := f.First; i < f.First+f.Count; i++ {
			if i == v.Selected {
				selectedTop = float64(len(rows)) * peekRowHeight
			}
			rows = append(rows, s.refRow(bc, colors, v, i, len(v.Files) > 1))
		}
	}
	if v.Selected != s.shown {
		s.shown = v.Selected
		top := selectedTop
		vm.post(func() {
			if !s.Mounted() || !s.scroll.HasClients() {
				return
			}
			pos := s.scroll.Position()
			view := pos.ViewportDimension()
			cur := s.scroll.Offset()
			switch {
			case top < cur:
				s.scroll.JumpTo(math.Max(top, 0))
			case top+peekRowHeight > cur+view:
				s.scroll.JumpTo(math.Min(top+peekRowHeight-view, pos.MaxScrollExtent()))
			}
		})
	}
	column := widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	list := widgets.NewRawScrollbar(
		widgets.NewSingleChildScrollView(column, widgets.SingleChildScrollViewOpts{Controller: s.scroll, Primary: foundation.Some(false)}),
		widgets.RawScrollbarOpts{Controller: s.scroll, Thickness: foundation.Some(6.0), ThumbColor: foundation.Some(colors.ScrollbarSliderBackground)})
	return widgets.NewColoredBox(colors.PeekViewResultBackground, list)
}

// fileRow is the row of a file: its name, its folder and the number of locations in it.
func (s *peekState) fileRow(bc widgets.BuildContext, colors kit.Colors, f PeekFile) widgets.Widget {
	name := kit.TextStyleOf(bc, peekFont, colors.PeekViewResultFileForeground)
	dim := kit.TextStyleOf(bc, peekFont, colors.PeekViewResultLineForeground)
	return widgets.NewSizedBox(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 0, 8, 0), widgets.NewRow([]widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconSymbolFile), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(colors.PeekViewResultFileForeground)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
		widgets.NewExpanded(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: []painting.InlineSpan{
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(f.Name), Style: name}),
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + f.Dir), Style: dim}),
		}}), widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		widgets.NewText(itoa(f.Count), widgets.TextOpts{Style: dim}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})), widgets.SizedBoxOpts{Height: foundation.Some(peekRowHeight)})
}

// refRow is the row of a location: the text of its line with the reference marked, or its line and column when the text is not
// read yet. A click selects it and a double click opens it.
func (s *peekState) refRow(bc widgets.BuildContext, colors kit.Colors, v *PeekView, i int, indented bool) widgets.Widget {
	vm := s.Widget().vm
	ref := v.Refs[i]
	selected := i == v.Selected
	fg := colors.PeekViewResultLineForeground
	bg := ui.Color{}
	if selected {
		fg, bg = colors.PeekViewResultSelectionForeground, colors.PeekViewResultSelectionBackground
	}
	base := kit.TextStyleOf(bc, peekFont, fg)
	var label widgets.Widget
	if ref.Loaded {
		match := base.CopyWith(painting.TextStyleOpts{FontWeight: ui.FontWeightBold, BackgroundColor: foundation.Some(colors.PeekViewResultMatchHighlightBackground)})
		label = widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: []painting.InlineSpan{
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(ref.Before)}),
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(ref.Match), Style: match}),
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(ref.After)}),
		}}), widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	} else {
		label = widgets.NewText(itoa(ref.Range.Start.Line+1)+":"+itoa(ref.Range.Start.Col+1), widgets.TextOpts{Style: base, MaxLines: foundation.Some(1)})
	}
	left := 8.0
	if indented {
		left = 28
	}
	row := widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(left, 0, 8, 0), widgets.NewAlign(label, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})),
		widgets.ContainerOpts{Height: foundation.Some(peekRowHeight), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg)})})
	return widgets.NewMouseRegion(widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{
		Behavior:    rendering.HitTestBehaviorOpaque,
		OnTapDown:   func(gestures.TapDownDetails) { vm.PeekSelect(i) },
		OnDoubleTap: func() { vm.PeekSelect(i); vm.PeekOpenSelected(false, true) },
	}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
}

// itoa writes a number.
func itoa(n int) string { return strconv.Itoa(n) }
