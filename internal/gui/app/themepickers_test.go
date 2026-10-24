package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of the theme pickers (step 8.4): the color theme picker and the file icon theme picker preview in the
// real window, write the setting on Enter and show the theme of the settings again when they are closed.

// The editor backgrounds of the themes, and the glyphs of the Seti icons of the files of the fixture.
var (
	darkBackground  = ui.NewColor(0xFF1F1F1F) // Dark Modern
	lightBackground = ui.NewColor(0xFFFFFFFF) // Light Modern
	plusBackground  = ui.NewColor(0xFF1E1E1E) // Dark+
	hcBackground    = ui.NewColor(0xFF000000) // Dark High Contrast
)

const (
	goGlyph = "\ue03a" // main.go
	mdGlyph = "\ue04d" // README.md
)

// themeApp is a window over a folder with a few files, with the explorer showing them.
type themeApp struct {
	*groupApp
	paths    storage.Paths
	folder   string
	userFile string
}

// newThemeApp makes a window over a folder with main.go and README.md; user is the text of the user's
// settings.json when it is not empty (otherwise the usual one of the tests), workspace the text of the
// settings.json of the folder.
func newThemeApp(t *testing.T, user, workspace string) *themeApp {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"editor.fontSize": 14, "files.hotExit": "off"})
	userFile, err := settings.UserSettingsPath(paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" {
		if err := os.WriteFile(userFile, []byte(user), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	folder := t.TempDir()
	writeTree(t, folder, "main.go", "README.md")
	if workspace != "" {
		if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, ".emetos", "settings.json"), []byte(workspace), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	win, err := NewWindow(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), folder)
	if err != nil {
		t.Fatal(err)
	}
	g := newGroupAppWith(t, win, testWindowSize, nil, nil)
	return &themeApp{groupApp: g, paths: paths, folder: win.Folder, userFile: userFile}
}

// background is the editor background of the theme the window shows.
func (a *themeApp) background() ui.Color { return uiBackground(a.t, a.ts) }

// userText is the text of the user's settings.json.
func (a *themeApp) userText() string {
	a.t.Helper()
	data, err := os.ReadFile(a.userFile)
	if err != nil {
		a.t.Fatal(err)
	}
	return string(data)
}

// workspaceText is the text of the settings.json of the folder, "" when there is none.
func (a *themeApp) workspaceText() string {
	data, err := os.ReadFile(filepath.Join(a.folder, ".emetos", "settings.json"))
	if err != nil {
		return ""
	}
	return string(data)
}

// pickerRows are the labels of the rows of the picker that is shown.
func (a *themeApp) pickerRows() []string {
	var out []string
	for _, r := range a.win.QuickInput.Picker().Rows() {
		out = append(out, r.Item.Label)
	}
	return out
}

// activeRow is the label of the active row of the picker.
func (a *themeApp) activeRow() string {
	a.t.Helper()
	r, ok := a.win.QuickInput.Picker().Active()
	if !ok {
		return ""
	}
	return r.Item.Label
}

// hasIcon tells whether the glyph of a Seti icon is on screen.
func (a *themeApp) hasIcon(glyph string) bool {
	return a.ts.Exists(gluttertest.ByText(glyph))
}

// setting is the value of a setting of the window.
func (a *themeApp) value(key string) any { return settings.Get[any](a.win.Settings, key, "") }

func TestCtrlKCtrlTOpensTheColorThemePicker(t *testing.T) {
	a := newThemeApp(t, "", "")
	if a.win.QuickInput.Visible() {
		t.Fatal("the picker is shown before it is asked for")
	}
	a.press("ctrl+k ctrl+t")
	ctl := a.win.QuickInput
	if !ctl.Visible() || ctl.Picker().Placeholder() != "Select Color Theme (Up/Down Keys to Preview)" {
		t.Fatalf("visible %v, placeholder %q", ctl.Visible(), ctl.Picker().Placeholder())
	}
	want := []string{"Light Modern", "Light+", "Dark Modern", "Dark+", "Dark High Contrast", "Light High Contrast"}
	if got := a.pickerRows(); !reflect.DeepEqual(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	// the groups are the separators of the first row of each group
	seps := map[string]string{}
	for _, r := range ctl.Picker().Rows() {
		if r.Separator != "" {
			seps[r.Item.Label] = r.Separator
		}
	}
	if wantSeps := map[string]string{"Light Modern": "light themes", "Dark Modern": "dark themes", "Dark High Contrast": "high contrast themes"}; !reflect.DeepEqual(seps, wantSeps) {
		t.Errorf("separators %v, want %v", seps, wantSeps)
	}
	if a.activeRow() != "Dark Modern" {
		t.Errorf("the theme in use is not the active item: %q", a.activeRow())
	}
	descriptions := map[string]string{}
	for _, r := range ctl.Picker().Rows() {
		descriptions[r.Item.Label] = r.Item.Description
	}
	if descriptions["Dark Modern"] != "Default Dark" || descriptions["Light Modern"] != "Default Light" || descriptions["Dark High Contrast"] != "Default High Contrast" {
		t.Errorf("descriptions %v", descriptions)
	}
	a.noErrors()
}

func TestColorThemePickerPreviewsAndEscapeReverts(t *testing.T) {
	a := newThemeApp(t, "", "")
	before := a.userText()
	a.press("ctrl+k ctrl+t")
	// from Dark Modern: Down is Dark+ (dark), then High Contrast (black), then Light High Contrast, and
	// Up from Light Modern wraps to the last row
	tests := []struct {
		name   string
		keys   []string
		active string
		bg     ui.Color
	}{
		{"down to Dark+", []string{"down"}, "Dark+", plusBackground},
		{"down to Dark High Contrast", []string{"down"}, "Dark High Contrast", hcBackground},
		{"up and up again to Dark Modern", []string{"up", "up"}, "Dark Modern", darkBackground},
		{"up to Light+", []string{"up"}, "Light+", lightBackground},
		{"up to Light Modern", []string{"up"}, "Light Modern", lightBackground},
	}
	for _, tc := range tests {
		for _, k := range tc.keys {
			a.press(k)
		}
		if a.activeRow() != tc.active {
			t.Fatalf("%s: active %q", tc.name, a.activeRow())
		}
		if got := a.background(); got != tc.bg {
			t.Errorf("%s: background %v, want %v", tc.name, got, tc.bg)
		}
		if got := a.win.Themes.CurrentInfo().Label; got != tc.active {
			t.Errorf("%s: the theme in use is %q", tc.name, got)
		}
		if !a.win.Themes.Previewing() && tc.active != "Dark Modern" {
			t.Errorf("%s: no preview is on", tc.name)
		}
		if a.userText() != before {
			t.Errorf("%s: the preview wrote the settings:\n%s", tc.name, a.userText())
		}
	}
	a.press("escape")
	if a.win.QuickInput.Visible() {
		t.Error("escape left the picker open")
	}
	if got := a.background(); got != darkBackground {
		t.Errorf("after escape the background is %v", got)
	}
	if a.win.Themes.Previewing() || a.win.Themes.CurrentInfo().Label != "Dark Modern" {
		t.Error("the preview is still on")
	}
	if a.userText() != before {
		t.Error("escape wrote the settings")
	}
	a.noErrors()
}

func TestColorThemePickerEnterWritesTheSetting(t *testing.T) {
	user := "{\n\t// my editor\n\t\"editor.fontSize\": 14,\n\t\"files.hotExit\": \"off\" // no backups\n}\n"
	a := newThemeApp(t, user, "")
	a.press("ctrl+k ctrl+t")
	a.press("up") // Dark Modern wraps? no: Up from the first row of Dark is Light+
	if a.activeRow() != "Light+" && a.activeRow() != "Light Modern" {
		// the dark group starts after the light one: Up from Dark Modern is Light+
		t.Fatalf("active %q", a.activeRow())
	}
	want := a.activeRow()
	a.press("enter")
	if a.win.QuickInput.Visible() {
		t.Error("enter left the picker open")
	}
	if got := a.win.Themes.CurrentInfo().Label; got != want {
		t.Errorf("the theme is %q, want %q", got, want)
	}
	if a.win.Themes.Previewing() {
		t.Error("the preview is still on")
	}
	if got := a.background(); got != lightBackground {
		t.Errorf("background %v", got)
	}
	text := a.userText()
	for _, keep := range []string{"// my editor", "// no backups", `"editor.fontSize": 14`} {
		if !strings.Contains(text, keep) {
			t.Errorf("the settings lost %q:\n%s", keep, text)
		}
	}
	if !strings.Contains(text, `"workbench.colorTheme": "`+want+`"`) {
		t.Errorf("the settings do not name %q:\n%s", want, text)
	}
	if got := a.value("workbench.colorTheme"); got != want {
		t.Errorf("the setting is %v", got)
	}
	if a.workspaceText() != "" {
		t.Error("a workspace file was written")
	}
	// the choice stays after the picker opens and closes again
	a.press("ctrl+k ctrl+t")
	if a.activeRow() != want {
		t.Errorf("the picker opens on %q, want %q", a.activeRow(), want)
	}
	a.press("escape")
	if got := a.win.Themes.CurrentInfo().Label; got != want {
		t.Errorf("after escape the theme is %q", got)
	}
	a.noErrors()
}

func TestColorThemeIsWrittenWhereTheSettingIs(t *testing.T) {
	tests := []struct {
		name      string
		workspace string
		wantFile  string // "user" or "workspace"
	}{
		{"no workspace setting: the user's file", "", "user"},
		{"a workspace file without the setting: the user's file", "{\n\t\"editor.tabSize\": 2\n}\n", "user"},
		{"the workspace sets it: the workspace's file", "{\n\t\"workbench.colorTheme\": \"Dark+\"\n}\n", "workspace"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newThemeApp(t, "", tc.workspace)
			userBefore := a.userText()
			a.press("ctrl+k ctrl+t")
			// Light Modern is the first row
			a.win.QuickInput.Picker().First()
			a.pump()
			if a.activeRow() != "Light Modern" {
				t.Fatalf("active %q", a.activeRow())
			}
			a.press("enter")
			userAfter, workspaceAfter := a.userText(), a.workspaceText()
			const line = `"workbench.colorTheme": "Light Modern"`
			switch tc.wantFile {
			case "user":
				if !strings.Contains(userAfter, line) {
					t.Errorf("the user's file lacks the setting:\n%s", userAfter)
				}
				if workspaceAfter != tc.workspace {
					t.Errorf("the workspace file changed:\n%s", workspaceAfter)
				}
			case "workspace":
				if !strings.Contains(workspaceAfter, line) {
					t.Errorf("the workspace file lacks the setting:\n%s", workspaceAfter)
				}
				if userAfter != userBefore {
					t.Errorf("the user's file changed:\n%s", userAfter)
				}
			}
			if got := a.win.Themes.CurrentInfo().Label; got != "Light Modern" {
				t.Errorf("the theme is %q", got)
			}
			a.noErrors()
		})
	}
}

func TestColorThemePickerRevertsWhenItClosesAnyOtherWay(t *testing.T) {
	tests := []struct {
		name  string
		close func(a *themeApp)
	}{
		{"hide", func(a *themeApp) { a.win.QuickInput.Hide() }},
		{"the command palette takes its place", func(a *themeApp) { a.press("ctrl+shift+p") }},
		{"the other theme picker takes its place", func(a *themeApp) { a.run("workbench.action.selectIconTheme") }},
		{"the same picker again", func(a *themeApp) { a.run("workbench.action.selectTheme") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newThemeApp(t, "", "")
			a.press("ctrl+k ctrl+t")
			a.press("up")
			a.press("up")
			if a.background() != lightBackground {
				t.Fatalf("no preview: %v", a.background())
			}
			tc.close(a)
			a.pump()
			if a.win.Themes.Previewing() {
				t.Error("the preview is still on")
			}
			if got := a.background(); got != darkBackground {
				t.Errorf("background %v after the picker was replaced", got)
			}
			a.win.QuickInput.Hide()
			a.noErrors()
		})
	}
}

func TestColorThemePickerFilterPreviewsTheMatch(t *testing.T) {
	a := newThemeApp(t, "", "")
	a.press("ctrl+k ctrl+t")
	a.win.QuickInput.SetValue("high")
	a.pump()
	if got := a.activeRow(); got != "Dark High Contrast" {
		t.Fatalf("active %q rows %v", got, a.pickerRows())
	}
	if a.background() != hcBackground {
		t.Errorf("background %v", a.background())
	}
	// no match: the theme in use is shown again
	a.win.QuickInput.SetValue("zzzz")
	a.pump()
	if a.background() != darkBackground || a.win.Themes.Previewing() {
		t.Errorf("background %v previewing %v", a.background(), a.win.Themes.Previewing())
	}
	a.win.QuickInput.Hide()
	a.noErrors()
}

func TestTheSettingWinsOverAPreview(t *testing.T) {
	a := newThemeApp(t, "", "")
	a.press("ctrl+k ctrl+t")
	a.press("up")
	a.press("up")
	if a.background() != lightBackground {
		t.Fatalf("no preview: %v", a.background())
	}
	// settings.json changes while the picker is open: the setting is shown, and the picker closing changes nothing
	a.setting("workbench.colorTheme", "Dark High Contrast")
	if a.win.Themes.Previewing() {
		t.Error("the preview goes on over a changed setting")
	}
	if a.background() != hcBackground {
		t.Errorf("background %v, want the theme of the setting", a.background())
	}
	a.press("escape")
	if a.background() != hcBackground || a.win.Themes.CurrentInfo().Label != "Dark High Contrast" {
		t.Errorf("after escape the theme is %q", a.win.Themes.CurrentInfo().Label)
	}
	a.noErrors()
}

func TestColorThemePickerFailsToWriteAFileWithErrors(t *testing.T) {
	a := newThemeApp(t, "{ \"editor.fontSize\": ", "")
	// the store holds the bad file: the write fails and the theme goes back
	a.press("ctrl+k ctrl+t")
	a.press("up")
	a.press("up")
	a.press("enter")
	if a.win.Themes.Previewing() || a.background() != darkBackground {
		t.Errorf("the failed choice stays: previewing %v background %v", a.win.Themes.Previewing(), a.background())
	}
	var told bool
	for _, item := range a.win.Notify.Items() {
		told = told || strings.Contains(item.Message, "Cannot save the color theme")
	}
	if !told {
		t.Error("the user is not told that the setting was not saved")
	}
}

func TestColorThemePickerIncludesTheUserThemes(t *testing.T) {
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"editor.fontSize": 14, "files.hotExit": "off"})
	if err := os.MkdirAll(paths.ThemesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := `{"name": "My Sepia", "type": "light", "colors": {"editor.background": "#f4ecd8"}}`
	if err := os.WriteFile(filepath.Join(paths.ThemesDir(), "sepia.json"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	win, err := NewWindow(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), "")
	if err != nil {
		t.Fatal(err)
	}
	a := &themeApp{groupApp: newGroupAppWith(t, win, testWindowSize, nil, nil), paths: paths}
	a.press("ctrl+k ctrl+t")
	if got := a.pickerRows(); !reflect.DeepEqual(got, []string{"Light Modern", "Light+", "My Sepia", "Dark Modern", "Dark+", "Dark High Contrast", "Light High Contrast"}) {
		t.Fatalf("rows %v", got)
	}
	a.win.QuickInput.Picker().SetActive(2)
	a.pump()
	if got := a.background(); got != ui.NewColor(0xFFF4ECD8) {
		t.Errorf("the user theme is not previewed: %v", got)
	}
	a.press("enter")
	if got := a.value("workbench.colorTheme"); got != "My Sepia" {
		t.Errorf("the setting is %v", got)
	}
	a.noErrors()
}

func TestColorThemePreviewReachesEveryWindow(t *testing.T) {
	env := newMultiEnv(t, nil)
	one, two := env.folder("one", "a.go"), env.folder("two", "b.go")
	app := env.run(Options{Folder: one})
	if _, err := app.m.Open(WindowSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	app.settle()
	if app.m.Count() != 2 {
		t.Fatalf("%d windows", app.m.Count())
	}
	w1, w2 := app.windowOf(one), app.windowOf(two)
	backgrounds := func() []ui.Color {
		var out []ui.Color
		for _, w := range app.ts.Widgets(gluttertest.ByType[*kit.Theme]()) {
			out = append(out, w.(*kit.Theme).Colors().EditorBackground)
		}
		return out
	}
	if got := backgrounds(); len(got) != 2 || got[0] != darkBackground || got[1] != darkBackground {
		t.Fatalf("backgrounds %v", got)
	}
	// the picker of one window previews in both
	if err := app.command(w1, "workbench.action.selectTheme", nil); err != nil {
		t.Fatal(err)
	}
	w1.QuickInput.Picker().First()
	app.settle()
	if got := backgrounds(); got[0] != lightBackground || got[1] != lightBackground {
		t.Errorf("backgrounds while previewing %v", got)
	}
	w1.QuickInput.Hide()
	app.settle()
	if got := backgrounds(); got[0] != darkBackground || got[1] != darkBackground {
		t.Errorf("backgrounds after the cancel %v", got)
	}
	// Enter keeps the theme in both
	if err := app.command(w2, "workbench.action.selectTheme", nil); err != nil {
		t.Fatal(err)
	}
	w2.QuickInput.Picker().First()
	w2.QuickInput.Accept(quickinput.KeyMods{})
	app.settle()
	if got := backgrounds(); got[0] != lightBackground || got[1] != lightBackground {
		t.Errorf("backgrounds after Enter %v", got)
	}
	if w1.Themes.Previewing() {
		t.Error("the preview is still on")
	}
}

func TestTheFileIconThemePickerHasNoneAndSeti(t *testing.T) {
	a := newThemeApp(t, "", "")
	a.run("workbench.action.selectIconTheme")
	ctl := a.win.QuickInput
	if !ctl.Visible() || ctl.Picker().Placeholder() != "Select File Icon Theme (Up/Down Keys to Preview)" {
		t.Fatalf("visible %v, placeholder %q", ctl.Visible(), ctl.Picker().Placeholder())
	}
	if got := a.pickerRows(); !reflect.DeepEqual(got, []string{"None", "Seti (Visual Studio Code)"}) {
		t.Errorf("rows %v", got)
	}
	rows := ctl.Picker().Rows()
	if rows[0].Separator != "file icon themes" || rows[0].Item.Description != "Disable File Icons" || rows[1].Item.Description != "vs-seti" {
		t.Errorf("separator %q, descriptions %q %q", rows[0].Separator, rows[0].Item.Description, rows[1].Item.Description)
	}
	// Seti is the theme in use and the active item
	if a.activeRow() != "Seti (Visual Studio Code)" {
		t.Errorf("active %q", a.activeRow())
	}
	a.noErrors()
}

func TestFileIconThemeActiveItemFollowsTheSetting(t *testing.T) {
	tests := []struct {
		name string
		user string
		want string
	}{
		{"the default", "", "Seti (Visual Studio Code)"},
		{"null", "{\"workbench.iconTheme\": null}\n", "None"},
		{"an unknown theme shows no icons", "{\"workbench.iconTheme\": \"nope\"}\n", "None"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newThemeApp(t, tc.user, "")
			a.run("workbench.action.selectIconTheme")
			if a.activeRow() != tc.want {
				t.Errorf("active %q, want %q", a.activeRow(), tc.want)
			}
			a.win.QuickInput.Hide()
		})
	}
}

func TestFileIconThemePickerPreviewsAndEscapeReverts(t *testing.T) {
	a := newThemeApp(t, "", "")
	before := a.userText()
	if !a.hasIcon(goGlyph) || !a.hasIcon(mdGlyph) {
		t.Fatal("the explorer has no Seti icons to begin with")
	}
	a.run("workbench.action.selectIconTheme")
	a.press("up") // None
	if a.activeRow() != "None" {
		t.Fatalf("active %q", a.activeRow())
	}
	if a.hasIcon(goGlyph) || a.hasIcon(mdGlyph) {
		t.Error("None leaves icons in the explorer")
	}
	if _, on := a.win.Process.FileIconThemePreview(); !on || a.win.FileIcons() != nil {
		t.Error("the window does not show no icons")
	}
	if a.userText() != before {
		t.Errorf("the preview wrote the settings:\n%s", a.userText())
	}
	a.press("down") // Seti again
	if !a.hasIcon(goGlyph) || !a.hasIcon(mdGlyph) {
		t.Error("Seti does not bring the icons back")
	}
	a.press("up")
	a.press("escape")
	if _, on := a.win.Process.FileIconThemePreview(); on {
		t.Error("the preview is still on")
	}
	if !a.hasIcon(goGlyph) || !a.hasIcon(mdGlyph) {
		t.Error("escape does not bring the icons back")
	}
	if a.userText() != before {
		t.Errorf("escape wrote the settings:\n%s", a.userText())
	}
	a.noErrors()
}

func TestFileIconThemePickerEnterWritesTheSetting(t *testing.T) {
	user := "{\n\t// icons\n\t\"editor.fontSize\": 14\n}\n"
	a := newThemeApp(t, user, "")
	a.run("workbench.action.selectIconTheme")
	a.press("up") // None
	a.press("enter")
	text := a.userText()
	if !strings.Contains(text, `"workbench.iconTheme": null`) || !strings.Contains(text, "// icons") {
		t.Errorf("the settings:\n%s", text)
	}
	if a.hasIcon(goGlyph) || a.win.FileIcons() != nil {
		t.Error("the icons stay after None was chosen")
	}
	if _, on := a.win.Process.FileIconThemePreview(); on {
		t.Error("the preview is still on")
	}
	// the picker opens on None now, and Seti brings the icons back and writes the id
	a.run("workbench.action.selectIconTheme")
	if a.activeRow() != "None" {
		t.Fatalf("active %q", a.activeRow())
	}
	a.press("down")
	a.press("enter")
	if text := a.userText(); !strings.Contains(text, `"workbench.iconTheme": "vs-seti"`) {
		t.Errorf("the settings:\n%s", text)
	}
	if !a.hasIcon(goGlyph) {
		t.Error("Seti does not bring the icons back")
	}
	a.noErrors()
}

func TestFileIconThemeIsWrittenWhereTheSettingIs(t *testing.T) {
	a := newThemeApp(t, "", "{\n\t\"workbench.iconTheme\": \"vs-seti\"\n}\n")
	userBefore := a.userText()
	a.run("workbench.action.selectIconTheme")
	a.press("up")
	a.press("enter")
	if text := a.workspaceText(); !strings.Contains(text, `"workbench.iconTheme": null`) {
		t.Errorf("the workspace file:\n%s", text)
	}
	if a.userText() != userBefore {
		t.Errorf("the user's file changed:\n%s", a.userText())
	}
	a.noErrors()
}

func TestFileIconThemeSettingEndsThePreview(t *testing.T) {
	a := newThemeApp(t, "{\"workbench.iconTheme\": null}\n", "")
	a.run("workbench.action.selectIconTheme")
	a.press("down") // Seti
	if !a.hasIcon(goGlyph) {
		t.Fatal("no preview")
	}
	// the setting changes while the picker is open: it wins over the theme that is tried
	a.setting("workbench.iconTheme", "nope")
	if _, on := a.win.Process.FileIconThemePreview(); on || a.hasIcon(goGlyph) {
		t.Errorf("the preview goes on over a changed setting")
	}
	a.win.QuickInput.Hide()
	a.pump()
	if a.hasIcon(goGlyph) {
		t.Error("closing the picker changes the icons")
	}
	a.noErrors()
}

func TestFileIconThemePreviewReachesEveryWindow(t *testing.T) {
	env := newMultiEnv(t, nil)
	one, two := env.folder("one", "a.go"), env.folder("two", "b.go")
	app := env.run(Options{Folder: one})
	if _, err := app.m.Open(WindowSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	app.settle()
	w1, w2 := app.windowOf(one), app.windowOf(two)
	icons := func() int { return app.ts.Count(gluttertest.ByText(goGlyph)) }
	if got := icons(); got != 2 {
		t.Fatalf("%d Go icons to begin with, want one in each window", got)
	}
	if err := app.command(w1, "workbench.action.selectIconTheme", nil); err != nil {
		t.Fatal(err)
	}
	w1.QuickInput.Picker().First() // None
	app.settle()
	if got := icons(); got != 0 {
		t.Errorf("%d Go icons while previewing None", got)
	}
	w1.QuickInput.Hide()
	app.settle()
	if got := icons(); got != 2 {
		t.Errorf("%d Go icons after the cancel", got)
	}
	// Enter in the second window keeps it in both
	if err := app.command(w2, "workbench.action.selectIconTheme", nil); err != nil {
		t.Fatal(err)
	}
	w2.QuickInput.Picker().First()
	w2.QuickInput.Accept(quickinput.KeyMods{})
	app.settle()
	if got := icons(); got != 0 {
		t.Errorf("%d Go icons after Enter", got)
	}
}

func TestThePickerDoesNotOutliveItsWindow(t *testing.T) {
	a := newThemeApp(t, "", "")
	a.press("ctrl+k ctrl+t")
	a.press("up")
	a.press("up")
	if !a.win.Themes.Previewing() {
		t.Fatal("no preview")
	}
	// the window goes away with the picker open
	a.ts.PumpWidget(widgets.NewSizedBoxShrink(nil))
	if a.win.Themes.Previewing() {
		t.Error("the preview of a window that is gone stays")
	}
}

func TestQuickOpenShowsTheIconsOfTheTheme(t *testing.T) {
	tests := []struct {
		name  string
		user  string
		icons int // Go icons on screen: the explorer's and the result of the quick open
	}{
		{"Seti", "", 2},
		{"None", "{\"workbench.iconTheme\": null}\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newThemeApp(t, tc.user, "")
			idx := a.win.FileIndex()
			a.waitFor("the walk", func() bool { return idx.Snapshot().Complete })
			a.press("ctrl+p")
			a.ts.EnterText(gluttertest.First(gluttertest.ByType[*widgets.EditableText]()), "main")
			a.waitFor("the result", func() bool {
				return slices.ContainsFunc(a.win.QuickInput.Picker().Rows(), func(r quickinput.Row) bool { return r.Item.Label == "main.go" })
			})
			a.pump()
			if got := a.ts.Count(gluttertest.ByText(goGlyph)); got != tc.icons {
				t.Errorf("%d Go icons on screen, want %d", got, tc.icons)
			}
			a.win.QuickInput.Hide()
		})
	}
}

func TestThemesInTheManageAndTheFileMenus(t *testing.T) {
	tests := []struct {
		name        string
		menu        string
		parents     []string // the rows to open before Themes
		row         string
		placeholder string
		key         string
	}{
		{"Color Theme in the Manage menu", "GlobalActivity", nil, "Color Theme", "Select Color Theme (Up/Down Keys to Preview)", "Ctrl+K Ctrl+T"},
		{"File Icon Theme in the Manage menu", "GlobalActivity", nil, "File Icon Theme", "Select File Icon Theme (Up/Down Keys to Preview)", ""},
		{"Color Theme in the File menu", "MenubarFileMenu", []string{"Preferences"}, "Color Theme", "Select Color Theme (Up/Down Keys to Preview)", "Ctrl+K Ctrl+T"},
		{"File Icon Theme in the File menu", "MenubarFileMenu", []string{"Preferences"}, "File Icon Theme", "Select File Icon Theme (Up/Down Keys to Preview)", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newThemeApp(t, "", "")
			a.run2("dev.openMenu", map[string]any{"id": tc.menu})
			for _, p := range tc.parents {
				a.ts.Tap(gluttertest.ByText(p))
				a.pump()
			}
			a.ts.Tap(gluttertest.ByText("Themes"))
			a.pump()
			for _, it := range a.openMenu() {
				if kit.MenuLabelText(it.Label) == tc.row && it.Keybinding != tc.key {
					t.Errorf("the key of the row %q", it.Keybinding)
				}
			}
			a.ts.Tap(gluttertest.ByText(tc.row))
			a.pump()
			if !a.win.QuickInput.Visible() || a.win.QuickInput.Picker().Placeholder() != tc.placeholder {
				t.Errorf("visible %v, placeholder %q", a.win.QuickInput.Visible(), a.win.QuickInput.Picker().Placeholder())
			}
			a.win.QuickInput.Hide()
			a.noErrors()
		})
	}
}

func TestThemeCommandsAreInThePalette(t *testing.T) {
	a := newThemeApp(t, "", "")
	a.press("ctrl+shift+p")
	a.win.QuickInput.SetValue(">theme")
	a.pump()
	got := map[string]bool{}
	for _, r := range a.win.QuickInput.Picker().Rows() {
		got[r.Item.Label] = true
	}
	if !got["Preferences: Color Theme"] || !got["Preferences: File Icon Theme"] {
		t.Errorf("the palette lists %v", got)
	}
	// choosing it opens the picker in the place of the palette
	for i, r := range a.win.QuickInput.Picker().Rows() {
		if r.Item.Label == "Preferences: Color Theme" {
			a.win.QuickInput.Picker().SetActive(i)
		}
	}
	a.press("enter")
	if !a.win.QuickInput.Visible() || a.win.QuickInput.Picker().Placeholder() != "Select Color Theme (Up/Down Keys to Preview)" {
		t.Errorf("the picker is not shown: %q", a.win.QuickInput.Picker().Placeholder())
	}
	a.win.QuickInput.Hide()
	a.noErrors()
}

func TestColorThemePickerRevertsWhenTheWindowLosesTheFocus(t *testing.T) {
	a := newThemeApp(t, "", "")
	a.ts.SetViewFocus(ui.ImplicitViewID, true)
	a.pump()
	a.press("ctrl+k ctrl+t")
	a.press("up")
	a.press("up")
	if a.background() != lightBackground {
		t.Fatalf("no preview: %v", a.background())
	}
	a.ts.SetViewFocus(ui.ImplicitViewID, false)
	a.pump()
	if a.win.QuickInput.Visible() {
		t.Fatal("the picker stays open without the focus")
	}
	if a.win.Themes.Previewing() || a.background() != darkBackground {
		t.Errorf("previewing %v, background %v after the picker closed with the focus", a.win.Themes.Previewing(), a.background())
	}
	a.noErrors()
}
