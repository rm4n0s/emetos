package snippet

import (
	"cmp"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetSession.ts

// DecorationOwner is the owner of the decorations that mark the tab stops of a snippet, for an
// Editor that names none (the owners 1 to 5 are the auto-closed characters, folding, find and the
// two highlights of the first editor of a document).
const DecorationOwner = 6

// The class names of the decorations of the tab stops: the final tab stop looks different.
const (
	ClassPlaceholder      = "snippetPlaceholder"
	ClassFinalPlaceholder = "finishSnippetPlaceholder"
)

// Editor is what a snippet session works on: the document and the cursors of an editor.
type Editor struct {
	Doc     *document.Document
	Cursors *edit.Cursors
	// Owner is the owner of the decorations of the tab stops; DecorationOwner when 0. Editors
	// that show the same document need different owners.
	Owner int
}

// owner is the owner of the decorations of the tab stops.
func (e Editor) owner() int {
	if e.Owner > 0 {
		return e.Owner
	}
	return DecorationOwner
}

// Env is what the variables of a snippet need besides the document, which the editor gathers
// when a snippet is inserted.
type Env struct {
	// WordSeparators finds the word at a cursor (TM_CURRENT_WORD); the default separators of
	// editor.wordSeparators when nil.
	WordSeparators *edit.WordClassifier
	// FilePath and RelativePath are the path of the file of the document (its label when it has
	// none) and the path relative to the folder that is open.
	FilePath, RelativePath string
	// Comments are the comment tokens of the language of the document.
	Comments CommentResolver
	// Workspace is the folder that is open.
	Workspace WorkspaceResolver
	// ClipboardSpread is editor.multiCursorPaste being "spread".
	ClipboardSpread bool
	// Now is the clock of the time variables; time.Now when nil. TimezoneName is the IANA name of
	// the zone.
	Now          func() time.Time
	TimezoneName string
	// Random is the source of randomness of the random variables; the system's when nil.
	Random io.Reader
}

// InsertOptions are the options of inserting a snippet.
type InsertOptions struct {
	// OverwriteBefore and OverwriteAfter are the bytes before and after each cursor that the
	// snippet replaces (the word that was typed to find it).
	OverwriteBefore, OverwriteAfter int
	// AdjustWhitespace indents the lines of the snippet as the line it is inserted into, and
	// normalizes tabs and spaces to the document's.
	AdjustWhitespace bool
	// UndoStopBefore and UndoStopAfter make the insertion an undo step of its own.
	UndoStopBefore, UndoStopAfter bool
	// ClipboardText is the text for CLIPBOARD, read before the snippet is inserted.
	ClipboardText string
	// Overtyped gives the text that the cursor with an index typed over last, or nil.
	Overtyped func(index int) *Overtyped
	// JoinUndo makes the insertion part of the open undo group of the document (document.EditOptions.Join), so that the
	// edits an action made just before it are undone with it.
	JoinUndo bool
}

// DefaultInsertOptions are the options of inserting a snippet as VS Code does by default: the
// whitespace is adjusted and the insertion is an undo step.
func DefaultInsertOptions() InsertOptions {
	return InsertOptions{AdjustWhitespace: true, UndoStopBefore: true, UndoStopAfter: true}
}

// Edit is a text to insert as a snippet in a range, for Controller.Apply.
type Edit struct {
	// Range is replaced by the template.
	Range buffer.Range
	// Template is the snippet.
	Template string
	// KeepWhitespace, when it is not nil, overrules AdjustWhitespace for this edit.
	KeepWhitespace *bool
}

type direction int

const (
	stay direction = iota
	forward
	backward
)

// oneSnippet is one snippet of a session: the one inserted at one cursor.
type oneSnippet struct {
	ed      Editor
	snippet *TextmateSnippet
	// lineWS is the white space of the line the snippet was inserted into, as far as the cursor.
	lineWS string
	groups [][]*Placeholder
	// groupIdx is the group of placeholders (of one index) that is active, or -1.
	groupIdx int
	// offset is where the text of the snippet starts in the document, or -1 before it is inserted.
	offset int
	decos  map[*Placeholder]int
}

func newOneSnippet(ed Editor, snippet *TextmateSnippet, lineWS string) *oneSnippet {
	return &oneSnippet{ed: ed, snippet: snippet, lineWS: lineWS, groups: groupPlaceholders(snippet.Placeholders()), groupIdx: -1, offset: -1}
}

// groupPlaceholders sorts placeholders by index, final one last, and groups the ones of an index.
func groupPlaceholders(all []*Placeholder) [][]*Placeholder {
	sorted := slices.Clone(all)
	slices.SortStableFunc(sorted, ComparePlaceholders)
	var groups [][]*Placeholder
	for _, p := range sorted {
		if n := len(groups); n == 0 || ComparePlaceholders(groups[n-1][0], p) != 0 {
			groups = append(groups, []*Placeholder{p})
		} else {
			groups[n-1] = append(groups[n-1], p)
		}
	}
	return groups
}

func (o *oneSnippet) dispose() {
	if o.decos != nil {
		for _, id := range o.decos {
			o.ed.Doc.RemoveDecorations(id)
		}
	}
	o.groups = nil
}

// decoOptions are the options of the decoration of a tab stop: the active one grows when text is
// typed at its edges, the others (and the final one) do not.
func decoOptions(final, active bool) document.DecorationOptions {
	class, st := ClassPlaceholder, document.NeverGrowsWhenTypingAtEdges
	if final {
		class = ClassFinalPlaceholder
	} else if active {
		st = document.AlwaysGrowsWhenTypingAtEdges
	}
	return document.DecorationOptions{Stickiness: st, ClassName: class}
}

// rangeOf is the range of a placeholder of a snippet inserted at an offset.
func rangeOf(snap buffer.Snapshot, snippet *TextmateSnippet, base int, p *Placeholder) buffer.Range {
	start := base + snippet.Offset(p)
	return buffer.Range{Start: snap.PositionAt(start), End: snap.PositionAt(start + snippet.FullLen(p))}
}

func (o *oneSnippet) initDecorations() {
	if o.decos != nil {
		return
	}
	o.decos = map[*Placeholder]int{}
	snap := o.ed.Doc.Snapshot()
	placeholders := o.snippet.Placeholders()
	adds := make([]document.NewDecoration, len(placeholders))
	for i, p := range placeholders {
		adds[i] = document.NewDecoration{Range: rangeOf(snap, o.snippet, o.offset, p), Options: decoOptions(p.IsFinalTabstop(), false)}
	}
	for i, id := range o.ed.Doc.DeltaDecorations(o.ed.owner(), nil, adds) {
		o.decos[placeholders[i]] = id
	}
}

// decorationRange is the range of the decoration of a placeholder; false when it has none.
func (o *oneSnippet) decorationRange(p *Placeholder) (buffer.Range, bool) {
	id, ok := o.decos[p]
	if !ok {
		return buffer.Range{}, false
	}
	d, ok := o.ed.Doc.Decoration(id)
	return d.Range, ok
}

// move goes to the next group of placeholders (forward), the previous (backward) or stays at the
// active one, and gives the selections that select the placeholders of the group. Placeholders
// that were emptied by the user are skipped.
func (o *oneSnippet) move(dir direction) ([]buffer.Selection, error) {
	for {
		o.initDecorations()
		doc := o.ed.Doc

		// transform the text of the placeholders that are left, when they have a transform
		if o.groupIdx >= 0 {
			var ops []document.Edit
			for _, p := range o.groups[o.groupIdx] {
				if p.Transform == nil {
					continue
				}
				if r, ok := o.decorationRange(p); ok {
					lines := splitLines(p.Transform.Resolve(doc.Snapshot().Text(r)))
					for i := 1; i < len(lines); i++ {
						lines[i] = o.normalize(o.lineWS + lines[i])
					}
					ops = append(ops, document.Edit{Range: r, Text: strings.Join(lines, "\n")})
				}
			}
			if len(ops) > 0 {
				if _, err := doc.Edit(ops, document.EditOptions{}); err != nil {
					return nil, err
				}
			}
		}

		couldSkip := false
		switch {
		case dir == forward && o.groupIdx < len(o.groups)-1:
			o.groupIdx++
			couldSkip = true
		case dir == backward && o.groupIdx > 0:
			o.groupIdx--
			couldSkip = true
		}

		active := map[*Placeholder]bool{}
		var selections []buffer.Selection
		for _, p := range o.groups[o.groupIdx] {
			r, ok := o.decorationRange(p)
			// skip this index when the decoration is empty but the placeholder was not: a strong
			// hint that it was deleted (all the placeholders of the group must say so)
			couldSkip = couldSkip && o.hasBeenCollapsed(p)
			if !ok {
				continue
			}
			selections = append(selections, buffer.SelectionFromRange(r, buffer.LTR))
			doc.ChangeDecorationOptions(o.decos[p], decoOptions(p.IsFinalTabstop(), true))
			active[p] = true
			for _, enclosing := range o.snippet.EnclosingPlaceholders(p) {
				if id, ok := o.decos[enclosing]; ok {
					doc.ChangeDecorationOptions(id, decoOptions(enclosing.IsFinalTabstop(), true))
					active[enclosing] = true
				}
			}
		}
		// the tab stops that are not active never grow
		for p, id := range o.decos {
			if !active[p] {
				doc.ChangeDecorationOptions(id, decoOptions(p.IsFinalTabstop(), false))
			}
		}
		if !couldSkip {
			return selections, nil
		}
	}
}

// normalize rewrites the indentation of a line as the document's.
func (o *oneSnippet) normalize(line string) string {
	tabSize, insertSpaces := o.ed.Doc.Indentation()
	return edit.NormalizeIndentation(line, tabSize, insertSpaces)
}

// hasBeenCollapsed tells whether a placeholder or one that holds it is empty although it was
// not when the snippet was written.
func (o *oneSnippet) hasBeenCollapsed(p *Placeholder) bool {
	for m := Marker(p); m != nil; m = m.Parent() {
		if ph, ok := m.(*Placeholder); ok {
			r, ok := o.decorationRange(ph)
			if (!ok || r.IsEmpty()) && len(ph.String()) > 0 {
				return true
			}
		}
	}
	return false
}

func (o *oneSnippet) isAtFirstPlaceholder() bool { return o.groupIdx <= 0 || len(o.groups) == 0 }

func (o *oneSnippet) isAtLastPlaceholder() bool { return o.groupIdx == len(o.groups)-1 }

func (o *oneSnippet) hasPlaceholder() bool { return len(o.snippet.Placeholders()) > 0 }

// isTrivial tells that a snippet has no placeholder, or only the final one at its very end.
func (o *oneSnippet) isTrivial() bool {
	phs := o.snippet.Placeholders()
	switch len(phs) {
	case 0:
		return true
	case 1:
		return phs[0].IsFinalTabstop() && RightMostDescendant(o.snippet) == Marker(phs[0])
	}
	return false
}

// possibleSelection is the ranges of the placeholders of one index.
type possibleSelection struct {
	index  int
	ranges []buffer.Range
}

// computePossibleSelections are the ranges of the placeholders by index, without the final tab
// stop; an index whose placeholder lost its decoration is left out.
func (o *oneSnippet) computePossibleSelections() []possibleSelection {
	var result []possibleSelection
	for _, group := range o.groups {
		var cur *possibleSelection
		for _, p := range group {
			if p.IsFinalTabstop() {
				break
			}
			if cur == nil {
				result = append(result, possibleSelection{index: p.Index})
				cur = &result[len(result)-1]
			}
			r, ok := o.decorationRange(p)
			if !ok {
				// one of the placeholders lost its decoration: pretend the placeholder (and its
				// mirrors) does not exist any more
				result = result[:len(result)-1]
				break
			}
			cur.ranges = append(cur.ranges, r)
		}
	}
	return result
}

// activeChoice is the choice of the active placeholder and where its text is.
func (o *oneSnippet) activeChoice() (*Choice, buffer.Range, bool) {
	if o.decos == nil || o.groupIdx < 0 || o.groupIdx >= len(o.groups) {
		return nil, buffer.Range{}, false
	}
	p := o.groups[o.groupIdx][0]
	choice := p.Choice()
	if choice == nil {
		return nil, buffer.Range{}, false
	}
	r, ok := o.decorationRange(p)
	if !ok {
		return nil, buffer.Range{}, false
	}
	return choice, r, true
}

func (o *oneSnippet) hasChoice() bool {
	result := false
	o.snippet.Walk(func(m Marker) bool {
		_, result = m.(*Choice)
		return !result
	})
	return result
}

func (o *oneSnippet) activePlaceholderCount() int {
	if o.groupIdx < 0 {
		return 0
	}
	return len(o.groups[o.groupIdx])
}

// merge puts nested snippets in the active placeholders of this one, one for each, in the order
// of the placeholders. The tab stops of a nested snippet are ordered right after the one it is in:
// they get fractional orders between that index and the next, and then all the indexes are
// numbered 1, 2, ... again.
func (o *oneSnippet) merge(others *[]*oneSnippet) {
	doc := o.ed.Doc
	snap := doc.Snapshot()
	order := map[*Placeholder]float64{}
	for _, p := range o.snippet.Placeholders() {
		order[p] = float64(p.Index)
	}
	var adds []document.NewDecoration
	var added []*Placeholder
	var remove []int
	for _, ph := range o.groups[o.groupIdx] {
		nested := (*others)[0]
		*others = (*others)[1:]
		base := order[ph]
		last := 0
		for _, np := range nested.snippet.Placeholders() {
			last = max(last, np.Index)
		}
		// every nested order is between base and base+1, whatever the number of tab stops
		step := float64(last + 2)
		for _, np := range nested.snippet.Placeholders() {
			if np.IsFinalTabstop() {
				order[np] = base + float64(last+1)/step
			} else {
				order[np] = base + float64(np.Index)/step
			}
		}
		o.snippet.Replace(ph, nested.snippet.Children())

		// remove the placeholder the snippet is inserted at, and its decoration
		if id, ok := o.decos[ph]; ok {
			remove = append(remove, id)
			delete(o.decos, ph)
		}
		// a decoration for each new placeholder watches how it grows and shrinks
		for _, np := range nested.snippet.Placeholders() {
			adds = append(adds, document.NewDecoration{Range: rangeOf(snap, nested.snippet, nested.offset, np), Options: decoOptions(false, false)})
			added = append(added, np)
		}
	}
	doc.RemoveDecorations(remove...)
	for i, id := range doc.DeltaDecorations(o.ed.owner(), nil, adds) {
		o.decos[added[i]] = id
	}

	// number the indexes 1, 2, ... again: fractions would lose their precision when nested deeply
	var unique []float64
	for _, p := range o.snippet.Placeholders() {
		if order[p] != 0 {
			unique = append(unique, order[p])
		}
	}
	slices.Sort(unique)
	unique = slices.Compact(unique)
	for _, p := range o.snippet.Placeholders() {
		if v := order[p]; v == 0 {
			p.Index = 0
		} else {
			idx, _ := slices.BinarySearch(unique, v)
			p.Index = idx + 1
		}
	}
	o.groups = groupPlaceholders(o.snippet.Placeholders())
}

// enclosingRange is the range that covers all the placeholders.
func (o *oneSnippet) enclosingRange() (buffer.Range, bool) {
	var result buffer.Range
	found := false
	for p := range o.decos {
		if r, ok := o.decorationRange(p); ok {
			if !found {
				result, found = r, true
			} else {
				result = result.Union(r)
			}
		}
	}
	return result, found
}

// compareRangesUsingStarts orders ranges by their start, then by their end.
func compareRangesUsingStarts(a, b buffer.Range) int {
	if c := a.Start.Compare(b.Start); c != 0 {
		return c
	}
	return a.End.Compare(b.End)
}

// leadingWhitespaceOf is the white space a line starts with, up to a column.
func leadingWhitespaceOf(line string, col int) string {
	return leadingWhitespace(line[:min(max(col, 0), len(line))])
}

// AdjustWhitespace puts the lines of the snippet as the line at a position: the first line of
// text is only normalized, when it is at the start of the snippet; the others (and a text that
// follows a line break) are indented as the line at the position is, and normalized. It returns
// the white space the line starts with, as far as the position. Only the texts in filter are
// changed when it is not nil.
func AdjustWhitespace(doc *document.Document, position buffer.Position, adjustIndentation bool, snippet *TextmateSnippet, filter map[Marker]bool) string {
	line, _ := doc.Snapshot().LineContent(position.Line)
	lineWS := leadingWhitespaceOf(line, position.Col)
	tabSize, insertSpaces := doc.Indentation()
	normalize := func(s string) string { return edit.NormalizeIndentation(s, tabSize, insertSpaces) }

	var text string
	haveText := false
	snippet.Walk(func(m Marker) bool {
		t, ok := m.(*Text)
		if !ok {
			return true
		}
		if _, inChoice := t.Parent().(*Choice); inChoice {
			return true
		}
		if filter != nil && !filter[m] {
			return true
		}
		lines := splitLines(t.Value)
		if adjustIndentation {
			// the start of the snippet does not get the extra indentation, it is only normalized;
			// all the following lines do, and so does a text that follows a line break
			if offset := snippet.Offset(t); offset == 0 {
				lines[0] = normalize(lines[0])
			} else {
				if !haveText {
					text, haveText = snippet.String(), true
				}
				if prev := text[offset-1]; prev == '\n' || prev == '\r' {
					lines[0] = normalize(lineWS + lines[0])
				}
			}
			for i := 1; i < len(lines); i++ {
				lines[i] = normalize(lineWS + lines[i])
			}
		}
		if value := strings.Join(lines, "\n"); value != t.Value {
			snippet.Replace(t, []Marker{NewText(value)})
			haveText = false
		}
		return true
	})
	return lineWS
}

// AdjustSelection widens a selection around its active position by bytes before and after,
// inside the line.
func AdjustSelection(snap buffer.Snapshot, sel buffer.Selection, overwriteBefore, overwriteAfter int) buffer.Selection {
	if overwriteBefore == 0 && overwriteAfter == 0 {
		return sel
	}
	p := sel.Active
	r := snap.ValidateRange(buffer.Range{
		Start: buffer.Position{Line: p.Line, Col: max(p.Col-overwriteBefore, 0)},
		End:   buffer.Position{Line: p.Line, Col: p.Col + overwriteAfter},
	})
	return buffer.SelectionFromRange(r, sel.Direction())
}

// firstNonWhitespace is the column of the first character of a line that is not a space or a
// tab, or -1.
func firstNonWhitespace(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return -1
}

// resolverFor is the variable resolver of a cursor.
func (env Env) resolverFor(file FileResolver, clip ClipboardResolver, sel SelectionResolver) VariableResolver {
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	return CompositeResolver{
		file, clip, sel, env.Comments,
		TimeResolver{Now: now(), TimezoneName: env.TimezoneName},
		env.Workspace,
		RandomResolver{Reader: env.Random},
	}
}

// pending is what an insertion makes: the edits and the snippets, by the index of the cursor or
// the edit they come from.
type pending struct {
	edits    []document.Edit
	snippets []*oneSnippet
}

func (s *Session) words() *edit.WordClassifier {
	if s.env.WordSeparators != nil {
		return s.env.WordSeparators
	}
	return edit.NewWordClassifier(edit.DefaultWordSeparators)
}

// fromSelections makes the edits and snippets that insert a template at the cursors.
func (s *Session) fromSelections(template string, overwriteBefore, overwriteAfter int, enforceFinalTabstop bool, opts InsertOptions) pending {
	doc := s.ed.Doc
	snap := doc.Snapshot()
	selections := s.ed.Cursors.Selections()
	if len(selections) == 0 {
		return pending{}
	}
	primary := selections[0]

	// what the extensions of the primary cursor select: a secondary cursor grows only when its
	// extensions select the same text
	firstBeforeText := snap.Text(AdjustSelection(snap, primary, overwriteBefore, 0).Range())
	firstAfterText := snap.Text(AdjustSelection(snap, primary, 0, overwriteAfter).Range())

	// the first non-whitespace column decides whether keepWhitespace is overruled for the others
	firstLine, _ := snap.LineContent(primary.Active.Line)
	firstLineFirstNonWhitespace := firstNonWhitespace(firstLine)

	// sort the selections by their start, but remember the index: the primary cursor stays primary
	type indexed struct {
		sel buffer.Selection
		idx int
	}
	sorted := make([]indexed, len(selections))
	for i, sel := range selections {
		sorted[i] = indexed{sel, i}
	}
	slices.SortStableFunc(sorted, func(a, b indexed) int { return compareRangesUsingStarts(a.sel.Range(), b.sel.Range()) })

	out := pending{edits: make([]document.Edit, len(selections)), snippets: make([]*oneSnippet, len(selections))}
	for _, it := range sorted {
		sel, idx := it.sel, it.idx
		extBefore := AdjustSelection(snap, sel, overwriteBefore, 0)
		extAfter := AdjustSelection(snap, sel, 0, overwriteAfter)
		if snap.Text(extBefore.Range()) != firstBeforeText {
			extBefore = sel
		}
		if snap.Text(extAfter.Range()) != firstAfterText {
			extAfter = sel
		}
		snippetRange := buffer.Range{Start: extBefore.Start(), End: extAfter.End()}

		snippet := NewParser().Parse(template, ParseOpts{InsertFinalTabstop: true, EnforceFinalTabstop: enforceFinalTabstop})
		// the white space of the template follows the rules of this place (it can differ for each
		// cursor): when asked for, or when this is a secondary cursor whose line has another indent
		line, _ := snap.LineContent(sel.Active.Line)
		adjust := opts.AdjustWhitespace || (idx > 0 && firstLineFirstNonWhitespace != firstNonWhitespace(line))
		lineWS := AdjustWhitespace(doc, snippetRange.Start, adjust, snippet, nil)

		var overtyped *Overtyped
		if opts.Overtyped != nil {
			overtyped = opts.Overtyped(idx)
		}
		snippet.ResolveVariables(s.env.resolverFor(
			FileResolver{Path: s.env.FilePath, RelativePath: s.env.RelativePath},
			ClipboardResolver{Text: opts.ClipboardText, Index: idx, Count: len(selections), Spread: s.env.ClipboardSpread},
			SelectionResolver{Values: SelectionValuesOf(snap, sel, idx, s.words(), overtyped)},
		))
		out.edits[idx] = document.Edit{Range: snippetRange, Text: snippet.String()}
		out.snippets[idx] = newOneSnippet(s.ed, snippet, lineWS)
	}
	return out
}

// fromEdits makes the edit and the snippet that insert a list of templates in ranges as one
// snippet, so that the tab stops of all of them are those of one session.
func (s *Session) fromEdits(snippetEdits []Edit, enforceFinalTabstop bool, opts InsertOptions) pending {
	if len(snippetEdits) == 0 {
		return pending{}
	}
	doc := s.ed.Doc
	snap := doc.Snapshot()
	parser := NewParser()
	snippet := NewTextmateSnippet()

	type indexed struct {
		edit Edit
		idx  int
	}
	sorted := make([]indexed, len(snippetEdits))
	for i, e := range snippetEdits {
		sorted[i] = indexed{e, i}
	}
	slices.SortStableFunc(sorted, func(a, b indexed) int { return compareRangesUsingStarts(a.edit.Range, b.edit.Range) })

	var out pending
	offset := 0
	for i, it := range sorted {
		e, idx := it.edit, it.idx
		// the gaps between the edits are texts, so that the offsets of the placeholders are right
		if i > 0 {
			gap := snap.Text(buffer.Range{Start: sorted[i-1].edit.Range.End, End: e.Range.Start})
			snippet.AppendChild(NewText(gap))
			offset += len(gap)
		}

		// the variables that are resolved already stay as they are: this edit's resolver only
		// touches the ones of its fragment and the clones that parsing fills into earlier
		// placeholders of the same index
		pre := map[*Variable]bool{}
		snippet.Walk(func(m Marker) bool {
			if v, ok := m.(*Variable); ok {
				pre[v] = true
			}
			return true
		})

		newNodes := parser.ParseFragment(e.Template, snippet)
		filter := make(map[Marker]bool, len(newNodes))
		for _, n := range newNodes {
			filter[n] = true
		}
		adjust := opts.AdjustWhitespace
		if e.KeepWhitespace != nil {
			adjust = !*e.KeepWhitespace
		}
		AdjustWhitespace(doc, e.Range.Start, adjust, snippet, filter)

		sel := buffer.SelectionFromRange(e.Range, buffer.LTR)
		var overtyped *Overtyped
		if opts.Overtyped != nil {
			overtyped = opts.Overtyped(idx)
		}
		resolver := s.env.resolverFor(
			FileResolver{Path: s.env.FilePath, RelativePath: s.env.RelativePath},
			ClipboardResolver{Text: opts.ClipboardText, Index: idx, Count: len(sorted), Spread: s.env.ClipboardSpread},
			SelectionResolver{Values: SelectionValuesOf(snap, sel, idx, s.words(), overtyped)},
		)
		snippet.Walk(func(m Marker) bool {
			if v, ok := m.(*Variable); ok && !pre[v] {
				v.Resolve(resolver)
			}
			return true
		})

		text := snippet.String()
		fragment := text[offset:]
		offset = len(text)
		out.edits = append(out.edits, document.Edit{Range: e.Range, Text: fragment})
	}
	parser.EnsureFinalTabstop(snippet, enforceFinalTabstop, true)
	out.snippets = []*oneSnippet{newOneSnippet(s.ed, snippet, "")}
	return out
}

// Session is a snippet that is being filled in: the tab stops, the mirrors and the nested
// snippets of one or more cursors.
type Session struct {
	ed       Editor
	env      Env
	template string
	edits    []Edit
	opts     InsertOptions
	snippets []*oneSnippet
	// join is JoinUndo of the insertion being run.
	join bool
}

// NewSession makes a session that inserts a template at the cursors of an editor.
func NewSession(ed Editor, env Env, template string, opts InsertOptions) *Session {
	return &Session{ed: ed, env: env, template: template, opts: opts}
}

// NewSessionFromEdits makes a session that inserts templates in ranges, as one snippet.
func NewSessionFromEdits(ed Editor, env Env, edits []Edit, opts InsertOptions) *Session {
	return &Session{ed: ed, env: env, edits: edits, opts: opts}
}

// Dispose takes the decorations of the tab stops away.
func (s *Session) Dispose() {
	for _, sn := range s.snippets {
		sn.dispose()
	}
}

// Insert inserts the template, as one edit that is one undo step, and selects the first tab stop
// (or puts the cursors after the text when there is none). It fails with the errors of
// document.Document.Edit (DocumentReadOnly, EditsOverlap).
func (s *Session) Insert() error {
	s.join = s.opts.JoinUndo
	var p pending
	if s.edits != nil {
		p = s.fromEdits(s.edits, false, s.opts)
	} else {
		p = s.fromSelections(s.template, s.opts.OverwriteBefore, s.opts.OverwriteAfter, false, s.opts)
	}
	s.snippets = p.snippets
	if len(p.snippets) == 0 {
		return nil
	}
	return s.run(p, func(ends []buffer.Position) ([]buffer.Selection, error) {
		if s.snippets[0].hasPlaceholder() {
			return s.move(forward)
		}
		sels := make([]buffer.Selection, len(ends))
		for i, e := range ends {
			sels[i] = buffer.NewCursor(e)
		}
		return sels, nil
	})
}

// run applies the edits of an insertion as one batch, tells the snippets where their text is
// and sets the cursors the callback chooses (given where the text of each edit ends).
func (s *Session) run(p pending, selections func(ends []buffer.Position) ([]buffer.Selection, error)) error {
	doc := s.ed.Doc
	before := doc.Snapshot()
	// where the text of each edit starts in the text after the batch
	type placed struct{ idx, start int }
	order := make([]placed, len(p.edits))
	for i, e := range p.edits {
		order[i] = placed{i, before.OffsetAt(before.ValidateRange(e.Range).Start)}
	}
	slices.SortStableFunc(order, func(a, b placed) int { return cmp.Compare(a.start, b.start) })
	starts := make([]int, len(p.edits))
	delta := 0
	for _, o := range order {
		e := p.edits[o.idx]
		r := before.ValidateRange(e.Range)
		starts[o.idx] = o.start + delta
		delta += len(e.Text) - (before.OffsetAt(r.End) - before.OffsetAt(r.Start))
	}

	var failure error
	finish := func() []buffer.Selection {
		after := doc.Snapshot()
		ends := make([]buffer.Position, len(p.edits))
		for i := range p.edits {
			if i < len(p.snippets) {
				p.snippets[i].offset = starts[i]
			}
			ends[i] = after.PositionAt(starts[i] + len(p.edits[i].Text))
		}
		sels, err := selections(ends)
		failure = err
		return sels
	}
	res, err := doc.Edit(p.edits, document.EditOptions{
		Join:   s.join,
		Before: s.ed.Cursors.Selections(),
		After:  func([]document.Edit) []buffer.Selection { return finish() },
	})
	if err != nil {
		return err
	}
	sels := res.Selections
	if !res.Changed {
		// nothing was inserted: the edit did not call back
		sels = finish()
	}
	if failure != nil {
		return failure
	}
	if len(sels) > 0 {
		s.ed.Cursors.SetSelections(sels)
	}
	return nil
}

// Merge inserts a nested snippet at the active tab stops of the session: its tab stops come
// between the one it is in and the next. A nested snippet that is only text, or that cannot be
// matched to the active tab stops, is inserted as text.
func (s *Session) Merge(template string, opts InsertOptions) error {
	s.join = opts.JoinUndo
	p := s.fromSelections(template, opts.OverwriteBefore, opts.OverwriteAfter, true, opts)
	if len(p.snippets) == 0 {
		return nil
	}
	merged := false
	err := s.run(p, func(ends []buffer.Position) ([]buffer.Selection, error) {
		// trivial snippets have no placeholder or are just the final one: they are text insertions
		// and are not merged. A merge needs a nested snippet for every active placeholder: moved
		// cursors can leave fewer
		trivial := p.snippets[0].isTrivial()
		count := 0
		for _, sn := range s.snippets {
			count += sn.activePlaceholderCount()
		}
		canMerge := len(p.snippets) == count
		if !trivial && canMerge {
			rest := slices.Clone(p.snippets)
			for _, sn := range s.snippets {
				sn.merge(&rest)
			}
			merged = true
		}
		if s.snippets[0].hasPlaceholder() && merged {
			return s.move(stay)
		}
		sels := make([]buffer.Selection, len(ends))
		for i, e := range ends {
			sels[i] = buffer.NewCursor(e)
		}
		return sels, nil
	})
	return err
}

// Next goes to the next tab stop and selects it.
func (s *Session) Next() error { return s.step(forward) }

// Prev goes to the previous tab stop and selects it.
func (s *Session) Prev() error { return s.step(backward) }

func (s *Session) step(dir direction) error {
	sels, err := s.move(dir)
	if err != nil {
		return err
	}
	if len(sels) > 0 {
		s.ed.Cursors.SetSelections(sels)
	}
	return nil
}

// move moves every snippet and gives the selections of all of them.
func (s *Session) move(dir direction) ([]buffer.Selection, error) {
	var sels []buffer.Selection
	for _, sn := range s.snippets {
		one, err := sn.move(dir)
		if err != nil {
			return nil, err
		}
		sels = append(sels, one...)
	}
	return sels, nil
}

// IsAtFirstPlaceholder tells whether there is no tab stop before the active one.
func (s *Session) IsAtFirstPlaceholder() bool { return s.snippets[0].isAtFirstPlaceholder() }

// IsAtLastPlaceholder tells whether the active tab stop is the last, which is the final one.
func (s *Session) IsAtLastPlaceholder() bool { return s.snippets[0].isAtLastPlaceholder() }

// HasPlaceholder tells whether the snippet has any tab stop.
func (s *Session) HasPlaceholder() bool { return len(s.snippets) > 0 && s.snippets[0].hasPlaceholder() }

// HasChoice tells whether the snippet has a choice.
func (s *Session) HasChoice() bool { return len(s.snippets) > 0 && s.snippets[0].hasChoice() }

// ActiveChoice is the choice of the active tab stop, and the range of its text.
func (s *Session) ActiveChoice() (*Choice, buffer.Range, bool) {
	if len(s.snippets) == 0 {
		return nil, buffer.Range{}, false
	}
	return s.snippets[0].activeChoice()
}

// IsSelectionWithinPlaceholders tells whether the selections of the editor are inside the tab
// stops of the session: the selections, one for each cursor the snippet was inserted at, must be
// inside the tab stops of one index.
func (s *Session) IsSelectionWithinPlaceholders() bool {
	if !s.HasPlaceholder() {
		return false
	}
	selections := s.ed.Cursors.Selections()
	if len(selections) < len(s.snippets) {
		// the session started with N cursors and has M < N: one snippet has no cursor
		return false
	}
	var indexes []int
	all := map[int][]buffer.Range{}
	for _, sn := range s.snippets {
		possible := sn.computePossibleSelections()
		// for the first snippet find the placeholders that contain a selection; the others must
		// use the same ones
		if len(all) == 0 {
			for _, ps := range possible {
				ranges := slices.Clone(ps.ranges)
				slices.SortStableFunc(ranges, compareRangesUsingStarts)
				for _, sel := range selections {
					if ranges[0].ContainsRange(sel.Range()) {
						all[ps.index] = []buffer.Range{}
						indexes = append(indexes, ps.index)
						break
					}
				}
			}
		}
		if len(all) == 0 {
			// no selection is in any tab stop of the first snippet
			return false
		}
		// add the ranges of this snippet, so that every range of every tab stop is known
		for index := range all {
			for _, ps := range possible {
				if ps.index == index {
					all[index] = append(all[index], ps.ranges...)
				}
			}
		}
	}

	sels := make([]buffer.Range, len(selections))
	for i, sel := range selections {
		sels[i] = sel.Range()
	}
	slices.SortStableFunc(sels, compareRangesUsingStarts)
	for _, index := range indexes {
		ranges, ok := all[index]
		if !ok {
			continue
		}
		if len(ranges) != len(sels) {
			delete(all, index)
			continue
		}
		slices.SortStableFunc(ranges, compareRangesUsingStarts)
		for i := range ranges {
			if !ranges[i].ContainsRange(sels[i]) {
				delete(all, index)
				break
			}
		}
	}
	return len(all) > 0
}

// EnclosingRange is the range that covers the tab stops of the whole session.
func (s *Session) EnclosingRange() (buffer.Range, bool) {
	var result buffer.Range
	found := false
	for _, sn := range s.snippets {
		if r, ok := sn.enclosingRange(); ok {
			if !found {
				result, found = r, true
			} else {
				result = result.Union(r)
			}
		}
	}
	return result, found
}
