package logs

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// clock is a settable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(year int, month time.Month, day, hour, min int) *clock {
	return &clock{t: time.Date(year, month, day, hour, min, 0, 0, time.UTC)}
}
func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustOpen(t *testing.T, opts Options) *Logs {
	t.Helper()
	l, err := Open(opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return l
}

func requireTag(t *testing.T, err error, tag string, routes ...string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error with tag %s, got %T (%v)", tag, err, err)
	}
	if appErr.Tag != tag {
		t.Errorf("Tag = %q, want %q", appErr.Tag, tag)
	}
	for _, r := range append(routes, tag) {
		if !appErr.HasRoute(r) {
			t.Errorf("route lacks %q: %s", r, appErr.Route())
		}
	}
}

func TestOpenWritesFileAndChannel(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(2026, 10, 23, 10, 0)
	l := mustOpen(t, Options{Dir: dir, Now: clk.Now})

	wantPath := filepath.Join(dir, "2026-10-23", "main.log")
	if l.Path() != wantPath {
		t.Errorf("Path = %q, want %q", l.Path(), wantPath)
	}
	l.Logger().Info("hello", "n", 1)
	l.Logger().Debug("hidden")
	l.Logger().Warn("careful")

	got := read(t, wantPath)
	for _, want := range []string{"level=INFO msg=hello n=1", "level=WARN msg=careful"} {
		if !strings.Contains(got, want) {
			t.Errorf("log file lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "hidden") {
		t.Errorf("a debug line was written at the default level:\n%s", got)
	}
	lines := l.Channels().Lines(MainChannel)
	if len(lines) != 2 || !strings.Contains(lines[0], "msg=hello") || !strings.Contains(lines[1], "msg=careful") {
		t.Errorf("channel lines = %q, want the hello and careful records", lines)
	}
	if got, want := l.Channels().Names(), []string{MainChannel}; !reflect.DeepEqual(got, want) {
		t.Errorf("channels = %v, want %v", got, want)
	}

	// Private: the folder and the file are the user's alone.
	for path, want := range map[string]os.FileMode{filepath.Dir(wantPath): 0o700, wantPath: 0o600} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", path, st.Mode().Perm(), want)
		}
	}
}

func TestOpenLevelAndAppend(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(2026, 10, 23, 10, 0)
	first := mustOpen(t, Options{Dir: dir, Now: clk.Now, Level: slog.LevelDebug})
	first.Logger().Debug("first run")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := mustOpen(t, Options{Dir: dir, Now: clk.Now, Level: slog.LevelDebug})
	second.Logger().Debug("second run")

	got := read(t, second.Path())
	if !strings.Contains(got, "first run") || !strings.Contains(got, "second run") {
		t.Errorf("a second Open on the same day must append:\n%s", got)
	}
}

func TestRotationByDay(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(2026, 10, 23, 23, 59)
	l := mustOpen(t, Options{Dir: dir, Now: clk.Now})
	l.Logger().Info("before midnight")

	clk.Set(time.Date(2026, 10, 24, 0, 1, 0, 0, time.UTC))
	l.Logger().Info("after midnight")

	day1 := read(t, filepath.Join(dir, "2026-10-23", "main.log"))
	day2 := read(t, filepath.Join(dir, "2026-10-24", "main.log"))
	if !strings.Contains(day1, "before midnight") || strings.Contains(day1, "after midnight") {
		t.Errorf("the first day's log is wrong:\n%s", day1)
	}
	if !strings.Contains(day2, "after midnight") || strings.Contains(day2, "before midnight") {
		t.Errorf("the second day's log is wrong:\n%s", day2)
	}
	if want := filepath.Join(dir, "2026-10-24", "main.log"); l.Path() != want {
		t.Errorf("Path = %q, want %q", l.Path(), want)
	}
	if n := len(l.Channels().Lines(MainChannel)); n != 2 {
		t.Errorf("the channel has %d lines, want 2 (it spans days)", n)
	}
}

func dayNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestRetention(t *testing.T) {
	today := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	days := func(from, to int) []string { // days relative to today, as folder names
		var out []string
		for d := from; d <= to; d++ {
			out = append(out, today.AddDate(0, 0, d).Format(dayLayout))
		}
		return out
	}
	tests := []struct {
		name string
		keep int
		have []string // day folders that exist before Open
		want []string // names left after Open, sorted
	}{
		{"default keeps today and six days", 0, days(-10, 0), days(-6, 0)},
		{"keep one is today only", 1, days(-3, 0), days(0, 0)},
		{"keep three", 3, days(-5, 0), days(-2, 0)},
		{"a future day is not old", 3, append(days(-5, -4), days(2, 2)...), append(days(0, 0), days(2, 2)...)},
		{"nothing old", 7, days(-2, -1), days(-2, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range tt.have {
				if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			mustOpen(t, Options{Dir: dir, KeepDays: tt.keep, Now: func() time.Time { return today }})
			if got := dayNames(t, dir); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("after Open: %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetentionLeavesOtherEntriesAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "not-a-day"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2020-01-01"), []byte("a file named like a day"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "2020-01-02"), 0o700); err != nil {
		t.Fatal(err)
	}
	clk := newClock(2026, 10, 23, 10, 0)
	mustOpen(t, Options{Dir: dir, Now: clk.Now})
	want := []string{"2020-01-01", "2026-10-23", "not-a-day"}
	if got := dayNames(t, dir); !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v, want %v", got, want)
	}
}

func TestRetentionOnRotation(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(2026, 10, 23, 12, 0)
	for _, d := range []string{"2026-10-17", "2026-10-18"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	l := mustOpen(t, Options{Dir: dir, Now: clk.Now})
	// Today is 10-23: with 7 days, 10-17 is the oldest kept, so nothing is removed yet.
	if got, want := dayNames(t, dir), []string{"2026-10-17", "2026-10-18", "2026-10-23"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after Open: %v, want %v", got, want)
	}
	clk.Set(time.Date(2026, 10, 24, 0, 5, 0, 0, time.UTC))
	l.Logger().Info("new day")
	if got, want := dayNames(t, dir), []string{"2026-10-18", "2026-10-23", "2026-10-24"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after the day changed: %v, want %v", got, want)
	}
}

func TestOpenErrors(t *testing.T) {
	clk := newClock(2026, 10, 23, 10, 0)

	t.Run("no folder", func(t *testing.T) {
		_, err := Open(Options{Now: clk.Now})
		requireTag(t, err, "LogDirEmpty", "Open")
	})
	t.Run("the folder cannot be created", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Open(Options{Dir: filepath.Join(file, "logs"), Now: clk.Now})
		requireTag(t, err, "LogDirCreateFailed", "Open", "rotate")
	})
	t.Run("the file cannot be opened", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "2026-10-23", "main.log"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := Open(Options{Dir: dir, Now: clk.Now})
		requireTag(t, err, "LogFileOpenFailed", "Open", "rotate")
	})
}

func TestPruneOldDaysErrors(t *testing.T) {
	today := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	t.Run("a missing folder is not an error", func(t *testing.T) {
		if err := pruneOldDays(filepath.Join(t.TempDir(), "none"), today, 7); err != nil {
			t.Errorf("pruneOldDays: %v", err)
		}
	})
	t.Run("the folder cannot be read", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		requireTag(t, pruneOldDays(file, today, 7), "LogRetentionFailed", "pruneOldDays")
	})
	t.Run("an old folder cannot be removed", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can remove anything")
		}
		dir := t.TempDir()
		stuck := filepath.Join(dir, "2020-01-01")
		if err := os.MkdirAll(filepath.Join(stuck, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(stuck, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(stuck, 0o700) })
		removable := filepath.Join(dir, "2020-01-02")
		if err := os.MkdirAll(removable, 0o700); err != nil {
			t.Fatal(err)
		}

		requireTag(t, pruneOldDays(dir, today, 7), "LogRetentionFailed", "pruneOldDays")
		if _, err := os.Stat(removable); !os.IsNotExist(err) {
			t.Errorf("the removable old folder must go even when another fails (%v)", err)
		}

		// Open is not failed by it: the problem becomes a line in the channel.
		l := mustOpen(t, Options{Dir: dir, Now: func() time.Time { return today }})
		found := false
		for _, line := range l.Channels().Lines(MainChannel) {
			found = found || strings.Contains(line, "removing old logs failed")
		}
		if !found {
			t.Errorf("the channel lacks the retention failure: %q", l.Channels().Lines(MainChannel))
		}
	})
}

func TestRotationFailureKeepsTheOldFile(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(2026, 10, 23, 23, 59)
	l := mustOpen(t, Options{Dir: dir, Now: clk.Now})
	// The next day's folder cannot be made: a file has its name.
	if err := os.WriteFile(filepath.Join(dir, "2026-10-24"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	clk.Set(time.Date(2026, 10, 24, 0, 1, 0, 0, time.UTC))
	l.Logger().Info("still logged")

	if !strings.Contains(read(t, filepath.Join(dir, "2026-10-23", "main.log")), "still logged") {
		t.Error("the line must go to the old day's file when rotating fails")
	}
	found := false
	for _, line := range l.Channels().Lines(MainChannel) {
		found = found || strings.Contains(line, "rotating the log failed")
	}
	if !found {
		t.Errorf("the channel lacks the rotation failure: %q", l.Channels().Lines(MainChannel))
	}
}

func TestClose(t *testing.T) {
	clk := newClock(2026, 10, 23, 10, 0)

	t.Run("a closed log refuses writes", func(t *testing.T) {
		l := mustOpen(t, Options{Dir: t.TempDir(), Now: clk.Now})
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		_, err := l.w.Write([]byte("x\n"))
		requireTag(t, err, "LogFileClosed", "dayWriter.Write")
		l.Logger().Info("after close") // reaches the channel, not a file
		last := l.Channels().Lines(MainChannel)
		if len(last) != 1 || !strings.Contains(last[0], "after close") {
			t.Errorf("channel = %q, want the line logged after Close", last)
		}
		if err := l.Close(); err != nil {
			t.Errorf("a second Close = %v, want nil", err)
		}
	})
	t.Run("the file cannot be closed", func(t *testing.T) {
		l, err := Open(Options{Dir: t.TempDir(), Now: clk.Now})
		if err != nil {
			t.Fatal(err)
		}
		if err := l.w.file.Close(); err != nil { // behind the log's back
			t.Fatal(err)
		}
		requireTag(t, l.Close(), "LogFileCloseFailed", "dayWriter.close")
	})
}

func TestLoggerIsSafeForConcurrentUse(t *testing.T) {
	l := mustOpen(t, Options{Dir: t.TempDir()})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				l.Logger().Info("concurrent", "i", i)
			}
		}()
	}
	wg.Wait()
	if n := len(l.Channels().Lines(MainChannel)); n != 800 {
		t.Errorf("%d channel lines, want 800", n)
	}
	b, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 800 {
		t.Errorf("%d file lines, want 800", n)
	}
}

var _ io.Writer = channelWriter{}
