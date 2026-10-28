package terminal

import (
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The keys of the settings of the terminal (VS Code's terminal.integrated.*, with its defaults on Linux).
const (
	settingDefaultProfile = "terminal.integrated.defaultProfile.linux"
	settingProfiles       = "terminal.integrated.profiles.linux"
	settingCwd            = "terminal.integrated.cwd"
	settingEnv            = "terminal.integrated.env.linux"
	settingScrollback     = "terminal.integrated.scrollback"
	settingFontFamily     = "terminal.integrated.fontFamily"
	settingFontSize       = "terminal.integrated.fontSize"
	settingLineHeight     = "terminal.integrated.lineHeight"
	settingLetterSpacing  = "terminal.integrated.letterSpacing"
	settingFontWeight     = "terminal.integrated.fontWeight"
	settingFontWeightBold = "terminal.integrated.fontWeightBold"
	settingCursorStyle    = "terminal.integrated.cursorStyle"
	settingCursorBlink    = "terminal.integrated.cursorBlinking"
	settingCursorWidth    = "terminal.integrated.cursorWidth"
	settingCursorInactive = "terminal.integrated.cursorStyleInactive"
	settingBoldBright     = "terminal.integrated.drawBoldTextInBrightColors"
	settingMinContrast    = "terminal.integrated.minimumContrastRatio"
	settingWheelSens      = "terminal.integrated.mouseWheelScrollSensitivity"
	settingFastSens       = "terminal.integrated.fastScrollSensitivity"
	settingWordSeps       = "terminal.integrated.wordSeparators"
	settingCopyOnSelect   = "terminal.integrated.copyOnSelection"
	settingRightClick     = "terminal.integrated.rightClickBehavior"
	settingMiddleClick    = "terminal.integrated.middleClickBehavior"
	settingSkipShell      = "terminal.integrated.commandsToSkipShell"
	settingSendKeys       = "terminal.integrated.sendKeybindingsToShell"
	settingAllowChords    = "terminal.integrated.allowChords"
	settingMultiLinePaste = "terminal.integrated.enableMultiLinePasteWarning"
	settingIgnoreBracket  = "terminal.integrated.ignoreBracketedPasteMode"
	settingEnableBell     = "terminal.integrated.enableBell"
	settingVisualBell     = "terminal.integrated.enableVisualBell"
	settingConfirmOnKill  = "terminal.integrated.confirmOnKill"
	settingConfirmOnExit  = "terminal.integrated.confirmOnExit"
	settingTabsEnabled    = "terminal.integrated.tabs.enabled"
	settingTabsLocation   = "terminal.integrated.tabs.location"
	settingTabsHide       = "terminal.integrated.tabs.hideCondition"
	settingTabsTitle      = "terminal.integrated.tabs.title"
	settingTabsDefaultIco = "terminal.integrated.tabs.defaultIcon"
	settingDetectLocale   = "terminal.integrated.detectLocale"
	settingSmoothScroll   = "terminal.integrated.smoothScrolling"
	settingTabStopWidth   = "terminal.integrated.tabStopWidth"
	settingTextBlinking   = "terminal.integrated.textBlinking"
	settingBellDuration   = "terminal.integrated.bellDuration"
	settingLinkSchemes    = "terminal.integrated.allowedLinkSchemes"

	// Step 17d.5: shell integration, sticky scroll and the folder of a split.
	settingShellInteg     = "terminal.integrated.shellIntegration.enabled"
	settingDecorations    = "terminal.integrated.shellIntegration.decorationsEnabled"
	settingQuickFix       = "terminal.integrated.shellIntegration.quickFixEnabled"
	settingHistory        = "terminal.integrated.shellIntegration.history"
	settingStickyEnabled  = "terminal.integrated.stickyScroll.enabled"
	settingStickyMaxLines = "terminal.integrated.stickyScroll.maxLineCount"
	settingStickyIgnored  = "terminal.integrated.stickyScroll.ignoredCommands"
	settingSplitCwd       = "terminal.integrated.splitCwd"

	// Step 17d.6: terminal IntelliSense.
	settingSugEnabled  = "terminal.integrated.suggest.enabled"
	settingSugQuick    = "terminal.integrated.suggest.quickSuggestions"
	settingSugTrigger  = "terminal.integrated.suggest.suggestOnTriggerCharacters"
	settingSugRunEnter = "terminal.integrated.suggest.runOnEnter"
	settingSugSelect   = "terminal.integrated.suggest.selectionMode"
	settingSugProvider = "terminal.integrated.suggest.providers"
	settingSugCdPath   = "terminal.integrated.suggest.cdPath"
	settingSugSpace    = "terminal.integrated.suggest.insertTrailingSpace"
	settingSugUpArrow  = "terminal.integrated.suggest.upArrowNavigatesHistory"
)

// defaultStickyIgnored are the commands that do not show sticky scroll (VS Code's default of
// terminal.integrated.stickyScroll.ignoredCommands).
var defaultStickyIgnored = []any{"clear", "cls", "clear-host", "agent", "agy", "copilot", "claude", "codex", "gemini"}

// weightEnum are the suggestions VS Code gives for a font weight.
var weightEnum = []any{"normal", "bold", "100", "200", "300", "400", "500", "600", "700", "800", "900"}

// settingSpecs are the settings the extension declares, with the ids, types, defaults and descriptions of VS Code's
// terminalConfiguration.ts (the defaults of Linux).
//
// Ported from VS Code: src/vs/workbench/contrib/terminal/common/terminalConfiguration.ts
func settingSpecs() []extension.SettingSpec {
	f := func(v float64) *float64 { return &v }
	const cat = "Terminal"
	return []extension.SettingSpec{
		{Key: settingDefaultProfile, Type: "string", Default: "", Category: cat,
			Description: "The default terminal profile on Linux: the name of one of terminal.integrated.profiles.linux. Empty uses the shell of the user ($SHELL)."},
		{Key: settingProfiles, Type: "object", Category: cat, Default: map[string]any{
			"bash": map[string]any{"path": "bash", "icon": "terminal-bash"},
			"zsh":  map[string]any{"path": "zsh"},
			"fish": map[string]any{"path": "fish"},
			"tmux": map[string]any{"path": "tmux", "icon": "terminal-tmux"},
			"pwsh": map[string]any{"path": "pwsh", "icon": "terminal-powershell"},
		}, Description: "The terminal profiles to show on Linux: each has a name and an object with the path of the shell (a program found on the PATH or an absolute path), its args, env and icon. A profile whose program is not found is not offered."},
		{Key: settingCwd, Type: "string", Default: "", Category: cat,
			Description: "An explicit start path where the terminal will be launched, this is used as the current working directory (cwd) for the shell process. This may be particularly useful in workspace settings if the root directory is not a convenient cwd."},
		{Key: settingEnv, Type: "object", Default: map[string]any{}, Category: cat,
			Description: "Object with environment variables that will be added to the Emetos process to be used by the terminal on Linux. Set to `null` to delete the environment variable."},
		{Key: settingScrollback, Type: "number", Default: 1000.0, Minimum: f(0), Maximum: f(1000000), Category: cat,
			Description: "Controls the maximum number of lines the terminal keeps in its buffer."},
		{Key: settingFontFamily, Type: "string", Default: "", Category: cat,
			Description: "Controls the font family of the terminal. Defaults to editor.fontFamily's value."},
		{Key: settingFontSize, Type: "number", Default: 14.0, Minimum: f(6), Maximum: f(100), Category: cat,
			Description: "Controls the font size in pixels of the terminal."},
		{Key: settingLetterSpacing, Type: "number", Default: 0.0, Category: cat,
			Description: "Controls the letter spacing of the terminal. This is an integer value which represents the number of additional pixels to add between characters."},
		{Key: settingLineHeight, Type: "number", Default: 1.0, Category: cat,
			Description: "Controls the line height of the terminal. This number is multiplied by the terminal font size to get the actual line-height in pixels."},
		{Key: settingFontWeight, Type: "string", Default: "normal", Enum: weightEnum, Category: cat,
			Description: "The font weight to use within the terminal for non-bold text. Accepts \"normal\" and \"bold\" keywords or numbers between 1 and 1000."},
		{Key: settingFontWeightBold, Type: "string", Default: "bold", Enum: weightEnum, Category: cat,
			Description: "The font weight to use within the terminal for bold text. Accepts \"normal\" and \"bold\" keywords or numbers between 1 and 1000."},
		{Key: settingCursorBlink, Type: "boolean", Default: false, Category: cat, Description: "Controls whether the terminal cursor blinks."},
		{Key: settingTextBlinking, Type: "boolean", Default: false, Category: cat, Description: "Controls whether text blinking is enabled in the terminal."},
		{Key: settingCursorStyle, Type: "string", Default: "block", Enum: []any{"block", "line", "underline"}, Category: cat,
			Description: "Controls the style of terminal cursor when the terminal is focused."},
		{Key: settingCursorInactive, Type: "string", Default: "outline", Enum: []any{"outline", "block", "line", "underline", "none"}, Category: cat,
			Description: "Controls the style of terminal cursor when the terminal is not focused."},
		{Key: settingCursorWidth, Type: "number", Default: 1.0, Category: cat,
			Description: "Controls the width of the cursor when terminal.integrated.cursorStyle is set to `line`."},
		{Key: settingBoldBright, Type: "boolean", Default: true, Category: cat,
			Description: "Controls whether bold text in the terminal will always use the \"bright\" ANSI color variant."},
		{Key: settingMinContrast, Type: "number", Default: 4.5, Category: cat,
			Description: "When set, the foreground color of each cell will change to try meet the contrast ratio specified. Example values: 1: do nothing and use the standard theme colors; 4.5: WCAG AA compliance (minimum, default); 7: WCAG AAA compliance (enhanced); 21: white on black or black on white."},
		{Key: settingWheelSens, Type: "number", Default: 1.0, Category: cat,
			Description: "A multiplier to be used on the `deltaY` of mouse wheel scroll events."},
		{Key: settingFastSens, Type: "number", Default: 5.0, Category: cat,
			Description: "Scrolling speed multiplier when pressing `Alt`."},
		{Key: settingSmoothScroll, Type: "boolean", Default: false, Category: cat,
			Description: "Controls whether the terminal will scroll using an animation."},
		{Key: settingWordSeps, Type: "string", Default: " ()[]{}',\"`─‘’“”|", Category: cat,
			Description: "A string containing all characters to be considered word separators when double-clicking to select word and in the fallback 'word' link detection."},
		{Key: settingCopyOnSelect, Type: "boolean", Default: false, Category: cat,
			Description: "Controls whether text selected in the terminal will be copied to the clipboard."},
		{Key: settingRightClick, Type: "string", Default: "default", Enum: []any{"default", "copyPaste", "paste", "selectWord", "nothing"}, Category: cat,
			Description: "Controls how terminal reacts to right click. default: show the context menu; copyPaste: copy when there is a selection, otherwise paste; paste: paste on right click; selectWord: select the word under the cursor and show the context menu; nothing: do nothing and pass event to terminal."},
		{Key: settingMiddleClick, Type: "string", Default: "default", Enum: []any{"default", "paste"}, Category: cat,
			Description: "Controls how terminal reacts to middle click. default: the platform default to focus the terminal, on Linux this will also paste the selection; paste: paste on middle click."},
		{Key: settingLinkSchemes, Type: "array", Category: cat,
			Default:     []any{"file", "http", "https", "mailto", "vscode", "vscode-insiders"},
			Description: "An array of strings containing the URI schemes that the terminal is allowed to open links for. By default, only a small subset of possible schemes are allowed for security reasons."},
		{Key: settingSkipShell, Type: "array", Default: []any{}, Category: cat,
			Description: "A set of command IDs whose keybindings will not be sent to the shell but instead always be handled by Emetos. This allows keybindings that would normally be consumed by the shell to act instead the same as when the terminal is not focused, for example `Ctrl+P` to launch Quick Open. Many commands are skipped by default. To override a default and pass that command's keybinding to the shell instead, add the command prefixed with the `-` character. For example add `-workbench.action.quickOpen` to allow `Ctrl+P` to reach the shell."},
		{Key: settingSendKeys, Type: "boolean", Default: false, Category: cat,
			Description: "Dispatches most keybindings to the terminal instead of the workbench, overriding terminal.integrated.commandsToSkipShell, which can be used alternatively for fine tuning."},
		{Key: settingAllowChords, Type: "boolean", Default: true, Category: cat,
			Description: "Whether or not to allow chord keybindings in the terminal. Note that when this is true and the keystroke results in a chord it will bypass terminal.integrated.commandsToSkipShell, setting this to false is particularly useful when you want ctrl+k to go to your shell (not Emetos)."},
		{Key: settingMultiLinePaste, Type: "string", Default: "auto", Enum: []any{"auto", "always", "never"}, Category: cat,
			Description: "Controls whether to show a warning dialog when pasting multiple lines into the terminal. auto: enable the warning but do not show it when bracketed paste mode is enabled (the shell supports multi-line paste natively); always: always show the warning if the text contains a new line; never: never show the warning."},
		{Key: settingIgnoreBracket, Type: "boolean", Default: false, Category: cat,
			Description: "Controls whether the terminal will ignore bracketed paste mode even if the terminal was put into the mode, omitting the `\\x1b[200~` and `\\x1b[201~` sequences when pasting."},
		{Key: settingEnableBell, Type: "boolean", Default: false, Category: cat,
			Description: "This is now deprecated. Instead use the `terminal.integrated.enableVisualBell` setting."},
		{Key: settingVisualBell, Type: "boolean", Default: false, Category: cat,
			Description: "Controls whether the visual terminal bell is enabled. This shows up next to the terminal's name."},
		{Key: settingBellDuration, Type: "number", Default: 1000.0, Category: cat,
			Description: "The number of milliseconds to show the bell within a terminal tab when triggered."},
		{Key: settingConfirmOnKill, Type: "string", Default: "editor", Enum: []any{"never", "editor", "panel", "always"}, Category: cat,
			Description: "Controls whether to confirm killing terminals when they have child processes. never: never confirm; editor: confirm if the terminal is in the editor; panel: confirm if the terminal is in the panel; always: confirm if the terminal is either in the editor or panel."},
		{Key: settingConfirmOnExit, Type: "string", Default: "never", Enum: []any{"never", "always", "hasChildProcesses"}, Category: cat,
			Description: "Controls whether to confirm when the window closes if there are active terminal sessions. never: never confirm; always: always confirm if there are terminals; hasChildProcesses: confirm if there are any terminals that have child processes."},
		{Key: settingTabsEnabled, Type: "boolean", Default: true, Category: cat,
			Description: "Controls whether terminal tabs display as a list to the side of the terminal. When this is disabled a dropdown will display instead."},
		{Key: settingTabsLocation, Type: "string", Default: "right", Enum: []any{"left", "right"}, Category: cat,
			Description: "Controls the location of the terminal tabs, either to the left or right of the actual terminal(s)."},
		{Key: settingTabsHide, Type: "string", Default: "singleTerminal", Enum: []any{"never", "singleTerminal", "singleGroup"}, Category: cat,
			Description: "Controls whether the terminal tabs view will hide under certain conditions. never: never hide the terminal tabs view; singleTerminal: hide the terminal tabs view when there is only a single terminal opened; singleGroup: hide the terminal tabs view when there is only a single terminal group opened."},
		{Key: settingTabsTitle, Type: "string", Default: "${process}", Category: cat,
			Description: "Controls the terminal title. Variables: ${cwd} the terminal's current working directory, ${cwdFolder} the terminal's current working directory name, ${process} the name of the foreground process of the terminal (its shell), ${sequence} the name provided to the terminal by the process, ${local} indicates a local terminal."},
		{Key: settingTabsDefaultIco, Type: "string", Default: "terminal", Category: cat,
			Description: "A codicon ID to associate with terminal icons by default."},
		{Key: settingDetectLocale, Type: "string", Default: "auto", Enum: []any{"auto", "off", "on"}, Category: cat,
			Description: "Controls whether to detect and set the `$LANG` environment variable to a UTF-8 compliant option since the terminal only supports UTF-8 encoded data coming from the shell. auto: set `$LANG` if the existing variable does not exist or it does not end in `.UTF-8`; off: do not set it; on: always set it."},
		{Key: settingTabStopWidth, Type: "number", Default: 8.0, Minimum: f(1), Category: cat,
			Description: "The number of cells in a tab stop."},
		{Key: settingShellInteg, Type: "boolean", Default: true, Category: cat,
			Description: "Determines whether or not shell integration is auto-injected to support features like enhanced command tracking and current working directory detection. Shell integration works by injecting the shell with a startup script. The script gives Emetos insight into what is happening within the terminal. Supported shells: bash, fish, pwsh, zsh. This setting applies only when terminals are created, so you will need to restart your terminals for it to take effect. Note that the script injection may not work if you have custom arguments defined in the terminal profile, have a complex bash `PROMPT_COMMAND`, or other unsupported setup. To disable decorations, see terminal.integrated.shellIntegration.decorationsEnabled."},
		{Key: settingDecorations, Type: "string", Default: "both", Enum: []any{"both", "gutter", "overviewRuler", "never"}, Category: cat,
			Description: "When shell integration is enabled, adds a decoration for each command. both: show decorations in the gutter (left) and overview ruler (right); gutter: show gutter decorations to the left of the terminal; overviewRuler: show overview ruler decorations to the right of the terminal (Emetos has no overview ruler in the terminal, so this shows none); never: do not show decorations."},
		{Key: settingQuickFix, Type: "boolean", Default: true, Category: cat,
			Description: "When shell integration is enabled, enables quick fixes for terminal commands that appear as a lightbulb to the left of the prompt."},
		{Key: settingHistory, Type: "number", Default: 100.0, Category: cat,
			Description: "Controls the number of recently used commands to keep in the terminal command history. Set to 0 to disable terminal command history."},
		{Key: settingStickyEnabled, Type: "boolean", Default: true, Category: cat,
			Description: "Shows the current command at the top of the terminal. This feature requires shell integration to be activated. See terminal.integrated.shellIntegration.enabled."},
		{Key: settingStickyMaxLines, Type: "number", Default: 5.0, Minimum: f(1), Maximum: f(10), Category: cat,
			Description: "Defines the maximum number of sticky lines to show. Sticky scroll lines will never exceed 40% of the viewport regardless of this setting."},
		{Key: settingStickyIgnored, Type: "array", Default: defaultStickyIgnored, Category: cat,
			Description: "A list of commands that should not trigger sticky scroll. When a command from this list is detected, the sticky scroll overlay will be hidden."},
		{Key: settingSplitCwd, Type: "string", Default: "inherited", Enum: []any{"workspaceRoot", "initial", "inherited"}, Category: cat,
			Description: "Controls the working directory a split terminal starts with. workspaceRoot: a new split terminal will use the workspace root as the working directory; initial: a new split terminal will use the working directory that the parent terminal started with; inherited: a new split terminal will use the working directory of the parent terminal."},
		// terminal IntelliSense (terminalSuggestConfiguration.ts): the settings Emetos has a use for
		{Key: settingSugEnabled, Type: "boolean", Default: true, Category: cat,
			Description: "Enables terminal IntelliSense suggestions (also known as autocomplete) for supported shells (bash, zsh, fish, pwsh). This requires terminal.integrated.shellIntegration.enabled to be enabled and working."},
		{Key: settingSugQuick, Type: "object", Category: cat, Default: map[string]any{"commands": "off", "arguments": "off", "unknown": "off"},
			Description: "Controls whether suggestions should automatically show up while typing. commands: for the first word of a command line; arguments: for anything after the first word; unknown: when it is unclear what the best suggestion is, files and folders are suggested as a fallback. Each is \"on\" or \"off\". Also be aware of terminal.integrated.suggest.suggestOnTriggerCharacters, which controls if suggestions are triggered by special characters."},
		{Key: settingSugTrigger, Type: "boolean", Default: false, Category: cat,
			Description: "Controls whether suggestions should automatically show up when typing trigger characters (the slash of a path)."},
		{Key: settingSugRunEnter, Type: "string", Default: "never", Enum: []any{"never", "exactMatch", "exactMatchIgnoreExtension", "always"}, Category: cat,
			Description: "Controls whether suggestions should run immediately when Enter (not Tab) is used to accept the result. never: never run on Enter; exactMatch: run on Enter when the suggestion is typed in its entirety; exactMatchIgnoreExtension: run on Enter when the suggestion is typed in its entirety or when a file is typed without its extension included; always: always run on Enter."},
		{Key: settingSugSelect, Type: "string", Default: "partial", Enum: []any{"partial", "always", "never"}, Category: cat,
			Description: "Controls how suggestion selection works in the integrated terminal. partial: partially select a suggestion when automatically triggering IntelliSense; Tab can be used to accept the first suggestion, only after navigating the suggestions via Down will Enter also accept the active suggestion; always: always select a suggestion when automatically triggering IntelliSense, Enter or Tab can be used to accept the first suggestion; never: never select a suggestion when automatically triggering IntelliSense, the list must be navigated via Down before Enter or Tab can be used to accept the active suggestion."},
		{Key: settingSugProvider, Type: "object", Category: cat, Default: map[string]any{providerCommands: true, providerFiles: true, providerHistory: true},
			Description: "Controls which suggestions show up. Providers are enabled by default; omit them by setting the id of the provider to false. terminal-suggest: the commands, the builtins of the shell and the completion specs; path: files and folders; history: lines of the command history."},
		{Key: settingSugCdPath, Type: "string", Default: "absolute", Enum: []any{"off", "relative", "absolute"}, Category: cat,
			Description: "Controls whether to enable $CDPATH support which exposes children of the folders in the $CDPATH variable regardless of the current working directory. off: disable the feature; relative: enable the feature and use relative paths; absolute: enable the feature and use absolute paths."},
		{Key: settingSugSpace, Type: "boolean", Default: false, Category: cat,
			Description: "Controls whether a space is automatically inserted after accepting a suggestion and re-trigger suggestions. Folders will never have a trailing space added."},
		{Key: settingSugUpArrow, Type: "boolean", Default: true, Category: cat,
			Description: "Determines whether the up arrow key navigates the command history when focus is on the first suggestion and navigation has not yet occurred. When set to false, the up arrow will move focus to the last suggestion instead."},
	}
}

// profileSetting is one entry of terminal.integrated.profiles.linux.
type profileSetting struct {
	Path any    `json:"path"`
	Args any    `json:"args"`
	Env  any    `json:"env"`
	Icon string `json:"icon"`
	// Overrides the name of the profile in the lists when set.
	Title string `json:"title"`
}

// weightOf reads a font weight setting: "normal", "bold" or a number from 1 to 1000; def when it is none of them.
func weightOf(v string, def int) int {
	switch strings.TrimSpace(v) {
	case "normal":
		return 400
	case "bold":
		return 700
	}
	n := 0
	for _, r := range strings.TrimSpace(v) {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
		if n > 1000 {
			return def
		}
	}
	if n < 1 {
		return def
	}
	return n
}
