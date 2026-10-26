package editor

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Metrics of the hover box (hover.css of VS Code's editor hover).
const (
	hoverMaxWidth    = 500.0
	hoverFontSize    = 13.0
	hoverActionsSize = 12.0
	hoverLineHeight  = 19.0
	hoverPadH        = 8.0
	hoverPadV        = 4.0
	hoverActionsRow  = 22.0
	hoverRadius      = 4.0
	hoverShadowBlur  = 8.0
	// hoverCharWidth estimates the width of a character of the box, to place the box before it is laid out.
	hoverCharWidth = 7.0
)

// SetOpeners gives the hover what its links open: openURI a web page (the code of a diagnostic), openAt a place in a file
// (a place related to a diagnostic) with a line and a column counted from 1.
func (vm *ViewModel) SetOpeners(openURI func(uri string), openAt func(resource string, line, column int)) {
	vm.openURI, vm.openAt = openURI, openAt
}

// hoverLayer is the hover box over the text, or nil while there is none. It stands above the line of the range it is about
// (below it when there is no room, or editor.hover.above is off), left-aligned with the start of the range and kept inside the
// editor.
func (s *editorState) hoverLayer(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	h := vm.Hover()
	if h == nil || s.textRO == nil || !s.textRO.Attached() || vm.viewport <= 0 {
		return nil
	}
	rect := s.textRO.RectOf(h.Range.Start)
	if rect.Bottom < 0 || rect.Top > vm.viewport {
		return nil // the range is scrolled out of view
	}
	width, height := estimateHoverSize(h)
	origin := s.gutterWidth
	if vm.mm.info.Show && vm.Options().Minimap.Side == "left" {
		origin += vm.mm.info.Width
	}
	left := origin + rect.Left
	if viewW, _ := vm.ViewportSize(); viewW > 0 {
		left = max(min(left, origin+viewW-width), 0)
	}
	pos := widgets.PositionedOpts{Left: foundation.Some(left)}
	fitsAbove, fitsBelow := rect.Top-height >= 0, rect.Bottom+height <= vm.viewport
	if (vm.opts.Hover.Above && fitsAbove) || (!fitsBelow && fitsAbove) || (!fitsAbove && !fitsBelow && rect.Top > vm.viewport-rect.Bottom) {
		pos.Bottom = foundation.Some(vm.viewport - rect.Top)
	} else {
		pos.Top = foundation.Some(max(rect.Bottom, 0))
	}
	box := s.hoverBox(bc, vm, colors, h)
	return widgets.NewStack([]widgets.Widget{widgets.NewPositioned(box, pos)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// estimateHoverSize is the size the box will have, worked out from the text: good enough to place it.
func estimateHoverSize(h *Hover) (width, height float64) {
	inner := hoverMaxWidth - 2*hoverPadH
	perLine := math.Max(math.Floor(inner/hoverCharWidth), 1)
	width = 0
	for _, p := range h.Parts {
		if p.Kind != HoverPartDiagnostic {
			continue
		}
		d := p.Diagnostic
		lines := 0
		for _, l := range strings.Split(d.Message, "\n") {
			n := float64(len([]rune(l)))
			lines += max(int(math.Ceil(n/perLine)), 1)
			width = math.Max(width, math.Min(n, perLine)*hoverCharWidth+2*hoverPadH)
		}
		height += float64(lines)*hoverLineHeight + 2*hoverPadV
		if d.Source != "" || d.Code != "" {
			height += hoverLineHeight
			width = math.Max(width, float64(len(d.Source)+len(d.Code)+2)*hoverCharWidth+2*hoverPadH)
		}
		height += float64(len(d.Related)) * hoverLineHeight
		if len(p.Actions) > 0 {
			height += hoverActionsRow
		}
	}
	height += float64(max(len(h.Parts)-1, 0)) + 2
	return math.Min(math.Max(width, 60), hoverMaxWidth), height
}

// hoverBox is the box: the parts one below the other, each with the links below it.
func (s *editorState) hoverBox(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, h *Hover) widgets.Widget {
	var rows []widgets.Widget
	for i, p := range h.Parts {
		if i > 0 {
			rows = append(rows, widgets.NewSizedBox(widgets.NewColoredBox(colors.EditorHoverWidgetBorder, nil), widgets.SizedBoxOpts{Height: foundation.Some(1.0)}))
		}
		if p.Kind == HoverPartDiagnostic {
			rows = append(rows, hoverDiagnostic(bc, vm, colors, p))
		}
	}
	column := widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
	box := widgets.NewContainer(widgets.NewClipRRect(column, widgets.ClipRRectOpts{BorderRadius: painting.NewBorderRadiusCircular(hoverRadius)}),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.EditorHoverWidgetBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.EditorHoverWidgetBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(hoverRadius),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: hoverShadowBlur})},
		})})
	constrained := widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(hoverMaxWidth)}), box)
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(constrained, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque}),
		widgets.MouseRegionOpts{
			OnEnter: func(*gestures.PointerEnterEvent) { vm.HoverBoxEntered(true) },
			OnExit:  func(*gestures.PointerExitEvent) { vm.HoverBoxEntered(false) },
		})
}

// hoverText is a text of the box in the UI font.
func hoverText(bc widgets.BuildContext, text string, size float64, color ui.Color) widgets.Widget {
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: kit.UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"}, FontSize: foundation.Some(size),
		Height: foundation.Some(hoverLineHeight / size), Color: foundation.Some(color),
	})
	return widgets.NewText(text, widgets.TextOpts{Style: style})
}

// hoverLink is a text that opens something when it is pressed.
func hoverLink(bc widgets.BuildContext, text string, size float64, color ui.Color, tap func()) widgets.Widget {
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: kit.UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"}, FontSize: foundation.Some(size),
		Height: foundation.Some(hoverLineHeight / size), Color: foundation.Some(color), Decoration: foundation.Some(ui.TextDecorationUnderline),
	})
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(widgets.NewText(text, widgets.TextOpts{Style: style}), widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque, OnTap: tap,
		}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
}

// hoverDiagnostic is the part of a diagnostic: its message, what found it and its code (a link when it has one), the places
// related to it, and the links below.
//
// Ported from VS Code: markerHoverParticipant.ts (renderMarkerHover, renderMarkerStatusbar)
func hoverDiagnostic(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, p HoverPart) widgets.Widget {
	d := p.Diagnostic
	fg := colors.EditorHoverWidgetForeground
	dim := fg.WithOpacity(fg.Opacity() * 0.6)
	rows := []widgets.Widget{hoverText(bc, d.Message, hoverFontSize, fg)}
	if d.Source != "" || d.Code != "" {
		var parts []widgets.Widget
		switch {
		case d.Code != "" && d.CodeHref != "":
			if d.Source != "" {
				parts = append(parts, hoverText(bc, d.Source, hoverFontSize, dim))
			}
			parts = append(parts, hoverText(bc, "(", hoverFontSize, dim),
				hoverLink(bc, d.Code, hoverFontSize, colors.TextLinkForeground, func() {
					if vm.openURI != nil {
						vm.openURI(d.CodeHref)
					}
				}),
				hoverText(bc, ")", hoverFontSize, dim))
		case d.Code != "" && d.Source != "":
			parts = append(parts, hoverText(bc, d.Source+"("+d.Code+")", hoverFontSize, dim))
		case d.Code != "":
			parts = append(parts, hoverText(bc, "("+d.Code+")", hoverFontSize, dim))
		default:
			parts = append(parts, hoverText(bc, d.Source, hoverFontSize, dim))
		}
		rows = append(rows, widgets.NewRow(parts, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}))
	}
	for _, r := range d.Related {
		r := r
		label := filepath.Base(r.Resource) + "(" + strconv.Itoa(r.Line) + ", " + strconv.Itoa(r.Column) + ")"
		rows = append(rows, widgets.NewRow([]widgets.Widget{
			hoverLink(bc, label, hoverFontSize, colors.TextLinkForeground, func() {
				if vm.openAt != nil {
					vm.openAt(r.Resource, r.Line, r.Column)
				}
			}),
			hoverText(bc, ": "+r.Message, hoverFontSize, fg),
		}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}))
	}
	body := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(hoverPadV, hoverPadH),
		widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin}))
	if len(p.Actions) == 0 {
		return body
	}
	var actions []widgets.Widget
	for i, a := range p.Actions {
		if i > 0 {
			actions = append(actions, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}))
		}
		a := a
		actions = append(actions, hoverLink(bc, a.Label, hoverActionsSize, colors.TextLinkForeground, func() {
			vm.HideHover()
			if a.Run != nil {
				a.Run()
			}
		}))
	}
	bar := widgets.NewSizedBox(widgets.NewColoredBox(colors.EditorHoverWidgetStatusBarBackground,
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, hoverPadH),
			widgets.NewAlign(widgets.NewRow(actions, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}), widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}))),
		widgets.SizedBoxOpts{Height: foundation.Some(hoverActionsRow)})
	return widgets.NewColumn([]widgets.Widget{body, bar}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
}
