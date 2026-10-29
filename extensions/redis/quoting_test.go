package redis

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestSplitArgsReadsACommandLineAsRedisCliDoes(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"empty", "", nil},
		{"white space only", " \t\n ", nil},
		{"words", "GET key", []string{"GET", "key"}},
		{"more white space", "  SET   k \t v  ", []string{"SET", "k", "v"}},
		{"double quotes keep spaces", `SET k "hello world"`, []string{"SET", "k", "hello world"}},
		{"an empty argument", `SET k ""`, []string{"SET", "k", ""}},
		{"escapes", `SET k "a\nb\tc\rd\be\af"`, []string{"SET", "k", "a\nb\tc\rd\be\x07f"}},
		{"an escaped quote and backslash", `SET k "say \"hi\" \\ done"`, []string{"SET", "k", `say "hi" \ done`}},
		{"hex escapes", `SET k "\x41\x42\xff"`, []string{"SET", "k", "AB\xff"}},
		{"a hex escape needs two digits", `SET k "\x4"`, []string{"SET", "k", "x4"}},
		{"any other backslash is the character", `SET k "\q\ "`, []string{"SET", "k", "q "}},
		{"single quotes keep everything but the escaped quote", `SET k 'a\nb \'c\' "d"'`, []string{"SET", "k", `a\nb 'c' "d"`}},
		{"quotes inside a word", `SET k a"b c"`, []string{"SET", "k", "ab c"}},
		{"two quoted words", `ECHO "a" 'b'`, []string{"ECHO", "a", "b"}},
		{"UTF-8", `SET k "héllo ✓"`, []string{"SET", "k", "héllo ✓"}},
		{"a pound sign is not a comment here", "SET k #x", []string{"SET", "k", "#x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SplitArgs(tc.line)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSplitArgsRefusesWhatRedisCliRefuses(t *testing.T) {
	tests := []struct {
		line, route string
	}{
		{`SET k "open`, "SplitArgs.RedisQuoteUnbalanced"},
		{`SET k 'open`, "SplitArgs.RedisQuoteUnbalanced"},
		{`SET k "end with a backslash\`, "SplitArgs.RedisQuoteUnbalanced"},
		{`SET k "a"b`, "SplitArgs.RedisQuoteSpacing"},
		{`SET k 'a'b`, "SplitArgs.RedisQuoteSpacing"},
		{`SET k "a""b"`, "SplitArgs.RedisQuoteSpacing"},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			got, err := SplitArgs(tc.line)
			if got != nil {
				t.Errorf("got %q with an error", got)
			}
			wantRoute(t, err, tc.route)
		})
	}
}

func TestQuoteArgAndSplitArgsAreInverses(t *testing.T) {
	fixed := []string{"", "a", "a b", `a"b`, `a'b`, `a\b`, "a\nb", "\x00\x01\xff", "héllo", "  ", "-", `"`, `'`, `\`, "x\\x41y"}
	for _, s := range fixed {
		got, err := SplitArgs(QuoteArg(s))
		if err != nil || len(got) != 1 || got[0] != s {
			t.Errorf("%q: quoted %q, read %q, %v", s, QuoteArg(s), got, err)
		}
	}
	// seeded random arguments: a command line made of quoted arguments reads back the arguments
	rng := rand.New(rand.NewSource(21))
	for i := 0; i < 2000; i++ {
		n := rng.Intn(6)
		args := make([]string, n)
		var line string
		for j := range args {
			b := make([]byte, rng.Intn(12))
			for k := range b {
				switch rng.Intn(4) {
				case 0:
					b[k] = byte(rng.Intn(256))
				case 1:
					b[k] = "\"'\\ \n\t"[rng.Intn(6)]
				default:
					b[k] = byte('a' + rng.Intn(26))
				}
			}
			args[j] = string(b)
			if j > 0 {
				line += " "
			}
			line += QuoteArg(args[j])
		}
		got, err := SplitArgs(line)
		if err != nil || (len(args) > 0 || len(got) > 0) && !reflect.DeepEqual(got, args) {
			t.Fatalf("iteration %d: %q read as %q (%v), want %q", i, line, got, err, args)
		}
	}
}

func TestSplitArgsNeverPanicsOnRandomText(t *testing.T) {
	rng := rand.New(rand.NewSource(22))
	alphabet := []byte("abc \t\n\"'\\x0123456789ABCDEFxX#\x00\xff")
	for i := 0; i < 5000; i++ {
		b := make([]byte, rng.Intn(30))
		for k := range b {
			b[k] = alphabet[rng.Intn(len(alphabet))]
		}
		args, err := SplitArgs(string(b))
		if err != nil && args != nil {
			t.Fatalf("%q: arguments %q with an error", b, args)
		}
	}
}
