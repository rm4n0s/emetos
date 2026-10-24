package diff

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// show writes lines as the tests write them: separated by spaces, "_" for an
// empty line and "-" for no line at all.
func show(lines []string) string {
	if len(lines) == 0 {
		return "-"
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l
		if l == "" {
			out[i] = "_"
		}
	}
	return strings.Join(out, " ")
}

// newModel makes a model of three texts written as the tests write lines.
func newModel(t *testing.T, base, in1, in2 string) *MergeModel {
	t.Helper()
	m, err := NewMergeModel(context.Background(), MergeInputs{Base: fields(base), Input1: fields(in1), Input2: fields(in2)}, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// checkModel checks the invariants of a model: the regions are sorted, do not
// overlap and lie inside the result, and the lines of a region that was not
// edited by hand are the ones its state gives. It also checks that the result
// is made of the base lines outside the regions, the whole being what
// concatenating the pieces gives, when pure is set (nothing outside the regions
// was edited by hand either).
func checkModel(t *testing.T, m *MergeModel, pure bool) {
	t.Helper()
	prevEnd := 1
	edited := false
	var rebuilt []string
	baseNext := 1
	for _, g := range m.Regions() {
		if g.Result.StartLine < prevEnd || g.Result.StartLine > g.Result.EndLine || g.Result.EndLine > len(m.result)+1 {
			t.Fatalf("region %d: result range %+v (previous ended at %d, result has %d lines)", g.Index, g.Result, prevEnd, len(m.result))
		}
		prevEnd = g.Result.EndLine
		have := m.result[g.Result.StartLine-1 : g.Result.EndLine-1]
		if g.State.Kind == StateUnrecognized {
			edited = true
			rebuilt = nil
			continue
		}
		want := g.Range.BaseLines()
		if g.State.Kind != StateBase || g.Handled {
			want, _ = g.Range.TextForState(g.State)
		}
		if !slices.Equal(have, want) {
			t.Fatalf("region %d in state %v has %q, want %q", g.Index, g.State, have, want)
		}
		rebuilt = append(rebuilt, lines(m.inputs.Base, baseNext, g.Range.Base.StartLine)...)
		rebuilt = append(rebuilt, have...)
		baseNext = g.Range.Base.EndLine
	}
	if !edited && pure {
		rebuilt = append(rebuilt, lines(m.inputs.Base, baseNext, len(m.inputs.Base)+1)...)
		if !slices.Equal(rebuilt, m.result) {
			t.Fatalf("result %q, but its pieces make %q", m.result, rebuilt)
		}
	}
	if got, want := m.ResultText(), JoinLines(m.result, m.eol); got != want {
		t.Fatalf("ResultText %q, want %q", got, want)
	}
}

// step is one operation on a model and what it must leave.
type step struct {
	op            string // "accept 0 input1", "acceptall both", "reset 0", "handled 0 true", "toggle 0 2", "edit 2 3 X Y"
	wantResult    string
	wantEdit      string // the edit the operation returns: "[2,3)->X"; empty for no check
	wantUnhandled int
	wantStates    string // the states of the conflicts: "input1 base"; empty for no check
	wantRanges    string // the result ranges of the conflicts: "[2,3) [6,7)"; empty for no check
}

// run does the operation and returns the edits it made.
func (s step) run(t *testing.T, m *MergeModel) []Edit {
	t.Helper()
	f := strings.Fields(s.op)
	num := func(i int) int {
		n, err := strconv.Atoi(f[i])
		if err != nil {
			t.Fatalf("bad operation %q", s.op)
		}
		return n
	}
	choice := func(name string) Choice {
		return map[string]Choice{"base": AcceptBase, "input1": AcceptInput1, "input2": AcceptInput2, "both": AcceptBoth}[name]
	}
	var (
		edits []Edit
		err   error
	)
	switch f[0] {
	case "accept":
		var e Edit
		e, err = m.Accept(num(1), choice(f[2]))
		edits = []Edit{e}
	case "acceptall":
		edits, err = m.AcceptAll(choice(f[1]))
	case "reset":
		var e Edit
		e, err = m.Reset(num(1))
		edits = []Edit{e}
	case "handled":
		err = m.SetHandled(num(1), f[2] == "true")
	case "toggle":
		var e Edit
		e, err = m.Toggle(num(1), Input(num(2)))
		edits = []Edit{e}
	case "edit":
		text := f[3:]
		if len(text) == 1 && text[0] == "-" {
			text = nil
		}
		err = m.ApplyResultEdit(num(1), num(2), fields(strings.Join(text, " ")))
	default:
		t.Fatalf("unknown operation %q", s.op)
	}
	if err != nil {
		t.Fatalf("%s: %v", s.op, err)
	}
	return edits
}

func describeEdit(e Edit) string {
	return fmt.Sprintf("[%d,%d)->%s", e.StartLine, e.EndLine, show(e.NewLines))
}

func runSteps(t *testing.T, m *MergeModel, steps []step) {
	t.Helper()
	pure := true
	for i, s := range steps {
		before := m.Result()
		edits := s.run(t, m)
		pure = pure && !strings.HasPrefix(s.op, "edit")
		checkModel(t, m, pure)
		where := fmt.Sprintf("step %d (%s)", i, s.op)
		if got := show(m.Result()); got != s.wantResult {
			t.Errorf("%s: result %q, want %q", where, got, s.wantResult)
		}
		if s.wantEdit != "" {
			got := make([]string, len(edits))
			for j, e := range edits {
				got[j] = describeEdit(e)
			}
			if strings.Join(got, " ") != s.wantEdit {
				t.Errorf("%s: edits %q, want %q", where, strings.Join(got, " "), s.wantEdit)
			}
		}
		// Every edit applies to the result before it: in order, they make the result.
		if len(edits) > 0 && !strings.HasPrefix(s.op, "edit") {
			replay := slices.Clone(before)
			for _, e := range edits {
				replay = slices.Replace(replay, e.StartLine-1, e.EndLine-1, e.NewLines...)
			}
			if !slices.Equal(replay, m.Result()) {
				t.Errorf("%s: applying the edits gives %q, the model has %q", where, replay, m.Result())
			}
		}
		if got := m.UnhandledCount(); got != s.wantUnhandled {
			t.Errorf("%s: %d unhandled, want %d", where, got, s.wantUnhandled)
		}
		if got := m.HandledCount() + m.UnhandledCount(); got != m.ConflictCount() {
			t.Errorf("%s: handled + unhandled = %d, conflicts %d", where, got, m.ConflictCount())
		}
		var states, rs []string
		for _, c := range m.Conflicts() {
			states = append(states, c.State.Kind.String())
			if c.State.Kind == StateBoth {
				states[len(states)-1] = fmt.Sprintf("both%d", c.State.FirstInput)
			}
			rs = append(rs, fmt.Sprintf("[%d,%d)", c.Result.StartLine, c.Result.EndLine))
		}
		if s.wantStates != "" && strings.Join(states, " ") != s.wantStates {
			t.Errorf("%s: states %q, want %q", where, strings.Join(states, " "), s.wantStates)
		}
		if s.wantRanges != "" && strings.Join(rs, " ") != s.wantRanges {
			t.Errorf("%s: result ranges %q, want %q", where, strings.Join(rs, " "), s.wantRanges)
		}
	}
}

// The fixture of the accept tests: line 2 is a conflict (X against Y), line 4
// only changed by input 1 (applied at the start), line 6 a conflict (input 1
// replaces it by two lines, input 2 deletes it).
const (
	fixBase = "a b c d e f g"
	fixIn1  = "a X c D e F1 F2 g"
	fixIn2  = "a Y c d e g"
)

func TestMergeModelInitialResult(t *testing.T) {
	m := newModel(t, fixBase, fixIn1, fixIn2)
	checkModel(t, m, true)
	if got := show(m.Result()); got != "a b c D e f g" {
		t.Errorf("result %q", got)
	}
	regions := m.Regions()
	if len(regions) != 3 || m.ConflictCount() != 2 {
		t.Fatalf("%d regions and %d conflicts", len(regions), m.ConflictCount())
	}
	want := []struct {
		conflict bool
		handled  bool
		index    int
		state    RangeState
		result   LineRange
	}{
		{true, false, 0, RangeState{}, LineRange{StartLine: 2, EndLine: 3}},
		{false, true, -1, RangeState{Kind: StateInput1}, LineRange{StartLine: 4, EndLine: 5}},
		{true, false, 1, RangeState{}, LineRange{StartLine: 6, EndLine: 7}},
	}
	for i, w := range want {
		g := regions[i]
		if g.Conflict != w.conflict || g.Handled != w.handled || g.ConflictIndex != w.index || g.State != w.state || g.Result != w.result || g.Index != i {
			t.Errorf("region %d: %+v, want %+v", i, g, w)
		}
	}
	if m.UnhandledCount() != 2 || m.HandledCount() != 0 {
		t.Errorf("unhandled %d handled %d", m.UnhandledCount(), m.HandledCount())
	}
	if g, ok := m.RegionAtResultLine(6); !ok || g.Index != 2 {
		t.Errorf("RegionAtResultLine(6) = %v, %v", g.Index, ok)
	}
	if _, ok := m.RegionAtResultLine(3); ok {
		t.Error("line 3 is in no region")
	}
	if got := m.EOL(); got != "\n" {
		t.Errorf("EOL %q", got)
	}
	if m.HitTimeout() {
		t.Error("HitTimeout")
	}
	if got := show(m.Inputs().Input2); got != fixIn2 {
		t.Errorf("Inputs().Input2 = %q", got)
	}
}

func TestMergeModelAccept(t *testing.T) {
	tests := []struct {
		name  string
		steps []step
	}{
		{"input 1 on the first conflict", []step{
			{"accept 0 input1", "a X c D e f g", "[2,3)->X", 1, "input1 base", "[2,3) [6,7)"},
		}},
		{"input 2 on the first conflict", []step{
			{"accept 0 input2", "a Y c D e f g", "[2,3)->Y", 1, "input2 base", "[2,3) [6,7)"},
		}},
		{"base on the first conflict", []step{
			{"accept 0 base", "a b c D e f g", "[2,3)->b", 1, "base base", "[2,3) [6,7)"},
		}},
		{"both is current then incoming and shifts the next conflict", []step{
			{"accept 0 both", "a Y X c D e f g", "[2,3)->Y X", 1, "both2 base", "[2,4) [7,8)"},
		}},
		{"input 1 on the second conflict replaces one line by two", []step{
			{"accept 1 input1", "a b c D e F1 F2 g", "[6,7)->F1 F2", 1, "base input1", "[2,3) [6,8)"},
		}},
		{"input 2 on the second conflict deletes the line", []step{
			{"accept 1 input2", "a b c D e g", "[6,7)->-", 1, "base input2", "[2,3) [6,6)"},
		}},
		{"both on the second conflict is the lines of input 1 as input 2 has none", []step{
			{"accept 1 both", "a b c D e F1 F2 g", "[6,7)->F1 F2", 1, "base both2", "[2,3) [6,8)"},
		}},
		{"both accepts shift later conflicts one after the other", []step{
			{"accept 1 input1", "a b c D e F1 F2 g", "", 1, "base input1", "[2,3) [6,8)"},
			{"accept 0 both", "a Y X c D e F1 F2 g", "[2,3)->Y X", 0, "both2 input1", "[2,4) [7,9)"},
			{"accept 0 base", "a b c D e F1 F2 g", "[2,4)->b", 0, "base input1", "[2,3) [6,8)"},
		}},
		{"changing the choice of a handled conflict", []step{
			{"accept 0 input1", "a X c D e f g", "", 1, "", ""},
			{"accept 0 input2", "a Y c D e f g", "[2,3)->Y", 1, "input2 base", ""},
		}},
		{"accept all", []step{
			{"acceptall input1", "a X c D e F1 F2 g", "[6,7)->F1 F2 [2,3)->X", 0, "input1 input1", "[2,3) [6,8)"},
		}},
		{"accept all input 2", []step{
			{"acceptall input2", "a Y c D e g", "[6,7)->- [2,3)->Y", 0, "input2 input2", "[2,3) [6,6)"},
		}},
		{"accept all both", []step{
			{"acceptall both", "a Y X c D e F1 F2 g", "[6,7)->F1 F2 [2,3)->Y X", 0, "both2 both2", "[2,4) [7,9)"},
		}},
		{"accept all base", []step{
			{"acceptall base", "a b c D e f g", "[6,7)->f [2,3)->b", 0, "base base", "[2,3) [6,7)"},
		}},
		{"accept all leaves handled conflicts alone", []step{
			{"accept 0 input1", "a X c D e f g", "", 1, "", ""},
			{"acceptall input2", "a X c D e g", "[6,7)->-", 0, "input1 input2", "[2,3) [6,6)"},
		}},
		{"accept all with nothing to do", []step{
			{"acceptall input1", "a X c D e F1 F2 g", "", 0, "", ""},
			{"acceptall input2", "a X c D e F1 F2 g", "", 0, "", ""},
		}},
		{"reset goes back to base and unhandled", []step{
			{"accept 0 both", "a Y X c D e f g", "", 1, "", ""},
			{"reset 0", "a b c D e f g", "[2,4)->b", 2, "base base", "[2,3) [6,7)"},
		}},
		{"toggle includes and excludes inputs", []step{
			{"toggle 0 1", "a X c D e f g", "[2,3)->X", 1, "input1 base", ""},
			{"toggle 0 2", "a X Y c D e f g", "[2,3)->X Y", 1, "both1 base", "[2,4) [7,8)"},
			{"toggle 0 1", "a Y c D e f g", "[2,4)->Y", 1, "input2 base", "[2,3) [6,7)"},
			{"toggle 0 2", "a b c D e f g", "[2,3)->b", 1, "base base", ""},
		}},
		{"handled flag", []step{
			{"handled 0 true", "a b c D e f g", "", 1, "base base", ""},
			{"handled 0 false", "a b c D e f g", "", 2, "", ""},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSteps(t, newModel(t, fixBase, fixIn1, fixIn2), tt.steps)
		})
	}
}

func TestMergeModelManualEdits(t *testing.T) {
	tests := []struct {
		name  string
		steps []step
	}{
		{"an edit before the regions shifts them", []step{
			{"edit 1 2 a1 a2 a3", "a1 a2 a3 b c D e f g", "", 2, "base base", "[4,5) [8,9)"},
			{"edit 1 3 -", "a3 b c D e f g", "", 2, "base base", "[2,3) [6,7)"},
		}},
		{"an edit between the regions shifts the ones after", []step{
			{"edit 3 4 c1 c2", "a b c1 c2 D e f g", "", 2, "base base", "[2,3) [7,8)"},
		}},
		{"an edit after all regions leaves them", []step{
			{"edit 7 8 G H", "a b c D e f G H", "", 2, "base base", "[2,3) [6,7)"},
		}},
		{"an edit inside a conflict marks it edited and keeps it unhandled", []step{
			{"edit 2 3 Z", "a Z c D e f g", "", 2, "unrecognized base", "[2,3) [6,7)"},
			{"accept 0 input1", "a X c D e f g", "[2,3)->X", 1, "input1 base", "[2,3) [6,7)"},
		}},
		{"an edit inside a handled conflict keeps it handled", []step{
			{"accept 0 input1", "a X c D e f g", "", 1, "", ""},
			{"edit 2 3 X X2", "a X X2 c D e f g", "", 1, "unrecognized base", "[2,4) [7,8)"},
		}},
		{"an edit that grows a conflict moves the next ones", []step{
			{"edit 2 3 b b2 b3", "a b b2 b3 c D e f g", "", 2, "unrecognized base", "[2,5) [8,9)"},
			{"accept 1 input2", "a b b2 b3 c D e g", "", 1, "unrecognized input2", "[2,5) [8,8)"},
			{"accept 0 input2", "a Y c D e g", "[2,5)->Y", 0, "input2 input2", "[2,3) [6,6)"},
		}},
		{"an edit that empties a conflict", []step{
			{"edit 2 3 -", "a c D e f g", "", 2, "unrecognized base", "[2,2) [5,6)"},
			{"accept 0 input1", "a X c D e f g", "[2,2)->X", 1, "input1 base", "[2,3) [6,7)"},
		}},
		{"typing into an empty conflict", []step{
			{"accept 1 input2", "a b c D e g", "", 1, "", "[2,3) [6,6)"},
			{"edit 6 6 new", "a b c D e new g", "", 1, "base unrecognized", "[2,3) [6,7)"},
		}},
		{"an insertion at the start of a conflict is outside it", []step{
			{"edit 2 2 new", "a new b c D e f g", "", 2, "base base", "[3,4) [7,8)"},
		}},
		{"an insertion at the end of a conflict is outside it", []step{
			{"edit 3 3 new", "a b new c D e f g", "", 2, "base base", "[2,3) [7,8)"},
		}},
		{"an edit of the line after a conflict is outside it", []step{
			{"edit 3 4 C", "a b C D e f g", "", 2, "base base", "[2,3) [6,7)"},
		}},
		{"an insertion in the middle of a conflict is inside it", []step{
			{"accept 0 both", "a Y X c D e f g", "", 1, "", ""},
			{"edit 3 3 mid", "a Y mid X c D e f g", "", 1, "unrecognized base", "[2,5) [8,9)"},
		}},
		{"an edit that replaces a conflict with an equal line is not an edit", []step{
			{"edit 2 3 b", "a b c D e f g", "", 2, "base base", "[2,3) [6,7)"},
		}},
		{"an edit that types the text of an input is recognized as it", []step{
			{"edit 2 3 Z", "a Z c D e f g", "", 2, "unrecognized base", ""},
			{"edit 2 3 X", "a X c D e f g", "", 2, "input1 base", ""},
			{"edit 2 3 Y", "a Y c D e f g", "", 2, "input2 base", ""},
			{"edit 2 3 b", "a b c D e f g", "", 2, "base base", ""},
		}},
		{"an edit that spans two conflicts gives its lines to the first", []step{
			{"edit 2 7 Q", "a Q g", "", 2, "unrecognized input2", "[2,3) [3,3)"},
			{"accept 1 input1", "a Q F1 F2 g", "", 1, "unrecognized input1", "[2,3) [3,5)"},
		}},
		{"an edit that starts inside a conflict and ends in the next region", []step{
			{"edit 2 5 R", "a R e f g", "", 2, "unrecognized base", "[2,3) [4,5)"},
		}},
		{"an edit that starts before a conflict and ends inside it", []step{
			{"edit 1 3 Q", "Q c D e f g", "", 2, "unrecognized base", "[1,2) [5,6)"},
		}},
		{"an edit that covers a conflict whole", []step{
			{"edit 1 4 Q", "Q D e f g", "", 2, "unrecognized base", "[1,2) [4,5)"},
		}},
		{"replacing the whole result", []step{
			{"edit 1 8 N M", "N M", "", 2, "unrecognized input2", "[1,3) [3,3)"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSteps(t, newModel(t, fixBase, fixIn1, fixIn2), tt.steps)
		})
	}
}

func TestMergeModelAcceptAfterEditElsewhere(t *testing.T) {
	m := newModel(t, fixBase, fixIn1, fixIn2)
	runSteps(t, m, []step{
		{"edit 1 1 top1 top2", "top1 top2 a b c D e f g", "", 2, "base base", "[4,5) [8,9)"},
		{"accept 1 input1", "top1 top2 a b c D e F1 F2 g", "[8,9)->F1 F2", 1, "base input1", "[4,5) [8,10)"},
		{"edit 4 5 B", "top1 top2 a B c D e F1 F2 g", "", 1, "unrecognized input1", "[4,5) [8,10)"},
		{"accept 0 both", "top1 top2 a Y X c D e F1 F2 g", "[4,5)->Y X", 0, "both2 input1", "[4,6) [9,11)"},
	})
	if got := m.ResultText(); got != "top1\ntop2\na\nY\nX\nc\nD\ne\nF1\nF2\ng" {
		t.Errorf("ResultText %q", got)
	}
}

func TestMergeModelEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		base         string
		in1, in2     string
		wantResult   string
		wantConflict int
	}{
		{"no changes", "a b", "a b", "a b", "a b", 0},
		{"inputs equal, base different", "a b", "x y", "x y", "x y", 0},
		{"one input equals the base", "a b c", "a b c", "a X c", "a X c", 0},
		{"empty base conflict", "", "x", "y", "_", 1},
		{"conflict over the whole file", "a b", "x y", "p q", "a b", 1},
		{"change at the first line", "a b c", "X b c", "Y b c", "a b c", 1},
		{"change at the last line", "a b c", "a b X", "a b Y", "a b c", 1},
		{"trailing empty line kept", "a _", "a X _", "a _", "a X _", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, tt.base, tt.in1, tt.in2)
			checkModel(t, m, true)
			if got := show(m.Result()); got != tt.wantResult {
				t.Errorf("result %q, want %q", got, tt.wantResult)
			}
			if m.ConflictCount() != tt.wantConflict {
				t.Errorf("%d conflicts, want %d", m.ConflictCount(), tt.wantConflict)
			}
		})
	}
}

func TestMergeModelFromMarkers(t *testing.T) {
	const withBase = "top\n<<<<<<< HEAD\nours\n||||||| base\norig\n=======\ntheirs\n>>>>>>> topic\nmid\n<<<<<<< HEAD\nours2\n||||||| base\norig2\n=======\ntheirs2\n>>>>>>> topic\nend\n"
	const withoutBase = "top\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nmid\n<<<<<<< HEAD\nours2\n=======\ntheirs2\n>>>>>>> topic\nend\n"
	tests := []struct {
		name       string
		text       string
		wantBase   string
		wantIn1    string
		wantIn2    string
		wantResult string
		afterAll   map[Choice]string // the text after AcceptAll
		labels     string
	}{
		{
			"with the base sections", withBase,
			"top orig mid orig2 end _", "top theirs mid theirs2 end _", "top ours mid ours2 end _",
			"top orig mid orig2 end _",
			map[Choice]string{
				AcceptInput1: "top\ntheirs\nmid\ntheirs2\nend\n",
				AcceptInput2: "top\nours\nmid\nours2\nend\n",
				AcceptBoth:   "top\nours\ntheirs\nmid\nours2\ntheirs2\nend\n",
				AcceptBase:   "top\norig\nmid\norig2\nend\n",
			},
			"in1=topic in2=HEAD base=base",
		},
		{
			"without the base sections", withoutBase,
			"top mid end _", "top theirs mid theirs2 end _", "top ours mid ours2 end _",
			"top mid end _",
			map[Choice]string{
				AcceptInput1: "top\ntheirs\nmid\ntheirs2\nend\n",
				AcceptInput2: "top\nours\nmid\nours2\nend\n",
				AcceptBoth:   "top\nours\ntheirs\nmid\nours2\ntheirs2\nend\n",
				AcceptBase:   "top\nmid\nend\n",
			},
			"in1=topic in2=HEAD base=",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := MergeInputsFromMarkerText(tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if show(in.Base) != tt.wantBase || show(in.Input1) != tt.wantIn1 || show(in.Input2) != tt.wantIn2 {
				t.Errorf("inputs:\n base %s\n in1  %s\n in2  %s", show(in.Base), show(in.Input1), show(in.Input2))
			}
			if got := fmt.Sprintf("in1=%s in2=%s base=%s", in.Input1Label, in.Input2Label, in.BaseLabel); got != tt.labels {
				t.Errorf("labels %q, want %q", got, tt.labels)
			}
			if in.EOL != "\n" || len(in.MarkedConflicts) != 2 {
				t.Errorf("EOL %q, %d marked conflicts", in.EOL, len(in.MarkedConflicts))
			}
			m, err := NewMergeModel(context.Background(), in, MergeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			checkModel(t, m, true)
			if got := show(m.Result()); got != tt.wantResult {
				t.Errorf("initial result %q, want %q", got, tt.wantResult)
			}
			if m.ConflictCount() != 2 || m.UnhandledCount() != 2 {
				t.Errorf("%d conflicts, %d unhandled", m.ConflictCount(), m.UnhandledCount())
			}
			for choice, want := range tt.afterAll {
				m, err := NewMergeModel(context.Background(), in, MergeOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := m.AcceptAll(choice); err != nil {
					t.Fatal(err)
				}
				checkModel(t, m, true)
				if got := m.ResultText(); got != want {
					t.Errorf("after AcceptAll(%d): %q, want %q", choice, got, want)
				}
				if m.UnhandledCount() != 0 || m.HandledCount() != 2 {
					t.Errorf("after AcceptAll: %d unhandled %d handled", m.UnhandledCount(), m.HandledCount())
				}
			}
		})
	}
}

func TestMergeModelFromMarkersOneSideEmpty(t *testing.T) {
	// The current side is empty and there is no base section: only the
	// incoming side differs from the (empty) base, which is still a conflict.
	in, err := MergeInputsFromMarkerText("a\n<<<<<<< HEAD\n=======\nb\n>>>>>>> t\nc\n")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMergeModel(context.Background(), in, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if m.ConflictCount() != 1 || m.UnhandledCount() != 1 {
		t.Fatalf("%d conflicts, %d unhandled", m.ConflictCount(), m.UnhandledCount())
	}
	if got := m.ResultText(); got != "a\nc\n" {
		t.Errorf("initial result %q", got)
	}
	steps := []struct {
		choice Choice
		want   string
	}{
		{AcceptInput1, "a\nb\nc\n"},
		{AcceptInput2, "a\nc\n"},
		{AcceptBoth, "a\nb\nc\n"},
		{AcceptBase, "a\nc\n"},
	}
	for _, s := range steps {
		if _, err := m.Accept(0, s.choice); err != nil {
			t.Fatal(err)
		}
		if got := m.ResultText(); got != s.want {
			t.Errorf("Accept(%d): %q, want %q", s.choice, got, s.want)
		}
	}
	// Equal sides are a conflict of a marker file too.
	in, err = MergeInputsFromMarkerText("<<<<<<< HEAD\nsame\n=======\nsame\n>>>>>>> t\n")
	if err != nil {
		t.Fatal(err)
	}
	m, err = NewMergeModel(context.Background(), in, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if m.ConflictCount() != 1 || m.UnhandledCount() != 1 {
		t.Errorf("equal sides: %d conflicts, %d unhandled", m.ConflictCount(), m.UnhandledCount())
	}
}

func TestMergeModelFromMarkersFileOnlyConflict(t *testing.T) {
	// No trailing line break and nothing but the conflict: the base has no line.
	in, err := MergeInputsFromMarkerText("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> t")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMergeModel(context.Background(), in, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkModel(t, m, true)
	if m.ConflictCount() != 1 {
		t.Fatalf("%d conflicts", m.ConflictCount())
	}
	if _, err := m.Accept(0, AcceptBoth); err != nil {
		t.Fatal(err)
	}
	if got := m.ResultText(); got != "ours\ntheirs" {
		t.Errorf("result %q", got)
	}
}

func TestMergeModelEOL(t *testing.T) {
	text := "top\r\n<<<<<<< HEAD\r\nours\r\n=======\r\ntheirs\r\n>>>>>>> topic\r\nend\r\n"
	in, err := MergeInputsFromMarkerText(text)
	if err != nil {
		t.Fatal(err)
	}
	if in.EOL != "\r\n" {
		t.Fatalf("EOL %q", in.EOL)
	}
	m, err := NewMergeModel(context.Background(), in, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ResultText(); got != "top\r\nend\r\n" {
		t.Errorf("initial result %q", got)
	}
	if _, err := m.Accept(0, AcceptBoth); err != nil {
		t.Fatal(err)
	}
	if got := m.ResultText(); got != "top\r\nours\r\ntheirs\r\nend\r\n" {
		t.Errorf("result %q", got)
	}
	if strings.Contains(strings.ReplaceAll(m.ResultText(), "\r\n", ""), "\n") {
		t.Error("a line break is not CRLF")
	}
	if m.EOL() != "\r\n" {
		t.Errorf("EOL() = %q", m.EOL())
	}

	// A mixed file takes the line break most lines have.
	in, err = MergeInputsFromMarkerText("a\n<<<<<<< H\r\nb\r\n=======\r\nc\r\n>>>>>>> t\r\nz")
	if err != nil {
		t.Fatal(err)
	}
	if in.EOL != "\r\n" {
		t.Errorf("mixed EOL %q", in.EOL)
	}
}

func TestMergeInputsFromFiles(t *testing.T) {
	in := MergeInputsFromFiles(fields("a b"), fields("a X"), fields("a Y"))
	if show(in.Base) != "a b" || show(in.Input1) != "a X" || show(in.Input2) != "a Y" || in.EOL != "" || len(in.MarkedConflicts) != 0 {
		t.Errorf("%+v", in)
	}
	m, err := NewMergeModel(context.Background(), in, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if m.ConflictCount() != 1 || show(m.Result()) != "a b" {
		t.Errorf("%d conflicts, result %q", m.ConflictCount(), show(m.Result()))
	}
	// The model copies the texts.
	in.Base[0] = "changed"
	if show(m.Inputs().Base) != "a b" {
		t.Error("the model shares the base with its caller")
	}
}

func TestMergeModelDoesNotShareResult(t *testing.T) {
	m := newModel(t, fixBase, fixIn1, fixIn2)
	r := m.Result()
	r[0] = "changed"
	if m.Result()[0] != "a" {
		t.Error("Result shares its slice with the model")
	}
}

func TestMergeModelFailures(t *testing.T) {
	m := newModel(t, fixBase, fixIn1, fixIn2)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name  string
		do    func() error
		route string
	}{
		{"accept a conflict that does not exist", func() error { _, err := m.Accept(2, AcceptInput1); return err }, "MergeModel.Accept->MergeModel.SetState->MergeModel.conflictRegion.MergeConflictIndexInvalid"},
		{"accept a negative conflict", func() error { _, err := m.Accept(-1, AcceptInput1); return err }, "MergeConflictIndexInvalid"},
		{"accept a wrong choice", func() error { _, err := m.Accept(0, Choice(9)); return err }, "MergeModel.Accept.MergeChoiceInvalid"},
		{"accept the zero choice", func() error { _, err := m.Accept(0, 0); return err }, "MergeChoiceInvalid"},
		{"accept all with a wrong choice", func() error { _, err := m.AcceptAll(Choice(-1)); return err }, "MergeModel.AcceptAll.MergeChoiceInvalid"},
		{"reset a conflict that does not exist", func() error { _, err := m.Reset(5); return err }, "MergeModel.Reset->MergeModel.conflictRegion.MergeConflictIndexInvalid"},
		{"set handled of a conflict that does not exist", func() error { return m.SetHandled(2, true) }, "MergeModel.SetHandled->MergeModel.conflictRegion.MergeConflictIndexInvalid"},
		{"toggle a conflict that does not exist", func() error { _, err := m.Toggle(2, Input1); return err }, "MergeModel.Toggle->MergeModel.conflictRegion.MergeConflictIndexInvalid"},
		{"toggle a wrong input", func() error { _, err := m.Toggle(0, Input(3)); return err }, "MergeModel.Toggle.MergeChoiceInvalid"},
		{"set the unrecognized state", func() error { _, err := m.SetState(0, RangeState{Kind: StateUnrecognized}); return err }, "MergeModel.SetState.MergeStateInvalid"},
		{"set a both state without a first input", func() error { _, err := m.SetState(0, RangeState{Kind: StateBoth}); return err }, "MergeStateInvalid"},
		{"set a state of an unknown kind", func() error { _, err := m.SetState(0, RangeState{Kind: StateKind(7)}); return err }, "MergeStateInvalid"},
		{"set a state of one input with a first input", func() error { _, err := m.SetState(0, RangeState{Kind: StateInput1, FirstInput: Input2}); return err }, "MergeStateInvalid"},
		{"edit before the result", func() error { return m.ApplyResultEdit(0, 1, nil) }, "MergeModel.ApplyResultEdit.MergeEditInvalid"},
		{"edit past the result", func() error { return m.ApplyResultEdit(1, 9, nil) }, "MergeEditInvalid"},
		{"edit with the end before the start", func() error { return m.ApplyResultEdit(3, 2, nil) }, "MergeEditInvalid"},
		{"an EOL that is not a line break", func() error {
			_, err := NewMergeModel(context.Background(), MergeInputs{Base: fields("a"), EOL: "\r"}, MergeOptions{})
			return err
		}, "NewMergeModel.MergeEOLInvalid"},
		{"a cancelled context", func() error {
			_, err := NewMergeModel(cancelled, MergeInputs{Base: fields("a b"), Input1: fields("a X"), Input2: fields("a Y")}, MergeOptions{})
			return err
		}, "NewMergeModel->Compute->computeDiff"},
		{"a negative time limit", func() error {
			_, err := NewMergeModel(context.Background(), MergeInputs{Base: fields("a b")}, MergeOptions{MaxComputationTime: -1})
			return err
		}, "DiffTimeoutInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.do()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), tt.route)
			}
		})
	}
	// A failed operation changes nothing.
	if got := show(m.Result()); got != "a b c D e f g" {
		t.Errorf("result changed to %q", got)
	}
	if err := (func() error {
		_, err := NewMergeModel(cancelled, MergeInputs{Base: fields("a b"), Input1: fields("a X"), Input2: fields("a Y")}, MergeOptions{})
		return err
	})(); !errors.Is(err, context.Canceled) {
		t.Errorf("the cancellation should wrap context.Canceled, got %v", err)
	}
}

func TestMergeInputsFromMarkersMarkedConflicts(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []LineRange
	}{
		{"with base sections", "top\n<<<<<<< a\nx\n||||||| b\norig\n=======\ny\n>>>>>>> c\nmid\n<<<<<<< a\nx\n||||||| b\no1\no2\n=======\ny\n>>>>>>> c\n",
			[]LineRange{{StartLine: 2, EndLine: 3}, {StartLine: 4, EndLine: 6}}},
		{"without base sections", "top\n<<<<<<< a\nx\n=======\ny\n>>>>>>> c\nmid\n<<<<<<< a\nx\n=======\ny\n>>>>>>> c\n",
			[]LineRange{{StartLine: 2, EndLine: 2}, {StartLine: 3, EndLine: 3}}},
		{"conflict at the start", "<<<<<<< a\nx\n=======\ny\n>>>>>>> c\nrest",
			[]LineRange{{StartLine: 1, EndLine: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := MergeInputsFromMarkerText(tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(in.MarkedConflicts, tt.want) {
				t.Errorf("got %v, want %v", in.MarkedConflicts, tt.want)
			}
		})
	}
	// With no conflicts the inputs are the lines themselves.
	lines := fields("a b c")
	in := MergeInputsFromMarkers(lines, nil)
	if !slices.Equal(in.Base, lines) || !slices.Equal(in.Input1, lines) || !slices.Equal(in.Input2, lines) || len(in.MarkedConflicts) != 0 {
		t.Errorf("%+v", in)
	}
}
