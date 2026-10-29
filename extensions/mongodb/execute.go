package mongodb

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// run runs the selection, or the statement under the cursor, or with all the whole playground, of the active editor.
func (q *playgrounds) run(all bool) {
	ad, ok := q.api.Documents().Active()
	if !ok || ad.LanguageID != LanguageID {
		q.api.Notifications().Info("Open a MongoDB playground to run it.")
		return
	}
	text, err := q.api.Documents().Read(context.Background(), ad.URI)
	if err != nil {
		q.api.Log().Warn("the text of the editor cannot be read", "uri", ad.URI, "err", err)
		q.api.Notifications().Error("Cannot read the editor: " + err.Error())
		return
	}
	if q.bindingOf(ad.URI).Connection == "" {
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

// runText runs part of the text of a playground: the selection if there is one, else the statement at the cursor, or all of it.
func (q *playgrounds) runText(uri, text string, sel *extension.Selection, all bool) {
	b := q.bindingOf(uri)
	c, ok := q.connectionOf(b.Connection)
	if !ok {
		q.setBinding(uri, binding{})
		q.updateStatus()
		q.api.Notifications().Error("The connection of this playground was deleted. Choose another one.")
		return
	}
	if !q.isOpen(c.ID) {
		q.api.Notifications().Error("\"" + c.Name + "\" is not connected. Connect to it in the Databases view, then run the playground again.")
		return
	}
	script := playground.Parse(text)
	var items []playground.Item
	switch {
	case sel != nil && sel.Anchor != sel.Active:
		a := offsetOf(text, sel.Anchor.Line, sel.Anchor.Col)
		e := offsetOf(text, sel.Active.Line, sel.Active.Col)
		if a > e {
			a, e = e, a
		}
		items = script.Between(a, e)
	case all || sel == nil:
		items = script.Items
	default:
		at := offsetOf(text, sel.Active.Line, sel.Active.Col)
		if it, found := script.At(at); found {
			items = []playground.Item{it}
		} else if it, found := nextItem(script, at); found {
			items = []playground.Item{it}
		}
	}
	if len(items) == 0 {
		q.api.Notifications().Info("There is nothing to run: the playground has no statement here.")
		return
	}
	q.begin(uri, q.nameOf(uri), c, b, text, script, items)
}

// nextItem is the statement Run runs for a cursor that is on no statement (on a comment or an empty line): the next one after it, or
// the last one when the cursor is after them all, as the query editors of SQL choose.
func nextItem(script *playground.Script, offset int) (playground.Item, bool) {
	if len(script.Items) == 0 {
		return playground.Item{}, false
	}
	for _, it := range script.Items {
		if it.Span.Start.Offset >= offset {
			return it, true
		}
	}
	return script.Items[len(script.Items)-1], true
}

// startDatabase is the database the first of the items runs on: the last "use" before it in the playground, else the one the editor
// is bound to, else the one of the connection.
func startDatabase(script *playground.Script, first playground.Item, b binding, c dbconn.Connection) string {
	db := databaseOf(b, c)
	for _, it := range script.Items {
		if it.Span.End.Offset > first.Span.Start.Offset {
			break
		}
		if u, ok := it.Statement.(*playground.Use); ok {
			db = u.Database
		}
	}
	return db
}

// begin checks the statements against the connection's safety rules and, if they pass, runs them.
func (q *playgrounds) begin(key, name string, c dbconn.Connection, b binding, text string, script *playground.Script, items []playground.Item) {
	runItems := make([]RunItem, len(items))
	var bad []*stepRecord
	for i, it := range items {
		runItems[i] = RunItem{Text: strings.TrimSpace(text[it.Span.Start.Offset:it.Span.End.Offset]), Span: it.Span, Statement: it.Statement}
		if it.Err != nil {
			res := &StepResult{Item: runItems[i], Err: runErrorOf(it.Err)}
			st := &stepRecord{res: res}
			if sp, ok := playground.Place(it.Err); ok {
				st.line, st.column = lineColumn(text, sp.Start.Offset)
				st.endLine, st.endColumn = lineColumn(text, sp.End.Offset)
			} else {
				st.line, st.column = lineColumn(text, it.Span.Start.Offset)
				st.endLine, st.endColumn = lineColumn(text, it.Span.End.Offset)
			}
			bad = append(bad, st)
		}
	}
	start := startDatabase(script, items[0], b, c)
	if len(bad) > 0 {
		q.refuseWith(key, name, c, start, text, len(items), bad, "Nothing was run: the playground has "+plural(len(bad), "mistake")+".")
		return
	}
	var why []string
	db := start
	for i, ri := range runItems {
		step, err := Translate(ri.Statement, db)
		if err != nil {
			continue // the run reports it
		}
		if step.Call == nil {
			db = step.Use
			continue
		}
		s := Classify(*step.Call)
		if c.ReadOnly && s.Writes() {
			q.api.Notifications().Error("\"" + c.Name + "\" is read-only: statement " + strconv.Itoa(i+1) + " (" + s.Verb + ") does not only read. Nothing was run.")
			return
		}
		if s.Class == ClassDestructive {
			why = append(why, s.Why)
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
		pick := []extension.QuickPickItem{
			{Label: "$(play) Run", Description: "on " + c.Name},
			{Label: "$(close) Cancel", Description: "nothing is run"},
		}
		err := q.api.QuickPick().Show(extension.QuickPickOptions{
			Placeholder: strings.Join(uniq, ", ") + " can lose data. Run " + plural(len(runItems), "statement") + " on \"" + c.Name + "\"?", Items: pick,
		}, func(i int) {
			if i == 0 {
				q.execute(key, name, c, start, text, runItems, true)
			}
		})
		if err != nil {
			q.api.Log().Warn("the confirmation cannot be shown", "err", err)
			q.api.Notifications().Error("Cannot ask for the confirmation: " + err.Error() + ". Nothing was run.")
		}
		return
	}
	q.execute(key, name, c, start, text, runItems, true)
}

// refuseWith shows a run that did not start: the mistakes of the playground, with their places.
func (q *playgrounds) refuseWith(key, name string, c dbconn.Connection, db, text string, total int, steps []*stepRecord, why string) {
	ed := q.editor(key, name)
	ed.nextRun++
	if ed.rec != nil {
		ed.rec.release()
	}
	rec := &runRecord{n: ed.nextRun, started: time.Now(), connID: c.ID, connection: c.Name, database: db, text: text, total: total, steps: steps, refused: why, skipped: total}
	ed.rec = rec
	q.clearMarkers(ed)
	q.shown = ed
	rec.tab = 0
	q.reveal()
	q.markErrors(ed, rec)
	q.refresh()
	q.api.Notifications().Error(why)
}

// execute starts the run on a goroutine and gives the panel the record that fills as the statements end. markers says the errors are
// put in the editor.
func (q *playgrounds) execute(key, name string, c dbconn.Connection, db, text string, items []RunItem, markers bool) {
	ed := q.editor(key, name)
	if ed.busy() {
		q.api.Notifications().Warn("A statement is running in this editor. Wait for it or cancel it first.")
		return
	}
	backend, ok := q.mgr.backend(c.ID)
	if !ok {
		q.api.Notifications().Error("\"" + c.Name + "\" is not connected.")
		return
	}
	ed.nextRun++
	rec := &runRecord{
		n: ed.nextRun, started: time.Now(), connID: c.ID, connection: c.Name, database: db, text: text, total: len(items), running: true, current: 1,
	}
	if ed.rec != nil {
		ed.rec.release()
	}
	ed.rec, ed.running = rec, rec
	if markers {
		q.clearMarkers(ed)
	}
	q.shown = ed
	q.reveal()
	q.refresh()
	ctx, cancel := context.WithCancel(context.Background())
	ed.cancel = cancel
	q.trackCancel(ed, cancel)
	opts := RunOptions{
		Database: db, PageSize: q.pageSize(),
		Started: func(i int, _ RunItem) {
			q.api.Post(func() {
				rec.current = i + 1
				q.refresh()
			})
		},
		Finished: func(res *StepResult) { q.api.Post(func() { q.finished(ed, rec, c, res) }) },
	}
	go func() {
		results := Run(ctx, backend, items, opts)
		q.api.Post(func() { q.ended(ed, rec, c, results, markers) })
	}()
}

// finished takes the result of one statement into the run.
func (q *playgrounds) finished(ed *editor, rec *runRecord, c dbconn.Connection, res *StepResult) {
	step := &stepRecord{res: res}
	step.line, step.column = lineColumn(rec.text, res.Item.Span.Start.Offset)
	step.endLine, step.endColumn = lineColumn(rec.text, res.Item.Span.End.Offset)
	rec.steps = append(rec.steps, step)
	for _, s := range res.Sets {
		s.OnDrop = q.untrackCursor
		if s.Cursor != nil {
			q.trackCursor(s.Cursor)
		}
		rec.sets = append(rec.sets, &setRecord{step: len(rec.steps) - 1, index: len(rec.sets), set: s, selected: -1})
	}
	if res.Err != nil {
		rec.tab = len(rec.sets) // the Messages, where the error is
	}
	if err := q.api.Events().Publish(CommandFinishedTopic, map[string]any{
		"connection": c.ID, "ok": res.Err == nil, "millis": res.Elapsed.Milliseconds(),
	}); err != nil {
		q.api.Log().Warn("an event of a statement cannot be published", "err", err)
	}
	q.refresh()
}

// ended closes the record of a run.
func (q *playgrounds) ended(ed *editor, rec *runRecord, c dbconn.Connection, results []*StepResult, markers bool) {
	rec.running = false
	rec.elapsed = time.Since(rec.started)
	if ed.running == rec {
		ed.running = nil
		if ed.cancel != nil {
			ed.cancel()
		}
		ed.cancel = nil
		q.untrackCancel(ed)
	}
	rec.skipped = rec.total - len(results)
	for _, r := range results {
		if r.Cancelled {
			rec.cancelled = true
		}
	}
	switch {
	case rec.failed():
		rec.tab = len(rec.sets)
		if markers {
			q.markErrors(ed, rec)
		}
	case len(rec.sets) > 0:
		rec.tab = 0
	default:
		rec.tab = len(rec.sets)
	}
	q.refresh()
	q.updateStatus()
}

// markErrors puts the error of the run in the editor: a squiggle and an entry in the Problems panel for a file; the cursor, for an
// untitled document, which the Problems panel cannot hold.
func (q *playgrounds) markErrors(ed *editor, rec *runRecord) {
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
			Source: "MongoDB", Message: st.res.Err.Text(),
			StartLine: st.line, StartColumn: st.column, EndLine: st.endLine, EndColumn: max(st.endColumn, st.column+1),
		})
	}
	if first == nil || strings.HasPrefix(ed.key, "collection:") {
		return
	}
	if strings.HasPrefix(ed.key, "file://") {
		if err := q.api.Problems().Set(ProblemsOwner, ed.key, markers); err != nil {
			q.api.Log().Warn("the markers of an error cannot be set", "err", err)
		}
		return
	}
	if err := q.api.Documents().Show(ed.key, first.line, first.column, false); err != nil {
		q.api.Log().Debug("the cursor cannot be moved to the error", "err", err)
	}
}

// clearMarkers takes the markers of the earlier run away.
func (q *playgrounds) clearMarkers(ed *editor) {
	if !strings.HasPrefix(ed.key, "file://") {
		return
	}
	if err := q.api.Problems().Set(ProblemsOwner, ed.key, nil); err != nil {
		q.api.Log().Debug("markers cannot be cleared", "err", err)
	}
}

// cancel stops the statement that runs in the active editor, else in the editor whose results show.
func (q *playgrounds) cancel() {
	ed := q.eds[q.active]
	if ed == nil || ed.running == nil {
		ed = q.shown
	}
	if ed == nil || ed.running == nil || ed.cancel == nil {
		q.api.Notifications().Info("No statement is running.")
		return
	}
	ed.cancel()
}

// fetchMore reads the next page of a set.
func (q *playgrounds) fetchMore(ed *editor, rec *runRecord, sr *setRecord) {
	set := sr.set
	if !set.More() || rec.fetching {
		return
	}
	cur := set.Cursor
	size := max(set.PageSize, 1)
	rec.fetching = true
	q.refresh()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		docs, done, err := cur.Next(ctx, size)
		q.api.Post(func() {
			rec.fetching = false
			if err != nil {
				q.api.Log().Warn("more documents cannot be fetched", "err", err)
				q.api.Notifications().Error("Cannot fetch more documents: " + err.Error())
			}
			if set.Cursor == cur { // the set was not released while the page was read
				set.Append(docs, done || err != nil)
			}
			q.syncViews(sr)
			q.refresh()
		})
	}()
}

// openCollectionCommand runs mongodb.openCollection.
func (q *playgrounds) openCollectionCommand(args any) {
	m, ok := args.(map[string]any)
	if !ok {
		q.api.Notifications().Info("Open a collection from the Databases view.")
		return
	}
	id, _ := m["connection"].(string)
	database, _ := m["database"].(string)
	collection, _ := m["collection"].(string)
	c, found := q.connectionOf(id)
	if !found || database == "" || collection == "" {
		q.api.Notifications().Info("Open a collection from the Databases view.")
		return
	}
	q.openCollection(c, database, collection)
}

// openCollection shows the first documents of a collection in the results.
func (q *playgrounds) openCollection(c dbconn.Connection, database, collection string) {
	if !q.isOpen(c.ID) {
		q.api.Notifications().Error("\"" + c.Name + "\" is not connected.")
		return
	}
	text := collectionRef(collection) + ".find({})"
	script := playground.Parse(text)
	if len(script.Items) != 1 || script.Items[0].Err != nil {
		q.api.Log().Warn("the statement to open a collection cannot be read", "collection", collection)
		q.api.Notifications().Error("Cannot open the collection " + collection + ".")
		return
	}
	it := script.Items[0]
	key := "collection:" + c.ID + "/" + database + "/" + collection
	q.execute(key, database+"."+collection, c, database, text, []RunItem{{Text: text, Span: it.Span, Statement: it.Statement}}, false)
}

// ---- what a tab keeps for the goroutine that ends it ----

// trackCancel remembers the way to stop the run of an editor.
func (q *playgrounds) trackCancel(ed *editor, cancel context.CancelFunc) {
	q.liveMu.Lock()
	defer q.liveMu.Unlock()
	q.cancels[ed] = cancel
}

// untrackCancel forgets it.
func (q *playgrounds) untrackCancel(ed *editor) {
	q.liveMu.Lock()
	defer q.liveMu.Unlock()
	delete(q.cancels, ed)
}

// trackCursor remembers an open cursor, so that deactivating the extension can close it.
func (q *playgrounds) trackCursor(c Cursor) {
	q.liveMu.Lock()
	defer q.liveMu.Unlock()
	q.cursors[c] = struct{}{}
}

// untrackCursor forgets a cursor that ran dry or was closed.
func (q *playgrounds) untrackCursor(c Cursor) {
	q.liveMu.Lock()
	defer q.liveMu.Unlock()
	delete(q.cursors, c)
}

// stop ends what the tab started: the runs are cancelled and the cursors on the servers are closed. It runs on the goroutine that
// notices the end of the activation, so it reaches only what the tab keeps under the lock.
func (q *playgrounds) stop() {
	q.liveMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(q.cancels))
	for _, c := range q.cancels {
		cancels = append(cancels, c)
	}
	cursors := make([]Cursor, 0, len(q.cursors))
	for c := range q.cursors {
		cursors = append(cursors, c)
	}
	clear(q.cancels)
	clear(q.cursors)
	q.liveMu.Unlock()
	for _, c := range cancels {
		c()
	}
	for _, c := range cursors {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := c.Close(ctx); err != nil && !errors.Is(err, context.Canceled) {
			q.api.Log().Debug("a cursor did not close", "err", err)
		}
		cancel()
	}
}
