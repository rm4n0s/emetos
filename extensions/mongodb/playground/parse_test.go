package playground

import (
	"math"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestStatements(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"use a name", "use shop", `use shop`},
		{"use with a dash and a comment", "use my-db // the shop", `use my-db`},
		{"use in quotes", `use "shop"`, `use shop`},
		{"use with single quotes", `use 'shop'`, `use shop`},
		{"use as a call", `use("shop")`, `use shop`},
		{"use ends at the line", "use a\nuse b", `use a ; use b`},
		{"use ends at a semicolon", "use a; use b", `use a ; use b`},
		{"show dbs", "show dbs", `show dbs`},
		{"show databases", "show databases", `show dbs`},
		{"show collections", "show collections", `show collections`},
		{"show tables", "show tables", `show collections`},
		{"find with no arguments", "db.users.find()", `db["users"].find()`},
		{"find with a filter and a projection", "db.users.find({age: {$gt: 21}}, {name: 1})", `db["users"].find({ age: { $gt: 21 } }, { name: 1 })`},
		{"find with a chain", "db.users.find({}).sort({age: -1}).skip(5).limit(10)", `db["users"].find({}).sort({ age: -1 }).skip(5).limit(10)`},
		{"find with project and toArray", "db.users.find().project({_id: 0}).toArray()", `db["users"].find().project({ _id: 0 }).toArray()`},
		{"find then pretty", "db.users.find({a: 1}).pretty()", `db["users"].find({ a: 1 }).pretty()`},
		{"find then count", "db.users.find({a: 1}).count()", `db["users"].find({ a: 1 }).count()`},
		{"a chain on the next lines", "db.users.find()\n  .sort({a: 1})\n  // a note\n  .limit(3)", `db["users"].find().sort({ a: 1 }).limit(3)`},
		{"findOne", "db.users.findOne({_id: 1})", `db["users"].findOne({ _id: 1 })`},
		{"aggregate", `db.orders.aggregate([{$match: {s: "a"}}, {$group: {_id: "$sku", n: {$sum: 1}}}])`, `db["orders"].aggregate([{ $match: { s: "a" } }, { $group: { _id: "$sku", n: { $sum: 1 } } }])`},
		{"aggregate with options", "db.o.aggregate([], {allowDiskUse: true})", `db["o"].aggregate([], { allowDiskUse: true })`},
		{"countDocuments", "db.o.countDocuments({a: 1})", `db["o"].countDocuments({ a: 1 })`},
		{"countDocuments with no filter", "db.o.countDocuments()", `db["o"].countDocuments()`},
		{"estimatedDocumentCount", "db.o.estimatedDocumentCount()", `db["o"].estimatedDocumentCount()`},
		{"distinct", `db.o.distinct("city", {a: 1})`, `db["o"].distinct("city", { a: 1 })`},
		{"insertOne", "db.o.insertOne({a: 1, b: 'two'})", `db["o"].insertOne({ a: 1, b: "two" })`},
		{"insertMany", "db.o.insertMany([{a: 1}, {a: 2}])", `db["o"].insertMany([{ a: 1 }, { a: 2 }])`},
		{"updateOne", "db.o.updateOne({a: 1}, {$set: {b: 2}}, {upsert: true})", `db["o"].updateOne({ a: 1 }, { $set: { b: 2 } }, { upsert: true })`},
		{"updateMany with a pipeline", "db.o.updateMany({}, [{$set: {b: '$a'}}])", `db["o"].updateMany({}, [{ $set: { b: "$a" } }])`},
		{"replaceOne", "db.o.replaceOne({_id: 1}, {a: 2})", `db["o"].replaceOne({ _id: 1 }, { a: 2 })`},
		{"deleteOne", "db.o.deleteOne({_id: 1})", `db["o"].deleteOne({ _id: 1 })`},
		{"deleteMany", "db.o.deleteMany({a: 1})", `db["o"].deleteMany({ a: 1 })`},
		{"bulkWrite", "db.o.bulkWrite([{insertOne: {document: {a: 1}}}])", `db["o"].bulkWrite([{ insertOne: { document: { a: 1 } } }])`},
		{"createIndex", "db.o.createIndex({a: 1, b: -1}, {unique: true})", `db["o"].createIndex({ a: 1, b: -1 }, { unique: true })`},
		{"dropIndex by name", `db.o.dropIndex("a_1")`, `db["o"].dropIndex("a_1")`},
		{"dropIndex by keys", `db.o.dropIndex({a: 1})`, `db["o"].dropIndex({ a: 1 })`},
		{"getIndexes", "db.o.getIndexes()", `db["o"].getIndexes()`},
		{"getIndexes then toArray", "db.o.getIndexes().toArray()", `db["o"].getIndexes().toArray()`},
		{"drop", "db.o.drop()", `db["o"].drop()`},
		{"getCollection", `db.getCollection("order items").find({})`, `db["order items"].find({})`},
		{"getCollection with a chain", `db.getCollection("a-b").find({}).limit(1)`, `db["a-b"].find({}).limit(1)`},
		{"a collection in brackets", `db["my.coll"].find()`, `db["my.coll"].find()`},
		{"a dotted collection name", "db.system.users.find()", `db["system.users"].find()`},
		{"runCommand", "db.runCommand({ping: 1})", `db.runCommand({ ping: 1 })`},
		{"adminCommand", `db.adminCommand({listDatabases: 1, nameOnly: true})`, `db.adminCommand({ listDatabases: 1, nameOnly: true })`},
		{"two statements on two lines", "use a\ndb.c.find()", `use a ; db["c"].find()`},
		{"two statements with semicolons", "db.a.find(); db.b.find();", `db["a"].find() ; db["b"].find()`},
		{"empty statements", ";;\n;db.a.drop();;", `db["a"].drop()`},
		{"comments", "// one\n/* two\nlines */ use a // three\n", `use a`},
		{"a statement after a block comment on the same line", "use a /* c */ ; use b", `use a ; use b`},
		{"nothing", "", ``},
		{"only comments", "// nothing here\n/* or here */", ``},
		{"windows line ends", "use a\r\ndb.c.find()\r\n", `use a ; db["c"].find()`},
		{"multiline arguments", "db.c.find({\n  a: 1,\n  b: [\n    1,\n    2,\n  ],\n})", `db["c"].find({ a: 1, b: [1, 2] })`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Parse(c.src, fixed)
			if got := renderAll(s); got != c.want {
				t.Errorf("Parse(%q) = %s\nwant %s", c.src, got, c.want)
			}
		})
	}
}

func TestValues(t *testing.T) {
	cases := []struct {
		name, literal, want string
	}{
		{"unquoted keys", "{a: 1, b2: 2, $c: 3, _d: 4}", `{ a: 1, b2: 2, $c: 3, _d: 4 }`},
		{"quoted keys", `{"a b": 1, 'c': 2}`, `{ "a b": 1, c: 2 }`},
		{"a number as a key", "{1: 'a', 20: 'b'}", `{ "1": "a", "20": "b" }`},
		{"an empty document and array", "{x: {}, y: []}", `{ x: {}, y: [] }`},
		{"a trailing comma", "{a: [1, 2,], b: 3,}", `{ a: [1, 2], b: 3 }`},
		{"the later value of a key wins", "{a: 1, b: 2, a: 3}", `{ a: 3, b: 2 }`},
		{"single quoted strings", `{a: 'it\'s', b: "say \"hi\""}`, `{ a: "it's", b: "say \"hi\"" }`},
		{"string escapes", `{a: "tab\there\nnew \\ \u00e9 \x41 \u{1F600}"}`, `{ a: "tab\there\nnew \\ é A 😀" }`},
		{"a string with a line continuation", "{a: \"one \\\ntwo\"}", `{ a: "one two" }`},
		{"true false null", "{a: true, b: false, c: null}", `{ a: true, b: false, c: null }`},
		{"whole numbers are int32", "{a: 0, b: -5, c: 2147483647, d: -2147483648, e: 1e3, f: 5.0}", `{ a: 0, b: -5, c: 2147483647, d: -2147483648, e: 1000, f: 5 }`},
		{"hexadecimal", "{a: 0x10, b: 0XfF}", `{ a: 16, b: 255 }`},
		{"fractions are doubles", "{a: 1.5, b: -0.25, c: .5, d: 1e-3, e: 2.5E+2}", `{ a: 1.5, b: -0.25, c: 0.5, d: 0.001, e: 250 }`},
		{"large numbers are doubles", "{a: 2147483648, b: -3000000000, c: 1e20}", `{ a: 2147483648, b: -3000000000, c: 100000000000000000000 }`},
		{"signs", "{a: +5, b: - 5, c: -Infinity, d: Infinity}", `{ a: 5, b: -5, c: -Infinity, d: Infinity }`},
		{"NaN", "{a: NaN}", `{ a: NaN }`},
		{"a regular expression", `{a: /^ab+c$/}`, `{ a: /^ab+c$/ }`},
		{"regular expression flags", `{a: /x/imsu}`, `{ a: /x/imsu }`},
		{"flags are put in order", `{a: /x/ui}`, `{ a: /x/iu }`},
		{"a slash in a class and an escaped slash", `{a: /[/]a\/b/}`, `{ a: /[\/]a\/b/ }`},
		{"ObjectId", `{a: ObjectId("507f1f77bcf86cd799439011")}`, `{ a: ObjectId("507f1f77bcf86cd799439011") }`},
		{"new ObjectId", `{a: new ObjectId("507f1f77bcf86cd799439011")}`, `{ a: ObjectId("507f1f77bcf86cd799439011") }`},
		{"ObjectId with no argument", `{a: ObjectId()}`, `{ a: ObjectId("0102030405060708090a0b0c") }`},
		{"ISODate", `{a: ISODate("2024-01-31T10:30:00Z")}`, `{ a: ISODate("2024-01-31T10:30:00.000Z") }`},
		{"ISODate with a fraction", `{a: ISODate("2024-01-31T10:30:00.123Z")}`, `{ a: ISODate("2024-01-31T10:30:00.123Z") }`},
		{"ISODate with an offset", `{a: ISODate("2024-01-31T10:30:00+02:00")}`, `{ a: ISODate("2024-01-31T08:30:00.000Z") }`},
		{"ISODate with a compact offset", `{a: ISODate("2024-01-31T10:30:00.5-0100")}`, `{ a: ISODate("2024-01-31T11:30:00.500Z") }`},
		{"ISODate with no zone is UTC", `{a: ISODate("2024-01-31T10:30:00")}`, `{ a: ISODate("2024-01-31T10:30:00.000Z") }`},
		{"ISODate with a day", `{a: ISODate("2024-01-31")}`, `{ a: ISODate("2024-01-31T00:00:00.000Z") }`},
		{"ISODate with no argument", `{a: ISODate()}`, `{ a: ISODate("2024-05-06T07:08:09.123Z") }`},
		{"new Date with a string", `{a: new Date("2024-01-31")}`, `{ a: ISODate("2024-01-31T00:00:00.000Z") }`},
		{"new Date with milliseconds", `{a: new Date(1700000000000)}`, `{ a: ISODate("2023-11-14T22:13:20.000Z") }`},
		{"new Date with a small number", `{a: new Date(1000)}`, `{ a: ISODate("1970-01-01T00:00:01.000Z") }`},
		{"new Date with no argument", `{a: new Date()}`, `{ a: ISODate("2024-05-06T07:08:09.123Z") }`},
		{"NumberLong with a number", `{a: NumberLong(5)}`, `{ a: NumberLong("5") }`},
		{"NumberLong with a string", `{a: NumberLong("9223372036854775807")}`, `{ a: NumberLong("9223372036854775807") }`},
		{"NumberLong that is negative", `{a: NumberLong(-7)}`, `{ a: NumberLong("-7") }`},
		{"NumberInt", `{a: NumberInt(7), b: NumberInt("8")}`, `{ a: 7, b: 8 }`},
		{"NumberDecimal", `{a: NumberDecimal("1.25"), b: NumberDecimal(3)}`, `{ a: NumberDecimal("1.25"), b: NumberDecimal("3") }`},
		{"UUID", `{a: UUID("12345678-9abc-4def-8012-3456789abcde")}`, `{ a: UUID("12345678-9abc-4def-8012-3456789abcde") }`},
		{"UUID without dashes", `{a: UUID("123456789abc4def80123456789abcde")}`, `{ a: UUID("12345678-9abc-4def-8012-3456789abcde") }`},
		{"UUID with no argument", `{a: UUID()}`, `{ a: UUID("12345678-9abc-4def-8012-3456789abcde") }`},
		{"BinData", `{a: BinData(0, "aGVsbG8=")}`, `{ a: BinData(0, "aGVsbG8=") }`},
		{"BinData of another subtype", `{a: BinData(128, "AQID")}`, `{ a: BinData(128, "AQID") }`},
		{"Timestamp", `{a: Timestamp(1700000000, 3)}`, `{ a: Timestamp(1700000000, 3) }`},
		{"Timestamp as a document", `{a: Timestamp({t: 5, i: 6})}`, `{ a: Timestamp(5, 6) }`},
		{"MinKey and MaxKey", `{a: MinKey(), b: MaxKey(), c: MinKey, d: new MaxKey()}`, `{ a: MinKey(), b: MaxKey(), c: MinKey(), d: MaxKey() }`},
		{"nested", `{a: {b: [ {c: [1, {d: null}]} ]}}`, `{ a: { b: [{ c: [1, { d: null }] }] } }`},
		{"comments inside", "{a: /* one */ 1, // two\n b: 2}", `{ a: 1, b: 2 }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Parse("db.t.insertOne({v: "+c.literal+"})", fixed)
			if len(s.Items) != 1 || s.Items[0].Err != nil {
				t.Fatalf("Parse: %s", renderAll(s))
			}
			call := s.Items[0].Statement.(*CollectionCall)
			// the literal is the value of v, and v is the only key
			if got := Format(call.Args[0].(bson.D)[0].Value); got != c.want {
				t.Errorf("%s\n got %s\nwant %s", c.literal, got, c.want)
			}
		})
	}
}

// TestValueTypes checks the Go types the literals become, which Format cannot tell apart.
func TestValueTypes(t *testing.T) {
	cases := []struct {
		literal string
		want    any
	}{
		{"5", int32(5)},
		{"-5", int32(-5)},
		{"5.0", int32(5)},
		{"1e3", int32(1000)},
		{"2147483648", float64(2147483648)},
		{"1.5", 1.5},
		{"0x7fffffff", int32(math.MaxInt32)},
		{"NumberLong(5)", int64(5)},
		{"NumberInt(5)", int32(5)},
		{`"5"`, "5"},
		{"true", true},
		{"null", nil},
		{"{}", bson.D{}},
		{"[]", bson.A{}},
		{"MinKey", bson.MinKey{}},
		{"MaxKey()", bson.MaxKey{}},
		{"Timestamp(1, 2)", bson.Timestamp{T: 1, I: 2}},
		{"/a/i", bson.Regex{Pattern: "a", Options: "i"}},
		{`UUID("12345678-9abc-4def-8012-3456789abcde")`, bson.Binary{Subtype: 4, Data: []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0x4d, 0xef, 0x80, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde}}},
		{`new Date(86400000)`, bson.DateTime(86400000)},
	}
	for _, c := range cases {
		t.Run(c.literal, func(t *testing.T) {
			s := Parse("db.t.insertOne({v: "+c.literal+"})", fixed)
			if len(s.Items) != 1 || s.Items[0].Err != nil {
				t.Fatalf("Parse: %s", renderAll(s))
			}
			got := s.Items[0].Statement.(*CollectionCall).Args[0].(bson.D)[0].Value
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}
	// the special numbers
	s := Parse("db.t.insertOne({a: NaN, b: Infinity, c: -Infinity})")
	doc := s.Items[0].Statement.(*CollectionCall).Args[0].(bson.D)
	if !math.IsNaN(doc[0].Value.(float64)) || !math.IsInf(doc[1].Value.(float64), 1) || !math.IsInf(doc[2].Value.(float64), -1) {
		t.Errorf("NaN and the infinities: %#v", doc)
	}
}
