package mongodb

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CallKind is what a Call asks of the server.
type CallKind string

// The kinds of call, one for each thing a playground can ask (SPEC §10.22 "Playgrounds").
const (
	// CallFind reads documents with a cursor (find, findOne is CallFindOne).
	CallFind CallKind = "find"
	// CallFindOne reads the first document that matches.
	CallFindOne CallKind = "findOne"
	// CallAggregate runs a pipeline and reads its documents with a cursor.
	CallAggregate CallKind = "aggregate"
	// CallCountDocuments counts the documents that match a filter (the count() of a find too).
	CallCountDocuments CallKind = "countDocuments"
	// CallEstimatedCount reads the estimated number of documents from the metadata of the collection.
	CallEstimatedCount CallKind = "estimatedDocumentCount"
	// CallDistinct lists the distinct values of a field.
	CallDistinct CallKind = "distinct"
	// CallInsertOne, CallInsertMany, CallUpdateOne, CallUpdateMany, CallReplaceOne, CallDeleteOne, CallDeleteMany and CallBulkWrite
	// change the documents of a collection.
	CallInsertOne  CallKind = "insertOne"
	CallInsertMany CallKind = "insertMany"
	CallUpdateOne  CallKind = "updateOne"
	CallUpdateMany CallKind = "updateMany"
	CallReplaceOne CallKind = "replaceOne"
	CallDeleteOne  CallKind = "deleteOne"
	CallDeleteMany CallKind = "deleteMany"
	CallBulkWrite  CallKind = "bulkWrite"
	// CallGetIndexes lists the indexes of a collection.
	CallGetIndexes CallKind = "getIndexes"
	// CallCommand runs a command document on a database (db.runCommand, db.adminCommand; createIndex, dropIndex and drop are
	// commands too).
	CallCommand CallKind = "command"
	// CallShowDatabases and CallShowCollections are "show dbs" and "show collections".
	CallShowDatabases   CallKind = "showDatabases"
	CallShowCollections CallKind = "showCollections"
)

// CallOptions are the options of a call that a playground may give as the last argument of a method.
type CallOptions struct {
	// Upsert makes an update or a replace insert when nothing matches.
	Upsert bool
	// Unordered makes insertMany and bulkWrite go on after an error (the default is ordered, as in the shell).
	Unordered bool
	// ArrayFilters are the filters of an update that names array elements.
	ArrayFilters bson.A
	// Collation is the collation document; Hint is the name of an index or its keys; Comment and Let are passed as they are.
	Collation bson.D
	Hint      any
	Comment   any
	Let       bson.D
	// AllowDiskUse lets an aggregation write to temporary files.
	AllowDiskUse bool
	// BypassValidation skips the validation rules of the collection.
	BypassValidation bool
	// MaxTime ends the call when the server takes longer (maxTimeMS); zero is no limit.
	MaxTime time.Duration
	// CountLimit and CountSkip are the limit and skip options of countDocuments.
	CountLimit, CountSkip int64
}

// BulkOp is one operation of a bulkWrite.
type BulkOp struct {
	// Kind is insertOne, updateOne, updateMany, replaceOne, deleteOne or deleteMany.
	Kind string
	// Filter selects the documents of an update, replace or delete.
	Filter bson.D
	// Doc is the document of insertOne and the replacement of replaceOne.
	Doc bson.D
	// Update is the update document (bson.D) or pipeline (bson.A).
	Update       any
	Upsert       bool
	ArrayFilters bson.A
	Collation    bson.D
	Hint         any
}

// Call is one request to the server, as a playground statement translates to it. It holds no connection and no driver type besides
// the values of the bson package, so that the translation is tested without a server and a fake Backend can answer it.
type Call struct {
	Kind CallKind
	// Database is the database the call runs on; Collection the collection, empty for a command, a show and a call on the server.
	Database, Collection string
	// Filter, Projection and Sort are the documents of a find, and of the update, delete and count calls (Filter).
	Filter, Projection, Sort bson.D
	// Skip and Limit are those of a find; zero is none.
	Skip, Limit int64
	// Pipeline is the pipeline of an aggregation.
	Pipeline bson.A
	// Docs are the documents of an insert (one for insertOne).
	Docs []bson.D
	// Update is the update document (bson.D) or the pipeline (bson.A) of an update.
	Update any
	// Replacement is the document of replaceOne.
	Replacement bson.D
	// Field is the field of distinct.
	Field string
	// Command is the command document of CallCommand.
	Command bson.D
	// Ops are the operations of a bulkWrite.
	Ops []BulkOp
	// Options are the options the statement gave.
	Options CallOptions
}

// Cursor reads the documents that a call left on the server, a page at a time.
type Cursor interface {
	// Next reads up to n more documents. done is true when the cursor has nothing more and is closed.
	Next(ctx context.Context, n int) (docs []bson.D, done bool, err error)
	// Close ends the cursor on the server; closing a cursor that is done does nothing.
	Close(ctx context.Context) error
}

// WriteSummary counts what a call that writes did.
type WriteSummary struct {
	Inserted, Matched, Modified, Upserted, Deleted int64
}

// Reply is the answer to a call.
type Reply struct {
	// Docs are the documents of the first page of a call that reads with a cursor, and all of the documents of any other call: the
	// documents of a command's answer, the result of a write as a document (acknowledged, insertedId, matchedCount...), a count as
	// {count: n}, the values of distinct as {value: v}.
	Docs []bson.D
	// Cursor reads the rest while it is not nil.
	Cursor Cursor
	// Write is set by a call that writes.
	Write *WriteSummary
}

// Class says what a call does to the data, for the read-only check and the confirmation.
type Class int

// The classes of call.
const (
	// ClassRead only reads.
	ClassRead Class = iota
	// ClassWrite changes data or the structure of a collection.
	ClassWrite
	// ClassDestructive can lose a lot of data: drop, dropDatabase, deleteMany({}) and updateMany({}, ...).
	ClassDestructive
)

// Safety is what Classify says of a call.
type Safety struct {
	Class Class
	// Verb names the call for a message: "deleteMany", "drop".
	Verb string
	// Why is the short text of a destructive call: "deleteMany({})", "dropDatabase".
	Why string
}

// Writes tells whether the call is not a read.
func (s Safety) Writes() bool { return s.Class != ClassRead }

// Editable tells whether the documents the call returns can be edited and deleted one by one: they are whole documents of a
// collection (a find without a projection), so that replacing one with the edited text loses nothing.
func (c Call) Editable() bool {
	return (c.Kind == CallFind || c.Kind == CallFindOne) && c.Database != "" && c.Collection != "" && len(c.Projection) == 0
}

// Title is the name of the call for a tab: orders.find, show dbs, runCommand.
func (c Call) Title() string {
	switch c.Kind {
	case CallShowDatabases:
		return "show dbs"
	case CallShowCollections:
		return "show collections"
	case CallCommand:
		if len(c.Command) > 0 {
			return c.Command[0].Key
		}
		return "command"
	}
	if c.Collection != "" {
		return c.Collection + "." + string(c.Kind)
	}
	return string(c.Kind)
}
