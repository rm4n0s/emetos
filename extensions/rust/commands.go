package rust

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The commands of rust-analyzer's VS Code extension that the pack has (SPEC §10.34 "Rust"), with its ids and titles. Each asks the
// server with a request of rust-analyzer's own protocol (LanguageServers().Request), off the UI thread, and shows the answer on it.

// commandCategory is the category of the commands in the palette, as VS Code's extension has it.
const commandCategory = "rust-analyzer"

// rustWhen enables a command that works on the file in front only for a Rust file.
const rustWhen = "editorLangId == rust"

// commandSpecs are the commands the manifest declares.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: ExpandMacroCommand, Title: "Expand macro recursively at caret", Category: commandCategory, When: rustWhen, Description: "Shows the expansion of the macro call at the cursor in a read-only tab."},
		{ID: ViewSyntaxTreeCommand, Title: "View Syntax Tree", Category: commandCategory, When: rustWhen, Description: "Shows the syntax tree of rust-analyzer for the file, or the selection, in a read-only tab."},
		{ID: ParentModuleCommand, Title: "Locate parent module", Category: commandCategory, When: rustWhen, Description: "Goes to the module that declares the module of the file."},
		{ID: OpenCargoTomlCommand, Title: "Open Cargo.toml", Category: commandCategory, When: rustWhen, Description: "Opens the Cargo.toml of the package of the file."},
		{ID: JoinLinesCommand, Title: "Join lines", Category: commandCategory, When: rustWhen, Description: "Joins the lines of the selections and removes the braces and semicolons that are no longer needed."},
		{ID: MatchingBraceCommand, Title: "Find matching brace", Category: commandCategory, When: rustWhen, Description: "Moves the cursor to the bracket that matches the one at the cursor."},
		{ID: ReloadWorkspaceCommand, Title: "Reload workspace", Category: commandCategory, Description: "Makes rust-analyzer read the Cargo workspace and the build scripts again."},
		{ID: RestartServerCommand, Title: "Restart server", Category: commandCategory, Description: "Stops rust-analyzer and starts it again."},
	}
}

// keybindings are the default keys VS Code's extension has.
func keybindings() []extension.Keybinding {
	const when = "editorTextFocus && " + rustWhen
	return []extension.Keybinding{
		{Command: ParentModuleCommand, Key: "ctrl+shift+u", When: when},
		{Command: JoinLinesCommand, Key: "ctrl+shift+j", When: when},
	}
}

// handlers are the commands of the pack and what runs them.
func (r *rust) handlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		ExpandMacroCommand:     func(ctx context.Context, _ any) error { r.expandMacro(ctx); return nil },
		ViewSyntaxTreeCommand:  func(ctx context.Context, _ any) error { r.viewSyntaxTree(ctx); return nil },
		ParentModuleCommand:    func(ctx context.Context, _ any) error { r.parentModule(ctx); return nil },
		OpenCargoTomlCommand:   func(context.Context, any) error { r.openCargoToml(); return nil },
		JoinLinesCommand:       func(ctx context.Context, _ any) error { r.joinLines(ctx); return nil },
		MatchingBraceCommand:   func(ctx context.Context, _ any) error { r.matchingBrace(ctx); return nil },
		ReloadWorkspaceCommand: func(context.Context, any) error { r.reloadWorkspace(); return nil },
		RestartServerCommand:   func(context.Context, any) error { r.restartServer(); return nil },
	}
}

// ---- what the requests are made of ----

// document names a document in the requests of the protocol.
type document struct {
	URI string `json:"uri"`
}

// positionParams are the params of the requests that work at a position.
type positionParams struct {
	TextDocument document            `json:"textDocument"`
	Position     langserver.Position `json:"position"`
}

// rustFile is the Rust file a command works on: where it is, its text, the selections in it and the encoding of the server's positions.
type rustFile struct {
	uri, text string
	sels      []extension.Selection
	encoding  string
}

// position is the protocol's position of a position of the API in the file.
func (f rustFile) position(p extension.Position) langserver.Position {
	return langserver.ToProtocol(f.text, p, f.encoding)
}

// activeFile reads the Rust file in front and checks that rust-analyzer runs. When it cannot work it tells the user and reports false.
func (r *rust) activeFile(ctx context.Context, what string) (rustFile, bool) {
	doc, ok := r.api.Documents().Active()
	if !ok || doc.LanguageID != "rust" || len(doc.Selections) == 0 {
		r.api.Notifications().Info(what + " needs a Rust file: open one and put the cursor in it.")
		return rustFile{}, false
	}
	if !r.running(what) {
		return rustFile{}, false
	}
	text, err := r.api.Documents().Read(ctx, doc.URI)
	if err != nil {
		r.api.Log().Warn("the Rust file cannot be read", "uri", doc.URI, "err", err)
		r.api.Notifications().Error(what + ": " + filepath.Base(doc.URI) + " cannot be read: " + describe(err))
		return rustFile{}, false
	}
	enc, err := r.api.LanguageServers().PositionEncoding(ServerID)
	if err != nil {
		r.api.Log().Warn("the position encoding of rust-analyzer is not known", "err", err)
		enc = langserver.EncodingUTF16
	}
	return rustFile{uri: doc.URI, text: text, sels: doc.Selections, encoding: enc}, true
}

// running tells whether rust-analyzer runs; when it does not, the user is told what state it is in.
func (r *rust) running(what string) bool {
	st, err := r.api.LanguageServers().Status(ServerID)
	if err != nil {
		r.api.Notifications().Error(what + ": " + describe(err))
		return false
	}
	if st.State == extension.ServerRunning {
		return true
	}
	r.api.Notifications().Warn(what + " needs rust-analyzer, which is " + stateWord(st.State) + ". Its state is in the status bar.")
	return false
}

// stateWord says in words what state the server is in.
func stateWord(s extension.ServerState) string {
	switch s {
	case extension.ServerStarting:
		return "starting"
	case extension.ServerMissing:
		return "not available (see the notification, or the menu of the status bar item)"
	case extension.ServerFailed:
		return "stopped after crashing (restart it from the menu of the status bar item)"
	case extension.ServerStopped:
		return "stopped"
	}
	return "not running yet (open a Rust file)"
}

// ask sends the request off the UI thread and calls done, on it, with the result when the server answered; a failure is shown to the
// user. The request ends with the activation.
func (r *rust) ask(what, method string, params any, done func(result json.RawMessage)) {
	r.work.Add(1)
	go func() {
		defer r.work.Done()
		result, err := r.api.LanguageServers().Request(r.ctx, ServerID, method, params)
		r.api.Post(func() {
			if err != nil {
				r.api.Log().Warn("a request of rust-analyzer failed", "method", method, "err", err)
				r.api.Notifications().Error(what + ": rust-analyzer failed: " + describe(err))
				return
			}
			done(result)
		})
	}()
}

// isNull tells whether the result of a request is null (rust-analyzer found nothing).
func isNull(result json.RawMessage) bool {
	s := strings.TrimSpace(string(result))
	return s == "" || s == "null"
}

// describe is the text of an error for the user.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// ---- the commands ----

// expandMacro is Expand macro recursively at caret.
func (r *rust) expandMacro(ctx context.Context) {
	const what = "Expand macro"
	f, ok := r.activeFile(ctx, what)
	if !ok {
		return
	}
	params := positionParams{TextDocument: document{f.uri}, Position: f.position(f.sels[0].Active)}
	r.ask(what, "rust-analyzer/expandMacro", params, func(result json.RawMessage) {
		var out struct {
			Name      string `json:"name"`
			Expansion string `json:"expansion"`
		}
		if isNull(result) || json.Unmarshal(result, &out) != nil || out.Expansion == "" {
			r.api.Notifications().Info(what + ": there is no macro call at the cursor.")
			return
		}
		r.showContent(expansionPath, "Expansion of "+strings.TrimSpace(out.Name), "rust", out.Expansion)
	})
}

// viewSyntaxTree is View Syntax Tree: the tree of the whole file, or of the selection.
func (r *rust) viewSyntaxTree(ctx context.Context) {
	const what = "View Syntax Tree"
	f, ok := r.activeFile(ctx, what)
	if !ok {
		return
	}
	params := struct {
		TextDocument document          `json:"textDocument"`
		Range        *langserver.Range `json:"range"`
	}{TextDocument: document{f.uri}}
	if sel := f.sels[0]; sel.Anchor != sel.Active {
		rg := langserver.RangeToProtocol(f.text, orderedRange(sel), f.encoding)
		params.Range = &rg
	}
	r.ask(what, "rust-analyzer/viewSyntaxTree", params, func(result json.RawMessage) {
		var tree string
		if isNull(result) || json.Unmarshal(result, &tree) != nil {
			r.api.Notifications().Info(what + ": rust-analyzer has no syntax tree for this file.")
			return
		}
		r.showContent(treePath, "Syntax Tree of "+filepath.Base(f.uri), "plaintext", tree)
	})
}

// orderedRange is the range a selection covers, whichever end is the cursor.
func orderedRange(sel extension.Selection) extension.Range {
	a, b := sel.Anchor, sel.Active
	if b.Line < a.Line || (b.Line == a.Line && b.Col < a.Col) {
		a, b = b, a
	}
	return extension.Range{Start: a, End: b}
}

// parentModule is Locate parent module.
func (r *rust) parentModule(ctx context.Context) {
	const what = "Locate parent module"
	f, ok := r.activeFile(ctx, what)
	if !ok {
		return
	}
	params := positionParams{TextDocument: document{f.uri}, Position: f.position(f.sels[0].Active)}
	r.ask(what, "experimental/parentModule", params, func(result json.RawMessage) {
		places := decodePlaces(result)
		switch len(places) {
		case 0:
			r.api.Notifications().Info(what + ": rust-analyzer found no parent module (this may be the root of the crate).")
		case 1:
			r.goTo(r.ctx, places[0], f.encoding)
		default:
			r.pickPlace(r.ctx, what, places, f.encoding)
		}
	})
}

// openCargoToml is Open Cargo.toml.
func (r *rust) openCargoToml() {
	const what = "Open Cargo.toml"
	ctx := r.ctx
	f, ok := r.activeFile(ctx, what)
	if !ok {
		return
	}
	r.ask(what, "experimental/openCargoToml", struct {
		TextDocument document `json:"textDocument"`
	}{document{f.uri}}, func(result json.RawMessage) {
		places := decodePlaces(result)
		if len(places) == 0 {
			r.api.Notifications().Info(what + ": rust-analyzer found no Cargo.toml for this file.")
			return
		}
		r.goTo(ctx, places[0], f.encoding)
	})
}

// joinLines is Join lines.
func (r *rust) joinLines(ctx context.Context) {
	const what = "Join lines"
	f, ok := r.activeFile(ctx, what)
	if !ok {
		return
	}
	params := struct {
		TextDocument document           `json:"textDocument"`
		Ranges       []langserver.Range `json:"ranges"`
	}{TextDocument: document{f.uri}}
	for _, sel := range f.sels {
		params.Ranges = append(params.Ranges, langserver.RangeToProtocol(f.text, orderedRange(sel), f.encoding))
	}
	r.ask(what, "experimental/joinLines", params, func(result json.RawMessage) {
		var edits []struct {
			Range   langserver.Range `json:"range"`
			NewText string           `json:"newText"`
		}
		if isNull(result) || json.Unmarshal(result, &edits) != nil || len(edits) == 0 {
			return
		}
		if now, err := r.api.Documents().Read(r.ctx, f.uri); err != nil || now != f.text {
			r.api.Notifications().Info(what + ": the file changed while rust-analyzer worked. Try again.")
			return
		}
		change := extension.ResourceChange{Kind: "edit", URI: f.uri}
		for _, e := range edits {
			change.Edits = append(change.Edits, extension.RangeEdit{Range: langserver.RangeFromProtocol(f.text, e.Range, f.encoding), Text: e.NewText})
		}
		if err := r.api.Documents().Apply(r.ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{change}}); err != nil {
			r.api.Log().Warn("the edit of rust-analyzer cannot be applied", "err", err)
			r.api.Notifications().Error(what + ": " + describe(err))
		}
	})
}

// matchingBrace is Find matching brace: the cursor goes to the bracket that matches.
func (r *rust) matchingBrace(ctx context.Context) {
	const what = "Find matching brace"
	f, ok := r.activeFile(ctx, what)
	if !ok {
		return
	}
	params := struct {
		TextDocument document              `json:"textDocument"`
		Positions    []langserver.Position `json:"positions"`
	}{TextDocument: document{f.uri}, Positions: []langserver.Position{f.position(f.sels[0].Active)}}
	r.ask(what, "experimental/matchingBrace", params, func(result json.RawMessage) {
		var out []langserver.Position
		if isNull(result) || json.Unmarshal(result, &out) != nil || len(out) == 0 {
			return
		}
		r.goTo(r.ctx, place{uri: f.uri, pos: out[0]}, f.encoding)
	})
}

// reloadWorkspace is Reload workspace.
func (r *rust) reloadWorkspace() {
	const what = "Reload workspace"
	if !r.running(what) {
		return
	}
	r.ask(what, "rust-analyzer/reloadWorkspace", nil, func(json.RawMessage) {})
}

// restartServer is Restart server.
func (r *rust) restartServer() {
	if err := r.api.LanguageServers().Restart(ServerID); err != nil {
		r.api.Log().Warn("rust-analyzer cannot be restarted", "err", err)
		r.api.Notifications().Error("Restart server: " + describe(err))
	}
}

// ---- places ----

// place is a place in a file, in the units of the server's encoding.
type place struct {
	uri string
	pos langserver.Position
}

// decodePlaces reads the answer of a request that gives places: a Location, a list of Locations or a list of LocationLinks (the
// start of the selection is the place). Nothing readable gives no places.
func decodePlaces(result json.RawMessage) []place {
	type one struct {
		URI                  string            `json:"uri"`
		Range                *langserver.Range `json:"range"`
		TargetURI            string            `json:"targetUri"`
		TargetSelectionRange *langserver.Range `json:"targetSelectionRange"`
		TargetRange          *langserver.Range `json:"targetRange"`
	}
	var many []one
	if err := json.Unmarshal(result, &many); err != nil {
		var single one
		if json.Unmarshal(result, &single) != nil || (single.URI == "" && single.TargetURI == "") {
			return nil
		}
		many = []one{single}
	}
	var out []place
	for _, o := range many {
		switch {
		case o.URI != "":
			p := place{uri: o.URI}
			if o.Range != nil {
				p.pos = o.Range.Start
			}
			out = append(out, p)
		case o.TargetURI != "":
			p := place{uri: o.TargetURI}
			switch {
			case o.TargetSelectionRange != nil:
				p.pos = o.TargetSelectionRange.Start
			case o.TargetRange != nil:
				p.pos = o.TargetRange.Start
			}
			out = append(out, p)
		}
	}
	return out
}

// goTo opens the file at the place, with the cursor there.
func (r *rust) goTo(ctx context.Context, p place, encoding string) {
	line, col := p.pos.Line, p.pos.Character
	if text, err := r.api.Documents().Read(ctx, p.uri); err == nil {
		pos := langserver.FromProtocol(text, p.pos, encoding)
		line, col = pos.Line, langserver.UTF16Column(text, pos)
	}
	if err := r.api.Documents().Show(p.uri, line+1, col+1, false); err != nil {
		r.api.Log().Warn("a place of rust-analyzer cannot be shown", "uri", p.uri, "err", err)
		r.api.Notifications().Error("Cannot open " + filepath.Base(p.uri) + ": " + describe(err))
	}
}

// pickPlace lets the user choose among places.
func (r *rust) pickPlace(ctx context.Context, what string, places []place, encoding string) {
	items := make([]extension.QuickPickItem, len(places))
	for i, p := range places {
		items[i] = extension.QuickPickItem{Label: r.relative(p.uri), Description: "line " + strconv.Itoa(p.pos.Line+1)}
	}
	err := r.api.QuickPick().Show(extension.QuickPickOptions{Title: what, Placeholder: "Choose where to go", Items: items}, func(i int) {
		if i >= 0 && i < len(places) {
			r.goTo(ctx, places[i], encoding)
		}
	})
	if err != nil {
		r.api.Log().Warn("the places of rust-analyzer cannot be listed", "err", err)
		r.goTo(ctx, places[0], encoding)
	}
}

// relative is the path of a file URI relative to the folder of the tab when it is inside, else the whole path.
func (r *rust) relative(uri string) string {
	path := strings.TrimPrefix(uri, "file://")
	if folder := r.api.Scope().Folder; folder != "" {
		if rel, err := filepath.Rel(folder, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}
