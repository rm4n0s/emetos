package confine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// fixture is a folder with files, links and a fifo, and a folder outside it with a secret.
type fixture struct {
	dir, outside string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside, filepath.Join(dir, "src", "deep")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, text string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "a.txt"), "hello\r\nworld\n")
	write(filepath.Join(dir, "src", "main.go"), "package main\n")
	write(filepath.Join(dir, "src", "deep", "x.txt"), "x")
	write(filepath.Join(dir, "binary.bin"), "\xff\xfe\x00bad")
	write(filepath.Join(outside, "secret"), "TOP SECRET")
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	link(outside, "out-dir")
	link(filepath.Join(outside, "secret"), "out-abs")
	link("../outside/secret", "out-rel")
	link("a.txt", "in-link")
	link("src", "in-dir-link")
	link(filepath.Join(dir, "a.txt"), "in-abs") // an absolute link, even to a file inside, is refused
	link("loop2", "loop1")
	link("loop1", "loop2")
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture{dir: dir, outside: outside}
}

// route asserts that err is an *errors.Error whose route contains want.
func route(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want ...%s)", appErr.Route(), want)
	}
}

func TestClean(t *testing.T) {
	tests := []struct {
		in, want, route string
	}{
		{"", "", ""},
		{".", "", ""},
		{"a/b", "a/b", ""},
		{"a//b/./c/", "a/b/c", ""},
		{"a b/é.txt", "a b/é.txt", ""},
		{"..", "", "Clean.ConfinePathInvalid"},
		{"../x", "", "Clean.ConfinePathInvalid"},
		{"a/../b", "", "Clean.ConfinePathInvalid"},
		{"a/b/..", "", "Clean.ConfinePathInvalid"},
		{"/etc/passwd", "", "Clean.ConfinePathInvalid"},
		{"a\x00b", "", "Clean.ConfinePathInvalid"},
		{strings.Repeat("a/", MaxPathLen), "", "Clean.ConfinePathInvalid"},
	}
	for _, tt := range tests {
		got, err := Clean(tt.in)
		if tt.route == "" {
			if err != nil || got != tt.want {
				t.Errorf("Clean(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
			continue
		}
		route(t, err, tt.route)
	}
}

func TestRead(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	t.Run("a file keeps its bytes", func(t *testing.T) {
		got, err := Read(ctx, f.dir, "a.txt", 0)
		if err != nil || got.Text != "hello\r\nworld\n" || got.Path != "a.txt" || got.Size != 13 {
			t.Fatalf("Read = %+v, %v", got, err)
		}
	})
	t.Run("a link inside the folder is followed", func(t *testing.T) {
		got, err := Read(ctx, f.dir, "in-link", 0)
		if err != nil || got.Text != "hello\r\nworld\n" {
			t.Fatalf("Read = %+v, %v", got, err)
		}
		got, err = Read(ctx, f.dir, "in-dir-link/main.go", 0)
		if err != nil || got.Text != "package main\n" {
			t.Fatalf("Read through a folder link = %+v, %v", got, err)
		}
	})
	t.Run("the folder reached through a link", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(f.dir, alias); err != nil {
			t.Fatal(err)
		}
		got, err := Read(ctx, alias, "src/main.go", 0)
		if err != nil || got.Text != "package main\n" {
			t.Fatalf("Read = %+v, %v", got, err)
		}
	})
	errs := []struct {
		name, rel string
		max       int64
		route     string
	}{
		{"dot dot", "../outside/secret", 0, "Read->Clean.ConfinePathInvalid"},
		{"dot dot inside", "src/../a.txt", 0, "Read->Clean.ConfinePathInvalid"},
		{"an absolute path", "/etc/passwd", 0, "Read->Clean.ConfinePathInvalid"},
		{"the absolute path of a file inside", "/tmp/x", 0, "Read->Clean.ConfinePathInvalid"},
		{"a link to a folder outside", "out-dir/secret", 0, "Read->statIn.ConfineOutside"},
		{"an absolute link outside", "out-abs", 0, "Read->statIn.ConfineOutside"},
		{"a relative link outside", "out-rel", 0, "Read->statIn.ConfineOutside"},
		{"an absolute link even inside", "in-abs", 0, "Read->statIn.ConfineOutside"},
		{"a link loop", "loop1", 0, "Read->statIn.ConfineSymlinkLoop"},
		{"a fifo", "fifo", 0, "Read.ConfineSpecialFile"},
		{"a folder", "src", 0, "Read.ConfineIsDirectory"},
		{"the folder", "", 0, "Read.ConfineIsDirectory"},
		{"a missing file", "nope.txt", 0, "Read->statIn.ConfineNotFound"},
		{"under a file", "a.txt/x", 0, "Read->statIn.ConfineFailed"},
		{"too large for the request", "a.txt", 5, "Read.ConfineTooLarge"},
		{"not text", "binary.bin", 0, "Read->readChecked.ConfineNotText"},
		{"no folder", "a.txt", 0, "Read->openRoot.ConfineNoFolder"},
	}
	for _, tt := range errs {
		t.Run(tt.name, func(t *testing.T) {
			dir := f.dir
			if tt.name == "no folder" {
				dir = ""
			}
			got, err := Read(ctx, dir, tt.rel, tt.max)
			if err == nil {
				t.Fatalf("Read(%q) = %+v, want an error", tt.rel, got)
			}
			route(t, err, tt.route)
		})
	}
	t.Run("a folder that is not there", func(t *testing.T) {
		_, err := Read(ctx, filepath.Join(f.dir, "missing"), "a.txt", 0)
		route(t, err, "Read->openRoot.ConfineRootFailed")
	})
	t.Run("a cancelled context", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		_, err := Read(c, f.dir, "a.txt", 0)
		route(t, err, "Read.ConfineCanceled")
	})
	t.Run("a file that cannot be opened", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root can read everything")
		}
		p := filepath.Join(f.dir, "locked.txt")
		if err := os.WriteFile(p, []byte("x"), 0o000); err != nil {
			t.Fatal(err)
		}
		_, err := Read(ctx, f.dir, "locked.txt", 0)
		route(t, err, "readChecked->openIn.ConfineFailed")
	})
	t.Run("a file that was replaced by another between the look and the open", func(t *testing.T) {
		r, err := os.OpenRoot(f.dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		other, err := r.Stat("src/main.go")
		if err != nil {
			t.Fatal(err)
		}
		_, err = readChecked(ctx, r, "a.txt", other, extension.MaxFilesReadSize)
		route(t, err, "readChecked.ConfineChanged")
		fifoInfo, err := r.Stat("fifo")
		if err != nil {
			t.Fatal(err)
		}
		_, err = readChecked(ctx, r, "fifo", fifoInfo, extension.MaxFilesReadSize)
		route(t, err, "readChecked.ConfineChanged")
	})
	t.Run("a cancelled context during the read", func(t *testing.T) {
		r, err := os.OpenRoot(f.dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		st, _ := r.Stat("a.txt")
		c, cancel := context.WithCancel(ctx)
		cancel()
		_, err = readChecked(c, r, "a.txt", st, extension.MaxFilesReadSize)
		route(t, err, "readChecked.ConfineCanceled")
	})
	t.Run("a file that grew beyond the limit while it was read", func(t *testing.T) {
		r, err := os.OpenRoot(f.dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		st, _ := r.Stat("a.txt") // 13 bytes, read with a limit of 5: what statIn said is stale
		_, err = readChecked(ctx, r, "a.txt", st, 5)
		route(t, err, "readChecked.ConfineTooLarge")
	})
	t.Run("the limit is the contract's", func(t *testing.T) {
		big := filepath.Join(f.dir, "big.txt")
		if err := os.WriteFile(big, []byte(strings.Repeat("x", extension.MaxFilesReadSize+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Read(ctx, f.dir, "big.txt", 0)
		route(t, err, "Read.ConfineTooLarge")
		_, err = Read(ctx, f.dir, "big.txt", 1<<40) // a request cannot raise the limit
		route(t, err, "Read.ConfineTooLarge")
	})
}

func TestStat(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tests := []struct {
		rel        string
		path, kind string
		symlink    bool
		size       int64
		route      string
	}{
		{"", ".", extension.EntryDir, false, -1, ""},
		{"a.txt", "a.txt", extension.EntryFile, false, 13, ""},
		{"src", "src", extension.EntryDir, false, -1, ""},
		{"src/main.go", "src/main.go", extension.EntryFile, false, 13, ""},
		{"in-link", "in-link", extension.EntryFile, true, 13, ""},
		{"in-dir-link", "in-dir-link", extension.EntryDir, true, -1, ""},
		{"fifo", "fifo", extension.EntryOther, false, -1, ""},
		{"out-dir", "", "", false, 0, "Stat->statIn.ConfineOutside"},
		{"out-abs", "", "", false, 0, "Stat->statIn.ConfineOutside"},
		{"in-abs", "", "", false, 0, "Stat->statIn.ConfineOutside"},
		{"loop1", "", "", false, 0, "Stat->statIn.ConfineSymlinkLoop"},
		{"nope", "", "", false, 0, "Stat->statIn.ConfineNotFound"},
		{"..", "", "", false, 0, "Stat->Clean.ConfinePathInvalid"},
		{"/", "", "", false, 0, "Stat->Clean.ConfinePathInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			got, err := Stat(ctx, f.dir, tt.rel)
			if tt.route != "" {
				route(t, err, tt.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Path != tt.path || got.Kind != tt.kind || got.Symlink != tt.symlink || (tt.size >= 0 && got.Size != tt.size) || got.ModTime.IsZero() {
				t.Errorf("Stat = %+v", got)
			}
		})
	}
	t.Run("a link that is the entry itself in the listing is not followed", func(t *testing.T) {
		got, err := List(ctx, f.dir, "", ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range got.Entries {
			if e.Name == "out-dir" && (e.Kind != extension.EntrySymlink || e.Symlink) {
				t.Errorf("entry = %+v", e)
			}
		}
	})
	t.Run("no folder", func(t *testing.T) {
		_, err := Stat(ctx, "", "a")
		route(t, err, "Stat->openRoot.ConfineNoFolder")
	})
	t.Run("a folder that is not there", func(t *testing.T) {
		_, err := Stat(ctx, filepath.Join(f.dir, "missing"), "a")
		route(t, err, "Stat->openRoot.ConfineRootFailed")
	})
	t.Run("a cancelled context", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		_, err := Stat(c, f.dir, "a.txt")
		route(t, err, "Stat.ConfineCanceled")
	})
}

func names(es []extension.FileEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Path
	}
	return out
}

func TestList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	all := []string{"a.txt", "binary.bin", "fifo", "in-abs", "in-dir-link", "in-link", "loop1", "loop2", "out-abs", "out-dir", "out-rel", "src"}
	tests := []struct {
		name string
		rel  string
		opts ListOptions
		want []string
		more bool
	}{
		{"the children", "", ListOptions{}, all, false},
		{"dot", ".", ListOptions{Depth: 1}, all, false},
		{"depth 2 lists a folder before what is in it", "", ListOptions{Depth: 2}, []string{"a.txt", "binary.bin", "fifo", "in-abs", "in-dir-link", "in-link", "loop1", "loop2", "out-abs", "out-dir", "out-rel", "src", "src/deep", "src/main.go"}, false},
		{"depth 3", "src", ListOptions{Depth: 3}, []string{"src/deep", "src/deep/x.txt", "src/main.go"}, false},
		{"a folder", "src", ListOptions{}, []string{"src/deep", "src/main.go"}, false},
		{"a link to a folder inside", "in-dir-link", ListOptions{}, []string{"in-dir-link/deep", "in-dir-link/main.go"}, false},
		{"a limit", "", ListOptions{Limit: 3}, all[:3], true},
		{"the next page", "", ListOptions{Offset: 3, Limit: 3}, all[3:6], true},
		{"the last page", "", ListOptions{Offset: 9, Limit: 5}, all[9:], false},
		{"exactly the entries", "", ListOptions{Limit: 12}, all, false},
		{"past the end", "", ListOptions{Offset: 100}, nil, false},
		{"a negative offset is zero", "", ListOptions{Offset: -4, Limit: 1}, all[:1], true},
		{"the depth is capped", "src", ListOptions{Depth: 1000}, []string{"src/deep", "src/deep/x.txt", "src/main.go"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := List(ctx, f.dir, tt.rel, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if g := names(got.Entries); strings.Join(g, ",") != strings.Join(tt.want, ",") || got.More != tt.more {
				t.Errorf("List = %v more=%v; want %v more=%v", g, got.More, tt.want, tt.more)
			}
		})
	}
	t.Run("the kinds", func(t *testing.T) {
		got, err := List(ctx, f.dir, "", ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]string{}
		for _, e := range got.Entries {
			kinds[e.Name] = e.Kind
		}
		want := map[string]string{"a.txt": extension.EntryFile, "src": extension.EntryDir, "fifo": extension.EntryOther, "out-dir": extension.EntrySymlink, "in-link": extension.EntrySymlink, "loop1": extension.EntrySymlink}
		for name, kind := range want {
			if kinds[name] != kind {
				t.Errorf("%s is %q, want %q", name, kinds[name], kind)
			}
		}
	})
	errs := []struct {
		name, rel, route string
	}{
		{"dot dot", "..", "List->Clean.ConfinePathInvalid"},
		{"absolute", "/", "List->Clean.ConfinePathInvalid"},
		{"a link out", "out-dir", "List->statIn.ConfineOutside"},
		{"below a link out", "out-dir/x", "List->statIn.ConfineOutside"},
		{"a loop", "loop1", "List->statIn.ConfineSymlinkLoop"},
		{"a file", "a.txt", "List.ConfineNotDirectory"},
		{"missing", "nope", "List->statIn.ConfineNotFound"},
	}
	for _, tt := range errs {
		t.Run(tt.name, func(t *testing.T) {
			_, err := List(ctx, f.dir, tt.rel, ListOptions{})
			route(t, err, tt.route)
		})
	}
	t.Run("no folder", func(t *testing.T) {
		_, err := List(ctx, "", "", ListOptions{})
		route(t, err, "List->openRoot.ConfineNoFolder")
	})
	t.Run("a cancelled context", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		_, err := List(c, f.dir, "", ListOptions{})
		route(t, err, "walker.walk.ConfineCanceled")
	})
	t.Run("a folder with too many entries", func(t *testing.T) {
		r, err := os.OpenRoot(f.dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		w := &walker{r: r, limit: 100, maxEntries: 3}
		route(t, w.walk(ctx, "", 1, true), "walker.walk.ConfineDirTooLarge")
	})
	t.Run("a folder that cannot be listed", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root can read everything")
		}
		p := filepath.Join(f.dir, "private")
		if err := os.Mkdir(p, 0o000); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(p, 0o755) }()
		got, err := List(ctx, f.dir, "", ListOptions{Depth: 2})
		if err != nil {
			t.Fatalf("a folder below the first that cannot be read is skipped: %v", err)
		}
		if !strings.Contains(strings.Join(names(got.Entries), ","), "private") {
			t.Errorf("the folder itself is listed: %v", names(got.Entries))
		}
		_, err = List(ctx, f.dir, "private", ListOptions{})
		route(t, err, "walker.walk->openIn.ConfineFailed")
	})
}

func TestContains(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(f.dir, alias); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, dir, abs, want, route string
	}{
		{"a file", f.dir, filepath.Join(f.dir, "src", "main.go"), "src/main.go", ""},
		{"the folder", f.dir, f.dir, "", ""},
		{"a file that does not exist yet", f.dir, filepath.Join(f.dir, "src", "new.go"), "src/new.go", ""},
		{"through the link of the folder", alias, filepath.Join(alias, "a.txt"), "a.txt", ""},
		{"by the real path when the folder was given through a link", alias, filepath.Join(f.dir, "a.txt"), "a.txt", ""},
		{"a link inside", f.dir, filepath.Join(f.dir, "in-link"), "in-link", ""},
		{"a file outside", f.dir, filepath.Join(f.outside, "secret"), "", "Contains.ConfineOutside"},
		{"a sibling with the folder's name as prefix", f.dir, f.dir + "-x/a", "", "Contains.ConfineOutside"},
		{"through a link out", f.dir, filepath.Join(f.dir, "out-dir", "secret"), "", "Contains->statIn.ConfineOutside"},
		{"a new file below a link out", f.dir, filepath.Join(f.dir, "out-dir", "new"), "", "Contains->statIn.ConfineOutside"},
		{"a new file in a folder that is not there", f.dir, filepath.Join(f.dir, "nope", "new"), "", "Contains->statIn.ConfineNotFound"},
		{"a relative path", f.dir, "a.txt", "", "Contains.ConfinePathInvalid"},
		{"no folder", "", "/a", "", "Contains.ConfineNoFolder"},
		{"a folder that is not there", filepath.Join(f.dir, "missing"), filepath.Join(f.dir, "missing", "a"), "", "Contains->openRoot.ConfineRootFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Contains(ctx, tt.dir, tt.abs)
			if tt.route != "" {
				route(t, err, tt.route)
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Contains = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	t.Run("a cancelled context", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		_, err := Contains(c, f.dir, f.dir)
		route(t, err, "Contains.ConfineCanceled")
	})
}

// The folder's own files can change while a list runs: a walk of a folder whose entries vanish does not fail.
func TestListSurvivesChanges(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := os.Remove(filepath.Join(f.dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	got, err := List(ctx, f.dir, "", ListOptions{Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names(got.Entries) {
		if n == "a.txt" {
			t.Errorf("a removed file is listed")
		}
	}
}

func TestCheckFile(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tests := []struct {
		rel, want, route string
	}{
		{"a.txt", filepath.Join(f.dir, "a.txt"), ""},
		{"src/main.go", filepath.Join(f.dir, "src", "main.go"), ""},
		{"in-link", filepath.Join(f.dir, "in-link"), ""},
		{"src", "", "CheckFile.ConfineIsDirectory"},
		{"fifo", "", "CheckFile.ConfineSpecialFile"},
		{"out-abs", "", "CheckFile->statIn.ConfineOutside"},
		{"loop1", "", "CheckFile->statIn.ConfineSymlinkLoop"},
		{"../outside/secret", "", "CheckFile->Clean.ConfinePathInvalid"},
		{"nope", "", "CheckFile->statIn.ConfineNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			got, err := CheckFile(ctx, f.dir, tt.rel)
			if tt.route != "" {
				route(t, err, tt.route)
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("CheckFile = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	t.Run("no folder", func(t *testing.T) {
		_, err := CheckFile(ctx, "", "a.txt")
		route(t, err, "CheckFile->openRoot.ConfineNoFolder")
	})
	t.Run("a cancelled context", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		_, err := CheckFile(c, f.dir, "a.txt")
		route(t, err, "CheckFile.ConfineCanceled")
	})
}
