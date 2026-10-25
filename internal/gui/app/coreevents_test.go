package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The core events of the extension bus (step 11.2): a workspace publishes what happens to its documents and editors to
// the extensions of its own scope, and to no other.

// eventRecorder is an extension that subscribes to every core event and keeps what it hears, with the scope.
type eventRecorder struct {
	mu     sync.Mutex
	events []heardEvent
}

type heardEvent struct {
	scope string
	ev    extension.Event
}

// factory makes the extension "emetos.recorder".
func (r *eventRecorder) factory() extension.Factory {
	return func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{ID: "emetos.recorder", Name: "Recorder", ActivationEvents: []string{extension.OnStartup}},
			activate: func(_ context.Context, api extension.API) error {
				_, err := api.Events().Subscribe("emetos.**", func(ev extension.Event) {
					r.mu.Lock()
					defer r.mu.Unlock()
					r.events = append(r.events, heardEvent{scope: api.Scope().Name, ev: ev})
				})
				return err
			},
		}
	}
}

// of lists the events of a topic heard in a scope ("" is any scope).
func (r *eventRecorder) of(scope, topic string) []extension.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []extension.Event
	for _, h := range r.events {
		if h.ev.Topic == topic && (scope == "" || h.scope == scope) {
			out = append(out, h.ev)
		}
	}
	return out
}

// topics lists the topics heard in a scope, in order.
func (r *eventRecorder) topics(scope string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, h := range r.events {
		if scope == "" || h.scope == scope {
			out = append(out, h.ev.Topic)
		}
	}
	return out
}

// payload decodes the value of an event.
func payload[T any](t *testing.T, ev extension.Event) T {
	t.Helper()
	v, err := extension.EventValue[T](ev)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// newEventApp makes a workspace with the recorder over the files, and shows it.
func newEventApp(t *testing.T, names []string, open ...string) (*groupApp, *eventRecorder) {
	t.Helper()
	rec := &eventRecorder{}
	p := extensionProcess(t, rec.factory())
	w, err := NewWorkspace(context.Background(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.release)
	return newGroupAppWith(t, w, testWindowSize, names, nil, open...), rec
}

func TestTheCoreEventsFollowTheDocumentsAndTheEditors(t *testing.T) {
	a, rec := newEventApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	aURI, bURI := document.FileURI(a.files["a.go"]).String(), document.FileURI(a.files["b.go"]).String()

	// open: the workspace, then the documents
	if got := rec.topics(""); len(got) < 3 || got[0] != extension.TopicWorkspaceOpened {
		t.Fatalf("events at the start: %v", got)
	}
	opened := rec.of("", extension.TopicDocumentOpened)
	if len(opened) != 2 {
		t.Fatalf("%d documents opened, want 2", len(opened))
	}
	if info := payload[extension.DocumentInfo](t, opened[0]); info.URI != aURI || info.LanguageID != "go" || info.Version != a.doc("a.go").Version() {
		t.Errorf("first document: %+v", info)
	}
	if opened[0].Publisher != extension.CorePublisher || opened[0].Scope.Kind != extension.ScopeWorkspace {
		t.Errorf("event %+v", opened[0])
	}

	// the active editor, and its selection after it
	active := rec.of("", extension.TopicEditorActiveChanged)
	if len(active) != 1 {
		t.Fatalf("%d active editor events: %s %s %s", len(active), active[0].Data, active[1].Data, active[2].Data)
	}
	if got := payload[extension.ActiveEditor](t, active[0]); got.URI != aURI || got.LanguageID != "go" {
		t.Errorf("active editor %+v", got)
	}
	topics := rec.topics("")
	if i, j := slices.Index(topics, extension.TopicEditorActiveChanged), slices.Index(topics, extension.TopicEditorSelectionChanged); i < 0 || j < i {
		t.Errorf("the selection of the new editor does not follow it: %v", topics)
	}
	sel := rec.of("", extension.TopicEditorSelectionChanged)
	if got := payload[extension.EditorSelection](t, sel[len(sel)-1]); got.URI != aURI || len(got.Selections) != 1 || got.Selections[0].Active != (extension.Position{}) {
		t.Errorf("selection %+v", got)
	}

	// another editor in front
	a.clickTab("b.go")
	active = rec.of("", extension.TopicEditorActiveChanged)
	if len(active) != 2 || payload[extension.ActiveEditor](t, active[1]).URI != bURI {
		t.Fatalf("active editor events after the click: %d", len(active))
	}

	// a selection
	before := len(rec.of("", extension.TopicEditorSelectionChanged))
	a.gotoLine("2")
	sels := rec.of("", extension.TopicEditorSelectionChanged)
	if len(sels) <= before {
		t.Fatalf("no selection event for the move of the cursor")
	}
	if got := payload[extension.EditorSelection](t, sels[len(sels)-1]); got.URI != bURI || got.Selections[0].Active.Line != 1 {
		t.Errorf("selection %+v", got)
	}

	// typing 100 characters in one frame is one batch of changes
	doc := a.doc("b.go")
	startVersion := doc.Version()
	for i := range 100 {
		if _, err := doc.Edit([]document.Edit{{Range: buffer.NewRange(buffer.Position{Line: 0, Col: i}, buffer.Position{Line: 0, Col: i}), Text: "x"}}, document.EditOptions{Kind: document.KindTyping}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(rec.of("", extension.TopicDocumentChanged)); n != 0 {
		t.Fatalf("%d change events before the frame ended", n)
	}
	a.pump()
	changed := rec.of("", extension.TopicDocumentChanged)
	if len(changed) != 1 {
		t.Fatalf("typing 100 characters gave %d events, want 1 batch", len(changed))
	}
	batch, err := extension.EventValues[extension.DocumentChange](changed[0])
	if err != nil || len(batch) != 100 {
		t.Fatalf("batch of %d (%v)", len(batch), err)
	}
	for i, c := range batch {
		want := extension.TextChange{Range: extension.Range{Start: extension.Position{Col: i}, End: extension.Position{Col: i}}, Text: "x"}
		if c.URI != bURI || c.Cause != "edit" || c.Version != startVersion+i+1 || len(c.Changes) != 1 || c.Changes[0] != want {
			t.Fatalf("change %d = %+v", i, c)
		}
	}

	// an undo
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if changed = rec.of("", extension.TopicDocumentChanged); len(changed) != 2 || payload[extension.DocumentChange](t, changed[1]).Cause != "undo" {
		t.Errorf("events after the undo: %d", len(changed))
	}

	// a save, and a save as
	if _, err := doc.Save(context.Background(), document.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	saved := rec.of("", extension.TopicDocumentSaved)
	if len(saved) != 1 {
		t.Fatalf("%d saved events", len(saved))
	}
	if got := payload[extension.DocumentSaved](t, saved[0]); got.URI != bURI || got.Version != doc.Version() || got.As || got.PreviousURI != "" {
		t.Errorf("saved %+v", got)
	}
	newPath := filepath.Join(filepath.Dir(a.files["b.go"]), "renamed.go")
	if _, err := doc.SaveAs(context.Background(), newPath, document.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	newURI := document.FileURI(newPath).String()
	saved = rec.of("", extension.TopicDocumentSaved)
	if len(saved) != 2 {
		t.Fatalf("%d saved events after Save As", len(saved))
	}
	if got := payload[extension.DocumentSaved](t, saved[1]); got.URI != newURI || !got.As || got.PreviousURI != bURI {
		t.Errorf("saved as %+v", got)
	}

	// close: the document goes, and the other editor is in front
	a.run("workbench.action.closeActiveEditor")
	closed := rec.of("", extension.TopicDocumentClosed)
	if len(closed) != 1 || payload[extension.DocumentClosed](t, closed[0]).URI != newURI {
		t.Fatalf("closed events: %d", len(closed))
	}
	active = rec.of("", extension.TopicEditorActiveChanged)
	if got := payload[extension.ActiveEditor](t, active[len(active)-1]); got.URI != aURI {
		t.Errorf("active editor after the close %+v", got)
	}

	// nothing is left of the tab: no editor, no active URI
	a.run("workbench.action.closeActiveEditor")
	active = rec.of("", extension.TopicEditorActiveChanged)
	if got := payload[extension.ActiveEditor](t, active[len(active)-1]); got.URI != "" {
		t.Errorf("active editor with none open: %+v", got)
	}
	a.noErrors()
}

func TestTheWorkspaceClosesWithAnEventBeforeTheExtensionsGo(t *testing.T) {
	rec := &eventRecorder{}
	p := extensionProcess(t, rec.factory())
	folder := t.TempDir()
	w, err := NewWorkspace(context.Background(), p, folder)
	if err != nil {
		t.Fatal(err)
	}
	opened := rec.of("", extension.TopicWorkspaceOpened)
	if len(opened) != 1 {
		t.Fatalf("%d opened events", len(opened))
	}
	if info := payload[extension.WorkspaceInfo](t, opened[0]); info.Folder != folder || info.Name != filepath.Base(folder) || info.Kind != extension.ScopeWorkspace {
		t.Errorf("workspace %+v", info)
	}
	w.release()
	if got := rec.of("", extension.TopicWorkspaceClosed); len(got) != 1 {
		t.Errorf("%d closed events", len(got))
	}
}

func TestTheCoreEventsOfOneWorkspaceReachItsExtensionsOnly(t *testing.T) {
	rec := &eventRecorder{}
	env := newMultiEnv(t, nil)
	one, two := env.folder("one", "a.go"), env.folder("two", "b.go")
	a := env.runWith(Options{Folder: one}, ProcessOptions{Extensions: []extension.Factory{rec.factory()}})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)

	// the files of each workspace are opened in it: the other one hears nothing of them
	edit := func(w *Workspace, name string) *document.Document {
		doc, err := w.Documents.OpenFile(context.Background(), filepath.Join(w.Folder, name), document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}
	d1, d2 := edit(w1, "a.go"), edit(w2, "b.go")
	a.settle()
	for _, d := range []*document.Document{d1, d2} {
		if _, err := d.Edit([]document.Edit{{Range: buffer.NewRange(buffer.Position{}, buffer.Position{}), Text: "y"}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	a.settle()
	for _, c := range []struct {
		scope string
		doc   *document.Document
	}{{filepath.Base(one), d1}, {filepath.Base(two), d2}} {
		uri := c.doc.URI().String()
		for _, topic := range []string{extension.TopicDocumentOpened, extension.TopicDocumentChanged} {
			evs := rec.of(c.scope, topic)
			if len(evs) != 1 {
				t.Fatalf("%s heard %d %s events, want 1", c.scope, len(evs), topic)
			}
			if !strings.Contains(string(evs[0].Data), uri) {
				t.Errorf("%s heard %s of another workspace: %s", c.scope, topic, evs[0].Data)
			}
			if evs[0].Scope.Folder == "" || !strings.HasSuffix(evs[0].Scope.Folder, c.scope) {
				t.Errorf("%s: the scope of the event is %+v", c.scope, evs[0].Scope)
			}
		}
	}

	// the window tab that comes to the front says so; the one that is behind does not
	activated := func(scope string) int { return len(rec.of(scope, extension.TopicWindowTabActivated)) }
	nameOne, nameTwo := filepath.Base(one), filepath.Base(two)
	if activated(nameTwo) != 1 {
		t.Errorf("%d activations of the tab that was opened last, want 1", activated(nameTwo))
	}
	before := activated(nameOne)
	if !a.m.Activate(w1.Tab()) {
		t.Fatal("the first tab cannot be activated")
	}
	a.settle()
	if activated(nameOne) != before+1 {
		t.Errorf("the first tab came to the front and heard %d activations (had %d)", activated(nameOne), before)
	}
	if activated(nameTwo) != 1 {
		t.Errorf("the tab behind heard its activation again: %d", activated(nameTwo))
	}
	if info := payload[extension.WorkspaceInfo](t, rec.of(nameOne, extension.TopicWindowTabActivated)[before]); info.Folder != one {
		t.Errorf("tab %+v", info)
	}
	a.t.Helper()
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}
