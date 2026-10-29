package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	extui "github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The views of extensions in the real workbench (step 11.5): the icon in the activity bar, the view in the side bar, the tab
// of the panel with its badge and actions, the state the views keep, and what an extension puts in the Output and Problems
// panels.

// vxState is what the test extension "vx" keeps; a test reads it.
type vxState struct {
	mu          sync.Mutex
	api         extension.API
	side, panel extension.ViewHandle
	// notes are the "state" of each view: what SaveState gives and RestoreState takes back.
	notes      map[string]string
	builds     map[string]int
	saved      []string
	restored   []string
	maxW, maxH float64
	files      []extension.FilesChanged
	activated  int
}

// vxFactory is the extension "vx": a container with one view in the side bar, a tab in the panel, title actions, a command,
// and an Output channel and markers it makes when it is activated.
func vxFactory(st *vxState) extension.Factory {
	return func() extension.Extension { return &vxExt{st: st} }
}

type vxExt struct{ st *vxState }

func (e *vxExt) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: "vx", Name: "Vx", Version: "1.0.0",
		Contributes: extension.Contributes{
			Commands:       []extension.CommandSpec{{ID: "vx.refresh", Title: "Refresh Vx", Category: "Vx"}, {ID: "vx.stop", Title: "Stop Vx", Category: "Vx"}},
			ViewContainers: []extension.ViewContainerSpec{{ID: "vx.box", Title: "Vx Box", Icon: "checklist", Order: 50}},
			Views:          []extension.ViewSpec{{ID: "vx.list", Name: "Items", Container: "vx.box"}},
			PanelViews:     []extension.PanelViewSpec{{ID: "vx.tab", Title: "Vx Tab", Icon: "terminal"}},
			ViewActions: []extension.ViewActionSpec{
				{View: "vx.list", Command: "vx.refresh", Icon: "refresh", Tooltip: "Refresh Items"},
				{View: "vx.tab", Command: "vx.refresh", Icon: "refresh", Tooltip: "Refresh Tab", Order: 1},
				{View: "vx.tab", Command: "vx.stop", Icon: "debug-stop", Tooltip: "Stop Tab", When: "vx.running", Order: 2},
			},
		},
	}
}

func (e *vxExt) Activate(_ context.Context, api extension.API) error {
	st := e.st
	st.mu.Lock()
	st.api = api
	st.activated++
	if st.notes == nil {
		st.notes = map[string]string{}
		st.builds = map[string]int{}
	}
	st.mu.Unlock()
	content := func(id string) extension.ViewContent {
		return extension.ViewContent{
			Build: func(bc widgets.BuildContext) widgets.Widget {
				st.mu.Lock()
				st.builds[id]++
				text := id + " shows " + st.notes[id]
				st.mu.Unlock()
				return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
					st.mu.Lock()
					st.maxW, st.maxH = c.MaxWidth, c.MaxHeight
					st.mu.Unlock()
					return widgets.NewText(text, widgets.TextOpts{Style: extui.TextStyle(bc, 13, extui.Theme(bc).Foreground)})
				})
			},
			SaveState: func() (any, error) {
				st.mu.Lock()
				defer st.mu.Unlock()
				st.saved = append(st.saved, id)
				return map[string]string{"note": st.notes[id]}, nil
			},
			RestoreState: func(raw json.RawMessage) error {
				var v map[string]string
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				st.mu.Lock()
				defer st.mu.Unlock()
				st.notes[id] = v["note"]
				st.restored = append(st.restored, id+"="+v["note"])
				return nil
			},
		}
	}
	var err error
	if st.side, err = api.Views().Provide("vx.list", content("vx.list")); err != nil {
		return err
	}
	if st.panel, err = api.Views().Provide("vx.tab", content("vx.tab")); err != nil {
		return err
	}
	for _, id := range []string{"vx.refresh", "vx.stop"} {
		if err := api.Commands().Handle(id, func(context.Context, any) error { return nil }); err != nil {
			return err
		}
	}
	ch, err := api.Output().Channel("Vx Log")
	if err != nil {
		return err
	}
	ch.AppendLine("vx started")
	if err := api.Problems().Set("lint", "file:///work/vx/a.go", []extension.Marker{{Message: "vx says no", StartLine: 1, StartColumn: 1}}); err != nil {
		return err
	}
	_, err = api.Events().Subscribe(extension.TopicFilesChanged, func(e extension.Event) {
		v, err := extension.EventValue[extension.FilesChanged](e)
		if err == nil {
			st.mu.Lock()
			st.files = append(st.files, v)
			st.mu.Unlock()
		}
	})
	return err
}

// setNote changes what a view shows and has it build again.
func (st *vxState) setNote(id, note string) {
	st.mu.Lock()
	st.notes[id] = note
	h := st.side
	if id == "vx.tab" {
		h = st.panel
	}
	st.mu.Unlock()
	h.Refresh()
}

// vxApp is a window over a folder with the extension vx.
type vxApp struct {
	*groupApp
	st *vxState
}

// newVxApp shows the workbench of a workspace with the extension.
func newVxApp(t *testing.T, folder string) *vxApp {
	t.Helper()
	st := &vxState{}
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"files.hotExit": "off", "window.menuBarVisibility": "classic", "editor.minimap.enabled": false})
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{vxFactory(st)}})
	win, err := NewWorkspace(context.Background(), p, folder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(win.release)
	a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 560}, nil, nil)
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.pump()
	return &vxApp{groupApp: a, st: st}
}

func (a *vxApp) do(id string) {
	a.t.Helper()
	if err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, nil); err != nil {
		a.t.Fatalf("%s: %v", id, err)
	}
	a.pump()
}

func (a *vxApp) text(s string) bool { return a.ts.Exists(gluttertest.ByText(s)) }

func TestTheActivityBarHasTheIconOfAnExtensionBeforeItRuns(t *testing.T) {
	a := newVxApp(t, "")
	icon := gluttertest.ByKey(workbench.ActivityItemKey("vx.box"))
	if !a.ts.Exists(icon) {
		t.Fatal("the icon of the container is not in the activity bar")
	}
	if a.st.activated != 0 {
		t.Errorf("the extension was activated %d times before its view was shown", a.st.activated)
	}
	explorer := a.ts.TopLeft(gluttertest.ByKey(workbench.ActivityItemKey(command.ViewContainerExplorer)))
	vx := a.ts.TopLeft(icon)
	if vx.Dy <= explorer.Dy {
		t.Errorf("the icon of the extension (y %v) is not below the Explorer's (y %v)", vx.Dy, explorer.Dy)
	}
}

func TestAnIconOpensTheContainerAndTheViewShowsTheContentInItsBox(t *testing.T) {
	a := newVxApp(t, "")
	a.ts.Tap(gluttertest.ByKey(workbench.ActivityItemKey("vx.box")))
	a.pump()
	a.pump()
	if a.win.Views.Active() != "vx.box" {
		t.Fatalf("the active container is %q", a.win.Views.Active())
	}
	if !a.text("VX BOX") || !a.text("ITEMS") {
		t.Error("the title of the side bar or the header of the view is missing")
	}
	if !a.text("vx.list shows ") {
		t.Fatal("the content of the view is not shown")
	}
	if a.st.activated != 1 {
		t.Errorf("activated %d times", a.st.activated)
	}
	// the box: the content got the constraints of the body of the view, which is inside the side bar
	side := a.ts.Size(gluttertest.ByKey(workbench.PartKey(workbench.PartSideBar)))
	if a.st.maxW <= 0 || a.st.maxW > side.Width || a.st.maxH <= 0 || a.st.maxH > side.Height {
		t.Errorf("the content got %v by %v in a side bar of %v", a.st.maxW, a.st.maxH, side)
	}
	if n := len(a.ts.Elements(gluttertest.Descendant(gluttertest.ByKey(workbench.PartKey(workbench.PartSideBar)), gluttertest.ByType[*widgets.ClipRect]()))); n == 0 {
		t.Error("the content is not clipped to its box")
	}
	// the title action of the view is a button of its header while the pointer is over the view
	a.ts.Hover(a.ts.Center(gluttertest.ByText("vx.list shows ")))
	a.pump()
	if !a.ts.Exists(gluttertest.Descendant(gluttertest.ByKey(workbench.ViewHeaderKey("vx.list")), gluttertest.ByType[*kit.IconButton]())) {
		t.Error("the header of the view has no title action")
	}
	// the key of the focused view is set while the focus is in the box, and the text follows a refresh
	a.st.setNote("vx.list", "new text")
	a.pump()
	if !a.text("vx.list shows new text") {
		t.Error("Refresh did not show the new content")
	}
}

func TestTheStateOfASideBarViewIsSavedWhenAnotherContainerIsShownAndRestoredAfterwards(t *testing.T) {
	a := newVxApp(t, "")
	a.do("workbench.view.extension.vx.box")
	a.pump()
	a.st.setNote("vx.list", "scrolled to 40")
	a.pump()
	a.do("workbench.view.explorer")
	if !slices.Contains(a.st.saved, "vx.list") {
		t.Fatalf("saved = %v after the container went", a.st.saved)
	}
	// the extension forgets (a new box starts from the state it saved)
	a.st.mu.Lock()
	a.st.notes["vx.list"] = ""
	a.st.mu.Unlock()
	a.do("workbench.view.extension.vx.box")
	a.pump()
	if !slices.Contains(a.st.restored, "vx.list=scrolled to 40") || !a.text("vx.list shows scrolled to 40") {
		t.Errorf("restored = %v; the view shows its old state: %v", a.st.restored, a.text("vx.list shows scrolled to 40"))
	}
}

func TestAPanelViewIsATabWithABadgeAndActions(t *testing.T) {
	a := newVxApp(t, "")
	a.do("workbench.actions.view.toggleProblems")
	tab := gluttertest.ByText("VX TAB")
	if !a.ts.Exists(tab) {
		t.Fatal("the panel has no tab for the view")
	}
	if a.ts.Exists(gluttertest.ByKey(workbench.PanelBadgeKey("vx.tab"))) {
		t.Error("the tab has a badge before the extension set one")
	}
	a.ts.Tap(tab)
	a.pump()
	a.pump()
	if a.win.Views.ActivePanel() != "vx.tab" || !a.text("vx.tab shows ") {
		t.Fatalf("the tab is not open: %q", a.win.Views.ActivePanel())
	}
	// the badge
	a.st.panel.SetBadge(3, "3 things")
	a.pump()
	if !a.ts.Exists(gluttertest.ByKey(workbench.PanelBadgeKey("vx.tab"))) || !a.text("3") {
		t.Error("the tab has no badge")
	}
	// the title actions: Refresh always, Stop only while vx.running holds
	actions := func() int {
		return len(a.ts.Elements(gluttertest.ByPredicate("a button of the panel title", func(w widgets.Widget) bool {
			b, ok := w.(*kit.IconButton)
			return ok && b != nil
		})))
	}
	before := actions()
	if err := a.st.api.Context().Set("vx.running", true); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if actions() != before+1 {
		t.Errorf("the title strip has %d buttons, was %d: Stop did not appear with vx.running", actions(), before)
	}
	// the Toggle command and the View menu entry
	if _, ok := a.win.Commands.Lookup("workbench.panel.extension.vx.tab.toggle"); !ok {
		t.Error("no toggle command")
	}
	a.ts.Tap(gluttertest.ByText("View"))
	a.pump()
	if !a.text("Vx Tab") {
		t.Error("the View menu has no entry for the panel view")
	}
}

func TestAPanelViewKeepsItsContentWhenTheTabChangesAndWhenThePanelMovesAndMaximizes(t *testing.T) {
	a := newVxApp(t, "")
	a.do("workbench.panel.extension.vx.tab.toggle")
	if a.win.Views.ActivePanel() != "vx.tab" || !a.win.Layout.State().PanelVisible {
		t.Fatalf("the toggle command did not show the tab: %q", a.win.Views.ActivePanel())
	}
	a.st.setNote("vx.tab", "line 12")
	a.pump()
	// another tab and back: the extension's own state is in its Go value, the widget state in what it saved
	a.do("workbench.actions.view.problems")
	a.do("workbench.action.output.toggleOutput")
	if a.win.Views.ActivePanel() != command.ViewContainerOutput {
		t.Fatalf("the Output tab is not shown: %q", a.win.Views.ActivePanel())
	}
	if !slices.Contains(a.st.saved, "vx.tab") {
		t.Errorf("the state was not saved when the tab went: %v", a.st.saved)
	}
	a.win.Views.OpenPanel("vx.tab")
	a.pump()
	if !a.text("vx.tab shows line 12") {
		t.Error("the content is not back")
	}
	// the panel moves with the position and maximizes with the editor area gone; the content is built at the new size
	for _, pos := range []string{"workbench.action.positionPanelRight", "workbench.action.positionPanelLeft", "workbench.action.positionPanelTop", "workbench.action.positionPanelBottom"} {
		a.do(pos)
		if !a.text("vx.tab shows line 12") {
			t.Errorf("the content is gone after %s", pos)
		}
	}
	a.do("workbench.action.positionPanelRight")
	narrow := a.st.maxW
	a.do("workbench.action.positionPanelBottom")
	if wide := a.st.maxW; wide <= narrow {
		t.Errorf("the content has %v px at the bottom and %v at the side", wide, narrow)
	}
	height := a.st.maxH
	a.do("workbench.action.toggleMaximizedPanel")
	if !a.text("vx.tab shows line 12") || a.st.maxH <= height {
		t.Errorf("maximized: the content has %v px of height, was %v", a.st.maxH, height)
	}
	a.do("workbench.action.toggleMaximizedPanel")
	// the panel's own tabs still work
	a.do("workbench.actions.view.problems")
	if a.win.Views.ActivePanel() != command.ViewContainerMarkers {
		t.Errorf("the Problems tab did not open: %q", a.win.Views.ActivePanel())
	}
}

func TestAnExtensionsOutputAndProblemsAppearAndGoWhenItIsTurnedOff(t *testing.T) {
	a := newVxApp(t, "")
	a.do("workbench.panel.extension.vx.tab.toggle")
	if !slices.Contains(a.win.Process.Channels.Names(), "Vx Log") || a.win.Process.Channels.Lines("Vx Log")[0] != "vx started" {
		t.Fatalf("channels = %v", a.win.Process.Channels.Names())
	}
	if !slices.Contains(a.win.Output.Channels(), "Vx Log") {
		t.Errorf("the Output panel lists %v", a.win.Output.Channels())
	}
	if got := a.win.Markers.Read(problems.ReadFilter{}); len(got) != 1 || got[0].Message != "vx says no" || got[0].Source != "Vx" || got[0].Resource != "/work/vx/a.go" {
		t.Errorf("markers = %+v", got)
	}
	// the Output panel shows the channel when the extension says so
	a.win.Output.SetActive("Vx Log")
	a.do("workbench.action.output.toggleOutput")
	if a.win.Output.Active() != "Vx Log" {
		t.Fatalf("active channel %q", a.win.Output.Active())
	}
	if err := a.win.setExtensionEnabled("vx", false); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.pump()
	if slices.Contains(a.win.Process.Channels.Names(), "Vx Log") || slices.Contains(a.win.Output.Channels(), "Vx Log") {
		t.Errorf("the channel stayed: %v", a.win.Process.Channels.Names())
	}
	if a.win.Output.Active() == "Vx Log" {
		t.Error("the Output panel still shows the channel that went")
	}
	if n := a.win.Markers.Stats().Total(); n != 0 {
		t.Errorf("%d markers stayed", n)
	}
	if _, ok := a.win.Views.Registry().Container("vx.tab"); ok || a.text("VX TAB") {
		t.Error("the tab of the extension stayed in the panel")
	}
	if a.ts.Exists(gluttertest.ByKey(workbench.ActivityItemKey("vx.box"))) {
		t.Error("the icon of the extension stayed in the activity bar")
	}
	if a.win.Views.ActivePanel() == "vx.tab" {
		t.Error("the panel still shows the tab that went")
	}
	// and it all comes back
	if err := a.win.setExtensionEnabled("vx", true); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if !a.ts.Exists(gluttertest.ByKey(workbench.ActivityItemKey("vx.box"))) {
		t.Error("the icon did not come back")
	}
}

func TestAViewOfAnExtensionThatFailsShowsItsNameAndShowLog(t *testing.T) {
	a := newVxApp(t, "")
	a.do("workbench.view.extension.vx.box")
	a.pump()
	if !a.text("vx.list shows ") {
		t.Fatal("the content is not shown")
	}
	// the extension panics in a callback
	a.st.mu.Lock()
	api := a.st.api
	a.st.mu.Unlock()
	api.Post(func() { panic("boom") })
	a.pump()
	a.pump()
	if !a.text("Vx failed") || !a.ts.Exists(gluttertest.ByText("Show Log")) {
		t.Error("the box does not say that the extension failed")
	}
	if scope := a.win.Extensions(); scope.State("vx") != exthost.StateFailed {
		t.Errorf("state = %v", scope.State("vx"))
	}
	// the editor around it is untouched
	if !a.ts.Exists(gluttertest.ByKey(workbench.PartKey(workbench.PartEditor))) {
		t.Error("the editor area is gone")
	}
}

func TestTheStateOfTheViewsComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	st := &vxState{}
	first := env.startWith(env.root, ProcessOptions{Extensions: []extension.Factory{vxFactory(st)}})
	first.command("workbench.view.extension.vx.box", nil)
	first.command("workbench.panel.extension.vx.tab.toggle", nil)
	st.setNote("vx.list", "list state")
	st.setNote("vx.tab", "tab state")
	first.pump()
	first.close()
	saved, ok := env.savedWindow(env.root)
	if !ok {
		t.Fatal("no state was written")
	}
	if string(saved.Views["vx.list"]) != `{"note":"list state"}` || string(saved.Views["vx.tab"]) != `{"note":"tab state"}` {
		t.Fatalf("saved views = %v", saved.Views)
	}
	// the next run: a new extension instance gets the state when its views are shown
	st2 := &vxState{}
	second := env.startWith(env.root, ProcessOptions{Extensions: []extension.Factory{vxFactory(st2)}})
	// the tab that is open is not part of the saved layout: the panel shows Problems until the tab is chosen
	second.command("workbench.panel.extension.vx.tab.toggle", nil)
	second.pump()
	if !second.ts.Exists(gluttertest.ByText("vx.tab shows tab state")) {
		t.Errorf("the panel view did not get its state back; restored = %v", st2.restored)
	}
	second.command("workbench.view.extension.vx.box", nil)
	second.pump()
	if !second.ts.Exists(gluttertest.ByText("vx.list shows list state")) {
		t.Errorf("the side bar view did not get its state back; restored = %v", st2.restored)
	}
	second.close()
}

func TestTheWatcherEventsReachTheExtensionsAsFileChanges(t *testing.T) {
	folder := t.TempDir()
	a := newVxApp(t, folder)
	a.do("workbench.view.extension.vx.box") // activates the extension
	a.pump()
	a.win.onFileEvents(context.Background(), []workspace.Event{
		{Kind: workspace.EventChanged, Path: filepath.Join(folder, "a.go")},
		{Kind: workspace.EventCreated, Path: filepath.Join(folder, "sub", "b.go")},
		{Kind: workspace.EventRenamed, Path: filepath.Join(folder, "c.go"), OldPath: filepath.Join(folder, "old.go")},
		{Kind: workspace.EventDeleted, Path: filepath.Join(folder, "gone")},
		{Kind: workspace.EventRescan, Path: folder},
		{Kind: workspace.EventChanged, Path: "/elsewhere/x.go"},
	})
	a.pump()
	a.pump()
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	var got []extension.FileChange
	for _, f := range a.st.files {
		got = append(got, f.Changes...)
	}
	want := []extension.FileChange{
		{Kind: extension.FileChanged, Path: "a.go"}, {Kind: extension.FileCreated, Path: "sub/b.go"},
		{Kind: extension.FileRenamed, Path: "c.go", OldPath: "old.go"}, {Kind: extension.FileDeleted, Path: "gone"},
		{Kind: extension.FileRescan, Path: "."},
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("the change %+v did not reach the extension; got %+v", w, got)
		}
	}
	for _, c := range got {
		if strings.Contains(c.Path, "elsewhere") {
			t.Errorf("a path outside the folder reached the extension: %+v", c)
		}
	}
}

func TestAHugeBatchOfFileEventsIsOneRescan(t *testing.T) {
	folder := t.TempDir()
	a := newVxApp(t, folder)
	a.do("workbench.view.extension.vx.box")
	var events []workspace.Event
	for i := range extension.MaxFileChanges + 1 {
		events = append(events, workspace.Event{Kind: workspace.EventChanged, Path: filepath.Join(folder, "f"+strconv.Itoa(i))})
	}
	a.win.onFileEvents(context.Background(), events)
	a.pump()
	a.pump()
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	if len(a.st.files) != 1 || len(a.st.files[0].Changes) != 1 || a.st.files[0].Changes[0] != (extension.FileChange{Kind: extension.FileRescan, Path: "."}) {
		t.Errorf("the extension got %+v", a.st.files)
	}
}

func TestAFileChangeIsMadeOfAWatcherEvent(t *testing.T) {
	folder := filepath.Join(os.TempDir(), "proj")
	tests := []struct {
		name string
		e    workspace.Event
		want extension.FileChange
		ok   bool
	}{
		{"a change", workspace.Event{Kind: workspace.EventChanged, Path: filepath.Join(folder, "a", "b.go")}, extension.FileChange{Kind: extension.FileChanged, Path: "a/b.go"}, true},
		{"the folder itself", workspace.Event{Kind: workspace.EventRescan, Path: folder}, extension.FileChange{Kind: extension.FileRescan, Path: "."}, true},
		{"a rename", workspace.Event{Kind: workspace.EventRenamed, Path: filepath.Join(folder, "n"), OldPath: filepath.Join(folder, "o")}, extension.FileChange{Kind: extension.FileRenamed, Path: "n", OldPath: "o"}, true},
		{"a rename out of the folder keeps the new place", workspace.Event{Kind: workspace.EventRenamed, Path: filepath.Join(folder, "n"), OldPath: "/x/o"}, extension.FileChange{Kind: extension.FileRenamed, Path: "n"}, true},
		{"outside the folder", workspace.Event{Kind: workspace.EventChanged, Path: "/x/y"}, extension.FileChange{}, false},
		{"a sibling with the same prefix", workspace.Event{Kind: workspace.EventChanged, Path: folder + "2/x"}, extension.FileChange{}, false},
		{"no path", workspace.Event{Kind: workspace.EventChanged}, extension.FileChange{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := fileChange(folder, tt.e)
			if ok != tt.ok || got != tt.want {
				t.Errorf("fileChange = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestEachWorkspaceHasItsOwnViews(t *testing.T) {
	st := &vxState{}
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"files.hotExit": "off"})
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{vxFactory(st)}})
	a, err := NewWorkspace(context.Background(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.release)
	b, err := NewWorkspace(context.Background(), p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.release)
	if _, ok := a.Views.Registry().Container("vx.box"); !ok {
		t.Error("the first workspace has no container")
	}
	if err := a.setExtensionEnabled("vx", false); err != nil {
		t.Fatal(err)
	}
	// turning it off is a setting of the profile, which every workspace follows; the registry of the process is untouched
	if _, ok := p.Views.Container("vx.box"); ok {
		t.Error("the views of an extension are in the registry of the process")
	}
	_ = settings.TargetUser
	_ = foundation.Some[int]
}

func TestAnExtensionShowsADocumentAtAPlace(t *testing.T) {
	a := newVxApp(t, "")
	dir := t.TempDir()
	path := filepath.Join(dir, "shown.go")
	if err := os.WriteFile(path, []byte("package shown\n\nfunc A() {}\n// héllo\U0001F600 x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs := extensionDocuments{w: a.win}
	if err := docs.Show("file://"+path, 4, 11, false); err != nil {
		t.Fatal(err)
	}
	for range 40 { // the file is read in the background
		a.pump()
		if e := a.shell().activeEditor(); e != nil {
			break
		}
	}
	e := a.shell().activeEditor()
	if e == nil || e.doc.URI().Path != path {
		t.Fatalf("the active editor does not show %s", path)
	}
	// column 11 counts UTF-16 units: "// héllo" and the emoji (two units) are before it, 13 bytes of the line
	sel := e.view.Cursors().Selections()
	if len(sel) != 1 || sel[0].Active != (buffer.Position{Line: 3, Col: 13}) {
		t.Errorf("selection = %+v, want line 4 and byte 13", sel)
	}
}

func TestShowingADocumentFailsWhenItCannotBeShown(t *testing.T) {
	a := newVxApp(t, "")
	docs := extensionDocuments{w: a.win}
	tests := []struct {
		name  string
		docs  extensionDocuments
		uri   string
		route string
	}{
		{"a document that is not a file", docs, "untitled:1", "extensionDocuments.Show.ExtDocumentShowUnsupported"},
		{"a URI that is not one", docs, "nothing", "ParseURI.URIInvalid"},
		{"a window tab that is not on screen", extensionDocuments{w: newTestWorkspace(t, "")}, "file:///a.go", "extensionDocuments.Show.ExtDocumentShowUnavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.docs.Show(tt.uri, 1, 1, true)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("got %v, want the route %s", err, tt.route)
			}
		})
	}
}

func TestAnExtensionOpensAnUntitledDocumentWithATextOfItsOwn(t *testing.T) {
	a := newVxApp(t, "")
	docs := extensionDocuments{w: a.win}
	tests := []struct {
		name, language, text, wantLanguage string
	}{
		{"a statement", "sql", "SELECT 1;\n", "sql"},
		{"no text", "sql", "", "sql"},
		{"a language that does not exist", "no-such-language", "x\n", "plaintext"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri, err := docs.OpenUntitled(tt.language, tt.text)
			if err != nil {
				t.Fatal(err)
			}
			var e *textEditor
			for range 40 { // the editor is made on the next frame
				a.pump()
				if e = a.shell().activeEditor(); e != nil && e.doc.URI().String() == uri {
					break
				}
			}
			if e == nil || e.doc.URI().String() != uri {
				t.Fatalf("the active editor is not the new document %s", uri)
			}
			if e.doc.Text() != tt.text || e.doc.LanguageID() != tt.wantLanguage || e.doc.URI().Scheme != document.SchemeUntitled {
				t.Errorf("document = %q in %q (%s), want %q in %q", e.doc.Text(), e.doc.LanguageID(), e.doc.URI(), tt.text, tt.wantLanguage)
			}
			// the text is the first state of the document: an untitled document with a text counts as changed, as in VS Code, and
			// there is nothing to undo into
			if e.doc.Dirty() != (tt.text != "") || e.doc.CanUndo() {
				t.Errorf("dirty = %v, can undo = %v", e.doc.Dirty(), e.doc.CanUndo())
			}
			if got, err := docs.Read(context.Background(), uri); err != nil || got != tt.text {
				t.Errorf("Read = %q, %v", got, err)
			}
			if got := len(docs.Open()); got < i+1 {
				t.Errorf("open documents = %d", got)
			}
		})
	}
}

func TestAnUntitledDocumentCannotBeOpenedInAWindowTabThatIsNotOnScreen(t *testing.T) {
	docs := extensionDocuments{w: newTestWorkspace(t, "")}
	_, err := docs.OpenUntitled("sql", "SELECT 1;")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("extensionDocuments.OpenUntitled.ExtDocumentShowUnavailable") {
		t.Errorf("got %v", err)
	}
}
