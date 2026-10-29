package mongodb

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The ids of the commands, the view, the settings, the context keys and the events of the playgrounds (SPEC §10.22 "Playgrounds",
// "Results", "Editing a document", "Safety").
const (
	// RunCommand runs the selection or the statement under the cursor of the active playground (Ctrl+Enter) and RunAllCommand every
	// statement of it (Ctrl+Shift+Enter).
	RunCommand    = "mongodb.run"
	RunAllCommand = "mongodb.runAll"
	// CancelCommand stops the statement that runs.
	CancelCommand = "mongodb.cancel"
	// SelectConnectionCommand binds the active playground to a connection and a database.
	SelectConnectionCommand = "mongodb.selectConnection"
	// OpenCollectionCommand shows the first documents of a collection in the results; its argument is {"connection": id,
	// "database": name, "collection": name}.
	OpenCollectionCommand = "mongodb.openCollection"
	// EditDocumentCommand opens the selected document of the results as Extended JSON, DeleteDocumentCommand deletes it after asking,
	// and SaveDocumentCommand saves the document of the active editor to the server (Ctrl+S in a document that is being edited).
	EditDocumentCommand   = "mongodb.editDocument"
	DeleteDocumentCommand = "mongodb.deleteDocument"
	SaveDocumentCommand   = "mongodb.saveDocument"
	// CopyDocumentCommand copies the selected document, or the documents of the shown set, as Extended JSON.
	CopyDocumentCommand = "mongodb.copyDocument"
	// ViewID is the panel tab "MongoDB Results".
	ViewID = ID + ".results"
	// PageSizeSetting is how many documents a cursor gives each time; ConfirmDestructiveSetting asks before drop, dropDatabase,
	// deleteMany({}) and updateMany({}, ...).
	PageSizeSetting           = "mongodb.pageSize"
	ConfirmDestructiveSetting = "mongodb.confirmDestructive"
	// RunningKey is true while the playground whose results show runs a statement; EditingKey while the active editor is a document
	// that is being edited.
	RunningKey = ID + ".running"
	EditingKey = ID + ".editingDocument"
	// CommandFinishedTopic is published when a statement ends: {"connection": id, "ok": bool, "millis": n}. It has no text of the
	// statement and no value.
	CommandFinishedTopic = ID + ".commandFinished"
	// ProblemsOwner is the owner of the markers of the errors of statements.
	ProblemsOwner = "mongodb"

	defaultPageSize = 20
)

// binding is the connection and database a playground is for.
type binding struct {
	Connection string `json:"connection"`
	Database   string `json:"database,omitempty"`
}

// editor is the state of one playground of the window tab, or of one collection opened from the explorer. It is used on the UI thread.
type editor struct {
	// key is the URI of the document, or "collection:..." for a collection opened from the explorer.
	key  string
	name string
	// rec is the last run, and running the run in progress, if any.
	rec     *runRecord
	running *runRecord
	// cancel ends the run in progress.
	cancel  context.CancelFunc
	nextRun int
}

// busy tells whether a statement runs.
func (ed *editor) busy() bool { return ed.running != nil }

// viewMode is how the documents of a result are shown.
type viewMode string

// The three views of a result (SPEC §10.22 "Results").
const (
	// ModeTree shows expandable fields with their BSON types, ModeJSON the documents as Extended JSON and ModeTable the top-level
	// fields as columns.
	ModeTree  viewMode = "tree"
	ModeJSON  viewMode = "json"
	ModeTable viewMode = "table"
)

// playgrounds is the playgrounds of one window tab, their panel, their status bar item and the documents being edited.
type playgrounds struct {
	e   *Extension
	api extension.API
	hub *dbconn.Hub
	mgr *manager

	// the following are used on the UI thread
	eds        map[string]*editor
	active     string
	activeLang string
	shown      *editor
	view       extension.ViewHandle
	focus      *widgets.FocusNode
	connItem   extension.StatusHandle
	mode       viewMode
	menu       *ui.ContextMenuController
	edits      map[string]*docEdit

	// bindMu guards binds.
	bindMu sync.RWMutex
	binds  map[string]binding

	// liveMu guards cancels and cursors, which the goroutine that ends the activation reaches.
	liveMu  sync.Mutex
	cancels map[*editor]context.CancelFunc
	cursors map[Cursor]struct{}
}

// newPlaygrounds makes the playgrounds of a window tab.
func newPlaygrounds(e *Extension, api extension.API) *playgrounds {
	return &playgrounds{
		e: e, api: api, hub: e.hub, mgr: e.mgr, eds: map[string]*editor{}, binds: map[string]binding{}, edits: map[string]*docEdit{},
		mode: ModeTree, menu: ui.NewContextMenuController(), cancels: map[*editor]context.CancelFunc{}, cursors: map[Cursor]struct{}{},
	}
}

// bindingOf returns the binding of a document; the zero binding when it has none.
func (q *playgrounds) bindingOf(uri string) binding {
	q.bindMu.RLock()
	defer q.bindMu.RUnlock()
	return q.binds[uri]
}

// setBinding records the binding of a document.
func (q *playgrounds) setBinding(uri string, b binding) {
	q.bindMu.Lock()
	defer q.bindMu.Unlock()
	if b.Connection == "" {
		delete(q.binds, uri)
		return
	}
	q.binds[uri] = b
}

// editor returns the state of the editor with the key, making it when there is none.
func (q *playgrounds) editor(key, name string) *editor {
	ed := q.eds[key]
	if ed == nil {
		ed = &editor{key: key, name: name}
		q.eds[key] = ed
	}
	return ed
}

// pageSize is the setting mongodb.pageSize.
func (q *playgrounds) pageSize() int {
	n, err := extension.SettingValue[int](q.api.Settings(), PageSizeSetting)
	if err != nil || n <= 0 {
		return defaultPageSize
	}
	return n
}

// confirmDestructive is the setting mongodb.confirmDestructive.
func (q *playgrounds) confirmDestructive() bool {
	on, err := extension.SettingValue[bool](q.api.Settings(), ConfirmDestructiveSetting)
	return err != nil || on
}

// start follows the active editor and the documents, and gives the panel its content and the status bar its item. It runs in Activate.
func (q *playgrounds) start() error {
	api := q.api
	handle, err := api.Views().Provide(ViewID, extension.ViewContent{Build: q.build, SaveState: q.saveState, RestoreState: q.restoreState})
	if err != nil {
		return err
	}
	q.view = handle
	if q.connItem, err = api.StatusBar().Add(extension.StatusItem{
		ID: "mongodb.connection", Name: "MongoDB Connection", Alignment: extension.StatusRight, Priority: 100, Command: SelectConnectionCommand,
	}); err != nil {
		return err
	}
	q.connItem.SetVisible(false)
	handlers := map[string]func(args any){
		RunCommand:              func(any) { q.run(false) },
		RunAllCommand:           func(any) { q.run(true) },
		CancelCommand:           func(any) { q.cancel() },
		SelectConnectionCommand: func(any) { q.chooseForActive() },
		OpenCollectionCommand:   q.openCollectionCommand,
		EditDocumentCommand:     func(any) { q.editSelected() },
		DeleteDocumentCommand:   func(any) { q.deleteSelected() },
		SaveDocumentCommand:     func(any) { q.saveDocument() },
		CopyDocumentCommand:     func(any) { q.copySelected() },
	}
	for id, fn := range handlers {
		if err := api.Commands().Handle(id, func(_ context.Context, args any) error { fn(args); return nil }); err != nil {
			return err
		}
	}
	for topic, fn := range map[string]func(extension.Event){
		extension.TopicEditorActiveChanged: q.onActive,
		extension.TopicDocumentClosed:      q.onClosed,
		extension.TopicDocumentSaved:       q.onSaved,
	} {
		if _, err := api.Events().Subscribe(topic, fn); err != nil {
			return err
		}
	}
	if ad, ok := api.Documents().Active(); ok {
		q.active, q.activeLang = ad.URI, ad.LanguageID
	}
	q.updateStatus()
	return nil
}

// ---- state saved with the workspace ----

// savedState is what the panel keeps for the next session: the bindings of the files of the workspace and the view of the results.
type savedState struct {
	Files map[string]binding `json:"files,omitempty"`
	Mode  viewMode           `json:"mode,omitempty"`
}

// saveState keeps the bindings of the documents that are files (an untitled document has no next session to be bound in).
func (q *playgrounds) saveState() (any, error) {
	q.bindMu.RLock()
	defer q.bindMu.RUnlock()
	st := savedState{Files: map[string]binding{}, Mode: q.mode}
	for uri, b := range q.binds {
		if strings.HasPrefix(uri, "file://") {
			st.Files[uri] = b
		}
	}
	return st, nil
}

// restoreState gives the files their bindings back.
func (q *playgrounds) restoreState(raw json.RawMessage) error {
	var st savedState
	if err := json.Unmarshal(raw, &st); err != nil {
		return errors.NewErr("MongoStateInvalid", err)
	}
	if st.Mode == ModeTree || st.Mode == ModeJSON || st.Mode == ModeTable {
		q.mode = st.Mode
	}
	q.bindMu.Lock()
	defer q.bindMu.Unlock()
	for uri, b := range st.Files {
		if _, taken := q.binds[uri]; !taken && b.Connection != "" {
			q.binds[uri] = b
		}
	}
	return nil
}

// ---- events ----

// onActive follows the editor that is in front.
func (q *playgrounds) onActive(ev extension.Event) {
	ae, err := extension.EventValue[extension.ActiveEditor](ev)
	if err != nil {
		q.api.Log().Warn("an active editor event cannot be read", "err", err)
		return
	}
	q.active, q.activeLang = ae.URI, ae.LanguageID
	if ed := q.eds[ae.URI]; ed != nil && ed.rec != nil {
		q.show(ed)
	}
	q.updateStatus()
	q.setKey(EditingKey, q.edits[ae.URI] != nil)
}

// onClosed lets go of an editor that was closed.
func (q *playgrounds) onClosed(ev extension.Event) {
	dc, err := extension.EventValue[extension.DocumentClosed](ev)
	if err != nil {
		return
	}
	if ed := q.eds[dc.URI]; ed != nil {
		q.drop(ed)
	}
	delete(q.edits, dc.URI)
	if !strings.HasPrefix(dc.URI, "file://") {
		q.setBinding(dc.URI, binding{})
	}
	if dc.URI == q.active {
		q.setKey(EditingKey, false)
	}
}

// onSaved moves an editor to the URI a Save As gave its document.
func (q *playgrounds) onSaved(ev extension.Event) {
	ds, err := extension.EventValue[extension.DocumentSaved](ev)
	if err != nil || !ds.As || ds.PreviousURI == "" || ds.PreviousURI == ds.URI {
		return
	}
	if b := q.bindingOf(ds.PreviousURI); b.Connection != "" {
		q.setBinding(ds.URI, b)
		q.setBinding(ds.PreviousURI, binding{})
	}
	if ed := q.eds[ds.PreviousURI]; ed != nil {
		delete(q.eds, ds.PreviousURI)
		ed.key = ds.URI
		ed.name = q.nameOf(ds.URI)
		q.eds[ds.URI] = ed
	}
	if q.active == ds.PreviousURI {
		q.active = ds.URI
	}
}

// connectionChanged is told that a connection was opened or closed: the results of a closed one cannot fetch more.
func (q *playgrounds) connectionChanged() {
	q.updateStatus()
	q.refresh()
}

// drop lets go of an editor: its markers, its results and its cursors.
func (q *playgrounds) drop(ed *editor) {
	if ed.cancel != nil {
		ed.cancel()
	}
	if ed.rec != nil {
		ed.rec.release()
	}
	ed.rec, ed.running = nil, nil
	if strings.HasPrefix(ed.key, "file://") {
		if err := q.api.Problems().Set(ProblemsOwner, ed.key, nil); err != nil {
			q.api.Log().Debug("markers cannot be cleared", "err", err)
		}
	}
	delete(q.eds, ed.key)
	if q.shown == ed {
		q.shown = nil
		q.refresh()
	}
	q.updateStatus()
}

// nameOf is the name of a document for the user: the name of its file, or Untitled-3.
func (q *playgrounds) nameOf(uri string) string {
	switch {
	case strings.HasPrefix(uri, "file://"):
		return filepath.Base(strings.TrimPrefix(uri, "file://"))
	case strings.HasPrefix(uri, "untitled:"):
		rest := strings.TrimPrefix(uri, "untitled:")
		if strings.HasPrefix(rest, "Untitled-") {
			return rest
		}
		return "Untitled-" + rest
	}
	return uri
}

// ---- connections and bindings ----

// connectionOf finds the connection of a binding among the saved ones.
func (q *playgrounds) connectionOf(id string) (dbconn.Connection, bool) {
	if id == "" {
		return dbconn.Connection{}, false
	}
	return q.hub.Store().Get(id)
}

// openConnections lists the MongoDB connections of the workspace that are open.
func (q *playgrounds) openConnections() []dbconn.Connection {
	var open []dbconn.Connection
	for _, c := range q.hub.Store().List(q.api.Scope().Folder) {
		if c.Engine != EngineID {
			continue
		}
		if _, ok := q.mgr.backend(c.ID); ok {
			open = append(open, c)
		}
	}
	return open
}

// isOpen tells whether the connection has a server.
func (q *playgrounds) isOpen(id string) bool {
	_, ok := q.mgr.backend(id)
	return ok
}

// databaseOf is the database a playground with the binding starts on: the one it was bound to, else the connection's.
func databaseOf(b binding, c dbconn.Connection) string {
	if b.Database != "" {
		return b.Database
	}
	return c.Database
}

// describeConnection is the short text after the name of a connection in a list.
func describeConnection(c dbconn.Connection) string {
	if c.Host != "" {
		return c.Host
	}
	return c.Engine
}

// bindEditor binds a document to a connection and database.
func (q *playgrounds) bindEditor(uri string, b binding) {
	q.setBinding(uri, b)
	q.updateStatus()
}

// chooseForActive asks which connection and database the active playground is for.
func (q *playgrounds) chooseForActive() {
	if q.activeLang != LanguageID || q.active == "" {
		q.api.Notifications().Info("Open a MongoDB playground first.")
		return
	}
	uri := q.active
	q.choose(func(b binding) { q.bindEditor(uri, b) })
}

// choose lets the user pick among the open connections and then a database, and calls done with the choice.
func (q *playgrounds) choose(done func(binding)) {
	open := q.openConnections()
	if len(open) == 0 {
		q.api.Notifications().Info("There is no open MongoDB connection. Connect to a server in the Databases view first.")
		return
	}
	items := make([]extension.QuickPickItem, len(open))
	for i, c := range open {
		items[i] = extension.QuickPickItem{Label: "$(database) " + c.Name, Description: describeConnection(c)}
	}
	err := q.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Which connection is this playground for?", Items: items}, func(i int) {
		if i < 0 || i >= len(open) {
			return
		}
		q.chooseDatabase(open[i], done)
	})
	if err != nil {
		q.api.Log().Warn("the list of connections cannot be shown", "err", err)
	}
}

// chooseDatabase asks for the database of a connection.
func (q *playgrounds) chooseDatabase(c dbconn.Connection, done func(binding)) {
	b, ok := q.mgr.backend(c.ID)
	if !ok {
		done(binding{Connection: c.ID})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dbs, err := b.Databases(ctx)
		q.api.Post(func() {
			if err != nil {
				q.api.Log().Warn("the databases cannot be listed", "connection", c.Name, "err", err)
				q.api.Notifications().Error("Cannot list the databases of " + c.Name + ": " + err.Error())
				return
			}
			items := []extension.QuickPickItem{{Label: "$(star) Default database", Description: databaseText(c)}}
			names := []string{""}
			for _, d := range dbs {
				items = append(items, extension.QuickPickItem{Label: "$(database) " + d.Name})
				names = append(names, d.Name)
			}
			perr := q.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Which database of " + c.Name + "?", Items: items}, func(i int) {
				if i >= 0 && i < len(names) {
					done(binding{Connection: c.ID, Database: names[i]})
				}
			})
			if perr != nil {
				q.api.Log().Warn("the list of databases cannot be shown", "err", perr)
			}
		})
	}()
}

// databaseText says which database a binding with no database uses.
func databaseText(c dbconn.Connection) string {
	if c.Database != "" {
		return "the database of the connection: " + c.Database
	}
	return "use the database of the playground, or " + DefaultDatabase
}

// ---- status bar ----

// updateStatus shows the connection of the active playground in the status bar.
func (q *playgrounds) updateStatus() {
	if q.connItem == nil {
		return
	}
	if q.activeLang != LanguageID || q.active == "" {
		q.connItem.SetVisible(false)
		return
	}
	b := q.bindingOf(q.active)
	item := extension.StatusItem{ID: "mongodb.connection", Name: "MongoDB Connection", Alignment: extension.StatusRight, Priority: 100, Command: SelectConnectionCommand}
	c, ok := q.connectionOf(b.Connection)
	switch {
	case b.Connection == "":
		item.Text, item.Tooltip = "$(database) Select Connection", "This playground is not bound to a server. Click to choose one."
	case !ok:
		item.Text, item.Tooltip = "$(warning) Connection deleted", "The connection of this playground was deleted. Click to choose another."
	default:
		item.Text = "$(database) " + c.Name
		if db := databaseOf(b, c); db != "" {
			item.Text += " · " + db
		}
		item.Tooltip = "Connection of this playground: " + c.Name + " (" + describeConnection(c) + ")"
		if c.ReadOnly {
			item.Tooltip += ", read-only"
		}
		item.Tooltip += ". Click to change."
		if !q.isOpen(c.ID) {
			item.Text += " (not connected)"
		}
		item.Color = c.Color
	}
	q.connItem.Update(item)
	q.connItem.SetVisible(true)
}

// setKey sets a context key and logs a failure.
func (q *playgrounds) setKey(key string, v bool) {
	if err := q.api.Context().Set(key, v); err != nil {
		if e, ok := errors.FromError(err); ok && e.Tag == "ExtensionContextUnavailable" {
			return // a scope without context keys (the hosts of some tests) has no keybindings to enable either
		}
		q.api.Log().Warn("a context key cannot be set", "key", key, "err", err)
	}
}

// ---- opening playgrounds ----

// openPlayground opens an untitled playground with the text, bound to the connection and database.
func (q *playgrounds) openPlayground(c dbconn.Connection, database, text string) {
	uri, err := q.api.Documents().OpenUntitled(LanguageID, text)
	if err != nil {
		q.api.Log().Warn("a playground cannot be opened", "err", err)
		q.api.Notifications().Error("Cannot open a playground: " + err.Error())
		return
	}
	q.bindEditor(uri, binding{Connection: c.ID, Database: database})
	q.active, q.activeLang = uri, LanguageID
	q.updateStatus()
}

// refresh builds the panel again.
func (q *playgrounds) refresh() {
	if q.view != nil {
		q.view.Refresh()
	}
	q.setKey(RunningKey, q.shown != nil && q.shown.running != nil)
}

// reveal shows the panel tab without taking the keyboard from the editor.
func (q *playgrounds) reveal() {
	if q.view != nil {
		q.view.Reveal(false)
	}
}

// show makes the results of an editor the ones the panel shows.
func (q *playgrounds) show(ed *editor) {
	if q.shown == ed {
		return
	}
	q.shown = ed
	q.refresh()
}
