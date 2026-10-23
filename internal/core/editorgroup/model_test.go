package editorgroup

import (
	"encoding/json"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func TestOpenPositioning(t *testing.T) {
	for _, tc := range []struct {
		name string
		pos  OpenPositioning
		want []string
	}{
		{"right of the active editor", PositionRight, []string{"a", "b", "new", "c"}},
		{"left of the active editor", PositionLeft, []string{"c", "new", "b", "a"}},
		{"first", PositionFirst, []string{"new", "c", "b", "a"}},
		{"last", PositionLast, []string{"a", "b", "c", "new"}},
		{"the default is right", "", []string{"a", "b", "new", "c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			m := NewModel(1, Options{OpenPositioning: tc.pos})
			for _, id := range []string{"a", "b", "c"} {
				m.OpenEditor(h.input(id), OpenOptions{Pinned: true, Active: true})
			}
			m.SetActive(h.input("b"))
			m.OpenEditor(h.input("new"), OpenOptions{Pinned: true})
			var got []string
			for _, e := range m.Editors(Sequential, false) {
				got = append(got, e.Name())
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got %v, want %v", got, tc.want)
					break
				}
			}
		})
	}
}

func TestOpenPositioningAfterTheStickyEditors(t *testing.T) {
	h := newHarness(t)
	m := NewModel(1, Options{OpenPositioning: PositionFirst})
	m.OpenEditor(h.input("pinned"), OpenOptions{Sticky: true})
	m.OpenEditor(h.input("new"), OpenOptions{Pinned: true})
	if got := m.IndexOf(h.input("new")); got != 1 {
		t.Errorf("a new editor is at %d, want it after the sticky editor", got)
	}
}

func TestPreviewEditorRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(h *harness, m *Model) (preview string, names []string)
	}{
		{"the next preview replaces the preview", func(h *harness, m *Model) (string, []string) {
			m.OpenEditor(h.input("a"), OpenOptions{})
			m.OpenEditor(h.input("b"), OpenOptions{})
			return "b", []string{"b"}
		}},
		{"a pinned editor is kept", func(h *harness, m *Model) (string, []string) {
			m.OpenEditor(h.input("a"), OpenOptions{Pinned: true})
			m.OpenEditor(h.input("b"), OpenOptions{})
			m.OpenEditor(h.input("c"), OpenOptions{})
			return "c", []string{"a", "c"}
		}},
		{"pinning the preview keeps it", func(h *harness, m *Model) (string, []string) {
			m.OpenEditor(h.input("a"), OpenOptions{})
			m.Pin(h.input("a"))
			m.OpenEditor(h.input("b"), OpenOptions{})
			return "b", []string{"a", "b"}
		}},
		{"opening the preview with pinned pins it", func(h *harness, m *Model) (string, []string) {
			m.OpenEditor(h.input("a"), OpenOptions{})
			m.OpenEditor(h.input("a"), OpenOptions{Pinned: true})
			m.OpenEditor(h.input("b"), OpenOptions{})
			return "b", []string{"a", "b"}
		}},
		{"opening a pinned editor as a preview leaves it pinned", func(h *harness, m *Model) (string, []string) {
			m.OpenEditor(h.input("a"), OpenOptions{Pinned: true})
			m.OpenEditor(h.input("a"), OpenOptions{})
			return "", []string{"a"}
		}},
		{"closing the preview leaves none", func(h *harness, m *Model) (string, []string) {
			m.OpenEditor(h.input("a"), OpenOptions{Pinned: true})
			m.OpenEditor(h.input("b"), OpenOptions{})
			m.CloseEditor(h.input("b"), CloseUnknown, true)
			m.OpenEditor(h.input("c"), OpenOptions{})
			return "c", []string{"a", "c"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			m := h.model()
			preview, names := tc.run(h, m)
			got := ""
			if p := m.PreviewEditor(); p != nil {
				got = p.Name()
			}
			if got != preview {
				t.Errorf("the preview is %q, want %q", got, preview)
			}
			var have []string
			for _, e := range m.Editors(Sequential, false) {
				have = append(have, e.Name())
			}
			if len(have) != len(names) {
				t.Fatalf("editors %v, want %v", have, names)
			}
			for i := range have {
				if have[i] != names[i] {
					t.Errorf("editors %v, want %v", have, names)
					break
				}
			}
		})
	}
}

func TestCloseChoosesTheNextActiveEditor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options Options
		touch   []string // activated in this order after the editors a..d are opened
		close   string
		want    string
	}{
		{"the most recent one", Options{FocusRecentEditorAfterClose: true}, []string{"a", "c"}, "c", "a"},
		{"the most recent one, the first closing", Options{FocusRecentEditorAfterClose: true}, []string{"b", "a"}, "a", "b"},
		{"the one at the right", Options{FocusRecentEditorAfterClose: false}, []string{"a", "c"}, "c", "d"},
		{"the one at the left, when it was the last", Options{FocusRecentEditorAfterClose: false}, nil, "d", "c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			m := NewModel(1, tc.options)
			for _, id := range []string{"a", "b", "c", "d"} {
				m.OpenEditor(h.input(id), pinnedActive)
			}
			for _, id := range tc.touch {
				m.SetActive(h.input(id))
			}
			m.CloseEditor(h.input(tc.close), CloseUnknown, true)
			if got := m.ActiveEditor().Name(); got != tc.want {
				t.Errorf("active = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestClosingAnInactiveEditorKeepsTheActiveOne(t *testing.T) {
	h := newHarness(t)
	m := h.model()
	a, b := h.input("a"), h.input("b")
	m.OpenEditor(a, pinnedActive)
	m.OpenEditor(b, pinnedActive)
	m.CloseEditor(a, CloseUnknown, true)
	if m.ActiveEditor() != EditorInput(b) {
		t.Errorf("active = %v", name(m.ActiveEditor()))
	}
	if got := m.SetActive(h.input("missing")); got != nil {
		t.Errorf("SetActive of an unknown editor gave %v", name(got))
	}
}

func TestMRUOrderFollowsTheActivations(t *testing.T) {
	h := newHarness(t)
	m := h.model()
	for _, id := range []string{"a", "b", "c"} {
		m.OpenEditor(h.input(id), pinnedActive)
	}
	m.SetActive(h.input("a"))
	m.SetActive(h.input("b"))
	// b, a, c; a closed editor leaves the order
	m.CloseEditor(h.input("a"), CloseUnknown, true)
	want := []string{"b", "c"}
	for i, e := range m.Editors(MostRecentlyActive, false) {
		if e.Name() != want[i] {
			t.Errorf("mru[%d] = %s, want %s", i, e.Name(), want[i])
		}
	}
}

func TestModelOptionsCanChange(t *testing.T) {
	m := NewModel(7, Options{})
	if m.ID() != 7 || m.Options().OpenPositioning != PositionRight {
		t.Errorf("id %d, options %+v", m.ID(), m.Options())
	}
	m.SetOptions(Options{OpenPositioning: PositionLast, FocusRecentEditorAfterClose: true})
	if o := m.Options(); o.OpenPositioning != PositionLast || !o.FocusRecentEditorAfterClose {
		t.Errorf("options %+v", o)
	}
	if o := DefaultOptions(); o.OpenPositioning != PositionRight || !o.FocusRecentEditorAfterClose {
		t.Errorf("default options %+v", o)
	}
}

func TestChangeEventsCanBeStopped(t *testing.T) {
	h := newHarness(t)
	m := h.model()
	var n int
	stop := m.OnChange(func(ChangeEvent) { n++ })
	m.OpenEditor(h.input("a"), pinnedActive) // open and active
	if n != 2 {
		t.Fatalf("%d events, want 2", n)
	}
	stop()
	m.OpenEditor(h.input("b"), pinnedActive)
	if n != 2 {
		t.Errorf("%d events after the stop", n)
	}
}

func TestSerializedModelRoundTripsThroughJSON(t *testing.T) {
	h := newHarness(t)
	m := NewModel(3, DefaultOptions())
	for _, id := range []string{"a", "b", "c"} {
		m.OpenEditor(h.input(id), pinnedActive)
	}
	m.Stick(h.input("b"))
	m.OpenEditor(h.input("p"), OpenOptions{})
	m.SetActive(h.input("a"))
	text, err := json.Marshal(m.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	var data SerializedModel
	if err := json.Unmarshal(text, &data); err != nil {
		t.Fatal(err)
	}
	r := h.restore(data)
	if r.ID() != 3 || r.Count() != 4 || r.StickyCount() != 1 || r.ActiveEditor().Name() != "a" || r.PreviewEditor().Name() != "p" {
		t.Errorf("id %d, %d editors, %d sticky, active %v, preview %v", r.ID(), r.Count(), r.StickyCount(), name(r.ActiveEditor()), name(r.PreviewEditor()))
	}
	if got := r.EditorAt(0).Name(); got != "b" {
		t.Errorf("the sticky editor is first, got %s", got)
	}
	if r.EditorAt(9) != nil || r.EditorAt(-1) != nil {
		t.Error("EditorAt outside the editors gives nil")
	}
}

func TestShorten(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
		want  []string
	}{
		{"sibling folders", []string{"/a/b/x", "/a/b/y"}, []string{"/a/…/x", "/a/…/y"}},
		{"VS Code's example 1", []string{"a/b/c/d", "a/f/b/c/d"}, []string{"a/b/…", "…/f/…"}},
		{"VS Code's example 2", []string{"a/b", "a/b/c"}, []string{"…/b", "…/c"}},
		{"one path is itself", []string{"/home/me/src"}, []string{"/home/…/src"}},
		{"an empty path", []string{"", "x"}, []string{"./", "x"}},
		{"the home folder", []string{"~/a/x", "~/b/x"}, []string{"~/a/…", "~/b/…"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Shorten(tc.paths)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("Shorten(%v) = %q, want %q", tc.paths, got, tc.want)
					return
				}
			}
		})
	}
}

func TestTabLabels(t *testing.T) {
	mk := func(files ...string) []EditorInput {
		var out []EditorInput
		for _, f := range files {
			out = append(out, NewTextInput(document.New(document.FileURI(f), "", document.NewOptions()), TextInputOpts{Folder: "/w"}))
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		files []string
		want  []TabLabel
	}{
		{"distinct names have no description", []string{"/w/a.go", "/w/b.go"}, []TabLabel{{"a.go", "", "/w/a.go"}, {"b.go", "", "/w/b.go"}}},
		{"the same name gets the folder", []string{"/w/x/main.go", "/w/y/main.go", "/w/z.go"},
			[]TabLabel{{"main.go", "x", "/w/x/main.go"}, {"main.go", "y", "/w/y/main.go"}, {"z.go", "", "/w/z.go"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TabLabels(mk(tc.files...))
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("label %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestTextInput(t *testing.T) {
	doc := document.New(document.FileURI("/home/me/proj/sub/main.go"), "package main\n", document.NewOptions())
	in := NewTextInput(doc, TextInputOpts{Folder: "/home/me/proj", Home: "/home/me"})
	other := NewTextInput(doc, TextInputOpts{})
	var events []InputEventKind
	in.Subscribe(func(e InputEvent) { events = append(events, e.Kind) })

	for _, tc := range []struct {
		what      string
		got, want any
	}{
		{"type", in.TypeID(), TextInputTypeID},
		{"name", in.Name(), "main.go"},
		{"description", in.Description(), "sub"},
		{"title", in.Title(), "~/proj/sub/main.go"},
		{"matches an input of the same document", in.Matches(other), true},
		{"does not match a plain input", in.Matches(&testInput{id: "x"}), false},
		{"capabilities", in.Capabilities(), CapCanSplit},
		{"document", in.Document(), doc},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.what, tc.got, tc.want)
		}
	}
	if uri, ok := in.Resource(); !ok || uri != document.FileURI("/home/me/proj/sub/main.go") {
		t.Errorf("resource = %v, %v", uri, ok)
	}
	if v, ok := in.Serialize(); !ok || v != "file:///home/me/proj/sub/main.go" {
		t.Errorf("serialize = %q, %v", v, ok)
	}

	// an edit makes the document dirty once
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "y"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if !in.Dirty() || len(events) != 1 || events[0] != InputDirty {
		t.Errorf("dirty = %v, events %v", in.Dirty(), events)
	}
	// undoing everything makes it clean again
	for doc.CanUndo() {
		if _, err := doc.Undo(); err != nil {
			t.Fatal(err)
		}
	}
	if in.Dirty() || len(events) != 2 {
		t.Errorf("dirty = %v, events %v", in.Dirty(), events)
	}

	in.Dispose()
	if len(events) != 3 || events[2] != InputWillDispose {
		t.Errorf("events %v, want a will-dispose last", events)
	}
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "z"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Errorf("a disposed input reported %v", events)
	}
}

func TestTextInputOfDocumentsThatAreNotFiles(t *testing.T) {
	untitled := document.New(document.UntitledURI(2), "", document.NewOptions())
	virtual := document.New(document.VirtualURI("settings.json"), "{}", document.NewOptions())
	virtual.SetReadOnly(true)
	for _, tc := range []struct {
		name      string
		in        *TextInput
		wantName  string
		wantCaps  Capabilities
		serialize bool
	}{
		{"untitled", NewTextInput(untitled, TextInputOpts{}), "Untitled-2", CapCanSplit | CapUntitled, false},
		{"virtual and read-only", NewTextInput(virtual, TextInputOpts{}), "settings.json", CapCanSplit | CapReadonly, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.in.Name() != tc.wantName || tc.in.Description() != "" {
				t.Errorf("name %q, description %q", tc.in.Name(), tc.in.Description())
			}
			if tc.in.Capabilities() != tc.wantCaps {
				t.Errorf("capabilities %v", tc.in.Capabilities())
			}
			if _, ok := tc.in.Serialize(); ok != tc.serialize {
				t.Errorf("serialize ok = %v", ok)
			}
			if !tc.in.Capabilities().Has(CapCanSplit) {
				t.Error("a text editor can be split")
			}
		})
	}
}

func TestTextInputDescriptions(t *testing.T) {
	for _, tc := range []struct {
		name, file, folder, home, want string
	}{
		{"below the folder", "/w/a/b/f.go", "/w", "/home/me", "a/b"},
		{"in the folder", "/w/f.go", "/w", "/home/me", ""},
		{"outside the folder, in the home folder", "/home/me/x/f.go", "/w", "/home/me", "~/x"},
		{"outside both", "/etc/f.conf", "/w", "/home/me", "/etc"},
		{"no folder open", "/home/me/f.go", "", "/home/me", "~"},
		{"a folder named like the parent", "/w/..x/f.go", "/w", "", "..x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := document.New(document.FileURI(tc.file), "", document.NewOptions())
			if got := NewTextInput(doc, TextInputOpts{Folder: tc.folder, Home: tc.home}).Description(); got != tc.want {
				t.Errorf("description = %q, want %q", got, tc.want)
			}
		})
	}
}
