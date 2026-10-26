package editor

import (
	"slices"
	"time"

	"github.com/rm4n0s/emetos/internal/core/settings"
)

// The settings of the code lenses, the inlay hints and the semantic highlighting (SPEC §9.12 "As built (step 12.9)").

// The values of editor.inlayHints.enabled.
const (
	inlayOn               = "on"
	inlayOff              = "off"
	inlayOnUnlessPressed  = "onUnlessPressed"
	inlayOffUnlessPressed = "offUnlessPressed"
)

// The values of LensOptions.Semantic.
const (
	// SemanticByTheme: the theme's semanticHighlighting decides (editor.semanticHighlighting.enabled "configuredByTheme").
	SemanticByTheme = "configuredByTheme"
	// SemanticOn and SemanticOff: the setting decides for every theme.
	SemanticOn  = "on"
	SemanticOff = "off"
)

// LensOptions are the settings of the code lenses, the inlay hints and the semantic highlighting.
type LensOptions struct {
	// CodeLens is editor.codeLens, CodeLensFontFamily editor.codeLensFontFamily and CodeLensFontSize editor.codeLensFontSize (0
	// is 90% of the editor's).
	CodeLens           bool
	CodeLensFontFamily string
	CodeLensFontSize   float64
	// InlayHints is editor.inlayHints.enabled: "on", "off", "onUnlessPressed" or "offUnlessPressed" (also when empty: "on").
	InlayHints string
	// InlayFontSize and InlayFontFamily are editor.inlayHints.fontSize (0: 90% of the editor's; a value under 5 or over the
	// editor's is the same) and editor.inlayHints.fontFamily (empty: the editor's); InlayPadding is editor.inlayHints.padding
	// and InlayMaxLength editor.inlayHints.maximumLength (0 for no limit).
	InlayFontSize   float64
	InlayFontFamily string
	InlayPadding    bool
	InlayMaxLength  int
	// Semantic is whether the semantic highlighting is on: SemanticByTheme, SemanticOn or SemanticOff. The setting
	// editor.semanticHighlighting.enabled decides, then the "enabled" of editor.semanticTokenColorCustomizations, then the
	// theme.
	Semantic string
	// SemanticRules are the "rules" of editor.semanticTokenColorCustomizations: styles for semantic tokens that win over the
	// theme's.
	SemanticRules map[string]any
}

// lensOptions reads the settings of the code lenses, the inlay hints and the semantic highlighting.
func lensOptions(store *settings.Store, languageID string) LensOptions {
	o := LensOptions{
		CodeLens:           settings.Get[bool](store, "editor.codeLens", languageID),
		CodeLensFontFamily: settings.Get[string](store, "editor.codeLensFontFamily", languageID),
		CodeLensFontSize:   settings.Get[float64](store, "editor.codeLensFontSize", languageID),
		InlayHints:         settings.Get[string](store, "editor.inlayHints.enabled", languageID),
		InlayFontSize:      settings.Get[float64](store, "editor.inlayHints.fontSize", languageID),
		InlayFontFamily:    settings.Get[string](store, "editor.inlayHints.fontFamily", languageID),
		InlayPadding:       settings.Get[bool](store, "editor.inlayHints.padding", languageID),
		InlayMaxLength:     int(settings.Get[float64](store, "editor.inlayHints.maximumLength", languageID)),
		Semantic:           SemanticByTheme,
	}
	switch v := settings.Get[any](store, "editor.semanticHighlighting.enabled", languageID).(type) {
	case bool:
		o.Semantic = SemanticOff
		if v {
			o.Semantic = SemanticOn
		}
	case string:
		switch v {
		case "true":
			o.Semantic = SemanticOn
		case "false":
			o.Semantic = SemanticOff
		}
	}
	if c, ok := settings.Get[any](store, "editor.semanticTokenColorCustomizations", languageID).(map[string]any); ok {
		if b, ok := c["enabled"].(bool); ok && o.Semantic == SemanticByTheme {
			o.Semantic = SemanticOff
			if b {
				o.Semantic = SemanticOn
			}
		}
		if rules, ok := c["rules"].(map[string]any); ok && len(rules) > 0 {
			o.SemanticRules = rules
		}
	}
	return o
}

// inlayMode is the mode of editor.inlayHints.enabled, "on" when it says nothing known.
func (o LensOptions) inlayMode() string {
	switch o.InlayHints {
	case inlayOff, inlayOnUnlessPressed, inlayOffUnlessPressed:
		return o.InlayHints
	}
	return inlayOn
}

// featureDebounce is the delay before a request that follows an edit or a scroll, as VS Code's feature debounce works it out:
// the average of the last answers' times, kept between a least and a greatest delay.
//
// Ported from VS Code: src/vs/editor/common/services/languageFeatureDebounce.ts (FeatureDebounceInformation)
type featureDebounce struct {
	min, max time.Duration
	history  []time.Duration
}

// debounceHistory is how many answer times the average is made of.
const debounceHistory = 6

func newFeatureDebounce(min, max time.Duration) *featureDebounce {
	return &featureDebounce{min: min, max: max}
}

// delay is the average of the times, or the least delay before there is a time.
func (d *featureDebounce) delay() time.Duration {
	if len(d.history) == 0 {
		return d.min
	}
	var sum time.Duration
	for _, t := range d.history {
		sum += t
	}
	return min(max(sum/time.Duration(len(d.history)), d.min), d.max)
}

// update notes how long an answer took.
func (d *featureDebounce) update(took time.Duration) {
	d.history = append(d.history, took)
	if len(d.history) > debounceHistory {
		d.history = slices.Delete(d.history, 0, len(d.history)-debounceHistory)
	}
}
