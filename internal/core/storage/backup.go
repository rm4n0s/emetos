package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// Backups of unsaved documents, for hot exit (SPEC §3.1 "Hot exit", §10.10). A window keeps one file for
// each document that has unsaved changes in $XDG_STATE_HOME/emetos/backups/<window id>/<SHA-256 of the URI>.json:
// the URI of the document, its text, its encoding, its byte order mark, its line ending, its language and the
// FileStat of the file it was read from. A file is replaced atomically, so a reader sees the old backup or the
// new one. The folder also holds the marker `running` of a window that is open (see BackupStore.MarkRunning).
//
// Ported from VS Code: src/vs/workbench/services/workingCopy/common/workingCopyBackupService.ts (the backup of a
// resource is named by the hash of its URI; a backup that cannot be read is dropped by the consistency check of
// init)

// BackupVersion is the version of the backup files. A file with another version is corrupt for this program.
const BackupVersion = 1

// MaxBackupSize is the largest text, in bytes, that is backed up; a document with a larger text has no backup
// (BackupTooLarge) and its unsaved changes are asked about when the window closes.
const MaxBackupSize = 100 << 20

// DefaultBackupDelay is how long after the last change of a dirty document its backup is written (SPEC §10.10).
const DefaultBackupDelay = time.Second

// runningMarker is the name of the file that says a window is open.
const runningMarker = "running"

// BackupStat is the FileStat of a file, as it is kept in a backup: the modification time (nanoseconds since the
// Unix epoch) and the size are what tells that the file changed since.
type BackupStat struct {
	ModTime int64  `json:"mtime"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode,omitempty"`
}

// BackupStatOf takes the stat of a file for a backup.
func BackupStatOf(s fileio.FileStat) BackupStat {
	return BackupStat{ModTime: s.ModTime.UnixNano(), Size: s.Size, Mode: uint32(s.Mode)}
}

// FileStat is the stat the backup keeps, as the FileStat of fileio.
func (s BackupStat) FileStat() fileio.FileStat {
	return fileio.FileStat{ModTime: time.Unix(0, s.ModTime), Size: s.Size, Mode: fs.FileMode(s.Mode)}
}

// Same tells whether the stat is the one of a file: the same size and modification time.
func (s BackupStat) Same(other fileio.FileStat) bool {
	return s.Size == other.Size && s.ModTime == other.ModTime.UnixNano()
}

// Backup is the unsaved state of a document, as a backup file keeps it.
type Backup struct {
	// Version is BackupVersion; Write sets it.
	Version int `json:"version"`
	// URI is the identity of the document ("file:///path", "untitled:3", document.URI.String()).
	URI string `json:"uri"`
	// Content is the text of the document, with "\n" line endings.
	Content string `json:"content"`
	// Encoding is the id of the encoding the document is saved in, BOM whether it has a byte order mark and
	// LineEnding its line ending; Language is its language id.
	Encoding   string            `json:"encoding,omitempty"`
	BOM        bool              `json:"bom,omitempty"`
	LineEnding fileio.LineEnding `json:"lineEnding,omitempty"`
	Language   string            `json:"language,omitempty"`
	// Stat is the stat of the file the document read or wrote last; nil for a document with no file.
	Stat *BackupStat `json:"stat,omitempty"`
	// Orphaned reports that the file of the document was gone when the backup was made.
	Orphaned bool `json:"orphaned,omitempty"`
}

// BackupName is the name of the backup file of a document, without the folder: the SHA-256 of its URI in
// hexadecimal, and ".json".
func BackupName(uri string) string {
	sum := sha256.Sum256([]byte(uri))
	return hex.EncodeToString(sum[:]) + ".json"
}

// BackupStoreOptions say how a BackupStore writes.
type BackupStoreOptions struct {
	// WriteFile replaces a file; WriteFileAtomic when nil. Tests give one that fails.
	WriteFile WriteFileFunc
	// Marshal encodes a backup; json.Marshal when nil. Tests give one that fails, since a Backup always encodes.
	Marshal func(v any) ([]byte, error)
	// Logger gets the files the consistency check and the listing leave out; slog.Default() when nil.
	Logger *slog.Logger
	// MaxSize is the largest text that is backed up; MaxBackupSize when zero. Tests lower it.
	MaxSize int
}

// BackupStore reads and writes the backups of one window (SPEC §10.10). It is safe for concurrent use, and
// every method that touches the disk takes a context.
type BackupStore struct {
	dir  string
	opts BackupStoreOptions
}

// NewBackupStore makes the store of the window with the id: the folder backups/<id> of the state folder. It
// reads and writes nothing yet. The error has the tag WindowIDInvalid.
func NewBackupStore(paths Paths, windowID string, opts BackupStoreOptions) (*BackupStore, error) {
	if !validWindowID(windowID) {
		return nil, errors.New("WindowIDInvalid", "a window id is 16 hexadecimal digits", "id", windowID)
	}
	if opts.WriteFile == nil {
		opts.WriteFile = WriteFileAtomic
	}
	if opts.Marshal == nil {
		opts.Marshal = json.Marshal
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.MaxSize <= 0 {
		opts.MaxSize = MaxBackupSize
	}
	return &BackupStore{dir: filepath.Join(paths.BackupsDir(), windowID), opts: opts}, nil
}

// Dir is the folder of the backups of the window.
func (s *BackupStore) Dir() string { return s.dir }

// MaxSize is the largest text the store backs up.
func (s *BackupStore) MaxSize() int { return s.opts.MaxSize }

func (s *BackupStore) path(uri string) string { return filepath.Join(s.dir, BackupName(uri)) }

// Write replaces the backup of the document b.URI. A text over the limit fails with the tag BackupTooLarge, an
// empty URI with BackupURIInvalid, and a write that fails with BackupWriteFailed (the old backup is left as
// it was).
func (s *BackupStore) Write(ctx context.Context, b Backup) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("BackupWriteFailed", err).SetMetadata("uri", b.URI)
	}
	if b.URI == "" {
		return errors.New("BackupURIInvalid", "a backup needs the URI of its document")
	}
	if len(b.Content) > s.opts.MaxSize {
		return errors.New("BackupTooLarge", "the text is too large to be backed up",
			"uri", b.URI, "size", len(b.Content), "limit", s.opts.MaxSize)
	}
	b.Version = BackupVersion
	data, err := s.opts.Marshal(b)
	if err != nil {
		return errors.NewErr("BackupEncodeFailed", err).SetMetadata("uri", b.URI)
	}
	if err := s.opts.WriteFile(s.path(b.URI), data); err != nil {
		return errors.NewErr("BackupWriteFailed", err).SetMetadata("uri", b.URI, "dir", s.dir)
	}
	return nil
}

// Discard removes the backup of the document with the URI. A backup that is not there is not an error. The error
// has the tag BackupDiscardFailed.
func (s *BackupStore) Discard(ctx context.Context, uri string) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("BackupDiscardFailed", err).SetMetadata("uri", uri)
	}
	if err := os.Remove(s.path(uri)); err != nil && !os.IsNotExist(err) {
		return errors.NewErr("BackupDiscardFailed", err).SetMetadata("uri", uri)
	}
	return nil
}

// DiscardAll removes every backup of the window (and the temporary files of writes that did not finish). The
// marker of a window that is open stays. The error has the tag BackupDiscardFailed (the first failure; the rest
// are still tried).
func (s *BackupStore) DiscardAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("BackupDiscardFailed", err).SetMetadata("dir", s.dir)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.NewErr("BackupDiscardFailed", err).SetMetadata("dir", s.dir)
	}
	var first error
	for _, e := range entries {
		if e.IsDir() || e.Name() == runningMarker {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil && !os.IsNotExist(err) && first == nil {
			first = errors.NewErr("BackupDiscardFailed", err).SetMetadata("file", e.Name())
		}
	}
	return first
}

// Read reads the backup of the document with the URI. The error has the tags BackupNotFound, BackupReadFailed
// and BackupCorrupt (the file is not JSON, has another version, or is not the backup of that URI).
func (s *BackupStore) Read(ctx context.Context, uri string) (Backup, error) {
	if err := ctx.Err(); err != nil {
		return Backup{}, errors.NewErr("BackupReadFailed", err).SetMetadata("uri", uri)
	}
	b, err := readBackupFile(s.path(uri))
	if err != nil {
		return Backup{}, err
	}
	if b.URI != uri {
		return Backup{}, errors.New("BackupCorrupt", "the file is the backup of another document", "uri", uri, "found", b.URI)
	}
	return b, nil
}

// readBackupFile reads and checks one backup file. The tags are BackupNotFound, BackupReadFailed and BackupCorrupt.
func readBackupFile(path string) (Backup, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Backup{}, errors.NewErr("BackupNotFound", err).SetMetadata("path", path)
		}
		return Backup{}, errors.NewErr("BackupReadFailed", err).SetMetadata("path", path)
	}
	var b Backup
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&b); err != nil {
		return Backup{}, errors.NewErr("BackupCorrupt", err).SetMetadata("path", path)
	}
	if b.Version != BackupVersion {
		return Backup{}, errors.New("BackupCorrupt", "the backup has another version than this program reads",
			"path", path, "version", b.Version, "supported", BackupVersion)
	}
	if b.URI == "" || filepath.Base(path) != BackupName(b.URI) {
		return Backup{}, errors.New("BackupCorrupt", "the name of the file is not the hash of the URI it holds", "path", path, "uri", b.URI)
	}
	return b, nil
}

// List reads the backups of the window, ordered by URI (so the order never depends on the file system). A file
// that cannot be used is left out and reported in problems (tags BackupReadFailed and BackupCorrupt); it is
// not removed here (see Check). A folder that is not there has no backups.
func (s *BackupStore) List(ctx context.Context) (backups []Backup, problems []error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if !os.IsNotExist(err) {
			problems = append(problems, errors.NewErr("BackupReadFailed", err).SetMetadata("dir", s.dir))
		}
		return nil, problems
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := ctx.Err(); err != nil {
			problems = append(problems, errors.NewErr("BackupReadFailed", err).SetMetadata("dir", s.dir))
			break
		}
		b, err := readBackupFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			problems = append(problems, err)
			continue
		}
		backups = append(backups, b)
	}
	slices.SortFunc(backups, func(a, b Backup) int { return strings.Compare(a.URI, b.URI) })
	return backups, problems
}

// HasBackups tells whether the window has a backup file (a file that may not be usable; Check and List say). A
// folder that is not there, or cannot be read, has none.
func (s *BackupStore) HasBackups() bool {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			return true
		}
	}
	return false
}

// Check is the consistency check made when a window starts: it removes the backup files of the window's folder
// that cannot be used (not JSON, another version, a name that is not the hash of the URI) and the temporary files
// of writes that did not finish, and says how many it removed; other files are left alone. What it removes is
// logged by the store's logger. The error has the tag BackupDiscardFailed.
func (s *BackupStore) Check(ctx context.Context) (removed int, err error) {
	entries, rerr := os.ReadDir(s.dir)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return 0, nil
		}
		return 0, errors.NewErr("BackupDiscardFailed", rerr).SetMetadata("dir", s.dir)
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == runningMarker {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		switch {
		case strings.HasSuffix(e.Name(), ".json"):
			_, berr := readBackupFile(path)
			if berr == nil {
				continue
			}
			s.opts.Logger.Warn("a backup cannot be used and is removed", "file", e.Name(), "err", berr)
		case strings.HasSuffix(e.Name(), ".tmp"):
			// the temporary file of a write that did not finish
		default:
			continue
		}
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			if err == nil {
				err = errors.NewErr("BackupDiscardFailed", rmErr).SetMetadata("file", e.Name())
			}
			continue
		}
		removed++
	}
	return removed, err
}

// MarkRunning writes the marker that says the window is open and tells whether it was there already: a marker
// that is found at start means the last run of the window did not end (SPEC §10.10). The error has the tag
// BackupMarkerFailed.
func (s *BackupStore) MarkRunning(ctx context.Context) (wasRunning bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, errors.NewErr("BackupMarkerFailed", err).SetMetadata("dir", s.dir)
	}
	path := filepath.Join(s.dir, runningMarker)
	if _, statErr := os.Stat(path); statErr == nil {
		wasRunning = true
	}
	if werr := s.opts.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n")); werr != nil {
		return wasRunning, errors.NewErr("BackupMarkerFailed", werr).SetMetadata("path", path)
	}
	return wasRunning, nil
}

// ClearRunning removes the marker: the window closed as it should. A marker that is not there is not an error. The
// error has the tag BackupMarkerFailed.
func (s *BackupStore) ClearRunning() error {
	if err := os.Remove(filepath.Join(s.dir, runningMarker)); err != nil && !os.IsNotExist(err) {
		return errors.NewErr("BackupMarkerFailed", err).SetMetadata("dir", s.dir)
	}
	return nil
}
