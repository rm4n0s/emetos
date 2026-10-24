package storage_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

const testWindow = "0123456789abcdef"

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newBackupStore(t *testing.T, mutate func(*storage.BackupStoreOptions)) (*storage.BackupStore, storage.Paths) {
	t.Helper()
	paths := storagetest.Isolate(t)
	opts := storage.BackupStoreOptions{Logger: quietLogger()}
	if mutate != nil {
		mutate(&opts)
	}
	s, err := storage.NewBackupStore(paths, testWindow, opts)
	if err != nil {
		t.Fatal(err)
	}
	return s, paths
}

func TestBackupLayoutAndName(t *testing.T) {
	s, paths := newBackupStore(t, nil)
	if want := filepath.Join(paths.StateHome, "emetos", "backups", testWindow); s.Dir() != want {
		t.Errorf("Dir = %q, want %q", s.Dir(), want)
	}
	name := storage.BackupName("file:///a/b.go")
	if len(name) != 64+len(".json") || !strings.HasSuffix(name, ".json") {
		t.Errorf("name = %q: want the SHA-256 in hexadecimal and .json", name)
	}
	if name == storage.BackupName("file:///a/c.go") {
		t.Error("two URIs have the same name")
	}
	ctx := context.Background()
	if err := s.Write(ctx, storage.Backup{URI: "file:///a/b.go", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(s.Dir(), name))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(s.Dir())
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("folder %v, %v: want mode 700", dirInfo, err)
	}
	// no temporary file is left behind
	entries, err := os.ReadDir(s.Dir())
	if err != nil || len(entries) != 1 {
		t.Errorf("folder holds %v (%v), want only the backup", entries, err)
	}
}

func TestBackupRoundTrip(t *testing.T) {
	stat := storage.BackupStat{ModTime: time.Date(2024, 5, 6, 7, 8, 9, 123456789, time.UTC).UnixNano(), Size: 42, Mode: 0o644}
	tests := []struct {
		name   string
		backup storage.Backup
	}{
		{"plain", storage.Backup{URI: "file:///a.txt", Content: "hello\nworld\n", Encoding: "utf8", Language: "plaintext"}},
		{"unicode", storage.Backup{URI: "file:///ü/日本.txt", Content: "héllo 😀 日本語\n"}},
		{"crlf file", storage.Backup{URI: "file:///c.txt", Content: "a\nb\n", LineEnding: fileio.CRLF, Encoding: "utf8bom", BOM: true}},
		{"nul and control", storage.Backup{URI: "file:///n.bin", Content: "a\x00b\x01\x1f\t\"\\ end"}},
		{"stat and orphaned", storage.Backup{URI: "file:///s.go", Content: "package s\n", Stat: &stat, Orphaned: true, Language: "go"}},
		{"untitled", storage.Backup{URI: "untitled:3", Content: "notes", Language: "markdown"}},
		{"empty content", storage.Backup{URI: "file:///e.txt", Content: ""}},
		{"a large text", storage.Backup{URI: "file:///big.txt", Content: strings.Repeat("0123456789abcdef\n", 1<<16)}},
	}
	s, _ := newBackupStore(t, nil)
	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Write(ctx, tc.backup); err != nil {
				t.Fatal(err)
			}
			got, err := s.Read(ctx, tc.backup.URI)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.backup
			want.Version = storage.BackupVersion
			if got.URI != want.URI || got.Content != want.Content || got.Encoding != want.Encoding || got.BOM != want.BOM ||
				got.LineEnding != want.LineEnding || got.Language != want.Language || got.Orphaned != want.Orphaned || got.Version != want.Version {
				t.Errorf("read %+v, want %+v", got, want)
			}
			if (got.Stat == nil) != (want.Stat == nil) || (got.Stat != nil && *got.Stat != *want.Stat) {
				t.Errorf("stat = %v, want %v", got.Stat, want.Stat)
			}
		})
	}
}

func TestBackupStat(t *testing.T) {
	at := time.Date(2024, 1, 2, 3, 4, 5, 6, time.UTC)
	stat := fileio.FileStat{ModTime: at, Size: 10, Mode: 0o640}
	b := storage.BackupStatOf(stat)
	if !b.Same(stat) {
		t.Error("a stat is not the same as itself")
	}
	if got := b.FileStat(); !got.ModTime.Equal(at) || got.Size != 10 || got.Mode != 0o640 {
		t.Errorf("FileStat = %+v", got)
	}
	for name, other := range map[string]fileio.FileStat{
		"another size": {ModTime: at, Size: 11},
		"another time": {ModTime: at.Add(time.Nanosecond), Size: 10},
	} {
		if b.Same(other) {
			t.Errorf("%s: reported as the same", name)
		}
	}
}

func TestBackupReplacesTheOldOneAtomically(t *testing.T) {
	s, _ := newBackupStore(t, nil)
	ctx := context.Background()
	b := storage.Backup{URI: "file:///a.txt", Content: "one"}
	if err := s.Write(ctx, b); err != nil {
		t.Fatal(err)
	}
	b.Content = "two"
	if err := s.Write(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(ctx, b.URI)
	if err != nil || got.Content != "two" {
		t.Errorf("read %q, %v", got.Content, err)
	}
	// a failed write leaves the old backup
	failing, _ := newBackupStore(t, func(o *storage.BackupStoreOptions) {
		o.WriteFile = func(string, []byte) error { return os.ErrPermission }
	})
	if err := failing.Write(ctx, b); err == nil {
		t.Fatal("the write did not fail")
	}
}

func TestBackupErrors(t *testing.T) {
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	tests := []struct {
		name  string
		run   func(t *testing.T, s *storage.BackupStore) error
		route string
		cause error
	}{
		{"too large", func(t *testing.T, s *storage.BackupStore) error {
			return s.Write(ctx, storage.Backup{URI: "file:///a", Content: strings.Repeat("x", 11)})
		}, "BackupStore.Write.BackupTooLarge", nil},
		{"no uri", func(t *testing.T, s *storage.BackupStore) error {
			return s.Write(ctx, storage.Backup{Content: "x"})
		}, "BackupStore.Write.BackupURIInvalid", nil},
		{"cancelled write", func(t *testing.T, s *storage.BackupStore) error {
			return s.Write(cancelled, storage.Backup{URI: "file:///a", Content: "x"})
		}, "BackupStore.Write.BackupWriteFailed", context.Canceled},
		{"not found", func(t *testing.T, s *storage.BackupStore) error {
			_, err := s.Read(ctx, "file:///nothing")
			return err
		}, "BackupStore.Read->readBackupFile.BackupNotFound", os.ErrNotExist},
		{"cancelled read", func(t *testing.T, s *storage.BackupStore) error {
			_, err := s.Read(cancelled, "file:///a")
			return err
		}, "BackupStore.Read.BackupReadFailed", context.Canceled},
		{"not json", func(t *testing.T, s *storage.BackupStore) error {
			writeRaw(t, s, "file:///a", "this is not json")
			_, err := s.Read(ctx, "file:///a")
			return err
		}, "BackupStore.Read->readBackupFile.BackupCorrupt", nil},
		{"another version", func(t *testing.T, s *storage.BackupStore) error {
			writeRaw(t, s, "file:///a", `{"version":99,"uri":"file:///a","content":"x"}`)
			_, err := s.Read(ctx, "file:///a")
			return err
		}, "BackupStore.Read->readBackupFile.BackupCorrupt", nil},
		{"the file of another document", func(t *testing.T, s *storage.BackupStore) error {
			writeRaw(t, s, "file:///a", `{"version":1,"uri":"file:///b","content":"x"}`)
			_, err := s.Read(ctx, "file:///a")
			return err
		}, "BackupStore.Read->readBackupFile.BackupCorrupt", nil},
		{"unreadable", func(t *testing.T, s *storage.BackupStore) error {
			// a folder where the file should be
			if err := os.MkdirAll(filepath.Join(s.Dir(), storage.BackupName("file:///a")), 0o700); err != nil {
				t.Fatal(err)
			}
			_, err := s.Read(ctx, "file:///a")
			return err
		}, "BackupStore.Read->readBackupFile.BackupReadFailed", nil},
		{"cancelled discard", func(t *testing.T, s *storage.BackupStore) error {
			return s.Discard(cancelled, "file:///a")
		}, "BackupStore.Discard.BackupDiscardFailed", context.Canceled},
		{"cancelled discard all", func(t *testing.T, s *storage.BackupStore) error {
			return s.DiscardAll(cancelled)
		}, "BackupStore.DiscardAll.BackupDiscardFailed", context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newBackupStore(t, func(o *storage.BackupStoreOptions) { o.MaxSize = 10 })
			err := tc.run(t, s)
			if err == nil {
				t.Fatal("no error")
			}
			if appErr := routeOf(t, err); !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tc.route)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Errorf("the cause is not %v: %v", tc.cause, err)
			}
		})
	}
	t.Run("write failed", func(t *testing.T) {
		s, _ := newBackupStore(t, func(o *storage.BackupStoreOptions) {
			o.WriteFile = func(string, []byte) error { return os.ErrPermission }
		})
		err := s.Write(ctx, storage.Backup{URI: "file:///a", Content: "x"})
		if appErr := routeOf(t, err); !appErr.HasRoute("BackupStore.Write.BackupWriteFailed") || !errors.Is(err, os.ErrPermission) {
			t.Errorf("unexpected failure: %v", err)
		}
	})
	t.Run("the window id", func(t *testing.T) {
		_, err := storage.NewBackupStore(storagetest.Isolate(t), "../x", storage.BackupStoreOptions{})
		if appErr := routeOf(t, err); !appErr.HasRoute("NewBackupStore.WindowIDInvalid") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
	})
}

// writeRaw puts a file with the name of the backup of uri in the folder of the store.
func writeRaw(t *testing.T, s *storage.BackupStore, uri, text string) {
	t.Helper()
	if err := os.MkdirAll(s.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), storage.BackupName(uri)), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupListDiscardAndCheck(t *testing.T) {
	ctx := context.Background()
	s, _ := newBackupStore(t, nil)
	for _, uri := range []string{"file:///c", "untitled:1", "file:///a", "file:///b"} {
		if err := s.Write(ctx, storage.Backup{URI: uri, Content: uri}); err != nil {
			t.Fatal(err)
		}
	}
	// a corrupt file, a foreign one and a leftover of a write that did not finish
	writeRaw(t, s, "file:///corrupt", "{not json")
	if err := os.WriteFile(filepath.Join(s.Dir(), "notes.txt"), []byte("keep out of the list"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), ".state-123.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	list, problems := s.List(ctx)
	var uris []string
	for _, b := range list {
		uris = append(uris, b.URI)
	}
	if want := []string{"file:///a", "file:///b", "file:///c", "untitled:1"}; !slices.Equal(uris, want) {
		t.Errorf("List = %v, want %v (sorted by URI)", uris, want)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want the corrupt file", problems)
	}
	if appErr := routeOf(t, problems[0]); appErr.Tag != "BackupCorrupt" {
		t.Errorf("problem tag = %s", appErr.Tag)
	}

	removed, err := s.Check(ctx)
	if err != nil || removed != 2 {
		t.Errorf("Check removed %d (%v), want 2 (the corrupt file and the temporary file)", removed, err)
	}
	list, problems = s.List(ctx)
	if len(list) != 4 || len(problems) != 0 {
		t.Errorf("after Check: %d backups, problems %v", len(list), problems)
	}

	if err := s.Discard(ctx, "file:///a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(ctx, "file:///a"); err != nil {
		t.Errorf("discarding a backup that is not there: %v", err)
	}
	if list, _ := s.List(ctx); len(list) != 3 {
		t.Errorf("%d backups after Discard, want 3", len(list))
	}
	if _, err := s.MarkRunning(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardAll(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx); len(list) != 0 {
		t.Errorf("%d backups after DiscardAll", len(list))
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "running")); err != nil {
		t.Errorf("DiscardAll took the running marker away: %v", err)
	}
}

func TestBackupListOfAFolderThatIsNotThere(t *testing.T) {
	s, _ := newBackupStore(t, nil)
	ctx := context.Background()
	if list, problems := s.List(ctx); len(list) != 0 || len(problems) != 0 {
		t.Errorf("List = %v, %v", list, problems)
	}
	if n, err := s.Check(ctx); n != 0 || err != nil {
		t.Errorf("Check = %d, %v", n, err)
	}
	if err := s.DiscardAll(ctx); err != nil {
		t.Error(err)
	}
	if err := s.ClearRunning(); err != nil {
		t.Error(err)
	}
}

func TestBackupRunningMarker(t *testing.T) {
	ctx := context.Background()
	s, _ := newBackupStore(t, nil)
	if was, err := s.MarkRunning(ctx); err != nil || was {
		t.Fatalf("first MarkRunning = %v, %v", was, err)
	}
	if was, err := s.MarkRunning(ctx); err != nil || !was {
		t.Fatalf("a marker left by a run that did not end must be found: %v, %v", was, err)
	}
	if err := s.ClearRunning(); err != nil {
		t.Fatal(err)
	}
	if was, err := s.MarkRunning(ctx); err != nil || was {
		t.Fatalf("after ClearRunning: %v, %v", was, err)
	}
	// the marker is not a backup
	if list, problems := s.List(ctx); len(list) != 0 || len(problems) != 0 {
		t.Errorf("List = %v, %v", list, problems)
	}

	failing, _ := newBackupStore(t, func(o *storage.BackupStoreOptions) {
		o.WriteFile = func(string, []byte) error { return os.ErrPermission }
	})
	_, err := failing.MarkRunning(ctx)
	if appErr := routeOf(t, err); !appErr.HasRoute("BackupStore.MarkRunning.BackupMarkerFailed") || !errors.Is(err, os.ErrPermission) {
		t.Errorf("unexpected failure: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.MarkRunning(cancelled)
	if appErr := routeOf(t, err); !appErr.HasRoute("BackupStore.MarkRunning.BackupMarkerFailed") {
		t.Errorf("unexpected failure: %v", err)
	}
	// a marker that is a folder cannot be removed
	dir, _ := newBackupStore(t, nil)
	if err := os.MkdirAll(filepath.Join(dir.Dir(), "running", "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = dir.ClearRunning()
	if appErr := routeOf(t, err); !appErr.HasRoute("BackupStore.ClearRunning.BackupMarkerFailed") {
		t.Errorf("unexpected failure: %v", err)
	}
}
