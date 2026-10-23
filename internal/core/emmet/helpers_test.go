package emmet

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// Helpers shared by the tests. The stringify functions are ports of the test assets of Emmet
// (packages/abbreviation/test/assets/stringify.ts, stringify-node.ts and test/assets/stringify.ts),
// which write the trees in a short form that the expected values are in.

// newConfig makes the configuration of a syntax, failing the test if it cannot.
func newConfig(t testing.TB, syntax string) Config {
	t.Helper()
	cfg, err := NewConfig(syntax)
	if err != nil {
		t.Fatalf("NewConfig(%q): %v", syntax, err)
	}
	return cfg
}

// fixedRand makes a source of random numbers that gives the same sequence on every run.
func fixedRand() func() float64 {
	state := uint64(12345)
	return func() float64 {
		state = state*6364136223846793005 + 1442695040888963407
		return float64(state>>11) / float64(1<<53)
	}
}

// wantRoute checks that the error is one of this package whose route has the given text.
func wantRoute(t testing.TB, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

// ---- the parser's tree (Statement) ----

func stringifyTokens(tokens []Token) string {
	var sb strings.Builder
	for _, t := range tokens {
		sb.WriteString(stringifyParserToken(t))
	}
	return sb.String()
}

func stringifyParserToken(t Token) string {
	switch t.Type {
	case TokenRepeater:
		if t.Implicit {
			return "*"
		}
		return "*" + strconv.Itoa(t.Count)
	case TokenRepeaterNumber:
		return strings.Repeat("$", t.Size)
	case TokenRepeaterPlaceholder:
		return "$#"
	case TokenField:
		index := ""
		if t.HasIndex {
			index = strconv.Itoa(t.Index)
		}
		sep := ""
		if index != "" && t.Name != "" {
			sep = ":"
		}
		return "${" + index + sep + t.Name + "}"
	case TokenOperator:
		return operatorChar(t.Operator)
	case TokenBracket:
		switch t.Context {
		case BracketAttribute:
			if t.Open {
				return "["
			}
			return "]"
		case BracketExpression:
			if t.Open {
				return "{"
			}
			return "}"
		}
		if t.Open {
			return "("
		}
		return ")"
	case TokenQuote:
		if t.Single {
			return "'"
		}
		return `"`
	case TokenLiteral:
		return t.Value
	case TokenWhiteSpace:
		return " "
	}
	return "?"
}

func stringifyStatement(n *Statement) string {
	if n.IsGroup {
		s := "(" + stringifyContent(n) + ")"
		if n.Repeat != nil {
			s += stringifyParserToken(*n.Repeat)
		}
		return s
	}
	name := "?"
	if n.Name != nil {
		name = stringifyTokens(n.Name)
	}
	repeat := ""
	if n.Repeat != nil {
		repeat = stringifyParserToken(*n.Repeat)
	}
	var attrs []string
	for _, a := range n.Attributes {
		an := stringifyTokens(a.Name)
		if an == "" {
			an = "?"
		}
		if a.HasValue {
			an += "=" + stringifyTokens(a.Value)
		}
		attrs = append(attrs, an)
	}
	attrText := ""
	if len(attrs) > 0 {
		attrText = " " + strings.Join(attrs, " ")
	}
	if n.SelfClose && len(n.Elements) == 0 {
		return "<" + name + repeat + attrText + " />"
	}
	return "<" + name + repeat + attrText + ">" + stringifyTokens(n.Value) + stringifyContent(n) + "</" + name + ">"
}

func stringifyContent(n *Statement) string {
	var sb strings.Builder
	for _, e := range n.Elements {
		sb.WriteString(stringifyStatement(e))
	}
	return sb.String()
}

// ---- the node tree ----

func stringifyValues(values []Value) string {
	var sb strings.Builder
	for _, v := range values {
		switch {
		case !v.IsField():
			sb.WriteString(v.Text)
		case v.Field.Name != "":
			sb.WriteString("${" + strconv.Itoa(v.Field.Index) + ":" + v.Field.Name + "}")
		default:
			sb.WriteString("${" + strconv.Itoa(v.Field.Index) + "}")
		}
	}
	return sb.String()
}

// stringifyNodes is the writer of stringify-node.ts: attributes are separated by commas.
func stringifyNodes(nodes []*Node) string {
	var sb strings.Builder
	for _, n := range nodes {
		name := n.Name
		if name == "" {
			name = "?"
		}
		var attrs []string
		for _, a := range n.Attributes {
			an := a.Name
			if an == "" {
				an = "?"
			}
			if a.Value != nil {
				an += `="` + stringifyValues(a.Value) + `"`
			}
			attrs = append(attrs, an)
		}
		attrText := ""
		if len(attrs) > 0 {
			attrText = " " + strings.Join(attrs, ", ")
		}
		repeat := ""
		if n.Repeat != nil {
			repeat = "*" + strconv.Itoa(n.Repeat.Count) + "@" + strconv.Itoa(n.Repeat.Value)
		}
		if n.SelfClosing && n.Value == nil && len(n.Children) == 0 {
			sb.WriteString("<" + name + repeat + attrText + " />")
		} else {
			sb.WriteString("<" + name + repeat + attrText + ">" + stringifyValues(n.Value) + stringifyNodes(n.Children) + "</" + name + ">")
		}
	}
	return sb.String()
}

// stringifyMarkup is the writer of test/assets/stringify.ts: attributes are separated by spaces,
// values are quoted, expressions are in braces and a node with no name and no attributes is its text.
func stringifyMarkup(nodes []*Node) string {
	var sb strings.Builder
	for _, n := range nodes {
		name := n.Name
		if name == "" {
			name = "?"
		}
		var attrs strings.Builder
		for _, a := range n.Attributes {
			an := a.Name
			if an == "" {
				an = "?"
			}
			before, after := `"`, `"`
			if a.ValueType == AttrExpression {
				before, after = "{", "}"
			}
			attrs.WriteString(" " + an + "=" + before + stringifyValues(a.Value) + after)
		}
		repeat := ""
		if n.Repeat != nil {
			repeat = "*" + strconv.Itoa(n.Repeat.Count) + "@" + strconv.Itoa(n.Repeat.Value)
		}
		value := stringifyValues(n.Value)
		switch {
		case n.Name == "" && n.Attributes == nil:
			sb.WriteString(value)
		case n.SelfClosing && n.Value == nil && len(n.Children) == 0:
			sb.WriteString("<" + name + repeat + attrs.String() + " />")
		default:
			sb.WriteString("<" + name + repeat + attrs.String() + ">" + value + stringifyMarkup(n.Children) + "</" + name + ">")
		}
	}
	return sb.String()
}

// errorTag gives the tag of an error made by this package, or "" for another error.
func errorTag(err error) string {
	if e, ok := errors.FromError(err); ok {
		return e.Tag
	}
	return ""
}
