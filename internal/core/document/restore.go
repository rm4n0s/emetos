package document

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// Unsaved is the unsaved state of a document that comes back from a backup (SPEC §10.10, hot exit).
type Unsaved struct {
	// Text is the text the document had, with "\n" line endings.
	Text string
	// Encoding, HasBOM and LineEnding are the properties the file is saved with; an empty Encoding or
	// LineEnding keeps what the document has.
	Encoding   string
	HasBOM     bool
	LineEnding fileio.LineEnding
	// LanguageID is the language the document had; empty keeps the document's.
	LanguageID string
	// Stat, when not zero, is the stat of the file the text was based on. A document whose file changed since
	// takes it, so that a save fails with FileModifiedSince until the user chooses to overwrite.
	Stat fileio.FileStat
	// Orphaned reports that the file is gone: the document is orphaned and a save makes the file again.
	Orphaned bool
	// Conflict, when not nil, is the stat the file has on the disk now, which differs from Stat: the document
	// is conflicted (Conflicted).
	Conflict *fileio.FileStat
}

// RestoreUnsaved puts the unsaved state of a backup into a document that was just made (from the file as it is
// on the disk now, or empty for an untitled document or one whose file is gone): the text is set as one edit, so
// the document is dirty and an undo returns to the text it had, and the properties, the stat and the state of
// the file are set as the backup says. It reports whether the document has unsaved changes afterwards: false when
// the backup holds the text the document has already (and the file is not gone). It fails with the tag
// DocumentReadOnly.
func (d *Document) RestoreUnsaved(u Unsaved) (dirty bool, err error) {
	if err := d.checkWritable(); err != nil {
		return false, err
	}
	if u.Encoding != "" {
		if _, ok := fileio.LookupEncoding(u.Encoding); ok {
			d.encoding, d.hasBOM = u.Encoding, u.HasBOM
		}
	}
	if u.LineEnding != "" {
		d.lineEnding = u.LineEnding
	}
	if u.LanguageID != "" {
		d.languageID = u.LanguageID
	}
	if u.Text != d.Text() {
		snap := d.buf.Snapshot()
		d.PushStackElement()
		_, err := d.Edit([]Edit{{Range: buffer.Range{End: snap.PositionAt(snap.Len())}, Text: u.Text}}, EditOptions{})
		if err != nil {
			return false, err
		}
		d.PushStackElement()
	}
	if !u.Stat.ModTime.IsZero() {
		d.stat = u.Stat
	}
	if u.Orphaned {
		if !d.Dirty() {
			d.savedVersion = -1 // the file is gone: the document keeps its content even when it is empty
		}
		d.setExternal(true, nil)
	} else if u.Conflict != nil {
		conflict := *u.Conflict
		d.setExternal(false, &conflict)
	}
	return d.Dirty(), nil
}
