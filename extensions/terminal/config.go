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
	sendKeys       bool
	allowChords    bool
	multiLinePaste string
	ignoreBracket  bool
	visualBell     bool
	bellDuration   float64

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
	}
	c.scrollback = min(max(c.scrollback, 0), 1000000)
	if c.lineHeight <= 0 {
		c.lineHeight = 1
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
