package cpp

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

// The commands of vscode-clangd that the pack has (SPEC §10.34 "C and C++"), with its ids and titles. The ones that need the server ask
// it with a request (LanguageServers().Request), off the UI thread, and show the answer on it.

// commandCategory is the category of the commands in the palette, as vscode-clangd has it.
const commandCategory = "clangd"

// cppWhen enables a command that works on the file in front only for a file clangd serves.
const cppWhen = "editorLangId == c || editorLangId == cpp || editorLangId == cuda-cpp"

// commandSpecs are the commands the manifest declares.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: SwitchHeaderSourceCommand, Title: "Switch Between Source/Header", Category: commandCategory, When: cppWhen, Description: "Opens the header of a source file, or the source file of a header."},
		{ID: ASTCommand, Title: "Show AST", Category: commandCategory, When: cppWhen, Description: "Shows the syntax tree clangd builds for the selection, or the cursor, in a read-only tab."},
		{ID: ToggleInlayHintsCommand, Title: "Toggle inlay hints", Category: commandCategory, Description: "Turns the inlay hints of the editor (editor.inlayHints.enabled) on or off."},
		{ID: ProjectConfigCommand, Title: "Open project configuration file", Category: commandCategory, Icon: "gear", Description: "Opens the .clangd of the project, and offers to create it with a CompileFlags stub when there is none."},
		{ID: RestartCommand, Title: "Restart language server", Category: commandCategory, Description: "Stops clangd and starts it again."},
	}
}

// keybindings are the default keys vscode-clangd has: Alt+O switches between the header and the source.
func keybindings() []extension.Keybinding {
	return []extension.Keybinding{
		{Command: SwitchHeaderSourceCommand, Key: "alt+o", When: "editorTextFocus && (" + cppWhen + ")"},
	}
}

// handlers are the commands of the pack and what runs them.
func (c *cpp) handlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		SwitchHeaderSourceCommand: func(context.Context, any) error { c.switchHeaderSource(); return nil },
		ASTCommand:                func(context.Context, any) error { c.showAST(); return nil },
		ToggleInlayHintsCommand:   func(context.Context, any) error { c.toggleInlayHints(); return nil },
		ProjectConfigCommand:      func(context.Context, any) error { c.projectConfig(); return nil },
		RestartCommand:            func(context.Context, any) error { c.restartServer(); return nil },
	}
}

// ---- what the requests are made of ----

// document names a document in the requests of the protocol.
type document struct {
	URI string `json:"uri"`
}

// file is the C or C++ file a command works on: where it is, its text, the selection in it and the encoding of the server's positions.
type file struct {
	uri, text string
	sel       extension.Selection
	encoding  string
}

// isCpp tells whether the language is one clangd serves.
func isCpp(languageID string) bool {
	for _, l := range cppLanguages {
		if l == languageID {
			return true
		}
	}
	return false
}

// activeFile reads the C or C++ file in front and checks that clangd runs. When it cannot work it tells the user and reports false.
func (c *cpp) activeFile(ctx context.Context, what string) (file, bool) {
	doc, ok := c.api.Documents().Active()
	if !ok || !isCpp(doc.LanguageID) || len(doc.Selections) == 0 {
		c.api.Notifications().Info(what + " needs a C or C++ file: open one and put the cursor in it.")
		return file{}, false
	}
	if !c.isRunning(what) {
		return file{}, false
	}
	text, err := c.api.Documents().Read(ctx, doc.URI)
	if err != nil {
		c.api.Log().Warn("the C or C++ file cannot be read", "uri", doc.URI, "err", err)
		c.api.Notifications().Error(what + ": " + filepath.Base(doc.URI) + " cannot be read: " + describe(err))
		return file{}, false
	}
	enc, err := c.api.LanguageServers().PositionEncoding(ServerID)
	if err != nil {
		c.api.Log().Warn("the position encoding of clangd is not known", "err", err)
		enc = langserver.EncodingUTF16
	}
	return file{uri: doc.URI, text: text, sel: doc.Selections[0], encoding: enc}, true
}

// isRunning tells whether clangd runs; when it does not, the user is told what state it is in.
func (c *cpp) isRunning(what string) bool {
	st, err := c.api.LanguageServers().Status(ServerID)
	if err != nil {
		c.api.Notifications().Error(what + ": " + describe(err))
		return false
	}
	if st.State == extension.ServerRunning {
		return true
	}
	c.api.Notifications().Warn(what + " needs clangd, which is " + stateWord(st.State) + ". Its state is in the status bar.")
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
	return "not running yet (open a C or C++ file)"
}

// ask sends the request off the UI thread and calls done, on it, with the result when the server answered; a failure is shown to the
// user. The request ends with the activation.
func (c *cpp) ask(what, method string, params any, done func(result json.RawMessage)) {
	c.work.Add(1)
	go func() {
		defer c.work.Done()
		result, err := c.api.LanguageServers().Request(c.ctx, ServerID, method, params)
		c.api.Post(func() {
			if err != nil {
				c.api.Log().Warn("a request of clangd failed", "method", method, "err", err)
				c.api.Notifications().Error(what + ": clangd failed: " + describe(err))
				return
			}
			done(result)
		})
	}()
}

// isNull tells whether the result of a request is null (clangd found nothing).
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

// switchHeaderSource is Switch Between Source/Header: clangd answers the URI of the other file.
func (c *cpp) switchHeaderSource() {
	const what = "Switch Between Source/Header"
	f, ok := c.activeFile(c.ctx, what)
	if !ok {
		return
	}
	c.ask(what, "textDocument/switchSourceHeader", document{f.uri}, func(result json.RawMessage) {
		var uri string
		if isNull(result) || json.Unmarshal(result, &uri) != nil || uri == "" {
			c.api.Notifications().Info(what + ": clangd found no header or source file for " + filepath.Base(f.uri) + ".")
			return
		}
		if err := c.api.Documents().Show(uri, 1, 1, false); err != nil {
			c.api.Log().Warn("the other file of clangd cannot be shown", "uri", uri, "err", err)
			c.api.Notifications().Error("Cannot open " + filepath.Base(uri) + ": " + describe(err))
		}
	})
}

// astNode is a node of the answer of textDocument/ast.
type astNode struct {
	Role     string            `json:"role"`
	Kind     string            `json:"kind"`
	Detail   string            `json:"detail"`
	Range    *langserver.Range `json:"range"`
	Children []astNode         `json:"children"`
}

// showAST is Show AST: the node at the selection (the cursor when it is empty) with everything below it.
func (c *cpp) showAST() {
	const what = "Show AST"
	f, ok := c.activeFile(c.ctx, what)
	if !ok {
		return
	}
	params := struct {
		TextDocument document         `json:"textDocument"`
		Range        langserver.Range `json:"range"`
	}{TextDocument: document{f.uri}, Range: langserver.RangeToProtocol(f.text, orderedRange(f.sel), f.encoding)}
	c.ask(what, "textDocument/ast", params, func(result json.RawMessage) {
		var root astNode
		if isNull(result) || json.Unmarshal(result, &root) != nil || root.Kind == "" {
			c.api.Notifications().Info(what + ": clangd has no syntax tree for this place.")
			return
		}
		c.showContent(astPath, "AST of "+filepath.Base(f.uri), "plaintext", renderAST(root, f))
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

// renderAST writes the tree one node a line, two spaces deeper for each level: the kind (and the role when it says more), the detail
// and the range as line:column-line:column of the file as the editor counts them.
func renderAST(root astNode, f file) string {
	var b strings.Builder
	var walk func(n astNode, depth int)
	walk = func(n astNode, depth int) {
		b.WriteString(strings.Repeat("  ", depth))
		b.WriteString(describeNode(n))
		if n.Detail != "" {
			b.WriteString(" ")
			b.WriteString(n.Detail)
		}
		if n.Range != nil {
			b.WriteString("  <" + f.place(n.Range.Start) + "-" + f.place(n.Range.End) + ">")
		}
		b.WriteString("\n")
		for _, child := range n.Children {
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	return b.String()
}

// describeNode is the label of a node as vscode-clangd writes it: the kind alone for the roles that every node of a kind has, else
// "kind role".
func describeNode(n astNode) string {
	switch n.Role {
	case "expression", "statement", "declaration", "template name":
		return n.Kind
	}
	return n.Kind + " " + n.Role
}

// place is a position of the server as line:column of the editor, counted from 1.
func (f file) place(p langserver.Position) string {
	pos := langserver.FromProtocol(f.text, p, f.encoding)
	return strconv.Itoa(pos.Line+1) + ":" + strconv.Itoa(langserver.UTF16Column(f.text, pos)+1)
}

// toggleInlayHints is Toggle inlay hints: it flips editor.inlayHints.enabled in the user's settings, as vscode-clangd's command does
// ("on" and "off", "onUnlessPressed" and "offUnlessPressed" swap, anything else stays as it is).
func (c *cpp) toggleInlayHints() {
	const what = "Toggle inlay hints"
	if c.api.Origins().Current() != extension.HostOrigin {
		c.api.Notifications().Info(what + ": the inlay hints are a setting of the host and cannot be changed from here.")
		return
	}
	current, err := extension.SettingValue[string](c.api.Settings(), InlayHintsSetting)
	if err != nil {
		c.api.Log().Warn("the setting of the inlay hints cannot be read", "err", err)
		c.api.Notifications().Error(what + ": " + describe(err))
		return
	}
	next, ok := toggledInlayHints(current)
	if !ok {
		c.api.Notifications().Info(what + ": " + InlayHintsSetting + " is \"" + current + "\", which has no opposite.")
		return
	}
	if err := c.api.Settings().Update(InlayHintsSetting, next); err != nil {
		c.api.Log().Warn("the setting of the inlay hints cannot be written", "err", err)
		c.api.Notifications().Error(what + ": " + describe(err))
		return
	}
}

// toggledInlayHints is the value of editor.inlayHints.enabled that is the opposite of the current one.
func toggledInlayHints(current string) (string, bool) {
	switch current {
	case "on", "":
		return "off", true
	case "off":
		return "on", true
	case "onUnlessPressed":
		return "offUnlessPressed", true
	case "offUnlessPressed":
		return "onUnlessPressed", true
	}
	return "", false
}

// restartServer is Restart language server.
func (c *cpp) restartServer() {
	if err := c.api.LanguageServers().Restart(ServerID); err != nil {
		c.api.Log().Warn("clangd cannot be restarted", "err", err)
		c.api.Notifications().Error("Restart language server: " + describe(err))
	}
}
