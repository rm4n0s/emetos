package editor

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// span is a part of a row drawn in a color and a font style: what a run of tokens with the same
// style comes to once the theme has been applied.
type span struct {
	start, end int
	color      ui.Color
	style      theme.FontStyle
}

// tokenStyle is the look a theme gives to a stack of scopes.
type tokenStyle struct {
	color ui.Color
	style theme.FontStyle
}

// styleTable gives the look of the scopes of tokens in a token theme, remembering what it
// resolved: resolving walks the rules of the theme and the scopes of a document repeat. It is
// used on the UI thread only.
type styleTable struct {
	theme  *theme.TokenTheme
	scopes *tokens.Scopes
	cache  map[tokens.ScopeID]tokenStyle
}

func newStyleTable(t *theme.TokenTheme, scopes *tokens.Scopes) *styleTable {
	return &styleTable{theme: t, scopes: scopes, cache: map[tokens.ScopeID]tokenStyle{}}
}

// resolve is the look of a stack of scopes.
func (t *styleTable) resolve(id tokens.ScopeID) tokenStyle {
	if s, ok := t.cache[id]; ok {
		return s
	}
	style := t.theme.Resolve(t.scopes.Names(id))
	out := tokenStyle{style: style.FontStyle}
	if c, ok := theme.ParseColor(t.theme.Color(style.Foreground)); ok {
		out.color = ui.NewColor(c.ARGB())
	} else {
		out.color = ui.NewColor(0xFFFFFFFF)
	}
	if out.style < 0 {
		out.style = theme.FontStyleNone
	}
	t.cache[id] = out
	return out
}

// spans are the styled parts of the text of a row, from the tokens of its line: the row is the
// bytes [start, end) of the line. Neighbouring tokens with the same look are one span, and the
// spans cover the row.
func (t *styleTable) spans(line []tokens.Token, start, end int) []span {
	var out []span
	from := 0
	for _, tok := range line {
		to := int(tok.End)
		if to <= start {
			from = to
			continue
		}
		if from >= end {
			break
		}
		s, e := max(from, start), min(to, end)
		from = to
		if s >= e {
			continue
		}
		look := t.resolve(tok.Scope)
		if n := len(out); n > 0 && out[n-1].color == look.color && out[n-1].style == look.style && out[n-1].end == s-start {
			out[n-1].end = e - start
			continue
		}
		out = append(out, span{start: s - start, end: e - start, color: look.color, style: look.style})
	}
	return out
}

// spansKey writes spans as the part of a paragraph's cache key that the tokens decide.
func spansKey(spans []span) string {
	if len(spans) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(strconv.Itoa(s.start))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(s.end))
		b.WriteByte(':')
		b.WriteString(strconv.FormatUint(uint64(s.color.ToARGB32()), 16))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(int(s.style)))
		b.WriteByte(';')
	}
	return b.String()
}

// textStyleOf is the text style that a span pushes on top of the base style of the paragraph.
func (s span) textStyle() *ui.TextStyle {
	color := s.color
	opts := ui.TextStyleOpts{Color: &color}
	if s.style&theme.FontStyleBold != 0 {
		opts.FontWeight = ui.FontWeightW700
	}
	if s.style&theme.FontStyleItalic != 0 {
		opts.FontStyle = ui.FontStyleItalic
	}
	var deco ui.TextDecoration
	if s.style&theme.FontStyleUnderline != 0 {
		deco |= ui.TextDecorationUnderline
	}
	if s.style&theme.FontStyleStrikethrough != 0 {
		deco |= ui.TextDecorationLineThrough
	}
	if deco != 0 {
		opts.Decoration = &deco
	}
	return ui.NewTextStyle(opts)
}
