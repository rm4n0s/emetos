package debug

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// MaxValueRenderLength is how many characters of a value a view shows (VS Code's MAX_VALUE_RENDER_LENGTH_IN_VIEWLET).
const MaxValueRenderLength = 1024

var (
	booleanRegex = regexp.MustCompile(`(?i)^(true|false)$`)
	stringRegex  = regexp.MustCompile(`^(['"]).*['"]$`)
)

// valueClass is how a value is colored: debugTokenExpression.<class>.
type valueClass int

const (
	classValue valueClass = iota
	classString
	classBoolean
	classNumber
	classError
	classChanged
)

// ClassOf is the class of a value of a node: VS Code's rules of renderValue. A node that is not available is an error, a value that
// changed since the last stop is highlighted, then the declared type, then what the text looks like.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugExpressionRenderer.ts (renderValue)
func classOf(value, typ string, available, changed bool) valueClass {
	if !available {
		if value != debug.NotAvailable {
			return classError
		}
		return classValue
	}
	if changed && value != debug.NotAvailable {
		return classChanged
	}
	switch typ {
	case "number":
		return classNumber
	case "boolean":
		return classBoolean
	case "string":
		return classString
	}
	if value != "" {
		if _, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return classNumber
		}
	}
	switch {
	case booleanRegex.MatchString(value):
		return classBoolean
	case stringRegex.MatchString(value):
		return classString
	}
	return classValue
}

// colorOf is the foreground of a value class, from the debugTokenExpression.* colors.
func colorOf(colors kit.Colors, c valueClass) ui.Color {
	id := "debugTokenExpression.value"
	switch c {
	case classString:
		id = "debugTokenExpression.string"
	case classBoolean:
		id = "debugTokenExpression.boolean"
	case classNumber:
		id = "debugTokenExpression.number"
	case classError:
		id = "debugTokenExpression.error"
	}
	if col, ok := colors.Named(id); ok {
		return col
	}
	return colors.Foreground
}

// named is the color of a theme id, or the fallback.
func named(colors kit.Colors, id string, fallback ui.Color) ui.Color {
	if c, ok := colors.Named(id); ok && c.Alpha() != 0 {
		return c
	}
	return fallback
}

// truncate cuts a value that is longer than MaxValueRenderLength.
func truncate(v string) string {
	if len(v) > MaxValueRenderLength {
		return v[:MaxValueRenderLength] + "..."
	}
	return v
}

// ExpressionLine is the text of a row that shows a node: name, the type when debug.showVariableTypes, and the value, in the colors of
// the debug tokens. The value that changed has the debugView.valueChangedHighlight behind it.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugExpressionRenderer.ts (renderVariable, renderValue) and
// variablesView.ts (VariablesRenderer, ScopesRenderer)
type ExpressionLine struct {
	// Name is the name; empty for a message.
	Name string
	// Type is shown before the value when ShowType is set.
	Type     string
	ShowType bool
	// Value is the value; Available is false for an expression the adapter could not evaluate.
	Value     string
	Available bool
	// Changed marks a value that changed since the last stop.
	Changed bool
	// Lazy marks a value that has to be asked for (the eye button of VS Code).
	Lazy bool
}

// LineOfNode makes the line of a node: a scope shows its name only, a variable or an expression its name and value.
func LineOfNode(n debug.Node, name string, showTypes bool) ExpressionLine {
	l := ExpressionLine{Name: name, Type: n.Type(), ShowType: showTypes, Value: n.Value(), Available: true, Changed: n.ValueChanged()}
	if h := n.PresentationHint(); h != nil && h.Lazy {
		l.Lazy = true
	}
	switch x := n.(type) {
	case *debug.Variable:
		l.Available = x.Available
	case *debug.Expression:
		l.Available = x.Available()
	}
	return l
}

// Spans make the text of the line in the style of a row: the name in debugTokenExpression.name, then "=" or ":" and the value.
func (l ExpressionLine) Spans(bc widgets.BuildContext, st kit.RowState) []painting.InlineSpan {
	colors := kit.ThemeOf(bc)
	base := kit.TextStyleOf(bc, 13, st.Foreground)
	var spans []painting.InlineSpan
	nameColor := named(colors, "debugTokenExpression.name", st.Foreground)
	if st.Selected && st.Active {
		nameColor = st.Foreground
	}
	if l.Name != "" {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(l.Name), Style: base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(nameColor)})}))
	}
	value := truncate(l.Value)
	if l.Available && l.Name != "" && value != "" {
		sep := " ="
		if l.Type != "" && l.ShowType {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(": "), Style: base}),
				painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(l.Type + " ="), Style: base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(named(colors, "debugTokenExpression.type", st.Foreground))})}))
		} else {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(sep), Style: base}))
		}
	} else if !l.Available && l.Name != "" && value != "" {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(":"), Style: base}))
	}
	if value != "" {
		class := classOf(l.Value, l.Type, l.Available, l.Changed)
		style := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colorOf(colors, class))})
		if class == classChanged {
			style = base.CopyWith(painting.TextStyleOpts{
				Color:           foundation.Some(colorOf(colors, classOf(l.Value, l.Type, l.Available, false))),
				BackgroundColor: foundation.Some(named(colors, "debugView.valueChangedHighlight", st.Foreground).WithOpacity(0.3)),
			})
		}
		prefix := " "
		if l.Name == "" {
			prefix = ""
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(prefix + value), Style: style}))
	}
	return spans
}

// Widget is the line as a one-line rich text that ends in an ellipsis.
func (l ExpressionLine) Widget(bc widgets.BuildContext, st kit.RowState) widgets.Widget {
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: l.Spans(bc, st)}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}
