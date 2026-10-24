package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// applyFixture is a folder with a/ (read, with b/ below it that was read and c/ that was not).
type applyFixture struct {
	root string
	w    *Workspace
	got  []Change
}

func newApplyFixture(t *testing.T) *applyFixture {
	t.Helper()
	root := tree(t, "a/b/deep.txt", "a/c/unread.txt", "a/f.txt", "z.txt", "other/o.txt")
	f := &applyFixture{root: root, w: open(t, root, nil)}
	ctx := context.Background()
	if _, err := f.w.Root().Children(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Find(filepath.Join(root, "a")).Children(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Find(filepath.Join(root, "a", "b")).Children(ctx); err != nil {
		t.Fatal(err)
	}
	f.w.OnChange(func(c Change) { f.got = append(f.got, c) })
	return f
}

func (f *applyFixture) path(parts ...string) string {
	return filepath.Join(append([]string{f.root}, parts...)...)
}

func TestApply(t *testing.T) {
	type step func(t *testing.T, f *applyFixture) []Event
	write := func(f *applyFixture, content string, parts ...string) {
		if err := os.WriteFile(f.path(parts...), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		do   step
		want []Change // Kind, Path and OldPath are compared
		then func(t *testing.T, f *applyFixture)
	}{
		{"a file created in a folder that was read is added", func(t *testing.T, f *applyFixture) []Event {
			write(f, "n", "a", "new.txt")
			return []Event{{Kind: EventCreated, Path: f.path("a", "new.txt")}}
		}, []Change{{Kind: ChangeAdded}}, func(t *testing.T, f *applyFixture) {
			if got := names(f.w.Find(f.path("a")).Loaded()); !slices.Equal(got, []string{"b", "c", "f.txt", "new.txt"}) {
				t.Errorf("a: %q", got)
			}
		}},
		{"a file created in a folder that was not read is ignored", func(t *testing.T, f *applyFixture) []Event {
			write(f, "n", "a", "c", "new.txt")
			return []Event{{Kind: EventCreated, Path: f.path("a", "c", "new.txt")}}
		}, nil, func(t *testing.T, f *applyFixture) {
			if c := f.w.Find(f.path("a", "c")); c.Resolved() {
				t.Error("the folder was read")
			}
		}},
		{"a file created with an excluded name is ignored", func(t *testing.T, f *applyFixture) []Event {
			write(f, "n", "a", ".DS_Store")
			return []Event{{Kind: EventCreated, Path: f.path("a", ".DS_Store")}}
		}, nil, nil},
		{"a path outside the folder is ignored", func(t *testing.T, f *applyFixture) []Event {
			return []Event{{Kind: EventCreated, Path: filepath.Join(filepath.Dir(f.root), "elsewhere.txt")}, {Kind: EventDeleted, Path: "/nowhere/x"}}
		}, nil, nil},
		{"a created event for a path that vanished is a no-op", func(t *testing.T, f *applyFixture) []Event {
			return []Event{{Kind: EventCreated, Path: f.path("a", "ghost.txt")}}
		}, nil, nil},
		{"a changed file is updated", func(t *testing.T, f *applyFixture) []Event {
			write(f, "longer content", "a", "f.txt")
			return []Event{{Kind: EventChanged, Path: f.path("a", "f.txt")}}
		}, []Change{{Kind: ChangeUpdated}}, func(t *testing.T, f *applyFixture) {
			if n := f.w.Find(f.path("a", "f.txt")).Size(); n != 14 {
				t.Errorf("size %d", n)
			}
		}},
		{"a changed file the tree does not have is added (replace by rename)", func(t *testing.T, f *applyFixture) []Event {
			write(f, "n", "a", "saved.txt")
			return []Event{{Kind: EventChanged, Path: f.path("a", "saved.txt")}}
		}, []Change{{Kind: ChangeAdded}}, nil},
		{"a deleted file is removed", func(t *testing.T, f *applyFixture) []Event {
			return []Event{{Kind: EventDeleted, Path: f.path("a", "f.txt")}}
		}, []Change{{Kind: ChangeRemoved}}, func(t *testing.T, f *applyFixture) {
			if f.w.Find(f.path("a", "f.txt")) != nil {
				t.Error("still there")
			}
		}},
		{"a deleted folder takes what was read below it", func(t *testing.T, f *applyFixture) []Event {
			return []Event{{Kind: EventDeleted, Path: f.path("a", "b")}}
		}, []Change{{Kind: ChangeRemoved}}, func(t *testing.T, f *applyFixture) {
			if f.w.Find(f.path("a", "b", "deep.txt")) != nil {
				t.Error("the child is still found")
			}
		}},
		{"a deleted file that was never read is ignored", func(t *testing.T, f *applyFixture) []Event {
			return []Event{{Kind: EventDeleted, Path: f.path("a", "c", "unread.txt")}}
		}, nil, nil},
		{"the folder itself deleted is reported and kept", func(t *testing.T, f *applyFixture) []Event {
			return []Event{{Kind: EventDeleted, Path: f.root}}
		}, []Change{{Kind: ChangeRemoved}}, func(t *testing.T, f *applyFixture) {
			if f.w.Root().Name() == "" || len(f.w.Root().Loaded()) == 0 {
				t.Error("the tree was emptied")
			}
		}},
		{"a rename keeps what was read below and follows the paths", func(t *testing.T, f *applyFixture) []Event {
			if err := os.Rename(f.path("a", "b"), f.path("a", "renamed")); err != nil {
				t.Fatal(err)
			}
			return []Event{{Kind: EventRenamed, OldPath: f.path("a", "b"), Path: f.path("a", "renamed"), IsDir: true}}
		}, []Change{{Kind: ChangeRenamed}}, func(t *testing.T, f *applyFixture) {
			r := f.w.Find(f.path("a", "renamed"))
			if r == nil || !r.Resolved() || r.Name() != "renamed" {
				t.Fatalf("renamed item: %v", r)
			}
			deep := f.w.Find(f.path("a", "renamed", "deep.txt"))
			if deep == nil || deep.Path() != f.path("a", "renamed", "deep.txt") {
				t.Errorf("child: %v", deep)
			}
			if f.w.Find(f.path("a", "b")) != nil {
				t.Error("the old path is still found")
			}
		}},
		{"a move to another folder that was read", func(t *testing.T, f *applyFixture) []Event {
			if err := os.Rename(f.path("a", "f.txt"), f.path("z2.txt")); err != nil {
				t.Fatal(err)
			}
			return []Event{{Kind: EventRenamed, OldPath: f.path("a", "f.txt"), Path: f.path("z2.txt")}}
		}, []Change{{Kind: ChangeRenamed}}, func(t *testing.T, f *applyFixture) {
			if f.w.Find(f.path("z2.txt")) == nil || f.w.Find(f.path("a", "f.txt")) != nil {
				t.Error("not moved")
			}
		}},
		{"a move into a folder that was not read removes it", func(t *testing.T, f *applyFixture) []Event {
			if err := os.Rename(f.path("a", "f.txt"), f.path("other", "f.txt")); err != nil {
				t.Fatal(err)
			}
			return []Event{{Kind: EventRenamed, OldPath: f.path("a", "f.txt"), Path: f.path("other", "f.txt")}}
		}, []Change{{Kind: ChangeRemoved}}, nil},
		{"a rename of something the tree does not have is an addition", func(t *testing.T, f *applyFixture) []Event {
			write(f, "n", "a", "to.txt")
			return []Event{{Kind: EventRenamed, OldPath: f.path("a", "c", "from.txt"), Path: f.path("a", "to.txt")}}
		}, []Change{{Kind: ChangeAdded}}, nil},
		{"a rename over another file replaces it", func(t *testing.T, f *applyFixture) []Event {
			if err := os.Rename(f.path("a", "f.txt"), f.path("z.txt")); err != nil {
				t.Fatal(err)
			}
			return []Event{{Kind: EventRenamed, OldPath: f.path("a", "f.txt"), Path: f.path("z.txt")}}
		}, []Change{{Kind: ChangeRenamed}}, func(t *testing.T, f *applyFixture) {
			if got := names(f.w.Root().Loaded()); !slices.Equal(got, []string{"a", "other", "z.txt"}) {
				t.Errorf("root: %q", got)
			}
		}},
		{"a rescan reads the folder again", func(t *testing.T, f *applyFixture) []Event {
			write(f, "n", "a", "lost.txt")
			return []Event{{Kind: EventRescan, Path: f.path("a")}}
		}, []Change{{Kind: ChangeRefreshed}}, func(t *testing.T, f *applyFixture) {
			if f.w.Find(f.path("a", "lost.txt")) == nil {
				t.Error("not found after the rescan")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newApplyFixture(t)
			events := tc.do(t, f)
			changes := f.w.Apply(context.Background(), events)
			kinds := func(cs []Change) []ChangeKind {
				var out []ChangeKind
				for _, c := range cs {
					out = append(out, c.Kind)
				}
				return out
			}
			var want []ChangeKind
			for _, c := range tc.want {
				want = append(want, c.Kind)
			}
			if !slices.Equal(kinds(changes), want) {
				t.Errorf("changes %+v, want kinds %v", changes, want)
			}
			if !slices.Equal(kinds(f.got), want) {
				t.Errorf("the listener heard %+v, want kinds %v", f.got, want)
			}
			if tc.then != nil {
				tc.then(t, f)
			}
		})
	}
}

func TestApplyAKindChange(t *testing.T) {
	f := newApplyFixture(t)
	// a folder becomes a file of the same name
	if err := os.RemoveAll(f.path("a", "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.path("a", "b"), []byte("now a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.w.Apply(context.Background(), []Event{{Kind: EventChanged, Path: f.path("a", "b")}})
	b := f.w.Find(f.path("a", "b"))
	if b == nil || b.IsDir() || b.Resolved() || len(b.Loaded()) != 0 {
		t.Errorf("b: dir %v resolved %v", b.IsDir(), b.Resolved())
	}
}

func TestApplyKeepsTheOrder(t *testing.T) {
	f := newApplyFixture(t)
	if err := os.WriteFile(f.path("a", "A0.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.w.Apply(context.Background(), []Event{{Kind: EventCreated, Path: f.path("a", "A0.txt")}})
	if got := names(f.w.Find(f.path("a")).Loaded()); !slices.Equal(got, []string{"b", "c", "A0.txt", "f.txt"}) {
		t.Errorf("order: %q", got)
	}
}
