package keybindingsui

import (
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The texts of the keybindings editor.
const (
	// SearchPlaceholder is shown in the empty search box.
	SearchPlaceholder = "Type to search in keybindings"
	// RecordingPlaceholder is shown in the empty search box while it records keys.
	RecordingPlaceholder = "Recording Keys. Press Escape to exit"
	// RecordingBadge is the text of the badge that shows while the search box records keys.
	RecordingBadge = "Recording Keys"
	// RecordKeysTooltip is the tooltip of the button that makes the search box record keys.
	RecordKeysTooltip = "Record Keys"
	// ClearSearchTooltip is the tooltip of the button that clears the search box.
	ClearSearchTooltip = "Clear Keybindings Search Input"
	// OpenJSONTooltip is the tooltip of the button that opens keybindings.json.
	OpenJSONTooltip = "Open Keyboard Shortcuts (JSON)"
)

// The metrics of the table, from VS Code's keybindingsEditor.css and the delegate of the table.
const (
	headerHeight  = 30.0
	rowHeight     = 24.0
	rowHeightTall = 40.0
	actionsWidth  = 40.0
	cellPadding   = 10.0
	fontSize      = 13.0
	// the weights of the columns Command, Keybinding, When and Source
	commandFlex, keybindingFlex, whenFlex, sourceFlex = 30, 20, 35, 15
)

// Actions are what the editor asks its owner to do to keybindings.json. Each makes the edit and reads the file
// again, so that the model the editor shows follows; the editor shows the error of one that fails.
type Actions struct {
	// Define gives the row the keybinding key (a user-settings text such as "ctrl+k ctrl+c") under the when-clause
	// of the row: a change of the row, or with add a keybinding more for its command. The rows in overwrite are
	// taken away first (the conflicts the user chose to overwrite).
	Define func(row keybinding.Row, add bool, key string, overwrite []keybinding.Row) error
	// Remove takes the keybinding of the row away.
	Remove func(row keybinding.Row) error
	// Reset gives the command of the row its built-in keybindings back.
	Reset func(row keybinding.Row) error
	// SetWhen gives the row another when-clause ("" for none).
	SetWhen func(row keybinding.Row, when string) error
}

// Options are the options of NewEditor.
type Options struct {
	// Model gives the rows of the table: the current model, the same one until the keybindings change.
	Model func() *keybinding.Model
	// Subscribe makes the editor told (on the UI thread) when the model changes, and gives the function that
	// stops it.
	Subscribe func(changed func()) (stop func())
	// Actions edit keybindings.json.
	Actions Actions
	// State is the view the editor starts with.
	State editorgroup.KeybindingsViewState
	// OnState is called when the user changes the view; the owner keeps it with the input (SPEC §10.10).
	OnState func(editorgroup.KeybindingsViewState)
	// OnOpenJSON opens keybindings.json in a text editor.
	OnOpenJSON func()
	// OnError is told of an edit that failed; the owner shows it.
	OnError func(err error)
	// CopyText puts a text on the clipboard.
	CopyText func(text string)
	// Record starts the recording mode of the dispatcher with the recorder (every key press goes to it and
	// nothing is resolved) and gives the function that ends it. Without it the editor cannot record keys.
	Record func(r keybinding.Recorder) (stop func())
	// KeyLabel gives the label of the keybinding of a command ("Enter"), for the tooltips and the menu; may be
	// nil.
	KeyLabel func(command string) string
	// Dialogs shows the dialog of the conflicts of a keybinding; without it a keybinding with conflicts is
	// added next to them.
	Dialogs *kit.DialogController
	// Keys is the scope of the context keys of the window, and Scopes the registry of the scopes of the focus
	// nodes: the editor sets inKeybindings, inKeybindingsSearch, keybindingsSearchHasValue, keybindingFocus and
	// whenFocus in scopes of its own. Both may be nil.
	Keys   *contextkey.Scope
	Scopes *kit.FocusScopes
	// CodeFont is the family of the ids and when-clauses; the desktop's monospace font by default.
	CodeFont string
	// Controller lets code drive the editor from outside.
	Controller *Controller
	// Autofocus gives the search box the focus when the editor is shown.
	Autofocus bool
}

// Editor is the keybindings editor (SPEC §11.3): a search box over a table of the keybindings of every command
// (Command, Keybinding, When, Source), whose rows can be changed, added to, removed and reset; the keys of a
// keybinding are recorded by the dispatcher.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/keybindingsEditor.ts
type Editor struct {
	widgets.StatefulWidget
	opts Options
}

// NewEditor makes a keybindings editor.
func NewEditor(opts Options) *Editor {
	return &Editor{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (e *Editor) CreateState() widgets.State { return &editorState{} }

// RowKey is the key of the widget of a row of the table; tests find rows by it.
func RowKey(id string) foundation.Key { return foundation.NewValueKey("keybindingsui-row:" + id) }

type editorState struct {
	widgets.StateOf[*Editor]

	search *kit.InputBoxController
	query  string
	// recording is set while the search box records keys.
	recording bool
	searchRec *Recorder
	stopRec   func()

	model      *keybinding.Model
	fetchedFor string
	results    []keybinding.Result
	byKey      map[string]int
	list       *kit.ListViewModel
	listCtl    *kit.ListController
	menu       *kit.ContextMenuController

	// reselect is the row the table selects after the next change of the model.
	reselect *rowRef
	// restoredRow is the row of the saved view, selected once the rows are there.
	restoredRow *rowRef

	define *defineState

	// whenRow is the id of the row whose when-clause is edited, and whenInput the box that edits it.
	whenRow   string
	whenInput *kit.InputBoxController

	paneNode, listNode *widgets.FocusNode
	paneScope          *contextkey.Scope
	searchScope        *contextkey.Scope
	listScope          *contextkey.Scope
	whenScope          *contextkey.Scope
	unregister         []func()
	stopSub            func()
}

// rowRef finds a row again after the model changed: the command and the keybinding.
type rowRef struct{ command, key string }

// InitState reads the view state, makes the search box, the table and the scopes, and follows the model.
func (s *editorState) InitState() {
	w := s.Widget()
	s.query = w.opts.State.Query
	s.search = kit.NewInputBoxController(s.query)
	s.byKey = map[string]int{}
	s.list = kit.NewListViewModel(kit.ListViewModelOpts{HeightOf: s.heightOf, RowHeight: foundation.Some(rowHeight)})
	s.listCtl = kit.NewListController()
	s.menu = kit.NewContextMenuController()
	s.whenInput = kit.NewInputBoxController("")
	// Escape in the box of the when-clause gives the edit up
	s.whenInput.FocusNode().SetOnKeyEvent(func(_ *widgets.FocusNode, ev services.KeyEvent) widgets.KeyEventResult {
		if _, down := ev.(*services.KeyDownEvent); !down || ev.LogicalKey() != services.LogicalKeyboardKeyEscape {
			return widgets.KeyEventResultIgnored
		}
		s.closeWhen()
		return widgets.KeyEventResultHandled
	})
	if st := w.opts.State; st.Command != "" {
		s.restoredRow = &rowRef{command: st.Command, key: st.Key}
	}
	if w.opts.Controller != nil {
		w.opts.Controller.state = s
	}
	s.paneNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "KeybindingsEditor", SkipTraversal: true})
	s.paneNode.SetCanRequestFocus(false)
	s.listNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "KeybindingsTable", SkipTraversal: true})
	s.listNode.SetCanRequestFocus(false)
	s.makeScopes()
	if w.opts.Subscribe != nil {
		s.stopSub = w.opts.Subscribe(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	}
}

// makeScopes makes the scopes of the context keys of the editor and attaches them to the focus nodes.
func (s *editorState) makeScopes() {
	w := s.Widget()
	if w.opts.Keys == nil || w.opts.Scopes == nil {
		return
	}
	s.paneScope = w.opts.Keys.Child()
	s.paneScope.Set("inKeybindings", true)
	s.searchScope = s.paneScope.Child()
	s.searchScope.Set("inKeybindingsSearch", true)
	s.searchScope.Set("inputFocus", true)
	s.searchScope.Set("keybindingsSearchHasValue", s.query != "")
	s.listScope = s.paneScope.Child()
	s.whenScope = s.paneScope.Child()
	s.whenScope.Set("whenFocus", true)
	s.whenScope.Set("inputFocus", true)
	s.unregister = []func(){
		w.opts.Scopes.Register(s.paneNode, s.paneScope),
		w.opts.Scopes.Register(s.search.FocusNode(), s.searchScope),
		w.opts.Scopes.Register(s.listNode, s.listScope),
		w.opts.Scopes.Register(s.whenInput.FocusNode(), s.whenScope),
	}
}

// DidUpdateWidget follows a new controller.
func (s *editorState) DidUpdateWidget(old *Editor) {
	w := s.Widget()
	if w.opts.Controller != old.opts.Controller && w.opts.Controller != nil {
		w.opts.Controller.state = s
	}
}

// Dispose ends the recording and lets go of the scopes, the focus nodes and the controllers.
func (s *editorState) Dispose() {
	s.stopRecording()
	if s.define != nil {
		s.define.stopRecording()
		s.define = nil
	}
	if s.stopSub != nil {
		s.stopSub()
	}
	if c := s.Widget().opts.Controller; c != nil && c.state == s {
		c.state = nil
	}
	for _, u := range s.unregister {
		u()
	}
	for _, sc := range []*contextkey.Scope{s.paneScope, s.searchScope, s.listScope, s.whenScope} {
		if sc != nil {
			sc.Dispose()
		}
	}
	s.paneNode.Dispose()
	s.listNode.Dispose()
	s.search.Dispose()
	s.whenInput.Dispose()
}

// ---- state ----

func (s *editorState) viewState() editorgroup.KeybindingsViewState {
	st := editorgroup.KeybindingsViewState{Query: s.query}
	if res, ok := s.activeResult(); ok {
		st.Command, st.Key = res.Row.Command, res.Row.Key
	}
	return st
}

// noteState tells the owner that the view changed.
func (s *editorState) noteState() {
	if f := s.Widget().opts.OnState; f != nil {
		f(s.viewState())
	}
}

// activeResult is the row that has the focus in the table, else the selected one.
func (s *editorState) activeResult() (keybinding.Result, bool) {
	key, ok := s.list.Focused()
	if !ok {
		key, ok = s.list.Selected()
	}
	if !ok {
		return keybinding.Result{}, false
	}
	i, found := s.byKey[key]
	if !found || i >= len(s.results) {
		return keybinding.Result{}, false
	}
	return s.results[i], true
}

// resultFor finds the shown result of a row key.
func (s *editorState) resultFor(key string) (keybinding.Result, bool) {
	i, ok := s.byKey[key]
	if !ok || i >= len(s.results) {
		return keybinding.Result{}, false
	}
	return s.results[i], true
}

// heightOf is the height of the row at an index: two lines when the search matched the id of a command that has
// a title (the id is shown under it), else one.
func (s *editorState) heightOf(i int) float64 {
	if i >= 0 && i < len(s.results) && s.twoLines(s.results[i]) {
		return rowHeightTall
	}
	return rowHeight
}

func (s *editorState) twoLines(r keybinding.Result) bool { return r.Row.Label != "" && r.ID != nil }

// refresh finds the rows of the query in the model and gives them to the table, when either changed.
func (s *editorState) refresh() {
	w := s.Widget()
	var m *keybinding.Model
	if w.opts.Model != nil {
		m = w.opts.Model()
	}
	if m == nil {
		m = keybinding.NewModel(nil, nil, nil)
	}
	if m == s.model && s.query == s.fetchedFor && s.results != nil {
		return
	}
	s.model, s.fetchedFor = m, s.query
	s.results = m.Fetch(s.query)
	if s.results == nil {
		s.results = []keybinding.Result{}
	}
	items := make([]kit.ListViewItem, len(s.results))
	s.byKey = make(map[string]int, len(s.results))
	for i, r := range s.results {
		items[i] = kit.ListViewItem{Key: r.Row.ID, Label: r.Row.Label}
		s.byKey[r.Row.ID] = i
	}
	if err := s.list.SetItems(items); err != nil {
		// the ids of the rows are made of the command, the keys and the when-clause, so a repeat is a row
		// that is the same twice: the model leaves those out
		if f := w.opts.OnError; f != nil {
			f(errors.NewErr("KeybindingsTableInvalid", err))
		}
	}
	switch {
	case s.reselect != nil:
		ref := s.reselect
		s.reselect = nil
		s.selectRef(*ref)
	case s.restoredRow != nil && len(s.results) > 0:
		ref := s.restoredRow
		s.restoredRow = nil
		s.selectRef(*ref)
	}
}

// selectRef selects the row of a command and a keybinding, else the first row of the command.
func (s *editorState) selectRef(ref rowRef) {
	pick := ""
	for _, r := range s.results {
		if r.Row.Command != ref.command {
			continue
		}
		if r.Row.Key == ref.key {
			pick = r.Row.ID
			break
		}
		if pick == "" {
			pick = r.Row.ID
		}
	}
	if pick == "" {
		return
	}
	s.list.SetSelected(pick)
	s.list.SetFocus(pick)
	s.noteState()
	binding := widgets.WidgetsBindingOf(s.Context())
	binding.Scheduler().ScheduleFrame()
	binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() {
			s.listCtl.Reveal(pick)
		}
	})
}

func (s *editorState) fail(err error) {
	if f := s.Widget().opts.OnError; f != nil && err != nil {
		f(err)
	}
}

func (s *editorState) copyText(text string) {
	if f := s.Widget().opts.CopyText; f != nil {
		f(text)
	}
}

func (s *editorState) codeFont() string {
	if f := s.Widget().opts.CodeFont; f != "" {
		return f
	}
	return "monospace"
}

func (s *editorState) keyLabel(command string) string {
	if f := s.Widget().opts.KeyLabel; f != nil {
		return f(command)
	}
	return ""
}

// ---- search ----

func (s *editorState) queryChanged(text string) {
	if text == s.query {
		return
	}
	s.SetState(func() { s.query = text })
	if s.searchScope != nil {
		s.searchScope.Set("keybindingsSearchHasValue", text != "")
	}
	s.noteState()
}

// setQuery puts a text in the search box.
func (s *editorState) setQuery(q string) {
	s.search.SetValue(q)
	s.queryChanged(q)
}

// startRecording makes the search box record the keys that are pressed: they fill it as an exact search.
func (s *editorState) startRecording() {
	w := s.Widget()
	if s.recording || w.opts.Record == nil {
		return
	}
	s.searchRec = &Recorder{RecordEnter: true}
	s.searchRec.OnChange = func() { s.setQuery(s.searchRec.Quoted()) }
	s.searchRec.OnEscape = s.stopRecordingKeys
	s.stopRec = w.opts.Record(s.searchRec)
	s.SetState(func() { s.recording = true })
	s.search.Focus()
}

// stopRecordingKeys ends the recording and shows it.
func (s *editorState) stopRecordingKeys() {
	if !s.recording {
		return
	}
	s.stopRecording()
	if s.Mounted() {
		s.SetState(func() { s.recording = false })
		s.search.Focus()
	}
}

// stopRecording ends the recording mode of the dispatcher.
func (s *editorState) stopRecording() {
	if s.stopRec != nil {
		s.stopRec()
		s.stopRec = nil
	}
	s.recording = false
	s.searchRec = nil
}

func (s *editorState) toggleRecording() {
	if s.recording {
		s.stopRecordingKeys()
	} else {
		s.startRecording()
	}
}

// ---- build ----

// Build implements widgets.State.
func (s *editorState) Build(bc widgets.BuildContext) widgets.Widget {
	colors := kit.ThemeOf(bc)
	s.refresh()
	body := widgets.NewColumn([]widgets.Widget{s.header(bc, colors), widgets.NewExpanded(s.table(bc, colors))},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	editor := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(27, 11, 0, 0), body)
	layers := []widgets.Widget{editor}
	if s.define != nil {
		layers = append(layers, s.defineOverlay(bc, colors))
	}
	stack := widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitExpand})
	return widgets.NewFocus(widgets.NewColoredBox(colors.EditorBackground, stack), widgets.FocusOpts{
		FocusNode: s.paneNode, CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "KeybindingsEditor",
	})
}

// header is the search box with the buttons that record keys and clear it, and the button that opens the JSON file.
func (s *editorState) header(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	var trailing []widgets.Widget
	if s.recording {
		trailing = append(trailing, s.recordingBadge(bc, colors))
	}
	record := RecordKeysTooltip
	if l := s.keyLabel("keybindings.editor.recordSearchKeys"); l != "" {
		record += " (" + l + ")"
	}
	trailing = append(trailing,
		kit.NewIconButton(fonts.CodiconRecordKeys, kit.IconButtonOpts{
			Key: foundation.NewValueKey("keybindingsui-record"), Tooltip: record, Checked: s.recording, Disabled: w.opts.Record == nil,
			OnPressed: s.toggleRecording,
		}),
		kit.NewIconButton(fonts.CodiconClearAll, kit.IconButtonOpts{
			Key: foundation.NewValueKey("keybindingsui-clear"), Tooltip: ClearSearchTooltip, Disabled: s.query == "",
			OnPressed: func() { s.clearSearch() },
		}))
	placeholder := SearchPlaceholder
	if s.recording {
		placeholder = RecordingPlaceholder
	}
	search := kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Key: foundation.NewValueKey("keybindingsui-search"), Controller: s.search, Placeholder: placeholder,
		OnChanged: s.queryChanged, Autofocus: w.opts.Autofocus, Trailing: trailing,
		OnFocusChange: func(focused bool) {
			if !focused && s.recording {
				// a search that records keys records them while it has the focus only
				binding := widgets.WidgetsBindingOf(s.Context())
				binding.Scheduler().ScheduleFrame()
				binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
					if s.Mounted() && s.recording && !s.search.HasFocus() && s.define == nil {
						s.stopRecordingKeys()
					}
				})
			}
		},
	})
	open := kit.NewIconButton(fonts.CodiconGoToFile, kit.IconButtonOpts{
		Key: foundation.NewValueKey("keybindingsui-open-json"), Tooltip: OpenJSONTooltip, OnPressed: s.openJSON,
	})
	row := widgets.NewRow([]widgets.Widget{widgets.NewExpanded(search), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}), open},
		widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 10, 11), row)
}

func (s *editorState) openJSON() {
	if f := s.Widget().opts.OnOpenJSON; f != nil {
		f()
	}
}

// clearSearch empties the search box; recording goes on.
func (s *editorState) clearSearch() {
	if s.searchRec != nil {
		s.searchRec.chords = nil
	}
	s.setQuery("")
}

// recordingBadge is the "Recording Keys" badge in the search box.
func (s *editorState) recordingBadge(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	// the metrics of the count badge (kit.Badge): 11 px text on a line of 11 px, 3 by 5 px of padding
	style := kit.TextStyleOf(bc, 11, colors.BadgeForeground).CopyWith(painting.TextStyleOpts{Height: foundation.Some(1.0)})
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 8, 0), widgets.NewCenter(widgets.NewDecoratedBox(
		painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.BadgeBackground),
			BorderRadius: painting.NewBorderRadiusCircular(11),
		}),
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(3, 6),
			widgets.NewText(RecordingBadge, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1)})))))
}

// textOf is a one-line text of the workbench's font.
func textOf(bc widgets.BuildContext, text string, size float64, color ui.Color, weight ui.FontWeight) widgets.Widget {
	style := kit.TextStyleOf(bc, size, color)
	if weight != 0 {
		style = style.CopyWith(painting.TextStyleOpts{FontWeight: weight})
	}
	return widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// table is the header row over the rows, with the context menu of a row.
func (s *editorState) table(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	header := widgets.NewContainer(
		widgets.NewRow(append([]widgets.Widget{widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(actionsWidth)})},
			s.cells(
				columnCell(textOf(bc, "Command", fontSize, colors.Foreground, ui.FontWeightBold), commandFlex),
				columnCell(textOf(bc, "Keybinding", fontSize, colors.Foreground, ui.FontWeightBold), keybindingFlex),
				columnCell(textOf(bc, "When", fontSize, colors.Foreground, ui.FontWeightBold), whenFlex),
				columnCell(textOf(bc, "Source", fontSize, colors.Foreground, ui.FontWeightBold), sourceFlex))...),
			widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.ContainerOpts{Height: foundation.Some(headerHeight), Color: foundation.Some(colors.KeybindingTableHeaderBackground)})
	list := kit.NewList(s.list, kit.ListOpts{
		Controller: s.listCtl, Render: s.renderRow, TypeToFind: foundation.Some(false), RowPadding: foundation.Some(0.0),
		OnOpen:        func(row kit.ListViewRow) { s.openDefine(row.Key, false) },
		OnContextMenu: s.showMenu,
		OnSelectionChange: func([]string, []int) {
			s.noteState()
		},
	})
	focused := widgets.NewFocus(list, widgets.FocusOpts{
		FocusNode: s.listNode, CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "KeybindingsTable",
		OnFocusChange: func(has bool) {
			if s.listScope != nil {
				s.listScope.Set("keybindingFocus", has)
			}
		},
	})
	menu := kit.NewContextMenu(focused, kit.ContextMenuOpts{Controller: s.menu})
	return widgets.NewColumn([]widgets.Widget{header, widgets.NewExpanded(menu)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// column is a cell of the table: its contents and its weight.
type column struct {
	child widgets.Widget
	flex  int
}

func columnCell(child widgets.Widget, flex int) column { return column{child: child, flex: flex} }

// cells lay the columns out in a row: each is as wide as its weight says, with room before its contents.
func (s *editorState) cells(cols ...column) []widgets.Widget {
	out := make([]widgets.Widget, len(cols))
	for i, c := range cols {
		out[i] = widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(cellPadding, 0, 4, 0), c.child), widgets.ExpandedOpts{Flex: foundation.Some(c.flex)})
	}
	return out
}

// ---- controller ----

// Controller drives a keybindings editor from outside: the commands of the editor, and tests. It belongs to one
// editor.
type Controller struct{ state *editorState }

// NewController makes a controller that is not attached to an editor yet; its methods do nothing until it is.
func NewController() *Controller { return &Controller{} }

// ViewState is the view of the editor now.
func (c *Controller) ViewState() editorgroup.KeybindingsViewState {
	if c.state == nil {
		return editorgroup.KeybindingsViewState{}
	}
	return c.state.viewState()
}

// SetQuery puts a text in the search box.
func (c *Controller) SetQuery(q string) {
	if c.state != nil {
		c.state.setQuery(q)
		c.state.search.Focus()
	}
}

// Query is the text of the search box.
func (c *Controller) Query() string {
	if c.state == nil {
		return ""
	}
	return c.state.query
}

// ClearSearch empties the search box (the command Clear Search Results).
func (c *Controller) ClearSearch() {
	if c.state != nil {
		c.state.clearSearch()
	}
}

// FocusSearch gives the search box the focus.
func (c *Controller) FocusSearch() {
	if c.state != nil {
		c.state.search.Focus()
	}
}

// FocusRows gives the table the focus, on the row that has it or the first.
func (c *Controller) FocusRows() {
	if c.state != nil {
		c.state.listCtl.Focus()
	}
}

// RecordSearchKeys makes the search box record the keys that are pressed.
func (c *Controller) RecordSearchKeys() {
	if c.state != nil {
		c.state.startRecording()
	}
}

// Recording tells whether the search box records keys.
func (c *Controller) Recording() bool { return c.state != nil && c.state.recording }

// Selected is the row that has the focus in the table, else the selected one.
func (c *Controller) Selected() (keybinding.Row, bool) {
	if c.state == nil {
		return keybinding.Row{}, false
	}
	r, ok := c.state.activeResult()
	return r.Row, ok
}

// Select selects the row of a command and a keybinding in the table (the first row of the command when none has
// the keybinding) and shows it.
func (c *Controller) Select(command, key string) {
	if c.state != nil {
		c.state.selectRef(rowRef{command: command, key: key})
	}
}

// Define opens the box that records the keys of the selected row (Change Keybinding), or, with add, of a
// keybinding more for its command.
func (c *Controller) Define(add bool) {
	if c.state == nil {
		return
	}
	if r, ok := c.state.activeResult(); ok {
		c.state.openDefine(r.Row.ID, add)
	}
}

// Remove takes the keybinding of the selected row away.
func (c *Controller) Remove() {
	if c.state == nil {
		return
	}
	if r, ok := c.state.activeResult(); ok {
		c.state.remove(r.Row)
	}
}

// Reset gives the command of the selected row its built-in keybindings back.
func (c *Controller) Reset() {
	if c.state == nil {
		return
	}
	if r, ok := c.state.activeResult(); ok {
		c.state.reset(r.Row)
	}
}

// DefineWhen opens the box that edits the when-clause of the selected row.
func (c *Controller) DefineWhen() {
	if c.state == nil {
		return
	}
	if r, ok := c.state.activeResult(); ok {
		c.state.openWhen(r.Row)
	}
}

// ShowSame searches for the keybinding of the selected row.
func (c *Controller) ShowSame() {
	if c.state == nil {
		return
	}
	if r, ok := c.state.activeResult(); ok {
		c.state.showSame(r.Row)
	}
}

// Copy puts the selected row on the clipboard: as JSON ("entry"), its command id ("command") or the title of its
// command ("title").
func (c *Controller) Copy(what string) {
	if c.state == nil {
		return
	}
	if r, ok := c.state.activeResult(); ok {
		c.state.copyRow(r.Row, what)
	}
}

// ---- edits ----

func (s *editorState) remove(row keybinding.Row) {
	f := s.Widget().opts.Actions.Remove
	if f == nil || !row.Bound() {
		return
	}
	s.reselect = &rowRef{command: row.Command}
	s.fail(f(row))
	s.afterEdit()
}

func (s *editorState) reset(row keybinding.Row) {
	f := s.Widget().opts.Actions.Reset
	if f == nil {
		return
	}
	s.reselect = &rowRef{command: row.Command}
	s.fail(f(row))
	s.afterEdit()
}

// afterEdit shows the table again and gives it the focus.
func (s *editorState) afterEdit() {
	if s.Mounted() {
		s.SetState(func() {})
		s.listCtl.Focus()
	}
}

// showSame searches for the keys of a row exactly.
func (s *editorState) showSame(row keybinding.Row) {
	if !row.Bound() {
		return
	}
	q := `"` + row.KeyLabel + `"`
	if q != s.query {
		s.setQuery(q)
	}
}

// copyRow puts a row on the clipboard.
func (s *editorState) copyRow(row keybinding.Row, what string) {
	switch what {
	case "command":
		s.copyText(row.Command)
	case "title":
		s.copyText(row.Label)
	default:
		s.copyText(entryJSON(row))
	}
}

// entryJSON is the keybinding of a row as the JSON of an entry of keybindings.json, indented with two spaces.
func entryJSON(row keybinding.Row) string {
	var b strings.Builder
	b.WriteString("{\n  \"key\": " + jsonString(row.Key) + ",\n  \"command\": " + jsonString(row.Command))
	if row.When != "" {
		b.WriteString(",\n  \"when\": " + jsonString(row.When))
	}
	b.WriteString("\n}")
	return b.String()
}

// ---- when-clause ----

// openWhen shows the box that edits the when-clause of a row.
func (s *editorState) openWhen(row keybinding.Row) {
	if !row.Bound() || s.Widget().opts.Actions.SetWhen == nil {
		return
	}
	s.list.SetSelected(row.ID)
	s.list.SetFocus(row.ID)
	s.whenInput.SetValue(row.When)
	s.SetState(func() { s.whenRow = row.ID })
	s.whenInput.Focus()
	s.whenInput.SelectAll()
}

// closeWhen hides the box that edits the when-clause.
func (s *editorState) closeWhen() {
	if s.whenRow == "" {
		return
	}
	s.SetState(func() { s.whenRow = "" })
	s.listCtl.Focus()
}

// acceptWhen gives the row the when-clause of the box, when it is a clause.
func (s *editorState) acceptWhen(text string) {
	res, ok := s.resultFor(s.whenRow)
	if !ok {
		s.closeWhen()
		return
	}
	text = strings.TrimSpace(text)
	if keybinding.ValidateWhen(text) != nil {
		return // the box shows why; it stays open
	}
	row := res.Row
	s.whenRow = ""
	if text != row.When {
		s.reselect = &rowRef{command: row.Command, key: row.Key}
		s.fail(s.Widget().opts.Actions.SetWhen(row, text))
	}
	s.afterEdit()
}

// validateWhen is the message of the box of the when-clause.
func validateWhen(text string) *kit.InputMessage {
	if err := keybinding.ValidateWhen(strings.TrimSpace(text)); err != nil {
		msg := err.Error()
		if appErr, ok := errors.FromError(err); ok {
			msg = appErr.Message
		}
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		return &kit.InputMessage{Content: "Not a when-clause: " + msg, Type: kit.ValidationError}
	}
	return nil
}
