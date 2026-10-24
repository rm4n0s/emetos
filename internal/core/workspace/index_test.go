package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// startIndex walks a folder to the end.
func startIndex(t testing.TB, root string, cfg *IndexConfig) *FileIndex {
	t.Helper()
	x := NewFileIndex(root, IndexOptions{Config: cfg})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		x.Close()
	})
	x.Start(ctx)
	waitComplete(t, x)
	return x
}

// waitComplete waits, with a bound, until the walk of an index is complete.
func waitComplete(t testing.TB, x *FileIndex) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !x.Snapshot().Complete {
		if time.Now().After(deadline) {
			t.Fatal("the walk did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

// waitFor polls a condition for a bounded time.
func waitUntil(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func sorted(files []string) []string {
	out := slices.Clone(files)
	slices.Sort(out)
	return out
}

func write(t testing.TB, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cfgWith(mod func(*IndexConfig)) *IndexConfig {
	c := DefaultIndexConfig()
	mod(&c)
	return &c
}

func TestIndexListing(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string // path -> content ("" for a plain file)
		cfg   *IndexConfig
		want  []string
	}{
		{"plain", map[string]string{"a.go": "", "src/b.go": "", "src/deep/c.go": ""}, nil,
			[]string{"a.go", "src/b.go", "src/deep/c.go"}},
		{"git folder is always skipped", map[string]string{"a.go": "", ".git/config": "", ".git/objects/x": ""},
			cfgWith(func(c *IndexConfig) { c.Exclude = nil }), []string{"a.go"}},
		{"files.exclude and search.exclude", map[string]string{"a.go": "", "x.tmp": "", "node_modules/m/i.js": "", "src/node_modules/j.js": "", "keep.js": ""},
			cfgWith(func(c *IndexConfig) {
				c.Exclude = glob.ExpressionFromSetting(map[string]any{"**/*.tmp": true, "**/node_modules": true})
			}), []string{"a.go", "keep.js"}},
		{"a disabled pattern excludes nothing", map[string]string{"a.tmp": ""},
			cfgWith(func(c *IndexConfig) { c.Exclude = glob.ExpressionFromSetting(map[string]any{"**/*.tmp": false}) }), []string{"a.tmp"}},
		{"root gitignore", map[string]string{".gitignore": "build/\n*.log\n", "a.go": "", "build/out.o": "", "x.log": "", "src/y.log": "", "src/build": "file named build"}, nil,
			[]string{".gitignore", "a.go", "src/build"}},
		{"nested gitignore applies below its folder", map[string]string{"a.log": "", "src/.gitignore": "*.log\n", "src/b.log": "", "src/d/c.log": "", "other/d.log": ""}, nil,
			[]string{"a.log", "other/d.log", "src/.gitignore"}},
		{"negation", map[string]string{".gitignore": "*.log\n!keep.log\n", "a.log": "", "keep.log": "", "d/keep.log": ""}, nil,
			[]string{".gitignore", "d/keep.log", "keep.log"}},
		{"an ignored folder stays ignored", map[string]string{".gitignore": "build/\n!build/keep.txt\n", "build/keep.txt": "", "build/o": ""}, nil,
			[]string{".gitignore"}},
		{".ignore overrides .gitignore", map[string]string{".gitignore": "*.txt\n", ".ignore": "!a.txt\n", "a.txt": "", "b.txt": ""}, nil,
			[]string{".gitignore", ".ignore", "a.txt"}},
		{"useIgnoreFiles off", map[string]string{".gitignore": "*.log\n", "a.log": ""},
			cfgWith(func(c *IndexConfig) { c.UseIgnoreFiles = false }), []string{".gitignore", "a.log"}},
		{"git info exclude", map[string]string{".git/info/exclude": "secret.txt\n", "secret.txt": "", "ok.txt": ""}, nil,
			[]string{"ok.txt"}},
		{"anchored pattern", map[string]string{".gitignore": "/gen\n", "gen": "", "src/gen": ""}, nil,
			[]string{".gitignore", "src/gen"}},
		{"a sibling clause", map[string]string{"a.js": "", "a.ts": "", "b.js": ""},
			cfgWith(func(c *IndexConfig) {
				c.Exclude = glob.ExpressionFromSetting(map[string]any{"**/*.js": map[string]any{"when": "$(basename).ts"}})
			}), []string{"a.ts", "b.js"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for p, text := range tc.files {
				if text == "" {
					text = "x"
				}
				if filepath.Base(p) == ".gitignore" || filepath.Base(p) == ".ignore" || filepath.Base(p) == "exclude" {
					text = tc.files[p]
				}
				write(t, filepath.Join(root, filepath.FromSlash(p)), text)
			}
			x := startIndex(t, root, tc.cfg)
			if got := sorted(x.Snapshot().Files); !slices.Equal(got, tc.want) {
				t.Errorf("files %q, want %q", got, tc.want)
			}
		})
	}
}

// readLogFS counts the folders that are read.
type readLogFS struct {
	FS
	mu   sync.Mutex
	read []string
}

func (c *readLogFS) ReadDir(path string) ([]fs.DirEntry, error) {
	c.mu.Lock()
	c.read = append(c.read, path)
	c.mu.Unlock()
	return c.FS.ReadDir(path)
}

func TestIndexDoesNotEnterExcludedFolders(t *testing.T) {
	root := tree(t, "src/a.go", "node_modules/pkg/i.js", "build/o", "docs/d.md")
	write(t, filepath.Join(root, ".gitignore"), "build/\n")
	cfs := &readLogFS{FS: osFS{}}
	x := NewFileIndex(root, IndexOptions{FS: cfs})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.Start(ctx)
	waitComplete(t, x)
	x.Close()
	var got []string
	for _, p := range cfs.read {
		rel, _ := filepath.Rel(root, p)
		got = append(got, rel)
	}
	slices.Sort(got)
	if want := []string{".", "docs", "src"}; !slices.Equal(got, want) {
		t.Errorf("folders read %q, want %q", got, want)
	}
}

func TestIndexParentAndGlobalIgnoreFiles(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "proj")
	write(t, filepath.Join(parent, ".gitignore"), "proj/parent-ignored.txt\n*.parent\n")
	write(t, filepath.Join(root, "parent-ignored.txt"), "x")
	write(t, filepath.Join(root, "a.parent"), "x")
	write(t, filepath.Join(root, "global.txt"), "x")
	write(t, filepath.Join(root, "kept.txt"), "x")
	global := filepath.Join(t.TempDir(), "ignore")
	write(t, global, "global.txt\n")
	tests := []struct {
		name string
		mod  func(*IndexConfig)
		want []string
	}{
		{"neither", func(*IndexConfig) {}, []string{"a.parent", "global.txt", "kept.txt", "parent-ignored.txt"}},
		{"parent files", func(c *IndexConfig) { c.UseParentIgnoreFiles = true }, []string{"global.txt", "kept.txt"}},
		{"global file", func(c *IndexConfig) { c.GlobalIgnoreFile = global }, []string{"a.parent", "kept.txt", "parent-ignored.txt"}},
		{"both", func(c *IndexConfig) { c.UseParentIgnoreFiles, c.GlobalIgnoreFile = true, global }, []string{"kept.txt"}},
		{"not without useIgnoreFiles", func(c *IndexConfig) {
			c.UseIgnoreFiles, c.UseParentIgnoreFiles, c.GlobalIgnoreFile = false, true, global
		},
			[]string{"a.parent", "global.txt", "kept.txt", "parent-ignored.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := startIndex(t, root, cfgWith(tc.mod))
			if got := sorted(x.Snapshot().Files); !slices.Equal(got, tc.want) {
				t.Errorf("files %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIndexSymlinks(t *testing.T) {
	root := tree(t, "real/f.go", "top.go")
	outside := tree(t, "o/other.go")
	mustLink := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	mustLink("real", "linkdir")                      // a link to a folder
	mustLink("top.go", "linkfile.go")                // a link to a file
	mustLink("nowhere", "broken")                    // a broken link
	mustLink("..", "real/up")                        // a loop to the parent of the folder
	mustLink(".", "real/self")                       // a loop to the folder itself
	mustLink(root, "real/toroot")                    // a loop to the opened folder
	mustLink(filepath.Join(outside, "o"), "outside") // a link out of the folder
	mustLink(filepath.Dir(root), "contains")         // a link to a folder that contains the opened one
	t.Run("followed", func(t *testing.T) {
		x := startIndex(t, root, nil)
		got := sorted(x.Snapshot().Files)
		want := []string{"linkdir/f.go", "linkfile.go", "outside/other.go", "real/f.go", "top.go"}
		if !slices.Equal(got, want) {
			t.Errorf("files %q, want %q", got, want)
		}
	})
	t.Run("not followed", func(t *testing.T) {
		x := startIndex(t, root, cfgWith(func(c *IndexConfig) { c.FollowSymlinks = false }))
		got := sorted(x.Snapshot().Files)
		if want := []string{"real/f.go", "top.go"}; !slices.Equal(got, want) {
			t.Errorf("files %q, want %q", got, want)
		}
	})
}

func TestIndexUnreadableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read every folder")
	}
	root := tree(t, "ok/a.go", "locked/b.go", "top.go")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	x := startIndex(t, root, nil)
	snap := x.Snapshot()
	if got := sorted(snap.Files); !slices.Equal(got, []string{"ok/a.go", "top.go"}) {
		t.Errorf("files %q", got)
	}
	if snap.Unreadable != 1 || !snap.Complete {
		t.Errorf("unreadable %d complete %v", snap.Unreadable, snap.Complete)
	}
}

// gateFS blocks the reading of one folder until the gate opens or the test ends.
type gateFS struct {
	FS
	folder  string
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func (g *gateFS) ReadDir(path string) ([]fs.DirEntry, error) {
	if path == g.folder {
		g.once.Do(func() { close(g.entered) })
		<-g.gate
	}
	return g.FS.ReadDir(path)
}

func TestIndexCancelStopsTheWalk(t *testing.T) {
	root := tree(t, "a/a.go", "b/b.go", "c/c.go", "top.go")
	g := &gateFS{FS: osFS{}, folder: filepath.Join(root, "b"), entered: make(chan struct{}), gate: make(chan struct{})}
	x := NewFileIndex(root, IndexOptions{FS: g})
	ctx, cancel := context.WithCancel(context.Background())
	x.Start(ctx)
	<-g.entered // the walk is inside folder b
	cancel()
	close(g.gate)
	done := make(chan struct{})
	go func() { x.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the walk does not stop")
	}
	snap := x.Snapshot()
	if snap.Complete {
		t.Error("a cancelled walk is complete")
	}
	if slices.Contains(snap.Files, "c/c.go") {
		t.Errorf("the walk went on after the cancel: %q", snap.Files)
	}
}

func TestIndexSnapshotWhileWalking(t *testing.T) {
	root := t.TempDir()
	for d := range 60 {
		for f := range 20 {
			write(t, filepath.Join(root, fmt.Sprintf("d%02d", d), fmt.Sprintf("f%02d.txt", f)), "x")
		}
	}
	x := NewFileIndex(root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var grew atomic.Int32
	x.OnChange(func(e IndexEvent) {
		if e.Kind == IndexGrew {
			grew.Add(1)
		}
	})
	x.Start(ctx)
	var prev []string
	for {
		snap := x.Snapshot()
		if len(snap.Files) < len(prev) || !slices.Equal(snap.Files[:len(prev)], prev) {
			t.Fatalf("a snapshot lost files of the earlier one")
		}
		seen := map[string]bool{}
		for _, f := range snap.Files {
			if seen[f] {
				t.Fatalf("%s listed twice", f)
			}
			seen[f] = true
		}
		prev = snap.Files
		if snap.Complete {
			break
		}
		time.Sleep(50 * time.Microsecond)
	}
	x.Close()
	if len(prev) != 1200 {
		t.Errorf("%d files, want 1200", len(prev))
	}
	if grew.Load() == 0 {
		t.Error("no IndexGrew event")
	}
}

func TestIndexEvents(t *testing.T) {
	root := tree(t, "a.go")
	x := NewFileIndex(root)
	var mu sync.Mutex
	var kinds []IndexEventKind
	stop := x.OnChange(func(e IndexEvent) {
		mu.Lock()
		kinds = append(kinds, e.Kind)
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.Start(ctx)
	waitComplete(t, x)
	x.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(kinds) == 0 || kinds[len(kinds)-1] != IndexDone {
		t.Errorf("events %v, want the last to be IndexDone", kinds)
	}
	n := len(kinds)
	mu.Unlock()
	stop()
	x.SetFiles([]string{"x"}) // nobody listens any more
	mu.Lock()
	if len(kinds) != n {
		t.Errorf("a listener that was stopped heard %v", kinds[n:])
	}
}

func TestIndexApply(t *testing.T) {
	root := tree(t, "a.go", "src/b.go", ".gitignore", "build/")
	write(t, filepath.Join(root, ".gitignore"), "build/\n*.log\n")
	x := startIndex(t, root, nil)
	ctx := context.Background()
	p := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	files := func() []string { return sorted(x.Snapshot().Files) }
	check := func(t *testing.T, want ...string) {
		t.Helper()
		waitUntil(t, fmt.Sprint(want), func() bool { return slices.Equal(files(), sorted(want)) })
	}

	t.Run("create a file", func(t *testing.T) {
		write(t, p("src/new.go"), "x")
		x.Apply(ctx, []Event{{Kind: EventCreated, Path: p("src/new.go")}})
		check(t, "a.go", "src/b.go", "src/new.go", ".gitignore")
	})
	t.Run("a repeated create is not listed twice", func(t *testing.T) {
		x.Apply(ctx, []Event{{Kind: EventCreated, Path: p("src/new.go")}, {Kind: EventCreated, Path: p("src/new.go")}})
		check(t, "a.go", "src/b.go", "src/new.go", ".gitignore")
	})
	t.Run("an ignored or excluded file is not added", func(t *testing.T) {
		write(t, p("x.log"), "x")
		write(t, p("build/out.o"), "x")
		write(t, p("node_modules/m.js"), "x")
		write(t, p("src/node_modules/n.js"), "x")
		x.Apply(ctx, []Event{
			{Kind: EventCreated, Path: p("x.log")}, {Kind: EventCreated, Path: p("build/out.o")},
			{Kind: EventCreated, Path: p("node_modules/m.js")}, {Kind: EventCreated, Path: p("src/node_modules/n.js")},
			{Kind: EventCreated, Path: filepath.Join(t.TempDir(), "outside.go")},
		})
		check(t, "a.go", "src/b.go", "src/new.go", ".gitignore")
	})
	t.Run("delete a file", func(t *testing.T) {
		before := x.Snapshot()
		if err := os.Remove(p("src/new.go")); err != nil {
			t.Fatal(err)
		}
		x.Apply(ctx, []Event{{Kind: EventDeleted, Path: p("src/new.go")}})
		check(t, "a.go", "src/b.go", ".gitignore")
		if !slices.Contains(before.Files, "src/new.go") {
			t.Error("the snapshot taken before changed")
		}
	})
	t.Run("rename a file", func(t *testing.T) {
		if err := os.Rename(p("a.go"), p("c.go")); err != nil {
			t.Fatal(err)
		}
		x.Apply(ctx, []Event{{Kind: EventRenamed, OldPath: p("a.go"), Path: p("c.go")}})
		check(t, "c.go", "src/b.go", ".gitignore")
	})
	t.Run("create a folder with files", func(t *testing.T) {
		write(t, p("lib/x.go"), "x")
		write(t, p("lib/deep/y.go"), "x")
		write(t, p("lib/deep/skip.log"), "x")
		x.Apply(ctx, []Event{{Kind: EventCreated, Path: p("lib"), IsDir: true}})
		check(t, "c.go", "src/b.go", ".gitignore", "lib/x.go", "lib/deep/y.go")
	})
	t.Run("rename a folder", func(t *testing.T) {
		if err := os.Rename(p("lib"), p("pkg")); err != nil {
			t.Fatal(err)
		}
		x.Apply(ctx, []Event{{Kind: EventRenamed, OldPath: p("lib"), Path: p("pkg"), IsDir: true}})
		check(t, "c.go", "src/b.go", ".gitignore", "pkg/x.go", "pkg/deep/y.go")
	})
	t.Run("delete a folder", func(t *testing.T) {
		if err := os.RemoveAll(p("pkg")); err != nil {
			t.Fatal(err)
		}
		x.Apply(ctx, []Event{{Kind: EventDeleted, Path: p("pkg"), IsDir: true}})
		check(t, "c.go", "src/b.go", ".gitignore")
	})
	t.Run("a change of an ignore file lists the folder again", func(t *testing.T) {
		write(t, p(".gitignore"), "build/\n")
		write(t, p("x.log"), "x")
		x.Apply(ctx, []Event{{Kind: EventChanged, Path: p(".gitignore")}})
		check(t, "c.go", "src/b.go", ".gitignore", "x.log")
	})
	t.Run("a folder that is moved in with an ignore file", func(t *testing.T) {
		write(t, p("mod/.gitignore"), "*.gen\n")
		write(t, p("mod/a.gen"), "x")
		write(t, p("mod/b.txt"), "x")
		x.Apply(ctx, []Event{{Kind: EventCreated, Path: p("mod"), IsDir: true}})
		check(t, "c.go", "src/b.go", ".gitignore", "x.log", "mod/.gitignore", "mod/b.txt")
	})
	t.Run("rescan reads the folder again", func(t *testing.T) {
		write(t, p("src/late.go"), "x")
		x.Apply(ctx, []Event{{Kind: EventRescan, Path: p("src")}})
		check(t, "c.go", "src/b.go", "src/late.go", ".gitignore", "x.log", "mod/.gitignore", "mod/b.txt")
	})
	x.Close()
}

func TestIndexSetConfigRebuilds(t *testing.T) {
	root := tree(t, "a.go", "gen/g.go", "src/b.go")
	x := startIndex(t, root, nil)
	if got := sorted(x.Snapshot().Files); !slices.Equal(got, []string{"a.go", "gen/g.go", "src/b.go"}) {
		t.Fatalf("files %q", got)
	}
	cfg := x.Config()
	if x.SetConfig(cfg) {
		t.Error("the same configuration rebuilt the index")
	}
	cfg.Exclude = append(slices.Clone(cfg.Exclude), glob.Clause{Pattern: "**/gen"})
	if !x.SetConfig(cfg) {
		t.Fatal("a new configuration did not rebuild the index")
	}
	waitComplete(t, x)
	if got := sorted(x.Snapshot().Files); !slices.Equal(got, []string{"a.go", "src/b.go"}) {
		t.Errorf("files %q after the exclude", got)
	}
	x.Rebuild()
	waitComplete(t, x)
	if got := sorted(x.Snapshot().Files); !slices.Equal(got, []string{"a.go", "src/b.go"}) {
		t.Errorf("files %q after Rebuild", got)
	}
}

func TestIndexCloseLeavesNothingRunning(t *testing.T) {
	root := tree(t, "a/1", "b/2", "c/3")
	x := NewFileIndex(root)
	x.Start(context.Background())
	x.Close()
	done := make(chan struct{})
	go func() { x.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait does not return after Close")
	}
	x.Rebuild() // a closed index ignores it
	x.Apply(context.Background(), []Event{{Kind: EventCreated, Path: filepath.Join(root, "a", "1")}})
}

func TestIndexConfigFromSettings(t *testing.T) {
	store := settings.NewStore(settings.DefaultRegistry(), settings.StoreOptions{})
	got := IndexConfigFromSettings(store)
	want := DefaultIndexConfig()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultIndexConfig differs from the default settings:\n%+v\n%+v", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if p := globalIgnorePath(); p != "/xdg/git/ignore" {
		t.Errorf("global ignore path %q", p)
	}
}

func TestIndexSearchRoute(t *testing.T) {
	root := tree(t, "a.go")
	x := startIndex(t, root, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := x.Search(ctx, "a", SearchOptions{})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("FileSearchCancelled") || !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
}
