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
	b.WriteString(strconv.Itoa(opts.FontWeight))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(opts.LetterSpacing, 'g', -1, 64))
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
func (s textStyle) build(text string, spans []span) *ui.Paragraph {
	p := s.make(text, spans)
	p.Layout(ui.NewParagraphConstraints(math.Inf(1)))
	return p
}

// make builds the paragraph of a text that is not laid out yet.
func (s textStyle) make(text string, spans []span) *ui.Paragraph {
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
	var spacing *float64
	if s.opts.LetterSpacing != 0 {
		spacing = &s.opts.LetterSpacing
	}
	weight := ui.FontWeight(s.opts.FontWeight)
	if s.opts.FontWeight == 0 {
		weight = ui.FontWeightW400
	}
	builder := ui.NewParagraphBuilder(s.factory, pstyle)
	builder.PushStyle(ui.NewTextStyle(ui.TextStyleOpts{
		Color:               &color,
		FontFamily:          family,
		FontFamilyFallback:  fallback,
		FontSize:            &size,
		FontWeight:          weight,
		LetterSpacing:       spacing,
		Height:              height,
		LeadingDistribution: ui.TextLeadingDistributionEven,
		FontFeatures:        features,
	}))
	if len(spans) == 0 {
		builder.AddText(text)
	} else {
		// Each span is drawn in its color and font style; what no span covers (the tokens of a
		// line cut short) is in the base style.
		at := 0
		for _, sp := range spans {
			start, end := min(max(sp.start, at), len(text)), min(sp.end, len(text))
			if start > at {
				builder.AddText(text[at:start])
			}
			if end > start {
				builder.PushStyle(sp.textStyle())
				builder.AddText(text[start:end])
				builder.Pop()
				at = end
			}
		}
		if at < len(text) {
			builder.AddText(text[at:])
		}
	}
	return builder.Build()
}

// paragraph returns the paragraph of a text, from the cache when it is there.
func (s textStyle) paragraph(cache *ParagraphCache, text string) *ui.Paragraph {
	return cache.get(s.key(text), func() *ui.Paragraph { return s.build(text, nil) })
}

// styledParagraph returns the paragraph of a text drawn in spans (the tokens of a row with the
// colors of the theme); the cache key has the spans. With no spans it is paragraph.
func (s textStyle) styledParagraph(cache *ParagraphCache, text string, spans []span) *ui.Paragraph {
	if len(spans) == 0 {
		return s.paragraph(cache, text)
	}
	key := s.key(text)
	key.spans = spansKey(spans)
	return cache.get(key, func() *ui.Paragraph { return s.build(text, spans) })
}

// digitWidth is the width of the widest digit in this style, for the width of the gutter.
func (s textStyle) digitWidth(cache *ParagraphCache) float64 {
	width := 0.0
	for d := range 10 {
		width = math.Max(width, s.paragraph(cache, strconv.Itoa(d)).LongestLine())
	}
	return width
}
