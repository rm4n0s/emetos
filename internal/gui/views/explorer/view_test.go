package explorer

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

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// opened is a file the view was asked to open.
type opened struct {
	path   string
	pinned bool
}

// rig is a view over a temporary folder, with a queue for the UI thread that the test runs itself.
type rig struct {
	t      *testing.T
	root   string
	ws     *workspace.Workspace
	view   *View
	keys   *contextkey.Scope
	mu     sync.Mutex
	posted []func()
	opened []opened
	failed []string
}

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

func newRig(t *testing.T, entries ...string) *rig {
	t.Helper()
	return newRigFS(t, nil, entries...)
}

// newRigFS is newRig over a file system that is not the operating system's.
func newRigFS(t *testing.T, fsys func(workspace.FS) workspace.FS, entries ...string) *rig {
	t.Helper()
	r := &rig{t: t, root: tree(t, entries...), keys: contextkey.NewRoot()}
	cfg := workspace.DefaultConfig()
	cfg.CompactFolders = true
	opts := workspace.Options{Config: &cfg}
	if fsys != nil {
		opts.FS = fsys(osFS{})
	}
	ws, err := workspace.Open(context.Background(), r.root, opts)
	if err != nil {
		t.Fatal(err)
	}
	r.ws = ws
	r.view = NewView(context.Background(), ws, ViewOpts{
		Post: func(f func()) {
			r.mu.Lock()
			r.posted = append(r.posted, f)
			r.mu.Unlock()
		},
		Keys: r.keys,
		Icons: func() *theme.FileIconTheme {
			th, err := theme.BundledFileIconTheme(theme.DefaultFileIconTheme)
			if err != nil {
				t.Error(err)
			}
			return th
		},
		Language:     languageOf,
		OnOpen:       func(path string, pinned bool) { r.opened = append(r.opened, opened{path, pinned}) },
		OnLoadFailed: func(path string, _ error) { r.failed = append(r.failed, path) },
	})
	t.Cleanup(r.view.Dispose)
	r.wait("the folder", func() bool { return r.view.Loaded() })
	return r
}

// languageOf is the language of a few extensions, as the language registry says them.
func languageOf(path string) string {
	switch filepath.Ext(path) {
	case ".go":
		return "go"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	}
	if filepath.Base(path) == "Makefile" {
		return "makefile"
	}
	return ""
}

// osFS is the operating system's file system, which the test file systems wrap.
type osFS struct{}

func (osFS) ReadDir(path string) ([]fs.DirEntry, error) { return os.ReadDir(path) }
func (osFS) Stat(path string) (fs.FileInfo, error)      { return os.Stat(path) }
func (osFS) Lstat(path string) (fs.FileInfo, error)     { return os.Lstat(path) }

// failing is a file system that cannot read some folders.
type failing struct {
	workspace.FS
	paths map[string]bool
}

func (f failing) ReadDir(path string) ([]fs.DirEntry, error) {
	if f.paths[path] {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission}
	}
	return f.FS.ReadDir(path)
}

// run runs what was posted to the UI thread; it reports how many functions ran.
func (r *rig) run() int {
	n := 0
	for {
		r.mu.Lock()
		posted := r.posted
		r.posted = nil
		r.mu.Unlock()
		if len(posted) == 0 {
			return n
		}
		for _, f := range posted {
			f()
			n++
		}
	}
}

// wait runs the UI thread until the condition holds; the background reads hand their answers to it.
func (r *rig) wait(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.run()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s; the rows are %v", what, r.keys2())
		}
		time.Sleep(time.Millisecond)
	}
}

// keys2 are the paths of the rows relative to the folder, a folder with a slash at the end, and a "*" before
// the selected one.
func (r *rig) keys2() []string {
	var out []string
	for _, n := range r.view.Tree().Rows() {
		rel, _ := filepath.Rel(r.root, n.Key)
		if n.Placeholder {
			rel = "(loading)"
		} else if nd, ok := n.Data.(*nodeData); ok && nd.IsDir {
			rel += "/"
		}
		if r.view.Tree().IsSelected(n.Key) {
			rel = "*" + rel
		}
		out = append(out, rel)
	}
	return out
}

// loaded tells that no folder shows the placeholder of a read in progress.
func (r *rig) loaded() bool {
	for _, n := range r.view.Tree().Rows() {
		if n.Placeholder {
			return false
		}
	}
	return len(r.view.Tree().Rows()) > 0
}

func (r *rig) path(rel string) string { return filepath.Join(r.root, filepath.FromSlash(rel)) }

func (r *rig) labels() []string {
	var out []string
	for _, n := range r.view.Tree().Rows() {
		if nd, ok := n.Data.(*nodeData); ok {
			out = append(out, nd.label())
		}
	}
	return out
}

func TestViewRows(t *testing.T) {
	r := newRig(t, "src/app/main.go", "src/app/util.go", "README.md", "docs/guide.md", "empty/", ".hidden", ".git/config")
	if got, want := r.keys2(), []string{"docs/", "empty/", "src/", ".hidden", "README.md"}; !slices.Equal(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	// nothing below a folder is read before it is expanded
	for _, key := range []string{"docs", "empty", "src"} {
		node, _ := r.view.Tree().Node(r.path(key))
		if !node.Expandable || node.Expanded {
			t.Errorf("%s: %+v, want an expandable folder that is closed", key, node)
		}
	}
	if docs := r.ws.Find(r.path("docs")); docs.Resolved() {
		t.Error("docs was read before it was expanded")
	}
}

func TestViewExpandsLazily(t *testing.T) {
	tests := []struct {
		name   string
		expand []string
		want   []string
	}{
		{"a folder with files", []string{"docs"}, []string{"docs/", "docs/guide.md", "src/", "top/"}},
		{"folders and files below it", []string{"docs", "src"}, []string{"docs/", "docs/guide.md", "src/", "src/app/", "src/lib.go", "top/"}},
		{"a chain of single folders is one row", []string{"top"}, []string{"docs/", "src/", "top/", "top/a/"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, "src/app/main.go", "src/app/util.go", "src/lib.go", "docs/guide.md", "top/a/b/c/f.txt", "top/a/b/c/g.txt")
			for _, p := range tt.expand {
				r.view.Tree().Expand(r.path(p))
				r.wait(p+" to be read", func() bool { return r.ws.Find(r.path(p)).Resolved() })
			}
			r.wait("the rows to show what was read", r.loaded)
			if got := r.keys2(); !slices.Equal(got, tt.want) {
				t.Errorf("rows %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("the chain is named with slashes and has its children", func(t *testing.T) {
		r := newRig(t, "top/a/b/c/f.txt", "top/a/b/c/g.txt")
		r.view.Tree().Expand(r.path("top"))
		r.wait("top", r.loaded)
		if got, want := r.labels(), []string{"top", "a/b/c"}; !slices.Equal(got, want) {
			t.Errorf("labels %v, want %v", got, want)
		}
		r.view.Tree().Expand(r.path("top/a"))
		r.run()
		if got, want := r.keys2(), []string{"top/", "top/a/", "top/a/b/c/f.txt", "top/a/b/c/g.txt"}; !slices.Equal(got, want) {
			t.Errorf("rows %v, want %v", got, want)
		}
	})
	t.Run("the setting off shows one folder a row", func(t *testing.T) {
		r := newRig(t, "top/a/b/c/f.txt")
		cfg := r.ws.Config()
		cfg.CompactFolders = false
		r.ws.SetConfig(cfg)
		r.view.Tree().Expand(r.path("top"))
		r.wait("top", r.loaded)
		if got, want := r.labels(), []string{"top", "a"}; !slices.Equal(got, want) {
			t.Errorf("labels %v, want %v", got, want)
		}
	})
}

func TestViewKeepsTheTreeWhenFilesChange(t *testing.T) {
	r := newRig(t, "src/a.go", "src/b.go", "docs/guide.md")
	r.view.Tree().Expand(r.path("src"))
	r.wait("src", r.loaded)
	r.view.Tree().SetSelected(r.path("src/b.go"))
	ctx := context.Background()
	tests := []struct {
		name  string
		do    func()
		want  []string
		check func(t *testing.T)
	}{
		{"a file is created", func() {
			if err := os.WriteFile(r.path("src/c.go"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			r.ws.Apply(ctx, []workspace.Event{{Kind: workspace.EventCreated, Path: r.path("src/c.go")}})
		}, []string{"docs/", "src/", "src/a.go", "*src/b.go", "src/c.go"}, nil},
		{"a file in a folder that was not read is not shown", func() {
			if err := os.WriteFile(r.path("docs/new.md"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			r.ws.Apply(ctx, []workspace.Event{{Kind: workspace.EventCreated, Path: r.path("docs/new.md")}})
		}, []string{"docs/", "src/", "src/a.go", "*src/b.go", "src/c.go"}, nil},
		{"a file is deleted", func() {
			if err := os.Remove(r.path("src/a.go")); err != nil {
				t.Fatal(err)
			}
			r.ws.Apply(ctx, []workspace.Event{{Kind: workspace.EventDeleted, Path: r.path("src/a.go")}})
		}, []string{"docs/", "src/", "*src/b.go", "src/c.go"}, nil},
		{"a folder is created and is lazy", func() {
			if err := os.MkdirAll(r.path("lib"), 0o755); err != nil {
				t.Fatal(err)
			}
			r.ws.Apply(ctx, []workspace.Event{{Kind: workspace.EventCreated, Path: r.path("lib"), IsDir: true}})
		}, []string{"docs/", "lib/", "src/", "*src/b.go", "src/c.go"}, nil},
		{"the selected file is renamed", func() {
			if err := os.Rename(r.path("src/b.go"), r.path("src/z.go")); err != nil {
				t.Fatal(err)
			}
			r.ws.Apply(ctx, []workspace.Event{{Kind: workspace.EventRenamed, OldPath: r.path("src/b.go"), Path: r.path("src/z.go")}})
		}, []string{"docs/", "lib/", "src/", "src/c.go", "src/z.go"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.do()
			r.wait("the rows", func() bool { return slices.Equal(r.keys2(), tt.want) })
		})
	}
}

func TestViewRefreshReadsTheFoldersAgain(t *testing.T) {
	r := newRig(t, "src/a.go")
	r.view.Tree().Expand(r.path("src"))
	r.wait("src", r.loaded)
	if err := os.WriteFile(r.path("src/b.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.path("top.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.view.Refresh()
	r.wait("the new files", func() bool { return slices.Contains(r.keys2(), "src/b.go") && slices.Contains(r.keys2(), "top.txt") })
	if !r.view.Tree().IsExpanded(r.path("src")) {
		t.Error("src was folded by the refresh")
	}
}

func TestViewCollapseAll(t *testing.T) {
	r := newRig(t, "a/x.go", "b/y.go")
	for _, p := range []string{"a", "b"} {
		r.view.Tree().Expand(r.path(p))
	}
	r.wait("both folders", func() bool { return len(r.view.Tree().Rows()) == 4 })
	r.view.CollapseAll()
	if got, want := r.keys2(), []string{"a/", "b/"}; !slices.Equal(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
}

func TestViewReveal(t *testing.T) {
	entries := []string{"src/app/main.go", "src/app/util.go", "src/lib/x.go", "docs/guide.md", "README.md", ".git/config", "top/a/b/c/f.txt", "top/a/b/c/g.txt"}
	tests := []struct {
		name     string
		path     string
		mode     RevealMode
		found    bool
		want     []string
		scroll   bool
		selected string
	}{
		{"a file in a folder opens the folders above it", "src/app/main.go", RevealSelect, true,
			[]string{"docs/", "src/", "src/app/", "src/app/main.go", "src/app/util.go", "src/lib/", "top/", "README.md"}, true, "src/app/main.go"},
		{"a file at the top", "README.md", RevealSelect, true, []string{"docs/", "src/", "top/", "README.md"}, true, "README.md"},
		{"a folder is selected and not opened", "docs", RevealSelect, true, []string{"docs/", "src/", "top/", "README.md"}, true, "docs"},
		{"focusNoScroll does not scroll", "docs/guide.md", RevealNoScroll, true, []string{"docs/", "docs/guide.md", "src/", "top/", "README.md"}, false, "docs/guide.md"},
		{"a file in a chain selects the chain", "top/a/b/c/f.txt", RevealSelect, true,
			[]string{"docs/", "src/", "top/", "top/a/", "top/a/b/c/f.txt", "top/a/b/c/g.txt", "README.md"}, true, "top/a/b/c/f.txt"},
		{"a folder in a chain is the chain", "top/a/b", RevealSelect, true, []string{"docs/", "src/", "top/", "top/a/", "README.md"}, true, "top/a"},
		{"a file that is not there", "src/nothing.go", RevealSelect, false, []string{"docs/", "src/", "top/", "README.md"}, false, ""},
		{"a file that files.exclude hides", ".git/config", RevealSelect, false, []string{"docs/", "src/", "top/", "README.md"}, false, ""},
		{"a file outside the folder", "../outside.go", RevealSelect, false, []string{"docs/", "src/", "top/", "README.md"}, false, ""},
		{"the folder itself", ".", RevealSelect, false, []string{"docs/", "src/", "top/", "README.md"}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, entries...)
			var done, found bool
			r.view.Reveal(r.path(tt.path), tt.mode, func(f bool) { done, found = true, f })
			r.wait("the reveal", func() bool { return done })
			if found != tt.found {
				t.Fatalf("found = %v, want %v", found, tt.found)
			}
			// the rows, without the mark of the selection
			var rows []string
			for _, row := range r.keys2() {
				rows = append(rows, strings.TrimPrefix(row, "*"))
			}
			if !slices.Equal(rows, tt.want) {
				t.Errorf("rows %v, want %v", rows, tt.want)
			}
			sel := r.view.SelectedPath()
			if want := tt.selected; (want == "" && sel != "") || (want != "" && sel != r.path(want)) {
				t.Errorf("selected %q, want %q", sel, want)
			}
			if got := r.view.scrollTo != ""; got != tt.scroll {
				t.Errorf("scroll asked = %v, want %v", got, tt.scroll)
			}
			if tt.selected != "" {
				if key, _ := r.view.Tree().Focused(); key != r.path(tt.selected) {
					t.Errorf("focused %q, want %q", key, tt.selected)
				}
			}
		})
	}
	t.Run("revealing another file keeps the folders that are open", func(t *testing.T) {
		r := newRig(t, entries...)
		for _, p := range []string{"src/app/main.go", "docs/guide.md", "src/lib/x.go"} {
			var done bool
			r.view.Reveal(r.path(p), RevealSelect, func(bool) { done = true })
			r.wait("the reveal", func() bool { return done })
		}
		want := []string{"docs/", "docs/guide.md", "src/", "src/app/", "src/app/main.go", "src/app/util.go", "src/lib/", "*src/lib/x.go", "top/", "README.md"}
		if got := r.keys2(); !slices.Equal(got, want) {
			t.Errorf("rows %v, want %v", got, want)
		}
	})
}

func TestViewOpensFiles(t *testing.T) {
	r := newRig(t, "src/a.go", "README.md")
	tree := r.view.Tree()
	file, _ := tree.Node(r.path("README.md"))
	folder, _ := tree.Node(r.path("src"))
	r.view.open(file, false)
	r.view.open(file, true)
	r.view.open(folder, false)
	want := []opened{{r.path("README.md"), false}, {r.path("README.md"), true}}
	if !slices.Equal(r.opened, want) {
		t.Errorf("opened %v, want %v", r.opened, want)
	}
}

func TestViewFolderThatCannotBeRead(t *testing.T) {
	var bad string
	r := newRigFS(t, func(base workspace.FS) workspace.FS {
		return failingLazy{base, &bad}
	}, "src/a.go", "locked/b.go")
	bad = filepath.Join(r.root, "locked")
	r.view.Tree().Expand(r.path("locked"))
	r.wait("the failure", func() bool { return len(r.failed) == 1 })
	if r.failed[0] != r.path("locked") {
		t.Errorf("failed %v", r.failed)
	}
	if r.view.Tree().IsExpanded(r.path("locked")) {
		t.Error("the folder that could not be read stays open")
	}
	// the next expansion tries again
	bad = ""
	r.view.Tree().Expand(r.path("locked"))
	r.wait("the second try", func() bool { return slices.Contains(r.keys2(), "locked/b.go") })
	// the rest of the tree is not affected
	if got := r.keys2(); !slices.Contains(got, "src/") {
		t.Errorf("rows %v", got)
	}
}

// failingLazy fails to read the folder that bad names, which may change during the test.
type failingLazy struct {
	workspace.FS
	bad *string
}

func (f failingLazy) ReadDir(path string) ([]fs.DirEntry, error) {
	if *f.bad != "" && path == *f.bad {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission}
	}
	return f.FS.ReadDir(path)
}

func TestViewRootThatCannotBeRead(t *testing.T) {
	var root string
	r := &rig{t: t, root: tree(t, "a.go"), keys: contextkey.NewRoot()}
	root = r.root
	cfg := workspace.DefaultConfig()
	ws, err := workspace.Open(context.Background(), root, workspace.Options{Config: &cfg, FS: failing{osFS{}, map[string]bool{root: true}}})
	if err != nil {
		t.Fatal(err)
	}
	r.view = NewView(context.Background(), ws, ViewOpts{Post: func(f func()) { r.mu.Lock(); r.posted = append(r.posted, f); r.mu.Unlock() }})
	t.Cleanup(r.view.Dispose)
	r.wait("the folder", func() bool { return r.view.Loaded() })
	err = r.view.RootError()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("WorkspaceReadDirFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if len(r.view.Tree().Rows()) != 0 {
		t.Error("a folder that cannot be read has rows")
	}
}

func TestViewContextKeys(t *testing.T) {
	r := newRig(t, "src/a.go", "README.md")
	tests := []struct {
		name   string
		focus  string
		folder bool
	}{
		{"a folder is focused", "src", true},
		{"a file is focused", "README.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.view.Tree().SetFocus(r.path(tt.focus))
			if got := r.keys.Get("explorerResourceIsFolder"); got != tt.folder {
				t.Errorf("explorerResourceIsFolder = %v, want %v", got, tt.folder)
			}
			if r.keys.Get("explorerResourceIsRoot") != false || r.keys.Get("explorerResourceReadonly") != false {
				t.Error("a row is not the root and is not read-only")
			}
		})
	}
	r.view.Dispose()
	if got := r.keys.Get("explorerResourceIsFolder"); got != nil {
		t.Errorf("the key stays after the view is gone: %v", got)
	}
}

func TestViewIcons(t *testing.T) {
	r := newRig(t, "src/main.go", "README.md", "docs.md", "LICENSE", "data.bin", ".gitignore")
	icon := func(rel string, typ theme.Type) (theme.FileIcon, bool) {
		n, ok := r.view.Tree().Node(r.path(rel))
		if !ok {
			t.Fatalf("no row %s", rel)
		}
		return r.view.iconFor(n.Data.(*nodeData), n.Expanded, typ)
	}
	tests := []struct {
		name  string
		path  string
		typ   theme.Type
		glyph rune
		ok    bool
	}{
		{"a folder has no icon in Seti", "src", theme.TypeDark, 0, false},
		{"a markdown file", "docs.md", theme.TypeDark, 0xE060, true},
		{"a file name beats the language", "README.md", theme.TypeDark, 0xE04D, true},
		{"a file name", "LICENSE", theme.TypeDark, 0xE05A, true},
		{"the default file icon", "data.bin", theme.TypeDark, 0xE023, true},
		{"the light variant keeps the glyph", "data.bin", theme.TypeLight, 0xE023, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := icon(tt.path, tt.typ)
			if ok != tt.ok || (ok && got.Glyph != tt.glyph) {
				t.Errorf("icon = %U, %v; want %U, %v", got.Glyph, ok, tt.glyph, tt.ok)
			}
		})
	}
	dark, _ := icon("data.bin", theme.TypeDark)
	light, _ := icon("data.bin", theme.TypeLight)
	if dark.Color.Equal(light.Color) {
		t.Errorf("the light theme has the color of the dark one: %s", dark.Color.Hex())
	}
	// no icon theme: no icons
	r.view.opts.Icons = func() *theme.FileIconTheme { return nil }
	if _, ok := icon("README.md", theme.TypeDark); ok {
		t.Error("an icon with no icon theme")
	}
	r.view.opts.Icons = nil
	if _, ok := icon("README.md", theme.TypeDark); ok {
		t.Error("an icon with no icons function")
	}
}
