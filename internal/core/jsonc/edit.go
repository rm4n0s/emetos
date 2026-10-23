package jsonc

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/rm4n0s/errors"
)

// Remove returns the edit that removes the property or array element at path, keeping
// the comments around it, and formats the lines it touches. It returns no edit when
// there is nothing at the path.
//
// Ported from VS Code: src/vs/base/common/jsonEdit.ts (removeProperty).
func Remove(text string, path Path, opts FormattingOptions) ([]Edit, error) {
	return setValue(text, path, nil, true, opts, nil)
}

// SetValue returns the edit that sets the value at path to value (anything
// encoding/json writes; nil is null), keeping the comments, and formats the lines it
// touches. Properties and array elements that are missing on the way are created; an
// index of -1 or past the end appends to an array, and an index inside it inserts.
// It fails with the tag JSONCCannotSet when a segment does not fit the value it is
// applied to (a name on an array, or an index on an object).
//
// Ported from VS Code: src/vs/base/common/jsonEdit.ts (setProperty).
func SetValue(text string, path Path, value any, opts FormattingOptions) ([]Edit, error) {
	return setValue(text, path, value, false, opts, nil)
}

// SetValueAt is SetValue where insertionIndex chooses, from the names of the
// properties of an object, the place a new property goes (a number from 0 to their
// count).
func SetValueAt(text string, path Path, value any, opts FormattingOptions, insertionIndex func(names []string) int) ([]Edit, error) {
	return setValue(text, path, value, false, opts, insertionIndex)
}

// stringify writes a value as compact JSON.
func stringify(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", errors.NewErr("JSONCEncodeFailed", err)
	}
	return string(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

func setValue(text string, originalPath Path, value any, remove bool, opts FormattingOptions, insertionIndex func([]string) int) ([]Edit, error) {
	path := append(Path(nil), originalPath...)
	root, _ := ParseTree(text, ParseOptions{})
	var parent *Node
	var last any
	for len(path) > 0 {
		last = path[len(path)-1]
		path = path[:len(path)-1]
		parent = FindNodeAtLocation(root, path)
		if parent != nil || remove {
			break
		}
		// The parent is missing: make it, with the value in it.
		if name, ok := last.(string); ok {
			value = map[string]any{name: value}
		} else {
			value = []any{value}
		}
	}

	switch {
	case parent == nil:
		// An empty document, or the path of the root.
		if remove {
			return nil, nil
		}
		content, err := stringify(value)
		if err != nil {
			return nil, err
		}
		edit := Edit{Content: content}
		if root != nil {
			edit.Offset, edit.Length = root.Offset, root.Length
		}
		return withFormatting(text, edit, opts), nil
	case parent.Type == ObjectNode && isString(last):
		return setInObject(text, parent, last.(string), value, remove, opts, insertionIndex)
	case parent.Type == ArrayNode && isInt(last):
		return setInArray(text, parent, last.(int), value, remove, opts)
	}
	what := "property"
	if isString(last) {
		what = "index"
	}
	return nil, errors.New("JSONCCannotSet", "can not add a "+what+" to a parent of that type", "parent", string(parent.Type))
}

func isString(v any) bool { _, ok := v.(string); return ok }
func isInt(v any) bool    { _, ok := v.(int); return ok }

func setInObject(text string, parent *Node, name string, value any, remove bool, opts FormattingOptions, insertionIndex func([]string) int) ([]Edit, error) {
	existing := FindNodeAtLocation(parent, Path{name})
	if existing != nil {
		prop := existing.Parent
		if remove {
			index := 0
			for i, c := range parent.Children {
				if c == prop {
					index = i
				}
			}
			removeBegin, removeEnd := 0, prop.End()
			switch {
			case index > 0:
				// Remove the comma of the previous property.
				removeBegin = parent.Children[index-1].End()
			default:
				removeBegin = parent.Offset + 1
				if len(parent.Children) > 1 {
					// Remove the comma of the next property.
					removeEnd = parent.Children[1].Offset
				} else {
					// Remove the trailing comma of the only property.
					sc := NewScanner(text, true)
					sc.SetPosition(removeEnd)
					if sc.Scan() == CommaToken {
						removeEnd = sc.Position()
					}
				}
			}
			return withFormatting(text, Edit{Offset: removeBegin, Length: removeEnd - removeBegin}, opts), nil
		}
		content, err := stringify(value)
		if err != nil {
			return nil, err
		}
		return withFormatting(text, Edit{Offset: existing.Offset, Length: existing.Length, Content: content}, opts), nil
	}
	if remove {
		return nil, nil
	}
	quoted, err := stringify(name)
	if err != nil {
		return nil, err
	}
	content, err := stringify(value)
	if err != nil {
		return nil, err
	}
	property := quoted + ": " + content
	index := len(parent.Children)
	if insertionIndex != nil {
		names := make([]string, len(parent.Children))
		for i, p := range parent.Children {
			names[i], _ = p.Children[0].Value.(string)
		}
		index = max(0, min(insertionIndex(names), len(parent.Children)))
	}
	var edit Edit
	switch {
	case index > 0:
		edit = Edit{Offset: parent.Children[index-1].End(), Content: "," + property}
	case len(parent.Children) == 0:
		edit = Edit{Offset: parent.Offset + 1, Content: property}
	default:
		edit = Edit{Offset: parent.Offset + 1, Content: property + ","}
	}
	return withFormatting(text, edit, opts), nil
}

func setInArray(text string, parent *Node, index int, value any, remove bool, opts FormattingOptions) ([]Edit, error) {
	n := len(parent.Children)
	if !remove {
		content, err := stringify(value)
		if err != nil {
			return nil, err
		}
		var edit Edit
		if n == 0 || index == 0 {
			edit = Edit{Offset: parent.Offset + 1, Content: content}
			if n != 0 {
				edit.Content += ","
			}
		} else {
			at := index
			if index == -1 || index > n {
				at = n
			}
			if at < 0 {
				return nil, errors.New("JSONCCannotSet", "the index is before the start of the array", "index", index)
			}
			edit = Edit{Offset: parent.Children[at-1].End(), Content: "," + content}
		}
		return withFormatting(text, edit, opts), nil
	}
	if index < 0 || index >= n {
		return nil, nil
	}
	var edit Edit
	switch {
	case n == 1:
		edit = Edit{Offset: parent.Offset + 1, Length: parent.Length - 2}
	case index == n-1:
		offset := parent.Children[index-1].End()
		edit = Edit{Offset: offset, Length: parent.End() - 1 - offset}
	default:
		toRemove := parent.Children[index]
		edit = Edit{Offset: toRemove.Offset, Length: parent.Children[index+1].Offset - toRemove.Offset}
	}
	return withFormatting(text, edit, opts), nil
}

// withFormatting applies the edit, formats the lines it changed, and returns one edit
// that makes both changes.
//
// Ported from VS Code: src/vs/base/common/jsonEdit.ts (withFormatting).
func withFormatting(text string, edit Edit, opts FormattingOptions) []Edit {
	newText := applyEdit(text, edit)
	begin := edit.Offset
	end := edit.Offset + len(edit.Content)
	if edit.Length == 0 || edit.Content == "" { // an insert or a removal
		for begin > 0 && !isEOL(newText, begin-1) {
			begin--
		}
		for end < len(newText) && !isEOL(newText, end) {
			end++
		}
	}
	edits := Format(newText, &Range{Offset: begin, Length: end - begin}, opts)

	// Apply the formatting from the last edit to the first, and track the range that changed.
	for i := len(edits) - 1; i >= 0; i-- {
		cur := edits[i]
		newText = applyEdit(newText, cur)
		begin = min(begin, cur.Offset)
		end = max(end, cur.Offset+cur.Length)
		end += len(cur.Content) - cur.Length
	}
	editLength := len(text) - (len(newText) - end) - begin
	return []Edit{{Offset: begin, Length: editLength, Content: newText[begin:end]}}
}

func applyEdit(text string, e Edit) string {
	return text[:e.Offset] + e.Content + text[e.Offset+e.Length:]
}

// ApplyEdits applies edits that do not overlap, each with its offset in the original
// text. It fails with the tags JSONCEditOutOfRange and JSONCEditsOverlap.
//
// Ported from VS Code: src/vs/base/common/jsonEdit.ts (applyEdits).
func ApplyEdits(text string, edits []Edit) (string, error) {
	sorted := append([]Edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Offset != sorted[j].Offset {
			return sorted[i].Offset < sorted[j].Offset
		}
		return sorted[i].Length < sorted[j].Length
	})
	size := len(text)
	last := size
	for i := len(sorted) - 1; i >= 0; i-- {
		e := sorted[i]
		if e.Offset < 0 || e.Length < 0 || e.Offset+e.Length > size {
			return "", errors.New("JSONCEditOutOfRange", "an edit is outside the text", "offset", e.Offset, "length", e.Length)
		}
		if e.Offset+e.Length > last {
			return "", errors.New("JSONCEditsOverlap", "two edits overlap", "offset", e.Offset)
		}
		text = applyEdit(text, e)
		last = e.Offset
	}
	return text, nil
}
