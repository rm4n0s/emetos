package kit

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// AnsiText is read-only text with the colors and styles of the ANSI escape sequences in it (SGR: the 16 colors of the theme, 256 and true
// colors, bold, italic, underline, strike-through, overline, dim, hidden) and links: the output of a program in the Debug Console, and
// the output of a test run. Its text is the owner's; the widget draws it, nothing more.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugANSIHandling.ts (handleANSIOutput, appendStylizedStringToContainer)
// and linkDetector.ts (linkify)
type AnsiText struct {
	widgets.StatefulWidget
	text string
	// spans, when not nil, are the parsed spans of the text (NewAnsiSpansText): the widget does not parse.
	spans []AnsiSpan
	opts  AnsiTextOpts
}

// NewAnsiSpansText makes the widget for spans that were parsed already (one line of AnsiLines, say), so that a list of lines of a
// long text parses it once and not at each build of a line.
func NewAnsiSpansText(spans []AnsiSpan, opts ...AnsiTextOpts) *AnsiText {
	if spans == nil {
		spans = []AnsiSpan{}
	}
	a := NewAnsiText("", opts...)
	a.spans = spans
	return a
}

// AnsiLines splits the spans of a text into lines: each line keeps its line feed at the end (TrimEndNewline of AnsiTextOpts leaves it
// out) and the style that the sequences before it set, which is carried over from one line to the next.
func AnsiLines(spans []AnsiSpan) [][]AnsiSpan {
	var lines [][]AnsiSpan
	var cur []AnsiSpan
	for _, sp := range spans {
		rest := sp
		for rest.Text != "" {
			i := strings.IndexByte(rest.Text, '\n')
			if i < 0 {
				cur = append(cur, rest)
				break
			}
			head := rest
			head.Text = rest.Text[:i+1]
			cur = append(cur, head)
			lines = append(lines, cur)
			cur = nil
			rest.Start += i + 1
			rest.Text = rest.Text[i+1:]
		}
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// AnsiLinks make the paths and addresses in the text links. Without them the text has none.
type AnsiLinks struct {
	// Resolve says whether a part of kind LinkWeb or LinkPath is a link that can be followed (a path that is a file, an address): the
	// text of one that is not stays plain. It is called while the text is built, so it must be quick; a path is looked up once and the
	// answer kept.
	Resolve func(LinkPart) bool
	// Open follows a link; it is called when the user presses the primary button on the link with Ctrl held (VS Code asks for Ctrl).
	Open func(LinkPart)
}

// AnsiTextOpts are the options of NewAnsiText.
type AnsiTextOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Style is the text style the spans build on: the font, the size, the line height and the color of text with no color of its own.
	// The default is the 13 px text of the workbench in the foreground of the theme.
	Style *painting.TextStyle
	// Background is the color the text is read on; the colors of the theme are moved until they have a contrast ratio of 4 on it.
	// The default is the background of the panel.
	Background foundation.Opt[ui.Color]
	// Wrap makes lines wrap at the width (the default is on); without it a line is as long as its text and clips.
	NoWrap bool
	// MaxLines limits the lines; 0 is no limit.
	MaxLines int
	// TrimEndNewline leaves out one line feed at the end of the text: a whole line of output ends in one, which would draw an empty line
	// under it.
	TrimEndNewline bool
	// Links make paths and addresses links; nil for none.
	Links *AnsiLinks
}

// NewAnsiText makes the widget for a text.
func NewAnsiText(text string, opts ...AnsiTextOpts) *AnsiText {
	a := &AnsiText{text: text}
	if len(opts) > 0 {
		a.opts = opts[0]
	}
	a.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: a.opts.Key})
	return a
}

// CreateState implements widgets.StatefulWidget.
func (a *AnsiText) CreateState() widgets.State { return &ansiTextState{} }

type ansiTextState struct {
	widgets.StateOf[*AnsiText]
	// taps are the recognizers of the links, by the order of the link in the text; they live as long as the state does and call the
	// link the last build put at their place.
	taps  []*gestures.TapGestureRecognizer
	links []LinkPart

	parsedText string
	parsed     []AnsiSpan
}

// Dispose lets go of the recognizers.
func (s *ansiTextState) Dispose() {
	for _, t := range s.taps {
		t.Dispose()
	}
	s.taps = nil
}

// AnsiPalette is the 16 colors of the theme for ANSI text (terminal.ansiBlack to terminal.ansiBrightWhite), each moved until it has
// a contrast ratio of 4 on the background.
//
// Ported from VS Code: debugANSIHandling.ts (the theming participant that sets --vscode-debug-ansi-*)
func AnsiPalette(colors Colors, background ui.Color) [16]ui.Color {
	var out [16]ui.Color
	for i, c := range colors.TerminalAnsi {
		out[i] = EnsureContrast(background, c, 4)
	}
	return out
}

// AnsiStyleOf is the text style of an AnsiStyle on a base style, with the palette of the theme.
func AnsiStyleOf(base *painting.TextStyle, st AnsiStyle, palette *[16]ui.Color, defaultFg ui.Color) *painting.TextStyle {
	colorOf := func(c AnsiColor) (ui.Color, bool) {
		switch c.Kind {
		case AnsiColorTheme:
			return palette[c.Index&15], true
		case AnsiColorRGB:
			return ui.NewColorFromARGB(255, int(c.R), int(c.G), int(c.B)), true
		}
		return ui.Color{}, false
	}
	o := painting.TextStyleOpts{}
	fg, hasFg := colorOf(st.Fg)
	if !hasFg {
		fg = defaultFg
	}
	switch {
	case st.Hidden:
		fg = fg.WithOpacity(0)
	case st.Dim:
		fg = fg.WithOpacity(0.4)
	}
	if hasFg || st.Hidden || st.Dim {
		o.Color = foundation.Some(fg)
	}
	if bg, ok := colorOf(st.Bg); ok {
		o.BackgroundColor = foundation.Some(bg)
	}
	if st.Bold {
		o.FontWeight = ui.FontWeightW700
	}
	if st.Italic {
		o.FontStyle = ui.FontStyleItalic
	}
	var deco ui.TextDecoration
	if st.Underline || st.DoubleUnderline {
		deco = ui.CombineTextDecorations(deco, ui.TextDecorationUnderline)
	}
	if st.StrikeThrough {
		deco = ui.CombineTextDecorations(deco, ui.TextDecorationLineThrough)
	}
	if st.Overline {
		deco = ui.CombineTextDecorations(deco, ui.TextDecorationOverline)
	}
	if deco != ui.TextDecorationNone {
		o.Decoration = foundation.Some(deco)
		if st.DoubleUnderline {
			o.DecorationStyle = ui.TextDecorationStyleDouble
		}
		if uc, ok := colorOf(st.UnderlineColor); ok {
			o.DecorationColor = foundation.Some(uc)
		}
	}
	if !hasStyle(o) {
		return base
	}
	return base.CopyWith(o)
}

func hasStyle(o painting.TextStyleOpts) bool {
	_, c := o.Color.Get()
	_, b := o.BackgroundColor.Get()
	_, d := o.Decoration.Get()
	return c || b || d || o.FontWeight != 0 || o.FontStyle != ui.FontStyleNormal
}

// Spans make the spans of the text, with the links and the style of the sequences. The recognizers of the links come from the state of
// the widget that draws them, so a caller who is no AnsiText gives a nil taps and gets plain text for the links.
func ansiSpans(text string, parsed []AnsiSpan, base *painting.TextStyle, palette *[16]ui.Color, defaultFg, linkColor ui.Color, links *AnsiLinks, tap func(LinkPart) gestures.GestureRecognizer) []painting.InlineSpan {
	var out []painting.InlineSpan
	for _, sp := range parsed {
		style := AnsiStyleOf(base, sp.Style, palette, defaultFg)
		if links == nil {
			out = append(out, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(sp.Text), Style: style}))
			continue
		}
		for _, line := range SplitLinkLines(sp.Text) {
			for _, part := range DetectLinks(line) {
				if part.Kind == LinkText || !links.Resolve(part) {
					out = append(out, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(part.Value), Style: style}))
					continue
				}
				linkStyle := style.CopyWith(painting.TextStyleOpts{
					Color:      foundation.Some(linkColor),
					Decoration: foundation.Some(ui.TextDecorationUnderline), DecorationColor: foundation.Some(linkColor),
				})
				out = append(out, painting.NewTextSpan(painting.TextSpanOpts{
					Text: foundation.Some(part.Value), Style: linkStyle, Recognizer: tap(part),
				}))
			}
		}
	}
	_ = text
	return out
}

// Build implements widgets.State.
func (s *ansiTextState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := ThemeOf(bc)
	base := w.opts.Style
	if base == nil {
		base = TextStyleOf(bc, 13, colors.Foreground)
	}
	bg := w.opts.Background.OrElse(colors.PanelBackground)
	palette := AnsiPalette(colors, bg)
	if w.spans != nil {
		s.parsedText, s.parsed = w.text, w.spans
	} else if s.parsedText != w.text || s.parsed == nil {
		s.parsedText, s.parsed = w.text, ParseANSI(w.text)
	}
	s.links = s.links[:0]
	var tap func(LinkPart) gestures.GestureRecognizer
	if w.opts.Links != nil {
		tap = func(p LinkPart) gestures.GestureRecognizer {
			i := len(s.links)
			s.links = append(s.links, p)
			for len(s.taps) <= i {
				j := len(s.taps)
				r := gestures.NewTapGestureRecognizer(widgets.WidgetsBindingOf(bc).Gestures())
				r.OnTap = func() { s.follow(j) }
				s.taps = append(s.taps, r)
			}
			return s.taps[i]
		}
	}
	defaultFg, _ := base.Color.Get()
	if defaultFg == (ui.Color{}) {
		defaultFg = colors.Foreground
	}
	parsed := s.parsed
	if w.opts.TrimEndNewline {
		parsed = trimEndNewline(parsed)
	}
	spans := ansiSpans(w.text, parsed, base, &palette, defaultFg, colors.TextLinkForeground, w.opts.Links, tap)
	opts := widgets.RichTextOpts{SoftWrap: foundation.Some(!w.opts.NoWrap)}
	if w.opts.NoWrap {
		opts.Overflow = painting.TextOverflowClip
	}
	if w.opts.MaxLines > 0 {
		opts.MaxLines = foundation.Some(w.opts.MaxLines)
	}
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans, Style: base}), opts)
}

// follow opens the link at an index when Ctrl is held.
func (s *ansiTextState) follow(i int) {
	l := s.Widget().opts.Links
	if l == nil || l.Open == nil || i >= len(s.links) {
		return
	}
	if !widgets.WidgetsBindingOf(s.Context()).Keyboard().IsControlPressed() {
		return
	}
	l.Open(s.links[i])
}

// trimEndNewline drops one line feed (with its carriage return) from the end of the last span that has text.
func trimEndNewline(spans []AnsiSpan) []AnsiSpan {
	if len(spans) == 0 {
		return spans
	}
	last := spans[len(spans)-1]
	switch {
	case strings.HasSuffix(last.Text, "\r\n"):
		last.Text = last.Text[:len(last.Text)-2]
	case strings.HasSuffix(last.Text, "\n"):
		last.Text = last.Text[:len(last.Text)-1]
	default:
		return spans
	}
	out := append([]AnsiSpan(nil), spans[:len(spans)-1]...)
	if last.Text != "" {
		out = append(out, last)
	}
	return out
}
