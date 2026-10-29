package redis

import (
	"context"
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// LinesToRun picks the command lines a Run asks for from the text of a .redis file: all of them (Run All), the lines a selection
// touches, or the line of the cursor. A selection that ends at the start of a line does not take that line. A .redis file is one
// command in each line; a blank line and a line that starts with # are skipped when the lines run.
func LinesToRun(text string, sel extension.Selection, all bool) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if all {
		return lines
	}
	start, end := sel.Anchor, sel.Active
	if end.Line < start.Line || (end.Line == start.Line && end.Col < start.Col) {
		start, end = end, start
	}
	last := end.Line
	if last > start.Line && end.Col == 0 {
		last--
	}
	first := max(start.Line, 0)
	last = min(last, len(lines)-1)
	if first > last {
		return nil
	}
	return lines[first : last+1]
}

// runFile runs the commands of the active .redis file in the console of its connection (SPEC §10.23 "The console": .redis files run like
// playgrounds). all runs the whole file, else the selected lines or the line of the cursor.
func (x *Extension) runFile(all bool) {
	doc, ok := x.api.Documents().Active()
	if !ok || doc.LanguageID != RedisLanguageID {
		x.api.Notifications().Info("Open a .redis file to run its commands.")
		return
	}
	text, err := x.api.Documents().Read(x.ctx, doc.URI)
	if err != nil {
		x.api.Log().Warn("the file cannot be read", "err", err)
		x.api.Notifications().Error("Cannot read the file: " + err.Error())
		return
	}
	var sel extension.Selection
	if len(doc.Selections) > 0 {
		sel = doc.Selections[0]
	}
	lines := LinesToRun(text, sel, all)
	conn := x.connectionOfFile(doc.URI)
	if conn == "" {
		x.api.Notifications().Warn("No Redis connection is open. Connect one in the Databases view, then run again.")
		return
	}
	x.runLinesOn(conn, lines)
}

// runLinesOn shows the console of a connection and runs lines in it.
func (x *Extension) runLinesOn(conn string, lines []string) {
	if len(lines) == 0 {
		x.api.Notifications().Info("There is no command to run.")
		return
	}
	x.setTarget(conn)
	x.showPanel(ConsoleViewID)
	x.consoleFor(conn).runLines(lines)
}

// connectionOfFile is the connection a file runs on: the one chosen for it, else the connection the tabs show, if it is open.
func (x *Extension) connectionOfFile(uri string) string {
	if id, ok := x.fileConn[uri]; ok && x.drv.Connected(id) {
		return id
	}
	return x.chooseTarget()
}

// bindFile remembers the connection of a file, for this session.
func (x *Extension) bindFile(uri, conn string) {
	x.fileConn[uri] = conn
	x.cmds.mu.Lock()
	x.cmds.bound[uri] = conn
	x.cmds.mu.Unlock()
	x.updateFileStatus()
}

// setTarget makes a connection the one the tabs of the panel show.
func (x *Extension) setTarget(conn string) {
	x.target = conn
	x.cmds.mu.Lock()
	x.cmds.target = conn
	x.cmds.mu.Unlock()
}

// updateFileStatus shows the connection of the active .redis file in the status bar, and hides the item for any other file.
func (x *Extension) updateFileStatus() {
	if x.status == nil {
		return
	}
	doc, ok := x.api.Documents().Active()
	if !ok || doc.LanguageID != RedisLanguageID {
		x.status.SetVisible(false)
		return
	}
	text := "$(database) No connection"
	if id := x.connectionOfFile(doc.URI); id != "" {
		text = "$(database) " + x.connectionName(id)
	}
	x.status.Update(extension.StatusItem{
		ID: statusID, Name: "Redis connection", Text: text, Tooltip: "The Redis connection this file runs on (click to change)",
		Alignment: extension.StatusLeft, Priority: 90, Command: CmdSelectConn,
	})
	x.status.SetVisible(true)
}

// statusID is the id of the status bar item.
const statusID = "connection"

// selectConnection asks for the connection of the active .redis file.
func (x *Extension) selectConnection() {
	doc, ok := x.api.Documents().Active()
	if !ok || doc.LanguageID != RedisLanguageID {
		x.api.Notifications().Info("Open a .redis file to choose its connection.")
		return
	}
	live := x.liveConnections()
	if len(live) == 0 {
		x.api.Notifications().Warn("No Redis connection is open. Connect one in the Databases view.")
		return
	}
	// a quick pick even with one connection: the user asked to choose
	items := make([]extension.QuickPickItem, len(live))
	for i, c := range live {
		items[i] = extension.QuickPickItem{Label: c.Name, Description: c.Host}
	}
	err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Redis connection of this file", Items: items}, func(i int) {
		if i >= 0 && i < len(live) {
			x.bindFile(doc.URI, live[i].ID)
		}
	})
	if err != nil {
		x.api.Log().Warn("the connection cannot be asked", "err", err)
	}
}

// completer completes the names of commands in .redis files.
type completer struct{ x *Extension }

var _ extension.CompletionProvider = completer{}

// ProvideCompletion implements extension.CompletionProvider: the first word of a line completes to the commands of the connection the
// file runs on (the server's list from COMMAND DOCS, else a built-in list).
func (c completer) ProvideCompletion(_ context.Context, doc extension.TextDocument, pos extension.Position, _ extension.CompletionTrigger) (extension.CompletionList, error) {
	lines := strings.Split(doc.Text, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return extension.CompletionList{}, nil
	}
	line := lines[pos.Line]
	col := min(max(pos.Col, 0), len(line))
	prefix := line[:col]
	trimmed := strings.TrimLeft(prefix, " \t")
	if strings.ContainsAny(trimmed, " \t") || strings.HasPrefix(trimmed, "#") {
		return extension.CompletionList{}, nil
	}
	return extension.CompletionList{Items: CompletionItems(c.x.docsForFile(doc.URI), trimmed, pos)}, nil
}

// docsForFile are the commands for a file: those of the connection it is bound to, else of the one the tabs show.
func (x *Extension) docsForFile(uri string) []CommandDoc {
	x.cmds.mu.Lock()
	conn := x.cmds.bound[uri]
	if conn == "" {
		conn = x.cmds.target
	}
	docs := x.cmds.docs[conn]
	x.cmds.mu.Unlock()
	if len(docs) > 0 {
		return docs
	}
	return builtinDocs()
}

// CompletionItems are the commands that start with the word typed (without regard to case), replacing that word ending at pos.
func CompletionItems(docs []CommandDoc, typed string, pos extension.Position) []extension.CompletionItem {
	upper := strings.ToUpper(typed)
	rng := extension.Range{Start: extension.Position{Line: pos.Line, Col: pos.Col - len(typed)}, End: pos}
	var items []extension.CompletionItem
	for _, d := range docs {
		if !strings.HasPrefix(d.Name, upper) {
			continue
		}
		item := extension.CompletionItem{Label: d.Name, Kind: extension.CompletionFunction, InsertText: d.Name, Range: &rng, SortText: d.Name}
		if d.Summary != "" {
			item.Detail = d.Summary
		}
		items = append(items, item)
	}
	slices.SortFunc(items, func(a, b extension.CompletionItem) int { return strings.Compare(a.Label, b.Label) })
	return items
}
