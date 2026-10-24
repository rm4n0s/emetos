package problems

import (
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Messages of the Problems panel.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/messages.ts
const (
	// MessageNoProblems is shown while there are no markers.
	MessageNoProblems = "No problems have been detected in the workspace."
	// MessageNoProblemsActiveFile is shown when the markers of the active file are asked for and it has none.
	MessageNoProblemsActiveFile = "No problems have been detected in the current file."
	// MessageNoResults is shown when the filter lets no marker through.
	MessageNoResults = "No results found with provided filter criteria."
	// FilterPlaceholder is the hint of the filter box.
	FilterPlaceholder = "Filter (e.g. text, **/*.ts, !**/node_modules/**, @source:ts)"
)

// Keys of the widgets of the panel, which tests find them by.
var (
	// TreeKey is the key of the tree of the problems.
	TreeKey = foundation.NewValueKey("problems:tree")
	// MessageKey is the key of the message that stands in place of the tree.
	MessageKey = foundation.NewValueKey("problems:message")
	// ClearFiltersKey is the key of the link that clears the filters in the message.
	ClearFiltersKey = foundation.NewValueKey("problems:clear-filters")
	// FilterKey is the key of the filter box.
	FilterKey = foundation.NewValueKey("problems:filter")
	// CollapseAllKey is the key of the button that collapses the tree.
	CollapseAllKey = foundation.NewValueKey("problems:collapse-all")
)

// ToggleKey is the key of the toggle of a severity (the Severity values) in the actions of the panel.
func ToggleKey(s Severity) foundation.Key {
	return foundation.NewValueKey("problems:toggle:" + strconv.Itoa(int(s)))
}

// ActiveFileKey is the key of the toggle that shows the markers of the active file only.
var ActiveFileKey = foundation.NewValueKey("problems:active-file")

// ViewOpts configure a View.
type ViewOpts struct {
	// Post hands a function to the UI thread (the binding's Post); changes of the markers are read
	// there. Without it they are read at once, which is for tests.
	Post func(func())
	// Root is the folder of the window; the folders of the files are shown relative to it.
	Root string
	// Keys is the scope of the context keys of the window: the view sets problemFocus,
	// problemsFilterFocus and problems.filter.errors, .warnings, .info and .activeFile there.
	Keys *contextkey.Scope
	// OnOpen is called when the user opens a marker: a click on it or Enter (pinned is false), a
	// double click (pinned is true). It shows the file with the cursor at the start of the range.
	OnOpen func(m Marker, pinned bool)
	// Logger gets the failures that have nobody to tell; slog.Default() when nil.
	Logger *slog.Logger
}

// View is the Problems panel without its widgets: the model of the markers, the tree that shows it,
// and the text of the filter box. The window makes one; NewBody and NewActions are the widgets
// that show it, and the commands of the panel call its methods. It is used on the UI thread only.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersView.ts (MarkersView)
type View struct {
	model     *Model
	tree      *kit.TreeModel
	treeCtl   *kit.TreeController
	filter    *kit.InputBoxController
	opts      ViewOpts
	seen      map[string]bool
	stop      func()
	listeners []*func()
}

// NewView makes the view of the markers of svc.
func NewView(svc *Service, opts ViewOpts) *View {
	post := opts.Post
	if post == nil {
		post = func(f func()) { f() }
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	v := &View{
		model:   NewModel(svc, post),
		tree:    kit.NewTreeModel(),
		treeCtl: kit.NewTreeController(),
		filter:  kit.NewInputBoxController(""),
		opts:    opts,
		seen:    map[string]bool{},
	}
	v.model.SetRoot(opts.Root)
	v.stop = v.model.OnChange(v.refresh)
	v.refresh()
	return v
}

// Dispose stops following the markers and releases the widgets' controllers.
func (v *View) Dispose() {
	v.stop()
	v.model.Close()
	v.filter.Dispose()
}

// Model is the model of the markers.
func (v *View) Model() *Model { return v.model }

// Tree is the model of the tree that shows the markers.
func (v *View) Tree() *kit.TreeModel { return v.tree }

// OnChange calls fn after the markers, the filter or the tree changed, and returns the function that
// stops it.
func (v *View) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	v.listeners = append(v.listeners, p)
	return func() {
		if i := slices.Index(v.listeners, p); i >= 0 {
			v.listeners = slices.Delete(v.listeners, i, i+1)
		}
	}
}

func (v *View) notify() {
	for _, f := range slices.Clone(v.listeners) {
		(*f)()
	}
}

// resourceKey and markerKey are the keys of the nodes of the tree.
func resourceKey(path string) string { return "r:" + path }

func markerKey(path string, i int) string { return "m:" + path + "#" + strconv.Itoa(i) }

// markerData is the data of a marker node.
type markerData struct {
	Marker Marker
	// Name is the file name, for the label.
	Name string
}

// refresh makes the nodes of the tree from the model. A file that shows up is expanded, as VS Code
// expands the tree it fills; a file the user collapsed stays collapsed while it has markers.
func (v *View) refresh() {
	var items []kit.TreeItem
	present := map[string]bool{}
	for _, r := range v.model.Resources() {
		key := resourceKey(r.Resource)
		present[key] = true
		item := kit.TreeItem{Key: key, Data: r, Label: r.Name}
		for i, m := range r.Markers {
			item.Children = append(item.Children, kit.TreeItem{Key: markerKey(r.Resource, i), Data: markerData{Marker: m, Name: r.Name}, Label: m.Message})
		}
		items = append(items, item)
	}
	if err := v.tree.SetItems(items); err != nil {
		// the keys are made from the path and the index, so a duplicate is a mistake here
		v.opts.Logger.Error("problems: cannot show the markers", "err", err)
	}
	for key := range v.seen {
		if !present[key] {
			delete(v.seen, key)
		}
	}
	for key := range present {
		if !v.seen[key] {
			v.seen[key] = true
			v.tree.Expand(key)
		}
	}
	v.syncKeys()
	v.notify()
}

// syncKeys sets the context keys of the filter.
func (v *View) syncKeys() {
	keys := v.opts.Keys
	if keys == nil {
		return
	}
	o := v.model.Options()
	keys.Batch(func() {
		keys.Set("problems.filter.errors", o.Severities.Has(SeverityError))
		keys.Set("problems.filter.warnings", o.Severities.Has(SeverityWarning))
		keys.Set("problems.filter.info", o.Severities.Has(SeverityInfo))
		keys.Set("problems.filter.activeFile", o.ActiveFileOnly)
	})
}

func (v *View) setKey(key string, value bool) {
	if v.opts.Keys != nil {
		v.opts.Keys.Set(key, value)
	}
}

// CollapseAll folds every file of the tree.
func (v *View) CollapseAll() {
	v.tree.CollapseAll()
	v.notify()
}

// FocusFilter gives the focus to the filter box and selects its text.
func (v *View) FocusFilter() {
	v.filter.Focus()
	v.filter.SelectAll()
}

// FocusTree gives the focus to the tree.
func (v *View) FocusTree() { v.treeCtl.Focus() }

// ClearFilterText empties the filter box.
func (v *View) ClearFilterText() { v.filter.SetValue("") }

// Filter is the controller of the filter box.
func (v *View) Filter() *kit.InputBoxController { return v.filter }

// SetActiveFile tells the view which file the active editor shows.
func (v *View) SetActiveFile(path string) { v.model.SetActiveFile(path) }

// open opens a marker as the tree asks.
func (v *View) open(node kit.TreeNode, pinned bool) {
	m, ok := node.Data.(markerData)
	if !ok || v.opts.OnOpen == nil {
		return
	}
	v.opts.OnOpen(m.Marker, pinned)
}

// PositionText is the position of a marker as the panel shows it: "[Ln 3, Col 5]".
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/messages.ts (MARKERS_PANEL_AT_LINE_COL_NUMBER)
func PositionText(m Marker) string {
	return "[Ln " + strconv.Itoa(m.StartLine) + ", Col " + strconv.Itoa(m.StartColumn) + "]"
}

// SourceText is the source and the code of a marker as the panel shows them after the message:
// "source(code)", "source" or "(code)".
func SourceText(m Marker) string {
	switch {
	case m.Source != "" && m.Code != "":
		return m.Source + "(" + m.Code + ")"
	case m.Source != "":
		return m.Source
	case m.Code != "":
		return "(" + m.Code + ")"
	}
	return ""
}

// Body is the body of the panel: the tree of the problems, or the message that says why there are none.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersView.ts and
// markersTreeViewer.ts (ResourceMarkersRenderer, MarkerRenderer)
type Body struct {
	widgets.StatefulWidget
	view *View
}

// NewBody makes the body of the panel for a view.
func NewBody(view *View) *Body {
	return &Body{StatefulWidget: widgets.NewStatefulWidgetBase(), view: view}
}

// CreateState implements widgets.StatefulWidget.
func (b *Body) CreateState() widgets.State { return &bodyState{} }

type bodyState struct {
	widgets.StateOf[*Body]
	stop func()
}

// InitState follows the view.
func (s *bodyState) InitState() { s.follow() }

func (s *bodyState) follow() {
	s.stop = s.Widget().view.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows another view.
func (s *bodyState) DidUpdateWidget(old *Body) {
	if old.view != s.Widget().view {
		s.stop()
		s.follow()
	}
}

// Dispose stops following the view.
func (s *bodyState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *bodyState) Build(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget().view
	colors := kit.ThemeOf(bc)
	if len(v.model.Resources()) == 0 {
		return s.message(bc, colors)
	}
	tree := kit.NewTree(v.tree, kit.TreeOpts{
		Key: TreeKey, Controller: v.treeCtl, Render: v.render,
		OnSelect: func(n kit.TreeNode) { v.open(n, false) },
		OnOpen:   func(n kit.TreeNode) { v.open(n, true) },
	})
	// problemFocus says that a problem has the keyboard focus
	return widgets.NewFocus(tree, widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "Problems",
		OnFocusChange: func(has bool) { v.setKey("problemFocus", has) },
	})
}

// message is what stands in place of the tree: no problems, none in the active file, or none that the
// filter lets through (with a link that clears the filter).
func (s *bodyState) message(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	v := s.Widget().view
	style := kit.TextStyleOf(bc, 13, colors.Foreground)
	var children []widgets.Widget
	switch o := v.model.Options(); {
	case o.ActiveFileOnly:
		text := MessageNoProblemsActiveFile
		if v.model.Total() > 0 {
			if _, ok := v.resourceHas(o.ActiveFile); ok {
				text = MessageNoResults
			}
		}
		children = append(children, widgets.NewText(text, widgets.TextOpts{Style: style}))
		if text == MessageNoResults {
			children = append(children, s.clearLink(bc, colors))
		}
	case v.model.Total() > 0:
		children = append(children, widgets.NewText(MessageNoResults, widgets.TextOpts{Style: style}), s.clearLink(bc, colors))
	default:
		children = append(children, widgets.NewText(MessageNoProblems, widgets.TextOpts{Style: style}))
	}
	row := widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	return widgets.NewKeyedSubtree(
		widgets.NewAlign(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 0, 8, 0), widgets.NewSizedBox(row, widgets.SizedBoxOpts{Height: foundation.Some(kit.DefaultTreeRowHeight)})),
			widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}),
		widgets.WidgetOpts{Key: MessageKey})
}

// resourceHas tells whether the service has markers for a file, whatever the filter.
func (v *View) resourceHas(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	n := len(v.model.svc.Read(ReadFilter{Resource: path, Severities: All}))
	return n, n > 0
}

// clearLink is the "Clear Filters" link after the message.
func (s *bodyState) clearLink(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	v := s.Widget().view
	link := widgets.NewText("Clear Filters", widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.TextLinkForeground)})
	return widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
		widgets.NewKeyedSubtree(
			widgets.NewMouseRegion(
				widgets.NewGestureDetector(link, widgets.GestureDetectorOpts{
					Behavior: rendering.HitTestBehaviorOpaque,
					OnTap: func() {
						v.ClearFilterText()
						v.model.ClearFilters()
					},
				}),
				widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}),
			widgets.WidgetOpts{Key: ClearFiltersKey}),
		widgets.NewText(".", widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.Foreground)}),
	}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin})
}

// render builds the contents of a row of the tree.
func (v *View) render(bc widgets.BuildContext, n kit.TreeNode, st kit.TreeRowState) widgets.Widget {
	colors := kit.ThemeOf(bc)
	switch d := n.Data.(type) {
	case ResourceMarkers:
		return v.renderResource(bc, colors, d, st)
	case markerData:
		return v.renderMarker(bc, colors, d.Marker, st)
	}
	return kit.NewHighlightedLabel(bc, n.Label, st)
}

// renderResource is the row of a file: its icon, name and folder, and the count of its markers.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersTreeViewer.ts (ResourceMarkersRenderer)
func (v *View) renderResource(bc widgets.BuildContext, colors kit.Colors, r ResourceMarkers, st kit.TreeRowState) widgets.Widget {
	dim := st.Foreground.WithAlpha(0xA0)
	children := []widgets.Widget{
		// Icons of the file icon theme come with step 6.14; the file is a plain document until then.
		widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconFileText), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(st.Foreground)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
		kit.NewHighlightedLabel(bc, r.Name, st),
	}
	if r.Dir != "" {
		children = append(children,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			widgets.NewFlexible(widgets.NewText(r.Dir, widgets.TextOpts{Style: kit.TextStyleOf(bc, 12, dim), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
	}
	children = append(children,
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(10.0)}),
		kit.NewBadge(len(r.Markers)))
	return widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// severityIcon is the Codicon and the color of the icon of a severity.
func severityIcon(colors kit.Colors, s Severity) (int, ui.Color) {
	switch s {
	case SeverityError:
		return fonts.CodiconError, colors.ProblemsErrorIconForeground
	case SeverityWarning:
		return fonts.CodiconWarning, colors.ProblemsWarningIconForeground
	}
	return fonts.CodiconInfo, colors.ProblemsInfoIconForeground
}

// renderMarker is the row of a marker: the icon of its severity, its message, the source and the code,
// and its position.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersTreeViewer.ts (MarkerRenderer)
func (v *View) renderMarker(bc widgets.BuildContext, colors kit.Colors, m Marker, st kit.TreeRowState) widgets.Widget {
	code, color := severityIcon(colors, m.Severity)
	dim := st.Foreground.WithAlpha(0xA0)
	// the message is on one line; a multi-line one shows its first line, as the single line mode does
	message, _, _ := strings.Cut(m.Message, "\n")
	children := []widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
		widgets.NewFlexible(kit.NewHighlightedLabel(bc, message, st)),
	}
	if text := SourceText(m); text != "" {
		children = append(children,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			widgets.NewText(text, widgets.TextOpts{Style: kit.TextStyleOf(bc, 12, dim), MaxLines: foundation.Some(1)}))
	}
	children = append(children,
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
		widgets.NewText(PositionText(m), widgets.TextOpts{Style: kit.TextStyleOf(bc, 12, dim), MaxLines: foundation.Some(1)}))
	return widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// Actions is what the panel shows in its title strip while the Problems are the tab: the filter box,
// the switches for errors, warnings, infos and the active file, and the button that collapses the tree.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersViewActions.ts (MarkersFilterActionViewItem)
type Actions struct {
	widgets.StatefulWidget
	view *View
}

// NewActions makes the actions of the panel for a view.
func NewActions(view *View) *Actions {
	return &Actions{StatefulWidget: widgets.NewStatefulWidgetBase(), view: view}
}

// CreateState implements widgets.StatefulWidget.
func (a *Actions) CreateState() widgets.State { return &actionsState{} }

type actionsState struct {
	widgets.StateOf[*Actions]
	stop func()
}

// InitState follows the view.
func (s *actionsState) InitState() {
	s.stop = s.Widget().view.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// Dispose stops following the view.
func (s *actionsState) Dispose() { s.stop() }

// Widths of the actions: the filter box at most filterWidth and at least filterMinWidth wide, and
// what the buttons after it take (four switches of 22 px, the 2 px before the collapse button and the
// button, and the 6 px between the box and the switches).
const (
	filterWidth    = 240.0
	filterMinWidth = 80.0
	buttonsWidth   = 4*22.0 + 2 + 22 + 6
)

// Build implements widgets.State.
func (s *actionsState) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, cons rendering.BoxConstraints) widgets.Widget {
		return s.actions(cons.MaxWidth)
	})
}

// actions are the actions in the room there is: with little room the filter box shrinks, and under
// its least width it is left out.
func (s *actionsState) actions(room float64) widgets.Widget {
	v := s.Widget().view
	o := v.model.Options()
	toggle := func(key foundation.Key, code int, tip string, on bool, flip func()) widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 1), kit.NewToggle(code, kit.ToggleOpts{
			Key: key, Checked: on, Tooltip: tip, NotFocusable: true, OnChanged: func(bool) { flip() },
		}))
	}
	var children []widgets.Widget
	if width := min(room-buttonsWidth, filterWidth); width >= filterMinWidth {
		children = append(children,
			widgets.NewSizedBox(
				kit.NewInputBox(nil, nil, kit.InputBoxOpts{
					Key: FilterKey, Controller: v.filter, Placeholder: FilterPlaceholder,
					OnChanged:     v.model.SetText,
					OnFocusChange: func(has bool) { v.setKey("problemsFilterFocus", has) },
				}),
				widgets.SizedBoxOpts{Width: foundation.Some(width)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
	}
	children = append(children,
		toggle(ToggleKey(SeverityError), fonts.CodiconError, "Show Errors", o.Severities.Has(SeverityError), func() { v.model.ToggleSeverity(SeverityError) }),
		toggle(ToggleKey(SeverityWarning), fonts.CodiconWarning, "Show Warnings", o.Severities.Has(SeverityWarning), func() { v.model.ToggleSeverity(SeverityWarning) }),
		toggle(ToggleKey(SeverityInfo), fonts.CodiconInfo, "Show Infos", o.Severities.Has(SeverityInfo), func() { v.model.ToggleSeverity(SeverityInfo) }),
		toggle(ActiveFileKey, fonts.CodiconGoToFile, "Show Active File Only", o.ActiveFileOnly, v.model.ToggleActiveFileOnly),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)}),
		kit.NewIconButton(fonts.CodiconCollapseAll, kit.IconButtonOpts{Key: CollapseAllKey, Tooltip: "Collapse All", OnPressed: v.CollapseAll}))
	return widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
