package dbconn

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The ids of the extension "Databases" (SPEC §10.24): the activity bar container and its view, the editor kind of the connection
// form, and the commands.
const (
	// ID is the id of the extension.
	ID = "emetos.databases"
	// ContainerID is the container of the activity bar, "Databases", shared by the three database extensions.
	ContainerID = ID + ".container"
	// ViewID is the view of the connections in the container.
	ViewID = ID + ".connections"
	// FormKindID is the editor kind of the connection form.
	FormKindID = ID + ".form"

	// NewConnection asks for an engine and opens the form of a new connection; EditConnection, DeleteConnection, Connect and
	// Disconnect take the ID of a connection as their argument, or ask which one; Refresh lists the open connections again.
	NewConnection    = ID + ".newConnection"
	EditConnection   = ID + ".editConnection"
	DeleteConnection = ID + ".deleteConnection"
	Connect          = ID + ".connect"
	Disconnect       = ID + ".disconnect"
	Refresh          = ID + ".refresh"
	// CollapseAll folds the tree of the view.
	CollapseAll = ID + ".collapseAll"
)

// Factory makes the Factory of the extension "Databases" over the hub that the three database extensions share
// (extensions.All makes one hub and gives it to each).
func Factory(h *Hub) extension.Factory {
	return func() extension.Extension { return &Extension{hub: h} }
}

// Extension is the extension "Databases" in one workspace: the view of the connections and the form tab.
type Extension struct {
	hub *Hub
	api extension.API
	ctx context.Context

	view *view
	// forms are the form tabs this instance made, which the tests look at.
	forms []*formTab
}

// Manifest implements extension.Extension.
func (*Extension) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Databases", Version: "0.1.0",
		Description:      "The connections to databases, and the view of them in the side bar, shared by the SQL, MongoDB and Redis extensions.",
		ActivationEvents: []string{extension.OnView(ViewID)},
		Contributes: extension.Contributes{
			Commands: []extension.CommandSpec{
				{ID: NewConnection, Title: "New Connection...", Category: "Databases"},
				{ID: EditConnection, Title: "Edit Connection...", Category: "Databases"},
				{ID: DeleteConnection, Title: "Delete Connection...", Category: "Databases"},
				{ID: Connect, Title: "Connect", Category: "Databases"},
				{ID: Disconnect, Title: "Disconnect", Category: "Databases"},
				{ID: Refresh, Title: "Refresh", Category: "Databases"},
				{ID: CollapseAll, Title: "Collapse All", Category: "Databases"},
			},
			ViewContainers: []extension.ViewContainerSpec{{ID: ContainerID, Title: "Databases", Icon: "database", Order: 40}},
			Views:          []extension.ViewSpec{{ID: ViewID, Name: "Connections", Container: ContainerID}},
			ViewActions: []extension.ViewActionSpec{
				{View: ViewID, Command: NewConnection, Icon: "add", Tooltip: "New Connection...", Order: 1},
				{View: ViewID, Command: Refresh, Icon: "refresh", Tooltip: "Refresh", Order: 2},
				{View: ViewID, Command: CollapseAll, Icon: "collapse-all", Tooltip: "Collapse All", Order: 3},
			},
			EditorKinds: []extension.EditorKindSpec{{ID: FormKindID, Name: "Connection", Icon: "database"}},
		},
	}
}

// Activate implements extension.Extension.
func (e *Extension) Activate(ctx context.Context, api extension.API) error {
	e.api, e.ctx = api, ctx
	e.view = newView(e)
	if err := e.view.provide(); err != nil {
		return err
	}
	if err := api.Editors().Provide(FormKindID, extension.EditorKind{Create: e.createForm}); err != nil {
		return err
	}
	handlers := map[string]extension.CommandHandler{
		NewConnection: func(_ context.Context, args any) error { e.newConnection(engineArg(args)); return nil },
		EditConnection: func(_ context.Context, args any) error {
			e.withConnection(args, "Edit which connection?", e.editConnection)
			return nil
		},
		DeleteConnection: func(_ context.Context, args any) error {
			e.withConnection(args, "Delete which connection?", e.deleteConnection)
			return nil
		},
		Connect: func(_ context.Context, args any) error {
			e.withConnection(args, "Connect to which connection?", e.connect)
			return nil
		},
		Disconnect: func(_ context.Context, args any) error {
			e.withConnection(args, "Disconnect which connection?", e.disconnect)
			return nil
		},
		Refresh:     func(context.Context, any) error { e.view.refreshAll(); return nil },
		CollapseAll: func(context.Context, any) error { e.view.collapseAll(); return nil },
	}
	for id, h := range handlers {
		if err := api.Commands().Handle(id, h); err != nil {
			return err
		}
	}
	stop := e.hub.Subscribe(func() { api.Post(e.view.rebuild) })
	stopReload := e.hub.SubscribeReload(func(id string) { api.Post(func() { e.view.reloadConnection(id) }) })
	go func() {
		<-ctx.Done()
		stop()
		stopReload()
	}()
	return nil
}

// folder is the folder of the workspace, or empty.
func (e *Extension) folder() string { return e.api.Scope().Folder }

// engineArg reads the engine a command was given: a string, or {"engine": "postgres"} (the command of another extension that
// opens the form prefilled uses the second form).
func engineArg(args any) string {
	switch a := args.(type) {
	case string:
		return a
	case map[string]any:
		s, _ := a["engine"].(string)
		return s
	}
	return ""
}

// connectionArg reads the ID of a connection a command was given: a string, or {"id": "..."}.
func connectionArg(args any) string {
	switch a := args.(type) {
	case string:
		return a
	case map[string]any:
		s, _ := a["id"].(string)
		return s
	}
	return ""
}

// withConnection runs fn for the connection the command names, or for the one the user picks from the list.
func (e *Extension) withConnection(args any, placeholder string, fn func(Connection)) {
	if id := connectionArg(args); id != "" {
		if c, ok := e.hub.Store().Get(id); ok {
			fn(c)
		} else {
			e.api.Notifications().Warn("This connection does not exist any more.")
		}
		return
	}
	conns := e.hub.Store().List(e.folder())
	if len(conns) == 0 {
		e.api.Notifications().Info("There are no connections yet. Use New Connection to add one.")
		return
	}
	items := make([]extension.QuickPickItem, len(conns))
	for i, c := range conns {
		items[i] = extension.QuickPickItem{Label: "$(" + e.iconOf(c) + ") " + c.Name, Description: describe(c)}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: placeholder, Items: items, MatchOnDescription: true}, func(i int) {
		if i >= 0 && i < len(conns) {
			fn(conns[i])
		}
	})
	if err != nil {
		e.api.Log().Warn("the list of connections cannot be shown", "err", err)
	}
}

// iconOf is the Codicon of a connection's engine.
func (e *Extension) iconOf(c Connection) string {
	if eng, _, ok := e.hub.Engine(c.Engine); ok {
		return eng.DisplayIcon()
	}
	return "database"
}

// describe is the short text after the name of a connection: where it points.
func describe(c Connection) string {
	switch {
	case c.File != "":
		return c.File
	case c.URL != "":
		return c.URL
	case c.Host != "" && c.Port != 0:
		return c.Host + ":" + strconv.Itoa(c.Port)
	}
	return c.Host
}

// newConnection opens the form of a new connection of the engine; with no engine it asks which.
func (e *Extension) newConnection(engineID string) {
	engines := e.hub.Engines()
	if engineID != "" {
		for _, eng := range engines {
			if eng.ID == engineID {
				e.openForm(formOpen{Draft: NewFormModel(eng, e.folder()).Draft()})
				return
			}
		}
		e.api.Notifications().Warn("No extension is active for the engine " + engineID + ".")
		return
	}
	switch len(engines) {
	case 0:
		e.api.Notifications().Info("No database extension is installed yet: there is no engine to connect to.")
		return
	case 1:
		e.openForm(formOpen{Draft: NewFormModel(engines[0], e.folder()).Draft()})
		return
	}
	items := make([]extension.QuickPickItem, len(engines))
	for i, eng := range engines {
		items[i] = extension.QuickPickItem{Label: "$(" + eng.DisplayIcon() + ") " + eng.Name}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Which kind of database?", Items: items}, func(i int) {
		if i >= 0 && i < len(engines) {
			e.openForm(formOpen{Draft: NewFormModel(engines[i], e.folder()).Draft()})
		}
	})
	if err != nil {
		e.api.Log().Warn("the list of engines cannot be shown", "err", err)
	}
}

// editConnection opens the form of a connection.
func (e *Extension) editConnection(c Connection) {
	eng, _, ok := e.hub.Engine(c.Engine)
	if !ok {
		e.api.Notifications().Warn("The extension for " + c.Engine + " is not active: its connections cannot be edited now.")
		return
	}
	e.openForm(formOpen{Draft: EditFormModel(eng, c, e.folder()).Draft()})
}

// deleteConnection asks, then deletes.
func (e *Extension) deleteConnection(c Connection) {
	e.api.Notifications().Warn("Delete the connection \""+c.Name+"\"? Its saved password is removed from the keyring.",
		extension.NotificationAction{Label: "Delete", Run: func() { e.doDelete(c) }})
}

// doDelete deletes a connection off the UI thread (the keyring is asked to remove its secret).
func (e *Extension) doDelete(c Connection) {
	go func() {
		keyringErr, err := e.hub.Delete(e.ctx, c.ID)
		e.api.Post(func() {
			switch {
			case err != nil:
				e.api.Notifications().Error("Cannot delete the connection \"" + c.Name + "\": " + err.Error())
			case keyringErr != nil:
				e.api.Log().Warn("the keyring did not remove a password", "err", keyringErr)
				e.api.Notifications().Warn("The connection \"" + c.Name + "\" is deleted, but the keyring did not remove its password: " + keyringErr.Error())
			}
		})
	}()
}

// connect opens a connection: it finds the password (the environment, the memory, the keyring), asks for it when there is none,
// and connects through the driver of the engine.
func (e *Extension) connect(c Connection) {
	switch e.hub.State(c.ID).Status {
	case Connecting, Connected:
		return
	}
	go func() {
		p, err := e.hub.Prepare(e.ctx, c.ID)
		e.api.Post(func() { e.prepared(c, p, err) })
	}()
}

// prepared goes on after Prepare: asks for the password, or connects.
func (e *Extension) prepared(c Connection, p Prepared, err error) {
	if err != nil && !p.Ask {
		e.hub.Fail(c.ID, err)
		e.api.Notifications().Error("Cannot connect to \"" + c.Name + "\": " + err.Error())
		return
	}
	if err != nil {
		// the keyring did not answer: the password is asked instead
		e.api.Log().Warn("the keyring did not give a password", "err", err)
		e.api.Notifications().Warn("The keyring did not answer (" + err.Error() + "). Enter the password for \"" + c.Name + "\".")
	}
	if !p.Ask {
		e.open(c, p, false)
		return
	}
	askErr := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Password for \"" + c.Name + "\" (Enter for none)", Password: true,
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		p.Password = Resolved{Password: value, Source: SourceMemory}
		e.open(c, p, true)
	})
	if askErr != nil {
		e.api.Log().Warn("the password cannot be asked", "err", askErr)
		e.hub.Fail(c.ID, askErr)
	}
}

// open connects, off the UI thread.
func (e *Extension) open(c Connection, p Prepared, typed bool) {
	e.hub.setState(c.ID, State{Status: Connecting})
	go func() {
		err := e.hub.Connect(e.ctx, p, typed)
		e.api.Post(func() {
			if err != nil {
				e.api.Notifications().Error("Cannot connect to \"" + c.Name + "\": " + err.Error())
			}
		})
	}()
}

// disconnect closes a connection. A driver that has work to lose (DisconnectGuard) is asked first, and the user is too.
func (e *Extension) disconnect(c Connection) {
	if _, drv, ok := e.hub.Engine(c.Engine); ok {
		if guard, isGuard := drv.(DisconnectGuard); isGuard {
			if work := guard.OpenWork(c); work != "" {
				err := e.api.QuickPick().Show(extension.QuickPickOptions{
					Placeholder: work,
					Items: []extension.QuickPickItem{
						{Label: "$(debug-disconnect) Disconnect", Description: "roll back and close \"" + c.Name + "\""},
						{Label: "$(close) Cancel", Description: "stay connected"},
					},
				}, func(i int) {
					if i == 0 {
						e.closeConnection(c)
					}
				})
				if err != nil {
					e.api.Log().Warn("the question about the open work cannot be shown", "err", err)
					e.api.Notifications().Warn(work + " Nothing was disconnected.")
				}
				return
			}
		}
	}
	e.closeConnection(c)
}

// closeConnection closes a connection.
func (e *Extension) closeConnection(c Connection) {
	if err := e.hub.Disconnect(c.ID); err != nil {
		e.api.Log().Warn("a connection did not close cleanly", "connection", c.Name, "err", err)
		e.api.Notifications().Warn("The connection \"" + c.Name + "\" did not close cleanly: " + err.Error())
	}
}

// sortedConnections lists the connections of the workspace by name.
func (e *Extension) sortedConnections() []Connection {
	conns := e.hub.Store().List(e.folder())
	sort.SliceStable(conns, func(i, j int) bool { return strings.ToLower(conns[i].Name) < strings.ToLower(conns[j].Name) })
	return conns
}
