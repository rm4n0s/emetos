package rust

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The parser of libtest's output (step 26.7): a table of outputs to the events the parser reports, and a table of failures to panics.

// show writes an event as one short text.
func show(ev libtestEvent) string {
	switch ev.Kind {
	case evRunning:
		return "running " + strconv.Itoa(ev.Count)
	case evResult:
		s := "result " + ev.Name + " "
		s += map[outcome]string{outcomeOK: "ok", outcomeFailed: "FAILED", outcomeIgnored: "ignored"}[ev.Outcome]
		if ev.Reason != "" {
			s += "(" + ev.Reason + ")"
		}
		if ev.ShouldPanic {
			s += " should-panic"
		}
		return s
	case evBlock:
		kind := "failure"
		if ev.Success {
			kind = "success"
		}
		return kind + " " + ev.Name + " [" + strings.ReplaceAll(ev.Text, "\n", "|") + "]"
	case evSummary:
		return "summary ok=" + strconv.FormatBool(ev.Ok) + " passed=" + strconv.Itoa(ev.Passed) + " failed=" + strconv.Itoa(ev.Failed) +
			" ignored=" + strconv.Itoa(ev.Ignored) + " filtered=" + strconv.Itoa(ev.Filtered) + " s=" + strconv.FormatFloat(ev.Seconds, 'f', 2, 64)
	}
	return "line " + ev.Text
}

func parseAll(text string) []string {
	var p libtestParser
	var out []string
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		for _, ev := range p.Feed(l) {
			out = append(out, show(ev))
		}
	}
	for _, ev := range p.Flush() {
		out = append(out, show(ev))
	}
	return out
}

func TestTheParserReadsLibtestOutput(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     []string
	}{
		{"all ok", `
   Compiling x v0.1.0
running 2 tests
test a ... ok
test m::b ... ok

test result: ok. 2 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s
`, []string{"line    Compiling x v0.1.0", "line running 2 tests", "running 2", "result a ok", "result m::b ok", "line ",
			"line test result: ok. 2 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s", "summary ok=true passed=2 failed=0 ignored=0 filtered=0 s=0.01"}},
		{"failed with the failures section", `running 2 tests
test a ... ok
test b ... FAILED

failures:

---- b stdout ----

thread 'b' panicked at src/lib.rs:3:5:
assertion failed: x

failures:
    b

test result: FAILED. 1 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s
`, []string{"line running 2 tests", "running 2", "result a ok", "result b FAILED", "line ", "failure b [|thread 'b' panicked at src/lib.rs:3:5:|assertion failed: x]",
			"line test result: FAILED. 1 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s", "summary ok=false passed=1 failed=1 ignored=0 filtered=0 s=0.00"}},
		{"ignored with and without a reason", `test a ... ignored
test b ... ignored, needs a network
test c ... ok
`, []string{"result a ignored", "result b ignored(needs a network)", "result c ok"}},
		{"should_panic", `test z - should panic ... ok
test y - should panic ... FAILED

failures:

---- y stdout ----
note: test did not panic as expected

failures:
    y
`, []string{"result z ok should-panic", "result y FAILED should-panic", "line ", "failure y [note: test did not panic as expected]"}},
		{"a panic in a thread", `test t ... FAILED

failures:

---- t stdout ----

thread '<unnamed>' (7) panicked at src/lib.rs:61:13:
worker failed

thread 't' (6) panicked at src/lib.rs:63:9:
the worker thread panicked


failures:
    t
`, []string{"result t FAILED", "line ", "failure t [|thread '<unnamed>' (7) panicked at src/lib.rs:61:13:|worker failed||thread 't' (6) panicked at src/lib.rs:63:9:|the worker thread panicked]"}},
		{"doc tests", `   Doc-tests calc

running 2 tests
test src/lib.rs - add (line 5) ... ok
test src/lib.rs - Foo::bar (line 20) ... FAILED

failures:

---- src/lib.rs - Foo::bar (line 20) stdout ----
Test executable failed (exit status: 101).

failures:
    src/lib.rs - Foo::bar (line 20)
`, []string{"line    Doc-tests calc", "line ", "line running 2 tests", "running 2", "result src/lib.rs - add (line 5) ok", "result src/lib.rs - Foo::bar (line 20) FAILED", "line ",
			"failure src/lib.rs - Foo::bar (line 20) [Test executable failed (exit status: 101).]"}},
		{"show-output successes", `test a ... ok
test b ... ok

successes:

---- a stdout ----
hello

successes:
    a
    b

test result: ok. 2 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out
`, []string{"result a ok", "result b ok", "line ", "success a [hello]", "line test result: ok. 2 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out",
			"summary ok=true passed=2 failed=0 ignored=0 filtered=0 s=0.00"}},
		{"empty sections in a row", `test a ... FAILED

successes:

successes:

failures:

---- a stdout ----
boom

failures:
    a
`, []string{"result a FAILED", "line ", "failure a [boom]"}},
		{"a name with three dots", `test odd ... name ... ok
`, []string{"result odd ... name ok"}},
		{"cargo's own lines and a line that only looks like a result", `    Finished test profile
     Running unittests src/lib.rs (target/debug/deps/x-1)
test result is great
test a ... nonsense
error: test failed, to rerun pass ` + "`--lib`" + `
`, []string{"line     Finished test profile", "line      Running unittests src/lib.rs (target/debug/deps/x-1)", "line test result is great", "line test a ... nonsense",
			"line error: test failed, to rerun pass `--lib`"}},
		{"a failure at the end of the output with no list", `test a ... FAILED

failures:

---- a stdout ----
cut off
`, []string{"result a FAILED", "line ", "failure a [cut off]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// the first case starts with a blank line the table keeps for readability
			got := parseAll(strings.TrimPrefix(tc.in, "\n"))
			if !slices.Equal(got, tc.want) {
				t.Errorf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestPanicsAreFoundInTheOutputOfAFailure(t *testing.T) {
	type want struct {
		thread, file string
		line, col    int
		text         string
		cmp          bool
		left, right  string
		frames       int
	}
	for _, tc := range []struct {
		name, in string
		want     []want
	}{
		{"assert_eq", "\nthread 'm::t' (9) panicked at src/lib.rs:43:9:\nassertion `left == right` failed\n  left: 5\n right: 4\nnote: run with `RUST_BACKTRACE=1` environment variable to display a backtrace\n",
			[]want{{"m::t", "src/lib.rs", 43, 9, "assertion `left == right` failed\n  left: 5\n right: 4", true, "5", "4", 0}}},
		{"assert_eq with a message", "thread 't' panicked at src/a.rs:2:5:\nassertion `left == right` failed: the totals differ\n  left: \"a\"\n right: \"b\"\n",
			[]want{{"t", "src/a.rs", 2, 5, "assertion `left == right` failed: the totals differ\n  left: \"a\"\n right: \"b\"", true, "\"a\"", "\"b\"", 0}}},
		{"assert_eq with sides of several lines", "thread 't' panicked at src/a.rs:2:5:\nassertion `left == right` failed\n  left: \"x\n\ny\"\n right: \"x\nz\"\n\nfailures:\n",
			[]want{{"t", "src/a.rs", 2, 5, "assertion `left == right` failed\n  left: \"x\n\ny\"\n right: \"x\nz\"", true, "\"x\n\ny\"", "\"x\nz\"", 0}}},
		{"assert_ne is not a comparison", "thread 't' panicked at src/a.rs:2:5:\nassertion `left != right` failed\n  left: 1\n right: 1\n",
			[]want{{"t", "src/a.rs", 2, 5, "assertion `left != right` failed\n  left: 1\n right: 1", false, "", "", 0}}},
		{"assert and a plain panic", "thread 'a' panicked at src/a.rs:2:5:\nassertion failed: x > 1\n\nthread 'a' panicked at src/a.rs:9:1:\nboom\n",
			[]want{{"a", "src/a.rs", 2, 5, "assertion failed: x > 1", false, "", "", 0}, {"a", "src/a.rs", 9, 1, "boom", false, "", "", 0}}},
		{"a backtrace", "thread 'a' panicked at src/a.rs:2:5:\nboom\nstack backtrace:\n   0: rust_begin_unwind\n             at /rustc/abc/library/std/src/panicking.rs:652:5\n   1: a::f\n             at ./src/a.rs:2:5\n   2: core::ops::function::FnOnce::call_once\n",
			[]want{{"a", "src/a.rs", 2, 5, "boom", false, "", "", 3}}},
		{"the format before 1.73", "thread 'a' panicked at 'boom', src/a.rs:2:5\nnote: run with `RUST_BACKTRACE=1`\n",
			[]want{{"a", "src/a.rs", 2, 5, "boom", false, "", "", 0}}},
		{"the old assert_eq", "thread 'a' panicked at 'assertion failed: `(left == right)`\n  left: `5`,\n right: `4`', src/a.rs:2:5\n",
			[]want{{"a", "src/a.rs", 2, 5, "assertion failed: `(left == right)`\n  left: `5`,\n right: `4`", true, "5", "4", 0}}},
		{"a doc test", "Test executable failed (exit status: 101).\n\nstderr:\n\nthread 'main' (1) panicked at src/lib.rs:5:1:\nassertion `left == right` failed\n  left: 5\n right: 4\n",
			[]want{{"main", "src/lib.rs", 5, 1, "assertion `left == right` failed\n  left: 5\n right: 4", true, "5", "4", 0}}},
		{"no panic", "note: test did not panic as expected\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePanics(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("%d panics: %+v", len(got), got)
			}
			for i, w := range tc.want {
				g := got[i]
				if g.Thread != w.thread || g.File != w.file || g.Line != w.line || g.Col != w.col || g.Text != w.text || g.Cmp != w.cmp || g.Left != w.left || g.Right != w.right || len(g.Frames) != w.frames {
					t.Errorf("panic %d = %+v\nwant %+v", i, g, w)
				}
			}
		})
	}
	// the frames of a backtrace
	got := parsePanics("thread 'a' panicked at src/a.rs:2:5:\nboom\nstack backtrace:\n   0: x\n             at /rustc/abc/std.rs:6:5\n   1: a::f\n             at ./src/a.rs:2:5\n")
	if len(got) != 1 || len(got[0].Frames) != 2 || got[0].Frames[1] != (frame{Name: "a::f", File: "./src/a.rs", Line: 2, Col: 5}) {
		t.Errorf("frames %+v", got)
	}
}

func TestACompilerErrorHasAPlace(t *testing.T) {
	for _, tc := range []struct {
		in        string
		file      string
		line, col int
		ok        bool
	}{
		{"error[E0425]: nope\n  --> src/lib.rs:44:20\n   |\n", "src/lib.rs", 44, 20, true},
		{"error: no place\n", "", 0, 0, false},
	} {
		file, line, col, ok := compilerPlace(tc.in)
		if file != tc.file || line != tc.line || col != tc.col || ok != tc.ok {
			t.Errorf("compilerPlace(%q) = %q %d %d %v", tc.in, file, line, col, ok)
		}
	}
}
