package editorgroup

import (
	"slices"
	"strconv"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// Tests ported from VS Code: src/vs/workbench/test/browser/parts/editor/editorGroupModel.test.ts
//
// The file has 49 tests. 32 are ported below, one of them without the parts that need diff and
// side by side inputs (contains), and 17 are skipped because they test what the Go model does not
// have:
//   - Clone Group: the model cannot be cloned (step 6.9 will need it, and ports the test then)
//   - isActive - untyped, contains() - untyped: untyped inputs (resource + options) belong to the
//     editor service, which Emetos does not have
//   - openEditor - prefers existing side by side editor if same, indexOf() - prefers direct matching
//     editor over side by side matching one: side by side inputs (SPEC §9.11 adds them later)
//   - group serialization (locked group), locked group: locking groups
//   - index, label, active: events of the group, which is not the model's in Go
//   - editor selection: selectedEditors, openEditor with inactive selection, closeEditor keeps
//     selection, setSeletion, isSelected, select invalid editor: the selection of several tabs
//   - editor transient: basics: transient editors

// testFlags switch the serialization of the test inputs off.
type testFlags struct{ disableSerialize, disableDeserialize bool }

const testInputTypeID = "testEditorInputForGroups"

// testInput is the TestEditorInput of the VS Code test: it matches an input of the same id and
// type; a non-serializable one has another type and cannot be saved. With a resource it matches
// by resource too (TestFileEditorInput).
type testInput struct {
	BaseInput
	id              string
	nonSerializable bool
	resource        *document.URI
	flags           *testFlags
	dirty           bool
}

func (t *testInput) TypeID() string {
	if t.nonSerializable {
		return testInputTypeID + "-nonSerializable"
	}
	return testInputTypeID
}
func (t *testInput) Resource() (document.URI, bool) {
	if t.resource == nil {
		return document.URI{}, false
	}
	return *t.resource, true
}
func (t *testInput) Name() string               { return t.id }
func (t *testInput) Description() string        { return "" }
func (t *testInput) Title() string              { return t.id }
func (t *testInput) Dirty() bool                { return t.dirty }
func (t *testInput) Capabilities() Capabilities { return 0 }
func (t *testInput) Matches(other EditorInput) bool {
	o, ok := other.(*testInput)
	if !ok || o == nil {
		return false
	}
	if o.id == t.id && o.nonSerializable == t.nonSerializable {
		return true
	}
	return t.resource != nil && o.resource != nil && *t.resource == *o.resource
}
func (t *testInput) Serialize() (string, bool) {
	if t.nonSerializable || (t.flags != nil && t.flags.disableSerialize) {
		return "", false
	}
	return t.id, true
}
func (t *testInput) Dispose() { t.BaseInput.Dispose() }

func (t *testInput) setDirty() { t.Emit(InputDirty) }
func (t *testInput) setLabel() { t.Emit(InputLabel) }

// groupEvents are the events of a model, sorted as the VS Code test sorts them.
type groupEvents struct {
	opened, closed, activated, pinned, unpinned, sticky, unsticky, moved, disposed []ChangeEvent
}

func listen(m *Model) *groupEvents {
	g := &groupEvents{}
	m.OnChange(func(e ChangeEvent) {
		switch e.Kind {
		case EditorOpen:
			g.opened = append(g.opened, e)
		case EditorClose:
			g.closed = append(g.closed, e)
		case EditorActive:
			g.activated = append(g.activated, e)
		case EditorPin:
			if m.IsPinned(e.Editor) {
				g.pinned = append(g.pinned, e)
			} else {
				g.unpinned = append(g.unpinned, e)
			}
		case EditorSticky:
			if m.IsSticky(e.Editor) {
				g.sticky = append(g.sticky, e)
			} else {
				g.unsticky = append(g.unsticky, e)
			}
		case EditorMove:
			g.moved = append(g.moved, e)
		case EditorWillDispose:
			g.disposed = append(g.disposed, e)
		case EditorDirty, EditorLabel, EditorCapabilities:
		}
	})
	return g
}

// harness is the setup of one test: inputs with a counter for the ids, and the flags.
type harness struct {
	t     *testing.T
	flags *testFlags
	index int
}

func newHarness(t *testing.T) *harness { return &harness{t: t, flags: &testFlags{}, index: 1} }

// input makes an input; no id gives it the next number.
func (h *harness) input(id ...string) *testInput {
	name := ""
	if len(id) > 0 {
		name = id[0]
	} else {
		name = strconv.Itoa(h.index)
		h.index++
	}
	return &testInput{id: name, flags: h.flags}
}

func (h *harness) nonSerializable(id string) *testInput {
	return &testInput{id: id, nonSerializable: true, flags: h.flags}
}

func (h *harness) withResource(id string, uri document.URI, nonSerializable bool) *testInput {
	return &testInput{id: id, resource: &uri, nonSerializable: nonSerializable, flags: h.flags}
}

// model makes a model with the options of the VS Code test (open to the right, focus the most
// recent editor after a close).
func (h *harness) model() *Model {
	return NewModel(0, Options{OpenPositioning: PositionRight, FocusRecentEditorAfterClose: true})
}

// restore makes a model from the state of another, with the deserializer of the test inputs.
func (h *harness) restore(data SerializedModel) *Model {
	return RestoreModel(data, Options{OpenPositioning: PositionRight, FocusRecentEditorAfterClose: true}, func(typeID, value string) (EditorInput, bool) {
		if typeID != testInputTypeID || h.flags.disableDeserialize {
			return nil, false
		}
		return &testInput{id: value, flags: h.flags}, true
	})
}

func (h *harness) closeAll(m *Model) {
	for _, e := range m.Editors(Sequential, false) {
		m.CloseEditor(e, CloseUnknown, false)
	}
}

// closeEditors closes the editors but one, or only those on one side of it (-1 the left, 1 the
// right, 0 all the others).
func (h *harness) closeEditors(m *Model, except EditorInput, direction int) {
	index := m.IndexOf(except)
	if index == -1 {
		return
	}
	switch direction {
	case -1:
		for i := index - 1; i >= 0; i-- {
			m.CloseEditor(m.EditorAt(i), CloseUnknown, true)
		}
	case 1:
		for i := len(m.Editors(Sequential, false)) - 1; i > index; i-- {
			m.CloseEditor(m.EditorAt(i), CloseUnknown, true)
		}
	default:
		for _, e := range m.Editors(MostRecentlyActive, false) {
			if !e.Matches(except) {
				m.CloseEditor(e, CloseUnknown, true)
			}
		}
	}
}

var (
	pinnedActive = OpenOptions{Pinned: true, Active: true}
	previewOpen  = OpenOptions{}
)

// wantEditors checks the editors of an order.
func wantEditors(t *testing.T, got []EditorInput, want ...EditorInput) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d editors, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("editor %d is %v, want %v", i, name(got[i]), name(want[i]))
		}
	}
}

func name(e EditorInput) string {
	if e == nil {
		return "nil"
	}
	return e.Name()
}

func is(t *testing.T, what string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestPortedEditorGroupModelTests(t *testing.T) {
	seq := func(m *Model) []EditorInput { return m.Editors(Sequential, false) }
	mru := func(m *Model) []EditorInput { return m.Editors(MostRecentlyActive, false) }
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, h *harness)
	}{
		{"contains()", func(t *testing.T, h *harness) {
			// the diff and side by side parts of the VS Code test are left out
			m := h.model()
			input1, input2 := h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			is(t, "contains(input1)", m.Contains(input1), true)
			is(t, "containsStrict(input1)", m.ContainsStrict(input1), true)
			is(t, "contains(input2)", m.Contains(input2), false)
			is(t, "containsStrict(input2)", m.ContainsStrict(input2), false)
			m.OpenEditor(input2, pinnedActive)
			is(t, "contains(input1)", m.Contains(input1), true)
			is(t, "contains(input2)", m.Contains(input2), true)
			m.CloseEditor(input1, CloseUnknown, true)
			is(t, "contains(input1) after the close", m.Contains(input1), false)
			is(t, "contains(input2)", m.Contains(input2), true)
			m.CloseEditor(input2, CloseUnknown, true)
			is(t, "contains(input2) after the close", m.Contains(input2), false)

			// inputs with a resource: another resource is another editor
			input3 := h.withResource("3", document.URI{Scheme: "foo", Path: "bar"}, true)
			input4 := h.withResource("4", document.URI{Scheme: "foo", Path: "barsomething"}, true)
			m.OpenEditor(input3, pinnedActive)
			is(t, "contains(input4)", m.Contains(input4), false)
			is(t, "contains(input3)", m.Contains(input3), true)
			m.CloseEditor(input3, CloseUnknown, true)
			is(t, "contains(input3) after the close", m.Contains(input3), false)
			is(t, "count", m.Count(), 0)
		}},
		{"group serialization", func(t *testing.T, h *harness) {
			m := h.model()
			input1, input2, input3 := h.input(), h.input(), h.input()
			// inputs can be serialized and deserialized
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, OpenOptions{Active: true})
			d := h.restore(m.Serialize())
			is(t, "id", d.ID(), m.ID())
			is(t, "count", d.Count(), 3)
			is(t, "sequential", len(seq(d)), 3)
			is(t, "mru", len(mru(d)), 3)
			is(t, "input1 pinned", d.IsPinned(input1), true)
			is(t, "input2 pinned", d.IsPinned(input2), true)
			is(t, "input3 pinned", d.IsPinned(input3), false)
			is(t, "input3 active", d.IsActive(input3), true)
			// inputs cannot be serialized
			h.flags.disableSerialize = true
			d = h.restore(m.Serialize())
			is(t, "id", d.ID(), m.ID())
			is(t, "count", d.Count(), 0)
			is(t, "mru", len(mru(d)), 0)
			// inputs cannot be deserialized
			h.flags.disableSerialize, h.flags.disableDeserialize = false, true
			d = h.restore(m.Serialize())
			is(t, "id", d.ID(), m.ID())
			is(t, "count", d.Count(), 0)
			is(t, "mru", len(mru(d)), 0)
		}},
		{"group serialization (sticky editor)", func(t *testing.T, h *harness) {
			m := h.model()
			input1, input2, input3 := h.input(), h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, OpenOptions{Active: true})
			m.Stick(input2)
			is(t, "input2 sticky", m.IsSticky(input2), true)
			d := h.restore(m.Serialize())
			is(t, "id", d.ID(), m.ID())
			is(t, "count", d.Count(), 3)
			for _, c := range []struct {
				in                     EditorInput
				pinned, active, sticky bool
			}{{input1, true, false, false}, {input2, true, false, true}, {input3, false, true, false}} {
				is(t, "pinned "+c.in.Name(), d.IsPinned(c.in), c.pinned)
				is(t, "active "+c.in.Name(), d.IsActive(c.in), c.active)
				is(t, "sticky "+c.in.Name(), d.IsSticky(c.in), c.sticky)
			}
			h.flags.disableSerialize = true
			d = h.restore(m.Serialize())
			is(t, "count", d.Count(), 0)
			is(t, "sticky count", d.StickyCount(), 0)
			h.flags.disableSerialize, h.flags.disableDeserialize = false, true
			d = h.restore(m.Serialize())
			is(t, "count", d.Count(), 0)
			is(t, "sticky count", d.StickyCount(), 0)
		}},
		{"One Editor", func(t *testing.T, h *harness) {
			m := h.model()
			ev := listen(m)
			is(t, "count", m.Count(), 0)
			is(t, "mru", len(mru(m)), 0)

			// active and pinned
			input1 := h.input()
			opened, isNew := m.OpenEditor(input1, OpenOptions{Active: true, Pinned: true})
			is(t, "opened", opened, EditorInput(input1))
			is(t, "isNew", isNew, true)
			is(t, "count", m.Count(), 1)
			is(t, "mru", len(mru(m)), 1)
			found, index, _ := m.Find(input1)
			is(t, "found", found, EditorInput(input1))
			is(t, "active", m.ActiveEditor(), EditorInput(input1))
			is(t, "isActive", m.IsActive(input1), true)
			is(t, "isPinned", m.IsPinned(input1), true)
			is(t, "isPinned(0)", m.IsPinnedAt(0), true)
			is(t, "isFirst", m.IsFirst(input1), true)
			is(t, "isLast", m.IsLast(input1), true)
			is(t, "opened[0]", ev.opened[0].Editor, EditorInput(input1))
			is(t, "opened[0] index", ev.opened[0].Index, 0)
			is(t, "activated[0]", ev.activated[0].Editor, EditorInput(input1))
			is(t, "activated[0] index", ev.activated[0].Index, 0)
			is(t, "index of input1", m.IndexOf(input1), index)
			res := m.CloseEditor(input1, CloseUnpin, true)
			is(t, "close result", res.Editor, EditorInput(input1))
			is(t, "close result index", res.Index, index)
			is(t, "count", m.Count(), 0)
			is(t, "active", m.ActiveEditor(), EditorInput(nil))
			is(t, "isFirst", m.IsFirst(input1), false)
			is(t, "isLast", m.IsLast(input1), false)
			is(t, "closed[0]", ev.closed[0].Editor, EditorInput(input1))
			is(t, "closed[0] index", ev.closed[0].Index, 0)
			is(t, "closed[0] context", ev.closed[0].Context, CloseUnpin)

			// active and preview
			input2 := h.input()
			m.OpenEditor(input2, OpenOptions{Active: true})
			is(t, "count", m.Count(), 1)
			is(t, "active", m.ActiveEditor(), EditorInput(input2))
			is(t, "isPinned", m.IsPinned(input2), false)
			is(t, "isPinned(0)", m.IsPinnedAt(0), false)
			is(t, "opened[1]", ev.opened[1].Editor, EditorInput(input2))
			is(t, "activated[1]", ev.activated[1].Editor, EditorInput(input2))
			m.CloseEditor(input2, CloseUnknown, true)
			is(t, "count", m.Count(), 0)
			is(t, "active", m.ActiveEditor(), EditorInput(nil))
			is(t, "closed[1]", ev.closed[1].Editor, EditorInput(input2))
			is(t, "closed[1] is a replace", ev.closed[1].Context == CloseReplace, false)
			is(t, "close of a closed editor", m.CloseEditor(input2, CloseUnknown, true) == nil, true)
			is(t, "closed events", len(ev.closed), 2)

			// not active and pinned: it becomes active because it is the first editor
			input3 := h.input()
			m.OpenEditor(input3, OpenOptions{Pinned: true})
			is(t, "count", m.Count(), 1)
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			is(t, "isPinned", m.IsPinned(input3), true)
			is(t, "opened[2]", ev.opened[2].Editor, EditorInput(input3))
			is(t, "activated[2]", ev.activated[2].Editor, EditorInput(input3))
			m.CloseEditor(input3, CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(nil))
			is(t, "closed[2]", ev.closed[2].Editor, EditorInput(input3))
			m.CloseEditor(input3, CloseUnknown, true)
			is(t, "closed events", len(ev.closed), 3)

			// not active and preview: it becomes active because it is the first editor
			input4 := h.input()
			m.OpenEditor(input4, previewOpen)
			is(t, "count", m.Count(), 1)
			is(t, "active", m.ActiveEditor(), EditorInput(input4))
			is(t, "isPinned", m.IsPinned(input4), false)
			is(t, "opened[3]", ev.opened[3].Editor, EditorInput(input4))
			is(t, "activated[3]", ev.activated[3].Editor, EditorInput(input4))
			m.CloseEditor(input4, CloseUnknown, true)
			is(t, "count", m.Count(), 0)
			is(t, "closed[3]", ev.closed[3].Editor, EditorInput(input4))
		}},
		{"Multiple Editors - Pinned and Active", func(t *testing.T, h *harness) {
			m := h.model()
			ev := listen(m)
			input1, input1Copy, input2, input3 := h.input("1"), h.input("1"), h.input("2"), h.input("3")
			opened, isNew := m.OpenEditor(input1, pinnedActive)
			is(t, "opened", opened, EditorInput(input1))
			is(t, "isNew", isNew, true)
			// opening a copy of an editor gives the existing one
			opened, isNew = m.OpenEditor(input1Copy, pinnedActive)
			is(t, "opened copy", opened, EditorInput(input1))
			is(t, "isNew copy", isNew, false)
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, pinnedActive)
			is(t, "count", m.Count(), 3)
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			for _, c := range []struct {
				in            EditorInput
				active, first bool
				last          bool
			}{{input1, false, true, false}, {input2, false, false, false}, {input3, true, false, true}} {
				is(t, "active "+c.in.Name(), m.IsActive(c.in), c.active)
				is(t, "pinned "+c.in.Name(), m.IsPinned(c.in), true)
				is(t, "first "+c.in.Name(), m.IsFirst(c.in), c.first)
				is(t, "last "+c.in.Name(), m.IsLast(c.in), c.last)
			}
			for i, in := range []EditorInput{input1, input2, input3} {
				is(t, "opened", ev.opened[i].Editor, in)
				is(t, "activated", ev.activated[i].Editor, in)
				is(t, "activated index", ev.activated[i].Index, i)
			}
			wantEditors(t, mru(m), input3, input2, input1)

			// events carry the editor of the model for an input that matches it
			sameInput1 := h.input("1")
			m.OpenEditor(sameInput1, pinnedActive)
			is(t, "activated[3]", ev.activated[3].Editor, EditorInput(input1))
			is(t, "activated[3] index", ev.activated[3].Index, 0)
			m.Unpin(sameInput1)
			is(t, "unpinned[0]", ev.unpinned[0].Editor, EditorInput(input1))
			is(t, "unpinned[0] index", ev.unpinned[0].Index, 0)
			m.Pin(sameInput1)
			is(t, "pinned[0]", ev.pinned[0].Editor, EditorInput(input1))
			is(t, "pinned[0] index", ev.pinned[0].Index, 0)
			m.Stick(sameInput1)
			is(t, "sticky[0]", ev.sticky[0].Editor, EditorInput(input1))
			is(t, "sticky[0] index", ev.sticky[0].Index, 0)
			m.Unstick(sameInput1)
			is(t, "unsticky[0]", ev.unsticky[0].Editor, EditorInput(input1))
			is(t, "unsticky[0] index", ev.unsticky[0].Index, 0)
			m.MoveEditor(sameInput1, 1)
			is(t, "moved[0]", ev.moved[0].Editor, EditorInput(input1))
			is(t, "moved[0] old index", ev.moved[0].OldIndex, 0)
			is(t, "moved[0] index", ev.moved[0].Index, 1)
			m.CloseEditor(sameInput1, CloseUnknown, true)
			is(t, "closed[0]", ev.closed[0].Editor, EditorInput(input1))
			is(t, "closed[0] index", ev.closed[0].Index, 1)
			h.closeAll(m)
			is(t, "closed events", len(ev.closed), 3)
			is(t, "count", m.Count(), 0)
		}},
		{"Multiple Editors - Preview editor moves to the side of the active one", func(t *testing.T, h *harness) {
			m := h.model()
			input1, input2, input3, input4 := h.input(), h.input(), h.input(), h.input()
			m.OpenEditor(input1, OpenOptions{Active: true})
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, pinnedActive)
			is(t, "third", seq(m)[2], EditorInput(input3))
			m.OpenEditor(input4, OpenOptions{Active: true}) // the preview editor moves after input3
			is(t, "third", seq(m)[2], EditorInput(input4))
		}},
		{"Multiple Editors - Pinned and Active (DEFAULT_OPEN_EDITOR_DIRECTION = Direction.LEFT)", func(t *testing.T, h *harness) {
			m := NewModel(0, Options{OpenPositioning: PositionLeft})
			ev := listen(m)
			input1, input2, input3 := h.input(), h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, pinnedActive)
			wantEditors(t, seq(m), input3, input2, input1)
			h.closeAll(m)
			is(t, "closed events", len(ev.closed), 3)
			is(t, "count", m.Count(), 0)
		}},
		{"Multiple Editors - Pinned and Not Active", func(t *testing.T, h *harness) {
			m := h.model()
			input1, input2, input3 := h.input(), h.input(), h.input()
			m.OpenEditor(input1, OpenOptions{Pinned: true})
			m.OpenEditor(input2, OpenOptions{Pinned: true})
			m.OpenEditor(input3, OpenOptions{Pinned: true})
			is(t, "count", m.Count(), 3)
			is(t, "active", m.ActiveEditor(), EditorInput(input1))
			for i, in := range []EditorInput{input1, input2, input3} {
				is(t, "pinned "+in.Name(), m.IsPinned(in), true)
				is(t, "pinned at", m.IsPinnedAt(i), true)
				is(t, "active "+in.Name(), m.IsActive(in), i == 0)
			}
			wantEditors(t, mru(m), input1, input3, input2)
		}},
		{"Multiple Editors - Preview gets overwritten", func(t *testing.T, h *harness) {
			m := h.model()
			ev := listen(m)
			input1, input2, input3 := h.input(), h.input(), h.input()
			m.OpenEditor(input1, previewOpen) // becomes active, preview
			m.OpenEditor(input2, previewOpen) // overwrites the preview
			m.OpenEditor(input3, previewOpen) // overwrites the preview
			is(t, "count", m.Count(), 1)
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			is(t, "pinned", m.IsPinned(input3), false)
			wantOpened := []EditorInput{input1, input2, input3}
			for i, in := range wantOpened {
				is(t, "opened", ev.opened[i].Editor, in)
			}
			is(t, "closed[0]", ev.closed[0].Editor, EditorInput(input1))
			is(t, "closed[1]", ev.closed[1].Editor, EditorInput(input2))
			is(t, "closed[0] is a replace", ev.closed[0].Context == CloseReplace, true)
			is(t, "closed[1] is a replace", ev.closed[1].Context == CloseReplace, true)
			wantEditors(t, mru(m), input3)
		}},
		{"Multiple Editors - set active", func(t *testing.T, h *harness) {
			m := h.model()
			ev := listen(m)
			input1, input2, input3 := h.input(), h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, OpenOptions{Active: true})
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			wantEditors(t, mru(m), input3, input2, input1)
			m.SetActive(input3)
			is(t, "activated", len(ev.activated), 3)
			m.SetActive(input1)
			is(t, "activated[3]", ev.activated[3].Editor, EditorInput(input1))
			is(t, "active", m.ActiveEditor(), EditorInput(input1))
			is(t, "isActive 2", m.IsActive(input2), false)
			is(t, "isActive 3", m.IsActive(input3), false)
			wantEditors(t, mru(m), input1, input3, input2)
		}},
		{"Multiple Editors - pin and unpin", func(t *testing.T, h *harness) {
			m := h.model()
			ev := listen(m)
			input1, input2, input3 := h.input(), h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, OpenOptions{Active: true})
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			is(t, "count", m.Count(), 3)

			m.Pin(input3)
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			is(t, "pinned", m.IsPinned(input3), true)
			is(t, "pinned[0]", ev.pinned[0].Editor, EditorInput(input3))
			is(t, "count", m.Count(), 3)

			m.Unpin(input1)
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			is(t, "pinned", m.IsPinned(input1), false)
			is(t, "active 1", m.IsActive(input1), false)
			is(t, "unpinned[0]", ev.unpinned[0].Editor, EditorInput(input1))
			is(t, "count", m.Count(), 3)

			m.Unpin(input2)
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			is(t, "count", m.Count(), 2) // two previews became one
			wantEditors(t, seq(m), input2, input3)
			is(t, "closed[0]", ev.closed[0].Editor, EditorInput(input1))

			m.Unpin(input3)
			is(t, "active", m.ActiveEditor(), EditorInput(input3))
			is(t, "count", m.Count(), 1) // unpinning replaced the preview
			wantEditors(t, seq(m), input3)
			is(t, "closed[1]", ev.closed[1].Editor, EditorInput(input2))
		}},
		{"Multiple Editors - closing picks next from MRU list", func(t *testing.T, h *harness) {
			m := h.model()
			ev := listen(m)
			in := make([]*testInput, 5)
			for i := range in {
				in[i] = h.input()
				m.OpenEditor(in[i], pinnedActive)
			}
			is(t, "active", m.ActiveEditor(), EditorInput(in[4]))
			is(t, "mru[0]", mru(m)[0], EditorInput(in[4]))
			is(t, "count", m.Count(), 5)

			m.CloseEditor(in[4], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(in[3]))
			is(t, "activated[5]", ev.activated[5].Editor, EditorInput(in[3]))
			is(t, "count", m.Count(), 4)

			m.SetActive(in[0])
			m.SetActive(in[3])
			m.CloseEditor(in[3], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(in[0]))
			is(t, "count", m.Count(), 3)

			m.CloseEditor(in[0], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(in[2]))
			is(t, "count", m.Count(), 2)

			m.SetActive(in[1])
			m.CloseEditor(in[1], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(in[2]))
			is(t, "count", m.Count(), 1)

			m.CloseEditor(in[2], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(nil))
			is(t, "count", m.Count(), 0)
		}},
		{"Multiple Editors - closing picks next to the right", func(t *testing.T, h *harness) {
			m := NewModel(0, Options{OpenPositioning: PositionRight, FocusRecentEditorAfterClose: false})
			ev := listen(m)
			in := make([]*testInput, 5)
			for i := range in {
				in[i] = h.input()
				m.OpenEditor(in[i], pinnedActive)
			}
			is(t, "active", m.ActiveEditor(), EditorInput(in[4]))
			is(t, "mru[0]", mru(m)[0], EditorInput(in[4]))
			is(t, "count", m.Count(), 5)

			m.CloseEditor(in[4], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(in[3]))
			is(t, "activated[5]", ev.activated[5].Editor, EditorInput(in[3]))
			is(t, "count", m.Count(), 4)

			m.SetActive(in[0])
			m.CloseEditor(in[0], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(in[1]))
			is(t, "count", m.Count(), 3)

			m.SetActive(in[2])
			m.CloseEditor(in[2], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(in[3]))
			is(t, "count", m.Count(), 2)

			m.CloseEditor(in[3], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(in[1]))
			is(t, "count", m.Count(), 1)

			m.CloseEditor(in[1], CloseUnknown, true)
			is(t, "active", m.ActiveEditor(), EditorInput(nil))
			is(t, "count", m.Count(), 0)
		}},
		{"Multiple Editors - move editor", func(t *testing.T, h *harness) {
			m := h.model()
			ev := listen(m)
			input1, input2, input3, input4, input5 := h.input(), h.input(), h.input(), h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, pinnedActive)

			m.MoveEditor(input1, 1)
			is(t, "moved[0]", ev.moved[0].Editor, EditorInput(input1))
			is(t, "moved[0] old index", ev.moved[0].OldIndex, 0)
			is(t, "moved[0] index", ev.moved[0].Index, 1)
			wantEditors(t, seq(m), input2, input1)

			m.SetActive(input1)
			m.OpenEditor(input3, pinnedActive)
			m.OpenEditor(input4, pinnedActive)
			m.OpenEditor(input5, pinnedActive)

			m.MoveEditor(input4, 0)
			is(t, "moved[1]", ev.moved[1].Editor, EditorInput(input4))
			is(t, "moved[1] old index", ev.moved[1].OldIndex, 3)
			is(t, "moved[1] index", ev.moved[1].Index, 0)
			wantEditors(t, seq(m), input4, input2, input1, input3, input5)

			m.MoveEditor(input4, 3)
			m.MoveEditor(input2, 1)
			wantEditors(t, seq(m), input1, input2, input3, input4, input5)

			is(t, "moved", len(ev.moved), 4)
			m.MoveEditor(input1, 0)
			is(t, "moved", len(ev.moved), 4)
			m.MoveEditor(input1, -1)
			is(t, "moved", len(ev.moved), 4)
			m.MoveEditor(input5, 4)
			is(t, "moved", len(ev.moved), 4)
			m.MoveEditor(input5, 100)
			is(t, "moved", len(ev.moved), 4)

			m.MoveEditor(input5, -1)
			is(t, "moved", len(ev.moved), 5)
			is(t, "first", seq(m)[0], EditorInput(input5))

			m.MoveEditor(input1, 100)
			is(t, "moved", len(ev.moved), 6)
			is(t, "last", seq(m)[4], EditorInput(input1))
		}},
		{"Multiple Editors - move editor across groups", func(t *testing.T, h *harness) {
			group1, group2 := h.model(), h.model()
			g1Input1, g1Input2, g2Input1 := h.input(), h.input(), h.input()
			group1.OpenEditor(g1Input1, OpenOptions{Active: true, Pinned: true})
			group1.OpenEditor(g1Input2, OpenOptions{Active: true, Pinned: true})
			group2.OpenEditor(g2Input1, OpenOptions{Active: true, Pinned: true})
			// a move across groups is a close in the one group and an open at an index in the other
			group2.CloseEditor(g2Input1, CloseUnknown, true)
			group1.OpenEditor(g2Input1, OpenOptions{Active: true, Pinned: true, Index: 1, HasIndex: true})
			is(t, "count", group1.Count(), 3)
			wantEditors(t, seq(group1), g1Input1, g2Input1, g1Input2)
		}},
		{"Multiple Editors - move editor across groups (input already exists in group 1)", func(t *testing.T, h *harness) {
			group1, group2 := h.model(), h.model()
			g1Input1, g1Input2, g1Input3 := h.input(), h.input(), h.input()
			g2Input1 := g1Input2
			group1.OpenEditor(g1Input1, OpenOptions{Active: true, Pinned: true})
			group1.OpenEditor(g1Input2, OpenOptions{Active: true, Pinned: true})
			group1.OpenEditor(g1Input3, OpenOptions{Active: true, Pinned: true})
			group2.OpenEditor(g2Input1, OpenOptions{Active: true, Pinned: true})
			group2.CloseEditor(g2Input1, CloseUnknown, true)
			group1.OpenEditor(g2Input1, OpenOptions{Active: true, Pinned: true, Index: 0, HasIndex: true})
			is(t, "count", group1.Count(), 3)
			wantEditors(t, seq(group1), g1Input2, g1Input1, g1Input3)
		}},
		{"Multiple Editors - Pinned & Non Active", func(t *testing.T, h *harness) {
			m := h.model()
			input1 := h.input()
			m.OpenEditor(input1, OpenOptions{})
			is(t, "active", m.ActiveEditor(), EditorInput(input1))
			is(t, "preview", m.PreviewEditor(), EditorInput(input1))
			wantEditors(t, seq(m), input1)

			input2 := h.input()
			m.OpenEditor(input2, OpenOptions{Pinned: true})
			is(t, "active", m.ActiveEditor(), EditorInput(input1))
			is(t, "preview", m.PreviewEditor(), EditorInput(input1))
			wantEditors(t, seq(m), input1, input2)

			input3 := h.input()
			m.OpenEditor(input3, OpenOptions{Pinned: true})
			is(t, "active", m.ActiveEditor(), EditorInput(input1))
			is(t, "preview", m.PreviewEditor(), EditorInput(input1))
			wantEditors(t, seq(m), input1, input3, input2)
			is(t, "pinned 1", m.IsPinned(input1), false)
			is(t, "pinned 2", m.IsPinned(input2), true)
			is(t, "pinned 3", m.IsPinned(input3), true)
			is(t, "count", m.Count(), 3)
		}},
		{"Multiple Editors - Close Others, Close Left, Close Right", func(t *testing.T, h *harness) {
			m := h.model()
			in := make([]*testInput, 5)
			for i := range in {
				in[i] = h.input()
			}
			openAll := func() {
				for _, e := range in {
					m.OpenEditor(e, OpenOptions{Active: true, Pinned: true})
				}
			}
			openAll()
			// close others
			h.closeEditors(m, m.ActiveEditor(), 0)
			is(t, "active", m.ActiveEditor(), EditorInput(in[4]))
			is(t, "count", m.Count(), 1)

			h.closeAll(m)
			openAll()
			m.SetActive(in[2])
			// close left
			is(t, "active", m.ActiveEditor(), EditorInput(in[2]))
			h.closeEditors(m, m.ActiveEditor(), -1)
			is(t, "active", m.ActiveEditor(), EditorInput(in[2]))
			is(t, "count", m.Count(), 3)
			wantEditors(t, seq(m), in[2], in[3], in[4])

			h.closeAll(m)
			openAll()
			m.SetActive(in[2])
			// close right
			is(t, "active", m.ActiveEditor(), EditorInput(in[2]))
			h.closeEditors(m, m.ActiveEditor(), 1)
			is(t, "active", m.ActiveEditor(), EditorInput(in[2]))
			is(t, "count", m.Count(), 3)
			wantEditors(t, seq(m), in[0], in[1], in[2])
		}},
		{"Multiple Editors - real user example", func(t *testing.T, h *harness) {
			m := h.model()
			// [] -> /index.html/
			indexHTML := h.input("index.html")
			opened, _ := m.OpenEditor(indexHTML, OpenOptions{})
			is(t, "opened", opened, EditorInput(indexHTML))
			is(t, "active", m.ActiveEditor(), EditorInput(indexHTML))
			is(t, "preview", m.PreviewEditor(), EditorInput(indexHTML))
			is(t, "count", m.Count(), 1)

			// /index.html/ -> /index.html/
			opened, _ = m.OpenEditor(h.input("index.html"), OpenOptions{})
			is(t, "opened", opened, EditorInput(indexHTML))
			is(t, "active", m.ActiveEditor(), EditorInput(indexHTML))
			is(t, "preview", m.PreviewEditor(), EditorInput(indexHTML))
			is(t, "count", m.Count(), 1)

			// /index.html/ -> /style.css/
			styleCSS := h.input("style.css")
			opened, _ = m.OpenEditor(styleCSS, OpenOptions{})
			is(t, "opened", opened, EditorInput(styleCSS))
			is(t, "active", m.ActiveEditor(), EditorInput(styleCSS))
			is(t, "preview", m.PreviewEditor(), EditorInput(styleCSS))
			is(t, "count", m.Count(), 1)

			// /style.css/ -> [/style.css/, test.js]
			testJS := h.input("test.js")
			opened, _ = m.OpenEditor(testJS, pinnedActive)
			is(t, "opened", opened, EditorInput(testJS))
			is(t, "preview", m.PreviewEditor(), EditorInput(styleCSS))
			is(t, "active", m.ActiveEditor(), EditorInput(testJS))
			is(t, "css pinned", m.IsPinned(styleCSS), false)
			is(t, "js pinned", m.IsPinned(testJS), true)
			wantEditors(t, seq(m), styleCSS, testJS)

			// [/style.css/, test.js] -> [test.js, /index.html/]
			indexHTML2 := h.input("index.html")
			m.OpenEditor(indexHTML2, OpenOptions{Active: true})
			is(t, "active", m.ActiveEditor(), EditorInput(indexHTML2))
			is(t, "preview", m.PreviewEditor(), EditorInput(indexHTML2))
			is(t, "html pinned", m.IsPinned(indexHTML2), false)
			is(t, "js pinned", m.IsPinned(testJS), true)
			wantEditors(t, seq(m), testJS, indexHTML2)

			// make test.js active
			testJS2 := h.input("test.js")
			m.SetActive(testJS2)
			is(t, "active", m.ActiveEditor(), EditorInput(testJS))
			is(t, "isActive", m.IsActive(testJS2), true)
			is(t, "count", m.Count(), 2)

			// [test.js, /indexHtml/] -> [test.js, index.html]
			indexHTML3 := h.input("index.html")
			m.Pin(indexHTML3)
			is(t, "pinned", m.IsPinned(indexHTML3), true)
			is(t, "active", m.ActiveEditor(), EditorInput(testJS))

			// [test.js, index.html] -> [test.js, file.ts, index.html]
			fileTS := h.input("file.ts")
			m.OpenEditor(fileTS, pinnedActive)
			is(t, "pinned", m.IsPinned(fileTS), true)
			is(t, "count", m.Count(), 3)
			is(t, "active", m.ActiveEditor(), EditorInput(fileTS))

			// [test.js, index.html, file.ts] -> [test.js, /file.ts/, index.html]
			m.Unpin(fileTS)
			is(t, "count", m.Count(), 3)
			is(t, "pinned", m.IsPinned(fileTS), false)
			is(t, "active", m.ActiveEditor(), EditorInput(fileTS))

			// [test.js, /file.ts/, index.html] -> [test.js, /other.ts/, index.html]
			otherTS := h.input("other.ts")
			m.OpenEditor(otherTS, OpenOptions{Active: true})
			is(t, "count", m.Count(), 3)
			is(t, "active", m.ActiveEditor(), EditorInput(otherTS))
			is(t, "first matches test.js", seq(m)[0].Matches(testJS), true)
			is(t, "second", seq(m)[1], EditorInput(otherTS))
			is(t, "third matches index.html", seq(m)[2].Matches(indexHTML), true)

			// make index.html active
			m.SetActive(h.input("index.html"))
			is(t, "active", m.ActiveEditor(), EditorInput(indexHTML2))

			// [test.js, /other.ts/, index.html] -> [test.js, /other.ts/]
			m.CloseEditor(indexHTML, CloseUnknown, true)
			is(t, "count", m.Count(), 2)
			is(t, "active", m.ActiveEditor(), EditorInput(otherTS))
			is(t, "first matches test.js", seq(m)[0].Matches(testJS), true)
			is(t, "second", seq(m)[1], EditorInput(otherTS))

			// [test.js, /other.ts/] -> [test.js]
			m.CloseEditor(otherTS, CloseUnknown, true)
			is(t, "count", m.Count(), 1)
			is(t, "active", m.ActiveEditor(), EditorInput(testJS))
			is(t, "first matches test.js", seq(m)[0].Matches(testJS), true)

			// [test.js] -> /test.js/
			m.Unpin(testJS)
			is(t, "count", m.Count(), 1)
			is(t, "active", m.ActiveEditor(), EditorInput(testJS))
			is(t, "pinned", m.IsPinned(testJS), false)

			// /test.js/ -> []
			m.CloseEditor(testJS, CloseUnknown, true)
			is(t, "count", m.Count(), 0)
			is(t, "active", m.ActiveEditor(), EditorInput(nil))
			is(t, "preview", m.PreviewEditor(), EditorInput(nil))
		}},
		{"Single Group, Single Editor - persist", func(t *testing.T, h *harness) {
			m := h.model()
			input1 := h.input()
			m.OpenEditor(input1, OpenOptions{})
			is(t, "count", m.Count(), 1)
			is(t, "active matches", m.ActiveEditor().Matches(input1), true)
			is(t, "preview matches", m.PreviewEditor().Matches(input1), true)
			is(t, "isActive", m.IsActive(input1), true)
			m = h.restore(m.Serialize())
			is(t, "count", m.Count(), 1)
			is(t, "active matches", m.ActiveEditor().Matches(input1), true)
			is(t, "preview matches", m.PreviewEditor().Matches(input1), true)
			is(t, "isActive", m.IsActive(input1), true)
		}},
		{"Multiple Groups, Multiple editors - persist", func(t *testing.T, h *harness) {
			group1, group2 := h.model(), h.model()
			g1 := []*testInput{h.input(), h.input(), h.input()}
			group1.OpenEditor(g1[0], OpenOptions{Active: true, Pinned: true})
			group1.OpenEditor(g1[1], OpenOptions{Active: true})
			group1.OpenEditor(g1[2], OpenOptions{Pinned: true})
			g2 := []*testInput{h.input(), h.input(), h.input()}
			group2.OpenEditor(g2[0], OpenOptions{Active: true, Pinned: true})
			group2.OpenEditor(g2[1], OpenOptions{})
			group2.OpenEditor(g2[2], OpenOptions{Pinned: true})
			check := func(group1, group2 *Model) {
				t.Helper()
				is(t, "count 1", group1.Count(), 3)
				is(t, "count 2", group2.Count(), 3)
				is(t, "active 1", group1.ActiveEditor().Matches(g1[1]), true)
				is(t, "active 2", group2.ActiveEditor().Matches(g2[0]), true)
				is(t, "preview 1", group1.PreviewEditor().Matches(g1[1]), true)
				is(t, "preview 2", group2.PreviewEditor().Matches(g2[1]), true)
				for i, want := range []*testInput{g1[1], g1[2], g1[0]} {
					is(t, "mru 1", mru(group1)[i].Matches(want), true)
				}
				for i, want := range []*testInput{g2[0], g2[2], g2[1]} {
					is(t, "mru 2", mru(group2)[i].Matches(want), true)
				}
			}
			check(group1, group2)
			check(h.restore(group1.Serialize()), h.restore(group2.Serialize()))
		}},
		{"Single group, multiple editors - persist (some not persistable)", func(t *testing.T, h *harness) {
			m := h.model()
			serializable1, nonSerializable2, serializable2 := h.input(), h.nonSerializable("3"), h.input()
			m.OpenEditor(serializable1, OpenOptions{Active: true, Pinned: true})
			m.OpenEditor(nonSerializable2, OpenOptions{Active: true})
			m.OpenEditor(serializable2, OpenOptions{Pinned: true})
			is(t, "count", m.Count(), 3)
			is(t, "active", m.ActiveEditor().Matches(nonSerializable2), true)
			is(t, "preview", m.PreviewEditor().Matches(nonSerializable2), true)
			for i, want := range []EditorInput{nonSerializable2, serializable2, serializable1} {
				is(t, "mru", mru(m)[i].Matches(want), true)
			}
			m = h.restore(m.Serialize())
			is(t, "count", m.Count(), 2)
			is(t, "active", m.ActiveEditor().Matches(serializable2), true)
			is(t, "preview", m.PreviewEditor(), EditorInput(nil))
			for i, want := range []EditorInput{serializable2, serializable1} {
				is(t, "mru", mru(m)[i].Matches(want), true)
			}
		}},
		{"Single group, multiple editors - persist (some not persistable, sticky editors)", func(t *testing.T, h *harness) {
			m := h.model()
			serializable1, nonSerializable2, serializable2 := h.input(), h.nonSerializable("3"), h.input()
			m.OpenEditor(serializable1, OpenOptions{Active: true, Pinned: true})
			m.OpenEditor(nonSerializable2, OpenOptions{Active: true, Pinned: true, Sticky: true})
			m.OpenEditor(serializable2, OpenOptions{Pinned: true})
			is(t, "count", m.Count(), 3)
			is(t, "sticky count", m.StickyCount(), 1)
			m = h.restore(m.Serialize())
			is(t, "count", m.Count(), 2)
			is(t, "sticky count", m.StickyCount(), 0)
		}},
		{"Multiple groups, multiple editors - persist (some not persistable, causes empty group)", func(t *testing.T, h *harness) {
			group1, group2 := h.model(), h.model()
			serializable1, serializable2, nonSerializable := h.input(), h.input(), h.nonSerializable("2")
			group1.OpenEditor(serializable1, OpenOptions{Pinned: true})
			group1.OpenEditor(serializable2, OpenOptions{})
			group2.OpenEditor(nonSerializable, OpenOptions{})
			group1 = h.restore(group1.Serialize())
			group2 = h.restore(group2.Serialize())
			is(t, "count 1", group1.Count(), 2)
			is(t, "first", seq(group1)[0].Matches(serializable1), true)
			is(t, "second", seq(group1)[1].Matches(serializable2), true)
			is(t, "count 2", group2.Count(), 0)
		}},
		{"Multiple Editors - Editor Dispose", func(t *testing.T, h *harness) {
			group1, group2 := h.model(), h.model()
			l1, l2 := listen(group1), listen(group2)
			input1, input2, input3 := h.input(), h.input(), h.input()
			group1.OpenEditor(input1, pinnedActive)
			group1.OpenEditor(input2, pinnedActive)
			group1.OpenEditor(input3, pinnedActive)
			group2.OpenEditor(input1, pinnedActive)
			group2.OpenEditor(input2, pinnedActive)

			input1.Dispose()
			is(t, "disposed 1", len(l1.disposed), 1)
			is(t, "disposed 1 index", l1.disposed[0].Index, 0)
			is(t, "disposed 2", len(l2.disposed), 1)
			is(t, "disposed 2 index", l2.disposed[0].Index, 0)
			is(t, "disposed 1 editor", l1.disposed[0].Editor.Matches(input1), true)
			is(t, "disposed 2 editor", l2.disposed[0].Editor.Matches(input1), true)

			input3.Dispose()
			is(t, "disposed 1", len(l1.disposed), 2)
			is(t, "disposed 1 index", l1.disposed[1].Index, 2)
			is(t, "disposed 2", len(l2.disposed), 1)
			is(t, "disposed 1 editor", l1.disposed[1].Editor.Matches(input3), true)
		}},
		{"Preview tab does not have a stable position (https://github.com/microsoft/vscode/issues/8245)", func(t *testing.T, h *harness) {
			m := h.model()
			input1, input2, input3 := h.input(), h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, OpenOptions{Active: true})
			m.SetActive(input1)
			m.OpenEditor(input3, OpenOptions{Active: true})
			is(t, "index", m.IndexOf(input3), 1)
		}},
		{"Multiple Editors - Editor Emits Dirty and Label Changed", func(t *testing.T, h *harness) {
			group1, group2 := h.model(), h.model()
			input1, input2 := h.input(), h.input()
			group1.OpenEditor(input1, pinnedActive)
			group2.OpenEditor(input2, pinnedActive)
			var dirty1, dirty2, label1, label2 int
			count := func(m *Model, dirty, label *int) {
				m.OnChange(func(e ChangeEvent) {
					switch e.Kind {
					case EditorDirty:
						*dirty++
					case EditorLabel:
						*label++
					case EditorOpen, EditorClose, EditorMove, EditorActive, EditorPin, EditorSticky, EditorCapabilities, EditorWillDispose:
					}
				})
			}
			count(group1, &dirty1, &label1)
			count(group2, &dirty2, &label2)

			input1.setDirty()
			input1.setLabel()
			is(t, "dirty 1", dirty1, 1)
			is(t, "label 1", label1, 1)
			input2.setDirty()
			input2.setLabel()
			is(t, "dirty 2", dirty2, 1)
			is(t, "label 2", label2, 1)

			h.closeAll(group2)
			input2.setDirty()
			input2.setLabel()
			is(t, "dirty 2", dirty2, 1)
			is(t, "label 2", label2, 1)
			is(t, "dirty 1", dirty1, 1)
			is(t, "label 1", label1, 1)
		}},
		{"Sticky Editors", func(t *testing.T, h *harness) {
			m := h.model()
			input1, input2, input3, input4 := h.input(), h.input(), h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, OpenOptions{Active: true})
			is(t, "sticky count", m.StickyCount(), 0)
			is(t, "sequential", len(m.Editors(Sequential, false)), 3)
			is(t, "sequential no sticky", len(m.Editors(Sequential, true)), 3)
			is(t, "mru", len(m.Editors(MostRecentlyActive, false)), 3)
			is(t, "mru no sticky", len(m.Editors(MostRecentlyActive, true)), 3)

			// sticking the last editor moves it first and pins it
			m.Stick(input3)
			is(t, "sticky count", m.StickyCount(), 1)
			is(t, "mru no sticky", len(m.Editors(MostRecentlyActive, true)), 2)
			is(t, "mru", len(m.Editors(MostRecentlyActive, false)), 3)
			is(t, "sticky 1", m.IsSticky(input1), false)
			is(t, "sticky 2", m.IsSticky(input2), false)
			is(t, "sticky 3", m.IsSticky(input3), true)
			is(t, "pinned 3", m.IsPinned(input3), true)
			is(t, "index 1", m.IndexOf(input1), 1)
			is(t, "index 2", m.IndexOf(input2), 2)
			is(t, "index 3", m.IndexOf(input3), 0)
			is(t, "sequential", len(m.Editors(Sequential, false)), 3)
			noSticky := m.Editors(Sequential, true)
			is(t, "sequential no sticky", len(noSticky), 2)
			is(t, "has 1", slices.Contains(noSticky, EditorInput(input1)), true)
			is(t, "has 2", slices.Contains(noSticky, EditorInput(input2)), true)
			noStickyMRU := m.Editors(MostRecentlyActive, true)
			is(t, "mru no sticky", len(noStickyMRU), 2)
			is(t, "mru has 1", slices.Contains(noStickyMRU, EditorInput(input1)), true)
			is(t, "mru has 2", slices.Contains(noStickyMRU, EditorInput(input2)), true)

			// sticking it again does nothing
			m.Stick(input3)
			is(t, "sticky 3", m.IsSticky(input3), true)

			// sticking the last editor moves it after the sticky one
			m.Stick(input2)
			is(t, "sticky count", m.StickyCount(), 2)
			is(t, "sticky 1", m.IsSticky(input1), false)
			is(t, "sticky 2", m.IsSticky(input2), true)
			is(t, "sticky 3", m.IsSticky(input3), true)
			is(t, "index 1", m.IndexOf(input1), 2)
			is(t, "index 2", m.IndexOf(input2), 1)
			is(t, "index 3", m.IndexOf(input3), 0)
			noSticky = m.Editors(Sequential, true)
			is(t, "sequential no sticky", len(noSticky), 1)
			is(t, "has 1", slices.Contains(noSticky, EditorInput(input1)), true)
			noStickyMRU = m.Editors(MostRecentlyActive, true)
			is(t, "mru no sticky", len(noStickyMRU), 1)
			is(t, "mru has 1", slices.Contains(noStickyMRU, EditorInput(input1)), true)

			// sticking the remaining editor works too
			m.Stick(input1)
			is(t, "sticky count", m.StickyCount(), 3)
			is(t, "index 1", m.IndexOf(input1), 2)
			is(t, "index 2", m.IndexOf(input2), 1)
			is(t, "index 3", m.IndexOf(input3), 0)
			is(t, "sequential no sticky", len(m.Editors(Sequential, true)), 0)
			is(t, "mru no sticky", len(m.Editors(MostRecentlyActive, true)), 0)

			// unsticking moves the editor after the sticky ones
			m.Unstick(input3)
			is(t, "sticky count", m.StickyCount(), 2)
			is(t, "sticky 1", m.IsSticky(input1), true)
			is(t, "sticky 2", m.IsSticky(input2), true)
			is(t, "sticky 3", m.IsSticky(input3), false)
			is(t, "index 1", m.IndexOf(input1), 1)
			is(t, "index 2", m.IndexOf(input2), 0)
			is(t, "index 3", m.IndexOf(input3), 2)

			// unsticking all works
			m.Unstick(input1)
			m.Unstick(input2)
			is(t, "sticky count", m.StickyCount(), 0)
			for _, in := range []*testInput{input1, input2, input3} {
				is(t, "sticky", m.IsSticky(in), false)
			}
			m.MoveEditor(input1, 0)
			m.MoveEditor(input2, 1)
			m.MoveEditor(input3, 2)

			// a new editor always opens after the sticky editors
			m.Stick(input1)
			m.Stick(input2)
			m.SetActive(input1)
			ev := listen(m)
			m.OpenEditor(input4, pinnedActive)
			is(t, "index 4", m.IndexOf(input4), 2)
			m.CloseEditor(input4, CloseUnknown, true)
			is(t, "closed[0] sticky", ev.closed[0].Sticky, false)
			m.SetActive(input2)
			m.OpenEditor(input4, pinnedActive)
			is(t, "index 4", m.IndexOf(input4), 2)
			m.CloseEditor(input4, CloseUnknown, true)
			is(t, "closed[1] sticky", ev.closed[1].Sticky, false)

			expect := func(count int, sticky [3]bool, index [3]int) {
				t.Helper()
				is(t, "sticky count", m.StickyCount(), count)
				for i, in := range []*testInput{input1, input2, input3} {
					is(t, "sticky "+in.Name(), m.IsSticky(in), sticky[i])
					is(t, "index "+in.Name(), m.IndexOf(in), index[i])
				}
			}
			// reset
			expect(2, [3]bool{true, true, false}, [3]int{0, 1, 2})

			// moving a sticky editor
			m.MoveEditor(input1, 1) // still within the sticky range
			expect(2, [3]bool{true, true, false}, [3]int{1, 0, 2})
			m.MoveEditor(input1, 0)
			expect(2, [3]bool{true, true, false}, [3]int{0, 1, 2})
			m.MoveEditor(input1, 2) // out of the sticky range
			expect(1, [3]bool{false, true, false}, [3]int{2, 0, 1})
			m.MoveEditor(input2, 2) // out of the sticky range
			expect(0, [3]bool{false, false, false}, [3]int{1, 2, 0})

			// reset
			m.MoveEditor(input1, 0)
			m.MoveEditor(input2, 1)
			m.MoveEditor(input3, 2)
			m.Stick(input1)
			m.Unstick(input2)
			expect(1, [3]bool{true, false, false}, [3]int{0, 1, 2})

			// moving an editor that is not sticky
			m.MoveEditor(input3, 1) // still within the unsticky range
			expect(1, [3]bool{true, false, false}, [3]int{0, 2, 1})
			m.MoveEditor(input3, 2)
			expect(1, [3]bool{true, false, false}, [3]int{0, 1, 2})
			m.MoveEditor(input3, 0) // into the sticky range
			expect(2, [3]bool{true, false, true}, [3]int{1, 2, 0})
			m.MoveEditor(input2, 0) // into the sticky range
			expect(3, [3]bool{true, true, true}, [3]int{2, 0, 1})

			// closing a sticky editor updates the state
			m.Stick(input1)
			m.Stick(input2)
			m.Unstick(input3)
			is(t, "sticky count", m.StickyCount(), 2)
			m.CloseEditor(input1, CloseUnknown, true)
			is(t, "closed[2] sticky", ev.closed[2].Sticky, true)
			is(t, "sticky count", m.StickyCount(), 1)
			m.CloseEditor(input2, CloseUnknown, true)
			is(t, "closed[3] sticky", ev.closed[3].Sticky, true)
			is(t, "sticky count", m.StickyCount(), 0)

			h.closeAll(m)
			is(t, "sticky count", m.StickyCount(), 0)

			// opening a sticky editor
			m.OpenEditor(input1, OpenOptions{Sticky: true})
			is(t, "sticky count", m.StickyCount(), 1)
			is(t, "sticky 1", m.IsSticky(input1), true)
			m.OpenEditor(input2, pinnedActive)
			is(t, "sticky count", m.StickyCount(), 1)
			is(t, "sticky 1", m.IsSticky(input1), true)
			is(t, "sticky 2", m.IsSticky(input2), false)
			m.OpenEditor(input2, OpenOptions{Sticky: true})
			is(t, "sticky count", m.StickyCount(), 2)
			is(t, "sticky 1", m.IsSticky(input1), true)
			is(t, "sticky 2", m.IsSticky(input2), true)
			m.OpenEditor(input3, pinnedActive)
			m.OpenEditor(input4, OpenOptions{Active: true, Sticky: true})
			is(t, "sticky count", m.StickyCount(), 3)
			is(t, "sticky 1", m.IsSticky(input1), true)
			is(t, "sticky 2", m.IsSticky(input2), true)
			is(t, "sticky 3", m.IsSticky(input3), false)
			is(t, "sticky 4", m.IsSticky(input4), true)
			is(t, "pinned 4", m.IsPinned(input4), true)
			is(t, "index 1", m.IndexOf(input1), 0)
			is(t, "index 2", m.IndexOf(input2), 1)
			is(t, "index 3", m.IndexOf(input3), 3)
			is(t, "index 4", m.IndexOf(input4), 2)
		}},
		{"Sticky/Unsticky Editors sends correct editor index", func(t *testing.T, h *harness) {
			m := h.model()
			input1, input2, input3 := h.input(), h.input(), h.input()
			m.OpenEditor(input1, pinnedActive)
			m.OpenEditor(input2, pinnedActive)
			m.OpenEditor(input3, OpenOptions{Active: true})
			is(t, "sticky count", m.StickyCount(), 0)
			ev := listen(m)

			m.Stick(input3)
			is(t, "sticky[0] index", ev.sticky[0].Index, 0)
			is(t, "sticky 3", m.IsSticky(input3), true)
			is(t, "sticky count", m.StickyCount(), 1)

			m.Stick(input2)
			is(t, "sticky[1] index", ev.sticky[1].Index, 1)
			is(t, "sticky 2", m.IsSticky(input2), true)
			is(t, "sticky count", m.StickyCount(), 2)

			m.Unstick(input3)
			is(t, "unsticky[0] index", ev.unsticky[0].Index, 1)
			is(t, "sticky 3", m.IsSticky(input3), false)
			is(t, "sticky 2", m.IsSticky(input2), true)
			is(t, "sticky count", m.StickyCount(), 1)
		}},
		{"onDidMoveEditor Event", func(t *testing.T, h *harness) {
			group1, group2 := h.model(), h.model()
			in1g1, in2g1, in1g2, in2g2 := h.input(), h.input(), h.input(), h.input()
			idx := func(i int) OpenOptions { return OpenOptions{Pinned: true, Active: i == 0, Index: i, HasIndex: true} }
			group1.OpenEditor(in1g1, idx(0))
			group1.OpenEditor(in2g1, idx(1))
			group2.OpenEditor(in1g2, idx(0))
			group2.OpenEditor(in2g2, idx(1))
			e1, e2 := listen(group1), listen(group2)
			group1.MoveEditor(in1g1, 1)
			is(t, "moved 1", e1.moved[0].Editor, EditorInput(in1g1))
			is(t, "moved 1 old index", e1.moved[0].OldIndex, 0)
			is(t, "moved 1 index", e1.moved[0].Index, 1)
			group2.MoveEditor(in1g2, 1)
			is(t, "moved 2", e2.moved[0].Editor, EditorInput(in1g2))
			is(t, "moved 2 old index", e2.moved[0].OldIndex, 0)
			is(t, "moved 2 index", e2.moved[0].Index, 1)
		}},
		{"onDidOpeneditor Event", func(t *testing.T, h *harness) {
			group1, group2 := h.model(), h.model()
			e1, e2 := listen(group1), listen(group2)
			in1g1, in2g1, in1g2, in2g2 := h.input(), h.input(), h.input(), h.input()
			idx := func(i int) OpenOptions { return OpenOptions{Pinned: true, Active: i == 0, Index: i, HasIndex: true} }
			group1.OpenEditor(in1g1, idx(0))
			group1.OpenEditor(in2g1, idx(1))
			group2.OpenEditor(in1g2, idx(0))
			group2.OpenEditor(in2g2, idx(1))
			is(t, "opened 1", len(e1.opened), 2)
			is(t, "opened 1[0]", e1.opened[0].Editor, EditorInput(in1g1))
			is(t, "opened 1[0] index", e1.opened[0].Index, 0)
			is(t, "opened 1[1]", e1.opened[1].Editor, EditorInput(in2g1))
			is(t, "opened 1[1] index", e1.opened[1].Index, 1)
			is(t, "opened 2", len(e2.opened), 2)
			is(t, "opened 2[0]", e2.opened[0].Editor, EditorInput(in1g2))
			is(t, "opened 2[0] index", e2.opened[0].Index, 0)
			is(t, "opened 2[1]", e2.opened[1].Editor, EditorInput(in2g2))
			is(t, "opened 2[1] index", e2.opened[1].Index, 1)
		}},
		{"moving editor sends sticky event when sticky changes", func(t *testing.T, h *harness) {
			group1 := h.model()
			in1, in2, in3 := h.input(), h.input(), h.input()
			group1.OpenEditor(in1, OpenOptions{Pinned: true, Active: true, Index: 0, HasIndex: true, Sticky: true})
			group1.OpenEditor(in2, OpenOptions{Pinned: true, Index: 1, HasIndex: true})
			group1.OpenEditor(in3, OpenOptions{Pinned: true, Index: 2, HasIndex: true})
			e1 := listen(group1)
			group1.MoveEditor(in2, 0)
			is(t, "sticky[0]", e1.sticky[0].Editor, EditorInput(in2))
			is(t, "sticky[0] index", e1.sticky[0].Index, 0)

			group2 := h.model()
			in1g2, in2g2, in3g2 := h.input(), h.input(), h.input()
			group2.OpenEditor(in1g2, OpenOptions{Pinned: true, Active: true, Index: 0, HasIndex: true, Sticky: true})
			group2.OpenEditor(in2g2, OpenOptions{Pinned: true, Index: 1, HasIndex: true})
			group2.OpenEditor(in3g2, OpenOptions{Pinned: true, Index: 2, HasIndex: true})
			e2 := listen(group2)
			group2.MoveEditor(in1g2, 1)
			is(t, "unsticky[0]", e2.unsticky[0].Editor, EditorInput(in1g2))
			is(t, "unsticky[0] index", e2.unsticky[0].Index, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, newHarness(t)) })
	}
}
