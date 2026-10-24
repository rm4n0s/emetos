package document

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/errors"
)

// Saving (SPEC §6.3, §3.1). A save is three steps so that the write, which waits for the disk, can
// run on a goroutine while the document is used on the UI thread only:
//
//  1. BeginSave (or BeginSaveAs) on the owner of the document: it runs the save participants, takes
//     the text, the encoding and the line ending, and remembers the version of the text;
//  2. SaveJob.Write on any goroutine: it checks that the file is as the document last saw it and
//     replaces it atomically;
//  3. FinishSave on the owner again: the version that was written becomes the saved version, so a
//     document that was edited while the write went on stays dirty, with the stat of the file as it
//     is now.
//
// Save and SaveAs do the three steps one after the other for callers that need no goroutine.
//
// Ported from VS Code: src/vs/workbench/services/textfile/common/textFileEditorModel.ts (doSave:
// the version id taken before the write, the dirty state cleared only when it is unchanged after it)

// SaveReason says why a document is saved; the participants behave differently for an automatic save.
type SaveReason int

const (
	// SaveExplicit is a save the user asked for (VS Code's SaveReason.EXPLICIT).
	SaveExplicit SaveReason = iota
	// SaveAuto is a save the editor makes by itself after a delay (files.autoSave "afterDelay"; VS Code's
	// SaveReason.AUTO). Only this reason keeps the white space in front of the cursors.
	SaveAuto
	// SaveFocusChange is an auto save because an editor lost the focus (files.autoSave "onFocusChange";
	// VS Code's SaveReason.FOCUS_CHANGE).
	SaveFocusChange
	// SaveWindowChange is an auto save because the window lost the focus (files.autoSave "onFocusChange" and
	// "onWindowChange"; VS Code's SaveReason.WINDOW_CHANGE).
	SaveWindowChange
)

// String names the reason as VS Code's SaveReason is named in its logs.
func (r SaveReason) String() string {
	switch r {
	case SaveExplicit:
		return "explicit"
	case SaveAuto:
		return "auto"
	case SaveFocusChange:
		return "focusChange"
	case SaveWindowChange:
		return "windowChange"
	}
	return "unknown"
}

// SaveOptions say how a document is saved.
type SaveOptions struct {
	// Reason says why the document is saved; participants get it.
	Reason SaveReason
	// Overwrite saves over a read-only file (its owner write permission is added), and lets SaveAs replace a
	// file that exists.
	Overwrite bool
	// IgnoreConflict saves although the file was changed by another program since the document read or wrote
	// it (files.saveConflictResolution "overwriteFileOnDisk", or the Overwrite button of the conflict).
	IgnoreConflict bool
	// Participate, when set, runs on the owner of the document before the text is taken, and may
	// edit the document: the save participants (trim trailing whitespace, final newlines). Nil
	// runs none ("Save without Formatting"). An error stops the save.
	Participate func(d *Document, reason SaveReason) error
	// DetectLanguage gives the language of a file from its path and first line, for SaveAs to find the
	// language of the new name; nil keeps the language.
	DetectLanguage func(path, firstLine string) string
}

// SavedEvent tells that the document was written to disk.
type SavedEvent struct {
	// Path is the file that was written.
	Path string
	// Stat is the FileStat of the file after the write.
	Stat fileio.FileStat
	// Clean reports whether the document has no unsaved changes: false when it was edited while the
	// write went on.
	Clean bool
	// As reports that the write was a Save As, which gave the document the URI of the file.
	As bool
}

type savedListener struct{ fn func(SavedEvent) }

// OnSaved calls fn after the document was written to disk (and the version, the stat and, for a Save As,
// the URI were updated). The returned function removes it.
func (d *Document) OnSaved(fn func(SavedEvent)) (unsubscribe func()) {
	l := &savedListener{fn: fn}
	d.savedListeners = append(d.savedListeners, l)
	return func() {
		for i, x := range d.savedListeners {
			if x == l {
				d.savedListeners = append(d.savedListeners[:i:i], d.savedListeners[i+1:]...)
				return
			}
		}
	}
}

// SaveJob is a save that was begun: the text and the properties to write, and the version they belong to.
// Write may be called from any goroutine; nothing else of a job may.
type SaveJob struct {
	doc  *Document
	uri  URI
	path string
	as   bool
	text string
	opts fileio.WriteOptions
	alt  int
	save SaveOptions
}

// Path is the file the job writes.
func (j *SaveJob) Path() string { return j.path }

// Document is the document the job saves.
func (j *SaveJob) Document() *Document { return j.doc }

// As reports that the job is a Save As.
func (j *SaveJob) As() bool { return j.as }

// checkSavable fails for a document that cannot be written.
func (d *Document) checkSavable() error {
	if d.readOnly {
		return errors.New("SaveReadOnlyDocument", "the document is read-only and cannot be saved", "uri", d.uri.String())
	}
	return nil
}

// BeginSave starts saving the document in its file. It fails with the tags SaveReadOnlyDocument,
// SaveNoFile (the document has no file: untitled or virtual; it needs SaveAs) and SaveParticipantFailed.
func (d *Document) BeginSave(opts SaveOptions) (*SaveJob, error) {
	if err := d.checkSavable(); err != nil {
		return nil, err
	}
	if d.uri.Scheme != SchemeFile {
		return nil, errors.New("SaveNoFile", "the document has no file to save in; use Save As", "uri", d.uri.String())
	}
	return d.begin(d.uri.Path, false, opts)
}

// BeginSaveAs starts saving the document in another file, which must not exist unless opts.Overwrite is
// set. It fails with the tags SaveReadOnlyDocument, SavePathInvalid (the path is empty or relative) and
// SaveParticipantFailed. A path that is the document's own file saves it in place, as BeginSave does.
func (d *Document) BeginSaveAs(path string, opts SaveOptions) (*SaveJob, error) {
	if err := d.checkSavable(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("SavePathInvalid", "the path to save in must be absolute", "path", path)
	}
	path = filepath.Clean(path)
	if d.uri.Scheme == SchemeFile && d.uri.Path == path {
		return d.begin(path, false, opts)
	}
	return d.begin(path, true, opts)
}

func (d *Document) begin(path string, as bool, opts SaveOptions) (*SaveJob, error) {
	if opts.Participate != nil {
		if err := opts.Participate(d, opts.Reason); err != nil {
			if _, ok := errors.FromError(err); ok {
				return nil, err
			}
			return nil, errors.NewErr("SaveParticipantFailed", err).SetMetadata("uri", d.uri.String())
		}
	}
	w := fileio.WriteOptions{Encoding: d.encoding, LineEnding: d.lineEnding, BOM: d.hasBOM, Overwrite: opts.Overwrite}
	switch {
	case as:
		w.FailIfExists = !opts.Overwrite
	case !opts.IgnoreConflict && d.uri.Scheme == SchemeFile && !d.stat.ModTime.IsZero():
		stat := d.stat
		w.IfUnchanged = &stat
	}
	j := &SaveJob{doc: d, uri: d.uri, path: path, as: as, text: d.Text(), opts: w, alt: d.altVersion, save: opts}
	if d.inflight == nil {
		d.inflight = map[*SaveJob]struct{}{}
	}
	d.inflight[j] = struct{}{}
	return j, nil
}

// Write writes the text of the job to its file and returns the FileStat of the result. It fails with the
// tags of fileio.Write: FileModifiedSince (another program changed the file since the document last read
// or wrote it), FileExists (a Save As over a file), FileReadOnly, FileNotFound (the folder is missing),
// FilePermissionDenied, FileWriteFailed, EncodingCannotRepresent and WriteCancelled. A file that was
// deleted since the document read it is made again.
func (j *SaveJob) Write(ctx context.Context) (fileio.FileStat, error) {
	return fileio.Write(ctx, j.path, j.text, j.opts)
}

// SaveResult is what a finished save did to the document.
type SaveResult struct {
	// Stat is the FileStat of the written file.
	Stat fileio.FileStat
	// Clean reports whether the document has no unsaved changes now.
	Clean bool
	// LanguageChanged reports that a Save As found another language for the new name.
	LanguageChanged bool
}

// FinishSave records that the job was written with the given stat. The text of the job is now the saved
// text, so the document is clean unless it was edited since BeginSave; it keeps the stat of the file
// either way, so that the next save does not take the document's own write for a change by another
// program. A Save As gives the document the URI of the file (SetURI tells its listeners) and looks for the
// language of the new name. Undo history is kept.
func (d *Document) FinishSave(j *SaveJob, stat fileio.FileStat) SaveResult {
	res := SaveResult{Stat: stat}
	delete(d.inflight, j)
	d.saveGen++
	if j.as {
		d.savedVersion, d.stat = j.alt, stat
		d.setExternal(false, nil) // the file of the new name is there as it was written
		d.SetURI(FileURI(j.path))
		res.LanguageChanged = d.detectLanguage(j.path, j.save.DetectLanguage)
	} else if d.uri == j.uri {
		d.savedVersion, d.stat = j.alt, stat
		d.setExternal(false, nil) // written: the file is there again if it was gone, and the changes are saved
	}
	res.Clean = !d.Dirty()
	ev := SavedEvent{Path: j.path, Stat: stat, Clean: res.Clean, As: j.as}
	for _, l := range append([]*savedListener(nil), d.savedListeners...) {
		l.fn(ev)
	}
	return res
}

// detectLanguage gives the document the language of its new name. A name that no language claims keeps the
// language the document has (an untitled document the user set to Go and saved as "Makefile.in" stays Go).
func (d *Document) detectLanguage(path string, detect func(path, firstLine string) string) bool {
	if detect == nil {
		return false
	}
	firstLine, _, _ := strings.Cut(d.Text(), "\n")
	id := detect(path, firstLine)
	if id == "" || id == DefaultLanguage || id == d.languageID {
		return false
	}
	d.languageID = id
	return true
}

// Save writes the document to its file: BeginSave, Write and FinishSave.
func (d *Document) Save(ctx context.Context, opts SaveOptions) (SaveResult, error) {
	j, err := d.BeginSave(opts)
	if err != nil {
		return SaveResult{}, err
	}
	stat, err := j.Write(ctx)
	if err != nil {
		j.Abort()
		return SaveResult{}, err
	}
	return d.FinishSave(j, stat), nil
}

// SaveAs writes the document to another file and makes that file the document's own: BeginSaveAs, Write and
// FinishSave. The file the document had stays as it was.
func (d *Document) SaveAs(ctx context.Context, path string, opts SaveOptions) (SaveResult, error) {
	j, err := d.BeginSaveAs(path, opts)
	if err != nil {
		return SaveResult{}, err
	}
	stat, err := j.Write(ctx)
	if err != nil {
		j.Abort()
		return SaveResult{}, err
	}
	return d.FinishSave(j, stat), nil
}

// RevertJob is a revert that was begun: the file to read and the encoding to read it in. Read may be
// called from any goroutine; nothing else of a job may.
type RevertJob struct {
	doc      *Document
	path     string
	untitled bool
	read     fileio.ReadOptions
}

// BeginRevert starts reading the document again from its file, in the encoding it has now ("Reopen with
// Encoding" is kept). An untitled document has nothing to read: its revert makes it empty. It fails with the
// tag RevertNoFile (a virtual document).
func (d *Document) BeginRevert(read fileio.ReadOptions) (*RevertJob, error) {
	switch d.uri.Scheme {
	case SchemeUntitled:
		return &RevertJob{doc: d, untitled: true}, nil
	case SchemeFile:
	default:
		return nil, errors.New("RevertNoFile", "the document has no file to read again", "uri", d.uri.String())
	}
	read.Encoding, read.ForceEncoding, read.AllowBinary = d.encoding, true, true
	return &RevertJob{doc: d, path: d.uri.Path, read: read}, nil
}

// Read reads the file of the job; it gives an empty result for an untitled document. It fails with the tags
// of fileio.Read.
func (j *RevertJob) Read(ctx context.Context) (fileio.ReadResult, error) {
	if j.untitled {
		return fileio.ReadResult{}, nil
	}
	return fileio.Read(ctx, j.path, j.read)
}

// FinishRevert replaces the text and the properties of the document with what the job read (Reload): the
// unsaved changes and the undo history are dropped and the document counts as saved. An untitled document
// becomes empty.
func (d *Document) FinishRevert(j *RevertJob, res fileio.ReadResult) {
	if j.untitled {
		d.SetText("")
		return
	}
	d.Reload(res)
}

// Revert reads the document again from its file and drops the unsaved changes and the undo history:
// BeginRevert, Read and FinishRevert. It fails with the tags RevertNoFile and those of fileio.Read.
func (d *Document) Revert(ctx context.Context, read fileio.ReadOptions) error {
	j, err := d.BeginRevert(read)
	if err != nil {
		return err
	}
	res, err := j.Read(ctx)
	if err != nil {
		return err
	}
	d.FinishRevert(j, res)
	return nil
}

// SetSaveEncoding makes the document save in another encoding ("Save with Encoding"): the encoding changes,
// and so does the byte order mark, which UTF-16 and "UTF-8 with BOM" have and the others do not (as VS Code's
// encoding.ts addBOM). The text stays as it is. It fails with the tag SaveEncodingUnknown.
func (d *Document) SetSaveEncoding(id string) error {
	if _, ok := fileio.LookupEncoding(id); !ok {
		return errors.New("SaveEncodingUnknown", "there is no such encoding", "encoding", id)
	}
	d.encoding = id
	d.hasBOM = id == fileio.UTF8BOM || id == fileio.UTF16LE || id == fileio.UTF16BE
	return nil
}
