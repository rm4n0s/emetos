package fakeapi

import (
	"encoding/json"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The window tab of the fake: the one tab of the scope the extension runs in.

// WindowTab is the window tab the fake made: it implements extension.WindowTab and records what the extension did to it.
type WindowTab struct {
	mu          sync.Mutex
	id, kind    string
	title, hint string
	// Refreshs, Changes and Closes count the calls of Refresh, StateChanged and Close.
	Refreshs, Changes, Closes int
	// Capturing is what the extension last set with CaptureKeys.
	Capturing bool
	// Content is what the extension made the tab of.
	Content extension.WindowTabContent
}

// ID implements extension.WindowTab.
func (t *WindowTab) ID() string { return t.id }

// Kind implements extension.WindowTab.
func (t *WindowTab) Kind() string { return t.kind }

// SetTitle implements extension.WindowTab.
func (t *WindowTab) SetTitle(title string) { t.mu.Lock(); t.title = title; t.mu.Unlock() }

// SetHint implements extension.WindowTab.
func (t *WindowTab) SetHint(hint string) { t.mu.Lock(); t.hint = hint; t.mu.Unlock() }

// StateChanged implements extension.WindowTab.
func (t *WindowTab) StateChanged() { t.mu.Lock(); t.Changes++; t.mu.Unlock() }

// CaptureKeys implements extension.WindowTab: the value is kept (Capturing).
func (t *WindowTab) CaptureKeys(capture bool) { t.mu.Lock(); t.Capturing = capture; t.mu.Unlock() }

// Refresh implements extension.WindowTab.
func (t *WindowTab) Refresh() { t.mu.Lock(); t.Refreshs++; t.mu.Unlock() }

// Close implements extension.WindowTab: the fake counts the call; CloseTab ends the tab.
func (t *WindowTab) Close() { t.mu.Lock(); t.Closes++; t.mu.Unlock() }

// Title is the title the extension set.
func (t *WindowTab) Title() string { t.mu.Lock(); defer t.mu.Unlock(); return t.title }

// Hint is the tooltip the extension set.
func (t *WindowTab) Hint() string { t.mu.Lock(); defer t.mu.Unlock(); return t.hint }

// State asks the content for the state it saves, as JSON.
func (t *WindowTab) State() json.RawMessage {
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

type windowTabs struct{ a *API }

// WindowTabs implements extension.API.
func (a *API) WindowTabs() extension.WindowTabs { return windowTabs{a} }

// Provide implements extension.WindowTabs: the kind is kept, and the tab is made by StartWindowTab.
func (w windowTabs) Provide(kindID string, kind extension.WindowTabKind) error {
	declared := false
	for _, k := range w.a.manifest.Contributes.WindowTabKinds {
		declared = declared || k.ID == kindID
	}
	if !declared {
		return errors.New("FakeWindowTabKindNotDeclared", "the manifest does not declare the window tab kind", "kind", kindID)
	}
	if kind.Create == nil {
		return errors.New("FakeWindowTabKindInvalid", "the kind has no Create function", "kind", kindID)
	}
	w.a.mu.Lock()
	w.a.windowKind = &kind
	w.a.windowKindID = kindID
	w.a.mu.Unlock()
	return nil
}

// StartWindowTab makes the tab of the kind the extension provided, as the window does when the tab opens or comes back:
// Create gets the state the tab saved (nil for a new tab).
func (a *API) StartWindowTab(id string, state json.RawMessage) *WindowTab {
	a.t.Helper()
	a.mu.Lock()
	kind, kindID := a.windowKind, a.windowKindID
	a.mu.Unlock()
	if kind == nil {
		a.t.Fatal("the extension did not provide a window tab kind")
	}
	t := &WindowTab{id: id, kind: kindID}
	content, err := kind.Create(t, state)
	if err != nil {
		a.t.Fatalf("start window tab: %v", err)
	}
	t.Content = content
	a.mu.Lock()
	a.windowTab = t
	a.mu.Unlock()
	return t
}

// CloseWindowTab ends the tab as the window does when the user closes it: OnClose is called.
func (a *API) CloseWindowTab() {
	a.mu.Lock()
	t := a.windowTab
	a.mu.Unlock()
	if t != nil && t.Content.OnClose != nil {
		t.Content.OnClose()
	}
}
