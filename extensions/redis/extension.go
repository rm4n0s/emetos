package redis

import (
	"context"
	"embed"
	"io/fs"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The ids of the extension (SPEC §10.23).
const (
	// ID is the id of the extension.
	ID = "emetos.redis"
	// KeyKindID is the editor kind of the key editor, and NewKeyKindID the one of the New Key form.
	KeyKindID    = ID + ".key"
	NewKeyKindID = ID + ".newKey"
	// SettingScanCount is the number of keys a page of the explorer asks SCAN for.
	SettingScanCount = "redis.scanCount"
)

// opTimeout bounds one operation of the key editor and of the explorer's actions.
const opTimeout = 30 * time.Second

//go:embed all:assets
var assets embed.FS

// Factory makes the Factory of the extension over the hub that the database extensions share (extensions.All makes one hub and gives
// it to each).
func Factory(h *dbconn.Hub) extension.Factory {
	return func() extension.Extension { return &Extension{hub: h, dialer: GoDialer{}} }
}

// Extension is "Redis" in one workspace: its driver in the Databases view, the key editor tabs and the New Key form.
type Extension struct {
	hub    *dbconn.Hub
	dialer Dialer

	api extension.API
	ctx context.Context
	drv *Driver

	// keyTabs and newKeyTabs are the tabs this instance made, which are on the UI thread only; the tests look at them.
	keyTabs    []*keyTab
	newKeyTabs []*newKeyTab

	// The console, Pub/Sub, Monitor and the server information (step 21.2). They belong to the UI thread; cmds is guarded for the
	// goroutine of the completion provider.
	consoles map[string]*console
	pubsubs  map[string]*pubsubTab
	monitors map[string]*monitorTab
	infoTabs []*infoTab
	// target is the connection the tabs of the panel show; fileConn the connection chosen for each .redis file.
	target   string
	fileConn map[string]string
	// zone is the time zone of the times in the tabs; nil is the desktop's. A test sets it.
	zone  *time.Location
	cmds  *commandIndex
	views struct{ panel extension.ViewHandle }
	// panelTab is the page of the Redis tab that shows: ConsoleViewID (also when empty), PubSubViewID or MonitorViewID
	panelTab string
	status   extension.StatusHandle

	// closers are the connections of Pub/Sub and Monitor that are open, closed when the activation ends.
	closeMu sync.Mutex
	closers map[closer]struct{}
}

// Manifest implements extension.Extension.
func (*Extension) Manifest() extension.Manifest {
	minCount, maxCount := 10.0, 10000.0
	langs, err := fs.Sub(assets, "assets")
	if err != nil {
		langs = nil // cannot happen: "assets" is the embedded folder; the pack is then left out
	}
	const whenFile = "editorTextFocus && editorLangId == redis"
	return extension.Manifest{
		ID: ID, Name: "Redis", Version: "0.2.0",
		Description: "Browse the keys of Redis, Valkey and compatible servers, edit their values, run commands, watch Pub/Sub and MONITOR, and read the server information.",
		ActivationEvents: []string{
			// onStartup: Connect to Database on a container of Redis or Valkey (SPEC §10.24) is an event that no view or command of this
			// extension starts, and an extension that is not active would miss it.
			extension.OnStartup,
			extension.OnView(dbconn.ViewID), extension.OnCommand(dbconn.NewConnection),
			extension.OnEditor(KeyKindID), extension.OnEditor(NewKeyKindID), extension.OnEditor(InfoKindID),
			extension.OnLanguage(RedisLanguageID),
		},
		Events: extension.EventDecl{
			Publishes:  []extension.EventSpec{{Topic: TopicConnected}, {Topic: TopicDisconnected}, {Topic: TopicCommandFinished}},
			Subscribes: append([]string{extension.TopicEditorActiveChanged}, dbconn.ContainerTopics()...),
		},
		Contributes: extension.Contributes{
			Languages: langs,
			Settings: []extension.SettingSpec{{
				Key: SettingScanCount, Type: "integer", Default: float64(DefaultScanCount), Minimum: &minCount, Maximum: &maxCount,
				Description: "The number of keys the Redis explorer asks for in each page (the COUNT of SCAN, and the keys one page shows). The explorer never uses KEYS.",
				Category:    "Redis",
			}, {
				Key: SettingConfirmDangerous, Type: "boolean", Default: true, Category: "Redis",
				Description: "Ask before the console runs FLUSHDB, FLUSHALL, SHUTDOWN, CONFIG SET, DEBUG, KEYS, SCRIPT FLUSH, CLIENT KILL or DEL of several keys, and before MONITOR starts.",
			}},
			EditorKinds: []extension.EditorKindSpec{
				{ID: KeyKindID, Name: "Redis Key", Icon: "key"},
				{ID: NewKeyKindID, Name: "New Redis Key", Icon: "add"},
				{ID: InfoKindID, Name: "Redis Server Information", Icon: "pulse"},
			},
			PanelViews: []extension.PanelViewSpec{
				{ID: PanelViewID, Title: "Redis", Icon: "database", Order: 60},
			},
			EditorActions: []extension.EditorActionSpec{
				{Command: CmdRun, Icon: "play", Tooltip: "Run the Selected Commands", When: "resourceLangId == redis", Order: 1},
				{Command: CmdRunAll, Icon: "run-all", Tooltip: "Run All Commands", When: "resourceLangId == redis", Order: 2},
			},
			Commands: []extension.CommandSpec{
				{ID: CmdOpenConsole, Title: "Open Console", Category: "Redis", Description: "Shows the Redis Console of a connection."},
				{ID: CmdClearConsole, Title: "Clear Console", Category: "Redis"},
				{ID: CmdCancel, Title: "Cancel the Running Command", Category: "Redis"},
				{ID: CmdRun, Title: "Run the Selected Commands", Category: "Redis", When: "editorLangId == redis", Description: "Runs the selected lines, or the line of the cursor, of a .redis file in the console."},
				{ID: CmdRunAll, Title: "Run All Commands", Category: "Redis", When: "editorLangId == redis", Description: "Runs every command of a .redis file in the console."},
				{ID: CmdSelectConn, Title: "Select Connection for This File", Category: "Redis", When: "editorLangId == redis"},
				{ID: CmdOpenPubSub, Title: "Open Pub/Sub", Category: "Redis"},
				{ID: CmdSubscribe, Title: "Subscribe to a Channel", Category: "Redis"},
				{ID: CmdPSubscribe, Title: "Subscribe to a Pattern", Category: "Redis"},
				{ID: CmdPublish, Title: "Publish a Message", Category: "Redis"},
				{ID: CmdOpenMonitor, Title: "Open Monitor", Category: "Redis"},
				{ID: CmdStartMonitor, Title: "Start Monitor", Category: "Redis"},
				{ID: CmdStopMonitor, Title: "Stop Monitor", Category: "Redis"},
				{ID: CmdOpenKey, Title: "Open Key...", Category: "Redis", Description: "Opens a key of the open connection in the key editor. With the arguments {connection, db, key} it opens that key."},
				{ID: CmdServerInfo, Title: "Show Server Information", Category: "Redis", Description: "Opens INFO, the slow log, the clients and the memory of a server."},
				{ID: CmdCopyLastReply, Title: "Copy the Last Reply of the Console", Category: "Redis"},
			},
			Menus: []extension.MenuItem{
				{Menu: extension.MenuEditorContext, Command: CmdRun, Title: "Redis: Run the Selected Commands", Group: "Redis group 1", Order: 1, When: whenFile},
				{Menu: extension.MenuEditorContext, Command: CmdRunAll, Title: "Redis: Run All Commands", Group: "Redis group 1", Order: 2, When: whenFile},
			},
			Keybindings: []extension.Keybinding{
				{Command: CmdRun, Key: "ctrl+enter", When: whenFile},
				{Command: CmdRunAll, Key: "ctrl+shift+enter", When: whenFile},
			},
		},
	}
}

// Activate implements extension.Extension.
func (x *Extension) Activate(ctx context.Context, api extension.API) error {
	x.api, x.ctx = api, ctx
	x.drv = newDriver(x, x.dialer)
	x.consoles, x.pubsubs, x.monitors = map[string]*console{}, map[string]*pubsubTab{}, map[string]*monitorTab{}
	x.fileConn, x.cmds, x.closers = map[string]string{}, newCommandIndex(), map[closer]struct{}{}
	if err := api.Editors().Provide(KeyKindID, extension.EditorKind{Create: x.createKeyTab}); err != nil {
		return err
	}
	if err := api.Editors().Provide(NewKeyKindID, extension.EditorKind{Create: x.createNewKeyTab}); err != nil {
		return err
	}
	if err := api.Editors().Provide(InfoKindID, extension.EditorKind{Create: x.createInfoTab}); err != nil {
		return err
	}
	if err := x.activateTabs(); err != nil {
		return err
	}
	// Connect to Database on a container of Redis or Valkey (SPEC §10.24)
	if err := dbconn.HandleContainerDatabase(ctx, api, EngineID); err != nil {
		return err
	}
	unregister := x.hub.RegisterDriver(x.drv)
	go func() {
		<-ctx.Done()
		unregister()
		x.closeAll()
	}()
	return nil
}

// closer is a Pub/Sub or Monitor connection.
type closer interface{ Close() }

// closeAll closes the Pub/Sub and Monitor connections that are open. It may be called from any goroutine.
func (x *Extension) closeAll() {
	x.closeMu.Lock()
	cs := make([]closer, 0, len(x.closers))
	for c := range x.closers {
		cs = append(cs, c)
	}
	x.closers = map[closer]struct{}{}
	x.closeMu.Unlock()
	for _, c := range cs {
		c.Close()
	}
}

// track remembers a connection to close it when the activation ends; untrack forgets it.
func (x *Extension) track(c closer) {
	x.closeMu.Lock()
	x.closers[c] = struct{}{}
	x.closeMu.Unlock()
}

func (x *Extension) untrack(c closer) {
	x.closeMu.Lock()
	delete(x.closers, c)
	x.closeMu.Unlock()
}

// Driver is the driver the extension registered, for the tests.
func (x *Extension) Driver() *Driver { return x.drv }

// scanCount is the setting redis.scanCount.
func (x *Extension) scanCount() int64 {
	n, err := extension.SettingValue[int](x.api.Settings(), SettingScanCount)
	if err != nil || n < 10 {
		return DefaultScanCount
	}
	return int64(min(n, 10000))
}

// KeyRef names a key of a connection: where the key editor and the actions of the explorer find it.
type KeyRef struct {
	Conn string
	DB   int
	Key  string
}

// keyState is the state of a key editor tab. A key is binary-safe, JSON text is not, so a key that is not UTF-8 is kept as bytes.
type keyState struct {
	Conn   string `json:"conn"`
	DB     int    `json:"db"`
	Key    string `json:"key,omitempty"`
	KeyRaw []byte `json:"keyRaw,omitempty"`
}

func stateOf(r KeyRef) keyState {
	if utf8.ValidString(r.Key) {
		return keyState{Conn: r.Conn, DB: r.DB, Key: r.Key}
	}
	return keyState{Conn: r.Conn, DB: r.DB, KeyRaw: []byte(r.Key)}
}

func (s keyState) ref() KeyRef {
	if s.KeyRaw != nil {
		return KeyRef{Conn: s.Conn, DB: s.DB, Key: string(s.KeyRaw)}
	}
	return KeyRef{Conn: s.Conn, DB: s.DB, Key: s.Key}
}

// openKey opens the key editor on a key, or shows the tab it is open in.
func (x *Extension) openKey(ref KeyRef) {
	for _, t := range x.keyTabs {
		if t.ref == ref {
			t.tab.Reveal(true)
			return
		}
	}
	tab, err := x.api.Editors().Open(KeyKindID, stateOf(ref), extension.EditorOpenOptions{})
	if err != nil {
		x.api.Log().Warn("the key editor cannot be opened", "err", err)
		x.api.Notifications().Error("Cannot open the key: " + err.Error())
		return
	}
	tab.Reveal(true)
}

// copyText puts text on the clipboard.
func (x *Extension) copyText(text string) {
	if err := x.api.Clipboard().WriteText(text); err != nil {
		x.api.Log().Warn("the clipboard did not take the text", "err", err)
		x.api.Notifications().Warn("Cannot copy to the clipboard: " + err.Error())
	}
}

// dbOp runs op on a goroutine with the database of a connection and a timeout, then done (or the notification of the error) on the UI
// thread.
func (x *Extension) dbOp(conn string, index int, what string, op func(ctx context.Context, db DB) error, done func()) {
	go func() {
		ctx, cancel := context.WithTimeout(x.ctx, opTimeout)
		defer cancel()
		db, _, err := x.drv.database(conn, index)
		if err == nil {
			err = op(ctx, db)
		}
		x.api.Post(func() {
			if err != nil {
				x.api.Log().Warn("a Redis operation failed", "operation", what, "err", err)
				x.api.Notifications().Error(what + " failed: " + err.Error())
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}

// state returns the explorer's state of a database, or nil.
func (x *Extension) state(conn string, index int) *dbState {
	s, err := x.drv.session(conn)
	if err != nil {
		return nil
	}
	st, err := s.state(index)
	if err != nil {
		return nil
	}
	return st
}

// loadMore scans the next page of keys of a database and lists the rows again.
func (x *Extension) loadMore(conn string, index int) {
	st := x.state(conn, index)
	if st == nil {
		return
	}
	x.dbOp(conn, index, "Loading the keys", func(ctx context.Context, _ DB) error { return st.loadMore(ctx, x.scanCount()) },
		func() { x.hub.Reload(conn) })
}

// hasFilter tells whether the database has a filter.
func (x *Extension) hasFilter(conn string, index int) bool {
	st := x.state(conn, index)
	if st == nil {
		return false
	}
	return st.filterText() != ""
}

// askFilter asks for the pattern of the keys to show.
func (x *Extension) askFilter(conn string, index int) {
	st := x.state(conn, index)
	if st == nil {
		return
	}
	current := st.filterText()
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Show the keys that contain this text, or match this pattern (* ? [abc])", Value: current,
	}, func(value string, ok bool) {
		if ok {
			x.setFilter(conn, index, value)
		}
	})
	if err != nil {
		x.api.Log().Warn("the filter cannot be asked", "err", err)
	}
}

// setFilter changes the filter of a database and lists its keys again.
func (x *Extension) setFilter(conn string, index int, filter string) {
	if st := x.state(conn, index); st != nil {
		st.setFilter(filter)
		x.hub.Reload(conn)
	}
}

// refreshDB forgets the keys of a database and lists them again.
func (x *Extension) refreshDB(conn string, index int) {
	if st := x.state(conn, index); st != nil {
		st.refresh()
		x.hub.Reload(conn)
	}
}

// ---- key actions, from the explorer and from the key editor ----

// renameKey asks for the new name and renames the key.
func (x *Extension) renameKey(ref KeyRef) {
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "New name of the key", Value: ref.Key,
		Validate: func(v string) string {
			switch v {
			case "":
				return "A key needs a name."
			case ref.Key:
				return "That is the name it has."
			}
			return ""
		},
	}, func(value string, ok bool) {
		if ok {
			x.doRename(ref, value)
		}
	})
	if err != nil {
		x.api.Log().Warn("the new name cannot be asked", "err", err)
	}
}

// doRename renames a key if the new name is free, and follows it in the tree and in the tabs.
func (x *Extension) doRename(ref KeyRef, newKey string) {
	x.dbOp(ref.Conn, ref.DB, "Renaming the key", func(ctx context.Context, db DB) error {
		renamed, err := db.Rename(ctx, ref.Key, newKey)
		if err != nil {
			return err
		}
		if !renamed {
			return errors.New("RedisKeyExists", "a key with this name exists")
		}
		if st := x.state(ref.Conn, ref.DB); st != nil {
			st.forgetKey(ref.Key)
			return st.addKey(ctx, newKey)
		}
		return nil
	}, func() {
		for _, t := range x.keyTabs {
			if t.ref == ref {
				t.renamed(newKey)
			}
		}
		x.hub.Reload(ref.Conn)
	})
}

// setTTL asks for a time to live in seconds and gives it to the key.
func (x *Extension) setTTL(ref KeyRef) {
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Time to live in seconds",
		Validate: func(v string) string {
			if _, err := ParseTTLSeconds(v); err != nil {
				return err.Error()
			}
			return ""
		},
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		ttl, err := ParseTTLSeconds(value)
		if err != nil {
			return
		}
		x.dbOp(ref.Conn, ref.DB, "Setting the TTL", func(ctx context.Context, db DB) error { return db.Expire(ctx, ref.Key, ttl) },
			func() { x.keyChanged(ref) })
	})
	if err != nil {
		x.api.Log().Warn("the TTL cannot be asked", "err", err)
	}
}

// ParseTTLSeconds reads a time to live written in seconds: a whole number above zero.
func ParseTTLSeconds(s string) (time.Duration, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("RedisTTLInvalid", "a time to live is a whole number of seconds above zero", "ttl", s)
	}
	if n > int64(time.Duration(1<<62)/time.Second) {
		return 0, errors.New("RedisTTLInvalid", "that time to live is too long", "ttl", s)
	}
	return time.Duration(n) * time.Second, nil
}

// persistKey removes the time to live of a key.
func (x *Extension) persistKey(ref KeyRef) {
	x.dbOp(ref.Conn, ref.DB, "Removing the TTL", func(ctx context.Context, db DB) error {
		_, err := db.Persist(ctx, ref.Key)
		return err
	}, func() { x.keyChanged(ref) })
}

// keyChanged tells the tabs of a key that its TTL changed, and refreshes the row of the explorer.
func (x *Extension) keyChanged(ref KeyRef) {
	for _, t := range x.keyTabs {
		if t.ref == ref {
			t.reload()
		}
	}
	go func() {
		st := x.state(ref.Conn, ref.DB)
		if st == nil {
			return
		}
		ctx, cancel := context.WithTimeout(x.ctx, opTimeout)
		defer cancel()
		if err := st.addKey(ctx, ref.Key); err != nil {
			x.api.Log().Warn("the row of a key cannot be refreshed", "err", err)
			return
		}
		x.api.Post(func() { x.hub.Reload(ref.Conn) })
	}()
}

// deleteKey asks first, then deletes the key.
func (x *Extension) deleteKey(ref KeyRef) {
	x.api.Notifications().Warn("Delete the key \""+ref.Key+"\"? This cannot be undone.",
		extension.NotificationAction{Label: "Delete", Run: func() { x.doDelete(ref) }})
}

// doDelete deletes a key and closes its tabs.
func (x *Extension) doDelete(ref KeyRef) {
	x.dbOp(ref.Conn, ref.DB, "Deleting the key", func(ctx context.Context, db DB) error {
		if _, err := db.Del(ctx, ref.Key); err != nil {
			return err
		}
		if st := x.state(ref.Conn, ref.DB); st != nil {
			st.forgetKey(ref.Key)
		}
		return nil
	}, func() {
		for _, t := range append([]*keyTab(nil), x.keyTabs...) {
			if t.ref == ref {
				t.tab.Close()
			}
		}
		x.hub.Reload(ref.Conn)
	})
}

// newKey opens the New Key form for a database; prefix is the folder it was asked in.
func (x *Extension) newKey(conn string, index int, prefix string) {
	tab, err := x.api.Editors().Open(NewKeyKindID, newKeyState{Conn: conn, DB: index, Prefix: prefix}, extension.EditorOpenOptions{})
	if err != nil {
		x.api.Log().Warn("the New Key form cannot be opened", "err", err)
		x.api.Notifications().Error("Cannot open the form: " + err.Error())
		return
	}
	tab.Reveal(true)
}

// keyCreated lists the rows again and opens the key that was made.
func (x *Extension) keyCreated(ref KeyRef) {
	x.hub.Reload(ref.Conn)
	x.openKey(ref)
}
