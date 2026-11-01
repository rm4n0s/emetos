package app

import (
	"context"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/mergeeditor"
)

// What an extension sees of the merge editor (step 24.14): it opens a file in it (OpenSpec.MergeEditor), finds the one in front
// (Documents().ActiveMergeEditor), and hears that a merge was completed (emetos.mergeEditor.completed).

// mergeExt is an extension that follows the merge editor, and the merge app it runs in.
type mergeExt struct {
	*mergeApp
	api extension.API

	mu        sync.Mutex
	completed []extension.MergeEditorCompleted
	// textAtEvent is the text of the file on disk when the event came.
	textAtEvent []string
}

func newMergeExt(t *testing.T, texts map[string]string) *mergeExt {
	t.Helper()
	m := &mergeExt{}
	factory := func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{ID: "emetos.mergetest", Name: "Merge test", ActivationEvents: []string{extension.OnStartup}},
			activate: func(_ context.Context, api extension.API) error {
				m.api = api
				_, err := api.Events().Subscribe(extension.TopicMergeEditorCompleted, func(ev extension.Event) {
					done, err := extension.EventValue[extension.MergeEditorCompleted](ev)
					if err != nil {
						t.Error(err)
						return
					}
					m.mu.Lock()
					defer m.mu.Unlock()
					m.completed = append(m.completed, done)
					if u, err := document.ParseURI(done.URI); err == nil {
						m.textAtEvent = append(m.textAtEvent, readFileIf(u.Path))
					}
				})
				return err
			},
		}
	}
	p := extensionProcess(t, factory)
	w, err := NewWorkspace(context.Background(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.release)
	names := make([]string, 0, len(texts))
	for n := range texts {
		names = append(names, n)
	}
	m.mergeApp = &mergeApp{newSaveAppWith(t, w, names, texts)}
	if m.api == nil {
		t.Fatal("the extension did not start")
	}
	return m
}

func (m *mergeExt) events() []extension.MergeEditorCompleted {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]extension.MergeEditorCompleted(nil), m.completed...)
}

func TestAnExtensionOpensAFileInTheMergeEditor(t *testing.T) {
	m := newMergeExt(t, map[string]string{"c.go": markerFile, "plain.go": "package x\n"})
	if _, ok := m.api.Documents().ActiveMergeEditor(); ok {
		t.Fatal("a merge editor is in front before any is open")
	}
	uri := document.FileURI(m.files["c.go"]).String()
	if err := m.api.Documents().OpenDocument(extension.OpenSpec{URI: uri, MergeEditor: true}); err != nil {
		t.Fatal(err)
	}
	m.waitMerge()
	got, ok := m.api.Documents().ActiveMergeEditor()
	if !ok || got.Result != uri || !got.Markers {
		t.Errorf("ActiveMergeEditor = %+v, %v", got, ok)
	}
	// no text editor of the file opened first: the merge editor is the only tab
	wantNames(t, m.tabNames(), "Merging: c.go")
	if m.mergeNotes() != 0 {
		t.Errorf("the notification that offers the merge editor came with the merge editor itself")
	}
	t.Run("a file without conflicts says so", func(t *testing.T) {
		m := newMergeExt(t, map[string]string{"plain.go": "package x\n"})
		if err := m.api.Documents().OpenDocument(extension.OpenSpec{URI: document.FileURI(m.files["plain.go"]).String(), MergeEditor: true}); err != nil {
			t.Fatal(err)
		}
		m.waitFor("the notice", func() bool {
			for _, n := range m.notes() {
				if n.Message == MergeNoConflictsNotice {
					return true
				}
			}
			return false
		})
		if _, ok := m.api.Documents().ActiveMergeEditor(); ok || m.pane() != nil {
			t.Error("a merge editor opened for a file without conflicts")
		}
	})
	m.noErrors()
}

func TestAnExtensionNamesTheSidesOfTheMergeEditor(t *testing.T) {
	tests := []struct {
		name              string
		labels            *extension.MergeLabels
		incoming, current string
	}{
		{"the names of the markers", nil, "feature", "HEAD"},
		{"both sides named", &extension.MergeLabels{Current: "Workspace", Incoming: "Worktree"}, "Worktree", "Workspace"},
		{"one side named", &extension.MergeLabels{Incoming: "Worktree"}, "Worktree", "HEAD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newMergeExt(t, map[string]string{"c.go": markerFile})
			uri := document.FileURI(m.files["c.go"]).String()
			if err := m.api.Documents().OpenDocument(extension.OpenSpec{URI: uri, MergeEditor: true, MergeLabels: tc.labels}); err != nil {
				t.Fatal(err)
			}
			m.waitMerge()
			model := m.pane().Model()
			if got, want := model.Label(mergeeditor.Incoming), tc.incoming; got != want {
				t.Errorf("incoming = %q, want %q", got, want)
			}
			if got, want := model.Label(mergeeditor.Current), tc.current; got != want {
				t.Errorf("current = %q, want %q", got, want)
			}
		})
	}
}

func TestCompletingAMergeIsToldToTheExtensions(t *testing.T) {
	t.Run("all conflicts handled", func(t *testing.T) {
		m := newMergeExt(t, map[string]string{"c.go": markerFile})
		uri := document.FileURI(m.files["c.go"]).String()
		p := m.resolve("c.go")
		if err := p.Model().AcceptAll(diff.Input1); err != nil {
			t.Fatal(err)
		}
		m.pump()
		if err := m.runWith("mergeEditor.acceptMerge", nil); err != nil {
			t.Fatal(err)
		}
		m.waitMergeClosed()
		m.waitFor("the event", func() bool { m.pump(); return len(m.events()) == 1 })
		if got := m.events(); got[0].URI != uri {
			t.Errorf("event %+v, want %s", got, uri)
		}
		// the result was saved before the event: a subscriber that stages the file stages the resolved one
		m.mu.Lock()
		defer m.mu.Unlock()
		if len(m.textAtEvent) != 1 || m.textAtEvent[0] != resolvedFile {
			t.Errorf("the file at the event: %q", m.textAtEvent)
		}
	})
	t.Run("completed with conflicts", func(t *testing.T) {
		m := newMergeExt(t, map[string]string{"c.go": markerFile})
		m.resolve("c.go")
		if err := m.runWith("mergeEditor.acceptMerge", nil); err != nil {
			t.Fatal(err)
		}
		m.tapDialog("Complete with Conflicts")
		m.waitMergeClosed()
		m.waitFor("the event", func() bool { m.pump(); return len(m.events()) == 1 })
	})
	t.Run("a merge that is closed or cancelled is no event", func(t *testing.T) {
		m := newMergeExt(t, map[string]string{"c.go": markerFile})
		m.resolve("c.go")
		if err := m.runWith("mergeEditor.acceptMerge", nil); err != nil {
			t.Fatal(err)
		}
		m.tapDialog("Cancel")
		m.run("workbench.action.closeActiveEditor")
		m.pump()
		if got := m.events(); len(got) != 0 {
			t.Errorf("events %+v", got)
		}
	})
}
