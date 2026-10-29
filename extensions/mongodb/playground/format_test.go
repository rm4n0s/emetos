package playground

import (
	"math"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestFormat(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"null", nil, "null"},
		{"a document with awkward keys", bson.D{{Key: "a", Value: int32(1)}, {Key: "b c", Value: true}, {Key: "", Value: "x"}, {Key: "1a", Value: nil}, {Key: "$set", Value: bson.D{}}}, `{ a: 1, "b c": true, "": "x", "1a": null, $set: {} }`},
		{"a nested array", bson.A{int32(1), bson.A{}, bson.D{{Key: "x", Value: bson.A{"y"}}}}, `[1, [], { x: ["y"] }]`},
		{"an int", 7, "7"},
		{"an int64", int64(1) << 60, `NumberLong("1152921504606846976")`},
		{"special floats", bson.A{math.NaN(), math.Inf(1), math.Inf(-1)}, `[NaN, Infinity, -Infinity]`},
		{"floats", bson.A{1.5, 1e-9, 1e30, 123456789.125}, `[1.5, 1e-09, 1e+30, 123456789.125]`},
		{"a regular expression with a slash and a newline", bson.Regex{Pattern: "a/b\nc", Options: "i"}, `/a\/b\nc/i`},
		{"a binary", bson.Binary{Subtype: 5, Data: []byte("hi")}, `BinData(5, "aGk=")`},
		{"a UUID", bson.Binary{Subtype: 4, Data: []byte("0123456789abcdef")}, `UUID("30313233-3435-3637-3839-616263646566")`},
		{"a binary of subtype 4 that is not 16 bytes", bson.Binary{Subtype: 4, Data: []byte("hi")}, `BinData(4, "aGk=")`},
		{"a value of a type that is not BSON", struct{}{}, `"<struct {}>"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Format(c.v); got != c.want {
				t.Errorf("Format = %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestMethodsAreTheTable(t *testing.T) {
	names := Methods()
	if len(names) != len(methods) {
		t.Fatalf("Methods lists %d names and the table has %d", len(names), len(methods))
	}
	for _, n := range names {
		if _, ok := methods[n]; !ok {
			t.Errorf("%s is listed and has no signature", n)
		}
	}
}
