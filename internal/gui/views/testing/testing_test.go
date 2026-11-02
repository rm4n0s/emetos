package testing

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
)

// The Test Explorer's model (step 26.2): the formats, the filter text, the projections of the tree and the list, the sorting, the filter
// and the choice of what the buttons of the title bar run.

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d       time.Duration
		want    string
		summary string
	}{
		{500 * time.Microsecond, "0.5ms", "0 ms"},
		{3200 * time.Microsecond, "3.2ms", "3 ms"},
		{12 * time.Millisecond, "12ms", "12 ms"},
		{999 * time.Millisecond, "999ms", "999 ms"},
		{time.Second, "1.0s", "1.0 s"},
		{2150 * time.Millisecond, "2.1s", "2.1 s"},
		{90 * time.Second, "90.0s", "90.0 s"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := FormatDuration(tc.d); got != tc.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tc.d, got, tc.want)
			}
			if got := SummaryDuration(tc.d); got != tc.summary {
				t.Errorf("SummaryDuration(%v) = %q, want %q", tc.d, got, tc.summary)
			}
		})
	}
}

func TestProgressText(t *testing.T) {
	tests := []struct {
		name string
		s    Summary
		want string
	}{
		{"nothing run yet", Summary{Running: true}, "Running tests..."},
		{"running", Summary{Running: true, Passed: 2, RunSoFar: 2, TotalWillBeRun: 5}, "Running tests, 2/5 passed (100%)"},
		{"running with a failure", Summary{Running: true, Passed: 2, Failed: 1, RunSoFar: 3, TotalWillBeRun: 5}, "Running tests, 2/5 passed (66.7%)"},
		{"running with skipped", Summary{Running: true, Passed: 1, Skipped: 1, RunSoFar: 1, TotalWillBeRun: 4}, "Running tests, 1/4 tests passed (100%, 1 skipped)"},
		{"all passed", Summary{Passed: 5, RunSoFar: 5}, "5/5 tests passed (100%)"},
		{"3 of 5", Summary{Passed: 3, Failed: 2, RunSoFar: 5}, "3/5 tests passed (60%)"},
		{"skipped", Summary{Passed: 3, Failed: 2, Skipped: 1, RunSoFar: 5}, "3/5 tests passed (60%, 1 skipped)"},
		{"one of 3 is not 100", Summary{Passed: 99, Failed: 1, RunSoFar: 100}, "99/100 tests passed (99%)"},
		{"never 100 with a failure", Summary{Passed: 999, Failed: 1, RunSoFar: 1000}, "999/1000 tests passed (99.9%)"},
		{"none passed", Summary{Failed: 2, RunSoFar: 2}, "0/2 tests passed (0%)"},
		{"nothing ran", Summary{Skipped: 2}, "0/0 tests passed (0%, 2 skipped)"},
		{"a third", Summary{Passed: 1, Failed: 2, RunSoFar: 3}, "1/3 tests passed (33.3%)"},
		{"five percent", Summary{Passed: 1, Failed: 19, RunSoFar: 20}, "1/20 tests passed (5%)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProgressText(tc.s); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBadgeCount(t *testing.T) {
	s := Summary{Passed: 7, Failed: 2, Skipped: 1}
	tests := []struct {
		setting string
		want    int
		tip     string
	}{
		{"failed", 2, "2 failed tests"}, {"passed", 7, "7 passed tests"}, {"skipped", 1, "1 skipped tests"}, {"off", 0, "0 failed tests"}, {"", 0, "0 failed tests"},
	}
	for _, tc := range tests {
		t.Run(tc.setting, func(t *testing.T) {
			n := BadgeCount(s, tc.setting)
			if n != tc.want {
				t.Errorf("BadgeCount = %d, want %d", n, tc.want)
			}
			if got := BadgeTooltip(tc.setting, n); got != tc.tip {
				t.Errorf("BadgeTooltip = %q, want %q", got, tc.tip)
			}
		})
	}
}

func TestParseFilter(t *testing.T) {
	g := func(include bool, text string) Glob { return Glob{Include: include, Text: text} }
	tests := []struct {
		name  string
		text  string
		terms []string
		globs []Glob
		inc   []string
		exc   []string
		ctrl  []string
	}{
		{"empty", "", nil, nil, nil, nil, nil},
		{"a word", "Add", nil, []Glob{g(true, "add")}, nil, nil, nil},
		{"an exclusion", "!slow", nil, []Glob{g(false, "slow")}, nil, nil, nil},
		{"words with commas", "add, !slow ,sub", nil, []Glob{g(true, "add"), g(false, "slow"), g(true, "sub")}, nil, nil, nil},
		{"a term", "@failed", []string{"@failed"}, nil, nil, nil, nil},
		{"a term and a word", "@failed add", []string{"@failed"}, []Glob{g(true, "add")}, nil, nil, nil},
		{"a word between terms", "@executed foo @hidden", []string{"@executed", "@hidden"}, []Glob{g(true, "foo")}, nil, nil, nil},
		{"all the terms", "@failed @executed @doc @openedFiles @hidden", []string{"@failed", "@executed", "@doc", "@openedFiles", "@hidden"}, nil, nil, nil, nil},
		{"an @ that is no term is text", "@smoke", nil, []Glob{g(true, "@smoke")}, nil, nil, nil},
		{"a tag", "@go:slow", nil, nil, []string{"go\x00slow"}, nil, nil},
		{"an excluded tag", "!@go:slow", nil, nil, nil, []string{"go\x00slow"}, nil},
		{"a quoted tag", `@go:"two words" x`, nil, []Glob{g(true, "x")}, []string{"go\x00two words"}, nil, nil},
		{"a quoted tag with an escape", `@go:"a \"b\""`, nil, nil, []string{"go\x00a \"b\""}, nil, nil},
		{"a controller", "@ctrl:python", nil, nil, nil, nil, []string{"python"}},
		{"braces keep their commas", "{a,b}", nil, []Glob{g(true, "{a,b}")}, nil, nil, nil},
		{"a term and a tag", "@failed @go:slow add", []string{"@failed"}, []Glob{g(true, "add")}, []string{"go\x00slow"}, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := ParseFilter(tc.text, false)
			var terms []string
			for _, term := range Terms {
				if f.Has(term) {
					terms = append(terms, term)
				}
			}
			keys := func(m map[string]bool) []string {
				var out []string
				for k := range m {
					out = append(out, k)
				}
				slices.Sort(out)
				return out
			}
			if !reflect.DeepEqual(terms, orNil(tc.terms)) && (len(terms) != 0 || len(tc.terms) != 0) {
				t.Errorf("terms = %v, want %v", terms, tc.terms)
			}
			if !reflect.DeepEqual(f.Globs, tc.globs) && (len(f.Globs) != 0 || len(tc.globs) != 0) {
				t.Errorf("globs = %v, want %v", f.Globs, tc.globs)
			}
			for what, pair := range map[string][2][]string{"include tags": {keys(f.IncludeTags), tc.inc}, "exclude tags": {keys(f.ExcludeTags), tc.exc},
				"controllers": {keys(f.Controllers), tc.ctrl}} {
				if !reflect.DeepEqual(pair[0], pair[1]) && (len(pair[0]) != 0 || len(pair[1]) != 0) {
					t.Errorf("%s = %q, want %q", what, pair[0], pair[1])
				}
			}
			if f.Empty() != (tc.text == "") {
				t.Errorf("Empty() = %v for %q", f.Empty(), tc.text)
			}
		})
	}
}

func orNil(s []string) []string { return s }

func TestToggleTerm(t *testing.T) {
	tests := []struct {
		name, text, term string
		on               bool
		want             string
	}{
		{"add to nothing", "", TermFailed, true, "@failed"},
		{"add to words", "add", TermFailed, true, "add @failed"},
		{"already there", "@failed add", TermFailed, true, "@failed add"},
		{"take it out", "@failed add", TermFailed, false, "add"},
		{"take out of the middle", "a @failed b", TermFailed, false, "a b"},
		{"not there", "add", TermFailed, false, "add"},
		{"the other term stays", "@failed @hidden", TermHidden, false, "@failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToggleTerm(tc.text, tc.term, tc.on); got != tc.want {
				t.Errorf("ToggleTerm(%q, %s, %v) = %q, want %q", tc.text, tc.term, tc.on, got, tc.want)
			}
		})
	}
}

func TestFuzzyContains(t *testing.T) {
	tests := []struct {
		target, query string
		want          bool
	}{
		{"TestParseNumber", "tpn", true}, {"TestParseNumber", "tnp", false}, {"abc", "abcd", false}, {"", "a", false}, {"abc", "", false},
		{"ÄbC", "äc", true},
	}
	for _, tc := range tests {
		if got := fuzzyContains(tc.target, tc.query); got != tc.want {
			t.Errorf("fuzzyContains(%q, %q) = %v", tc.target, tc.query, got)
		}
	}
}

func TestCompareNatural(t *testing.T) {
	names := []string{"test10", "Test2", "test1", "a", "B", "test02"}
	slices.SortStableFunc(names, compareNatural)
	if want := []string{"a", "B", "test1", "Test2", "test02", "test10"}; !slices.Equal(names, want) {
		t.Errorf("sorted %v, want %v", names, want)
	}
}

// model is a service with one controller "c": two files with tests, and a lone test.
//
//	a_test.go (a.go:0)  Add (line 3), Sub (line 9), Slow (line 20)
//	b_test.go (b.go:0)  Parse (line 4), Nested (line 8)
//	Lone
type model struct {
	t   *testing.T
	svc *coretesting.Service
	ctl *coretesting.Controller
}

func cid(path ...string) string { return coretesting.JoinID("c", path...) }

func newModel(t *testing.T) *model {
	t.Helper()
	svc := coretesting.NewService(coretesting.Options{})
	t.Cleanup(svc.Close)
	ctl, err := svc.RegisterController(coretesting.ControllerOptions{ID: "c", Label: "Controller C"})
	if err != nil {
		t.Fatal(err)
	}
	m := &model{t: t, svc: svc, ctl: ctl}
	rng := func(line int) *coretesting.Range {
		return &coretesting.Range{Start: coretesting.Pos{Line: line}, End: coretesting.Pos{Line: line + 3}}
	}
	for _, it := range []coretesting.Item{
		{ID: cid("a_test.go"), Label: "a_test.go", URI: "file:///w/a_test.go"},
		{ID: cid("a_test.go", "Add"), Label: "Add", URI: "file:///w/a_test.go", Range: rng(3), Tags: []string{"fast"}},
		{ID: cid("a_test.go", "Sub"), Label: "Sub", URI: "file:///w/a_test.go", Range: rng(9), Tags: []string{"fast"}},
		{ID: cid("a_test.go", "Slow"), Label: "Slow", URI: "file:///w/a_test.go", Range: rng(20), Tags: []string{"slow"}},
		{ID: cid("b_test.go"), Label: "b_test.go", URI: "file:///w/b_test.go"},
		{ID: cid("b_test.go", "Parse"), Label: "Parse", URI: "file:///w/b_test.go", Range: rng(4)},
		{ID: cid("b_test.go", "Nested"), Label: "Nested", URI: "file:///w/b_test.go", Range: rng(8)},
		{ID: cid("Lone"), Label: "Lone"},
	} {
		if err := ctl.Upsert(it); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

// result makes a result of one run with the states; a state with a negative duration has none.
func (m *model) result(states map[string]coretesting.State, durations map[string]time.Duration) *coretesting.Result {
	m.t.Helper()
	res := m.svc.StartExternalRun(coretesting.ExternalRun{Request: coretesting.RunRequest{Group: coretesting.GroupRun}, Persist: true})
	run, err := res.AddTask(coretesting.TaskSpec{Name: "t", ControllerID: "c"})
	if err != nil {
		m.t.Fatal(err)
	}
	for id, st := range states {
		d, ok := durations[id]
		if !ok {
			d = coretesting.NoDuration
		}
		run.Ensure(cid(strings.Split(id, "/")...))
		run.SetState(cid(strings.Split(id, "/")...), st, d)
	}
	run.End()
	return res
}

func (m *model) project(mode ViewMode, by Sorting, hidden ...string) []*Element {
	h := map[string]bool{}
	for _, id := range hidden {
		h[id] = true
	}
	return Project(m.svc, ProjectOptions{Mode: mode, Sorting: by, Hidden: h})
}

// labels lists the labels of the rows of a tree, indented by depth.
func labels(es []*Element) []string {
	var out []string
	var walk func(es []*Element, depth int)
	walk = func(es []*Element, depth int) {
		for _, e := range es {
			out = append(out, strings.Repeat(" ", depth)+e.Label)
			walk(e.Children, depth+1)
		}
	}
	walk(es, 0)
	return out
}

func TestProjectTree(t *testing.T) {
	m := newModel(t)
	tests := []struct {
		name string
		by   Sorting
		want []string
	}{
		// by location: the tests of a file by their line, the files by name; "Lone" has no file, so it is by name
		{"by location", SortByLocation, []string{"a_test.go", " Add", " Sub", " Slow", "b_test.go", " Parse", " Nested", "Lone"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := labels(m.project(ModeTree, tc.by)); !slices.Equal(got, tc.want) {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestProjectStatesAndDurations(t *testing.T) {
	m := newModel(t)
	m.result(map[string]coretesting.State{
		"a_test.go/Add": coretesting.StatePassed, "a_test.go/Sub": coretesting.StateFailed, "a_test.go/Slow": coretesting.StateSkipped,
		"b_test.go/Parse": coretesting.StatePassed,
	}, map[string]time.Duration{"a_test.go/Add": 3 * time.Millisecond, "a_test.go/Sub": 12 * time.Millisecond, "b_test.go/Parse": 5 * time.Millisecond})
	roots := m.project(ModeTree, SortByLocation)
	want := map[string]struct {
		state coretesting.State
		dur   time.Duration
	}{
		"a_test.go": {coretesting.StateFailed, 15 * time.Millisecond},
		"Add":       {coretesting.StatePassed, 3 * time.Millisecond},
		"Sub":       {coretesting.StateFailed, 12 * time.Millisecond},
		"Slow":      {coretesting.StateSkipped, coretesting.NoDuration},
		"b_test.go": {coretesting.StatePassed, 5 * time.Millisecond},
		"Nested":    {coretesting.StateUnset, coretesting.NoDuration},
		"Lone":      {coretesting.StateUnset, coretesting.NoDuration},
	}
	var walk func(es []*Element)
	walk = func(es []*Element) {
		for _, e := range es {
			if w, ok := want[e.Label]; ok && e.Kind == KindTest {
				if e.State != w.state || e.Duration != w.dur {
					t.Errorf("%s: state %v duration %v, want %v %v", e.Label, e.State, e.Duration, w.state, w.dur)
				}
				if (w.dur >= 0) != e.HasDuration() {
					t.Errorf("%s: HasDuration = %v", e.Label, e.HasDuration())
				}
			}
			walk(e.Children)
		}
	}
	walk(roots)
}

func TestSorting(t *testing.T) {
	m := newModel(t)
	m.result(map[string]coretesting.State{
		"a_test.go/Add": coretesting.StatePassed, "a_test.go/Sub": coretesting.StateFailed, "a_test.go/Slow": coretesting.StatePassed,
		"b_test.go/Parse": coretesting.StateErrored, "Lone": coretesting.StatePassed,
	}, map[string]time.Duration{"a_test.go/Add": 3 * time.Millisecond, "a_test.go/Sub": 12 * time.Millisecond, "a_test.go/Slow": 700 * time.Millisecond,
		"b_test.go/Parse": 5 * time.Millisecond, "Lone": 40 * time.Millisecond})
	tests := []struct {
		name string
		by   Sorting
		want []string
	}{
		// the errored file first, then the failed (the errored one beats it), then the passed lone test; within a file the failed test, then the passed
		{"by status", SortByStatus, []string{"b_test.go", " Parse", " Nested", "a_test.go", " Sub", " Add", " Slow", "Lone"}},
		// the longest first
		{"by duration", SortByDuration, []string{"a_test.go", " Slow", " Sub", " Add", "Lone", "b_test.go", " Parse", " Nested"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := labels(m.project(ModeTree, tc.by))
			// the error row is always first under its test, which no sort order moves
			clean := func(in []string) []string {
				return slices.DeleteFunc(slices.Clone(in), func(s string) bool { return strings.Contains(s, "cannot read") })
			}
			if !slices.Equal(clean(got), clean(tc.want)) {
				t.Errorf("got  %q\nwant %q", clean(got), clean(tc.want))
			}
		})
	}
}

func TestProjectList(t *testing.T) {
	m := newModel(t)
	roots := m.project(ModeList, SortByLocation)
	var got []string
	for _, e := range roots {
		got = append(got, e.Label+"|"+e.Description)
	}
	want := []string{"Add|a_test.go", "Sub|a_test.go", "Slow|a_test.go", "Parse|b_test.go", "Nested|b_test.go", "Lone|"}
	// the tests of a file are by line and the files by name, but across files the locations are not comparable: the sort text (the label) decides
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("the list is %q, want %q", got, want)
	}
}

func TestProjectWithSeveralControllersShowsThemAsRoots(t *testing.T) {
	m := newModel(t)
	other, err := m.svc.RegisterController(coretesting.ControllerOptions{ID: "d", Label: "Controller D"})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Upsert(coretesting.Item{ID: coretesting.JoinID("d", "X"), Label: "X"}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []ViewMode{ModeTree, ModeList} {
		roots := m.project(mode, SortByLocation)
		if len(roots) != 2 || roots[0].Kind != KindController || roots[0].Label != "Controller C" || roots[1].Label != "Controller D" {
			t.Fatalf("%s: roots %q", mode, labels(roots))
		}
		if roots[1].Children[0].Parent != roots[1] {
			t.Errorf("%s: a test has no parent link", mode)
		}
	}
}

func TestHiddenTestsAreHiddenWithTheirChildren(t *testing.T) {
	m := newModel(t)
	roots := m.project(ModeTree, SortByLocation, cid("a_test.go"))
	f := ParseFilter("", false)
	vis := Visible(roots, f, FilterContext{})
	for _, e := range roots[0].Children {
		if vis[e] || !e.Hidden {
			t.Errorf("%s under a hidden file: visible %v, hidden %v", e.Label, vis[e], e.Hidden)
		}
	}
	if vis[roots[0]] {
		t.Error("the hidden file shows")
	}
	vis = Visible(roots, ParseFilter("@hidden", false), FilterContext{})
	if !vis[roots[0]] || !vis[roots[0].Children[0]] {
		t.Error("@hidden shows the hidden tests")
	}
}

func visibleLabels(roots []*Element, vis map[*Element]bool) []string {
	var out []string
	var walk func(es []*Element, depth int)
	walk = func(es []*Element, depth int) {
		for _, e := range es {
			if vis[e] && e.Kind != KindError {
				out = append(out, strings.Repeat(" ", depth)+e.Label)
				walk(e.Children, depth+1)
			}
		}
	}
	walk(roots, 0)
	return out
}

// The filter terms: which rows show for each text of the filter.
func TestFilterTerms(t *testing.T) {
	m := newModel(t)
	m.result(map[string]coretesting.State{
		"a_test.go/Add": coretesting.StatePassed, "a_test.go/Sub": coretesting.StateFailed, "b_test.go/Parse": coretesting.StateErrored,
	}, nil)
	docs := FilterContext{DocumentURIs: []string{"file:///w/b_test.go"}}
	tests := []struct {
		name  string
		text  string
		fuzzy bool
		ctx   FilterContext
		want  []string
	}{
		{"no filter", "", false, FilterContext{}, []string{"a_test.go", " Add", " Sub", " Slow", "b_test.go", " Parse", " Nested", "Lone"}},
		{"failed", "@failed", false, FilterContext{}, []string{"a_test.go", " Sub", "b_test.go", " Parse"}},
		{"executed", "@executed", false, FilterContext{}, []string{"a_test.go", " Add", " Sub", "b_test.go", " Parse"}},
		{"text", "add", false, FilterContext{}, []string{"a_test.go", " Add"}},
		{"text is not case sensitive", "SUB", false, FilterContext{}, []string{"a_test.go", " Sub"}},
		{"text of a file shows its tests", "a_test", false, FilterContext{}, []string{"a_test.go", " Add", " Sub", " Slow"}},
		{"an exclusion", "!add", false, FilterContext{}, []string{"a_test.go", " Sub", " Slow", "b_test.go", " Parse", " Nested", "Lone"}},
		{"two words", "add,lone", false, FilterContext{}, []string{"a_test.go", " Add", "Lone"}},
		{"a word and @failed", "@failed sub", false, FilterContext{}, []string{"a_test.go", " Sub"}},
		{"fuzzy", "ps", true, FilterContext{}, []string{"b_test.go", " Parse"}},
		{"not fuzzy", "ps", false, FilterContext{}, nil},
		{"a tag", "@c:slow", false, FilterContext{}, []string{"a_test.go", " Slow"}},
		{"an excluded tag", "!@c:slow @c:fast", false, FilterContext{}, []string{"a_test.go", " Add", " Sub"}},
		{"the controller", "@ctrl:c", false, FilterContext{}, []string{"a_test.go", " Add", " Sub", " Slow", "b_test.go", " Parse", " Nested", "Lone"}},
		{"another controller", "@ctrl:zz", false, FilterContext{}, nil},
		{"the files of the editors", "@openedFiles", false, docs, []string{"b_test.go", " Parse", " Nested"}},
		{"the file of the active editor", "@doc", false, docs, []string{"b_test.go", " Parse", " Nested"}},
		{"@doc with no document shows all", "@doc", false, FilterContext{}, []string{"a_test.go", " Add", " Sub", " Slow", "b_test.go", " Parse", " Nested", "Lone"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roots := m.project(ModeTree, SortByLocation)
			vis := Visible(roots, ParseFilter(tc.text, tc.fuzzy), tc.ctx)
			got := visibleLabels(roots, vis)
			if !slices.Equal(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("filter %q: got %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// IncludeExclude is what the title bar's run buttons start: the controller, with what the filter hides excluded.
func TestIncludeExclude(t *testing.T) {
	m := newModel(t)
	if _, err := m.ctl.AddProfile(coretesting.ProfileSpec{Label: "run", Group: coretesting.GroupRun, IsDefault: true,
		Run: func(context.Context, coretesting.ProfileRequest) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	m.result(map[string]coretesting.State{"a_test.go/Sub": coretesting.StateFailed, "b_test.go/Parse": coretesting.StateFailed}, nil)
	can := func(string) bool { return true }
	tests := []struct {
		name         string
		filter       string
		selected     []string
		wantInclude  []string
		wantExcluded []string
		selectedOnly bool
	}{
		{"everything", "", nil, []string{"c"}, nil, false},
		{"what the filter shows: a file with a majority", "@failed", nil, []string{"c"}, []string{"a_test.go/Add", "a_test.go/Slow", "b_test.go/Nested", "Lone"}, false},
		{"hidden rows are excluded", "sub", nil, []string{"c"}, []string{"a_test.go/Add", "a_test.go/Slow", "b_test.go", "Lone"}, false},
		{"a selected file", "", []string{"a_test.go"}, []string{"a_test.go"}, nil, true},
		{"selected tests", "", []string{"a_test.go/Add", "Lone"}, []string{"a_test.go/Add", "Lone"}, nil, true},
		{"a selected child of a selected file is part of it", "", []string{"a_test.go", "a_test.go/Add"}, []string{"a_test.go"}, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := NewExplorer(Opts{Service: m.svc, Post: func(f func()) { f() }})
			defer x.Dispose()
			x.SetFilterText(tc.filter)
			var keys []string
			for _, p := range tc.selected {
				keys = append(keys, cid(strings.Split(p, "/")...))
			}
			x.Model().SetSelection(keys)
			inc, exc := x.IncludeExclude(tc.selectedOnly, can)
			toID := func(ps []string) []string {
				var out []string
				for _, p := range ps {
					if p == "c" {
						out = append(out, "c")
						continue
					}
					out = append(out, cid(strings.Split(p, "/")...))
				}
				return out
			}
			slices.Sort(inc)
			slices.Sort(exc)
			wi, we := toID(tc.wantInclude), toID(tc.wantExcluded)
			slices.Sort(wi)
			slices.Sort(we)
			if !slices.Equal(inc, wi) && (len(inc) != 0 || len(wi) != 0) {
				t.Errorf("include %q, want %q", inc, wi)
			}
			if !slices.Equal(exc, we) && (len(exc) != 0 || len(we) != 0) {
				t.Errorf("exclude %q, want %q", exc, we)
			}
		})
	}
}

// A test the controller could not read has an error row below it, which the filter always shows (with the test), and the sort order keeps
// first.
func TestAnErrorRowIsShownWithItsTest(t *testing.T) {
	m := newModel(t)
	if err := m.ctl.Upsert(coretesting.Item{ID: cid("Broken"), Label: "Broken", Error: "cannot read the subtests"}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []ViewMode{ModeTree, ModeList} {
		roots := m.project(mode, SortByStatus)
		var broken *Element
		for _, e := range roots {
			if e.Label == "Broken" {
				broken = e
			}
		}
		if broken == nil || len(broken.Children) != 1 || broken.Children[0].Kind != KindError || broken.Children[0].Error != "cannot read the subtests" {
			t.Fatalf("%s: no error row below Broken: %q", mode, labels(roots))
		}
		vis := Visible(roots, ParseFilter("nothing-matches-this", false), FilterContext{})
		if !vis[broken] || !vis[broken.Children[0]] {
			t.Errorf("%s: the test with an error is hidden by a text that does not match it", mode)
		}
	}
}
