package logs

import (
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

const (
	// dayLayout names the folder of a day's log.
	dayLayout = "2006-01-02"
	// logFileName is the log file inside the folder of a day.
	logFileName = "main.log"
	// DefaultKeepDays is how many days of logs are kept when Options.KeepDays is 0.
	DefaultKeepDays = 7
)

// Options configures Open.
type Options struct {
	// Dir is the folder of the logs, storage.Paths.LogsDir(). The log of a day
	// goes to Dir/<YYYY-MM-DD>/main.log.
	Dir string
	// Level is the lowest level that is logged; the default is slog.LevelInfo.
	Level slog.Leveler
	// KeepDays is how many days of logs are kept, today included; the default is
	// DefaultKeepDays.
	KeepDays int
	// Now is the clock that chooses the day's folder; the default is time.Now.
	Now func() time.Time
}

// Logs is Emetos's logging: a slog.Logger that writes to the log file of the
// day and to the MainChannel of its Store (SPEC §13).
type Logs struct {
	logger *slog.Logger
	store  *Store
	w      *dayWriter
}

// Open starts logging into opts.Dir: it creates the folder of the current day,
// opens its log file (appending to it), and removes the folders of days older
// than opts.KeepDays. From then on the writer moves to a new folder when the
// day (by opts.Now) changes. The error tags are LogDirEmpty, LogDirCreateFailed
// and LogFileOpenFailed. A failure to remove an old folder is not an error of
// Open: it is a line in the MainChannel.
func Open(opts Options) (*Logs, error) {
	if opts.Dir == "" {
		return nil, errors.New("LogDirEmpty", "the log folder is not set")
	}
	level := opts.Level
	if level == nil {
		level = slog.LevelInfo
	}
	keep := opts.KeepDays
	if keep <= 0 {
		keep = DefaultKeepDays
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	store := NewStore(0)
	w := &dayWriter{
		dir:    opts.Dir,
		keep:   keep,
		now:    now,
		report: func(msg string) { store.Append(MainChannel, "log: "+msg) },
	}
	if err := w.rotate(now()); err != nil {
		return nil, err
	}
	// The channel is written first: a failing file must not hide a line from the
	// Output panel (io.MultiWriter stops at the first error).
	out := io.MultiWriter(channelWriter{store: store, channel: MainChannel}, w)
	return &Logs{
		logger: slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})),
		store:  store,
		w:      w,
	}, nil
}

// Logger returns the logger to use for everything Emetos logs.
func (l *Logs) Logger() *slog.Logger { return l.logger }

// Channels returns the channels of the Output panel; MainChannel holds this log.
func (l *Logs) Channels() *Store { return l.store }

// Path returns the log file that is being written.
func (l *Logs) Path() string { return l.w.currentPath() }

// Close closes the log file. Lines logged afterwards reach the channel but not a
// file. The error tag is LogFileCloseFailed.
func (l *Logs) Close() error { return l.w.close() }

// channelWriter appends the lines it is given to a channel of a Store.
type channelWriter struct {
	store   *Store
	channel string
}

func (c channelWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		c.store.Append(c.channel, line)
	}
	return len(p), nil
}

// dayWriter writes to the log file of the current day, and moves to a new file
// when the day changes.
type dayWriter struct {
	dir    string
	keep   int
	now    func() time.Time
	report func(msg string)

	mu   sync.Mutex
	day  string
	path string
	file *os.File
}

// Write writes p to today's file. When the day has changed it first moves to the
// new day's file; if that fails, it reports the failure and goes on with the old
// file. The error tag is LogFileClosed.
func (w *dayWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if now.Format(dayLayout) != w.day && w.file != nil {
		if err := w.rotate(now); err != nil {
			w.report("rotating the log failed: " + err.Error())
		}
	}
	if w.file == nil {
		return 0, errors.New("LogFileClosed", "the log file is closed")
	}
	return w.file.Write(p)
}

// rotate opens the log file of now's day, makes it the current file, closes the
// old one, and removes the folders of old days. Only the opening can fail the
// call; the others are reported. rotate needs w.mu, except when w is new.
func (w *dayWriter) rotate(now time.Time) error {
	day := now.Format(dayLayout)
	folder := filepath.Join(w.dir, day)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return errors.NewErr("LogDirCreateFailed", err).SetMetadata("dir", folder)
	}
	path := filepath.Join(folder, logFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return errors.NewErr("LogFileOpenFailed", err).SetMetadata("path", path)
	}
	old := w.file
	w.file, w.day, w.path = f, day, path
	if old != nil {
		if err := old.Close(); err != nil {
			w.report("closing the old log file failed: " + err.Error())
		}
	}
	if err := pruneOldDays(w.dir, now, w.keep); err != nil {
		w.report("removing old logs failed: " + err.Error())
	}
	return nil
}

func (w *dayWriter) currentPath() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.path
}

func (w *dayWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	f := w.file
	w.file = nil
	if err := f.Close(); err != nil {
		return errors.NewErr("LogFileCloseFailed", err).SetMetadata("path", w.path)
	}
	return nil
}

// pruneOldDays removes the day folders in dir that are older than the last keep
// days (today included). Entries whose names are not a day, and files, are left
// alone. The error tag is LogRetentionFailed; every removable folder is removed
// even when another fails.
func pruneOldDays(dir string, today time.Time, keep int) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.NewErr("LogRetentionFailed", err).SetMetadata("dir", dir)
	}
	y, m, d := today.Date()
	cutoff := time.Date(y, m, d, 0, 0, 0, 0, today.Location()).AddDate(0, 0, -(keep - 1))
	var first error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		day, perr := time.ParseInLocation(dayLayout, e.Name(), today.Location())
		if perr != nil {
			continue // not a folder of ours
		}
		if !day.Before(cutoff) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if rerr := os.RemoveAll(path); rerr != nil && first == nil {
			first = errors.NewErr("LogRetentionFailed", rerr).SetMetadata("dir", path)
		}
	}
	return first
}
