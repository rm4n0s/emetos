package agents

import (
	"context"
	"encoding/json"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// The proposed edits (SPEC §10.16 "As built (step 13.3)"): for Edit, Write and NotebookEdit the permission request becomes a
// diff in the editor. This file works out the two texts of the diff from the input of the tool and the file as it is on disk,
// and the input to send back when the user changed the proposed text. Nothing here writes a file: the program applies the
// edit when it is allowed.

// editPlan is a change a tool wants to make, ready to be reviewed.
type editPlan struct {
	// uri is the file:// URI of the file (of the notebook for NotebookEdit) and rel its path relative to the folder.
	uri, rel string
	// original is the text on the left (empty for a file that does not exist yet) and proposed the text on the right.
	original, proposed string
	// title is the title of the diff editor.
	title string
	// language is the language of both sides when the name of the file does not say it.
	language string
	// rebuild makes the input of the tool from the text the user ended with in the diff; it is called only when the user
	// changed the proposed text. A nil input means that nothing is left to apply.
	rebuild func(final string) (json.RawMessage, error)
}

// maxEditInput is the largest input of a tool request that is parsed for a diff.
const maxEditInput = 16 << 20

// isEditTool tells whether a tool is shown as a diff.
func isEditTool(name string) bool {
	return name == "Edit" || name == "Write" || name == "NotebookEdit"
}

// planEdit makes the plan of an Edit, Write or NotebookEdit request. The file is read from the folder of the workspace only
// (a file outside it, or one a read deny rule excludes, is not read: the request is then a plain dialog). The failures that
// say the tool cannot do what it asks (EditOldStringMissing, EditOldStringAmbiguous, EditFileMissing, NotebookCellMissing)
// are returned with their tags for the caller to deny the request with the message; the others (EditInputInvalid,
// EditOutsideFolder, EditFileDenied, EditFileUnreadable, NotebookInvalid) mean that no diff can be shown.
func (a *agents) planEdit(ctx context.Context, req claude.ToolRequest) (*editPlan, error) {
	var input map[string]any
	if len(req.Input) > maxEditInput {
		return nil, errors.New("EditInputInvalid", "the input of the tool is too large to show as a diff", "tool", req.ToolName)
	}
	if err := json.Unmarshal(req.Input, &input); err != nil {
		return nil, errors.NewErr("EditInputInvalid", err).SetMetadata("tool", req.ToolName)
	}
	str := func(key string) string { s, _ := input[key].(string); return s }
	file := str("file_path")
	if req.ToolName == "NotebookEdit" {
		file = str("notebook_path")
	}
	if file == "" {
		return nil, errors.New("EditInputInvalid", "the tool names no file", "tool", req.ToolName)
	}
	folder := a.api.Scope().Folder
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(folder, file)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(folder, abs)
	if folder == "" || err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("EditOutsideFolder", "the file is not in the folder of the workspace", "path", abs)
	}
	rel = filepath.ToSlash(rel)
	if a.denied(ctx, rel) {
		return nil, errors.New("EditFileDenied", "a read deny rule excludes the file", "path", rel)
	}
	text, exists := "", true
	switch data, err := a.api.Workspace().ReadFile(ctx, rel); {
	case err == nil:
		text = data
	case errors.Is(err, fs.ErrNotExist):
		exists = false
	default:
		return nil, errors.NewErr("EditFileUnreadable", err).SetMetadata("path", rel)
	}
	p := &editPlan{uri: "file://" + filepath.ToSlash(abs), rel: rel, original: text, title: rel + " (proposed)"}
	switch req.ToolName {
	case "Write":
		p.proposed = str("content")
		p.rebuild = func(final string) (json.RawMessage, error) { return withInput(input, "content", final) }
	case "Edit":
		if err := planStringEdit(p, input, exists); err != nil {
			return nil, err
		}
	default:
		if err := planNotebookEdit(p, input, text, exists, abs); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// planStringEdit fills the plan of an Edit: the proposed text is the file with old_string replaced by new_string. The
// failure is returned with the plan; a plan with a failure is not shown.
func planStringEdit(p *editPlan, input map[string]any, exists bool) error {
	old, _ := input["old_string"].(string)
	repl, _ := input["new_string"].(string)
	all, _ := input["replace_all"].(bool)
	switch {
	case !exists && old != "":
		return errors.New("EditFileMissing", "the file does not exist, so there is nothing to replace in it", "path", p.rel)
	case old == "":
		if p.original != "" {
			return errors.New("EditOldStringMissing", "old_string is empty but the file has text", "path", p.rel)
		}
		p.proposed = repl // a new file, or an empty one
	default:
		src, o, n := p.original, old, repl
		count := strings.Count(src, o)
		if count == 0 && strings.Contains(src, "\r\n") {
			// the agent writes "\n"; a file with "\r\n" lines is matched in its normalized form
			src, o, n = normalizeEOL(src), normalizeEOL(old), normalizeEOL(repl)
			count = strings.Count(src, o)
		}
		switch {
		case count == 0:
			return errors.New("EditOldStringMissing", "old_string is not in the file", "path", p.rel)
		case count > 1 && !all:
			return errors.New("EditOldStringAmbiguous", "old_string is in the file more than once and replace_all is off", "path", p.rel, "count", count)
		}
		if all {
			p.proposed = strings.ReplaceAll(src, o, n)
		} else {
			p.proposed = strings.Replace(src, o, n, 1)
		}
	}
	p.rebuild = func(final string) (json.RawMessage, error) {
		oldS, newS, ok := minimalEdit(p.original, final)
		if !ok {
			return nil, nil
		}
		next := cloneInput(input)
		next["old_string"], next["new_string"] = oldS, newS
		delete(next, "replace_all")
		return marshalInput(next)
	}
	return nil
}

// planNotebookEdit fills the plan of a NotebookEdit: the diff is the source of the cell, the one the edit replaces (empty for
// a new cell, and the proposed text empty for a deleted one), because the notebook's JSON would show noise.
func planNotebookEdit(p *editPlan, input map[string]any, text string, exists bool, abs string) error {
	if !exists {
		return errors.New("EditFileMissing", "the notebook does not exist", "path", p.rel)
	}
	var nb struct {
		Cells []struct {
			ID       string `json:"id"`
			CellType string `json:"cell_type"`
			Source   any    `json:"source"`
		} `json:"cells"`
		Metadata struct {
			Language struct {
				Name string `json:"name"`
			} `json:"language_info"`
			Kernel struct {
				Language string `json:"language"`
			} `json:"kernelspec"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(text), &nb); err != nil {
		return errors.NewErr("NotebookInvalid", err).SetMetadata("path", p.rel)
	}
	id, _ := input["cell_id"].(string)
	mode, _ := input["edit_mode"].(string)
	if mode == "" {
		mode = "replace"
	}
	cellType, _ := input["cell_type"].(string)
	source, _ := input["new_source"].(string)
	index := -1
	for i, c := range nb.Cells {
		if id != "" && c.ID == id {
			index = i
		}
	}
	if index < 0 && strings.HasPrefix(id, "cell-") {
		if n, err := strconv.Atoi(strings.TrimPrefix(id, "cell-")); err == nil && n >= 0 && n < len(nb.Cells) {
			index = n
		}
	}
	if index < 0 && (id != "" || mode != "insert") {
		return errors.New("NotebookCellMissing", "the notebook has no cell with that id", "path", p.rel, "cell", id)
	}
	original := ""
	if mode != "insert" {
		original = cellSource(nb.Cells[index].Source)
		if cellType == "" {
			cellType = nb.Cells[index].CellType
		}
	}
	p.original = original
	if mode != "delete" {
		p.proposed = source
	}
	p.uri = "file://" + filepath.ToSlash(abs)
	name := path.Base(p.rel)
	if id == "" {
		p.title = name + " new cell (proposed)"
	} else {
		p.title = name + " cell " + id + " (proposed)"
	}
	switch {
	case cellType == "markdown":
		p.language = "markdown"
	case nb.Metadata.Language.Name != "":
		p.language = nb.Metadata.Language.Name
	default:
		p.language = nb.Metadata.Kernel.Language
	}
	p.rebuild = func(final string) (json.RawMessage, error) { return withInput(input, "new_source", final) }
	return nil
}

// cellSource is the text of the source of a notebook cell: a string, or a list of lines.
func cellSource(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []any:
		var sb strings.Builder
		for _, line := range s {
			if l, ok := line.(string); ok {
				sb.WriteString(l)
			}
		}
		return sb.String()
	}
	return ""
}

// normalizeEOL makes the line endings of a text "\n".
func normalizeEOL(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// cloneInput copies the top level of an input.
func cloneInput(input map[string]any) map[string]any {
	out := make(map[string]any, len(input)+1)
	for k, v := range input {
		out[k] = v
	}
	return out
}

// marshalInput writes an input as JSON.
func marshalInput(input map[string]any) (json.RawMessage, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, errors.NewErr("EditInputInvalid", err)
	}
	return data, nil
}

// withInput is the input with one field set.
func withInput(input map[string]any, key string, value any) (json.RawMessage, error) {
	next := cloneInput(input)
	next[key] = value
	return marshalInput(next)
}

// minimalEdit turns the user's final text into an Edit of the original file: old is the part of the original that is
// replaced, new the text that takes its place. The part is made of whole lines and is made longer, a line at a time, until it
// is in the original once, as the Edit tool needs. ok is false when the final text is the original.
func minimalEdit(original, final string) (old, repl string, ok bool) {
	if original == final {
		return "", "", false
	}
	if original == "" {
		return "", final, true
	}
	p := 0
	for p < len(original) && p < len(final) && original[p] == final[p] {
		p++
	}
	s := 0
	for s < len(original)-p && s < len(final)-p && original[len(original)-1-s] == final[len(final)-1-s] {
		s++
	}
	for p > 0 && original[p-1] != '\n' {
		p-- // the part starts at the start of a line
	}
	for s > 0 && original[len(original)-s-1] != '\n' {
		s-- // and ends at the end of one
	}
	lo, hi := p, len(original)-s
	for !occursOnceAt(original, original[lo:hi], lo) {
		switch {
		case lo > 0:
			lo-- // a line more before
			for lo > 0 && original[lo-1] != '\n' {
				lo--
			}
		case hi < len(original):
			hi++ // or after
			for hi < len(original) && original[hi-1] != '\n' {
				hi++
			}
		default:
			return original, final, true
		}
	}
	return original[lo:hi], final[lo : len(final)-(len(original)-hi)], true
}

// occursOnceAt tells whether part is in text once, at index at; an occurrence that overlaps another counts as two.
func occursOnceAt(text, part string, at int) bool {
	if part == "" {
		return false
	}
	return strings.HasPrefix(text[at:], part) && !strings.Contains(text[at+1:], part) && !strings.Contains(text[:at+len(part)-1], part)
}
