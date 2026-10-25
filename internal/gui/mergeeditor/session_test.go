package mergeeditor

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// twoConflicts is a file with two conflicts and no base section in them.
const twoConflicts = `a
<<<<<<< HEAD
current1
=======
incoming1
>>>>>>> feature
b
<<<<<<< HEAD
cur2
=======
inc2
>>>>>>> feature
c`

func fileDoc(text string) *document.Document {
	return document.New(document.FileURI("/w/a.go"), text, document.NewOptions())
}

// newMarkerSession makes the session of a marker file over a document that holds it.
func newMarkerSession(t *testing.T, text string, initial Initial) (*Session, *document.Document) {
	t.Helper()
	doc := fileDoc(text)
	inputs, err := diff.MergeInputsFromMarkerText(text)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(context.Background(), SessionOpts{Result: doc, Inputs: inputs, Initial: initial,
		OnError: func(err error) { t.Errorf("the session failed: %v", err) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Dispose)
	return s, doc
}

func typeAt(t *testing.T, doc *document.Document, line, col int, text string) {
	t.Helper()
	at := buffer.Position{Line: line, Col: col}
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: text}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionInitialReplace(t *testing.T) {
	s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
	if got := doc.Text(); got != "a\nb\nc" {
		t.Fatalf("the document is %q", got)
	}
	if !doc.Dirty() {
		t.Error("the replaced document is not dirty")
	}
	if n, u := s.Model().ConflictCount(), s.Model().UnhandledCount(); n != 2 || u != 2 {
		t.Errorf("conflicts %d unhandled %d", n, u)
	}
	// the replacement is one undo step that brings the markers back
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if doc.Text() != twoConflicts {
		t.Errorf("the undo gave %q", doc.Text())
	}
	if doc.Dirty() {
		t.Error("the undone document is dirty")
	}
	if _, err := doc.Redo(); err != nil {
		t.Fatal(err)
	}
	if doc.Text() != "a\nb\nc" {
		t.Errorf("the redo gave %q", doc.Text())
	}
}

func TestSessionInitialReplaceKeepsAnEqualText(t *testing.T) {
	// a result file that holds the merged text already is not changed
	doc := fileDoc("a\nb\nc")
	inputs := diff.MergeInputsFromFiles([]string{"a", "b", "c"}, []string{"a", "b", "c"}, []string{"a", "b", "c"})
	if _, err := NewSession(context.Background(), SessionOpts{Result: doc, Inputs: inputs}); err != nil {
		t.Fatal(err)
	}
	if doc.Dirty() || doc.CanUndo() {
		t.Error("an equal text was replaced")
	}
}

func TestSessionFollowsEdits(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(t *testing.T, doc *document.Document)
		// want is the result range of each conflict afterwards
		want [][2]int
		text string
	}{
		{"typing a line before the first conflict", func(t *testing.T, doc *document.Document) { typeAt(t, doc, 0, 1, "\nnew") },
			[][2]int{{3, 3}, {4, 4}}, "a\nnew\nb\nc"},
		{"typing inside the text between", func(t *testing.T, doc *document.Document) { typeAt(t, doc, 1, 1, "x") },
			[][2]int{{2, 2}, {3, 3}}, "a\nbx\nc"},
		{"a line break at the end of the text", func(t *testing.T, doc *document.Document) { typeAt(t, doc, 2, 1, "\nd") },
			[][2]int{{2, 2}, {3, 3}}, "a\nb\nc\nd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
			versions := s.Version()
			tc.edit(t, doc)
			if doc.Text() != tc.text {
				t.Fatalf("the document is %q", doc.Text())
			}
			var got [][2]int
			for _, c := range s.Model().Conflicts() {
				got = append(got, [2]int{c.Result.StartLine, c.Result.EndLine})
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("conflict ranges %v, want %v", got, tc.want)
			}
			if s.Version() == versions {
				t.Error("the session did not notice the change")
			}
			if lines := strings.Count(doc.Text(), "\n") + 1; s.Model().ResultLineCount() != lines || s.Resyncs() != 0 {
				t.Errorf("the model has %d lines, the document %d; %d resyncs", s.Model().ResultLineCount(), lines, s.Resyncs())
			}
		})
	}
}

func TestSessionToggle(t *testing.T) {
	for _, tc := range []struct {
		name      string
		steps     []diff.Input // toggled in order, on conflict 0
		text      string
		state     diff.RangeState
		unhandled int
	}{
		{"incoming", []diff.Input{diff.Input1}, "a\nincoming1\nb\nc", diff.RangeState{Kind: diff.StateInput1}, 1},
		{"current", []diff.Input{diff.Input2}, "a\ncurrent1\nb\nc", diff.RangeState{Kind: diff.StateInput2}, 1},
		{"both: current then incoming", []diff.Input{diff.Input2, diff.Input1}, "a\ncurrent1\nincoming1\nb\nc", diff.BothState(diff.Input2), 1},
		{"toggled off again", []diff.Input{diff.Input1, diff.Input1}, "a\nb\nc", diff.RangeState{}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
			changes := 0
			s.OnChange(func() { changes++ })
			for _, in := range tc.steps {
				if err := s.Toggle(0, in); err != nil {
					t.Fatal(err)
				}
			}
			if doc.Text() != tc.text {
				t.Errorf("the document is %q, want %q", doc.Text(), tc.text)
			}
			c := s.Model().Conflicts()[0]
			if c.State != tc.state || s.Model().UnhandledCount() != tc.unhandled {
				t.Errorf("state %v unhandled %d", c.State, s.Model().UnhandledCount())
			}
			if changes != len(tc.steps) || s.Resyncs() != 0 {
				t.Errorf("%d changes for %d steps, %d resyncs", changes, len(tc.steps), s.Resyncs())
			}
			// the second conflict moved with the first
			if second := s.Model().Conflicts()[1]; !strings.HasPrefix(strings.Join(strings.Split(doc.Text(), "\n")[second.Result.StartLine-1:], "\n"), "c") && second.Result.EndLine != second.Result.StartLine {
				t.Errorf("the second conflict is at %v", second.Result)
			}
		})
	}
}

func TestSessionToggleIsOneUndoStep(t *testing.T) {
	s, _ := newMarkerSession(t, twoConflicts, InitialReplace)
	if err := s.Toggle(1, diff.Input1); err != nil {
		t.Fatal(err)
	}
	if got := s.Result().Text(); got != "a\nb\ninc2\nc" {
		t.Fatalf("the document is %q", got)
	}
	if _, err := s.Result().Undo(); err != nil {
		t.Fatal(err)
	}
	if got := s.Result().Text(); got != "a\nb\nc" {
		t.Errorf("one undo gave %q", got)
	}
	// the model follows the undo: the region has the base lines again
	if c := s.Model().Conflicts()[1]; c.State.Kind != diff.StateBase || c.Result.Len() != 0 {
		t.Errorf("the conflict is %+v after the undo", c)
	}
	if s.Resyncs() != 0 {
		t.Errorf("%d resyncs", s.Resyncs())
	}
}

func TestSessionRouteErrors(t *testing.T) {
	s, _ := newMarkerSession(t, twoConflicts, InitialReplace)
	err := s.Toggle(5, diff.Input1)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute("Session.Toggle->MergeModel.Toggle->MergeModel.conflictRegion.MergeConflictIndexInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	// a done context is the model's DiffCancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inputs := diff.MergeInputsFromFiles([]string{"a"}, []string{"b"}, []string{"c"})
	_, err = NewSession(ctx, SessionOpts{Result: fileDoc(""), Inputs: inputs})
	appErr, ok = errors.FromError(err)
	if !ok || !appErr.HasRoute("NewSession->NewMergeModel") {
		t.Errorf("a done context gave %v", err)
	}
	// a read-only document refuses the edit
	ro := document.NewVirtual(document.VirtualURI("/r"), document.VirtualContent{Text: "a\nb"})
	if _, err = NewSession(context.Background(), SessionOpts{Result: ro, Inputs: diff.MergeInputsFromFiles([]string{"a"}, []string{"x"}, []string{"y"})}); err == nil {
		t.Error("a read-only result was taken")
	} else if appErr, ok = errors.FromError(err); !ok || !appErr.HasRoute("NewSession->Session.replaceDocument->Document.Edit->Document.checkWritable.DocumentReadOnly") {
		t.Errorf("unexpected failure path: %v", appErr.Route())
	}
}

func TestSessionSyncKeepsTheDocument(t *testing.T) {
	// a document restored after the conflicts were resolved: the model follows it
	text := "a\nincoming1\nb\nc"
	s, doc := newMarkerSession(t, twoConflicts, InitialReplace)
	doc.SetText(text)
	if s.Resyncs() != 1 {
		t.Fatalf("%d resyncs after a reload", s.Resyncs())
	}
	if s.Model().ResultLineCount() != 4 {
		t.Fatalf("the model has %d lines", s.Model().ResultLineCount())
	}
	// the first conflict has the incoming line now, and the model knows which state it is in
	if c := s.Model().Conflicts()[0]; c.State.Kind != diff.StateInput1 || c.Result.Len() != 1 {
		t.Errorf("the first conflict is %+v", c)
	}
	// and a session made over a document with that text starts from it
	inputs, _ := diff.MergeInputsFromMarkerText(twoConflicts)
	doc2 := fileDoc(text)
	s2, err := NewSession(context.Background(), SessionOpts{Result: doc2, Inputs: inputs, Initial: InitialSync})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Dispose()
	if doc2.Text() != text || doc2.CanUndo() {
		t.Errorf("the synced document was changed: %q", doc2.Text())
	}
	if c := s2.Model().Conflicts()[0]; c.State.Kind != diff.StateInput1 || !c.Handled {
		t.Errorf("the first conflict is %v, handled %v", c.State, c.Handled)
	}
	// the second still has the base's lines: it is not handled
	if c := s2.Model().Conflicts()[1]; c.State.Kind != diff.StateBase || c.Handled || s2.Model().UnhandledCount() != 1 {
		t.Errorf("the second conflict is %v, handled %v", c.State, c.Handled)
	}
}

func TestDocumentEdit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		edit  diff.Edit
		after string
	}{
		{"replace a middle line", "a\nb\nc", diff.Edit{StartLine: 2, EndLine: 3, NewLines: []string{"X", "Y"}}, "a\nX\nY\nc"},
		{"delete a middle line", "a\nb\nc", diff.Edit{StartLine: 2, EndLine: 3}, "a\nc"},
		{"insert between lines", "a\nb\nc", diff.Edit{StartLine: 2, EndLine: 2, NewLines: []string{"X"}}, "a\nX\nb\nc"},
		{"insert at the top", "a\nb", diff.Edit{StartLine: 1, EndLine: 1, NewLines: []string{"X"}}, "X\na\nb"},
		{"replace the last line", "a\nb\nc", diff.Edit{StartLine: 3, EndLine: 4, NewLines: []string{"X"}}, "a\nb\nX"},
		{"replace through the end", "a\nb\nc", diff.Edit{StartLine: 2, EndLine: 4, NewLines: []string{"X", "Y"}}, "a\nX\nY"},
		{"delete the last line", "a\nb\nc", diff.Edit{StartLine: 3, EndLine: 4}, "a\nb"},
		{"delete through the end", "a\nb\nc", diff.Edit{StartLine: 2, EndLine: 4}, "a"},
		{"delete everything", "a\nb", diff.Edit{StartLine: 1, EndLine: 3}, ""},
		{"append after the last line", "a\nb", diff.Edit{StartLine: 3, EndLine: 3, NewLines: []string{"X", "Y"}}, "a\nb\nX\nY"},
		{"insert before an empty last line", "a\n", diff.Edit{StartLine: 2, EndLine: 2, NewLines: []string{"X"}}, "a\nX\n"},
		{"replace the empty last line", "a\n", diff.Edit{StartLine: 2, EndLine: 3, NewLines: []string{"X"}}, "a\nX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := fileDoc(tc.text)
			de, ok := documentEdit(doc.Snapshot(), tc.edit)
			if !ok {
				t.Fatal("no edit")
			}
			if _, err := doc.Edit([]document.Edit{de}, document.EditOptions{}); err != nil {
				t.Fatal(err)
			}
			if doc.Text() != tc.after {
				t.Errorf("got %q, want %q", doc.Text(), tc.after)
			}
			// the model's view of the same edit: the lines with [start, end) replaced
			lines := strings.Split(tc.text, "\n")
			want := append(slices.Clone(lines[:tc.edit.StartLine-1]), tc.edit.NewLines...)
			want = append(want, lines[min(tc.edit.EndLine-1, len(lines)):]...)
			if len(want) == 0 {
				want = []string{""}
			}
			if got := strings.Split(doc.Text(), "\n"); !slices.Equal(got, want) {
				t.Errorf("the lines are %q, the model's are %q", got, want)
			}
		})
	}
	if _, ok := documentEdit(fileDoc("a\nb").Snapshot(), diff.Edit{StartLine: 3, EndLine: 3}); ok {
		t.Error("an empty insertion past the end is an edit")
	}
}

func TestLineEdits(t *testing.T) {
	apply := func(lines []string, edits []diff.Edit) []string {
		out := slices.Clone(lines)
		for _, e := range edits { // from the last to the first
			out = slices.Replace(out, e.StartLine-1, e.EndLine-1, e.NewLines...)
		}
		return out
	}
	pos := func(l, c int) buffer.Position { return buffer.Position{Line: l, Col: c} }
	rng := func(a, b buffer.Position) buffer.Range { return buffer.Range{Start: a, End: b} }
	for _, tc := range []struct {
		name  string
		text  string
		batch []document.Edit
	}{
		{"typing", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(1, 1), pos(1, 1)), Text: "X"}}},
		{"a line break inside a line", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(1, 1), pos(1, 1)), Text: "\n"}}},
		{"joining two lines", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(0, 3), pos(1, 0)), Text: ""}}},
		{"two edits on a line", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(1, 0), pos(1, 1)), Text: "1"}, {Range: rng(pos(1, 2), pos(1, 3)), Text: "3\n4"}}},
		{"two edits on two lines", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(0, 1), pos(0, 2)), Text: "B\nB"}, {Range: rng(pos(2, 0), pos(2, 3)), Text: "G"}}},
		{"an edit that ends where the next starts", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(0, 1), pos(1, 1)), Text: "-"}, {Range: rng(pos(1, 2), pos(2, 1)), Text: "+\n+"}}},
		{"deleting a whole text", "abc\ndef", []document.Edit{{Range: rng(pos(0, 0), pos(1, 3)), Text: ""}}},
		{"an edit at the end of the text", "abc", []document.Edit{{Range: rng(pos(0, 3), pos(0, 3)), Text: "\n\n"}}},
		{"whole lines inserted", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(1, 0), pos(1, 0)), Text: "x\ny\n"}}},
		{"whole lines deleted", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(0, 0), pos(2, 0)), Text: ""}}},
		{"whole lines replaced", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(1, 0), pos(2, 0)), Text: "x\ny\n"}}},
		{"whole lines next to a line edit", "abc\ndef\nghi", []document.Edit{{Range: rng(pos(0, 0), pos(1, 0)), Text: ""}, {Range: rng(pos(1, 1), pos(1, 2)), Text: "E"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := fileDoc(tc.text)
			var events []document.ChangeEvent
			doc.Subscribe(func(ev document.ChangeEvent) { events = append(events, ev) })
			if _, err := doc.Edit(tc.batch, document.EditOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := doc.Undo(); err != nil {
				t.Fatal(err)
			}
			if _, err := doc.Redo(); err != nil {
				t.Fatal(err)
			}
			for i, ev := range events {
				before := snapshotLines(ev.Before)
				got := apply(before, LineEdits(ev))
				if want := snapshotLines(ev.After); !slices.Equal(got, want) {
					t.Errorf("event %d (cause %d): the edits give %q, the document has %q", i, ev.Cause, got, want)
				}
			}
		})
	}
}
