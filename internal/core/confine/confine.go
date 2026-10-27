package confine

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The limits of the package.
const (
	// MaxPathLen is the longest path a call takes.
	MaxPathLen = 4096
	// MaxDirEntries is the most entries a single folder may have for a listing; a larger folder fails with ConfineDirTooLarge
	// (the names of a folder are held in memory to sort them).
	MaxDirEntries = 100000
)

// readChunk is how much a read takes between two looks at its context.
const readChunk = 64 << 10

// Clean checks a path of a call and returns it in its clean form: "" and "." are the folder, and the rest has forward
// slashes only as separators. It fails with ConfinePathInvalid for a path that is absolute, has a ".." element, a NUL, or is
// longer than MaxPathLen. It does not look at the disk.
func Clean(rel string) (string, error) {
	switch {
	case len(rel) > MaxPathLen:
		return "", errors.New("ConfinePathInvalid", "the path is too long", "length", len(rel))
	case strings.ContainsRune(rel, 0):
		return "", errors.New("ConfinePathInvalid", "the path has a NUL", "path", rel)
	case strings.HasPrefix(rel, "/"):
		return "", errors.New("ConfinePathInvalid", "a path is relative to the folder, not absolute", "path", rel)
	case slices.Contains(strings.Split(rel, "/"), ".."):
		return "", errors.New("ConfinePathInvalid", "a path has no .. element", "path", rel)
	}
	return path.Clean("/" + rel)[1:], nil
}

// openRoot opens the folder as a root. It fails with ConfineNoFolder for an empty folder and ConfineRootFailed when it
// cannot be opened.
func openRoot(dir string) (*os.Root, error) {
	if dir == "" {
		return nil, errors.New("ConfineNoFolder", "the workspace has no folder")
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.NewErr("ConfineRootFailed", err).SetMetadata("folder", dir)
	}
	return r, nil
}

// rootPath is the path to give a Root: the clean path, with "." for the folder.
func rootPath(clean string) string {
	if clean == "" {
		return "."
	}
	return clean
}

// statIn looks at a path of the root, following links (follow) or not. It fails with ConfineOutside for a path that leaves
// the root (a link that points out, an absolute link), ConfineSymlinkLoop, ConfineNotFound, ConfinePathInvalid for a path the
// system refuses (a NUL) and ConfineFailed for anything else.
func statIn(r *os.Root, clean string, follow bool) (fs.FileInfo, error) {
	var info fs.FileInfo
	var err error
	if follow {
		info, err = r.Stat(rootPath(clean))
	} else {
		info, err = r.Lstat(rootPath(clean))
	}
	if err == nil {
		return info, nil
	}
	switch {
	case isEscape(err):
		return nil, errors.New("ConfineOutside", "the path leads out of the folder", "path", clean)
	case errors.Is(err, syscall.ELOOP):
		return nil, errors.New("ConfineSymlinkLoop", "the path goes through links that loop", "path", clean)
	case errors.Is(err, fs.ErrNotExist):
		return nil, errors.New("ConfineNotFound", "no such file or folder", "path", clean)
	case errors.Is(err, fs.ErrInvalid):
		return nil, errors.New("ConfinePathInvalid", "the system refuses the path", "path", clean)
	}
	return nil, errors.NewErr("ConfineFailed", err).SetMetadata("path", clean)
}

// openIn opens a path of the root for reading, without blocking on a fifo. The errors are those of statIn.
func openIn(r *os.Root, clean string) (*os.File, error) {
	f, err := r.OpenFile(rootPath(clean), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err == nil {
		return f, nil
	}
	switch {
	case isEscape(err):
		return nil, errors.New("ConfineOutside", "the path leads out of the folder", "path", clean)
	case errors.Is(err, syscall.ELOOP):
		return nil, errors.New("ConfineSymlinkLoop", "the path goes through links that loop", "path", clean)
	case errors.Is(err, fs.ErrNotExist):
		return nil, errors.New("ConfineNotFound", "no such file or folder", "path", clean)
	case errors.Is(err, fs.ErrInvalid):
		return nil, errors.New("ConfinePathInvalid", "the system refuses the path", "path", clean)
	}
	return nil, errors.NewErr("ConfineFailed", err).SetMetadata("path", clean)
}

// isEscape tells whether os.Root refused a path because it leaves the root. os.Root has no sentinel for it; its error says
// "path escapes from parent".
func isEscape(err error) bool {
	return err != nil && strings.Contains(err.Error(), "path escapes from parent")
}

// kindOf is the FileEntry kind of a mode.
func kindOf(m fs.FileMode) string {
	switch {
	case m.IsRegular():
		return extension.EntryFile
	case m.IsDir():
		return extension.EntryDir
	case m.Type()&fs.ModeSymlink != 0:
		return extension.EntrySymlink
	}
	return extension.EntryOther
}

// entryOf makes the entry of a path from its info.
func entryOf(clean string, info fs.FileInfo, symlink bool) extension.FileEntry {
	p := clean
	if p == "" {
		p = "."
	}
	name := path.Base(p)
	if p == "." {
		name = "."
	}
	return extension.FileEntry{Path: p, Name: name, Kind: kindOf(info.Mode()), Symlink: symlink, Size: info.Size(), ModTime: info.ModTime()}
}

// Stat describes a path of the folder. A link that stays inside the folder is followed and the entry is what it points to,
// with Symlink true. The errors are those of Clean, ConfineNoFolder, ConfineRootFailed and statIn.
func Stat(ctx context.Context, dir, rel string) (extension.FileEntry, error) {
	if err := ctx.Err(); err != nil {
		return extension.FileEntry{}, errors.NewErr("ConfineCanceled", err)
	}
	clean, err := Clean(rel)
	if err != nil {
		return extension.FileEntry{}, err
	}
	r, err := openRoot(dir)
	if err != nil {
		return extension.FileEntry{}, err
	}
	defer func() { _ = r.Close() }() // nothing was written
	lst, err := statIn(r, clean, false)
	if err != nil {
		return extension.FileEntry{}, err
	}
	if lst.Mode().Type()&fs.ModeSymlink == 0 {
		return entryOf(clean, lst, false), nil
	}
	info, err := statIn(r, clean, true)
	if err != nil {
		return extension.FileEntry{}, err
	}
	return entryOf(clean, info, true), nil
}

// ListOptions are the options of List.
type ListOptions struct {
	// Depth is how deep to go; 1 lists the children of the folder. Zero is 1.
	Depth int
	// Offset skips that many entries of the walk; Limit caps the entries returned (zero is DefaultFilesListLimit).
	Offset, Limit int
}

// List lists a folder: the entries below it to Depth, in the order of a walk that sorts each folder by name and lists a
// folder before what is in it. Links are listed and not followed. A folder below the first that cannot be read is skipped.
// It fails with ConfineNotDirectory for a path that is not a folder, ConfineDirTooLarge for a folder of more than
// MaxDirEntries entries, ConfineCanceled when ctx ends, and the errors of Stat. The walk stops as soon as it has the entries
// asked for and one more, which says More.
func List(ctx context.Context, dir, rel string, opts ListOptions) (extension.FilesListReply, error) {
	clean, err := Clean(rel)
	if err != nil {
		return extension.FilesListReply{}, err
	}
	depth := min(max(opts.Depth, 1), extension.MaxFilesListDepth)
	limit := opts.Limit
	if limit <= 0 {
		limit = extension.DefaultFilesListLimit
	}
	limit = min(limit, extension.MaxFilesListLimit)
	offset := max(opts.Offset, 0)
	r, err := openRoot(dir)
	if err != nil {
		return extension.FilesListReply{}, err
	}
	defer func() { _ = r.Close() }() // nothing was written
	info, err := statIn(r, clean, true)
	if err != nil {
		return extension.FilesListReply{}, err
	}
	if !info.IsDir() {
		return extension.FilesListReply{}, errors.New("ConfineNotDirectory", "the path is not a folder", "path", clean)
	}
	w := &walker{r: r, offset: offset, limit: limit, maxEntries: MaxDirEntries}
	if err := w.walk(ctx, clean, depth, true); err != nil && !errors.Is(err, errWalkDone) {
		return extension.FilesListReply{}, err
	}
	return extension.FilesListReply{Entries: w.entries, More: w.more}, nil
}

// errWalkDone ends a walk that has all it was asked for.
var errWalkDone = &errors.Error{Tag: "ConfineWalkDone"}

// walker is a walk of a folder.
type walker struct {
	r             *os.Root
	offset, limit int
	seen          int
	entries       []extension.FileEntry
	more          bool
	// maxEntries is MaxDirEntries; a test lowers it.
	maxEntries int
}

// walk lists the children of the folder dir (clean path) and, while depth allows, what is in the folders among them. top
// says that dir is the folder the call asked for, whose failure is the call's.
func (w *walker) walk(ctx context.Context, dir string, depth int, top bool) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("ConfineCanceled", err)
	}
	f, err := openIn(w.r, dir)
	if err != nil {
		if top {
			return err
		}
		return nil // a folder that cannot be opened is left out
	}
	defer func() { _ = f.Close() }() // nothing was written
	var infos []fs.DirEntry
	for {
		batch, rerr := f.ReadDir(1024)
		infos = append(infos, batch...)
		if len(infos) > w.maxEntries {
			return errors.New("ConfineDirTooLarge", "the folder has too many entries to list", "path", dir, "max", w.maxEntries)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if top {
				return errors.NewErr("ConfineFailed", rerr).SetMetadata("path", dir)
			}
			break
		}
		if err := ctx.Err(); err != nil {
			return errors.NewErr("ConfineCanceled", err)
		}
	}
	slices.SortFunc(infos, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, de := range infos {
		info, ierr := de.Info()
		if ierr != nil {
			continue // gone since the read
		}
		child := path.Join(dir, de.Name())
		if dir == "" {
			child = de.Name()
		}
		if w.seen >= w.offset+w.limit {
			w.more = true
			return errWalkDone
		}
		if w.seen >= w.offset {
			w.entries = append(w.entries, entryOf(child, info, false))
		}
		w.seen++
		if info.IsDir() && depth > 1 {
			if err := w.walk(ctx, child, depth-1, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// Read reads a file of the folder as text of at most maxBytes (zero is extension.MaxFilesReadSize, and no more than it is
// ever read). The file must be a regular file: a folder fails with ConfineIsDirectory, a fifo, a socket or a device with
// ConfineSpecialFile, a file larger than the limit with ConfineTooLarge, one that is not UTF-8 with ConfineNotText, one that
// was replaced while it was opened with ConfineChanged, and ctx ending with ConfineCanceled. The others are those of Stat.
func Read(ctx context.Context, dir, rel string, maxBytes int64) (extension.FilesReadReply, error) {
	if err := ctx.Err(); err != nil {
		return extension.FilesReadReply{}, errors.NewErr("ConfineCanceled", err)
	}
	clean, err := Clean(rel)
	if err != nil {
		return extension.FilesReadReply{}, err
	}
	limit := int64(extension.MaxFilesReadSize)
	if maxBytes > 0 {
		limit = min(maxBytes, limit)
	}
	r, err := openRoot(dir)
	if err != nil {
		return extension.FilesReadReply{}, err
	}
	defer func() { _ = r.Close() }() // nothing was written
	st, err := statIn(r, clean, true)
	if err != nil {
		return extension.FilesReadReply{}, err
	}
	switch {
	case st.IsDir():
		return extension.FilesReadReply{}, errors.New("ConfineIsDirectory", "the path is a folder", "path", clean)
	case !st.Mode().IsRegular():
		return extension.FilesReadReply{}, errors.New("ConfineSpecialFile", "only a regular file is read, not a fifo, a socket or a device", "path", clean)
	case st.Size() > limit:
		return extension.FilesReadReply{}, errors.New("ConfineTooLarge", "the file is larger than the limit", "path", clean, "size", st.Size(), "limit", limit)
	}
	return readChecked(ctx, r, clean, st, limit)
}

// readChecked opens the file that st described and reads it, after checking that it is still that file: a regular file, the
// same as st (ConfineChanged otherwise).
func readChecked(ctx context.Context, r *os.Root, clean string, st fs.FileInfo, limit int64) (extension.FilesReadReply, error) {
	f, err := openIn(r, clean)
	if err != nil {
		return extension.FilesReadReply{}, err
	}
	defer func() { _ = f.Close() }() // read only
	fst, err := f.Stat()
	if err != nil {
		return extension.FilesReadReply{}, errors.NewErr("ConfineFailed", err).SetMetadata("path", clean)
	}
	if !fst.Mode().IsRegular() || !os.SameFile(st, fst) {
		return extension.FilesReadReply{}, errors.New("ConfineChanged", "the file was replaced while it was opened", "path", clean)
	}
	data := make([]byte, 0, min(fst.Size(), limit)+1)
	for int64(len(data)) <= limit {
		if err := ctx.Err(); err != nil {
			return extension.FilesReadReply{}, errors.NewErr("ConfineCanceled", err)
		}
		chunk := min(readChunk, limit+1-int64(len(data)))
		buf := make([]byte, chunk)
		n, rerr := f.Read(buf)
		data = append(data, buf[:n]...)
		if rerr == io.EOF {
			break
		}
		if rerr != nil && !errors.Is(rerr, syscall.EAGAIN) {
			return extension.FilesReadReply{}, errors.NewErr("ConfineFailed", rerr).SetMetadata("path", clean)
		}
	}
	if int64(len(data)) > limit {
		return extension.FilesReadReply{}, errors.New("ConfineTooLarge", "the file grew beyond the limit while it was read", "path", clean, "limit", limit)
	}
	if !utf8.Valid(data) {
		return extension.FilesReadReply{}, errors.New("ConfineNotText", "the file is not UTF-8 text", "path", clean)
	}
	return extension.FilesReadReply{Path: rootPath(clean), Text: string(data), Size: int64(len(data)), ModTime: fst.ModTime()}, nil
}

// CheckFile checks that a path of the folder is a regular file a client may open (a link that stays inside is followed) and
// returns its absolute path, the folder joined with the clean path. The errors are those of Read before it opens the file
// (ConfineIsDirectory, ConfineSpecialFile and those of Stat). The caller opens the file afterwards, outside this package,
// so the check does not hold against a link swapped in between (SPEC §10.19 "As built (step 16.1)").
func CheckFile(ctx context.Context, dir, rel string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.NewErr("ConfineCanceled", err)
	}
	clean, err := Clean(rel)
	if err != nil {
		return "", err
	}
	r, err := openRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }() // nothing was written
	st, err := statIn(r, clean, true)
	if err != nil {
		return "", err
	}
	switch {
	case st.IsDir():
		return "", errors.New("ConfineIsDirectory", "the path is a folder", "path", clean)
	case !st.Mode().IsRegular():
		return "", errors.New("ConfineSpecialFile", "only a regular file is opened, not a fifo, a socket or a device", "path", clean)
	}
	return filepath.Join(dir, filepath.FromSlash(clean)), nil
}

// Contains checks that the absolute path (a document's file) is in the folder, as a path a call of this package could
// name, and returns it relative to the folder with forward slashes. The folder may be given through a link: the path is
// accepted when it is below the folder as written or below the folder with its links resolved. A path whose file does not
// exist yet is accepted when its folder is in. It fails with ConfinePathInvalid for a path that is not absolute, ConfineOutside
// for one that is not in the folder or leaves it through a link, and the errors of Stat.
func Contains(ctx context.Context, dir, abs string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.NewErr("ConfineCanceled", err)
	}
	if !filepath.IsAbs(abs) {
		return "", errors.New("ConfinePathInvalid", "the path is not absolute", "path", abs)
	}
	if dir == "" {
		return "", errors.New("ConfineNoFolder", "the workspace has no folder")
	}
	abs = filepath.Clean(abs)
	rel, ok := below(dir, abs)
	if !ok {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			rel, ok = below(real, abs)
		}
	}
	if !ok {
		return "", errors.New("ConfineOutside", "the file is not in the folder of the workspace", "path", abs)
	}
	r, err := openRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }() // nothing was written
	if _, err := statIn(r, rel, true); err != nil {
		if e, ok := errors.FromError(err); ok && e.Tag == "ConfineNotFound" {
			// a file that is not there yet: its folder decides
			if _, derr := statIn(r, path.Dir(rootPath(rel)), true); derr != nil {
				return "", derr
			}
			return rel, nil
		}
		return "", err
	}
	return rel, nil
}

// below returns abs relative to dir with forward slashes, and whether it is below dir (or dir itself).
func below(dir, abs string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(dir), abs)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "", true
	}
	return rel, true
}
