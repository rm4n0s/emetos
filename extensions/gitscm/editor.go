package gitscm

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
)

// The editor of git (SPEC §10.30 "Passwords and editors", "The editor"): git commit with no -m, git rebase -i, git merge and the like
// start GIT_EDITOR with a file; the helper sends the path, this extension opens the file in an editor tab of the window tab and answers
// when the user closes the tab: success after a save, failure when the file was not saved or the changes were dropped (git then
// aborts). The check marks of the title of the tab of a commit message accept or cancel it.
//
// Ported from VS Code: extensions/git/src/gitEditor.ts and commands.ts (commitMessageAccept, commitMessageDiscard).

// editorWait is a file git waits for.
type editorWait struct {
	reply func(helper.Reply)
	// saved says the file was written while its tab was open.
	saved bool
	// accepted says the user pressed Accept on a tab with nothing to save: the file is what it is.
	accepted bool
	// discarded says the user pressed Cancel.
	discarded bool
}

// editorMenus are the title actions of the tab of a commit message: Accept (the check mark) and Cancel (the cross).
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: editor/title)
func editorMenus() []extension.MenuItem {
	const when = "config.git.enabled && !git.missing && gitOpenRepositoryCount != 0 && editorLangId == git-commit"
	return []extension.MenuItem{
		item(extension.MenuEditorTitle, "git.commitMessageAccept", "navigation@1", when),
		item(extension.MenuEditorTitle, "git.commitMessageDiscard", "navigation@2", when),
	}
}

// openEditor opens the file git wants edited and waits for its tab to close.
func (x *gitscm) openEditor(req helper.Request, reply func(helper.Reply)) {
	path := editorPath(req)
	if path == "" {
		reply(helper.Reply{Error: "git gave Emetos no file to edit"})
		return
	}
	if old := x.editors[path]; old != nil {
		// the same file again: the first command lost its editor
		old.reply(helper.Reply{})
	}
	w := &editorWait{reply: reply}
	x.editors[path] = w
	if err := x.api.Documents().Show(uriOf(path), 1, 1, false); err != nil {
		delete(x.editors, path)
		if tag := errTag(err); tag != "ExtensionDocumentUnavailable" && tag != "ExtensionDeactivated" {
			x.log.Warn("cannot open the file git wants edited", "err", err)
		}
		reply(helper.Reply{Error: "Emetos cannot open the file in an editor: " + describe(err)})
	}
}

// editorPath is the file of an editor request: the last argument, which git gives relative to the directory it runs in.
func editorPath(req helper.Request) string {
	if len(req.Argv) == 0 {
		return ""
	}
	path := req.Argv[len(req.Argv)-1]
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		if req.Dir == "" {
			return ""
		}
		path = filepath.Join(req.Dir, path)
	}
	return filepath.Clean(path)
}

// editorSaved notes that a file git waits for was written.
func (x *gitscm) editorSaved(path string) {
	if w := x.editors[path]; w != nil {
		w.saved = true
	}
}

// onClosed answers git when the document it waits for is closed.
func (x *gitscm) onClosed(ev extension.Event) {
	var closed extension.DocumentClosed
	if err := jsonUnmarshal(ev.Data, &closed); err != nil {
		return
	}
	path, ok := uriPath(closed.URI)
	if !ok {
		return
	}
	w := x.editors[path]
	if w == nil {
		return
	}
	delete(x.editors, path)
	w.reply(helper.Reply{OK: (w.saved || w.accepted) && !w.discarded})
}

// editorTab finds the tab a commit message command is for: the document of its arguments ({"uri": ...}, as the title actions of an
// editor give them) or the active text editor.
func (x *gitscm) editorTab(args any) (path string, tab extension.DocumentTab, ok bool) {
	uri := ""
	if m, isMap := args.(map[string]any); isMap {
		uri, _ = m["uri"].(string)
	}
	if uri == "" {
		if a, has := x.api.Documents().Active(); has {
			uri = a.URI
		}
	}
	path, isFile := uriPath(uri)
	if !isFile {
		return "", extension.DocumentTab{}, false
	}
	for _, t := range x.api.Documents().Tabs() {
		if t.Kind != "text" {
			continue
		}
		if p, isFile := uriPath(t.URI); isFile && p == path {
			return path, t, true
		}
	}
	return path, extension.DocumentTab{}, false
}

// cmdCommitMessageAccept saves the commit message and closes its tab, which answers git with success.
func (x *gitscm) cmdCommitMessageAccept(_ context.Context, args any) error {
	path, tab, ok := x.editorTab(args)
	if !ok {
		return nil
	}
	if w := x.editors[path]; w != nil && !tab.Dirty {
		w.accepted = true
	}
	x.api.Documents().CloseTabsAs(extension.TabCloseSave, tab)
	return nil
}

// cmdCommitMessageDiscard closes the tab of the commit message without saving it, which answers git with failure: it aborts.
func (x *gitscm) cmdCommitMessageDiscard(_ context.Context, args any) error {
	path, tab, ok := x.editorTab(args)
	if !ok {
		return nil
	}
	if w := x.editors[path]; w != nil {
		w.discarded = true
	}
	x.api.Documents().CloseTabsAs(extension.TabCloseDiscard, tab)
	return nil
}
