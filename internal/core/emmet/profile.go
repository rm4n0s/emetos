package emmet

import (
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
)

// The profiles of the setting emmet.syntaxProfiles.
//
// VS Code reads the setting in its Emmet extension and the helper library it uses (the helper's
// source is not a part of the references of the project), which turns a profile into the options of
// Emmet 2. The keys below are what Emetos accepts: the options of Emmet 2 by their short names, and the
// names of Emmet 1's profiles that the documentation of VS Code still shows. An unknown key is
// ignored, as there it is.

// profileKeys maps the keys of a profile to what they set.
var profileKeys = map[string]func(o *Options, v any) error{
	// Emmet 2
	"tagCase":                  func(o *Options, v any) error { return setCase(&o.TagCase, v) },
	"attributeCase":            func(o *Options, v any) error { return setCase(&o.AttributeCase, v) },
	"attributeQuotes":          func(o *Options, v any) error { return setQuotes(o, v) },
	"format":                   func(o *Options, v any) error { return setBool(&o.Format, v) },
	"formatLeafNode":           func(o *Options, v any) error { return setBool(&o.FormatLeafNode, v) },
	"formatSkip":               func(o *Options, v any) error { return setStrings(&o.FormatSkip, v) },
	"formatForce":              func(o *Options, v any) error { return setStrings(&o.FormatForce, v) },
	"inlineBreak":              func(o *Options, v any) error { return setCount(&o.InlineBreak, v) },
	"compactBooleanAttributes": func(o *Options, v any) error { return setBool(&o.CompactBoolean, v) },
	"booleanAttributes":        func(o *Options, v any) error { return setStrings(&o.BooleanAttributes, v) },
	"reverseAttributes":        func(o *Options, v any) error { return setBool(&o.ReverseAttributes, v) },
	"selfClosingStyle":         func(o *Options, v any) error { return setSelfClosingStyle(o, v) },
	"inlineElements":           func(o *Options, v any) error { return setStrings(&o.InlineElements, v) },
	// Emmet 1
	"tag_case":         func(o *Options, v any) error { return setCase(&o.TagCase, v) },
	"attr_case":        func(o *Options, v any) error { return setCase(&o.AttributeCase, v) },
	"attr_quotes":      func(o *Options, v any) error { return setQuotes(o, v) },
	"tag_nl":           setTagNL,
	"indent":           setIndentFlag,
	"inline_break":     func(o *Options, v any) error { return setCount(&o.InlineBreak, v) },
	"self_closing_tag": setSelfClosingTag,
	"compact_bool":     func(o *Options, v any) error { return setBool(&o.CompactBoolean, v) },
	"filters":          setFilters,
}

func init() {
	profileKeys["compactBoolean"] = profileKeys["compactBooleanAttributes"]
}

// ApplyProfiles applies the profile that the setting emmet.syntaxProfiles has for the syntax of
// the configuration (its value is an object of profiles named by syntax; no profile for the
// syntax is fine). As in VS Code, JSX is written with self-closing tags like `<br />`, and XML
// and XSL like `<br/>`, unless the profile sets the style (`selfClosingStyle` or
// `self_closing_tag`).
func (c *Config) ApplyProfiles(profiles map[string]any) error {
	var profile map[string]any
	if v, ok := profiles[c.Syntax]; ok && v != nil {
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("ProfileInvalid", "a profile of emmet.syntaxProfiles must be an object", "syntax", c.Syntax, "got", v)
		}
		profile = m
	}
	_, hasNew := profile["selfClosingStyle"]
	_, hasOld := profile["self_closing_tag"]
	if !hasNew && !hasOld {
		switch c.Syntax {
		case "jsx":
			c.Options.SelfClosingStyle = SelfClosingXHTML
		case "xml", "xsl":
			c.Options.SelfClosingStyle = SelfClosingXML
		}
	}
	return c.ApplyProfile(profile)
}

// ApplyProfile applies a profile: an object of the keys of profileKeys. Nothing is changed if a
// value of a known key is not valid.
func (c *Config) ApplyProfile(profile map[string]any) error {
	opts := c.Options
	for _, key := range slices.Sorted(maps.Keys(profile)) {
		set, ok := profileKeys[key]
		if !ok {
			continue
		}
		if err := set(&opts, profile[key]); err != nil {
			if appErr, ok := errors.FromError(err); ok {
				return appErr.SetMetadata("key", key)
			}
			return err
		}
	}
	c.Options = opts
	return nil
}

func setBool(dst *bool, v any) error {
	b, ok := v.(bool)
	if !ok {
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+"true or false", "got", v)
	}
	*dst = b
	return nil
}

func setCase(dst *StringCase, v any) error {
	s, ok := v.(string)
	if ok {
		switch strings.ToLower(s) {
		case "upper":
			*dst = CaseUpper
			return nil
		case "lower":
			*dst = CaseLower
			return nil
		case "", "leave", "asis":
			*dst = CaseKeep
			return nil
		}
	}
	return errors.New("ProfileValueInvalid", "the value of a profile key must be "+`"upper", "lower" or "leave"`, "got", v)
}

func setQuotes(o *Options, v any) error {
	switch v {
	case "single":
		o.AttributeQuotes = QuoteSingle
		return nil
	case "double":
		o.AttributeQuotes = QuoteDouble
		return nil
	}
	return errors.New("ProfileValueInvalid", "the value of a profile key must be "+`"single" or "double"`, "got", v)
}

func setStrings(dst *[]string, v any) error {
	list, ok := v.([]any)
	if !ok {
		if ss, isStrings := v.([]string); isStrings {
			*dst = slices.Clone(ss)
			return nil
		}
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+"an array of strings", "got", v)
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			return errors.New("ProfileValueInvalid", "the value of a profile key must be "+"an array of strings", "got", v)
		}
		out = append(out, s)
	}
	*dst = out
	return nil
}

// setCount reads a whole number from 0.
func setCount(dst *int, v any) error {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	default:
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+"a whole number", "got", v)
	}
	if f < 0 || f != math.Trunc(f) || f > 1e9 {
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+"a whole number from 0", "got", v)
	}
	*dst = int(f)
	return nil
}

func setSelfClosingStyle(o *Options, v any) error {
	switch v {
	case "html":
		o.SelfClosingStyle = SelfClosingHTML
	case "xml":
		o.SelfClosingStyle = SelfClosingXML
	case "xhtml":
		o.SelfClosingStyle = SelfClosingXHTML
	default:
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+`"html", "xml" or "xhtml"`, "got", v)
	}
	return nil
}

// setSelfClosingTag reads Emmet 1's self_closing_tag: true is `<br/>`, false `<br>` and "xhtml" `<br />`.
func setSelfClosingTag(o *Options, v any) error {
	switch v {
	case true:
		o.SelfClosingStyle = SelfClosingXML
	case false:
		o.SelfClosingStyle = SelfClosingHTML
	case "xhtml":
		o.SelfClosingStyle = SelfClosingXHTML
	default:
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+`true, false or "xhtml"`, "got", v)
	}
	return nil
}

// setTagNL reads Emmet 1's tag_nl: true, false or "decide" (the formatting of Emmet 2 decides, which is on).
func setTagNL(o *Options, v any) error {
	switch v {
	case true, "decide":
		o.Format = true
	case false:
		o.Format = false
	default:
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+`true, false or "decide"`, "got", v)
	}
	return nil
}

// setIndentFlag reads Emmet 1's indent: false is no indentation.
func setIndentFlag(o *Options, v any) error {
	b, ok := v.(bool)
	if !ok {
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+"true or false", "got", v)
	}
	if !b {
		o.Indent = ""
	}
	return nil
}

// setFilters reads the filters of Emmet 1 as a comma separated string: bem and c (comments) are
// the ones that exist in Emmet 2; the others are ignored.
func setFilters(o *Options, v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("ProfileValueInvalid", "the value of a profile key must be "+"a string", "got", v)
	}
	for _, f := range strings.Split(s, ",") {
		switch strings.TrimSpace(f) {
		case "bem":
			o.BEMEnabled = true
		case "c":
			o.CommentEnabled = true
		}
	}
	return nil
}
