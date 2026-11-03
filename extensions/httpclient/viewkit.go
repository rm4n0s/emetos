package httpclient

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// rowText is one line of text of a row, cut with an ellipsis.
func rowText(bc widgets.BuildContext, s string, color gui.Color) widgets.Widget {
	return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// gap is a space of a width.
func gap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

// note is a paragraph of the view: what is wrong or missing.
func note(bc widgets.BuildContext, text string, color gui.Color) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 8, 12, 4), widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color)}))
}

// column stacks the widgets of a view, filling its width.
func column(children ...widgets.Widget) widgets.Widget {
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// The colors of the method badges are Postman's (SPEC §10.33 "The side bar"); they read on the dark and the light side bar.
var (
	colorGet    = gui.NewColor(0xFF3FB27F)
	colorPost   = gui.NewColor(0xFFE0A100)
	colorPut    = gui.NewColor(0xFF3B8EEA)
	colorPatch  = gui.NewColor(0xFFA66CD6)
	colorDelete = gui.NewColor(0xFFE5534B)
)

// methodColor is the color of a method's badge; other methods are gray.
func methodColor(method string, other gui.Color) gui.Color {
	switch strings.ToUpper(method) {
	case "GET":
		return colorGet
	case "POST":
		return colorPost
	case "PUT":
		return colorPut
	case "PATCH":
		return colorPatch
	case "DELETE":
		return colorDelete
	}
	return other
}

// methodLabel is the short name in a badge: DELETE is DEL, OPTIONS is OPT, and a method of the user's own is cut to four letters.
func methodLabel(method string) string {
	m := strings.ToUpper(strings.TrimSpace(method))
	switch m {
	case "":
		return "GET"
	case "DELETE":
		return "DEL"
	case "OPTIONS":
		return "OPT"
	}
	if r := []rune(m); len(r) > 5 {
		return string(r[:5])
	}
	return m
}

// methodBadge is the colored name of a method in a fixed width, so the names of the rows line up.
func methodBadge(bc widgets.BuildContext, method string) widgets.Widget {
	colors := ui.Theme(bc)
	style := pageui.Bold(ui.TextStyle(bc, 10, methodColor(method, colors.DescriptionForeground)))
	return widgets.NewSizedBox(widgets.NewText(methodLabel(method), widgets.TextOpts{Style: style, MaxLines: foundation.Some(1)}),
		widgets.SizedBoxOpts{Width: foundation.Some(38.0)})
}

// crossCenter centers the parts of a row across it.
const crossCenter = rendering.CrossAxisAlignmentCenter

// paddingH is the padding of a button under a note.
func paddingH(h float64) painting.EdgeInsets { return painting.NewEdgeInsetsFromLTRB(h, 4, h, 8) }

// optSome is foundation.Some for a float.
func optSome(v float64) foundation.Opt[float64] { return foundation.Some(v) }
