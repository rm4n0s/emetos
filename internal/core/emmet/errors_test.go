package emmet

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

// The routes of the errors: every tag of this package has a case.

func TestExpandErrorRoutes(t *testing.T) {
	cases := []struct {
		name, abbr, route, message string
	}{
		{"a character that is nothing", "str?", "Expand->Parse->ParseAbbreviation->Tokenize->scanner.fail.UnexpectedCharacter", "Unexpected character at 4"},
		{"a field with a brace in the placeholder that is never closed", "a[b=${1:{x]", "Tokenize->getToken->field->consumeFieldPlaceholder->scanner.fail.PlaceholderUnclosed", "Expecting } at"},
		{"a variable with no closing brace", "a[b=${1]", "Tokenize->getToken->field->scanner.fail.FieldUnclosed", "Expecting } at 8"},
		{"a token that is nothing", "/", "ParseAbbreviation->ParseTokens->tokenScanner.fail.UnexpectedToken", "Unexpected character at 0"},
		{"a token in the attributes that is nothing", "[a=b=c]", "ParseTokens->statements->element->attributeSet->tokenScanner.fail.UnexpectedAttributeToken", `Unexpected "Operator" token at 4`},
		{"a quote that is never closed", `[a="foo]`, "attributeSet->attribute->quoted->tokenScanner.fail.UnclosedQuote", "Unclosed quote at 3"},
		{"groups nested too deep", strings.Repeat("(", maxNesting+2) + "a" + strings.Repeat(")", maxNesting+2), "statements->group->statements->tokenScanner.fail.NestingTooDeep", "nested too deep"},
		{"elements nested too deep", strings.Repeat("a>", maxNesting+2) + "a", "ParseTokens->statements->tokenScanner.fail.NestingTooDeep", "nested too deep"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Expand(tc.abbr, newConfig(t, "html"))
			if err == nil {
				t.Fatalf("%q: no error", tc.abbr)
			}
			wantRoute(t, err, tc.route)
			if !strings.Contains(err.Error(), tc.message) {
				t.Errorf("message %q does not have %q", err.Error(), tc.message)
			}
		})
	}
}

func TestTagsAreCovered(t *testing.T) {
	// the tags of the errors of the package, and the case above or below that checks each one
	tagged := map[string]func() error{
		"UnexpectedCharacter":      func() error { _, err := Tokenize("?"); return err },
		"FieldUnclosed":            func() error { _, err := Tokenize("[a=${1"); return err },
		"PlaceholderUnclosed":      func() error { _, err := Tokenize("[a=${1:{x"); return err },
		"UnexpectedToken":          func() error { _, err := ParseAbbreviation("/", ParserOptions{}); return err },
		"UnexpectedAttributeToken": func() error { _, err := ParseAbbreviation("[a=b=c]", ParserOptions{}); return err },
		"UnclosedQuote":            func() error { _, err := ParseAbbreviation(`[a="b]`, ParserOptions{}); return err },
		"NestingTooDeep":           func() error { _, err := ParseAbbreviation(strings.Repeat("(", 2000), ParserOptions{}); return err },
		"EmmetAssetMissing":        func() error { _, err := readStringMapFrom(fstest.MapFS{}, "assets/html.json"); return err },
		"EmmetAssetInvalid": func() error {
			_, err := readStringMapFrom(fstest.MapFS{"assets/html.json": {Data: []byte("[1]")}}, "assets/html.json")
			return err
		},
		"LoremAssetMissing": func() error { _, err := loadVocabularyFrom(fstest.MapFS{}, "ru"); return err },
		"LoremAssetInvalid": func() error {
			_, err := loadVocabularyFrom(fstest.MapFS{"assets/lorem/russian.json": {Data: []byte("{")}}, "ru")
			return err
		},
		"VariablesInvalid":     func() error { _, err := VariablesFromSettings("x"); return err },
		"VariableValueInvalid": func() error { _, err := VariablesFromSettings(map[string]any{"a": 1}); return err },
		"ProfileInvalid": func() error {
			cfg := Config{Syntax: "html"}
			return cfg.ApplyProfiles(map[string]any{"html": "upper"})
		},
		"ProfileValueInvalid": func() error {
			cfg := Config{Syntax: "html"}
			return cfg.ApplyProfile(map[string]any{"tagCase": 3})
		},
	}
	for tag, make := range tagged {
		t.Run(tag, func(t *testing.T) {
			err := make()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if appErr.Tag != tag {
				t.Errorf("tag %q, want %q (route %s)", appErr.Tag, tag, appErr.Route())
			}
			if !appErr.HasRoute(tag) {
				t.Errorf("route %s does not end with the tag", appErr.Route())
			}
		})
	}
}

func TestAssetErrorRoutes(t *testing.T) {
	_, err := readStringMapFrom(fstest.MapFS{}, "assets/html.json")
	wantRoute(t, err, "readStringMapFrom.EmmetAssetMissing")
	_, err = readStringMapFrom(fstest.MapFS{"assets/html.json": {Data: []byte("[1]")}}, "assets/html.json")
	wantRoute(t, err, "readStringMapFrom->parseStringMap.EmmetAssetInvalid")
	_, err = loadVocabularyFrom(fstest.MapFS{}, "ru")
	wantRoute(t, err, "loadVocabularyFrom.LoremAssetMissing")
	_, err = loadVocabularyFrom(fstest.MapFS{"assets/lorem/russian.json": {Data: []byte("{")}}, "ru")
	wantRoute(t, err, "loadVocabularyFrom.LoremAssetInvalid")
}

func TestSettingsErrorRoutes(t *testing.T) {
	cfg := newConfig(t, "html")

	_, err := VariablesFromSettings("x")
	wantRoute(t, err, "VariablesFromSettings.VariablesInvalid")
	_, err = VariablesFromSettings(map[string]any{"a": 1})
	wantRoute(t, err, "VariablesFromSettings.VariableValueInvalid")

	err = cfg.ApplyProfiles(map[string]any{"html": []any{}})
	wantRoute(t, err, "Config.ApplyProfiles.ProfileInvalid")

	err = cfg.ApplyProfile(map[string]any{"attributeQuotes": "triple"})
	wantRoute(t, err, "setQuotes->invalid.ProfileValueInvalid")
	appErr, _ := errors.FromError(err)
	if appErr.Metadata["key"] != "attributeQuotes" {
		t.Errorf("metadata %v has no key", appErr.Metadata)
	}
}

// A broken tree or snippet is not an error: the snippet is left out and the user is told.
func TestNoErrorForBrokenSnippets(t *testing.T) {
	cfg := newConfig(t, "html")
	cfg.SetSnippets(map[string]string{"bad": "a[b=", "worse": "(("})
	for _, abbr := range []string{"bad", "worse"} {
		if _, err := Expand(abbr, cfg); err != nil {
			t.Errorf("%q: %v", abbr, err)
		}
	}
}
