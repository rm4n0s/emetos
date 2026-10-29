package mongodb

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// editRig is a playground whose find returned the sample document, selected in the results.
type editRig struct {
	*playRig
	rec *runRecord
	sr  *setRecord
}

func newEditRig(t *testing.T) *editRig {
	t.Helper()
	p := newPlayRig(t, "db.orders.find({})", true)
	doc2 := bson.D{{Key: "_id", Value: bson.ObjectID{2}}, {Key: "sku", Value: "B2"}, {Key: "qty", Value: int64(5)}}
	p.answer(func(c Call, size int) (Reply, error) {
		switch c.Kind {
		case CallFind:
			return Reply{Docs: []bson.D{sampleDoc(), doc2}}, nil
		case CallReplaceOne:
			return Reply{Docs: []bson.D{{{Key: "acknowledged", Value: true}}}, Write: &WriteSummary{Matched: 1, Modified: 1}}, nil
		case CallDeleteOne:
			return Reply{Docs: []bson.D{{{Key: "acknowledged", Value: true}}}, Write: &WriteSummary{Deleted: 1}}, nil
		}
		return Reply{}, nil
	})
	rec := p.run(RunCommand)
	e := &editRig{playRig: p, rec: rec, sr: rec.sets[0]}
	e.sr.selected = 0
	return e
}

// editedURI opens the selected document for editing and returns the URI of the editor.
func (e *editRig) editedURI() string {
	e.t.Helper()
	before := len(e.mgAPI.UntitledDocuments())
	if err := e.mgAPI.Run(EditDocumentCommand, nil); err != nil {
		e.t.Fatal(err)
	}
	docs := e.mgAPI.UntitledDocuments()
	if len(docs) != before+1 {
		e.t.Fatalf("no document was opened: %v", e.notes())
	}
	d := docs[len(docs)-1]
	if d.LanguageID != "json" {
		e.t.Errorf("language = %q", d.LanguageID)
	}
	e.mgAPI.SetActive(&extension.ActiveDocument{URI: d.URI, LanguageID: "json"})
	e.mgAPI.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: d.URI, LanguageID: "json"})
	e.mgAPI.Pump()
	return d.URI
}

func TestEditingADocumentOpensItAsExtendedJSON(t *testing.T) {
	e := newEditRig(t)
	uri := e.editedURI()
	text, _ := e.mgAPI.DocumentText(uri)
	want, _ := DocumentText(sampleDoc())
	if text != want+"\n" {
		t.Errorf("text = %q", text)
	}
	if e.mgAPI.ContextKey(EditingKey) != true {
		t.Error("the editing key is not set")
	}
	if got := e.lastNote(); !strings.HasPrefix(got, `info: Editing the document ObjectId("507f1f77bcf86cd799439011") of shop.orders. Press Ctrl+S`) {
		t.Errorf("note = %q", got)
	}
	// another editor takes the key away, and the playground editor is not an edited document
	e.activate(nil)
	if e.mgAPI.ContextKey(EditingKey) != false {
		t.Error("the editing key stays set for a playground")
	}
	if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.lastNote(); !strings.Contains(got, "not a MongoDB document") {
		t.Errorf("note = %q", got)
	}
	// closing the editor forgets the document
	e.mgAPI.Emit(extension.TopicDocumentClosed, extension.DocumentClosed{URI: uri})
	e.mgAPI.Pump()
	if e.ext.pg.edits[uri] != nil {
		t.Error("the edited document is still known after its editor closed")
	}
}

func TestSavingADocumentAsksWithTheChangedFieldsAndRunsReplaceOne(t *testing.T) {
	e := newEditRig(t)
	uri := e.editedURI()
	edited := sampleDoc()
	edited[2].Value = int32(3)
	edited[6].Value = bson.D{{Key: "name", Value: "Ann"}, {Key: "address", Value: bson.D{{Key: "city", Value: "Sparta"}}}}
	edited = append(edited[:8:8], bson.E{Key: "vip", Value: true}) // the note is gone, vip is new
	text, err := DocumentText(edited)
	if err != nil {
		t.Fatal(err)
	}
	e.mgAPI.SetDocument(uri, text)
	if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick := e.mgAPI.LastPick()
	if pick == nil {
		t.Fatalf("nothing was asked: %v", e.notes())
	}
	wantPlaceholder := `Replace ObjectId("507f1f77bcf86cd799439011") in shop.orders? 4 fields changed: qty, customer.address.city, note, vip`
	if got := pick.Options().Placeholder; got != wantPlaceholder {
		t.Errorf("placeholder = %q", got)
	}
	labels := pick.Labels()
	if len(labels) != 6 || labels[0] != "$(save) Replace the document" || labels[1] != "$(close) Cancel" {
		t.Fatalf("items = %v", labels)
	}
	joined := strings.Join(labels[2:], "\n")
	for _, want := range []string{"~ qty: 2 -> 3", `~ customer.address.city: "Athens" -> "Sparta"`, "- note", "+ vip: true"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the list of changes has no %q:\n%s", want, joined)
		}
	}
	if len(e.fb.execList()) != 1 {
		t.Fatal("it replaced before the answer")
	}
	pick.Choose(1) // Cancel
	if len(e.fb.execList()) != 1 {
		t.Fatal("Cancel replaced")
	}
	if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	e.mgAPI.LastPick().Choose(0)
	e.mgAPI.WaitFor("replaceOne", func() bool { return len(e.fb.execList()) == 2 })
	call := e.fb.execList()[1].Call
	if call.Kind != CallReplaceOne || call.Database != "shop" || call.Collection != "orders" {
		t.Fatalf("call = %+v", call)
	}
	if len(call.Filter) != 1 || call.Filter[0].Key != "_id" || call.Filter[0].Value != (bson.ObjectID{0x50, 0x7f, 0x1f, 0x77, 0xbc, 0xf8, 0x6c, 0xd7, 0x99, 0x43, 0x90, 0x11}) {
		t.Errorf("filter = %v", call.Filter)
	}
	// the fields that did not change keep their types
	if fieldOf(call.Replacement, "big") != int64(5) {
		t.Errorf("big = %#v", fieldOf(call.Replacement, "big"))
	}
	if _, ok := fieldOf(call.Replacement, "created").(bson.DateTime); !ok {
		t.Errorf("created = %#v", fieldOf(call.Replacement, "created"))
	}
	if fieldOf(call.Replacement, "qty") != int32(3) || fieldOf(call.Replacement, "vip") != true {
		t.Errorf("replacement = %v", call.Replacement)
	}
	for _, e := range call.Replacement {
		if e.Key == "note" {
			t.Error("the removed field is still there")
		}
	}
	e.mgAPI.WaitFor("the note", func() bool { return strings.HasPrefix(e.lastNote(), "info: Saved the document") })
	// the results show the saved document, and a second save with no change says so
	if got := fieldOf(e.sr.set.Docs[0], "qty"); got != int32(3) {
		t.Errorf("the results show qty = %#v", got)
	}
	if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.lastNote(); got != "info: Nothing changed in the document." {
		t.Errorf("note = %q", got)
	}
	// an event for the command
	var n int
	for _, pub := range e.mgAPI.PublishedEvents() {
		if pub.Topic == CommandFinishedTopic {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d commandFinished events, want the find and the replace", n)
	}
}

func TestSavingADocumentThatCannotBeSaved(t *testing.T) {
	e := newEditRig(t)
	uri := e.editedURI()
	orig, _ := e.mgAPI.DocumentText(uri)
	cases := []struct {
		name string
		text string
		want string
	}{
		{"not JSON", `{"a": `, "error: The text is not a document:"},
		{"not a document", `[1, 2]`, "error: The text is not a document:"},
		{"no _id", `{"sku": "A"}`, `error: The document has no _id. Put back the _id: ObjectId("507f1f77bcf86cd799439011")`},
		{"another _id", strings.Replace(orig, "507f1f77bcf86cd799439011", "507f1f77bcf86cd799439012", 1), "error: The _id cannot be changed"},
		{"no change", orig, "info: Nothing changed in the document."},
	}
	picks := len(e.mgAPI.Picks())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.mgAPI.SetDocument(uri, tc.text)
			if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
				t.Fatal(err)
			}
			if got := e.lastNote(); !strings.HasPrefix(got, tc.want) {
				t.Errorf("note = %q, want %q", got, tc.want)
			}
		})
	}
	if len(e.mgAPI.Picks()) != picks || len(e.fb.execList()) != 1 {
		t.Error("something was asked or sent")
	}
	// the connection is read-only now, deleted, or closed
	changed := strings.Replace(orig, `"qty": 2`, `"qty": 9`, 1)
	e.mgAPI.SetDocument(uri, changed)
	c := e.conn
	c.ReadOnly = true
	if _, _, err := e.hub.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
		t.Fatal(err)
	}
	if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.lastNote(); got != `error: "Shop" is read-only.` {
		t.Errorf("note = %q", got)
	}
	c.ReadOnly = false
	if _, _, err := e.hub.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
		t.Fatal(err)
	}
	if err := e.dbAPI.Run(dbconn.Disconnect, e.conn.ID); err != nil {
		t.Fatal(err)
	}
	e.mgAPI.WaitFor("the disconnect", func() bool { return e.hub.State(e.conn.ID).Status != dbconn.Connected })
	if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.lastNote(); got != `error: "Shop" is not connected.` {
		t.Errorf("note = %q", got)
	}
	if _, err := e.hub.Delete(context.Background(), e.conn.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.lastNote(); got != "error: The connection of this document was deleted." {
		t.Errorf("note = %q", got)
	}
}

func TestTheServerRefusingTheReplacementOrNotFindingTheDocument(t *testing.T) {
	e := newEditRig(t)
	uri := e.editedURI()
	orig, _ := e.mgAPI.DocumentText(uri)
	e.mgAPI.SetDocument(uri, strings.Replace(orig, `"qty": 2`, `"qty": 9`, 1))
	save := func() {
		t.Helper()
		if err := e.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
			t.Fatal(err)
		}
		e.mgAPI.LastPick().Choose(0)
	}
	e.answer(func(c Call, size int) (Reply, error) {
		return Reply{}, errors.New("MongoWriteFailed", "Document failed validation", "code", 121, "codeName", "DocumentValidationFailure")
	})
	save()
	e.mgAPI.WaitFor("the error", func() bool { return strings.HasPrefix(e.lastNote(), "error: Cannot save the document:") })
	if got := e.lastNote(); got != "error: Cannot save the document: Error 121 (DocumentValidationFailure): Document failed validation" {
		t.Errorf("note = %q", got)
	}
	if got := fieldOf(e.sr.set.Docs[0], "qty"); got != int32(2) {
		t.Errorf("the results changed although the save failed: %#v", got)
	}
	e.answer(func(c Call, size int) (Reply, error) { return Reply{Write: &WriteSummary{}}, nil })
	save()
	e.mgAPI.WaitFor("the warning", func() bool { return strings.HasPrefix(e.lastNote(), "warn:") })
	if got := e.lastNote(); !strings.Contains(got, "is not in shop.orders any more, so nothing was saved.") {
		t.Errorf("note = %q", got)
	}
}

func TestDeletingADocumentAsksFirst(t *testing.T) {
	e := newEditRig(t)
	e.sr.selected = 1
	if err := e.mgAPI.Run(DeleteDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick := e.mgAPI.LastPick()
	if pick == nil {
		t.Fatal("nothing was asked")
	}
	if got := pick.Options().Placeholder; got != `Delete the document ObjectId("020000000000000000000000") from shop.orders on "Shop"? This cannot be undone.` {
		t.Errorf("placeholder = %q", got)
	}
	if labels := pick.Labels(); len(labels) != 2 || labels[0] != "$(trash) Delete the document" || labels[1] != "$(close) Cancel" {
		t.Errorf("items = %v", labels)
	}
	pick.Choose(1)
	if len(e.fb.execList()) != 1 || len(e.sr.set.Docs) != 2 {
		t.Fatal("Cancel deleted")
	}
	if err := e.mgAPI.Run(DeleteDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	e.mgAPI.LastPick().Choose(0)
	e.mgAPI.WaitFor("deleteOne", func() bool { return len(e.fb.execList()) == 2 })
	call := e.fb.execList()[1].Call
	if call.Kind != CallDeleteOne || call.Collection != "orders" || len(call.Filter) != 1 || call.Filter[0].Key != "_id" || call.Filter[0].Value != (bson.ObjectID{2}) {
		t.Fatalf("call = %+v", call)
	}
	e.mgAPI.WaitFor("the document to go", func() bool { return len(e.sr.set.Docs) == 1 })
	if e.sr.selected != -1 || e.sr.title() != "orders.find (1 document)" {
		t.Errorf("selected %d title %q", e.sr.selected, e.sr.title())
	}
	if got := e.lastNote(); !strings.HasPrefix(got, "info: Deleted the document") {
		t.Errorf("note = %q", got)
	}
	// the server did not find it: the results drop it anyway
	e.answer(func(c Call, size int) (Reply, error) { return Reply{Write: &WriteSummary{}}, nil })
	e.sr.selected = 0
	if err := e.mgAPI.Run(DeleteDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	e.mgAPI.LastPick().Choose(0)
	e.mgAPI.WaitFor("the warning", func() bool { return strings.HasPrefix(e.lastNote(), "warn:") })
	if len(e.sr.set.Docs) != 0 {
		t.Errorf("docs = %d", len(e.sr.set.Docs))
	}
	// an error of the server
	e2 := newEditRig(t)
	e2.answer(func(c Call, size int) (Reply, error) {
		return Reply{}, errors.New("MongoWriteFailed", "not authorized on shop to execute command { delete }", "code", 13, "codeName", "Unauthorized")
	})
	if err := e2.mgAPI.Run(DeleteDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	e2.mgAPI.LastPick().Choose(0)
	e2.mgAPI.WaitFor("the error", func() bool { return strings.HasPrefix(e2.lastNote(), "error: Cannot delete the document:") })
	if got := e2.lastNote(); !strings.Contains(got, "Error 13 (Unauthorized)") || len(e2.sr.set.Docs) != 2 {
		t.Errorf("note = %q docs %d", got, len(e2.sr.set.Docs))
	}
}

func TestEditAndDeleteAreNotOfferedWhereTheyCannotWork(t *testing.T) {
	// a find with a projection, an aggregation, a read-only connection and a document with no _id
	cases := []struct {
		name   string
		script string
		reply  []bson.D
		setup  func(e *playRig)
		want   string
	}{
		{"a projection", "db.orders.find({}, {sku: 1})", docsOf(2), nil, "info: These documents cannot be edited: they come from a projection"},
		{"a projection in the chain", "db.orders.find({}).project({sku: 1})", docsOf(2), nil, "info: These documents cannot be edited"},
		{"an aggregation", "db.orders.aggregate([{$match: {}}])", docsOf(2), nil, "info: These documents cannot be edited"},
		{"a command", "db.runCommand({ping: 1})", docsOf(1), nil, "info: These documents cannot be edited"},
		{"no _id", "db.orders.find({})", []bson.D{{{Key: "a", Value: int32(1)}}}, nil, "error: The document has no _id"},
		{"a read-only connection", "db.orders.find({})", docsOf(1), func(p *playRig) {
			c := p.conn
			c.ReadOnly = true
			if _, _, err := p.hub.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
				t.Fatal(err)
			}
		}, `error: "Shop" is read-only.`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPlayRig(t, tc.script, true)
			p.answer(func(c Call, size int) (Reply, error) { return Reply{Docs: tc.reply}, nil })
			rec := p.run(RunCommand)
			if tc.setup != nil {
				tc.setup(p)
			}
			rec.sets[0].selected = 0
			for _, command := range []string{EditDocumentCommand, DeleteDocumentCommand} {
				picks, docs := len(p.mgAPI.Picks()), len(p.mgAPI.UntitledDocuments())
				if err := p.mgAPI.Run(command, nil); err != nil {
					t.Fatal(err)
				}
				if got := p.lastNote(); !strings.HasPrefix(got, tc.want) {
					t.Errorf("%s: note = %q, want %q", command, got, tc.want)
				}
				if len(p.mgAPI.Picks()) != picks || len(p.mgAPI.UntitledDocuments()) != docs {
					t.Errorf("%s opened something", command)
				}
			}
			// the menu of a document has no edit and delete either
			var items []string
			for _, it := range p.ext.pg.documentMenu(rec.sets[0], 0) {
				items = append(items, it.Label)
			}
			if tc.name == "no _id" {
				return // the menu offers them; the command says why not
			}
			if strings.Join(items, ",") != "Copy Document" {
				t.Errorf("menu = %v", items)
			}
		})
	}
}

func TestTheSelectedDocumentAndTheMenuOfADocument(t *testing.T) {
	e := newEditRig(t)
	var items []string
	for _, it := range e.ext.pg.documentMenu(e.sr, 1) {
		items = append(items, it.Label)
	}
	if strings.Join(items, ",") != "Edit Document,Delete Document,Copy Document" {
		t.Errorf("menu = %v", items)
	}
	if got := e.ext.pg.documentMenu(e.sr, 5); got != nil {
		t.Errorf("menu of a document that is not there = %v", got)
	}
	// nothing selected in a set of two asks to select; a set of one needs no selection
	e.sr.selected = -1
	if err := e.mgAPI.Run(EditDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.lastNote(); got != "info: Select a document first." {
		t.Errorf("note = %q", got)
	}
	if err := e.mgAPI.Run(DeleteDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.lastNote(); got != "info: Select a document first." {
		t.Errorf("note = %q", got)
	}
	one := newEditRig(t)
	one.sr.set.Docs = one.sr.set.Docs[:1]
	one.sr.selected = -1
	if idx := one.ext.pg.selectedIndex(one.sr); idx != 0 {
		t.Errorf("index = %d", idx)
	}
	// with nothing run there is nothing to select in
	r := newRig(t)
	for _, command := range []string{EditDocumentCommand, DeleteDocumentCommand, CopyDocumentCommand} {
		if err := r.mgAPI.Run(command, nil); err != nil {
			t.Fatal(err)
		}
		if got := r.notes(); got[len(got)-1] != "info: Run a statement that returns documents first." {
			t.Errorf("%s: notes = %v", command, got)
		}
	}
}

func TestCopyingADocument(t *testing.T) {
	e := newEditRig(t)
	e.sr.selected = 1
	if err := e.mgAPI.Run(CopyDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	want, _ := DocumentText(e.sr.set.Docs[1])
	if got := e.mgAPI.ClipboardTexts(); len(got) != 1 || got[0] != want {
		t.Errorf("clipboard = %q", got)
	}
	// with a set of several documents and none selected, all of them
	e.sr.selected = -1
	if err := e.mgAPI.Run(CopyDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	all, _ := DocumentsText(e.sr.set.Docs)
	if got := e.mgAPI.ClipboardTexts(); len(got) != 2 || got[1] != all {
		t.Errorf("clipboard = %q", got)
	}
	// a clipboard that fails says so
	e.mgAPI.FailClipboard(errors.New("FakeClipboard", "no clipboard"))
	if err := e.mgAPI.Run(CopyDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.lastNote(); got != "error: Cannot copy: no clipboard" {
		t.Errorf("note = %q", got)
	}
}
