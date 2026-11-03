package javascript

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// feedTAP reads a text line by line and returns the results and the output.
func feedTAP(text string) (results []tapResult, output []string) {
	var p tapParser
	take := func(evs []tapEvent) {
		for _, ev := range evs {
			if ev.IsOutput {
				output = append(output, ev.Output)
			} else {
				results = append(results, *ev.Result)
			}
		}
	}
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		take(p.Feed(l))
	}
	take(p.Flush())
	return results, output
}

// summary writes a result as "path type ok|not ok|skip".
func (r tapResult) summary() string {
	state := "not ok"
	switch {
	case r.OK && r.Skip:
		state = "skip"
	case r.OK:
		state = "ok"
	}
	return strings.Join(r.Path, " > ") + " [" + r.Type + "] " + state
}

func TestTAPParserReadsTheRecordedRuns(t *testing.T) {
	for _, tc := range []struct {
		file string
		want []string
		out  []string
	}{
		{"node_math.tap", []string{
			"math > adds [test] ok", "math > divides [test] not ok", "math > later [test] skip", "math > nested > throws on zero [test] ok",
			"math > nested [suite] ok", "math [suite] not ok", "prints and passes [test] ok",
		}, []string{"hello from a passing test", "tests 5", "suites 2", "pass 3", "fail 1", "cancelled 0", "skipped 1", "todo 0"}},
		{"node_other.tap", []string{"other passes [test] ok", "other crashes [test] not ok"}, nil},
		{"node_filtered.tap", []string{"math > adds [test] ok", "math > nested > throws on zero [test] ok", "math > nested [suite] ok", "math [suite] ok"}, nil},
		{"node_broken.tap", []string{"test/broken.test.js [test] not ok"}, nil},
	} {
		results, output := feedTAP(recorded(t, tc.file, "/p"))
		var got []string
		for _, r := range results {
			got = append(got, r.summary())
		}
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s results:\n%s\nwant:\n%s", tc.file, strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
		}
		for _, o := range tc.out {
			if !strings.Contains(strings.Join(output, "\n"), o) {
				t.Errorf("%s: no output %q in %q", tc.file, o, output)
			}
		}
	}
}

func TestTAPParserReadsAFailure(t *testing.T) {
	results, _ := feedTAP(recorded(t, "node_math.tap", "/p"))
	var div tapResult
	for _, r := range results {
		if strings.HasSuffix(r.summary(), "math > divides [test] not ok") {
			div = r
		}
	}
	if div.FailureType != "testCodeFailure" || div.Code != "ERR_ASSERTION" || div.Expected != "4" || div.Actual != "5" || !div.HasSides ||
		div.Location != "/p/test/math.test.js:10:3" || div.Duration <= 0 {
		t.Errorf("%+v", div)
	}
	if !strings.HasPrefix(div.Error, "Expected values to be strictly equal:") || !strings.Contains(div.Error, "5 !== 4") {
		t.Errorf("error %q", div.Error)
	}
	r, ok := div.toResult("/p/test/math.test.js")
	if !ok || r.State != stateFailed || len(r.Failures) != 1 {
		t.Fatalf("%+v", r)
	}
	f := r.Failures[0]
	if f.Expected != "4" || f.Actual != "5" || len(f.Frames) == 0 || f.Frames[0].File != "/p/test/math.test.js" || f.Frames[0].Line != 11 || f.Frames[0].Col != 12 {
		t.Errorf("failure %+v", f)
	}
	for _, fr := range f.Frames {
		if strings.HasPrefix(fr.File, "node:") {
			t.Errorf("a frame of node itself: %+v", fr)
		}
	}
}

func TestTAPParserLines(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       []string
		output     []string
	}{
		{"a skip directive", "ok 1 - later # SKIP\n", []string{"later [] skip"}, nil},
		{"a todo directive with a reason", "ok 1 - maybe # TODO not yet\n", []string{"maybe [] skip"}, nil},
		{"a hash in a name", "ok 1 - issue \\#12 fixed\n", []string{"issue #12 fixed [] ok"}, nil},
		{"a result without a block", "# Subtest: a\nok 1 - a\n# Subtest: b\nnot ok 2 - b\n", []string{"a [] ok", "b [] not ok"}, nil},
		{"levels", "# Subtest: s\n    # Subtest: t\n    ok 1 - t\n      ---\n      type: 'test'\n      ...\nok 1 - s\n  ---\n  type: 'suite'\n  ...\n", []string{"s > t [test] ok", "s [suite] ok"}, nil},
		{"plan and version are not output", "TAP version 13\n1..1\n  1..2\n", nil, nil},
		{"text is output", "plain\n# comment\n#\n", nil, []string{"plain", "comment", ""}},
		{"a block that is cut off", "not ok 1 - a\n  ---\n  error: 'x'\n", []string{"a [] not ok"}, nil},
		{"quotes", "not ok 1 - a\n  ---\n  error: 'it''s'\n  code: \"E\"\n  ...\n", []string{"a [] not ok"}, nil},
	} {
		results, output := feedTAP(tc.text)
		var got []string
		for _, r := range results {
			got = append(got, r.summary())
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") || strings.Join(output, "|") != strings.Join(tc.output, "|") {
			t.Errorf("%s: results %q output %q, want %q %q", tc.name, got, output, tc.want, tc.output)
		}
	}
	results, _ := feedTAP("not ok 1 - a\n  ---\n  error: 'it''s'\n  code: \"E\"\n  expected: 'x'\n  actual: 'y'\n  ...\n")
	if r := results[0]; r.Error != "it's" || r.Code != "E" || r.Expected != "x" || r.Actual != "y" {
		t.Errorf("%+v", r)
	}
}

func TestTAPResultsBecomeResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  tapResult
		want string
		skip bool
	}{
		{"passed", tapResult{Path: []string{"a"}, OK: true, Duration: 1.5}, statePassed, false},
		{"skipped", tapResult{Path: []string{"a"}, OK: true, Skip: true, Duration: -1}, stateSkipped, false},
		{"failed", tapResult{Path: []string{"a"}, Error: "x", Duration: 2}, stateFailed, false},
		{"cancelled by its parent", tapResult{Path: []string{"a"}, FailureType: "cancelledByParent", Duration: -1}, stateErrored, false},
		{"a suite", tapResult{Path: []string{"a"}, OK: true, Type: "suite"}, "", true},
	} {
		r, ok := tc.res.toResult("/f")
		if ok == tc.skip || r.State != tc.want {
			t.Errorf("%s: %+v %v", tc.name, r, ok)
		}
	}
	r, _ := tapResult{Path: []string{"a"}, OK: true, Duration: -1}.toResult("/f")
	if r.Duration != extension.TestNoDuration {
		t.Errorf("duration %v", r.Duration)
	}
	// a failure with no stack points at the test
	r, _ = tapResult{Path: []string{"a"}, Location: "/p/a.test.js:8:1", Duration: -1}.toResult("/f")
	if len(r.Failures) != 1 || r.Failures[0].Text != "The test failed." || len(r.Failures[0].Frames) != 1 || r.Failures[0].Frames[0].Line != 8 {
		t.Errorf("%+v", r.Failures)
	}
}
