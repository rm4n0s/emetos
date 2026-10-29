package terminal

import (
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// config is what the settings say, read again whenever one changes. It is a plain value: the widgets copy it.
type config struct {
	scrollback   int
	fontFamilies []string
	fontSize     float64
	lineHeight   float64
	letterSpace  float64
	weight       int
	weightBold   int

	cursorStyle    string
	cursorBlink    bool
	cursorWidth    float64
	cursorInactive string
	boldBright     bool
	minContrast    float64
	textBlinking   bool

	wheelSens, fastSens float64
	wordSeparators      string
	copyOnSelection     bool
	rightClick          string
	middleClick         string

	skip           skipSet
	linkSchemes    []string
	sendKeys       bool
	allowChords    bool
	multiLinePaste string
	ignoreBracket  bool
	visualBell     bool
	bellDuration   float64

	// shell integration (step 17d.5)
	shellInteg    bool
	decorations   string
	quickFix      bool
	historyLimit  int
	stickyOn      bool
	stickyMax     int
	stickyIgnored []string
	splitCwd      string

	// terminal IntelliSense (step 17d.6)
	sugEnabled       bool
	sugQuick         quickSettings
	sugTriggerChars  bool
	sugRunOnEnter    string
	sugSelection     string
	sugProviders     map[string]bool
	sugCdPath        string
	sugTrailingSpace bool
	sugUpHistory     bool

	// step 17d.7: where a new terminal goes, and the persistent sessions
	defaultLocation string
	persistent      bool
	reviveProcess   string
	persistLines    int

	tabsEnabled  bool
	tabsLocation string
	tabsHide     string
	tabsTitle    string
	defaultIcon  string
}

// readConfig reads the settings. A setting that cannot be read is left at VS Code's default, and the reason is logged by the
// caller through the returned error of the first failure.
func readConfig(s extension.Settings) config {
	str := func(key, def string) string {
		if v, err := extension.SettingValue[string](s, key); err == nil {
			return v
		}
		return def
	}
	num := func(key string, def float64) float64 {
		if v, err := extension.SettingValue[float64](s, key); err == nil {
			return v
		}
		return def
	}
	flag := func(key string, def bool) bool {
		if v, err := extension.SettingValue[bool](s, key); err == nil {
			return v
		}
		return def
	}
	c := config{
		scrollback:      int(num(settingScrollback, 1000)),
		fontSize:        min(max(num(settingFontSize, 14), 6), 100),
		lineHeight:      num(settingLineHeight, 1),
		letterSpace:     num(settingLetterSpacing, 0),
		weight:          weightOf(str(settingFontWeight, "normal"), 400),
		weightBold:      weightOf(str(settingFontWeightBold, "bold"), 700),
		cursorStyle:     str(settingCursorStyle, "block"),
		cursorBlink:     flag(settingCursorBlink, false),
		cursorWidth:     max(num(settingCursorWidth, 1), 1),
		cursorInactive:  str(settingCursorInactive, "outline"),
		boldBright:      flag(settingBoldBright, true),
		minContrast:     num(settingMinContrast, 4.5),
		textBlinking:    flag(settingTextBlinking, false),
		wheelSens:       num(settingWheelSens, 1),
		fastSens:        num(settingFastSens, 5),
		wordSeparators:  str(settingWordSeps, " ()[]{}',\"`─‘’“”|"),
		copyOnSelection: flag(settingCopyOnSelect, false),
		rightClick:      str(settingRightClick, "default"),
		middleClick:     str(settingMiddleClick, "default"),
		sendKeys:        flag(settingSendKeys, false),
		allowChords:     flag(settingAllowChords, true),
		multiLinePaste:  str(settingMultiLinePaste, "auto"),
		ignoreBracket:   flag(settingIgnoreBracket, false),
		visualBell:      flag(settingVisualBell, false),
		bellDuration:    num(settingBellDuration, 1000),
		tabsEnabled:     flag(settingTabsEnabled, true),
		tabsLocation:    str(settingTabsLocation, "right"),
		tabsHide:        str(settingTabsHide, "singleTerminal"),
		tabsTitle:       str(settingTabsTitle, "${process}"),
		defaultIcon:     str(settingTabsDefaultIco, "terminal"),
		shellInteg:      flag(settingShellInteg, true),
		decorations:     str(settingDecorations, "both"),
		quickFix:        flag(settingQuickFix, true),
		historyLimit:    int(num(settingHistory, 100)),
		stickyOn:        flag(settingStickyEnabled, true),
		stickyMax:       min(max(int(num(settingStickyMaxLines, 5)), 1), 10),
		splitCwd:        str(settingSplitCwd, "inherited"),

		defaultLocation: str(settingDefaultLocation, "view"),
		persistent:      flag(settingPersistent, true),
		reviveProcess:   str(settingReviveProcess, "onExit"),
		persistLines:    min(max(int(num(settingPersistScrollback, 100)), 0), 1000),
	}
	c.sugEnabled, c.sugTriggerChars = flag(settingSugEnabled, true), flag(settingSugTrigger, false)
	c.sugRunOnEnter, c.sugSelection = str(settingSugRunEnter, "never"), str(settingSugSelect, "partial")
	c.sugCdPath, c.sugTrailingSpace, c.sugUpHistory = str(settingSugCdPath, "absolute"), flag(settingSugSpace, false), flag(settingSugUpArrow, true)
	c.sugQuick = readQuickSuggestions(s)
	c.sugProviders = map[string]bool{}
	if v, err := extension.SettingValue[map[string]bool](s, settingSugProvider); err == nil {
		c.sugProviders = v
	}
	c.stickyIgnored = []string{"clear", "cls", "clear-host", "agent", "agy", "copilot", "claude", "codex", "gemini"}
	if v, err := extension.SettingValue[[]string](s, settingStickyIgnored); err == nil {
		c.stickyIgnored = v
	}
	c.scrollback = min(max(c.scrollback, 0), 1000000)
	if c.lineHeight <= 0 {
		c.lineHeight = 1
	}
	if v, err := extension.SettingValue[[]string](s, settingLinkSchemes); err == nil {
		c.linkSchemes = v
	}
	user, _ := extension.SettingValue[[]string](s, settingSkipShell)
	c.skip = newSkipSet(user)
	families := str(settingFontFamily, "")
	if strings.TrimSpace(families) == "" {
		families, _ = extension.SettingValue[string](s, "editor.fontFamily")
	}
	c.fontFamilies = fontFamilies(families)
	return c
}

// fontFamilies splits a font family setting: comma-separated names, each trimmed and without the quotes around a name with
// spaces.
func fontFamilies(value string) []string {
	var out []string
	for _, name := range strings.Split(value, ",") {
		name = strings.Trim(strings.TrimSpace(name), `'"`)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// readQuickSuggestions reads terminal.integrated.suggest.quickSuggestions: an object with commands, arguments and unknown, each
// "on" or "off" (VS Code's default is all three off), or a boolean, which VS Code reads as commands and arguments on and unknown
// off (normalizeQuickSuggestionsConfig).
func readQuickSuggestions(s extension.Settings) quickSettings {
	if b, err := extension.SettingValue[bool](s, settingSugQuick); err == nil {
		return quickSettings{commands: b, arguments: b}
	}
	m, err := extension.SettingValue[map[string]string](s, settingSugQuick)
	if err != nil {
		return quickSettings{}
	}
	return quickSettings{commands: m["commands"] == "on", arguments: m["arguments"] == "on", unknown: m["unknown"] == "on"}
}
