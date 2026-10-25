package emmet

import (
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
)

// The preferences of the setting emmet.preferences that Emetos reads, for the stylesheet
// abbreviations. VS Code's helper library, which turns the setting into the options of Emmet 2,
// is not a part of the references of the project, so the keys are those of the schema of the
// setting (extensions/emmet/package.json). The other keys are ignored.

// DefaultFuzzySearchMinScore is the default of css.fuzzySearchMinScore in VS Code (Emmet's own is 0).
const DefaultFuzzySearchMinScore = 0.3

// ApplyPreferences applies the preferences of the setting emmet.preferences that concern the
// stylesheets: css.intUnit, css.floatUnit, css.unitAliases (an object of strings, or a string
// `e:em, p:%`), css.fuzzySearchMinScore (0 to 1), css.color.short, and the symbols at the end of a
// property and between its name and value (<syntax>.propertyEnd and <syntax>.valueSeparator, where
// the syntax of scss, less and sss is css). Nothing is changed if a value of a known key is not valid.
func (c *Config) ApplyPreferences(prefs map[string]any) error {
	opts := c.Options
	syntaxKey := c.Syntax
	switch c.Syntax {
	case "scss", "less", "sss":
		syntaxKey = "css"
	}
	setters := map[string]func(v any) error{
		"css.intUnit":             func(v any) error { return prefString(&opts.StylesheetIntUnit, v) },
		"css.floatUnit":           func(v any) error { return prefString(&opts.StylesheetFloatUnit, v) },
		"css.unitAliases":         func(v any) error { return prefAliases(&opts.StylesheetUnitAliases, v) },
		"css.fuzzySearchMinScore": func(v any) error { return prefScore(&opts.StylesheetFuzzySearchMinScore, v) },
		"css.color.short":         func(v any) error { return prefBool(&opts.StylesheetShortHex, v) },
		syntaxKey + ".propertyEnd": func(v any) error {
			return prefString(&opts.StylesheetAfter, v)
		},
		syntaxKey + ".valueSeparator": func(v any) error {
			return prefString(&opts.StylesheetBetween, v)
		},
	}
	for _, key := range slices.Sorted(maps.Keys(prefs)) {
		set, ok := setters[key]
		if !ok {
			continue
		}
		if err := set(prefs[key]); err != nil {
			if appErr, ok := errors.FromError(err); ok {
				return appErr.SetMetadata("key", key)
			}
			return err
		}
	}
	c.Options = opts
	return nil
}

func prefString(dst *string, v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("PreferenceValueInvalid", "the value of a preference must be "+"a string", "got", v)
	}
	*dst = s
	return nil
}

func prefBool(dst *bool, v any) error {
	b, ok := v.(bool)
	if !ok {
		return errors.New("PreferenceValueInvalid", "the value of a preference must be "+"true or false", "got", v)
	}
	*dst = b
	return nil
}

func prefScore(dst *float64, v any) error {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	default:
		return errors.New("PreferenceValueInvalid", "the value of a preference must be "+"a number from 0 to 1", "got", v)
	}
	if f < 0 || f > 1 || math.IsNaN(f) {
		return errors.New("PreferenceValueInvalid", "the value of a preference must be "+"a number from 0 to 1", "got", v)
	}
	*dst = f
	return nil
}

func prefAliases(dst *map[string]string, v any) error {
	aliases := map[string]string{}
	switch x := v.(type) {
	case map[string]any:
		for k, a := range x {
			s, ok := a.(string)
			if !ok {
				return errors.New("PreferenceValueInvalid", "the value of a preference must be "+"an object of strings, or a string like `e:em, p:%`", "got", v)
			}
			aliases[k] = s
		}
	case map[string]string:
		maps.Copy(aliases, x)
	case string:
		// The old notation: `e:em, p:%, x:ex, r:rem`
		for _, pair := range strings.Split(x, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			k, a, ok := strings.Cut(pair, ":")
			if !ok || strings.TrimSpace(k) == "" {
				return errors.New("PreferenceValueInvalid", "the value of a preference must be "+"an object of strings, or a string like `e:em, p:%`", "got", v)
			}
			aliases[strings.TrimSpace(k)] = strings.TrimSpace(a)
		}
	default:
		return errors.New("PreferenceValueInvalid", "the value of a preference must be "+"an object of strings, or a string like `e:em, p:%`", "got", v)
	}
	*dst = aliases
	return nil
}
