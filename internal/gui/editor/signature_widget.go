package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The parameter hints widget (SPEC §9.12 "As built (step 12.6)"): the active signature with its parameter in the highlight
// color, the documentation under it, and, when there are several signatures, the arrows and the counter ("1/3") at its left.
// It stands above the cursor, below it when there is no room.
//
// Ported from VS Code: src/vs/editor/contrib/parameterHints/browser/parameterHintsWidget.ts and parameterHints.css

// Metrics of the widget (parameterHints.css).
const (
	signatureMaxWidth = 440.0
	signatureRadius   = 4.0
	signatureShadow   = 8.0
	// signatureControlsMinWidth is the width of the column of the arrows at least (the counter makes it wider when it needs).
	signatureControlsMinWidth = 22.0
	signatureButton           = 16.0
	signatureCharWidth        = 0.6
)

// signatureMaxHeight is the tallest the widget gets before it scrolls: a quarter of the editor, 250 pixels at least.
func signatureMaxHeight(viewport float64) float64 { return math.Max(viewport/4, 250) }

// signatureControlsWidth is the width of the column of the arrows: 22 pixels, or the width of the counter and 2 pixels at each side.
func signatureControlsWidth(vm *ViewModel, h *langfeatures.SignatureHelp) float64 {
	chars := float64(len(signatureCounter(h.ActiveSignature, len(h.Signatures))))
	return math.Max(signatureControlsMinWidth, chars*vm.opts.FontSize*signatureCharWidth+8)
}

// signatureCounter is the text of the counter: the number of the signature, padded with zeros to the width of the count.
func signatureCounter(active, count int) string {
	return fmt.Sprintf("%0*d/%d", len(fmt.Sprint(count)), active+1, count)
}

// signatureDocs are the texts under the signature: the documentation of the active parameter and that of the signature.
func signatureDocs(sig langfeatures.Signature, activeParam int) []langfeatures.Markup {
	var docs []langfeatures.Markup
	if activeParam >= 0 && activeParam < len(sig.Parameters) {
		if d := sig.Parameters[activeParam].Documentation; d != nil && d.Value != "" {
			docs = append(docs, *d)
		}
	}
	if d := sig.Documentation; d != nil && d.Value != "" {
		docs = append(docs, *d)
	}
	return docs
}

// activeParameter is the index of the active parameter of the active signature: its own, else that of the help.
func activeParameter(h *langfeatures.SignatureHelp) (langfeatures.Signature, int) {
	sig := h.Signatures[h.ActiveSignature]
	if sig.ActiveParameter != nil {
		return sig, *sig.ActiveParameter
	}
	return sig, h.ActiveParameter
}

// signatureLayer is the widget over the text, or nil while there are no hints.
func (s *editorState) signatureLayer(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	h := vm.SignatureHints()
	if h == nil || s.textRO == nil || !s.textRO.Attached() || vm.viewport <= 0 {
		return nil
	}
	rect := s.textRO.RectOf(vm.cursors.Primary().Position())
	if rect.Bottom < 0 || rect.Top > vm.viewport {
		return nil
	}
	origin := s.gutterWidth
	if vm.mm.info.Show && vm.Options().Minimap.Side == "left" {
		origin += vm.mm.info.Width
	}
	width, height := estimateSignatureSize(h, vm)
	left := origin + rect.Left
	if viewW, _ := vm.ViewportSize(); viewW > 0 {
		left = math.Max(math.Min(left, origin+viewW-width), 0)
	}
	pos := widgets.PositionedOpts{Left: foundation.Some(left)}
	fitsAbove, fitsBelow := rect.Top-height >= 0, rect.Bottom+height <= vm.viewport
	if fitsAbove || !fitsBelow && rect.Top > vm.viewport-rect.Bottom {
		pos.Bottom = foundation.Some(vm.viewport - rect.Top)
	} else {
		pos.Top = foundation.Some(math.Max(rect.Bottom, 0))
	}
	box := s.signatureBox(bc, vm, colors, h)
	return widgets.NewStack([]widgets.Widget{widgets.NewPositioned(box, pos)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// estimateSignatureSize is the size the widget will have, worked out from the text: good enough to place it.
func estimateSignatureSize(h *langfeatures.SignatureHelp, vm *ViewModel) (width, height float64) {
	sig, active := activeParameter(h)
	multiple := len(h.Signatures) > 1
	lineH := math.Max(vm.opts.LineHeight, 1)
	avail := signatureMaxWidth - 2*hoverPadH
	if multiple {
		avail -= signatureControlsWidth(vm, h)
	}
	charW := math.Max(vm.opts.FontSize*signatureCharWidth, 1)
	perLine := math.Max(math.Floor(avail/charW), 1)
	rows := math.Max(math.Ceil(float64(len([]rune(sig.Label)))/perLine), 1)
	width = math.Min(float64(len([]rune(sig.Label)))*charW+2*hoverPadH+10, signatureMaxWidth)
	if len(signatureDocs(sig, active)) > 0 {
		width = signatureMaxWidth // the documentation fills the widget
	}
	height = rows*lineH + 8
	for _, d := range signatureDocs(sig, active) {
		n := 0
		for _, l := range strings.Split(d.Value, "\n") {
			n += max(int(math.Ceil(float64(len([]rune(l)))/math.Max(math.Floor(avail/(hoverCharWidth)), 1))), 1)
		}
		height += float64(n)*hoverLineHeight + 16
	}
	if multiple {
		width = math.Min(width+signatureControlsWidth(vm, h), signatureMaxWidth)
		height = math.Max(height, 3.3*lineH)
	}
	return math.Max(width, 60), math.Min(height+2, signatureMaxHeight(vm.viewport))
}

// signatureBox is the widget.
func (s *editorState) signatureBox(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, h *langfeatures.SignatureHelp) widgets.Widget {
	sig, active := activeParameter(h)
	fg := colors.EditorHoverWidgetForeground
	multiple := len(h.Signatures) > 1
	lineH := math.Max(vm.opts.LineHeight, 1)

	body := []widgets.Widget{
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(5, 4, 5, 4), widgets.NewRichText(vm.signatureLabel(colors, sig, active))),
	}
	docs := signatureDocs(sig, active)
	if len(docs) > 0 {
		body = append(body, widgets.NewOpacity(0.5, widgets.NewSizedBox(widgets.NewColoredBox(colors.EditorHoverWidgetBorder, nil), widgets.SizedBoxOpts{Height: foundation.Some(1.0)})))
		for _, d := range docs {
			var w widgets.Widget
			if d.Markdown {
				opts := vm.markdownOptions()
				w = markdown.NewView(d.Value, opts)
			} else {
				value := d.Value
				w = widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
					return hoverText(bc, value, hoverFontSize, fg)
				})
			}
			body = append(body, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(5, 4, 10, 4), w))
		}
	}
	content := widgets.NewColumn(body, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
	scrolled := widgets.NewRawScrollbar(
		widgets.NewSingleChildScrollView(content, widgets.SingleChildScrollViewOpts{Controller: s.signatureScroll, Primary: foundation.Some(false)}),
		widgets.RawScrollbarOpts{Controller: s.signatureScroll, Thickness: foundation.Some(6.0), ThumbColor: foundation.Some(colors.ScrollbarSliderBackground)})
	var inner widgets.Widget = scrolled
	minH := 0.0
	if multiple {
		minH = 3.3 * lineH
		controlsW := signatureControlsWidth(vm, h)
		// the arrows stand at the bottom of the left column and the body, with a line at its left, takes the rest
		line := widgets.NewContainer(scrolled, widgetsContainerLeftLine(colors))
		body := widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinHeight: foundation.Some(minH)}),
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(controlsW, 0, 0, 0), line))
		inner = widgets.NewStack([]widgets.Widget{
			body,
			widgets.NewPositioned(signatureControls(vm, colors, h, controlsW), widgets.PositionedOpts{Left: foundation.Some(0.0), Bottom: foundation.Some(0.0)}),
		}, widgets.StackOpts{Fit: rendering.StackFitLoose})
	}
	box := widgets.NewContainer(widgets.NewClipRRect(inner, widgets.ClipRRectOpts{BorderRadius: painting.NewBorderRadiusCircular(signatureRadius)}),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.EditorHoverWidgetBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.EditorHoverWidgetBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(signatureRadius),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: signatureShadow})},
		})})
	constrained := widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{
		MaxWidth: foundation.Some(signatureMaxWidth), MinHeight: foundation.Some(minH), MaxHeight: foundation.Some(signatureMaxHeight(vm.viewport)),
	}), box)
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(widgets.NewIntrinsicWidth(constrained), widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Basic})
}

// signatureLabel is the label of the signature in the code font, the active parameter bold in the highlight color.
//
// Ported from VS Code: parameterHintsWidget.ts (renderParameters)
func (vm *ViewModel) signatureLabel(colors kit.Colors, sig langfeatures.Signature, active int) painting.InlineSpan {
	size := vm.opts.FontSize
	style := vm.suggestStyle(colors.EditorHoverWidgetForeground, size)
	if len(sig.Parameters) == 0 {
		return painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(sig.Label), Style: style})
	}
	start, end := sig.LabelRange(active)
	start, end = min(max(start, 0), len(sig.Label)), min(max(end, 0), len(sig.Label))
	end = max(end, start)
	mark := style.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.EditorHoverWidgetHighlightForeground), FontWeight: ui.FontWeightBold})
	return painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: []painting.InlineSpan{
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(sig.Label[:start])}),
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(sig.Label[start:end]), Style: mark}),
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(sig.Label[end:])}),
	}})
}

// widgetsContainerLeftLine is the decoration of the body of the hints when there are arrows: a line at its left, half seen.
func widgetsContainerLeftLine(colors kit.Colors) widgets.ContainerOpts {
	c := colors.EditorHoverWidgetBorder
	return widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Border: painting.Border{Left: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(c.WithOpacity(c.Opacity() * 0.5)), Width: foundation.Some(1.0)})},
	})}
}

// signatureControls are the arrows with the counter between them, at the bottom of the left column.
func signatureControls(vm *ViewModel, colors kit.Colors, h *langfeatures.SignatureHelp, width float64) widgets.Widget {
	fg := colors.EditorHoverWidgetForeground
	arrow := func(code int, tap func()) widgets.Widget {
		icon := widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(signatureButton), Color: foundation.Some(fg)})
		return widgets.NewMouseRegion(
			widgets.NewGestureDetector(widgets.NewSizedBox(icon, widgets.SizedBoxOpts{Width: foundation.Some(signatureButton), Height: foundation.Some(signatureButton)}),
				widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: tap}),
			widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
	}
	family, fallback := "monospace", []string(nil)
	if f := vm.opts.FontFamilies; len(f) > 0 {
		family, fallback = f[0], f[1:]
	}
	counter := widgets.NewText(signatureCounter(h.ActiveSignature, len(h.Signatures)), widgets.TextOpts{Style: painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: family, FontFamilyFallback: fallback, FontSize: foundation.Some(vm.opts.FontSize), Color: foundation.Some(fg), Height: foundation.Some(12 / vm.opts.FontSize),
	}), SoftWrap: foundation.Some(false)})
	column := widgets.NewColumn([]widgets.Widget{
		arrow(fonts.CodiconChevronUp, vm.SignaturePrevious),
		widgets.NewSizedBox(widgets.NewCenter(counter, widgets.CenterOpts{}), widgets.SizedBoxOpts{Height: foundation.Some(12.0)}),
		arrow(fonts.CodiconChevronDown, vm.SignatureNext),
	}, widgets.ColumnOpts{MainAxisAlignment: rendering.MainAxisAlignmentEnd, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	return widgets.NewSizedBox(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 2), column), widgets.SizedBoxOpts{Width: foundation.Some(width)})
}

// scrollHoverBox scrolls the box of the hover when it has the keys.
func (s *editorState) scrollHoverBox(how HoverScroll) {
	c := s.hoverScroll
	if c == nil || !c.HasClients() {
		return
	}
	p := c.Position()
	top, page, bottom := c.Offset(), p.ViewportDimension(), p.MaxScrollExtent()
	var to float64
	switch how {
	case HoverScrollUp:
		to = top - hoverLineHeight
	case HoverScrollDown:
		to = top + hoverLineHeight
	case HoverScrollPageUp:
		to = top - page
	case HoverScrollPageDown:
		to = top + page
	case HoverScrollTop:
		to = 0
	case HoverScrollBottom:
		to = bottom
	default:
		return // the box wraps its text: there is no sideways scroll
	}
	c.JumpTo(math.Min(math.Max(to, 0), bottom))
}
