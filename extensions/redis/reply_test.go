package redis

import (
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestConvertReplyReadsTheValuesOfGoRedis(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		v     any
		resp3 bool
		want  Reply
	}{
		{"nil", nil, nil, false, Reply{Kind: ReplyNil}},
		{"a status word", nil, "OK", false, Reply{Kind: ReplyStatus, Str: "OK"}},
		{"PONG", nil, "PONG", false, Reply{Kind: ReplyStatus, Str: "PONG"}},
		{"a bulk string", nil, "hello", false, Reply{Kind: ReplyString, Str: "hello"}},
		{"bytes", nil, []byte("hi"), false, Reply{Kind: ReplyString, Str: "hi"}},
		{"an integer", nil, int64(42), false, Reply{Kind: ReplyInt, Int: 42}},
		{"a double (RESP3)", nil, 3.5, true, Reply{Kind: ReplyDouble, Float: 3.5}},
		{"a boolean (RESP3)", nil, true, true, Reply{Kind: ReplyBool, Bool: true}},
		{"a big number (RESP3)", nil, big.NewInt(0).Exp(big.NewInt(10), big.NewInt(30), nil), true, Reply{Kind: ReplyBigNumber, Str: "1000000000000000000000000000000"}},
		{"an array", []string{"LRANGE"}, []any{"a", int64(1), nil}, false,
			Reply{Kind: ReplyArray, Items: []Reply{{Kind: ReplyString, Str: "a"}, {Kind: ReplyInt, Int: 1}, {Kind: ReplyNil}}}},
		{"SMEMBERS in RESP2 is an array", []string{"smembers", "s"}, []any{"a"}, false, Reply{Kind: ReplyArray, Items: []Reply{{Kind: ReplyString, Str: "a"}}}},
		{"SMEMBERS in RESP3 is a set", []string{"smembers", "s"}, []any{"a"}, true, Reply{Kind: ReplySet, Items: []Reply{{Kind: ReplyString, Str: "a"}}}},
		{"LRANGE in RESP3 stays an array", []string{"LRANGE", "l", "0", "-1"}, []any{"a"}, true, Reply{Kind: ReplyArray, Items: []Reply{{Kind: ReplyString, Str: "a"}}}},
		{"a map is sorted by its keys", nil, map[any]any{"b": int64(2), "a": int64(1)}, true,
			Reply{Kind: ReplyMap, Pairs: []ReplyPair{
				{Key: Reply{Kind: ReplyString, Str: "a"}, Value: Reply{Kind: ReplyInt, Int: 1}},
				{Key: Reply{Kind: ReplyString, Str: "b"}, Value: Reply{Kind: ReplyInt, Int: 2}}}}},
		{"an error inside an array", nil, []any{errors.New("TestBoom", "WRONGTYPE bad")}, false,
			Reply{Kind: ReplyArray, Items: []Reply{{Kind: ReplyError, Str: "WRONGTYPE bad"}}}},
		{"something else", nil, struct{ A int }{3}, false, Reply{Kind: ReplyString, Str: "{3}"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ConvertReply(tc.args, tc.v, tc.resp3)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func str(s string) Reply   { return Reply{Kind: ReplyString, Str: s} }
func num(n int64) Reply    { return Reply{Kind: ReplyInt, Int: n} }
func arr(r ...Reply) Reply { return Reply{Kind: ReplyArray, Items: r} }

func TestFormatReplyWritesRepliesAsRedisCliDoes(t *testing.T) {
	tests := []struct {
		name string
		r    Reply
		want string
	}{
		{"nil", Reply{Kind: ReplyNil}, "(nil)"},
		{"status", Reply{Kind: ReplyStatus, Str: "OK"}, "OK"},
		{"error", Reply{Kind: ReplyError, Str: "ERR unknown command 'x'"}, "(error) ERR unknown command 'x'"},
		{"integer", num(-7), "(integer) -7"},
		{"string", str("hello"), `"hello"`},
		{"an empty string", str(""), `""`},
		{"escapes", str("a\"b\\c\td\x07\x08\x00\x7f"), `"a\"b\\c\td\a\b\x00\x7f"`},
		{"binary data with a line break stays quoted", str("a\n\xff"), `"a\n\xff"`},
		{"a value with a line break is shown as text", str("a\nb"), "a\nb"},
		{"UTF-8 stays", str("héllo ✓"), `"héllo ✓"`},
		{"invalid UTF-8 is escaped", str("a\xffb\xc3"), `"a\xffb\xc3"`},
		{"double", Reply{Kind: ReplyDouble, Float: 1.5}, "(double) 1.5"},
		{"infinity", Reply{Kind: ReplyDouble, Float: math.Inf(-1)}, "(double) -inf"},
		{"nan", Reply{Kind: ReplyDouble, Float: math.NaN()}, "(double) nan"},
		{"true", Reply{Kind: ReplyBool, Bool: true}, "(true)"},
		{"false", Reply{Kind: ReplyBool}, "(false)"},
		{"big number", Reply{Kind: ReplyBigNumber, Str: "123456789012345678901234567890"}, "(big number) 123456789012345678901234567890"},
		{"empty array", arr(), "(empty array)"},
		{"empty set", Reply{Kind: ReplySet}, "(empty set)"},
		{"empty map", Reply{Kind: ReplyMap}, "(empty hash)"},
		{"array", arr(str("a"), num(2), Reply{Kind: ReplyNil}), "1) \"a\"\n2) (integer) 2\n3) (nil)"},
		{"nested arrays are indented under their number", arr(arr(str("a"), str("b")), str("c")), "1) 1) \"a\"\n   2) \"b\"\n2) \"c\""},
		{"set", Reply{Kind: ReplySet, Items: []Reply{str("a"), str("b")}}, "1~ \"a\"\n2~ \"b\""},
		{"map", Reply{Kind: ReplyMap, Pairs: []ReplyPair{{str("name"), str("Ada")}, {str("age"), num(36)}}},
			"1# \"name\" => \"Ada\"\n2# \"age\" => (integer) 36"},
		{"a map with an array", Reply{Kind: ReplyMap, Pairs: []ReplyPair{{str("tags"), arr(str("x"), str("y"))}, {str("n"), arr()}}},
			"1# \"tags\" => 1) \"x\"\n             2) \"y\"\n2# \"n\" => (empty array)"},
		{"ten elements align their numbers", func() Reply {
			r := arr()
			for i := 0; i < 10; i++ {
				r.Items = append(r.Items, num(int64(i)))
			}
			return r
		}(), " 1) (integer) 0\n 2) (integer) 1\n 3) (integer) 2\n 4) (integer) 3\n 5) (integer) 4\n 6) (integer) 5\n 7) (integer) 6\n 8) (integer) 7\n 9) (integer) 8\n10) (integer) 9"},
		{"a text of several lines is shown as it is", str("# Server\r\nredis_version:8.2.2\r\n\r\n# Clients\r\n"), "# Server\nredis_version:8.2.2\n\n# Clients"},
		{"an error inside an array", arr(Reply{Kind: ReplyError, Str: "WRONGTYPE x"}, str("ok")), "1) (error) WRONGTYPE x\n2) \"ok\""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(FormatReply(tc.r), "\n"); got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestFormatReplyIsBounded(t *testing.T) {
	t.Run("elements", func(t *testing.T) {
		r := arr()
		for i := 0; i < MaxReplyItems+25; i++ {
			r.Items = append(r.Items, num(int64(i)))
		}
		lines := FormatReply(r)
		if len(lines) != MaxReplyItems+1 || lines[len(lines)-1] != "... 25 more elements" {
			t.Errorf("%d lines, last %q", len(lines), lines[len(lines)-1])
		}
	})
	t.Run("entries", func(t *testing.T) {
		r := Reply{Kind: ReplyMap}
		for i := 0; i < MaxReplyItems+1; i++ {
			r.Pairs = append(r.Pairs, ReplyPair{str("k"), num(1)})
		}
		lines := FormatReply(r)
		if len(lines) != MaxReplyItems+1 || lines[len(lines)-1] != "... 1 more entry" {
			t.Errorf("%d lines, last %q", len(lines), lines[len(lines)-1])
		}
	})
	t.Run("a long string", func(t *testing.T) {
		lines := FormatReply(str(strings.Repeat("x", MaxReplyString+100)))
		if len(lines) != 1 || !strings.HasSuffix(lines[0], `" ... (100 more bytes)`) || len(lines[0]) > MaxReplyString+60 {
			t.Errorf("a string of %d bytes gives %d bytes", MaxReplyString+100, len(lines[0]))
		}
	})
	t.Run("a long text", func(t *testing.T) {
		lines := FormatReply(str(strings.Repeat("line\n", MaxReplyLines+10)))
		if len(lines) != MaxReplyLines+1 || !strings.HasPrefix(lines[len(lines)-1], "... ") {
			t.Errorf("%d lines, last %q", len(lines), lines[len(lines)-1])
		}
	})
	t.Run("the lines of nested replies", func(t *testing.T) {
		inner := arr()
		for i := 0; i < 500; i++ {
			inner.Items = append(inner.Items, num(int64(i)))
		}
		outer := arr()
		for i := 0; i < 30; i++ {
			outer.Items = append(outer.Items, inner)
		}
		lines := FormatReply(outer)
		if len(lines) > MaxReplyLines+1 {
			t.Errorf("%d lines", len(lines))
		}
	})
}
