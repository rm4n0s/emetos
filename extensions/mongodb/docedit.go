package mongodb

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// docEdit is a document that is open in an editor as Extended JSON, to be saved to the server with replaceOne (SPEC §10.22 "Editing a
// document"). The text is an untitled document, so it is never written to a file of Emetos.
type docEdit struct {
	connID, connection   string
	database, collection string
	// id is the _id of the document, and original the document as it was stored when it was opened or last saved.
	id       any
	original bson.D
}

// where names the collection of the document for a message: shop.orders.
func (e *docEdit) where() string { return e.database + "." + e.collection }

// shownSet returns the set whose tab is open in the panel.
func (q *playgrounds) shownSet() (*editor, *runRecord, *setRecord) {
	if q.shown == nil || q.shown.rec == nil {
		return nil, nil, nil
	}
	return q.shown, q.shown.rec, q.currentSet(q.shown.rec)
}

// selectedIndex is the index of the document the user selected in the set: the row of the table, or the node or line that was
// clicked; the only document of a set that has one.
func (q *playgrounds) selectedIndex(sr *setRecord) int {
	if q.mode == ModeTable && sr.table != nil {
		if i := sr.table.selectedDoc(); i >= 0 && i < len(sr.set.Docs) {
			return i
		}
	}
	if sr.selected >= 0 && sr.selected < len(sr.set.Docs) {
		return sr.selected
	}
	if len(sr.set.Docs) == 1 {
		return 0
	}
	return -1
}

// documentMenu is the context menu of a document of the results.
func (q *playgrounds) documentMenu(sr *setRecord, idx int) []ui.MenuItem {
	return q.documentMenuAt(sr, func() int { return idx })
}

// documentMenuAt is the context menu of the document that pick returns when the menu is made.
func (q *playgrounds) documentMenuAt(sr *setRecord, pick func() int) []ui.MenuItem {
	idx := pick()
	if idx < 0 || idx >= len(sr.set.Docs) {
		return nil
	}
	var items []ui.MenuItem
	if _, rec, _ := q.shownSet(); rec != nil && sr.set.Editable && !q.readOnly(rec) {
		items = append(items,
			ui.MenuItem{Label: "Edit Document", OnRun: func() { q.editDoc(sr, idx) }},
			ui.MenuItem{Label: "Delete Document", OnRun: func() { q.deleteDoc(sr, idx) }})
	}
	return append(items, ui.MenuItem{Label: "Copy Document", OnRun: func() { q.copyDoc(sr, idx) }})
}

// editSelected opens the selected document of the shown set in an editor.
func (q *playgrounds) editSelected() {
	_, _, sr := q.shownSet()
	if sr == nil {
		q.api.Notifications().Info("Run a statement that returns documents first.")
		return
	}
	idx := q.selectedIndex(sr)
	if idx < 0 {
		q.api.Notifications().Info("Select a document first.")
		return
	}
	q.editDoc(sr, idx)
}

// deleteSelected deletes the selected document of the shown set after asking.
func (q *playgrounds) deleteSelected() {
	_, _, sr := q.shownSet()
	if sr == nil {
		q.api.Notifications().Info("Run a statement that returns documents first.")
		return
	}
	idx := q.selectedIndex(sr)
	if idx < 0 {
		q.api.Notifications().Info("Select a document first.")
		return
	}
	q.deleteDoc(sr, idx)
}

// copySelected copies the selected document, or all the documents of the set, as Extended JSON.
func (q *playgrounds) copySelected() {
	_, _, sr := q.shownSet()
	if sr == nil {
		q.api.Notifications().Info("Run a statement that returns documents first.")
		return
	}
	q.copyDoc(sr, q.selectedIndex(sr))
}

// copyDoc copies a document, or the whole set when idx is negative.
func (q *playgrounds) copyDoc(sr *setRecord, idx int) {
	var text string
	var err error
	if idx >= 0 && idx < len(sr.set.Docs) {
		text, err = DocumentText(sr.set.Docs[idx])
	} else {
		text, err = DocumentsText(sr.set.Docs)
	}
	if err != nil {
		q.api.Notifications().Error("Cannot write the document: " + err.Error())
		return
	}
	if err := q.api.Clipboard().WriteText(text); err != nil {
		q.api.Log().Warn("the clipboard cannot be written", "err", err)
		q.api.Notifications().Error("Cannot copy: " + err.Error())
	}
}

// editable checks that the document of the set can be edited or deleted: whole documents of a collection of a connection that is
// not read-only. It tells the user why not.
func (q *playgrounds) editable(rec *runRecord, sr *setRecord, idx int) (id any, ok bool) {
	switch {
	case !sr.set.Editable:
		q.api.Notifications().Info("These documents cannot be edited: they come from a projection, an aggregation or a command, not whole from a collection.")
		return nil, false
	case q.readOnly(rec):
		q.api.Notifications().Error("\"" + rec.connection + "\" is read-only.")
		return nil, false
	case !q.isOpen(rec.connID):
		q.api.Notifications().Error("\"" + rec.connection + "\" is not connected.")
		return nil, false
	case idx < 0 || idx >= len(sr.set.Docs):
		return nil, false
	}
	id, has := idOf(sr.set.Docs[idx])
	if !has {
		q.api.Notifications().Error("The document has no _id, so it cannot be replaced or deleted by it.")
		return nil, false
	}
	return id, true
}

// editDoc opens a document of the set as Extended JSON in an untitled editor.
func (q *playgrounds) editDoc(sr *setRecord, idx int) {
	_, rec, _ := q.shownSet()
	if rec == nil {
		return
	}
	id, ok := q.editable(rec, sr, idx)
	if !ok {
		return
	}
	doc := sr.set.Docs[idx]
	text, err := DocumentText(doc)
	if err != nil {
		q.api.Notifications().Error("Cannot write the document: " + err.Error())
		return
	}
	uri, err := q.api.Documents().OpenUntitled("json", text+"\n")
	if err != nil {
		q.api.Log().Warn("a document cannot be opened", "err", err)
		q.api.Notifications().Error("Cannot open the document: " + err.Error())
		return
	}
	e := &docEdit{connID: rec.connID, connection: rec.connection, database: sr.set.Call.Database, collection: sr.set.Call.Collection, id: id, original: doc}
	q.edits[uri] = e
	q.active, q.activeLang = uri, "json"
	q.setKey(EditingKey, true)
	q.api.Notifications().Info("Editing the document " + IDText(id) + " of " + e.where() + ". Press Ctrl+S to replace it on the server.")
}

// saveDocument saves the document of the active editor to the server: it shows the fields that changed and, after the user agrees,
// runs replaceOne({_id}, document).
func (q *playgrounds) saveDocument() {
	ad, ok := q.api.Documents().Active()
	if !ok {
		q.api.Notifications().Info("Open a document with Edit Document first.")
		return
	}
	e := q.edits[ad.URI]
	if e == nil {
		q.api.Notifications().Info("This editor is not a MongoDB document. Open one with Edit Document in the results.")
		return
	}
	text, err := q.api.Documents().Read(context.Background(), ad.URI)
	if err != nil {
		q.api.Log().Warn("the text of the editor cannot be read", "uri", ad.URI, "err", err)
		q.api.Notifications().Error("Cannot read the editor: " + err.Error())
		return
	}
	edited, err := ParseDocument(text)
	if err != nil {
		q.api.Notifications().Error("The text is not a document: " + err.Error())
		return
	}
	id, has := idOf(edited)
	switch {
	case !has:
		q.api.Notifications().Error("The document has no _id. Put back the _id: " + IDText(e.id))
		return
	case valueText(id) != valueText(e.id):
		q.api.Notifications().Error("The _id cannot be changed (it is " + IDText(e.id) + "). Change it back, or insert a new document with a playground.")
		return
	}
	c, found := q.connectionOf(e.connID)
	switch {
	case !found:
		q.api.Notifications().Error("The connection of this document was deleted.")
		return
	case c.ReadOnly:
		q.api.Notifications().Error("\"" + c.Name + "\" is read-only.")
		return
	case !q.isOpen(c.ID):
		q.api.Notifications().Error("\"" + c.Name + "\" is not connected.")
		return
	}
	changes := Diff(e.original, edited)
	if len(changes) == 0 {
		q.api.Notifications().Info("Nothing changed in the document.")
		return
	}
	merged := Merge(e.original, edited)
	items := []extension.QuickPickItem{
		{Label: "$(save) Replace the document", Description: "replaceOne in " + e.where() + " on " + c.Name},
		{Label: "$(close) Cancel", Description: "nothing is changed"},
	}
	for _, ch := range changes {
		items = append(items, extension.QuickPickItem{Label: "$(diff) " + ch.String(), Description: "changed field"})
	}
	err = q.api.QuickPick().Show(extension.QuickPickOptions{
		Placeholder: "Replace " + IDText(e.id) + " in " + e.where() + "? " + plural(len(changes), "field") + " changed: " + changedNames(changes),
		Items:       items,
	}, func(i int) {
		if i == 0 {
			q.replace(e, merged)
		}
	})
	if err != nil {
		q.api.Log().Warn("the confirmation cannot be shown", "err", err)
		q.api.Notifications().Error("Cannot ask for the confirmation: " + err.Error() + ". Nothing was saved.")
	}
}

// changedNames lists the names of the fields that changed, a few of them.
func changedNames(changes []Change) string {
	var names []string
	for i, ch := range changes {
		if i == 4 {
			names = append(names, "...")
			break
		}
		names = append(names, ch.Path)
	}
	return strings.Join(names, ", ")
}

// replace runs replaceOne for a document that the user confirmed.
func (q *playgrounds) replace(e *docEdit, doc bson.D) {
	b, ok := q.mgr.backend(e.connID)
	if !ok {
		q.api.Notifications().Error("\"" + e.connection + "\" is not connected.")
		return
	}
	call := Call{Kind: CallReplaceOne, Database: e.database, Collection: e.collection, Filter: bson.D{{Key: "_id", Value: e.id}}, Replacement: doc}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		start := time.Now()
		reply, err := b.Exec(ctx, call, 1)
		q.api.Post(func() {
			q.publishFinished(e.connID, err == nil, time.Since(start))
			switch {
			case err != nil:
				q.api.Log().Warn("a document could not be replaced", "err", err)
				q.api.Notifications().Error("Cannot save the document: " + runErrorOf(err).Text())
			case reply.Write != nil && reply.Write.Matched == 0:
				q.api.Notifications().Warn("The document " + IDText(e.id) + " is not in " + e.where() + " any more, so nothing was saved.")
			default:
				e.original = doc
				q.replaceInSets(e, doc)
				q.api.Notifications().Info("Saved the document " + IDText(e.id) + " to " + e.where() + ".")
			}
		})
	}()
}

// replaceInSets puts the saved document in the results that show it.
func (q *playgrounds) replaceInSets(e *docEdit, doc bson.D) {
	for _, ed := range q.eds {
		if ed.rec == nil {
			continue
		}
		for _, sr := range ed.rec.sets {
			if sr.set.Call.Database != e.database || sr.set.Call.Collection != e.collection {
				continue
			}
			for i, d := range sr.set.Docs {
				if id, ok := idOf(d); ok && valueText(id) == valueText(e.id) {
					sr.set.Docs[i] = doc
					sr.invalidate()
				}
			}
		}
	}
	q.refresh()
}

// deleteDoc asks, and then deletes a document of the set with deleteOne({_id}).
func (q *playgrounds) deleteDoc(sr *setRecord, idx int) {
	_, rec, _ := q.shownSet()
	if rec == nil {
		return
	}
	id, ok := q.editable(rec, sr, idx)
	if !ok {
		return
	}
	call := sr.set.Call
	where := call.Database + "." + call.Collection
	connID, connName := rec.connID, rec.connection
	err := q.api.QuickPick().Show(extension.QuickPickOptions{
		Placeholder: "Delete the document " + IDText(id) + " from " + where + " on \"" + connName + "\"? This cannot be undone.",
		Items: []extension.QuickPickItem{
			{Label: "$(trash) Delete the document", Description: "deleteOne in " + where},
			{Label: "$(close) Cancel", Description: "nothing is deleted"},
		},
	}, func(i int) {
		if i == 0 {
			q.deleteConfirmed(connID, connName, call.Database, call.Collection, id)
		}
	})
	if err != nil {
		q.api.Log().Warn("the confirmation cannot be shown", "err", err)
		q.api.Notifications().Error("Cannot ask for the confirmation: " + err.Error() + ". Nothing was deleted.")
	}
}

// deleteConfirmed runs deleteOne for a document that the user confirmed deleting.
func (q *playgrounds) deleteConfirmed(connID, connName, database, collection string, id any) {
	b, ok := q.mgr.backend(connID)
	if !ok {
		q.api.Notifications().Error("\"" + connName + "\" is not connected.")
		return
	}
	call := Call{Kind: CallDeleteOne, Database: database, Collection: collection, Filter: bson.D{{Key: "_id", Value: id}}}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		start := time.Now()
		reply, err := b.Exec(ctx, call, 1)
		q.api.Post(func() {
			q.publishFinished(connID, err == nil, time.Since(start))
			switch {
			case err != nil:
				q.api.Log().Warn("a document could not be deleted", "err", err)
				q.api.Notifications().Error("Cannot delete the document: " + runErrorOf(err).Text())
			case reply.Write != nil && reply.Write.Deleted == 0:
				q.api.Notifications().Warn("The document " + IDText(id) + " is not in " + database + "." + collection + " any more.")
				q.removeFromSets(database, collection, id)
			default:
				q.api.Notifications().Info("Deleted the document " + IDText(id) + " from " + database + "." + collection + ".")
				q.removeFromSets(database, collection, id)
			}
		})
	}()
}

// removeFromSets takes a deleted document out of the results that show it.
func (q *playgrounds) removeFromSets(database, collection string, id any) {
	want := valueText(id)
	for _, ed := range q.eds {
		if ed.rec == nil {
			continue
		}
		for _, sr := range ed.rec.sets {
			if sr.set.Call.Database != database || sr.set.Call.Collection != collection || !sr.set.Editable {
				continue
			}
			kept := sr.set.Docs[:0:0]
			for _, d := range sr.set.Docs {
				if did, ok := idOf(d); ok && valueText(did) == want {
					continue
				}
				kept = append(kept, d)
			}
			if len(kept) != len(sr.set.Docs) {
				sr.set.Docs = kept
				sr.selected = -1
				sr.invalidate()
			}
		}
	}
	q.refresh()
}

// publishFinished sends the event of a command that ended.
func (q *playgrounds) publishFinished(connID string, ok bool, took time.Duration) {
	if err := q.api.Events().Publish(CommandFinishedTopic, map[string]any{"connection": connID, "ok": ok, "millis": took.Milliseconds()}); err != nil {
		q.api.Log().Warn("an event of a command cannot be published", "err", err)
	}
}
