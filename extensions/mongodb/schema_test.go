package mongodb

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// flatten writes the fields of a schema one per line, indented by depth: path, types and count.
func flatten(fields []*SchemaField, depth int, out *[]string) {
	for _, f := range fields {
		*out = append(*out, fmt.Sprintf("%s%s %s x%d", strings.Repeat("  ", depth), f.Path, f.TypesText(), f.Count))
		flatten(f.Children, depth+1, out)
	}
}

func TestInferSchema(t *testing.T) {
	oid := bson.ObjectID{1}
	cases := []struct {
		name string
		docs []bson.D
		want []string
	}{
		{"no documents", nil, nil},
		{"one flat document", []bson.D{{{Key: "a", Value: "x"}, {Key: "b", Value: int32(1)}}}, []string{"a string x1", "b int x1"}},
		{"types are counted and ordered by how often", []bson.D{
			{{Key: "v", Value: "a"}}, {{Key: "v", Value: int32(1)}}, {{Key: "v", Value: "b"}}, {{Key: "v", Value: nil}},
		}, []string{"v string | int | null x4"}},
		{"every BSON type", []bson.D{{
			{Key: "s", Value: "x"}, {Key: "i", Value: int32(1)}, {Key: "l", Value: int64(1)}, {Key: "d", Value: 1.5}, {Key: "b", Value: true},
			{Key: "o", Value: oid}, {Key: "t", Value: bson.NewDateTimeFromTime(time.Unix(1, 0))}, {Key: "dec", Value: bson.Decimal128{}},
			{Key: "bin", Value: bson.Binary{Subtype: 0, Data: []byte{1}}}, {Key: "uuid", Value: bson.Binary{Subtype: 4, Data: make([]byte, 16)}},
			{Key: "ts", Value: bson.Timestamp{T: 1}}, {Key: "re", Value: bson.Regex{Pattern: "a"}}, {Key: "min", Value: bson.MinKey{}},
			{Key: "max", Value: bson.MaxKey{}}, {Key: "js", Value: bson.JavaScript("x")}, {Key: "sym", Value: bson.Symbol("s")},
			{Key: "und", Value: bson.Undefined{}}, {Key: "ptr", Value: bson.DBPointer{}}, {Key: "m", Value: bson.M{"a": 1}}, {Key: "odd", Value: struct{}{}},
		}}, []string{
			"s string x1", "i int x1", "l long x1", "d double x1", "b bool x1", "o objectId x1", "t date x1", "dec decimal x1", "bin binData x1", "uuid uuid x1",
			"ts timestamp x1", "re regex x1", "min minKey x1", "max maxKey x1", "js javascript x1", "sym symbol x1", "und undefined x1", "ptr dbPointer x1",
			"m object x1", "odd unknown x1",
		}},
		{"fields keep the order they were first seen in", []bson.D{
			{{Key: "b", Value: 1}, {Key: "a", Value: 1}}, {{Key: "c", Value: 1}, {Key: "a", Value: 1}},
		}, []string{"b unknown x1", "a unknown x2", "c unknown x1"}},
		{"a document inside a document", []bson.D{
			{{Key: "u", Value: bson.D{{Key: "name", Value: "a"}, {Key: "geo", Value: bson.D{{Key: "lat", Value: 1.5}}}}}},
			{{Key: "u", Value: bson.D{{Key: "name", Value: "b"}}}},
		}, []string{"u object x2", "  u.name string x2", "  u.geo object x1", "    u.geo.lat double x1"}},
		{"an array of strings", []bson.D{{{Key: "tags", Value: bson.A{"a", "b"}}}, {{Key: "tags", Value: bson.A{}}}}, []string{"tags array<string> x2"}},
		{"an array of mixed items", []bson.D{{{Key: "xs", Value: bson.A{int32(1), "a", int32(2), nil}}}}, []string{"xs array<int | null | string> x1"}},
		{"an array of documents", []bson.D{
			{{Key: "lines", Value: bson.A{bson.D{{Key: "sku", Value: "a"}, {Key: "n", Value: int32(1)}}, bson.D{{Key: "sku", Value: "b"}}}}},
		}, []string{"lines array<object> x1", "  lines.sku string x2", "  lines.n int x1"}},
		{"an array of arrays", []bson.D{{{Key: "m", Value: bson.A{bson.A{int32(1)}, bson.A{}}}}}, []string{"m array<array> x1"}},
		{"a field that is a document in one and a string in another", []bson.D{
			{{Key: "x", Value: bson.D{{Key: "a", Value: 1}}}}, {{Key: "x", Value: "plain"}},
		}, []string{"x object | string x2", "  x.a unknown x1"}},
		{"a key used twice in a document counts once", []bson.D{{{Key: "k", Value: int32(1)}, {Key: "k", Value: "x"}}}, []string{"k int | string x1"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := InferSchema(tt.docs)
			var got []string
			flatten(s.Fields, 0, &got)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("schema:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
			if s.Documents != len(tt.docs) {
				t.Errorf("Documents = %d, want %d", s.Documents, len(tt.docs))
			}
		})
	}
}

func TestSchemaDepthIsLimited(t *testing.T) {
	var inner any = int32(1)
	for i := 0; i < schemaMaxDepth+4; i++ {
		inner = bson.D{{Key: "n", Value: inner}}
	}
	s := InferSchema([]bson.D{{{Key: "root", Value: inner}}})
	depth := 0
	for f := s.Fields[0]; len(f.Children) > 0; f = f.Children[0] {
		depth++
	}
	if depth != schemaMaxDepth {
		t.Errorf("followed %d levels of fields", depth)
	}
}

func TestPercentOfASample(t *testing.T) {
	cases := []struct{ count, of, want int }{
		{0, 10, 1}, {1, 1000, 1}, {1, 3, 33}, {2, 3, 67}, {10, 10, 100}, {11, 10, 100}, {5, 0, 0}, {1, -1, 0}, {1, 2, 50},
	}
	for _, tt := range cases {
		f := &SchemaField{Count: tt.count}
		if got := f.Percent(tt.of); got != tt.want {
			t.Errorf("Percent(%d of %d) = %d, want %d", tt.count, tt.of, got, tt.want)
		}
	}
}

// TestInferSchemaOfRandomDocuments checks, on seeded random documents, what is true of any schema: a field is never in more documents
// than the sample has, the types add up to the times the field was seen, and the paths follow the nesting.
func TestInferSchemaOfRandomDocuments(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	var randDoc func(depth int) bson.D
	randValue := func(depth int) any {
		switch r.Intn(6) {
		case 0:
			return int32(r.Intn(10))
		case 1:
			return "s"
		case 2:
			return nil
		case 3:
			return bson.A{int32(1), "x", bson.D{{Key: "k", Value: true}}}
		case 4:
			if depth > 0 {
				return randDoc(depth - 1)
			}
			return 1.5
		}
		return bson.ObjectID{byte(r.Intn(256))}
	}
	randDoc = func(depth int) bson.D {
		n := r.Intn(6)
		d := bson.D{}
		for i := 0; i < n; i++ {
			d = append(d, bson.E{Key: string(rune('a' + r.Intn(5))), Value: randValue(depth)})
		}
		return d
	}
	for i := 0; i < 200; i++ {
		docs := make([]bson.D, r.Intn(30))
		for j := range docs {
			docs[j] = randDoc(3)
		}
		s := InferSchema(docs)
		var check func(fields []*SchemaField, prefix string, top bool)
		check = func(fields []*SchemaField, prefix string, top bool) {
			for _, f := range fields {
				if wantPath := prefix + f.Name; f.Path != wantPath {
					t.Fatalf("path %q, want %q", f.Path, wantPath)
				}
				if top && f.Count > len(docs) {
					t.Fatalf("%s is in %d of %d documents", f.Path, f.Count, len(docs))
				}
				sum := 0
				for _, n := range f.Types {
					sum += n
				}
				if sum < f.Count {
					t.Fatalf("%s: the types add up to %d, less than %d documents", f.Path, sum, f.Count)
				}
				check(f.Children, f.Path+".", false)
			}
		}
		check(s.Fields, "", true)
	}
}
