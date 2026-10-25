package app

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// The tests of the system tray and of the close button (step 9b.4, SPEC §10.14): the tabs inside Glutter's SystemTray,
// on the fake desktop of gluttertest, which has a watcher for the tray and answers the file chooser.

// trayRun is a run of the program with its tabs inside the tray, on a fake desktop.
type trayRun struct {
	*multiApp
	fd   *gluttertest.FakeDesktop
	dirs []string
}

// runTray starts the program with one tab for each folder name (the first is the folder of the command line, the
// others open after it) and waits until the tray is registered. extra are settings.
func runTray(t *testing.T, extra map[string]any, names ...string) *trayRun {
	t.Helper()
	env := newMultiEnv(t, extra)
	r := &trayRun{}
	for _, name := range names {
		r.dirs = append(r.dirs, env.folder(name, "a.go"))
	}
	var fd *gluttertest.FakeDesktop
	r.multiApp, fd = env.runRoot(Options{Folder: r.dirs[0]}, ProcessOptions{}, true)
	r.fd = fd
	for _, dir := range r.dirs[1:] {
		if _, err := r.m.Open(WorkspaceSpec{Folder: dir}); err != nil {
			t.Fatal(err)
		}
	}
	r.settle()
	r.waitFor("the tray", fd.TrayRegistered)
	r.settle()
	return r
}

// closeButton presses the close button of the window the way the platform does, and reports whether the platform would
// close the window itself (it never does: the delegate of the window decides).
func (r *trayRun) closeButton() bool {
	r.t.Helper()
	closes := r.ts.RequestWindowClose(ui.ImplicitViewID)
	r.settle()
	return closes
}

func (r *trayRun) hidden() bool { return r.ts.WindowHidden(ui.ImplicitViewID) }

// menu is the labels of the menu of the tray.
func (r *trayRun) menu() []string { return r.fd.TrayMenu() }

// backups are the URIs of the backups that the workspace has on the disk.
func (r *trayRun) backups(w *Workspace) []string {
	r.t.Helper()
	store, err := storage.NewBackupStore(r.env.paths, w.stateID, storage.BackupStoreOptions{})
	if err != nil {
		r.t.Fatal(err)
	}
	list, problems := store.List(context.Background())
	if len(problems) != 0 {
		r.t.Fatalf("backup problems: %v", problems)
	}
	var out []string
	for _, b := range list {
		out = append(out, b.URI)
	}
	return out
}

func TestTheTrayRegistersWithTheTitleTheTooltipAndTheIcon(t *testing.T) {
	r := runTray(t, nil, "one", "two")
	fd := r.fd
	for name, want := range map[string]any{"Title": "Emetos", "Id": AppID, "Status": "Active", "Category": "ApplicationStatus"} {
		if got := fd.TrayProperty(name); got != want {
			t.Errorf("%s is %v, want %v", name, got, want)
		}
	}
	tip := reflect.ValueOf(fd.TrayProperty("ToolTip"))
	if title, body := tip.FieldByName("Title").String(), tip.FieldByName("Description").String(); title != "Emetos" || body != "2 workspaces" {
		t.Errorf("the tooltip is %q under %q", title, body)
	}
	// the pictures of the icon, at the sizes of a panel
	pix := reflect.ValueOf(fd.TrayProperty("IconPixmap"))
	var sizes []int
	for i := 0; i < pix.Len(); i++ {
		sizes = append(sizes, int(pix.Index(i).FieldByName("Width").Int()))
		data := pix.Index(i).FieldByName("Data").Bytes()
		if !slices.ContainsFunc(data, func(b byte) bool { return b != 0 }) {
			t.Errorf("the picture %d is empty", i)
		}
	}
	if !slices.Equal(sizes, trayIconSizes) {
		t.Errorf("the tray has pictures of %v, want %v", sizes, trayIconSizes)
	}
	// a click is the icon's action, so a click does not open the menu
	if fd.TrayProperty("ItemIsMenu") != false {
		t.Error("a click on the icon opens the menu")
	}
}

func TestTheTooltipSaysHowManyWorkspacesAreOpen(t *testing.T) {
	r := runTray(t, nil, "one")
	body := func() string {
		return reflect.ValueOf(r.fd.TrayProperty("ToolTip")).FieldByName("Description").String()
	}
	if got := body(); got != "1 workspace" {
		t.Errorf("one tab: %q", got)
	}
	if _, err := r.m.NewTab(); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := body(); got != "2 workspaces" {
		t.Errorf("two tabs: %q", got)
	}
}

func TestTheMenuOfTheTrayFollowsTheTabs(t *testing.T) {
	r := runTray(t, nil, "one", "two")
	tail := []string{"-", "Open Folder...", "New Workspace Tab", "-", "Hide Emetos", "Quit"}
	want := func(tabs ...string) []string { return append(slices.Clone(tabs), tail...) }
	check := func(what string, tabs ...string) {
		t.Helper()
		r.settle()
		if got := r.menu(); !slices.Equal(got, want(tabs...)) {
			t.Errorf("%s: the menu is %q, want %q", what, got, want(tabs...))
		}
	}
	check("two tabs", "one", "two")

	// a tab opens: after the active one
	three := r.env.folder("three", "a.go")
	if _, err := r.m.Open(WorkspaceSpec{Folder: three}); err != nil {
		t.Fatal(err)
	}
	check("a tab opened", "one", "two", "three")

	// a tab moves
	if !r.m.Move(r.m.Tabs()[2], 0) {
		t.Fatal("the tab did not move")
	}
	check("a tab moved", "three", "one", "two")

	// a tab is renamed: it opens another folder
	four := r.env.folder("four", "a.go")
	r.m.OpenFolder(r.m.Tabs()[1].ws, four, false, true, nil)
	r.waitFor("the folder to replace the tab", func() bool { return r.m.workspaceForFolder(four) != nil })
	check("a tab opened another folder", "three", "four", "two")

	// a tab closes
	closed := false
	r.m.Close(r.m.Tabs()[0], func(ok bool) { closed = ok })
	r.settle()
	if !closed {
		t.Fatal("the tab did not close")
	}
	check("a tab closed", "four", "two")
}

func TestTheMenuOfTheTrayTellsTabsOfTheSameNameApart(t *testing.T) {
	env := newMultiEnv(t, nil)
	a := env.folder("api", "a.go")
	b := env.folder("api", "a.go")
	r := &trayRun{dirs: []string{a, b}}
	var fd *gluttertest.FakeDesktop
	r.multiApp, fd = env.runRoot(Options{Folder: a}, ProcessOptions{}, true)
	r.fd = fd
	if _, err := r.m.Open(WorkspaceSpec{Folder: b}); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the tray", fd.TrayRegistered)
	r.settle()
	menu := r.menu()
	if !strings.HasPrefix(menu[0], "api — ") || !strings.HasPrefix(menu[1], "api — ") || menu[0] == menu[1] {
		t.Errorf("the tabs are %q and %q", menu[0], menu[1])
	}
}

func TestAClickOnTheIconHidesTheWindowThatIsInFrontAndShowsOneThatIsNot(t *testing.T) {
	r := runTray(t, nil, "one")
	fd := r.fd
	viewActivations := func() int {
		n := 0
		for _, a := range r.ts.ViewActions(ui.ImplicitViewID) {
			if a.Kind == ui.WindowActionActivate {
				n++
			}
		}
		return n
	}

	// in front: the click hides it
	fd.ActivateTray()
	r.settle()
	if !r.hidden() {
		t.Fatal("a click on the icon did not hide the window that is in front")
	}
	if got := r.menu(); got[len(got)-2] != "Show Emetos" {
		t.Errorf("the menu of a hidden window is %q", got)
	}

	// hidden: the click shows it and brings it to the front
	before := viewActivations()
	fd.ActivateTray()
	r.settle()
	if r.hidden() || viewActivations() != before+1 {
		t.Fatalf("the click did not show the window (hidden %v, activations %d, was %d)", r.hidden(), viewActivations(), before)
	}
	if got := r.menu(); got[len(got)-2] != "Hide Emetos" {
		t.Errorf("the menu of a shown window is %q", got)
	}

	// shown but not in front: the click brings it to the front and does not hide it
	r.ts.SetWindowInfo(ui.ImplicitViewID, ui.WindowInfo{Decoration: ui.DecorationApp, Capabilities: ui.AllWindowCapabilities, Title: "Emetos"})
	r.settle()
	before = viewActivations()
	fd.ActivateTray()
	r.settle()
	if r.hidden() || viewActivations() != before+1 {
		t.Errorf("the click on a window that is behind: hidden %v, activations %d, was %d", r.hidden(), viewActivations(), before)
	}
}

func TestTheHideAndShowItemOfTheMenuToggleTheWindow(t *testing.T) {
	r := runTray(t, nil, "one")
	r.fd.ClickTrayMenuItem("Hide Emetos")
	r.settle()
	if !r.hidden() {
		t.Fatal("Hide Emetos did not hide the window")
	}
	r.fd.ClickTrayMenuItem("Show Emetos")
	r.settle()
	if r.hidden() {
		t.Fatal("Show Emetos did not show the window")
	}
	if got := r.menu(); got[len(got)-2] != "Hide Emetos" {
		t.Errorf("the menu is %q", got)
	}
}

func TestChoosingATabInTheMenuShowsTheWindowWithThatTab(t *testing.T) {
	r := runTray(t, nil, "one", "two")
	r.m.ActivateIndex(0)
	r.settle()
	r.fd.ClickTrayMenuItem("Hide Emetos")
	r.settle()
	if !r.hidden() || r.m.Active().Folder != r.dirs[0] {
		t.Fatalf("hidden %v, active %q", r.hidden(), r.m.Active().Folder)
	}
	r.fd.ClickTrayMenuItem("two")
	r.settle()
	if r.hidden() || r.m.Active().Folder != r.dirs[1] {
		t.Errorf("hidden %v, active %q, want the window shown with %q", r.hidden(), r.m.Active().Folder, r.dirs[1])
	}
}

func TestTheOpenFolderItemShowsTheWindowAndOpensTheFolderAsATab(t *testing.T) {
	r := runTray(t, nil, "one")
	other := r.env.folder("other", "a.go")
	r.fd.ClickTrayMenuItem("Hide Emetos")
	r.settle()
	r.fd.ClickTrayMenuItem("Open Folder...")
	r.waitFor("the folder dialog", func() bool { return r.fd.PendingFileChoosers() == 1 })
	if r.hidden() {
		t.Error("the window stays hidden under the dialog")
	}
	r.fd.AnswerFileChooser(other)
	r.waitFor("the new tab", func() bool { return r.m.workspaceForFolder(other) != nil })
	r.settle()
	if r.m.Count() != 2 || r.m.Active().Folder != other {
		t.Errorf("%d tabs, active %q", r.m.Count(), r.m.Active().Folder)
	}
}

func TestTheNewWorkspaceTabItemOpensATabWithoutAFolderAndShowsTheWindow(t *testing.T) {
	r := runTray(t, nil, "one")
	r.fd.ClickTrayMenuItem("Hide Emetos")
	r.settle()
	r.fd.ClickTrayMenuItem("New Workspace Tab")
	r.settle()
	if r.hidden() || r.m.Count() != 2 || r.m.Active().Folder != "" {
		t.Errorf("hidden %v, %d tabs, active folder %q", r.hidden(), r.m.Count(), r.m.Active().Folder)
	}
	if got := r.menu(); !slices.Equal(got[:2], []string{"one", "No Folder"}) {
		t.Errorf("the menu is %q", got)
	}
}

func TestQuitInTheMenuQuits(t *testing.T) {
	r := runTray(t, nil, "one", "two")
	r.fd.ClickTrayMenuItem("Quit")
	r.settle()
	if r.quits != 1 {
		t.Errorf("%d quits", r.quits)
	}
}

func TestQuitFromTheTrayBacksUpWithHotExitWithoutShowingTheWindow(t *testing.T) {
	r := runTray(t, map[string]any{"files.hotExit": "onExit"}, "one", "two")
	w1, w2 := r.workspaceOf(r.dirs[0]), r.workspaceOf(r.dirs[1])
	r.edit(w1, filepath.Join(r.dirs[0], "a.go"))
	r.edit(w2, filepath.Join(r.dirs[1], "a.go"))
	r.fd.ClickTrayMenuItem("Hide Emetos")
	r.settle()
	r.fd.ClickTrayMenuItem("Quit")
	r.waitFor("the quit", func() bool { return r.quits == 1 })
	if !r.hidden() || r.dialog(w1) != "" || r.dialog(w2) != "" {
		t.Errorf("hidden %v, dialogs %q %q: hot exit asks nothing", r.hidden(), r.dialog(w1), r.dialog(w2))
	}
	if len(r.backups(w1)) != 1 || len(r.backups(w2)) != 1 {
		t.Errorf("backups %v and %v", r.backups(w1), r.backups(w2))
	}
}

func TestQuitFromTheTrayShowsTheWindowFirstToAskAboutUnsavedChanges(t *testing.T) {
	r := runTray(t, nil, "one", "two") // files.hotExit is off
	w1, w2 := r.workspaceOf(r.dirs[0]), r.workspaceOf(r.dirs[1])
	r.edit(w2, filepath.Join(r.dirs[1], "a.go"))
	r.m.ActivateIndex(0)
	r.settle()
	r.fd.ClickTrayMenuItem("Hide Emetos")
	r.settle()
	if !r.hidden() || r.m.Active() != w1 {
		t.Fatalf("hidden %v, active %q", r.hidden(), r.m.Active().Folder)
	}
	r.fd.ClickTrayMenuItem("Quit")
	r.settle()
	if r.hidden() {
		t.Fatal("the question is asked in a window nobody sees")
	}
	if got := r.dialog(w2); got != askAbout("a.go") || r.m.Active() != w2 {
		t.Fatalf("the tab with the changes asks %q (active %q)", got, r.m.Active().Folder)
	}
	if r.quits != 0 {
		t.Fatal("the application quit before the answer")
	}
	r.answer(w2, "Don't Save")
	r.waitFor("the quit", func() bool { return r.quits == 1 })
}

func TestTheCloseButtonHidesTheWindowToTheTray(t *testing.T) {
	r := runTray(t, nil, "one", "two")
	if r.closeButton() {
		t.Error("the platform would close the window itself")
	}
	if !r.hidden() || r.quits != 0 {
		t.Fatalf("hidden %v, %d quits", r.hidden(), r.quits)
	}
	if r.m.Count() != 2 {
		t.Errorf("%d tabs after the window was hidden", r.m.Count())
	}
	// a second press of a button that is not there does nothing, and the tray says how to bring the window back
	if got := r.menu(); got[len(got)-2] != "Show Emetos" {
		t.Errorf("the menu is %q", got)
	}
}

func TestTheCloseButtonWritesTheBackupsBeforeItHidesTheWindow(t *testing.T) {
	r := runTray(t, map[string]any{"files.hotExit": "onExit"}, "one", "two")
	w1, w2 := r.workspaceOf(r.dirs[0]), r.workspaceOf(r.dirs[1])
	r.edit(w1, filepath.Join(r.dirs[0], "a.go"))
	r.edit(w2, filepath.Join(r.dirs[1], "a.go"))
	// the timers of the backups use a clock that does not move: only the write of the close button can have written
	if len(r.backups(w1))+len(r.backups(w2)) != 0 {
		t.Fatal("a backup was written before the delay")
	}
	r.closeButton()
	r.waitFor("the window to hide", r.hidden)
	if len(r.backups(w1)) != 1 || len(r.backups(w2)) != 1 {
		t.Errorf("backups %v and %v", r.backups(w1), r.backups(w2))
	}
	if r.quits != 0 {
		t.Error("the window was hidden, and the application quit")
	}
}

func TestAHiddenWindowGetsNoFramesAndTheWorkspacesKeepRunning(t *testing.T) {
	r := runTray(t, nil, "one")
	w1 := r.m.Active()
	r.edit(w1, filepath.Join(r.dirs[0], "a.go"))
	r.closeButton()
	if !r.hidden() {
		t.Fatal("the window is not hidden")
	}
	sched := r.ts.Binding.Scheduler()
	if sched.FramesEnabled() {
		t.Error("frames are enabled for a hidden window")
	}
	scenes := len(r.ts.Scenes())
	// the workspaces work: a tab opens, a document changes, and the tray's state asks to be built again
	three := r.env.folder("three", "a.go")
	if _, err := r.m.Open(WorkspaceSpec{Folder: three}); err != nil {
		t.Fatal(err)
	}
	doc := w1.Documents.All()[0]
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "y"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := len(r.ts.Scenes()); got != scenes {
		t.Errorf("%d frames were drawn while the window was hidden", got-scenes)
	}
	if r.m.Count() != 2 || r.workspaceOf(three) == nil || !strings.HasPrefix(doc.Text(), "yx") {
		t.Errorf("%d tabs, the text is %q: the workspaces stopped", r.m.Count(), doc.Text())
	}
	// showing the window builds what changed
	r.fd.ClickTrayMenuItem("Show Emetos")
	r.settle()
	if len(r.ts.Scenes()) == scenes || !sched.FramesEnabled() {
		t.Errorf("frames after the window was shown: %d, enabled %v", len(r.ts.Scenes())-scenes, sched.FramesEnabled())
	}
	if got := r.menu(); !slices.Equal(got[:2], []string{"one", "three"}) {
		t.Errorf("the menu is %q", got)
	}
}

func TestAShowThatComesBeforeTheWindowIsHiddenCancelsTheHide(t *testing.T) {
	r := runTray(t, nil, "one")
	c, _ := r.m.implicitController()
	r.m.hideWindow(c)
	if !r.m.tray.hidePending {
		t.Fatal("the hide did not start")
	}
	r.m.showWindow(nil)
	r.settle()
	if r.hidden() || r.m.tray.hiding || r.m.tray.hidePending {
		t.Errorf("hidden %v, hiding %v, pending %v", r.hidden(), r.m.tray.hiding, r.m.tray.hidePending)
	}
	if got := r.menu(); got[len(got)-2] != "Hide Emetos" {
		t.Errorf("the menu is %q", got)
	}
}

func TestTheCloseButtonQuitsWithTheSettingOff(t *testing.T) {
	r := runTray(t, map[string]any{"window.closeToTray": false}, "one")
	r.closeButton()
	if r.hidden() || r.quits != 1 {
		t.Errorf("hidden %v, %d quits", r.hidden(), r.quits)
	}
}

func TestTheCloseButtonQuitsWithTheQuestionsWhenTheSettingIsOff(t *testing.T) {
	r := runTray(t, map[string]any{"window.closeToTray": false}, "one")
	w := r.workspaceOf(r.dirs[0])
	r.edit(w, filepath.Join(r.dirs[0], "a.go"))
	r.closeButton()
	if got := r.dialog(w); got != askAbout("a.go") || r.hidden() || r.quits != 0 {
		t.Fatalf("dialog %q, hidden %v, %d quits", got, r.hidden(), r.quits)
	}
	r.answer(w, "Don't Save")
	r.waitFor("the quit", func() bool { return r.quits == 1 })
}

func TestTheSettingIsReadWhenTheButtonIsPressed(t *testing.T) {
	r := runTray(t, nil, "one")
	w := r.m.Active()
	if err := w.Settings.Update(r.ctx, "window.closeToTray", false, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	r.closeButton()
	if r.hidden() || r.quits != 1 {
		t.Errorf("hidden %v, %d quits after the setting was turned off", r.hidden(), r.quits)
	}
}

func TestTheCloseButtonQuitsWhenNoTrayIsShown(t *testing.T) {
	env := newMultiEnv(t, nil)
	env.realBus = true
	// there is no session bus: the tray cannot be shown (and the test never reaches the bus of the machine)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	dir := env.folder("one", "a.go")
	a, _ := env.runRoot(Options{Folder: dir}, ProcessOptions{Logger: logger}, true)
	a.waitFor("the report", func() bool { return len(a.ts.Errors()) > 0 })
	var others []error
	report := a.m.errorReporter(func(err error) { others = append(others, err) })
	for _, err := range a.ts.TakeErrors() {
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "TrayUnavailable" {
			t.Fatalf("Glutter reported %v, want TrayUnavailable", err)
		}
		report(err)
		report(err) // told twice, noted once
	}
	if len(others) != 0 {
		t.Errorf("the reporter passed %v on", others)
	}
	if a.m.trayShown() {
		t.Fatal("the tray is shown")
	}
	if n := strings.Count(logs.String(), "no system tray is shown"); n != 1 {
		t.Errorf("the log says it %d times:\n%s", n, logs.String())
	}
	if items := a.m.Active().Notify.Items(); len(items) != 0 {
		t.Errorf("the user was told: %v", items)
	}
	closes := a.ts.RequestWindowClose(ui.ImplicitViewID)
	a.settle()
	if closes || a.ts.WindowHidden(ui.ImplicitViewID) || a.quits != 1 {
		t.Errorf("closes %v, hidden %v, %d quits: the close button must quit", closes, a.ts.WindowHidden(ui.ImplicitViewID), a.quits)
	}
}

func TestTheOtherErrorsOfTheFrameworkAreNotTakenByTheTray(t *testing.T) {
	r := runTray(t, nil, "one")
	var got []error
	report := r.m.errorReporter(func(err error) { got = append(got, err) })
	other := errors.New("SomethingElse", "not the tray")
	report(other)
	if len(got) != 1 || got[0] != other || !r.m.trayShown() {
		t.Errorf("passed on %v, tray shown %v", got, r.m.trayShown())
	}
	for _, tag := range []string{"TrayUnavailable", "TrayRegisterFailed", "TrayDuplicate", "TrayIconInvalid"} {
		if !trayTags[tag] {
			t.Errorf("%s does not say the tray is gone", tag)
		}
	}
}

func TestATrayThatFailsAfterTheWindowWasHiddenShowsTheWindowAgain(t *testing.T) {
	r := runTray(t, nil, "one")
	r.closeButton()
	if !r.hidden() {
		t.Fatal("the window is not hidden")
	}
	report := r.m.errorReporter(func(error) {})
	report(errors.New("TrayRegisterFailed", "the watcher went away"))
	r.settle()
	if r.hidden() || r.m.trayShown() {
		t.Errorf("hidden %v, tray shown %v: the user has no way to the window", r.hidden(), r.m.trayShown())
	}
	// and from now on the close button quits
	r.closeButton()
	if r.quits != 1 {
		t.Errorf("%d quits", r.quits)
	}
}

func TestTheCloseButtonDuringTheQuestionsOfAQuitDoesNothing(t *testing.T) {
	r := runTray(t, nil, "one")
	w := r.m.Active()
	r.edit(w, filepath.Join(r.dirs[0], "a.go"))
	r.m.RequestQuit(w)
	r.settle()
	if r.dialog(w) == "" {
		t.Fatal("no question was asked")
	}
	r.closeButton()
	if r.hidden() || r.dialog(w) == "" {
		t.Errorf("hidden %v, dialog %q", r.hidden(), r.dialog(w))
	}
	r.answer(w, "Cancel")
	if r.quits != 0 {
		t.Errorf("%d quits", r.quits)
	}
}
