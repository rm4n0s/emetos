package storage

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// DefaultStateDelay is how long after a change a state file is written (SPEC §10.10).
const DefaultStateDelay = time.Second

// AfterFunc calls f in a goroutine of its own once d has passed and returns the function that cancels the call,
// which reports whether it did (time.AfterFunc is one). Tests give the store a clock they move by hand.
type AfterFunc func(d time.Duration, f func()) (stop func() bool)

// realAfter is AfterFunc over the system clock.
func realAfter(d time.Duration, f func()) func() bool {
	t := time.AfterFunc(d, f)
	return t.Stop
}

// WriteFileFunc replaces the file at path by data, completely or not at all. WriteFileAtomic is the
// function the store uses; tests give one that fails.
type WriteFileFunc func(path string, data []byte) error

// WriteFileAtomic writes data to a temporary file in the folder of path, flushes it to disk and renames it
// over path, so a reader sees the old file or the new one and a failure leaves the old one. The folder is made
// when it is not there (permissions 0700) and the file has the permissions 0600: a state file holds the paths
// the user works on. The error is the cause, not tagged: the writer that calls it tags it.
func WriteFileAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// writer writes one state file, debounced: Set keeps the latest bytes and has them written once the delay has
// passed since the first change that was not written yet, so a burst of changes is one write and a file is
// never more than the delay behind. Flush writes at once. A write that fails is logged and tried again by the
// next change or the next Flush; it is never fatal.
type writer struct {
	path   string
	delay  time.Duration
	after  AfterFunc
	write  WriteFileFunc
	logger *slog.Logger

	// writeMu orders the writes: the one that comes last has the latest bytes.
	writeMu sync.Mutex

	mu    sync.Mutex
	data  []byte
	dirty bool
	// scheduled is whether a timer is set, gen which timer: a timer that fires with another generation than
	// the current one was cancelled by a Flush and does nothing. stop cancels the timer, once it is known.
	scheduled bool
	gen       int
	stop      func() bool
}

func newWriter(path string, o StateStoreOptions) *writer {
	return &writer{path: path, delay: o.Delay, after: o.After, write: o.WriteFile, logger: o.Logger}
}

// Set replaces the bytes to write and sets the timer when none is.
func (w *writer) Set(data []byte) {
	w.mu.Lock()
	w.data, w.dirty = data, true
	if w.scheduled {
		w.mu.Unlock()
		return
	}
	w.scheduled = true
	w.gen++
	gen := w.gen
	w.mu.Unlock()
	// the timer may run its function before After returns, and the function takes mu
	stop := w.after(w.delay, func() { w.fire(gen) })
	w.mu.Lock()
	if w.scheduled && w.gen == gen {
		w.stop = stop
	}
	w.mu.Unlock()
}

// fire is the timer: it forgets the timer and writes.
func (w *writer) fire(gen int) {
	w.mu.Lock()
	if !w.scheduled || w.gen != gen {
		w.mu.Unlock()
		return
	}
	w.scheduled, w.stop = false, nil
	w.mu.Unlock()
	if err := w.flush(); err != nil {
		w.logger.Warn("cannot write the state file; it is tried again at the next change", "err", err)
	}
}

// Flush cancels the timer and writes what is waiting. The error has the tags StateFlushCancelled (ctx is done
// already) and StateWriteFailed.
func (w *writer) Flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("StateFlushCancelled", err).SetMetadata("path", w.path)
	}
	w.mu.Lock()
	if w.scheduled {
		w.scheduled = false
		w.gen++
		if w.stop != nil {
			w.stop()
			w.stop = nil
		}
	}
	w.mu.Unlock()
	return w.flush()
}

// Pending tells whether bytes wait to be written.
func (w *writer) Pending() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dirty
}

// flush writes the bytes that wait. A failed write leaves them waiting.
func (w *writer) flush() error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	w.mu.Lock()
	if !w.dirty {
		w.mu.Unlock()
		return nil
	}
	data := w.data
	w.dirty = false
	w.mu.Unlock()
	if err := w.write(w.path, data); err != nil {
		w.mu.Lock()
		w.dirty = true
		w.mu.Unlock()
		return errors.NewErr("StateWriteFailed", err).SetMetadata("path", w.path)
	}
	return nil
}
