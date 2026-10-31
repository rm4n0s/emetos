package scm

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// EventKind says what changed in a Model.
type EventKind int

// The kinds of Event.
const (
	// SourceAdded and SourceRemoved: a source control came or went.
	SourceAdded EventKind = iota
	// SourceRemoved is a source control that was removed.
	SourceRemoved
	// SourceChanged: the count, the status bar commands, the busy mark, the name, the accept command or the action button of a
	// source control changed.
	SourceChanged
	// GroupsChanged: a group was added, removed or changed its label, its flag or its context value.
	GroupsChanged
	// ResourcesChanged: the resources of a group were replaced.
	ResourcesChanged
	// InputChanged: the text, the placeholder, the validation, the visibility, the enablement of an input changed, or the input
	// asked for the focus (Input.FocusRequested).
	InputChanged
	// ViewChanged: the view mode, the sort key, the repositories' order, which repositories are shown, the focused or the active
	// repository, or the configuration changed.
	ViewChanged
)

// Event is a change of the Model.
type Event struct {
	// Kind says what changed.
	Kind EventKind
	// Source is the source control concerned; nil for a ViewChanged that is about the view as a whole.
	Source *Source
	// Group is the group concerned, for GroupsChanged and ResourcesChanged.
	Group *Group
}

// ViewMode is how the resources of a group are shown (VS Code's ViewMode).
type ViewMode string

// The view modes.
const (
	// ViewList shows the resources as a flat list.
	ViewList ViewMode = "list"
	// ViewTree shows them as a tree of folders.
	ViewTree ViewMode = "tree"
)

// RepositorySortKey is the order of the repositories (VS Code's ISCMRepositorySortKey; the setting writes "discovery time").
type RepositorySortKey string

// The orders of the repositories.
const (
	// SortDiscoveryTime orders them as they were found; the repositories shown in the Changes view in the order they were
	// selected.
	SortDiscoveryTime RepositorySortKey = "discoveryTime"
	// SortRepositoryName orders them by name, then by path.
	SortRepositoryName RepositorySortKey = "name"
	// SortRepositoryPath orders them by path.
	SortRepositoryPath RepositorySortKey = "path"
)

// SelectionMode says how many repositories the view shows at once (scm.repositories.selectionMode).
type SelectionMode string

// The selection modes.
const (
	// SelectSingle shows one repository.
	SelectSingle SelectionMode = "single"
	// SelectMultiple shows the repositories that are selected.
	SelectMultiple SelectionMode = "multiple"
)

// CountBadge says which counts make the badge of the Source Control icon (scm.countBadge).
type CountBadge string

// The values of scm.countBadge.
const (
	// CountAll adds the counts of the repositories that are shown.
	CountAll CountBadge = "all"
	// CountFocused is the count of the active repository.
	CountFocused CountBadge = "focused"
	// CountOff shows no badge.
	CountOff CountBadge = "off"
)

// ProviderCountBadge says when the count badge of a repository is shown (scm.providerCountBadge).
type ProviderCountBadge string

// The values of scm.providerCountBadge.
const (
	// ProviderCountHidden never shows it.
	ProviderCountHidden ProviderCountBadge = "hidden"
	// ProviderCountAuto shows it when the count is not zero.
	ProviderCountAuto ProviderCountBadge = "auto"
	// ProviderCountVisible always shows it.
	ProviderCountVisible ProviderCountBadge = "visible"
)

// Config is what the settings say; the host gives it with Model.SetConfig. The zero value is not valid: use DefaultConfig.
type Config struct {
	// CountBadge is scm.countBadge and ProviderCountBadge scm.providerCountBadge.
	CountBadge         CountBadge
	ProviderCountBadge ProviderCountBadge
	// DefaultViewMode is scm.defaultViewMode and DefaultViewSortKey scm.defaultViewSortKey.
	DefaultViewMode    ViewMode
	DefaultViewSortKey SortKey
	// CompactFolders is scm.compactFolders.
	CompactFolders bool
	// SelectionMode is scm.repositories.selectionMode.
	SelectionMode SelectionMode
	// RepositoriesSortOrder is scm.repositories.sortOrder ("discovery time", "name", "path"), read when the Model is made.
	RepositoriesSortOrder string
}

// DefaultConfig is the configuration of VS Code's defaults.
func DefaultConfig() Config {
	return Config{
		CountBadge: CountAll, ProviderCountBadge: ProviderCountHidden, DefaultViewMode: ViewList, DefaultViewSortKey: SortByPath,
		CompactFolders: true, SelectionMode: SelectMultiple, RepositoriesSortOrder: "discovery time",
	}
}

// ParseRepositorySortOrder reads the value of scm.repositories.sortOrder.
func ParseRepositorySortOrder(s string) RepositorySortKey {
	switch s {
	case "name":
		return SortRepositoryName
	case "path":
		return SortRepositoryPath
	}
	return SortDiscoveryTime
}

// Model holds the source controls of one workspace and the state of the Source Control view that does not belong to a widget:
// which repositories are shown, which is active, and the view mode and sort key.
type Model struct {
	mu   sync.Mutex
	subs []*subscriber

	cfg     Config
	sources []*Source // every source control, in the order of the repositories
	handles int       // numbers the sources
	// histories are the commit messages of the repositories of this session, by StorageKey: a repository that is closed and found
	// again has its messages back (VS Code's SCMInputHistory, which also saves them in the workspace state; Emetos keeps them for
	// the session).
	histories map[string]*History
	disc      uint64 // numbers the discovery order

	// the view (scmViewService.ts)
	sortKey      RepositorySortKey
	editorRepo   *Source
	editorLatest bool // the active editor's repository changed after the focus did
	pinned       *Source
	activeURI    string

	// the Changes view (scmViewPane.ts)
	viewMode     ViewMode
	viewChosen   bool // the user chose the mode: the default no longer applies
	viewSortKey  SortKey
	storedSort   SortKey
	hasStoredKey bool

	// the saved state (state.go)
	restored    *State
	restoreOpen bool // source controls that are added are matched with restored
	didSelect   bool // a repository that restored shows came: the others are not shown
}

type subscriber struct{ fn func(Event) }

// NewModel makes an empty model with a configuration.
//
// The repositories that are shown, their order and the view mode are the state of the window: Restore and State (state.go).
func NewModel(cfg Config) *Model {
	m := &Model{cfg: normalizeConfig(cfg)}
	m.sortKey = ParseRepositorySortOrder(m.cfg.RepositoriesSortOrder)
	m.viewMode = m.cfg.DefaultViewMode
	m.viewSortKey = m.computeSortKey()
	return m
}

// normalizeConfig replaces the values that are none of the known ones by the defaults.
func normalizeConfig(c Config) Config {
	d := DefaultConfig()
	switch c.CountBadge {
	case CountAll, CountFocused, CountOff:
	default:
		c.CountBadge = d.CountBadge
	}
	switch c.ProviderCountBadge {
	case ProviderCountHidden, ProviderCountAuto, ProviderCountVisible:
	default:
		c.ProviderCountBadge = d.ProviderCountBadge
	}
	if c.DefaultViewMode != ViewTree {
		c.DefaultViewMode = ViewList
	}
	c.DefaultViewSortKey = ParseSortKey(string(c.DefaultViewSortKey))
	if c.SelectionMode != SelectSingle {
		c.SelectionMode = SelectMultiple
	}
	return c
}

// Subscribe calls fn for each change of the model, from the goroutine that made it and with no lock held, and returns the
// function that stops the calls.
func (m *Model) Subscribe(fn func(Event)) (cancel func()) {
	s := &subscriber{fn: fn}
	m.mu.Lock()
	m.subs = append(m.subs, s)
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.subs = slices.DeleteFunc(m.subs, func(o *subscriber) bool { return o == s })
	}
}

// notify tells the subscribers about events. The caller must not hold the lock.
func (m *Model) notify(events ...Event) {
	if len(events) == 0 {
		return
	}
	m.mu.Lock()
	subs := slices.Clone(m.subs)
	m.mu.Unlock()
	for _, e := range events {
		for _, s := range subs {
			s.fn(e)
		}
	}
}

// Config is the configuration now.
func (m *Model) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// SetConfig gives the model the settings after they changed. A change of the selection mode applies at once (to a single
// repository the first of those shown, to multiple all of them); a default view mode or sort key applies unless the user chose
// one.
func (m *Model) SetConfig(cfg Config) {
	cfg = normalizeConfig(cfg)
	m.mu.Lock()
	old := m.cfg
	cfg.RepositoriesSortOrder = old.RepositoriesSortOrder // read when the model is made
	m.cfg = cfg
	events := []Event{{Kind: ViewChanged}}
	if cfg.SelectionMode != old.SelectionMode {
		events = append(events, m.applySelectionModeLocked()...)
	}
	if !m.viewChosen && cfg.DefaultViewMode != old.DefaultViewMode {
		m.viewMode = cfg.DefaultViewMode
	}
	m.viewSortKey = m.computeSortKey()
	m.mu.Unlock()
	m.notify(events...)
}

// Name is how a source control is called in the view and the status bar: the folder name of its root, the label when it has none.
func (s *Source) Name() string {
	if s.rootPath != "" {
		if b := baseName(s.rootPath); b != "" {
			return b
		}
	}
	return s.label
}

// SourceSpec says what a source control is; see Model.Add.
type SourceSpec struct {
	// ProviderID is the id of the provider ("git"), the value of the context key scmProvider.
	ProviderID string
	// Label is the name of the provider ("Git").
	Label string
	// RootURI is the repository's folder as file:///path, or empty.
	RootURI string
	// ContextValue is the value of scmProviderContext.
	ContextValue string
	// Owner is the id of the extension that made the source control; the model does not use it.
	Owner string
}

// Add adds a source control. Which repositories are shown follows scmViewService: with several selected allowed the new one is
// selected, with a single one only when nothing is. While the saved state is being restored (Restore, until FinishRestore) a
// source control that the state does not know shows every repository, one that it shows replaces what was shown before it, and
// one that it hides is shown only until a repository the state shows comes.
func (m *Model) Add(spec SourceSpec) *Source {
	m.mu.Lock()
	m.handles++
	m.disc++
	s := &Source{
		m: m, key: "scm" + strconv.Itoa(m.handles), providerID: spec.ProviderID, label: spec.Label, rootURI: spec.RootURI,
		rootPath: PathOfURI(spec.RootURI), contextValue: spec.ContextValue, owner: spec.Owner, count: -1, discovery: m.disc, selection: -1,
	}
	if spec.RootURI == "" {
		s.rootPath = ""
	}
	h := m.historyLocked(s)
	s.input = &Input{src: s, enabled: true, visible: true, history: h, value: h.Current()}
	events := []Event{}
	if m.restored != nil && m.restoreOpen {
		if done := m.addRestoredLocked(s, &events); done {
			m.mu.Unlock()
			m.notify(events...)
			return s
		}
	}
	if m.cfg.SelectionMode == SelectMultiple || !m.anySelectedLocked() {
		s.selection = m.maxSelectionLocked() + 1
	}
	m.insertLocked(s)
	events = append(events, Event{Kind: SourceAdded, Source: s})
	if m.focusedLocked() == nil {
		m.setFocusLocked(s)
	}
	m.mu.Unlock()
	m.notify(events...)
	return s
}

// addRestoredLocked is the part of Add that follows the saved state; it reports whether it added the source control.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewService.ts (onDidAddRepository)
func (m *Model) addRestoredLocked(s *Source, events *[]Event) (done bool) {
	i := slices.Index(m.restored.All, s.StorageKey())
	if i < 0 {
		// not in the saved state: closed before, or new; every repository is shown
		m.insertLocked(s)
		if m.cfg.SelectionMode == SelectMultiple || !m.anySelectedLocked() {
			for n, o := range m.sources {
				o.selection = n
			}
		}
		m.didSelect = false
		*events = append(*events, Event{Kind: SourceAdded, Source: s}, Event{Kind: ViewChanged})
		if m.focusedLocked() == nil {
			m.setFocusLocked(m.sources[0])
		}
		return true
	}
	if !slices.Contains(m.restored.Visible, i) {
		if m.didSelect {
			m.insertLocked(s)
			*events = append(*events, Event{Kind: SourceAdded, Source: s})
			return true
		}
		return false
	}
	if !m.didSelect {
		for _, o := range m.sources {
			o.selection, o.focused = -1, false
		}
		m.didSelect = true
		*events = append(*events, Event{Kind: ViewChanged})
	}
	return false
}

// historyLocked is the history of the commit messages of a source control: the one its repository had in this session, else a new
// one. A source control with no root folder keeps its messages in memory, with the source control.
func (m *Model) historyLocked(s *Source) *History {
	if s.rootURI == "" {
		return NewHistory(historyCapacity)
	}
	key := s.StorageKey()
	if h, ok := m.histories[key]; ok {
		return h
	}
	if m.histories == nil {
		m.histories = map[string]*History{}
	}
	h := NewHistory(historyCapacity)
	m.histories[key] = h
	return h
}

// Remove takes a source control away, with its groups. It does nothing for one that was removed.
func (m *Model) Remove(s *Source) {
	m.mu.Lock()
	i := slices.Index(m.sources, s)
	if i < 0 {
		m.mu.Unlock()
		return
	}
	m.sources = slices.Delete(m.sources, i, i+1)
	s.removed = true
	// the message being written stays with the repository for the session (VS Code saves it when the state is saved)
	if h := s.input.history; h.IsAtEnd() {
		h.ReplaceLast(s.input.value)
	}
	events := []Event{{Kind: SourceRemoved, Source: s}}
	// the first of the others is shown when none is
	if len(m.sources) > 0 && len(m.visibleLocked()) == 0 {
		m.sources[0].selection = 0
	}
	if s.focused {
		s.focused = false
		if vis := m.visibleLocked(); len(vis) > 0 {
			m.setFocusLocked(vis[0])
		}
	}
	if m.pinned == s {
		m.pinned = nil
	}
	if m.editorRepo == s {
		m.editorRepo = nil
	}
	m.mu.Unlock()
	m.notify(events...)
}

// Source returns the source control with the handle, or nil.
func (m *Model) Source(key string) *Source {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sources {
		if s.key == key {
			return s
		}
	}
	return nil
}

// Count is the number of source controls (the context key scm.providerCount).
func (m *Model) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sources)
}

// Sources are the source controls in the order of the repositories (VS Code's ISCMViewService.repositories).
func (m *Model) Sources() []*Source {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sources)
}

// Repository is the source control that an open file belongs to: the one with the deepest root that holds it. The file itself
// at a root belongs to no repository (VS Code's ISCMService.getRepository). It returns nil for a file that is in none.
func (m *Model) Repository(uri string) *Source {
	if !strings.HasPrefix(uri, "file://") {
		return nil
	}
	p := PathOfURI(uri)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.repositoryLocked(p)
}

func (m *Model) repositoryLocked(p string) *Source {
	var best *Source
	bestLen := int(^uint(0) >> 1)
	for _, s := range m.sources {
		if s.rootPath == "" {
			continue
		}
		if rel, ok := relativePath(s.rootPath, p); ok && len(rel) < bestLen {
			best, bestLen = s, len(rel)
		}
	}
	return best
}

// ---- the sources of a Model ----

// Source is a source control: a repository, with its groups, input and action button.
type Source struct {
	m            *Model
	key          string
	providerID   string
	label        string
	rootURI      string
	rootPath     string
	contextValue string
	owner        string
	discovery    uint64
	input        *Input
	removed      bool

	// guarded by m.mu
	count      int
	status     []StatusCommand
	activeName string
	busy       bool
	accept     Command
	button     *ActionButton
	groups     []*Group
	selection  int // the place among the selected repositories; -1 for one that is not selected
	focused    bool
}

// Key is the handle of the source control, unique in the model ("scm3"): the member "sourceControl" of the arguments of a menu
// command.
func (s *Source) Key() string { return s.key }

// ProviderID is the id of the provider ("git"), the value of the context key scmProvider.
func (s *Source) ProviderID() string { return s.providerID }

// Label is the name of the provider ("Git").
func (s *Source) Label() string { return s.label }

// RootURI is the folder of the repository as file:///path, or empty.
func (s *Source) RootURI() string { return s.rootURI }

// RootPath is the path of the folder of the repository, or empty.
func (s *Source) RootPath() string { return s.rootPath }

// ContextValue is the value of scmProviderContext.
func (s *Source) ContextValue() string { return s.contextValue }

// Owner is the id of the extension that made the source control.
func (s *Source) Owner() string { return s.owner }

// Input is the commit message box.
func (s *Source) Input() *Input { return s.input }

// Removed tells whether the source control was removed from the model.
func (s *Source) Removed() bool {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.removed
}

// SetCount sets the count the provider gives; negative is unset (the number of resources counts).
func (s *Source) SetCount(count int) {
	s.mutate(func() bool {
		if count < 0 {
			count = -1
		}
		if s.count == count {
			return false
		}
		s.count = count
		return true
	})
}

// SetStatusBarCommands sets the items of the header and the status bar.
func (s *Source) SetStatusBarCommands(commands []StatusCommand) {
	s.mutate(func() bool {
		if slices.EqualFunc(s.status, commands, func(a, b StatusCommand) bool {
			// Args is any: it holds a map or a slice for the commands of Git, which == cannot compare (it panics)
			return a.Text == b.Text && a.Tooltip == b.Tooltip && a.Command == b.Command && reflect.DeepEqual(a.Args, b.Args)
		}) {
			return false
		}
		s.status = slices.Clone(commands)
		return true
	})
}

// SetActiveRepositoryName sets the name that replaces Name while the source control is active; empty clears it.
func (s *Source) SetActiveRepositoryName(name string) {
	s.mutate(func() bool {
		if s.activeName == name {
			return false
		}
		s.activeName = name
		return true
	})
}

// SetBusy sets the progress bar of the header.
func (s *Source) SetBusy(busy bool) {
	s.mutate(func() bool {
		if s.busy == busy {
			return false
		}
		s.busy = busy
		return true
	})
}

// SetAcceptInputCommand sets the command of Ctrl+Enter; an empty ID clears it.
func (s *Source) SetAcceptInputCommand(c Command) {
	s.mutate(func() bool { s.accept = c; return true })
}

// SetActionButton sets the button under the input; nil removes it.
func (s *Source) SetActionButton(b *ActionButton) {
	s.mutate(func() bool {
		if b == nil {
			s.button = nil
			return true
		}
		c := *b
		c.Secondary = make([][]Command, len(b.Secondary))
		for i, g := range b.Secondary {
			c.Secondary[i] = slices.Clone(g)
		}
		s.button = &c
		return true
	})
}

// mutate runs a change under the lock of the model and tells the subscribers when it changed something. A removed source
// control is not changed.
func (s *Source) mutate(fn func() bool) {
	m := s.m
	// the lock is released by defer: a panic in fn (a comparison that panics, say) must not leave the model locked
	changed := func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return !s.removed && fn()
	}()
	if !changed {
		return
	}
	m.notify(Event{Kind: SourceChanged, Source: s})
}

// Count is the count the provider gave, or -1.
func (s *Source) Count() int {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.count
}

// StatusBarCommands are the items of the header and the status bar.
func (s *Source) StatusBarCommands() []StatusCommand {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return slices.Clone(s.status)
}

// ActiveRepositoryName is the name the title bar shows while the source control is active: the one the provider set, else Name.
func (s *Source) ActiveRepositoryName() string {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if s.activeName != "" {
		return s.activeName
	}
	return s.Name()
}

// Busy tells whether the progress bar is shown.
func (s *Source) Busy() bool {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.busy
}

// AcceptInputCommand is the command of Ctrl+Enter; the second result is false when there is none.
func (s *Source) AcceptInputCommand() (Command, bool) {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.accept, s.accept.ID != ""
}

// HasActionButton tells whether the provider gave a button under the input.
func (s *Source) HasActionButton() bool {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.button != nil
}

// ActionButton is the button under the input, or nil.
func (s *Source) ActionButton() *ActionButton {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if s.button == nil {
		return nil
	}
	c := *s.button
	c.Secondary = make([][]Command, len(s.button.Secondary))
	for i, g := range s.button.Secondary {
		c.Secondary[i] = slices.Clone(g)
	}
	return &c
}

// Groups are the groups in the order they were created.
func (s *Source) Groups() []*Group {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return slices.Clone(s.groups)
}

// ResourceCount is the number of resources in all the groups (VS Code's getRepositoryResourceCount).
func (s *Source) ResourceCount() int {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.resourceCountLocked()
}

func (s *Source) resourceCountLocked() int {
	n := 0
	for _, g := range s.groups {
		n += len(g.resources)
	}
	return n
}

// BadgeCount is the count of the badge of the repository: what the provider gave, else the number of resources.
func (s *Source) BadgeCount() int {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.badgeLocked()
}

func (s *Source) badgeLocked() int {
	if s.count >= 0 {
		return s.count
	}
	return s.resourceCountLocked()
}

// AddGroup adds a group below the others. The id of a group is not checked here.
func (s *Source) AddGroup(id, label string, hideWhenEmpty bool, contextValue string) *Group {
	m := s.m
	m.mu.Lock()
	if s.removed {
		m.mu.Unlock()
		return nil
	}
	g := &Group{src: s, id: id, label: label, hide: hideWhenEmpty, contextValue: contextValue}
	s.groups = append(s.groups, g)
	m.mu.Unlock()
	m.notify(Event{Kind: GroupsChanged, Source: s, Group: g})
	return g
}

// Group returns the group with the id, or nil.
func (s *Source) Group(id string) *Group {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	for _, g := range s.groups {
		if g.id == id {
			return g
		}
	}
	return nil
}
