package edit

import (
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Result is what an editing command does: the edits to apply, the selections
// that follow and the kind that decides how the edits join the undo history
// (document.EditOptions.Kind). The caller applies Edits with Document.Edit, using
// Selections as the selections after, and sets them as the new cursors.
type Result struct {
	// Edits is one batch: sorted, not overlapping, every range in the text before.
	// It is empty when the command changes nothing.
	Edits []document.Edit
	// Selections are the selections in the text after the edits, in the order of the
	// selections the command was given. A cursor whose edits overlapped the edits
	// of an earlier cursor is dropped, as in VS Code, so there can be fewer.
	Selections []buffer.Selection
	// Kind is the undo kind of the edits.
	Kind document.EditKind
	// AutoClosed are the closing characters the command inserted, in the text after the edits:
	// the editor tracks them (SPEC §8.6.4).
	AutoClosed []AutoClosed
	// FollowUp, when it is not nil, is a second command to run once this one is applied, as its own
	// undo step: it is given the commands over the text after this result and the selections now.
	// It reports false when it has nothing to do, for instance when the text is not what it
	// expected.
	FollowUp func(c Commands, sels []buffer.Selection) (Result, bool)
}

// AutoClosed is a closing character that the editor typed for the user, such as the ")" after a
// typed "(": typing the closing character over it, or Backspace after the opening one, takes it
// away again (SPEC §8.6.4).
type AutoClosed struct {
	// Character is where the closing text is.
	Character buffer.Range
	// Enclosing is the typed opening character and the closing text; the cursor must stay inside
	// it for the closing text to count as auto-closed.
	Enclosing buffer.Range
}

// plan is what a command does for one cursor: its edits, in the text before, and
// a function that finds the cursor's selection after, given where the inserted
// text of each of its edits ended up.
type plan struct {
	edits     []document.Edit
	selection func(m *mapper, now []buffer.Range) buffer.Selection
	// custom makes build use selection even when there are no edits, for a command
	// that moves the cursor without changing the text.
	custom bool
	// closed, when it is not nil, tells the closing character the plan inserted.
	closed func(now []buffer.Range) AutoClosed
}

// replacePlan replaces r by text and leaves a cursor after it.
func replacePlan(r buffer.Range, text string) plan {
	return plan{
		edits:     []document.Edit{{Range: r, Text: text}},
		selection: func(_ *mapper, now []buffer.Range) buffer.Selection { return buffer.NewCursor(now[0].End) },
	}
}

// mappedEdit is an edit with the range its text took in the text after the batch.
type mappedEdit struct {
	document.Edit
	now          buffer.Range
	owner, index int
}

// mapper moves positions of the text before a batch into the text after it.
type mapper struct {
	edits []mappedEdit // sorted by position
}

// advance is the position after text, written at p; text only has "\n" line breaks.
func advance(p buffer.Position, text string) buffer.Position {
	n := strings.Count(text, "\n")
	if n == 0 {
		return buffer.Position{Line: p.Line, Col: p.Col + len(text)}
	}
	return buffer.Position{Line: p.Line + n, Col: len(text) - strings.LastIndexByte(text, '\n') - 1}
}

// old is where a position of the text before ends up. A position at an edit's end
// (or at an insertion) goes after the inserted text; a position inside a replaced
// range goes to the start of the new text.
func (m *mapper) old(p buffer.Position) buffer.Position { return m.at(p, true) }

// at is old; with after false, text inserted exactly at p is left after it.
func (m *mapper) at(p buffer.Position, after bool) buffer.Position {
	i := sort.Search(len(m.edits), func(i int) bool { return m.edits[i].Range.End.Compare(p) > 0 })
	if i < len(m.edits) && m.edits[i].Range.Start.Compare(p) < 0 {
		return m.edits[i].now.Start
	}
	if !after {
		for i > 0 && m.edits[i-1].Range.Start == p && m.edits[i-1].Range.End == p {
			i--
		}
	}
	if i == 0 {
		return p
	}
	prev := m.edits[i-1]
	if p.Line == prev.Range.End.Line {
		return buffer.Position{Line: prev.now.End.Line, Col: prev.now.End.Col + p.Col - prev.Range.End.Col}
	}
	return buffer.Position{Line: p.Line + prev.now.End.Line - prev.Range.End.Line, Col: p.Col}
}

// track moves a selection through the edits as a decoration moves: a selection
// that is not empty does not grow at its ends (text inserted at its start goes
// before it, text inserted at its end after it); a caret goes after text inserted
// at it, unless it stays before it.
func (m *mapper) track(s buffer.Selection, caretStaysBefore bool) buffer.Selection {
	if s.IsEmpty() {
		return buffer.NewCursor(m.at(s.Active, !caretStaysBefore))
	}
	start, end := m.at(s.Start(), true), m.at(s.End(), false)
	if s.Anchor.Compare(s.Active) <= 0 {
		return buffer.Selection{Anchor: start, Active: end}
	}
	return buffer.Selection{Anchor: end, Active: start}
}

// build turns the plans of the cursors (nil for a cursor that does nothing) into a
// Result. Edits that overlap the edits of an earlier cursor are dropped with their
// cursor, as VS Code does when it executes commands.
func build(sels []buffer.Selection, plans []plan, kind document.EditKind) Result {
	type op struct {
		edit         document.Edit
		owner, index int
	}
	var ops []op
	for i, p := range plans {
		for j, e := range p.edits {
			ops = append(ops, op{e, i, j})
		}
	}
	dropped := map[int]bool{}
	for {
		byEnd := append([]op(nil), ops...)
		sort.SliceStable(byEnd, func(i, j int) bool { return buffer.CompareUsingEnds(byEnd[i].edit.Range, byEnd[j].edit.Range) > 0 })
		loser := -1
		for i := 1; i < len(byEnd); i++ {
			prev, cur := byEnd[i-1], byEnd[i]
			if prev.edit.Range.Start.IsBefore(cur.edit.Range.End) {
				loser = max(prev.owner, cur.owner)
				break
			}
		}
		if loser < 0 {
			break
		}
		if loser == 0 {
			return Result{Selections: append([]buffer.Selection(nil), sels...), Kind: kind}
		}
		dropped[loser] = true
		kept := ops[:0]
		for _, o := range ops {
			if o.owner != loser {
				kept = append(kept, o)
			}
		}
		ops = kept
	}

	sort.SliceStable(ops, func(i, j int) bool { return buffer.CompareUsingStarts(ops[i].edit.Range, ops[j].edit.Range) < 0 })
	m := &mapper{edits: make([]mappedEdit, len(ops))}
	var prevOld, prevNew buffer.Position
	for i, o := range ops {
		var start buffer.Position
		switch {
		case i > 0 && o.edit.Range.Start.Line == prevOld.Line:
			start = buffer.Position{Line: prevNew.Line, Col: prevNew.Col + o.edit.Range.Start.Col - prevOld.Col}
		case i > 0:
			start = buffer.Position{Line: o.edit.Range.Start.Line + prevNew.Line - prevOld.Line, Col: o.edit.Range.Start.Col}
		default:
			start = o.edit.Range.Start
		}
		end := advance(start, o.edit.Text)
		m.edits[i] = mappedEdit{Edit: o.edit, now: buffer.Range{Start: start, End: end}, owner: o.owner, index: o.index}
		prevOld, prevNew = o.edit.Range.End, end
	}

	now := make([][]buffer.Range, len(plans))
	for i, p := range plans {
		now[i] = make([]buffer.Range, len(p.edits))
	}
	res := Result{Kind: kind}
	for _, me := range m.edits {
		now[me.owner][me.index] = me.now
		if !me.Range.IsEmpty() || me.Text != "" {
			res.Edits = append(res.Edits, me.Edit)
		}
	}
	for i, s := range sels {
		switch {
		case dropped[i]:
		case i >= len(plans) || (len(plans[i].edits) == 0 && !plans[i].custom):
			res.Selections = append(res.Selections, buffer.Selection{Anchor: m.old(s.Anchor), Active: m.old(s.Active)})
		default:
			res.Selections = append(res.Selections, plans[i].selection(m, now[i]))
			if plans[i].closed != nil {
				res.AutoClosed = append(res.AutoClosed, plans[i].closed(now[i]))
			}
		}
	}
	return res
}

// normalizeNewlines turns "\r\n" and "\r" into "\n", the only line break in a
// document.
func normalizeNewlines(text string) string {
	if !strings.Contains(text, "\r") {
		return text
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// Commands are the editing commands: each reads a text and the selections of the
// cursors and returns a Result, without changing anything (SPEC §7.2).
type Commands struct {
	// Model is the text the commands read.
	Model Model
	// Options are the settings they read.
	Options Options
	mv      Mover
}

// NewCommands makes the commands of a text.
func NewCommands(m Model, o Options) Commands {
	return Commands{Model: m, Options: o, mv: NewMover(m, o, nil)}
}

// validSelection is a selection with both ends inside the text.
func (c Commands) validSelection(s buffer.Selection) buffer.Selection {
	return buffer.Selection{Anchor: c.Model.ValidatePosition(s.Anchor), Active: c.Model.ValidatePosition(s.Active)}
}

// textIn is the text of a range of the model, with "\n" line breaks.
func (c Commands) textIn(r buffer.Range) string {
	if r.Start.Line == r.End.Line {
		return c.Model.LineContent(r.Start.Line)[r.Start.Col:r.End.Col]
	}
	var sb strings.Builder
	sb.WriteString(c.Model.LineContent(r.Start.Line)[r.Start.Col:])
	for line := r.Start.Line + 1; line < r.End.Line; line++ {
		sb.WriteByte('\n')
		sb.WriteString(c.Model.LineContent(line))
	}
	sb.WriteByte('\n')
	sb.WriteString(c.Model.LineContent(r.End.Line)[:r.End.Col])
	return sb.String()
}

// validRange is the range of a selection with both ends inside the text.
func (c Commands) validRange(s buffer.Selection) buffer.Range {
	return buffer.RangeFromPositions(c.Model.ValidatePosition(s.Anchor), c.Model.ValidatePosition(s.Active))
}
