package typescript

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The commands of the TypeScript and JavaScript packs (SPEC §10.34 "TypeScript and JavaScript"), with the ids and titles of VS Code's
// TypeScript extension. The ones that need the server ask it with workspace/executeCommand (LanguageServers().Request), off the UI
// thread, and show the answer on it.

// The ids of the commands.
const (
	// RestartCommand restarts the language server ("TypeScript: Restart TS Server").
	RestartCommand = "typescript.restartTsServer"
	// SelectVersionCommand lists the TypeScript versions and keeps the choice for the folder ("TypeScript: Select TypeScript Version...").
	// A string argument is the choice (ChoiceGlobal or ChoiceWorkspace) to make without asking.
	SelectVersionCommand = "typescript.selectTypeScriptVersion"
	// GoToProjectConfigCommand and GoToJSProjectConfigCommand open the tsconfig.json or jsconfig.json of the file in front.
	GoToProjectConfigCommand   = "typescript.goToProjectConfig"
	GoToJSProjectConfigCommand = "javascript.goToProjectConfig"
	// GoToSourceDefinitionCommand goes to the definition in the source of a library instead of its declaration file.
	GoToSourceDefinitionCommand = "typescript.goToSourceDefinition"
	// FindFileReferencesCommand lists the places that import the file in front (or the file the command is given).
	FindFileReferencesCommand = "typescript.findAllFileReferences"
	// OrganizeImportsCommand sorts and combines the imports of the file in front and removes the unused ones.
	OrganizeImportsCommand = "typescript.organizeImports"
	// SortImportsCommand and RemoveUnusedImportsCommand do one half of it; the JavaScript variants are the same for the JavaScript files.
	SortImportsCommand           = "typescript.sortImports"
	JSSortImportsCommand         = "javascript.sortImports"
	RemoveUnusedImportsCommand   = "typescript.removeUnusedImports"
	JSRemoveUnusedImportsCommand = "javascript.removeUnusedImports"
)

// What the commands ask of the server and how they are shown.
const (
	executeCommandMethod         = "workspace/executeCommand"
	serverGoToSourceDefinition   = "_typescript.goToSourceDefinition"
	serverTsserverRequestCommand = "typescript.tsserverRequest"
	projectInfoCommand           = "projectInfo"
	fileReferencesCommand        = "fileReferences"
	// implicitProjectConfigPrefix starts the name tsserver gives the project of a file that has no tsconfig.json or jsconfig.json.
	implicitProjectConfigPrefix = "/dev/null/"
	// referencesPickerLimit is how many references Find File References lists.
	referencesPickerLimit = 500

	kindOrganizeImports     = "source.organizeImports"
	kindSortImports         = "source.sortImports"
	kindRemoveUnusedImports = "source.removeUnusedImports"

	commandCategoryTS = "TypeScript"
	commandCategoryJS = "JavaScript"
	whenTS            = "editorLangId == typescript || editorLangId == typescriptreact"
	whenJS            = "editorLangId == javascript || editorLangId == javascriptreact"
	whenAny           = "(" + whenTS + ") || (" + whenJS + ")"

	tsconfigName = "tsconfig.json"
	jsconfigName = "jsconfig.json"
)

// commandSpecs are the commands the manifest declares.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: RestartCommand, Title: "Restart TS Server", Category: commandCategoryTS, Description: "Stops typescript-language-server and starts it again."},
		{ID: SelectVersionCommand, Title: "Select TypeScript Version...", Category: commandCategoryTS, Description: "Chooses the TypeScript the language server uses for this folder: the one installed beside it, or the one in the folder's node_modules."},
		{ID: GoToProjectConfigCommand, Title: "Go to Project Configuration (tsconfig)", Category: commandCategoryTS, When: whenTS, Description: "Opens the tsconfig.json that the file in front belongs to."},
		{ID: GoToJSProjectConfigCommand, Title: "Go to Project Configuration (jsconfig / tsconfig)", Category: commandCategoryJS, When: whenJS, Description: "Opens the jsconfig.json or tsconfig.json that the file in front belongs to."},
		{ID: GoToSourceDefinitionCommand, Title: "Go to Source Definition", Category: commandCategoryTS, When: whenAny, Description: "Goes to the definition of the symbol at the cursor in the source of its library, not in a declaration file."},
		{ID: FindFileReferencesCommand, Title: "Find File References", Category: commandCategoryTS, When: whenAny, Description: "Lists the files that import the file in front."},
		{ID: OrganizeImportsCommand, Title: "Organize Imports", Category: commandCategoryTS, When: whenAny, Description: "Sorts and combines the imports of the file and removes the ones it does not use."},
		{ID: SortImportsCommand, Title: "Sort Imports", Category: commandCategoryTS, When: whenTS, Description: "Sorts and combines the imports of the file without removing any."},
		{ID: JSSortImportsCommand, Title: "Sort Imports", Category: commandCategoryJS, When: whenJS, Description: "Sorts and combines the imports of the file without removing any."},
		{ID: RemoveUnusedImportsCommand, Title: "Remove Unused Imports", Category: commandCategoryTS, When: whenTS, Description: "Removes the imports of the file that nothing uses."},
		{ID: JSRemoveUnusedImportsCommand, Title: "Remove Unused Imports", Category: commandCategoryJS, When: whenJS, Description: "Removes the imports of the file that nothing uses."},
	}
}

// handlers are the commands of the pack and what runs them.
func (t *typescript) handlers() map[string]extension.CommandHandler {
	source := func(what, kind string) extension.CommandHandler {
		return func(ctx context.Context, _ any) error { t.applySourceAction(ctx, what, kind); return nil }
	}
	return map[string]extension.CommandHandler{
		RestartCommand:               func(context.Context, any) error { t.restart(); return nil },
		SelectVersionCommand:         t.selectVersionCommand,
		GoToProjectConfigCommand:     func(context.Context, any) error { t.goToProjectConfig(); return nil },
		GoToJSProjectConfigCommand:   func(context.Context, any) error { t.goToProjectConfig(); return nil },
		GoToSourceDefinitionCommand:  func(context.Context, any) error { t.goToSourceDefinition(); return nil },
		FindFileReferencesCommand:    func(_ context.Context, args any) error { t.findFileReferences(args); return nil },
		OrganizeImportsCommand:       source("Organize Imports", kindOrganizeImports),
		SortImportsCommand:           source("Sort Imports", kindSortImports),
		JSSortImportsCommand:         source("Sort Imports", kindSortImports),
		RemoveUnusedImportsCommand:   source("Remove Unused Imports", kindRemoveUnusedImports),
		JSRemoveUnusedImportsCommand: source("Remove Unused Imports", kindRemoveUnusedImports),
	}
}

// ---- what the requests are made of ----

// file is the TypeScript or JavaScript file a command works on: where it is, its text, the selection in it and the encoding of the
// server's positions.
type file struct {
	uri, text string
	sel       extension.Selection
	encoding  string
}

// path is the path of the file on disk, "" for a document that is not a file.
func (f file) path() string { return filePath(f.uri) }

// filePath is the path of a file: URI, "" for any other.
func filePath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}

// fileURI is the file: URI of a path.
func fileURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// activeFile reads the file in front and checks that the server runs. When it cannot work it tells the user and reports false.
func (t *typescript) activeFile(ctx context.Context, what string) (file, bool) {
	doc, ok := t.api.Documents().Active()
	if !ok || !isServed(doc.LanguageID) || len(doc.Selections) == 0 {
		t.api.Notifications().Info(what + " needs a TypeScript or JavaScript file: open one and put the cursor in it.")
		return file{}, false
	}
	if !t.isRunning(what) {
		return file{}, false
	}
	text, err := t.api.Documents().Read(ctx, doc.URI)
	if err != nil {
		t.api.Log().Warn("the TypeScript or JavaScript file cannot be read", "uri", doc.URI, "err", err)
		t.api.Notifications().Error(what + ": " + filepath.Base(doc.URI) + " cannot be read: " + describe(err))
		return file{}, false
	}
	enc, err := t.api.LanguageServers().PositionEncoding(ServerID)
	if err != nil {
		t.api.Log().Warn("the position encoding of the TypeScript server is not known", "err", err)
		enc = langserver.EncodingUTF16
	}
	return file{uri: doc.URI, text: text, sel: doc.Selections[0], encoding: enc}, true
}

// isRunning tells whether the server runs; when it does not, the user is told what state it is in.
func (t *typescript) isRunning(what string) bool {
	st, err := t.api.LanguageServers().Status(ServerID)
	if err != nil {
		t.api.Notifications().Error(what + ": " + describe(err))
		return false
	}
	if st.State == extension.ServerRunning {
		return true
	}
	t.api.Notifications().Warn(what + " needs typescript-language-server, which is " + stateWord(st.State) + ". Its state is in the status bar.")
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
	return "not running yet (open a TypeScript or JavaScript file)"
}

// benign tells whether an error only says that the host has no such UI or the extension ended.
func benign(err error) bool {
	appErr, ok := errors.FromError(err)
	return ok && (strings.HasSuffix(appErr.Tag, "Unavailable") || appErr.Tag == "ExtensionDeactivated")
}

// describe is the text of an error for the user.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// execute sends workspace/executeCommand to the server off the UI thread and calls done, on it, with the result when the server answered;
// a failure is shown to the user. The request ends with the activation.
func (t *typescript) execute(what, command string, args []any, done func(result json.RawMessage)) {
	t.work.Add(1)
	go func() {
		defer t.work.Done()
		result, err := t.api.LanguageServers().Request(t.ctx, ServerID, executeCommandMethod, map[string]any{"command": command, "arguments": args})
		t.api.Post(func() {
			if err != nil {
				t.api.Log().Warn("a request of the TypeScript server failed", "command", command, "err", err)
				t.api.Notifications().Error(what + ": the language server failed: " + describe(err))
				return
			}
			done(result)
		})
	}()
}

// isNull tells whether the result of a request is null.
func isNull(result json.RawMessage) bool {
	s := strings.TrimSpace(string(result))
	return s == "" || s == "null"
}

// ---- the commands ----

// restart restarts the language server.
func (t *typescript) restart() {
	if err := t.api.LanguageServers().Restart(ServerID); err != nil && !benign(err) {
		t.api.Log().Warn("the TypeScript language server cannot be restarted", "err", err)
		t.api.Notifications().Error("The language server cannot be restarted: " + describe(err))
	}
}

// applySourceAction asks the server for the source action of the kind for the file in front and applies it.
func (t *typescript) applySourceAction(ctx context.Context, what, kind string) {
	doc, ok := t.api.Documents().Active()
	if !ok || !isServed(doc.LanguageID) {
		t.api.Notifications().Info(what + " needs a TypeScript or JavaScript file: open one first.")
		return
	}
	if !t.isRunning(what) {
		return
	}
	req := extension.CodeActionRequest{Only: []string{kind}}
	if len(doc.Selections) > 0 {
		req.Range = extension.Range{Start: doc.Selections[0].Anchor, End: doc.Selections[0].Active}
	}
	name := filepath.Base(doc.URI)
	err := t.api.LanguageServers().ApplyCodeAction(ctx, ServerID, doc.URI, req, func(title string, err error) {
		switch {
		case err != nil:
			t.api.Notifications().Error(what + ": the language server failed: " + describe(err))
		case title == "":
			t.api.Notifications().Info(what + ": nothing to change in " + name + ".")
		}
	})
	if err != nil {
		t.api.Notifications().Error(what + ": " + describe(err))
	}
}

// location is a Location of the protocol.
type location struct {
	URI   string           `json:"uri"`
	Range langserver.Range `json:"range"`
}

// goToSourceDefinition is Go to Source Definition: the server answers the places in the source of the library; one is shown, several are
// listed.
func (t *typescript) goToSourceDefinition() {
	const what = "Go to Source Definition"
	f, ok := t.activeFile(t.ctx, what)
	if !ok {
		return
	}
	pos := langserver.ToProtocol(f.text, f.sel.Active, f.encoding)
	t.execute(what, serverGoToSourceDefinition, []any{f.uri, pos}, func(result json.RawMessage) {
		var locs []location
		if isNull(result) || json.Unmarshal(result, &locs) != nil || len(locs) == 0 {
			t.api.Notifications().Error("No source definitions found.")
			return
		}
		t.showLocations(what, locs)
	})
}

// showLocations shows the place when there is one, else lists the places to choose from.
func (t *typescript) showLocations(what string, locs []location) {
	if len(locs) == 1 {
		t.showLocation(locs[0])
		return
	}
	rows := make([]extension.QuickPickItem, len(locs))
	for i, l := range locs {
		rows[i] = extension.QuickPickItem{Label: filepath.Base(filePath(l.URI)), Description: strconv.Itoa(int(l.Range.Start.Line)+1) + ":" + strconv.Itoa(int(l.Range.Start.Character)+1), Detail: short(langserver.DefaultEnv(t.api.Scope().Folder, nil), filePath(l.URI))}
	}
	err := t.api.QuickPick().Show(extension.QuickPickOptions{Title: what, Placeholder: "Choose a place", Items: rows, MatchOnDescription: true}, func(n int) {
		if n >= 0 && n < len(locs) {
			t.showLocation(locs[n])
		}
	})
	if err != nil && !benign(err) {
		t.api.Log().Warn("the list of places cannot be shown", "err", err)
	}
}

// showLocation opens the file of the place at its start. The column of the protocol counts UTF-16 units, as Show does.
func (t *typescript) showLocation(l location) {
	if err := t.api.Documents().Show(l.URI, int(l.Range.Start.Line)+1, int(l.Range.Start.Character)+1, false); err != nil {
		t.api.Log().Warn("a place cannot be shown", "uri", l.URI, "err", err)
		t.api.Notifications().Error("Cannot open " + filepath.Base(filePath(l.URI)) + ": " + describe(err))
	}
}

// tsserverRequest runs a request of tsserver itself through the server's command typescript.tsserverRequest and gives its response
// ({"type":"response","body":...}) to done.
func (t *typescript) tsserverRequest(what, request string, args any, done func(body json.RawMessage)) {
	t.execute(what, serverTsserverRequestCommand, []any{request, args, map[string]any{"isAsync": false, "lowPriority": false}}, func(result json.RawMessage) {
		var response struct {
			Body json.RawMessage `json:"body"`
		}
		if isNull(result) || json.Unmarshal(result, &response) != nil || isNull(response.Body) {
			t.api.Notifications().Warn(what + ": the language server could not answer for this file.")
			return
		}
		done(response.Body)
	})
}

// goToProjectConfig opens the tsconfig.json or jsconfig.json of the file in front: tsserver says which project the file belongs to.
func (t *typescript) goToProjectConfig() {
	const what = "Go to Project Configuration"
	f, ok := t.activeFile(t.ctx, what)
	if !ok {
		return
	}
	path := f.path()
	if path == "" {
		t.api.Notifications().Warn("Could not determine the TypeScript or JavaScript project: " + filepath.Base(f.uri) + " is not a file.")
		return
	}
	doc, _ := t.api.Documents().Active()
	javascript := strings.HasPrefix(doc.LanguageID, "javascript")
	t.tsserverRequest(what, projectInfoCommand, map[string]any{"file": path, "needFileNameList": false}, func(body json.RawMessage) {
		var info struct {
			ConfigFileName string `json:"configFileName"`
		}
		if json.Unmarshal(body, &info) != nil || info.ConfigFileName == "" {
			t.api.Notifications().Warn("Could not determine the TypeScript or JavaScript project.")
			return
		}
		if strings.HasPrefix(info.ConfigFileName, implicitProjectConfigPrefix) {
			kind, config := "TypeScript", tsconfigName
			if javascript {
				kind, config = "JavaScript", jsconfigName
			}
			t.api.Notifications().Info("File is not part of a " + kind + " project. Add a " + config + " to the folder to make it one.")
			return
		}
		if err := t.api.Documents().Show(fileURI(info.ConfigFileName), 1, 1, false); err != nil {
			t.api.Log().Warn("the project configuration cannot be shown", "path", info.ConfigFileName, "err", err)
			t.api.Notifications().Error("Cannot open " + filepath.Base(info.ConfigFileName) + ": " + describe(err))
		}
	})
}

// findFileReferences is Find File References: the files that import the file in front (or the file the command is given as a URI).
func (t *typescript) findFileReferences(args any) {
	const what = "Find File References"
	path := ""
	if uri, ok := args.(string); ok {
		path = filePath(uri)
	}
	if path == "" {
		f, ok := t.activeFile(t.ctx, what)
		if !ok {
			return
		}
		if path = f.path(); path == "" {
			t.api.Notifications().Warn(what + ": " + filepath.Base(f.uri) + " is not a file.")
			return
		}
	} else if !t.isRunning(what) {
		return
	}
	t.tsserverRequest(what, fileReferencesCommand, map[string]any{"file": path}, func(body json.RawMessage) {
		var refs struct {
			Refs []struct {
				File  string `json:"file"`
				Start struct {
					Line   int `json:"line"`
					Offset int `json:"offset"`
				} `json:"start"`
				LineText string `json:"lineText"`
			} `json:"refs"`
		}
		if json.Unmarshal(body, &refs) != nil || len(refs.Refs) == 0 {
			t.api.Notifications().Info(what + ": no file imports " + filepath.Base(path) + ".")
			return
		}
		env := langserver.DefaultEnv(t.api.Scope().Folder, nil)
		type place struct {
			uri          string
			line, column int
		}
		places := make([]place, 0, len(refs.Refs))
		rows := make([]extension.QuickPickItem, 0, len(refs.Refs))
		for _, r := range refs.Refs {
			if len(places) == referencesPickerLimit {
				break
			}
			places = append(places, place{fileURI(r.File), r.Start.Line, r.Start.Offset})
			rows = append(rows, extension.QuickPickItem{
				Label: strings.TrimSpace(r.LineText), Description: short(env, r.File) + ":" + strconv.Itoa(r.Start.Line), AlwaysShow: false,
			})
		}
		err := t.api.QuickPick().Show(extension.QuickPickOptions{Title: what + ": " + filepath.Base(path), Placeholder: strconv.Itoa(len(places)) + " references", Items: rows, MatchOnDescription: true}, func(n int) {
			if n < 0 || n >= len(places) {
				return
			}
			p := places[n]
			if err := t.api.Documents().Show(p.uri, p.line, p.column, false); err != nil {
				t.api.Notifications().Error("Cannot open " + filepath.Base(filePath(p.uri)) + ": " + describe(err))
			}
		})
		if err != nil && !benign(err) {
			t.api.Log().Warn("the list of references cannot be shown", "err", err)
		}
	})
}
