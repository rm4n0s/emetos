package terminal

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The commands that put text in a terminal (VS Code's runSelectedText, runActiveFile and sendSequence), the ones that name
// terminals and shells (rename, selectDefaultShell), the find commands' entry point and the opening of links.

// editorState is the text editor in front, as the events of the editor tell it: where it is and its primary selection. The
// commands that run the selected text or the file follow it.
type editorState struct {
	uri    string
	sel    extension.Selection
	hasSel bool
}

// editorEvent follows the editor in front and its selection. When no text editor is in front (the tab shows something other
// than a text) the last one stays the one to refer to, as it does in VS Code; a document that is closed is forgotten.
func (t *terminals) editorEvent(ev extension.Event) {
	switch ev.Topic {
	case extension.TopicEditorSelectionChanged:
		var es extension.EditorSelection
		if ev.Decode(&es) != nil || es.URI == "" || len(es.Selections) == 0 {
			return
		}
		if es.URI != t.editor.uri {
			t.editor = editorState{uri: es.URI}
		}
		t.editor.sel, t.editor.hasSel = es.Selections[0], true
	case extension.TopicEditorActiveChanged:
		var ae extension.ActiveEditor
		if ev.Decode(&ae) != nil || ae.URI == "" {
			return
		}
		if ae.URI != t.editor.uri {
			t.editor = editorState{uri: ae.URI}
		}
	case extension.TopicDocumentClosed:
		var dc extension.DocumentClosed
		if ev.Decode(&dc) == nil && dc.URI == t.editor.uri {
			t.editor = editorState{}
		}
	}
}

// editorNow is the editor the commands refer to: the one the events told about, and when none did (the extension is activated
// by a command, after the editor events it would have followed) the text editor in front, as Documents().Active tells it.
func (t *terminals) editorNow() editorState {
	if t.editor.uri != "" {
		return t.editor
	}
	if doc, ok := t.api.Documents().Active(); ok && doc.URI != "" {
		t.editor = editorState{uri: doc.URI}
		if len(doc.Selections) > 0 {
			t.editor.sel, t.editor.hasSel = doc.Selections[0], true
		}
	}
	return t.editor
}

// onFind runs f on the pane whose find input has the keyboard, else on the terminal a command acts on.
func (t *terminals) onFind(f func(pv *paneView)) {
	for _, e := range t.all() {
		if e.pv.find.inputFocused {
			f(e.pv)
			return
		}
	}
	if e := t.target(); e != nil {
		f(e.pv)
	}
}

// focusTerminalAt focuses the n-th terminal of the list (from 0), the terminals of a split counted one by one.
func (t *terminals) focusTerminalAt(n int) {
	if all := t.all(); n >= 0 && n < len(all) {
		t.focusEntry(all[n], true)
	}
}

// activeOrCreate is the terminal that gets the text: the active one, or a new one when there is none.
func (t *terminals) activeOrCreate() *entry {
	if e := t.activeEntry(); e != nil {
		return e
	}
	e, err := t.open(openOpts{})
	if err != nil {
		t.api.Log().Warn("the terminal cannot be started", "err", err)
		return nil
	}
	return e
}

// textBetween is the text of a document from one position to another (zero-based lines, byte columns), in either order.
func textBetween(text string, a, b extension.Position) string {
	if b.Line < a.Line || (b.Line == a.Line && b.Col < a.Col) {
		a, b = b, a
	}
	lines := strings.Split(text, "\n")
	offset := func(p extension.Position) int {
		line := min(max(p.Line, 0), len(lines)-1)
		off := 0
		for _, l := range lines[:line] {
			off += len(l) + 1
		}
		return off + min(max(p.Col, 0), len(lines[line]))
	}
	return text[offset(a):offset(b)]
}

// searchWorkspace is VS Code's workbench.action.terminal.searchWorkspace: it opens the Search view (the Search extension's
// workbench.action.findInFiles) with the text selected in the terminal as the query, and the search starts. With nothing
// selected it does nothing. The Search extension being off is said in a notification.
func (t *terminals) searchWorkspace(ctx context.Context) {
	e := t.target()
	if e == nil {
		return
	}
	query := e.pv.selectedText()
	if query == "" {
		return
	}
	args, err := json.Marshal(map[string]any{"query": query, "triggerSearch": true})
	if err != nil {
		t.api.Log().Warn("the search arguments cannot be encoded", "err", err)
		return
	}
	err = t.api.Events().Request(ctx, extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: findInFilesCommand, Args: args}, func(_ extension.Event, err error) {
		if err == nil {
			return
		}
		if appErr, ok := errors.FromError(err); ok && (appErr.Tag == "CoreCommandNotFound" || appErr.Tag == "CoreCommandDisabled") {
			t.api.Notifications().Warn("Search Workspace needs the Search extension, which is not available here")
			return
		}
		t.api.Log().Warn("the search cannot be started", "err", err)
	})
	if err != nil {
		t.api.Log().Warn("the search cannot be asked for", "err", err)
	}
}

// runSelectedText sends the selection of the editor in front to the active terminal and presses Enter: with no selection, the
// line of the cursor with its white space trimmed. The text is pasted as bracketed paste when the shell asked for it. The
// panel shows with the keyboard staying where it is.
func (t *terminals) runSelectedText(ctx context.Context) {
	ed := t.editorNow()
	if ed.uri == "" {
		return
	}
	text, err := t.api.Documents().Read(ctx, ed.uri)
	if err != nil {
		t.api.Log().Warn("the text of the editor cannot be read", "uri", ed.uri, "err", err)
		return
	}
	var out string
	if !ed.hasSel || ed.sel.Anchor == ed.sel.Active {
		lines := strings.Split(text, "\n")
		if line := ed.sel.Active.Line; ed.hasSel && line >= 0 && line < len(lines) {
			out = strings.TrimSpace(lines[line])
		}
	} else {
		out = textBetween(text, ed.sel.Anchor, ed.sel.Active)
	}
	e := t.activeOrCreate()
	if e == nil {
		return
	}
	e.pv.sendText(out, true, true)
	t.handle.Reveal(false)
}

// runActiveFile sends the path of the file of the editor in front to the active terminal, quoted for its shell, and presses
// Enter. Only a file on disk can be run.
func (t *terminals) runActiveFile() {
	ed := t.editorNow()
	if ed.uri == "" {
		return
	}
	e := t.activeOrCreate()
	if e == nil {
		return
	}
	u, err := url.Parse(ed.uri)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		t.api.Notifications().Warn("Only files on disk can be run in the terminal")
		return
	}
	e.pv.sendText(preparePathForShell(u.Path, e.term.Profile().path, e.term.Meta().title), true, false)
	t.handle.Reveal(false)
}

// preparePathForShell quotes a path to be typed in a shell (VS Code's preparePathForShell and escapeNonWindowsPath for Linux):
// the control characters go, then the characters a shell would act on, and the path is put in the quotes the shell understands.
//
// Ported from VS Code: src/vs/workbench/contrib/terminal/common/terminalEnvironment.ts and
// src/vs/platform/terminal/common/terminalEnvironment.ts
func preparePathForShell(path, executable, title string) string {
	path = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, path)
	if executable == "" {
		return path
	}
	base := strings.TrimSuffix(filepath.Base(executable), ".exe")
	isPwsh := base == "pwsh" || title == "pwsh" || base == "powershell" || title == "powershell"
	hasSpace := strings.Contains(path, " ")
	hasParens := strings.ContainsAny(path, "()")
	if isPwsh && (hasSpace || strings.Contains(path, "'")) {
		return "& '" + strings.ReplaceAll(path, "'", "''") + "'"
	}
	if hasParens && isPwsh {
		return "& '" + path + "'"
	}
	return escapeNonWindowsPath(path, base, isPwsh)
}

// escapeNonWindowsPath puts a path in quotes for a POSIX shell, fish or PowerShell.
func escapeNonWindowsPath(path, shell string, pwsh bool) string {
	path = strings.ReplaceAll(path, `\`, `\\`)
	// the characters a shell acts on go; the quotes are escaped below
	path = strings.Map(func(r rune) rune {
		if strings.ContainsRune("`$|&>~#!^*;<", r) {
			return -1
		}
		return r
	}, path)
	single, double := strings.Contains(path, "'"), strings.Contains(path, `"`)
	switch {
	case pwsh:
		switch {
		case single && double:
			return `"` + strings.ReplaceAll(path, `"`, "`\"") + `"`
		case single:
			return "'" + strings.ReplaceAll(path, "'", "''") + "'"
		}
	case shell == "fish":
		switch {
		case single && double:
			return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
		case single:
			return "'" + strings.ReplaceAll(path, "'", `\'`) + "'"
		}
	default:
		switch {
		case single && double:
			return "$'" + strings.ReplaceAll(path, "'", `\'`) + "'"
		case single:
			return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
		}
	}
	return "'" + path + "'"
}

// hexEscape is \xAB in a sequence the user typed.
var hexEscape = regexp.MustCompile(`\\x([0-9a-fA-F]{2})`)

// sendSequence sends text to the active terminal as it is, with no Enter added: the text of the argument {text}, or what the
// user types in the box that asks (\n, \r and \xAB are understood there). ${workspaceFolder} and the other variables of the text
// are replaced.
func (t *terminals) sendSequence(ctx context.Context, args any) {
	e := t.activeEntry()
	if e == nil {
		return // VS Code sends to the active instance only
	}
	if m, ok := args.(map[string]any); ok {
		if text, _ := m["text"].(string); text != "" {
			e.pv.sendText(t.resolveVariables(ctx, text), false, false)
			return
		}
	}
	err := t.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Enter sequence to send to the terminal (supports \\n, \\r, \\xAB)"}, func(value string, ok bool) {
		if !ok || value == "" {
			return
		}
		value = strings.NewReplacer(`\n`, "\n", `\r`, "\r").Replace(value)
		value = hexEscape.ReplaceAllStringFunc(value, func(m string) string {
			n, _ := strconv.ParseUint(m[2:], 16, 8)
			return string(rune(n))
		})
		if t.alive(e) {
			e.pv.sendText(t.resolveVariables(ctx, value), false, false)
		}
	})
	if err != nil {
		t.api.Log().Warn("the box for the sequence cannot be shown", "err", err)
	}
}

var variablePattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// resolveVariables replaces the variables of VS Code's configuration resolver that a sequence may name: the folder of the
// workspace, the home folder, the environment, the file in front and the position in it. A variable it does not know, or
// that needs a file when there is none, stays as it is.
func (t *terminals) resolveVariables(ctx context.Context, text string) string {
	if !strings.Contains(text, "${") {
		return text
	}
	var file, relative string
	ed := t.editorNow()
	if u, err := url.Parse(ed.uri); err == nil && u.Scheme == "file" {
		file = u.Path
		if t.folder != "" {
			if r, err := filepath.Rel(t.folder, file); err == nil {
				relative = r
			}
		}
	}
	return variablePattern.ReplaceAllStringFunc(text, func(m string) string {
		name := m[2 : len(m)-1]
		switch {
		case strings.HasPrefix(name, "env:"):
			return os.Getenv(name[4:])
		case name == "workspaceFolder" || name == "cwd":
			if t.folder != "" {
				return t.folder
			}
		case name == "workspaceFolderBasename":
			if t.folder != "" {
				return filepath.Base(t.folder)
			}
		case name == "userHome":
			if t.env.home != "" {
				return t.env.home
			}
		case name == "pathSeparator" || name == "/":
			return string(filepath.Separator)
		case name == "file" && file != "":
			return file
		case name == "fileBasename" && file != "":
			return filepath.Base(file)
		case name == "fileBasenameNoExtension" && file != "":
			return strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		case name == "fileDirname" && file != "":
			return filepath.Dir(file)
		case name == "fileExtname" && file != "":
			return filepath.Ext(file)
		case name == "relativeFile" && relative != "":
			return relative
		case name == "relativeFileDirname" && relative != "":
			return filepath.Dir(relative)
		case name == "lineNumber" && ed.hasSel:
			return strconv.Itoa(ed.sel.Active.Line + 1)
		case name == "selectedText" && ed.hasSel && ed.uri != "":
			if text, err := t.api.Documents().Read(ctx, ed.uri); err == nil {
				return textBetween(text, ed.sel.Anchor, ed.sel.Active)
			}
		}
		return m
	})
}

// rename is the command Rename...: it asks for the name of the active terminal, which the tab then shows.
func (t *terminals) rename() { t.renameEntry(t.target()) }

// renameEntry asks for the name of a terminal.
func (t *terminals) renameEntry(e *entry) {
	if e == nil {
		return
	}
	err := t.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Enter terminal name", Value: t.title(e)}, func(value string, ok bool) {
		if !ok || strings.TrimSpace(value) == "" {
			return
		}
		if t.alive(e) {
			e.term.SetName(value)
		}
	})
	if err != nil {
		t.api.Log().Warn("the box for the name cannot be shown", "err", err)
	}
}

// renameWithArgs is the command that renames the active terminal to the name of its argument, {name}.
func (t *terminals) renameWithArgs(args any) {
	name := ""
	if m, ok := args.(map[string]any); ok {
		name, _ = m["name"].(string)
	}
	if name == "" {
		t.api.Notifications().Warn("No name argument provided")
		return
	}
	if e := t.target(); e != nil {
		e.term.SetName(name)
	}
}

// selectDefaultProfile is the command Select Default Profile: the profiles in a quick pick, and the choice is written to
// terminal.integrated.defaultProfile.linux in the settings of the user.
func (t *terminals) selectDefaultProfile() {
	profiles := t.profiles()
	if len(profiles) == 0 {
		t.api.Notifications().Warn("No terminal profile can be started: none of the programs of terminal.integrated.profiles.linux was found.")
		return
	}
	def := t.defaultProfile()
	items := make([]extension.QuickPickItem, len(profiles))
	for i, p := range profiles {
		items[i] = extension.QuickPickItem{Label: "$(" + t.iconOf(p) + ") " + p.name, Description: p.path}
		if p.name == def.name {
			items[i].Detail = "default"
		}
	}
	err := t.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select your default terminal profile", Items: items, MatchOnDescription: true}, func(i int) {
		if i < 0 || i >= len(profiles) {
			return
		}
		if err := t.api.Settings().Update(settingDefaultProfile, profiles[i].name); err != nil {
			t.api.Log().Warn("the default profile cannot be written", "err", err)
			t.api.Notifications().Error("The default terminal profile cannot be saved: " + err.Error())
		}
	})
	if err != nil {
		t.api.Log().Warn("the profiles cannot be listed", "err", err)
	}
}

// ---- links ----

// altClick says that links are followed with Alt (editor.multiCursorModifier is ctrlCmd), not with Ctrl.
func (t *terminals) altClick() bool {
	v, err := extension.SettingValue[string](t.api.Settings(), "editor.multiCursorModifier")
	return err == nil && v == "ctrlCmd"
}

// openFile opens a file of the workspace in an editor, at a line and column (none for the start).
func (t *terminals) openFile(path string, row, col int) {
	line, column := row, col
	if line < 1 {
		line = 1
	}
	if column < 1 {
		column = 1
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	if err := t.api.Documents().Show(uri, line, column, false); err != nil {
		t.api.Log().Warn("the file of the link cannot be shown", "path", path, "err", err)
		t.api.Notifications().Warn("The file cannot be opened: " + err.Error())
	}
}

// openLink opens an address with the desktop. The address of a link the program set (OSC 8) is opened only when its scheme is
// one of terminal.integrated.allowedLinkSchemes; for another one the user is asked, and the scheme is added to the setting
// when the user allows it. A file address opens in an editor when the file is one of the workspace.
func (t *terminals) openLink(target string, fromProgram bool) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme == "" {
		t.api.Log().Debug("a link has no scheme", "link", target)
		return
	}
	scheme := strings.ToLower(u.Scheme)
	open := func() {
		if scheme == "file" {
			if path := filepath.Clean(u.Path); isFileUnder(t.folder, path) {
				t.openFile(path, none, none)
			}
			return
		}
		if err := t.api.Workspace().OpenURI(t.ctx, target); err != nil {
			t.api.Log().Warn("the link cannot be opened", "link", target, "err", err)
			t.api.Notifications().Warn("The link cannot be opened: " + err.Error())
		}
	}
	if !fromProgram || slices.Contains(t.cfg.linkSchemes, scheme) {
		open()
		return
	}
	t.api.Notifications().Warn("Opening URIs can be insecure, do you want to allow opening links with the scheme "+scheme+"?",
		extension.NotificationAction{Label: "Allow " + scheme, Run: func() {
			if err := t.api.Settings().Update(settingLinkSchemes, append(slices.Clone(t.cfg.linkSchemes), scheme)); err != nil {
				t.api.Log().Warn("the setting cannot be written", "err", err)
			}
			open()
		}})
}

// iconOf is the name of the Codicon of a profile in a list.
func (t *terminals) iconOf(p profile) string {
	if _, ok := ui.CodiconCode(p.icon); ok {
		return p.icon
	}
	return "terminal"
}
