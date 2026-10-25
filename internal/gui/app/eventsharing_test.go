package app

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/extensionsui"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The event sharing between window tabs in the window (step 11.3): the question in the receiving tab, the file that keeps
// the links, and the page "Extensions > Event Sharing".

// sharer is the extension "emetos.sharer": it listens to the document events of the other tabs (it declares them as
// cross-tab topics) and keeps what it hears, with the tab it heard it in.
type sharer struct {
	mu     sync.Mutex
	events []heardEvent
}

func (s *sharer) factory() extension.Factory {
	return func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{
				ID: "emetos.sharer", Name: "Sharer", ActivationEvents: []string{extension.OnStartup},
				Events: extension.EventDecl{CrossTab: []string{"emetos.document.*", extension.TopicSharingRevoked}},
			},
			activate: func(_ context.Context, api extension.API) error {
				record := func(ev extension.Event) {
					s.mu.Lock()
					defer s.mu.Unlock()
					s.events = append(s.events, heardEvent{scope: api.Scope().Name, ev: ev})
				}
				if _, err := api.Events().Subscribe("emetos.document.opened", record, extension.FromOtherScopes()); err != nil {
					return err
				}
				_, err := api.Events().Subscribe(extension.TopicSharingRevoked, record)
				return err
			},
		}
	}
}

// heard lists the topics heard in a tab, with the tab each was published in.
func (s *sharer) heard(tab string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, h := range s.events {
		if h.scope == tab {
			out = append(out, h.ev.Topic+"@"+h.ev.Scope.Name)
		}
	}
	return out
}

// sharingWorld is a process with the sharer and two workspaces over folders named notes and office.
type sharingWorld struct {
	t             *testing.T
	p             *Process
	sharer        *sharer
	notes, office *Workspace
	paths         storage.Paths
	root          string
	logs          *bytes.Buffer
}

func newSharingWorld(t *testing.T) *sharingWorld {
	t.Helper()
	return openSharingWorld(t, testutil.Isolate(t), &sharer{}, t.TempDir())
}

// openSharingWorld makes the process over the paths: a second call over the same paths is a restart of Emetos.
func openSharingWorld(t *testing.T, paths storage.Paths, s *sharer, root string, extra ...map[string]any) *sharingWorld {
	t.Helper()
	settings := map[string]any{"files.hotExit": "off"}
	for _, m := range extra {
		for k, v := range m {
			settings[k] = v
		}
	}
	testutil.Settings(t, paths, settings)
	w := &sharingWorld{t: t, sharer: s, paths: paths, root: root, logs: &bytes.Buffer{}}
	w.p = newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{s.factory()}, Logger: slog.New(slog.NewTextHandler(w.logs, nil))})
	for _, name := range []string{"notes", "office"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.notes = newExtensionWorkspace(t, w.p, filepath.Join(w.root, "notes"))
	w.office = newExtensionWorkspace(t, w.p, filepath.Join(w.root, "office"))
	return w
}

// openIn opens a new file in the folder of the workspace, which publishes emetos.document.opened there.
func (w *sharingWorld) openIn(ws *Workspace, name string) {
	w.t.Helper()
	path := filepath.Join(ws.Folder, name)
	if err := os.WriteFile(path, []byte("text\n"), 0o644); err != nil {
		w.t.Fatal(err)
	}
	if _, err := ws.Documents.OpenFile(context.Background(), path, document.NewOptions()); err != nil {
		w.t.Fatal(err)
	}
}

// answer presses a button of the dialog the workspace shows.
func answer(t *testing.T, ws *Workspace, button int) kit.DialogSpec {
	t.Helper()
	spec, ok := ws.Dialogs.Current()
	if !ok {
		t.Fatal("no dialog shows")
	}
	spec.OnAnswer(kit.DialogResult{Button: button})
	ws.Dialogs.Dismiss()
	return spec
}

func TestTheQuestionIsAskedInTheReceivingTabAndTheAnswersAreKept(t *testing.T) {
	tests := []struct {
		name     string
		button   int
		cancel   bool
		wantFile bool
		wantLink bool
	}{
		{"Allow", 0, false, true, true},
		{"This Session Only", 1, false, false, true},
		{"Deny", 2, false, false, false},
		{"Escape", 0, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newSharingWorld(t)
			w.openIn(w.notes, "a.txt")
			if w.notes.Dialogs.IsShowing() {
				t.Fatal("the tab the events come from was asked")
			}
			spec, ok := w.office.Dialogs.Current()
			if !ok {
				t.Fatal("the receiving tab was not asked")
			}
			if want := "Let events from notes reach office?"; spec.Message != want {
				t.Errorf("message = %q, want %q", spec.Message, want)
			}
			for _, want := range []string{"Sharer", "emetos.document.opened", w.notes.Folder, w.office.Folder, "event-sharing.json"} {
				if !strings.Contains(spec.Detail, want) {
					t.Errorf("the detail lacks %q:\n%s", want, spec.Detail)
				}
			}
			if !slices.Equal(spec.Buttons, []string{"Allow", "This Session Only", "Deny"}) {
				t.Errorf("buttons = %v", spec.Buttons)
			}
			if c, _ := spec.CancelButton.Get(); c != 2 {
				t.Errorf("the cancel button is %d, want Deny (2)", c)
			}
			if tt.cancel {
				spec.OnAnswer(kit.DialogResult{Button: 2, Cancelled: true})
			} else {
				spec.OnAnswer(kit.DialogResult{Button: tt.button})
			}
			if _, err := os.Stat(w.paths.EventSharingPath()); (err == nil) != tt.wantFile {
				t.Errorf("file written = %v, want %v", err == nil, tt.wantFile)
			}
			w.openIn(w.notes, "b.txt")
			crossed := slices.Contains(w.sharer.heard("office"), "emetos.document.opened@notes")
			if crossed != tt.wantLink {
				t.Errorf("events of notes reached office = %v, want %v (heard %v)", crossed, tt.wantLink, w.sharer.heard("office"))
			}
			if slices.Contains(w.sharer.heard("notes"), "emetos.document.opened@office") {
				t.Error("events went from office to notes")
			}
		})
	}
}

func TestALinkAllowedBeforeTheRestartWorksWithoutAQuestion(t *testing.T) {
	w := newSharingWorld(t)
	w.openIn(w.notes, "a.txt")
	answer(t, w.office, 0)
	w.notes.release()
	w.office.release()

	again := openSharingWorld(t, w.paths, &sharer{}, w.root)
	again.openIn(again.notes, "b.txt")
	if again.office.Dialogs.IsShowing() {
		t.Fatal("the restarted process asked again")
	}
	if got := again.sharer.heard("office"); !slices.Contains(got, "emetos.document.opened@notes") {
		t.Errorf("the link did not work after the restart: office heard %v", got)
	}
	snap := again.p.Extensions.SharingSnapshot()
	if len(snap.Links) != 1 || snap.Links[0].FromName != "notes" || snap.Links[0].ToName != "office" || snap.Links[0].Session {
		t.Errorf("links after the restart = %+v", snap.Links)
	}
}

func TestACorruptSharingFileIsIgnoredWithAWarning(t *testing.T) {
	paths := testutil.Isolate(t)
	if err := os.MkdirAll(filepath.Dir(paths.EventSharingPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.EventSharingPath(), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := openSharingWorld(t, paths, &sharer{}, t.TempDir())
	if got := w.p.Extensions.SharingSnapshot(); len(got.Links) != 0 {
		t.Errorf("links = %+v from a corrupt file", got.Links)
	}
	log := w.logs.String()
	if !strings.Contains(log, "level=WARN") || !strings.Contains(log, "event-sharing.json is ignored") || !strings.Contains(log, "SharingFileCorrupt") {
		t.Errorf("the log does not warn about the file:\n%s", log)
	}
	// the process works: events do not cross, and the first Allow replaces the file
	w.openIn(w.notes, "a.txt")
	answer(t, w.office, 0)
	again := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
	if err := again.Load(context.Background()); err != nil || len(again.Snapshot().Links) != 1 {
		t.Errorf("the file was not replaced: %v %+v", err, again.Snapshot())
	}
}

func TestATabWithNoFolderTakesPartInNothing(t *testing.T) {
	w := newSharingWorld(t)
	empty := newExtensionWorkspace(t, w.p, "")
	if empty.sharingID() != "" || w.office.sharingID() != filepath.Clean(w.office.Folder) {
		t.Fatalf("ids: %q and %q", empty.sharingID(), w.office.sharingID())
	}
	w.openIn(w.notes, "a.txt")
	if empty.Dialogs.IsShowing() {
		t.Error("a tab with no folder was asked")
	}
	if got := w.sharer.heard("No Folder"); len(got) != 0 {
		t.Errorf("a tab with no folder heard %v", got)
	}
}

func TestAnExtensionWithoutTheTopicInItsManifestNeverAsks(t *testing.T) {
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"files.hotExit": "off"})
	var subscribeErr error
	factory := func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{ID: "emetos.nosy", Name: "Nosy", ActivationEvents: []string{extension.OnStartup}},
			activate: func(_ context.Context, api extension.API) error {
				_, subscribeErr = api.Events().Subscribe("emetos.document.opened", func(extension.Event) {}, extension.FromOtherScopes())
				return nil
			},
		}
	}
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{factory}})
	root := t.TempDir()
	for _, name := range []string{"notes", "office"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	notes := newExtensionWorkspace(t, p, filepath.Join(root, "notes"))
	office := newExtensionWorkspace(t, p, filepath.Join(root, "office"))
	w := &sharingWorld{t: t, notes: notes, office: office}
	w.openIn(notes, "a.txt")
	if appErr, ok := errors.FromError(subscribeErr); !ok || !appErr.HasRoute("EventCrossTabNotDeclared") {
		t.Errorf("Subscribe error = %v", subscribeErr)
	}
	if office.Dialogs.IsShowing() || notes.Dialogs.IsShowing() {
		t.Error("an undeclared topic caused a question")
	}
}

func TestTheEventSharingPageListsRemovesAndSwitchesPrivate(t *testing.T) {
	w := newSharingWorld(t)
	w.openIn(w.notes, "a.txt")
	answer(t, w.office, 0)

	a := &groupApp{t: t, win: w.office, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), w.office, nil, nil, ""))
	a.pump()

	// reachable from the Extensions page
	a.run("workbench.extensions.action.showInstalledExtensions")
	a.ts.Tap(gluttertest.ByKey(extensionsui.ManageSharingKey()))
	a.pump()
	if a.active() != "Event Sharing" {
		t.Fatalf("the active tab is %q, want Event Sharing", a.active())
	}
	// and by its command, which reveals the page instead of opening another
	a.run("emetos.extensions.manageEventSharing")
	count := 0
	for _, n := range a.tabNames() {
		if n == "Event Sharing" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d Event Sharing tabs", count)
	}
	notesName, officeName := filepath.Base(w.notes.Folder), filepath.Base(w.office.Folder)
	for _, want := range []string{"Extensions \u203a Event Sharing", notesName + " \u2192 " + officeName, "emetos.document.opened", "Remove", "Private: never send events to other tabs"} {
		if !a.ts.Exists(gluttertest.ByTextContaining(want)) {
			t.Errorf("the page does not show %q", want)
		}
	}
	// the private switch writes the file and turns the sending of that tab off at once
	a.ts.Tap(gluttertest.ByKey(extensionsui.SharingPrivateKey(w.notes.Folder)))
	a.pump()
	if !w.p.Extensions.SharingSnapshot().Scopes[0].Private {
		t.Fatalf("notes is not private: %+v", w.p.Extensions.SharingSnapshot().Scopes)
	}
	before := len(w.sharer.heard("office"))
	w.openIn(w.notes, "b.txt")
	if got := len(w.sharer.heard("office")); got != before {
		t.Errorf("a private workspace sent an event: office heard %d, was %d", got, before)
	}
	// Remove takes the link away at once and tells the receiving tab
	a.ts.Tap(gluttertest.ByKey(extensionsui.SharingRemoveKey(w.notes.Folder, w.office.Folder, false)))
	a.pump()
	if got := w.p.Extensions.SharingSnapshot().Links; len(got) != 0 {
		t.Errorf("links after Remove = %+v", got)
	}
	if !slices.Contains(w.sharer.heard("office"), "emetos.sharing.revoked@"+officeName) {
		t.Errorf("office was not told: %v", w.sharer.heard("office"))
	}
	if !a.ts.Exists(gluttertest.ByTextContaining("No tab shares events")) {
		t.Errorf("the page still lists the link")
	}
	data, err := os.ReadFile(w.paths.EventSharingPath())
	if err != nil || strings.Contains(string(data), "\"from\"") || !strings.Contains(string(data), w.notes.Folder) {
		t.Errorf("event-sharing.json = %s (%v); want no link and notes private", data, err)
	}
}

func TestTheQuestionAnswersByTappingItsButtons(t *testing.T) {
	w := newSharingWorld(t)
	a := &groupApp{t: t, win: w.office, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), w.office, nil, nil, ""))
	a.pump()
	w.openIn(w.notes, "a.txt")
	a.pump()
	for _, label := range []string{"Allow", "This Session Only", "Deny"} {
		if !a.ts.Exists(gluttertest.ByText(label)) {
			t.Fatalf("the dialog has no %q button", label)
		}
	}
	a.ts.Tap(gluttertest.ByText("This Session Only"))
	a.pump()
	links := w.p.Extensions.SharingSnapshot().Links
	if len(links) != 1 || !links[0].Session {
		t.Errorf("links = %+v, want one session link", links)
	}
	if _, err := os.Stat(w.paths.EventSharingPath()); err == nil {
		t.Error("a session link was written")
	}
	if w.office.Dialogs.IsShowing() {
		t.Error("the dialog is still showing")
	}
}

func TestEventSharingPageIsAnEditorKindThatRestores(t *testing.T) {
	var found bool
	for _, k := range editorKinds() {
		if k.typeID == editorgroup.EventSharingInputTypeID {
			found = true
			in, ok := k.restore("{}")
			if !ok || in.TypeID() != editorgroup.EventSharingInputTypeID || k.icon == 0 || k.newPane == nil {
				t.Errorf("kind = %+v, restored %v %v", k, in, ok)
			}
		}
	}
	if !found {
		t.Error("the Event Sharing page is not an editor kind")
	}
	w := newSharingWorld(t)
	if _, ok := w.office.Commands.Lookup("emetos.extensions.manageEventSharing"); !ok {
		t.Error("the command is not registered")
	}
}
