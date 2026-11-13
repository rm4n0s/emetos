package fakeapi

import (
	"encoding/json"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// File editors (SPEC §10.37): the providers the extension gives, and OpenFileEditor, which opens a file in one as the window does when
// a tab shows it.

type fileEditors struct{ a *API }

// FileEditors implements extension.API.
func (a *API) FileEditors() extension.FileEditors { return fileEditors{a} }

// Provide implements extension.FileEditors with the errors of the host for an editor the manifest does not declare
// (ExtensionFileEditorNotDeclared) and a provider without Open (ExtensionFileEditorInvalid).
func (f fileEditors) Provide(editorID string, p extension.FileEditorProvider) error {
	declared := false
	for _, e := range f.a.manifest.Contributes.FileEditors {
		declared = declared || e.ID == editorID
	}
	if !declared {
		return errors.New("ExtensionFileEditorNotDeclared", "the manifest of the extension does not declare this file editor", "editor", editorID)
	}
	if p.Open == nil {
		return errors.New("ExtensionFileEditorInvalid", "a file editor needs an Open function", "editor", editorID)
	}
	f.a.mu.Lock()
	if f.a.fileEditors == nil {
		f.a.fileEditors = map[string]extension.FileEditorProvider{}
	}
	f.a.fileEditors[editorID] = p
	f.a.mu.Unlock()
	return nil
}

// FakeFileEditor is a file opened in a file editor of the extension (OpenFileEditor): the content it made, and the test's side of its
// file and tab.
type FakeFileEditor struct {
	a       *API
	path    string
	folder  string
	state   json.RawMessage
	changes []*func(extension.FileEditorChange)
	status  []extension.StatusItem
	busy    bool
	// Content is what the provider's Open returned.
	Content extension.FileEditorContent
	refresh int
	group   int
}

// OpenFileEditor opens the file at path in the file editor with the id, with the state of a tab that comes back (nil for a new one),
// as the window does. It fails with FakeFileEditorNotProvided when the extension did not provide the editor, and with the error of
// Open.
func (a *API) OpenFileEditor(editorID, path string, state json.RawMessage) (*FakeFileEditor, error) {
	a.mu.Lock()
	p, ok := a.fileEditors[editorID]
	a.mu.Unlock()
	if !ok {
		return nil, errors.New("FakeFileEditorNotProvided", "the extension did not provide the file editor", "editor", editorID)
	}
	fe := &FakeFileEditor{a: a, path: path, state: state, group: 1}
	content, err := p.Open(fakeEditorFile{fe}, fakeEditorTab{fe})
	if err != nil {
		return nil, err
	}
	fe.Content = content
	return fe, nil
}

// Change tells the editor that its file changed on disk (deleted: it is gone), as the window does.
func (f *FakeFileEditor) Change(deleted bool) {
	f.a.mu.Lock()
	calls := slices.Clone(f.changes)
	f.a.mu.Unlock()
	for _, c := range calls {
		(*c)(extension.FileEditorChange{Deleted: deleted})
	}
}

// Status are the status bar items the editor set last.
func (f *FakeFileEditor) Status() []extension.StatusItem {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return slices.Clone(f.status)
}

// Busy tells whether the editor marked its tab busy.
func (f *FakeFileEditor) Busy() bool {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return f.busy
}

// Refreshes is how many times the editor asked to be built again.
func (f *FakeFileEditor) Refreshes() int {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return f.refresh
}

// SaveState calls the content's SaveState and gives its JSON (nil without one).
func (f *FakeFileEditor) SaveState() (json.RawMessage, error) {
	if f.Content.SaveState == nil {
		return nil, nil
	}
	v, err := f.Content.SaveState()
	if err != nil || v == nil {
		return nil, err
	}
	return json.Marshal(v)
}

// Close closes the tab: OnClose is called and the listeners of the file stop.
func (f *FakeFileEditor) Close() {
	f.a.mu.Lock()
	f.changes = nil
	f.a.mu.Unlock()
	if f.Content.OnClose != nil {
		f.Content.OnClose()
	}
}

type fakeEditorFile struct{ f *FakeFileEditor }

func (x fakeEditorFile) Path() string   { return x.f.path }
func (x fakeEditorFile) URI() string    { return "file://" + x.f.path }
func (x fakeEditorFile) Folder() string { return x.f.folder }
func (x fakeEditorFile) State() json.RawMessage {
	return x.f.state
}
func (x fakeEditorFile) OnChange(fn func(extension.FileEditorChange)) (cancel func()) {
	p := &fn
	x.f.a.mu.Lock()
	x.f.changes = append(x.f.changes, p)
	x.f.a.mu.Unlock()
	return func() {
		x.f.a.mu.Lock()
		defer x.f.a.mu.Unlock()
		x.f.changes = slices.DeleteFunc(x.f.changes, func(c *func(extension.FileEditorChange)) bool { return c == p })
	}
}

type fakeEditorTab struct{ f *FakeFileEditor }

func (x fakeEditorTab) SetBusy(busy bool) { x.f.a.mu.Lock(); x.f.busy = busy; x.f.a.mu.Unlock() }
func (x fakeEditorTab) Refresh()          { x.f.a.mu.Lock(); x.f.refresh++; x.f.a.mu.Unlock() }
func (x fakeEditorTab) SetStatus(items []extension.StatusItem) {
	x.f.a.mu.Lock()
	x.f.status = slices.Clone(items)
	x.f.a.mu.Unlock()
}
func (x fakeEditorTab) Group() int { return x.f.group }
