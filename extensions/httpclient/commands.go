package httpclient

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

// The commands of the extension (SPEC §10.33 "Commands"), in the category "HTTP Client".
const (
	CmdFocus             = ID + ".focus"
	CmdNewCollection     = ID + ".newCollection"
	CmdNewFolder         = ID + ".newFolder"
	CmdNewRequest        = ID + ".newRequest"
	CmdNewEnvironment    = ID + ".newEnvironment"
	CmdOpenRequest       = ID + ".openRequest"
	CmdRename            = ID + ".rename"
	CmdDuplicate         = ID + ".duplicate"
	CmdDelete            = ID + ".delete"
	CmdCopyName          = ID + ".copyName"
	CmdRefresh           = ID + ".refresh"
	CmdCollapseAll       = ID + ".collapseAll"
	CmdSelectEnvironment = ID + ".selectEnvironment"
	CmdEditEnvironment   = ID + ".editEnvironment"
	CmdOpenHistory       = ID + ".openHistory"
	CmdSaveHistory       = ID + ".saveHistoryToCollection"
	CmdClearHistory      = ID + ".clearHistory"
)

const category = "HTTP Client"

// commandSpecs are the commands the manifest declares.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: CmdFocus, Title: "Focus on Collections View", Category: category},
		{ID: CmdNewCollection, Title: "New Collection...", Category: category, Icon: "new-folder"},
		{ID: CmdNewFolder, Title: "New Folder...", Category: category, Icon: "new-folder"},
		{ID: CmdNewRequest, Title: "New Request...", Category: category, Icon: "new-file"},
		{ID: CmdNewEnvironment, Title: "New Environment...", Category: category, Icon: "add"},
		{ID: CmdOpenRequest, Title: "Open Request", Category: category},
		{ID: CmdRename, Title: "Rename...", Category: category},
		{ID: CmdDuplicate, Title: "Duplicate", Category: category},
		{ID: CmdDelete, Title: "Delete...", Category: category},
		{ID: CmdCopyName, Title: "Copy Name", Category: category},
		{ID: CmdRefresh, Title: "Refresh", Category: category, Icon: "refresh"},
		{ID: CmdCollapseAll, Title: "Collapse All", Category: category, Icon: "collapse-all"},
		{ID: CmdSelectEnvironment, Title: "Select Environment...", Category: category},
		{ID: CmdEditEnvironment, Title: "Edit Environment...", Category: category},
		{ID: CmdOpenHistory, Title: "Open History Entry...", Category: category},
		{ID: CmdSaveHistory, Title: "Save History Entry to Collection...", Category: category},
		{ID: CmdClearHistory, Title: "Clear History", Category: category, Icon: "clear-all"},
	}
}

// viewActions are the buttons at the right end of the headers of the views.
func viewActions() []extension.ViewActionSpec {
	return []extension.ViewActionSpec{
		{View: CollectionsViewID, Command: CmdNewCollection, Icon: "new-folder", Tooltip: "New Collection...", Order: 1},
		{View: CollectionsViewID, Command: CmdNewRequest, Icon: "new-file", Tooltip: "New Request...", Order: 2},
		{View: CollectionsViewID, Command: CmdRefresh, Icon: "refresh", Tooltip: "Refresh", Order: 3},
		{View: CollectionsViewID, Command: CmdCollapseAll, Icon: "collapse-all", Tooltip: "Collapse All", Order: 4},
		{View: EnvironmentsViewID, Command: CmdNewEnvironment, Icon: "add", Tooltip: "New Environment...", Order: 1},
		{View: HistoryViewID, Command: CmdClearHistory, Icon: "clear-all", Tooltip: "Clear History", Order: 1},
	}
}

// settingSpecs are the settings of the extension. The ones that decide where requests go or what they trust are the user's alone
// (scope machine): a folder's settings must not turn TLS verification off, name a proxy, give a client certificate or let a collection read
// the environment.
func settingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{
		{Key: SettingTimeoutMs, Type: "integer", Default: 30000, Minimum: ptr(0.0), Category: category,
			Description: "How long a request may take, in milliseconds, the time to read the response included. 0 is no timeout. A request's own setting wins."},
		{Key: SettingVerifyTLS, Type: "boolean", Default: true, Scope: "machine", Category: category,
			Description: "Check the certificate of an HTTPS server. Turning this off is for testing: a request that is sent without the check shows a warning."},
		{Key: SettingProxy, Type: "string", Default: "", Scope: "machine", Category: category,
			Description: "The proxy requests go through (`http://host:port`). Empty uses `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` of the environment."},
		{Key: SettingClientCerts, Type: "array", Default: []any{}, Scope: "machine", Category: category,
			Description: "Client certificates by host: objects with `host` (a pattern such as `*.example.com`), `certificate` and `key` (files). The passphrase of a key is kept in the keyring."},
		{Key: SettingHistoryLimit, Type: "integer", Default: 200, Minimum: ptr(0.0), Maximum: ptr(1000.0), Category: category,
			Description: "How many sent requests the History view keeps for the workspace. 0 keeps none."},
		{Key: SettingResponseLayout, Type: "string", Default: "vertical", Enum: []any{"vertical", "horizontal"}, Category: category,
			EnumDescriptions: []string{"The response below the request.", "The response beside the request."},
			Description:      "Where the response is shown in relation to the request."},
		{Key: SettingAllowedProcessEnv, Type: "array", Default: []any{}, Scope: "machine", Category: category,
			Description: "The names of the environment variables that `{{$processEnv NAME}}` may read. Empty by default, so a collection cannot read the environment unasked."},
		{Key: SettingMaxResponseMiB, Type: "integer", Default: 50, Minimum: ptr(1.0), Scope: "machine", Category: category,
			Description: "How much of a response is kept in memory, in MiB; a larger body is streamed to a file in Emetos's cache."},
	}
}

func ptr[T any](v T) *T { return &v }

// handleCommands sets the handlers of the commands.
func (e *Extension) handleCommands() error {
	h := map[string]extension.CommandHandler{
		CmdFocus: func(context.Context, any) error {
			e.collections.handle.Reveal(true)
			return nil
		},
		CmdNewCollection:  func(context.Context, any) error { e.newCollection(); return nil },
		CmdNewFolder:      func(_ context.Context, a any) error { e.newFolder(a); return nil },
		CmdNewRequest:     func(_ context.Context, a any) error { e.newRequest(a); return nil },
		CmdNewEnvironment: func(context.Context, any) error { e.newEnvironment(); return nil },
		CmdOpenRequest: func(_ context.Context, a any) error {
			e.withNode(a, "Open which request?", func(n *store.Node) { e.openNode(n) }, store.KindRequest)
			return nil
		},
		CmdRename: func(_ context.Context, a any) error {
			e.withNode(a, "Rename which one?", e.rename)
			return nil
		},
		CmdDuplicate: func(_ context.Context, a any) error {
			e.withNode(a, "Duplicate which one?", e.duplicate, store.KindCollection, store.KindFolder, store.KindRequest, store.KindEnvironment)
			return nil
		},
		CmdDelete: func(_ context.Context, a any) error {
			e.withNode(a, "Delete which one?", e.delete)
			return nil
		},
		CmdCopyName: func(_ context.Context, a any) error {
			e.withNode(a, "Copy the name of which one?", func(n *store.Node) { e.copyText(n.Name) })
			return nil
		},
		CmdRefresh:     func(context.Context, any) error { e.refresh(); return nil },
		CmdCollapseAll: func(context.Context, any) error { e.collections.collapseAll(); return nil },
		CmdSelectEnvironment: func(_ context.Context, a any) error {
			e.selectEnvironment(a)
			return nil
		},
		CmdEditEnvironment: func(_ context.Context, a any) error {
			e.withNode(a, "Edit which environment?", e.editEnvironment, store.KindEnvironment)
			return nil
		},
		CmdOpenHistory: func(_ context.Context, a any) error {
			e.withEntry(a, "Open which request?", e.openEntry)
			return nil
		},
		CmdSaveHistory: func(_ context.Context, a any) error {
			e.withEntry(a, "Save which request?", e.saveEntry)
			return nil
		},
		CmdClearHistory: func(context.Context, any) error { e.clearHistory(); return nil },
	}
	for id, fn := range h {
		if err := e.api.Commands().Handle(id, fn); err != nil {
			return err
		}
	}
	return nil
}

// ---- finding the row a command is for ----

// nodeArg reads the path of the node a command was given: a string, or {"path": "..."}.
func nodeArg(args any) string { return argString(args, "path") }

// withNode runs fn for the node the command names, or the one selected in the Collections view (when it is of one of the kinds), or the one
// the user picks from a list. kinds say which nodes fit; none means every kind.
func (e *Extension) withNode(args any, placeholder string, fn func(*store.Node), kinds ...store.Kind) {
	if !e.ready() {
		return
	}
	fits := func(n *store.Node) bool {
		if len(kinds) == 0 {
			return true
		}
		for _, k := range kinds {
			if n.Kind == k {
				return true
			}
		}
		return false
	}
	if path := nodeArg(args); path != "" {
		if n := e.tree.Find(path); n != nil && fits(n) {
			fn(n)
		} else {
			e.api.Notifications().Warn("This does not exist any more.")
		}
		return
	}
	if n := e.collections.selectedNode(); n != nil && fits(n) {
		fn(n)
		return
	}
	var nodes []*store.Node
	e.tree.Walk(func(n *store.Node) bool {
		if fits(n) {
			nodes = append(nodes, n)
		}
		return true
	})
	if len(nodes) == 0 {
		e.api.Notifications().Info("There is nothing to choose from yet. Use New Collection or New Environment first.")
		return
	}
	items := make([]extension.QuickPickItem, len(nodes))
	for i, n := range nodes {
		items[i] = extension.QuickPickItem{Label: "$(" + nodeIcon(n) + ") " + n.Name, Description: e.where(n)}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: placeholder, Items: items, MatchOnDescription: true}, func(i int) {
		if i >= 0 && i < len(nodes) {
			fn(nodes[i])
		}
	})
	if err != nil {
		e.fail("show the list", err)
	}
}

// where is the place of a node, "Collection / Folder", for the lists.
func (e *Extension) where(n *store.Node) string {
	if n.Kind == store.KindEnvironment {
		return "environment"
	}
	var parts []string
	for p := e.tree.Parent(n); p != nil; p = e.tree.Parent(p) {
		parts = append([]string{p.Name}, parts...)
	}
	if n.Kind == store.KindCollection {
		return "collection"
	}
	return strings.Join(parts, " / ")
}

// nodeIcon is the Codicon of a kind of node.
func nodeIcon(n *store.Node) string {
	switch n.Kind {
	case store.KindCollection:
		return "library"
	case store.KindFolder:
		return "folder"
	case store.KindEnvironment:
		return "symbol-variable"
	}
	return "file"
}

// openNode shows a request or opens the editor of an environment.
func (e *Extension) openNode(n *store.Node) {
	switch n.Kind {
	case store.KindRequest:
		e.openFile(n.File)
	case store.KindEnvironment:
		e.editEnvironment(n)
	}
}

// ---- making things ----

// askName shows the input box for a name and calls fn with the trimmed name.
func (e *Extension) askName(prompt, value string, fn func(name string)) {
	err := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: prompt, Value: value,
		Validate: func(v string) string {
			if strings.TrimSpace(v) == "" {
				return "The name cannot be empty."
			}
			return ""
		},
	}, func(v string, ok bool) {
		if ok && strings.TrimSpace(v) != "" {
			fn(strings.TrimSpace(v))
		}
	})
	if err != nil {
		e.fail("ask for the name", err)
	}
}

// newCollection asks for a name and makes a collection.
func (e *Extension) newCollection() {
	if !e.ready() {
		return
	}
	e.askName("Name of the new collection", "", func(name string) {
		var path string
		e.async(func(ctx context.Context) (err error) {
			path, err = e.store.CreateCollection(ctx, name)
			return err
		}, func(err error) {
			if err != nil {
				e.fail("make the collection", err)
				return
			}
			e.collections.reveal(path)
		})
	})
}

// newFolder asks for a name and makes a folder in the collection or folder the command names.
func (e *Extension) newFolder(args any) {
	e.withNode(args, "Make the folder in which collection or folder?", func(parent *store.Node) {
		e.askName("Name of the new folder", "", func(name string) {
			var path string
			e.async(func(ctx context.Context) (err error) {
				path, err = e.store.CreateFolder(ctx, parent.Path, name)
				return err
			}, func(err error) {
				if err != nil {
					e.fail("make the folder", err)
					return
				}
				e.collections.reveal(path)
			})
		})
	}, store.KindCollection, store.KindFolder)
}

// newRequest asks for a name and makes a request in the collection or folder the command names, then opens it.
func (e *Extension) newRequest(args any) {
	e.withNode(args, "Make the request in which collection or folder?", func(parent *store.Node) {
		e.askName("Name of the new request", "", func(name string) {
			var path string
			e.async(func(ctx context.Context) (err error) {
				path, err = e.store.CreateRequest(ctx, parent.Path, store.RequestFiles{Request: format.Request{Name: name, Method: "GET"}})
				return err
			}, func(err error) {
				if err != nil {
					e.fail("make the request", err)
					return
				}
				e.collections.reveal(path)
				e.openFile(path)
			})
		})
	}, store.KindCollection, store.KindFolder)
}

// newEnvironment asks for a name, makes the environment and opens its editor.
func (e *Extension) newEnvironment() {
	if !e.ready() {
		return
	}
	e.askName("Name of the new environment", "", func(name string) {
		var path string
		e.async(func(ctx context.Context) (err error) {
			path, err = e.store.CreateEnvironment(ctx, name)
			return err
		}, func(err error) {
			if err != nil {
				e.fail("make the environment", err)
				return
			}
			if n := e.tree.Find(path); n != nil {
				e.editEnvironment(n)
			}
		})
	})
}

// rename asks for the new name and renames the node and its files.
func (e *Extension) rename(n *store.Node) {
	if n.ReadOnly {
		e.api.Notifications().Warn("\"" + n.Name + "\" is from a newer version of the format and is read-only.")
		return
	}
	e.askName("New name of \""+n.Name+"\"", n.Name, func(name string) {
		if name == n.Name {
			return
		}
		oldKey, kind := n.Key, n.Kind
		var path string
		e.async(func(ctx context.Context) (err error) {
			path, err = e.store.Rename(ctx, n.Path, name)
			return err
		}, func(err error) {
			if err != nil {
				e.fail("rename \""+n.Name+"\"", err)
				return
			}
			if kind == store.KindEnvironment {
				e.environmentRenamed(oldKey, envKeyOf(path))
			}
			e.collections.reveal(path)
		})
	})
}

// duplicate makes a copy of the node.
func (e *Extension) duplicate(n *store.Node) {
	var path string
	e.async(func(ctx context.Context) (err error) {
		path, err = e.store.Duplicate(ctx, n.Path)
		return err
	}, func(err error) {
		if err != nil {
			e.fail("duplicate \""+n.Name+"\"", err)
			return
		}
		e.collections.reveal(path)
	})
}

// delete asks, then moves the node and its body files to the trash, and offers to take it back.
func (e *Extension) delete(n *store.Node) {
	what := map[store.Kind]string{store.KindCollection: "collection", store.KindFolder: "folder", store.KindRequest: "request", store.KindEnvironment: "environment"}[n.Kind]
	detail := "It is moved to the trash."
	if n.Kind == store.KindCollection || n.Kind == store.KindFolder {
		detail = "Everything in it goes with it. It is moved to the trash."
	}
	err := e.api.Dialogs().Ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "Delete the " + what + " \"" + n.Name + "\"?", Detail: detail,
		Buttons: []extension.DialogButton{{Label: "Delete"}, {Label: "Cancel", Cancel: true}},
	}, func(r extension.DialogResult) {
		if r.Button != 0 {
			return
		}
		var undo func(ctx context.Context) error
		e.async(func(ctx context.Context) (err error) {
			undo, err = e.store.Delete(ctx, n.Path)
			return err
		}, func(err error) {
			if err != nil {
				e.fail("delete \""+n.Name+"\"", err)
				return
			}
			e.api.Notifications().Info("Moved \""+n.Name+"\" to the trash.", extension.NotificationAction{Label: "Undo", Run: func() {
				e.async(undo, func(err error) {
					if err != nil {
						e.fail("take \""+n.Name+"\" back from the trash", err)
					}
				})
			}})
		})
	})
	if err != nil {
		e.fail("ask", err)
	}
}

// refresh reads the files again.
func (e *Extension) refresh() {
	if !e.ready() {
		return
	}
	e.async(func(ctx context.Context) error { _, err := e.store.Refresh(ctx); return err }, func(err error) {
		if err != nil {
			e.fail("read the files", err)
		}
	})
}
