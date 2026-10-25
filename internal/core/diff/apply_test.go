package diff

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func change(o1, o2, m1, m2 int) LineRangeMapping {
	return LineRangeMapping{Original: LineRange{o1, o2}, Modified: LineRange{m1, m2}}
}

func TestApply(t *testing.T) {
	tests := []struct {
		name     string
		original string
		modified string
		changes  []LineRangeMapping
		want     string
	}{
		{"no changes", "a b c", "a b c", nil, "a b c"},
		{"replace", "a b c", "a x c", []LineRangeMapping{change(2, 3, 2, 3)}, "a x c"},
		{"insert", "a c", "a b c", []LineRangeMapping{change(2, 2, 2, 3)}, "a b c"},
		{"delete", "a b c", "a c", []LineRangeMapping{change(2, 3, 2, 2)}, "a c"},
		{"two changes", "a b c d e", "x b c y z e", []LineRangeMapping{change(1, 2, 1, 2), change(4, 5, 4, 6)}, "x b c y z e"},
		{"everything", "a b", "x y z", []LineRangeMapping{change(1, 3, 1, 4)}, "x y z"},
		{"empty original", "", "x y", []LineRangeMapping{change(1, 1, 1, 3)}, "x y"},
		{"to empty", "x y", "", []LineRangeMapping{change(1, 3, 1, 1)}, ""},
		{"out of range is clipped", "a b", "a b", []LineRangeMapping{change(1, 9, 1, 9)}, "a b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Apply(fields(tc.original), fields(tc.modified), tc.changes)
			if want := fields(tc.want); !equalLines(got, want) {
				t.Errorf("Apply = %q, want %q", got, want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name         string
		changes      []LineRangeMapping
		nOrig, nMod  int
		wantInvalid  bool
		reasonSubstr string
	}{
		{"no changes, equal sizes", nil, 3, 3, false, ""},
		{"no changes, different sizes", nil, 3, 4, true, "after the last change"},
		{"empty texts", nil, 0, 0, false, ""},
		{"one change", []LineRangeMapping{change(2, 3, 2, 4)}, 3, 4, false, ""},
		{"change at the start", []LineRangeMapping{change(1, 2, 1, 3)}, 3, 4, false, ""},
		{"change at the end", []LineRangeMapping{change(3, 4, 3, 5)}, 3, 4, false, ""},
		{"whole texts", []LineRangeMapping{change(1, 4, 1, 3)}, 3, 2, false, ""},
		{"two changes", []LineRangeMapping{change(1, 2, 1, 2), change(4, 5, 4, 6)}, 5, 6, false, ""},
		{"gap before the change differs", []LineRangeMapping{change(2, 3, 3, 4)}, 3, 4, true, "before the change"},
		{"gap between the changes differs", []LineRangeMapping{change(1, 2, 1, 2), change(4, 5, 5, 6)}, 5, 6, true, "before the change"},
		{"touching changes", []LineRangeMapping{change(1, 2, 1, 2), change(2, 3, 2, 3)}, 3, 3, true, "touch"},
		{"overlapping changes", []LineRangeMapping{change(1, 3, 1, 3), change(2, 4, 2, 4)}, 4, 4, true, "overlap"},
		{"unsorted changes", []LineRangeMapping{change(4, 5, 4, 5), change(1, 2, 1, 2)}, 5, 5, true, ""},
		{"empty change", []LineRangeMapping{change(2, 2, 2, 2)}, 3, 3, true, "empty change"},
		{"start below one", []LineRangeMapping{change(0, 1, 0, 1)}, 3, 3, true, "out of its text"},
		{"end past the text", []LineRangeMapping{change(2, 5, 2, 3)}, 3, 3, true, "out of its text"},
		{"reversed range", []LineRangeMapping{change(3, 2, 3, 2)}, 3, 3, true, "out of its text"},
		{"modified past the text", []LineRangeMapping{change(1, 2, 1, 9)}, 3, 3, true, "out of its text"},
		{"tail differs", []LineRangeMapping{change(1, 2, 1, 2)}, 4, 3, true, "after the last change"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.changes, tc.nOrig, tc.nMod)
			if !tc.wantInvalid {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("Validate.DiffInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if reason, _ := appErr.Metadata["reason"].(string); tc.reasonSubstr != "" && !strings.Contains(reason, tc.reasonSubstr) {
				t.Errorf("reason = %q, want it to mention %q", reason, tc.reasonSubstr)
			}
		})
	}
}

func TestLineRange(t *testing.T) {
	r := LineRange{StartLine: 3, EndLine: 7}
	if r.Len() != 4 || r.IsEmpty() {
		t.Errorf("%+v: Len %d, IsEmpty %v", r, r.Len(), r.IsEmpty())
	}
	if e := (LineRange{StartLine: 5, EndLine: 5}); e.Len() != 0 || !e.IsEmpty() {
		t.Errorf("%+v: Len %d, IsEmpty %v", e, e.Len(), e.IsEmpty())
	}
}
