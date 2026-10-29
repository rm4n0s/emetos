// Package playground reads the playgrounds of the MongoDB extension (SPEC §10.22): the .mongodb files, a subset of the mongosh
// syntax that Emetos parses and runs itself, because Emetos has no JavaScript engine.
//
// # What a playground is
//
// A playground is a list of statements, one after the other, each ending at a semicolon or at the end of its line (a line that
// goes on with a dot, as in db.users.find({})\n  .sort({name: 1}), is the same statement):
//
//	use shop
//	show dbs
//	show collections
//	db.orders.find({status: "open"}, {total: 1}).sort({total: -1}).limit(20)
//	db.getCollection("order items").aggregate([{$match: {sku: /^A/i}}, {$group: {_id: "$sku", n: {$sum: 1}}}])
//	db.runCommand({ping: 1})
//
// The values are relaxed JavaScript literals: documents with quoted or unquoted keys, arrays, single or double quoted strings,
// numbers, true, false, null, NaN and Infinity, regular expressions (/^a/i), trailing commas and comments, and the helpers of
// the shell (ObjectId, ISODate, new Date, NumberLong, NumberInt, NumberDecimal, UUID, BinData, Timestamp, MinKey, MaxKey).
// They become the values of the BSON package of the driver: documents are bson.D (the order of the keys is kept), arrays are
// bson.A, and the rest are int32, int64, float64, string, bool, nil, bson.ObjectID, bson.DateTime, bson.Decimal128,
// bson.Binary, bson.Timestamp, bson.MinKey, bson.MaxKey and bson.Regex. A number that is a whole number in the range of int32
// is an int32 and any other number a float64, as mongosh stores it; use NumberLong for a 64-bit integer.
//
// Anything else (variables, functions, loops, operators, template strings, a method the subset does not have) is refused with the
// place it is at.
//
// # Errors and places
//
// Parse never fails as a whole: it returns every statement it could read, and for each one it could not, the error and the
// place of the error, then goes on at the next statement, so that one mistake does not hide the rest of the file. A place is
// a Pos: the byte offset in the source and the zero-based line and column, the column counted in bytes of the UTF-8 line (as the
// events of the editor count). The errors have the tags MongoParse* and carry "offset", "end", "line" and "column" in their
// metadata; Place reads them.
package playground
