package exthost

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// What an extension does with its language server beyond following it (step 14.2): the commands the server announced,
// workspace/executeCommand, "apply the first code action of a kind", Documents().Apply and the input box.

const commandCaps = `{"textDocumentSync":2,"executeCommandProvider":{"commands":["fake.two","fake.one"]},` +
	`"codeActionProvider":{"resolveProvider":true,"codeActionKinds":["refactor.rewrite.fillStruct","refactor.extract"]}}`

// editJSON is a WorkspaceEdit of the server that replaces the first line "package a" of a.go with text.
func editJSON(text string) string {
	return `{"changes":{"file://{{folder}}/a.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":9}},"newText":"` + text + `"}]}}`
}

func commandScript() lsptest.Script {
	return lsptest.Script{Capabilities: raw(commandCaps), Responses: map[string]lsptest.Response{lsp.MethodExecuteCommand: {}}}
}

// waitDone pumps the tab until done reported.
func waitDone(t *testing.T, tb *tab, what string, reported *bool) {
	t.Helper()
	waitFor(t, tb, what, func() bool { return *reported })
}

func TestTheExtensionListsTheCommandsOfItsServer(t *testing.T) {
	st := newServerTest(t, commandScript(), nil)
	tb := st.startTab("one")
	// the server is not running: nothing is announced
	if got, err := st.api.LanguageServers().Commands(testServer); err != nil || len(got) != 0 {
		t.Errorf("before the server runs: %v, %v", got, err)
	}
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	got, err := st.api.LanguageServers().Commands(testServer)
	if err != nil || !slices.Equal(got, []string{"fake.one", "fake.two"}) {
		t.Errorf("Commands = %v, %v; want the announced commands, sorted", got, err)
	}
	_, err = st.api.LanguageServers().Commands("nothing")
	wantRoute(t, err, "serversAPI.Commands.ExtensionServerUnknown")
}

func TestTheCommandsOfTheServerIncludeThoseItRegistered(t *testing.T) {
	script := commandScript()
	script.Send = []lsptest.Outgoing{{
		After: lsp.MethodInitialized, Request: true, Method: "client/registerCapability",
		Params: raw(`{"registrations":[{"id":"r1","method":"workspace/executeCommand","registerOptions":{"commands":["fake.late","fake.one"]}}]}`),
	}}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	waitFor(t, tb, "the registration", func() bool {
		got, _ := st.api.LanguageServers().Commands(testServer)
		return slices.Contains(got, "fake.late")
	})
	got, _ := st.api.LanguageServers().Commands(testServer)
	if !slices.Equal(got, []string{"fake.late", "fake.one", "fake.two"}) {
		t.Errorf("Commands = %v", got)
	}
}

func TestAnExtensionExecutesACommandOfItsServer(t *testing.T) {
	script := commandScript()
	script.Send = []lsptest.Outgoing{{
		After: lsp.MethodExecuteCommand, Request: true, Method: lsp.MethodApplyEdit,
		Params: raw(`{"label":"command","edit":` + editJSON("PACKAGE a") + `}`),
	}}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	a := st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	var done bool
	var told error
	args := []any{map[string]any{"Modification": "add", "Add": "json", "Range": map[string]any{"start": map[string]any{"line": 1}}}, "second"}
	err := st.api.LanguageServers().ExecuteCommand(context.Background(), testServer, "fake.one", args, func(err error) { done, told = true, err })
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, tb, "the end of the command", &done)
	if told != nil {
		t.Errorf("done got %v", told)
	}
	if a.Text() != "PACKAGE a\n" {
		t.Errorf("the edit the server made while it ran the command: %q", a.Text())
	}
	// the server got the command and the arguments as JSON
	var got struct {
		Params lsp.ExecuteCommandParams `json:"params"`
	}
	for _, m := range traffic(t, st.controller(tb)) {
		if m.FromClient && m.Method == lsp.MethodExecuteCommand {
			if err := json.Unmarshal(m.Body, &got); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got.Params.Command != "fake.one" || len(got.Params.Arguments) != 2 || string(got.Params.Arguments[1]) != `"second"` ||
		!strings.Contains(string(got.Params.Arguments[0]), `"Modification":"add"`) {
		t.Errorf("the server got %+v", got.Params)
	}
}

func TestACommandTheServerRefusesIsReportedToTheExtension(t *testing.T) {
	script := commandScript()
	script.Responses[lsp.MethodExecuteCommand] = lsptest.Response{Error: &lsp.ResponseError{Code: -32603, Message: "no such tag"}}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	var done bool
	var told error
	if err := st.api.LanguageServers().ExecuteCommand(context.Background(), testServer, "fake.one", nil, func(err error) { done, told = true, err }); err != nil {
		t.Fatal(err)
	}
	waitDone(t, tb, "the end of the command", &done)
	if told == nil || !strings.Contains(told.Error(), "no such tag") {
		t.Errorf("done got %v", told)
	}
}

func TestExecutingACommandFailsInTheCasesTheAPILists(t *testing.T) {
	st := newServerTest(t, commandScript(), nil)
	tb := st.startTab("one")
	ls := st.api.LanguageServers()
	ctx := context.Background()
	// the server is not running yet: done is told, later
	var told error
	var done bool
	if err := ls.ExecuteCommand(ctx, testServer, "fake.one", nil, func(err error) { done, told = true, err }); err != nil {
		t.Fatal(err)
	}
	if done {
		t.Error("done was called before the call returned")
	}
	waitDone(t, tb, "done", &done)
	wantRoute(t, told, "serversAPI.ExecuteCommand.ExtensionServerNotRunning")
	// arguments that do not encode
	wantRoute(t, ls.ExecuteCommand(ctx, testServer, "fake.one", []any{make(chan int)}, nil), "serversAPI.ExecuteCommand.ExtensionArgumentsInvalid")
	// an id the manifest does not declare, and no function to call
	wantRoute(t, ls.ExecuteCommand(ctx, "nothing", "fake.one", nil, nil), "serversAPI.ExecuteCommand.ExtensionServerUnknown")
	if err := ls.ExecuteCommand(ctx, testServer, "fake.one", nil, nil); err != nil {
		t.Errorf("no function to call is fine: %v", err)
	}
	tb.pump()
	// after the activation
	st.rig.host.Shutdown()
	wantRoute(t, ls.ExecuteCommand(ctx, testServer, "fake.one", nil, nil), "serversAPI.ExecuteCommand.ExtensionDeactivated")
	wantRoute(t, ls.ApplyCodeAction(ctx, testServer, "file:///a.go", extension.CodeActionRequest{}, nil), "serversAPI.ApplyCodeAction.ExtensionDeactivated")
}

func TestAnExtensionAppliesTheFirstCodeActionOfAKind(t *testing.T) {
	fill := `{"title":"Fill Foo","kind":"refactor.rewrite.fillStruct","edit":` + editJSON("package FILLED") + `}`
	extract := `{"title":"Extract","kind":"refactor.extract","edit":` + editJSON("package EXTRACTED") + `}`
	tests := []struct {
		name      string
		actions   string
		resolve   *lsptest.Response
		only      []string
		wantTitle string
		wantText  string
		wantCmds  int
		wantErr   string
	}{
		{"the action of the kind", `[` + extract + `,` + fill + `]`, nil, []string{"refactor.rewrite.fillStruct"}, "Fill Foo", "package FILLED\n", 0, ""},
		{"a kind below the one asked for", `[` + fill + `]`, nil, []string{"refactor.rewrite"}, "Fill Foo", "package FILLED\n", 0, ""},
		{"every kind", `[` + extract + `,` + fill + `]`, nil, nil, "Extract", "package EXTRACTED\n", 0, ""},
		{"the preferred one before the others", `[` + extract + `,` + strings.Replace(extract, `"Extract"`, `"Best","isPreferred":true`, 1) + `]`, nil, nil, "Best", "package EXTRACTED\n", 0, ""},
		{"a disabled one is passed over", `[` + strings.Replace(fill, `"Fill Foo"`, `"Fill Off","disabled":{"reason":"no"}`, 1) + `,` + extract + `]`, nil, nil, "Extract", "package EXTRACTED\n", 0, ""},
		{"none of the kind", `[` + extract + `]`, nil, []string{"refactor.rewrite.fillStruct"}, "", "package a\n", 0, ""},
		{"none at all", `null`, nil, []string{"refactor.rewrite.fillStruct"}, "", "package a\n", 0, ""},
		{"an edit the server resolves", `[{"title":"Fill Foo","kind":"refactor.rewrite.fillStruct","data":{"x":1}}]`,
			&lsptest.Response{Result: raw(`{"title":"Fill Foo","kind":"refactor.rewrite.fillStruct","edit":` + editJSON("package RESOLVED") + `}`)},
			[]string{"refactor.rewrite.fillStruct"}, "Fill Foo", "package RESOLVED\n", 0, ""},
		{"a command", `[{"title":"Fill Foo","kind":"refactor.rewrite.fillStruct","command":{"title":"Fill Foo","command":"fake.one","arguments":[7]}}]`, nil,
			[]string{"refactor.rewrite.fillStruct"}, "Fill Foo", "package a\n", 1, ""},
		{"an edit and a command", `[{"title":"Fill Foo","kind":"refactor.rewrite.fillStruct","edit":` + editJSON("package BOTH") + `,"command":{"title":"x","command":"fake.two"}}]`, nil,
			nil, "Fill Foo", "package BOTH\n", 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := commandScript()
			script.Responses[lsp.MethodCodeAction] = lsptest.Response{Result: raw(tt.actions)}
			if tt.resolve != nil {
				script.Responses[lsp.MethodCodeActionResolve] = *tt.resolve
			}
			st := newServerTest(t, script, nil)
			tb := st.startTab("one")
			a := st.goDoc(tb, "a.go", "package a\n")
			st.waitRunning(tb)
			var done bool
			var title string
			var told error
			req := extension.CodeActionRequest{Range: extension.Range{Start: extension.Position{Line: 0, Col: 3}, End: extension.Position{Line: 0, Col: 3}}, Only: tt.only}
			if err := st.api.LanguageServers().ApplyCodeAction(context.Background(), testServer, a.URI().String(), req, func(ti string, err error) { done, title, told = true, ti, err }); err != nil {
				t.Fatal(err)
			}
			waitDone(t, tb, "the code action", &done)
			if told != nil || title != tt.wantTitle || a.Text() != tt.wantText {
				t.Errorf("title %q, error %v, text %q; want %q, nil, %q", title, told, a.Text(), tt.wantTitle, tt.wantText)
			}
			if n := countMethod(t, st.controller(tb), lsp.MethodExecuteCommand); n != tt.wantCmds {
				t.Errorf("the server was asked to execute %d commands, want %d", n, tt.wantCmds)
			}
			// the server was asked for the kinds, at the range of the request
			var params lsp.CodeActionParams
			for _, m := range traffic(t, st.controller(tb)) {
				if m.FromClient && m.Method == lsp.MethodCodeAction {
					var body struct {
						Params lsp.CodeActionParams `json:"params"`
					}
					if err := json.Unmarshal(m.Body, &body); err != nil {
						t.Fatal(err)
					}
					params = body.Params
				}
			}
			if !slices.Equal(params.Context.Only, tt.only) || params.Range.Start.Character != 3 || params.Range.Start.Line != 0 {
				t.Errorf("the server was asked %+v", params)
			}
		})
	}
}

func TestACodeActionThatCannotBeAppliedIsReported(t *testing.T) {
	script := commandScript()
	script.Responses[lsp.MethodCodeAction] = lsptest.Response{Error: &lsp.ResponseError{Code: -32603, Message: "gopls broke"}}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	ls := st.api.LanguageServers()
	ctx := context.Background()
	req := extension.CodeActionRequest{Only: []string{"refactor"}}
	var done bool
	var told error
	wait := func(uri string) error {
		done, told = false, nil
		if err := ls.ApplyCodeAction(ctx, testServer, uri, req, func(_ string, err error) { done, told = true, err }); err != nil {
			t.Fatal(err)
		}
		waitDone(t, tb, "the code action", &done)
		return told
	}
	// the server is not running
	wantRoute(t, wait("file://"+st.folder+"/a.go"), "serversAPI.ApplyCodeAction.ExtensionServerNotRunning")
	a := st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	// the server fails
	if err := wait(a.URI().String()); err == nil || !strings.Contains(err.Error(), "gopls broke") {
		t.Errorf("done got %v", err)
	}
	// a document that is not open, and a URI that is not one
	wantRoute(t, wait("file://"+filepath.Join(st.folder, "other.go")), "serversAPI.ApplyCodeAction.ExtensionDocumentUnavailable")
	wantRoute(t, wait("a.go"), "serversAPI.ApplyCodeAction->ParseURI.URIInvalid")
	wantRoute(t, ls.ApplyCodeAction(ctx, "nothing", a.URI().String(), req, nil), "serversAPI.ApplyCodeAction.ExtensionServerUnknown")
}

func TestAnEditThatCannotBeAppliedFailsTheCodeAction(t *testing.T) {
	script := commandScript()
	script.Responses[lsp.MethodCodeAction] = lsptest.Response{Result: raw(
		`[{"title":"Broken","kind":"refactor.rewrite.fillStruct","edit":{"changes":{"file://{{folder}}/missing.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"x"}]}}}]`)}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	a := st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	var done bool
	var title string
	var told error
	if err := st.api.LanguageServers().ApplyCodeAction(context.Background(), testServer, a.URI().String(), extension.CodeActionRequest{}, func(ti string, err error) { done, title, told = true, ti, err }); err != nil {
		t.Fatal(err)
	}
	waitDone(t, tb, "the code action", &done)
	if told == nil || title != "Broken" || a.Text() != "package a\n" {
		t.Errorf("title %q, error %v, text %q", title, told, a.Text())
	}
}

func TestDocumentsApplyEditsAndCreatesLikeARename(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	a := st.goDoc(tb, "a.go", "package a\n")
	newFile := filepath.Join(st.folder, "a_test.go")
	rng := func(l1, c1, l2, c2 int) extension.Range {
		return extension.Range{Start: extension.Position{Line: l1, Col: c1}, End: extension.Position{Line: l2, Col: c2}}
	}
	err := st.api.Documents().Apply(context.Background(), extension.WorkspaceEdit{Changes: []extension.ResourceChange{
		{Kind: "edit", URI: a.URI().String(), Edits: []extension.RangeEdit{{Range: rng(0, 0, 0, 7), Text: "PACKAGE"}, {Range: rng(1, 0, 1, 0), Text: "// end\n"}}},
		{Kind: "create", URI: "file://" + newFile},
		{Kind: "edit", URI: "file://" + newFile, Edits: []extension.RangeEdit{{Range: rng(0, 0, 0, 0), Text: "package a\n"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Text() != "PACKAGE a\n// end\n" {
		t.Errorf("a.go = %q", a.Text())
	}
	// the document is one undo step
	if !a.CanUndo() {
		t.Error("the edit left nothing to undo")
	}
	if !slices.Contains(tb.lsp.edits.log, "create "+newFile) {
		t.Errorf("the window did not create the file: %v", tb.lsp.edits.log)
	}
	// the new document is open in the window with the text
	d, ok := tb.lsp.edits.Document(document.FileURI(newFile))
	if !ok || d.Text() != "package a\n" {
		t.Errorf("the new document = %v, %v", d, ok)
	}
}

func TestDocumentsApplyRefusals(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	st.startTab("one")
	ctx := context.Background()
	wantRoute(t, st.api.Documents().Apply(ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "burn", URI: "file:///x.go"}}}), "documentsAPI.Apply->fromWorkspaceEdit.ExtensionEditInvalid")
	// a change the window cannot make: the document is not there
	err := st.api.Documents().Apply(ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "edit", URI: "file://" + st.folder + "/none.go", Edits: []extension.RangeEdit{{Text: "x"}}}}})
	if err == nil {
		t.Error("an edit of a file that is not there was applied")
	}
	// a tab that cannot edit
	other := newServerTest(t, simple(), nil)
	other.rig.noLSP["one"] = true
	other.startTab("one")
	wantRoute(t, other.api.Documents().Apply(ctx, extension.WorkspaceEdit{}), "documentsAPI.Apply.ExtensionDocumentUnavailable")
	// after the activation
	st.rig.host.Shutdown()
	wantRoute(t, st.api.Documents().Apply(ctx, extension.WorkspaceEdit{}), "documentsAPI.Apply.ExtensionDeactivated")
}

func TestInputBoxAsksForALineOfText(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	type answer struct {
		value string
		ok    bool
	}
	var answers []answer
	done := func(v string, ok bool) { answers = append(answers, answer{v, ok}) }
	var validated []string
	opts := extension.InputBoxOptions{Prompt: "Tags", Value: "json", Validate: func(v string) string {
		validated = append(validated, v)
		if strings.Contains(v, " ") {
			return "no spaces"
		}
		return ""
	}}
	if err := st.api.InputBox().Show(opts, done); err != nil {
		t.Fatal(err)
	}
	req := tb.inputs[0]
	if req.Prompt != "Tags" || req.Value != "json" || req.Validate("a b") != "no spaces" || req.Validate("ab") != "" || !slices.Equal(validated, []string{"a b", "ab"}) {
		t.Fatalf("request = %+v, validated %v", req, validated)
	}
	req.Accepted("xml")
	req.Hidden() // a second answer is ignored
	req.Accepted("again")
	// closed with no text
	if err := st.api.InputBox().Show(extension.InputBoxOptions{Prompt: "More"}, done); err != nil {
		t.Fatal(err)
	}
	if tb.inputs[1].Validate != nil {
		t.Error("a box with no validation got one")
	}
	tb.inputs[1].Hidden()
	// no function to call is fine
	if err := st.api.InputBox().Show(extension.InputBoxOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	tb.inputs[2].Accepted("x")
	if !slices.Equal(answers, []answer{{"xml", true}, {"", false}}) {
		t.Errorf("answers = %v", answers)
	}
}

func TestInputBoxRefusals(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	st.rig.noInput["one"] = true
	st.startTab("one")
	wantRoute(t, st.api.InputBox().Show(extension.InputBoxOptions{}, nil), "inputBoxAPI.Show.ExtensionInputBoxUnavailable")
	st.rig.host.Shutdown()
	wantRoute(t, st.api.InputBox().Show(extension.InputBoxOptions{}, nil), "inputBoxAPI.Show.ExtensionDeactivated")
}

func TestACallbackOfTheNewAPIsThatPanicsFailsTheExtension(t *testing.T) {
	tests := []struct {
		name  string
		setup func(st *serverTest)
		route string
		tag   string
		fire  func(st *serverTest, tb *tab)
	}{
		{
			name: "the end of an input box",
			setup: func(st *serverTest) {
				st.onActivate = func(_ context.Context, a extension.API) error {
					return a.InputBox().Show(extension.InputBoxOptions{}, func(string, bool) { panic("boom") })
				}
			},
			route: "Scope.runInputBoxDone.func1.ExtensionInputBoxCallbackPanicked", tag: "ExtensionInputBoxCallbackPanicked",
			fire: func(_ *serverTest, tb *tab) { tb.inputs[0].Accepted("x") },
		},
		{
			name: "the validation of an input box",
			setup: func(st *serverTest) {
				st.onActivate = func(_ context.Context, a extension.API) error {
					return a.InputBox().Show(extension.InputBoxOptions{Validate: func(string) string { panic("boom") }}, nil)
				}
			},
			route: "Scope.runInputBoxValidate.func1.ExtensionInputBoxCallbackPanicked", tag: "ExtensionInputBoxCallbackPanicked",
			fire: func(_ *serverTest, tb *tab) { tb.inputs[0].Validate("x") },
		},
		{
			name: "the end of a command of the server",
			setup: func(st *serverTest) {
				st.onActivate = func(_ context.Context, a extension.API) error {
					return a.LanguageServers().ExecuteCommand(context.Background(), testServer, "fake.one", nil, func(error) { panic("boom") })
				}
			},
			route: "Scope.runServerDone.func1.ExtensionServerCallbackPanicked", tag: "ExtensionServerCallbackPanicked",
			fire: func(*serverTest, *tab) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newServerTest(t, commandScript(), nil)
			tt.setup(st)
			tb := st.startTab("one")
			tt.fire(st, tb)
			tb.pump()
			waitFor(t, tb, "the extension to fail", func() bool { return tb.scope.State(testExt) == StateFailed })
			appErr, ok := errors.FromError(tb.scope.Failure(testExt).Err)
			if !ok || appErr.Tag != tt.tag || !appErr.HasRoute(tt.route) {
				t.Errorf("failure = %v, want tag %s and route %s", tb.scope.Failure(testExt).Err, tt.tag, tt.route)
			}
		})
	}
}
