package notes

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// rig is the extension activated over the fake API.
type rig struct {
	t   *testing.T
	api *fakeapi.API
	ext *notes
}

func newRig(t *testing.T) *rig {
	t.Helper()
	e := New()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
	if err := e.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, api: api, ext: e.(*notes)}
}

func (r *rig) titles() []string {
	var out []string
	for _, n := range r.ext.list {
		out = append(out, n.name())
	}
	return out
}

// run runs a command of the extension.
func run(t *testing.T, r *rig, id string, args any) {
	t.Helper()
	if err := r.api.Run(id, args); err != nil {
		t.Fatal(err)
	}
}

func route(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

func TestTheManifestIsValidAndDeclaresWhatTheExtensionNeeds(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	c := m.Contributes
	if len(c.EditorKinds) != 1 || c.EditorKinds[0].ID != KindID || len(c.EditorActions) != 1 || c.EditorActions[0].Command != NewNoteCommand ||
		len(c.Views) != 1 || len(c.ViewContainers) != 1 {
		t.Errorf("the contributions are %+v", c)
	}
}

func TestNewNoteMakesANoteAndOpensItInTheGroupOfTheIcon(t *testing.T) {
	r := newRig(t)
	if err := r.api.Run(NewNoteCommand, map[string]any{"groupId": float64(3)}); err != nil {
		t.Fatal(err)
	}
	tabs := r.api.EditorTabs()
	if len(tabs) != 1 || tabs[0].Group_ != 3 || tabs[0].Title() != untitled {
		t.Fatalf("tabs = %+v", tabs)
	}
	if !slices.Equal(r.titles(), []string{untitled}) {
		t.Errorf("notes = %v", r.titles())
	}
	// from the palette: the active group
	if err := r.api.Run(NewNoteCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.api.EditorTabs(); len(got) != 2 || got[1].Group_ != 0 || len(r.ext.list) != 2 || r.ext.list[0].ID == r.ext.list[1].ID {
		t.Errorf("the second note: %+v", got)
	}
}

func TestEditingATabKeepsTheNoteAndTitlesTheTab(t *testing.T) {
	r := newRig(t)
	if err := r.api.Run(NewNoteCommand, nil); err != nil {
		t.Fatal(err)
	}
	tab := r.api.EditorTabs()[0]
	nt := r.ext.tabs["n1"][0]
	nt.title.SetValue("Groceries")
	nt.body.SetValue("milk")
	nt.changed()
	if tab.Title() != "Groceries" || r.ext.list[0].Text != "milk" || !slices.Equal(r.titles(), []string{"Groceries"}) {
		t.Errorf("the tab is %q, the note %+v", tab.Title(), r.ext.list[0])
	}
	// the state of the tab and of the view hold the note
	var st tabState
	if err := json.Unmarshal(tab.State(), &st); err != nil || st.Note.Title != "Groceries" || st.Note.Text != "milk" {
		t.Errorf("the state of the tab is %s", tab.State())
	}
	saved, _ := r.ext.saveState()
	if ls := saved.(listState); len(ls.Notes) != 1 || ls.Notes[0].Text != "milk" {
		t.Errorf("the state of the view is %+v", saved)
	}
}

func TestTheListOpensANoteInATabAndRevealsTheOneThatIsOpen(t *testing.T) {
	r := newRig(t)
	run(t, r, NewNoteCommand, nil)
	tab := r.api.EditorTabs()[0]
	n := r.ext.list[0]
	r.ext.open(n)
	if len(r.api.EditorTabs()) != 1 || !tab.Revealed {
		t.Errorf("an open note opened another tab (%d tabs) or was not revealed", len(r.api.EditorTabs()))
	}
	tab.Close()
	r.ext.open(n)
	if got := len(r.api.EditorTabs()); got != 2 {
		t.Errorf("a note without a tab did not open one: %d tabs", got)
	}
	// two tabs of one note follow each other
	r.ext.tabs[n.ID][0].title.SetValue("Shared")
	r.ext.tabs[n.ID][0].changed()
	if len(r.ext.tabs[n.ID]) != 1 {
		t.Fatalf("%d tabs of the note", len(r.ext.tabs[n.ID]))
	}
	copyTab := r.api.RestoreTab(KindID, "copy", mustJSON(tabState{Note: n}))
	r.ext.tabs[n.ID][1].body.SetValue("edited in the copy")
	r.ext.tabs[n.ID][1].changed()
	if got := r.ext.tabs[n.ID][0].body.Value(); got != "edited in the copy" {
		t.Errorf("the first tab shows %q", got)
	}
	if copyTab.Title() != "Shared" {
		t.Errorf("the title of the copy is %q", copyTab.Title())
	}
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestDeletingANoteClosesItsTabs(t *testing.T) {
	r := newRig(t)
	run(t, r, NewNoteCommand, nil)
	run(t, r, NewNoteCommand, nil)
	tab := r.api.EditorTabs()[0]
	if err := r.api.Run(DeleteNoteCommand, map[string]any{"note": "n1"}); err != nil {
		t.Fatal(err)
	}
	if !tab.Closed || len(r.ext.list) != 1 || r.ext.list[0].ID != "n2" {
		t.Errorf("closed %v, notes %v", tab.Closed, r.titles())
	}
	run(t, r, DeleteNoteCommand, "nonsense")
	run(t, r, DeleteNoteCommand, map[string]any{"note": "gone"})
	if len(r.ext.list) != 1 {
		t.Error("a delete of nothing deleted something")
	}
}

func TestTheNotesAndTheirTabsComeBackAfterARestart(t *testing.T) {
	r := newRig(t)
	run(t, r, NewNoteCommand, nil)
	run(t, r, NewNoteCommand, nil)
	for i, text := range []string{"one", "two"} {
		nt := r.ext.tabs[r.ext.list[i].ID][0]
		nt.title.SetValue("Note " + text)
		nt.body.SetValue(text)
		nt.changed()
	}
	viewState, _ := r.ext.saveState()
	viewJSON := mustJSON(viewState)
	tabJSON := r.api.EditorTabs()[1].State()

	// a new run: the view gets its state, and the tab that was open gets its own
	second := newRig(t)
	if err := second.ext.restoreState(viewJSON); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(second.titles(), []string{"Note one", "Note two"}) {
		t.Fatalf("the restored notes are %v", second.titles())
	}
	restored := second.api.RestoreTab(KindID, "t2", tabJSON)
	if restored.Title() != "Note two" || len(second.ext.list) != 2 {
		t.Errorf("the restored tab is %q and there are %d notes", restored.Title(), len(second.ext.list))
	}
	// the tab alone brings its note back when the list did not
	third := newRig(t)
	back := third.api.RestoreTab(KindID, "t2", tabJSON)
	if back.Title() != "Note two" || !slices.Equal(third.titles(), []string{"Note two"}) {
		t.Errorf("the note of a tab without a list: %q %v", back.Title(), third.titles())
	}
}

func TestBadStatesAreRefusedWithARoute(t *testing.T) {
	r := newRig(t)
	route(t, r.ext.restoreState(json.RawMessage(`[1]`)), "notes.restoreState.NotesStateInvalid")
	_, err := r.ext.createTab(r.api.RestoreTab(KindID, "x", mustJSON(tabState{Note: &note{ID: "n9"}})), json.RawMessage(`nope`))
	route(t, err, "notes.createTab.NotesTabStateInvalid")
	_, err = r.ext.createTab(r.api.RestoreTab(KindID, "y", mustJSON(tabState{Note: &note{ID: "n8"}})), nil)
	route(t, err, "notes.createTab.NotesTabStateInvalid")
	_, err = r.ext.createTab(r.api.RestoreTab(KindID, "z", mustJSON(tabState{Note: &note{ID: "n7"}})), json.RawMessage(`{"note":{}}`))
	route(t, err, "notes.createTab.NotesTabStateInvalid")
}

func TestGoldenTheNotesView(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			for _, title := range []string{"Groceries", "Ideas for the weekend", ""} {
				run(t, r, NewNoteCommand, nil)
				nt := r.ext.tabs[r.ext.list[len(r.ext.list)-1].ID][0]
				nt.title.SetValue(title)
				nt.changed()
			}
			build := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.api.View(ViewID).Content.Build(bc) })
			ts := uitest.Pump(t, th, gui.Size{Width: 300, Height: 160}, build)
			ts.Pump()
			uitest.MatchesGolden(ts, "notes_view_"+th.Name)
		})
	}
}

func TestGoldenANoteTab(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			run(t, r, NewNoteCommand, nil)
			nt := r.ext.tabs["n1"][0]
			nt.title.SetValue("Groceries")
			nt.body.SetValue("milk\neggs\nbread")
			nt.changed()
			build := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.api.EditorTabs()[0].Content.Build(bc) })
			ts := uitest.Pump(t, th, gui.Size{Width: 420, Height: 200}, build)
			ts.Pump()
			uitest.MatchesGolden(ts, "note_tab_"+th.Name)
		})
	}
}
