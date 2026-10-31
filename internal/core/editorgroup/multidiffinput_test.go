package editorgroup

import (
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestMultiDiffInputNameShowsTheNumberOfFiles(t *testing.T) {
	item := func(p string) MultiDiffItem { return MultiDiffItem{Path: p, Modified: "file:///" + p} }
	tests := []struct {
		name  string
		input *MultiDiffInput
		items []MultiDiffItem
		want  string
	}{
		{"not loaded", NewMultiDiffInput("git.changes", "r/index", "Git: Staged Changes"), nil, "Git: Staged Changes"},
		{"none", NewMultiDiffInput("git.changes", "r/index", "Git: Changes"), []MultiDiffItem{}, "Git: Changes (0 files)"},
		{"one", NewMultiDiffInput("git.changes", "r/index", "Git: Changes"), []MultiDiffItem{item("a")}, "Git: Changes (1 file)"},
		{"three", NewMultiDiffInput("git.commit", "r/abc", "abc - Fix"), []MultiDiffItem{item("a"), item("b"), item("c")}, "abc - Fix (3 files)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var events []InputEventKind
			tt.input.Subscribe(func(e InputEvent) { events = append(events, e.Kind) })
			if tt.items != nil {
				tt.input.SetItems(tt.items)
				if !slices.Equal(events, []InputEventKind{InputLabel}) || !tt.input.Loaded() {
					t.Errorf("events %v loaded %v", events, tt.input.Loaded())
				}
			}
			if got := tt.input.Name(); got != tt.want {
				t.Errorf("Name = %q, want %q", got, tt.want)
			}
			if tt.input.Dirty() || tt.input.Capabilities() != 0 {
				t.Errorf("a multi-file diff editor is clean and has no capabilities")
			}
			if _, ok := tt.input.Resource(); ok {
				t.Errorf("it shows no one resource")
			}
		})
	}
}

func TestMultiDiffInputMatchesAndRestores(t *testing.T) {
	a := NewMultiDiffInput("git.changes", "r/index", "A")
	b := NewMultiDiffInput("git.changes", "r/index", "B")
	c := NewMultiDiffInput("git.changes", "r/working", "A")
	d := NewMultiDiffInput("git.commit", "r/index", "A")
	f1 := NewFixedMultiDiffInput("1", "A", []MultiDiffItem{{Path: "a"}})
	f2 := NewFixedMultiDiffInput("2", "A", []MultiDiffItem{{Path: "a"}})
	if !a.Matches(b) || a.Matches(c) || a.Matches(d) || a.Matches(f1) || f1.Matches(f2) || !f1.Matches(f1) || a.Matches(nil) || a.Matches(NewExtensionsInput()) {
		t.Errorf("Matches compares the kind and the key, or the fixed set itself")
	}
	if a.TabKey() == c.TabKey() || a.TabKey() == d.TabKey() || f1.TabKey() == f2.TabKey() {
		t.Errorf("two sets share a tab key")
	}
	value, ok := b.Serialize()
	if !ok {
		t.Fatal("Serialize failed")
	}
	back, err := ParseMultiDiffInput(value)
	if err != nil {
		t.Fatal(err)
	}
	if back.Kind() != "git.changes" || back.Key() != "r/index" || back.Name() != "B" || back.Loaded() || len(back.Items()) != 0 || !back.Matches(a) {
		t.Errorf("restored = %+v", back)
	}
	if _, ok := f1.Serialize(); ok || !f1.Fixed() || !f1.Loaded() {
		t.Errorf("a fixed set is loaded and not saved")
	}
}

func TestParseMultiDiffInputInvalid(t *testing.T) {
	for _, value := range []string{"", "not json", `{}`, `{"kind":"k"}`, `{"key":"k"}`} {
		_, err := ParseMultiDiffInput(value)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("ParseMultiDiffInput.MultiDiffInputInvalid") {
			t.Errorf("%q: error %v", value, err)
		}
	}
}

func TestMultiDiffInputFailureAndTitle(t *testing.T) {
	in := NewMultiDiffInput("k", "x", "T")
	var n int
	in.Subscribe(func(InputEvent) { n++ })
	in.SetFailure("boom")
	in.SetTitle("T")
	in.SetTitle("U")
	if in.Failure() != "boom" || in.Title() != "U" || n != 2 {
		t.Errorf("failure %q title %q events %d", in.Failure(), in.Title(), n)
	}
	in.SetItems(nil)
	if in.Failure() != "" {
		t.Errorf("an answer clears the failure")
	}
}

func TestMultiDiffItemKey(t *testing.T) {
	a := MultiDiffItem{Path: "a", Original: "o", Modified: "m"}
	if a.Key() == (MultiDiffItem{Path: "a", Original: "o", Modified: "n"}).Key() || a.Key() != (MultiDiffItem{Path: "a", Original: "o", Modified: "m", Letter: "M"}).Key() {
		t.Errorf("the key is the path and the two documents")
	}
}
