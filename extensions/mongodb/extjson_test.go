package mongodb

import (
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func sampleDoc() bson.D {
	return bson.D{
		{Key: "_id", Value: bson.ObjectID{0x50, 0x7f, 0x1f, 0x77, 0xbc, 0xf8, 0x6c, 0xd7, 0x99, 0x43, 0x90, 0x11}},
		{Key: "sku", Value: "A1"},
		{Key: "qty", Value: int32(2)},
		{Key: "big", Value: int64(5)},
		{Key: "price", Value: 9.5},
		{Key: "created", Value: bson.NewDateTimeFromTime(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))},
		{Key: "customer", Value: bson.D{{Key: "name", Value: "Ann"}, {Key: "address", Value: bson.D{{Key: "city", Value: "Athens"}}}}},
		{Key: "tags", Value: bson.A{"new", "gift"}},
		{Key: "note", Value: nil},
	}
}

func TestADocumentIsWrittenAsRelaxedExtendedJSONAndReadBack(t *testing.T) {
	d := sampleDoc()
	text, err := DocumentText(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"_id": {`, `"$oid": "507f1f77bcf86cd799439011"`, `"qty": 2,`, `"big": 5,`, `"$date": "2024-01-02T03:04:05Z"`, `"city": "Athens"`, `"note": null`} {
		if !strings.Contains(text, want) {
			t.Errorf("the text has no %q:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(text, "{\n  \"_id\"") {
		t.Errorf("not indented with two spaces:\n%s", text)
	}
	back, err := ParseDocument(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(Diff(d, back)) != 0 {
		t.Errorf("the document changed on the way: %v", Diff(d, back))
	}
	// canonical text reads as well
	canon, err := bson.MarshalExtJSON(d, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseDocument(string(canon)); err != nil {
		t.Errorf("canonical text: %v", err)
	}
}

func TestDocumentsAreWrittenAsOneArray(t *testing.T) {
	text, err := DocumentsText([]bson.D{{{Key: "a", Value: int32(1)}}, {{Key: "b", Value: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "[\n  {\n    \"a\": 1\n  },\n  {\n    \"b\": \"x\"\n  }\n]"
	if text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
	if empty, _ := DocumentsText(nil); empty != "[]" {
		t.Errorf("empty = %q", empty)
	}
}

func TestADocumentThatCannotBeWritten(t *testing.T) {
	_, err := DocumentText(bson.D{{Key: "c", Value: make(chan int)}})
	wantRoute(t, err, "DocumentText.MongoDocumentNotJSON")
	_, err = DocumentsText([]bson.D{{{Key: "c", Value: make(chan int)}}})
	wantRoute(t, err, "DocumentText.MongoDocumentNotJSON")
}

func TestParsingTheTextOfADocument(t *testing.T) {
	cases := []struct {
		name, text string
		ok         bool
	}{
		{"a document", `{"a": 1}`, true},
		{"with space around", "  \n{\"a\": 1}\n ", true},
		{"an array", `[{"a": 1}]`, false},
		{"a number", `5`, false},
		{"empty", ``, false},
		{"broken", `{"a": }`, false},
		{"unterminated", `{"a": 1`, false},
		{"a bad date", `{"d": {"$date": "yesterday"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDocument(tc.text)
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				wantRoute(t, err, "ParseDocument.MongoDocumentInvalid")
			}
		})
	}
}

func TestTheChangesBetweenTwoDocuments(t *testing.T) {
	orig := sampleDoc()
	cases := []struct {
		name string
		edit func(d bson.D) bson.D
		want []string
	}{
		{"nothing", func(d bson.D) bson.D { return d }, nil},
		{"a number", func(d bson.D) bson.D { d[2].Value = int32(3); return d }, []string{"~ qty: 2 -> 3"}},
		{"an int32 for an int64 of the same number is no change", func(d bson.D) bson.D { d[3].Value = int32(5); return d }, nil},
		{"a field of a document", func(d bson.D) bson.D {
			d[6].Value = bson.D{{Key: "name", Value: "Bob"}, {Key: "address", Value: bson.D{{Key: "city", Value: "Athens"}}}}
			return d
		}, []string{`~ customer.name: "Ann" -> "Bob"`}},
		{"a deep field", func(d bson.D) bson.D {
			d[6].Value = bson.D{{Key: "name", Value: "Ann"}, {Key: "address", Value: bson.D{{Key: "city", Value: "Sparta"}}}}
			return d
		}, []string{`~ customer.address.city: "Athens" -> "Sparta"`}},
		{"an array is one field", func(d bson.D) bson.D { d[7].Value = bson.A{"new"}; return d }, []string{`~ tags: ["new","gift"] -> ["new"]`}},
		{"a field removed", func(d bson.D) bson.D { return append(d[:8:8], d[9:]...) }, []string{"- note"}},
		{"a field added", func(d bson.D) bson.D { return append(d, bson.E{Key: "extra", Value: true}) }, []string{"+ extra: true"}},
		{"a field added in a document", func(d bson.D) bson.D {
			d[6].Value = bson.D{{Key: "name", Value: "Ann"}, {Key: "address", Value: bson.D{{Key: "city", Value: "Athens"}}}, {Key: "vip", Value: true}}
			return d
		}, []string{"+ customer.vip: true"}},
		{"a document that became a number", func(d bson.D) bson.D { d[6].Value = int32(1); return d }, []string{`~ customer: {"name":"Ann","address":{"city":"Athens"}} -> 1`}},
		{"a long value is cut", func(d bson.D) bson.D { d[1].Value = strings.Repeat("x", 100); return d }, []string{`~ sku: "A1" -> "` + strings.Repeat("x", 59) + "..."}},
		{"the order alone is no change", func(d bson.D) bson.D { d[1], d[2] = d[2], d[1]; return d }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edited := tc.edit(append(bson.D(nil), orig...))
			var got []string
			for _, c := range Diff(orig, edited) {
				got = append(got, c.String())
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("changes = %q\nwant      %q", got, tc.want)
			}
		})
	}
}

func TestMergeKeepsTheTypesOfTheFieldsThatDidNotChange(t *testing.T) {
	orig := sampleDoc()
	orig = append(orig, bson.E{Key: "items", Value: bson.A{bson.D{{Key: "n", Value: int64(1)}}, bson.D{{Key: "n", Value: int64(2)}}}},
		bson.E{Key: "dec", Value: mustDecimal(t, "1.50")})
	text, err := DocumentText(orig)
	if err != nil {
		t.Fatal(err)
	}
	// the user changes qty and the second item
	text = strings.Replace(text, `"qty": 2`, `"qty": 7`, 1)
	text = strings.Replace(text, `"n": 2`, `"n": 9`, 1)
	edited, err := ParseDocument(text)
	if err != nil {
		t.Fatal(err)
	}
	merged := Merge(orig, edited)
	if got := fieldOf(merged, "big"); got != int64(5) {
		t.Errorf("big = %#v, want an int64", got)
	}
	if got := fieldOf(merged, "qty"); got != int32(7) {
		t.Errorf("qty = %#v", got)
	}
	if _, ok := fieldOf(merged, "created").(bson.DateTime); !ok {
		t.Errorf("created = %#v", fieldOf(merged, "created"))
	}
	if _, ok := fieldOf(merged, "dec").(bson.Decimal128); !ok {
		t.Errorf("dec = %#v", fieldOf(merged, "dec"))
	}
	items, _ := fieldOf(merged, "items").(bson.A)
	if len(items) != 2 || fieldOf(items[0].(bson.D), "n") != int64(1) {
		t.Errorf("items = %#v", items)
	}
	if n := fieldOf(items[1].(bson.D), "n"); n != int32(9) {
		t.Errorf("second item = %#v", n)
	}
	if got := Diff(orig, merged); len(got) != 2 {
		t.Errorf("changes of the merged document = %v", got)
	}
	// a field the edit added takes the edited value, and an array of another length is taken whole
	edited = append(edited, bson.E{Key: "extra", Value: int32(1)})
	edited[7].Value = bson.A{"only"}
	merged = Merge(orig, edited)
	if fieldOf(merged, "extra") != int32(1) {
		t.Errorf("extra = %#v", fieldOf(merged, "extra"))
	}
	if tags, _ := fieldOf(merged, "tags").(bson.A); len(tags) != 1 || tags[0] != "only" {
		t.Errorf("tags = %#v", fieldOf(merged, "tags"))
	}
}

func mustDecimal(t *testing.T, s string) bson.Decimal128 {
	t.Helper()
	d, err := bson.ParseDecimal128(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fieldOf(d bson.D, key string) any {
	for _, e := range d {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

func TestTheTextOfAnID(t *testing.T) {
	cases := []struct {
		id   any
		want string
	}{
		{bson.ObjectID{0x50, 0x7f, 0x1f, 0x77, 0xbc, 0xf8, 0x6c, 0xd7, 0x99, 0x43, 0x90, 0x11}, `ObjectId("507f1f77bcf86cd799439011")`},
		{"abc", `"abc"`},
		{int32(12), `12`},
	}
	for _, tc := range cases {
		if got := IDText(tc.id); got != tc.want {
			t.Errorf("IDText(%v) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
