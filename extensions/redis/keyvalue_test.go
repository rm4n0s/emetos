package redis

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func mustDB(t *testing.T, s *fakeServer, i int) DB {
	t.Helper()
	db, err := s.DB(i)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLoadStringCutsAtOneMebibyte(t *testing.T) {
	s := newFakeServer()
	// "é" is two bytes: the cut must not leave half of one
	s.put(0, "big", &fakeKey{typ: TypeString, str: strings.Repeat("a", MaxStringBytes-1) + "é" + "tail"})
	s.put(0, "small", &fakeKey{typ: TypeString, str: "héllo"})
	db := mustDB(t, s, 0)
	big, err := LoadString(context.Background(), db, "big")
	if err != nil {
		t.Fatal(err)
	}
	if !big.Truncated || big.Size != MaxStringBytes+5 || len(big.Data) != MaxStringBytes-1 || big.Binary() {
		t.Errorf("big: truncated %v, size %d, len %d, binary %v", big.Truncated, big.Size, len(big.Data), big.Binary())
	}
	small, err := LoadString(context.Background(), db, "small")
	if err != nil || small.Truncated || small.Data != "héllo" || small.Size != 6 {
		t.Errorf("small = %+v, %v", small, err)
	}
	s.fail["TYPE"] = nil
	s.fail["GETRANGE"] = errors.New("Boom", "boom")
	if _, err := LoadString(context.Background(), db, "small"); err == nil {
		t.Error("an error of the server was lost")
	}
}

func TestStringViews(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		binary     bool
		wantPretty string
		wantJSON   bool
	}{
		{"plain text", "hello", false, "", false},
		{"a JSON object", `{"a":1,"b":[true,null]}`, false, "{\n  \"a\": 1,\n  \"b\": [\n    true,\n    null\n  ]\n}", true},
		{"a JSON array with spaces around", "  [1,2]  ", false, "[\n  1,\n  2\n]", true},
		{"a number is text", "42", false, "", false},
		{"a quoted string is text", `"x"`, false, "", false},
		{"broken JSON", `{"a":`, false, "", false},
		{"empty", "", false, "", false},
		{"invalid UTF-8", "\xff\xfe", true, "", false},
		{"a NUL byte", "a\x00b", true, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := StringValue{Data: tc.data, Size: int64(len(tc.data))}
			if v.Binary() != tc.binary {
				t.Errorf("Binary = %v, want %v", v.Binary(), tc.binary)
			}
			pretty, ok := v.PrettyJSON()
			if ok != tc.wantJSON || pretty != tc.wantPretty {
				t.Errorf("PrettyJSON = %q, %v; want %q, %v", pretty, ok, tc.wantPretty, tc.wantJSON)
			}
		})
	}
	if _, ok := (StringValue{Data: `{"a":1}`, Truncated: true}).PrettyJSON(); ok {
		t.Error("a value that was cut is not shown as JSON")
	}
}

func TestJSONForSave(t *testing.T) {
	tests := []struct {
		name, original, edited, want string
		wantErr                      bool
	}{
		{"compact stays compact", `{"a":1}`, "{\n  \"a\": 2\n}", `{"a":2}`, false},
		{"indented stays as written", "{\n  \"a\": 1\n}", "{\n  \"a\": 2\n}", "{\n  \"a\": 2\n}", false},
		{"invalid", `{"a":1}`, `{"a":`, "", true},
		{"a value with trailing space was compact", ` {"a":1} `, `{"a": 3}`, `{"a":3}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := JSONForSave(tc.original, tc.edited)
			if tc.wantErr {
				wantRoute(t, err, "JSONForSave.RedisJSONInvalid")
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("JSONForSave = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestSaveStringKeepsTheTTLAndRefusesACutValue(t *testing.T) {
	s := fixtureServer()
	db := mustDB(t, s, 0)
	ctx := context.Background()
	v, err := LoadString(ctx, db, "config:app")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveString(ctx, db, "config:app", v, `{"debug":false}`); err != nil {
		t.Fatal(err)
	}
	info, _ := db.Info(ctx, "config:app")
	if info.TTLMillis != 90_000 {
		t.Errorf("TTL = %d, want it kept", info.TTLMillis)
	}
	if got, _, _ := db.GetRange(ctx, "config:app", 100); got != `{"debug":false}` {
		t.Errorf("value = %q", got)
	}
	err = SaveString(ctx, db, "config:app", StringValue{Truncated: true, Size: 5 << 20}, "x")
	wantRoute(t, err, "SaveString.RedisValueTruncated")
	if got, _, _ := db.GetRange(ctx, "config:app", 100); got != `{"debug":false}` {
		t.Errorf("a refused save changed the value: %q", got)
	}
}

func TestHexDump(t *testing.T) {
	got := HexDump("Hello, Redis!\x00\xff\x7f")
	want := "00000000  48 65 6c 6c 6f 2c 20 52  65 64 69 73 21 00 ff 7f  |Hello, Redis!...|\n"
	if got != want {
		t.Errorf("HexDump =\n%q\nwant\n%q", got, want)
	}
	two := HexDump(strings.Repeat("a", 17))
	lines := strings.Split(strings.TrimRight(two, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "00000010  61 ") || !strings.HasSuffix(lines[1], "|a|") {
		t.Errorf("two lines = %q", lines)
	}
	if HexDump("") != "" {
		t.Error("an empty value has no dump")
	}
}

func TestJSONKeys(t *testing.T) {
	s := fixtureServer()
	db := mustDB(t, s, 0)
	ctx := context.Background()
	stored, pretty, err := LoadJSON(ctx, db, "doc:1")
	if err != nil || stored != `{"title":"Hello","tags":["a","b"]}` || !strings.Contains(pretty, "\n  \"title\": \"Hello\"") {
		t.Fatalf("LoadJSON = %q, %q, %v", stored, pretty, err)
	}
	if err := SaveJSON(ctx, db, "doc:1", stored, "{\n \"title\": \"Bye\"\n}"); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.JSONGet(ctx, "doc:1"); got != `{"title":"Bye"}` {
		t.Errorf("saved %q", got)
	}
	wantRoute(t, SaveJSON(ctx, db, "doc:1", stored, "{"), "SaveJSON->JSONForSave.RedisJSONInvalid")
	// a value the server holds that is not valid JSON is shown as it is
	s.put(0, "odd", &fakeKey{typ: TypeJSON, json: "not json"})
	st, pr, err := LoadJSON(ctx, db, "odd")
	if err != nil || st != "not json" || pr != "not json" {
		t.Errorf("LoadJSON(odd) = %q, %q, %v", st, pr, err)
	}
	s.fail["JSON.GET"] = errors.New("Boom", "boom")
	if _, _, err := LoadJSON(ctx, db, "doc:1"); err == nil {
		t.Error("the error of the server was lost")
	}
}

func TestCreateKey(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		typ    string
		values []string
		ttl    time.Duration
		check  func(t *testing.T, db DB)
	}{
		{"string", TypeString, []string{"hello"}, 0, func(t *testing.T, db DB) {
			if got, _, _ := db.GetRange(ctx, "k", 10); got != "hello" {
				t.Errorf("value = %q", got)
			}
		}},
		{"string with a TTL", TypeString, []string{"x"}, 90 * time.Second, func(t *testing.T, db DB) {
			if info, _ := db.Info(ctx, "k"); info.TTLMillis != 90_000 {
				t.Errorf("TTL = %d", info.TTLMillis)
			}
		}},
		{"hash", TypeHash, []string{"f", "v"}, 0, func(t *testing.T, db DB) {
			if f, _, _ := db.HScan(ctx, "k", 0, 10); len(f) != 1 || f[0] != (Pair{"f", "v"}) {
				t.Errorf("fields = %+v", f)
			}
		}},
		{"list", TypeList, []string{"first"}, 0, func(t *testing.T, db DB) {
			if l, _ := db.LRange(ctx, "k", 0, -1); len(l) != 1 || l[0] != "first" {
				t.Errorf("items = %q", l)
			}
		}},
		{"set", TypeSet, []string{"m"}, 0, func(t *testing.T, db DB) {
			if m, _, _ := db.SScan(ctx, "k", 0, 10); len(m) != 1 || m[0] != "m" {
				t.Errorf("members = %q", m)
			}
		}},
		{"sorted set", TypeZSet, []string{"m", "2.5"}, 0, func(t *testing.T, db DB) {
			if m, _, _ := db.ZScan(ctx, "k", 0, 10); len(m) != 1 || m[0] != (ZMember{"m", 2.5}) {
				t.Errorf("members = %+v", m)
			}
		}},
		{"stream", TypeStream, []string{"", "temp 21.5\nhum 40"}, 0, func(t *testing.T, db DB) {
			e, _ := db.XRevRange(ctx, "k", "+", 10)
			if len(e) != 1 || len(e[0].Fields) != 2 || e[0].Fields[1] != (Pair{"hum", "40"}) {
				t.Errorf("entries = %+v", e)
			}
		}},
		{"JSON", TypeJSON, []string{`{"a":1}`}, 0, func(t *testing.T, db DB) {
			if got, _ := db.JSONGet(ctx, "k"); got != `{"a":1}` {
				t.Errorf("doc = %q", got)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeServer()
			db := mustDB(t, s, 0)
			if err := CreateKey(ctx, db, "k", tc.typ, tc.values, tc.ttl); err != nil {
				t.Fatal(err)
			}
			tc.check(t, db)
		})
	}
}

func TestCreateKeyErrors(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	db := mustDB(t, s, 0)
	tests := []struct {
		name   string
		key    string
		typ    string
		values []string
		route  string
	}{
		{"no name", "", TypeString, []string{"x"}, "CreateKey.RedisNameMissing"},
		{"a type that cannot be made", "k", "TSDB-TYPE", nil, "CreateKey.RedisTypeUnknown"},
		{"a key that exists", "greeting", TypeString, []string{"x"}, "CreateKey.RedisKeyExists"},
		{"JSON that is not JSON", "k", TypeJSON, []string{"{"}, "CreateKey.RedisNewJSONInvalid"},
		{"JSON with no text", "k", TypeJSON, nil, "CreateKey.RedisNewJSONInvalid"},
		{"a hash with no field", "k", TypeHash, []string{"", "v"}, "CreateKey->hashColl.Add->need.RedisFieldMissing"},
		{"a sorted set with no score", "k", TypeZSet, []string{"m"}, "CreateKey->zsetColl.Add->need.RedisFieldMissing"},
		{"a sorted set with a bad score", "k", TypeZSet, []string{"m", "x"}, "CreateKey->zsetColl.Add->ParseScore.RedisScoreInvalid"},
		{"a stream with no fields", "k", TypeStream, []string{"", "  \n "}, "CreateKey->streamColl.Add->need.RedisFieldMissing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantRoute(t, CreateKey(ctx, db, tc.key, tc.typ, tc.values, 0), tc.route)
			if ok, _ := db.Exists(ctx, tc.key); ok && tc.key != "greeting" && tc.key != "" {
				t.Errorf("a failed CreateKey left the key %q", tc.key)
			}
		})
	}
	if got, _, _ := db.GetRange(ctx, "greeting", 100); got != "hello world" {
		t.Errorf("a key that existed was changed: %q", got)
	}
	s.fail["EXISTS"] = errors.New("Boom", "boom")
	if err := CreateKey(ctx, db, "z", TypeString, []string{"x"}, 0); err == nil {
		t.Error("the error of EXISTS was lost")
	}
	s.fail["SET"] = errors.New("Boom", "boom")
	if err := CreateKey(ctx, db, "z", TypeString, []string{"x"}, 0); err == nil {
		t.Error("the error of SET was lost")
	}
}

func TestKeyTypeNames(t *testing.T) {
	for _, k := range KeyTypes {
		if IconOf(k.Type) != k.Icon || LabelOf(k.Type) != k.Label {
			t.Errorf("%s: icon %q label %q", k.Type, IconOf(k.Type), LabelOf(k.Type))
		}
		if len(NewKeyFields(k.Type)) == 0 {
			t.Errorf("%s has no fields in the New Key form", k.Type)
		}
	}
	if IconOf("TSDB-TYPE") != "symbol-misc" || LabelOf("TSDB-TYPE") != "TSDB-TYPE" || NewKeyFields("x") != nil {
		t.Error("an unknown type is plain")
	}
}

func TestFormatting(t *testing.T) {
	ttl := []struct {
		ms   int64
		want string
	}{{-1, "no expiry"}, {-2, "gone"}, {0, "0 ms"}, {999, "999 ms"}, {1000, "1s"}, {59_999, "59s"}, {60_000, "1m 0s"}, {200_000, "3m 20s"},
		{3_600_000, "1h 0m"}, {7_500_000, "2h 5m"}, {86_400_000, "1d 0h"}, {4*86_400_000 + 3*3_600_000, "4d 3h"}}
	for _, tc := range ttl {
		if got := FormatTTL(tc.ms); got != tc.want {
			t.Errorf("FormatTTL(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
	bytes := []struct {
		n    int64
		want string
	}{{-1, "?"}, {0, "0 B"}, {1023, "1023 B"}, {1024, "1.0 KB"}, {1536, "1.5 KB"}, {5 << 20, "5.0 MB"}, {3 << 30, "3.0 GB"}}
	for _, tc := range bytes {
		if got := FormatBytes(tc.n); got != tc.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
	commas := []struct {
		n    int64
		want string
	}{{0, "0"}, {999, "999"}, {1000, "1,000"}, {1234567, "1,234,567"}, {-12345, "-12,345"}}
	for _, tc := range commas {
		if got := Commas(tc.n); got != tc.want {
			t.Errorf("Commas(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
	if plural(1, "key", "keys") != "1 key" || plural(0, "key", "keys") != "0 keys" || plural(12345, "key", "keys") != "12,345 keys" {
		t.Error("plural")
	}
}

func TestPattern(t *testing.T) {
	tests := []struct{ in, want string }{{"", ""}, {"  ", ""}, {"user", "*user*"}, {"user:*", "user:*"}, {"a?c", "a?c"}, {"[ab]x", "[ab]x"}, {`a\*b`, `a\*b`}, {" cache ", "*cache*"}}
	for _, tc := range tests {
		if got := Pattern(tc.in); got != tc.want {
			t.Errorf("Pattern(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseTTLSeconds(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{{"60", time.Minute, true}, {" 5 ", 5 * time.Second, true}, {"0", 0, false}, {"-1", 0, false}, {"x", 0, false}, {"1.5", 0, false}, {"", 0, false},
		{"99999999999999999999", 0, false}, {"9999999999999", 0, false}}
	for _, tc := range tests {
		got, err := ParseTTLSeconds(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParseTTLSeconds(%q) = %v, %v", tc.in, got, err)
		}
		if err != nil {
			wantRoute(t, err, "ParseTTLSeconds.RedisTTLInvalid")
		}
	}
}
