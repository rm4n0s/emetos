package editorgroup

import (
	"slices"
)

// OpenPositioning says where a new editor goes (`workbench.editor.openPositioning`).
type OpenPositioning string

// The values of `workbench.editor.openPositioning`.
const (
	// PositionRight puts the editor right of the active one.
	PositionRight OpenPositioning = "right"
	// PositionLeft puts it left of the active one.
	PositionLeft OpenPositioning = "left"
	// PositionFirst puts it first.
	PositionFirst OpenPositioning = "first"
	// PositionLast puts it last.
	PositionLast OpenPositioning = "last"
)

// Options are the settings a Model follows.
type Options struct {
	// OpenPositioning is `workbench.editor.openPositioning`; empty is PositionRight.
	OpenPositioning OpenPositioning
	// FocusRecentEditorAfterClose is `workbench.editor.focusRecentEditorAfterClose`: closing the
	// active editor activates the most recently used one, else the one at its right.
	FocusRecentEditorAfterClose bool
}

// DefaultOptions are VS Code's defaults of the settings the model follows.
func DefaultOptions() Options {
	return Options{OpenPositioning: PositionRight, FocusRecentEditorAfterClose: true}
}

// OpenOptions say how an editor is opened (VS Code's IEditorOpenOptions).
type OpenOptions struct {
	// Pinned opens the editor as a regular editor; without it the editor is the preview editor,
	// which the next preview open replaces.
	Pinned bool
	// Sticky also pins the editor and puts it among the sticky editors, at the left (VS Code's
	// "pinned" tabs, the ones with the pin icon).
	Sticky bool
	// Active makes the editor the active one. The first editor of a group is always active, and so
	// is one that replaces an active preview editor.
	Active bool
	// Index is the place of the editor, when HasIndex is set.
	Index    int
	HasIndex bool
}

// CloseContext says why an editor closes (VS Code's EditorCloseContext).
type CloseContext int

const (
	// CloseUnknown is a close that the user asked for, or that nothing else explains.
	CloseUnknown CloseContext = iota
	// CloseReplace is the close of a preview editor that another editor replaces.
	CloseReplace
	// CloseMove is the close of an editor that moves to another group.
	CloseMove
	// CloseUnpin is the close of a preview editor that another editor became the preview of.
	CloseUnpin
)

// EditorsOrder is the order Editors lists the editors in.
type EditorsOrder int

const (
	// Sequential lists the editors as the tabs are, left to right.
	Sequential EditorsOrder = iota
	// MostRecentlyActive lists the active editor first, then the one active before it, and so on.
	MostRecentlyActive
)

// ChangeKind says what changed in a Model (VS Code's GroupModelChangeKind, without the group
// kinds and the selection).
type ChangeKind int

const (
	// EditorOpen is an editor added; Index is its place.
	EditorOpen ChangeKind = iota
	// EditorClose is an editor removed; Index is the place it had, Context why it closed and
	// Sticky whether it was sticky.
	EditorClose
	// EditorMove is an editor moved; OldIndex is where it was and Index where it is.
	EditorMove
	// EditorActive is a new active editor; Index is its place.
	EditorActive
	// EditorPin is an editor pinned or unpinned (made the preview editor); IsPinned says which.
	EditorPin
	// EditorSticky is an editor made sticky or not sticky; IsSticky says which.
	EditorSticky
	// EditorDirty is a change of the dirty mark of an editor.
	EditorDirty
	// EditorLabel is a change of the name, the description or the title of an editor.
	EditorLabel
	// EditorCapabilities is a change of the capabilities of an editor.
	EditorCapabilities
	// EditorWillDispose is sent when an editor of the model is about to be disposed.
	EditorWillDispose
)

// ChangeEvent tells that a Model changed.
type ChangeEvent struct {
	Kind   ChangeKind
	Editor EditorInput
	// Index is the place of Editor (after the change, except for a close).
	Index int
	// OldIndex is where a moved editor was.
	OldIndex int
	// Context is why an editor closed.
	Context CloseContext
	// Sticky tells whether a closed editor was sticky.
	Sticky bool
}

// CloseResult is what CloseEditor reports about the editor it closed.
type CloseResult struct {
	Editor  EditorInput
	Index   int
	Sticky  bool
	Context CloseContext
}

type changeListener struct{ fn func(ChangeEvent) }

// Model is the editors of one group, as data: their order, the preview editor, the sticky ones
// (kept at the left), the active one and the most recently used order. It is used on the UI
// thread only.
//
// Not ported from VS Code's EditorGroupModel: the selection of several tabs, transient editors,
// locking, cloning, side by side matching and the group's own events (index, label, active).
//
// Ported from VS Code: src/vs/workbench/common/editor/editorGroupModel.ts
type Model struct {
	id        int
	opts      Options
	editors   []EditorInput
	mru       []EditorInput
	active    EditorInput
	preview   EditorInput
	sticky    int // the index of the last sticky editor, -1 when there is none
	listeners []*changeListener
	// inputListeners holds the stop functions of the subscriptions to the inputs.
	inputListeners map[EditorInput]func()
}

// NewModel makes an empty model for the group with an id.
func NewModel(id int, opts Options) *Model {
	if opts.OpenPositioning == "" {
		opts.OpenPositioning = PositionRight
	}
	return &Model{id: id, opts: opts, sticky: -1, inputListeners: map[EditorInput]func(){}}
}

// ID is the id of the group.
func (m *Model) ID() int { return m.id }

// Options are the settings the model follows.
func (m *Model) Options() Options { return m.opts }

// SetOptions changes the settings the model follows.
func (m *Model) SetOptions(opts Options) {
	if opts.OpenPositioning == "" {
		opts.OpenPositioning = PositionRight
	}
	m.opts = opts
}

// OnChange calls fn for every change of the model, and gives the function that stops the calls.
func (m *Model) OnChange(fn func(ChangeEvent)) (unsubscribe func()) {
	l := &changeListener{fn: fn}
	m.listeners = append(m.listeners, l)
	return func() {
		for i, x := range m.listeners {
			if x == l {
				m.listeners = append(m.listeners[:i:i], m.listeners[i+1:]...)
				return
			}
		}
	}
}

func (m *Model) fire(e ChangeEvent) {
	for _, l := range append([]*changeListener(nil), m.listeners...) {
		l.fn(e)
	}
}

// Count is the number of editors.
func (m *Model) Count() int { return len(m.editors) }

// StickyCount is the number of sticky editors.
func (m *Model) StickyCount() int { return m.sticky + 1 }

// Editors lists the editors in an order. ExcludeSticky leaves the sticky ones out.
func (m *Model) Editors(order EditorsOrder, excludeSticky bool) []EditorInput {
	var editors []EditorInput
	if order == MostRecentlyActive {
		editors = slices.Clone(m.mru)
	} else {
		editors = slices.Clone(m.editors)
	}
	if !excludeSticky {
		return editors
	}
	if order == MostRecentlyActive {
		return slices.DeleteFunc(editors, m.IsSticky)
	}
	return editors[min(m.sticky+1, len(editors)):]
}

// EditorAt gives the editor at an index, or nil when there is none.
func (m *Model) EditorAt(index int) EditorInput {
	if index < 0 || index >= len(m.editors) {
		return nil
	}
	return m.editors[index]
}

// ActiveEditor is the active editor, or nil when the group is empty.
func (m *Model) ActiveEditor() EditorInput { return m.active }

// IsActive tells whether an editor (or one that matches it) is the active one.
func (m *Model) IsActive(candidate EditorInput) bool { return matches(m.active, candidate, false) }

// PreviewEditor is the preview editor, or nil when there is none.
func (m *Model) PreviewEditor() EditorInput { return m.preview }

func matches(editor, candidate EditorInput, strict bool) bool {
	if editor == nil || candidate == nil {
		return false
	}
	if editor == candidate {
		return true
	}
	return !strict && editor.Matches(candidate)
}

// OpenEditor adds an editor, or finds the one that matches it. It gives the editor that is in the
// model (the existing one, when there is one) and whether it was added. An editor opened with no
// option is the preview editor and replaces the preview editor there is; opening an existing
// editor with Pinned pins it, and with Active activates it.
func (m *Model) OpenEditor(candidate EditorInput, opts OpenOptions) (editor EditorInput, isNew bool) {
	makeSticky := opts.Sticky || (opts.HasIndex && m.IsStickyAt(opts.Index))
	makePinned := opts.Pinned || opts.Sticky
	makeActive := opts.Active || m.active == nil || (!makePinned && m.preview == m.active)

	existing, existingIndex, found := m.Find(candidate)
	if !found {
		newEditor := candidate
		indexOfActive := m.IndexOf(m.active)

		// where it goes
		var targetIndex int
		switch {
		case opts.HasIndex:
			targetIndex = opts.Index
		case m.opts.OpenPositioning == PositionFirst:
			targetIndex = 0
			// after the sticky editors, unless it becomes sticky
			if !makeSticky && m.IsStickyAt(targetIndex) {
				targetIndex = m.sticky + 1
			}
		case m.opts.OpenPositioning == PositionLast:
			targetIndex = len(m.editors)
		default:
			if m.opts.OpenPositioning == PositionLeft {
				if indexOfActive == 0 || len(m.editors) == 0 {
					targetIndex = 0
				} else {
					targetIndex = indexOfActive
				}
			} else {
				targetIndex = indexOfActive + 1
			}
			if !makeSticky && m.IsStickyAt(targetIndex) {
				targetIndex = m.sticky + 1
			}
		}

		// a sticky editor goes to the end of the sticky ones
		if makeSticky {
			m.sticky++
			if !m.IsStickyAt(targetIndex) {
				targetIndex = m.sticky
			}
		}

		// it is added to the list when it is pinned or there is no preview editor to replace
		if makePinned || m.preview == nil {
			m.splice(targetIndex, false, newEditor)
		}

		if !makePinned {
			// the preview editor, if any, is replaced by this one
			if m.preview != nil {
				indexOfPreview := m.IndexOf(m.preview)
				if targetIndex > indexOfPreview {
					targetIndex-- // the preview editor closes
				}
				m.replaceEditor(m.preview, newEditor, targetIndex, !makeActive)
			}
			m.preview = newEditor
		}

		m.registerInput(newEditor)
		m.fire(ChangeEvent{Kind: EditorOpen, Editor: newEditor, Index: targetIndex})

		if makeActive {
			m.setActiveEditor(newEditor)
		} else {
			m.setActiveEditor(m.active)
		}
		return newEditor, true
	}

	if makePinned {
		m.doPin(existing, existingIndex)
	}
	if makeActive {
		m.setActiveEditor(existing)
	} else {
		m.setActiveEditor(m.active)
	}
	if opts.HasIndex {
		m.MoveEditor(existing, opts.Index)
	}
	// intentionally after MoveEditor, in case the editor moved into the sticky range
	if makeSticky {
		m.doStick(existing, m.IndexOf(existing))
	}
	return existing, false
}

// registerInput follows the events of an input while it is in the model.
func (m *Model) registerInput(editor EditorInput) {
	stop := editor.Subscribe(func(e InputEvent) {
		index := m.IndexOf(editor)
		switch e.Kind {
		case InputDirty:
			m.fire(ChangeEvent{Kind: EditorDirty, Editor: editor, Index: index})
		case InputLabel:
			m.fire(ChangeEvent{Kind: EditorLabel, Editor: editor, Index: index})
		case InputCapabilities:
			m.fire(ChangeEvent{Kind: EditorCapabilities, Editor: editor, Index: index})
		case InputWillDispose:
			if index >= 0 {
				m.fire(ChangeEvent{Kind: EditorWillDispose, Editor: editor, Index: index})
			}
		}
	})
	m.inputListeners[editor] = stop
}

func (m *Model) unregisterInput(editor EditorInput) {
	if stop, ok := m.inputListeners[editor]; ok {
		stop()
		delete(m.inputListeners, editor)
	}
}

func (m *Model) replaceEditor(toReplace, replaceWith EditorInput, replaceIndex int, openNext bool) {
	closeResult, ok := m.doCloseEditor(toReplace, CloseReplace, openNext)
	// the new editor is added to the model before the close event is sent, because the event can
	// dispose the editor that was replaced
	m.splice(replaceIndex, false, replaceWith)
	if ok {
		m.fire(ChangeEvent{Kind: EditorClose, Editor: closeResult.Editor, Index: closeResult.Index, Sticky: closeResult.Sticky, Context: closeResult.Context})
	}
}

// CloseEditor removes an editor (or the one that matches it). When it was the active editor and
// openNext is set, another one becomes active: the most recently used, or the one at its right
// without FocusRecentEditorAfterClose. It gives nil when the model has no such editor.
func (m *Model) CloseEditor(candidate EditorInput, context CloseContext, openNext bool) *CloseResult {
	result, ok := m.doCloseEditor(candidate, context, openNext)
	if !ok {
		return nil
	}
	m.fire(ChangeEvent{Kind: EditorClose, Editor: result.Editor, Index: result.Index, Sticky: result.Sticky, Context: result.Context})
	return &result
}

func (m *Model) doCloseEditor(candidate EditorInput, context CloseContext, openNext bool) (CloseResult, bool) {
	index := m.IndexOf(candidate)
	if index == -1 {
		return CloseResult{}, false
	}
	editor := m.editors[index]
	sticky := m.IsStickyAt(index)

	if m.active == editor && openNext {
		if len(m.mru) > 1 {
			var newActive EditorInput
			switch {
			case m.opts.FocusRecentEditorAfterClose:
				newActive = m.mru[1] // the active editor is always first in the MRU list
			case index == len(m.editors)-1:
				newActive = m.editors[index-1] // the last editor closes: the previous one
			default:
				newActive = m.editors[index+1]
			}
			m.doSetActive(newActive, slices.Index(m.editors, newActive))
		} else {
			m.doSetActive(nil, -1)
		}
	}

	if m.preview == editor {
		m.preview = nil
	}
	m.unregisterInput(editor)
	m.splice(index, true, nil)
	return CloseResult{Editor: editor, Index: index, Sticky: sticky, Context: context}, true
}

// MoveEditor moves an editor to an index (kept within the editors). Moving it into the sticky
// range makes it sticky, and out of it not sticky. It gives the editor, or nil when nothing moved.
func (m *Model) MoveEditor(candidate EditorInput, toIndex int) EditorInput {
	if toIndex >= len(m.editors) {
		toIndex = len(m.editors) - 1
	} else if toIndex < 0 {
		toIndex = 0
	}
	index := m.IndexOf(candidate)
	if index < 0 || toIndex == index {
		return nil
	}
	editor := m.editors[index]
	sticky := m.sticky

	switch {
	case m.IsStickyAt(index) && toIndex > m.sticky:
		m.sticky--
	case !m.IsStickyAt(index) && toIndex <= m.sticky:
		m.sticky++
	}

	m.editors = slices.Delete(m.editors, index, index+1)
	m.editors = slices.Insert(m.editors, toIndex, editor)
	m.fire(ChangeEvent{Kind: EditorMove, Editor: editor, OldIndex: index, Index: toIndex})
	if sticky != m.sticky {
		m.fire(ChangeEvent{Kind: EditorSticky, Editor: editor, Index: toIndex})
	}
	return editor
}

// SetActive makes an editor the active one. It gives the editor of the model, or nil when the
// model has none that matches.
func (m *Model) SetActive(candidate EditorInput) EditorInput {
	editor, index, ok := m.Find(candidate)
	if !ok {
		return nil
	}
	m.doSetActive(editor, index)
	return editor
}

func (m *Model) setActiveEditor(candidate EditorInput) {
	if candidate == nil {
		return
	}
	if editor, index, ok := m.Find(candidate); ok {
		m.doSetActive(editor, index)
	}
}

func (m *Model) doSetActive(editor EditorInput, index int) {
	previous := m.active
	m.active = editor
	if editor == nil || index < 0 || previous == editor {
		return
	}
	// to the front of the most recently used list
	if i := indexIn(m.mru, editor, false); i >= 0 {
		m.mru = slices.Delete(m.mru, i, i+1)
	}
	m.mru = slices.Insert(m.mru, 0, editor)
	m.fire(ChangeEvent{Kind: EditorActive, Editor: editor, Index: index})
}

// Pin makes a preview editor a regular editor. It gives the editor of the model, or nil.
func (m *Model) Pin(candidate EditorInput) EditorInput {
	editor, index, ok := m.Find(candidate)
	if !ok {
		return nil
	}
	m.doPin(editor, index)
	return editor
}

func (m *Model) doPin(editor EditorInput, index int) {
	if m.IsPinned(editor) {
		return
	}
	m.preview = nil
	m.fire(ChangeEvent{Kind: EditorPin, Editor: editor, Index: index})
}

// Unpin makes a regular editor the preview editor; the preview editor there was closes. It
// gives the editor of the model, or nil.
func (m *Model) Unpin(candidate EditorInput) EditorInput {
	editor, index, ok := m.Find(candidate)
	if !ok {
		return nil
	}
	m.doUnpin(editor, index)
	return editor
}

func (m *Model) doUnpin(editor EditorInput, index int) {
	if !m.IsPinned(editor) {
		return
	}
	oldPreview := m.preview
	m.preview = editor
	m.fire(ChangeEvent{Kind: EditorPin, Editor: editor, Index: index})
	if oldPreview != nil {
		m.CloseEditor(oldPreview, CloseUnpin, true)
	}
}

// IsPinned tells whether an editor is a regular editor, not the preview editor.
func (m *Model) IsPinned(editor EditorInput) bool { return !matches(m.preview, editor, false) }

// IsPinnedAt is IsPinned for the editor at an index.
func (m *Model) IsPinnedAt(index int) bool { return m.IsPinned(m.EditorAt(index)) }

// Stick pins an editor and moves it to the end of the sticky editors. It gives the editor of the
// model, or nil.
func (m *Model) Stick(candidate EditorInput) EditorInput {
	editor, index, ok := m.Find(candidate)
	if !ok {
		return nil
	}
	m.doStick(editor, index)
	return editor
}

func (m *Model) doStick(editor EditorInput, index int) {
	if m.IsStickyAt(index) {
		return
	}
	m.Pin(editor)
	newIndex := m.sticky + 1
	m.MoveEditor(editor, newIndex)
	m.sticky++
	m.fire(ChangeEvent{Kind: EditorSticky, Editor: editor, Index: newIndex})
}

// Unstick moves a sticky editor to the start of the editors that are not sticky. It gives the
// editor of the model, or nil.
func (m *Model) Unstick(candidate EditorInput) EditorInput {
	editor, index, ok := m.Find(candidate)
	if !ok {
		return nil
	}
	m.doUnstick(editor, index)
	return editor
}

func (m *Model) doUnstick(editor EditorInput, index int) {
	if !m.IsStickyAt(index) {
		return
	}
	newIndex := m.sticky
	m.MoveEditor(editor, newIndex)
	m.sticky--
	m.fire(ChangeEvent{Kind: EditorSticky, Editor: editor, Index: newIndex})
}

// IsSticky tells whether an editor is sticky.
func (m *Model) IsSticky(editor EditorInput) bool { return m.IsStickyAt(m.IndexOf(editor)) }

// IsStickyAt tells whether the editor at an index is sticky.
func (m *Model) IsStickyAt(index int) bool {
	return m.sticky >= 0 && index >= 0 && index <= m.sticky
}

// splice adds an editor at an index, or removes the editor there, in both lists.
func (m *Model) splice(index int, del bool, editor EditorInput) {
	var toDelete EditorInput
	if index >= 0 && index < len(m.editors) {
		toDelete = m.editors[index]
	}
	if del && m.IsStickyAt(index) {
		m.sticky--
	}
	if del {
		if index >= 0 && index < len(m.editors) {
			m.editors = slices.Delete(m.editors, index, index+1)
		}
	} else {
		m.editors = slices.Insert(m.editors, min(max(index, 0), len(m.editors)), editor)
	}

	// the most recently used list
	switch {
	case !del && editor != nil:
		// a new editor is the most recent one after the active one: it is not the most recently
		// active unless it becomes active, but it is more recent than the others
		if len(m.mru) == 0 {
			m.mru = append(m.mru, editor)
		} else {
			m.mru = slices.Insert(m.mru, 1, editor)
		}
	case del:
		if i := indexIn(m.mru, toDelete, false); i >= 0 {
			m.mru = slices.Delete(m.mru, i, i+1)
		}
	}
}

func indexIn(list []EditorInput, candidate EditorInput, strict bool) int {
	if candidate == nil {
		return -1
	}
	for i, editor := range list {
		if matches(editor, candidate, strict) {
			return i
		}
	}
	return -1
}

// IndexOf is the index of an editor that matches the candidate, or -1.
func (m *Model) IndexOf(candidate EditorInput) int { return indexIn(m.editors, candidate, false) }

// IndexOfStrict is IndexOf for the very same input only.
func (m *Model) IndexOfStrict(candidate EditorInput) int { return indexIn(m.editors, candidate, true) }

// Find gives the editor that matches the candidate, with its index.
func (m *Model) Find(candidate EditorInput) (editor EditorInput, index int, ok bool) {
	index = m.IndexOf(candidate)
	if index == -1 {
		return nil, -1, false
	}
	return m.editors[index], index, true
}

// IsFirst tells whether the candidate matches the first editor.
func (m *Model) IsFirst(candidate EditorInput) bool {
	return len(m.editors) > 0 && matches(m.editors[0], candidate, false)
}

// IsLast tells whether the candidate matches the last editor.
func (m *Model) IsLast(candidate EditorInput) bool {
	return len(m.editors) > 0 && matches(m.editors[len(m.editors)-1], candidate, false)
}

// Contains tells whether an editor matches the candidate.
func (m *Model) Contains(candidate EditorInput) bool { return m.IndexOf(candidate) != -1 }

// ContainsStrict tells whether the very same input is in the model.
func (m *Model) ContainsStrict(candidate EditorInput) bool { return m.IndexOfStrict(candidate) != -1 }

// Dispose stops following the inputs. It does not dispose the inputs.
func (m *Model) Dispose() {
	for editor, stop := range m.inputListeners {
		stop()
		delete(m.inputListeners, editor)
	}
	m.listeners = nil
}

// Clone makes a model with the same editors in the same state (order, preview, sticky, active and
// most recently used order), under another id. dup gives the input the clone holds for an input
// of this model: nil leaves the editor out of the clone; a nil dup shares all the inputs, as VS
// Code shares the inputs of a cloned group. The clone follows the events of its inputs.
//
// Ported from VS Code: EditorGroupModel.clone
func (m *Model) Clone(id int, dup func(EditorInput) EditorInput) *Model {
	c := NewModel(id, m.opts)
	mapped := map[EditorInput]EditorInput{}
	for i, e := range m.editors {
		d := e
		if dup != nil {
			d = dup(e)
		}
		if d == nil {
			continue
		}
		mapped[e] = d
		c.editors = append(c.editors, d)
		if m.IsStickyAt(i) {
			c.sticky++
		}
	}
	for _, e := range m.mru {
		if d, ok := mapped[e]; ok {
			c.mru = append(c.mru, d)
		}
	}
	c.active = mapped[m.active]
	if c.active == nil && len(c.mru) > 0 {
		c.active = c.mru[0]
	}
	c.preview = mapped[m.preview]
	for _, e := range c.editors {
		c.registerInput(e)
	}
	return c
}
