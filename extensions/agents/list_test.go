package agents

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// rows lists the labels of the rows of the side bar view.
func rows(r *rig) []string {
	var out []string
	for _, row := range r.ext.model.Rows() {
		out = append(out, row.Label)
	}
	return out
}

// twoSessions opens two sessions and sends a message in each, a minute apart.
func twoSessions(t *testing.T, r *rig) (a, b *sessionTab) {
	t.Helper()
	a = r.open()
	r.ready(a)
	r.send(a, "first question")
	r.idle(a)
	r.now = r.now.Add(time.Minute)
	b = r.open()
	r.ready(b)
	r.send(b, "second question")
	r.idle(b)
	return a, b
}

func TestTheSideBarListsTheSessionsNewestFirstWithTheFirstMessageAsTitle(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	a, b := twoSessions(t, r)
	if got := rows(r); !slices.Equal(got, []string{"second question", "first question"}) {
		t.Errorf("rows = %q", got)
	}
	if r.ext.entry(a.id).Created == 0 || r.ext.entry(b.id).Updated <= r.ext.entry(a.id).Updated {
		t.Errorf("the times: %+v %+v", r.ext.entry(a.id), r.ext.entry(b.id))
	}
	// the view builds, and says when
	if got := ago(r.now, r.ext.entry(a.id).Updated); got != "1 min" {
		t.Errorf("ago = %q", got)
	}
}

func TestAgoSaysHowLongBefore(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		then time.Time
		want string
	}{
		{"seconds", now.Add(-20 * time.Second), "now"},
		{"minutes", now.Add(-5 * time.Minute), "5 min"},
		{"hours", now.Add(-3 * time.Hour), "3 h"},
		{"days", now.Add(-50 * time.Hour), "2 d"},
		{"months", now.Add(-60 * 24 * time.Hour), "Jan 3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ago(now, tt.then.UnixMilli()); got != tt.want {
				t.Errorf("ago = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenameChangesTheNameInTheListAndTheTab(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	a, _ := twoSessions(t, r)
	if err := r.api.Run(RenameSessionCommand, map[string]any{"session": a.id, "name": "  Fix the build  "}); err != nil {
		t.Fatal(err)
	}
	if got := rows(r); !slices.Equal(got, []string{"second question", "Fix the build"}) {
		t.Errorf("rows = %q", got)
	}
	if got := r.api.EditorTabs()[0].Title(); got != "Fix the build" {
		t.Errorf("the title of the tab is %q", got)
	}
	// without a name the row becomes an input box with the current name, and a name commits
	if err := r.api.Run(RenameSessionCommand, map[string]any{"session": a.id}); err != nil {
		t.Fatal(err)
	}
	if r.ext.renaming != a.id || r.ext.renameIn == nil || r.ext.renameIn.Value() != "Fix the build" {
		t.Fatalf("renaming = %q", r.ext.renaming)
	}
	r.ext.rename(a.id, "Third name")
	if r.ext.renaming != "" || r.ext.entry(a.id).Name != "Third name" {
		t.Errorf("after the rename: %+v", r.ext.entry(a.id))
	}
	// an empty name takes the name away, and the first message is the title again
	r.ext.rename(a.id, "")
	if got := rows(r); !slices.Equal(got, []string{"second question", "first question"}) {
		t.Errorf("rows = %q", got)
	}
	// a name that is the title is not kept as a name
	r.ext.rename(a.id, "first question")
	if r.ext.entry(a.id).Name != "" {
		t.Errorf("name = %q", r.ext.entry(a.id).Name)
	}
}

func TestDeleteRemovesTheSessionFromTheListAndClosesItsTabAndProgram(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	a, b := twoSessions(t, r)
	pgids := r.started()
	if err := r.api.Run(DeleteSessionCommand, map[string]any{"session": a.id}); err != nil {
		t.Fatal(err)
	}
	if got := rows(r); !slices.Equal(got, []string{"second question"}) {
		t.Errorf("rows = %q", got)
	}
	if !r.api.EditorTabs()[0].Closed || r.api.EditorTabs()[1].Closed {
		t.Error("only the tab of the deleted session closes")
	}
	if r.ext.entry(a.id) != nil {
		t.Error("closing the tab put the deleted session back in the index")
	}
	r.ext.wait()
	if len(pgids) != 2 {
		t.Fatalf("two programs were started: %d", len(pgids))
	}
	r.api.WaitFor("the program of the deleted session to end", func() bool { return a.sess == nil })
	if b.sess == nil {
		t.Error("the program of the other session was ended")
	}
}

func TestOpenSessionShowsTheTabThatHasItOrResumesAClosedOne(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	a, _ := twoSessions(t, r)
	// the session has a tab: it is shown
	if err := r.api.Run(OpenSessionCommand, map[string]any{"session": a.id}); err != nil {
		t.Fatal(err)
	}
	if !r.api.EditorTabs()[0].Revealed || len(r.api.EditorTabs()) != 2 {
		t.Errorf("the open tab was not revealed (tabs: %d)", len(r.api.EditorTabs()))
	}
	// close it: the program ends, the conversation stays in the index
	before := texts(a)
	r.api.EditorTabs()[0].Close()
	r.ext.wait()
	if len(r.ext.tabs) != 1 {
		t.Fatalf("tabs = %d", len(r.ext.tabs))
	}
	// open it again from the list: a new tab with the conversation and no program
	starts := len(r.started())
	if err := r.api.Run(OpenSessionCommand, map[string]any{"session": a.id}); err != nil {
		t.Fatal(err)
	}
	again := r.ext.tabs[len(r.ext.tabs)-1]
	if again.id != a.id || !slices.Equal(texts(again), before) || !again.used || again.phase != phaseIdle {
		t.Fatalf("the resumed tab: id %s used %v phase %v items %q (want %q)", again.id, again.used, again.phase, texts(again), before)
	}
	if len(r.started()) != starts {
		t.Error("resuming from the list started a program before the user sent a message")
	}
	// the next message resumes the conversation of the program
	r.send(again, "go on")
	r.ready(again)
	r.idle(again)
	last := r.started()[len(r.started())-1]
	if last.value("--resume") != a.id || last.has("--session-id") {
		t.Errorf("the program was started with %v", last.Argv)
	}
	if got := texts(again); got[len(got)-1] != "text:It is a Go file." {
		t.Errorf("the conversation ends with %q", got[len(got)-1])
	}
}

func TestTheCommandsOfTheListFailForASessionThatIsNotThere(t *testing.T) {
	r := newRig(t, "")
	for _, id := range []string{OpenSessionCommand, RenameSessionCommand, DeleteSessionCommand} {
		err := r.api.Run(id, map[string]any{"session": "nope"})
		if err == nil {
			t.Errorf("%s: no error", id)
			continue
		}
		route(t, err, "SessionUnknown")
		err = r.api.Run(id, nil)
		route(t, err, "sessionArg.SessionArgumentMissing")
	}
}

func TestTheIndexIsTheStateOfTheViewAndComesBack(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	a, _ := twoSessions(t, r)
	if err := r.api.Run(RenameSessionCommand, map[string]any{"session": a.id, "name": "Kept"}); err != nil {
		t.Fatal(err)
	}
	state, err := r.api.View(ViewID).Content.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	// a new window: the extension is activated again and gets the state
	r2 := newRig(t, "chat.jsonl")
	if err := r2.api.View(ViewID).Content.RestoreState(raw); err != nil {
		t.Fatal(err)
	}
	if got := rows(r2); !slices.Equal(got, []string{"second question", "Kept"}) {
		t.Errorf("the restored rows are %q", got)
	}
	if e := r2.ext.entry(a.id); e == nil || len(e.Items) == 0 || e.Items[0].Text != "first question" {
		t.Errorf("the entry did not come back with its conversation: %+v", e)
	}

	err = r2.api.View(ViewID).Content.RestoreState([]byte(`{"sessions": 3}`))
	route(t, err, "restoreState.AgentsStateInvalid")
}

func TestOnlyTheMostRecentSessionsKeepTheirConversationInTheIndex(t *testing.T) {
	r := newRig(t, "")
	for i := range maxSnapshots + 5 {
		r.ext.index = append(r.ext.index, &sessionEntry{ID: string(rune('a'+i)) + "-id", Title: "t", Updated: int64(i), Items: []*item{{Kind: kindUser, Text: "x"}}})
	}
	state, _ := r.ext.saveState()
	kept := 0
	for _, e := range state.(listState).Sessions {
		if len(e.Items) > 0 {
			kept++
			if e.Updated < 5 {
				t.Errorf("an old session kept its conversation: %+v", e)
			}
		}
	}
	if kept != maxSnapshots {
		t.Errorf("%d sessions keep a conversation, want %d", kept, maxSnapshots)
	}
}

func TestTheNewSessionCommandOpensATabInTheGroupOfTheIcon(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	if err := r.api.Run(NewSessionCommand, map[string]any{"groupId": float64(3)}); err != nil {
		t.Fatal(err)
	}
	tabs := r.api.EditorTabs()
	if len(tabs) != 1 || tabs[0].Group_ != 3 || tabs[0].Title() != untitled {
		t.Fatalf("tabs = %+v", tabs)
	}
	var _ extension.EditorTab = (*fakeapi.Tab)(nil)
}
