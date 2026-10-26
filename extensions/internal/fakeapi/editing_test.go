package fakeapi

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func rng(l1, c1, l2, c2 int) extension.Range {
	return extension.Range{Start: extension.Position{Line: l1, Col: c1}, End: extension.Position{Line: l2, Col: c2}}
}

func TestApplyRangeEdits(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		edits []extension.RangeEdit
		want  string
	}{
		{"an insertion", "ab\ncd\n", []extension.RangeEdit{{Range: rng(1, 1, 1, 1), Text: "X"}}, "ab\ncXd\n"},
		{"a replacement over lines", "ab\ncd\nef\n", []extension.RangeEdit{{Range: rng(0, 1, 1, 1), Text: "-"}}, "a-d\nef\n"},
		{"two edits, the later first in the text", "ab\ncd\n", []extension.RangeEdit{{Range: rng(0, 0, 0, 1), Text: "A"}, {Range: rng(1, 0, 1, 1), Text: "C"}}, "Ab\nCd\n"},
		{"edits at one place keep their order", "x", []extension.RangeEdit{{Range: rng(0, 1, 0, 1), Text: "1"}, {Range: rng(0, 1, 0, 1), Text: "2"}}, "x12"},
		{"the end of the text", "ab", []extension.RangeEdit{{Range: rng(0, 2, 0, 2), Text: "\nc"}}, "ab\nc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyRangeEdits(tt.text, tt.edits)
			if err != nil || got != tt.want {
				t.Errorf("= %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	_, err := ApplyRangeEdits("ab", []extension.RangeEdit{{Range: rng(3, 0, 3, 0)}})
	route(t, err, "ApplyRangeEdits.func1.FakeEditRange")
}

func TestTheFakeApplyChangesTheOpenTextAndCreatesFiles(t *testing.T) {
	folder := t.TempDir()
	a := New(t, manifest(), extension.ScopeInfo{Folder: folder})
	existing := filepath.Join(folder, "a.go")
	if err := os.WriteFile(existing, []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(folder, "a_test.go")
	ctx := context.Background()
	err := a.Documents().Apply(ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{
		{Kind: "edit", URI: "file://" + existing, Edits: []extension.RangeEdit{{Range: rng(1, 0, 1, 0), Text: "// more\n"}}},
		{Kind: "create", URI: "file://" + created},
		{Kind: "edit", URI: "file://" + created, Edits: []extension.RangeEdit{{Range: rng(0, 0, 0, 0), Text: "package a\n"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := a.DocumentText("file://" + existing); got != "package a\n// more\n" {
		t.Errorf("a.go = %q", got)
	}
	if got, _ := a.DocumentText("file://" + created); got != "package a\n" {
		t.Errorf("a_test.go = %q", got)
	}
	if text, err := a.Documents().Read(ctx, "file://"+created); err != nil || text != "package a\n" {
		t.Errorf("Read = %q, %v", text, err)
	}
	if data, err := os.ReadFile(existing); err != nil || string(data) != "package a\n" {
		t.Errorf("the file on disk changed: %q, %v", data, err)
	}
	if len(a.Applied()) != 1 {
		t.Errorf("applied = %d", len(a.Applied()))
	}
	// a create of a file that exists fails, a document that is not there cannot be edited, a rename is not supported
	route(t, a.Documents().Apply(ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "create", URI: "file://" + existing}}}), "documents.Apply.FakeFileExists")
	route(t, a.Documents().Apply(ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "edit", URI: "file://" + folder + "/none.go"}}}), "documents.Apply.FakeEditFailed")
	route(t, a.Documents().Apply(ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "rename", URI: "file://" + existing}}}), "documents.Apply.FakeEditUnsupported")
	route(t, a.Documents().Apply(ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "create", URI: "file://" + folder + "/no/dir.go"}}}), "documents.Apply.FakeEditFailed")
}

func TestTheFakeServerCommandsAndCodeActions(t *testing.T) {
	a := New(t, serverManifest(), extension.ScopeInfo{})
	ls := a.LanguageServers()
	a.SetServerCommands("ls", []string{"ls.one"})
	if got, err := ls.Commands("ls"); err != nil || !slices.Equal(got, []string{"ls.one"}) {
		t.Errorf("Commands = %v, %v", got, err)
	}
	var told []error
	var titles []string
	if err := ls.ExecuteCommand(context.Background(), "ls", "ls.one", []any{1}, func(err error) { told = append(told, err) }); err != nil {
		t.Fatal(err)
	}
	a.OnServerCommand(func(ServerCall) error { return errors.New("FakeTest", "refused") })
	if err := ls.ExecuteCommand(context.Background(), "ls", "ls.two", nil, func(err error) { told = append(told, err) }); err != nil {
		t.Fatal(err)
	}
	if len(told) != 0 {
		t.Fatal("done was called before Pump")
	}
	a.Pump()
	if len(told) != 2 || told[0] != nil || told[1] == nil {
		t.Errorf("told = %v", told)
	}
	if calls := a.ServerCalls(); len(calls) != 2 || calls[0].Command != "ls.one" || calls[0].Args[0] != 1 {
		t.Errorf("calls = %+v", calls)
	}
	a.OnCodeAction(func(c CodeActionCall) (string, error) { return "Fill " + c.URI, nil })
	if err := ls.ApplyCodeAction(context.Background(), "ls", "file:///x.go", extension.CodeActionRequest{Only: []string{"refactor"}}, func(title string, err error) { titles = append(titles, title) }); err != nil {
		t.Fatal(err)
	}
	a.Pump()
	if !slices.Equal(titles, []string{"Fill file:///x.go"}) || len(a.CodeActionCalls()) != 1 {
		t.Errorf("titles = %v, calls = %+v", titles, a.CodeActionCalls())
	}
	_, e1 := ls.Commands("none")
	e2 := ls.ExecuteCommand(context.Background(), "none", "x", nil, nil)
	e3 := ls.ApplyCodeAction(context.Background(), "none", "x", extension.CodeActionRequest{}, nil)
	route(t, e1, "API.server.FakeServerUnknown")
	route(t, e2, "API.server.FakeServerUnknown")
	route(t, e3, "API.server.FakeServerUnknown")
}

func TestTheFakeInputBox(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	type answer struct {
		v  string
		ok bool
	}
	var got []answer
	opts := extension.InputBoxOptions{Prompt: "Tags", Value: "json", Validate: func(v string) string {
		if v == "bad" {
			return "bad text"
		}
		return ""
	}}
	if err := a.InputBox().Show(opts, func(v string, ok bool) { got = append(got, answer{v, ok}) }); err != nil {
		t.Fatal(err)
	}
	in := a.LastInput()
	if in.Options().Value != "json" || in.Validate("bad") != "bad text" || in.Validate("ok") != "" {
		t.Errorf("options %+v", in.Options())
	}
	in.Accept("xml")
	if err := a.InputBox().Show(extension.InputBoxOptions{Prompt: "More"}, func(v string, ok bool) { got = append(got, answer{v, ok}) }); err != nil {
		t.Fatal(err)
	}
	if a.LastInput().Validate("anything") != "" {
		t.Error("a box with no validation refused a text")
	}
	a.LastInput().Cancel()
	if len(got) != 2 || got[0] != (answer{"xml", true}) || got[1] != (answer{"", false}) || len(a.Inputs()) != 2 {
		t.Errorf("got %v, inputs %d", got, len(a.Inputs()))
	}
	if New(t, manifest(), extension.ScopeInfo{}).LastInput() != nil {
		t.Error("a fake that showed nothing has an input")
	}
}
