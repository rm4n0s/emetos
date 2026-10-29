package resultgrid

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestValueDisplayAndFull(t *testing.T) {
	long := strings.Repeat("abcdefghij", 30) // 300 characters
	when := time.Date(2026, 10, 29, 13, 45, 7, 123456000, time.FixedZone("EEST", 3*3600))
	tests := []struct {
		name    string
		v       Value
		display string
		cut     bool
		full    string
	}{
		{"null", Null(), "NULL", false, "NULL"},
		{"text", Text("hello"), "hello", false, "hello"},
		{"empty text", Text(""), "", false, ""},
		{"a text with a line break", Text("a\nb"), "a b", true, "a\nb"},
		{"a text with a tab and a return", Text("a\tb\r\nc"), "a b  c", true, "a\tb\r\nc"},
		{"a long text", Text(long), long[:200] + "…", true, long},
		{"a text of exactly the limit", Text(long[:200]), long[:200], false, long[:200]},
		{"unicode is cut by characters", Text(strings.Repeat("日", 250)), strings.Repeat("日", 200) + "…", true, strings.Repeat("日", 250)},
		{"json", JSON(`{"a": 1}`), `{"a": 1}`, false, `{"a": 1}`},
		{"int", Int(-42), "-42", false, "-42"},
		{"a big int keeps its digits", Int(math.MaxInt64), "9223372036854775807", false, "9223372036854775807"},
		{"float", Float(0.1), "0.1", false, "0.1"},
		{"float with exponent", Float(1e21), "1e+21", false, "1e+21"},
		{"nan", Float(math.NaN()), "NaN", false, "NaN"},
		{"inf", Float(math.Inf(-1)), "-Inf", false, "-Inf"},
		{"true", Bool(true), "true", false, "true"},
		{"false", Bool(false), "false", false, "false"},
		{"short bytes", Bytes([]byte{0xde, 0xad, 0xbe, 0xef}), "0xdeadbeef", false, "0xdeadbeef"},
		{"no bytes", Bytes(nil), "0x", false, "0x"},
		{"long bytes", Bytes(make([]byte, 40)), "0x" + strings.Repeat("00", 16) + "… (40 bytes)", true, "0x" + strings.Repeat("00", 40)},
		{"kilobytes", Bytes(make([]byte, 2048)), "0x" + strings.Repeat("00", 16) + "… (2.0 KB)", true, "0x" + strings.Repeat("00", 2048)},
		{"a time keeps its zone", Time(when), "2026-10-29T13:45:07.123456+03:00", false, "2026-10-29T13:45:07.123456+03:00"},
		{"a time in UTC", Time(time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)), "1999-12-31T23:59:59Z", false, "1999-12-31T23:59:59Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cut := tt.v.Display(DisplayOptions{})
			if got != tt.display || cut != tt.cut {
				t.Errorf("Display = %q, %v; want %q, %v", got, cut, tt.display, tt.cut)
			}
			if full := tt.v.Full(""); full != tt.full {
				t.Errorf("Full = %q, want %q", full, tt.full)
			}
		})
	}
	if got, _ := Null().Display(DisplayOptions{NullText: "∅"}); got != "∅" {
		t.Errorf("NullText: %q", got)
	}
	if got := Null().Full("(null)"); got != "(null)" {
		t.Errorf("Full NullText: %q", got)
	}
	if got, cut := Text(long).Display(DisplayOptions{MaxRunes: 10}); got != long[:10]+"…" || !cut {
		t.Errorf("MaxRunes: %q %v", got, cut)
	}
}

func TestTipHoldsMoreOfALongValueAndSaysSo(t *testing.T) {
	short := Text("a\nb")
	if got := short.Tip(""); got != "a\nb" {
		t.Errorf("Tip = %q", got)
	}
	long := Text(strings.Repeat("x", MaxTipRunes+500))
	tip := long.Tip("")
	if !strings.HasPrefix(tip, strings.Repeat("x", MaxTipRunes)+"…") || !strings.Contains(tip, "2500 characters") {
		t.Errorf("Tip of a long value = %.60q ... %q", tip, tip[len(tip)-60:])
	}
}

func TestCompareOrdersValues(t *testing.T) {
	when := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		a, b Value
		want int
	}{
		{"null equals null", Null(), Null(), 0},
		{"null first", Null(), Int(-5), -1},
		{"null first, reversed", Text(""), Null(), 1},
		{"ints", Int(2), Int(10), -1},
		{"ints equal", Int(7), Int(7), 0},
		{"a big int is not lost to a float", Int(math.MaxInt64), Int(math.MaxInt64 - 1), 1},
		{"int and float", Int(2), Float(2.5), -1},
		{"float and int equal", Float(3), Int(3), 0},
		{"nan before numbers", Float(math.NaN()), Float(-1e300), -1},
		{"nan with nan", Float(math.NaN()), Float(math.NaN()), 0},
		{"text ignores case", Text("apple"), Text("Banana"), -1},
		{"text ignores case, reversed", Text("Banana"), Text("apple"), 1},
		{"text that differs in case is ordered by its bytes", Text("Bob"), Text("bob"), -1},
		{"a prefix is smaller", Text("ab"), Text("abc"), -1},
		{"unicode", Text("é"), Text("f"), 1},
		{"bool", Bool(false), Bool(true), -1},
		{"bytes", Bytes([]byte{1, 2}), Bytes([]byte{1, 3}), -1},
		{"times", Time(when), Time(when.Add(time.Second)), -1},
		{"times in other zones are the same instant", Time(when), Time(when.In(time.FixedZone("x", 7200))), 0},
		{"kinds that cannot be compared are ordered by kind", Bool(true), Text("a"), -1},
		{"text and bytes", Text("zzz"), Bytes([]byte{0}), -1},
		{"json is text", JSON("a"), Text("b"), -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Errorf("Compare = %d, want %d", got, tt.want)
			}
			if got := Compare(tt.b, tt.a); got != -tt.want {
				t.Errorf("Compare reversed = %d, want %d", got, -tt.want)
			}
		})
	}
}

func TestTSVFieldQuoting(t *testing.T) {
	tests := map[string]string{"plain": "plain", "": "", "a\tb": "\"a\tb\"", "a\nb": "\"a\nb\"", "say \"hi\"": "\"say \"\"hi\"\"\"", "a,b": "a,b"}
	for in, want := range tests {
		if got := tsvField(in); got != want {
			t.Errorf("tsvField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommasAndPlural(t *testing.T) {
	for n, want := range map[int]string{0: "0 rows", 1: "1 row", 2: "2 rows", 999: "999 rows", 1000: "1,000 rows", 100000: "100,000 rows", 1234567: "1,234,567 rows"} {
		if got := plural(n, "row"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}
