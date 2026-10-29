package playground

import (
	"strings"
	"testing"
)

// TestErrors holds every tag of the parser to its route and to the place it points at (zero-based line and column).
func TestErrors(t *testing.T) {
	deep := "db.c.find(" + strings.Repeat("[", 101) + strings.Repeat("]", 101) + ")"
	cases := []struct {
		name, src, route string
		line, col        int
	}{
		{"a string cut by the end of the line", "db.c.find({a: 'x})", "lexer.str.MongoParseUnterminatedString", 0, 14},
		{"a string cut by the end of the text", "db.c.find({a: 'x", "lexer.str.MongoParseUnterminatedString", 0, 14},
		{"a comment that does not end", "use a\n/* never", "lexer.skipSpace.MongoParseUnterminatedComment", 1, 0},
		{"a short unicode escape", `db.c.find({a: "\u12"})`, "lexer.str.MongoParseInvalidEscape", 0, 15},
		{"an octal escape", `db.c.find({a: "\077"})`, "lexer.str.MongoParseInvalidEscape", 0, 15},
		{"a BigInt", "db.c.find({a: 5n})", "lexer.number.MongoParseInvalidNumber", 0, 14},
		{"a hexadecimal number with no digits", "db.c.find({a: 0x})", "lexer.number.MongoParseInvalidNumber", 0, 14},
		{"a character that is not part of the language", "db.c.find({a: €})", "lexer.next.MongoParseUnexpectedCharacter", 0, 14},
		{"text that is not UTF-8", "db.c.find({a: \xff})", "lexer.next.MongoParseUnexpectedCharacter", 0, 14},
		{"a regular expression that does not end", "db.c.find({a: /abc})", "lexer.regex.MongoParseUnterminatedRegex", 0, 14},
		{"a regular expression with a bad flag", "db.c.find({a: /abc/z})", "lexer.regex.MongoParseInvalidRegexFlags", 0, 19},
		{"a colon missing", "db.c.find({a 1})", "parser.expect.MongoParseExpectedToken", 0, 13},
		{"a value missing", "db.c.find({a: })", "parser.value.MongoParseExpectedToken", 0, 14},
		{"a sign before a name", "db.c.find({a: -x})", "parser.value.MongoParseExpectedToken", 0, 14},
		{"db alone", "db", "parser.dbStatement.MongoParseExpectedToken", 0, 2},
		{"db. alone", "db.", "parser.dbStatement.MongoParseExpectedToken", 0, 3},
		{"a collection with no method", "db.c", "parser.dbStatement.MongoParseExpectedToken", 0, 4},
		{"a method with no call", "db.c.find", "parser.dbStatement.MongoParseExpectedToken", 0, 9},
		{"a dot with no name", "db.c.", "parser.dbStatement.MongoParseExpectedToken", 0, 5},
		{"a collection in brackets that is not a string", "db[1]", "parser.dbStatement.MongoParseExpectedToken", 0, 3},
		{"new with nothing after it", "db.c.find({a: new})", "parser.identValue.MongoParseExpectedToken", 0, 17},
		{"a document that does not end", "db.c.find({a: 1", "parser.object.MongoParseUnexpectedEnd", 0, 10},
		{"an array that does not end", "db.c.find({a: [1, 2", "parser.array.MongoParseUnexpectedEnd", 0, 14},
		{"arguments that do not end", "db.c.find(", "parser.args.MongoParseUnexpectedEnd", 0, 9},
		{"a second statement on the line", "db.c.find({a:1}) db.c.find()", "parser.endStatement.MongoParseTrailing", 0, 17},
		{"a word after use", "use a b", "parser.endStatement.MongoParseTrailing", 0, 6},
		{"a variable", "var x = 1", "parser.statement.MongoParseUnsupported", 0, 0},
		{"a name as a value", "db.c.find({a: x})", "parser.helper.MongoParseUnsupported", 0, 14},
		{"a template string", "db.c.find({a: `t`})", "parser.value.MongoParseUnsupported", 0, 14},
		{"undefined", "db.c.find({a: undefined})", "parser.identValue.MongoParseUnsupported", 0, 14},
		{"Date without new", "db.c.find({a: Date(1)})", "parser.helper.MongoParseUnsupported", 0, 14},
		{"a gap in an array", "db.c.find({a: [1,,2]})", "parser.array.MongoParseUnsupported", 0, 17},
		{"a helper that does not exist", "db.c.find({a: Foo(1)})", "parser.helper.MongoParseUnknownHelper", 0, 14},
		{"a method that does not exist", "db.c.fnd()", "parser.collectionCall.MongoParseUnknownMethod", 0, 5},
		{"a method of db that does not exist", "db.stats()", "parser.dbMethod.MongoParseUnknownMethod", 0, 3},
		{"too many arguments", "db.c.find({}, {}, {})", "parser.checkArgs.MongoParseBadArguments", 0, 5},
		{"too few arguments", "db.c.insertOne()", "parser.checkArgs.MongoParseBadArguments", 0, 5},
		{"too many arguments to a helper", "db.c.find({a: ObjectId(1, 2)})", "parser.helper.MongoParseBadArguments", 0, 14},
		{"runCommand with no document", "db.runCommand()", "parser.dbMethod.MongoParseBadArguments", 0, 3},
		{"runCommand with an empty document", "db.runCommand({})", "parser.dbMethod.MongoParseBadArguments", 0, 3},
		{"getCollection with a number", "db.getCollection(1).find()", "parser.dbMethod.MongoParseBadArguments", 0, 3},
		{"a string where a document goes", `db.c.find("a")`, "parser.checkArgs.MongoParseBadArgumentType", 0, 10},
		{"a negative limit", "db.c.find().limit(-1)", "parser.checkArgs.MongoParseBadArgumentType", 0, 18},
		{"a pipeline that is a document", "db.c.aggregate({})", "parser.checkArgs.MongoParseBadArgumentType", 0, 15},
		{"a pipeline stage that is a number", "db.c.aggregate([1])", "parser.checkArgs.MongoParseBadArgumentType", 0, 15},
		{"distinct of a number", "db.c.distinct(1)", "parser.checkArgs.MongoParseBadArgumentType", 0, 14},
		{"dropIndex of a number", "db.c.dropIndex(1)", "parser.checkArgs.MongoParseBadArgumentType", 0, 15},
		{"a bad ObjectId", `db.c.find({a: ObjectId("zz")})`, "parser.helper.MongoParseBadLiteral", 0, 14},
		{"a bad date", `db.c.find({a: ISODate("tomorrow")})`, "parser.helper.MongoParseBadLiteral", 0, 14},
		{"a NumberLong with a fraction", "db.c.find({a: NumberLong(1.5)})", "parser.helper.MongoParseBadLiteral", 0, 14},
		{"a NumberInt out of range", "db.c.find({a: NumberInt(1e10)})", "parser.helper.MongoParseBadLiteral", 0, 14},
		{"a bad NumberDecimal", `db.c.find({a: NumberDecimal("x")})`, "parser.helper.MongoParseBadLiteral", 0, 14},
		{"a bad UUID", `db.c.find({a: UUID("x")})`, "parser.helper.MongoParseBadLiteral", 0, 14},
		{"a bad BinData", `db.c.find({a: BinData(300, "x")})`, "parser.helper.MongoParseBadLiteral", 0, 14},
		{"a Timestamp with one number", "db.c.find({a: Timestamp(1)})", "parser.helper.MongoParseBadLiteral", 0, 14},
		{"a number too large", "db.c.find({a: 1e999})", "parser.number.MongoParseBadLiteral", 0, 14},
		{"a hexadecimal number too large", "db.c.find({a: 0xfffffffffffffffff})", "parser.number.MongoParseBadLiteral", 0, 14},
		{"sort after insertOne", "db.c.insertOne({}).sort({})", "parser.collectionCall.MongoParseBadChain", 0, 19},
		{"limit after count", "db.c.find().count().limit(1)", "parser.collectionCall.MongoParseBadChain", 0, 20},
		{"an unknown call after find", "db.c.find().foo()", "parser.collectionCall.MongoParseBadChain", 0, 12},
		{"a database name with a dot", "use a.b", "parser.use.MongoParseBadDatabaseName", 0, 4},
		{"use with no name", "use", "parser.use.MongoParseBadDatabaseName", 0, 0},
		{"use as a call with a bad name", `use("x.y")`, "parser.use.MongoParseBadDatabaseName", 0, 4},
		{"show something else", "show profile", "parser.show.MongoParseUnsupportedShow", 0, 5},
		{"show alone", "show", "parser.show.MongoParseUnsupportedShow", 0, 0},
		{"a statement that is not one", "foo.bar()", "parser.statement.MongoParseUnknownStatement", 0, 0},
		{"a number as a statement", "123", "parser.statement.MongoParseUnknownStatement", 0, 0},
		{"documents nested too deeply", deep, "parser.array.MongoParseTooDeep", 0, 110},
		{"a key that is a bracket", "db.c.find({[a]: 1})", "parser.object.MongoParseBadKey", 0, 11},
		{"a key with a fraction", "db.c.find({1.5: 1})", "parser.object.MongoParseBadKey", 0, 11},
		{"the error is on its own line", "use a\ndb.c.find({a: 1})\ndb.c.fnd()", "parser.collectionCall.MongoParseUnknownMethod", 2, 5},
		{"a column counts bytes", "db.c.find({a: 'é', b: x})", "parser.helper.MongoParseUnsupported", 0, 23},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Parse(c.src, fixed)
			var bad *Item
			for i := range s.Items {
				if s.Items[i].Err != nil {
					bad = &s.Items[i]
					break
				}
			}
			if bad == nil {
				t.Fatalf("Parse(%q) found no error: %s", c.src, renderAll(s))
			}
			wantErr(t, *bad, c.route, c.line, c.col)
		})
	}
}
