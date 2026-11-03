package httpclient

import (
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

// Opening requests: the request tab for a request file, for an entry of the history, and the text of the file ("Open as Text").

// StateAsText holds, as a JSON list, the request files that the person opened as text, so that opening them again (and a restart) does
// not turn them into request tabs.
const StateAsText = "httpclient.asText"

// openRequest shows the request tab of a request file, the one that is open already when there is one.
func (e *Extension) openRequest(path string, section ...string) {
	want := ""
	if len(section) > 0 {
		want = section[0]
	}
	for _, t := range e.reqTabs {
		if t.path == path {
			t.showSection(want)
			t.tab.Reveal(true)
			return
		}
	}
	tab, err := e.api.Editors().Open(RequestKindID, reqTabState{Path: path, Section: want}, extension.EditorOpenOptions{})
	if err != nil {
		e.fail("open the request", err)
		return
	}
	tab.Reveal(true)
}

// openDraft shows a request that is not saved anywhere in a request tab: the request of an entry of the history, with its body.
func (e *Extension) openDraft(files store.RequestFiles) {
	tab, err := e.api.Editors().Open(RequestKindID, reqTabState{Draft: &files}, extension.EditorOpenOptions{})
	if err != nil {
		e.fail("open the request", err)
		return
	}
	tab.Reveal(true)
}

// openEntry opens the request of an entry of the history as a new request that is not saved anywhere.
func (e *Extension) openEntry(en history.Entry) {
	req := requestOf(en)
	if b := req.Body; b != nil { // the text of the body is in the entry, not in files
		c := *b
		c.File, c.Variables = "", ""
		req.Body = &c
	}
	e.openDraft(store.RequestFiles{Request: req, Body: en.Body, Variables: en.Variables})
}

// withRequestTab runs fn for the request tab the command names ({"tab": id}), else the one in front (or the one of the group the title
// action was run for), else says there is none.
func (e *Extension) withRequestTab(args any, fn func(*reqTab)) {
	if id := argString(args, "tab"); id != "" {
		for _, t := range e.reqTabs {
			if t.tab.ID() == id {
				fn(t)
				return
			}
		}
	}
	if t := e.tabOfGroup(extension.GroupOfArgs(args)); t != nil {
		fn(t)
		return
	}
	e.api.Notifications().Info("Open a request first.")
}

// tabOfGroup is the request tab in front: the last one shown or focused, in the group when one is given.
func (e *Extension) tabOfGroup(group int) *reqTab {
	if t := e.lastReq; t != nil && (group == 0 || t.tab.Group() == group) {
		return t
	}
	for i := len(e.reqTabs) - 1; i >= 0; i-- {
		if t := e.reqTabs[i]; group == 0 || t.tab.Group() == group {
			return t
		}
	}
	return nil
}

// openAsText shows the file of a request as text: the one the command names ({"path": ...}), else the request of the tab in front.
func (e *Extension) openAsText(args any) {
	path := nodeArg(args)
	if path == "" {
		t := e.tabOfGroup(extension.GroupOfArgs(args))
		if t == nil || t.path == "" {
			e.api.Notifications().Info("Only a request that is saved in a file can be opened as text.")
			return
		}
		path = t.path
	}
	path = e.nodePath(path)
	e.rememberAsText(path, true)
	if err := e.api.Documents().Show(fileURI(path), 0, 0, false); err != nil {
		e.fail("open the file", err)
	}
}

// nodePath makes the path of a command argument absolute: a relative path is looked for in the folders of the collections, then in
// the folder of the HTTP client, then in the workspace folder.
func (e *Extension) nodePath(p string) string {
	if filepath.IsAbs(p) || e.store == nil {
		return p
	}
	for _, base := range []string{filepath.Join(e.store.Root(), format.CollectionsDir), e.store.Root(), e.folder} {
		if full := filepath.Join(base, filepath.FromSlash(p)); e.tree != nil && e.tree.Find(full) != nil {
			return full
		}
	}
	return filepath.Join(e.folder, filepath.FromSlash(p))
}

// asText lists the files opened as text.
func (e *Extension) asText() []string {
	list, _ := extension.StateValue[[]string](e.api.State(), StateAsText)
	return list
}

// rememberAsText adds a file to the list of those opened as text, or takes it out.
func (e *Extension) rememberAsText(path string, on bool) {
	list := e.asText()
	has := slices.Contains(list, path)
	switch {
	case on && !has:
		list = append(list, path)
	case !on && has:
		list = slices.DeleteFunc(list, func(p string) bool { return p == path })
	default:
		return
	}
	var v any
	if len(list) > 0 {
		v = list
	}
	if err := e.api.State().Set(StateAsText, v); err != nil {
		e.api.Log().Warn("the list of files opened as text is not kept", "err", err)
	}
}

// watchRequestFiles makes a request file that is opened from the Explorer (or anywhere else) a request tab, unless the person asked for
// its text: the document that was opened is closed again.
func (e *Extension) watchRequestFiles() error {
	_, err := e.api.Events().Subscribe(extension.TopicDocumentOpened, func(ev extension.Event) {
		info, derr := extension.EventValue[extension.DocumentInfo](ev)
		if derr != nil {
			return
		}
		e.documentOpened(info.URI)
	})
	if err != nil {
		return err
	}
	_, err = e.api.Events().Subscribe(extension.TopicDocumentClosed, func(ev extension.Event) {
		info, derr := extension.EventValue[extension.DocumentInfo](ev)
		if derr != nil {
			return
		}
		if path, ok := e.requestPathOf(info.URI); ok {
			e.rememberAsText(path, false)
		}
	})
	return err
}

// requestPathOf is the path of a request file of the collections of this workspace that a document URI names.
func (e *Extension) requestPathOf(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || !strings.HasSuffix(u.Path, format.RequestSuffix) || e.folder == "" {
		return "", false
	}
	root := filepath.Join(e.folder, filepath.FromSlash(format.Dir), format.CollectionsDir) + string(filepath.Separator)
	if !strings.HasPrefix(u.Path, root) {
		return "", false
	}
	return u.Path, true
}

// documentOpened turns the document of a request file into a request tab.
func (e *Extension) documentOpened(uri string) {
	path, ok := e.requestPathOf(uri)
	if !ok || slices.Contains(e.asText(), path) {
		return
	}
	e.api.Post(func() {
		if e.store == nil || e.tree == nil || e.tree.Find(path) == nil || slices.Contains(e.asText(), path) {
			return
		}
		group := 0
		var close []extension.DocumentTab
		for _, t := range e.api.Documents().Tabs() {
			if t.Kind == "text" && t.URI == uri && !t.Dirty {
				close = append(close, t)
				group = t.Group
			}
		}
		if len(close) == 0 {
			return // a document that is edited or has no tab of its own stays as it is
		}
		tab, err := e.api.Editors().Open(RequestKindID, reqTabState{Path: path}, extension.EditorOpenOptions{Group: group})
		if err != nil {
			e.fail("open the request", err)
			return
		}
		e.api.Documents().CloseTabs(close...)
		tab.Reveal(true)
	})
}
