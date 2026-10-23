package editor

import (
	"math"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/ui"
)

// textStyle builds the paragraphs of one look: a font, a size, a line height, features and
// a color. The render objects make one per frame from the view model's options.
type textStyle struct {
	factory ui.ParagraphFactory
	opts    Options
	color   ui.Color
	// fingerprint is part of the cache key of every paragraph of this look.
	fingerprint string
}

// newTextStyle makes the style of text in a color.
func newTextStyle(factory ui.ParagraphFactory, opts Options, color ui.Color) textStyle {
	var b strings.Builder
	b.WriteString(strings.Join(opts.FontFamilies, ","))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(opts.FontSize, 'g', -1, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(opts.LineHeight, 'g', -1, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatUint(uint64(color.ToARGB32()), 16))
	for _, f := range opts.FontFeatures {
		b.WriteByte('|')
		b.WriteString(f.Tag)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(f.Value))
	}
	return textStyle{factory: factory, opts: opts, color: color, fingerprint: b.String()}
}

// key is the cache key of a text in this style.
func (s textStyle) key(text string) paragraphKey {
	return paragraphKey{text: text, style: s.fingerprint, tabSize: s.opts.TabSize}
}

// build makes a paragraph of the text, laid out on one line (an editor row is never wrapped
// by the paragraph; wrapping splits the text into rows before). Tab stops come from the
// tab size, so that offsets in the paragraph are byte columns of the text.
func (s textStyle) build(text string) *ui.Paragraph {
	size := s.opts.FontSize
	family, fallback := "", []string(nil)
	if len(s.opts.FontFamilies) > 0 {
		family, fallback = s.opts.FontFamilies[0], s.opts.FontFamilies[1:]
	}
	pstyle := ui.NewParagraphStyle(ui.ParagraphStyleOpts{
		TextDirection: ui.TextDirectionLTR,
		FontFamily:    family,
		FontSize:      &size,
		TabSize:       float64(s.opts.TabSize),
	})
	var height *float64
	if size > 0 {
		h := s.opts.LineHeight / size
		height = &h
	}
	var features []ui.FontFeature
	for _, f := range s.opts.FontFeatures {
		features = append(features, ui.NewFontFeature(f.Tag, f.Value))
	}
	color := s.color
	builder := ui.NewParagraphBuilder(s.factory, pstyle)
	builder.PushStyle(ui.NewTextStyle(ui.TextStyleOpts{
		Color:               &color,
		FontFamily:          family,
		FontFamilyFallback:  fallback,
		FontSize:            &size,
		Height:              height,
		LeadingDistribution: ui.TextLeadingDistributionEven,
		FontFeatures:        features,
	}))
	builder.AddText(text)
	p := builder.Build()
	p.Layout(ui.NewParagraphConstraints(math.Inf(1)))
	return p
}

// paragraph returns the paragraph of a text, from the cache when it is there.
func (s textStyle) paragraph(cache *ParagraphCache, text string) *ui.Paragraph {
	return cache.get(s.key(text), func() *ui.Paragraph { return s.build(text) })
}

// digitWidth is the width of the widest digit in this style, for the width of the gutter.
func (s textStyle) digitWidth(cache *ParagraphCache) float64 {
	width := 0.0
	for d := range 10 {
		width = math.Max(width, s.paragraph(cache, strconv.Itoa(d)).LongestLine())
	}
	return width
}
