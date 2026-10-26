package fakeapi

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The fake of what an extension does to documents and language servers (step 14.2): Documents().Apply, the commands and code
// actions of a server, and the input box.

// SetDocument makes the document with the URI open with the text, as an editor with unsaved changes has it: Documents().Read
// answers with it, and Apply changes it.
func (a *API) SetDocument(uri, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.docs == nil {
		a.docs = map[string]string{}
	}
	a.docs[uri] = text
}

// DocumentText is the text of an open document (one SetDocument made, or Apply opened or created), and whether it is open.
func (a *API) DocumentText(uri string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	text, ok := a.docs[uri]
	return text, ok
}

// Applied are the edits that were applied with Documents().Apply, the first first.
func (a *API) Applied() []extension.WorkspaceEdit {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.applied)
}

// Apply implements extension.Documents the way the window does for what the tests look at: a create makes the empty file on disk
// and opens it, an edit changes the open text (a document that is not open is read from disk and stays open, with the change
// unsaved), a rename and a delete are done on disk.
func (d documents) Apply(_ context.Context, edit extension.WorkspaceEdit) error {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.docs == nil {
		a.docs = map[string]string{}
	}
	a.applied = append(a.applied, edit)
	for _, c := range edit.Changes {
		switch c.Kind {
		case "create":
			path := strings.TrimPrefix(c.URI, "file://")
			if _, err := os.Stat(path); err == nil && !c.Overwrite && !c.IgnoreIfExists {
				return errors.New("FakeFileExists", "the file exists", "uri", c.URI)
			}
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				return errors.NewErr("FakeEditFailed", err).SetMetadata("uri", c.URI)
			}
			a.docs[c.URI] = ""
		case "edit", "":
			text, ok := a.docs[c.URI]
			if !ok {
				data, err := os.ReadFile(strings.TrimPrefix(c.URI, "file://"))
				if err != nil {
					return errors.NewErr("FakeEditFailed", err).SetMetadata("uri", c.URI)
				}
				text = string(data)
			}
			out, err := ApplyRangeEdits(text, c.Edits)
			if err != nil {
				return err
			}
			a.docs[c.URI] = out
		default:
			return errors.New("FakeEditUnsupported", "the fake applies edits and creates only", "kind", c.Kind)
		}
	}
	return nil
}

// ApplyRangeEdits applies edits whose ranges (lines and byte columns from 0) are in the text, as the window does: the edits do
// not overlap, and the ones at the same place keep their order.
func ApplyRangeEdits(text string, edits []extension.RangeEdit) (string, error) {
	lines := strings.Split(text, "\n")
	offset := func(p extension.Position) (int, error) {
		if p.Line < 0 || p.Line >= len(lines) || p.Col < 0 || p.Col > len(lines[p.Line]) {
			return 0, errors.New("FakeEditRange", "a position is outside the text", "line", p.Line, "col", p.Col)
		}
		n := 0
		for _, l := range lines[:p.Line] {
			n += len(l) + 1
		}
		return n + p.Col, nil
	}
	type span struct {
		from, to, index int
		text            string
	}
	spans := make([]span, len(edits))
	for i, e := range edits {
		from, err := offset(e.Range.Start)
		if err != nil {
			return "", err
		}
		to, err := offset(e.Range.End)
		if err != nil {
			return "", err
		}
		spans[i] = span{from, to, i, e.Text}
	}
	slices.SortFunc(spans, func(x, y span) int {
		if x.from != y.from {
			return y.from - x.from
		}
		return y.index - x.index // at one place the later edit goes in first, so that the edits end up in order
	})
	out := text
	for _, sp := range spans {
		out = out[:sp.from] + sp.text + out[sp.to:]
	}
	return out, nil
}

// ---- language server commands and code actions ----

// ServerCall is a call of LanguageServers().ExecuteCommand.
type ServerCall struct {
	Server, Command string
	Args            []any
}

// CodeActionCall is a call of LanguageServers().ApplyCodeAction.
type CodeActionCall struct {
	Server, URI string
	Req         extension.CodeActionRequest
}

// SetServerCommands sets what LanguageServers().Commands answers for the server: the commands the running server announced.
func (a *API) SetServerCommands(server string, commands []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.serverCommands == nil {
		a.serverCommands = map[string][]string{}
	}
	a.serverCommands[server] = slices.Clone(commands)
}

// OnServerCommand sets what happens when the extension executes a command of a server: the function runs on the goroutine of the
// call and returns the error the extension is told (nil is success). It may call Documents().Apply, as a server that sends
// workspace/applyEdit does. Without one every command succeeds and changes nothing.
func (a *API) OnServerCommand(fn func(ServerCall) error) {
	a.mu.Lock()
	a.commandHandler = fn
	a.mu.Unlock()
}

// OnCodeAction sets what a server answers to ApplyCodeAction: the title of the action it applied (empty for none) and the
// error. The function may call Documents().Apply. Without one the server offers no action.
func (a *API) OnCodeAction(fn func(CodeActionCall) (string, error)) {
	a.mu.Lock()
	a.actionHandler = fn
	a.mu.Unlock()
}

// ServerCalls are the commands the extension asked servers to execute, the first first.
func (a *API) ServerCalls() []ServerCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.serverCalls)
}

// CodeActionCalls are the code actions the extension asked servers to apply, the first first.
func (a *API) CodeActionCalls() []CodeActionCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.actionCalls)
}

func (s serversAPI) Commands(id string) ([]string, error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if _, err := s.a.server(id); err != nil {
		return nil, err
	}
	return slices.Clone(s.a.serverCommands[id]), nil
}

// ExecuteCommand implements extension.LanguageServers: the handler runs at once, and done is posted, so that it is called on the
// next Pump, on the UI thread of the test.
func (s serversAPI) ExecuteCommand(_ context.Context, id, command string, args []any, done func(error)) error {
	a := s.a
	a.mu.Lock()
	if _, err := a.server(id); err != nil {
		a.mu.Unlock()
		return err
	}
	call := ServerCall{Server: id, Command: command, Args: slices.Clone(args)}
	a.serverCalls = append(a.serverCalls, call)
	handler := a.commandHandler
	a.mu.Unlock()
	var err error
	if handler != nil {
		err = handler(call)
	}
	if done != nil {
		a.Post(func() { done(err) })
	}
	return nil
}

// ApplyCodeAction implements extension.LanguageServers like ExecuteCommand does.
func (s serversAPI) ApplyCodeAction(_ context.Context, id, uri string, req extension.CodeActionRequest, done func(string, error)) error {
	a := s.a
	a.mu.Lock()
	if _, err := a.server(id); err != nil {
		a.mu.Unlock()
		return err
	}
	call := CodeActionCall{Server: id, URI: uri, Req: req}
	a.actionCalls = append(a.actionCalls, call)
	handler := a.actionHandler
	a.mu.Unlock()
	var title string
	var err error
	if handler != nil {
		title, err = handler(call)
	}
	if done != nil {
		a.Post(func() { done(title, err) })
	}
	return nil
}

// ---- the input box ----

// Input is an input box the extension showed.
type Input struct {
	a    *API
	opts extension.InputBoxOptions
	done func(string, bool)
	mu   sync.Mutex
	over bool
}

// Options are what the extension asked to show.
func (in *Input) Options() extension.InputBoxOptions { return in.opts }

// Validate is the message the extension's validation gives for the text (empty when it accepts it or has none).
func (in *Input) Validate(text string) string {
	if in.opts.Validate == nil {
		return ""
	}
	return in.opts.Validate(text)
}

// Accept ends the box with the text, as the user pressing Enter does; a text the validation refuses fails the test.
func (in *Input) Accept(text string) {
	in.a.t.Helper()
	if msg := in.Validate(text); msg != "" {
		in.a.t.Fatalf("the input box refuses %q: %s", text, msg)
	}
	in.end(text, true)
}

// Cancel closes the box with no text.
func (in *Input) Cancel() { in.a.t.Helper(); in.end("", false) }

func (in *Input) end(text string, ok bool) {
	in.a.t.Helper()
	in.mu.Lock()
	over := in.over
	in.over = true
	in.mu.Unlock()
	if over {
		in.a.t.Fatal("the input box is over already")
		return
	}
	if in.done != nil {
		in.done(text, ok)
	}
}

type inputBox struct{ a *API }

// InputBox implements extension.API.
func (a *API) InputBox() extension.InputBox { return inputBox{a} }

func (b inputBox) Show(opts extension.InputBoxOptions, done func(string, bool)) error {
	b.a.mu.Lock()
	b.a.inputs = append(b.a.inputs, &Input{a: b.a, opts: opts, done: done})
	b.a.mu.Unlock()
	return nil
}

// Inputs are the input boxes the extension showed, the first first.
func (a *API) Inputs() []*Input {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.inputs)
}

// LastInput is the input box shown last, or nil.
func (a *API) LastInput() *Input {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.inputs) == 0 {
		return nil
	}
	return a.inputs[len(a.inputs)-1]
}
