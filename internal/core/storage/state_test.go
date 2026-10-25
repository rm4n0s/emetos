package storage_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// fakeInput is an editor input that serializes to its name.
type fakeInput struct {
	editorgroup.BaseInput
	name string
}

func (f *fakeInput) TypeID() string                         { return "test.input" }
func (f *fakeInput) Resource() (document.URI, bool)         { return document.URI{}, false }
func (f *fakeInput) Name() string                           { return f.name }
func (f *fakeInput) Description() string                    { return "" }
func (f *fakeInput) Title() string                          { return f.name }
func (f *fakeInput) Dirty() bool                            { return false }
func (f *fakeInput) Capabilities() editorgroup.Capabilities { return 0 }
func (f *fakeInput) Serialize() (string, bool)              { return f.name, true }
func (f *fakeInput) Matches(other editorgroup.EditorInput) bool {
	o, ok := other.(*fakeInput)
	return ok && o.name == f.name
}

// store makes a store over an isolated state folder with a clock that is moved by hand.
func newStore(t *testing.T, mutate func(*storage.StateStoreOptions)) (*storage.StateStore, *storagetest.Clock, storage.Paths) {
	t.Helper()
	paths := storagetest.Isolate(t)
	clock := storagetest.NewClock()
	opts := storage.StateStoreOptions{After: clock.AfterFunc, Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))}
	if mutate != nil {
		mutate(&opts)
	}
	s, err := storage.NewStateStore(paths, storage.DefaultProfile, opts)
	if err != nil {
		t.Fatal(err)
	}
	return s, clock, paths
}

func routeOf(t *testing.T, err error) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	return appErr
}

func TestWindowID(t *testing.T) {
	a, err := storage.WindowID("/home/u/project")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		folder string
		same   bool
	}{
		{"the same path", "/home/u/project", true},
		{"a trailing slash is the same window", "/home/u/project/", true},
		{"a dot segment is the same window", "/home/u/./project", true},
		{"another folder", "/home/u/project2", false},
		{"a path that differs in case", "/home/u/Project", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storage.WindowID(tc.folder)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 16 || strings.Trim(got, "0123456789abcdef") != "" {
				t.Errorf("id %q is not 16 hexadecimal digits", got)
			}
			if (got == a) != tc.same {
				t.Errorf("id %q, first id %q, want same = %v", got, a, tc.same)
			}
		})
	}
	if _, err := storage.WindowID("relative/dir"); err == nil {
		t.Error("a relative folder has no id")
	} else if appErr := routeOf(t, err); !appErr.HasRoute("WindowID.WindowFolderNotAbsolute") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	for range 3 {
		id, err := storage.NewEmptyWindowID()
		if err != nil || len(id) != 16 || strings.Trim(id, "0123456789abcdef") != "" {
			t.Errorf("empty window id %q, %v", id, err)
		}
	}
}

func TestWindowIDIsTheSHA256OfThePath(t *testing.T) {
	// sha256("/") = 8a5edab282632443219e051e4ade2d1d5bbc671c781051bf1437897cbdfea0f1
	got, err := storage.WindowID("/")
	if err != nil {
		t.Fatal(err)
	}
	if want := "8a5edab282632443"; got != want {
		t.Errorf("WindowID(/) = %q, want %q", got, want)
	}
}

func TestStatePaths(t *testing.T) {
	paths := storagetest.Isolate(t)
	global, err := paths.GlobalStatePath(storage.DefaultProfile)
	if err != nil || global != filepath.Join(paths.StateHome, "emetos", "state.json") {
		t.Errorf("global %q, %v", global, err)
	}
	win, err := paths.WindowStatePath(storage.DefaultProfile, "0123456789abcdef")
	if err != nil || win != filepath.Join(paths.StateHome, "emetos", "windows", "0123456789abcdef.json") {
		t.Errorf("window %q, %v", win, err)
	}
	prof, err := paths.WindowStatePath("work", "0123456789abcdef")
	if err != nil || prof != filepath.Join(paths.StateHome, "emetos", "profiles", "work", "windows", "0123456789abcdef.json") {
		t.Errorf("profile %q, %v", prof, err)
	}
	for _, id := range []string{"", "short", "../../etc/passwd0", "0123456789ABCDEF", "0123456789abcdeg"} {
		if _, err := paths.WindowStatePath(storage.DefaultProfile, id); err == nil {
			t.Errorf("id %q was accepted", id)
		} else if appErr := routeOf(t, err); !appErr.HasRoute("Paths.WindowStatePath.WindowIDInvalid") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
	}
}

func TestRecentlyOpened(t *testing.T) {
	tests := []struct {
		name        string
		do          func(r *storage.RecentlyOpened)
		folders     []string
		files       []string
		changedLast bool
	}{
		{"most recent first", func(r *storage.RecentlyOpened) {
			r.AddFolder("/a")
			r.AddFolder("/b")
			r.AddFile("/x")
			r.AddFile("/y")
		},
			[]string{"/b", "/a"}, []string{"/y", "/x"}, true},
		{"a repeat moves to the front", func(r *storage.RecentlyOpened) { r.AddFolder("/a"); r.AddFolder("/b"); r.AddFolder("/a") },
			[]string{"/a", "/b"}, nil, true},
		{"a repeat of the first changes nothing", func(r *storage.RecentlyOpened) { r.AddFile("/a"); r.AddFile("/a") },
			nil, []string{"/a"}, false},
		{"paths are cleaned", func(r *storage.RecentlyOpened) { r.AddFolder("/a/b/"); r.AddFolder("/a/./b") },
			[]string{"/a/b"}, nil, false},
		{"relative paths are not added", func(r *storage.RecentlyOpened) { r.AddFolder("a"); r.AddFile("") },
			nil, nil, false},
		{"the files git makes are not added", func(r *storage.RecentlyOpened) {
			r.AddFile("/p/.git/COMMIT_EDITMSG")
			r.AddFile("/p/.git/MERGE_MSG")
			r.AddFile("/p/.git/rebase-merge/git-rebase-todo")
			r.AddFile("/p/main.go")
		}, nil, []string{"/p/main.go"}, true},
		{"remove takes a path out of both lists", func(r *storage.RecentlyOpened) {
			r.AddFolder("/a")
			r.AddFile("/a")
			r.AddFile("/b")
			if !r.Remove("/a") || r.Remove("/none") {
				t.Error("Remove reported wrongly")
			}
		}, nil, []string{"/b"}, true},
		{"clear", func(r *storage.RecentlyOpened) { r.AddFolder("/a"); r.AddFile("/b"); r.Clear() }, nil, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r storage.RecentlyOpened
			tc.do(&r)
			if !slices.Equal(r.Folders, tc.folders) || !slices.Equal(r.Files, tc.files) {
				t.Errorf("folders %q files %q, want %q and %q", r.Folders, r.Files, tc.folders, tc.files)
			}
		})
	}

	t.Run("the lists are limited", func(t *testing.T) {
		var r storage.RecentlyOpened
		for i := range storage.MaxRecentEntries + 25 {
			r.AddFolder("/f" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + strings.Repeat("y", i/26))
			r.AddFile("/g" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + strings.Repeat("y", i/26))
		}
		if len(r.Folders) != storage.MaxRecentEntries || len(r.Files) != storage.MaxRecentEntries {
			t.Errorf("%d folders, %d files, want %d each", len(r.Folders), len(r.Files), storage.MaxRecentEntries)
		}
		last := "/f" + strings.Repeat("x", (storage.MaxRecentEntries+24)%7) + string(rune('a'+(storage.MaxRecentEntries+24)%26)) + strings.Repeat("y", (storage.MaxRecentEntries+24)/26)
		if r.Folders[0] != last {
			t.Errorf("the newest folder is %q, want %q", r.Folders[0], last)
		}
	})

	t.Run("missing paths are dropped when read, not from the list", func(t *testing.T) {
		dir := t.TempDir()
		present := filepath.Join(dir, "here")
		if err := os.WriteFile(present, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		r := storage.RecentlyOpened{Folders: []string{dir, "/does/not/exist"}, Files: []string{"/gone", present}}
		got := r.Existing(storage.DiskExists)
		if !reflect.DeepEqual(got.Folders, []string{dir}) || !reflect.DeepEqual(got.Files, []string{present}) {
			t.Errorf("existing %+v", got)
		}
		if len(r.Folders) != 2 || len(r.Files) != 2 {
			t.Errorf("the list changed: %+v", r)
		}
		if r.Empty() || !got.Clone().Existing(func(string) bool { return false }).Empty() {
			t.Error("Empty is wrong")
		}
	})
}

func sampleGrid(t *testing.T) editorgroup.SerializedGrid {
	t.Helper()
	g := editorgroup.NewGrid(editorgroup.DefaultGridOptions())
	t.Cleanup(g.Dispose)
	first := g.Active()
	a, b, c := &fakeInput{name: "a"}, &fakeInput{name: "b"}, &fakeInput{name: "c"}
	first.Model.OpenEditor(a, editorgroup.OpenOptions{Pinned: true, Active: true})
	first.Model.OpenEditor(b, editorgroup.OpenOptions{Pinned: true, Active: true, Sticky: true})
	first.Model.OpenEditor(c, editorgroup.OpenOptions{Active: true}) // the preview editor
	second, err := g.AddGroup(first, editorgroup.DirRight)
	if err != nil {
		t.Fatal(err)
	}
	second.Model.OpenEditor(&fakeInput{name: "d"}, editorgroup.OpenOptions{Pinned: true, Active: true})
	third, err := g.AddGroup(second, editorgroup.DirDown)
	if err != nil {
		t.Fatal(err)
	}
	third.Model.OpenEditor(&fakeInput{name: "e"}, editorgroup.OpenOptions{Pinned: true, Active: true})
	if err := g.Activate(second); err != nil {
		t.Fatal(err)
	}
	return g.SerializeWith(func(in editorgroup.EditorInput) json.RawMessage {
		return json.RawMessage(`{"version":1,"scrollTop":` + map[string]string{"a": "10", "b": "20", "c": "30", "d": "40", "e": "50"}[in.Name()] + `}`)
	})
}

func TestWindowStateRoundTrip(t *testing.T) {
	s, clock, paths := newStore(t, nil)
	id, err := storage.WindowID("/work/project")
	if err != nil {
		t.Fatal(err)
	}
	want := storage.WindowState{
		Folder:   "/work/project",
		Geometry: &storage.Geometry{Width: 1111, Height: 777.5, Maximized: true},
		Layout:   json.RawMessage(`{"sideBarVisible":false,"panelHeight":412}`),
		Grid:     ptr(sampleGrid(t)),
		Explorer: &storage.ExplorerState{Expanded: []string{"internal", "internal/core", "docs"}},
		Recent:   []string{"/work/project/main.go", "/work/project/go.mod"},
		Views:    map[string]json.RawMessage{"emetos.todo.list": json.RawMessage(`{"expanded":["main.go"]}`), "emetos.tasks.panel": json.RawMessage(`{"command":"go test ./..."}`)},
	}
	if err := s.SaveWindow(id, want); err != nil {
		t.Fatal(err)
	}
	if s.Pending() != true {
		t.Error("nothing waits after a save")
	}
	if _, found, err := s.LoadWindow(t.Context(), id); err != nil || found {
		t.Fatalf("the file is written before the delay: found %v, err %v", found, err)
	}
	clock.Advance(storage.DefaultStateDelay)

	// a second store over the same folders reads what the first wrote
	again, err := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{After: clock.AfterFunc})
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := again.LoadWindow(t.Context(), id)
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	want.Version = storage.StateVersion
	// RawMessage compares by bytes: both sides went through the same encoder, except the layout, which
	// the encoder compacts
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		t.Errorf("round trip differs\n got %s\nwant %s", gj, wj)
	}

	// the grid comes back as a grid: the same groups, editors, preview, sticky and active ones, with the state
	restored, err := editorgroup.RestoreGridWith(*got.Grid, editorgroup.DefaultGridOptions(), func(in editorgroup.SerializedInput) (editorgroup.EditorInput, bool) {
		if len(in.State) == 0 {
			t.Errorf("the input %q lost its state", in.Value)
		}
		return &fakeInput{name: in.Value}, true
	})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Dispose()
	if restored.Count() != 3 || restored.Active().ID() != want.Grid.Active {
		t.Errorf("%d groups, active %d", restored.Count(), restored.Active().ID())
	}
	if !reflect.DeepEqual(restored.Serialize().Root, stripState(want.Grid.Root)) {
		t.Errorf("the restored grid differs")
	}
}

func ptr[T any](v T) *T { return &v }

// stripState drops the State of the inputs, which Serialize without a function leaves out.
func stripState(nodes []editorgroup.SerializedNode) []editorgroup.SerializedNode {
	out := make([]editorgroup.SerializedNode, len(nodes))
	for i, n := range nodes {
		out[i] = editorgroup.SerializedNode{Size: n.Size, Groups: stripState(n.Groups)}
		if len(n.Groups) == 0 {
			out[i].Groups = nil
		}
		if n.Model != nil {
			m := *n.Model
			m.Editors = append([]editorgroup.SerializedInput(nil), m.Editors...)
			for j := range m.Editors {
				m.Editors[j].State = nil
			}
			out[i].Model = &m
		}
	}
	return out
}

func TestGlobalStateRoundTrip(t *testing.T) {
	s, clock, paths := newStore(t, nil)
	id, err := s.EmptyWindowID(t.Context())
	if err != nil || len(id) != 16 {
		t.Fatalf("empty window id %q, %v", id, err)
	}
	if again, err := s.EmptyWindowID(t.Context()); err != nil || again != id {
		t.Errorf("the empty window id changed: %q, %v", again, err)
	}
	err = s.UpdateGlobal(t.Context(), func(g *storage.GlobalState) bool {
		g.LastWindow = &storage.LastWindow{Folder: "/work/project"}
		g.WindowGeometry = &storage.Geometry{Width: 900, Height: 600}
		g.Recent.AddFolder("/work/project")
		g.Recent.AddFile("/work/project/main.go")
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(storage.DefaultStateDelay)

	again, err := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.LoadGlobal(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := storage.GlobalState{
		Version: storage.StateVersion, EmptyWindowID: id,
		LastWindow: &storage.LastWindow{Folder: "/work/project"}, WindowGeometry: &storage.Geometry{Width: 900, Height: 600},
		Recent: storage.RecentlyOpened{Folders: []string{"/work/project"}, Files: []string{"/work/project/main.go"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got.EmptyWindowID != id {
		t.Error("the empty window id was not kept")
	}
}

func TestAnUpdateWithACancelledContextWritesNothingOverTheFile(t *testing.T) {
	writes := 0
	s, _, _ := newStore(t, func(o *storage.StateStoreOptions) {
		o.WriteFile = func(string, []byte) error { writes++; return nil }
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := s.UpdateGlobal(ctx, func(g *storage.GlobalState) bool { return g.Recent.AddFolder("/a") })
	if err == nil {
		t.Fatal("an update without a state succeeded")
	}
	if appErr := routeOf(t, err); !appErr.HasRoute("StateStore.LoadGlobal.StateFileUnreadable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if err := s.Flush(t.Context()); err != nil || writes != 0 {
		t.Errorf("flush %v, %d writes", err, writes)
	}
	// with a context that works the state is read and the update goes on
	if err := s.UpdateGlobal(t.Context(), func(g *storage.GlobalState) bool { return g.Recent.AddFolder("/a") }); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.LoadGlobal(t.Context()); len(g.Recent.Folders) != 1 {
		t.Errorf("state %+v", g)
	}
}

func TestAnUpdateThatChangesNothingWritesNothing(t *testing.T) {
	writes := 0
	s, clock, _ := newStore(t, func(o *storage.StateStoreOptions) {
		o.WriteFile = func(string, []byte) error { writes++; return nil }
	})
	for range 3 {
		if err := s.UpdateGlobal(t.Context(), func(g *storage.GlobalState) bool { return g.Recent.AddFolder("/a") }); err != nil {
			t.Fatal(err)
		}
		clock.Advance(storage.DefaultStateDelay)
	}
	if writes != 1 {
		t.Errorf("%d writes for one change", writes)
	}
}

func TestDebouncedWrites(t *testing.T) {
	var writes []string
	s, clock, paths := newStore(t, func(o *storage.StateStoreOptions) {
		o.WriteFile = func(path string, data []byte) error {
			writes = append(writes, filepath.Base(path)+" "+string(data))
			return nil
		}
	})
	idA, _ := storage.WindowID("/a")
	idB, _ := storage.WindowID("/b")
	save := func(id, folder string) {
		t.Helper()
		if err := s.SaveWindow(id, storage.WindowState{Folder: folder}); err != nil {
			t.Fatal(err)
		}
	}

	// a burst is one write with the last state
	for i := range 10 {
		save(idA, "/a/"+string(rune('0'+i)))
		clock.Advance(50 * time.Millisecond)
	}
	if len(writes) != 0 {
		t.Fatalf("written before the delay: %v", writes)
	}
	clock.Advance(storage.DefaultStateDelay)
	if len(writes) != 1 || !strings.Contains(writes[0], idA+".json") || !strings.Contains(writes[0], `"folder":"/a/9"`) {
		t.Fatalf("writes %v, want one with the last folder", writes)
	}
	if clock.Pending() != 0 || s.Pending() {
		t.Error("a timer or bytes are left")
	}

	// a file never waits more than the delay, however long the changes go on
	writes = nil
	for range 30 {
		save(idA, "/a/again")
		clock.Advance(100 * time.Millisecond)
	}
	if len(writes) != 3 {
		t.Errorf("%d writes in 3 seconds of changes, want 3: %v", len(writes), writes)
	}

	// Flush writes at once and cancels the timer
	writes = nil
	save(idA, "/a/flushed")
	save(idB, "/b/flushed")
	if err := s.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatalf("flush wrote %v", writes)
	}
	clock.Advance(10 * storage.DefaultStateDelay)
	if len(writes) != 2 {
		t.Errorf("the timer wrote again after a flush: %v", writes)
	}
	// two windows are two files: neither writes over the other
	if !strings.Contains(strings.Join(writes, "\n"), idA+".json") || !strings.Contains(strings.Join(writes, "\n"), idB+".json") {
		t.Errorf("writes %v", writes)
	}
	// nothing to write is no write
	if err := s.Flush(t.Context()); err != nil || len(writes) != 2 {
		t.Errorf("flush with nothing waiting: %v, %d writes", err, len(writes))
	}
	_ = paths
}

func TestAFailingWriteIsRetriedAndNeverFatal(t *testing.T) {
	failing := true
	var written []string
	s, clock, _ := newStore(t, func(o *storage.StateStoreOptions) {
		o.WriteFile = func(path string, data []byte) error {
			if failing {
				return os.ErrPermission
			}
			written = append(written, string(data))
			return nil
		}
	})
	id, _ := storage.WindowID("/a")
	if err := s.SaveWindow(id, storage.WindowState{Folder: "/a/1"}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(storage.DefaultStateDelay) // fails, is logged
	if !s.Pending() {
		t.Error("the bytes of a failed write are not kept")
	}
	// the next change is written, with the latest state, once the write works
	failing = false
	if err := s.SaveWindow(id, storage.WindowState{Folder: "/a/2"}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(storage.DefaultStateDelay)
	if len(written) != 1 || !strings.Contains(written[0], `"folder":"/a/2"`) {
		t.Errorf("written %v", written)
	}

	// Flush reports the failure, and writes once it can
	failing = true
	_ = s.SaveWindow(id, storage.WindowState{Folder: "/a/3"})
	err := s.Flush(t.Context())
	if err == nil {
		t.Fatal("flush hid the failure")
	}
	if appErr := routeOf(t, err); !appErr.HasRoute("writer.flush.StateWriteFailed") || !errors.Is(err, os.ErrPermission) {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	failing = false
	if err := s.Flush(t.Context()); err != nil || len(written) != 2 {
		t.Errorf("retry: %v, written %v", err, written)
	}
}

func TestFlushWithACancelledContext(t *testing.T) {
	s, _, _ := newStore(t, nil)
	id, _ := storage.WindowID("/a")
	if err := s.SaveWindow(id, storage.WindowState{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := s.Flush(ctx)
	if err == nil {
		t.Fatal("flush with a cancelled context succeeded")
	}
	if appErr := routeOf(t, err); !appErr.HasRoute("writer.Flush.StateFlushCancelled") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if !s.Pending() {
		t.Error("the bytes were dropped")
	}
}

func TestFilesAreWrittenAtomicallyWithPrivatePermissions(t *testing.T) {
	s, _, paths := newStore(t, nil)
	id, _ := storage.WindowID("/a")
	if err := s.SaveWindow(id, storage.WindowState{Folder: "/a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateGlobal(t.Context(), func(g *storage.GlobalState) bool { return g.Recent.AddFolder("/a") }); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(paths.StateHome, "emetos")
	for _, p := range []string{filepath.Join(dir, "state.json"), filepath.Join(dir, "windows", id+".json")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s has the permissions %v, want 0600", p, info.Mode().Perm())
		}
	}
	info, err := os.Stat(filepath.Join(dir, "windows"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the windows folder: %v, %v", info, err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "windows"))
	if err != nil || len(entries) != 1 {
		t.Errorf("the windows folder has %v (a temporary file was left?), %v", entries, err)
	}
}

func TestStateFilesThatCannotBeUsed(t *testing.T) {
	tests := []struct {
		name  string
		write func(path string) error
		route string
	}{
		{"not JSON", func(p string) error { return os.WriteFile(p, []byte("{not json"), 0o600) }, "readStateFile.StateFileCorrupt"},
		{"empty", func(p string) error { return os.WriteFile(p, nil, 0o600) }, "readStateFile.StateFileCorrupt"},
		{"JSON of another shape", func(p string) error { return os.WriteFile(p, []byte(`[1,2]`), 0o600) }, "readStateFile.StateFileCorrupt"},
		{"a newer version", func(p string) error { return os.WriteFile(p, []byte(`{"version":2,"folder":"/a"}`), 0o600) }, "readStateFile.StateVersionUnsupported"},
		{"no version", func(p string) error { return os.WriteFile(p, []byte(`{"folder":"/a"}`), 0o600) }, "readStateFile.StateVersionUnsupported"},
		{"a folder in place of the file", func(p string) error { return os.MkdirAll(p, 0o700) }, "readStateFile.StateFileUnreadable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, clock, paths := newStore(t, nil)
			id, _ := storage.WindowID("/a")
			path, err := paths.WindowStatePath(storage.DefaultProfile, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := tc.write(path); err != nil {
				t.Fatal(err)
			}
			st, found, err := s.LoadWindow(t.Context(), id)
			if err == nil {
				t.Fatal("the file was used")
			}
			if appErr := routeOf(t, err); !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tc.route)
			}
			if found || st.Folder != "" {
				t.Errorf("a state came out of an unusable file: %+v", st)
			}
			if tc.name == "a folder in place of the file" {
				return // nothing can replace a folder
			}
			// the next save replaces the file
			if err := s.SaveWindow(id, storage.WindowState{Folder: "/a/new"}); err != nil {
				t.Fatal(err)
			}
			clock.Advance(storage.DefaultStateDelay)
			st, found, err = s.LoadWindow(t.Context(), id)
			if err != nil || !found || st.Folder != "/a/new" {
				t.Errorf("after the save: %+v, %v, %v", st, found, err)
			}
		})
	}

	t.Run("a global state that cannot be used is an empty one, replaced by the next update", func(t *testing.T) {
		s, clock, paths := newStore(t, nil)
		path, _ := paths.GlobalStatePath(storage.DefaultProfile)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"version":9}`), 0o600); err != nil {
			t.Fatal(err)
		}
		g, err := s.LoadGlobal(t.Context())
		if err == nil {
			t.Fatal("a newer global state was used")
		}
		if appErr := routeOf(t, err); !appErr.HasRoute("StateStore.LoadGlobal->StateStore.readGlobal->readStateFile.StateVersionUnsupported") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
		if !g.Recent.Empty() {
			t.Errorf("state %+v", g)
		}
		if err := s.UpdateGlobal(t.Context(), func(g *storage.GlobalState) bool { return g.Recent.AddFolder("/x") }); err != nil {
			t.Fatalf("an update after an unusable file: %v", err)
		}
		clock.Advance(storage.DefaultStateDelay)
		again, _ := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{})
		if g, err := again.LoadGlobal(t.Context()); err != nil || len(g.Recent.Folders) != 1 {
			t.Errorf("after the update: %+v, %v", g, err)
		}
	})
}

func TestWindowStateIsCleanedWhenRead(t *testing.T) {
	s, clock, _ := newStore(t, nil)
	id, _ := storage.WindowID("/a")
	err := s.SaveWindow(id, storage.WindowState{
		Folder:   "/a",
		Geometry: &storage.Geometry{Width: -5, Height: 600},
		Explorer: &storage.ExplorerState{Expanded: []string{"src", "../outside", "/abs", "", "src/./deep/", "."}},
		Recent:   []string{"/a/x.go", "relative.go", "/a/../b/y.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(storage.DefaultStateDelay)
	got, found, err := s.LoadWindow(t.Context(), id)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	if got.Geometry != nil {
		t.Errorf("a geometry with a negative width was kept: %+v", got.Geometry)
	}
	if want := []string{"src", "src/deep"}; !reflect.DeepEqual(got.Explorer.Expanded, want) {
		t.Errorf("expanded %q, want %q", got.Explorer.Expanded, want)
	}
	if want := []string{"/a/x.go", "/b/y.go"}; !reflect.DeepEqual(got.Recent, want) {
		t.Errorf("recent %q, want %q", got.Recent, want)
	}
}

func TestExplorerPaths(t *testing.T) {
	rel := storage.ExpandedRelative("/work/p", []string{"/work/p/z", "/work/p/a/b", "/work/p", "/elsewhere/q", "/work/p/../x"})
	if want := []string{"a/b", "z"}; !reflect.DeepEqual(rel, want) {
		t.Errorf("relative %q, want %q", rel, want)
	}
	abs := (&storage.ExplorerState{Expanded: rel}).ExpandedPaths("/work/p")
	if want := []string{"/work/p/a/b", "/work/p/z"}; !reflect.DeepEqual(abs, want) {
		t.Errorf("absolute %q, want %q", abs, want)
	}
	if got := (*storage.ExplorerState)(nil).ExpandedPaths("/work/p"); got != nil {
		t.Errorf("nil state gave %q", got)
	}
}

func TestAStateThatCannotBeEncoded(t *testing.T) {
	s, _, _ := newStore(t, nil)
	id, _ := storage.WindowID("/a")
	err := s.SaveWindow(id, storage.WindowState{Layout: json.RawMessage("{not json")})
	if err == nil {
		t.Fatal("a layout that is not JSON was saved")
	}
	if appErr := routeOf(t, err); !appErr.HasRoute("StateStore.SaveWindow->encodeState.StateEncodeFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if s.Pending() {
		t.Error("something waits to be written")
	}
}

func TestStoreOfAProfileWithAnUnsafeName(t *testing.T) {
	paths := storagetest.Isolate(t)
	if _, err := storage.NewStateStore(paths, "../evil", storage.StateStoreOptions{}); err == nil {
		t.Error("an unsafe profile was accepted")
	} else if appErr := routeOf(t, err); !appErr.HasRoute("profileDir.ProfilePathUnsafe") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	s, err := storage.NewStateStore(paths, "work", storage.StateStoreOptions{After: storagetest.NewClock().AfterFunc})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWindow("0123456789abcdef", storage.WindowState{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.StateHome, "emetos", "profiles", "work", "windows", "0123456789abcdef.json")); err != nil {
		t.Error(err)
	}
	if err := s.SaveWindow("nope", storage.WindowState{}); err == nil {
		t.Error("a bad window id was accepted")
	}
}

func TestTheWorkspaceTabsAreKeptInTheGlobalState(t *testing.T) {
	s, clock, paths := newStore(t, nil)
	list := []storage.WorkspaceTab{
		{ID: "0123456789abcdef", StateID: "aaaaaaaaaaaaaaaa", Folder: "/work/one", Profile: "Work"},
		{ID: "fedcba9876543210", StateID: "bbbbbbbbbbbbbbbb"},
	}
	// the state id of a tab with a folder is the one the folder makes
	one, err := storage.WindowID("/work/one")
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(list)
	want[0].StateID = one
	err = s.UpdateGlobal(t.Context(), func(g *storage.GlobalState) bool {
		g.WorkspaceTabs, g.ActiveWorkspaceTab = list, "fedcba9876543210"
		g.WindowGeometry = &storage.Geometry{Width: 1200, Height: 700, Maximized: true}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(storage.DefaultStateDelay)
	again, err := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.LoadGlobal(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.WorkspaceTabs, want) || got.ActiveWorkspaceTab != "fedcba9876543210" {
		t.Errorf("tabs %+v active %q, want %+v", got.WorkspaceTabs, got.ActiveWorkspaceTab, want)
	}
	if got.WindowGeometry == nil || *got.WindowGeometry != (storage.Geometry{Width: 1200, Height: 700, Maximized: true}) {
		t.Errorf("window geometry %+v", got.WindowGeometry)
	}
	// the copy that LoadGlobal gives is the caller's
	got.WorkspaceTabs[0].Folder = "/changed"
	got.WindowGeometry.Width = 1
	if again2, _ := again.LoadGlobal(t.Context()); again2.WorkspaceTabs[0].Folder != "/work/one" || again2.WindowGeometry.Width != 1200 {
		t.Error("a change of the copy reached the store")
	}
}

func TestTheWorkspaceTabsOfAFileAreCleanedWhenRead(t *testing.T) {
	_, _, paths := newStore(t, nil)
	path, err := paths.GlobalStatePath(storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	text := `{"version": 1, "activeWorkspaceTab": "not a tab", "workspaceTabs": [
		{"id": "0123456789abcdef", "folder": "/a/b/../c"},
		{"id": "0123456789abcdef", "folder": "/again"},
		{"id": "not an id"},
		{"id": "fedcba9876543210", "folder": "relative/folder"},
		{"id": "1111111111111111", "kind": "browser"},
		{"id": "2222222222222222", "kind": "workspace", "stateId": "../escape"},
		{"id": "00000000000000aa", "stateId": "00000000000000bb", "profile": "bad/name"}]}`
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.LoadGlobal(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c, _ := storage.WindowID("/a/c")
	want := []storage.WorkspaceTab{
		{ID: "0123456789abcdef", StateID: c, Folder: "/a/c"}, {ID: "2222222222222222"}, {ID: "00000000000000aa", StateID: "00000000000000bb"},
	}
	if !slices.Equal(g.WorkspaceTabs, want) {
		t.Errorf("tabs %+v, want %+v", g.WorkspaceTabs, want)
	}
	if g.ActiveWorkspaceTab != "" {
		t.Errorf("an active tab that is not in the list was kept: %q", g.ActiveWorkspaceTab)
	}
}

// A state.json written before step 9b.1 has the list of windows and the geometry of the last window: they are the
// tabs, in the same order, and the geometry of the window, and the file is written with the new fields only.
func TestAnOldStateFileIsReadAsTabs(t *testing.T) {
	s0, _, paths := newStore(t, nil)
	_ = s0
	path, err := paths.GlobalStatePath(storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	one, _ := storage.WindowID("/work/one")
	text := `{"version": 1, "emptyWindowId": "00000000000000aa",
		"lastWindow": {"folder": "/work/one"},
		"geometry": {"width": 1100, "height": 650, "maximized": true},
		"openWindows": [
			{"id": "` + one + `", "folder": "/work/one", "profile": "Work"},
			{"id": "00000000000000aa"},
			{"id": "00000000000000bb"}]}`
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	clock := storagetest.NewClock()
	s, err := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{After: clock.AfterFunc})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.LoadGlobal(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []storage.WorkspaceTab{
		{ID: one, StateID: one, Folder: "/work/one", Profile: "Work"},
		{ID: "00000000000000aa", StateID: "00000000000000aa"},
		{ID: "00000000000000bb", StateID: "00000000000000bb"},
	}
	if !slices.Equal(g.WorkspaceTabs, want) {
		t.Errorf("tabs %+v, want %+v", g.WorkspaceTabs, want)
	}
	if g.WindowGeometry == nil || *g.WindowGeometry != (storage.Geometry{Width: 1100, Height: 650, Maximized: true}) {
		t.Errorf("window geometry %+v", g.WindowGeometry)
	}
	if len(g.OpenWindows) != 0 || g.Geometry != nil {
		t.Errorf("the old fields stay: %+v %+v", g.OpenWindows, g.Geometry)
	}
	if g.LastWindow == nil || g.LastWindow.Folder != "/work/one" || g.EmptyWindowID != "00000000000000aa" {
		t.Errorf("the rest of the file was not kept: %+v", g)
	}
	// the next write has the new fields, and not the old ones
	if err := s.UpdateGlobal(t.Context(), func(g *storage.GlobalState) bool { g.ActiveWorkspaceTab = one; return true }); err != nil {
		t.Fatal(err)
	}
	clock.Advance(storage.DefaultStateDelay)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{`"workspaceTabs"`, `"activeWorkspaceTab"`, `"windowGeometry"`} {
		if !strings.Contains(string(raw), need) {
			t.Errorf("the state file has no %s: %s", need, raw)
		}
	}
	for _, gone := range []string{`"openWindows"`, `"geometry"`} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("the state file still has %s: %s", gone, raw)
		}
	}
	// a file that has both lists keeps the new one
	text = `{"version": 1, "workspaceTabs": [{"id": "00000000000000cc"}], "openWindows": [{"id": "00000000000000dd"}],
		"windowGeometry": {"width": 900, "height": 500}, "geometry": {"width": 1100, "height": 650}}`
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	both, err := storage.NewStateStore(paths, storage.DefaultProfile, storage.StateStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := both.LoadGlobal(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(g2.WorkspaceTabs) != 1 || g2.WorkspaceTabs[0].ID != "00000000000000cc" || g2.WindowGeometry.Width != 900 {
		t.Errorf("tabs %+v geometry %+v", g2.WorkspaceTabs, g2.WindowGeometry)
	}
}

func TestDeleteWindowRemovesTheStateAndWhatWaitsToBeWritten(t *testing.T) {
	s, clock, paths := newStore(t, nil)
	const id = "0123456789abcdef"
	if err := s.SaveWindow(id, storage.WindowState{Folder: "/work/one"}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(storage.DefaultStateDelay)
	path, err := paths.WindowStatePath(storage.DefaultProfile, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the state was not written: %v", err)
	}
	// a state that waits for its timer is not written after the window is gone
	if err := s.SaveWindow(id, storage.WindowState{Folder: "/work/two"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWindow(id); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * storage.DefaultStateDelay)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the state of the deleted window is there: %v", err)
	}
	// a window that has none is not an error
	if err := s.DeleteWindow(id); err != nil {
		t.Errorf("DeleteWindow of nothing: %v", err)
	}
	if _, found, err := s.LoadWindow(t.Context(), id); err != nil || found {
		t.Errorf("LoadWindow = %v, %v", found, err)
	}
}

func TestDeleteWindowErrors(t *testing.T) {
	s, _, paths := newStore(t, nil)
	if err := s.DeleteWindow("../x"); routeOf(t, err).Tag != "WindowIDInvalid" {
		t.Errorf("error %v", err)
	}
	// a state file that cannot be removed: the file is a folder with something in it
	const id = "0123456789abcdef"
	path, _ := paths.WindowStatePath(storage.DefaultProfile, id)
	if err := os.MkdirAll(filepath.Join(path, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWindow(id); !routeOf(t, err).HasRoute("StateStore.DeleteWindow.StateDeleteFailed") {
		t.Errorf("error %v", err)
	}
}
