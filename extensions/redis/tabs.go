package redis

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// activateTabs provides the tab of the panel, the commands, the completion of .redis files and the status bar item.
func (x *Extension) activateTabs() error {
	api := x.api
	var err error
	if x.views.panel, err = api.Views().Provide(PanelViewID, extension.ViewContent{Build: x.buildPanel}); err != nil {
		return err
	}
	for id, fn := range x.commandHandlers() {
		if err := api.Commands().Handle(id, fn); err != nil {
			return err
		}
	}
	if _, err := api.Languages().RegisterCompletionProvider(extension.DocumentSelector{Languages: []string{RedisLanguageID}}, completer{x}); err != nil {
		return err
	}
	if x.status, err = api.StatusBar().Add(extension.StatusItem{ID: statusID, Name: "Redis connection", Alignment: extension.StatusLeft, Priority: 90, Command: CmdSelectConn}); err != nil {
		return err
	}
	x.status.SetVisible(false)
	if _, err := api.Events().Subscribe(extension.TopicEditorActiveChanged, func(extension.Event) { x.updateFileStatus() }); err != nil {
		return err
	}
	stop := x.hub.Subscribe(func() { api.Post(x.connectionsChanged) })
	go func() {
		<-x.ctx.Done()
		stop()
	}()
	return nil
}

// commandHandlers are the handlers of the commands of the step.
func (x *Extension) commandHandlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		CmdOpenConsole: func(_ context.Context, args any) error {
			if conn := x.connectionArg(args); conn != "" {
				x.setTarget(conn)
			}
			if x.chooseTarget() == "" {
				x.api.Notifications().Warn("No Redis connection is open. Connect one in the Databases view.")
			}
			x.showPanel(ConsoleViewID)
			return nil
		},
		CmdClearConsole: func(context.Context, any) error {
			if conn := x.chooseTarget(); conn != "" {
				x.consoleFor(conn).clear()
			}
			return nil
		},
		CmdCancel: func(context.Context, any) error {
			if conn := x.chooseTarget(); conn != "" {
				x.consoleFor(conn).cancel()
			}
			return nil
		},
		CmdCopyLastReply: func(context.Context, any) error {
			if conn := x.chooseTarget(); conn != "" {
				x.consoleFor(conn).copyLast()
			}
			return nil
		},
		CmdRun: func(_ context.Context, args any) error {
			if lines, ok := linesArg(args); ok {
				conn := x.connectionArg(args)
				if conn == "" {
					conn = x.chooseTarget()
				}
				if conn == "" {
					x.api.Notifications().Warn("No Redis connection is open. Connect one in the Databases view.")
					return nil
				}
				x.runLinesOn(conn, lines)
				return nil
			}
			x.runFile(false)
			return nil
		},
		CmdOpenKey: func(_ context.Context, args any) error {
			x.openKeyCommand(args)
			return nil
		},
		CmdRunAll: func(context.Context, any) error { x.runFile(true); return nil },
		CmdSelectConn: func(context.Context, any) error {
			x.selectConnection()
			return nil
		},
		CmdOpenPubSub: func(_ context.Context, args any) error {
			x.openPanel(PubSubViewID, args)
			return nil
		},
		CmdSubscribe:  func(context.Context, any) error { x.subscribePrompt(false); return nil },
		CmdPSubscribe: func(context.Context, any) error { x.subscribePrompt(true); return nil },
		CmdPublish:    func(context.Context, any) error { x.publishPrompt(); return nil },
		CmdOpenMonitor: func(_ context.Context, args any) error {
			x.openPanel(MonitorViewID, args)
			return nil
		},
		CmdStartMonitor: func(_ context.Context, args any) error {
			x.openPanel(MonitorViewID, args)
			if conn := x.chooseTarget(); conn != "" {
				x.monitorFor(conn).askStart()
			}
			return nil
		},
		CmdStopMonitor: func(context.Context, any) error {
			if conn := x.chooseTarget(); conn != "" {
				x.monitorFor(conn).stop()
			}
			return nil
		},
		CmdServerInfo: func(_ context.Context, args any) error {
			if conn := x.connectionArg(args); conn != "" {
				x.setTarget(conn)
			}
			conn := x.chooseTarget()
			if conn == "" {
				x.api.Notifications().Warn("No Redis connection is open. Connect one in the Databases view.")
				return nil
			}
			x.openServerInfo(conn)
			return nil
		},
	}
}

// connectionArg reads the connection a command was given: its id, or {"connection": id}.
func (x *Extension) connectionArg(args any) string {
	switch a := args.(type) {
	case string:
		return a
	case map[string]any:
		if s, ok := a["connection"].(string); ok {
			return s
		}
	}
	return ""
}

// linesArg reads the command lines a command was given: {"lines": ["PING", "GET k"]}.
func linesArg(args any) ([]string, bool) {
	m, ok := args.(map[string]any)
	if !ok {
		return nil, false
	}
	raw, ok := m["lines"].([]any)
	if !ok {
		return nil, false
	}
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		s, ok := l.(string)
		if !ok {
			return nil, false
		}
		lines = append(lines, s)
	}
	return lines, true
}

// openKeyCommand opens a key in the key editor: the one the arguments name ({connection, db, key}), else the one the user types, in
// the database of the console of the open connection.
func (x *Extension) openKeyCommand(args any) {
	if m, ok := args.(map[string]any); ok {
		if key, ok := m["key"].(string); ok && key != "" {
			conn := x.connectionArg(args)
			if conn == "" {
				conn = x.chooseTarget()
			}
			db := 0
			if n, ok := m["db"].(float64); ok {
				db = int(n)
			}
			if conn == "" {
				x.api.Notifications().Warn("No Redis connection is open. Connect one in the Databases view.")
				return
			}
			x.openKey(KeyRef{Conn: conn, DB: db, Key: key})
			return
		}
	}
	conn := x.chooseTarget()
	if conn == "" {
		x.api.Notifications().Warn("No Redis connection is open. Connect one in the Databases view.")
		return
	}
	db := x.consoleDB(conn)
	err := x.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Key to open in database " + itoa(db) + " of " + x.connectionName(conn)}, func(key string, ok bool) {
		if ok && key != "" {
			x.openKey(KeyRef{Conn: conn, DB: db, Key: key})
		}
	})
	if err != nil {
		x.api.Log().Warn("the key cannot be asked", "err", err)
	}
}

// openPanel makes the connection of the arguments the target and shows a tab of the panel.
func (x *Extension) openPanel(view string, args any) {
	if conn := x.connectionArg(args); conn != "" {
		x.setTarget(conn)
	}
	if x.chooseTarget() == "" {
		x.api.Notifications().Warn("No Redis connection is open. Connect one in the Databases view.")
	}
	x.showPanel(view)
}

// showPanel shows a page of the Redis tab of the panel with the focus.
func (x *Extension) showPanel(view string) {
	x.panelTab = view
	if h := x.views.panel; h != nil {
		h.Refresh()
		h.Reveal(true)
	}
}

// connectionsChanged follows the Databases view: a connection that closed ends what ran on it, and the tabs list the open ones again.
func (x *Extension) connectionsChanged() {
	if x.ctx.Err() != nil {
		return
	}
	for id, t := range x.pubsubs {
		if !x.drv.Connected(id) && (t.sub != nil || t.opening) {
			t.close()
			t.note = pageui.Fine("The connection closed.")
		}
	}
	for id, t := range x.monitors {
		if !x.drv.Connected(id) && (t.running() || t.starting) {
			t.stop()
			t.note = pageui.Fine("The connection closed.")
		}
	}
	for id, c := range x.consoles {
		if !x.drv.Connected(id) && c.busy() {
			c.cancel()
		}
	}
	x.refreshConsole()
	x.refreshPubSub()
	x.refreshMonitor()
	x.updateFileStatus()
}

// sessionOpened is called when a connection opens: the events, and the tabs list the connection.
func (x *Extension) sessionOpened(c dbconn.Connection) {
	x.publishConnection(TopicConnected, c)
	x.api.Post(x.connectionsChanged)
}

// sessionClosed is called when a connection closes.
func (x *Extension) sessionClosed(c dbconn.Connection) {
	x.publishConnection(TopicDisconnected, c)
	x.forgetCommandDocs(c.ID)
	x.api.Post(x.connectionsChanged)
}

// opened tells the extension that the driver opened a session, from the goroutine that connected; the tabs and the events follow on
// the UI thread. It does nothing when the activation has not happened or has ended.
func (x *Extension) opened(c dbconn.Connection) {
	if x.api == nil || x.ctx == nil || x.ctx.Err() != nil {
		return
	}
	x.api.Post(func() { x.sessionOpened(c) })
}

// closed is opened for a session that ended.
func (x *Extension) closed(c dbconn.Connection) {
	if x.api == nil || x.ctx == nil || x.ctx.Err() != nil {
		return
	}
	x.api.Post(func() { x.sessionClosed(c) })
}

// openConsoleOn shows the console of a connection; db is the database its commands go to from now on, or -1 to keep the one it has.
func (x *Extension) openConsoleOn(conn string, db int) {
	x.setTarget(conn)
	c := x.consoleFor(conn)
	if db >= 0 && c.db != db {
		c.db = db
		c.add(entryNote, "The commands now go to database "+itoa(db)+".")
	}
	x.showPanel(ConsoleViewID)
}
