package httpclient

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The highlighting of the boxes of the request tab: the {{variables}} of the URL box and the tokens of a raw body. Both set the
// function that builds the text span of the box's text controller (Glutter's TextEditingController.SetBuildTextSpan), so the text
// stays an editable box and only its colors change.

// varRef is a {{name}} in a text: the bytes [start, end) include the braces.
type varRef struct {
	start, end int
	name       string
}

// findVars lists the {{variables}} of a text in order. A name is what is between the braces, trimmed.
func findVars(text string) []varRef {
	var out []varRef
	for at := 0; at < len(text); {
		i := strings.Index(text[at:], "{{")
		if i < 0 {
			break
		}
		i += at
		j := strings.Index(text[i+2:], "}}")
		if j < 0 {
			break
		}
		end := i + 2 + j + 2
		out = append(out, varRef{start: i, end: end, name: strings.TrimSpace(text[i+2 : end-2])})
		at = end
	}
	return out
}

// varState is what the tab knows about a variable.
type varState int

const (
	varResolved varState = iota
	varSecret
	varUnresolved
)

// varLook answers, for the name of a variable, its state and its value (empty for a secret and an unresolved name) and where it came
// from (for the hover).
type varLook func(name string) (state varState, value, source string)

// urlSpan builds the span of the URL box: plain text, the resolved variables in the link color, the secret ones in the same color
// and the ones without a value underlined in the warning color.
func urlSpan(bc widgets.BuildContext, text string, style *painting.TextStyle, look varLook) *painting.TextSpan {
	colors := ui.Theme(bc)
	var spans []painting.InlineSpan
	at := 0
	plain := func(s string) {
		if s != "" {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(s)}))
		}
	}
	for _, v := range findVars(text) {
		plain(text[at:v.start])
		var st *painting.TextStyle
		state := varUnresolved
		if look != nil {
			state, _, _ = look(v.name)
		}
		switch state {
		case varUnresolved:
			st = style.CopyWith(painting.TextStyleOpts{
				Color: foundation.Some(colors.EditorWarningForeground), Decoration: foundation.Some(gui.TextDecorationUnderline),
				DecorationStyle: gui.TextDecorationStyleWavy, DecorationColor: foundation.Some(colors.EditorWarningForeground),
			})
		default:
			st = style.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.TextLinkForeground)})
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text[v.start:v.end]), Style: st}))
		at = v.end
	}
	plain(text[at:])
	return painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: spans})
}

// maxHighlighted is the longest text of a body that is colored; a longer one stays plain (tokenizing costs a pass over the text on
// the UI thread).
const maxHighlighted = 64 << 10

// tokenCache keeps the tokens of the last text of a box, so that building the span again for the same text costs nothing.
type tokenCache struct {
	text string
	lang string
	toks [][]extension.Token
}

// bodySpan builds the span of a raw body: the tokens of its language in the colors of the theme. A text that cannot be tokenized is
// plain.
func bodySpan(bc widgets.BuildContext, text, lang string, style *painting.TextStyle, langs extension.Languages, cache *tokenCache) *painting.TextSpan {
	plain := painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text), Style: style})
	if langs == nil || lang == "" || text == "" || len(text) > maxHighlighted {
		return plain
	}
	if cache.text != text || cache.lang != lang {
		cache.text, cache.lang, cache.toks = text, lang, nil
		if id, ok := langs.Resolve(lang); ok {
			if toks, err := langs.Tokenize(id, text); err == nil {
				cache.toks = toks
			}
		}
	}
	if cache.toks == nil {
		return plain
	}
	look := ui.TokenLooker(bc)
	fallback := ui.Theme(bc).EditorForeground
	lines := strings.Split(text, "\n")
	var spans []painting.InlineSpan
	add := func(s string, st *painting.TextStyle) {
		if s == "" {
			return
		}
		if st == nil {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(s)}))
			return
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(s), Style: st}))
	}
	for i, line := range lines {
		at := 0
		if i < len(cache.toks) {
			for _, t := range cache.toks[i] {
				start, end := min(max(t.Start, at), len(line)), min(t.End, len(line))
				add(line[at:start], nil)
				if end > start {
					l := look(t.Scopes, fallback)
					o := painting.TextStyleOpts{Color: foundation.Some(l.Color)}
					if l.Bold {
						o.FontWeight = gui.FontWeightBold
					}
					if l.Italic {
						o.FontStyle = gui.FontStyleItalic
					}
					add(line[start:end], style.CopyWith(o))
				}
				at = max(at, end)
			}
		}
		add(line[at:], nil)
		if i < len(lines)-1 {
			add("\n", nil)
		}
	}
	return painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: spans})
}

// languageOf is the name of the language a raw kind of body is colored with.
func languageOf(kind string) string {
	switch kind {
	case "json":
		return "json"
	case "xml":
		return "xml"
	case "html":
		return "html"
	case "javascript":
		return "javascript"
	case "graphql":
		return "graphql"
	}
	return ""
}
