package search

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The ids of the extension: VS Code's, where VS Code has them. The container and the view are under the id of the extension (a
// rule of the manifest), and the command workbench.view.search shows them.
const (
	// ID is the id of the extension.
	ID = "emetos.search"
	// ContainerID and ViewID are the container in the activity bar and its view.
	ContainerID = "emetos.search.container"
	ViewID      = "emetos.search.view"

	// The ids of the commands (search/common/constants.ts, SearchCommandIds).
	FindInFilesCommand            = "workbench.action.findInFiles"
	ReplaceInFilesCommand         = "workbench.action.replaceInFiles"
	ShowSearchCommand             = "workbench.view.search"
	ToggleQueryDetailsCommand     = "workbench.action.search.toggleQueryDetails"
	CloseReplaceWidgetCommand     = "closeReplaceInFilesWidget"
	ToggleCaseSensitiveCommand    = "toggleSearchCaseSensitive"
	ToggleWholeWordCommand        = "toggleSearchWholeWord"
	ToggleRegexCommand            = "toggleSearchRegex"
	TogglePreserveCaseCommand     = "toggleSearchPreserveCase"
	FocusNextResultCommand        = "search.action.focusNextSearchResult"
	FocusPreviousResultCommand    = "search.action.focusPreviousSearchResult"
	OpenResultCommand             = "search.action.openResult"
	ReplaceCommand                = "search.action.replace"
	ReplaceAllInFileCommand       = "search.action.replaceAllInFile"
	ReplaceAllCommand             = "search.action.replaceAll"
	RemoveCommand                 = "search.action.remove"
	CopyMatchCommand              = "search.action.copyMatch"
	CopyPathCommand               = "search.action.copyPath"
	CopyAllCommand                = "search.action.copyAll"
	RefreshCommand                = "search.action.refreshSearchResults"
	ClearCommand                  = "search.action.clearSearchResults"
	CollapseCommand               = "search.action.collapseSearchResults"
	ExpandCommand                 = "search.action.expandSearchResults"
	ViewAsTreeCommand             = "search.action.viewAsTree"
	ViewAsListCommand             = "search.action.viewAsList"
	CancelCommand                 = "search.action.cancel"
	FocusSearchListCommand        = "search.action.focusSearchList"
	FocusNextInputCommand         = "search.focus.nextInputBox"
	FocusPreviousInputCommand     = "search.focus.previousInputBox"
	FocusSearchFromResultsCommand = "search.action.focusSearchFromResults"
	ToggleSearchOnTypeCommand     = "workbench.action.toggleSearchOnType"
)

// The settings of the extension, with VS Code's names (search.contribution.ts). The settings search.exclude, search.useIgnoreFiles,
// search.useGlobalIgnoreFiles, search.useParentIgnoreFiles and search.followSymlinks are the editor's own (they also decide what
// the quick open lists).
const (
	settingSmartCase        = "search.smartCase"
	settingMaxResults       = "search.maxResults"
	settingSearchOnType     = "search.searchOnType"
	settingDebounce         = "search.searchOnTypeDebouncePeriod"
	settingSeedNearestWord  = "search.seedWithNearestWord"
	settingSeedOnFocus      = "search.seedOnFocus"
	settingCollapseResults  = "search.collapseResults"
	settingShowLineNumbers  = "search.showLineNumbers"
	settingDefaultViewMode  = "search.defaultViewMode"
	settingSortOrder        = "search.sortOrder"
	settingDecorationColors = "search.decorations.colors"
	settingDecorationBadges = "search.decorations.badges"
)

// The context keys, by VS Code's names (search/common/constants.ts, SearchContext).
var contextKeys = []string{
	"searchViewletVisible", "searchViewletFocus", "searchResultListFocused", "inputBoxFocus", "searchInputBoxFocus",
	"replaceInputBoxFocus", "patternIncludesInputBoxFocus", "patternExcludesInputBoxFocus", "replaceActive", "hasSearchResult",
	"firstMatchFocus", "fileMatchOrMatchFocus", "fileMatchOrFolderMatchFocus", "fileMatchOrFolderMatchWithResourceFocus",
	"fileMatchFocus", "folderMatchFocus", "folderMatchWithResourceFocus", "matchFocus", "viewHasSearchPattern",
	"viewHasReplacePattern", "viewHasFilePattern", "viewHasSomeCollapsibleResult", "inTreeView",
}

// extKeyRunning is the key of the extension that says a search is going on (the title action Cancel Search).
const extKeyRunning = ID + ".running"

// New is the Factory of the extension.
func New() extension.Extension { return &view{} }

// when-clauses of the keybindings.
const (
	whenViewFocus     = "searchViewletFocus"
	whenViewVisible   = "searchViewletVisible"
	whenResults       = "hasSearchResult"
	whenMatchSelected = "searchViewletVisible && fileMatchOrMatchFocus"
)

// Manifest implements extension.Extension. The container is declared, so it is in the activity bar before the extension runs;
// showing the view, or running one of the commands, activates it.
func (*view) Manifest() extension.Manifest {
	cmd := func(id, title string) extension.CommandSpec {
		return extension.CommandSpec{ID: id, Title: title, Category: "Search"}
	}
	hidden := func(id, title string) extension.CommandSpec { return extension.CommandSpec{ID: id, Title: title} }
	m := extension.Manifest{
		ID: ID, Name: "Search", Version: "0.1.0",
		Description: "Finds and replaces a string in the files of the folder.",
		Contributes: extension.Contributes{
			ContextKeys: contextKeys,
			Commands: []extension.CommandSpec{
				{ID: FindInFilesCommand, Title: "Find in Files", Category: "Search", Description: "Open a workspace search"},
				cmd(ReplaceInFilesCommand, "Replace in Files"),
				{ID: ShowSearchCommand, Title: "Show Search", Category: "View"},
				cmd(ToggleQueryDetailsCommand, "Toggle Query Details"),
				hidden(CloseReplaceWidgetCommand, "Close Replace Widget"),
				cmd(ToggleCaseSensitiveCommand, "Toggle Case Sensitive"),
				cmd(ToggleWholeWordCommand, "Toggle Whole Word"),
				cmd(ToggleRegexCommand, "Toggle Regex"),
				cmd(TogglePreserveCaseCommand, "Toggle Preserve Case"),
				cmd(FocusNextResultCommand, "Focus Next Search Result"),
				cmd(FocusPreviousResultCommand, "Focus Previous Search Result"),
				cmd(OpenResultCommand, "Open Match"),
				cmd(ReplaceCommand, "Replace"),
				cmd(ReplaceAllInFileCommand, "Replace All"),
				cmd(ReplaceAllCommand, "Replace All"),
				cmd(RemoveCommand, "Dismiss"),
				cmd(CopyMatchCommand, "Copy"),
				cmd(CopyPathCommand, "Copy Path"),
				cmd(CopyAllCommand, "Copy All"),
				cmd(RefreshCommand, "Refresh"),
				cmd(ClearCommand, "Clear Search Results"),
				cmd(CollapseCommand, "Collapse All"),
				cmd(ExpandCommand, "Expand All"),
				cmd(ViewAsTreeCommand, "View as Tree"),
				cmd(ViewAsListCommand, "View as List"),
				cmd(CancelCommand, "Cancel Search"),
				cmd(FocusSearchListCommand, "Focus List"),
				cmd(FocusNextInputCommand, "Focus Next Input"),
				cmd(FocusPreviousInputCommand, "Focus Previous Input"),
				cmd(FocusSearchFromResultsCommand, "Focus Search From Results"),
				cmd(ToggleSearchOnTypeCommand, "Toggle Search on Type"),
			},
			Keybindings: []extension.Keybinding{
				{Command: FindInFilesCommand, Key: "ctrl+shift+f"},
				{Command: ReplaceInFilesCommand, Key: "ctrl+shift+h"},
				{Command: ToggleQueryDetailsCommand, Key: "ctrl+shift+j", When: whenViewFocus},
				{Command: CloseReplaceWidgetCommand, Key: "escape", When: whenViewVisible + " && replaceInputBoxFocus"},
				{Command: ToggleCaseSensitiveCommand, Key: "alt+c", When: whenViewFocus},
				{Command: ToggleWholeWordCommand, Key: "alt+w", When: whenViewFocus},
				{Command: ToggleRegexCommand, Key: "alt+r", When: whenViewFocus},
				{Command: TogglePreserveCaseCommand, Key: "alt+p", When: whenViewFocus},
				{Command: FocusNextResultCommand, Key: "f4", When: whenResults},
				{Command: FocusPreviousResultCommand, Key: "shift+f4", When: whenResults},
				{Command: OpenResultCommand, Key: "enter", When: whenMatchSelected + " && !inputBoxFocus"},
				{Command: ReplaceCommand, Key: "ctrl+shift+1", When: "replaceActive && " + whenViewVisible + " && matchFocus && !inputBoxFocus"},
				{Command: ReplaceAllInFileCommand, Key: "ctrl+shift+1", When: "replaceActive && " + whenViewVisible + " && fileMatchFocus && !inputBoxFocus"},
				{Command: ReplaceAllCommand, Key: "ctrl+alt+enter", When: whenViewVisible + " && replaceActive && hasSearchResult"},
				{Command: RemoveCommand, Key: "delete", When: whenViewVisible + " && fileMatchOrMatchFocus && !inputBoxFocus"},
				{Command: CopyMatchCommand, Key: "ctrl+c", When: "fileMatchOrMatchFocus && !inputBoxFocus"},
				{Command: CopyPathCommand, Key: "ctrl+alt+c", When: "fileMatchOrFolderMatchWithResourceFocus && !inputBoxFocus"},
				{Command: FocusNextInputCommand, Key: "ctrl+down", When: "inputBoxFocus && " + whenViewVisible},
				{Command: FocusPreviousInputCommand, Key: "ctrl+up", When: "inputBoxFocus && " + whenViewVisible},
				{Command: FocusSearchFromResultsCommand, Key: "ctrl+up", When: whenViewVisible + " && firstMatchFocus"},
			},
			Menus: []extension.MenuItem{
				{Menu: extension.MenuEdit, Command: FindInFilesCommand, Title: "Find in Files", Group: "4_find_global", Order: 1},
				{Menu: extension.MenuEdit, Command: ReplaceInFilesCommand, Title: "Replace in Files", Group: "4_find_global", Order: 2},
				{Menu: extension.MenuCommandPalette, Command: CloseReplaceWidgetCommand, When: "false"},
			},
			ViewContainers: []extension.ViewContainerSpec{{ID: ContainerID, Title: "Search", Icon: "search", Order: 1}},
			Views:          []extension.ViewSpec{{ID: ViewID, Name: "Search", Container: ContainerID}},
			ViewActions: []extension.ViewActionSpec{
				{View: ViewID, Command: CancelCommand, Icon: "search-stop", Tooltip: "Cancel Search", When: extKeyRunning, Order: 0},
				{View: ViewID, Command: RefreshCommand, Icon: "refresh", Tooltip: "Refresh", When: "!" + extKeyRunning, Order: 0.5},
				{View: ViewID, Command: ClearCommand, Icon: "clear-all", Tooltip: "Clear Search Results", Order: 1},
				{View: ViewID, Command: ViewAsTreeCommand, Icon: "list-tree", Tooltip: "View as Tree", When: "!inTreeView", Order: 2},
				{View: ViewID, Command: ViewAsListCommand, Icon: "list-flat", Tooltip: "View as List", When: "inTreeView", Order: 2},
				{View: ViewID, Command: CollapseCommand, Icon: "collapse-all", Tooltip: "Collapse All", When: "!hasSearchResult || viewHasSomeCollapsibleResult", Order: 4},
				{View: ViewID, Command: ExpandCommand, Icon: "expand-all", Tooltip: "Expand All", When: "hasSearchResult && !viewHasSomeCollapsibleResult", Order: 4},
			},
			Settings: []extension.SettingSpec{
				{Key: settingSmartCase, Type: "boolean", Default: false, Category: "Search",
					Description: "Search case-insensitively if the pattern is all lowercase, otherwise, search case-sensitively."},
				{Key: settingMaxResults, Type: "number", Default: 20000, Category: "Search",
					Description: "Controls the maximum number of search results. An empty value (`null`) means 20000; Emetos has no unlimited search."},
				{Key: settingSearchOnType, Type: "boolean", Default: true, Category: "Search", Description: "Search all files as you type."},
				{Key: settingDebounce, Type: "number", Default: 300, Category: "Search",
					Description: "When `#search.searchOnType#` is enabled, controls the timeout in milliseconds between a character being typed and the search starting. Has no effect when `search.searchOnType` is disabled."},
				{Key: settingSeedNearestWord, Type: "boolean", Default: false, Category: "Search",
					Description: "Enable seeding search from the word nearest the cursor when the active editor has no selection."},
				{Key: settingSeedOnFocus, Type: "boolean", Default: false, Category: "Search",
					Description: "Update the search query to the editor's selected text when focusing the search view. This happens either on click or when triggering the `workbench.views.search.focus` command."},
				{Key: settingCollapseResults, Type: "string", Default: "alwaysExpand", Enum: []any{"auto", "alwaysCollapse", "alwaysExpand"}, Category: "Search",
					Description: "Controls whether the search results will be collapsed or expanded."},
				{Key: settingShowLineNumbers, Type: "boolean", Default: false, Category: "Search", Description: "Controls whether to show line numbers for search results."},
				{Key: settingDefaultViewMode, Type: "string", Default: "list", Enum: []any{"tree", "list"}, Category: "Search",
					Description: "Controls the default search result view mode."},
				{Key: settingSortOrder, Type: "string", Default: "default",
					Enum: []any{"default", "fileNames", "type", "modified", "countDescending", "countAscending"}, Category: "Search",
					Description: "Controls sorting order of search results."},
				{Key: settingDecorationColors, Type: "boolean", Default: true, Category: "Search", Description: "Controls whether search file decorations should use colors."},
				{Key: settingDecorationBadges, Type: "boolean", Default: true, Category: "Search", Description: "Controls whether search file decorations should use badges."},
			},
		},
	}
	return m
}

// view is the extension of one scope. Everything but the search goroutine (run) and the batch it fills (push) belongs to the
// UI thread.
type view struct {
	api    extension.API
	ctx    context.Context
	folder string
	handle extension.ViewHandle

	inputs
	res    *results
	tree   treeState
	status searchStatus

	// the search that runs (UI thread), and the batch its goroutine fills (mu)
	gen    int
	cancel context.CancelFunc
	mu     sync.Mutex
	batch  []extension.TextSearchFile
	posted bool

	// built tells whether the view was built once (it is then in the window)
	built bool
	// the keys
	keys map[string]any

	// timer is the debounce of the search while typing, and typeGen tells which typing it is for.
	timer   *time.Timer
	typeGen int
}

// Activate implements extension.Extension.
func (v *view) Activate(ctx context.Context, api extension.API) error {
	v.api, v.ctx, v.folder = api, ctx, api.Scope().Folder
	v.res = newResults()
	v.keys = map[string]any{}
	v.initInputs()
	v.initTree()
	var err error
	v.handle, err = api.Views().Provide(ViewID, extension.ViewContent{Build: v.build, SaveState: v.saveState, RestoreState: v.restoreState})
	if err != nil {
		return err
	}
	api.Settings().OnChange(func() { v.settingsChanged() })
	if err := v.handleCommands(); err != nil {
		return err
	}
	v.tree.treeView = v.string(settingDefaultViewMode) == "tree"
	v.updateKeys()
	return nil
}

// handleCommands sets the handlers of the commands the manifest declares.
func (v *view) handleCommands() error {
	do := func(f func()) extension.CommandHandler {
		return func(context.Context, any) error { f(); return nil }
	}
	handlers := map[string]extension.CommandHandler{
		FindInFilesCommand:            func(_ context.Context, args any) error { v.findInFiles(args, false); return nil },
		ReplaceInFilesCommand:         func(_ context.Context, args any) error { v.findInFiles(args, true); return nil },
		ShowSearchCommand:             do(func() { v.handle.Reveal(true); v.focusSearchBox() }),
		ToggleQueryDetailsCommand:     do(v.toggleDetails),
		CloseReplaceWidgetCommand:     do(v.closeReplace),
		ToggleCaseSensitiveCommand:    do(func() { v.setFlag(&v.caseSensitive, !v.caseSensitive) }),
		ToggleWholeWordCommand:        do(func() { v.setFlag(&v.wholeWord, !v.wholeWord) }),
		ToggleRegexCommand:            do(func() { v.setFlag(&v.regex, !v.regex) }),
		TogglePreserveCaseCommand:     do(func() { v.setFlag(&v.preserveCase, !v.preserveCase); v.refreshView() }),
		FocusNextResultCommand:        do(func() { v.focusResult(1) }),
		FocusPreviousResultCommand:    do(func() { v.focusResult(-1) }),
		OpenResultCommand:             do(func() { v.openFocused(false) }),
		ReplaceCommand:                do(v.replaceFocused),
		ReplaceAllInFileCommand:       do(v.replaceFocused),
		ReplaceAllCommand:             do(v.replaceAll),
		RemoveCommand:                 do(v.dismissFocused),
		CopyMatchCommand:              do(v.copyFocused),
		CopyPathCommand:               do(v.copyPathFocused),
		CopyAllCommand:                do(v.copyAll),
		RefreshCommand:                do(v.refreshSearch),
		ClearCommand:                  do(v.clear),
		CollapseCommand:               do(func() { v.collapseAll(true) }),
		ExpandCommand:                 do(func() { v.collapseAll(false) }),
		ViewAsTreeCommand:             do(func() { v.setTreeView(true) }),
		ViewAsListCommand:             do(func() { v.setTreeView(false) }),
		CancelCommand:                 do(v.cancelSearch),
		FocusSearchListCommand:        do(v.focusList),
		FocusNextInputCommand:         do(func() { v.moveInputFocus(1) }),
		FocusPreviousInputCommand:     do(func() { v.moveInputFocus(-1) }),
		FocusSearchFromResultsCommand: do(v.focusSearchBox),
		ToggleSearchOnTypeCommand:     do(v.toggleSearchOnType),
	}
	for id, h := range handlers {
		if err := v.api.Commands().Handle(id, h); err != nil {
			return err
		}
	}
	return nil
}

// ---- the settings ----

func (v *view) bool(key string) bool {
	b, err := extension.SettingValue[bool](v.api.Settings(), key)
	if err != nil {
		v.api.Log().Warn("a setting cannot be read", "key", key, "err", err)
	}
	return b
}

func (v *view) string(key string) string {
	s, err := extension.SettingValue[string](v.api.Settings(), key)
	if err != nil {
		v.api.Log().Warn("a setting cannot be read", "key", key, "err", err)
	}
	return s
}

// number reads a number setting; def is the value when it is null or cannot be read.
func (v *view) number(key string, def int) int {
	raw, err := v.api.Settings().Get(key)
	if err != nil || raw == nil {
		return def
	}
	if f, ok := raw.(float64); ok {
		return int(f)
	}
	n, err := extension.SettingValue[int](v.api.Settings(), key)
	if err != nil {
		return def
	}
	return n
}

// settingsChanged follows the settings that change what the view shows.
func (v *view) settingsChanged() {
	v.rebuildTree()
	v.refreshView()
}

// ---- the search ----

// query is the search of the boxes.
func (v *view) query() extension.TextSearchQuery {
	return extension.TextSearchQuery{
		Pattern: v.searchBox.Value(), IsRegExp: v.regex, IsCaseSensitive: v.caseSensitive, IsWordMatch: v.wholeWord,
		IsSmartCase: v.bool(settingSmartCase),
	}
}

// options are the options of the search of the boxes.
func (v *view) options() extension.TextSearchOptions {
	return extension.TextSearchOptions{
		Include: strings.TrimSpace(v.includeBox.Value()), Exclude: strings.TrimSpace(v.excludeBox.Value()),
		DisregardExcludeSettings: !v.useExcludes, DisregardIgnoreFiles: !v.useExcludes, OnlyOpenEditors: v.onlyOpen,
		MaxResults: v.number(settingMaxResults, 0),
	}
}

// queryChanged is called when a box or a toggle changed the search: the search starts, now, or after the debounce while typing
// (search.searchOnType).
func (v *view) queryChanged(immediate bool) {
	v.updateKeys()
	if v.timer != nil {
		v.timer.Stop()
		v.timer = nil
	}
	v.typeGen++
	if v.searchBox.Value() == "" {
		v.clearResults()
		return
	}
	if !immediate && !v.bool(settingSearchOnType) {
		return
	}
	if immediate {
		v.startSearch()
		return
	}
	gen, wait := v.typeGen, time.Duration(v.number(settingDebounce, 300))*time.Millisecond
	v.timer = time.AfterFunc(wait, func() {
		v.api.Post(func() {
			if gen == v.typeGen {
				v.startSearch()
			}
		})
	})
}

// startSearch cancels the search that runs and starts another with the boxes.
func (v *view) startSearch() {
	v.stopSearch()
	v.status = searchStatus{}
	v.res = newResults()
	v.tree.seen = map[string]bool{}
	v.tree.curKey = ""
	q, o := v.query(), v.options()
	if q.Pattern == "" {
		v.rebuildTree()
		v.refreshView()
		return
	}
	if v.folder == "" && !o.OnlyOpenEditors {
		v.status = searchStatus{done: true, noFolder: true}
		v.rebuildTree()
		v.refreshView()
		return
	}
	if err := v.validateQuery(q); err != nil {
		v.status = searchStatus{done: true, err: err}
		v.queryInvalid = true
		v.rebuildTree()
		v.refreshView()
		return
	}
	v.queryInvalid = false
	v.gen++
	gen := v.gen
	ctx, cancel := context.WithCancel(v.ctx)
	v.cancel = cancel
	v.status = searchStatus{running: true, query: q, opts: o}
	v.replacer = nil
	v.updateKeys()
	v.refreshView()
	go func() {
		sum, err := v.api.Workspace().FindTextInFiles(ctx, q, o, func(f extension.TextSearchFile) { v.push(gen, f) })
		v.api.Post(func() { v.finish(gen, sum, err) })
	}()
}

// validateQuery returns the error of a regular expression that does not compile, before any search starts.
func (v *view) validateQuery(q extension.TextSearchQuery) error {
	if !q.IsRegExp {
		return nil
	}
	_, err := v.api.Workspace().NewTextReplacer(q, "", false)
	return err
}

// stopSearch cancels the search that runs; its results are dropped.
func (v *view) stopSearch() {
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	v.gen++
	v.mu.Lock()
	v.batch = nil
	v.mu.Unlock()
	v.status.running = false
}

// cancelSearch is the command Cancel Search: the search stops and keeps the results it has.
func (v *view) cancelSearch() {
	if !v.status.running {
		return
	}
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	v.flush()
	v.gen++
	v.status.running = false
	v.status.done = true
	v.status.cancelled = true
	v.updateKeys()
	v.refreshView()
}

// push is called by the search for each file with matches, on a goroutine of the engine (one at a time). It puts the file in
// the batch and, unless the UI thread has been asked already, asks it to take the batch.
func (v *view) push(gen int, f extension.TextSearchFile) {
	v.mu.Lock()
	if v.ctx.Err() != nil {
		v.mu.Unlock()
		return
	}
	v.batch = append(v.batch, f)
	ask := !v.posted
	v.posted = true
	v.mu.Unlock()
	if ask {
		v.api.Post(func() { v.deliver(gen) })
	}
}

// deliverGap is the least time between two takes of the batch: the UI thread does the work of the tree once for every file the
// search found in it, so the cost of a frame does not grow with the speed of the search.
const deliverGap = 30 * time.Millisecond

// deliver takes the batch on the UI thread.
func (v *view) deliver(gen int) {
	if gen == v.gen {
		v.flush()
		v.rebuildTree()
		v.refreshView()
	}
	time.AfterFunc(deliverGap, func() {
		v.mu.Lock()
		again := len(v.batch) > 0
		v.posted = again
		v.mu.Unlock()
		if again {
			v.api.Post(func() { v.deliver(v.gen) })
		}
	})
}

// flush moves the batch into the results.
func (v *view) flush() {
	v.mu.Lock()
	batch := v.batch
	v.batch = nil
	v.mu.Unlock()
	for _, f := range batch {
		v.res.add(f)
	}
}

// finish is called when the search is over.
func (v *view) finish(gen int, sum extension.TextSearchSummary, err error) {
	if gen != v.gen {
		return
	}
	v.flush()
	v.cancel = nil
	v.status.running = false
	v.status.done = true
	v.status.summary = sum
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "SearchCancelled" {
			v.status.cancelled = true
		} else {
			v.status.err = err
		}
	}
	v.rebuildTree()
	v.updateKeys()
	v.refreshView()
}

// refreshSearch runs the search of the boxes again.
func (v *view) refreshSearch() {
	if v.searchBox.Value() == "" {
		return
	}
	v.startSearch()
}

// clear is the command Clear Search Results: the results and the text of the boxes go.
func (v *view) clear() {
	v.stopSearch()
	v.searchBox.SetValue("")
	v.replaceBox.SetValue("")
	v.includeBox.SetValue("")
	v.excludeBox.SetValue("")
	v.onlyOpen = false
	v.clearResults()
	v.focusSearchBox()
}

// clearResults empties the results.
func (v *view) clearResults() {
	v.stopSearch()
	v.res = newResults()
	v.status = searchStatus{}
	v.queryInvalid = false
	v.tree.seen = map[string]bool{}
	v.rebuildTree()
	v.updateKeys()
	v.refreshView()
}

// ---- the state of the view ----

// state is what the view keeps across a restart: the boxes, the toggles and the mode.
type state struct {
	Query         string `json:"query,omitempty"`
	Replace       string `json:"replace,omitempty"`
	Include       string `json:"include,omitempty"`
	Exclude       string `json:"exclude,omitempty"`
	Case          bool   `json:"case,omitempty"`
	Word          bool   `json:"word,omitempty"`
	Regex         bool   `json:"regex,omitempty"`
	PreserveCase  bool   `json:"preserveCase,omitempty"`
	ReplaceOpen   bool   `json:"replaceOpen,omitempty"`
	DetailsOpen   bool   `json:"detailsOpen,omitempty"`
	NoExcludes    bool   `json:"noExcludes,omitempty"`
	OnlyOpen      bool   `json:"onlyOpen,omitempty"`
	TreeView      bool   `json:"treeView,omitempty"`
	TreeViewKnown bool   `json:"treeViewKnown,omitempty"`
}

func (v *view) saveState() (any, error) {
	return state{
		Query: v.searchBox.Value(), Replace: v.replaceBox.Value(), Include: v.includeBox.Value(), Exclude: v.excludeBox.Value(),
		Case: v.caseSensitive, Word: v.wholeWord, Regex: v.regex, PreserveCase: v.preserveCase, ReplaceOpen: v.replaceOpen,
		DetailsOpen: v.detailsOpen, NoExcludes: !v.useExcludes, OnlyOpen: v.onlyOpen, TreeView: v.tree.treeView, TreeViewKnown: true,
	}, nil
}

func (v *view) restoreState(raw json.RawMessage) error {
	var s state
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	v.searchBox.SetValue(s.Query)
	v.replaceBox.SetValue(s.Replace)
	v.includeBox.SetValue(s.Include)
	v.excludeBox.SetValue(s.Exclude)
	v.caseSensitive, v.wholeWord, v.regex, v.preserveCase = s.Case, s.Word, s.Regex, s.PreserveCase
	v.replaceOpen, v.detailsOpen, v.useExcludes, v.onlyOpen = s.ReplaceOpen, s.DetailsOpen, !s.NoExcludes, s.OnlyOpen
	if s.TreeViewKnown {
		v.tree.treeView = s.TreeView
	}
	v.updateKeys()
	return nil
}
