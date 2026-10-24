package workspace

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// PathMove is an item that now lives at another path.
type PathMove struct {
	Old, New string
}

// OpResult says what a file operation did, so that the tree and the editors can follow it.
type OpResult struct {
	// Label describes the operation ("Rename a.txt to b.txt"), for the undo history.
	Label string
	// Moves are the items that changed path (a rename, a move, and the undo of either).
	Moves []PathMove
	// Created are the paths that exist now and did not before (a new file, a copy, an item restored
	// from the trash). For a path that was made with the folders above it, it is the topmost new one.
	Created []string
	// Removed are the paths that are gone (trashed, deleted, or removed by an undo).
	Removed []string
	// Targets are the paths the user will want selected or opened afterwards: the new file, the
	// copies, the moved items.
	Targets []string
}

// Empty tells whether nothing happened.
func (r OpResult) Empty() bool {
	return len(r.Moves) == 0 && len(r.Created) == 0 && len(r.Removed) == 0
}

// Events turns the result into the events of the watcher, to apply to the tree at once (the watcher
// reports the same changes later, which applying twice does no harm to).
func (r OpResult) Events() []Event {
	var out []Event
	for _, p := range r.Removed {
		out = append(out, Event{Kind: EventDeleted, Path: p})
	}
	for _, m := range r.Moves {
		out = append(out, Event{Kind: EventRenamed, Path: m.New, OldPath: m.Old})
	}
	for _, p := range r.Created {
		out = append(out, Event{Kind: EventCreated, Path: p})
	}
	return out
}

func (r *OpResult) merge(o OpResult) {
	r.Moves = append(r.Moves, o.Moves...)
	r.Created = append(r.Created, o.Created...)
	r.Removed = append(r.Removed, o.Removed...)
	r.Targets = append(r.Targets, o.Targets...)
}

// FileOpsOptions say how the operations reach the file system.
type FileOpsOptions struct {
	// Trash is the trash that deletes go to and that undo restores from; nil leaves the trash out
	// (Trash fails with FileOpTrashUnavailable, and an undo that has to remove something deletes it).
	Trash *fileio.HomeTrash
	// Rename moves an item on the same file system; os.Rename when nil. Tests replace it to make a
	// move cross file systems (syscall.EXDEV), which moves by copying and deleting.
	Rename func(oldPath, newPath string) error
}

// MoveOptions are the options of Move.
type MoveOptions struct {
	// Overwrite replaces an item that has the name of the one moved (the replaced item goes to the
	// trash, so undo restores it). Without it such a move fails with FileOpDestinationExists.
	Overwrite bool
}

// CopyOptions are the options of Copy.
type CopyOptions struct {
	// Naming is explorer.incrementalNaming: what a copy does when its name is taken ("" is NamingSimple).
	Naming IncrementalNaming
	// Overwrite replaces an item of the same name when Naming is NamingDisabled (the replaced item goes
	// to the trash). Without it such a copy fails with FileOpDestinationExists.
	Overwrite bool
}

// maxUndoRecords is how many operations the history keeps.
const maxUndoRecords = 100

// maxCopyDepth bounds the recursion of a copy of a folder.
const maxCopyDepth = 200

type actionKind int

const (
	actCreate actionKind = iota + 1
	actMove
	actCopy
	actTrash
)

// action is one step of an operation, with what is needed to take it back and to do it again.
type action struct {
	kind actionKind
	// path is the item: the one created, the copy, the one trashed (where it was), the new path of a move.
	path string
	// from is where a moved item was, or the source of a copy.
	from string
	// top is the topmost path that a creation made, and isDir tells that it made a folder.
	top   string
	isDir bool
	// trashed is the item in the trash: what a trash action put there, or what an undo of a creation or
	// of a copy put there.
	trashed *fileio.TrashedItem
	// replaced is the item that a move or a copy overwrote, in the trash.
	replaced *fileio.TrashedItem
}

// UndoPrompt says when taking back an operation asks first (explorer.confirmUndo): VS Code asks before
// undoing what removes files, or all operations, depending on the setting.
type UndoPrompt int

const (
	// PromptNever never asks: undoing a delete only restores files.
	PromptNever UndoPrompt = iota
	// PromptVerbose asks only when explorer.confirmUndo is "verbose" (a rename, a move).
	PromptVerbose
	// PromptDefault asks when it is "verbose" or "default" (a paste, which an undo removes).
	PromptDefault
	// PromptAlways asks whatever the setting says (a new file or folder, which an undo removes).
	PromptAlways
)

// record is an operation in the history.
type record struct {
	label   string
	prompt  UndoPrompt
	actions []*action
}

// FileOps does the file operations of the Explorer and keeps their history, which Undo and Redo
// walk (SPEC §10.5). The methods do I/O and take a context; they are safe for concurrent use but run
// one at a time.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (the actions),
// src/vs/workbench/contrib/files/browser/fileImportExport.ts and src/vs/workbench/contrib/files/browser/explorerService.ts
// (applyBulkEdit and the undo history of the Explorer)
type FileOps struct {
	opts FileOpsOptions

	run sync.Mutex // one operation at a time
	mu  sync.Mutex // guards the history
	// undo and redo are the history, the last operation at the end.
	undo, redo []*record
}

// NewFileOps makes the file operations over a trash.
func NewFileOps(opts FileOpsOptions) *FileOps {
	if opts.Rename == nil {
		opts.Rename = os.Rename
	}
	return &FileOps{opts: opts}
}

// CanUndo tells whether there is an operation to undo, and CanRedo whether there is one to redo.
func (o *FileOps) CanUndo() bool { o.mu.Lock(); defer o.mu.Unlock(); return len(o.undo) > 0 }

// CanRedo tells whether there is an undone operation to do again.
func (o *FileOps) CanRedo() bool { o.mu.Lock(); defer o.mu.Unlock(); return len(o.redo) > 0 }

// UndoLabel is the description of the operation Undo would take back, "" with none.
func (o *FileOps) UndoLabel() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if n := len(o.undo); n > 0 {
		return o.undo[n-1].label
	}
	return ""
}

// NextUndo describes the operation Undo would take back: its label and when taking it back asks first. It
// reports false when there is none.
func (o *FileOps) NextUndo() (label string, prompt UndoPrompt, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if n := len(o.undo); n > 0 {
		return o.undo[n-1].label, o.undo[n-1].prompt, true
	}
	return "", PromptNever, false
}

// RedoLabel is the description of the operation Redo would do again, "" with none.
func (o *FileOps) RedoLabel() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if n := len(o.redo); n > 0 {
		return o.redo[n-1].label
	}
	return ""
}

// ClearHistory forgets the history (the folder of the window changed).
func (o *FileOps) ClearHistory() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.undo, o.redo = nil, nil
}

// push puts an operation at the end of the history and forgets what could be redone.
func (o *FileOps) push(r *record) {
	if len(r.actions) == 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.redo = nil
	o.undo = append(o.undo, r)
	if len(o.undo) > maxUndoRecords {
		o.undo = o.undo[len(o.undo)-maxUndoRecords:]
	}
}

// checkPath makes sure a path is absolute, clean and not the root.
func checkPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return errors.New("FileOpPathInvalid", "the path must be absolute, clean and not the root", "path", path)
	}
	return nil
}

func cancelled(ctx context.Context, tag string) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr(tag, err)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// isInside tells whether path is dir or below it.
func isInside(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// newPathOf is the path of an item below a moved folder: path with the folder's old path replaced by
// its new one. It reports false when path is not the folder or below it.
func newPathOf(path string, move PathMove) (string, bool) {
	if path == move.Old {
		return move.New, true
	}
	if rest, ok := strings.CutPrefix(path, move.Old+string(filepath.Separator)); ok {
		return filepath.Join(move.New, rest), true
	}
	return "", false
}

// Follow gives the path an item has after the moves of a result, or the path itself when no move
// touched it. gone is true when the item or a folder above it was removed.
func (r OpResult) Follow(path string) (newPath string, gone bool) {
	for _, m := range r.Moves {
		if np, ok := newPathOf(path, m); ok {
			return np, false
		}
	}
	for _, p := range r.Removed {
		if isInside(path, p) {
			return path, true
		}
	}
	return path, false
}

// CreateFile makes a file in a folder. name may hold slashes ("a/b/c.txt"), which make the folders
// above the file; a name that ends in a slash makes a folder. It fails with the tags FileOpPathInvalid,
// FileOpNameInvalid, FileOpParentMissing, FileOpDestinationExists, FileOpCreateFailed and
// FileOpCancelled.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (openExplorerAndCreate)
func (o *FileOps) CreateFile(ctx context.Context, parent, name string) (OpResult, error) {
	return o.create(ctx, parent, name, false)
}

// CreateFolder makes a folder in a folder; see CreateFile.
func (o *FileOps) CreateFolder(ctx context.Context, parent, name string) (OpResult, error) {
	return o.create(ctx, parent, name, true)
}

func (o *FileOps) create(ctx context.Context, parent, name string, folder bool) (OpResult, error) {
	o.run.Lock()
	defer o.run.Unlock()
	if err := checkPath(parent); err != nil {
		return OpResult{}, err
	}
	if err := cancelled(ctx, "FileOpCancelled"); err != nil {
		return OpResult{}, err
	}
	rel, endsInSlash := CleanNewName(name)
	if endsInSlash {
		folder = true
	}
	if rel == "" || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
		return OpResult{}, errors.New("FileOpNameInvalid", "the name is not valid", "name", name)
	}
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return OpResult{}, errors.New("FileOpParentMissing", "the folder to create in is not there", "path", parent)
	}
	target := filepath.Join(parent, filepath.FromSlash(rel))
	if exists(target) {
		return OpResult{}, errors.New("FileOpDestinationExists", "an item of this name exists already", "path", target)
	}
	top := target
	for p := filepath.Dir(target); p != parent && !exists(p); p = filepath.Dir(p) {
		top = p
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return OpResult{}, errors.NewErr("FileOpCreateFailed", err).SetMetadata("path", target)
	}
	if folder {
		if err := os.Mkdir(target, 0o755); err != nil {
			return OpResult{}, errors.NewErr("FileOpCreateFailed", err).SetMetadata("path", target)
		}
	} else {
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return OpResult{}, errors.NewErr("FileOpCreateFailed", err).SetMetadata("path", target)
		}
		if err := f.Close(); err != nil {
			return OpResult{}, errors.NewErr("FileOpCreateFailed", err).SetMetadata("path", target)
		}
	}
	label := "Create " + filepath.Base(target)
	o.push(&record{label: label, prompt: PromptAlways, actions: []*action{{kind: actCreate, path: target, top: top, isDir: folder}}})
	return OpResult{Label: label, Created: []string{top}, Targets: []string{target}}, nil
}

// Rename gives an item another name in its folder. A new name with slashes moves the item below its
// folder ("sub/b.txt"). Renaming to the name it has does nothing. It fails with the tags of Move.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (renameHandler)
func (o *FileOps) Rename(ctx context.Context, path, newName string) (OpResult, error) {
	o.run.Lock()
	defer o.run.Unlock()
	if err := checkPath(path); err != nil {
		return OpResult{}, err
	}
	rel, _ := CleanNewName(newName)
	if rel == "" || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
		return OpResult{}, errors.New("FileOpNameInvalid", "the name is not valid", "name", newName)
	}
	target := filepath.Join(filepath.Dir(path), filepath.FromSlash(rel))
	if target == path {
		return OpResult{}, nil
	}
	label := "Rename " + filepath.Base(path) + " to " + rel
	return o.moveAll(ctx, label, []PathMove{{Old: path, New: target}}, false)
}

// Move moves items into a folder, keeping their names. It fails with the tags FileOpPathInvalid,
// FileOpSourceMissing, FileOpMoveSame (the item is in the folder already), FileOpMoveIntoItself (a
// folder into itself or below itself), FileOpDestinationExists (see MoveOptions), FileOpMoveFailed and
// FileOpCancelled. The items moved before a failure stay moved, and can be undone.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerViewer.ts
// (doHandleExplorerDropOnMove) and fileActions.ts (pasteFileHandler)
func (o *FileOps) Move(ctx context.Context, dstDir string, srcs []string, opts MoveOptions) (OpResult, error) {
	o.run.Lock()
	defer o.run.Unlock()
	if err := checkPath(dstDir); err != nil {
		return OpResult{}, err
	}
	moves := make([]PathMove, 0, len(srcs))
	for _, src := range srcs {
		if err := checkPath(src); err != nil {
			return OpResult{}, err
		}
		moves = append(moves, PathMove{Old: src, New: filepath.Join(dstDir, filepath.Base(src))})
	}
	label := "Move " + describeSources(srcs)
	return o.moveAll(ctx, label, moves, opts.Overwrite)
}

// describeSources names the items of an operation for its label: the name of one, or the number of
// several.
func describeSources(srcs []string) string {
	switch len(srcs) {
	case 0:
		return "nothing"
	case 1:
		return filepath.Base(srcs[0])
	}
	return strconv.Itoa(len(srcs)) + " files"
}

// moveAll moves each item to its new path and records the operation. The caller holds run.
func (o *FileOps) moveAll(ctx context.Context, label string, moves []PathMove, overwrite bool) (OpResult, error) {
	rec := &record{label: label, prompt: PromptVerbose}
	res := OpResult{Label: label}
	var firstErr error
	for _, m := range moves {
		replaced, err := o.moveItem(ctx, m.Old, m.New, overwrite)
		if err != nil {
			firstErr = err
			break
		}
		rec.actions = append(rec.actions, &action{kind: actMove, path: m.New, from: m.Old, replaced: replaced})
		res.Moves = append(res.Moves, m)
		if replaced != nil {
			res.Removed = append(res.Removed, m.New)
		}
		res.Targets = append(res.Targets, m.New)
	}
	o.push(rec)
	return res, firstErr
}

// moveItem moves one item. It returns the item it replaced, which went to the trash.
func (o *FileOps) moveItem(ctx context.Context, src, dst string, overwrite bool) (*fileio.TrashedItem, error) {
	if err := cancelled(ctx, "FileOpCancelled"); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(src); err != nil {
		return nil, errors.NewErr("FileOpSourceMissing", err).SetMetadata("path", src)
	}
	if src == dst {
		return nil, errors.New("FileOpMoveSame", "the item is in this folder already", "path", src)
	}
	if isInside(dst, src) {
		return nil, errors.New("FileOpMoveIntoItself", "a folder cannot be moved into itself", "path", src, "target", dst)
	}
	var replaced *fileio.TrashedItem
	if exists(dst) {
		if !overwrite {
			return nil, errors.New("FileOpDestinationExists", "an item of this name exists in the destination", "path", dst)
		}
		var err error
		if replaced, err = o.removeForReplace(ctx, dst); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, errors.NewErr("FileOpMoveFailed", err).SetMetadata("path", dst)
	}
	if err := o.rename(ctx, src, dst); err != nil {
		if replaced != nil {
			// put back what was in the way
			if rerr := o.opts.Trash.Restore(ctx, *replaced); rerr != nil {
				return nil, rerr
			}
		}
		return nil, err
	}
	return replaced, nil
}

// removeForReplace clears the way for an item that overwrites another: the old one goes to the trash,
// or is deleted when there is no trash or it cannot take it.
func (o *FileOps) removeForReplace(ctx context.Context, path string) (*fileio.TrashedItem, error) {
	if o.opts.Trash != nil {
		if item, err := o.opts.Trash.Trash(ctx, path); err == nil {
			return &item, nil
		}
	}
	if err := fileio.Delete(ctx, path); err != nil {
		return nil, err
	}
	return nil, nil
}

// rename moves an item, by copying and deleting when it is on another file system.
func (o *FileOps) rename(ctx context.Context, src, dst string) error {
	err := o.opts.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return errors.NewErr("FileOpMoveFailed", err).SetMetadata("path", src, "target", dst)
	}
	if err := copyTree(ctx, src, dst, 0); err != nil {
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return errors.NewErr("FileOpMoveFailed", err).SetMetadata("path", src)
	}
	return nil
}

func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

// Conflicts lists the sources whose names are taken in a folder, for the confirmation a move or an
// import asks before it overwrites.
func (o *FileOps) Conflicts(dstDir string, srcs []string) []string {
	var out []string
	for _, src := range srcs {
		if exists(filepath.Join(dstDir, filepath.Base(src))) {
			out = append(out, src)
		}
	}
	return out
}

// Copy copies items into a folder. A copy whose name is taken gets the next free name
// (CopyOptions.Naming); a folder is copied with everything in it, links as links. It fails with the
// tags FileOpPathInvalid, FileOpSourceMissing, FileOpCopyIntoItself, FileOpCopySame, FileOpDestinationExists,
// FileOpCopyUnsupported (a socket or a device), FileOpCopyTooDeep, FileOpCopyFailed and FileOpCancelled.
// The items copied before a failure stay, and can be undone.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (findValidPasteFileTarget,
// pasteFileHandler)
func (o *FileOps) Copy(ctx context.Context, dstDir string, srcs []string, opts CopyOptions) (OpResult, error) {
	o.run.Lock()
	defer o.run.Unlock()
	if err := checkPath(dstDir); err != nil {
		return OpResult{}, err
	}
	label := "Paste " + describeSources(srcs)
	rec := &record{label: label, prompt: PromptDefault}
	res := OpResult{Label: label}
	var firstErr error
	for _, src := range srcs {
		a, err := o.copyItem(ctx, src, dstDir, opts)
		if err != nil {
			firstErr = err
			break
		}
		rec.actions = append(rec.actions, a)
		res.Created = append(res.Created, a.path)
		res.Targets = append(res.Targets, a.path)
		if a.replaced != nil {
			res.Removed = append(res.Removed, a.path)
		}
	}
	o.push(rec)
	return res, firstErr
}

func (o *FileOps) copyItem(ctx context.Context, src, dstDir string, opts CopyOptions) (*action, error) {
	if err := checkPath(src); err != nil {
		return nil, err
	}
	if err := cancelled(ctx, "FileOpCancelled"); err != nil {
		return nil, err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return nil, errors.NewErr("FileOpSourceMissing", err).SetMetadata("path", src)
	}
	isDir := info.IsDir()
	if info.Mode()&os.ModeSymlink != 0 {
		if st, serr := os.Stat(src); serr == nil {
			isDir = st.IsDir()
		}
	}
	if info.IsDir() && isInside(dstDir, src) {
		return nil, errors.New("FileOpCopyIntoItself", "a folder cannot be copied into itself", "path", src, "target", dstDir)
	}
	naming := opts.Naming
	if naming == "" {
		naming = NamingSimple
	}
	name := filepath.Base(src)
	dst := filepath.Join(dstDir, name)
	var replaced *fileio.TrashedItem
	if naming == NamingDisabled {
		if dst == src {
			return nil, errors.New("FileOpCopySame", "an item cannot be copied over itself", "path", src)
		}
		if exists(dst) {
			if !opts.Overwrite {
				return nil, errors.New("FileOpDestinationExists", "an item of this name exists in the destination", "path", dst)
			}
			if isInside(src, dst) {
				return nil, errors.New("FileOpCopyIntoItself", "a folder cannot replace the folder it is copied from", "path", dst)
			}
			if replaced, err = o.removeForReplace(ctx, dst); err != nil {
				return nil, err
			}
		}
	} else {
		for i := 0; exists(dst); i++ {
			if i > 100000 {
				return nil, errors.New("FileOpDestinationExists", "no free name was found for the copy", "path", dst)
			}
			name = IncrementFileName(name, isDir, naming)
			dst = filepath.Join(dstDir, name)
		}
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", dstDir)
	}
	if err := copyTree(ctx, src, dst, 0); err != nil {
		if replaced != nil {
			if rerr := o.opts.Trash.Restore(ctx, *replaced); rerr != nil {
				return nil, rerr
			}
		}
		return nil, err
	}
	return &action{kind: actCopy, path: dst, from: src, replaced: replaced}, nil
}

// copyTree copies a file, a link or a folder to a path that is free. A copy that fails is removed.
func copyTree(ctx context.Context, src, dst string, depth int) error {
	if depth > maxCopyDepth {
		return errors.New("FileOpCopyTooDeep", "the folders are nested too deep to copy", "path", src)
	}
	if err := cancelled(ctx, "FileOpCancelled"); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return errors.NewErr("FileOpSourceMissing", err).SetMetadata("path", src)
	}
	switch mode := info.Mode(); {
	case mode&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", src)
		}
		if err := os.Symlink(target, dst); err != nil {
			return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", dst)
		}
		return nil
	case mode.IsDir():
		if err := copyDir(ctx, src, dst, info, depth); err != nil {
			_ = os.RemoveAll(dst)
			return err
		}
		return nil
	case mode.IsRegular():
		if err := copyFile(ctx, src, dst, info); err != nil {
			_ = os.Remove(dst)
			return err
		}
		return nil
	}
	return errors.New("FileOpCopyUnsupported", "only files, folders and links can be copied", "path", src)
}

func copyDir(ctx context.Context, src, dst string, info fs.FileInfo, depth int) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", src)
	}
	// the folder is writable while it is filled, and gets its own mode after
	if err := os.Mkdir(dst, 0o700); err != nil {
		return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", dst)
	}
	for _, e := range entries {
		if err := copyTree(ctx, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), depth+1); err != nil {
			return err
		}
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", dst)
	}
	return nil
}

// ctxReader stops a copy when its context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func copyFile(ctx context.Context, src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", src)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", dst)
	}
	if _, err := io.Copy(out, ctxReader{ctx: ctx, r: in}); err != nil {
		_ = out.Close()
		if ctx.Err() != nil {
			return errors.NewErr("FileOpCancelled", err).SetMetadata("path", src)
		}
		return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", dst)
	}
	if err := out.Close(); err != nil {
		return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", dst)
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return errors.NewErr("FileOpCopyFailed", err).SetMetadata("path", dst)
	}
	return nil
}

// Trash moves items to the trash. An item that cannot be trashed (it is on another file system than
// the trash) fails with the tags of fileio.HomeTrash.Trash, and the caller offers to delete it for
// good; without a trash it fails with FileOpTrashUnavailable. Items trashed before a failure stay
// trashed, and can be undone.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (deleteFiles)
func (o *FileOps) Trash(ctx context.Context, paths []string) (OpResult, error) {
	o.run.Lock()
	defer o.run.Unlock()
	if o.opts.Trash == nil {
		return OpResult{}, errors.New("FileOpTrashUnavailable", "there is no trash to move items to")
	}
	label := "Delete " + describeSources(paths)
	rec := &record{label: label}
	res := OpResult{Label: label}
	var firstErr error
	for _, p := range paths {
		if err := checkPath(p); err != nil {
			firstErr = err
			break
		}
		item, err := o.opts.Trash.Trash(ctx, p)
		if err != nil {
			firstErr = err
			break
		}
		it := item
		rec.actions = append(rec.actions, &action{kind: actTrash, path: p, trashed: &it})
		res.Removed = append(res.Removed, p)
	}
	o.push(rec)
	return res, firstErr
}

// Delete removes items for good. It cannot be undone. It fails with the tags of fileio.Delete.
func (o *FileOps) Delete(ctx context.Context, paths []string) (OpResult, error) {
	o.run.Lock()
	defer o.run.Unlock()
	res := OpResult{Label: "Delete " + describeSources(paths)}
	for _, p := range paths {
		if err := fileio.Delete(ctx, p); err != nil {
			return res, err
		}
		res.Removed = append(res.Removed, p)
	}
	return res, nil
}

// Undo takes back the last operation: a created item is trashed, a moved one goes back, a copy is
// trashed, a trashed item is restored (SPEC §10.5). It fails with FileOpNothingToUndo when there is
// none, and with the tags of the steps (FileOpDestinationExists when something is at the old path
// now); the operation stays in the history when it fails.
func (o *FileOps) Undo(ctx context.Context) (OpResult, error) {
	o.run.Lock()
	defer o.run.Unlock()
	o.mu.Lock()
	if len(o.undo) == 0 {
		o.mu.Unlock()
		return OpResult{}, errors.New("FileOpNothingToUndo", "there is no file operation to undo")
	}
	rec := o.undo[len(o.undo)-1]
	o.mu.Unlock()
	res := OpResult{Label: "Undo " + rec.label}
	for i := len(rec.actions) - 1; i >= 0; i-- {
		r, err := o.undoAction(ctx, rec.actions[i])
		res.merge(r)
		if err != nil {
			return res, err
		}
	}
	o.mu.Lock()
	o.undo = o.undo[:len(o.undo)-1]
	o.redo = append(o.redo, rec)
	o.mu.Unlock()
	return res, nil
}

// Redo does the last undone operation again; see Undo.
func (o *FileOps) Redo(ctx context.Context) (OpResult, error) {
	o.run.Lock()
	defer o.run.Unlock()
	o.mu.Lock()
	if len(o.redo) == 0 {
		o.mu.Unlock()
		return OpResult{}, errors.New("FileOpNothingToRedo", "there is no file operation to redo")
	}
	rec := o.redo[len(o.redo)-1]
	o.mu.Unlock()
	res := OpResult{Label: "Redo " + rec.label}
	for _, a := range rec.actions {
		r, err := o.redoAction(ctx, a)
		res.merge(r)
		if err != nil {
			return res, err
		}
	}
	o.mu.Lock()
	o.redo = o.redo[:len(o.redo)-1]
	o.undo = append(o.undo, rec)
	o.mu.Unlock()
	return res, nil
}

// removeCreated takes away an item that an operation made: to the trash when there is one, else for good.
func (o *FileOps) removeCreated(ctx context.Context, path string) (*fileio.TrashedItem, error) {
	if o.opts.Trash != nil {
		if item, err := o.opts.Trash.Trash(ctx, path); err == nil {
			return &item, nil
		}
	}
	if err := fileio.Delete(ctx, path); err != nil {
		return nil, err
	}
	return nil, nil
}

func (o *FileOps) undoAction(ctx context.Context, a *action) (OpResult, error) {
	switch a.kind {
	case actCreate:
		item, err := o.removeCreated(ctx, a.top)
		if err != nil {
			return OpResult{}, err
		}
		a.trashed = item
		return OpResult{Removed: []string{a.top}}, nil
	case actCopy:
		item, err := o.removeCreated(ctx, a.path)
		if err != nil {
			return OpResult{}, err
		}
		a.trashed = item
		res := OpResult{Removed: []string{a.path}}
		if a.replaced != nil {
			if err := o.opts.Trash.Restore(ctx, *a.replaced); err != nil {
				return res, err
			}
			res.Created = append(res.Created, a.path)
		}
		return res, nil
	case actMove:
		if exists(a.from) {
			return OpResult{}, errors.New("FileOpDestinationExists", "something is at the old path now", "path", a.from)
		}
		if err := o.rename(ctx, a.path, a.from); err != nil {
			return OpResult{}, err
		}
		res := OpResult{Moves: []PathMove{{Old: a.path, New: a.from}}, Targets: []string{a.from}}
		if a.replaced != nil {
			if err := o.opts.Trash.Restore(ctx, *a.replaced); err != nil {
				return res, err
			}
			res.Created = append(res.Created, a.path)
		}
		return res, nil
	case actTrash:
		if o.opts.Trash == nil {
			return OpResult{}, errors.New("FileOpTrashUnavailable", "there is no trash to restore from")
		}
		if err := o.opts.Trash.Restore(ctx, *a.trashed); err != nil {
			return OpResult{}, err
		}
		return OpResult{Created: []string{a.path}, Targets: []string{a.path}}, nil
	}
	return OpResult{}, nil
}

func (o *FileOps) redoAction(ctx context.Context, a *action) (OpResult, error) {
	switch a.kind {
	case actCreate, actCopy:
		path := a.path
		if a.kind == actCreate {
			path = a.top
		}
		var res OpResult
		if a.kind == actCopy && a.replaced != nil {
			// the undo restored the item the copy had replaced: it goes to the trash again
			replaced, err := o.removeForReplace(ctx, a.path)
			if err != nil {
				return res, err
			}
			a.replaced = replaced
		} else if exists(path) {
			return res, errors.New("FileOpDestinationExists", "something is at the path now", "path", path)
		}
		switch {
		case a.trashed != nil && o.opts.Trash != nil:
			if err := o.opts.Trash.Restore(ctx, *a.trashed); err != nil {
				return res, err
			}
		case a.kind == actCopy:
			if err := copyTree(ctx, a.from, a.path, 0); err != nil {
				return res, err
			}
		default:
			if err := recreate(a.path, a.isDir); err != nil {
				return res, err
			}
		}
		a.trashed = nil
		return OpResult{Created: []string{path}, Targets: []string{a.path}}, nil
	case actMove:
		replaced, err := o.moveItem(ctx, a.from, a.path, a.replaced != nil)
		if err != nil {
			return OpResult{}, err
		}
		a.replaced = replaced
		res := OpResult{Moves: []PathMove{{Old: a.from, New: a.path}}, Targets: []string{a.path}}
		if replaced != nil {
			res.Removed = append(res.Removed, a.path)
		}
		return res, nil
	case actTrash:
		if o.opts.Trash == nil {
			return OpResult{}, errors.New("FileOpTrashUnavailable", "there is no trash to move items to")
		}
		item, err := o.opts.Trash.Trash(ctx, a.path)
		if err != nil {
			return OpResult{}, err
		}
		a.trashed = &item
		return OpResult{Removed: []string{a.path}}, nil
	}
	return OpResult{}, nil
}

// recreate makes an item again that was deleted for good (there was no trash): an empty file or an empty
// folder, with the folders above it.
func recreate(path string, isDir bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.NewErr("FileOpCreateFailed", err).SetMetadata("path", path)
	}
	if isDir {
		if err := os.Mkdir(path, 0o755); err != nil {
			return errors.NewErr("FileOpCreateFailed", err).SetMetadata("path", path)
		}
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return errors.NewErr("FileOpCreateFailed", err).SetMetadata("path", path)
	}
	if err := f.Close(); err != nil {
		return errors.NewErr("FileOpCreateFailed", err).SetMetadata("path", path)
	}
	return nil
}
