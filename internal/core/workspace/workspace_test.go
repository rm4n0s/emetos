package workspace

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// tree makes files and folders in a temporary folder: a name that ends in "/" is a folder.
func tree(t testing.TB, entries ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, e := range entries {
		p := filepath.Join(root, filepath.FromSlash(e))
		if strings.HasSuffix(e, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(items []*Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name()
	}
	return out
}

func open(t testing.TB, root string, cfg *Config) *Workspace {
	t.Helper()
	w, err := Open(context.Background(), root, Options{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestOpen(t *testing.T) {
	root := tree(t, "a.txt")
	w := open(t, root, nil)
	if w.Name() != filepath.Base(root) || w.Path() != root || !w.Root().IsRoot() || !w.Root().IsDir() {
		t.Errorf("Name %q Path %q", w.Name(), w.Path())
	}
	if w.Root().Resolved() {
		t.Error("the root was read at open")
	}
}

func TestOpenErrors(t *testing.T) {
	root := tree(t, "a.txt")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name  string
		ctx   context.Context
		path  string
		route string
	}{
		{"cancelled", cancelled, root, "Open.WorkspaceOpenCancelled"},
		{"empty", context.Background(), "", "Open.WorkspacePathInvalid"},
		{"missing", context.Background(), filepath.Join(root, "nope"), "Open.WorkspaceFolderNotFound"},
		{"a file", context.Background(), filepath.Join(root, "a.txt"), "Open.WorkspaceNotDirectory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Open(tc.ctx, tc.path)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestOpenStatFailed(t *testing.T) {
	root := tree(t, "a/")
	if err := os.Chmod(root, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if f, err := os.Open(filepath.Join(root, "a")); err == nil {
		_ = f.Close()
		t.Skip("permissions are not enforced (running as root?)")
	}
	_, err := Open(context.Background(), filepath.Join(root, "a"))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Open.WorkspaceStatFailed") {
		t.Errorf("got %v", err)
	}
}

func TestSortOrders(t *testing.T) {
	root := tree(t, "b.txt", "B.go", "a.txt", "dir2/", "Dir10/", "file10.txt", "file2.txt", "Zeta.go", "adir/", ".hidden")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// the modification times: newest last in this list
	for i, n := range []string{"file2.txt", "a.txt", "b.txt", "dir2", "adir", "Dir10", "B.go", "file10.txt", "Zeta.go", ".hidden"} {
		if err := os.Chtimes(filepath.Join(root, n), base.Add(time.Duration(i)*time.Hour), base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{"default: folders first, names natural", Config{SortOrder: SortDefault},
			[]string{"adir", "dir2", "Dir10", ".hidden", "a.txt", "B.go", "b.txt", "file2.txt", "file10.txt", "Zeta.go"}},
		{"default reversed", Config{SortOrder: SortDefault, Reverse: true},
			[]string{"Zeta.go", "file10.txt", "file2.txt", "b.txt", "B.go", "a.txt", ".hidden", "Dir10", "dir2", "adir"}},
		{"mixed: folders among files", Config{SortOrder: SortMixed},
			[]string{".hidden", "a.txt", "adir", "B.go", "b.txt", "dir2", "Dir10", "file2.txt", "file10.txt", "Zeta.go"}},
		{"filesFirst", Config{SortOrder: SortFilesFirst},
			[]string{".hidden", "a.txt", "B.go", "b.txt", "file2.txt", "file10.txt", "Zeta.go", "adir", "dir2", "Dir10"}},
		{"type: by extension", Config{SortOrder: SortType},
			[]string{"adir", "dir2", "Dir10", ".hidden", "B.go", "Zeta.go", "a.txt", "b.txt", "file2.txt", "file10.txt"}},
		{"modified: newest first", Config{SortOrder: SortModified},
			[]string{"Dir10", "adir", "dir2", ".hidden", "Zeta.go", "file10.txt", "B.go", "b.txt", "a.txt", "file2.txt"}},
		{"foldersNestsFiles is default without nesting", Config{SortOrder: SortFoldersNestsFiles},
			[]string{"adir", "dir2", "Dir10", ".hidden", "a.txt", "B.go", "b.txt", "file2.txt", "file10.txt", "Zeta.go"}},
		{"upper case first", Config{SortOrder: SortDefault, Lexicographic: LexUpper},
			[]string{"Dir10", "adir", "dir2", ".hidden", "B.go", "Zeta.go", "a.txt", "b.txt", "file2.txt", "file10.txt"}},
		{"lower case first", Config{SortOrder: SortDefault, Lexicographic: LexLower},
			[]string{"adir", "dir2", "Dir10", ".hidden", "a.txt", "b.txt", "file2.txt", "file10.txt", "B.go", "Zeta.go"}},
		{"unicode", Config{SortOrder: SortDefault, Lexicographic: LexUnicode},
			[]string{"Dir10", "adir", "dir2", ".hidden", "B.go", "Zeta.go", "a.txt", "b.txt", "file10.txt", "file2.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			w := open(t, root, &cfg)
			kids, err := w.Root().Children(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := names(kids); !slices.Equal(got, tc.want) {
				t.Errorf("order\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestSetConfigReorders(t *testing.T) {
	root := tree(t, "b.txt", "a.txt", "dir/")
	w := open(t, root, nil)
	if _, err := w.Root().Children(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := w.Config()
	cfg.SortOrder = SortFilesFirst
	if !w.SetConfig(cfg) {
		t.Error("SetConfig said nothing changed")
	}
	if w.SetConfig(cfg) {
		t.Error("SetConfig said the same configuration changed")
	}
	if got, want := names(w.Root().Loaded()), []string{"a.txt", "b.txt", "dir"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExclude(t *testing.T) {
	root := tree(t, ".git/config", ".DS_Store", "a.ts", "a.js", "b.js", "src/.git/x", "src/Thumbs.db", "src/keep.txt", "node_modules/x/y.js")
	tests := []struct {
		name    string
		exclude map[string]any
		want    []string
	}{
		{"the defaults of VS Code", nil, []string{"node_modules", "src", "a.js", "a.ts", "b.js"}},
		{"a pattern turned off", map[string]any{"**/.git": false, "**/.DS_Store": true},
			[]string{".git", "node_modules", "src", "a.js", "a.ts", "b.js"}},
		{"a sibling: js beside a ts of the same name", map[string]any{
			"**/.git": true, "**/.DS_Store": true, "**/*.js": map[string]any{"when": "$(basename).ts"},
		}, []string{"node_modules", "src", "a.ts", "b.js"}},
		{"a folder", map[string]any{"**/node_modules": true, "**/.git": true, "**/.DS_Store": true},
			[]string{"src", "a.js", "a.ts", "b.js"}},
		{"nothing", map[string]any{}, []string{".git", "node_modules", "src", ".DS_Store", "a.js", "a.ts", "b.js"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			if tc.exclude != nil {
				cfg.Exclude = glob.ExpressionFromSetting(tc.exclude)
			}
			w := open(t, root, &cfg)
			kids, err := w.Root().Children(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := names(kids); !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExcludeBelowTheRootUsesTheRelativePath(t *testing.T) {
	root := tree(t, "src/gen/a.go", "src/keep/b.go", "gen/c.go")
	cfg := Config{Exclude: glob.ExpressionFromSetting(map[string]any{"src/gen": true})}
	w := open(t, root, &cfg)
	ctx := context.Background()
	top, _ := w.Root().Children(ctx)
	if got := names(top); !slices.Equal(got, []string{"gen", "src"}) {
		t.Fatalf("top: %q", got)
	}
	src, _ := w.Find(filepath.Join(root, "src")).Children(ctx)
	if got := names(src); !slices.Equal(got, []string{"keep"}) {
		t.Errorf("src: %q", got)
	}
	if !w.Excluded(filepath.Join(root, "src", "gen"), nil) || w.Excluded(filepath.Join(root, "gen"), nil) {
		t.Error("Excluded gave the wrong answers")
	}
	if w.Excluded(filepath.Join(t.TempDir(), "src", "gen"), nil) {
		t.Error("a path outside the folder is not excluded")
	}
}

// countingFS counts the folders it reads.
type countingFS struct {
	mu    sync.Mutex
	reads map[string]int
	fail  map[string]error
}

func (c *countingFS) ReadDir(path string) ([]fs.DirEntry, error) {
	c.mu.Lock()
	c.reads[path]++
	err := c.fail[path]
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return os.ReadDir(path)
}
func (c *countingFS) Stat(path string) (fs.FileInfo, error)  { return os.Stat(path) }
func (c *countingFS) Lstat(path string) (fs.FileInfo, error) { return os.Lstat(path) }

func (c *countingFS) count(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads[path]
}

func TestChildrenAreReadLazilyAndOnce(t *testing.T) {
	root := tree(t, "a/b/c.txt", "a/d.txt", "z/y.txt", "f.txt")
	cfs := &countingFS{reads: map[string]int{}}
	w, err := Open(context.Background(), root, Options{FS: cfs})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if len(cfs.reads) != 0 {
		t.Fatalf("Open read %v", cfs.reads)
	}
	for i := 0; i < 3; i++ {
		if _, err := w.Root().Children(ctx); err != nil {
			t.Fatal(err)
		}
	}
	a := w.Find(filepath.Join(root, "a"))
	if _, err := a.Children(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(cfs.reads); got != 2 {
		t.Errorf("read %d folders (%v), want the root and a", got, cfs.reads)
	}
	if cfs.count(root) != 1 || cfs.count(filepath.Join(root, "a")) != 1 {
		t.Errorf("reads: %v", cfs.reads)
	}
	if f := w.Find(filepath.Join(root, "f.txt")); f == nil {
		t.Error("f.txt is not found")
	} else if kids, err := f.Children(ctx); kids != nil || err != nil {
		t.Errorf("a file has children %v, %v", kids, err)
	}
	if w.Find(filepath.Join(root, "z", "y.txt")) != nil {
		t.Error("Find returned an item of a folder that was not read")
	}
}

func TestAFolderThatCannotBeReadKeepsItsError(t *testing.T) {
	root := tree(t, "ok/f.txt", "bad/g.txt")
	cfs := &countingFS{reads: map[string]int{}, fail: map[string]error{filepath.Join(root, "bad"): fs.ErrPermission}}
	w, err := Open(context.Background(), root, Options{FS: cfs})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := w.Root().Children(ctx); err != nil {
		t.Fatal(err)
	}
	bad := w.Find(filepath.Join(root, "bad"))
	_, err = bad.Children(ctx)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Item.Children->Workspace.read->Workspace.list.WorkspaceReadDirFailed") {
		t.Fatalf("got %v", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Error("the cause is lost")
	}
	if bad.Err() == nil || bad.Resolved() {
		t.Errorf("Err %v Resolved %v", bad.Err(), bad.Resolved())
	}
	ok1, err := w.Find(filepath.Join(root, "ok")).Children(ctx)
	if err != nil || len(ok1) != 1 {
		t.Errorf("the rest of the tree is affected: %v %v", ok1, err)
	}
	// the folder is read again, and works, when it is expanded again
	cfs.mu.Lock()
	delete(cfs.fail, filepath.Join(root, "bad"))
	cfs.mu.Unlock()
	if kids, err := bad.Children(ctx); err != nil || len(kids) != 1 || bad.Err() != nil {
		t.Errorf("retry: %v %v %v", kids, err, bad.Err())
	}
}

func TestReadCancelled(t *testing.T) {
	root := tree(t, "a.txt")
	w := open(t, root, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.Root().Children(ctx)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Item.Children->Workspace.read->Workspace.list.WorkspaceReadCancelled") {
		t.Errorf("got %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("the cause is lost")
	}
}

func TestSymlinks(t *testing.T) {
	root := tree(t, "real/f.txt", "file.txt")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Symlink("real", filepath.Join(root, "link")))
	must(os.Symlink("file.txt", filepath.Join(root, "flink")))
	must(os.Symlink("nowhere", filepath.Join(root, "broken")))
	must(os.Symlink(".", filepath.Join(root, "real", "loop")))
	w := open(t, root, nil)
	ctx := context.Background()
	kids, err := w.Root().Children(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type flags struct{ dir, link bool }
	got := map[string]flags{}
	for _, k := range kids {
		got[k.Name()] = flags{k.IsDir(), k.IsSymlink()}
	}
	want := map[string]flags{"real": {true, false}, "link": {true, true}, "flink": {false, true}, "broken": {false, true}, "file.txt": {false, false}}
	for n, f := range want {
		if got[n] != f {
			t.Errorf("%s: %+v, want %+v", n, got[n], f)
		}
	}
	// a link that points at its own folder can be expanded again and again, but never by itself
	real := w.Find(filepath.Join(root, "real"))
	loop, err := real.Children(ctx)
	if err != nil || len(loop) != 2 {
		t.Fatalf("real: %v %v", names(loop), err)
	}
	var l *Item
	for _, k := range loop {
		if k.Name() == "loop" {
			l = k
		}
	}
	for i := 0; i < 5; i++ {
		kids, err := l.Children(ctx)
		if err != nil || len(kids) != 2 {
			t.Fatalf("depth %d: %v %v", i, names(kids), err)
		}
		for _, k := range kids {
			if k.Name() == "loop" {
				l = k
			}
		}
	}
}

func TestCompactChains(t *testing.T) {
	root := tree(t, "top/a/b/c/f.txt", "top/a/b/c/g.txt", "one/x/y/z.txt", "multi/p/q.txt", "multi/r.txt", "e/", "deep/m/n/")
	ctx := context.Background()
	chain := func(cfg Config, path string) []string {
		t.Helper()
		w := open(t, root, &cfg)
		if _, err := w.Root().Children(ctx); err != nil {
			t.Fatal(err)
		}
		// find the item by expanding its parents
		item := w.Root()
		for _, name := range strings.Split(path, "/") {
			kids, err := item.Children(ctx)
			if err != nil {
				t.Fatal(err)
			}
			item = nil
			for _, k := range kids {
				if k.Name() == name {
					item = k
				}
			}
			if item == nil {
				t.Fatalf("no %s", name)
			}
		}
		c, err := w.CompactChain(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		return names(c)
	}
	on := Config{CompactFolders: true}
	tests := []struct {
		name string
		cfg  Config
		path string
		want []string
	}{
		{"a chain ends where there are several children", on, "top/a", []string{"a", "b", "c"}},
		{"a folder below the root is not compressed", on, "top", []string{"top"}},
		{"a folder with a file and a folder is alone", on, "multi/p", []string{"p"}},
		{"a chain ends at an empty folder", on, "deep/m", []string{"m", "n"}},
		{"a chain ends at a folder with a file", on, "one/x", []string{"x", "y"}},
		{"an empty folder", on, "e", []string{"e"}},
		{"the setting is off", Config{}, "top/a", []string{"a"}},
		{"a file is alone", on, "multi/r.txt", []string{"r.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chain(tc.cfg, tc.path); !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompactChainsDoNotFollowLinks(t *testing.T) {
	root := tree(t, "d/a/f.txt")
	if err := os.Symlink("..", filepath.Join(root, "d", "a", "up")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "d", "a", "f.txt")); err != nil {
		t.Fatal(err)
	}
	cfg := Config{CompactFolders: true}
	w := open(t, root, &cfg)
	ctx := context.Background()
	d, _ := w.Root().Children(ctx)
	sub, _ := d[0].Children(ctx)
	chain, err := w.CompactChain(ctx, sub[0])
	if err != nil || !slices.Equal(names(chain), []string{"a"}) {
		t.Errorf("chain %q, %v: a link must end the chain", names(chain), err)
	}
}

func TestCompactChainError(t *testing.T) {
	root := tree(t, "d/a/b/f.txt")
	cfs := &countingFS{reads: map[string]int{}, fail: map[string]error{filepath.Join(root, "d", "a", "b"): fs.ErrPermission}}
	cfg := Config{CompactFolders: true}
	w, err := Open(context.Background(), root, Options{FS: cfs, Config: &cfg})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d, _ := w.Root().Children(ctx)
	sub, _ := d[0].Children(ctx)
	chain, err := w.CompactChain(ctx, sub[0])
	if err == nil || !slices.Equal(names(chain), []string{"a", "b"}) {
		t.Errorf("chain %q, err %v: the chain should end at the folder that cannot be read", names(chain), err)
	}
}

func TestRefreshKeepsItemsAndWhatIsRead(t *testing.T) {
	root := tree(t, "a/b/f.txt", "a/g.txt", "c.txt")
	w := open(t, root, nil)
	ctx := context.Background()
	top, _ := w.Root().Children(ctx)
	a := top[0]
	aKids, _ := a.Children(ctx)
	b := aKids[0]
	if _, err := b.Children(ctx); err != nil {
		t.Fatal(err)
	}
	var changes []Change
	w.OnChange(func(c Change) { changes = append(changes, c) })

	if err := os.WriteFile(filepath.Join(root, "a", "new.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "c.txt")); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := names(w.Root().Loaded()); !slices.Equal(got, []string{"a"}) {
		t.Errorf("root: %q", got)
	}
	if got := names(a.Loaded()); !slices.Equal(got, []string{"b", "g.txt", "new.txt"}) {
		t.Errorf("a: %q", got)
	}
	if a.Child("b") != b || !b.Resolved() {
		t.Error("refresh made new items or forgot what was read")
	}
	if len(changes) != 1 || changes[0].Kind != ChangeRefreshed || changes[0].Path != root {
		t.Errorf("changes: %+v", changes)
	}
	// a folder that was read and is gone leaves the tree
	if err := os.RemoveAll(filepath.Join(root, "a", "b")); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if a.Child("b") != nil {
		t.Error("the folder that is gone is still in the tree")
	}
}

func TestRefreshOfAFolderThatWasNotReadDoesNothing(t *testing.T) {
	root := tree(t, "a/f.txt")
	w := open(t, root, nil)
	if err := w.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if w.Root().Resolved() {
		t.Error("Refresh read a folder that was not read")
	}
}

func TestRefreshCancelled(t *testing.T) {
	root := tree(t, "a/f.txt")
	w := open(t, root, nil)
	if _, err := w.Root().Children(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := w.Refresh(ctx, nil)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Workspace.Refresh->Workspace.refresh->Workspace.read->Workspace.list.WorkspaceReadCancelled") {
		t.Errorf("got %v", err)
	}
}

func TestConfigFromSettings(t *testing.T) {
	reg := settings.DefaultRegistry()
	store := settings.NewStore(reg, settings.StoreOptions{})
	got := ConfigFromSettings(store)
	if got.SortOrder != SortDefault || got.Lexicographic != LexDefault || got.Reverse || !got.CompactFolders {
		t.Errorf("defaults: %+v", got)
	}
	if !got.equal(DefaultConfig()) {
		t.Errorf("DefaultConfig differs from the default settings:\n%+v\n%+v", got, DefaultConfig())
	}
	ws := WatchSettingsFromSettings(store)
	if want := []string{"*/.git/objects/**", "*/.git/subtree-cache/**", "*/.hg/store/**", ".git/objects/**", ".git/subtree-cache/**", ".hg/store/**"}; !slices.Equal(ws.Exclude, want) {
		t.Errorf("watcherExclude: %q", ws.Exclude)
	}
	if len(ws.Include) != 0 {
		t.Errorf("watcherInclude: %q", ws.Include)
	}
}
