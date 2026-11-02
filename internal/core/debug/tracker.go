package debug

import (
	"context"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Tracker makes the breakpoints of the open documents follow their edits. Each breakpoint of a document has a decoration (a range of
// one character at the breakpoint, or at the first character of its line, that never grows when text is typed at its edges), which the
// document moves with every edit; Sync reads where the decorations are and tells the service, which sends the new places to the
// adapters when the file is saved.
//
// Documents belong to the UI thread, so a Tracker is used from it only: the window calls Track when an editor opens a document,
// Sync after its edits (and when the breakpoints change), and Untrack when the document closes.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointEditorContribution.ts (createBreakpointDecorations,
// onModelDecorationsChanged)
type Tracker struct {
	svc  *Service
	docs map[string]*tracked
}

type tracked struct {
	doc   *document.Document
	owner int
	// ids maps a breakpoint id to the id of its decoration.
	ids map[string]int
}

// NewTracker makes a tracker for a service.
func NewTracker(svc *Service) *Tracker { return &Tracker{svc: svc, docs: map[string]*tracked{}} }

// Track starts following the breakpoints of the document that is the file at path and puts a decoration on each of them. Tracking a
// path again replaces the document.
func (t *Tracker) Track(path string, doc *document.Document) {
	t.Untrack(path)
	tr := &tracked{doc: doc, owner: doc.AllocOwners(1), ids: map[string]int{}}
	t.docs[path] = tr
	t.reconcile(path, tr)
	t.svc.model.SetDocumentDirty(path, doc.Dirty())
}

// Untrack stops following a document and removes its decorations.
func (t *Tracker) Untrack(path string) {
	tr := t.docs[path]
	if tr == nil {
		return
	}
	tr.doc.RemoveDecorationsOf(tr.owner)
	delete(t.docs, path)
	t.svc.model.SetDocumentDirty(path, false)
}

// Sync brings the breakpoints and the decorations of a tracked document in line. First the places the decorations are at become the
// places of the breakpoints (they moved with the edits); then the decorations of breakpoints that are gone are removed and the
// breakpoints that have none get one. It reports how many breakpoints moved. A path that is not tracked does nothing.
func (t *Tracker) Sync(ctx context.Context, path string) (int, error) {
	tr := t.docs[path]
	if tr == nil {
		return 0, nil
	}
	t.svc.model.SetDocumentDirty(path, tr.doc.Dirty())
	text := tr.doc.Snapshot()
	updates := map[string]BreakpointUpdate{}
	for _, bp := range t.svc.model.Breakpoints(BreakpointFilter{OriginalPath: path}) {
		did, ok := tr.ids[bp.ID]
		if !ok {
			continue
		}
		d, ok := tr.doc.Decoration(did)
		if !ok {
			continue
		}
		line := d.Range.Start.Line + 1
		column := 0
		if bp.RequestedColumn > 0 {
			lc, err := text.LineContent(d.Range.Start.Line)
			if err == nil {
				column = byteToUTF16Column(lc, d.Range.Start.Col)
			} else {
				column = bp.RequestedColumn
			}
		}
		if line != bp.RequestedLine || column != bp.RequestedColumn {
			u := BreakpointUpdate{Line: line}
			if bp.RequestedColumn > 0 {
				u.Column = ip(column)
			}
			updates[bp.ID] = u
		}
	}
	if len(updates) > 0 {
		if err := t.svc.UpdateBreakpoints(ctx, path, updates, true); err != nil {
			return 0, err
		}
	}
	t.reconcile(path, tr)
	return len(updates), nil
}

// reconcile makes the decorations of a document the ones its breakpoints need.
func (t *Tracker) reconcile(path string, tr *tracked) {
	text := tr.doc.Snapshot()
	bps := t.svc.model.Breakpoints(BreakpointFilter{OriginalPath: path})
	want := map[string]Breakpoint{}
	for _, bp := range bps {
		want[bp.ID] = bp
	}
	var remove []int
	for id, did := range tr.ids {
		bp, ok := want[id]
		if !ok {
			remove = append(remove, did)
			delete(tr.ids, id)
			continue
		}
		// A breakpoint whose requested place is no longer the decoration's (changed through the model) gets a new decoration.
		if d, ok := tr.doc.Decoration(did); ok && d.Range.Start.Line+1 == bp.RequestedLine {
			continue
		}
		remove = append(remove, did)
		delete(tr.ids, id)
	}
	var add []document.NewDecoration
	var addIDs []string
	for _, bp := range bps {
		if _, ok := tr.ids[bp.ID]; ok {
			continue
		}
		if bp.RequestedLine < 1 || bp.RequestedLine > text.LineCount() {
			continue
		}
		add = append(add, document.NewDecoration{Range: decorationRange(text, bp), Options: document.DecorationOptions{Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "debug-breakpoint"}})
		addIDs = append(addIDs, bp.ID)
	}
	if len(remove) == 0 && len(add) == 0 {
		return
	}
	ids := tr.doc.DeltaDecorations(tr.owner, remove, add)
	for i, id := range ids {
		tr.ids[addIDs[i]] = id
	}
}

// decorationRange is the range a breakpoint's decoration covers: one character at its column, or at the first character of its line
// that is not a space (a range must have a width, VS Code's issue 20688).
func decorationRange(text buffer.Snapshot, bp Breakpoint) buffer.Range {
	line := bp.RequestedLine - 1
	content, err := text.LineContent(line)
	if err != nil {
		return buffer.Range{Start: buffer.Position{Line: line}, End: buffer.Position{Line: line}}
	}
	col := 0
	if bp.RequestedColumn > 0 {
		col = utf16ColumnToByte(content, bp.RequestedColumn)
	} else {
		for col < len(content) && (content[col] == ' ' || content[col] == '\t') {
			col++
		}
	}
	end := col
	if col < len(content) {
		_, size := utf8.DecodeRuneInString(content[col:])
		end = col + size
	}
	return buffer.Range{Start: buffer.Position{Line: line, Col: col}, End: buffer.Position{Line: line, Col: end}}
}

// utf16ColumnToByte is the byte offset of a column counted from 1 in UTF-16 code units, clamped to the line.
func utf16ColumnToByte(line string, column int) int {
	units := 0
	for i, r := range line {
		if units >= column-1 {
			return i
		}
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return len(line)
}

// byteToUTF16Column is the column, counted from 1 in UTF-16 code units, of a byte offset in a line.
func byteToUTF16Column(line string, off int) int {
	units := 0
	for i, r := range line {
		if i >= off {
			break
		}
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return units + 1
}

// ByteOfColumn is the byte offset in a line of a column counted from 1 in UTF-16 code units (how the protocol counts), clamped to the line.
func ByteOfColumn(line string, column int) int { return utf16ColumnToByte(line, column) }

// ColumnOfByte is the column, counted from 1 in UTF-16 code units, of a byte offset in a line.
func ColumnOfByte(line string, off int) int { return byteToUTF16Column(line, off) }
