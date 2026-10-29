package mongodb

import (
	"context"
	"log/slog"
	"slices"
	"sort"
	"strconv"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// serverMeta is the metadata an error of the driver adds to the error of a call: the code and name the server gave, so that the
// results can say "Error 11000 (DuplicateKey)".
func serverMeta(err error) []any {
	var meta []any
	var ce mongo.CommandError
	var we mongo.WriteException
	var be mongo.BulkWriteException
	switch {
	case errors.As(err, &ce):
		meta = append(meta, "code", int(ce.Code), "codeName", ce.Name)
	case errors.As(err, &we):
		switch {
		case len(we.WriteErrors) > 0:
			meta = append(meta, "code", we.WriteErrors[0].Code, "codeName", nameOfWrite(we.WriteErrors[0].Code, we.WriteErrors[0].Raw))
		case we.WriteConcernError != nil:
			meta = append(meta, "code", we.WriteConcernError.Code, "codeName", we.WriteConcernError.Name)
		}
	case errors.As(err, &be):
		switch {
		case len(be.WriteErrors) > 0:
			meta = append(meta, "code", be.WriteErrors[0].Code, "codeName", nameOfWrite(be.WriteErrors[0].Code, be.WriteErrors[0].Raw), "index", be.WriteErrors[0].Index)
		case be.WriteConcernError != nil:
			meta = append(meta, "code", be.WriteConcernError.Code, "codeName", be.WriteConcernError.Name)
		}
	}
	return meta
}

// nameOfWrite is the name of the code of a write error: the one the server gave, else the one Emetos knows.
func nameOfWrite(code int, raw bson.Raw) string {
	if n := codeNameOf(raw); n != "" {
		return n
	}
	return codeNames[code]
}

// codeNameOf reads the codeName a write error carries, if the server gave one.
func codeNameOf(raw bson.Raw) string {
	if raw != nil {
		if v, err := raw.LookupErr("codeName"); err == nil {
			if s, _ := v.StringValueOK(); s != "" {
				return s
			}
		}
	}
	return ""
}

// codeNames are the names of the codes a write most often fails with; the server gives a write error a code and no name.
var codeNames = map[int]string{
	2: "BadValue", 13: "Unauthorized", 26: "NamespaceNotFound", 50: "MaxTimeMSExpired", 112: "WriteConflict", 121: "DocumentValidationFailure",
	11000: "DuplicateKey", 11601: "Interrupted",
}

// ServerError reads the code and the name of the code that the server gave in the error of a call; zero and empty when it gave none.
func ServerError(err error) (code int, name string) {
	appErr, ok := errors.FromError(err)
	if !ok {
		return 0, ""
	}
	code, _ = appErr.Metadata["code"].(int)
	name, _ = appErr.Metadata["codeName"].(string)
	return code, name
}

// Exec implements Backend: it runs the call with the driver. The calls that read with a cursor leave a Cursor in the reply; the batch
// size is the page, so that the server sends no more than the page shows.
func (b *driverBackend) Exec(ctx context.Context, c Call, pageSize int) (Reply, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if c.Options.MaxTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Options.MaxTime)
		defer cancel()
	}
	db := b.client.Database(c.Database)
	switch c.Kind {
	case CallShowDatabases:
		dbs, err := b.Databases(ctx)
		if err != nil {
			return Reply{}, err
		}
		sort.SliceStable(dbs, func(i, j int) bool { return dbs[i].Name < dbs[j].Name })
		r := Reply{}
		for _, d := range dbs {
			r.Docs = append(r.Docs, bson.D{{Key: "name", Value: d.Name}, {Key: "sizeOnDisk", Value: d.SizeOnDisk}, {Key: "empty", Value: d.Empty}})
		}
		return r, nil
	case CallShowCollections:
		colls, err := b.Collections(ctx, c.Database)
		if err != nil {
			return Reply{}, err
		}
		sort.SliceStable(colls, func(i, j int) bool { return colls[i].Name < colls[j].Name })
		r := Reply{}
		for _, col := range colls {
			r.Docs = append(r.Docs, bson.D{{Key: "name", Value: col.Name}, {Key: "type", Value: string(col.Kind)}})
		}
		return r, nil
	case CallCommand:
		var out bson.D
		if err := db.RunCommand(ctx, c.Command).Decode(&out); err != nil {
			return Reply{}, errors.NewErr("MongoCommandFailed", err).SetMetadata(append([]any{"database", c.Database, "command", firstKey(c.Command)}, serverMeta(err)...)...)
		}
		return Reply{Docs: []bson.D{out}}, nil
	}
	coll := db.Collection(c.Collection)
	switch c.Kind {
	case CallFind, CallFindOne:
		return b.find(ctx, coll, c, pageSize)
	case CallAggregate:
		o := options.Aggregate().SetBatchSize(int32(pageSize))
		if c.Options.AllowDiskUse {
			o.SetAllowDiskUse(true)
		}
		if col, err := collationOf(c.Options.Collation); err != nil {
			return Reply{}, err
		} else if col != nil {
			o.SetCollation(col)
		}
		if c.Options.Hint != nil {
			o.SetHint(c.Options.Hint)
		}
		if c.Options.Comment != nil {
			o.SetComment(c.Options.Comment)
		}
		if len(c.Options.Let) > 0 {
			o.SetLet(c.Options.Let)
		}
		if c.Options.BypassValidation {
			o.SetBypassDocumentValidation(true)
		}
		cur, err := coll.Aggregate(ctx, c.Pipeline, o)
		if err != nil {
			return Reply{}, errors.NewErr("MongoReadFailed", err).SetMetadata(append([]any{"database", c.Database, "collection", c.Collection, "call", string(c.Kind)}, serverMeta(err)...)...)
		}
		return firstPage(ctx, cur, pageSize)
	case CallGetIndexes:
		cur, err := coll.Indexes().List(ctx)
		if err != nil {
			return Reply{}, errors.NewErr("MongoReadFailed", err).SetMetadata(append([]any{"database", c.Database, "collection", c.Collection, "call", string(c.Kind)}, serverMeta(err)...)...)
		}
		cur.SetBatchSize(int32(pageSize))
		return firstPage(ctx, cur, pageSize)
	case CallCountDocuments:
		o := options.Count()
		if c.Options.CountLimit > 0 {
			o.SetLimit(c.Options.CountLimit)
		}
		if c.Options.CountSkip > 0 {
			o.SetSkip(c.Options.CountSkip)
		}
		if c.Options.Hint != nil {
			o.SetHint(c.Options.Hint)
		}
		if col, err := collationOf(c.Options.Collation); err != nil {
			return Reply{}, err
		} else if col != nil {
			o.SetCollation(col)
		}
		n, err := coll.CountDocuments(ctx, nonNil(c.Filter), o)
		if err != nil {
			return Reply{}, errors.NewErr("MongoReadFailed", err).SetMetadata(append([]any{"database", c.Database, "collection", c.Collection, "call", string(c.Kind)}, serverMeta(err)...)...)
		}
		return Reply{Docs: []bson.D{{{Key: "count", Value: n}}}}, nil
	case CallEstimatedCount:
		n, err := coll.EstimatedDocumentCount(ctx)
		if err != nil {
			return Reply{}, errors.NewErr("MongoReadFailed", err).SetMetadata(append([]any{"database", c.Database, "collection", c.Collection, "call", string(c.Kind)}, serverMeta(err)...)...)
		}
		return Reply{Docs: []bson.D{{{Key: "count", Value: n}}}}, nil
	case CallDistinct:
		o := options.Distinct()
		if col, err := collationOf(c.Options.Collation); err != nil {
			return Reply{}, err
		} else if col != nil {
			o.SetCollation(col)
		}
		res := coll.Distinct(ctx, c.Field, nonNil(c.Filter), o)
		var values bson.A
		// Err first: Decode of a failed result reads the empty array and says something else
		err := res.Err()
		if err == nil {
			err = res.Decode(&values)
		}
		if err != nil {
			return Reply{}, errors.NewErr("MongoReadFailed", err).SetMetadata(append([]any{"database", c.Database, "collection", c.Collection, "call", string(c.Kind)}, serverMeta(err)...)...)
		}
		r := Reply{}
		for _, v := range values {
			r.Docs = append(r.Docs, bson.D{{Key: "value", Value: v}})
		}
		return r, nil
	}
	return b.write(ctx, coll, c)
}

// firstKey is the name of a command.
func firstKey(d bson.D) string {
	if len(d) == 0 {
		return ""
	}
	return d[0].Key
}

// nonNil returns a document for a filter: the server wants {} and not null.
func nonNil(d bson.D) bson.D {
	if d == nil {
		return bson.D{}
	}
	return d
}

// collationOf reads the collation document of a statement.
func collationOf(d bson.D) (*options.Collation, error) {
	if len(d) == 0 {
		return nil, nil
	}
	data, err := bson.Marshal(d)
	if err != nil {
		return nil, errors.NewErr("MongoCollationInvalid", err)
	}
	var c options.Collation
	if err := bson.Unmarshal(data, &c); err != nil {
		return nil, errors.NewErr("MongoCollationInvalid", err)
	}
	return &c, nil
}

// find runs find and findOne.
func (b *driverBackend) find(ctx context.Context, coll *mongo.Collection, c Call, pageSize int) (Reply, error) {
	batch := pageSize
	if c.Limit > 0 && c.Limit <= int64(pageSize) {
		// a batch of exactly the limit leaves the cursor open on the server, which then waits for a getMore that has nothing to give;
		// one more than the limit closes it with the first batch
		batch = int(c.Limit) + 1
	}
	o := options.Find().SetBatchSize(int32(batch))
	if len(c.Projection) > 0 {
		o.SetProjection(c.Projection)
	}
	if len(c.Sort) > 0 {
		o.SetSort(c.Sort)
	}
	if c.Skip > 0 {
		o.SetSkip(c.Skip)
	}
	if c.Limit > 0 {
		o.SetLimit(c.Limit)
	}
	cur, err := coll.Find(ctx, nonNil(c.Filter), o)
	if err != nil {
		return Reply{}, errors.NewErr("MongoReadFailed", err).SetMetadata(append([]any{"database", c.Database, "collection", c.Collection, "call", string(c.Kind)}, serverMeta(err)...)...)
	}
	return firstPage(ctx, cur, pageSize)
}

// firstPage reads the first page of a cursor and leaves the cursor in the reply while it has more.
func firstPage(ctx context.Context, cur *mongo.Cursor, pageSize int) (Reply, error) {
	dc := &driverCursor{cur: cur}
	docs, done, err := dc.Next(ctx, pageSize)
	if err != nil {
		return Reply{}, err
	}
	if done {
		return Reply{Docs: docs}, nil
	}
	return Reply{Docs: docs, Cursor: dc}, nil
}

// driverCursor is a Cursor over the cursor of the driver.
type driverCursor struct {
	cur    *mongo.Cursor
	closed bool
}

// Next implements Cursor.
func (d *driverCursor) Next(ctx context.Context, n int) ([]bson.D, bool, error) {
	if d.closed {
		return nil, true, nil
	}
	var docs []bson.D
	for len(docs) < n && d.cur.Next(ctx) {
		var doc bson.D
		if err := d.cur.Decode(&doc); err != nil {
			d.closeQuietly()
			return docs, true, errors.NewErr("MongoCursorFailed", err)
		}
		docs = append(docs, doc)
	}
	if err := d.cur.Err(); err != nil {
		d.closeQuietly()
		return docs, true, errors.NewErr("MongoCursorFailed", err).SetMetadata(serverMeta(err)...)
	}
	if d.cur.ID() == 0 && d.cur.RemainingBatchLength() == 0 {
		d.closeQuietly()
		return docs, true, nil
	}
	return docs, false, nil
}

// closeQuietly ends the cursor of the driver after it ran dry or failed; the error of the close is of no use then.
func (d *driverCursor) closeQuietly() {
	d.closed = true
	if err := d.cur.Close(context.WithoutCancel(context.Background())); err != nil {
		slog.Debug("a cursor did not close", "err", err)
	}
}

// Close implements Cursor.
func (d *driverCursor) Close(ctx context.Context) error {
	if d.closed {
		return nil
	}
	d.closed = true
	if err := d.cur.Close(ctx); err != nil {
		return errors.NewErr("MongoCursorCloseFailed", err)
	}
	return nil
}

// write runs the calls that change documents and gives the answer as the shell writes it: a document with acknowledged and the counts.
func (b *driverBackend) write(ctx context.Context, coll *mongo.Collection, c Call) (Reply, error) {
	meta := func(err error) []any {
		return append([]any{"database", c.Database, "collection", c.Collection, "call", string(c.Kind)}, serverMeta(err)...)
	}
	col, err := collationOf(c.Options.Collation)
	if err != nil {
		return Reply{}, err
	}
	switch c.Kind {
	case CallInsertOne:
		o := options.InsertOne()
		if c.Options.BypassValidation {
			o.SetBypassDocumentValidation(true)
		}
		if c.Options.Comment != nil {
			o.SetComment(c.Options.Comment)
		}
		res, err := coll.InsertOne(ctx, c.Docs[0], o)
		if err != nil {
			return Reply{}, errors.NewErr("MongoWriteFailed", err).SetMetadata(meta(err)...)
		}
		return Reply{Docs: []bson.D{{{Key: "acknowledged", Value: true}, {Key: "insertedId", Value: res.InsertedID}}}, Write: &WriteSummary{Inserted: 1}}, nil
	case CallInsertMany:
		o := options.InsertMany().SetOrdered(!c.Options.Unordered)
		if c.Options.BypassValidation {
			o.SetBypassDocumentValidation(true)
		}
		if c.Options.Comment != nil {
			o.SetComment(c.Options.Comment)
		}
		docs := make([]any, len(c.Docs))
		for i, d := range c.Docs {
			docs[i] = d
		}
		res, err := coll.InsertMany(ctx, docs, o)
		if err != nil {
			return Reply{}, errors.NewErr("MongoWriteFailed", err).SetMetadata(meta(err)...)
		}
		ids := bson.D{}
		for i, id := range res.InsertedIDs {
			ids = append(ids, bson.E{Key: strconv.Itoa(i), Value: id})
		}
		return Reply{Docs: []bson.D{{{Key: "acknowledged", Value: true}, {Key: "insertedIds", Value: ids}}}, Write: &WriteSummary{Inserted: int64(len(res.InsertedIDs))}}, nil
	case CallUpdateOne, CallUpdateMany:
		var res *mongo.UpdateResult
		var err error
		if c.Kind == CallUpdateOne {
			o := options.UpdateOne().SetUpsert(c.Options.Upsert)
			applyUpdateOptionsOne(o, c, col)
			res, err = coll.UpdateOne(ctx, nonNil(c.Filter), c.Update, o)
		} else {
			o := options.UpdateMany().SetUpsert(c.Options.Upsert)
			applyUpdateOptionsMany(o, c, col)
			res, err = coll.UpdateMany(ctx, nonNil(c.Filter), c.Update, o)
		}
		if err != nil {
			return Reply{}, errors.NewErr("MongoWriteFailed", err).SetMetadata(meta(err)...)
		}
		return updateReply(res), nil
	case CallReplaceOne:
		o := options.Replace().SetUpsert(c.Options.Upsert)
		if col != nil {
			o.SetCollation(col)
		}
		if c.Options.Hint != nil {
			o.SetHint(c.Options.Hint)
		}
		if c.Options.BypassValidation {
			o.SetBypassDocumentValidation(true)
		}
		if c.Options.Comment != nil {
			o.SetComment(c.Options.Comment)
		}
		if len(c.Options.Let) > 0 {
			o.SetLet(c.Options.Let)
		}
		res, err := coll.ReplaceOne(ctx, nonNil(c.Filter), c.Replacement, o)
		if err != nil {
			return Reply{}, errors.NewErr("MongoWriteFailed", err).SetMetadata(meta(err)...)
		}
		return updateReply(res), nil
	case CallDeleteOne, CallDeleteMany:
		var res *mongo.DeleteResult
		var err error
		if c.Kind == CallDeleteOne {
			o := options.DeleteOne()
			if col != nil {
				o.SetCollation(col)
			}
			if c.Options.Hint != nil {
				o.SetHint(c.Options.Hint)
			}
			if c.Options.Comment != nil {
				o.SetComment(c.Options.Comment)
			}
			if len(c.Options.Let) > 0 {
				o.SetLet(c.Options.Let)
			}
			res, err = coll.DeleteOne(ctx, nonNil(c.Filter), o)
		} else {
			o := options.DeleteMany()
			if col != nil {
				o.SetCollation(col)
			}
			if c.Options.Hint != nil {
				o.SetHint(c.Options.Hint)
			}
			if c.Options.Comment != nil {
				o.SetComment(c.Options.Comment)
			}
			if len(c.Options.Let) > 0 {
				o.SetLet(c.Options.Let)
			}
			res, err = coll.DeleteMany(ctx, nonNil(c.Filter), o)
		}
		if err != nil {
			return Reply{}, errors.NewErr("MongoWriteFailed", err).SetMetadata(meta(err)...)
		}
		return Reply{Docs: []bson.D{{{Key: "acknowledged", Value: true}, {Key: "deletedCount", Value: res.DeletedCount}}}, Write: &WriteSummary{Deleted: res.DeletedCount}}, nil
	case CallBulkWrite:
		models := make([]mongo.WriteModel, 0, len(c.Ops))
		for _, op := range c.Ops {
			m, err := bulkModel(op)
			if err != nil {
				return Reply{}, err
			}
			models = append(models, m)
		}
		o := options.BulkWrite().SetOrdered(!c.Options.Unordered)
		if c.Options.BypassValidation {
			o.SetBypassDocumentValidation(true)
		}
		if c.Options.Comment != nil {
			o.SetComment(c.Options.Comment)
		}
		res, err := coll.BulkWrite(ctx, models, o)
		if err != nil {
			return Reply{}, errors.NewErr("MongoWriteFailed", err).SetMetadata(meta(err)...)
		}
		upserted := bson.D{}
		keys := make([]int64, 0, len(res.UpsertedIDs))
		for k := range res.UpsertedIDs {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			upserted = append(upserted, bson.E{Key: strconv.Itoa(int(k)), Value: res.UpsertedIDs[k]})
		}
		return Reply{Docs: []bson.D{{
			{Key: "acknowledged", Value: true}, {Key: "insertedCount", Value: res.InsertedCount}, {Key: "matchedCount", Value: res.MatchedCount},
			{Key: "modifiedCount", Value: res.ModifiedCount}, {Key: "deletedCount", Value: res.DeletedCount}, {Key: "upsertedCount", Value: res.UpsertedCount},
			{Key: "upsertedIds", Value: upserted},
		}}, Write: &WriteSummary{Inserted: res.InsertedCount, Matched: res.MatchedCount, Modified: res.ModifiedCount, Upserted: res.UpsertedCount, Deleted: res.DeletedCount}}, nil
	}
	return Reply{}, errors.New("MongoCallUnsupported", "the backend cannot run this kind of call", "call", string(c.Kind))
}

// updateReply is the answer to an update or a replace.
func updateReply(res *mongo.UpdateResult) Reply {
	return Reply{
		Docs: []bson.D{{
			{Key: "acknowledged", Value: true}, {Key: "matchedCount", Value: res.MatchedCount}, {Key: "modifiedCount", Value: res.ModifiedCount},
			{Key: "upsertedCount", Value: res.UpsertedCount}, {Key: "upsertedId", Value: res.UpsertedID},
		}},
		Write: &WriteSummary{Matched: res.MatchedCount, Modified: res.ModifiedCount, Upserted: res.UpsertedCount},
	}
}

// applyUpdateOptionsOne puts the options of a statement on updateOne.
func applyUpdateOptionsOne(o *options.UpdateOneOptionsBuilder, c Call, col *options.Collation) {
	if len(c.Options.ArrayFilters) > 0 {
		o.SetArrayFilters(c.Options.ArrayFilters)
	}
	if col != nil {
		o.SetCollation(col)
	}
	if c.Options.Hint != nil {
		o.SetHint(c.Options.Hint)
	}
	if c.Options.BypassValidation {
		o.SetBypassDocumentValidation(true)
	}
	if c.Options.Comment != nil {
		o.SetComment(c.Options.Comment)
	}
	if len(c.Options.Let) > 0 {
		o.SetLet(c.Options.Let)
	}
}

// applyUpdateOptionsMany puts the options of a statement on updateMany.
func applyUpdateOptionsMany(o *options.UpdateManyOptionsBuilder, c Call, col *options.Collation) {
	if len(c.Options.ArrayFilters) > 0 {
		o.SetArrayFilters(c.Options.ArrayFilters)
	}
	if col != nil {
		o.SetCollation(col)
	}
	if c.Options.Hint != nil {
		o.SetHint(c.Options.Hint)
	}
	if c.Options.BypassValidation {
		o.SetBypassDocumentValidation(true)
	}
	if c.Options.Comment != nil {
		o.SetComment(c.Options.Comment)
	}
	if len(c.Options.Let) > 0 {
		o.SetLet(c.Options.Let)
	}
}

// bulkModel makes the model of the driver for one operation of a bulkWrite.
func bulkModel(op BulkOp) (mongo.WriteModel, error) {
	col, err := collationOf(op.Collation)
	if err != nil {
		return nil, err
	}
	switch op.Kind {
	case "insertOne":
		return mongo.NewInsertOneModel().SetDocument(op.Doc), nil
	case "updateOne":
		m := mongo.NewUpdateOneModel().SetFilter(nonNil(op.Filter)).SetUpdate(op.Update).SetUpsert(op.Upsert)
		if len(op.ArrayFilters) > 0 {
			m.SetArrayFilters(op.ArrayFilters)
		}
		if col != nil {
			m.SetCollation(col)
		}
		if op.Hint != nil {
			m.SetHint(op.Hint)
		}
		return m, nil
	case "updateMany":
		m := mongo.NewUpdateManyModel().SetFilter(nonNil(op.Filter)).SetUpdate(op.Update).SetUpsert(op.Upsert)
		if len(op.ArrayFilters) > 0 {
			m.SetArrayFilters(op.ArrayFilters)
		}
		if col != nil {
			m.SetCollation(col)
		}
		if op.Hint != nil {
			m.SetHint(op.Hint)
		}
		return m, nil
	case "replaceOne":
		m := mongo.NewReplaceOneModel().SetFilter(nonNil(op.Filter)).SetReplacement(op.Doc).SetUpsert(op.Upsert)
		if col != nil {
			m.SetCollation(col)
		}
		if op.Hint != nil {
			m.SetHint(op.Hint)
		}
		return m, nil
	case "deleteOne":
		m := mongo.NewDeleteOneModel().SetFilter(nonNil(op.Filter))
		if col != nil {
			m.SetCollation(col)
		}
		if op.Hint != nil {
			m.SetHint(op.Hint)
		}
		return m, nil
	case "deleteMany":
		m := mongo.NewDeleteManyModel().SetFilter(nonNil(op.Filter))
		if col != nil {
			m.SetCollation(col)
		}
		if op.Hint != nil {
			m.SetHint(op.Hint)
		}
		return m, nil
	}
	return nil, errors.New("MongoCallUnsupported", "the backend cannot run this operation of a bulkWrite", "kind", op.Kind)
}
