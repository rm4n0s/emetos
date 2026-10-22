package document

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/errors"
)

// Default indentation, as VS Code's.
const (
	DefaultTabSize      = 4
	DefaultInsertSpaces = true
)

// DefaultLanguage is the language of a document nothing else is known about.
const DefaultLanguage = "plaintext"

// Options are what a document starts with besides its text.
type Options struct {
	// LanguageID is the language; empty means "plaintext".
	LanguageID string
	// TabSize is editor.tabSize; zero means 4.
	TabSize int
	// InsertSpaces is editor.insertSpaces. Because false is also the zero value,
	// NewOptions starts from VS Code's true.
	InsertSpaces bool
	// LineEnding is the document's line ending; empty means LF.
	LineEnding fileio.LineEnding
	// ReadOnly makes the document refuse edits.
	ReadOnly bool
}

// NewOptions are Options with VS Code's defaults.
func NewOptions() Options {
	return Options{LanguageID: DefaultLanguage, TabSize: DefaultTabSize, InsertSpaces: DefaultInsertSpaces, LineEnding: fileio.LF}
}

// Document is a text buffer with what an editor needs to know about it: its URI,
// version ids that tell whether it is dirty, an undo history, and the file
// properties kept for saving (SPEC §7.1). A Document belongs to one goroutine; its
// Snapshots can go to others.
type Document struct {
	uri URI
	buf *buffer.Buffer

	version      int
	altVersion   int
	savedVersion int

	languageID   string
	encoding     string
	lineEnding   fileio.LineEnding
	hasBOM       bool
	tabSize      int
	insertSpaces bool
	largeFile    bool
	readOnly     bool
	stat         fileio.FileStat

	history   history
	listeners []*listener
}

// New makes a document that holds text, which must use "\n" only. Its encoding
// is "utf8" and it counts as saved.
func New(uri URI, text string, opts Options) *Document {
	d := &Document{
		uri:          uri,
		buf:          buffer.New(text),
		version:      1,
		altVersion:   1,
		savedVersion: 1,
		languageID:   opts.LanguageID,
		encoding:     fileio.UTF8,
		lineEnding:   opts.LineEnding,
		tabSize:      opts.TabSize,
		insertSpaces: opts.InsertSpaces,
		readOnly:     opts.ReadOnly,
	}
	if d.languageID == "" {
		d.languageID = DefaultLanguage
	}
	if d.lineEnding == "" {
		d.lineEnding = fileio.LF
	}
	if d.tabSize == 0 {
		d.tabSize = DefaultTabSize
	}
	return d
}

// FromRead makes the document of a file that fileio.Read returned: its text,
// encoding, line ending, byte order mark, large-file mode and stat.
func FromRead(uri URI, res fileio.ReadResult, opts Options) *Document {
	if opts.LineEnding == "" {
		opts.LineEnding = res.LineEnding
	}
	d := New(uri, res.Text, opts)
	d.encoding = res.Encoding
	d.lineEnding = res.LineEnding
	d.hasBOM = res.HasBOM
	d.largeFile = res.LargeFile
	d.stat = res.Stat
	return d
}

// URI is the document's identity.
func (d *Document) URI() URI { return d.uri }

// Snapshot is the text now, as an immutable value.
func (d *Document) Snapshot() buffer.Snapshot { return d.buf.Snapshot() }

// Text is the whole text, with "\n" line endings.
func (d *Document) Text() string { return d.buf.String() }

// Version is the version id: it goes up with every change, undo and redo included.
func (d *Document) Version() int { return d.version }

// AlternativeVersion is the alternative version id: it goes up with every edit,
// and an undo or a redo sets it to the value it had at that point of the
// history, so it is the same again when the text is.
func (d *Document) AlternativeVersion() int { return d.altVersion }

// SavedAlternativeVersion is the alternative version id of the text last saved or read.
func (d *Document) SavedAlternativeVersion() int { return d.savedVersion }

// Dirty reports whether the text differs from the one last saved or read; undoing
// back to the saved text clears it.
func (d *Document) Dirty() bool { return d.altVersion != d.savedVersion }

// MarkSaved records that the text now is the one on disk, with the FileStat the
// write returned.
func (d *Document) MarkSaved(stat fileio.FileStat) {
	d.savedVersion = d.altVersion
	d.stat = stat
}

// LanguageID is the language id.
func (d *Document) LanguageID() string { return d.languageID }

// SetLanguageID changes the language.
func (d *Document) SetLanguageID(id string) { d.languageID = id }

// Encoding is the id of the file's encoding (see fileio.Encodings).
func (d *Document) Encoding() string { return d.encoding }

// SetEncoding changes the encoding the file is saved in.
func (d *Document) SetEncoding(id string) { d.encoding = id }

// LineEnding is the line ending the file is saved with.
func (d *Document) LineEnding() fileio.LineEnding { return d.lineEnding }

// HasBOM reports whether the file has a byte order mark.
func (d *Document) HasBOM() bool { return d.hasBOM }

// SetBOM changes whether the file is saved with a byte order mark.
func (d *Document) SetBOM(has bool) { d.hasBOM = has }

// Indentation is editor.tabSize and editor.insertSpaces for this document.
func (d *Document) Indentation() (tabSize int, insertSpaces bool) { return d.tabSize, d.insertSpaces }

// SetIndentation changes the indentation options, as detecting them from the text or the "Indent Using" commands do.
func (d *Document) SetIndentation(tabSize int, insertSpaces bool) {
	if tabSize < 1 {
		tabSize = DefaultTabSize
	}
	d.tabSize, d.insertSpaces = tabSize, insertSpaces
}

// LargeFile reports whether the document is in large file mode (SPEC §6.4); it is
// decided when the document is made and features check it.
func (d *Document) LargeFile() bool { return d.largeFile }

// ReadOnly reports whether the document refuses edits.
func (d *Document) ReadOnly() bool { return d.readOnly }

// SetReadOnly changes whether the document refuses edits.
func (d *Document) SetReadOnly(readOnly bool) { d.readOnly = readOnly }

// Stat is the FileStat of the last read or write.
func (d *Document) Stat() fileio.FileStat { return d.stat }

func (d *Document) checkWritable() error {
	if d.readOnly {
		return errors.New("DocumentReadOnly", "the document is read-only", "uri", d.uri.String())
	}
	return nil
}
