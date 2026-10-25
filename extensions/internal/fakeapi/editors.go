package fakeapi

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The editor contributions of the fake: editor kinds and their tabs, file views and the languages.

// Tab is an editor tab the fake made: it implements extension.EditorTab and records what the extension did to it.
type Tab struct {
	mu               sync.Mutex
	id, kind         string
	title, hint      string
	dirty, busy      bool
	Refreshs         int
	Closed, Revealed bool
	Content          extension.EditorTabContent
	Group_           int
	a                *API
}

// ID implements extension.EditorTab.
func (t *Tab) ID() string { return t.id }

// Kind implements extension.EditorTab.
func (t *Tab) Kind() string { return t.kind }

// SetTitle implements extension.EditorTab.
func (t *Tab) SetTitle(title string) { t.mu.Lock(); t.title = title; t.mu.Unlock() }

// SetHint implements extension.EditorTab.
func (t *Tab) SetHint(hint string) { t.mu.Lock(); t.hint = hint; t.mu.Unlock() }

// SetDirty implements extension.EditorTab.
func (t *Tab) SetDirty(dirty bool) { t.mu.Lock(); t.dirty = dirty; t.mu.Unlock() }

// SetBusy implements extension.EditorTab.
func (t *Tab) SetBusy(busy bool) { t.mu.Lock(); t.busy = busy; t.mu.Unlock() }

// Refresh implements extension.EditorTab.
func (t *Tab) Refresh() { t.mu.Lock(); t.Refreshs++; t.mu.Unlock() }

// Reveal implements extension.EditorTab.
func (t *Tab) Reveal(bool) { t.mu.Lock(); t.Revealed = true; t.mu.Unlock() }

// Group implements extension.EditorTab.
func (t *Tab) Group() int { return t.Group_ }

// Close implements extension.EditorTab: the fake closes the tab at once and calls OnClose.
func (t *Tab) Close() {
	t.mu.Lock()
	already := t.Closed
	t.Closed = true
	t.mu.Unlock()
	if !already && t.Content.OnClose != nil {
		t.Content.OnClose()
	}
}

// Title is the title the extension set.
func (t *Tab) Title() string { t.mu.Lock(); defer t.mu.Unlock(); return t.title }

// Hint is the tooltip the extension set.
func (t *Tab) Hint() string { t.mu.Lock(); defer t.mu.Unlock(); return t.hint }

// Dirty is the dirty mark.
func (t *Tab) Dirty() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.dirty }

// Busy is the busy mark.
func (t *Tab) Busy() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.busy }

// State asks the content for the state it saves, as JSON.
func (t *Tab) State() json.RawMessage {
	if t.Content.SaveState == nil {
		return nil
	}
	v, err := t.Content.SaveState()
	if err != nil || v == nil {
		return nil
	}
	data, _ := json.Marshal(v)
	return data
}

type editors struct{ a *API }

// Editors implements extension.API.
func (a *API) Editors() extension.Editors { return editors{a} }

// Provide implements extension.Editors.
func (e editors) Provide(kindID string, kind extension.EditorKind) error {
	declared := false
	for _, k := range e.a.manifest.Contributes.EditorKinds {
		declared = declared || k.ID == kindID
	}
	if !declared {
		return errors.New("FakeEditorKindNotDeclared", "the manifest does not declare the editor kind", "kind", kindID)
	}
	if kind.Create == nil {
		return errors.New("FakeEditorKindInvalid", "the kind has no Create function", "kind", kindID)
	}
	e.a.mu.Lock()
	if e.a.kinds == nil {
		e.a.kinds = map[string]extension.EditorKind{}
	}
	e.a.kinds[kindID] = kind
	e.a.mu.Unlock()
	return nil
}

// Open implements extension.Editors.
func (e editors) Open(kindID string, state any, opts extension.EditorOpenOptions) (extension.EditorTab, error) {
	var raw json.RawMessage
	if state != nil {
		data, err := json.Marshal(state)
		if err != nil {
			return nil, errors.NewErr("FakeEditorStateInvalid", err)
		}
		raw = data
	}
	t, err := e.a.makeTab(kindID, "", raw)
	if err != nil {
		return nil, err
	}
	t.Group_ = opts.Group
	return t, nil
}

// Tabs implements extension.Editors.
func (e editors) Tabs(kindID string) []extension.EditorTab {
	e.a.mu.Lock()
	defer e.a.mu.Unlock()
	var out []extension.EditorTab
	for _, t := range e.a.tabs {
		if t.kind == kindID && !t.Closed {
			out = append(out, t)
		}
	}
	return out
}

// makeTab makes a tab of a kind with the state and keeps it; id is empty for a new tab.
func (a *API) makeTab(kindID, id string, raw json.RawMessage) (*Tab, error) {
	a.mu.Lock()
	kind, ok := a.kinds[kindID]
	if id == "" {
		id = "tab" + string(rune('a'+len(a.tabs)))
	}
	a.mu.Unlock()
	if !ok {
		return nil, errors.New("FakeEditorKindNotProvided", "the extension did not provide the editor kind", "kind", kindID)
	}
	t := &Tab{id: id, kind: kindID, a: a}
	content, err := kind.Create(t, raw)
	if err != nil {
		return nil, err
	}
	t.Content = content
	a.mu.Lock()
	a.tabs = append(a.tabs, t)
	a.mu.Unlock()
	return t, nil
}

// RestoreTab makes a tab as the window does for a tab that came back from the last session: Create gets the state the tab
// saved.
func (a *API) RestoreTab(kindID, id string, state json.RawMessage) *Tab {
	a.t.Helper()
	t, err := a.makeTab(kindID, id, state)
	if err != nil {
		a.t.Fatalf("restore tab: %v", err)
	}
	return t
}

// EditorTabs are the tabs the fake holds, in the order they were made.
func (a *API) EditorTabs() []*Tab {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.tabs)
}

// FileViewOpen is a call of FileViews().Open.
type FileViewOpen struct {
	URI, View string
	Side      bool
	Group     int
}

type fileViews struct{ a *API }

// FileViews implements extension.API.
func (a *API) FileViews() extension.FileViews { return fileViews{a} }

// Provide implements extension.FileViews.
func (f fileViews) Provide(viewID string, p extension.FileViewProvider) error {
	declared := false
	for _, v := range f.a.manifest.Contributes.FileViews {
		declared = declared || v.ID == viewID
	}
	if !declared {
		return errors.New("FakeFileViewNotDeclared", "the manifest does not declare the file view", "view", viewID)
	}
	if p.Open == nil {
		return errors.New("FakeFileViewInvalid", "the provider has no Open function", "view", viewID)
	}
	f.a.mu.Lock()
	if f.a.fileViews == nil {
		f.a.fileViews = map[string]extension.FileViewProvider{}
	}
	f.a.fileViews[viewID] = p
	f.a.mu.Unlock()
	return nil
}

// Open implements extension.FileViews: the call is recorded.
func (f fileViews) Open(uri, viewID string, opts extension.FileViewOpenOptions) error {
	f.a.mu.Lock()
	f.a.fileViewOpens = append(f.a.fileViewOpens, FileViewOpen{URI: uri, View: viewID, Side: opts.Side, Group: opts.Group})
	f.a.mu.Unlock()
	return nil
}

// FileViewOpens are the calls of FileViews().Open.
func (a *API) FileViewOpens() []FileViewOpen {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.fileViewOpens)
}

// OpenFileView makes the content of the file view the extension provided, as the window does when a tab shows it.
func (a *API) OpenFileView(viewID string, doc extension.DocumentHandle) (extension.FileViewContent, error) {
	a.mu.Lock()
	p, ok := a.fileViews[viewID]
	a.mu.Unlock()
	if !ok {
		return extension.FileViewContent{}, errors.New("FakeFileViewNotProvided", "the extension did not provide the file view", "view", viewID)
	}
	return p.Open(doc, fileTab{a: a})
}

type fileTab struct{ a *API }

// Refresh implements extension.FileViewTab: it is counted (FileViewRefreshes).
func (t fileTab) Refresh() { t.a.mu.Lock(); t.a.fileViewRefreshes++; t.a.mu.Unlock() }

// FileViewRefreshes is how many times a file view asked to be built again.
func (a *API) FileViewRefreshes() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fileViewRefreshes
}

type languages struct{ a *API }

// Languages implements extension.API.
func (a *API) Languages() extension.Languages { return languages{a} }

// Resolve implements extension.Languages: the name is the id.
func (l languages) Resolve(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	return strings.ToLower(name), true
}

// Tokenize implements extension.Languages: every line is one token with the scope source.<language>, or the function the
// test set with SetTokenizer.
func (l languages) Tokenize(languageID, text string) ([][]extension.Token, error) {
	l.a.mu.Lock()
	fn := l.a.tokenizer
	l.a.mu.Unlock()
	if fn != nil {
		return fn(languageID, text)
	}
	var out [][]extension.Token
	for _, line := range strings.Split(text, "\n") {
		out = append(out, []extension.Token{{Start: 0, End: len(line), Scopes: []string{"source." + languageID}}})
	}
	return out, nil
}

// SetTokenizer sets the function Languages().Tokenize calls.
func (a *API) SetTokenizer(fn func(languageID, text string) ([][]extension.Token, error)) {
	a.mu.Lock()
	a.tokenizer = fn
	a.mu.Unlock()
}

// Document is a document handle for a test of a file view: it holds a text, records the calls and lets the test change the
// text and scroll the editor.
type Document struct {
	mu                sync.Mutex
	uri, lang, folder string
	text              string
	version           int
	top               int
	onChange          []func(extension.ViewDocumentChange)
	onScroll          []func(int)
	// Edits are the batches the view applied, and Opened the files it opened.
	Edits  [][]extension.TextEdit
	Opened []string
}

// NewDocument makes a document with a URI, a language, the folder of the workspace and a text.
func NewDocument(uri, lang, folder, text string) *Document {
	return &Document{uri: uri, lang: lang, folder: folder, text: text, version: 1}
}

// URI implements extension.DocumentHandle.
func (d *Document) URI() string { return d.uri }

// LanguageID implements extension.DocumentHandle.
func (d *Document) LanguageID() string { return d.lang }

// Version implements extension.DocumentHandle.
func (d *Document) Version() int { d.mu.Lock(); defer d.mu.Unlock(); return d.version }

// Text implements extension.DocumentHandle.
func (d *Document) Text() string { d.mu.Lock(); defer d.mu.Unlock(); return d.text }

// ReadOnly implements extension.DocumentHandle.
func (d *Document) ReadOnly() bool { return false }

// Folder implements extension.DocumentHandle.
func (d *Document) Folder() string { return d.folder }

// Edit implements extension.DocumentHandle: the batch is recorded.
func (d *Document) Edit(edits []extension.TextEdit) error {
	d.mu.Lock()
	d.Edits = append(d.Edits, slices.Clone(edits))
	d.mu.Unlock()
	return nil
}

// OnChange implements extension.DocumentHandle.
func (d *Document) OnChange(fn func(extension.ViewDocumentChange)) func() {
	d.mu.Lock()
	d.onChange = append(d.onChange, fn)
	i := len(d.onChange) - 1
	d.mu.Unlock()
	return func() { d.mu.Lock(); d.onChange[i] = nil; d.mu.Unlock() }
}

// TopLine implements extension.DocumentHandle.
func (d *Document) TopLine() int { d.mu.Lock(); defer d.mu.Unlock(); return d.top }

// OnScroll implements extension.DocumentHandle.
func (d *Document) OnScroll(fn func(int)) func() {
	d.mu.Lock()
	d.onScroll = append(d.onScroll, fn)
	i := len(d.onScroll) - 1
	d.mu.Unlock()
	return func() { d.mu.Lock(); d.onScroll[i] = nil; d.mu.Unlock() }
}

// OpenFile implements extension.DocumentHandle: the path is recorded.
func (d *Document) OpenFile(path string) error {
	d.mu.Lock()
	d.Opened = append(d.Opened, path)
	d.mu.Unlock()
	return nil
}

// SetText changes the text and tells the listeners, as an edit of the editor does.
func (d *Document) SetText(text string) {
	d.mu.Lock()
	d.text = text
	d.version++
	version := d.version
	fns := slices.Clone(d.onChange)
	d.mu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn(extension.ViewDocumentChange{Version: version, Cause: "edit"})
		}
	}
}

// Scroll scrolls the Text view to a line and tells the listeners.
func (d *Document) Scroll(line int) {
	d.mu.Lock()
	d.top = line
	fns := slices.Clone(d.onScroll)
	d.mu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn(line)
		}
	}
}

// Listeners are how many callbacks of OnChange and OnScroll are still registered.
func (d *Document) Listeners() (change, scroll int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, fn := range d.onChange {
		if fn != nil {
			change++
		}
	}
	for _, fn := range d.onScroll {
		if fn != nil {
			scroll++
		}
	}
	return change, scroll
}

var _ extension.DocumentHandle = (*Document)(nil)
