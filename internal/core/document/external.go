package document

import (
	"context"
	"os"
	"syscall"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/errors"
)

// Files changed on disk (SPEC §3.1 "Files", §10.5). The watcher tells the window that something happened to
// the file of an open document; what the window does with it is decided by looking at the file, not by the kind
// of the event, so a batch of events is idempotent and an event that comes late (or twice) does no harm. Like a
// save, a look is three steps, so that the disk is never touched on the UI thread:
//
//  1. BeginExternal on the owner of the document: it remembers the stat the document has and whether it can be
//     reloaded (it has no unsaved changes);
//  2. ExternalJob.Probe on any goroutine: it looks at the file and, when it differs from what the document last
//     saw and the document may be reloaded, reads it;
//  3. ApplyExternal on the owner again: it applies what was found, unless the document changed meanwhile.
//
// The rules (VS Code's text file models, where a dirty model is never reloaded and an orphaned model is one
// whose file is gone):
//
//   - the file is gone: the document is orphaned (Orphaned), keeps its text and its dirty state;
//   - the file has the stat the document has (size and modification time): nothing changed, and an orphaned
//     or conflicted document is mended (the file came back as it was);
//   - the file differs and the document has no unsaved changes: it is reloaded (Reload);
//   - the file differs and the document has unsaved changes: its text is left alone and it is marked conflicted
//     (Conflicted); a save then fails with FileModifiedSince unless it overwrites, and a revert reads the file.
//
// Ported from VS Code: src/vs/workbench/services/textfile/common/textFileEditorModel.ts (onDidFilesChange: the
// orphaned state, set only when the file is really gone) and
// src/vs/workbench/services/textfile/common/textFileEditorModelManager.ts (onDidFilesChange: models with unsaved
// changes are never reloaded)

// ExternalState is what is known of the file of a document that the document itself does not show.
type ExternalState struct {
	// Orphaned reports that the file is gone from the disk (VS Code's TextFileEditorModelState.ORPHAN). The
	// document keeps its text; a save makes the file again.
	Orphaned bool
	// Conflicted reports that the file changed on the disk while the document has unsaved changes.
	Conflicted bool
}

type externalListener struct{ fn func(ExternalState) }

// ExternalState tells whether the file is gone or was changed under the unsaved changes of the document.
func (d *Document) ExternalState() ExternalState {
	return ExternalState{Orphaned: d.orphaned, Conflicted: d.conflict != nil}
}

// Orphaned reports that the file of the document was deleted (or moved away) by another program.
func (d *Document) Orphaned() bool { return d.orphaned }

// Conflicted reports that the file changed on the disk while the document has unsaved changes.
func (d *Document) Conflicted() bool { return d.conflict != nil }

// DiskStat is the FileStat the file has on the disk, as the last look at it found it; it is only set while
// Conflicted.
func (d *Document) DiskStat() fileio.FileStat {
	if d.conflict == nil {
		return fileio.FileStat{}
	}
	return *d.conflict
}

// OnExternalStateChange calls fn after the orphaned or the conflicted state of the document changed, with the
// state it has now. The returned function removes it.
func (d *Document) OnExternalStateChange(fn func(ExternalState)) (unsubscribe func()) {
	l := &externalListener{fn: fn}
	d.externalListeners = append(d.externalListeners, l)
	return func() {
		for i, x := range d.externalListeners {
			if x == l {
				d.externalListeners = append(d.externalListeners[:i:i], d.externalListeners[i+1:]...)
				return
			}
		}
	}
}

// setExternal sets the orphaned state and the stat that conflicts (nil for none), and tells the listeners when
// either changed. It reports whether it did.
func (d *Document) setExternal(orphaned bool, conflict *fileio.FileStat) bool {
	before := d.ExternalState()
	d.orphaned, d.conflict = orphaned, conflict
	after := d.ExternalState()
	if before == after {
		return false
	}
	for _, l := range append([]*externalListener(nil), d.externalListeners...) {
		l.fn(after)
	}
	return true
}

// SameStat reports whether two stats describe the same file content as far as the editor can tell: the same
// size and the same modification time. This is the test of SPEC §10.5, for the editor's own writes and for the
// changes of other programs.
func SameStat(a, b fileio.FileStat) bool {
	return a.Size == b.Size && a.ModTime.Equal(b.ModTime)
}

// SaveInFlight reports that a save of the document was begun and is not finished: its file is being written.
func (d *Document) SaveInFlight() bool { return len(d.inflight) > 0 }

// Abort tells the document that the write of the job failed or will not be finished, so that the document is
// not taken for one that is being written. The window calls it when Write returns an error.
func (j *SaveJob) Abort() { delete(j.doc.inflight, j) }

// Disk is what looking at a file asks of the file system. The default is the operating system's; tests put
// their own in to make a read fail or to change a file between the look and the read.
type Disk interface {
	// Stat gives the stat of the file, and whether it exists. A path that is gone, or that is a folder or has a
	// file in the place of a folder above it, does not exist; other failures are errors.
	Stat(ctx context.Context, path string) (stat fileio.FileStat, exists bool, err error)
	// Read reads the file as fileio.Read does.
	Read(ctx context.Context, path string, opts fileio.ReadOptions) (fileio.ReadResult, error)
}

// OSDisk is the Disk of the operating system.
type OSDisk struct{}

// Stat implements Disk. It fails with the tag ExternalStatFailed (a failure that is not a missing file).
func (OSDisk) Stat(ctx context.Context, path string) (fileio.FileStat, bool, error) {
	if err := ctx.Err(); err != nil {
		return fileio.FileStat{}, false, errors.NewErr("ExternalStatFailed", err).SetMetadata("path", path)
	}
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return fileio.FileStat{}, false, nil
	case err == nil:
		return fileio.FileStat{ModTime: info.ModTime(), Size: info.Size(), Mode: info.Mode()}, true, nil
	case os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR):
		return fileio.FileStat{}, false, nil
	}
	return fileio.FileStat{}, false, errors.NewErr("ExternalStatFailed", err).SetMetadata("path", path)
}

// Read implements Disk.
func (OSDisk) Read(ctx context.Context, path string, opts fileio.ReadOptions) (fileio.ReadResult, error) {
	return fileio.Read(ctx, path, opts)
}

// ExternalJob is a look at the file of a document that was begun. Probe may be called from any goroutine;
// nothing else of a job may.
type ExternalJob struct {
	doc  *Document
	uri  URI
	path string
	// alt, gen and stat are what the document was when the job began: a document that is another by the end is
	// looked at again.
	alt, gen int
	stat     fileio.FileStat
	// read reports that the job reads a file that differs: the document had no unsaved changes.
	read bool
	opts fileio.ReadOptions
}

// Path is the file the job looks at.
func (j *ExternalJob) Path() string { return j.path }

// BeginExternal starts a look at the file of the document after another program may have changed it. read says
// how a file is read when the document is reloaded (the size limits, binary files); the encoding is the one the
// document has. It fails with the tag ExternalNoFile for a document that has no file (untitled, virtual).
func (d *Document) BeginExternal(read fileio.ReadOptions) (*ExternalJob, error) {
	if d.uri.Scheme != SchemeFile {
		return nil, errors.New("ExternalNoFile", "the document has no file to look at", "uri", d.uri.String())
	}
	// the encoding is kept (VS Code resolves a model again with the encoding it has); a byte order mark that the
	// file has now still wins, and no guessing replaces the choice of the user
	read.Encoding, read.ForceEncoding, read.AutoGuess = d.encoding, false, false
	return &ExternalJob{
		doc: d, uri: d.uri, path: d.uri.Path, alt: d.altVersion, gen: d.saveGen, stat: d.stat,
		read: !d.Dirty(), opts: read,
	}, nil
}

// ExternalProbe is what a look at the file found.
type ExternalProbe struct {
	// Exists reports that the file is there.
	Exists bool
	// Stat is the stat of the file (of the one that was read, when Read is set).
	Stat fileio.FileStat
	// Read is the file read again, set when it differs from what the document has and the document had no
	// unsaved changes.
	Read *fileio.ReadResult
	// Unstable reports that the file changed while it was looked at (a program is still writing it): the
	// next event of the watcher is the one to act on.
	Unstable bool
	// Err is a failure of the look or of the read; the document is left as it is and the look is tried again
	// at the next event.
	Err error
}

// Probe looks at the file: whether it is there, its stat, and, when it differs from the stat the document has and
// the document can be reloaded, its content. The stat is taken before and after the read; a file that changed
// in between is being written, and the probe says so (Unstable) instead of returning half a write. disk nil is
// the operating system's. Failures are in the result (tags ExternalStatFailed and those of fileio.Read).
func (j *ExternalJob) Probe(ctx context.Context, disk Disk) ExternalProbe {
	if disk == nil {
		disk = OSDisk{}
	}
	before, exists, err := disk.Stat(ctx, j.path)
	if err != nil {
		return ExternalProbe{Err: tagged(err, "ExternalStatFailed", j.path)}
	}
	if !exists {
		return ExternalProbe{}
	}
	p := ExternalProbe{Exists: true, Stat: before}
	if SameStat(before, j.stat) || !j.read {
		return p
	}
	res, err := disk.Read(ctx, j.path, j.opts)
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "FileNotFound" {
			p.Unstable = true // gone since the look: the watcher says so next
			return p
		}
		p.Err = tagged(err, "ExternalReadFailed", j.path)
		return p
	}
	after, exists, err := disk.Stat(ctx, j.path)
	if err != nil {
		p.Err = tagged(err, "ExternalStatFailed", j.path)
		return p
	}
	if !exists || !SameStat(before, res.Stat) || !SameStat(res.Stat, after) {
		p.Unstable = true
		return p
	}
	p.Stat, p.Read = res.Stat, &res
	return p
}

// tagged keeps an error of this package as it is and gives any other the tag.
func tagged(err error, tag, path string) error {
	if _, ok := errors.FromError(err); ok {
		return err
	}
	return errors.NewErr(tag, err).SetMetadata("path", path)
}

// ExternalOutcome says what ApplyExternal did.
type ExternalOutcome int

const (
	// ExternalUnchanged means the file is as the document has it: nothing was reloaded. An orphaned or
	// conflicted state was cleared when the file is back as it was.
	ExternalUnchanged ExternalOutcome = iota
	// ExternalReloaded means the file differed and the document had no unsaved changes: its text and
	// properties were replaced (Reload), so the editors must keep their view state.
	ExternalReloaded
	// ExternalConflicted means the file differs and the document has unsaved changes: the text was left alone
	// and the document is conflicted.
	ExternalConflicted
	// ExternalOrphaned means the file is gone: the document is orphaned.
	ExternalOrphaned
	// ExternalRetry means the file could not be read or was being written: nothing was changed, and the next
	// event of the watcher should be acted on.
	ExternalRetry
	// ExternalStale means the document changed while the file was looked at (it was edited, saved or renamed):
	// the look is of no use and should be made again.
	ExternalStale
	// ExternalDeferred means a save of the document is going on: the look should be made again when it ends.
	ExternalDeferred
)

// String names the outcome.
func (o ExternalOutcome) String() string {
	switch o {
	case ExternalUnchanged:
		return "unchanged"
	case ExternalReloaded:
		return "reloaded"
	case ExternalConflicted:
		return "conflicted"
	case ExternalOrphaned:
		return "orphaned"
	case ExternalRetry:
		return "retry"
	case ExternalStale:
		return "stale"
	case ExternalDeferred:
		return "deferred"
	}
	return "unknown"
}

// ExternalResult is what ApplyExternal did.
type ExternalResult struct {
	// Outcome says what happened.
	Outcome ExternalOutcome
	// StateChanged reports that the orphaned or the conflicted state of the document changed.
	StateChanged bool
	// Err is the failure behind ExternalRetry, if there was one.
	Err error
}

// ApplyExternal applies what Probe found to the document, on the goroutine that owns it. Nothing is changed when
// the document is not as it was when the job began (a different file, saved, or edited in a way that matters):
// the outcome is ExternalStale, and a look must be made again. While a save of the document is in flight the
// outcome is ExternalDeferred: the file is being written, and what is on the disk now means nothing.
func (d *Document) ApplyExternal(j *ExternalJob, p ExternalProbe) ExternalResult {
	switch {
	case j.doc != d || d.uri != j.uri || d.saveGen != j.gen || !SameStat(d.stat, j.stat):
		return ExternalResult{Outcome: ExternalStale}
	case d.SaveInFlight():
		return ExternalResult{Outcome: ExternalDeferred}
	case p.Err != nil:
		return ExternalResult{Outcome: ExternalRetry, Err: p.Err}
	case !p.Exists:
		changed := d.setExternal(true, nil)
		return ExternalResult{Outcome: ExternalOrphaned, StateChanged: changed}
	case p.Unstable:
		return ExternalResult{Outcome: ExternalRetry}
	case SameStat(p.Stat, d.stat):
		changed := d.setExternal(false, nil)
		return ExternalResult{Outcome: ExternalUnchanged, StateChanged: changed}
	case d.Dirty():
		// the unsaved changes are never touched
		stat := p.Stat
		changed := d.setExternal(false, &stat)
		return ExternalResult{Outcome: ExternalConflicted, StateChanged: changed}
	case p.Read == nil:
		// the document was dirty when the look began and is clean now (the changes were undone): look again
		return ExternalResult{Outcome: ExternalStale}
	}
	res := *p.Read
	if res.Text == d.Text() && res.Encoding == d.encoding && res.HasBOM == d.hasBOM && res.LineEnding == d.lineEnding {
		// only the time of the file changed (touch, a save with the same content): the history stays
		d.stat = res.Stat
		changed := d.setExternal(false, nil)
		return ExternalResult{Outcome: ExternalUnchanged, StateChanged: changed}
	}
	before := d.ExternalState()
	d.Reload(res)
	return ExternalResult{Outcome: ExternalReloaded, StateChanged: before != d.ExternalState()}
}

// CheckExternal looks at the file of the document and applies what it finds: BeginExternal, Probe and
// ApplyExternal in a row, for callers that need no goroutine.
func (d *Document) CheckExternal(ctx context.Context, disk Disk, read fileio.ReadOptions) (ExternalResult, error) {
	j, err := d.BeginExternal(read)
	if err != nil {
		return ExternalResult{}, err
	}
	return d.ApplyExternal(j, j.Probe(ctx, disk)), nil
}
