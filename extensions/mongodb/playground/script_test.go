package playground

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestRecovery checks that one mistake does not hide the statements around it.
func TestRecovery(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"an error between two statements", "use a\ndb.c.fnd()\nuse b", `use a ; ERR(MongoParseUnknownMethod) ; use b`},
		{"an error with brackets open", "db.c.find({a: x,\n  b: 2})\nuse b", `ERR(MongoParseUnsupported) ; use b`},
		{"an error ends at a semicolon", "db.c.find({a: x}); use b", `ERR(MongoParseUnsupported) ; use b`},
		{"two errors", "foo\nbar\nuse c", `ERR(MongoParseUnknownStatement) ; ERR(MongoParseUnknownStatement) ; use c`},
		{"an unterminated string stays on its line", "db.c.find({a: 'x})\nuse b", `ERR(MongoParseUnterminatedString) ; use b`},
		{"an unterminated comment is the end", "use a\n/* oops\nuse b", `use a ; ERR(MongoParseUnterminatedComment)`},
		{"a stray closing bracket", "})\nuse b", `ERR(MongoParseUnknownStatement) ; use b`},
		{"a statement of several lines after an error", "db.x.fnd()\ndb.c.find({\n a: 1\n})", `ERR(MongoParseUnknownMethod) ; db["c"].find({ a: 1 })`},
		{"trailing words are an error but the statement before stays", "use a b\nuse c", `ERR(MongoParseTrailing) ; use c`},
		{"a bad name for use", "use a.b\nuse c", `ERR(MongoParseBadDatabaseName) ; use c`},
		{"a statement starts a line even with brackets open", "db.c.find({a: x,\ndb.d.find()", `ERR(MongoParseUnsupported) ; db["d"].find()`},
		{"use at the start of a line ends the lost statement", "db.c.find({a: 'x\nuse b\nshow dbs", `ERR(MongoParseUnterminatedString) ; use b ; show dbs`},
		{"an indented use is not a new statement", "db.c.find({\n  use : 1\n})", `db["c"].find({ use: 1 })`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderAll(Parse(c.src, fixed)); got != c.want {
				t.Errorf("Parse(%q) = %s\nwant %s", c.src, got, c.want)
			}
		})
	}
}

// TestSpansAndAt checks where the items are and which one is under the cursor.
func TestSpansAndAt(t *testing.T) {
	src := "use shop\n\ndb.orders.find({\n  a: 1\n}).limit(2); // note\nshow dbs   \n\n\ndb.c.fnd()\n"
	s := Parse(src, fixed)
	if len(s.Items) != 4 {
		t.Fatalf("items: %s", renderAll(s))
	}
	want := []struct {
		text         string
		line, endLin int
	}{
		{"use shop", 0, 0},
		{"db.orders.find({\n  a: 1\n}).limit(2)", 2, 4},
		{"show dbs", 5, 5},
		{"db.c.fnd()", 8, 8},
	}
	for i, w := range want {
		it := s.Items[i]
		if got := src[it.Span.Start.Offset:it.Span.End.Offset]; got != w.text {
			t.Errorf("item %d is %q, want %q", i, got, w.text)
		}
		if it.Span.Start.Line != w.line || it.Span.End.Line != w.endLin {
			t.Errorf("item %d lines %d-%d, want %d-%d", i, it.Span.Start.Line, it.Span.End.Line, w.line, w.endLin)
		}
	}
	if s.Items[3].Err == nil || s.Items[0].Err != nil {
		t.Errorf("errors: %v %v", s.Items[0].Err, s.Items[3].Err)
	}
	at := []struct {
		name   string
		offset int
		item   int // -1: none
	}{
		{"start of the first", 0, 0},
		{"inside the first", 4, 0},
		{"end of the first line", 8, 0},
		{"in the blank line after it", 9, -1},
		{"inside the second, on its first line", strings.Index(src, "orders") + 2, 1},
		{"inside the second, on a middle line", strings.Index(src, "a: 1"), 1},
		{"after the semicolon of the second", strings.Index(src, ";") + 1, 1},
		{"in the comment after it", strings.Index(src, "note"), 1},
		{"in the trailing spaces of the third", strings.Index(src, "show dbs") + 10, 2},
		{"in the blank lines before the fourth", strings.Index(src, "db.c.fnd") - 1, -1},
		{"in the error", strings.Index(src, "fnd"), 3},
		{"at the very end", len(src), -1},
	}
	for _, a := range at {
		t.Run(a.name, func(t *testing.T) {
			it, ok := s.At(a.offset)
			switch {
			case a.item < 0 && ok:
				t.Errorf("At(%d) = the item at line %d, want none", a.offset, it.Span.Start.Line)
			case a.item >= 0 && !ok:
				t.Errorf("At(%d) found none, want item %d", a.offset, a.item)
			case a.item >= 0 && it.Span != s.Items[a.item].Span:
				t.Errorf("At(%d) = the item at line %d, want item %d", a.offset, it.Span.Start.Line, a.item)
			}
		})
	}
	if got := s.Between(0, 9); len(got) != 1 {
		t.Errorf("Between(0, 9) has %d items", len(got))
	}
	if got := s.Between(0, len(src)); len(got) != 4 {
		t.Errorf("Between(all) has %d items", len(got))
	}
	if p := s.Pos(len(src) + 10); p.Offset != len(src) {
		t.Errorf("Pos past the end = %+v", p)
	}
	if p := s.Pos(-3); p.Offset != 0 || p.Line != 0 {
		t.Errorf("Pos before the start = %+v", p)
	}
}

func TestPlaceOfAnErrorFromElsewhere(t *testing.T) {
	if _, ok := Place(io.EOF); ok {
		t.Error("Place read a place from a plain error")
	}
}

// randomValue makes a value of the kinds Format writes, nested up to depth. It makes no NaN and no whole float64 in the range of int32
// (those read back as an int32, by design).
func randomValue(r *rand.Rand, depth int) any {
	kinds := 17
	if depth <= 0 {
		kinds = 14
	}
	switch r.Intn(kinds) {
	case 0:
		return nil
	case 1:
		return r.Intn(2) == 0
	case 2:
		return randomString(r)
	case 3:
		return int32(r.Uint32())
	case 4:
		return int64(r.Uint64())
	case 5:
		f := r.NormFloat64() * math.Pow(10, float64(r.Intn(30)-10))
		if f == math.Trunc(f) && math.Abs(f) <= math.MaxInt32 {
			f += 0.5
		}
		return f
	case 6:
		var id bson.ObjectID
		r.Read(id[:])
		return id
	case 7:
		return bson.NewDateTimeFromTime(time.Unix(r.Int63n(4_000_000_000), r.Int63n(1000)*1_000_000).UTC())
	case 8:
		d, _ := bson.ParseDecimal128(fmt.Sprintf("%d.%d", r.Intn(1000)-500, r.Intn(1000)))
		return d
	case 9:
		data := make([]byte, r.Intn(20))
		r.Read(data)
		sub := byte(r.Intn(256))
		if sub == 4 && len(data) == 16 {
			sub = 0
		}
		return bson.Binary{Subtype: sub, Data: data}
	case 10:
		return bson.Timestamp{T: r.Uint32(), I: r.Uint32()}
	case 11:
		if r.Intn(2) == 0 {
			return bson.MinKey{}
		}
		return bson.MaxKey{}
	case 12:
		pats := []string{"^a", "a/b", "[/]x", "a.*b", `\d+`, "é"}
		flags := []string{"", "i", "im", "imsxu"}
		return bson.Regex{Pattern: pats[r.Intn(len(pats))], Options: flags[r.Intn(len(flags))]}
	case 13:
		u := make([]byte, 16)
		r.Read(u)
		return bson.Binary{Subtype: 4, Data: u}
	case 14, 15:
		n := r.Intn(4)
		d := make(bson.D, 0, n)
		seen := map[string]bool{}
		for i := 0; i < n; i++ {
			key := randomKey(r)
			if seen[key] {
				continue
			}
			seen[key] = true
			d = append(d, bson.E{Key: key, Value: randomValue(r, depth-1)})
		}
		return d
	default:
		n := r.Intn(4)
		a := make(bson.A, 0, n)
		for i := 0; i < n; i++ {
			a = append(a, randomValue(r, depth-1))
		}
		return a
	}
}

func randomString(r *rand.Rand) string {
	alphabet := []rune("abcXYZ 09_-\"'\\\n\t/é😀\u2028\x00$.{}[]")
	n := r.Intn(12)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(alphabet[r.Intn(len(alphabet))])
	}
	return b.String()
}

func randomKey(r *rand.Rand) string {
	if r.Intn(2) == 0 {
		names := []string{"a", "b2", "$set", "_id", "name", "x_y", "$"}
		return names[r.Intn(len(names))]
	}
	return randomString(r)
}

// TestFormatReadsBack writes seeded random values with Format, reads them again in a statement, and expects the same values.
func TestFormatReadsBack(t *testing.T) {
	r := rand.New(rand.NewSource(20260429))
	for i := 0; i < 3000; i++ {
		v := randomValue(r, 3)
		doc := bson.D{{Key: "v", Value: v}}
		src := "db.t.insertOne(" + Format(doc) + ")"
		s := Parse(src, fixed)
		if len(s.Items) != 1 || s.Items[0].Err != nil {
			t.Fatalf("case %d: Parse(%s) = %s", i, src, renderAll(s))
		}
		got := s.Items[0].Statement.(*CollectionCall).Args[0].(bson.D)
		if !reflect.DeepEqual(got, doc) {
			t.Fatalf("case %d: %s\n got %#v\nwant %#v", i, src, got, doc)
		}
	}
}

// TestQuote reads back strings with the awkward characters.
func TestQuote(t *testing.T) {
	cases := []string{"", "plain", `say "hi"`, `back\slash`, "line\nbreak\r\n", "tab\t", "\x00\x01\x1f\x7f", "é😀", "\u2028\u2029", "'single'", "/slash/", "$dollar", "\ufffd"}
	for _, s := range cases {
		src := "db.t.insertOne({v: " + Quote(s) + "})"
		sc := Parse(src)
		if len(sc.Items) != 1 || sc.Items[0].Err != nil {
			t.Fatalf("Parse(%s) = %s", src, renderAll(sc))
		}
		if got := sc.Items[0].Statement.(*CollectionCall).Args[0].(bson.D)[0].Value; got != s {
			t.Errorf("Quote(%q) read back as %q", s, got)
		}
	}
}

// checkInvariants holds a parse to what every script satisfies, whatever its text.
func checkInvariants(t *testing.T, src string) *Script {
	t.Helper()
	s := Parse(src, fixed)
	last := 0
	for i, it := range s.Items {
		a, b := it.Span.Start.Offset, it.Span.End.Offset
		if a < last || b < a || b > len(src) {
			t.Fatalf("%q: item %d has the span %d-%d after %d", src, i, a, b, last)
		}
		last = b
		if (it.Err == nil) == (it.Statement == nil) {
			t.Fatalf("%q: item %d has an error %v and a statement %v", src, i, it.Err, it.Statement)
		}
		if it.Err != nil {
			if _, ok := Place(it.Err); !ok {
				t.Fatalf("%q: item %d: the error has no place: %v", src, i, it.Err)
			}
		}
		if got := s.Pos(a); got.Line != it.Span.Start.Line || got.Column != it.Span.Start.Column {
			t.Fatalf("%q: item %d start %+v differs from Pos %+v", src, i, it.Span.Start, got)
		}
	}
	return s
}

// TestMutatedScripts damages valid scripts at random places (seeded) and checks that the parser neither panics nor loops, and that
// the items keep their order.
func TestMutatedScripts(t *testing.T) {
	seeds := []string{
		"use shop\nshow dbs\nshow collections\n",
		"db.orders.find({status: 'open', n: {$gt: 5}}, {total: 1}).sort({total: -1}).skip(2).limit(20)\n",
		"db.getCollection(\"a b\").aggregate([{$match: {sku: /^A/i, d: ISODate('2024-01-02'), id: ObjectId('507f1f77bcf86cd799439011')}}])\n",
		"db.runCommand({ping: 1}); db.adminCommand({listDatabases: 1})\n",
		"db.c.updateOne({_id: NumberLong(5)}, {$set: {t: new Date(1700000000000), u: UUID('12345678-9abc-4def-8012-3456789abcde')}}, {upsert: true})\n",
		"// note\n/* block\ncomment */ db.c.insertMany([{a: 1,}, {b: [1,2,3,],},])\n",
	}
	r := rand.New(rand.NewSource(7))
	junk := []string{"{", "}", "(", ")", "[", "]", "'", "\"", "/", "/*", "//", "\\", ",", ";", ":", ".", "\n", "`", "$", "-", "0x", "é", "\xff", "new ", "db.", "use "}
	for _, seed := range seeds {
		for i := 0; i < 400; i++ {
			b := []byte(seed)
			for n := 1 + r.Intn(4); n > 0; n-- {
				pos := r.Intn(len(b) + 1)
				switch r.Intn(3) {
				case 0: // delete
					if pos < len(b) {
						end := min(pos+1+r.Intn(5), len(b))
						b = append(b[:pos:pos], b[end:]...)
					}
				case 1: // insert junk
					j := junk[r.Intn(len(junk))]
					b = append(b[:pos:pos], append([]byte(j), b[pos:]...)...)
				default: // cut
					b = b[:pos]
				}
			}
			checkInvariants(t, string(b))
		}
	}
}

func TestEmptyAndOddSources(t *testing.T) {
	for _, src := range []string{"", " ", "\n\n", ";", "\xef\xbb\xbfuse a", "\ufeffuse a", "use\u00a0a", strings.Repeat("(", 10000), strings.Repeat("db.c.find()\n", 2000), strings.Repeat("{", 5000), strings.Repeat("/*", 3000)} {
		checkInvariants(t, src)
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"use shop\nshow dbs\n", "db.c.find({a: 1}).sort({a: -1}).limit(5)", "db.getCollection('x').aggregate([{$match: {}}])", "db.runCommand({ping: 1})",
		"db.c.insertOne({d: ISODate('2024-01-01'), o: ObjectId('507f1f77bcf86cd799439011'), r: /a/i, n: NumberLong(3)})", "// c\n/* c */ use a",
		"db.c.find({", "db.c.find({a: 'x", "var x", "db.c.find().limit(-1)", "use a.b", "{{{{[[[[",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 1<<14 {
			return
		}
		checkInvariants(t, src)
	})
}
