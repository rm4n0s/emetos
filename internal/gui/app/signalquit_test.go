package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/storage"
)

// The signal that ends the program (SIGINT, SIGTERM) is a quit without questions (SPEC §10.3, DECISIONS-TO-REVIEW 2026-10-31):
// the backups of the unsaved documents are written, the state is written and the application ends.

// backupsOf are the backups on the disk of the window of a folder, by URI.
func (e *multiEnv) backupsOf(folder string) map[string]storage.Backup {
	e.t.Helper()
	id, err := storage.WindowID(folder)
	if err != nil {
		e.t.Fatal(err)
	}
	store, err := storage.NewBackupStore(e.paths, id, storage.BackupStoreOptions{})
	if err != nil {
		e.t.Fatal(err)
	}
	list, problems := store.List(context.Background())
	if len(problems) != 0 {
		e.t.Fatalf("backup problems: %v", problems)
	}
	out := map[string]storage.Backup{}
	for _, b := range list {
		out[b.URI] = b
	}
	return out
}

func TestTheSignalQuitBacksUpEveryTabAndEndsWithoutAsking(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"files.hotExit": "onExit"})
	sig, signal := context.WithCancel(context.Background())
	defer signal()
	env.signal = sig
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	a.edit(a.workspaceOf(one), filepath.Join(one, "a.go"))
	a.edit(a.workspaceOf(two), filepath.Join(two, "b.go"))
	signal()
	a.waitFor("the exit", func() bool { a.ts.FlushPosted(); return a.ts.ExitRequests() == 1 })
	for _, folder := range []string{one, two} {
		if got := env.backupsOf(folder); len(got) != 1 {
			t.Errorf("backups of %s: %v", folder, got)
		}
	}
	for _, w := range a.m.All() {
		if got := a.dialog(w); got != "" {
			t.Errorf("the window of %s asks %q", w.Folder, got)
		}
	}
	// the files themselves are as they were: the backups are what the next start restores
	if data, err := os.ReadFile(filepath.Join(one, "a.go")); err != nil || strings.HasPrefix(string(data), "x") {
		t.Errorf("a.go: %q %v", data, err)
	}
}

func TestABackupThatFailsOnTheSignalIsLoggedAndTheQuitEnds(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"files.hotExit": "onExit"})
	sig, signal := context.WithCancel(context.Background())
	defer signal()
	env.signal = sig
	logs := &syncBuffer{}
	var fail atomic.Bool
	one := env.folder("one", "a.go")
	a := env.runWith(Options{Folder: one}, ProcessOptions{
		Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
		BackupWriteFile: func(path string, data []byte) error {
			if fail.Load() && strings.HasSuffix(path, ".json") {
				return os.ErrPermission
			}
			return storage.WriteFileAtomic(path, data)
		},
	})
	a.edit(a.workspaceOf(one), filepath.Join(one, "a.go"))
	fail.Store(true)
	signal()
	a.waitFor("the exit", func() bool { a.ts.FlushPosted(); return a.ts.ExitRequests() == 1 })
	if got := logs.warnLines("cannot be backed up"); len(got) != 1 || !strings.Contains(got[0], "permission denied") {
		t.Errorf("the log says %q", got)
	}
	if a.dialog(a.workspaceOf(one)) != "" {
		t.Error("the window asked although the program ends")
	}
}

func TestRunContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		// run says what happens to ctx and to the run after the context is made
		signal, end bool
		wantEnded   bool
		wantWarning bool
	}{
		{"no signal and the run goes on", false, false, false, false},
		{"the signal and no quit in time ends the run", true, false, true, true},
		{"the run that ends by itself after the signal is not ended again", true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, signal := context.WithCancel(context.Background())
			defer signal()
			logs := &syncBuffer{}
			grace := 50 * time.Millisecond
			runCtx, end := runContext(ctx, grace, slog.New(slog.NewJSONHandler(logs, nil)))
			defer end()
			if tc.signal {
				signal()
				if runCtx.Err() != nil {
					t.Fatal("the run ended at once: the quit has no time")
				}
			}
			if tc.end {
				end()
			}
			select {
			case <-runCtx.Done():
				if !tc.wantEnded {
					t.Error("the run ended")
				}
			case <-time.After(4 * grace):
				if tc.wantEnded {
					t.Error("the run did not end")
				}
			}
			time.Sleep(2 * grace)
			if got := logs.warnLines("did not end"); (len(got) != 0) != tc.wantWarning {
				t.Errorf("log: %q", got)
			}
		})
	}
}
