package language

import (
	"fmt"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/errors"
)

// Ported from VS Code:
//   reference/vscode/src/vs/workbench/contrib/codeEditor/common/languageConfigurationExtensionPoint.ts
//   reference/vscode/src/vs/editor/common/languages/languageConfiguration.ts (MIT)

// Pair is an opening and a closing string, such as ( and ).
type Pair struct {
	Open, Close string
}

// AutoClosingPair is a pair that is closed automatically when its opening is typed.
type AutoClosingPair struct {
	Pair
	// NotIn lists the kinds of token the pair is not closed in: string, comment or regex.
	NotIn []string
	// BeforeText, when it is not nil, must match the text before the cursor for the pair to be
	// closed.
	BeforeText *regex.Regexp
}

// CommentRule says how comments are written.
type CommentRule struct {
	// LineComment starts a comment that ends with the line; empty for none.
	LineComment string
	// LineCommentNoIndent makes the line comment start at the first column when it is toggled.
	LineCommentNoIndent bool
	// BlockComment is the start and end of a block comment; nil for none.
	BlockComment *Pair
}

// IndentationRules decide the indentation of the line after a line, and of a line that is
// typed.
type IndentationRules struct {
	// IncreaseIndentPattern matches a line after which the next is indented.
	IncreaseIndentPattern *regex.Regexp
	// DecreaseIndentPattern matches a line that is indented less than the line before it.
	DecreaseIndentPattern *regex.Regexp
	// IndentNextLinePattern matches a line after which only the next line is indented.
	IndentNextLinePattern *regex.Regexp
	// UnIndentedLinePattern matches a line whose indentation is not looked at.
	UnIndentedLinePattern *regex.Regexp
}

// IndentAction is what pressing Enter does to the indentation.
type IndentAction int

// The indent actions, with the values of VS Code's IndentAction.
const (
	// IndentNone keeps the indentation of the line.
	IndentNone IndentAction = iota
	// IndentIndent indents one level more.
	IndentIndent
	// IndentOutdent puts the text after the cursor on its own line, one level out, and the cursor
	// on an indented line between.
	IndentOutdent
	// IndentOutdentOne indents one level less.
	IndentOutdentOne
)

// EnterAction is the effect of an on-enter rule.
type EnterAction struct {
	// Indent changes the indentation.
	Indent IndentAction
	// AppendText is typed after the line break and the indentation.
	AppendText string
	// RemoveText is the number of characters taken off the indentation.
	RemoveText int
}

// OnEnterRule is a rule for what pressing Enter does when the text around the cursor matches.
type OnEnterRule struct {
	// BeforeText must match the text before the cursor.
	BeforeText *regex.Regexp
	// AfterText, when it is not nil, must match the text after the cursor.
	AfterText *regex.Regexp
	// PreviousLineText, when it is not nil, must match the line above.
	PreviousLineText *regex.Regexp
	// Action is what happens.
	Action EnterAction
}

// FoldingRules say how a language is folded.
type FoldingRules struct {
	// OffSide makes the language fold by indentation.
	OffSide bool
	// HasOffSide tells whether the configuration says it.
	HasOffSide bool
	// MarkersStart and MarkersEnd, when both are not nil, match the lines that start and end a
	// region.
	MarkersStart, MarkersEnd *regex.Regexp
}

// Configuration is the language configuration of a language (SPEC §8.1): what the editor needs
// to know about a language without a language server. A field that is nil (or has no value) is
// not defined.
type Configuration struct {
	// Comments tells how comments are written.
	Comments *CommentRule
	// Brackets are the bracket pairs, for matching and for the indentation.
	Brackets []Pair
	// AutoClosingPairs are the pairs closed when the opening is typed.
	AutoClosingPairs []AutoClosingPair
	// SurroundingPairs are the pairs that can surround a selection.
	SurroundingPairs []Pair
	// ColorizedBracketPairs are the pairs whose levels have colors.
	ColorizedBracketPairs []Pair
	// AutoCloseBefore are the characters before which a pair is closed automatically.
	AutoCloseBefore *string
	// WordPattern matches a word of the language.
	WordPattern *regex.Regexp
	// IndentationRules decide the indentation.
	IndentationRules *IndentationRules
	// OnEnterRules say what Enter does.
	OnEnterRules []OnEnterRule
	// Folding says how the language is folded.
	Folding *FoldingRules
}

// Merge gives the configuration with the fields of other that are defined taking the place of
// the fields of c.
func (c *Configuration) Merge(other *Configuration) *Configuration {
	m := *c
	if other.Comments != nil {
		m.Comments = other.Comments
	}
	if other.Brackets != nil {
		m.Brackets = other.Brackets
	}
	if other.AutoClosingPairs != nil {
		m.AutoClosingPairs = other.AutoClosingPairs
	}
	if other.SurroundingPairs != nil {
		m.SurroundingPairs = other.SurroundingPairs
	}
	if other.ColorizedBracketPairs != nil {
		m.ColorizedBracketPairs = other.ColorizedBracketPairs
	}
	if other.AutoCloseBefore != nil {
		m.AutoCloseBefore = other.AutoCloseBefore
	}
	if other.WordPattern != nil {
		m.WordPattern = other.WordPattern
	}
	if other.IndentationRules != nil {
		m.IndentationRules = other.IndentationRules
	}
	if other.OnEnterRules != nil {
		m.OnEnterRules = other.OnEnterRules
	}
	if other.Folding != nil {
		m.Folding = other.Folding
	}
	return &m
}

// configReader reads the fields of a language configuration, and collects what is wrong in them.
type configReader struct {
	language string
	problems []error
}

// invalid notes a field that is not valid.
func (r *configReader) invalid(field, reason string) {
	r.problems = append(r.problems, errors.New("ConfigurationFieldInvalid", "a field of the language configuration is not valid",
		"language", r.language, "field", field, "reason", reason))
}

// ParseConfiguration reads a language configuration file (language-configuration.json). What is
// not valid is left out and listed in the problems: ConfigurationSyntaxError for the errors of
// the JSON, ConfigurationNotObject for a file that is not an object, ConfigurationFieldInvalid
// for a field of the wrong type and ConfigurationRegexInvalid for a regular expression that does
// not compile.
func ParseConfiguration(languageID string, data []byte) (*Configuration, []error) {
	r := &configReader{language: languageID}
	value, errs := jsonc.Parse(string(data), jsonc.ParseOptions{AllowEmptyContent: true})
	for _, e := range errs {
		r.problems = append(r.problems, errors.New("ConfigurationSyntaxError", "the language configuration has an error",
			"language", languageID, "offset", e.Offset, "message", e.Message()))
	}
	obj, ok := value.(map[string]any)
	if !ok {
		if value != nil {
			r.problems = append(r.problems, errors.New("ConfigurationNotObject", "a language configuration is a JSON object", "language", languageID))
		}
		return &Configuration{}, r.problems
	}
	c := &Configuration{
		Comments:              r.comments(obj["comments"]),
		Brackets:              r.pairs("brackets", obj["brackets"]),
		AutoClosingPairs:      r.autoClosingPairs(obj["autoClosingPairs"]),
		SurroundingPairs:      r.surroundingPairs(obj["surroundingPairs"]),
		ColorizedBracketPairs: r.pairs("colorizedBracketPairs", obj["colorizedBracketPairs"]),
		OnEnterRules:          r.onEnterRules(obj["onEnterRules"]),
	}
	if s, ok := obj["autoCloseBefore"].(string); ok {
		c.AutoCloseBefore = &s
	}
	if v, has := obj["wordPattern"]; has && v != nil && v != "" {
		c.WordPattern = r.regex("wordPattern", v)
	}
	if v, has := obj["indentationRules"]; has && v != nil {
		c.IndentationRules = r.indentationRules(v)
	}
	if v, has := obj["folding"]; has && v != nil {
		c.Folding = r.folding(v)
	}
	return c, r.problems
}

// stringPair reads an array of two strings.
func stringPair(v any) (Pair, bool) {
	a, ok := v.([]any)
	if !ok || len(a) != 2 {
		return Pair{}, false
	}
	open, ok1 := a[0].(string)
	closing, ok2 := a[1].(string)
	return Pair{open, closing}, ok1 && ok2
}

// stringList reads an array of strings.
func stringList(v any) ([]string, bool) {
	a, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		s, ok := x.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// comments reads the comments field.
func (r *configReader) comments(v any) *CommentRule {
	if v == nil {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		r.invalid("comments", "expected an object")
		return nil
	}
	var out *CommentRule
	if lc, has := obj["lineComment"]; has {
		switch lc := lc.(type) {
		case string:
			out = &CommentRule{LineComment: lc}
		case map[string]any:
			if text, ok := lc["comment"].(string); ok {
				noIndent, _ := lc["noIndent"].(bool)
				out = &CommentRule{LineComment: text, LineCommentNoIndent: noIndent}
			} else {
				r.invalid("comments.lineComment.comment", "expected a string")
			}
		default:
			r.invalid("comments.lineComment", "expected a string or an object with a comment property")
		}
	}
	if bc, has := obj["blockComment"]; has {
		if p, ok := stringPair(bc); ok {
			if out == nil {
				out = &CommentRule{}
			}
			out.BlockComment = &p
		} else {
			r.invalid("comments.blockComment", "expected an array of two strings")
		}
	}
	return out
}

// pairs reads an array of pairs of strings; a pair that is not valid is left out.
func (r *configReader) pairs(field string, v any) []Pair {
	if v == nil {
		return nil
	}
	a, ok := v.([]any)
	if !ok {
		r.invalid(field, "expected an array")
		return nil
	}
	var out []Pair
	for i, item := range a {
		p, ok := stringPair(item)
		if !ok {
			r.invalid(fmt.Sprintf("%s[%d]", field, i), "expected an array of two strings")
			continue
		}
		out = append(out, p)
	}
	if field == "colorizedBracketPairs" && out == nil {
		out = []Pair{}
	}
	return out
}

// autoClosingPairs reads the autoClosingPairs field: arrays of two strings and objects.
func (r *configReader) autoClosingPairs(v any) []AutoClosingPair {
	if v == nil {
		return nil
	}
	a, ok := v.([]any)
	if !ok {
		r.invalid("autoClosingPairs", "expected an array")
		return nil
	}
	var out []AutoClosingPair
	for i, item := range a {
		field := fmt.Sprintf("autoClosingPairs[%d]", i)
		if p, ok := stringPair(item); ok {
			out = append(out, AutoClosingPair{Pair: p})
			continue
		}
		if _, isArray := item.([]any); isArray {
			r.invalid(field, "expected an array of two strings or an object")
			continue
		}
		obj, ok := item.(map[string]any)
		if !ok {
			r.invalid(field, "expected an array of two strings or an object")
			continue
		}
		open, ok := obj["open"].(string)
		if !ok {
			r.invalid(field+".open", "expected a string")
			continue
		}
		closing, ok := obj["close"].(string)
		if !ok {
			r.invalid(field+".close", "expected a string")
			continue
		}
		pair := AutoClosingPair{Pair: Pair{open, closing}}
		if notIn, has := obj["notIn"]; has {
			list, ok := stringList(notIn)
			if !ok {
				r.invalid(field+".notIn", "expected a string array")
				continue
			}
			pair.NotIn = list
		}
		if bt, has := obj["beforeText"]; has {
			pair.BeforeText = r.regex(field+".beforeText", bt)
			if pair.BeforeText == nil {
				continue
			}
		}
		out = append(out, pair)
	}
	return out
}

// surroundingPairs reads the surroundingPairs field: arrays of two strings and objects.
func (r *configReader) surroundingPairs(v any) []Pair {
	if v == nil {
		return nil
	}
	a, ok := v.([]any)
	if !ok {
		r.invalid("surroundingPairs", "expected an array")
		return nil
	}
	var out []Pair
	for i, item := range a {
		field := fmt.Sprintf("surroundingPairs[%d]", i)
		if p, ok := stringPair(item); ok {
			out = append(out, p)
			continue
		}
		obj, isObject := item.(map[string]any)
		if !isObject {
			r.invalid(field, "expected an array of two strings or an object")
			continue
		}
		open, ok := obj["open"].(string)
		if !ok {
			r.invalid(field+".open", "expected a string")
			continue
		}
		closing, ok := obj["close"].(string)
		if !ok {
			r.invalid(field+".close", "expected a string")
			continue
		}
		out = append(out, Pair{open, closing})
	}
	return out
}

// onEnterRules reads the onEnterRules field.
func (r *configReader) onEnterRules(v any) []OnEnterRule {
	if v == nil {
		return nil
	}
	a, ok := v.([]any)
	if !ok {
		r.invalid("onEnterRules", "expected an array")
		return nil
	}
	var out []OnEnterRule
	for i, item := range a {
		field := fmt.Sprintf("onEnterRules[%d]", i)
		obj, ok := item.(map[string]any)
		if !ok {
			r.invalid(field, "expected an object")
			continue
		}
		act, ok := obj["action"].(map[string]any)
		if !ok {
			r.invalid(field+".action", "expected an object")
			continue
		}
		var action EnterAction
		switch act["indent"] {
		case "none":
			action.Indent = IndentNone
		case "indent":
			action.Indent = IndentIndent
		case "indentOutdent":
			action.Indent = IndentOutdent
		case "outdent":
			action.Indent = IndentOutdentOne
		default:
			r.invalid(field+".action.indent", "expected 'none', 'indent', 'indentOutdent' or 'outdent'")
			continue
		}
		if at, has := act["appendText"]; has && at != nil && at != "" {
			if s, ok := at.(string); ok {
				action.AppendText = s
			} else {
				r.invalid(field+".action.appendText", "expected undefined or a string")
			}
		}
		if rt, has := act["removeText"]; has && rt != nil && rt != float64(0) {
			if n, ok := rt.(float64); ok {
				action.RemoveText = int(n)
			} else {
				r.invalid(field+".action.removeText", "expected undefined or a number")
			}
		}
		rule := OnEnterRule{Action: action}
		rule.BeforeText = r.regex(field+".beforeText", obj["beforeText"])
		if rule.BeforeText == nil {
			continue
		}
		if at, has := obj["afterText"]; has && at != nil && at != "" {
			rule.AfterText = r.regex(field+".afterText", at)
		}
		if pt, has := obj["previousLineText"]; has && pt != nil && pt != "" {
			rule.PreviousLineText = r.regex(field+".previousLineText", pt)
		}
		out = append(out, rule)
	}
	return out
}

// indentationRules reads the indentationRules field; without both the increase and the decrease
// patterns there are no rules.
func (r *configReader) indentationRules(v any) *IndentationRules {
	obj, ok := v.(map[string]any)
	if !ok {
		r.invalid("indentationRules", "expected an object")
		return nil
	}
	inc := r.regex("indentationRules.increaseIndentPattern", obj["increaseIndentPattern"])
	if inc == nil {
		return nil
	}
	dec := r.regex("indentationRules.decreaseIndentPattern", obj["decreaseIndentPattern"])
	if dec == nil {
		return nil
	}
	rules := &IndentationRules{IncreaseIndentPattern: inc, DecreaseIndentPattern: dec}
	if p, has := obj["indentNextLinePattern"]; has && p != nil && p != "" {
		rules.IndentNextLinePattern = r.regex("indentationRules.indentNextLinePattern", p)
	}
	if p, has := obj["unIndentedLinePattern"]; has && p != nil && p != "" {
		rules.UnIndentedLinePattern = r.regex("indentationRules.unIndentedLinePattern", p)
	}
	return rules
}

// folding reads the folding field.
func (r *configReader) folding(v any) *FoldingRules {
	obj, ok := v.(map[string]any)
	if !ok {
		r.invalid("folding", "expected an object")
		return nil
	}
	f := &FoldingRules{}
	if off, ok := obj["offSide"].(bool); ok {
		f.OffSide, f.HasOffSide = off, true
	}
	if markers, ok := obj["markers"].(map[string]any); ok {
		var start, end *regex.Regexp
		if s, has := markers["start"]; has && s != nil && s != "" {
			start = r.regex("folding.markers.start", s)
		}
		if e, has := markers["end"]; has && e != nil && e != "" {
			end = r.regex("folding.markers.end", e)
		}
		if start != nil && end != nil {
			f.MarkersStart, f.MarkersEnd = start, end
		}
	}
	return f
}

// regex reads a regular expression written as a string or as {pattern, flags}, in JavaScript
// syntax. It returns nil, with a problem noted, when it is not valid.
func (r *configReader) regex(field string, v any) *regex.Regexp {
	var pattern, flags string
	switch v := v.(type) {
	case string:
		pattern = v
	case map[string]any:
		p, ok := v["pattern"].(string)
		if !ok {
			r.invalid(field+".pattern", "expected a string")
			return nil
		}
		pattern = p
		if f, has := v["flags"]; has && f != nil {
			s, ok := f.(string)
			if !ok {
				r.invalid(field+".flags", "expected a string")
				return nil
			}
			flags = s
		}
	default:
		r.invalid(field, "expected a string or an object")
		return nil
	}
	opts := regex.Options{Syntax: regex.ECMAScript}
	for _, f := range flags {
		switch f {
		case 'i':
			opts.IgnoreCase = true
		case 'm':
			opts.Multiline = true
		case 's':
			opts.DotAll = true
		case 'u':
			opts.Unicode = true
		case 'g', 'y', 'd':
			// These change where a search goes on from, which the editor decides.
		default:
			r.problems = append(r.problems, errors.New("ConfigurationRegexInvalid", "the flags of a regular expression are not valid",
				"language", r.language, "field", field, "flags", flags))
			return nil
		}
	}
	re, err := regex.Compile(pattern, opts)
	if err != nil {
		cause := err.Error()
		if appErr, ok := errors.FromError(err); ok {
			cause = appErr.Tag + ": " + appErr.Message
		}
		r.problems = append(r.problems, errors.New("ConfigurationRegexInvalid", "a regular expression of the language configuration does not compile",
			"language", r.language, "field", field, "pattern", clip(pattern), "cause", strings.TrimSpace(cause)))
		return nil
	}
	return re
}

// clip shortens a text for a message.
func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
