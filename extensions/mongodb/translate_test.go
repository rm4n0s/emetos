package mongodb

import (
	"strconv"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// describeCall writes a call on one line for the tables: the kind, the place, and the fields that are set, as playground text.
func describeCall(c Call) string {
	parts := []string{string(c.Kind), c.Database}
	if c.Collection != "" {
		parts[1] += "." + c.Collection
	}
	add := func(name string, v any, set bool) {
		if set {
			parts = append(parts, name+"="+playground.Format(v))
		}
	}
	add("filter", c.Filter, c.Filter != nil)
	add("proj", c.Projection, c.Projection != nil)
	add("sort", c.Sort, c.Sort != nil)
	num := func(name string, n int64) {
		if n != 0 {
			parts = append(parts, name+"="+strconv.FormatInt(n, 10))
		}
	}
	flag := func(name string, set bool) {
		if set {
			parts = append(parts, name)
		}
	}
	num("skip", c.Skip)
	num("limit", c.Limit)
	add("pipeline", c.Pipeline, c.Pipeline != nil)
	for _, d := range c.Docs {
		add("doc", d, true)
	}
	add("update", c.Update, c.Update != nil)
	add("replacement", c.Replacement, c.Replacement != nil)
	add("field", c.Field, c.Field != "")
	add("command", c.Command, c.Command != nil)
	for _, op := range c.Ops {
		parts = append(parts, "op:"+describeOp(op))
	}
	o := c.Options
	flag("upsert", o.Upsert)
	flag("unordered", o.Unordered)
	add("arrayFilters", o.ArrayFilters, o.ArrayFilters != nil)
	add("collation", o.Collation, o.Collation != nil)
	add("hint", o.Hint, o.Hint != nil)
	add("comment", o.Comment, o.Comment != nil)
	add("let", o.Let, o.Let != nil)
	flag("allowDiskUse", o.AllowDiskUse)
	flag("bypass", o.BypassValidation)
	num("maxTime", o.MaxTime.Milliseconds())
	num("countLimit", o.CountLimit)
	num("countSkip", o.CountSkip)
	return strings.Join(parts, " ")
}

func describeOp(op BulkOp) string {
	parts := []string{op.Kind}
	if op.Filter != nil {
		parts = append(parts, "filter="+playground.Format(op.Filter))
	}
	if op.Doc != nil {
		parts = append(parts, "doc="+playground.Format(op.Doc))
	}
	if op.Update != nil {
		parts = append(parts, "update="+playground.Format(op.Update))
	}
	if op.Upsert {
		parts = append(parts, "upsert")
	}
	if op.ArrayFilters != nil {
		parts = append(parts, "arrayFilters="+playground.Format(op.ArrayFilters))
	}
	if op.Collation != nil {
		parts = append(parts, "collation="+playground.Format(op.Collation))
	}
	if op.Hint != nil {
		parts = append(parts, "hint="+playground.Format(op.Hint))
	}
	return strings.Join(parts, ",")
}

// translateText parses a one-statement playground and translates it.
func translateText(t *testing.T, src, db string) (Step, error) {
	t.Helper()
	s := playground.Parse(src)
	if len(s.Items) != 1 || s.Items[0].Err != nil {
		t.Fatalf("%q does not parse as one statement: %+v", src, s.Items)
	}
	return Translate(s.Items[0].Statement, db)
}

// TestTranslatingStatementsToCalls checks the call every statement form becomes.
func TestTranslatingStatementsToCalls(t *testing.T) {
	cases := []struct {
		name, src, db string
		want          string
	}{
		{"use", `use shop`, "", `use=shop`},
		{"show dbs", `show dbs`, "shop", `showDatabases admin`},
		{"show collections", `show collections`, "shop", `showCollections shop`},
		{"show collections with no database", `show collections`, "", `showCollections test`},
		{"runCommand", `db.runCommand({ping: 1})`, "shop", `command shop command={ ping: 1 }`},
		{"adminCommand", `db.adminCommand({listDatabases: 1})`, "shop", `command admin command={ listDatabases: 1 }`},
		{"find with nothing", `db.orders.find()`, "shop", `find shop.orders filter={}`},
		{"find", `db.orders.find({qty: {$gt: 1}}, {sku: 1})`, "shop", `find shop.orders filter={ qty: { $gt: 1 } } proj={ sku: 1 }`},
		{"find with the whole chain", `db.orders.find({a: 1}).sort({a: -1}).skip(2).limit(3).project({a: 1})`, "shop",
			`find shop.orders filter={ a: 1 } proj={ a: 1 } sort={ a: -1 } skip=2 limit=3`},
		{"find with limit zero", `db.orders.find().limit(0)`, "shop", `find shop.orders filter={}`},
		{"find with toArray and pretty", `db.orders.find({a: 1}).toArray().pretty()`, "shop", `find shop.orders filter={ a: 1 }`},
		{"find then count", `db.orders.find({a: 1}).skip(5).limit(2).count()`, "shop", `countDocuments shop.orders filter={ a: 1 }`},
		{"find on getCollection", `db.getCollection("order items").find({})`, "shop", `find shop.order items filter={}`},
		{"find on db[]", `db["a.b"].find({})`, "shop", `find shop.a.b filter={}`},
		{"findOne", `db.orders.findOne({_id: 1})`, "shop", `findOne shop.orders filter={ _id: 1 } limit=1`},
		{"findOne with a projection", `db.orders.findOne({}, {a: 1})`, "shop", `findOne shop.orders filter={} proj={ a: 1 } limit=1`},
		{"aggregate", `db.orders.aggregate([{$match: {a: 1}}, {$limit: 5}])`, "shop", `aggregate shop.orders pipeline=[{ $match: { a: 1 } }, { $limit: 5 }]`},
		{"aggregate with options", `db.orders.aggregate([{$match: {}}], {allowDiskUse: true, maxTimeMS: 2000, hint: "a_1", comment: "x", collation: {locale: "en"}, let: {v: 1}})`, "shop",
			`aggregate shop.orders pipeline=[{ $match: {} }] collation={ locale: "en" } hint="a_1" comment="x" let={ v: 1 } allowDiskUse maxTime=2000`},
		{"countDocuments", `db.orders.countDocuments({a: 1})`, "shop", `countDocuments shop.orders filter={ a: 1 }`},
		{"countDocuments with options", `db.orders.countDocuments({}, {limit: 10, skip: 2, hint: {a: 1}})`, "shop", `countDocuments shop.orders filter={} hint={ a: 1 } countLimit=10 countSkip=2`},
		{"estimatedDocumentCount", `db.orders.estimatedDocumentCount()`, "shop", `estimatedDocumentCount shop.orders`},
		{"distinct", `db.orders.distinct("sku", {qty: 1})`, "shop", `distinct shop.orders filter={ qty: 1 } field="sku"`},
		{"distinct with no filter", `db.orders.distinct("sku")`, "shop", `distinct shop.orders filter={} field="sku"`},
		{"insertOne", `db.orders.insertOne({sku: "A"})`, "shop", `insertOne shop.orders doc={ sku: "A" }`},
		{"insertMany", `db.orders.insertMany([{a: 1}, {a: 2}], {ordered: false})`, "shop", `insertMany shop.orders doc={ a: 1 } doc={ a: 2 } unordered`},
		{"updateOne", `db.orders.updateOne({a: 1}, {$set: {b: 2}})`, "shop", `updateOne shop.orders filter={ a: 1 } update={ $set: { b: 2 } }`},
		{"updateMany with options", `db.orders.updateMany({}, {$inc: {n: 1}}, {upsert: true, arrayFilters: [{"x.a": 1}]})`, "shop",
			`updateMany shop.orders filter={} update={ $inc: { n: 1 } } upsert arrayFilters=[{ "x.a": 1 }]`},
		{"updateOne with a pipeline", `db.orders.updateOne({a: 1}, [{$set: {b: "$a"}}])`, "shop", `updateOne shop.orders filter={ a: 1 } update=[{ $set: { b: "$a" } }]`},
		{"replaceOne", `db.orders.replaceOne({_id: 1}, {a: 2}, {upsert: true})`, "shop", `replaceOne shop.orders filter={ _id: 1 } replacement={ a: 2 } upsert`},
		{"deleteOne", `db.orders.deleteOne({_id: 1})`, "shop", `deleteOne shop.orders filter={ _id: 1 }`},
		{"deleteMany", `db.orders.deleteMany({a: 1}, {collation: {locale: "fr"}, comment: "c"})`, "shop", `deleteMany shop.orders filter={ a: 1 } collation={ locale: "fr" } comment="c"`},
		{"bulkWrite", `db.orders.bulkWrite([{insertOne: {document: {a: 1}}}, {updateOne: {filter: {a: 1}, update: {$set: {a: 2}}, upsert: true}},
			{updateMany: {filter: {}, update: {$inc: {n: 1}}}}, {replaceOne: {filter: {a: 2}, replacement: {a: 3}}}, {deleteOne: {filter: {a: 3}}}, {deleteMany: {filter: {b: 1}}}], {ordered: false})`, "shop",
			`bulkWrite shop.orders op:insertOne,doc={ a: 1 } op:updateOne,filter={ a: 1 },update={ $set: { a: 2 } },upsert op:updateMany,filter={},update={ $inc: { n: 1 } } ` +
				`op:replaceOne,filter={ a: 2 },doc={ a: 3 } op:deleteOne,filter={ a: 3 } op:deleteMany,filter={ b: 1 } unordered`},
		{"createIndex with a name made up", `db.orders.createIndex({sku: 1, created: -1})`, "shop",
			`command shop command={ createIndexes: "orders", indexes: [{ key: { sku: 1, created: -1 }, name: "sku_1_created_-1" }] }`},
		{"createIndex with options", `db.orders.createIndex({loc: "2dsphere"}, {name: "geo", unique: true, expireAfterSeconds: 60})`, "shop",
			`command shop command={ createIndexes: "orders", indexes: [{ key: { loc: "2dsphere" }, name: "geo", unique: true, expireAfterSeconds: 60 }] }`},
		{"dropIndex by name", `db.orders.dropIndex("geo")`, "shop", `command shop command={ dropIndexes: "orders", index: "geo" }`},
		{"dropIndex by keys", `db.orders.dropIndex({a: 1})`, "shop", `command shop command={ dropIndexes: "orders", index: { a: 1 } }`},
		{"getIndexes", `db.orders.getIndexes()`, "shop", `getIndexes shop.orders`},
		{"drop", `db.orders.drop()`, "shop", `command shop command={ drop: "orders" }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, err := translateText(t, tc.src, tc.db)
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if step.Call == nil {
				got = "use=" + step.Use
			} else {
				got = describeCall(*step.Call)
			}
			if got != tc.want {
				t.Errorf("\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestTheTypesOfTheValuesReachTheCall checks that the values are not turned into other types on the way: an int32 stays an int32, a
// long a long, an ObjectId an ObjectId.
func TestTheTypesOfTheValuesReachTheCall(t *testing.T) {
	step, err := translateText(t, `db.orders.insertOne({a: 1, b: NumberLong("5"), c: ObjectId("507f1f77bcf86cd799439011"), d: 1.5, e: ISODate("2024-01-02T03:04:05Z")})`, "shop")
	if err != nil {
		t.Fatal(err)
	}
	d := step.Call.Docs[0]
	if _, ok := d[0].Value.(int32); !ok {
		t.Errorf("a = %T", d[0].Value)
	}
	if _, ok := d[1].Value.(int64); !ok {
		t.Errorf("b = %T", d[1].Value)
	}
	if _, ok := d[2].Value.(bson.ObjectID); !ok {
		t.Errorf("c = %T", d[2].Value)
	}
	if _, ok := d[3].Value.(float64); !ok {
		t.Errorf("d = %T", d[3].Value)
	}
	if _, ok := d[4].Value.(bson.DateTime); !ok {
		t.Errorf("e = %T", d[4].Value)
	}
}

// TestTranslationErrors checks each way a statement that parses cannot become a call, with the route of the error.
func TestTranslationErrors(t *testing.T) {
	cases := []struct {
		name, src string
		route     string
	}{
		{"an unknown option of aggregate", `db.orders.aggregate([{$match: {}}], {explain: true})`, "parseOptions.MongoRunUnknownOption"},
		{"an unknown option of updateOne", `db.orders.updateOne({}, {$set: {a: 1}}, {multi: true})`, "parseOptions.MongoRunUnknownOption"},
		{"upsert that is not a boolean", `db.orders.updateOne({}, {$set: {a: 1}}, {upsert: 1})`, "parseOptions.MongoRunBadOption"},
		{"allowDiskUse that is not a boolean", `db.orders.aggregate([{$match: {}}], {allowDiskUse: "yes"})`, "parseOptions.MongoRunBadOption"},
		{"ordered that is not a boolean", `db.orders.insertMany([{a: 1}], {ordered: "no"})`, "parseOptions.MongoRunBadOption"},
		{"arrayFilters that is not an array", `db.orders.updateOne({}, {$set: {a: 1}}, {arrayFilters: {a: 1}})`, "parseOptions.MongoRunBadOption"},
		{"a collation that is not a document", `db.orders.deleteOne({}, {collation: "en"})`, "parseOptions.MongoRunBadOption"},
		{"a hint that is a number", `db.orders.deleteOne({}, {hint: 1})`, "parseOptions.MongoRunBadOption"},
		{"maxTimeMS that is negative", `db.orders.aggregate([{$match: {}}], {maxTimeMS: -1})`, "parseOptions.MongoRunBadOption"},
		{"a skip that is not a number", `db.orders.countDocuments({}, {skip: "a"})`, "parseOptions.MongoRunBadOption"},
		{"an update with no operator", `db.orders.updateOne({}, {a: 1})`, "updateOf.MongoRunUpdateNeedsOperators"},
		{"an empty update", `db.orders.updateMany({}, {})`, "updateOf.MongoRunUpdateNeedsOperators"},
		{"an empty pipeline update", `db.orders.updateOne({}, [])`, "updateOf.MongoRunUpdateNeedsOperators"},
		{"a replacement with operators", `db.orders.replaceOne({}, {$set: {a: 1}})`, "MongoRunReplacementHasOperators"},
		{"insertMany of nothing", `db.orders.insertMany([])`, "MongoRunNothingToInsert"},
		{"bulkWrite of nothing", `db.orders.bulkWrite([])`, "MongoRunBadBulkOperation"},
		{"a bulkWrite operation with two keys", `db.orders.bulkWrite([{insertOne: {document: {}}, deleteOne: {filter: {}}}])`, "bulkOpOf.MongoRunBadBulkOperation"},
		{"an unknown bulkWrite operation", `db.orders.bulkWrite([{findOne: {filter: {}}}])`, "bulkOpOf.MongoRunBadBulkOperation"},
		{"a bulkWrite operation that is not a document", `db.orders.bulkWrite([{insertOne: 1}])`, "bulkOpOf.MongoRunBadBulkOperation"},
		{"a key the operation does not have", `db.orders.bulkWrite([{deleteOne: {filter: {}, limit: 1}}])`, "bulkOpOf.MongoRunBadBulkOperation"},
		{"a missing key", `db.orders.bulkWrite([{updateOne: {filter: {}}}])`, "bulkOpOf.MongoRunBadBulkOperation"},
		{"a filter that is not a document", `db.orders.bulkWrite([{deleteOne: {filter: 1}}])`, "bulkOpOf.MongoRunBadBulkOperation"},
		{"a replacement with operators in a bulkWrite", `db.orders.bulkWrite([{replaceOne: {filter: {}, replacement: {$set: {a: 1}}}}])`, "bulkOpOf.MongoRunBadBulkOperation"},
		{"upsert that is not a boolean in a bulkWrite", `db.orders.bulkWrite([{updateOne: {filter: {}, update: {$set: {a: 1}}, upsert: 1}}])`, "bulkOpOf.MongoRunBadBulkOperation"},
		{"an update with no operator in a bulkWrite", `db.orders.bulkWrite([{updateOne: {filter: {}, update: {a: 1}}}])`, "updateOf.MongoRunUpdateNeedsOperators"},
		{"createIndex with no keys", `db.orders.createIndex({})`, "MongoRunBadIndex"},
		{"createIndex with the key as an option", `db.orders.createIndex({a: 1}, {key: {b: 1}})`, "MongoRunBadIndex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translateText(t, tc.src, "shop")
			wantRoute(t, err, tc.route)
		})
	}
}

// TestTranslatingWhatTheParserNeverMakes covers the guards for a statement made by hand.
func TestTranslatingWhatTheParserNeverMakes(t *testing.T) {
	_, err := Translate(nil, "shop")
	wantRoute(t, err, "Translate.MongoRunUnknownStatement")
	_, err = Translate(&playground.CollectionCall{Collection: "c", Method: "findAndModify"}, "shop")
	wantRoute(t, err, "translateCollectionCall.MongoRunUnknownMethod")
}

func TestTheNameOfAnIndex(t *testing.T) {
	cases := []struct {
		keys bson.D
		want string
	}{
		{bson.D{{Key: "a", Value: int32(1)}}, "a_1"},
		{bson.D{{Key: "a", Value: int32(1)}, {Key: "b", Value: int32(-1)}}, "a_1_b_-1"},
		{bson.D{{Key: "a", Value: float64(1)}}, "a_1"},
		{bson.D{{Key: "t", Value: "text"}}, "t_text"},
		{bson.D{{Key: "loc", Value: "2dsphere"}, {Key: "n", Value: int64(1)}}, "loc_2dsphere_n_1"},
		{bson.D{{Key: "a", Value: bson.D{{Key: "x", Value: int32(1)}}}}, "a_{ x: 1 }"},
	}
	for _, tc := range cases {
		if got := indexName(tc.keys); got != tc.want {
			t.Errorf("indexName(%v) = %q, want %q", tc.keys, got, tc.want)
		}
	}
}

// TestClassifyingCalls checks what the read-only check and the confirmation look at.
func TestClassifyingCalls(t *testing.T) {
	cases := []struct {
		src   string
		class Class
		why   string
	}{
		{`show dbs`, ClassRead, ""},
		{`show collections`, ClassRead, ""},
		{`db.orders.find({})`, ClassRead, ""},
		{`db.orders.findOne({})`, ClassRead, ""},
		{`db.orders.find({}).count()`, ClassRead, ""},
		{`db.orders.countDocuments({})`, ClassRead, ""},
		{`db.orders.estimatedDocumentCount()`, ClassRead, ""},
		{`db.orders.distinct("a")`, ClassRead, ""},
		{`db.orders.getIndexes()`, ClassRead, ""},
		{`db.orders.aggregate([{$match: {}}])`, ClassRead, ""},
		{`db.orders.aggregate([{$match: {}}, {$out: "x"}])`, ClassWrite, "$out"},
		{`db.orders.aggregate([{$merge: {into: "x"}}])`, ClassWrite, "$merge"},
		{`db.orders.insertOne({})`, ClassWrite, ""},
		{`db.orders.insertMany([{}])`, ClassWrite, ""},
		{`db.orders.updateOne({}, {$set: {a: 1}})`, ClassWrite, ""},
		{`db.orders.updateMany({a: 1}, {$set: {a: 2}})`, ClassWrite, ""},
		{`db.orders.updateMany({}, {$set: {a: 2}})`, ClassDestructive, "updateMany({}, ...)"},
		{`db.orders.replaceOne({}, {a: 1})`, ClassWrite, ""},
		{`db.orders.deleteOne({})`, ClassWrite, ""},
		{`db.orders.deleteMany({a: 1})`, ClassWrite, ""},
		{`db.orders.deleteMany({})`, ClassDestructive, "deleteMany({})"},
		{`db.orders.bulkWrite([{deleteOne: {filter: {}}}])`, ClassWrite, ""},
		{`db.orders.bulkWrite([{deleteMany: {filter: {}}}])`, ClassDestructive, "deleteMany({})"},
		{`db.orders.bulkWrite([{updateMany: {filter: {}, update: {$set: {a: 1}}}}])`, ClassDestructive, "updateMany({})"},
		{`db.orders.createIndex({a: 1})`, ClassWrite, ""},
		{`db.orders.dropIndex("a_1")`, ClassWrite, ""},
		{`db.orders.drop()`, ClassDestructive, "drop"},
		{`db.runCommand({ping: 1})`, ClassRead, ""},
		{`db.runCommand({ listCollections: 1 })`, ClassRead, ""},
		{`db.runCommand({ dbStats: 1 })`, ClassRead, ""},
		{`db.adminCommand({ listDatabases: 1 })`, ClassRead, ""},
		{`db.runCommand({ aggregate: "c", pipeline: [{$match: {}}], cursor: {} })`, ClassRead, ""},
		{`db.runCommand({ aggregate: "c", pipeline: [{$out: "x"}], cursor: {} })`, ClassWrite, "$out"},
		{`db.runCommand({ create: "c" })`, ClassWrite, ""},
		{`db.runCommand({ insert: "c", documents: [{}] })`, ClassWrite, ""},
		{`db.runCommand({ drop: "c" })`, ClassDestructive, "drop"},
		{`db.runCommand({ dropDatabase: 1 })`, ClassDestructive, "dropDatabase"},
		{`db.runCommand({ DROPDATABASE: 1 })`, ClassDestructive, "DROPDATABASE"},
		{`db.runCommand({ shutdown: 1 })`, ClassWrite, ""},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			step, err := translateText(t, tc.src, "shop")
			if err != nil {
				t.Fatal(err)
			}
			got := Classify(*step.Call)
			if got.Class != tc.class || got.Why != tc.why {
				t.Errorf("Classify = %+v, want class %d why %q", got, tc.class, tc.why)
			}
			if got.Writes() != (tc.class != ClassRead) {
				t.Errorf("Writes = %v", got.Writes())
			}
		})
	}
	if got := Classify(Call{Kind: "mystery"}); !got.Writes() {
		t.Errorf("a call of an unknown kind is a read: %+v", got)
	}
	if got := Classify(Call{Kind: CallCommand}); !got.Writes() {
		t.Errorf("a command with no name is a read: %+v", got)
	}
}

func TestCallTitlesAndEditable(t *testing.T) {
	cases := []struct {
		c        Call
		title    string
		editable bool
	}{
		{Call{Kind: CallFind, Database: "d", Collection: "orders"}, "orders.find", true},
		{Call{Kind: CallFindOne, Database: "d", Collection: "orders"}, "orders.findOne", true},
		{Call{Kind: CallFind, Database: "d", Collection: "orders", Projection: bson.D{{Key: "a", Value: int32(1)}}}, "orders.find", false},
		{Call{Kind: CallAggregate, Database: "d", Collection: "orders"}, "orders.aggregate", false},
		{Call{Kind: CallShowDatabases}, "show dbs", false},
		{Call{Kind: CallShowCollections}, "show collections", false},
		{Call{Kind: CallCommand, Command: bson.D{{Key: "ping", Value: int32(1)}}}, "ping", false},
		{Call{Kind: CallCommand}, "command", false},
		{Call{Kind: CallInsertOne}, "insertOne", false},
	}
	for _, tc := range cases {
		if got := tc.c.Title(); got != tc.title {
			t.Errorf("Title(%+v) = %q, want %q", tc.c, got, tc.title)
		}
		if got := tc.c.Editable(); got != tc.editable {
			t.Errorf("Editable(%+v) = %v", tc.c, got)
		}
	}
}
