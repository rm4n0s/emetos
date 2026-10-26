package folding

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/document"
)

func TestSanitize(t *testing.T) {
	// Ported from VS Code: src/vs/editor/contrib/folding/test/browser/syntaxFold.test.ts (sanitizeRanges), as far as the cases
	// need no provider; lines are 0-based here.
	tests := []struct {
		name   string
		ranges []SyntaxRange
		limit  Limit
		want   string
	}{
		{"nested ranges", []SyntaxRange{{Start: 0, End: 10}, {Start: 2, End: 4}, {Start: 6, End: 8}}, Limit{}, "[ -] 0/10, [ -] 2/4, [ -] 6/8"},
		{"unsorted ranges are sorted", []SyntaxRange{{Start: 6, End: 8}, {Start: 0, End: 10}, {Start: 2, End: 4}}, Limit{}, "[ -] 0/10, [ -] 2/4, [ -] 6/8"},
		{"the same start: the earlier provider wins and the other is dropped", []SyntaxRange{{Start: 1, End: 5, Rank: 1}, {Start: 1, End: 3, Rank: 0}}, Limit{}, "[ -] 1/3"},
		{"a range that crosses the one before it is dropped", []SyntaxRange{{Start: 0, End: 5}, {Start: 3, End: 8}, {Start: 6, End: 7}}, Limit{}, "[ -] 0/5, [ -] 6/7"},
		{"siblings after a nested range", []SyntaxRange{{Start: 0, End: 9}, {Start: 1, End: 3}, {Start: 4, End: 6}, {Start: 7, End: 8}}, Limit{}, "[ -] 0/9, [ -] 1/3, [ -] 4/6, [ -] 7/8"},
		{"a range after the whole of an outer one", []SyntaxRange{{Start: 0, End: 3}, {Start: 1, End: 2}, {Start: 5, End: 7}}, Limit{}, "[ -] 0/3, [ -] 1/2, [ -] 5/7"},
		{"the outermost levels are kept when there are too many", []SyntaxRange{{Start: 0, End: 20}, {Start: 1, End: 5}, {Start: 2, End: 3}, {Start: 6, End: 9}, {Start: 7, End: 8}}, Limit{Max: 3}, "[ -] 0/20, [ -] 1/5, [ -] 6/9"},
		{"nothing", nil, Limit{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Sanitize(tt.ranges, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tt.want {
				t.Errorf("regions = %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestSanitizeKindsAndTheLimitReport(t *testing.T) {
	var computed, limited int
	r, err := Sanitize([]SyntaxRange{{Start: 0, End: 5, Kind: KindComment}, {Start: 6, End: 9}, {Start: 10, End: 12, Kind: KindImports}},
		Limit{Max: 2, Update: func(c, l int) { computed, limited = c, l }})
	if err != nil {
		t.Fatal(err)
	}
	if computed != 3 || limited != 2 || r.Len() != 2 {
		t.Errorf("computed %d, limited %d, %d regions", computed, limited, r.Len())
	}
	r, err = Sanitize([]SyntaxRange{{Start: 0, End: 5, Kind: KindComment}, {Start: 6, End: 9}}, Limit{})
	if err != nil || !r.HasTypes() || r.Type(0) != KindComment || r.Type(1) != "" {
		t.Errorf("kinds = %v %v", r, err)
	}
	if r, _ := Sanitize([]SyntaxRange{{Start: 0, End: 5}}, Limit{}); r.HasTypes() {
		t.Error("no kind, no types")
	}
	if r, _ := Sanitize([]SyntaxRange{{Start: 0, End: MaxLine + 1}}, Limit{}); r.Len() != 0 {
		t.Error("a line beyond the largest is left out")
	}
}

func TestUpdaterTagAndRefresh(t *testing.T) {
	doc := document.New(document.UntitledURI(1), "a\n  b\nc\n  d", document.NewOptions())
	seen := make(chan any, 8)
	calls := make(chan struct{}, 8)
	p := tagProvider{seen: seen}
	n := 0
	u := AttachUpdater(doc, p, UpdaterOptions{Delay: time.Hour, Tag: func() any { n++; return n }}, func(*Regions) { calls <- struct{}{} })
	defer u.Stop()
	wait := func(want any) {
		t.Helper()
		select {
		case got := <-seen:
			if got != want {
				t.Errorf("tag = %v, want %v", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no computation")
		}
		select {
		case <-calls:
		case <-time.After(5 * time.Second):
			t.Fatal("no regions")
		}
	}
	wait(1)
	// the delay is an hour: only Refresh makes a computation, for the tag of the time it was asked
	u.Refresh()
	wait(2)
	if TagOf(context.Background()) != nil {
		t.Error("a context without a tag has none")
	}
}

type tagProvider struct{ seen chan any }

func (p tagProvider) Compute(ctx context.Context, _ Text, _ int) (*Regions, error) {
	p.seen <- TagOf(ctx)
	return NewRegions(nil, nil, nil)
}
