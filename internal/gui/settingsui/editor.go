package settingsui

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of the settings editor, from VS Code's settingsEditor2.css.
const (
	headerTop        = 14.0
	headerSide       = 24.0
	tocWidth         = 220.0
	tocMinWidth      = 120.0
	tocMaxWidth      = 400.0
	groupTitleSide   = 15.0
	groupTitlePad    = 10.0
	searchPlaceholer = "Search settings"
	// UserTabLabel and WorkspaceTabLabel are the texts of the tabs of the targets.
	UserTabLabel      = "User"
	WorkspaceTabLabel = "Workspace"
	// NoWorkspaceTooltip is the tooltip of the Workspace tab while no folder is open.
	NoWorkspaceTooltip = "Open a folder to edit the settings of the workspace."
	// OpenJSONTooltip is the tooltip of the button that opens the file of the tab.
	OpenJSONTooltip = "Open Settings (JSON)"
	// NoResultsText is what the list says while nothing matches the search.
	NoResultsText = "No Settings Found."
	// LanguageHeaderPrefix is the text before the name of the language in the title of the list of @lang:.
	LanguageHeaderPrefix = "Language: "
	// ClearSearchTooltip is the tooltip of the button that clears the search box.
	ClearSearchTooltip = "Clear Search Results"
)

// Options are the options of NewEditor.
type Options struct {
	// Model is the model of the settings over the store the editor reads and writes (core/settings).
	Model *settings.UIModel
	// Context is the context of the writes.
	Context context.Context
	// State is the view the editor starts with: the tab, the group of the tree that is selected, how far the
	// list is scrolled and the text of the search.
	State editorgroup.SettingsViewState
	// OnState is called when the user changes the view (the tab, the group, the scroll position, the search); the
	// owner keeps it with the input of the editor (SPEC §10.10).
	OnState func(editorgroup.SettingsViewState)
	// HasFolder says that a folder is open: the Workspace tab is enabled only then.
	HasFolder bool
	// OnOpenJSON opens the settings.json of a target in a text editor, at the key of a setting when it is not
	// empty. The button of the header and the "Edit in settings.json" link call it.
	OnOpenJSON func(target settings.Target, key string)
	// Languages finds the language of the filter @lang: by its id or name; without it the text after @lang: is the
	// id of a language that exists.
	Languages settings.LanguageResolver
	// OnError is told of a write that failed (the file has errors, cannot be written); the owner shows it.
	OnError func(err error)
	// CopyText puts a text on the clipboard ("Copy Setting ID").
	CopyText func(text string)
	// CodeFont is the family of the code spans of the descriptions; the desktop's monospace font by default.
	CodeFont string
	// Controller lets code drive the editor from outside.
	Controller *Controller
	// Autofocus gives the search box the focus when the editor is shown.
	Autofocus bool
}

// Controller drives a settings editor from outside: the commands that open the editor with a search, and tests.
// It belongs to one editor.
type Controller struct{ state *editorState }

// NewController makes a controller that is not attached to an editor yet; its methods do nothing until it is.
func NewController() *Controller { return &Controller{} }

// ViewState is the view of the editor now.
func (c *Controller) ViewState() editorgroup.SettingsViewState {
	if c.state == nil {
		return editorgroup.SettingsViewState{}
	}
	return c.state.viewState()
}

// SetTarget switches the tab.
func (c *Controller) SetTarget(t editorgroup.SettingsTarget) {
	if c.state != nil {
		c.state.setTarget(targetOf(t))
	}
}

// SetQuery puts a text in the search box and filters the list by it.
func (c *Controller) SetQuery(q string) {
	if c.state != nil {
		c.state.search.SetValue(q)
	}
}

// SelectCategory selects a group of the tree and scrolls the list to it.
func (c *Controller) SelectCategory(id string) {
	if c.state != nil {
		c.state.selectCategory(id)
	}
}

// SearchInput is the text and the focus node of the search box, nil while the editor is not built; with SearchTyped it is what
// kit.StartTypeAheadLater needs to type the keys that came before the box was there.
func (c *Controller) SearchInput() (*widgets.TextEditingController, *widgets.FocusNode) {
	if c.state == nil {
		return nil, nil
	}
	return c.state.search.TextController(), c.state.search.FocusNode()
}

// SearchTyped is told that the text of the search box was changed other than by typing in it (a type-ahead put keys there):
// the list is filtered by it, as it is by the keys the box takes itself.
func (c *Controller) SearchTyped(text string) {
	if c.state != nil {
		c.state.queryChanged(text)
	}
}

// FocusSearch gives the search box the focus.
func (c *Controller) FocusSearch() {
	if c.state != nil {
		c.state.search.Focus()
	}
}

func targetOf(t editorgroup.SettingsTarget) settings.Target {
	if t == editorgroup.SettingsWorkspace {
		return settings.TargetWorkspace
	}
	return settings.TargetUser
}

func targetName(t settings.Target) editorgroup.SettingsTarget {
	if t == settings.TargetWorkspace {
		return editorgroup.SettingsWorkspace
	}
	return editorgroup.SettingsUser
}

// RowKey is the key of the widget of a setting in a group of the list; tests find rows by it.
func RowKey(categoryID, key string) foundation.Key {
	return foundation.NewValueKey("settingsui-row:" + categoryID + "|" + key)
}

// groupKey is the key of the header of a group of the list.
func groupKey(id string) foundation.Key { return foundation.NewValueKey("settingsui-group:" + id) }

// Editor is the settings editor (SPEC §11.1): a search box, the tabs of the user's and the workspace's
// settings and the button that opens the JSON file over a tree of the groups of settings and the list of the
// settings of each group, whose rows are as high as their text and control are. A change of the settings by
// anything else, such as typing in settings.json, shows in the rows at once.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/settingsEditor2.ts and settingsTree.ts
type Editor struct {
	widgets.StatefulWidget
	opts Options
}

// NewEditor makes a settings editor.
func NewEditor(opts Options) *Editor {
	return &Editor{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (e *Editor) CreateState() widgets.State { return &editorState{} }

type editorState struct {
	widgets.StateOf[*Editor]
	target   settings.Target
	category string
	query    string
	search   *kit.InputBoxController
	scroll   kit.ScrollHandle
	content  kit.Bounds
	headers  map[string]*kit.Bounds
	tree     *kit.TreeModel
	treeCtl  *kit.TreeController
	// result is the search of the query; cats are its groups, the ones the list shows.
	result    settings.SearchResult
	cats      []*settings.Category
	treeSig   string
	stop      func()
	scrollTop float64
	// restore counts the frames left to put the view back as it was (the list is not laid out before the
	// first frames); programmatic is set while the editor moves the list itself, so that the move is not
	// taken for the user's.
	restore      int
	programmatic bool
	syncQueued   bool
}

// InitState reads the view state, makes the search box and the tree and follows the settings.
func (s *editorState) InitState() {
	w := s.Widget()
	st := w.opts.State
	s.target = targetOf(st.Target)
	if s.target == settings.TargetWorkspace && !w.opts.HasFolder {
		s.target = settings.TargetUser
	}
	s.category, s.query, s.scrollTop = st.Category, st.Query, st.Scroll
	s.search = kit.NewInputBoxController(st.Query)
	s.headers = map[string]*kit.Bounds{}
	s.tree = kit.NewTreeModel(kit.TreeModelOpts{})
	s.treeCtl = kit.NewTreeController()
	if w.opts.Controller != nil {
		w.opts.Controller.state = s
	}
	// Escape in the search box clears it
	s.search.FocusNode().SetOnKeyEvent(func(_ *widgets.FocusNode, ev services.KeyEvent) widgets.KeyEventResult {
		if _, down := ev.(*services.KeyDownEvent); !down || ev.LogicalKey() != services.LogicalKeyboardKeyEscape || s.search.Value() == "" {
			return widgets.KeyEventResultIgnored
		}
		s.search.SetValue("")
		return widgets.KeyEventResultHandled
	})
	binding := widgets.WidgetsBindingOf(s.Context())
	s.stop = w.opts.Model.Store().OnChange(func(settings.ChangeEvent) {
		binding.Post(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	})
	if s.scrollTop > 0 || s.category != "" {
		s.restore = 3
	}
}

// DidUpdateWidget follows a folder that goes away.
func (s *editorState) DidUpdateWidget(old *Editor) {
	w := s.Widget()
	if w.opts.Controller != old.opts.Controller && w.opts.Controller != nil {
		w.opts.Controller.state = s
	}
	if s.target == settings.TargetWorkspace && !w.opts.HasFolder {
		s.target = settings.TargetUser
		s.noteState()
	}
}

// Dispose stops following the settings.
func (s *editorState) Dispose() {
	if s.stop != nil {
		s.stop()
	}
	if c := s.Widget().opts.Controller; c != nil && c.state == s {
		c.state = nil
	}
	s.search.Dispose()
}

func (s *editorState) viewState() editorgroup.SettingsViewState {
	return editorgroup.SettingsViewState{Target: targetName(s.target), Category: s.category, Scroll: s.scrollTop, Query: s.query}
}

// noteState tells the owner that the view changed.
func (s *editorState) noteState() {
	if f := s.Widget().opts.OnState; f != nil {
		f(s.viewState())
	}
}

func (s *editorState) setTarget(t settings.Target) {
	if t == settings.TargetWorkspace && !s.Widget().opts.HasFolder {
		return
	}
	if t == s.target {
		return
	}
	s.SetState(func() { s.target, s.scrollTop = t, 0 })
	s.programmatic = true
	s.scroll.ScrollTo(0, 0)
	s.afterFrame(func() { s.programmatic = false })
	s.noteState()
}

func (s *editorState) queryChanged(text string) {
	if text == s.query {
		return
	}
	s.SetState(func() { s.query, s.scrollTop = text, 0 })
	s.programmatic = true
	s.scroll.ScrollTo(0, 0)
	s.afterFrame(func() { s.programmatic = false })
	s.noteState()
}

// afterFrame runs f after the next frame.
func (s *editorState) afterFrame(f func()) {
	sched := widgets.WidgetsBindingOf(s.Context()).Scheduler()
	sched.ScheduleFrame()
	sched.AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() {
			f()
		}
	})
}

// runSearch finds the settings the target shows for the text of the search box (core/settings, Search).
func (s *editorState) runSearch() settings.SearchResult {
	return s.Widget().opts.Model.Search(s.target, s.query, s.Widget().opts.Languages)
}

// language is the language whose block the rows show and edit (@lang:), or "".
func (s *editorState) language() string { return s.result.Language }

// ---- tree ----

// treeItems are the groups as the items of the tree; while a search shows (counts), the label of a group has the
// number of settings found in it and below it, as VS Code's "Font (3)".
func treeItems(cats []*settings.Category, counts bool) []kit.TreeItem {
	out := make([]kit.TreeItem, len(cats))
	for i, c := range cats {
		out[i] = kit.TreeItem{Key: c.ID, Data: c, Label: treeLabel(c, counts), Children: treeItems(c.Children, counts)}
	}
	return out
}

// treeLabel is the text of a group in the tree.
func treeLabel(c *settings.Category, counts bool) string {
	if !counts {
		return c.Label
	}
	return c.Label + " (" + strconv.Itoa(c.Count()) + ")"
}

func categorySig(cats []*settings.Category, counts bool) string {
	var b strings.Builder
	for _, c := range cats {
		c.Walk(func(c *settings.Category) { b.WriteString(treeLabel(c, counts) + "|" + c.ID + ",") })
	}
	return b.String()
}

// syncTree gives the tree the groups the list shows, all expanded, and selects the current group.
func (s *editorState) syncTree() {
	counts := s.result.Active
	if sig := categorySig(s.cats, counts); sig != s.treeSig {
		s.treeSig = sig
		if err := s.tree.SetItems(treeItems(s.cats, counts)); err == nil {
			s.tree.ExpandAll()
		}
	}
	if s.category == "" || !s.tree.Has(s.category) {
		if len(s.cats) > 0 {
			s.tree.SetSelected(s.cats[0].ID)
		}
		return
	}
	s.tree.SetSelected(s.category)
}

// selectCategory is a click on a group of the tree: the list scrolls to its header.
func (s *editorState) selectCategory(id string) {
	s.category = id
	s.tree.SetSelected(id)
	if off, ok := s.headerOffset(id); ok {
		s.programmatic = true
		s.scroll.ScrollTo(0, off)
		s.afterFrame(func() { s.programmatic = false })
		s.scrollTop = off
	}
	s.noteState()
}

// headerOffset is how far the header of a group is from the top of the list.
func (s *editorState) headerOffset(id string) (float64, bool) {
	hb := s.headers[id]
	if hb == nil {
		return 0, false
	}
	h, ok1 := hb.Rect()
	c, ok2 := s.content.Rect()
	if !ok1 || !ok2 {
		return 0, false
	}
	return h.Top - c.Top, true
}

// scrolled follows the list: the group at the top of the view is the selected one of the tree.
func (s *editorState) scrolled() {
	if s.programmatic {
		return
	}
	_, top := s.scroll.Position()
	s.scrollTop = top
	if s.syncQueued {
		return
	}
	s.syncQueued = true
	s.afterFrame(func() {
		s.syncQueued = false
		if s.programmatic {
			return
		}
		active := ""
		s.eachCategory(func(c *settings.Category) {
			if off, ok := s.headerOffset(c.ID); ok && off <= s.scrollTop+1 {
				active = c.ID
			}
		})
		if active != "" && active != s.category {
			s.category = active
			s.tree.SetSelected(active)
			s.treeCtl.Reveal(active)
		}
		s.noteState()
	})
}

func (s *editorState) eachCategory(f func(*settings.Category)) {
	for _, c := range s.cats {
		c.Walk(f)
	}
}

// ---- rowHost ----

func (s *editorState) fail(err error) {
	if f := s.Widget().opts.OnError; f != nil {
		f(err)
	}
}

func (s *editorState) set(key string, value any) {
	w := s.Widget()
	if err := w.opts.Model.SetValueFor(w.opts.Context, key, s.target, s.language(), value); err != nil {
		s.fail(err)
	}
	s.SetState(func() {})
}

func (s *editorState) setText(key, text string) {
	w := s.Widget()
	if err := w.opts.Model.SetTextFor(w.opts.Context, key, s.target, s.language(), text); err != nil {
		s.fail(err)
	}
	s.SetState(func() {})
}

func (s *editorState) reset(key string) {
	w := s.Widget()
	if err := w.opts.Model.ResetFor(w.opts.Context, key, s.target, s.language()); err != nil {
		s.fail(err)
	}
	s.SetState(func() {})
}

func (s *editorState) openJSON(key string) {
	if f := s.Widget().opts.OnOpenJSON; f != nil {
		f(s.target, key)
	}
}

func (s *editorState) copyText(text string) {
	if f := s.Widget().opts.CopyText; f != nil {
		f(text)
	}
}

func (s *editorState) settingJSON(row settings.Row) string {
	data, err := json.Marshal(row.Value)
	if err != nil {
		data = []byte("null")
	}
	return `"` + row.Key + `": ` + string(data)
}

func (s *editorState) codeFont() string {
	if f := s.Widget().opts.CodeFont; f != "" {
		return f
	}
	return "monospace"
}

// ---- build ----

// Build implements widgets.State.
func (s *editorState) Build(bc widgets.BuildContext) widgets.Widget {
	colors := kit.ThemeOf(bc)
	s.result = s.runSearch()
	s.cats = s.result.Categories
	s.syncTree()
	if s.restore > 0 {
		s.restoreView()
	}
	split := kit.NewSplitView([]kit.SplitPane{
		{Child: s.tocPane(colors), Size: tocWidth, MinSize: tocMinWidth, MaxSize: tocMaxWidth},
		{Child: s.listPane(bc, colors), Priority: kit.LayoutHigh},
	}, kit.SplitViewOpts{Orientation: kit.SplitHorizontal, SeparatorBorder: colors.SettingsSashBorder})
	column := widgets.NewColumn([]widgets.Widget{s.header(bc, colors), widgets.NewExpanded(split)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return widgets.NewColoredBox(colors.EditorBackground, column)
}

// restoreView puts the list back where the saved view had it, once the list is laid out: to the saved position,
// else to the header of the saved group.
func (s *editorState) restoreView() {
	s.restore--
	want, category := s.scrollTop, s.category
	s.afterFrame(func() {
		s.programmatic = true
		if want > 0 {
			s.scroll.ScrollTo(0, want)
		} else if off, ok := s.headerOffset(category); ok && category != "" {
			s.scroll.ScrollTo(0, off)
		}
		s.afterFrame(func() { s.programmatic = false })
	})
}

// header is the search box over the tabs and the button that opens the JSON file, with a line under it.
func (s *editorState) header(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	var trailing []widgets.Widget
	if s.query != "" {
		trailing = append(trailing, kit.NewIconButton(fonts.CodiconClearAll, kit.IconButtonOpts{
			Tooltip: ClearSearchTooltip, OnPressed: func() { s.search.SetValue("") },
		}))
	}
	search := kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Controller: s.search, Placeholder: searchPlaceholer, OnChanged: s.queryChanged, Autofocus: w.opts.Autofocus,
		Trailing: trailing,
	})
	tabs := tabStrip(bc, colors, []tabButton{
		{label: UserTabLabel, checked: s.target == settings.TargetUser, onTap: func() { s.setTarget(settings.TargetUser) }},
		{label: WorkspaceTabLabel, checked: s.target == settings.TargetWorkspace, disabled: !w.opts.HasFolder,
			tooltip: noWorkspaceTooltip(w.opts.HasFolder),
			onTap:   func() { s.setTarget(settings.TargetWorkspace) }},
	})
	open := kit.NewIconButton(fonts.CodiconGoToFile, kit.IconButtonOpts{
		Tooltip: OpenJSONTooltip, OnPressed: func() { s.openJSON("") },
	})
	controls := widgets.NewContainer(
		widgets.NewRow([]widgets.Widget{tabs, widgets.NewSpacer(), open}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.ContainerOpts{
			Margin: painting.NewEdgeInsetsFromLTRB(0, 10, 0, 0),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.SettingsHeaderBorder), Width: foundation.Some(1.0)})},
			}),
		})
	parts := []widgets.Widget{search}
	if s.result.Active {
		parts = append(parts, s.resultsNote(bc, colors))
	}
	parts = append(parts, controls)
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(headerSide, headerTop, headerSide, 0),
		widgets.NewColumn(parts, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}

// resultsNote is the line under the search box: how many settings were found ("3 Settings Found"), and a line for
// each filter that was ignored or found nothing.
func (s *editorState) resultsNote(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	lines := []widgets.Widget{textOf(bc, s.result.Summary(), descriptionFontSize, colors.Foreground, 0)}
	for _, hint := range s.result.Hints {
		lines = append(lines, textOf(bc, hint, descriptionFontSize, colors.Foreground.WithOpacity(0.8), 0))
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(2, 6, 0, 0),
		widgets.NewColumn(lines, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin}))
}

// noWorkspaceTooltip is the tooltip of the Workspace tab: the reason it is disabled.
func noWorkspaceTooltip(hasFolder bool) string {
	if hasFolder {
		return ""
	}
	return NoWorkspaceTooltip
}

// tocPane is the tree of the groups.
func (s *editorState) tocPane(colors kit.Colors) widgets.Widget {
	tree := kit.NewTree(s.tree, kit.TreeOpts{
		Controller: s.treeCtl, ToggleOnClick: foundation.Some(false), TypeToFind: foundation.Some(false),
		Render: func(bc widgets.BuildContext, n kit.TreeNode, st kit.TreeRowState) widgets.Widget {
			return kit.NewHighlightedLabel(bc, n.Label, st)
		},
		OnSelect: func(n kit.TreeNode) { s.selectCategory(n.Key) },
	})
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 8, 0, 0), tree)
}

// listPane is the headers of the groups and the rows of their settings, as high as they are, scrolled
// with the wheel and the scrollbar.
func (s *editorState) listPane(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	var children []widgets.Widget
	if s.result.Language != "" && len(s.cats) > 0 {
		children = append(children, s.languageHeader(bc, colors))
	}
	s.eachCategory(func(c *settings.Category) {
		children = append(children, s.groupHeader(bc, colors, c))
		for _, key := range c.Keys {
			row, ok := s.Widget().opts.Model.RowFor(key, s.target, s.result.Language, c.ID)
			if !ok {
				continue
			}
			children = append(children, newSettingRow(RowKey(c.ID, key), row, s))
		}
	})
	if len(children) == 0 {
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(headerSide, 20, headerSide, 0), textOf(bc, NoResultsText, descriptionFontSize, colors.Foreground, 0))
	}
	column := widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	scrolled := kit.NewScrollableElement(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 24), kit.NewBoundsProbe(column, &s.content)),
		kit.ScrollableElementOpts{Handle: &s.scroll})
	return widgets.NewNotificationListener(func(widgets.ScrollNotification) bool {
		s.scrolled()
		return false
	}, scrolled)
}

// languageHeader is the title of the list while it shows the settings of a language (@lang:): the rows show and edit
// the "[go]" block of the file of the tab.
func (s *editorState) languageHeader(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	id := s.result.Language
	title := textOf(bc, LanguageHeaderPrefix+s.result.LanguageName, 22, colors.SettingsHeaderForeground, ui.FontWeightW600)
	note := textOf(bc, "The settings below are those of the "+settings.OverrideKey(id)+" block of the file.", descriptionFontSize, colors.Foreground.WithOpacity(0.9), 0)
	return widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(groupTitleSide, groupTitlePad, groupTitlePad, 0),
		widgets.NewColumn([]widgets.Widget{title, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(4.0)}), note},
			widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin})),
		widgets.WidgetOpts{Key: foundation.NewValueKey("settingsui-language")})
}

// groupHeader is the title of a group: 26 px for a group of the first level, 22 px for the second, 18 px below.
func (s *editorState) groupHeader(bc widgets.BuildContext, colors kit.Colors, c *settings.Category) widgets.Widget {
	size := map[int]float64{1: 26, 2: 22}[c.Level]
	if size == 0 {
		size = 18
	}
	b := s.headers[c.ID]
	if b == nil {
		b = &kit.Bounds{}
		s.headers[c.ID] = b
	}
	title := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(groupTitleSide, groupTitlePad, groupTitlePad, groupTitlePad),
		textOf(bc, c.Label, size, colors.SettingsHeaderForeground, ui.FontWeightW600))
	return widgets.NewKeyedSubtree(kit.NewBoundsProbe(title, b), widgets.WidgetOpts{Key: groupKey(c.ID)})
}
