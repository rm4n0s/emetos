package mergeeditor

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/mergeEditorModel.ts (the result text model
// and its edits: setState, the diffs of the result, isApplyingEditInResult) and
// src/vs/workbench/contrib/mergeEditor/browser/mergeEditorInputModel.ts. VS Code diffs the result against the base
// after every edit; Emetos keeps the regions as line ranges that follow the edits (SPEC §9.11), so this file turns the
// change events of the result document into the whole-line edits the model takes.

// resyncTimeLimit is how long the diff may take that puts the model right with a document it lost track of.
const resyncTimeLimit = time.Second

// Initial says how a session takes the result document.
type Initial int

const (
	// InitialReplace puts the result of the model in the document as one undoable edit: the merge starts from the
	// base with the changes that do not conflict applied (a document with conflict markers, or a file that is to be
	// overwritten, gets the merged text; an undo brings back what it had).
	InitialReplace Initial = iota
	// InitialSync keeps the text of the document and makes the model follow it: a merge that is restored after the
	// result was edited and backed up. The conflicts whose lines are not the base's are taken as handled.
	InitialSync
)

// SessionOpts are the options of NewSession.
type SessionOpts struct {
	// Result is the document that holds the result of the merge. Required.
	Result *document.Document
	// Inputs are the texts of the merge. Its EOL is ignored: the document keeps its own line ending.
	Inputs diff.MergeInputs
	// Model is the merge model when the caller made it already (the diffs run on a goroutine, the session is made on
	// the UI thread); it was made from Inputs with the line break "\n", and the session takes it over. Without it
	// the session makes the model.
	Model *diff.MergeModel
	// Initial says how the document is taken.
	Initial Initial
	// Merge are the options of the model's diffs.
	Merge diff.MergeOptions
	// OnError is told about a failure to keep the model in step with the document (the merge goes on with the
	// model re-synced from the text); nil drops it.
	OnError func(error)
}

type sessionListener struct{ fn func() }

// Session is a three-way merge being edited (SPEC §9.11): the merge model and the result document, kept the same.
// A change of the document, whoever makes it (typing, undo and redo, a reload), is handed to the model as whole-line
// edits, so that the regions follow it; an edit the session makes to the document itself (a toggled checkbox, the
// accept actions, Accept All and Reset Result) is made to the model first and applied to the document as one undoable
// edit, with the handled flags of the conflicts remembered for undo and redo. It works
// on the UI thread only, as the document does.
type Session struct {
	model     *diff.MergeModel
	result    *document.Document
	opts      SessionOpts
	stop      func()
	applying  bool
	listeners []*sessionListener
	version   int
	resyncs   int
	disposed  bool
	// handled holds, for the alternative versions of the result document that an accept action left or reached, the
	// handled flags of the conflicts at that version, so that undo and redo give them back (VS Code's attached
	// history elements, MarkAsHandledUndoRedoElement).
	handled map[int][]bool
}

// NewSession makes the model of the merge and takes the result document. The error of the model (DiffCancelled for
// a done context) is returned as it is.
func NewSession(ctx context.Context, opts SessionOpts) (*Session, error) {
	model := opts.Model
	if model == nil {
		opts.Inputs.EOL = "\n"
		var err error
		if model, err = diff.NewMergeModel(ctx, opts.Inputs, opts.Merge); err != nil {
			return nil, err
		}
	}
	s := &Session{model: model, result: opts.Result, opts: opts}
	switch opts.Initial {
	case InitialReplace:
		if err := s.replaceDocument(model.Result()); err != nil {
			return nil, err
		}
	case InitialSync:
		if err := s.resync(ctx); err != nil {
			return nil, err
		}
		// a conflict whose lines in the document are not the base's was dealt with before: the merge starts from the text
		// of a result that was edited (VS Code derives the same from the diffs of the result)
		for i, c := range model.Conflicts() {
			if c.State.Kind != diff.StateBase && !c.Handled {
				if err := model.SetHandled(i, true); err != nil {
					return nil, err
				}
			}
		}
	}
	s.stop = opts.Result.Subscribe(s.documentChanged)
	return s, nil
}

// Model is the merge model: the regions, the conflicts and the counts. The caller must not change it; the session's
// methods do, and keep the document in step.
func (s *Session) Model() *diff.MergeModel { return s.model }

// Result is the document that holds the result.
func (s *Session) Result() *document.Document { return s.result }

// Version counts the changes of the session: it grows with every change of the result document and of the model.
func (s *Session) Version() int { return s.version }

// Resyncs counts how many times the model had to be put right with the document by a diff (a reload of the file, a
// change the line edits could not follow); it is for the tests.
func (s *Session) Resyncs() int { return s.resyncs }

// OnChange calls fn after every change of the result or of the model; it gives the function that stops the calls.
func (s *Session) OnChange(fn func()) (stop func()) {
	l := &sessionListener{fn: fn}
	s.listeners = append(s.listeners, l)
	return func() {
		s.listeners = slices.DeleteFunc(s.listeners, func(x *sessionListener) bool { return x == l })
	}
}

func (s *Session) changed() {
	s.version++
	for _, l := range slices.Clone(s.listeners) {
		l.fn()
	}
}

// Dispose stops following the document.
func (s *Session) Dispose() {
	if s.disposed {
		return
	}
	s.disposed = true
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	s.listeners = nil
}

// Toggle includes the input in the result of the conflict if it is not in, and takes it out if it is: the
// checkbox of the input (VS Code's setState with the toggled state), one undoable edit of the document. The error
// is that of the model (MergeConflictIndexInvalid, MergeChoiceInvalid) or of the document.
func (s *Session) Toggle(conflict int, in diff.Input) error {
	before := s.begin()
	e, err := s.model.Toggle(conflict, in)
	if err != nil {
		return err
	}
	return s.finish(before, []diff.Edit{e})
}

// Accept resolves a conflict with a choice: the lines of the base, of the incoming input, of the current input, or of
// both (current first, SPEC §9.11), and marks it handled; one undoable edit of the document. The errors are those of
// the model (MergeConflictIndexInvalid, MergeChoiceInvalid) or of the document.
func (s *Session) Accept(conflict int, c diff.Choice) error {
	before := s.begin()
	e, err := s.model.Accept(conflict, c)
	if err != nil {
		return err
	}
	return s.finish(before, []diff.Edit{e})
}

// SetState puts the result of a conflict in a state (the general form of Accept: AcceptBoth puts the current input first,
// while "Accept Combination" of an input puts that input first) and marks it handled; one undoable edit.
func (s *Session) SetState(conflict int, st diff.RangeState) error {
	before := s.begin()
	e, err := s.model.SetState(conflict, st)
	if err != nil {
		return err
	}
	return s.finish(before, []diff.Edit{e})
}

// AcceptAll resolves every conflict that is not handled with a choice, as one undoable edit of the document (VS Code's
// merge.acceptAllInput1 and merge.acceptAllInput2, the choices AcceptInput1 and AcceptInput2). Nothing happens when no
// conflict is unhandled.
func (s *Session) AcceptAll(c diff.Choice) error {
	before := s.begin()
	edits, err := s.model.AcceptAll(c)
	if err != nil {
		return err
	}
	return s.finish(before, edits)
}

// ResetResult puts every conflict that was resolved back to the lines of the base and marks it unhandled, as one
// undoable edit (VS Code's mergeEditor.resetResultToBaseAndAutoMerge; the text outside the conflicts, and the changes
// that do not conflict, stay as they are now).
func (s *Session) ResetResult() error {
	before := s.begin()
	conflicts := s.model.Conflicts()
	var edits []diff.Edit
	for i := len(conflicts) - 1; i >= 0; i-- {
		if c := conflicts[i]; c.State.Kind == diff.StateBase && !c.Handled && s.holdsBase(c) {
			continue
		}
		e, err := s.model.Reset(i)
		if err != nil {
			// the edits the model made before it failed are in its result: the document gets them too
			s.reportResync(s.applyEdits(edits))
			return err
		}
		edits = append(edits, e)
	}
	return s.finish(before, edits)
}

// AcceptAllCombination puts the lines of both inputs in the result of every conflict that has not got those of the
// incoming input yet and was not edited by hand (the one that is in the result stays first), as one undoable edit (VS
// Code's mergeEditor.acceptAllCombination, without the smart combination of the characters).
func (s *Session) AcceptAllCombination() error {
	before := s.begin()
	conflicts := s.model.Conflicts()
	var edits []diff.Edit
	for i := len(conflicts) - 1; i >= 0; i-- {
		c := conflicts[i]
		if c.State.Kind == diff.StateUnrecognized || c.State.IncludesInput(diff.Input1) || !c.Range.IsConflicting() {
			continue
		}
		e, err := s.model.SetState(i, c.State.WithInputValue(diff.Input1, true).WithInputValue(diff.Input2, true))
		if err != nil {
			// the edits the model made before it failed are in its result: the document gets them too
			s.reportResync(s.applyEdits(edits))
			return err
		}
		edits = append(edits, e)
	}
	return s.finish(before, edits)
}

// holdsBase tells whether the lines of the result in a region are the lines of its base.
func (s *Session) holdsBase(r diff.Region) bool {
	got := s.model.Result()
	want := r.Range.BaseLines()
	if r.Result.StartLine < 1 || r.Result.EndLine-1 > len(got) || r.Result.Len() != len(want) {
		return false
	}
	return slices.Equal(got[r.Result.StartLine-1:r.Result.EndLine-1], want)
}

// handledFlags are the handled flags of the conflicts.
func (s *Session) handledFlags() []bool {
	conflicts := s.model.Conflicts()
	flags := make([]bool, len(conflicts))
	for i, c := range conflicts {
		flags[i] = c.Handled
	}
	return flags
}

// beginning is the state of the result before a change of the model: the alternative version of the document and the
// handled flags.
type beginning struct {
	alt   int
	flags []bool
}

// begin takes note of the state before a change of the model.
func (s *Session) begin() beginning {
	return beginning{alt: s.result.AlternativeVersion(), flags: s.handledFlags()}
}

// finish applies the edits the model made to the document as one undoable edit. The handled flags of the conflicts before
// and after are remembered for the versions of the document, so that an undo or a redo gives them back.
func (s *Session) finish(before beginning, edits []diff.Edit) error {
	err := s.applyEdits(edits)
	if s.handled == nil {
		s.handled = map[int][]bool{}
	}
	if _, ok := s.handled[before.alt]; !ok {
		s.handled[before.alt] = before.flags
	}
	s.handled[s.result.AlternativeVersion()] = s.handledFlags()
	if err == nil {
		s.changed()
	}
	return err
}

// restoreHandled gives the conflicts the handled flags they had at a version of the document that an undo or a redo
// reached, when an accept action left or reached it.
func (s *Session) restoreHandled(version int) {
	flags, ok := s.handled[version]
	if !ok {
		return
	}
	for i, c := range s.model.Conflicts() {
		if i < len(flags) && c.Handled != flags[i] {
			s.reportResync(s.model.SetHandled(i, flags[i]))
		}
	}
}

// applyEdits applies edits the model made to its own result (from the last to the first, each valid against the result
// before them all) to the document, as one undoable edit. Edits that change no line are left out.
func (s *Session) applyEdits(edits []diff.Edit) error {
	before := s.result.Snapshot()
	var des []document.Edit
	for _, e := range edits {
		if sameLines(before, e) {
			continue
		}
		if de, ok := documentEdit(before, e); ok {
			des = append(des, de)
		}
	}
	if len(des) == 0 {
		return nil
	}
	s.result.PushStackElement()
	s.applying = true
	_, err := s.result.Edit(des, document.EditOptions{Kind: document.KindOther})
	s.applying = false
	if err != nil {
		// the model has the edits and the document does not: put the model right
		s.reportResync(err)
		s.reportResync(s.resync(context.Background()))
		s.changed()
		return err
	}
	s.result.PushStackElement()
	return nil
}

// sameLines tells whether an edit puts the lines that are there already.
func sameLines(snap buffer.Snapshot, e diff.Edit) bool {
	if e.EndLine-e.StartLine != len(e.NewLines) || e.EndLine-1 > snap.LineCount() {
		return false
	}
	for i, line := range e.NewLines {
		got, err := snap.LineContent(e.StartLine - 1 + i)
		if err != nil || got != line {
			return false
		}
	}
	return true
}

// replaceDocument puts lines in the document as its whole text, as one undoable edit that is not handed to the model
// (it has these lines already).
func (s *Session) replaceDocument(lines []string) error {
	text := strings.Join(lines, "\n")
	snap := s.result.Snapshot()
	if snap.String() == text {
		return nil
	}
	whole := buffer.Range{End: snap.PositionAt(snap.Len())}
	s.applying = true
	_, err := s.result.Edit([]document.Edit{{Range: whole, Text: text}}, document.EditOptions{Kind: document.KindOther})
	s.applying = false
	if err != nil {
		return err
	}
	s.result.PushStackElement()
	return nil
}

// documentEdit turns an edit of whole lines of the model's result into the edit of the document; ok is false for an
// edit that changes nothing. The document is the text the model's result is joined from: the lines are ended by
// line feeds except the last.
func documentEdit(snap buffer.Snapshot, e diff.Edit) (document.Edit, bool) {
	n := snap.LineCount()
	s, end := e.StartLine-1, e.EndLine-1 // 0-based, the end exclusive
	text := strings.Join(e.NewLines, "\n")
	switch {
	case end < n:
		// the lines up to a line that is there: every new line ends with a line feed
		if len(e.NewLines) > 0 {
			text += "\n"
		}
		return document.Edit{Range: buffer.Range{Start: buffer.Position{Line: s}, End: buffer.Position{Line: end}}, Text: text}, s != end || len(e.NewLines) > 0
	case s >= n:
		// after the last line: the new lines come after a line feed
		if len(e.NewLines) == 0 {
			return document.Edit{}, false
		}
		last, _ := snap.LineLength(n - 1)
		at := buffer.Position{Line: n - 1, Col: last}
		return document.Edit{Range: buffer.Range{Start: at, End: at}, Text: "\n" + text}, true
	}
	// through the end of the text
	endPos := buffer.Position{Line: n - 1}
	endPos.Col, _ = snap.LineLength(n - 1)
	if len(e.NewLines) == 0 && s > 0 {
		// the line feed before the first removed line goes too
		prev, _ := snap.LineLength(s - 1)
		return document.Edit{Range: buffer.Range{Start: buffer.Position{Line: s - 1, Col: prev}, End: endPos}}, true
	}
	return document.Edit{Range: buffer.Range{Start: buffer.Position{Line: s}, End: endPos}, Text: text}, true
}

// documentChanged follows a change of the result document.
func (s *Session) documentChanged(ev document.ChangeEvent) {
	if s.applying || s.disposed {
		return
	}
	switch ev.Cause {
	case document.CauseLineEnding:
		return // no text changed
	case document.CauseSetText:
		s.handled = nil
		s.reportResync(s.resync(context.Background()))
	default:
		for _, e := range LineEdits(ev) {
			if err := s.model.ApplyResultEdit(e.StartLine, e.EndLine, e.NewLines); err != nil {
				s.reportResync(err)
				s.reportResync(s.resyncTo(context.Background(), ev.After))
				break
			}
		}
		if lines := ev.After.LineCount(); s.model.ResultLineCount() != lines {
			s.reportResync(s.resyncTo(context.Background(), ev.After))
		}
		if ev.Cause == document.CauseUndo || ev.Cause == document.CauseRedo {
			s.restoreHandled(ev.AlternativeVersion)
		}
	}
	s.changed()
}

// reportResync tells the owner of a failure of the model to follow the document.
func (s *Session) reportResync(err error) {
	if err != nil && s.opts.OnError != nil {
		s.opts.OnError(err)
	}
}

// resync puts the model right with the text of the document.
func (s *Session) resync(ctx context.Context) error { return s.resyncTo(ctx, s.result.Snapshot()) }

// resyncTo makes the result of the model the lines of a snapshot with the hunks of a line diff, so that the regions
// that the changes miss keep their states.
func (s *Session) resyncTo(ctx context.Context, snap buffer.Snapshot) error {
	s.resyncs++
	want := snapshotLines(snap)
	if s.model.ResultLineCount() == 0 {
		return s.model.ApplyResultEdit(1, 1, want)
	}
	res, err := diff.ComputeLines(ctx, s.model.Result(), want, diff.Options{MaxComputationTime: resyncTimeLimit})
	if err != nil {
		return err
	}
	for i := len(res.Changes) - 1; i >= 0; i-- {
		c := res.Changes[i]
		if err := s.model.ApplyResultEdit(c.Original.StartLine, c.Original.EndLine, slices.Clone(want[c.Modified.StartLine-1:c.Modified.EndLine-1])); err != nil {
			return err
		}
	}
	return nil
}

// snapshotLines are the lines of a snapshot, without their line breaks.
func snapshotLines(snap buffer.Snapshot) []string {
	lines := make([]string, 0, snap.LineCount())
	for _, line := range snap.Lines(0) {
		lines = append(lines, line)
	}
	return lines
}

// LineEdits turns a change event of a document into replacements of whole lines for the merge model
// (diff.MergeModel.ApplyResultEdit): the changes that share a line are one edit, each edit is made of the lines of
// the text before the change it replaces and the lines of the text after it that take their place. The edits are
// from the last to the first, so each is valid against the text before the event whatever the ones after it did.
func LineEdits(ev document.ChangeEvent) []diff.Edit {
	// group is a stretch of lines of the text before (oldFirst up to oldEnd, the end excluded, counted from 0) and
	// the lines of the text after that take their place
	type group struct{ oldFirst, oldEnd, newFirst, newEnd int }
	var groups []group
	for _, c := range ev.Changes {
		o, n := c.OldRange, c.NewRange
		g := group{oldFirst: o.Start.Line, oldEnd: o.End.Line + 1, newFirst: n.Start.Line, newEnd: n.End.Line + 1}
		if o.Start.Col == 0 && o.End.Col == 0 && n.Start.Col == 0 && n.End.Col == 0 {
			// the change starts and ends at the start of lines, on both sides: whole lines come out and go in, and the line
			// the change ends at is not touched
			g.oldEnd, g.newEnd = o.End.Line, n.End.Line
		}
		if k := len(groups); k > 0 && (g.oldFirst < groups[k-1].oldEnd || g.oldFirst == groups[k-1].oldFirst) {
			groups[k-1].oldEnd, groups[k-1].newEnd = g.oldEnd, g.newEnd
			continue
		}
		groups = append(groups, g)
	}
	edits := make([]diff.Edit, 0, len(groups))
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		var lines []string
		for line, text := range ev.After.Lines(g.newFirst) {
			if line >= g.newEnd {
				break
			}
			lines = append(lines, text)
		}
		edits = append(edits, diff.Edit{StartLine: g.oldFirst + 1, EndLine: g.oldEnd + 1, NewLines: lines})
	}
	return edits
}
