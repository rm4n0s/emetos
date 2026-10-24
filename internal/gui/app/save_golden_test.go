package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of saving: the notification of a save that failed, the question before a file is replaced, the prompt for a
// path, the File menu with its save rows and the context menu of a tab that has unsaved changes.

func goldenSave(t *testing.T, theme string) *saveApp {
	t.Helper()
	win := settingsWindow(t, map[string]any{"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false})
	texts := map[string]string{
		"main.go": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n",
		"util.go": "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n",
	}
	names := []string{"main.go", "util.go"}
	g := newGroupAppWith(t, win, ui.Size{Width: 900, Height: 460}, names, texts, "main.go", "util.go")
	g.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a := &saveApp{groupApp: g, fd: g.ts.FakeDesktop()}
	a.clickTab("main.go")
	a.typeText("// edited\n")
	return a
}

func TestGoldenSaving(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" failed save", func(t *testing.T) {
			a := goldenSave(t, th.theme)
			if err := os.WriteFile(a.files["main.go"], []byte("package main // changed by another program\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			a.press("ctrl+s")
			a.waitFor("the notification", func() bool { return len(a.notes()) > 0 })
			a.ts.Pump(400 * time.Millisecond)
			a.ts.MatchesGolden("save_error_" + th.name)
			a.noErrors()
		})
		t.Run(th.name+" replace question", func(t *testing.T) {
			a := goldenSave(t, th.theme)
			target := filepath.Join(t.TempDir(), "project", "notes.go")
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("package notes\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			a.run("workbench.action.files.saveAs")
			a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
			a.fd.AnswerFileChooser(target)
			a.waitFor("the question", func() bool { return a.ts.Exists(gluttertest.ByText("Replace")) })
			a.ts.Pump(300 * time.Millisecond)
			a.ts.MatchesGolden("save_replace_dialog_" + th.name)
			a.noErrors()
		})
		t.Run(th.name+" file menu", func(t *testing.T) {
			a := goldenSave(t, th.theme)
			s := a.shell()
			s.menuBar.Open(s.menus.BarIndex(command.MenubarFileMenu))
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			a.ts.MatchesGolden("save_file_menu_" + th.name)
			a.noErrors()
		})
		t.Run(th.name+" tab menu of a dirty tab", func(t *testing.T) {
			a := goldenSave(t, th.theme)
			a.rightClickTab(1, "main.go")
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			a.ts.MatchesGolden("save_tab_menu_" + th.name)
			a.noErrors()
		})
		t.Run(th.name+" path prompt", func(t *testing.T) {
			a := goldenSave(t, th.theme)
			a.win.SaveDialog = unavailable
			a.run("workbench.action.files.saveAs")
			a.waitFor("the prompt", func() bool { return a.win.QuickInput.Prompting() })
			a.win.QuickInput.SetValue("/no/such/folder/new.go")
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			a.ts.MatchesGolden("save_path_prompt_" + th.name)
			a.noErrors()
		})
	}
}
