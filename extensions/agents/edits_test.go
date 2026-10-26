package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// The proposed edits (step 13.3): the two texts of the diff and the input that goes back when the user changed the text.

// writeFile writes a file of the folder of the rig.
func (r *rig) writeFile(name, text string) {
	r.t.Helper()
	path := filepath.Join(r.folder, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// request makes a permission request for a tool with an input.
func request(tool string, input map[string]any) claude.ToolRequest {
	data, _ := json.Marshal(input)
	return claude.ToolRequest{RequestID: "p", ToolName: tool, Input: data}
}

func TestPlanEditWorksOutTheTwoTextsOfTheDiff(t *testing.T) {
	notebook := `{"cells":[{"id":"c1","cell_type":"code","source":["x = 1\n","y = 2"]},{"id":"c2","cell_type":"markdown","source":"# Title"}],"metadata":{"language_info":{"name":"python"}}}`
	tests := []struct {
		name         string
		files        map[string]string
		tool         string
		input        map[string]any
		original     string
		proposed     string
		title        string
		language     string
		editedFinal  string // the text the user ends with, to rebuild the input from ("" skips)
		wantRebuilt  map[string]any
		wantUnchaged bool
	}{
		{name: "an edit replaces the first and only occurrence", files: map[string]string{"main.go": "package main\n\nfunc main() {}\n"},
			tool: "Edit", input: map[string]any{"file_path": "main.go", "old_string": "func main() {}", "new_string": "func main() {\n}"},
			original: "package main\n\nfunc main() {}\n", proposed: "package main\n\nfunc main() {\n}\n", title: "main.go (proposed)"},
		{name: "an edit with an absolute path", files: map[string]string{"a/b.go": "one\ntwo\n"},
			tool: "Edit", input: map[string]any{"file_path": "@ABS@/a/b.go", "old_string": "two", "new_string": "2"},
			original: "one\ntwo\n", proposed: "one\n2\n", title: "a/b.go (proposed)"},
		{name: "replace_all replaces every occurrence", files: map[string]string{"x.txt": "a b a b\n"},
			tool: "Edit", input: map[string]any{"file_path": "x.txt", "old_string": "a", "new_string": "z", "replace_all": true},
			original: "a b a b\n", proposed: "z b z b\n", title: "x.txt (proposed)"},
		{name: "an edit of a file with CRLF lines is matched in its normalized form", files: map[string]string{"w.txt": "one\r\ntwo\r\n"},
			tool: "Edit", input: map[string]any{"file_path": "w.txt", "old_string": "one\ntwo", "new_string": "1\n2"},
			original: "one\r\ntwo\r\n", proposed: "1\n2\n", title: "w.txt (proposed)"},
		{name: "an empty old_string makes a new file", tool: "Edit", input: map[string]any{"file_path": "fresh.go", "old_string": "", "new_string": "package fresh\n"},
			original: "", proposed: "package fresh\n", title: "fresh.go (proposed)"},
		{name: "a write of an existing file", files: map[string]string{"main.go": "old\n"}, tool: "Write", input: map[string]any{"file_path": "main.go", "content": "new\n"},
			original: "old\n", proposed: "new\n", title: "main.go (proposed)"},
		{name: "a write of a new file has an empty left side", tool: "Write", input: map[string]any{"file_path": "deep/new.txt", "content": "text\n"},
			original: "", proposed: "text\n", title: "deep/new.txt (proposed)"},
		{name: "a cell of a notebook is replaced", files: map[string]string{"nb.ipynb": notebook}, tool: "NotebookEdit",
			input:    map[string]any{"notebook_path": "nb.ipynb", "cell_id": "c1", "new_source": "x = 10\n"},
			original: "x = 1\ny = 2", proposed: "x = 10\n", title: "nb.ipynb cell c1 (proposed)", language: "python"},
		{name: "a cell of a notebook by its number", files: map[string]string{"nb.ipynb": notebook}, tool: "NotebookEdit",
			input:    map[string]any{"notebook_path": "nb.ipynb", "cell_id": "cell-1", "new_source": "# Other"},
			original: "# Title", proposed: "# Other", title: "nb.ipynb cell cell-1 (proposed)", language: "markdown"},
		{name: "a new cell", files: map[string]string{"nb.ipynb": notebook}, tool: "NotebookEdit",
			input:    map[string]any{"notebook_path": "nb.ipynb", "new_source": "z = 3", "cell_type": "code", "edit_mode": "insert"},
			original: "", proposed: "z = 3", title: "nb.ipynb new cell (proposed)", language: "python"},
		{name: "a deleted cell has an empty right side", files: map[string]string{"nb.ipynb": notebook}, tool: "NotebookEdit",
			input:    map[string]any{"notebook_path": "nb.ipynb", "cell_id": "c2", "edit_mode": "delete"},
			original: "# Title", proposed: "", title: "nb.ipynb cell c2 (proposed)", language: "markdown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "")
			for name, text := range tc.files {
				r.writeFile(name, text)
			}
			input := map[string]any{}
			for k, v := range tc.input {
				if s, ok := v.(string); ok && strings.HasPrefix(s, "@ABS@") {
					v = r.folder + strings.TrimPrefix(s, "@ABS@")
				}
				input[k] = v
			}
			plan, err := r.ext.planEdit(context.Background(), request(tc.tool, input))
			if err != nil {
				t.Fatal(err)
			}
			if plan.original != tc.original || plan.proposed != tc.proposed || plan.title != tc.title || plan.language != tc.language {
				t.Errorf("plan = original %q proposed %q title %q language %q\nwant   original %q proposed %q title %q language %q",
					plan.original, plan.proposed, plan.title, plan.language, tc.original, tc.proposed, tc.title, tc.language)
			}
			if want := "file://" + r.folder + "/"; !strings.HasPrefix(plan.uri, want) {
				t.Errorf("the URI is %q, want one under %q", plan.uri, want)
			}
			if plan.rebuild == nil {
				t.Error("the plan cannot make the input again")
			}
		})
	}
}

func TestPlanEditFailures(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		files map[string]string
		input map[string]any
		route string
	}{
		{"an input that is not JSON", "Edit", nil, nil, "planEdit.EditInputInvalid"},
		{"no file", "Edit", nil, map[string]any{"old_string": "a"}, "planEdit.EditInputInvalid"},
		{"a file outside the folder", "Write", nil, map[string]any{"file_path": "/etc/hostname", "content": "x"}, "planEdit.EditOutsideFolder"},
		{"a path that leaves the folder", "Write", nil, map[string]any{"file_path": "../elsewhere.txt", "content": "x"}, "planEdit.EditOutsideFolder"},
		{"a file a deny rule excludes", "Write", map[string]string{".env": "A=1\n"}, map[string]any{"file_path": ".env", "content": "x"}, "planEdit.EditFileDenied"},
		{"a file the project denies", "Edit", map[string]string{"secrets/key.txt": "k"}, map[string]any{"file_path": "secrets/key.txt", "old_string": "k", "new_string": "j"}, "planEdit.EditFileDenied"},
		{"a folder instead of a file", "Write", nil, map[string]any{"file_path": "util", "content": "x"}, "planEdit.EditFileUnreadable"},
		{"old_string that is not in the file", "Edit", map[string]string{"main.go": "package main\n"}, map[string]any{"file_path": "main.go", "old_string": "func", "new_string": "x"}, "planStringEdit.EditOldStringMissing"},
		{"old_string twice without replace_all", "Edit", map[string]string{"a.txt": "x\nx\n"}, map[string]any{"file_path": "a.txt", "old_string": "x", "new_string": "y"}, "planStringEdit.EditOldStringAmbiguous"},
		{"an empty old_string on a file with text", "Edit", map[string]string{"a.txt": "x\n"}, map[string]any{"file_path": "a.txt", "old_string": "", "new_string": "y"}, "planStringEdit.EditOldStringMissing"},
		{"an edit of a file that is not there", "Edit", nil, map[string]any{"file_path": "gone.txt", "old_string": "a", "new_string": "b"}, "planStringEdit.EditFileMissing"},
		{"a notebook that is not there", "NotebookEdit", nil, map[string]any{"notebook_path": "gone.ipynb", "new_source": "x"}, "planNotebookEdit.EditFileMissing"},
		{"a notebook that is not JSON", "NotebookEdit", map[string]string{"bad.ipynb": "not json"}, map[string]any{"notebook_path": "bad.ipynb", "cell_id": "c1", "new_source": "x"}, "planNotebookEdit.NotebookInvalid"},
		{"a cell that is not in the notebook", "NotebookEdit", map[string]string{"nb.ipynb": `{"cells":[{"id":"c1","source":"a"}]}`}, map[string]any{"notebook_path": "nb.ipynb", "cell_id": "nope", "new_source": "x"}, "planNotebookEdit.NotebookCellMissing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "")
			for name, text := range tc.files {
				r.writeFile(name, text)
			}
			req := request(tc.tool, tc.input)
			if tc.input == nil {
				req.Input = json.RawMessage(`[1]`)
			}
			_, err := r.ext.planEdit(context.Background(), req)
			if err == nil {
				t.Fatal("no error")
			}
			route(t, err, tc.route)
		})
	}
}

func TestMinimalEditGivesAnEditThatIsUniqueAndRebuildsTheText(t *testing.T) {
	tests := []struct {
		name, original, final string
		ok                    bool
	}{
		{"nothing changed", "a\nb\n", "a\nb\n", false},
		{"one line changed", "a\nb\nc\n", "a\nB\nc\n", true},
		{"a line added", "a\nb\n", "a\nx\nb\n", true},
		{"a line removed", "a\nb\nc\n", "a\nc\n", true},
		{"the first line changed", "a\nb\n", "z\nb\n", true},
		{"the last line changed", "a\nb", "a\nz", true},
		{"a change inside a line", "let x = 1\nlet y = 2\n", "let x = 10\nlet y = 2\n", true},
		{"a line repeated elsewhere is made unique by its neighbours", "x\ny\nx\nz\n", "x\ny\nx\nw\n", true},
		{"a repeated line that changes in the second place", "x\nx\nx\n", "x\nx\nq\n", true},
		{"an insertion between equal lines", "a\na\n", "a\nb\na\n", true},
		{"an insertion into an empty file", "", "text\n", true},
		{"everything replaced", "old\n", "new\n", true},
		{"a file emptied", "gone\n", "", true},
		{"non-ASCII text", "héllo\nwörld\n", "héllo\nwörld!\n", true},
		{"a change in a file without a final newline", "a\nb", "a\nb\n", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			old, repl, ok := minimalEdit(tc.original, tc.final)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if tc.original != "" && (strings.Count(tc.original, old) != 1 || strings.Index(tc.original, old) != strings.LastIndex(tc.original, old)) {
				t.Errorf("old_string %q is in the file more than once", old)
			}
			if got := strings.Replace(tc.original, old, repl, 1); got != tc.final {
				t.Errorf("applying (%q -> %q) gives %q, want %q", old, repl, got, tc.final)
			}
		})
	}
}

func TestPlanEditRebuildsTheInputFromTheTextTheUserEndedWith(t *testing.T) {
	r := newRig(t, "")
	r.writeFile("main.go", "package main\n\nfunc main() {}\n")
	r.writeFile("nb.ipynb", `{"cells":[{"id":"c1","cell_type":"code","source":"a"}]}`)
	tests := []struct {
		name  string
		tool  string
		input map[string]any
		final string
		check func(t *testing.T, got map[string]any)
	}{
		{"an edit", "Edit", map[string]any{"file_path": "main.go", "old_string": "func main() {}", "new_string": "func main() {\n}", "replace_all": true},
			"package main\n\nfunc main() {\n\tprintln(1)\n}\n", func(t *testing.T, got map[string]any) {
				old, repl := got["old_string"].(string), got["new_string"].(string)
				if got["file_path"] != "main.go" || got["replace_all"] != nil {
					t.Errorf("the input = %v", got)
				}
				if res := strings.Replace("package main\n\nfunc main() {}\n", old, repl, 1); res != "package main\n\nfunc main() {\n\tprintln(1)\n}\n" {
					t.Errorf("the edit gives %q", res)
				}
			}},
		{"a write", "Write", map[string]any{"file_path": "main.go", "content": "x"}, "y\n", func(t *testing.T, got map[string]any) {
			if got["content"] != "y\n" || got["file_path"] != "main.go" {
				t.Errorf("the input = %v", got)
			}
		}},
		{"a cell", "NotebookEdit", map[string]any{"notebook_path": "nb.ipynb", "cell_id": "c1", "new_source": "b"}, "c", func(t *testing.T, got map[string]any) {
			if got["new_source"] != "c" || got["cell_id"] != "c1" {
				t.Errorf("the input = %v", got)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := r.ext.planEdit(context.Background(), request(tc.tool, tc.input))
			if err != nil {
				t.Fatal(err)
			}
			data, err := plan.rebuild(tc.final)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			tc.check(t, got)
		})
	}
	// the user undid the whole change: nothing is left to apply
	plan, err := r.ext.planEdit(context.Background(), request("Edit", map[string]any{"file_path": "main.go", "old_string": "func main", "new_string": "func run"}))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := plan.rebuild(plan.original); err != nil || data != nil {
		t.Errorf("rebuild of the original text = %q, %v, want nothing", data, err)
	}
}
