package testing_test

import (
	"testing"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

func rng(l1, c1, l2, c2 int) ct.Range {
	return ct.Range{Start: ct.Pos{Line: l1, Col: c1}, End: ct.Pos{Line: l2, Col: c2}}
}

// BuildDetailRanges cuts nested details into ranges that do not overlap: the outer one keeps the pieces around the inner one, and only
// its first piece is primary (VS Code's CoverageDetailsModel).
func TestBuildDetailRanges(t *testing.T) {
	type piece struct {
		r       ct.Range
		kind    ct.DetailKind
		primary bool
		hits    int
	}
	cases := []struct {
		name string
		d    ct.Details
		want []piece
	}{
		{
			name: "statements side by side",
			d:    ct.Details{Statements: []ct.StatementDetail{{Executed: 1, Location: rng(0, 0, 1, 0)}, {Executed: 0, Location: rng(1, 0, 2, 0)}}},
			want: []piece{{rng(0, 0, 1, 0), ct.DetailStatement, true, 1}, {rng(1, 0, 2, 0), ct.DetailStatement, true, 0}},
		},
		{
			name: "a function with a statement in it keeps the pieces around the statement",
			d: ct.Details{
				Declarations: []ct.DeclarationDetail{{Name: "f", Executed: 2, Location: rng(0, 0, 4, 1)}},
				Statements:   []ct.StatementDetail{{Executed: 2, Location: rng(1, 1, 2, 0)}},
			},
			want: []piece{
				{rng(0, 0, 1, 1), ct.DetailDeclaration, true, 2},
				{rng(1, 1, 2, 0), ct.DetailStatement, true, 2},
				{rng(2, 0, 4, 1), ct.DetailDeclaration, false, 2},
			},
		},
		{
			name: "a branch with no place is the empty range at the end of its statement",
			d: ct.Details{Statements: []ct.StatementDetail{
				{Executed: 3, Location: rng(5, 0, 6, 0), Branches: []ct.BranchDetail{{Executed: 3}, {Executed: 0}}},
			}},
			want: []piece{
				{rng(5, 0, 6, 0), ct.DetailStatement, true, 3},
				{rng(6, 0, 6, 0), ct.DetailBranch, true, 3},
				{rng(6, 0, 6, 0), ct.DetailBranch, true, 0},
			},
		},
		{
			name: "a branch with a place that is a statement's own range empties the statement's piece",
			d: ct.Details{Statements: []ct.StatementDetail{
				{Executed: 1, Location: rng(8, 0, 9, 0), Branches: []ct.BranchDetail{{Executed: 0, Location: &ct.Range{Start: ct.Pos{Line: 8}, End: ct.Pos{Line: 9}}}}},
			}},
			want: []piece{{rng(8, 0, 8, 0), ct.DetailStatement, true, 1}, {rng(8, 0, 9, 0), ct.DetailBranch, true, 0}},
		},
		{
			name: "no details",
			d:    ct.Details{},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ct.BuildDetailRanges(c.d)
			var pieces []piece
			for _, g := range got {
				pieces = append(pieces, piece{g.Range, g.Kind, g.Primary, g.Executed})
			}
			if len(pieces) != len(c.want) {
				t.Fatalf("%d ranges %+v, want %d %+v", len(pieces), pieces, len(c.want), c.want)
			}
			for i := range pieces {
				if pieces[i] != c.want[i] {
					t.Errorf("range %d is %+v, want %+v", i, pieces[i], c.want[i])
				}
			}
		})
	}
}
