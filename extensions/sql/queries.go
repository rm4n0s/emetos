package sql

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// The ids of the commands, the view, the setting names, the context keys and the events of the query editors (SPEC §10.21 "Query
// editors", "The results panel", "Safety").
const (
	// RunQuery runs the selection or the statement under the cursor of the active SQL editor (Ctrl+Enter), and RunAllQueries the
	// whole script (Ctrl+Shift+Enter).
	RunQuery      = ID + ".run"
	RunAllQueries = ID + ".runAll"
	// CancelQuery stops the statement that runs in the editor.
	CancelQuery = ID + ".cancel"
	// BeginTransaction, CommitTransaction and RollbackTransaction control the transaction of the editor's session.
	BeginTransaction    = ID + ".begin"
	CommitTransaction   = ID + ".commit"
	RollbackTransaction = ID + ".rollback"
	// ChooseConnection binds the active SQL editor to a connection and a database.
	ChooseConnection = ID + ".selectConnection"
	// ExportResult writes the result that the panel shows to a file; its argument is "csv", "json" or "sql", or none to ask.
	ExportResult = ID + ".exportResult"
	// ViewID is the panel tab "Query Results".
	ViewID = ID + ".results"
	// SettingMaxRows is the rows a result set reads at first and by each Fetch More; SettingConfirmDestructive asks before DROP,
	// TRUNCATE, ALTER and a DELETE or UPDATE without WHERE.
	SettingMaxRows            = "sql.maxRows"
	SettingConfirmDestructive = "sql.confirmDestructive"
	// RunningKey is true while the editor whose results show is running something; InTransactionKey while the active editor has a
	// transaction open.
	RunningKey       = ID + ".running"
	InTransactionKey = ID + ".inTransaction"
	// TopicQueryFinished is published when a statement ends: {"connection": id, "ok": bool, "rows": n, "millis": n}. It carries no
	// SQL text and no values.
	TopicQueryFinished = ID + ".queryFinished"
	// ProblemsOwner is the owner of the markers of the errors of statements.
	ProblemsOwner = "sql"
)

// binding is the connection and database a query editor is for.
type binding struct {
	Connection string `json:"connection"`
	Database   string `json:"database,omitempty"`
}

// queryEditor is the state of one SQL editor of the window tab. Everything in it is used on the UI thread; the runner has a lock of
// its own.
type queryEditor struct {
	uri    string
	runner *Runner
	// bound is the binding the runner was made for.
	bound binding
	runs  []*runRecord
	shown *runRecord
	// running is the run in progress, if any, and fetching says that Fetch More is reading.
	running  *runRecord
	fetching bool
	nextRun  int
}

// busy tells whether a statement runs or rows are being fetched.
func (ed *queryEditor) busy() bool { return ed.running != nil || ed.fetching }

// queries is the query editors of one window tab, their panel, their status bar items and their completion.
type queries struct {
	hub *Hub
	api extension.API

	// the following are used on the UI thread
	eds        map[string]*queryEditor
	active     string
	activeLang string
	shown      *queryEditor
	view       extension.ViewHandle
	focus      *widgets.FocusNode
	connItem   extension.StatusHandle
	txItem     extension.StatusHandle

	// bindMu guards binds, which the completion provider reads from its own goroutines.
	bindMu sync.RWMutex
	binds  map[string]binding

	// created are the runners this tab made and has not disposed, which stop ends from the goroutine of the deactivation.
	createdMu sync.Mutex
	created   map[*Runner]struct{}
}

// newQueries makes the query editors of a window tab.
func newQueries(h *Hub, api extension.API) *queries {
	return &queries{hub: h, api: api, eds: map[string]*queryEditor{}, binds: map[string]binding{}, created: map[*Runner]struct{}{}}
}

// bindingOf returns the binding of a document; the zero binding when it has none. It may be called from any goroutine.
func (q *queries) bindingOf(uri string) binding {
	q.bindMu.RLock()
	defer q.bindMu.RUnlock()
	return q.binds[uri]
}

// setBinding records the binding of a document.
func (q *queries) setBinding(uri string, b binding) {
	q.bindMu.Lock()
	if b.Connection == "" {
		delete(q.binds, uri)
	} else {
		q.binds[uri] = b
	}
	q.bindMu.Unlock()
}

// editor returns the state of the editor of the document, making it when there is none.
func (q *queries) editor(uri string) *queryEditor {
	ed := q.eds[uri]
	if ed == nil {
		ed = &queryEditor{uri: uri}
		q.eds[uri] = ed
	}
	return ed
}

// start follows the active editor and the documents, and gives the panel its content and the status bar its items. It runs in Activate.
func (q *queries) start() error {
	api := q.api
	handle, err := api.Views().Provide(ViewID, extension.ViewContent{
		Build: q.build, SaveState: q.saveState, RestoreState: q.restoreState,
	})
	if err != nil {
		return err
	}
	q.view = handle
	if q.connItem, err = api.StatusBar().Add(extension.StatusItem{
		ID: "sql.connection", Name: "SQL Connection", Alignment: extension.StatusRight, Priority: 100, Command: ChooseConnection,
	}); err != nil {
		return err
	}
	if q.txItem, err = api.StatusBar().Add(extension.StatusItem{
		ID: "sql.transaction", Name: "SQL Transaction", Alignment: extension.StatusRight, Priority: 101, Command: CommitTransaction,
	}); err != nil {
		return err
	}
	q.connItem.SetVisible(false)
	q.txItem.SetVisible(false)
	handlers := map[string]func(args any){
		RunQuery:            func(any) { q.run(false) },
		RunAllQueries:       func(any) { q.run(true) },
		CancelQuery:         func(any) { q.cancel() },
		BeginTransaction:    func(any) { q.transaction("begin") },
		CommitTransaction:   func(any) { q.transaction("commit") },
		RollbackTransaction: func(any) { q.transaction("rollback") },
		ChooseConnection:    func(any) { q.chooseForActive() },
		ExportResult:        q.export,
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
	if _, err := api.Languages().RegisterCompletionProvider(extension.DocumentSelector{Languages: []string{"sql"}}, &completer{q: q}, "."); err != nil {
		return err
	}
	if ad, ok := api.Documents().Active(); ok {
		q.active, q.activeLang = ad.URI, ad.LanguageID
	}
	q.updateStatus()
	return nil
}

// stop ends every session of the tab: an open transaction is rolled back, a running statement is cancelled. It runs on the goroutine
// that notices the end of the activation, so it reaches only what the tab made under a lock.
func (q *queries) stop() {
	q.createdMu.Lock()
	runners := make([]*Runner, 0, len(q.created))
	for r := range q.created {
		runners = append(runners, r)
	}
	clear(q.created)
	q.createdMu.Unlock()
	for _, r := range runners {
		if _, err := r.Dispose(); err != nil {
			q.api.Log().Debug("a session did not close cleanly", "err", err)
		}
	}
}

// dispose ends a runner in the background and forgets it; done is called on the UI thread with the outcome.
func (q *queries) dispose(r *Runner, done func(rolledBack bool, err error)) {
	q.createdMu.Lock()
	delete(q.created, r)
	q.createdMu.Unlock()
	go func() {
		rolledBack, err := r.Dispose()
		if done != nil {
			q.api.Post(func() { done(rolledBack, err) })
		}
	}()
}

// ---- state saved with the workspace ----

// savedState is what the panel keeps for the next session: the bindings of the files of the workspace.
type savedState struct {
	Files map[string]binding `json:"files,omitempty"`
}

// saveState keeps the bindings of the documents that are files (an untitled document has no next session to be bound in).
func (q *queries) saveState() (any, error) {
	q.bindMu.RLock()
	defer q.bindMu.RUnlock()
	st := savedState{Files: map[string]binding{}}
	for uri, b := range q.binds {
		if strings.HasPrefix(uri, "file://") {
			st.Files[uri] = b
		}
	}
	if len(st.Files) == 0 {
		return nil, nil
	}
	return st, nil
}

// restoreState gives the files their bindings back.
func (q *queries) restoreState(raw json.RawMessage) error {
	var st savedState
	if err := json.Unmarshal(raw, &st); err != nil {
		return err
	}
	q.bindMu.Lock()
	for uri, b := range st.Files {
		if _, taken := q.binds[uri]; !taken && b.Connection != "" {
			q.binds[uri] = b
		}
	}
	q.bindMu.Unlock()
	return nil
}

// ---- events ----

// onActive follows the editor that is in front.
func (q *queries) onActive(ev extension.Event) {
	ae, err := extension.EventValue[extension.ActiveEditor](ev)
	if err != nil {
		q.api.Log().Warn("an active editor event cannot be read", "err", err)
		return
	}
	q.active, q.activeLang = ae.URI, ae.LanguageID
	if ed := q.eds[ae.URI]; ed != nil && ae.LanguageID == "sql" && len(ed.runs) > 0 {
		q.show(ed)
	}
	q.updateStatus()
}

// onClosed lets go of an editor that was closed.
func (q *queries) onClosed(ev extension.Event) {
	dc, err := extension.EventValue[extension.DocumentClosed](ev)
	if err != nil {
		return
	}
	if ed := q.eds[dc.URI]; ed != nil {
		q.drop(ed, true)
	}
	if !strings.HasPrefix(dc.URI, "file://") {
		q.setBinding(dc.URI, binding{})
	}
}

// onSaved moves an editor to the URI a Save As gave its document.
func (q *queries) onSaved(ev extension.Event) {
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
		ed.uri = ds.URI
		q.eds[ds.URI] = ed
	}
	if q.active == ds.PreviousURI {
		q.active = ds.URI
	}
}

// connectionChanged is told by the service that a connection was opened or closed: the sessions on a closed one are gone.
func (q *queries) connectionChanged(id string) {
	if !q.hub.svc.IsOpen(id) {
		for _, ed := range q.eds {
			if ed.bound.Connection == id && ed.runner != nil {
				r := ed.runner
				ed.runner = nil
				q.dispose(r, func(_ bool, err error) {
					if err != nil {
						q.api.Log().Debug("a session on a closed connection did not close cleanly", "err", err)
					}
				})
			}
		}
	}
	q.updateStatus()
}

// drop lets go of an editor: its session, its markers and its results.
func (q *queries) drop(ed *queryEditor, closed bool) {
	if ed.runner != nil {
		r := ed.runner
		ed.runner = nil
		name := q.nameOf(ed.uri)
		q.dispose(r, func(rolledBack bool, err error) {
			switch {
			case err != nil:
				q.api.Log().Warn("a session did not close cleanly", "editor", name, "err", err)
			case rolledBack:
				q.api.Notifications().Info("The transaction of " + name + " was rolled back, as its editor was closed.")
			}
		})
	}
	for _, run := range ed.runs {
		run.release()
	}
	ed.runs, ed.shown, ed.running = nil, nil, nil
	if strings.HasPrefix(ed.uri, "file://") {
		if err := q.api.Problems().Set(ProblemsOwner, ed.uri, nil); err != nil {
			q.api.Log().Debug("markers cannot be cleared", "err", err)
		}
	}
	if closed {
		delete(q.eds, ed.uri)
	}
	if q.shown == ed {
		q.shown = nil
		q.refresh()
	}
	q.updateStatus()
}

// nameOf is the name of a document for the user: the name of its file, or Untitled-3.
func (q *queries) nameOf(uri string) string {
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
func (q *queries) connectionOf(id string) (dbconn.Connection, bool) {
	if id == "" {
		return dbconn.Connection{}, false
	}
	return q.hub.db.Store().Get(id)
}

// openConnections lists the SQL connections of the workspace that are open.
func (q *queries) openConnections() []dbconn.Connection {
	var open []dbconn.Connection
	for _, c := range q.hub.db.Store().List(q.api.Scope().Folder) {
		if _, drv, ok := q.hub.db.Engine(c.Engine); ok && drv == dbconn.Driver(q.hub.drv) && q.hub.svc.IsOpen(c.ID) {
			open = append(open, c)
		}
	}
	return open
}

// bindEditor binds a document to a connection and database, and makes it a runner when it has none for them.
func (q *queries) bindEditor(uri string, b binding) *queryEditor {
	ed := q.editor(uri)
	q.setBinding(uri, b)
	if ed.runner != nil && ed.bound != b {
		r := ed.runner
		ed.runner = nil
		q.dispose(r, func(_ bool, err error) {
			if err != nil {
				q.api.Log().Warn("a session did not close when its editor was bound again", "err", err)
			}
		})
	}
	q.updateStatus()
	return ed
}

// runnerOf returns the runner of the editor for its binding, making it if needed.
func (q *queries) runnerOf(ed *queryEditor) (*Runner, error) {
	b := q.bindingOf(ed.uri)
	if ed.runner != nil && ed.bound == b {
		return ed.runner, nil
	}
	r, err := q.hub.svc.NewRunner(b.Connection, b.Database)
	if err != nil {
		return nil, err
	}
	ed.runner, ed.bound = r, b
	q.createdMu.Lock()
	q.created[r] = struct{}{}
	q.createdMu.Unlock()
	return r, nil
}

// chooseForActive asks which connection and database the active SQL editor is for.
func (q *queries) chooseForActive() {
	if q.activeLang != "sql" || q.active == "" {
		q.api.Notifications().Info("Open a SQL editor first.")
		return
	}
	uri := q.active
	q.choose(func(b binding) { q.bindEditor(uri, b) })
}

// choose lets the user pick among the open connections and then a database, and calls done with the choice.
func (q *queries) choose(done func(binding)) {
	open := q.openConnections()
	if len(open) == 0 {
		q.api.Notifications().Info("There is no open SQL connection. Connect to a database in the Databases view first.")
		return
	}
	items := make([]extension.QuickPickItem, len(open))
	for i, c := range open {
		items[i] = extension.QuickPickItem{Label: "$(database) " + c.Name, Description: describeConnection(c)}
	}
	err := q.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Which connection is this editor for?", Items: items}, func(i int) {
		if i < 0 || i >= len(open) {
			return
		}
		q.chooseDatabase(open[i], done)
	})
	if err != nil {
		q.api.Log().Warn("the list of connections cannot be shown", "err", err)
	}
}

// chooseDatabase asks for the database of a connection whose engine has several; the others bind at once.
func (q *queries) chooseDatabase(c dbconn.Connection, done func(binding)) {
	d, ok := dialect.ByID(c.Engine)
	if !ok || !d.Levels().Databases {
		done(binding{Connection: c.ID})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), generateTimeout)
		defer cancel()
		rows, _, err := q.hub.svc.Rows(ctx, c.ID, "", d.Databases(showSystem(c)), 0)
		q.api.Post(func() {
			if err != nil {
				q.api.Log().Warn("the databases cannot be listed", "connection", c.Name, "err", err)
				q.api.Notifications().Error("Cannot list the databases of " + c.Name + ": " + err.Error())
				return
			}
			items := []extension.QuickPickItem{{Label: "$(star) Default database", Description: "the database of the connection"}}
			names := []string{""}
			for _, r := range rows {
				items = append(items, extension.QuickPickItem{Label: "$(database) " + cell(r, 0)})
				names = append(names, cell(r, 0))
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

// ---- status bar ----

// updateStatus shows the connection of the active SQL editor in the status bar, and the transaction when it has one open.
func (q *queries) updateStatus() {
	if q.connItem == nil {
		return
	}
	if q.activeLang != "sql" || q.active == "" {
		q.connItem.SetVisible(false)
		q.txItem.SetVisible(false)
		q.setKey(InTransactionKey, false)
		return
	}
	b := q.bindingOf(q.active)
	item := extension.StatusItem{ID: "sql.connection", Name: "SQL Connection", Alignment: extension.StatusRight, Priority: 100, Command: ChooseConnection}
	c, ok := q.connectionOf(b.Connection)
	switch {
	case b.Connection == "":
		item.Text, item.Tooltip = "$(database) Select Connection", "This editor is not bound to a database. Click to choose one."
	case !ok:
		item.Text, item.Tooltip = "$(warning) Connection deleted", "The connection of this editor was deleted. Click to choose another."
	default:
		item.Text = "$(database) " + c.Name
		if b.Database != "" {
			item.Text += " · " + b.Database
		}
		item.Tooltip = "Connection of this editor: " + c.Name + " (" + describeConnection(c) + ")"
		if c.ReadOnly {
			item.Tooltip += ", read-only"
		}
		item.Tooltip += ". Click to change."
		if !q.hub.svc.IsOpen(c.ID) {
			item.Text += " (not connected)"
		}
		item.Color = c.Color
	}
	q.connItem.Update(item)
	q.connItem.SetVisible(true)
	inTx := false
	if ed := q.eds[q.active]; ed != nil && ed.runner != nil {
		inTx = ed.runner.InTransaction()
	}
	if inTx {
		q.txItem.Update(extension.StatusItem{
			ID: "sql.transaction", Name: "SQL Transaction", Alignment: extension.StatusRight, Priority: 101,
			Text: "$(circle-filled) Transaction open", Tooltip: "A transaction is open in this editor. Click to commit it.", Command: CommitTransaction,
		})
	}
	q.txItem.SetVisible(inTx)
	q.setKey(InTransactionKey, inTx)
}

// setKey sets a context key and logs a failure.
func (q *queries) setKey(key string, v bool) {
	if err := q.api.Context().Set(key, v); err != nil {
		if e, ok := errors.FromError(err); ok && e.Tag == "ExtensionContextUnavailable" {
			return // a scope without context keys (the hosts of some tests) has no keybindings to enable either
		}
		q.api.Log().Warn("a context key cannot be set", "key", key, "err", err)
	}
}

// ---- running ----

// maxRows is the setting sql.maxRows.
func (q *queries) maxRows() int {
	n, err := extension.SettingValue[int](q.api.Settings(), SettingMaxRows)
	if err != nil || n <= 0 {
		return DefaultMaxRows
	}
	return n
}

// confirmDestructive is the setting sql.confirmDestructive.
func (q *queries) confirmDestructive() bool {
	on, err := extension.SettingValue[bool](q.api.Settings(), SettingConfirmDestructive)
	return err != nil || on
}

// run runs the selection, or the statement under the cursor, or with all the whole script, of the active SQL editor.
func (q *queries) run(all bool) {
	ad, ok := q.api.Documents().Active()
	if !ok || ad.LanguageID != "sql" {
		q.api.Notifications().Info("Open a SQL editor to run a query.")
		return
	}
	text, err := q.api.Documents().Read(context.Background(), ad.URI)
	if err != nil {
		q.api.Log().Warn("the text of the editor cannot be read", "uri", ad.URI, "err", err)
		q.api.Notifications().Error("Cannot read the editor: " + err.Error())
		return
	}
	b := q.bindingOf(ad.URI)
	if b.Connection == "" {
		q.choose(func(chosen binding) {
			q.bindEditor(ad.URI, chosen)
			q.run(all)
		})
		return
	}
	var sel *extension.Selection
	if len(ad.Selections) > 0 {
		sel = &ad.Selections[0]
	}
	q.runText(ad.URI, text, sel, all)
}

// runText runs part of the text of a document: the selection if there is one, else the statement at the cursor, or all of it.
func (q *queries) runText(uri, text string, sel *extension.Selection, all bool) {
	b := q.bindingOf(uri)
	c, ok := q.connectionOf(b.Connection)
	if !ok {
		q.setBinding(uri, binding{})
		q.updateStatus()
		q.api.Notifications().Error("The connection of this editor was deleted. Choose another one.")
		return
	}
	d, ok := dialect.ByID(c.Engine)
	if !ok {
		q.api.Notifications().Error("This connection's engine is not one the SQL extension runs.")
		return
	}
	if !q.hub.svc.IsOpen(c.ID) {
		q.api.Notifications().Error("\"" + c.Name + "\" is not connected. Connect to it in the Databases view, then run the query again.")
		return
	}
	script, base := text, 0
	var stmts []dialect.Statement
	switch {
	case sel != nil && sel.Anchor != sel.Active:
		a := offsetOf(text, sel.Anchor.Line, sel.Anchor.Col)
		e := offsetOf(text, sel.Active.Line, sel.Active.Col)
		if a > e {
			a, e = e, a
		}
		script, base = text[a:e], a
		stmts = dialect.Split(d, script)
	case all || sel == nil:
		stmts = dialect.Split(d, script)
	default:
		stmts = dialect.Split(d, script)
		if st, found := dialect.At(script, stmts, offsetOf(text, sel.Active.Line, sel.Active.Col)); found {
			stmts = []dialect.Statement{st}
		}
	}
	if len(stmts) == 0 {
		q.api.Notifications().Info("There is nothing to run: the editor has no statement here.")
		return
	}
	q.begin(uri, c, d, b, text, base, stmts)
}

// begin checks the statements against the connection's safety rules and, if they pass, runs them.
func (q *queries) begin(uri string, c dbconn.Connection, d dialect.Dialect, b binding, text string, base int, stmts []dialect.Statement) {
	var why []string
	for i, st := range stmts {
		info := dialect.Classify(d, st.Text)
		if c.ReadOnly && !info.Allowed() {
			q.api.Notifications().Error("\"" + c.Name + "\" is read-only: statement " + strconv.Itoa(i+1) + " (" + info.Verb + ") does not only read. Nothing was run.")
			return
		}
		if info.Destructive {
			why = append(why, info.Why)
		}
	}
	if len(why) > 0 && q.confirmDestructive() {
		seen := map[string]bool{}
		var uniq []string
		for _, w := range why {
			if !seen[w] {
				seen[w] = true
				uniq = append(uniq, w)
			}
		}
		items := []extension.QuickPickItem{
			{Label: "$(play) Run", Description: "on " + c.Name},
			{Label: "$(close) Cancel", Description: "nothing is run"},
		}
		err := q.api.QuickPick().Show(extension.QuickPickOptions{
			Placeholder: strings.Join(uniq, ", ") + " can lose data. Run " + plural(len(stmts), "statement") + " on \"" + c.Name + "\"?", Items: items,
		}, func(i int) {
			if i == 0 {
				q.execute(uri, c, d, b, text, base, stmts)
			}
		})
		if err != nil {
			q.api.Log().Warn("the confirmation cannot be shown", "err", err)
			q.api.Notifications().Error("Cannot ask for the confirmation: " + err.Error() + ". Nothing was run.")
		}
		return
	}
	q.execute(uri, c, d, b, text, base, stmts)
}

// execute starts the run on a goroutine and gives the panel the record that fills as the statements end.
func (q *queries) execute(uri string, c dbconn.Connection, d dialect.Dialect, b binding, text string, base int, stmts []dialect.Statement) {
	ed := q.editor(uri)
	if ed.busy() {
		q.api.Notifications().Warn("A statement is running in this editor. Wait for it or cancel it first.")
		return
	}
	runner, err := q.runnerOf(ed)
	if err != nil {
		q.api.Notifications().Error("Cannot run the query: " + err.Error())
		return
	}
	ed.nextRun++
	rec := &runRecord{
		n: ed.nextRun, started: time.Now(), connection: c.Name, database: b.Database, engine: d.ID(),
		text: text, base: base, total: len(stmts), running: true, current: 1,
	}
	ed.runs = append(ed.runs, rec)
	q.trim(ed)
	ed.shown, ed.running = rec, rec
	q.clearMarkers(ed)
	q.shown = ed
	q.reveal()
	q.updateStatus()
	q.refresh()
	opts := RunOptions{
		MaxRows: q.maxRows(),
		Started: func(i int, _ dialect.Statement) {
			q.api.Post(func() {
				rec.current = i + 1
				q.refresh()
			})
		},
		Finished: func(res *StatementResult) {
			q.api.Post(func() { q.finished(ed, rec, c, res) })
		},
	}
	go func() {
		results, err := runner.Run(context.Background(), stmts, opts)
		q.api.Post(func() { q.ended(ed, rec, c, d, results, len(stmts), err) })
	}()
}

// trim drops the oldest runs of an editor beyond the ten it keeps, and the rows of old runs beyond the memory it may hold.
func (q *queries) trim(ed *queryEditor) {
	for len(ed.runs) > historyRuns {
		ed.runs[0].release()
		ed.runs = ed.runs[1:]
	}
	var total int64
	for i := len(ed.runs) - 1; i >= 0; i-- {
		total += ed.runs[i].bytes()
		if total > editorBytes && i < len(ed.runs)-1 {
			ed.runs[i].release()
		}
	}
}

// finished takes the result of one statement into the run.
func (q *queries) finished(ed *queryEditor, rec *runRecord, c dbconn.Connection, res *StatementResult) {
	step := &stepRecord{res: res}
	rec.steps = append(rec.steps, step)
	for _, s := range res.Sets {
		rec.sets = append(rec.sets, &setRecord{step: len(rec.steps) - 1, index: len(rec.sets), set: s})
	}
	if res.Info.Schema && res.Err == nil {
		if cat, err := q.hub.svc.Catalog(c.ID); err == nil {
			cat.Invalidate()
		}
	}
	if res.Err != nil && res.Err.Offset >= 0 {
		abs := rec.base + res.Statement.Start + res.Err.Offset
		step.line, step.column = lineColumn(rec.text, abs)
		step.endLine, step.endColumn = lineColumn(rec.text, abs+res.Err.Length)
	}
	if res.Err != nil {
		rec.tab = len(rec.sets) // the Messages, where the error is
	}
	rows := 0
	for _, s := range res.Sets {
		rows += s.Table.Len()
	}
	if res.HasAffected {
		rows = int(res.Affected)
	}
	if err := q.api.Events().Publish(TopicQueryFinished, map[string]any{
		"connection": c.ID, "ok": res.Err == nil, "rows": rows, "millis": res.Elapsed.Milliseconds(),
	}); err != nil {
		q.api.Log().Warn("an event of a statement cannot be published", "err", err)
	}
	q.refresh()
}

// ended closes the record of a run: it did not fail to start, or it did and says so.
func (q *queries) ended(ed *queryEditor, rec *runRecord, c dbconn.Connection, d dialect.Dialect, results []*StatementResult, total int, err error) {
	rec.running = false
	rec.elapsed = time.Since(rec.started)
	if ed.running == rec {
		ed.running = nil
	}
	if err != nil {
		q.api.Log().Warn("a run could not start", "connection", c.Name, "err", err)
		q.api.Notifications().Error("Cannot run the query: " + err.Error())
		q.refresh()
		q.updateStatus()
		return
	}
	rec.skipped = total - len(results)
	for _, r := range results {
		if r.Cancelled {
			rec.cancelled = true
		}
	}
	if rec.failed() {
		q.markErrors(ed, rec)
	} else if len(rec.sets) > 0 {
		rec.tab = 0
	} else {
		rec.tab = len(rec.sets)
	}
	q.refresh()
	q.updateStatus()
}

// markErrors puts the error of the run in the editor: a squiggle and an entry in the Problems panel for a file; the cursor, for an
// untitled document, which the Problems panel cannot hold.
func (q *queries) markErrors(ed *queryEditor, rec *runRecord) {
	var first *stepRecord
	var markers []extension.Marker
	for _, st := range rec.steps {
		if st.res.Err == nil || st.res.Cancelled || st.line == 0 {
			continue
		}
		if first == nil {
			first = st
		}
		markers = append(markers, extension.Marker{
			Source: "SQL", Message: st.res.Err.Text(),
			StartLine: st.line, StartColumn: st.column, EndLine: st.endLine, EndColumn: max(st.endColumn, st.column+1),
		})
	}
	if first == nil {
		return
	}
	if strings.HasPrefix(ed.uri, "file://") {
		if err := q.api.Problems().Set(ProblemsOwner, ed.uri, markers); err != nil {
			q.api.Log().Warn("the markers of an error cannot be set", "err", err)
		}
		return
	}
	if err := q.api.Documents().Show(ed.uri, first.line, first.column, false); err != nil {
		q.api.Log().Debug("the cursor cannot be moved to the error", "err", err)
	}
}

// clearMarkers takes the markers of the earlier run away.
func (q *queries) clearMarkers(ed *queryEditor) {
	if !strings.HasPrefix(ed.uri, "file://") {
		return
	}
	if err := q.api.Problems().Set(ProblemsOwner, ed.uri, nil); err != nil {
		q.api.Log().Debug("markers cannot be cleared", "err", err)
	}
}

// cancel stops the statement that runs in the active editor, else in the editor whose results show.
func (q *queries) cancel() {
	ed := q.eds[q.active]
	if ed == nil || ed.running == nil {
		ed = q.shown
	}
	if ed == nil || ed.running == nil || ed.runner == nil {
		q.api.Notifications().Info("No statement is running in this editor.")
		return
	}
	r := ed.runner
	go func() {
		if err := r.Cancel(); err != nil {
			q.api.Post(func() {
				q.api.Log().Warn("a statement could not be cancelled", "err", err)
				q.api.Notifications().Error("Cannot cancel the statement: " + err.Error())
			})
		}
	}()
}

// transaction runs Begin, Commit or Rollback on the session of the active editor.
func (q *queries) transaction(what string) {
	if q.activeLang != "sql" || q.active == "" {
		q.api.Notifications().Info("Open a SQL editor first.")
		return
	}
	uri := q.active
	b := q.bindingOf(uri)
	c, ok := q.connectionOf(b.Connection)
	if !ok {
		q.chooseForActive()
		return
	}
	if !q.hub.svc.IsOpen(c.ID) {
		q.api.Notifications().Error("\"" + c.Name + "\" is not connected.")
		return
	}
	if c.ReadOnly && what == "begin" {
		// a transaction on a read-only connection is harmless but of no use; the engine refuses the writes
		q.api.Notifications().Info("\"" + c.Name + "\" is read-only.")
	}
	ed := q.editor(uri)
	if ed.busy() {
		q.api.Notifications().Warn("A statement is running in this editor. Wait for it or cancel it first.")
		return
	}
	runner, err := q.runnerOf(ed)
	if err != nil {
		q.api.Notifications().Error("Cannot run the query: " + err.Error())
		return
	}
	if what == "commit" && !runner.InTransaction() {
		q.api.Notifications().Info("There is no open transaction in this editor.")
		return
	}
	go func() {
		ctx := context.Background()
		var err error
		switch what {
		case "begin":
			err = runner.Begin(ctx)
		case "commit":
			err = runner.Commit(ctx)
		default:
			err = runner.Rollback(ctx)
		}
		q.api.Post(func() {
			if err != nil {
				q.api.Log().Warn("a transaction command failed", "what", what, "err", err)
				q.api.Notifications().Error("Cannot " + what + " the transaction: " + err.Error())
			}
			q.updateStatus()
		})
	}()
}

// ---- opening editors ----

// newQuery is the command New Query: an editor for the connection the argument names, or the one the user picks among the open ones.
func (q *queries) newQuery(args any) {
	id, database := "", ""
	switch a := args.(type) {
	case string:
		id = a
	case map[string]any:
		id, _ = a["connection"].(string)
		database, _ = a["database"].(string)
	}
	if id != "" {
		if c, ok := q.connectionOf(id); ok {
			q.openQuery(c, database, "", false)
			return
		}
	}
	q.choose(func(b binding) {
		if c, ok := q.connectionOf(b.Connection); ok {
			q.openQuery(c, b.Database, "", false)
		}
	})
}

// openQuery opens an untitled SQL editor with the text, bound to the connection and database, and runs it when run is set.
func (q *queries) openQuery(c dbconn.Connection, database, text string, run bool) {
	uri, err := q.api.Documents().OpenUntitled("sql", text)
	if err != nil {
		q.api.Log().Warn("an SQL editor cannot be opened", "err", err)
		q.api.Notifications().Error("Cannot open the SQL editor: " + err.Error())
		return
	}
	q.bindEditor(uri, binding{Connection: c.ID, Database: database})
	q.active, q.activeLang = uri, "sql"
	q.updateStatus()
	if run {
		q.runText(uri, text, nil, true)
	}
}

// refresh builds the panel again.
func (q *queries) refresh() {
	if q.view != nil {
		q.view.Refresh()
	}
	running := q.shown != nil && q.shown.running != nil
	q.setKey(RunningKey, running)
}

// reveal shows the panel tab without taking the keyboard from the editor.
func (q *queries) reveal() {
	if q.view != nil {
		q.view.Reveal(false)
	}
}

// show makes the results of an editor the ones the panel shows.
func (q *queries) show(ed *queryEditor) {
	if q.shown == ed {
		return
	}
	q.shown = ed
	q.refresh()
}

// the grid of a result set is made when the panel first shows it.
func (q *queries) gridOf(ed *queryEditor, rec *runRecord, sr *setRecord) *resultgrid.Grid {
	if sr.grid != nil {
		return sr.grid
	}
	sr.grid = resultgrid.New(resultgrid.Options{
		OnCopy: func(text string) {
			if err := q.api.Clipboard().WriteText(text); err != nil {
				q.api.Log().Warn("the clipboard cannot be written", "err", err)
				q.api.Notifications().Error("Cannot copy: " + err.Error())
			}
		},
		OnFetchMore: func() { q.fetchMore(ed, rec, sr) },
	})
	sr.grid.SetSource(sr.set.Table)
	sr.grid.SetMore(sr.set.More())
	return sr.grid
}

// fetchMore reads the next rows of a result set.
func (q *queries) fetchMore(ed *queryEditor, rec *runRecord, sr *setRecord) {
	if ed.runner == nil || !sr.set.More() || ed.busy() {
		return
	}
	runner := ed.runner
	opts := RunOptions{MaxRows: q.maxRows()}
	ed.fetching = true
	q.refresh()
	go func() {
		err := runner.FetchMore(context.Background(), sr.set, opts)
		q.api.Post(func() {
			ed.fetching = false
			if err != nil {
				q.api.Log().Warn("more rows cannot be fetched", "err", err)
				q.api.Notifications().Error("Cannot fetch more rows: " + err.Error())
			}
			if sr.grid != nil {
				sr.grid.Appended()
				sr.grid.SetMore(sr.set.More())
			}
			q.refresh()
		})
	}()
}
