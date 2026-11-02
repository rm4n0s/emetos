package app

import (
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The Testing view of a screen (SPEC §10.32, step 26.2): the container with the Test Explorer, the badge of its icon and the context keys,
// over the test service of the workspace. The host's window and the view of each client of a hosted workspace (D46) each have one of these,
// built from their own shell over the same service, so they show the same results; what a screen asks (a pick, a notification) is answered
// on that screen. The service and its host are in testing.go, the commands in testingcommands.go.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts, testingViewPaneContainer.ts and
// testingProgressUiService.ts

// testingShell is what the shell keeps of the Testing view.
type testingShell struct {
	explorer *testview.Explorer
	// menu opens the context menus of the tests, the menu behind "..." and the menu of the filter.
	menu *kit.ContextMenuController
	// stops end what the view follows, and removeCommands give its commands back.
	stops          []func()
	removeCommands []func()
	// focus tells whether the Test Explorer has the keyboard focus.
	focus bool
	// refreshing is set while Refresh Tests runs, and cancelRefresh stops it.
	refreshing    *atomic.Bool
	cancelRefresh func()
	// ticking is set while a goroutine redraws the summary line once a second for a run that goes on.
	ticking *atomic.Bool
	// fuzzy is the Fuzzy Match entry of the filter menu.
	fuzzy *atomic.Bool
}

// testingSettingsKeys are the settings the view reads while it draws.
var testingSettingsKeys = []string{"testing.countBadge"}

// testingContextKeys are the context keys the view sets.
var testingContextKeys = []string{"testing.providerCount", "testing.canRefresh", "testing.isRefreshing", "testing.isContinuousModeOn",
	"testing.hasDebuggableTests", "testing.hasRunnableTests", "testing.hasCoverableTests", "testing.hasNonDefaultProfile",
	"testing.hasConfigurableProfile", "testing.supportsContinuousRun", "testing.hasAnyResults", "testing.explorerViewMode",
	"testing.explorerViewSorting", "testing.isRunning", "testing.isPeekVisible", "testing.hasCoverageResults"}

// initTesting makes the Test Explorer over the test service and follows it.
func (s *shellState) initTesting(binding *widgets.WidgetsBinding) {
	win := s.win
	svc := win.TestingService()
	if svc == nil {
		return
	}
	st := &s.testing
	st.menu = kit.NewContextMenuController()
	// the flags are pointers: a goroutine of a refresh or of the clock may still set one when disposeTesting clears the struct
	st.refreshing, st.ticking, st.fuzzy = new(atomic.Bool), new(atomic.Bool), new(atomic.Bool)
	st.explorer = testview.NewExplorer(testview.Opts{
		Ctx: win.ctx, Service: svc, Post: binding.Post, DocumentURIs: s.testingDocumentURIs, Fuzzy: st.fuzzy.Load,
	})
	st.stops = append(st.stops, st.explorer.Dispose)
	// pending is set while a burst of events waits to be applied; it is not a field of s.testing, which disposeTesting clears on the UI
	// thread while the model may still be telling an event on its goroutine
	pending := new(atomic.Bool)
	st.stops = append(st.stops, svc.Subscribe(func(ev coretesting.Event) { s.onTestingEvent(binding, pending, ev) }))
	st.stops = append(st.stops, win.Settings.OnChange(func(e settings.ChangeEvent) {
		for _, key := range testingSettingsKeys {
			if e.Affects(key, "") {
				binding.Post(func() {
					if s.Mounted() && s.testing.explorer != nil {
						s.syncTestingParts()
					}
				})
				return
			}
		}
	}))
	s.bindTestingCommands(binding)
	s.syncTestingParts()
}

// disposeTesting ends the view and gives the commands back.
func (s *shellState) disposeTesting() {
	st := &s.testing
	for _, stop := range st.stops {
		stop()
	}
	for _, remove := range st.removeCommands {
		remove()
	}
	if st.cancelRefresh != nil {
		st.cancelRefresh()
	}
	*st = testingShell{}
}

// resetTestingParts takes the keys and the badge of the view away.
func (s *shellState) resetTestingParts() {
	win := s.win
	if win == nil || win.TestingService() == nil {
		return
	}
	win.Keys.Batch(func() {
		for _, key := range testingContextKeys {
			win.Keys.Reset(key)
		}
	})
	win.Views.SetBadge(command.ViewContainerTesting, workbench.ActivityBadge{})
}

// onTestingEvent follows the service: a burst of events is applied once on the UI thread.
func (s *shellState) onTestingEvent(binding *widgets.WidgetsBinding, pending *atomic.Bool, ev coretesting.Event) {
	if ev.Kind == coretesting.EventCoverage {
		return
	}
	if pending.Swap(true) {
		return
	}
	binding.Post(func() {
		pending.Store(false)
		if s.Mounted() && s.testing.explorer != nil {
			s.syncTestingParts()
			if ev.Kind == coretesting.EventResult || ev.Kind == coretesting.EventRunning {
				s.stateChangedIfMounted()
			}
		}
	})
}

// syncTestingParts applies the state of the model to the parts of the window that show it: the context keys and the badge of the icon of
// the container.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (ResultSummaryView.renderActivityBadge) and
// common/testingContextKeys.ts
func (s *shellState) syncTestingParts() {
	win := s.win
	svc := win.TestingService()
	st := &s.testing
	if svc == nil || st.explorer == nil {
		return
	}
	caps := svc.Capabilities("")
	ctrls := svc.Controllers()
	var nonDefault, configurable bool
	for _, c := range ctrls {
		for _, p := range svc.Profiles(c.ID) {
			nonDefault = nonDefault || !p.IsDefault
			configurable = configurable || p.HasConfigure
		}
	}
	coverage := false
	for _, r := range svc.Results() {
		for _, t := range r.Tasks() {
			coverage = coverage || t.HasCoverage
		}
	}
	win.Keys.Batch(func() {
		k := win.Keys
		k.Set("testing.providerCount", len(ctrls))
		k.Set("testing.canRefresh", len(ctrls) > 0)
		k.Set("testing.isRefreshing", st.refreshing.Load())
		k.Set("testing.isContinuousModeOn", svc.IsContinuousEnabled())
		k.Set("testing.hasDebuggableTests", caps.Debug)
		k.Set("testing.hasRunnableTests", caps.Run)
		k.Set("testing.hasCoverableTests", caps.Coverage)
		k.Set("testing.hasNonDefaultProfile", caps.HasNonDefault && nonDefault)
		k.Set("testing.hasConfigurableProfile", configurable)
		k.Set("testing.supportsContinuousRun", caps.Continuous)
		k.Set("testing.hasAnyResults", svc.HasAnyResults())
		k.Set("testing.explorerViewMode", string(st.explorer.Mode()))
		k.Set("testing.explorerViewSorting", string(st.explorer.Sorting()))
		k.Set("testing.isRunning", svc.IsRunning())
		k.Set("testing.isPeekVisible", false)
		k.Set("testing.hasCoverageResults", coverage)
	})
	s.syncTestingBadge(svc)
	if svc.IsRunning() {
		s.testingTick(s.binding)
	}
	s.SetState(func() {})
}

// syncTestingBadge shows the number of failed (passed, skipped) tests on the icon of the container, as testing.countBadge says.
func (s *shellState) syncTestingBadge(svc *coretesting.Service) {
	win := s.win
	sum := testview.SummaryOf(svc, win.Process.testingElapsed)
	badge := workbench.ActivityBadge{}
	if !sum.None && !sum.Running {
		setting := settings.Get[string](win.Settings, "testing.countBadge", "")
		if n := testview.BadgeCount(sum.Counts, setting); n > 0 {
			badge = workbench.ActivityBadge{Count: n, Tooltip: testview.BadgeTooltip(setting, n)}
		}
	}
	win.Views.SetBadge(command.ViewContainerTesting, badge)
}

// testingDocumentURIs are the files @doc (only the active editor's) and @openedFiles stand for.
func (s *shellState) testingDocumentURIs(activeOnly bool) []string {
	win := s.win
	if activeOnly {
		if s.Mounted() {
			if e := s.activeEditor(); e != nil && e.doc != nil && e.doc.URI().Scheme == "file" {
				return []string{explorerURI(e.doc.URI().Path)}
			}
		}
		return nil
	}
	var out []string
	for _, doc := range win.Documents.All() {
		if doc.URI().Scheme == "file" {
			out = append(out, explorerURI(doc.URI().Path))
		}
	}
	return out
}

// testingPath is the path of a file:// URI.
func testingPath(uri string) string {
	p := strings.TrimPrefix(uri, "file://")
	if un, err := url.PathUnescape(p); err == nil {
		return un
	}
	return p
}

// ---- the view

// isTestingView tells whether a view is the Test Explorer.
func isTestingView(id string) bool { return id == command.ViewTestExplorer }

// testingBody is the body of the Test Explorer, or nil where there is no testing.
func (s *shellState) testingBody() widgets.Widget {
	st := &s.testing
	if st.explorer == nil {
		return nil
	}
	body := testview.NewBody(st.explorer, testview.BodyOpts{
		Message: "No tests have been found in this workspace yet.",
		Summary: func() testview.SummaryState {
			return testview.SummaryOf(s.win.TestingService(), s.win.Process.testingElapsed)
		},
		Select: func(e *testview.Element) { s.testingRowSelected(e) },
		Open:   func(e *testview.Element) { s.testingOpenRow(e) },
		Menu:   func(e *testview.Element, at ui.Offset) { s.showTestingMenu(e, at) },
		FilterMenu: func(at ui.Offset) {
			st.menu.Show(at, s.testingFilterMenu())
		},
		Actions: s.testingInlineActions,
		RunAction: func(e *testview.Element, id string) {
			s.runTestingCommand(id, e.ID)
		},
		SummaryActions: func(sum testview.SummaryState) []kit.Action {
			if sum.None || sum.Running {
				return nil
			}
			icon := func(name string) int { c, _ := fonts.CodiconByName(name); return c }
			return []kit.Action{
				{ID: command.TestingReRunLastRun, Label: "Rerun Last Run", Icon: icon("debug-rerun")},
				{ID: command.TestingDebugLastRun, Label: "Debug Last Run", Icon: icon("debug-alt-small")},
			}
		},
		RunSummaryAction: func(id string) { s.runTestingCommand(id, nil) },
		OnFocusChange:    func(has bool) { s.testingFilterFocused(has) },
	})
	return kit.NewContextMenu(body, kit.ContextMenuOpts{Controller: st.menu, OnRun: s.menus.Run, OnHide: func() {}})
}

// testingFilterFocused follows the focus of the filter box into the keys of the view.
func (s *shellState) testingFilterFocused(has bool) {
	if has {
		s.win.Keys.Set("focusedView", command.ViewTestExplorer)
	} else if s.win.Keys.Get("focusedView") == command.ViewTestExplorer && !s.testing.focus {
		s.win.Keys.Set("focusedView", "")
	}
}

// testingViewFocused follows the focus into the view.
func (s *shellState) testingViewFocused(has bool) {
	win := s.win
	s.testing.focus = has
	if has {
		win.Keys.Set("focusedView", command.ViewTestExplorer)
	} else if win.Keys.Get("focusedView") == command.ViewTestExplorer {
		win.Keys.Set("focusedView", "")
	}
}

// testingRowSelected notes the row the user selected.
func (s *shellState) testingRowSelected(e *testview.Element) {
	s.win.Keys.Set("focusedView", command.ViewTestExplorer)
}

// testingOpenRow opens the file of a test at its place (Enter and a double click).
func (s *shellState) testingOpenRow(e *testview.Element) {
	if e == nil || e.Kind != testview.KindTest {
		return
	}
	s.testingGoTo(e.Item)
}

// testingGoTo opens the file of an item at its range.
func (s *shellState) testingGoTo(it coretesting.Item) {
	if it.URI == "" {
		return
	}
	path := testingPath(it.URI)
	if _, err := os.Stat(path); err != nil {
		return
	}
	line, col := 0, 0
	if it.Range != nil {
		line, col = it.Range.Start.Line+1, it.Range.Start.Col+1
	}
	s.openPathAt(s.binding, path, line, col, openOptions{Preview: true, TakeFocus: true})
}

// testingRowKeys are the context keys of a test, for its menu and its buttons.
func (s *shellState) testingRowKeys(e *testview.Element) map[string]any {
	svc := s.win.TestingService()
	extra := map[string]any{"view": command.ViewTestExplorer}
	if e == nil || svc == nil {
		return extra
	}
	caps := svc.Capabilities(e.ID)
	extra["testing.hasRunnableTests"] = caps.Run
	extra["testing.hasDebuggableTests"] = caps.Debug
	extra["testing.hasCoverableTests"] = caps.Coverage
	extra["testing.hasNonDefaultProfile"] = caps.HasNonDefault
	extra["testing.supportsContinuousRun"] = caps.Continuous
	extra["testing.testItemHasUri"] = e.Item.URI != ""
	extra["controllerId"] = coretesting.ControllerOf(e.ID)
	extra["testId"] = e.ID
	extra["testing.testItemIsHidden"] = s.testing.explorer.IsHidden(e.ID)
	extra["testing.isContinuousModeOn"] = caps.Continuous && svc.ContinuousSpecificallyFor(e.ID)
	extra["testing.isParentRunningContinuously"] = caps.Continuous && svc.ContinuousForAParentOf(e.ID)
	extra["testResultOutdated"] = e.Retired
	extra["testResultState"] = e.State.String()
	return extra
}

// testingInlineActions are the buttons on a test while the pointer is over it: the items of the group inline of its menu.
func (s *shellState) testingInlineActions(e *testview.Element) []kit.Action {
	win := s.win
	if e.Kind != testview.KindTest {
		return nil
	}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: s.testingRowKeys(e)}
	var out []kit.Action
	for _, m := range win.Menus.Items(command.TestingItem, ctx) {
		if m.Command == "" || m.Group != "inline" {
			continue
		}
		code, _ := fonts.CodiconByName(m.Icon)
		out = append(out, kit.Action{ID: m.Command, Label: m.Title, Icon: code, Disabled: m.Disabled, Checked: m.Checked})
	}
	return out
}

// showTestingMenu opens the context menu of a test; below the rows it is the menu of the whole view.
func (s *shellState) showTestingMenu(e *testview.Element, at ui.Offset) {
	win := s.win
	if e == nil || e.Kind != testview.KindTest {
		items := s.menus.ItemsWithout(command.TestingViewTitle, menuContext{base: win.Keys, settings: win.Settings, extra: map[string]any{"view": command.ViewTestExplorer}},
			func(m command.MenuEntry) bool { return m.Group == "navigation" })
		if len(items) > 0 {
			s.testing.menu.Show(at, items)
		}
		return
	}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: s.testingRowKeys(e)}
	items := s.menus.ItemsWithout(command.TestingItem, ctx, func(m command.MenuEntry) bool { return m.Group == "inline" })
	for i := range items {
		if !items[i].Separator && !items[i].Submenu && items[i].ID != "" && items[i].Data == nil {
			items[i].Data = e.ID
		}
	}
	if len(items) > 0 {
		s.testing.menu.Show(at, items)
	}
}

// testingFilterMenu is the menu of the filter: the terms, Fuzzy Match, and the hidden tests.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerFilter.ts (FiltersDropdownMenuActionViewItem)
func (s *shellState) testingFilterMenu() []kit.MenuItem {
	ex := s.testing.explorer
	var items []kit.MenuItem
	for _, term := range []string{testview.TermFailed, testview.TermExecuted, testview.TermDoc, testview.TermOpenedFiles} {
		items = append(items, kit.MenuItem{ID: "term:" + term, Label: testview.TermDescriptions[term], Checked: ex.Filter().Has(term),
			OnRun: func() { ex.ToggleTerm(term) }})
	}
	items = append(items, kit.SeparatorItem(),
		kit.MenuItem{ID: "fuzzy", Label: "Fuzzy Match", Checked: s.testing.fuzzy.Load(), OnRun: func() {
			s.testing.fuzzy.Store(!s.testing.fuzzy.Load())
			ex.SetFilterText(ex.FilterText() + " ")
			ex.SetFilterText(strings.TrimSpace(ex.FilterText()))
		}},
		kit.SeparatorItem(),
		kit.MenuItem{ID: "showHidden", Label: "Show Hidden Tests", Checked: ex.Filter().Has(testview.TermHidden), Disabled: !ex.HasHidden(),
			OnRun: func() { ex.ToggleTerm(testview.TermHidden) }},
		kit.MenuItem{ID: "unhideAll", Label: "Unhide All Tests", Disabled: !ex.HasHidden(), OnRun: func() { ex.UnhideAll() }},
	)
	return items
}

// testingViewActions are the buttons in the header of the Test Explorer: the items of group navigation of the menu of its title bar that
// hold, and "..." with the other items.
func (s *shellState) testingViewActions() widgets.Widget {
	win := s.win
	if s.testing.explorer == nil {
		return nil
	}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: map[string]any{"view": command.ViewTestExplorer}}
	var buttons []kit.Action
	for _, e := range win.Menus.Items(command.TestingViewTitle, ctx) {
		if e.Command == "" || e.Group != "navigation" {
			continue
		}
		code, _ := fonts.CodiconByName(e.Icon)
		buttons = append(buttons, kit.Action{ID: e.Command, Label: e.Title, Icon: code, Disabled: e.Disabled})
	}
	more := s.menus.ItemsWithout(command.TestingViewTitle, ctx, func(e command.MenuEntry) bool { return e.Group == "navigation" })
	var cells []widgets.Widget
	if len(buttons) > 0 {
		cells = append(cells, kit.NewActionBar(buttons, kit.ActionBarOpts{OnRun: func(a kit.Action) { s.runTestingCommand(a.ID, nil) }}))
	}
	if len(more) > 0 {
		cells = append(cells, widgets.NewGestureDetector(
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconEllipsis), widgets.IconOpts{Size: foundation.Some(16.0)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTapDown: func(d gestures.TapDownDetails) {
				s.testing.menu.Show(ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy + 10}, more)
			}}))
	}
	if len(cells) == 0 {
		return nil
	}
	return widgets.NewRow(cells, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// runTestingCommand runs a command of Testing for a button, on this screen.
func (s *shellState) runTestingCommand(id string, args any) {
	win := s.win
	if err := win.Commands.Run(command.Context{Ctx: win.ctx, Keys: win.Keys, Source: command.SourceMenu}, id, args); err != nil {
		win.Process.Logger.Warn("a command of Testing failed", "command", id, "err", err)
	}
}

// showTesting shows the container, with the tree (or the filter box) focused when focus is set.
func (s *shellState) showTesting(binding *widgets.WidgetsBinding, focus, filter bool) {
	win := s.win
	win.Views.Open(command.ViewContainerTesting)
	if !focus {
		return
	}
	win.Views.SetHidden(command.ViewTestExplorer, false)
	win.Views.SetCollapsed(command.ViewTestExplorer, false)
	s.afterFrame(binding, func() {
		ex := s.testing.explorer
		if ex == nil {
			return
		}
		if filter {
			ex.Input().Focus()
		} else {
			ex.Controller().Focus()
		}
	})
}

// testingTick redraws the summary line once a second while a run goes on, so that its duration counts up.
func (s *shellState) testingTick(binding *widgets.WidgetsBinding) {
	st := &s.testing
	svc := s.win.TestingService()
	if st.ticking.Swap(true) {
		return
	}
	ex, ticking, ctx := st.explorer, st.ticking, s.win.ctx
	go func() {
		defer ticking.Store(false)
		for svc.IsRunning() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			binding.Post(func() {
				if s.Mounted() && s.testing.explorer == ex {
					ex.Touch()
				}
			})
		}
	}()
}

// sideBarActions are the buttons in the title of the side bar: those of Run and Debug while it is shown, and those of the Test Explorer
// while Testing is (VS Code merges the only view of the Testing container with its title).
func (s *shellState) sideBarActions(bc widgets.BuildContext) widgets.Widget {
	if s.win.Views.Active() == command.ViewContainerTesting {
		return s.testingViewActions()
	}
	return s.debugSideBarActions(bc)
}
