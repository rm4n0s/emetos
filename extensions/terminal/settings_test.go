package terminal

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// settingsAPI is a fake API with the settings of the extension.
func settingsAPI(t *testing.T) *fakeapi.API {
	t.Helper()
	return fakeapi.New(t, New().Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "p", ID: "p"})
}

func TestDefaultsAreVSCodes(t *testing.T) {
	c := readConfig(settingsAPI(t).Settings())
	if c.scrollback != 1000 || c.fontSize != 14 || c.lineHeight != 1 || c.weight != 400 || c.weightBold != 700 ||
		c.cursorStyle != "block" || c.cursorBlink || c.cursorWidth != 1 || c.cursorInactive != "outline" || !c.boldBright ||
		c.minContrast != 4.5 || c.wheelSens != 1 || c.fastSens != 5 || c.copyOnSelection || c.rightClick != "default" ||
		c.multiLinePaste != "auto" || !c.tabsEnabled || c.tabsLocation != "right" || c.tabsHide != "singleTerminal" ||
		c.tabsTitle != "${process}" || c.sendKeys || !c.allowChords || c.visualBell || c.bellDuration != 1000 {
		t.Errorf("a default is not VS Code's: %+v", c)
	}
	if c.wordSeparators != " ()[]{}',\"`─‘’“”|" {
		t.Errorf("word separators = %q", c.wordSeparators)
	}
}

func TestSettingsHaveTheirBounds(t *testing.T) {
	tests := []struct {
		key   string
		value any
		check func(config) bool
	}{
		{settingFontSize, 2, func(c config) bool { return c.fontSize == 6 }},
		{settingFontSize, 500, func(c config) bool { return c.fontSize == 100 }},
		{settingScrollback, 99999999, func(c config) bool { return c.scrollback == 1000000 }},
		{settingScrollback, -4, func(c config) bool { return c.scrollback == 0 }},
		{settingLineHeight, 0, func(c config) bool { return c.lineHeight == 1 }},
		{settingFontWeight, "600", func(c config) bool { return c.weight == 600 }},
		{settingFontWeight, "nonsense", func(c config) bool { return c.weight == 400 }},
		{settingFontWeightBold, "normal", func(c config) bool { return c.weightBold == 400 }},
		{settingCursorWidth, 0, func(c config) bool { return c.cursorWidth == 1 }},
		{settingSkipShell, []string{"a.b", "-workbench.action.quickOpen"}, func(c config) bool {
			return c.skip.has("a.b") && !c.skip.has("workbench.action.quickOpen") && c.skip.has("workbench.action.showCommands")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			api := settingsAPI(t)
			api.SetSetting(tt.key, tt.value)
			if c := readConfig(api.Settings()); !tt.check(c) {
				t.Errorf("config after %s = %v: %+v", tt.key, tt.value, c)
			}
		})
	}
}

func TestEverySettingHasAKindADefaultAndADescription(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range settingSpecs() {
		if seen[s.Key] {
			t.Errorf("%s is declared twice", s.Key)
		}
		seen[s.Key] = true
		if !strings.HasPrefix(s.Key, "terminal.integrated.") || s.Description == "" || !extension.ValidSettingType(s.Type) {
			t.Errorf("setting %+v", s)
		}
	}
	// the settings VS Code's terminal has by default that the step builds
	for _, key := range []string{settingScrollback, settingFontSize, settingCursorStyle, settingRightClick, settingSkipShell, settingTabsHide} {
		if !seen[key] {
			t.Errorf("%s is not declared", key)
		}
	}
}

func TestFontFamilies(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Menlo, Monaco, 'Courier New', monospace", []string{"Menlo", "Monaco", "Courier New", "monospace"}},
		{`"Fira Code"`, []string{"Fira Code"}},
		{" , ,", nil},
	}
	for _, tt := range tests {
		if got := fontFamilies(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("fontFamilies(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWeightOf(t *testing.T) {
	tests := []struct {
		in   string
		def  int
		want int
	}{{"normal", 1, 400}, {"bold", 1, 700}, {"100", 1, 100}, {"1000", 1, 1000}, {"1001", 5, 5}, {"0", 5, 5}, {"bolder", 9, 9}, {"", 3, 3}}
	for _, tt := range tests {
		if got := weightOf(tt.in, tt.def); got != tt.want {
			t.Errorf("weightOf(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestProfilesAndTheDefaultProfile(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	env := func(vars ...string) environ {
		return environ{vars: vars, lookPath: exec.LookPath, home: "/home/u"}
	}
	tests := []struct {
		name     string
		settings map[string]any
		env      environ
		want     []string // the names of the profiles found, in order
		defName  string
		defPath  string
	}{
		{"nothing set: the shell of the user", nil, env("SHELL=" + sh), []string{"bash"}, "sh", sh},
		{"no SHELL: sh", nil, env("PATH=" + filepath.Dir(sh)), []string{"bash"}, "sh", ""},
		{"a profile that is named", map[string]any{
			settingProfiles:       map[string]any{"mine": map[string]any{"path": sh, "args": []string{"-i"}}, "gone": map[string]any{"path": "/nonexistent/x"}},
			settingDefaultProfile: "mine",
		}, env(), []string{"mine"}, "mine", sh},
		{"a default that is not found falls back", map[string]any{
			settingProfiles: map[string]any{"mine": map[string]any{"path": sh}}, settingDefaultProfile: "zsh2",
		}, env("SHELL=" + sh), []string{"mine"}, "mine", sh},
		{"a list of paths takes the first that exists", map[string]any{
			settingProfiles: map[string]any{"first": map[string]any{"path": []string{"/nonexistent/a", sh}}},
		}, env(), []string{"first"}, "sh", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := settingsAPI(t)
			for k, v := range tt.settings {
				api.SetSetting(k, v)
			}
			var got []string
			profiles := profilesOf(api.Settings(), tt.env)
			for _, p := range profiles {
				got = append(got, p.name)
			}
			if tt.name == "nothing set: the shell of the user" || tt.name == "no SHELL: sh" {
				// the default profile list names bash, zsh, fish, tmux and pwsh: the ones installed are found
				got = nil
				for _, p := range profiles {
					if p.name == "bash" {
						got = []string{"bash"}
					}
				}
				if _, err := exec.LookPath("bash"); err != nil {
					got = []string{"bash"}
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("profiles = %v, want %v", got, tt.want)
			}
			def := defaultProfileOf(api.Settings(), tt.env, profiles)
			if def.name != tt.defName || (tt.defPath != "" && def.path != tt.defPath) {
				t.Errorf("default profile = %+v, want %s %s", def, tt.defName, tt.defPath)
			}
		})
	}
}

func TestTheEnvironmentOfATerminal(t *testing.T) {
	str := func(s string) *string { return &s }
	base := []string{"PATH=/bin", "HOME=/home/u", "LANG=C", "FOO=1", "BAR=2"}
	tests := []struct {
		name    string
		setting map[string]*string
		profile map[string]*string
		locale  string
		want    map[string]string // "" is: the variable is not there
	}{
		{"the terminal's own variables", nil, nil, "off", map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor", "TERM_PROGRAM": "emetos", "TERM_PROGRAM_VERSION": "9.9", "FOO": "1", "LANG": "C"}},
		{"a setting adds and a profile overrides", map[string]*string{"FOO": str("a"), "NEW": str("n")}, map[string]*string{"FOO": str("b")}, "off", map[string]string{"FOO": "b", "NEW": "n"}},
		{"null deletes", map[string]*string{"BAR": nil}, nil, "off", map[string]string{"BAR": ""}},
		{"the locale is set when it is not UTF-8", nil, nil, "auto", map[string]string{"LANG": "en_US.UTF-8"}},
		{"on always sets it", nil, nil, "on", map[string]string{"LANG": "en_US.UTF-8"}},
		{"the terminal's variables win over the settings", map[string]*string{"TERM": str("dumb")}, nil, "off", map[string]string{"TERM": "xterm-256color"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]string{}
			for _, kv := range buildEnv(base, tt.setting, tt.profile, tt.locale, "9.9") {
				k, v, _ := strings.Cut(kv, "=")
				got[k] = v
			}
			for k, want := range tt.want {
				if got[k] != want {
					t.Errorf("%s = %q, want %q", k, got[k], want)
				}
			}
		})
	}
	// an environment with a UTF-8 locale keeps it
	for _, kv := range buildEnv([]string{"LANG=de_DE.UTF-8"}, nil, nil, "auto", "1") {
		if kv == "LANG=en_US.UTF-8" {
			t.Error("a UTF-8 locale was replaced")
		}
	}
}

func TestStartDir(t *testing.T) {
	tests := []struct {
		cwd, folder, home, want string
	}{
		{"", "/work/p", "/home/u", "/work/p"},
		{"", "", "/home/u", "/home/u"},
		{"", "", "", "/"},
		{"${workspaceFolder}/sub", "/work/p", "/home/u", "/work/p/sub"},
		{"~/code", "/work/p", "/home/u", "/home/u/code"},
		{"rel", "/work/p", "/home/u", "/work/p/rel"},
		{"/abs", "/work/p", "/home/u", "/abs"},
		{"  ", "/work/p", "/home/u", "/work/p"},
	}
	for _, tt := range tests {
		if got := startDir(tt.cwd, tt.folder, tt.home); got != tt.want {
			t.Errorf("startDir(%q, %q, %q) = %q, want %q", tt.cwd, tt.folder, tt.home, got, tt.want)
		}
	}
}

func TestSkipShellDefaultsAreVSCodes(t *testing.T) {
	for _, id := range []string{
		"workbench.action.quickOpen", "workbench.action.showCommands", "workbench.action.terminal.new", "workbench.action.terminal.paste",
		"workbench.action.terminal.copySelection", "workbench.action.terminal.kill", "workbench.action.togglePanel",
		"workbench.action.nextEditor", "workbench.action.focusActiveEditorGroup", "workbench.action.debug.start",
	} {
		if !newSkipSet(nil).has(id) {
			t.Errorf("%s is not skipped by default", id)
		}
	}
	for _, id := range []string{"workbench.action.files.save", "workbench.action.closeActiveEditor", "editor.action.clipboardCopyAction"} {
		if newSkipSet(nil).has(id) {
			t.Errorf("%s is skipped by default: Ctrl+S and Ctrl+W go to the shell", id)
		}
	}
	if !slices.IsSorted(sortedDefaults()) {
		t.Error("sortedDefaults is not sorted")
	}
}

func TestContrastAndMix(t *testing.T) {
	if c := contrast(0xFFFFFFFF, 0xFF000000); c < 20.9 || c > 21.1 {
		t.Errorf("contrast of white on black = %v, want 21", c)
	}
	// a color that is too close to the background is moved away from it until the ratio is met
	bg := uint32(0xFF1E1E1E)
	got := ensureContrast(0xFF2A2A2A, bg, 4.5)
	if contrast(got, bg) < 4.5 {
		t.Errorf("ensureContrast gave %x with contrast %v", got, contrast(got, bg))
	}
	if got := ensureContrast(0xFFFFFFFF, bg, 4.5); got != 0xFFFFFFFF {
		t.Errorf("a color that has the contrast was changed to %x", got)
	}
	if m := mix(0xFF000000, 0xFFFFFFFF, 0.5); m != 0xFF808080 {
		t.Errorf("mix = %x", m)
	}
	// on a light background the color goes darker
	light := ensureContrast(0xFFE0E0E0, 0xFFF8F8F8, 4.5)
	if luminance(light) >= luminance(0xFFE0E0E0) {
		t.Errorf("on a light background the color went lighter: %x", light)
	}
}

func TestShapesFillTheCell(t *testing.T) {
	tests := []struct {
		name  string
		r     rune
		check func(m *mask) bool
	}{
		{"full block", '█', func(m *mask) bool { return allAbove(m, 0, 0, m.w, m.h, 255) }},
		{"lower half", '▄', func(m *mask) bool {
			return allAbove(m, 0, (m.h+1)/2, m.w, m.h, 255) && allBelow(m, 0, 0, m.w, m.h/2, 0)
		}},
		{"left half", '▌', func(m *mask) bool {
			return allAbove(m, 0, 0, m.w/2, m.h, 255) && allBelow(m, (m.w+1)/2, 0, m.w, m.h, 0)
		}},
		{"horizontal line reaches both edges", '─', func(m *mask) bool { return touchesLeftAndRight(m) && m.a[0] == 0 }},
		{"vertical line reaches both edges", '│', func(m *mask) bool { return touchesTopAndBottom(m) && m.a[0] == 0 }},
		{"a cross touches all four edges", '┼', touchesAllFour},
		{"braille with every dot", '⣿', func(m *mask) bool { return anyAbove(m, 0, 0, m.w, m.h) }},
		{"the empty braille", '⠀', func(m *mask) bool { return !anyAbove(m, 0, 0, m.w, m.h) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, size := range [][2]int{{8, 19}, {9, 20}, {13, 28}, {7, 15}} {
				if !isShape(tt.r) {
					t.Fatalf("%q is not a shape", tt.r)
				}
				if m := shapeMask(tt.r, size[0], size[1], max(size[0]/8, 1)); !tt.check(m) {
					t.Errorf("%q in a cell of %dx%d is wrong", tt.r, size[0], size[1])
				}
			}
		})
	}
	if isShape('a') || isShape('─'-1) {
		t.Error("a letter is drawn as a shape")
	}
}

// touchesLeftAndRight tells whether a row of the mask has coverage at both side edges.
func touchesLeftAndRight(m *mask) bool {
	for y := 0; y < m.h; y++ {
		if m.a[y*m.w] > 0 && m.a[y*m.w+m.w-1] > 0 {
			return true
		}
	}
	return false
}

// touchesTopAndBottom tells whether a column of the mask has coverage at the top and the bottom edge.
func touchesTopAndBottom(m *mask) bool {
	for x := 0; x < m.w; x++ {
		if m.a[x] > 0 && m.a[(m.h-1)*m.w+x] > 0 {
			return true
		}
	}
	return false
}

func touchesAllFour(m *mask) bool { return touchesLeftAndRight(m) && touchesTopAndBottom(m) }

func allAbove(m *mask, x0, y0, x1, y1 int, v uint8) bool {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if m.a[y*m.w+x] < v {
				return false
			}
		}
	}
	return true
}

func allBelow(m *mask, x0, y0, x1, y1 int, v uint8) bool {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if m.a[y*m.w+x] > v {
				return false
			}
		}
	}
	return true
}

func anyAbove(m *mask, x0, y0, x1, y1 int) bool { return !allBelow(m, x0, y0, x1, y1, 0) }
