package golang

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The commands of step 14.2 over the fake API: the editor in front is told to the extension by events, the documents are the
// fake's, and gopls is the fake's too (the real one is in the tests with the build tag emetos_gopls).

// focus writes the file in the folder, tells the extension that it is in front with the selection the marks (§ ‹ ›) say, and
// returns its path, its URI and its text.
func (r *rig) focus(name, marked string) (path, uri, text string) {
	r.t.Helper()
	text, start, end := marks(r.t, marked)
	path = filepath.Join(r.folder, name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		r.t.Fatal(err)
	}
	uri = fileURI(path)
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: "go"})
	r.api.Emit(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: uri, Selections: []extension.Selection{
		{Anchor: positionAt(text, start), Active: positionAt(text, end)},
	}})
	r.api.Pump()
	return path, uri, text
}

// note is the message of the last notification, and its severity; empty when there is none.
func (r *rig) lastNote() (severity, message string) {
	notes := r.api.Notes()
	if len(notes) == 0 {
		return "", ""
	}
	n := notes[len(notes)-1]
	return n.Severity, n.Message
}

// docText is the text of the document as the fake has it after the edits.
func (r *rig) docText(uri string) string {
	r.t.Helper()
	text, ok := r.api.DocumentText(uri)
	if !ok {
		r.t.Fatalf("%s is not open", uri)
	}
	return text
}

// commandArgs are the arguments of the command for a case: the tags, the options, the transform and the template; false when the
// command cannot say it (overwrite, skipping the unexported fields, clearing only the options, removing and adding together).
func (c tagCase) commandArgs(t *testing.T) (args map[string]any, remove, ok bool) {
	t.Helper()
	m := c.mod(t)
	if m.Overwrite || m.SkipUnexported || (m.ClearOptions && !m.Clear) {
		return nil, false, false
	}
	str := func(k string) string { s, _ := c.Args[k].(string); return s }
	if c.Args["Modification"] == "remove" {
		return map[string]any{"tags": str("Remove"), "options": str("RemoveOptions")}, true, len(m.Add) == 0
	}
	if m.Clear {
		return nil, false, false
	}
	return map[string]any{"tags": str("Add"), "options": str("AddOptions"), "transform": str("Transform"), "template": str("ValueFormat")}, false, true
}

// equalJSON tells whether two values are the same JSON.
func equalJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	return string(x) == string(y)
}

func TestTheTagCommandsChangeTheDocumentLikeTheEditorOfTags(t *testing.T) {
	ran := 0
	for _, tt := range loadTagCases(t) {
		args, remove, ok := tt.commandArgs(t)
		if !ok {
			continue // not a thing the command says
		}
		ran++
		t.Run(tt.Name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			_, uri, _ := r.focus("tags.go", tt.Src)
			id := AddTagsCommand
			if remove {
				id = RemoveTagsCommand
			}
			if remove && args["tags"] == "" && args["options"] == "" {
				// nothing named: the command asks, and empty answers remove all the tags
				if err := r.api.Run(id, nil); err != nil {
					t.Fatal(err)
				}
				r.api.LastInput().Accept("")
				r.api.LastInput().Accept("")
			} else if err := r.api.Run(id, args); err != nil {
				t.Fatal(err)
			}
			r.api.Pump()
			if got := r.docText(uri); got != tt.Want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.Want)
			}
		})
	}
	if ran < 15 {
		t.Errorf("only %d cases are run through the command", ran)
	}
}

func TestAddTagsAsksWhenTheSettingHasNothing(t *testing.T) {
	r := newRig(t, rigOptions{settings: map[string]any{AddTagsSetting: map[string]any{"transform": "lispcase"}}})
	_, uri, _ := r.focus("a.go", "package p\n\ntype T struct {\n\tUserName string§\n}\n")
	if err := r.api.Run(AddTagsCommand, nil); err != nil {
		t.Fatal(err)
	}
	in := r.api.LastInput()
	if in == nil || in.Options().Prompt != "Enter comma separated tag names" || in.Options().Value != "" {
		t.Fatalf("the first box = %+v", in)
	}
	in.Accept(" json , xml ")
	if len(r.api.Inputs()) != 2 || r.api.LastInput().Options().Prompt != "Enter comma separated options (tag=option)" {
		t.Fatalf("inputs = %d", len(r.api.Inputs()))
	}
	r.api.LastInput().Accept("json=omitempty")
	pick := r.api.LastPick()
	if pick == nil || pick.Options().Placeholder != "Select transform" {
		t.Fatalf("pick = %+v", pick)
	}
	want := []string{"snakecase", "camelcase", "lispcase", "pascalcase", "keep"}
	if !slices.Equal(pick.Labels(), want) || pick.Options().Items[2].Description != "default" {
		t.Errorf("labels %v, items %+v", pick.Labels(), pick.Options().Items)
	}
	pick.Choose(1)
	if got := r.docText(uri); got != "package p\n\ntype T struct {\n\tUserName string `json:\"userName,omitempty\" xml:\"userName\"`\n}\n" {
		t.Errorf("got:\n%s", got)
	}
}

func TestAddTagsWithEmptyAnswersAddsNothing(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n}\n")
	if err := r.api.Run(AddTagsCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.api.LastInput().Accept("")
	r.api.LastInput().Accept("")
	r.api.LastPick().Choose(0)
	if sev, msg := r.lastNote(); sev != "info" || !strings.Contains(msg, "nothing was added") || len(r.api.Applied()) != 0 {
		t.Errorf("notification = %s %q, applied %+v", sev, msg, r.api.Applied())
	}
}

func TestPromptForTagsAsksEvenWhenTheSettingHasTags(t *testing.T) {
	r := newRig(t, rigOptions{settings: map[string]any{AddTagsSetting: map[string]any{"tags": "json", "options": "json=omitempty", "promptForTags": true}}})
	_, uri, text := r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n}\n")
	if err := r.api.Run(AddTagsCommand, nil); err != nil {
		t.Fatal(err)
	}
	// the boxes start with the configured values
	if v := r.api.LastInput().Options().Value; v != "json" {
		t.Errorf("the tags box starts with %q", v)
	}
	r.api.LastInput().Accept("json")
	if v := r.api.LastInput().Options().Value; v != "json=omitempty" {
		t.Errorf("the options box starts with %q", v)
	}
	r.api.LastInput().Accept("")
	r.api.LastPick().Choose(0)
	if got := r.docText(uri); got == text || !strings.Contains(got, "`json:\"a\"`") {
		t.Errorf("got:\n%s", got)
	}
}

func TestClosingAPromptCancelsTheCommand(t *testing.T) {
	steps := []struct {
		name string
		stop func(r *rig)
	}{
		{"the tags box", func(r *rig) { r.api.LastInput().Cancel() }},
		{"the options box", func(r *rig) { r.api.LastInput().Accept("json"); r.api.LastInput().Cancel() }},
		{"the list of transforms", func(r *rig) { r.api.LastInput().Accept("json"); r.api.LastInput().Accept(""); r.api.LastPick().Close() }},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			_, uri, text := r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n}\n")
			if err := r.api.Run(AddTagsCommand, nil); err != nil {
				t.Fatal(err)
			}
			st.stop(r)
			if _, open := r.api.DocumentText(uri); open || len(r.api.Applied()) != 0 {
				t.Errorf("the document changed: %q", r.docTextOrEmpty(uri))
			}
			if got, _ := os.ReadFile(strings.TrimPrefix(uri, "file://")); string(got) != text {
				t.Errorf("the file changed: %q", got)
			}
		})
	}
}

func (r *rig) docTextOrEmpty(uri string) string { t, _ := r.api.DocumentText(uri); return t }

func TestRemoveTagsAsksAndClearsEverythingWhenTheAnswerIsEmpty(t *testing.T) {
	r := newRig(t, rigOptions{})
	_, uri, _ := r.focus("a.go", "package p\n\ntype T struct {\n\tA int `json:\"a\"`§\n\tB string `xml:\"b\"`\n}\n")
	if err := r.api.Run(RemoveTagsCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.api.LastInput().Accept("")
	r.api.LastInput().Accept("")
	if len(r.api.Picks()) != 0 {
		t.Error("removing tags asked for a transform")
	}
	if got := r.docText(uri); got != "package p\n\ntype T struct {\n\tA int\n\tB string\n}\n" {
		t.Errorf("got:\n%s", got)
	}
}

func TestTheSettingAndTheArgumentsOfTheTagCommands(t *testing.T) {
	r := newRig(t, rigOptions{settings: map[string]any{
		AddTagsSetting:    map[string]any{"tags": "json", "options": "json=omitempty", "transform": "camelcase", "template": ""},
		RemoveTagsSetting: map[string]any{"tags": "json"},
	}})
	_, uri, _ := r.focus("a.go", "package p\n\ntype T struct {\n\tUserName string§\n}\n")
	// the setting alone: nothing is asked
	if err := r.api.Run(AddTagsCommand, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.api.Inputs()) != 0 {
		t.Fatal("asked, with the tags in the setting")
	}
	if got := r.docText(uri); !strings.Contains(got, "`json:\"userName,omitempty\"`") {
		t.Fatalf("got:\n%s", got)
	}
	// the setting removes the tag
	if err := r.api.Run(RemoveTagsCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.docText(uri); strings.Contains(got, "json") {
		t.Fatalf("got:\n%s", got)
	}
	// the arguments of the command win over the setting
	if err := r.api.Run(AddTagsCommand, map[string]any{"tags": "yaml", "options": "", "transform": "keep"}); err != nil {
		t.Fatal(err)
	}
	if got := r.docText(uri); !strings.Contains(got, "`yaml:\"UserName\"`") {
		t.Fatalf("got:\n%s", got)
	}
	// and promptForTags in them asks again
	if err := r.api.Run(AddTagsCommand, map[string]any{"promptForTags": true, "tags": "xml"}); err != nil {
		t.Fatal(err)
	}
	if in := r.api.LastInput(); in == nil || in.Options().Value != "xml" {
		t.Errorf("the box = %+v", in)
	}
}

func TestTheTagCommandsTellWhatIsWrong(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(r *rig)
		id       string
		args     map[string]any
		severity string
		contains string
	}{
		{"no editor", func(*rig) {}, AddTagsCommand, nil, "info", "needs a Go file"},
		{"the cursor is not in a struct", func(r *rig) { r.focus("a.go", "package p\n\nfunc f() {§}\n") }, AddTagsCommand, map[string]any{"tags": "json"}, "warn", "not in a struct"},
		{"the tags are there already", func(r *rig) { r.focus("a.go", "package p\n\ntype T struct {\n\tA int `json:\"a\"`§\n}\n") }, AddTagsCommand, map[string]any{"tags": "json"}, "info", "as they should be already"},
		{"a tag that cannot be read", func(r *rig) { r.focus("a.go", "package p\n\ntype T struct {\n\tA int `json`§\n}\n") }, AddTagsCommand, map[string]any{"tags": "json"}, "warn", "cannot be read"},
		{"the file has a syntax error", func(r *rig) { r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n") }, RemoveTagsCommand, map[string]any{"tags": "json"}, "warn", "syntax"},
		{"an option with no equal sign", func(r *rig) { r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n}\n") }, AddTagsCommand, map[string]any{"tags": "json", "options": "omitempty"}, "warn", "tag=option"},
		{"a transform that does not exist", func(r *rig) { r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n}\n") }, AddTagsCommand, map[string]any{"tags": "json", "transform": "shout"}, "warn", "transform"},
		{"only options and none of the tags are there", func(r *rig) { r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n}\n") }, AddTagsCommand, map[string]any{"options": "json=omitempty"}, "info", "as they should be already"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			tt.setup(r)
			if err := r.api.Run(tt.id, tt.args); err != nil {
				t.Fatal(err)
			}
			sev, msg := r.lastNote()
			if sev != tt.severity || !strings.Contains(msg, tt.contains) {
				t.Errorf("notification = %s %q, want %s containing %q", sev, msg, tt.severity, tt.contains)
			}
			if len(r.api.Applied()) != 0 {
				t.Errorf("an edit was applied: %+v", r.api.Applied())
			}
		})
	}
}

func TestTheTagCommandsAreForGoFilesOnly(t *testing.T) {
	r := newRig(t, rigOptions{})
	uri := fileURI(filepath.Join(r.folder, "go.mod"))
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: "go.mod"})
	r.api.Emit(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: uri, Selections: []extension.Selection{{}}})
	for _, id := range []string{AddTagsCommand, RemoveTagsCommand, FillStructCommand, GenerateTestsCommand, ToggleTestFileCommand} {
		if err := r.api.Run(id, map[string]any{"tags": "json"}); err != nil {
			t.Fatal(err)
		}
		if sev, msg := r.lastNote(); sev != "info" || !strings.Contains(msg, "needs a Go file") {
			t.Errorf("%s: notification = %s %q", id, sev, msg)
		}
	}
}

func TestAddTagsWithGoplsSendsItsCommand(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.api.SetServerCommands(ServerID, []string{"gopls.add_import", modifyTagsCommand})
	// the line before the field has a letter that takes two units in UTF-16: the server counts in those
	_, uri, _ := r.focus("a.go", "package p\n\n// 😀 is 2 UTF-16 units\ntype T struct {\n\tA int\n\t‹B string\n\tC bool›\n}\n")
	err := r.api.Run(AddTagsCommand, map[string]any{"tags": "json,xml", "options": "json=omitempty", "transform": "camelcase", "template": "col:{field}"})
	if err != nil {
		t.Fatal(err)
	}
	calls := r.api.ServerCalls()
	if len(calls) != 1 || calls[0].Server != ServerID || calls[0].Command != modifyTagsCommand || len(calls[0].Args) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	got := calls[0].Args[0].(map[string]any)
	want := map[string]any{
		"Modification": "add", "URI": uri, "Add": "json,xml", "AddOptions": "json=omitempty", "Transform": "camelcase", "ValueFormat": "col:{field}",
		"Range": map[string]any{
			"start": map[string]any{"line": 5, "character": 1},
			"end":   map[string]any{"line": 6, "character": 7},
		},
	}
	if !equalJSON(t, got, want) {
		t.Errorf("args = %v\nwant %v", got, want)
	}
	// the document is the server's to change: the extension did not
	if len(r.api.Applied()) != 0 {
		t.Error("the extension edited the document itself")
	}
	r.api.Pump()
	if len(r.api.Notes()) != 0 {
		t.Errorf("notes = %+v", r.api.Notes())
	}
}

func TestRemoveTagsWithGoplsSendsItsCommand(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want map[string]any
	}{
		{"some tags and options", map[string]any{"tags": "json,xml", "options": "xml=attr"},
			map[string]any{"Modification": "remove", "Remove": "json,xml", "RemoveOptions": "xml=attr", "Clear": false, "ClearOptions": false}},
		{"all", map[string]any{"promptForTags": false, "tags": "", "options": ""},
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			r.api.SetServerCommands(ServerID, []string{modifyTagsCommand})
			_, uri, _ := r.focus("a.go", "package p\n\ntype T struct {\n\tA int `json:\"a\"`§\n}\n")
			if err := r.api.Run(RemoveTagsCommand, tt.args); err != nil {
				t.Fatal(err)
			}
			if tt.want == nil { // nothing to remove by name: the user is asked, and the empty answers clear it all
				r.api.LastInput().Accept("")
				r.api.LastInput().Accept("")
				tt.want = map[string]any{"Modification": "remove", "Remove": "", "RemoveOptions": "", "Clear": true, "ClearOptions": true}
			}
			calls := r.api.ServerCalls()
			if len(calls) != 1 {
				t.Fatalf("calls = %+v", calls)
			}
			got := calls[0].Args[0].(map[string]any)
			tt.want["URI"], tt.want["Transform"], tt.want["ValueFormat"] = uri, "", ""
			tt.want["Range"] = map[string]any{"start": map[string]any{"line": 3, "character": 17}, "end": map[string]any{"line": 3, "character": 17}}
			if !equalJSON(t, got, tt.want) {
				t.Errorf("args = %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestGoplsFailingToModifyTagsIsReported(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.api.SetServerCommands(ServerID, []string{modifyTagsCommand})
	r.api.OnServerCommand(func(fakeapi.ServerCall) error { return errString("no enclosing struct type") })
	r.focus("a.go", "package p\n\nfunc f() {§}\n")
	if err := r.api.Run(AddTagsCommand, map[string]any{"tags": "json"}); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if sev, msg := r.lastNote(); sev != "error" || !strings.Contains(msg, "gopls failed: no enclosing struct type") {
		t.Errorf("notification = %s %q", sev, msg)
	}
}

// errString is an error that is only a text.
type errString string

func (e errString) Error() string { return string(e) }

func TestWithoutTheCommandOfGoplsTheTagsAreChangedHere(t *testing.T) {
	r := newRig(t, rigOptions{})
	// gopls runs, but this version has no modify_tags
	r.api.SetServerCommands(ServerID, []string{"gopls.add_import"})
	_, uri, _ := r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n}\n")
	if err := r.api.Run(AddTagsCommand, map[string]any{"tags": "json"}); err != nil {
		t.Fatal(err)
	}
	if len(r.api.ServerCalls()) != 0 || !strings.Contains(r.docText(uri), "`json:\"a\"`") {
		t.Errorf("server calls %+v, document:\n%s", r.api.ServerCalls(), r.docText(uri))
	}
	// the edit is the field line alone, so that the cursor stays where it is
	applied := r.api.Applied()
	if len(applied) != 1 || len(applied[0].Changes) != 1 || len(applied[0].Changes[0].Edits) != 1 {
		t.Fatalf("applied = %+v", applied)
	}
	e := applied[0].Changes[0].Edits[0]
	if e.Range.Start.Line != 3 || e.Range.End.Line != 3 || e.Text != " `json:\"a\"`" {
		t.Errorf("edit = %+v", e)
	}
}

func TestTheTagsAreReadAgainAfterThePrompts(t *testing.T) {
	r := newRig(t, rigOptions{})
	_, uri, _ := r.focus("a.go", "package p\n\ntype T struct {\n\tA int§\n}\n")
	if err := r.api.Run(AddTagsCommand, nil); err != nil {
		t.Fatal(err)
	}
	// while the boxes are open the user types in the document: a field before the cursor, and the cursor moves
	r.api.SetDocument(uri, "package p\n\n// moved\ntype T struct {\n\tA int\n}\n")
	r.api.Emit(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: uri, Selections: []extension.Selection{{Anchor: pos(4, 5), Active: pos(4, 5)}}})
	r.api.LastInput().Accept("json")
	r.api.LastInput().Accept("")
	r.api.LastPick().Choose(0)
	if got := r.docText(uri); got != "package p\n\n// moved\ntype T struct {\n\tA int `json:\"a\"`\n}\n" {
		t.Errorf("got:\n%s", got)
	}
}

func TestFillStructAppliesTheCodeActionOfGopls(t *testing.T) {
	tests := []struct {
		name     string
		state    extension.ServerState
		answer   func(fakeapi.CodeActionCall) (string, error)
		calls    int
		severity string
		contains string
		reversed bool
	}{
		{"the action is applied", extension.ServerRunning, func(fakeapi.CodeActionCall) (string, error) { return "Fill T", nil }, 1, "", "", false},
		{"the selection runs backwards", extension.ServerRunning, func(fakeapi.CodeActionCall) (string, error) { return "Fill T", nil }, 1, "", "", true},
		{"nothing to fill", extension.ServerRunning, func(fakeapi.CodeActionCall) (string, error) { return "", nil }, 1, "info", "offers nothing to fill", false},
		{"gopls fails", extension.ServerRunning, func(fakeapi.CodeActionCall) (string, error) { return "", errString("boom") }, 1, "error", "gopls failed: boom", false},
		{"gopls is starting", extension.ServerStarting, nil, 0, "warn", "needs gopls, which is starting", false},
		{"gopls is missing", extension.ServerMissing, nil, 0, "warn", "not installed", false},
		{"gopls crashed", extension.ServerFailed, nil, 0, "warn", "stopped after crashing", false},
		{"gopls is idle", extension.ServerIdle, nil, 0, "warn", "not running", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			r.api.SetServerStatus(ServerID, extension.ServerStatus{State: tt.state})
			r.api.OnCodeAction(tt.answer)
			src := "package p\n\nvar _ = T{‹A: 1›}\n"
			_, uri, _ := r.focus("a.go", src)
			if tt.reversed {
				r.api.Emit(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: uri, Selections: []extension.Selection{{Anchor: pos(2, 14), Active: pos(2, 10)}}})
			}
			if err := r.api.Run(FillStructCommand, nil); err != nil {
				t.Fatal(err)
			}
			r.api.Pump()
			calls := r.api.CodeActionCalls()
			if len(calls) != tt.calls {
				t.Fatalf("calls = %+v", calls)
			}
			if tt.calls == 1 {
				c := calls[0]
				want := extension.Range{Start: pos(2, 10), End: pos(2, 14)}
				if c.Server != ServerID || c.URI != uri || !slices.Equal(c.Req.Only, []string{"refactor.rewrite.fillStruct"}) || c.Req.Range != want {
					t.Errorf("call = %+v", c)
				}
			}
			sev, msg := r.lastNote()
			if sev != tt.severity || !strings.Contains(msg, tt.contains) {
				t.Errorf("notification = %s %q, want %s containing %q", sev, msg, tt.severity, tt.contains)
			}
		})
	}
}

func TestFillStructNeedsASelectionKnown(t *testing.T) {
	r := newRig(t, rigOptions{})
	uri := fileURI(filepath.Join(r.folder, "a.go"))
	if err := os.WriteFile(filepath.Join(r.folder, "a.go"), []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: "go"})
	for _, id := range []string{FillStructCommand, AddTagsCommand, RemoveTagsCommand} {
		if err := r.api.Run(id, nil); err != nil {
			t.Fatal(err)
		}
		if sev, msg := r.lastNote(); sev != "info" || !strings.Contains(msg, "click in the struct") {
			t.Errorf("%s: notification = %s %q", id, sev, msg)
		}
	}
}

func TestToggleTestFile(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		src     string
		exist   map[string]string
		want    string
		created string
	}{
		{"a file to its test file", "foo.go", "package foo\n§", map[string]string{"foo_test.go": "package foo\n"}, "foo_test.go", ""},
		{"a test file to its file", "foo_test.go", "package foo\n§", map[string]string{"foo.go": "package foo\n"}, "foo.go", ""},
		{"a test file that is not there is made with the package clause", "foo.go", "package foo\n\nfunc F() {}\n§", nil, "foo_test.go", "package foo\n"},
		{"a file that is not there is made for a test file", "foo_test.go", "package foo\n§", nil, "foo.go", "package foo\n"},
		{"the package of an external test is the package of the file", "foo_test.go", "package foo_test\n§", nil, "foo.go", "package foo\n"},
		{"the package of a main file", "main.go", "package main\n§", nil, "main_test.go", "package main\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			for name, text := range tt.exist {
				if err := os.WriteFile(filepath.Join(r.folder, name), []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			r.focus(tt.file, tt.src)
			if err := r.api.Run(ToggleTestFileCommand, nil); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(r.folder, tt.want)
			shown := r.api.ShownDocuments()
			if len(shown) != 1 || shown[0].URI != fileURI(target) || shown[0].Line != 1 || shown[0].Preview {
				t.Fatalf("shown = %+v, want %s", shown, target)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if tt.created != "" {
				if string(data) != tt.created {
					t.Errorf("the new file = %q, want %q", data, tt.created)
				}
				if sev, msg := r.lastNote(); sev != "info" || !strings.Contains(msg, "did not exist") {
					t.Errorf("notification = %s %q", sev, msg)
				}
			} else if len(r.api.Notes()) != 0 {
				t.Errorf("notes = %+v", r.api.Notes())
			}
			// toggling back shows the file we came from, which is there now
			r.focus(tt.want, "package x\n§")
			if err := r.api.Run(ToggleTestFileCommand, nil); err != nil {
				t.Fatal(err)
			}
			if shown := r.api.ShownDocuments(); len(shown) != 2 || shown[1].URI != fileURI(filepath.Join(r.folder, tt.file)) {
				t.Errorf("shown = %+v", shown)
			}
		})
	}
}

func TestToggleTestFileFindsADocumentThatIsOpenButNotSaved(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.focus("foo.go", "package foo\n§")
	// the test file has unsaved text and no file on disk: it is shown as it is, and nothing is made
	testURI := fileURI(filepath.Join(r.folder, "foo_test.go"))
	r.api.SetDocument(testURI, "package foo\n\n// unsaved\n")
	if err := r.api.Run(ToggleTestFileCommand, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.folder, "foo_test.go")); err == nil {
		t.Error("a file was made over an open document")
	}
	if shown := r.api.ShownDocuments(); len(shown) != 1 || shown[0].URI != testURI || len(r.api.Notes()) != 0 {
		t.Errorf("shown %+v, notes %+v", shown, r.api.Notes())
	}
}

func TestToggleTestFileWithoutAPackageClause(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.focus("foo.go", "// nothing here§\n")
	if err := r.api.Run(ToggleTestFileCommand, nil); err != nil {
		t.Fatal(err)
	}
	if sev, msg := r.lastNote(); sev != "warn" || !strings.Contains(msg, "no package clause") {
		t.Errorf("notification = %s %q", sev, msg)
	}
	if _, err := os.Stat(filepath.Join(r.folder, "foo_test.go")); err == nil || len(r.api.ShownDocuments()) != 0 {
		t.Errorf("a file was made or shown (%v)", err)
	}
}

func TestTheCommandsAreDeclaredAndHandled(t *testing.T) {
	r := newRig(t, rigOptions{})
	m := New().Manifest()
	titles := map[string]string{
		AddTagsCommand: "Add Tags To Struct Fields", RemoveTagsCommand: "Remove Tags From Struct Fields", FillStructCommand: "Fill Struct",
		GenerateTestsCommand: "Generate Unit Tests For Function", ToggleTestFileCommand: "Toggle Test File", RunCommand: "Run",
	}
	declared := map[string]extension.CommandSpec{}
	for _, c := range m.Contributes.Commands {
		declared[c.ID] = c
	}
	for id, title := range titles {
		if c := declared[id]; c.Title != title || c.Category != "Go" || c.Description == "" {
			t.Errorf("%s = %+v", id, c)
		}
		if !r.api.HasHandler(id) {
			t.Errorf("%s has no handler", id)
		}
	}
	// the context menu of the editor, for Go files: where VS Code's Go extension puts them
	inMenu := map[string]bool{}
	for _, it := range m.Contributes.Menus {
		if it.Menu != extension.MenuEditorContext || !strings.Contains(it.When, "editorLangId == go") || !strings.Contains(it.When, "editorTextFocus") {
			t.Errorf("menu item %+v", it)
		}
		inMenu[it.Command] = true
	}
	for _, id := range []string{AddTagsCommand, RemoveTagsCommand, FillStructCommand, GenerateTestsCommand, ToggleTestFileCommand} {
		if !inMenu[id] {
			t.Errorf("%s is not in the context menu", id)
		}
	}
	if inMenu[RunCommand] {
		t.Error("Run is in the context menu")
	}
}
