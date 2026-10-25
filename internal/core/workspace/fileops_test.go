package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

var opsTime = time.Date(2026, 10, 23, 14, 5, 9, 0, time.Local)

// opsEnv is a folder to work in and a trash in an isolated XDG_DATA_HOME.
type opsEnv struct {
	t     *testing.T
	root  string
	trash string
	ops   *FileOps
}

func newOpsEnv(t *testing.T) *opsEnv {
	t.Helper()
	paths := storagetest.Isolate(t)
	trash := fileio.NewHomeTrash(paths.TrashDir())
	trash.Now = func() time.Time { return opsTime }
	// the trash must be the isolated one, never the user's
	if !strings.HasPrefix(paths.TrashDir(), os.Getenv("XDG_DATA_HOME")) {
		t.Fatalf("the trash %s is not in XDG_DATA_HOME %s", paths.TrashDir(), os.Getenv("XDG_DATA_HOME"))
	}
	return &opsEnv{t: t, root: t.TempDir(), trash: paths.TrashDir(), ops: NewFileOps(FileOpsOptions{Trash: trash})}
}

func (e *opsEnv) p(rel string) string { return filepath.Join(e.root, filepath.FromSlash(rel)) }

// put writes files; a name that ends in a slash is a folder.
func (e *opsEnv) put(entries ...string) {
	e.t.Helper()
	for _, rel := range entries {
		if strings.HasSuffix(rel, "/") {
			if err := os.MkdirAll(e.p(rel), 0o755); err != nil {
				e.t.Fatal(err)
			}
			continue
		}
		name, content, _ := strings.Cut(rel, "=")
		if err := os.MkdirAll(filepath.Dir(e.p(name)), 0o755); err != nil {
			e.t.Fatal(err)
		}
		if content == "" {
			content = name
		}
		if err := os.WriteFile(e.p(name), []byte(content), 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
}

// tree lists the paths below the folder (relative, folders with a slash), sorted.
func (e *opsEnv) tree() []string {
	e.t.Helper()
	var out []string
	err := filepath.WalkDir(e.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(e.root, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			rel += "/"
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func (e *opsEnv) read(rel string) string {
	e.t.Helper()
	b, err := os.ReadFile(e.p(rel))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *opsEnv) wantTree(want ...string) {
	e.t.Helper()
	slices.Sort(want)
	if got := e.tree(); !slices.Equal(got, want) {
		e.t.Fatalf("tree = %v, want %v", got, want)
	}
}

func wantTag(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func ctx() context.Context { return context.Background() }

func TestCreate(t *testing.T) {
	tests := []struct {
		name   string
		setup  []string
		folder bool
		input  string
		want   []string
		target string
		top    string
	}{
		{"file", nil, false, "new.txt", []string{"new.txt"}, "new.txt", "new.txt"},
		{"folder", nil, true, "newdir", []string{"newdir/"}, "newdir", "newdir"},
		{"nested file makes the folders", nil, false, "a/b/c.txt", []string{"a/", "a/b/", "a/b/c.txt"}, "a/b/c.txt", "a"},
		{"nested file below an existing folder", []string{"a/"}, false, "a/b/c.txt", []string{"a/", "a/b/", "a/b/c.txt"}, "a/b/c.txt", "a/b"},
		{"a trailing slash makes a folder", nil, false, "d/", []string{"d/"}, "d", "d"},
		{"tabs are trimmed", nil, false, "\tx.txt\t", []string{"x.txt"}, "x.txt", "x.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpsEnv(t)
			e.put(tt.setup...)
			var res OpResult
			var err error
			if tt.folder {
				res, err = e.ops.CreateFolder(ctx(), e.root, tt.input)
			} else {
				res, err = e.ops.CreateFile(ctx(), e.root, tt.input)
			}
			if err != nil {
				t.Fatal(err)
			}
			want := append(append([]string{}, tt.setup...), tt.want...)
			slices.Sort(want)
			want = slices.Compact(want)
			e.wantTree(want...)
			if !slices.Equal(res.Targets, []string{e.p(tt.target)}) || !slices.Equal(res.Created, []string{e.p(tt.top)}) {
				t.Errorf("targets %v created %v", res.Targets, res.Created)
			}
		})
	}
}

func TestCreateErrors(t *testing.T) {
	tests := []struct {
		name   string
		setup  []string
		parent func(e *opsEnv) string
		input  string
		route  string
	}{
		{"exists", []string{"a.txt"}, nil, "a.txt", "FileOps.create.FileOpDestinationExists"},
		{"exists as a folder", []string{"d/"}, nil, "d/", "FileOps.create.FileOpDestinationExists"},
		{"empty name", nil, nil, "", "FileOps.create.FileOpNameInvalid"},
		{"dot dot", nil, nil, "../x", "FileOps.create.FileOpNameInvalid"},
		{"dot", nil, nil, ".", "FileOps.create.FileOpNameInvalid"},
		{"relative parent", nil, func(*opsEnv) string { return "rel" }, "x", "FileOps.create->checkPath.FileOpPathInvalid"},
		{"missing parent", nil, func(e *opsEnv) string { return e.p("nope") }, "x", "FileOps.create.FileOpParentMissing"},
		{"a file is no parent", []string{"f"}, func(e *opsEnv) string { return e.p("f") }, "x", "FileOps.create.FileOpParentMissing"},
		{"blocked by a file", []string{"f"}, nil, "f/x.txt", "FileOps.create.FileOpCreateFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpsEnv(t)
			e.put(tt.setup...)
			parent := e.root
			if tt.parent != nil {
				parent = tt.parent(e)
			}
			_, err := e.ops.CreateFile(ctx(), parent, tt.input)
			wantTag(t, err, tt.route)
			if e.ops.CanUndo() {
				t.Error("a failed operation is in the history")
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		e := newOpsEnv(t)
		c, cancel := context.WithCancel(ctx())
		cancel()
		_, err := e.ops.CreateFile(c, e.root, "x")
		wantTag(t, err, "FileOps.create.FileOpCancelled")
	})
}

func TestRename(t *testing.T) {
	tests := []struct {
		name  string
		setup []string
		path  string
		to    string
		want  []string
		moves []PathMove
	}{
		{"file", []string{"a.txt"}, "a.txt", "b.txt", []string{"b.txt"}, []PathMove{{"a.txt", "b.txt"}}},
		{"case only", []string{"readme.md"}, "readme.md", "README.md", []string{"README.md"}, []PathMove{{"readme.md", "README.md"}}},
		{"folder with content", []string{"d/x.txt"}, "d", "e", []string{"e/", "e/x.txt"}, []PathMove{{"d", "e"}}},
		{"the same name does nothing", []string{"a.txt"}, "a.txt", "a.txt", []string{"a.txt"}, nil},
		{"into a subfolder", []string{"a.txt"}, "a.txt", "sub/a.txt", []string{"sub/", "sub/a.txt"}, []PathMove{{"a.txt", "sub/a.txt"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpsEnv(t)
			e.put(tt.setup...)
			res, err := e.ops.Rename(ctx(), e.p(tt.path), tt.to)
			if err != nil {
				t.Fatal(err)
			}
			e.wantTree(tt.want...)
			var moves []PathMove
			for _, m := range tt.moves {
				moves = append(moves, PathMove{e.p(m.Old), e.p(m.New)})
			}
			if !slices.Equal(res.Moves, moves) {
				t.Errorf("moves = %v, want %v", res.Moves, moves)
			}
			if got := e.ops.CanUndo(); got != (len(moves) > 0) {
				t.Errorf("CanUndo = %v", got)
			}
		})
	}
}

func TestRenameErrors(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt", "b.txt", "d/")
	tests := []struct {
		name  string
		path  string
		to    string
		route string
	}{
		{"onto another file", e.p("a.txt"), "b.txt", "FileOps.Rename->FileOps.moveAll->FileOps.moveItem.FileOpDestinationExists"},
		{"missing source", e.p("nope"), "x", "FileOps.moveItem.FileOpSourceMissing"},
		{"empty name", e.p("a.txt"), "", "FileOps.Rename.FileOpNameInvalid"},
		{"up", e.p("a.txt"), "../x", "FileOps.Rename.FileOpNameInvalid"},
		{"relative path", "a.txt", "x", "FileOps.Rename->checkPath.FileOpPathInvalid"},
		{"into itself", e.p("d"), "d/sub/d", "FileOps.moveItem.FileOpMoveIntoItself"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.ops.Rename(ctx(), tt.path, tt.to)
			wantTag(t, err, tt.route)
		})
	}
	e.wantTree("a.txt", "b.txt", "d/")
}

func TestMove(t *testing.T) {
	tests := []struct {
		name      string
		setup     []string
		srcs      []string
		dst       string
		overwrite bool
		want      []string
		trashed   []string // names in the trash
	}{
		{"file into a folder", []string{"a.txt", "d/"}, []string{"a.txt"}, "d", false, []string{"d/", "d/a.txt"}, nil},
		{"several items", []string{"a.txt", "b.txt", "d/"}, []string{"a.txt", "b.txt"}, "d", false, []string{"d/", "d/a.txt", "d/b.txt"}, nil},
		{"a folder with content", []string{"x/y.txt", "d/"}, []string{"x"}, "d", false, []string{"d/", "d/x/", "d/x/y.txt"}, nil},
		{"overwrite sends the old one to the trash", []string{"a.txt=new", "d/a.txt=old"}, []string{"a.txt"}, "d", true, []string{"d/", "d/a.txt"}, []string{"a.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpsEnv(t)
			e.put(tt.setup...)
			var srcs []string
			for _, s := range tt.srcs {
				srcs = append(srcs, e.p(s))
			}
			res, err := e.ops.Move(ctx(), e.p(tt.dst), srcs, MoveOptions{Overwrite: tt.overwrite})
			if err != nil {
				t.Fatal(err)
			}
			e.wantTree(tt.want...)
			if len(res.Moves) != len(srcs) {
				t.Errorf("moves = %v", res.Moves)
			}
			entries, _ := os.ReadDir(filepath.Join(e.trash, "files"))
			var names []string
			for _, en := range entries {
				names = append(names, en.Name())
			}
			if !slices.Equal(names, tt.trashed) {
				t.Errorf("trash = %v, want %v", names, tt.trashed)
			}
		})
	}
}

func TestMoveErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup []string
		src   string
		dst   string
		route string
	}{
		{"name taken", []string{"a.txt", "d/a.txt"}, "a.txt", "d", "FileOps.moveItem.FileOpDestinationExists"},
		{"into itself", []string{"d/sub/"}, "d", "d/sub", "FileOps.moveItem.FileOpMoveIntoItself"},
		{"onto itself", []string{"d/"}, "d", "d", "FileOps.moveItem.FileOpMoveIntoItself"},
		{"already there", []string{"d/a.txt"}, "d/a.txt", "d", "FileOps.moveItem.FileOpMoveSame"},
		{"missing", nil, "nope", "", "FileOps.moveItem.FileOpSourceMissing"},
		{"relative", nil, "rel", "", "FileOps.Move->checkPath.FileOpPathInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpsEnv(t)
			e.put(tt.setup...)
			src := e.p(tt.src)
			if tt.src == "rel" {
				src = "rel"
			}
			before := e.tree()
			_, err := e.ops.Move(ctx(), e.p(tt.dst), []string{src}, MoveOptions{})
			wantTag(t, err, tt.route)
			if !slices.Equal(before, e.tree()) {
				t.Errorf("the tree changed: %v -> %v", before, e.tree())
			}
		})
	}
}

func TestMoveAcrossFileSystems(t *testing.T) {
	e := newOpsEnv(t)
	e.put("x/y.txt=hello", "d/")
	calls := 0
	e.ops.opts.Rename = func(oldPath, newPath string) error {
		calls++
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
	}
	res, err := e.ops.Move(ctx(), e.p("d"), []string{e.p("x")}, MoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e.wantTree("d/", "d/x/", "d/x/y.txt")
	if calls != 1 || e.read("d/x/y.txt") != "hello" || len(res.Moves) != 1 {
		t.Errorf("calls %d, content %q, moves %v", calls, e.read("d/x/y.txt"), res.Moves)
	}
	// undo goes the same way
	if _, err := e.ops.Undo(ctx()); err != nil {
		t.Fatal(err)
	}
	e.wantTree("d/", "x/", "x/y.txt")
	if calls != 2 {
		t.Errorf("rename calls = %d", calls)
	}
}

func TestMoveFailureIsTagged(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt", "d/")
	e.ops.opts.Rename = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EACCES}
	}
	_, err := e.ops.Move(ctx(), e.p("d"), []string{e.p("a.txt")}, MoveOptions{})
	wantTag(t, err, "FileOps.rename.FileOpMoveFailed")
}

func TestMoveFailureKeepsTheReplacedItem(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt=new", "d/a.txt=old")
	e.ops.opts.Rename = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EACCES}
	}
	_, err := e.ops.Move(ctx(), e.p("d"), []string{e.p("a.txt")}, MoveOptions{Overwrite: true})
	wantTag(t, err, "FileOpMoveFailed")
	if got := e.read("d/a.txt"); got != "old" {
		t.Errorf("the item in the way was lost: %q", got)
	}
}

func TestConflicts(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt", "b.txt", "d/a.txt")
	got := e.ops.Conflicts(e.p("d"), []string{e.p("a.txt"), e.p("b.txt")})
	if !slices.Equal(got, []string{e.p("a.txt")}) {
		t.Errorf("conflicts = %v", got)
	}
}

func TestCopy(t *testing.T) {
	tests := []struct {
		name   string
		setup  []string
		srcs   []string
		dst    string
		naming IncrementalNaming
		over   bool
		want   []string
	}{
		{"into another folder keeps the name", []string{"x.txt", "d/"}, []string{"x.txt"}, "d", NamingSimple, false, []string{"x.txt", "d/", "d/x.txt"}},
		{"simple: into the same folder", []string{"x.txt"}, []string{"x.txt"}, "", NamingSimple, false, []string{"x.txt", "x copy.txt"}},
		{"default naming is simple", []string{"x.txt"}, []string{"x.txt"}, "", "", false, []string{"x.txt", "x copy.txt"}},
		{"simple: the second paste", []string{"x.txt", "x copy.txt"}, []string{"x.txt"}, "", NamingSimple, false, []string{"x.txt", "x copy.txt", "x copy 2.txt"}},
		{"simple: a copy of a copy", []string{"x.txt", "x copy.txt"}, []string{"x copy.txt"}, "", NamingSimple, false, []string{"x.txt", "x copy.txt", "x copy 2.txt"}},
		{"simple: a folder", []string{"d/f.txt"}, []string{"d"}, "", NamingSimple, false, []string{"d/", "d/f.txt", "d copy/", "d copy/f.txt"}},
		{"smart", []string{"x.txt"}, []string{"x.txt"}, "", NamingSmart, false, []string{"x.txt", "x.1.txt"}},
		{"smart: the second", []string{"x.txt", "x.1.txt"}, []string{"x.txt"}, "", NamingSmart, false, []string{"x.txt", "x.1.txt", "x.2.txt"}},
		{"disabled: other folder", []string{"x.txt", "d/"}, []string{"x.txt"}, "d", NamingDisabled, false, []string{"x.txt", "d/", "d/x.txt"}},
		{"disabled: overwrite", []string{"x.txt=new", "d/x.txt=old"}, []string{"x.txt"}, "d", NamingDisabled, true, []string{"x.txt", "d/", "d/x.txt"}},
		{"several", []string{"a", "b", "d/"}, []string{"a", "b"}, "d", NamingSimple, false, []string{"a", "b", "d/", "d/a", "d/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpsEnv(t)
			e.put(tt.setup...)
			var srcs []string
			for _, s := range tt.srcs {
				srcs = append(srcs, e.p(s))
			}
			dst := e.root
			if tt.dst != "" {
				dst = e.p(tt.dst)
			}
			res, err := e.ops.Copy(ctx(), dst, srcs, CopyOptions{Naming: tt.naming, Overwrite: tt.over})
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, w := range tt.want {
				want = append(want, strings.SplitN(w, "=", 2)[0])
			}
			e.wantTree(want...)
			if len(res.Created) != len(srcs) || len(res.Targets) != len(srcs) {
				t.Errorf("result = %+v", res)
			}
		})
	}
}

func TestRepeatedPastesMakeIncrementingNames(t *testing.T) {
	e := newOpsEnv(t)
	e.put("x.txt")
	var made []string
	for range 4 {
		res, err := e.ops.Copy(ctx(), e.root, []string{e.p("x.txt")}, CopyOptions{})
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, filepath.Base(res.Targets[0]))
	}
	want := []string{"x copy.txt", "x copy 2.txt", "x copy 3.txt", "x copy 4.txt"}
	if !slices.Equal(made, want) {
		t.Errorf("names = %v, want %v", made, want)
	}
}

func TestCopyKeepsContentModeAndLinks(t *testing.T) {
	e := newOpsEnv(t)
	e.put("src/a.txt=hello", "src/sub/b.sh=#!/bin/sh", "dst/")
	if err := os.Chmod(e.p("src/sub/b.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", e.p("src/link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", e.p("src/dirlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ops.Copy(ctx(), e.p("dst"), []string{e.p("src")}, CopyOptions{}); err != nil {
		t.Fatal(err)
	}
	if e.read("dst/src/a.txt") != "hello" || e.read("dst/src/sub/b.sh") != "#!/bin/sh" {
		t.Error("content differs")
	}
	if info, _ := os.Stat(e.p("dst/src/sub/b.sh")); info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", info.Mode())
	}
	for _, name := range []string{"link", "dirlink"} {
		target, err := os.Readlink(e.p("dst/src/" + name))
		if err != nil {
			t.Fatalf("%s is not a link: %v", name, err)
		}
		want, _ := os.Readlink(e.p("src/" + name))
		if target != want {
			t.Errorf("%s -> %s, want %s", name, target, want)
		}
	}
}

func TestCopyErrors(t *testing.T) {
	tests := []struct {
		name   string
		setup  []string
		src    string
		dst    string
		naming IncrementalNaming
		route  string
		fifo   bool
	}{
		{"missing", nil, "nope", "", NamingSimple, "FileOps.Copy->FileOps.copyItem.FileOpSourceMissing", false},
		{"into itself", []string{"d/sub/"}, "d", "d/sub", NamingSimple, "FileOps.copyItem.FileOpCopyIntoItself", false},
		{"disabled: name taken", []string{"x.txt", "d/x.txt"}, "x.txt", "d", NamingDisabled, "FileOps.copyItem.FileOpDestinationExists", false},
		{"disabled: over itself", []string{"x.txt"}, "x.txt", "", NamingDisabled, "FileOps.copyItem.FileOpCopySame", false},
		{"a pipe", nil, "pipe", "", NamingSimple, "FileOps.copyItem->copyTree.FileOpCopyUnsupported", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpsEnv(t)
			e.put(tt.setup...)
			if tt.fifo {
				if err := syscall.Mkfifo(e.p("pipe"), 0o644); err != nil {
					t.Skipf("no fifo: %v", err)
				}
			}
			dst := e.root
			if tt.dst != "" {
				dst = e.p(tt.dst)
			}
			before := e.tree()
			_, err := e.ops.Copy(ctx(), dst, []string{e.p(tt.src)}, CopyOptions{Naming: tt.naming})
			wantTag(t, err, tt.route)
			if !slices.Equal(before, e.tree()) {
				t.Errorf("a failed copy changed the tree: %v -> %v", before, e.tree())
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		e := newOpsEnv(t)
		e.put("x.txt")
		c, cancel := context.WithCancel(ctx())
		cancel()
		_, err := e.ops.Copy(c, e.root, []string{e.p("x.txt")}, CopyOptions{})
		wantTag(t, err, "FileOps.copyItem.FileOpCancelled")
	})
	t.Run("an unreadable file", func(t *testing.T) {
		e := newOpsEnv(t)
		e.put("d/secret.txt", "dst/")
		if err := os.Chmod(e.p("d/secret.txt"), 0); err != nil {
			t.Fatal(err)
		}
		if f, err := os.Open(e.p("d/secret.txt")); err == nil {
			_ = f.Close()
			t.Skip("the file can be read anyway (running as root)")
		}
		_, err := e.ops.Copy(ctx(), e.p("dst"), []string{e.p("d")}, CopyOptions{})
		wantTag(t, err, "copyTree->copyDir->copyTree->copyFile.FileOpCopyFailed")
		if exists(e.p("dst/d")) {
			t.Error("the partial copy was left")
		}
	})
	t.Run("too deep", func(t *testing.T) {
		e := newOpsEnv(t)
		deep := e.root
		for range maxCopyDepth + 2 {
			deep = filepath.Join(deep, "d")
		}
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Skipf("path too long: %v", err)
		}
		e.put("dst/")
		_, err := e.ops.Copy(ctx(), e.p("dst"), []string{e.p("d")}, CopyOptions{})
		wantTag(t, err, "FileOpCopyTooDeep")
		if _, serr := os.Lstat(e.p("dst/d")); serr == nil {
			t.Error("the partial copy was left")
		}
	})
}

func TestTrashAndRestore(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt=hello", "d/x.txt")
	res, err := e.ops.Trash(ctx(), []string{e.p("a.txt"), e.p("d")})
	if err != nil {
		t.Fatal(err)
	}
	e.wantTree()
	if !slices.Equal(res.Removed, []string{e.p("a.txt"), e.p("d")}) {
		t.Errorf("removed = %v", res.Removed)
	}
	info, err := os.ReadFile(filepath.Join(e.trash, "info", "a.txt.trashinfo"))
	if err != nil {
		t.Fatal(err)
	}
	want := "[Trash Info]\nPath=" + e.p("a.txt") + "\nDeletionDate=2026-10-23T14:05:09\n"
	if string(info) != want {
		t.Errorf("trashinfo = %q, want %q", info, want)
	}
	if b, _ := os.ReadFile(filepath.Join(e.trash, "files", "a.txt")); string(b) != "hello" {
		t.Errorf("trashed content = %q", b)
	}
	if e.ops.UndoLabel() != "Delete 2 files" {
		t.Errorf("label = %q", e.ops.UndoLabel())
	}

	undone, err := e.ops.Undo(ctx())
	if err != nil {
		t.Fatal(err)
	}
	e.wantTree("a.txt", "d/", "d/x.txt")
	if e.read("a.txt") != "hello" || len(undone.Created) != 2 {
		t.Errorf("restored content %q, result %+v", e.read("a.txt"), undone)
	}
	if exists(filepath.Join(e.trash, "info", "a.txt.trashinfo")) {
		t.Error("the trashinfo was left after the restore")
	}
	redone, err := e.ops.Redo(ctx())
	if err != nil {
		t.Fatal(err)
	}
	e.wantTree()
	if len(redone.Removed) != 2 {
		t.Errorf("redo result %+v", redone)
	}
}

func TestTrashErrors(t *testing.T) {
	t.Run("no trash", func(t *testing.T) {
		e := newOpsEnv(t)
		e.put("a.txt")
		e.ops.opts.Trash = nil
		_, err := e.ops.Trash(ctx(), []string{e.p("a.txt")})
		wantTag(t, err, "FileOps.Trash.FileOpTrashUnavailable")
		e.wantTree("a.txt")
	})
	t.Run("relative path", func(t *testing.T) {
		e := newOpsEnv(t)
		_, err := e.ops.Trash(ctx(), []string{"a.txt"})
		wantTag(t, err, "FileOps.Trash->checkPath.FileOpPathInvalid")
	})
	t.Run("missing item", func(t *testing.T) {
		e := newOpsEnv(t)
		_, err := e.ops.Trash(ctx(), []string{e.p("nope")})
		wantTag(t, err, "HomeTrash.Trash.TrashSourceMissing")
	})
	t.Run("a trash that cannot be made", func(t *testing.T) {
		e := newOpsEnv(t)
		e.put("a.txt")
		blocker := e.p("blocker")
		e.put("blocker")
		e.ops.opts.Trash = fileio.NewHomeTrash(filepath.Join(blocker, "Trash"))
		_, err := e.ops.Trash(ctx(), []string{e.p("a.txt")})
		wantTag(t, err, "HomeTrash.Trash.TrashFailed")
		if !exists(e.p("a.txt")) {
			t.Error("the item was lost")
		}
	})
}

func TestDelete(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt", "d/x.txt", "keep")
	res, err := e.ops.Delete(ctx(), []string{e.p("a.txt"), e.p("d")})
	if err != nil {
		t.Fatal(err)
	}
	e.wantTree("keep")
	if len(res.Removed) != 2 || e.ops.CanUndo() {
		t.Errorf("result %+v, CanUndo %v", res, e.ops.CanUndo())
	}
	if entries, _ := os.ReadDir(filepath.Join(e.trash, "files")); len(entries) != 0 {
		t.Errorf("a permanent delete used the trash: %v", entries)
	}
	_, err = e.ops.Delete(ctx(), []string{e.p("a.txt")})
	wantTag(t, err, "FileOps.Delete->Delete->deleteWith.FileNotFound")
	_, err = e.ops.Delete(ctx(), []string{"rel"})
	wantTag(t, err, "FileOps.Delete->Delete->deleteWith.DeletePathInvalid")
}

// TestUndoRedo does each operation, undoes it, does it again, and undoes it again.
func TestUndoRedo(t *testing.T) {
	tests := []struct {
		name   string
		setup  []string
		do     func(e *opsEnv) (OpResult, error)
		after  []string
		before []string
		check  func(t *testing.T, e *opsEnv, undone, redone OpResult)
	}{
		{
			name: "create file", setup: nil,
			do:     func(e *opsEnv) (OpResult, error) { return e.ops.CreateFile(ctx(), e.root, "n.txt") },
			before: nil, after: []string{"n.txt"},
			check: func(t *testing.T, _ *opsEnv, undone, redone OpResult) {
				if len(undone.Removed) != 1 || len(redone.Created) != 1 {
					t.Errorf("undone %+v redone %+v", undone, redone)
				}
			},
		},
		{
			name:  "create nested folder",
			do:    func(e *opsEnv) (OpResult, error) { return e.ops.CreateFolder(ctx(), e.root, "a/b") },
			after: []string{"a/", "a/b/"},
		},
		{
			name: "rename", setup: []string{"a.txt=hello"},
			do:     func(e *opsEnv) (OpResult, error) { return e.ops.Rename(ctx(), e.p("a.txt"), "b.txt") },
			before: []string{"a.txt"}, after: []string{"b.txt"},
			check: func(t *testing.T, e *opsEnv, undone, redone OpResult) {
				if undone.Moves[0].Old != e.p("b.txt") || undone.Moves[0].New != e.p("a.txt") || redone.Moves[0].New != e.p("b.txt") {
					t.Errorf("undone %+v redone %+v", undone.Moves, redone.Moves)
				}
			},
		},
		{
			name: "move", setup: []string{"a.txt", "d/"},
			do: func(e *opsEnv) (OpResult, error) {
				return e.ops.Move(ctx(), e.p("d"), []string{e.p("a.txt")}, MoveOptions{})
			},
			before: []string{"a.txt", "d/"}, after: []string{"d/", "d/a.txt"},
		},
		{
			name: "move with overwrite", setup: []string{"a.txt=new", "d/a.txt=old"},
			do: func(e *opsEnv) (OpResult, error) {
				return e.ops.Move(ctx(), e.p("d"), []string{e.p("a.txt")}, MoveOptions{Overwrite: true})
			},
			before: []string{"a.txt", "d/", "d/a.txt"}, after: []string{"d/", "d/a.txt"},
			check: func(t *testing.T, e *opsEnv, _, _ OpResult) {
				// after the last undo the old file is back and the moved one is where it was
				if e.read("d/a.txt") != "new" {
					t.Errorf("content after redo = %q", e.read("d/a.txt"))
				}
			},
		},
		{
			name: "copy", setup: []string{"x.txt"},
			do: func(e *opsEnv) (OpResult, error) {
				return e.ops.Copy(ctx(), e.root, []string{e.p("x.txt")}, CopyOptions{})
			},
			before: []string{"x.txt"}, after: []string{"x.txt", "x copy.txt"},
		},
		{
			name: "copy folder", setup: []string{"d/f.txt"},
			do:     func(e *opsEnv) (OpResult, error) { return e.ops.Copy(ctx(), e.root, []string{e.p("d")}, CopyOptions{}) },
			before: []string{"d/", "d/f.txt"}, after: []string{"d/", "d/f.txt", "d copy/", "d copy/f.txt"},
		},
		{
			name: "copy over with disabled naming", setup: []string{"x.txt=new", "d/x.txt=old"},
			do: func(e *opsEnv) (OpResult, error) {
				return e.ops.Copy(ctx(), e.p("d"), []string{e.p("x.txt")}, CopyOptions{Naming: NamingDisabled, Overwrite: true})
			},
			before: []string{"x.txt", "d/", "d/x.txt"}, after: []string{"x.txt", "d/", "d/x.txt"},
			check: func(t *testing.T, e *opsEnv, _, _ OpResult) {
				if e.read("d/x.txt") != "new" {
					t.Errorf("content after redo = %q", e.read("d/x.txt"))
				}
			},
		},
		{
			name: "trash", setup: []string{"a.txt", "d/x"},
			do:     func(e *opsEnv) (OpResult, error) { return e.ops.Trash(ctx(), []string{e.p("a.txt"), e.p("d")}) },
			before: []string{"a.txt", "d/", "d/x"}, after: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOpsEnv(t)
			e.put(tt.setup...)
			before := e.tree()
			if _, err := e.ops.Redo(ctx()); err == nil {
				t.Fatal("redo with nothing to redo")
			} else {
				wantTag(t, err, "FileOps.Redo.FileOpNothingToRedo")
			}
			if _, err := e.ops.Undo(ctx()); err == nil {
				t.Fatal("undo with nothing to undo")
			} else {
				wantTag(t, err, "FileOps.Undo.FileOpNothingToUndo")
			}
			if _, err := tt.do(e); err != nil {
				t.Fatal(err)
			}
			after := e.tree()
			if tt.after != nil || tt.before != nil {
				slices.Sort(tt.after)
				if !slices.Equal(after, append([]string(nil), tt.after...)) && tt.after != nil {
					t.Fatalf("after = %v, want %v", after, tt.after)
				}
			}
			if !e.ops.CanUndo() || e.ops.CanRedo() {
				t.Fatalf("CanUndo %v CanRedo %v", e.ops.CanUndo(), e.ops.CanRedo())
			}
			undone, err := e.ops.Undo(ctx())
			if err != nil {
				t.Fatal(err)
			}
			if got := e.tree(); !slices.Equal(got, before) {
				t.Fatalf("after undo = %v, want %v", got, before)
			}
			if e.ops.CanUndo() || !e.ops.CanRedo() {
				t.Fatalf("CanUndo %v CanRedo %v after undo", e.ops.CanUndo(), e.ops.CanRedo())
			}
			redone, err := e.ops.Redo(ctx())
			if err != nil {
				t.Fatal(err)
			}
			if got := e.tree(); !slices.Equal(got, after) {
				t.Fatalf("after redo = %v, want %v", got, after)
			}
			if tt.check != nil {
				tt.check(t, e, undone, redone)
			}
			// and once more
			if _, err := e.ops.Undo(ctx()); err != nil {
				t.Fatal(err)
			}
			if got := e.tree(); !slices.Equal(got, before) {
				t.Fatalf("after the second undo = %v, want %v", got, before)
			}
		})
	}
}

func TestUndoRestoresTheOverwrittenFile(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt=new", "d/a.txt=old")
	if _, err := e.ops.Move(ctx(), e.p("d"), []string{e.p("a.txt")}, MoveOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if e.read("d/a.txt") != "new" {
		t.Fatal("not moved")
	}
	if _, err := e.ops.Undo(ctx()); err != nil {
		t.Fatal(err)
	}
	if e.read("a.txt") != "new" || e.read("d/a.txt") != "old" {
		t.Errorf("after undo: a.txt=%q d/a.txt=%q", e.read("a.txt"), e.read("d/a.txt"))
	}
}

func TestUndoErrors(t *testing.T) {
	t.Run("something took the old path", func(t *testing.T) {
		e := newOpsEnv(t)
		e.put("a.txt")
		if _, err := e.ops.Rename(ctx(), e.p("a.txt"), "b.txt"); err != nil {
			t.Fatal(err)
		}
		e.put("a.txt=other")
		_, err := e.ops.Undo(ctx())
		wantTag(t, err, "FileOps.undoAction.FileOpDestinationExists")
		if !e.ops.CanUndo() {
			t.Error("the failed undo left the history")
		}
	})
	t.Run("the trashed item is gone", func(t *testing.T) {
		e := newOpsEnv(t)
		e.put("a.txt")
		if _, err := e.ops.Trash(ctx(), []string{e.p("a.txt")}); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(e.trash, "files")); err != nil {
			t.Fatal(err)
		}
		_, err := e.ops.Undo(ctx())
		wantTag(t, err, "HomeTrash.Restore.TrashItemMissing")
	})
	t.Run("the original path is taken", func(t *testing.T) {
		e := newOpsEnv(t)
		e.put("a.txt")
		if _, err := e.ops.Trash(ctx(), []string{e.p("a.txt")}); err != nil {
			t.Fatal(err)
		}
		e.put("a.txt=new")
		_, err := e.ops.Undo(ctx())
		wantTag(t, err, "HomeTrash.Restore.RestoreTargetExists")
	})
	t.Run("redo onto a taken path", func(t *testing.T) {
		e := newOpsEnv(t)
		if _, err := e.ops.CreateFile(ctx(), e.root, "n.txt"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.ops.Undo(ctx()); err != nil {
			t.Fatal(err)
		}
		e.put("n.txt=mine")
		_, err := e.ops.Redo(ctx())
		wantTag(t, err, "FileOps.redoAction.FileOpDestinationExists")
		if e.read("n.txt") != "mine" {
			t.Error("the file in the way was changed")
		}
	})
}

func TestANewOperationClearsTheRedoHistory(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt")
	if _, err := e.ops.Rename(ctx(), e.p("a.txt"), "b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ops.Undo(ctx()); err != nil {
		t.Fatal(err)
	}
	if !e.ops.CanRedo() || e.ops.RedoLabel() != "Rename a.txt to b.txt" {
		t.Fatalf("redo label = %q", e.ops.RedoLabel())
	}
	if _, err := e.ops.CreateFile(ctx(), e.root, "c.txt"); err != nil {
		t.Fatal(err)
	}
	if e.ops.CanRedo() {
		t.Error("the redo history stayed")
	}
	e.ops.ClearHistory()
	if e.ops.CanUndo() {
		t.Error("ClearHistory left the history")
	}
}

func TestHistoryIsBounded(t *testing.T) {
	e := newOpsEnv(t)
	for i := range maxUndoRecords + 5 {
		if _, err := e.ops.CreateFile(ctx(), e.root, "f"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+strings.Repeat("y", i/26)); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for e.ops.CanUndo() {
		e.ops.mu.Lock()
		e.ops.undo = e.ops.undo[:len(e.ops.undo)-1]
		e.ops.mu.Unlock()
		n++
	}
	if n != maxUndoRecords {
		t.Errorf("the history kept %d operations, want %d", n, maxUndoRecords)
	}
}

func TestUndoOfDeletedForGoodWithoutTrash(t *testing.T) {
	e := newOpsEnv(t)
	e.ops.opts.Trash = nil
	if _, err := e.ops.CreateFolder(ctx(), e.root, "a/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ops.Undo(ctx()); err != nil {
		t.Fatal(err)
	}
	e.wantTree()
	if _, err := e.ops.Redo(ctx()); err != nil {
		t.Fatal(err)
	}
	e.wantTree("a/", "a/b/")
}

func TestOpResultFollowAndEvents(t *testing.T) {
	r := OpResult{
		Moves:   []PathMove{{Old: "/w/d", New: "/w/e"}},
		Removed: []string{"/w/gone"},
		Created: []string{"/w/new"},
	}
	tests := []struct {
		path string
		want string
		gone bool
	}{
		{"/w/d", "/w/e", false},
		{"/w/d/x/y.go", "/w/e/x/y.go", false},
		{"/w/dd/x", "/w/dd/x", false},
		{"/w/gone/x", "/w/gone/x", true},
		{"/w/other", "/w/other", false},
	}
	for _, tt := range tests {
		got, gone := r.Follow(tt.path)
		if got != tt.want || gone != tt.gone {
			t.Errorf("Follow(%s) = %s, %v; want %s, %v", tt.path, got, gone, tt.want, tt.gone)
		}
	}
	events := r.Events()
	if len(events) != 3 || events[0].Kind != EventDeleted || events[1].Kind != EventRenamed || events[1].OldPath != "/w/d" || events[2].Kind != EventCreated {
		t.Errorf("events = %+v", events)
	}
	if (OpResult{}).Empty() == false || r.Empty() {
		t.Error("Empty is wrong")
	}
}

// TestEventsKeepATreeInStep applies the events of an operation to a tree that was read, as the window does.
func TestEventsKeepATreeInStep(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt", "d/x.txt")
	ws, err := Open(ctx(), e.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Root().Children(ctx()); err != nil {
		t.Fatal(err)
	}
	names := func() []string {
		var out []string
		for _, it := range ws.Root().Loaded() {
			out = append(out, it.Name())
		}
		slices.Sort(out)
		return out
	}
	steps := []struct {
		name string
		do   func() (OpResult, error)
		want []string
	}{
		{"rename", func() (OpResult, error) { return e.ops.Rename(ctx(), e.p("a.txt"), "b.txt") }, []string{"b.txt", "d"}},
		{"create", func() (OpResult, error) { return e.ops.CreateFile(ctx(), e.root, "c.txt") }, []string{"b.txt", "c.txt", "d"}},
		{"trash", func() (OpResult, error) { return e.ops.Trash(ctx(), []string{e.p("d")}) }, []string{"b.txt", "c.txt"}},
		{"undo trash", func() (OpResult, error) { return e.ops.Undo(ctx()) }, []string{"b.txt", "c.txt", "d"}},
	}
	for _, s := range steps {
		res, err := s.do()
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		ws.Apply(ctx(), res.Events())
		ws.Apply(ctx(), res.Events()) // the watcher reports it again later, which does no harm
		if got := names(); !slices.Equal(got, s.want) {
			t.Fatalf("%s: tree = %v, want %v", s.name, got, s.want)
		}
	}
}

func TestNextUndoTellsWhenToAsk(t *testing.T) {
	e := newOpsEnv(t)
	e.put("a.txt", "b.txt", "d/")
	if _, _, ok := e.ops.NextUndo(); ok {
		t.Error("an empty history has an operation to undo")
	}
	tests := []struct {
		name   string
		do     func() error
		label  string
		prompt UndoPrompt
	}{
		{"create", func() error { _, err := e.ops.CreateFile(ctx(), e.root, "n.txt"); return err }, "Create n.txt", PromptAlways},
		{"rename", func() error { _, err := e.ops.Rename(ctx(), e.p("a.txt"), "c.txt"); return err }, "Rename a.txt to c.txt", PromptVerbose},
		{"move", func() error {
			_, err := e.ops.Move(ctx(), e.p("d"), []string{e.p("c.txt")}, MoveOptions{})
			return err
		}, "Move c.txt", PromptVerbose},
		{"paste", func() error {
			_, err := e.ops.Copy(ctx(), e.root, []string{e.p("b.txt")}, CopyOptions{})
			return err
		}, "Paste b.txt", PromptDefault},
		{"delete", func() error { _, err := e.ops.Trash(ctx(), []string{e.p("b.txt")}); return err }, "Delete b.txt", PromptNever},
	}
	for _, tt := range tests {
		if err := tt.do(); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		label, prompt, ok := e.ops.NextUndo()
		if !ok || label != tt.label || prompt != tt.prompt {
			t.Errorf("%s: NextUndo = %q, %v, %v; want %q, %v", tt.name, label, prompt, ok, tt.label, tt.prompt)
		}
	}
}
