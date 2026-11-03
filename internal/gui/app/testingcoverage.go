package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/decorations"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// Coverage in the window (SPEC §10.32 "Test Coverage", step 26.4): the coverage a run reported is open in the Test Coverage view, in the
// editors of its files (the gutter, and inline the ranges, the counts and the branches) and in the Explorer (the percentage of every file
// and folder, as a decoration). The workspace holds which coverage is open (coretesting.CoverageService), so the screens of the host and of
// its clients show the same; the toolbar over the editor, the view and the keys are each screen's.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/codeCoverageDecorations.ts, testCoverageView.ts, testCoverageBars.ts and
// common/testCoverageService.ts

// coverageDecorationWeight orders the percentage of the Explorer after the decorations of the extensions (Git's status has the weight 10).
const coverageDecorationWeight = 1

// coverageSettings are the settings the coverage reads while it draws.
var coverageSettings = []string{"testing.displayedCoveragePercent", "testing.coverageBarThresholds", "testing.showCoverageInExplorer", "testing.coverageToolbarEnabled"}

// coverageDecorations is the provider of the percentages in the Explorer: it answers for the files and folders of the open coverage.
type coverageDecorations struct {
	w *Workspace
}

// Decoration implements decorations.Provider: the displayed percentage as a badge, in the color of the bar for it.
func (p *coverageDecorations) Decoration(uri string) (decorations.Data, bool) {
	w := p.w
	if !settings.Get[bool](w.Settings, "testing.showCoverageInExplorer", "") {
		return decorations.Data{}, false
	}
	sel, ok := w.Coverage.Selected()
	if !ok {
		return decorations.Data{}, false
	}
	cov, ok := sel.Coverage.Computed(uri)
	if !ok {
		return decorations.Data{}, false
	}
	stat := coretesting.DisplayedStat(cov.Statement, cov.Branch, cov.Declaration, settings.Get[string](w.Settings, "testing.displayedCoveragePercent", ""))
	th := coretesting.ThresholdsOf(settings.Get[map[string]any](w.Settings, "testing.coverageBarThresholds", ""))
	return decorations.Data{
		Weight: coverageDecorationWeight, Color: "charts." + coretesting.CoverageColor(stat, th), Letter: coretesting.DisplayPercent(stat, 0),
		Tooltip: testview.BarsTooltip(testview.BarsOf(cov)),
	}, true
}

// initCoverage makes the coverage service of the workspace over its test service, and the provider of the Explorer's percentages.
func (w *Workspace) initCoverage(svc *coretesting.Service) {
	w.Coverage = coretesting.NewCoverageService(svc)
	w.stops = append(w.stops, w.Coverage.Dispose)
	if w.Decorations == nil {
		return
	}
	reg, err := w.Decorations.Register("testing.coverage", &coverageDecorations{w: w})
	if err != nil {
		w.Process.Logger.Warn("cannot register the coverage decorations", "err", err)
		return
	}
	w.stops = append(w.stops, reg.Dispose, w.Coverage.OnChange(func() { reg.Fire() }), w.Settings.OnChange(func(e settings.ChangeEvent) {
		for _, key := range coverageSettings[:3] {
			if e.Affects(key, "") {
				reg.Fire()
				return
			}
		}
	}))
}

// coverageThresholds and coveragePercent are the settings of the bars now.
func (s *shellState) coverageThresholds() coretesting.BarThresholds {
	return coretesting.ThresholdsOf(settings.Get[map[string]any](s.win.Settings, "testing.coverageBarThresholds", ""))
}

func (s *shellState) coveragePercent() string {
	return settings.Get[string](s.win.Settings, "testing.displayedCoveragePercent", "")
}

// initTestCoverage makes the Test Coverage view over the coverage service and follows it.
func (s *shellState) initTestCoverage(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.testing
	if win.Coverage == nil {
		return
	}
	st.coverage = testview.NewCoverage(testview.CoverageOpts{
		Ctx: win.ctx, Service: win.Coverage, Post: binding.Post,
		Top: func(path string) string {
			if win.Folder != "" {
				if rel, err := filepath.Rel(win.Folder, path); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
					return filepath.ToSlash(rel)
				}
			}
			return filepath.Base(path)
		},
		Icons: win.FileIcons, Language: win.languageOf,
		Percent: s.coveragePercent, Thresholds: s.coverageThresholds,
		OnOpen: s.coverageOpenNode,
	})
	st.stops = append(st.stops, st.coverage.Dispose)
	pending := new(atomic.Bool)
	st.stops = append(st.stops, win.Coverage.OnChange(func() {
		if pending.Swap(true) {
			return
		}
		binding.Post(func() {
			pending.Store(false)
			if s.Mounted() && s.testing.explorer != nil {
				s.syncTestingParts()
			}
		})
	}))
	st.stops = append(st.stops, win.Settings.OnChange(func(e settings.ChangeEvent) {
		for _, key := range coverageSettings {
			if e.Affects(key, "") {
				binding.Post(func() {
					if s.Mounted() && s.testing.coverage != nil {
						s.testing.coverage.Refresh()
						s.syncTestingParts()
					}
				})
				return
			}
		}
	}))
}

// coverageOpenNode opens the file of a node, at the declaration for one.
func (s *shellState) coverageOpenNode(n *testview.CoverageNode) {
	if n == nil || n.Kind == testview.CoverageFolder {
		return
	}
	line, col := 0, 0
	if n.Kind == testview.CoverageDeclaration {
		line, col = n.Range.Start.Line+1, n.Range.Start.Col+1
	}
	path := testingPath(n.URI)
	s.openPathAt(s.binding, path, line, col, openOptions{Preview: true, PreserveFocus: false, TakeFocus: true})
}

// isCoverageView tells whether a view is the Test Coverage view.
func isCoverageView(id string) bool { return id == command.ViewTestCoverage }

// testCoverageBody is the body of the Test Coverage view, or nil where there is none.
func (s *shellState) testCoverageBody() widgets.Widget {
	if s.testing.coverage == nil {
		return nil
	}
	return testview.NewCoverageBody(s.testing.coverage)
}

// coverageViewFocused follows the focus into the Test Coverage view.
func (s *shellState) coverageViewFocused(has bool) {
	keys := s.win.Keys
	if has {
		keys.Set("focusedView", command.ViewTestCoverage)
	} else if keys.Get("focusedView") == command.ViewTestCoverage {
		keys.Set("focusedView", "")
	}
}

// coverageViewActions are the buttons in the header of the Test Coverage view: the items of group navigation of its title bar.
func (s *shellState) coverageViewActions() widgets.Widget {
	win := s.win
	if s.testing.coverage == nil {
		return nil
	}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: map[string]any{"view": command.ViewTestCoverage}}
	var buttons []kit.Action
	for _, e := range win.Menus.Items(command.TestingCoverageTitle, ctx) {
		if e.Command == "" || e.Group != "navigation" {
			continue
		}
		code, _ := fonts.CodiconByName(e.Icon)
		buttons = append(buttons, kit.Action{ID: e.Command, Label: e.Title, Icon: code, Disabled: e.Disabled})
	}
	if len(buttons) == 0 {
		return nil
	}
	return widgets.NewRow([]widgets.Widget{kit.NewActionBar(buttons, kit.ActionBarOpts{OnRun: func(a kit.Action) { s.runTestingCommand(a.ID, nil) }})},
		widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// showCoverageView shows the container with the Test Coverage view and gives its tree the keyboard.
func (s *shellState) showCoverageView(binding *widgets.WidgetsBinding) {
	win := s.win
	if _, ok := win.Coverage.Selected(); !ok {
		return
	}
	win.Views.Open(command.ViewContainerTesting)
	win.Views.SetHidden(command.ViewTestCoverage, false)
	win.Views.SetCollapsed(command.ViewTestCoverage, false)
	s.afterFrame(binding, func() {
		if cv := s.testing.coverage; cv != nil {
			cv.Controller().Focus()
		}
	})
}

// changeCoverageSort asks which order the Test Coverage view is sorted in.
//
// Ported from VS Code: testCoverageView.ts (TestCoverageChangeSortingAction)
func (s *shellState) changeCoverageSort() {
	cv := s.testing.coverage
	if cv == nil {
		return
	}
	var items []quickinput.Item
	for _, c := range testview.CoverageSortChoices {
		choice := c
		items = append(items, quickinput.Item{ID: string(c.Sort), Label: c.Label, Description: c.Description, Accept: func(quickinput.KeyMods) {
			cv.SetSort(choice.Sort)
		}})
	}
	s.win.QuickInput.Pick(quickinput.PickOptions{Title: "Change Sort Order", Placeholder: "Sort the Test Coverage view...", Items: items})
}

// openCoverage opens the coverage of the newest result that has any (Open Coverage).
//
// Ported from VS Code: testExplorerActions.ts (OpenCoverage)
func (s *shellState) openCoverage() {
	win := s.win
	results := win.TestingService().Results()
	if len(results) > 0 {
		for _, t := range results[0].Tasks() {
			if t.HasCoverage {
				win.Coverage.Open(results[0], t.ID)
				s.showCoverageView(s.binding)
				return
			}
		}
	}
	win.Notify.Info("Emetos", "No coverage information available on the last test run.")
}

// toggleSetting flips a boolean setting of the user.
func (s *shellState) toggleSetting(key string) {
	win := s.win
	on := settings.Get[bool](win.Settings, key, "")
	if err := win.Settings.Update(win.ctx, key, !on, settings.TargetUser); err != nil {
		win.Notify.Error("Emetos", "Cannot change "+key+": "+describe(err))
	}
}

// bindCoverageCommands adds the commands of the coverage to the handlers of Testing.
func (s *shellState) bindCoverageCommands(handlers map[string]command.Handler) {
	win := s.win
	run := func(f func()) command.Handler { return func(command.Context, any) error { f(); return nil } }
	handlers[command.TestingFocusCoverage] = run(func() { s.showCoverageView(s.binding) })
	handlers[command.TestingToggleInlineCoverage] = run(func() {
		if win.Coverage != nil {
			win.Coverage.SetShowInline(!win.Coverage.ShowInline())
		}
	})
	handlers[command.TestingToggleCoverageToolbar] = run(func() { s.toggleSetting("testing.coverageToolbarEnabled") })
	handlers[command.TestingToggleCoverageInExplorer] = run(func() { s.toggleSetting("testing.showCoverageInExplorer") })
	handlers[command.TestingCoverageClose] = run(func() { win.Coverage.Close() })
	handlers[command.TestingCoverageNextMissed] = run(func() { s.goToMissed(true) })
	handlers[command.TestingCoveragePreviousMissed] = run(func() { s.goToMissed(false) })
	handlers[command.TestingCoverageChangeSorting] = run(s.changeCoverageSort)
	handlers[command.TestingCoverageCollapseAll] = run(func() {
		if cv := s.testing.coverage; cv != nil {
			cv.CollapseAll()
		}
	})
	handlers[command.TestingOpenCoverage] = run(s.openCoverage)
}

// goToMissed moves the cursor of the active editor to the next (or previous) line that has uncovered code and none that ran, going round
// at the end of the file.
//
// Ported from VS Code: codeCoverageDecorations.ts (navigateToMissedLine)
func (s *shellState) goToMissed(forward bool) {
	e := s.activeEditor()
	if e == nil || e.cov == nil {
		return
	}
	line := nextMissed(e.cov.missedLines(), e.view.Cursors().Primary().Selection.Active.Line, forward)
	if line < 0 {
		return
	}
	at := buffer.Position{Line: line}
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(at)})
	e.view.RevealPositionInCenter(at)
}

// syncCoverageKeys sets the context keys of the coverage: the report that is open, the inline coverage, the toolbar, and what the active
// editor has of it.
func (s *shellState) syncCoverageKeys() {
	win := s.win
	if win == nil || win.Coverage == nil {
		return
	}
	_, open := win.Coverage.Selected()
	hasFile, hasDetails := false, false
	if e := s.activeEditor(); e != nil && e.cov != nil {
		hasFile, hasDetails = e.cov.hasFile(), e.cov.hasDetails()
	}
	win.Keys.Batch(func() {
		win.Keys.Set("testing.isTestCoverageOpen", open)
		win.Keys.Set("testing.inlineCoverageEnabled", win.Coverage.ShowInline())
		win.Keys.Set("testing.coverageToolbarEnabled", settings.Get[bool](win.Settings, "testing.coverageToolbarEnabled", ""))
		win.Keys.Set("testing.hasCoverageInFile", hasFile)
		win.Keys.Set("testing.hasInlineCoverageDetails", hasDetails)
	})
}

// ---- the editor

// editorCoverage is the coverage of one text editor: the details of its file, and the marks it puts on the editor.
type editorCoverage struct {
	s       *shellState
	binding *widgets.WidgetsBinding
	win     *Workspace
	e       *textEditor

	path, uri string
	pending   atomic.Bool
	stops     []func()
	disposed  bool

	// cov and fileID say which coverage the details are of.
	cov    *coretesting.Coverage
	fileID string
	file   coretesting.FileCoverage
	has    bool
	// details are those of the file, loaded says that they are, and ranges are their ranges laid out.
	details coretesting.Details
	loaded  bool
	ranges  []coretesting.DetailRange
	layer   coverageLayer
	cancel  context.CancelFunc
	// loadErr is the failure of the last load, kept for the tests.
	loadErr error
}

// followCoverage makes the editor show the open coverage. It returns the function that stops it.
func (s *shellState) followCoverage(binding *widgets.WidgetsBinding, e *textEditor) func() {
	win := PeekWorkspace(s.Context())
	if win == nil || win.Coverage == nil {
		return func() {}
	}
	d := &editorCoverage{s: s, binding: binding, win: win, e: e}
	e.cov = d
	d.attach()
	d.stops = append(d.stops,
		win.Coverage.OnChange(d.schedule),
		win.Settings.OnChange(func(ev settings.ChangeEvent) {
			if ev.Affects("testing", "") {
				d.schedule()
			}
		}),
		e.doc.OnURIChange(func(_, _ document.URI) {
			binding.Post(func() {
				if !d.disposed {
					d.attach()
					d.refresh()
				}
			})
		}),
	)
	d.refresh()
	return d.dispose
}

// schedule refreshes the editor on the UI thread, once for any number of changes.
func (d *editorCoverage) schedule() {
	if d.pending.CompareAndSwap(false, true) {
		d.binding.Post(func() {
			d.pending.Store(false)
			if !d.disposed {
				d.refresh()
			}
		})
	}
}

func (d *editorCoverage) attach() {
	d.path, d.uri = "", ""
	u := d.e.doc.URI()
	if u.Scheme == document.SchemeFile && u.Path != "" {
		d.path, d.uri = u.Path, explorerURI(u.Path)
	}
}

func (d *editorCoverage) dispose() {
	if d.disposed {
		return
	}
	d.disposed = true
	for _, stop := range d.stops {
		stop()
	}
	d.stops = nil
	if d.cancel != nil {
		d.cancel()
	}
	d.e.view.DisposeCoverage()
	d.e.cov = nil
	d.s.syncCoverageKeys()
}

// hasFile says that the open coverage has numbers for the file, hasDetails that it has the details of its lines.
func (d *editorCoverage) hasFile() bool    { return d != nil && d.has }
func (d *editorCoverage) hasDetails() bool { return d != nil && d.has && d.loaded && len(d.ranges) > 0 }

// missedLines are the lines with uncovered code, for the navigation.
func (d *editorCoverage) missedLines() []int {
	if d == nil {
		return nil
	}
	// the lines move with the edits of the document, but the navigation uses the lines of the details, as VS Code's decorations do not
	// move them either until the file is reported again
	return d.layer.missed
}

// clear takes the coverage off the editor.
func (d *editorCoverage) clear() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.cov, d.fileID, d.has, d.loaded, d.ranges, d.layer = nil, "", false, false, nil, coverageLayer{}
	d.e.view.SetCoverageMarks(nil)
	d.e.view.SetCoverageAnnotations(nil)
}

// refresh puts the open coverage on the editor.
func (d *editorCoverage) refresh() {
	sel, ok := d.win.Coverage.Selected()
	var file coretesting.FileCoverage
	has := false
	if ok && d.uri != "" {
		file, has = sel.Coverage.Computed(d.uri)
	}
	if !has {
		d.clear()
		d.s.syncCoverageKeys()
		d.s.SetState(func() {})
		return
	}
	same := d.cov == sel.Coverage && d.fileID == file.ID && d.loaded
	d.cov, d.fileID, d.file, d.has = sel.Coverage, file.ID, file, true
	if same {
		d.apply()
	} else {
		d.load(sel.Coverage)
	}
	d.s.syncCoverageKeys()
	d.s.SetState(func() {})
}

// load reads the details of the file in the background and shows them when they arrive.
func (d *editorCoverage) load(cov *coretesting.Coverage) {
	if d.cancel != nil {
		d.cancel()
	}
	d.loaded = false
	ctx, cancel := context.WithCancel(d.win.ctx)
	d.cancel = cancel
	uri, id := d.uri, d.fileID
	go func() {
		details, err := cov.Details(ctx, uri)
		d.binding.Post(func() {
			if d.disposed || ctx.Err() != nil || d.cov != cov || d.fileID != id {
				return
			}
			d.loadErr = err
			if err != nil {
				d.win.Process.Logger.Warn("cannot load the coverage details", "file", d.path, "err", err)
				d.layer = coverageLayer{}
				d.e.view.SetCoverageMarks(nil)
				d.e.view.SetCoverageAnnotations(nil)
				d.s.syncCoverageKeys()
				return
			}
			d.details, d.loaded = details, true
			d.ranges = coretesting.BuildDetailRanges(details)
			d.apply()
			d.s.syncCoverageKeys()
			d.s.SetState(func() {})
		})
	}()
}

// apply puts the marks of the loaded details on the editor.
func (d *editorCoverage) apply() {
	if !d.loaded {
		return
	}
	snap := d.e.doc.Snapshot()
	textOf := func(r coretesting.Range) string { return rangeText(snap, r) }
	d.layer = buildCoverageLayer(d.details, d.ranges, d.win.Coverage.ShowInline(), textOf)
	d.e.view.SetCoverageMarks(d.layer.marks)
	d.e.view.SetCoverageAnnotations(d.layer.notes)
}

// rangeText is the text of a range of a document, empty when a line is not there.
func rangeText(snap buffer.Snapshot, r coretesting.Range) string {
	var b strings.Builder
	for l := r.Start.Line; l <= r.End.Line && l < snap.LineCount(); l++ {
		text, err := snap.LineContent(l)
		if err != nil {
			break
		}
		from, to := 0, len(text)
		if l == r.Start.Line {
			from = min(r.Start.Col, len(text))
		}
		if l == r.End.Line {
			to = min(r.End.Col, len(text))
		}
		if from < to {
			b.WriteString(text[from:to])
		}
		if l < r.End.Line {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// ---- the toolbar

// withCoverageToolbar puts the toolbar of the coverage over the top of the editor of a file that has coverage while
// testing.coverageToolbarEnabled is on.
//
// Ported from VS Code: codeCoverageDecorations.ts (CoverageToolbarWidget)
func (s *shellState) withCoverageToolbar(area widgets.Widget, ed *textEditor) widgets.Widget {
	win := s.win
	if ed.cov == nil || !ed.cov.hasFile() || win.Coverage == nil || !settings.Get[bool](win.Settings, "testing.coverageToolbarEnabled", "") {
		return area
	}
	bar := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		colors := kit.ThemeOf(bc)
		d := ed.cov
		inline := win.Coverage.ShowInline()
		toggle := "Show Inline"
		if inline {
			toggle = "Hide Inline"
		}
		missed := len(d.missedLines()) > 0
		btn := func(code int, tip string, enabled bool, run func(), checked bool) widgets.Widget {
			opts := kit.IconButtonOpts{Tooltip: tip, Disabled: !enabled, Checked: checked}
			if enabled {
				opts.OnPressed = run
			}
			return kit.NewIconButton(code, opts)
		}
		bg, border := colors.EditorWidgetBackground, colors.WidgetBorder
		box := widgets.NewDecoratedBox(
			painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color: foundation.Some(bg), BorderRadius: painting.NewBorderRadiusCircular(4),
				Border:    painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
				BoxShadow: []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: 4})},
			}),
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(4, 8), widgets.NewRow([]widgets.Widget{
				testview.NewCoverageBars(bc, testview.BarsOf(d.file), testview.CoverageBarsOpts{NoOverall: true, Percent: s.coveragePercent(), Thresholds: s.coverageThresholds(), Key: coverageToolbarBarsKey}),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
				btn(fonts.CodiconArrowUp, "Go to Previous Uncovered Line", missed, func() { s.runTestingCommand(command.TestingCoveragePreviousMissed, nil) }, false),
				btn(fonts.CodiconArrowDown, "Go to Next Uncovered Line", missed, func() { s.runTestingCommand(command.TestingCoverageNextMissed, nil) }, false),
				btn(fonts.CodiconCoverage, toggle, true, func() { s.runTestingCommand(command.TestingToggleInlineCoverage, nil) }, inline),
			}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})))
		return widgets.NewAlign(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 6, 0, 0), widgets.NewKeyedSubtree(box, widgets.WidgetOpts{Key: CoverageToolbarKey})),
			widgets.AlignOpts{Alignment: painting.AlignmentTopCenter})
	})
	return widgets.NewStack([]widgets.Widget{area, bar}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// Keys of the toolbar, which tests find it by.
var (
	// CoverageToolbarKey is the key of the toolbar over an editor and coverageToolbarBarsKey that of its bars.
	CoverageToolbarKey     = foundation.NewValueKey("testcoverage:toolbar")
	coverageToolbarBarsKey = foundation.NewValueKey("testcoverage:toolbar-bars")
)
