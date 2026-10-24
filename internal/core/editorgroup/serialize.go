package editorgroup

import "encoding/json"

// SerializedInput is an editor input in the saved state of a window (step 7.5).
type SerializedInput struct {
	// TypeID is the TypeID of the input; Value is what its Serialize gave.
	TypeID string `json:"id"`
	Value  string `json:"value"`
	// State is what the window keeps of the editor of the input besides the input: its view state (the
	// cursors, the scroll position and the folds). The model does not look into it.
	State json.RawMessage `json:"state,omitempty"`
}

// InputStateFunc gives the State of an input when a model or a grid is serialized; nil or an empty
// result leaves the input without one.
type InputStateFunc func(EditorInput) json.RawMessage

// SerializedModel is a Model in the saved state of a window: VS Code's ISerializedEditorGroupModel
// without the lock.
type SerializedModel struct {
	ID      int               `json:"id"`
	Editors []SerializedInput `json:"editors"`
	// MRU is the most recently used order, as indexes into Editors.
	MRU []int `json:"mru"`
	// Preview is the index of the preview editor in Editors.
	Preview *int `json:"preview,omitempty"`
	// Sticky is the index in Editors of the last sticky editor.
	Sticky *int `json:"sticky,omitempty"`
}

// Serialize gives the state of the model to save. The editors that cannot be serialized are left
// out, and so are they from the most recently used order, the preview editor and the sticky
// editors.
func (m *Model) Serialize() SerializedModel { return m.SerializeWith(nil) }

// SerializeWith is Serialize with the State of each input given by state.
func (m *Model) SerializeWith(state InputStateFunc) SerializedModel {
	out := SerializedModel{ID: m.id, Editors: []SerializedInput{}, MRU: []int{}}
	var kept []EditorInput
	sticky := m.sticky
	for i, editor := range m.editors {
		value, ok := editor.Serialize()
		if !ok {
			if m.IsStickyAt(i) {
				sticky--
			}
			continue
		}
		saved := SerializedInput{TypeID: editor.TypeID(), Value: value}
		if state != nil {
			saved.State = state(editor)
		}
		out.Editors = append(out.Editors, saved)
		kept = append(kept, editor)
		if m.preview == editor {
			p := len(kept) - 1
			out.Preview = &p
		}
	}
	for _, editor := range m.mru {
		if i := indexIn(kept, editor, false); i >= 0 {
			out.MRU = append(out.MRU, i)
		}
	}
	if sticky >= 0 {
		out.Sticky = &sticky
	}
	return out
}

// RestoreModel makes a model from the state Serialize gave. create makes the input of a saved
// editor, or reports false when it cannot (an unknown type, a file that is gone): the editor is
// left out, and so it is from the order, the preview editor and the sticky editors.
func RestoreModel(data SerializedModel, opts Options, create func(typeID, value string) (EditorInput, bool)) *Model {
	return RestoreModelWith(data, opts, func(in SerializedInput) (EditorInput, bool) { return create(in.TypeID, in.Value) })
}

// RestoreModelWith is RestoreModel with a factory that is given the whole saved input, State included.
func RestoreModelWith(data SerializedModel, opts Options, create func(SerializedInput) (EditorInput, bool)) *Model {
	m := NewModel(data.ID, opts)
	byIndex := make([]EditorInput, len(data.Editors))
	stickyLast := -1
	if data.Sticky != nil {
		stickyLast = *data.Sticky
	}
	m.sticky = -1
	for i, saved := range data.Editors {
		editor, ok := create(saved)
		if !ok || editor == nil {
			continue
		}
		byIndex[i] = editor
		m.editors = append(m.editors, editor)
		m.registerInput(editor)
		if i <= stickyLast {
			m.sticky++
		}
	}
	for _, i := range data.MRU {
		if i >= 0 && i < len(byIndex) && byIndex[i] != nil {
			m.mru = append(m.mru, byIndex[i])
		}
	}
	if len(m.mru) > 0 {
		m.active = m.mru[0]
	}
	if data.Preview != nil && *data.Preview >= 0 && *data.Preview < len(byIndex) {
		m.preview = byIndex[*data.Preview]
	}
	return m
}
