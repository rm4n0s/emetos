package buffer

import "testing"

func TestSelectionDirection(t *testing.T) {
	a, b := Position{1, 1}, Position{2, 3}
	tests := []struct {
		name string
		sel  Selection
		dir  Direction
	}{
		{"cursor", NewCursor(a), LTR},
		{"forward", Selection{Anchor: a, Active: b}, LTR},
		{"backward", Selection{Anchor: b, Active: a}, RTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sel.Direction(); got != tt.dir {
				t.Errorf("Direction = %v, want %v", got, tt.dir)
			}
			if got, want := tt.sel.IsEmpty(), tt.name == "cursor"; got != want {
				t.Errorf("IsEmpty = %v", got)
			}
			if tt.sel.Start().IsBefore(tt.sel.End()) == tt.sel.IsEmpty() {
				t.Errorf("Start %v and End %v disagree with IsEmpty", tt.sel.Start(), tt.sel.End())
			}
		})
	}
}

func TestSelectionFromRange(t *testing.T) {
	r := Range{Position{1, 1}, Position{2, 3}}
	ltr := SelectionFromRange(r, LTR)
	if ltr.Anchor != r.Start || ltr.Active != r.End {
		t.Errorf("LTR = %+v", ltr)
	}
	rtl := SelectionFromRange(r, RTL)
	if rtl.Anchor != r.End || rtl.Active != r.Start {
		t.Errorf("RTL = %+v", rtl)
	}
	if ltr.Range() != r || rtl.Range() != r {
		t.Errorf("Range = %+v, %+v, want %+v", ltr.Range(), rtl.Range(), r)
	}
}

// Ported from VS Code: reference/vscode/src/vs/editor/common/core/selection.ts
// (setStartPosition and setEndPosition keep the direction).
func TestSelectionWithStartAndEnd(t *testing.T) {
	r := Range{Position{1, 2}, Position{3, 4}}
	n := Position{0, 9}
	tests := []struct {
		name string
		got  Selection
		want Selection
	}{
		{"LTR start", SelectionFromRange(r, LTR).WithStart(n), Selection{Anchor: n, Active: r.End}},
		{"LTR end", SelectionFromRange(r, LTR).WithEnd(n), Selection{Anchor: r.Start, Active: n}},
		{"RTL start", SelectionFromRange(r, RTL).WithStart(n), Selection{Anchor: r.End, Active: n}},
		{"RTL end", SelectionFromRange(r, RTL).WithEnd(n), Selection{Anchor: n, Active: r.Start}},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %+v, want %+v", tt.name, tt.got, tt.want)
		}
	}
}

func TestSelectionsEqual(t *testing.T) {
	a := []Selection{NewCursor(Position{0, 0}), {Anchor: Position{1, 0}, Active: Position{1, 4}}}
	b := []Selection{a[0], a[1]}
	if !SelectionsEqual(a, b) {
		t.Error("equal lists reported different")
	}
	b[1].Active = Position{1, 5}
	if SelectionsEqual(a, b) {
		t.Error("different lists reported equal")
	}
	if SelectionsEqual(a, a[:1]) {
		t.Error("lists of different length reported equal")
	}
	if !SelectionsEqual(nil, []Selection{}) {
		t.Error("two empty lists reported different")
	}
}
